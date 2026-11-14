// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package structs

// AgentIdentity is metadata declared by the token owner, recorded by the server.
type AgentIdentity struct {
	Agent   string `json:"agent"`
	TaskURL string `json:"task_url,omitempty"`
	Sponsor string `json:"sponsor,omitempty"`
}
