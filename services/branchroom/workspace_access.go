// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"errors"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/git/gitrepo"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
)

const ReasonWorkspaceUnavailable = "workspace_unavailable"

var errWorkspaceReadDenied = errors.New("Branchroom workspace read denied")

// authorizeWorkspaceReadRequest identifies an internal direct-user source-read request.
// It intentionally carries neither a caller-asserted policy actor nor a storage identity.
type authorizeWorkspaceReadRequest struct {
	RepoID       int64
	WorkspaceKey string
	UserID       int64
}

// AuthorizeWorkspaceGitHTTPReadRequest identifies a smart HTTP read request by
// its opaque public route key. It carries no caller-asserted storage identity.
type AuthorizeWorkspaceGitHTTPReadRequest struct {
	RepoID   int64
	RouteKey string
	UserID   int64
}

// AuthorizeWorkspaceGitHTTPWriteRequest identifies a restricted smart HTTP
// receive request by its opaque public route key. It carries no caller-asserted
// storage identity or receive credential.
type AuthorizeWorkspaceGitHTTPWriteRequest struct {
	RepoID   int64
	RouteKey string
	UserID   int64
}

type authorizedWorkspaceRead struct {
	storeID string
	denied  bool
}

type workspaceReadIdentity struct {
	workspaceID  int64
	scopeID      int64
	workspaceKey string
	routeKey     string
}

func (identity workspaceReadIdentity) matches(workspace *branchroom_model.Workspace) bool {
	if workspace == nil || workspace.ID != identity.workspaceID {
		return false
	}
	if identity.workspaceKey != "" {
		return workspace.WorkspaceKey == identity.workspaceKey
	}
	return workspace.RouteKey == identity.routeKey
}

// authorizeWorkspaceRead resolves one ready isolated store only after a fresh
// direct-user policy decision. It has no route, URL, CLI, or Git transport caller.
func authorizeWorkspaceRead(ctx context.Context, request authorizeWorkspaceReadRequest) (gitrepo.RepositoryFacade, error) {
	if !setting.Branchroom.Enabled {
		return nil, workspaceReadNotFound()
	}
	if request.RepoID <= 0 || request.UserID <= 0 {
		return nil, workspaceReadNotFound()
	}
	if !branchroom_model.IsCanonicalUUID(request.WorkspaceKey) || db.InTransaction(ctx) {
		return nil, workspaceReadNotFound()
	}

	initial, err := branchroom_model.GetWorkspaceByKey(ctx, request.WorkspaceKey)
	if err != nil {
		if errors.Is(err, util.ErrInvalidArgument) || errors.Is(err, util.ErrNotExist) {
			return nil, workspaceReadNotFound()
		}
		return nil, err
	}
	return authorizeWorkspaceReadByIdentity(ctx, request.RepoID, request.UserID, workspaceReadIdentity{
		workspaceID:  initial.ID,
		scopeID:      initial.ScopeID,
		workspaceKey: request.WorkspaceKey,
	}, branchroom_model.SurfaceSystem)
}

// AuthorizeWorkspaceGitHTTPRead resolves one ready isolated store for a direct
// user only after a fresh smart HTTP policy decision. It is the sole service
// entry point that can return a restricted facade for the Git HTTP surface.
func AuthorizeWorkspaceGitHTTPRead(ctx context.Context, request AuthorizeWorkspaceGitHTTPReadRequest) (gitrepo.RepositoryFacade, error) {
	if !setting.Branchroom.Enabled || request.RepoID <= 0 || request.UserID <= 0 || !branchroom_model.IsWorkspaceRouteKey(request.RouteKey) || db.InTransaction(ctx) {
		return nil, workspaceReadNotFound()
	}

	initial, err := branchroom_model.GetWorkspaceByRouteKeyAndRepoID(ctx, request.RouteKey, request.RepoID)
	if err != nil {
		if errors.Is(err, util.ErrInvalidArgument) || errors.Is(err, util.ErrNotExist) {
			return nil, workspaceReadNotFound()
		}
		return nil, err
	}
	return authorizeWorkspaceReadByIdentity(ctx, request.RepoID, request.UserID, workspaceReadIdentity{
		workspaceID: initial.ID,
		scopeID:     initial.ScopeID,
		routeKey:    request.RouteKey,
	}, branchroom_model.SurfaceGitHTTP)
}

