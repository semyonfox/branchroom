// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unit"
	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBEvaluatorDirectGrantRevocationAndMetadata(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	evaluator := fixedEvaluator(100)
	scope := insertTestScope(t, 1, "security-fix")
	grant := &branchroom_model.Grant{
		ScopeID:     scope.ID,
		SubjectType: branchroom_model.SubjectTypeUser,
		SubjectID:   2,
		Action:      branchroom_model.ActionDiscover,
		CreatedByID: 1,
	}
	require.NoError(t, branchroom_model.InsertGrant(ctx, grant))
	assert.NotZero(t, grant.ID)

	decision, err := evaluator.Evaluate(ctx, testRequest(scope, Actor{Type: branchroom_model.ActorTypeUser, ID: 2}, true))
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	assert.Equal(t, ReasonGrantMatched, decision.ReasonCode)
	assert.NotEmpty(t, decision.Key)

	metadata, err := getScopeMetadata(ctx, evaluator, ScopeMetadataRequest{
		ScopeID: scope.ID,
		RepoID:  scope.RepoID,
		Actor:   Actor{Type: branchroom_model.ActorTypeUser, ID: 2},
		Ceiling: RepositoryCeiling{CodeRead: true},
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.Equal(t, scope.Name, metadata.Name)

	require.NoError(t, branchroom_model.RevokeGrant(ctx, grant.ID, 1, 100))
	decision, err = evaluator.Evaluate(ctx, testRequest(scope, Actor{Type: branchroom_model.ActorTypeUser, ID: 2}, true))
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonNoActiveGrant, decision.ReasonCode)

	_, err = getScopeMetadata(ctx, evaluator, ScopeMetadataRequest{
		ScopeID: scope.ID,
		RepoID:  scope.RepoID,
		Actor:   Actor{Type: branchroom_model.ActorTypeUser, ID: 2},
		Ceiling: RepositoryCeiling{CodeRead: true},
		Surface: branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)

	var decisions, audits int64
	decisions, err = db.GetEngine(ctx).Count(new(branchroom_model.Decision))
	require.NoError(t, err)
	audits, err = db.GetEngine(ctx).Count(new(branchroom_model.AuditEvent))
	require.NoError(t, err)
	assert.Equal(t, decisions, audits)
	assert.GreaterOrEqual(t, decisions, int64(4))
}

func TestDBEvaluatorFailsClosed(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	evaluator := fixedEvaluator(100)
	scope := insertTestScope(t, 1, "restricted-metadata")
	require.NoError(t, branchroom_model.InsertGrant(ctx, &branchroom_model.Grant{
		ScopeID:     scope.ID,
		SubjectType: branchroom_model.SubjectTypeUser,
		SubjectID:   2,
		Action:      branchroom_model.ActionDiscover,
		CreatedByID: 1,
	}))

	decision, err := evaluator.Evaluate(ctx, testRequest(scope, Actor{Type: branchroom_model.ActorTypeUser, ID: 2}, false))
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonOrdinaryPermissionDeny, decision.ReasonCode)
	assert.Nil(t, decision.Scope)

	disabled := insertTestScope(t, 1, "disabled-metadata")
	disabled.State = branchroom_model.ScopeStateDisabled
	_, err = db.GetEngine(ctx).ID(disabled.ID).Cols("state").Update(disabled)
	require.NoError(t, err)
	decision, err = evaluator.Evaluate(ctx, testRequest(disabled, Actor{Type: branchroom_model.ActorTypeUser, ID: 2}, true))
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonScopeInactive, decision.ReasonCode)

	decision, err = evaluator.Evaluate(ctx, Request{
		ScopeID:      scope.ID,
		RepoID:       scope.RepoID,
		Actor:        Actor{Type: branchroom_model.ActorTypeUser, ID: 2},
		Ceiling:      RepositoryCeiling{CodeRead: true},
		Action:       branchroom_model.ActionReadSource,
		ResourceType: branchroom_model.ResourceTypeScope,
		ResourceID:   scopeResourceID(scope.ID),
		Surface:      branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonResourceMismatch, decision.ReasonCode)

	request := testRequest(scope, Actor{Type: branchroom_model.ActorTypeUser, ID: 2}, true)
	request.ResourceType = branchroom_model.ResourceTypeEnvironment
	request.ResourceID = "environment/production"
	decision, err = evaluator.Evaluate(ctx, request)
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonResourceMismatch, decision.ReasonCode)

	request = testRequest(scope, Actor{Type: branchroom_model.ActorTypeUser, ID: 2}, true)
	request.Action = branchroom_model.ActionMaterialize
	decision, err = evaluator.Evaluate(ctx, request)
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonUnsupportedAction, decision.ReasonCode)

	decision, err = evaluator.Evaluate(ctx, testRequest(&branchroom_model.Scope{ID: 9999, RepoID: 1}, Actor{Type: branchroom_model.ActorTypeUser, ID: 2}, true))
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonScopeNotFound, decision.ReasonCode)
}

