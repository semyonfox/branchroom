// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package private

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"gitea.dev/modules/json"
	"gitea.dev/modules/setting"
)

var errBranchroomPreReceiveDenied = errors.New("Branchroom receive denied")

var branchroomInterfaceAddrs = net.InterfaceAddrs

// BranchroomPushProtectionError carries the authenticated hook's redacted findings.
type BranchroomPushProtectionError string

func (e BranchroomPushProtectionError) Error() string { return string(e) }

type BranchroomPreReceiveRequest struct {
	SessionKey   string `json:"session_key"`
	BearerSecret []byte `json:"bearer_secret"`
	Updates      []byte `json:"updates"`
	AllowSecrets bool   `json:"allow_secrets"`

	// raw server-child Git quarantine facts, forwarded verbatim and never derived
	ObjectDirectory            string `json:"object_directory"`
	AlternateObjectDirectories string `json:"alternate_object_directories"`
	QuarantinePath             string `json:"quarantine_path"`
}

// BranchroomPreReceive posts the receive session payload to the internal admission
// endpoint and accepts exactly a bodyless 204 response. It does not follow redirects,
// because the session bearer must only reach the configured internal endpoint. Every
// other result maps to a static error, except for redacted push protection findings.
// The caller keeps ownership of the request buffers.
func BranchroomPreReceive(ctx context.Context, request BranchroomPreReceiveRequest) error {
	payload, err := json.Marshal(request)
	if err != nil {
		return errBranchroomPreReceiveDenied
	}
	defer clear(payload)

	client, ok := newBranchroomPreReceiveHTTPClient()
	if !ok {
		return errBranchroomPreReceiveDenied
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, setting.LocalURL+"api/internal/branchroom/pre-receive", bytes.NewReader(payload))
	if err != nil {
		return errBranchroomPreReceiveDenied
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Real-IP", getClientIP())
	req.Header.Set("X-Gitea-Internal-Auth", "Bearer "+setting.InternalToken)

	response, err := client.Do(req)
	if err != nil {
		return errBranchroomPreReceiveDenied
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnprocessableEntity {
		const maxMessageBytes = 64 * 1024
		body, err := io.ReadAll(io.LimitReader(response.Body, maxMessageBytes+1))
		var rejected Response
		if err == nil && len(body) <= maxMessageBytes && json.Unmarshal(body, &rejected) == nil && rejected.UserMsg != "" {
			return BranchroomPushProtectionError(rejected.UserMsg)
		}
		return errBranchroomPreReceiveDenied
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 1))
	if err != nil || response.StatusCode != http.StatusNoContent || len(body) != 0 {
		return errBranchroomPreReceiveDenied
	}
	return nil
}

// branchroomPreReceiveInternalEndpointIsTrusted permits a unix socket, loopback,
// or an IP assigned to this host over HTTP, or an HTTPS hop whose certificate
// will be verified.
func branchroomPreReceiveInternalEndpointIsTrusted(protocol setting.Scheme, localURL string) bool {
	u, err := url.Parse(localURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return internalAPIConnectionIsLocal(protocol, localURL) || u.Scheme == "https" || branchroomPreReceiveHostIsLocalInterface(u.Hostname())
}

func branchroomPreReceiveHostIsLocalInterface(host string) bool {
	hostIP := net.ParseIP(host)
	if hostIP == nil || !hostIP.IsGlobalUnicast() {
		return false
	}
	addrs, err := branchroomInterfaceAddrs()
	if err != nil {
		return false
	}
	for _, addr := range addrs {
		var addrIP net.IP
		switch addr := addr.(type) {
		case *net.IPNet:
			addrIP = addr.IP
		case *net.IPAddr:
			addrIP = addr.IP
		}
		if addrIP.Equal(hostIP) {
			return true
		}
	}
	return false
}

func newBranchroomPreReceiveHTTPClient() (*http.Client, bool) {
	if setting.InternalToken == "" || !branchroomPreReceiveInternalEndpointIsTrusted(setting.Protocol, setting.LocalURL) {
		return nil, false
	}

	local := internalAPIConnectionIsLocal(setting.Protocol, setting.LocalURL)
	return &http.Client{
		Transport: &http.Transport{
			DialContext: dialContextInternalAPI,
			TLSClientConfig: &tls.Config{
				// Loopback and unix-socket HTTPS may use Gitea's unverified local
				// certificate. HTTPS over a local-interface or remote address uses
				// Go's normal certificate verification.
				InsecureSkipVerify: local,
			},
		},
		Timeout: 60 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, true
}
