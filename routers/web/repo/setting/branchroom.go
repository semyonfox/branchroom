// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"errors"
	"net/http"

	branchroom_model "gitea.dev/models/branchroom"
	unit_model "gitea.dev/models/unit"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/git"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/templates"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
	"gitea.dev/modules/web"
	branchroom_service "gitea.dev/services/branchroom"
	"gitea.dev/services/context"
	"gitea.dev/services/forms"
)

const tplBranchroom templates.TplName = "repo/settings/branchroom"

type branchroomScopeData struct {
	ID                    int64
	Kind                  branchroom_model.ScopeKind
	Name                  string
	State                 branchroom_model.ScopeState
	CanGrantDiscovery     bool
	CanProvisionWorkspace bool
	HasWorkspace          bool
	WorkspaceState        branchroom_model.WorkspaceState
	WorkspacePublicBase   string
	DiscoveryGrants       []branchroomDiscoveryGrantData
}

type branchroomDiscoveryGrantData struct {
	ID          int64
	SubjectName string
}

// MustEnableBranchroom hides all Branchroom routes while the feature is disabled.
func MustEnableBranchroom(ctx *context.Context) {
	if !setting.Branchroom.Enabled {
		ctx.NotFound(nil)
		return
	}
}

// Branchroom renders policy controls for the repository's actual owner.
func Branchroom(ctx *context.Context) {
	if !requireBranchroomScopeOwner(ctx) {
		return
	}

	scopes, err := branchroom_model.FindScopesByRepoID(ctx, ctx.Repo.Repository.ID)
	if err != nil {
		ctx.ServerError("FindScopesByRepoID", err)
		return
	}
	visibleScopes := make([]*branchroom_model.Scope, 0, len(scopes))
	for _, scope := range scopes {
		if scope.Kind == branchroom_model.ScopeKindRepository && scope.State != branchroom_model.ScopeStateRetired {
			visibleScopes = append(visibleScopes, scope)
		}
	}
	workspaces, err := branchroom_model.FindWorkspacesByRepoID(ctx, ctx.Repo.Repository.ID)
	if err != nil {
		ctx.ServerError("FindBranchroomWorkspaces", err)
		return
	}
	workspacesByScopeID := make(map[int64]*branchroom_model.Workspace, len(workspaces))
	for _, workspace := range workspaces {
		if workspace.RepoID != ctx.Repo.Repository.ID {
			ctx.ServerError("ValidateBranchroomWorkspace", util.ErrInvalidArgument)
			return
		}
		if err := workspace.Validate(); err != nil {
			ctx.ServerError("ValidateBranchroomWorkspace", err)
			return
		}
		workspacesByScopeID[workspace.ScopeID] = workspace
	}

	grantsByScopeID := make(map[int64][]*branchroom_model.Grant, len(visibleScopes))
	userIDs := make([]int64, 0)
	now := timeutil.TimeStampNow()
	for _, scope := range visibleScopes {
		grants, err := branchroom_model.FindActiveGrantsAt(ctx, scope.ID, branchroom_model.ActionDiscover, now)
		if err != nil {
			ctx.ServerError("FindActiveBranchroomGrants", err)
			return
		}
		for _, grant := range grants {
			if grant.SubjectType != branchroom_model.SubjectTypeUser {
				continue
			}
			grantsByScopeID[scope.ID] = append(grantsByScopeID[scope.ID], grant)
			userIDs = append(userIDs, grant.SubjectID)
		}
	}
	users, err := user_model.GetUsersMapByIDs(ctx, userIDs)
	if err != nil {
		ctx.ServerError("GetBranchroomGrantUsers", err)
		return
	}

	scopeData := make([]branchroomScopeData, 0, len(visibleScopes))
	for _, scope := range visibleScopes {
		workspace := workspacesByScopeID[scope.ID]
		data := branchroomScopeData{
			ID:                    scope.ID,
			Kind:                  scope.Kind,
			Name:                  scope.Name,
			State:                 scope.State,
			CanGrantDiscovery:     scope.State == branchroom_model.ScopeStateActive,
			CanProvisionWorkspace: scope.State == branchroom_model.ScopeStateActive && workspace == nil,
			HasWorkspace:          workspace != nil,
			DiscoveryGrants:       make([]branchroomDiscoveryGrantData, 0, len(grantsByScopeID[scope.ID])),
		}
		if workspace != nil {
			data.WorkspaceState = workspace.State
			data.WorkspacePublicBase = workspace.PublicBaseSHA
		}
		for _, grant := range grantsByScopeID[scope.ID] {
			subjectName := string(ctx.Tr("repo.settings.branchroom.deleted_user", grant.SubjectID))
			if user, ok := users[grant.SubjectID]; ok {
				subjectName = user.Name
			}
			data.DiscoveryGrants = append(data.DiscoveryGrants, branchroomDiscoveryGrantData{
				ID:          grant.ID,
				SubjectName: subjectName,
			})
		}
		scopeData = append(scopeData, data)
	}

	ctx.Data["Title"] = ctx.Tr("repo.settings.branchroom")
	ctx.Data["PageIsSettingsBranchroom"] = true
	ctx.Data["BranchroomScopes"] = scopeData
	ctx.HTML(http.StatusOK, tplBranchroom)
}

