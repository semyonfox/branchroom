// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package inbox

import (
	"context"
	"fmt"

	issues_model "gitea.dev/models/issues"
	access_model "gitea.dev/models/perm/access"
	"gitea.dev/models/unit"
	"gitea.dev/modules/cache"
	"gitea.dev/modules/git"
	"gitea.dev/modules/log"
	"gitea.dev/services/gitdiff"
)

// PullFacts is what a pull request row shows at a glance
type PullFacts struct {
	Size  string
	Trust string
}

// ListPullFacts returns size and author trust for the open pull requests in a list, keyed by issue ID
func ListPullFacts(ctx context.Context, issues issues_model.IssueList) (map[int64]PullFacts, error) {
	facts := map[int64]PullFacts{}
	trust := map[string]string{}
	for _, issue := range issues {
		if !issue.IsPull || issue.IsClosed || issue.PullRequest == nil || issue.Repo == nil {
			continue
		}
		key := fmt.Sprintf("%d/%d", issue.RepoID, issue.PosterID)
		if _, ok := trust[key]; !ok {
			level, err := posterTrust(ctx, issue)
			if err != nil {
				return nil, err
			}
			trust[key] = level
		}
		size, err := pullSize(ctx, issue)
		if err != nil {
			log.Warn("ListPullFacts: size of pull request %d in repository %d: %v", issue.Index, issue.RepoID, err) // a size chip is not worth the list
		}
		facts[issue.ID] = PullFacts{Size: size, Trust: trust[key]}
	}
	return facts, nil
}

func posterTrust(ctx context.Context, issue *issues_model.Issue) (string, error) {
	if err := issue.LoadPoster(ctx); err != nil {
		return "", err
	}
	p, err := access_model.GetDoerRepoPermission(ctx, issue.Repo, issue.Poster)
	if err != nil {
		return "", err
	}
	if p.IsAdmin() || p.CanWrite(unit.TypeCode) {
		return "maintainer", nil
	}
	if p.IsMemberOrCollaborator() {
		return "collaborator", nil
	}
	merged, err := issues_model.HasMergedPullRequestInRepo(ctx, issue.RepoID, issue.PosterID)
	if err != nil || merged {
		return "contributor", err
	}
	return "first-time contributor", nil
}

func pullSize(ctx context.Context, issue *issues_model.Issue) (string, error) {
	pr := issue.PullRequest
	gitRepo, closer, err := git.RepositoryFromContextOrOpen(ctx, issue.Repo)
	if err != nil {
		return "", err
	}
	defer closer.Close()
	head, err := gitRepo.GetRefCommitID(ctx, pr.GetGitHeadRefName())
	if err != nil {
		return "", nil // a PR whose head ref is gone just shows no size
	}
	// the diff for a merge base and head never changes, so a hit is always right
	key := fmt.Sprintf("pull-size/%d/%s/%s", issue.RepoID, pr.MergeBase, head)
	var size string
	if hit, _ := cache.GetCache().GetJSON(key, &size); hit {
		return size, nil
	}
	stat, err := gitdiff.GetDiffShortStat(ctx, gitRepo, &gitdiff.DiffCommonOptions{BeforeCommitID: pr.MergeBase, AfterCommitID: head})
	if err != nil {
		// a merge base that no longer exists costs the chip, not the list, and is not retried on every view
		log.Warn("pullSize: diff stat for pull request %d in repository %d: %v", issue.Index, issue.RepoID, err)
		_ = cache.GetCache().PutJSON(key, "", 10*60)
		return "", nil
	}
	size = sizeLabel(stat.TotalAddition + stat.TotalDeletion)
	_ = cache.GetCache().PutJSON(key, size, 24*60*60)
	return size, nil
}
