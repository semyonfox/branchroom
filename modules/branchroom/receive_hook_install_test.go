// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

const testRestrictedStoreID = "3d2f9c8e-4a1b-4c6d-8e2f-9a0b1c2d3e4f"

// newSafeTestRoot keeps the whole parent chain private so tests exercise the same
// local-integrity requirement as a configured restricted root.
func newSafeTestRoot(t *testing.T) string {
	t.Helper()
	parent, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	//nolint:usetesting // t.TempDir does not guarantee the private ancestor this test requires.
	root, err := os.MkdirTemp(parent, ".gitea-branchroom-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	assert.NoError(t, os.Chmod(root, safeHookDirPerm))
	return root
}

func TestInstallRestrictedStoreHookOrdinary(t *testing.T) {
	setHookSettings(t, "sh", "/usr/local/bin/gitea", "/etc/gitea/app.ini")
	root := newSafeTestRoot(t)

	for _, attempt := range []string{"install", "repeat"} {
		t.Run(attempt, func(t *testing.T) {
			hookDir, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
			assert.NoError(t, err)
			assert.Equal(t, filepath.Join(root, ".branchroom-hooks", testRestrictedStoreID), hookDir)

			target := filepath.Join(hookDir, "pre-receive")
			content, err := os.ReadFile(target)
			assert.NoError(t, err)
			assert.Equal(t, ReceivePreReceiveHookTemplate(), string(content))

			info, err := os.Stat(target)
			assert.NoError(t, err)
			assert.True(t, info.Mode().IsRegular())
			assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

			for _, component := range []string{filepath.Join(root, ".branchroom-hooks"), hookDir} {
				dirInfo, err := os.Stat(component)
				assert.NoError(t, err)
				assert.True(t, dirInfo.IsDir())
				assert.Zero(t, dirInfo.Mode().Perm()&0o077)
			}

			entries, err := os.ReadDir(hookDir)
			assert.NoError(t, err)
			assert.Len(t, entries, 1)
			assert.Equal(t, "pre-receive", entries[0].Name())
		})
	}
}

func TestInstallRestrictedStoreHookScriptContentIsServerOwned(t *testing.T) {
	setHookSettings(t, "sh", "/usr/local/bin/gitea", "/etc/gitea/app.ini")
	root := newSafeTestRoot(t)

	hookDir, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
	assert.NoError(t, err)

	content, err := os.ReadFile(filepath.Join(hookDir, "pre-receive"))
	assert.NoError(t, err)
	assert.NotContains(t, string(content), testRestrictedStoreID)
	assert.NotContains(t, string(content), root)
}

func TestInstallRestrictedStoreHookInvalidInputs(t *testing.T) {
	setHookSettings(t, "sh", "/usr/local/bin/gitea", "/etc/gitea/app.ini")
	root := newSafeTestRoot(t)

	t.Run("store ID", func(t *testing.T) {
		for _, storeID := range []string{
			"",
			"not-a-uuid",
			"../escape",
			"3D2F9C8E-4A1B-4C6D-8E2F-9A0B1C2D3E4F",
			"{3d2f9c8e-4a1b-4c6d-8e2f-9a0b1c2d3e4f}",
			"3d2f9c8e4a1b4c6d8e2f9a0b1c2d3e4f",
		} {
			hookDir, err := InstallRestrictedStoreHook(root, storeID)
			assert.ErrorIs(t, err, ErrInvalidRestrictedStoreID, "storeID %q", storeID)
			assert.Empty(t, hookDir)
		}
		entries, err := os.ReadDir(root)
		assert.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("root", func(t *testing.T) {
		for _, restrictedRoot := range []string{
			"",
			"relative/root",
			filepath.Join(root, "sub") + "/",
			filepath.Join(root, "sub") + "//deeper",
		} {
			hookDir, err := InstallRestrictedStoreHook(restrictedRoot, testRestrictedStoreID)
			assert.ErrorIs(t, err, ErrInvalidRestrictedRoot, "root %q", restrictedRoot)
			assert.Empty(t, hookDir)
		}
	})
}

func TestInstallRestrictedStoreHookUnsafeRoot(t *testing.T) {
	setHookSettings(t, "sh", "/usr/local/bin/gitea", "/etc/gitea/app.ini")

	t.Run("symlink", func(t *testing.T) {
		realRoot := filepath.Join(t.TempDir(), "outside-marker-root")
		assert.NoError(t, os.MkdirAll(realRoot, 0o700))
		symlinkedRoot := filepath.Join(t.TempDir(), "link-marker-root")
		assert.NoError(t, os.Symlink(realRoot, symlinkedRoot))

		hookDir, err := InstallRestrictedStoreHook(symlinkedRoot, testRestrictedStoreID)
		assert.ErrorIs(t, err, ErrUnsafeRestrictedRoot)
		assert.Empty(t, hookDir)
		assert.NotContains(t, err.Error(), "marker")

		entries, readErr := os.ReadDir(realRoot)
		assert.NoError(t, readErr)
		assert.Empty(t, entries)
	})

	t.Run("intermediate-symlink", func(t *testing.T) {
		base := t.TempDir()
		linkTarget := filepath.Join(t.TempDir(), "outside-marker-parent")
		assert.NoError(t, os.MkdirAll(linkTarget, 0o700))
		assert.NoError(t, os.Symlink(linkTarget, filepath.Join(base, "link")))
		root := filepath.Join(base, "link", "real-marker-root")
		assert.NoError(t, os.MkdirAll(root, 0o700))

		hookDir, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
		assert.ErrorIs(t, err, ErrUnsafeRestrictedRoot)
		assert.Empty(t, hookDir)
		assert.NotContains(t, err.Error(), "marker")

		entries, readErr := os.ReadDir(root)
		assert.NoError(t, readErr)
		assert.Empty(t, entries)
	})

	t.Run("group-writable", func(t *testing.T) {
		root := t.TempDir()
		assert.NoError(t, os.Chmod(root, 0o775))

		hookDir, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
		assert.ErrorIs(t, err, ErrUnsafeRestrictedRoot)
		assert.Empty(t, hookDir)
		assert.NotContains(t, err.Error(), root)
	})

	t.Run("world-writable", func(t *testing.T) {
		root := t.TempDir()
		assert.NoError(t, os.Chmod(root, 0o777))

		hookDir, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
		assert.ErrorIs(t, err, ErrUnsafeRestrictedRoot)
		assert.Empty(t, hookDir)
		assert.NotContains(t, err.Error(), root)
	})

	if runtime.GOOS != "windows" {
		t.Run("non-sticky-writable-parent", func(t *testing.T) {
			parent := filepath.Join(newSafeTestRoot(t), "writable-parent")
			root := filepath.Join(parent, "restricted-root")
			assert.NoError(t, os.MkdirAll(root, 0o700))
			assert.NoError(t, os.Chmod(parent, 0o777))

			hookDir, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
			assert.ErrorIs(t, err, ErrUnsafeRestrictedRoot)
			assert.Empty(t, hookDir)
		})

		t.Run("sticky-writable-parent-owned-root", func(t *testing.T) {
			parent := filepath.Join(newSafeTestRoot(t), "sticky-parent")
			root := filepath.Join(parent, "restricted-root")
			assert.NoError(t, os.MkdirAll(root, 0o700))
			assert.NoError(t, os.Chmod(parent, 0o777|os.ModeSticky))

			hookDir, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
			assert.NoError(t, err)
			assert.NotEmpty(t, hookDir)
		})
	}

	t.Run("nonexistent", func(t *testing.T) {
		missingRoot := filepath.Join(t.TempDir(), "missing-marker-root")

		hookDir, err := InstallRestrictedStoreHook(missingRoot, testRestrictedStoreID)
		assert.ErrorIs(t, err, ErrRestrictedRootUnavailable)
		assert.Empty(t, hookDir)
		assert.NotContains(t, err.Error(), "marker")
	})
}

func TestInstallRestrictedStoreHookRootIsFile(t *testing.T) {
	setHookSettings(t, "sh", "/usr/local/bin/gitea", "/etc/gitea/app.ini")
	root := filepath.Join(t.TempDir(), "file-marker-root")
	assert.NoError(t, os.WriteFile(root, []byte("not a directory"), 0o600))

	hookDir, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
	assert.ErrorIs(t, err, ErrUnsafeRestrictedRoot)
	assert.Empty(t, hookDir)
	assert.NotContains(t, err.Error(), "marker")
}

func TestInstallRestrictedStoreHookSetIDBitExistingTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("setuid/setgid bits are not supported on windows")
	}
	setHookSettings(t, "sh", "/usr/local/bin/gitea", "/etc/gitea/app.ini")
	content := []byte(ReceivePreReceiveHookTemplate())

	// Go FileModes carry setuid/setgid as type bits, not Unix high permission bits
	for _, tc := range []struct {
		name  string
		chmod fs.FileMode
		want  fs.FileMode
	}{
		{"setuid", fs.ModeSetuid | 0o700, fs.ModeSetuid | 0o700},
		{"setgid", fs.ModeSetgid | 0o700, fs.ModeSetgid | 0o700},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := prepareUnsafeTargetRoot(t, content, tc.chmod)

			hookDir, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
			assert.ErrorIs(t, err, ErrUnsafeReceiveHook)
			assert.Empty(t, hookDir)

			target := filepath.Join(root, ".branchroom-hooks", testRestrictedStoreID, "pre-receive")
			existing, readErr := os.ReadFile(target)
			assert.NoError(t, readErr)
			assert.Equal(t, content, existing)
			info, statErr := os.Stat(target)
			assert.NoError(t, statErr)
			assert.Equal(t, tc.want, info.Mode())
		})
	}
}

