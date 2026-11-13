// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"

	"gitea.dev/modelmigration/base"

	"xorm.io/xorm/schemas"
)

// ConvertBranchroomAuditHashesToVarchar stops PostgreSQL from blank-padding the empty
// head and previous hashes of an audit chain, which made every first audit event invalid.
func ConvertBranchroomAuditHashesToVarchar(ctx context.Context, x base.EngineMigration) error {
	dbType := x.Dialect().URI().DBType
	if dbType == schemas.SQLITE { // sqlite stores both as TEXT without padding
		return nil
	}
	for _, target := range []struct{ table, column string }{
		{"branchroom_scope", "audit_head_hash"},
		{"branchroom_audit_event", "previous_hash"},
	} {
		if err := base.ModifyColumn(ctx, x, target.table, &schemas.Column{
			Name:    target.column,
			SQLType: schemas.SQLType{Name: "VARCHAR"},
			Length:  64,
			Default: "''",
		}); err != nil {
			return err
		}
		trim := "UPDATE " + target.table + " SET " + target.column + " = TRIM(" + target.column + ")"
		if dbType == schemas.MSSQL {
			trim = "UPDATE " + target.table + " SET " + target.column + " = RTRIM(LTRIM(" + target.column + "))"
		}
		if _, err := x.Exec(trim); err != nil {
			return err
		}
	}
	return nil
}
