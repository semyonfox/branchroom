// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package inbox

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"gitea.dev/models/db"
	issues_model "gitea.dev/models/issues"
	perm_model "gitea.dev/models/perm"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unit"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/cache"
	api "gitea.dev/modules/structs"
	pull_service "gitea.dev/services/pull"

	"xorm.io/builder"
)

func maintainedRepos(doer *user_model.User) builder.Cond {
	return builder.In("issue.repo_id", maintainedRepoIDs(doer))
}

func maintainedRepoIDs(doer *user_model.User) *builder.Builder {
	cond := builder.NewCond()
	if !doer.IsAdmin {
		codeRepos := builder.Select("repo_id").From("repo_unit").Where(builder.Eq{"type": unit.TypeCode})
		teams := builder.Select("team_repo.repo_id").From("team_repo").
			InnerJoin("team_user", "team_user.team_id = team_repo.team_id").
			InnerJoin("team", "team.id = team_repo.team_id").
			LeftJoin("team_unit", builder.Expr("team_unit.team_id = team.id AND team_unit.type = ?", unit.TypeCode)).
			Where(builder.Eq{"team_user.uid": doer.ID}.And(builder.Or(builder.Gte{"team.authorize": perm_model.AccessModeAdmin}, builder.Or(builder.Gte{"team.authorize": perm_model.AccessModeWrite}, builder.Gte{"team_unit.access_mode": perm_model.AccessModeWrite}).And(builder.In("team_repo.repo_id", codeRepos)))))
		collaborators := builder.Select("repo_id").From("collaboration").Where(builder.Eq{"user_id": doer.ID}.And(builder.Or(builder.Gte{"mode": perm_model.AccessModeAdmin}, builder.Gte{"mode": perm_model.AccessModeWrite}.And(builder.In("repo_id", codeRepos)))))
		cond = builder.Or(builder.Eq{"owner_id": doer.ID}, builder.In("id", collaborators), builder.In("id", teams))
	}
	return builder.Select("id").From("repository").Where(cond)
}

func GetViews(ctx context.Context, doer *user_model.User) (*api.Inbox, error) {
	result, err := get(ctx, doer, []string{"decision", "blocked", "triage", "yours", "agents", "mentions", "snoozed", "done"})
	if err != nil {
		return nil, err
	}
	result.Groups = append([]api.InboxGroup{needsGroup(result.Groups[:2])}, result.Groups[2:]...)
	result.CanTriage, err = canTriage(ctx, doer)
	return result, err
}

func needsCountKey(userID int64) string {
	return fmt.Sprintf("inbox-needs-count/%d", userID)
}

// GetNeedsCount counts what needs the user. It runs the whole inbox, so the header reads a copy up to a minute old,
// cleared whenever the user changes inbox state
func GetNeedsCount(ctx context.Context, doer *user_model.User) (int, error) {
	var count int
	if hit, _ := cache.GetCache().GetJSON(needsCountKey(doer.ID), &count); hit {
		return count, nil
	}
	result, err := get(ctx, doer, []string{"decision", "blocked"})
	if err != nil {
		return 0, err
	}
	count = needsGroup(result.Groups).Count
	_ = cache.GetCache().PutJSON(needsCountKey(doer.ID), count, 60)
	return count, nil
}

func needsGroup(groups []api.InboxGroup) api.InboxGroup {
	needs := api.InboxGroup{Kind: "needs", Items: []api.InboxItem{}}
	seen := map[int64]bool{}
	for _, group := range groups {
		for _, item := range group.Items {
			if !seen[item.ID] && len(needs.Items) < groupLimit {
				needs.Items = append(needs.Items, item)
				seen[item.ID] = true
			}
		}
	}
	needs.Count = len(needs.Items)
	return needs
}

func canTriage(ctx context.Context, doer *user_model.User) (bool, error) {
	return db.GetEngine(ctx).Where(builder.In("id", maintainedRepoIDs(doer))).Exist(new(repo_model.Repository))
}

