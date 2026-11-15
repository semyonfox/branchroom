// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import "testing"

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
		{"waiting author", conversationNextInput{Pull: true, Changes: true}, "changes", ""},
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
