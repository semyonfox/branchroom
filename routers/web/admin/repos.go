// Copyright 2014 The Gogs Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package admin

import (
	"errors"
	"net/url"

	repo_model "gitea.dev/models/repo"
	"gitea.dev/modules/log"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/templates"
	"gitea.dev/routers/web/explore"
	branchroom_service "gitea.dev/services/branchroom"
	"gitea.dev/services/context"
	repo_service "gitea.dev/services/repository"
)

const (
	tplRepos templates.TplName = "admin/repo/list"
)

// Repos show all the repositories
func Repos(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("admin.repositories")
	ctx.Data["PageIsAdminRepositories"] = true

	explore.RenderRepoSearch(ctx, &explore.RepoSearchOptions{
		Private:          true,
		PageSize:         setting.UI.Admin.RepoPagingNum,
		TplName:          tplRepos,
		OnlyShowRelevant: false,
	})
}

// DeleteRepo delete one repository
func DeleteRepo(ctx *context.Context) {
	repo, err := repo_model.GetRepositoryByID(ctx, ctx.FormInt64("id"))
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}

	if err := repo_service.DeleteRepository(ctx, ctx.Doer, repo, true); err != nil {
		if errors.Is(err, branchroom_service.ErrWorkspaceRetentionRequired) {
			ctx.Flash.Error(ctx.Tr("admin.repos.branchroom_retention_required", repo.FullName()))
			ctx.JSONRedirect(setting.AppSubURL + "/-/admin/repos?page=" + url.QueryEscape(ctx.FormString("page")) + "&sort=" + url.QueryEscape(ctx.FormString("sort")))
			return
		}
		ctx.ServerErrorAuto(err)
		return
	}
	log.Trace("Repository deleted: %s", repo.FullName())

	ctx.Flash.Success(ctx.Tr("repo.settings.deletion_success"))
	ctx.JSONRedirect("")
}
