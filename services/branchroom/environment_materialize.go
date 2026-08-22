// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/google/uuid"
)

// materializeEnvironmentLeaseRequest contains the internal, root-confined request for one lease.
// materializeEnvironmentLeaseToRoot consumes and clears Credential.BearerSecret before returning.
type materializeEnvironmentLeaseRequest struct {
	Credential  environmentLeaseCredential
	Actor       Actor
	Destination string
	RequestID   string
}

type claimedEnvironmentLease struct {
	lease    *branchroom_model.EnvironmentLease
	revision *branchroom_model.EnvironmentRevision
	denied   bool
}

// materializeEnvironmentLeaseToRoot decrypts one claimed revision into a relative destination below root.
// Callers must provide a Branchroom-owned ephemeral root. This is not a general checkout writer.
func materializeEnvironmentLeaseToRoot(ctx context.Context, root *os.Root, request materializeEnvironmentLeaseRequest) error {
	defer request.Credential.clear()
	if err := requireDevelopmentEnvironment(); err != nil {
		return err
	}
	if root == nil || request.Actor.Type != branchroom_model.ActorTypeUser || request.Actor.ID <= 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom environment materialization request")
	}
	destination, err := validateEnvironmentDestination(request.Destination)
	if err != nil {
		return err
	}
	if !branchroom_model.IsCanonicalUUID(request.Credential.LeaseKey) || len(request.Credential.BearerSecret) != environmentLeaseBearerSecretLength {
		return util.NewNotExistErrorf("Branchroom environment lease does not exist")
	}
	initialLease, err := branchroom_model.GetEnvironmentLeaseByKey(ctx, request.Credential.LeaseKey)
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			return util.NewNotExistErrorf("Branchroom environment lease does not exist")
		}
		return err
	}
	if subtle.ConstantTimeCompare([]byte(initialLease.BearerSecretHash), []byte(hashEnvironmentLeaseSecret(request.Credential.BearerSecret))) != 1 {
		return util.NewNotExistErrorf("Branchroom environment lease does not exist")
	}
	initialRevision, err := branchroom_model.GetEnvironmentRevisionByID(ctx, initialLease.RevisionID)
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			return util.NewNotExistErrorf("Branchroom environment lease does not exist")
		}
		return err
	}
	if initialLease.ScopeID != initialRevision.ScopeID {
		return util.NewNotExistErrorf("Branchroom environment lease does not exist")
	}

	claimed, err := claimEnvironmentLease(ctx, initialLease, initialRevision, request)
	if err != nil {
		return err
	}
	if claimed.denied {
		return util.NewNotExistErrorf("Branchroom environment lease does not exist")
	}

	kek, err := loadDevelopmentEnvironmentKEK()
	if err != nil {
		_ = finishEnvironmentLease(ctx, claimed, request, branchroom_model.EnvironmentLeaseOutcomeFailed)
		return ErrEnvironmentUnavailable
	}
	defer clearBytes(kek)
	if claimed.revision.CiphertextDigest != branchroom_model.EnvironmentCiphertextDigest(claimed.revision.PayloadCiphertext) {
		_ = finishEnvironmentLease(ctx, claimed, request, branchroom_model.EnvironmentLeaseOutcomeFailed)
		return ErrEnvironmentMaterialization
	}
	if claimed.revision.CipherSuite != branchroom_model.EnvironmentCipherSuiteAES256GCM || claimed.revision.KeyProvider != branchroom_model.EnvironmentKeyProviderLocalDev || claimed.revision.KeyReference != environmentDevelopmentKeyReference {
		_ = finishEnvironmentLease(ctx, claimed, request, branchroom_model.EnvironmentLeaseOutcomeFailed)
		return ErrEnvironmentMaterialization
	}
	aad, err := environmentRevisionAAD(claimed.revision.RepoID, claimed.revision.ScopeID, claimed.revision.Revision, claimed.revision.RevisionKey, string(claimed.revision.CipherSuite), string(claimed.revision.KeyProvider), claimed.revision.KeyReference)
	if err != nil {
		_ = finishEnvironmentLease(ctx, claimed, request, branchroom_model.EnvironmentLeaseOutcomeFailed)
		return ErrEnvironmentMaterialization
	}
	plaintext, err := decryptEnvironmentValue(kek, &environmentEncryptedValue{
		PayloadNonce:         claimed.revision.PayloadNonce,
		PayloadCiphertext:    claimed.revision.PayloadCiphertext,
		WrappedDEKNonce:      claimed.revision.WrappedDEKNonce,
		WrappedDEKCiphertext: claimed.revision.WrappedDEKCiphertext,
	}, aad)
	clearBytes(aad)
	if err != nil {
		_ = finishEnvironmentLease(ctx, claimed, request, branchroom_model.EnvironmentLeaseOutcomeFailed)
		return ErrEnvironmentMaterialization
	}
	defer clearBytes(plaintext)
	if err := writeEnvironmentMaterialization(root, destination, plaintext); err != nil {
		_ = finishEnvironmentLease(ctx, claimed, request, branchroom_model.EnvironmentLeaseOutcomeFailed)
		return ErrEnvironmentMaterialization
	}
	if err := finishEnvironmentLease(ctx, claimed, request, branchroom_model.EnvironmentLeaseOutcomeMaterialized); err != nil {
		return ErrEnvironmentMaterialization
	}
	return nil
}

