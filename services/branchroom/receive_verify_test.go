// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	"gitea.dev/modules/timeutil"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyClaimedReceiveUsesWriteSourceDeny(t *testing.T) {
	scope, workspace, session, bearerSecret, quarantine := prepareReceiveVerifierFixture(t)
	insertWorkspaceActionGrant(t, scope.ID, session.ActorID, branchroom_model.ActionWriteSource, 0, 0)

	suppliedSecret := bytes.Clone(bearerSecret)
	err := VerifyReceiveSessionPush(t.Context(), VerifyReceiveSessionPushRequest{
		SessionKey:   session.SessionKey,
		BearerSecret: suppliedSecret,
		Updates:      receiveVerifierUpdates("refs/heads/branchroom/main"),
		Quarantine:   quarantine,
	})
	assert.ErrorIs(t, err, errReceiveDenied)
	assert.Equal(t, make([]byte, len(suppliedSecret)), suppliedSecret)

	decision := new(branchroom_model.Decision)
	has, err := db.GetEngine(t.Context()).Where("scope_id = ? AND action = ?", scope.ID, branchroom_model.ActionWriteSource).Get(decision)
	require.NoError(t, err)
	require.True(t, has)
	assert.Equal(t, scope.ID, decision.ScopeID)
	assert.Equal(t, session.RepoID, decision.RepoID)
	assert.Equal(t, int64(0), decision.GrantID)
	assert.Equal(t, branchroom_model.ActorTypeUser, decision.ActorType)
	assert.Equal(t, session.ActorID, decision.ActorID)
	assert.Equal(t, branchroom_model.ActionWriteSource, decision.Action)
	assert.Equal(t, branchroom_model.ResourceTypeWorkspace, decision.ResourceType)
	assert.Equal(t, workspaceResourceID(workspace.WorkspaceKey), decision.ResourceID)
	assert.Equal(t, branchroom_model.SurfaceGitHTTP, decision.Surface)
	assert.Equal(t, branchroom_model.DecisionResultDeny, decision.Result)
	assert.Equal(t, ReasonWriteSourceUnavailable, decision.ReasonCode)
	assert.True(t, decision.OrdinaryAllowed)

	audit := new(branchroom_model.AuditEvent)
	has, err = db.GetEngine(t.Context()).Where("decision_key = ?", decision.DecisionKey).Get(audit)
	require.NoError(t, err)
	require.True(t, has)
	assert.Equal(t, decision.DecisionKey, audit.DecisionKey)
	assert.Equal(t, decision.Action, audit.Action)
	assert.Equal(t, decision.ResourceID, audit.ResourceID)
	assert.Equal(t, decision.Surface, audit.Surface)
	assert.Equal(t, decision.ReasonCode, audit.ReasonCode)
	assert.Equal(t, decision.GrantID, audit.GrantID)

	stored, err := branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomeDenied, stored.Outcome)
	assert.Equal(t, session.ClaimedUnix, stored.ClaimedUnix)
	assert.Equal(t, receiveVerifierBearerHash(bearerSecret), stored.BearerSecretHash)
}

func TestVerifyClaimedReceiveFinalizesDeniedOnce(t *testing.T) {
	scope, _, session, bearerSecret, quarantine := prepareReceiveVerifierFixture(t)
	insertWorkspaceActionGrant(t, scope.ID, session.ActorID, branchroom_model.ActionWriteSource, 0, 0)

	for i := range 2 {
		err := VerifyReceiveSessionPush(t.Context(), VerifyReceiveSessionPushRequest{
			SessionKey:   session.SessionKey,
			BearerSecret: bytes.Clone(bearerSecret),
			Updates:      receiveVerifierUpdates("refs/heads/branchroom/main"),
			Quarantine:   quarantine,
		})
		assert.ErrorIs(t, err, errReceiveDenied)

		stored, err := branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
		require.NoError(t, err)
		assert.Equal(t, branchroom_model.ReceiveSessionStateCompleted, stored.State)
		assert.Equal(t, branchroom_model.ReceiveSessionOutcomeDenied, stored.Outcome)
		decisions, audits := receiveVerifierPolicyCounts(t, scope.ID)
		assert.Equal(t, int64(1), decisions, "verification %d must not add a second decision", i)
		assert.Equal(t, int64(1), audits, "verification %d must not add a second audit", i)
	}
}

