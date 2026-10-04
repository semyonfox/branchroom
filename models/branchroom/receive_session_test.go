// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"strings"
	"testing"

	"gitea.dev/models/unittest"
	"gitea.dev/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReceiveSessionValidation(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	session := newTestReceiveSession()
	require.NoError(t, InsertReceiveSession(ctx, session))

	stored, err := GetReceiveSessionByKey(ctx, session.SessionKey)
	require.NoError(t, err)
	assert.Equal(t, session.BearerSecretHash, stored.BearerSecretHash)
	storedByHash, err := GetReceiveSessionByBearerSecretHash(ctx, session.BearerSecretHash)
	require.NoError(t, err)
	assert.Equal(t, session.ID, storedByHash.ID)
	_, err = GetReceiveSessionByKey(ctx, "55555555-5555-5555-5555-999999999999")
	assert.ErrorIs(t, err, util.ErrNotExist)

	duplicateKey := newTestReceiveSession()
	duplicateKey.BearerSecretHash = strings.Repeat("e", 64)
	assert.Error(t, InsertReceiveSession(ctx, duplicateKey))
	duplicateHash := newTestReceiveSession()
	duplicateHash.SessionKey = "66666666-6666-6666-6666-666666666666"
	assert.Error(t, InsertReceiveSession(ctx, duplicateHash))

	invalid := newTestReceiveSession()
	invalid.BearerSecretHash = "raw-bearer-secret"
	assert.Error(t, invalid.Validate())
	invalid = newTestReceiveSession()
	invalid.ActorType = ActorTypeActionsTask
	assert.Error(t, invalid.Validate())
	invalid = newTestReceiveSession()
	invalid.State = ReceiveSessionStateClaimed
	assert.Error(t, invalid.Validate())
	invalid = newTestReceiveSession()
	invalid.State = ReceiveSessionStateCompleted
	assert.Error(t, invalid.Validate())

	invalid = newTestReceiveSession()
	invalid.Outcome = ReceiveSessionOutcomeDenied
	assert.Error(t, invalid.Validate())
	invalid = newTestReceiveSession()
	invalid.State = ReceiveSessionStateClaimed
	invalid.ClaimedUnix = 150
	invalid.Outcome = ReceiveSessionOutcomeFailed
	assert.Error(t, invalid.Validate())
	invalid = newTestReceiveSession()
	invalid.State = ReceiveSessionStateCompleted
	invalid.ClaimedUnix = 150
	assert.Error(t, invalid.Validate())
	invalid = newTestReceiveSession()
	invalid.State = ReceiveSessionStateExpired
	invalid.Outcome = ReceiveSessionOutcomePending
	assert.Error(t, invalid.Validate())
	invalid = newTestReceiveSession()
	invalid.State = ReceiveSessionStateRevoked
	invalid.Outcome = ReceiveSessionOutcomeLegacy
	assert.Error(t, invalid.Validate())

	valid := newTestReceiveSession()
	valid.State = ReceiveSessionStateCompleted
	valid.ClaimedUnix = 150
	valid.Outcome = ReceiveSessionOutcomeDenied
	assert.NoError(t, valid.Validate())
	valid.Outcome = ReceiveSessionOutcomeFailed
	assert.NoError(t, valid.Validate())
	valid.Outcome = ReceiveSessionOutcomeLegacy
	assert.NoError(t, valid.Validate())
	valid.Outcome = ReceiveSessionOutcomeAdmitted
	assert.NoError(t, valid.Validate())

	claimed := newTestReceiveSession()
	claimed.State = ReceiveSessionStateClaimed
	claimed.ClaimedUnix = 150
	claimed.Outcome = ReceiveSessionOutcomeAdmitted
	assert.Error(t, claimed.Validate())
}

