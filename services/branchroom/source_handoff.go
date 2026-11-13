// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"errors"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	organization_model "gitea.dev/models/organization"
	repo_model "gitea.dev/models/repo"
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
		repository, err := repo_model.GetRepositoryByID(txCtx, request.RepoID)
		if err != nil {
			return nil, err
		}
		now := timeutil.TimeStampNow()
		handoffs := make([]RepositorySourceHandoff, 0)
		for _, scope := range scopes {
			if scope.Validate() != nil || scope.RepoID != request.RepoID || scope.Kind != branchroom_model.ScopeKindRepository || scope.State != branchroom_model.ScopeStateActive {
				continue
			}
			authorized, err := userHasSourceHandoffAuthorization(txCtx, repository.OwnerID, scope.ID, request.UserID, now)
			if err != nil {
				return nil, err
			}
			if !authorized {
				continue
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

func userHasSourceHandoffAuthorization(ctx context.Context, orgID, scopeID, userID int64, now timeutil.TimeStamp) (bool, error) {
	for _, action := range []branchroom_model.Action{branchroom_model.ActionDiscover, branchroom_model.ActionReadSource} {
		grants, err := branchroom_model.FindActiveGrantsAt(ctx, scopeID, action, now)
		if err != nil {
			return false, err
		}
		allowed := false
		for _, grant := range grants {
			if grant.SubjectType == branchroom_model.SubjectTypeUser && grant.SubjectID == userID {
				allowed = true
				break
			}
			if grant.SubjectType == branchroom_model.SubjectTypeTeam {
				member, err := organization_model.IsTeamMember(ctx, orgID, grant.SubjectID, userID)
				if err != nil {
					return false, err
				}
				if member {
					allowed = true
					break
				}
			}
		}
		if !allowed {
			return false, nil
		}
	}
	return true, nil
}

// CanDiscoverRepository checks discovery without recording a page-view decision.
func CanDiscoverRepository(ctx context.Context, repoID int64, actor Actor) (bool, error) {
	if !setting.Branchroom.Enabled {
		return false, nil
	}
	ceiling, active, err := resolveDirectUserRepositoryCeiling(ctx, repoID, actor)
	if err != nil || !active || !ceiling.CodeRead {
		return false, err
	}
	scopes, err := branchroom_model.FindScopesByRepoID(ctx, repoID)
	if err != nil {
		return false, err
	}
	for _, scope := range scopes {
		if scope.Kind != branchroom_model.ScopeKindRepository || scope.State != branchroom_model.ScopeStateActive {
			continue
		}
		grant, _, err := findMatchingGrant(ctx, scope, Request{Actor: actor, Action: branchroom_model.ActionDiscover}, timeutil.TimeStampNow())
		if err != nil {
			return false, err
		}
		if grant != nil {
			return true, nil
		}
	}
	return false, nil
}