// BranchroomDiscoveryGrantPost grants discovery to one direct user for an active scope.
func BranchroomDiscoveryGrantPost(ctx *context.Context) {
	if !requireBranchroomScopeOwner(ctx) {
		return
	}
	form := web.GetForm[*forms.BranchroomDiscoveryGrantForm](ctx)
	if ctx.HasError() {
		ctx.Flash.Error(ctx.GetErrMsg())
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}

	_, err := branchroom_service.CreateRepositoryDiscoveryGrant(ctx, branchroom_service.CreateRepositoryDiscoveryGrantRequest{
		RepoID:          ctx.Repo.Repository.ID,
		ScopeID:         form.ScopeID,
		Actor:           branchroom_service.ActorFromUser(ctx.Doer),
		SubjectUsername: form.Username,
		Surface:         branchroom_model.SurfaceWeb,
	})
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			ctx.NotFound(nil)
			return
		}
		if errors.Is(err, branchroom_service.ErrDiscoveryGrantAlreadyExists) {
			ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.grant_exists"))
		} else {
			ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.grant_create_failed"))
		}
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}

	ctx.Flash.Success(ctx.Tr("repo.settings.branchroom.grant_created"))
	ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
}

// BranchroomWorkspacePost provisions an isolated store from the current default branch.
// The store remains unreachable until a later transport gateway is reviewed and added.
func BranchroomWorkspacePost(ctx *context.Context) {
	if !requireBranchroomScopeOwner(ctx) {
		return
	}
	form := web.GetForm[*forms.BranchroomWorkspaceForm](ctx)
	if ctx.HasError() {
		ctx.Flash.Error(ctx.GetErrMsg())
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}

	publicBaseSHA, err := git.GetBranchCommitID(ctx, ctx.Repo.Repository.CodeStorageRepo(), ctx.Repo.Repository.DefaultBranch)
	if err != nil {
		ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.workspace_provision_failed"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}
	_, err = branchroom_service.CreateRepositoryWorkspace(ctx, branchroom_service.CreateRepositoryWorkspaceRequest{
		RepoID:        ctx.Repo.Repository.ID,
		ScopeID:       form.ScopeID,
		Actor:         branchroom_service.ActorFromUser(ctx.Doer),
		PublicBaseSHA: publicBaseSHA,
		Surface:       branchroom_model.SurfaceWeb,
	})
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			ctx.NotFound(nil)
			return
		}
		if errors.Is(err, branchroom_service.ErrWorkspaceAlreadyExists) {
			ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.workspace_exists"))
		} else {
			ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.workspace_provision_failed"))
		}
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}

	ctx.Flash.Success(ctx.Tr("repo.settings.branchroom.workspace_provisioned"))
	ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
}

// BranchroomDiscoveryGrantRevokePost revokes one direct-user discovery grant.
func BranchroomDiscoveryGrantRevokePost(ctx *context.Context) {
	if !requireBranchroomScopeOwner(ctx) {
		return
	}
	scopeID := ctx.PathParamInt64("scopeID")
	grantID := ctx.PathParamInt64("grantID")
	if scopeID <= 0 || grantID <= 0 {
		ctx.NotFound(nil)
		return
	}
	err := branchroom_service.RevokeRepositoryDiscoveryGrant(ctx, branchroom_service.RevokeRepositoryDiscoveryGrantRequest{
		RepoID:  ctx.Repo.Repository.ID,
		ScopeID: scopeID,
		GrantID: grantID,
		Actor:   branchroom_service.ActorFromUser(ctx.Doer),
		Surface: branchroom_model.SurfaceWeb,
	})
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			ctx.NotFound(nil)
			return
		}
		ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.grant_revoke_failed"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}

	ctx.Flash.Success(ctx.Tr("repo.settings.branchroom.grant_revoked"))
	ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
}

// BranchroomPost creates a scope for the actual repository owner.
func BranchroomPost(ctx *context.Context) {
	if !requireBranchroomScopeOwner(ctx) {
		return
	}
	form := web.GetForm[*forms.BranchroomScopeForm](ctx)
	if ctx.HasError() {
		ctx.Flash.Error(ctx.GetErrMsg())
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}

	_, err := branchroom_service.CreateRepositoryScope(ctx, branchroom_service.CreateRepositoryScopeRequest{
		RepoID:  ctx.Repo.Repository.ID,
		Actor:   branchroom_service.ActorFromUser(ctx.Doer),
		Name:    form.Name,
		Surface: branchroom_model.SurfaceWeb,
	})
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			ctx.NotFound(nil)
			return
		}
		ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.create_failed"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}

	ctx.Flash.Success(ctx.Tr("repo.settings.branchroom.created"))
	ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
}

func requireBranchroomScopeOwner(ctx *context.Context) bool {
	if !setting.Branchroom.Enabled || !ctx.Repo.Repository.UnitEnabled(ctx, unit_model.TypeCode) {
		ctx.NotFound(nil)
		return false
	}
	owner, err := branchroom_service.CanManageRepositoryScopes(ctx, ctx.Repo.Repository.ID, branchroom_service.ActorFromUser(ctx.Doer))
	if err != nil {
		ctx.ServerError("CanManageRepositoryScopes", err)
		return false
	}
	if !owner {
		ctx.NotFound(nil)
		return false
	}
	return true
}
