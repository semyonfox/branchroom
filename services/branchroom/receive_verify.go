// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/json"
	"gitea.dev/modules/log"
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
// the outcome is chained independently of the request-time permission decision.
func completeReceiveSessionDenied(txCtx context.Context, sessionID int64) (receiveVerification, error) {
	session, err := branchroom_model.GetReceiveSessionByID(txCtx, sessionID)
	if err != nil {
		return receiveVerification{}, err
	}
	if err := recordReceiveOutcome(txCtx, session, "receive_rejected", "receive verification failed"); err != nil {
		return receiveVerification{}, err
	}
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

		updates, err := branchroom_module.ParseReceiveUpdates(request.Updates)
		if err != nil {
			return completeReceiveSessionDenied(txCtx, session.ID)
		}
		session.Updates = string(request.Updates)
		if _, err := db.GetEngine(txCtx).ID(session.ID).Cols("updates").Update(session); err != nil {
			return receiveVerification{}, err
		}
		for _, update := range updates {
			if err := branchroom_module.CheckReceiveRefPolicyV0(update); err != nil {
				if err := recordReceiveOutcome(txCtx, session, "receive_rejected", err.Error()); err != nil {
					return receiveVerification{}, err
				}
				finalized, err := branchroom_model.CompleteReceiveSession(txCtx, session.ID, branchroom_model.ReceiveSessionOutcomeDenied)
				if err != nil || !finalized {
					return receiveVerification{}, errReceiveDenied
				}
				return receiveVerification{}, nil
			}
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

// FinalizeReceiveAdmission records the receive outcome after Git exits.
// a ref changed by a concurrent push is reported as unconfirmed, never accepted.
func FinalizeReceiveAdmission(ctx context.Context, credential *ReceiveAdmissionCredential) {
	ctx = context.WithoutCancel(ctx)
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

	err = db.WithTx(ctx, func(txCtx context.Context) error {
		session, err := branchroom_model.GetReceiveSessionByIDForUpdate(txCtx, initial.ID)
		if err != nil || session.Validate() != nil || session.SessionKey != credential.SessionKey ||
			subtle.ConstantTimeCompare([]byte(session.BearerSecretHash), bearerHashHex) != 1 {
			return nil
		}
		if session.State == branchroom_model.ReceiveSessionStateClaimed {
			finalized, err := branchroom_model.CompleteReceiveSession(txCtx, session.ID, branchroom_model.ReceiveSessionOutcomeFailed)
			if err != nil || !finalized {
				return err
			}
			return recordReceiveOutcome(txCtx, session, "receive_rejected", "receive-pack exited without hook admission")
		}
		if session.Outcome != branchroom_model.ReceiveSessionOutcomeAdmitted {
			return nil
		}
		workspace, err := branchroom_model.GetWorkspaceByID(txCtx, session.WorkspaceID)
		if err != nil {
			return err
		}
		restricted, err := branchroom_module.RestrictedRepository(workspace.StoreID)
		if err != nil {
			return err
		}
		updates, err := branchroom_module.ParseReceiveUpdates([]byte(session.Updates))
		if err != nil {
			return err
		}
		reason, explanation := "receive_accepted", ""
		for _, update := range updates {
			oid, _, err := gitcmd.NewCommand("rev-parse", "--verify").AddDynamicArguments(update.Ref).WithRepo(restricted).RunStdString(txCtx)
			if err != nil || strings.TrimSpace(oid) != update.NewOID {
				reason, explanation = "receive_unconfirmed", "refs did not match the admitted update after receive-pack exited"
				break
			}
		}
		outcome := branchroom_model.ReceiveSessionOutcomeAccepted
		if reason != "receive_accepted" {
			outcome = branchroom_model.ReceiveSessionOutcomeFailed
		}
		affected, err := db.GetEngine(txCtx).ID(session.ID).Where("outcome = ?", branchroom_model.ReceiveSessionOutcomeAdmitted).Cols("outcome").Update(&branchroom_model.ReceiveSession{Outcome: outcome})
		if err != nil || affected != 1 {
			return err
		}
		return recordReceiveOutcome(txCtx, session, reason, explanation)
	})
	if err != nil {
		log.Error("Branchroom receive outcome audit failed: %v", err)
	}
}

func recordReceiveOutcome(ctx context.Context, session *branchroom_model.ReceiveSession, reason, explanation string) error {
	scope, err := branchroom_model.GetScopeByIDForUpdate(ctx, session.ScopeID)
	if err != nil {
		return err
	}
	updates, _ := branchroom_module.ParseReceiveUpdates([]byte(session.Updates))
	details, err := json.Marshal(struct {
		Updates []branchroom_module.ReceiveRefUpdate `json:"updates,omitempty"`
		Reason  string                               `json:"reason,omitempty"`
	}{updates, explanation})
	if err != nil {
		return err
	}
	result := branchroom_model.DecisionResultDeny
	if reason == "receive_accepted" {
		result = branchroom_model.DecisionResultAllow
	}
	_, err = branchroom_model.RecordDecisionAndAudit(ctx, scope, &branchroom_model.Decision{
		ScopeID: session.ScopeID, RepoID: session.RepoID, ActorType: session.ActorType, ActorID: session.ActorID,
		Action: branchroom_model.ActionWriteSource, ResourceType: branchroom_model.ResourceTypeScope,
		ResourceID: scopeResourceID(session.ScopeID), Surface: branchroom_model.SurfaceGitHTTP,
		Result: result, ReasonCode: reason, OrdinaryAllowed: true, Details: string(details),
	})
	return err
}
