// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/private"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/timeutil"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyClaimedReceiveAdmitsAuthorizedPush(t *testing.T) {
	scope, workspace, session, bearerSecret, quarantine := prepareReceiveVerifierFixture(t)
	updates := receiveVerifierAdmissibleUpdates(t, workspace, quarantine)

	suppliedSecret := bytes.Clone(bearerSecret)
	err := VerifyReceiveSessionPush(t.Context(), VerifyReceiveSessionPushRequest{
		SessionKey:   session.SessionKey,
		BearerSecret: suppliedSecret,
		Updates:      updates,
		Quarantine:   quarantine,
	})
	require.NoError(t, err)
	assert.Equal(t, make([]byte, len(suppliedSecret)), suppliedSecret)

	decisions, audits := receiveVerifierPolicyCounts(t, scope.ID)
	assert.Zero(t, decisions)
	assert.Zero(t, audits)

	stored, err := branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomeAdmitted, stored.Outcome)
	assert.Equal(t, session.ClaimedUnix, stored.ClaimedUnix)
	assert.Equal(t, receiveVerifierBearerHash(bearerSecret), stored.BearerSecretHash)
}

func TestVerifyClaimedReceiveAdmitsAfterPostClaimRevocation(t *testing.T) {
	scope, workspace, session, bearerSecret, quarantine := prepareReceiveVerifierFixture(t)
	grant := insertWorkspaceActionGrant(t, scope.ID, session.ActorID, branchroom_model.ActionWriteSource, 0, 0)
	require.NoError(t, branchroom_model.RevokeGrant(t.Context(), grant.ID, session.ActorID, timeutil.TimeStampNow()))
	updates := receiveVerifierAdmissibleUpdates(t, workspace, quarantine)

	err := VerifyReceiveSessionPush(t.Context(), VerifyReceiveSessionPushRequest{
		SessionKey:   session.SessionKey,
		BearerSecret: bytes.Clone(bearerSecret),
		Updates:      updates,
		Quarantine:   quarantine,
	})
	require.NoError(t, err)

	stored, err := branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomeAdmitted, stored.Outcome)
	assert.ErrorIs(t, VerifyReceiveSessionPush(t.Context(), VerifyReceiveSessionPushRequest{
		SessionKey:   session.SessionKey,
		BearerSecret: bytes.Clone(bearerSecret),
		Updates:      updates,
		Quarantine:   quarantine,
	}), errReceiveDenied)
	decisions, audits := receiveVerifierPolicyCounts(t, scope.ID)
	assert.Zero(t, decisions)
	assert.Zero(t, audits)
}

// Claim time enforces the expiry bound. A receive already claimed before expiry
// can finish admission after the nominal expiry time.
func TestVerifyClaimedReceiveAdmitsAfterExpiry(t *testing.T) {
	now := timeutil.TimeStampNow()
	scope, workspace, session, bearerSecret, quarantine := prepareReceiveVerifierFixtureAt(t, now-1200, now-600, now-700)
	require.Less(t, int64(session.ClaimedUnix), int64(session.ExpiresUnix))
	insertWorkspaceActionGrant(t, scope.ID, session.ActorID, branchroom_model.ActionWriteSource, 0, 0)

	err := VerifyReceiveSessionPush(t.Context(), VerifyReceiveSessionPushRequest{
		SessionKey:   session.SessionKey,
		BearerSecret: bytes.Clone(bearerSecret),
		Updates:      receiveVerifierAdmissibleUpdates(t, workspace, quarantine),
		Quarantine:   quarantine,
	})
	require.NoError(t, err)

	stored, err := branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomeAdmitted, stored.Outcome)
}

func TestVerifyClaimedReceiveAdmitsAfterScopeDisabled(t *testing.T) {
	scope, workspace, session, bearerSecret, quarantine := prepareReceiveVerifierFixture(t)
	_, err := db.GetEngine(t.Context()).ID(scope.ID).Cols("state").Update(&branchroom_model.Scope{State: branchroom_model.ScopeStateDisabled})
	require.NoError(t, err)

	err = VerifyReceiveSessionPush(t.Context(), VerifyReceiveSessionPushRequest{
		SessionKey:   session.SessionKey,
		BearerSecret: bytes.Clone(bearerSecret),
		Updates:      receiveVerifierAdmissibleUpdates(t, workspace, quarantine),
		Quarantine:   quarantine,
	})
	require.NoError(t, err)

	stored, err := branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomeAdmitted, stored.Outcome)
	decisions, audits := receiveVerifierPolicyCounts(t, scope.ID)
	assert.Zero(t, decisions)
	assert.Zero(t, audits)
}

