// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	repo_model "gitea.dev/models/repo"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/git"
	"gitea.dev/modules/log"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
)

// Audit reason codes recorded for owner-initiated workspace lifecycle operations.
const (
	reasonWorkspaceRetryRequested = "workspace_retry_requested"
	reasonWorkspaceRetired        = "workspace_retired"
	reasonWorkspacePurged         = "workspace_purged"
)

var (
	// ErrWorkspaceQuotaReached denies provisioning without revealing quota state,
	// surfacing like every other Branchroom not-exist denial.
	ErrWorkspaceQuotaReached = util.ErrorWrap(util.ErrNotExist, "Branchroom workspace does not exist")
	// ErrWorkspacePurgeStoreFailed leaves the mapping untouched when the filesystem
	// store could not be removed, so purge stays crash-safe by ordering.
	ErrWorkspacePurgeStoreFailed = errors.New("unable to remove Branchroom restricted store")
)

// RetireWorkspaceRequest contains owner-authorized facts for retiring one workspace.
type RetireWorkspaceRequest struct {
	RepoID    int64
	ScopeID   int64
	Actor     Actor
	Surface   branchroom_model.Surface
	RequestID string
}

// PurgeWorkspaceRequest contains owner-authorized facts for purging one workspace.
type PurgeWorkspaceRequest struct {
	RepoID    int64
	ScopeID   int64
	Actor     Actor
	Surface   branchroom_model.Surface
	RequestID string
}

// RetryWorkspaceProvisioningRequest contains owner-authorized facts for retrying one failed workspace.
type RetryWorkspaceProvisioningRequest struct {
	RepoID    int64
	ScopeID   int64
	Actor     Actor
	Surface   branchroom_model.Surface
	RequestID string
}

// RetireWorkspace marks a ready or failed workspace retired in the caller's
// transaction-free context; retirement is terminal but keeps the isolated store.
func RetireWorkspace(ctx context.Context, request RetireWorkspaceRequest) (*branchroom_model.Workspace, error) {
	if err := requireLifecycleFeatureAndRequest(request.RepoID, request.ScopeID, request.Actor, request.Surface); err != nil {
		return nil, err
	}
	return db.WithTx2(ctx, func(txCtx context.Context) (*branchroom_model.Workspace, error) {
		scope, err := requireActiveScopeTx(txCtx, request.RepoID, request.Actor, request.ScopeID, branchroom_model.ScopeKindRepository, "Branchroom workspace does not exist")
		if err != nil {
			return nil, err
		}
		workspace, err := branchroom_model.GetWorkspaceByScopeIDForUpdate(txCtx, scope.ID)
		if err != nil {
			return nil, err
		}
		if workspace.State != branchroom_model.WorkspaceStateReady && workspace.State != branchroom_model.WorkspaceStateFailed {
			return nil, ErrWorkspaceStateChanged
		}
		now := timeutil.TimeStampNow()
		if err := transitionLockedWorkspace(txCtx, scope, workspace, branchroom_model.WorkspaceStateRetired, "", now, request.Actor, request.Surface, request.RequestID); err != nil {
			return nil, err
		}
		workspace.State = branchroom_model.WorkspaceStateRetired
		workspace.RetiredUnix = now
		return workspace, nil
	})
}

