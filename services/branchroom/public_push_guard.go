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

// RestrictedSource is a repository whose restricted store holds commits that a write would publish.
type RestrictedSource struct {
	Repo  *repo_model.Repository
	Scope *branchroom_model.Scope
}

// FindRestrictedSources lists target and the repositories it was forked from whose restricted store holds commits reachable from commitID.
// The store mirrors the public repository, so commits count only when they are new to the repository holding the workspace.
// objectDirs hold the objects of commitID that repository may lack.
func FindRestrictedSources(ctx context.Context, target *repo_model.Repository, commitID string, objectDirs []string) ([]RestrictedSource, error) {
	var sources []RestrictedSource
	for source := target; source != nil; source = source.BaseRepo {
		if err := source.GetBaseRepo(ctx); err != nil {
			return nil, err
		}
		workspaces, err := branchroom_model.FindWorkspacesByRepoID(ctx, source.ID)
		if err != nil {
			return nil, err
		}
		if len(workspaces) == 0 {
			continue
		}
		env := append(os.Environ(), private.GitAlternativeObjectDirectories+"="+strings.Join(objectDirs, string(filepath.ListSeparator)))
		output, _, err := gitcmd.NewCommand("rev-list").AddDynamicArguments(commitID).AddArguments("--not", "--all").
			WithEnv(env).WithRepo(source).RunStdString(ctx)
		if err != nil {
			return nil, fmt.Errorf("FindRestrictedSources: list commits new to %s: %w", source.FullName(), err)
		}
		scope, found, err := FindRestrictedSource(ctx, workspaces, strings.Fields(output))
		if err != nil {
			return nil, err
		}
		if found {
			sources = append(sources, RestrictedSource{Repo: source, Scope: scope})
		}
	}
	return sources, nil
}

// CheckPullRequestHead refuses a pull request head carrying restricted work of the base repository or one it was forked from.
// The head repository need not be a fork, so its pre-receive check cannot cover what a pull request copies into the base.
// Admins of the repository holding the workspace may publish. Without a doer restricted work is refused unaudited.
func CheckPullRequestHead(ctx context.Context, doer *user_model.User, base, head *repo_model.Repository, headBranch, headCommitID string) error {
	if !setting.Branchroom.Enabled || base.ID == head.ID {
		return nil
	}
	sources, err := FindRestrictedSources(ctx, base, headCommitID, []string{filepath.Join(gitrepo.RepoLocalPath(head), "objects")})
	if err != nil {
		return fmt.Errorf("CheckPullRequestHead: check %s against %s: %w", head.FullName(), base.FullName(), err)
	}
	for _, source := range sources {
		if doer == nil {
			return util.ErrorWrapTranslatable(util.ErrPermissionDenied, "repo.pulls.restricted_work_refused")
		}
		permission, err := access_model.GetDoerRepoPermission(ctx, source.Repo, doer)
		if err != nil {
			return err
		}
		allowed := permission.IsAdmin()
		ref := head.FullName() + ":" + headBranch
		if err := RecordPublicPushOfRestrictedWork(ctx, source.Scope, doer, ref, allowed); err != nil {
			log.Error("record restricted publish decision for %s: %v", source.Repo.FullName(), err)
		}
		audit.RecordAs(ctx, doer, util.Iif(allowed, audit_model.RepositoryRestrictedPublished, audit_model.RepositoryRestrictedRefused), source.Repo,
			"ref", ref, "workspace", source.Scope.Name)
		if !allowed {
			return util.ErrorWrapTranslatable(util.ErrPermissionDenied, "repo.pulls.restricted_work_refused")
		}
	}
	return nil
}