func TestDBEvaluatorReadSourceAllowsOnlyDedicatedSurfacesAndDirectUsers(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	evaluator := fixedEvaluator(100)
	scope := insertTestScope(t, 1, "restricted-source")
	workspaceKey := uuid.NewString()
	grant := &branchroom_model.Grant{
		ScopeID:     scope.ID,
		SubjectType: branchroom_model.SubjectTypeUser,
		SubjectID:   2,
		Action:      branchroom_model.ActionReadSource,
		CreatedByID: 1,
	}
	require.NoError(t, branchroom_model.InsertGrant(ctx, grant))

	request := Request{
		ScopeID:      scope.ID,
		RepoID:       scope.RepoID,
		Actor:        Actor{Type: branchroom_model.ActorTypeUser, ID: 2},
		Ceiling:      RepositoryCeiling{CodeRead: true},
		Action:       branchroom_model.ActionReadSource,
		ResourceType: branchroom_model.ResourceTypeWorkspace,
		ResourceID:   workspaceResourceID(workspaceKey),
		Surface:      branchroom_model.SurfaceSystem,
	}
	decision, err := evaluator.Evaluate(ctx, request)
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	assert.Equal(t, grant.ID, decision.GrantID)

	request.Surface = branchroom_model.SurfaceGitHTTP
	decision, err = evaluator.Evaluate(ctx, request)
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	assert.Equal(t, grant.ID, decision.GrantID)

	request.Surface = branchroom_model.SurfaceWeb
	decision, err = evaluator.Evaluate(ctx, request)
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonReadSourceUnavailable, decision.ReasonCode)

	request.Surface = branchroom_model.SurfaceGitHTTP
	request.Actor = Actor{Type: branchroom_model.ActorTypeDeployKey, ID: 5}
	decision, err = evaluator.Evaluate(ctx, request)
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonReadSourceUnavailable, decision.ReasonCode)
}

