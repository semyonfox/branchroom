// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"strings"
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepositoryDiscoveryGrantManagement(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	owner := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}

	scope, err := CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  1,
		Actor:   owner,
		Name:    "direct-discovery-grants",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)

	grant, err := CreateRepositoryDiscoveryGrant(ctx, CreateRepositoryDiscoveryGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "user4",
		Surface:         branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.SubjectTypeUser, grant.SubjectType)
	assert.EqualValues(t, 4, grant.SubjectID)
	assert.Equal(t, branchroom_model.ActionDiscover, grant.Action)
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.AuditEvent{
		ScopeID:    scope.ID,
		GrantID:    grant.ID,
		ReasonCode: "discovery_grant_created",
	})

	decision, err := fixedEvaluator(timeutil.TimeStampNow()).Evaluate(ctx, testRequest(scope, Actor{Type: branchroom_model.ActorTypeUser, ID: 4}, true))
	require.NoError(t, err)
	assert.True(t, decision.Allowed)

	otherScope, err := CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  1,
		Actor:   owner,
		Name:    "other-direct-discovery-grants",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	err = RevokeRepositoryDiscoveryGrant(ctx, RevokeRepositoryDiscoveryGrantRequest{
		RepoID:  1,
		ScopeID: otherScope.ID,
		GrantID: grant.ID,
		Actor:   owner,
		Surface: branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)
	stored, err := branchroom_model.GetGrantByID(ctx, grant.ID)
	require.NoError(t, err)
	assert.Zero(t, stored.RevokedUnix)

	_, err = CreateRepositoryDiscoveryGrant(ctx, CreateRepositoryDiscoveryGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "user4",
		Surface:         branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, ErrDiscoveryGrantAlreadyExists)

	_, err = CreateRepositoryDiscoveryGrant(ctx, CreateRepositoryDiscoveryGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "org3",
		Surface:         branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, ErrInvalidDiscoveryGrantTarget)

	require.NoError(t, RevokeRepositoryDiscoveryGrant(ctx, RevokeRepositoryDiscoveryGrantRequest{
		RepoID:  1,
		ScopeID: scope.ID,
		GrantID: grant.ID,
		Actor:   owner,
		Surface: branchroom_model.SurfaceSystem,
	}))
	stored, err = branchroom_model.GetGrantByID(ctx, grant.ID)
	require.NoError(t, err)
	assert.NotZero(t, stored.RevokedUnix)
	assert.Equal(t, owner.ID, stored.RevokedByID)
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.AuditEvent{
		ScopeID:    scope.ID,
		GrantID:    grant.ID,
		ReasonCode: "discovery_grant_revoked",
	})

	decision, err = fixedEvaluator(timeutil.TimeStampNow()).Evaluate(ctx, testRequest(scope, Actor{Type: branchroom_model.ActorTypeUser, ID: 4}, true))
	require.NoError(t, err)
	assert.False(t, decision.Allowed)

	_, err = CreateRepositoryDiscoveryGrant(ctx, CreateRepositoryDiscoveryGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "user4",
		Surface:         branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)

	_, err = CreateRepositoryDiscoveryGrant(ctx, CreateRepositoryDiscoveryGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           Actor{Type: branchroom_model.ActorTypeUser, ID: 1},
		SubjectUsername: "user5",
		Surface:         branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)
}

