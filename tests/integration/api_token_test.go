// Copyright 2018 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	activities_model "gitea.dev/models/activities"
	auth_model "gitea.dev/models/auth"
	"gitea.dev/models/db"
	issues_model "gitea.dev/models/issues"
	pull_model "gitea.dev/models/pull"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/git"
	"gitea.dev/modules/log"
	"gitea.dev/modules/queue"
	repo_module "gitea.dev/modules/repository"
	api "gitea.dev/modules/structs"
	"gitea.dev/modules/timeutil"
	repo_service "gitea.dev/services/repository"
	"gitea.dev/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPITokens(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	// TestAPIGetTokensPermission ensures that only the admin can get tokens from other users
	t.Run("GetTokensPermission", func(t *testing.T) {
		// admin can get tokens for other users
		user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
		req := NewRequest(t, "GET", "/api/v1/users/user2/tokens").
			AddBasicAuth(user.Name)
		MakeRequest(t, req, http.StatusOK)

		// non-admin can get tokens for himself
		user = unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		req = NewRequest(t, "GET", "/api/v1/users/user2/tokens").
			AddBasicAuth(user.Name)
		MakeRequest(t, req, http.StatusOK)

		// non-admin can't get tokens for other users
		user = unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
		req = NewRequest(t, "GET", "/api/v1/users/user2/tokens").
			AddBasicAuth(user.Name)
		MakeRequest(t, req, http.StatusForbidden)
	})

	// TestAPIDeleteMissingToken ensures that error is thrown when token not found
	t.Run("DeleteMissingToken", func(t *testing.T) {
		user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})

		req := NewRequestf(t, "DELETE", "/api/v1/users/user1/tokens/%d", unittest.NonexistentID).
			AddBasicAuth(user.Name)
		MakeRequest(t, req, http.StatusNotFound)
	})

	// TestAPICreateAndDeleteToken tests that token that was just created can be deleted
	t.Run("CreateAndDeleteToken", func(t *testing.T) {
		user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})

		newAccessToken := createAPIAccessTokenWithoutCleanUp(t, "test-key-1", user, []auth_model.AccessTokenScope{auth_model.AccessTokenScopeAll})
		deleteAPIAccessToken(t, newAccessToken, user)

		newAccessToken = createAPIAccessTokenWithoutCleanUp(t, "test-key-2", user, []auth_model.AccessTokenScope{auth_model.AccessTokenScopeAll})
		deleteAPIAccessToken(t, newAccessToken, user)
	})

	// TestAPIDeniesPermissionBasedOnTokenScope tests that API routes forbid access
	// when the correct token scope is not included.
	t.Run("DeniesPermissionBasedOnTokenScope", func(t *testing.T) {
		// We'll assert that each endpoint, when fetched with a token with all
		// scopes *except* the ones specified, a forbidden status code is returned.
		//
		// This is to protect against endpoints having their access check copied
		// from other endpoints and not updated.
		//
		// Test cases are in alphabetical order by URL.
		type route struct {
			url, method string
			level       auth_model.AccessTokenScopeLevel
		}
		testCases := []struct {
			categories []auth_model.AccessTokenScopeCategory
			routes     []route
		}{
			{
				[]auth_model.AccessTokenScopeCategory{auth_model.AccessTokenScopeCategoryAdmin},
				[]route{
					{"/api/v1/admin/emails", "GET", auth_model.Read},
					{"/api/v1/admin/users", "GET", auth_model.Read},
					{"/api/v1/admin/users", "POST", auth_model.Write},
					{"/api/v1/admin/users/user2", "PATCH", auth_model.Write},
					{"/api/v1/admin/users/user2/orgs", "GET", auth_model.Read},
					{"/api/v1/admin/users/user2/orgs", "POST", auth_model.Write},
					{"/api/v1/admin/orgs", "GET", auth_model.Read},
				},
			},
			{
				[]auth_model.AccessTokenScopeCategory{auth_model.AccessTokenScopeCategoryNotification},
				[]route{
					{"/api/v1/notifications", "GET", auth_model.Read},
					{"/api/v1/notifications", "PUT", auth_model.Write},
				},
			},
			{
				[]auth_model.AccessTokenScopeCategory{auth_model.AccessTokenScopeCategoryOrganization, auth_model.AccessTokenScopeCategoryRepository},
				[]route{
					{"/api/v1/org/org1/repos", "POST", auth_model.Write},
				},
			},
			{
				[]auth_model.AccessTokenScopeCategory{auth_model.AccessTokenScopeCategoryPackage},
				[]route{
					{"/api/v1/packages/user1/type/name/1", "GET", auth_model.Read},
					{"/api/v1/packages/user1/type/name/1", "DELETE", auth_model.Write},
				},
			},
			{
				[]auth_model.AccessTokenScopeCategory{auth_model.AccessTokenScopeCategoryRepository},
				[]route{
					{"/api/v1/repos/user1/repo1", "GET", auth_model.Read},
					{"/api/v1/repos/user1/repo1", "PATCH", auth_model.Write},
					{"/api/v1/repos/user1/repo1", "DELETE", auth_model.Write},
					{"/api/v1/repos/user1/repo1/branches", "GET", auth_model.Read},
					{"/api/v1/repos/user1/repo1/archive/foo", "GET", auth_model.Read},
				},
			},
			{
				[]auth_model.AccessTokenScopeCategory{auth_model.AccessTokenScopeCategoryIssue},
				[]route{
					{"/api/v1/repos/user1/repo1/issues", "GET", auth_model.Read},
				},
			},
			{
				[]auth_model.AccessTokenScopeCategory{auth_model.AccessTokenScopeCategoryRepository},
				[]route{
					{"/api/v1/repos/user1/repo1/media/foo", "GET", auth_model.Read},
					{"/api/v1/repos/user1/repo1/raw/foo", "GET", auth_model.Read},
					{"/api/v1/repos/user1/repo1/teams", "GET", auth_model.Read},
					{"/api/v1/repos/user1/repo1/teams/team1", "PUT", auth_model.Write},
					{"/api/v1/repos/user1/repo1/transfer", "POST", auth_model.Write},
					// Private repo
					{"/api/v1/repos/user2/repo2", "GET", auth_model.Read},
					// Private repo
					{"/api/v1/repos/user2/repo2", "GET", auth_model.Read},
				},
			},
			{
				[]auth_model.AccessTokenScopeCategory{auth_model.AccessTokenScopeCategoryUser},
				[]route{
					{"/api/v1/user", "GET", auth_model.Read},
					{"/api/v1/user/emails", "GET", auth_model.Read},
					{"/api/v1/user/emails", "POST", auth_model.Write},
					{"/api/v1/user/emails", "DELETE", auth_model.Write},
					{"/api/v1/user/applications/oauth2", "GET", auth_model.Read},
					{"/api/v1/user/applications/oauth2", "POST", auth_model.Write},
					{"/api/v1/users/search", "GET", auth_model.Read},
					// Private user
					{"/api/v1/users/user31", "GET", auth_model.Read},
					// Private user
					{"/api/v1/users/user31/gpg_keys", "GET", auth_model.Read},
				},
			},
		}

		// User needs to be admin so that we can verify that tokens without admin
		// scopes correctly deny access.
		user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
		assert.True(t, user.IsAdmin, "User needs to be admin")

		for _, testCase := range testCases {
			for _, route := range testCase.routes {
				requiredPermissions := make([]permission, len(testCase.categories))
				for i, category := range testCase.categories {
					requiredPermissions[i] = permission{category, route.level}
				}
				runTestCase(t, &requiredScopeTestCase{route.url, route.method, requiredPermissions}, user)
			}
		}
	})

	// TestAPIDeleteTokensPermission ensures that only the admin can delete tokens from other users
	t.Run("DeleteTokensPermission", func(t *testing.T) {
		admin := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
		user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		user4 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})

		// admin can delete tokens for other users
		createAPIAccessTokenWithoutCleanUp(t, "test-key-1", user2, []auth_model.AccessTokenScope{auth_model.AccessTokenScopeAll})
		req := NewRequest(t, "DELETE", "/api/v1/users/"+user2.LoginName+"/tokens/test-key-1").
			AddBasicAuth(admin.Name)
		MakeRequest(t, req, http.StatusNoContent)

		// non-admin can delete tokens for himself
		createAPIAccessTokenWithoutCleanUp(t, "test-key-2", user2, []auth_model.AccessTokenScope{auth_model.AccessTokenScopeAll})
		req = NewRequest(t, "DELETE", "/api/v1/users/"+user2.LoginName+"/tokens/test-key-2").
			AddBasicAuth(user2.Name)
		MakeRequest(t, req, http.StatusNoContent)

		// non-admin can't delete tokens for other users
		createAPIAccessTokenWithoutCleanUp(t, "test-key-3", user2, []auth_model.AccessTokenScope{auth_model.AccessTokenScopeAll})
		req = NewRequest(t, "DELETE", "/api/v1/users/"+user2.LoginName+"/tokens/test-key-3").
			AddBasicAuth(user4.Name)
		MakeRequest(t, req, http.StatusForbidden)
	})

	// TestAPICreateTokenScopeEscalation ensures a token-authenticated request cannot
	// mint a new token with a broader scope than the authenticating token.
	t.Run("CreateTokenScopeEscalation", func(t *testing.T) {
		user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})

		// a write:user-scoped token authenticates the create requests below
		writeUserToken := getUserToken(t, user.Name, auth_model.AccessTokenScopeWriteUser)

		// requesting a broader scope ("all") than the authenticating token is rejected
		req := NewRequestWithJSON(t, "POST", "/api/v1/users/"+user.LoginName+"/tokens", map[string]any{
			"name":   "escalated",
			"scopes": []string{"all"},
		})
		req.Request.SetBasicAuth(user.Name, writeUserToken)
		MakeRequest(t, req, http.StatusForbidden)

		// requesting a subset scope ("read:user") is allowed
		req = NewRequestWithJSON(t, "POST", "/api/v1/users/"+user.LoginName+"/tokens", map[string]any{
			"name":   "subset",
			"scopes": []string{"read:user"},
		})
		req.Request.SetBasicAuth(user.Name, writeUserToken)
		MakeRequest(t, req, http.StatusCreated)

		// password (non-token) auth may still create a token with any scope
		req = NewRequestWithJSON(t, "POST", "/api/v1/users/"+user.LoginName+"/tokens", map[string]any{
			"name":   "by-password",
			"scopes": []string{"all"},
		}).AddBasicAuth(user.Name)
		MakeRequest(t, req, http.StatusCreated)

		// a public-only token must not mint a token that drops the public-only restriction
		publicOnlyToken := getUserToken(t, user.Name, auth_model.AccessTokenScopeWriteUser, auth_model.AccessTokenScopePublicOnly)
		req = NewRequestWithJSON(t, "POST", "/api/v1/users/"+user.LoginName+"/tokens", map[string]any{
			"name":   "still-public-only",
			"scopes": []string{"write:user"},
		})
		req.Request.SetBasicAuth(user.Name, publicOnlyToken)
		resp := MakeRequest(t, req, http.StatusCreated)
		var createdToken api.AccessToken
		DecodeJSON(t, resp, &createdToken)
		assert.Contains(t, createdToken.Scopes, string(auth_model.AccessTokenScopePublicOnly))

		// an unrestricted parent token may create a narrower public-only child: public-only is a restriction,
		// not a grantable permission, so the subset check must not reject it
		req = NewRequestWithJSON(t, "POST", "/api/v1/users/"+user.LoginName+"/tokens", map[string]any{
			"name":   "narrower-public-only",
			"scopes": []string{"write:user", "public-only"},
		})
		req.Request.SetBasicAuth(user.Name, writeUserToken)
		resp = MakeRequest(t, req, http.StatusCreated)
		DecodeJSON(t, resp, &createdToken)
		assert.Contains(t, createdToken.Scopes, string(auth_model.AccessTokenScopePublicOnly))
	})
}

