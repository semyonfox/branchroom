// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

//go:build unix

package branchroom

import (
	"io/fs"
	"os"
	"syscall"
)

func restrictedStoreIntegritySupported() bool {
	return true
}

// isOwnedByGiteaProcess accepts only a path whose lstat owner is the effective account running
// Gitea. The caller must pass Lstat-derived metadata so a symlink can never substitute its target.
func isOwnedByGiteaProcess(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	euid := os.Geteuid()
	return euid >= 0 && uint64(stat.Uid) == uint64(euid)
}

// isTrustedBranchroomPathOwner accepts only the service account or root. Root-owned ancestors
// are expected for paths such as / and a sticky /tmp; a different account can later chmod an
// otherwise read-only ancestor and replace an entry below it.
func isTrustedBranchroomPathOwner(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	euid := os.Geteuid()
	return euid >= 0 && (uint64(stat.Uid) == uint64(euid) || stat.Uid == 0)
}
