// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	issues_model "gitea.dev/models/issues"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/git"
	"gitea.dev/modules/git/gitrepo"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
	branchroom_service "gitea.dev/services/branchroom"
	repository_service "gitea.dev/services/repository"
	"gitea.dev/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// branchroomTestRoot prepares a restricted root with the permissions the
// production path requires, regardless of the developer's umask.
func branchroomTestRoot(t *testing.T) string {
	t.Helper()
	parent, err := os.UserHomeDir()
	require.NoError(t, err)
	//nolint:usetesting // t.TempDir does not guarantee the private ancestor this test requires.
	root, err := os.MkdirTemp(parent, ".gitea-branchroom-test-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(root) })
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
		assert.Contains(t, body, "This direct grant allows restricted HTTPS pushes")
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

func TestBranchroomWorkspaceRetentionRequiresPhysicalPurgeAfterTransfer(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Branchroom)()
	setting.Branchroom = setting.BranchroomConfig{Enabled: true, RestrictedRepoRootPath: branchroomTestRoot(t)}
	repository := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	require.NoError(t, repository.LoadOwner(t.Context()))
	admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	newOwner := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	baseSHA, err := git.GetBranchCommitID(t.Context(), repository.CodeStorageRepo(), repository.DefaultBranch)
	require.NoError(t, err)
	actor := branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID}
	scope, err := branchroom_service.CreateRepositoryScope(t.Context(), branchroom_service.CreateRepositoryScopeRequest{RepoID: repository.ID, Actor: actor, Name: "retained-store", Surface: branchroom_model.SurfaceSystem})
	require.NoError(t, err)
	workspace, err := branchroom_service.CreateRepositoryWorkspace(t.Context(), branchroom_service.CreateRepositoryWorkspaceRequest{RepoID: repository.ID, ScopeID: scope.ID, Actor: actor, PublicBaseSHA: baseSHA, Surface: branchroom_model.SurfaceSystem})
	require.NoError(t, err)
	store := filepath.Join(setting.Branchroom.RestrictedRepoRootPath, workspace.StoreID+".git")
	require.DirExists(t, store)
	_, err = db.GetEngine(t.Context()).ID(workspace.ID).Cols("state", "updated_unix").NoAutoTime().Update(&branchroom_model.Workspace{State: branchroom_model.WorkspaceStateProvisioning, UpdatedUnix: timeutil.TimeStamp(time.Now().Add(-2 * time.Hour).Unix())})
	require.NoError(t, err)
	owner := loginUser(t, "user2")
	page := owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/user2/repo1/settings/branchroom"), http.StatusOK)
	html := NewHTMLParser(t, page.Body)
	scopeID := strconv.FormatInt(scope.ID, 10)
	AssertHTMLElement(t, html, "#branchroom-workspace-retry-"+scopeID, true)
	AssertHTMLElement(t, html, "#branchroom-workspace-purge-"+scopeID, true)
	_, err = db.GetEngine(t.Context()).ID(workspace.ID).Cols("state").Update(&branchroom_model.Workspace{State: branchroom_model.WorkspaceStateReady})
	require.NoError(t, err)
	// exercise the outgoing pull request closure path
	_, err = db.GetEngine(t.Context()).ID(2).Cols("base_repo_id").Update(&issues_model.PullRequest{BaseRepoID: 3})
	require.NoError(t, err)
	require.NoError(t, repository_service.StartRepositoryTransfer(t.Context(), admin, newOwner, repository, nil))
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.Scope{ID: scope.ID, State: branchroom_model.ScopeStateRetired})
	unittest.AssertExistsAndLoadBean(t, &branchroom_model.Workspace{ID: workspace.ID, State: branchroom_model.WorkspaceStateRetired})
	require.ErrorIs(t, branchroom_service.ReleaseRepositoryWorkspaceRetention(t.Context(), repository.ID), branchroom_service.ErrWorkspaceRetentionRequired)
	_, err = branchroom_service.PurgeWorkspace(t.Context(), branchroom_service.PurgeWorkspaceRequest{RepoID: repository.ID, ScopeID: scope.ID, Actor: actor, Surface: branchroom_model.SurfaceSystem})
	require.ErrorIs(t, err, util.ErrNotExist)
	owner = loginUser(t, "user4")
	settingsURL := "/user4/repo1/settings"
	branchroomURL := settingsURL + "/branchroom"
	page = owner.MakeRequest(t, NewRequest(t, http.MethodGet, branchroomURL), http.StatusOK)
	html = NewHTMLParser(t, page.Body)
	item := html.Find("#branchroom-workspace-purge-" + scopeID).Closest(".item-main")
	require.Contains(t, item.Text(), "retained-store")
	require.Equal(t, 1, item.Find("form").Length())
	assert.Equal(t, branchroomURL+"/scopes/"+scopeID+"/workspaces/purge", item.Find("form").AttrOr("action", ""))
	response := owner.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, settingsURL+"?action=delete", map[string]string{"repo_name": repository.Name}), http.StatusOK)
	redirect := test.ParseJSONRedirect(response.Body.Bytes()).Redirect
	require.NotNil(t, redirect)
	assert.Equal(t, branchroomURL, *redirect)
	page = owner.MakeRequest(t, NewRequest(t, http.MethodGet, branchroomURL), http.StatusOK)
	assert.Contains(t, page.Body.String(), "Purge the restricted workspaces in Branchroom settings")
	unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: repository.ID})
	issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 3})
	assert.False(t, issue.IsClosed)
	require.DirExists(t, store)
	response = owner.MakeRequest(t, NewRequest(t, http.MethodDelete, "/api/v1/repos/user4/repo1").AddBasicAuth("user4"), http.StatusConflict)
	assert.Contains(t, response.Body.String(), branchroom_service.ErrWorkspaceRetentionRequired.Error())
	owner.MakeRequest(t, NewRequest(t, http.MethodPost, branchroomURL+"/scopes/"+scopeID+"/workspaces/purge"), http.StatusSeeOther)
	require.NoDirExists(t, store)
	testDeleteRepository(t, owner, "user4", repository.Name)
	unittest.AssertNotExistsBean(t, &repo_model.Repository{ID: repository.ID})
}
