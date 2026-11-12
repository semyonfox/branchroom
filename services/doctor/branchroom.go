// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package doctor

import (
	"context"
	"fmt"

	"gitea.dev/modules/log"
	"gitea.dev/modules/setting"
	branchroom_service "gitea.dev/services/branchroom"
)

func checkBranchroom(ctx context.Context, logger log.Logger, autofix bool) error {
	report, err := branchroom_service.CheckIntegrity(ctx, autofix)
	if err != nil {
		return err
	}
	for _, repaired := range report.Repaired {
		logger.Info("Branchroom: %s", repaired)
	}
	for _, problem := range report.Problems {
		logger.Warn("Branchroom: %s", problem)
	}
	if !setting.Branchroom.Enabled {
		logger.Info("Branchroom is disabled, restricted stores were not checked")
	}
	if len(report.Problems) > 0 {
		return fmt.Errorf("found %d Branchroom problems", len(report.Problems))
	}
	logger.Info("Checked %d Branchroom scopes", report.Scopes)
	return nil
}

func init() {
	Register(&Check{
		Title:     "Check Branchroom audit chains, restricted stores and receive hooks",
		Name:      "branchroom",
		IsDefault: false,
		Run:       checkBranchroom,
		Priority:  1,
	})
}
