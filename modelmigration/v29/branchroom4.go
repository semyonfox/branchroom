// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"
	"crypto/rand"
	"encoding/base32"

	"gitea.dev/modelmigration/base"

	"xorm.io/xorm"
	"xorm.io/xorm/schemas"
)

const branchroomWorkspaceRouteKeyLength = 26

var branchroomWorkspaceRouteKeyEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

type branchroomWorkspaceRouteKeyMigration struct {
	RouteKey string `xorm:"CHAR(26) NOT NULL DEFAULT ''"`
}

func (branchroomWorkspaceRouteKeyMigration) TableName() string { return "branchroom_workspace" }

type branchroomWorkspaceRouteKeyBackfill struct {
	ID           int64
	WorkspaceKey string
	StoreID      string
	RouteKey     string
}

func (branchroomWorkspaceRouteKeyBackfill) TableName() string { return "branchroom_workspace" }

// AddBranchroomWorkspaceRouteKey adds durable, opaque route locators for existing workspaces.
func AddBranchroomWorkspaceRouteKey(ctx context.Context, x base.EngineMigration) error {
	if _, err := x.SyncWithOptions(xorm.SyncOptions{
		IgnoreDropIndices: true,
		IgnoreConstrains:  true,
	}, new(branchroomWorkspaceRouteKeyMigration)); err != nil {
		return err
	}

	workspaces := make([]*branchroomWorkspaceRouteKeyBackfill, 0)
	if err := x.Table("branchroom_workspace").Asc("id").Find(&workspaces); err != nil {
		return err
	}
	used := make(map[string]struct{}, len(workspaces))
	for _, workspace := range workspaces {
		if workspace.RouteKey != "" {
			used[workspace.RouteKey] = struct{}{}
		}
	}
	for _, workspace := range workspaces {
		if workspace.RouteKey != "" {
			continue
		}
		routeKey, err := newWorkspaceRouteKey(used)
		if err != nil {
			return err
		}
		if _, err := x.Table("branchroom_workspace").ID(workspace.ID).Cols("route_key").Update(&branchroomWorkspaceRouteKeyBackfill{RouteKey: routeKey}); err != nil {
			return err
		}
		used[routeKey] = struct{}{}
	}

	indexes, err := x.Dialect().GetIndexes(x.DB(), ctx, "branchroom_workspace")
	if err != nil {
		return err
	}
	for _, index := range indexes {
		if index.Type == schemas.UniqueType && len(index.Cols) == 1 && index.Cols[0] == "route_key" {
			return nil
		}
	}
	index := schemas.NewIndex("route_key", schemas.UniqueType)
	index.AddColumn("route_key")
	_, err = x.Exec(x.Dialect().CreateIndexSQL("branchroom_workspace", index))
	return err
}

func newWorkspaceRouteKey(used map[string]struct{}) (string, error) {
	for {
		value := make([]byte, 16)
		if _, err := rand.Read(value); err != nil {
			return "", err
		}
		key := branchroomWorkspaceRouteKeyEncoding.EncodeToString(value)
		if _, exists := used[key]; !exists {
			return key, nil
		}
	}
}
