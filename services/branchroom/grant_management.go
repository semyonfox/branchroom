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
	// ErrSourceReadGrantAlreadyExists means the recipient already has an active direct source-read grant.
	ErrSourceReadGrantAlreadyExists = errors.New("Branchroom source-read grant already exists")
	// ErrInvalidSourceReadGrantTarget means a subject cannot authenticate as a direct Branchroom user.
	ErrInvalidSourceReadGrantTarget = errors.New("invalid Branchroom source-read grant target")
	// ErrSourceWriteGrantAlreadyExists means the recipient already has an active direct source-write grant.
	ErrSourceWriteGrantAlreadyExists = errors.New("Branchroom source-write grant already exists")
	// ErrInvalidSourceWriteGrantTarget means a subject cannot authenticate as a direct Branchroom user.
	ErrInvalidSourceWriteGrantTarget = errors.New("invalid Branchroom source-write grant target")
)

// CreateRepositoryDiscoveryGrantRequest contains an owner-authorized direct-user discovery grant.
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
	return createRepositoryDirectUserGrant(ctx, createRepositoryDirectUserGrantRequest{
		RepoID:           request.RepoID,
		ScopeID:          request.ScopeID,
		Actor:            request.Actor,
		SubjectUsername:  request.SubjectUsername,
		Surface:          request.Surface,
		RequestID:        request.RequestID,
		Action:           branchroom_model.ActionDiscover,
		InvalidTargetErr: ErrInvalidDiscoveryGrantTarget,
		AlreadyExistsErr: ErrDiscoveryGrantAlreadyExists,
		AuditReasonCode:  reasonDiscoveryGrantCreated,
	})
}

// CreateRepositorySourceReadGrantRequest contains an owner-authorized direct-user source-read grant.
// It is one required check for a verified ready workspace's narrow read-only Git HTTP path.
type CreateRepositorySourceReadGrantRequest struct {
	RepoID          int64
	ScopeID         int64
	Actor           Actor
	SubjectUsername string
	Surface         branchroom_model.Surface
	RequestID       string
}

// CreateRepositorySourceReadGrant grants source read to one active direct user for a ready isolated workspace.
func CreateRepositorySourceReadGrant(ctx context.Context, request CreateRepositorySourceReadGrantRequest) (*branchroom_model.Grant, error) {
	return createRepositorySourceGrant(ctx, createRepositorySourceGrantRequest{
		RepoID:                request.RepoID,
		ScopeID:               request.ScopeID,
		Actor:                 request.Actor,
		SubjectUsername:       request.SubjectUsername,
		Surface:               request.Surface,
		RequestID:             request.RequestID,
		Action:                branchroom_model.ActionReadSource,
		RequestInvalidMessage: "invalid Branchroom source-read grant request",
		ErrInvalidTarget:      ErrInvalidSourceReadGrantTarget,
		ErrGrantAlreadyExists: ErrSourceReadGrantAlreadyExists,
		AuditReasonCode:       "source_read_grant_created",
	})
}

// CreateRepositorySourceWriteGrantRequest contains an owner-authorized direct-user source-write grant.
// It is stored policy groundwork for a future receive boundary; no source-write path exists today.
type CreateRepositorySourceWriteGrantRequest struct {
	RepoID          int64
	ScopeID         int64
	Actor           Actor
	SubjectUsername string
	Surface         branchroom_model.Surface
	RequestID       string
}

// CreateRepositorySourceWriteGrant records a direct-user source-write policy grant for a ready isolated workspace.
// Stored groundwork only: Branchroom still denies every source write until a future receive boundary exists.
func CreateRepositorySourceWriteGrant(ctx context.Context, request CreateRepositorySourceWriteGrantRequest) (*branchroom_model.Grant, error) {
	return createRepositorySourceGrant(ctx, createRepositorySourceGrantRequest{
		RepoID:                request.RepoID,
		ScopeID:               request.ScopeID,
		Actor:                 request.Actor,
		SubjectUsername:       request.SubjectUsername,
		Surface:               request.Surface,
		RequestID:             request.RequestID,
		Action:                branchroom_model.ActionWriteSource,
		RequestInvalidMessage: "invalid Branchroom source-write grant request",
		ErrInvalidTarget:      ErrInvalidSourceWriteGrantTarget,
		ErrGrantAlreadyExists: ErrSourceWriteGrantAlreadyExists,
		AuditReasonCode:       "source_write_grant_created",
	})
}

