// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	branchroom_model "gitea.dev/models/branchroom"
	user_model "gitea.dev/models/user"
	"gitea.dev/modules/json"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
	"gitea.dev/services/context"
)

type branchroomAuditFilter struct {
	Actor    string `json:"actor"`
	Kind     string `json:"kind"`
	From     string `json:"from"`
	To       string `json:"to"`
	fromUnix int64
	toUnix   int64
}

func branchroomAuditFilters(ctx *context.Context) (branchroomAuditFilter, error) {
	f := branchroomAuditFilter{Actor: ctx.FormString("actor"), Kind: ctx.FormString("kind"), From: ctx.FormString("from"), To: ctx.FormString("to")}
	switch f.Kind {
	case "", "read", "write", "grant", "lifecycle":
	default:
		return f, errors.New("invalid kind")
	}
	for _, date := range []struct {
		value  string
		target *int64
	}{{f.From, &f.fromUnix}, {f.To, &f.toUnix}} {
		if date.value == "" {
			continue
		}
		parsed, err := time.Parse(time.DateOnly, date.value)
		if err != nil {
			return f, err
		}
		*date.target = parsed.Unix()
	}
	if f.To != "" {
		f.toUnix += 24 * 60 * 60
	}
	if f.From != "" && f.To != "" && f.fromUnix >= f.toUnix {
		return f, errors.New("invalid date range")
	}
	return f, nil
}

func (f branchroomAuditFilter) matches(event *branchroom_model.AuditEvent, actor string) bool {
	kind := "lifecycle"
	switch {
	case event.Action == branchroom_model.ActionReadSource || event.Action == branchroom_model.ActionReadHistory || event.Action == branchroom_model.ActionDiscover:
		kind = "read"
	case event.Action == branchroom_model.ActionWriteSource || event.Action == branchroom_model.ActionWriteReview || event.Action == branchroom_model.ActionPublish:
		kind = "write"
	case event.GrantID != 0:
		kind = "grant"
	}
	return (f.Actor == "" || strings.EqualFold(f.Actor, actor)) && (f.Kind == "" || f.Kind == kind) && (f.From == "" || int64(event.CreatedUnix) >= f.fromUnix) && (f.To == "" || int64(event.CreatedUnix) < f.toUnix)
}

type branchroomAuditRow struct {
	Event     *branchroom_model.AuditEvent
	Actor     string
	ActorLink string
}

func branchroomAuditScope(ctx *context.Context) *branchroom_model.Scope {
	if !requireBranchroomScopeOwner(ctx) {
		return nil
	}
	scope, err := branchroom_model.GetScopeByID(ctx, ctx.PathParamInt64("scopeID"))
	if errors.Is(err, util.ErrNotExist) {
		ctx.NotFound()
		return nil
	}
	if err != nil {
		ctx.ServerErrorAuto(err)
		return nil
	}
	if scope.RepoID != ctx.Repo.Repository.ID || scope.Kind != branchroom_model.ScopeKindRepository {
		ctx.NotFound()
		return nil
	}
	return scope
}

func BranchroomAudit(ctx *context.Context)    { branchroomAudit(ctx, false) }
func BranchroomEvidence(ctx *context.Context) { branchroomAudit(ctx, true) }

func branchroomAudit(ctx *context.Context, export bool) {
	scope := branchroomAuditScope(ctx)
	if scope == nil {
		return
	}
	ctx.Resp.Header().Set("Cache-Control", "no-store")
	filter, err := branchroomAuditFilters(ctx)
	if err != nil {
		ctx.PlainText(http.StatusBadRequest, string(ctx.Tr("repo.settings.branchroom.audit.invalid_filters")))
		return
	}
	scope, events, err := branchroom_model.LoadScopeAuditSnapshot(ctx, scope.ID)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	verification := branchroom_model.VerifyAuditEvents(scope, events)
	brokenSequence := int64(0)
	if chainErr, ok := errors.AsType[*branchroom_model.AuditChainError](verification); ok {
		brokenSequence = chainErr.Sequence
	}
	ids := make([]int64, 0)
	for _, event := range events {
		if event.ActorType == branchroom_model.ActorTypeUser {
			ids = append(ids, event.ActorID)
		}
	}
	users, err := user_model.GetUsersMapByIDs(ctx, ids)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	rows := make([]branchroomAuditRow, 0)
	for _, event := range slices.Backward(events) {
		row := branchroomAuditRow{Event: event, Actor: string(event.ActorType)}
		if event.ActorType == branchroom_model.ActorTypeUser {
			row.Actor = string(ctx.Tr("repo.settings.branchroom.deleted_user", event.ActorID))
			if user := users[event.ActorID]; user != nil {
				row.Actor = user.Name
				row.ActorLink = user.HomeLink()
			}
		}
		if filter.matches(event, row.Actor) {
			rows = append(rows, row)
		}
	}
	if export {
		branchroomExportEvidence(ctx, scope, events, rows, filter, brokenSequence)
		return
	}
	page := max(1, ctx.FormInt("page"))
	const pageSize = 50
	pages := max(1, (len(rows)+pageSize-1)/pageSize)
	page = min(page, pages)
	query := url.Values{"actor": {filter.Actor}, "kind": {filter.Kind}, "from": {filter.From}, "to": {filter.To}}
	pageURL := func(n int) string { query.Set("page", strconv.Itoa(n)); return "?" + query.Encode() }
	if page > 1 {
		ctx.Data["AuditPrevious"] = pageURL(page - 1)
	}
	if page < pages {
		ctx.Data["AuditNext"] = pageURL(page + 1)
	}
	ctx.Data["Title"] = ctx.Tr("repo.settings.branchroom.audit.title")
	ctx.Data["PageIsSettingsBranchroom"] = true
	ctx.Data["AuditScope"] = scope
	ctx.Data["AuditFilter"] = filter
	ctx.Data["AuditRows"] = rows[(page-1)*pageSize : min(page*pageSize, len(rows))]
	ctx.Data["AuditBrokenSequence"] = brokenSequence
	ctx.HTML(http.StatusOK, "repo/settings/branchroom_audit")
}

