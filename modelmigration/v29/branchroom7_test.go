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

func TestConvertBranchroomAuditHashesToVarchar(t *testing.T) {
	x, deferable := migrationtest.PrepareTestEnv(t, 0, new(branchroomScopeMigration), new(branchroomAuditEventMigration))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}

	scope := &branchroomScopeMigration{Kind: "repository", RepoID: 1, Name: "s", LowerName: "s", State: "active", OwnerType: "user", OwnerID: 2, CreatedByID: 2}
	_, err := x.Insert(scope)
	require.NoError(t, err)
	event := &branchroomAuditEventMigration{EventKey: "11111111-1111-1111-1111-111111111111", DecisionKey: "22222222-2222-2222-2222-222222222222", ScopeID: scope.ID, Sequence: 1, RepoID: 1, ActorType: "user", ActorID: 2, Action: "discover", ResourceType: "scope", ResourceID: "scope/1", Surface: "system", Result: "allow", ReasonCode: "ok", EventHash: strings.Repeat("a", 64)}
	_, err = x.Insert(event)
	require.NoError(t, err)

	require.NoError(t, ConvertBranchroomAuditHashesToVarchar(t.Context(), x))

	var head, previous string
	_, err = x.SQL("SELECT audit_head_hash FROM branchroom_scope WHERE id = ?", scope.ID).Get(&head)
	require.NoError(t, err)
	_, err = x.SQL("SELECT previous_hash FROM branchroom_audit_event WHERE id = ?", event.ID).Get(&previous)
	require.NoError(t, err)
	assert.Empty(t, head)
	assert.Empty(t, previous)
}
