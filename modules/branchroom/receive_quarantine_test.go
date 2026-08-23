// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testQuarantineEntry = "incoming-quarantine"

// prepareQuarantineFixture builds a safe root holding <storeID>.git/objects/<entry> and the raw
// values describing it, without creating a real repository.
func prepareQuarantineFixture(t *testing.T) (string, ReceiveQuarantineValues) {
	t.Helper()
	root := newSafeTestRoot(t)
	repoPath := filepath.Join(root, testRestrictedStoreID+".git")
	objectsDir := filepath.Join(repoPath, "objects")
	quarantineDir := filepath.Join(objectsDir, testQuarantineEntry)
	for _, dir := range []string{repoPath, objectsDir, quarantineDir} {
		require.NoError(t, os.Mkdir(dir, 0o755))
	}
	values := ReceiveQuarantineValues{
		ObjectDirectory:            quarantineDir,
		AlternateObjectDirectories: objectsDir,
		QuarantinePath:             quarantineDir,
	}
	return root, values
}

func TestValidateReceiveQuarantineOrdinary(t *testing.T) {
	root, values := prepareQuarantineFixture(t)

	repoPath := filepath.Join(root, testRestrictedStoreID+".git")
	paths := []string{
		root,
		repoPath,
		filepath.Join(repoPath, "objects"),
		filepath.Join(repoPath, "objects", testQuarantineEntry),
	}

	before := snapshotVerifiedStore(t, paths...)
	for range 2 {
		assert.NoError(t, ValidateReceiveQuarantine(root, testRestrictedStoreID, values))
	}
	assert.Equal(t, before, snapshotVerifiedStore(t, paths...), "validation must not mutate any state")
}

func TestValidateReceiveQuarantineInvalidRootAndStore(t *testing.T) {
	root, values := prepareQuarantineFixture(t)

	t.Run("root", func(t *testing.T) {
		for _, restrictedRoot := range []string{"", "relative/root", root + "/", root + "//deeper"} {
			err := ValidateReceiveQuarantine(restrictedRoot, testRestrictedStoreID, values)
			assert.ErrorIs(t, err, ErrInvalidRestrictedRoot, "root %q", restrictedRoot)
			if restrictedRoot != "" {
				assert.NotContains(t, err.Error(), restrictedRoot)
			}
		}
	})

	t.Run("store-id", func(t *testing.T) {
		for _, storeID := range []string{"", "not-a-uuid", "../escape", strings.ToUpper(testRestrictedStoreID)} {
			err := ValidateReceiveQuarantine(root, storeID, values)
			assert.ErrorIs(t, err, ErrInvalidRestrictedStoreID, "storeID %q", storeID)
			if storeID != "" {
				assert.NotContains(t, err.Error(), storeID)
			}
		}
	})
}

