// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package v29

import (
	"strings"
	"testing"

	"gitea.dev/modelmigration/migrationtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddBranchroomEnvironmentSchema(t *testing.T) {
	x, deferable := migrationtest.PrepareTestEnv(t, 0)
	defer deferable()
	if x == nil || t.Failed() {
		return
	}

	require.NoError(t, AddBranchroomEnvironmentSchema(t.Context(), x))
	require.NoError(t, AddBranchroomEnvironmentSchema(t.Context(), x))

	tables := migrationtest.LoadTableSchemasMap(t, x)
	for tableName, columns := range map[string][]string{
		"branchroom_environment_revision": {
			"id", "revision_key", "scope_id", "repo_id", "revision", "cipher_suite", "payload_nonce", "payload_ciphertext", "wrapped_dek_nonce", "wrapped_dek_ciphertext", "key_provider", "key_reference", "ciphertext_digest", "plaintext_length", "state", "created_by_id", "retired_unix",
		},
		"branchroom_environment_lease": {
			"id", "lease_key", "bearer_secret_hash", "scope_id", "revision_id", "actor_type", "actor_id", "grant_id", "decision_id", "state", "outcome", "issued_unix", "expires_unix", "claimed_unix",
		},
	} {
		table := tables[tableName]
		require.NotNil(t, table)
		for _, column := range columns {
			assert.Contains(t, table.ColumnsSeq(), column)
		}
	}

	revisionIndexes, err := x.Dialect().GetIndexes(x.DB(), t.Context(), "branchroom_environment_revision")
	require.NoError(t, err)
	assert.True(t, hasBranchroomIndex(revisionIndexes, []string{"revision_key"}, true))
	assert.True(t, hasBranchroomIndex(revisionIndexes, []string{"scope_id", "revision"}, true))
	assert.True(t, hasBranchroomIndex(revisionIndexes, []string{"scope_id", "state"}, false))
	assert.True(t, hasBranchroomIndex(revisionIndexes, []string{"repo_id", "state"}, false))

	leaseIndexes, err := x.Dialect().GetIndexes(x.DB(), t.Context(), "branchroom_environment_lease")
	require.NoError(t, err)
	assert.True(t, hasBranchroomIndex(leaseIndexes, []string{"lease_key"}, true))
	assert.True(t, hasBranchroomIndex(leaseIndexes, []string{"bearer_secret_hash"}, true))
	assert.True(t, hasBranchroomIndex(leaseIndexes, []string{"revision_id"}, false))
	assert.True(t, hasBranchroomIndex(leaseIndexes, []string{"scope_id", "revision_id", "state"}, false))
	assert.True(t, hasBranchroomIndex(leaseIndexes, []string{"state", "expires_unix"}, false))

	_, err = x.Insert(&branchroomEnvironmentRevisionMigration{
		RevisionKey:          "11111111-1111-1111-1111-111111111111",
		ScopeID:              1,
		RepoID:               1,
		Revision:             1,
		CipherSuite:          "aes-256-gcm",
		PayloadNonce:         []byte("payload-nonce"),
		PayloadCiphertext:    []byte("ciphertext"),
		WrappedDEKNonce:      []byte("wrapped-nonce"),
		WrappedDEKCiphertext: []byte("wrapped-key"),
		KeyProvider:          "local-dev",
		KeyReference:         "local-dev/primary-v1",
		CiphertextDigest:     strings.Repeat("a", 64),
		PlaintextLength:      1,
		State:                "active",
		CreatedByID:          1,
	})
	require.NoError(t, err)
	_, err = x.Insert(&branchroomEnvironmentRevisionMigration{
		RevisionKey:          "22222222-2222-2222-2222-222222222222",
		ScopeID:              1,
		RepoID:               1,
		Revision:             1,
		CipherSuite:          "aes-256-gcm",
		PayloadNonce:         []byte("payload-nonce"),
		PayloadCiphertext:    []byte("ciphertext"),
		WrappedDEKNonce:      []byte("wrapped-nonce"),
		WrappedDEKCiphertext: []byte("wrapped-key"),
		KeyProvider:          "local-dev",
		KeyReference:         "local-dev/primary-v1",
		CiphertextDigest:     strings.Repeat("b", 64),
		PlaintextLength:      1,
		State:                "active",
		CreatedByID:          1,
	})
	assert.Error(t, err)

	_, err = x.Insert(&branchroomEnvironmentLeaseMigration{
		LeaseKey:         "33333333-3333-3333-3333-333333333333",
		BearerSecretHash: strings.Repeat("c", 64),
		ScopeID:          1,
		RevisionID:       1,
		ActorType:        "user",
		ActorID:          2,
		GrantID:          1,
		DecisionID:       1,
		State:            "issued",
		Outcome:          "pending",
		IssuedUnix:       100,
		ExpiresUnix:      200,
	})
	require.NoError(t, err)
	_, err = x.Insert(&branchroomEnvironmentLeaseMigration{
		LeaseKey:         "44444444-4444-4444-4444-444444444444",
		BearerSecretHash: strings.Repeat("c", 64),
		ScopeID:          1,
		RevisionID:       1,
		ActorType:        "user",
		ActorID:          2,
		GrantID:          1,
		DecisionID:       1,
		State:            "issued",
		Outcome:          "pending",
		IssuedUnix:       100,
		ExpiresUnix:      200,
	})
	assert.Error(t, err)
}