// AuthorizeWorkspaceGitHTTPWrite resolves one ready isolated store for a user
// holding source-read and direct source-write access. It is used only for
// receive-pack advertisement; the RPC still creates a one-use sealed session.
func AuthorizeWorkspaceGitHTTPWrite(ctx context.Context, request AuthorizeWorkspaceGitHTTPWriteRequest) (gitrepo.RepositoryFacade, error) {
	if !setting.Branchroom.Enabled || request.RepoID <= 0 || request.UserID <= 0 || !branchroom_model.IsWorkspaceRouteKey(request.RouteKey) || db.InTransaction(ctx) {
		return nil, workspaceReadNotFound()
	}

	initial, err := branchroom_model.GetWorkspaceByRouteKeyAndRepoID(ctx, request.RouteKey, request.RepoID)
	if err != nil {
		if errors.Is(err, util.ErrInvalidArgument) || errors.Is(err, util.ErrNotExist) {
			return nil, workspaceReadNotFound()
		}
		return nil, err
	}
	return authorizeWorkspaceWriteByIdentity(ctx, request.RepoID, request.UserID, workspaceReadIdentity{
		workspaceID: initial.ID,
		scopeID:     initial.ScopeID,
		routeKey:    request.RouteKey,
	})
}

func authorizeWorkspaceReadByIdentity(ctx context.Context, repoID, userID int64, identity workspaceReadIdentity, surface branchroom_model.Surface) (gitrepo.RepositoryFacade, error) {
	authorized, err := db.WithTx2(ctx, func(txCtx context.Context) (authorizedWorkspaceRead, error) {
		switch surface {
		case branchroom_model.SurfaceSystem:
			if err := lockRepositoryLifecycle(txCtx, repoID); err != nil {
				return authorizedWorkspaceRead{}, err
			}
		case branchroom_model.SurfaceGitHTTP:
			if err := lockRepositoryLifecycleForRead(txCtx, repoID); err != nil {
				return authorizedWorkspaceRead{}, err
			}
		default:
			return authorizedWorkspaceRead{}, errWorkspaceReadDenied
		}

		scope, err := branchroom_model.GetScopeByIDForUpdate(txCtx, identity.scopeID)
		if err != nil {
			if errors.Is(err, util.ErrNotExist) {
				return authorizedWorkspaceRead{}, errWorkspaceReadDenied
			}
			return authorizedWorkspaceRead{}, err
		}
		workspace, err := branchroom_model.GetWorkspaceByIDForUpdate(txCtx, identity.workspaceID)
		if err != nil {
			if errors.Is(err, util.ErrNotExist) {
				return authorizedWorkspaceRead{}, errWorkspaceReadDenied
			}
			return authorizedWorkspaceRead{}, err
		}
		if scope.Validate() != nil || workspace.Validate() != nil || !identity.matches(workspace) || scope.RepoID != repoID || workspace.ScopeID != scope.ID || workspace.RepoID != repoID || workspace.RepoID != scope.RepoID {
			return authorizedWorkspaceRead{}, errWorkspaceReadDenied
		}

		actor := Actor{Type: branchroom_model.ActorTypeUser, ID: userID}
		ceiling, _, err := resolveDirectUserRepositoryCeiling(txCtx, repoID, actor)
		if err != nil {
			return authorizedWorkspaceRead{}, err
		}
		if scope.RepoID == repoID && scope.Kind == branchroom_model.ScopeKindRepository && scope.State == branchroom_model.ScopeStateActive && workspace.State != branchroom_model.WorkspaceStateReady {
			if err := recordWorkspaceReadUnavailable(txCtx, scope, workspace, actor, ceiling, surface); err != nil {
				return authorizedWorkspaceRead{}, err
			}
			return authorizedWorkspaceRead{denied: true}, nil
		}

		decision, err := NewDBEvaluator().Evaluate(txCtx, Request{
			ScopeID:      scope.ID,
			RepoID:       repoID,
			Actor:        actor,
			Ceiling:      ceiling,
			Action:       branchroom_model.ActionReadSource,
			ResourceType: branchroom_model.ResourceTypeWorkspace,
			ResourceID:   workspaceResourceID(workspace.WorkspaceKey),
			Surface:      surface,
		})
		if err != nil {
			return authorizedWorkspaceRead{}, err
		}
		if !decision.Allowed {
			return authorizedWorkspaceRead{denied: true}, nil
		}
		return authorizedWorkspaceRead{storeID: workspace.StoreID}, nil
	})
	if err != nil {
		if errors.Is(err, errWorkspaceReadDenied) {
			return nil, workspaceReadNotFound()
		}
		return nil, err
	}
	if authorized.denied {
		return nil, workspaceReadNotFound()
	}
	restricted, err := branchroom_module.RestrictedRepository(authorized.storeID)
	if err != nil {
		return nil, workspaceReadNotFound()
	}
	return restricted, nil
}

