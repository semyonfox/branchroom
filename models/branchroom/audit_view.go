// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"

	"gitea.dev/models/db"
)

// FindScopeAuditEvents returns the scope's complete chain in verification order.
func FindScopeAuditEvents(ctx context.Context, scopeID int64) ([]*AuditEvent, error) {
	events := make([]*AuditEvent, 0)
	err := db.GetEngine(ctx).Where("scope_id = ?", scopeID).Asc("sequence").Find(&events)
	return events, err
}

// FindScopeDecisions returns the scope's stored policy results.
func FindScopeDecisions(ctx context.Context, scopeID int64) ([]*Decision, error) {
	decisions := make([]*Decision, 0)
	err := db.GetEngine(ctx).Where("scope_id = ?", scopeID).Asc("id").Find(&decisions)
	return decisions, err
}

// LoadScopeAuditSnapshot retries when an append changes the head during the read.
func LoadScopeAuditSnapshot(ctx context.Context, scopeID int64) (*Scope, []*AuditEvent, error) {
	for range 3 {
		scope, err := GetScopeByID(ctx, scopeID)
		if err != nil {
			return nil, nil, err
		}
		events, err := FindScopeAuditEvents(ctx, scopeID)
		if err != nil {
			return nil, nil, err
		}
		current, err := GetScopeByID(ctx, scopeID)
		if err != nil {
			return nil, nil, err
		}
		if scope.AuditSequence == current.AuditSequence && scope.AuditHeadHash == current.AuditHeadHash {
			return scope, events, nil
		}
	}
	return nil, nil, ErrAuditConflict
}
