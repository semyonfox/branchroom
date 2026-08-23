// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package private

import (
	"context"
	"time"

	"gitea.dev/modules/json"
	"gitea.dev/modules/setting"
)

type BranchroomPreReceiveRequest struct {
	SessionKey   string `json:"session_key"`
	BearerSecret []byte `json:"bearer_secret"`
	Updates      []byte `json:"updates"`

	// raw server-child Git quarantine facts, forwarded verbatim and never derived
	ObjectDirectory            string `json:"object_directory"`
	AlternateObjectDirectories string `json:"alternate_object_directories"`
	QuarantinePath             string `json:"quarantine_path"`
}

// BranchroomPreReceive posts the receive session payload to the internal deny-only
// branchroom endpoint and reports only the transport/protocol error. It returns an
// error for any non-2xx reply (the current endpoint always denies), without exposing
// the response's user-facing message. The caller keeps ownership of the request buffers.
func BranchroomPreReceive(ctx context.Context, request BranchroomPreReceiveRequest) error {
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	defer clear(payload)

	req := NewInternalRequest(ctx, setting.LocalURL+"api/internal/branchroom/pre-receive", "POST")
	req.Header("Content-Type", "application/json")
	req.Body(payload)
	req.SetReadWriteTimeout(60 * time.Second)
	_, extra := requestJSONResp(req, &ResponseText{})
	return extra.Error
}
