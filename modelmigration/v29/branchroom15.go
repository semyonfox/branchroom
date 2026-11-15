// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"

	"gitea.dev/modelmigration/base"

	"xorm.io/xorm"
)

func AddBranchroomAgentCredit(_ context.Context, x base.EngineMigration) error {
	type BranchroomTokenIdentity struct {
		Model   string `xorm:"VARCHAR(255)"`
		Harness string `xorm:"VARCHAR(255)"`
	}
	type BranchroomAgentAction BranchroomTokenIdentity
	type BranchroomPushReceipt BranchroomTokenIdentity
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true, IgnoreConstrains: true}, new(BranchroomTokenIdentity), new(BranchroomAgentAction), new(BranchroomPushReceipt))
	return err
}
