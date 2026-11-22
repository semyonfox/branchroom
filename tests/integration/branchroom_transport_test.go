// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	audit_model "gitea.dev/models/audit"
	auth_model "gitea.dev/models/auth"
	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	"gitea.dev/models/perm"
	access_model "gitea.dev/models/perm/access"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unit"
	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/branchroom"
	"gitea.dev/modules/git"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/git/gitrepo"
	"gitea.dev/modules/private"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	branchroom_service "gitea.dev/services/branchroom"
	repository_service "gitea.dev/services/repository"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchroomUnregisteredStoreDoesNotLeakThroughPublicGitHTTP(t *testing.T) {
	defer test.MockVariableValue(&setting.AppURL)()
	setting.AppURL = "http://127.0.0.1:0/"

	onGiteaRun(t, func(t *testing.T, u *url.URL) {
		defer test.MockVariableValue(&setting.Branchroom)()
		defer test.MockVariableValue(&setting.LocalURL)()
		setting.Branchroom = setting.BranchroomConfig{
			Enabled:                true,
			RestrictedRepoRootPath: branchroomTestRoot(t),
		}
		setting.LocalURL = u.String()

		restricted, err := branchroom.RestrictedRepository("a13e2a27-167d-4586-a511-42910dc3a955")
		require.NoError(t, err)
		require.NoError(t, git.InitRepository(t.Context(), restricted, git.Sha1ObjectFormat.Name(), ""))
		require.NoError(t, git.ForceFastImport(t.Context(), restricted, []git.FastImportCommit{{
			Ref:     "refs/heads/restricted",
			Message: "restricted commit",
			Files: []git.FastImportFile{
				{Path: "restricted.txt", Content: "restricted only\n"},
			},
		}}))

		restrictedCommit := branchroomObjectID(t, restricted, "refs/heads/restricted")
		restrictedBlob := branchroomObjectID(t, restricted, "refs/heads/restricted:restricted.txt")
		publicURL := u.JoinPath("user2/repo1.git")
		for _, path := range []string{
			"/user2/repo1/raw/commit/" + restrictedCommit + "/restricted.txt",
			"/user2/repo1/src/commit/" + restrictedCommit + "/restricted.txt",
			"/user2/repo1/blame/commit/" + restrictedCommit + "/restricted.txt",
			"/user2/repo1/commit/" + restrictedCommit,
			"/user2/repo1/commit/" + restrictedCommit + ".diff",
			"/user2/repo1/compare/master..." + restrictedCommit,
			"/user2/repo1/archive/" + restrictedCommit + ".zip",
			"/api/v1/repos/user2/repo1/git/commits/" + restrictedCommit,
			"/api/v1/repos/user2/repo1/git/blobs/" + restrictedBlob,
			"/api/v1/repos/user2/repo1/contents/restricted.txt?ref=" + restrictedCommit,
			"/api/v1/repos/user2/repo1/archive/" + restrictedCommit + ".zip",
		} {
			t.Run(path, func(t *testing.T) {
				expected := http.StatusNotFound
				if strings.Contains(path, "/git/blobs/") {
					expected = http.StatusBadRequest
				}
				MakeRequest(t, NewRequest(t, http.MethodGet, path), expected)
			})
		}

		t.Run("smart HTTP advertisement", func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, publicURL.JoinPath("info/refs").String()+"?service=git-upload-pack", nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.NotContains(t, string(body), restrictedCommit)
		})

		t.Run("known SHA fetch", func(t *testing.T) {
			local := t.TempDir()
			require.NoError(t, git.InitRepositoryLocal(t.Context(), local, false, git.Sha1ObjectFormat.Name(), ""))
			_, _, err := gitcmd.NewCommand("fetch").AddDynamicArguments(publicURL.String(), restrictedCommit).
				WithDir(local).RunStdString(t.Context())
			assert.Error(t, err)
		})

		t.Run("dumb HTTP object", func(t *testing.T) {
			objectPath := "objects/" + restrictedBlob[:2] + "/" + restrictedBlob[2:]
			req, err := http.NewRequest(http.MethodGet, publicURL.JoinPath(objectPath).String(), nil)
			require.NoError(t, err)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			assert.Equal(t, http.StatusNotFound, resp.StatusCode)
			body, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.NotContains(t, string(body), "restricted only")
		})

		t.Run("remote archive", func(t *testing.T) {
			_, _, err := gitcmd.NewCommand("archive").AddOptionValues("--remote", publicURL.String()).
				AddDynamicArguments(restrictedCommit).WithDir(t.TempDir()).RunStdString(t.Context())
			assert.Error(t, err)
		})

		t.Run("SSH storage resolution", func(t *testing.T) {
			results, extra := private.ServCommand(t.Context(), 1, "user2", "repo1", perm.AccessModeRead, "git-upload-pack", "")
			require.NoError(t, extra.Error)

			rel, err := filepath.Rel(setting.Branchroom.RestrictedRepoRootPath, results.RepoStoragePath)
			require.NoError(t, err)
			assert.True(t, rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)))
		})
	})
}

