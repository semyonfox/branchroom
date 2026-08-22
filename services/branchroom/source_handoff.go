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
)

// ListRepositorySourceHandoffsRequest identifies a signed-in direct user asking
// for the stable web handoff for one ordinary repository.
type ListRepositorySourceHandoffsRequest struct {
	RepoID int64
	UserID int64
}

// RepositorySourceHandoff contains only the metadata needed to render one
// authorized HTTP clone handoff. RouteKey is a locator, not a credential.
type RepositorySourceHandoff struct {
	ScopeName string
	RouteKey  string
}

// ListRepositorySourceHandoffs returns source handoffs only after fresh ordinary
// permission, discovery-grant, source-read-grant, scope, and workspace checks. It
// intentionally does not invoke DBEvaluator: rendering a stable page must not append
// a policy decision or audit event on every refresh. Git HTTP reauthorizes and audits
// actual source access at each protocol phase.
func ListRepositorySourceHandoffs(ctx context.Context, request ListRepositorySourceHandoffsRequest) ([]RepositorySourceHandoff, error) {
	if !setting.Branchroom.Enabled || setting.Repository.DisableHTTPGit || request.RepoID <= 0 || request.UserID <= 0 || db.InTransaction(ctx) {
		return nil, sourceHandoffNotFound()
	}

	handoffs, err := db.WithTx2(ctx, func(txCtx context.Context) ([]RepositorySourceHandoff, error) {
		if err := lockRepositoryLifecycleForRead(txCtx, request.RepoID); err != nil {
			if errors.Is(err, util.ErrNotExist) {
				return nil, sourceHandoffNotFound()
			}
			return nil, err
		}

		actor := Actor{Type: branchroom_model.ActorTypeUser, ID: request.UserID}
		ceiling, activeUser, err := resolveDirectUserRepositoryCeiling(txCtx, request.RepoID, actor)
		if err != nil {
			return nil, err
		}
		if !activeUser || !ceiling.CodeRead {
			return nil, sourceHandoffNotFound()
		}

		scopes, err := branchroom_model.FindScopesByRepoIDForUpdate(txCtx, request.RepoID)
		if err != nil {
			return nil, err
		}
		now := timeutil.TimeStampNow()
		handoffs := make([]RepositorySourceHandoff, 0)
		for _, scope := range scopes {
			if scope.Validate() != nil || scope.RepoID != request.RepoID || scope.Kind != branchroom_model.ScopeKindRepository || scope.State != branchroom_model.ScopeStateActive {
				continue
			}
			if _, err := branchroom_model.GetActiveDirectUserGrantAt(txCtx, scope.ID, request.UserID, branchroom_model.ActionDiscover, now); err != nil {
				if errors.Is(err, util.ErrNotExist) {
					continue
				}
				return nil, err
			}
			if _, err := branchroom_model.GetActiveDirectUserGrantAt(txCtx, scope.ID, request.UserID, branchroom_model.ActionReadSource, now); err != nil {
				if errors.Is(err, util.ErrNotExist) {
					continue
				}
				return nil, err
			}

			workspace, err := branchroom_model.GetWorkspaceByScopeIDForUpdate(txCtx, scope.ID)
			if err != nil {
				if errors.Is(err, util.ErrNotExist) {
					continue
				}
				return nil, err
			}
			if workspace.Validate() != nil || workspace.ScopeID != scope.ID || workspace.RepoID != request.RepoID || workspace.State != branchroom_model.WorkspaceStateReady {
				continue
			}
			handoffs = append(handoffs, RepositorySourceHandoff{
				ScopeName: scope.Name,
				RouteKey:  workspace.RouteKey,
			})
		}
		if len(handoffs) == 0 {
			return nil, sourceHandoffNotFound()
		}
		return handoffs, nil
	})
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			return nil, sourceHandoffNotFound()
		}
		return nil, err
	}
	return handoffs, nil
}

func sourceHandoffNotFound() error {
	return util.NewNotExistErrorf("Branchroom source handoff does not exist")
}
