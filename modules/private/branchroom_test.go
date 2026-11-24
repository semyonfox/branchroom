// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package private

import (
	"bytes"
	"errors"
	"net"
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
		userMsg   = "branchroom-test-server-message-marker"
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
	assert.ErrorIs(t, err, errBranchroomPreReceiveDenied)
	assert.NotContains(t, err.Error(), sent.SessionKey)
	assert.NotContains(t, err.Error(), string(sent.BearerSecret))
	assert.NotContains(t, err.Error(), string(sent.Updates))
	assert.NotContains(t, err.Error(), sent.ObjectDirectory)
	assert.NotContains(t, err.Error(), sent.QuarantinePath)
	assert.NotContains(t, err.Error(), userMsg)
	assert.Equal(t, sent, received)
}

func TestBranchroomPreReceiveRequiresBodylessNoContent(t *testing.T) {
	sent := BranchroomPreReceiveRequest{
		SessionKey:   "branchroom-test-session-key-marker",
		BearerSecret: []byte("branchroom-test-bearer-secret-marker"),
		Updates:      []byte("branchroom-test-updates-marker-payload"),
	}

	for _, testCase := range []struct {
		name    string
		status  int
		body    string
		allowed bool
	}{
		{name: "bodyless no content", status: http.StatusNoContent, allowed: true},
		{name: "other success status", status: http.StatusOK},
		{name: "other success status with body", status: http.StatusAccepted, body: "unexpected"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(testCase.status)
				_, _ = w.Write([]byte(testCase.body))
			}))
			defer server.Close()

			defer test.MockVariableValue(&setting.Protocol, setting.HTTP)()
			defer test.MockVariableValue(&setting.InternalToken, "branchroom-test-internal-token")()
			defer test.MockVariableValue(&setting.LocalURL, server.URL+"/")()

			err := BranchroomPreReceive(t.Context(), sent)
			if testCase.allowed {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errBranchroomPreReceiveDenied)
			assert.NotContains(t, err.Error(), sent.SessionKey)
			assert.NotContains(t, err.Error(), string(sent.BearerSecret))
			assert.NotContains(t, err.Error(), string(sent.Updates))
		})
	}
}

func TestBranchroomPreReceiveRejectsRedirect(t *testing.T) {
	sent := BranchroomPreReceiveRequest{
		SessionKey:   "branchroom-test-session-key-marker",
		BearerSecret: []byte("branchroom-test-bearer-secret-marker"),
		Updates:      []byte("branchroom-test-updates-marker-payload"),
	}
	redirectTargetCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admitted" {
			redirectTargetCalled = true
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Redirect(w, r, "/admitted", http.StatusFound)
	}))
	defer server.Close()

	defer test.MockVariableValue(&setting.Protocol, setting.HTTP)()
	defer test.MockVariableValue(&setting.InternalToken, "branchroom-test-internal-token")()
	defer test.MockVariableValue(&setting.LocalURL, server.URL+"/")()

	err := BranchroomPreReceive(t.Context(), sent)
	require.ErrorIs(t, err, errBranchroomPreReceiveDenied)
	assert.False(t, redirectTargetCalled)
	assert.NotContains(t, err.Error(), sent.SessionKey)
	assert.NotContains(t, err.Error(), string(sent.BearerSecret))
	assert.NotContains(t, err.Error(), string(sent.Updates))
}

func TestBranchroomPreReceiveInternalEndpointIsTrusted(t *testing.T) {
	originalInterfaceAddrs := branchroomInterfaceAddrs
	t.Cleanup(func() { branchroomInterfaceAddrs = originalInterfaceAddrs })
	branchroomInterfaceAddrs = func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("10.0.0.5"), Mask: net.CIDRMask(24, 32)},
			&net.IPNet{IP: net.ParseIP("2001:db8::5"), Mask: net.CIDRMask(64, 128)},
		}, nil
	}

	for _, testCase := range []struct {
		name     string
		protocol setting.Scheme
		localURL string
		trusted  bool
	}{
		{name: "unix socket", protocol: setting.HTTPUnix, localURL: "http://gitea.internal/", trusted: true},
		{name: "loopback http", protocol: setting.HTTP, localURL: "http://127.0.0.1:3000/", trusted: true},
		{name: "loopback https", protocol: setting.HTTPS, localURL: "https://localhost:3000/", trusted: true},
		{name: "remote https", protocol: setting.HTTPS, localURL: "https://gitea.internal/", trusted: true},
		{name: "assigned IPv4 http", protocol: setting.HTTP, localURL: "http://10.0.0.5:3000/", trusted: true},
		{name: "assigned IPv6 http", protocol: setting.HTTP, localURL: "http://[2001:db8::5]:3000/", trusted: true},
		{name: "unassigned private IPv4 http", protocol: setting.HTTP, localURL: "http://10.0.0.6:3000/"},
		{name: "remote http", protocol: setting.HTTP, localURL: "http://192.0.2.1:3000/"},
		{name: "DNS name http", protocol: setting.HTTP, localURL: "http://gitea.internal/"},
		{name: "unspecified http", protocol: setting.HTTP, localURL: "http://0.0.0.0:3000/", trusted: true}, // dialling 0.0.0.0 reaches this host
		{name: "multicast http", protocol: setting.HTTP, localURL: "http://224.0.0.1:3000/"},
		{name: "missing host", protocol: setting.HTTP, localURL: "http:///"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.trusted, branchroomPreReceiveInternalEndpointIsTrusted(testCase.protocol, testCase.localURL))
		})
	}
}

func TestBranchroomPreReceiveInternalEndpointFailsClosedWhenInterfacesUnavailable(t *testing.T) {
	originalInterfaceAddrs := branchroomInterfaceAddrs
	t.Cleanup(func() { branchroomInterfaceAddrs = originalInterfaceAddrs })
	branchroomInterfaceAddrs = func() ([]net.Addr, error) {
		return nil, errors.New("interface lookup failed")
	}

	assert.False(t, branchroomPreReceiveInternalEndpointIsTrusted(setting.HTTP, "http://10.0.0.5:3000/"))
}

func TestBranchroomPreReceiveRejectsNonLoopbackHTTP(t *testing.T) {
	defer test.MockVariableValue(&setting.Protocol, setting.HTTP)()
	defer test.MockVariableValue(&setting.InternalToken, "branchroom-test-internal-token")()
	defer test.MockVariableValue(&setting.LocalURL, "http://192.0.2.1:3000/")()

	err := BranchroomPreReceive(t.Context(), BranchroomPreReceiveRequest{
		SessionKey:   "branchroom-test-session-key-marker",
		BearerSecret: []byte("branchroom-test-bearer-secret-marker"),
		Updates:      []byte("branchroom-test-updates-marker-payload"),
	})
	require.ErrorIs(t, err, errBranchroomPreReceiveDenied)
}
