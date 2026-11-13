// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

// SecretScanning blocks pushes that add credentials with a recognisable format
var SecretScanning = struct {
	PushProtection bool
}{}

func loadSecretScanningFrom(rootCfg ConfigProvider) {
	mustMapSetting(rootCfg, "repository.secret_scanning", &SecretScanning)
}
