// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package structs

// AgentIdentity is metadata declared by the token owner, recorded by the server.
type AgentIdentity struct {
	Model   string `json:"model,omitempty"`
	Harness string `json:"harness,omitempty"`
	Agent   string `json:"agent"`
	TaskURL string `json:"task_url,omitempty"`
	Sponsor string `json:"sponsor,omitempty"`
	// On a token: the owner lets this agent approve and request changes as them. On a review: it was made that way.
	ApprovesAsOwner bool `json:"approves_as_owner,omitempty"`
}
