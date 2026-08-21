// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unittest"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/git"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/git/gitrepo"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	"gitea.dev/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateRepositoryWorkspaceImportsPublicBase(t *testing.T) {
	public, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-import")

	workspace, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       scope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.WorkspaceStateReady, workspace.State)
	assert.Equal(t, repository.ObjectFormatName, workspace.ObjectFormatName)
	assert.Equal(t, baseSHA, workspace.PublicBaseSHA)

	restricted, err := branchroom_module.RestrictedRepository(workspace.StoreID)
	require.NoError(t, err)
	assert.NotEqual(t, gitrepo.RepoLocalPath(public), gitrepo.RepoLocalPath(restricted))
	assert.NoFileExists(t, filepath.Join(gitrepo.RepoLocalPath(restricted), "objects", "info", "alternates"))
	remotes, _, err := gitcmd.NewCommand("remote").WithRepo(restricted).RunStdString(t.Context())
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(remotes))
	_, _, err = gitcmd.NewCommand("cat-file", "-e").AddDynamicArguments(baseSHA + "^{commit}").WithRepo(restricted).RunStdString(t.Context())
	require.NoError(t, err)

	require.NoError(t, git.ForceFastImport(t.Context(), restricted, []git.FastImportCommit{{
		Ref:     "refs/heads/restricted",
		Message: "restricted change",
		Files:   []git.FastImportFile{{Path: "restricted.txt", Content: "restricted only\n"}},
	}}))
	restrictedCommit, _, err := gitcmd.NewCommand("rev-parse", "refs/heads/restricted").WithRepo(restricted).RunStdString(t.Context())
	require.NoError(t, err)
	_, _, err = gitcmd.NewCommand("cat-file", "-e").AddDynamicArguments(strings.TrimSpace(restrictedCommit) + "^{commit}").WithRepo(public).RunStdString(t.Context())
	assert.Error(t, err)

	stored, err := branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
	require.NoError(t, err)
	assert.Equal(t, workspace.WorkspaceKey, stored.WorkspaceKey)
	assert.Equal(t, branchroom_model.WorkspaceStateReady, stored.State)
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.AuditEvent{
		ScopeID:    scope.ID,
		ResourceID: workspaceResourceID(workspace.WorkspaceKey),
		ReasonCode: "workspace_created",
	})

	_, err = CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       scope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, ErrWorkspaceAlreadyExists)
}

func TestCreateRepositoryWorkspaceFailedImportStaysNonRoutable(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-failed-import")

	blockedRoot := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blockedRoot, []byte("blocked"), 0o600))
	setting.Branchroom.RestrictedRepoRootPath = blockedRoot

	workspace, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       scope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	assert.Nil(t, workspace)
	assert.ErrorIs(t, err, ErrWorkspaceProvisioning)

	stored, err := branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.WorkspaceStateFailed, stored.State)
	assert.Equal(t, "import_failed", stored.FailureCode)
	assert.NotEqual(t, branchroom_model.WorkspaceStateReady, stored.State)
	_, err = branchroom_model.TransitionWorkspace(t.Context(), stored.ID, branchroom_model.WorkspaceStateFailed, branchroom_model.WorkspaceStateReady, "", 0)
	assert.ErrorIs(t, err, util.ErrInvalidArgument)
}

func TestCreateRepositoryWorkspaceRejectsEnvironmentScope(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	scope := &branchroom_model.Scope{
		Kind:        branchroom_model.ScopeKindEnvironment,
		RepoID:      repository.ID,
		Name:        "environment-workspace",
		State:       branchroom_model.ScopeStateActive,
		OwnerType:   branchroom_model.ScopeOwnerTypeUser,
		OwnerID:     repository.OwnerID,
		CreatedByID: repository.OwnerID,
	}
	require.NoError(t, branchroom_model.InsertScope(t.Context(), scope))

	workspace, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       scope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	assert.Nil(t, workspace)
	assert.ErrorIs(t, err, util.ErrNotExist)
	_, err = branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
	assert.ErrorIs(t, err, util.ErrNotExist)
}

