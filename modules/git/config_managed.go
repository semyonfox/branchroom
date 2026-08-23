// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package git

import (
	"bytes"
	"context"
	"fmt"

	"gitea.dev/modules/git/gitcmd"
)

// ManagedConfigAdd add a git configuration key to a specific value for the given repository.
func ManagedConfigAdd(ctx context.Context, repo RepositoryFacade, key, value string) error {
	return LockConfigAndDo(ctx, repo, func(ctx context.Context) error {
		_, _, err := gitcmd.NewCommand("config", "--add").
			AddDynamicArguments(key, value).WithRepo(repo).RunStdString(ctx)
		return err
	})
}

// ManagedConfigSet updates a git configuration key to a specific value for the given repository.
// If the key does not exist, it will be created.
// If the key exists, it will be updated to the new value.
func ManagedConfigSet(ctx context.Context, repo RepositoryFacade, key, value string) error {
	return LockConfigAndDo(ctx, repo, func(ctx context.Context) error {
		_, _, err := gitcmd.NewCommand("config").
			AddDynamicArguments(key, value).WithRepo(repo).RunStdString(ctx)
		return err
	})
}

// ManagedConfigSetAndVerify replaces all values of a git configuration key with a single
// value, then verifies the local config holds exactly that one value byte-for-byte.
// It rejects missing, duplicate, malformed (non NUL-terminated), or mismatched values.
func ManagedConfigSetAndVerify(ctx context.Context, repo RepositoryFacade, key, value string) error {
	return LockConfigAndDo(ctx, repo, func(ctx context.Context) error {
		if _, _, err := gitcmd.NewCommand("config", "--local", "--replace-all").
			AddDynamicArguments(key, value).WithRepo(repo).RunStdString(ctx); err != nil {
			return err
		}
		stdout, _, err := gitcmd.NewCommand("config", "--local", "--get-all", "--null").
			AddDynamicArguments(key).WithRepo(repo).RunStdBytes(ctx)
		if err != nil {
			return err
		}
		if want := []byte(value + "\x00"); !bytes.Equal(stdout, want) {
			return fmt.Errorf("git config %q: expected one value %q, got %q", key, value, stdout)
		}
		return nil
	})
}