func TestRepositoryDiscoveryGrantRejectsIneligibleTargetsWithoutAudit(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	owner := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}
	scope, err := CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  1,
		Actor:   owner,
		Name:    "ineligible-direct-discovery-grants",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)

	countAuditEvents := func() int64 {
		count, err := db.GetEngine(ctx).Where("scope_id = ?", scope.ID).Count(new(branchroom_model.AuditEvent))
		require.NoError(t, err)
		return count
	}
	initialAuditCount := countAuditEvents()

	user, err := user_model.GetUserByName(ctx, "user4")
	require.NoError(t, err)
	_, err = db.GetEngine(ctx).ID(user.ID).Cols("prohibit_login").Update(&user_model.User{ProhibitLogin: true})
	require.NoError(t, err)
	_, err = CreateRepositoryDiscoveryGrant(ctx, CreateRepositoryDiscoveryGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "user4",
		Surface:         branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, ErrInvalidDiscoveryGrantTarget)

	_, err = db.GetEngine(ctx).ID(user.ID).Cols("prohibit_login", "type").Update(&user_model.User{ProhibitLogin: false, Type: user_model.UserTypeBot})
	require.NoError(t, err)
	_, err = CreateRepositoryDiscoveryGrant(ctx, CreateRepositoryDiscoveryGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "user4",
		Surface:         branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, ErrInvalidDiscoveryGrantTarget)

	_, err = CreateRepositoryDiscoveryGrant(ctx, CreateRepositoryDiscoveryGrantRequest{
		RepoID:          1,
		ScopeID:         9999,
		Actor:           owner,
		SubjectUsername: "unknown-user",
		Surface:         branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)
	_, err = db.GetEngine(ctx).ID(scope.ID).Cols("state").Update(&branchroom_model.Scope{State: branchroom_model.ScopeStateRetired})
	require.NoError(t, err)
	_, err = CreateRepositoryDiscoveryGrant(ctx, CreateRepositoryDiscoveryGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "unknown-user",
		Surface:         branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)
	assert.Equal(t, initialAuditCount, countAuditEvents())
}

func TestRepositoryDiscoveryGrantAllowsOrganizationTeamAdmin(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	admin := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}
	scope, err := CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  3,
		Actor:   admin,
		Name:    "organization-direct-discovery-grants",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)

	grant, err := CreateRepositoryDiscoveryGrant(ctx, CreateRepositoryDiscoveryGrantRequest{
		RepoID:          3,
		ScopeID:         scope.ID,
		Actor:           admin,
		SubjectUsername: "user4",
		Surface:         branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 4, grant.SubjectID)
}

func TestRepositoryDiscoveryGrantRejectsEnvironmentScopes(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	owner := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}
	scope := insertTestEnvironmentScope(t, 1, "hidden-environment-scope")
	grant := &branchroom_model.Grant{
		ScopeID:     scope.ID,
		SubjectType: branchroom_model.SubjectTypeUser,
		SubjectID:   4,
		Action:      branchroom_model.ActionDiscover,
		CreatedByID: owner.ID,
	}
	require.NoError(t, branchroom_model.InsertGrant(ctx, grant))

	_, err := CreateRepositoryDiscoveryGrant(ctx, CreateRepositoryDiscoveryGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "user5",
		Surface:         branchroom_model.SurfaceWeb,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)
	err = RevokeRepositoryDiscoveryGrant(ctx, RevokeRepositoryDiscoveryGrantRequest{
		RepoID:  1,
		ScopeID: scope.ID,
		GrantID: grant.ID,
		Actor:   owner,
		Surface: branchroom_model.SurfaceWeb,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)
	stored, err := branchroom_model.GetGrantByID(ctx, grant.ID)
	require.NoError(t, err)
	assert.Zero(t, stored.RevokedUnix)
}

