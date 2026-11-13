// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"testing"

	"gitea.dev/modelmigration/migrationtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddAccessTokenRestrictions(t *testing.T) {
	type AccessToken struct {
		ID   int64 `xorm:"pk autoincr"`
		Name string
	}
	x, cleanup := migrationtest.PrepareTestEnv(t, 0, new(AccessToken))
	defer cleanup()
	if x == nil || t.Failed() {
		return
	}
	original := &AccessToken{Name: "existing"}
	_, err := x.Insert(original)
	require.NoError(t, err)
	require.NoError(t, AddAccessTokenRestrictions(t.Context(), x))
	require.NoError(t, AddAccessTokenRestrictions(t.Context(), x))
	var migrated accessTokenRestrictions
	has, err := x.Table("access_token").Where("id = ?", original.ID).Get(&migrated)
	require.NoError(t, err)
	require.True(t, has)
	assert.Zero(t, migrated.ExpiresUnix)
	assert.Empty(t, migrated.RepositoryIDs)
	var preserved AccessToken
	_, err = x.ID(original.ID).Get(&preserved)
	require.NoError(t, err)
	assert.Equal(t, original.Name, preserved.Name)
}
