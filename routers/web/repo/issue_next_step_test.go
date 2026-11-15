// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"testing"

	git_model "gitea.dev/models/git"
	issues_model "gitea.dev/models/issues"
	"gitea.dev/modules/commitstatus"
	"gitea.dev/modules/translation"
)

func TestIssueNextStep(t *testing.T) {
	for _, tt := range []struct {
		name        string
		in          conversationNextInput
		key, action string
	}{
		{"merged wins", conversationNextInput{Pull: true, Merged: true, Requested: true}, "merged", ""},
		{"closed wins", conversationNextInput{Closed: true, Writer: true}, "closed", ""},
		{"author failing", conversationNextInput{Pull: true, Author: true, Failing: 2, Changes: true}, "your_checks", "repo.conversation.failing_log"},
		{"requested reviewer", conversationNextInput{Pull: true, Requested: true, Failing: 2, Changes: true}, "your_review", "repo.conversation.review"},
		{"author changes", conversationNextInput{Pull: true, Author: true, Changes: true}, "your_changes", "repo.conversation.answer"},
		{"waiting author", conversationNextInput{Pull: true, Changes: true, CanMerge: true}, "changes", ""},
		{"draft", conversationNextInput{Pull: true, Draft: true, CanMerge: true}, "draft", ""},
		{"conflict", conversationNextInput{Pull: true, Conflict: true, CanMerge: true}, "conflict", "repo.conversation.resolve"},
		{"pending", conversationNextInput{Pull: true, Pending: 1, CanMerge: true}, "pending", ""},
		{"reviews", conversationNextInput{Pull: true, WaitingReviews: true, CanMerge: true}, "reviews", ""},
		{"blocked", conversationNextInput{Pull: true, Blocked: true, CanMerge: true}, "blocked", "repo.conversation.merge_requirements"},
		{"ready writer", conversationNextInput{Pull: true, CanMerge: true}, "ready", "repo.conversation.merge"},
		{"ready reader", conversationNextInput{Pull: true}, "ready_other", ""},
		{"assignee", conversationNextInput{Assignee: "niamh", Writer: true}, "assignee", ""},
		{"triage", conversationNextInput{Writer: true}, "triage", "repo.conversation.edit_details"},
		{"linked PR", conversationNextInput{LinkedIndex: 12, Assignee: "niamh"}, "linked_open", "repo.conversation.view_linked"},
		{"discussion", conversationNextInput{Labels: 1}, "discussion", "repo.issues.create_comment"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := issueNextStep(tt.in)
			if got.Key != "repo.conversation."+tt.key || got.Action != tt.action {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestNextStepCheckPrompt(t *testing.T) {
	data := &pullCommitStatusCheckData{
		RequiredChecksState:   commitstatus.CommitStatusFailure,
		pullCommitStatusState: commitstatus.CommitStatusFailure,
		PullCommitStatuses:    []*git_model.CommitStatus{{Context: "build", State: commitstatus.CommitStatusFailure}},
		IsContextRequired:     func(string) bool { return false },
	}
	if got := data.CommitStatusCheckPrompt(translation.MockLocale{}); got != "repo.pulls.status_checks_failure" {
		t.Fatalf("optional failure described as required: %s", got)
	}
	data.IsContextRequired = func(string) bool { return true }
	if got := data.CommitStatusCheckPrompt(translation.MockLocale{}); got != "repo.pulls.status_checks_failure_required" {
		t.Fatalf("required failure: %s", got)
	}
}

func TestNextStepReviews(t *testing.T) {
	approval := &issues_model.Review{ID: 1, ReviewerID: 1, Type: issues_model.ReviewTypeApprove}
	rejection := &issues_model.Review{ID: 2, ReviewerID: 2, Type: issues_model.ReviewTypeReject}
	comment := &issues_model.Review{ID: 3, ReviewerID: 2, Type: issues_model.ReviewTypeComment}
	commentOnly := &issues_model.Review{ID: 4, ReviewerID: 3, Type: issues_model.ReviewTypeComment}
	pending := &issues_model.Review{ID: 5, ReviewerID: 4, Type: issues_model.ReviewTypePending}
	reviews := issues_model.ReviewList{approval, rejection, comment, commentOnly, pending}
	count, latest := conversationReviews(reviews)
	if count != 4 || len(latest) != 3 || latest[0] != approval || latest[1] != rejection || latest[2] != commentOnly {
		t.Fatalf("count = %d, latest = %+v", count, latest)
	}
	approvalAfterComment := &issues_model.Review{ID: 6, ReviewerID: 2, Type: issues_model.ReviewTypeApprove}
	reviews = append(reviews, approvalAfterComment)
	count, latest = conversationReviews(reviews)
	if count != 5 || latest[len(latest)-1] != approvalAfterComment {
		t.Fatalf("new decision did not replace rejection: %+v", latest)
	}
	approvalAfterComment.Dismissed = true
	_, latest = conversationReviews(reviews)
	if latest[len(latest)-1] != approvalAfterComment {
		t.Fatal("dismissal must not revive the previous rejection")
	}
	request := &issues_model.Review{ID: 7, ReviewerID: 2, Type: issues_model.ReviewTypeRequest}
	count, latest = conversationReviews(append(reviews, request))
	if count != 5 || latest[len(latest)-1] != request {
		t.Fatalf("review request is not a submitted review: count = %d, latest = %+v", count, latest)
	}
}

func TestNextStepUnresolvedConversations(t *testing.T) {
	reviews := issues_model.ReviewList{{ID: 1, Type: issues_model.ReviewTypeComment}, {ID: 2, Type: issues_model.ReviewTypePending}}
	comments := issues_model.CommentList{
		{ID: 1, Type: issues_model.CommentTypeCode, ReviewID: 1, TreePath: "main.go", Line: 10},
		{ID: 2, Type: issues_model.CommentTypeCode, ReviewID: 1, TreePath: "main.go", Line: 10},
		{ID: 3, Type: issues_model.CommentTypeCode, ReviewID: 1, TreePath: "main.go", Line: 20, ResolveDoerID: 1},
		{ID: 4, Type: issues_model.CommentTypeCode, ReviewID: 2, TreePath: "main.go", Line: 30},
	}
	if got := conversationUnresolvedCount(comments, reviews); got != 1 {
		t.Fatalf("got %d unresolved conversations, want 1", got)
	}
	comments[0].ResolveDoerID = 1
	if got := conversationUnresolvedCount(comments, reviews); got != 0 {
		t.Fatalf("resolved conversation still counted: %d", got)
	}
}
