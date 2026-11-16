// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package mirror

import (
	"context"
	"errors"
	"fmt"

	audit_model "gitea.dev/models/audit"
	"gitea.dev/models/db"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/util"
	"gitea.dev/services/audit"
	repo_service "gitea.dev/services/repository"
)

// ErrFinalPullFailed means the source could not be pulled one last time, so nothing was changed
var ErrFinalPullFailed = errors.New("the last pull from the mirror source failed")

// MakePrimary turns a pull mirror into a regular repository that pushes to pushAddr, the source it used to pull from
func MakePrimary(ctx context.Context, repo *repo_model.Repository, pushAddr string) error {
	remoteAddress, err := util.SanitizeURL(pushAddr)
	if err != nil {
		return err
	}
	// the push mirror overwrites the source, so nothing there may be newer than what is here
	if !SyncPullMirror(ctx, repo.ID) {
		return ErrFinalPullFailed
	}

	repo.IsMirror = false
	if _, err := repo_service.CleanUpMigrateInfo(ctx, repo); err != nil {
		return fmt.Errorf("MakePrimary: %w", err)
	}
	if err := repo_model.DeleteRepoPullMirror(ctx, repo.ID); err != nil {
		return fmt.Errorf("MakePrimary: %w", err)
	}
	audit.Record(ctx, audit_model.RepositoryConvertMirror, repo)

	pushMirror := &repo_model.PushMirror{
		RepoID:        repo.ID,
		Repo:          repo,
		RemoteName:    "remote_mirror_" + util.CryptoRandomString(10),
		SyncOnCommit:  true,
		Interval:      setting.Mirror.DefaultInterval,
		RemoteAddress: remoteAddress,
	}
	if err := db.Insert(ctx, pushMirror); err != nil {
		return fmt.Errorf("MakePrimary: %w", err)
	}
	if err := AddPushMirrorRemote(ctx, pushMirror, pushAddr); err != nil {
		return fmt.Errorf("MakePrimary: %w", err)
	}
	audit.Record(ctx, audit_model.RepositoryMirrorPushAdd, repo, "mirror_id", pushMirror.ID, "remote_address", pushMirror.RemoteAddress)

	AddPushMirrorToQueue(pushMirror.ID)
	return nil
}