func claimEnvironmentLease(ctx context.Context, initialLease *branchroom_model.EnvironmentLease, initialRevision *branchroom_model.EnvironmentRevision, request materializeEnvironmentLeaseRequest) (claimedEnvironmentLease, error) {
	return db.WithTx2(ctx, func(txCtx context.Context) (claimedEnvironmentLease, error) {
		if err := lockRepositoryLifecycle(txCtx, initialRevision.RepoID); err != nil {
			return claimedEnvironmentLease{}, err
		}
		scope, err := branchroom_model.GetScopeByIDForUpdate(txCtx, initialLease.ScopeID)
		if err != nil {
			return claimedEnvironmentLease{}, err
		}
		revision, err := branchroom_model.GetEnvironmentRevisionByIDForUpdate(txCtx, initialRevision.ID)
		if err != nil {
			return claimedEnvironmentLease{}, err
		}
		lease, err := branchroom_model.GetEnvironmentLeaseByIDForUpdate(txCtx, initialLease.ID)
		if err != nil {
			return claimedEnvironmentLease{}, err
		}
		if scope.RepoID != revision.RepoID || scope.Kind != branchroom_model.ScopeKindEnvironment || revision.ScopeID != scope.ID || lease.ScopeID != scope.ID || lease.RevisionID != revision.ID || lease.ActorType != request.Actor.Type || lease.ActorID != request.Actor.ID || lease.LeaseKey != request.Credential.LeaseKey || lease.State != branchroom_model.EnvironmentLeaseStateIssued {
			return claimedEnvironmentLease{denied: true}, nil
		}
		now := timeutil.TimeStampNow()
		if lease.ExpiresUnix <= now {
			expired, err := branchroom_model.ExpireEnvironmentLease(txCtx, lease.ID, now)
			if err != nil {
				return claimedEnvironmentLease{}, err
			}
			if expired {
				if err := recordEnvironmentEvent(txCtx, scope, revision, lease.GrantID, request.Actor, branchroom_model.ActionMaterialize, false, branchroom_model.DecisionResultDeny, reasonEnvironmentLeaseExpired, request.RequestID); err != nil {
					return claimedEnvironmentLease{}, err
				}
			}
			return claimedEnvironmentLease{denied: true}, nil
		}
		if revision.State != branchroom_model.EnvironmentRevisionStateActive {
			return claimDeniedEnvironmentLease(txCtx, scope, revision, lease, request, false)
		}
		ceiling, allowed, err := resolveDirectUserRepositoryCeiling(txCtx, revision.RepoID, request.Actor)
		if err != nil {
			return claimedEnvironmentLease{}, err
		}
		if !allowed {
			return claimDeniedEnvironmentLease(txCtx, scope, revision, lease, request, false)
		}
		decision, err := NewDBEvaluator().Evaluate(txCtx, Request{
			ScopeID:      scope.ID,
			RepoID:       revision.RepoID,
			Actor:        request.Actor,
			Ceiling:      ceiling,
			Action:       branchroom_model.ActionMaterialize,
			ResourceType: branchroom_model.ResourceTypeEnvironment,
			ResourceID:   environmentRevisionResourceID(scope.ID, revision.RevisionKey),
			Surface:      branchroom_model.SurfaceSystem,
			RequestID:    request.RequestID,
		})
		if err != nil {
			return claimedEnvironmentLease{}, err
		}
		if !decision.Allowed {
			return claimDeniedEnvironmentLease(txCtx, scope, revision, lease, request, ceiling.CodeRead)
		}
		claimed, err := branchroom_model.ClaimEnvironmentLease(txCtx, lease.ID, now)
		if err != nil {
			return claimedEnvironmentLease{}, err
		}
		if !claimed {
			return claimedEnvironmentLease{denied: true}, nil
		}
		if err := recordEnvironmentEvent(txCtx, scope, revision, decision.GrantID, request.Actor, branchroom_model.ActionMaterialize, ceiling.CodeRead, branchroom_model.DecisionResultAllow, reasonEnvironmentLeaseClaimed, request.RequestID); err != nil {
			return claimedEnvironmentLease{}, err
		}
		lease.State = branchroom_model.EnvironmentLeaseStateClaimed
		lease.ClaimedUnix = now
		return claimedEnvironmentLease{lease: lease, revision: revision}, nil
	})
}

