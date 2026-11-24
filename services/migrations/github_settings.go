// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package migrations

import (
	"context"
	"fmt"
	"strconv"

	"gitea.dev/modules/glob"
	base "gitea.dev/modules/migration"
	webhook_module "gitea.dev/modules/webhook"

	"github.com/google/go-github/v93/github"
)

const githubActionsAppSlug = "github-actions"

// GetSettings reads the settings the token may see and notes the rest, so a missing permission never fails a migration
func (g *GithubGraphQLDownloader) GetSettings(ctx context.Context) (*base.RepoSettings, error) {
	g.waitAndPickClient(ctx)
	repo, resp, err := g.getClient().Repositories.Get(ctx, g.repoOwner, g.repoName)
	g.setRateFromResponse(resp)
	if err != nil {
		return nil, err
	}

	settings := &base.RepoSettings{}
	if !repo.GetPermissions().GetAdmin() {
		settings.Notes = append(settings.Notes, base.SettingsNote{Kind: base.SettingsNoteNotAdmin})
	}

	rules, err := g.getBranchProtectionRules(ctx)
	for _, rule := range readableSettings(settings, "branch protection", rules, err) {
		settings.BranchProtections = append(settings.BranchProtections, convertGithubBranchProtection(rule))
	}

	rulesets, err := githubListAll(ctx, g.GithubDownloaderV3, func(opts *github.ListOptions) ([]*github.RepositoryRuleset, *github.Response, error) {
		return g.getClient().Repositories.GetAllRulesets(ctx, g.repoOwner, g.repoName, &github.RepositoryListRulesetsOptions{ListOptions: *opts})
	})
	for _, ruleset := range readableSettings(settings, "rulesets", rulesets, err) {
		settings.Notes = append(settings.Notes, base.SettingsNote{Kind: base.SettingsNoteRuleset, Name: ruleset.Name})
	}

	collaborators, err := githubListAll(ctx, g.GithubDownloaderV3, func(opts *github.ListOptions) ([]*github.User, *github.Response, error) {
		return g.getClient().Repositories.ListCollaborators(ctx, g.repoOwner, g.repoName, &github.ListCollaboratorsOptions{Affiliation: "direct", ListOptions: *opts})
	})
	for _, collaborator := range readableSettings(settings, "collaborators", collaborators, err) {
		settings.Collaborators = append(settings.Collaborators, &base.Collaborator{
			ExternalID: strconv.FormatInt(collaborator.GetID(), 10),
			Login:      collaborator.GetLogin(),
			Permission: convertGithubPermission(collaborator.GetPermissions().GetAdmin(), collaborator.GetPermissions().GetPush()),
		})
	}

	isOrg := repo.GetOwner().GetType() == "Organization"
	if isOrg {
		teams, err := githubListAll(ctx, g.GithubDownloaderV3, func(opts *github.ListOptions) ([]*github.Team, *github.Response, error) {
			return g.getClient().Repositories.ListTeams(ctx, g.repoOwner, g.repoName, opts)
		})
		for _, team := range readableSettings(settings, "teams", teams, err) {
			permission := team.GetPermission()
			settings.Teams = append(settings.Teams, &base.Team{
				Name:       team.GetName(),
				Slug:       team.GetSlug(),
				Permission: convertGithubPermission(permission == "admin", permission == "push" || permission == "maintain"),
			})
		}
	}

	hooks, err := githubListAll(ctx, g.GithubDownloaderV3, func(opts *github.ListOptions) ([]*github.Hook, *github.Response, error) {
		return g.getClient().Repositories.ListHooks(ctx, g.repoOwner, g.repoName, opts)
	})
	for _, hook := range readableSettings(settings, "webhooks", hooks, err) {
		if hook.GetName() == "web" { // anything else is a long retired GitHub service
			settings.Webhooks = append(settings.Webhooks, convertGithubWebhook(hook))
		}
	}

	variables, err := githubListAll(ctx, g.GithubDownloaderV3, func(opts *github.ListOptions) ([]*github.ActionsVariable, *github.Response, error) {
		page, resp, err := g.getClient().Actions.ListRepoVariables(ctx, g.repoOwner, g.repoName, opts)
		return page.GetVariables(), resp, err
	})
	for _, variable := range readableSettings(settings, "Actions variables", variables, err) {
		settings.Variables = append(settings.Variables, &base.ActionsVariable{Name: variable.Name, Value: variable.Value})
	}

	secrets, err := githubListAll(ctx, g.GithubDownloaderV3, func(opts *github.ListOptions) ([]*github.Secret, *github.Response, error) {
		page, resp, err := g.getClient().Actions.ListRepoSecrets(ctx, g.repoOwner, g.repoName, opts)
		return page.GetSecrets(), resp, err
	})
	for _, secret := range readableSettings(settings, "Actions secrets", secrets, err) {
		settings.Notes = append(settings.Notes, base.SettingsNote{Kind: base.SettingsNoteSecret, Name: secret.Name})
	}

	if isOrg {
		orgSecrets, err := githubListAll(ctx, g.GithubDownloaderV3, func(opts *github.ListOptions) ([]*github.Secret, *github.Response, error) {
			page, resp, err := g.getClient().Actions.ListRepoOrgSecrets(ctx, g.repoOwner, g.repoName, opts)
			return page.GetSecrets(), resp, err
		})
		for _, secret := range readableSettings(settings, "organization Actions secrets", orgSecrets, err) {
			settings.Notes = append(settings.Notes, base.SettingsNote{Kind: base.SettingsNoteOrgSecret, Name: secret.Name})
		}

		orgVariables, err := githubListAll(ctx, g.GithubDownloaderV3, func(opts *github.ListOptions) ([]*github.ActionsVariable, *github.Response, error) {
			page, resp, err := g.getClient().Actions.ListRepoOrgVariables(ctx, g.repoOwner, g.repoName, opts)
			return page.GetVariables(), resp, err
		})
		for _, variable := range readableSettings(settings, "organization Actions variables", orgVariables, err) {
			settings.Notes = append(settings.Notes, base.SettingsNote{Kind: base.SettingsNoteOrgVariable, Name: variable.Name})
		}
	}

	environments, err := githubListAll(ctx, g.GithubDownloaderV3, func(opts *github.ListOptions) ([]*github.Environment, *github.Response, error) {
		page, resp, err := g.getClient().Repositories.ListEnvironments(ctx, g.repoOwner, g.repoName, &github.EnvironmentListOptions{ListOptions: *opts})
		return page.GetEnvironments(), resp, err
	})
	for _, environment := range readableSettings(settings, "environments", environments, err) {
		settings.Notes = append(settings.Notes, base.SettingsNote{Kind: base.SettingsNoteEnvironment, Name: environment.GetName()})
	}

	return settings, ctx.Err()
}

