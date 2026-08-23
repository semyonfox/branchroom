// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

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
	ActionWriteSource  Action = "write_source"
	ActionWriteReview  Action = "write_review"
	ActionMaterialize  Action = "materialize"
	ActionReadHistory  Action = "read_history"
	ActionPublish      Action = "publish"
	ActionManagePolicy Action = "manage_policy"
)

// IsActionValid reports whether action is persisted by the current model.
func IsActionValid(action Action) bool {
	switch action {
	case ActionDiscover, ActionReadSource, ActionWriteSource, ActionWriteReview, ActionMaterialize, ActionReadHistory, ActionPublish, ActionManagePolicy:
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
	// SurfaceBranchroomReceive classifies audit records from the Branchroom pre-receive admission path only.
	SurfaceBranchroomReceive Surface = "branchroom_receive"
)

// IsSurfaceValid reports whether surface is persisted by the current model.
func IsSurfaceValid(surface Surface) bool {
	switch surface {
	case SurfaceWeb, SurfaceAPI, SurfaceGitHTTP, SurfaceGitSSH, SurfaceActions, SurfaceSystem, SurfaceBranchroomReceive:
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
