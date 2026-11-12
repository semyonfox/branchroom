// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"gitea.dev/modules/git"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// prepareVerifiedStore builds a healthy ready-store: installed static hook plus an initialized
// bare repository whose local config points core.hooksPath at the derived hook directory.
func prepareVerifiedStore(t *testing.T) string {
	t.Helper()
	setHookSettings(t, "sh", "/usr/local/bin/gitea", "/etc/gitea/app.ini")
	root := newSafeTestRoot(t)

	_, err := InstallRestrictedStoreHook(root, testRestrictedStoreID)
	require.NoError(t, err)

	repoPath := filepath.Join(root, testRestrictedStoreID+".git")
	require.NoError(t, git.InitRepositoryLocal(t.Context(), repoPath, true, git.Sha1ObjectFormat.Name(), ""))
	// pin deterministic modes because git init honours the umask
	require.NoError(t, os.Chmod(repoPath, 0o755))
	require.NoError(t, os.Chmod(filepath.Join(repoPath, "config"), 0o644))

	addStoreLocalConfigValue(t, repoPath, filepath.Join(root, branchroomHooksDirName, testRestrictedStoreID))
	return root
}

func addStoreLocalConfigValue(t *testing.T, repoPath, value string) {
	t.Helper()
	_, _, err := gitcmd.NewCommand().
		AddOptionFormat("--git-dir=%s", repoPath).
		AddArguments("config", "--add").
		AddDynamicArguments("core.hooksPath", value).
		RunStdString(t.Context())
	require.NoError(t, err)
}

func unsetStoreLocalConfigValue(t *testing.T, repoPath string) {
	t.Helper()
	_, _, err := gitcmd.NewCommand().
		AddOptionFormat("--git-dir=%s", repoPath).
		AddArguments("config", "--unset").
		AddDynamicArguments("core.hooksPath").
		RunStdString(t.Context())
	require.NoError(t, err)
}

