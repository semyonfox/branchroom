// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"strings"
	"testing"

	"gitea.dev/modelmigration/migrationtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddBranchroomReceiveSessionSchema(t *testing.T) {
	x, deferable := migrationtest.PrepareTestEnv(t, 0)
	defer deferable()
	if x == nil || t.Failed() {
		return
	}

	require.NoError(t, AddBranchroomReceiveSessionSchema(t.Context(), x))
	require.NoError(t, AddBranchroomReceiveSessionSchema(t.Context(), x))

	table := migrationtest.LoadTableSchemasMap(t, x)["branchroom_receive_session"]
	require.NotNil(t, table)
	for _, column := range []string{
		"id", "session_key", "bearer_secret_hash", "scope_id", "workspace_id", "repo_id", "actor_type", "actor_id", "state", "issued_unix", "expires_unix", "claimed_unix",
	} {
		assert.Contains(t, table.ColumnsSeq(), column)
	}

	indexes, err := x.Dialect().GetIndexes(x.DB(), t.Context(), "branchroom_receive_session")
	require.NoError(t, err)
	assert.True(t, hasBranchroomIndex(indexes, []string{"session_key"}, true))
	assert.True(t, hasBranchroomIndex(indexes, []string{"bearer_secret_hash"}, true))
	assert.True(t, hasBranchroomIndex(indexes, []string{"scope_id", "state"}, false))
	assert.True(t, hasBranchroomIndex(indexes, []string{"repo_id", "state"}, false))
	assert.True(t, hasBranchroomIndex(indexes, []string{"actor_type", "actor_id", "state"}, false))
	assert.True(t, hasBranchroomIndex(indexes, []string{"state", "expires_unix"}, false))

	_, err = x.Insert(&branchroomReceiveSessionMigration{
		SessionKey:       "11111111-1111-1111-1111-111111111111",
		BearerSecretHash: strings.Repeat("a", 64),
		ScopeID:          1,
		WorkspaceID:      1,
		RepoID:           1,
		ActorType:        "user",
		ActorID:          2,
		State:            "issued",
		IssuedUnix:       100,
		ExpiresUnix:      200,
	})
	require.NoError(t, err)

	_, err = x.Insert(&branchroomReceiveSessionMigration{
		SessionKey:       "11111111-1111-1111-1111-111111111111",
		BearerSecretHash: strings.Repeat("b", 64),
		ScopeID:          1,
		WorkspaceID:      1,
		RepoID:           1,
		ActorType:        "user",
		ActorID:          2,
		State:            "issued",
		IssuedUnix:       100,
		ExpiresUnix:      200,
	})
	assert.Error(t, err)

	_, err = x.Insert(&branchroomReceiveSessionMigration{
		SessionKey:       "22222222-2222-2222-2222-222222222222",
		BearerSecretHash: strings.Repeat("a", 64),
		ScopeID:          1,
		WorkspaceID:      1,
		RepoID:           1,
		ActorType:        "user",
		ActorID:          2,
		State:            "issued",
		IssuedUnix:       100,
		ExpiresUnix:      200,
	})
	assert.Error(t, err)
}