// PurgeWorkspace permanently removes the isolated store and then marks the
// workspace purged. The filesystem removal happens first outside any database
// transaction and treats a missing store as success; only then does a locked
// compare-and-swap mark the mapping purged, so a crash between the two steps
// can always be repaired by running the purge again.
func PurgeWorkspace(ctx context.Context, request PurgeWorkspaceRequest) (*branchroom_model.Workspace, error) {
	if db.InTransaction(ctx) {
		return nil, ErrWorkspaceInTransaction
	}
	if err := requireLifecycleFeatureAndRequest(request.RepoID, request.ScopeID, request.Actor, request.Surface); err != nil {
		return nil, err
	}

	captured, err := capturePurgeableWorkspace(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := removeRestrictedStore(ctx, captured.StoreID); err != nil {
		return nil, err
	}

	purgedAt := timeutil.TimeStampNow()
	err = db.WithTx(ctx, func(txCtx context.Context) error {
		if err := lockRepositoryLifecycle(txCtx, request.RepoID); err != nil {
			return err
		}
		current, err := branchroom_model.GetWorkspaceByIDForUpdate(txCtx, captured.ID)
		if err != nil {
			return err
		}
		if current.RepoID != request.RepoID || current.ScopeID != captured.ScopeID {
			return ErrWorkspaceStateChanged
		}
		switch current.State {
		case branchroom_model.WorkspaceStateReady, branchroom_model.WorkspaceStateFailed, branchroom_model.WorkspaceStateRetired:
		default:
			// a retry moved the workspace back to provisioning after the store was removed
			return ErrWorkspaceStateChanged
		}
		scope, err := branchroom_model.GetScopeByID(txCtx, captured.ScopeID)
		if err != nil {
			return err
		}
		return transitionLockedWorkspace(txCtx, scope, current, branchroom_model.WorkspaceStatePurged, "", purgedAt, request.Actor, request.Surface, request.RequestID)
	})
	if err != nil {
		return nil, err
	}

	captured.State = branchroom_model.WorkspaceStatePurged
	captured.RetiredUnix = purgedAt
	return captured, nil
}

func capturePurgeableWorkspace(ctx context.Context, request PurgeWorkspaceRequest) (*branchroom_model.Workspace, error) {
	return db.WithTx2(ctx, func(txCtx context.Context) (*branchroom_model.Workspace, error) {
		scope, err := requireActiveScopeTx(txCtx, request.RepoID, request.Actor, request.ScopeID, branchroom_model.ScopeKindRepository, "Branchroom workspace does not exist")
		if err != nil {
			return nil, err
		}
		workspace, err := branchroom_model.GetWorkspaceByScopeIDForUpdate(txCtx, scope.ID)
		if err != nil {
			return nil, err
		}
		switch workspace.State {
		case branchroom_model.WorkspaceStateReady, branchroom_model.WorkspaceStateFailed, branchroom_model.WorkspaceStateRetired:
			return workspace, nil
		default:
			return nil, ErrWorkspaceStateChanged
		}
	})
}

func removeRestrictedStore(ctx context.Context, storeID string) error {
	restricted, err := branchroom_module.RestrictedRepository(storeID)
	if err != nil {
		return ErrWorkspacePurgeStoreFailed
	}
	exists, err := git.IsRepositoryExist(ctx, restricted)
	if err != nil {
		return ErrWorkspacePurgeStoreFailed
	}
	if exists {
		if err := git.DeleteRepository(ctx, restricted); err != nil {
			log.Error("Unable to remove Branchroom restricted store %s: %v", storeID, err)
			return ErrWorkspacePurgeStoreFailed
		}
	}
	hookDir := filepath.Join(setting.Branchroom.RestrictedRepoRootPath, ".branchroom-hooks", storeID)
	if err := os.RemoveAll(hookDir); err != nil {
		log.Error("Unable to remove Branchroom restricted hook directory %s: %v", hookDir, err)
		return ErrWorkspacePurgeStoreFailed
	}
	return nil
}

// RetryWorkspaceProvisioning moves one failed workspace back to provisioning and
// reruns the existing import primitive against its reserved mapping.
func RetryWorkspaceProvisioning(ctx context.Context, request RetryWorkspaceProvisioningRequest) (*branchroom_model.Workspace, error) {
	if db.InTransaction(ctx) {
		return nil, ErrWorkspaceInTransaction
	}
	if err := requireLifecycleFeatureAndRequest(request.RepoID, request.ScopeID, request.Actor, request.Surface); err != nil {
		return nil, err
	}

	owner, err := CanManageRepositoryScopes(ctx, request.RepoID, request.Actor)
	if err != nil {
		return nil, err
	} else if !owner {
		return nil, util.NewNotExistErrorf("Branchroom workspace does not exist")
	}
	repository, err := repo_model.GetRepositoryByID(ctx, request.RepoID)
	if err != nil {
		return nil, err
	}

	reserved, err := reserveWorkspaceRetry(ctx, request)
	if err != nil {
		return nil, err
	}
	return importReservedWorkspace(ctx, repository, reserved, CreateRepositoryWorkspaceRequest{
		RepoID:        request.RepoID,
		ScopeID:       request.ScopeID,
		Actor:         request.Actor,
		PublicBaseSHA: reserved.PublicBaseSHA,
		Surface:       request.Surface,
		RequestID:     request.RequestID,
	})
}

func reserveWorkspaceRetry(ctx context.Context, request RetryWorkspaceProvisioningRequest) (*branchroom_model.Workspace, error) {
	return db.WithTx2(ctx, func(txCtx context.Context) (*branchroom_model.Workspace, error) {
		if err := lockRepositoryLifecycle(txCtx, request.RepoID); err != nil {
			return nil, err
		}
		scope, err := lockActiveScopeForWorkspaceReservation(txCtx, request.RepoID, request.ScopeID)
		if err != nil {
			return nil, err
		}
		owner, err := CanManageRepositoryScopes(txCtx, request.RepoID, request.Actor)
		if err != nil {
			return nil, err
		} else if !owner {
			return nil, util.NewNotExistErrorf("Branchroom workspace does not exist")
		}
		workspace, err := branchroom_model.GetWorkspaceByScopeIDForUpdate(txCtx, scope.ID)
		if err != nil {
			return nil, err
		}
		if workspace.State != branchroom_model.WorkspaceStateFailed {
			return nil, ErrWorkspaceStateChanged
		}
		updated, err := branchroom_model.TransitionWorkspace(txCtx, workspace.ID, branchroom_model.WorkspaceStateFailed, branchroom_model.WorkspaceStateProvisioning, "", 0)
		if err != nil {
			return nil, err
		} else if !updated {
			return nil, ErrWorkspaceStateChanged
		}
		now := timeutil.TimeStampNow()
		if _, err := branchroom_model.RecordDecisionAndAudit(txCtx, scope, &branchroom_model.Decision{
			ScopeID:         scope.ID,
			RepoID:          scope.RepoID,
			ActorType:       request.Actor.Type,
			ActorID:         request.Actor.ID,
			Action:          branchroom_model.ActionManagePolicy,
			ResourceType:    branchroom_model.ResourceTypeWorkspace,
			ResourceID:      workspaceResourceID(workspace.WorkspaceKey),
			Surface:         request.Surface,
			Result:          branchroom_model.DecisionResultAllow,
			ReasonCode:      reasonWorkspaceRetryRequested,
			OrdinaryAllowed: true,
			RequestID:       request.RequestID,
			CreatedUnix:     now,
			ExpiresUnix:     now,
		}); err != nil {
			return nil, err
		}
		workspace.State = branchroom_model.WorkspaceStateProvisioning
		workspace.FailureCode = ""
		return workspace, nil
	})
}

func requireLifecycleFeatureAndRequest(repoID, scopeID int64, actor Actor, surface branchroom_model.Surface) error {
	if !setting.Branchroom.Enabled {
		return util.NewNotExistErrorf("Branchroom workspace does not exist")
	}
	if repoID <= 0 || scopeID <= 0 || actor.Type != branchroom_model.ActorTypeUser || actor.ID <= 0 || !branchroom_model.IsSurfaceValid(surface) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom workspace lifecycle request")
	}
	return nil
}

