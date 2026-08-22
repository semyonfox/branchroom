// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unittest"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/git/gitrepo"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthorizeWorkspaceReadRequiresDirectActiveGrant(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	ensureTestRepositoryCodeUnit(t.Context(), t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-read-grant")
	workspace, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       scope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)

	ctx := t.Context()
	insertWorkspaceActionGrant(t, scope.ID, repository.OwnerID, branchroom_model.ActionDiscover, 0, 0)
	assertWorkspaceReadDenied(ctx, t, authorizeWorkspaceReadRequest{
		RepoID:       repository.ID,
		WorkspaceKey: workspace.WorkspaceKey,
		UserID:       repository.OwnerID,
	})

	now := timeutil.TimeStampNow()
	insertWorkspaceActionGrant(t, scope.ID, repository.OwnerID, branchroom_model.ActionReadSource, now+60, 0)
	insertWorkspaceActionGrant(t, scope.ID, repository.OwnerID, branchroom_model.ActionReadSource, 0, now)
	assertWorkspaceReadDenied(ctx, t, authorizeWorkspaceReadRequest{
		RepoID:       repository.ID,
		WorkspaceKey: workspace.WorkspaceKey,
		UserID:       repository.OwnerID,
	})

	grant := insertWorkspaceActionGrant(t, scope.ID, repository.OwnerID, branchroom_model.ActionReadSource, 0, 0)
	restricted, err := authorizeWorkspaceRead(ctx, authorizeWorkspaceReadRequest{
		RepoID:       repository.ID,
		WorkspaceKey: workspace.WorkspaceKey,
		UserID:       repository.OwnerID,
	})
	require.NoError(t, err)
	expected, err := branchroom_module.RestrictedRepository(workspace.StoreID)
	require.NoError(t, err)
	assert.Equal(t, gitrepo.RepoLocalPath(expected), gitrepo.RepoLocalPath(restricted))

	decision := unittest.AssertExistsAndLoadBean(t, &branchroom_model.Decision{
		ScopeID:      scope.ID,
		Action:       branchroom_model.ActionReadSource,
		ResourceType: branchroom_model.ResourceTypeWorkspace,
		ResourceID:   workspaceResourceID(workspace.WorkspaceKey),
		Surface:      branchroom_model.SurfaceSystem,
		Result:       branchroom_model.DecisionResultAllow,
	})
	assert.NotContains(t, decision.ResourceID, workspace.StoreID)
	assert.NotContains(t, decision.ResourceID, gitrepo.RepoLocalPath(restricted))

	require.NoError(t, branchroom_model.RevokeGrant(ctx, grant.ID, repository.OwnerID, timeutil.TimeStampNow()))
	assertWorkspaceReadDenied(ctx, t, authorizeWorkspaceReadRequest{
		RepoID:       repository.ID,
		WorkspaceKey: workspace.WorkspaceKey,
		UserID:       repository.OwnerID,
	})
}

