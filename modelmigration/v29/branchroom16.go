// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"

	"gitea.dev/modelmigration/base"
)

func AddUserInboxState(_ context.Context, x base.EngineMigration) error {
	type UserInboxState struct {
		UserID       int64  `xorm:"pk"`
		IssueID      int64  `xorm:"pk"`
		State        string `xorm:"VARCHAR(16) NOT NULL"`
		SnoozedUntil int64
		UpdatedUnix  int64
	}
	return x.Sync(new(UserInboxState))
}