func TestVerifyClaimedReceiveAdmitsAfterWorkspaceRetired(t *testing.T) {
	scope, workspace, session, bearerSecret, quarantine := prepareReceiveVerifierFixture(t)
	retired, err := branchroom_model.TransitionWorkspace(t.Context(), workspace.ID, branchroom_model.WorkspaceStateReady, branchroom_model.WorkspaceStateRetired, "", timeutil.TimeStampNow())
	require.NoError(t, err)
	require.True(t, retired)

	err = VerifyReceiveSessionPush(t.Context(), VerifyReceiveSessionPushRequest{
		SessionKey:   session.SessionKey,
		BearerSecret: bytes.Clone(bearerSecret),
		Updates:      receiveVerifierAdmissibleUpdates(t, workspace, quarantine),
		Quarantine:   quarantine,
	})
	require.NoError(t, err)

	stored, err := branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomeAdmitted, stored.Outcome)
	decisions, audits := receiveVerifierPolicyCounts(t, scope.ID)
	assert.Zero(t, decisions)
	assert.Zero(t, audits)
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

func TestVerifyClaimedReceiveRejectsMissingReachableTree(t *testing.T) {
	scope, workspace, session, bearerSecret, quarantine := prepareReceiveVerifierFixture(t)

	err := VerifyReceiveSessionPush(t.Context(), VerifyReceiveSessionPushRequest{
		SessionKey:   session.SessionKey,
		BearerSecret: bytes.Clone(bearerSecret),
		Updates:      receiveVerifierMissingTreeUpdates(t, workspace, quarantine),
		Quarantine:   quarantine,
	})
	assert.ErrorIs(t, err, errReceiveDenied)

	stored, err := branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomeDenied, stored.Outcome)
	decisions, audits := receiveVerifierPolicyCounts(t, scope.ID)
	assert.Zero(t, decisions)
	assert.Zero(t, audits)
}

func TestFinalizeReceiveAdmissionFailsOnlyPendingClaim(t *testing.T) {
	_, _, session, bearerSecret, _ := prepareReceiveVerifierFixture(t)
	credential := &ReceiveAdmissionCredential{SessionKey: session.SessionKey, BearerSecret: bytes.Clone(bearerSecret)}

	FinalizeReceiveAdmission(t.Context(), credential)
	stored, err := branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateCompleted, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomeFailed, stored.Outcome)

	FinalizeReceiveAdmission(t.Context(), credential)
	stored, err = branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomeFailed, stored.Outcome)

	_, _, pendingSession, _, _ := prepareReceiveVerifierFixture(t)
	FinalizeReceiveAdmission(t.Context(), &ReceiveAdmissionCredential{SessionKey: pendingSession.SessionKey, BearerSecret: []byte("wrong-receive-admission-secret")})
	stored, err = branchroom_model.GetReceiveSessionByID(t.Context(), pendingSession.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionStateClaimed, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomePending, stored.Outcome)

	_, workspace, session, bearerSecret, quarantine := prepareReceiveVerifierFixture(t)
	err = VerifyReceiveSessionPush(t.Context(), VerifyReceiveSessionPushRequest{
		SessionKey:   session.SessionKey,
		BearerSecret: bytes.Clone(bearerSecret),
		Updates:      receiveVerifierAdmissibleUpdates(t, workspace, quarantine),
		Quarantine:   quarantine,
	})
	require.NoError(t, err)
	FinalizeReceiveAdmission(t.Context(), &ReceiveAdmissionCredential{SessionKey: session.SessionKey, BearerSecret: bytes.Clone(bearerSecret)})
	stored, err = branchroom_model.GetReceiveSessionByID(t.Context(), session.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomeAdmitted, stored.Outcome)
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
	storePath := filepath.Join(setting.Branchroom.RestrictedRepoRootPath, workspace.StoreID+".git")
	require.NoError(t, os.Chmod(setting.Branchroom.RestrictedRepoRootPath, 0o700))
	require.NoError(t, os.Chmod(storePath, 0o755))
	require.NoError(t, os.Chmod(filepath.Join(storePath, "config"), 0o644))

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

