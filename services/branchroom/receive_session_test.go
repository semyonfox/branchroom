// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"strings"
	"testing"
	"time"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	"gitea.dev/models/unittest"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReconcileStaleReceiveSessions(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	now := timeutil.TimeStampNow()

	stale := newReconciliationReceiveSession("11111111-1111-1111-1111-111111111111", strings.Repeat("a", 64), now.AddDuration(-5*time.Minute))
	fresh := newReconciliationReceiveSession("22222222-2222-2222-2222-222222222222", strings.Repeat("b", 64), now.AddDuration(-5*time.Minute+time.Second))
	completed := newReconciliationReceiveSession("33333333-3333-3333-3333-333333333333", strings.Repeat("c", 64), now.AddDuration(-10*time.Minute))
	completed.State = branchroom_model.ReceiveSessionStateCompleted
	completed.Outcome = branchroom_model.ReceiveSessionOutcomeAdmitted
	for _, session := range []*branchroom_model.ReceiveSession{stale, fresh, completed} {
		require.NoError(t, branchroom_model.InsertReceiveSession(ctx, session))
	}

	failed, err := reconcileStaleReceiveSessions(ctx, now, 5*time.Minute)
	require.NoError(t, err)
	assert.EqualValues(t, 1, failed)

	stored, err := branchroom_model.GetReceiveSessionByID(ctx, stale.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomeFailed, stored.Outcome)
	stored, err = branchroom_model.GetReceiveSessionByID(ctx, fresh.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateClaimed, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomePending, stored.Outcome)
	stored, err = branchroom_model.GetReceiveSessionByID(ctx, completed.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomeAdmitted, stored.Outcome)

	_, err = ReconcileStaleReceiveSessions(ctx, 0)
	assert.ErrorIs(t, err, util.ErrInvalidArgument)
	_, err = ReconcileStaleReceiveSessions(ctx, time.Millisecond)
	assert.ErrorIs(t, err, util.ErrInvalidArgument)
}

func newReconciliationReceiveSession(sessionKey, bearerHash string, claimedUnix timeutil.TimeStamp) *branchroom_model.ReceiveSession {
	return &branchroom_model.ReceiveSession{
		SessionKey:       sessionKey,
		BearerSecretHash: bearerHash,
		ScopeID:          1,
		WorkspaceID:      1,
		RepoID:           1,
		ActorType:        branchroom_model.ActorTypeUser,
		ActorID:          2,
		State:            branchroom_model.ReceiveSessionStateClaimed,
		Outcome:          branchroom_model.ReceiveSessionOutcomePending,
		IssuedUnix:       claimedUnix.AddDuration(-time.Minute),
		ExpiresUnix:      claimedUnix.AddDuration(time.Hour),
		ClaimedUnix:      claimedUnix,
	}
}

func TestPurgeRemovesReceiveSessions(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	scope := &branchroom_model.Scope{
		Kind:        branchroom_model.ScopeKindRepository,
		RepoID:      2,
		Name:        "receive-session-purge",
		State:       branchroom_model.ScopeStateActive,
		OwnerType:   branchroom_model.ScopeOwnerTypeUser,
		OwnerID:     2,
		CreatedByID: 2,
	}
	require.NoError(t, branchroom_model.InsertScope(ctx, scope))
	session := &branchroom_model.ReceiveSession{
		// no workspace row: purge must still remove sessions orphaned by a future retention collector
		SessionKey:       "55555555-5555-5555-5555-555555555555",
		BearerSecretHash: strings.Repeat("c", 64),
		ScopeID:          scope.ID,
		WorkspaceID:      1,
		RepoID:           2,
		ActorType:        branchroom_model.ActorTypeUser,
		ActorID:          2,
		State:            branchroom_model.ReceiveSessionStateExpired,
		Outcome:          branchroom_model.ReceiveSessionOutcomeExpired,
		IssuedUnix:       100,
		ExpiresUnix:      200,
	}
	require.NoError(t, branchroom_model.InsertReceiveSession(ctx, session))

	require.NoError(t, db.WithTx(ctx, func(txCtx context.Context) error {
		return RetainRepositoryPolicyForDeletion(txCtx, 2)
	}))

	sessions, err := branchroom_model.FindReceiveSessionsByRepoID(ctx, 2)
	require.NoError(t, err)
	assert.Empty(t, sessions)
	retained := unittest.AssertExistsAndLoadBean(t, &branchroom_model.Scope{ID: scope.ID})
	assert.Equal(t, branchroom_model.ScopeStateRetired, retained.State)
}
