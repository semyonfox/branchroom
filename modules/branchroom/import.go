// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"errors"
	"fmt"

	"gitea.dev/modules/git"
	"gitea.dev/modules/git/gitcmd"
	"gitea.dev/modules/git/gitrepo"
)

// ImportPublicBase copies a public bare repository into an empty restricted store.
func ImportPublicBase(ctx context.Context, public, restricted gitrepo.RepositoryFacade) error {
	if public == nil {
		return errors.New("public repository must be set")
	}
	if restricted == nil {
		return errors.New("restricted repository must be set")
	}

	exists, err := git.IsRepositoryExist(ctx, restricted)
	if err != nil {
		return fmt.Errorf("check restricted store: %w", err)
	}
	if exists {
		return errors.New("restricted store already exists")
	}

	if err := git.CloneManaged(ctx, public, restricted, git.CloneRepoOptions{
		Mirror:      true,
		NoLocal:     true,
		NoHardlinks: true,
	}); err != nil {
		return fmt.Errorf("import public base: %w", err)
	}

	if _, _, err := gitcmd.NewCommand("remote", "remove", "origin").WithRepo(restricted).RunStdString(ctx); err != nil {
		return fmt.Errorf("remove public remote: %w", err)
	}
	return nil
}
