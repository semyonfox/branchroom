// Copyright 2024 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package modelmigration

import (
	"testing"

	"gitea.dev/modelmigration/migrationtest"
	"gitea.dev/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	migrationtest.MainTest(m)
}

func TestMigrations(t *testing.T) {
	defer test.MockVariableValue(&preparedMigrations)()
	preparedMigrations = []*migration{
		{idNumber: 70},
		{idNumber: 71},
	}
	assert.EqualValues(t, 72, calcDBVersion(preparedMigrations))
	assert.EqualValues(t, 72, ExpectedDBVersion())

	assert.EqualValues(t, 71, migrationIDNumberToDBVersion(70))

	assert.Equal(t, []*migration{{idNumber: 70}, {idNumber: 71}}, getPendingMigrations(70, preparedMigrations))
	assert.Equal(t, []*migration{{idNumber: 71}}, getPendingMigrations(71, preparedMigrations))
	assert.Equal(t, []*migration{}, getPendingMigrations(72, preparedMigrations))
}

type legacyBranchroomScope struct {
	ID int64 `xorm:"pk autoincr"`
}

func (legacyBranchroomScope) TableName() string { return "branchroom_scope" }

func TestRewindBranchroomLegacyVersion(t *testing.T) {
	x, deferable := migrationtest.PrepareTestEnv(t, 0, new(Version))
	defer deferable()
	if x == nil || t.Failed() {
		return
	}
	rewind := func(from int64) int64 {
		_, err := x.Exec("DELETE FROM version")
		require.NoError(t, err)
		_, err = x.Insert(&Version{ID: 1, Version: from})
		require.NoError(t, err)
		got, err := rewindBranchroomLegacyVersion(x, &Version{ID: 1, Version: from})
		require.NoError(t, err)
		stored, err := GetCurrentDBVersion(x)
		require.NoError(t, err)
		assert.Equal(t, got, stored)
		return got
	}

	assert.EqualValues(t, 376, rewind(376), "an upstream database without Branchroom tables is left alone")

	require.NoError(t, x.Sync(new(legacyBranchroomScope)))
	assert.EqualValues(t, 352, rewind(357))
	assert.EqualValues(t, 369, rewind(370))
	assert.EqualValues(t, 369, rewind(376))
	assert.EqualValues(t, 369, rewind(369))
	assert.EqualValues(t, 352, rewind(352))

	require.NoError(t, x.Sync(new(BranchroomVersion)))
	assert.EqualValues(t, 376, rewind(376), "a database with separate tracking is never rewound")
}
