// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"

	"gitea.dev/modelmigration/base"
)

func AddPathReviewerRules(_ context.Context, x base.EngineMigration) error {
	type BranchroomPathReviewer struct {
		ID                int64  `xorm:"pk autoincr"`
		RepoID            int64  `xorm:"INDEX NOT NULL"`
		ProtectedBranchID int64  `xorm:"INDEX NOT NULL"`
		Pattern           string `xorm:"VARCHAR(255) NOT NULL"`
		TeamID            int64
		CodeOwners        bool
		RequiredApprovals int64
	}
	return x.Sync(new(BranchroomPathReviewer))
}
