// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"errors"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/util"
)

// resolveEnvironmentReferenceRequest contains a parsed safe-pointer file from a
// future isolated checkout. It contains neither ciphertext nor a credential.
type resolveEnvironmentReferenceRequest struct {
	RepoID    int64
	Reference *branchroom_module.EnvironmentReference
}

// environmentReferenceResolution contains only the values needed to request a
// separately authorized lease. It deliberately excludes encrypted payload fields.
type environmentReferenceResolution struct {
	ScopeID     int64
	RevisionKey string
}

// resolveEnvironmentReference validates a parsed safe tracked pointer, then resolves
// it to one active immutable revision. It does not authorize, decrypt,
// materialize, or audit access; a later lease request must independently do that.
func resolveEnvironmentReference(ctx context.Context, request resolveEnvironmentReferenceRequest) (environmentReferenceResolution, error) {
	if err := requireDevelopmentEnvironment(); err != nil {
		return environmentReferenceResolution{}, err
	}
	if request.RepoID <= 0 {
		return environmentReferenceResolution{}, util.NewInvalidArgumentErrorf("invalid Branchroom environment reference repository")
	}
	if err := request.Reference.Validate(); err != nil {
		return environmentReferenceResolution{}, err
	}

	return db.WithTx2(ctx, func(txCtx context.Context) (environmentReferenceResolution, error) {
		// This deliberate repository-row write serializes transfer and deletion. The
		// resolved identity is therefore a non-cacheable snapshot, not authorization.
		if err := lockRepositoryLifecycle(txCtx, request.RepoID); err != nil {
			return environmentReferenceResolution{}, err
		}
		scope, err := branchroom_model.GetScopeByRepoIDAndLowerNameForUpdate(txCtx, request.RepoID, request.Reference.Environment)
		if err != nil {
			if errors.Is(err, util.ErrNotExist) {
				return environmentReferenceResolution{}, util.NewNotExistErrorf("Branchroom environment reference does not exist")
			}
			return environmentReferenceResolution{}, err
		}
		if scope.RepoID != request.RepoID || scope.Kind != branchroom_model.ScopeKindEnvironment || scope.State != branchroom_model.ScopeStateActive || scope.Validate() != nil {
			return environmentReferenceResolution{}, util.NewNotExistErrorf("Branchroom environment reference does not exist")
		}

		revision, err := branchroom_model.GetEnvironmentRevisionByScopeIDAndRevisionForUpdate(txCtx, scope.ID, request.Reference.Revision)
		if err != nil {
			if errors.Is(err, util.ErrNotExist) {
				return environmentReferenceResolution{}, util.NewNotExistErrorf("Branchroom environment reference does not exist")
			}
			return environmentReferenceResolution{}, err
		}
		if revision.RepoID != request.RepoID || revision.ScopeID != scope.ID || revision.State != branchroom_model.EnvironmentRevisionStateActive || revision.Validate() != nil {
			return environmentReferenceResolution{}, util.NewNotExistErrorf("Branchroom environment reference does not exist")
		}

		return environmentReferenceResolution{
			ScopeID:     scope.ID,
			RevisionKey: revision.RevisionKey,
		}, nil
	})
}
