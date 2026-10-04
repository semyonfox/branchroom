// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	safeRestrictedStoreDirPerm    os.FileMode = 0o755
	safeRestrictedStoreConfigPerm os.FileMode = 0o644
)

// ErrRestrictedStorePermissionsUnavailable collapses every imported-store lookup, validation or
// mode-normalization failure so filesystem details are never exposed to a caller.
var ErrRestrictedStorePermissionsUnavailable = errors.New("Branchroom restricted store permissions unavailable")

// NormalizeRestrictedStorePermissions pins the imported store directory and its config file to
// the modes required by receive integrity checks. It only operates below a verified restricted
// root, rejects substituted path components, and changes modes through opened file descriptors.
func NormalizeRestrictedStorePermissions(restrictedRoot, storeID string) error {
	if !filepath.IsAbs(restrictedRoot) || filepath.Clean(restrictedRoot) != restrictedRoot {
		return ErrInvalidRestrictedRoot
	}
	if !isCanonicalUUID(storeID) {
		return ErrInvalidRestrictedStoreID
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

	// OpenRoot must have landed on the exact directory that was validated, not a swapped-in lookalike.
	enteredRootInfo, err := root.Lstat(".")
	if err != nil || !os.SameFile(initialRootInfo, enteredRootInfo) || !isPrivateRestrictedRoot(enteredRootInfo) {
		return ErrUnsafeRestrictedRoot
	}

	storeRoot, err := openImportedStoreRoot(root, storeID+".git")
	if err != nil {
		return err
	}
	defer storeRoot.Close()

	if err := chmodOpenedDirectory(storeRoot, safeRestrictedStoreDirPerm); err != nil {
		return err
	}
	if err := chmodImportedStoreObjects(storeRoot, safeRestrictedStoreDirPerm); err != nil {
		return err
	}
	return chmodImportedStoreConfig(storeRoot, safeRestrictedStoreConfigPerm)
}

func openImportedStoreRoot(root *os.Root, name string) (*os.Root, error) {
	initialInfo, err := root.Lstat(name)
	if err != nil || !isRealDirectory(initialInfo) {
		return nil, ErrRestrictedStorePermissionsUnavailable
	}

	storeRoot, err := root.OpenRoot(name)
	if err != nil {
		return nil, ErrRestrictedStorePermissionsUnavailable
	}
	enteredInfo, err := storeRoot.Lstat(".")
	if err != nil || !os.SameFile(initialInfo, enteredInfo) || !isRealDirectory(enteredInfo) {
		_ = storeRoot.Close()
		return nil, ErrRestrictedStorePermissionsUnavailable
	}
	return storeRoot, nil
}

func chmodOpenedDirectory(root *os.Root, mode os.FileMode) error {
	directory, err := root.Open(".")
	if err != nil {
		return ErrRestrictedStorePermissionsUnavailable
	}
	defer directory.Close()

	info, err := directory.Stat()
	if err != nil || !isRealDirectory(info) {
		return ErrRestrictedStorePermissionsUnavailable
	}
	if err := directory.Chmod(mode); err != nil {
		return ErrRestrictedStorePermissionsUnavailable
	}
	info, err = directory.Stat()
	if err != nil || !hasExactSafeMode(info, mode) {
		return ErrRestrictedStorePermissionsUnavailable
	}
	return nil
}

func chmodImportedStoreConfig(root *os.Root, mode os.FileMode) error {
	initialInfo, err := root.Lstat("config")
	if err != nil || !isRealRegularFile(initialInfo) {
		return ErrRestrictedStorePermissionsUnavailable
	}

	config, err := root.Open("config")
	if err != nil {
		return ErrRestrictedStorePermissionsUnavailable
	}
	defer config.Close()

	info, err := config.Stat()
	if err != nil || !os.SameFile(initialInfo, info) || !isRealRegularFile(info) {
		return ErrRestrictedStorePermissionsUnavailable
	}
	if err := config.Chmod(mode); err != nil {
		return ErrRestrictedStorePermissionsUnavailable
	}
	info, err = config.Stat()
	if err != nil || !hasExactSafeMode(info, mode) {
		return ErrRestrictedStorePermissionsUnavailable
	}
	return nil
}

func chmodImportedStoreObjects(root *os.Root, mode os.FileMode) error {
	initialInfo, err := root.Lstat("objects")
	if err != nil || !isRealDirectory(initialInfo) {
		return ErrRestrictedStorePermissionsUnavailable
	}

	objectsRoot, err := root.OpenRoot("objects")
	if err != nil {
		return ErrRestrictedStorePermissionsUnavailable
	}
	defer objectsRoot.Close()

	enteredInfo, err := objectsRoot.Lstat(".")
	if err != nil || !os.SameFile(initialInfo, enteredInfo) || !isRealDirectory(enteredInfo) {
		return ErrRestrictedStorePermissionsUnavailable
	}
	return chmodOpenedDirectory(objectsRoot, mode)
}

func isRealDirectory(info fs.FileInfo) bool {
	return info.Mode()&fs.ModeSymlink == 0 && info.IsDir()
}

func isRealRegularFile(info fs.FileInfo) bool {
	return info.Mode()&fs.ModeSymlink == 0 && info.Mode().IsRegular()
}

func hasExactSafeMode(info fs.FileInfo, mode os.FileMode) bool {
	return info.Mode().Perm() == mode && info.Mode()&(fs.ModeSetuid|fs.ModeSetgid|fs.ModeSticky) == 0
}
