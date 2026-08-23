// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"os"

	auth_model "gitea.dev/models/auth"
	branchroom_model "gitea.dev/models/branchroom"
	repo_model "gitea.dev/models/repo"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/git/gitrepo"
	"gitea.dev/modules/log"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/util"
	"gitea.dev/modules/web/types"
	branchroom_service "gitea.dev/services/branchroom"
	"gitea.dev/services/context"
)

const branchroomGitHTTPMaxRequestBody = 1 << 20

func setBranchroomGitHTTPResponseHeaders(header http.Header) {
	header.Set("Cache-Control", "private, no-store")
	header.Set("Vary", "Authorization")
}

// BranchroomGitHTTPHeaders prevents restricted transport responses from being shared.
var BranchroomGitHTTPHeaders types.PreMiddlewareProvider = func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		next.ServeHTTP(newBranchroomPrivacyResponseWriter(resp, setBranchroomGitHTTPResponseHeaders), req)
	})
}

// BranchroomGitHTTPInfoRefs serves only smart upload-pack advertisements.
func BranchroomGitHTTPInfoRefs(ctx *context.Context) {
	if !isBranchroomGitHTTPInfoRefsRequest(ctx.Req) {
		branchroomGitHTTPNotFound(ctx)
		return
	}
	restricted, ok := authorizeBranchroomGitHTTPRead(ctx)
	if !ok {
		return
	}

	environ := os.Environ()
	if protocol := ctx.Req.Header.Get("Git-Protocol"); protocol != "" && safeGitProtocolHeader.MatchString(protocol) {
		environ = append(environ, "GIT_PROTOCOL="+protocol)
	}
	refs, _, err := gitcmd.NewCommand(ServiceTypeUploadPack).
		AddArguments("--stateless-rpc", "--advertise-refs", ".").
		WithEnv(environ).
		WithRepo(restricted).
		RunStdBytes(ctx)
	if err != nil {
		log.Error("Branchroom Git HTTP upload-pack advertisement failed")
		branchroomGitHTTPServerError(ctx)
		return
	}

	ctx.RespHeader().Set("Content-Type", "application/x-git-upload-pack-advertisement")
	ctx.Status(http.StatusOK)
	_, _ = ctx.Write(packetWrite("# service=git-upload-pack\n"))
	_, _ = ctx.Write([]byte("0000"))
	_, _ = ctx.Write(refs)
}

// BranchroomGitHTTPUploadPack serves only authenticated smart upload-pack RPCs.
func BranchroomGitHTTPUploadPack(ctx *context.Context) {
	defer ctx.Req.Body.Close()

	if !isBranchroomGitHTTPUploadPackRequest(ctx.Req) {
		branchroomGitHTTPNotFound(ctx)
		return
	}
	restricted, ok := authorizeBranchroomGitHTTPRead(ctx)
	if !ok {
		return
	}

	body, err := io.ReadAll(io.LimitReader(ctx.Req.Body, branchroomGitHTTPMaxRequestBody+1))
	if err != nil || len(body) > branchroomGitHTTPMaxRequestBody {
		branchroomGitHTTPNotFound(ctx)
		return
	}

	environ := os.Environ()
	if protocol := ctx.Req.Header.Get("Git-Protocol"); protocol != "" && safeGitProtocolHeader.MatchString(protocol) {
		environ = append(environ, "GIT_PROTOCOL="+protocol)
	}
	ctx.RespHeader().Set("Content-Type", "application/x-git-upload-pack-result")
	err = gitcmd.NewCommand(ServiceTypeUploadPack).
		AddArguments("--stateless-rpc", ".").
		WithEnv(environ).
		WithRepo(restricted).
		WithStdinCopy(bytes.NewReader(body)).
		WithStdoutCopy(ctx.Resp).
		RunWithStderr(ctx)
	if err != nil && !gitcmd.IsErrorCanceledOrKilled(err) {
		log.Error("Branchroom Git HTTP upload-pack RPC failed")
		if !ctx.Written() {
			branchroomGitHTTPServerError(ctx)
		}
	}
}

func isBranchroomGitHTTPInfoRefsRequest(req *http.Request) bool {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return false
	}
	query := req.URL.Query()
	return len(query) == 1 && len(query["service"]) == 1 && query.Get("service") == "git-upload-pack"
}

func isBranchroomGitHTTPUploadPackRequest(req *http.Request) bool {
	return req.Method == http.MethodPost &&
		len(req.Header.Values("Content-Type")) == 1 && req.Header.Get("Content-Type") == "application/x-git-upload-pack-request" &&
		len(req.Header.Values("Content-Encoding")) == 0 &&
		req.ContentLength <= branchroomGitHTTPMaxRequestBody
}

