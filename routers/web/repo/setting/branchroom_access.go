// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"errors"
	"slices"
	"strings"
	"time"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
	"gitea.dev/modules/web"
	branchroom_service "gitea.dev/services/branchroom"
	"gitea.dev/services/context"
	"gitea.dev/services/forms"
)

// access levels from lowest to highest, each needs every level before it
var branchroomAccessLevels = []branchroom_model.Action{
	branchroom_model.ActionDiscover,
	branchroom_model.ActionReadSource,
	branchroom_model.ActionWriteSource,
}

var branchroomRevokeByAction = map[branchroom_model.Action]branchroomGrantRevokeFunc{
	branchroom_model.ActionDiscover:    revokeBranchroomDiscoveryGrant,
	branchroom_model.ActionReadSource:  revokeBranchroomSourceReadGrant,
	branchroom_model.ActionWriteSource: revokeBranchroomSourceWriteGrant,
}

// branchroomPersonAccess is one direct user's highest access level in a scope.
type branchroomPersonAccess struct {
	UserID      int64
	Name        string
	Level       branchroom_model.Action
	ExpiresUnix timeutil.TimeStamp
	ExpiresSoon bool
}

// groupBranchroomPeople folds a person's grants into one row, showing the earliest expiry since access ends there.
func groupBranchroomPeople(data branchroomScopeData, now timeutil.TimeStamp) []branchroomPersonAccess {
	byUser := map[int64]*branchroomPersonAccess{}
	add := func(level branchroom_model.Action, grants []branchroomDirectUserGrantData) {
		for _, grant := range grants {
			person, ok := byUser[grant.SubjectID]
			if !ok {
				person = &branchroomPersonAccess{UserID: grant.SubjectID, Name: grant.SubjectName, Level: level, ExpiresUnix: grant.ExpiresUnix}
				byUser[grant.SubjectID] = person
			}
			if slices.Index(branchroomAccessLevels, level) > slices.Index(branchroomAccessLevels, person.Level) {
				person.Level = level
			}
			if grant.ExpiresUnix != 0 && (person.ExpiresUnix == 0 || grant.ExpiresUnix < person.ExpiresUnix) {
				person.ExpiresUnix = grant.ExpiresUnix
			}
		}
	}
	add(branchroom_model.ActionDiscover, data.DiscoveryGrants)
	add(branchroom_model.ActionReadSource, data.SourceReadGrants)
	add(branchroom_model.ActionWriteSource, data.SourceWriteGrants)

	people := make([]branchroomPersonAccess, 0, len(byUser))
	for _, person := range byUser {
		person.ExpiresSoon = person.ExpiresUnix != 0 && person.ExpiresUnix <= now.AddDuration(7*24*time.Hour)
		people = append(people, *person)
	}
	slices.SortFunc(people, func(a, b branchroomPersonAccess) int { return strings.Compare(a.Name, b.Name) })
	return people
}

// findBranchroomPersonGrants returns the user's unrevoked, unexpired direct grants in one scope of this repository.
func findBranchroomPersonGrants(ctx *context.Context, scopeID, userID int64) ([]*branchroom_model.Grant, error) {
	grants := make([]*branchroom_model.Grant, 0, len(branchroomAccessLevels))
	now := timeutil.TimeStampNow()
	err := db.GetEngine(ctx).Table("branchroom_grant").
		Join("INNER", "branchroom_scope", "branchroom_scope.id = branchroom_grant.scope_id").
		Where("branchroom_scope.repo_id = ? AND branchroom_grant.scope_id = ?", ctx.Repo.Repository.ID, scopeID).
		And("branchroom_grant.subject_type = ? AND branchroom_grant.subject_id = ?", branchroom_model.SubjectTypeUser, userID).
		And("branchroom_grant.revoked_unix = 0 AND (branchroom_grant.expires_unix = 0 OR branchroom_grant.expires_unix > ?)", now).
		In("branchroom_grant.action", branchroomAccessLevels).
		Select("branchroom_grant.*").Find(&grants)
	if err != nil {
		return nil, err
	}
	// revoke from the top so a lower grant is never removed while a higher one still stands
	slices.SortFunc(grants, func(a, b *branchroom_model.Grant) int {
		return slices.Index(branchroomAccessLevels, b.Action) - slices.Index(branchroomAccessLevels, a.Action)
	})
	return grants, nil
}

