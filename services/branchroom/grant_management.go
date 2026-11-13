// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"errors"
	"strings"
	"time"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	organization_model "gitea.dev/models/organization"
	repo_model "gitea.dev/models/repo"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/json"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
)

// grantTimeWindowClockSkew tolerates modest clock drift when validating explicit grant windows.
const grantTimeWindowClockSkew = time.Minute

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
	// ErrGrantDurationExceeded means an expiry exceeds the instance maximum.
	ErrGrantDurationExceeded = errors.New("Branchroom grant exceeds the maximum duration")
	// ErrTeamGrantAlreadyExists means the team already holds an unexpired grant of that action.
	ErrTeamGrantAlreadyExists = errors.New("Branchroom team grant already exists")
	// ErrInvalidTeamGrantTarget means the team cannot receive a Branchroom team grant for this repository.
	ErrInvalidTeamGrantTarget = errors.New("invalid Branchroom team grant target")
)

// CreateRepositoryDiscoveryGrantRequest contains an owner-authorized direct-user discovery grant.
type CreateRepositoryDiscoveryGrantRequest struct {
	RepoID          int64
	ScopeID         int64
	Actor           Actor
	SubjectUsername string
	NotBeforeUnix   timeutil.TimeStamp
	ExpiresUnix     timeutil.TimeStamp
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
	if request.ExpiresUnix == 0 {
		request.ExpiresUnix = timeutil.TimeStampNow().AddDuration(setting.Branchroom.GrantDuration())
	}
	if err := validateGrantTimeWindow(request.NotBeforeUnix, request.ExpiresUnix); err != nil {
		return nil, err
	}
	return createRepositoryDirectUserGrant(ctx, createRepositoryDirectUserGrantRequest{
		RepoID:           request.RepoID,
		ScopeID:          request.ScopeID,
		Actor:            request.Actor,
		SubjectUsername:  request.SubjectUsername,
		NotBeforeUnix:    request.NotBeforeUnix,
		ExpiresUnix:      request.ExpiresUnix,
		Surface:          request.Surface,
		RequestID:        request.RequestID,
		Action:           branchroom_model.ActionDiscover,
		InvalidTargetErr: ErrInvalidDiscoveryGrantTarget,
		AlreadyExistsErr: ErrDiscoveryGrantAlreadyExists,
		AuditReasonCode:  reasonDiscoveryGrantCreated,
	})
}

