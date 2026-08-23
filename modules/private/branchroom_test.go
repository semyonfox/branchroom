// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package private

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitea.dev/modules/json"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchroomPreReceiveRequestJSON(t *testing.T) {
	req := BranchroomPreReceiveRequest{
		SessionKey:                 "session-key",
		BearerSecret:               []byte("bearer"),
		Updates:                    []byte("update"),
		ObjectDirectory:            "/store.git/objects/incoming-quarantine",
		AlternateObjectDirectories: "/store.git/objects",
		QuarantinePath:             "/store.git/objects/incoming-quarantine",
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var fields map[string]json.Value
	require.NoError(t, json.Unmarshal(data, &fields))
	require.Len(t, fields, 6)
	for _, name := range []string{
		"session_key", "bearer_secret", "updates",
		"object_directory", "alternate_object_directories", "quarantine_path",
	} {
		assert.Contains(t, fields, name)
	}
	for _, camel := range []string{"SessionKey", "BearerSecret", "Updates", "ObjectDirectory", "AlternateObjectDirectories", "QuarantinePath"} {
		assert.NotContains(t, fields, camel)
	}

	var got BranchroomPreReceiveRequest
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, req.SessionKey, got.SessionKey)
	assert.True(t, bytes.Equal(req.BearerSecret, got.BearerSecret))
	assert.True(t, bytes.Equal(req.Updates, got.Updates))
	assert.Equal(t, req.ObjectDirectory, got.ObjectDirectory)
	assert.Equal(t, req.AlternateObjectDirectories, got.AlternateObjectDirectories)
	assert.Equal(t, req.QuarantinePath, got.QuarantinePath)
}

func TestBranchroomPreReceiveDeniesAndKeepsSecretsOutOfErrors(t *testing.T) {
	const (
		testToken = "branchroom-test-internal-token"
		userMsg   = "Branchroom receive denied"
	)

	sent := BranchroomPreReceiveRequest{
		SessionKey:                 "branchroom-test-session-key-marker",
		BearerSecret:               []byte("branchroom-test-bearer-secret-marker"),
		Updates:                    []byte("branchroom-test-updates-marker-payload"),
		ObjectDirectory:            "/branchroom-test-store.git/objects/incoming-quarantine",
		AlternateObjectDirectories: "/branchroom-test-store.git/objects",
		QuarantinePath:             "/branchroom-test-store.git/objects/incoming-quarantine",
	}

	var received BranchroomPreReceiveRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/internal/branchroom/pre-receive", r.URL.Path)
		assert.Empty(t, r.URL.RawQuery)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "Bearer "+testToken, r.Header.Get("X-Gitea-Internal-Auth"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&received))
		w.WriteHeader(http.StatusForbidden)
		assert.NoError(t, json.NewEncoder(w).Encode(Response{UserMsg: userMsg}))
	}))
	defer server.Close()

	defer test.MockVariableValue(&setting.Protocol, setting.HTTP)()
	defer test.MockVariableValue(&setting.InternalToken, testToken)()
	defer test.MockVariableValue(&setting.LocalURL, server.URL+"/")()

	err := BranchroomPreReceive(t.Context(), sent)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), sent.SessionKey)
	assert.NotContains(t, err.Error(), string(sent.BearerSecret))
	assert.NotContains(t, err.Error(), string(sent.Updates))
	assert.NotContains(t, err.Error(), sent.ObjectDirectory)
	assert.NotContains(t, err.Error(), sent.QuarantinePath)
	assert.NotContains(t, err.Error(), userMsg)
	assert.Equal(t, sent, received)
}
