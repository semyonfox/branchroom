// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"sync"
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/modules/git"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	branchroom_service "gitea.dev/services/branchroom"
	"gitea.dev/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchroomOwnerQuotaHoldsUnderConcurrentProvisioning(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	defer test.MockVariableValue(&setting.Branchroom, setting.BranchroomConfig{Enabled: true, RestrictedRepoRootPath: branchroomTestRoot(t), MaxWorkspacesPerOwner: 1})()
	ctx := t.Context()

	requests := make([]branchroom_service.CreateRepositoryWorkspaceRequest, 0, 2)
	for _, repoID := range []int64{1, 2} {
		repository, err := repo_model.GetRepositoryByID(ctx, repoID)
		require.NoError(t, err)
		require.EqualValues(t, 2, repository.OwnerID)
		base, err := git.GetBranchCommitID(ctx, repository.CodeStorageRepo(), repository.DefaultBranch)
		require.NoError(t, err)
		owner := branchroom_service.Actor{Type: branchroom_model.ActorTypeUser, ID: 2}
		scope, err := branchroom_service.CreateRepositoryScope(ctx, branchroom_service.CreateRepositoryScopeRequest{RepoID: repoID, Actor: owner, Name: "quota", Surface: branchroom_model.SurfaceSystem})
		require.NoError(t, err)
		requests = append(requests, branchroom_service.CreateRepositoryWorkspaceRequest{RepoID: repoID, ScopeID: scope.ID, Actor: owner, PublicBaseSHA: base, Surface: branchroom_model.SurfaceSystem})
	}

	errs := make([]error, len(requests))
	var wg sync.WaitGroup
	for i, request := range requests {
		wg.Go(func() {
			_, errs[i] = branchroom_service.CreateRepositoryWorkspace(ctx, request)
		})
	}
	wg.Wait()

	succeeded := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else {
			assert.ErrorIs(t, err, branchroom_service.ErrWorkspaceQuotaReached)
		}
	}
	assert.Equal(t, 1, succeeded)
}
