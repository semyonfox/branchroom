// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"

	"gitea.dev/models/db"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
)

// WorkspaceState describes whether an isolated restricted store may be used.
// A future transport gateway must require both an active workspace and an active scope.
type WorkspaceState string

const (
	WorkspaceStateProvisioning WorkspaceState = "provisioning"
	WorkspaceStateReady        WorkspaceState = "ready"
	WorkspaceStateFailed       WorkspaceState = "failed"
	WorkspaceStateRetired      WorkspaceState = "retired"
)

// IsWorkspaceStateValid reports whether state is persisted by the current model.
func IsWorkspaceStateValid(state WorkspaceState) bool {
	switch state {
	case WorkspaceStateProvisioning, WorkspaceStateReady, WorkspaceStateFailed, WorkspaceStateRetired:
		return true
	default:
		return false
	}
}

// Workspace maps one repository scope to an isolated Git object store.
// StoreID is opaque and must never be rendered as a filesystem path or clone URL.
type Workspace struct {
	ID               int64              `xorm:"pk autoincr"`
	WorkspaceKey     string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	ScopeID          int64              `xorm:"UNIQUE NOT NULL"`
	RepoID           int64              `xorm:"INDEX(repo_state) NOT NULL"`
	StoreID          string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	PublicBaseSHA    string             `xorm:"VARCHAR(64) NOT NULL"`
	ObjectFormatName string             `xorm:"VARCHAR(6) NOT NULL"`
	State            WorkspaceState     `xorm:"VARCHAR(16) INDEX(repo_state) NOT NULL"`
	FailureCode      string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	CreatedByID      int64              `xorm:"NOT NULL"`
	RetiredUnix      timeutil.TimeStamp `xorm:"INDEX NOT NULL DEFAULT 0"`
	CreatedUnix      timeutil.TimeStamp `xorm:"created NOT NULL"`
	UpdatedUnix      timeutil.TimeStamp `xorm:"updated NOT NULL"`
}

// TableName returns the stable Branchroom workspace table name.
func (Workspace) TableName() string { return "branchroom_workspace" }

// Validate checks the storage mapping without resolving its physical repository.
func (workspace *Workspace) Validate() error {
	if !isCanonicalUUID(workspace.WorkspaceKey) || !isCanonicalUUID(workspace.StoreID) || workspace.ScopeID <= 0 || workspace.RepoID <= 0 || workspace.CreatedByID <= 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom workspace identity")
	}
	if !workspaceBaseMatchesObjectFormat(workspace.PublicBaseSHA, workspace.ObjectFormatName) || !IsWorkspaceStateValid(workspace.State) || workspace.RetiredUnix < 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom workspace state")
	}
	switch workspace.State {
	case WorkspaceStateFailed:
		if !isReasonCode(workspace.FailureCode) || workspace.RetiredUnix != 0 {
			return util.NewInvalidArgumentErrorf("invalid Branchroom workspace failure")
		}
	case WorkspaceStateRetired:
		if workspace.FailureCode != "" || workspace.RetiredUnix <= 0 {
			return util.NewInvalidArgumentErrorf("invalid Branchroom workspace retirement")
		}
	default:
		if workspace.FailureCode != "" || workspace.RetiredUnix != 0 {
			return util.NewInvalidArgumentErrorf("invalid Branchroom workspace lifecycle")
		}
	}
	return nil
}

func workspaceBaseMatchesObjectFormat(sha, objectFormatName string) bool {
	switch objectFormatName {
	case "sha1":
		return len(sha) == 40 && IsGitObjectID(sha)
	case "sha256":
		return len(sha) == 64 && IsGitObjectID(sha)
	default:
		return false
	}
}

// InsertWorkspace validates and inserts a workspace through the caller's transaction context.
func InsertWorkspace(ctx context.Context, workspace *Workspace) error {
	if workspace == nil {
		return util.NewInvalidArgumentErrorf("Branchroom workspace is required")
	}
	if err := workspace.Validate(); err != nil {
		return err
	}
	return db.Insert(ctx, workspace)
}

// GetWorkspaceByID returns a workspace by its database ID.
func GetWorkspaceByID(ctx context.Context, id int64) (*Workspace, error) {
	return getWorkspaceByID(ctx, id, false)
}

// GetWorkspaceByIDForUpdate returns a workspace with a row lock on databases that support it.
func GetWorkspaceByIDForUpdate(ctx context.Context, id int64) (*Workspace, error) {
	return getWorkspaceByID(ctx, id, true)
}

