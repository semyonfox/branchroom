// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrInvalidReceiveQuarantine collapses every malformed or unavailable quarantine fact so no
// supplied path, store identity or filesystem detail is ever echoed.
var ErrInvalidReceiveQuarantine = errors.New("invalid Branchroom receive quarantine")

// ReceiveQuarantineValues holds the raw quarantine facts exactly as received, before any derivation.
type ReceiveQuarantineValues struct {
	ObjectDirectory            string
	AlternateObjectDirectories string
	QuarantinePath             string
}

// ValidateReceiveQuarantine is a strictly read-only, deny-first check that raw quarantine facts
// describe exactly one real non-symlink directory directly below the derived objects directory of
// one canonical-UUID restricted store. It repairs nothing, invokes no Git and returns no path,
// store identity or allow decision.
func ValidateReceiveQuarantine(restrictedRoot, storeID string, values ReceiveQuarantineValues) error {
	if !filepath.IsAbs(restrictedRoot) || filepath.Clean(restrictedRoot) != restrictedRoot {
		return ErrInvalidRestrictedRoot
	}
	if !isCanonicalUUID(storeID) {
		return ErrInvalidRestrictedStoreID
	}
	mainObjects := filepath.Join(restrictedRoot, storeID+".git", "objects")

	// byte-for-byte equality below already rejects lists, quotes and wrong-store values; shape checks
	// come first so a crafted value can never smuggle NULs, relatives or unclean forms past equality
	for _, raw := range []string{values.ObjectDirectory, values.AlternateObjectDirectories, values.QuarantinePath} {
		if raw == "" || strings.ContainsRune(raw, 0) || !filepath.IsAbs(raw) || filepath.Clean(raw) != raw {
			return ErrInvalidReceiveQuarantine
		}
	}
	if values.ObjectDirectory != values.QuarantinePath || values.AlternateObjectDirectories != mainObjects {
		return ErrInvalidReceiveQuarantine
	}

	// cleanliness above makes the final element one plain component, so only placement remains to judge;
	// this alone rejects nested, parent and same-main paths without splitting or normalising anything
	objectDir := values.ObjectDirectory
	if objectDir == mainObjects || filepath.Dir(objectDir) != mainObjects {
		return ErrInvalidReceiveQuarantine
	}
	quarantineBase := filepath.Base(objectDir)
	if quarantineBase == "" || quarantineBase == "." || quarantineBase == ".." {
		return ErrInvalidReceiveQuarantine
	}

	initialRootInfo, err := ensureIntegritySafeRoot(restrictedRoot)
	if err != nil {
		return err
	}

	root, err := os.OpenRoot(restrictedRoot)
	if err != nil {
		return ErrRestrictedRootUnavailable
	}
	defer root.Close()

	// OpenRoot must have landed on the exact directory that was validated, not a swapped-in lookalike
	enteredInfo, lstatErr := root.Lstat(".")
	if lstatErr != nil || !os.SameFile(initialRootInfo, enteredInfo) {
		return ErrUnsafeRestrictedRoot
	}

	// only server-derived relative names are inspected below this point; Lstat never follows symlinks
	for _, name := range []string{
		storeID + ".git",
		path.Join(storeID+".git", "objects"),
		path.Join(storeID+".git", "objects", quarantineBase),
	} {
		info, lstatErr := root.Lstat(name)
		if lstatErr != nil || !isSafeDirectory(info) {
			return ErrInvalidReceiveQuarantine
		}
	}
	return nil
}