func appendStoreLocalConfigLines(t *testing.T, repoPath, lines string) {
	t.Helper()
	configPath := filepath.Join(repoPath, "config")
	file, err := os.OpenFile(configPath, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, wErr := file.WriteString(lines)
	require.NoError(t, file.Close())
	require.NoError(t, wErr)
}

func verifiedStoreRepoPath(root string) string {
	return filepath.Join(root, testRestrictedStoreID+".git")
}

func verifiedStoreHookDir(root string) string {
	return filepath.Join(root, branchroomHooksDirName, testRestrictedStoreID)
}

func assertIntegrityRejected(t *testing.T, root string, err error) {
	t.Helper()
	assert.ErrorIs(t, err, ErrRestrictedStoreIntegrityUnavailable)
	assert.NotContains(t, err.Error(), root)
	assert.NotContains(t, err.Error(), testRestrictedStoreID)
	assert.NotContains(t, err.Error(), "fatal:")
}

type storeState struct {
	mode fs.FileMode
	size int64
}

// snapshotVerifiedStore captures modes and sizes so tests can prove verification never repairs or mutates anything.
func snapshotVerifiedStore(t *testing.T, paths ...string) map[string]storeState {
	t.Helper()
	snapshot := make(map[string]storeState, len(paths))
	for _, p := range paths {
		info, err := os.Lstat(p)
		require.NoError(t, err)
		snapshot[p] = storeState{mode: info.Mode(), size: info.Size()}
	}
	return snapshot
}

func verifiedStoreSnapshotPaths(root string) []string {
	repoPath := verifiedStoreRepoPath(root)
	return []string{
		root,
		filepath.Join(root, branchroomHooksDirName),
		verifiedStoreHookDir(root),
		filepath.Join(verifiedStoreHookDir(root), preReceiveHookFileName),
		repoPath,
		filepath.Join(repoPath, "config"),
	}
}

func TestVerifyRestrictedStoreHookOrdinary(t *testing.T) {
	root := prepareVerifiedStore(t)

	before := snapshotVerifiedStore(t, verifiedStoreSnapshotPaths(root)...)
	for range 2 {
		assert.NoError(t, VerifyRestrictedStoreHook(t.Context(), root, testRestrictedStoreID))
	}
	assert.Equal(t, before, snapshotVerifiedStore(t, verifiedStoreSnapshotPaths(root)...), "verification must not mutate any state")
}

func TestVerifyRestrictedStoreHookInvalidInputs(t *testing.T) {
	root := prepareVerifiedStore(t)

	t.Run("store ID", func(t *testing.T) {
		for _, storeID := range []string{
			"",
			"not-a-uuid",
			"../escape",
			"3D2F9C8E-4A1B-4C6D-8E2F-9A0B1C2D3E4F",
			"{3d2f9c8e-4a1b-4c6d-8e2f-9a0b1c2d3e4f}",
			"3d2f9c8e4a1b4c6d8e2f9a0b1c2d3e4f",
		} {
			err := VerifyRestrictedStoreHook(t.Context(), root, storeID)
			assert.ErrorIs(t, err, ErrInvalidRestrictedStoreID, "storeID %q", storeID)
			if storeID != "" {
				assert.NotContains(t, err.Error(), storeID)
			}
		}
	})

	t.Run("root", func(t *testing.T) {
		for _, restrictedRoot := range []string{
			"",
			"relative/root",
			filepath.Join(root, "sub") + "/",
			filepath.Join(root, "sub") + "//deeper",
		} {
			err := VerifyRestrictedStoreHook(t.Context(), restrictedRoot, testRestrictedStoreID)
			assert.ErrorIs(t, err, ErrInvalidRestrictedRoot, "root %q", restrictedRoot)
			if restrictedRoot != "" {
				assert.NotContains(t, err.Error(), restrictedRoot)
			}
		}
	})

	t.Run("unavailable-root", func(t *testing.T) {
		missingRoot := filepath.Join(t.TempDir(), "missing-marker-root")
		err := VerifyRestrictedStoreHook(t.Context(), missingRoot, testRestrictedStoreID)
		assert.ErrorIs(t, err, ErrRestrictedRootUnavailable)
		assert.NotContains(t, err.Error(), "marker")
	})

	t.Run("unsafe-roots", func(t *testing.T) {
		linkTarget := filepath.Join(t.TempDir(), "outside-marker-root")
		require.NoError(t, os.MkdirAll(linkTarget, 0o700))
		symlinkedRoot := filepath.Join(t.TempDir(), "link-marker-root")
		require.NoError(t, os.Symlink(linkTarget, symlinkedRoot))
		err := VerifyRestrictedStoreHook(t.Context(), symlinkedRoot, testRestrictedStoreID)
		assert.ErrorIs(t, err, ErrUnsafeRestrictedRoot)
		assert.NotContains(t, err.Error(), "marker")

		groupWritableRoot := t.TempDir()
		require.NoError(t, os.Chmod(groupWritableRoot, 0o775))
		err = VerifyRestrictedStoreHook(t.Context(), groupWritableRoot, testRestrictedStoreID)
		assert.ErrorIs(t, err, ErrUnsafeRestrictedRoot)
		assert.NotContains(t, err.Error(), groupWritableRoot)
	})
}

func TestVerifyRestrictedStoreHookHooksPathConfig(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(t *testing.T, root string)
		skipOnWin bool
	}{
		{name: "missing-key", mutate: func(t *testing.T, root string) {
			unsetStoreLocalConfigValue(t, verifiedStoreRepoPath(root))
		}},
		{name: "duplicate", mutate: func(t *testing.T, root string) {
			addStoreLocalConfigValue(t, verifiedStoreRepoPath(root), "/dup/marker")
		}},
		{name: "wrong-value", mutate: func(t *testing.T, root string) {
			addStoreLocalConfigValue(t, verifiedStoreRepoPath(root), "/wrong/marker")
		}},
		{name: "relative-value", mutate: func(t *testing.T, root string) {
			addStoreLocalConfigValue(t, verifiedStoreRepoPath(root), "hooks/bin")
		}},
		{name: "malformed-config", mutate: func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(filepath.Join(verifiedStoreRepoPath(root), "config"), []byte("[core\n\thooksPath = "), 0o644))
		}},
		{name: "unreadable-config", skipOnWin: true, mutate: func(t *testing.T, root string) {
			if os.Getuid() == 0 {
				t.Skip("permission-based failure requires a non-root user")
			}
			configPath := filepath.Join(verifiedStoreRepoPath(root), "config")
			require.NoError(t, os.Chmod(configPath, 0o000))
			t.Cleanup(func() { _ = os.Chmod(configPath, 0o644) })
		}},
		{name: "global-only", mutate: func(t *testing.T, root string) {
			unsetStoreLocalConfigValue(t, verifiedStoreRepoPath(root))
			gitHome := t.TempDir()
			globalConfig := "[core]\n\thooksPath = /global/marker\n"
			require.NoError(t, os.WriteFile(filepath.Join(gitHome, ".gitconfig"), []byte(globalConfig), 0o600))
			defer test.MockVariableValue(&setting.Git.HomePath, gitHome)()
		}},
	}

	for _, tc := range tests {
		if tc.skipOnWin && runtime.GOOS == "windows" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			root := prepareVerifiedStore(t)

			tc.mutate(t, root)

			err := VerifyRestrictedStoreHook(t.Context(), root, testRestrictedStoreID)
			assertIntegrityRejected(t, root, err)
			for _, secret := range []string{"/dup/marker", "/wrong/marker", "/global/marker"} {
				assert.NotContains(t, err.Error(), secret)
			}
		})
	}
}