func getWorkspaceByID(ctx context.Context, id int64, forUpdate bool) (*Workspace, error) {
	if id <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom workspace ID")
	}
	workspace := new(Workspace)
	engine := db.GetEngine(ctx).ID(id)
	if forUpdate && branchroomSupportsForUpdate() {
		engine = engine.ForUpdate()
	}
	has, err := engine.Get(workspace)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.NewNotExistErrorf("Branchroom workspace %d does not exist", id)
	}
	return workspace, nil
}

// GetWorkspaceByScopeID returns the workspace mapped to scopeID.
func GetWorkspaceByScopeID(ctx context.Context, scopeID int64) (*Workspace, error) {
	return getWorkspaceByScopeID(ctx, scopeID, false)
}

// GetWorkspaceByScopeIDForUpdate returns a scope mapping with a row lock on databases that support it.
func GetWorkspaceByScopeIDForUpdate(ctx context.Context, scopeID int64) (*Workspace, error) {
	return getWorkspaceByScopeID(ctx, scopeID, true)
}

func getWorkspaceByScopeID(ctx context.Context, scopeID int64, forUpdate bool) (*Workspace, error) {
	if scopeID <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom scope ID")
	}
	workspace := new(Workspace)
	engine := db.GetEngine(ctx).Where("scope_id = ?", scopeID)
	if forUpdate && branchroomSupportsForUpdate() {
		engine = engine.ForUpdate()
	}
	has, err := engine.Get(workspace)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.NewNotExistErrorf("Branchroom workspace for scope %d does not exist", scopeID)
	}
	return workspace, nil
}

// FindWorkspacesByRepoID returns workspaces in deterministic creation order.
func FindWorkspacesByRepoID(ctx context.Context, repoID int64) ([]*Workspace, error) {
	return findWorkspacesByRepoID(ctx, repoID, false)
}

// FindWorkspacesByRepoIDForUpdate returns repository mappings with row locks on databases that support them.
func FindWorkspacesByRepoIDForUpdate(ctx context.Context, repoID int64) ([]*Workspace, error) {
	return findWorkspacesByRepoID(ctx, repoID, true)
}

func findWorkspacesByRepoID(ctx context.Context, repoID int64, forUpdate bool) ([]*Workspace, error) {
	if repoID <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom repository ID")
	}
	workspaces := make([]*Workspace, 0)
	engine := db.GetEngine(ctx).Where("repo_id = ?", repoID).Asc("id")
	if forUpdate && branchroomSupportsForUpdate() {
		engine = engine.ForUpdate()
	}
	err := engine.Find(&workspaces)
	return workspaces, err
}

// TransitionWorkspace changes one workspace state only when its current state matches from.
func TransitionWorkspace(ctx context.Context, workspaceID int64, from, to WorkspaceState, failureCode string, retiredUnix timeutil.TimeStamp) (bool, error) {
	if workspaceID <= 0 || !IsWorkspaceStateValid(from) || !IsWorkspaceStateValid(to) || !isWorkspaceTransitionValid(from, to) || retiredUnix < 0 {
		return false, util.NewInvalidArgumentErrorf("invalid Branchroom workspace transition")
	}
	if to == WorkspaceStateFailed {
		if !isReasonCode(failureCode) || retiredUnix != 0 {
			return false, util.NewInvalidArgumentErrorf("invalid Branchroom workspace failure transition")
		}
	} else if to == WorkspaceStateRetired {
		if failureCode != "" || retiredUnix <= 0 {
			return false, util.NewInvalidArgumentErrorf("invalid Branchroom workspace retirement transition")
		}
	} else if failureCode != "" || retiredUnix != 0 {
		return false, util.NewInvalidArgumentErrorf("invalid Branchroom workspace transition state")
	}

	affected, err := db.GetEngine(ctx).ID(workspaceID).Where("state = ?", from).Cols("state", "failure_code", "retired_unix").Update(&Workspace{
		State:       to,
		FailureCode: failureCode,
		RetiredUnix: retiredUnix,
	})
	return affected == 1, err
}

func isWorkspaceTransitionValid(from, to WorkspaceState) bool {
	switch from {
	case WorkspaceStateProvisioning:
		return to == WorkspaceStateReady || to == WorkspaceStateFailed || to == WorkspaceStateRetired
	case WorkspaceStateReady, WorkspaceStateFailed:
		return to == WorkspaceStateRetired
	default:
		return false
	}
}

func init() {
	db.RegisterModel(new(Workspace))
}
