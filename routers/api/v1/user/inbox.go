// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package user

import (
	"net/http"

	api "gitea.dev/modules/structs"
	"gitea.dev/modules/web"
	"gitea.dev/services/context"
	inbox_service "gitea.dev/services/inbox"
)

// GetInbox returns work needing the authenticated user's attention.
func GetInbox(ctx *context.APIContext) {
	// swagger:operation GET /user/inbox user userGetInbox
	// ---
	// summary: Get review decisions, blocked work, agent activity and unread mentions
	// description: Each group returns at most 25 items from 100 recent candidates. Counts describe returned items. Requires read:issue and read:repository scopes.
	// parameters:
	// - name: view
	//   in: query
	//   type: string
	//   enum: [needs, triage, yours, agents, mentions, snoozed, done]
	// produces:
	// - application/json
	// responses:
	//   "200":
	//     "$ref": "#/responses/Inbox"
	//   default:
	//     "$ref": "#/responses/error"
	var result *api.Inbox
	var err error
	if ctx.FormString("view") == "" {
		result, err = inbox_service.Get(ctx, ctx.Doer)
	} else {
		result, err = inbox_service.GetViews(ctx, ctx.Doer)
	}
	if err != nil {
		ctx.APIErrorInternal(err)
		return
	}
	if view := ctx.FormString("view"); view != "" {
		found := false
		for _, group := range result.Groups {
			if group.Kind == view && (view != "triage" || result.CanTriage) {
				result.Groups = []api.InboxGroup{group}
				found = true
				break
			}
		}
		if !found {
			ctx.APIError(http.StatusBadRequest, "unknown or unavailable inbox view")
			return
		}
	}
	ctx.JSON(http.StatusOK, result)
}

func SetInboxState(ctx *context.APIContext) {
	// swagger:operation POST /user/inbox/state user userSetInboxState
	// ---
	// summary: Apply a bulk inbox action with a result for each item
	// parameters:
	// - name: body
	//   in: body
	//   required: true
	//   schema:
	//     "$ref": "#/definitions/InboxStateOptions"
	// produces:
	// - application/json
	// responses:
	//   "200":
	//     "$ref": "#/responses/InboxStateResults"
	//   default:
	//     "$ref": "#/responses/error"
	opts := web.GetForm[*api.InboxStateOptions](ctx)
	if len(opts.Items) == 0 || len(opts.Items) > 100 {
		ctx.APIError(http.StatusBadRequest, "select between 1 and 100 items")
		return
	}
	ctx.JSON(http.StatusOK, inbox_service.Apply(ctx, ctx.Doer, *opts))
}