func TestBranchroomSmartHTTPReadGateway(t *testing.T) {
	defer test.MockVariableValue(&setting.AppURL)()
	setting.AppURL = "http://127.0.0.1:0/"

	onGiteaRun(t, func(t *testing.T, u *url.URL) {
		defer test.MockVariableValue(&setting.Branchroom)()
		defer test.MockVariableValue(&setting.Repository.DisableHTTPGit)()
		setting.Branchroom = setting.BranchroomConfig{
			Enabled:                true,
			RestrictedRepoRootPath: branchroomTestRoot(t),
			MaxGitRequestSize:      1 << 20,
		}

		repository, err := repo_model.GetRepositoryByID(t.Context(), 1)
		require.NoError(t, err)
		baseSHA, err := git.GetBranchCommitID(t.Context(), repository.CodeStorageRepo(), repository.DefaultBranch)
		require.NoError(t, err)
		scope, err := branchroom_service.CreateRepositoryScope(t.Context(), branchroom_service.CreateRepositoryScopeRequest{
			RepoID:  repository.ID,
			Actor:   branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
			Name:    "smart-http-read",
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
			Message: "restricted commit",
			Files: []git.FastImportFile{
				{Path: "restricted.txt", Content: "restricted only\n"},
			},
		}}))
		restrictedCommit := branchroomObjectID(t, restricted, "refs/heads/restricted")

		gatewayURL := u.JoinPath("_branchroom", "user2", "repo1", workspace.RouteKey+".git")
		infoRefsURL := gatewayURL.JoinPath("info/refs").String() + "?service=git-upload-pack"
		unknownRouteKey := strings.Repeat("a", 26)
		if unknownRouteKey == workspace.RouteKey {
			unknownRouteKey = strings.Repeat("b", 26)
		}
		unknownGatewayURL := u.JoinPath("_branchroom", "user2", "repo1", unknownRouteKey+".git")
		unknownInfoRefsURL := unknownGatewayURL.JoinPath("info/refs").String() + "?service=git-upload-pack"

		t.Run("anonymous requests do not disclose the route key", func(t *testing.T) {
			known := makeBranchroomGitHTTPRequest(t, http.MethodGet, infoRefsURL, "", "")
			unknown := makeBranchroomGitHTTPRequest(t, http.MethodGet, unknownInfoRefsURL, "", "")
			assert.Equal(t, http.StatusUnauthorized, known.StatusCode)
			assert.Equal(t, known, unknown)
			assert.Equal(t, "private, no-store", known.CacheControl)
			assert.Equal(t, "Authorization", known.Vary)
			assert.NotEmpty(t, known.WWWAuthenticate)
			assert.NotContains(t, known.Body, workspace.RouteKey)
			assert.NotContains(t, known.Body, workspace.StoreID)
			assert.NotContains(t, known.Body, workspace.WorkspaceKey)

			session := loginUser(t, "user4")
			sessionCookie := session.GetRawCookie(setting.SessionConfig.CookieName)
			require.NotNil(t, sessionCookie)
			fromSession := makeBranchroomGitHTTPRequestWithRequest(t, http.MethodGet, infoRefsURL, "", "", "", "", func(req *http.Request) {
				req.AddCookie(sessionCookie)
			})
			fromBearer := makeBranchroomGitHTTPRequestWithRequest(t, http.MethodGet, infoRefsURL, "", "", "", "", func(req *http.Request) {
				req.Header.Set("Authorization", "Bearer ignored-by-branchroom")
			})
			assert.Equal(t, known, fromSession)
			assert.Equal(t, known, fromBearer)
		})

		t.Run("an authenticated ordinary reader still needs a source grant", func(t *testing.T) {
			known := makeBranchroomGitHTTPRequest(t, http.MethodGet, infoRefsURL, "user4", userPassword)
			unknown := makeBranchroomGitHTTPRequest(t, http.MethodGet, unknownInfoRefsURL, "user4", userPassword)
			assert.Equal(t, http.StatusNotFound, known.StatusCode)
			assert.Equal(t, known, unknown)
		})

		grant, err := branchroom_service.CreateRepositorySourceReadGrant(t.Context(), branchroom_service.CreateRepositorySourceReadGrantRequest{
			RepoID:          repository.ID,
			ScopeID:         scope.ID,
			Actor:           branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
			SubjectUsername: "user4",
			Surface:         branchroom_model.SurfaceSystem,
		})
		require.NoError(t, err)

		t.Run("authorized clone reads only the isolated store", func(t *testing.T) {
			cloneURL := *gatewayURL
			cloneURL.User = url.UserPassword("user4", userPassword)
			cloneDir := t.TempDir()
			require.NoError(t, git.Clone(t.Context(), cloneURL.String(), cloneDir, git.CloneRepoOptions{}))
			commit, _, err := gitcmd.NewCommand("rev-parse", "refs/remotes/origin/restricted").WithDir(cloneDir).RunStdString(t.Context())
			require.NoError(t, err)
			assert.Equal(t, restrictedCommit, strings.TrimSpace(commit))
			contents, _, err := gitcmd.NewCommand("show", "refs/remotes/origin/restricted:restricted.txt").WithDir(cloneDir).RunStdString(t.Context())
			require.NoError(t, err)
			assert.Equal(t, "restricted only\n", contents)

			publicURL := u.JoinPath("user2/repo1.git", "info/refs").String() + "?service=git-upload-pack"
			public := makeBranchroomGitHTTPRequest(t, http.MethodGet, publicURL, "", "")
			assert.Equal(t, http.StatusOK, public.StatusCode)
			assert.NotContains(t, public.Body, restrictedCommit)
		})

		t.Run("gzip clone receives all distinct refs", func(t *testing.T) {
			commits := make([]git.FastImportCommit, 150)
			for i := range commits {
				commits[i] = git.FastImportCommit{Ref: fmt.Sprintf("refs/heads/gzip-%03d", i), Message: fmt.Sprintf("gzip commit %d", i)}
			}
			require.NoError(t, git.ForceFastImport(t.Context(), restricted, commits))
			cloneURL := *gatewayURL
			cloneURL.User = url.UserPassword("user4", userPassword)
			cloneDir := t.TempDir()
			tracePath := filepath.Join(t.TempDir(), "curl.trace")
			_, _, err := gitcmd.NewCommand("-c", "protocol.version=0", "clone").AddDynamicArguments(cloneURL.String(), cloneDir).WithEnv(append(os.Environ(), "GIT_TRACE_CURL="+tracePath, "GIT_TRACE_CURL_NO_DATA=1")).RunStdString(t.Context())
			require.NoError(t, err)
			trace, readErr := os.ReadFile(tracePath)
			require.NoError(t, readErr)
			assert.Contains(t, strings.ToLower(string(trace)), "content-encoding: gzip")
			refs, _, err := gitcmd.NewCommand("for-each-ref", "--format=%(objectname) %(refname)", "refs/remotes/origin/gzip-*").WithDir(cloneDir).RunStdString(t.Context())
			require.NoError(t, err)
			assert.Len(t, strings.Fields(refs), 300)
			expected, _, err := gitcmd.NewCommand("for-each-ref", "--format=%(objectname) %(refname)", "refs/heads/gzip-*").WithRepo(restricted).RunStdString(t.Context())
			require.NoError(t, err)
			assert.Equal(t, strings.ReplaceAll(expected, " refs/heads/", " refs/remotes/origin/"), refs)
		})

		t.Run("mandatory password changes deny source access", func(t *testing.T) {
			require.NoError(t, user_model.UpdateUserCols(t.Context(), &user_model.User{ID: 4, MustChangePassword: true}, "must_change_password"))
			response := makeBranchroomGitHTTPRequest(t, http.MethodGet, infoRefsURL, "user4", userPassword)
			assert.Equal(t, http.StatusNotFound, response.StatusCode)
			assert.Equal(t, "Repository not found\n", response.Body)
			require.NoError(t, user_model.UpdateUserCols(t.Context(), &user_model.User{ID: 4, MustChangePassword: false}, "must_change_password"))
		})

		t.Run("token scope and feature gates fail closed", func(t *testing.T) {
			session := loginUser(t, "user4")
			badToken := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeReadNotification)
			deniedToken := makeBranchroomGitHTTPRequest(t, http.MethodGet, infoRefsURL, badToken, "x-oauth-basic")
			assert.Equal(t, http.StatusNotFound, deniedToken.StatusCode)
			publicOnlyToken := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopePublicOnly, auth_model.AccessTokenScopeReadRepository)
			deniedPublicOnlyToken := makeBranchroomGitHTTPRequest(t, http.MethodGet, infoRefsURL, publicOnlyToken, "x-oauth-basic")
			assert.Equal(t, http.StatusNotFound, deniedPublicOnlyToken.StatusCode)

			setting.Branchroom.Enabled = false
			assert.Equal(t, http.StatusNotFound, makeBranchroomGitHTTPRequest(t, http.MethodGet, infoRefsURL, "user4", userPassword).StatusCode)
			setting.Branchroom.Enabled = true
			setting.Repository.DisableHTTPGit = true
			assert.Equal(t, http.StatusNotFound, makeBranchroomGitHTTPRequest(t, http.MethodGet, infoRefsURL, "user4", userPassword).StatusCode)
			setting.Repository.DisableHTTPGit = false
		})

		t.Run("every unsupported read transport surface is indistinguishable", func(t *testing.T) {
			decisionsBefore, err := db.GetEngine(t.Context()).Where("scope_id = ?", scope.ID).Count(new(branchroom_model.Decision))
			require.NoError(t, err)
			auditsBefore, err := db.GetEngine(t.Context()).Where("scope_id = ?", scope.ID).Count(new(branchroom_model.AuditEvent))
			require.NoError(t, err)
			baseline := makeBranchroomGitHTTPRequest(t, http.MethodGet, gatewayURL.JoinPath("HEAD").String(), "user4", userPassword)
			assert.Equal(t, http.StatusNotFound, baseline.StatusCode)
			for _, request := range []struct {
				method      string
				url         string
				contentType string
				body        string
				configure   func(*http.Request)
			}{
				{method: http.MethodGet, url: gatewayURL.JoinPath("info/refs").String() + "?service=git-upload-pack&go-get=1"},
				{method: http.MethodPost, url: gatewayURL.JoinPath("git-upload-archive").String()},
				{method: http.MethodPost, url: gatewayURL.JoinPath("git-upload-pack").String()},
				{method: http.MethodPost, url: gatewayURL.JoinPath("git-upload-pack").String(), contentType: "application/x-git-upload-pack-request", configure: func(req *http.Request) { req.Header.Set("Content-Encoding", "br") }},
				{method: http.MethodGet, url: gatewayURL.JoinPath("objects/info/packs").String()},
				{method: http.MethodGet, url: gatewayURL.JoinPath("objects", restrictedCommit[:2], restrictedCommit[2:]).String()},
				{method: http.MethodPost, url: gatewayURL.JoinPath("info/lfs/objects/batch").String()},
				{method: http.MethodOptions, url: infoRefsURL},
				{method: http.MethodGet, url: gatewayURL.JoinPath("info/refs").String() + "?go-get=1"},
			} {
				response := makeBranchroomGitHTTPRequestWithRequest(t, request.method, request.url, "user4", userPassword, request.contentType, request.body, request.configure)
				assert.Equal(t, baseline, response, request.method+" "+request.url)
			}
			decisionsAfter, err := db.GetEngine(t.Context()).Where("scope_id = ?", scope.ID).Count(new(branchroom_model.Decision))
			require.NoError(t, err)
			auditsAfter, err := db.GetEngine(t.Context()).Where("scope_id = ?", scope.ID).Count(new(branchroom_model.AuditEvent))
			require.NoError(t, err)
			assert.Equal(t, decisionsBefore, decisionsAfter)
			assert.Equal(t, auditsBefore, auditsAfter)
		})

		t.Run("oversized upload-pack requests are rejected after authorisation", func(t *testing.T) {
			url := gatewayURL.JoinPath("git-upload-pack").String()
			body := strings.Repeat("x", int(setting.Branchroom.GitRequestSizeLimit())+1)
			response := makeBranchroomGitHTTPRequestWithBody(t, http.MethodPost, url, "user4", userPassword, "application/x-git-upload-pack-request", body)
			assert.Equal(t, http.StatusRequestEntityTooLarge, response.StatusCode)
		})

		t.Run("a route key cannot be replayed for another repository", func(t *testing.T) {
			decisionsBefore, err := db.GetEngine(t.Context()).Where("scope_id = ?", scope.ID).Count(new(branchroom_model.Decision))
			require.NoError(t, err)
			wrongRepositoryURL := u.JoinPath("_branchroom", "user2", "repo2", workspace.RouteKey+".git", "info/refs").String() + "?service=git-upload-pack"
			response := makeBranchroomGitHTTPRequest(t, http.MethodGet, wrongRepositoryURL, "user4", userPassword)
			assert.Equal(t, http.StatusNotFound, response.StatusCode)
			assert.Equal(t, "Repository not found\n", response.Body)
			decisionsAfter, err := db.GetEngine(t.Context()).Where("scope_id = ?", scope.ID).Count(new(branchroom_model.Decision))
			require.NoError(t, err)
			assert.Equal(t, decisionsBefore, decisionsAfter)
		})

		t.Run("the RPC rechecks revocation after advertisement", func(t *testing.T) {
			advertisement := makeBranchroomGitHTTPRequest(t, http.MethodGet, infoRefsURL, "user4", userPassword)
			assert.Equal(t, http.StatusOK, advertisement.StatusCode)
			require.NoError(t, branchroom_service.RevokeRepositorySourceReadGrant(t.Context(), branchroom_service.RevokeRepositorySourceReadGrantRequest{
				RepoID:  repository.ID,
				ScopeID: scope.ID,
				GrantID: grant.ID,
				Actor:   branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
				Surface: branchroom_model.SurfaceSystem,
			}))
			rpc := makeBranchroomGitHTTPRequestWithBody(t, http.MethodPost, gatewayURL.JoinPath("git-upload-pack").String(), "user4", userPassword, "application/x-git-upload-pack-request", "")
			assert.Equal(t, http.StatusNotFound, rpc.StatusCode)
			assert.Equal(t, "Repository not found\n", rpc.Body)
		})
	})
}

