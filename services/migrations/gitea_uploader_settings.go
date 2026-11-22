// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package migrations

import (
	"context"
	"errors"
	"net/url"
	"strings"

	actions_model "gitea.dev/models/actions"
	auth_model "gitea.dev/models/auth"
	"gitea.dev/models/db"
	git_model "gitea.dev/models/git"
	"gitea.dev/models/organization"
	"gitea.dev/models/perm"
	access_model "gitea.dev/models/perm/access"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unit"
	user_model "gitea.dev/models/user"
	webhook_model "gitea.dev/models/webhook"
	"gitea.dev/modules/container"
	base "gitea.dev/modules/migration"
	"gitea.dev/modules/optional"
	"gitea.dev/modules/structs"
	"gitea.dev/modules/util"
	webhook_module "gitea.dev/modules/webhook"
	actions_service "gitea.dev/services/actions"
	"gitea.dev/services/auth/source/oauth2"
	repo_service "gitea.dev/services/repository"
	secret_service "gitea.dev/services/secrets"
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
				CanPush:                       canPush,
				CanForcePush:                  canPush && protection.AllowForcePush,
				CanDelete:                     canPush && protection.AllowDeletion,
				EnableStatusCheck:             protection.EnableStatusCheck,
				StatusCheckContexts:           protection.StatusCheckContexts,
				RequiredApprovals:             protection.RequiredApprovals,
				BlockOnRejectedReviews:        protection.BlockOnRejectedReviews,
				BlockOnOfficialReviewRequests: protection.BlockOnReviewRequests,
				IgnoreStaleApprovals:          protection.IgnoreStaleApprovals,
				ProtectedFilePatterns:         protection.ProtectedFilePatterns,
				UnprotectedFilePatterns:       protection.UnprotectedFilePatterns,
				BlockOnCodeownerReviews:       protection.RequireCodeOwnerReviews,
				BlockOnOutdatedBranch:         protection.RequireUpToDateBranch,
				DismissStaleApprovals:         protection.DismissStaleApprovals,
				RequireSignedCommits:          protection.RequireSignedCommits,
				BlockAdminMergeOverride:       protection.EnforceForAdmins,
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
	"none":  perm.AccessModeNone,
	"read":  perm.AccessModeRead,
	"write": perm.AccessModeWrite,
	"admin": perm.AccessModeAdmin,
}

func (g *GiteaLocalUploader) createCollaborators(ctx context.Context, settings *base.RepoSettings, report *base.SettingsReport) error {
	for _, collaborator := range settings.Collaborators {
		userID, err := g.linkedUserID(ctx, collaborator.ExternalID)
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

// linkedUserID returns the user who signs in here with that account of the migration source, 0 if nobody does.
// Only that proves who a login is: the same name can belong to someone else, and so can the same id on another instance of the forge.
func (g *GiteaLocalUploader) linkedUserID(ctx context.Context, externalID string) (int64, error) {
	if externalID == "" {
		return 0, nil
	}
	source, err := url.Parse(g.repo.OriginalURL)
	if err != nil {
		return 0, err
	}
	provider := g.gitServiceType.Name()
	if g.gitServiceType == structs.ForgejoService {
		provider = structs.GiteaService.Name() // Forgejo has no sign-in provider of its own
	}
	loginSources, err := db.Find[auth_model.Source](ctx, auth_model.FindSourcesOptions{LoginType: auth_model.OAuth2, IsActive: optional.Some(true)})
	if err != nil {
		return 0, err
	}
	for _, loginSource := range loginSources {
		config := auth_model.MustSourceCfg[*oauth2.Source](loginSource)
		if config.Provider != provider || !strings.EqualFold(loginSourceHost(loginSource), source.Host) {
			continue
		}
		links, err := db.Find[user_model.ExternalLoginUser](ctx, user_model.FindExternalUserOptions{LoginSourceID: loginSource.ID, ExternalID: externalID})
		if err != nil {
			return 0, err
		}
		if len(links) > 0 {
			return links[0].UserID, nil
		}
	}
	return 0, nil
}

func loginSourceHost(loginSource *auth_model.Source) string {
	config, ok := loginSource.Cfg.(*oauth2.Source)
	if !ok {
		return ""
	}
	if config.Provider == structs.BitbucketService.Name() {
		return "bitbucket.org" // the one provider here that can not point anywhere else
	}
	provider, err := oauth2.CreateProviderFromSource(loginSource)
	if err != nil || provider.CustomURLSettings() == nil {
		return ""
	}
	authURL, err := url.Parse(provider.CustomURLSettings().OverrideWith(config.CustomURLMapping).AuthURL)
	if err != nil {
		return ""
	}
	return authURL.Host
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
				BranchFilter:   source.BranchFilter,
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

// createVariables also stores the secrets a source gave back. Names are upper-cased here, so two that differ only by case would collide
func (g *GiteaLocalUploader) createVariables(ctx context.Context, settings *base.RepoSettings, report *base.SettingsReport) error {
	scope := actions_model.BelongingScopeRepo(g.repo.ID)
	names := container.Set[string]{}
	for _, variable := range settings.Variables {
		created := false
		if names.Add(strings.ToUpper(variable.Name)) {
			_, err := actions_service.CreateVariable(ctx, scope, variable.Name, variable.Value, "")
			if err != nil && !errors.Is(err, util.ErrInvalidArgument) {
				return err
			}
			created = err == nil
		}
		if created {
			report.Variables++
		} else {
			report.Notes = append(report.Notes, base.SettingsNote{Kind: base.SettingsNoteVariable, Name: variable.Name})
		}
	}
	names = container.Set[string]{}
	for _, secret := range settings.Secrets {
		created := false
		if names.Add(strings.ToUpper(secret.Name)) {
			_, _, err := secret_service.CreateOrUpdateSecret(ctx, scope, secret.Name, secret.Value, "")
			if err != nil && !errors.Is(err, util.ErrInvalidArgument) {
				return err
			}
			created = err == nil
		}
		if created {
			report.Secrets++
		} else {
			report.Notes = append(report.Notes, base.SettingsNote{Kind: base.SettingsNoteSecret, Name: secret.Name})
		}
	}
	return nil
}
