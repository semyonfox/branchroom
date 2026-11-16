// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

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
	"gitea.dev/modules/setting"

	"github.com/google/uuid"
)

// IntegrityReport lists what CheckIntegrity found, phrased for an operator.
type IntegrityReport struct {
	Scopes   int
	Problems []string
	Repaired []string
}

// CheckIntegrity verifies every stored audit chain and, when Branchroom is enabled, that each
// mapping has its store and a verified receive hook and that no store lacks a mapping. Repair
// only reinstalls hooks: it never touches audit rows or deletes a store, since either would
// destroy evidence or restricted source.
func CheckIntegrity(ctx context.Context, repairHooks bool) (*IntegrityReport, error) {
	report := &IntegrityReport{}
	problem := func(format string, args ...any) {
		report.Problems = append(report.Problems, fmt.Sprintf(format, args...))
	}

	scopes := make([]*branchroom_model.Scope, 0)
	if err := db.GetEngine(ctx).Asc("id").Find(&scopes); err != nil {
		return nil, err
	}
	report.Scopes = len(scopes)
	for _, scope := range scopes {
		if err := branchroom_model.VerifyScopeAuditChain(ctx, scope); err != nil {
			problem("scope %d audit chain: %v", scope.ID, err)
		}
	}
	if !setting.Branchroom.Enabled {
		return report, nil
	}

	root := setting.Branchroom.RestrictedRepoRootPath
	workspaces := make([]*branchroom_model.Workspace, 0)
	if err := db.GetEngine(ctx).Asc("id").Find(&workspaces); err != nil {
		return nil, err
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
			problem("workspace %d store is unavailable: %v", workspace.ID, err)
			continue
		}
		if !WorkspaceStoreAvailable(workspace) {
			problem("workspace %d (%s) has no store on disk", workspace.ID, workspace.State)
			continue
		}
		if workspace.State != branchroom_model.WorkspaceStateReady {
			continue
		}
		err = branchroom_module.VerifyRestrictedStoreHook(ctx, root, workspace.StoreID)
		if err == nil {
			continue
		}
		if !repairHooks {
			problem("workspace %d receive hook fails verification: %v", workspace.ID, err)
			continue
		}
		if err := reinstallReceiveHook(ctx, restricted, root, workspace.StoreID); err != nil {
			problem("workspace %d receive hook could not be reinstalled: %v", workspace.ID, err)
			continue
		}
		report.Repaired = append(report.Repaired, fmt.Sprintf("reinstalled the receive hook of workspace %d", workspace.ID))
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
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
			problem("store %s has no workspace mapping and must be removed by hand", entry.Name())
		}
	}
	return report, nil
}

func reinstallReceiveHook(ctx context.Context, restricted git.RepositoryFacade, root, storeID string) error {
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
