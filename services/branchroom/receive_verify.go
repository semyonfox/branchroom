// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	repo_model "gitea.dev/models/repo"
	user_model "gitea.dev/models/user"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/json"
	"gitea.dev/modules/log"
	"gitea.dev/modules/private"
	"gitea.dev/modules/secretscan"
	"gitea.dev/modules/setting"
	secret_service "gitea.dev/services/secrets"
)

var errReceiveDenied = errors.New("Branchroom receive denied")

type VerifyReceiveSessionPushRequest struct {
	SessionKey   string
	BearerSecret []byte
	Updates      []byte
	Quarantine   branchroom_module.ReceiveQuarantineValues
	AllowSecrets bool
}

func VerifyReceiveSessionPush(ctx context.Context, request VerifyReceiveSessionPushRequest) error {
	return verifyClaimedReceive(ctx, verifyClaimedReceiveRequest(request))
}

type verifyClaimedReceiveRequest struct {
	SessionKey   string
	BearerSecret []byte
	Updates      []byte
	Quarantine   branchroom_module.ReceiveQuarantineValues
	AllowSecrets bool
}

type receiveVerification struct {
	admitted  bool
	rejection private.BranchroomPushProtectionError
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

	if initial.Validate() != nil || initial.State != branchroom_model.ReceiveSessionStateClaimed ||
		subtle.ConstantTimeCompare([]byte(initial.BearerSecretHash), bearerHashHex) != 1 {
		return errReceiveDenied
	}

	updates, parseErr := branchroom_module.ParseReceiveUpdates(request.Updates)
	var policyErr error
	for _, update := range updates {
		if policyErr = branchroom_module.CheckReceiveRefPolicyV0(update); policyErr != nil {
			break
		}
	}
	var scanStoreID string
	var findings []secretscan.Finding
	var scanErr error
	scanEnabled := setting.SecretScanning.PushProtection
	if scanEnabled && parseErr == nil && policyErr == nil {
		scanStoreID, findings, scanErr = scanReceiveSecrets(ctx, initial, updates, request.Quarantine)
	}
	bypass := request.AllowSecrets && setting.SecretScanning.AllowBypass
	target := "restricted refs"
	if len(updates) == 1 {
		target = strings.TrimPrefix(updates[0].Ref, "refs/heads/")
	}

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

		if parseErr != nil {
			return completeReceiveSessionDenied(txCtx, session.ID)
		}
		session.Updates = string(request.Updates)
		if _, err := db.GetEngine(txCtx).ID(session.ID).Cols("updates").Update(session); err != nil {
			return receiveVerification{}, err
		}
		if policyErr != nil {
			if err := recordReceiveOutcome(txCtx, session, "receive_rejected", policyErr.Error()); err != nil {
				return receiveVerification{}, err
			}
			finalized, err := branchroom_model.CompleteReceiveSession(txCtx, session.ID, branchroom_model.ReceiveSessionOutcomeDenied)
			if err != nil || !finalized {
				return receiveVerification{}, errReceiveDenied
			}
			return receiveVerification{}, nil
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
		// quarantine facts are judged only after the session is authenticated and bound
		quarantine, err := branchroom_module.ValidateReceiveQuarantine(setting.Branchroom.RestrictedRepoRootPath, workspace.StoreID, request.Quarantine)
		if err != nil {
			return completeReceiveSessionDenied(txCtx, session.ID)
		}
		if err := branchroom_module.VerifyRestrictedStoreHook(txCtx, setting.Branchroom.RestrictedRepoRootPath, workspace.StoreID); err != nil {
			return completeReceiveSessionDenied(txCtx, session.ID)
		}
		if scanEnabled && (scanErr != nil || scanStoreID != workspace.StoreID) {
			return completeReceiveSessionDenied(txCtx, session.ID)
		}
		if !verifyReceiveCommitUpdates(txCtx, workspace, updates, quarantine) {
			return completeReceiveSessionDenied(txCtx, session.ID)
		}
		if len(findings) > 0 && !bypass {
			denied, err := completeReceiveSessionDenied(txCtx, session.ID)
			denied.rejection = private.BranchroomPushProtectionError(secret_service.PushProtectionMessage(target, findings))
			return denied, err
		}
		return completeReceiveSessionAdmitted(txCtx, session.ID)
	})
	if err != nil {
		return errReceiveDenied
	}
	if verified.rejection != "" {
		return verified.rejection
	}
	if !verified.admitted {
		return errReceiveDenied
	}
	if len(findings) > 0 {
		repo, repoErr := repo_model.GetRepositoryByID(ctx, initial.RepoID)
		actor, actorErr := user_model.GetUserByID(ctx, initial.ActorID)
		if repoErr != nil || actorErr != nil {
			log.Error("record restricted secret scan bypass for repository %d, actor %d: %v", initial.RepoID, initial.ActorID, errors.Join(repoErr, actorErr))
		} else {
			secret_service.RecordPushBypass(ctx, actor.Name, repo.FullName(), target, findings)
		}
	}
	return nil
}

