// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"time"

	auth_model "gitea.dev/models/auth"
	branchroom_model "gitea.dev/models/branchroom"
	repo_model "gitea.dev/models/repo"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/git/gitrepo"
	"gitea.dev/modules/log"
	repo_module "gitea.dev/modules/repository"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/util"
	"gitea.dev/modules/web/types"
	"gitea.dev/services/auth"
	branchroom_service "gitea.dev/services/branchroom"
	"gitea.dev/services/context"
)

const (
	branchroomGitHTTPUploadTimeout = 10 * time.Minute
)

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

// BranchroomGitHTTPInfoRefs serves authenticated smart Git advertisements.
func BranchroomGitHTTPInfoRefs(ctx *context.Context) {
	service, ok := branchroomGitHTTPInfoRefsService(ctx.Req)
	if !ok {
		branchroomGitHTTPNotFound(ctx)
		return
	}

	var restricted gitrepo.RepositoryFacade
	if service == ServiceTypeUploadPack {
		restricted, ok = authorizeBranchroomGitHTTPRead(ctx)
	} else {
		restricted, ok = authorizeBranchroomGitHTTPWrite(ctx)
	}
	if !ok {
		return
	}

	refs, _, err := gitcmd.NewCommand().
		AddDynamicArguments(service).
		AddArguments("--stateless-rpc", "--advertise-refs", ".").
		WithEnv(branchroomGitHTTPEnvironment(ctx)).
		WithRepo(restricted).
		RunStdBytes(ctx)
	if err != nil {
		log.Error("Branchroom Git HTTP advertisement failed")
		branchroomGitHTTPServerError(ctx)
		return
	}

	ctx.RespHeader().Set("Content-Type", "application/x-git-"+service+"-advertisement")
	ctx.Status(http.StatusOK)
	_, _ = ctx.Write(packetWrite("# service=git-" + service + "\n"))
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
	rpcDeadline := time.Now().Add(setting.Branchroom.GitRequestTimeLimit())
	restricted, ok := authorizeBranchroomGitHTTPRead(ctx)
	if !ok {
		return
	}

	body, ok := readBranchroomGitHTTPRPCBody(ctx.Resp, ctx.Req, rpcDeadline)
	if !ok {
		branchroomGitHTTPBodyRejected(ctx, body)
		return
	}
	defer clear(body)
	rpcTimeout := time.Until(rpcDeadline)
	if rpcTimeout <= 0 {
		branchroomGitHTTPNotFound(ctx)
		return
	}

	ctx.RespHeader().Set("Content-Type", "application/x-git-upload-pack-result")
	err := gitcmd.NewCommand(ServiceTypeUploadPack).
		AddArguments("--stateless-rpc", ".").
		WithEnv(branchroomGitHTTPEnvironment(ctx)).
		WithRepo(restricted).
		WithStdinBytes(body).
		WithStdoutCopy(ctx.Resp).
		WithTimeout(branchroomGitHTTPUploadTimeout).
		RunWithStderr(ctx)
	if err != nil && !gitcmd.IsErrorCanceledOrKilled(err) {
		log.Error("Branchroom Git HTTP upload-pack RPC failed")
		if !ctx.Written() {
			branchroomGitHTTPServerError(ctx)
		}
	}
}

