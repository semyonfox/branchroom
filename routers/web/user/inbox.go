// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package user

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"gitea.dev/modules/setting"
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
	var busiest api.InboxGroup
	for _, group := range result.Groups {
		if group.Kind == "triage" && !result.CanTriage {
			continue
		}
		if group.Kind != view && group.Count > busiest.Count {
			busiest = group
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
	ctx.Data["InboxBusiest"] = busiest
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
		ctx.Redirect(setting.AppSubURL + "/inbox")
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

// InboxNext walks a view one item at a time. Acting on an item drops it from the view,
// so the next item is the first one left. Skipping sends an item to the back
func InboxNext(ctx *context.Context) {
	view, skipped := inbox_service.ParseQueueCookie(ctx.GetSiteCookie(inbox_service.QueueCookie))
	if v := ctx.FormString("view"); v != "" && v != view {
		view, skipped = v, nil
	}
	if view == "" {
		view = "needs"
	}
	back := setting.AppSubURL + "/inbox?view=" + url.QueryEscape(view)
	if ctx.FormBool("exit") {
		ctx.DeleteSiteCookie(inbox_service.QueueCookie)
		ctx.Redirect(back)
		return
	}
	items, found, err := inbox_service.Queue(ctx, ctx.Doer, view)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	if !found {
		ctx.NotFound()
		return
	}
	from, to := ctx.FormInt64("from"), ctx.FormInt64("to")
	if from > 0 {
		// skipped, or acted on but still waiting (approved, not merged): either way it goes to the back
		skipped = append(slices.DeleteFunc(skipped, func(id int64) bool { return id == from }), from)
	}
	skipped = slices.DeleteFunc(skipped, func(id int64) bool { // keeps the cookie to what is still waiting
		return !slices.ContainsFunc(items, func(item api.InboxItem) bool { return item.ID == id })
	})
	items = inbox_service.QueueOrder(items, skipped)
	next := slices.IndexFunc(items, func(item api.InboxItem) bool { return item.ID == to })
	if next < 0 { // no target, or it has left the view since the list loaded
		next = slices.IndexFunc(items, func(item api.InboxItem) bool { return item.ID != from })
	}
	if next < 0 {
		ctx.DeleteSiteCookie(inbox_service.QueueCookie)
		ctx.Flash.Success(ctx.Tr("inbox.queue_finished", ctx.Tr("inbox.view_"+view)))
		ctx.Redirect(back)
		return
	}
	ctx.SetSiteCookie(inbox_service.QueueCookie, inbox_service.FormatQueueCookie(view, skipped), 0)
	item := items[next]
	// decision items open on the diff, every other kind on the conversation
	target := item.HTMLURL
	if strings.HasPrefix(item.Action.URL, item.HTMLURL) {
		target = item.Action.URL
	}
	if did := ctx.FormString("did"); from > 0 && did != "" && !ctx.FormBool("skip") {
		if u, err := url.Parse(target); err == nil {
			q := u.Query()
			q.Set("done", strconv.FormatInt(from, 10))
			q.Set("did", did)
			u.RawQuery = q.Encode()
			target = u.String()
		}
	}
	ctx.Redirect(target)
}
