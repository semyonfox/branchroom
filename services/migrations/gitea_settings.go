// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package migrations

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"

	base "gitea.dev/modules/migration"
	webhook_module "gitea.dev/modules/webhook"
	gitea_sdk "gitea.dev/sdk"
)

// GetSettings reads the settings the token may see and notes the rest
func (g *GiteaDownloader) GetSettings(ctx context.Context) (*base.RepoSettings, error) {
	repo, _, err := g.client.Repositories.GetRepo(ctx, g.repoOwner, g.repoName)
	if err != nil {
		return nil, err
	}

	settings := &base.RepoSettings{}
	if repo.Permissions == nil || !repo.Permissions.Admin {
		settings.Notes = append(settings.Notes, base.SettingsNote{Kind: base.SettingsNoteNotAdmin})
	}

	protections, err := giteaListAll(g, func(page gitea_sdk.ListOptions) ([]*gitea_sdk.BranchProtection, *gitea_sdk.Response, error) {
		return g.client.Repositories.ListBranchProtections(ctx, g.repoOwner, g.repoName, gitea_sdk.ListBranchProtectionsOptions{ListOptions: page})
	})
	for _, protection := range readableSettings(settings, "branch protection", protections, err) {
		settings.BranchProtections = append(settings.BranchProtections, convertGiteaBranchProtection(protection))
	}

	collaborators, err := g.getCollaborators(ctx)
	settings.Collaborators = readableSettings(settings, "collaborators", collaborators, err)

	teams, resp, err := g.client.Repositories.GetRepoTeams(ctx, g.repoOwner, g.repoName)
	if resp != nil && resp.StatusCode == http.StatusMethodNotAllowed { // the owner is a person, so there are no teams
		teams, err = nil, nil
	}
	for _, team := range readableSettings(settings, "teams", teams, giteaStatusError(resp, err)) {
		if team.IncludesAllRepositories {
			continue
		}
		access := string(team.Permission)
		if len(team.UnitsMap) > 0 { // a team with per-unit access reports "none" as its permission
			access = cmp.Or(team.UnitsMap["repo.code"], "none")
		}
		settings.Teams = append(settings.Teams, &base.Team{Name: team.Name, Slug: team.Name, Permission: convertGiteaAccessMode(access)})
	}

	hooks, err := giteaListAll(g, func(page gitea_sdk.ListOptions) ([]*gitea_sdk.Hook, *gitea_sdk.Response, error) {
		return g.client.Hooks.ListRepoHooks(ctx, g.repoOwner, g.repoName, gitea_sdk.ListHooksOptions{ListOptions: page})
	})
	for _, hook := range readableSettings(settings, "webhooks", hooks, err) {
		if hook.Type != "gitea" && hook.Type != "forgejo" {
			settings.Notes = append(settings.Notes, base.SettingsNote{Kind: base.SettingsNoteWebhookKind, Name: webhookHost(hook.Config["url"]), Detail: hook.Type})
			continue
		}
		settings.Webhooks = append(settings.Webhooks, convertGiteaWebhook(hook))
	}

	variables, err := giteaListAll(g, func(page gitea_sdk.ListOptions) ([]*gitea_sdk.RepoActionsVariable, *gitea_sdk.Response, error) {
		return g.client.Actions.ListRepoVariables(ctx, g.repoOwner, g.repoName, gitea_sdk.ListRepoActionsVariableOption{ListOptions: page})
	})
	for _, variable := range readableSettings(settings, "Actions variables", variables, err) {
		settings.Variables = append(settings.Variables, &base.ActionsVariable{Name: variable.Name, Value: variable.Value})
	}

	secrets, err := giteaListAll(g, func(page gitea_sdk.ListOptions) ([]*gitea_sdk.Secret, *gitea_sdk.Response, error) {
		return g.client.Actions.ListRepoSecrets(ctx, g.repoOwner, g.repoName, gitea_sdk.ListRepoActionsSecretOption{ListOptions: page})
	})
	for _, secret := range readableSettings(settings, "Actions secrets", secrets, err) {
		settings.Notes = append(settings.Notes, base.SettingsNote{Kind: base.SettingsNoteSecret, Name: secret.Name})
	}

	return settings, ctx.Err()
}

