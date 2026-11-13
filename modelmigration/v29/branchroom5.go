// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"

	"gitea.dev/modelmigration/base"
	"gitea.dev/modules/timeutil"
)

type branchroomReceiveSessionMigration struct {
	ID               int64
	SessionKey       string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	BearerSecretHash string             `xorm:"CHAR(64) UNIQUE NOT NULL"`
	ScopeID          int64              `xorm:"INDEX(scope_state) NOT NULL"`
	WorkspaceID      int64              `xorm:"NOT NULL"`
	RepoID           int64              `xorm:"INDEX(repo_state) NOT NULL"`
	ActorType        string             `xorm:"VARCHAR(16) INDEX(actor_state) NOT NULL"`
	ActorID          int64              `xorm:"INDEX(actor_state) NOT NULL"`
	State            string             `xorm:"VARCHAR(16) INDEX(scope_state) INDEX(repo_state) INDEX(actor_state) INDEX(state_expiry) NOT NULL"`
	IssuedUnix       timeutil.TimeStamp `xorm:"NOT NULL"`
	ExpiresUnix      timeutil.TimeStamp `xorm:"INDEX(state_expiry) NOT NULL"`
	ClaimedUnix      timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"`
	CreatedUnix      timeutil.TimeStamp `xorm:"created NOT NULL"`
	UpdatedUnix      timeutil.TimeStamp `xorm:"updated NOT NULL"`
}

func (branchroomReceiveSessionMigration) TableName() string {
	return "branchroom_receive_session"
}

// AddBranchroomReceiveSessionSchema creates one-use source-write receive sessions.
func AddBranchroomReceiveSessionSchema(_ context.Context, x base.EngineMigration) error {
	return x.Sync(new(branchroomReceiveSessionMigration))
}