func scanReceiveSecrets(ctx context.Context, session *branchroom_model.ReceiveSession, updates []branchroom_module.ReceiveRefUpdate, rawQuarantine branchroom_module.ReceiveQuarantineValues) (string, []secretscan.Finding, error) {
	workspace, err := branchroom_model.GetWorkspaceByID(ctx, session.WorkspaceID)
	if err != nil || workspace.Validate() != nil || workspace.ScopeID != session.ScopeID || workspace.RepoID != session.RepoID {
		return "", nil, errReceiveDenied
	}
	quarantine, err := branchroom_module.ValidateReceiveQuarantine(setting.Branchroom.RestrictedRepoRootPath, workspace.StoreID, rawQuarantine)
	if err != nil {
		return "", nil, err
	}
	restricted, err := branchroom_module.RestrictedRepository(workspace.StoreID)
	if err != nil {
		return "", nil, err
	}
	oids := make([]string, 0, len(updates))
	for _, update := range updates {
		oids = append(oids, update.NewOID)
	}
	findings, err := secret_service.ScanPush(ctx, restricted, receiveQuarantineEnv(quarantine), oids...)
	if err != nil {
		log.Error("secret scan of a restricted push for repository %d failed: %v", session.RepoID, err)
		return "", nil, err
	}
	return workspace.StoreID, findings, nil
}

// FinalizeReceiveAdmission records the receive outcome after Git exits.
// only Git's status report can confirm acceptance, independently of later pushes.
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
		if err := lockRepositoryLifecycleForRead(txCtx, initial.RepoID); err != nil {
			return err
		}
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

		updates, err := branchroom_module.ParseReceiveUpdates([]byte(session.Updates))
		if err != nil {
			return err
		}
		reason, explanation := receiveReportOutcome(credential.Report, updates)
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
	var agent branchroom_model.AgentAction
	hasAgent, err := db.GetEngine(ctx).Where("kind = ? AND target_id = ?", "receive", session.ID).Get(&agent)
	if err != nil {
		return err
	}
	var identity *branchroom_model.AgentAction
	if hasAgent {
		identity = &agent
	}
	details, err := json.Marshal(struct {
		Agent   *branchroom_model.AgentAction        `json:"agent_identity,omitempty"`
		Updates []branchroom_module.ReceiveRefUpdate `json:"updates,omitempty"`
		Reason  string                               `json:"reason,omitempty"`
	}{identity, updates, explanation})
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

func receiveReportOutcome(report []byte, updates []branchroom_module.ReceiveRefUpdate) (string, string) {
	// sideband frames can split an inner status packet anywhere
	var sideband, plain []byte
	for len(report) >= 4 {
		size, err := strconv.ParseUint(string(report[:4]), 16, 16)
		if err != nil {
			return "receive_unconfirmed", "invalid Git status report"
		}
		report = report[4:]
		if size <= 2 {
			continue
		}
		if size < 4 || int(size)-4 > len(report) {
			return "receive_unconfirmed", "incomplete Git status report"
		}
		payload := report[:int(size)-4]
		report = report[int(size)-4:]
		if len(payload) != 0 && payload[0] == 1 {
			sideband = append(sideband, payload[1:]...)
		} else if len(payload) != 0 && payload[0] > 3 {
			plain = append(plain, payload...)
		}
	}
	if len(report) != 0 {
		return "receive_unconfirmed", "incomplete Git status report"
	}
	if len(sideband) != 0 {
		return receiveReportOutcome(sideband, updates)
	}
	accepted := make(map[string]bool, len(updates))
	unpacked := false
	for line := range strings.SplitSeq(string(plain), "\n") {
		if line == "unpack ok" {
			unpacked = true
		}
		if strings.HasPrefix(line, "ng ") || strings.HasPrefix(line, "unpack ") && line != "unpack ok" {
			return "receive_rejected", "Git rejected the received objects or a ref update"
		}
		if ref, ok := strings.CutPrefix(line, "ok "); ok {
			accepted[ref] = true
		}
	}
	if unpacked && len(updates) != 0 {
		for _, update := range updates {
			if !accepted[update.Ref] {
				return "receive_unconfirmed", "Git did not confirm every ref update"
			}
		}
		return "receive_accepted", ""
	}
	return "receive_unconfirmed", "Git did not return a complete status report"
}