func (g *GiteaDownloader) getCollaborators(ctx context.Context) ([]*base.Collaborator, error) {
	users, err := giteaListAll(g, func(page gitea_sdk.ListOptions) ([]*gitea_sdk.User, *gitea_sdk.Response, error) {
		return g.client.Repositories.ListCollaborators(ctx, g.repoOwner, g.repoName, gitea_sdk.ListCollaboratorsOptions{ListOptions: page})
	})
	if err != nil && !errors.Is(err, errSettingsPageLimit) {
		return nil, err
	}
	collaborators := make([]*base.Collaborator, 0, len(users))
	for _, user := range users {
		permission, resp, err := g.client.Repositories.CollaboratorPermission(ctx, g.repoOwner, g.repoName, user.UserName)
		if err != nil {
			return nil, giteaStatusError(resp, err)
		}
		collaborators = append(collaborators, &base.Collaborator{
			ExternalID: strconv.FormatInt(user.ID, 10),
			Login:      user.UserName,
			Permission: convertGiteaAccessMode(string(permission.Permission)),
		})
	}
	return collaborators, err
}

func giteaListAll[T any](g *GiteaDownloader, list func(page gitea_sdk.ListOptions) ([]T, *gitea_sdk.Response, error)) ([]T, error) {
	var all []T
	for page := 1; page <= maxSettingsPages; page++ {
		items, resp, err := list(gitea_sdk.ListOptions{Page: page, PageSize: g.maxPerPage})
		if err != nil {
			return nil, giteaStatusError(resp, err)
		}
		all = append(all, items...)
		if !g.pagination || len(items) < g.maxPerPage {
			return all, nil
		}
	}
	return all, errSettingsPageLimit
}

func giteaStatusError(resp *gitea_sdk.Response, err error) error {
	if err == nil || resp == nil || resp.Response == nil {
		return err
	}
	return &settingsStatusError{status: resp.StatusCode, err: err}
}

func convertGiteaAccessMode(mode string) string {
	if mode == "owner" {
		return "admin"
	}
	return mode
}

func convertGiteaBranchProtection(source *gitea_sdk.BranchProtection) *base.BranchProtection {
	protection := &base.BranchProtection{
		Pattern:                 source.RuleName,
		BlockPush:               !source.EnablePush || source.EnablePushWhitelist,
		AllowForcePush:          source.EnableForcePush && !source.EnableForcePushAllowlist,
		RequiredApprovals:       source.RequiredApprovals,
		BlockOnRejectedReviews:  source.BlockOnRejectedReviews,
		BlockOnReviewRequests:   source.BlockOnOfficialReviewRequests,
		DismissStaleApprovals:   source.DismissStaleApprovals,
		IgnoreStaleApprovals:    source.IgnoreStaleApprovals,
		EnableStatusCheck:       source.EnableStatusCheck,
		StatusCheckContexts:     source.StatusCheckContexts,
		RequireUpToDateBranch:   source.BlockOnOutdatedBranch,
		RequireSignedCommits:    source.RequireSignedCommits,
		EnforceForAdmins:        source.BlockAdminMergeOverride,
		ProtectedFilePatterns:   source.ProtectedFilePatterns,
		UnprotectedFilePatterns: source.UnprotectedFilePatterns,
	}
	if protection.Pattern == "" {
		protection.Pattern = source.BranchName // servers before rule names
	}
	// the lists name people and teams of the source, without them the rule is kept at its strictest where it can be
	for _, list := range []struct {
		name    string
		enabled bool
	}{
		{"push allow-list (direct pushes are off instead)", source.EnablePush && source.EnablePushWhitelist},
		{"force push allow-list (force pushes are off instead)", source.EnableForcePush && source.EnableForcePushAllowlist},
		{"merge allow-list (anyone with write access can merge)", source.EnableMergeWhitelist},
		{"approval allow-list (any reviewer with write access counts)", source.EnableApprovalsWhitelist},
	} {
		if list.enabled {
			protection.Unsupported = append(protection.Unsupported, list.name)
		}
	}
	return protection
}

func convertGiteaWebhook(hook *gitea_sdk.Hook) *base.Webhook {
	webhook := &base.Webhook{URL: hook.Config["url"], ContentType: hook.Config["content_type"], BranchFilter: hook.BranchFilter}
	known := append(webhook_module.AllEvents(), webhook_module.HookEventPullRequestReview)
	for _, event := range hook.Events {
		if slices.Contains(known, webhook_module.HookEventType(event)) {
			webhook.Events = append(webhook.Events, event)
		} else {
			webhook.UnsupportedEvents = append(webhook.UnsupportedEvents, event)
		}
	}
	return webhook
}