type branchroomEvidenceRow struct {
	Type            string                          `json:"type"`
	Sequence        int64                           `json:"sequence"`
	PreviousHash    string                          `json:"previous_hash"`
	EventHash       string                          `json:"event_hash"`
	EventKey        string                          `json:"event_key,omitempty"`
	DecisionKey     string                          `json:"decision_key"`
	ScopeID         int64                           `json:"scope_id"`
	RepoID          int64                           `json:"repo_id"`
	GrantID         int64                           `json:"grant_id"`
	ActorType       branchroom_model.ActorType      `json:"actor_type"`
	ActorID         int64                           `json:"actor_id"`
	Action          branchroom_model.Action         `json:"action"`
	ResourceType    branchroom_model.ResourceType   `json:"resource_type"`
	ResourceID      string                          `json:"resource_id"`
	Surface         branchroom_model.Surface        `json:"surface"`
	Result          branchroom_model.DecisionResult `json:"result"`
	ReasonCode      string                          `json:"reason_code"`
	OrdinaryAllowed bool                            `json:"ordinary_allowed"`
	RequestID       string                          `json:"request_id"`
	CreatedUnix     timeutil.TimeStamp              `json:"created_unix"`
	ExpiresUnix     timeutil.TimeStamp              `json:"expires_unix,omitempty"`
}

func branchroomEvidenceEvent(event *branchroom_model.AuditEvent) branchroomEvidenceRow {
	return branchroomEvidenceRow{
		Type:            "event",
		Sequence:        event.Sequence,
		PreviousHash:    event.PreviousHash,
		EventHash:       event.EventHash,
		EventKey:        event.EventKey,
		DecisionKey:     event.DecisionKey,
		ScopeID:         event.ScopeID,
		RepoID:          event.RepoID,
		GrantID:         event.GrantID,
		ActorType:       event.ActorType,
		ActorID:         event.ActorID,
		Action:          event.Action,
		ResourceType:    event.ResourceType,
		ResourceID:      event.ResourceID,
		Surface:         event.Surface,
		Result:          event.Result,
		ReasonCode:      event.ReasonCode,
		OrdinaryAllowed: event.OrdinaryAllowed,
		RequestID:       event.RequestID,
		CreatedUnix:     event.CreatedUnix,
	}
}

func branchroomEvidenceDecision(event *branchroom_model.AuditEvent, decision *branchroom_model.Decision) branchroomEvidenceRow {
	if event == nil {
		event = &branchroom_model.AuditEvent{}
	}
	return branchroomEvidenceRow{
		Type:            "decision",
		Sequence:        event.Sequence,
		PreviousHash:    event.PreviousHash,
		EventHash:       event.EventHash,
		DecisionKey:     decision.DecisionKey,
		ScopeID:         decision.ScopeID,
		RepoID:          decision.RepoID,
		GrantID:         decision.GrantID,
		ActorType:       decision.ActorType,
		ActorID:         decision.ActorID,
		Action:          decision.Action,
		ResourceType:    decision.ResourceType,
		ResourceID:      decision.ResourceID,
		Surface:         decision.Surface,
		Result:          decision.Result,
		ReasonCode:      decision.ReasonCode,
		OrdinaryAllowed: decision.OrdinaryAllowed,
		RequestID:       decision.RequestID,
		CreatedUnix:     decision.CreatedUnix,
		ExpiresUnix:     decision.ExpiresUnix,
	}
}