func TestDBEvaluatorWriteSourceRemainsUnavailable(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	evaluator := fixedEvaluator(100)
	scope := insertTestScope(t, 1, "restricted-write")
	workspaceKey := uuid.NewString()
	grant := &branchroom_model.Grant{
		ScopeID:     scope.ID,
		SubjectType: branchroom_model.SubjectTypeUser,
		SubjectID:   2,
		Action:      branchroom_model.ActionWriteSource,
		CreatedByID: 1,
	}
	require.NoError(t, branchroom_model.InsertGrant(ctx, grant))

	request := Request{
		ScopeID:      scope.ID,
		RepoID:       scope.RepoID,
		Actor:        Actor{Type: branchroom_model.ActorTypeUser, ID: 2},
		Ceiling:      RepositoryCeiling{CodeRead: true, CodeWrite: true},
		Action:       branchroom_model.ActionWriteSource,
		ResourceType: branchroom_model.ResourceTypeWorkspace,
		ResourceID:   workspaceResourceID(workspaceKey),
		Surface:      branchroom_model.SurfaceGitHTTP,
	}
	decision, err := evaluator.Evaluate(ctx, request)
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonWriteSourceUnavailable, decision.ReasonCode)
	assert.Zero(t, decision.GrantID)
	assert.NotZero(t, decision.ID)

	request.Surface = branchroom_model.SurfaceWeb
	decision, err = evaluator.Evaluate(ctx, request)
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonWriteSourceUnavailable, decision.ReasonCode)

	request.Surface = branchroom_model.SurfaceGitHTTP
	request.Actor = Actor{Type: branchroom_model.ActorTypeDeployKey, ID: 5}
	decision, err = evaluator.Evaluate(ctx, request)
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonWriteSourceUnavailable, decision.ReasonCode)

	request.Actor = Actor{Type: branchroom_model.ActorTypeUser, ID: 2}
	request.RequestID = "write-source-code-read-only"
	request.Ceiling.CodeWrite = false
	decision, err = evaluator.Evaluate(ctx, request)
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonWriteSourceUnavailable, decision.ReasonCode)

	request.RequestID = "write-source-code-read-denied"
	request.Ceiling = RepositoryCeiling{CodeWrite: true}
	decision, err = evaluator.Evaluate(ctx, request)
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonOrdinaryPermissionDeny, decision.ReasonCode)
	decisionRecord := unittest.AssertExistsAndLoadBean(t, &branchroom_model.Decision{
		ScopeID:         scope.ID,
		Action:          branchroom_model.ActionWriteSource,
		ReasonCode:      ReasonOrdinaryPermissionDeny,
		RequestID:       request.RequestID,
		OrdinaryAllowed: false,
	})
	auditRecord := unittest.AssertExistsAndLoadBean(t, &branchroom_model.AuditEvent{
		DecisionKey:     decisionRecord.DecisionKey,
		ScopeID:         scope.ID,
		Action:          branchroom_model.ActionWriteSource,
		ReasonCode:      ReasonOrdinaryPermissionDeny,
		RequestID:       request.RequestID,
		OrdinaryAllowed: false,
	})
	assert.Equal(t, decisionRecord.DecisionKey, auditRecord.DecisionKey)
}

func TestFindMatchingGrantRejectsRawNonUserWriteSourceGrant(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	scope := insertTestScope(t, 3, "write-source-team")
	require.NoError(t, db.Insert(ctx, &branchroom_model.Grant{
		ScopeID:     scope.ID,
		SubjectType: branchroom_model.SubjectTypeTeam,
		SubjectID:   2,
		Action:      branchroom_model.ActionWriteSource,
		CreatedByID: 2,
	}))

	grant, reasonCode, err := findMatchingGrant(ctx, scope, Request{
		Actor:  Actor{Type: branchroom_model.ActorTypeUser, ID: 2},
		Action: branchroom_model.ActionWriteSource,
	}, 100)
	require.NoError(t, err)
	assert.Nil(t, grant)
	assert.Equal(t, ReasonNoActiveGrant, reasonCode)
}