// BranchroomGitHTTPReceivePack serves only authenticated, one-use smart
// receive-pack RPCs. The hook admission bearer is generated after authorization
// and is inherited by the Git child only; it is never returned to the client.
func BranchroomGitHTTPReceivePack(ctx *context.Context) {
	defer ctx.Req.Body.Close()

	if !isBranchroomGitHTTPReceivePackRequest(ctx.Req) {
		branchroomGitHTTPNotFound(ctx)
		return
	}
	rpcDeadline := time.Now().Add(setting.Branchroom.GitRequestTimeLimit())
	restricted, authorization, ok := authorizeBranchroomGitHTTPWriteWithIdentity(ctx)
	if !ok {
		return
	}
	body, ok := readBranchroomGitHTTPRPCBody(ctx.Resp, ctx.Req, rpcDeadline)
	if !ok {
		branchroomGitHTTPBodyRejected(ctx, body)
		return
	}
	defer clear(body)

	credential, err := branchroom_service.IssueReceiveAdmission(ctx, branchroom_service.IssueReceiveAdmissionRequest{
		RepoID:   authorization.repoID,
		RouteKey: authorization.routeKey,
		UserID:   authorization.userID,
	})
	if err != nil {
		branchroomGitHTTPNotFound(ctx)
		return
	}
	defer credential.Clear()
	defer branchroom_service.FinalizeReceiveAdmission(ctx, credential)

	environ := branchroomGitHTTPEnvironment(ctx)
	environ = append(environ,
		repo_module.EnvBranchroomReceiveSessionKey+"="+credential.SessionKey,
		repo_module.EnvBranchroomReceiveBearerSecret+"="+string(credential.BearerSecret),
	)
	defer clear(environ)
	rpcTimeout := time.Until(rpcDeadline)
	if rpcTimeout <= 0 {
		branchroomGitHTTPNotFound(ctx)
		return
	}
	ctx.RespHeader().Set("Content-Type", "application/x-git-receive-pack-result")
	err = gitcmd.NewCommand("-c", "receive.fsckObjects=true", ServiceTypeReceivePack).
		AddArguments("--stateless-rpc", ".").
		WithEnv(environ).
		WithRepo(restricted).
		WithStdinBytes(body).
		WithStdoutCopy(ctx.Resp).
		WithTimeout(rpcTimeout).
		RunWithStderr(ctx)
	if err != nil && !gitcmd.IsErrorCanceledOrKilled(err) {
		log.Error("Branchroom Git HTTP receive-pack RPC failed")
		if !ctx.Written() {
			branchroomGitHTTPNotFound(ctx)
		}
	}
}

func branchroomGitHTTPInfoRefsService(req *http.Request) (string, bool) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return "", false
	}
	query := req.URL.Query()
	if len(query) != 1 || len(query["service"]) != 1 {
		return "", false
	}
	switch query.Get("service") {
	case "git-upload-pack":
		return ServiceTypeUploadPack, true
	case "git-receive-pack":
		return ServiceTypeReceivePack, true
	default:
		return "", false
	}
}

func isBranchroomGitHTTPUploadPackRequest(req *http.Request) bool {
	return req.Method == http.MethodPost &&
		len(req.Header.Values("Content-Type")) == 1 && req.Header.Get("Content-Type") == "application/x-git-upload-pack-request" &&
		(len(req.Header.Values("Content-Encoding")) == 0 || len(req.Header.Values("Content-Encoding")) == 1 && req.Header.Get("Content-Encoding") == "gzip") &&
		req.ContentLength <= setting.Branchroom.GitRequestSizeLimit()
}

func isBranchroomGitHTTPReceivePackRequest(req *http.Request) bool {
	return req.Method == http.MethodPost &&
		len(req.Header.Values("Content-Type")) == 1 && req.Header.Get("Content-Type") == "application/x-git-receive-pack-request" &&
		(len(req.Header.Values("Content-Encoding")) == 0 || len(req.Header.Values("Content-Encoding")) == 1 && req.Header.Get("Content-Encoding") == "gzip") &&
		req.ContentLength <= setting.Branchroom.GitRequestSizeLimit()
}

// readBranchroomGitHTTPBody consumes an RPC request before a Git child starts so
// command cancellation never depends on a client-controlled streaming reader.
func readBranchroomGitHTTPBody(body io.Reader) ([]byte, bool) {
	limit := setting.Branchroom.GitRequestSizeLimit()
	contents, err := io.ReadAll(io.LimitReader(body, limit+1))
	return contents, err == nil && int64(len(contents)) <= limit
}