func githubListAll[T any](ctx context.Context, g *GithubDownloaderV3, list func(opts *github.ListOptions) ([]T, *github.Response, error)) ([]T, error) {
	var all []T
	opts := &github.ListOptions{PerPage: g.maxPerPage}
	for {
		g.waitAndPickClient(ctx)
		items, resp, err := list(opts)
		g.setRateFromResponse(resp)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if resp.NextPage == 0 {
			return all, nil
		}
		opts.Page = resp.NextPage
	}
}

func convertGithubPermission(admin, write bool) string {
	switch {
	case admin:
		return "admin"
	case write:
		return "write"
	}
	return "read"
}

type gqlBranchProtectionRule struct {
	Pattern                      string `json:"pattern"`
	AllowsDeletions              bool   `json:"allowsDeletions"`
	AllowsForcePushes            bool   `json:"allowsForcePushes"`
	DismissesStaleReviews        bool   `json:"dismissesStaleReviews"`
	IsAdminEnforced              bool   `json:"isAdminEnforced"`
	LockBranch                   bool   `json:"lockBranch"`
	RequireLastPushApproval      bool   `json:"requireLastPushApproval"`
	RequiredApprovingReviewCount int64  `json:"requiredApprovingReviewCount"`
	RequiredStatusChecks         []struct {
		Context string `json:"context"`
		App     *struct {
			Slug string `json:"slug"`
		} `json:"app"`
	} `json:"requiredStatusChecks"`
	RequiresApprovingReviews       bool `json:"requiresApprovingReviews"`
	RequiresCodeOwnerReviews       bool `json:"requiresCodeOwnerReviews"`
	RequiresCommitSignatures       bool `json:"requiresCommitSignatures"`
	RequiresConversationResolution bool `json:"requiresConversationResolution"`
	RequiresDeployments            bool `json:"requiresDeployments"`
	RequiresLinearHistory          bool `json:"requiresLinearHistory"`
	RequiresStatusChecks           bool `json:"requiresStatusChecks"`
	RequiresStrictStatusChecks     bool `json:"requiresStrictStatusChecks"`
	RestrictsPushes                bool `json:"restrictsPushes"`
	RestrictsReviewDismissals      bool `json:"restrictsReviewDismissals"`
}

// REST only lists protection per existing branch, GraphQL returns the rules with their patterns
func (g *GithubGraphQLDownloader) getBranchProtectionRules(ctx context.Context) ([]*gqlBranchProtectionRule, error) {
	const query = `query($owner:String!,$name:String!){repository(owner:$owner,name:$name){branchProtectionRules(first:100){nodes{
pattern allowsDeletions allowsForcePushes dismissesStaleReviews isAdminEnforced lockBranch requireLastPushApproval requiredApprovingReviewCount
requiredStatusChecks{context app{slug}} requiresApprovingReviews requiresCodeOwnerReviews requiresCommitSignatures requiresConversationResolution
requiresDeployments requiresLinearHistory requiresStatusChecks requiresStrictStatusChecks restrictsPushes restrictsReviewDismissals}}}}`
	var out struct {
		Repository struct {
			BranchProtectionRules struct {
				Nodes []*gqlBranchProtectionRule `json:"nodes"`
			} `json:"branchProtectionRules"`
		} `json:"repository"`
	}
	if err := g.doGraphQL(ctx, query, map[string]any{"owner": g.repoOwner, "name": g.repoName}, &out); err != nil {
		return nil, err
	}
	return out.Repository.BranchProtectionRules.Nodes, nil
}

