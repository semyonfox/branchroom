// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package migrations

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	base "gitea.dev/modules/migration"
)

type bitbucketBranchRestriction struct {
	Kind            string          `json:"kind"`
	BranchMatchKind string          `json:"branch_match_kind"`
	BranchType      string          `json:"branch_type"`
	Pattern         string          `json:"pattern"`
	Value           int64           `json:"value"`
	Users           []bitbucketUser `json:"users"`
	Groups          []struct{}      `json:"groups"`
}

type bitbucketPermission struct {
	Permission string        `json:"permission"`
	User       bitbucketUser `json:"user"`
	Group      struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"group"`
}

type bitbucketHook struct {
	URL string `json:"url"`
}

type bitbucketVariable struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Secured bool   `json:"secured"`
}

// GetSettings reads the settings the token may see and notes the rest
func (b *BitbucketDownloader) GetSettings(ctx context.Context) (*base.RepoSettings, error) {
	settings := &base.RepoSettings{}

	restrictions, err := bitbucketListAll[bitbucketBranchRestriction](ctx, b, "/branch-restrictions")
	settings.BranchProtections, settings.Notes = convertBitbucketBranchRestrictions(readableSettings(settings, "branch restrictions", restrictions, err), settings.Notes)

	users, err := bitbucketListAll[bitbucketPermission](ctx, b, "/permissions-config/users")
	for _, user := range readableSettings(settings, "user permissions", users, err) {
		settings.Collaborators = append(settings.Collaborators, &base.Collaborator{ExternalID: user.User.UUID, Login: bitbucketUserName(user.User), Permission: user.Permission})
	}

	groups, err := bitbucketListAll[bitbucketPermission](ctx, b, "/permissions-config/groups")
	for _, group := range readableSettings(settings, "group permissions", groups, err) {
		settings.Teams = append(settings.Teams, &base.Team{Name: group.Group.Name, Slug: group.Group.Slug, Permission: group.Permission})
	}

	hooks, err := bitbucketListAll[bitbucketHook](ctx, b, "/hooks")
	for _, hook := range readableSettings(settings, "webhooks", hooks, err) {
		settings.Notes = append(settings.Notes, base.SettingsNote{Kind: base.SettingsNoteWebhookFormat, Name: webhookHost(hook.URL)})
	}

	variables, err := bitbucketListAll[bitbucketVariable](ctx, b, "/pipelines_config/variables")
	for _, variable := range readableSettings(settings, "pipeline variables", variables, err) {
		if variable.Secured {
			settings.Notes = append(settings.Notes, base.SettingsNote{Kind: base.SettingsNoteSecret, Name: variable.Key})
		} else {
			settings.Variables = append(settings.Variables, &base.ActionsVariable{Name: variable.Key, Value: variable.Value})
		}
	}

	return settings, ctx.Err()
}

func bitbucketListAll[T any](ctx context.Context, b *BitbucketDownloader, path string) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		var resp bitbucketPage[T]
		if err := b.doAPI(ctx, path, url.Values{"pagelen": {"100"}, "page": {strconv.Itoa(page)}}, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Values...)
		if resp.Next == "" {
			return all, nil
		}
	}
}

// convertBitbucketBranchRestrictions folds Bitbucket's one-restriction-per-kind list into one rule per pattern
func convertBitbucketBranchRestrictions(restrictions []bitbucketBranchRestriction, notes []base.SettingsNote) ([]*base.BranchProtection, []base.SettingsNote) {
	var protections []*base.BranchProtection
	byPattern := map[string]*base.BranchProtection{}
	branchTypes := map[string]bool{}
	for _, restriction := range restrictions {
		if restriction.BranchMatchKind != "glob" {
			// these follow the branching model, which names its branches somewhere else
			if !branchTypes[restriction.BranchType] {
				branchTypes[restriction.BranchType] = true
				notes = append(notes, base.SettingsNote{Kind: base.SettingsNoteRuleset, Name: "branching model: " + restriction.BranchType})
			}
			continue
		}
		protection := byPattern[restriction.Pattern]
		if protection == nil {
			// a Bitbucket wildcard also matches "/". what no restriction forbids stays allowed
			protection = &base.BranchProtection{Pattern: strings.ReplaceAll(restriction.Pattern, "*", "**"), AllowForcePush: true, AllowDeletion: true}
			byPattern[restriction.Pattern] = protection
			protections = append(protections, protection)
		}
		named := len(restriction.Users)+len(restriction.Groups) > 0
		switch restriction.Kind {
		case "push":
			protection.BlockPush = true
			if named {
				protection.Unsupported = append(protection.Unsupported, "only named people and groups could push (direct pushes are off instead)")
			}
		case "force":
			protection.AllowForcePush = false
		case "delete":
			protection.AllowDeletion = false
		case "require_approvals_to_merge":
			protection.RequiredApprovals = restriction.Value
		case "require_no_changes_requested":
			protection.BlockOnRejectedReviews = true
		case "reset_pullrequest_approvals_on_change":
			protection.DismissStaleApprovals = true
		case "restrict_merges":
			protection.Unsupported = append(protection.Unsupported, "only named people and groups could merge (anyone with write access can merge)")
		case "require_passing_builds_to_merge":
			protection.Unsupported = append(protection.Unsupported, fmt.Sprintf("%d builds must pass (no check is required here until you pick one)", restriction.Value))
		case "enforce_merge_checks", "allow_auto_merge_when_builds_pass": // not rules of their own
		default:
			protection.Unsupported = append(protection.Unsupported, strings.ReplaceAll(restriction.Kind, "_", " "))
		}
	}
	return protections, notes
}
