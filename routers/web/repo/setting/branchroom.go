// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	stdcontext "context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	organization_model "gitea.dev/models/organization"
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
	repo_service "gitea.dev/services/repository"
)

const tplBranchroom templates.TplName = "repo/settings/branchroom"

type branchroomScopeData struct {
	ID                    int64
	Kind                  branchroom_model.ScopeKind
	Name                  string
	State                 branchroom_model.ScopeState
	CanGrantDiscovery     bool
	CanGrantSourceRead    bool
	CanGrantSourceWrite   bool
	CanProvisionWorkspace bool
	CanRotateRouteKey     bool
	CanRetryWorkspace     bool
	CanRetireWorkspace    bool
	CanPurgeWorkspace     bool
	HasWorkspace          bool
	WorkspaceState        branchroom_model.WorkspaceState
	WorkspaceStoreMissing bool
	WorkspacePublicBase   string
	DiscoveryGrants       []branchroomDirectUserGrantData
	SourceReadGrants      []branchroomDirectUserGrantData
	SourceWriteGrants     []branchroomDirectUserGrantData
	TeamGrants            []branchroomTeamGrantData
	People                []branchroomPersonAccess
}

type branchroomDirectUserGrantData struct {
	ID          int64
	SubjectID   int64
	SubjectName string
	ExpiresUnix timeutil.TimeStamp
	ExpiresSoon bool
}

type branchroomTeamGrantData struct {
	ID          int64
	TeamName    string
	ExpiresUnix timeutil.TimeStamp
	ExpiresSoon bool
	Action      branchroom_model.Action
}

