// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/timeutil"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIssueReceiveAdmissionIssuesClaimedSession(t *testing.T) {
	scope, workspace := prepareReceiveAdmissionFixture(t, "receive-admission-success")
	insertWorkspaceActionGrant(t, scope.ID, scope.OwnerID, branchroom_model.ActionWriteSource, 0, 0)

	credential, err := IssueReceiveAdmission(t.Context(), IssueReceiveAdmissionRequest{
		RepoID:   scope.RepoID,
		RouteKey: workspace.RouteKey,
		UserID:   scope.OwnerID,
	})
	require.NoError(t, err)
	require.NotNil(t, credential)

	parsed, err := uuid.Parse(credential.SessionKey)
	require.NoError(t, err)
	assert.Equal(t, credential.SessionKey, parsed.String())
	assert.Len(t, credential.BearerSecret, receiveAdmissionBearerLen)

	stored, err := branchroom_model.GetReceiveSessionByKey(t.Context(), credential.SessionKey)
	require.NoError(t, err)
	bearerHash := sha256.Sum256(credential.BearerSecret)
	assert.Equal(t, hex.EncodeToString(bearerHash[:]), stored.BearerSecretHash)
	assert.Equal(t, scope.ID, stored.ScopeID)
	assert.Equal(t, workspace.ID, stored.WorkspaceID)
	assert.Equal(t, scope.RepoID, stored.RepoID)
	assert.Equal(t, branchroom_model.ActorTypeUser, stored.ActorType)
	assert.Equal(t, scope.OwnerID, stored.ActorID)
	assert.Equal(t, branchroom_model.ReceiveSessionStateClaimed, stored.State)
	assert.Equal(t, branchroom_model.ReceiveSessionOutcomePending, stored.Outcome)
	assert.NotZero(t, stored.ClaimedUnix)

	decisions, audits := receiveVerifierPolicyCounts(t, scope.ID)
	assert.Zero(t, decisions)
	assert.Zero(t, audits)
}

func TestIssueReceiveAdmissionDenials(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		withGrant bool
		mutate    func(t *testing.T, scope *branchroom_model.Scope, workspace *branchroom_model.Workspace, request *IssueReceiveAdmissionRequest)
	}{
		{
			name:      "malformed route key",
			withGrant: true,
			mutate: func(_ *testing.T, _ *branchroom_model.Scope, _ *branchroom_model.Workspace, request *IssueReceiveAdmissionRequest) {
				request.RouteKey = "not-a-workspace-route-key"
			},
		},
		{
			name:      "unknown route key",
			withGrant: true,
			mutate: func(t *testing.T, _ *branchroom_model.Scope, _ *branchroom_model.Workspace, request *IssueReceiveAdmissionRequest) {
				routeKey, err := branchroom_model.NewWorkspaceRouteKey()
				require.NoError(t, err)
				request.RouteKey = routeKey
			},
		},
		{
			name:      "wrong repository",
			withGrant: true,
			mutate: func(_ *testing.T, scope *branchroom_model.Scope, _ *branchroom_model.Workspace, request *IssueReceiveAdmissionRequest) {
				request.RepoID = scope.RepoID + 1
			},
		},
		{
			name:      "missing grant",
			withGrant: false,
		},
		{
			name:      "revoked grant",
			withGrant: false,
			mutate: func(t *testing.T, scope *branchroom_model.Scope, _ *branchroom_model.Workspace, _ *IssueReceiveAdmissionRequest) {
				grant := insertWorkspaceActionGrant(t, scope.ID, scope.OwnerID, branchroom_model.ActionWriteSource, 0, 0)
				require.NoError(t, branchroom_model.RevokeGrant(t.Context(), grant.ID, scope.OwnerID, timeutil.TimeStampNow()))
			},
		},
		{
			name:      "disabled scope",
			withGrant: true,
			mutate: func(t *testing.T, scope *branchroom_model.Scope, _ *branchroom_model.Workspace, _ *IssueReceiveAdmissionRequest) {
				_, err := db.GetEngine(t.Context()).ID(scope.ID).Cols("state").Update(&branchroom_model.Scope{State: branchroom_model.ScopeStateDisabled})
				require.NoError(t, err)
			},
		},
		{
			name:      "retired workspace",
			withGrant: true,
			mutate: func(t *testing.T, _ *branchroom_model.Scope, workspace *branchroom_model.Workspace, _ *IssueReceiveAdmissionRequest) {
				retired, err := branchroom_model.TransitionWorkspace(t.Context(), workspace.ID, branchroom_model.WorkspaceStateReady, branchroom_model.WorkspaceStateRetired, "", timeutil.TimeStampNow())
				require.NoError(t, err)
				require.True(t, retired)
			},
		},
		{
			name:      "wrong user",
			withGrant: true,
			mutate: func(_ *testing.T, _ *branchroom_model.Scope, _ *branchroom_model.Workspace, request *IssueReceiveAdmissionRequest) {
				request.UserID = 4 // active individual without code-write access to the public repo
			},
		},
		{
			name:      "inactive user",
			withGrant: true,
			mutate: func(_ *testing.T, _ *branchroom_model.Scope, _ *branchroom_model.Workspace, request *IssueReceiveAdmissionRequest) {
				request.UserID = 9 // inactive individual from fixtures
			},
		},
		{
			name:      "disabled feature",
			withGrant: true,
			mutate: func(t *testing.T, _ *branchroom_model.Scope, _ *branchroom_model.Workspace, _ *IssueReceiveAdmissionRequest) {
				enabled := setting.Branchroom.Enabled
				setting.Branchroom.Enabled = false
				t.Cleanup(func() { setting.Branchroom.Enabled = enabled })
			},
		},
		{
			name:      "tampered hook",
			withGrant: true,
			mutate: func(t *testing.T, _ *branchroom_model.Scope, workspace *branchroom_model.Workspace, _ *IssueReceiveAdmissionRequest) {
				hookPath := filepath.Join(setting.Branchroom.RestrictedRepoRootPath, ".branchroom-hooks", workspace.StoreID, "pre-receive")
				require.NoError(t, os.WriteFile(hookPath, []byte("#!/bin/sh\nexit 1\n"), 0o700))
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			scopeName := strings.ReplaceAll(testCase.name, " ", "-")
			scope, workspace := prepareReceiveAdmissionFixture(t, "receive-admission-"+scopeName)
			if testCase.withGrant {
				insertWorkspaceActionGrant(t, scope.ID, scope.OwnerID, branchroom_model.ActionWriteSource, 0, 0)
			}
			request := IssueReceiveAdmissionRequest{
				RepoID:   scope.RepoID,
				RouteKey: workspace.RouteKey,
				UserID:   scope.OwnerID,
			}
			if testCase.mutate != nil {
				testCase.mutate(t, scope, workspace, &request)
			}

			assertReceiveAdmissionDenied(t, request, scope.RepoID)
		})
	}
}