type permission struct {
	category auth_model.AccessTokenScopeCategory
	level    auth_model.AccessTokenScopeLevel
}

type requiredScopeTestCase struct {
	url                 string
	method              string
	requiredPermissions []permission
}

func (c *requiredScopeTestCase) Name() string {
	return fmt.Sprintf("%v %v", c.method, c.url)
}

// runTestCase Helper function to run a single test case.
func runTestCase(t *testing.T, testCase *requiredScopeTestCase, user *user_model.User) {
	t.Run(testCase.Name(), func(t *testing.T) {
		defer tests.PrintCurrentTest(t)()

		// Create a token with all scopes NOT required by the endpoint.
		var unauthorizedScopes []auth_model.AccessTokenScope
		for _, category := range auth_model.AllAccessTokenScopeCategories {
			// For permissions, Write > Read > NoAccess.  So we need to
			// find the minimum required, and only grant permission up to but
			// not including the minimum required.
			minRequiredLevel := auth_model.Write
			categoryIsRequired := false
			for _, requiredPermission := range testCase.requiredPermissions {
				if requiredPermission.category != category {
					continue
				}
				categoryIsRequired = true
				if requiredPermission.level < minRequiredLevel {
					minRequiredLevel = requiredPermission.level
				}
			}
			unauthorizedLevel := auth_model.Write
			if categoryIsRequired {
				switch minRequiredLevel {
				case auth_model.Read:
					unauthorizedLevel = auth_model.NoAccess
				case auth_model.Write:
					unauthorizedLevel = auth_model.Read
				default:
					assert.FailNow(t, "Invalid test case", "Unknown access token scope level: %v", minRequiredLevel)
				}
			}

			if unauthorizedLevel == auth_model.NoAccess {
				continue
			}
			categoryUnauthorizedScopes := auth_model.GetRequiredScopes(
				unauthorizedLevel,
				category)
			unauthorizedScopes = append(unauthorizedScopes, categoryUnauthorizedScopes...)
		}

		accessToken := createAPIAccessTokenWithoutCleanUp(t, "test-token", user, unauthorizedScopes)
		defer deleteAPIAccessToken(t, accessToken, user)

		// Request the endpoint.  Verify that permission is denied.
		req := NewRequest(t, testCase.method, testCase.url).
			AddTokenAuth(accessToken.Token)
		MakeRequest(t, req, http.StatusForbidden)
	})
}

