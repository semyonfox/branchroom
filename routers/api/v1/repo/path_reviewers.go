// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"net/http"

	git_model "gitea.dev/models/git"
	"gitea.dev/models/organization"
	"gitea.dev/models/perm"
	"gitea.dev/models/unit"
	api "gitea.dev/modules/structs"
	"gitea.dev/modules/web"
	"gitea.dev/services/context"
)

func GetPathReviewerRules(ctx *context.APIContext) {
	// swagger:operation GET /repos/{owner}/{repo}/branch_protections/path_reviewers/{name} repository repoGetPathReviewerRules
	// ---
	// summary: List path reviewer rules for a protected branch
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: owner
	//   in: path
	//   type: string
	//   required: true
	// - name: repo
	//   in: path
	//   type: string
	//   required: true
	// - name: name
	//   in: path
	//   description: protected branch rule name
	//   type: string
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/PathReviewerRules"
	//   default:
	//     "$ref": "#/responses/error"

	pb, err := git_model.GetProtectedBranchRuleByName(ctx, ctx.Repo.Repository.ID, ctx.PathParam("*"))
	if err != nil {
		ctx.APIErrorInternal(err)
		return
	}
	if pb == nil {
		ctx.APIErrorNotFound()
		return
	}
	writePathReviewerRules(ctx, pb.ID)
}

func SetPathReviewerRules(ctx *context.APIContext) {
	// swagger:operation PUT /repos/{owner}/{repo}/branch_protections/path_reviewers/{name} repository repoSetPathReviewerRules
	// ---
	// summary: Replace path reviewer rules for a protected branch
	// consumes:
	// - application/json
	// produces:
	// - application/json
	// parameters:
	// - name: owner
	//   in: path
	//   type: string
	//   required: true
	// - name: repo
	//   in: path
	//   type: string
	//   required: true
	// - name: name
	//   in: path
	//   description: protected branch rule name
	//   type: string
	//   required: true
	// - name: body
	//   in: body
	//   required: true
	//   schema:
	//     "$ref": "#/definitions/SetPathReviewerRulesOptions"
	// responses:
	//   "200":
	//     "$ref": "#/responses/PathReviewerRules"
	//   default:
	//     "$ref": "#/responses/error"

	pb, err := git_model.GetProtectedBranchRuleByName(ctx, ctx.Repo.Repository.ID, ctx.PathParam("*"))
	if err != nil {
		ctx.APIErrorInternal(err)
		return
	}
	if pb == nil {
		ctx.APIErrorNotFound()
		return
	}
	form := web.GetForm[*api.SetPathReviewerRulesOptions](ctx)
	rules := make([]*git_model.BranchroomPathReviewer, 0, len(form.Rules))
	for _, input := range form.Rules {
		rule := &git_model.BranchroomPathReviewer{Pattern: input.Pattern, CodeOwners: input.CodeOwners, RequiredApprovals: input.RequiredApprovals}
		if input.Team != "" {
			ids, err := organization.GetTeamIDsByNames(ctx, ctx.Repo.Repository.OwnerID, []string{input.Team}, false)
			if err != nil {
				ctx.APIError(http.StatusUnprocessableEntity, "team does not exist in the repository's organisation")
				return
			}
			if len(ids) != 1 {
				ctx.APIError(http.StatusUnprocessableEntity, "require one team")
				return
			}
			team, err := organization.GetTeamByID(ctx, ids[0])
			if err != nil {
				ctx.APIErrorInternal(err)
				return
			}
			if !organization.HasTeamRepo(ctx, team.OrgID, team.ID, ctx.Repo.Repository.ID) || team.UnitAccessMode(ctx, unit.TypeCode) < perm.AccessModeWrite {
				ctx.APIError(http.StatusUnprocessableEntity, "team must have code write access to this repository")
				return
			}
			rule.TeamID = team.ID
		}
		if err := git_model.ValidatePathReviewerRule(rule); err != nil {
			ctx.APIError(http.StatusUnprocessableEntity, err.Error())
			return
		}
		rules = append(rules, rule)
	}
	if err := git_model.ReplacePathReviewerRules(ctx, pb, rules); err != nil {
		ctx.APIErrorInternal(err)
		return
	}
	writePathReviewerRules(ctx, pb.ID)
}

func writePathReviewerRules(ctx *context.APIContext, branchID int64) {
	rules, err := git_model.GetPathReviewerRules(ctx, branchID)
	if err != nil {
		ctx.APIErrorInternal(err)
		return
	}
	result := make([]api.PathReviewerRule, 0, len(rules))
	for _, rule := range rules {
		item := api.PathReviewerRule{Pattern: rule.Pattern, TeamID: rule.TeamID, CodeOwners: rule.CodeOwners, RequiredApprovals: rule.RequiredApprovals}
		if rule.TeamID > 0 {
			team, err := organization.GetTeamByID(ctx, rule.TeamID)
			if err != nil && !organization.IsErrTeamNotExist(err) {
				ctx.APIErrorInternal(err)
				return
			}
			if team != nil {
				item.Team = team.Name
			}
		}
		result = append(result, item)
	}
	ctx.JSON(http.StatusOK, result)
}