func authorizeWorkspaceWriteByIdentity(ctx context.Context, repoID, userID int64, identity workspaceReadIdentity) (gitrepo.RepositoryFacade, error) {
	authorized, err := db.WithTx2(ctx, func(txCtx context.Context) (authorizedWorkspaceRead, error) {
		if err := lockRepositoryLifecycleForRead(txCtx, repoID); err != nil {
			return authorizedWorkspaceRead{}, err
		}

		scope, err := branchroom_model.GetScopeByIDForUpdate(txCtx, identity.scopeID)
		if err != nil {
			if errors.Is(err, util.ErrNotExist) {
				return authorizedWorkspaceRead{}, errWorkspaceReadDenied
			}
			return authorizedWorkspaceRead{}, err
		}
		workspace, err := branchroom_model.GetWorkspaceByIDForUpdate(txCtx, identity.workspaceID)
		if err != nil {
			if errors.Is(err, util.ErrNotExist) {
				return authorizedWorkspaceRead{}, errWorkspaceReadDenied
			}
			return authorizedWorkspaceRead{}, err
		}
		if scope.Validate() != nil || workspace.Validate() != nil || !identity.matches(workspace) ||
			scope.RepoID != repoID || workspace.ScopeID != scope.ID || workspace.RepoID != repoID || workspace.RepoID != scope.RepoID ||
			scope.Kind != branchroom_model.ScopeKindRepository || scope.State != branchroom_model.ScopeStateActive || workspace.State != branchroom_model.WorkspaceStateReady {
			return authorizedWorkspaceRead{denied: true}, nil
		}

		_, allowed, err := authorizeSealedSourceWriteAdmission(txCtx, scope, workspace, userID)
		if err != nil {
			return authorizedWorkspaceRead{}, err
		}
		if !allowed || branchroom_module.VerifyRestrictedStoreHook(txCtx, setting.Branchroom.RestrictedRepoRootPath, workspace.StoreID) != nil {
			return authorizedWorkspaceRead{denied: true}, nil
		}
		return authorizedWorkspaceRead{storeID: workspace.StoreID}, nil
	})
	if err != nil {
		if errors.Is(err, errWorkspaceReadDenied) {
			return nil, workspaceReadNotFound()
		}
		return nil, err
	}
	if authorized.denied {
		return nil, workspaceReadNotFound()
	}
	restricted, err := branchroom_module.RestrictedRepository(authorized.storeID)
	if err != nil {
		return nil, workspaceReadNotFound()
	}
	return restricted, nil
}

func recordWorkspaceReadUnavailable(ctx context.Context, scope *branchroom_model.Scope, workspace *branchroom_model.Workspace, actor Actor, ceiling RepositoryCeiling, surface branchroom_model.Surface) error {
	now := timeutil.TimeStampNow()
	_, err := branchroom_model.RecordDecisionAndAudit(ctx, scope, &branchroom_model.Decision{
		ScopeID:         scope.ID,
		RepoID:          scope.RepoID,
		ActorType:       actor.Type,
		ActorID:         actor.ID,
		Action:          branchroom_model.ActionReadSource,
		ResourceType:    branchroom_model.ResourceTypeWorkspace,
		ResourceID:      workspaceResourceID(workspace.WorkspaceKey),
		Surface:         surface,
		Result:          branchroom_model.DecisionResultDeny,
		ReasonCode:      ReasonWorkspaceUnavailable,
		OrdinaryAllowed: ceiling.CodeRead,
		ExpiresUnix:     now,
		CreatedUnix:     now,
	})
	return err
}

func workspaceReadNotFound() error {
	return util.NewNotExistErrorf("Branchroom workspace does not exist")
}
