// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"testing"

	"gitea.dev/modelmigration/migrationtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBindBranchroomTokenSponsors(t *testing.T) {
	x, cleanup := migrationtest.PrepareTestEnv(t, 0)
	defer cleanup()
	if x == nil || t.Failed() {
		return
	}
	type User struct {
		ID        int64 `xorm:"pk"`
		Name      string
		LowerName string
		Type      int
	}
	type AccessToken struct {
		ID  int64 `xorm:"pk"`
		UID int64
	}
	require.NoError(t, x.Sync(new(User), new(AccessToken)))
	require.NoError(t, AddBranchroomAgentIdentity(t.Context(), x))
	_, err := x.Insert(
		&User{ID: 1, Name: "Owner", LowerName: "owner"},
		&User{ID: 2, Name: "Bot", LowerName: "bot", Type: 4},
		&User{ID: 3, Name: "Sponsor", LowerName: "sponsor"},
		&AccessToken{ID: 1, UID: 1},
		&AccessToken{ID: 2, UID: 2},
		&AccessToken{ID: 3, UID: 1},
		&BranchroomTokenIdentity{TokenID: 1, Agent: "agent", Sponsor: "Sponsor"},
		&BranchroomTokenIdentity{TokenID: 2, Agent: "agent", Sponsor: "Sponsor"},
		&BranchroomTokenIdentity{TokenID: 3, Agent: "agent", Sponsor: "Owner"},
	)
	require.NoError(t, err)
	for range 2 {
		require.NoError(t, BindBranchroomTokenSponsors(t.Context(), x))
	}
	var tokens []BranchroomTokenIdentity
	require.NoError(t, x.OrderBy("token_id").Find(&tokens))
	require.Len(t, tokens, 3)
	assert.Equal(t, "Owner", tokens[0].Sponsor)
	assert.Equal(t, "Sponsor", tokens[1].Sponsor)
	assert.Equal(t, "Owner", tokens[2].Sponsor)
}
