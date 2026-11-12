// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"

	"gitea.dev/models/db"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
)

// ReceiveSessionState controls the one-use lifecycle of a source-write receive session.
type ReceiveSessionState string

const (
	ReceiveSessionStateIssued    ReceiveSessionState = "issued"
	ReceiveSessionStateClaimed   ReceiveSessionState = "claimed"
	ReceiveSessionStateCompleted ReceiveSessionState = "completed"
	ReceiveSessionStateExpired   ReceiveSessionState = "expired"
	ReceiveSessionStateRevoked   ReceiveSessionState = "revoked"
)

// IsReceiveSessionStateValid reports whether state is persisted by the current schema.
func IsReceiveSessionStateValid(state ReceiveSessionState) bool {
	switch state {
	case ReceiveSessionStateIssued, ReceiveSessionStateClaimed, ReceiveSessionStateCompleted, ReceiveSessionStateExpired, ReceiveSessionStateRevoked:
		return true
	default:
		return false
	}
}

// ReceiveSessionOutcome records why a receive session reached its terminal state.
// Legacy is reserved for rows completed before outcomes existed.
type ReceiveSessionOutcome string

const (
	ReceiveSessionOutcomePending ReceiveSessionOutcome = "pending"
	ReceiveSessionOutcomeDenied  ReceiveSessionOutcome = "denied"
	ReceiveSessionOutcomeFailed  ReceiveSessionOutcome = "failed"
	ReceiveSessionOutcomeExpired ReceiveSessionOutcome = "expired"
	ReceiveSessionOutcomeRevoked ReceiveSessionOutcome = "revoked"
	ReceiveSessionOutcomeLegacy  ReceiveSessionOutcome = "legacy"

	// ReceiveSessionOutcomeAdmitted means a pre-receive admission gate accepted an update;
	// it does not mean Git committed refs.
	ReceiveSessionOutcomeAdmitted ReceiveSessionOutcome = "admitted"
)

// IsReceiveSessionOutcomeValid reports whether outcome is persisted by the current schema.
func IsReceiveSessionOutcomeValid(outcome ReceiveSessionOutcome) bool {
	switch outcome {
	case ReceiveSessionOutcomePending, ReceiveSessionOutcomeDenied, ReceiveSessionOutcomeFailed, ReceiveSessionOutcomeExpired, ReceiveSessionOutcomeRevoked, ReceiveSessionOutcomeLegacy, ReceiveSessionOutcomeAdmitted:
		return true
	default:
		return false
	}
}

// ReceiveSession binds one direct user to one repository workspace for a single source-write push.
// SessionKey is an opaque public locator and BearerSecretHash is a SHA-256 hash only; raw bearer
// secrets, store IDs, route keys, clone locators, refs, object IDs, and paths must never be persisted here.
type ReceiveSession struct {
	ID               int64                 `xorm:"pk autoincr"`
	SessionKey       string                `xorm:"CHAR(36) UNIQUE NOT NULL"`
	BearerSecretHash string                `xorm:"CHAR(64) UNIQUE NOT NULL"`
	ScopeID          int64                 `xorm:"INDEX(scope_state) NOT NULL"`
	WorkspaceID      int64                 `xorm:"NOT NULL"`
	RepoID           int64                 `xorm:"INDEX(repo_state) NOT NULL"`
	ActorType        ActorType             `xorm:"VARCHAR(16) INDEX(actor_state) NOT NULL"`
	ActorID          int64                 `xorm:"INDEX(actor_state) NOT NULL"`
	State            ReceiveSessionState   `xorm:"VARCHAR(16) INDEX(scope_state) INDEX(repo_state) INDEX(actor_state) INDEX(state_expiry) NOT NULL"`
	Outcome          ReceiveSessionOutcome `xorm:"VARCHAR(16) NOT NULL DEFAULT 'pending'"`
	IssuedUnix       timeutil.TimeStamp    `xorm:"NOT NULL"`
	ExpiresUnix      timeutil.TimeStamp    `xorm:"INDEX(state_expiry) NOT NULL"`
	ClaimedUnix      timeutil.TimeStamp    `xorm:"NOT NULL DEFAULT 0"`
	CreatedUnix      timeutil.TimeStamp    `xorm:"created NOT NULL"`
	UpdatedUnix      timeutil.TimeStamp    `xorm:"updated NOT NULL"`
}

// TableName returns the stable Branchroom receive-session table name.
func (ReceiveSession) TableName() string { return "branchroom_receive_session" }

