// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"net/http"

	git_model "gitea.dev/models/git"
	issues_model "gitea.dev/models/issues"
	api "gitea.dev/modules/structs"
	"gitea.dev/modules/util"
	"gitea.dev/services/context"
	"gitea.dev/services/convert"
	pull_service "gitea.dev/services/pull"
)

// GetPullRequestReadiness reports whether a pull request can merge
func GetPullRequestReadiness(ctx *context.APIContext) {
	// swagger:operation GET /repos/{owner}/{repo}/pulls/{index}/readiness repository repoGetPullRequestReadiness
	// ---
	// summary: Get whether a pull request can merge and everything that blocks it
	// description: Send the last fingerprint as If-None-Match to get 304 when nothing changed.
	// produces:
	// - application/json
	// parameters:
	// - name: owner
	//   in: path
	//   description: owner of the repo
	//   type: string
	//   required: true
	// - name: repo
	//   in: path
	//   description: name of the repo
	//   type: string
	//   required: true
	// - name: index
	//   in: path
	//   description: index of the pull request
	//   type: integer
	//   format: int64
	//   required: true
	// responses:
	//   "200":
	//     "$ref": "#/responses/PullRequestReadiness"
	//   "304":
	//     description: unchanged since the fingerprint sent in If-None-Match
	//   default:
	//     "$ref": "#/responses/error"

	pr, err := issues_model.GetPullRequestByIndex(ctx, ctx.Repo.Repository.ID, ctx.PathParamInt64("index"))
	if err != nil {
		if issues_model.IsErrPullRequestNotExist(err) {
			ctx.APIErrorNotFound()
		} else {
			ctx.APIErrorInternal(err)
		}
		return
	}
	pull_service.StartPullRequestCheckOnView(ctx, pr)

	r, err := pull_service.GetReadiness(ctx, pr)
	if err != nil {
		ctx.APIErrorInternal(err)
		return
	}

	etag := `"` + r.Fingerprint + `"`
	ctx.Resp.Header().Set("ETag", etag)
	if ctx.Req.Header.Get("If-None-Match") == etag {
		ctx.Status(http.StatusNotModified)
		return
	}

	git_model.CommitStatusesApplyDoerPermission(ctx, ctx.Doer, r.Statuses)
	blockers := make([]string, len(r.Blockers))
	for i, b := range r.Blockers {
		blockers[i] = string(b)
	}
	ctx.JSON(http.StatusOK, &api.PullRequestReadiness{
		HeadSHA:        r.HeadSHA,
		Draft:          r.Draft,
		Mergeable:      r.Mergeable,
		ReviewDecision: string(r.ReviewDecision),
		ChecksState:    string(r.ChecksState),
		RequiredChecks: util.SliceNilAsEmpty(r.RequiredChecks),
		Checks:         convert.ToCommitStatuses(ctx, r.Statuses),
		Blockers:       blockers,
		Ready:          len(blockers) == 0,
		Fingerprint:    r.Fingerprint,
	})
}

// parsePullFilters reads the checks and review list filters, answering 422 for unknown values
func parsePullFilters(ctx *context.APIContext) (issues_model.PullChecksFilter, issues_model.PullReviewFilter, bool) {
	checksStr, reviewStr := ctx.FormTrim("checks"), ctx.FormTrim("review")
	checks, review := issues_model.ParsePullChecksFilter(checksStr), issues_model.ParsePullReviewFilter(reviewStr)
	if checksStr != "" && checks == "" {
		ctx.APIError(http.StatusUnprocessableEntity, "checks must be success, failure or pending")
		return "", "", false
	}
	if reviewStr != "" && review == "" {
		ctx.APIError(http.StatusUnprocessableEntity, "review must be approved, changes_requested or review_required")
		return "", "", false
	}
	return checks, review, true
}
