// Copyright 2017 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository_test

import (
	"context"
	"strings"
	"testing"

	actions_model "gitea.dev/models/actions"
	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	git_model "gitea.dev/models/git"
	"gitea.dev/models/organization"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/timeutil"
	branchroom_service "gitea.dev/services/branchroom"
	repo_service "gitea.dev/services/repository"

	"github.com/stretchr/testify/assert"
)

func TestTeam_HasRepository(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())

	test := func(teamID, repoID int64, expected bool) {
		team := unittest.AssertExistsAndLoadBean(t, &organization.Team{ID: teamID})
		assert.Equal(t, expected, repo_service.HasRepository(t.Context(), team, repoID))
	}
	test(1, 1, false)
	test(1, 3, true)
	test(1, 5, true)
	test(1, unittest.NonexistentID, false)

	test(2, 3, true)
	test(2, 5, false)
}

func TestTeam_RemoveRepository(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())

	testSuccess := func(teamID, repoID int64) {
		team := unittest.AssertExistsAndLoadBean(t, &organization.Team{ID: teamID})
		assert.NoError(t, repo_service.RemoveRepositoryFromTeam(t.Context(), team, repoID))
		unittest.AssertNotExistsBean(t, &organization.TeamRepo{TeamID: teamID, RepoID: repoID})
		unittest.CheckConsistencyFor(t, &organization.Team{ID: teamID}, &repo_model.Repository{ID: repoID})
	}
	testSuccess(2, 3)
	testSuccess(2, 5)
	testSuccess(1, unittest.NonexistentID)
}

func TestDeleteOwnerRepositoriesDirectly(t *testing.T) {
	unittest.PrepareTestEnv(t)

	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

	assert.NoError(t, repo_service.DeleteOwnerRepositoriesDirectly(t.Context(), user))

	t.Run("RejectedInTransaction", func(t *testing.T) {
		unittest.PrepareTestEnv(t)

		err := db.WithTx(t.Context(), func(ctx context.Context) error {
			return repo_service.DeleteRepositoryDirectly(ctx, 1)
		})
		assert.ErrorContains(t, err, "must not be called within a transaction")
		unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	})
}

func TestDeleteRepositoryDirectlyPurgesRepoScopedRows(t *testing.T) {
	unittest.PrepareTestEnv(t)
	branchroomScope := &branchroom_model.Scope{
		Kind:        branchroom_model.ScopeKindRepository,
		RepoID:      1,
		Name:        "to purge",
		State:       branchroom_model.ScopeStateActive,
		OwnerType:   branchroom_model.ScopeOwnerTypeUser,
		OwnerID:     2,
		CreatedByID: 2,
	}
	assert.NoError(t, branchroom_model.InsertScope(t.Context(), branchroomScope))
	branchroomGrant := &branchroom_model.Grant{
		ScopeID:     branchroomScope.ID,
		SubjectType: branchroom_model.SubjectTypeUser,
		SubjectID:   2,
		Action:      branchroom_model.ActionDiscover,
		CreatedByID: 2,
	}
	assert.NoError(t, branchroom_model.InsertGrant(t.Context(), branchroomGrant))
	_, err := branchroom_model.RecordDecisionAndAudit(t.Context(), branchroomScope, &branchroom_model.Decision{
		ScopeID:         branchroomScope.ID,
		RepoID:          1,
		GrantID:         branchroomGrant.ID,
		ActorType:       branchroom_model.ActorTypeUser,
		ActorID:         2,
		Action:          branchroom_model.ActionDiscover,
		ResourceType:    branchroom_model.ResourceTypeScope,
		ResourceID:      "scope/to-purge",
		Surface:         branchroom_model.SurfaceSystem,
		Result:          branchroom_model.DecisionResultAllow,
		ReasonCode:      "grant_matched",
		OrdinaryAllowed: true,
	})
	assert.NoError(t, err)

	// One row per table that repository deletion used to leave behind (#38494).
	assert.NoError(t, db.Insert(t.Context(),
		&actions_model.ActionVariable{RepoID: 1, Name: "to_purge", Data: "value"},
		&actions_model.ActionRunAttempt{RepoID: 1, RunID: unittest.NonexistentID, Attempt: 1},
		&actions_model.ActionTasksVersion{RepoID: 1, Version: 1},
		&git_model.RenamedBranch{RepoID: 1, From: "old-name", To: "new-name"},
		&git_model.CommitStatusSummary{RepoID: 1, SHA: "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", State: "success"},
		&repo_model.RepoTransfer{RepoID: 1, DoerID: 2, RecipientID: 3},
	))
	unittest.AssertExistsAndLoadBean(t, &git_model.CommitStatusIndex{RepoID: 1})

	assert.NoError(t, repo_service.DeleteRepositoryDirectly(t.Context(), 1))

	unittest.AssertNotExistsBean(t, &actions_model.ActionVariable{RepoID: 1})
	unittest.AssertNotExistsBean(t, &actions_model.ActionRunAttempt{RepoID: 1})
	unittest.AssertNotExistsBean(t, &actions_model.ActionTasksVersion{RepoID: 1})
	unittest.AssertNotExistsBean(t, &git_model.RenamedBranch{RepoID: 1})
	unittest.AssertNotExistsBean(t, &git_model.CommitStatusSummary{RepoID: 1})
	unittest.AssertNotExistsBean(t, &git_model.CommitStatusIndex{RepoID: 1})
	unittest.AssertNotExistsBean(t, &repo_model.RepoTransfer{RepoID: 1})
	unittest.AssertNotExistsBean(t, &branchroom_model.Scope{RepoID: 1})
	unittest.AssertNotExistsBean(t, &branchroom_model.Grant{ScopeID: branchroomScope.ID})
	unittest.AssertNotExistsBean(t, &branchroom_model.Decision{RepoID: 1})
	unittest.AssertNotExistsBean(t, &branchroom_model.AuditEvent{RepoID: 1})
}

