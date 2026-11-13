// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package user

import (
	"context"

	"gitea.dev/models/db"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"

	"xorm.io/builder"
)

// SavedReply is a user's canned comment, inserted from the comment editor
type SavedReply struct {
	ID          int64              `xorm:"pk autoincr"`
	UserID      int64              `xorm:"INDEX NOT NULL"`
	Title       string             `xorm:"VARCHAR(255) NOT NULL"`
	Content     string             `xorm:"LONGTEXT NOT NULL"`
	CreatedUnix timeutil.TimeStamp `xorm:"created"`
	UpdatedUnix timeutil.TimeStamp `xorm:"updated"`
}

func init() {
	db.RegisterModel(new(SavedReply))
}

// GetSavedReplies returns the user's saved replies by title
func GetSavedReplies(ctx context.Context, userID int64) ([]*SavedReply, error) {
	replies := make([]*SavedReply, 0, 8)
	return replies, db.GetEngine(ctx).Where("user_id = ?", userID).OrderBy("title, id").Find(&replies)
}

// GetSavedReply returns one of the user's saved replies
func GetSavedReply(ctx context.Context, userID, id int64) (*SavedReply, error) {
	reply := &SavedReply{}
	has, err := db.GetEngine(ctx).Where(builder.Eq{"id": id, "user_id": userID}).Get(reply)
	if err != nil {
		return nil, err
	} else if !has {
		return nil, util.ErrNotExist
	}
	return reply, nil
}

// SaveSavedReply creates the reply, or updates it when it has an ID
func SaveSavedReply(ctx context.Context, reply *SavedReply) error {
	if reply.ID == 0 {
		return db.Insert(ctx, reply)
	}
	_, err := db.GetEngine(ctx).Where(builder.Eq{"id": reply.ID, "user_id": reply.UserID}).Cols("title", "content").Update(reply)
	return err
}

// DeleteSavedReply deletes one of the user's saved replies
func DeleteSavedReply(ctx context.Context, userID, id int64) error {
	_, err := db.GetEngine(ctx).Where(builder.Eq{"id": id, "user_id": userID}).Delete(new(SavedReply))
	return err
}