func TestDBEvaluatorMatchesTeamAndTaskConditions(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	evaluator := fixedEvaluator(100)

	teamScope := insertTestScope(t, 3, "organization-security")
	require.NoError(t, branchroom_model.InsertGrant(ctx, &branchroom_model.Grant{
		ScopeID:     teamScope.ID,
		SubjectType: branchroom_model.SubjectTypeTeam,
		SubjectID:   2,
		Action:      branchroom_model.ActionDiscover,
		CreatedByID: 2,
	}))
	decision, err := evaluator.Evaluate(ctx, testRequest(teamScope, Actor{Type: branchroom_model.ActorTypeUser, ID: 2}, true))
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	decision, err = evaluator.Evaluate(ctx, testRequest(teamScope, Actor{Type: branchroom_model.ActorTypeUser, ID: 5}, true))
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	teamReadSourceGrant := &branchroom_model.Grant{
		ScopeID:     teamScope.ID,
		SubjectType: branchroom_model.SubjectTypeTeam,
		SubjectID:   2,
		Action:      branchroom_model.ActionReadSource,
		CreatedByID: 2,
	}
	require.NoError(t, db.Insert(ctx, teamReadSourceGrant))
	readSourceRequest := Request{
		ScopeID:      teamScope.ID,
		RepoID:       teamScope.RepoID,
		Actor:        Actor{Type: branchroom_model.ActorTypeUser, ID: 2},
		Ceiling:      RepositoryCeiling{CodeRead: true},
		Action:       branchroom_model.ActionReadSource,
		ResourceType: branchroom_model.ResourceTypeWorkspace,
		ResourceID:   workspaceResourceID(uuid.NewString()),
		Surface:      branchroom_model.SurfaceSystem,
	}
	decision, err = evaluator.Evaluate(ctx, readSourceRequest)
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	assert.Equal(t, teamReadSourceGrant.ID, decision.GrantID)
	readSourceRequest.Actor = Actor{Type: branchroom_model.ActorTypeUser, ID: 5}
	decision, err = evaluator.Evaluate(ctx, readSourceRequest)
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonNoActiveGrant, decision.ReasonCode)

	taskScope := insertTestScope(t, 1, "deployment-workflow")
	require.NoError(t, branchroom_model.InsertGrant(ctx, &branchroom_model.Grant{
		ScopeID:           taskScope.ID,
		SubjectType:       branchroom_model.SubjectTypeActionsTask,
		SubjectID:         55,
		Action:            branchroom_model.ActionDiscover,
		WorkflowID:        "deploy.yml",
		WorkflowRepoID:    1,
		WorkflowCommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Ref:               "refs/heads/main",
		RunnerID:          7,
		ExpiresUnix:       200,
		CreatedByID:       1,
	}))
	actor := ActorFromUser(user_model.NewActionsUserWithTaskID(55))
	assert.Equal(t, Actor{Type: branchroom_model.ActorTypeActionsTask, ID: 55}, actor)
	request := testRequest(taskScope, actor, true)
	request.WorkflowID = "deploy.yml"
	request.WorkflowRepoID = 1
	request.WorkflowCommitSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	request.Ref = "refs/heads/main"
	request.RunnerID = 7
	decision, err = evaluator.Evaluate(ctx, request)
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	request.Ref = "refs/heads/release"
	decision, err = evaluator.Evaluate(ctx, request)
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonConditionMismatch, decision.ReasonCode)

	deployKeyScope := insertTestScope(t, 1, "deploy-key-metadata")
	require.NoError(t, branchroom_model.InsertGrant(ctx, &branchroom_model.Grant{
		ScopeID:     deployKeyScope.ID,
		SubjectType: branchroom_model.SubjectTypeDeployKey,
		SubjectID:   17,
		Action:      branchroom_model.ActionDiscover,
		CreatedByID: 1,
	}))
	decision, err = evaluator.Evaluate(ctx, testRequest(deployKeyScope, Actor{Type: branchroom_model.ActorTypeDeployKey, ID: 17}, true))
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
}

func TestDBEvaluatorHonorsGrantTimeWindow(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	scope := insertTestScope(t, 1, "timed-scope")
	require.NoError(t, branchroom_model.InsertGrant(ctx, &branchroom_model.Grant{
		ScopeID:       scope.ID,
		SubjectType:   branchroom_model.SubjectTypeUser,
		SubjectID:     2,
		Action:        branchroom_model.ActionDiscover,
		NotBeforeUnix: 101,
		ExpiresUnix:   200,
		CreatedByID:   1,
	}))

	decision, err := fixedEvaluator(100).Evaluate(ctx, testRequest(scope, Actor{Type: branchroom_model.ActorTypeUser, ID: 2}, true))
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonNoActiveGrant, decision.ReasonCode)
	decision, err = fixedEvaluator(101).Evaluate(ctx, testRequest(scope, Actor{Type: branchroom_model.ActorTypeUser, ID: 2}, true))
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	decision, err = fixedEvaluator(200).Evaluate(ctx, testRequest(scope, Actor{Type: branchroom_model.ActorTypeUser, ID: 2}, true))
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonNoActiveGrant, decision.ReasonCode)
}