// MustEnableBranchroom hides all Branchroom routes while the feature is disabled.
func MustEnableBranchroom(ctx *context.Context) {
	if !setting.Branchroom.Enabled {
		ctx.NotFound()
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
		ctx.ServerErrorAuto(err)
		return
	}
	workspaces, err := branchroom_model.FindWorkspacesByRepoID(ctx, ctx.Repo.Repository.ID)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	workspacesByScopeID := make(map[int64]*branchroom_model.Workspace, len(workspaces))
	for _, workspace := range workspaces {
		if workspace.RepoID != ctx.Repo.Repository.ID {
			ctx.ServerErrorAuto(errors.New("branchroom workspace validation failed"))
			return
		}
		if err := workspace.Validate(); err != nil {
			ctx.ServerErrorAuto(err)
			return
		}
		workspacesByScopeID[workspace.ScopeID] = workspace
	}

	visibleScopes := make([]*branchroom_model.Scope, 0, len(scopes))
	for _, scope := range scopes {
		if scope.Kind != branchroom_model.ScopeKindRepository {
			continue
		}
		workspace := workspacesByScopeID[scope.ID]
		if scope.State != branchroom_model.ScopeStateRetired || workspace != nil && (workspace.State == branchroom_model.WorkspaceStateRetired || workspace.State == branchroom_model.WorkspaceStateFailed || branchroom_service.WorkspaceProvisioningAbandoned(workspace)) {
			visibleScopes = append(visibleScopes, scope)
		}
	}

	discoveryGrantsByScopeID := make(map[int64][]*branchroom_model.Grant, len(visibleScopes))
	sourceReadGrantsByScopeID := make(map[int64][]*branchroom_model.Grant, len(visibleScopes))
	sourceWriteGrantsByScopeID := make(map[int64][]*branchroom_model.Grant, len(visibleScopes))
	teamGrantsByScopeID := make(map[int64][]*branchroom_model.Grant, len(visibleScopes))
	userIDs := make([]int64, 0)
	teamIDs := make([]int64, 0)
	now := timeutil.TimeStampNow()
	for _, scope := range visibleScopes {
		if scope.State == branchroom_model.ScopeStateRetired {
			continue
		}
		discoveryGrants, err := branchroom_model.FindActiveGrantsAt(ctx, scope.ID, branchroom_model.ActionDiscover, now)
		if err != nil {
			ctx.ServerErrorAuto(err)
			return
		}
		for _, grant := range discoveryGrants {
			switch grant.SubjectType {
			case branchroom_model.SubjectTypeUser:
				discoveryGrantsByScopeID[scope.ID] = append(discoveryGrantsByScopeID[scope.ID], grant)
				userIDs = append(userIDs, grant.SubjectID)
			case branchroom_model.SubjectTypeTeam:
				teamGrantsByScopeID[scope.ID] = append(teamGrantsByScopeID[scope.ID], grant)
				teamIDs = append(teamIDs, grant.SubjectID)
			}
		}
		sourceReadGrants, err := branchroom_model.FindActiveGrantsAt(ctx, scope.ID, branchroom_model.ActionReadSource, now)
		if err != nil {
			ctx.ServerErrorAuto(err)
			return
		}
		for _, grant := range sourceReadGrants {
			switch grant.SubjectType {
			case branchroom_model.SubjectTypeUser:
				sourceReadGrantsByScopeID[scope.ID] = append(sourceReadGrantsByScopeID[scope.ID], grant)
				userIDs = append(userIDs, grant.SubjectID)
			case branchroom_model.SubjectTypeTeam:
				teamGrantsByScopeID[scope.ID] = append(teamGrantsByScopeID[scope.ID], grant)
				teamIDs = append(teamIDs, grant.SubjectID)
			}
		}
		sourceWriteGrants, err := branchroom_model.FindActiveGrantsAt(ctx, scope.ID, branchroom_model.ActionWriteSource, now)
		if err != nil {
			ctx.ServerErrorAuto(err)
			return
		}
		for _, grant := range sourceWriteGrants {
			if grant.SubjectType != branchroom_model.SubjectTypeUser {
				continue
			}
			sourceWriteGrantsByScopeID[scope.ID] = append(sourceWriteGrantsByScopeID[scope.ID], grant)
			userIDs = append(userIDs, grant.SubjectID)
		}
	}
	peopleGrants := make([]*branchroom_model.Grant, 0)
	if err := db.GetEngine(ctx).Table("branchroom_grant").
		Join("INNER", "branchroom_scope", "branchroom_scope.id = branchroom_grant.scope_id").
		Where("branchroom_scope.repo_id = ? AND branchroom_grant.subject_type = ? AND branchroom_grant.revoked_unix = 0", ctx.Repo.Repository.ID, branchroom_model.SubjectTypeUser).
		Select("branchroom_grant.*").Find(&peopleGrants); err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	for _, grant := range peopleGrants {
		userIDs = append(userIDs, grant.SubjectID)
	}
	users, err := user_model.GetUsersMapByIDs(ctx, userIDs)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	teams, err := organization_model.GetTeamsByIDs(ctx, teamIDs)
	if err != nil {
		ctx.ServerErrorAuto(err)
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
			CanGrantDiscovery:     scope.State == branchroom_model.ScopeStateActive && (workspace == nil || workspace.State != branchroom_model.WorkspaceStatePurged),
			CanProvisionWorkspace: scope.State == branchroom_model.ScopeStateActive && workspace == nil,
			HasWorkspace:          workspace != nil,
			DiscoveryGrants:       make([]branchroomDirectUserGrantData, 0, len(discoveryGrantsByScopeID[scope.ID])),
			SourceReadGrants:      make([]branchroomDirectUserGrantData, 0, len(sourceReadGrantsByScopeID[scope.ID])),
			SourceWriteGrants:     make([]branchroomDirectUserGrantData, 0, len(sourceWriteGrantsByScopeID[scope.ID])),
		}
		if workspace != nil {
			data.WorkspaceState = workspace.State
			data.WorkspaceStoreMissing = workspace.State == branchroom_model.WorkspaceStateReady && !branchroom_service.WorkspaceStoreAvailable(workspace)
			data.WorkspacePublicBase = workspace.PublicBaseSHA
			data.CanGrantSourceRead = scope.State == branchroom_model.ScopeStateActive && workspace.State == branchroom_model.WorkspaceStateReady
			data.CanGrantSourceWrite = data.CanGrantSourceRead
			data.CanRotateRouteKey = data.CanGrantSourceRead && !data.WorkspaceStoreMissing
			data.CanRetryWorkspace = scope.State == branchroom_model.ScopeStateActive && (workspace.State == branchroom_model.WorkspaceStateFailed || branchroom_service.WorkspaceProvisioningAbandoned(workspace))
			data.CanRetireWorkspace = scope.State == branchroom_model.ScopeStateActive && (workspace.State == branchroom_model.WorkspaceStateReady || workspace.State == branchroom_model.WorkspaceStateFailed)
			data.CanPurgeWorkspace = workspace.State == branchroom_model.WorkspaceStateReady || workspace.State == branchroom_model.WorkspaceStateFailed || workspace.State == branchroom_model.WorkspaceStateRetired || branchroom_service.WorkspaceProvisioningAbandoned(workspace)
		}
		for _, grant := range discoveryGrantsByScopeID[scope.ID] {
			subjectName := string(ctx.Tr("repo.settings.branchroom.deleted_user", grant.SubjectID))
			if user, ok := users[grant.SubjectID]; ok {
				subjectName = user.Name
			}
			data.DiscoveryGrants = append(data.DiscoveryGrants, branchroomDirectUserGrantData{
				ID:          grant.ID,
				SubjectID:   grant.SubjectID,
				ExpiresUnix: grant.ExpiresUnix,
				ExpiresSoon: grant.ExpiresUnix != 0 && grant.ExpiresUnix <= now.AddDuration(7*24*time.Hour),
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
				SubjectID:   grant.SubjectID,
				ExpiresUnix: grant.ExpiresUnix,
				ExpiresSoon: grant.ExpiresUnix != 0 && grant.ExpiresUnix <= now.AddDuration(7*24*time.Hour),
				SubjectName: subjectName,
			})
		}
		for _, grant := range sourceWriteGrantsByScopeID[scope.ID] {
			subjectName := string(ctx.Tr("repo.settings.branchroom.deleted_user", grant.SubjectID))
			if user, ok := users[grant.SubjectID]; ok {
				subjectName = user.Name
			}
			data.SourceWriteGrants = append(data.SourceWriteGrants, branchroomDirectUserGrantData{
				ID:          grant.ID,
				SubjectID:   grant.SubjectID,
				ExpiresUnix: grant.ExpiresUnix,
				ExpiresSoon: grant.ExpiresUnix != 0 && grant.ExpiresUnix <= now.AddDuration(7*24*time.Hour),
				SubjectName: subjectName,
			})
		}
		for _, grant := range teamGrantsByScopeID[scope.ID] {
			teamName := fmt.Sprintf("#%d", grant.SubjectID)
			if team, ok := teams[grant.SubjectID]; ok {
				teamName = team.Name
			}
			data.TeamGrants = append(data.TeamGrants, branchroomTeamGrantData{
				ID:          grant.ID,
				ExpiresUnix: grant.ExpiresUnix,
				ExpiresSoon: grant.ExpiresUnix != 0 && grant.ExpiresUnix <= now.AddDuration(7*24*time.Hour),
				TeamName:    teamName,
				Action:      grant.Action,
			})
		}
		data.People = groupBranchroomPeople(data, now)
		scopeData = append(scopeData, data)
	}

	ctx.Data["Title"] = ctx.Tr("repo.settings.branchroom")
	ctx.Data["PageIsSettingsBranchroom"] = true
	ctx.Data["BranchroomScopes"] = scopeData
	people := make([]*user_model.User, 0, len(users))
	for _, user := range users {
		people = append(people, user)
	}
	slices.SortFunc(people, func(a, b *user_model.User) int { return strings.Compare(a.Name, b.Name) })
	ctx.Data["BranchroomPeople"] = people
	ctx.Data["BranchroomRenewExpiry"] = time.Now().Add(setting.Branchroom.GrantDuration()).Format("2006-01-02T15:04")
	auditScopes := make([]*branchroom_model.Scope, 0, len(scopes))
	for _, scope := range scopes {
		if scope.Kind == branchroom_model.ScopeKindRepository {
			auditScopes = append(auditScopes, scope)
		}
	}
	ctx.Data["BranchroomAuditScopes"] = auditScopes
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