func TestIssueReceiveAdmissionRejectsAmbientTransaction(t *testing.T) {
	scope, workspace := prepareReceiveAdmissionFixture(t, "receive-admission-transaction")
	insertWorkspaceActionGrant(t, scope.ID, scope.OwnerID, branchroom_model.ActionWriteSource, 0, 0)

	require.NoError(t, db.WithTx(t.Context(), func(txCtx context.Context) error {
		credential, err := IssueReceiveAdmission(txCtx, IssueReceiveAdmissionRequest{
			RepoID:   scope.RepoID,
			RouteKey: workspace.RouteKey,
			UserID:   scope.OwnerID,
		})
		assert.Nil(t, credential)
		assert.ErrorIs(t, err, errReceiveAdmissionDenied)
		return nil
	}))

	sessions, err := branchroom_model.FindReceiveSessionsByRepoID(t.Context(), scope.RepoID)
	require.NoError(t, err)
	assert.Empty(t, sessions)
}

func prepareReceiveAdmissionFixture(t *testing.T, name string) (*branchroom_model.Scope, *branchroom_model.Workspace) {
	t.Helper()
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	// git init and mkdir honour the umask, so pin the modes the integrity check requires
	require.NoError(t, os.Chmod(setting.Branchroom.RestrictedRepoRootPath, 0o755))
	ensureTestRepositoryCodeUnit(t.Context(), t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, name)
	workspace, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       scope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	// git init and mkdir honour the umask, so pin the modes the integrity check requires
	storePath := filepath.Join(setting.Branchroom.RestrictedRepoRootPath, workspace.StoreID+".git")
	require.NoError(t, os.Chmod(storePath, 0o755))
	require.NoError(t, os.Chmod(filepath.Join(storePath, "config"), 0o644))
	return scope, workspace
}

func assertReceiveAdmissionDenied(t *testing.T, request IssueReceiveAdmissionRequest, repoID int64) {
	t.Helper()
	credential, err := IssueReceiveAdmission(t.Context(), request)
	assert.Nil(t, credential)
	assert.ErrorIs(t, err, errReceiveAdmissionDenied)
	sessions, err := branchroom_model.FindReceiveSessionsByRepoID(t.Context(), repoID)
	require.NoError(t, err)
	assert.Empty(t, sessions)
}