// CreateRepositorySourceReadGrantRequest contains an owner-authorized direct-user source-read grant.
// It is one required check for a verified ready workspace's restricted Git HTTP transport.
type CreateRepositorySourceReadGrantRequest struct {
	RepoID          int64
	ScopeID         int64
	Actor           Actor
	SubjectUsername string
	NotBeforeUnix   timeutil.TimeStamp
	ExpiresUnix     timeutil.TimeStamp
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
		NotBeforeUnix:         request.NotBeforeUnix,
		ExpiresUnix:           request.ExpiresUnix,
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
// The restricted HTTPS receive boundary also requires active source-read and ordinary code-write access.
type CreateRepositorySourceWriteGrantRequest struct {
	RepoID          int64
	ScopeID         int64
	Actor           Actor
	SubjectUsername string
	NotBeforeUnix   timeutil.TimeStamp
	ExpiresUnix     timeutil.TimeStamp
	Surface         branchroom_model.Surface
	RequestID       string
}

// CreateRepositorySourceWriteGrant records a direct-user source-write grant for a ready isolated workspace.
// It is consumed only by the restricted HTTPS receive boundary.
func CreateRepositorySourceWriteGrant(ctx context.Context, request CreateRepositorySourceWriteGrantRequest) (*branchroom_model.Grant, error) {
	return createRepositorySourceGrant(ctx, createRepositorySourceGrantRequest{
		RepoID:                request.RepoID,
		ScopeID:               request.ScopeID,
		Actor:                 request.Actor,
		SubjectUsername:       request.SubjectUsername,
		NotBeforeUnix:         request.NotBeforeUnix,
		ExpiresUnix:           request.ExpiresUnix,
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
	NotBeforeUnix         timeutil.TimeStamp
	ExpiresUnix           timeutil.TimeStamp
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
	if request.ExpiresUnix == 0 {
		request.ExpiresUnix = timeutil.TimeStampNow().AddDuration(setting.Branchroom.GrantDuration())
	}
	if err := validateGrantTimeWindow(request.NotBeforeUnix, request.ExpiresUnix); err != nil {
		return nil, err
	}
	return createRepositoryDirectUserGrant(ctx, createRepositoryDirectUserGrantRequest{
		RepoID:                request.RepoID,
		ScopeID:               request.ScopeID,
		Actor:                 request.Actor,
		SubjectUsername:       request.SubjectUsername,
		NotBeforeUnix:         request.NotBeforeUnix,
		ExpiresUnix:           request.ExpiresUnix,
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
	NotBeforeUnix         timeutil.TimeStamp
	ExpiresUnix           timeutil.TimeStamp
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
		scope, err := requireActiveScopeTx(txCtx, request.RepoID, request.Actor, request.ScopeID, "Branchroom scope does not exist")
		if err != nil {
			return nil, err
		}
		if request.RequireReadyWorkspace {
			if err := requireReadyWorkspaceTx(txCtx, scope); err != nil {
				return nil, err
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

		exists, err := activeGrantExists(txCtx, scope.ID, request.Action, branchroom_model.SubjectTypeUser, subject.ID)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, request.AlreadyExistsErr
		}

		grant := &branchroom_model.Grant{
			ScopeID:       scope.ID,
			SubjectType:   branchroom_model.SubjectTypeUser,
			SubjectID:     subject.ID,
			Action:        request.Action,
			NotBeforeUnix: request.NotBeforeUnix,
			ExpiresUnix:   request.ExpiresUnix,
			CreatedByID:   request.Actor.ID,
		}
		if err := branchroom_model.InsertGrant(txCtx, grant); err != nil {
			return nil, err
		}
		if err := recordRepositoryGrantChange(txCtx, scope, grant.ID, request.Actor, request.Surface, request.RequestID, request.AuditReasonCode); err != nil {
			return nil, err
		}
		return grant, nil
	})
}

func validateGrantTimeWindow(notBefore, expires timeutil.TimeStamp) error {
	if notBefore < 0 || expires < 0 || expires != 0 && notBefore != 0 && expires <= notBefore {
		return util.NewInvalidArgumentErrorf("invalid Branchroom grant time window")
	}
	now := timeutil.TimeStampNow()
	if setting.Branchroom.MaxGrantDuration > 0 && (expires == 0 || expires > now.AddDuration(setting.Branchroom.MaxGrantDuration)) {
		return ErrGrantDurationExceeded
	}
	if notBefore != 0 && notBefore < now.AddDuration(-grantTimeWindowClockSkew) || expires != 0 && expires < now.AddDuration(-grantTimeWindowClockSkew) {
		return util.NewInvalidArgumentErrorf("Branchroom grant time window already passed")
	}
	return nil
}

func isTeamGrantAction(action branchroom_model.Action) bool {
	return action == branchroom_model.ActionDiscover || action == branchroom_model.ActionReadSource
}

func requireReadyWorkspaceTx(ctx context.Context, scope *branchroom_model.Scope) error {
	workspace, err := branchroom_model.GetWorkspaceByScopeIDForUpdate(ctx, scope.ID)
	if err != nil {
		return err
	}
	if workspace.RepoID != scope.RepoID || workspace.State != branchroom_model.WorkspaceStateReady || workspace.Validate() != nil {
		return util.NewNotExistErrorf("Branchroom scope does not exist")
	}
	return nil
}

func activeGrantExists(ctx context.Context, scopeID int64, action branchroom_model.Action, subjectType branchroom_model.SubjectType, subjectID int64) (bool, error) {
	return branchroom_model.HasUnexpiredSubjectGrant(ctx, scopeID, action, subjectType, subjectID, timeutil.TimeStampNow())
}

// CreateRepositoryTeamGrantRequest contains an owner-authorized organization-team grant.
type CreateRepositoryTeamGrantRequest struct {
	RepoID        int64
	ScopeID       int64
	Actor         Actor
	TeamID        int64
	Action        branchroom_model.Action
	NotBeforeUnix timeutil.TimeStamp
	ExpiresUnix   timeutil.TimeStamp
	Surface       branchroom_model.Surface
	RequestID     string
}

// CreateRepositoryTeamGrant grants discover or source read to one organization team of an org-owned repository.
func CreateRepositoryTeamGrant(ctx context.Context, request CreateRepositoryTeamGrantRequest) (*branchroom_model.Grant, error) {
	if !setting.Branchroom.Enabled {
		return nil, util.NewNotExistErrorf("Branchroom scope does not exist")
	}
	if request.RepoID <= 0 || request.ScopeID <= 0 || request.TeamID <= 0 || request.Actor.Type != branchroom_model.ActorTypeUser || request.Actor.ID <= 0 || !branchroom_model.IsSurfaceValid(request.Surface) || !isTeamGrantAction(request.Action) {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom team grant request")
	}
	if request.ExpiresUnix == 0 {
		request.ExpiresUnix = timeutil.TimeStampNow().AddDuration(setting.Branchroom.GrantDuration())
	}
	if err := validateGrantTimeWindow(request.NotBeforeUnix, request.ExpiresUnix); err != nil {
		return nil, err
	}

	return db.WithTx2(ctx, func(txCtx context.Context) (*branchroom_model.Grant, error) {
		scope, err := requireActiveScopeTx(txCtx, request.RepoID, request.Actor, request.ScopeID, "Branchroom scope does not exist")
		if err != nil {
			return nil, err
		}
		repository, err := repo_model.GetRepositoryByID(txCtx, scope.RepoID)
		if err != nil {
			return nil, err
		}
		team, err := organization_model.GetTeamByID(txCtx, request.TeamID)
		if err != nil {
			if errors.Is(err, util.ErrNotExist) {
				return nil, ErrInvalidTeamGrantTarget
			}
			return nil, err
		}
		if repository.OwnerID != team.OrgID {
			return nil, ErrInvalidTeamGrantTarget
		}
		if request.Action == branchroom_model.ActionReadSource {
			if err := requireReadyWorkspaceTx(txCtx, scope); err != nil {
				return nil, err
			}
		}

		exists, err := activeGrantExists(txCtx, scope.ID, request.Action, branchroom_model.SubjectTypeTeam, team.ID)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, ErrTeamGrantAlreadyExists
		}

		grant := &branchroom_model.Grant{
			ScopeID:       scope.ID,
			SubjectType:   branchroom_model.SubjectTypeTeam,
			SubjectID:     team.ID,
			Action:        request.Action,
			NotBeforeUnix: request.NotBeforeUnix,
			ExpiresUnix:   request.ExpiresUnix,
			CreatedByID:   request.Actor.ID,
		}
		if err := branchroom_model.InsertGrant(txCtx, grant); err != nil {
			return nil, err
		}
		if err := recordRepositoryGrantChange(txCtx, scope, grant.ID, request.Actor, request.Surface, request.RequestID, reasonTeamGrantCreated); err != nil {
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
		SubjectType:     branchroom_model.SubjectTypeUser,
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
		SubjectType:     branchroom_model.SubjectTypeUser,
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
		SubjectType:     branchroom_model.SubjectTypeUser,
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
	SubjectType     branchroom_model.SubjectType
	NotExistMessage string
	AuditReasonCode string
	SiteAdmin       bool
}

func revokeRepositoryDirectUserGrant(ctx context.Context, request revokeRepositoryDirectUserGrantRequest) error {
	return db.WithTx(ctx, func(txCtx context.Context) error {
		if err := lockRepositoryLifecycle(txCtx, request.RepoID); err != nil {
			return err
		}
		owner, err := CanManageRepositoryScopes(txCtx, request.RepoID, request.Actor)
		if request.SiteAdmin {
			admin, adminErr := user_model.GetUserByID(txCtx, request.Actor.ID)
			if adminErr != nil {
				return adminErr
			}
			owner = admin.IsAdmin && admin.IsActive && !admin.ProhibitLogin
		}
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
		if scope.RepoID != request.RepoID || scope.Kind != branchroom_model.ScopeKindRepository || scope.State == branchroom_model.ScopeStateRetired || grant.ScopeID != scope.ID || grant.SubjectType != request.SubjectType || grant.Action != request.Action || grant.RevokedUnix != 0 {
			return util.NewNotExistErrorf("%s", request.NotExistMessage)
		}

		now := timeutil.TimeStampNow()
		if err := branchroom_model.RevokeGrant(txCtx, grant.ID, request.Actor.ID, now); err != nil {
			return err
		}
		return recordRepositoryGrantChange(txCtx, scope, grant.ID, request.Actor, request.Surface, request.RequestID, request.AuditReasonCode)
	})
}

// RevokeRepositoryTeamGrantRequest identifies one organization-team grant to revoke.
type RevokeRepositoryTeamGrantRequest struct {
	RepoID    int64
	ScopeID   int64
	GrantID   int64
	Actor     Actor
	Action    branchroom_model.Action
	Surface   branchroom_model.Surface
	RequestID string
}

// RevokeRepositoryTeamGrant revokes an organization-team grant while retaining its audit history.
func RevokeRepositoryTeamGrant(ctx context.Context, request RevokeRepositoryTeamGrantRequest) error {
	if !setting.Branchroom.Enabled {
		return util.NewNotExistErrorf("Branchroom scope does not exist")
	}
	if request.RepoID <= 0 || request.ScopeID <= 0 || request.GrantID <= 0 || request.Actor.Type != branchroom_model.ActorTypeUser || request.Actor.ID <= 0 || !branchroom_model.IsSurfaceValid(request.Surface) || !isTeamGrantAction(request.Action) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom team grant revocation request")
	}

	return revokeRepositoryDirectUserGrant(ctx, revokeRepositoryDirectUserGrantRequest{
		RepoID:          request.RepoID,
		ScopeID:         request.ScopeID,
		GrantID:         request.GrantID,
		Actor:           request.Actor,
		Surface:         request.Surface,
		RequestID:       request.RequestID,
		Action:          request.Action,
		SubjectType:     branchroom_model.SubjectTypeTeam,
		NotExistMessage: "active Branchroom team grant does not exist",
		AuditReasonCode: reasonTeamGrantRevoked,
	})
}

func recordRepositoryGrantChange(ctx context.Context, scope *branchroom_model.Scope, grantID int64, actor Actor, surface branchroom_model.Surface, requestID, reasonCode string) error {
	grant, err := branchroom_model.GetGrantByID(ctx, grantID)
	if err != nil {
		return err
	}
	details, err := json.Marshal(struct {
		NotBeforeUnix timeutil.TimeStamp `json:"not_before_unix"`
		ExpiresUnix   timeutil.TimeStamp `json:"expires_unix"`
	}{grant.NotBeforeUnix, grant.ExpiresUnix})
	if err != nil {
		return err
	}
	now := timeutil.TimeStampNow()
	_, err = branchroom_model.RecordDecisionAndAudit(ctx, scope, &branchroom_model.Decision{
		Details:         string(details),
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

// RenewRepositoryGrantRequest identifies an owner-authorised grant renewal.
type RenewRepositoryGrantRequest struct {
	RepoID    int64
	ScopeID   int64
	GrantID   int64
	Actor     Actor
	Surface   branchroom_model.Surface
	RequestID string
}

// RenewRepositoryGrant renews an active grant under the same owner authority as creation.
func RenewRepositoryGrant(ctx context.Context, request RenewRepositoryGrantRequest) error {
	if !setting.Branchroom.Enabled || request.Actor.Type != branchroom_model.ActorTypeUser || !branchroom_model.IsSurfaceValid(request.Surface) {
		return util.NewNotExistErrorf("Branchroom grant does not exist")
	}
	return db.WithTx(ctx, func(txCtx context.Context) error {
		scope, err := requireActiveScopeTx(txCtx, request.RepoID, request.Actor, request.ScopeID, "Branchroom scope does not exist")
		if err != nil {
			return err
		}
		grant, err := branchroom_model.GetGrantByIDForUpdate(txCtx, request.GrantID)
		if err != nil {
			return err
		}
		now := timeutil.TimeStampNow()
		if grant.ScopeID != scope.ID || !grant.IsActiveAt(now) || (grant.SubjectType != branchroom_model.SubjectTypeUser && grant.SubjectType != branchroom_model.SubjectTypeTeam) {
			return util.NewNotExistErrorf("active Branchroom grant does not exist")
		}
		grant.ExpiresUnix = now.AddDuration(setting.Branchroom.GrantDuration())
		if _, err := db.GetEngine(txCtx).ID(grant.ID).Cols("expires_unix").Update(grant); err != nil {
			return err
		}
		return recordRepositoryGrantChange(txCtx, scope, grant.ID, request.Actor, request.Surface, request.RequestID, "grant_renewed")
	})
}

// RevokeAllDirectUserGrants lets a site administrator offboard one user.
func RevokeAllDirectUserGrants(ctx context.Context, userID int64, actor Actor) error {
	if !setting.Branchroom.Enabled || actor.Type != branchroom_model.ActorTypeUser {
		return util.NewNotExistErrorf("Branchroom access does not exist")
	}
	admin, err := user_model.GetUserByID(ctx, actor.ID)
	if err != nil {
		return err
	}
	if !admin.IsAdmin || !admin.IsActive || admin.ProhibitLogin {
		return util.NewNotExistErrorf("Branchroom access does not exist")
	}
	return db.WithTx(ctx, func(txCtx context.Context) error {
		grants, err := branchroom_model.FindUserAccess(txCtx, userID)
		if err != nil {
			return err
		}
		for _, access := range grants {
			if access.SubjectType != branchroom_model.SubjectTypeUser {
				continue
			}
			reason := "discovery_grant_revoked"
			switch access.Action {
			case branchroom_model.ActionReadSource:
				reason = "source_read_grant_revoked"
			case branchroom_model.ActionWriteSource:
				reason = "source_write_grant_revoked"
			}
			if err := revokeRepositoryDirectUserGrant(txCtx, revokeRepositoryDirectUserGrantRequest{
				RepoID:          access.RepoID,
				ScopeID:         access.ScopeID,
				GrantID:         access.ID,
				Actor:           actor,
				Surface:         branchroom_model.SurfaceWeb,
				Action:          access.Action,
				SubjectType:     branchroom_model.SubjectTypeUser,
				SiteAdmin:       true,
				NotExistMessage: "active Branchroom grant does not exist",
				AuditReasonCode: reason,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}
