// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BranchroomConfig contains the static configuration for Branchroom foundations.
type BranchroomConfig struct {
	Enabled                bool
	RestrictedRepoRootPath string
}

// Branchroom stores the static Branchroom configuration.
var Branchroom BranchroomConfig

func loadBranchroomFrom(rootCfg ConfigProvider) error {
	sec := rootCfg.Section("branchroom")
	Branchroom = BranchroomConfig{
		Enabled: sec.Key("ENABLED").MustBool(false),
	}
	if !Branchroom.Enabled {
		return nil
	}

	restrictedRoot := strings.TrimSpace(sec.Key("RESTRICTED_ROOT").String())
	if restrictedRoot == "" {
		return errors.New("[branchroom].RESTRICTED_ROOT must be set when [branchroom].ENABLED is true")
	}

	publicRoot, err := canonicalBranchroomRoot("[repository].ROOT", RepoRootPath)
	if err != nil {
		return err
	}
	restrictedRoot, err = canonicalBranchroomRoot("[branchroom].RESTRICTED_ROOT", restrictedRoot)
	if err != nil {
		return err
	}
	if branchroomRootsOverlap(publicRoot, restrictedRoot) {
		return errors.New("[branchroom].RESTRICTED_ROOT must not overlap [repository].ROOT")
	}

	Branchroom.RestrictedRepoRootPath = restrictedRoot
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
