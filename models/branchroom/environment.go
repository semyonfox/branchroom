// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"gitea.dev/models/db"
	"gitea.dev/modules/timeutil"
	"gitea.dev/modules/util"
)

const (
	environmentAESGCMNonceLength     = 12
	maxEnvironmentKeyReferenceLength = 255
)

// EnvironmentCipherSuite identifies the authenticated encryption format used for a revision.
type EnvironmentCipherSuite string

const EnvironmentCipherSuiteAES256GCM EnvironmentCipherSuite = "aes-256-gcm"

// IsEnvironmentCipherSuiteValid reports whether cipherSuite is supported by the current schema.
func IsEnvironmentCipherSuiteValid(cipherSuite EnvironmentCipherSuite) bool {
	return cipherSuite == EnvironmentCipherSuiteAES256GCM
}

// EnvironmentKeyProvider identifies the provider that wrapped a revision's data-encryption key.
type EnvironmentKeyProvider string

const EnvironmentKeyProviderLocalDev EnvironmentKeyProvider = "local-dev"

// IsEnvironmentKeyProviderValid reports whether keyProvider is supported by the current schema.
func IsEnvironmentKeyProviderValid(keyProvider EnvironmentKeyProvider) bool {
	return keyProvider == EnvironmentKeyProviderLocalDev
}

// EnvironmentRevisionState controls whether an encrypted revision can be materialized.
type EnvironmentRevisionState string

const (
	EnvironmentRevisionStateActive  EnvironmentRevisionState = "active"
	EnvironmentRevisionStateRetired EnvironmentRevisionState = "retired"
)

// IsEnvironmentRevisionStateValid reports whether state is persisted by the current schema.
func IsEnvironmentRevisionStateValid(state EnvironmentRevisionState) bool {
	switch state {
	case EnvironmentRevisionStateActive, EnvironmentRevisionStateRetired:
		return true
	default:
		return false
	}
}

// EnvironmentLeaseState controls the one-use lifecycle of an environment lease.
type EnvironmentLeaseState string

const (
	EnvironmentLeaseStateIssued    EnvironmentLeaseState = "issued"
	EnvironmentLeaseStateClaimed   EnvironmentLeaseState = "claimed"
	EnvironmentLeaseStateCompleted EnvironmentLeaseState = "completed"
	EnvironmentLeaseStateExpired   EnvironmentLeaseState = "expired"
	EnvironmentLeaseStateRevoked   EnvironmentLeaseState = "revoked"
)

// IsEnvironmentLeaseStateValid reports whether state is persisted by the current schema.
func IsEnvironmentLeaseStateValid(state EnvironmentLeaseState) bool {
	switch state {
	case EnvironmentLeaseStateIssued, EnvironmentLeaseStateClaimed, EnvironmentLeaseStateCompleted, EnvironmentLeaseStateExpired, EnvironmentLeaseStateRevoked:
		return true
	default:
		return false
	}
}

// EnvironmentLeaseOutcome records the terminal result of a lease without storing plaintext.
type EnvironmentLeaseOutcome string

const (
	EnvironmentLeaseOutcomePending      EnvironmentLeaseOutcome = "pending"
	EnvironmentLeaseOutcomeMaterialized EnvironmentLeaseOutcome = "materialized"
	EnvironmentLeaseOutcomeDenied       EnvironmentLeaseOutcome = "denied"
	EnvironmentLeaseOutcomeFailed       EnvironmentLeaseOutcome = "failed"
	EnvironmentLeaseOutcomeExpired      EnvironmentLeaseOutcome = "expired"
	EnvironmentLeaseOutcomeRevoked      EnvironmentLeaseOutcome = "revoked"
)

// IsEnvironmentLeaseOutcomeValid reports whether outcome is persisted by the current schema.
func IsEnvironmentLeaseOutcomeValid(outcome EnvironmentLeaseOutcome) bool {
	switch outcome {
	case EnvironmentLeaseOutcomePending, EnvironmentLeaseOutcomeMaterialized, EnvironmentLeaseOutcomeDenied, EnvironmentLeaseOutcomeFailed, EnvironmentLeaseOutcomeExpired, EnvironmentLeaseOutcomeRevoked:
		return true
	default:
		return false
	}
}

