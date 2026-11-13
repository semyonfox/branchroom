// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"

	"gitea.dev/modelmigration/base"

	"xorm.io/xorm"
)

func AddBranchroomAuditDetails(_ context.Context, x base.EngineMigration) error {
	for _, table := range []string{"branchroom_decision", "branchroom_audit_event"} {
		if _, err := x.Table(table).SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true, IgnoreConstrains: true}, &struct {
			Details string `xorm:"TEXT"`
		}{}); err != nil {
			return err
		}
	}
	_, err := x.Table("branchroom_receive_session").SyncWithOptions(xorm.SyncOptions{IgnoreDropIndices: true, IgnoreConstrains: true}, &struct {
		Updates string `xorm:"TEXT"`
	}{})
	return err
}
