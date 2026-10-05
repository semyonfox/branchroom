// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"time"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
)

// ReconcileStaleReceiveSessions marks interrupted receive sessions as failed.
func ReconcileStaleReceiveSessions(ctx context.Context, staleAfter time.Duration) (int64, error) {
	return reconcileStaleReceiveSessions(ctx, timeutil.TimeStampNow(), staleAfter)
}

func reconcileStaleReceiveSessions(ctx context.Context, now timeutil.TimeStamp, staleAfter time.Duration) (int64, error) {
	if now <= 0 || staleAfter < time.Second {
		return 0, util.NewInvalidArgumentErrorf("invalid Branchroom receive session reconciliation")
	}
	return branchroom_model.FailStaleClaimedReceiveSessions(ctx, now.AddDuration(-staleAfter))
}