// EnvironmentRevision is one immutable encrypted environment payload.
// It deliberately holds ciphertext and wrapped-key material only, never plaintext or a filesystem path.
type EnvironmentRevision struct {
	ID                   int64                    `xorm:"pk autoincr"`
	RevisionKey          string                   `xorm:"CHAR(36) UNIQUE NOT NULL"`
	ScopeID              int64                    `xorm:"UNIQUE(scope_revision) INDEX(scope_state) NOT NULL"`
	RepoID               int64                    `xorm:"INDEX(repo_state) NOT NULL"`
	Revision             int64                    `xorm:"UNIQUE(scope_revision) NOT NULL"`
	CipherSuite          EnvironmentCipherSuite   `xorm:"VARCHAR(32) NOT NULL"`
	PayloadNonce         []byte                   `xorm:"BLOB NOT NULL"`
	PayloadCiphertext    []byte                   `xorm:"BLOB NOT NULL"`
	WrappedDEKNonce      []byte                   `xorm:"BLOB NOT NULL"`
	WrappedDEKCiphertext []byte                   `xorm:"BLOB NOT NULL"`
	KeyProvider          EnvironmentKeyProvider   `xorm:"VARCHAR(32) NOT NULL"`
	KeyReference         string                   `xorm:"VARCHAR(255) NOT NULL"`
	CiphertextDigest     string                   `xorm:"CHAR(64) NOT NULL"`
	PlaintextLength      int64                    `xorm:"NOT NULL"`
	State                EnvironmentRevisionState `xorm:"VARCHAR(16) INDEX(scope_state) INDEX(repo_state) NOT NULL"`
	CreatedByID          int64                    `xorm:"NOT NULL"`
	RetiredUnix          timeutil.TimeStamp       `xorm:"INDEX NOT NULL DEFAULT 0"`
	CreatedUnix          timeutil.TimeStamp       `xorm:"created NOT NULL"`
	UpdatedUnix          timeutil.TimeStamp       `xorm:"updated NOT NULL"`
}

// TableName returns the stable Branchroom environment-revision table name.
func (EnvironmentRevision) TableName() string { return "branchroom_environment_revision" }

// Validate checks the encrypted revision's bounded, non-plaintext metadata.
func (revision *EnvironmentRevision) Validate() error {
	if !IsCanonicalUUID(revision.RevisionKey) || revision.ScopeID <= 0 || revision.RepoID <= 0 || revision.Revision <= 0 || revision.CreatedByID <= 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom environment revision identity")
	}
	if !IsEnvironmentCipherSuiteValid(revision.CipherSuite) || !IsEnvironmentKeyProviderValid(revision.KeyProvider) || !isEnvironmentKeyReference(revision.KeyReference) || revision.PlaintextLength < 0 || !IsEnvironmentRevisionStateValid(revision.State) || revision.RetiredUnix < 0 {
		return util.NewInvalidArgumentErrorf("invalid Branchroom environment revision metadata")
	}
	if len(revision.PayloadNonce) != environmentAESGCMNonceLength || len(revision.WrappedDEKNonce) != environmentAESGCMNonceLength || len(revision.PayloadCiphertext) == 0 || len(revision.WrappedDEKCiphertext) == 0 || !isSHA256Hex(revision.CiphertextDigest) || revision.CiphertextDigest != EnvironmentCiphertextDigest(revision.PayloadCiphertext) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom environment revision ciphertext")
	}
	switch revision.State {
	case EnvironmentRevisionStateActive:
		if revision.RetiredUnix != 0 {
			return util.NewInvalidArgumentErrorf("invalid active Branchroom environment revision")
		}
	case EnvironmentRevisionStateRetired:
		if revision.RetiredUnix <= 0 {
			return util.NewInvalidArgumentErrorf("invalid retired Branchroom environment revision")
		}
	}
	return nil
}

