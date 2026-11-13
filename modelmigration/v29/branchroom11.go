// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"

	"gitea.dev/modelmigration/base"

	"xorm.io/xorm"
)

func AddAutoMergeCredentials(_ context.Context, x base.EngineMigration) error {
	type PullAutoMerge struct {
		UserExtDoerData string `xorm:"TEXT"`
	}
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true, IgnoreConstrains: true}, new(PullAutoMerge))
	return err
}