// BranchroomSourceWriteGrantPost stores a source-write policy grant for one direct user.
// It is deny-only groundwork for a future receive boundary; no write path exists today.
func BranchroomSourceWriteGrantPost(ctx *context.Context) {
	branchroomDirectUserGrantPost(ctx, branchroomSourceWriteGrantForm, createBranchroomSourceWriteGrant, branchroom_service.ErrSourceWriteGrantAlreadyExists,
		"repo.settings.branchroom.source_write_grant_exists", "repo.settings.branchroom.source_write_grant_create_failed", "repo.settings.branchroom.source_write_grant_created")
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
			ctx.NotFound()
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

// BranchroomWorkspaceRouteKeyRotatePost invalidates copied clone URLs for a ready workspace.
func BranchroomWorkspaceRouteKeyRotatePost(ctx *context.Context) {
	if !requireBranchroomScopeOwner(ctx) {
		return
	}

	scopeID := ctx.PathParamInt64("scopeID")
	if scopeID <= 0 {
		ctx.NotFound()
		return
	}
	if err := branchroom_service.RotateRepositoryWorkspaceRouteKey(ctx, branchroom_service.RotateRepositoryWorkspaceRouteKeyRequest{
		RepoID:  ctx.Repo.Repository.ID,
		ScopeID: scopeID,
		Actor:   branchroom_service.ActorFromUser(ctx.Doer),
		Surface: branchroom_model.SurfaceWeb,
	}); err != nil {
		if errors.Is(err, util.ErrNotExist) {
			ctx.NotFound()
			return
		}
		ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.route_key_rotate_failed"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}

	ctx.Flash.Success(ctx.Tr("repo.settings.branchroom.route_key_rotated"))
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

// BranchroomSourceWriteGrantRevokePost revokes one direct-user source-write policy grant.
func BranchroomSourceWriteGrantRevokePost(ctx *context.Context) {
	branchroomDirectUserGrantRevokePost(ctx, revokeBranchroomSourceWriteGrant,
		"repo.settings.branchroom.source_write_grant_revoke_failed", "repo.settings.branchroom.source_write_grant_revoked")
}

type branchroomGrantFormValues struct {
	ScopeID   int64
	Username  string
	NotBefore string
	Expires   string
}

// parseBranchroomGrantWindow converts optional datetime-local inputs into unix stamps; empty means unset.
func parseBranchroomGrantWindow(notBefore, expires string) (timeutil.TimeStamp, timeutil.TimeStamp, bool) {
	notBeforeUnix, ok := parseBranchroomLocalDatetime(notBefore)
	if !ok {
		return 0, 0, false
	}
	expiresUnix, ok := parseBranchroomLocalDatetime(expires)
	if !ok {
		return 0, 0, false
	}
	return notBeforeUnix, expiresUnix, true
}

func parseBranchroomLocalDatetime(value string) (timeutil.TimeStamp, bool) {
	if value == "" {
		return 0, true
	}
	parsed, err := time.ParseInLocation("2006-01-02T15:04", value, time.Local)
	if err != nil {
		return 0, false
	}
	return timeutil.TimeStamp(parsed.Unix()), true
}

type branchroomGrantCreateFunc func(ctx *context.Context, repoID, scopeID int64, actor branchroom_service.Actor, username string, notBefore, expires timeutil.TimeStamp) error

func branchroomDiscoveryGrantForm(ctx *context.Context) branchroomGrantFormValues {
	form := web.GetForm[*forms.BranchroomDiscoveryGrantForm](ctx)
	return branchroomGrantFormValues{ScopeID: form.ScopeID, Username: form.Username, NotBefore: form.NotBefore, Expires: form.Expires}
}

func branchroomSourceReadGrantForm(ctx *context.Context) branchroomGrantFormValues {
	form := web.GetForm[*forms.BranchroomSourceReadGrantForm](ctx)
	return branchroomGrantFormValues{ScopeID: form.ScopeID, Username: form.Username, NotBefore: form.NotBefore, Expires: form.Expires}
}

func branchroomSourceWriteGrantForm(ctx *context.Context) branchroomGrantFormValues {
	form := web.GetForm[*forms.BranchroomSourceWriteGrantForm](ctx)
	return branchroomGrantFormValues{ScopeID: form.ScopeID, Username: form.Username, NotBefore: form.NotBefore, Expires: form.Expires}
}

func createBranchroomDiscoveryGrant(ctx *context.Context, repoID, scopeID int64, actor branchroom_service.Actor, username string, notBefore, expires timeutil.TimeStamp) error {
	_, err := branchroom_service.CreateRepositoryDiscoveryGrant(ctx, branchroom_service.CreateRepositoryDiscoveryGrantRequest{
		RepoID:          repoID,
		ScopeID:         scopeID,
		Actor:           actor,
		SubjectUsername: username,
		NotBeforeUnix:   notBefore,
		ExpiresUnix:     expires,
		Surface:         branchroom_model.SurfaceWeb,
	})
	return err
}

func createBranchroomSourceReadGrant(ctx *context.Context, repoID, scopeID int64, actor branchroom_service.Actor, username string, notBefore, expires timeutil.TimeStamp) error {
	_, err := branchroom_service.CreateRepositorySourceReadGrant(ctx, branchroom_service.CreateRepositorySourceReadGrantRequest{
		RepoID:          repoID,
		ScopeID:         scopeID,
		Actor:           actor,
		SubjectUsername: username,
		NotBeforeUnix:   notBefore,
		ExpiresUnix:     expires,
		Surface:         branchroom_model.SurfaceWeb,
	})
	return err
}

func createBranchroomSourceWriteGrant(ctx *context.Context, repoID, scopeID int64, actor branchroom_service.Actor, username string, notBefore, expires timeutil.TimeStamp) error {
	_, err := branchroom_service.CreateRepositorySourceWriteGrant(ctx, branchroom_service.CreateRepositorySourceWriteGrantRequest{
		RepoID:          repoID,
		ScopeID:         scopeID,
		Actor:           actor,
		SubjectUsername: username,
		NotBeforeUnix:   notBefore,
		ExpiresUnix:     expires,
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
	notBefore, expires, ok := parseBranchroomGrantWindow(form.NotBefore, form.Expires)
	if !ok {
		ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.grant_window_invalid"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}

	err := createGrant(ctx, ctx.Repo.Repository.ID, form.ScopeID, branchroom_service.ActorFromUser(ctx.Doer), form.Username, notBefore, expires)
	if err != nil {
		if errors.Is(err, existsError) {
			ctx.Flash.Error(ctx.Tr(existsMessage))
			ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
			return
		}
		branchroomGrantCreateError(ctx, err, failedMessage)
		return
	}

	ctx.Flash.Success(ctx.Tr(successMessage))
	ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
}

func branchroomGrantCreateError(ctx *context.Context, err error, failedMessage string) {
	switch {
	case errors.Is(err, util.ErrNotExist):
		ctx.NotFound()
		return
	case errors.Is(err, branchroom_service.ErrGrantDurationExceeded):
		ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.grant_duration_exceeded", setting.Branchroom.MaxGrantDuration.String()))
	case errors.Is(err, util.ErrInvalidArgument):
		ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.grant_window_invalid"))
	default:
		ctx.Flash.Error(ctx.Tr(failedMessage))
	}
	ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
}

// BranchroomTeamGrantPost grants discover or source read to one organization team of the repository owner.
func BranchroomTeamGrantPost(ctx *context.Context) {
	if !requireBranchroomScopeOwner(ctx) {
		return
	}
	form := web.GetForm[*forms.BranchroomTeamGrantForm](ctx)
	if ctx.HasError() {
		ctx.Flash.Error(ctx.GetErrMsg())
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}
	notBefore, expires, ok := parseBranchroomGrantWindow(form.NotBefore, form.Expires)
	if !ok {
		ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.grant_window_invalid"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}

	team, err := organization_model.GetTeam(ctx, ctx.Repo.Owner.ID, strings.TrimSpace(form.Team))
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.team_not_found"))
			ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
			return
		}
		ctx.ServerErrorAuto(err)
		return
	}
	_, err = branchroom_service.CreateRepositoryTeamGrant(ctx, branchroom_service.CreateRepositoryTeamGrantRequest{
		RepoID:        ctx.Repo.Repository.ID,
		ScopeID:       form.ScopeID,
		Actor:         branchroom_service.ActorFromUser(ctx.Doer),
		TeamID:        team.ID,
		Action:        branchroom_model.Action(form.Action),
		NotBeforeUnix: notBefore,
		ExpiresUnix:   expires,
		Surface:       branchroom_model.SurfaceWeb,
	})
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			ctx.NotFound()
			return
		}
		switch {
		case errors.Is(err, branchroom_service.ErrTeamGrantAlreadyExists):
			ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.team_grant_exists"))
		case errors.Is(err, branchroom_service.ErrInvalidTeamGrantTarget):
			ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.team_not_found"))
		case errors.Is(err, branchroom_service.ErrGrantDurationExceeded):
			ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.grant_duration_exceeded", setting.Branchroom.MaxGrantDuration.String()))
		case errors.Is(err, util.ErrInvalidArgument):
			ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.grant_window_invalid"))
		default:
			ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.team_grant_create_failed"))
		}
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}

	ctx.Flash.Success(ctx.Tr("repo.settings.branchroom.team_grant_created"))
	ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
}

// BranchroomTeamGrantRevokePost revokes one organization-team grant.
func BranchroomTeamGrantRevokePost(ctx *context.Context) {
	if !requireBranchroomScopeOwner(ctx) {
		return
	}
	scopeID := ctx.PathParamInt64("scopeID")
	grantID := ctx.PathParamInt64("grantID")
	if scopeID <= 0 || grantID <= 0 {
		ctx.NotFound()
		return
	}
	grant, err := branchroom_model.GetGrantByID(ctx, grantID)
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			ctx.NotFound()
			return
		}
		ctx.ServerErrorAuto(err)
		return
	}
	err = branchroom_service.RevokeRepositoryTeamGrant(ctx, branchroom_service.RevokeRepositoryTeamGrantRequest{
		RepoID:  ctx.Repo.Repository.ID,
		ScopeID: scopeID,
		GrantID: grantID,
		Actor:   branchroom_service.ActorFromUser(ctx.Doer),
		Action:  grant.Action,
		Surface: branchroom_model.SurfaceWeb,
	})
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			ctx.NotFound()
			return
		}
		ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.team_grant_revoke_failed"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}

	ctx.Flash.Success(ctx.Tr("repo.settings.branchroom.team_grant_revoked"))
	ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
}

