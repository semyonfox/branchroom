// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package user

import (
	"net/http"
	"strconv"

	api "gitea.dev/modules/structs"
	"gitea.dev/modules/templates"
	"gitea.dev/services/context"
	inbox_service "gitea.dev/services/inbox"
)

func Inbox(ctx *context.Context) {
	result, err := inbox_service.GetViews(ctx, ctx.Doer)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	view := ctx.FormString("view")
	if view == "" {
		view = "needs"
	}
	found := false
	for _, group := range result.Groups {
		if group.Kind == "triage" && !result.CanTriage {
			continue
		}
		if group.Kind == view {
			ctx.Data["InboxItems"] = group.Items
			found = true
		}
	}
	if !found {
		ctx.NotFound()
		return
	}
	ctx.Data["PageIsDashboard"] = true
	ctx.Data["Title"] = ctx.Locale.TrString("inbox.title")
	ctx.Data["Inbox"] = result
	ctx.Data["InboxView"] = view
	ctx.HTML(http.StatusOK, templates.TplName("user/inbox/page"))
}

func InboxAction(ctx *context.Context) {
	if err := ctx.Req.ParseForm(); err != nil {
		ctx.HTTPError(http.StatusBadRequest)
		return
	}
	ids := ctx.Req.PostForm["items"]
	if len(ids) == 0 || len(ids) > 100 {
		ctx.Flash.Error(ctx.Locale.TrString("inbox.select_items"))
		ctx.Redirect("/inbox")
		return
	}
	opts := api.InboxStateOptions{Action: ctx.FormString("action"), Reason: ctx.FormString("reason")}
	for _, raw := range ids {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			ctx.HTTPError(http.StatusBadRequest)
			return
		}
		opts.Items = append(opts.Items, id)
	}
	results := inbox_service.Apply(ctx, ctx.Doer, opts)
	ctx.Data["InboxResults"] = results
	Inbox(ctx)
}