func TestVerifyRestrictedStoreHookIncludeDirectives(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lines string
	}{
		{"include-path", "[include]\n\tpath = /etc/evil-marker\n"},
		{"include-if-path", "[includeIf \"gitdir:/x/\"]\n\tpath = /etc/evil-marker\n"},
		{"include-path-uppercase-key", "[include]\n\tPATH = /etc/evil-marker\n"},
		{"include-if-mixed-case-section", "[IncludeIf \"gitdir:/x/\"]\n\tpath = /etc/evil-marker\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := prepareVerifiedStore(t)
			appendStoreLocalConfigLines(t, verifiedStoreRepoPath(root), tc.lines)

			assertIntegrityRejected(t, root, VerifyRestrictedStoreHook(t.Context(), root, testRestrictedStoreID))
		})
	}
}

func TestVerifyRestrictedStoreHookUnsafeHookTree(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(t *testing.T, root string)
		check     func(t *testing.T, root string)
		skipOnWin bool
	}{
		{"wrong-content", func(t *testing.T, root string) {
			target := filepath.Join(verifiedStoreHookDir(root), preReceiveHookFileName)
			require.NoError(t, os.WriteFile(target, []byte("#!/bin/sh\nrm -rf /\n"), 0o700))
		}, func(t *testing.T, root string) {
			target := filepath.Join(verifiedStoreHookDir(root), preReceiveHookFileName)
			existing, err := os.ReadFile(target)
			assert.NoError(t, err)
			assert.Equal(t, "#!/bin/sh\nrm -rf /\n", string(existing))
		}, false},
		{"not-owner-executable", func(t *testing.T, root string) {
			require.NoError(t, os.Chmod(filepath.Join(verifiedStoreHookDir(root), preReceiveHookFileName), 0o600))
		}, func(t *testing.T, root string) {
			info, err := os.Stat(filepath.Join(verifiedStoreHookDir(root), preReceiveHookFileName))
			assert.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		}, false},
		{"group-writable-hook", func(t *testing.T, root string) {
			require.NoError(t, os.Chmod(filepath.Join(verifiedStoreHookDir(root), preReceiveHookFileName), 0o720))
		}, func(t *testing.T, root string) {
			info, err := os.Stat(filepath.Join(verifiedStoreHookDir(root), preReceiveHookFileName))
			assert.NoError(t, err)
			assert.Equal(t, os.FileMode(0o720), info.Mode().Perm())
		}, false},
		{"setgid-hook", func(t *testing.T, root string) {
			require.NoError(t, os.Chmod(filepath.Join(verifiedStoreHookDir(root), preReceiveHookFileName), fs.ModeSetgid|0o700))
		}, func(t *testing.T, root string) {
			info, err := os.Stat(filepath.Join(verifiedStoreHookDir(root), preReceiveHookFileName))
			assert.NoError(t, err)
			assert.NotZero(t, info.Mode()&fs.ModeSetgid)
		}, true},
		{"symlinked-hook", func(t *testing.T, root string) {
			outside := filepath.Join(t.TempDir(), "outside.sh")
			require.NoError(t, os.WriteFile(outside, []byte("#!/bin/sh\n"), 0o700))
			target := filepath.Join(verifiedStoreHookDir(root), preReceiveHookFileName)
			require.NoError(t, os.Remove(target))
			require.NoError(t, os.Symlink(outside, target))
		}, func(t *testing.T, root string) {
			entries, err := os.ReadDir(verifiedStoreHookDir(root))
			assert.NoError(t, err)
			assert.Len(t, entries, 1)
			info, statErr := os.Lstat(filepath.Join(verifiedStoreHookDir(root), preReceiveHookFileName))
			assert.NoError(t, statErr)
			assert.NotZero(t, info.Mode()&fs.ModeSymlink)
		}, false},
		{"extra-hook-entry", func(t *testing.T, root string) {
			require.NoError(t, os.WriteFile(filepath.Join(verifiedStoreHookDir(root), "post-receive"), []byte("#!/bin/sh\n"), 0o700))
		}, func(t *testing.T, root string) {
			entries, err := os.ReadDir(verifiedStoreHookDir(root))
			assert.NoError(t, err)
			assert.Len(t, entries, 2)
		}, false},
		{"missing-hook-file", func(t *testing.T, root string) {
			require.NoError(t, os.Remove(filepath.Join(verifiedStoreHookDir(root), preReceiveHookFileName)))
		}, nil, false},
		{"missing-store-hook-dir", func(t *testing.T, root string) {
			require.NoError(t, os.RemoveAll(verifiedStoreHookDir(root)))
		}, nil, false},
		{"group-writable-hooks-dir", func(t *testing.T, root string) {
			require.NoError(t, os.Chmod(filepath.Join(root, branchroomHooksDirName), 0o775))
		}, func(t *testing.T, root string) {
			info, err := os.Stat(filepath.Join(root, branchroomHooksDirName))
			assert.NoError(t, err)
			assert.Equal(t, os.FileMode(0o775), info.Mode().Perm())
		}, true},
		{"world-writable-store-hook-dir", func(t *testing.T, root string) {
			require.NoError(t, os.Chmod(verifiedStoreHookDir(root), 0o772))
		}, func(t *testing.T, root string) {
			info, err := os.Stat(verifiedStoreHookDir(root))
			assert.NoError(t, err)
			assert.Equal(t, os.FileMode(0o772), info.Mode().Perm())
		}, true},
		{"symlinked-store-hook-dir", func(t *testing.T, root string) {
			outside := filepath.Join(t.TempDir(), "outside-hooks")
			require.NoError(t, os.MkdirAll(outside, 0o700))
			require.NoError(t, os.RemoveAll(verifiedStoreHookDir(root)))
			require.NoError(t, os.Symlink(outside, verifiedStoreHookDir(root)))
		}, nil, false},
	}

	for _, tc := range tests {
		if tc.skipOnWin && runtime.GOOS == "windows" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			root := prepareVerifiedStore(t)

			tc.mutate(t, root)

			assertIntegrityRejected(t, root, VerifyRestrictedStoreHook(t.Context(), root, testRestrictedStoreID))
			if tc.check != nil {
				tc.check(t, root)
			}
		})
	}
}

