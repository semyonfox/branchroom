// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package migration

// RepoSettings is what a source says about how a repository is run, as opposed to what is in it
type RepoSettings struct {
	BranchProtections []*BranchProtection
	Collaborators     []*Collaborator
	Teams             []*Team
	Webhooks          []*Webhook
	Variables         []*ActionsVariable
	Notes             []SettingsNote // what the source could not hand over
}

// BranchProtection is a branch protection rule
type BranchProtection struct {
	Pattern                 string
	BlockPush               bool
	AllowForcePush          bool
	AllowDeletion           bool
	RequiredApprovals       int64
	DismissStaleApprovals   bool
	RequireCodeOwnerReviews bool
	EnableStatusCheck       bool
	StatusCheckContexts     []string
	RequireUpToDateBranch   bool
	RequireSignedCommits    bool
	EnforceForAdmins        bool
	Unsupported             []string // source settings with no counterpart, in the source's own words
}

// Collaborator is a person with direct access to the repository
type Collaborator struct {
	ExternalID string // the account's id at the source, as a sign-in with that source reports it
	Login      string
	Permission string // admin, write or read
}

// Team is a team with access to the repository
type Team struct {
	Name       string
	Slug       string
	Permission string // admin, write or read
}

// Webhook is a repository webhook, without its secret because no source gives that back
type Webhook struct {
	URL               string
	ContentType       string // json or form
	Events            []string
	SendEverything    bool
	UnsupportedEvents []string
}

// ActionsVariable is a plain-text Actions variable
type ActionsVariable struct {
	Name  string
	Value string
}

// SettingsNoteKind groups notes in the report, each kind has its own heading and explanation
type SettingsNoteKind string

const (
	SettingsNoteSecret           SettingsNoteKind = "secret"
	SettingsNoteOrgSecret        SettingsNoteKind = "org_secret"
	SettingsNoteOrgVariable      SettingsNoteKind = "org_variable"
	SettingsNoteVariable         SettingsNoteKind = "variable"
	SettingsNoteEnvironment      SettingsNoteKind = "environment"
	SettingsNoteWebhook          SettingsNoteKind = "webhook"
	SettingsNoteWebhookSkipped   SettingsNoteKind = "webhook_skipped"
	SettingsNoteCollaborator     SettingsNoteKind = "collaborator"
	SettingsNoteTeam             SettingsNoteKind = "team"
	SettingsNoteTeamAccess       SettingsNoteKind = "team_access"
	SettingsNoteBranchProtection SettingsNoteKind = "branch_protection"
	SettingsNoteRuleset          SettingsNoteKind = "ruleset"
	SettingsNoteUnreadable       SettingsNoteKind = "unreadable"
	SettingsNoteNotAdmin         SettingsNoteKind = "not_admin"
	SettingsNoteUnsupported      SettingsNoteKind = "unsupported"
)

// SettingsNoteKinds is the order the report shows them in, what needs doing first
var SettingsNoteKinds = []SettingsNoteKind{
	SettingsNoteUnsupported,
	SettingsNoteNotAdmin,
	SettingsNoteUnreadable,
	SettingsNoteSecret,
	SettingsNoteOrgSecret,
	SettingsNoteOrgVariable,
	SettingsNoteVariable,
	SettingsNoteEnvironment,
	SettingsNoteWebhook,
	SettingsNoteWebhookSkipped,
	SettingsNoteCollaborator,
	SettingsNoteTeam,
	SettingsNoteTeamAccess,
	SettingsNoteBranchProtection,
	SettingsNoteRuleset,
}

// SettingsNote is one thing a person has to finish or should know after the import
type SettingsNote struct {
	Kind   SettingsNoteKind
	Name   string `json:",omitempty"`
	Detail string `json:",omitempty"`
}

// SettingsReport is what the settings import did and what it left for a person
type SettingsReport struct {
	BranchProtections int
	Collaborators     int
	Teams             int
	Webhooks          int
	Variables         int
	Notes             []SettingsNote
}