// createAPIAccessTokenWithoutCleanUp Create an API access token and assert that
// creation succeeded.  The caller is responsible for deleting the token.
func createAPIAccessTokenWithoutCleanUp(t *testing.T, tokenName string, user *user_model.User, scopes []auth_model.AccessTokenScope) api.AccessToken {
	payload := map[string]any{
		"name":   tokenName,
		"scopes": scopes,
	}

	log.Debug("Requesting creation of token with scopes: %v", scopes)
	req := NewRequestWithJSON(t, "POST", "/api/v1/users/"+user.LoginName+"/tokens", payload).
		AddBasicAuth(user.Name)
	resp := MakeRequest(t, req, http.StatusCreated)

	newAccessToken := DecodeJSON(t, resp, &api.AccessToken{})
	unittest.AssertExistsAndLoadBean(t, &auth_model.AccessToken{
		ID:    newAccessToken.ID,
		Name:  newAccessToken.Name,
		Token: newAccessToken.Token,
		UID:   user.ID,
	})

	return *newAccessToken
}

// deleteAPIAccessToken deletes an API access token and assert that deletion succeeded.
func deleteAPIAccessToken(t *testing.T, accessToken api.AccessToken, user *user_model.User) {
	req := NewRequestf(t, "DELETE", "/api/v1/users/"+user.LoginName+"/tokens/%d", accessToken.ID).
		AddBasicAuth(user.Name)
	MakeRequest(t, req, http.StatusNoContent)

	unittest.AssertNotExistsBean(t, &auth_model.AccessToken{ID: accessToken.ID})
}

