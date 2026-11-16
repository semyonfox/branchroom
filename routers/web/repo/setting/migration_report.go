// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"net/http"

	repo_model "gitea.dev/models/repo"
	"gitea.dev/modules/migration"
	"gitea.dev/modules/templates"
	"gitea.dev/services/context"
)

const tplMigrationReport templates.TplName = "repo/settings/migration_report"

type migrationReportSection struct {
	Kind  migration.SettingsNoteKind
	Link  string // where in the settings the notes get resolved
	Notes []migration.SettingsNote
}

var migrationReportLinks = map[migration.SettingsNoteKind]string{
	migration.SettingsNoteSecret:           "/settings/actions/secrets",
	migration.SettingsNoteOrgSecret:        "/settings/actions/secrets",
	migration.SettingsNoteOrgVariable:      "/settings/actions/variables",
	migration.SettingsNoteVariable:         "/settings/actions/variables",
	migration.SettingsNoteWebhook:          "/settings/hooks",
	migration.SettingsNoteWebhookSkipped:   "/settings/hooks",
	migration.SettingsNoteCollaborator:     "/settings/collaboration",
	migration.SettingsNoteTeam:             "/settings/collaboration",
	migration.SettingsNoteTeamAccess:       "/settings/collaboration",
	migration.SettingsNoteBranchProtection: "/settings/branches",
	migration.SettingsNoteRuleset:          "/settings/branches",
}

// MigrationReport shows what a migration copied and what it left for a person
func MigrationReport(ctx *context.Context) {
	ctx.Data["Title"] = ctx.Tr("repo.migrate.report.title")
	ctx.Data["PageIsSettingsMigrationReport"] = true

	report, err := repo_model.GetMigrationReport(ctx, ctx.Repo.Repository.ID)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	if report == nil {
		ctx.NotFound()
		return
	}

	notes := make(map[migration.SettingsNoteKind][]migration.SettingsNote)
	for _, note := range report.Report.Notes {
		notes[note.Kind] = append(notes[note.Kind], note)
	}
	var sections []migrationReportSection
	for _, kind := range migration.SettingsNoteKinds {
		if len(notes[kind]) > 0 {
			sections = append(sections, migrationReportSection{Kind: kind, Link: migrationReportLinks[kind], Notes: notes[kind]})
		}
	}
	ctx.Data["Report"] = report.Report
	ctx.Data["ReportSections"] = sections
	ctx.HTML(http.StatusOK, tplMigrationReport)
}

// MigrationReportDismiss removes the report once its owner is done with it
func MigrationReportDismiss(ctx *context.Context) {
	if err := repo_model.DeleteMigrationReport(ctx, ctx.Repo.Repository.ID); err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	ctx.JSONRedirect(ctx.Repo.RepoLink + "/settings")
}
