// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

//go:build !unix

package setting

func branchroomRestrictedStoreIntegritySupported() bool {
	return false
}
