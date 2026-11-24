// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"

	"gitea.dev/modelmigration/base"
	"gitea.dev/modules/timeutil"
)

type branchroomScopeMigration struct {
	ID            int64
	Kind          string             `xorm:"VARCHAR(32) NOT NULL"`
	RepoID        int64              `xorm:"UNIQUE(repo_lower_name) INDEX(repo_state) NOT NULL"`
	Name          string             `xorm:"VARCHAR(255) NOT NULL"`
	LowerName     string             `xorm:"VARCHAR(255) UNIQUE(repo_lower_name) NOT NULL"`
	State         string             `xorm:"VARCHAR(16) INDEX(repo_state) NOT NULL"`
	OwnerType     string             `xorm:"VARCHAR(16) INDEX(owner) NOT NULL"`
	OwnerID       int64              `xorm:"INDEX(owner) NOT NULL"`
	CreatedByID   int64              `xorm:"NOT NULL"`
	AuditSequence int64              `xorm:"NOT NULL DEFAULT 0"`
	AuditHeadHash string             `xorm:"CHAR(64) NOT NULL DEFAULT ''"`
	CreatedUnix   timeutil.TimeStamp `xorm:"created NOT NULL"`
	UpdatedUnix   timeutil.TimeStamp `xorm:"updated NOT NULL"`
}

func (branchroomScopeMigration) TableName() string { return "branchroom_scope" }

type branchroomGrantMigration struct {
	ID                int64
	ScopeID           int64              `xorm:"INDEX(scope_action_subject) NOT NULL"`
	SubjectType       string             `xorm:"VARCHAR(16) INDEX(scope_action_subject) NOT NULL"`
	SubjectID         int64              `xorm:"INDEX(scope_action_subject) NOT NULL"`
	Action            string             `xorm:"VARCHAR(32) INDEX(scope_action_subject) NOT NULL"`
	WorkflowID        string             `xorm:"VARCHAR(255) NOT NULL DEFAULT ''"`
	WorkflowRepoID    int64              `xorm:"NOT NULL DEFAULT 0"`
	WorkflowCommitSHA string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	Ref               string             `xorm:"VARCHAR(255) NOT NULL DEFAULT ''"`
	RunnerID          int64              `xorm:"NOT NULL DEFAULT 0"`
	NotBeforeUnix     timeutil.TimeStamp `xorm:"INDEX NOT NULL DEFAULT 0"`
	ExpiresUnix       timeutil.TimeStamp `xorm:"INDEX NOT NULL DEFAULT 0"`
	RevokedUnix       timeutil.TimeStamp `xorm:"INDEX NOT NULL DEFAULT 0"`
	RevokedByID       int64              `xorm:"NOT NULL DEFAULT 0"`
	CreatedByID       int64              `xorm:"NOT NULL"`
	CreatedUnix       timeutil.TimeStamp `xorm:"created NOT NULL"`
	UpdatedUnix       timeutil.TimeStamp `xorm:"updated NOT NULL"`
}

func (branchroomGrantMigration) TableName() string { return "branchroom_grant" }

type branchroomDecisionMigration struct {
	ID              int64
	DecisionKey     string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	ScopeID         int64              `xorm:"INDEX(scope_created) NOT NULL"`
	RepoID          int64              `xorm:"NOT NULL"`
	GrantID         int64              `xorm:"INDEX NOT NULL DEFAULT 0"`
	ActorType       string             `xorm:"VARCHAR(16) INDEX(actor_created) NOT NULL"`
	ActorID         int64              `xorm:"INDEX(actor_created) NOT NULL"`
	Action          string             `xorm:"VARCHAR(32) NOT NULL"`
	ResourceType    string             `xorm:"VARCHAR(32) NOT NULL"`
	ResourceID      string             `xorm:"VARCHAR(255) NOT NULL"`
	Surface         string             `xorm:"VARCHAR(32) NOT NULL"`
	Result          string             `xorm:"VARCHAR(8) NOT NULL"`
	ReasonCode      string             `xorm:"VARCHAR(64) NOT NULL"`
	OrdinaryAllowed bool               `xorm:"NOT NULL"`
	RequestID       string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	ExpiresUnix     timeutil.TimeStamp `xorm:"INDEX NOT NULL DEFAULT 0"`
	CreatedUnix     timeutil.TimeStamp `xorm:"created INDEX(scope_created) INDEX(actor_created) NOT NULL"`
}

func (branchroomDecisionMigration) TableName() string { return "branchroom_decision" }

type branchroomAuditEventMigration struct {
	ID              int64
	EventKey        string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	DecisionKey     string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	ScopeID         int64              `xorm:"UNIQUE(scope_sequence) INDEX(scope_created) NOT NULL"`
	Sequence        int64              `xorm:"UNIQUE(scope_sequence) NOT NULL"`
	RepoID          int64              `xorm:"NOT NULL"`
	GrantID         int64              `xorm:"NOT NULL DEFAULT 0"`
	ActorType       string             `xorm:"VARCHAR(16) INDEX(actor_created) NOT NULL"`
	ActorID         int64              `xorm:"INDEX(actor_created) NOT NULL"`
	Action          string             `xorm:"VARCHAR(32) NOT NULL"`
	ResourceType    string             `xorm:"VARCHAR(32) NOT NULL"`
	ResourceID      string             `xorm:"VARCHAR(255) NOT NULL"`
	Surface         string             `xorm:"VARCHAR(32) NOT NULL"`
	Result          string             `xorm:"VARCHAR(8) NOT NULL"`
	ReasonCode      string             `xorm:"VARCHAR(64) NOT NULL"`
	OrdinaryAllowed bool               `xorm:"NOT NULL"`
	RequestID       string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	PreviousHash    string             `xorm:"CHAR(64) NOT NULL DEFAULT ''"`
	EventHash       string             `xorm:"CHAR(64) UNIQUE NOT NULL"`
	CreatedUnix     timeutil.TimeStamp `xorm:"created INDEX(scope_created) INDEX(actor_created) NOT NULL"`
}

func (branchroomAuditEventMigration) TableName() string { return "branchroom_audit_event" }

// AddBranchroomPolicySchema creates the append-only policy records used by Branchroom.
func AddBranchroomPolicySchema(_ context.Context, x base.EngineMigration) error {
	return x.Sync(
		new(branchroomScopeMigration),
		new(branchroomGrantMigration),
		new(branchroomDecisionMigration),
		new(branchroomAuditEventMigration),
	)
}
