// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/modules/util"
)

// ScopeMetadataRequest identifies one metadata read without exposing a storage location.
type ScopeMetadataRequest struct {
	ScopeID   int64
	RepoID    int64
	Actor     Actor
	Ceiling   RepositoryCeiling
	Surface   branchroom_model.Surface
	RequestID string
}

// ScopeMetadata is the limited information available after a successful discover decision.
type ScopeMetadata struct {
	ID     int64
	RepoID int64
	Kind   branchroom_model.ScopeKind
	Name   string
	State  branchroom_model.ScopeState
}

// GetScopeMetadata is the first protected Branchroom operation.
func GetScopeMetadata(ctx context.Context, evaluator Evaluator, request ScopeMetadataRequest) (*ScopeMetadata, error) {
	if evaluator == nil {
		return nil, util.NewNotExistErrorf("Branchroom scope does not exist")
	}
	decision, err := evaluator.Evaluate(ctx, Request{
		ScopeID:      request.ScopeID,
		RepoID:       request.RepoID,
		Actor:        request.Actor,
		Ceiling:      request.Ceiling,
		Action:       branchroom_model.ActionDiscover,
		ResourceType: branchroom_model.ResourceTypeScope,
		ResourceID:   scopeResourceID(request.ScopeID),
		Surface:      request.Surface,
		RequestID:    request.RequestID,
	})
	if err != nil || !decision.Allowed || decision.Scope == nil {
		return nil, util.NewNotExistErrorf("Branchroom scope does not exist")
	}
	return &ScopeMetadata{
		ID:     decision.Scope.ID,
		RepoID: decision.Scope.RepoID,
		Kind:   decision.Scope.Kind,
		Name:   decision.Scope.Name,
		State:  decision.Scope.State,
	}, nil
}
