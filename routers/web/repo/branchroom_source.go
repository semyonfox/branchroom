// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"errors"
	"net/http"
	"net/url"

	"gitea.dev/modules/httplib"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/templates"
	"gitea.dev/modules/util"
	"gitea.dev/modules/web/types"
	branchroom_service "gitea.dev/services/branchroom"
	"gitea.dev/services/context"
)

const tplBranchroomSourceHandoffs templates.TplName = "repo/branchroom/source_handoffs"

type branchroomSourceHandoffResponseWriter struct {
	http.ResponseWriter
}

func (resp *branchroomSourceHandoffResponseWriter) setPrivacyHeaders() {
	setBranchroomSourceHandoffResponseHeaders(resp.Header())
}

func (resp *branchroomSourceHandoffResponseWriter) WriteHeader(status int) {
	resp.setPrivacyHeaders()
	resp.ResponseWriter.WriteHeader(status)
}

func (resp *branchroomSourceHandoffResponseWriter) Write(data []byte) (int, error) {
	resp.setPrivacyHeaders()
	return resp.ResponseWriter.Write(data)
}

func (resp *branchroomSourceHandoffResponseWriter) Flush() {
	resp.setPrivacyHeaders()
	if flusher, ok := resp.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (resp *branchroomSourceHandoffResponseWriter) Unwrap() http.ResponseWriter {
	return resp.ResponseWriter
}

// BranchroomSourceHandoffHeaders protects every response selected by the source
// handoff route, including responses produced by earlier route middleware.
var BranchroomSourceHandoffHeaders types.PreMiddlewareProvider = func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(resp http.ResponseWriter, req *http.Request) {
		privateResp := &branchroomSourceHandoffResponseWriter{ResponseWriter: resp}
		privateResp.setPrivacyHeaders()
		next.ServeHTTP(privateResp, req)
	})
}

type branchroomSourceHandoffData struct {
	ScopeName string
	CloneURL  string
}

// BranchroomSourceHandoffs renders the stable page that an authorized direct user
// can use to retrieve a read-only Branchroom HTTP clone URL.
func BranchroomSourceHandoffs(ctx *context.Context) {
	setBranchroomSourceHandoffHeaders(ctx)
	if !setting.Branchroom.Enabled || !ctx.IsSigned || ctx.Doer == nil || ctx.DoerIsImpersonated() || !ctx.Doer.IsIndividual() || !ctx.Doer.IsActive || ctx.Doer.ProhibitLogin || ctx.Doer.MustChangePassword || ctx.Doer.IsGiteaActions() {
		ctx.NotFound(nil)
		return
	}

	handoffs, err := branchroom_service.ListRepositorySourceHandoffs(ctx, branchroom_service.ListRepositorySourceHandoffsRequest{
		RepoID: ctx.Repo.Repository.ID,
		UserID: ctx.Doer.ID,
	})
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			ctx.NotFound(nil)
			return
		}
		ctx.ServerError("ListRepositorySourceHandoffs", err)
		return
	}

	data := make([]branchroomSourceHandoffData, 0, len(handoffs))
	for _, handoff := range handoffs {
		data = append(data, branchroomSourceHandoffData{
			ScopeName: handoff.ScopeName,
			CloneURL:  branchroomSourceCloneURL(ctx, handoff.RouteKey),
		})
	}
	ctx.Data["Title"] = ctx.Tr("repo.branchroom.source_handoffs")
	ctx.Data["PageIsBranchroomSourceHandoffs"] = true
	ctx.Data["BranchroomSourceHandoffs"] = data
	ctx.HTML(http.StatusOK, tplBranchroomSourceHandoffs)
}

func branchroomSourceCloneURL(ctx *context.Context, routeKey string) string {
	return httplib.GuessCurrentAppURL(ctx) + "_branchroom/" + url.PathEscape(ctx.Repo.Owner.Name) + "/" + url.PathEscape(ctx.Repo.Repository.Name) + "/" + routeKey + ".git"
}

func setBranchroomSourceHandoffHeaders(ctx *context.Context) {
	setBranchroomSourceHandoffResponseHeaders(ctx.RespHeader())
}

func setBranchroomSourceHandoffResponseHeaders(header http.Header) {
	header.Set("Cache-Control", "private, no-store")
	header.Set("Vary", "Cookie, Authorization")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Robots-Tag", "noindex, nofollow")
}
