// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

//go:build unix

package setting

import (
	"io/fs"
	"os"
	"syscall"
)

func branchroomRestrictedStoreIntegritySupported() bool {
	return true
}

// BranchroomRootOwnedByService checks the ownership required by startup and receive.
func BranchroomRootOwnedByService(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && os.Geteuid() >= 0 && uint64(stat.Uid) == uint64(os.Geteuid())
}