func TestInstallRestrictedStoreHookOperationalFailureCollapsed(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permission-based failure requires a non-root user")
	}
	setHookSettings(t, "sh", "/usr/local/bin/gitea", "/etc/gitea/app.ini")
	root := newSafeTestRoot(t)
	hooksDir := filepath.Join(root, branchroomHooksDirName)
	assert.NoError(t, os.Mkdir(hooksDir, safeHookDirPerm))
	// owner-writable bits removed so creating the store subdirectory fails operationally
	assert.NoError(t, os.Chmod(hooksDir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(hooksDir, 0o700) })

	hookDir, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
	assert.ErrorIs(t, err, ErrReceiveHookUnavailable)
	assert.NotErrorIs(t, err, ErrUnsafeReceiveHook)
	assert.Empty(t, hookDir)
	assert.NotContains(t, err.Error(), testRestrictedStoreID)
	assert.NotContains(t, err.Error(), root)

	entries, readErr := os.ReadDir(hooksDir)
	assert.NoError(t, readErr)
	assert.Empty(t, entries)
}

func TestInstallRestrictedStoreHookSyncsDirectoryEntries(t *testing.T) {
	setHookSettings(t, "sh", "/usr/local/bin/gitea", "/etc/gitea/app.ini")
	root := newSafeTestRoot(t)

	var synced []string
	original := syncHookDirectoryEntry
	syncHookDirectoryEntry = func(r *os.Root, name string) error {
		synced = append(synced, name)
		return original(r, name)
	}
	t.Cleanup(func() { syncHookDirectoryEntry = original })

	_, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
	assert.NoError(t, err)
	assert.Equal(t, []string{
		".",
		branchroomHooksDirName,
		path.Join(branchroomHooksDirName, testRestrictedStoreID),
	}, synced)
}

