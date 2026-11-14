// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"

	"gitea.dev/modelmigration/base"
	"gitea.dev/modules/timeutil"
)

type BranchroomTokenIdentity struct {
	TokenID int64  `xorm:"pk"`
	Agent   string `xorm:"VARCHAR(255) NOT NULL"`
	TaskURL string `xorm:"TEXT"`
	Sponsor string `xorm:"VARCHAR(255)"`
}

type BranchroomAgentAction struct {
	ID       int64  `xorm:"pk autoincr"`
	Kind     string `xorm:"VARCHAR(16) UNIQUE(target)"`
	TargetID int64  `xorm:"UNIQUE(target)"`
	TokenID  int64
	Agent    string `xorm:"VARCHAR(255)"`
	TaskURL  string `xorm:"TEXT"`
	Sponsor  string `xorm:"VARCHAR(255)"`
}

type BranchroomPushReceipt struct {
	ID          int64  `xorm:"pk autoincr"`
	RepoID      int64  `xorm:"INDEX(commit) NOT NULL"`
	Ref         string `xorm:"VARCHAR(255)"`
	OldSHA      string `xorm:"VARCHAR(64)"`
	NewSHA      string `xorm:"VARCHAR(64) INDEX(commit)"`
	TokenID     int64
	Agent       string             `xorm:"VARCHAR(255)"`
	TaskURL     string             `xorm:"TEXT"`
	Sponsor     string             `xorm:"VARCHAR(255)"`
	CreatedUnix timeutil.TimeStamp `xorm:"created"`
}

func AddBranchroomAgentIdentity(_ context.Context, x base.EngineMigration) error {
	return x.Sync(new(BranchroomTokenIdentity), new(BranchroomAgentAction), new(BranchroomPushReceipt))
}
