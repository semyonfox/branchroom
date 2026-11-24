// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	issues_model "gitea.dev/models/issues"
	access_model "gitea.dev/models/perm/access"
	pull_model "gitea.dev/models/pull"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unit"
	"gitea.dev/modules/setting"
	api "gitea.dev/modules/structs"
	"gitea.dev/modules/util"
	"gitea.dev/services/context"
	inbox_service "gitea.dev/services/inbox"
	pull_service "gitea.dev/services/pull"
)

type inboxQueueBar struct {
	View     string
	Position int // 0 when this pull request has already left the view
	Count    int
	Next     string // where a merge or review hands back to, without the outcome
	SkipURL  string
	ExitURL  string

	Done     *issues_model.Issue // what the previous step acted on
	DoneDid  string
	DoneIcon string

	// every slot always renders, an unusable one says why in its place
	HeadSHA      string
	CanReview    bool
	ReviewReason string
	Why          string   // what stands between this pull request and merged, in plain words
	Fix          queueFix // the merge slot: merge, or the one click that clears the top blocker
	ApproveFirst bool     // the doer's approval is part of the fix, so it is given first
	MergeStyle   repo_model.MergeStyle
	MergeLabel   string
	DeleteBranch bool
}

// queueFix is what the merge slot does. Kind "off" is a disabled button whose label says why
type queueFix struct {
	Kind  string // merge, automerge, update, ready, rerequest, ask, link, off
	Label string
	URL   string
	Data  map[string]string // form fields the action posts
}

// approvalBlockers clear once the doer approves
var approvalBlockers = []pull_service.ReadinessBlocker{pull_service.BlockerApprovals, pull_service.BlockerReviewRequests}

var mergeStyleLabels = map[repo_model.MergeStyle]string{
	repo_model.MergeStyleMerge:           "repo.pulls.merge_pull_request",
	repo_model.MergeStyleSquash:          "repo.pulls.squash_merge_pull_request",
	repo_model.MergeStyleRebase:          "repo.pulls.rebase_merge_pull_request",
	repo_model.MergeStyleRebaseMerge:     "repo.pulls.rebase_merge_commit_pull_request",
	repo_model.MergeStyleFastForwardOnly: "repo.pulls.fast_forward_only_merge_pull_request",
}

// prepareInboxQueue shows the queue bar while the user works through an inbox view
func prepareInboxQueue(ctx *context.Context, issue *issues_model.Issue) {
	view, skipped := inbox_service.ParseQueueCookie(ctx.GetSiteCookie(inbox_service.QueueCookie))
	if view == "" || !ctx.IsSigned {
		return
	}
	items, found, err := inbox_service.Queue(ctx, ctx.Doer, view)
	if err != nil || !found {
		return // the bar is a convenience, never a reason to fail the page
	}
	items = inbox_service.QueueOrder(items, skipped)
	next := fmt.Sprintf("%s/inbox/next?view=%s&from=%d", setting.AppSubURL, url.QueryEscape(view), issue.ID)
	bar := &inboxQueueBar{View: view, Count: len(items), Next: next, SkipURL: next + "&skip=1", ExitURL: setting.AppSubURL + "/inbox/next?exit=1&view=" + url.QueryEscape(view)}
	bar.Position = slices.IndexFunc(items, func(item api.InboxItem) bool { return item.ID == issue.ID }) + 1
	bar.Done, bar.DoneDid = queueReceipt(ctx)
	bar.DoneIcon = receiptIcons[bar.DoneDid]
	switch {
	case !issue.IsPull || issue.PullRequest == nil:
		bar.ReviewReason = ctx.Locale.TrString("inbox.queue_off_issue")
		bar.Fix = queueFix{Kind: "off", Label: bar.ReviewReason}
	case issue.PullRequest.HasMerged:
		bar.ReviewReason = ctx.Locale.TrString("inbox.queue_off_merged")
		bar.Fix = queueFix{Kind: "off", Label: bar.ReviewReason}
	case issue.IsClosed:
		bar.ReviewReason = ctx.Locale.TrString("inbox.queue_off_closed")
		bar.Fix = queueFix{Kind: "off", Label: bar.ReviewReason}
	default:
		prepareQueueDecision(ctx, issue, bar)
	}
	ctx.Data["InboxQueue"] = bar
}

var receiptIcons = map[string]string{
	"merged": "octicon-git-merge", "approve": "octicon-check", "reject": "octicon-file-diff", "comment": "octicon-comment",
	"scheduled": "octicon-clock", "updated": "octicon-sync", "ready": "octicon-check", "rerequested": "octicon-comment", "asked": "octicon-comment",
}

