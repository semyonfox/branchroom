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
// one canonical-UUID restricted store. It repairs nothing and invokes no Git. On success it
// returns canonical quarantine values for later Git verification; raw paths are never reused.
func ValidateReceiveQuarantine(restrictedRoot, storeID string, values ReceiveQuarantineValues) (ReceiveQuarantineValues, error) {
	if !filepath.IsAbs(restrictedRoot) || filepath.Clean(restrictedRoot) != restrictedRoot {
		return ReceiveQuarantineValues{}, ErrInvalidRestrictedRoot
	}
	if !isCanonicalUUID(storeID) {
		return ReceiveQuarantineValues{}, ErrInvalidRestrictedStoreID
	}
	mainObjects := filepath.Join(restrictedRoot, storeID+".git", "objects")

	// Git can provide an absolute quarantine path containing harmless lexical components.
	// Clean values are only compared or used to derive a final component; filesystem access
	// below remains entirely server-derived and descriptor-based.
	objectDir, ok := cleanReceiveQuarantinePath(values.ObjectDirectory)
	if !ok {
		return ReceiveQuarantineValues{}, ErrInvalidReceiveQuarantine
	}
	alternateObjects, ok := cleanReceiveQuarantinePath(values.AlternateObjectDirectories)
	if !ok {
		return ReceiveQuarantineValues{}, ErrInvalidReceiveQuarantine
	}
	quarantinePath, ok := cleanReceiveQuarantinePath(values.QuarantinePath)
	if !ok {
		return ReceiveQuarantineValues{}, ErrInvalidReceiveQuarantine
	}
	if objectDir != quarantinePath || alternateObjects != mainObjects {
		return ReceiveQuarantineValues{}, ErrInvalidReceiveQuarantine
	}

	// The cleaned final element is a plain component, so only placement remains to judge;
	// this alone rejects nested, parent and same-main paths without splitting raw input.
	if objectDir == mainObjects || filepath.Dir(objectDir) != mainObjects {
		return ReceiveQuarantineValues{}, ErrInvalidReceiveQuarantine
	}
	quarantineBase := filepath.Base(objectDir)
	if quarantineBase == "" || quarantineBase == "." || quarantineBase == ".." {
		return ReceiveQuarantineValues{}, ErrInvalidReceiveQuarantine
	}

	initialRootInfo, err := ensurePrivateRestrictedRoot(restrictedRoot)
	if err != nil {
		return ReceiveQuarantineValues{}, err
	}

	root, err := os.OpenRoot(restrictedRoot)
	if err != nil {
		return ReceiveQuarantineValues{}, ErrRestrictedRootUnavailable
	}
	defer root.Close()

	// OpenRoot must have landed on the exact directory that was validated, not a swapped-in lookalike
	enteredInfo, lstatErr := root.Lstat(".")
	if lstatErr != nil || !os.SameFile(initialRootInfo, enteredInfo) || !isPrivateRestrictedRoot(enteredInfo) || !isOwnedByGiteaProcess(enteredInfo) {
		return ReceiveQuarantineValues{}, ErrUnsafeRestrictedRoot
	}

	// only server-derived relative names are inspected below this point; Lstat never follows symlinks
	for _, name := range []string{
		storeID + ".git",
		path.Join(storeID+".git", "objects"),
		path.Join(storeID+".git", "objects", quarantineBase),
	} {
		info, lstatErr := root.Lstat(name)
		if lstatErr != nil || !isSafeDirectory(info) {
			return ReceiveQuarantineValues{}, ErrInvalidReceiveQuarantine
		}
	}
	return ReceiveQuarantineValues{
		ObjectDirectory:            objectDir,
		AlternateObjectDirectories: alternateObjects,
		QuarantinePath:             quarantinePath,
	}, nil
}

func cleanReceiveQuarantinePath(raw string) (string, bool) {
	if raw == "" || strings.ContainsRune(raw, 0) || !filepath.IsAbs(raw) {
		return "", false
	}
	return filepath.Clean(raw), true
}