func TestBranchroomSmartHTTPWriteGateway(t *testing.T) {
	defer test.MockVariableValue(&setting.AppURL)()
	setting.AppURL = "http://127.0.0.1:0/"

	onGiteaRun(t, func(t *testing.T, u *url.URL) {
		defer test.MockVariableValue(&setting.Branchroom)()
		defer test.MockVariableValue(&setting.CustomConf)()
		defer test.MockVariableValue(&setting.LocalURL, u.String())()
		restrictedRoot := branchroomTestRoot(t)
		setting.CustomConf = branchroomReceiveHookTestConfig(t, u, restrictedRoot)
		setting.Branchroom = setting.BranchroomConfig{
			Enabled:                true,
			RestrictedRepoRootPath: restrictedRoot,
		}

		repository, err := repo_model.GetRepositoryByID(t.Context(), 1)
		require.NoError(t, err)
		require.NoError(t, git.CreateDelegateHooks(t.Context(), repository))
		baseSHA, err := git.GetBranchCommitID(t.Context(), repository.CodeStorageRepo(), repository.DefaultBranch)
		require.NoError(t, err)
		scope, err := branchroom_service.CreateRepositoryScope(t.Context(), branchroom_service.CreateRepositoryScopeRequest{
			RepoID:  repository.ID,
			Actor:   branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
			Name:    "smart-http-write",
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

		gatewayURL := u.JoinPath("_branchroom", "user2", "repo1", workspace.RouteKey+".git")
		receiveInfoRefsURL := gatewayURL.JoinPath("info/refs").String() + "?service=git-receive-pack"
		receivePackURL := gatewayURL.JoinPath("git-receive-pack").String()
		owner := branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID}

		ordinaryWriter, err := user_model.GetUserByName(t.Context(), "user4")
		require.NoError(t, err)
		require.NoError(t, repository_service.AddOrUpdateCollaborator(t.Context(), repository, ordinaryWriter, perm.AccessModeWrite))
		ordinaryPermission, err := access_model.GetIndividualUserRepoPermission(t.Context(), repository, ordinaryWriter)
		require.NoError(t, err)
		require.True(t, ordinaryPermission.CanWrite(unit.TypeCode))

		t.Run("ordinary writers without a direct grant cannot advertise or write", func(t *testing.T) {
			assert.Equal(t, http.StatusNotFound, makeBranchroomGitHTTPRequest(t, http.MethodGet, receiveInfoRefsURL, ordinaryWriter.Name, userPassword).StatusCode)
			assert.Equal(t, http.StatusNotFound, makeBranchroomGitHTTPRequestWithBody(t, http.MethodPost, receivePackURL, ordinaryWriter.Name, userPassword, "application/x-git-receive-pack-request", "").StatusCode)
		})

		writeGrant, err := branchroom_service.CreateRepositorySourceWriteGrant(t.Context(), branchroom_service.CreateRepositorySourceWriteGrantRequest{
			RepoID:          repository.ID,
			ScopeID:         scope.ID,
			Actor:           owner,
			SubjectUsername: ordinaryWriter.Name,
			Surface:         branchroom_model.SurfaceSystem,
		})
		require.NoError(t, err)

		t.Run("a write grant alone cannot disclose private refs", func(t *testing.T) {
			assert.Equal(t, http.StatusNotFound, makeBranchroomGitHTTPRequest(t, http.MethodGet, receiveInfoRefsURL, ordinaryWriter.Name, userPassword).StatusCode)
		})

		_, err = branchroom_service.CreateRepositorySourceReadGrant(t.Context(), branchroom_service.CreateRepositorySourceReadGrantRequest{
			RepoID:          repository.ID,
			ScopeID:         scope.ID,
			Actor:           owner,
			SubjectUsername: ordinaryWriter.Name,
			Surface:         branchroom_model.SurfaceSystem,
		})
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, makeBranchroomGitHTTPRequest(t, http.MethodGet, receiveInfoRefsURL, ordinaryWriter.Name, userPassword).StatusCode)

		writerURL := *gatewayURL
		writerURL.User = url.UserPassword(ordinaryWriter.Name, userPassword)
		cloneDir := t.TempDir()
		require.NoError(t, git.Clone(t.Context(), writerURL.String(), cloneDir, git.CloneRepoOptions{}))
		firstCommit := commitBranchroomFakeEnvironment(t, cloneDir, "BRANCHROOM_FAKE_TOKEN=not-a-real-token\n", "add fake environment")
		const restrictedRef = "refs/heads/branchroom/fake-environment"
		publicURL := *u.JoinPath("user2", "repo1.git")
		publicURL.User = url.UserPassword(ordinaryWriter.Name, userPassword)
		_, stderr, err := gitcmd.NewCommand("push").AddDynamicArguments(publicURL.String(), "HEAD:"+restrictedRef).WithDir(cloneDir).RunStdString(t.Context())
		require.Error(t, err)
		assert.Contains(t, stderr, "Use the restricted clone URL")
		response := MakeRequest(t, NewRequestWithJSON(t, "POST", "/api/v1/repos/user2/repo1/branches", map[string]string{"new_branch_name": "branchroom/api", "old_branch_name": repository.DefaultBranch}).AddBasicAuth(ordinaryWriter.Name), http.StatusForbidden)
		assert.Contains(t, response.Body.String(), "Use the restricted clone URL")
		response = MakeRequest(t, NewRequestWithJSON(t, "POST", "/api/v1/repos/user2/repo1/contents/reserved.txt", map[string]string{"branch": repository.DefaultBranch, "new_branch": "branchroom/api-file", "content": "dGVzdA=="}).AddBasicAuth(ordinaryWriter.Name), http.StatusForbidden)
		assert.Contains(t, response.Body.String(), "Use the restricted clone URL")

		t.Run("a direct writer cannot push outside the branchroom namespace", func(t *testing.T) {
			_, _, pushErr := gitcmd.NewCommand("push").AddDynamicArguments("origin", "HEAD:refs/heads/"+repository.DefaultBranch).WithDir(cloneDir).RunStdString(t.Context())
			require.Error(t, pushErr)
			assert.Equal(t, baseSHA, branchroomObjectID(t, restricted, "refs/heads/"+repository.DefaultBranch))
			events, err := branchroom_model.FindScopeAuditEvents(t.Context(), scope.ID)
			require.NoError(t, err)
			assert.Equal(t, "receive_rejected", events[len(events)-1].ReasonCode)
			assert.Contains(t, events[len(events)-1].Details, "refs/heads/"+repository.DefaultBranch)
		})
		_, _, err = gitcmd.NewCommand("push").AddDynamicArguments("origin", "HEAD:"+restrictedRef).WithDir(cloneDir).RunStdString(t.Context())
		require.NoError(t, err)
		secondAllowedCommit := commitBranchroomFakeEnvironment(t, cloneDir, "BRANCHROOM_FAKE_TOKEN=still-not-real\n", "update fake environment")
		_, _, err = gitcmd.NewCommand("push").AddDynamicArguments("origin", "HEAD:"+restrictedRef).WithDir(cloneDir).RunStdString(t.Context())
		require.NoError(t, err)

		t.Run("a direct writer pushes only to the isolated branchroom namespace", func(t *testing.T) {
			events, err := branchroom_model.FindScopeAuditEvents(t.Context(), scope.ID)
			require.NoError(t, err)
			event := events[len(events)-1]
			assert.Equal(t, "receive_accepted", event.ReasonCode)
			assert.Contains(t, event.Details, restrictedRef)
			assert.Contains(t, event.Details, firstCommit)
			assert.Contains(t, event.Details, secondAllowedCommit)
			require.NoError(t, branchroom_model.VerifyScopeAuditChain(t.Context(), scope))

			assert.Equal(t, secondAllowedCommit, branchroomObjectID(t, restricted, restrictedRef))
			contents, _, err := gitcmd.NewCommand("show").AddDynamicArguments(restrictedRef + ":.env").WithRepo(restricted).RunStdString(t.Context())
			require.NoError(t, err)
			assert.Equal(t, "BRANCHROOM_FAKE_TOKEN=still-not-real\n", contents)
			restrictedTree := branchroomObjectID(t, restricted, restrictedRef+"^{tree}")
			restrictedBlob := branchroomObjectID(t, restricted, restrictedRef+":.env")

			publicRepo := repository.CodeStorageRepo()
			_, _, err = gitcmd.NewCommand("show-ref", "--verify", "--quiet").AddDynamicArguments(restrictedRef).WithRepo(publicRepo).RunStdString(t.Context())
			assert.Error(t, err)
			assertGitObjectsMissing(t, publicRepo, firstCommit, secondAllowedCommit, restrictedTree, restrictedBlob)

			publicGitURL := u.JoinPath("user2", "repo1.git")
			publicInfoRefsURL := publicGitURL.JoinPath("info/refs").String() + "?service=git-upload-pack"
			publicAdvertisement := makeBranchroomGitHTTPRequest(t, http.MethodGet, publicInfoRefsURL, "", "")
			assert.Equal(t, http.StatusOK, publicAdvertisement.StatusCode)
			assert.NotContains(t, publicAdvertisement.Body, firstCommit)
			assert.NotContains(t, publicAdvertisement.Body, secondAllowedCommit)

			publicClone := t.TempDir()
			require.NoError(t, git.InitRepositoryLocal(t.Context(), publicClone, false, git.Sha1ObjectFormat.Name(), ""))
			_, stderr, err := gitcmd.NewCommand("fetch").AddDynamicArguments(publicGitURL.String(), "refs/heads/"+repository.DefaultBranch).WithDir(publicClone).RunStdString(t.Context())
			require.NoError(t, err, "%s", stderr)
			assert.Equal(t, baseSHA, branchroomLocalRevision(t, publicClone, "FETCH_HEAD"))
			_, _, err = gitcmd.NewCommand("fetch").AddDynamicArguments(publicGitURL.String(), secondAllowedCommit).WithDir(publicClone).RunStdString(t.Context())
			assert.Error(t, err, "public Git endpoint served restricted commit %s", secondAllowedCommit)

			publicObjectURL := publicGitURL.JoinPath("objects", restrictedBlob[:2], restrictedBlob[2:])
			publicObject := makeBranchroomGitHTTPRequest(t, http.MethodGet, publicObjectURL.String(), "", "")
			assert.Equal(t, http.StatusNotFound, publicObject.StatusCode)
		})

		t.Run("ordinary pushes cannot publish restricted commits through other refs or a fork", func(t *testing.T) {
			fork, err := repository_service.ForkRepository(t.Context(), ordinaryWriter, ordinaryWriter, repository_service.ForkRepoOptions{
				BaseRepo: repository,
				Name:     "restricted-publish-fork",
			})
			require.NoError(t, err)
			forkURL := u.JoinPath(ordinaryWriter.Name, fork.Name+".git")
			forkURL.User = publicURL.User
			for _, push := range []struct {
				remote *url.URL
				ref    string
			}{
				{&publicURL, "refs/tags/restricted-tag"},
				{&publicURL, "refs/for/" + repository.DefaultBranch + "/restricted-review"},
				{forkURL, "refs/heads/restricted-fork-branch"},
			} {
				t.Run(push.ref, func(t *testing.T) {
					_, stderr, err := gitcmd.NewCommand("push").AddDynamicArguments(push.remote.String(), "HEAD:"+push.ref).WithDir(cloneDir).RunStdString(t.Context())
					require.Error(t, err)
					assert.Contains(t, stderr, "restricted workspace")
				})
			}
		})

		t.Run("ordinary write cannot publish restricted commits, an admin can", func(t *testing.T) {
			defer test.MockVariableValue(&setting.Audit.RecordOutput, setting.AuditRecordOutputDatabase)()
			_, stderr, pushErr := gitcmd.NewCommand("push").AddDynamicArguments(publicURL.String(), "HEAD:refs/heads/fix-public").WithDir(cloneDir).RunStdString(t.Context())
			require.Error(t, pushErr)
			assert.Contains(t, stderr, "restricted workspace")
			_, _, refErr := gitcmd.NewCommand("show-ref", "--verify", "--quiet", "refs/heads/fix-public").WithRepo(repository.CodeStorageRepo()).RunStdString(t.Context())
			assert.Error(t, refErr)
			events, err := branchroom_model.FindScopeAuditEvents(t.Context(), scope.ID)
			require.NoError(t, err)
			assert.Equal(t, "restricted_commit_public_push_refused", events[len(events)-1].ReasonCode)
			unittest.AssertExistsAndLoadBean(t, &audit_model.Event{Action: audit_model.RepositoryRestrictedRefused, ActorID: ordinaryWriter.ID, ScopeID: repository.ID})
			unittest.AssertExistsAndLoadBean(t, &audit_model.Event{Action: audit_model.RepositoryGitWrite, ActorID: ordinaryWriter.ID, ScopeID: repository.ID})

			adminURL := *u.JoinPath("user2", "repo1.git")
			adminURL.User = url.UserPassword("user2", userPassword)
			_, _, pushErr = gitcmd.NewCommand("push").AddDynamicArguments(adminURL.String(), "HEAD:refs/heads/published-fix").WithDir(cloneDir).RunStdString(t.Context())
			require.NoError(t, pushErr)
			assert.Equal(t, secondAllowedCommit, branchroomObjectID(t, repository.CodeStorageRepo(), "refs/heads/published-fix"))
			events, err = branchroom_model.FindScopeAuditEvents(t.Context(), scope.ID)
			require.NoError(t, err)
			assert.Equal(t, "restricted_commit_published_by_admin", events[len(events)-1].ReasonCode)
			require.NoError(t, branchroom_model.VerifyScopeAuditChain(t.Context(), scope))
			unittest.AssertExistsAndLoadBean(t, &audit_model.Event{Action: audit_model.RepositoryGitPush, ActorID: repository.OwnerID, ScopeID: repository.ID})
		})

		t.Run("an unsafe restricted root hides private refs", func(t *testing.T) {
			require.NoError(t, os.Chmod(restrictedRoot, 0o755))
			t.Cleanup(func() {
				require.NoError(t, os.Chmod(restrictedRoot, 0o700))
			})

			response := makeBranchroomGitHTTPRequest(t, http.MethodGet, receiveInfoRefsURL, ordinaryWriter.Name, userPassword)
			assert.Equal(t, http.StatusNotFound, response.StatusCode)
			assert.NotContains(t, response.Body, restrictedRef)
			assert.NotContains(t, response.Body, secondAllowedCommit)
		})

		require.NoError(t, branchroom_service.RevokeRepositorySourceWriteGrant(t.Context(), branchroom_service.RevokeRepositorySourceWriteGrantRequest{
			RepoID:  repository.ID,
			ScopeID: scope.ID,
			GrantID: writeGrant.ID,
			Actor:   owner,
			Surface: branchroom_model.SurfaceSystem,
		}))

		t.Run("revoking write access blocks the next push", func(t *testing.T) {
			assert.Equal(t, http.StatusNotFound, makeBranchroomGitHTTPRequest(t, http.MethodGet, receiveInfoRefsURL, ordinaryWriter.Name, userPassword).StatusCode)
			thirdCommit := commitBranchroomFakeEnvironment(t, cloneDir, "BRANCHROOM_FAKE_TOKEN=revoked-access\n", "change fake environment after revocation")
			assert.NotEqual(t, secondAllowedCommit, thirdCommit)
			_, _, err := gitcmd.NewCommand("push").AddDynamicArguments("origin", "HEAD:"+restrictedRef).WithDir(cloneDir).RunStdString(t.Context())
			assert.Error(t, err)
			assert.Equal(t, secondAllowedCommit, branchroomObjectID(t, restricted, restrictedRef))
		})
	})
}

