// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v28

import (
	"testing"

	"gitea.dev/modelmigration/migrationtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddBranchroomWorkspaceSchema(t *testing.T) {
	x, deferable := migrationtest.PrepareTestEnv(t, 0)
	defer deferable()
	if x == nil || t.Failed() {
		return
	}

	require.NoError(t, AddBranchroomWorkspaceSchema(t.Context(), x))
	require.NoError(t, AddBranchroomWorkspaceSchema(t.Context(), x))

	table := migrationtest.LoadTableSchemasMap(t, x)["branchroom_workspace"]
	require.NotNil(t, table)
	for _, column := range []string{
		"id", "workspace_key", "scope_id", "repo_id", "store_id", "public_base_sha", "object_format_name", "state", "failure_code", "created_by_id", "retired_unix",
	} {
		assert.Contains(t, table.ColumnsSeq(), column)
	}

	indexes, err := x.Dialect().GetIndexes(x.DB(), t.Context(), "branchroom_workspace")
	require.NoError(t, err)
	assert.True(t, hasBranchroomIndex(indexes, []string{"scope_id"}, true))
	assert.True(t, hasBranchroomIndex(indexes, []string{"workspace_key"}, true))
	assert.True(t, hasBranchroomIndex(indexes, []string{"store_id"}, true))
	assert.True(t, hasBranchroomIndex(indexes, []string{"repo_id", "state"}, false))
}
