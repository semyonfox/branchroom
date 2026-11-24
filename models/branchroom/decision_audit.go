// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"

	"gitea.dev/models/db"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/google/uuid"
)

var ErrAuditConflict = errors.New("branchroom audit head changed")

// Decision records the safe result of one policy evaluation.
type Decision struct {
	ID              int64              `xorm:"pk autoincr"`
	DecisionKey     string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	ScopeID         int64              `xorm:"INDEX(scope_created) NOT NULL"`
	RepoID          int64              `xorm:"NOT NULL"`
	GrantID         int64              `xorm:"INDEX NOT NULL DEFAULT 0"`
	ActorType       ActorType          `xorm:"VARCHAR(16) INDEX(actor_created) NOT NULL"`
	ActorID         int64              `xorm:"INDEX(actor_created) NOT NULL"`
	Action          Action             `xorm:"VARCHAR(32) NOT NULL"`
	ResourceType    ResourceType       `xorm:"VARCHAR(32) NOT NULL"`
	ResourceID      string             `xorm:"VARCHAR(255) NOT NULL"`
	Surface         Surface            `xorm:"VARCHAR(32) NOT NULL"`
	Result          DecisionResult     `xorm:"VARCHAR(8) NOT NULL"`
	ReasonCode      string             `xorm:"VARCHAR(64) NOT NULL"`
	OrdinaryAllowed bool               `xorm:"NOT NULL"`
	RequestID       string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	ExpiresUnix     timeutil.TimeStamp `xorm:"INDEX NOT NULL DEFAULT 0"`
	CreatedUnix     timeutil.TimeStamp `xorm:"INDEX(scope_created) INDEX(actor_created) NOT NULL"`
}

// TableName returns the stable Branchroom decision table name.
func (Decision) TableName() string { return "branchroom_decision" }

// Validate checks that a decision carries only bounded safe metadata.
func (decision *Decision) Validate() error {
	if !IsCanonicalUUID(decision.DecisionKey) || decision.ScopeID <= 0 || decision.RepoID <= 0 || decision.GrantID < 0 || !IsActorTypeValid(decision.ActorType) || !IsActionValid(decision.Action) || !IsResourceTypeValid(decision.ResourceType) || !IsSurfaceValid(decision.Surface) || !IsDecisionResultValid(decision.Result) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom decision metadata")
	}
	if !isActorIDValid(decision.ActorType, decision.ActorID) || !isSafeLogicalID(decision.ResourceID, maxResourceIDLength) || !isReasonCode(decision.ReasonCode) || !isOptionalSafeLogicalID(decision.RequestID, maxRequestIDLength) || decision.ExpiresUnix <= 0 || decision.CreatedUnix != 0 && decision.ExpiresUnix < decision.CreatedUnix {
		return util.NewInvalidArgumentErrorf("invalid Branchroom decision safe metadata")
	}
	return nil
}

// AuditEvent is one immutable, hash-linked event in a scope audit stream.
type AuditEvent struct {
	ID              int64              `xorm:"pk autoincr"`
	EventKey        string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	DecisionKey     string             `xorm:"CHAR(36) UNIQUE NOT NULL"`
	ScopeID         int64              `xorm:"UNIQUE(scope_sequence) INDEX(scope_created) NOT NULL"`
	Sequence        int64              `xorm:"UNIQUE(scope_sequence) NOT NULL"`
	RepoID          int64              `xorm:"NOT NULL"`
	GrantID         int64              `xorm:"NOT NULL DEFAULT 0"`
	ActorType       ActorType          `xorm:"VARCHAR(16) INDEX(actor_created) NOT NULL"`
	ActorID         int64              `xorm:"INDEX(actor_created) NOT NULL"`
	Action          Action             `xorm:"VARCHAR(32) NOT NULL"`
	ResourceType    ResourceType       `xorm:"VARCHAR(32) NOT NULL"`
	ResourceID      string             `xorm:"VARCHAR(255) NOT NULL"`
	Surface         Surface            `xorm:"VARCHAR(32) NOT NULL"`
	Result          DecisionResult     `xorm:"VARCHAR(8) NOT NULL"`
	ReasonCode      string             `xorm:"VARCHAR(64) NOT NULL"`
	OrdinaryAllowed bool               `xorm:"NOT NULL"`
	RequestID       string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	PreviousHash    string             `xorm:"VARCHAR(64) NOT NULL DEFAULT ''"`
	EventHash       string             `xorm:"CHAR(64) UNIQUE NOT NULL"`
	CreatedUnix     timeutil.TimeStamp `xorm:"INDEX(scope_created) INDEX(actor_created) NOT NULL"`
}

// TableName returns the stable Branchroom audit-event table name.
func (AuditEvent) TableName() string { return "branchroom_audit_event" }

