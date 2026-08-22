// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	activities_model "gitea.dev/models/activities"
	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/organization"
	access_model "gitea.dev/models/perm/access"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/git"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	"gitea.dev/modules/timeutil"
	"gitea.dev/services/feed"
	notify_service "gitea.dev/services/notify"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var notifySync sync.Once

func registerNotifier() {
	notifySync.Do(func() {
		notify_service.RegisterNotifier(feed.NewNotifier())
	})
}

func TestTransferOwnership(t *testing.T) {
	registerNotifier()

	assert.NoError(t, unittest.PrepareTestDatabase())

	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	sourceRepo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 3})
	formerScopeUser := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	branchroomScope := &branchroom_model.Scope{
		Kind:        branchroom_model.ScopeKindRepository,
		RepoID:      sourceRepo.ID,
		Name:        "transfer scope",
		State:       branchroom_model.ScopeStateActive,
		OwnerType:   branchroom_model.ScopeOwnerTypeUser,
		OwnerID:     formerScopeUser.ID,
		CreatedByID: formerScopeUser.ID,
	}
	assert.NoError(t, branchroom_model.InsertScope(t.Context(), branchroomScope))
	branchroomGrant := &branchroom_model.Grant{
		ScopeID:     branchroomScope.ID,
		SubjectType: branchroom_model.SubjectTypeUser,
		SubjectID:   formerScopeUser.ID,
		Action:      branchroom_model.ActionDiscover,
		CreatedByID: formerScopeUser.ID,
	}
	assert.NoError(t, branchroom_model.InsertGrant(t.Context(), branchroomGrant))
	branchroomSourceGrant := &branchroom_model.Grant{
		ScopeID:     branchroomScope.ID,
		SubjectType: branchroom_model.SubjectTypeUser,
		SubjectID:   formerScopeUser.ID,
		Action:      branchroom_model.ActionReadSource,
		CreatedByID: formerScopeUser.ID,
	}
	assert.NoError(t, branchroom_model.InsertGrant(t.Context(), branchroomSourceGrant))
	branchroomWorkspace := &branchroom_model.Workspace{
		WorkspaceKey:     "11111111-1111-1111-1111-111111111111",
		ScopeID:          branchroomScope.ID,
		RepoID:           sourceRepo.ID,
		StoreID:          "22222222-2222-2222-2222-222222222222",
		PublicBaseSHA:    strings.Repeat("a", 40),
		ObjectFormatName: "sha1",
		State:            branchroom_model.WorkspaceStateReady,
		CreatedByID:      formerScopeUser.ID,
	}
	assert.NoError(t, branchroom_model.InsertWorkspace(t.Context(), branchroomWorkspace))
	branchroomEnvironmentScope := &branchroom_model.Scope{
		Kind:        branchroom_model.ScopeKindEnvironment,
		RepoID:      sourceRepo.ID,
		Name:        "transfer environment",
		State:       branchroom_model.ScopeStateActive,
		OwnerType:   branchroom_model.ScopeOwnerTypeUser,
		OwnerID:     formerScopeUser.ID,
		CreatedByID: formerScopeUser.ID,
	}
	assert.NoError(t, branchroom_model.InsertScope(t.Context(), branchroomEnvironmentScope))
	environmentPayload := []byte("ciphertext-only-environment-payload")
	branchroomEnvironmentRevision := &branchroom_model.EnvironmentRevision{
		RevisionKey:          "33333333-3333-3333-3333-333333333333",
		ScopeID:              branchroomEnvironmentScope.ID,
		RepoID:               sourceRepo.ID,
		Revision:             1,
		CipherSuite:          branchroom_model.EnvironmentCipherSuiteAES256GCM,
		PayloadNonce:         bytes.Repeat([]byte{1}, 12),
		PayloadCiphertext:    environmentPayload,
		WrappedDEKNonce:      bytes.Repeat([]byte{2}, 12),
		WrappedDEKCiphertext: []byte("wrapped-data-encryption-key"),
		KeyProvider:          branchroom_model.EnvironmentKeyProviderLocalDev,
		KeyReference:         "local-dev/primary-v1",
		CiphertextDigest:     branchroom_model.EnvironmentCiphertextDigest(environmentPayload),
		PlaintextLength:      27,
		State:                branchroom_model.EnvironmentRevisionStateActive,
		CreatedByID:          formerScopeUser.ID,
	}
	assert.NoError(t, branchroom_model.InsertEnvironmentRevision(t.Context(), branchroomEnvironmentRevision))
	branchroomEnvironmentLease := &branchroom_model.EnvironmentLease{
		LeaseKey:         "44444444-4444-4444-4444-444444444444",
		BearerSecretHash: strings.Repeat("b", 64),
		ScopeID:          branchroomEnvironmentScope.ID,
		RevisionID:       branchroomEnvironmentRevision.ID,
		ActorType:        branchroom_model.ActorTypeUser,
		ActorID:          formerScopeUser.ID,
		GrantID:          branchroomGrant.ID,
		DecisionID:       1,
		IssuedUnix:       100,
		ExpiresUnix:      200,
		State:            branchroom_model.EnvironmentLeaseStateIssued,
		Outcome:          branchroom_model.EnvironmentLeaseOutcomePending,
	}
	assert.NoError(t, branchroom_model.InsertEnvironmentLease(t.Context(), branchroomEnvironmentLease))
	sourceRepoBak := new(*sourceRepo)
	assert.NoError(t, sourceRepo.LoadOwner(t.Context()))
	repoTransfer := unittest.AssertExistsAndLoadBean(t, &repo_model.RepoTransfer{ID: 1})
	assert.NoError(t, repoTransfer.LoadAttributes(t.Context()))
	assert.NoError(t, AcceptTransferOwnership(t.Context(), sourceRepo, doer))

	transferredRepo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 3})
	assert.EqualValues(t, 1, transferredRepo.OwnerID) // repo_transfer.yml id=1
	unittest.AssertNotExistsBean(t, &repo_model.RepoTransfer{ID: 1})

	exist, err := git.IsRepositoryExist(t.Context(), sourceRepoBak)
	assert.NoError(t, err)
	assert.False(t, exist)
	exist, err = git.IsRepositoryExist(t.Context(), transferredRepo)
	assert.NoError(t, err)
	assert.True(t, exist)
	transferredScope := unittest.AssertExistsAndLoadBean(t, &branchroom_model.Scope{ID: branchroomScope.ID})
	assert.Equal(t, branchroom_model.ScopeStateRetired, transferredScope.State)
	transferredGrant := unittest.AssertExistsAndLoadBean(t, &branchroom_model.Grant{ID: branchroomGrant.ID})
	assert.NotZero(t, transferredGrant.RevokedUnix)
	transferredSourceGrant := unittest.AssertExistsAndLoadBean(t, &branchroom_model.Grant{ID: branchroomSourceGrant.ID})
	assert.NotZero(t, transferredSourceGrant.RevokedUnix)
	activeGrants, err := branchroom_model.FindActiveGrantsAt(t.Context(), transferredScope.ID, branchroom_model.ActionDiscover, timeutil.TimeStampNow())
	assert.NoError(t, err)
	assert.Empty(t, activeGrants)
	activeSourceGrants, err := branchroom_model.FindActiveGrantsAt(t.Context(), transferredScope.ID, branchroom_model.ActionReadSource, timeutil.TimeStampNow())
	assert.NoError(t, err)
	assert.Empty(t, activeSourceGrants)
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.AuditEvent{ScopeID: branchroomScope.ID, ReasonCode: "repository_transferred"})
	transferredWorkspace := unittest.AssertExistsAndLoadBean(t, &branchroom_model.Workspace{ID: branchroomWorkspace.ID})
	assert.Equal(t, branchroom_model.WorkspaceStateRetired, transferredWorkspace.State)
	assert.NotZero(t, transferredWorkspace.RetiredUnix)
	transferredEnvironmentRevision := unittest.AssertExistsAndLoadBean(t, &branchroom_model.EnvironmentRevision{ID: branchroomEnvironmentRevision.ID})
	assert.Equal(t, branchroom_model.EnvironmentRevisionStateRetired, transferredEnvironmentRevision.State)
	assert.NotZero(t, transferredEnvironmentRevision.RetiredUnix)
	transferredEnvironmentLease := unittest.AssertExistsAndLoadBean(t, &branchroom_model.EnvironmentLease{ID: branchroomEnvironmentLease.ID})
	assert.Equal(t, branchroom_model.EnvironmentLeaseStateRevoked, transferredEnvironmentLease.State)
	assert.Equal(t, branchroom_model.EnvironmentLeaseOutcomeRevoked, transferredEnvironmentLease.Outcome)
	unittest.AssertExistsAndLoadBean(t, &activities_model.Action{
		OpType:    activities_model.ActionTransferRepo,
		ActUserID: 1,
		RepoID:    3,
		Content:   "org3/repo3",
	})

	unittest.CheckConsistencyFor(t, &repo_model.Repository{}, &user_model.User{}, &organization.Team{})
}

