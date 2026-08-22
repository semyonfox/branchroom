// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unit"
	"gitea.dev/models/unittest"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentRevisionLeaseMaterialization(t *testing.T) {
	enableDevelopmentEnvironment(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	ensureTestRepositoryCodeUnit(ctx, t)
	owner := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}
	scope, err := createRepositoryEnvironmentScope(ctx, CreateRepositoryScopeRequest{
		RepoID:  1,
		Actor:   owner,
		Name:    "production-environment",
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)

	grants, err := branchroom_model.FindActiveGrantsAt(ctx, scope.ID, branchroom_model.ActionMaterialize, timeutil.TimeStampNow())
	require.NoError(t, err)
	require.Len(t, grants, 1)
	assert.Equal(t, owner.ID, grants[0].SubjectID)

	plaintextValue := "BRANCHROOM_TEST_SECRET=materialize-once"
	plaintext := []byte(plaintextValue)
	revision, err := createEnvironmentRevision(ctx, createEnvironmentRevisionRequest{
		RepoID:    1,
		ScopeID:   scope.ID,
		Actor:     owner,
		Plaintext: plaintext,
		RequestID: "revision-test",
	})
	require.NoError(t, err)
	assert.Equal(t, make([]byte, len(plaintext)), plaintext)
	assert.NotContains(t, string(revision.PayloadCiphertext), plaintextValue)
	assert.NotContains(t, string(revision.WrappedDEKCiphertext), plaintextValue)
	assert.Equal(t, int64(len(plaintextValue)), revision.PlaintextLength)
	ceiling, allowed, err := resolveDirectUserRepositoryCeiling(ctx, 1, owner)
	require.NoError(t, err)
	require.True(t, allowed)
	decision, err := NewDBEvaluator().Evaluate(ctx, Request{
		ScopeID:      scope.ID,
		RepoID:       1,
		Actor:        owner,
		Ceiling:      ceiling,
		Action:       branchroom_model.ActionMaterialize,
		ResourceType: branchroom_model.ResourceTypeEnvironment,
		ResourceID:   environmentRevisionResourceID(scope.ID, revision.RevisionKey),
		Surface:      branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	require.True(t, decision.Allowed, decision.ReasonCode)

	credential, err := issueEnvironmentLease(ctx, issueEnvironmentLeaseRequest{
		RepoID:      1,
		ScopeID:     scope.ID,
		RevisionKey: revision.RevisionKey,
		Actor:       owner,
		RequestID:   "lease-test",
	})
	require.NoError(t, err)
	require.Len(t, credential.BearerSecret, environmentLeaseBearerSecretLength)
	replaySecret := append([]byte(nil), credential.BearerSecret...)
	defer clearBytes(replaySecret)
	assert.NotEqual(t, hashEnvironmentLeaseSecret(credential.BearerSecret), string(credential.BearerSecret))

	rootPath := t.TempDir()
	root, err := os.OpenRoot(rootPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })
	require.NoError(t, materializeEnvironmentLeaseToRoot(ctx, root, materializeEnvironmentLeaseRequest{
		Credential:  *credential,
		Actor:       owner,
		Destination: "private/.env",
		RequestID:   "materialize-test",
	}))
	assert.Equal(t, make([]byte, len(credential.BearerSecret)), credential.BearerSecret)
	materialized, err := os.ReadFile(filepath.Join(rootPath, "private", ".env"))
	require.NoError(t, err)
	assert.Equal(t, plaintextValue, string(materialized))
	info, err := os.Stat(filepath.Join(rootPath, "private", ".env"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	storedLease, err := branchroom_model.GetEnvironmentLeaseByKey(ctx, credential.LeaseKey)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.EnvironmentLeaseStateCompleted, storedLease.State)
	assert.Equal(t, branchroom_model.EnvironmentLeaseOutcomeMaterialized, storedLease.Outcome)
	assert.NotEqual(t, hashEnvironmentLeaseSecret(replaySecret), replaySecret)

	err = materializeEnvironmentLeaseToRoot(ctx, root, materializeEnvironmentLeaseRequest{
		Credential:  environmentLeaseCredential{LeaseKey: credential.LeaseKey, BearerSecret: replaySecret},
		Actor:       owner,
		Destination: "private/replay.env",
		RequestID:   "replay-test",
	})
	assert.ErrorIs(t, err, util.ErrNotExist)
	assert.NoFileExists(t, filepath.Join(rootPath, "private", "replay.env"))

	events := make([]*branchroom_model.AuditEvent, 0)
	require.NoError(t, db.GetEngine(ctx).Where("scope_id = ?", scope.ID).Find(&events))
	for _, event := range events {
		assert.NotContains(t, event.ResourceID, plaintextValue)
		assert.NotContains(t, event.ReasonCode, plaintextValue)
		assert.NotContains(t, event.RequestID, plaintextValue)
	}
}

func TestEnvironmentMaterializationRevocationAndExpiry(t *testing.T) {
	enableDevelopmentEnvironment(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	ensureTestRepositoryCodeUnit(ctx, t)
	owner := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}
	scope, revision := createTestEnvironmentRevision(t, owner, "BRANCHROOM_TEST_SECRET=revoked")

	credential, err := issueEnvironmentLease(ctx, issueEnvironmentLeaseRequest{
		RepoID:      1,
		ScopeID:     scope.ID,
		RevisionKey: revision.RevisionKey,
		Actor:       owner,
		RequestID:   "revoked-lease",
	})
	require.NoError(t, err)
	materializeGrants, err := branchroom_model.FindActiveGrantsAt(ctx, scope.ID, branchroom_model.ActionMaterialize, timeutil.TimeStampNow())
	require.NoError(t, err)
	require.Len(t, materializeGrants, 1)
	require.NoError(t, branchroom_model.RevokeGrant(ctx, materializeGrants[0].ID, owner.ID, timeutil.TimeStampNow()))

	root, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })
	err = materializeEnvironmentLeaseToRoot(ctx, root, materializeEnvironmentLeaseRequest{
		Credential:  *credential,
		Actor:       owner,
		Destination: "denied.env",
		RequestID:   "revoked-materialize",
	})
	assert.ErrorIs(t, err, util.ErrNotExist)
	stored, err := branchroom_model.GetEnvironmentLeaseByKey(ctx, credential.LeaseKey)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.EnvironmentLeaseStateCompleted, stored.State)
	assert.Equal(t, branchroom_model.EnvironmentLeaseOutcomeDenied, stored.Outcome)

	newScope, newRevision := createTestEnvironmentRevision(t, owner, "BRANCHROOM_TEST_SECRET=expired")
	expiringCredential, err := issueEnvironmentLease(ctx, issueEnvironmentLeaseRequest{
		RepoID:      1,
		ScopeID:     newScope.ID,
		RevisionKey: newRevision.RevisionKey,
		Actor:       owner,
		RequestID:   "expired-lease",
	})
	require.NoError(t, err)
	expiringLease, err := branchroom_model.GetEnvironmentLeaseByKey(ctx, expiringCredential.LeaseKey)
	require.NoError(t, err)
	_, err = db.GetEngine(ctx).ID(expiringLease.ID).Cols("expires_unix").Update(&branchroom_model.EnvironmentLease{ExpiresUnix: timeutil.TimeStampNow()})
	require.NoError(t, err)

	err = materializeEnvironmentLeaseToRoot(ctx, root, materializeEnvironmentLeaseRequest{
		Credential:  *expiringCredential,
		Actor:       owner,
		Destination: "expired.env",
		RequestID:   "expired-materialize",
	})
	assert.ErrorIs(t, err, util.ErrNotExist)
	stored, err = branchroom_model.GetEnvironmentLeaseByKey(ctx, expiringCredential.LeaseKey)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.EnvironmentLeaseStateExpired, stored.State)
	assert.Equal(t, branchroom_model.EnvironmentLeaseOutcomeExpired, stored.Outcome)
}

