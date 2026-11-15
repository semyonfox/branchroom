// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package user

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	activities_model "gitea.dev/models/activities"
	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	git_model "gitea.dev/models/git"
	issues_model "gitea.dev/models/issues"
	"gitea.dev/models/organization"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unit"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/commitstatus"
	"gitea.dev/modules/container"
	"gitea.dev/modules/optional"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/templates"
	"gitea.dev/services/context"

	"xorm.io/builder"
)

type homeWorkCard struct {
	Issue *issues_model.Issue
	State string
}

type homeUpdate struct {
	Action *activities_model.Action
	Pushes int
}

func Home(ctx *context.Context) {
	prepareDashboardContextUserOrgTeams(ctx)
	if ctx.Written() {
		return
	}
	ctx.Data["Title"] = ctx.Tr("home.your_work")
	ctx.Data["PageIsDashboard"] = true
	ctx.Data["MirrorsEnabled"] = setting.Mirror.Enabled
	count, err := organization.GetOrganizationCount(ctx, ctx.Doer)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	ctx.Data["UserOrgsCount"] = count
	ctx.PageData["dashboardRepoList"] = map[string]any{"searchLimit": setting.UI.User.RepoPagingNum, "uid": ctx.Doer.ID}
	for _, prepare := range []func(*context.Context) error{prepareHomeWork, prepareHomeUpdates, prepareHomeDiscover} {
		if err := prepare(ctx); err != nil {
			ctx.ServerErrorAuto(err)
			return
		}
	}
	ctx.HTML(http.StatusOK, templates.TplName("user/home/page"))
}

func prepareHomeWork(ctx *context.Context) error {
	uid := strconv.FormatInt(ctx.Doer.ID, 10)
	opts := &issues_model.IssuesOptions{
		Doer: ctx.Doer, IsClosed: optional.Some(false), IsArchived: optional.Some(false),
		SortType: "recentupdate", Paginator: &db.ListOptions{Page: 1, PageSize: 8},
		IsPull: optional.Some(true), PosterID: uid,
	}
	pulls, err := issues_model.Issues(ctx, opts)
	if err != nil {
		return err
	}
	assigned, err := issues_model.Issues(ctx, opts.Copy(func(o *issues_model.IssuesOptions) {
		o.IsPull = optional.Some(false)
		o.PosterID = ""
		o.AssigneeID = uid
	}))
	if err != nil {
		return err
	}
	issues := slices.Concat(pulls, assigned)
	if len(issues) == 0 {
		return nil
	}
	if err := issues.LoadPullRequests(ctx); err != nil {
		return err
	}
	if _, err := issues.LoadRepositories(ctx); err != nil {
		return err
	}
	ids := make([]int64, 0, len(pulls))
	branchCond := builder.NewCond()
	for _, issue := range pulls {
		ids = append(ids, issue.ID)
		if pr := issue.PullRequest; pr != nil {
			branchCond = branchCond.Or(builder.Eq{"repo_id": pr.HeadRepoID, "name": pr.HeadBranch, "is_deleted": false})
		}
	}
	var reviews []*issues_model.Review
	var branches []*git_model.Branch
	if len(ids) > 0 {
		if err := db.GetEngine(ctx).Cols("issue_id", "reviewer_id", "reviewer_team_id", "type").In("issue_id", ids).In("type", issues_model.ReviewTypeApprove, issues_model.ReviewTypeReject, issues_model.ReviewTypeRequest).
			Where("dismissed = ?", false).Desc("id").Find(&reviews); err != nil {
			return err
		}
	}
	reviewIDs := make([]int64, 0, len(reviews))
	for _, review := range reviews {
		reviewIDs = append(reviewIDs, review.ID)
	}
	agentReviews, err := branchroom_model.AgentReviewIDs(ctx, reviewIDs)
	if err != nil {
		return err
	}
	statuses := map[git_model.RepoSHA]commitstatus.CommitStatusState{}
	if branchCond.IsValid() {
		if err := db.GetEngine(ctx).Where(branchCond).Find(&branches); err != nil {
			return err
		}
		var pairs []git_model.RepoSHA
		for _, issue := range pulls {
			pr := issue.PullRequest
			if pr == nil {
				continue
			}
			for _, branch := range branches {
				if branch.RepoID == pr.HeadRepoID && branch.Name == pr.HeadBranch {
					pr.HeadCommitID = branch.CommitID
					pairs = append(pairs, git_model.RepoSHA{RepoID: pr.BaseRepoID, SHA: branch.CommitID})
				}
			}
		}
		if len(pairs) > 0 {
			byRepo, err := git_model.GetLatestCommitStatusForPairs(ctx, pairs)
			if err != nil {
				return err
			}
			for _, pair := range pairs {
				var matching []*git_model.CommitStatus
				for _, status := range byRepo[pair.RepoID] {
					if status.SHA == pair.SHA && !(status.CreatorID == user_model.ActionsUserID && strings.HasSuffix(status.Context, " (workflow_dispatch)")) {
						matching = append(matching, status)
					}
				}
				if status := git_model.CalcCommitStatus(matching); status != nil {
					statuses[pair] = status.State
				}
			}
		}
	}
	cards := make([]homeWorkCard, 0, len(issues))
	for _, issue := range issues {
		state := "assigned"
		if pr := issue.PullRequest; pr != nil {
			state = homePullState(pr, statuses[git_model.RepoSHA{RepoID: pr.BaseRepoID, SHA: pr.HeadCommitID}], reviews, agentReviews)
		}
		cards = append(cards, homeWorkCard{Issue: issue, State: state})
	}
	ctx.Data["HomeWork"] = cards
	return nil
}