func TestDeleteRepositoryDirectlyBlocksActiveBranchroomWorkspace(t *testing.T) {
	for _, state := range []branchroom_model.WorkspaceState{
		branchroom_model.WorkspaceStateProvisioning,
		branchroom_model.WorkspaceStateReady,
		branchroom_model.WorkspaceStateFailed,
	} {
		t.Run(string(state), func(t *testing.T) {
			unittest.PrepareTestEnv(t)
			branchroomScope := &branchroom_model.Scope{
				Kind:        branchroom_model.ScopeKindRepository,
				RepoID:      1,
				Name:        "requires retention",
				State:       branchroom_model.ScopeStateActive,
				OwnerType:   branchroom_model.ScopeOwnerTypeUser,
				OwnerID:     2,
				CreatedByID: 2,
			}
			assert.NoError(t, branchroom_model.InsertScope(t.Context(), branchroomScope))
			workspace := &branchroom_model.Workspace{
				WorkspaceKey:     "11111111-1111-1111-1111-111111111111",
				ScopeID:          branchroomScope.ID,
				RepoID:           1,
				StoreID:          "22222222-2222-2222-2222-222222222222",
				PublicBaseSHA:    strings.Repeat("a", 40),
				ObjectFormatName: "sha1",
				State:            state,
				FailureCode:      "import_failed",
				CreatedByID:      2,
			}
			if state != branchroom_model.WorkspaceStateFailed {
				workspace.FailureCode = ""
			}
			assert.NoError(t, branchroom_model.InsertWorkspace(t.Context(), workspace))

			err := repo_service.DeleteRepositoryDirectly(t.Context(), 1)
			assert.ErrorIs(t, err, branchroom_service.ErrWorkspaceRetentionRequired)
			unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
			unittest.AssertExistsAndLoadBean(t, &branchroom_model.Scope{ID: branchroomScope.ID})
			unittest.AssertExistsAndLoadBean(t, &branchroom_model.Workspace{ID: workspace.ID})
		})
	}
}

