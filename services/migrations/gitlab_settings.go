// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package migrations

import (
	"context"
	"errors"
	"net/http"
	"path"
	"slices"
	"strconv"
	"strings"

	base "gitea.dev/modules/migration"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

// GetSettings reads the settings the token may see and notes the rest
func (g *GitlabDownloader) GetSettings(ctx context.Context) (*base.RepoSettings, error) {
	project, _, err := g.client.Projects.GetProject(g.repoID, nil, nil, gitlab.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	settings := &base.RepoSettings{}
	if gitlabOwnAccess(project) < gitlab.MaintainerPermissions {
		settings.Notes = append(settings.Notes, base.SettingsNote{Kind: base.SettingsNoteNotAdmin})
	}

	approvalRules, err := gitlabListAll(g, func(page gitlab.ListOptions) ([]*gitlab.ProjectApprovalRule, *gitlab.Response, error) {
		return g.client.Projects.GetProjectApprovalRules(g.repoID, &gitlab.GetProjectApprovalRulesListsOptions{ListOptions: page}, gitlab.WithContext(ctx))
	})
	if failed, ok := errors.AsType[*settingsStatusError](err); ok && failed.status == http.StatusNotFound {
		err = nil // approval rules are a paid feature, without it there is nothing to copy
	}
	approvalRules = readableSettings(settings, "merge request approval rules", approvalRules, err)

	branches, err := gitlabListAll(g, func(page gitlab.ListOptions) ([]*gitlab.ProtectedBranch, *gitlab.Response, error) {
		return g.client.ProtectedBranches.ListProtectedBranches(g.repoID, &gitlab.ListProtectedBranchesOptions{ListOptions: page}, gitlab.WithContext(ctx))
	})
	for _, branch := range readableSettings(settings, "protected branches", branches, err) {
		settings.BranchProtections = append(settings.BranchProtections, convertGitlabProtectedBranch(project, branch, approvalRules))
	}

	members, err := gitlabListAll(g, func(page gitlab.ListOptions) ([]*gitlab.ProjectMember, *gitlab.Response, error) {
		return g.client.ProjectMembers.ListProjectMembers(g.repoID, &gitlab.ListProjectMembersOptions{ListOptions: page}, gitlab.WithContext(ctx))
	})
	for _, member := range readableSettings(settings, "members", members, err) {
		permission := convertGitlabAccessLevel(int64(member.AccessLevel))
		if permission == "none" {
			settings.Notes = append(settings.Notes, base.SettingsNote{Kind: base.SettingsNoteCollaboratorRole, Name: member.Username})
			continue
		}
		settings.Collaborators = append(settings.Collaborators, &base.Collaborator{ExternalID: strconv.FormatInt(member.ID, 10), Login: member.Username, Permission: permission})
	}

	for _, group := range project.SharedWithGroups {
		settings.Teams = append(settings.Teams, &base.Team{Name: group.GroupName, Slug: path.Base(group.GroupFullPath), Permission: convertGitlabAccessLevel(group.GroupAccessLevel)})
	}

	hooks, err := gitlabListAll(g, func(page gitlab.ListOptions) ([]*gitlab.ProjectHook, *gitlab.Response, error) {
		return g.client.Projects.ListProjectHooks(g.repoID, &gitlab.ListProjectHooksOptions{ListOptions: page}, gitlab.WithContext(ctx))
	})
	for _, hook := range readableSettings(settings, "webhooks", hooks, err) {
		settings.Notes = append(settings.Notes, base.SettingsNote{Kind: base.SettingsNoteWebhookFormat, Name: webhookHost(hook.URL)})
	}

	variables, err := gitlabListAll(g, func(page gitlab.ListOptions) ([]*gitlab.ProjectVariable, *gitlab.Response, error) {
		return g.client.ProjectVariables.ListVariables(g.repoID, &gitlab.ListProjectVariablesOptions{ListOptions: page}, gitlab.WithContext(ctx))
	})
	for _, variable := range readableSettings(settings, "CI/CD variables", variables, err) {
		switch {
		case variable.EnvironmentScope != "" && variable.EnvironmentScope != "*":
			settings.Notes = append(settings.Notes, base.SettingsNote{Kind: base.SettingsNoteEnvironment, Name: variable.EnvironmentScope, Detail: variable.Key})
		case variable.Hidden: // GitLab never shows these again either
			settings.Notes = append(settings.Notes, base.SettingsNote{Kind: base.SettingsNoteSecret, Name: variable.Key})
		case variable.Masked || variable.Protected:
			settings.Secrets = append(settings.Secrets, &base.ActionsSecret{Name: variable.Key, Value: variable.Value})
		default:
			settings.Variables = append(settings.Variables, &base.ActionsVariable{Name: variable.Key, Value: variable.Value})
		}
	}

	return settings, ctx.Err()
}

func gitlabListAll[T any](g *GitlabDownloader, list func(page gitlab.ListOptions) ([]T, *gitlab.Response, error)) ([]T, error) {
	var all []T
	for page := int64(1); page <= maxSettingsPages; page++ {
		items, resp, err := list(gitlab.ListOptions{Page: page, PerPage: int64(g.maxPerPage)})
		if err != nil {
			if resp != nil && resp.Response != nil {
				return nil, &settingsStatusError{status: resp.StatusCode, err: err}
			}
			return nil, err
		}
		all = append(all, items...)
		if len(items) < g.maxPerPage {
			return all, nil
		}
	}
	return all, errSettingsPageLimit
}

func gitlabOwnAccess(project *gitlab.Project) gitlab.AccessLevelValue {
	var access gitlab.AccessLevelValue
	if project.Permissions != nil {
		if project.Permissions.ProjectAccess != nil {
			access = project.Permissions.ProjectAccess.AccessLevel
		}
		if project.Permissions.GroupAccess != nil {
			access = max(access, project.Permissions.GroupAccess.AccessLevel)
		}
	}
	return access
}

// convertGitlabAccessLevel leaves guests and planners out, on GitLab they can not read the code of a private project
func convertGitlabAccessLevel(level int64) string {
	switch {
	case level >= int64(gitlab.MaintainerPermissions):
		return "admin"
	case level >= int64(gitlab.DeveloperPermissions):
		return "write"
	case level >= int64(gitlab.ReporterPermissions):
		return "read"
	}
	return "none"
}

// gitlabDevelopersAllowed reports whether a plain role level lets Developers in, the alternative being Maintainers, named people or no one
func gitlabDevelopersAllowed(levels []*gitlab.BranchAccessDescription) bool {
	return slices.ContainsFunc(levels, func(level *gitlab.BranchAccessDescription) bool {
		return level.UserID == 0 && level.GroupID == 0 && level.DeployKeyID == 0 && level.AccessLevel == gitlab.DeveloperPermissions
	})
}

func convertGitlabProtectedBranch(project *gitlab.Project, branch *gitlab.ProtectedBranch, approvalRules []*gitlab.ProjectApprovalRule) *base.BranchProtection {
	developersPush := gitlabDevelopersAllowed(branch.PushAccessLevels)
	protection := &base.BranchProtection{
		Pattern:                 strings.ReplaceAll(branch.Name, "*", "**"), // a GitLab wildcard also matches "/"
		BlockPush:               !developersPush,
		AllowForcePush:          branch.AllowForcePush,
		RequireCodeOwnerReviews: branch.CodeOwnerApprovalRequired,
	}
	for _, rule := range approvalRules {
		applies := rule.AppliesToAllProtectedBranches || len(rule.ProtectedBranches) == 0 ||
			slices.ContainsFunc(rule.ProtectedBranches, func(protected *gitlab.ProtectedBranch) bool { return protected.Name == branch.Name })
		if applies && rule.RuleType != "report_approver" {
			protection.RequiredApprovals = max(protection.RequiredApprovals, rule.ApprovalsRequired)
		}
	}

	noOnePushes := !slices.ContainsFunc(branch.PushAccessLevels, func(level *gitlab.BranchAccessDescription) bool { return level.AccessLevel != gitlab.NoPermissions })
	for _, setting := range []struct {
		name    string
		enabled bool
	}{
		{"only Maintainers or named people could push (direct pushes are off instead)", !developersPush && !noOnePushes},
		{"only Maintainers or named people could merge (anyone with write access can merge)", !gitlabDevelopersAllowed(branch.MergeAccessLevels)},
		{"pipelines must succeed (no check is required here until you pick one)", project.OnlyAllowMergeIfPipelineSucceeds},
		{"all threads must be resolved", project.OnlyAllowMergeIfAllDiscussionsAreResolved},
	} {
		if setting.enabled {
			protection.Unsupported = append(protection.Unsupported, setting.name)
		}
	}
	return protection
}
