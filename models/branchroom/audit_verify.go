// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"fmt"

	"gitea.dev/models/db"
)

// VerifyScopeAuditChain rereads a scope's stored audit events and checks that every event
// hashes to itself, links to its predecessor, and that the scope head names the last event.
func VerifyScopeAuditChain(ctx context.Context, scope *Scope) error {
	events := make([]*AuditEvent, 0, scope.AuditSequence)
	if err := db.GetEngine(ctx).Where("scope_id = ?", scope.ID).Asc("sequence").Find(&events); err != nil {
		return err
	}
	previous := ""
	for i, event := range events {
		if event.Sequence != int64(i+1) {
			return fmt.Errorf("audit event %d is missing", i+1)
		}
		if event.PreviousHash != previous {
			return fmt.Errorf("audit event %d does not link to its predecessor", event.Sequence)
		}
		if event.ComputeHash() != event.EventHash {
			return fmt.Errorf("audit event %d does not match its hash", event.Sequence)
		}
		previous = event.EventHash
	}
	if scope.AuditSequence != int64(len(events)) || scope.AuditHeadHash != previous {
		return fmt.Errorf("scope head does not match its last audit event")
	}
	return nil
}