// prepareReceiveVerifierQuarantine builds a real safe quarantine layout below the
// provisioned restricted store. 0755 keeps every level integrity-safe.
func prepareReceiveVerifierQuarantine(t *testing.T, storeID string) branchroom_module.ReceiveQuarantineValues {
	t.Helper()

	root := setting.Branchroom.RestrictedRepoRootPath
	storePath := filepath.Join(root, storeID+".git")
	objectsPath := filepath.Join(storePath, "objects")
	quarantinePath := filepath.Join(objectsPath, receiveVerifierQuarantineEntry)
	require.NoError(t, os.Mkdir(quarantinePath, 0o755))
	// Pin the private root and its child directories after creation so integrity does
	// not depend on the process umask.
	require.NoError(t, os.Chmod(root, 0o700))
	for _, dir := range []string{storePath, objectsPath, quarantinePath} {
		require.NoError(t, os.Chmod(dir, 0o755))
	}

	return branchroom_module.ReceiveQuarantineValues{
		ObjectDirectory:            quarantinePath,
		AlternateObjectDirectories: objectsPath,
		QuarantinePath:             quarantinePath,
	}
}

func receiveVerifierAdmissibleUpdates(t *testing.T, workspace *branchroom_model.Workspace, quarantine branchroom_module.ReceiveQuarantineValues) []byte {
	t.Helper()
	restricted, err := branchroom_module.RestrictedRepository(workspace.StoreID)
	require.NoError(t, err)
	environ := append(gitcmd.CommonGitCmdEnvs(),
		private.GitObjectDirectory+"="+quarantine.ObjectDirectory,
		private.GitAlternativeObjectDirectories+"="+quarantine.AlternateObjectDirectories,
		private.GitQuarantinePath+"="+quarantine.QuarantinePath,
	)
	treeOID, _, err := gitcmd.NewCommand("rev-parse").
		AddDynamicArguments(workspace.PublicBaseSHA + "^{tree}").
		WithEnv(environ).
		WithRepo(restricted).
		RunStdString(t.Context())
	require.NoError(t, err)
	commitOID, _, err := gitcmd.NewCommand("commit-tree").
		AddConfig("user.name", "Branchroom Test").
		AddConfig("user.email", "branchroom-test@example.invalid").
		AddDynamicArguments(strings.TrimSpace(treeOID)).
		AddArguments("-p").
		AddDynamicArguments(workspace.PublicBaseSHA).
		WithEnv(environ).
		WithRepo(restricted).
		WithStdinBytes([]byte("Branchroom test commit\n")).
		RunStdString(t.Context())
	require.NoError(t, err)
	commitOID = strings.TrimSpace(commitOID)
	return []byte(strings.Repeat("0", len(commitOID)) + " " + commitOID + " refs/heads/branchroom/main\n")
}

func receiveVerifierMissingTreeUpdates(t *testing.T, workspace *branchroom_model.Workspace, quarantine branchroom_module.ReceiveQuarantineValues) []byte {
	t.Helper()
	restricted, err := branchroom_module.RestrictedRepository(workspace.StoreID)
	require.NoError(t, err)
	environ := append(gitcmd.CommonGitCmdEnvs(),
		private.GitObjectDirectory+"="+quarantine.ObjectDirectory,
		private.GitAlternativeObjectDirectories+"="+quarantine.AlternateObjectDirectories,
		private.GitQuarantinePath+"="+quarantine.QuarantinePath,
	)
	missingTree := strings.Repeat("0", len(workspace.PublicBaseSHA))
	commit := fmt.Sprintf("tree %s\nparent %s\nauthor Branchroom Test <branchroom-test@example.invalid> 1 +0000\ncommitter Branchroom Test <branchroom-test@example.invalid> 1 +0000\n\nmissing tree\n", missingTree, workspace.PublicBaseSHA)
	commitOID, _, err := gitcmd.NewCommand("hash-object", "-w", "-t", "commit", "--stdin").
		WithEnv(environ).
		WithRepo(restricted).
		WithStdinBytes([]byte(commit)).
		RunStdString(t.Context())
	require.NoError(t, err)
	commitOID = strings.TrimSpace(commitOID)
	return []byte(strings.Repeat("0", len(commitOID)) + " " + commitOID + " refs/heads/branchroom/missing-tree\n")
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
