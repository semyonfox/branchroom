// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"fmt"
)

// VerifyScopeAuditChain rereads a scope's stored audit events and checks that every event
// hashes to itself, links to its predecessor, and that the scope head names the last event.
func VerifyScopeAuditChain(ctx context.Context, scope *Scope) error {
	scope, events, err := LoadScopeAuditSnapshot(ctx, scope.ID)
	if err != nil {
		return err
	}
	return VerifyAuditEvents(scope, events)
}

type AuditChainError struct {
	Sequence int64
	Problem  string
}

func (err *AuditChainError) Error() string { return err.Problem }

// VerifyAuditEvents checks an ordered snapshot against its scope head.
func VerifyAuditEvents(scope *Scope, events []*AuditEvent) error {
	previous := ""
	for i, event := range events {
		if event.Sequence != int64(i+1) {
			return &AuditChainError{int64(i + 1), fmt.Sprintf("audit event %d is missing", i+1)}
		}
		if event.PreviousHash != previous {
			return &AuditChainError{event.Sequence, fmt.Sprintf("audit event %d does not link to its predecessor", event.Sequence)}
		}
		if event.ComputeHash() != event.EventHash {
			return &AuditChainError{event.Sequence, fmt.Sprintf("audit event %d does not match its hash", event.Sequence)}
		}
		previous = event.EventHash
	}
	if scope.AuditSequence != int64(len(events)) || scope.AuditHeadHash != previous {
		return &AuditChainError{max(1, min(scope.AuditSequence, int64(len(events))+1)), "scope head does not match its last audit event"}
	}
	return nil
}
