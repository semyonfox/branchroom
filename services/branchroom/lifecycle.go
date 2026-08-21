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

var (
	// ErrWorkspaceRetentionRequired prevents repository deletion from orphaning an
	// isolated store before the retention collector has explicitly removed it.
	ErrWorkspaceRetentionRequired = errors.New("Branchroom restricted workspace retention is required before deleting this repository")
	// ErrEnvironmentRetentionRequired prevents deletion from silently discarding
	// encrypted environment revisions before a retention collector exists.
	ErrEnvironmentRetentionRequired = errors.New("Branchroom environment retention is required before deleting this repository")
)

// RetireRepositoryScopesForTransfer retires a repository's scopes and revokes their
// active grants in the caller's transaction before ownership changes take effect.
func RetireRepositoryScopesForTransfer(ctx context.Context, repoID, actorID int64) error {
	if repoID <= 0 || actorID <= 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom repository transfer")
	}

	if err := lockRepositoryLifecycle(ctx, repoID); err != nil {
		return err
	}
	now := timeutil.TimeStampNow()
	// These current writes make mappings committed while a transfer waited on the
	// repository lock visible before the lifecycle queries below.
	if _, err := db.GetEngine(ctx).Where("repo_id = ?", repoID).Cols("updated_unix").Update(&branchroom_model.Scope{UpdatedUnix: now}); err != nil {
		return err
	}
	if _, err := db.GetEngine(ctx).Where("repo_id = ?", repoID).Cols("updated_unix").Update(&branchroom_model.Workspace{UpdatedUnix: now}); err != nil {
		return err
	}
	if _, err := db.GetEngine(ctx).Where("repo_id = ?", repoID).Cols("updated_unix").Update(&branchroom_model.EnvironmentRevision{UpdatedUnix: now}); err != nil {
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
			if _, err := db.GetEngine(ctx).Where("scope_id = ? AND revoked_unix = 0", scope.ID).Cols("revoked_unix", "revoked_by_id").Update(&branchroom_model.Grant{RevokedUnix: now, RevokedByID: actorID}); err != nil {
				return err
			}
			changed = true
		}

		workspace, err := branchroom_model.GetWorkspaceByScopeIDForUpdate(ctx, scope.ID)
		if err == nil && workspace.State != branchroom_model.WorkspaceStateRetired {
			updated, err := branchroom_model.TransitionWorkspace(ctx, workspace.ID, workspace.State, branchroom_model.WorkspaceStateRetired, "", now)
			if err != nil {
				return err
			}
			changed = changed || updated
		} else if err != nil && !errors.Is(err, util.ErrNotExist) {
			return err
		}

		revisions, err := branchroom_model.FindEnvironmentRevisionsByScopeIDForUpdate(ctx, scope.ID)
		if err != nil {
			return err
		}
		for _, revision := range revisions {
			if revision.State != branchroom_model.EnvironmentRevisionStateActive {
				continue
			}
			retired, err := branchroom_model.RetireEnvironmentRevision(ctx, revision.ID, now)
			if err != nil {
				return err
			}
			changed = changed || retired
		}
		leases, err := branchroom_model.FindEnvironmentLeasesByScopeIDForUpdate(ctx, scope.ID)
		if err != nil {
			return err
		}
		for _, lease := range leases {
			if lease.State != branchroom_model.EnvironmentLeaseStateIssued {
				continue
			}
			revoked, err := branchroom_model.RevokeEnvironmentLease(ctx, lease.ID)
			if err != nil {
				return err
			}
			changed = changed || revoked
		}
		if !changed {
			continue
		}
		if _, err := branchroom_model.RecordDecisionAndAudit(ctx, scope, &branchroom_model.Decision{
			ScopeID:         scope.ID,
			RepoID:          repoID,
			ActorType:       branchroom_model.ActorTypeUser,
			ActorID:         actorID,
			Action:          branchroom_model.ActionManagePolicy,
			ResourceType:    branchroom_model.ResourceTypeScope,
			ResourceID:      "scope/" + strconv.FormatInt(scope.ID, 10),
			Surface:         branchroom_model.SurfaceSystem,
			Result:          branchroom_model.DecisionResultAllow,
			ReasonCode:      ReasonRepositoryTransferred,
			OrdinaryAllowed: true,
			CreatedUnix:     now,
			ExpiresUnix:     now,
		}); err != nil {
			return err
		}
	}
	return nil
}

// PurgeRepositoryPolicy removes policy records as part of permanent repository deletion.
func PurgeRepositoryPolicy(ctx context.Context, repoID int64) error {
	if repoID <= 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom repository ID")
	}

	if err := lockRepositoryLifecycle(ctx, repoID); err != nil {
		return err
	}
	// These current writes make mappings committed while deletion waited on the
	// repository lock visible before the workspace check below.
	if _, err := db.GetEngine(ctx).Where("repo_id = ?", repoID).Cols("updated_unix").Update(&branchroom_model.Scope{UpdatedUnix: timeutil.TimeStampNow()}); err != nil {
		return err
	}
	if _, err := db.GetEngine(ctx).Where("repo_id = ?", repoID).Cols("updated_unix").Update(&branchroom_model.Workspace{UpdatedUnix: timeutil.TimeStampNow()}); err != nil {
		return err
	}
	if _, err := db.GetEngine(ctx).Where("repo_id = ?", repoID).Cols("updated_unix").Update(&branchroom_model.EnvironmentRevision{UpdatedUnix: timeutil.TimeStampNow()}); err != nil {
		return err
	}

	workspaces, err := branchroom_model.FindWorkspacesByRepoIDForUpdate(ctx, repoID)
	if err != nil {
		return err
	}
	if len(workspaces) > 0 {
		return ErrWorkspaceRetentionRequired
	}
	environmentRevisions, err := branchroom_model.FindEnvironmentRevisionsByRepoIDForUpdate(ctx, repoID)
	if err != nil {
		return err
	}
	if len(environmentRevisions) > 0 {
		return ErrEnvironmentRetentionRequired
	}

	scopes, err := branchroom_model.FindScopesByRepoIDForUpdate(ctx, repoID)
	if err != nil {
		return err
	}
	scopeIDs := make([]int64, 0, len(scopes))
	for _, scope := range scopes {
		scopeIDs = append(scopeIDs, scope.ID)
	}

	engine := db.GetEngine(ctx)
	if _, err := engine.Where("repo_id = ?", repoID).Delete(new(branchroom_model.AuditEvent)); err != nil {
		return err
	}
	if _, err := engine.Where("repo_id = ?", repoID).Delete(new(branchroom_model.Decision)); err != nil {
		return err
	}
	if len(scopeIDs) == 0 {
		return nil
	}
	if _, err := engine.In("scope_id", scopeIDs).Delete(new(branchroom_model.Grant)); err != nil {
		return err
	}
	if _, err := engine.In("id", scopeIDs).Delete(new(branchroom_model.Scope)); err != nil {
		return err
	}
	return nil
}
