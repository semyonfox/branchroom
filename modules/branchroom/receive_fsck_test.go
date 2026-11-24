// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gitea.dev/modules/git/gitcmd"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReceivePackFsckRejectsInvalidIncomingObjects(t *testing.T) {
	for _, kind := range []string{"valid", "missing tree", "malformed commit"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			_, _, err := gitcmd.NewCommand("init", "--bare").WithDir(dir).RunStdString(t.Context())
			require.NoError(t, err)
			marker := filepath.Join(dir, "hook-called")
			require.NoError(t, os.WriteFile(filepath.Join(dir, "hooks", "pre-receive"), []byte("#!/bin/sh\ntouch '"+marker+"'\nexit 1\n"), 0o700))
			tree := "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
			if kind == "missing tree" {
				tree = strings.Repeat("a", 40)
			}
			commit := "tree " + tree + "\nauthor Test <test@example.invalid> 1 +0000\ncommitter Test <test@example.invalid> 1 +0000\n\ntest\n"
			if kind == "malformed commit" {
				commit = "tree " + tree + "\n\nmissing author\n"
			}
			oid := sha1.Sum(fmt.Appendf(nil, "commit %d%c%s", len(commit), 0, commit))
			var pack bytes.Buffer
			pack.WriteString("PACK")
			require.NoError(t, binary.Write(&pack, binary.BigEndian, uint32(2)))
			require.NoError(t, binary.Write(&pack, binary.BigEndian, uint32(2)))
			writeBranchroomPackObject(t, &pack, 2, nil)
			writeBranchroomPackObject(t, &pack, 1, []byte(commit))
			checksum := sha1.Sum(pack.Bytes())
			pack.Write(checksum[:])
			command := fmt.Sprintf("%s %x refs/heads/branchroom/test%c report-status\n", strings.Repeat("0", 40), oid, 0)
			input := fmt.Appendf(nil, "%04x%s0000", len(command)+4, command)
			input = append(input, pack.Bytes()...)
			output, _, _ := gitcmd.NewCommand("-c", "receive.fsckObjects=true", "receive-pack", "--stateless-rpc", ".").WithDir(dir).WithStdinBytes(input).RunStdString(t.Context())
			assert.Contains(t, output, "ng refs/heads/branchroom/test")
			if kind == "valid" {
				assert.FileExists(t, marker)
			} else {
				assert.NoFileExists(t, marker)
			}
			_, _, err = gitcmd.NewCommand("rev-parse", "--verify", "refs/heads/branchroom/test").WithDir(dir).RunStdString(t.Context())
			require.Error(t, err)
		})
	}
}

func writeBranchroomPackObject(t *testing.T, pack *bytes.Buffer, kind byte, data []byte) {
	t.Helper()
	size := len(data)
	first := byte(size&15) | kind<<4
	size >>= 4
	if size != 0 {
		first |= 128
	}
	pack.WriteByte(first)
	for size != 0 {
		next := byte(size & 127)
		size >>= 7
		if size != 0 {
			next |= 128
		}
		pack.WriteByte(next)
	}
	writer := zlib.NewWriter(pack)
	_, err := writer.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
}
