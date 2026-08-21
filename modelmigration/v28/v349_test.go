// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v28

import (
	"slices"
	"strings"
	"testing"

	"gitea.dev/modelmigration/migrationtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"xorm.io/xorm/schemas"
)

func TestAddBranchroomPolicySchema(t *testing.T) {
	x, deferable := migrationtest.PrepareTestEnv(t, 0)
	defer deferable()
	if x == nil || t.Failed() {
		return
	}

	require.NoError(t, AddBranchroomPolicySchema(t.Context(), x))
	require.NoError(t, AddBranchroomPolicySchema(t.Context(), x))

	tables := migrationtest.LoadTableSchemasMap(t, x)
	for tableName, columns := range map[string][]string{
		"branchroom_scope": {
			"id", "kind", "repo_id", "name", "lower_name", "state", "owner_type", "owner_id", "created_by_id", "audit_sequence", "audit_head_hash",
		},
		"branchroom_grant": {
			"id", "scope_id", "subject_type", "subject_id", "action", "workflow_id", "workflow_repo_id", "workflow_commit_sha", "ref", "runner_id", "not_before_unix", "expires_unix", "revoked_unix", "revoked_by_id", "created_by_id",
		},
		"branchroom_decision": {
			"id", "decision_key", "scope_id", "repo_id", "grant_id", "actor_type", "actor_id", "action", "resource_type", "resource_id", "surface", "result", "reason_code", "ordinary_allowed", "request_id", "expires_unix",
		},
		"branchroom_audit_event": {
			"id", "event_key", "decision_key", "scope_id", "sequence", "repo_id", "grant_id", "actor_type", "actor_id", "action", "resource_type", "resource_id", "surface", "result", "reason_code", "ordinary_allowed", "request_id", "previous_hash", "event_hash",
		},
	} {
		table := tables[tableName]
		require.NotNil(t, table)
		for _, column := range columns {
			assert.Contains(t, table.ColumnsSeq(), column)
		}
	}

	scopeIndexes, err := x.Dialect().GetIndexes(x.DB(), t.Context(), "branchroom_scope")
	require.NoError(t, err)
	assert.True(t, hasBranchroomIndex(scopeIndexes, []string{"repo_id", "lower_name"}, true))
	assert.True(t, hasBranchroomIndex(scopeIndexes, []string{"repo_id", "state"}, false))

	grantIndexes, err := x.Dialect().GetIndexes(x.DB(), t.Context(), "branchroom_grant")
	require.NoError(t, err)
	assert.True(t, hasBranchroomIndex(grantIndexes, []string{"scope_id", "subject_type", "subject_id", "action"}, false))

	auditIndexes, err := x.Dialect().GetIndexes(x.DB(), t.Context(), "branchroom_audit_event")
	require.NoError(t, err)
	assert.True(t, hasBranchroomIndex(auditIndexes, []string{"scope_id", "sequence"}, true))
	assert.True(t, hasBranchroomIndex(auditIndexes, []string{"decision_key"}, true))
	assert.True(t, hasBranchroomIndex(auditIndexes, []string{"event_hash"}, true))

	_, err = x.Insert(&branchroomScopeMigration{
		Kind:        "repository",
		RepoID:      1,
		Name:        "Security fix",
		LowerName:   "security fix",
		State:       "active",
		OwnerType:   "user",
		OwnerID:     1,
		CreatedByID: 1,
	})
	require.NoError(t, err)
	_, err = x.Insert(&branchroomScopeMigration{
		Kind:        "repository",
		RepoID:      1,
		Name:        "Another spelling",
		LowerName:   "security fix",
		State:       "active",
		OwnerType:   "user",
		OwnerID:     1,
		CreatedByID: 1,
	})
	assert.Error(t, err)

	_, err = x.Insert(&branchroomAuditEventMigration{
		EventKey:     "11111111-1111-1111-1111-111111111111",
		DecisionKey:  "22222222-2222-2222-2222-222222222222",
		ScopeID:      1,
		Sequence:     1,
		RepoID:       1,
		ActorType:    "user",
		ActorID:      1,
		Action:       "discover",
		ResourceType: "scope",
		ResourceID:   "scope/1",
		Surface:      "system",
		Result:       "allow",
		ReasonCode:   "grant_matched",
		EventHash:    strings.Repeat("a", 64),
	})
	require.NoError(t, err)
	_, err = x.Insert(&branchroomAuditEventMigration{
		EventKey:     "33333333-3333-3333-3333-333333333333",
		DecisionKey:  "22222222-2222-2222-2222-222222222222",
		ScopeID:      1,
		Sequence:     2,
		RepoID:       1,
		ActorType:    "user",
		ActorID:      1,
		Action:       "discover",
		ResourceType: "scope",
		ResourceID:   "scope/1",
		Surface:      "system",
		Result:       "allow",
		ReasonCode:   "grant_matched",
		PreviousHash: strings.Repeat("a", 64),
		EventHash:    strings.Repeat("b", 64),
	})
	assert.Error(t, err)
}

func hasBranchroomIndex(indexes map[string]*schemas.Index, columns []string, unique bool) bool {
	for _, index := range indexes {
		if (index.Type == schemas.UniqueType) == unique && slices.Equal(index.Cols, columns) {
			return true
		}
	}
	return false
}
