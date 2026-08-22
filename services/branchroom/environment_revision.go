// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/util"

	"github.com/google/uuid"
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
		scope, err := requireActiveScopeTx(txCtx, request.RepoID, request.Actor, request.ScopeID, branchroom_model.ScopeKindEnvironment, "Branchroom environment scope does not exist")
		if err != nil {
			return nil, err
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
		if err := recordEnvironmentEvent(txCtx, scope, revision, 0, request.Actor, branchroom_model.ActionManagePolicy, true, branchroom_model.DecisionResultAllow, reasonEnvironmentRevisionCreated, request.RequestID); err != nil {
			return nil, err
		}
		return revision, nil
	})
}

func requireDevelopmentEnvironment() error {
	if !setting.Branchroom.Enabled || !setting.BranchroomEnvironment.Enabled || setting.IsProd || setting.BranchroomEnvironment.Provider != "development-file" || setting.BranchroomEnvironment.DevelopmentKeyFilePath == "" || setting.BranchroomEnvironment.MaxRevisionBytes <= 0 || setting.BranchroomEnvironment.LeaseDuration <= 0 {
		return ErrEnvironmentUnavailable
	}
	return nil
}