func TestEnvironmentMaterializationRejectsTamperedCipherSuite(t *testing.T) {
	enableDevelopmentEnvironment(t)
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	ensureTestRepositoryCodeUnit(ctx, t)
	owner := Actor{Type: branchroom_model.ActorTypeUser, ID: 2}
	scope, revision := createTestEnvironmentRevision(t, owner, "BRANCHROOM_TEST_SECRET=tampered")
	credential, err := issueEnvironmentLease(ctx, issueEnvironmentLeaseRequest{
		RepoID:      1,
		ScopeID:     scope.ID,
		RevisionKey: revision.RevisionKey,
		Actor:       owner,
		RequestID:   "tampered-lease",
	})
	require.NoError(t, err)
	_, err = db.GetEngine(ctx).ID(revision.ID).Cols("cipher_suite").Update(&branchroom_model.EnvironmentRevision{CipherSuite: "unexpected"})
	require.NoError(t, err)

	rootPath := t.TempDir()
	root, err := os.OpenRoot(rootPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })
	err = materializeEnvironmentLeaseToRoot(ctx, root, materializeEnvironmentLeaseRequest{
		Credential:  *credential,
		Actor:       owner,
		Destination: "tampered.env",
		RequestID:   "tampered-materialize",
	})
	assert.ErrorIs(t, err, ErrEnvironmentMaterialization)
	assert.NoFileExists(t, filepath.Join(rootPath, "tampered.env"))
	stored, err := branchroom_model.GetEnvironmentLeaseByKey(ctx, credential.LeaseKey)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.EnvironmentLeaseStateCompleted, stored.State)
	assert.Equal(t, branchroom_model.EnvironmentLeaseOutcomeFailed, stored.Outcome)
}