func TestStartRepositoryTransferSetPermission(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())

	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	recipient := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 5})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 2})
	assert.NoError(t, repo.LoadOwner(t.Context()))

	hasAccess, err := access_model.HasAnyUnitAccess(t.Context(), recipient.ID, repo)
	assert.NoError(t, err)
	assert.False(t, hasAccess)

	assert.NoError(t, StartRepositoryTransfer(t.Context(), doer, recipient, repo, nil))

	hasAccess, err = access_model.HasAnyUnitAccess(t.Context(), recipient.ID, repo)
	assert.NoError(t, err)
	assert.True(t, hasAccess)

	unittest.CheckConsistencyFor(t, &repo_model.Repository{}, &user_model.User{}, &organization.Team{})
}

func TestRepositoryTransfer(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())

	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 3})

	transfer, err := repo_model.GetPendingRepositoryTransfer(t.Context(), repo)
	assert.NoError(t, err)
	assert.NotNil(t, transfer)

	// Cancel transfer
	assert.NoError(t, CancelRepositoryTransfer(t.Context(), transfer, doer))

	transfer, err = repo_model.GetPendingRepositoryTransfer(t.Context(), repo)
	assert.Error(t, err)
	assert.Nil(t, transfer)
	assert.True(t, repo_model.IsErrNoPendingTransfer(err))

	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

	assert.NoError(t, repo_model.CreatePendingRepositoryTransfer(t.Context(), doer, user2, repo.ID, nil))

	transfer, err = repo_model.GetPendingRepositoryTransfer(t.Context(), repo)
	assert.NoError(t, err)
	assert.NoError(t, transfer.LoadAttributes(t.Context()))
	assert.Equal(t, "user2", transfer.Recipient.Name)

	org6 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

	// Only transfer can be started at any given time
	err = repo_model.CreatePendingRepositoryTransfer(t.Context(), doer, org6, repo.ID, nil)
	assert.Error(t, err)
	assert.True(t, repo_model.IsErrRepoTransferInProgress(err))

	repo2 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 2})
	// Unknown user, transfer non-existent transfer repo id = 2
	err = repo_model.CreatePendingRepositoryTransfer(t.Context(), doer, &user_model.User{ID: 1000, LowerName: "user1000"}, repo2.ID, nil)
	assert.Error(t, err)

	// Reject transfer
	err = RejectRepositoryTransfer(t.Context(), repo2, doer)
	assert.True(t, repo_model.IsErrNoPendingTransfer(err))
}

