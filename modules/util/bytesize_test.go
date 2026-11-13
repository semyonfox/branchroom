// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatByteSize(t *testing.T) {
	var size int64 = 512
	assert.Equal(t, "512 B", FormatByteSize(size))
	size *= 1024
	assert.Equal(t, "512 KiB", FormatByteSize(size))
	size *= 1024
	assert.Equal(t, "512 MiB", FormatByteSize(size))
	size *= 1024
	assert.Equal(t, "512 GiB", FormatByteSize(size))
	size *= 1024
	assert.Equal(t, "512 TiB", FormatByteSize(size))
	size *= 1024
	assert.Equal(t, "512 PiB", FormatByteSize(size))
	size *= 4
	assert.Equal(t, "2.0 EiB", FormatByteSize(size))
}

func TestFormatByteSizeDecimal(t *testing.T) {
	for _, tc := range []struct {
		size int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{13002342, "12.4 MiB"},
		{512 * 1024 * 1024, "512.0 MiB"},
	} {
		assert.Equal(t, tc.want, FormatByteSizeDecimal(tc.size))
	}
}
