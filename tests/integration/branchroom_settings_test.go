// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"net/http"
	"os"
	"strconv"
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	repo_model "gitea.dev/models/repo"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/git"
	"gitea.dev/modules/git/gitrepo"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
	"gitea.dev/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// branchroomTestRoot prepares a restricted root with the permissions the
// production path requires, regardless of the developer's umask.
func branchroomTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.Chmod(root, 0o700))
	return root
}

func TestBranchroomSettings(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Branchroom)()

	owner := loginUser(t, "user2")
	siteAdmin := loginUser(t, "user1")
	collaborator := loginUser(t, "user4")
	settingsURL := "/user2/repo1/settings"
	branchroomURL := settingsURL + "/branchroom"
	workspacesURL := branchroomURL + "/workspaces"
	grantsURL := branchroomURL + "/grants"
	sourceGrantsURL := branchroomURL + "/source-grants"
	sourceWriteGrantsURL := branchroomURL + "/source-write-grants"

	t.Run("disabled is undiscoverable", func(t *testing.T) {
		setting.Branchroom = setting.BranchroomConfig{}

		resp := owner.MakeRequest(t, NewRequest(t, http.MethodGet, settingsURL), http.StatusOK)
		AssertHTMLElement(t, NewHTMLParser(t, resp.Body), `a[href="/user2/repo1/settings/branchroom"]`, false)
		owner.MakeRequest(t, NewRequest(t, http.MethodGet, branchroomURL), http.StatusNotFound)
		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, branchroomURL, map[string]string{"name": "security-fix"}), http.StatusNotFound)
		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, workspacesURL, map[string]string{"scope_id": "1"}), http.StatusNotFound)
		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, grantsURL, map[string]string{"scope_id": "1", "username": "user4"}), http.StatusNotFound)
		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, sourceGrantsURL, map[string]string{"scope_id": "1", "username": "user4"}), http.StatusNotFound)
		owner.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/1/route-key/rotate"), http.StatusNotFound)
		owner.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/1/source-grants/1/revoke"), http.StatusNotFound)
		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, sourceWriteGrantsURL, map[string]string{"scope_id": "1", "username": "user4"}), http.StatusNotFound)
		owner.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/1/source-write-grants/1/revoke"), http.StatusNotFound)
	})

	t.Run("real owner creates a scope without a site-admin bypass", func(t *testing.T) {
		setting.Branchroom = setting.BranchroomConfig{Enabled: true, RestrictedRepoRootPath: branchroomTestRoot(t)}

		resp := owner.MakeRequest(t, NewRequest(t, http.MethodGet, settingsURL), http.StatusOK)
		AssertHTMLElement(t, NewHTMLParser(t, resp.Body), `a[href="/user2/repo1/settings/branchroom"]`, true)

		resp = owner.MakeRequest(t, NewRequest(t, http.MethodGet, branchroomURL), http.StatusOK)
		doc := NewHTMLParser(t, resp.Body)
		AssertHTMLElement(t, doc, "#branchroom-scope-name", true)
		assert.Contains(t, doc.Find(".ui.warning.message").Text(), "Provisioning alone does not expose isolated source")

		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, branchroomURL, map[string]string{"name": "security-fix"}), http.StatusSeeOther)

		resp = owner.MakeRequest(t, NewRequest(t, http.MethodGet, branchroomURL), http.StatusOK)
		doc = NewHTMLParser(t, resp.Body)
		assert.Contains(t, doc.Find(".item-title").Text(), "security-fix")

		scopes, err := branchroom_model.FindScopesByRepoID(t.Context(), 1)
		require.NoError(t, err)
		require.Len(t, scopes, 1)
		scope := scopes[0]
		AssertHTMLElement(t, doc, "#branchroom-source-grant-user-"+strconv.FormatInt(scope.ID, 10), false)
		AssertHTMLElement(t, doc, "#branchroom-source-write-grant-user-"+strconv.FormatInt(scope.ID, 10), false)
		AssertHTMLElement(t, doc, "#branchroom-route-key-rotate-"+strconv.FormatInt(scope.ID, 10), false)

		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, workspacesURL, map[string]string{
			"scope_id": strconv.FormatInt(scope.ID, 10),
		}), http.StatusSeeOther)
		workspace, err := branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
		require.NoError(t, err)
		assert.Equal(t, branchroom_model.WorkspaceStateReady, workspace.State)
		repository, err := repo_model.GetRepositoryByID(t.Context(), 1)
		require.NoError(t, err)
		defaultBranchSHA, err := git.GetBranchCommitID(t.Context(), repository.CodeStorageRepo(), repository.DefaultBranch)
		require.NoError(t, err)
		assert.Equal(t, defaultBranchSHA, workspace.PublicBaseSHA)
		oldRouteKey := workspace.RouteKey
		owner.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+strconv.FormatInt(scope.ID, 10)+"/route-key/rotate"), http.StatusSeeOther)
		rotatedWorkspace, err := branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
		require.NoError(t, err)
		assert.NotEqual(t, oldRouteKey, rotatedWorkspace.RouteKey)
		assert.Equal(t, workspace.WorkspaceKey, rotatedWorkspace.WorkspaceKey)
		assert.Equal(t, workspace.StoreID, rotatedWorkspace.StoreID)
		assert.Equal(t, workspace.PublicBaseSHA, rotatedWorkspace.PublicBaseSHA)
		assert.Equal(t, workspace.ObjectFormatName, rotatedWorkspace.ObjectFormatName)
		assert.Equal(t, workspace.State, rotatedWorkspace.State)
		_, err = branchroom_model.GetWorkspaceByRouteKey(t.Context(), oldRouteKey)
		assert.ErrorIs(t, err, util.ErrNotExist)
		workspace = rotatedWorkspace

		resp = owner.MakeRequest(t, NewRequest(t, http.MethodGet, branchroomURL), http.StatusOK)
		body := resp.Body.String()
		assert.Contains(t, body, string(branchroom_model.WorkspaceStateReady))
		assert.NotContains(t, body, workspace.WorkspaceKey)
		assert.NotContains(t, body, oldRouteKey)
		assert.NotContains(t, body, workspace.RouteKey)
		assert.NotContains(t, body, workspace.StoreID)
		restricted, err := branchroom_module.RestrictedRepository(workspace.StoreID)
		require.NoError(t, err)
		assert.NotContains(t, body, gitrepo.RepoLocalPath(restricted))
		doc = NewHTMLParser(t, resp.Body)
		assert.Contains(t, doc.Find(".help").Text(), "remains hidden until a direct user")
		AssertHTMLElement(t, doc, "#branchroom-source-grant-user-"+strconv.FormatInt(scope.ID, 10), true)
		AssertHTMLElement(t, doc, "#branchroom-source-write-grant-user-"+strconv.FormatInt(scope.ID, 10), true)
		AssertHTMLElement(t, doc, "#branchroom-route-key-rotate-"+strconv.FormatInt(scope.ID, 10), true)

		siteAdmin.MakeRequest(t, NewRequest(t, http.MethodPost, "/-/admin/users/2/impersonate"), http.StatusOK)
		impersonatedScopeCount, err := db.GetEngine(t.Context()).Where("repo_id = ?", 1).Count(new(branchroom_model.Scope))
		require.NoError(t, err)
		siteAdmin.MakeRequest(t, NewRequest(t, http.MethodGet, branchroomURL), http.StatusNotFound)
		siteAdmin.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, branchroomURL, map[string]string{"name": "impersonated-owner"}), http.StatusNotFound)
		siteAdmin.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+strconv.FormatInt(scope.ID, 10)+"/route-key/rotate"), http.StatusNotFound)
		siteAdmin.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, sourceWriteGrantsURL, map[string]string{"scope_id": strconv.FormatInt(scope.ID, 10), "username": "user4"}), http.StatusNotFound)
		siteAdmin.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+strconv.FormatInt(scope.ID, 10)+"/source-write-grants/1/revoke"), http.StatusNotFound)
		storedScopeCount, err := db.GetEngine(t.Context()).Where("repo_id = ?", 1).Count(new(branchroom_model.Scope))
		require.NoError(t, err)
		assert.Equal(t, impersonatedScopeCount, storedScopeCount)
		storedWorkspace, err := branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
		require.NoError(t, err)
		assert.Equal(t, workspace.RouteKey, storedWorkspace.RouteKey)
		siteAdmin.MakeRequest(t, NewRequest(t, http.MethodGet, "/user/logout"), http.StatusSeeOther)

		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, workspacesURL, map[string]string{
			"scope_id": strconv.FormatInt(scope.ID, 10),
		}), http.StatusSeeOther)
		siteAdmin.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, workspacesURL, map[string]string{
			"scope_id": strconv.FormatInt(scope.ID, 10),
		}), http.StatusNotFound)
		collaborator.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, workspacesURL, map[string]string{
			"scope_id": strconv.FormatInt(scope.ID, 10),
		}), http.StatusNotFound)
		collaborator.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+strconv.FormatInt(scope.ID, 10)+"/route-key/rotate"), http.StatusNotFound)
		collaborator.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, sourceWriteGrantsURL, map[string]string{"scope_id": strconv.FormatInt(scope.ID, 10), "username": "user4"}), http.StatusNotFound)
		collaborator.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+strconv.FormatInt(scope.ID, 10)+"/source-write-grants/1/revoke"), http.StatusNotFound)
		storedWorkspace, err = branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
		require.NoError(t, err)
		assert.Equal(t, workspace.RouteKey, storedWorkspace.RouteKey)
		assertWorkspaceMissing := func(blockedScope *branchroom_model.Scope) {
			t.Helper()
			owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, workspacesURL, map[string]string{
				"scope_id": strconv.FormatInt(blockedScope.ID, 10),
			}), http.StatusNotFound)
			_, err := branchroom_model.GetWorkspaceByScopeID(t.Context(), blockedScope.ID)
			assert.ErrorIs(t, err, util.ErrNotExist)
		}
		foreignScope := &branchroom_model.Scope{
			Kind:        branchroom_model.ScopeKindRepository,
			RepoID:      2,
			Name:        "foreign-workspace-scope",
			State:       branchroom_model.ScopeStateActive,
			OwnerType:   branchroom_model.ScopeOwnerTypeUser,
			OwnerID:     2,
			CreatedByID: 2,
		}
		require.NoError(t, branchroom_model.InsertScope(t.Context(), foreignScope))
		assertWorkspaceMissing(foreignScope)
		disabledScope := &branchroom_model.Scope{
			Kind:        branchroom_model.ScopeKindRepository,
			RepoID:      1,
			Name:        "disabled-workspace-scope",
			State:       branchroom_model.ScopeStateDisabled,
			OwnerType:   branchroom_model.ScopeOwnerTypeUser,
			OwnerID:     2,
			CreatedByID: 2,
		}
		require.NoError(t, branchroom_model.InsertScope(t.Context(), disabledScope))
		assertWorkspaceMissing(disabledScope)
		retiredScope := &branchroom_model.Scope{
			Kind:        branchroom_model.ScopeKindRepository,
			RepoID:      1,
			Name:        "retired-workspace-scope",
			State:       branchroom_model.ScopeStateRetired,
			OwnerType:   branchroom_model.ScopeOwnerTypeUser,
			OwnerID:     2,
			CreatedByID: 2,
		}
		require.NoError(t, branchroom_model.InsertScope(t.Context(), retiredScope))
		assertWorkspaceMissing(retiredScope)
		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, grantsURL, map[string]string{
			"scope_id": strconv.FormatInt(scope.ID, 10),
			"username": "user4",
		}), http.StatusSeeOther)

		resp = owner.MakeRequest(t, NewRequest(t, http.MethodGet, branchroomURL), http.StatusOK)
		doc = NewHTMLParser(t, resp.Body)
		assert.Contains(t, doc.Find(".ui.list").Text(), "user4")
		grants, err := branchroom_model.FindActiveGrantsAt(t.Context(), scope.ID, branchroom_model.ActionDiscover, 1)
		require.NoError(t, err)
		var userGrantID int64
		for _, grant := range grants {
			if grant.SubjectType == branchroom_model.SubjectTypeUser && grant.SubjectID == 4 {
				userGrantID = grant.ID
				break
			}
		}
		require.NotZero(t, userGrantID)
		owner.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+strconv.FormatInt(scope.ID, 10)+"/grants/"+strconv.FormatInt(userGrantID, 10)+"/revoke"), http.StatusSeeOther)

		resp = owner.MakeRequest(t, NewRequest(t, http.MethodGet, branchroomURL), http.StatusOK)
		assert.NotContains(t, NewHTMLParser(t, resp.Body).Find(".ui.list").Text(), "user4")

		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, sourceGrantsURL, map[string]string{
			"scope_id": strconv.FormatInt(scope.ID, 10),
			"username": "user4",
		}), http.StatusSeeOther)
		resp = owner.MakeRequest(t, NewRequest(t, http.MethodGet, branchroomURL), http.StatusOK)
		body = resp.Body.String()
		assert.NotContains(t, body, workspace.WorkspaceKey)
		assert.NotContains(t, body, workspace.StoreID)
		assert.NotContains(t, body, gitrepo.RepoLocalPath(restricted))
		doc = NewHTMLParser(t, resp.Body)
		assert.Contains(t, doc.Find(".ui.list").Text(), "user4")
		sourceGrants, err := branchroom_model.FindActiveGrantsAt(t.Context(), scope.ID, branchroom_model.ActionReadSource, timeutil.TimeStampNow())
		require.NoError(t, err)
		require.Len(t, sourceGrants, 1)
		owner.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+strconv.FormatInt(scope.ID, 10)+"/source-grants/"+strconv.FormatInt(sourceGrants[0].ID, 10)+"/revoke"), http.StatusSeeOther)
		storedSourceGrant, err := branchroom_model.GetGrantByID(t.Context(), sourceGrants[0].ID)
		require.NoError(t, err)
		assert.NotZero(t, storedSourceGrant.RevokedUnix)

		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, sourceWriteGrantsURL, map[string]string{
			"scope_id": strconv.FormatInt(scope.ID, 10),
			"username": "user4",
		}), http.StatusSeeOther)
		resp = owner.MakeRequest(t, NewRequest(t, http.MethodGet, branchroomURL), http.StatusOK)
		body = resp.Body.String()
		assert.NotContains(t, body, workspace.WorkspaceKey)
		assert.NotContains(t, body, workspace.RouteKey)
		assert.NotContains(t, body, workspace.StoreID)
		assert.NotContains(t, body, gitrepo.RepoLocalPath(restricted))
		doc = NewHTMLParser(t, resp.Body)
		assert.Contains(t, doc.Find(".ui.list").Text(), "user4")
		sourceWriteGrants, err := branchroom_model.FindActiveGrantsAt(t.Context(), scope.ID, branchroom_model.ActionWriteSource, timeutil.TimeStampNow())
		require.NoError(t, err)
		require.Len(t, sourceWriteGrants, 1)
		assert.Equal(t, branchroom_model.SubjectTypeUser, sourceWriteGrants[0].SubjectType)
		assert.Equal(t, int64(4), sourceWriteGrants[0].SubjectID)
		owner.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+strconv.FormatInt(scope.ID, 10)+"/source-write-grants/"+strconv.FormatInt(sourceWriteGrants[0].ID, 10)+"/revoke"), http.StatusSeeOther)
		storedSourceWriteGrant, err := branchroom_model.GetGrantByID(t.Context(), sourceWriteGrants[0].ID)
		require.NoError(t, err)
		assert.NotZero(t, storedSourceWriteGrant.RevokedUnix)
		resp = owner.MakeRequest(t, NewRequest(t, http.MethodGet, branchroomURL), http.StatusOK)
		assert.NotContains(t, NewHTMLParser(t, resp.Body).Find(".ui.list").Text(), "user4")

		_, err = db.GetEngine(t.Context()).ID(scope.ID).Cols("state").Update(&branchroom_model.Scope{State: branchroom_model.ScopeStateRetired})
		require.NoError(t, err)
		resp = owner.MakeRequest(t, NewRequest(t, http.MethodGet, branchroomURL), http.StatusOK)
		assert.NotContains(t, NewHTMLParser(t, resp.Body).Find(".item-title").Text(), "security-fix")
		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, grantsURL, map[string]string{"scope_id": strconv.FormatInt(scope.ID, 10), "username": "user5"}), http.StatusNotFound)
		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, sourceGrantsURL, map[string]string{"scope_id": strconv.FormatInt(scope.ID, 10), "username": "user5"}), http.StatusNotFound)
		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, sourceWriteGrantsURL, map[string]string{"scope_id": strconv.FormatInt(scope.ID, 10), "username": "user5"}), http.StatusNotFound)
		owner.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+strconv.FormatInt(scope.ID, 10)+"/source-write-grants/"+strconv.FormatInt(storedSourceWriteGrant.ID, 10)+"/revoke"), http.StatusNotFound)
		owner.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+strconv.FormatInt(scope.ID, 10)+"/route-key/rotate"), http.StatusNotFound)

		environmentScope := &branchroom_model.Scope{
			Kind:        branchroom_model.ScopeKindEnvironment,
			RepoID:      1,
			Name:        "hidden-environment-scope",
			State:       branchroom_model.ScopeStateActive,
			OwnerType:   branchroom_model.ScopeOwnerTypeUser,
			OwnerID:     2,
			CreatedByID: 2,
		}
		require.NoError(t, branchroom_model.InsertScope(t.Context(), environmentScope))
		environmentGrant := &branchroom_model.Grant{
			ScopeID:     environmentScope.ID,
			SubjectType: branchroom_model.SubjectTypeUser,
			SubjectID:   4,
			Action:      branchroom_model.ActionDiscover,
			CreatedByID: 2,
		}
		require.NoError(t, branchroom_model.InsertGrant(t.Context(), environmentGrant))
		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, grantsURL, map[string]string{
			"scope_id": strconv.FormatInt(environmentScope.ID, 10),
			"username": "user5",
		}), http.StatusNotFound)
		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, workspacesURL, map[string]string{
			"scope_id": strconv.FormatInt(environmentScope.ID, 10),
		}), http.StatusNotFound)
		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, sourceGrantsURL, map[string]string{
			"scope_id": strconv.FormatInt(environmentScope.ID, 10),
			"username": "user5",
		}), http.StatusNotFound)
		owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, sourceWriteGrantsURL, map[string]string{
			"scope_id": strconv.FormatInt(environmentScope.ID, 10),
			"username": "user5",
		}), http.StatusNotFound)
		environmentWriteGrant := &branchroom_model.Grant{
			ScopeID:     environmentScope.ID,
			SubjectType: branchroom_model.SubjectTypeUser,
			SubjectID:   4,
			Action:      branchroom_model.ActionWriteSource,
			CreatedByID: 2,
		}
		require.NoError(t, branchroom_model.InsertGrant(t.Context(), environmentWriteGrant))
		owner.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+strconv.FormatInt(environmentScope.ID, 10)+"/source-write-grants/"+strconv.FormatInt(environmentWriteGrant.ID, 10)+"/revoke"), http.StatusNotFound)
		storedEnvironmentWriteGrant, err := branchroom_model.GetGrantByID(t.Context(), environmentWriteGrant.ID)
		require.NoError(t, err)
		assert.Zero(t, storedEnvironmentWriteGrant.RevokedUnix)
		owner.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+strconv.FormatInt(environmentScope.ID, 10)+"/route-key/rotate"), http.StatusNotFound)
		_, err = branchroom_model.GetWorkspaceByScopeID(t.Context(), environmentScope.ID)
		assert.ErrorIs(t, err, util.ErrNotExist)
		owner.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+strconv.FormatInt(environmentScope.ID, 10)+"/grants/"+strconv.FormatInt(environmentGrant.ID, 10)+"/revoke"), http.StatusNotFound)
		storedEnvironmentGrant, err := branchroom_model.GetGrantByID(t.Context(), environmentGrant.ID)
		require.NoError(t, err)
		assert.Zero(t, storedEnvironmentGrant.RevokedUnix)

		siteAdmin.MakeRequest(t, NewRequest(t, http.MethodGet, settingsURL), http.StatusOK)
		siteAdmin.MakeRequest(t, NewRequest(t, http.MethodGet, branchroomURL), http.StatusNotFound)
		siteAdmin.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, branchroomURL, map[string]string{"name": "admin-bypass"}), http.StatusNotFound)
		siteAdmin.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, grantsURL, map[string]string{"scope_id": strconv.FormatInt(scope.ID, 10), "username": "user5"}), http.StatusNotFound)
		siteAdmin.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, sourceGrantsURL, map[string]string{"scope_id": strconv.FormatInt(scope.ID, 10), "username": "user5"}), http.StatusNotFound)
		siteAdmin.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, sourceWriteGrantsURL, map[string]string{"scope_id": strconv.FormatInt(scope.ID, 10), "username": "user5"}), http.StatusNotFound)
		siteAdmin.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+strconv.FormatInt(scope.ID, 10)+"/route-key/rotate"), http.StatusNotFound)
		siteAdmin.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+strconv.FormatInt(scope.ID, 10)+"/grants/1/revoke"), http.StatusNotFound)
		siteAdmin.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+strconv.FormatInt(scope.ID, 10)+"/source-grants/1/revoke"), http.StatusNotFound)
		siteAdmin.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+strconv.FormatInt(scope.ID, 10)+"/source-write-grants/1/revoke"), http.StatusNotFound)
	})
}