// Validate checks that a session contains only opaque credentials and safe lifecycle metadata.
func (session *ReceiveSession) Validate() error {
	if !IsCanonicalUUID(session.SessionKey) || !isSHA256Hex(session.BearerSecretHash) || session.ScopeID <= 0 || session.WorkspaceID <= 0 || session.RepoID <= 0 || session.ActorType != ActorTypeUser || session.ActorID <= 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom receive session identity")
	}
	if session.IssuedUnix <= 0 || session.ExpiresUnix <= session.IssuedUnix || session.ClaimedUnix < 0 || !IsReceiveSessionStateValid(session.State) || !IsReceiveSessionOutcomeValid(session.Outcome) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom receive session lifecycle")
	}
	if session.ClaimedUnix != 0 && (session.ClaimedUnix < session.IssuedUnix || session.ClaimedUnix >= session.ExpiresUnix) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom receive session claim")
	}
	switch session.State {
	case ReceiveSessionStateIssued, ReceiveSessionStateExpired, ReceiveSessionStateRevoked:
		if session.ClaimedUnix != 0 {
			return util.NewInvalidArgumentErrorf("invalid unclaimed Branchroom receive session")
		}
	case ReceiveSessionStateClaimed, ReceiveSessionStateCompleted:
		if session.ClaimedUnix == 0 {
			return util.NewInvalidArgumentErrorf("invalid claimed Branchroom receive session")
		}
	}
	// issued and claimed stay pending; completed only records denied, failed, admitted, or pre-outcome legacy.
	var outcomeValid bool
	switch session.State {
	case ReceiveSessionStateIssued, ReceiveSessionStateClaimed:
		outcomeValid = session.Outcome == ReceiveSessionOutcomePending
	case ReceiveSessionStateCompleted:
		switch session.Outcome {
		case ReceiveSessionOutcomeDenied, ReceiveSessionOutcomeFailed, ReceiveSessionOutcomeLegacy, ReceiveSessionOutcomeAdmitted:
			outcomeValid = true
		}
	case ReceiveSessionStateExpired:
		outcomeValid = session.Outcome == ReceiveSessionOutcomeExpired
	case ReceiveSessionStateRevoked:
		outcomeValid = session.Outcome == ReceiveSessionOutcomeRevoked
	}
	if !outcomeValid {
		return util.NewInvalidArgumentErrorf("invalid Branchroom receive session outcome")
	}
	return nil
}

// InsertReceiveSession validates and inserts a one-use receive session through the caller's transaction context.
func InsertReceiveSession(ctx context.Context, session *ReceiveSession) error {
	if session == nil {
		return util.NewInvalidArgumentErrorf("Branchroom receive session is required")
	}
	if err := session.Validate(); err != nil {
		return err
	}
	return db.Insert(ctx, session)
}

// GetReceiveSessionByID returns a receive session by its database ID.
func GetReceiveSessionByID(ctx context.Context, id int64) (*ReceiveSession, error) {
	return getReceiveSessionByID(ctx, id, false)
}

// GetReceiveSessionByIDForUpdate returns a receive session with a row lock on supported databases.
func GetReceiveSessionByIDForUpdate(ctx context.Context, id int64) (*ReceiveSession, error) {
	return getReceiveSessionByID(ctx, id, true)
}

func getReceiveSessionByID(ctx context.Context, id int64, forUpdate bool) (*ReceiveSession, error) {
	if id <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom receive session ID")
	}
	session := new(ReceiveSession)
	engine := db.GetEngine(ctx).ID(id)
	if forUpdate && branchroomSupportsForUpdate() {
		engine = engine.ForUpdate()
	}
	has, err := engine.Get(session)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.NewNotExistErrorf("Branchroom receive session %d does not exist", id)
	}
	return session, nil
}

// GetReceiveSessionByKey returns a receive session by its opaque UUID key.
func GetReceiveSessionByKey(ctx context.Context, sessionKey string) (*ReceiveSession, error) {
	if !IsCanonicalUUID(sessionKey) {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom receive session key")
	}
	session := new(ReceiveSession)
	has, err := db.GetEngine(ctx).Where("session_key = ?", sessionKey).Get(session)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.NewNotExistErrorf("Branchroom receive session does not exist")
	}
	return session, nil
}

