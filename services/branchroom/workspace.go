// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"errors"
	"time"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	repo_model "gitea.dev/models/repo"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/git"
	"gitea.dev/modules/git/gitrepo"
	"gitea.dev/modules/log"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/google/uuid"
)

const workspaceProvisioningWindow = time.Hour

var (
	// ErrWorkspaceAlreadyExists means a scope already owns an isolated workspace mapping.
	ErrWorkspaceAlreadyExists = errors.New("Branchroom workspace already exists")
	// ErrWorkspaceProvisioning means the store was not made ready and remains non-routable.
	ErrWorkspaceProvisioning = errors.New("unable to create restricted workspace")
	// ErrWorkspaceStateChanged means a concurrent lifecycle operation made the workspace non-routable.
	ErrWorkspaceStateChanged = errors.New("Branchroom workspace state changed")
	// ErrWorkspaceInTransaction prevents a durable physical import from being coupled to a caller transaction.
	ErrWorkspaceInTransaction = errors.New("Branchroom workspace creation cannot run in a database transaction")
)

// CreateRepositoryWorkspaceRequest contains owner-authorized facts for an isolated store import.
// PublicBaseSHA must be a full commit ID in the repository's configured object format.
type CreateRepositoryWorkspaceRequest struct {
	RepoID        int64
	ScopeID       int64
	Actor         Actor
	PublicBaseSHA string
	Surface       branchroom_model.Surface
	RequestID     string
}

// CreateRepositoryWorkspace reserves an opaque mapping, imports the public base into
// an isolated store, verifies it, then makes the mapping ready. It creates no URL or
// Git transport route.
func CreateRepositoryWorkspace(ctx context.Context, request CreateRepositoryWorkspaceRequest) (*branchroom_model.Workspace, error) {
	if db.InTransaction(ctx) {
		return nil, ErrWorkspaceInTransaction
	}
	if !setting.Branchroom.Enabled {
		return nil, util.NewNotExistErrorf("Branchroom workspace does not exist")
	}
	if request.RepoID <= 0 || request.ScopeID <= 0 || request.Actor.Type != branchroom_model.ActorTypeUser || request.Actor.ID <= 0 || !branchroom_model.IsSurfaceValid(request.Surface) {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom workspace creation request")
	}

	owner, err := CanManageRepositoryScopes(ctx, request.RepoID, request.Actor)
	if err != nil {
		return nil, err
	}
	if !owner {
		return nil, util.NewNotExistErrorf("Branchroom workspace does not exist")
	}

	repository, err := repo_model.GetRepositoryByID(ctx, request.RepoID)
	if err != nil {
		return nil, err
	}
	if err := verifyPublicBase(ctx, repository, request.PublicBaseSHA); err != nil {
		return nil, err
	}

	workspace, err := reserveRepositoryWorkspace(ctx, request, repository.ObjectFormatName)
	if err != nil {
		return nil, err
	}
	return importReservedWorkspace(ctx, repository, workspace, request)
}

// importReservedWorkspace is the durable physical import for an already-reserved
// provisioning workspace: it fills the isolated store, installs transport guards,
// then marks the mapping ready or failed.
func importReservedWorkspace(ctx context.Context, repository *repo_model.Repository, workspace *branchroom_model.Workspace, request CreateRepositoryWorkspaceRequest) (*branchroom_model.Workspace, error) {
	importCtx, cancel := context.WithDeadline(ctx, workspace.UpdatedUnix.AsTime().Add(workspaceProvisioningWindow))
	defer cancel()

	restricted, err := branchroom_module.RestrictedRepository(workspace.StoreID)
	if err != nil {
		markWorkspaceProvisioningFailed(ctx, workspace, request, reasonWorkspaceStoreUnavailable)
		return nil, ErrWorkspaceProvisioning
	}
	if err := branchroom_module.ImportPublicBase(importCtx, repository.CodeStorageRepo(), restricted); err != nil {
		removeFailedRestrictedStore(ctx, restricted)
		markWorkspaceProvisioningFailed(ctx, workspace, request, reasonWorkspaceImportFailed)
		return nil, ErrWorkspaceProvisioning
	}
	if err := branchroom_module.NormalizeRestrictedStorePermissions(setting.Branchroom.RestrictedRepoRootPath, workspace.StoreID); err != nil {
		removeFailedRestrictedStore(ctx, restricted)
		markWorkspaceProvisioningFailed(ctx, workspace, request, reasonWorkspaceStorePermissionsFailed)
		return nil, ErrWorkspaceProvisioning
	}
	if err := verifyImportedBase(importCtx, restricted, workspace.PublicBaseSHA, workspace.ObjectFormatName); err != nil {
		removeFailedRestrictedStore(ctx, restricted)
		markWorkspaceProvisioningFailed(ctx, workspace, request, reasonWorkspaceImportVerifyFailed)
		return nil, ErrWorkspaceProvisioning
	}

	hookDir, err := branchroom_module.InstallRestrictedStoreHook(setting.Branchroom.RestrictedRepoRootPath, workspace.StoreID)
	if err != nil {
		removeFailedRestrictedStore(ctx, restricted)
		markWorkspaceProvisioningFailed(ctx, workspace, request, "receive_hook_install_failed")
		return nil, ErrWorkspaceProvisioning
	}
	if err := git.ManagedConfigSetAndVerify(importCtx, restricted, "core.hooksPath", hookDir); err != nil {
		removeFailedRestrictedStore(ctx, restricted)
		markWorkspaceProvisioningFailed(ctx, workspace, request, "receive_hook_config_failed")
		return nil, ErrWorkspaceProvisioning
	}

	ready, err := markWorkspaceReady(importCtx, workspace, request)
	if err != nil {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		removeFailedRestrictedStore(cleanupCtx, restricted)
		markWorkspaceProvisioningFailed(cleanupCtx, workspace, request, "workspace_ready_failed")
	}
	return ready, err
}

