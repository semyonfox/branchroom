// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v28

import (
	"strings"
	"testing"

	"gitea.dev/modelmigration/migrationtest"
	"gitea.dev/modules/timeutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type branchroomWorkspaceBeforeRouteKeyMigration struct {
	ID               int64              `xorm:"pk autoincr"`
	WorkspaceKey     string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	ScopeID          int64              `xorm:"UNIQUE NOT NULL"`
	RepoID           int64              `xorm:"INDEX(repo_state) NOT NULL"`
	StoreID          string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	PublicBaseSHA    string             `xorm:"VARCHAR(64) NOT NULL"`
	ObjectFormatName string             `xorm:"VARCHAR(6) NOT NULL"`
	State            string             `xorm:"VARCHAR(16) INDEX(repo_state) NOT NULL"`
	FailureCode      string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	CreatedByID      int64              `xorm:"NOT NULL"`
	RetiredUnix      timeutil.TimeStamp `xorm:"INDEX NOT NULL DEFAULT 0"`
	CreatedUnix      timeutil.TimeStamp `xorm:"created NOT NULL"`
	UpdatedUnix      timeutil.TimeStamp `xorm:"updated NOT NULL"`
}

func (branchroomWorkspaceBeforeRouteKeyMigration) TableName() string { return "branchroom_workspace" }

func TestAddBranchroomWorkspaceRouteKey(t *testing.T) {
	x, deferable := migrationtest.PrepareTestEnv(t, 0, new(branchroomWorkspaceBeforeRouteKeyMigration))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}

	workspaces := []*branchroomWorkspaceBeforeRouteKeyMigration{
		{
			WorkspaceKey:     "11111111-1111-1111-1111-111111111111",
			ScopeID:          1,
			RepoID:           1,
			StoreID:          "22222222-2222-2222-2222-222222222222",
			PublicBaseSHA:    strings.Repeat("a", 40),
			ObjectFormatName: "sha1",
			State:            "ready",
			CreatedByID:      1,
		},
		{
			WorkspaceKey:     "33333333-3333-3333-3333-333333333333",
			ScopeID:          2,
			RepoID:           1,
			StoreID:          "44444444-4444-4444-4444-444444444444",
			PublicBaseSHA:    strings.Repeat("b", 40),
			ObjectFormatName: "sha1",
			State:            "provisioning",
			CreatedByID:      2,
		},
	}
	for _, workspace := range workspaces {
		_, err := x.Insert(workspace)
		require.NoError(t, err)
	}

	require.NoError(t, AddBranchroomWorkspaceRouteKey(t.Context(), x))

	table := migrationtest.LoadTableSchemasMap(t, x)["branchroom_workspace"]
	require.NotNil(t, table)
	assert.Contains(t, table.ColumnsSeq(), "route_key")

	var migrated []*branchroomWorkspaceRouteKeyBackfill
	require.NoError(t, x.Table("branchroom_workspace").Asc("id").Find(&migrated))
	require.Len(t, migrated, len(workspaces))
	keys := make(map[string]struct{}, len(migrated))
	for _, workspace := range migrated {
		assert.Len(t, workspace.RouteKey, branchroomWorkspaceRouteKeyLength)
		assert.Regexp(t, "^[a-z2-7]+$", workspace.RouteKey)
		assert.Equal(t, strings.ToLower(workspace.RouteKey), workspace.RouteKey)
		keys[workspace.RouteKey] = struct{}{}
	}
	assert.Len(t, keys, len(migrated))
	assert.Equal(t, workspaces[0].WorkspaceKey, migrated[0].WorkspaceKey)
	assert.Equal(t, workspaces[1].StoreID, migrated[1].StoreID)
	keysBeforeRerun := make([]string, len(migrated))
	for i, workspace := range migrated {
		keysBeforeRerun[i] = workspace.RouteKey
	}
	require.NoError(t, AddBranchroomWorkspaceRouteKey(t.Context(), x))
	migrated = nil
	require.NoError(t, x.Table("branchroom_workspace").Asc("id").Find(&migrated))
	for i, workspace := range migrated {
		assert.Equal(t, keysBeforeRerun[i], workspace.RouteKey)
	}

	indexes, err := x.Dialect().GetIndexes(x.DB(), t.Context(), "branchroom_workspace")
	require.NoError(t, err)
	assert.True(t, hasBranchroomIndex(indexes, []string{"route_key"}, true))

	_, err = x.Exec("INSERT INTO branchroom_workspace (workspace_key, route_key, scope_id, repo_id, store_id, public_base_sha, object_format_name, state, failure_code, created_by_id, retired_unix) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)", "55555555-5555-5555-5555-555555555555", migrated[0].RouteKey, 3, 1, "66666666-6666-6666-6666-666666666666", strings.Repeat("c", 40), "sha1", "ready", "", 3, 0)
	assert.Error(t, err)
}
