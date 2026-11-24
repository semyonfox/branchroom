// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"fmt"
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
	Model   string `xorm:"VARCHAR(255)"`
	Harness string `xorm:"VARCHAR(255)"`
	// the owner lets this agent approve and request changes as them
	ApprovesAsOwner bool `xorm:"NOT NULL DEFAULT false"`
}

func (TokenIdentity) TableName() string { return "branchroom_token_identity" }

func ValidateAgentIdentity(identity *api.AgentIdentity) error {
	if identity == nil {
		return nil
	}
	if strings.TrimSpace(identity.Agent) == "" || len(identity.Agent) > 255 || len(identity.Model) > 255 || len(identity.Harness) > 255 || len(identity.Sponsor) > 255 || len(identity.TaskURL) > 2048 {
		return util.NewInvalidArgumentErrorf("agent name is required and identity fields must fit their limits")
	}
	for _, value := range []string{identity.Agent, identity.Model, identity.Harness, identity.Sponsor, identity.TaskURL} {
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

func PrepareTokenIdentity(ctx context.Context, ownerID int64, identity *api.AgentIdentity) error {
	if err := ValidateAgentIdentity(identity); err != nil || identity == nil {
		return err
	}
	const (
		individualUserType = 0
		botUserType        = 4
	) // persisted user types, without importing user and creating an auth cycle
	var owner struct {
		Name          string
		IsActive      bool
		ProhibitLogin bool
		Type          int
	}
	has, err := db.GetEngine(ctx).Table("user").Where("id = ?", ownerID).Get(&owner)
	if err != nil {
		return fmt.Errorf("PrepareTokenIdentity: %w", err)
	}
	invalidSponsor := util.NewInvalidArgumentErrorf("responsible person must be an existing active user")
	if !has {
		return invalidSponsor
	}
	if owner.Type == botUserType && identity.Sponsor != "" {
		has, err = db.GetEngine(ctx).Table("user").Where("lower_name = ?", strings.ToLower(identity.Sponsor)).Get(&owner)
		if err != nil {
			return fmt.Errorf("PrepareTokenIdentity: %w", err)
		}
		if !has {
			return invalidSponsor
		}
	}
	if !owner.IsActive || owner.ProhibitLogin || owner.Type != individualUserType {
		return invalidSponsor
	}
	if identity.Sponsor != "" && !strings.EqualFold(identity.Sponsor, owner.Name) {
		return util.NewInvalidArgumentErrorf("responsible person must be the token owner or be named by a bot account")
	}
	identity.Sponsor = owner.Name
	return nil
}

func InsertTokenIdentity(ctx context.Context, id, ownerID int64, identity *api.AgentIdentity) error {
	if identity == nil {
		return nil
	}
	if err := PrepareTokenIdentity(ctx, ownerID, identity); err != nil {
		return err
	}
	return db.Insert(ctx, &TokenIdentity{TokenID: id, Agent: identity.Agent, TaskURL: identity.TaskURL, Sponsor: identity.Sponsor, Model: identity.Model, Harness: identity.Harness, ApprovesAsOwner: identity.ApprovesAsOwner})
}

func GetTokenIdentity(ctx context.Context, id int64) (*api.AgentIdentity, error) {
	var identity TokenIdentity
	has, err := db.GetEngine(ctx).ID(id).Get(&identity)
	if err != nil || !has {
		return nil, err
	}
	return &api.AgentIdentity{Agent: identity.Agent, TaskURL: identity.TaskURL, Sponsor: identity.Sponsor, Model: identity.Model, Harness: identity.Harness, ApprovesAsOwner: identity.ApprovesAsOwner}, nil // legacy sponsors may not name an existing user
}

type AgentAction struct {
	ID       int64  `xorm:"pk autoincr" json:"id"`
	Kind     string `xorm:"VARCHAR(16) UNIQUE(target)" json:"kind"`
	TargetID int64  `xorm:"UNIQUE(target)" json:"target_id"`
	TokenID  int64  `json:"token_id"`
	Agent    string `xorm:"VARCHAR(255)" json:"agent"`
	TaskURL  string `xorm:"TEXT" json:"task_url"`
	Sponsor  string `xorm:"VARCHAR(255)" json:"sponsor"`
	Model    string `xorm:"VARCHAR(255)" json:"model"`
	Harness  string `xorm:"VARCHAR(255)" json:"harness"`
	// a review the agent made as the token owner, so it is the owner's own decision
	AsOwner bool `xorm:"NOT NULL DEFAULT false" json:"as_owner"`
}

func (AgentAction) TableName() string { return "branchroom_agent_action" }

// CredentialIdentity returns the agent identity behind the credential, or nil when it is not an agent token
func CredentialIdentity(ctx context.Context, credential string) (int64, *api.AgentIdentity, error) {
	idText, ok := strings.CutPrefix(credential, "access-token:")
	if !ok {
		return 0, nil, nil
	}
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil {
		return 0, nil, err
	}
	identity, err := GetTokenIdentity(ctx, id)
	return id, identity, err
}

func RecordAgentAction(ctx context.Context, credential, kind string, targetID int64) error {
	return recordAgentAction(ctx, credential, kind, targetID, false)
}

// RecordAgentReview credits a review. asOwner marks an approval or change request the agent made as the token owner.
func RecordAgentReview(ctx context.Context, credential string, reviewID int64, asOwner bool) error {
	return recordAgentAction(ctx, credential, "review", reviewID, asOwner)
}

func recordAgentAction(ctx context.Context, credential, kind string, targetID int64, asOwner bool) error {
	id, identity, err := CredentialIdentity(ctx, credential)
	if err != nil || identity == nil {
		return err
	}
	return db.Insert(ctx, &AgentAction{Kind: kind, TargetID: targetID, TokenID: id, Agent: identity.Agent, TaskURL: identity.TaskURL, Sponsor: identity.Sponsor, Model: identity.Model, Harness: identity.Harness, AsOwner: asOwner})
}

func GetAgentAction(ctx context.Context, kind string, targetID int64) (*api.AgentIdentity, error) {
	var action AgentAction
	has, err := db.GetEngine(ctx).Where("kind = ? AND target_id = ?", kind, targetID).Get(&action)
	if err != nil || !has {
		return nil, err
	}
	return &api.AgentIdentity{Agent: action.Agent, TaskURL: action.TaskURL, Sponsor: action.Sponsor, Model: action.Model, Harness: action.Harness, ApprovesAsOwner: action.AsOwner}, nil
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
	Model       string             `xorm:"VARCHAR(255)"`
	Harness     string             `xorm:"VARCHAR(255)"`
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
	return db.Insert(ctx, &PushReceipt{RepoID: repoID, Ref: ref, OldSHA: oldSHA, NewSHA: newSHA, TokenID: id, Agent: identity.Agent, TaskURL: identity.TaskURL, Sponsor: identity.Sponsor, Model: identity.Model, Harness: identity.Harness})
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
