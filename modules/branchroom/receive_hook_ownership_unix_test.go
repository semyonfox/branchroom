// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

//go:build unix

package branchroom

import (
	"io/fs"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type ownershipTestFileInfo struct {
	mode fs.FileMode
	sys  any
}

func (info ownershipTestFileInfo) Name() string       { return "entry" }
func (info ownershipTestFileInfo) Size() int64        { return 0 }
func (info ownershipTestFileInfo) Mode() fs.FileMode  { return info.mode }
func (info ownershipTestFileInfo) ModTime() time.Time { return time.Time{} }
func (info ownershipTestFileInfo) IsDir() bool        { return info.mode.IsDir() }
func (info ownershipTestFileInfo) Sys() any           { return info.sys }

func TestSafeProcessOwnedPathsRequireCurrentEffectiveUID(t *testing.T) {
	euid := uint32(os.Geteuid())
	notEUID := euid ^ 1
	foreignUID := euid + 1
	if foreignUID == 0 {
		foreignUID = 1
	}

	assert.True(t, isSafeProcessOwnedDirectory(ownershipTestFileInfo{
		mode: fs.ModeDir | 0o700,
		sys:  &syscall.Stat_t{Uid: euid},
	}))
	assert.True(t, isSafeProcessOwnedRegularFile(ownershipTestFileInfo{
		mode: 0o600,
		sys:  &syscall.Stat_t{Uid: euid},
	}))
	assert.True(t, isSafeProcessOwnedHookFile(ownershipTestFileInfo{
		mode: 0o700,
		sys:  &syscall.Stat_t{Uid: euid},
	}))

	assert.False(t, isSafeProcessOwnedDirectory(ownershipTestFileInfo{
		mode: fs.ModeDir | 0o700,
		sys:  &syscall.Stat_t{Uid: notEUID},
	}))
	assert.False(t, isSafeProcessOwnedRegularFile(ownershipTestFileInfo{
		mode: 0o600,
		sys:  &syscall.Stat_t{Uid: notEUID},
	}))
	assert.False(t, isSafeProcessOwnedHookFile(ownershipTestFileInfo{
		mode: 0o700,
		sys:  &syscall.Stat_t{Uid: notEUID},
	}))
	assert.False(t, isOwnedByGiteaProcess(ownershipTestFileInfo{mode: fs.ModeDir | 0o700}))

	assert.True(t, isTrustedBranchroomPathOwner(ownershipTestFileInfo{
		mode: fs.ModeDir | 0o755,
		sys:  &syscall.Stat_t{Uid: euid},
	}))
	assert.True(t, isTrustedBranchroomPathOwner(ownershipTestFileInfo{
		mode: fs.ModeDir | 0o755,
		sys:  &syscall.Stat_t{Uid: 0},
	}))
	assert.False(t, isTrustedBranchroomPathOwner(ownershipTestFileInfo{
		mode: fs.ModeDir | 0o755,
		sys:  &syscall.Stat_t{Uid: foreignUID},
	}))
	assert.False(t, isTrustedBranchroomPathOwner(ownershipTestFileInfo{mode: fs.ModeDir | 0o755}))
}
