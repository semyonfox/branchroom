// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package git

import (
	"context"
	"errors"

	"gitea.dev/models/db"
	"gitea.dev/modules/glob"
	"gitea.dev/modules/timeutil"
)

type BranchroomPathReviewer struct {
	ID                int64  `xorm:"pk autoincr"`
	RepoID            int64  `xorm:"INDEX NOT NULL"`
	ProtectedBranchID int64  `xorm:"INDEX NOT NULL"`
	Pattern           string `xorm:"VARCHAR(255) NOT NULL"`
	TeamID            int64
	CodeOwners        bool
	RequiredApprovals int64
}

func init() {
	db.RegisterModel(new(BranchroomPathReviewer))
}

func GetPathReviewerRules(ctx context.Context, branchID int64) ([]*BranchroomPathReviewer, error) {
	rules := make([]*BranchroomPathReviewer, 0)
	err := db.GetEngine(ctx).Where("protected_branch_id = ?", branchID).OrderBy("id").Find(&rules)
	return rules, err
}

func ValidatePathReviewerRule(rule *BranchroomPathReviewer) error {
	if rule.Pattern == "" || len(rule.Pattern) > 255 || rule.RequiredApprovals < 1 || (rule.TeamID > 0) == rule.CodeOwners {
		return errors.New("ValidatePathReviewerRule: require a path pattern, positive approval count and exactly one reviewer source")
	}
	_, err := glob.Compile(rule.Pattern, '/')
	return err
}

func ReplacePathReviewerRules(ctx context.Context, pb *ProtectedBranch, rules []*BranchroomPathReviewer) error {
	for _, rule := range rules {
		if err := ValidatePathReviewerRule(rule); err != nil {
			return err
		}
	}
	return db.WithTx(ctx, func(ctx context.Context) error {
		_, err := db.GetEngine(ctx).ID(pb.ID).Where("repo_id = ?", pb.RepoID).Cols("updated_unix").Update(&ProtectedBranch{UpdatedUnix: timeutil.TimeStampNow()})
		if err != nil {
			return err
		}
		exists, err := db.GetEngine(ctx).Where("id = ? AND repo_id = ?", pb.ID, pb.RepoID).Exist(new(ProtectedBranch))
		if err != nil {
			return err
		}
		if !exists {
			return errors.New("ReplacePathReviewerRules: protected branch no longer exists")
		}
		if _, err := db.GetEngine(ctx).Where("protected_branch_id = ?", pb.ID).Delete(new(BranchroomPathReviewer)); err != nil {
			return err
		}
		for _, rule := range rules {
			rule.ID, rule.RepoID, rule.ProtectedBranchID = 0, pb.RepoID, pb.ID
			if _, err := db.GetEngine(ctx).Insert(rule); err != nil {
				return err
			}
		}
		return nil
	})
}
