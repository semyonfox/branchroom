// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"cmp"
	"slices"

	branchroom_model "gitea.dev/models/branchroom"
	git_model "gitea.dev/models/git"
	issues_model "gitea.dev/models/issues"
	access_model "gitea.dev/models/perm/access"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/container"
	"gitea.dev/services/context"
)

type conversationNextInput struct {
	LinkedIndex                                                                int64
	Pull, Closed, Merged, Author, Writer, Requested, Draft, Conflict, Checking bool
	Changes, WaitingReviews, Blocked, CanMerge                                 bool
	Failing, Pending                                                           int
	Assignee, AuthorName, FailingURL                                           string
	Labels                                                                     int
}

type conversationNextStep struct {
	Key, Actor, Tone, Action, URL string
	Count                         int
	Unresolved                    int
}

func issueNextStep(in conversationNextInput) conversationNextStep {
	step := func(key, actor, tone, action, url string) conversationNextStep {
		return conversationNextStep{Key: "repo.conversation." + key, Actor: actor, Tone: tone, Action: action, URL: url, Count: in.Failing}
	}
	switch {
	case in.Merged:
		return step("merged", "", "quiet", "", "")
	case in.Closed:
		return step("closed", "", "quiet", "", "")
	case !in.Pull && in.LinkedIndex > 0:
		return step("linked_open", "", "waiting", "repo.conversation.view_linked", "")
	case !in.Pull && in.Assignee != "":
		return step("assignee", in.Assignee, "waiting", "", "")
	case !in.Pull && in.Writer && in.Labels == 0:
		return step("triage", "", "yours", "repo.conversation.edit_details", "#conversation-details")
	case !in.Pull:
		return step("discussion", "", "waiting", "repo.issues.create_comment", "#comment-form")
	case in.Author && in.Failing > 0:
		return step("your_checks", "", "yours", "repo.conversation.failing_log", in.FailingURL)
	case in.Requested:
		return step("your_review", in.AuthorName, "yours", "repo.conversation.review", "files")
	case in.Changes && in.Author:
		return step("your_changes", "", "yours", "repo.conversation.answer", "#comment-form")
	case in.Changes:
		return step("changes", in.AuthorName, "waiting", "", "")
	case in.Draft:
		return step("draft", in.AuthorName, "waiting", "", "")
	case in.Conflict:
		return step("conflict", in.AuthorName, "waiting", "repo.conversation.resolve", "#pull-merge-box")
	case in.Failing > 0:
		return step("checks", in.AuthorName, "waiting", "repo.conversation.failing_log", in.FailingURL)
	case in.Checking || in.Pending > 0:
		return step("pending", "", "waiting", "", "")
	case in.WaitingReviews:
		return step("reviews", "", "waiting", "", "")
	case in.Blocked:
		return step("blocked", "", "waiting", "repo.conversation.merge_requirements", "#pull-merge-box")
	case in.CanMerge:
		return step("ready", "", "ready", "repo.conversation.merge", "#pull-merge-box")
	default:
		return step("ready_other", "", "ready", "", "")
	}
}

type conversationSummary struct {
	Next                      conversationNextStep
	Checks                    []*git_model.CommitStatus
	Failing, Pending, Passing int
	ReviewCount               int
	Reviews                   issues_model.ReviewList
	Linked                    []*issues_model.Issue
}