func TestEnvironmentMaterializationRejectsUnsafeDestinations(t *testing.T) {
	for _, destination := range []string{
		"",
		".env/../secret",
		"../secret",
		"/absolute/secret",
		"C:/windows/secret",
		"nested\\secret",
		".git/config",
		"nested/.GIT/secret",
		"nested//secret",
	} {
		t.Run(destination, func(t *testing.T) {
			_, err := validateEnvironmentDestination(destination)
			assert.ErrorIs(t, err, util.ErrInvalidArgument)
		})
	}
	for _, destination := range []string{".env", "nested/.env", "nested/config.toml"} {
		t.Run(destination, func(t *testing.T) {
			actual, err := validateEnvironmentDestination(destination)
			require.NoError(t, err)
			assert.Equal(t, destination, actual)
		})
	}

	rootPath := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.env")
	require.NoError(t, os.Symlink(filepath.Dir(outside), filepath.Join(rootPath, "escaped")))
	root, err := os.OpenRoot(rootPath)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })
	err = writeEnvironmentMaterialization(root, "escaped/outside.env", []byte("value"))
	assert.Error(t, err)
	assert.NoFileExists(t, outside)
}

func createTestEnvironmentRevision(t *testing.T, owner Actor, plaintextValue string) (*branchroom_model.Scope, *branchroom_model.EnvironmentRevision) {
	t.Helper()
	scope, err := createRepositoryEnvironmentScope(t.Context(), CreateRepositoryScopeRequest{
		RepoID:  1,
		Actor:   owner,
		Name:    "environment-" + plaintextValue[len("BRANCHROOM_TEST_SECRET="):],
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	plaintext := []byte(plaintextValue)
	revision, err := createEnvironmentRevision(t.Context(), createEnvironmentRevisionRequest{
		RepoID:    1,
		ScopeID:   scope.ID,
		Actor:     owner,
		Plaintext: plaintext,
	})
	require.NoError(t, err)
	assert.Equal(t, make([]byte, len(plaintext)), plaintext)
	return scope, revision
}

func enableDevelopmentEnvironment(t *testing.T) {
	t.Helper()
	enableBranchroom(t)
	restoreEnvironment := test.MockVariableValue(&setting.BranchroomEnvironment)
	t.Cleanup(restoreEnvironment)
	restoreProduction := test.MockVariableValue(&setting.IsProd)
	t.Cleanup(restoreProduction)

	key := bytes.Repeat([]byte{7}, 32)
	keyFile := filepath.Join(t.TempDir(), "branchroom-development-key")
	require.NoError(t, os.WriteFile(keyFile, []byte(base64.StdEncoding.EncodeToString(key)), 0o600))
	clearBytes(key)
	setting.IsProd = false
	setting.BranchroomEnvironment = setting.BranchroomEnvironmentConfig{
		Enabled:                true,
		Provider:               "development-file",
		DevelopmentKeyFilePath: keyFile,
		MaxRevisionBytes:       64 * 1024,
		LeaseDuration:          5 * time.Minute,
	}
}

func ensureTestRepositoryCodeUnit(ctx context.Context, t *testing.T) {
	t.Helper()
	exists, err := db.GetEngine(ctx).Where("repo_id = ? AND type = ?", 1, unit.TypeCode).Exist(new(repo_model.RepoUnit))
	require.NoError(t, err)
	if exists {
		return
	}
	require.NoError(t, db.Insert(ctx, &repo_model.RepoUnit{
		RepoID: 1,
		Type:   unit.TypeCode,
		Config: &repo_model.UnitConfig{},
	}))
}
