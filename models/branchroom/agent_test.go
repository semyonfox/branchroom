// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"testing"

	"gitea.dev/models/unittest"
	api "gitea.dev/modules/structs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentIdentity(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	identity := &api.AgentIdentity{Agent: "Opus 5.5", TaskURL: "https://example.org/tasks/1", Sponsor: "user2"}
	require.NoError(t, InsertTokenIdentity(ctx, 123, identity))
	require.NoError(t, RecordAgentAction(ctx, "access-token:123", "comment", 42))
	require.NoError(t, RecordAgentAction(ctx, "", "comment", 43))
	got, err := GetAgentAction(ctx, "comment", 42)
	require.NoError(t, err)
	assert.Equal(t, identity, got)
	got, err = GetAgentAction(ctx, "comment", 43)
	require.NoError(t, err)
	assert.Nil(t, got)
	require.NoError(t, RecordPushReceipt(ctx, "access-token:123|all", 1, "refs/heads/main", "old", "new"))
	receipts, err := GetPushReceipts(ctx, 1, "new")
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	assert.Equal(t, int64(123), receipts[0].TokenID)
	assert.Equal(t, identity.Agent, receipts[0].Agent)
	assert.Equal(t, identity.TaskURL, receipts[0].TaskURL)
	for _, bad := range []*api.AgentIdentity{{}, {Agent: "name", TaskURL: "javascript:alert(1)"}, {Agent: "name", TaskURL: "https://user:pass@example.org"}, {Agent: "name\n"}} {
		assert.Error(t, ValidateAgentIdentity(bad))
	}
}
