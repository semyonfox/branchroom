// Copyright 2017 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package user

import (
	"net/http"
	"slices"
	"testing"

	activities_model "gitea.dev/models/activities"
	"gitea.dev/models/db"
	issues_model "gitea.dev/models/issues"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unit"
	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/commitstatus"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/templates"
	"gitea.dev/services/context"
	"gitea.dev/services/contexttest"

	"github.com/stretchr/testify/assert"
)

func TestArchivedIssues(t *testing.T) {
	// Arrange
	setting.UI.IssuePagingNum = 1
	assert.NoError(t, unittest.LoadFixtures())

	ctx, _ := contexttest.MockContext(t, "issues")
	contexttest.LoadUser(t, ctx, 30)
	ctx.Req.Form.Set("state", "open")

	// Assume: User 30 has access to two Repos with Issues, one of the Repos being archived.
	repos, _, _ := repo_model.GetUserRepositories(t.Context(), repo_model.SearchRepoOptions{OwnerID: ctx.Doer.ID})
	assert.Len(t, repos, 3)
	IsArchived := make(map[int64]bool)
	NumIssues := make(map[int64]int)
	for _, repo := range repos {
		IsArchived[repo.ID] = repo.IsArchived
		NumIssues[repo.ID] = repo.NumIssues
	}
	assert.False(t, IsArchived[50])
	assert.Equal(t, 1, NumIssues[50])
	assert.True(t, IsArchived[51])
	assert.Equal(t, 1, NumIssues[51])

	// Act
	Issues(ctx)

	// Assert: One Issue (ID 30) from one Repo (ID 50) is retrieved, while nothing from archived Repo 51 is retrieved
	assert.Equal(t, http.StatusOK, ctx.Resp.WrittenStatus())

	assert.Len(t, ctx.Data["Issues"], 1)
}

func TestIssues(t *testing.T) {
	setting.UI.IssuePagingNum = 1
	assert.NoError(t, unittest.LoadFixtures())

	ctx, _ := contexttest.MockContext(t, "issues")
	contexttest.LoadUser(t, ctx, 2)
	ctx.Req.Form.Set("state", "closed")
	Issues(ctx)
	assert.Equal(t, http.StatusOK, ctx.Resp.WrittenStatus())

	assert.EqualValues(t, true, ctx.Data["IsShowClosed"])
	assert.Len(t, ctx.Data["Issues"], 1)
}

func TestPulls(t *testing.T) {
	setting.UI.IssuePagingNum = 20
	assert.NoError(t, unittest.LoadFixtures())

	ctx, _ := contexttest.MockContext(t, "pulls")
	contexttest.LoadUser(t, ctx, 2)
	ctx.Req.Form.Set("state", "open")
	Pulls(ctx)
	assert.Equal(t, http.StatusOK, ctx.Resp.WrittenStatus())

	assert.Len(t, ctx.Data["Issues"], 5)
}

func TestMilestones(t *testing.T) {
	setting.UI.IssuePagingNum = 1
	assert.NoError(t, unittest.LoadFixtures())

	for _, repoID := range []string{"", "1"} {
		t.Run("repo="+repoID, func(t *testing.T) {
			ctx, _ := contexttest.MockContext(t, "milestones")
			contexttest.LoadUser(t, ctx, 2)
			ctx.SetPathParam("sort", "issues")
			if repoID != "" {
				ctx.SetPathParam("repo", repoID)
			}
			ctx.Req.Form.Set("state", "closed")
			ctx.Req.Form.Set("sort", "furthestduedate")
			Milestones(ctx)
			assert.Equal(t, http.StatusOK, ctx.Resp.WrittenStatus())
			assert.EqualValues(t, map[int64]int64{1: 1}, ctx.Data["Counts"])
			assert.EqualValues(t, true, ctx.Data["IsShowClosed"])
			assert.EqualValues(t, "furthestduedate", ctx.Data["SortType"])
			assert.EqualValues(t, 1, ctx.Data["Total"])
			assert.Len(t, ctx.Data["Milestones"], 1)
			assert.Len(t, ctx.Data["Repos"], 2) // both repo 42 and 1 have milestones and both are owned by user 2
		})
	}
}

func TestDashboardPagination(t *testing.T) {
	ctx, _ := contexttest.MockContext(t, "/", contexttest.MockContextOption{Render: templates.PageRenderer()})
	page := context.NewPagerBuilder(ctx).TotalCount(10).PerPageLimit(3).CurPage(1).NavPageNum(3).Build()

	for _, subURL := range []string{"/SubPath", ""} {
		setting.AppSubURL = subURL
		out, err := ctx.RenderToHTML("base/paginate", map[string]any{"Link": setting.AppSubURL, "Page": page})
		assert.NoError(t, err)
		assert.Contains(t, out, `<a class=" item navigation" href="`+subURL+`/?page=2">`)
	}
}