func commitBranchroomFakeEnvironment(t *testing.T, repositoryPath, contents, message string) string {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(repositoryPath, ".env"), []byte(contents), 0o600))
	require.NoError(t, gitAddChangesDeprecated(t.Context(), repositoryPath, false, ".env"))
	signature := &git.Signature{Email: "user4@example.com", Name: "User Four", When: time.Now()}
	require.NoError(t, gitCommitChangesDeprecated(t.Context(), repositoryPath, gitCommitChangesOptions{
		Committer: signature,
		Author:    signature,
		Message:   message,
	}))
	commit, _, err := gitcmd.NewCommand("rev-parse", "HEAD").WithDir(repositoryPath).RunStdString(t.Context())
	require.NoError(t, err)
	return strings.TrimSpace(commit)
}

func branchroomReceiveHookTestConfig(t *testing.T, serverURL *url.URL, restrictedRoot string) string {
	t.Helper()

	config, err := os.ReadFile(setting.CustomConf)
	require.NoError(t, err)
	lines := strings.Split(string(config), "\n")
	for index, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "LOCAL_ROOT_URL") {
			lines[index] = "LOCAL_ROOT_URL   = " + strings.TrimSuffix(serverURL.String(), "/") + "/"
			lines = append(lines,
				"",
				"[branchroom]",
				"ENABLED = true",
				"RESTRICTED_ROOT = "+restrictedRoot,
				"",
				"[security]",
				"INTERNAL_TOKEN = "+setting.InternalToken,
			)
			path := filepath.Join(t.TempDir(), "branchroom-hook.ini")
			require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600))
			return path
		}
	}
	t.Fatal("integration configuration has no LOCAL_ROOT_URL")
	return ""
}

