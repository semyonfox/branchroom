// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"encoding/base32"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/google/uuid"
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

	expectedHookDir := filepath.Join(setting.Branchroom.RestrictedRepoRootPath, ".branchroom-hooks", workspace.StoreID)
	hooksPath, _, err := gitcmd.NewCommand("config", "--local", "--null", "--get-all").AddDynamicArguments("core.hooksPath").WithRepo(restricted).RunStdBytes(t.Context())
	require.NoError(t, err)
	assert.Equal(t, expectedHookDir+"\x00", string(hooksPath))
	preReceive := filepath.Join(expectedHookDir, "pre-receive")
	preReceiveContent, err := os.ReadFile(preReceive)
	require.NoError(t, err)
	assert.Equal(t, branchroom_module.ReceivePreReceiveHookTemplate(), string(preReceiveContent))
	// Lstat sees the link itself, so a planted symlink cannot satisfy these checks
	preReceiveInfo, err := os.Lstat(preReceive)
	require.NoError(t, err)
	assert.True(t, preReceiveInfo.Mode().IsRegular())
	assert.Equal(t, os.FileMode(0o700), preReceiveInfo.Mode().Perm())
	assert.Zero(t, preReceiveInfo.Mode()&(os.ModeSetuid|os.ModeSetgid))

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

func TestCreateRepositoryWorkspaceUnavailableStoreStaysNonRoutable(t *testing.T) {
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
	assert.Equal(t, "store_unavailable", stored.FailureCode)
	assert.NotEqual(t, branchroom_model.WorkspaceStateReady, stored.State)
	_, err = branchroom_model.TransitionWorkspace(t.Context(), stored.ID, branchroom_model.WorkspaceStateFailed, branchroom_model.WorkspaceStateReady, "", 0)
	assert.ErrorIs(t, err, util.ErrInvalidArgument)
}

func TestCreateRepositoryWorkspaceHookInstallFailureStaysNonRoutable(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-hook-install-failed")

	hooksRoot := filepath.Join(setting.Branchroom.RestrictedRepoRootPath, ".branchroom-hooks")
	require.NoError(t, os.Mkdir(hooksRoot, 0o700))
	require.NoError(t, os.Chmod(hooksRoot, 0o770))

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
	assert.Equal(t, "receive_hook_install_failed", stored.FailureCode)

	restricted, err := branchroom_module.RestrictedRepository(stored.StoreID)
	require.NoError(t, err)
	exists, err := git.IsRepositoryExist(t.Context(), restricted)
	require.NoError(t, err)
	assert.False(t, exists)
	assert.NoDirExists(t, filepath.Join(setting.Branchroom.RestrictedRepoRootPath, ".branchroom-hooks", stored.StoreID))
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

func TestRetryWorkspaceProvisioningReimportsFailedStore(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-retry")

	goodRoot := setting.Branchroom.RestrictedRepoRootPath
	blockedRoot := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blockedRoot, []byte("blocked"), 0o600))
	setting.Branchroom.RestrictedRepoRootPath = blockedRoot

	_, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       scope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, ErrWorkspaceProvisioning)
	failed, err := branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
	require.NoError(t, err)
	require.Equal(t, branchroom_model.WorkspaceStateFailed, failed.State)

	setting.Branchroom.RestrictedRepoRootPath = goodRoot
	retried, err := RetryWorkspaceProvisioning(t.Context(), RetryWorkspaceProvisioningRequest{
		RepoID:  repository.ID,
		ScopeID: scope.ID,
		Actor:   Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.WorkspaceStateReady, retried.State)
	assert.Equal(t, baseSHA, retried.PublicBaseSHA)
	assert.Empty(t, retried.FailureCode)

	restricted, err := branchroom_module.RestrictedRepository(retried.StoreID)
	require.NoError(t, err)
	exists, err := git.IsRepositoryExist(t.Context(), restricted)
	require.NoError(t, err)
	assert.True(t, exists)
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.AuditEvent{
		ScopeID:    scope.ID,
		ResourceID: workspaceResourceID(retried.WorkspaceKey),
		ReasonCode: "workspace_retry_requested",
	})

	_, err = RetryWorkspaceProvisioning(t.Context(), RetryWorkspaceProvisioningRequest{
		RepoID:  repository.ID,
		ScopeID: scope.ID,
		Actor:   Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		Surface: branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, ErrWorkspaceStateChanged)

	_, err = RetryWorkspaceProvisioning(t.Context(), RetryWorkspaceProvisioningRequest{
		RepoID:  repository.ID,
		ScopeID: scope.ID,
		Actor:   Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID + 1000},
		Surface: branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, util.ErrNotExist)

	require.NoError(t, db.WithTx(t.Context(), func(txCtx context.Context) error {
		_, err := RetryWorkspaceProvisioning(txCtx, RetryWorkspaceProvisioningRequest{
			RepoID:  repository.ID,
			ScopeID: scope.ID,
			Actor:   Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
			Surface: branchroom_model.SurfaceSystem,
		})
		assert.ErrorIs(t, err, ErrWorkspaceInTransaction)
		return nil
	}))
}

