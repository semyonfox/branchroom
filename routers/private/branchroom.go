// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package private

import (
	"errors"
	"io"
	"net/http"

	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/json"
	"gitea.dev/modules/private"
	branchroom_service "gitea.dev/services/branchroom"
	"gitea.dev/services/context"
)

const maxBranchroomPreReceiveRequestBytes = 384 << 10

func branchroomReceiveDenied(ctx *context.PrivateContext) {
	ctx.PrivateUserErrorf(http.StatusForbidden, "Branchroom receive denied")
}

// BranchroomPreReceive verifies a claimed Branchroom receive session push.
// It is deny-only: every outcome answers with the same static forbidden response,
// so this endpoint can never allow a Git write and never echoes request content.
func BranchroomPreReceive(ctx *context.PrivateContext) {
	var request private.BranchroomPreReceiveRequest
	defer func() {
		clear(request.BearerSecret)
		request.BearerSecret = nil
		clear(request.Updates)
		request.Updates = nil
	}()

	ctx.Req.Body = http.MaxBytesReader(ctx.Resp, ctx.Req.Body, maxBranchroomPreReceiveRequestBytes)
	decoder := json.NewDecoder(ctx.Req.Body)
	if err := decoder.Decode(&request); err != nil {
		branchroomReceiveDenied(ctx)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		branchroomReceiveDenied(ctx)
		return
	}

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

	// the verification result never changes the response; raw values are mapped verbatim,
	// with no parsing, splitting or cleaning left to this endpoint
	_ = branchroom_service.VerifyReceiveSessionPush(ctx, branchroom_service.VerifyReceiveSessionPushRequest{
		SessionKey:   request.SessionKey,
		BearerSecret: request.BearerSecret,
		Updates:      request.Updates,
		Quarantine: branchroom_module.ReceiveQuarantineValues{
			ObjectDirectory:            request.ObjectDirectory,
			AlternateObjectDirectories: request.AlternateObjectDirectories,
			QuarantinePath:             request.QuarantinePath,
		},
	})
	branchroomReceiveDenied(ctx)
}
