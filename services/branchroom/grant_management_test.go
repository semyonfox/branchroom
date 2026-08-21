// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

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