func TestDeleteRepositoryDirectlyAllowsTerminalBranchroomWorkspace(t *testing.T) {
	unittest.PrepareTestEnv(t)
	retiredScope := &branchroom_model.Scope{
		Kind:        branchroom_model.ScopeKindRepository,
		RepoID:      1,
		Name:        "retired retention",
		State:       branchroom_model.ScopeStateActive,
		OwnerType:   branchroom_model.ScopeOwnerTypeUser,
		OwnerID:     2,
		CreatedByID: 2,
	}
	assert.NoError(t, branchroom_model.InsertScope(t.Context(), retiredScope))
	retired := &branchroom_model.Workspace{
		WorkspaceKey:     "11111111-1111-1111-1111-111111111111",
		ScopeID:          retiredScope.ID,
		RepoID:           1,
		StoreID:          "22222222-2222-2222-2222-222222222222",
		PublicBaseSHA:    strings.Repeat("a", 40),
		ObjectFormatName: "sha1",
		State:            branchroom_model.WorkspaceStateRetired,
		RetiredUnix:      timeutil.TimeStampNow(),
		CreatedByID:      2,
	}
	assert.NoError(t, branchroom_model.InsertWorkspace(t.Context(), retired))

	purgedScope := &branchroom_model.Scope{
		Kind:        branchroom_model.ScopeKindRepository,
		RepoID:      1,
		Name:        "purged retention",
		State:       branchroom_model.ScopeStateActive,
		OwnerType:   branchroom_model.ScopeOwnerTypeUser,
		OwnerID:     2,
		CreatedByID: 2,
	}
	assert.NoError(t, branchroom_model.InsertScope(t.Context(), purgedScope))
	purged := &branchroom_model.Workspace{
		WorkspaceKey:     "33333333-3333-3333-3333-333333333333",
		ScopeID:          purgedScope.ID,
		RepoID:           1,
		StoreID:          "44444444-4444-4444-4444-444444444444",
		PublicBaseSHA:    strings.Repeat("a", 40),
		ObjectFormatName: "sha1",
		State:            branchroom_model.WorkspaceStatePurged,
		RetiredUnix:      timeutil.TimeStampNow(),
		CreatedByID:      2,
	}
	assert.NoError(t, branchroom_model.InsertWorkspace(t.Context(), purged))

	assert.NoError(t, repo_service.DeleteRepositoryDirectly(t.Context(), 1))

	unittest.AssertNotExistsBean(t, &repo_model.Repository{ID: 1})
	unittest.AssertNotExistsBean(t, &branchroom_model.Workspace{RepoID: 1})
	unittest.AssertNotExistsBean(t, &branchroom_model.Scope{RepoID: 1})
}

func TestDeleteRepositoryDirectlyBlocksUnpurgedBranchroomEnvironmentRevision(t *testing.T) {
	unittest.PrepareTestEnv(t)
	branchroomScope := &branchroom_model.Scope{
		Kind:        branchroom_model.ScopeKindEnvironment,
		RepoID:      1,
		Name:        "environment requires retention",
		State:       branchroom_model.ScopeStateActive,
		OwnerType:   branchroom_model.ScopeOwnerTypeUser,
		OwnerID:     2,
		CreatedByID: 2,
	}
	assert.NoError(t, branchroom_model.InsertScope(t.Context(), branchroomScope))
	payloadCiphertext := []byte("ciphertext-only-environment-payload")
	revision := &branchroom_model.EnvironmentRevision{
		RevisionKey:          "33333333-3333-3333-3333-333333333333",
		ScopeID:              branchroomScope.ID,
		RepoID:               1,
		Revision:             1,
		CipherSuite:          branchroom_model.EnvironmentCipherSuiteAES256GCM,
		PayloadNonce:         []byte("0123456789ab"),
		PayloadCiphertext:    payloadCiphertext,
		WrappedDEKNonce:      []byte("abcdefghijkl"),
		WrappedDEKCiphertext: []byte("wrapped-data-encryption-key"),
		KeyProvider:          branchroom_model.EnvironmentKeyProviderLocalDev,
		KeyReference:         "local-dev/primary-v1",
		CiphertextDigest:     branchroom_model.EnvironmentCiphertextDigest(payloadCiphertext),
		PlaintextLength:      27,
		State:                branchroom_model.EnvironmentRevisionStateActive,
		CreatedByID:          2,
	}
	assert.NoError(t, branchroom_model.InsertEnvironmentRevision(t.Context(), revision))

	err := repo_service.DeleteRepositoryDirectly(t.Context(), 1)
	assert.ErrorIs(t, err, branchroom_service.ErrEnvironmentRetentionRequired)
	unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.Scope{ID: branchroomScope.ID})
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.EnvironmentRevision{ID: revision.ID})
}