func TestRetireWorkspaceTransitionsAndAudits(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-retire")
	request := RetireWorkspaceRequest{
		RepoID:  repository.ID,
		ScopeID: scope.ID,
		Actor:   Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		Surface: branchroom_model.SurfaceSystem,
	}

	workspace, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       scope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)

	retired, err := RetireWorkspace(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.WorkspaceStateRetired, retired.State)
	assert.NotZero(t, retired.RetiredUnix)
	assert.Empty(t, retired.FailureCode)
	stored, err := branchroom_model.GetWorkspaceByID(t.Context(), workspace.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.WorkspaceStateRetired, stored.State)
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.AuditEvent{
		ScopeID:    scope.ID,
		ResourceID: workspaceResourceID(workspace.WorkspaceKey),
		ReasonCode: "workspace_retired",
	})

	_, err = RetireWorkspace(t.Context(), request)
	assert.ErrorIs(t, err, ErrWorkspaceStateChanged)

	nonOwner := request
	nonOwner.Actor = Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID + 1000}
	_, err = RetireWorkspace(t.Context(), nonOwner)
	assert.ErrorIs(t, err, util.ErrNotExist)
}

func TestRetireWorkspaceAllowsFailedWorkspace(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-retire-failed")

	blockedRoot := filepath.Join(t.TempDir(), "not-a-directory")
	require.NoError(t, os.WriteFile(blockedRoot, []byte("blocked"), 0o600))
	setting.Branchroom.RestrictedRepoRootPath = blockedRoot
	_, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       scope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, ErrWorkspaceProvisioning)

	retired, err := RetireWorkspace(t.Context(), RetireWorkspaceRequest{
		RepoID:  repository.ID,
		ScopeID: scope.ID,
		Actor:   Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.WorkspaceStateRetired, retired.State)
	stored, err := branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.WorkspaceStateRetired, stored.State)
	assert.Empty(t, stored.FailureCode)
}