func triageDetails(ctx context.Context, issue *issues_model.Issue, item *api.InboxItem, readiness *pull_service.Readiness) error {
	trust, err := posterTrust(ctx, issue)
	if err != nil {
		return err
	}
	item.Trust = trust
	if !issue.IsPull {
		// issues have no merged-PR history to judge by
		if trust == "first-time contributor" {
			item.Trust = "contributor"
		}
		return nil
	}
	if readiness != nil {
		item.Size, err = pullSize(ctx, issue)
	}
	return err
}

func sizeLabel(lines int) string {
	switch {
	case lines < 10:
		return "XS"
	case lines < 30:
		return "S"
	case lines < 100:
		return "M"
	case lines < 500:
		return "L"
	default:
		return "XL"
	}
}

func triageReason(ctx context.Context, issueID int64) (string, error) {
	var reviewers []struct{ Name string }
	err := db.GetEngine(ctx).Table("review").Select("`user`.name").
		Join("INNER", "user", "`user`.id = review.reviewer_id").
		Where(builder.Eq{"review.issue_id": issueID, "review.type": issues_model.ReviewTypeRequest}).
		OrderBy("`user`.name").Find(&reviewers)
	if err != nil {
		return "", err
	}
	var names []string
	for _, reviewer := range reviewers {
		names = append(names, reviewer.Name)
	}
	var teams []struct{ Name string }
	err = db.GetEngine(ctx).Table("review").Select("team.name").
		Join("INNER", "team", "team.id = review.reviewer_team_id").
		Where(builder.Eq{"review.issue_id": issueID, "review.type": issues_model.ReviewTypeRequest}).
		OrderBy("team.name").Find(&teams)
	if err != nil {
		return "", err
	}
	for _, team := range teams {
		names = append(names, team.Name)
	}
	if len(names) > 0 {
		return "Review requested from " + strings.Join(names, ", ") + ".", nil
	}
	return "No assignee or review yet.", nil
}

// QueueCookie holds the inbox view being worked through, so pull request pages can show where you are in it
const QueueCookie = "inbox_queue"

// Queue returns one view's items in inbox order, false when the view does not exist for the user
func Queue(ctx context.Context, doer *user_model.User, view string) ([]api.InboxItem, bool, error) {
	result, err := GetViews(ctx, doer)
	if err != nil {
		return nil, false, err
	}
	for _, group := range result.Groups {
		if group.Kind == view && (view != "triage" || result.CanTriage) {
			return group.Items, true, nil
		}
	}
	return nil, false, nil
}

// QueueOrder puts skipped items behind the rest, so skipping sends an item to the back of the queue
func QueueOrder(items []api.InboxItem, skipped []int64) []api.InboxItem {
	ordered := make([]api.InboxItem, 0, len(items))
	for _, item := range items {
		if !slices.Contains(skipped, item.ID) {
			ordered = append(ordered, item)
		}
	}
	for _, id := range skipped {
		if i := slices.IndexFunc(items, func(item api.InboxItem) bool { return item.ID == id }); i >= 0 {
			ordered = append(ordered, items[i])
		}
	}
	return ordered
}

// ParseQueueCookie reads "view-skippedID-skippedID"
func ParseQueueCookie(value string) (view string, skipped []int64) {
	parts := strings.Split(value, "-")
	for _, part := range parts[1:] {
		if id, err := strconv.ParseInt(part, 10, 64); err == nil && id > 0 && len(skipped) < 50 {
			skipped = append(skipped, id)
		}
	}
	return parts[0], skipped
}

func FormatQueueCookie(view string, skipped []int64) string {
	var value strings.Builder
	value.WriteString(view)
	for _, id := range skipped {
		value.WriteByte('-')
		value.WriteString(strconv.FormatInt(id, 10))
	}
	return value.String()
}