// EnvironmentLease binds one direct user, policy decision, and encrypted revision to a one-use bearer secret.
// BearerSecretHash is a SHA-256 hash only. The raw bearer secret must never be persisted.
type EnvironmentLease struct {
	ID               int64                   `xorm:"pk autoincr"`
	LeaseKey         string                  `xorm:"CHAR(36) UNIQUE NOT NULL"`
	BearerSecretHash string                  `xorm:"CHAR(64) UNIQUE NOT NULL"`
	ScopeID          int64                   `xorm:"INDEX(scope_revision_state) NOT NULL"`
	RevisionID       int64                   `xorm:"INDEX(scope_revision_state) INDEX NOT NULL"`
	ActorType        ActorType               `xorm:"VARCHAR(16) INDEX(actor_state) NOT NULL"`
	ActorID          int64                   `xorm:"INDEX(actor_state) NOT NULL"`
	GrantID          int64                   `xorm:"INDEX NOT NULL"`
	DecisionID       int64                   `xorm:"INDEX NOT NULL"`
	State            EnvironmentLeaseState   `xorm:"VARCHAR(16) INDEX(scope_revision_state) INDEX(actor_state) INDEX(state_expiry) NOT NULL"`
	Outcome          EnvironmentLeaseOutcome `xorm:"VARCHAR(16) NOT NULL"`
	IssuedUnix       timeutil.TimeStamp      `xorm:"NOT NULL"`
	ExpiresUnix      timeutil.TimeStamp      `xorm:"INDEX(state_expiry) NOT NULL"`
	ClaimedUnix      timeutil.TimeStamp      `xorm:"NOT NULL DEFAULT 0"`
	CreatedUnix      timeutil.TimeStamp      `xorm:"created NOT NULL"`
	UpdatedUnix      timeutil.TimeStamp      `xorm:"updated NOT NULL"`
}

// TableName returns the stable Branchroom environment-lease table name.
func (EnvironmentLease) TableName() string { return "branchroom_environment_lease" }

// Validate checks that a lease contains only opaque credentials and safe lifecycle metadata.
func (lease *EnvironmentLease) Validate() error {
	if !IsCanonicalUUID(lease.LeaseKey) || !isSHA256Hex(lease.BearerSecretHash) || lease.ScopeID <= 0 || lease.RevisionID <= 0 || lease.GrantID <= 0 || lease.DecisionID <= 0 || !isEnvironmentLeaseActorValid(lease.ActorType, lease.ActorID) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom environment lease identity")
	}
	if lease.IssuedUnix <= 0 || lease.ExpiresUnix <= lease.IssuedUnix || lease.ClaimedUnix < 0 || !IsEnvironmentLeaseStateValid(lease.State) || !IsEnvironmentLeaseOutcomeValid(lease.Outcome) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom environment lease lifecycle")
	}
	if lease.ClaimedUnix != 0 && (lease.ClaimedUnix < lease.IssuedUnix || lease.ClaimedUnix >= lease.ExpiresUnix) {
		return util.NewInvalidArgumentErrorf("invalid Branchroom environment lease claim")
	}
	switch lease.State {
	case EnvironmentLeaseStateIssued:
		if lease.ClaimedUnix != 0 || lease.Outcome != EnvironmentLeaseOutcomePending {
			return util.NewInvalidArgumentErrorf("invalid issued Branchroom environment lease")
		}
	case EnvironmentLeaseStateClaimed:
		if lease.ClaimedUnix == 0 || lease.Outcome != EnvironmentLeaseOutcomePending {
			return util.NewInvalidArgumentErrorf("invalid claimed Branchroom environment lease")
		}
	case EnvironmentLeaseStateCompleted:
		if lease.ClaimedUnix == 0 || lease.Outcome != EnvironmentLeaseOutcomeMaterialized && lease.Outcome != EnvironmentLeaseOutcomeDenied && lease.Outcome != EnvironmentLeaseOutcomeFailed {
			return util.NewInvalidArgumentErrorf("invalid completed Branchroom environment lease")
		}
	case EnvironmentLeaseStateExpired:
		if lease.ClaimedUnix != 0 || lease.Outcome != EnvironmentLeaseOutcomeExpired {
			return util.NewInvalidArgumentErrorf("invalid expired Branchroom environment lease")
		}
	case EnvironmentLeaseStateRevoked:
		if lease.ClaimedUnix != 0 || lease.Outcome != EnvironmentLeaseOutcomeRevoked {
			return util.NewInvalidArgumentErrorf("invalid revoked Branchroom environment lease")
		}
	}
	return nil
}

