// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"gitea.dev/models/db"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/google/uuid"
)

const (
	maxScopeNameLength  = 255
	maxConditionLength  = 255
	maxResourceIDLength = 255
	maxRequestIDLength  = 64
	maxReasonCodeLength = 64
	sha256HexLength     = 64
)

var ErrAuditConflict = errors.New("branchroom audit head changed")

// ScopeKind identifies the resource category a scope protects.
type ScopeKind string

const (
	ScopeKindRepository  ScopeKind = "repository"
	ScopeKindEnvironment ScopeKind = "environment"
)

// IsScopeKindValid reports whether kind is persisted by the current model.
func IsScopeKindValid(kind ScopeKind) bool {
	return kind == ScopeKindRepository || kind == ScopeKindEnvironment
}

// ScopeState controls whether a scope can grant access.
type ScopeState string

const (
	ScopeStateActive   ScopeState = "active"
	ScopeStateDisabled ScopeState = "disabled"
	ScopeStateRetired  ScopeState = "retired"
)

// IsScopeStateValid reports whether state is persisted by the current model.
func IsScopeStateValid(state ScopeState) bool {
	switch state {
	case ScopeStateActive, ScopeStateDisabled, ScopeStateRetired:
		return true
	default:
		return false
	}
}

// ScopeOwnerType identifies the recorded scope owner. It is metadata, not an implicit grant.
type ScopeOwnerType string

const (
	ScopeOwnerTypeUser         ScopeOwnerType = "user"
	ScopeOwnerTypeTeam         ScopeOwnerType = "team"
	ScopeOwnerTypeOrganization ScopeOwnerType = "organization"
)

// IsScopeOwnerTypeValid reports whether ownerType is persisted by the current model.
func IsScopeOwnerTypeValid(ownerType ScopeOwnerType) bool {
	switch ownerType {
	case ScopeOwnerTypeUser, ScopeOwnerTypeTeam, ScopeOwnerTypeOrganization:
		return true
	default:
		return false
	}
}

// SubjectType identifies the principal named by a grant.
type SubjectType string

const (
	SubjectTypeUser        SubjectType = "user"
	SubjectTypeTeam        SubjectType = "team"
	SubjectTypeActionsTask SubjectType = "actions_task"
	SubjectTypeDeployKey   SubjectType = "deploy_key"
)

// IsSubjectTypeValid reports whether subjectType is persisted by the current model.
func IsSubjectTypeValid(subjectType SubjectType) bool {
	switch subjectType {
	case SubjectTypeUser, SubjectTypeTeam, SubjectTypeActionsTask, SubjectTypeDeployKey:
		return true
	default:
		return false
	}
}

// Action is an operation a scope may permit.
type Action string

const (
	ActionDiscover     Action = "discover"
	ActionReadSource   Action = "read_source"
	ActionWriteReview  Action = "write_review"
	ActionMaterialize  Action = "materialize"
	ActionReadHistory  Action = "read_history"
	ActionPublish      Action = "publish"
	ActionManagePolicy Action = "manage_policy"
)

// IsActionValid reports whether action is persisted by the current model.
func IsActionValid(action Action) bool {
	switch action {
	case ActionDiscover, ActionReadSource, ActionWriteReview, ActionMaterialize, ActionReadHistory, ActionPublish, ActionManagePolicy:
		return true
	default:
		return false
	}
}

// ActorType identifies the identity that requested a decision.
type ActorType string

const (
	ActorTypeAnonymous   ActorType = "anonymous"
	ActorTypeUser        ActorType = "user"
	ActorTypeActionsTask ActorType = "actions_task"
	ActorTypeDeployKey   ActorType = "deploy_key"
	ActorTypeSystem      ActorType = "system"
)

// IsActorTypeValid reports whether actorType is persisted by the current model.
func IsActorTypeValid(actorType ActorType) bool {
	switch actorType {
	case ActorTypeAnonymous, ActorTypeUser, ActorTypeActionsTask, ActorTypeDeployKey, ActorTypeSystem:
		return true
	default:
		return false
	}
}

// ResourceType identifies a logical resource and never a storage path.
type ResourceType string

const (
	ResourceTypeScope       ResourceType = "scope"
	ResourceTypeRepository  ResourceType = "repository"
	ResourceTypeEnvironment ResourceType = "environment"
	ResourceTypeWorkspace   ResourceType = "workspace"
)

// IsResourceTypeValid reports whether resourceType is persisted by the current model.
func IsResourceTypeValid(resourceType ResourceType) bool {
	switch resourceType {
	case ResourceTypeScope, ResourceTypeRepository, ResourceTypeEnvironment, ResourceTypeWorkspace:
		return true
	default:
		return false
	}
}

