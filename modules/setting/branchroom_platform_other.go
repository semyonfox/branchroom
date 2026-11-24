// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

//go:build !unix

package setting

import "io/fs"

func branchroomRestrictedStoreIntegritySupported() bool {
	return false
}

func BranchroomRootOwnedByService(info fs.FileInfo) bool { return false }
