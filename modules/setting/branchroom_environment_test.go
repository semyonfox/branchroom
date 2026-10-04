// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gitea.dev/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadBranchroomEnvironment(t *testing.T) {
	defer test.MockVariableValue(&Branchroom)()
	defer test.MockVariableValue(&BranchroomEnvironment)()
	defer test.MockVariableValue(&IsProd)()

	keyFile := filepath.Join(t.TempDir(), "development-key")
	require.NoError(t, os.WriteFile(keyFile, []byte("development only"), 0o600))
	Branchroom.Enabled = true

	loadConfig := func(t *testing.T, config string) error {
		t.Helper()
		cfg, err := NewConfigProviderFromData(config)
		require.NoError(t, err)
		return loadBranchroomEnvironmentFrom(cfg)
	}

	t.Run("DisabledByDefault", func(t *testing.T) {
		IsProd = true
		require.NoError(t, loadConfig(t, ""))
		assert.Equal(t, BranchroomEnvironmentConfig{}, BranchroomEnvironment)
	})

	t.Run("DoesNotInheritEnabledFromBranchroom", func(t *testing.T) {
		IsProd = true
		require.NoError(t, loadConfig(t, "[branchroom]\nENABLED = true\n"))
		assert.Equal(t, BranchroomEnvironmentConfig{}, BranchroomEnvironment)
	})

	t.Run("DisabledWithParentDisabled", func(t *testing.T) {
		IsProd = true
		Branchroom.Enabled = false
		require.NoError(t, loadConfig(t, "[branchroom.environment]\nENABLED = true\n"))
		assert.Equal(t, BranchroomEnvironmentConfig{}, BranchroomEnvironment)
		Branchroom.Enabled = true
	})

	t.Run("EnabledDevelopmentFileProvider", func(t *testing.T) {
		IsProd = false
		require.NoError(t, loadConfig(t, "[branchroom.environment]\nENABLED = true\nDEVELOPMENT_KEY_URI = file://"+keyFile+"\nMAX_REVISION_BYTES = 1024\nLEASE_DURATION = 30s\n"))
		assert.Equal(t, BranchroomEnvironmentConfig{
			Enabled:                true,
			Provider:               branchroomDevelopmentFileProvider,
			DevelopmentKeyFilePath: keyFile,
			MaxRevisionBytes:       1024,
			LeaseDuration:          30 * time.Second,
		}, BranchroomEnvironment)
	})

	t.Run("ProductionFailsClosed", func(t *testing.T) {
		IsProd = true
		err := loadConfig(t, "[branchroom.environment]\nENABLED = true\nDEVELOPMENT_KEY_URI = file://"+keyFile+"\n")
		assert.ErrorContains(t, err, "unavailable in production")
	})

	for _, testCase := range []struct {
		name   string
		config string
		match  string
	}{
		{
			name:   "RequiresFileURI",
			config: "[branchroom.environment]\nENABLED = true\nDEVELOPMENT_KEY_URI = " + keyFile + "\n",
			match:  "must be an absolute file URI",
		},
		{
			name:   "RejectsUnknownProvider",
			config: "[branchroom.environment]\nENABLED = true\nPROVIDER = kms\nDEVELOPMENT_KEY_URI = file://" + keyFile + "\n",
			match:  "PROVIDER",
		},
		{
			name:   "BoundsRevisionBytes",
			config: "[branchroom.environment]\nENABLED = true\nDEVELOPMENT_KEY_URI = file://" + keyFile + "\nMAX_REVISION_BYTES = 0\n",
			match:  "MAX_REVISION_BYTES",
		},
		{
			name:   "BoundsLeaseDuration",
			config: "[branchroom.environment]\nENABLED = true\nDEVELOPMENT_KEY_URI = file://" + keyFile + "\nLEASE_DURATION = 2h\n",
			match:  "LEASE_DURATION",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			IsProd = false
			err := loadConfig(t, testCase.config)
			assert.ErrorContains(t, err, testCase.match)
		})
	}
}
