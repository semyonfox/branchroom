// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package git

import (
	"os"
	"path/filepath"
	"testing"

	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/git/gitrepo"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getLocalConfigValuesNull(t *testing.T, repo RepositoryFacade, key string) string {
	t.Helper()
	stdout, _, err := gitcmd.NewCommand("config", "--local", "--get-all", "--null").
		AddDynamicArguments(key).WithRepo(repo).RunStdBytes(t.Context())
	require.NoError(t, err)
	return string(stdout)
}

func initBareRepoForTest(t *testing.T, name string) RepositoryFacade {
	t.Helper()
	repoDir := filepath.Join(t.TempDir(), "repo.git")
	require.NoError(t, gitcmd.NewCommand("init", "--bare").AddDynamicArguments(repoDir).Run(t.Context()))
	return gitrepo.RepositoryManaged(name, repoDir)
}

func TestManagedConfigSetAndVerify(t *testing.T) {
	ctx := t.Context()
	repo := initBareRepoForTest(t, "test-managed-config-set-and-verify")

	key := "core.hooksPath"
	for _, value := range []string{"/hooks/a", "/hooks/b"} {
		require.NoError(t, gitcmd.NewCommand("config", "--local", "--add").
			AddDynamicArguments(key, value).WithRepo(repo).Run(ctx))
	}
	assert.Equal(t, "/hooks/a\x00/hooks/b\x00", getLocalConfigValuesNull(t, repo, key))

	require.NoError(t, ManagedConfigSetAndVerify(ctx, repo, key, "/hooks/final"))
	assert.Equal(t, "/hooks/final\x00", getLocalConfigValuesNull(t, repo, key))

	// setting again must keep exactly one value
	require.NoError(t, ManagedConfigSetAndVerify(ctx, repo, key, "/hooks/final"))
	assert.Equal(t, "/hooks/final\x00", getLocalConfigValuesNull(t, repo, key))
}

func TestManagedConfigSetAndVerifyAbsentKey(t *testing.T) {
	ctx := t.Context()
	repo := initBareRepoForTest(t, "test-managed-config-absent-key")

	require.NoError(t, ManagedConfigSetAndVerify(ctx, repo, "core.hooksPath", "/hooks/only"))
	assert.Equal(t, "/hooks/only\x00", getLocalConfigValuesNull(t, repo, "core.hooksPath"))
}

func TestManagedConfigSetAndVerifyIgnoresGlobalConfig(t *testing.T) {
	ctx := t.Context()
	repo := initBareRepoForTest(t, "test-managed-config-ignores-global")

	globalCfg := filepath.Join(t.TempDir(), "global.gitconfig")
	require.NoError(t, os.WriteFile(globalCfg, []byte("[core]\n\thooksPath = /hooks/global\n"), 0o600))
	t.Setenv("GIT_CONFIG_GLOBAL", globalCfg)

	require.NoError(t, ManagedConfigSetAndVerify(ctx, repo, "core.hooksPath", "/hooks/local"))
	assert.Equal(t, "/hooks/local\x00", getLocalConfigValuesNull(t, repo, "core.hooksPath"))
}

func TestManagedConfigSetAndVerifyErroringFacade(t *testing.T) {
	err := ManagedConfigSetAndVerify(t.Context(),
		gitrepo.RepositoryManaged("test-managed-config-erroring", filepath.Join(t.TempDir(), "missing.git")),
		"core.hooksPath", "/hooks/final")
	require.Error(t, err)
}
