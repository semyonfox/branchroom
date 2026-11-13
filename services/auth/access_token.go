// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"strings"

	auth_model "gitea.dev/models/auth"
	access_model "gitea.dev/models/perm/access"
	repo_model "gitea.dev/models/repo"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/util"
)

func ResolveTokenRepositories(ctx context.Context, doer *user_model.User, names []string) ([]int64, error) {
	ids := make([]int64, 0, len(names))
	for _, name := range names {
		owner, repoName, ok := strings.Cut(name, "/")
		if !ok || owner == "" || repoName == "" {
			return nil, util.NewInvalidArgumentErrorf("invalid repository name: %s", name)
		}
		repo, err := repo_model.GetRepositoryByOwnerAndName(ctx, owner, repoName)
		if err != nil {
			return nil, err
		}
		permission, err := access_model.GetDoerRepoPermission(ctx, repo, doer)
		if err != nil {
			return nil, err
		}
		if !permission.HasAnyUnitAccessOrPublicAccess() {
			return nil, util.NewNotExistErrorf("repository not found")
		}
		ids = append(ids, repo.ID)
	}
	return ids, nil
}

func TokenRepositoryNames(ctx context.Context, t *auth_model.AccessToken) ([]string, error) {
	names := make([]string, 0, len(t.RepositoryIDs))
	for _, id := range t.RepositoryIDs {
		repo, err := repo_model.GetRepositoryByID(ctx, id)
		if repo_model.IsErrRepoNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := repo.LoadOwner(ctx); err != nil {
			return nil, err
		}
		names = append(names, repo.FullName())
	}
	return names, nil
}
