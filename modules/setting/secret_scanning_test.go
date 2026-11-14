// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"testing"

	"gitea.dev/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretScanningPatterns(t *testing.T) {
	defer test.MockVariableValue(&SecretScanning)()
	defer test.MockVariableValue(&SecretScanningScanner)()
	cfg, err := NewConfigProviderFromData(`[repository.secret_scanning.patterns]
internal = company_[A-Za-z0-9]{16}
`)
	require.NoError(t, err)
	require.NoError(t, loadSecretScanningFrom(cfg))
	kind, _ := SecretScanningScanner.Line("company_123456789abcdefg")
	assert.Equal(t, "internal", kind)
	previous := SecretScanningScanner
	cfg, err = NewConfigProviderFromData(`[repository.secret_scanning.patterns]
invalid = [
`)
	require.NoError(t, err)
	require.Error(t, loadSecretScanningFrom(cfg))
	assert.Same(t, previous, SecretScanningScanner)
	cfg, err = NewConfigProviderFromData("")
	require.NoError(t, err)
	require.NoError(t, loadSecretScanningFrom(cfg))
	kind, _ = SecretScanningScanner.Line("company_123456789abcdefg")
	assert.Empty(t, kind)
}
