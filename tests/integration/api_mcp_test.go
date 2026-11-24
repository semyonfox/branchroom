// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"net/http"
	"testing"

	api "gitea.dev/modules/structs"
	"gitea.dev/tests"

	"github.com/stretchr/testify/assert"
)

func TestMCPRepositoryToken(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	createToken := func(name, repository string) string {
		req := NewRequestWithJSON(t, "POST", "/api/v1/users/user2/tokens", api.CreateAccessTokenOption{Name: name, Scopes: []string{"all"}, Repositories: []string{repository}}).AddBasicAuth("user2")
		return DecodeJSON(t, MakeRequest(t, req, http.StatusCreated), &api.AccessToken{}).Token
	}
	allowedToken := createToken("mcp-allowed", "user2/repo1")
	otherToken := createToken("mcp-other", "user2/repo2")
	call := func(token string) string {
		req := NewRequestWithJSON(t, "POST", "/api/mcp", map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "read_pr", "arguments": map[string]any{"owner": "user2", "repo": "repo1", "index": 3}},
		}).AddTokenAuth(token)
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("MCP-Protocol-Version", "2025-06-18")
		return MakeRequest(t, req, http.StatusOK).Body.String()
	}
	allowed := call(allowedToken)
	assert.NotContains(t, allowed, `"isError":true`)
	assert.Contains(t, allowed, "fingerprint")
	assert.Contains(t, allowed, "threads")
	MakeRequest(t, NewRequest(t, "GET", "/api/v1/repos/user2/repo1/pulls/3").AddTokenAuth(otherToken), http.StatusNotFound)
	blocked := call(otherToken)
	assert.Contains(t, blocked, `"isError":true`)
	assert.Contains(t, blocked, "REST returned 404")
	assert.NotContains(t, blocked, "fingerprint")
	req := NewRequestWithJSON(t, "POST", "/api/mcp", map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"})
	MakeRequest(t, req, http.StatusForbidden)
}
