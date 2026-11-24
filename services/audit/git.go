// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package audit

import (
	"context"

	audit_model "gitea.dev/models/audit"
	repo_model "gitea.dev/models/repo"
	user_model "gitea.dev/models/user"
)

// RecordGitAccess records one authenticated git session against a repository, so a leak can be traced to who
// read the code and from where. Anonymous reads of public repositories carry no identity and are not recorded.
func RecordGitAccess(ctx context.Context, doer *user_model.User, repo *repo_model.Repository, write, isWiki bool) {
	if doer == nil || repo == nil {
		return
	}
	action := audit_model.RepositoryGitRead
	if write {
		action = audit_model.RepositoryGitWrite
	}
	RecordAs(ctx, doer, action, repo, "wiki", isWiki)
}

// RecordArchiveDownload records a signed-in download of a source archive, which carries the whole tree like a clone.
func RecordArchiveDownload(ctx context.Context, repo *repo_model.Repository, name string) {
	if doer := doerFromContext(ctx); doer != nil {
		RecordAs(ctx, doer, audit_model.RepositoryDownloadArchive, repo, "archive", name)
	}
}