func removeFailedRestrictedStore(ctx context.Context, restricted gitrepo.RepositoryFacade) {
	exists, err := git.IsRepositoryExist(ctx, restricted)
	if err != nil || !exists {
		return
	}
	if err := git.DeleteRepository(ctx, restricted); err != nil {
		log.Error("Unable to remove failed Branchroom restricted store")
	}
}

func reserveRepositoryWorkspace(ctx context.Context, request CreateRepositoryWorkspaceRequest, objectFormatName string) (*branchroom_model.Workspace, error) {
	return db.WithTx2(ctx, func(txCtx context.Context) (*branchroom_model.Workspace, error) {
		if err := lockRepositoryLifecycle(txCtx, request.RepoID); err != nil {
			return nil, err
		}
		scope, err := lockActiveScopeForWorkspaceReservation(txCtx, request.RepoID, request.ScopeID)
		if err != nil {
			return nil, err
		}
		if err := lockWorkspaceOwner(txCtx, scope.OwnerID); err != nil {
			return nil, err
		}

		owner, err := CanManageRepositoryScopes(txCtx, request.RepoID, request.Actor)
		if err != nil {
			return nil, err
		}
		if !owner {
			return nil, util.NewNotExistErrorf("Branchroom workspace does not exist")
		}
		if _, err := branchroom_model.GetWorkspaceByScopeIDForUpdate(txCtx, scope.ID); err == nil {
			return nil, ErrWorkspaceAlreadyExists
		} else if !errors.Is(err, util.ErrNotExist) {
			return nil, err
		}
		if err := enforceWorkspaceOwnerQuota(txCtx, scope.OwnerID); err != nil {
			return nil, err
		}

		workspace := &branchroom_model.Workspace{
			WorkspaceKey:     uuid.NewString(),
			ScopeID:          scope.ID,
			RepoID:           request.RepoID,
			StoreID:          uuid.NewString(),
			PublicBaseSHA:    request.PublicBaseSHA,
			ObjectFormatName: objectFormatName,
			State:            branchroom_model.WorkspaceStateProvisioning,
			CreatedByID:      request.Actor.ID,
		}
		if err := branchroom_model.InsertWorkspace(txCtx, workspace); err != nil {
			return nil, err
		}
		return workspace, nil
	})
}

// lockActiveScopeForWorkspaceReservation serializes a new mapping with repository
// transfer and deletion, both of which write the same scope row in their transaction.
func lockActiveScopeForWorkspaceReservation(ctx context.Context, repoID, scopeID int64) (*branchroom_model.Scope, error) {
	_, err := db.GetEngine(ctx).ID(scopeID).
		Where("repo_id = ? AND state = ?", repoID, branchroom_model.ScopeStateActive).
		Cols("updated_unix").
		Update(&branchroom_model.Scope{UpdatedUnix: timeutil.TimeStampNow()})
	if err != nil {
		return nil, err
	}
	scope, err := branchroom_model.GetScopeByIDForUpdate(ctx, scopeID)
	if err != nil {
		return nil, err
	}
	if scope.RepoID != repoID || scope.Kind != branchroom_model.ScopeKindRepository || scope.State != branchroom_model.ScopeStateActive {
		return nil, util.NewNotExistErrorf("Branchroom workspace does not exist")
	}
	return scope, nil
}

func verifyPublicBase(ctx context.Context, repository *repo_model.Repository, publicBaseSHA string) error {
	if repository == nil || !workspaceBaseMatchesRepositoryObjectFormat(publicBaseSHA, repository.ObjectFormatName) {
		return util.NewInvalidArgumentErrorf("Branchroom public base must be a full commit ID in the repository object format")
	}
	return verifyBaseCommit(ctx, repository.CodeStorageRepo(), publicBaseSHA, repository.ObjectFormatName)
}

func verifyImportedBase(ctx context.Context, restricted gitrepo.RepositoryFacade, publicBaseSHA, objectFormatName string) error {
	return verifyBaseCommit(ctx, restricted, publicBaseSHA, objectFormatName)
}