func claimDeniedEnvironmentLease(ctx context.Context, scope *branchroom_model.Scope, revision *branchroom_model.EnvironmentRevision, lease *branchroom_model.EnvironmentLease, request materializeEnvironmentLeaseRequest, ordinaryAllowed bool) (claimedEnvironmentLease, error) {
	now := timeutil.TimeStampNow()
	claimed, err := branchroom_model.ClaimEnvironmentLease(ctx, lease.ID, now)
	if err != nil {
		return claimedEnvironmentLease{}, err
	}
	if !claimed {
		return claimedEnvironmentLease{denied: true}, nil
	}
	completed, err := branchroom_model.CompleteEnvironmentLease(ctx, lease.ID, branchroom_model.EnvironmentLeaseOutcomeDenied)
	if err != nil {
		return claimedEnvironmentLease{}, err
	}
	if !completed {
		return claimedEnvironmentLease{denied: true}, nil
	}
	if err := recordEnvironmentEvent(ctx, scope, revision, lease.GrantID, request.Actor, branchroom_model.ActionMaterialize, ordinaryAllowed, branchroom_model.DecisionResultDeny, reasonEnvironmentLeaseDenied, request.RequestID); err != nil {
		return claimedEnvironmentLease{}, err
	}
	return claimedEnvironmentLease{denied: true}, nil
}

