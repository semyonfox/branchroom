// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"encoding/base32"
	"strings"
	"testing"

	"gitea.dev/models/unittest"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkspaceValidationAndTransitions(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	workspace := newTestWorkspace()
	require.NoError(t, InsertWorkspace(ctx, workspace))

	stored, err := GetWorkspaceByScopeID(ctx, workspace.ScopeID)
	require.NoError(t, err)
	assert.Equal(t, workspace.WorkspaceKey, stored.WorkspaceKey)
	stored, err = GetWorkspaceByKey(ctx, workspace.WorkspaceKey)
	require.NoError(t, err)
	assert.Equal(t, workspace.ID, stored.ID)
	stored, err = GetWorkspaceByRouteKey(ctx, workspace.RouteKey)
	require.NoError(t, err)
	assert.Equal(t, workspace.ID, stored.ID)
	stored, err = GetWorkspaceByRouteKeyAndRepoID(ctx, workspace.RouteKey, workspace.RepoID)
	require.NoError(t, err)
	assert.Equal(t, workspace.ID, stored.ID)
	_, err = GetWorkspaceByKey(ctx, "not-a-workspace-key")
	assert.ErrorIs(t, err, util.ErrInvalidArgument)
	_, err = GetWorkspaceByRouteKey(ctx, "not-a-workspace-route-key")
	assert.ErrorIs(t, err, util.ErrInvalidArgument)
	_, err = GetWorkspaceByRouteKey(ctx, strings.Repeat("a", workspaceRouteKeyLength))
	assert.ErrorIs(t, err, util.ErrNotExist)
	_, err = GetWorkspaceByRouteKeyAndRepoID(ctx, workspace.RouteKey, workspace.RepoID+1)
	assert.ErrorIs(t, err, util.ErrNotExist)

	updated, err := TransitionWorkspace(ctx, workspace.ID, WorkspaceStateProvisioning, WorkspaceStateReady, "", 0)
	require.NoError(t, err)
	assert.True(t, updated)
	updated, err = TransitionWorkspace(ctx, workspace.ID, WorkspaceStateProvisioning, WorkspaceStateFailed, "import_failed", 0)
	require.NoError(t, err)
	assert.False(t, updated)
	updated, err = TransitionWorkspace(ctx, workspace.ID, WorkspaceStateReady, WorkspaceStateRetired, "", timeutil.TimeStampNow())
	require.NoError(t, err)
	assert.True(t, updated)

	retired, err := GetWorkspaceByID(ctx, workspace.ID)
	require.NoError(t, err)
	assert.Equal(t, WorkspaceStateRetired, retired.State)
	assert.NotZero(t, retired.RetiredUnix)

	duplicateScope := newTestWorkspace()
	duplicateScope.ScopeID = workspace.ScopeID
	assert.Error(t, InsertWorkspace(ctx, duplicateScope))

	invalid := newTestWorkspace()
	invalid.StoreID = "not-a-uuid"
	assert.Error(t, invalid.Validate())
	invalid = newTestWorkspace()
	invalid.ObjectFormatName = "sha256"
	assert.Error(t, invalid.Validate())
	invalid = newTestWorkspace()
	invalid.State = WorkspaceStateFailed
	assert.Error(t, invalid.Validate())
	invalid = newTestWorkspace()
	invalid.RouteKey = "A" + invalid.RouteKey[1:]
	assert.Error(t, invalid.Validate())
	invalid = newTestWorkspace()
	parsed, err := uuid.Parse(invalid.WorkspaceKey)
	require.NoError(t, err)
	invalid.RouteKey = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding).EncodeToString(parsed[:])
	assert.Error(t, invalid.Validate())
	invalid = newTestWorkspace()
	parsed, err = uuid.Parse(invalid.StoreID)
	require.NoError(t, err)
	invalid.RouteKey = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding).EncodeToString(parsed[:])
	assert.Error(t, invalid.Validate())
}

func TestNewWorkspaceRouteKey(t *testing.T) {
	first, err := NewWorkspaceRouteKey()
	require.NoError(t, err)
	second, err := NewWorkspaceRouteKey()
	require.NoError(t, err)
	assert.True(t, IsWorkspaceRouteKey(first))
	assert.True(t, IsWorkspaceRouteKey(second))
	assert.NotEqual(t, first, second)
}

func newTestWorkspace() *Workspace {
	return &Workspace{
		WorkspaceKey:     "11111111-1111-1111-1111-111111111111",
		RouteKey:         "mfrggzdfmztwq2lkmmqgczdmmq",
		ScopeID:          1,
		RepoID:           1,
		StoreID:          "22222222-2222-2222-2222-222222222222",
		PublicBaseSHA:    strings.Repeat("a", 40),
		ObjectFormatName: "sha1",
		State:            WorkspaceStateProvisioning,
		CreatedByID:      1,
	}
}
