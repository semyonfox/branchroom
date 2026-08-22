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

func TestRotateWorkspaceRouteKey(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()

	workspace := newTestWorkspace()
	workspace.State = WorkspaceStateReady
	require.NoError(t, InsertWorkspace(ctx, workspace))
	current, err := GetWorkspaceByID(ctx, workspace.ID)
	require.NoError(t, err)

	oldKey := current.RouteKey
	newKey := strings.Repeat("b", workspaceRouteKeyLength)
	rotated, err := RotateWorkspaceRouteKey(ctx, current, newKey)
	require.NoError(t, err)
	assert.True(t, rotated)
	assert.Equal(t, oldKey, current.RouteKey)

	_, err = GetWorkspaceByRouteKey(ctx, oldKey)
	assert.ErrorIs(t, err, util.ErrNotExist)
	rotatedWorkspace, err := GetWorkspaceByRouteKeyAndRepoID(ctx, newKey, workspace.RepoID)
	require.NoError(t, err)
	assert.Equal(t, workspace.ID, rotatedWorkspace.ID)

	rotated, err = RotateWorkspaceRouteKey(ctx, current, strings.Repeat("c", workspaceRouteKeyLength))
	require.NoError(t, err)
	assert.False(t, rotated)

	for _, key := range []string{"not-a-workspace-route-key", newKey} {
		rotated, err = RotateWorkspaceRouteKey(ctx, rotatedWorkspace, key)
		assert.ErrorIs(t, err, util.ErrInvalidArgument)
		assert.False(t, rotated)
	}

	parsed, err := uuid.Parse(rotatedWorkspace.WorkspaceKey)
	require.NoError(t, err)
	correlatingKey := workspaceRouteKeyEncoding.EncodeToString(parsed[:])
	rotated, err = RotateWorkspaceRouteKey(ctx, rotatedWorkspace, correlatingKey)
	assert.ErrorIs(t, err, util.ErrInvalidArgument)
	assert.False(t, rotated)

	nonReady := *rotatedWorkspace
	nonReady.State = WorkspaceStateProvisioning
	rotated, err = RotateWorkspaceRouteKey(ctx, &nonReady, strings.Repeat("d", workspaceRouteKeyLength))
	assert.ErrorIs(t, err, util.ErrInvalidArgument)
	assert.False(t, rotated)

	invalidCurrent := *rotatedWorkspace
	invalidCurrent.StoreID = "not-a-uuid"
	rotated, err = RotateWorkspaceRouteKey(ctx, &invalidCurrent, strings.Repeat("d", workspaceRouteKeyLength))
	assert.ErrorIs(t, err, util.ErrInvalidArgument)
	assert.False(t, rotated)

	other := newTestWorkspace()
	other.WorkspaceKey = "33333333-3333-3333-3333-333333333333"
	other.RouteKey = strings.Repeat("e", workspaceRouteKeyLength)
	other.ScopeID = 2
	other.StoreID = "44444444-4444-4444-4444-444444444444"
	other.State = WorkspaceStateReady
	require.NoError(t, InsertWorkspace(ctx, other))

	rotated, err = RotateWorkspaceRouteKey(ctx, rotatedWorkspace, other.RouteKey)
	assert.Error(t, err)
	assert.False(t, rotated)
	stillRotated, err := GetWorkspaceByRouteKey(ctx, newKey)
	require.NoError(t, err)
	assert.Equal(t, workspace.ID, stillRotated.ID)
	collision, err := GetWorkspaceByRouteKey(ctx, other.RouteKey)
	require.NoError(t, err)
	assert.Equal(t, other.ID, collision.ID)
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
