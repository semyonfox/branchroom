// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package pull

import (
	"context"

	git_model "gitea.dev/models/git"
	issues_model "gitea.dev/models/issues"
	"gitea.dev/models/organization"
	"gitea.dev/models/perm"
	"gitea.dev/models/unit"
	"gitea.dev/modules/git"
	"gitea.dev/modules/glob"
	"gitea.dev/modules/optional"
	issue_service "gitea.dev/services/issue"
)

// HasRequiredPathReviews evaluates rules against the target branch and current PR diff.
func HasRequiredPathReviews(ctx context.Context, pb *git_model.ProtectedBranch, pr *issues_model.PullRequest) (bool, error) {
	rules, err := git_model.GetPathReviewerRules(ctx, pb.ID)
	if err != nil || len(rules) == 0 {
		return err == nil, err
	}
	if err := pr.LoadIssue(ctx); err != nil {
		return false, err
	}
	if err := pr.LoadBaseRepo(ctx); err != nil {
		return false, err
	}
	repo, closer, err := git.RepositoryFromContextOrOpen(ctx, pr.BaseRepo)
	if err != nil {
		return false, err
	}
	defer closer.Close()
	base, err := repo.GetBranchCommit(ctx, pr.BaseBranch)
	if err != nil {
		return false, err
	}
	mergeBase, err := git.MergeBase(ctx, pr.BaseRepo, base.ID.String(), pr.GetGitHeadRefName())
	if err != nil {
		return false, err
	}
	paths, err := repo.GetFilesChangedBetween(ctx, mergeBase, pr.GetGitHeadRefName())
	if err != nil {
		return false, err
	}
	reviews, err := issues_model.FindLatestReviews(ctx, issues_model.FindReviewOptions{
		Types:   []issues_model.ReviewType{issues_model.ReviewTypeApprove, issues_model.ReviewTypeReject, issues_model.ReviewTypeRequest},
		IssueID: pr.IssueID, Dismissed: optional.Some(false), PersonsOwnOnly: true,
	})
	if err != nil {
		return false, err
	}
	approvals := make(map[int64]bool)
	for _, review := range reviews {
		if review.Type == issues_model.ReviewTypeApprove && review.ReviewerID > 0 && review.ReviewerID != pr.Issue.PosterID && (!pb.IgnoreStaleApprovals || !review.Stale) {
			approvals[review.ReviewerID] = true
		}
	}
	for _, rule := range rules {
		if err := git_model.ValidatePathReviewerRule(rule); err != nil {
			return false, err
		}
		matcher, err := glob.Compile(rule.Pattern, '/')
		if err != nil {
			return false, err
		}
		var matched []string
		for _, path := range paths {
			if matcher.Match(path) {
				matched = append(matched, path)
			}
		}
		if len(matched) == 0 {
			continue
		}
		if !rule.CodeOwners {
			team, err := organization.GetTeamByID(ctx, rule.TeamID)
			if organization.IsErrTeamNotExist(err) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			if team.OrgID != pr.BaseRepo.OwnerID || !organization.HasTeamRepo(ctx, team.OrgID, team.ID, pr.BaseRepoID) || team.UnitAccessMode(ctx, unit.TypeCode) < perm.AccessModeWrite {
				return false, nil
			}
			if err := team.LoadMembers(ctx); err != nil {
				return false, err
			}
			eligible := make(map[int64]bool)
			for _, member := range team.Members {
				eligible[member.ID] = true
			}
			if countPathApprovals(approvals, eligible) < rule.RequiredApprovals {
				return false, nil
			}
			continue
		}
		owners, warnings, err := issue_service.GetCodeOwnerRules(ctx, repo, base)
		if err != nil {
			return false, err
		}
		if len(warnings) > 0 {
			return false, nil
		}
		matching, warnings := issue_service.MatchCodeOwnerRules(owners, matched)
		if len(warnings) > 0 {
			return false, nil
		}
		if len(matching) == 0 {
			return false, nil
		}
		for _, owner := range matching {
			eligible := make(map[int64]bool)
			for _, user := range owner.Users {
				eligible[user.ID] = true
			}
			for _, team := range owner.Teams {
				if err := team.LoadMembers(ctx); err != nil {
					return false, err
				}
				for _, member := range team.Members {
					eligible[member.ID] = true
				}
			}
			if countPathApprovals(approvals, eligible) < rule.RequiredApprovals {
				return false, nil
			}
		}
	}
	return true, nil
}

func countPathApprovals(approvals, eligible map[int64]bool) int64 {
	var count int64
	for id := range approvals {
		if eligible[id] {
			count++
		}
	}
	return count
}
