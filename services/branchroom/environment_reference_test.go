// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	"gitea.dev/models/unittest"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveEnvironmentReference(t *testing.T) {
	enableDevelopmentEnvironment(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	owner := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}
	scope, revision := createTestEnvironmentRevision(t, owner, "BRANCHROOM_TEST_SECRET=resolver")
	plaintext := []byte("BRANCHROOM_TEST_SECRET=resolver-newest")
	latestRevision, err := createEnvironmentRevision(ctx, createEnvironmentRevisionRequest{
		RepoID:    1,
		ScopeID:   scope.ID,
		Actor:     owner,
		Plaintext: plaintext,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), latestRevision.Revision)
	auditCount, err := db.GetEngine(ctx).Where("scope_id = ?", scope.ID).Count(new(branchroom_model.AuditEvent))
	require.NoError(t, err)
	decisionCount, err := db.GetEngine(ctx).Count(new(branchroom_model.Decision))
	require.NoError(t, err)
	leaseCount, err := db.GetEngine(ctx).Count(new(branchroom_model.EnvironmentLease))
	require.NoError(t, err)
	reference, err := branchroom_module.ParseEnvironmentReference([]byte("environment = \"environment-resolver\"\nrevision = 1\ndestination = \"private/.env\"\n"))
	require.NoError(t, err)

	resolved, err := resolveEnvironmentReference(ctx, resolveEnvironmentReferenceRequest{
		RepoID:    1,
		Reference: reference,
	})
	require.NoError(t, err)
	assert.Equal(t, environmentReferenceResolution{
		ScopeID:     scope.ID,
		RevisionKey: revision.RevisionKey,
	}, resolved)
	updatedAuditCount, err := db.GetEngine(ctx).Where("scope_id = ?", scope.ID).Count(new(branchroom_model.AuditEvent))
	require.NoError(t, err)
	assert.Equal(t, auditCount, updatedAuditCount)
	updatedDecisionCount, err := db.GetEngine(ctx).Count(new(branchroom_model.Decision))
	require.NoError(t, err)
	assert.Equal(t, decisionCount, updatedDecisionCount)
	updatedLeaseCount, err := db.GetEngine(ctx).Count(new(branchroom_model.EnvironmentLease))
	require.NoError(t, err)
	assert.Equal(t, leaseCount, updatedLeaseCount)
}

func TestResolveEnvironmentReferenceFailsClosed(t *testing.T) {
	enableDevelopmentEnvironment(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	owner := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}
	scope, _ := createTestEnvironmentRevision(t, owner, "BRANCHROOM_TEST_SECRET=resolver")

	resolve := func(repoID int64, contents string) error {
		reference, err := branchroom_module.ParseEnvironmentReference([]byte(contents))
		if err != nil {
			return err
		}
		_, err = resolveEnvironmentReference(ctx, resolveEnvironmentReferenceRequest{RepoID: repoID, Reference: reference})
		return err
	}

	assert.ErrorIs(t, resolve(1, "environment = \"unknown\"\nrevision = 1\ndestination = \"private/.env\"\n"), util.ErrNotExist)
	assert.ErrorIs(t, resolve(1, "environment = \"environment-resolver\"\nrevision = 2\ndestination = \"private/.env\"\n"), util.ErrNotExist)
	assert.ErrorIs(t, resolve(2, "environment = \"environment-resolver\"\nrevision = 1\ndestination = \"private/.env\"\n"), util.ErrNotExist)
	assert.ErrorIs(t, resolve(1, "environment = \"environment-resolver\"\nrevision = 1\ndestination = \"../.env\"\n"), branchroom_module.ErrInvalidEnvironmentReference)
	_, err := resolveEnvironmentReference(ctx, resolveEnvironmentReferenceRequest{
		RepoID:    1,
		Reference: &branchroom_module.EnvironmentReference{Environment: "environment-resolver", Revision: 1, Destination: "../.env"},
	})
	assert.ErrorIs(t, err, branchroom_module.ErrInvalidEnvironmentReference)

	ordinaryScope, err := CreateRepositoryScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  1,
		Actor:   owner,
		Name:    "ordinary-reference",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.NotEqual(t, scope.ID, ordinaryScope.ID)
	assert.ErrorIs(t, resolve(1, "environment = \"ordinary-reference\"\nrevision = 1\ndestination = \"private/.env\"\n"), util.ErrNotExist)

	disabledScope, _ := createTestEnvironmentRevision(t, owner, "BRANCHROOM_TEST_SECRET=disabled")
	_, err = db.GetEngine(ctx).ID(disabledScope.ID).Cols("state").Update(&branchroom_model.Scope{
		State: branchroom_model.ScopeStateDisabled,
	})
	require.NoError(t, err)
	assert.ErrorIs(t, resolve(1, "environment = \"environment-disabled\"\nrevision = 1\ndestination = \"private/.env\"\n"), util.ErrNotExist)

	require.NoError(t, db.WithTx(ctx, func(txCtx context.Context) error {
		return RetireRepositoryScopesForTransfer(txCtx, 1, 3)
	}))
	assert.ErrorIs(t, resolve(1, "environment = \"environment-resolver\"\nrevision = 1\ndestination = \"private/.env\"\n"), util.ErrNotExist)

	setting.BranchroomEnvironment.Enabled = false
	assert.ErrorIs(t, resolve(1, "environment = \"environment-resolver\"\nrevision = 1\ndestination = \"private/.env\"\n"), ErrEnvironmentUnavailable)
}
