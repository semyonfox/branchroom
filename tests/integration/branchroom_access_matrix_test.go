// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	actions_model "gitea.dev/models/actions"
	auth_model "gitea.dev/models/auth"
	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/git"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	branchroom_service "gitea.dev/services/branchroom"
	"gitea.dev/services/oauth2_provider"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchroomAccessMatrices(t *testing.T) {
	defer test.MockVariableValue(&setting.AppURL, "http://127.0.0.1:0/")()
	onGiteaRun(t, func(t *testing.T, u *url.URL) {
		defer test.MockVariableValue(&setting.Branchroom, setting.BranchroomConfig{Enabled: true, RestrictedRepoRootPath: branchroomTestRoot(t), MaxGitRequestSize: 1 << 20})()
		repository, err := repo_model.GetRepositoryByID(t.Context(), 3)
		require.NoError(t, err)
		base, err := git.GetBranchCommitID(t.Context(), repository.CodeStorageRepo(), repository.DefaultBranch)
		require.NoError(t, err)
		scope, err := branchroom_service.CreateRepositoryScope(t.Context(), branchroom_service.CreateRepositoryScopeRequest{RepoID: 3, Actor: branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: 2}, Name: "access-matrix", Surface: branchroom_model.SurfaceSystem})
		require.NoError(t, err)
		workspace, err := branchroom_service.CreateRepositoryWorkspace(t.Context(), branchroom_service.CreateRepositoryWorkspaceRequest{RepoID: 3, ScopeID: scope.ID, Actor: branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: 2}, PublicBaseSHA: base, Surface: branchroom_model.SurfaceSystem})
		require.NoError(t, err)
		session := loginUser(t, "user4")
		readPAT := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeReadRepository)
		publicPAT := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopePublicOnly, auth_model.AccessTokenScopeReadRepository)
		writePAT := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeWriteRepository)
		grant := &auth_model.OAuth2Grant{UserID: 4, ApplicationID: 1, Scope: "read:repository write:repository"}
		require.NoError(t, db.Insert(t.Context(), grant))
		token := &oauth2_provider.Token{GrantID: grant.ID, Kind: oauth2_provider.KindAccessToken, RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}
		oauth, err := token.SignToken(oauth2_provider.DefaultSigningKey)
		require.NoError(t, err)
		task := unittest.AssertExistsAndLoadBean(t, &actions_model.ActionTask{ID: 47})
		task.GenerateAndFillToken()
		task.Status = actions_model.StatusRunning
		require.NoError(t, actions_model.UpdateTask(t.Context(), task, "token_hash", "token_salt", "token_last_eight", "status"))
		read := branchroom_model.ActionReadSource
		discover := branchroom_model.ActionDiscover
		write := branchroom_model.ActionWriteSource
		rows := []struct {
			name       string
			userID     int64
			actions    []branchroom_model.Action
			team       bool
			credential string
			statuses   [5]int
		}{
			{name: "anonymous", statuses: [5]int{401, 401, 401, 401, 404}},
			{name: "unrelated", userID: 5, statuses: [5]int{404, 404, 404, 404, 404}},
			{name: "collaborator without grant", userID: 4, statuses: [5]int{404, 404, 404, 404, 404}},
			{name: "team read alone", userID: 4, actions: []branchroom_model.Action{read}, team: true, statuses: [5]int{200, 200, 404, 404, 404}},
			{name: "team discovery and read", userID: 4, actions: []branchroom_model.Action{discover, read}, team: true, statuses: [5]int{200, 200, 404, 404, 200}},
			{name: "discover only", userID: 4, actions: []branchroom_model.Action{discover}, statuses: [5]int{404, 404, 404, 404, 404}},
			{name: "direct read alone", userID: 4, actions: []branchroom_model.Action{read}, statuses: [5]int{200, 200, 404, 404, 404}},
			{name: "direct discovery and read", userID: 4, actions: []branchroom_model.Action{discover, read}, statuses: [5]int{200, 200, 404, 404, 200}},
			{name: "direct write", userID: 4, actions: []branchroom_model.Action{discover, read, write}, statuses: [5]int{200, 200, 200, 200, 200}},
			{name: "owner without grants", userID: 2, statuses: [5]int{404, 404, 404, 404, 404}},
			{name: "site admin", userID: 1, statuses: [5]int{404, 404, 404, 404, 404}},
			{name: "Actions token", userID: 4, actions: []branchroom_model.Action{discover, read, write}, credential: task.Token, statuses: [5]int{404, 404, 404, 404, 404}},
			{name: "OAuth password", userID: 4, actions: []branchroom_model.Action{discover, read, write}, credential: oauth, statuses: [5]int{404, 404, 404, 404, 404}},
			{name: "OAuth username", userID: 4, actions: []branchroom_model.Action{discover, read, write}, credential: oauth, statuses: [5]int{404, 404, 404, 404, 404}},
			{name: "PAT read", userID: 4, actions: []branchroom_model.Action{discover, read, write}, credential: readPAT, statuses: [5]int{200, 200, 404, 404, 404}},
			{name: "PAT public only", userID: 4, actions: []branchroom_model.Action{discover, read, write}, credential: publicPAT, statuses: [5]int{404, 404, 404, 404, 404}},
			{name: "PAT write", userID: 4, actions: []branchroom_model.Action{discover, read, write}, credential: writePAT, statuses: [5]int{200, 200, 200, 200, 404}},
			{name: "password with 2FA", userID: 4, actions: []branchroom_model.Action{discover, read, write}, statuses: [5]int{401, 401, 401, 401, 200}},
			{name: "prohibited", userID: 4, actions: []branchroom_model.Action{discover, read, write}, statuses: [5]int{401, 401, 401, 401, 404}},
			{name: "deactivated", userID: 4, actions: []branchroom_model.Action{discover, read, write}, statuses: [5]int{404, 404, 404, 404, 404}},
		}
		gateway := u.JoinPath("_branchroom", "org3", "repo3", workspace.RouteKey+".git")
		operations := []struct{ name, method, url, contentType string }{
			{"upload advertisement", "GET", gateway.JoinPath("info/refs").String() + "?service=git-upload-pack", ""},
			{"upload RPC", "POST", gateway.JoinPath("git-upload-pack").String(), "application/x-git-upload-pack-request"},
			{"receive advertisement", "GET", gateway.JoinPath("info/refs").String() + "?service=git-receive-pack", ""},
			{"receive RPC", "POST", gateway.JoinPath("git-receive-pack").String(), "application/x-git-receive-pack-request"},
			{"handoff", "GET", u.JoinPath("org3", "repo3", "-", "branchroom").String(), ""},
		}
		for _, row := range rows {
			t.Run(row.name, func(t *testing.T) {
				_, err := db.GetEngine(t.Context()).Where("scope_id = ?", scope.ID).Delete(new(branchroom_model.Grant))
				require.NoError(t, err)
				for _, action := range row.actions {
					subjectType, subjectID := branchroom_model.SubjectTypeUser, row.userID
					if row.team {
						subjectType, subjectID = branchroom_model.SubjectTypeTeam, 2
					}
					require.NoError(t, branchroom_model.InsertGrant(t.Context(), &branchroom_model.Grant{ScopeID: scope.ID, SubjectType: subjectType, SubjectID: subjectID, Action: action, CreatedByID: 2}))
				}
				username, password := "", ""
				var webSession *TestSession
				if row.userID != 0 {
					user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: row.userID})
					username, password = user.Name, userPassword
					if row.credential == "" {
						webSession = loginUser(t, user.Name)
					} else {
						password = row.credential
					}
				}
				if row.name == "OAuth username" {
					username, password = row.credential, "x-oauth-basic"
				}
				if row.name == "password with 2FA" {
					twoFactor := &auth_model.TwoFactor{UID: 4}
					require.NoError(t, twoFactor.SetSecret("JBSWY3DPEHPK3PXP"))
					require.NoError(t, auth_model.NewTwoFactor(t.Context(), twoFactor))
					defer func() {
						_, err := db.GetEngine(t.Context()).Where("uid = ?", 4).Delete(new(auth_model.TwoFactor))
						require.NoError(t, err)
					}()
				}
				if row.name == "prohibited" {
					require.NoError(t, user_model.UpdateUserCols(t.Context(), &user_model.User{ID: 4, ProhibitLogin: true}, "prohibit_login"))
					defer func() {
						require.NoError(t, user_model.UpdateUserCols(t.Context(), &user_model.User{ID: 4, ProhibitLogin: false}, "prohibit_login"))
					}()
				}
				if row.name == "deactivated" {
					require.NoError(t, user_model.UpdateUserCols(t.Context(), &user_model.User{ID: 4, IsActive: false}, "is_active"))
					defer func() {
						require.NoError(t, user_model.UpdateUserCols(t.Context(), &user_model.User{ID: 4, IsActive: true}, "is_active"))
					}()
				}
				for column, operation := range operations {
					t.Run(operation.name, func(t *testing.T) {
						if column == 4 && webSession != nil {
							webSession.MakeRequest(t, NewRequest(t, http.MethodGet, "/org3/repo3/-/branchroom"), row.statuses[column])
							return
						}
						response := makeBranchroomGitHTTPRequestWithBody(t, operation.method, operation.url, username, password, operation.contentType, "0000")
						require.Equal(t, row.statuses[column], response.StatusCode)
					})
				}
			})
		}

		t.Run("oversized chunked body", func(t *testing.T) {
			_, err := db.GetEngine(t.Context()).Where("scope_id = ?", scope.ID).Delete(new(branchroom_model.Grant))
			require.NoError(t, err)
			require.NoError(t, branchroom_model.InsertGrant(t.Context(), &branchroom_model.Grant{ScopeID: scope.ID, SubjectType: branchroom_model.SubjectTypeUser, SubjectID: 4, Action: read, CreatedByID: 2}))
			chunked := func(req *http.Request) { req.ContentLength = -1 }
			oversized := strings.Repeat("0", 1<<20+1)
			response := makeBranchroomGitHTTPRequestWithRequest(t, http.MethodPost, operations[1].url, "user4", userPassword, operations[1].contentType, oversized, chunked)
			assert.Equal(t, http.StatusRequestEntityTooLarge, response.StatusCode)
			response = makeBranchroomGitHTTPRequestWithRequest(t, http.MethodPost, operations[1].url, "", "", operations[1].contentType, oversized, chunked)
			assert.Equal(t, http.StatusUnauthorized, response.StatusCode)
		})
	})
}