func homePullState(pr *issues_model.PullRequest, status commitstatus.CommitStatusState, reviews []*issues_model.Review, agentReviews container.Set[int64]) string {
	if issues_model.HasWorkInProgressPrefix(pr.Issue.Title) {
		return "draft"
	}
	if status.IsFailure() || status.IsError() {
		return "checks_failing"
	}
	// a request that survives means the reviewer still owes a review, whatever an agent did with their token
	requested := false
	pending := map[[2]int64]bool{}
	for _, review := range reviews {
		if review.IssueID == pr.IssueID && review.Type == issues_model.ReviewTypeRequest {
			pending[[2]int64{review.ReviewerID, review.ReviewerTeamID}] = true
			requested = true
		}
	}
	ownDecision := map[[2]int64]bool{}
	for _, review := range reviews {
		if review.IssueID == pr.IssueID && review.Type != issues_model.ReviewTypeRequest && !agentReviews.Contains(review.ID) {
			ownDecision[[2]int64{review.ReviewerID, review.ReviewerTeamID}] = true
		}
	}
	seen := map[[2]int64]bool{}
	for _, review := range reviews {
		if review.IssueID != pr.IssueID {
			continue
		}
		key := [2]int64{review.ReviewerID, review.ReviewerTeamID}
		// reviews come newest first, and an agent with a person's token never stands in for that person's own decision
		if seen[key] || pending[key] || (agentReviews.Contains(review.ID) && ownDecision[key]) {
			continue
		}
		seen[key] = true
		if review.Type == issues_model.ReviewTypeReject {
			return "changes_requested"
		}
	}
	if requested {
		return "review_requested"
	}
	if pr.Status == issues_model.PullRequestStatusConflict {
		return "conflicts"
	}
	if status.IsSuccess() {
		if pr.Status == issues_model.PullRequestStatusMergeable {
			return "ready"
		}
		return "checks_passing"
	}
	return "checks_pending"
}