func prepareIssueConversation(ctx *context.Context, issue *issues_model.Issue, meta *IssuePageMetaData, pr *pullRequestViewInfo) {
	summary := &conversationSummary{}
	unresolved := 0
	in := conversationNextInput{Pull: issue.IsPull, Closed: issue.IsClosed, Author: ctx.IsSigned && issue.IsPoster(ctx.Doer.ID), Writer: ctx.Repo.Permission.CanWriteIssuesOrPulls(issue.IsPull), AuthorName: issue.Poster.GetDisplayName(), Labels: len(issue.Labels)}
	if len(issue.Assignees) > 0 {
		in.Assignee = issue.Assignees[0].GetDisplayName()
	}
	seen := map[int64]bool{}
	for _, c := range issue.Comments {
		if issues_model.CommentTypeIsRef(c.Type) {
			if err := c.LoadRefIssue(ctx); err != nil {
				if issues_model.IsErrIssueNotExist(err) {
					continue
				}
				ctx.ServerErrorAuto(err)
				return
			}
		}
		if c.RefIssue != nil && c.RefIssue.ID != issue.ID && !seen[c.RefIssue.ID] {
			seen[c.RefIssue.ID] = true
			summary.Linked = append(summary.Linked, c.RefIssue)
		}
	}
	if issue.IsPull {
		pull := issue.PullRequest
		refs, err := pull.ResolveCrossReferences(ctx)
		if err != nil {
			ctx.ServerErrorAuto(err)
			return
		}
		for _, ref := range refs {
			if seen[ref.IssueID] {
				continue
			}
			if err := ref.LoadIssue(ctx); err != nil {
				ctx.ServerErrorAuto(err)
				return
			}
			target := ref.Issue
			if err := target.LoadRepo(ctx); err != nil {
				ctx.ServerErrorAuto(err)
				return
			}
			permission := ctx.Repo.Permission
			if target.RepoID != issue.RepoID {
				permission, err = access_model.GetDoerRepoPermission(ctx, target.Repo, ctx.Doer)
				if err != nil {
					ctx.ServerErrorAuto(err)
					return
				}
			}
			if permission.CanReadIssuesOrPulls(target.IsPull) {
				seen[target.ID] = true
				summary.Linked = append(summary.Linked, target)
			}
		}
		in.Merged, in.Draft, in.Checking = pull.HasMerged, pull.IsWorkInProgress(ctx), pull.IsChecking()
		in.Conflict = !pull.IsStatusMergeable() && !pull.IsEmpty() && !in.Checking

		reviews, err := issues_model.FindReviews(ctx, issues_model.FindReviewOptions{IssueID: issue.ID})
		if err != nil {
			ctx.ServerErrorAuto(err)
			return
		}
		reviewIDs := make([]int64, 0, len(reviews))
		for _, review := range reviews {
			reviewIDs = append(reviewIDs, review.ID)
		}
		agentReviews, err := branchroom_model.AgentReviewIDs(ctx, reviewIDs)
		if err != nil {
			ctx.ServerErrorAuto(err)
			return
		}
		summary.ReviewCount, summary.Reviews = conversationReviews(reviews, agentReviews)
		if err := summary.Reviews.LoadReviewers(ctx); err != nil {
			ctx.ServerErrorAuto(err)
			return
		}
		if err := summary.Reviews.LoadReviewersTeams(ctx); err != nil {
			ctx.ServerErrorAuto(err)
			return
		}
		for _, review := range summary.Reviews {
			if review.Type == issues_model.ReviewTypeReject && !review.Dismissed {
				in.Changes = true
			}
		}
		for _, reviewer := range meta.ReviewersData.CurrentPullReviewers {
			for _, review := range summary.Reviews {
				if review.ReviewerID == reviewer.ItemID && review.OriginalAuthorID == 0 && review.ReviewerTeamID == 0 {
					reviewer.Review = review
					reviewer.Requested = review.Type == issues_model.ReviewTypeRequest
					reviewer.CanChange = meta.ReviewersData.CanChooseReviewer || (ctx.IsSigned && ctx.Doer.ID == review.ReviewerID &&
						(review.Type == issues_model.ReviewTypeRequest || (review.Type == issues_model.ReviewTypeApprove && !issue.IsClosed && !pull.HasMerged && !issue.Repo.IsArchived)))
					reviewer.CanBeDismissed = ctx.Repo.Permission.IsAdmin() && !issue.IsClosed && !pull.HasMerged && !review.Dismissed &&
						(review.Type == issues_model.ReviewTypeApprove || review.Type == issues_model.ReviewTypeReject)
					break
				}
			}
			in.WaitingReviews = in.WaitingReviews || reviewer.Requested
			if reviewer.Requested && reviewer.User != nil && ctx.IsSigned && reviewer.User.ID == ctx.Doer.ID {
				in.Requested = true
			}
		}
		comments, err := issues_model.FindComments(ctx, &issues_model.FindCommentsOptions{IssueID: issue.ID, Type: issues_model.CommentTypeCode})
		if err != nil {
			ctx.ServerErrorAuto(err)
			return
		}
		unresolved = conversationUnresolvedCount(comments, reviews)

		if box := pr.MergeBoxData; box != nil {
			in.Blocked = len(box.infoCommitBlockers.items) > 0 || box.hasOverridableBlockers || (box.requireSigned && !box.willSign) || pr.IsPullRequestBroken
			in.CanMerge = box.hasPermToMerge && box.canMergeNow
			if box.StatusCheckData != nil {
				summary.Checks = box.StatusCheckData.PullCommitStatuses
				summary.Pending = len(box.StatusCheckData.MissingRequiredChecks)
				for _, status := range summary.Checks {
					switch {
					case status.State.IsFailure() || status.State.IsError():
						summary.Failing++
						if in.FailingURL == "" {
							in.FailingURL = status.TargetURL
						}
					case status.State.IsSuccess():
						summary.Passing++
					default:
						summary.Pending++
					}
				}
			}
		}
	}
	in.Failing, in.Pending = summary.Failing, summary.Pending
	if in.FailingURL == "" {
		in.FailingURL = "#conversation-checks"
	}
	for _, linked := range summary.Linked {
		if !issue.IsPull && linked.IsPull && !linked.IsClosed {
			in.LinkedIndex = linked.Index
			break
		}
	}
	summary.Next = issueNextStep(in)
	summary.Next.Unresolved = unresolved
	if in.LinkedIndex > 0 && !in.Closed {
		summary.Next.Count = int(in.LinkedIndex)
		for _, linked := range summary.Linked {
			if linked.Index == in.LinkedIndex {
				summary.Next.URL = linked.Link()
				break
			}
		}
	}
	if summary.Next.URL == "files" {
		summary.Next.URL = issue.Link() + "/files"
	}
	ctx.Data["Conversation"] = summary
}