func readBranchroomGitHTTPRPCBody(resp http.ResponseWriter, req *http.Request, deadline time.Time) (contents []byte, ok bool) {
	controller := http.NewResponseController(resp)
	if err := controller.SetReadDeadline(deadline); err != nil {
		return nil, false
	}
	defer func() {
		if !ok {
			_ = controller.SetReadDeadline(time.Now())
			_ = req.Body.Close()
		}
		_ = controller.SetReadDeadline(time.Time{})
	}()
	if !deadline.After(time.Now()) {
		return nil, false
	}
	var body io.Reader = req.Body
	if req.Header.Get("Content-Encoding") == "gzip" {
		// chunked bodies carry no length, so bound the compressed side too
		decoded, err := gzip.NewReader(io.LimitReader(body, setting.Branchroom.GitRequestSizeLimit()+1))
		if err != nil {
			return nil, false
		}
		defer decoded.Close()
		body = decoded
	}
	return readBranchroomGitHTTPBody(body)
}

func branchroomGitHTTPEnvironment(ctx *context.Context) []string {
	// Do not inherit the web process environment: ambient GIT_* values can alter
	// receive behavior and unrelated service variables do not belong in a child.
	environ := gitcmd.CommonGitCmdEnvs()
	if protocol := ctx.Req.Header.Get("Git-Protocol"); protocol != "" && safeGitProtocolHeader.MatchString(protocol) {
		environ = append(environ, "GIT_PROTOCOL="+protocol)
	}
	return environ
}

// BranchroomGitHTTPNotFound rejects every transport surface outside the narrow
// smart upload-pack and receive-pack gateways.
func BranchroomGitHTTPNotFound(ctx *context.Context) {
	branchroomGitHTTPNotFound(ctx)
}

