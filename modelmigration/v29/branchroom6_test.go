// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"strings"
	"testing"

	"gitea.dev/modelmigration/migrationtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm/schemas"
)

func TestAddBranchroomReceiveSessionOutcomes(t *testing.T) {
	x, deferable := migrationtest.PrepareTestEnv(t, 0, new(branchroomReceiveSessionMigration))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}

	rows := []*branchroomReceiveSessionMigration{
		{SessionKey: "11111111-1111-1111-1111-111111111111", BearerSecretHash: strings.Repeat("a", 64), ScopeID: 1, WorkspaceID: 1, RepoID: 1, ActorType: "user", ActorID: 2, State: "issued", IssuedUnix: 100, ExpiresUnix: 200},
		{SessionKey: "22222222-2222-2222-2222-222222222222", BearerSecretHash: strings.Repeat("b", 64), ScopeID: 1, WorkspaceID: 1, RepoID: 1, ActorType: "user", ActorID: 2, State: "claimed", IssuedUnix: 100, ExpiresUnix: 200, ClaimedUnix: 150},
		{SessionKey: "33333333-3333-3333-3333-333333333333", BearerSecretHash: strings.Repeat("c", 64), ScopeID: 1, WorkspaceID: 1, RepoID: 1, ActorType: "user", ActorID: 2, State: "expired", IssuedUnix: 100, ExpiresUnix: 200},
		{SessionKey: "44444444-4444-4444-4444-444444444444", BearerSecretHash: strings.Repeat("d", 64), ScopeID: 1, WorkspaceID: 1, RepoID: 1, ActorType: "user", ActorID: 2, State: "revoked", IssuedUnix: 100, ExpiresUnix: 200},
		{SessionKey: "55555555-5555-5555-5555-555555555555", BearerSecretHash: strings.Repeat("e", 64), ScopeID: 1, WorkspaceID: 1, RepoID: 1, ActorType: "user", ActorID: 2, State: "completed", IssuedUnix: 100, ExpiresUnix: 200, ClaimedUnix: 150},
	}
	for _, row := range rows {
		_, err := x.Insert(row)
		require.NoError(t, err)
	}

	require.NoError(t, AddBranchroomReceiveSessionOutcomes(t.Context(), x))

	table := migrationtest.LoadTableSchemasMap(t, x)["branchroom_receive_session"]
	require.NotNil(t, table)
	var outcomeColumn *schemas.Column
	for _, column := range table.Columns() {
		if column.Name == "outcome" {
			outcomeColumn = column
		}
	}
	require.NotNil(t, outcomeColumn)
	assert.Contains(t, outcomeColumn.Default, "pending")

	findRows := func() []*branchroomReceiveSessionOutcomeBackfill {
		migrated := make([]*branchroomReceiveSessionOutcomeBackfill, 0)
		require.NoError(t, x.Table("branchroom_receive_session").Asc("id").Find(&migrated))
		return migrated
	}
	assertBackfill := func(sessions []*branchroomReceiveSessionOutcomeBackfill) {
		require.Len(t, sessions, len(rows))
		expectedStates := []string{"issued", "claimed", "expired", "revoked", "completed"}
		expectedOutcomes := []string{"pending", "pending", "expired", "revoked", "legacy"}
		for i, session := range sessions {
			assert.Equal(t, expectedStates[i], session.State)
			assert.Equal(t, expectedOutcomes[i], session.Outcome)
		}
	}
	sessions := findRows()
	assertBackfill(sessions)

	// rerun must not change anything
	require.NoError(t, AddBranchroomReceiveSessionOutcomes(t.Context(), x))
	sessionsAfterRerun := findRows()
	assert.Equal(t, sessions, sessionsAfterRerun)

	for _, outcome := range []string{"denied", "admitted", "accepted", "failed"} {
		_, err := x.Table("branchroom_receive_session").ID(rows[4].ID).
			Cols("outcome").
			Update(&branchroomReceiveSessionOutcomeBackfill{Outcome: outcome})
		require.NoError(t, err)
		require.NoError(t, AddBranchroomReceiveSessionOutcomes(t.Context(), x))
		sessions = findRows()
		require.Len(t, sessions, len(rows))
		assert.Equal(t, outcome, sessions[4].Outcome)
		assert.Equal(t, "revoked", sessions[3].Outcome)
	}

	// new rows fall back to the pending default
	_, err := x.Insert(&branchroomReceiveSessionMigration{
		SessionKey:       "66666666-6666-6666-6666-666666666666",
		BearerSecretHash: strings.Repeat("f", 64),
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
	sessions = findRows()
	require.Len(t, sessions, len(rows)+1)
	assert.Equal(t, "pending", sessions[5].Outcome)
}
