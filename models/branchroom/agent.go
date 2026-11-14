// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"gitea.dev/models/db"
	api "gitea.dev/modules/structs"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
)

type TokenIdentity struct {
	TokenID int64  `xorm:"pk"`
	Agent   string `xorm:"VARCHAR(255) NOT NULL"`
	TaskURL string `xorm:"TEXT"`
	Sponsor string `xorm:"VARCHAR(255)"`
}

func (TokenIdentity) TableName() string { return "branchroom_token_identity" }

func ValidateAgentIdentity(identity *api.AgentIdentity) error {
	if identity == nil {
		return nil
	}
	if strings.TrimSpace(identity.Agent) == "" || len(identity.Agent) > 255 || len(identity.Sponsor) > 255 || len(identity.TaskURL) > 2048 {
		return util.NewInvalidArgumentErrorf("agent name is required and identity fields must fit their limits")
	}
	for _, value := range []string{identity.Agent, identity.Sponsor, identity.TaskURL} {
		if strings.ContainsFunc(value, unicode.IsControl) {
			return util.NewInvalidArgumentErrorf("agent identity cannot contain control characters")
		}
	}
	if identity.TaskURL != "" {
		u, err := url.Parse(identity.TaskURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
			return util.NewInvalidArgumentErrorf("task URL must be an HTTP or HTTPS URL without credentials")
		}
	}
	return nil
}

func InsertTokenIdentity(ctx context.Context, id int64, identity *api.AgentIdentity) error {
	if identity == nil {
		return nil
	}
	if err := ValidateAgentIdentity(identity); err != nil {
		return err
	}
	return db.Insert(ctx, &TokenIdentity{TokenID: id, Agent: identity.Agent, TaskURL: identity.TaskURL, Sponsor: identity.Sponsor})
}

func GetTokenIdentity(ctx context.Context, id int64) (*api.AgentIdentity, error) {
	var identity TokenIdentity
	has, err := db.GetEngine(ctx).ID(id).Get(&identity)
	if err != nil || !has {
		return nil, err
	}
	return &api.AgentIdentity{Agent: identity.Agent, TaskURL: identity.TaskURL, Sponsor: identity.Sponsor}, nil
}

type AgentAction struct {
	ID       int64  `xorm:"pk autoincr" json:"id"`
	Kind     string `xorm:"VARCHAR(16) UNIQUE(target)" json:"kind"`
	TargetID int64  `xorm:"UNIQUE(target)" json:"target_id"`
	TokenID  int64  `json:"token_id"`
	Agent    string `xorm:"VARCHAR(255)" json:"agent"`
	TaskURL  string `xorm:"TEXT" json:"task_url"`
	Sponsor  string `xorm:"VARCHAR(255)" json:"sponsor"`
}

func (AgentAction) TableName() string { return "branchroom_agent_action" }

func RecordAgentAction(ctx context.Context, credential, kind string, targetID int64) error {
	idText, ok := strings.CutPrefix(credential, "access-token:")
	if !ok {
		return nil
	}
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		return err
	}
	identity, err := GetTokenIdentity(ctx, id)
	if err != nil || identity == nil {
		return err
	}
	return db.Insert(ctx, &AgentAction{Kind: kind, TargetID: targetID, TokenID: id, Agent: identity.Agent, TaskURL: identity.TaskURL, Sponsor: identity.Sponsor})
}

func GetAgentAction(ctx context.Context, kind string, targetID int64) (*api.AgentIdentity, error) {
	var action AgentAction
	has, err := db.GetEngine(ctx).Where("kind = ? AND target_id = ?", kind, targetID).Get(&action)
	if err != nil || !has {
		return nil, err
	}
	return &api.AgentIdentity{Agent: action.Agent, TaskURL: action.TaskURL, Sponsor: action.Sponsor}, nil
}

type PushReceipt struct {
	ID          int64  `xorm:"pk autoincr"`
	RepoID      int64  `xorm:"INDEX(commit) NOT NULL"`
	Ref         string `xorm:"VARCHAR(255)"`
	OldSHA      string `xorm:"VARCHAR(64)"`
	NewSHA      string `xorm:"VARCHAR(64) INDEX(commit)"`
	TokenID     int64
	Agent       string             `xorm:"VARCHAR(255)"`
	TaskURL     string             `xorm:"TEXT"`
	Sponsor     string             `xorm:"VARCHAR(255)"`
	CreatedUnix timeutil.TimeStamp `xorm:"created"`
}

func (PushReceipt) TableName() string { return "branchroom_push_receipt" }

func RecordPushReceipt(ctx context.Context, credential string, repoID int64, ref, oldSHA, newSHA string) error {
	credential, _, _ = strings.Cut(credential, "|")
	text, ok := strings.CutPrefix(credential, "access-token:")
	if !ok {
		return nil
	}
	id, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return err
	}
	identity, err := GetTokenIdentity(ctx, id)
	if err != nil || identity == nil {
		return err
	}
	return db.Insert(ctx, &PushReceipt{RepoID: repoID, Ref: ref, OldSHA: oldSHA, NewSHA: newSHA, TokenID: id, Agent: identity.Agent, TaskURL: identity.TaskURL, Sponsor: identity.Sponsor})
}

func GetPushReceipts(ctx context.Context, repoID int64, sha string) ([]*PushReceipt, error) {
	var receipts []*PushReceipt
	err := db.GetEngine(ctx).Where("repo_id = ? AND new_sha = ?", repoID, sha).OrderBy("id ASC").Find(&receipts)
	return receipts, err
}

func init() {
	db.RegisterModel(new(TokenIdentity))
	db.RegisterModel(new(AgentAction))
	db.RegisterModel(new(PushReceipt))
}
