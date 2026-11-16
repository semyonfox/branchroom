// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"

	"gitea.dev/modelmigration/base"

	"xorm.io/xorm"
)

func AddBranchroomAgentApproval(_ context.Context, x base.EngineMigration) error {
	type BranchroomTokenIdentity struct {
		ApprovesAsOwner bool `xorm:"NOT NULL DEFAULT false"`
	}
	type BranchroomAgentAction struct {
		AsOwner bool `xorm:"NOT NULL DEFAULT false"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true, IgnoreConstrains: true}, new(BranchroomTokenIdentity), new(BranchroomAgentAction))
	return err
}
