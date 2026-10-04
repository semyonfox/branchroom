// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	auth_model "gitea.dev/models/auth"
	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/perm"
	access_model "gitea.dev/models/perm/access"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unit"
	user_model "gitea.dev/models/user"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/git"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/setting"
	api "gitea.dev/modules/structs"
	"gitea.dev/modules/test"
	branchroom_service "gitea.dev/services/branchroom"
	"gitea.dev/services/migrations"
	repository_service "gitea.dev/services/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchroomMergeAndRepositoryMigrationIsolation(t *testing.T) {
	defer test.MockVariableValue(&setting.AppURL)()
	setting.AppURL = "http://127.0.0.1:0/"

	onGiteaRun(t, func(t *testing.T, u *url.URL) {
		setting.AppURL = strings.TrimSuffix(u.String(), "/") + "/"
		defer test.MockVariableValue(&setting.Branchroom)()
		defer test.MockVariableValue(&setting.CustomConf)()
		restoreLocalMigrations := test.MockVariableValue(&setting.Migrations.AllowLocalNetworks, true)
		defer func() {
			restoreLocalMigrations()
			require.NoError(t, migrations.Init())
		}()

		restrictedRoot := branchroomTestRoot(t)
		setting.CustomConf = branchroomReceiveHookTestConfig(t, u, restrictedRoot)
		setting.Branchroom = setting.BranchroomConfig{
			Enabled:                true,
			RestrictedRepoRootPath: restrictedRoot,
		}
		require.NoError(t, migrations.Init())

		repository, err := repo_model.GetRepositoryByID(t.Context(), 1)
		require.NoError(t, err)
		require.NoError(t, git.CreateDelegateHooks(t.Context(), repository.CodeStorageRepo()))
		owner, err := user_model.GetUserByID(t.Context(), repository.OwnerID)
		require.NoError(t, err)
		baseSHA, err := git.GetBranchCommitID(t.Context(), repository.CodeStorageRepo(), repository.DefaultBranch)
		require.NoError(t, err)

		scope, err := branchroom_service.CreateRepositoryScope(t.Context(), branchroom_service.CreateRepositoryScopeRequest{
			RepoID:  repository.ID,
			Actor:   branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: owner.ID},
			Name:    "merge-migration-isolation",
			Surface: branchroom_model.SurfaceSystem,
		})
		require.NoError(t, err)
		workspace, err := branchroom_service.CreateRepositoryWorkspace(t.Context(), branchroom_service.CreateRepositoryWorkspaceRequest{
			RepoID:        repository.ID,
			ScopeID:       scope.ID,
			Actor:         branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: owner.ID},
			PublicBaseSHA: baseSHA,
			Surface:       branchroom_model.SurfaceSystem,
		})
		require.NoError(t, err)
		restricted, err := branchroom_module.RestrictedRepository(workspace.StoreID)
		require.NoError(t, err)

		writer, err := user_model.GetUserByName(t.Context(), "user4")
		require.NoError(t, err)
		require.NoError(t, repository_service.AddOrUpdateCollaborator(t.Context(), repository, writer, perm.AccessModeWrite))
		writerPermission, err := access_model.GetIndividualUserRepoPermission(t.Context(), repository, writer)
		require.NoError(t, err)
		require.True(t, writerPermission.CanWrite(unit.TypeCode))
		for _, createGrant := range []func() error{
			func() error {
				_, err := branchroom_service.CreateRepositorySourceReadGrant(t.Context(), branchroom_service.CreateRepositorySourceReadGrantRequest{
					RepoID: repository.ID, ScopeID: scope.ID,
					Actor:           branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: owner.ID},
					SubjectUsername: writer.Name, Surface: branchroom_model.SurfaceSystem,
				})
				return err
			},
			func() error {
				_, err := branchroom_service.CreateRepositorySourceWriteGrant(t.Context(), branchroom_service.CreateRepositorySourceWriteGrantRequest{
					RepoID: repository.ID, ScopeID: scope.ID,
					Actor:           branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: owner.ID},
					SubjectUsername: writer.Name, Surface: branchroom_model.SurfaceSystem,
				})
				return err
			},
		} {
			require.NoError(t, createGrant())
		}

		gatewayURL := u.JoinPath("_branchroom", repository.OwnerName, repository.Name, workspace.RouteKey+".git")
		writerURL := *gatewayURL
		writerURL.User = url.UserPassword(writer.Name, userPassword)
		cloneDir := t.TempDir()
		require.NoError(t, git.Clone(t.Context(), writerURL.String(), cloneDir, git.CloneRepoOptions{}))

		firstRestrictedCommit := commitBranchroomIsolationFile(t, cloneDir, ".env", "BRANCHROOM_FAKE_TOKEN=not-a-real-token\n", "add fake restricted environment")
		const restrictedRef = "refs/heads/branchroom/merge-isolation"
		branchroomPushIsolationRef(t, cloneDir, restrictedRef)

		_, _, err = gitcmd.NewCommand("switch", "-c").AddDynamicArguments("branchroom-side").WithDir(cloneDir).RunStdString(t.Context())
		require.NoError(t, err)
		sideCommit := commitBranchroomIsolationFile(t, cloneDir, "restricted-side.txt", "restricted side\n", "add restricted side")
		_, _, err = gitcmd.NewCommand("switch", "-c").AddDynamicArguments("branchroom-main", firstRestrictedCommit).WithDir(cloneDir).RunStdString(t.Context())
		require.NoError(t, err)
		mainCommit := commitBranchroomIsolationFile(t, cloneDir, "restricted-main.txt", "restricted main\n", "add restricted main")
		_, _, err = gitcmd.NewCommand().
			AddOptionValues("-c", "user.name=Branchroom Test").
			AddOptionValues("-c", "user.email=branchroom@example.com").
			AddArguments("merge", "--no-ff").
			AddOptionFormat("--message=%s", "merge restricted side").
			AddDynamicArguments("branchroom-side").
			WithDir(cloneDir).
			RunStdString(t.Context())
		require.NoError(t, err)
		restrictedMergeCommit := branchroomLocalRevision(t, cloneDir, "HEAD")
		assertCommitParentCount(t, cloneDir, restrictedMergeCommit, 2)
		branchroomPushIsolationRef(t, cloneDir, restrictedRef)

		assert.Equal(t, restrictedMergeCommit, branchroomObjectID(t, restricted, restrictedRef))
		assert.Equal(t, "BRANCHROOM_FAKE_TOKEN=not-a-real-token\n", branchroomShowFile(t, restricted, restrictedRef+":.env"))
		assertGitObjectsMissing(t, repository.CodeStorageRepo(), firstRestrictedCommit, sideCommit, mainCommit, restrictedMergeCommit)
		assert.False(t, git.IsReferenceExist(t.Context(), repository.CodeStorageRepo(), restrictedRef))

		publicBranch := "branchroom-public-merge-isolation"
		testCreateFileInBranch(t, owner, repository, createFileInBranchOptions{NewBranch: publicBranch}, map[string]string{
			"public-merge.txt": "ordinary public merge\n",
		})
		apiContext := NewAPITestContext(t, repository.OwnerName, repository.Name, auth_model.AccessTokenScopeWriteRepository)
		pullRequest, err := doAPICreatePullRequest(apiContext, repository.OwnerName, repository.Name, repository.DefaultBranch, publicBranch)(t)
		require.NoError(t, err)
		doAPIMergePullRequest(apiContext, repository.OwnerName, repository.Name, pullRequest.Index)(t)

		publicMergeCommit, err := git.GetBranchCommitID(t.Context(), repository.CodeStorageRepo(), repository.DefaultBranch)
		require.NoError(t, err)
		assert.NotEqual(t, baseSHA, publicMergeCommit)
		assertCommitParentCountInRepo(t, repository.CodeStorageRepo(), publicMergeCommit, 2)
		assert.Equal(t, "ordinary public merge\n", branchroomShowFile(t, repository.CodeStorageRepo(), publicMergeCommit+":public-merge.txt"))
		assertGitRevisionMissing(t, repository.CodeStorageRepo(), publicMergeCommit+":.env")
		assert.False(t, git.IsReferenceExist(t.Context(), repository.CodeStorageRepo(), restrictedRef))
		assertGitObjectsMissing(t, repository.CodeStorageRepo(), firstRestrictedCommit, sideCommit, mainCommit, restrictedMergeCommit)

		publicAdvertisement := makeBranchroomGitHTTPRequest(t, http.MethodGet, u.JoinPath(repository.OwnerName, repository.Name+".git", "info/refs").String()+"?service=git-upload-pack", "", "")
		assert.Equal(t, http.StatusOK, publicAdvertisement.StatusCode)
		assert.NotContains(t, publicAdvertisement.Body, restrictedRef)
		assert.NotContains(t, publicAdvertisement.Body, restrictedMergeCommit)

		const migratedRepoName = "branchroom-public-migration-isolation"
		migrationRequest := NewRequestWithJSON(t, http.MethodPost, "/api/v1/repos/migrate", &api.MigrateRepoOptions{
			CloneAddr:   u.JoinPath(repository.OwnerName, repository.Name+".git").String(),
			RepoOwnerID: owner.ID,
			RepoName:    migratedRepoName,
		}).AddTokenAuth(apiContext.Token)
		MakeRequest(t, migrationRequest, http.StatusCreated)

		migrated, err := repo_model.GetRepositoryByOwnerAndName(t.Context(), owner.Name, migratedRepoName)
		require.NoError(t, err)
		migratedMergeCommit, err := git.GetBranchCommitID(t.Context(), migrated.CodeStorageRepo(), migrated.DefaultBranch)
		require.NoError(t, err)
		assert.Equal(t, publicMergeCommit, migratedMergeCommit)
		assert.Equal(t, "ordinary public merge\n", branchroomShowFile(t, migrated.CodeStorageRepo(), migratedMergeCommit+":public-merge.txt"))
		assertGitRevisionMissing(t, migrated.CodeStorageRepo(), migratedMergeCommit+":.env")
		assert.False(t, git.IsReferenceExist(t.Context(), migrated.CodeStorageRepo(), restrictedRef))
		assertGitObjectsMissing(t, migrated.CodeStorageRepo(), firstRestrictedCommit, sideCommit, mainCommit, restrictedMergeCommit)
		migratedScopes, err := branchroom_model.FindScopesByRepoID(t.Context(), migrated.ID)
		require.NoError(t, err)
		assert.Empty(t, migratedScopes)
		assert.Equal(t, restrictedMergeCommit, branchroomObjectID(t, restricted, restrictedRef))
	})
}

