// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRestrictedRepository(t *testing.T) {
	defer test.MockVariableValue(&setting.Branchroom)()

	_, err := RestrictedRepository("a13e2a27-167d-4586-a511-42910dc3a955")
	assert.ErrorContains(t, err, "branchroom is disabled")

	root := newSafeTestRoot(t)
	setting.Branchroom = setting.BranchroomConfig{
		Enabled:                true,
		RestrictedRepoRootPath: root,
	}

	repo, err := RestrictedRepository("a13e2a27-167d-4586-a511-42910dc3a955")
	require.NoError(t, err)
	assert.Equal(t, "branchroom-store-a13e2a27-167d-4586-a511-42910dc3a955", repo.GitRepoManagedID())
	assert.Equal(t, filepath.Join(root, "a13e2a27-167d-4586-a511-42910dc3a955.git"), repo.GitRepoLocation())

	_, err = RestrictedRepository("")
	assert.ErrorContains(t, err, "restricted store ID must be a canonical UUID")

	_, err = RestrictedRepository("../public")
	assert.ErrorContains(t, err, "restricted store ID must be a canonical UUID")

	for _, id := range []string{"../public", "not-a-uuid", "{a13e2a27-167d-4586-a511-42910dc3a955}", strings.ToUpper("a13e2a27-167d-4586-a511-42910dc3a955")} {
		_, err := RestrictedRepository(id)
		assert.ErrorContains(t, err, "canonical UUID")
	}

	require.NoError(t, os.Chmod(root, 0o755))
	_, err = RestrictedRepository("a13e2a27-167d-4586-a511-42910dc3a955")
	assert.ErrorIs(t, err, ErrUnsafeRestrictedRoot)
}
