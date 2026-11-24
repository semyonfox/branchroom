// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package private

import (
	"io"
	"net/http"

	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/json"
	"gitea.dev/modules/private"
	branchroom_service "gitea.dev/services/branchroom"
	"gitea.dev/services/context"
)

const maxBranchroomPreReceiveRequestBytes = 384 << 10

// verifyBranchroomReceiveSessionPush is a test seam for the internal admission service.
var verifyBranchroomReceiveSessionPush = branchroom_service.VerifyReceiveSessionPush

func branchroomReceiveDenied(ctx *context.PrivateContext) {
	ctx.PrivateUserErrorf(http.StatusForbidden, "Branchroom receive denied")
}

// BranchroomPreReceive verifies a claimed Branchroom receive session push. It
// returns a bodyless success only after the dedicated admission service accepts
// the quarantined update; every denial stays the same static forbidden response.
func BranchroomPreReceive(ctx *context.PrivateContext) {
	var request private.BranchroomPreReceiveRequest
	defer func() {
		clear(request.BearerSecret)
		request.BearerSecret = nil
		clear(request.Updates)
		request.Updates = nil
	}()

	bodyReader := http.MaxBytesReader(ctx.Resp, ctx.Req.Body, maxBranchroomPreReceiveRequestBytes)
	defer bodyReader.Close()
	body, err := io.ReadAll(bodyReader)
	if err != nil || json.Unmarshal(body, &request) != nil {
		clear(body)
		branchroomReceiveDenied(ctx)
		return
	}
	defer clear(body)

	if request.SessionKey == "" ||
		len(request.BearerSecret) == 0 ||
		len(request.BearerSecret) > branchroom_module.MaxReceiveBearerSecretBytes ||
		len(request.Updates) == 0 ||
		len(request.Updates) > branchroom_module.MaxReceiveUpdatesBytes {
		branchroomReceiveDenied(ctx)
		return
	}
	for _, raw := range []string{request.ObjectDirectory, request.AlternateObjectDirectories, request.QuarantinePath} {
		if len(raw) > branchroom_module.MaxReceiveQuarantineValueBytes {
			branchroomReceiveDenied(ctx)
			return
		}
	}

	// Raw values are mapped verbatim, with no parsing, splitting or cleaning left to
	// this endpoint. The verifier owns every authorization and object decision.
	err = verifyBranchroomReceiveSessionPush(ctx, branchroom_service.VerifyReceiveSessionPushRequest{
		SessionKey:   request.SessionKey,
		BearerSecret: request.BearerSecret,
		Updates:      request.Updates,
		Quarantine: branchroom_module.ReceiveQuarantineValues{
			ObjectDirectory:            request.ObjectDirectory,
			AlternateObjectDirectories: request.AlternateObjectDirectories,
			QuarantinePath:             request.QuarantinePath,
		},
	})
	if err != nil {
		branchroomReceiveDenied(ctx)
		return
	}
	ctx.Status(http.StatusNoContent)
}
