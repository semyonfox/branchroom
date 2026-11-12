// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"gitea.dev/modules/setting"

	"github.com/google/uuid"
)

const (
	branchroomHooksDirName             = ".branchroom-hooks"
	preReceiveHookFileName             = "pre-receive"
	safeHookDirPerm        os.FileMode = 0o700
	safeHookFilePerm       os.FileMode = 0o700
)

var (
	// ErrInvalidRestrictedRoot rejects a supplied restricted root without echoing it.
	ErrInvalidRestrictedRoot = errors.New("invalid Branchroom restricted repository root")
	// ErrUnsafeRestrictedRoot rejects a symlinked, non-directory or non-private root without echoing it.
	ErrUnsafeRestrictedRoot = errors.New("unsafe Branchroom restricted repository root")
	// ErrRestrictedRootUnavailable collapses root lookup and open failures so no supplied path is echoed.
	ErrRestrictedRootUnavailable = errors.New("Branchroom restricted repository root unavailable")
	// ErrInvalidRestrictedStoreID rejects a store ID without echoing it.
	ErrInvalidRestrictedStoreID = errors.New("invalid Branchroom restricted store ID")
	// ErrUnsafeReceiveHook rejects an unsafe hook-tree component or target; untrusted paths are never chmod-repaired.
	ErrUnsafeReceiveHook = errors.New("unsafe Branchroom receive hook path")
	// ErrReceiveHookUnavailable collapses unexpected child directory/hook operational failures so no path or raw system error text is echoed.
	ErrReceiveHookUnavailable = errors.New("Branchroom receive hook unavailable")
)

// InstallRestrictedStoreHook installs the fixed static pre-receive delegate script for one
// canonical-UUID restricted store below a server-owned hidden directory under restrictedRoot.
// It writes no Git config and no push path; it returns the exact absolute hook directory so a
// later trusted step can point core.hooksPath at it.
func InstallRestrictedStoreHook(restrictedRoot, storeID string) (string, error) {
	if !filepath.IsAbs(restrictedRoot) || filepath.Clean(restrictedRoot) != restrictedRoot {
		return "", ErrInvalidRestrictedRoot
	}
	if !isCanonicalUUID(storeID) {
		return "", ErrInvalidRestrictedStoreID
	}
	initialRootInfo, err := ensureIntegritySafeRoot(restrictedRoot)
	if err != nil {
		return "", err
	}

	root, err := os.OpenRoot(restrictedRoot)
	if err != nil {
		return "", ErrRestrictedRootUnavailable
	}
	defer root.Close()

	// OpenRoot must have landed on the exact directory that was validated, not a swapped-in lookalike
	enteredInfo, lstatErr := root.Lstat(".")
	if lstatErr != nil || !os.SameFile(initialRootInfo, enteredInfo) || !isPrivateRestrictedRoot(enteredInfo) {
		return "", ErrUnsafeRestrictedRoot
	}

	storeHooksDir := path.Join(branchroomHooksDirName, storeID)
	if err := ensureSafeHookDirectory(root, branchroomHooksDirName); err != nil {
		return "", collapseUnexpectedHookError(err)
	}
	// fsync the root so the new .branchroom-hooks entry survives a crash
	if err := syncHookDirectoryEntry(root, "."); err != nil {
		return "", collapseUnexpectedHookError(err)
	}
	if err := ensureSafeHookDirectory(root, storeHooksDir); err != nil {
		return "", collapseUnexpectedHookError(err)
	}
	if err := syncHookDirectoryEntry(root, branchroomHooksDirName); err != nil {
		return "", collapseUnexpectedHookError(err)
	}
	target := path.Join(storeHooksDir, preReceiveHookFileName)
	if err := installPreReceiveHookFile(root, target); err != nil {
		return "", collapseUnexpectedHookError(err)
	}
	// fsync the store directory so the renamed hook entry survives a crash
	if err := syncHookDirectoryEntry(root, storeHooksDir); err != nil {
		return "", collapseUnexpectedHookError(err)
	}
	return filepath.Join(restrictedRoot, branchroomHooksDirName, storeID), nil
}

// collapseUnexpectedHookError keeps only the explicit unsafe-path verdict; every other child-work failure becomes one stable sentinel.
func collapseUnexpectedHookError(err error) error {
	if errors.Is(err, ErrUnsafeReceiveHook) {
		return err
	}
	return ErrReceiveHookUnavailable
}

func isCanonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

// ensureIntegritySafeRoot verifies the supplied root before os.OpenRoot enters it: a real,
// non-symlink, service-controlled directory at the exact given path with no group or other
// access. It returns the initial FileInfo so the caller can confirm with os.SameFile that
// OpenRoot entered the same file.
func ensureIntegritySafeRoot(restrictedRoot string) (fs.FileInfo, error) {
	if !restrictedStoreIntegritySupported() {
		return nil, ErrUnsafeRestrictedRoot
	}
	info, err := os.Lstat(restrictedRoot)
	if err != nil {
		return nil, ErrRestrictedRootUnavailable
	}
	// Lstat sees the link itself, so a symlinked root is rejected here rather than followed by OpenRoot.
	if !isPrivateRestrictedRoot(info) || !isTrustedBranchroomPathOwner(info) {
		return nil, ErrUnsafeRestrictedRoot
	}
	// Lstat cannot see symlinked ancestors below the final component, so require full canonical resolution.
	resolved, err := filepath.EvalSymlinks(restrictedRoot)
	if err != nil {
		return nil, ErrRestrictedRootUnavailable
	}
	if resolved != restrictedRoot {
		return nil, ErrUnsafeRestrictedRoot
	}
	if !hasSafeRestrictedRootAncestors(restrictedRoot, info) {
		return nil, ErrUnsafeRestrictedRoot
	}
	return info, nil
}

