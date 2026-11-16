// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package migrations

import (
	"context"
	"errors"
	"strings"

	actions_model "gitea.dev/models/actions"
	"gitea.dev/models/db"
	git_model "gitea.dev/models/git"
	"gitea.dev/models/organization"
	"gitea.dev/models/perm"
	access_model "gitea.dev/models/perm/access"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unit"
	user_model "gitea.dev/models/user"
	webhook_model "gitea.dev/models/webhook"
	base "gitea.dev/modules/migration"
	"gitea.dev/modules/util"
	webhook_module "gitea.dev/modules/webhook"
	actions_service "gitea.dev/services/actions"
	repo_service "gitea.dev/services/repository"
)

// CreateSettings applies the settings and saves a report of what a person still has to do
func (g *GiteaLocalUploader) CreateSettings(ctx context.Context, settings *base.RepoSettings) error {
	report := &base.SettingsReport{Notes: settings.Notes}
	// one transaction, so a retried migration never meets half-applied settings
	return db.WithTx(ctx, func(ctx context.Context) error {
		for _, create := range []func(context.Context, *base.RepoSettings, *base.SettingsReport) error{
			g.createBranchProtections, g.createCollaborators, g.createTeams, g.createWebhooks, g.createVariables,
		} {
			if err := create(ctx, settings, report); err != nil {
				return err
			}
		}
		return repo_model.SaveMigrationReport(ctx, g.repo.ID, report)
	})
}

func (g *GiteaLocalUploader) createBranchProtections(ctx context.Context, settings *base.RepoSettings, report *base.SettingsReport) error {
	for _, protection := range settings.BranchProtections {
		canPush := !protection.BlockPush
		rule := &git_model.ProtectedBranch{
			RepoID:   g.repo.ID,
			RuleName: protection.Pattern,
			ProtectedBranchConfig: git_model.ProtectedBranchConfig{
				CanPush:                 canPush,
				CanForcePush:            canPush && protection.AllowForcePush,
				CanDelete:               canPush && protection.AllowDeletion,
				EnableStatusCheck:       protection.EnableStatusCheck,
				StatusCheckContexts:     protection.StatusCheckContexts,
				RequiredApprovals:       protection.RequiredApprovals,
				BlockOnRejectedReviews:  protection.RequiredApprovals > 0,
				BlockOnCodeownerReviews: protection.RequireCodeOwnerReviews,
				BlockOnOutdatedBranch:   protection.RequireUpToDateBranch,
				DismissStaleApprovals:   protection.DismissStaleApprovals,
				RequireSignedCommits:    protection.RequireSignedCommits,
				BlockAdminMergeOverride: protection.EnforceForAdmins,
			},
		}
		if err := git_model.UpdateProtectBranch(ctx, g.repo, rule, git_model.WhitelistOptions{}); err != nil {
			return err
		}
		report.BranchProtections++
		if len(protection.Unsupported) > 0 {
			report.Notes = append(report.Notes, base.SettingsNote{Kind: base.SettingsNoteBranchProtection, Name: protection.Pattern, Detail: strings.Join(protection.Unsupported, ", ")})
		}
	}
	return nil
}

var settingsAccessModes = map[string]perm.AccessMode{
	"read":  perm.AccessModeRead,
	"write": perm.AccessModeWrite,
	"admin": perm.AccessModeAdmin,
}

func (g *GiteaLocalUploader) createCollaborators(ctx context.Context, settings *base.RepoSettings, report *base.SettingsReport) error {
	for _, collaborator := range settings.Collaborators {
		// only a linked account proves who a login is here, the same name can belong to someone else
		userID, err := g.remapExternalUser(ctx, collaborator)
		if err != nil {
			return err
		}
		if userID == g.repo.OwnerID {
			continue
		}
		if userID > 0 {
			user, err := user_model.GetUserByID(ctx, userID)
			if err != nil {
				return err
			}
			err = repo_service.AddOrUpdateCollaborator(ctx, g.repo, user, settingsAccessModes[collaborator.Permission])
			if err == nil {
				report.Collaborators++
				continue
			}
			if !errors.Is(err, user_model.ErrBlockedUser) {
				return err
			}
		}
		report.Notes = append(report.Notes, base.SettingsNote{Kind: base.SettingsNoteCollaborator, Name: collaborator.Login, Detail: collaborator.Permission})
	}
	return nil
}

