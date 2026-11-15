// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom_test

import (
	"strings"
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	api "gitea.dev/modules/structs"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentIdentity(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	require.NoError(t, db.Insert(ctx, &user_model.User{ID: 2, Name: "user2", LowerName: "user2", IsActive: true}))
	identity := &api.AgentIdentity{Agent: "Harbour helper", Model: "Claude Opus 5.5", Harness: "T3 Code", TaskURL: "https://example.org/tasks/1", Sponsor: "user2"}
	require.NoError(t, branchroom_model.InsertTokenIdentity(ctx, 123, identity))
	require.NoError(t, branchroom_model.RecordAgentAction(ctx, "access-token:123", "comment", 42))
	require.NoError(t, branchroom_model.RecordAgentAction(ctx, "", "comment", 43))
	got, err := branchroom_model.GetAgentAction(ctx, "comment", 42)
	require.NoError(t, err)
	assert.Equal(t, identity, got)
	got, err = branchroom_model.GetAgentAction(ctx, "comment", 43)
	require.NoError(t, err)
	assert.Nil(t, got)
	require.NoError(t, branchroom_model.RecordPushReceipt(ctx, "access-token:123|all", 1, "refs/heads/main", "old", "new"))
	receipts, err := branchroom_model.GetPushReceipts(ctx, 1, "new")
	require.NoError(t, err)
	require.Len(t, receipts, 1)
	assert.Equal(t, int64(123), receipts[0].TokenID)
	assert.Equal(t, identity.Agent, receipts[0].Agent)
	assert.Equal(t, identity.TaskURL, receipts[0].TaskURL)
	assert.Equal(t, identity.Model, receipts[0].Model)
	assert.Equal(t, identity.Harness, receipts[0].Harness)
	unknown := &api.AgentIdentity{Agent: "helper", Sponsor: "not-an-existing-user"}
	require.ErrorContains(t, branchroom_model.InsertTokenIdentity(ctx, 124, unknown), "responsible person must be an existing active user")
	legacy := &branchroom_model.TokenIdentity{TokenID: 125, Agent: "legacy helper", Sponsor: "deleted-user"}
	require.NoError(t, db.Insert(ctx, legacy))
	require.NoError(t, branchroom_model.RecordAgentAction(ctx, "access-token:125", "comment", 44))
	got, err = branchroom_model.GetAgentAction(ctx, "comment", 44)
	require.NoError(t, err)
	assert.Equal(t, legacy.Sponsor, got.Sponsor)
	for _, bad := range []*api.AgentIdentity{{}, {Agent: "name", TaskURL: "javascript:alert(1)"}, {Agent: "name", TaskURL: "https://user:pass@example.org"}, {Agent: "name\n"}, {Agent: "name", Model: strings.Repeat("x", 256)}, {Agent: "name", Harness: strings.Repeat("x", 256)}, {Agent: "name", Model: "model\n"}, {Agent: "name", Harness: "harness\t"}} {
		assert.Error(t, branchroom_model.ValidateAgentIdentity(bad))
	}
}