type branchroomEvidenceSummary struct {
	Type           string                `json:"type"`
	ScopeID        int64                 `json:"scope_id"`
	Repository     string                `json:"repository"`
	Filters        branchroomAuditFilter `json:"filters"`
	Events         int                   `json:"events"`
	Decisions      int                   `json:"decisions"`
	Verified       bool                  `json:"verified"`
	BrokenSequence int64                 `json:"broken_sequence,omitempty"`
	HeadSequence   int64                 `json:"head_sequence"`
	HeadHash       string                `json:"head_hash"`
	ExportedAt     time.Time             `json:"exported_at"`
	FirstEventUnix timeutil.TimeStamp    `json:"first_event_unix,omitempty"`
	LastEventUnix  timeutil.TimeStamp    `json:"last_event_unix,omitempty"`
}

func branchroomExportEvidence(ctx *context.Context, scope *branchroom_model.Scope, events []*branchroom_model.AuditEvent, rows []branchroomAuditRow, filter branchroomAuditFilter, broken int64) {
	decisions, err := branchroom_model.FindScopeDecisions(ctx, scope.ID)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	current, err := branchroom_model.GetScopeByID(ctx, scope.ID)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	if current.AuditSequence != scope.AuditSequence || current.AuditHeadHash != scope.AuditHeadHash {
		ctx.ServerErrorAuto(branchroom_model.ErrAuditConflict)
		return
	}
	linked := make(map[string]bool, len(events))
	for _, event := range events {
		linked[event.DecisionKey] = true
	}
	actorIDs := make([]int64, 0)
	for _, decision := range decisions {
		if !linked[decision.DecisionKey] && decision.ActorType == branchroom_model.ActorTypeUser {
			actorIDs = append(actorIDs, decision.ActorID)
		}
	}
	users, err := user_model.GetUsersMapByIDs(ctx, actorIDs)
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	byKey := make(map[string]*branchroom_model.Decision, len(decisions))
	for _, decision := range decisions {
		byKey[decision.DecisionKey] = decision
	}
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	count := 0
	for _, row := range rows {
		event := row.Event
		if err := encoder.Encode(branchroomEvidenceEvent(event)); err != nil {
			ctx.ServerErrorAuto(err)
			return
		}
		_ = output.WriteByte('\n')
		if decision := byKey[event.DecisionKey]; decision != nil {
			if err := encoder.Encode(branchroomEvidenceDecision(event, decision)); err != nil {
				ctx.ServerErrorAuto(err)
				return
			}
			_ = output.WriteByte('\n')
			count++
		}
	}
	for _, decision := range decisions {
		if linked[decision.DecisionKey] {
			continue
		}
		actor := string(decision.ActorType)
		if user := users[decision.ActorID]; decision.ActorType == branchroom_model.ActorTypeUser && user != nil {
			actor = user.Name
		}
		event := &branchroom_model.AuditEvent{Action: decision.Action, GrantID: decision.GrantID, CreatedUnix: decision.CreatedUnix}
		if !filter.matches(event, actor) {
			continue
		}
		if err := encoder.Encode(branchroomEvidenceDecision(nil, decision)); err != nil {
			ctx.ServerErrorAuto(err)
			return
		}
		_ = output.WriteByte('\n')
		count++
	}

	summary := branchroomEvidenceSummary{
		Type:           "summary",
		ScopeID:        scope.ID,
		Repository:     ctx.Repo.Repository.FullName(),
		Filters:        filter,
		Events:         len(rows),
		Decisions:      count,
		Verified:       broken == 0,
		BrokenSequence: broken,
		HeadSequence:   scope.AuditSequence,
		HeadHash:       scope.AuditHeadHash,
		ExportedAt:     time.Now().UTC(),
	}
	if len(rows) > 0 {
		summary.FirstEventUnix = rows[0].Event.CreatedUnix
		summary.LastEventUnix = rows[0].Event.CreatedUnix
		for _, row := range rows {
			summary.FirstEventUnix = min(summary.FirstEventUnix, row.Event.CreatedUnix)
			summary.LastEventUnix = max(summary.LastEventUnix, row.Event.CreatedUnix)
		}
	}
	if err := encoder.Encode(summary); err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	_ = output.WriteByte('\n')
	_, err = branchroom_model.RecordDecisionAndAudit(ctx, scope, &branchroom_model.Decision{
		ScopeID:         scope.ID,
		RepoID:          scope.RepoID,
		ActorType:       branchroom_model.ActorTypeUser,
		ActorID:         ctx.Doer.ID,
		Action:          branchroom_model.ActionManagePolicy,
		ResourceType:    branchroom_model.ResourceTypeScope,
		ResourceID:      "scope/" + strconv.FormatInt(scope.ID, 10),
		Surface:         branchroom_model.SurfaceWeb,
		Result:          branchroom_model.DecisionResultAllow,
		ReasonCode:      "evidence_exported",
		OrdinaryAllowed: true,
	})
	if err != nil {
		ctx.ServerErrorAuto(err)
		return
	}
	ctx.Resp.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="branchroom-scope-%d.jsonl"`, scope.ID))
	ctx.Resp.Header().Set("Content-Type", "application/x-ndjson")
	ctx.Resp.WriteHeader(http.StatusOK)
	_, _ = ctx.Resp.Write(output.Bytes())
}