// BranchroomGitHTTPNotFound rejects every transport surface other than upload-pack.
func BranchroomGitHTTPNotFound(ctx *context.Context) {
	branchroomGitHTTPNotFound(ctx)
}

func authorizeBranchroomGitHTTPRead(ctx *context.Context) (gitrepo.RepositoryFacade, bool) {
	if !setting.Branchroom.Enabled || setting.Repository.DisableHTTPGit {
		branchroomGitHTTPNotFound(ctx)
		return nil, false
	}
	if !ctx.IsBasicAuth || !ctx.IsSigned || ctx.Doer == nil {
		branchroomGitHTTPUnauthorized(ctx)
		return nil, false
	}
	if !isEligibleBranchroomDoer(ctx) {
		branchroomGitHTTPNotFound(ctx)
		return nil, false
	}
	if ctx.Data["ApiTokenScope"] == nil {
		_, err := auth_model.GetTwoFactorByUID(ctx, ctx.Doer.ID)
		if err == nil {
			branchroomGitHTTPUnauthorized(ctx)
			return nil, false
		}
		if !auth_model.IsErrTwoFactorNotEnrolled(err) {
			log.Error("Branchroom Git HTTP authentication verification failed")
			branchroomGitHTTPServerError(ctx)
			return nil, false
		}
	}

	routeKey := ctx.PathParam("routeKey")
	if !branchroom_model.IsWorkspaceRouteKey(routeKey) {
		branchroomGitHTTPNotFound(ctx)
		return nil, false
	}
	owner, err := user_model.GetUserByName(ctx, ctx.PathParam("username"))
	if err != nil {
		if !user_model.IsErrUserNotExist(err) {
			log.Error("Branchroom Git HTTP owner lookup failed")
			branchroomGitHTTPServerError(ctx)
			return nil, false
		}
		branchroomGitHTTPNotFound(ctx)
		return nil, false
	}
	if !owner.IsOrganization() && !owner.IsActive {
		branchroomGitHTTPNotFound(ctx)
		return nil, false
	}
	repository, err := repo_model.GetRepositoryByName(ctx, owner.ID, ctx.PathParam("reponame"))
	if err != nil {
		if !repo_model.IsErrRepoNotExist(err) {
			log.Error("Branchroom Git HTTP repository lookup failed")
			branchroomGitHTTPServerError(ctx)
			return nil, false
		}
		branchroomGitHTTPNotFound(ctx)
		return nil, false
	}
	if !branchroomGitHTTPTokenCanRead(ctx) {
		branchroomGitHTTPNotFound(ctx)
		return nil, false
	}

	restricted, err := branchroom_service.AuthorizeWorkspaceGitHTTPRead(ctx, branchroom_service.AuthorizeWorkspaceGitHTTPReadRequest{
		RepoID:   repository.ID,
		RouteKey: routeKey,
		UserID:   ctx.Doer.ID,
	})
	if err != nil {
		if !errors.Is(err, util.ErrNotExist) {
			log.Error("Branchroom Git HTTP workspace authorization failed")
			branchroomGitHTTPServerError(ctx)
			return nil, false
		}
		branchroomGitHTTPNotFound(ctx)
		return nil, false
	}
	return restricted, true
}

func branchroomGitHTTPTokenCanRead(ctx *context.Context) bool {
	scope, hasAPITokenScope := ctx.Data["ApiTokenScope"].(auth_model.AccessTokenScope)
	if !hasAPITokenScope {
		return true
	}
	publicOnly, err := scope.PublicOnly()
	if err != nil {
		return false
	}
	if publicOnly {
		// A restricted workspace is never a public resource, even when its
		// ordinary repository is publicly readable.
		return false
	}
	hasScope, err := scope.HasAnyScope(auth_model.GetRequiredScopes(auth_model.Read, auth_model.AccessTokenScopeCategoryRepository)...)
	return err == nil && hasScope
}

func branchroomGitHTTPUnauthorized(ctx *context.Context) {
	if setting.OAuth2.Enabled {
		ctx.RespHeader().Set("WWW-Authenticate", `Basic realm="Gitea"`)
	} else {
		ctx.RespHeader().Set("WWW-Authenticate", `Basic realm="Gitea (Basic Auth)"`)
	}
	ctx.PlainText(http.StatusUnauthorized, "Unauthorized\n")
}

func branchroomGitHTTPNotFound(ctx *context.Context) {
	ctx.PlainText(http.StatusNotFound, "Repository not found\n")
}

func branchroomGitHTTPServerError(ctx *context.Context) {
	ctx.PlainText(http.StatusInternalServerError, "Internal Server Error\n")
}
