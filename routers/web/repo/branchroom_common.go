// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"net/http"

	"gitea.dev/services/context"
)

// branchroomPrivacyResponseWriter stamps surface-specific privacy headers onto
// every response crossing a Branchroom transport route.
type branchroomPrivacyResponseWriter struct {
	http.ResponseWriter
	setHeaders func(http.Header)
}

func (resp *branchroomPrivacyResponseWriter) applyPrivacyHeaders() {
	resp.setHeaders(resp.Header())
}

func (resp *branchroomPrivacyResponseWriter) WriteHeader(status int) {
	resp.applyPrivacyHeaders()
	resp.ResponseWriter.WriteHeader(status)
}

func (resp *branchroomPrivacyResponseWriter) Write(data []byte) (int, error) {
	resp.applyPrivacyHeaders()
	return resp.ResponseWriter.Write(data)
}

func (resp *branchroomPrivacyResponseWriter) Flush() {
	resp.applyPrivacyHeaders()
	if flusher, ok := resp.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (resp *branchroomPrivacyResponseWriter) Unwrap() http.ResponseWriter {
	return resp.ResponseWriter
}

func newBranchroomPrivacyResponseWriter(resp http.ResponseWriter, setHeaders func(http.Header)) *branchroomPrivacyResponseWriter {
	privateResp := &branchroomPrivacyResponseWriter{ResponseWriter: resp, setHeaders: setHeaders}
	privateResp.applyPrivacyHeaders()
	return privateResp
}

// isEligibleBranchroomDoer reports whether the signed-in identity may use any
// Branchroom transport surface at all.
func isEligibleBranchroomDoer(ctx *context.Context) bool {
	doer := ctx.Doer
	// system users such as Gitea Actions and deploy keys have negative IDs
	return doer != nil && doer.ID > 0 && !context.IsDoerSessionImpersonated(ctx.Session) && doer.IsIndividual() && doer.IsActive && !doer.ProhibitLogin && !doer.MustChangePassword
}
