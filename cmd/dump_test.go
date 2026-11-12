// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchroomDumpExcludesSymlinkedDataPath(t *testing.T) {
	disk, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	restricted := filepath.Join(disk, "branchroom")
	require.NoError(t, os.Mkdir(restricted, 0o700))
	dataPath := filepath.Join(t.TempDir(), "data")
	require.NoError(t, os.Symlink(disk, dataPath))
	other := t.TempDir()

	excludes := branchroomDumpExcludes(restricted, dataPath, other, filepath.Join(other, "missing"))
	assert.ElementsMatch(t, []string{restricted, filepath.Join(dataPath, "branchroom")}, excludes)
}