func TestAPITokenRestrictions(t *testing.T) {
	onGiteaRun(t, func(t *testing.T, baseURL *url.URL) {
		expiry := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
		req := NewRequestWithJSON(t, "POST", "/api/v1/users/user2/tokens", api.CreateAccessTokenOption{Name: "limited", Scopes: []string{"all"}, ExpiresAt: &expiry, Repositories: []string{"user2/repo1"}}).AddBasicAuth("user2")
		response := MakeRequest(t, req, http.StatusCreated)
		token := DecodeJSON(t, response, &api.AccessToken{})
		require.False(t, token.Created.IsZero())
		require.False(t, token.Updated.IsZero())
		require.Equal(t, expiry, *token.ExpiresAt)
		require.Equal(t, []string{"user2/repo1"}, token.Repositories)
		MakeRequest(t, NewRequest(t, "GET", "/api/v1/repos/user2/repo1").AddTokenAuth(token.Token), http.StatusOK)
		MakeRequest(t, NewRequest(t, "GET", "/api/v1/repos/user2/repo2").AddTokenAuth(token.Token), http.StatusNotFound)
		req = NewRequestWithJSON(t, "POST", "/api/v1/user/repos", api.CreateRepoOption{Name: "must-not-exist"}).AddTokenAuth(token.Token)
		MakeRequest(t, req, http.StatusForbidden)
		req = NewRequest(t, "GET", "/api/v1/repos/search?q=repo2&private=true").AddTokenAuth(token.Token)
		result := DecodeJSON(t, MakeRequest(t, req, http.StatusOK), &api.SearchResults{})
		assert.Empty(t, result.Data)
		cloneURL := *baseURL
		cloneURL.Path += "user2/repo2.git"
		cloneURL.User = url.UserPassword("user2", token.Token)
		t.Run("clone outside allowlist", doGitCloneFail(&cloneURL))
		stored := unittest.AssertExistsAndLoadBean(t, &auth_model.AccessToken{ID: token.ID})
		stored.ExpiresUnix = timeutil.TimeStampNow() - 1
		require.NoError(t, auth_model.UpdateAccessToken(t.Context(), stored))
		MakeRequest(t, NewRequest(t, "GET", "/api/v1/repos/user2/repo1").AddTokenAuth(token.Token), http.StatusUnauthorized)
		cloneURL.Path = baseURL.Path + "user2/repo2.git"
		MakeRequest(t, NewRequest(t, "GET", "/user2/repo2.git/info/refs?service=git-upload-pack").AddBasicAuth("user2", token.Token), http.StatusUnauthorized)
		t.Run("clone expired", doGitCloneFail(&cloneURL))
	})
}