func prepareHomeUpdates(ctx *context.Context) error {
	cond, err := activities_model.ActivityQueryCondition(ctx, activities_model.GetFeedsOptions{Actor: ctx.Doer, IncludePrivate: true})
	if err != nil {
		return err
	}
	uid := ctx.Doer.ID
	watched := builder.Select("repo_id").From("watch").Where(builder.Eq{"user_id": uid}.And(builder.In("mode", repo_model.WatchModeNormal, repo_model.WatchModeAuto, repo_model.WatchModeCustom)))
	starred := builder.Select("repo_id").From("star").Where(builder.Eq{"uid": uid})
	followed := builder.Select("follow_id").From("follow").Where(builder.Eq{"user_id": uid})
	network := builder.Or(builder.Eq{"act_user_id": uid}, builder.In("repo_id", watched), builder.In("repo_id", starred), builder.In("act_user_id", followed))
	canRead := func(unitType unit.Type) builder.Cond {
		return builder.In("repo_id", builder.Select("id").From("repository").Where(repo_model.AccessibleRepositoryCondition(ctx.Doer, unitType)))
	}
	readable := builder.Or(
		builder.In("op_type", activities_model.ActionCreateRepo, activities_model.ActionRenameRepo, activities_model.ActionTransferRepo, activities_model.ActionStarRepo, activities_model.ActionWatchRepo),
		builder.In("op_type", activities_model.ActionCommitRepo, activities_model.ActionPushTag, activities_model.ActionDeleteTag, activities_model.ActionDeleteBranch,
			activities_model.ActionMirrorSyncPush, activities_model.ActionMirrorSyncCreate, activities_model.ActionMirrorSyncDelete).And(canRead(unit.TypeCode)),
		builder.Eq{"op_type": activities_model.ActionPublishRelease}.And(canRead(unit.TypeReleases)),
		builder.In("op_type", activities_model.ActionCreateIssue, activities_model.ActionCommentIssue, activities_model.ActionCloseIssue, activities_model.ActionReopenIssue).And(canRead(unit.TypeIssues)),
		builder.In("op_type", activities_model.ActionCreatePullRequest, activities_model.ActionMergePullRequest, activities_model.ActionClosePullRequest,
			activities_model.ActionReopenPullRequest, activities_model.ActionCommentPull, activities_model.ActionApprovePullRequest, activities_model.ActionRejectPullRequest,
			activities_model.ActionPullReviewDismissed, activities_model.ActionPullRequestReadyForReview, activities_model.ActionAutoMergePullRequest).And(canRead(unit.TypePullRequests)),
	)
	var actions activities_model.ActionList
	if err := db.GetEngine(ctx).Where(cond).And(network).And(readable).And("`action`.user_id = `action`.act_user_id").
		Desc("created_unix", "id").Limit(30).Find(&actions); err != nil {
		return err
	}
	if err := actions.LoadAttributes(ctx); err != nil {
		return err
	}
	updates := groupHomeUpdates(actions)
	ctx.Data["HomeUpdates"] = updates[:min(len(updates), 15)]
	return nil
}

func groupHomeUpdates(actions activities_model.ActionList) []homeUpdate {
	var updates []homeUpdate
	for _, action := range actions {
		if action.Repo == nil || action.ActUser == nil {
			continue
		}
		push := action.OpType == activities_model.ActionCommitRepo || action.OpType == activities_model.ActionMirrorSyncPush
		if push && len(updates) > 0 {
			last := &updates[len(updates)-1]
			if last.Pushes > 0 && last.Action.RepoID == action.RepoID {
				last.Pushes++
				continue
			}
		}
		update := homeUpdate{Action: action}
		if push {
			update.Pushes = 1
		}
		updates = append(updates, update)
	}
	return updates
}

func prepareHomeDiscover(ctx *context.Context) error {
	repos, _, err := repo_model.SearchRepository(ctx, repo_model.SearchRepoOptions{
		Actor: ctx.Doer, AllPublic: true, OrderBy: db.SearchOrderByNewest,
		ListOptions: db.ListOptions{Page: 1, PageSize: 8},
	})
	if err != nil {
		return err
	}
	// your own repositories are already on the left, discover is for everyone else's
	repos = slices.DeleteFunc(repos, func(repo *repo_model.Repository) bool { return repo.OwnerID == ctx.Doer.ID })
	ctx.Data["HomeDiscoverRepos"] = repos[:min(len(repos), 3)]
	starred := repo_model.SearchRepositoryCondition(repo_model.SearchRepoOptions{
		Actor: ctx.Doer, AllPublic: true, Private: true, StarredByID: ctx.Doer.ID, UnitType: unit.TypeReleases,
	}).And(repo_model.AccessibleRepositoryCondition(ctx.Doer, unit.TypeReleases))
	var releases []*repo_model.Release
	if err := db.GetEngine(ctx).Where("is_draft = ? AND is_tag = ?", false, false).
		And(builder.In("repo_id", builder.Select("id").From("repository").Where(starred))).
		Desc("published_unix", "id").Limit(3).Find(&releases); err != nil {
		return err
	}
	if len(releases) > 0 {
		ids := make([]int64, 0, len(releases))
		for _, release := range releases {
			ids = append(ids, release.RepoID)
		}
		repos, err := repo_model.GetRepositoriesMapByIDs(ctx, ids)
		if err != nil {
			return err
		}
		for _, release := range releases {
			release.Repo = repos[release.RepoID]
		}
	}
	ctx.Data["HomeReleases"] = releases
	return nil
}