func TestVerifyRestrictedStoreHookUnsafeStoreAndConfig(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, root string)
		check  func(t *testing.T, root string)
	}{
		{"missing-store-repo", func(t *testing.T, root string) {
			require.NoError(t, os.RemoveAll(verifiedStoreRepoPath(root)))
		}, nil},
		{"regular-file-store-repo", func(t *testing.T, root string) {
			require.NoError(t, os.RemoveAll(verifiedStoreRepoPath(root)))
			require.NoError(t, os.WriteFile(verifiedStoreRepoPath(root), []byte("not a repository\n"), 0o644))
		}, func(t *testing.T, root string) {
			info, err := os.Lstat(verifiedStoreRepoPath(root))
			assert.NoError(t, err)
			assert.True(t, info.Mode().IsRegular())
			assert.Equal(t, int64(len("not a repository\n")), info.Size())
		}},
		{"symlinked-store-repo", func(t *testing.T, root string) {
			outside := filepath.Join(t.TempDir(), "outside.git")
			require.NoError(t, os.MkdirAll(outside, 0o700))
			require.NoError(t, os.RemoveAll(verifiedStoreRepoPath(root)))
			require.NoError(t, os.Symlink(outside, verifiedStoreRepoPath(root)))
		}, nil},
		{"group-writable-store-repo", func(t *testing.T, root string) {
			require.NoError(t, os.Chmod(verifiedStoreRepoPath(root), 0o730))
		}, func(t *testing.T, root string) {
			info, err := os.Stat(verifiedStoreRepoPath(root))
			assert.NoError(t, err)
			assert.Equal(t, os.FileMode(0o730), info.Mode().Perm())
		}},
		{"missing-config", func(t *testing.T, root string) {
			require.NoError(t, os.Remove(filepath.Join(verifiedStoreRepoPath(root), "config")))
		}, nil},
		{"symlinked-config", func(t *testing.T, root string) {
			outside := filepath.Join(t.TempDir(), "outside-config")
			require.NoError(t, os.WriteFile(outside, []byte("[core]\n"), 0o644))
			configPath := filepath.Join(verifiedStoreRepoPath(root), "config")
			require.NoError(t, os.Remove(configPath))
			require.NoError(t, os.Symlink(outside, configPath))
		}, func(t *testing.T, root string) {
			info, err := os.Lstat(filepath.Join(verifiedStoreRepoPath(root), "config"))
			assert.NoError(t, err)
			assert.NotZero(t, info.Mode()&fs.ModeSymlink)
		}},
		{"group-writable-config", func(t *testing.T, root string) {
			require.NoError(t, os.Chmod(filepath.Join(verifiedStoreRepoPath(root), "config"), 0o664))
		}, func(t *testing.T, root string) {
			info, err := os.Stat(filepath.Join(verifiedStoreRepoPath(root), "config"))
			assert.NoError(t, err)
			assert.Equal(t, os.FileMode(0o664), info.Mode().Perm())
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := prepareVerifiedStore(t)

			tc.mutate(t, root)

			assertIntegrityRejected(t, root, VerifyRestrictedStoreHook(t.Context(), root, testRestrictedStoreID))
			if tc.check != nil {
				tc.check(t, root)
			}
		})
	}
}
