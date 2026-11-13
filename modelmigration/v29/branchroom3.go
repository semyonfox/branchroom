// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"context"

	"gitea.dev/modelmigration/base"
	"gitea.dev/modules/timeutil"
)

type branchroomEnvironmentRevisionMigration struct {
	ID                   int64
	RevisionKey          string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	ScopeID              int64              `xorm:"UNIQUE(scope_revision) INDEX(scope_state) NOT NULL"`
	RepoID               int64              `xorm:"INDEX(repo_state) NOT NULL"`
	Revision             int64              `xorm:"UNIQUE(scope_revision) NOT NULL"`
	CipherSuite          string             `xorm:"VARCHAR(32) NOT NULL"`
	PayloadNonce         []byte             `xorm:"BLOB NOT NULL"`
	PayloadCiphertext    []byte             `xorm:"BLOB NOT NULL"`
	WrappedDEKNonce      []byte             `xorm:"BLOB NOT NULL"`
	WrappedDEKCiphertext []byte             `xorm:"BLOB NOT NULL"`
	KeyProvider          string             `xorm:"VARCHAR(32) NOT NULL"`
	KeyReference         string             `xorm:"VARCHAR(255) NOT NULL"`
	CiphertextDigest     string             `xorm:"CHAR(64) NOT NULL"`
	PlaintextLength      int64              `xorm:"NOT NULL"`
	State                string             `xorm:"VARCHAR(16) INDEX(scope_state) INDEX(repo_state) NOT NULL"`
	CreatedByID          int64              `xorm:"NOT NULL"`
	RetiredUnix          timeutil.TimeStamp `xorm:"INDEX NOT NULL DEFAULT 0"`
	CreatedUnix          timeutil.TimeStamp `xorm:"created NOT NULL"`
	UpdatedUnix          timeutil.TimeStamp `xorm:"updated NOT NULL"`
}

func (branchroomEnvironmentRevisionMigration) TableName() string {
	return "branchroom_environment_revision"
}

type branchroomEnvironmentLeaseMigration struct {
	ID               int64
	LeaseKey         string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	BearerSecretHash string             `xorm:"CHAR(64) UNIQUE NOT NULL"`
	ScopeID          int64              `xorm:"INDEX(scope_revision_state) NOT NULL"`
	RevisionID       int64              `xorm:"INDEX(scope_revision_state) INDEX NOT NULL"`
	ActorType        string             `xorm:"VARCHAR(16) INDEX(actor_state) NOT NULL"`
	ActorID          int64              `xorm:"INDEX(actor_state) NOT NULL"`
	GrantID          int64              `xorm:"INDEX NOT NULL"`
	DecisionID       int64              `xorm:"INDEX NOT NULL"`
	State            string             `xorm:"VARCHAR(16) INDEX(scope_revision_state) INDEX(actor_state) INDEX(state_expiry) NOT NULL"`
	Outcome          string             `xorm:"VARCHAR(16) NOT NULL"`
	IssuedUnix       timeutil.TimeStamp `xorm:"NOT NULL"`
	ExpiresUnix      timeutil.TimeStamp `xorm:"INDEX(state_expiry) NOT NULL"`
	ClaimedUnix      timeutil.TimeStamp `xorm:"NOT NULL DEFAULT 0"`
	CreatedUnix      timeutil.TimeStamp `xorm:"created NOT NULL"`
	UpdatedUnix      timeutil.TimeStamp `xorm:"updated NOT NULL"`
}

func (branchroomEnvironmentLeaseMigration) TableName() string {
	return "branchroom_environment_lease"
}

// AddBranchroomEnvironmentSchema creates encrypted environment revisions and one-use leases.
func AddBranchroomEnvironmentSchema(_ context.Context, x base.EngineMigration) error {
	return x.Sync(
		new(branchroomEnvironmentRevisionMigration),
		new(branchroomEnvironmentLeaseMigration),
	)
}
