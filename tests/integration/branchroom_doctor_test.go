// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"os"
	"path/filepath"
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/modules/git"
	"gitea.dev/modules/log"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	branchroom_service "gitea.dev/services/branchroom"
	"gitea.dev/services/doctor"
	"gitea.dev/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchroomDoctorCheck(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Branchroom, setting.BranchroomConfig{Enabled: true, RestrictedRepoRootPath: branchroomTestRoot(t)})()
	ctx := t.Context()

	var check *doctor.Check
	for _, candidate := range doctor.Checks {
		if candidate.Name == "branchroom" {
			check = candidate
		}
	}
	require.NotNil(t, check)
	run := func(autofix bool) error {
		return check.Run(ctx, log.GetLogger(log.DEFAULT), autofix)
	}

	repository, err := repo_model.GetRepositoryByID(ctx, 1)
	require.NoError(t, err)
	base, err := git.GetBranchCommitID(ctx, repository.CodeStorageRepo(), repository.DefaultBranch)
	require.NoError(t, err)
	owner := branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID}
	scope, err := branchroom_service.CreateRepositoryScope(ctx, branchroom_service.CreateRepositoryScopeRequest{RepoID: 1, Actor: owner, Name: "doctor", Surface: branchroom_model.SurfaceSystem})
	require.NoError(t, err)
	workspace, err := branchroom_service.CreateRepositoryWorkspace(ctx, branchroom_service.CreateRepositoryWorkspaceRequest{RepoID: 1, ScopeID: scope.ID, Actor: owner, PublicBaseSHA: base, Surface: branchroom_model.SurfaceSystem})
	require.NoError(t, err)
	require.NoError(t, run(false))

	t.Run("hook after an executable move", func(t *testing.T) {
		defer test.MockVariableValue(&setting.AppPath, "/opt/moved/gitea")()
		require.ErrorContains(t, run(false), "found 1 Branchroom problems")
		require.NoError(t, run(true))
		hook, err := os.ReadFile(filepath.Join(setting.Branchroom.RestrictedRepoRootPath, ".branchroom-hooks", workspace.StoreID, "pre-receive"))
		require.NoError(t, err)
		assert.Contains(t, string(hook), "/opt/moved/gitea")
		require.NoError(t, run(false))
	})
	require.NoError(t, run(true))

	t.Run("store without a mapping", func(t *testing.T) {
		orphan := filepath.Join(setting.Branchroom.RestrictedRepoRootPath, "0f0e0d0c-0b0a-4908-8706-050403020100.git")
		require.NoError(t, os.Mkdir(orphan, 0o700))
		defer os.Remove(orphan)
		require.ErrorContains(t, run(true), "found 1 Branchroom problems")
		assert.DirExists(t, orphan)
	})

	t.Run("rewritten audit event", func(t *testing.T) {
		_, err := db.GetEngine(ctx).Where("scope_id = ? AND sequence = 1", scope.ID).Cols("reason_code").Update(&branchroom_model.AuditEvent{ReasonCode: "rewritten"})
		require.NoError(t, err)
		require.ErrorContains(t, run(true), "found 1 Branchroom problems")
	})
}
