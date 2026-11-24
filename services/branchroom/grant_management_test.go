// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"strings"
	"testing"
	"time"

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

func TestRepositorySourceWriteGrantManagement(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	owner := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}

	scope, err := CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  1,
		Actor:   owner,
		Name:    "direct-source-write-grants",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)

	_, err = CreateRepositorySourceWriteGrant(ctx, CreateRepositorySourceWriteGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "user4",
		Surface:         branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)

	workspace := insertReadyWorkspaceForGrant(t, scope, owner.ID)
	grant, err := CreateRepositorySourceWriteGrant(ctx, CreateRepositorySourceWriteGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "user4",
		Surface:         branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.SubjectTypeUser, grant.SubjectType)
	assert.EqualValues(t, 4, grant.SubjectID)
	assert.Equal(t, branchroom_model.ActionWriteSource, grant.Action)
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.AuditEvent{
		ScopeID:    scope.ID,
		GrantID:    grant.ID,
		ReasonCode: "source_write_grant_created",
	})

	decision, err := fixedEvaluator(timeutil.TimeStampNow()).Evaluate(ctx, Request{
		ScopeID:      scope.ID,
		RepoID:       scope.RepoID,
		Actor:        Actor{Type: branchroom_model.ActorTypeUser, ID: 4},
		Ceiling:      RepositoryCeiling{CodeRead: true, CodeWrite: true},
		Action:       branchroom_model.ActionWriteSource,
		ResourceType: branchroom_model.ResourceTypeWorkspace,
		ResourceID:   workspaceResourceID(workspace.WorkspaceKey),
		Surface:      branchroom_model.SurfaceGitHTTP,
	})
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	assert.Equal(t, ReasonWriteSourceUnavailable, decision.ReasonCode)
	assert.Zero(t, decision.GrantID)

	_, err = CreateRepositorySourceWriteGrant(ctx, CreateRepositorySourceWriteGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "user4",
		Surface:         branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, ErrSourceWriteGrantAlreadyExists)

	_, err = CreateRepositorySourceWriteGrant(ctx, CreateRepositorySourceWriteGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "org3",
		Surface:         branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, ErrInvalidSourceWriteGrantTarget)

	err = RevokeRepositorySourceReadGrant(ctx, RevokeRepositorySourceReadGrantRequest{
		RepoID:  1,
		ScopeID: scope.ID,
		GrantID: grant.ID,
		Actor:   owner,
		Surface: branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)

	err = RevokeRepositorySourceWriteGrant(ctx, RevokeRepositorySourceWriteGrantRequest{
		RepoID:  1,
		ScopeID: scope.ID,
		GrantID: grant.ID,
		Actor:   Actor{Type: branchroom_model.ActorTypeUser, ID: 1},
		Surface: branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)
	stored, err := branchroom_model.GetGrantByID(ctx, grant.ID)
	require.NoError(t, err)
	assert.Zero(t, stored.RevokedUnix)

	require.NoError(t, RevokeRepositorySourceWriteGrant(ctx, RevokeRepositorySourceWriteGrantRequest{
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
		ReasonCode: "source_write_grant_revoked",
	})
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

func TestRepositoryTeamGrantManagement(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	admin := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}

	scope, err := CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  3,
		Actor:   admin,
		Name:    "team-grants",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)

	windowStart := timeutil.TimeStampNow().AddDuration(time.Minute)
	windowEnd := timeutil.TimeStampNow().AddDuration(2 * time.Minute)
	grant, err := CreateRepositoryTeamGrant(ctx, CreateRepositoryTeamGrantRequest{
		RepoID:        3,
		ScopeID:       scope.ID,
		Actor:         admin,
		TeamID:        2,
		Action:        branchroom_model.ActionDiscover,
		NotBeforeUnix: windowStart,
		ExpiresUnix:   windowEnd,
		Surface:       branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.SubjectTypeTeam, grant.SubjectType)
	assert.EqualValues(t, 2, grant.SubjectID)
	assert.Equal(t, branchroom_model.ActionDiscover, grant.Action)
	assert.Equal(t, windowStart, grant.NotBeforeUnix)
	assert.Equal(t, windowEnd, grant.ExpiresUnix)
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.AuditEvent{
		ScopeID:    scope.ID,
		GrantID:    grant.ID,
		ReasonCode: reasonTeamGrantCreated,
	})

	_, err = CreateRepositoryTeamGrant(ctx, CreateRepositoryTeamGrantRequest{
		RepoID:  3,
		ScopeID: scope.ID,
		Actor:   admin,
		TeamID:  2,
		Action:  branchroom_model.ActionDiscover,
		Surface: branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, ErrTeamGrantAlreadyExists)

	decision, err := fixedEvaluator(windowStart).Evaluate(ctx, testRequest(scope, Actor{Type: branchroom_model.ActorTypeUser, ID: 4}, true))
	require.NoError(t, err)
	assert.True(t, decision.Allowed)
	decision, err = fixedEvaluator(windowEnd).Evaluate(ctx, testRequest(scope, Actor{Type: branchroom_model.ActorTypeUser, ID: 4}, true))
	require.NoError(t, err)
	assert.False(t, decision.Allowed)
	decision, err = fixedEvaluator(windowStart).Evaluate(ctx, testRequest(scope, Actor{Type: branchroom_model.ActorTypeUser, ID: 5}, true))
	require.NoError(t, err)
	assert.False(t, decision.Allowed)

	err = RevokeRepositoryTeamGrant(ctx, RevokeRepositoryTeamGrantRequest{
		RepoID:  3,
		ScopeID: scope.ID,
		GrantID: grant.ID,
		Actor:   admin,
		Action:  branchroom_model.ActionReadSource,
		Surface: branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)
	stored, err := branchroom_model.GetGrantByID(ctx, grant.ID)
	require.NoError(t, err)
	assert.Zero(t, stored.RevokedUnix)

	require.NoError(t, RevokeRepositoryTeamGrant(ctx, RevokeRepositoryTeamGrantRequest{
		RepoID:  3,
		ScopeID: scope.ID,
		GrantID: grant.ID,
		Actor:   admin,
		Action:  branchroom_model.ActionDiscover,
		Surface: branchroom_model.SurfaceSystem,
	}))
	stored, err = branchroom_model.GetGrantByID(ctx, grant.ID)
	require.NoError(t, err)
	assert.NotZero(t, stored.RevokedUnix)
	assert.Equal(t, admin.ID, stored.RevokedByID)
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.AuditEvent{
		ScopeID:    scope.ID,
		GrantID:    grant.ID,
		ReasonCode: reasonTeamGrantRevoked,
	})
	decision, err = fixedEvaluator(windowStart).Evaluate(ctx, testRequest(scope, Actor{Type: branchroom_model.ActorTypeUser, ID: 4}, true))
	require.NoError(t, err)
	assert.False(t, decision.Allowed)

	insertReadyWorkspaceForGrant(t, scope, admin.ID)
	readSourceGrant, err := CreateRepositoryTeamGrant(ctx, CreateRepositoryTeamGrantRequest{
		RepoID:  3,
		ScopeID: scope.ID,
		Actor:   admin,
		TeamID:  1,
		Action:  branchroom_model.ActionReadSource,
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.EqualValues(t, 1, readSourceGrant.SubjectID)
}

func TestRepositoryDirectUserGrantsAcceptOptionalTimeWindow(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	owner := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}
	scope, err := CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  1,
		Actor:   owner,
		Name:    "windowed-direct-grants",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	insertReadyWorkspaceForGrant(t, scope, owner.ID)

	windowStart := timeutil.TimeStampNow().AddDuration(time.Minute)
	windowEnd := timeutil.TimeStampNow().AddDuration(2 * time.Minute)
	discoveryGrant, err := CreateRepositoryDiscoveryGrant(ctx, CreateRepositoryDiscoveryGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "user4",
		NotBeforeUnix:   windowStart,
		ExpiresUnix:     windowEnd,
		Surface:         branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.Equal(t, windowStart, discoveryGrant.NotBeforeUnix)
	assert.Equal(t, windowEnd, discoveryGrant.ExpiresUnix)

	sourceReadGrant, err := CreateRepositorySourceReadGrant(ctx, CreateRepositorySourceReadGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "user4",
		NotBeforeUnix:   windowStart,
		Surface:         branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.Equal(t, windowStart, sourceReadGrant.NotBeforeUnix)
	assert.InDelta(t, int64(timeutil.TimeStampNow().AddDuration(setting.Branchroom.GrantDuration())), int64(sourceReadGrant.ExpiresUnix), 2)

	sourceWriteGrant, err := CreateRepositorySourceWriteGrant(ctx, CreateRepositorySourceWriteGrantRequest{
		RepoID:          1,
		ScopeID:         scope.ID,
		Actor:           owner,
		SubjectUsername: "user4",
		ExpiresUnix:     windowEnd,
		Surface:         branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.Zero(t, sourceWriteGrant.NotBeforeUnix)
	assert.Equal(t, windowEnd, sourceWriteGrant.ExpiresUnix)

	expired := timeutil.TimeStampNow().AddDuration(-10 * time.Minute)
	for _, request := range []struct {
		create func() error
	}{
		{func() error {
			_, err := CreateRepositoryDiscoveryGrant(ctx, CreateRepositoryDiscoveryGrantRequest{
				RepoID:          1,
				ScopeID:         scope.ID,
				Actor:           owner,
				SubjectUsername: "user5",
				NotBeforeUnix:   expired,
				Surface:         branchroom_model.SurfaceSystem,
			})
			return err
		}},
		{func() error {
			_, err := CreateRepositorySourceReadGrant(ctx, CreateRepositorySourceReadGrantRequest{
				RepoID:          1,
				ScopeID:         scope.ID,
				Actor:           owner,
				SubjectUsername: "user5",
				NotBeforeUnix:   windowEnd,
				ExpiresUnix:     windowStart,
				Surface:         branchroom_model.SurfaceSystem,
			})
			return err
		}},
		{func() error {
			_, err := CreateRepositorySourceWriteGrant(ctx, CreateRepositorySourceWriteGrantRequest{
				RepoID:          1,
				ScopeID:         scope.ID,
				Actor:           owner,
				SubjectUsername: "user5",
				ExpiresUnix:     expired,
				Surface:         branchroom_model.SurfaceSystem,
			})
			return err
		}},
	} {
		err := request.create()
		assert.ErrorIs(t, err, util.ErrInvalidArgument)
		grants, err := db.GetEngine(ctx).Where("subject_id = ?", 5).Count(new(branchroom_model.Grant))
		require.NoError(t, err)
		assert.Zero(t, grants)
	}
}

func TestRepositoryTeamGrantRejectsInvalidRequests(t *testing.T) {
	enableBranchroom(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	admin := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}
	scope, err := CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  3,
		Actor:   admin,
		Name:    "team-grant-rejections",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	userScope, err := CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  1,
		Actor:   admin,
		Name:    "user-owned-team-grant-rejections",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)

	countGrantsAndAudits := func() (int64, int64) {
		grants, err := db.GetEngine(ctx).Count(new(branchroom_model.Grant))
		require.NoError(t, err)
		audits, err := db.GetEngine(ctx).Count(new(branchroom_model.AuditEvent))
		require.NoError(t, err)
		return grants, audits
	}
	initialGrants, initialAudits := countGrantsAndAudits()
	request := func(mutate func(*CreateRepositoryTeamGrantRequest)) CreateRepositoryTeamGrantRequest {
		request := CreateRepositoryTeamGrantRequest{
			RepoID:  3,
			ScopeID: scope.ID,
			Actor:   admin,
			TeamID:  2,
			Action:  branchroom_model.ActionDiscover,
			Surface: branchroom_model.SurfaceSystem,
		}
		if mutate != nil {
			mutate(&request)
		}
		return request
	}

	setting.Branchroom.Enabled = false
	_, err = CreateRepositoryTeamGrant(ctx, request(nil))
	assert.ErrorIs(t, err, util.ErrNotExist)
	setting.Branchroom.Enabled = true

	expectRejection := func(request CreateRepositoryTeamGrantRequest, expected error) {
		t.Helper()
		_, err := CreateRepositoryTeamGrant(ctx, request)
		assert.ErrorIs(t, err, expected)
		grants, audits := countGrantsAndAudits()
		assert.Equal(t, initialGrants, grants)
		assert.Equal(t, initialAudits, audits)
	}

	expectRejection(request(func(r *CreateRepositoryTeamGrantRequest) { r.TeamID = 0 }), util.ErrInvalidArgument)
	expectRejection(request(func(r *CreateRepositoryTeamGrantRequest) {
		r.Actor = Actor{Type: branchroom_model.ActorTypeDeployKey, ID: 5}
	}), util.ErrInvalidArgument)
	expectRejection(request(func(r *CreateRepositoryTeamGrantRequest) { r.Action = branchroom_model.ActionWriteSource }), util.ErrInvalidArgument)
	expectRejection(request(func(r *CreateRepositoryTeamGrantRequest) { r.Action = branchroom_model.ActionManagePolicy }), util.ErrInvalidArgument)

	now := timeutil.TimeStampNow()
	expectRejection(request(func(r *CreateRepositoryTeamGrantRequest) { r.NotBeforeUnix, r.ExpiresUnix = -1, 0 }), util.ErrInvalidArgument)
	expectRejection(request(func(r *CreateRepositoryTeamGrantRequest) { r.NotBeforeUnix, r.ExpiresUnix = 0, -1 }), util.ErrInvalidArgument)
	expectRejection(request(func(r *CreateRepositoryTeamGrantRequest) {
		r.NotBeforeUnix, r.ExpiresUnix = now.AddDuration(2*time.Minute), now.AddDuration(time.Minute)
	}), util.ErrInvalidArgument)
	expectRejection(request(func(r *CreateRepositoryTeamGrantRequest) { r.ExpiresUnix = now.AddDuration(-10 * time.Minute) }), util.ErrInvalidArgument)
	expectRejection(request(func(r *CreateRepositoryTeamGrantRequest) { r.NotBeforeUnix = now.AddDuration(-10 * time.Minute) }), util.ErrInvalidArgument)

	skewedPast := now.AddDuration(-30 * time.Second)
	withinSkew, err := CreateRepositoryTeamGrant(ctx, request(func(r *CreateRepositoryTeamGrantRequest) { r.NotBeforeUnix = skewedPast }))
	require.NoError(t, err)
	assert.Equal(t, skewedPast, withinSkew.NotBeforeUnix)
	initialGrants, initialAudits = countGrantsAndAudits()

	expectRejection(request(func(r *CreateRepositoryTeamGrantRequest) { r.TeamID = 9999 }), ErrInvalidTeamGrantTarget)
	expectRejection(request(func(r *CreateRepositoryTeamGrantRequest) { r.TeamID = 3 }), ErrInvalidTeamGrantTarget)
	expectRejection(request(func(r *CreateRepositoryTeamGrantRequest) { r.ScopeID, r.RepoID = userScope.ID, userScope.RepoID }), ErrInvalidTeamGrantTarget)
	expectRejection(request(func(r *CreateRepositoryTeamGrantRequest) {
		r.Actor = Actor{Type: branchroom_model.ActorTypeUser, ID: 1}
	}), util.ErrNotExist)
	expectRejection(request(func(r *CreateRepositoryTeamGrantRequest) { r.Action = branchroom_model.ActionReadSource }), util.ErrNotExist)

	err = RevokeRepositoryTeamGrant(ctx, RevokeRepositoryTeamGrantRequest{RepoID: 3, ScopeID: scope.ID, GrantID: 9999, Actor: admin, Action: branchroom_model.ActionDiscover, Surface: branchroom_model.SurfaceSystem})
	assert.ErrorIs(t, err, util.ErrNotExist)
	err = RevokeRepositoryTeamGrant(ctx, RevokeRepositoryTeamGrantRequest{RepoID: 3, ScopeID: scope.ID, GrantID: withinSkew.ID, Actor: admin, Action: branchroom_model.ActionWriteSource, Surface: branchroom_model.SurfaceSystem})
	assert.ErrorIs(t, err, util.ErrInvalidArgument)

	directUserGrant, err := CreateRepositoryDiscoveryGrant(ctx, CreateRepositoryDiscoveryGrantRequest{
		RepoID:          3,
		ScopeID:         scope.ID,
		Actor:           admin,
		SubjectUsername: "user4",
		Surface:         branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	err = RevokeRepositoryTeamGrant(ctx, RevokeRepositoryTeamGrantRequest{RepoID: 3, ScopeID: scope.ID, GrantID: directUserGrant.ID, Actor: admin, Action: branchroom_model.ActionDiscover, Surface: branchroom_model.SurfaceSystem})
	assert.ErrorIs(t, err, util.ErrNotExist)
	storedDirectUserGrant, err := branchroom_model.GetGrantByID(ctx, directUserGrant.ID)
	require.NoError(t, err)
	assert.Zero(t, storedDirectUserGrant.RevokedUnix)
}
