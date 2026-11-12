// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BranchroomConfig contains the static configuration for Branchroom foundations.
type BranchroomConfig struct {
	Enabled                bool
	RestrictedRepoRootPath string
	MaxWorkspacesPerOwner  int
	MaxGitRequestSize      int64
	GitRequestTimeout      time.Duration
}

const (
	defaultBranchroomMaxGitRequestSize = 32 << 20
	defaultBranchroomGitRequestTimeout = 2 * time.Minute
)

// GitRequestSizeLimit bounds a gateway request body, which is held in memory before Git starts.
func (c BranchroomConfig) GitRequestSizeLimit() int64 {
	if c.MaxGitRequestSize > 0 {
		return c.MaxGitRequestSize
	}
	return defaultBranchroomMaxGitRequestSize
}

// GitRequestTimeLimit bounds reading a gateway request body and running receive-pack.
func (c BranchroomConfig) GitRequestTimeLimit() time.Duration {
	if c.GitRequestTimeout > 0 {
		return c.GitRequestTimeout
	}
	return defaultBranchroomGitRequestTimeout
}

// Branchroom stores the static Branchroom configuration.
var Branchroom BranchroomConfig

func loadBranchroomFrom(rootCfg ConfigProvider) error {
	sec := rootCfg.Section("branchroom")
	Branchroom = BranchroomConfig{
		Enabled:               sec.Key("ENABLED").MustBool(false),
		MaxWorkspacesPerOwner: sec.Key("MAX_WORKSPACES_PER_OWNER").MustInt(10),
		MaxGitRequestSize:     sec.Key("MAX_GIT_REQUEST_SIZE").MustInt64(defaultBranchroomMaxGitRequestSize),
		GitRequestTimeout:     sec.Key("GIT_REQUEST_TIMEOUT").MustDuration(defaultBranchroomGitRequestTimeout),
	}
	if !Branchroom.Enabled {
		configured := strings.TrimSpace(sec.Key("RESTRICTED_ROOT").String())
		if configured != "" {
			if !filepath.IsAbs(configured) {
				configured = filepath.Join(AppWorkPath, configured)
			}
			resolved, err := filepath.EvalSymlinks(configured)
			if err == nil {
				configured = resolved
			}
			Branchroom.RestrictedRepoRootPath = filepath.Clean(configured)
		}
		return nil
	}
	if DatabaseType(rootCfg.Section("database").Key("DB_TYPE").String()).IsMSSQL() {
		return errors.New("[branchroom] does not support MSSQL")
	}
	if !DisableGitHooks {
		return errors.New("[branchroom] requires [security] DISABLE_GIT_HOOKS, editable server-side hooks run as the service account and can read restricted stores")
	}
	if Protocol == FCGI || Protocol == FCGIUnix {
		return errors.New("[branchroom] does not support FastCGI, the gateway needs connection read deadlines")
	}
	if !branchroomRestrictedStoreIntegritySupported() {
		return errors.New("[branchroom] requires Unix filesystem ownership checks")
	}
	if Branchroom.MaxWorkspacesPerOwner < 0 {
		return errors.New("[branchroom].MAX_WORKSPACES_PER_OWNER must not be negative")
	}
	if Branchroom.MaxGitRequestSize <= 0 || Branchroom.MaxGitRequestSize > 1<<30 {
		return errors.New("[branchroom].MAX_GIT_REQUEST_SIZE must be between 1 byte and 1 GiB")
	}
	if Branchroom.GitRequestTimeout <= 0 {
		return errors.New("[branchroom].GIT_REQUEST_TIMEOUT must be positive")
	}

	restrictedRoot := strings.TrimSpace(sec.Key("RESTRICTED_ROOT").String())
	if restrictedRoot == "" {
		return errors.New("[branchroom].RESTRICTED_ROOT must be set when [branchroom].ENABLED is true")
	}

	// the public root is created lazily after config load, so a fresh install may not have it yet
	publicRoot, err := resolveBranchroomPath(RepoRootPath)
	if err != nil {
		return fmt.Errorf("resolve [repository].ROOT: %w", err)
	}
	restrictedRoot, err = canonicalBranchroomRoot("[branchroom].RESTRICTED_ROOT", restrictedRoot)
	if err != nil {
		return err
	}
	if branchroomRootsOverlap(publicRoot, restrictedRoot) {
		return errors.New("[branchroom].RESTRICTED_ROOT must not overlap [repository].ROOT")
	}
	// only the asset, template and option trees are served or read, the roots themselves default to the work path
	for _, base := range []string{CustomPath, StaticRootPath} {
		for _, dir := range []string{"public", "templates", "options"} {
			if base == "" {
				continue
			}
			if err := validateBranchroomSeparateRoot(restrictedRoot, filepath.Join(base, dir)); err != nil {
				return err
			}
		}
	}
	for _, storage := range []*Storage{Attachment.Storage, LFS.Storage, Avatar.Storage, RepoAvatar.Storage, Packages.Storage, Actions.LogStorage, Actions.ArtifactStorage, RepoArchive.Storage} {
		if storage != nil && storage.Type == LocalStorageType {
			if err := validateBranchroomSeparateRoot(restrictedRoot, storage.Path); err != nil {
				return err
			}
		}
	}
	if err := ValidateBranchroomRestrictedRoot(restrictedRoot); err != nil {
		return err
	}

	Branchroom.RestrictedRepoRootPath = restrictedRoot
	return nil
}

