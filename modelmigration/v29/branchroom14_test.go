// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"testing"

	"gitea.dev/modelmigration/migrationtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddBranchroomAgentIdentity(t *testing.T) {
	x, cleanup := migrationtest.PrepareTestEnv(t, 0)
	defer cleanup()
	if x == nil || t.Failed() {
		return
	}
	require.NoError(t, AddBranchroomAgentIdentity(t.Context(), x))
	require.NoError(t, AddBranchroomAgentIdentity(t.Context(), x))
	tables := migrationtest.LoadTableSchemasMap(t, x)
	for _, name := range []string{"branchroom_token_identity", "branchroom_agent_action", "branchroom_push_receipt"} {
		assert.Contains(t, tables, name)
	}
}