// Test transfer rejections
func TestRepositoryTransferRejection(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	// Set limit to 0 repositories so no repositories can be transferred
	defer test.MockVariableValue(&setting.Repository.MaxCreationLimit, 0)()
	defer test.MockVariableValue(&setting.Repository.UserMaxCreationLimit, 0)()
	defer test.MockVariableValue(&setting.Repository.OrgMaxCreationLimit, 0)()

	// Admin case
	doerAdmin := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	repo := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 5})

	transfer, err := repo_model.GetPendingRepositoryTransfer(t.Context(), repo)
	require.NoError(t, err)
	require.NotNil(t, transfer)
	require.NoError(t, transfer.LoadRecipient(t.Context()))

	require.True(t, doerAdmin.CanCreateRepoIn(transfer.Recipient)) // admin is not subject to limits

	// Administrator should not be affected by the limits so transfer should be successful
	assert.NoError(t, AcceptTransferOwnership(t.Context(), repo, doerAdmin))

	// Non admin user case
	doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 10})
	repo = unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 21})

	transfer, err = repo_model.GetPendingRepositoryTransfer(t.Context(), repo)
	require.NoError(t, err)
	require.NotNil(t, transfer)
	require.NoError(t, transfer.LoadRecipient(t.Context()))

	require.False(t, doer.CanCreateRepoIn(transfer.Recipient)) // regular user is subject to limits

	// Cannot accept because of the limit
	err = AcceptTransferOwnership(t.Context(), repo, doer)
	assert.Error(t, err)
	assert.True(t, IsRepositoryLimitReached(err))
}
