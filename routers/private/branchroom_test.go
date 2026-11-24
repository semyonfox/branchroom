// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package private

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/json"
	"gitea.dev/modules/private"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	branchroom_service "gitea.dev/services/branchroom"
	"gitea.dev/services/contexttest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchroomPreReceive(t *testing.T) {
	const (
		testSessionKey    = "branchroom-test-session-key-0123456789abcdef"
		testBearerSecret  = "branchroom-test-bearer-secret-0123456789abcdef"
		testUpdatesMarker = "branchroom-test-updates-marker-payload"
	)

	enabledBeforeTest := setting.Branchroom.Enabled
	setting.Branchroom.Enabled = false
	t.Cleanup(func() {
		setting.Branchroom.Enabled = enabledBeforeTest
	})

	validBody := func(updates []byte, quarantine ...string) []byte {
		req := private.BranchroomPreReceiveRequest{
			SessionKey:   testSessionKey,
			BearerSecret: []byte(testBearerSecret),
			Updates:      updates,
		}
		if len(quarantine) > 0 {
			req.ObjectDirectory = quarantine[0]
			req.AlternateObjectDirectories = quarantine[1]
			req.QuarantinePath = quarantine[2]
		}
		body, err := json.Marshal(req)
		assert.NoError(t, err)
		return body
	}

	oversizeUpdates := make([]byte, branchroom_module.MaxReceiveUpdatesBytes+1)
	copy(oversizeUpdates, testUpdatesMarker)

	testObjectDir := "/branchroom-test-store.git/objects/incoming-quarantine"
	testAlternates := "/branchroom-test-store.git/objects"
	oversizeQuarantine := strings.Repeat("q", branchroom_module.MaxReceiveQuarantineValueBytes+1)

	for _, tc := range []struct {
		name string
		body []byte
	}{
		{name: "MalformedJSON", body: []byte(`{"session_key":`)},
		{name: "SecondJSONValue", body: append(validBody([]byte(testUpdatesMarker)), `{"session_key":"extra"}`...)},
		{name: "BodyOverLimit", body: []byte(`{"session_key":"` + strings.Repeat("x", maxBranchroomPreReceiveRequestBytes+1) + `"}`)},
		{name: "UpdatesOverLimit", body: validBody(oversizeUpdates)},
		{name: "MissingQuarantineFields", body: validBody([]byte(testUpdatesMarker))},
		{name: "BoundedQuarantineFields", body: validBody([]byte(testUpdatesMarker), testObjectDir, testAlternates, testObjectDir)},
		{name: "ObjectDirectoryOverLimit", body: validBody([]byte(testUpdatesMarker), oversizeQuarantine, testAlternates, testObjectDir)},
		{name: "AlternatesOverLimit", body: validBody([]byte(testUpdatesMarker), testObjectDir, oversizeQuarantine, testObjectDir)},
		{name: "QuarantinePathOverLimit", body: validBody([]byte(testUpdatesMarker), testObjectDir, testAlternates, oversizeQuarantine)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, resp := contexttest.MockPrivateContext(t, "POST /branchroom/pre-receive")
			ctx.Req.Body = io.NopCloser(bytes.NewReader(tc.body))

			BranchroomPreReceive(ctx)

			respText := resp.Body.String()
			var response private.Response
			assert.NoError(t, json.Unmarshal([]byte(respText), &response))
			assert.Equal(t, http.StatusForbidden, resp.Code)
			assert.Equal(t, "Branchroom receive denied", response.UserMsg)
			assert.Empty(t, response.Err)
			assert.NotContains(t, respText, testSessionKey)
			assert.NotContains(t, respText, testBearerSecret)
			assert.NotContains(t, respText, testUpdatesMarker)
			assert.NotContains(t, respText, testObjectDir)
			assert.NotContains(t, respText, oversizeQuarantine)
		})
	}
}

func TestBranchroomPreReceiveAdmitsVerifiedPush(t *testing.T) {
	const (
		testSessionKey    = "branchroom-test-session-key-0123456789abcdef"
		testBearerSecret  = "branchroom-test-bearer-secret-0123456789abcdef"
		testUpdatesMarker = "branchroom-test-updates-marker-payload"
		testObjectDir     = "/branchroom-test-store.git/objects/incoming-quarantine"
		testAlternates    = "/branchroom-test-store.git/objects"
	)

	var got branchroom_service.VerifyReceiveSessionPushRequest
	reset := test.MockVariableValue(&verifyBranchroomReceiveSessionPush, func(_ context.Context, request branchroom_service.VerifyReceiveSessionPushRequest) error {
		got = branchroom_service.VerifyReceiveSessionPushRequest{
			SessionKey:   request.SessionKey,
			BearerSecret: bytes.Clone(request.BearerSecret),
			Updates:      bytes.Clone(request.Updates),
			Quarantine:   request.Quarantine,
		}
		return nil
	})
	defer reset()

	body, err := json.Marshal(private.BranchroomPreReceiveRequest{
		SessionKey:                 testSessionKey,
		BearerSecret:               []byte(testBearerSecret),
		Updates:                    []byte(testUpdatesMarker),
		ObjectDirectory:            testObjectDir,
		AlternateObjectDirectories: testAlternates,
		QuarantinePath:             testObjectDir,
	})
	require.NoError(t, err)
	ctx, resp := contexttest.MockPrivateContext(t, "POST /branchroom/pre-receive")
	ctx.Req.Body = io.NopCloser(bytes.NewReader(body))

	BranchroomPreReceive(ctx)

	assert.Equal(t, http.StatusNoContent, resp.Code)
	assert.Empty(t, resp.Body.String())
	assert.Equal(t, testSessionKey, got.SessionKey)
	assert.Equal(t, []byte(testBearerSecret), got.BearerSecret)
	assert.Equal(t, []byte(testUpdatesMarker), got.Updates)
	assert.Equal(t, testObjectDir, got.Quarantine.ObjectDirectory)
	assert.Equal(t, testAlternates, got.Quarantine.AlternateObjectDirectories)
	assert.Equal(t, testObjectDir, got.Quarantine.QuarantinePath)
}