// BranchroomAccessPost sets one direct user's access level: missing lower grants are created and higher ones revoked.
func BranchroomAccessPost(ctx *context.Context) {
	if !requireBranchroomScopeOwner(ctx) {
		return
	}
	form := web.GetForm[*forms.BranchroomAccessForm](ctx)
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
	level := slices.Index(branchroomAccessLevels, branchroom_model.Action(form.Access))
	if level < 0 {
		ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.grant_create_failed"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}

	type step struct {
		create        branchroomGrantCreateFunc
		exists        error
		failedMessage string
	}
	steps := []step{
		{createBranchroomDiscoveryGrant, branchroom_service.ErrDiscoveryGrantAlreadyExists, "repo.settings.branchroom.grant_create_failed"},
		{createBranchroomSourceReadGrant, branchroom_service.ErrSourceReadGrantAlreadyExists, "repo.settings.branchroom.source_grant_create_failed"},
		{createBranchroomSourceWriteGrant, branchroom_service.ErrSourceWriteGrantAlreadyExists, "repo.settings.branchroom.source_write_grant_create_failed"},
	}
	actor := branchroom_service.ActorFromUser(ctx.Doer)
	changed := 0
	for _, s := range steps[:level+1] {
		err := s.create(ctx, ctx.Repo.Repository.ID, form.ScopeID, actor, form.Username, notBefore, expires)
		if errors.Is(err, s.exists) {
			continue
		}
		if err != nil {
			branchroomGrantCreateError(ctx, err, s.failedMessage)
			return
		}
		changed++
	}

	// the discovery grant above already proved the user exists and may hold grants here
	subject, err := user_model.GetUserByName(ctx, form.Username)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	grants, err := findBranchroomPersonGrants(ctx, form.ScopeID, subject.ID)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	for _, grant := range grants {
		if slices.Index(branchroomAccessLevels, grant.Action) <= level {
			continue
		}
		if err := branchroomRevokeByAction[grant.Action](ctx, ctx.Repo.Repository.ID, form.ScopeID, grant.ID, actor); err != nil {
			ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.access_change_failed"))
			ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
			return
		}
		changed++
	}

	if changed == 0 {
		ctx.Flash.Info(ctx.Tr("repo.settings.branchroom.access_unchanged", form.Username))
	} else {
		ctx.Flash.Success(ctx.Tr("repo.settings.branchroom.access_given", form.Username))
	}
	ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
}

func branchroomPersonGrantsFromPath(ctx *context.Context) (int64, []*branchroom_model.Grant, bool) {
	if !requireBranchroomScopeOwner(ctx) {
		return 0, nil, false
	}
	scopeID, userID := ctx.PathParamInt64("scopeID"), ctx.PathParamInt64("userID")
	if scopeID <= 0 || userID <= 0 {
		ctx.NotFound()
		return 0, nil, false
	}
	grants, err := findBranchroomPersonGrants(ctx, scopeID, userID)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return 0, nil, false
	}
	if len(grants) == 0 {
		ctx.NotFound()
		return 0, nil, false
	}
	return scopeID, grants, true
}

// BranchroomPersonRevokePost removes every direct grant one person holds in a scope.
func BranchroomPersonRevokePost(ctx *context.Context) {
	scopeID, grants, ok := branchroomPersonGrantsFromPath(ctx)
	if !ok {
		return
	}
	actor := branchroom_service.ActorFromUser(ctx.Doer)
	for _, grant := range grants {
		if err := branchroomRevokeByAction[grant.Action](ctx, ctx.Repo.Repository.ID, scopeID, grant.ID, actor); err != nil {
			if errors.Is(err, util.ErrNotExist) {
				ctx.NotFound()
				return
			}
			ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.access_change_failed"))
			ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
			return
		}
	}
	ctx.Flash.Success(ctx.Tr("repo.settings.branchroom.access_removed"))
	ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
}

// BranchroomPersonRenewPost moves the expiry of every direct grant one person holds in a scope.
func BranchroomPersonRenewPost(ctx *context.Context) {
	scopeID, grants, ok := branchroomPersonGrantsFromPath(ctx)
	if !ok {
		return
	}
	expires, ok := parseBranchroomLocalDatetime(ctx.FormString("expires"))
	if !ok || expires == 0 {
		ctx.Flash.Error(ctx.Tr("repo.settings.branchroom.grant_window_invalid"))
		ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
		return
	}
	actor := branchroom_service.ActorFromUser(ctx.Doer)
	for _, grant := range grants {
		err := branchroom_service.RenewRepositoryGrant(ctx, branchroom_service.RenewRepositoryGrantRequest{
			ExpiresUnix: expires, RepoID: ctx.Repo.Repository.ID, ScopeID: scopeID, GrantID: grant.ID, Actor: actor, Surface: branchroom_model.SurfaceWeb,
		})
		if err != nil {
			branchroomGrantCreateError(ctx, err, "repo.settings.branchroom.grant_renew_failed")
			return
		}
	}
	ctx.Flash.Success(ctx.Tr("repo.settings.branchroom.grant_renewed"))
	ctx.Redirect(ctx.Repo.RepoLink + "/settings/branchroom")
}