// A claimed session whose nominal expiry passed while claimed still finalizes denied because
// claim time already enforced the expiry bound. It never authorizes a push.
func TestVerifyClaimedReceiveFinalizesDeniedAfterExpiry(t *testing.T) {
	now := timeutil.TimeStampNow()
	scope, _, session, bearerSecret, quarantine := prepareReceiveVerifierFixtureAt(t, now-1200, now-600, now-700)
	require.Less(t, int64(session.ClaimedUnix), int64(session.ExpiresUnix))
	insertWorkspaceActionGrant(t, scope.ID, session.ActorID, branchroom_model.ActionWriteSource, 0, 0)

	err := VerifyReceiveSessionPush(t.Context(), VerifyReceiveSessionPushRequest{
		SessionKey:   session.SessionKey,
		BearerSecret: bytes.Clone(bearerSecret),
		Updates:      receiveVerifierUpdates("refs/heads/branchroom/main"),
		Quarantine:   quarantine,
	})
	assert.ErrorIs(t, err, errReceiveDenied)

	stored, err := branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomeDenied, stored.Outcome)
}

func TestVerifyClaimedReceiveCompletesDeniedOnDisabledScopeWithoutAudit(t *testing.T) {
	scope, _, session, bearerSecret, quarantine := prepareReceiveVerifierFixture(t)
	insertWorkspaceActionGrant(t, scope.ID, session.ActorID, branchroom_model.ActionWriteSource, 0, 0)
	_, err := db.GetEngine(t.Context()).ID(scope.ID).Cols("state").Update(&branchroom_model.Scope{State: branchroom_model.ScopeStateDisabled})
	require.NoError(t, err)
	decisionsBefore, auditsBefore := receiveVerifierPolicyCounts(t, scope.ID)

	err = VerifyReceiveSessionPush(t.Context(), VerifyReceiveSessionPushRequest{
		SessionKey:   session.SessionKey,
		BearerSecret: bytes.Clone(bearerSecret),
		Updates:      receiveVerifierUpdates("refs/heads/branchroom/main"),
		Quarantine:   quarantine,
	})
	assert.ErrorIs(t, err, errReceiveDenied)

	stored, err := branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomeDenied, stored.Outcome)
	decisionsAfter, auditsAfter := receiveVerifierPolicyCounts(t, scope.ID)
	assert.Equal(t, decisionsBefore, decisionsAfter)
	assert.Equal(t, auditsBefore, auditsAfter)
}

func TestVerifyClaimedReceiveCompletesDeniedOnRetiredWorkspaceWithoutAudit(t *testing.T) {
	scope, workspace, session, bearerSecret, quarantine := prepareReceiveVerifierFixture(t)
	insertWorkspaceActionGrant(t, scope.ID, session.ActorID, branchroom_model.ActionWriteSource, 0, 0)
	retired, err := branchroom_model.TransitionWorkspace(t.Context(), workspace.ID, branchroom_model.WorkspaceStateReady, branchroom_model.WorkspaceStateRetired, "", timeutil.TimeStampNow())
	require.NoError(t, err)
	require.True(t, retired)
	decisionsBefore, auditsBefore := receiveVerifierPolicyCounts(t, scope.ID)

	err = VerifyReceiveSessionPush(t.Context(), VerifyReceiveSessionPushRequest{
		SessionKey:   session.SessionKey,
		BearerSecret: bytes.Clone(bearerSecret),
		Updates:      receiveVerifierUpdates("refs/heads/branchroom/main"),
		Quarantine:   quarantine,
	})
	assert.ErrorIs(t, err, errReceiveDenied)

	stored, err := branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomeDenied, stored.Outcome)
	decisionsAfter, auditsAfter := receiveVerifierPolicyCounts(t, scope.ID)
	assert.Equal(t, decisionsBefore, decisionsAfter)
	assert.Equal(t, auditsAfter, auditsBefore)
}

func TestVerifyClaimedReceiveRejectsMalformedUpdatesWithoutMutation(t *testing.T) {
	scope, _, session, bearerSecret, quarantine := prepareReceiveVerifierFixture(t)
	decisionsBefore, auditsBefore := receiveVerifierPolicyCounts(t, scope.ID)

	for name, updates := range map[string][]byte{
		"garbage":       []byte("not-a-receive-update"),
		"invalid ref":   receiveVerifierUpdates("refs/heads/main"),
		"empty records": []byte("\n\n"),
	} {
		err := VerifyReceiveSessionPush(t.Context(), VerifyReceiveSessionPushRequest{
			SessionKey:   session.SessionKey,
			BearerSecret: bytes.Clone(bearerSecret),
			Updates:      updates,
			Quarantine:   quarantine,
		})
		assert.ErrorIs(t, err, errReceiveDenied, name)
	}

	decisionsAfter, auditsAfter := receiveVerifierPolicyCounts(t, scope.ID)
	assert.Equal(t, decisionsBefore, decisionsAfter)
	assert.Equal(t, auditsBefore, auditsAfter)
	stored, err := branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateClaimed, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomePending, stored.Outcome)
}

