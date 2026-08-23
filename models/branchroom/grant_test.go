// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"testing"
	"time"

	"gitea.dev/models/db"
	"gitea.dev/models/unittest"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasUnexpiredSubjectGrant(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())
	ctx := t.Context()
	now := timeutil.TimeStampNow()

	scope := newTestScope()
	require.NoError(t, InsertScope(ctx, scope))
	exists, err := HasUnexpiredSubjectGrant(ctx, scope.ID, ActionDiscover, SubjectTypeTeam, 2, now)
	require.NoError(t, err)
	assert.False(t, exists)

	futureGrant := &Grant{
		ScopeID:       scope.ID,
		SubjectType:   SubjectTypeTeam,
		SubjectID:     2,
		Action:        ActionDiscover,
		NotBeforeUnix: now.AddDuration(time.Minute),
		ExpiresUnix:   now.AddDuration(2 * time.Minute),
		CreatedByID:   1,
	}
	require.NoError(t, InsertGrant(ctx, futureGrant))
	exists, err = HasUnexpiredSubjectGrant(ctx, scope.ID, ActionDiscover, SubjectTypeTeam, 2, now)
	require.NoError(t, err)
	assert.True(t, exists)

	require.NoError(t, RevokeGrant(ctx, futureGrant.ID, 1, now))
	exists, err = HasUnexpiredSubjectGrant(ctx, scope.ID, ActionDiscover, SubjectTypeTeam, 2, now)
	require.NoError(t, err)
	assert.False(t, exists)

	expiredGrant := &Grant{
		ScopeID:       scope.ID,
		SubjectType:   SubjectTypeTeam,
		SubjectID:     2,
		Action:        ActionDiscover,
		NotBeforeUnix: now.AddDuration(-2 * time.Minute),
		ExpiresUnix:   now.AddDuration(-time.Minute),
		CreatedByID:   1,
	}
	require.NoError(t, db.Insert(ctx, expiredGrant))
	exists, err = HasUnexpiredSubjectGrant(ctx, scope.ID, ActionDiscover, SubjectTypeTeam, 2, now)
	require.NoError(t, err)
	assert.False(t, exists)

	openEndedGrant := &Grant{
		ScopeID:     scope.ID,
		SubjectType: SubjectTypeUser,
		SubjectID:   2,
		Action:      ActionDiscover,
		CreatedByID: 1,
	}
	require.NoError(t, InsertGrant(ctx, openEndedGrant))
	exists, err = HasUnexpiredSubjectGrant(ctx, scope.ID, ActionDiscover, SubjectTypeUser, 2, now)
	require.NoError(t, err)
	assert.True(t, exists)

	_, err = HasUnexpiredSubjectGrant(ctx, 0, ActionDiscover, SubjectTypeTeam, 2, now)
	assert.ErrorIs(t, err, util.ErrInvalidArgument)
}
