// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"errors"
	"strings"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
)

var (
	// ErrDiscoveryGrantAlreadyExists means the recipient already has an active direct discovery grant.
	ErrDiscoveryGrantAlreadyExists = errors.New("Branchroom discovery grant already exists")
	// ErrInvalidDiscoveryGrantTarget means a subject cannot authenticate as a direct Branchroom user.
	ErrInvalidDiscoveryGrantTarget = errors.New("invalid Branchroom discovery grant target")
)

// CreateRepositoryDiscoveryGrantRequest contains an owner-authorized direct-user discovery grant.
// It intentionally cannot create source, write, team, deploy-key, or Actions-task grants.
type CreateRepositoryDiscoveryGrantRequest struct {
	RepoID          int64
	ScopeID         int64
	Actor           Actor
	SubjectUsername string
	Surface         branchroom_model.Surface
	RequestID       string
}

// CreateRepositoryDiscoveryGrant grants discovery to one active direct user for an active scope.
func CreateRepositoryDiscoveryGrant(ctx context.Context, request CreateRepositoryDiscoveryGrantRequest) (*branchroom_model.Grant, error) {
	if !setting.Branchroom.Enabled {
		return nil, util.NewNotExistErrorf("Branchroom scope does not exist")
	}
	request.SubjectUsername = strings.TrimSpace(request.SubjectUsername)
	if request.RepoID <= 0 || request.ScopeID <= 0 || request.SubjectUsername == "" || len(request.SubjectUsername) > 255 || request.Actor.Type != branchroom_model.ActorTypeUser || request.Actor.ID <= 0 || !branchroom_model.IsSurfaceValid(request.Surface) {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom discovery grant request")
	}

	return db.WithTx2(ctx, func(txCtx context.Context) (*branchroom_model.Grant, error) {
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

		scope, err := branchroom_model.GetScopeByIDForUpdate(txCtx, request.ScopeID)
		if err != nil {
			return nil, err
		}
		if scope.RepoID != request.RepoID || scope.Kind != branchroom_model.ScopeKindRepository || scope.State != branchroom_model.ScopeStateActive {
			return nil, util.NewNotExistErrorf("Branchroom scope does not exist")
		}

		subject, err := user_model.GetIndividualUserByName(txCtx, request.SubjectUsername)
		if err != nil {
			if errors.Is(err, util.ErrNotExist) {
				return nil, ErrInvalidDiscoveryGrantTarget
			}
			return nil, err
		}
		if !subject.IsActive || subject.ProhibitLogin {
			return nil, ErrInvalidDiscoveryGrantTarget
		}

		now := timeutil.TimeStampNow()
		grants, err := branchroom_model.FindActiveGrantsAt(txCtx, scope.ID, branchroom_model.ActionDiscover, now)
		if err != nil {
			return nil, err
		}
		for _, grant := range grants {
			if grant.SubjectType == branchroom_model.SubjectTypeUser && grant.SubjectID == subject.ID {
				return nil, ErrDiscoveryGrantAlreadyExists
			}
		}

		grant := &branchroom_model.Grant{
			ScopeID:     scope.ID,
			SubjectType: branchroom_model.SubjectTypeUser,
			SubjectID:   subject.ID,
			Action:      branchroom_model.ActionDiscover,
			CreatedByID: request.Actor.ID,
		}
		if err := branchroom_model.InsertGrant(txCtx, grant); err != nil {
			return nil, err
		}
		if err := recordDiscoveryGrantChange(txCtx, scope, grant.ID, request.Actor, request.Surface, request.RequestID, "discovery_grant_created"); err != nil {
			return nil, err
		}
		return grant, nil
	})
}

// RevokeRepositoryDiscoveryGrantRequest identifies one direct-user discovery grant to revoke.
type RevokeRepositoryDiscoveryGrantRequest struct {
	RepoID    int64
	ScopeID   int64
	GrantID   int64
	Actor     Actor
	Surface   branchroom_model.Surface
	RequestID string
}

// RevokeRepositoryDiscoveryGrant revokes a direct-user discovery grant while retaining its audit history.
func RevokeRepositoryDiscoveryGrant(ctx context.Context, request RevokeRepositoryDiscoveryGrantRequest) error {
	if !setting.Branchroom.Enabled {
		return util.NewNotExistErrorf("Branchroom scope does not exist")
	}
	if request.RepoID <= 0 || request.ScopeID <= 0 || request.GrantID <= 0 || request.Actor.Type != branchroom_model.ActorTypeUser || request.Actor.ID <= 0 || !branchroom_model.IsSurfaceValid(request.Surface) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom discovery grant revocation request")
	}

	return db.WithTx(ctx, func(txCtx context.Context) error {
		if err := lockRepositoryLifecycle(txCtx, request.RepoID); err != nil {
			return err
		}
		owner, err := CanManageRepositoryScopes(txCtx, request.RepoID, request.Actor)
		if err != nil {
			return err
		}
		if !owner {
			return util.NewNotExistErrorf("Branchroom scope does not exist")
		}

		scope, err := branchroom_model.GetScopeByIDForUpdate(txCtx, request.ScopeID)
		if err != nil {
			return err
		}
		grant, err := branchroom_model.GetGrantByIDForUpdate(txCtx, request.GrantID)
		if err != nil {
			return err
		}
		if scope.RepoID != request.RepoID || scope.Kind != branchroom_model.ScopeKindRepository || scope.State == branchroom_model.ScopeStateRetired || grant.ScopeID != scope.ID || grant.SubjectType != branchroom_model.SubjectTypeUser || grant.Action != branchroom_model.ActionDiscover || grant.RevokedUnix != 0 {
			return util.NewNotExistErrorf("active Branchroom discovery grant does not exist")
		}

		now := timeutil.TimeStampNow()
		if err := branchroom_model.RevokeGrant(txCtx, grant.ID, request.Actor.ID, now); err != nil {
			return err
		}
		return recordDiscoveryGrantChange(txCtx, scope, grant.ID, request.Actor, request.Surface, request.RequestID, "discovery_grant_revoked")
	})
}

func recordDiscoveryGrantChange(ctx context.Context, scope *branchroom_model.Scope, grantID int64, actor Actor, surface branchroom_model.Surface, requestID, reasonCode string) error {
	now := timeutil.TimeStampNow()
	_, err := branchroom_model.RecordDecisionAndAudit(ctx, scope, &branchroom_model.Decision{
		ScopeID:         scope.ID,
		RepoID:          scope.RepoID,
		GrantID:         grantID,
		ActorType:       actor.Type,
		ActorID:         actor.ID,
		Action:          branchroom_model.ActionManagePolicy,
		ResourceType:    branchroom_model.ResourceTypeScope,
		ResourceID:      scopeResourceID(scope.ID),
		Surface:         surface,
		Result:          branchroom_model.DecisionResultAllow,
		ReasonCode:      reasonCode,
		OrdinaryAllowed: true,
		RequestID:       requestID,
		ExpiresUnix:     now,
		CreatedUnix:     now,
	})
	return err
}