// Surface identifies where a policy decision was requested.
type Surface string

const (
	SurfaceWeb     Surface = "web"
	SurfaceAPI     Surface = "api"
	SurfaceGitHTTP Surface = "git_http"
	SurfaceGitSSH  Surface = "git_ssh"
	SurfaceActions Surface = "actions"
	SurfaceSystem  Surface = "system"
)

// IsSurfaceValid reports whether surface is persisted by the current model.
func IsSurfaceValid(surface Surface) bool {
	switch surface {
	case SurfaceWeb, SurfaceAPI, SurfaceGitHTTP, SurfaceGitSSH, SurfaceActions, SurfaceSystem:
		return true
	default:
		return false
	}
}

// DecisionResult is the evaluator outcome recorded in decisions and audit events.
type DecisionResult string

const (
	DecisionResultAllow DecisionResult = "allow"
	DecisionResultDeny  DecisionResult = "deny"
)

// IsDecisionResultValid reports whether result is persisted by the current model.
func IsDecisionResultValid(result DecisionResult) bool {
	return result == DecisionResultAllow || result == DecisionResultDeny
}

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
	AuditHeadHash string             `xorm:"CHAR(64) NOT NULL DEFAULT ''"`
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
	return nil
}

// IsActiveAt reports whether a grant is within its validity window.
func (grant *Grant) IsActiveAt(now timeutil.TimeStamp) bool {
	return grant.RevokedUnix == 0 && (grant.NotBeforeUnix == 0 || now >= grant.NotBeforeUnix) && (grant.ExpiresUnix == 0 || now < grant.ExpiresUnix)
}

// Decision records the safe result of one policy evaluation.
type Decision struct {
	ID              int64              `xorm:"pk autoincr"`
	DecisionKey     string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	ScopeID         int64              `xorm:"INDEX(scope_created) NOT NULL"`
	RepoID          int64              `xorm:"NOT NULL"`
	GrantID         int64              `xorm:"INDEX NOT NULL DEFAULT 0"`
	ActorType       ActorType          `xorm:"VARCHAR(16) INDEX(actor_created) NOT NULL"`
	ActorID         int64              `xorm:"INDEX(actor_created) NOT NULL"`
	Action          Action             `xorm:"VARCHAR(32) NOT NULL"`
	ResourceType    ResourceType       `xorm:"VARCHAR(32) NOT NULL"`
	ResourceID      string             `xorm:"VARCHAR(255) NOT NULL"`
	Surface         Surface            `xorm:"VARCHAR(32) NOT NULL"`
	Result          DecisionResult     `xorm:"VARCHAR(8) NOT NULL"`
	ReasonCode      string             `xorm:"VARCHAR(64) NOT NULL"`
	OrdinaryAllowed bool               `xorm:"NOT NULL"`
	RequestID       string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	ExpiresUnix     timeutil.TimeStamp `xorm:"INDEX NOT NULL DEFAULT 0"`
	CreatedUnix     timeutil.TimeStamp `xorm:"created INDEX(scope_created) INDEX(actor_created) NOT NULL"`
}

// TableName returns the stable Branchroom decision table name.
func (Decision) TableName() string { return "branchroom_decision" }

// Validate checks that a decision carries only bounded safe metadata.
func (decision *Decision) Validate() error {
	if !isCanonicalUUID(decision.DecisionKey) || decision.ScopeID <= 0 || decision.RepoID <= 0 || decision.GrantID < 0 || !IsActorTypeValid(decision.ActorType) || !IsActionValid(decision.Action) || !IsResourceTypeValid(decision.ResourceType) || !IsSurfaceValid(decision.Surface) || !IsDecisionResultValid(decision.Result) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom decision metadata")
	}
	if !isActorIDValid(decision.ActorType, decision.ActorID) || !isSafeLogicalID(decision.ResourceID, maxResourceIDLength) || !isReasonCode(decision.ReasonCode) || !isOptionalSafeLogicalID(decision.RequestID, maxRequestIDLength) || decision.ExpiresUnix <= 0 || decision.CreatedUnix != 0 && decision.ExpiresUnix < decision.CreatedUnix {
		return util.NewInvalidArgumentErrorf("invalid Branchroom decision safe metadata")
	}
	return nil
}

