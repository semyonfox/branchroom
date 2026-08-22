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
	CanGrantSourceRead    bool
	CanProvisionWorkspace bool
	HasWorkspace          bool
	WorkspaceState        branchroom_model.WorkspaceState
	WorkspacePublicBase   string
	DiscoveryGrants       []branchroomDirectUserGrantData
	SourceReadGrants      []branchroomDirectUserGrantData
}

type branchroomDirectUserGrantData struct {
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

	discoveryGrantsByScopeID := make(map[int64][]*branchroom_model.Grant, len(visibleScopes))
	sourceReadGrantsByScopeID := make(map[int64][]*branchroom_model.Grant, len(visibleScopes))
	userIDs := make([]int64, 0)
	now := timeutil.TimeStampNow()
	for _, scope := range visibleScopes {
		discoveryGrants, err := branchroom_model.FindActiveGrantsAt(ctx, scope.ID, branchroom_model.ActionDiscover, now)
		if err != nil {
			ctx.ServerError("FindActiveBranchroomGrants", err)
			return
		}
		for _, grant := range discoveryGrants {
			if grant.SubjectType != branchroom_model.SubjectTypeUser {
				continue
			}
			discoveryGrantsByScopeID[scope.ID] = append(discoveryGrantsByScopeID[scope.ID], grant)
			userIDs = append(userIDs, grant.SubjectID)
		}
		sourceReadGrants, err := branchroom_model.FindActiveGrantsAt(ctx, scope.ID, branchroom_model.ActionReadSource, now)
		if err != nil {
			ctx.ServerError("FindActiveBranchroomSourceReadGrants", err)
			return
		}
		for _, grant := range sourceReadGrants {
			if grant.SubjectType != branchroom_model.SubjectTypeUser {
				continue
			}
			sourceReadGrantsByScopeID[scope.ID] = append(sourceReadGrantsByScopeID[scope.ID], grant)
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
			DiscoveryGrants:       make([]branchroomDirectUserGrantData, 0, len(discoveryGrantsByScopeID[scope.ID])),
			SourceReadGrants:      make([]branchroomDirectUserGrantData, 0, len(sourceReadGrantsByScopeID[scope.ID])),
		}
		if workspace != nil {
			data.WorkspaceState = workspace.State
			data.WorkspacePublicBase = workspace.PublicBaseSHA
			data.CanGrantSourceRead = scope.State == branchroom_model.ScopeStateActive && workspace.State == branchroom_model.WorkspaceStateReady
		}
		for _, grant := range discoveryGrantsByScopeID[scope.ID] {
			subjectName := string(ctx.Tr("repo.settings.branchroom.deleted_user", grant.SubjectID))
			if user, ok := users[grant.SubjectID]; ok {
				subjectName = user.Name
			}
			data.DiscoveryGrants = append(data.DiscoveryGrants, branchroomDirectUserGrantData{
				ID:          grant.ID,
				SubjectName: subjectName,
			})
		}
		for _, grant := range sourceReadGrantsByScopeID[scope.ID] {
			subjectName := string(ctx.Tr("repo.settings.branchroom.deleted_user", grant.SubjectID))
			if user, ok := users[grant.SubjectID]; ok {
				subjectName = user.Name
			}
			data.SourceReadGrants = append(data.SourceReadGrants, branchroomDirectUserGrantData{
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
	branchroomDirectUserGrantPost(ctx, branchroomDiscoveryGrantForm, createBranchroomDiscoveryGrant, branchroom_service.ErrDiscoveryGrantAlreadyExists,
		"repo.settings.branchroom.grant_exists", "repo.settings.branchroom.grant_create_failed", "repo.settings.branchroom.grant_created")
}

// BranchroomSourceReadGrantPost grants source read to one direct user for a ready workspace.
// It does not reveal a clone URL; the narrow Git HTTP route reevaluates the grant.
func BranchroomSourceReadGrantPost(ctx *context.Context) {
	branchroomDirectUserGrantPost(ctx, branchroomSourceReadGrantForm, createBranchroomSourceReadGrant, branchroom_service.ErrSourceReadGrantAlreadyExists,
		"repo.settings.branchroom.source_grant_exists", "repo.settings.branchroom.source_grant_create_failed", "repo.settings.branchroom.source_grant_created")
}

// BranchroomWorkspacePost provisions an isolated store from the current default branch.
// A ready store remains hidden until a direct user passes the Git HTTP authorization gate.
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
	branchroomDirectUserGrantRevokePost(ctx, revokeBranchroomDiscoveryGrant,
		"repo.settings.branchroom.grant_revoke_failed", "repo.settings.branchroom.grant_revoked")
}

// BranchroomSourceReadGrantRevokePost revokes one direct-user source-read grant.
func BranchroomSourceReadGrantRevokePost(ctx *context.Context) {
	branchroomDirectUserGrantRevokePost(ctx, revokeBranchroomSourceReadGrant,
		"repo.settings.branchroom.source_grant_revoke_failed", "repo.settings.branchroom.source_grant_revoked")
}

type branchroomGrantFormValues struct {
	ScopeID  int64
	Username string
}

type branchroomGrantCreateFunc func(ctx *context.Context, repoID, scopeID int64, actor branchroom_service.Actor, username string) error

func branchroomDiscoveryGrantForm(ctx *context.Context) branchroomGrantFormValues {
	form := web.GetForm[*forms.BranchroomDiscoveryGrantForm](ctx)
	return branchroomGrantFormValues{ScopeID: form.ScopeID, Username: form.Username}
}

func branchroomSourceReadGrantForm(ctx *context.Context) branchroomGrantFormValues {
	form := web.GetForm[*forms.BranchroomSourceReadGrantForm](ctx)
	return branchroomGrantFormValues{ScopeID: form.ScopeID, Username: form.Username}
}

func createBranchroomDiscoveryGrant(ctx *context.Context, repoID, scopeID int64, actor branchroom_service.Actor, username string) error {
	_, err := branchroom_service.CreateRepositoryDiscoveryGrant(ctx, branchroom_service.CreateRepositoryDiscoveryGrantRequest{
		RepoID:          repoID,
		ScopeID:         scopeID,
		Actor:           actor,
		SubjectUsername: username,
		Surface:         branchroom_model.SurfaceWeb,
	})
	return err
}

func createBranchroomSourceReadGrant(ctx *context.Context, repoID, scopeID int64, actor branchroom_service.Actor, username string) error {
	_, err := branchroom_service.CreateRepositorySourceReadGrant(ctx, branchroom_service.CreateRepositorySourceReadGrantRequest{
		RepoID:          repoID,
		ScopeID:         scopeID,
		Actor:           actor,
		SubjectUsername: username,
		Surface:         branchroom_model.SurfaceWeb,
	})
	return err
}

func branchroomDirectUserGrantPost(ctx *context.Context, getForm func(ctx *context.Context) branchroomGrantFormValues, createGrant branchroomGrantCreateFunc, existsError error, existsMessage, failedMessage, successMessage string) {
	if !requireBranchroomScopeOwner(ctx) {
		return
	}
	form := getForm(ctx)
	if ctx.HasError() {
		ctx.Flash.Error(ctx.GetErrMsg())
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}

	err := createGrant(ctx, ctx.Repo.Repository.ID, form.ScopeID, branchroom_service.ActorFromUser(ctx.Doer), form.Username)
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			ctx.NotFound(nil)
			return
		}
		if errors.Is(err, existsError) {
			ctx.Flash.Error(ctx.Tr(existsMessage))
		} else {
			ctx.Flash.Error(ctx.Tr(failedMessage))
		}
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}

	ctx.Flash.Success(ctx.Tr(successMessage))
	ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
}

type branchroomGrantRevokeFunc func(ctx *context.Context, repoID, scopeID, grantID int64, actor branchroom_service.Actor) error

func revokeBranchroomDiscoveryGrant(ctx *context.Context, repoID, scopeID, grantID int64, actor branchroom_service.Actor) error {
	return branchroom_service.RevokeRepositoryDiscoveryGrant(ctx, branchroom_service.RevokeRepositoryDiscoveryGrantRequest{
		RepoID:  repoID,
		ScopeID: scopeID,
		GrantID: grantID,
		Actor:   actor,
		Surface: branchroom_model.SurfaceWeb,
	})
}

func revokeBranchroomSourceReadGrant(ctx *context.Context, repoID, scopeID, grantID int64, actor branchroom_service.Actor) error {
	return branchroom_service.RevokeRepositorySourceReadGrant(ctx, branchroom_service.RevokeRepositorySourceReadGrantRequest{
		RepoID:  repoID,
		ScopeID: scopeID,
		GrantID: grantID,
		Actor:   actor,
		Surface: branchroom_model.SurfaceWeb,
	})
}

func branchroomDirectUserGrantRevokePost(ctx *context.Context, revokeGrant branchroomGrantRevokeFunc, failedMessage, successMessage string) {
	if !requireBranchroomScopeOwner(ctx) {
		return
	}
	scopeID := ctx.PathParamInt64("scopeID")
	grantID := ctx.PathParamInt64("grantID")
	if scopeID <= 0 || grantID <= 0 {
		ctx.NotFound(nil)
		return
	}
	err := revokeGrant(ctx, ctx.Repo.Repository.ID, scopeID, grantID, branchroom_service.ActorFromUser(ctx.Doer))
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			ctx.NotFound(nil)
			return
		}
		ctx.Flash.Error(ctx.Tr(failedMessage))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}

	ctx.Flash.Success(ctx.Tr(successMessage))
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
	if !setting.Branchroom.Enabled || ctx.Doer == nil || ctx.DoerIsImpersonated() || ctx.Doer.MustChangePassword || !ctx.Repo.Repository.UnitEnabled(ctx, unit_model.TypeCode) {
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