func commitBranchroomIsolationFile(t *testing.T, repositoryPath, name, contents, message string) string {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(repositoryPath, name), []byte(contents), 0o600))
	require.NoError(t, gitAddChangesDeprecated(t.Context(), repositoryPath, false, name))
	signature := &git.Signature{Email: "branchroom@example.com", Name: "Branchroom Test", When: time.Now()}
	require.NoError(t, gitCommitChangesDeprecated(t.Context(), repositoryPath, gitCommitChangesOptions{
		Committer: signature,
		Author:    signature,
		Message:   message,
	}))
	return branchroomLocalRevision(t, repositoryPath, "HEAD")
}

func branchroomPushIsolationRef(t *testing.T, repositoryPath, ref string) {
	t.Helper()
	_, _, err := gitcmd.NewCommand("push").AddDynamicArguments("origin", "HEAD:"+ref).WithDir(repositoryPath).RunStdString(t.Context())
	require.NoError(t, err)
}

func branchroomLocalRevision(t *testing.T, repositoryPath, revision string) string {
	t.Helper()
	objectID, _, err := gitcmd.NewCommand("rev-parse").AddDynamicArguments(revision).WithDir(repositoryPath).RunStdString(t.Context())
	require.NoError(t, err)
	return strings.TrimSpace(objectID)
}