// InsertEnvironmentRevision validates and inserts an encrypted revision through the caller's transaction context.
func InsertEnvironmentRevision(ctx context.Context, revision *EnvironmentRevision) error {
	if revision == nil {
		return util.NewInvalidArgumentErrorf("Branchroom environment revision is required")
	}
	if err := revision.Validate(); err != nil {
		return err
	}
	return db.Insert(ctx, revision)
}

// GetEnvironmentRevisionByID returns an encrypted revision by its database ID.
func GetEnvironmentRevisionByID(ctx context.Context, id int64) (*EnvironmentRevision, error) {
	return getEnvironmentRevisionByID(ctx, id, false)
}

// GetEnvironmentRevisionByIDForUpdate returns an encrypted revision with a row lock on supported databases.
func GetEnvironmentRevisionByIDForUpdate(ctx context.Context, id int64) (*EnvironmentRevision, error) {
	return getEnvironmentRevisionByID(ctx, id, true)
}

func getEnvironmentRevisionByID(ctx context.Context, id int64, forUpdate bool) (*EnvironmentRevision, error) {
	if id <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom environment revision ID")
	}
	revision := new(EnvironmentRevision)
	engine := db.GetEngine(ctx).ID(id)
	if forUpdate && branchroomSupportsForUpdate() {
		engine = engine.ForUpdate()
	}
	has, err := engine.Get(revision)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.NewNotExistErrorf("Branchroom environment revision %d does not exist", id)
	}
	return revision, nil
}

// GetEnvironmentRevisionByKey returns an encrypted revision by its opaque UUID key.
func GetEnvironmentRevisionByKey(ctx context.Context, revisionKey string) (*EnvironmentRevision, error) {
	if !IsCanonicalUUID(revisionKey) {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom environment revision key")
	}
	revision := new(EnvironmentRevision)
	has, err := db.GetEngine(ctx).Where("revision_key = ?", revisionKey).Get(revision)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.NewNotExistErrorf("Branchroom environment revision does not exist")
	}
	return revision, nil
}

// GetEnvironmentRevisionByScopeIDAndRevision returns one immutable revision by its scope-local number.
func GetEnvironmentRevisionByScopeIDAndRevision(ctx context.Context, scopeID, revisionNumber int64) (*EnvironmentRevision, error) {
	return getEnvironmentRevisionByScopeIDAndRevision(ctx, scopeID, revisionNumber, false)
}

// GetEnvironmentRevisionByScopeIDAndRevisionForUpdate returns one immutable revision with a row lock on supported databases.
func GetEnvironmentRevisionByScopeIDAndRevisionForUpdate(ctx context.Context, scopeID, revisionNumber int64) (*EnvironmentRevision, error) {
	return getEnvironmentRevisionByScopeIDAndRevision(ctx, scopeID, revisionNumber, true)
}

func getEnvironmentRevisionByScopeIDAndRevision(ctx context.Context, scopeID, revisionNumber int64, forUpdate bool) (*EnvironmentRevision, error) {
	if scopeID <= 0 || revisionNumber <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom environment scope or revision")
	}
	revision := new(EnvironmentRevision)
	engine := db.GetEngine(ctx).Where("scope_id = ? AND revision = ?", scopeID, revisionNumber)
	if forUpdate && branchroomSupportsForUpdate() {
		engine = engine.ForUpdate()
	}
	has, err := engine.Get(revision)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.NewNotExistErrorf("Branchroom environment revision does not exist")
	}
	return revision, nil
}

// FindEnvironmentRevisionsByScopeID returns a scope's revisions in revision order.
func FindEnvironmentRevisionsByScopeID(ctx context.Context, scopeID int64) ([]*EnvironmentRevision, error) {
	return findEnvironmentRevisionsByScopeID(ctx, scopeID, false)
}