// queueReceipt names the item the last step acted on, if the viewer may still see it
func queueReceipt(ctx *context.Context) (*issues_model.Issue, string) {
	id, did := ctx.FormInt64("done"), ctx.FormString("did")
	if id <= 0 || !slices.Contains([]string{"merged", "approve", "reject", "comment", "scheduled", "updated", "ready", "rerequested", "asked"}, did) {
		return nil, ""
	}
	issue, err := issues_model.GetIssueByID(ctx, id)
	if err != nil || issue.LoadRepo(ctx) != nil {
		return nil, ""
	}
	perm, err := access_model.GetDoerRepoPermission(ctx, issue.Repo, ctx.Doer)
	if err != nil || !perm.CanReadIssuesOrPulls(issue.IsPull) {
		return nil, ""
	}
	return issue, did
}

// blockerOrder puts what a person has to do before what clears on its own, so the bar names the real holdup
var blockerOrder = []pull_service.ReadinessBlocker{
	pull_service.BlockerChangesRequested, pull_service.BlockerConflict, pull_service.BlockerDraft,
	pull_service.BlockerChecksFailing, pull_service.BlockerOutdated, pull_service.BlockerProtectedFiles,
	pull_service.BlockerDependencies, pull_service.BlockerCodeOwners, pull_service.BlockerPathReviews,
	pull_service.BlockerApprovals, pull_service.BlockerReviewRequests, pull_service.BlockerChecksPending,
	pull_service.BlockerChecking, pull_service.BlockerClosed,
}

// waitingOnChecks clear by themselves, so merging when they pass is a real fix
var waitingOnChecks = []pull_service.ReadinessBlocker{pull_service.BlockerChecksPending, pull_service.BlockerChecking}

func prepareQueueDecision(ctx *context.Context, issue *issues_model.Issue, bar *inboxQueueBar) {
	pr := issue.PullRequest
	tr := ctx.Locale.TrString
	bar.ReviewReason = tr("inbox.queue_off_yours")
	bar.Fix = queueFix{Kind: "off", Label: tr("inbox.queue_off_unknown")}
	r, err := pull_service.GetReadiness(ctx, pr)
	if err != nil {
		return // a pull request whose head is gone can still be skipped
	}
	bar.HeadSHA = r.HeadSHA
	isAuthor := issue.IsPoster(ctx.Doer.ID)
	bar.CanReview = !isAuthor && ctx.Repo.Permission.CanRead(unit.TypePullRequests)
	if bar.CanReview {
		bar.ReviewReason = ""
	}
	if prUnit, err := issue.Repo.GetUnit(ctx, unit.TypePullRequests); err == nil {
		cfg := prUnit.PullRequestsConfig()
		bar.MergeStyle = cfg.DefaultMergeStyle
		bar.DeleteBranch = cfg.DefaultDeleteBranchAfterMerge
	}
	bar.MergeLabel = tr(mergeStyleLabels[bar.MergeStyle])
	canMerge, err := pull_service.IsUserAllowedToMerge(ctx, pr, ctx.Repo.Permission, ctx.Doer)
	if err != nil {
		return
	}

	rest := r.Blockers
	if bar.CanReview { // their approval is theirs to give, so it never counts against them
		rest = slices.DeleteFunc(slices.Clone(rest), func(b pull_service.ReadinessBlocker) bool { return slices.Contains(approvalBlockers, b) })
	}
	approveFirst := len(rest) != len(r.Blockers)
	onlyChecks := len(rest) > 0 && !slices.ContainsFunc(rest, func(b pull_service.ReadinessBlocker) bool { return !slices.Contains(waitingOnChecks, b) })
	switch {
	case len(rest) == 0 && canMerge:
		bar.ApproveFirst = approveFirst
		bar.Why = tr(util.Iif(approveFirst, "inbox.queue_why_approve_merge", "inbox.queue_why_ready"))
		bar.Fix = queueFix{Kind: "merge", Label: tr(util.Iif(approveFirst, "inbox.queue_approve_merge", "inbox.queue_merge"))}
		return
	case len(rest) == 0:
		bar.Why = tr("inbox.queue_why_ready_other")
		bar.Fix = queueFix{Kind: "off", Label: tr("inbox.queue_off_permission")}
		return
	case onlyChecks && canMerge:
		if scheduled, _, err := pull_model.GetScheduledMergeByPullID(ctx, pr.ID); err == nil && scheduled {
			bar.Why = tr("inbox.queue_why_scheduled")
			bar.Fix = queueFix{Kind: "off", Label: tr("inbox.queue_off_scheduled")}
			return
		}
		bar.ApproveFirst = approveFirst
		bar.Why = tr("inbox.queue_why_automerge")
		bar.Fix = queueFix{Kind: "automerge", Label: tr(util.Iif(approveFirst, "inbox.queue_fix_approve_automerge", "inbox.queue_fix_automerge"))}
		return
	}

	top := rest[0]
	for _, b := range blockerOrder {
		if slices.Contains(rest, b) {
			top = b
			break
		}
	}
	bar.Fix = queueFix{Kind: "off", Label: tr("inbox.queue_off_" + string(top))}
	bar.Why = tr("inbox.queue_why_" + string(top))
	if err := issue.LoadPoster(ctx); err != nil {
		return
	}
	author := issue.Poster.Name
	switch top {
	case pull_service.BlockerChangesRequested:
		prepareChangesRequested(ctx, issue, r.HeadSHA, isAuthor, bar)
	case pull_service.BlockerConflict:
		files := pr.ConflictedFiles
		if len(files) > 2 {
			files = append(slices.Clone(files[:2]), tr("inbox.queue_and_more"))
		}
		named := strings.Join(files, ", ")
		if named == "" {
			named = tr("inbox.queue_some_files")
		}
		if isAuthor {
			bar.Why = tr("inbox.queue_why_conflict_yours", named, pr.BaseBranch)
			bar.Fix = queueFix{Kind: "link", Label: tr("inbox.queue_fix_how"), URL: issue.Link() + "#pull-conflict-help"}
			return
		}
		bar.Why = tr("inbox.queue_why_conflict", named, pr.BaseBranch, author)
		bar.Fix = queueFix{Kind: "ask", Label: tr("inbox.queue_fix_ask", author), URL: fmt.Sprintf("%s/issues/%d/comments", issue.Repo.Link(), issue.Index), Data: map[string]string{
			"content": tr("inbox.queue_ask_conflict", author, named, pr.BaseBranch, pr.BaseBranch),
		}}
	case pull_service.BlockerDraft:
		if isAuthor || ctx.Repo.Permission.CanWriteIssuesOrPulls(true) {
			prefix := pr.GetWorkInProgressPrefix(ctx)
			bar.Fix = queueFix{Kind: "ready", Label: tr("inbox.queue_fix_ready"), URL: fmt.Sprintf("%s/issues/%d/title", issue.Repo.Link(), issue.Index), Data: map[string]string{
				"title": strings.TrimSpace(issue.Title[len(prefix):]),
			}}
		}
	case pull_service.BlockerChecksFailing:
		for _, status := range r.Statuses {
			if (status.State.IsFailure() || status.State.IsError()) && status.TargetURL != "" {
				bar.Why = tr("inbox.queue_why_checks_failing_named", status.Context)
				bar.Fix = queueFix{Kind: "link", Label: tr("inbox.queue_fix_failing"), URL: status.TargetURL}
				break
			}
		}
	case pull_service.BlockerOutdated:
		if err := pr.LoadHeadRepo(ctx); err != nil {
			return
		}
		styles, err := pull_service.CheckUserAllowedToUpdate(ctx, pr, ctx.Doer)
		if err != nil || !(styles.MergeAllowed || styles.RebaseAllowed) {
			return
		}
		// bringing the base in never rewrites anyone's commits, so it is the default unless only rebase is allowed
		style := util.Iif(styles.MergeAllowed, string(repo_model.UpdateStyleMerge), string(repo_model.UpdateStyleRebase))
		bar.Why = tr("inbox.queue_why_branch_outdated_named", pr.BaseBranch)
		bar.Fix = queueFix{Kind: "update", Label: tr("inbox.queue_fix_update"), URL: issue.Link() + "/update", Data: map[string]string{"style": style}}
	}
}

