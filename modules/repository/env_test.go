// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repository

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBranchroomReceiveEnvKeys(t *testing.T) {
	assert.Equal(t, "GITEA_BRANCHROOM_RECEIVE_SESSION_KEY", EnvBranchroomReceiveSessionKey)
	assert.Equal(t, "GITEA_BRANCHROOM_RECEIVE_BEARER_SECRET", EnvBranchroomReceiveBearerSecret)
}