// FindEnvironmentRevisionsByScopeIDForUpdate returns a scope's revisions with row locks on supported databases.
func FindEnvironmentRevisionsByScopeIDForUpdate(ctx context.Context, scopeID int64) ([]*EnvironmentRevision, error) {
	return findEnvironmentRevisionsByScopeID(ctx, scopeID, true)
}

func findEnvironmentRevisionsByScopeID(ctx context.Context, scopeID int64, forUpdate bool) ([]*EnvironmentRevision, error) {
	if scopeID <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom scope ID")
	}
	revisions := make([]*EnvironmentRevision, 0)
	engine := db.GetEngine(ctx).Where("scope_id = ?", scopeID).Asc("revision", "id")
	if forUpdate && branchroomSupportsForUpdate() {
		engine = engine.ForUpdate()
	}
	err := engine.Find(&revisions)
	return revisions, err
}

// FindEnvironmentRevisionsByRepoID returns a repository's revisions in deterministic scope and revision order.
func FindEnvironmentRevisionsByRepoID(ctx context.Context, repoID int64) ([]*EnvironmentRevision, error) {
	return findEnvironmentRevisionsByRepoID(ctx, repoID, false)
}

// FindEnvironmentRevisionsByRepoIDForUpdate returns repository revisions with row locks on supported databases.
func FindEnvironmentRevisionsByRepoIDForUpdate(ctx context.Context, repoID int64) ([]*EnvironmentRevision, error) {
	return findEnvironmentRevisionsByRepoID(ctx, repoID, true)
}

func findEnvironmentRevisionsByRepoID(ctx context.Context, repoID int64, forUpdate bool) ([]*EnvironmentRevision, error) {
	if repoID <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom repository ID")
	}
	revisions := make([]*EnvironmentRevision, 0)
	engine := db.GetEngine(ctx).Where("repo_id = ?", repoID).Asc("scope_id", "revision", "id")
	if forUpdate && branchroomSupportsForUpdate() {
		engine = engine.ForUpdate()
	}
	err := engine.Find(&revisions)
	return revisions, err
}

// InsertEnvironmentLease validates and inserts a one-use lease through the caller's transaction context.
func InsertEnvironmentLease(ctx context.Context, lease *EnvironmentLease) error {
	if lease == nil {
		return util.NewInvalidArgumentErrorf("Branchroom environment lease is required")
	}
	if err := lease.Validate(); err != nil {
		return err
	}
	return db.Insert(ctx, lease)
}

// GetEnvironmentLeaseByID returns a lease by its database ID.
func GetEnvironmentLeaseByID(ctx context.Context, id int64) (*EnvironmentLease, error) {
	return getEnvironmentLeaseByID(ctx, id, false)
}

// GetEnvironmentLeaseByIDForUpdate returns a lease with a row lock on supported databases.
func GetEnvironmentLeaseByIDForUpdate(ctx context.Context, id int64) (*EnvironmentLease, error) {
	return getEnvironmentLeaseByID(ctx, id, true)
}

func getEnvironmentLeaseByID(ctx context.Context, id int64, forUpdate bool) (*EnvironmentLease, error) {
	if id <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom environment lease ID")
	}
	lease := new(EnvironmentLease)
	engine := db.GetEngine(ctx).ID(id)
	if forUpdate && branchroomSupportsForUpdate() {
		engine = engine.ForUpdate()
	}
	has, err := engine.Get(lease)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.NewNotExistErrorf("Branchroom environment lease %d does not exist", id)
	}
	return lease, nil
}

// GetEnvironmentLeaseByKey returns a lease by its opaque UUID key.
func GetEnvironmentLeaseByKey(ctx context.Context, leaseKey string) (*EnvironmentLease, error) {
	if !IsCanonicalUUID(leaseKey) {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom environment lease key")
	}
	lease := new(EnvironmentLease)
	has, err := db.GetEngine(ctx).Where("lease_key = ?", leaseKey).Get(lease)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.NewNotExistErrorf("Branchroom environment lease does not exist")
	}
	return lease, nil
}

