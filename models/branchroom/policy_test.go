// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"testing"
	"time"

	"gitea.dev/models/db"
	"gitea.dev/models/unittest"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrantValidationAndRevocation(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	now := timeutil.TimeStampNow()

	scope := newTestScope()
	require.NoError(t, InsertScope(ctx, scope))
	storedByName, err := GetScopeByRepoIDAndLowerName(ctx, scope.RepoID, scope.LowerName)
	require.NoError(t, err)
	assert.Equal(t, scope.ID, storedByName.ID)
	_, err = GetScopeByRepoIDAndLowerName(ctx, scope.RepoID, "unknown-scope")
	assert.ErrorIs(t, err, util.ErrNotExist)

	grant := &Grant{
		ScopeID:     scope.ID,
		SubjectType: SubjectTypeUser,
		SubjectID:   2,
		Action:      ActionDiscover,
		CreatedByID: 1,
	}
	require.NoError(t, InsertGrant(ctx, grant))

	grants, err := FindActiveGrantsAt(ctx, scope.ID, ActionDiscover, now)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	stored, err := GetActiveDirectUserGrantAt(ctx, scope.ID, 2, ActionDiscover, now)
	require.NoError(t, err)
	assert.Equal(t, grant.ID, stored.ID)
	_, err = GetActiveDirectUserGrantAt(ctx, scope.ID, 3, ActionDiscover, now)
	assert.ErrorIs(t, err, util.ErrNotExist)

	require.NoError(t, RevokeGrant(ctx, grant.ID, 1, now))
	grants, err = FindActiveGrantsAt(ctx, scope.ID, ActionDiscover, now)
	require.NoError(t, err)
	assert.Empty(t, grants)
	_, err = GetActiveDirectUserGrantAt(ctx, scope.ID, 2, ActionDiscover, now)
	assert.ErrorIs(t, err, util.ErrNotExist)
	assert.ErrorIs(t, RevokeGrant(ctx, grant.ID, 1, now), util.ErrNotExist)

	assert.Error(t, (&Grant{
		ScopeID:     scope.ID,
		SubjectType: SubjectTypeActionsTask,
		SubjectID:   3,
		Action:      ActionDiscover,
		CreatedByID: 1,
	}).Validate())
	assert.Error(t, (&Grant{
		ScopeID:     scope.ID,
		SubjectType: SubjectTypeActionsTask,
		SubjectID:   3,
		Action:      ActionDiscover,
		WorkflowID:  "deploy.yml",
		Ref:         "refs/heads/main",
		ExpiresUnix: now + 60,
		CreatedByID: 1,
	}).Validate())
	assert.Error(t, (&Grant{
		ScopeID:     scope.ID,
		SubjectType: SubjectTypeUser,
		SubjectID:   3,
		Action:      ActionDiscover,
		WorkflowID:  "deploy.yml",
		CreatedByID: 1,
	}).Validate())
	assert.Error(t, (&Grant{
		ScopeID:     scope.ID,
		SubjectType: SubjectTypeTeam,
		SubjectID:   3,
		Action:      ActionMaterialize,
		CreatedByID: 1,
	}).Validate())
	assert.Error(t, (&Grant{
		ScopeID:     scope.ID,
		SubjectType: SubjectTypeTeam,
		SubjectID:   3,
		Action:      ActionReadSource,
		CreatedByID: 1,
	}).Validate())
	assert.Error(t, (&Grant{
		ScopeID:           scope.ID,
		SubjectType:       SubjectTypeActionsTask,
		SubjectID:         3,
		Action:            ActionReadSource,
		WorkflowID:        "deploy.yml",
		WorkflowRepoID:    1,
		WorkflowCommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Ref:               "refs/heads/main",
		RunnerID:          1,
		ExpiresUnix:       now + 60,
		CreatedByID:       1,
	}).Validate())
	assert.Error(t, (&Grant{
		ScopeID:     scope.ID,
		SubjectType: SubjectTypeDeployKey,
		SubjectID:   3,
		Action:      ActionReadSource,
		CreatedByID: 1,
	}).Validate())
	assert.NoError(t, (&Grant{
		ScopeID:     scope.ID,
		SubjectType: SubjectTypeUser,
		SubjectID:   3,
		Action:      ActionReadSource,
		CreatedByID: 1,
	}).Validate())
}

func TestRecordDecisionAndAudit(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	defer timeutil.MockSet(time.Date(2026, time.August, 21, 12, 0, 0, 0, time.UTC))()

	scope := newTestScope()
	require.NoError(t, InsertScope(ctx, scope))

	first, err := RecordDecisionAndAudit(ctx, scope, newTestDecision(scope, DecisionResultAllow, "grant_matched"))
	require.NoError(t, err)
	assert.EqualValues(t, 1, first.Sequence)
	assert.Empty(t, first.PreviousHash)
	assert.Equal(t, first.EventHash, scope.AuditHeadHash)

	second, err := RecordDecisionAndAudit(ctx, scope, newTestDecision(scope, DecisionResultDeny, "scope_inactive"))
	require.NoError(t, err)
	assert.EqualValues(t, 2, second.Sequence)
	assert.Equal(t, first.EventHash, second.PreviousHash)
	assert.Equal(t, second.EventHash, scope.AuditHeadHash)

	storedScope, err := GetScopeByID(ctx, scope.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 2, storedScope.AuditSequence)
	assert.Equal(t, second.EventHash, storedScope.AuditHeadHash)

	var decisionCount, auditCount int64
	decisionCount, err = db.GetEngine(ctx).Count(new(Decision))
	require.NoError(t, err)
	auditCount, err = db.GetEngine(ctx).Count(new(AuditEvent))
	require.NoError(t, err)
	assert.EqualValues(t, 2, decisionCount)
	assert.EqualValues(t, 2, auditCount)
	storedScope, err = GetScopeByID(ctx, scope.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 2, storedScope.AuditSequence)
	assert.Equal(t, second.EventHash, storedScope.AuditHeadHash)

	invalid := newTestDecision(scope, DecisionResultDeny, "invalid")
	invalid.ResourceID = "contains a space"
	_, err = RecordDecisionAndAudit(ctx, scope, invalid)
	assert.Error(t, err)
	decisionCount, err = db.GetEngine(ctx).Count(new(Decision))
	require.NoError(t, err)
	auditCount, err = db.GetEngine(ctx).Count(new(AuditEvent))
	require.NoError(t, err)
	assert.EqualValues(t, 2, decisionCount)
	assert.EqualValues(t, 2, auditCount)

	tampered := *second
	tampered.ReasonCode = "tampered"
	assert.Error(t, tampered.Validate())
}

func newTestScope() *Scope {
	return &Scope{
		Kind:        ScopeKindRepository,
		RepoID:      1,
		Name:        "Security Fix",
		State:       ScopeStateActive,
		OwnerType:   ScopeOwnerTypeUser,
		OwnerID:     1,
		CreatedByID: 1,
	}
}

func newTestDecision(scope *Scope, result DecisionResult, reasonCode string) *Decision {
	return &Decision{
		ScopeID:         scope.ID,
		RepoID:          scope.RepoID,
		GrantID:         0,
		ActorType:       ActorTypeUser,
		ActorID:         2,
		Action:          ActionDiscover,
		ResourceType:    ResourceTypeScope,
		ResourceID:      "scope/1",
		Surface:         SurfaceSystem,
		Result:          result,
		ReasonCode:      reasonCode,
		OrdinaryAllowed: true,
	}
}
