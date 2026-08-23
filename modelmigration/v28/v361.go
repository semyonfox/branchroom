// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v28

import (
	"context"

	"gitea.dev/modelmigration/base"

	"xorm.io/xorm"
)

type branchroomReceiveSessionOutcomeMigration struct {
	Outcome string `xorm:"VARCHAR(16) NOT NULL DEFAULT 'pending'"`
}

func (branchroomReceiveSessionOutcomeMigration) TableName() string {
	return "branchroom_receive_session"
}

type branchroomReceiveSessionOutcomeBackfill struct {
	ID      int64
	State   string
	Outcome string
}

func (branchroomReceiveSessionOutcomeBackfill) TableName() string {
	return "branchroom_receive_session"
}

// branchroomReceiveSessionBackfillOutcome maps a pre-outcome row to its outcome.
// It reports whether the row already carries the target (completed rows finished after the
// migration keep denied or failed).
func branchroomReceiveSessionBackfillOutcome(state, outcome string) (string, bool) {
	switch state {
	case "issued", "claimed":
		return "pending", outcome == "pending"
	case "expired":
		return "expired", outcome == "expired"
	case "revoked":
		return "revoked", outcome == "revoked"
	case "completed":
		switch outcome {
		case "denied", "failed", "legacy":
			return "", true
		}
		return "legacy", false
	default:
		return "", true
	}
}

// AddBranchroomReceiveSessionOutcomes records terminal outcomes for receive sessions and
// backfills existing rows defensively.
func AddBranchroomReceiveSessionOutcomes(_ context.Context, x base.EngineMigration) error {
	if _, err := x.SyncWithOptions(xorm.SyncOptions{
		IgnoreDropIndices: true,
		IgnoreConstrains:  true,
	}, new(branchroomReceiveSessionOutcomeMigration)); err != nil {
		return err
	}

	sessions := make([]*branchroomReceiveSessionOutcomeBackfill, 0)
	if err := x.Table("branchroom_receive_session").Asc("id").Find(&sessions); err != nil {
		return err
	}
	for _, session := range sessions {
		outcome, settled := branchroomReceiveSessionBackfillOutcome(session.State, session.Outcome)
		if settled {
			continue
		}
		if _, err := x.Table("branchroom_receive_session").ID(session.ID).
			Cols("outcome").
			Update(&branchroomReceiveSessionOutcomeBackfill{Outcome: outcome}); err != nil {
			return err
		}
	}
	return nil
}