func TestReceiveSessionClaimAndComplete(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	session := newTestReceiveSession()
	require.NoError(t, InsertReceiveSession(ctx, session))

	claimed, err := ClaimReceiveSession(ctx, session.ID, 150)
	require.NoError(t, err)
	assert.True(t, claimed)
	stored, err := GetReceiveSessionByID(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, ReceiveSessionOutcomePending, stored.Outcome)
	claimed, err = ClaimReceiveSession(ctx, session.ID, 151)
	require.NoError(t, err)
	assert.False(t, claimed)

	stored, err = GetReceiveSessionByID(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, ReceiveSessionStateClaimed, stored.State)
	assert.Equal(t, int64(150), int64(stored.ClaimedUnix))
	revoked, err := RevokeReceiveSession(ctx, session.ID)
	require.NoError(t, err)
	assert.False(t, revoked)
	expired, err := ExpireReceiveSession(ctx, session.ID, 400)
	require.NoError(t, err)
	assert.False(t, expired)

	for _, outcome := range []ReceiveSessionOutcome{
		ReceiveSessionOutcomePending,
		ReceiveSessionOutcomeExpired,
		ReceiveSessionOutcomeRevoked,
		ReceiveSessionOutcomeLegacy,
		ReceiveSessionOutcome("nonsense"),
	} {
		completed, err := CompleteReceiveSession(ctx, session.ID, outcome)
		assert.Error(t, err)
		assert.False(t, completed)
	}
	completed, err := CompleteReceiveSession(ctx, session.ID, ReceiveSessionOutcomeDenied)
	require.NoError(t, err)
	assert.True(t, completed)
	completed, err = CompleteReceiveSession(ctx, session.ID, ReceiveSessionOutcomeFailed)
	require.NoError(t, err)
	assert.False(t, completed)
	stored, err = GetReceiveSessionByID(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, ReceiveSessionOutcomeDenied, stored.Outcome)

	expiredSession := newTestReceiveSession()
	expiredSession.SessionKey = "66666666-6666-6666-6666-666666666666"
	expiredSession.BearerSecretHash = strings.Repeat("e", 64)
	expiredSession.ExpiresUnix = 150
	require.NoError(t, InsertReceiveSession(ctx, expiredSession))
	claimed, err = ClaimReceiveSession(ctx, expiredSession.ID, expiredSession.ExpiresUnix)
	require.NoError(t, err)
	assert.False(t, claimed)
	expired, err = ExpireReceiveSession(ctx, expiredSession.ID, expiredSession.ExpiresUnix)
	require.NoError(t, err)
	assert.True(t, expired)
	expired, err = ExpireReceiveSession(ctx, expiredSession.ID, expiredSession.ExpiresUnix)
	require.NoError(t, err)
	assert.False(t, expired)
	stored, err = GetReceiveSessionByID(ctx, expiredSession.ID)
	require.NoError(t, err)
	assert.Equal(t, ReceiveSessionStateExpired, stored.State)
	assert.Equal(t, ReceiveSessionOutcomeExpired, stored.Outcome)
}

// Admitted completes a claimed session like denied or failed, and the session still ends one-use.
func TestReceiveSessionCompleteAdmitted(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	session := newTestReceiveSession()
	require.NoError(t, InsertReceiveSession(ctx, session))

	completed, err := CompleteReceiveSession(ctx, session.ID, ReceiveSessionOutcomeAdmitted)
	require.NoError(t, err)
	assert.False(t, completed)

	claimed, err := ClaimReceiveSession(ctx, session.ID, 150)
	require.NoError(t, err)
	require.True(t, claimed)

	completed, err = CompleteReceiveSession(ctx, session.ID, ReceiveSessionOutcomeAdmitted)
	require.NoError(t, err)
	assert.True(t, completed)
	stored, err := GetReceiveSessionByID(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, ReceiveSessionOutcomeAdmitted, stored.Outcome)

	claimed, err = ClaimReceiveSession(ctx, session.ID, 151)
	require.NoError(t, err)
	assert.False(t, claimed)
	completed, err = CompleteReceiveSession(ctx, session.ID, ReceiveSessionOutcomeDenied)
	require.NoError(t, err)
	assert.False(t, completed)
}

