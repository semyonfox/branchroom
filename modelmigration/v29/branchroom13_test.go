// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"testing"

	"gitea.dev/modelmigration/migrationtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddPathReviewerRules(t *testing.T) {
	x, cleanup := migrationtest.PrepareTestEnv(t, 0)
	defer cleanup()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, AddPathReviewerRules(t.Context(), x))
	type BranchroomPathReviewer struct {
		ID                int64 `xorm:"pk autoincr"`
		RepoID            int64
		ProtectedBranchID int64
		Pattern           string
		TeamID            int64
		CodeOwners        bool
		RequiredApprovals int64
	}
	original := &BranchroomPathReviewer{RepoID: 1, ProtectedBranchID: 2, Pattern: "src/**", TeamID: 3, RequiredApprovals: 2}
	_, err := x.Insert(original)
	require.NoError(t, err)
	require.NoError(t, AddPathReviewerRules(t.Context(), x))
	var got BranchroomPathReviewer
	has, err := x.ID(original.ID).Get(&got)
	require.NoError(t, err)
	require.True(t, has)
	assert.Equal(t, *original, got)
}
