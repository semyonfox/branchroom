// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package inbox

import (
	"context"
	"errors"
	"time"

	"gitea.dev/models/db"
	issues_model "gitea.dev/models/issues"
	access_model "gitea.dev/models/perm/access"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/cache"
	api "gitea.dev/modules/structs"
	issue_service "gitea.dev/services/issue"

	"xorm.io/builder"
)

type UserInboxState struct {
	UserID       int64  `xorm:"pk"`
	IssueID      int64  `xorm:"pk"`
	State        string `xorm:"VARCHAR(16) NOT NULL"`
	SnoozedUntil int64
	UpdatedUnix  int64
}

func init() { db.RegisterModel(new(UserInboxState)) }

func stateIDs(userID int64, state string) *builder.Builder {
	cond := builder.Or(builder.Eq{"state": "done"}.And(builder.Expr("user_inbox_state.updated_unix >= issue.updated_unix")), builder.Eq{"state": "snoozed"}.And(builder.Gt{"snoozed_until": time.Now().Unix()}))
	if state != "" {
		cond = cond.And(builder.Eq{"state": state})
	}
	return builder.Select("issue_id").From("user_inbox_state").Where(builder.Eq{"user_id": userID}.And(cond))
}

func activeState(userID int64, state string) builder.Cond {
	return builder.In("issue.id", stateIDs(userID, state))
}

func Apply(ctx context.Context, doer *user_model.User, opts api.InboxStateOptions) []api.InboxStateResult {
	results := make([]api.InboxStateResult, 0, len(opts.Items))
	for _, id := range opts.Items {
		result := api.InboxStateResult{ID: id}
		if err := applyItem(ctx, doer, id, opts); err != nil {
			result.Error = err.Error()
		}
		results = append(results, result)
	}
	_ = cache.GetCache().Delete(needsCountKey(doer.ID))
	return results
}

func applyItem(ctx context.Context, doer *user_model.User, id int64, opts api.InboxStateOptions) error {
	issue, err := issues_model.GetIssueByID(ctx, id)
	if err != nil {
		return errors.New("item unavailable")
	}
	if err = issue.LoadRepo(ctx); err != nil {
		return err
	}
	permission, err := access_model.GetDoerRepoPermission(ctx, issue.Repo, doer)
	if err != nil {
		return err
	}
	if !permission.CanReadIssuesOrPulls(issue.IsPull) {
		return errors.New("item unavailable")
	}
	switch opts.Action {
	case "done", "snooze-day", "snooze-week":
		state := &UserInboxState{UserID: doer.ID, IssueID: id, State: "done", UpdatedUnix: time.Now().Unix()}
		if opts.Action != "done" {
			state.State = "snoozed"
			duration := 24 * time.Hour
			if opts.Action == "snooze-week" {
				duration *= 7
			}
			state.SnoozedUntil = time.Now().Add(duration).Unix()
		}
		return db.WithTx(ctx, func(ctx context.Context) error {
			if _, err := db.GetEngine(ctx).Where("user_id = ? AND issue_id = ?", doer.ID, id).Delete(new(UserInboxState)); err != nil {
				return err
			}
			return db.Insert(ctx, state)
		})
	case "assign":
		if issue.Repo.IsArchived {
			return errors.New("repository is archived")
		}
		if !permission.CanWriteIssuesOrPulls(issue.IsPull) {
			return errors.New("cannot assign this item")
		}
		return issue_service.AddAssignees(ctx, issue, doer, []int64{doer.ID})
	case "close":
		if issue.Repo.IsArchived {
			return errors.New("repository is archived")
		}
		if !permission.CanWriteIssuesOrPulls(issue.IsPull) && issue.PosterID != doer.ID {
			return errors.New("cannot close this item")
		}
		if issue.IsClosed {
			return errors.New("item is already closed")
		}
		switch opts.Reason {
		case "duplicate", "not planned", "out of scope":
		case "needs a rebase":
			if !issue.IsPull {
				return errors.New("only pull requests can need a rebase")
			}
		default:
			return errors.New("choose a close reason")
		}
		return issue_service.CloseIssueWithComment(ctx, issue, doer, "Close reason: "+opts.Reason+".")
	default:
		return errors.New("unknown inbox action")
	}
}
