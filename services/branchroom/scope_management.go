// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"strconv"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	organization_model "gitea.dev/models/organization"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
)

// CreateRepositoryScopeRequest contains the owner-authorized bootstrap facts for a new scope.
type CreateRepositoryScopeRequest struct {
	RepoID    int64
	Actor     Actor
	Name      string
	Surface   branchroom_model.Surface
	RequestID string
}

// CreateRepositoryScope creates a scope and its creator's explicit discovery grant.
// It verifies actual repository ownership without treating site-administrator status as a bypass.
func CreateRepositoryScope(ctx context.Context, request CreateRepositoryScopeRequest) (*branchroom_model.Scope, error) {
	if !setting.Branchroom.Enabled {
		return nil, util.NewNotExistErrorf("Branchroom scope does not exist")
	}
	return createRepositoryScope(ctx, request, branchroom_model.ScopeKindRepository)
}

// createRepositoryEnvironmentScope creates an internal encrypted-environment scope.
// It intentionally has no web, API, Git, or runner entry point in the development slice.
func createRepositoryEnvironmentScope(ctx context.Context, request CreateRepositoryScopeRequest) (*branchroom_model.Scope, error) {
	if !setting.Branchroom.Enabled || !setting.BranchroomEnvironment.Enabled {
		return nil, util.NewNotExistErrorf("Branchroom environment scope does not exist")
	}
	if request.Surface != branchroom_model.SurfaceSystem {
		return nil, util.NewInvalidArgumentErrorf("Branchroom environment scopes require an internal surface")
	}
	return createRepositoryScope(ctx, request, branchroom_model.ScopeKindEnvironment)
}

func createRepositoryScope(ctx context.Context, request CreateRepositoryScopeRequest, kind branchroom_model.ScopeKind) (*branchroom_model.Scope, error) {
	if request.RepoID <= 0 || request.Actor.Type != branchroom_model.ActorTypeUser || request.Actor.ID <= 0 || !branchroom_model.IsSurfaceValid(request.Surface) {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom scope creation request")
	}

	return db.WithTx2(ctx, func(txCtx context.Context) (*branchroom_model.Scope, error) {
		if err := lockRepositoryLifecycle(txCtx, request.RepoID); err != nil {
			return nil, err
		}
		owner, err := CanManageRepositoryScopes(txCtx, request.RepoID, request.Actor)
		if err != nil {
			return nil, err
		}
		if !owner {
			return nil, util.NewNotExistErrorf("Branchroom scope does not exist")
		}

		scope := &branchroom_model.Scope{
			Kind:        kind,
			RepoID:      request.RepoID,
			Name:        request.Name,
			State:       branchroom_model.ScopeStateActive,
			OwnerType:   branchroom_model.ScopeOwnerTypeUser,
			OwnerID:     request.Actor.ID,
			CreatedByID: request.Actor.ID,
		}
		if err := branchroom_model.InsertScope(txCtx, scope); err != nil {
			return nil, err
		}
		if err := branchroom_model.InsertGrant(txCtx, &branchroom_model.Grant{
			ScopeID:     scope.ID,
			SubjectType: branchroom_model.SubjectTypeUser,
			SubjectID:   request.Actor.ID,
			Action:      branchroom_model.ActionDiscover,
			CreatedByID: request.Actor.ID,
		}); err != nil {
			return nil, err
		}
		if kind == branchroom_model.ScopeKindEnvironment {
			if err := branchroom_model.InsertGrant(txCtx, &branchroom_model.Grant{
				ScopeID:     scope.ID,
				SubjectType: branchroom_model.SubjectTypeUser,
				SubjectID:   request.Actor.ID,
				Action:      branchroom_model.ActionMaterialize,
				CreatedByID: request.Actor.ID,
			}); err != nil {
				return nil, err
			}
		}

		now := timeutil.TimeStampNow()
		_, err = branchroom_model.RecordDecisionAndAudit(txCtx, scope, &branchroom_model.Decision{
			ScopeID:         scope.ID,
			RepoID:          scope.RepoID,
			ActorType:       request.Actor.Type,
			ActorID:         request.Actor.ID,
			Action:          branchroom_model.ActionManagePolicy,
			ResourceType:    branchroom_model.ResourceTypeScope,
			ResourceID:      "scope/" + strconv.FormatInt(scope.ID, 10),
			Surface:         request.Surface,
			Result:          branchroom_model.DecisionResultAllow,
			ReasonCode:      "scope_created",
			OrdinaryAllowed: true,
			RequestID:       request.RequestID,
			ExpiresUnix:     now,
			CreatedUnix:     now,
		})
		if err != nil {
			return nil, err
		}
		return scope, nil
	})
}

// lockRepositoryLifecycle serializes scope and workspace changes with repository
// transfer and deletion, which also write the repository row in their transaction.
func lockRepositoryLifecycle(ctx context.Context, repoID int64) error {
	if repoID <= 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom repository ID")
	}
	_, err := db.GetEngine(ctx).ID(repoID).Cols("updated_unix").Update(&repo_model.Repository{UpdatedUnix: timeutil.TimeStampNow()})
	return err
}

// CanManageRepositoryScopes returns whether actor has real repository-owner authority.
// It deliberately ignores site-administrator status as a Branchroom policy bypass.
func CanManageRepositoryScopes(ctx context.Context, repoID int64, actor Actor) (bool, error) {
	if repoID <= 0 || actor.Type != branchroom_model.ActorTypeUser || actor.ID <= 0 {
		return false, nil
	}
	return isActualRepositoryOwner(ctx, repoID, actor.ID)
}

func isActualRepositoryOwner(ctx context.Context, repoID, userID int64) (bool, error) {
	repository, err := repo_model.GetRepositoryByID(ctx, repoID)
	if err != nil {
		return false, err
	}
	if repository.OwnerID == userID {
		return true, nil
	}
	if err := repository.LoadOwner(ctx); err != nil {
		return false, err
	}
	if !repository.Owner.IsOrganization() {
		return false, nil
	}
	teams, err := organization_model.GetUserRepoTeams(ctx, repository.OwnerID, userID, repository.ID)
	if err != nil {
		return false, err
	}
	for _, team := range teams {
		if team.HasAdminAccess() {
			return true, nil
		}
	}
	return false, nil
}
