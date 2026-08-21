// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"bytes"
	"strings"
	"testing"

	"gitea.dev/models/unittest"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentRevisionValidation(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	revision := newTestEnvironmentRevision()
	require.NoError(t, InsertEnvironmentRevision(ctx, revision))

	stored, err := GetEnvironmentRevisionByKey(ctx, revision.RevisionKey)
	require.NoError(t, err)
	assert.Equal(t, revision.CiphertextDigest, stored.CiphertextDigest)
	storedByNumber, err := GetEnvironmentRevisionByScopeIDAndRevision(ctx, revision.ScopeID, revision.Revision)
	require.NoError(t, err)
	assert.Equal(t, revision.ID, storedByNumber.ID)
	_, err = GetEnvironmentRevisionByScopeIDAndRevision(ctx, revision.ScopeID, revision.Revision+1)
	assert.ErrorIs(t, err, util.ErrNotExist)

	duplicateRevision := newTestEnvironmentRevision()
	duplicateRevision.RevisionKey = "22222222-2222-2222-2222-222222222222"
	assert.Error(t, InsertEnvironmentRevision(ctx, duplicateRevision))

	invalid := newTestEnvironmentRevision()
	invalid.KeyReference = "/tmp/branchroom-key"
	assert.Error(t, invalid.Validate())
	invalid = newTestEnvironmentRevision()
	invalid.PayloadCiphertext = []byte("different-ciphertext")
	assert.Error(t, invalid.Validate())
	invalid = newTestEnvironmentRevision()
	invalid.State = EnvironmentRevisionStateRetired
	assert.Error(t, invalid.Validate())
}

func TestEnvironmentLeaseClaim(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	revision := newTestEnvironmentRevision()
	require.NoError(t, InsertEnvironmentRevision(ctx, revision))
	lease := newTestEnvironmentLease(revision.ID)
	require.NoError(t, InsertEnvironmentLease(ctx, lease))

	stored, err := GetEnvironmentLeaseByBearerSecretHash(ctx, lease.BearerSecretHash)
	require.NoError(t, err)
	assert.Equal(t, lease.LeaseKey, stored.LeaseKey)

	claimed, err := ClaimEnvironmentLease(ctx, lease.ID, 150)
	require.NoError(t, err)
	assert.True(t, claimed)
	claimed, err = ClaimEnvironmentLease(ctx, lease.ID, 151)
	require.NoError(t, err)
	assert.False(t, claimed)

	stored, err = GetEnvironmentLeaseByID(ctx, lease.ID)
	require.NoError(t, err)
	assert.Equal(t, EnvironmentLeaseStateClaimed, stored.State)
	assert.Equal(t, EnvironmentLeaseOutcomePending, stored.Outcome)
	assert.Equal(t, timeutil.TimeStamp(150), stored.ClaimedUnix)
	revoked, err := RevokeEnvironmentLease(ctx, lease.ID)
	require.NoError(t, err)
	assert.False(t, revoked)

	completed, err := CompleteEnvironmentLease(ctx, lease.ID, EnvironmentLeaseOutcomeMaterialized)
	require.NoError(t, err)
	assert.True(t, completed)
	completed, err = CompleteEnvironmentLease(ctx, lease.ID, EnvironmentLeaseOutcomeMaterialized)
	require.NoError(t, err)
	assert.False(t, completed)
	stored, err = GetEnvironmentLeaseByID(ctx, lease.ID)
	require.NoError(t, err)
	assert.Equal(t, EnvironmentLeaseStateCompleted, stored.State)
	assert.Equal(t, EnvironmentLeaseOutcomeMaterialized, stored.Outcome)

	expired := newTestEnvironmentLease(revision.ID)
	expired.LeaseKey = "44444444-4444-4444-4444-444444444444"
	expired.BearerSecretHash = strings.Repeat("b", 64)
	expired.ExpiresUnix = 150
	require.NoError(t, InsertEnvironmentLease(ctx, expired))
	claimed, err = ClaimEnvironmentLease(ctx, expired.ID, expired.ExpiresUnix)
	require.NoError(t, err)
	assert.False(t, claimed)
	expiredLease, err := ExpireEnvironmentLease(ctx, expired.ID, expired.ExpiresUnix)
	require.NoError(t, err)
	assert.True(t, expiredLease)
	expiredLease, err = ExpireEnvironmentLease(ctx, expired.ID, expired.ExpiresUnix)
	require.NoError(t, err)
	assert.False(t, expiredLease)

	invalid := newTestEnvironmentLease(revision.ID)
	invalid.BearerSecretHash = "raw-secret"
	assert.Error(t, invalid.Validate())
	invalid = newTestEnvironmentLease(revision.ID)
	invalid.ActorType = ActorTypeActionsTask
	assert.Error(t, invalid.Validate())
}