func conversationReviews(reviews issues_model.ReviewList, agentReviews container.Set[int64]) (int, issues_model.ReviewList) {
	type reviewerKey struct {
		user, team, original int64
		name                 string
	}
	latest := map[reviewerKey]*issues_model.Review{}
	count := 0
	for _, review := range reviews {
		switch review.Type {
		case issues_model.ReviewTypeApprove, issues_model.ReviewTypeReject, issues_model.ReviewTypeComment:
			count++
		case issues_model.ReviewTypeRequest:
		default:
			continue
		}
		key := reviewerKey{user: review.ReviewerID, team: review.ReviewerTeamID, original: review.OriginalAuthorID}
		if review.OriginalAuthorID != 0 {
			key.user = 0
			if review.OriginalAuthorID == user_model.GhostUserID {
				key.name = review.OriginalAuthor
			}
		}
		previous := latest[key]
		// a person's own review deletes their request, so one that survives means they still owe a review
		if review.Type == issues_model.ReviewTypeRequest {
			latest[key] = review
			continue
		}
		if previous != nil && previous.Type == issues_model.ReviewTypeRequest {
			continue
		}
		// an agent working with a person's token never replaces that person's own decision
		if previous != nil && agentReviews.Contains(review.ID) && !agentReviews.Contains(previous.ID) {
			continue
		}
		// comment reviews do not replace the decision used by GetReviewByIssueIDAndUserID
		if previous != nil {
			if review.Type == issues_model.ReviewTypeComment && previous.Type != issues_model.ReviewTypeComment {
				continue
			}
			if review.ID <= previous.ID && !(previous.Type == issues_model.ReviewTypeComment && review.Type != issues_model.ReviewTypeComment) {
				continue
			}
		}
		latest[key] = review
	}
	result := make(issues_model.ReviewList, 0, len(latest))
	for _, review := range latest {
		result = append(result, review)
	}
	slices.SortFunc(result, func(a, b *issues_model.Review) int { return cmp.Compare(a.ID, b.ID) })
	return count, result
}

func conversationUnresolvedCount(comments issues_model.CommentList, reviews issues_model.ReviewList) int {
	submitted := map[int64]bool{}
	for _, review := range reviews {
		submitted[review.ID] = review.Type == issues_model.ReviewTypeApprove || review.Type == issues_model.ReviewTypeReject || review.Type == issues_model.ReviewTypeComment
	}
	type threadKey struct {
		review int64
		path   string
		line   int64
	}
	threads := map[threadKey]*issues_model.Comment{}
	for _, comment := range comments {
		if !submitted[comment.ReviewID] {
			continue
		}
		key := threadKey{review: comment.ReviewID, path: comment.TreePath, line: comment.Line}
		if previous := threads[key]; previous == nil || comment.ID < previous.ID {
			threads[key] = comment
		}
	}
	count := 0
	for _, thread := range threads {
		if !thread.IsResolved() {
			count++
		}
	}
	return count
}
