// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	"gitea.dev/models/unittest"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRotateRepositoryWorkspaceRouteKey(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	owner := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}
	scope, err := CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  1,
		Actor:   owner,
		Name:    "rotate-route-key",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	workspace := insertReadyWorkspaceForGrant(t, scope, owner.ID)
	oldRouteKey := workspace.RouteKey
	decisionsBefore, auditsBefore := workspaceRouteKeyRotationCounts(t)

	err = RotateRepositoryWorkspaceRouteKey(ctx, RotateRepositoryWorkspaceRouteKeyRequest{
		RepoID:    scope.RepoID,
		ScopeID:   scope.ID,
		Actor:     owner,
		Surface:   branchroom_model.SurfaceWeb,
		RequestID: "rotate-route-key",
	})
	require.NoError(t, err)

	_, err = branchroom_model.GetWorkspaceByRouteKey(ctx, oldRouteKey)
	assert.ErrorIs(t, err, util.ErrNotExist)
	rotated, err := branchroom_model.GetWorkspaceByID(ctx, workspace.ID)
	require.NoError(t, err)
	assert.True(t, branchroom_model.IsWorkspaceRouteKey(rotated.RouteKey))
	assert.NotEqual(t, oldRouteKey, rotated.RouteKey)
	assert.Equal(t, workspace.WorkspaceKey, rotated.WorkspaceKey)
	assert.Equal(t, workspace.StoreID, rotated.StoreID)
	assert.Equal(t, branchroom_model.WorkspaceStateReady, rotated.State)

	decision := unittest.AssertExistsAndLoadBean(t, &branchroom_model.Decision{
		ScopeID:      scope.ID,
		RepoID:       scope.RepoID,
		ActorType:    owner.Type,
		ActorID:      owner.ID,
		Action:       branchroom_model.ActionManagePolicy,
		ResourceType: branchroom_model.ResourceTypeWorkspace,
		ResourceID:   workspaceResourceID(workspace.WorkspaceKey),
		Surface:      branchroom_model.SurfaceWeb,
		Result:       branchroom_model.DecisionResultAllow,
		ReasonCode:   "workspace_route_key_rotated",
		RequestID:    "rotate-route-key",
	})
	assert.True(t, decision.OrdinaryAllowed)
	assert.NotContains(t, decision.ResourceID, oldRouteKey)
	assert.NotContains(t, decision.ResourceID, rotated.RouteKey)
	assert.NotContains(t, decision.ResourceID, workspace.StoreID)

	audit := unittest.AssertExistsAndLoadBean(t, &branchroom_model.AuditEvent{
		ScopeID:      scope.ID,
		RepoID:       scope.RepoID,
		ActorType:    owner.Type,
		ActorID:      owner.ID,
		Action:       branchroom_model.ActionManagePolicy,
		ResourceType: branchroom_model.ResourceTypeWorkspace,
		ResourceID:   workspaceResourceID(workspace.WorkspaceKey),
		Surface:      branchroom_model.SurfaceWeb,
		Result:       branchroom_model.DecisionResultAllow,
		ReasonCode:   "workspace_route_key_rotated",
		RequestID:    "rotate-route-key",
	})
	assert.NotContains(t, audit.ResourceID, oldRouteKey)
	assert.NotContains(t, audit.ResourceID, rotated.RouteKey)
	assert.NotContains(t, audit.ResourceID, workspace.StoreID)
	decisionsAfter, auditsAfter := workspaceRouteKeyRotationCounts(t)
	assert.Equal(t, decisionsBefore+1, decisionsAfter)
	assert.Equal(t, auditsBefore+1, auditsAfter)
}

