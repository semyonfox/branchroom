// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package user

import (
	"net/http"

	"gitea.dev/services/context"
	inbox_service "gitea.dev/services/inbox"
)

// GetInbox returns work needing the authenticated user's attention.
func GetInbox(ctx *context.APIContext) {
	// swagger:operation GET /user/inbox user userGetInbox
	// ---
	// summary: Get review decisions, blocked work, agent activity and unread mentions
	// description: Each group returns at most 25 items from 100 recent candidates. Counts describe returned items. Requires read:issue and read:repository scopes.
	// produces:
	// - application/json
	// responses:
	//   "200":
	//     "$ref": "#/responses/Inbox"
	//   default:
	//     "$ref": "#/responses/error"
	result, err := inbox_service.Get(ctx, ctx.Doer)
	if err != nil {
		ctx.APIErrorInternal(err)
		return
	}
	ctx.JSON(http.StatusOK, result)
}