func TestAuthorizeWorkspaceGitHTTPReadUsesRouteKeyWithoutChangingRepositoryMetadata(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	ensureTestRepositoryCodeUnit(t.Context(), t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-git-http-read")
	workspace, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       scope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)

	request := AuthorizeWorkspaceGitHTTPReadRequest{
		RepoID:   repository.ID,
		RouteKey: workspace.RouteKey,
		UserID:   repository.OwnerID,
	}
	assertWorkspaceGitHTTPReadDenied(t.Context(), t, request)

	insertWorkspaceActionGrant(t, scope.ID, repository.OwnerID, branchroom_model.ActionReadSource, 0, 0)
	storedRepository, err := repo_model.GetRepositoryByID(t.Context(), repository.ID)
	require.NoError(t, err)
	updatedBefore := storedRepository.UpdatedUnix

	restricted, err := AuthorizeWorkspaceGitHTTPRead(t.Context(), request)
	require.NoError(t, err)
	expected, err := branchroom_module.RestrictedRepository(workspace.StoreID)
	require.NoError(t, err)
	assert.Equal(t, gitrepo.RepoLocalPath(expected), gitrepo.RepoLocalPath(restricted))
	storedRepository, err = repo_model.GetRepositoryByID(t.Context(), repository.ID)
	require.NoError(t, err)
	assert.Equal(t, updatedBefore, storedRepository.UpdatedUnix)

	decision := unittest.AssertExistsAndLoadBean(t, &branchroom_model.Decision{
		ScopeID:      scope.ID,
		Action:       branchroom_model.ActionReadSource,
		ResourceType: branchroom_model.ResourceTypeWorkspace,
		ResourceID:   workspaceResourceID(workspace.WorkspaceKey),
		Surface:      branchroom_model.SurfaceGitHTTP,
		Result:       branchroom_model.DecisionResultAllow,
	})
	assert.NotContains(t, decision.ResourceID, workspace.RouteKey)
	assert.NotContains(t, decision.ResourceID, workspace.StoreID)

	decisionsBefore, err := db.GetEngine(t.Context()).Where("scope_id = ?", scope.ID).Count(new(branchroom_model.Decision))
	require.NoError(t, err)
	request.RepoID = 2
	assertWorkspaceGitHTTPReadDenied(t.Context(), t, request)
	decisionsAfter, err := db.GetEngine(t.Context()).Where("scope_id = ?", scope.ID).Count(new(branchroom_model.Decision))
	require.NoError(t, err)
	assert.Equal(t, decisionsBefore, decisionsAfter)
	request.RepoID = repository.ID

	request.RouteKey = "aaaaaaaaaaaaaaaaaaaaaaaaaa"
	assertWorkspaceGitHTTPReadDenied(t.Context(), t, request)
	request.RouteKey = workspace.RouteKey

	grants, err := branchroom_model.FindActiveGrantsAt(t.Context(), scope.ID, branchroom_model.ActionReadSource, timeutil.TimeStampNow())
	require.NoError(t, err)
	require.Len(t, grants, 1)
	require.NoError(t, branchroom_model.RevokeGrant(t.Context(), grants[0].ID, repository.OwnerID, timeutil.TimeStampNow()))
	assertWorkspaceGitHTTPReadDenied(t.Context(), t, request)
}

