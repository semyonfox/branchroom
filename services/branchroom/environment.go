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
	access_model "gitea.dev/models/perm/access"
	repo_model "gitea.dev/models/repo"
	user_model "gitea.dev/models/user"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/google/uuid"
)

const (
	environmentDevelopmentKeyReference = "local-dev/primary-v1"
	environmentLeaseBearerSecretLength = 32
)

var (
	// ErrEnvironmentUnavailable deliberately does not reveal local key-provider configuration.
	ErrEnvironmentUnavailable = errors.New("Branchroom encrypted environment service is unavailable")
	// ErrEnvironmentMaterialization deliberately omits the target path and plaintext details.
	ErrEnvironmentMaterialization = errors.New("unable to materialize Branchroom environment")
)

// createEnvironmentRevisionRequest contains the internal, development-only inputs for one immutable revision.
// createEnvironmentRevision consumes and clears Plaintext before returning.
type createEnvironmentRevisionRequest struct {
	RepoID    int64
	ScopeID   int64
	Actor     Actor
	Plaintext []byte
	RequestID string
}

// createEnvironmentRevision stores a development-only encrypted environment revision.
// It has no web, API, CLI, Git, Actions, or direct-storage entry point.
func createEnvironmentRevision(ctx context.Context, request createEnvironmentRevisionRequest) (*branchroom_model.EnvironmentRevision, error) {
	defer clearBytes(request.Plaintext)
	if err := requireDevelopmentEnvironment(); err != nil {
		return nil, err
	}
	if request.RepoID <= 0 || request.ScopeID <= 0 || request.Actor.Type != branchroom_model.ActorTypeUser || request.Actor.ID <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom environment revision request")
	}
	if len(request.Plaintext) > setting.BranchroomEnvironment.MaxRevisionBytes {
		return nil, util.NewInvalidArgumentErrorf("Branchroom environment revision is too large")
	}

	owner, err := CanManageRepositoryScopes(ctx, request.RepoID, request.Actor)
	if err != nil {
		return nil, err
	}
	if !owner {
		return nil, util.NewNotExistErrorf("Branchroom environment scope does not exist")
	}

	kek, err := loadDevelopmentEnvironmentKEK()
	if err != nil {
		return nil, ErrEnvironmentUnavailable
	}
	defer clearBytes(kek)

	return db.WithTx2(ctx, func(txCtx context.Context) (*branchroom_model.EnvironmentRevision, error) {
		if err := lockRepositoryLifecycle(txCtx, request.RepoID); err != nil {
			return nil, err
		}
		owner, err := CanManageRepositoryScopes(txCtx, request.RepoID, request.Actor)
		if err != nil {
			return nil, err
		}
		if !owner {
			return nil, util.NewNotExistErrorf("Branchroom environment scope does not exist")
		}
		scope, err := branchroom_model.GetScopeByIDForUpdate(txCtx, request.ScopeID)
		if err != nil {
			return nil, err
		}
		if scope.RepoID != request.RepoID || scope.Kind != branchroom_model.ScopeKindEnvironment || scope.State != branchroom_model.ScopeStateActive {
			return nil, util.NewNotExistErrorf("Branchroom environment scope does not exist")
		}

		revisions, err := branchroom_model.FindEnvironmentRevisionsByScopeIDForUpdate(txCtx, scope.ID)
		if err != nil {
			return nil, err
		}
		nextRevision := int64(1)
		if len(revisions) > 0 {
			nextRevision = revisions[len(revisions)-1].Revision + 1
		}
		revisionKey := uuid.NewString()
		aad, err := environmentRevisionAAD(request.RepoID, scope.ID, nextRevision, revisionKey, environmentCipherSuiteAES256GCM, string(branchroom_model.EnvironmentKeyProviderLocalDev), environmentDevelopmentKeyReference)
		if err != nil {
			return nil, ErrEnvironmentUnavailable
		}
		encrypted, err := encryptEnvironmentValue(kek, request.Plaintext, aad)
		clearBytes(aad)
		if err != nil {
			return nil, ErrEnvironmentUnavailable
		}
		revision := &branchroom_model.EnvironmentRevision{
			RevisionKey:          revisionKey,
			ScopeID:              scope.ID,
			RepoID:               request.RepoID,
			Revision:             nextRevision,
			CipherSuite:          branchroom_model.EnvironmentCipherSuiteAES256GCM,
			PayloadNonce:         encrypted.PayloadNonce,
			PayloadCiphertext:    encrypted.PayloadCiphertext,
			WrappedDEKNonce:      encrypted.WrappedDEKNonce,
			WrappedDEKCiphertext: encrypted.WrappedDEKCiphertext,
			KeyProvider:          branchroom_model.EnvironmentKeyProviderLocalDev,
			KeyReference:         environmentDevelopmentKeyReference,
			CiphertextDigest:     branchroom_model.EnvironmentCiphertextDigest(encrypted.PayloadCiphertext),
			PlaintextLength:      int64(len(request.Plaintext)),
			State:                branchroom_model.EnvironmentRevisionStateActive,
			CreatedByID:          request.Actor.ID,
		}
		if err := branchroom_model.InsertEnvironmentRevision(txCtx, revision); err != nil {
			return nil, err
		}
		if err := recordEnvironmentEvent(txCtx, scope, revision, 0, request.Actor, branchroom_model.ActionManagePolicy, true, branchroom_model.DecisionResultAllow, "environment_revision_created", request.RequestID); err != nil {
			return nil, err
		}
		return revision, nil
	})
}

