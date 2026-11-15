// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"strings"
	"time"

	"gitea.dev/models/activities"
	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	perm_model "gitea.dev/models/perm"
	access_model "gitea.dev/models/perm/access"
	unit_model "gitea.dev/models/unit"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/timeutil"
	"gitea.dev/services/context"

	"xorm.io/builder"
)

const repoHomeViewSetting = "repo_home_view"

func prepareRepoHomeView(ctx *context.Context) {
	isHome := strings.TrimSuffix(ctx.Req.URL.Path, "/") == ctx.Repo.RepoLink
	view := ctx.FormString("view")
	explicit := view == "overview" || view == "code"
	if !isHome {
		view = "code"
	}
	if ctx.Doer != nil {
		if explicit && (isHome || ctx.FormString("view") == "code") {
			if err := user_model.SetUserSetting(ctx, ctx.Doer.ID, repoHomeViewSetting, view); err != nil {
				ctx.ServerErrorAuto(err)
				return
			}
		} else if isHome {
			var err error
			view, err = user_model.GetUserSetting(ctx, ctx.Doer.ID, repoHomeViewSetting, "overview")
			if err != nil {
				ctx.ServerErrorAuto(err)
				return
			}
		}
	}
	if isHome && view != "code" {
		ctx.Data["PageIsRepoOverview"] = true
	}
}

func prepareRepoOverviewFacts(ctx *context.Context) {
	repo := ctx.Repo.Repository
	userIDs, err := access_model.GetUserIDsWithAnyUnitAccess(ctx, repo, perm_model.AccessModeWrite, unit_model.TypeCode)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	if len(userIDs) > 0 {
		var maintainers []*user_model.User
		if err := db.GetEngine(ctx).In("id", userIDs.Values()).OrderBy("name").Limit(3).Find(&maintainers); err != nil {
			ctx.ServerErrorAuto(err)
			return
		}
		ctx.Data["OverviewMaintainers"] = maintainers
	}

	push := new(activities.Action)
	has, err := db.GetEngine(ctx).Cols("created_unix").Where("repo_id = ? AND op_type = ?", repo.ID, activities.ActionCommitRepo).OrderBy("created_unix DESC").Get(push)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	if has {
		ctx.Data["OverviewLastPush"] = push.CreatedUnix
	}

	// action rows inherit the repository and date from their target
	since := timeutil.TimeStamp(time.Now().AddDate(0, 0, -30).Unix())
	issueTargets := builder.Select("id").From("issue").Where(builder.Eq{"repo_id": repo.ID}.And(builder.Gte{"created_unix": since}))
	commentTargets := builder.Select("comment.id").From("comment").InnerJoin("issue", "issue.id = comment.issue_id").Where(builder.Eq{"issue.repo_id": repo.ID}.And(builder.Gte{"comment.created_unix": since}))
	reviewTargets := builder.Select("review.id").From("review").InnerJoin("issue", "issue.id = review.issue_id").Where(builder.Eq{"issue.repo_id": repo.ID}.And(builder.Gte{"review.created_unix": since}))
	receiveTargets := builder.Select("id").From("branchroom_receive_session").Where(builder.Eq{"repo_id": repo.ID}.And(builder.Gte{"created_unix": since}))
	count, err := db.GetEngine(ctx).Where(builder.Neq{"agent": ""}.And(builder.Or(
		builder.Eq{"kind": "issue"}.And(builder.In("target_id", issueTargets)),
		builder.Eq{"kind": "comment"}.And(builder.In("target_id", commentTargets)),
		builder.Eq{"kind": "review"}.And(builder.In("target_id", reviewTargets)),
		builder.Eq{"kind": "receive"}.And(builder.In("target_id", receiveTargets)),
	))).Distinct("agent").Count(new(branchroom_model.AgentAction))
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	ctx.Data["OverviewAgentCount"] = count

	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		entry, err := ctx.Repo.Commit.GetTreeEntryByPath(ctx, ctx.Repo.GitRepo, name)
		if err == nil && (entry.IsRegular() || entry.IsExecutable() || entry.IsLink()) {
			ctx.Data["OverviewAgentFile"] = name
			break
		}
	}
}
