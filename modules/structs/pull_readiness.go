// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package structs

// PullRequestReadiness reports whether a pull request can merge and every reason it can't yet
type PullRequestReadiness struct {
	HeadSHA   string `json:"head_sha"`
	Draft     bool   `json:"draft"`
	Mergeable bool   `json:"mergeable"`
	// empty when no review is required or given
	// enum: ["","approved","changes_requested","review_required"]
	ReviewDecision string `json:"review_decision"`
	// combined state of the required checks, or of all checks when none are required. empty when there are none
	// enum: ["","success","failure","pending"]
	ChecksState string `json:"checks_state"`
	// status context patterns branch protection requires
	RequiredChecks []string        `json:"required_checks"`
	Checks         []*CommitStatus `json:"checks"`
	// any of closed, draft, mergeability_checking, conflict, checks_failing, checks_pending, changes_requested,
	// approvals_missing, review_requests_pending, code_owner_reviews_missing, branch_outdated, protected_files_changed and dependencies_open
	Blockers []string `json:"blockers"`
	// true when nothing blocks the merge. merge permission is checked separately when merging
	Ready bool `json:"ready"`
	// changes whenever the head, a check, a review or the conversation does. also sent as the ETag
	Fingerprint string `json:"fingerprint"`
}