func TestAuthorizeWorkspaceGitHTTPReadFailsClosedAfterTransfer(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	ensureTestRepositoryCodeUnit(t.Context(), t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-git-http-transfer")
	workspace, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       scope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	insertWorkspaceActionGrant(t, scope.ID, repository.OwnerID, branchroom_model.ActionReadSource, 0, 0)

	request := AuthorizeWorkspaceGitHTTPReadRequest{RepoID: repository.ID, RouteKey: workspace.RouteKey, UserID: repository.OwnerID}
	_, err = AuthorizeWorkspaceGitHTTPRead(t.Context(), request)
	require.NoError(t, err)
	require.NoError(t, db.WithTx(t.Context(), func(txCtx context.Context) error {
		return RetireRepositoryScopesForTransfer(txCtx, repository.ID, repository.OwnerID)
	}))
	assertWorkspaceGitHTTPReadDenied(t.Context(), t, request)
}

func TestAuthorizeWorkspaceReadFailsClosedForRepositoryAndLifecycle(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	ensureTestRepositoryCodeUnit(t.Context(), t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-read-lifecycle")
	workspace, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       scope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	insertWorkspaceActionGrant(t, scope.ID, repository.OwnerID, branchroom_model.ActionReadSource, 0, 0)
	ctx := t.Context()

	assertWorkspaceReadDenied(ctx, t, authorizeWorkspaceReadRequest{
		RepoID:       repository.ID + 1,
		WorkspaceKey: workspace.WorkspaceKey,
		UserID:       repository.OwnerID,
	})
	assertWorkspaceReadDenied(ctx, t, authorizeWorkspaceReadRequest{
		RepoID:       repository.ID,
		WorkspaceKey: uuid.NewString(),
		UserID:       repository.OwnerID,
	})
	assertWorkspaceReadDenied(ctx, t, authorizeWorkspaceReadRequest{
		RepoID:       repository.ID,
		WorkspaceKey: "not-a-workspace-key",
		UserID:       repository.OwnerID,
	})

	insertWorkspaceActionGrant(t, scope.ID, 4, branchroom_model.ActionReadSource, 0, 0)
	_, err = db.GetEngine(ctx).ID(repository.ID).Cols("is_private").Update(&repo_model.Repository{IsPrivate: true})
	require.NoError(t, err)
	assertWorkspaceReadDenied(ctx, t, authorizeWorkspaceReadRequest{
		RepoID:       repository.ID,
		WorkspaceKey: workspace.WorkspaceKey,
		UserID:       4,
	})
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.Decision{
		ScopeID:    scope.ID,
		ActorType:  branchroom_model.ActorTypeUser,
		ActorID:    4,
		Action:     branchroom_model.ActionReadSource,
		Result:     branchroom_model.DecisionResultDeny,
		ReasonCode: ReasonOrdinaryPermissionDeny,
	})

	updated, err := branchroom_model.TransitionWorkspace(ctx, workspace.ID, branchroom_model.WorkspaceStateReady, branchroom_model.WorkspaceStateRetired, "", timeutil.TimeStampNow())
	require.NoError(t, err)
	require.True(t, updated)
	assertWorkspaceReadDenied(ctx, t, authorizeWorkspaceReadRequest{
		RepoID:       repository.ID,
		WorkspaceKey: workspace.WorkspaceKey,
		UserID:       repository.OwnerID,
	})
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.Decision{
		ScopeID:    scope.ID,
		ActorType:  branchroom_model.ActorTypeUser,
		ActorID:    repository.OwnerID,
		Action:     branchroom_model.ActionReadSource,
		Result:     branchroom_model.DecisionResultDeny,
		ReasonCode: ReasonWorkspaceUnavailable,
	})

	transferScope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-read-transfer")
	transferWorkspace, err := CreateRepositoryWorkspace(ctx, CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       transferScope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	insertWorkspaceActionGrant(t, transferScope.ID, repository.OwnerID, branchroom_model.ActionReadSource, 0, 0)

	require.NoError(t, db.WithTx(ctx, func(txCtx context.Context) error {
		return RetireRepositoryScopesForTransfer(txCtx, repository.ID, repository.OwnerID)
	}))
	storedTransferScope, err := branchroom_model.GetScopeByID(ctx, transferScope.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ScopeStateRetired, storedTransferScope.State)
	storedTransferWorkspace, err := branchroom_model.GetWorkspaceByID(ctx, transferWorkspace.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.WorkspaceStateRetired, storedTransferWorkspace.State)
	assertWorkspaceReadDenied(ctx, t, authorizeWorkspaceReadRequest{
		RepoID:       repository.ID,
		WorkspaceKey: transferWorkspace.WorkspaceKey,
		UserID:       repository.OwnerID,
	})
}

func TestAuthorizeWorkspaceReadRejectsInactiveScopesAndNonreadyMappings(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	ensureTestRepositoryCodeUnit(t.Context(), t)
	ctx := t.Context()
	now := timeutil.TimeStampNow()

	for _, testCase := range []struct {
		name       string
		kind       branchroom_model.ScopeKind
		scopeState branchroom_model.ScopeState
		state      branchroom_model.WorkspaceState
		failure    string
		retired    timeutil.TimeStamp
		reasonCode string
	}{
		{
			name:       "disabled repository scope",
			kind:       branchroom_model.ScopeKindRepository,
			scopeState: branchroom_model.ScopeStateDisabled,
			state:      branchroom_model.WorkspaceStateReady,
			reasonCode: ReasonScopeInactive,
		},
		{
			name:       "environment scope",
			kind:       branchroom_model.ScopeKindEnvironment,
			scopeState: branchroom_model.ScopeStateActive,
			state:      branchroom_model.WorkspaceStateReady,
			reasonCode: ReasonResourceMismatch,
		},
		{
			name:       "provisioning workspace",
			kind:       branchroom_model.ScopeKindRepository,
			scopeState: branchroom_model.ScopeStateActive,
			state:      branchroom_model.WorkspaceStateProvisioning,
			reasonCode: ReasonWorkspaceUnavailable,
		},
		{
			name:       "failed workspace",
			kind:       branchroom_model.ScopeKindRepository,
			scopeState: branchroom_model.ScopeStateActive,
			state:      branchroom_model.WorkspaceStateFailed,
			failure:    "import_failed",
			reasonCode: ReasonWorkspaceUnavailable,
		},
		{
			name:       "retired workspace",
			kind:       branchroom_model.ScopeKindRepository,
			scopeState: branchroom_model.ScopeStateActive,
			state:      branchroom_model.WorkspaceStateRetired,
			retired:    now,
			reasonCode: ReasonWorkspaceUnavailable,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			scope := &branchroom_model.Scope{
				Kind:        testCase.kind,
				RepoID:      repository.ID,
				Name:        "workspace-read-" + testCase.name,
				State:       testCase.scopeState,
				OwnerType:   branchroom_model.ScopeOwnerTypeUser,
				OwnerID:     repository.OwnerID,
				CreatedByID: repository.OwnerID,
			}
			require.NoError(t, branchroom_model.InsertScope(ctx, scope))
			workspace := &branchroom_model.Workspace{
				WorkspaceKey:     uuid.NewString(),
				ScopeID:          scope.ID,
				RepoID:           repository.ID,
				StoreID:          uuid.NewString(),
				PublicBaseSHA:    baseSHA,
				ObjectFormatName: repository.ObjectFormatName,
				State:            testCase.state,
				FailureCode:      testCase.failure,
				CreatedByID:      repository.OwnerID,
				RetiredUnix:      testCase.retired,
			}
			require.NoError(t, branchroom_model.InsertWorkspace(ctx, workspace))
			insertWorkspaceActionGrant(t, scope.ID, repository.OwnerID, branchroom_model.ActionReadSource, 0, 0)

			assertWorkspaceReadDenied(ctx, t, authorizeWorkspaceReadRequest{
				RepoID:       repository.ID,
				WorkspaceKey: workspace.WorkspaceKey,
				UserID:       repository.OwnerID,
			})
			unittest.AssertExistsAndLoadBean(t, &branchroom_model.Decision{
				ScopeID:    scope.ID,
				Action:     branchroom_model.ActionReadSource,
				Result:     branchroom_model.DecisionResultDeny,
				ReasonCode: testCase.reasonCode,
			})
		})
	}
}

