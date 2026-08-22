// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"

	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/modules/setting"
)

const environmentCipherSuiteAES256GCM = "aes-256-gcm"

var (
	// ErrEnvironmentKeyUnavailable hides development key-file details from callers.
	ErrEnvironmentKeyUnavailable = errors.New("Branchroom environment key is unavailable")
	// ErrEnvironmentDecrypt hides ciphertext and authentication details from callers.
	ErrEnvironmentDecrypt = errors.New("unable to decrypt Branchroom environment value")
)

type environmentEncryptedValue struct {
	PayloadNonce         []byte
	PayloadCiphertext    []byte
	WrappedDEKNonce      []byte
	WrappedDEKCiphertext []byte
}

func loadDevelopmentEnvironmentKEK() ([]byte, error) {
	config := setting.BranchroomEnvironment
	if setting.IsProd || !config.Enabled || config.Provider != "development-file" || config.DevelopmentKeyFilePath == "" {
		return nil, ErrEnvironmentKeyUnavailable
	}
	encodedKey, err := os.ReadFile(config.DevelopmentKeyFilePath)
	if err != nil {
		return nil, ErrEnvironmentKeyUnavailable
	}
	defer clearBytes(encodedKey)
	key := make([]byte, base64.StdEncoding.DecodedLen(len(encodedKey)))
	decodedLength, err := base64.StdEncoding.Decode(key, bytes.TrimSpace(encodedKey))
	if err != nil || decodedLength != 32 {
		clearBytes(key)
		return nil, ErrEnvironmentKeyUnavailable
	}
	return key[:decodedLength], nil
}

func encryptEnvironmentValue(kek, plaintext, aad []byte) (*environmentEncryptedValue, error) {
	wrappingAEAD, err := newEnvironmentAEAD(kek)
	if err != nil {
		return nil, ErrEnvironmentKeyUnavailable
	}
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		clearBytes(dek)
		return nil, errors.New("unable to generate Branchroom environment data key")
	}
	defer clearBytes(dek)
	payloadAEAD, err := newEnvironmentAEAD(dek)
	if err != nil {
		return nil, errors.New("unable to initialize Branchroom environment cipher")
	}
	payloadNonce, err := randomEnvironmentNonce(payloadAEAD)
	if err != nil {
		return nil, err
	}
	wrappedDEKNonce, err := randomEnvironmentNonce(wrappingAEAD)
	if err != nil {
		clearBytes(payloadNonce)
		return nil, err
	}
	return &environmentEncryptedValue{
		PayloadNonce:         payloadNonce,
		PayloadCiphertext:    payloadAEAD.Seal(nil, payloadNonce, plaintext, aad),
		WrappedDEKNonce:      wrappedDEKNonce,
		WrappedDEKCiphertext: wrappingAEAD.Seal(nil, wrappedDEKNonce, dek, aad),
	}, nil
}

func decryptEnvironmentValue(kek []byte, encrypted *environmentEncryptedValue, aad []byte) ([]byte, error) {
	if encrypted == nil {
		return nil, ErrEnvironmentDecrypt
	}
	wrappingAEAD, err := newEnvironmentAEAD(kek)
	if err != nil || len(encrypted.WrappedDEKNonce) != wrappingAEAD.NonceSize() || len(encrypted.PayloadNonce) == 0 {
		return nil, ErrEnvironmentDecrypt
	}
	dek, err := wrappingAEAD.Open(nil, encrypted.WrappedDEKNonce, encrypted.WrappedDEKCiphertext, aad)
	if err != nil || len(dek) != 32 {
		clearBytes(dek)
		return nil, ErrEnvironmentDecrypt
	}
	defer clearBytes(dek)
	payloadAEAD, err := newEnvironmentAEAD(dek)
	if err != nil || len(encrypted.PayloadNonce) != payloadAEAD.NonceSize() {
		return nil, ErrEnvironmentDecrypt
	}
	plaintext, err := payloadAEAD.Open(nil, encrypted.PayloadNonce, encrypted.PayloadCiphertext, aad)
	if err != nil {
		return nil, ErrEnvironmentDecrypt
	}
	return plaintext, nil
}

func newEnvironmentAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("invalid Branchroom environment key")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func randomEnvironmentNonce(aead cipher.AEAD) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		clearBytes(nonce)
		return nil, errors.New("unable to generate Branchroom environment nonce")
	}
	return nonce, nil
}

func environmentRevisionAAD(repoID, scopeID, revision int64, revisionKey, cipherSuite, keyProvider, keyReference string) ([]byte, error) {
	if repoID <= 0 || scopeID <= 0 || revision <= 0 || !branchroom_model.IsCanonicalUUID(revisionKey) || cipherSuite != environmentCipherSuiteAES256GCM || keyProvider == "" || keyReference == "" {
		return nil, errors.New("invalid Branchroom environment encryption metadata")
	}
	fields := []string{
		"branchroom-environment-v1",
		strconv.FormatInt(repoID, 10),
		strconv.FormatInt(scopeID, 10),
		strconv.FormatInt(revision, 10),
		revisionKey,
		cipherSuite,
		keyProvider,
		keyReference,
	}
	var aad bytes.Buffer
	for _, field := range fields {
		_, _ = fmt.Fprintf(&aad, "%d:%s", len(field), field)
	}
	return aad.Bytes(), nil
}

func clearBytes(value []byte) {
	clear(value)
}
