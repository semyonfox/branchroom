// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v1

import (
	"gitea.dev/modules/web"
	mcp_router "gitea.dev/routers/api/mcp"
	"gitea.dev/routers/common"
	"gitea.dev/services/context"
)

// MCPRoutes shares API authentication with REST and dispatches tools through its router.
func MCPRoutes(rest *web.Router) *web.Router {
	dispatch := web.NewRouter()
	dispatch.BeforeRouting(common.RequestContextHandler())
	dispatch.Mount("/api/v1", rest)
	m := web.NewRouter()
	m.AfterRouting(context.APIContexter(), apiAuth(buildAuthGroup()), verifyAuthWithOptionsAPI(&common.VerifyOptions{SignInRequired: true}), reqToken())
	m.Any("", mcp_router.Handler(dispatch))
	return m
}