type branchroomGitHTTPResponse struct {
	StatusCode      int
	Body            string
	CacheControl    string
	Vary            string
	WWWAuthenticate string
}

func makeBranchroomGitHTTPRequest(t *testing.T, method, requestURL, username, password string) branchroomGitHTTPResponse {
	t.Helper()
	return makeBranchroomGitHTTPRequestWithBody(t, method, requestURL, username, password, "", "")
}

func makeBranchroomGitHTTPRequestWithBody(t *testing.T, method, requestURL, username, password, contentType, body string) branchroomGitHTTPResponse {
	t.Helper()
	return makeBranchroomGitHTTPRequestWithRequest(t, method, requestURL, username, password, contentType, body, nil)
}

func makeBranchroomGitHTTPRequestWithRequest(t *testing.T, method, requestURL, username, password, contentType, body string, configure func(*http.Request)) branchroomGitHTTPResponse {
	t.Helper()
	req, err := http.NewRequest(method, requestURL, strings.NewReader(body))
	require.NoError(t, err)
	if username != "" {
		req.SetBasicAuth(username, password)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if configure != nil {
		configure(req)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return branchroomGitHTTPResponse{
		StatusCode:      resp.StatusCode,
		Body:            string(responseBody),
		CacheControl:    resp.Header.Get("Cache-Control"),
		Vary:            resp.Header.Get("Vary"),
		WWWAuthenticate: resp.Header.Get("WWW-Authenticate"),
	}
}

func branchroomObjectID(t *testing.T, repo gitrepo.RepositoryFacade, revision string) string {
	t.Helper()

	objectID, _, err := gitcmd.NewCommand("rev-parse").AddDynamicArguments(revision).WithRepo(repo).RunStdString(t.Context())
	require.NoError(t, err)
	return strings.TrimSpace(objectID)
}
