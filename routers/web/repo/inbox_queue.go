// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"fmt"
	"net/url"

	issues_model "gitea.dev/models/issues"
	"gitea.dev/modules/setting"
	"gitea.dev/services/context"
	inbox_service "gitea.dev/services/inbox"
	pull_service "gitea.dev/services/pull"
)

type inboxQueueBar struct {
	View     string
	Position int // 0 when this pull request has already left the view
	Count    int
	Redirect string
	CanMerge bool
	SkipURL  string
	ExitURL  string
}

// prepareInboxQueue shows the queue bar while the user works through an inbox view
func prepareInboxQueue(ctx *context.Context, issue *issues_model.Issue) {
	view, skipped := inbox_service.ParseQueueCookie(ctx.GetSiteCookie(inbox_service.QueueCookie))
	if view == "" || !ctx.IsSigned {
		return
	}
	items, found, err := inbox_service.Queue(ctx, ctx.Doer, view)
	if err != nil || !found {
		return // the bar is a convenience, never a reason to fail the page
	}
	items = inbox_service.QueueOrder(items, skipped)
	base := fmt.Sprintf("%s/inbox/next?view=%s&from=%d", setting.AppSubURL, url.QueryEscape(view), issue.ID)
	bar := &inboxQueueBar{View: view, Count: len(items), Redirect: base, SkipURL: base + "&skip=1", ExitURL: setting.AppSubURL + "/inbox/next?exit=1&view=" + url.QueryEscape(view)}
	for i, item := range items {
		if item.ID == issue.ID {
			bar.Position = i + 1
		}
	}
	if issue.PullRequest != nil && !issue.IsClosed {
		if r, err := pull_service.GetReadiness(ctx, issue.PullRequest); err == nil && len(r.Blockers) == 0 {
			bar.CanMerge, _ = pull_service.IsUserAllowedToMerge(ctx, issue.PullRequest, ctx.Repo.Permission, ctx.Doer)
		}
	}
	ctx.Data["InboxQueue"] = bar
}