type branchroomWorkspaceLifecycleFunc func(ctx *context.Context, repoID, scopeID int64, actor branchroom_service.Actor) (*branchroom_model.Workspace, error)

func retryBranchroomWorkspace(ctx *context.Context, repoID, scopeID int64, actor branchroom_service.Actor) (*branchroom_model.Workspace, error) {
	return branchroom_service.RetryWorkspaceProvisioning(ctx, branchroom_service.RetryWorkspaceProvisioningRequest{
		RepoID:  repoID,
		ScopeID: scopeID,
		Actor:   actor,
		Surface: branchroom_model.SurfaceWeb,
	})
}

func retireBranchroomWorkspace(ctx *context.Context, repoID, scopeID int64, actor branchroom_service.Actor) (*branchroom_model.Workspace, error) {
	return branchroom_service.RetireWorkspace(ctx, branchroom_service.RetireWorkspaceRequest{
		RepoID:  repoID,
		ScopeID: scopeID,
		Actor:   actor,
		Surface: branchroom_model.SurfaceWeb,
	})
}

func purgeBranchroomWorkspace(ctx *context.Context, repoID, scopeID int64, actor branchroom_service.Actor) (*branchroom_model.Workspace, error) {
	return branchroom_service.PurgeWorkspace(ctx, branchroom_service.PurgeWorkspaceRequest{
		RepoID:  repoID,
		ScopeID: scopeID,
		Actor:   actor,
		Surface: branchroom_model.SurfaceWeb,
	})
}

