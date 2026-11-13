// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"

	"gitea.dev/modelmigration/base"
	"gitea.dev/modules/timeutil"
)

type branchroomWorkspaceMigration struct {
	ID               int64
	WorkspaceKey     string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	ScopeID          int64              `xorm:"UNIQUE NOT NULL"`
	RepoID           int64              `xorm:"INDEX(repo_state) NOT NULL"`
	StoreID          string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	PublicBaseSHA    string             `xorm:"VARCHAR(64) NOT NULL"`
	ObjectFormatName string             `xorm:"VARCHAR(6) NOT NULL"`
	State            string             `xorm:"VARCHAR(16) INDEX(repo_state) NOT NULL"`
	FailureCode      string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	CreatedByID      int64              `xorm:"NOT NULL"`
	RetiredUnix      timeutil.TimeStamp `xorm:"INDEX NOT NULL DEFAULT 0"`
	CreatedUnix      timeutil.TimeStamp `xorm:"created NOT NULL"`
	UpdatedUnix      timeutil.TimeStamp `xorm:"updated NOT NULL"`
}

func (branchroomWorkspaceMigration) TableName() string { return "branchroom_workspace" }

// AddBranchroomWorkspaceSchema creates the isolated-store mapping used by restricted workspaces.
func AddBranchroomWorkspaceSchema(_ context.Context, x base.EngineMigration) error {
	return x.Sync(new(branchroomWorkspaceMigration))
}