func ensurePrivateRestrictedRoot(restrictedRoot string) (fs.FileInfo, error) {
	info, err := ensureIntegritySafeRoot(restrictedRoot)
	if err != nil {
		return nil, err
	}
	if err := setting.ValidateBranchroomRestrictedRoot(restrictedRoot); err != nil {
		return nil, ErrUnsafeRestrictedRoot
	}
	return info, nil
}

func ensureSafeHookDirectory(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		if mkErr := root.Mkdir(name, safeHookDirPerm); mkErr != nil && !errors.Is(mkErr, fs.ErrExist) {
			return mkErr
		}
		info, err = root.Lstat(name)
	}
	if err != nil {
		return err
	}
	// Lstat sees the link itself, so a symlinked component is rejected here rather than followed.
	if !isSafeDirectory(info) {
		return ErrUnsafeReceiveHook
	}
	return nil
}

// isSafeDirectory reports whether info from Lstat is a real non-symlink directory that group or others cannot write.
func isSafeDirectory(info fs.FileInfo) bool {
	return info.Mode()&fs.ModeSymlink == 0 && info.IsDir() && info.Mode().Perm()&0o022 == 0
}

func isPrivateRestrictedRoot(info fs.FileInfo) bool {
	return isSafeDirectory(info) && info.Mode().Perm()&0o077 == 0
}

func hasSafeRestrictedRootAncestors(restrictedRoot string, childInfo fs.FileInfo) bool {
	for parent := filepath.Dir(restrictedRoot); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err != nil || info.Mode()&fs.ModeSymlink != 0 || !info.IsDir() {
			return false
		}
		// An account that owns an ancestor can relax its mode after this check, so
		// every ancestor must be controlled by the service account or root.
		if !isTrustedBranchroomPathOwner(info) {
			return false
		}
		if info.Mode().Perm()&0o022 != 0 && (info.Mode()&fs.ModeSticky == 0 || !isOwnedByGiteaProcess(childInfo)) {
			return false
		}
		if parent == filepath.Dir(parent) {
			return true
		}
		childInfo = info
	}
}

// syncHookDirectoryEntry fsyncs a directory below root so its entries survive a crash; var only so tests can inject sync failures.
var syncHookDirectoryEntry = func(root *os.Root, name string) error {
	dir, err := root.Open(name)
	if err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		_ = dir.Close()
		return err
	}
	return dir.Close()
}

func installPreReceiveHookFile(root *os.Root, target string) (err error) {
	content := ReceivePreReceiveHookTemplate()
	if info, lstatErr := root.Lstat(target); lstatErr == nil {
		return validateHookTarget(root, target, info, content)
	} else if !errors.Is(lstatErr, fs.ErrNotExist) {
		return lstatErr
	}

	temporary := path.Join(path.Dir(target), "."+path.Base(target)+".tmp-"+uuid.NewString())
	file, openErr := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, safeHookFilePerm)
	if openErr != nil {
		return openErr
	}
	renamed := false
	defer func() {
		if !renamed && err != nil {
			_ = root.Remove(temporary)
		}
	}()
	if written, writeErr := file.WriteString(content); writeErr != nil {
		_ = file.Close()
		return writeErr
	} else if written != len(content) {
		_ = file.Close()
		return io.ErrShortWrite
	}
	// our own fresh temp file is trusted, so restore bits a umask may have stripped before rename
	if chmodErr := file.Chmod(safeHookFilePerm); chmodErr != nil {
		_ = file.Close()
		return chmodErr
	}
	if syncErr := file.Sync(); syncErr != nil {
		_ = file.Close()
		return syncErr
	}
	if closeErr := file.Close(); closeErr != nil {
		return closeErr
	}
	if renameErr := root.Rename(temporary, target); renameErr != nil {
		return renameErr
	}
	renamed = true
	// re-validate what actually landed at the target; report a mismatch, never repair it
	info, lstatErr := root.Lstat(target)
	if lstatErr != nil {
		return lstatErr
	}
	return validateHookTarget(root, target, info, content)
}

func validateHookTarget(root *os.Root, target string, info fs.FileInfo, content string) error {
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() || !isSafeHookFileMode(info.Mode()) {
		return ErrUnsafeReceiveHook
	}
	existing, readErr := root.ReadFile(target)
	if readErr != nil {
		return readErr
	}
	if !bytes.Equal(existing, []byte(content)) {
		return ErrUnsafeReceiveHook
	}
	return nil
}

func isSafeHookFileMode(mode fs.FileMode) bool {
	return mode.Perm()&0o100 != 0 && mode.Perm()&0o022 == 0 && mode&(fs.ModeSetuid|fs.ModeSetgid) == 0
}