func branchroomWorkspaceLifecyclePost(ctx *context.Context, run branchroomWorkspaceLifecycleFunc, successMessage, failedMessage string) {
	if !requireBranchroomScopeOwner(ctx) {
		return
	}
	scopeID := ctx.PathParamInt64("scopeID")
	if scopeID <= 0 {
		ctx.NotFound()
		return
	}
	if _, err := run(ctx, ctx.Repo.Repository.ID, scopeID, branchroom_service.ActorFromUser(ctx.Doer)); err != nil {
		if errors.Is(err, util.ErrNotExist) {
			ctx.NotFound()
			return
		}
		if errors.Is(err, branchroom_service.ErrWorkspaceStateChanged) {
			ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.workspace_state_changed"))
		} else {
			ctx.Flash.Error(ctx.Tr(failedMessage))
		}
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}

	ctx.Flash.Success(ctx.Tr(successMessage))
	ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
}

// BranchroomWorkspaceRetryPost moves a failed workspace back to provisioning.
func BranchroomWorkspaceRetryPost(ctx *context.Context) {
	branchroomWorkspaceLifecyclePost(ctx, retryBranchroomWorkspace,
		"repo.settings.branchroom.workspace_retry_started", "repo.settings.branchroom.workspace_retry_failed")
}

// BranchroomWorkspaceRetirePost marks a ready or failed workspace retired.
func BranchroomWorkspaceRetirePost(ctx *context.Context) {
	branchroomWorkspaceLifecyclePost(ctx, retireBranchroomWorkspace,
		"repo.settings.branchroom.workspace_retired", "repo.settings.branchroom.workspace_retire_failed")
}

