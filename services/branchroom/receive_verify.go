// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/setting"
)

var errReceiveDenied = errors.New("Branchroom receive denied")

type VerifyReceiveSessionPushRequest struct {
	SessionKey   string
	BearerSecret []byte
	Updates      []byte
	Quarantine   branchroom_module.ReceiveQuarantineValues
}

func VerifyReceiveSessionPush(ctx context.Context, request VerifyReceiveSessionPushRequest) error {
	return verifyClaimedReceive(ctx, verifyClaimedReceiveRequest(request))
}

type verifyClaimedReceiveRequest struct {
	SessionKey   string
	BearerSecret []byte
	Updates      []byte
	Quarantine   branchroom_module.ReceiveQuarantineValues
}

type receiveVerification struct {
	admitted bool
}

// completeReceiveSessionDenied closes an authenticated claim so it can never dangle.
// It deliberately bypasses the policy evaluator so no decision or audit rows exist.
func completeReceiveSessionDenied(txCtx context.Context, sessionID int64) (receiveVerification, error) {
	finalized, err := branchroom_model.CompleteReceiveSession(txCtx, sessionID, branchroom_model.ReceiveSessionOutcomeDenied)
	if err != nil || !finalized {
		return receiveVerification{}, errReceiveDenied
	}
	return receiveVerification{}, nil
}

func completeReceiveSessionAdmitted(txCtx context.Context, sessionID int64) (receiveVerification, error) {
	finalized, err := branchroom_model.CompleteReceiveSession(txCtx, sessionID, branchroom_model.ReceiveSessionOutcomeAdmitted)
	if err != nil || !finalized {
		return receiveVerification{}, errReceiveDenied
	}
	return receiveVerification{admitted: true}, nil
}

func verifyClaimedReceive(ctx context.Context, request verifyClaimedReceiveRequest) error {
	defer clear(request.BearerSecret)

	if !setting.Branchroom.Enabled || request.SessionKey == "" || len(request.BearerSecret) == 0 || len(request.Updates) == 0 || db.InTransaction(ctx) {
		return errReceiveDenied
	}

	updates, err := branchroom_module.ParseReceiveUpdates(request.Updates)
	if err != nil {
		return errReceiveDenied
	}
	for _, update := range updates {
		if err := branchroom_module.CheckReceiveRefPolicyV0(update); err != nil {
			return errReceiveDenied
		}
	}

	initial, err := branchroom_model.GetReceiveSessionByKey(ctx, request.SessionKey)
	if err != nil {
		return errReceiveDenied
	}

	bearerHash := sha256.Sum256(request.BearerSecret)
	defer clear(bearerHash[:])
	bearerHashHex := make([]byte, hex.EncodedLen(len(bearerHash)))
	hex.Encode(bearerHashHex, bearerHash[:])
	defer clear(bearerHashHex)

	verified, err := db.WithTx2(ctx, func(txCtx context.Context) (receiveVerification, error) {
		if err := lockRepositoryLifecycleForRead(txCtx, initial.RepoID); err != nil {
			return receiveVerification{}, errReceiveDenied
		}
		session, err := branchroom_model.GetReceiveSessionByIDForUpdate(txCtx, initial.ID)
		if err != nil {
			return receiveVerification{}, errReceiveDenied
		}
		// bearer authentication must precede every other row read so wrong or missing
		// secrets stay completely inert and cannot be probed through scope state
		if session.Validate() != nil ||
			session.ID != initial.ID ||
			session.SessionKey != request.SessionKey ||
			session.RepoID != initial.RepoID ||
			session.ScopeID != initial.ScopeID ||
			session.WorkspaceID != initial.WorkspaceID ||
			subtle.ConstantTimeCompare([]byte(session.BearerSecretHash), bearerHashHex) != 1 {
			return receiveVerification{}, errReceiveDenied
		}
		// Claim is the authorization boundary. A valid pre-receive request completes
		// the one receive already admitted there even if a later grant revocation,
		// workspace retirement, or repository transfer races after the Git child started.
		if session.State != branchroom_model.ReceiveSessionStateClaimed {
			return receiveVerification{}, errReceiveDenied
		}

		scope, err := branchroom_model.GetScopeByIDForUpdate(txCtx, session.ScopeID)
		if err != nil {
			return completeReceiveSessionDenied(txCtx, session.ID)
		}
		workspace, err := branchroom_model.GetWorkspaceByIDForUpdate(txCtx, session.WorkspaceID)
		if err != nil {
			return completeReceiveSessionDenied(txCtx, session.ID)
		}
		if scope.Validate() != nil || workspace.Validate() != nil ||
			scope.ID != session.ScopeID ||
			scope.RepoID != session.RepoID ||
			scope.Kind != branchroom_model.ScopeKindRepository ||
			workspace.ID != session.WorkspaceID ||
			workspace.ScopeID != scope.ID ||
			workspace.RepoID != session.RepoID {
			return completeReceiveSessionDenied(txCtx, session.ID)
		}
		// quarantine facts are judged only once the session is fully authenticated and bound,
		// so an invalid or missing quarantine terminates the claim without any evaluator trace
		quarantine, err := branchroom_module.ValidateReceiveQuarantine(setting.Branchroom.RestrictedRepoRootPath, workspace.StoreID, request.Quarantine)
		if err != nil {
			return completeReceiveSessionDenied(txCtx, session.ID)
		}
		if err := branchroom_module.VerifyRestrictedStoreHook(txCtx, setting.Branchroom.RestrictedRepoRootPath, workspace.StoreID); err != nil {
			return completeReceiveSessionDenied(txCtx, session.ID)
		}
		if !verifyReceiveCommitUpdates(txCtx, workspace, updates, quarantine) {
			return completeReceiveSessionDenied(txCtx, session.ID)
		}
		return completeReceiveSessionAdmitted(txCtx, session.ID)
	})
	if err != nil {
		return errReceiveDenied
	}
	if verified.admitted {
		return nil
	}
	return errReceiveDenied
}

// FinalizeReceiveAdmission records a claimed session as failed when receive-pack
// exits without passing through pre-receive. It accepts only the server-generated
// credential and cannot overwrite a completed admission or denial.
func FinalizeReceiveAdmission(ctx context.Context, credential *ReceiveAdmissionCredential) {
	if credential == nil || credential.SessionKey == "" || len(credential.BearerSecret) == 0 || db.InTransaction(ctx) {
		return
	}
	initial, err := branchroom_model.GetReceiveSessionByKey(ctx, credential.SessionKey)
	if err != nil {
		return
	}

	bearerHash := sha256.Sum256(credential.BearerSecret)
	defer clear(bearerHash[:])
	bearerHashHex := make([]byte, hex.EncodedLen(len(bearerHash)))
	hex.Encode(bearerHashHex, bearerHash[:])
	defer clear(bearerHashHex)

	_ = db.WithTx(ctx, func(txCtx context.Context) error {
		session, err := branchroom_model.GetReceiveSessionByIDForUpdate(txCtx, initial.ID)
		if err != nil || session.Validate() != nil || session.SessionKey != credential.SessionKey ||
			subtle.ConstantTimeCompare([]byte(session.BearerSecretHash), bearerHashHex) != 1 {
			return nil
		}
		_, _ = branchroom_model.CompleteReceiveSession(txCtx, session.ID, branchroom_model.ReceiveSessionOutcomeFailed)
		return nil
	})
}
