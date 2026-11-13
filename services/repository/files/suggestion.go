// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package files

import (
	"context"
	"errors"
	"strings"

	"gitea.dev/models/db"
	issues_model "gitea.dev/models/issues"
	access_model "gitea.dev/models/perm/access"
	"gitea.dev/models/renderhelper"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/git"
	"gitea.dev/modules/markup/markdown"
)

var ErrSuggestionUnavailable = errors.New("suggestion is stale or cannot be applied to this branch")

type preparedSuggestion struct {
	content string
	head    string
}

func prepareSuggestion(ctx context.Context, comment *issues_model.Comment, doer *user_model.User) (*preparedSuggestion, error) {
	if doer == nil || comment.Type != issues_model.CommentTypeCode || comment.Line <= 0 || comment.Invalidated || comment.ResolveDoerID != 0 || (comment.CommentMetaData != nil && comment.CommentMetaData.AppliedSuggestionCommit != "") {
		return nil, ErrSuggestionUnavailable
	}
	count := markdown.SuggestionLineCount(comment.StartLine, comment.Line)
	if count < 1 {
		return nil, ErrSuggestionUnavailable
	}
	reviewed, ok := markdown.SuggestionContext(comment.Patch, comment.Line, count)
	if !ok {
		return nil, ErrSuggestionUnavailable
	}
	if err := comment.LoadIssue(ctx); err != nil {
		return nil, err
	}
	issue := comment.Issue
	if err := issue.LoadRepo(ctx); err != nil {
		return nil, err
	}
	rctx := renderhelper.NewRenderContextRepoComment(ctx, issue.Repo, renderhelper.RepoCommentOptions{})
	suggestions := markdown.Suggestions(rctx, comment.Content)
	if len(suggestions) != 1 {
		return nil, ErrSuggestionUnavailable
	}
	if !issue.IsPull || issue.IsClosed {
		return nil, ErrSuggestionUnavailable
	}
	if err := issue.LoadPullRequest(ctx); err != nil {
		return nil, err
	}
	pr := issue.PullRequest
	if pr.HasMerged || pr.Flow == issues_model.PullRequestFlowAGit {
		return nil, ErrSuggestionUnavailable
	}
	if err := pr.LoadHeadRepo(ctx); err != nil {
		return nil, err
	}
	if pr.HeadRepo == nil || !pr.HeadRepo.CanContentChange() {
		return nil, ErrSuggestionUnavailable
	}
	if err := comment.LoadReview(ctx); err != nil {
		return nil, err
	}
	if comment.Review == nil || comment.Review.Type == issues_model.ReviewTypePending {
		return nil, ErrSuggestionUnavailable
	}
	perm, err := access_model.GetDoerRepoPermission(ctx, pr.HeadRepo, doer)
	if err != nil {
		return nil, err
	}
	if !issues_model.CanMaintainerWriteToBranch(ctx, perm, pr.HeadBranch, doer) {
		return nil, ErrSuggestionUnavailable
	}
	gitRepo, closer, err := git.RepositoryFromContextOrOpen(ctx, pr.HeadRepo)
	if err != nil {
		return nil, err
	}
	defer closer.Close()
	if err := VerifyBranchProtection(ctx, pr.HeadRepo, gitRepo, doer, pr.HeadBranch, []string{comment.TreePath}); err != nil {
		return nil, err
	}
	commit, err := gitRepo.GetBranchCommit(ctx, pr.HeadBranch)
	if err != nil {
		return nil, err
	}
	entry, err := commit.GetTreeEntryByPath(ctx, gitRepo, comment.TreePath)
	if err != nil {
		return nil, err
	}
	if !entry.IsRegular() && !entry.IsExecutable() {
		return nil, ErrSuggestionUnavailable
	}
	content, err := entry.Blob(gitRepo).GetBlobContent(ctx, entry.GetSize(ctx, gitRepo))
	if err != nil {
		return nil, err
	}
	lines := strings.SplitAfter(content, "\n")
	// the commented line and the reviewed lines above it must all still be where they were,
	// otherwise a push moved things and an identical line elsewhere could take the change
	first := comment.Line - int64(len(reviewed))
	if comment.Line > int64(len(lines)) || first < 0 {
		return nil, ErrSuggestionUnavailable
	}
	for i, want := range reviewed {
		if strings.TrimRight(lines[first+int64(i)], "\r\n") != want {
			return nil, ErrSuggestionUnavailable
		}
	}
	start := comment.Line - count // index of the first replaced line
	target := lines[comment.Line-1]
	ending := target[len(strings.TrimRight(target, "\r\n")):]
	replacement := suggestions[0]
	switch ending {
	case "":
		replacement = strings.TrimSuffix(replacement, "\n")
	case "\r\n":
		replacement = strings.ReplaceAll(replacement, "\n", "\r\n")
	}
	updated := strings.Join(lines[:start], "") + replacement + strings.Join(lines[comment.Line:], "")
	if updated == content {
		return nil, ErrSuggestionUnavailable
	}
	return &preparedSuggestion{content: updated, head: commit.ID.String()}, nil
}

func CanApplySuggestion(ctx context.Context, comment *issues_model.Comment, doer *user_model.User) bool {
	_, err := prepareSuggestion(ctx, comment, doer)
	return err == nil
}

func ApplySuggestion(ctx context.Context, comment *issues_model.Comment, doer *user_model.User) error {
	prepared, err := prepareSuggestion(ctx, comment, doer)
	if err != nil {
		return err
	}
	pr := comment.Issue.PullRequest
	response, err := ChangeRepoFiles(ctx, pr.HeadRepo, doer, &ChangeRepoFilesOptions{
		OldBranch: pr.HeadBranch, NewBranch: pr.HeadBranch,
		LastCommitID: prepared.head, Message: "Apply suggestion from review",
		Files: []*ChangeRepoFile{{Operation: "update", TreePath: comment.TreePath, ContentReader: strings.NewReader(prepared.content)}},
	})
	if err != nil {
		return err
	}
	if comment.CommentMetaData == nil {
		comment.CommentMetaData = &issues_model.CommentMetaData{}
	}
	comment.CommentMetaData.AppliedSuggestionCommit = response.Commit.SHA
	_, err = db.GetEngine(ctx).ID(comment.ID).Cols("comment_meta_data").Update(comment)
	return err
}