func finishEnvironmentLease(ctx context.Context, claimed claimedEnvironmentLease, request materializeEnvironmentLeaseRequest, outcome branchroom_model.EnvironmentLeaseOutcome) error {
	if claimed.lease == nil || claimed.revision == nil {
		return ErrEnvironmentMaterialization
	}
	return db.WithTx(ctx, func(txCtx context.Context) error {
		if err := lockRepositoryLifecycle(txCtx, claimed.revision.RepoID); err != nil {
			return err
		}
		scope, err := branchroom_model.GetScopeByIDForUpdate(txCtx, claimed.lease.ScopeID)
		if err != nil {
			return err
		}
		revision, err := branchroom_model.GetEnvironmentRevisionByIDForUpdate(txCtx, claimed.revision.ID)
		if err != nil {
			return err
		}
		lease, err := branchroom_model.GetEnvironmentLeaseByIDForUpdate(txCtx, claimed.lease.ID)
		if err != nil {
			return err
		}
		if scope.RepoID != revision.RepoID || revision.ScopeID != scope.ID || lease.ScopeID != scope.ID || lease.RevisionID != revision.ID || lease.ActorType != request.Actor.Type || lease.ActorID != request.Actor.ID {
			return ErrEnvironmentMaterialization
		}
		completed, err := branchroom_model.CompleteEnvironmentLease(txCtx, lease.ID, outcome)
		if err != nil {
			return err
		}
		if !completed {
			return ErrEnvironmentMaterialization
		}
		result := branchroom_model.DecisionResultAllow
		reasonCode := reasonEnvironmentLeaseMaterialized
		if outcome == branchroom_model.EnvironmentLeaseOutcomeFailed {
			result = branchroom_model.DecisionResultDeny
			reasonCode = reasonEnvironmentLeaseFailed
		}
		return recordEnvironmentEvent(txCtx, scope, revision, lease.GrantID, request.Actor, branchroom_model.ActionMaterialize, true, result, reasonCode, request.RequestID)
	})
}

// recordEnvironmentEvent appends one environment-surface decision to the scope audit chain.
func recordEnvironmentEvent(ctx context.Context, scope *branchroom_model.Scope, revision *branchroom_model.EnvironmentRevision, grantID int64, actor Actor, action branchroom_model.Action, ordinaryAllowed bool, result branchroom_model.DecisionResult, reasonCode, requestID string) error {
	if scope == nil || revision == nil {
		return util.NewInvalidArgumentErrorf("Branchroom environment scope and revision are required")
	}
	now := timeutil.TimeStampNow()
	_, err := branchroom_model.RecordDecisionAndAudit(ctx, scope, &branchroom_model.Decision{
		ScopeID:         scope.ID,
		RepoID:          scope.RepoID,
		GrantID:         grantID,
		ActorType:       actor.Type,
		ActorID:         actor.ID,
		Action:          action,
		ResourceType:    branchroom_model.ResourceTypeEnvironment,
		ResourceID:      environmentRevisionResourceID(scope.ID, revision.RevisionKey),
		Surface:         branchroom_model.SurfaceSystem,
		Result:          result,
		ReasonCode:      reasonCode,
		OrdinaryAllowed: ordinaryAllowed,
		RequestID:       requestID,
		ExpiresUnix:     now,
		CreatedUnix:     now,
	})
	return err
}

func newEnvironmentLeaseSecret() ([]byte, error) {
	secret := make([]byte, environmentLeaseBearerSecretLength)
	if _, err := rand.Read(secret); err != nil {
		clearBytes(secret)
		return nil, ErrEnvironmentUnavailable
	}
	return secret, nil
}

func hashEnvironmentLeaseSecret(secret []byte) string {
	digest := sha256.Sum256(secret)
	return hex.EncodeToString(digest[:])
}

func validateEnvironmentDestination(destination string) (string, error) {
	destination, err := branchroom_module.ValidateEnvironmentDestination(destination)
	if err != nil {
		return "", util.NewInvalidArgumentErrorf("invalid Branchroom environment destination")
	}
	return destination, nil
}

func writeEnvironmentMaterialization(root *os.Root, destination string, plaintext []byte) (err error) {
	parent := path.Dir(destination)
	if parent != "." {
		if err := root.MkdirAll(parent, 0o700); err != nil {
			return err
		}
	}
	if info, err := root.Lstat(destination); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return ErrEnvironmentMaterialization
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	temporary := path.Join(parent, "."+path.Base(destination)+".branchroom-"+uuid.NewString())
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = root.Remove(temporary)
		}
	}()
	if written, writeErr := file.Write(plaintext); writeErr != nil {
		_ = file.Close()
		return writeErr
	} else if written != len(plaintext) {
		_ = file.Close()
		return io.ErrShortWrite
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return root.Rename(temporary, destination)
}