// GetEnvironmentLeaseByBearerSecretHash returns a lease from a canonical SHA-256 hash.
// Callers must hash the opaque bearer secret before this model boundary.
func GetEnvironmentLeaseByBearerSecretHash(ctx context.Context, bearerSecretHash string) (*EnvironmentLease, error) {
	if !isSHA256Hex(bearerSecretHash) {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom environment lease bearer hash")
	}
	lease := new(EnvironmentLease)
	has, err := db.GetEngine(ctx).Where("bearer_secret_hash = ?", bearerSecretHash).Get(lease)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, util.NewNotExistErrorf("Branchroom environment lease does not exist")
	}
	return lease, nil
}

// FindEnvironmentLeasesByScopeID returns a scope's leases in deterministic creation order.
func FindEnvironmentLeasesByScopeID(ctx context.Context, scopeID int64) ([]*EnvironmentLease, error) {
	return findEnvironmentLeasesByScopeID(ctx, scopeID, false)
}

// FindEnvironmentLeasesByScopeIDForUpdate returns a scope's leases with row locks on supported databases.
func FindEnvironmentLeasesByScopeIDForUpdate(ctx context.Context, scopeID int64) ([]*EnvironmentLease, error) {
	return findEnvironmentLeasesByScopeID(ctx, scopeID, true)
}

func findEnvironmentLeasesByScopeID(ctx context.Context, scopeID int64, forUpdate bool) ([]*EnvironmentLease, error) {
	if scopeID <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom scope ID")
	}
	leases := make([]*EnvironmentLease, 0)
	engine := db.GetEngine(ctx).Where("scope_id = ?", scopeID).Asc("id")
	if forUpdate && branchroomSupportsForUpdate() {
		engine = engine.ForUpdate()
	}
	err := engine.Find(&leases)
	return leases, err
}

// FindEnvironmentLeasesByRepoID returns a repository's leases in deterministic creation order.
func FindEnvironmentLeasesByRepoID(ctx context.Context, repoID int64) ([]*EnvironmentLease, error) {
	return findEnvironmentLeasesByRepoID(ctx, repoID, false)
}

// FindEnvironmentLeasesByRepoIDForUpdate returns repository leases with row locks on supported databases.
func FindEnvironmentLeasesByRepoIDForUpdate(ctx context.Context, repoID int64) ([]*EnvironmentLease, error) {
	return findEnvironmentLeasesByRepoID(ctx, repoID, true)
}

func findEnvironmentLeasesByRepoID(ctx context.Context, repoID int64, forUpdate bool) ([]*EnvironmentLease, error) {
	if repoID <= 0 {
		return nil, util.NewInvalidArgumentErrorf("invalid Branchroom repository ID")
	}
	leases := make([]*EnvironmentLease, 0)
	engine := db.GetEngine(ctx).
		Table(new(EnvironmentLease)).
		Select("branchroom_environment_lease.*").
		Join("INNER", "branchroom_environment_revision", "branchroom_environment_revision.id = branchroom_environment_lease.revision_id").
		Where("branchroom_environment_revision.repo_id = ?", repoID).
		Asc("branchroom_environment_lease.id")
	if forUpdate && branchroomSupportsForUpdate() {
		engine = engine.ForUpdate()
	}
	err := engine.Find(&leases)
	return leases, err
}

// ClaimEnvironmentLease atomically changes an unexpired issued lease to claimed.
// Claim is the irrevocable authorization boundary: later revocation cannot recall
// a plaintext release already in progress.
func ClaimEnvironmentLease(ctx context.Context, leaseID int64, claimedUnix timeutil.TimeStamp) (bool, error) {
	if leaseID <= 0 || claimedUnix <= 0 {
		return false, util.NewInvalidArgumentErrorf("invalid Branchroom environment lease claim")
	}
	affected, err := db.GetEngine(ctx).ID(leaseID).
		Where("state = ? AND claimed_unix = 0 AND issued_unix <= ? AND expires_unix > ?", EnvironmentLeaseStateIssued, claimedUnix, claimedUnix).
		Cols("state", "claimed_unix").
		Update(&EnvironmentLease{State: EnvironmentLeaseStateClaimed, ClaimedUnix: claimedUnix})
	return affected == 1, err
}

