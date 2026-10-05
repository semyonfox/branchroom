// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	organization_model "gitea.dev/models/organization"
	"gitea.dev/models/perm"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/git"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	"gitea.dev/modules/timeutil"
	org_service "gitea.dev/services/org"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchroomRbacLifecycle(t *testing.T) {
	defer test.MockVariableValue(&setting.AppURL)()
	setting.AppURL = "http://127.0.0.1:0/"

	onGiteaRun(t, func(t *testing.T, u *url.URL) {
		defer test.MockVariableValue(&setting.Branchroom)()
		setting.AppURL = u.String()
		restrictedRoot := branchroomTestRoot(t)
		setting.Branchroom = setting.BranchroomConfig{Enabled: true, RestrictedRepoRootPath: restrictedRoot}

		repository, err := repo_model.GetRepositoryByID(t.Context(), 3)
		require.NoError(t, err)
		require.NoError(t, repository.LoadOwner(t.Context()))
		baseSHA, err := git.GetBranchCommitID(t.Context(), repository.CodeStorageRepo(), repository.DefaultBranch)
		require.NoError(t, err)
		team, err := organization_model.GetTeam(t.Context(), repository.OwnerID, "team1")
		require.NoError(t, err)

		manager := loginUser(t, "user2")
		member := loginUser(t, "user4")
		nonMember := loginUser(t, "user5")

		settingsURL := "/org3/repo3/settings/branchroom"
		handoffURL := "/org3/repo3/-/branchroom"
		gatewayBase := u.JoinPath("_branchroom", repository.Owner.Name, repository.Name)

		scopeCount := 0
		createScope := func(t *testing.T) *branchroom_model.Scope {
			t.Helper()
			scopeCount++
			name := fmt.Sprintf("rbac-lifecycle-%d", scopeCount)
			manager.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, settingsURL, map[string]string{"name": name}), http.StatusSeeOther)
			scopes, err := branchroom_model.FindScopesByRepoID(t.Context(), repository.ID)
			require.NoError(t, err)
			for _, scope := range scopes {
				if scope.Name == name {
					return scope
				}
			}
			t.Fatalf("scope %q was not created", name)
			return nil
		}

		provisionReadyWorkspace := func(t *testing.T, scope *branchroom_model.Scope) *branchroom_model.Workspace {
			t.Helper()
			manager.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, settingsURL+"/workspaces", map[string]string{
				"scope_id": strconv.FormatInt(scope.ID, 10),
			}), http.StatusSeeOther)
			workspace, err := branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
			require.NoError(t, err)
			require.Equal(t, branchroom_model.WorkspaceStateReady, workspace.State)
			return workspace
		}

		postTeamGrantForm := func(session *TestSession, scopeID int64, teamName, action, notBefore, expires string, expectedStatus int) {
			t.Helper()
			values := map[string]string{
				"scope_id": strconv.FormatInt(scopeID, 10),
				"team":     teamName,
				"action":   action,
			}
			if notBefore != "" {
				values["not_before"] = notBefore
			}
			if expires != "" {
				values["expires"] = expires
			}
			session.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, settingsURL+"/team-grants", values), expectedStatus)
		}

		assertDeniedFlash := func(session *TestSession) {
			t.Helper()
			flash := session.GetCookieFlashMessage()
			require.NotNil(t, flash)
			assert.NotEmpty(t, flash.ErrorMsg)
		}

		assertAllowedFlash := func(session *TestSession) {
			t.Helper()
			flash := session.GetCookieFlashMessage()
			require.NotNil(t, flash)
			assert.NotEmpty(t, flash.SuccessMsg)
		}

		advertiseSource := func(routeKey, username string) branchroomGitHTTPResponse {
			infoRefsURL := gatewayBase.JoinPath(routeKey+".git", "info/refs").String() + "?service=git-upload-pack"
			return makeBranchroomGitHTTPRequest(t, http.MethodGet, infoRefsURL, username, userPassword)
		}

		datetimeLocal := func(at time.Time) string {
			return at.Format("2006-01-02T15:04")
		}

		insertWorkspace := func(t *testing.T, scope *branchroom_model.Scope, state branchroom_model.WorkspaceState, failureCode string) *branchroom_model.Workspace {
			t.Helper()
			workspace := &branchroom_model.Workspace{
				WorkspaceKey:     uuid.NewString(),
				ScopeID:          scope.ID,
				RepoID:           repository.ID,
				StoreID:          uuid.NewString(),
				PublicBaseSHA:    baseSHA,
				ObjectFormatName: repository.ObjectFormatName,
				State:            state,
				FailureCode:      failureCode,
				CreatedByID:      repository.OwnerID,
			}
			require.NoError(t, branchroom_model.InsertWorkspace(t.Context(), workspace))
			return workspace
		}

		lifecyclePost := func(action string, scopeID int64) {
			manager.MakeRequest(t, NewRequest(t, http.MethodPost, settingsURL+"/scopes/"+strconv.FormatInt(scopeID, 10)+"/workspaces/"+action), http.StatusSeeOther)
		}

		t.Run("team grant end to end", func(t *testing.T) {
			scope := createScope(t)
			workspace := provisionReadyWorkspace(t, scope)
			member.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusNotFound)

			postTeamGrantForm(manager, scope.ID, team.Name, "read_source", "", "", http.StatusSeeOther)
			assertAllowedFlash(manager)
			grant := unittest.AssertExistsAndLoadBean(t, &branchroom_model.Grant{
				ScopeID:     scope.ID,
				SubjectType: branchroom_model.SubjectTypeTeam,
				SubjectID:   team.ID,
				Action:      branchroom_model.ActionReadSource,
			})

			resp := member.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusOK)
			expectedCloneURL := gatewayBase.JoinPath(workspace.RouteKey + ".git").String()
			assert.Contains(t, resp.Body.String(), expectedCloneURL)
			nonMember.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusNotFound)

			assert.Equal(t, http.StatusOK, advertiseSource(workspace.RouteKey, "user4").StatusCode)
			assert.Equal(t, http.StatusNotFound, advertiseSource(workspace.RouteKey, "user5").StatusCode)

			revokeURL := settingsURL + "/scopes/" + strconv.FormatInt(scope.ID, 10) + "/team-grants/" + strconv.FormatInt(grant.ID, 10) + "/revoke"
			manager.MakeRequest(t, NewRequest(t, http.MethodPost, revokeURL), http.StatusSeeOther)
			storedGrant := unittest.AssertExistsAndLoadBean(t, &branchroom_model.Grant{ID: grant.ID})
			assert.NotZero(t, storedGrant.RevokedUnix)
			member.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusNotFound)
			assert.Equal(t, http.StatusNotFound, advertiseSource(workspace.RouteKey, "user4").StatusCode)
		})

		t.Run("team grant rejections", func(t *testing.T) {
			scope := createScope(t)

			postTeamGrantForm(manager, scope.ID, team.Name, "read_source", "", "", http.StatusNotFound)
			unittest.AssertNotExistsBean(t, &branchroom_model.Grant{ScopeID: scope.ID, SubjectType: branchroom_model.SubjectTypeTeam})

			for _, rejected := range []struct{ teamName, action string }{
				{team.Name + "-missing", "discover"},
				{"review_team", "discover"}, // a team that only exists in another organization
				{team.Name, "write_source"},
			} {
				postTeamGrantForm(manager, scope.ID, rejected.teamName, rejected.action, "", "", http.StatusSeeOther)
				assertDeniedFlash(manager)
			}
			unittest.AssertNotExistsBean(t, &branchroom_model.Grant{ScopeID: scope.ID, SubjectType: branchroom_model.SubjectTypeTeam})

			manager.MakeRequest(t, NewRequestWithValues(t, http.MethodPost, "/user2/repo1/settings/branchroom/team-grants", map[string]string{
				"scope_id": strconv.FormatInt(scope.ID, 10),
				"team":     "owners",
				"action":   "discover",
			}), http.StatusSeeOther)
			assertDeniedFlash(manager)
		})

		t.Run("grant time windows", func(t *testing.T) {
			scope := createScope(t)
			workspace := provisionReadyWorkspace(t, scope)

			postTeamGrantForm(manager, scope.ID, team.Name, "read_source", "", datetimeLocal(time.Now().Add(-time.Hour)), http.StatusSeeOther)
			assertDeniedFlash(manager)
			unittest.AssertNotExistsBean(t, &branchroom_model.Grant{ScopeID: scope.ID, SubjectType: branchroom_model.SubjectTypeTeam})

			futureGrant := &branchroom_model.Grant{
				ScopeID:       scope.ID,
				SubjectType:   branchroom_model.SubjectTypeTeam,
				SubjectID:     team.ID,
				Action:        branchroom_model.ActionReadSource,
				NotBeforeUnix: timeutil.TimeStampNow().AddDuration(time.Hour),
				CreatedByID:   repository.OwnerID,
			}
			require.NoError(t, branchroom_model.InsertGrant(t.Context(), futureGrant))
			member.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusNotFound)
			assert.Equal(t, http.StatusNotFound, advertiseSource(workspace.RouteKey, "user4").StatusCode)
			_, err = db.GetEngine(t.Context()).ID(futureGrant.ID).Delete(new(branchroom_model.Grant))
			require.NoError(t, err)

			postTeamGrantForm(manager, scope.ID, team.Name, "read_source", "", datetimeLocal(time.Now().Add(24*time.Hour)), http.StatusSeeOther)
			assertAllowedFlash(manager)
			activeGrant := unittest.AssertExistsAndLoadBean(t, &branchroom_model.Grant{
				ScopeID:     scope.ID,
				SubjectType: branchroom_model.SubjectTypeTeam,
				SubjectID:   team.ID,
				Action:      branchroom_model.ActionReadSource,
			})
			assert.NotZero(t, activeGrant.ExpiresUnix)
			resp := member.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusOK)
			assert.Contains(t, resp.Body.String(), gatewayBase.JoinPath(workspace.RouteKey+".git").String())
			assert.Equal(t, http.StatusOK, advertiseSource(workspace.RouteKey, "user4").StatusCode)

			_, err := db.GetEngine(t.Context()).ID(activeGrant.ID).Cols("expires_unix").Update(&branchroom_model.Grant{ExpiresUnix: timeutil.TimeStampNow().AddDuration(-time.Minute)})
			require.NoError(t, err)
			member.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusNotFound)
			assert.Equal(t, http.StatusNotFound, advertiseSource(workspace.RouteKey, "user4").StatusCode)
		})

		t.Run("workspace lifecycle matrix", func(t *testing.T) {
			scope := createScope(t)

			failed := insertWorkspace(t, scope, branchroom_model.WorkspaceStateFailed, "import_failed")
			lifecyclePost("retry", scope.ID)
			assertAllowedFlash(manager)
			retried, err := branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
			require.NoError(t, err)
			assert.Equal(t, failed.ID, retried.ID)
			assert.Equal(t, branchroom_model.WorkspaceStateReady, retried.State)

			postTeamGrantForm(manager, scope.ID, team.Name, "read_source", "", "", http.StatusSeeOther)
			assert.Equal(t, http.StatusOK, advertiseSource(retried.RouteKey, "user4").StatusCode)

			lifecyclePost("retire", scope.ID)
			assertAllowedFlash(manager)
			retired, err := branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
			require.NoError(t, err)
			assert.Equal(t, branchroom_model.WorkspaceStateRetired, retired.State)
			member.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusNotFound)
			assert.Equal(t, http.StatusNotFound, advertiseSource(retired.RouteKey, "user4").StatusCode)

			restricted, err := branchroom_module.RestrictedRepository(retired.StoreID)
			require.NoError(t, err)
			storeExists, err := git.IsRepositoryExist(t.Context(), restricted)
			require.NoError(t, err)
			assert.True(t, storeExists)

			lifecyclePost("purge", scope.ID)
			assertAllowedFlash(manager)
			purged, err := branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
			require.NoError(t, err)
			assert.Equal(t, branchroom_model.WorkspaceStatePurged, purged.State)
			storeExists, err = git.IsRepositoryExist(t.Context(), restricted)
			require.NoError(t, err)
			assert.False(t, storeExists)

			reserved := createScope(t)
			insertWorkspace(t, reserved, branchroom_model.WorkspaceStateProvisioning, "")
			lifecyclePost("purge", reserved.ID)
			assertDeniedFlash(manager)
			stillReserved, err := branchroom_model.GetWorkspaceByScopeID(t.Context(), reserved.ID)
			require.NoError(t, err)
			assert.Equal(t, branchroom_model.WorkspaceStateProvisioning, stillReserved.State)
		})

		t.Run("organization team admin manages scopes", func(t *testing.T) {
			adminUser, err := user_model.GetUserByID(t.Context(), 28)
			require.NoError(t, err)
			adminTeam := &organization_model.Team{
				OrgID:                   repository.OwnerID,
				Name:                    "branchroom-admins",
				AccessMode:              perm.AccessModeAdmin,
				IncludesAllRepositories: true,
			}
			require.NoError(t, org_service.NewTeam(t.Context(), adminTeam))
			require.NoError(t, org_service.AddTeamMember(t.Context(), adminTeam, adminUser))

			orgAdmin := loginUser(t, "user28")
			scope := createScope(t)
			provisionReadyWorkspace(t, scope)

			orgAdmin.MakeRequest(t, NewRequest(t, http.MethodGet, settingsURL), http.StatusOK)

			postTeamGrantForm(orgAdmin, scope.ID, team.Name, "discover", "", "", http.StatusSeeOther)
			assertAllowedFlash(orgAdmin)
			grant := unittest.AssertExistsAndLoadBean(t, &branchroom_model.Grant{
				ScopeID:     scope.ID,
				SubjectType: branchroom_model.SubjectTypeTeam,
				SubjectID:   team.ID,
				Action:      branchroom_model.ActionDiscover,
			})
			assert.Equal(t, adminUser.ID, grant.CreatedByID)
		})
	})
}
