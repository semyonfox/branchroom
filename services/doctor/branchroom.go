// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package doctor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/git"
	"gitea.dev/modules/log"
	"gitea.dev/modules/setting"

	"github.com/google/uuid"
)

// checkBranchroom reports broken audit chains, mappings without a usable store or hook, and
// stores without a mapping. Autofix only reinstalls hooks: it never repairs audit rows or deletes
// a store, since either would destroy evidence or restricted source.
func checkBranchroom(ctx context.Context, logger log.Logger, autofix bool) error {
	problems := 0
	report := func(format string, args ...any) {
		problems++
		logger.Warn(format, args...)
	}

	scopes := make([]*branchroom_model.Scope, 0)
	if err := db.GetEngine(ctx).Asc("id").Find(&scopes); err != nil {
		return err
	}
	for _, scope := range scopes {
		if err := branchroom_model.VerifyScopeAuditChain(ctx, scope); err != nil {
			report("Branchroom scope %d audit chain: %v", scope.ID, err)
		}
	}

	if !setting.Branchroom.Enabled {
		logger.Info("Branchroom is disabled, restricted stores were not checked")
	} else if err := checkBranchroomStores(ctx, logger, autofix, report); err != nil {
		return err
	}

	if problems > 0 {
		return fmt.Errorf("found %d Branchroom problems", problems)
	}
	logger.Info("Checked %d Branchroom scopes", len(scopes))
	return nil
}

func checkBranchroomStores(ctx context.Context, logger log.Logger, autofix bool, report func(string, ...any)) error {
	root := setting.Branchroom.RestrictedRepoRootPath
	workspaces := make([]*branchroom_model.Workspace, 0)
	if err := db.GetEngine(ctx).Asc("id").Find(&workspaces); err != nil {
		return err
	}
	mapped := make(map[string]bool, len(workspaces))
	for _, workspace := range workspaces {
		if workspace.State == branchroom_model.WorkspaceStatePurged {
			continue
		}
		mapped[workspace.StoreID] = true
		if workspace.State != branchroom_model.WorkspaceStateReady && workspace.State != branchroom_model.WorkspaceStateRetired {
			continue
		}
		restricted, err := branchroom_module.RestrictedRepository(workspace.StoreID)
		if err != nil {
			report("Branchroom workspace %d store is unavailable: %v", workspace.ID, err)
			continue
		}
		if exists, err := git.IsRepositoryExist(ctx, restricted); err != nil || !exists {
			report("Branchroom workspace %d (%s) has no store on disk", workspace.ID, workspace.State)
			continue
		}
		if workspace.State != branchroom_model.WorkspaceStateReady {
			continue
		}
		if err := branchroom_module.VerifyRestrictedStoreHook(ctx, root, workspace.StoreID); err == nil {
			continue
		} else if !autofix {
			report("Branchroom workspace %d receive hook fails verification: %v", workspace.ID, err)
			continue
		}
		if err := reinstallBranchroomHook(ctx, restricted, root, workspace.StoreID); err != nil {
			report("Branchroom workspace %d receive hook could not be reinstalled: %v", workspace.ID, err)
			continue
		}
		logger.Info("Reinstalled the receive hook of Branchroom workspace %d", workspace.ID)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		storeID, isStore := strings.CutSuffix(entry.Name(), ".git")
		if !isStore || !entry.IsDir() {
			continue
		}
		if id, err := uuid.Parse(storeID); err != nil || id.String() != storeID {
			continue
		}
		if !mapped[storeID] {
			report("Branchroom store %s has no workspace mapping and must be removed by hand", entry.Name())
		}
	}
	return nil
}

func reinstallBranchroomHook(ctx context.Context, restricted git.RepositoryFacade, root, storeID string) error {
	hookDir, err := branchroom_module.ReinstallRestrictedStoreHook(root, storeID)
	if err != nil {
		return err
	}
	if err := git.ManagedConfigSetAndVerify(ctx, restricted, "core.hooksPath", hookDir); err != nil {
		return err
	}
	if err := branchroom_module.VerifyRestrictedStoreHook(ctx, root, storeID); err != nil {
		return errors.Join(errors.New("still failing after reinstall"), err)
	}
	return nil
}

func init() {
	Register(&Check{
		Title:     "Check Branchroom audit chains, restricted stores and receive hooks",
		Name:      "branchroom",
		IsDefault: false,
		Run:       checkBranchroom,
		Priority:  1,
	})
}
