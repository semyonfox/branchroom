// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package structs

import "time"

// Inbox contains bounded groups of work needing the current user's attention.
type Inbox struct {
	CanTriage bool         `json:"can_triage"`
	Groups    []InboxGroup `json:"groups"`
}

// InboxGroup counts the items returned, rather than an unbounded backlog.
type InboxGroup struct {
	Kind  string      `json:"kind"`
	Count int         `json:"count"`
	Items []InboxItem `json:"items"`
}

// InboxItem is an issue or pull request needing attention.
type InboxItem struct {
	ID        int64       `json:"id"`
	CreatedAt time.Time   `json:"created_at"`
	Size      string      `json:"size,omitempty"`
	Trust     string      `json:"trust,omitempty"`
	Checks    string      `json:"checks,omitempty"`
	Kind      string      `json:"kind"`
	Repo      string      `json:"repo"`
	Index     int64       `json:"index"`
	Title     string      `json:"title"`
	HTMLURL   string      `json:"html_url"`
	UpdatedAt time.Time   `json:"updated_at"`
	Reason    string      `json:"reason"`
	Action    InboxAction `json:"action"`
	Agent     *InboxAgent `json:"agent,omitempty"`
}

// InboxAction links to the next step.
type InboxAction struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// InboxAgent identifies the recorded agent and sponsor.
type InboxAgent struct {
	Name    string `json:"name"`
	TaskURL string `json:"task_url"`
	Sponsor string `json:"sponsor"`
	Model   string `json:"model,omitempty"`
	Harness string `json:"harness,omitempty"`
}

// InboxStateOptions applies one action to at most 100 issue IDs.
// swagger:model
type InboxStateOptions struct {
	Items  []int64 `json:"items"`
	Action string  `json:"action"`
	Reason string  `json:"reason,omitempty"`
}

// swagger:model InboxStateResult
// InboxStateResult reports an individual item's outcome.
type InboxStateResult struct {
	ID    int64  `json:"id"`
	Error string `json:"error,omitempty"`
}