// ValidateBranchroomRestrictedRoot checks the filesystem contract shared by startup and receive.
func ValidateBranchroomRestrictedRoot(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("[branchroom].RESTRICTED_ROOT must be an existing directory: %w", err)
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return errors.New("[branchroom].RESTRICTED_ROOT must deny group and other access")
	}
	if !BranchroomRootOwnedByService(info) {
		return errors.New("[branchroom].RESTRICTED_ROOT must be owned by the service account")
	}
	for parent := filepath.Dir(root); ; parent = filepath.Dir(parent) {
		parentInfo, err := os.Lstat(parent)
		if err != nil || parentInfo.Mode()&fs.ModeSymlink != 0 || !parentInfo.IsDir() || (parentInfo.Mode().Perm()&0o022 != 0 && parentInfo.Mode()&fs.ModeSticky == 0) {
			return errors.New("[branchroom].RESTRICTED_ROOT must not be below a group- or world-writable directory")
		}
		if parent == filepath.Dir(parent) {
			break
		}
	}
	return nil
}

func canonicalBranchroomRoot(name, root string) (string, error) {
	if !filepath.IsAbs(root) {
		root = filepath.Join(AppWorkPath, root)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("make %s absolute: %w", name, err)
	}

	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("%s must be an existing directory: %w", name, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s must be a directory", name)
	}

	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks for %s: %w", name, err)
	}
	return filepath.Clean(root), nil
}

func branchroomRootsOverlap(first, second string) bool {
	return branchroomPathContains(first, second) || branchroomPathContains(second, first)
}

func branchroomPathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

func validateBranchroomSeparateRoot(restricted, root string) error {
	if root == "" {
		return nil
	}
	resolved, err := resolveBranchroomPath(root)
	if err != nil {
		return err
	}
	if branchroomRootsOverlap(restricted, resolved) {
		return errors.New("[branchroom].RESTRICTED_ROOT must not overlap local storage or served asset, template and option directories")
	}
	return nil
}

// resolveBranchroomPath resolves symlinks through the nearest existing ancestor of a path that may not exist yet
func resolveBranchroomPath(root string) (string, error) {
	if !filepath.IsAbs(root) {
		root = filepath.Join(AppWorkPath, root)
	}
	root = filepath.Clean(root)
	suffix := ""
	for {
		resolved, err := filepath.EvalSymlinks(root)
		if err == nil {
			return filepath.Join(resolved, suffix), nil
		}
		if !errors.Is(err, fs.ErrNotExist) || root == filepath.Dir(root) {
			return "", err
		}
		suffix = filepath.Join(filepath.Base(root), suffix)
		root = filepath.Dir(root)
	}
}