func TestAuthorizeWorkspaceReadRejectsAmbientTransaction(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	ensureTestRepositoryCodeUnit(t.Context(), t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-read-transaction")
	workspace, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       scope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	insertWorkspaceActionGrant(t, scope.ID, repository.OwnerID, branchroom_model.ActionReadSource, 0, 0)
	var decisionsBefore int64
	decisionsBefore, err = db.GetEngine(t.Context()).Count(new(branchroom_model.Decision))
	require.NoError(t, err)

	require.NoError(t, db.WithTx(t.Context(), func(txCtx context.Context) error {
		assertWorkspaceReadDenied(txCtx, t, authorizeWorkspaceReadRequest{
			RepoID:       repository.ID,
			WorkspaceKey: workspace.WorkspaceKey,
			UserID:       repository.OwnerID,
		})
		return nil
	}))
	decisionsAfter, err := db.GetEngine(t.Context()).Count(new(branchroom_model.Decision))
	require.NoError(t, err)
	assert.Equal(t, decisionsBefore, decisionsAfter)
}

func insertWorkspaceActionGrant(t *testing.T, scopeID, userID int64, action branchroom_model.Action, notBefore, expires timeutil.TimeStamp) *branchroom_model.Grant {
	t.Helper()
	grant := &branchroom_model.Grant{
		ScopeID:       scopeID,
		SubjectType:   branchroom_model.SubjectTypeUser,
		SubjectID:     userID,
		Action:        action,
		NotBeforeUnix: notBefore,
		ExpiresUnix:   expires,
		CreatedByID:   1,
	}
	require.NoError(t, branchroom_model.InsertGrant(t.Context(), grant))
	return grant
}

func assertWorkspaceReadDenied(ctx context.Context, t *testing.T, request authorizeWorkspaceReadRequest) {
	t.Helper()
	repository, err := authorizeWorkspaceRead(ctx, request)
	assert.Nil(t, repository)
	assert.ErrorIs(t, err, util.ErrNotExist)
	assert.Equal(t, "Branchroom workspace does not exist", err.Error())
}

func assertWorkspaceGitHTTPReadDenied(ctx context.Context, t *testing.T, request AuthorizeWorkspaceGitHTTPReadRequest) {
	t.Helper()
	repository, err := AuthorizeWorkspaceGitHTTPRead(ctx, request)
	assert.Nil(t, repository)
	assert.ErrorIs(t, err, util.ErrNotExist)
	assert.Equal(t, "Branchroom workspace does not exist", err.Error())
}