// BranchroomWorkspacePurgePost deletes the isolated store and marks the workspace purged.
func BranchroomWorkspacePurgePost(ctx *context.Context) {
	branchroomWorkspaceLifecyclePost(ctx, purgeBranchroomWorkspace,
		"repo.settings.branchroom.workspace_purged", "repo.settings.branchroom.workspace_purge_failed")
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

func revokeBranchroomSourceWriteGrant(ctx *context.Context, repoID, scopeID, grantID int64, actor branchroom_service.Actor) error {
	return branchroom_service.RevokeRepositorySourceWriteGrant(ctx, branchroom_service.RevokeRepositorySourceWriteGrantRequest{
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
		ctx.NotFound()
		return
	}
	err := revokeGrant(ctx, ctx.Repo.Repository.ID, scopeID, grantID, branchroom_service.ActorFromUser(ctx.Doer))
	if err != nil {
		if errors.Is(err, util.ErrNotExist) {
			ctx.NotFound()
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
			ctx.NotFound()
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
	if !setting.Branchroom.Enabled || ctx.Doer == nil || context.IsDoerSessionImpersonated(ctx.Session) || ctx.Doer.MustChangePassword || !ctx.Repo.Repository.UnitEnabled(ctx, unit_model.TypeCode) {
		ctx.NotFound()
		return false
	}
	owner, err := branchroom_service.CanManageRepositoryScopes(ctx, ctx.Repo.Repository.ID, branchroom_service.ActorFromUser(ctx.Doer))
	if err != nil {
		ctx.ServerErrorAuto(err)
		return false
	}
	if !owner {
		ctx.NotFound()
		return false
	}
	return true
}

// BranchroomGrantRenewPost renews a grant for the repository owner.
func BranchroomGrantRenewPost(ctx *context.Context) {
	branchroomDirectUserGrantRevokePost(ctx, func(ctx *context.Context, repoID, scopeID, grantID int64, actor branchroom_service.Actor) error {
		expires, ok := parseBranchroomLocalDatetime(ctx.FormString("expires"))
		if !ok || expires == 0 {
			return util.NewInvalidArgumentErrorf("invalid renewal expiry")
		}
		return branchroom_service.RenewRepositoryGrant(ctx, branchroom_service.RenewRepositoryGrantRequest{ExpiresUnix: expires, RepoID: repoID, ScopeID: scopeID, GrantID: grantID, Actor: actor, Surface: branchroom_model.SurfaceWeb})
	}, "repo.settings.branchroom.grant_renew_failed", "repo.settings.branchroom.grant_renewed")
}

func BranchroomRemovePersonPost(ctx *context.Context) {
	if !requireBranchroomScopeOwner(ctx) {
		return
	}
	if err := db.WithTx(ctx, func(txCtx stdcontext.Context) error {
		userID := ctx.PathParamInt64("userID")
		actor := branchroom_service.ActorFromUser(ctx.Doer)
		if err := branchroom_service.RemoveRepositoryPerson(txCtx, ctx.Repo.Repository.ID, userID, actor); err != nil {
			return err
		}
		if !ctx.FormBool("remove_collaborator") {
			return nil
		}
		subject, err := user_model.GetUserByID(txCtx, userID)
		if err != nil {
			return err
		}
		if err := repo_service.DeleteCollaboration(txCtx, ctx.Repo.Repository, subject); err != nil {
			return err
		}
		return branchroom_service.RecordRepositoryCollaboratorRemoval(txCtx, ctx.Repo.Repository.ID, userID, actor)
	}); err != nil {
		if errors.Is(err, util.ErrNotExist) {
			ctx.NotFound()
			return
		}
		ctx.ServerErrorAuto(err)
		return
	}
	ctx.Flash.Success(ctx.Tr("repo.settings.branchroom.person_removed"))
	ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
}
