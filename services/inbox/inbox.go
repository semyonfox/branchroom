// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package inbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	activities_model "gitea.dev/models/activities"
	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	issues_model "gitea.dev/models/issues"
	access_model "gitea.dev/models/perm/access"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unit"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/httplib"
	"gitea.dev/modules/optional"
	api "gitea.dev/modules/structs"
	pull_service "gitea.dev/services/pull"

	"xorm.io/builder"
)

const (
	groupLimit     = 25
	candidateLimit = 100
)

// Get returns recent attention items. Counts describe returned items. Each group scans
// at most 100 candidates, with readiness evaluated only for those candidate PRs.
func Get(ctx context.Context, doer *user_model.User) (*api.Inbox, error) {
	return get(ctx, doer, []string{"decision", "blocked", "agents", "mentions"})
}

func get(ctx context.Context, doer *user_model.User, kinds []string) (*api.Inbox, error) {
	result := &api.Inbox{}
	permissions := map[int64]access_model.Permission{}
	readiness := map[int64]*pull_service.Readiness{}
	since := time.Now().Add(-7 * 24 * time.Hour).Unix()
	for _, kind := range kinds {
		group := api.InboxGroup{Kind: kind, Items: []api.InboxItem{}}
		cond := builder.NewCond()
		switch kind {
		case "triage":
			cond = maintainedRepos(doer).And(builder.Gte{"issue.created_unix": time.Now().Add(-30 * 24 * time.Hour).Unix()}, builder.NotIn("issue.id", builder.Select("issue_id").From("issue_assignees")), builder.NotIn("issue.id", builder.Select("issue_id").From("review").Where(builder.In("type", issues_model.ReviewTypeApprove, issues_model.ReviewTypeReject, issues_model.ReviewTypeComment))))
		case "yours":
			cond = builder.Eq{"issue.poster_id": doer.ID}
		case "done", "snoozed":
			cond = activeState(doer.ID, kind)
		case "decision":
			teams := builder.Select("team_id").From("team_user").Where(builder.Eq{"uid": doer.ID})
			cond = builder.In("issue.id", builder.Select("issue_id").From("review").Where(builder.Eq{"type": issues_model.ReviewTypeRequest}.And(builder.Or(builder.Eq{"reviewer_id": doer.ID}, builder.In("reviewer_team_id", teams)))))
		case "blocked":
			cond = builder.Eq{"issue.poster_id": doer.ID}
		case "agents":
			cond = agentCandidates(since)
		case "mentions":
			cond = builder.In("issue.id", builder.Select("issue_id").From("notification").Where(builder.Eq{"user_id": doer.ID, "status": activities_model.NotificationStatusUnread})).And(builder.In("issue.id", builder.Select("issue_id").From("issue_user").Where(builder.Eq{"uid": doer.ID, "is_mentioned": true})))
		}
		opts := &issues_model.IssuesOptions{RepoCond: repo_model.SearchRepositoryCondition(repo_model.SearchRepoOptions{Actor: doer, Private: true, Collaborate: optional.None[bool]()}), SortType: "recentupdate", Paginator: &db.ListOptions{Page: 1, PageSize: candidateLimit}}
		if kind == "decision" || kind == "blocked" {
			opts.IsPull = optional.Some(true)
			opts.IsClosed = optional.Some(false)
			cond = cond.And(builder.In("issue.id", builder.Select("issue_id").From("pull_request").Where(builder.Eq{"has_merged": false})))
		}
		if kind == "triage" || kind == "yours" {
			opts.IsClosed = optional.Some(false)
		}
		if kind != "done" && kind != "snoozed" {
			cond = cond.And(builder.NotIn("issue.id", stateIDs(doer.ID, "")))
		}
		ids, _, err := issues_model.IssueIDs(ctx, opts, cond)
		if err != nil {
			return nil, fmt.Errorf("get: %w", err)
		}
		issues, err := issues_model.GetIssuesByIDs(ctx, ids, true)
		if err != nil {
			return nil, fmt.Errorf("get: %w", err)
		}
		if err = issues.LoadPullRequests(ctx); err != nil {
			return nil, fmt.Errorf("get: %w", err)
		}
		if _, err = issues.LoadRepositories(ctx); err != nil {
			return nil, fmt.Errorf("get: %w", err)
		}
		visible := issues[:0]
		for _, issue := range issues {
			permission, ok := permissions[issue.RepoID]
			if !ok {
				permission, err = access_model.GetDoerRepoPermission(ctx, issue.Repo, doer)
				if err != nil {
					return nil, fmt.Errorf("get: %w", err)
				}
				permissions[issue.RepoID] = permission
			}
			if (kind != "triage" || permission.IsAdmin() || permission.CanWrite(unit.TypeCode)) && permission.CanReadIssuesOrPulls(issue.IsPull) && (kind != "agents" || permission.CanWriteIssuesOrPulls(issue.IsPull)) {
				visible = append(visible, issue)
			}
		}
		issues = visible
		agents, err := loadAgents(ctx, ids, since)
		if err != nil {
			return nil, fmt.Errorf("get: %w", err)
		}
		reviewed := map[int64]bool{}
		agentReviews := map[int64]string{}
		if kind == "decision" {
			headCond := builder.NewCond()
			for _, issue := range issues {
				r, err := pull_service.GetReadiness(ctx, issue.PullRequest)
				if err != nil {
					return nil, fmt.Errorf("get: %w", err)
				}
				readiness[issue.ID] = r
				headCond = headCond.Or(builder.Eq{"issue_id": issue.ID, "commit_id": r.HeadSHA})
			}
			if len(issues) > 0 {
				var reviewedIDs []int64
				err := db.GetEngine(ctx).Table("review").Distinct("issue_id").Where(headCond).And(builder.Eq{"reviewer_id": doer.ID, "dismissed": false}).In("type", issues_model.ReviewTypeApprove, issues_model.ReviewTypeReject, issues_model.ReviewTypeComment).And(humanReview()).Find(&reviewedIDs)
				if err != nil {
					return nil, fmt.Errorf("get: %w", err)
				}
				for _, id := range reviewedIDs {
					reviewed[id] = true
				}
				var rows []struct {
					IssueID int64
					Agent   string
					Type    int
				}
				err = db.GetEngine(ctx).Table("review").Select("review.issue_id, review.type, branchroom_agent_action.agent").
					Join("INNER", "branchroom_agent_action", "branchroom_agent_action.target_id = review.id AND branchroom_agent_action.kind = 'review'").
					Where(headCond).And(builder.Eq{"reviewer_id": doer.ID, "dismissed": false}).
					In("type", issues_model.ReviewTypeApprove, issues_model.ReviewTypeReject, issues_model.ReviewTypeComment).
					OrderBy("review.id ASC").Find(&rows)
				if err != nil {
					return nil, fmt.Errorf("get: %w", err)
				}
				for _, row := range rows {
					agentReviews[row.IssueID] = "Your agent " + agentEvent(&branchroom_model.AgentAction{Agent: row.Agent, Kind: "review"}, row.Type) + ", your review is still requested."
				}
			}
		}
		for _, issue := range issues {
			item := api.InboxItem{ID: issue.ID, CreatedAt: issue.CreatedUnix.AsTime(), Kind: "issue", Repo: issue.Repo.FullName(), Index: issue.Index, Title: issue.Title, HTMLURL: issue.HTMLURL(ctx), UpdatedAt: issue.UpdatedUnix.AsTime()}
			if issue.IsPull {
				item.Kind = "pull"
			}
			item.Action = api.InboxAction{Label: "Read", URL: item.HTMLURL}
			if agent := agents[issue.ID].author; agent != nil {
				item.Agent = &api.InboxAgent{Name: agent.Agent, TaskURL: agent.TaskURL, Sponsor: agent.Sponsor, Model: agent.Model, Harness: agent.Harness}
			}
			var r *pull_service.Readiness
			if issue.IsPull && !issue.IsClosed && !issue.PullRequest.HasMerged && kind != "mentions" && kind != "done" && kind != "snoozed" {
				r = readiness[issue.ID]
				if r == nil {
					r, err = pull_service.GetReadiness(ctx, issue.PullRequest)
					if err != nil {
						return nil, fmt.Errorf("get: %w", err)
					}
					readiness[issue.ID] = r
				}
			}
			if r != nil {
				item.Checks = string(r.ChecksState)
				if item.Checks == "" {
					item.Checks = "none"
				}
			}
			if kind == "triage" {
				if err := triageDetails(ctx, issue, &item, r); err != nil {
					return nil, err
				}
			}
			switch kind {
			case "triage":
				item.Reason, err = triageReason(ctx, issue.ID)
				if err != nil {
					return nil, fmt.Errorf("get: %w", err)
				}
				if issue.IsPull {
					item.Action = api.InboxAction{Label: "Review", URL: item.HTMLURL + "/files"}
				}
			case "decision":
				if reviewed[issue.ID] || issue.PosterID == doer.ID {
					continue
				}
				item.Reason = "Your review is requested."
				if reason := agentReviews[issue.ID]; reason != "" {
					item.Reason = reason
				}
				item.Action = api.InboxAction{Label: "Review", URL: item.HTMLURL + "/files"}
			case "blocked":
				item.Reason, item.Action = blockedReason(r, item.HTMLURL)
				if item.Reason == "" {
					continue
				}
			case "agents":
				if agents[issue.ID].latest == nil {
					continue
				}
				if r != nil && r.ChecksState.IsPending() {
					item.Reason = "Checks queued or pending."
				}
			case "mentions":
				item.Reason = "You were mentioned in an unread notification."
			}
			if activity := agents[issue.ID]; activity.latest != nil && (activity.latest.Kind != "issue" || kind == "agents") && (kind != "decision" || agentReviews[issue.ID] == "") {
				item.Reason = strings.TrimSpace(item.Reason + " " + activity.event + ".")
			}
			item.Action.URL = httplib.MakeAbsoluteURL(ctx, item.Action.URL)
			group.Items = append(group.Items, item)
			if len(group.Items) == groupLimit {
				break
			}
		}
		group.Count = len(group.Items)
		result.Groups = append(result.Groups, group)
	}
	return result, nil
}

