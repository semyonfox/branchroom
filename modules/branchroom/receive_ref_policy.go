// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	oidHexLenSHA1   = 40
	oidHexLenSHA256 = 64

	branchroomRefPrefix = "refs/heads/branchroom/"

	maxReceiveUpdatesCount  = 1000
	maxReceiveUpdateLineLen = 4096
	maxBranchroomNameBytes  = 255
)

// ReceiveRefUpdate is one strictly parsed pre-receive record.
type ReceiveRefUpdate struct {
	OldOID string
	NewOID string
	Ref    string
}

var (
	// ErrInvalidReceiveUpdates reports a malformed pre-receive stream. Details never echo untrusted input.
	ErrInvalidReceiveUpdates = errors.New("invalid pre-receive update stream")
	// ErrReceiveUpdatesTooLarge reports a stream or record over the size bounds.
	ErrReceiveUpdatesTooLarge = errors.New("pre-receive update stream exceeds size limits")
	// ErrTooManyReceiveUpdates reports more records than the bounded parser accepts.
	ErrTooManyReceiveUpdates = errors.New("too many pre-receive updates")
	// ErrRefDeletionDenied denies every delete attempt.
	ErrRefDeletionDenied = errors.New("Branchroom receive policy denies reference deletion")
	// ErrRefOutsideNamespace rejects references outside refs/heads/branchroom/.
	ErrRefOutsideNamespace = errors.New("Branchroom receive policy only allows refs/heads/branchroom/ references")
	// ErrUnsafeBranchroomRefName rejects malformed or unsafe branch names inside the namespace.
	ErrUnsafeBranchroomRefName = errors.New("Branchroom receive policy rejects unsafe branch name")
)

// ParseReceiveUpdates parses pre-receive stdin records, one per line as
// oldOID SP newOID SP ref, with an optional single final LF only.
func ParseReceiveUpdates(data []byte) ([]ReceiveRefUpdate, error) {
	if len(data) == 0 {
		return nil, ErrInvalidReceiveUpdates
	}
	if len(data) > MaxReceiveUpdatesBytes {
		return nil, ErrReceiveUpdatesTooLarge
	}
	lines := strings.Split(string(data), "\n")
	if lines[len(lines)-1] == "" { // exactly one optional final LF; a second yields a blank record below
		lines = lines[:len(lines)-1]
	}
	if len(lines) > maxReceiveUpdatesCount {
		return nil, ErrTooManyReceiveUpdates
	}

	updates := make([]ReceiveRefUpdate, 0, len(lines))
	for i, line := range lines {
		update, err := parseReceiveUpdateLine(line)
		if err != nil {
			return nil, fmt.Errorf("%w: record %d", err, i+1)
		}
		updates = append(updates, update)
	}
	return updates, nil
}

func parseReceiveUpdateLine(line string) (ReceiveRefUpdate, error) {
	switch {
	case line == "":
		return ReceiveRefUpdate{}, ErrInvalidReceiveUpdates
	case len(line) > maxReceiveUpdateLineLen:
		return ReceiveRefUpdate{}, fmt.Errorf("%w: record over line limit", ErrReceiveUpdatesTooLarge)
	case !utf8.ValidString(line):
		return ReceiveRefUpdate{}, fmt.Errorf("%w: invalid encoding", ErrInvalidReceiveUpdates)
	}
	for i := 0; i < len(line); i++ {
		if c := line[i]; c < 0x20 || c == 0x7f {
			return ReceiveRefUpdate{}, fmt.Errorf("%w: control character", ErrInvalidReceiveUpdates)
		}
	}

	oldOID, rest, ok := strings.Cut(line, " ")
	newOID, ref, ok2 := strings.Cut(rest, " ")
	if !ok || !ok2 || ref == "" || strings.Contains(ref, " ") {
		return ReceiveRefUpdate{}, fmt.Errorf("%w: expected three fields", ErrInvalidReceiveUpdates)
	}
	if err := validateOIDPair(oldOID, newOID); err != nil {
		return ReceiveRefUpdate{}, err
	}
	return ReceiveRefUpdate{OldOID: oldOID, NewOID: newOID, Ref: ref}, nil
}

func validateOIDPair(oldOID, newOID string) error {
	if !isCanonicalHexOID(oldOID) || !isCanonicalHexOID(newOID) {
		return fmt.Errorf("%w: malformed object id", ErrInvalidReceiveUpdates)
	}
	if len(oldOID) != len(newOID) {
		return fmt.Errorf("%w: mixed object formats in record", ErrInvalidReceiveUpdates)
	}
	if isZeroOID(oldOID) && isZeroOID(newOID) {
		return fmt.Errorf("%w: both object ids are zero", ErrInvalidReceiveUpdates)
	}
	return nil
}

func isCanonicalHexOID(oid string) bool {
	if len(oid) != oidHexLenSHA1 && len(oid) != oidHexLenSHA256 {
		return false
	}
	for i := 0; i < len(oid); i++ {
		c := oid[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func isZeroOID(oid string) bool {
	return oid == "" || strings.Trim(oid, "0") == ""
}

// CheckReceiveRefPolicyV0 permits creates and updates under refs/heads/branchroom/
// with a safe relative name and denies everything else. Malformed input is denied.
func CheckReceiveRefPolicyV0(update ReceiveRefUpdate) error {
	if err := validateOIDPair(update.OldOID, update.NewOID); err != nil {
		return ErrInvalidReceiveUpdates
	}
	if isZeroOID(update.NewOID) {
		return ErrRefDeletionDenied
	}
	name, ok := strings.CutPrefix(update.Ref, branchroomRefPrefix)
	if !ok {
		return ErrRefOutsideNamespace
	}
	return checkBranchroomName(name)
}

func checkBranchroomName(name string) error {
	if name == "" || len(name) > maxBranchroomNameBytes || !utf8.ValidString(name) ||
		strings.ContainsAny(name, "~^:?*[\\ ") || strings.Contains(name, "..") ||
		strings.Contains(name, "@{") || strings.HasSuffix(name, ".") {
		return ErrUnsafeBranchroomRefName
	}
	for segment := range strings.SplitSeq(name, "/") {
		if segment == "" || segment == "@" || strings.HasPrefix(segment, ".") || strings.HasSuffix(segment, ".lock") {
			return ErrUnsafeBranchroomRefName
		}
	}
	for i := 0; i < len(name); i++ {
		if c := name[i]; c < 0x20 || c == 0x7f {
			return ErrUnsafeBranchroomRefName
		}
	}
	return nil
}
