// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"gitea.dev/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadBranchroom(t *testing.T) {
	defer test.MockVariableValue(&Branchroom)()
	defer test.MockVariableValue(&RepoRootPath)()
	defer test.MockVariableValue(&AppWorkPath)()

	testRoot := privateBranchroomTestRoot(t)
	publicRoot := filepath.Join(testRoot, "public-repositories")
	require.NoError(t, os.MkdirAll(publicRoot, 0o755))
	RepoRootPath = publicRoot
	AppWorkPath = testRoot

	loadConfig := func(t *testing.T, config string) error {
		t.Helper()
		cfg, err := NewConfigProviderFromData(config)
		require.NoError(t, err)
		return loadBranchroomFrom(cfg)
	}

	t.Run("DisabledByDefault", func(t *testing.T) {
		require.NoError(t, loadConfig(t, ""))
		assert.Equal(t, BranchroomConfig{MaxWorkspacesPerOwner: 10, MaxGitRequestSize: 32 << 20, GitRequestTimeout: 2 * time.Minute}, Branchroom)
	})

	t.Run("DisabledRetainsRestrictedRootForBackupExclusion", func(t *testing.T) {
		require.NoError(t, loadConfig(t, "[branchroom]\nENABLED = false\nRESTRICTED_ROOT = public-repositories\n"))
		assert.Equal(t, BranchroomConfig{MaxWorkspacesPerOwner: 10, MaxGitRequestSize: 32 << 20, GitRequestTimeout: 2 * time.Minute, RestrictedRepoRootPath: publicRoot}, Branchroom)
	})

	if !branchroomRestrictedStoreIntegritySupported() {
		t.Run("EnabledRequiresUnix", func(t *testing.T) {
			err := loadConfig(t, "[branchroom]\nENABLED = true\n")
			assert.ErrorContains(t, err, "[branchroom] requires Unix filesystem ownership checks")
		})
		return
	}

	t.Run("EnabledRequiresRoot", func(t *testing.T) {
		err := loadConfig(t, "[branchroom]\nENABLED = true\n")
		assert.ErrorContains(t, err, "[branchroom].RESTRICTED_ROOT must be set")
	})

	t.Run("EnabledRequiresExistingRoot", func(t *testing.T) {
		err := loadConfig(t, "[branchroom]\nENABLED = true\nRESTRICTED_ROOT = missing-root\n")
		assert.ErrorContains(t, err, "[branchroom].RESTRICTED_ROOT must be an existing directory")
	})

	t.Run("EnabledResolvesRelativeRoot", func(t *testing.T) {
		restrictedRoot := filepath.Join(testRoot, "restricted-repositories")
		require.NoError(t, os.MkdirAll(restrictedRoot, 0o700))

		require.NoError(t, loadConfig(t, "[branchroom]\nENABLED = true\nRESTRICTED_ROOT = restricted-repositories\n"))
		assert.Equal(t, BranchroomConfig{
			Enabled:                true,
			RestrictedRepoRootPath: restrictedRoot,
			MaxWorkspacesPerOwner:  10,
			MaxGitRequestSize:      32 << 20,
			GitRequestTimeout:      2 * time.Minute,
		}, Branchroom)
	})

	t.Run("FreshInstallWithoutPublicRoot", func(t *testing.T) {
		defer test.MockVariableValue(&RepoRootPath, filepath.Join(testRoot, "fresh", "repositories"))()
		require.NoError(t, loadConfig(t, "[branchroom]\nENABLED = true\nRESTRICTED_ROOT = restricted-repositories\n"))

		inside := filepath.Join(testRoot, "restricted-repositories", "missing-public")
		RepoRootPath = inside
		err := loadConfig(t, "[branchroom]\nENABLED = true\nRESTRICTED_ROOT = restricted-repositories\n")
		assert.ErrorContains(t, err, "must not overlap [repository].ROOT")
	})

	t.Run("EnabledRequiresPrivateRestrictedRoot", func(t *testing.T) {
		restrictedRoot := filepath.Join(testRoot, "publicly-readable-restricted-repositories")
		require.NoError(t, os.MkdirAll(restrictedRoot, 0o700))
		require.NoError(t, os.Chmod(restrictedRoot, 0o755))

		err := loadConfig(t, "[branchroom]\nENABLED = true\nRESTRICTED_ROOT = "+restrictedRoot+"\n")
		assert.ErrorContains(t, err, "must deny group and other access")
	})

	if runtime.GOOS != "windows" {
		t.Run("EnabledRejectsWritableParent", func(t *testing.T) {
			parent := filepath.Join(testRoot, "writable-parent")
			restrictedRoot := filepath.Join(parent, "restricted-repositories")
			require.NoError(t, os.MkdirAll(restrictedRoot, 0o700))
			require.NoError(t, os.Chmod(parent, 0o777))

			err := loadConfig(t, "[branchroom]\nENABLED = true\nRESTRICTED_ROOT = "+restrictedRoot+"\n")
			assert.ErrorContains(t, err, "must not be below a group- or world-writable directory")
		})
	}

	for _, testCase := range []struct {
		name string
		root string
	}{
		{
			name: "EqualRoot",
			root: publicRoot,
		},
		{
			name: "RootInsidePublicRoot",
			root: filepath.Join(publicRoot, "restricted"),
		},
		{
			name: "PublicRootInsideRoot",
			root: testRoot,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.NoError(t, os.MkdirAll(testCase.root, 0o755))

			err := loadConfig(t, "[branchroom]\nENABLED = true\nRESTRICTED_ROOT = "+testCase.root+"\n")
			assert.ErrorContains(t, err, "must not overlap [repository].ROOT")
		})
	}

	t.Run("AcceptsRootUnderDefaultStaticRoot", func(t *testing.T) {
		defer test.MockVariableValue(&StaticRootPath, AppWorkPath)()
		defer test.MockVariableValue(&CustomPath, filepath.Join(AppWorkPath, "custom"))()
		restricted := filepath.Join(testRoot, "data", "branchroom")
		require.NoError(t, os.MkdirAll(restricted, 0o700))
		require.NoError(t, os.Chmod(filepath.Dir(restricted), 0o700))
		require.NoError(t, loadConfig(t, "[branchroom]\nENABLED = true\nRESTRICTED_ROOT = data/branchroom\n"))

		served := filepath.Join(testRoot, "public", "branchroom")
		require.NoError(t, os.MkdirAll(served, 0o700))
		require.NoError(t, os.Chmod(filepath.Dir(served), 0o700))
		err := loadConfig(t, "[branchroom]\nENABLED = true\nRESTRICTED_ROOT = public/branchroom\n")
		assert.ErrorContains(t, err, "must not overlap local storage")
	})

	t.Run("RejectsEditableGitHooks", func(t *testing.T) {
		defer test.MockVariableValue(&DisableGitHooks, false)()
		err := loadConfig(t, "[branchroom]\nENABLED = true\n")
		assert.ErrorContains(t, err, "requires [security] DISABLE_GIT_HOOKS")
	})

	t.Run("GitRequestLimits", func(t *testing.T) {
		restrictedRoot := filepath.Join(testRoot, "restricted-repositories")
		require.NoError(t, os.MkdirAll(restrictedRoot, 0o700))
		base := "[branchroom]\nENABLED = true\nRESTRICTED_ROOT = restricted-repositories\n"
		require.NoError(t, loadConfig(t, base))
		assert.EqualValues(t, 32<<20, Branchroom.GitRequestSizeLimit())
		assert.Equal(t, 2*time.Minute, Branchroom.GitRequestTimeLimit())
		require.NoError(t, loadConfig(t, base+"MAX_GIT_REQUEST_SIZE = 1048576\nGIT_REQUEST_TIMEOUT = 30s\n"))
		assert.EqualValues(t, 1<<20, Branchroom.GitRequestSizeLimit())
		assert.Equal(t, 30*time.Second, Branchroom.GitRequestTimeLimit())
		assert.ErrorContains(t, loadConfig(t, base+"MAX_GIT_REQUEST_SIZE = 0\n"), "MAX_GIT_REQUEST_SIZE")
		assert.ErrorContains(t, loadConfig(t, base+"GIT_REQUEST_TIMEOUT = -1s\n"), "GIT_REQUEST_TIMEOUT")
	})

	t.Run("RejectsFastCGI", func(t *testing.T) {
		defer test.MockVariableValue(&Protocol, FCGI)()
		err := loadConfig(t, "[branchroom]\nENABLED = true\n")
		assert.ErrorContains(t, err, "does not support FastCGI")
	})

	t.Run("RejectsSymlinkAlias", func(t *testing.T) {
		alias := filepath.Join(testRoot, "public-alias")
		if err := os.Symlink(publicRoot, alias); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}

		err := loadConfig(t, "[branchroom]\nENABLED = true\nRESTRICTED_ROOT = "+alias+"\n")
		assert.ErrorContains(t, err, "must not overlap [repository].ROOT")
	})
}

func privateBranchroomTestRoot(t *testing.T) string {
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

func TestBranchroomRejectsMSSQL(t *testing.T) {
	defer test.MockVariableValue(&Branchroom)()
	cfg, err := NewConfigProviderFromData("[database]\nDB_TYPE = mssql\n[branchroom]\nENABLED = true\n")
	require.NoError(t, err)
	require.ErrorContains(t, loadBranchroomFrom(cfg), "does not support MSSQL")
}

func TestBranchroomSeparateRoots(t *testing.T) {
	root := privateBranchroomTestRoot(t)
	restricted := filepath.Join(root, "restricted")
	require.NoError(t, os.Mkdir(restricted, 0o700))
	alias := filepath.Join(root, "alias")
	require.NoError(t, os.Symlink(restricted, alias))
	for _, path := range []string{restricted, filepath.Join(restricted, "new", "storage"), alias, filepath.Join(alias, "new"), root} {
		require.ErrorContains(t, validateBranchroomSeparateRoot(restricted, path), "must not overlap")
	}
	require.NoError(t, validateBranchroomSeparateRoot(restricted, filepath.Join(root, "other", "new")))
}