func assertCommitParentCount(t *testing.T, repositoryPath, revision string, expected int) {
	t.Helper()
	parents, _, err := gitcmd.NewCommand("rev-list", "--parents", "-n", "1").AddDynamicArguments(revision).WithDir(repositoryPath).RunStdString(t.Context())
	require.NoError(t, err)
	assert.Len(t, strings.Fields(parents), expected+1)
}

func assertCommitParentCountInRepo(t *testing.T, repository git.RepositoryFacade, revision string, expected int) {
	t.Helper()
	parents, _, err := gitcmd.NewCommand("rev-list", "--parents", "-n", "1").AddDynamicArguments(revision).WithRepo(repository).RunStdString(t.Context())
	require.NoError(t, err)
	assert.Len(t, strings.Fields(parents), expected+1)
}

func branchroomShowFile(t *testing.T, repository git.RepositoryFacade, revision string) string {
	t.Helper()
	contents, _, err := gitcmd.NewCommand("show").AddDynamicArguments(revision).WithRepo(repository).RunStdString(t.Context())
	require.NoError(t, err)
	return contents
}

func assertGitRevisionMissing(t *testing.T, repository git.RepositoryFacade, revision string) {
	t.Helper()
	_, _, err := gitcmd.NewCommand("cat-file", "-e").AddDynamicArguments(revision).WithRepo(repository).RunStdString(t.Context())
	assert.Error(t, err, "%s unexpectedly exists", revision)
}

func assertGitObjectsMissing(t *testing.T, repository git.RepositoryFacade, objectIDs ...string) {
	t.Helper()
	for _, objectID := range objectIDs {
		assertGitRevisionMissing(t, repository, objectID+"^{object}")
	}
}