func TestRepositorySourceReadGrantManagement(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	owner := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}

	scope, err := CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  1,
		Actor:   owner,
		Name:    "direct-source-read-grants",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)

	_, err = CreateRepositorySourceReadGrant(ctx, CreateRepositorySourceReadGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "user4",
		Surface:         branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)

	workspace := insertReadyWorkspaceForGrant(t, scope, owner.ID)
	grant, err := CreateRepositorySourceReadGrant(ctx, CreateRepositorySourceReadGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "user4",
		Surface:         branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.SubjectTypeUser, grant.SubjectType)
	assert.EqualValues(t, 4, grant.SubjectID)
	assert.Equal(t, branchroom_model.ActionReadSource, grant.Action)
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.AuditEvent{
		ScopeID:    scope.ID,
		GrantID:    grant.ID,
		ReasonCode: "source_read_grant_created",
	})

	decision, err := fixedEvaluator(timeutil.TimeStampNow()).Evaluate(ctx, Request{
		ScopeID:      scope.ID,
		RepoID:       scope.RepoID,
		Actor:        Actor{Type: branchroom_model.ActorTypeUser, ID: 4},
		Ceiling:      RepositoryCeiling{CodeRead: true},
		Action:       branchroom_model.ActionReadSource,
		ResourceType: branchroom_model.ResourceTypeWorkspace,
		ResourceID:   workspaceResourceID(workspace.WorkspaceKey),
		Surface:      branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.True(t, decision.Allowed)

	_, err = CreateRepositorySourceReadGrant(ctx, CreateRepositorySourceReadGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "user4",
		Surface:         branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, ErrSourceReadGrantAlreadyExists)

	err = RevokeRepositoryDiscoveryGrant(ctx, RevokeRepositoryDiscoveryGrantRequest{
		RepoID:  1,
		ScopeID: scope.ID,
		GrantID: grant.ID,
		Actor:   owner,
		Surface: branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)

	require.NoError(t, RevokeRepositorySourceReadGrant(ctx, RevokeRepositorySourceReadGrantRequest{
		RepoID:  1,
		ScopeID: scope.ID,
		GrantID: grant.ID,
		Actor:   owner,
		Surface: branchroom_model.SurfaceSystem,
	}))
	stored, err := branchroom_model.GetGrantByID(ctx, grant.ID)
	require.NoError(t, err)
	assert.NotZero(t, stored.RevokedUnix)
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.AuditEvent{
		ScopeID:    scope.ID,
		GrantID:    grant.ID,
		ReasonCode: "source_read_grant_revoked",
	})

	_, err = CreateRepositorySourceReadGrant(ctx, CreateRepositorySourceReadGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           Actor{Type: branchroom_model.ActorTypeUser, ID: 1},
		SubjectUsername: "user5",
		Surface:         branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)
}

func TestRepositorySourceReadGrantRejectsUnsafeTargetsWithoutAudit(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	owner := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}
	scope, err := CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  1,
		Actor:   owner,
		Name:    "source-read-rejections",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	workspace := insertReadyWorkspaceForGrant(t, scope, owner.ID)

	counts := func() (int64, int64) {
		grants, err := db.GetEngine(ctx).Where("scope_id = ? AND action = ?", scope.ID, branchroom_model.ActionReadSource).Count(new(branchroom_model.Grant))
		require.NoError(t, err)
		audits, err := db.GetEngine(ctx).Where("scope_id = ?", scope.ID).Count(new(branchroom_model.AuditEvent))
		require.NoError(t, err)
		return grants, audits
	}
	initialGrants, initialAudits := counts()
	assertUnchanged := func() {
		t.Helper()
		grants, audits := counts()
		assert.Equal(t, initialGrants, grants)
		assert.Equal(t, initialAudits, audits)
	}
	request := func(repoID, scopeID int64, username string) CreateRepositorySourceReadGrantRequest {
		return CreateRepositorySourceReadGrantRequest{
			RepoID:          repoID,
			ScopeID:         scopeID,
			Actor:           owner,
			SubjectUsername: username,
			Surface:         branchroom_model.SurfaceSystem,
		}
	}
	assertNotFound := func(request CreateRepositorySourceReadGrantRequest) {
		t.Helper()
		_, err := CreateRepositorySourceReadGrant(ctx, request)
		assert.ErrorIs(t, err, util.ErrNotExist)
		assertUnchanged()
	}

	setting.Branchroom.Enabled = false
	assertNotFound(request(1, scope.ID, "user4"))
	setting.Branchroom.Enabled = true

	assertNotFound(request(2, scope.ID, "user4"))
	environmentScope := insertTestEnvironmentScope(t, 1, "source-read-environment")
	assertNotFound(request(1, environmentScope.ID, "user4"))

	_, err = db.GetEngine(ctx).ID(scope.ID).Cols("state").Update(&branchroom_model.Scope{State: branchroom_model.ScopeStateDisabled})
	require.NoError(t, err)
	assertNotFound(request(1, scope.ID, "user4"))
	_, err = db.GetEngine(ctx).ID(scope.ID).Cols("state").Update(&branchroom_model.Scope{State: branchroom_model.ScopeStateRetired})
	require.NoError(t, err)
	assertNotFound(request(1, scope.ID, "user4"))
	_, err = db.GetEngine(ctx).ID(scope.ID).Cols("state").Update(&branchroom_model.Scope{State: branchroom_model.ScopeStateActive})
	require.NoError(t, err)

	for _, state := range []struct {
		state       branchroom_model.WorkspaceState
		failureCode string
		retiredUnix timeutil.TimeStamp
	}{
		{state: branchroom_model.WorkspaceStateProvisioning},
		{state: branchroom_model.WorkspaceStateFailed, failureCode: "workspace_failed"},
		{state: branchroom_model.WorkspaceStateRetired, retiredUnix: timeutil.TimeStampNow()},
	} {
		_, err = db.GetEngine(ctx).ID(workspace.ID).Cols("state", "failure_code", "retired_unix").Update(&branchroom_model.Workspace{
			State:       state.state,
			FailureCode: state.failureCode,
			RetiredUnix: state.retiredUnix,
		})
		require.NoError(t, err)
		assertNotFound(request(1, scope.ID, "user4"))
	}
	_, err = db.GetEngine(ctx).ID(workspace.ID).Cols("state", "failure_code", "retired_unix").Update(&branchroom_model.Workspace{State: branchroom_model.WorkspaceStateReady})
	require.NoError(t, err)
	_, err = db.GetEngine(ctx).ID(workspace.ID).Cols("store_id").Update(&branchroom_model.Workspace{StoreID: "not-a-uuid"})
	require.NoError(t, err)
	assertNotFound(request(1, scope.ID, "user4"))
	_, err = db.GetEngine(ctx).ID(workspace.ID).Cols("store_id").Update(&branchroom_model.Workspace{StoreID: workspace.StoreID})
	require.NoError(t, err)

	for _, username := range []string{"missing-user", "org3"} {
		_, err := CreateRepositorySourceReadGrant(ctx, request(1, scope.ID, username))
		assert.ErrorIs(t, err, ErrInvalidSourceReadGrantTarget)
		assertUnchanged()
	}
	user, err := user_model.GetUserByName(ctx, "user4")
	require.NoError(t, err)
	_, err = db.GetEngine(ctx).ID(user.ID).Cols("prohibit_login").Update(&user_model.User{ProhibitLogin: true})
	require.NoError(t, err)
	_, err = CreateRepositorySourceReadGrant(ctx, request(1, scope.ID, "user4"))
	assert.ErrorIs(t, err, ErrInvalidSourceReadGrantTarget)
	assertUnchanged()
	_, err = db.GetEngine(ctx).ID(user.ID).Cols("prohibit_login", "is_active", "type").Update(&user_model.User{ProhibitLogin: false, IsActive: false, Type: user_model.UserTypeIndividual})
	require.NoError(t, err)
	_, err = CreateRepositorySourceReadGrant(ctx, request(1, scope.ID, "user4"))
	assert.ErrorIs(t, err, ErrInvalidSourceReadGrantTarget)
	assertUnchanged()
	_, err = db.GetEngine(ctx).ID(user.ID).Cols("is_active", "type").Update(&user_model.User{IsActive: true, Type: user_model.UserTypeBot})
	require.NoError(t, err)
	_, err = CreateRepositorySourceReadGrant(ctx, request(1, scope.ID, "user4"))
	assert.ErrorIs(t, err, ErrInvalidSourceReadGrantTarget)
	assertUnchanged()
}

