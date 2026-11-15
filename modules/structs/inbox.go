// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package structs

import "time"

// Inbox contains bounded groups of work needing the current user's attention.
type Inbox struct {
	Groups []InboxGroup `json:"groups"`
}

// InboxGroup counts the items returned, rather than an unbounded backlog.
type InboxGroup struct {
	Kind  string      `json:"kind"`
	Count int         `json:"count"`
	Items []InboxItem `json:"items"`
}

// InboxItem is an issue or pull request needing attention.
type InboxItem struct {
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
}