func TestPurgeWorkspaceRemovesStoreThenMarksPurged(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-purge")
	request := PurgeWorkspaceRequest{
		RepoID:  repository.ID,
		ScopeID: scope.ID,
		Actor:   Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		Surface: branchroom_model.SurfaceSystem,
	}

	workspace, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       scope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	restricted, err := branchroom_module.RestrictedRepository(workspace.StoreID)
	require.NoError(t, err)
	storePath := gitrepo.RepoLocalPath(restricted)
	assert.DirExists(t, storePath)

	purged, err := PurgeWorkspace(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.WorkspaceStatePurged, purged.State)
	assert.NotZero(t, purged.RetiredUnix)
	assert.NoDirExists(t, storePath)
	assert.NoDirExists(t, filepath.Join(setting.Branchroom.RestrictedRepoRootPath, ".branchroom-hooks", workspace.StoreID))
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.AuditEvent{
		ScopeID:    scope.ID,
		ResourceID: workspaceResourceID(workspace.WorkspaceKey),
		ReasonCode: "workspace_purged",
	})
	stored, err := branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.WorkspaceStatePurged, stored.State)

	_, err = PurgeWorkspace(t.Context(), request)
	assert.ErrorIs(t, err, ErrWorkspaceStateChanged)

	nonOwner := request
	nonOwner.Actor = Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID + 1000}
	_, err = PurgeWorkspace(t.Context(), nonOwner)
	assert.ErrorIs(t, err, util.ErrNotExist)

	require.NoError(t, db.WithTx(t.Context(), func(txCtx context.Context) error {
		_, err := PurgeWorkspace(txCtx, request)
		assert.ErrorIs(t, err, ErrWorkspaceInTransaction)
		return nil
	}))
}

