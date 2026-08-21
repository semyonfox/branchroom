// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"os"
	"path/filepath"
	"testing"

	"gitea.dev/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadBranchroom(t *testing.T) {
	defer test.MockVariableValue(&Branchroom)()
	defer test.MockVariableValue(&RepoRootPath)()
	defer test.MockVariableValue(&AppWorkPath)()

	testRoot := t.TempDir()
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
		assert.Equal(t, BranchroomConfig{}, Branchroom)
	})

	t.Run("DisabledIgnoresRestrictedRoot", func(t *testing.T) {
		require.NoError(t, loadConfig(t, "[branchroom]\nENABLED = false\nRESTRICTED_ROOT = public-repositories\n"))
		assert.Equal(t, BranchroomConfig{}, Branchroom)
	})

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
		require.NoError(t, os.MkdirAll(restrictedRoot, 0o755))

		require.NoError(t, loadConfig(t, "[branchroom]\nENABLED = true\nRESTRICTED_ROOT = restricted-repositories\n"))
		assert.Equal(t, BranchroomConfig{
			Enabled:                true,
			RestrictedRepoRootPath: restrictedRoot,
		}, Branchroom)
	})

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

	t.Run("RejectsSymlinkAlias", func(t *testing.T) {
		alias := filepath.Join(testRoot, "public-alias")
		if err := os.Symlink(publicRoot, alias); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}

		err := loadConfig(t, "[branchroom]\nENABLED = true\nRESTRICTED_ROOT = "+alias+"\n")
		assert.ErrorContains(t, err, "must not overlap [repository].ROOT")
	})
}