func TestHomeDashboard(t *testing.T) {
	assert.NoError(t, unittest.LoadFixtures())
	ctx, _ := contexttest.MockContext(t, "/")
	contexttest.LoadUser(t, ctx, 2)
	Home(ctx)
	assert.Equal(t, http.StatusOK, ctx.Resp.WrittenStatus())
	assert.NotContains(t, ctx.Data, "HeatmapURL")
	action := &activities_model.Action{UserID: 2, ActUserID: 2, RepoID: 1, OpType: activities_model.ActionCreateIssue, Content: "1|Issue title"}
	assert.NoError(t, db.Insert(ctx, action))
	assert.NoError(t, prepareHomeUpdates(ctx))
	updates, ok := ctx.Data["HomeUpdates"].([]homeUpdate)
	assert.True(t, ok)
	assert.True(t, slices.ContainsFunc(updates, func(update homeUpdate) bool { return update.Action.ID == action.ID }))
	_, err := db.GetEngine(ctx).Where("repo_id = ? AND type = ?", 1, unit.TypeIssues).Delete(new(repo_model.RepoUnit))
	assert.NoError(t, err)
	assert.NoError(t, prepareHomeUpdates(ctx))
	updates, ok = ctx.Data["HomeUpdates"].([]homeUpdate)
	assert.True(t, ok)
	assert.False(t, slices.ContainsFunc(updates, func(update homeUpdate) bool { return update.Action.ID == action.ID }))
	cards, ok := ctx.Data["HomeWork"].([]homeWorkCard)
	assert.True(t, ok)
	for _, card := range cards {
		assert.False(t, card.Issue.IsClosed)
		assert.NotNil(t, card.Issue.Repo)
		if card.Issue.IsPull {
			assert.Equal(t, ctx.Doer.ID, card.Issue.PosterID)
		} else {
			assert.True(t, slices.ContainsFunc(card.Issue.Assignees, func(user *user_model.User) bool { return user.ID == ctx.Doer.ID }))
		}
	}
}

func TestHomeWorkAndUpdates(t *testing.T) {
	pr := &issues_model.PullRequest{IssueID: 1, Status: issues_model.PullRequestStatusMergeable, Issue: &issues_model.Issue{Title: "Fix tide dates"}}
	reviews := []*issues_model.Review{
		{IssueID: 1, ReviewerID: 2, Type: issues_model.ReviewTypeReject},
		{IssueID: 1, ReviewerID: 2, Type: issues_model.ReviewTypeApprove},
	}
	assert.Equal(t, "changes_requested", homePullState(pr, commitstatus.CommitStatusSuccess, reviews, nil))
	assert.Equal(t, "checks_failing", homePullState(pr, commitstatus.CommitStatusFailure, nil, nil))
	assert.Equal(t, "checks_pending", homePullState(pr, "", nil, nil))
	assert.Equal(t, "ready", homePullState(pr, commitstatus.CommitStatusSuccess, nil, nil))
	assert.Equal(t, "review_requested", homePullState(pr, commitstatus.CommitStatusSuccess, []*issues_model.Review{{IssueID: 1, Type: issues_model.ReviewTypeRequest}}, nil))

	pr.Status = issues_model.PullRequestStatusConflict
	assert.Equal(t, "conflicts", homePullState(pr, commitstatus.CommitStatusSuccess, nil, nil))
	pr.Status = issues_model.PullRequestStatusChecking
	assert.Equal(t, "checks_passing", homePullState(pr, commitstatus.CommitStatusSuccess, nil, nil))

	repository := &repo_model.Repository{ID: 1}
	actor := &user_model.User{ID: 2}
	push := &activities_model.Action{RepoID: 1, Repo: repository, ActUser: actor, OpType: activities_model.ActionCommitRepo}
	merged := &activities_model.Action{RepoID: 1, Repo: repository, ActUser: actor, OpType: activities_model.ActionMergePullRequest}
	updates := groupHomeUpdates(activities_model.ActionList{push, push, push, merged, push})
	assert.Len(t, updates, 3)
	assert.Equal(t, 3, updates[0].Pushes)
	assert.Same(t, merged, updates[1].Action)
	assert.Equal(t, 1, updates[2].Pushes)
}
