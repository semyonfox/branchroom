// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"

	"gitea.dev/models/db"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
)

// Grant names one action for one subject and optional workload conditions.
type Grant struct {
	ID                int64              `xorm:"pk autoincr"`
	ScopeID           int64              `xorm:"INDEX(scope_action_subject) NOT NULL"`
	SubjectType       SubjectType        `xorm:"VARCHAR(16) INDEX(scope_action_subject) NOT NULL"`
	SubjectID         int64              `xorm:"INDEX(scope_action_subject) NOT NULL"`
	Action            Action             `xorm:"VARCHAR(32) INDEX(scope_action_subject) NOT NULL"`
	WorkflowID        string             `xorm:"VARCHAR(255) NOT NULL DEFAULT ''"`
	WorkflowRepoID    int64              `xorm:"NOT NULL DEFAULT 0"`
	WorkflowCommitSHA string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	Ref               string             `xorm:"VARCHAR(255) NOT NULL DEFAULT ''"`
	RunnerID          int64              `xorm:"NOT NULL DEFAULT 0"`
	NotBeforeUnix     timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"`
	ExpiresUnix       timeutil.TimeStamp `xorm:"INDEX NOT NULL DEFAULT 0"`
	RevokedUnix       timeutil.TimeStamp `xorm:"INDEX NOT NULL DEFAULT 0"`
	RevokedByID       int64              `xorm:"NOT NULL DEFAULT 0"`
	CreatedByID       int64              `xorm:"NOT NULL"`
	CreatedUnix       timeutil.TimeStamp `xorm:"created NOT NULL"`
	UpdatedUnix       timeutil.TimeStamp `xorm:"updated NOT NULL"`
}

// TableName returns the stable Branchroom grant table name.
func (Grant) TableName() string { return "branchroom_grant" }

// Validate checks structural invariants without deciding whether the grant applies.
func (grant *Grant) Validate() error {
	if grant.ScopeID <= 0 || grant.SubjectID <= 0 || grant.CreatedByID <= 0 || !IsSubjectTypeValid(grant.SubjectType) || !IsActionValid(grant.Action) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom grant identity")
	}
	if grant.WorkflowRepoID < 0 || grant.RunnerID < 0 || !IsConditionValue(grant.WorkflowID) || !IsConditionValue(grant.Ref) || (grant.WorkflowCommitSHA != "" && !IsGitObjectID(grant.WorkflowCommitSHA)) || grant.NotBeforeUnix < 0 || grant.ExpiresUnix < 0 || grant.RevokedUnix < 0 || grant.RevokedByID < 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom grant condition")
	}
	if grant.ExpiresUnix != 0 && grant.NotBeforeUnix != 0 && grant.ExpiresUnix <= grant.NotBeforeUnix {
		return util.NewInvalidArgumentErrorf("Branchroom grant expiry must follow its not-before time")
	}
	if grant.RevokedUnix == 0 && grant.RevokedByID != 0 || grant.RevokedUnix != 0 && grant.RevokedByID == 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom grant revocation")
	}
	if grant.SubjectType == SubjectTypeActionsTask {
		if grant.WorkflowID == "" || grant.WorkflowRepoID <= 0 || !IsGitObjectID(grant.WorkflowCommitSHA) || grant.Ref == "" || grant.RunnerID <= 0 || grant.ExpiresUnix == 0 {
			return util.NewInvalidArgumentErrorf("Branchroom Actions task grants require workflow source, ref, runner, and expiry")
		}
	} else if grant.WorkflowID != "" || grant.WorkflowRepoID != 0 || grant.WorkflowCommitSHA != "" || grant.Ref != "" || grant.RunnerID != 0 {
		return util.NewInvalidArgumentErrorf("Branchroom grant conditions require an Actions task subject")
	}
	if grant.Action == ActionMaterialize && grant.SubjectType != SubjectTypeUser {
		return util.NewInvalidArgumentErrorf("Branchroom environment materialization grants require a direct user")
	}
	if grant.Action == ActionReadSource && grant.SubjectType != SubjectTypeUser && grant.SubjectType != SubjectTypeTeam {
		return util.NewInvalidArgumentErrorf("Branchroom source-read grants require a direct user or organization team")
	}
	if grant.Action == ActionWriteSource && grant.SubjectType != SubjectTypeUser {
		return util.NewInvalidArgumentErrorf("Branchroom source-write grants require a direct user")
	}
	return nil
}

// IsActiveAt reports whether a grant is within its validity window.
func (grant *Grant) IsActiveAt(now timeutil.TimeStamp) bool {
	return grant.RevokedUnix == 0 && (grant.NotBeforeUnix == 0 || now >= grant.NotBeforeUnix) && (grant.ExpiresUnix == 0 || now < grant.ExpiresUnix)
}

