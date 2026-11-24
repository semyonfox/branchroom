// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	audit_model "gitea.dev/models/audit"
	branchroom_model "gitea.dev/models/branchroom"
	access_model "gitea.dev/models/perm/access"
	repo_model "gitea.dev/models/repo"
	user_model "gitea.dev/models/user"
	branchroom_module "gitea.dev/modules/branchroom"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/git/gitrepo"
	"gitea.dev/modules/json"
	"gitea.dev/modules/log"
	"gitea.dev/modules/private"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/util"
	"gitea.dev/services/audit"
)

// FindRestrictedSource reports the scope whose restricted store already holds one of commitIDs.
// Copying a commit object into the ordinary repository is what publishes restricted work, so the object
// itself is checked rather than branch names. A rewritten or retyped change has new IDs and is not caught.
func FindRestrictedSource(ctx context.Context, workspaces []*branchroom_model.Workspace, commitIDs []string) (*branchroom_model.Scope, bool, error) {
	if len(commitIDs) == 0 {
		return nil, false, nil
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

// CheckPullRequestHead refuses a pull request head carrying restricted work of the base repository or one it was forked from.
// The head repository need not be a fork, so its pre-receive check cannot cover what a pull request copies into the base.
// Admins of the repository holding the workspace may publish. Without a doer restricted work is refused unaudited.
func CheckPullRequestHead(ctx context.Context, doer *user_model.User, base, head *repo_model.Repository, headBranch, headCommitID string) error {
	if !setting.Branchroom.Enabled || base.ID == head.ID {
		return nil
	}
	var newCommits []string
	for source := base; source != nil; source = source.BaseRepo {
		if err := source.GetBaseRepo(ctx); err != nil {
			return err
		}
		workspaces, err := branchroom_model.FindWorkspacesByRepoID(ctx, source.ID)
		if err != nil {
			return err
		}
		if len(workspaces) == 0 {
			continue
		}
		if newCommits == nil {
			// run in the base repository with the head's objects borrowed, so --all excludes what the base already has
			env := append(os.Environ(), private.GitAlternativeObjectDirectories+"="+filepath.Join(gitrepo.RepoLocalPath(head), "objects"))
			output, _, err := gitcmd.NewCommand("rev-list").AddDynamicArguments(headCommitID).AddArguments("--not", "--all").
				WithEnv(env).WithRepo(base).RunStdString(ctx)
			if err != nil {
				return fmt.Errorf("CheckPullRequestHead: list commits of %s new to %s: %w", head.FullName(), base.FullName(), err)
			}
			newCommits = strings.Fields(output)
			if len(newCommits) == 0 {
				return nil
			}
		}
		scope, found, err := FindRestrictedSource(ctx, workspaces, newCommits)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		if doer == nil {
			return util.ErrorWrapTranslatable(util.ErrPermissionDenied, "repo.pulls.restricted_work_refused")
		}

		permission, err := access_model.GetDoerRepoPermission(ctx, source, doer)
		if err != nil {
			return err
		}
		allowed := permission.IsAdmin()
		ref := head.FullName() + ":" + headBranch
		if err := RecordPublicPushOfRestrictedWork(ctx, scope, doer, ref, allowed); err != nil {
			log.Error("record restricted publish decision for %s: %v", source.FullName(), err)
		}
		audit.RecordAs(ctx, doer, util.Iif(allowed, audit_model.RepositoryRestrictedPublished, audit_model.RepositoryRestrictedRefused), source,
			"ref", ref, "workspace", scope.Name)
		if !allowed {
			return util.ErrorWrapTranslatable(util.ErrPermissionDenied, "repo.pulls.restricted_work_refused")
		}
	}
	return nil
}
