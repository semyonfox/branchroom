// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"strings"

	"gitea.dev/models/db"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
)

// Scope names a protected repository-level area and its audit-chain head.
type Scope struct {
	ID            int64              `xorm:"pk autoincr"`
	Kind          ScopeKind          `xorm:"VARCHAR(32) NOT NULL"`
	RepoID        int64              `xorm:"UNIQUE(repo_lower_name) INDEX(repo_state) NOT NULL"`
	Name          string             `xorm:"VARCHAR(255) NOT NULL"`
	LowerName     string             `xorm:"VARCHAR(255) UNIQUE(repo_lower_name) NOT NULL"`
	State         ScopeState         `xorm:"VARCHAR(16) INDEX(repo_state) NOT NULL"`
	OwnerType     ScopeOwnerType     `xorm:"VARCHAR(16) INDEX(owner) NOT NULL"`
	OwnerID       int64              `xorm:"INDEX(owner) NOT NULL"`
	CreatedByID   int64              `xorm:"NOT NULL"`
	AuditSequence int64              `xorm:"NOT NULL DEFAULT 0"`
	AuditHeadHash string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	CreatedUnix   timeutil.TimeStamp `xorm:"created NOT NULL"`
	UpdatedUnix   timeutil.TimeStamp `xorm:"updated NOT NULL"`
}

// TableName returns the stable Branchroom scope table name.
func (Scope) TableName() string { return "branchroom_scope" }

// NormalizeName applies the persisted scope-name canonicalization.
func (scope *Scope) NormalizeName() {
	scope.Name = strings.TrimSpace(scope.Name)
	scope.LowerName = strings.ToLower(scope.Name)
}

// Validate checks structural invariants without evaluating access policy.
func (scope *Scope) Validate() error {
	normalizedName := strings.ToLower(scope.Name)
	if !IsScopeKindValid(scope.Kind) || scope.RepoID <= 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom scope kind or repository")
	}
	if !isScopeName(scope.Name) || scope.LowerName != normalizedName {
		return util.NewInvalidArgumentErrorf("invalid Branchroom scope name")
	}
	if !IsScopeStateValid(scope.State) || !IsScopeOwnerTypeValid(scope.OwnerType) || scope.OwnerID <= 0 || scope.CreatedByID <= 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom scope state or owner")
	}
	if scope.AuditSequence < 0 || scope.AuditSequence == 0 && scope.AuditHeadHash != "" || scope.AuditSequence > 0 && !isSHA256Hex(scope.AuditHeadHash) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom scope audit head")
	}
	return nil
}

// InsertScope normalizes and inserts a scope through the caller's transaction context.
func InsertScope(ctx context.Context, scope *Scope) error {
	if scope == nil {
		return util.NewInvalidArgumentErrorf("Branchroom scope is required")
	}
	scope.NormalizeName()
	if err := scope.Validate(); err != nil {
		return err
	}
	return db.Insert(ctx, scope)
}

// GetScopeByID returns the scope with id.
func GetScopeByID(ctx context.Context, id int64) (*Scope, error) {
	return getScopeByID(ctx, id, false)
}

// GetScopeByIDForUpdate returns a scope with a row lock on databases that support it.
func GetScopeByIDForUpdate(ctx context.Context, id int64) (*Scope, error) {
	return getScopeByID(ctx, id, true)
}

func getScopeByID(ctx context.Context, id int64, forUpdate bool) (*Scope, error) {
	if id <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom scope ID")
	}
	scope := new(Scope)
	engine := db.GetEngine(ctx).ID(id)
	if forUpdate && branchroomSupportsForUpdate() {
		engine = engine.ForUpdate()
	}
	has, err := engine.Get(scope)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.NewNotExistErrorf("Branchroom scope %d does not exist", id)
	}
	return scope, nil
}

// GetScopeByRepoIDAndLowerName returns a scope by its repository-local canonical name.
func GetScopeByRepoIDAndLowerName(ctx context.Context, repoID int64, lowerName string) (*Scope, error) {
	return getScopeByRepoIDAndLowerName(ctx, repoID, lowerName, false)
}

// GetScopeByRepoIDAndLowerNameForUpdate returns a scope by canonical name with a row lock on supported databases.
func GetScopeByRepoIDAndLowerNameForUpdate(ctx context.Context, repoID int64, lowerName string) (*Scope, error) {
	return getScopeByRepoIDAndLowerName(ctx, repoID, lowerName, true)
}

func getScopeByRepoIDAndLowerName(ctx context.Context, repoID int64, lowerName string, forUpdate bool) (*Scope, error) {
	if repoID <= 0 || !isScopeName(lowerName) || lowerName != strings.ToLower(lowerName) {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom scope repository or canonical name")
	}
	scope := new(Scope)
	engine := db.GetEngine(ctx).Where("repo_id = ? AND lower_name = ?", repoID, lowerName)
	if forUpdate && branchroomSupportsForUpdate() {
		engine = engine.ForUpdate()
	}
	has, err := engine.Get(scope)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.NewNotExistErrorf("Branchroom scope does not exist")
	}
	return scope, nil
}

// FindScopesByRepoID returns scopes in deterministic name order.
func FindScopesByRepoID(ctx context.Context, repoID int64) ([]*Scope, error) {
	return findScopesByRepoID(ctx, repoID, false)
}

// FindScopesByRepoIDForUpdate returns repository scopes with row locks on databases that support them.
func FindScopesByRepoIDForUpdate(ctx context.Context, repoID int64) ([]*Scope, error) {
	return findScopesByRepoID(ctx, repoID, true)
}

func findScopesByRepoID(ctx context.Context, repoID int64, forUpdate bool) ([]*Scope, error) {
	if repoID <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom repository ID")
	}
	scopes := make([]*Scope, 0)
	engine := db.GetEngine(ctx).Where("repo_id = ?", repoID).Asc("lower_name", "id")
	if forUpdate && branchroomSupportsForUpdate() {
		engine = engine.ForUpdate()
	}
	err := engine.Find(&scopes)
	return scopes, err
}
