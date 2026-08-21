// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"bytes"
	"errors"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const maxEnvironmentReferenceBytes = 4096

var (
	// ErrInvalidEnvironmentReference hides reference contents from callers.
	ErrInvalidEnvironmentReference = errors.New("invalid Branchroom environment reference")
	// ErrInvalidEnvironmentDestination identifies an unsafe materialization target without echoing it.
	ErrInvalidEnvironmentDestination = errors.New("invalid Branchroom environment destination")
)

// EnvironmentReference is the safe, tracked pointer to one encrypted environment revision.
// It intentionally contains no ciphertext, credential, key reference, scope ID, or filesystem root.
type EnvironmentReference struct {
	Environment string
	Revision    int64
	Destination string
}

// ParseEnvironmentReference strictly parses the tracked Branchroom environment-reference format:
//
//	environment = "production"
//	revision = 42
//	destination = ".env"
//
// The format deliberately permits no comments, unknown fields, duplicate fields, or string escapes.
func ParseEnvironmentReference(data []byte) (*EnvironmentReference, error) {
	if len(data) == 0 || len(data) > maxEnvironmentReferenceBytes || !utf8.Valid(data) || bytes.ContainsRune(data, '\x00') {
		return nil, ErrInvalidEnvironmentReference
	}
	values := make(map[string]string, 3)
	for rawLine := range strings.SplitSeq(strings.TrimSuffix(string(data), "\n"), "\n") {
		line := strings.TrimSuffix(rawLine, "\r")
		if line == "" {
			continue
		}
		key, rawValue, ok := strings.Cut(line, "=")
		if !ok {
			return nil, ErrInvalidEnvironmentReference
		}
		key = strings.TrimSpace(key)
		value := strings.TrimSpace(rawValue)
		if key != "environment" && key != "revision" && key != "destination" || value == "" {
			return nil, ErrInvalidEnvironmentReference
		}
		if _, exists := values[key]; exists {
			return nil, ErrInvalidEnvironmentReference
		}
		values[key] = value
	}
	if len(values) != 3 {
		return nil, ErrInvalidEnvironmentReference
	}
	environment, ok := parseEnvironmentReferenceString(values["environment"])
	if !ok {
		return nil, ErrInvalidEnvironmentReference
	}
	destination, ok := parseEnvironmentReferenceString(values["destination"])
	if !ok {
		return nil, ErrInvalidEnvironmentReference
	}
	revision, ok := parseEnvironmentReferenceRevision(values["revision"])
	if !ok {
		return nil, ErrInvalidEnvironmentReference
	}
	reference := &EnvironmentReference{
		Environment: environment,
		Revision:    revision,
		Destination: destination,
	}
	if err := reference.Validate(); err != nil {
		return nil, err
	}
	return reference, nil
}

// Validate checks an environment reference after parsing or a trusted handoff.
func (reference *EnvironmentReference) Validate() error {
	if reference == nil || !isEnvironmentReferenceName(reference.Environment) || reference.Revision <= 0 {
		return ErrInvalidEnvironmentReference
	}
	if _, err := ValidateEnvironmentDestination(reference.Destination); err != nil {
		return ErrInvalidEnvironmentReference
	}
	return nil
}

// ValidateEnvironmentDestination checks a strict relative destination for a Branchroom-owned root.
func ValidateEnvironmentDestination(destination string) (string, error) {
	if destination == "" || len(destination) > maxEnvironmentReferenceBytes || destination != strings.TrimSpace(destination) || path.IsAbs(destination) || filepath.IsAbs(destination) || filepath.VolumeName(destination) != "" || strings.ContainsAny(destination, `\\:`) || path.Clean(destination) != destination {
		return "", ErrInvalidEnvironmentDestination
	}
	for segment := range strings.SplitSeq(destination, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.EqualFold(segment, ".git") {
			return "", ErrInvalidEnvironmentDestination
		}
		for _, character := range segment {
			if unicode.IsControl(character) {
				return "", ErrInvalidEnvironmentDestination
			}
		}
	}
	return destination, nil
}

func parseEnvironmentReferenceString(value string) (string, bool) {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", false
	}
	value = value[1 : len(value)-1]
	return value, !strings.ContainsAny(value, `"\\`)
}

func parseEnvironmentReferenceRevision(value string) (int64, bool) {
	if value == "" || value[0] == '0' {
		return 0, false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return 0, false
		}
	}
	revision, err := strconv.ParseInt(value, 10, 64)
	return revision, err == nil && revision > 0
}

func isEnvironmentReferenceName(name string) bool {
	if len(name) == 0 || len(name) > 63 || name != strings.ToLower(name) || name[0] == '-' || name[len(name)-1] == '-' {
		return false
	}
	for _, character := range name {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' {
			continue
		}
		return false
	}
	return true
}