// issueEnvironmentLeaseRequest identifies one internally requested materialization lease.
// The actor's ordinary repository ceiling is always recomputed from persisted Gitea state.
type issueEnvironmentLeaseRequest struct {
	RepoID      int64
	ScopeID     int64
	RevisionKey string
	Actor       Actor
	RequestID   string
}

// environmentLeaseCredential is an opaque one-use credential.
// clear must be called by a future caller once it has handed the credential to a private runner root.
type environmentLeaseCredential struct {
	LeaseKey     string
	BearerSecret []byte
}

// clear removes the raw bearer secret from memory owned by this credential.
func (credential *environmentLeaseCredential) clear() {
	if credential != nil {
		clearBytes(credential.BearerSecret)
	}
}

type issuedEnvironmentLease struct {
	credential environmentLeaseCredential
	denied     bool
}

// issueEnvironmentLease creates a short-lived, one-use credential after a fresh policy decision.
// It is intentionally limited to direct active Gitea users on the internal system surface.
func issueEnvironmentLease(ctx context.Context, request issueEnvironmentLeaseRequest) (*environmentLeaseCredential, error) {
	if err := requireDevelopmentEnvironment(); err != nil {
		return nil, err
	}
	if request.RepoID <= 0 || request.ScopeID <= 0 || request.Actor.Type != branchroom_model.ActorTypeUser || request.Actor.ID <= 0 || !isCanonicalEnvironmentUUID(request.RevisionKey) {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom environment lease request")
	}
	initialRevision, err := branchroom_model.GetEnvironmentRevisionByKey(ctx, request.RevisionKey)
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			return nil, util.NewNotExistErrorf("Branchroom environment revision does not exist")
		}
		return nil, err
	}
	if initialRevision.RepoID != request.RepoID || initialRevision.ScopeID != request.ScopeID {
		return nil, util.NewNotExistErrorf("Branchroom environment revision does not exist")
	}

	issued, err := db.WithTx2(ctx, func(txCtx context.Context) (issuedEnvironmentLease, error) {
		if err := lockRepositoryLifecycle(txCtx, request.RepoID); err != nil {
			return issuedEnvironmentLease{}, err
		}
		scope, err := branchroom_model.GetScopeByIDForUpdate(txCtx, request.ScopeID)
		if err != nil {
			return issuedEnvironmentLease{}, err
		}
		revision, err := branchroom_model.GetEnvironmentRevisionByIDForUpdate(txCtx, initialRevision.ID)
		if err != nil {
			return issuedEnvironmentLease{}, err
		}
		if scope.RepoID != request.RepoID || scope.Kind != branchroom_model.ScopeKindEnvironment || revision.ScopeID != scope.ID || revision.RepoID != request.RepoID || revision.RevisionKey != request.RevisionKey || revision.State != branchroom_model.EnvironmentRevisionStateActive {
			return issuedEnvironmentLease{denied: true}, nil
		}
		ceiling, allowed, err := resolveEnvironmentUserCeiling(txCtx, request.RepoID, request.Actor)
		if err != nil {
			return issuedEnvironmentLease{}, err
		}
		if !allowed {
			return issuedEnvironmentLease{denied: true}, nil
		}
		decision, err := NewDBEvaluator().Evaluate(txCtx, Request{
			ScopeID:      scope.ID,
			RepoID:       request.RepoID,
			Actor:        request.Actor,
			Ceiling:      ceiling,
			Action:       branchroom_model.ActionMaterialize,
			ResourceType: branchroom_model.ResourceTypeEnvironment,
			ResourceID:   environmentRevisionResourceID(scope.ID, revision.RevisionKey),
			Surface:      branchroom_model.SurfaceSystem,
			RequestID:    request.RequestID,
		})
		if err != nil {
			return issuedEnvironmentLease{}, err
		}
		if !decision.Allowed {
			return issuedEnvironmentLease{denied: true}, nil
		}

		secret, err := newEnvironmentLeaseSecret()
		if err != nil {
			return issuedEnvironmentLease{}, err
		}
		credential := environmentLeaseCredential{LeaseKey: uuid.NewString(), BearerSecret: secret}
		now := timeutil.TimeStampNow()
		lease := &branchroom_model.EnvironmentLease{
			LeaseKey:         credential.LeaseKey,
			BearerSecretHash: hashEnvironmentLeaseSecret(secret),
			ScopeID:          scope.ID,
			RevisionID:       revision.ID,
			ActorType:        request.Actor.Type,
			ActorID:          request.Actor.ID,
			GrantID:          decision.GrantID,
			DecisionID:       decision.ID,
			State:            branchroom_model.EnvironmentLeaseStateIssued,
			Outcome:          branchroom_model.EnvironmentLeaseOutcomePending,
			IssuedUnix:       now,
			ExpiresUnix:      now.AddDuration(setting.BranchroomEnvironment.LeaseDuration),
		}
		if err := branchroom_model.InsertEnvironmentLease(txCtx, lease); err != nil {
			credential.clear()
			return issuedEnvironmentLease{}, err
		}
		if err := recordEnvironmentEvent(txCtx, scope, revision, lease.GrantID, request.Actor, branchroom_model.ActionMaterialize, ceiling.CodeRead, branchroom_model.DecisionResultAllow, "environment_lease_issued", request.RequestID); err != nil {
			credential.clear()
			return issuedEnvironmentLease{}, err
		}
		return issuedEnvironmentLease{credential: credential}, nil
	})
	if err != nil {
		issued.credential.clear()
		return nil, err
	}
	if issued.denied {
		issued.credential.clear()
		return nil, util.NewNotExistErrorf("Branchroom environment revision does not exist")
	}
	return &issued.credential, nil
}

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
	if !isCanonicalEnvironmentUUID(request.Credential.LeaseKey) || len(request.Credential.BearerSecret) != environmentLeaseBearerSecretLength {
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

func requireDevelopmentEnvironment() error {
	if !setting.Branchroom.Enabled || !setting.BranchroomEnvironment.Enabled || setting.IsProd || setting.BranchroomEnvironment.Provider != "development-file" || setting.BranchroomEnvironment.DevelopmentKeyFilePath == "" || setting.BranchroomEnvironment.MaxRevisionBytes <= 0 || setting.BranchroomEnvironment.LeaseDuration <= 0 {
		return ErrEnvironmentUnavailable
	}
	return nil
}

func resolveEnvironmentUserCeiling(ctx context.Context, repoID int64, actor Actor) (RepositoryCeiling, bool, error) {
	if actor.Type != branchroom_model.ActorTypeUser || actor.ID <= 0 {
		return RepositoryCeiling{}, false, nil
	}
	user, err := user_model.GetUserByID(ctx, actor.ID)
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			return RepositoryCeiling{}, false, nil
		}
		return RepositoryCeiling{}, false, err
	}
	if !user.IsIndividual() || !user.IsActive || user.ProhibitLogin {
		return RepositoryCeiling{}, false, nil
	}
	repository, err := repo_model.GetRepositoryByID(ctx, repoID)
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			return RepositoryCeiling{}, false, nil
		}
		return RepositoryCeiling{}, false, err
	}
	permission, err := access_model.GetIndividualUserRepoPermission(ctx, repository, user)
	if err != nil {
		return RepositoryCeiling{}, false, err
	}
	return NewRepositoryCeiling(permission), true, nil
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
				if err := recordEnvironmentEvent(txCtx, scope, revision, lease.GrantID, request.Actor, branchroom_model.ActionMaterialize, false, branchroom_model.DecisionResultDeny, "environment_lease_expired", request.RequestID); err != nil {
					return claimedEnvironmentLease{}, err
				}
			}
			return claimedEnvironmentLease{denied: true}, nil
		}
		if revision.State != branchroom_model.EnvironmentRevisionStateActive {
			return claimDeniedEnvironmentLease(txCtx, scope, revision, lease, request, false)
		}
		ceiling, allowed, err := resolveEnvironmentUserCeiling(txCtx, revision.RepoID, request.Actor)
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
		if err := recordEnvironmentEvent(txCtx, scope, revision, decision.GrantID, request.Actor, branchroom_model.ActionMaterialize, ceiling.CodeRead, branchroom_model.DecisionResultAllow, "environment_lease_claimed", request.RequestID); err != nil {
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
	if err := recordEnvironmentEvent(ctx, scope, revision, lease.GrantID, request.Actor, branchroom_model.ActionMaterialize, ordinaryAllowed, branchroom_model.DecisionResultDeny, "environment_lease_denied", request.RequestID); err != nil {
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
		reasonCode := "environment_lease_materialized"
		if outcome == branchroom_model.EnvironmentLeaseOutcomeFailed {
			result = branchroom_model.DecisionResultDeny
			reasonCode = "environment_lease_failed"
		}
		return recordEnvironmentEvent(txCtx, scope, revision, lease.GrantID, request.Actor, branchroom_model.ActionMaterialize, true, result, reasonCode, request.RequestID)
	})
}

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

func isCanonicalEnvironmentUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
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