func blockedReason(r *pull_service.Readiness, url string) (string, api.InboxAction) {
	action := api.InboxAction{Label: "Read", URL: url}
	var reasons []string
	if r.ChecksState.IsFailure() {
		var names []string
		for _, status := range r.Statuses {
			if status.State.IsFailure() || status.State.IsError() || status.State.IsWarning() {
				names = append(names, status.Context)
				if action.Label == "Read" && status.TargetURL != "" {
					action = api.InboxAction{Label: "Open log", URL: status.TargetURL}
				}
			}
		}
		reason := "Checks failing"
		if len(names) > 0 {
			reason += ": " + strings.Join(names, ", ")
		}
		reasons = append(reasons, reason)
	}
	if r.ReviewDecision == issues_model.PullReviewChangesRequested {
		reasons = append(reasons, "Changes requested")
	}
	for _, blocker := range r.Blockers {
		if blocker == pull_service.BlockerChecksFailing || blocker == pull_service.BlockerChangesRequested {
			continue
		}
		if blocker == pull_service.BlockerClosed {
			continue
		}
		reasons = append(reasons, map[pull_service.ReadinessBlocker]string{
			pull_service.BlockerDraft:          "PR is a draft",
			pull_service.BlockerChecking:       "Mergeability is being checked",
			pull_service.BlockerConflict:       "Merge conflict",
			pull_service.BlockerChecksPending:  "Checks pending",
			pull_service.BlockerApprovals:      "Approvals missing",
			pull_service.BlockerReviewRequests: "Reviews pending",
			pull_service.BlockerCodeOwners:     "Code owner reviews missing",
			pull_service.BlockerOutdated:       "Branch is out of date",
			pull_service.BlockerPathReviews:    "Required path reviews missing",
			pull_service.BlockerProtectedFiles: "Protected files changed",
			pull_service.BlockerDependencies:   "Dependencies still open",
		}[blocker])
	}
	return strings.Join(reasons, ". "), action
}

