// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package inbox

import (
	"testing"

	git_model "gitea.dev/models/git"
	issues_model "gitea.dev/models/issues"
	"gitea.dev/modules/commitstatus"
	pull_service "gitea.dev/services/pull"

	"github.com/stretchr/testify/assert"
)

func TestBlockedReason(t *testing.T) {
	r := &pull_service.Readiness{ChecksState: commitstatus.CommitStatusFailure, ReviewDecision: issues_model.PullReviewChangesRequested, Blockers: []pull_service.ReadinessBlocker{pull_service.BlockerChecksFailing, pull_service.BlockerChangesRequested, pull_service.BlockerConflict}, Statuses: []*git_model.CommitStatus{{Context: "test / test (pull_request)", State: commitstatus.CommitStatusFailure, TargetURL: "https://example.org/log"}}}
	reason, action := blockedReason(r, "https://example.org/pull")
	assert.Equal(t, "Checks failing: test / test (pull_request). Changes requested. Merge conflict", reason)
	assert.Equal(t, "Open log", action.Label)
	assert.Equal(t, "https://example.org/log", action.URL)
	reason, _ = blockedReason(&pull_service.Readiness{}, "")
	assert.Empty(t, reason)
}
