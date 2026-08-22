// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"errors"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/google/uuid"
)

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
	if request.RepoID <= 0 || request.ScopeID <= 0 || request.Actor.Type != branchroom_model.ActorTypeUser || request.Actor.ID <= 0 || !branchroom_model.IsCanonicalUUID(request.RevisionKey) {
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
		ceiling, allowed, err := resolveDirectUserRepositoryCeiling(txCtx, request.RepoID, request.Actor)
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
		if err := recordEnvironmentEvent(txCtx, scope, revision, lease.GrantID, request.Actor, branchroom_model.ActionMaterialize, ceiling.CodeRead, branchroom_model.DecisionResultAllow, reasonEnvironmentLeaseIssued, request.RequestID); err != nil {
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