func TestValidateReceiveQuarantineRawValues(t *testing.T) {
	root, baseline := prepareQuarantineFixture(t)
	repoPath := filepath.Join(root, testRestrictedStoreID+".git")
	objectsDir := filepath.Join(repoPath, "objects")
	quarantineDir := filepath.Join(objectsDir, testQuarantineEntry)

	tests := []struct {
		name   string
		mutate func(v *ReceiveQuarantineValues) string
	}{
		{"missing-object-directory", func(v *ReceiveQuarantineValues) string { v.ObjectDirectory = ""; return "" }},
		{"relative-object-directory", func(v *ReceiveQuarantineValues) string {
			v.ObjectDirectory = filepath.Join("objects", testQuarantineEntry)
			return v.ObjectDirectory
		}},
		{"nul-object-directory", func(v *ReceiveQuarantineValues) string {
			v.ObjectDirectory = quarantineDir + "\x00"
			return quarantineDir
		}},
		{"unclean-object-directory", func(v *ReceiveQuarantineValues) string { v.ObjectDirectory += "/"; return v.ObjectDirectory }},
		{"missing-quarantine-path", func(v *ReceiveQuarantineValues) string { v.QuarantinePath = ""; return "" }},
		{"object-quarantine-mismatch", func(v *ReceiveQuarantineValues) string {
			v.QuarantinePath = filepath.Join(objectsDir, "other-entry")
			return v.QuarantinePath
		}},
		{"missing-alternates", func(v *ReceiveQuarantineValues) string { v.AlternateObjectDirectories = ""; return "" }},
		{"relative-alternates", func(v *ReceiveQuarantineValues) string {
			v.AlternateObjectDirectories = "objects"
			return v.AlternateObjectDirectories
		}},
		{"nul-alternates", func(v *ReceiveQuarantineValues) string {
			v.AlternateObjectDirectories = objectsDir + "\x00"
			return objectsDir
		}},
		{"alternates-list", func(v *ReceiveQuarantineValues) string {
			v.AlternateObjectDirectories = objectsDir + ":" + filepath.Join(filepath.Dir(repoPath), "other.git", "objects")
			return v.AlternateObjectDirectories
		}},
		{"alternates-quoted", func(v *ReceiveQuarantineValues) string {
			v.AlternateObjectDirectories = `"` + objectsDir + `"`
			return v.AlternateObjectDirectories
		}},
		{"alternates-wrong-store", func(v *ReceiveQuarantineValues) string {
			v.AlternateObjectDirectories = filepath.Join(filepath.Dir(repoPath), "11111111-2222-4333-8444-555555555555.git", "objects")
			return v.AlternateObjectDirectories
		}},
		{"nested-under-quarantine", func(v *ReceiveQuarantineValues) string {
			v.ObjectDirectory = filepath.Join(quarantineDir, "packs", "deep")
			v.QuarantinePath = v.ObjectDirectory
			return v.ObjectDirectory
		}},
		{"parent-repo-path", func(v *ReceiveQuarantineValues) string {
			v.ObjectDirectory = repoPath
			v.QuarantinePath = v.ObjectDirectory
			return v.ObjectDirectory
		}},
		{"parent-root-path", func(v *ReceiveQuarantineValues) string {
			v.ObjectDirectory = root
			v.QuarantinePath = v.ObjectDirectory
			return v.ObjectDirectory
		}},
		{"same-as-main-objects", func(v *ReceiveQuarantineValues) string {
			v.ObjectDirectory = objectsDir
			v.QuarantinePath = v.ObjectDirectory
			return v.ObjectDirectory
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			values := baseline
			raw := tc.mutate(&values)

			err := ValidateReceiveQuarantine(root, testRestrictedStoreID, values)
			assert.ErrorIs(t, err, ErrInvalidReceiveQuarantine)
			if raw != "" {
				assert.NotContains(t, err.Error(), raw)
			}
		})
	}
}

