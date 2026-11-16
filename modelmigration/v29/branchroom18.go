// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"

	"gitea.dev/modelmigration/base"
)

func AddRepoMigrationReport(_ context.Context, x base.EngineMigration) error {
	type RepoMigrationReport struct {
		RepoID      int64  `xorm:"pk"`
		Report      string `xorm:"JSON LONGTEXT"`
		CreatedUnix int64  `xorm:"created"`
	}
	return x.Sync(new(RepoMigrationReport))
}
