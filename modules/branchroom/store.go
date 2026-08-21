// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"errors"
	"strings"

	"gitea.dev/modules/git/gitrepo"
	"gitea.dev/modules/setting"
)

// RestrictedRepository returns the future physical repository for one restricted store.
func RestrictedRepository(storeID string) (gitrepo.RepositoryFacade, error) {
	if !setting.Branchroom.Enabled {
		return nil, errors.New("branchroom is disabled")
	}
	if strings.TrimSpace(storeID) == "" {
		return nil, errors.New("restricted store ID must not be empty")
	}

	return gitrepo.RepositoryManagedInRoot(
		"branchroom-store-"+storeID,
		setting.Branchroom.RestrictedRepoRootPath,
		storeID+".git",
	)
}
