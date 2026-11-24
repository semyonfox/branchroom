// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package cmd

import (
	"archive/zip"
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"gitea.dev/modules/dump"

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

func TestDumpBranchroomStoresKeepsModes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "restricted")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".branchroom-hooks", "id"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".branchroom-hooks", "id", "pre-receive"), []byte("#!/bin/sh\n"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "id.git"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "id.git", "config"), nil, 0o644))
	require.NoError(t, os.Chmod(root, 0o700))

	var buf bytes.Buffer
	dumper, err := dump.NewDumper(t.Context(), "zip", &buf)
	require.NoError(t, err)
	dumper.GlobalExcludeAbsPath(root)
	require.NoError(t, dumpBranchroomStores(dumper, root))
	require.NoError(t, dumper.Close())

	archive, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	modes := map[string]fs.FileMode{}
	for _, file := range archive.File {
		modes[file.Name] = file.Mode()
	}
	assert.Equal(t, map[string]fs.FileMode{
		"branchroom/":                                 fs.ModeDir | 0o700,
		"branchroom/.branchroom-hooks/":               fs.ModeDir | 0o700,
		"branchroom/.branchroom-hooks/id/":            fs.ModeDir | 0o700,
		"branchroom/.branchroom-hooks/id/pre-receive": 0o700,
		"branchroom/id.git/":                          fs.ModeDir | 0o755,
		"branchroom/id.git/config":                    0o644,
	}, modes)
}
