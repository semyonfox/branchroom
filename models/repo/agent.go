// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"context"

	branchroom_model "gitea.dev/models/branchroom"
)

func (repo *Repository) AgentPushReceipts(ctx context.Context, sha string) ([]*branchroom_model.PushReceipt, error) {
	return branchroom_model.GetPushReceipts(ctx, repo.ID, sha)
}
