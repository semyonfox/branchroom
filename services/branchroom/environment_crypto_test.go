// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentEnvelopeEncryption(t *testing.T) {
	kek := make([]byte, 32)
	_, err := rand.Read(kek)
	require.NoError(t, err)
	defer clearBytes(kek)
	plaintext := []byte("environment-value-that-must-not-appear-in-errors")
	defer clearBytes(plaintext)
	aad, err := environmentRevisionAAD(12, 34, 1, uuid.NewString(), environmentCipherSuiteAES256GCM, "local-dev", "development-key")
	require.NoError(t, err)

	first, err := encryptEnvironmentValue(kek, plaintext, aad)
	require.NoError(t, err)
	second, err := encryptEnvironmentValue(kek, plaintext, aad)
	require.NoError(t, err)
	assert.NotEqual(t, first.PayloadNonce, second.PayloadNonce)
	assert.NotEqual(t, first.WrappedDEKNonce, second.WrappedDEKNonce)
	assert.NotEqual(t, first.PayloadCiphertext, second.PayloadCiphertext)

	decrypted, err := decryptEnvironmentValue(kek, first, aad)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
	clearBytes(decrypted)

	t.Run("RejectsPayloadTampering", func(t *testing.T) {
		tampered := cloneEncryptedValue(first)
		tampered.PayloadCiphertext[0] ^= 1
		_, err := decryptEnvironmentValue(kek, tampered, aad)
		assert.ErrorIs(t, err, ErrEnvironmentDecrypt)
	})
	t.Run("RejectsWrappedKeyTampering", func(t *testing.T) {
		tampered := cloneEncryptedValue(first)
		tampered.WrappedDEKCiphertext[0] ^= 1
		_, err := decryptEnvironmentValue(kek, tampered, aad)
		assert.ErrorIs(t, err, ErrEnvironmentDecrypt)
	})
	t.Run("RejectsAADSwap", func(t *testing.T) {
		otherAAD, err := environmentRevisionAAD(12, 34, 2, uuid.NewString(), environmentCipherSuiteAES256GCM, "local-dev", "development-key")
		require.NoError(t, err)
		_, err = decryptEnvironmentValue(kek, first, otherAAD)
		assert.ErrorIs(t, err, ErrEnvironmentDecrypt)
	})
	t.Run("RejectsUnsupportedCipherSuite", func(t *testing.T) {
		_, err := environmentRevisionAAD(12, 34, 1, uuid.NewString(), "aes-128-gcm", "local-dev", "development-key")
		assert.Error(t, err)
	})
}

func TestLoadDevelopmentEnvironmentKEK(t *testing.T) {
	defer test.MockVariableValue(&setting.BranchroomEnvironment)()
	defer test.MockVariableValue(&setting.IsProd)()

	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	defer clearBytes(key)
	keyFile := filepath.Join(t.TempDir(), "development-key")
	require.NoError(t, os.WriteFile(keyFile, []byte(base64.StdEncoding.EncodeToString(key)), 0o600))
	setting.BranchroomEnvironment = setting.BranchroomEnvironmentConfig{
		Enabled:                true,
		Provider:               "development-file",
		DevelopmentKeyFilePath: keyFile,
	}
	setting.IsProd = false

	loaded, err := loadDevelopmentEnvironmentKEK()
	require.NoError(t, err)
	assert.Equal(t, key, loaded)
	clearBytes(loaded)

	setting.IsProd = true
	_, err = loadDevelopmentEnvironmentKEK()
	assert.ErrorIs(t, err, ErrEnvironmentKeyUnavailable)
}

func cloneEncryptedValue(value *environmentEncryptedValue) *environmentEncryptedValue {
	return &environmentEncryptedValue{
		PayloadNonce:         append([]byte(nil), value.PayloadNonce...),
		PayloadCiphertext:    append([]byte(nil), value.PayloadCiphertext...),
		WrappedDEKNonce:      append([]byte(nil), value.WrappedDEKNonce...),
		WrappedDEKCiphertext: append([]byte(nil), value.WrappedDEKCiphertext...),
	}
}
