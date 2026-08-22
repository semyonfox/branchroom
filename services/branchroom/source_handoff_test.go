// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"testing"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	repo_model "gitea.dev/models/repo"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListRepositorySourceHandoffsRequiresBothDirectGrants(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	ensureTestRepositoryCodeUnit(t.Context(), t)

	firstScope, firstWorkspace := createSourceHandoffWorkspace(t, repository, baseSHA, "alpha-source")
	secondScope, secondWorkspace := createSourceHandoffWorkspace(t, repository, baseSHA, "zulu-source")
	request := ListRepositorySourceHandoffsRequest{RepoID: repository.ID, UserID: 4}

	assertSourceHandoffDenied(t.Context(), t, request)
	insertWorkspaceActionGrant(t, firstScope.ID, request.UserID, branchroom_model.ActionReadSource, 0, 0)
	assertSourceHandoffDenied(t.Context(), t, request)
	insertWorkspaceActionGrant(t, firstScope.ID, request.UserID, branchroom_model.ActionDiscover, 0, 0)

	decisionsBefore, auditsBefore := branchroomSourceHandoffCounts(t)
	storedRepository, err := repo_model.GetRepositoryByID(t.Context(), repository.ID)
	require.NoError(t, err)
	updatedBefore := storedRepository.UpdatedUnix
	handoffs, err := ListRepositorySourceHandoffs(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, []RepositorySourceHandoff{{ScopeName: firstScope.Name, RouteKey: firstWorkspace.RouteKey}}, handoffs)
	storedRepository, err = repo_model.GetRepositoryByID(t.Context(), repository.ID)
	require.NoError(t, err)
	assert.Equal(t, updatedBefore, storedRepository.UpdatedUnix)
	assertBranchroomSourceHandoffCounts(t, decisionsBefore, auditsBefore)

	insertWorkspaceActionGrant(t, secondScope.ID, request.UserID, branchroom_model.ActionDiscover, 0, 0)
	secondGrant := insertWorkspaceActionGrant(t, secondScope.ID, request.UserID, branchroom_model.ActionReadSource, 0, 0)
	handoffs, err = ListRepositorySourceHandoffs(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, []RepositorySourceHandoff{
		{ScopeName: firstScope.Name, RouteKey: firstWorkspace.RouteKey},
		{ScopeName: secondScope.Name, RouteKey: secondWorkspace.RouteKey},
	}, handoffs)
	require.NoError(t, branchroom_model.RevokeGrant(t.Context(), secondGrant.ID, repository.OwnerID, timeutil.TimeStampNow()))
	handoffs, err = ListRepositorySourceHandoffs(t.Context(), request)
	require.NoError(t, err)
	assert.Equal(t, []RepositorySourceHandoff{{ScopeName: firstScope.Name, RouteKey: firstWorkspace.RouteKey}}, handoffs)
	assertBranchroomSourceHandoffCounts(t, decisionsBefore, auditsBefore)
}

func TestListRepositorySourceHandoffsFailsClosed(t *testing.T) {
	_, repository, baseSHA := prepareWorkspaceSourceRepository(t)
	ensureTestRepositoryCodeUnit(t.Context(), t)
	scope, _ := createSourceHandoffWorkspace(t, repository, baseSHA, "source-handoff-lifecycle")
	request := ListRepositorySourceHandoffsRequest{RepoID: repository.ID, UserID: 4}
	insertWorkspaceActionGrant(t, scope.ID, request.UserID, branchroom_model.ActionDiscover, 0, 0)
	insertWorkspaceActionGrant(t, scope.ID, request.UserID, branchroom_model.ActionReadSource, 0, 0)
	decisionsBefore, auditsBefore := branchroomSourceHandoffCounts(t)

	defer test.MockVariableValue(&setting.Repository.DisableHTTPGit)()
	setting.Repository.DisableHTTPGit = true
	assertSourceHandoffDenied(t.Context(), t, request)
	setting.Repository.DisableHTTPGit = false

	_, err := db.GetEngine(t.Context()).ID(repository.ID).Cols("is_private").Update(&repo_model.Repository{IsPrivate: true})
	require.NoError(t, err)
	assertSourceHandoffDenied(t.Context(), t, request)
	_, err = db.GetEngine(t.Context()).ID(repository.ID).Cols("is_private").Update(&repo_model.Repository{IsPrivate: false})
	require.NoError(t, err)

	require.NoError(t, db.WithTx(t.Context(), func(txCtx context.Context) error {
		assertSourceHandoffDenied(txCtx, t, request)
		return nil
	}))
	assertSourceHandoffDenied(t.Context(), t, ListRepositorySourceHandoffsRequest{RepoID: repository.ID, UserID: repository.OwnerID})
	require.NoError(t, user_model.UpdateUserCols(t.Context(), &user_model.User{ID: request.UserID, MustChangePassword: true}, "must_change_password"))
	assertSourceHandoffDenied(t.Context(), t, request)
	require.NoError(t, user_model.UpdateUserCols(t.Context(), &user_model.User{ID: request.UserID, MustChangePassword: false}, "must_change_password"))
	assertBranchroomSourceHandoffCounts(t, decisionsBefore, auditsBefore)
}

func createSourceHandoffWorkspace(t *testing.T, repository *repo_model.Repository, baseSHA, name string) (*branchroom_model.Scope, *branchroom_model.Workspace) {
	t.Helper()
	scope := insertWorkspaceScope(t, repository.ID, repository.OwnerID, name)
	workspace, err := CreateRepositoryWorkspace(t.Context(), CreateRepositoryWorkspaceRequest{
		RepoID:        repository.ID,
		ScopeID:       scope.ID,
		Actor:         Actor{Type: branchroom_model.ActorTypeUser, ID: repository.OwnerID},
		PublicBaseSHA: baseSHA,
		Surface:       branchroom_model.SurfaceSystem,
	})
	require.NoError(t, err)
	return scope, workspace
}

func assertSourceHandoffDenied(ctx context.Context, t *testing.T, request ListRepositorySourceHandoffsRequest) {
	t.Helper()
	handoffs, err := ListRepositorySourceHandoffs(ctx, request)
	assert.Nil(t, handoffs)
	assert.ErrorIs(t, err, util.ErrNotExist)
	assert.Equal(t, "Branchroom source handoff does not exist", err.Error())
}

func branchroomSourceHandoffCounts(t *testing.T) (int64, int64) {
	t.Helper()
	decisions, err := db.GetEngine(t.Context()).Count(new(branchroom_model.Decision))
	require.NoError(t, err)
	audits, err := db.GetEngine(t.Context()).Count(new(branchroom_model.AuditEvent))
	require.NoError(t, err)
	return decisions, audits
}

func assertBranchroomSourceHandoffCounts(t *testing.T, decisions, audits int64) {
	t.Helper()
	actualDecisions, actualAudits := branchroomSourceHandoffCounts(t)
	assert.Equal(t, decisions, actualDecisions)
	assert.Equal(t, audits, actualAudits)
}
