// Copyright 2019 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package issue

import (
	"context"

	"gitea.dev/models/db"
	issues_model "gitea.dev/models/issues"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/log"
	notify_service "gitea.dev/services/notify"
)

// CloseIssue close an issue.
func CloseIssue(ctx context.Context, issue *issues_model.Issue, doer *user_model.User, commitID string) error {
	return closeIssue(ctx, issue, doer, commitID, "")
}

// CloseIssueWithComment closes an issue and adds a comment in the same transaction.
func CloseIssueWithComment(ctx context.Context, issue *issues_model.Issue, doer *user_model.User, content string) error {
	return closeIssue(ctx, issue, doer, "", content)
}

func closeIssue(ctx context.Context, issue *issues_model.Issue, doer *user_model.User, commitID, content string) error {
	var comment, reasonComment *issues_model.Comment
	var mentions []*user_model.User
	var stopwatchFinished bool
	if err := db.WithTx(ctx, func(ctx context.Context) error {
		var err error
		if content != "" {
			reasonComment, mentions, err = createIssueComment(ctx, doer, issue.Repo, issue, content, nil)
			if err != nil {
				return err
			}
		}
		comment, err = issues_model.CloseIssue(ctx, issue, doer)
		if err != nil {
			if issues_model.IsErrDependenciesLeft(err) {
				if _, err := issues_model.FinishIssueStopwatch(ctx, doer, issue); err != nil {
					log.Error("Unable to stop stopwatch for issue[%d]#%d: %v", issue.ID, issue.Index, err)
				}
			}
			return err
		}

		stopwatchFinished, err = issues_model.FinishIssueStopwatch(ctx, doer, issue)
		return err
	}); err != nil {
		return err
	}

	// after the tx: publishing inside it would announce a change a rollback undoes
	if stopwatchFinished {
		notify_service.StopwatchChanged(ctx, doer)
	}

	if reasonComment != nil {
		notify_service.CreateIssueComment(ctx, doer, issue.Repo, issue, reasonComment, mentions)
	}
	notify_service.IssueChangeStatus(ctx, doer, commitID, issue, comment, true)
	return nil
}

// ReopenIssue reopen an issue.
// FIXME: If some issues dependent this one are closed, should we also reopen them?
func ReopenIssue(ctx context.Context, issue *issues_model.Issue, doer *user_model.User, commitID string) error {
	comment, err := issues_model.ReopenIssue(ctx, issue, doer)
	if err != nil {
		return err
	}

	notify_service.IssueChangeStatus(ctx, doer, commitID, issue, comment, false)

	return nil
}
