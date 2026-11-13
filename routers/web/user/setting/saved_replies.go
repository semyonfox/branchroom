// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	user_model "gitea.dev/models/user"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/templates"
	"gitea.dev/modules/util"
	"gitea.dev/services/context"
)

const tplSettingsSavedReplies templates.TplName = "user/settings/saved_replies"

// SavedReplies lists the user's saved replies, with the one in ?edit= loaded into the form
func SavedReplies(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("settings.saved_replies")
	ctx.Data["PageIsSettingsSavedReplies"] = true

	replies, err := user_model.GetSavedReplies(ctx, ctx.Doer.ID)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	ctx.Data["SavedReplies"] = replies
	if id := ctx.FormInt64("edit"); id > 0 {
		reply, err := user_model.GetSavedReply(ctx, ctx.Doer.ID, id)
		if err != nil && !errors.Is(err, util.ErrNotExist) {
			ctx.ServerErrorAuto(err)
			return
		}
		ctx.Data["EditReply"] = reply
	}
	ctx.HTML(http.StatusOK, tplSettingsSavedReplies)
}

// SavedRepliesPost creates a saved reply, or updates the one named by id
func SavedRepliesPost(ctx *context.Context) {
	reply := &user_model.SavedReply{
		ID:      ctx.FormInt64("id"),
		UserID:  ctx.Doer.ID,
		Title:   strings.TrimSpace(ctx.FormString("title")),
		Content: ctx.FormString("content"),
	}
	link := setting.AppSubURL + "/user/settings/replies"
	if reply.Title == "" || utf8.RuneCountInString(reply.Title) > 255 || strings.TrimSpace(reply.Content) == "" || len(reply.Content) > 64*1024 {
		ctx.Flash.Error(ctx.Tr("settings.saved_replies_invalid"))
		ctx.Redirect(link)
		return
	}
	if reply.ID > 0 {
		if _, err := user_model.GetSavedReply(ctx, ctx.Doer.ID, reply.ID); err != nil {
			ctx.ServerErrorAuto(err)
			return
		}
	}
	if err := user_model.SaveSavedReply(ctx, reply); err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	ctx.Flash.Success(ctx.Tr("settings.saved_replies_saved"))
	ctx.Redirect(link)
}

// SavedRepliesDelete deletes one of the user's saved replies
func SavedRepliesDelete(ctx *context.Context) {
	if err := user_model.DeleteSavedReply(ctx, ctx.Doer.ID, ctx.FormInt64("id")); err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	ctx.Flash.Success(ctx.Tr("settings.saved_replies_deleted"))
	ctx.JSONRedirect(setting.AppSubURL + "/user/settings/replies")
}

type savedReplyItem struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

// SavedRepliesList feeds the comment editor's saved replies menu
func SavedRepliesList(ctx *context.Context) {
	replies, err := user_model.GetSavedReplies(ctx, ctx.Doer.ID)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	items := make([]savedReplyItem, len(replies))
	for i, r := range replies {
		items[i] = savedReplyItem{Title: r.Title, Content: r.Content}
	}
	ctx.JSON(http.StatusOK, items)
}