func TestRotateRepositoryWorkspaceRouteKeyFailsClosed(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	owner := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}
	scope, err := CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  1,
		Actor:   owner,
		Name:    "rotate-route-key-denials",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	workspace := insertReadyWorkspaceForGrant(t, scope, owner.ID)
	request := RotateRepositoryWorkspaceRouteKeyRequest{
		RepoID:  scope.RepoID,
		ScopeID: scope.ID,
		Actor:   owner,
		Surface: branchroom_model.SurfaceWeb,
	}
	decisionsBefore, auditsBefore := workspaceRouteKeyRotationCounts(t)
	assertDenied := func(request RotateRepositoryWorkspaceRouteKeyRequest) {
		t.Helper()
		err := RotateRepositoryWorkspaceRouteKey(ctx, request)
		assert.ErrorIs(t, err, util.ErrNotExist)
		assert.Equal(t, "Branchroom workspace does not exist", err.Error())
		stored, err := branchroom_model.GetWorkspaceByID(ctx, workspace.ID)
		require.NoError(t, err)
		assert.Equal(t, workspace.RouteKey, stored.RouteKey)
		decisions, audits := workspaceRouteKeyRotationCounts(t)
		assert.Equal(t, decisionsBefore, decisions)
		assert.Equal(t, auditsBefore, audits)
	}

	setting.Branchroom.Enabled = false
	assertDenied(request)
	setting.Branchroom.Enabled = true

	nonOwner := request
	nonOwner.Actor = Actor{Type: branchroom_model.ActorTypeUser, ID: 1}
	assertDenied(nonOwner)
	invalidActor := request
	invalidActor.Actor = Actor{Type: branchroom_model.ActorTypeActionsTask, ID: 1}
	assertDenied(invalidActor)
	invalidSurface := request
	invalidSurface.Surface = "unknown"
	assertDenied(invalidSurface)
	wrongScope := request
	wrongScope.ScopeID = scope.ID + 999
	assertDenied(wrongScope)
	foreignScope := insertTestScope(t, 2, "foreign-rotate-route-key")
	foreignRequest := request
	foreignRequest.ScopeID = foreignScope.ID
	assertDenied(foreignRequest)
	environmentScope := insertTestEnvironmentScope(t, scope.RepoID, "rotate-route-key-environment")
	environmentRequest := request
	environmentRequest.ScopeID = environmentScope.ID
	assertDenied(environmentRequest)

	_, err = db.GetEngine(ctx).ID(scope.ID).Cols("state").Update(&branchroom_model.Scope{State: branchroom_model.ScopeStateDisabled})
	require.NoError(t, err)
	assertDenied(request)
	_, err = db.GetEngine(ctx).ID(scope.ID).Cols("state").Update(&branchroom_model.Scope{State: branchroom_model.ScopeStateActive})
	require.NoError(t, err)
	_, err = db.GetEngine(ctx).ID(scope.ID).Cols("state").Update(&branchroom_model.Scope{State: branchroom_model.ScopeStateRetired})
	require.NoError(t, err)
	assertDenied(request)
	_, err = db.GetEngine(ctx).ID(scope.ID).Cols("state").Update(&branchroom_model.Scope{State: branchroom_model.ScopeStateActive})
	require.NoError(t, err)

	_, err = db.GetEngine(ctx).ID(workspace.ID).Cols("state").Update(&branchroom_model.Workspace{State: branchroom_model.WorkspaceStateProvisioning})
	require.NoError(t, err)
	assertDenied(request)
	_, err = db.GetEngine(ctx).ID(workspace.ID).Cols("state").Update(&branchroom_model.Workspace{State: branchroom_model.WorkspaceStateReady})
	require.NoError(t, err)

	_, err = db.GetEngine(ctx).ID(workspace.ID).Cols("repo_id").Update(&branchroom_model.Workspace{RepoID: scope.RepoID + 1})
	require.NoError(t, err)
	assertDenied(request)
	_, err = db.GetEngine(ctx).ID(workspace.ID).Cols("repo_id").Update(&branchroom_model.Workspace{RepoID: scope.RepoID})
	require.NoError(t, err)

	_, err = db.GetEngine(ctx).ID(workspace.ID).Cols("store_id").Update(&branchroom_model.Workspace{StoreID: "not-a-uuid"})
	require.NoError(t, err)
	assertDenied(request)
}

func workspaceRouteKeyRotationCounts(t *testing.T) (int64, int64) {
	t.Helper()
	decisions, err := db.GetEngine(t.Context()).Count(new(branchroom_model.Decision))
	require.NoError(t, err)
	audits, err := db.GetEngine(t.Context()).Count(new(branchroom_model.AuditEvent))
	require.NoError(t, err)
	return decisions, audits
}
