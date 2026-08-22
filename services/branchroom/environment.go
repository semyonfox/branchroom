// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import "errors"

const (
	environmentDevelopmentKeyReference = "local-dev/primary-v1"
	environmentLeaseBearerSecretLength = 32
)

var (
	// ErrEnvironmentUnavailable deliberately does not reveal local key-provider configuration.
	ErrEnvironmentUnavailable = errors.New("Branchroom encrypted environment service is unavailable")
	// ErrEnvironmentMaterialization deliberately omits the target path and plaintext details.
	ErrEnvironmentMaterialization = errors.New("unable to materialize Branchroom environment")
)