type createRepositorySourceGrantRequest struct {
	RepoID                int64
	ScopeID               int64
	Actor                 Actor
	SubjectUsername       string
	Surface               branchroom_model.Surface
	RequestID             string
	Action                branchroom_model.Action
	RequestInvalidMessage string
	ErrInvalidTarget      error
	ErrGrantAlreadyExists error
	AuditReasonCode       string
}

func createRepositorySourceGrant(ctx context.Context, request createRepositorySourceGrantRequest) (*branchroom_model.Grant, error) {
	if !setting.Branchroom.Enabled {
		return nil, util.NewNotExistErrorf("Branchroom scope does not exist")
	}
	request.SubjectUsername = strings.TrimSpace(request.SubjectUsername)
	if request.RepoID <= 0 || request.ScopeID <= 0 || request.SubjectUsername == "" || len(request.SubjectUsername) > 255 || request.Actor.Type != branchroom_model.ActorTypeUser || request.Actor.ID <= 0 || !branchroom_model.IsSurfaceValid(request.Surface) {
		return nil, util.NewInvalidArgumentErrorf("%s", request.RequestInvalidMessage)
	}
	return createRepositoryDirectUserGrant(ctx, createRepositoryDirectUserGrantRequest{
		RepoID:                request.RepoID,
		ScopeID:               request.ScopeID,
		Actor:                 request.Actor,
		SubjectUsername:       request.SubjectUsername,
		Surface:               request.Surface,
		RequestID:             request.RequestID,
		Action:                request.Action,
		RequireReadyWorkspace: true,
		InvalidTargetErr:      request.ErrInvalidTarget,
		AlreadyExistsErr:      request.ErrGrantAlreadyExists,
		AuditReasonCode:       request.AuditReasonCode,
	})
}

type createRepositoryDirectUserGrantRequest struct {
	RepoID                int64
	ScopeID               int64
	Actor                 Actor
	SubjectUsername       string
	Surface               branchroom_model.Surface
	RequestID             string
	Action                branchroom_model.Action
	RequireReadyWorkspace bool
	InvalidTargetErr      error
	AlreadyExistsErr      error
	AuditReasonCode       string
}