func TestDBEvaluatorIsDisabledWithoutAnAuditWrite(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	scope := insertTestScope(t, 1, "disabled-feature")

	decision, err := fixedEvaluator(100).Evaluate(ctx, testRequest(scope, Actor{Type: branchroom_model.ActorTypeUser, ID: 2}, true))
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonFeatureDisabled, decision.ReasonCode)

	count, err := db.GetEngine(ctx).Count(new(branchroom_model.Decision))
	require.NoError(t, err)
	assert.Zero(t, count)
}

func TestCreateRepositoryScopeUsesActualOwnerAndBootstrapsDiscovery(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	scope, err := CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  1,
		Actor:   Actor{Type: branchroom_model.ActorTypeUser, ID: 2},
		Name:    "owner-created",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 2, scope.OwnerID)

	grants, err := branchroom_model.FindActiveGrantsAt(ctx, scope.ID, branchroom_model.ActionDiscover, timeutil.TimeStampNow())
	require.NoError(t, err)
	require.Len(t, grants, 1)
	assert.EqualValues(t, 2, grants[0].SubjectID)

	metadata, err := getScopeMetadata(ctx, fixedEvaluator(timeutil.TimeStampNow()), ScopeMetadataRequest{
		ScopeID: scope.ID,
		RepoID:  scope.RepoID,
		Actor:   Actor{Type: branchroom_model.ActorTypeUser, ID: 2},
		Ceiling: RepositoryCeiling{CodeRead: true},
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.Equal(t, scope.Name, metadata.Name)

	_, err = CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  1,
		Actor:   Actor{Type: branchroom_model.ActorTypeUser, ID: 1},
		Name:    "admin-bypass",
		Surface: branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)
}

func fixedEvaluator(now timeutil.TimeStamp) *DBEvaluator {
	return &DBEvaluator{Now: func() timeutil.TimeStamp { return now }}
}

func enableBranchroom(t *testing.T) {
	t.Helper()
	restore := test.MockVariableValue(&setting.Branchroom)
	t.Cleanup(restore)
	setting.Branchroom.Enabled = true
}

func insertTestScope(t *testing.T, repoID int64, name string) *branchroom_model.Scope {
	t.Helper()
	scope := &branchroom_model.Scope{
		Kind:        branchroom_model.ScopeKindRepository,
		RepoID:      repoID,
		Name:        name,
		State:       branchroom_model.ScopeStateActive,
		OwnerType:   branchroom_model.ScopeOwnerTypeUser,
		OwnerID:     1,
		CreatedByID: 1,
	}
	require.NoError(t, branchroom_model.InsertScope(t.Context(), scope))
	return scope
}

func insertTestEnvironmentScope(t *testing.T, repoID int64, name string) *branchroom_model.Scope {
	t.Helper()
	scope := &branchroom_model.Scope{
		Kind:        branchroom_model.ScopeKindEnvironment,
		RepoID:      repoID,
		Name:        name,
		State:       branchroom_model.ScopeStateActive,
		OwnerType:   branchroom_model.ScopeOwnerTypeUser,
		OwnerID:     1,
		CreatedByID: 1,
	}
	require.NoError(t, branchroom_model.InsertScope(t.Context(), scope))
	return scope
}

func ensureTestRepositoryCodeUnit(ctx context.Context, t *testing.T) {
	t.Helper()
	exists, err := db.GetEngine(ctx).Where("repo_id = ? AND type = ?", 1, unit.TypeCode).Exist(new(repo_model.RepoUnit))
	require.NoError(t, err)
	if exists {
		return
	}
	require.NoError(t, db.Insert(ctx, &repo_model.RepoUnit{
		RepoID: 1,
		Type:   unit.TypeCode,
		Config: &repo_model.UnitConfig{},
	}))
}

func testRequest(scope *branchroom_model.Scope, actor Actor, codeRead bool) Request {
	return Request{
		ScopeID:      scope.ID,
		RepoID:       scope.RepoID,
		Actor:        actor,
		Ceiling:      RepositoryCeiling{CodeRead: codeRead},
		Action:       branchroom_model.ActionDiscover,
		ResourceType: branchroom_model.ResourceTypeScope,
		ResourceID:   scopeResourceID(scope.ID),
		Surface:      branchroom_model.SurfaceSystem,
	}
}
