// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

//go:build !unix

package branchroom

import "io/fs"

// Ownership cannot be verified portably on this platform, so restricted stores stay unavailable
// rather than treating an unverified path as server-controlled.
func isOwnedByGiteaProcess(fs.FileInfo) bool {
	return false
}

func restrictedStoreIntegritySupported() bool {
	return false
}

// This platform cannot establish a trusted owner for a restricted store path.
func isTrustedBranchroomPathOwner(fs.FileInfo) bool {
	return false
}