func findReceiveSessionsByScopeID(ctx context.Context, scopeID int64, forUpdate bool) ([]*ReceiveSession, error) {
	if scopeID <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom scope ID")
	}
	sessions := make([]*ReceiveSession, 0)
	engine := db.GetEngine(ctx).Where("scope_id = ?", scopeID).Asc("id")
	if forUpdate && branchroomSupportsForUpdate() {
		engine = engine.ForUpdate()
	}
	err := engine.Find(&sessions)
	return sessions, err
}

// FindReceiveSessionsByRepoID returns a repository's sessions in deterministic creation order.
func FindReceiveSessionsByRepoID(ctx context.Context, repoID int64) ([]*ReceiveSession, error) {
	return findReceiveSessionsByRepoID(ctx, repoID, false)
}

// FindReceiveSessionsByRepoIDForUpdate returns a repository's sessions with row locks on supported databases.
func FindReceiveSessionsByRepoIDForUpdate(ctx context.Context, repoID int64) ([]*ReceiveSession, error) {
	return findReceiveSessionsByRepoID(ctx, repoID, true)
}

func findReceiveSessionsByRepoID(ctx context.Context, repoID int64, forUpdate bool) ([]*ReceiveSession, error) {
	if repoID <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom repository ID")
	}
	sessions := make([]*ReceiveSession, 0)
	engine := db.GetEngine(ctx).Where("repo_id = ?", repoID).Asc("id")
	if forUpdate && branchroomSupportsForUpdate() {
		engine = engine.ForUpdate()
	}
	err := engine.Find(&sessions)
	return sessions, err
}

// ClaimReceiveSession atomically changes an unexpired issued session to claimed.
// Claim is the irrevocable authorization boundary: later revocation cannot recall a push already in progress.
func ClaimReceiveSession(ctx context.Context, sessionID int64, claimedUnix timeutil.TimeStamp) (bool, error) {
	if sessionID <= 0 || claimedUnix <= 0 {
		return false, util.NewInvalidArgumentErrorf("invalid Branchroom receive session claim")
	}
	affected, err := db.GetEngine(ctx).ID(sessionID).
		Where("state = ? AND outcome = ? AND claimed_unix = 0 AND issued_unix <= ? AND expires_unix > ?", ReceiveSessionStateIssued, ReceiveSessionOutcomePending, claimedUnix, claimedUnix).
		Cols("state", "claimed_unix", "outcome").
		Update(&ReceiveSession{State: ReceiveSessionStateClaimed, ClaimedUnix: claimedUnix, Outcome: ReceiveSessionOutcomePending})
	return affected == 1, err
}

// CompleteReceiveSession atomically closes a claimed session so it can never be used again.
// Only denied, failed, or admitted may be recorded; legacy is reserved for pre-outcome rows
// and must never be generated by a normal completion. Admitted records that the pre-receive
// admission gate accepted an update, not that Git committed refs.
func CompleteReceiveSession(ctx context.Context, sessionID int64, outcome ReceiveSessionOutcome) (bool, error) {
	if sessionID <= 0 || outcome != ReceiveSessionOutcomeDenied && outcome != ReceiveSessionOutcomeFailed && outcome != ReceiveSessionOutcomeAdmitted {
		return false, util.NewInvalidArgumentErrorf("invalid Branchroom receive session completion")
	}
	affected, err := db.GetEngine(ctx).ID(sessionID).
		Where("state = ? AND outcome = ? AND claimed_unix > 0", ReceiveSessionStateClaimed, ReceiveSessionOutcomePending).
		Cols("state", "outcome").
		Update(&ReceiveSession{State: ReceiveSessionStateCompleted, Outcome: outcome})
	return affected == 1, err
}

// FailStaleClaimedReceiveSessions closes claimed sessions whose receive process did
// not finish before staleBefore. The predicate makes this safe to race with normal
// completion: a completed session can never be overwritten.
func FailStaleClaimedReceiveSessions(ctx context.Context, staleBefore timeutil.TimeStamp) (int64, error) {
	if staleBefore <= 0 {
		return 0, util.NewInvalidArgumentErrorf("invalid Branchroom receive session stale cutoff")
	}
	return db.GetEngine(ctx).
		Where("state = ? AND outcome = ? AND claimed_unix > 0 AND claimed_unix <= ?", ReceiveSessionStateClaimed, ReceiveSessionOutcomePending, staleBefore).
		Cols("state", "outcome").
		Update(&ReceiveSession{State: ReceiveSessionStateCompleted, Outcome: ReceiveSessionOutcomeFailed})
}

func init() {
	db.RegisterModel(new(ReceiveSession))
}