func verifyBaseCommit(ctx context.Context, repository gitrepo.RepositoryFacade, publicBaseSHA, objectFormatName string) error {
	if !workspaceBaseMatchesRepositoryObjectFormat(publicBaseSHA, objectFormatName) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom public base")
	}
	gitRepository, err := git.OpenRepository(ctx, repository)
	if err != nil {
		return ErrWorkspaceProvisioning
	}
	defer gitRepository.Close()

	objectFormat, err := gitRepository.GetObjectFormat(ctx)
	if err != nil || objectFormat.Name() != objectFormatName {
		return ErrWorkspaceProvisioning
	}
	commit, err := gitRepository.GetCommit(ctx, publicBaseSHA)
	if err != nil || commit.ID.String() != publicBaseSHA {
		return util.NewInvalidArgumentErrorf("Branchroom public base must name a commit in the repository")
	}
	return nil
}

func workspaceBaseMatchesRepositoryObjectFormat(publicBaseSHA, objectFormatName string) bool {
	objectFormat := git.ObjectFormatFromName(objectFormatName)
	return git.IsValidObjectFormat(objectFormatName) && git.IsStringValidObjectID(objectFormat, publicBaseSHA)
}

func markWorkspaceReady(ctx context.Context, workspace *branchroom_model.Workspace, request CreateRepositoryWorkspaceRequest) (*branchroom_model.Workspace, error) {
	return db.WithTx2(ctx, func(txCtx context.Context) (*branchroom_model.Workspace, error) {
		if err := lockRepositoryLifecycle(txCtx, request.RepoID); err != nil {
			return nil, err
		}
		scope, err := branchroom_model.GetScopeByIDForUpdate(txCtx, workspace.ScopeID)
		if err != nil {
			return nil, err
		}
		current, err := branchroom_model.GetWorkspaceByIDForUpdate(txCtx, workspace.ID)
		if err != nil {
			return nil, err
		}
		if current.RepoID != request.RepoID || current.StoreID != workspace.StoreID || scope.RepoID != request.RepoID || scope.State != branchroom_model.ScopeStateActive {
			return nil, ErrWorkspaceStateChanged
		}
		updated, err := branchroom_model.TransitionWorkspace(txCtx, current.ID, branchroom_model.WorkspaceStateProvisioning, branchroom_model.WorkspaceStateReady, "", 0)
		if err != nil {
			return nil, err
		}
		if !updated {
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
			ResourceID:      workspaceResourceID(current.WorkspaceKey),
			Surface:         request.Surface,
			Result:          branchroom_model.DecisionResultAllow,
			ReasonCode:      reasonWorkspaceCreated,
			OrdinaryAllowed: true,
			RequestID:       request.RequestID,
			ExpiresUnix:     now,
			CreatedUnix:     now,
		}); err != nil {
			return nil, err
		}
		current.State = branchroom_model.WorkspaceStateReady
		return current, nil
	})
}

func markWorkspaceProvisioningFailed(ctx context.Context, workspace *branchroom_model.Workspace, request CreateRepositoryWorkspaceRequest, reasonCode string) {
	if err := db.WithTx(ctx, func(txCtx context.Context) error {
		if err := lockRepositoryLifecycle(txCtx, request.RepoID); err != nil {
			return err
		}
		scope, err := branchroom_model.GetScopeByIDForUpdate(txCtx, workspace.ScopeID)
		if err != nil {
			return err
		}
		current, err := branchroom_model.GetWorkspaceByIDForUpdate(txCtx, workspace.ID)
		if err != nil {
			return err
		}
		if current.ScopeID != scope.ID || current.RepoID != request.RepoID || scope.RepoID != request.RepoID {
			return ErrWorkspaceStateChanged
		}
		if current.State != branchroom_model.WorkspaceStateProvisioning || current.StoreID != workspace.StoreID {
			return nil
		}
		updated, err := branchroom_model.TransitionWorkspace(txCtx, current.ID, branchroom_model.WorkspaceStateProvisioning, branchroom_model.WorkspaceStateFailed, reasonCode, 0)
		if err != nil {
			return err
		}
		if !updated {
			return nil
		}
		now := timeutil.TimeStampNow()
		_, err = branchroom_model.RecordDecisionAndAudit(txCtx, scope, &branchroom_model.Decision{
			ScopeID:         scope.ID,
			RepoID:          scope.RepoID,
			ActorType:       request.Actor.Type,
			ActorID:         request.Actor.ID,
			Action:          branchroom_model.ActionManagePolicy,
			ResourceType:    branchroom_model.ResourceTypeWorkspace,
			ResourceID:      workspaceResourceID(current.WorkspaceKey),
			Surface:         request.Surface,
			Result:          branchroom_model.DecisionResultDeny,
			ReasonCode:      reasonCode,
			OrdinaryAllowed: true,
			RequestID:       request.RequestID,
			ExpiresUnix:     now,
			CreatedUnix:     now,
		})
		return err
	}); err != nil {
		log.Error("Unable to record failed Branchroom workspace provisioning: %v", err)
	}
}

func workspaceResourceID(workspaceKey string) string {
	return "workspace/" + workspaceKey
}

func WorkspaceProvisioningAbandoned(workspace *branchroom_model.Workspace) bool {
	return workspace.State == branchroom_model.WorkspaceStateProvisioning && workspace.UpdatedUnix.AsTime().Add(workspaceProvisioningWindow).Before(time.Now())
}