func TestEnvironmentLifecycleFindsAndTransitions(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	revision := newTestEnvironmentRevision()
	require.NoError(t, InsertEnvironmentRevision(ctx, revision))
	otherRevision := newTestEnvironmentRevision()
	otherRevision.RevisionKey = "22222222-2222-2222-2222-222222222222"
	otherRevision.ScopeID = 2
	otherRevision.RepoID = 2
	require.NoError(t, InsertEnvironmentRevision(ctx, otherRevision))

	lease := newTestEnvironmentLease(revision.ID)
	require.NoError(t, InsertEnvironmentLease(ctx, lease))
	otherLease := newTestEnvironmentLease(otherRevision.ID)
	otherLease.LeaseKey = "44444444-4444-4444-4444-444444444444"
	otherLease.BearerSecretHash = strings.Repeat("b", 64)
	otherLease.ScopeID = otherRevision.ScopeID
	require.NoError(t, InsertEnvironmentLease(ctx, otherLease))

	revisions, err := FindEnvironmentRevisionsByScopeID(ctx, revision.ScopeID)
	require.NoError(t, err)
	require.Len(t, revisions, 1)
	assert.Equal(t, revision.ID, revisions[0].ID)
	revisions, err = FindEnvironmentRevisionsByRepoID(ctx, revision.RepoID)
	require.NoError(t, err)
	require.Len(t, revisions, 1)
	assert.Equal(t, revision.ID, revisions[0].ID)

	leases, err := FindEnvironmentLeasesByScopeID(ctx, lease.ScopeID)
	require.NoError(t, err)
	require.Len(t, leases, 1)
	assert.Equal(t, lease.ID, leases[0].ID)
	leases, err = FindEnvironmentLeasesByRepoID(ctx, revision.RepoID)
	require.NoError(t, err)
	require.Len(t, leases, 1)
	assert.Equal(t, lease.ID, leases[0].ID)

	updated, err := RetireEnvironmentRevision(ctx, revision.ID, 250)
	require.NoError(t, err)
	assert.True(t, updated)
	updated, err = RetireEnvironmentRevision(ctx, revision.ID, 251)
	require.NoError(t, err)
	assert.False(t, updated)

	updated, err = RevokeEnvironmentLease(ctx, lease.ID)
	require.NoError(t, err)
	assert.True(t, updated)
	updated, err = RevokeEnvironmentLease(ctx, lease.ID)
	require.NoError(t, err)
	assert.False(t, updated)
	revoked, err := GetEnvironmentLeaseByID(ctx, lease.ID)
	require.NoError(t, err)
	assert.Equal(t, EnvironmentLeaseStateRevoked, revoked.State)
	assert.Equal(t, EnvironmentLeaseOutcomeRevoked, revoked.Outcome)
}

func newTestEnvironmentRevision() *EnvironmentRevision {
	payloadCiphertext := []byte("ciphertext-only-environment-payload")
	return &EnvironmentRevision{
		RevisionKey:          "11111111-1111-1111-1111-111111111111",
		ScopeID:              1,
		RepoID:               1,
		Revision:             1,
		CipherSuite:          EnvironmentCipherSuiteAES256GCM,
		PayloadNonce:         bytes.Repeat([]byte{1}, environmentAESGCMNonceLength),
		PayloadCiphertext:    payloadCiphertext,
		WrappedDEKNonce:      bytes.Repeat([]byte{2}, environmentAESGCMNonceLength),
		WrappedDEKCiphertext: []byte("wrapped-data-encryption-key"),
		KeyProvider:          EnvironmentKeyProviderLocalDev,
		KeyReference:         "local-dev/primary-v1",
		CiphertextDigest:     EnvironmentCiphertextDigest(payloadCiphertext),
		PlaintextLength:      27,
		State:                EnvironmentRevisionStateActive,
		CreatedByID:          1,
	}
}

func newTestEnvironmentLease(revisionID int64) *EnvironmentLease {
	return &EnvironmentLease{
		LeaseKey:         "33333333-3333-3333-3333-333333333333",
		BearerSecretHash: strings.Repeat("a", 64),
		ScopeID:          1,
		RevisionID:       revisionID,
		ActorType:        ActorTypeUser,
		ActorID:          42,
		GrantID:          1,
		DecisionID:       1,
		IssuedUnix:       100,
		ExpiresUnix:      200,
		State:            EnvironmentLeaseStateIssued,
		Outcome:          EnvironmentLeaseOutcomePending,
	}
}
