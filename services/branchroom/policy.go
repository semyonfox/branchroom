// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"errors"
	"strconv"
	"strings"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	organization_model "gitea.dev/models/organization"
	access_model "gitea.dev/models/perm/access"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unit"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
)

const (
	ReasonFeatureDisabled        = "feature_disabled"
	ReasonScopeNotFound          = "scope_not_found"
	ReasonRepositoryMismatch     = "repository_mismatch"
	ReasonResourceMismatch       = "resource_mismatch"
	ReasonUnsupportedAction      = "unsupported_action"
	ReasonScopeInactive          = "scope_inactive"
	ReasonOrdinaryPermissionDeny = "ordinary_permission_denied"
	ReasonNoActiveGrant          = "no_active_grant"
	ReasonConditionMismatch      = "condition_mismatch"
	ReasonGrantMatched           = "grant_matched"
	ReasonMaterializeUnavailable = "materialize_unavailable"
	ReasonReadSourceUnavailable  = "read_source_unavailable"
	ReasonWriteSourceUnavailable = "write_source_unavailable"
)

// audit reason codes recorded alongside manage-policy and lifecycle decisions.
const (
	reasonScopeCreated                 = "scope_created"
	reasonWorkspaceCreated             = "workspace_created"
	reasonWorkspaceStoreUnavailable    = "store_unavailable"
	reasonWorkspaceImportFailed        = "import_failed"
	reasonWorkspaceImportVerifyFailed  = "import_verification_failed"
	reasonWorkspaceRouteKeyRotated     = "workspace_route_key_rotated"
	reasonDiscoveryGrantCreated        = "discovery_grant_created"
	reasonSourceReadGrantCreated       = "source_read_grant_created"
	reasonEnvironmentRevisionCreated   = "environment_revision_created"
	reasonEnvironmentLeaseIssued       = "environment_lease_issued"
	reasonEnvironmentLeaseExpired      = "environment_lease_expired"
	reasonEnvironmentLeaseClaimed      = "environment_lease_claimed"
	reasonEnvironmentLeaseDenied       = "environment_lease_denied"
	reasonEnvironmentLeaseMaterialized = "environment_lease_materialized"
	reasonEnvironmentLeaseFailed       = "environment_lease_failed"
)

// Actor is the concrete authenticated identity considered by policy.
type Actor struct {
	Type branchroom_model.ActorType
	ID   int64
}

// ActorFromUser preserves an Actions task identity instead of flattening it to its bot user.
func ActorFromUser(user *user_model.User) Actor {
	if user == nil {
		return Actor{Type: branchroom_model.ActorTypeAnonymous}
	}
	if taskID, ok := user_model.GetActionsUserTaskID(user); ok {
		return Actor{Type: branchroom_model.ActorTypeActionsTask, ID: taskID}
	}
	return Actor{Type: branchroom_model.ActorTypeUser, ID: user.ID}
}

// RepositoryCeiling captures the already-clamped ordinary Gitea permission for this operation.
type RepositoryCeiling struct {
	CodeRead  bool
	CodeWrite bool
}

// NewRepositoryCeiling derives the code-access ceiling from Gitea's calculated permission.
func NewRepositoryCeiling(permission access_model.Permission) RepositoryCeiling {
	return RepositoryCeiling{
		CodeRead:  permission.CanRead(unit.TypeCode),
		CodeWrite: permission.CanWrite(unit.TypeCode),
	}
}

// resolveDirectUserRepositoryCeiling reloads a direct user and their ordinary
// repository permission. It deliberately does not resolve Actions, deploy-key, or
// token identities as a user.
func resolveDirectUserRepositoryCeiling(ctx context.Context, repoID int64, actor Actor) (RepositoryCeiling, bool, error) {
	if repoID <= 0 || actor.Type != branchroom_model.ActorTypeUser || actor.ID <= 0 {
		return RepositoryCeiling{}, false, nil
	}
	user, err := user_model.GetUserByID(ctx, actor.ID)
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			return RepositoryCeiling{}, false, nil
		}
		return RepositoryCeiling{}, false, err
	}
	if !user.IsIndividual() || !user.IsActive || user.ProhibitLogin || user.MustChangePassword {
		return RepositoryCeiling{}, false, nil
	}
	repository, err := repo_model.GetRepositoryByID(ctx, repoID)
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			return RepositoryCeiling{}, false, nil
		}
		return RepositoryCeiling{}, false, err
	}
	if err := repository.LoadOwner(ctx); err != nil {
		return RepositoryCeiling{}, false, err
	}
	if repository.Owner == nil || (!repository.Owner.IsOrganization() && !repository.Owner.IsActive) {
		return RepositoryCeiling{}, false, nil
	}
	permission, err := access_model.GetIndividualUserRepoPermission(ctx, repository, user)
	if err != nil {
		return RepositoryCeiling{}, false, err
	}
	return NewRepositoryCeiling(permission), true, nil
}

