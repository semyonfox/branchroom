// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"strings"

	"gitea.dev/models/db"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/google/uuid"
)

const workspaceRouteKeyLength = 26

var workspaceRouteKeyEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// WorkspaceState describes whether an isolated restricted store may be used.
// A future transport gateway must require both an active workspace and an active scope.
type WorkspaceState string

const (
	WorkspaceStateProvisioning WorkspaceState = "provisioning"
	WorkspaceStateReady        WorkspaceState = "ready"
	WorkspaceStateFailed       WorkspaceState = "failed"
	WorkspaceStateRetired      WorkspaceState = "retired"
	WorkspaceStatePurged       WorkspaceState = "purged"
)

// IsWorkspaceStateValid reports whether state is persisted by the current model.
func IsWorkspaceStateValid(state WorkspaceState) bool {
	switch state {
	case WorkspaceStateProvisioning, WorkspaceStateReady, WorkspaceStateFailed, WorkspaceStateRetired, WorkspaceStatePurged:
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
	RouteKey         string             `xorm:"CHAR(26) UNIQUE NOT NULL"`
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
	if !IsCanonicalUUID(workspace.WorkspaceKey) || !isWorkspaceRouteKey(workspace.RouteKey) || workspaceRouteKeyCorrelates(workspace.RouteKey, workspace.WorkspaceKey, workspace.StoreID) || !IsCanonicalUUID(workspace.StoreID) || workspace.ScopeID <= 0 || workspace.RepoID <= 0 || workspace.CreatedByID <= 0 {
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
	case WorkspaceStateRetired, WorkspaceStatePurged:
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

// NewWorkspaceRouteKey returns a new opaque public locator. It is never an authorization credential.
func NewWorkspaceRouteKey() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return workspaceRouteKeyEncoding.EncodeToString(value), nil
}

// IsWorkspaceRouteKey reports whether value is a canonical public workspace route key.
func IsWorkspaceRouteKey(value string) bool {
	return isWorkspaceRouteKey(value)
}

func isWorkspaceRouteKey(value string) bool {
	if len(value) != workspaceRouteKeyLength || value != strings.ToLower(value) {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= '2' && r <= '7') {
			return false
		}
	}
	return true
}

func workspaceRouteKeyCorrelates(routeKey, workspaceKey, storeID string) bool {
	for _, value := range []string{workspaceKey, storeID} {
		parsed, err := uuid.Parse(value)
		if err == nil && routeKey == workspaceRouteKeyEncoding.EncodeToString(parsed[:]) {
			return true
		}
	}
	return false
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
	if workspace.RouteKey == "" {
		routeKey, err := NewWorkspaceRouteKey()
		if err != nil {
			return err
		}
		workspace.RouteKey = routeKey
	}
	if err := workspace.Validate(); err != nil {
		return err
	}
	return db.Insert(ctx, workspace)
}

// GetWorkspaceByRouteKey returns a workspace by its opaque canonical public route key.
func GetWorkspaceByRouteKey(ctx context.Context, routeKey string) (*Workspace, error) {
	if !isWorkspaceRouteKey(routeKey) {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom workspace route key")
	}
	workspace := new(Workspace)
	has, err := db.GetEngine(ctx).Where("route_key = ?", routeKey).Get(workspace)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.NewNotExistErrorf("Branchroom workspace does not exist")
	}
	return workspace, nil
}

// GetWorkspaceByRouteKeyAndRepoID returns a workspace only when its opaque route
// locator belongs to repoID.
func GetWorkspaceByRouteKeyAndRepoID(ctx context.Context, routeKey string, repoID int64) (*Workspace, error) {
	if !isWorkspaceRouteKey(routeKey) || repoID <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom workspace route lookup")
	}
	workspace := new(Workspace)
	has, err := db.GetEngine(ctx).Where("route_key = ? AND repo_id = ?", routeKey, repoID).Get(workspace)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.NewNotExistErrorf("Branchroom workspace does not exist")
	}
	return workspace, nil
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

// GetWorkspaceByKey returns a workspace by its opaque canonical UUID key.
func GetWorkspaceByKey(ctx context.Context, workspaceKey string) (*Workspace, error) {
	if !IsCanonicalUUID(workspaceKey) {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom workspace key")
	}
	workspace := new(Workspace)
	engine := db.GetEngine(ctx).Where("workspace_key = ?", workspaceKey)
	has, err := engine.Get(workspace)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.NewNotExistErrorf("Branchroom workspace does not exist")
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
	} else if to == WorkspaceStateRetired || to == WorkspaceStatePurged {
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

// RotateWorkspaceRouteKey replaces the public route key only when current still
// identifies a ready workspace. The caller retains its snapshot so a false
// result can be handled as a stale compare-and-swap attempt.
func RotateWorkspaceRouteKey(ctx context.Context, current *Workspace, newKey string) (bool, error) {
	if current == nil || current.ID <= 0 || current.State != WorkspaceStateReady {
		return false, util.NewInvalidArgumentErrorf("invalid Branchroom workspace route-key rotation")
	}
	if err := current.Validate(); err != nil {
		return false, err
	}
	if newKey == current.RouteKey {
		return false, util.NewInvalidArgumentErrorf("Branchroom workspace route key must change")
	}

	candidate := *current
	candidate.RouteKey = newKey
	if err := candidate.Validate(); err != nil {
		return false, err
	}

	affected, err := db.GetEngine(ctx).
		ID(current.ID).
		Where("route_key = ? AND state = ?", current.RouteKey, WorkspaceStateReady).
		Cols("route_key").
		Update(&Workspace{RouteKey: newKey})
	return affected == 1, err
}

func isWorkspaceTransitionValid(from, to WorkspaceState) bool {
	switch from {
	case WorkspaceStateProvisioning:
		return to == WorkspaceStateReady || to == WorkspaceStateFailed || to == WorkspaceStateRetired
	case WorkspaceStateFailed:
		return to == WorkspaceStateRetired || to == WorkspaceStatePurged || to == WorkspaceStateProvisioning
	case WorkspaceStateReady:
		return to == WorkspaceStateRetired || to == WorkspaceStatePurged
	case WorkspaceStateRetired:
		return to == WorkspaceStatePurged
	default:
		return false
	}
}

// CountOwnerActiveWorkspaces counts workspaces in provisioning, ready, or failed
// state across all scopes owned by ownerID.
func CountOwnerActiveWorkspaces(ctx context.Context, ownerID int64) (int64, error) {
	if ownerID <= 0 {
		return 0, util.NewInvalidArgumentErrorf("invalid Branchroom scope owner ID")
	}
	return db.GetEngine(ctx).
		Table(new(Workspace)).
		Join("INNER", new(Scope), "branchroom_scope.id = branchroom_workspace.scope_id").
		Where("branchroom_scope.owner_id = ?", ownerID).
		In("branchroom_workspace.state", WorkspaceStateProvisioning, WorkspaceStateReady, WorkspaceStateFailed).
		Count()
}

func init() {
	db.RegisterModel(new(Workspace))
}
