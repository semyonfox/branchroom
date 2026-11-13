// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package issues

import (
	"gitea.dev/models/db"

	"xorm.io/builder"
)

// PullChecksFilter filters pull requests by the combined status of their head commit
type PullChecksFilter string

const (
	PullChecksSuccess PullChecksFilter = "success"
	PullChecksFailure PullChecksFilter = "failure"
	PullChecksPending PullChecksFilter = "pending"
)

// PullReviewFilter filters pull requests by their official, non-dismissed reviews
type PullReviewFilter string

const (
	PullReviewApproved         PullReviewFilter = "approved"
	PullReviewChangesRequested PullReviewFilter = "changes_requested"
	PullReviewRequired         PullReviewFilter = "review_required"
)

// ParsePullChecksFilter returns the filter, or "" when the value isn't one
func ParsePullChecksFilter(s string) PullChecksFilter {
	switch f := PullChecksFilter(s); f {
	case PullChecksSuccess, PullChecksFailure, PullChecksPending:
		return f
	}
	return ""
}

// ParsePullReviewFilter returns the filter, or "" when the value isn't one
func ParsePullReviewFilter(s string) PullReviewFilter {
	switch f := PullReviewFilter(s); f {
	case PullReviewApproved, PullReviewChangesRequested, PullReviewRequired:
		return f
	}
	return ""
}

// pullChecksCond reads the summary's combined state, which is only ever success, failure or pending.
// It finds the head commit through the branch table, so AGit pull requests never match
func pullChecksCond(f PullChecksFilter) builder.Cond {
	return builder.In("issue.id", builder.Select("pull_request.issue_id").From("pull_request").
		InnerJoin("branch", "branch.repo_id = pull_request.head_repo_id AND branch.name = pull_request.head_branch").
		InnerJoin("commit_status_summary", "commit_status_summary.repo_id = pull_request.base_repo_id AND commit_status_summary.sha = branch.commit_id").
		Where(builder.Eq{"branch.is_deleted": false}.And(builder.Eq{"commit_status_summary.state": string(f)})))
}

func reviewExistsCond(typ ReviewType) builder.Cond {
	return builder.Expr("EXISTS (SELECT 1 FROM review WHERE review.issue_id = issue.id AND review.type = ? AND review.official = ? AND review.dismissed = ?)", typ, true, false)
}

func pullReviewCond(f PullReviewFilter) builder.Cond {
	rejected := reviewExistsCond(ReviewTypeReject)
	approved := reviewExistsCond(ReviewTypeApprove)
	switch f {
	case PullReviewChangesRequested:
		return rejected
	case PullReviewApproved:
		return approved.And(builder.Not{rejected})
	case PullReviewRequired:
		return builder.Not{approved}.And(builder.Not{rejected})
	}
	return nil
}

func applyPullFilterConditions(sess db.Session, opts *IssuesOptions) {
	if opts.PullChecks != "" {
		sess.And(pullChecksCond(opts.PullChecks))
	}
	if opts.PullReview != "" {
		sess.And(pullReviewCond(opts.PullReview))
	}
}
