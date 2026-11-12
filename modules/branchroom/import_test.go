// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.dev/modules/git"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/git/gitrepo"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportPublicBaseIsolated(t *testing.T) {
	defer test.MockVariableValue(&setting.Branchroom)()

	testRoot := newSafeTestRoot(t)
	publicRoot := filepath.Join(testRoot, "public-repositories")
	restrictedRoot := filepath.Join(testRoot, "restricted-repositories")
	require.NoError(t, os.MkdirAll(publicRoot, 0o755))
	require.NoError(t, os.MkdirAll(restrictedRoot, 0o700))
	require.NoError(t, os.Chmod(restrictedRoot, 0o700))
	setting.Branchroom = setting.BranchroomConfig{
		Enabled:                true,
		RestrictedRepoRootPath: restrictedRoot,
	}

	public, err := gitrepo.RepositoryManagedInRoot("public-store", publicRoot, "public.git")
	require.NoError(t, err)
	require.NoError(t, git.InitRepository(t.Context(), public, git.Sha1ObjectFormat.Name(), ""))
	require.NoError(t, git.ForceFastImport(t.Context(), public, []git.FastImportCommit{
		{
			Ref:     "refs/heads/main",
			Message: "public main",
			Files: []git.FastImportFile{
				{Path: "README.md", Content: "public base\n"},
			},
		},
		{
			Ref:     "refs/heads/release",
			Message: "public release",
			Files: []git.FastImportFile{
				{Path: "RELEASE.md", Content: "public release\n"},
			},
		},
	}))
	_, _, err = gitcmd.NewCommand("symbolic-ref", "HEAD", "refs/heads/main").WithRepo(public).RunStdString(t.Context())
	require.NoError(t, err)
	_, _, err = gitcmd.NewCommand("repack", "-ad").WithRepo(public).RunStdString(t.Context())
	require.NoError(t, err)

	restricted, err := RestrictedRepository("a13e2a27-167d-4586-a511-42910dc3a955")
	require.NoError(t, err)
	require.NoError(t, ImportPublicBase(t.Context(), public, restricted))

	assertBareRepository(t, public)
	assertBareRepository(t, restricted)
	assert.NotEqual(t, gitrepo.RepoLocalPath(public), gitrepo.RepoLocalPath(restricted))
	assertObjectDirectoriesDistinct(t, public, restricted)
	assertEqualRefs(t, public, restricted)
	assertNoAlternates(t, public)
	assertNoAlternates(t, restricted)
	assertNoRemote(t, restricted)
	assertObjectFilesAreDistinct(t, public, restricted)

	require.NoError(t, git.ForceFastImport(t.Context(), restricted, []git.FastImportCommit{{
		Ref:     "refs/heads/restricted",
		Message: "restricted change",
		Files: []git.FastImportFile{
			{Path: "restricted.txt", Content: "restricted only\n"},
		},
	}}))

	for _, revision := range []string{
		"refs/heads/restricted",
		"refs/heads/restricted^{tree}",
		"refs/heads/restricted:restricted.txt",
	} {
		objectID, _, err := gitcmd.NewCommand("rev-parse").AddDynamicArguments(revision).WithRepo(restricted).RunStdString(t.Context())
		require.NoError(t, err)
		_, _, err = gitcmd.NewCommand("cat-file", "-e").AddDynamicArguments(strings.TrimSpace(objectID) + "^{object}").WithRepo(public).RunStdString(t.Context())
		assert.Error(t, err, "%s leaked into the public store", revision)
	}

	assertObjectFilesAreDistinct(t, public, restricted)
	assert.ErrorContains(t, ImportPublicBase(t.Context(), public, restricted), "restricted store already exists")
}

func assertBareRepository(t *testing.T, repo gitrepo.RepositoryFacade) {
	t.Helper()

	bare, _, err := gitcmd.NewCommand("rev-parse", "--is-bare-repository").WithRepo(repo).RunStdString(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "true", strings.TrimSpace(bare))
}

func assertEqualRefs(t *testing.T, first, second gitrepo.RepositoryFacade) {
	t.Helper()

	firstRefs, _, err := gitcmd.NewCommand("for-each-ref", "--format=%(refname) %(objectname)").WithRepo(first).RunStdString(t.Context())
	require.NoError(t, err)
	secondRefs, _, err := gitcmd.NewCommand("for-each-ref", "--format=%(refname) %(objectname)").WithRepo(second).RunStdString(t.Context())
	require.NoError(t, err)
	assert.Equal(t, firstRefs, secondRefs)
}

func assertNoAlternates(t *testing.T, repo gitrepo.RepositoryFacade) {
	t.Helper()

	objectsDir := filepath.Join(gitrepo.RepoLocalPath(repo), "objects", "info")
	assert.NoFileExists(t, filepath.Join(objectsDir, "alternates"))
	assert.NoFileExists(t, filepath.Join(objectsDir, "http-alternates"))
}

func assertNoRemote(t *testing.T, repo gitrepo.RepositoryFacade) {
	t.Helper()

	remotes, _, err := gitcmd.NewCommand("remote").WithRepo(repo).RunStdString(t.Context())
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(remotes))
}

func assertObjectDirectoriesDistinct(t *testing.T, first, second gitrepo.RepositoryFacade) {
	t.Helper()

	firstInfo, err := os.Stat(filepath.Join(gitrepo.RepoLocalPath(first), "objects"))
	require.NoError(t, err)
	secondInfo, err := os.Stat(filepath.Join(gitrepo.RepoLocalPath(second), "objects"))
	require.NoError(t, err)
	assert.False(t, os.SameFile(firstInfo, secondInfo))
}

func assertObjectFilesAreDistinct(t *testing.T, first, second gitrepo.RepositoryFacade) {
	t.Helper()

	firstFiles := objectFiles(t, first)
	secondFiles := objectFiles(t, second)
	require.NotEmpty(t, firstFiles)
	require.NotEmpty(t, secondFiles)

	for _, firstFile := range firstFiles {
		for _, secondFile := range secondFiles {
			assert.False(t, os.SameFile(firstFile, secondFile), "object files %q and %q share storage", firstFile.Name(), secondFile.Name())
		}
	}
}

func objectFiles(t *testing.T, repo gitrepo.RepositoryFacade) []fs.FileInfo {
	t.Helper()

	var files []fs.FileInfo
	err := filepath.WalkDir(filepath.Join(gitrepo.RepoLocalPath(repo), "objects"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("object store contains symlink %q", path)
		}
		if !entry.Type().IsRegular() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		files = append(files, info)
		return nil
	})
	require.NoError(t, err)
	return files
}
