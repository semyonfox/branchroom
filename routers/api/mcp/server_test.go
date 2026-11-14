// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"
	"net/http"
	"testing"
	"time"

	"gitea.dev/modules/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolBudget(t *testing.T) {
	server := newServer(&client{})
	st, ct := mcp.NewInMemoryTransports()
	session, err := server.Connect(t.Context(), st, nil)
	require.NoError(t, err)
	defer session.Close()
	peer, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(t.Context(), ct, nil)
	require.NoError(t, err)
	defer peer.Close()
	list, err := peer.ListTools(t.Context(), nil)
	require.NoError(t, err)
	data, err := json.Marshal(list)
	require.NoError(t, err)
	t.Logf("serialised tool list: %d bytes", len(data))
	// 8 KiB is about 2k tokens, leaving room for the PR conversation in small contexts
	assert.LessOrEqual(t, len(data), 8*1024)
}

func TestWait(t *testing.T) {
	calls := 0
	c := &client{rest: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assert.Equal(t, "token example", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"fingerprint":"current"}`))
	}), authorization: "token example"}
	text, err := c.wait(t.Context(), "/api/v1/repos/owner/repo/pulls/1/readiness", "old", time.Second)
	require.NoError(t, err)
	assert.Contains(t, text, "current")
	assert.Equal(t, 1, calls)
	_, err = c.wait(t.Context(), "/api/v1/repos/owner/repo/pulls/1/readiness", "current", time.Millisecond)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = c.wait(ctx, "/api/v1/repos/owner/repo/pulls/1/readiness", "current", time.Second)
	require.ErrorIs(t, err, context.Canceled)
	c.rest = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	_, err = c.wait(t.Context(), "/api/v1/repos/owner/repo/pulls/1/readiness", "current", time.Second)
	require.ErrorContains(t, err, "401")
}

func TestRepositoryPath(t *testing.T) {
	for _, name := range []string{"..", "repo/other", "repo%2fother", "repo?sudo=admin", ""} {
		_, err := (repository{Owner: "owner", Repo: name}).path()
		require.Error(t, err)
	}
}