func TestInstallRestrictedStoreHookSyncFailureCollapsed(t *testing.T) {
	setHookSettings(t, "sh", "/usr/local/bin/gitea", "/etc/gitea/app.ini")
	injected := errors.New("injected sync failure")

	for _, tc := range []struct {
		name       string
		failOnCall int
	}{
		{name: "restricted-root-entry", failOnCall: 1},
		{name: "hooks-dir-entry", failOnCall: 2},
		{name: "store-dir-entry", failOnCall: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newSafeTestRoot(t)
			calls := 0
			original := syncHookDirectoryEntry
			syncHookDirectoryEntry = func(r *os.Root, name string) error {
				calls++
				if calls == tc.failOnCall {
					return injected
				}
				return original(r, name)
			}
			t.Cleanup(func() { syncHookDirectoryEntry = original })

			hookDir, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
			assert.ErrorIs(t, err, ErrReceiveHookUnavailable)
			assert.NotErrorIs(t, err, ErrUnsafeReceiveHook)
			assert.Empty(t, hookDir)
			assert.NotContains(t, err.Error(), injected.Error())
			assert.NotContains(t, err.Error(), root)
			assert.NotContains(t, err.Error(), testRestrictedStoreID)

			storeHooksDir := filepath.Join(root, branchroomHooksDirName, testRestrictedStoreID)
			switch tc.failOnCall {
			case 1:
				entries, readErr := os.ReadDir(filepath.Join(root, branchroomHooksDirName))
				assert.NoError(t, readErr)
				assert.Empty(t, entries)
			case 2:
				entries, readErr := os.ReadDir(storeHooksDir)
				assert.NoError(t, readErr)
				assert.Empty(t, entries)
			case 3:
				content, readErr := os.ReadFile(filepath.Join(storeHooksDir, preReceiveHookFileName))
				assert.NoError(t, readErr)
				assert.Equal(t, ReceivePreReceiveHookTemplate(), string(content))

				// the durable state on disk is correct, so a repeat install must succeed once syncs work again
				syncHookDirectoryEntry = original
				repeatDir, repeatErr := InstallRestrictedStoreHook(root, testRestrictedStoreID)
				assert.NoError(t, repeatErr)
				assert.Equal(t, storeHooksDir, repeatDir)
			}
		})
	}
}