func authorizeBranchroomGitHTTPRead(ctx *context.Context) (gitrepo.RepositoryFacade, bool) {
	authorization, ok := authorizeBranchroomGitHTTPIdentity(ctx, branchroomGitHTTPTokenCanRead)
	if !ok {
		return nil, false
	}

	restricted, err := branchroom_service.AuthorizeWorkspaceGitHTTPRead(ctx, branchroom_service.AuthorizeWorkspaceGitHTTPReadRequest{
		RepoID:   authorization.repoID,
		RouteKey: authorization.routeKey,
		UserID:   authorization.userID,
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

func authorizeBranchroomGitHTTPWrite(ctx *context.Context) (gitrepo.RepositoryFacade, bool) {
	restricted, _, ok := authorizeBranchroomGitHTTPWriteWithIdentity(ctx)
	return restricted, ok
}

func authorizeBranchroomGitHTTPWriteWithIdentity(ctx *context.Context) (gitrepo.RepositoryFacade, branchroomGitHTTPAuthorization, bool) {
	authorization, ok := authorizeBranchroomGitHTTPIdentity(ctx, branchroomGitHTTPTokenCanWrite)
	if !ok {
		return nil, branchroomGitHTTPAuthorization{}, false
	}

	restricted, err := branchroom_service.AuthorizeWorkspaceGitHTTPWrite(ctx, branchroom_service.AuthorizeWorkspaceGitHTTPWriteRequest{
		RepoID:   authorization.repoID,
		RouteKey: authorization.routeKey,
		UserID:   authorization.userID,
	})
	if err != nil {
		if !errors.Is(err, util.ErrNotExist) {
			log.Error("Branchroom Git HTTP workspace write authorization failed")
			branchroomGitHTTPServerError(ctx)
			return nil, branchroomGitHTTPAuthorization{}, false
		}
		branchroomGitHTTPNotFound(ctx)
		return nil, branchroomGitHTTPAuthorization{}, false
	}
	return restricted, authorization, true
}

type branchroomGitHTTPAuthorization struct {
	repoID   int64
	routeKey string
	userID   int64
}

func authorizeBranchroomGitHTTPIdentity(ctx *context.Context, tokenAuthorized func(*context.Context) bool) (branchroomGitHTTPAuthorization, bool) {
	if !setting.Branchroom.Enabled || setting.Repository.DisableHTTPGit {
		branchroomGitHTTPNotFound(ctx)
		return branchroomGitHTTPAuthorization{}, false
	}
	if !ctx.IsBasicAuth || !ctx.IsSigned || ctx.Doer == nil {
		branchroomGitHTTPUnauthorized(ctx)
		return branchroomGitHTTPAuthorization{}, false
	}
	switch ctx.Data["LoginMethod"] {
	case auth.BasicMethodName, auth.AccessTokenMethodName:
	default:
		branchroomGitHTTPNotFound(ctx)
		return branchroomGitHTTPAuthorization{}, false
	}
	if !isEligibleBranchroomDoer(ctx) {
		branchroomGitHTTPNotFound(ctx)
		return branchroomGitHTTPAuthorization{}, false
	}
	if ctx.Data["ApiTokenScope"] == nil {
		_, err := auth_model.GetTwoFactorByUID(ctx, ctx.Doer.ID)
		if err == nil {
			branchroomGitHTTPUnauthorized(ctx)
			return branchroomGitHTTPAuthorization{}, false
		}
		if !auth_model.IsErrTwoFactorNotEnrolled(err) {
			log.Error("Branchroom Git HTTP authentication verification failed")
			branchroomGitHTTPServerError(ctx)
			return branchroomGitHTTPAuthorization{}, false
		}
	}

	routeKey := ctx.PathParam("routeKey")
	if !branchroom_model.IsWorkspaceRouteKey(routeKey) {
		branchroomGitHTTPNotFound(ctx)
		return branchroomGitHTTPAuthorization{}, false
	}
	owner, err := user_model.GetUserByName(ctx, ctx.PathParam("username"))
	if err != nil {
		if !user_model.IsErrUserNotExist(err) {
			log.Error("Branchroom Git HTTP owner lookup failed")
			branchroomGitHTTPServerError(ctx)
			return branchroomGitHTTPAuthorization{}, false
		}
		branchroomGitHTTPNotFound(ctx)
		return branchroomGitHTTPAuthorization{}, false
	}
	if !owner.IsOrganization() && !owner.IsActive {
		branchroomGitHTTPNotFound(ctx)
		return branchroomGitHTTPAuthorization{}, false
	}
	repository, err := repo_model.GetRepositoryByName(ctx, owner.ID, ctx.PathParam("reponame"))
	if err != nil {
		if !repo_model.IsErrRepoNotExist(err) {
			log.Error("Branchroom Git HTTP repository lookup failed")
			branchroomGitHTTPServerError(ctx)
			return branchroomGitHTTPAuthorization{}, false
		}
		branchroomGitHTTPNotFound(ctx)
		return branchroomGitHTTPAuthorization{}, false
	}
	if !tokenAuthorized(ctx) {
		branchroomGitHTTPNotFound(ctx)
		return branchroomGitHTTPAuthorization{}, false
	}
	return branchroomGitHTTPAuthorization{repoID: repository.ID, routeKey: routeKey, userID: ctx.Doer.ID}, true
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

func branchroomGitHTTPTokenCanWrite(ctx *context.Context) bool {
	scope, hasAPITokenScope := ctx.Data["ApiTokenScope"].(auth_model.AccessTokenScope)
	if !hasAPITokenScope {
		return true
	}
	publicOnly, err := scope.PublicOnly()
	if err != nil || publicOnly {
		return false
	}
	hasScope, err := scope.HasAnyScope(auth_model.GetRequiredScopes(auth_model.Write, auth_model.AccessTokenScopeCategoryRepository)...)
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

// branchroomGitHTTPBodyRejected runs only after authorization, so saying a body was too
// large tells an authorized client nothing about the workspace it did not already know
func branchroomGitHTTPBodyRejected(ctx *context.Context, body []byte) {
	tooLarge := int64(len(body)) > setting.Branchroom.GitRequestSizeLimit()
	clear(body)
	if tooLarge {
		ctx.PlainText(http.StatusRequestEntityTooLarge, "Request exceeds the restricted gateway size limit\n")
		return
	}
	branchroomGitHTTPNotFound(ctx)
}

func branchroomGitHTTPServerError(ctx *context.Context) {
	ctx.PlainText(http.StatusInternalServerError, "Internal Server Error\n")
}
