// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package pull

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	git_model "gitea.dev/models/git"
	issues_model "gitea.dev/models/issues"
	"gitea.dev/modules/commitstatus"
	"gitea.dev/modules/git"
	issue_service "gitea.dev/services/issue"
)

// ReadinessBlocker names one reason a pull request can't merge yet
type ReadinessBlocker string

const (
	BlockerClosed           ReadinessBlocker = "closed"
	BlockerDraft            ReadinessBlocker = "draft"
	BlockerChecking         ReadinessBlocker = "mergeability_checking"
	BlockerConflict         ReadinessBlocker = "conflict"
	BlockerChecksFailing    ReadinessBlocker = "checks_failing"
	BlockerChecksPending    ReadinessBlocker = "checks_pending"
	BlockerChangesRequested ReadinessBlocker = "changes_requested"
	BlockerApprovals        ReadinessBlocker = "approvals_missing"
	BlockerReviewRequests   ReadinessBlocker = "review_requests_pending"
	BlockerCodeOwners       ReadinessBlocker = "code_owner_reviews_missing"
	BlockerOutdated         ReadinessBlocker = "branch_outdated"
	BlockerProtectedFiles   ReadinessBlocker = "protected_files_changed"
	BlockerDependencies     ReadinessBlocker = "dependencies_open"
)

// Readiness is everything that decides whether a pull request can merge, read at once
type Readiness struct {
	HeadSHA        string
	Draft          bool
	Mergeable      bool
	ReviewDecision issues_model.PullReviewFilter // empty when no review is required or given
	ChecksState    commitstatus.CommitStatusState
	RequiredChecks []string
	Statuses       []*git_model.CommitStatus
	Blockers       []ReadinessBlocker
	Fingerprint    string
}

// GetReadiness evaluates every merge condition independently, so it lists all blockers rather than the first.
// It ignores who is asking: merge permission and force merge belong to the merge request itself
func GetReadiness(ctx context.Context, pr *issues_model.PullRequest) (*Readiness, error) {
	if err := pr.LoadIssue(ctx); err != nil {
		return nil, err
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return nil, err
	}

	r := &Readiness{
		Draft:     pr.IsWorkInProgress(ctx),
		Mergeable: pr.IsStatusMergeable() || pr.IsEmpty(),
	}
	gitRepo, closer, err := git.RepositoryFromContextOrOpen(ctx, pr.BaseRepo)
	if err != nil {
		return nil, err
	}
	defer closer.Close()
	headSHA, err := gitRepo.GetRefCommitID(ctx, pr.GetGitHeadRefName())
	if err != nil {
		return nil, fmt.Errorf("GetRefCommitID: %w", err)
	}
	r.HeadSHA = headSHA

	pb, err := git_model.GetFirstMatchProtectedBranchRule(ctx, pr.BaseRepoID, pr.BaseBranch)
	if err != nil {
		return nil, err
	}
	if r.Statuses, err = GetLatestPullCommitStatuses(ctx, pr.BaseRepoID, headSHA); err != nil {
		return nil, err
	}
	if r.RequiredChecks, err = EffectiveRequiredContexts(ctx, pr.BaseRepo, pb); err != nil {
		return nil, err
	}
	if len(r.Statuses) > 0 || len(r.RequiredChecks) > 0 {
		r.ChecksState = MergeRequiredContextsCommitStatus(r.Statuses, r.RequiredChecks)
	}
	if r.ReviewDecision, err = reviewDecision(ctx, pb, pr); err != nil {
		return nil, err
	}

	block := func(cond bool, b ReadinessBlocker) {
		if cond {
			r.Blockers = append(r.Blockers, b)
		}
	}
	block(pr.HasMerged || pr.Issue.IsClosed, BlockerClosed)
	block(r.Draft, BlockerDraft)
	block(pr.IsChecking(), BlockerChecking)
	block(!r.Mergeable && !pr.IsChecking(), BlockerConflict)
	if pb != nil {
		checksGate := pb.EnableStatusCheck || len(r.RequiredChecks) > 0
		block(checksGate && r.ChecksState.IsFailure(), BlockerChecksFailing)
		block(checksGate && !r.ChecksState.IsSuccess() && !r.ChecksState.IsFailure(), BlockerChecksPending)
		block(issues_model.MergeBlockedByRejectedReview(ctx, pb, pr), BlockerChangesRequested)
		block(!issues_model.HasEnoughApprovals(ctx, pb, pr), BlockerApprovals)
		block(issues_model.MergeBlockedByOfficialReviewRequests(ctx, pb, pr), BlockerReviewRequests)
		block(!issue_service.HasAllRequiredCodeownerReviews(ctx, pb, pr), BlockerCodeOwners)
		block(issues_model.MergeBlockedByOutdatedBranch(pb, pr), BlockerOutdated)
		block(pb.MergeBlockedByProtectedFiles(pr.ChangedProtectedFiles), BlockerProtectedFiles)
	}
	noDeps, err := issues_model.IssueNoDependenciesLeft(ctx, pr.Issue)
	if err != nil {
		return nil, err
	}
	block(!noDeps, BlockerDependencies)

	r.Fingerprint = readinessFingerprint(pr, r)
	return r, nil
}

// reviewDecision follows GitHub: requested changes win, then the branch's approval count decides
func reviewDecision(ctx context.Context, pb *git_model.ProtectedBranch, pr *issues_model.PullRequest) (issues_model.PullReviewFilter, error) {
	counts, err := pr.GetApprovalCounts(ctx)
	if err != nil {
		return "", err
	}
	var approvals, rejections, requests int64
	for _, c := range counts {
		switch c.Type {
		case issues_model.ReviewTypeApprove:
			approvals = c.Count
		case issues_model.ReviewTypeReject:
			rejections = c.Count
		case issues_model.ReviewTypeRequest:
			requests = c.Count
		}
	}
	switch {
	case rejections > 0:
		return issues_model.PullReviewChangesRequested, nil
	case pb != nil && pb.RequiredApprovals > 0:
		if issues_model.HasEnoughApprovals(ctx, pb, pr) {
			return issues_model.PullReviewApproved, nil
		}
		return issues_model.PullReviewRequired, nil
	case approvals > 0:
		return issues_model.PullReviewApproved, nil
	case requests > 0:
		return issues_model.PullReviewRequired, nil
	}
	return "", nil
}

// readinessFingerprint changes whenever the head, a status, a review or the conversation does,
// so a watcher can compare it instead of rereading everything
func readinessFingerprint(pr *issues_model.PullRequest, r *Readiness) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s|%d|%d|%t|%s|%s|%v\n", r.HeadSHA, pr.Status, pr.Issue.UpdatedUnix, pr.HasMerged, r.ChecksState, r.ReviewDecision, r.Blockers)
	for _, s := range r.Statuses {
		_, _ = fmt.Fprintf(h, "%s=%s@%d\n", s.Context, s.State, s.UpdatedUnix)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
