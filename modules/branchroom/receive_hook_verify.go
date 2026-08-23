// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gitea.dev/modules/git/gitcmd"
)

// ErrRestrictedStoreIntegrityUnavailable collapses every store hook/config integrity mismatch or
// operational failure so neither supplied paths nor raw Git output is echoed.
var ErrRestrictedStoreIntegrityUnavailable = errors.New("Branchroom restricted store integrity unavailable")

// VerifyRestrictedStoreHook is a strictly read-only check that one canonical-UUID restricted store
// below restrictedRoot is a healthy ready-store: it verifies the store's own directory entry, its
// config file, its exact static hook tree, and a local core.hooksPath pointing byte-for-byte at the
// derived hook directory; the rest of the bare repository interior is not examined. It repairs
// nothing and returns no path or repository facade; it exists for a future write-admission service.
func VerifyRestrictedStoreHook(ctx context.Context, restrictedRoot, storeID string) error {
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

	// OpenRoot must have landed on the exact directory that was validated, not a swapped-in lookalike
	enteredInfo, lstatErr := root.Lstat(".")
	if lstatErr != nil || !os.SameFile(initialRootInfo, enteredInfo) {
		return ErrUnsafeRestrictedRoot
	}

	// only server-derived relative paths are ever inspected below this point
	storeHooksDirRel := path.Join(branchroomHooksDirName, storeID)
	for _, name := range []string{storeID + ".git", branchroomHooksDirName, storeHooksDirRel} {
		info, lstatErr := root.Lstat(name)
		if lstatErr != nil || !isSafeDirectory(info) {
			return ErrRestrictedStoreIntegrityUnavailable
		}
	}
	if err := requireSafeStoreConfigFile(root, path.Join(storeID+".git", "config")); err != nil {
		return err
	}
	if err := requireExactPreReceiveTree(root, storeHooksDirRel); err != nil {
		return err
	}
	repoDirAbs := filepath.Join(restrictedRoot, storeID+".git")
	hookDirAbs := filepath.Join(restrictedRoot, branchroomHooksDirName, storeID)
	return verifyStoreHooksPath(ctx, repoDirAbs, hookDirAbs)
}

// requireSafeStoreConfigFile requires a real regular non-symlink config file that group or others cannot write.
func requireSafeStoreConfigFile(root *os.Root, name string) error {
	info, err := root.Lstat(name)
	if err != nil || info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return ErrRestrictedStoreIntegrityUnavailable
	}
	return nil
}

// requireExactPreReceiveTree requires the store hook directory to hold exactly one entry,
// pre-receive, matching the installed hook's safe-mode semantics byte for byte, without repairing it.
func requireExactPreReceiveTree(root *os.Root, storeHooksDirRel string) error {
	dir, err := root.Open(storeHooksDirRel)
	if err != nil {
		return ErrRestrictedStoreIntegrityUnavailable
	}
	entries, readErr := dir.ReadDir(-1)
	closeErr := dir.Close()
	if readErr != nil || closeErr != nil {
		return ErrRestrictedStoreIntegrityUnavailable
	}
	targetRel := path.Join(storeHooksDirRel, preReceiveHookFileName)
	if len(entries) != 1 || entries[0].Name() != preReceiveHookFileName || !entries[0].Type().IsRegular() {
		return ErrRestrictedStoreIntegrityUnavailable
	}
	info, lstatErr := root.Lstat(targetRel)
	if lstatErr != nil {
		return ErrRestrictedStoreIntegrityUnavailable
	}
	if err := validateHookTarget(root, targetRel, info, ReceivePreReceiveHookTemplate()); err != nil {
		return ErrRestrictedStoreIntegrityUnavailable
	}
	return nil
}

// verifyStoreHooksPath reads only the local git config with a sanitized environment and explicit
// --git-dir so no directory discovery can select another repo. Git's exit status for an absent or
// unreadable config is non-zero and must reject, never pass.
func verifyStoreHooksPath(ctx context.Context, repoDirAbs, hookDirAbs string) error {
	stdout, _, err := gitcmd.NewCommand().
		AddOptionFormat("--git-dir=%s", repoDirAbs).
		AddArguments("config", "--local", "--no-includes", "--get-all", "--null").
		AddDynamicArguments("core.hooksPath").
		WithEnv(gitcmd.CommonGitCmdEnvs()).
		RunStdBytes(ctx)
	if err != nil || !bytes.Equal(stdout, []byte(hookDirAbs+"\x00")) {
		return ErrRestrictedStoreIntegrityUnavailable
	}

	keys, _, err := gitcmd.NewCommand().
		AddOptionFormat("--git-dir=%s", repoDirAbs).
		AddArguments("config", "--local", "--no-includes", "--list", "--null", "--name-only").
		WithEnv(gitcmd.CommonGitCmdEnvs()).
		RunStdBytes(ctx)
	if err != nil {
		return ErrRestrictedStoreIntegrityUnavailable
	}
	for _, key := range strings.Split(string(keys), "\x00") {
		lower := strings.ToLower(key)
		// include directives could be honoured by a future receive process, so their mere presence rejects
		if lower == "include.path" || (strings.HasPrefix(lower, "includeif.") && strings.HasSuffix(lower, ".path")) {
			return ErrRestrictedStoreIntegrityUnavailable
		}
	}
	return nil
}