// Validate checks structural invariants for an immutable audit event.
func (event *AuditEvent) Validate() error {
	if !IsCanonicalUUID(event.EventKey) || !IsCanonicalUUID(event.DecisionKey) || event.ScopeID <= 0 || event.Sequence <= 0 || event.RepoID <= 0 || event.GrantID < 0 || !IsActorTypeValid(event.ActorType) || !IsActionValid(event.Action) || !IsResourceTypeValid(event.ResourceType) || !IsSurfaceValid(event.Surface) || !IsDecisionResultValid(event.Result) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom audit metadata")
	}
	if !isActorIDValid(event.ActorType, event.ActorID) || !isSafeLogicalID(event.ResourceID, maxResourceIDLength) || !isReasonCode(event.ReasonCode) || !isOptionalSafeLogicalID(event.RequestID, maxRequestIDLength) || !isSHA256Hex(event.EventHash) || event.EventHash != event.ComputeHash() || event.CreatedUnix <= 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom audit safe metadata")
	}
	if event.Sequence == 1 && event.PreviousHash != "" || event.Sequence > 1 && !isSHA256Hex(event.PreviousHash) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom audit chain")
	}
	return nil
}

// ComputeHash returns the canonical hash for event without using its database ID or hash field.
func (event *AuditEvent) ComputeHash() string {
	hash := sha256.New()
	for _, field := range []string{
		"branchroom-audit-v1",
		event.EventKey,
		event.DecisionKey,
		strconv.FormatInt(event.ScopeID, 10),
		strconv.FormatInt(event.Sequence, 10),
		strconv.FormatInt(event.RepoID, 10),
		strconv.FormatInt(event.GrantID, 10),
		string(event.ActorType),
		strconv.FormatInt(event.ActorID, 10),
		string(event.Action),
		string(event.ResourceType),
		event.ResourceID,
		string(event.Surface),
		string(event.Result),
		event.ReasonCode,
		strconv.FormatBool(event.OrdinaryAllowed),
		event.RequestID,
		event.PreviousHash,
		strconv.FormatInt(int64(event.CreatedUnix), 10),
	} {
		_, _ = hash.Write([]byte(strconv.Itoa(len(field))))
		_, _ = hash.Write([]byte{':'})
		_, _ = hash.Write([]byte(field))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// RecordDecisionAndAudit writes a decision and its audit event atomically.
func RecordDecisionAndAudit(ctx context.Context, scope *Scope, decision *Decision) (*AuditEvent, error) {
	if scope == nil || decision == nil || scope.ID <= 0 {
		return nil, util.NewInvalidArgumentErrorf("Branchroom scope and decision are required")
	}

	return recordDecisionAndAuditOnce(ctx, scope, decision)
}

func recordDecisionAndAuditOnce(ctx context.Context, scope *Scope, decision *Decision) (*AuditEvent, error) {
	var event *AuditEvent
	err := db.WithTx(ctx, func(txCtx context.Context) error {
		current, err := GetScopeByID(txCtx, scope.ID)
		if err != nil {
			return err
		}
		if decision.ScopeID != current.ID || decision.RepoID != current.RepoID {
			return util.NewInvalidArgumentErrorf("Branchroom decision does not match its scope")
		}

		now := timeutil.TimeStampNow()
		if decision.DecisionKey == "" {
			decision.DecisionKey = uuid.NewString()
		}
		if decision.CreatedUnix == 0 {
			decision.CreatedUnix = now
		}
		if decision.ExpiresUnix == 0 {
			decision.ExpiresUnix = decision.CreatedUnix
		}
		event = &AuditEvent{
			EventKey:        uuid.NewString(),
			DecisionKey:     decision.DecisionKey,
			ScopeID:         current.ID,
			Sequence:        current.AuditSequence + 1,
			RepoID:          current.RepoID,
			GrantID:         decision.GrantID,
			ActorType:       decision.ActorType,
			ActorID:         decision.ActorID,
			Action:          decision.Action,
			ResourceType:    decision.ResourceType,
			ResourceID:      decision.ResourceID,
			Surface:         decision.Surface,
			Result:          decision.Result,
			ReasonCode:      decision.ReasonCode,
			OrdinaryAllowed: decision.OrdinaryAllowed,
			RequestID:       decision.RequestID,
			PreviousHash:    current.AuditHeadHash,
			CreatedUnix:     decision.CreatedUnix,
		}
		event.EventHash = event.ComputeHash()

		affected, err := db.GetEngine(txCtx).ID(current.ID).
			Where("audit_sequence = ? AND audit_head_hash = ?", current.AuditSequence, current.AuditHeadHash).
			Cols("audit_sequence", "audit_head_hash").
			Update(&Scope{AuditSequence: event.Sequence, AuditHeadHash: event.EventHash})
		if err != nil {
			return err
		}
		if affected != 1 {
			return ErrAuditConflict
		}
		if err := InsertDecision(txCtx, decision); err != nil {
			return err
		}
		if err := insertAuditEvent(txCtx, event); err != nil {
			return err
		}
		*scope = *current
		scope.AuditSequence = event.Sequence
		scope.AuditHeadHash = event.EventHash
		return nil
	})
	return event, err
}

// InsertDecision validates and inserts a decision through the caller's transaction context.
func InsertDecision(ctx context.Context, decision *Decision) error {
	if decision == nil {
		return util.NewInvalidArgumentErrorf("Branchroom decision is required")
	}
	if err := decision.Validate(); err != nil {
		return err
	}
	return db.Insert(ctx, decision)
}

// insertAuditEvent validates and inserts an immutable audit event through the caller's transaction context.
func insertAuditEvent(ctx context.Context, event *AuditEvent) error {
	if event == nil {
		return util.NewInvalidArgumentErrorf("Branchroom audit event is required")
	}
	if err := event.Validate(); err != nil {
		return err
	}
	return db.Insert(ctx, event)
}
