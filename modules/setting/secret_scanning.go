// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

// SecretScanning blocks pushes that add credentials with a recognisable format
var SecretScanning = struct {
	PushProtection bool
	// AllowBypass lets a pusher skip a match with the push option. Each bypass is
	// written to the admin system notices
	AllowBypass bool
}{
	AllowBypass: true,
}

func loadSecretScanningFrom(rootCfg ConfigProvider) {
	mustMapSetting(rootCfg, "repository.secret_scanning", &SecretScanning)
}
