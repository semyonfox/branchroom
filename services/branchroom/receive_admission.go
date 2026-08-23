// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/timeutil"

	"github.com/google/uuid"
)

var errReceiveAdmissionDenied = errors.New("Branchroom receive admission denied")

const receiveAdmissionLifetime = time.Minute

const receiveAdmissionBearerLen = 32

type IssueReceiveAdmissionRequest struct {
	RepoID   int64
	RouteKey string
	UserID   int64
}

type ReceiveAdmissionCredential struct {
	SessionKey   string
	BearerSecret []byte
}

func (credential *ReceiveAdmissionCredential) clear() {
	if credential == nil {
		return
	}
	clearBytes(credential.BearerSecret)
	credential.SessionKey = ""
	credential.BearerSecret = nil
}

// IssueReceiveAdmission is the sealed issuer for source-write receive sessions.
// It collapses every failure into errReceiveAdmissionDenied so callers can never
// distinguish missing workspaces from policy denials or storage faults.
func IssueReceiveAdmission(ctx context.Context, request IssueReceiveAdmissionRequest) (*ReceiveAdmissionCredential, error) {
	if !setting.Branchroom.Enabled || request.RepoID <= 0 || request.UserID <= 0 || !branchroom_model.IsWorkspaceRouteKey(request.RouteKey) {
		return nil, errReceiveAdmissionDenied
	}
	if db.InTransaction(ctx) {
		return nil, errReceiveAdmissionDenied
	}

	initial, err := branchroom_model.GetWorkspaceByRouteKeyAndRepoID(ctx, request.RouteKey, request.RepoID)
	if err != nil || initial == nil {
		return nil, errReceiveAdmissionDenied
	}

	credential, err := db.WithTx2(ctx, func(txCtx context.Context) (*ReceiveAdmissionCredential, error) {
		if err := lockRepositoryLifecycleForRead(txCtx, request.RepoID); err != nil {
			return nil, errReceiveAdmissionDenied
		}

		scope, err := branchroom_model.GetScopeByIDForUpdate(txCtx, initial.ScopeID)
		if err != nil {
			return nil, errReceiveAdmissionDenied
		}
		if err := scope.Validate(); err != nil {
			return nil, errReceiveAdmissionDenied
		}
		if scope.ID != initial.ScopeID || scope.RepoID != request.RepoID || scope.Kind != branchroom_model.ScopeKindRepository || scope.State != branchroom_model.ScopeStateActive {
			return nil, errReceiveAdmissionDenied
		}

		workspace, err := branchroom_model.GetWorkspaceByIDForUpdate(txCtx, initial.ID)
		if err != nil {
			return nil, errReceiveAdmissionDenied
		}
		if err := workspace.Validate(); err != nil {
			return nil, errReceiveAdmissionDenied
		}
		if workspace.ID != initial.ID || workspace.RouteKey != initial.RouteKey || workspace.RouteKey != request.RouteKey || workspace.RepoID != request.RepoID || workspace.ScopeID != scope.ID || workspace.State != branchroom_model.WorkspaceStateReady {
			return nil, errReceiveAdmissionDenied
		}

		actor := Actor{Type: branchroom_model.ActorTypeUser, ID: request.UserID}
		ceiling, active, err := resolveDirectUserRepositoryCeiling(txCtx, request.RepoID, actor)
		if err != nil || !active || !ceiling.CodeRead || !ceiling.CodeWrite {
			return nil, errReceiveAdmissionDenied
		}

		now := timeutil.TimeStampNow()
		if _, err := branchroom_model.GetActiveDirectUserGrantAt(txCtx, scope.ID, request.UserID, branchroom_model.ActionWriteSource, now); err != nil {
			return nil, errReceiveAdmissionDenied
		}

		if err := branchroom_module.VerifyRestrictedStoreHook(txCtx, setting.Branchroom.RestrictedRepoRootPath, workspace.StoreID); err != nil {
			return nil, errReceiveAdmissionDenied
		}

		sessionKeyUUID, err := uuid.NewRandom()
		if err != nil {
			return nil, errReceiveAdmissionDenied
		}
		bearerSecret := make([]byte, receiveAdmissionBearerLen)
		if _, err := rand.Read(bearerSecret); err != nil {
			clearBytes(bearerSecret)
			return nil, errReceiveAdmissionDenied
		}

		credential := &ReceiveAdmissionCredential{
			SessionKey:   sessionKeyUUID.String(),
			BearerSecret: bearerSecret,
		}
		bearerHash := sha256.Sum256(bearerSecret)
		session := &branchroom_model.ReceiveSession{
			SessionKey:       credential.SessionKey,
			BearerSecretHash: hex.EncodeToString(bearerHash[:]),
			ScopeID:          scope.ID,
			WorkspaceID:      workspace.ID,
			RepoID:           request.RepoID,
			ActorType:        branchroom_model.ActorTypeUser,
			ActorID:          request.UserID,
			State:            branchroom_model.ReceiveSessionStateIssued,
			Outcome:          branchroom_model.ReceiveSessionOutcomePending,
			IssuedUnix:       now,
			ExpiresUnix:      now.AddDuration(receiveAdmissionLifetime),
		}
		if err := branchroom_model.InsertReceiveSession(txCtx, session); err != nil {
			credential.clear()
			return nil, errReceiveAdmissionDenied
		}
		claimed, err := branchroom_model.ClaimReceiveSession(txCtx, session.ID, now)
		if err != nil || !claimed {
			credential.clear()
			return nil, errReceiveAdmissionDenied
		}
		return credential, nil
	})
	if err != nil || credential == nil {
		credential.clear()
		return nil, errReceiveAdmissionDenied
	}
	return credential, nil
}