func TestAPITokenRepositoryBoundaries(t *testing.T) {
	onGiteaRun(t, func(t *testing.T, _ *url.URL) {
		expiry := time.Now().UTC().Add(time.Hour)
		create := func(user, name string) *api.AccessToken {
			req := NewRequestWithJSON(t, "POST", "/api/v1/users/"+user+"/tokens", api.CreateAccessTokenOption{
				Name: name, Scopes: []string{"all"}, ExpiresAt: &expiry, Repositories: []string{"user2/repo1"},
			}).AddBasicAuth(user)
			return DecodeJSON(t, MakeRequest(t, req, http.StatusCreated), &api.AccessToken{})
		}
		token := create("user2", "boundaries")
		stored := unittest.AssertExistsAndLoadBean(t, &auth_model.AccessToken{ID: token.ID})
		doer := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		doer.ExtDoerData = user_model.NewAccessTokenExtDoerData(stored)

		t.Run("push commit references", func(t *testing.T) {
			issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 4})
			require.True(t, issue.IsClosed)
			comments := issue.NumComments
			req := NewRequestWithJSON(t, "POST", "/api/v1/repos/user2/repo1/contents/token-boundary.txt", api.CreateFileOptions{
				FileOptions: api.FileOptions{Message: "reopen user2/repo2#1"}, ContentBase64: "eA==",
			}).AddTokenAuth(token.Token)
			MakeRequest(t, req, http.StatusCreated)
			require.NoError(t, queue.GetManager().FlushAll(t.Context(), 10*time.Second))
			issue = unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 4})
			assert.True(t, issue.IsClosed)
			assert.Equal(t, comments, issue.NumComments)
		})

		t.Run("notifications", func(t *testing.T) {
			for _, path := range []string{"/notifications?all=true", "/notifications/new", "/notifications/threads/5"} {
				MakeRequest(t, NewRequest(t, "GET", "/api/v1"+path).AddTokenAuth(token.Token), http.StatusForbidden)
			}
			rows, err := db.Find[activities_model.Notification](t.Context(), activities_model.FindNotificationOptions{UserID: 2, Doer: doer})
			require.NoError(t, err)
			require.NotEmpty(t, rows)
			for _, row := range rows {
				assert.EqualValues(t, 1, row.RepoID)
			}
		})

		t.Run("own feeds", func(t *testing.T) {
			MakeRequest(t, NewRequest(t, "GET", "/api/v1/users/user2/activities/feeds").AddTokenAuth(token.Token), http.StatusForbidden)
			response := MakeRequest(t, NewRequest(t, "GET", "/user2.rss").AddBasicAuth("user2", token.Token), http.StatusOK)
			assert.NotContains(t, response.Body.String(), "issue4")
			feeds, _, err := activities_model.GetFeeds(t.Context(), activities_model.GetFeedsOptions{Actor: doer, RequestedUser: doer, IncludePrivate: true})
			require.NoError(t, err)
			for _, feed := range feeds {
				assert.EqualValues(t, 1, feed.RepoID)
			}
		})

		t.Run("admin and sudo", func(t *testing.T) {
			admin := create("user1", "limited-admin")
			MakeRequest(t, NewRequest(t, "GET", "/api/v1/users/user1").AddTokenAuth(admin.Token), http.StatusOK)
			for _, path := range []string{"/admin/users", "/admin/emails", "/admin/actions/runs", "/admin/actions/jobs", "/orgs/org3/hooks", "/repos/user2/repo1?sudo=user2", "/users/user2", "/users/user31"} {
				MakeRequest(t, NewRequest(t, "GET", "/api/v1"+path).AddTokenAuth(admin.Token), http.StatusForbidden)
			}
		})

		t.Run("repository creation", func(t *testing.T) {
			for _, route := range []string{"forks", "generate", "transfer", "transfer/accept", "transfer/reject"} {
				for _, owner := range []string{"user2", "org3"} {
					req := NewRequestWithJSON(t, "POST", "/api/v1/repos/user2/repo1/"+route, map[string]string{"name": "must-not-exist", "owner": owner, "organization": owner, "new_owner": owner}).AddTokenAuth(token.Token)
					MakeRequest(t, req, http.StatusForbidden)
				}
			}
			unittest.AssertNotExistsBean(t, &repo_model.Repository{Name: "must-not-exist"})
		})

		t.Run("account reads", func(t *testing.T) {
			for _, path := range []string{"/user/emails", "/user/keys", "/user/gpg_keys", "/user/teams", "/orgs/org3/teams", "/orgs/org3/actions/runners", "/orgs/org3/actions/variables", "/orgs/org3/actions/secrets", "/users/user2/tokens", "/users/user4", "/teams/1/repos/org3/repo3", "/teams/1/repos/org3/nope"} {
				MakeRequest(t, NewRequest(t, "GET", "/api/v1"+path).AddTokenAuth(token.Token), http.StatusForbidden)
			}
			for _, path := range []string{"/user", "/version", "/users/user2", "/settings/api", "/repos/search"} {
				MakeRequest(t, NewRequest(t, "GET", "/api/v1"+path).AddTokenAuth(token.Token), http.StatusOK)
			}
		})

		t.Run("issue search and catalog", func(t *testing.T) {
			response := MakeRequest(t, NewRequest(t, "GET", "/api/v1/repos/issues/search?state=all").AddTokenAuth(token.Token), http.StatusOK)
			issues := DecodeJSON(t, response, []*api.Issue{})
			require.NotEmpty(t, issues)
			for _, issue := range issues {
				assert.Equal(t, "repo1", issue.Repo.Name)
			}
			MakeRequest(t, NewRequest(t, "GET", "/v2/_catalog").AddBasicAuth("user2", token.Token), http.StatusNotFound)
		})

		t.Run("hidden repository error", func(t *testing.T) {
			var messages []string
			for _, name := range []string{"user10/repo6", "user10/no-such-repo"} {
				request := NewRequestWithJSON(t, "POST", "/api/v1/users/user2/tokens", api.CreateAccessTokenOption{Name: "hidden", Scopes: []string{"all"}, Repositories: []string{name}}).AddBasicAuth("user2")
				response := MakeRequest(t, request, http.StatusUnprocessableEntity)
				messages = append(messages, response.Body.String())
			}
			assert.Equal(t, messages[0], messages[1])
		})

		t.Run("fork parent", func(t *testing.T) {
			request := NewRequestWithJSON(t, "POST", "/api/v1/users/user1/tokens", api.CreateAccessTokenOption{Name: "fork-only", Scopes: []string{"all"}, Repositories: []string{"user20/big_test_private_fork_7"}}).AddBasicAuth("user1")
			forkToken := DecodeJSON(t, MakeRequest(t, request, http.StatusCreated), &api.AccessToken{})
			response := MakeRequest(t, NewRequest(t, "GET", "/api/v1/repos/user20/big_test_private_fork_7").AddTokenAuth(forkToken.Token), http.StatusOK)
			fork := DecodeJSON(t, response, &api.Repository{})
			assert.True(t, fork.Fork)
			assert.Nil(t, fork.Parent)
		})

		t.Run("reserved path names", func(t *testing.T) {
			for _, name := range []string{"forks", "generate"} {
				request := NewRequestWithJSON(t, "POST", "/api/v1/repos/user2/repo1/contents/scripts/"+name, api.CreateFileOptions{ContentBase64: "eA=="}).AddTokenAuth(token.Token)
				MakeRequest(t, request, http.StatusCreated)
				request = NewRequestWithJSON(t, "POST", "/api/v1/repos/user2/repo1/branches", api.CreateBranchRepoOption{BranchName: "feature/" + name}).AddTokenAuth(token.Token)
				MakeRequest(t, request, http.StatusCreated)
				MakeRequest(t, NewRequest(t, "DELETE", "/api/v1/repos/user2/repo1/branches/feature/"+name).AddTokenAuth(token.Token), http.StatusNoContent)
				MakeRequest(t, NewRequest(t, "PUT", "/api/v1/repos/user2/repo1/topics/"+name).AddTokenAuth(token.Token), http.StatusNoContent)
			}
		})

		t.Run("team repository writes", func(t *testing.T) {
			request := NewRequestWithJSON(t, "POST", "/api/v1/users/user2/tokens", api.CreateAccessTokenOption{Name: "team-repo", Scopes: []string{"all"}, Repositories: []string{"org3/repo3"}}).AddBasicAuth("user2")
			teamToken := DecodeJSON(t, MakeRequest(t, request, http.StatusCreated), &api.AccessToken{})
			for _, method := range []string{"PUT", "DELETE"} {
				MakeRequest(t, NewRequest(t, method, "/api/v1/teams/2/repos/org3/repo3").AddTokenAuth(teamToken.Token), http.StatusForbidden)
			}
		})

		t.Run("sudo file commit", func(t *testing.T) {
			admin := getUserToken(t, "user1", auth_model.AccessTokenScopeAll)
			request := NewRequestWithJSON(t, "POST", "/api/v1/repos/user2/repo1/contents/sudo.txt?sudo=user2", api.CreateFileOptions{ContentBase64: "eA=="}).AddTokenAuth(admin)
			MakeRequest(t, request, http.StatusCreated)
		})

		t.Run("userinfo memberships", func(t *testing.T) {
			response := MakeRequest(t, NewRequest(t, "GET", "/login/oauth/userinfo").AddTokenAuth(token.Token), http.StatusOK)
			info := DecodeJSON(t, response, &map[string]any{})
			assert.NotContains(t, *info, "groups")
		})

		t.Run("revoked push credential", func(t *testing.T) {
			pushToken := create("user2", "queued-push")
			source := unittest.AssertExistsAndLoadBean(t, &auth_model.AccessToken{ID: pushToken.ID})
			credential := user_model.NewAccessTokenExtDoerData(source).EncodeToString()
			request := NewRequestWithJSON(t, "POST", "/api/v1/repos/user2/repo1/contents/queued.txt", api.CreateFileOptions{
				FileOptions: api.FileOptions{Message: "reopen user2/repo2#1"}, ContentBase64: "eA==",
			}).AddTokenAuth(pushToken.Token)
			response := DecodeJSON(t, MakeRequest(t, request, http.StatusCreated), &api.FileResponse{})
			require.NoError(t, queue.GetManager().FlushAll(t.Context(), 10*time.Second))
			deleteAPIAccessToken(t, *pushToken, doer)
			issue := unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 4})
			comments := issue.NumComments
			countActions := func() int64 {
				count, err := db.GetEngine(t.Context()).Count(&activities_model.Action{RepoID: 1})
				require.NoError(t, err)
				return count
			}
			before := countActions()
			require.NoError(t, repo_service.PushUpdates(&repo_module.PushUpdateOptions{
				PusherID: 2, PusherName: "user2", RepoUserName: "user2", RepoName: "repo1", UserExtDoerData: credential,
				RefFullName: git.RefNameFromBranch("master"), OldCommitID: git.Sha1ObjectFormat.EmptyObjectID().String(), NewCommitID: response.Commit.SHA,
			}))
			require.NoError(t, queue.GetManager().FlushAll(t.Context(), 10*time.Second))
			assert.Greater(t, countActions(), before)
			issue = unittest.AssertExistsAndLoadBean(t, &issues_model.Issue{ID: 4})
			assert.True(t, issue.IsClosed)
			assert.Equal(t, comments, issue.NumComments)
		})

		t.Run("auto merge credential", func(t *testing.T) {
			require.NoError(t, pull_model.ScheduleAutoMerge(t.Context(), doer, 2, repo_model.MergeStyleMerge, "", true))
			exists, scheduled, err := pull_model.GetScheduledMergeByPullID(t.Context(), 2)
			require.NoError(t, err)
			require.True(t, exists)
			reloaded, err := user_model.GetDoerPermissionUser(t.Context(), scheduled.DoerID, scheduled.UserExtDoerData)
			require.NoError(t, err)
			assert.Equal(t, []int64{1}, user_model.GetDoerRepositoryIDs(reloaded))
			assert.False(t, user_model.DoerTokenAllowsRepositoryID(reloaded, 2))
			stored.ExpiresUnix = timeutil.TimeStampNow() - 1
			require.NoError(t, auth_model.UpdateAccessToken(t.Context(), stored))
			_, err = user_model.GetDoerPermissionUser(t.Context(), scheduled.DoerID, scheduled.UserExtDoerData)
			require.Error(t, err)
		})
	})
}

