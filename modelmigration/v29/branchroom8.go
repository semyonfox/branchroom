// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"

	"gitea.dev/modelmigration/base"
	"gitea.dev/modules/timeutil"

	"xorm.io/xorm"
)

type accessTokenRestrictions struct {
	ExpiresUnix   timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"`
	RepositoryIDs []int64            `xorm:"JSON TEXT"`
}

func (accessTokenRestrictions) TableName() string { return "access_token" }

func AddAccessTokenRestrictions(_ context.Context, x base.EngineMigration) error {
	_, err := x.SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true, IgnoreConstrains: true}, new(accessTokenRestrictions))
	return err
}