func (g *GiteaLocalUploader) createTeams(ctx context.Context, settings *base.RepoSettings, report *base.SettingsReport) error {
	if len(settings.Teams) == 0 {
		return nil
	}
	permission, err := access_model.GetDoerRepoPermission(ctx, g.repo, g.doer)
	if err != nil {
		return err
	}
	mayAddTeams := access_model.CanDoerManageOrgRepoCollaboratorTeam(ctx, g.repo, &permission)
	for _, source := range settings.Teams {
		var team *organization.Team
		if mayAddTeams {
			if team, err = g.findTeam(ctx, source); err != nil && !organization.IsErrTeamNotExist(err) {
				return err
			}
		}
		switch {
		case team == nil:
			report.Notes = append(report.Notes, base.SettingsNote{Kind: base.SettingsNoteTeam, Name: source.Name, Detail: source.Permission})
		case team.IncludesAllRepositories:
		case team.UnitAccessMode(ctx, unit.TypeCode) > settingsAccessModes[source.Permission]:
			// a team has one access level for all its repositories here, joining it must not hand out more than the source did
			report.Notes = append(report.Notes, base.SettingsNote{Kind: base.SettingsNoteTeamAccess, Name: source.Name, Detail: source.Permission})
		default:
			if err := repo_service.TeamAddRepository(ctx, team, g.repo); err != nil {
				return err
			}
			report.Teams++
		}
	}
	return nil
}

func (g *GiteaLocalUploader) findTeam(ctx context.Context, source *base.Team) (team *organization.Team, err error) {
	for _, name := range []string{source.Slug, source.Name} {
		if team, err = organization.GetTeam(ctx, g.repo.OwnerID, name); !organization.IsErrTeamNotExist(err) {
			break
		}
	}
	return team, err
}

func (g *GiteaLocalUploader) createWebhooks(ctx context.Context, settings *base.RepoSettings, report *base.SettingsReport) error {
	for _, source := range settings.Webhooks {
		host := webhookHost(source.URL)
		if !source.SendEverything && len(source.Events) == 0 {
			report.Notes = append(report.Notes, base.SettingsNote{Kind: base.SettingsNoteWebhookSkipped, Name: host, Detail: strings.Join(source.UnsupportedEvents, ", ")})
			continue
		}
		events := make(webhook_module.HookEvents, len(source.Events))
		for _, event := range source.Events {
			events[webhook_module.HookEventType(event)] = true
		}
		webhook := &webhook_model.Webhook{
			RepoID:      g.repo.ID,
			URL:         source.URL,
			HTTPMethod:  "POST",
			ContentType: webhook_model.ToHookContentType(source.ContentType),
			HookEvent: &webhook_module.HookEvent{
				SendEverything: source.SendEverything,
				ChooseEvents:   !source.SendEverything,
				HookEvents:     events,
			},
			// the secret can not be read and the source may still be sending, so a person switches it on
			IsActive: false,
			Type:     webhook_module.GITEA,
		}
		if err := webhook.UpdateEvent(); err != nil {
			return err
		}
		if err := webhook_model.CreateWebhook(ctx, webhook); err != nil {
			return err
		}
		report.Webhooks++
		report.Notes = append(report.Notes, base.SettingsNote{Kind: base.SettingsNoteWebhook, Name: host, Detail: strings.Join(source.UnsupportedEvents, ", ")})
	}
	return nil
}

func (g *GiteaLocalUploader) createVariables(ctx context.Context, settings *base.RepoSettings, report *base.SettingsReport) error {
	for _, variable := range settings.Variables {
		_, err := actions_service.CreateVariable(ctx, actions_model.BelongingScopeRepo(g.repo.ID), variable.Name, variable.Value, "")
		if errors.Is(err, util.ErrInvalidArgument) {
			report.Notes = append(report.Notes, base.SettingsNote{Kind: base.SettingsNoteVariable, Name: variable.Name})
			continue
		}
		if err != nil {
			return err
		}
		report.Variables++
	}
	return nil
}