func TestBranchroomTokenFormats(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})
	token := createAPIAccessTokenWithoutCleanUp(t, "prefixed", user, []auth_model.AccessTokenScope{auth_model.AccessTokenScopeAll})
	assert.True(t, strings.HasPrefix(token.Token, auth_model.AccessTokenPrefix))
	MakeRequest(t, NewRequest(t, "GET", "/api/v1/user").AddTokenAuth(token.Token), http.StatusOK)
	legacyToken := strings.TrimPrefix(token.Token, auth_model.AccessTokenPrefix)
	assert.Len(t, legacyToken, 40)
	stored, err := auth_model.GetAccessTokenByID(t.Context(), token.ID, user.ID)
	require.NoError(t, err)
	stored.TokenHash = auth_model.HashToken(legacyToken, stored.TokenSalt)
	require.NoError(t, auth_model.UpdateAccessToken(t.Context(), stored))
	MakeRequest(t, NewRequest(t, "GET", "/api/v1/user").AddTokenAuth(legacyToken), http.StatusOK)
}

func TestBranchroomAgentComment(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	identity := &api.AgentIdentity{Agent: "Opus 5.5", TaskURL: "https://example.org/tasks/1", Sponsor: "user2"}
	req := NewRequestWithJSON(t, "POST", "/api/v1/users/user2/tokens", &api.CreateAccessTokenOption{Name: "agent-comment", Scopes: []string{"all"}, AgentIdentity: identity}).AddBasicAuth("user2")
	token := DecodeJSON(t, MakeRequest(t, req, http.StatusCreated), api.AccessToken{})
	assert.Equal(t, identity, token.AgentIdentity)
	req = NewRequestWithJSON(t, "POST", "/api/v1/repos/user2/repo1/issues/1/comments", &api.CreateIssueCommentOption{Body: "recorded by the server"}).AddTokenAuth(token.Token)
	comment := DecodeJSON(t, MakeRequest(t, req, http.StatusCreated), api.Comment{})
	assert.Equal(t, identity, comment.AgentIdentity)
	req = NewRequestf(t, "GET", "/api/v1/repos/user2/repo1/issues/comments/%d", comment.ID).AddTokenAuth(token.Token)
	fetched := DecodeJSON(t, MakeRequest(t, req, http.StatusOK), api.Comment{})
	assert.Equal(t, identity, fetched.AgentIdentity)
	page := MakeRequest(t, NewRequest(t, "GET", "/user2/repo1/issues/1"), http.StatusOK)
	assert.Contains(t, page.Body.String(), "via Opus 5.5")
}
