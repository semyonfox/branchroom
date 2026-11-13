// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package activities

import (
	"context"

	"gitea.dev/models/db"
	issues_model "gitea.dev/models/issues"

	"xorm.io/builder"
)

// NotificationReason is why a user gets a notification, named as GitHub names it
type NotificationReason string

const (
	NotificationReasonReviewRequested NotificationReason = "review_requested"
	NotificationReasonAssign          NotificationReason = "assign"
	NotificationReasonMention         NotificationReason = "mention"
	NotificationReasonAuthor          NotificationReason = "author"
	NotificationReasonSubscribed      NotificationReason = "subscribed"
)

// NotificationReasons are the filterable reasons, strongest first. A notification reports its strongest
var NotificationReasons = []NotificationReason{
	NotificationReasonReviewRequested, NotificationReasonAssign, NotificationReasonMention, NotificationReasonAuthor,
}

// ParseNotificationReason returns the reason, or "" when the value isn't a filterable one
func ParseNotificationReason(s string) NotificationReason {
	for _, r := range NotificationReasons {
		if string(r) == s {
			return r
		}
	}
	return ""
}

// reasonIssueIDs selects the issues where the user, matched by isUser on a user ID column, has the reason.
// It reads the issue's current state, and only counts review requests to the user, not to their teams
func reasonIssueIDs(r NotificationReason, isUser func(col string) builder.Cond) *builder.Builder {
	switch r {
	case NotificationReasonReviewRequested:
		return builder.Select("review.issue_id").From("review").
			Where(isUser("review.reviewer_id").And(builder.Eq{"review.type": issues_model.ReviewTypeRequest}))
	case NotificationReasonAssign:
		return builder.Select("issue_assignees.issue_id").From("issue_assignees").Where(isUser("issue_assignees.assignee_id"))
	case NotificationReasonMention:
		return builder.Select("issue_user.issue_id").From("issue_user").
			Where(isUser("issue_user.uid").And(builder.Eq{"issue_user.is_mentioned": true}))
	case NotificationReasonAuthor:
		return builder.Select("issue.id").From("issue").Where(isUser("issue.poster_id"))
	}
	return nil
}

func notificationReasonCond(r NotificationReason) builder.Cond {
	q := reasonIssueIDs(r, func(col string) builder.Cond { return builder.Expr(col + " = notification.user_id") })
	if q == nil {
		return builder.NewCond()
	}
	return builder.In("notification.issue_id", q)
}

// LoadReasons returns each notification's strongest reason by notification ID, subscribed when it has none of the others
func (nl NotificationList) LoadReasons(ctx context.Context, userID int64) (map[int64]NotificationReason, error) {
	issueIDs := make([]int64, 0, len(nl))
	for _, n := range nl {
		if n.IssueID != 0 {
			issueIDs = append(issueIDs, n.IssueID)
		}
	}
	isUser := func(col string) builder.Cond { return builder.Eq{col: userID} }
	byIssue := make(map[int64]NotificationReason, len(issueIDs))
	// weakest first, so the strongest reason is written last
	for i := len(NotificationReasons) - 1; i >= 0 && len(issueIDs) > 0; i-- {
		r := NotificationReasons[i]
		var ids []int64
		if err := db.GetEngine(ctx).Table("issue").Cols("id").
			Where(builder.In("id", issueIDs).And(builder.In("id", reasonIssueIDs(r, isUser)))).Find(&ids); err != nil {
			return nil, err
		}
		for _, id := range ids {
			byIssue[id] = r
		}
	}
	reasons := make(map[int64]NotificationReason, len(nl))
	for _, n := range nl {
		reasons[n.ID] = NotificationReasonSubscribed
		if r, ok := byIssue[n.IssueID]; ok {
			reasons[n.ID] = r
		}
	}
	return reasons, nil
}

// DeleteNotification marks a notification done. It comes back with the thread's next activity
func DeleteNotification(ctx context.Context, userID, notificationID int64) error {
	_, err := db.GetEngine(ctx).Where(builder.Eq{"id": notificationID, "user_id": userID}).Delete(new(Notification))
	return err
}
