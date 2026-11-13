// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"errors"

	"gitea.dev/modules/git/gitrepo"
	"gitea.dev/modules/setting"

	"github.com/google/uuid"
)

// RestrictedRepository returns the future physical repository for one restricted store.
func RestrictedRepository(storeID string) (gitrepo.RepositoryFacade, error) {
	if !setting.Branchroom.Enabled {
		return nil, errors.New("branchroom is disabled")
	}
	if id, err := uuid.Parse(storeID); err != nil || id.String() != storeID {
		return nil, errors.New("restricted store ID must be a canonical UUID")
	}
	// A private root prevents local users from traversing into stores. Receive-only
	// ownership checks stay in the hook path so read access remains portable.
	if _, err := ensureIntegritySafeRoot(setting.Branchroom.RestrictedRepoRootPath); err != nil {
		return nil, err
	}

	return gitrepo.RepositoryManagedInRoot(
		"branchroom-store-"+storeID,
		setting.Branchroom.RestrictedRepoRootPath,
		storeID+".git",
	)
}
