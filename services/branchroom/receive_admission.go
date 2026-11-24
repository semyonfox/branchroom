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
	"gitea.dev/modules/util"

	"github.com/google/uuid"
)

var errReceiveAdmissionDenied = errors.New("Branchroom receive admission denied")

const receiveAdmissionLifetime = time.Minute

const receiveAdmissionBearerEntropyLen = 32

type IssueReceiveAdmissionRequest struct {
	Credential string
	RepoID     int64
	RouteKey   string
	UserID     int64
}

type ReceiveAdmissionCredential struct {
	Report       []byte
	SessionKey   string
	BearerSecret []byte
}

// Clear clears the caller-owned bearer buffer and removes the credential's references
// after the receive-pack child has inherited it. Neither value is persisted or logged.
func (credential *ReceiveAdmissionCredential) Clear() {
	if credential == nil {
		return
	}
	clear(credential.Report)
	credential.Report = nil
	clear(credential.BearerSecret)
	credential.SessionKey = ""
	credential.BearerSecret = nil
}

type sealedSourceWriteAdmission struct {
	actor      Actor
	ceiling    RepositoryCeiling
	writeGrant *branchroom_model.Grant
	now        timeutil.TimeStamp
}

// authorizeSealedSourceWriteAdmission is the only source-write grant check that
// can succeed. Callers hold the repository lifecycle lock and have already bound
// a persisted scope and workspace to the request. It requires source-read access
// because a receive advertisement exposes the restricted ref namespace.
func authorizeSealedSourceWriteAdmission(ctx context.Context, scope *branchroom_model.Scope, workspace *branchroom_model.Workspace, userID int64) (sealedSourceWriteAdmission, bool, error) {
	if scope == nil || workspace == nil || userID <= 0 ||
		scope.Validate() != nil || workspace.Validate() != nil ||
		scope.Kind != branchroom_model.ScopeKindRepository || scope.State != branchroom_model.ScopeStateActive ||
		workspace.ScopeID != scope.ID || workspace.RepoID != scope.RepoID || workspace.State != branchroom_model.WorkspaceStateReady {
		return sealedSourceWriteAdmission{}, false, nil
	}

	actor := Actor{Type: branchroom_model.ActorTypeUser, ID: userID}
	ceiling, active, err := resolveDirectUserRepositoryCeiling(ctx, scope.RepoID, actor)
	if err != nil {
		return sealedSourceWriteAdmission{}, false, err
	}
	if !active || !ceiling.CodeRead {
		return sealedSourceWriteAdmission{}, false, nil
	}

	readDecision, err := NewDBEvaluator().Evaluate(ctx, Request{
		ScopeID:      scope.ID,
		RepoID:       scope.RepoID,
		Actor:        actor,
		Ceiling:      ceiling,
		Action:       branchroom_model.ActionReadSource,
		ResourceType: branchroom_model.ResourceTypeWorkspace,
		ResourceID:   workspaceResourceID(workspace.WorkspaceKey),
		Surface:      branchroom_model.SurfaceGitHTTP,
	})
	if err != nil {
		return sealedSourceWriteAdmission{}, false, err
	}
	if !readDecision.Allowed {
		return sealedSourceWriteAdmission{}, false, nil
	}

	now := timeutil.TimeStampNow()
	writeGrant, err := branchroom_model.GetActiveDirectUserGrantAt(ctx, scope.ID, userID, branchroom_model.ActionWriteSource, now)
	if errors.Is(err, util.ErrNotExist) {
		return sealedSourceWriteAdmission{}, false, nil
	}
	if err != nil || writeGrant.Validate() != nil {
		return sealedSourceWriteAdmission{}, false, err
	}
	return sealedSourceWriteAdmission{actor: actor, ceiling: ceiling, writeGrant: writeGrant, now: now}, true, nil
}

// recordSealedSourceWriteAdmission records the successful sealed receive decision.
// Generic DBEvaluator write requests remain deny-only because they cannot prove this binding.
func recordSealedSourceWriteAdmission(ctx context.Context, scope *branchroom_model.Scope, workspace *branchroom_model.Workspace, admission sealedSourceWriteAdmission) error {
	if admission.writeGrant == nil {
		return errReceiveAdmissionDenied
	}
	_, err := branchroom_model.RecordDecisionAndAudit(ctx, scope, &branchroom_model.Decision{
		ScopeID:         scope.ID,
		RepoID:          scope.RepoID,
		GrantID:         admission.writeGrant.ID,
		ActorType:       admission.actor.Type,
		ActorID:         admission.actor.ID,
		Action:          branchroom_model.ActionWriteSource,
		ResourceType:    branchroom_model.ResourceTypeWorkspace,
		ResourceID:      workspaceResourceID(workspace.WorkspaceKey),
		Surface:         branchroom_model.SurfaceGitHTTP,
		Result:          branchroom_model.DecisionResultAllow,
		ReasonCode:      ReasonGrantMatched,
		OrdinaryAllowed: admission.ceiling.CodeRead,
		ExpiresUnix:     admission.now,
		CreatedUnix:     admission.now,
	})
	return err
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

		admission, allowed, err := authorizeSealedSourceWriteAdmission(txCtx, scope, workspace, request.UserID)
		if err != nil || !allowed {
			return nil, errReceiveAdmissionDenied
		}

		if err := branchroom_module.VerifyRestrictedStoreHook(txCtx, setting.Branchroom.RestrictedRepoRootPath, workspace.StoreID); err != nil {
			return nil, errReceiveAdmissionDenied
		}
		if err := recordSealedSourceWriteAdmission(txCtx, scope, workspace, admission); err != nil {
			return nil, errReceiveAdmissionDenied
		}

		sessionKeyUUID, err := uuid.NewRandom()
		if err != nil {
			return nil, errReceiveAdmissionDenied
		}
		bearerEntropy := make([]byte, receiveAdmissionBearerEntropyLen)
		if _, err := rand.Read(bearerEntropy); err != nil {
			clear(bearerEntropy)
			return nil, errReceiveAdmissionDenied
		}
		bearerSecret := make([]byte, hex.EncodedLen(len(bearerEntropy)))
		hex.Encode(bearerSecret, bearerEntropy)
		clear(bearerEntropy)

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
			IssuedUnix:       admission.now,
			ExpiresUnix:      admission.now.AddDuration(receiveAdmissionLifetime),
		}
		if err := branchroom_model.InsertReceiveSession(txCtx, session); err != nil {
			credential.Clear()
			return nil, errReceiveAdmissionDenied
		}
		if err := branchroom_model.RecordAgentAction(txCtx, request.Credential, "receive", session.ID); err != nil {
			credential.Clear()
			return nil, errReceiveAdmissionDenied
		}
		claimed, err := branchroom_model.ClaimReceiveSession(txCtx, session.ID, admission.now)
		if err != nil || !claimed {
			credential.Clear()
			return nil, errReceiveAdmissionDenied
		}
		return credential, nil
	})
	if err != nil || credential == nil {
		credential.Clear()
		return nil, errReceiveAdmissionDenied
	}
	return credential, nil
}