// CompleteEnvironmentLease records a terminal result for a claimed one-use lease.
// A lease remains consumed even if materialization fails after it has been claimed.
func CompleteEnvironmentLease(ctx context.Context, leaseID int64, outcome EnvironmentLeaseOutcome) (bool, error) {
	if leaseID <= 0 || outcome != EnvironmentLeaseOutcomeMaterialized && outcome != EnvironmentLeaseOutcomeDenied && outcome != EnvironmentLeaseOutcomeFailed {
		return false, util.NewInvalidArgumentErrorf("invalid Branchroom environment lease completion")
	}
	affected, err := db.GetEngine(ctx).ID(leaseID).Where("state = ? AND claimed_unix > 0", EnvironmentLeaseStateClaimed).
		Cols("state", "outcome").
		Update(&EnvironmentLease{State: EnvironmentLeaseStateCompleted, Outcome: outcome})
	return affected == 1, err
}

// ExpireEnvironmentLease atomically records that an unclaimed issued lease can no longer be used.
func ExpireEnvironmentLease(ctx context.Context, leaseID int64, now timeutil.TimeStamp) (bool, error) {
	if leaseID <= 0 || now <= 0 {
		return false, util.NewInvalidArgumentErrorf("invalid Branchroom environment lease expiry")
	}
	affected, err := db.GetEngine(ctx).ID(leaseID).
		Where("state = ? AND claimed_unix = 0 AND expires_unix <= ?", EnvironmentLeaseStateIssued, now).
		Cols("state", "outcome").
		Update(&EnvironmentLease{State: EnvironmentLeaseStateExpired, Outcome: EnvironmentLeaseOutcomeExpired})
	return affected == 1, err
}

// RetireEnvironmentRevision atomically makes an active revision unavailable for new materialization.
func RetireEnvironmentRevision(ctx context.Context, revisionID int64, retiredUnix timeutil.TimeStamp) (bool, error) {
	if revisionID <= 0 || retiredUnix <= 0 {
		return false, util.NewInvalidArgumentErrorf("invalid Branchroom environment revision retirement")
	}
	affected, err := db.GetEngine(ctx).ID(revisionID).Where("state = ?", EnvironmentRevisionStateActive).
		Cols("state", "retired_unix").
		Update(&EnvironmentRevision{State: EnvironmentRevisionStateRetired, RetiredUnix: retiredUnix})
	return affected == 1, err
}

// RevokeEnvironmentLease atomically revokes an unclaimed issued lease.
// A claimed lease is retained for its caller to complete, preserving the one-use claim audit trail.
func RevokeEnvironmentLease(ctx context.Context, leaseID int64) (bool, error) {
	if leaseID <= 0 {
		return false, util.NewInvalidArgumentErrorf("invalid Branchroom environment lease revocation")
	}
	affected, err := db.GetEngine(ctx).ID(leaseID).Where("state = ? AND claimed_unix = 0", EnvironmentLeaseStateIssued).
		Cols("state", "outcome").
		Update(&EnvironmentLease{State: EnvironmentLeaseStateRevoked, Outcome: EnvironmentLeaseOutcomeRevoked})
	return affected == 1, err
}

func isEnvironmentKeyReference(value string) bool {
	return isSafeLogicalID(value, maxEnvironmentKeyReferenceLength) && !strings.HasPrefix(value, "/") && !strings.Contains(value, "..")
}

func isEnvironmentLeaseActorValid(actorType ActorType, actorID int64) bool {
	return actorType == ActorTypeUser && actorID > 0
}

// EnvironmentCiphertextDigest returns the canonical SHA-256 digest of encrypted payload bytes.
func EnvironmentCiphertextDigest(ciphertext []byte) string {
	digest := sha256.Sum256(ciphertext)
	return hex.EncodeToString(digest[:])
}

func init() {
	db.RegisterModel(new(EnvironmentRevision))
	db.RegisterModel(new(EnvironmentLease))
}