// prepareChangesRequested names who asked for changes. Once the author has pushed since, asking them again is the fix
func prepareChangesRequested(ctx *context.Context, issue *issues_model.Issue, headSHA string, isAuthor bool, bar *inboxQueueBar) {
	tr := ctx.Locale.TrString
	reviews, err := issues_model.FindLatestReviews(ctx, issues_model.FindReviewOptions{IssueID: issue.ID, PersonsOwnOnly: true, Types: []issues_model.ReviewType{issues_model.ReviewTypeReject, issues_model.ReviewTypeApprove, issues_model.ReviewTypeComment}})
	if err != nil {
		return
	}
	for _, review := range reviews {
		if review.Type != issues_model.ReviewTypeReject || review.Dismissed || review.LoadReviewer(ctx) != nil {
			continue
		}
		name := review.Reviewer.Name
		quote, _, _ := strings.Cut(strings.TrimSpace(review.Content), "\n")
		bar.Why = tr("inbox.queue_why_changes_named", name, quote)
		bar.Fix = queueFix{Kind: "off", Label: tr("inbox.queue_off_changes_named", name)}
		if isAuthor && review.CommitID != headSHA {
			bar.Why = tr("inbox.queue_why_changes_pushed", name)
			bar.Fix = queueFix{Kind: "rerequest", Label: tr("inbox.queue_fix_rerequest", name), URL: issue.Repo.Link() + "/issues/request_review", Data: map[string]string{
				"issue_ids": strconv.FormatInt(issue.ID, 10), "id": strconv.FormatInt(review.ReviewerID, 10), "action": "attach",
			}}
		}
		return
	}
}
