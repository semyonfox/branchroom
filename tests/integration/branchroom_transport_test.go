// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"gitea.dev/models/perm"
	"gitea.dev/modules/branchroom"
	"gitea.dev/modules/git"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/git/gitrepo"
	"gitea.dev/modules/private"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchroomUnregisteredStoreDoesNotLeakThroughPublicGitHTTP(t *testing.T) {
	defer test.MockVariableValue(&setting.AppURL)()
	setting.AppURL = "http://127.0.0.1:0/"

	onGiteaRun(t, func(t *testing.T, u *url.URL) {
		defer test.MockVariableValue(&setting.Branchroom)()
		defer test.MockVariableValue(&setting.LocalURL)()
		setting.Branchroom = setting.BranchroomConfig{
			Enabled:                true,
			RestrictedRepoRootPath: t.TempDir(),
		}
		setting.LocalURL = u.String()

		restricted, err := branchroom.RestrictedRepository("transport-regression")
		require.NoError(t, err)
		require.NoError(t, git.InitRepository(t.Context(), restricted, git.Sha1ObjectFormat.Name()))
		require.NoError(t, git.ForceFastImport(t.Context(), restricted, []git.FastImportCommit{{
			Ref:     "refs/heads/restricted",
			Message: "restricted commit",
			Files: []git.FastImportFile{
				{Path: "restricted.txt", Content: "restricted only\n"},
			},
		}}))

		restrictedCommit := branchroomObjectID(t, restricted, "refs/heads/restricted")
		restrictedBlob := branchroomObjectID(t, restricted, "refs/heads/restricted:restricted.txt")
		publicURL := u.JoinPath("user2/repo1.git")

		t.Run("smart HTTP advertisement", func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, publicURL.JoinPath("info/refs").String()+"?service=git-upload-pack", nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.NotContains(t, string(body), restrictedCommit)
		})

		t.Run("known SHA fetch", func(t *testing.T) {
			local := t.TempDir()
			require.NoError(t, git.InitRepositoryLocal(t.Context(), local, false, git.Sha1ObjectFormat.Name()))
			_, _, err := gitcmd.NewCommand("fetch").AddDynamicArguments(publicURL.String(), restrictedCommit).
				WithDir(local).RunStdString(t.Context())
			assert.Error(t, err)
		})

		t.Run("dumb HTTP object", func(t *testing.T) {
			objectPath := "objects/" + restrictedBlob[:2] + "/" + restrictedBlob[2:]
			req, err := http.NewRequest(http.MethodGet, publicURL.JoinPath(objectPath).String(), nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.NotContains(t, string(body), "restricted only")
		})

		t.Run("remote archive", func(t *testing.T) {
			_, _, err := gitcmd.NewCommand("archive").AddOptionValues("--remote", publicURL.String()).
				AddDynamicArguments(restrictedCommit).WithDir(t.TempDir()).RunStdString(t.Context())
			assert.Error(t, err)
		})

		t.Run("SSH storage resolution", func(t *testing.T) {
			results, extra := private.ServCommand(t.Context(), 1, "user2", "repo1", perm.AccessModeRead, "git-upload-pack", "")
			require.NoError(t, extra.Error)

			rel, err := filepath.Rel(setting.Branchroom.RestrictedRepoRootPath, results.RepoStoragePath)
			require.NoError(t, err)
			assert.True(t, rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)))
		})
	})
}

func branchroomObjectID(t *testing.T, repo gitrepo.RepositoryFacade, revision string) string {
	t.Helper()

	objectID, _, err := gitcmd.NewCommand("rev-parse").AddDynamicArguments(revision).WithRepo(repo).RunStdString(t.Context())
	require.NoError(t, err)
	return strings.TrimSpace(objectID)
}