// Request contains all facts used to evaluate one protected operation.
type Request struct {
	ScopeID           int64
	RepoID            int64
	Actor             Actor
	Ceiling           RepositoryCeiling
	Action            branchroom_model.Action
	ResourceType      branchroom_model.ResourceType
	ResourceID        string
	Surface           branchroom_model.Surface
	RequestID         string
	WorkflowID        string
	WorkflowRepoID    int64
	WorkflowCommitSHA string
	Ref               string
	RunnerID          int64
}

// Decision is the server-internal result of an evaluation.
type Decision struct {
	ID          int64
	Key         string
	GrantID     int64
	Allowed     bool
	ReasonCode  string
	ExpiresUnix timeutil.TimeStamp
	Scope       *branchroom_model.Scope
}

// Evaluator is the one policy interface that protected Branchroom operations use.
type Evaluator interface {
	Evaluate(context.Context, Request) (Decision, error)
}

// DBEvaluator evaluates persisted scopes and grants without caching decisions.
type DBEvaluator struct {
	Now func() timeutil.TimeStamp
}

// NewDBEvaluator returns the production evaluator with the system clock.
func NewDBEvaluator() *DBEvaluator {
	return &DBEvaluator{Now: timeutil.TimeStampNow}
}

// Evaluate composes the ordinary Gitea ceiling with an active Branchroom grant.
func (e *DBEvaluator) Evaluate(ctx context.Context, request Request) (Decision, error) {
	if err := validateRequest(request); err != nil {
		return Decision{}, err
	}
	if !setting.Branchroom.Enabled {
		return Decision{ReasonCode: ReasonFeatureDisabled}, nil
	}

	now := timeutil.TimeStampNow()
	if e != nil && e.Now != nil {
		now = e.Now()
	}
	if now <= 0 {
		return Decision{}, util.NewInvalidArgumentErrorf("invalid Branchroom policy clock")
	}

	return db.WithTx2(ctx, func(txCtx context.Context) (Decision, error) {
		scope, err := branchroom_model.GetScopeByID(txCtx, request.ScopeID)
		if err != nil {
			if errors.Is(err, util.ErrNotExist) {
				return Decision{ReasonCode: ReasonScopeNotFound}, nil
			}
			return Decision{}, err
		}

		result := Decision{ExpiresUnix: now}
		if request.RepoID != scope.RepoID {
			return e.record(txCtx, scope, result, request, now, branchroom_model.DecisionResultDeny, ReasonRepositoryMismatch, 0)
		}
		switch request.Action {
		case branchroom_model.ActionDiscover:
			if request.ResourceType != branchroom_model.ResourceTypeScope || request.ResourceID != scopeResourceID(scope.ID) {
				return e.record(txCtx, scope, result, request, now, branchroom_model.DecisionResultDeny, ReasonResourceMismatch, 0)
			}
		case branchroom_model.ActionReadSource:
			// This checks only the logical key shape. authorizeWorkspaceRead binds it
			// to a ready persisted workspace before returning a storage facade. The
			// Git HTTP surface is reachable only through its dedicated authorizer.
			if (request.Surface != branchroom_model.SurfaceSystem && request.Surface != branchroom_model.SurfaceGitHTTP) || request.Actor.Type != branchroom_model.ActorTypeUser {
				return e.record(txCtx, scope, result, request, now, branchroom_model.DecisionResultDeny, ReasonReadSourceUnavailable, 0)
			}
			if scope.Kind != branchroom_model.ScopeKindRepository || request.ResourceType != branchroom_model.ResourceTypeWorkspace || !isWorkspaceResourceID(request.ResourceID) {
				return e.record(txCtx, scope, result, request, now, branchroom_model.DecisionResultDeny, ReasonResourceMismatch, 0)
			}
		case branchroom_model.ActionWriteSource:
			if request.Surface != branchroom_model.SurfaceGitHTTP || request.Actor.Type != branchroom_model.ActorTypeUser {
				return e.record(txCtx, scope, result, request, now, branchroom_model.DecisionResultDeny, ReasonWriteSourceUnavailable, 0)
			}
			if scope.Kind != branchroom_model.ScopeKindRepository || request.ResourceType != branchroom_model.ResourceTypeWorkspace || !isWorkspaceResourceID(request.ResourceID) {
				return e.record(txCtx, scope, result, request, now, branchroom_model.DecisionResultDeny, ReasonResourceMismatch, 0)
			}
		case branchroom_model.ActionMaterialize:
			if !setting.BranchroomEnvironment.Enabled || request.Surface != branchroom_model.SurfaceSystem || request.Actor.Type != branchroom_model.ActorTypeUser {
				return e.record(txCtx, scope, result, request, now, branchroom_model.DecisionResultDeny, ReasonMaterializeUnavailable, 0)
			}
			if scope.Kind != branchroom_model.ScopeKindEnvironment || request.ResourceType != branchroom_model.ResourceTypeEnvironment || !isEnvironmentRevisionResourceID(scope.ID, request.ResourceID) {
				return e.record(txCtx, scope, result, request, now, branchroom_model.DecisionResultDeny, ReasonResourceMismatch, 0)
			}
		default:
			return e.record(txCtx, scope, result, request, now, branchroom_model.DecisionResultDeny, ReasonUnsupportedAction, 0)
		}
		if scope.State != branchroom_model.ScopeStateActive {
			return e.record(txCtx, scope, result, request, now, branchroom_model.DecisionResultDeny, ReasonScopeInactive, 0)
		}
		if request.Action == branchroom_model.ActionWriteSource {
			if !request.Ceiling.CodeRead || !request.Ceiling.CodeWrite {
				return e.record(txCtx, scope, result, request, now, branchroom_model.DecisionResultDeny, ReasonOrdinaryPermissionDeny, 0)
			}
			return e.record(txCtx, scope, result, request, now, branchroom_model.DecisionResultDeny, ReasonWriteSourceUnavailable, 0)
		}
		if !request.Ceiling.CodeRead {
			return e.record(txCtx, scope, result, request, now, branchroom_model.DecisionResultDeny, ReasonOrdinaryPermissionDeny, 0)
		}

		grant, reasonCode, err := findMatchingGrant(txCtx, scope, request, now)
		if err != nil {
			return Decision{}, err
		}
		if grant == nil {
			return e.record(txCtx, scope, result, request, now, branchroom_model.DecisionResultDeny, reasonCode, 0)
		}
		result.Scope = scope
		return e.record(txCtx, scope, result, request, now, branchroom_model.DecisionResultAllow, ReasonGrantMatched, grant.ID)
	})
}