func TestPurgeWorkspaceToleratesMissingStore(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-purge-missing")

	workspace, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       scope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	restricted, err := branchroom_module.RestrictedRepository(workspace.StoreID)
	require.NoError(t, err)
	require.NoError(t, git.DeleteRepository(t.Context(), restricted))

	purged, err := PurgeWorkspace(t.Context(), PurgeWorkspaceRequest{
		RepoID:  repository.ID,
		ScopeID: scope.ID,
		Actor:   Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		Surface: branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	assert.Equal(t, branchroom_model.WorkspaceStatePurged, purged.State)
}

func TestPurgeWorkspaceRejectsProvisioningWorkspace(t *testing.T) {
	_, repository, _ := prepareWorkspaceSourceRepository(t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-purge-provisioning")
	insertRawWorkspace(t, scope, branchroom_model.WorkspaceStateProvisioning)

	_, err := PurgeWorkspace(t.Context(), PurgeWorkspaceRequest{
		RepoID:  repository.ID,
		ScopeID: scope.ID,
		Actor:   Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		Surface: branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, ErrWorkspaceStateChanged)
}

func TestCreateRepositoryWorkspaceEnforcesOwnerQuota(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	setting.Branchroom.MaxWorkspacesPerOwner = 1

	first := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-quota-first")
	_, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       first.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)

	second := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "workspace-quota-second")
	_, err = CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       second.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	assert.ErrorIs(t, err, ErrWorkspaceQuotaReached)
	assert.ErrorIs(t, err, util.ErrNotExist)
	_, err = branchroom_model.GetWorkspaceByScopeID(t.Context(), second.ID)
	assert.ErrorIs(t, err, util.ErrNotExist)

	setting.Branchroom.MaxWorkspacesPerOwner = 0
	_, err = CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       second.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	assert.NoError(t, err)
}

func insertRawWorkspace(t *testing.T, scope *branchroom_model.Scope, state branchroom_model.WorkspaceState) *branchroom_model.Workspace {
	t.Helper()
	workspaceKey := uuid.NewString()
	storeID := uuid.NewString()
	routeUUID := uuid.New()
	routeKey := base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding).EncodeToString(routeUUID[:])
	workspace := &branchroom_model.Workspace{
		WorkspaceKey:     workspaceKey,
		RouteKey:         routeKey,
		ScopeID:          scope.ID,
		RepoID:           scope.RepoID,
		StoreID:          storeID,
		PublicBaseSHA:    strings.Repeat("a", 40),
		ObjectFormatName: "sha1",
		State:            state,
		RetiredUnix:      timeutil.TimeStampNow(),
		CreatedByID:      scope.CreatedByID,
	}
	if state != branchroom_model.WorkspaceStatePurged && state != branchroom_model.WorkspaceStateRetired {
		workspace.RetiredUnix = 0
	}
	require.NoError(t, branchroom_model.InsertWorkspace(t.Context(), workspace))
	return workspace
}

func prepareWorkspaceSourceRepository(t *testing.T) (gitrepo.RepositoryFacade, *repo_model.Repository, string) {
	t.Helper()
	restoreRepoRoot := test.MockVariableValue(&setting.RepoRootPath)
	t.Cleanup(restoreRepoRoot)
	restoreBranchroom := test.MockVariableValue(&setting.Branchroom)
	t.Cleanup(restoreBranchroom)

	root := newPrivateRestrictedTestRoot(t)
	publicRoot := filepath.Join(root, "public-repositories")
	restrictedRoot := newPrivateRestrictedTestRoot(t)
	require.NoError(t, os.MkdirAll(publicRoot, 0o755))
	require.NoError(t, os.Chmod(restrictedRoot, 0o700))
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

func newPrivateRestrictedTestRoot(t *testing.T) string {
	t.Helper()
	parent, err := os.UserHomeDir()
	require.NoError(t, err)
	//nolint:usetesting // t.TempDir does not guarantee the private ancestor this test requires.
	root, err := os.MkdirTemp(parent, ".gitea-branchroom-test-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	require.NoError(t, os.Chmod(root, 0o700))
	return root
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

func TestAbandonedWorkspaceProvisioning(t *testing.T) {
	for _, operation := range []string{"retry", "purge"} {
		t.Run(operation, func(t *testing.T) {
			_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
			scope, workspace := createSourceHandoffWorkspace(t, repository, baseSHA, "abandoned-store")
			_, err := db.GetEngine(t.Context()).ID(workspace.ID).Cols("state", "updated_unix").NoAutoTime().Update(&branchroom_model.Workspace{State: branchroom_model.WorkspaceStateProvisioning, UpdatedUnix: timeutil.TimeStamp(time.Now().Add(-workspaceProvisioningWindow - time.Minute).Unix())})
			require.NoError(t, err)
			if operation == "retry" {
				retried, err := RetryWorkspaceProvisioning(t.Context(), RetryWorkspaceProvisioningRequest{RepoID: repository.ID, ScopeID: scope.ID, Actor: Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID}, Surface: branchroom_model.SurfaceSystem})
				require.NoError(t, err)
				require.Equal(t, branchroom_model.WorkspaceStateReady, retried.State)
			} else {
				_, err := PurgeWorkspace(t.Context(), PurgeWorkspaceRequest{RepoID: repository.ID, ScopeID: scope.ID, Actor: Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID}, Surface: branchroom_model.SurfaceSystem})
				require.NoError(t, err)
				require.NoDirExists(t, filepath.Join(setting.Branchroom.RestrictedRepoRootPath, workspace.StoreID+".git"))
			}
		})
	}
}

func TestWorkspaceReadyFailureRemovesImportedStore(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, "ready-failure")
	request := CreateRepositoryWorkspaceRequest{RepoID: repository.ID, ScopeID: scope.ID, Actor: Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID}, PublicBaseSHA: baseSHA, Surface: branchroom_model.SurfaceSystem}
	workspace, err := reserveRepositoryWorkspace(t.Context(), request, repository.ObjectFormatName)
	require.NoError(t, err)
	_, err = db.GetEngine(t.Context()).ID(scope.ID).Cols("state").Update(&branchroom_model.Scope{State: branchroom_model.ScopeStateRetired})
	require.NoError(t, err)
	_, err = importReservedWorkspace(t.Context(), repository, workspace, request)
	require.ErrorIs(t, err, ErrWorkspaceStateChanged)
	require.NoDirExists(t, filepath.Join(setting.Branchroom.RestrictedRepoRootPath, workspace.StoreID+".git"))
	stored, err := branchroom_model.GetWorkspaceByID(t.Context(), workspace.ID)
	require.NoError(t, err)
	require.Equal(t, branchroom_model.WorkspaceStateFailed, stored.State)
}