func TestVerifyClaimedReceiveRejectsWrongBearerWithoutAudit(t *testing.T) {
	scope, _, session, _, quarantine := prepareReceiveVerifierFixture(t)
	decisionsBefore, auditsBefore := receiveVerifierPolicyCounts(t, scope.ID)
	suppliedSecret := []byte("wrong-receive-verifier-secret")

	for name, values := range map[string]branchroom_module.ReceiveQuarantineValues{
		"valid quarantine":   quarantine,
		"invalid quarantine": {},
	} {
		supplied := bytes.Clone(suppliedSecret)
		err := VerifyReceiveSessionPush(t.Context(), VerifyReceiveSessionPushRequest{
			SessionKey:   session.SessionKey,
			BearerSecret: supplied,
			Updates:      receiveVerifierUpdates("refs/heads/branchroom/main"),
			Quarantine:   values,
		})
		assert.ErrorIs(t, err, errReceiveDenied, name)
		assert.Equal(t, make([]byte, len(supplied)), supplied)
	}

	decisionsAfter, auditsAfter := receiveVerifierPolicyCounts(t, scope.ID)
	assert.Equal(t, decisionsBefore, decisionsAfter)
	assert.Equal(t, auditsBefore, auditsAfter)

	stored, err := branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateClaimed, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomePending, stored.Outcome)
	assert.Equal(t, session.ClaimedUnix, stored.ClaimedUnix)
}

// A correctly authenticated claimed session whose quarantine facts are missing or invalid
// terminates denied in the same transaction with no evaluator decision or audit row.
func TestVerifyClaimedReceiveCompletesDeniedOnInvalidQuarantineWithoutAudit(t *testing.T) {
	for name, mutate := range map[string]func(t *testing.T, v *branchroom_module.ReceiveQuarantineValues){
		"missing": func(_ *testing.T, v *branchroom_module.ReceiveQuarantineValues) {
			*v = branchroom_module.ReceiveQuarantineValues{}
		},
		"partial": func(_ *testing.T, v *branchroom_module.ReceiveQuarantineValues) { v.QuarantinePath = "" },
		"wrong store": func(_ *testing.T, v *branchroom_module.ReceiveQuarantineValues) {
			v.AlternateObjectDirectories += "-other"
		},
		"relative path": func(_ *testing.T, v *branchroom_module.ReceiveQuarantineValues) {
			v.ObjectDirectory = filepath.Base(v.ObjectDirectory)
		},
		"missing on disk": func(t *testing.T, v *branchroom_module.ReceiveQuarantineValues) {
			require.NoError(t, os.Remove(v.QuarantinePath))
			require.NoError(t, os.Remove(filepath.Dir(v.QuarantinePath)))
		},
	} {
		t.Run(name, func(t *testing.T) {
			scope, _, session, bearerSecret, quarantine := prepareReceiveVerifierFixture(t)
			insertWorkspaceActionGrant(t, scope.ID, session.ActorID, branchroom_model.ActionWriteSource, 0, 0)

			values := quarantine
			mutate(t, &values)

			err := VerifyReceiveSessionPush(t.Context(), VerifyReceiveSessionPushRequest{
				SessionKey:   session.SessionKey,
				BearerSecret: bytes.Clone(bearerSecret),
				Updates:      receiveVerifierUpdates("refs/heads/branchroom/main"),
				Quarantine:   values,
			})
			assert.ErrorIs(t, err, errReceiveDenied)

			stored, err := branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
			require.NoError(t, err)
			assert.Equal(t, branchroom_model.ReceiveSessionStateCompleted, stored.State)
			assert.Equal(t, branchroom_model.ReceiveSessionOutcomeDenied, stored.Outcome)
			decisions, audits := receiveVerifierPolicyCounts(t, scope.ID)
			assert.Zero(t, decisions)
			assert.Zero(t, audits)
		})
	}
}

// An unknown session key denies before any row read, so the real fixture session stays
// untouched even though its actor holds a valid grant and the quarantine facts are real.
func TestVerifyReceiveUnknownSessionKeyStaysInert(t *testing.T) {
	scope, _, session, _, quarantine := prepareReceiveVerifierFixture(t)
	insertWorkspaceActionGrant(t, scope.ID, session.ActorID, branchroom_model.ActionWriteSource, 0, 0)
	decisionsBefore, auditsBefore := receiveVerifierPolicyCounts(t, scope.ID)
	unknownKey := uuid.NewString()
	require.NotEqual(t, session.SessionKey, unknownKey)

	suppliedSecret := bytes.Clone([]byte("receive-verifier-unknown-key-secret"))
	err := VerifyReceiveSessionPush(t.Context(), VerifyReceiveSessionPushRequest{
		SessionKey:   unknownKey,
		BearerSecret: suppliedSecret,
		Updates:      receiveVerifierUpdates("refs/heads/branchroom/main"),
		Quarantine:   quarantine,
	})
	assert.ErrorIs(t, err, errReceiveDenied)
	assert.Equal(t, make([]byte, len(suppliedSecret)), suppliedSecret)

	stored, err := branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateClaimed, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomePending, stored.Outcome)
	assert.Equal(t, session.ClaimedUnix, stored.ClaimedUnix)

	decisionsAfter, auditsAfter := receiveVerifierPolicyCounts(t, scope.ID)
	assert.Equal(t, decisionsBefore, decisionsAfter)
	assert.Equal(t, auditsBefore, auditsAfter)
}