func scopeResourceID(scopeID int64) string {
	return "scope/" + strconv.FormatInt(scopeID, 10)
}

func environmentRevisionResourceID(scopeID int64, revisionKey string) string {
	return scopeResourceID(scopeID) + "/environment-revision/" + revisionKey
}

func isEnvironmentRevisionResourceID(scopeID int64, resourceID string) bool {
	prefix := scopeResourceID(scopeID) + "/environment-revision/"
	revisionKey, ok := strings.CutPrefix(resourceID, prefix)
	return ok && branchroom_model.IsCanonicalUUID(revisionKey)
}

func isWorkspaceResourceID(resourceID string) bool {
	workspaceKey, ok := strings.CutPrefix(resourceID, "workspace/")
	return ok && branchroom_model.IsCanonicalUUID(workspaceKey)
}

func (request Request) ordinaryAllowed() bool {
	if request.Action == branchroom_model.ActionWriteSource {
		return request.Ceiling.CodeRead && request.Ceiling.CodeWrite
	}
	return request.Ceiling.CodeRead
}

func (e *DBEvaluator) record(ctx context.Context, scope *branchroom_model.Scope, result Decision, request Request, now timeutil.TimeStamp, outcome branchroom_model.DecisionResult, reasonCode string, grantID int64) (Decision, error) {
	persisted := &branchroom_model.Decision{
		ScopeID:         scope.ID,
		RepoID:          scope.RepoID,
		GrantID:         grantID,
		ActorType:       request.Actor.Type,
		ActorID:         request.Actor.ID,
		Action:          request.Action,
		ResourceType:    request.ResourceType,
		ResourceID:      request.ResourceID,
		Surface:         request.Surface,
		Result:          outcome,
		ReasonCode:      reasonCode,
		OrdinaryAllowed: request.ordinaryAllowed(),
		RequestID:       request.RequestID,
		ExpiresUnix:     now,
		CreatedUnix:     now,
	}
	_, err := branchroom_model.RecordDecisionAndAudit(ctx, scope, persisted)
	if err != nil {
		return Decision{}, err
	}
	result.Key = persisted.DecisionKey
	result.ID = persisted.ID
	result.GrantID = grantID
	result.Allowed = outcome == branchroom_model.DecisionResultAllow
	result.ReasonCode = reasonCode
	return result, nil
}

