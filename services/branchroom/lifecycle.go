// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"errors"
	"strconv"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
)

const ReasonRepositoryTransferred = "repository_transferred"

const ReasonRepositoryDeleted = "repository_deleted"

// ErrWorkspaceRetentionRequired prevents repository deletion from orphaning an
// isolated store before its owner has explicitly purged it.
var ErrWorkspaceRetentionRequired = errors.New("Branchroom restricted workspace retention is required before deleting this repository")

// RetireRepositoryScopesForTransfer retires a repository's scopes and revokes their
// active grants in the caller's transaction before ownership changes take effect.
func RetireRepositoryScopesForTransfer(ctx context.Context, repoID, actorID int64) error {
	if repoID <= 0 || actorID <= 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom repository transfer")
	}
	if err := lockRepositoryLifecycle(ctx, repoID); err != nil {
		return err
	}
	return retireRepositoryScopes(ctx, repoID, Actor{Type: branchroom_model.ActorTypeUser, ID: actorID}, ReasonRepositoryTransferred, true)
}

// RetainRepositoryPolicyForDeletion runs inside permanent repository deletion. It refuses
// while a workspace store is unpurged, then retires the scopes but keeps scopes, grants,
// decisions and the audit chain so history outlives the repository.
func RetainRepositoryPolicyForDeletion(ctx context.Context, repoID int64) error {
	if repoID <= 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom repository ID")
	}
	if err := lockRepositoryLifecycle(ctx, repoID); err != nil {
		return err
	}
	// a current write makes mappings committed while deletion waited on the lock visible below
	if _, err := db.GetEngine(ctx).Where("repo_id = ?", repoID).NoAutoTime().SetExpr("updated_unix", "updated_unix").Update(new(branchroom_model.Workspace)); err != nil {
		return err
	}
	workspaces, err := branchroom_model.FindWorkspacesByRepoIDForUpdate(ctx, repoID)
	if err != nil {
		return err
	}
	if len(workspaces) > 0 {
		return ErrWorkspaceRetentionRequired
	}
	if err := retireRepositoryScopes(ctx, repoID, Actor{Type: branchroom_model.ActorTypeSystem}, ReasonRepositoryDeleted, false); err != nil {
		return err
	}
	engine := db.GetEngine(ctx)
	if _, err := engine.Where("repo_id = ?", repoID).Delete(new(branchroom_model.ReceiveSession)); err != nil {
		return err
	}
	// rows from the removed development-only environment store hold only unreadable ciphertext
	for _, table := range []string{"branchroom_environment_lease", "branchroom_environment_revision"} {
		if exists, err := engine.IsTableExist(table); err != nil {
			return err
		} else if !exists {
			continue
		}
		if _, err := engine.Exec("DELETE FROM "+table+" WHERE scope_id IN (SELECT id FROM branchroom_scope WHERE repo_id = ?)", repoID); err != nil {
			return err
		}
	}
	return nil
}

func retireRepositoryScopes(ctx context.Context, repoID int64, actor Actor, reason string, retireWorkspaces bool) error {
	now := timeutil.TimeStampNow()
	// a current write makes rows committed while the caller waited on the lock visible below
	if _, err := db.GetEngine(ctx).Where("repo_id = ?", repoID).Cols("updated_unix").Update(&branchroom_model.Scope{UpdatedUnix: now}); err != nil {
		return err
	}
	scopes, err := branchroom_model.FindScopesByRepoIDForUpdate(ctx, repoID)
	if err != nil {
		return err
	}
	for _, scope := range scopes {
		changed := false
		if scope.State != branchroom_model.ScopeStateRetired {
			if _, err := db.GetEngine(ctx).ID(scope.ID).Cols("state").Update(&branchroom_model.Scope{State: branchroom_model.ScopeStateRetired}); err != nil {
				return err
			}
			// a retired scope already denies every grant, deletion has no user to record as revoker
			if actor.Type == branchroom_model.ActorTypeUser {
				if _, err := db.GetEngine(ctx).Where("scope_id = ? AND revoked_unix = 0", scope.ID).Cols("revoked_unix", "revoked_by_id").Update(&branchroom_model.Grant{RevokedUnix: now, RevokedByID: actor.ID}); err != nil {
					return err
				}
			}
			changed = true
		}
		if retireWorkspaces {
			workspace, err := branchroom_model.GetWorkspaceByScopeIDForUpdate(ctx, scope.ID)
			if err == nil && workspace.State != branchroom_model.WorkspaceStateRetired && workspace.State != branchroom_model.WorkspaceStatePurged {
				updated, err := branchroom_model.TransitionWorkspace(ctx, workspace.ID, workspace.State, branchroom_model.WorkspaceStateRetired, "", now)
				if err != nil {
					return err
				}
				changed = changed || updated
			} else if err != nil && !errors.Is(err, util.ErrNotExist) {
				return err
			}
		}
		if !changed {
			continue
		}
		if _, err := branchroom_model.RecordDecisionAndAudit(ctx, scope, &branchroom_model.Decision{
			ScopeID:         scope.ID,
			RepoID:          repoID,
			ActorType:       actor.Type,
			ActorID:         actor.ID,
			Action:          branchroom_model.ActionManagePolicy,
			ResourceType:    branchroom_model.ResourceTypeScope,
			ResourceID:      "scope/" + strconv.FormatInt(scope.ID, 10),
			Surface:         branchroom_model.SurfaceSystem,
			Result:          branchroom_model.DecisionResultAllow,
			ReasonCode:      reason,
			OrdinaryAllowed: true,
			CreatedUnix:     now,
			ExpiresUnix:     now,
		}); err != nil {
			return err
		}
	}
	return nil
}
