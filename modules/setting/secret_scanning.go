// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import "gitea.dev/modules/secretscan"

// SecretScanning blocks pushes that add credentials with a recognisable format
var SecretScanning = struct {
	PushProtection bool
	// AllowBypass lets a pusher skip a match with the push option. Each bypass is
	// written to the admin system notices
	AllowBypass bool
}{
	AllowBypass: true,
}

var SecretScanningScanner = new(secretscan.Scanner)

func loadSecretScanningFrom(rootCfg ConfigProvider) error {
	mustMapSetting(rootCfg, "repository.secret_scanning", &SecretScanning)
	custom := map[string]string{}
	for _, key := range rootCfg.Section("repository.secret_scanning.patterns").Keys() {
		custom[key.Name()] = key.Value()
	}
	scanner, err := secretscan.NewScanner(custom)
	if err != nil {
		return err
	}
	SecretScanningScanner = scanner
	return nil
}
