// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

const (
	// MaxReceiveUpdatesBytes bounds the total pre-receive update stream size.
	MaxReceiveUpdatesBytes = 256 << 10
	// MaxReceiveBearerSecretBytes bounds the bearer secret length accepted from clients.
	MaxReceiveBearerSecretBytes = 128
	// MaxReceiveQuarantineValueBytes bounds each raw quarantine environment value forwarded by clients.
	MaxReceiveQuarantineValueBytes = 4096
)