func findMatchingGrant(ctx context.Context, scope *branchroom_model.Scope, request Request, now timeutil.TimeStamp) (*branchroom_model.Grant, string, error) {
	grants, err := branchroom_model.FindActiveGrantsAt(ctx, scope.ID, request.Action, now)
	if err != nil {
		return nil, "", err
	}
	conditionMismatch := false
	for _, grant := range grants {
		if (request.Action == branchroom_model.ActionReadSource || request.Action == branchroom_model.ActionWriteSource) && grant.SubjectType != branchroom_model.SubjectTypeUser {
			continue
		}
		matched, conditionsMatch, err := grantMatches(ctx, scope, grant, request)
		if err != nil {
			return nil, "", err
		}
		if matched && conditionsMatch {
			return grant, ReasonGrantMatched, nil
		}
		if matched {
			conditionMismatch = true
		}
	}
	if conditionMismatch {
		return nil, ReasonConditionMismatch, nil
	}
	return nil, ReasonNoActiveGrant, nil
}

func grantMatches(ctx context.Context, scope *branchroom_model.Scope, grant *branchroom_model.Grant, request Request) (bool, bool, error) {
	switch grant.SubjectType {
	case branchroom_model.SubjectTypeUser:
		return request.Actor.Type == branchroom_model.ActorTypeUser && request.Actor.ID == grant.SubjectID, true, nil
	case branchroom_model.SubjectTypeActionsTask:
		if request.Actor.Type != branchroom_model.ActorTypeActionsTask || request.Actor.ID != grant.SubjectID {
			return false, false, nil
		}
		return true, grant.WorkflowID == request.WorkflowID && grant.WorkflowRepoID == request.WorkflowRepoID && grant.WorkflowCommitSHA == request.WorkflowCommitSHA && grant.Ref == request.Ref && grant.RunnerID == request.RunnerID, nil
	case branchroom_model.SubjectTypeDeployKey:
		return request.Actor.Type == branchroom_model.ActorTypeDeployKey && request.Actor.ID == grant.SubjectID, true, nil
	case branchroom_model.SubjectTypeTeam:
		if request.Actor.Type != branchroom_model.ActorTypeUser {
			return false, false, nil
		}
		repository, err := repo_model.GetRepositoryByID(ctx, scope.RepoID)
		if err != nil {
			return false, false, err
		}
		member, err := organization_model.IsTeamMember(ctx, repository.OwnerID, grant.SubjectID, request.Actor.ID)
		return member, true, err
	default:
		return false, false, util.NewInvalidArgumentErrorf("invalid Branchroom grant subject")
	}
}

func validateRequest(request Request) error {
	if request.ScopeID <= 0 || request.RepoID <= 0 || request.WorkflowRepoID < 0 || request.RunnerID < 0 || !branchroom_model.IsActorTypeValid(request.Actor.Type) || !branchroom_model.IsActionValid(request.Action) || !branchroom_model.IsResourceTypeValid(request.ResourceType) || !branchroom_model.IsSurfaceValid(request.Surface) || !branchroom_model.IsConditionValue(request.WorkflowID) || !branchroom_model.IsConditionValue(request.Ref) || request.WorkflowCommitSHA != "" && !branchroom_model.IsGitObjectID(request.WorkflowCommitSHA) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom policy request")
	}
	if request.Actor.Type == branchroom_model.ActorTypeAnonymous || request.Actor.Type == branchroom_model.ActorTypeSystem {
		if request.Actor.ID != 0 {
			return util.NewInvalidArgumentErrorf("invalid Branchroom policy actor")
		}
	} else if request.Actor.ID <= 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom policy actor")
	}
	if request.Actor.Type == branchroom_model.ActorTypeActionsTask {
		if request.WorkflowID == "" || request.WorkflowRepoID <= 0 || !branchroom_model.IsGitObjectID(request.WorkflowCommitSHA) || request.Ref == "" || request.RunnerID <= 0 {
			return util.NewInvalidArgumentErrorf("incomplete Branchroom Actions task facts")
		}
	} else if request.WorkflowID != "" || request.WorkflowRepoID != 0 || request.WorkflowCommitSHA != "" || request.Ref != "" || request.RunnerID != 0 {
		return util.NewInvalidArgumentErrorf("unexpected Branchroom workload facts")
	}
	return nil
}
