// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package inbox

import (
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	git_model "gitea.dev/models/git"
	issues_model "gitea.dev/models/issues"
	"gitea.dev/models/unittest"
	"gitea.dev/modules/commitstatus"
	pull_service "gitea.dev/services/pull"

	_ "gitea.dev/models"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlockedReason(t *testing.T) {
	r := &pull_service.Readiness{ChecksState: commitstatus.CommitStatusFailure, ReviewDecision: issues_model.PullReviewChangesRequested, Blockers: []pull_service.ReadinessBlocker{pull_service.BlockerChecksFailing, pull_service.BlockerChangesRequested, pull_service.BlockerConflict}, Statuses: []*git_model.CommitStatus{{Context: "test / test (pull_request)", State: commitstatus.CommitStatusFailure, TargetURL: "https://example.org/log"}}}
	reason, action := blockedReason(r, "https://example.org/pull")
	assert.Equal(t, "Checks failing: test / test (pull_request). Changes requested. Merge conflict", reason)
	assert.Equal(t, "Open log", action.Label)
	assert.Equal(t, "https://example.org/log", action.URL)
	reason, _ = blockedReason(&pull_service.Readiness{}, "")
	assert.Empty(t, reason)
}

func TestMain(m *testing.M) {
	unittest.MainTest(m)
}

func TestAgentActivity(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	for _, issueID := range []int64{1, 2} {
		require.NoError(t, db.Insert(ctx, &branchroom_model.AgentAction{Kind: "issue", TargetID: issueID, Agent: "author-agent"}))
		comment := &issues_model.Comment{IssueID: issueID, Type: issues_model.CommentTypeLabel}
		require.NoError(t, db.Insert(ctx, comment))
		require.NoError(t, db.Insert(ctx, &branchroom_model.AgentAction{Kind: "comment", TargetID: comment.ID, Agent: "triage-agent"}))
	}
	comment := &issues_model.Comment{IssueID: 3, Type: issues_model.CommentTypeComment}
	require.NoError(t, db.Insert(ctx, comment))
	require.NoError(t, db.Insert(ctx, &branchroom_model.AgentAction{Kind: "comment", TargetID: comment.ID, Agent: "triage-agent"}))
	activities, err := loadAgents(ctx, []int64{1, 2, 3}, 0)
	require.NoError(t, err)
	assert.Equal(t, "author-agent", activities[1].author.Agent)
	assert.Equal(t, "triage-agent updated labels", activities[1].event)
	assert.Nil(t, activities[3].author)
	assert.Equal(t, "triage-agent commented", activities[3].event)

	review := &issues_model.Review{IssueID: 1, ReviewerID: 2, Type: issues_model.ReviewTypeComment}
	require.NoError(t, db.Insert(ctx, review))
	require.NoError(t, db.Insert(ctx, &branchroom_model.AgentAction{Kind: "review", TargetID: review.ID, Agent: "triage-agent"}))
	count, err := db.GetEngine(ctx).Where("id = ?", review.ID).Count(new(issues_model.Review))
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
	count, err = db.GetEngine(ctx).Where("id = ?", review.ID).And(humanReview()).Count(new(issues_model.Review))
	require.NoError(t, err)
	assert.Zero(t, count)
	human := &issues_model.Review{IssueID: 1, ReviewerID: 2, Type: issues_model.ReviewTypeComment}
	require.NoError(t, db.Insert(ctx, human))
	count, err = db.GetEngine(ctx).Where("id = ?", human.ID).And(humanReview()).Count(new(issues_model.Review))
	require.NoError(t, err)
	assert.EqualValues(t, 1, count)
}

func TestTriageReason(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	require.NoError(t, db.Insert(t.Context(), &issues_model.Review{IssueID: 1, ReviewerID: 2, Type: issues_model.ReviewTypeRequest}))
	reason, err := triageReason(t.Context(), 1)
	require.NoError(t, err)
	assert.Equal(t, "Review requested from user2.", reason)
}