func TestRepositorySourceReadGrantRevocationRemainsAvailableForDisabledTargets(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	owner := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}
	scope, err := CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  1,
		Actor:   owner,
		Name:    "source-read-revocation",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	workspace := insertReadyWorkspaceForGrant(t, scope, owner.ID)
	createSourceGrant := func(username string) *branchroom_model.Grant {
		t.Helper()
		grant, err := CreateRepositorySourceReadGrant(ctx, CreateRepositorySourceReadGrantRequest{
			RepoID:          1,
			ScopeID:         scope.ID,
			Actor:           owner,
			SubjectUsername: username,
			Surface:         branchroom_model.SurfaceSystem,
		})
		require.NoError(t, err)
		return grant
	}

	discoveryGrant, err := CreateRepositoryDiscoveryGrant(ctx, CreateRepositoryDiscoveryGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "user5",
		Surface:         branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	err = RevokeRepositorySourceReadGrant(ctx, RevokeRepositorySourceReadGrantRequest{
		RepoID:  1,
		ScopeID: scope.ID,
		GrantID: discoveryGrant.ID,
		Actor:   owner,
		Surface: branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)

	grant := createSourceGrant("user4")
	otherScope, err := CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  1,
		Actor:   owner,
		Name:    "other-source-read-revocation",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	err = RevokeRepositorySourceReadGrant(ctx, RevokeRepositorySourceReadGrantRequest{
		RepoID:  1,
		ScopeID: otherScope.ID,
		GrantID: grant.ID,
		Actor:   owner,
		Surface: branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)
	err = RevokeRepositorySourceReadGrant(ctx, RevokeRepositorySourceReadGrantRequest{
		RepoID:  2,
		ScopeID: scope.ID,
		GrantID: grant.ID,
		Actor:   owner,
		Surface: branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)
	stored, err := branchroom_model.GetGrantByID(ctx, grant.ID)
	require.NoError(t, err)
	assert.Zero(t, stored.RevokedUnix)

	_, err = db.GetEngine(ctx).ID(scope.ID).Cols("state").Update(&branchroom_model.Scope{State: branchroom_model.ScopeStateDisabled})
	require.NoError(t, err)
	require.NoError(t, RevokeRepositorySourceReadGrant(ctx, RevokeRepositorySourceReadGrantRequest{
		RepoID:  1,
		ScopeID: scope.ID,
		GrantID: grant.ID,
		Actor:   owner,
		Surface: branchroom_model.SurfaceSystem,
	}))

	_, err = db.GetEngine(ctx).ID(scope.ID).Cols("state").Update(&branchroom_model.Scope{State: branchroom_model.ScopeStateActive})
	require.NoError(t, err)
	grant = createSourceGrant("user4")
	_, err = db.GetEngine(ctx).ID(workspace.ID).Cols("state", "retired_unix").Update(&branchroom_model.Workspace{
		State:       branchroom_model.WorkspaceStateRetired,
		RetiredUnix: timeutil.TimeStampNow(),
	})
	require.NoError(t, err)
	require.NoError(t, RevokeRepositorySourceReadGrant(ctx, RevokeRepositorySourceReadGrantRequest{
		RepoID:  1,
		ScopeID: scope.ID,
		GrantID: grant.ID,
		Actor:   owner,
		Surface: branchroom_model.SurfaceSystem,
	}))
	err = RevokeRepositorySourceReadGrant(ctx, RevokeRepositorySourceReadGrantRequest{
		RepoID:  1,
		ScopeID: scope.ID,
		GrantID: grant.ID,
		Actor:   owner,
		Surface: branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)
}

func insertReadyWorkspaceForGrant(t *testing.T, scope *branchroom_model.Scope, ownerID int64) *branchroom_model.Workspace {
	t.Helper()
	workspace := &branchroom_model.Workspace{
		WorkspaceKey:     uuid.NewString(),
		ScopeID:          scope.ID,
		RepoID:           scope.RepoID,
		StoreID:          uuid.NewString(),
		PublicBaseSHA:    strings.Repeat("a", 40),
		ObjectFormatName: "sha1",
		State:            branchroom_model.WorkspaceStateReady,
		CreatedByID:      ownerID,
	}
	require.NoError(t, branchroom_model.InsertWorkspace(t.Context(), workspace))
	return workspace
}
