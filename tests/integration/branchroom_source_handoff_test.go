// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	repo_model "gitea.dev/models/repo"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/branchroom"
	"gitea.dev/modules/git"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	"gitea.dev/routers"
	branchroom_service "gitea.dev/services/branchroom"
	"gitea.dev/tests"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchroomSourceHandoffs(t *testing.T) {
	defer test.MockVariableValue(&setting.AppURL)()
	setting.AppURL = "http://127.0.0.1:0/"

	onGiteaRun(t, func(t *testing.T, u *url.URL) {
		defer test.MockVariableValue(&setting.Branchroom)()
		setting.AppURL = u.String()
		setting.Branchroom = setting.BranchroomConfig{
			Enabled:                true,
			RestrictedRepoRootPath: branchroomTestRoot(t),
		}

		repository, err := repo_model.GetRepositoryByID(t.Context(), 1)
		require.NoError(t, err)
		baseSHA, err := git.GetBranchCommitID(t.Context(), repository.CodeStorageRepo(), repository.DefaultBranch)
		require.NoError(t, err)
		scope, err := branchroom_service.CreateRepositoryScope(t.Context(), branchroom_service.CreateRepositoryScopeRequest{
			RepoID:  repository.ID,
			Actor:   branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
			Name:    "source-handoff",
			Surface: branchroom_model.SurfaceSystem,
		})
		require.NoError(t, err)
		workspace, err := branchroom_service.CreateRepositoryWorkspace(t.Context(), branchroom_service.CreateRepositoryWorkspaceRequest{
			RepoID:        repository.ID,
			ScopeID:       scope.ID,
			Actor:         branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
			PublicBaseSHA: baseSHA,
			Surface:       branchroom_model.SurfaceSystem,
		})
		require.NoError(t, err)
		restricted, err := branchroom.RestrictedRepository(workspace.StoreID)
		require.NoError(t, err)
		require.NoError(t, git.ForceFastImport(t.Context(), restricted, []git.FastImportCommit{{
			Ref:     "refs/heads/restricted",
			Message: "restricted handoff commit",
			Files:   []git.FastImportFile{{Path: "handoff.txt", Content: "restricted handoff\n"}},
		}}))
		restrictedCommit := branchroomObjectID(t, restricted, "refs/heads/restricted")

		otherScope := &branchroom_model.Scope{
			Kind:        branchroom_model.ScopeKindRepository,
			RepoID:      repository.ID,
			Name:        "another-user-source",
			State:       branchroom_model.ScopeStateActive,
			OwnerType:   branchroom_model.ScopeOwnerTypeUser,
			OwnerID:     repository.OwnerID,
			CreatedByID: repository.OwnerID,
		}
		require.NoError(t, branchroom_model.InsertScope(t.Context(), otherScope))
		otherRouteKey, err := branchroom_model.NewWorkspaceRouteKey()
		require.NoError(t, err)
		require.NoError(t, branchroom_model.InsertWorkspace(t.Context(), &branchroom_model.Workspace{
			WorkspaceKey:     uuid.NewString(),
			RouteKey:         otherRouteKey,
			ScopeID:          otherScope.ID,
			RepoID:           repository.ID,
			StoreID:          uuid.NewString(),
			PublicBaseSHA:    baseSHA,
			ObjectFormatName: repository.ObjectFormatName,
			State:            branchroom_model.WorkspaceStateReady,
			CreatedByID:      repository.OwnerID,
		}))
		for _, action := range []branchroom_model.Action{branchroom_model.ActionDiscover, branchroom_model.ActionReadSource} {
			require.NoError(t, branchroom_model.InsertGrant(t.Context(), &branchroom_model.Grant{
				ScopeID:     otherScope.ID,
				SubjectType: branchroom_model.SubjectTypeUser,
				SubjectID:   5,
				Action:      action,
				CreatedByID: repository.OwnerID,
			}))
		}

		owner := loginUser(t, "user2")
		user := loginUser(t, "user4")
		siteAdmin := loginUser(t, "user1")
		unprivilegedUser := loginUser(t, "user5")
		handoffURL := "/user2/repo1/-/branchroom"

		assertBranchroomSourceHandoffPrivacy(t, MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusNotFound))
		goGetResponse := MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL+"?go-get=1"), http.StatusNotFound)
		assertBranchroomSourceHandoffPrivacy(t, goGetResponse)
		assert.NotContains(t, goGetResponse.Body.String(), "go-import")
		owner.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusNotFound)
		user.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusNotFound)
		siteAdmin.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusNotFound)
		ordinaryRepositoryPage := owner.MakeRequest(t, NewRequest(t, http.MethodGet, "/user2/repo1"), http.StatusOK)
		assert.NotContains(t, ordinaryRepositoryPage.Body.String(), workspace.RouteKey)

		discoveryGrant, err := branchroom_service.CreateRepositoryDiscoveryGrant(t.Context(), branchroom_service.CreateRepositoryDiscoveryGrantRequest{
			RepoID:          repository.ID,
			ScopeID:         scope.ID,
			Actor:           branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
			SubjectUsername: "user4",
			Surface:         branchroom_model.SurfaceSystem,
		})
		require.NoError(t, err)
		user.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusNotFound)
		sourceGrant, err := branchroom_service.CreateRepositorySourceReadGrant(t.Context(), branchroom_service.CreateRepositorySourceReadGrantRequest{
			RepoID:          repository.ID,
			ScopeID:         scope.ID,
			Actor:           branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
			SubjectUsername: "user4",
			Surface:         branchroom_model.SurfaceSystem,
		})
		require.NoError(t, err)

		resp := user.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusOK)
		body := resp.Body.String()
		expectedCloneURL := u.JoinPath("_branchroom", "user2", "repo1", workspace.RouteKey+".git").String()
		assertBranchroomSourceHandoffPrivacy(t, resp)
		assert.Contains(t, body, scope.Name)
		assert.Contains(t, body, expectedCloneURL)
		assert.NotContains(t, body, otherScope.Name)
		assert.NotContains(t, body, otherRouteKey)
		assert.NotContains(t, body, workspace.WorkspaceKey)
		assert.NotContains(t, body, workspace.StoreID)
		AssertHTMLElement(t, NewHTMLParser(t, resp.Body), "#branchroom-source-clone-0", true)

		cloneURL, err := url.Parse(expectedCloneURL)
		require.NoError(t, err)
		cloneURL.User = url.UserPassword("user4", userPassword)
		cloneDir := t.TempDir()
		require.NoError(t, git.Clone(t.Context(), cloneURL.String(), cloneDir, git.CloneRepoOptions{}))
		commit, _, err := gitcmd.NewCommand("rev-parse", "refs/remotes/origin/restricted").WithDir(cloneDir).RunStdString(t.Context())
		require.NoError(t, err)
		assert.Equal(t, restrictedCommit, strings.TrimSpace(commit))

		oldRouteKey := workspace.RouteKey
		owner.MakeRequest(t, NewRequest(t, http.MethodPost, "/user2/repo1/settings/branchroom/scopes/"+strconv.FormatInt(scope.ID, 10)+"/route-key/rotate"), http.StatusSeeOther)
		workspace, err = branchroom_model.GetWorkspaceByScopeID(t.Context(), scope.ID)
		require.NoError(t, err)
		assert.NotEqual(t, oldRouteKey, workspace.RouteKey)

		decisionsBeforeOldURL, err := db.GetEngine(t.Context()).Where("scope_id = ?", scope.ID).Count(new(branchroom_model.Decision))
		require.NoError(t, err)
		auditsBeforeOldURL, err := db.GetEngine(t.Context()).Where("scope_id = ?", scope.ID).Count(new(branchroom_model.AuditEvent))
		require.NoError(t, err)
		oldGatewayURL := u.JoinPath("_branchroom", "user2", "repo1", oldRouteKey+".git")
		oldAdvertisement := makeBranchroomGitHTTPRequest(t, http.MethodGet, oldGatewayURL.JoinPath("info/refs").String()+"?service=git-upload-pack", "user4", userPassword)
		oldRPC := makeBranchroomGitHTTPRequestWithBody(t, http.MethodPost, oldGatewayURL.JoinPath("git-upload-pack").String(), "user4", userPassword, "application/x-git-upload-pack-request", "")
		assert.Equal(t, http.StatusNotFound, oldAdvertisement.StatusCode)
		assert.Equal(t, oldAdvertisement, oldRPC)
		decisionsAfterOldURL, err := db.GetEngine(t.Context()).Where("scope_id = ?", scope.ID).Count(new(branchroom_model.Decision))
		require.NoError(t, err)
		auditsAfterOldURL, err := db.GetEngine(t.Context()).Where("scope_id = ?", scope.ID).Count(new(branchroom_model.AuditEvent))
		require.NoError(t, err)
		assert.Equal(t, decisionsBeforeOldURL, decisionsAfterOldURL)
		assert.Equal(t, auditsBeforeOldURL, auditsAfterOldURL)

		resp = user.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusOK)
		body = resp.Body.String()
		newCloneURL := u.JoinPath("_branchroom", "user2", "repo1", workspace.RouteKey+".git").String()
		assert.Contains(t, body, newCloneURL)
		assert.NotContains(t, body, oldRouteKey)
		cloneURL, err = url.Parse(newCloneURL)
		require.NoError(t, err)
		cloneURL.User = url.UserPassword("user4", userPassword)
		rotatedCloneDir := t.TempDir()
		require.NoError(t, git.Clone(t.Context(), cloneURL.String(), rotatedCloneDir, git.CloneRepoOptions{}))
		commit, _, err = gitcmd.NewCommand("rev-parse", "refs/remotes/origin/restricted").WithDir(rotatedCloneDir).RunStdString(t.Context())
		require.NoError(t, err)
		assert.Equal(t, restrictedCommit, strings.TrimSpace(commit))

		siteAdmin.MakeRequest(t, NewRequest(t, http.MethodPost, "/-/admin/users/4/impersonate"), http.StatusOK)
		impersonatedResponse := siteAdmin.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusNotFound)
		assertBranchroomSourceHandoffPrivacy(t, impersonatedResponse)
		assert.NotContains(t, impersonatedResponse.Body.String(), workspace.RouteKey)
		siteAdmin.MakeRequest(t, NewRequest(t, http.MethodGet, "/user/logout"), http.StatusSeeOther)

		require.NoError(t, user_model.UpdateUserCols(t.Context(), &user_model.User{ID: 4, MustChangePassword: true}, "must_change_password"))
		passwordChangeResponse := user.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusNotFound)
		assertBranchroomSourceHandoffPrivacy(t, passwordChangeResponse)
		require.NoError(t, user_model.UpdateUserCols(t.Context(), &user_model.User{ID: 4, MustChangePassword: false}, "must_change_password"))

		require.NoError(t, repo_model.UpdateRepositoryColsNoAutoTime(t.Context(), &repo_model.Repository{ID: repository.ID, IsPrivate: true}, "is_private"))
		deniedByCodePermission := unprivilegedUser.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusNotFound)
		assertBranchroomSourceHandoffPrivacy(t, deniedByCodePermission)
		require.NoError(t, repo_model.UpdateRepositoryColsNoAutoTime(t.Context(), &repo_model.Repository{ID: repository.ID, IsPrivate: false}, "is_private"))

		require.NoError(t, branchroom_service.RevokeRepositorySourceReadGrant(t.Context(), branchroom_service.RevokeRepositorySourceReadGrantRequest{
			RepoID:  repository.ID,
			ScopeID: scope.ID,
			GrantID: sourceGrant.ID,
			Actor:   branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
			Surface: branchroom_model.SurfaceSystem,
		}))
		user.MakeRequest(t, NewRequest(t, http.MethodGet, handoffURL), http.StatusNotFound)
		failedCloneDir := t.TempDir()
		assert.Error(t, git.Clone(t.Context(), cloneURL.String(), failedCloneDir, git.CloneRepoOptions{}))

		require.NoError(t, branchroom_service.RevokeRepositoryDiscoveryGrant(t.Context(), branchroom_service.RevokeRepositoryDiscoveryGrantRequest{
			RepoID:  repository.ID,
			ScopeID: scope.ID,
			GrantID: discoveryGrant.ID,
			Actor:   branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
			Surface: branchroom_model.SurfaceSystem,
		}))
	})
}

func TestBranchroomSourceHandoffsStayOpaqueWithStrictSignIn(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Branchroom)()
	defer test.MockVariableValue(&setting.Service.RequireSignInViewStrict, true)()
	defer test.MockVariableValue(&testWebRoutes, routers.NormalRoutes())()

	setting.Branchroom = setting.BranchroomConfig{Enabled: true, RestrictedRepoRootPath: branchroomTestRoot(t)}
	response := MakeRequest(t, NewRequest(t, http.MethodGet, "/user2/repo1/-/branchroom"), http.StatusNotFound)
	assertBranchroomSourceHandoffPrivacy(t, response)
}

func assertBranchroomSourceHandoffPrivacy(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	assert.Equal(t, "private, no-store", response.Header().Get("Cache-Control"))
	assert.Equal(t, "Cookie, Authorization", response.Header().Get("Vary"))
	assert.Equal(t, "no-referrer", response.Header().Get("Referrer-Policy"))
	assert.Equal(t, "noindex, nofollow", response.Header().Get("X-Robots-Tag"))
}