func TestFailStaleClaimedReceiveSessions(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	stale := newTestReceiveSession()
	stale.ExpiresUnix = 400
	require.NoError(t, InsertReceiveSession(ctx, stale))
	claimed, err := ClaimReceiveSession(ctx, stale.ID, 150)
	require.NoError(t, err)
	require.True(t, claimed)

	fresh := newTestReceiveSession()
	fresh.SessionKey = "66666666-6666-6666-6666-666666666666"
	fresh.BearerSecretHash = strings.Repeat("e", 64)
	fresh.ExpiresUnix = 400
	require.NoError(t, InsertReceiveSession(ctx, fresh))
	claimed, err = ClaimReceiveSession(ctx, fresh.ID, 151)
	require.NoError(t, err)
	require.True(t, claimed)

	completed := newTestReceiveSession()
	completed.SessionKey = "77777777-7777-7777-7777-777777777777"
	completed.BearerSecretHash = strings.Repeat("f", 64)
	completed.ExpiresUnix = 400
	require.NoError(t, InsertReceiveSession(ctx, completed))
	claimed, err = ClaimReceiveSession(ctx, completed.ID, 150)
	require.NoError(t, err)
	require.True(t, claimed)
	finished, err := CompleteReceiveSession(ctx, completed.ID, ReceiveSessionOutcomeAdmitted)
	require.NoError(t, err)
	require.True(t, finished)

	failed, err := FailStaleClaimedReceiveSessions(ctx, 150)
	require.NoError(t, err)
	assert.EqualValues(t, 1, failed)

	stored, err := GetReceiveSessionByID(ctx, stale.ID)
	require.NoError(t, err)
	assert.Equal(t, ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, ReceiveSessionOutcomeFailed, stored.Outcome)
	stored, err = GetReceiveSessionByID(ctx, fresh.ID)
	require.NoError(t, err)
	assert.Equal(t, ReceiveSessionStateClaimed, stored.State)
	assert.Equal(t, ReceiveSessionOutcomePending, stored.Outcome)
	stored, err = GetReceiveSessionByID(ctx, completed.ID)
	require.NoError(t, err)
	assert.Equal(t, ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, ReceiveSessionOutcomeAdmitted, stored.Outcome)

	failed, err = FailStaleClaimedReceiveSessions(ctx, 150)
	require.NoError(t, err)
	assert.Zero(t, failed)
	_, err = FailStaleClaimedReceiveSessions(ctx, 0)
	assert.ErrorIs(t, err, util.ErrInvalidArgument)
}

// A claimed session stays completable after its nominal expiry because claim already enforced
// the expiry bound and claim is irreversible.
func TestReceiveSessionClaimedAfterExpiryCompletes(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	session := newTestReceiveSession()
	session.ExpiresUnix = 150
	require.NoError(t, InsertReceiveSession(ctx, session))
	claimed, err := ClaimReceiveSession(ctx, session.ID, 149)
	require.NoError(t, err)
	assert.True(t, claimed)

	expired, err := ExpireReceiveSession(ctx, session.ID, 400)
	require.NoError(t, err)
	assert.False(t, expired)
	revoked, err := RevokeReceiveSession(ctx, session.ID)
	require.NoError(t, err)
	assert.False(t, revoked)

	completed, err := CompleteReceiveSession(ctx, session.ID, ReceiveSessionOutcomeFailed)
	require.NoError(t, err)
	assert.True(t, completed)
	stored, err := GetReceiveSessionByID(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, ReceiveSessionOutcomeFailed, stored.Outcome)
}

func TestReceiveSessionRevokeAndFindings(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	session := newTestReceiveSession()
	require.NoError(t, InsertReceiveSession(ctx, session))
	otherScopeSession := newTestReceiveSession()
	otherScopeSession.SessionKey = "66666666-6666-6666-6666-666666666666"
	otherScopeSession.BearerSecretHash = strings.Repeat("e", 64)
	otherScopeSession.ScopeID = 2
	otherScopeSession.RepoID = 2
	require.NoError(t, InsertReceiveSession(ctx, otherScopeSession))

	sessions, err := FindReceiveSessionsByScopeID(ctx, session.ScopeID)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, session.ID, sessions[0].ID)
	sessions, err = FindReceiveSessionsByRepoID(ctx, session.RepoID)
	require.NoError(t, err)
	require.Len(t, sessions, 1)
	assert.Equal(t, session.ID, sessions[0].ID)

	revoked, err := RevokeReceiveSession(ctx, session.ID)
	require.NoError(t, err)
	assert.True(t, revoked)
	revoked, err = RevokeReceiveSession(ctx, session.ID)
	require.NoError(t, err)
	assert.False(t, revoked)
	stored, err := GetReceiveSessionByID(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, ReceiveSessionStateRevoked, stored.State)
	assert.Equal(t, ReceiveSessionOutcomeRevoked, stored.Outcome)
	claimed, err := ClaimReceiveSession(ctx, session.ID, 150)
	require.NoError(t, err)
	assert.False(t, claimed)
}

func newTestReceiveSession() *ReceiveSession {
	return &ReceiveSession{
		SessionKey:       "55555555-5555-5555-5555-555555555555",
		BearerSecretHash: strings.Repeat("d", 64),
		ScopeID:          1,
		WorkspaceID:      1,
		RepoID:           1,
		ActorType:        ActorTypeUser,
		ActorID:          42,
		State:            ReceiveSessionStateIssued,
		Outcome:          ReceiveSessionOutcomePending,
		IssuedUnix:       100,
		ExpiresUnix:      200,
	}
}