// InsertGrant validates and inserts a grant through the caller's transaction context.
func InsertGrant(ctx context.Context, grant *Grant) error {
	if grant == nil {
		return util.NewInvalidArgumentErrorf("Branchroom grant is required")
	}
	if err := grant.Validate(); err != nil {
		return err
	}
	return db.Insert(ctx, grant)
}

// GetGrantByID returns a grant by its database ID.
func GetGrantByID(ctx context.Context, id int64) (*Grant, error) {
	return getGrantByID(ctx, id, false)
}

// GetGrantByIDForUpdate returns a grant with a row lock on databases that support it.
func GetGrantByIDForUpdate(ctx context.Context, id int64) (*Grant, error) {
	return getGrantByID(ctx, id, true)
}

func getGrantByID(ctx context.Context, id int64, forUpdate bool) (*Grant, error) {
	if id <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom grant ID")
	}
	grant := new(Grant)
	engine := db.GetEngine(ctx).ID(id)
	if forUpdate && branchroomSupportsForUpdate() {
		engine = engine.ForUpdate()
	}
	has, err := engine.Get(grant)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.NewNotExistErrorf("Branchroom grant %d does not exist", id)
	}
	return grant, nil
}

// FindActiveGrantsAt returns active grants without matching workload conditions.
func FindActiveGrantsAt(ctx context.Context, scopeID int64, action Action, now timeutil.TimeStamp) ([]*Grant, error) {
	if scopeID <= 0 || !IsActionValid(action) || now <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom grant query")
	}
	grants := make([]*Grant, 0)
	err := db.GetEngine(ctx).
		Where("scope_id = ? AND action = ? AND revoked_unix = 0 AND (not_before_unix = 0 OR not_before_unix <= ?) AND (expires_unix = 0 OR expires_unix > ?)", scopeID, action, now, now).
		Asc("id").
		Find(&grants)
	return grants, err
}

// HasUnexpiredSubjectGrant reports whether the subject holds a non-revoked,
// not-yet-expired grant for the scope and action, including one whose window
// has not started yet.
func HasUnexpiredSubjectGrant(ctx context.Context, scopeID int64, action Action, subjectType SubjectType, subjectID int64, now timeutil.TimeStamp) (bool, error) {
	if scopeID <= 0 || !IsActionValid(action) || !IsSubjectTypeValid(subjectType) || subjectID <= 0 || now <= 0 {
		return false, util.NewInvalidArgumentErrorf("invalid Branchroom grant query")
	}
	has, err := db.GetEngine(ctx).
		Where("scope_id = ? AND action = ? AND subject_type = ? AND subject_id = ? AND revoked_unix = 0 AND (expires_unix = 0 OR expires_unix > ?)", scopeID, action, subjectType, subjectID, now).
		Get(new(Grant))
	return has, err
}

// GetActiveDirectUserGrantAt returns one active direct-user grant for the exact
// scope, action, and user. It deliberately does not match teams, deploy keys, or
// Actions-task identities.
func GetActiveDirectUserGrantAt(ctx context.Context, scopeID, userID int64, action Action, now timeutil.TimeStamp) (*Grant, error) {
	if scopeID <= 0 || userID <= 0 || !IsActionValid(action) || now <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom direct-user grant query")
	}
	grant := new(Grant)
	has, err := db.GetEngine(ctx).
		Where("scope_id = ? AND subject_type = ? AND subject_id = ? AND action = ? AND revoked_unix = 0 AND (not_before_unix = 0 OR not_before_unix <= ?) AND (expires_unix = 0 OR expires_unix > ?)", scopeID, SubjectTypeUser, userID, action, now, now).
		Asc("id").
		Get(grant)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.NewNotExistErrorf("active Branchroom direct-user grant does not exist")
	}
	return grant, nil
}

// RevokeGrant records a revocation once and leaves the grant row intact for audit history.
func RevokeGrant(ctx context.Context, grantID, revokedByID int64, now timeutil.TimeStamp) error {
	if grantID <= 0 || revokedByID <= 0 || now <= 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom grant revocation")
	}
	affected, err := db.GetEngine(ctx).ID(grantID).Where("revoked_unix = 0").Cols("revoked_unix", "revoked_by_id").Update(&Grant{RevokedUnix: now, RevokedByID: revokedByID})
	if err != nil {
		return err
	}
	if affected != 1 {
		return util.NewNotExistErrorf("active Branchroom grant %d does not exist", grantID)
	}
	return nil
}
