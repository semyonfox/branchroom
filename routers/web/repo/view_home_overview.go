// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"gitea.dev/models/activities"
	"gitea.dev/models/db"
	perm_model "gitea.dev/models/perm"
	access_model "gitea.dev/models/perm/access"
	repo_model "gitea.dev/models/repo"
	unit_model "gitea.dev/models/unit"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/services/context"
	repository_service "gitea.dev/services/repository"

	"xorm.io/builder"
)

func prepareRepoOverviewFacts(ctx *context.Context) {
	repo := ctx.Repo.Repository
	userIDs, err := access_model.GetUserIDsWithAnyUnitAccess(ctx, repo, perm_model.AccessModeWrite, unit_model.TypeCode)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	if len(userIDs) > 0 {
		var users []*user_model.User
		if err := db.GetEngine(ctx).In("id", userIDs.Values()).OrderBy("name").Find(&users); err != nil {
			ctx.ServerErrorAuto(err)
			return
		}
		var maintainers []overviewMaintainer
		for _, user := range users {
			if user.Type != user_model.UserTypeIndividual { // bot and agent accounts write code but never answer for it
				continue
			}
			permission, err := access_model.GetIndividualUserRepoPermission(ctx, repo, user)
			if err != nil {
				ctx.ServerErrorAuto(err)
				return
			}
			if !permission.CanWrite(unit_model.TypeCode) {
				continue
			}
			role := "write"
			if permission.IsOwner() {
				role = "owner"
			} else if permission.IsAdmin() {
				role = "admin"
			}
			maintainers = append(maintainers, overviewMaintainer{User: user, Role: role})
		}
		if len(maintainers) > 5 {
			ctx.Data["OverviewMoreMaintainers"] = len(maintainers) - 5
			maintainers = maintainers[:5]
		}
		ctx.Data["OverviewMaintainers"] = maintainers
	}

	push := new(activities.Action)
	has, err := db.GetEngine(ctx).Cols("created_unix", "ref_name").Where("repo_id = ? AND op_type = ?", repo.ID, activities.ActionCommitRepo).OrderBy("created_unix DESC").Get(push)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	if has {
		ctx.Data["OverviewLastPush"] = push.CreatedUnix
		ctx.Data["OverviewLastPushBranch"] = push.GetBranch()
	}

	if err := prepareOverviewContributors(ctx); err != nil {
		ctx.ServerErrorAuto(err)
		return
	}

	entry, err := ctx.Repo.Commit.GetTreeEntryByPath(ctx, ctx.Repo.GitRepo, "AGENTS.md")
	if err == nil && (entry.IsRegular() || entry.IsExecutable() || entry.IsLink()) {
		ctx.Data["OverviewAgentFile"] = "AGENTS.md"
	}
}

type overviewMaintainer struct {
	User *user_model.User
	Role string
}

type overviewDownload struct {
	Attachment *repo_model.Attachment
	Platform   string
	Label      string
	Arch       string
}

var overviewAssetToken = regexp.MustCompile(`[^a-z0-9]+`)

func overviewAssetPlatform(name string) (platform, arch string) {
	tokens := overviewAssetToken.Split(strings.ToLower(name), -1)
	for i, token := range tokens {
		switch token {
		case "sha256", "sha512", "checksums", "checksum", "sig", "asc":
			return "", ""
		case "linux":
			platform = "Linux"
		case "windows", "win":
			platform = "Windows"
		case "darwin", "macos", "mac":
			platform = "macOS"
		case "amd64":
			arch = "x86-64"
		case "x86":
			if i+1 < len(tokens) && tokens[i+1] == "64" {
				arch = "x86-64"
			}
		case "arm64", "aarch64":
			arch = "ARM64"
		}
	}
	return platform, arch
}

func prepareOverviewDownloads(ctx *context.Context, release *repo_model.Release) error {
	if err := repo_model.GetReleaseAttachments(ctx, release); err != nil {
		return err
	}
	var downloads []overviewDownload
	platforms := map[string]bool{}
	for _, attachment := range release.Attachments {
		platform, arch := overviewAssetPlatform(attachment.Name)
		if platform != "" && !platforms[platform] {
			platforms[platform] = true
			downloads = append(downloads, overviewDownload{attachment, platform, platform, arch})
		}
	}
	if len(downloads) == 0 {
		for _, attachment := range release.Attachments[:min(3, len(release.Attachments))] {
			downloads = append(downloads, overviewDownload{Attachment: attachment, Label: attachment.Name, Platform: attachment.Name})
		}
	}
	ctx.Data["OverviewDownloads"] = downloads
	return nil
}

type overviewContributor struct {
	Name    string
	Email   string
	User    *user_model.User
	Agent   bool
	Sponsor string
}

type overviewAuthor struct {
	Name  string
	Email string
}

