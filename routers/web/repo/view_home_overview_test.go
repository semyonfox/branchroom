// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOverviewAssetPlatform(t *testing.T) {
	for _, test := range []struct{ name, platform, arch string }{
		{"harbour-linux-amd64.tar.gz", "Linux", "x86-64"},
		{"harbour-Windows-x86_64.zip", "Windows", "x86-64"},
		{"harbour-win-arm64.exe", "Windows", "ARM64"},
		{"harbour-darwin-aarch64.tar.gz", "macOS", "ARM64"},
		{"harbour-macos-amd64.zip", "macOS", "x86-64"},
		{"harbour-mac.zip", "macOS", ""},
		{"harbour-linux", "Linux", ""},
		{"checksums.txt", "", ""},
		{"harbour-linux-amd64.sha256", "", ""},
		{"darwinner.zip", "", ""},
		{"harbour-source.zip", "", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			platform, arch := overviewAssetPlatform(test.name)
			assert.Equal(t, test.platform, platform)
			assert.Equal(t, test.arch, arch)
		})
	}
}