func TestCreateRepositoryWorkspaceRejectsUnauthorizedAndInvalidBases(t *testing.T) {
	public, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	treeSHA, _, err := gitcmd.NewCommand("rev-parse").AddDynamicArguments(baseSHA + "^{tree}").WithRepo(public).RunStdString(t.Context())
	require.NoError(t, err)
	treeSHA = strings.TrimSpace(treeSHA)

	t.Run("non-owner", func(t *testing.T) {
		scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-non-owner")
		workspace, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
			RepoID:        repository.ID,
			ScopeID:       scope.ID,
			Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID + 1000},
			PublicBaseSHA: baseSHA,
			Surface:       branchroom_model.SurfaceSystem,
		})
		assert.Nil(t, workspace)
		assert.ErrorIs(t, err, util.ErrNotExist)
		_, err = branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
		assert.ErrorIs(t, err, util.ErrNotExist)
	})

	for _, testCase := range []struct {
		name          string
		publicBaseSHA string
	}{
		{name: "abbreviated", publicBaseSHA: baseSHA[:7]},
		{name: "tree", publicBaseSHA: treeSHA},
		{name: "wrong format", publicBaseSHA: strings.Repeat("0", 64)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-invalid-base-"+testCase.name)
			workspace, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
				RepoID:        repository.ID,
				ScopeID:       scope.ID,
				Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
				PublicBaseSHA: testCase.publicBaseSHA,
				Surface:       branchroom_model.SurfaceSystem,
			})
			assert.Nil(t, workspace)
			assert.ErrorIs(t, err, util.ErrInvalidArgument)
			_, err = branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
			assert.ErrorIs(t, err, util.ErrNotExist)
		})
	}
}

func TestCreateRepositoryWorkspaceRejectsAmbientTransaction(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-transaction")

	require.NoError(t, db.WithTx(t.Context(), func(txCtx context.Context) error {
		workspace, err := CreateRepositoryWorkspace(txCtx, CreateRepositoryWorkspaceRequest{
			RepoID:        repository.ID,
			ScopeID:       scope.ID,
			Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
			PublicBaseSHA: baseSHA,
			Surface:       branchroom_model.SurfaceSystem,
		})
		assert.Nil(t, workspace)
		assert.ErrorIs(t, err, ErrWorkspaceInTransaction)
		return nil
	}))

	_, err := branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
	assert.ErrorIs(t, err, util.ErrNotExist)
}

func prepareWorkspaceSourceRepository(t *testing.T) (gitrepo.RepositoryFacade, *repo_model.Repository, string) {
	t.Helper()
	restoreRepoRoot := test.MockVariableValue(&setting.RepoRootPath)
	t.Cleanup(restoreRepoRoot)
	restoreBranchroom := test.MockVariableValue(&setting.Branchroom)
	t.Cleanup(restoreBranchroom)

	root := t.TempDir()
	publicRoot := filepath.Join(root, "public-repositories")
	restrictedRoot := filepath.Join(root, "restricted-repositories")
	require.NoError(t, os.MkdirAll(publicRoot, 0o755))
	require.NoError(t, os.MkdirAll(restrictedRoot, 0o755))
	setting.RepoRootPath = publicRoot
	setting.Branchroom = setting.BranchroomConfig{
		Enabled:                true,
		RestrictedRepoRootPath: restrictedRoot,
	}
	require.NoError(t, unittest.PrepareTestDatabase())

	repository := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	public := repository.CodeStorageRepo()
	require.NoError(t, git.InitRepository(t.Context(), public, repository.ObjectFormatName))
	require.NoError(t, git.ForceFastImport(t.Context(), public, []git.FastImportCommit{{
		Ref:     "refs/heads/main",
		Message: "public main",
		Files:   []git.FastImportFile{{Path: "README.md", Content: "public base\n"}},
	}}))
	baseSHA, _, err := gitcmd.NewCommand("rev-parse", "refs/heads/main").WithRepo(public).RunStdString(t.Context())
	require.NoError(t, err)
	return public, repository, strings.TrimSpace(baseSHA)
}

func insertWorkspaceScope(t *testing.T, repoID, ownerID int64, name string) *branchroom_model.Scope {
	t.Helper()
	scope := &branchroom_model.Scope{
		Kind:        branchroom_model.ScopeKindRepository,
		RepoID:      repoID,
		Name:        name,
		State:       branchroom_model.ScopeStateActive,
		OwnerType:   branchroom_model.ScopeOwnerTypeUser,
		OwnerID:     ownerID,
		CreatedByID: ownerID,
	}
	require.NoError(t, branchroom_model.InsertScope(t.Context(), scope))
	return scope
}