// transitionLockedWorkspace commits a compare-and-swap state change for a
// workspace already locked in this transaction and records its audit decision.
func transitionLockedWorkspace(ctx context.Context, scope *branchroom_model.Scope, workspace *branchroom_model.Workspace, to branchroom_model.WorkspaceState, failureCode string, retiredUnix timeutil.TimeStamp, actor Actor, surface branchroom_model.Surface, requestID string) error {
	updated, err := branchroom_model.TransitionWorkspace(ctx, workspace.ID, workspace.State, to, failureCode, retiredUnix)
	if err != nil {
		return err
	} else if !updated {
		return ErrWorkspaceStateChanged
	}
	now := timeutil.TimeStampNow()
	_, err = branchroom_model.RecordDecisionAndAudit(ctx, scope, &branchroom_model.Decision{
		ScopeID:         scope.ID,
		RepoID:          scope.RepoID,
		ActorType:       actor.Type,
		ActorID:         actor.ID,
		Action:          branchroom_model.ActionManagePolicy,
		ResourceType:    branchroom_model.ResourceTypeWorkspace,
		ResourceID:      workspaceResourceID(workspace.WorkspaceKey),
		Surface:         surface,
		Result:          branchroom_model.DecisionResultAllow,
		ReasonCode:      reasonForWorkspaceTransition(to),
		OrdinaryAllowed: true,
		RequestID:       requestID,
		CreatedUnix:     now,
		ExpiresUnix:     now,
	})
	return err
}

