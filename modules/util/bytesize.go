// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package util

import (
	"fmt"

	"github.com/dustin/go-humanize"
)

// FormatByteSize formats a byte count as a user-friendly string, e.g. "1.5 MiB".
func FormatByteSize(size int64) string {
	return humanize.IBytes(uint64(size))
}

// ParseByteSize parses a byte size such as "100MB" or "1.5 GiB".
func ParseByteSize(value string) (uint64, error) {
	return humanize.ParseBytes(value)
}

// FormatByteSizeDecimal keeps one decimal place for release asset sizes.
func FormatByteSizeDecimal(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	value := float64(size)
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	unit := 0
	for value >= 1024 && unit < len(units)-1 {
		value /= 1024
		unit++
	}
	return fmt.Sprintf("%.1f %s", value, units[unit])
}