func convertGithubBranchProtection(rule *gqlBranchProtectionRule) *base.BranchProtection {
	protection := &base.BranchProtection{
		Pattern:                 rule.Pattern,
		BlockPush:               rule.RequiresApprovingReviews || rule.RestrictsPushes || rule.LockBranch,
		AllowForcePush:          rule.AllowsForcePushes,
		AllowDeletion:           rule.AllowsDeletions,
		RequiredApprovals:       rule.RequiredApprovingReviewCount,
		BlockOnRejectedReviews:  rule.RequiredApprovingReviewCount > 0,
		DismissStaleApprovals:   rule.DismissesStaleReviews,
		RequireCodeOwnerReviews: rule.RequiresCodeOwnerReviews,
		EnableStatusCheck:       rule.RequiresStatusChecks && len(rule.RequiredStatusChecks) > 0,
		RequireUpToDateBranch:   rule.RequiresStatusChecks && rule.RequiresStrictStatusChecks,
		RequireSignedCommits:    rule.RequiresCommitSignatures,
		EnforceForAdmins:        rule.IsAdminEnforced,
	}
	if protection.EnableStatusCheck {
		for _, check := range rule.RequiredStatusChecks {
			if check.App != nil && check.App.Slug == githubActionsAppSlug {
				// GitHub names an Actions check after its job, here it is "workflow / job (event)"
				protection.StatusCheckContexts = append(protection.StatusCheckContexts, "* / "+glob.QuoteMeta(check.Context)+" (*)")
				continue
			}
			protection.StatusCheckContexts = append(protection.StatusCheckContexts, glob.QuoteMeta(check.Context))
			protection.Unsupported = append(protection.Unsupported, fmt.Sprintf("required check %q is not from GitHub Actions and nothing reports it here yet", check.Context))
		}
	}
	for _, setting := range []struct {
		name    string
		enabled bool
	}{
		{"lock branch (direct pushes are off instead)", rule.LockBranch},
		{"restrict who can push (direct pushes are off and the list was not copied)", rule.RestrictsPushes},
		{"require approval of the most recent push", rule.RequireLastPushApproval},
		{"require conversation resolution", rule.RequiresConversationResolution},
		{"require deployments to succeed", rule.RequiresDeployments},
		{"require linear history", rule.RequiresLinearHistory},
		{"restrict who can dismiss reviews", rule.RestrictsReviewDismissals},
	} {
		if setting.enabled {
			protection.Unsupported = append(protection.Unsupported, setting.name)
		}
	}
	return protection
}

var githubWebhookEvents = map[string][]webhook_module.HookEventType{
	"create":                      {webhook_module.HookEventCreate},
	"delete":                      {webhook_module.HookEventDelete},
	"fork":                        {webhook_module.HookEventFork},
	"push":                        {webhook_module.HookEventPush},
	"issues":                      {webhook_module.HookEventIssues, webhook_module.HookEventIssueAssign, webhook_module.HookEventIssueLabel, webhook_module.HookEventIssueMilestone},
	"issue_comment":               {webhook_module.HookEventIssueComment, webhook_module.HookEventPullRequestComment},
	"pull_request":                {webhook_module.HookEventPullRequest, webhook_module.HookEventPullRequestAssign, webhook_module.HookEventPullRequestLabel, webhook_module.HookEventPullRequestMilestone, webhook_module.HookEventPullRequestSync, webhook_module.HookEventPullRequestReviewRequest},
	"pull_request_review":         {webhook_module.HookEventPullRequestReview},
	"pull_request_review_comment": {webhook_module.HookEventPullRequestReview},
	"gollum":                      {webhook_module.HookEventWiki},
	"repository":                  {webhook_module.HookEventRepository},
	"release":                     {webhook_module.HookEventRelease},
	"package":                     {webhook_module.HookEventPackage},
	"registry_package":            {webhook_module.HookEventPackage},
	"status":                      {webhook_module.HookEventStatus},
	"workflow_run":                {webhook_module.HookEventWorkflowRun},
	"workflow_job":                {webhook_module.HookEventWorkflowJob},
}

func convertGithubWebhook(hook *github.Hook) *base.Webhook {
	webhook := &base.Webhook{
		URL:         hook.GetConfig().GetURL(),
		ContentType: hook.GetConfig().GetContentType(),
	}
	for _, event := range hook.Events {
		switch mapped, ok := githubWebhookEvents[event]; {
		case event == "*":
			webhook.SendEverything = true
		case ok:
			for _, name := range mapped {
				webhook.Events = append(webhook.Events, string(name))
			}
		default:
			webhook.UnsupportedEvents = append(webhook.UnsupportedEvents, event)
		}
	}
	return webhook
}