// AuditEvent is one immutable, hash-linked event in a scope audit stream.
type AuditEvent struct {
	ID              int64              `xorm:"pk autoincr"`
	EventKey        string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	DecisionKey     string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	ScopeID         int64              `xorm:"UNIQUE(scope_sequence) INDEX(scope_created) NOT NULL"`
	Sequence        int64              `xorm:"UNIQUE(scope_sequence) NOT NULL"`
	RepoID          int64              `xorm:"NOT NULL"`
	GrantID         int64              `xorm:"NOT NULL DEFAULT 0"`
	ActorType       ActorType          `xorm:"VARCHAR(16) INDEX(actor_created) NOT NULL"`
	ActorID         int64              `xorm:"INDEX(actor_created) NOT NULL"`
	Action          Action             `xorm:"VARCHAR(32) NOT NULL"`
	ResourceType    ResourceType       `xorm:"VARCHAR(32) NOT NULL"`
	ResourceID      string             `xorm:"VARCHAR(255) NOT NULL"`
	Surface         Surface            `xorm:"VARCHAR(32) NOT NULL"`
	Result          DecisionResult     `xorm:"VARCHAR(8) NOT NULL"`
	ReasonCode      string             `xorm:"VARCHAR(64) NOT NULL"`
	OrdinaryAllowed bool               `xorm:"NOT NULL"`
	RequestID       string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	PreviousHash    string             `xorm:"CHAR(64) NOT NULL DEFAULT ''"`
	EventHash       string             `xorm:"CHAR(64) UNIQUE NOT NULL"`
	CreatedUnix     timeutil.TimeStamp `xorm:"created INDEX(scope_created) INDEX(actor_created) NOT NULL"`
}

// TableName returns the stable Branchroom audit-event table name.
func (AuditEvent) TableName() string { return "branchroom_audit_event" }

// Validate checks structural invariants for an immutable audit event.
func (event *AuditEvent) Validate() error {
	if !isCanonicalUUID(event.EventKey) || !isCanonicalUUID(event.DecisionKey) || event.ScopeID <= 0 || event.Sequence <= 0 || event.RepoID <= 0 || event.GrantID < 0 || !IsActorTypeValid(event.ActorType) || !IsActionValid(event.Action) || !IsResourceTypeValid(event.ResourceType) || !IsSurfaceValid(event.Surface) || !IsDecisionResultValid(event.Result) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom audit metadata")
	}
	if !isActorIDValid(event.ActorType, event.ActorID) || !isSafeLogicalID(event.ResourceID, maxResourceIDLength) || !isReasonCode(event.ReasonCode) || !isOptionalSafeLogicalID(event.RequestID, maxRequestIDLength) || !isSHA256Hex(event.EventHash) || event.EventHash != event.ComputeHash() || event.CreatedUnix <= 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom audit safe metadata")
	}
	if event.Sequence == 1 && event.PreviousHash != "" || event.Sequence > 1 && !isSHA256Hex(event.PreviousHash) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom audit chain")
	}
	return nil
}

