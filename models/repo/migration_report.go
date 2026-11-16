// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"context"

	"gitea.dev/models/db"
	"gitea.dev/modules/migration"
	"gitea.dev/modules/timeutil"

	"xorm.io/builder"
)

// RepoMigrationReport keeps what a migration left for a person to finish, until they dismiss it
type RepoMigrationReport struct { //revive:disable-line:exported
	RepoID      int64                     `xorm:"pk"`
	Report      *migration.SettingsReport `xorm:"JSON LONGTEXT"`
	CreatedUnix timeutil.TimeStamp        `xorm:"created"`
}

func init() {
	db.RegisterModel(new(RepoMigrationReport))
}

// SaveMigrationReport stores the report of a repository's migration
func SaveMigrationReport(ctx context.Context, repoID int64, report *migration.SettingsReport) error {
	return db.Insert(ctx, &RepoMigrationReport{RepoID: repoID, Report: report})
}

// GetMigrationReport returns nil when the repository has no report
func GetMigrationReport(ctx context.Context, repoID int64) (*RepoMigrationReport, error) {
	report, exist, err := db.GetByID[RepoMigrationReport](ctx, repoID)
	if err != nil || !exist {
		return nil, err
	}
	return report, nil
}

// DeleteMigrationReport removes the report of a repository's migration
func DeleteMigrationReport(ctx context.Context, repoID int64) error {
	_, err := db.DeleteByID[RepoMigrationReport](ctx, repoID)
	return err
}

// HasMigrationReport skips the query for repositories that were never migrated
func HasMigrationReport(ctx context.Context, repo *Repository) (bool, error) {
	if repo.OriginalURL == "" {
		return false, nil
	}
	return db.Exist[RepoMigrationReport](ctx, builder.Eq{"repo_id": repo.ID})
}