func TestValidateReceiveQuarantineFilesystemState(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(t *testing.T, root, repoPath, objectsDir, quarantineDir string)
		wantErr error
	}{
		{"missing-repo", func(t *testing.T, _, repoPath, _, _ string) {
			require.NoError(t, os.RemoveAll(repoPath))
		}, ErrInvalidReceiveQuarantine},
		{"missing-main-objects", func(t *testing.T, _, _, objectsDir, _ string) {
			require.NoError(t, os.RemoveAll(objectsDir))
		}, ErrInvalidReceiveQuarantine},
		{"missing-quarantine", func(t *testing.T, _, _, _, quarantineDir string) {
			require.NoError(t, os.Remove(quarantineDir))
		}, ErrInvalidReceiveQuarantine},
		{"symlinked-root", nil, ErrUnsafeRestrictedRoot},
		{"symlinked-repo", func(t *testing.T, _, repoPath, _, _ string) {
			outside := filepath.Join(t.TempDir(), "outside.git")
			require.NoError(t, os.Mkdir(outside, 0o700))
			require.NoError(t, os.RemoveAll(repoPath))
			require.NoError(t, os.Symlink(outside, repoPath))
		}, ErrInvalidReceiveQuarantine},
		{"symlinked-main-objects", func(t *testing.T, _, repoPath, objectsDir, _ string) {
			outside := filepath.Join(t.TempDir(), "outside-objects")
			require.NoError(t, os.MkdirAll(filepath.Join(outside, testQuarantineEntry), 0o755))
			require.NoError(t, os.RemoveAll(objectsDir))
			require.NoError(t, os.Symlink(outside, objectsDir))
		}, ErrInvalidReceiveQuarantine},
		{"symlinked-quarantine", func(t *testing.T, _, _, objectsDir, quarantineDir string) {
			outside := filepath.Join(t.TempDir(), "outside-quarantine")
			require.NoError(t, os.Mkdir(outside, 0o755))
			require.NoError(t, os.Remove(quarantineDir))
			require.NoError(t, os.Symlink(outside, quarantineDir))
		}, ErrInvalidReceiveQuarantine},
		{"file-instead-of-repo", func(t *testing.T, _, repoPath, _, _ string) {
			require.NoError(t, os.RemoveAll(repoPath))
			require.NoError(t, os.WriteFile(repoPath, []byte("not a repository\n"), 0o644))
		}, ErrInvalidReceiveQuarantine},
		{"file-instead-of-main-objects", func(t *testing.T, _, _, objectsDir, _ string) {
			require.NoError(t, os.RemoveAll(objectsDir))
			require.NoError(t, os.WriteFile(objectsDir, []byte("not objects\n"), 0o644))
		}, ErrInvalidReceiveQuarantine},
		{"file-instead-of-quarantine", func(t *testing.T, _, _, _, quarantineDir string) {
			require.NoError(t, os.Remove(quarantineDir))
			require.NoError(t, os.WriteFile(quarantineDir, []byte("not a quarantine\n"), 0o644))
		}, ErrInvalidReceiveQuarantine},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "symlinked-root" {
				linkTarget := filepath.Join(t.TempDir(), "outside-marker-root")
				require.NoError(t, os.MkdirAll(linkTarget, 0o700))
				symlinkedRoot := filepath.Join(t.TempDir(), "link-marker-root")
				require.NoError(t, os.Symlink(linkTarget, symlinkedRoot))

				objectsDir := filepath.Join(symlinkedRoot, testRestrictedStoreID+".git", "objects")
				values := ReceiveQuarantineValues{
					ObjectDirectory:            filepath.Join(objectsDir, testQuarantineEntry),
					AlternateObjectDirectories: objectsDir,
					QuarantinePath:             filepath.Join(objectsDir, testQuarantineEntry),
				}
				err := ValidateReceiveQuarantine(symlinkedRoot, testRestrictedStoreID, values)
				assert.ErrorIs(t, err, tc.wantErr)
				assert.NotContains(t, err.Error(), "marker")
				return
			}

			root, values := prepareQuarantineFixture(t)
			repoPath := filepath.Join(root, testRestrictedStoreID+".git")
			objectsDir := filepath.Join(repoPath, "objects")

			tc.mutate(t, root, repoPath, objectsDir, filepath.Join(objectsDir, testQuarantineEntry))

			err := ValidateReceiveQuarantine(root, testRestrictedStoreID, values)
			assert.ErrorIs(t, err, tc.wantErr)
			assert.NotContains(t, err.Error(), root)
			assert.NotContains(t, err.Error(), testRestrictedStoreID)
		})
	}
}

func TestValidateReceiveQuarantineUnavailableRoot(t *testing.T) {
	missingRoot := filepath.Join(t.TempDir(), "missing-marker-root")
	objectsDir := filepath.Join(missingRoot, testRestrictedStoreID+".git", "objects")
	values := ReceiveQuarantineValues{
		ObjectDirectory:            filepath.Join(objectsDir, testQuarantineEntry),
		AlternateObjectDirectories: objectsDir,
		QuarantinePath:             filepath.Join(objectsDir, testQuarantineEntry),
	}

	err := ValidateReceiveQuarantine(missingRoot, testRestrictedStoreID, values)
	assert.ErrorIs(t, err, ErrRestrictedRootUnavailable)
	assert.NotContains(t, err.Error(), "marker")
}
