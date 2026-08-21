// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"path/filepath"
	"testing"

	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestrictedRepository(t *testing.T) {
	defer test.MockVariableValue(&setting.Branchroom)()

	_, err := RestrictedRepository("security-fix-2026-14")
	assert.ErrorContains(t, err, "branchroom is disabled")

	root := t.TempDir()
	setting.Branchroom = setting.BranchroomConfig{
		Enabled:                true,
		RestrictedRepoRootPath: root,
	}

	repo, err := RestrictedRepository("security-fix-2026-14")
	require.NoError(t, err)
	assert.Equal(t, "branchroom-store-security-fix-2026-14", repo.GitRepoManagedID())
	assert.Equal(t, filepath.Join(root, "security-fix-2026-14.git"), repo.GitRepoLocation())

	_, err = RestrictedRepository("")
	assert.ErrorContains(t, err, "restricted store ID must not be empty")

	_, err = RestrictedRepository("../public")
	assert.ErrorContains(t, err, "repository path must be relative")
}