func createRepositoryDirectUserGrant(ctx context.Context, request createRepositoryDirectUserGrantRequest) (*branchroom_model.Grant, error) {
	return db.WithTx2(ctx, func(txCtx context.Context) (*branchroom_model.Grant, error) {
		scope, err := requireActiveScopeTx(txCtx, request.RepoID, request.Actor, request.ScopeID, branchroom_model.ScopeKindRepository, "Branchroom scope does not exist")
		if err != nil {
			return nil, err
		}
		if request.RequireReadyWorkspace {
			workspace, err := branchroom_model.GetWorkspaceByScopeIDForUpdate(txCtx, scope.ID)
			if err != nil {
				return nil, err
			}
			if workspace.RepoID != request.RepoID || workspace.State != branchroom_model.WorkspaceStateReady || workspace.Validate() != nil {
				return nil, util.NewNotExistErrorf("Branchroom scope does not exist")
			}
		}

		subject, err := user_model.GetIndividualUserByName(txCtx, request.SubjectUsername)
		if err != nil {
			if errors.Is(err, util.ErrNotExist) {
				return nil, request.InvalidTargetErr
			}
			return nil, err
		}
		if !subject.IsActive || subject.ProhibitLogin {
			return nil, request.InvalidTargetErr
		}

		now := timeutil.TimeStampNow()
		grants, err := branchroom_model.FindActiveGrantsAt(txCtx, scope.ID, request.Action, now)
		if err != nil {
			return nil, err
		}
		for _, grant := range grants {
			if grant.SubjectType == branchroom_model.SubjectTypeUser && grant.SubjectID == subject.ID {
				return nil, request.AlreadyExistsErr
			}
		}

		grant := &branchroom_model.Grant{
			ScopeID:     scope.ID,
			SubjectType: branchroom_model.SubjectTypeUser,
			SubjectID:   subject.ID,
			Action:      request.Action,
			CreatedByID: request.Actor.ID,
		}
		if err := branchroom_model.InsertGrant(txCtx, grant); err != nil {
			return nil, err
		}
		if err := recordDirectUserGrantChange(txCtx, scope, grant.ID, request.Actor, request.Surface, request.RequestID, request.AuditReasonCode); err != nil {
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

	return revokeRepositoryDirectUserGrant(ctx, revokeRepositoryDirectUserGrantRequest{
		RepoID:          request.RepoID,
		ScopeID:         request.ScopeID,
		GrantID:         request.GrantID,
		Actor:           request.Actor,
		Surface:         request.Surface,
		RequestID:       request.RequestID,
		Action:          branchroom_model.ActionDiscover,
		NotExistMessage: "active Branchroom discovery grant does not exist",
		AuditReasonCode: "discovery_grant_revoked",
	})
}

// RevokeRepositorySourceReadGrantRequest identifies one direct-user source-read grant to revoke.
type RevokeRepositorySourceReadGrantRequest struct {
	RepoID    int64
	ScopeID   int64
	GrantID   int64
	Actor     Actor
	Surface   branchroom_model.Surface
	RequestID string
}

// RevokeRepositorySourceReadGrant revokes a direct-user source-read grant while retaining its audit history.
func RevokeRepositorySourceReadGrant(ctx context.Context, request RevokeRepositorySourceReadGrantRequest) error {
	if !setting.Branchroom.Enabled {
		return util.NewNotExistErrorf("Branchroom scope does not exist")
	}
	if request.RepoID <= 0 || request.ScopeID <= 0 || request.GrantID <= 0 || request.Actor.Type != branchroom_model.ActorTypeUser || request.Actor.ID <= 0 || !branchroom_model.IsSurfaceValid(request.Surface) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom source-read grant revocation request")
	}

	return revokeRepositoryDirectUserGrant(ctx, revokeRepositoryDirectUserGrantRequest{
		RepoID:          request.RepoID,
		ScopeID:         request.ScopeID,
		GrantID:         request.GrantID,
		Actor:           request.Actor,
		Surface:         request.Surface,
		RequestID:       request.RequestID,
		Action:          branchroom_model.ActionReadSource,
		NotExistMessage: "active Branchroom source-read grant does not exist",
		AuditReasonCode: "source_read_grant_revoked",
	})
}

// RevokeRepositorySourceWriteGrantRequest identifies one direct-user source-write grant to revoke.
type RevokeRepositorySourceWriteGrantRequest struct {
	RepoID    int64
	ScopeID   int64
	GrantID   int64
	Actor     Actor
	Surface   branchroom_model.Surface
	RequestID string
}

// RevokeRepositorySourceWriteGrant revokes a direct-user source-write grant while retaining its audit history.
func RevokeRepositorySourceWriteGrant(ctx context.Context, request RevokeRepositorySourceWriteGrantRequest) error {
	if !setting.Branchroom.Enabled {
		return util.NewNotExistErrorf("Branchroom scope does not exist")
	}
	if request.RepoID <= 0 || request.ScopeID <= 0 || request.GrantID <= 0 || request.Actor.Type != branchroom_model.ActorTypeUser || request.Actor.ID <= 0 || !branchroom_model.IsSurfaceValid(request.Surface) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom source-write grant revocation request")
	}

	return revokeRepositoryDirectUserGrant(ctx, revokeRepositoryDirectUserGrantRequest{
		RepoID:          request.RepoID,
		ScopeID:         request.ScopeID,
		GrantID:         request.GrantID,
		Actor:           request.Actor,
		Surface:         request.Surface,
		RequestID:       request.RequestID,
		Action:          branchroom_model.ActionWriteSource,
		NotExistMessage: "active Branchroom source-write grant does not exist",
		AuditReasonCode: "source_write_grant_revoked",
	})
}

type revokeRepositoryDirectUserGrantRequest struct {
	RepoID          int64
	ScopeID         int64
	GrantID         int64
	Actor           Actor
	Surface         branchroom_model.Surface
	RequestID       string
	Action          branchroom_model.Action
	NotExistMessage string
	AuditReasonCode string
}

func revokeRepositoryDirectUserGrant(ctx context.Context, request revokeRepositoryDirectUserGrantRequest) error {
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
		if scope.RepoID != request.RepoID || scope.Kind != branchroom_model.ScopeKindRepository || scope.State == branchroom_model.ScopeStateRetired || grant.ScopeID != scope.ID || grant.SubjectType != branchroom_model.SubjectTypeUser || grant.Action != request.Action || grant.RevokedUnix != 0 {
			return util.NewNotExistErrorf("%s", request.NotExistMessage)
		}

		now := timeutil.TimeStampNow()
		if err := branchroom_model.RevokeGrant(txCtx, grant.ID, request.Actor.ID, now); err != nil {
			return err
		}
		return recordDirectUserGrantChange(txCtx, scope, grant.ID, request.Actor, request.Surface, request.RequestID, request.AuditReasonCode)
	})
}

func recordDirectUserGrantChange(ctx context.Context, scope *branchroom_model.Scope, grantID int64, actor Actor, surface branchroom_model.Surface, requestID, reasonCode string) error {
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
