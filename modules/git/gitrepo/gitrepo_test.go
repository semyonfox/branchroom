// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package gitrepo

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepositoryManagedInRoot(t *testing.T) {
	root := t.TempDir()

	repo, err := RepositoryManagedInRoot("branchroom-store-1", root, "store-1.git")
	require.NoError(t, err)
	assert.Equal(t, "branchroom-store-1", repo.GitRepoManagedID())
	assert.Equal(t, filepath.Join(root, "store-1.git"), repo.GitRepoLocation())
	assert.Equal(t, filepath.Join(root, "store-1.git"), RepoLocalPath(repo))

	for _, testCase := range []struct {
		name         string
		id           string
		root         string
		relativePath string
	}{
		{
			name:         "EmptyID",
			root:         root,
			relativePath: "store-1.git",
		},
		{
			name:         "RelativeRoot",
			id:           "branchroom-store-1",
			root:         "restricted",
			relativePath: "store-1.git",
		},
		{
			name:         "AbsoluteRepositoryPath",
			id:           "branchroom-store-1",
			root:         root,
			relativePath: filepath.Join(root, "store-1.git"),
		},
		{
			name:         "ParentRepositoryPath",
			id:           "branchroom-store-1",
			root:         root,
			relativePath: "../store-1.git",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := RepositoryManagedInRoot(testCase.id, testCase.root, testCase.relativePath)
			assert.Error(t, err)
		})
	}
}