func agentCandidates(since int64) builder.Cond {
	created := builder.In("issue.id", builder.Select("target_id").From("branchroom_agent_action").Where(builder.Eq{"kind": "issue"}))
	recent := builder.Or(builder.Eq{"issue.is_closed": false}, builder.Gte{"issue.updated_unix": since})
	cond := created.And(recent)
	for _, kind := range []string{"comment", "review"} {
		targets := builder.Select(kind+".issue_id").From(kind).InnerJoin("branchroom_agent_action", "branchroom_agent_action.target_id = "+kind+".id").Where(builder.Eq{"branchroom_agent_action.kind": kind}.And(builder.Gte{kind + ".created_unix": since}))
		cond = cond.Or(builder.In("issue.id", targets))
	}
	return cond
}

type agentActivity struct {
	author *branchroom_model.AgentAction
	latest *branchroom_model.AgentAction
	event  string
}

func humanReview() builder.Cond {
	return builder.NotIn("review.id", builder.Select("target_id").From("branchroom_agent_action").Where(builder.Eq{"kind": "review"}))
}

func loadAgents(ctx context.Context, ids []int64, since int64) (map[int64]agentActivity, error) {
	result := map[int64]agentActivity{}
	if len(ids) == 0 {
		return result, nil
	}
	// actions have no timestamp, so use the immutable comment or review creation time
	for _, kind := range []string{"issue", "comment", "review"} {
		var rows []struct {
			branchroom_model.AgentAction `xorm:"extends"`
			IssueID                      int64
			Type                         int
		}
		targetColumn := kind + ".issue_id"
		cond := builder.NewCond()
		if kind == "issue" {
			targetColumn = "issue.id"
		} else {
			cond = builder.Gte{kind + ".created_unix": since}
		}
		columns := "branchroom_agent_action.*, " + targetColumn + " AS issue_id"
		if kind != "issue" {
			columns += ", " + kind + ".type"
		}
		err := db.GetEngine(ctx).Table("branchroom_agent_action").Select(columns).Join("INNER", kind, "branchroom_agent_action.target_id = "+kind+".id").Where(builder.Eq{"branchroom_agent_action.kind": kind}.And(builder.In(targetColumn, ids), cond)).OrderBy("branchroom_agent_action.id DESC").Limit(candidateLimit).Find(&rows)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			activity := result[row.IssueID]
			action := row.AgentAction
			if kind == "issue" {
				activity.author = &action
			}
			if activity.latest == nil || activity.latest.ID < row.ID {
				activity.latest = &action
				activity.event = agentEvent(&action, row.Type)
			}
			result[row.IssueID] = activity
		}
	}
	return result, nil
}

func agentEvent(action *branchroom_model.AgentAction, actionType int) string {
	verb := "updated this item"
	switch action.Kind {
	case "issue":
		verb = "opened this item"
	case "review":
		switch issues_model.ReviewType(actionType) {
		case issues_model.ReviewTypeApprove:
			verb = "approved"
		case issues_model.ReviewTypeReject:
			verb = "requested changes"
		case issues_model.ReviewTypeComment:
			verb = "commented"
		default:
			verb = "started a review"
		}
	case "comment":
		switch issues_model.CommentType(actionType) {
		case issues_model.CommentTypeComment, issues_model.CommentTypeCode, issues_model.CommentTypeReview:
			verb = "commented"
		case issues_model.CommentTypeLabel:
			verb = "updated labels"
		}
	}
	return action.Agent + " " + verb
}
