// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"bufio"
	"context"
	"strings"

	branchroom_model "gitea.dev/models/branchroom"
	repo_model "gitea.dev/models/repo"
	user_model "gitea.dev/models/user"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/json"
)

// FindRestrictedSource reports the scope whose restricted store already holds one of commitIDs.
// Copying a commit object into the ordinary repository is what publishes restricted work, so the object
// itself is checked rather than branch names. A rewritten or retyped change has new IDs and is not caught.
func FindRestrictedSource(ctx context.Context, repo *repo_model.Repository, commitIDs []string) (*branchroom_model.Scope, bool, error) {
	if len(commitIDs) == 0 {
		return nil, false, nil
	}
	workspaces, err := branchroom_model.FindWorkspacesByRepoID(ctx, repo.ID)
	if err != nil {
		return nil, false, err
	}
	stdin := []byte(strings.Join(commitIDs, "\n") + "\n")
	for _, workspace := range workspaces {
		if workspace.State == branchroom_model.WorkspaceStatePurged || workspace.State == branchroom_model.WorkspaceStateProvisioning {
			continue
		}
		store, err := branchroom_module.RestrictedRepository(workspace.StoreID)
		if err != nil {
			return nil, false, err
		}
		stdout, _, runErr := gitcmd.NewCommand("cat-file", "--batch-check").WithRepo(store).WithStdinBytes(stdin).RunStdString(ctx)
		if runErr != nil {
			return nil, false, runErr
		}
		scanner := bufio.NewScanner(strings.NewReader(stdout))
		for scanner.Scan() {
			if !strings.HasSuffix(scanner.Text(), " missing") {
				scope, err := branchroom_model.GetScopeByID(ctx, workspace.ScopeID)
				return scope, err == nil, err
			}
		}
	}
	return nil, false, nil
}

// RecordPublicPushOfRestrictedWork adds the refusal or the admin publication to the workspace's own audit chain.
func RecordPublicPushOfRestrictedWork(ctx context.Context, scope *branchroom_model.Scope, pusher *user_model.User, ref string, allowed bool) error {
	details, err := json.Marshal(struct {
		Pusher string `json:"pusher_username"`
		Ref    string `json:"ref"`
	}{pusher.Name, ref})
	if err != nil {
		return err
	}
	actor := ActorFromUser(pusher)
	result, reason := branchroom_model.DecisionResultDeny, "restricted_commit_public_push_refused"
	if allowed {
		result, reason = branchroom_model.DecisionResultAllow, "restricted_commit_published_by_admin"
	}
	_, err = branchroom_model.RecordDecisionAndAudit(ctx, scope, &branchroom_model.Decision{
		Details: string(details), ScopeID: scope.ID, RepoID: scope.RepoID, ActorType: actor.Type, ActorID: actor.ID,
		Action: branchroom_model.ActionPublish, ResourceType: branchroom_model.ResourceTypeScope, ResourceID: scopeResourceID(scope.ID),
		Surface: branchroom_model.SurfaceSystem, Result: result, ReasonCode: reason, OrdinaryAllowed: allowed,
	})
	return err
}