func TestInstallRestrictedStoreHookSymlinkedHooksDirectory(t *testing.T) {
	setHookSettings(t, "sh", "/usr/local/bin/gitea", "/etc/gitea/app.ini")
	root, outside := newSafeTestRoot(t), filepath.Join(t.TempDir(), "outside")
	assert.NoError(t, os.MkdirAll(outside, 0o700))
	assert.NoError(t, os.Symlink(outside, filepath.Join(root, ".branchroom-hooks")))

	hookDir, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
	assert.ErrorIs(t, err, ErrUnsafeReceiveHook)
	assert.Empty(t, hookDir)

	entries, err := os.ReadDir(outside)
	assert.NoError(t, err)
	assert.Empty(t, entries)
}

func TestInstallRestrictedStoreHookSymlinkedTarget(t *testing.T) {
	setHookSettings(t, "sh", "/usr/local/bin/gitea", "/etc/gitea/app.ini")
	root, outside := newSafeTestRoot(t), filepath.Join(t.TempDir(), "outside.sh")
	storeHooksDir := filepath.Join(root, ".branchroom-hooks", testRestrictedStoreID)
	assert.NoError(t, os.MkdirAll(storeHooksDir, 0o700))
	assert.NoError(t, os.WriteFile(outside, []byte("#!/bin/sh\n"), 0o700))
	assert.NoError(t, os.Symlink(outside, filepath.Join(storeHooksDir, "pre-receive")))

	hookDir, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
	assert.ErrorIs(t, err, ErrUnsafeReceiveHook)
	assert.Empty(t, hookDir)

	content, err := os.ReadFile(outside)
	assert.NoError(t, err)
	assert.Equal(t, "#!/bin/sh\n", string(content))

	entries, err := os.ReadDir(storeHooksDir)
	assert.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestInstallRestrictedStoreHookUnsafeExistingTarget(t *testing.T) {
	setHookSettings(t, "sh", "/usr/local/bin/gitea", "/etc/gitea/app.ini")
	content := ReceivePreReceiveHookTemplate()

	t.Run("wrong content", func(t *testing.T) {
		root := prepareUnsafeTargetRoot(t, []byte("#!/bin/sh\nrm -rf /\n"), 0o700)
		_, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
		assert.ErrorIs(t, err, ErrUnsafeReceiveHook)
		assertUnsafeTargetUntouched(t, root, []byte("#!/bin/sh\nrm -rf /\n"), 0o700)
	})

	t.Run("not owner-executable", func(t *testing.T) {
		root := prepareUnsafeTargetRoot(t, []byte(content), 0o600)
		_, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
		assert.ErrorIs(t, err, ErrUnsafeReceiveHook)
		assertUnsafeTargetUntouched(t, root, []byte(content), 0o600)
	})

	t.Run("group-writable", func(t *testing.T) {
		root := prepareUnsafeTargetRoot(t, []byte(content), 0o720)
		_, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
		assert.ErrorIs(t, err, ErrUnsafeReceiveHook)
		assertUnsafeTargetUntouched(t, root, []byte(content), 0o720)
	})

	t.Run("directory instead of file", func(t *testing.T) {
		root := newSafeTestRoot(t)
		target := filepath.Join(root, ".branchroom-hooks", testRestrictedStoreID, "pre-receive")
		assert.NoError(t, os.MkdirAll(target, 0o700))
		_, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
		assert.ErrorIs(t, err, ErrUnsafeReceiveHook)

		info, statErr := os.Stat(target)
		assert.NoError(t, statErr)
		assert.True(t, info.IsDir())
	})
}

func prepareUnsafeTargetRoot(t *testing.T, content []byte, perm os.FileMode) string {
	t.Helper()
	root := newSafeTestRoot(t)
	target := filepath.Join(root, ".branchroom-hooks", testRestrictedStoreID, "pre-receive")
	assert.NoError(t, os.MkdirAll(filepath.Dir(target), 0o700))
	assert.NoError(t, os.WriteFile(target, content, 0o600))
	// chmod after creation because umask would mask the requested mode on open
	assert.NoError(t, os.Chmod(target, perm))
	return root
}

func assertUnsafeTargetUntouched(t *testing.T, root string, content []byte, perm os.FileMode) {
	t.Helper()
	target := filepath.Join(root, ".branchroom-hooks", testRestrictedStoreID, "pre-receive")
	existing, err := os.ReadFile(target)
	assert.NoError(t, err)
	assert.Equal(t, content, existing)
	info, err := os.Stat(target)
	assert.NoError(t, err)
	assert.Equal(t, perm, info.Mode().Perm())
	entries, err := os.ReadDir(filepath.Dir(target))
	assert.NoError(t, err)
	assert.Len(t, entries, 1)
}
