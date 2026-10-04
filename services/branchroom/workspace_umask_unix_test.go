// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

//go:build unix

package branchroom

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/git/gitrepo"
	"gitea.dev/modules/setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateRepositoryWorkspaceNormalizesImportedStoreModes(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-permissive-umask")

	var workspace *branchroom_model.Workspace
	var err error
	func() {
		oldUmask := syscall.Umask(0)
		defer syscall.Umask(oldUmask)
		workspace, err = CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
			RepoID:        repository.ID,
			ScopeID:       scope.ID,
			Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
			PublicBaseSHA: baseSHA,
			Surface:       branchroom_model.SurfaceSystem,
		})
	}()
	require.NoError(t, err)

	restricted, err := branchroom_module.RestrictedRepository(workspace.StoreID)
	require.NoError(t, err)
	storePath := gitrepo.RepoLocalPath(restricted)
	for path, want := range map[string]os.FileMode{
		storePath:                           0o755,
		filepath.Join(storePath, "objects"): 0o755,
		filepath.Join(storePath, "config"):  0o644,
	} {
		info, statErr := os.Lstat(path)
		require.NoError(t, statErr)
		assert.Equal(t, want, info.Mode().Perm(), path)
	}
	assert.NoError(t, branchroom_module.VerifyRestrictedStoreHook(t.Context(), setting.Branchroom.RestrictedRepoRootPath, workspace.StoreID))
}