// ComputeHash returns the canonical hash for event without using its database ID or hash field.
func (event *AuditEvent) ComputeHash() string {
	hash := sha256.New()
	for _, field := range []string{
		"branchroom-audit-v1",
		event.EventKey,
		event.DecisionKey,
		strconv.FormatInt(event.ScopeID, 10),
		strconv.FormatInt(event.Sequence, 10),
		strconv.FormatInt(event.RepoID, 10),
		strconv.FormatInt(event.GrantID, 10),
		string(event.ActorType),
		strconv.FormatInt(event.ActorID, 10),
		string(event.Action),
		string(event.ResourceType),
		event.ResourceID,
		string(event.Surface),
		string(event.Result),
		event.ReasonCode,
		strconv.FormatBool(event.OrdinaryAllowed),
		event.RequestID,
		event.PreviousHash,
		strconv.FormatInt(int64(event.CreatedUnix), 10),
	} {
		_, _ = hash.Write([]byte(strconv.Itoa(len(field))))
		_, _ = hash.Write([]byte{':'})
		_, _ = hash.Write([]byte(field))
	}
	return hex.EncodeToString(hash.Sum(nil))
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

// RecordDecisionAndAudit writes a decision and its audit event atomically.
func RecordDecisionAndAudit(ctx context.Context, scope *Scope, decision *Decision) (*AuditEvent, error) {
	if scope == nil || decision == nil || scope.ID <= 0 {
		return nil, util.NewInvalidArgumentErrorf("Branchroom scope and decision are required")
	}

	var event *AuditEvent
	err := db.WithTx(ctx, func(txCtx context.Context) error {
		current, err := GetScopeByID(txCtx, scope.ID)
		if err != nil {
			return err
		}
		if decision.ScopeID != current.ID || decision.RepoID != current.RepoID {
			return util.NewInvalidArgumentErrorf("Branchroom decision does not match its scope")
		}

		now := timeutil.TimeStampNow()
		if decision.DecisionKey == "" {
			decision.DecisionKey = uuid.NewString()
		}
		if decision.CreatedUnix == 0 {
			decision.CreatedUnix = now
		}
		if decision.ExpiresUnix == 0 {
			decision.ExpiresUnix = decision.CreatedUnix
		}
		event = &AuditEvent{
			EventKey:        uuid.NewString(),
			DecisionKey:     decision.DecisionKey,
			ScopeID:         current.ID,
			Sequence:        current.AuditSequence + 1,
			RepoID:          current.RepoID,
			GrantID:         decision.GrantID,
			ActorType:       decision.ActorType,
			ActorID:         decision.ActorID,
			Action:          decision.Action,
			ResourceType:    decision.ResourceType,
			ResourceID:      decision.ResourceID,
			Surface:         decision.Surface,
			Result:          decision.Result,
			ReasonCode:      decision.ReasonCode,
			OrdinaryAllowed: decision.OrdinaryAllowed,
			RequestID:       decision.RequestID,
			PreviousHash:    current.AuditHeadHash,
			CreatedUnix:     decision.CreatedUnix,
		}
		event.EventHash = event.ComputeHash()

		affected, err := db.GetEngine(txCtx).ID(current.ID).
			Where("audit_sequence = ? AND audit_head_hash = ?", current.AuditSequence, current.AuditHeadHash).
			Cols("audit_sequence", "audit_head_hash").
			Update(&Scope{AuditSequence: event.Sequence, AuditHeadHash: event.EventHash})
		if err != nil {
			return err
		}
		if affected != 1 {
			return ErrAuditConflict
		}
		if err := InsertDecision(txCtx, decision); err != nil {
			return err
		}
		if err := InsertAuditEvent(txCtx, event); err != nil {
			return err
		}
		*scope = *current
		scope.AuditSequence = event.Sequence
		scope.AuditHeadHash = event.EventHash
		return nil
	})
	return event, err
}

// InsertDecision validates and inserts a decision through the caller's transaction context.
func InsertDecision(ctx context.Context, decision *Decision) error {
	if decision == nil {
		return util.NewInvalidArgumentErrorf("Branchroom decision is required")
	}
	if err := decision.Validate(); err != nil {
		return err
	}
	return db.Insert(ctx, decision)
}

// InsertAuditEvent validates and inserts an immutable audit event through the caller's transaction context.
func InsertAuditEvent(ctx context.Context, event *AuditEvent) error {
	if event == nil {
		return util.NewInvalidArgumentErrorf("Branchroom audit event is required")
	}
	if err := event.Validate(); err != nil {
		return err
	}
	return db.Insert(ctx, event)
}

func isCanonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func isScopeName(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || utf8.RuneCountInString(value) > maxScopeNameLength {
		return false
	}
	return !strings.ContainsFunc(value, unicode.IsControl)
}

func isSHA256Hex(value string) bool {
	if len(value) != sha256HexLength || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// IsGitObjectID reports whether value is a canonical SHA-1 or SHA-256 Git object ID.
func IsGitObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// IsConditionValue reports whether value can safely be stored as a workload condition.
func IsConditionValue(value string) bool {
	if len(value) > maxConditionLength || value != strings.TrimSpace(value) {
		return false
	}
	return !strings.ContainsFunc(value, unicode.IsControl)
}

func isActorIDValid(actorType ActorType, actorID int64) bool {
	if actorType == ActorTypeAnonymous || actorType == ActorTypeSystem {
		return actorID == 0
	}
	return actorID > 0
}

func isOptionalSafeLogicalID(value string, maxLength int) bool {
	return value == "" || isSafeLogicalID(value, maxLength)
}

func isSafeLogicalID(value string, maxLength int) bool {
	if value == "" || len(value) > maxLength || value != strings.TrimSpace(value) {
		return false
	}
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && !strings.ContainsRune("._-/:", r) {
			return false
		}
	}
	return true
}

func isReasonCode(value string) bool {
	if len(value) == 0 || len(value) > maxReasonCodeLength {
		return false
	}
	for index, r := range value {
		if index == 0 {
			if r < 'a' || r > 'z' {
				return false
			}
			continue
		}
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func branchroomSupportsForUpdate() bool {
	return setting.Database.Type.IsMySQL() || setting.Database.Type.IsPostgreSQL()
}

func init() {
	db.RegisterModel(new(Scope))
	db.RegisterModel(new(Grant))
	db.RegisterModel(new(Decision))
	db.RegisterModel(new(AuditEvent))
}