const receiveVerifierQuarantineEntry = "incoming-quarantine"

func prepareReceiveVerifierFixture(t *testing.T) (*branchroom_model.Scope, *branchroom_model.Workspace, *branchroom_model.ReceiveSession, []byte, branchroom_module.ReceiveQuarantineValues) {
	t.Helper()

	now := timeutil.TimeStampNow()
	return prepareReceiveVerifierFixtureAt(t, now-60, now+600, now)
}

func prepareReceiveVerifierFixtureAt(t *testing.T, issuedUnix, expiresUnix, claimedUnix timeutil.TimeStamp) (*branchroom_model.Scope, *branchroom_model.Workspace, *branchroom_model.ReceiveSession, []byte, branchroom_module.ReceiveQuarantineValues) {
	t.Helper()

	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	ensureTestRepositoryCodeUnit(t.Context(), t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "receive-verifier")
	workspace, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       scope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)

	bearerSecret := []byte("receive-verifier-claimed-secret")
	session := &branchroom_model.ReceiveSession{
		SessionKey:       uuid.NewString(),
		BearerSecretHash: receiveVerifierBearerHash(bearerSecret),
		ScopeID:          scope.ID,
		WorkspaceID:      workspace.ID,
		RepoID:           repository.ID,
		ActorType:        branchroom_model.ActorTypeUser,
		ActorID:          repository.OwnerID,
		State:            branchroom_model.ReceiveSessionStateClaimed,
		Outcome:          branchroom_model.ReceiveSessionOutcomePending,
		IssuedUnix:       issuedUnix,
		ExpiresUnix:      expiresUnix,
		ClaimedUnix:      claimedUnix,
	}
	require.NoError(t, branchroom_model.InsertReceiveSession(t.Context(), session))
	return scope, workspace, session, bearerSecret, prepareReceiveVerifierQuarantine(t, workspace.StoreID)
}

// prepareReceiveVerifierQuarantine builds a real safe quarantine layout for one restricted
// store and pins it as the configured restricted root; 0755 keeps every level integrity-safe.
func prepareReceiveVerifierQuarantine(t *testing.T, storeID string) branchroom_module.ReceiveQuarantineValues {
	t.Helper()

	root := t.TempDir()
	storePath := filepath.Join(root, storeID+".git")
	objectsPath := filepath.Join(storePath, "objects")
	quarantinePath := filepath.Join(objectsPath, receiveVerifierQuarantineEntry)
	for _, dir := range []string{storePath, objectsPath, quarantinePath} {
		require.NoError(t, os.Mkdir(dir, 0o755))
	}
	// pin every level to 0755 after creation so integrity does not depend on the process umask
	for _, dir := range []string{root, storePath, objectsPath, quarantinePath} {
		require.NoError(t, os.Chmod(dir, 0o755))
	}
	reset := test.MockVariableValue(&setting.Branchroom.RestrictedRepoRootPath, root)
	t.Cleanup(reset)

	return branchroom_module.ReceiveQuarantineValues{
		ObjectDirectory:            quarantinePath,
		AlternateObjectDirectories: objectsPath,
		QuarantinePath:             quarantinePath,
	}
}

func receiveVerifierUpdates(ref string) []byte {
	return []byte(strings.Repeat("0", 40) + " " + strings.Repeat("a", 40) + " " + ref + "\n")
}

func receiveVerifierBearerHash(secret []byte) string {
	sum := sha256.Sum256(secret)
	return hex.EncodeToString(sum[:])
}

func receiveVerifierPolicyCounts(t *testing.T, scopeID int64) (int64, int64) {
	t.Helper()

	decisions, err := db.GetEngine(t.Context()).Where("scope_id = ? AND action = ?", scopeID, branchroom_model.ActionWriteSource).Count(new(branchroom_model.Decision))
	require.NoError(t, err)
	audits, err := db.GetEngine(t.Context()).Where("scope_id = ? AND action = ?", scopeID, branchroom_model.ActionWriteSource).Count(new(branchroom_model.AuditEvent))
	require.NoError(t, err)
	return decisions, audits
}