func reasonForWorkspaceTransition(to branchroom_model.WorkspaceState) string {
	switch to {
	case branchroom_model.WorkspaceStateRetired:
		return reasonWorkspaceRetired
	case branchroom_model.WorkspaceStatePurged:
		return reasonWorkspacePurged
	default:
		return ""
	}
}

// enforceWorkspaceOwnerQuota rejects new provisioning once the scope owner has
// reached the configured number of active workspaces across all repositories.
func enforceWorkspaceOwnerQuota(ctx context.Context, ownerID int64) error {
	limit := setting.Branchroom.MaxWorkspacesPerOwner
	if limit <= 0 {
		return nil
	}
	count, err := branchroom_model.CountOwnerActiveWorkspaces(ctx, ownerID)
	if err != nil {
		return err
	}
	if count >= int64(limit) {
		return ErrWorkspaceQuotaReached
	}
	return nil
}

// ReleaseRepositoryWorkspaceRetention deletes terminal (retired or purged)
// workspace mappings so permanent repository deletion can proceed, and refuses
// while any mapping still holds provisioning, ready, or failed state.
func ReleaseRepositoryWorkspaceRetention(ctx context.Context, repoID int64) error {
	if repoID <= 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom repository ID")
	}
	return db.WithTx(ctx, func(txCtx context.Context) error {
		if err := lockRepositoryLifecycle(txCtx, repoID); err != nil {
			return err
		}
		// These current writes make mappings committed while deletion waited on the
		// repository lock visible before the retention query below.
		if _, err := db.GetEngine(txCtx).Where("repo_id = ?", repoID).Cols("updated_unix").Update(&branchroom_model.Workspace{UpdatedUnix: timeutil.TimeStampNow()}); err != nil {
			return err
		}
		workspaces, err := branchroom_model.FindWorkspacesByRepoIDForUpdate(txCtx, repoID)
		if err != nil {
			return err
		}
		terminalIDs := make([]int64, 0, len(workspaces))
		for _, workspace := range workspaces {
			switch workspace.State {
			case branchroom_model.WorkspaceStateRetired, branchroom_model.WorkspaceStatePurged:
				terminalIDs = append(terminalIDs, workspace.ID)
			default:
				return ErrWorkspaceRetentionRequired
			}
		}
		if len(terminalIDs) == 0 {
			return nil
		}
		_, err = db.GetEngine(txCtx).In("id", terminalIDs).Delete(new(branchroom_model.Workspace))
		return err
	})
}