func prepareOverviewContributors(ctx *context.Context) error {
	repo := ctx.Repo.Repository
	commit, err := ctx.Repo.GitRepo.GetBranchCommit(ctx, repo.DefaultBranch)
	if err != nil {
		return err
	}
	key := fmt.Sprintf("repo-overview-authors/%d/%s", repo.ID, commit.ID.String())
	var authors []overviewAuthor
	if hit, _ := ctx.Cache.GetJSON(key, &authors); !hit {
		var stats map[string]*repository_service.ContributorData
		if hit, cacheErr := ctx.Cache.GetJSON(fmt.Sprintf("GetContributorStats/%s/%s", repo.FullName(), repo.DefaultBranch), &stats); hit && cacheErr == nil {
			for email, contributor := range stats {
				if email != "total" {
					authors = append(authors, overviewAuthor{Name: contributor.Name, Email: email})
				}
			}
			slices.SortFunc(authors, func(a, b overviewAuthor) int { return cmp.Compare(a.Email, b.Email) })
		} else {
			// one git log for names and emails, CommitsBeforeLimit stops at commits shared with other branches
			stdout, _, err := gitcmd.NewCommand("log", "--max-count=300", "--format=%aN%x00%aE").
				AddDynamicArguments(commit.ID.String()).
				WithRepo(ctx.Repo.GitRepo).
				RunStdString(ctx)
			if err != nil {
				return err
			}
			seen := map[string]bool{}
			for line := range strings.SplitSeq(strings.TrimSpace(stdout), "\n") {
				name, email, ok := strings.Cut(line, "\x00")
				email = strings.ToLower(email)
				if ok && email != "" && !seen[email] {
					seen[email] = true
					authors = append(authors, overviewAuthor{Name: name, Email: email})
				}
			}
		}
		_ = ctx.Cache.PutJSON(key, authors, 60*10)
	}
	emails := make([]string, 0, len(authors))
	for _, author := range authors {
		emails = append(emails, author.Email)
	}
	users, err := user_model.GetUsersByEmails(ctx, emails)
	if err != nil {
		return err
	}
	var contributors []overviewContributor
	seen := map[string]bool{}
	for _, author := range authors {
		user := users.GetByEmail(author.Email)
		identity := author.Email
		if user != nil {
			identity = strconv.FormatInt(user.ID, 10)
		}
		if seen[identity] {
			continue
		}
		seen[identity] = true
		person := overviewContributor{Name: author.Name, Email: author.Email, User: user}
		if user != nil {
			person.Name = user.DisplayName()
			person.Agent = user.Type == user_model.UserTypeBot
		}
		contributors = append(contributors, person)
	}
	ctx.Data["OverviewContributorCount"] = len(contributors)
	contributors = contributors[:min(8, len(contributors))]
	userIDs := make([]int64, 0, len(contributors))
	for _, person := range contributors {
		if person.Agent {
			userIDs = append(userIDs, person.User.ID)
		}
	}
	if len(userIDs) > 0 {
		var agents []struct {
			UID     int64
			Sponsor string
		}
		issueAgents := builder.Select("issue.poster_id AS uid", "branchroom_agent_action.sponsor").From("branchroom_agent_action").
			InnerJoin("issue", "issue.id = branchroom_agent_action.target_id").
			Where(builder.Eq{"branchroom_agent_action.kind": "issue", "issue.repo_id": repo.ID}.And(builder.In("issue.poster_id", userIDs)))
		commentAgents := builder.Select("comment.poster_id AS uid", "branchroom_agent_action.sponsor").From("branchroom_agent_action").
			InnerJoin("comment", "comment.id = branchroom_agent_action.target_id").InnerJoin("issue", "issue.id = comment.issue_id").
			Where(builder.Eq{"branchroom_agent_action.kind": "comment", "issue.repo_id": repo.ID}.And(builder.In("comment.poster_id", userIDs)))
		reviewAgents := builder.Select("review.reviewer_id AS uid", "branchroom_agent_action.sponsor").From("branchroom_agent_action").
			InnerJoin("review", "review.id = branchroom_agent_action.target_id").InnerJoin("issue", "issue.id = review.issue_id").
			Where(builder.Eq{"branchroom_agent_action.kind": "review", "issue.repo_id": repo.ID}.And(builder.In("review.reviewer_id", userIDs)))
		receiveAgents := builder.Select("branchroom_receive_session.actor_id AS uid", "branchroom_agent_action.sponsor").From("branchroom_agent_action").
			InnerJoin("branchroom_receive_session", "branchroom_receive_session.id = branchroom_agent_action.target_id").
			Where(builder.Eq{"branchroom_agent_action.kind": "receive", "branchroom_receive_session.repo_id": repo.ID}.And(builder.In("branchroom_receive_session.actor_id", userIDs)))
		var queries []string
		var args []any
		for _, query := range []*builder.Builder{issueAgents, commentAgents, reviewAgents, receiveAgents} {
			statement, values, err := query.ToSQL()
			if err != nil {
				return err
			}
			queries = append(queries, statement)
			args = append(args, values...)
		}
		if err := db.GetEngine(ctx).SQL(strings.Join(queries, " UNION "), args...).Find(&agents); err != nil {
			return err
		}

		for i := range contributors {
			person := &contributors[i]
			if !person.Agent {
				continue
			}
			for _, agent := range agents {
				if agent.UID == person.User.ID {
					if agent.Sponsor != "" {
						person.Sponsor = agent.Sponsor
						break
					}
				}
			}
		}
	}

	ctx.Data["OverviewContributors"] = contributors
	return nil
}
