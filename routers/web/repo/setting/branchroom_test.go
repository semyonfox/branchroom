// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"net/http"
	"testing"
	"time"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	repo_model "gitea.dev/models/repo"
	unit_model "gitea.dev/models/unit"
	"gitea.dev/models/unittest"
	"gitea.dev/modules/session"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/web"
	"gitea.dev/services/contexttest"
	"gitea.dev/services/forms"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBranchroomGrantWindow(t *testing.T) {
	notBefore, expires, ok := parseBranchroomGrantWindow("", "")
	assert.True(t, ok)
	assert.Zero(t, notBefore)
	assert.Zero(t, expires)

	base := time.Date(2026, 8, 23, 12, 30, 0, 0, time.Local)
	notBefore, expires, ok = parseBranchroomGrantWindow("2026-08-23T12:30", "2026-08-24T12:30")
	assert.True(t, ok)
	assert.Equal(t, timeutil.TimeStamp(base.Unix()), notBefore)
	assert.Equal(t, timeutil.TimeStamp(base.Add(24*time.Hour).Unix()), expires)

	_, _, ok = parseBranchroomGrantWindow("not-a-date", "")
	assert.False(t, ok)

	_, _, ok = parseBranchroomGrantWindow("", "2026-13-99T99:99")
	assert.False(t, ok)
}

func insertBranchroomScopeForTest(t *testing.T, repoID int64, name string) *branchroom_model.Scope {
	t.Helper()
	// fixture repos may lack unit rows; the handler requires the code unit enabled
	hasUnit, err := db.GetEngine(t.Context()).Where("repo_id = ? AND type = ?", repoID, unit_model.TypeCode).Exist(&repo_model.RepoUnit{})
	require.NoError(t, err)
	if !hasUnit {
		require.NoError(t, db.Insert(t.Context(), &repo_model.RepoUnit{RepoID: repoID, Type: unit_model.TypeCode, Config: &repo_model.UnitConfig{}}))
	}
	scope := &branchroom_model.Scope{
		Kind:        branchroom_model.ScopeKindRepository,
		RepoID:      repoID,
		Name:        name,
		State:       branchroom_model.ScopeStateActive,
		OwnerType:   branchroom_model.ScopeOwnerTypeOrganization,
		OwnerID:     26,
		CreatedByID: 26,
	}
	require.NoError(t, branchroom_model.InsertScope(t.Context(), scope))
	return scope
}

func TestBranchroomTeamGrantPost(t *testing.T) {
	defer test.MockVariableValue(&setting.Branchroom.Enabled, true)()
	unittest.PrepareTestEnv(t)

	scope := insertBranchroomScopeForTest(t, 43, "team-grants")

	t.Run("CreatesDiscoverGrant", func(t *testing.T) {
		ctx, _ := contexttest.MockContext(t, "org26/repo26/settings/branchroom/team-grants", contexttest.MockContextOption{SessionStore: session.NewMockMemStore("dummy-sid")})
		contexttest.LoadUser(t, ctx, 26)
		contexttest.LoadRepo(t, ctx, 43)
		web.SetForm(ctx, &forms.BranchroomTeamGrantForm{
			ScopeID: scope.ID,
			Team:    "team11",
			Action:  string(branchroom_model.ActionDiscover),
		})

		BranchroomTeamGrantPost(ctx)

		assert.Equal(t, http.StatusSeeOther, ctx.Resp.WrittenStatus())
		assert.Empty(t, ctx.Flash.ErrorMsg)
		unittest.AssertExistsAndLoadBean(t, &branchroom_model.Grant{
			ScopeID:     scope.ID,
			SubjectType: branchroom_model.SubjectTypeTeam,
			SubjectID:   11,
			Action:      branchroom_model.ActionDiscover,
		})
	})

	t.Run("RejectsUnknownTeam", func(t *testing.T) {
		ctx, _ := contexttest.MockContext(t, "org26/repo26/settings/branchroom/team-grants", contexttest.MockContextOption{SessionStore: session.NewMockMemStore("dummy-sid")})
		contexttest.LoadUser(t, ctx, 26)
		contexttest.LoadRepo(t, ctx, 43)
		web.SetForm(ctx, &forms.BranchroomTeamGrantForm{
			ScopeID: scope.ID,
			Team:    "no-such-team",
			Action:  string(branchroom_model.ActionDiscover),
		})

		BranchroomTeamGrantPost(ctx)

		assert.Equal(t, http.StatusSeeOther, ctx.Resp.WrittenStatus())
		assert.NotEmpty(t, ctx.Flash.ErrorMsg)
	})
}

func TestBranchroomUserGrantPostWithWindow(t *testing.T) {
	defer test.MockVariableValue(&setting.Branchroom.Enabled, true)()
	unittest.PrepareTestEnv(t)

	scope := insertBranchroomScopeForTest(t, 1, "user-grant-window")

	t.Run("RejectsUnparseableWindow", func(t *testing.T) {
		ctx, _ := contexttest.MockContext(t, "user2/repo1/settings/branchroom/grants", contexttest.MockContextOption{SessionStore: session.NewMockMemStore("dummy-sid")})
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		web.SetForm(ctx, &forms.BranchroomDiscoveryGrantForm{
			ScopeID:   scope.ID,
			Username:  "user4",
			NotBefore: "not-a-date",
		})

		BranchroomDiscoveryGrantPost(ctx)

		assert.Equal(t, http.StatusSeeOther, ctx.Resp.WrittenStatus())
		assert.NotEmpty(t, ctx.Flash.ErrorMsg)
	})

	t.Run("CreatesGrantWithExpiry", func(t *testing.T) {
		ctx, _ := contexttest.MockContext(t, "user2/repo1/settings/branchroom/grants", contexttest.MockContextOption{SessionStore: session.NewMockMemStore("dummy-sid")})
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		expires := time.Now().Add(24 * time.Hour).Format("2006-01-02T15:04")
		web.SetForm(ctx, &forms.BranchroomDiscoveryGrantForm{
			ScopeID:  scope.ID,
			Username: "user4",
			Expires:  expires,
		})

		BranchroomDiscoveryGrantPost(ctx)

		assert.Equal(t, http.StatusSeeOther, ctx.Resp.WrittenStatus())
		assert.Empty(t, ctx.Flash.ErrorMsg)
		grant := unittest.AssertExistsAndLoadBean(t, &branchroom_model.Grant{
			ScopeID:     scope.ID,
			SubjectType: branchroom_model.SubjectTypeUser,
			SubjectID:   4,
			Action:      branchroom_model.ActionDiscover,
		})
		expected, err := time.ParseInLocation("2006-01-02T15:04", expires, time.Local)
		require.NoError(t, err)
		assert.Equal(t, timeutil.TimeStamp(expected.Unix()), grant.ExpiresUnix)
	})

	t.Run("RejectsExpiredWindow", func(t *testing.T) {
		ctx, _ := contexttest.MockContext(t, "user2/repo1/settings/branchroom/grants", contexttest.MockContextOption{SessionStore: session.NewMockMemStore("dummy-sid")})
		contexttest.LoadUser(t, ctx, 2)
		contexttest.LoadRepo(t, ctx, 1)
		web.SetForm(ctx, &forms.BranchroomDiscoveryGrantForm{
			ScopeID:  scope.ID,
			Username: "user4",
			Expires:  "2020-01-01T00:00",
		})

		BranchroomDiscoveryGrantPost(ctx)

		assert.Equal(t, http.StatusSeeOther, ctx.Resp.WrittenStatus())
		assert.NotEmpty(t, ctx.Flash.ErrorMsg)
	})
}
