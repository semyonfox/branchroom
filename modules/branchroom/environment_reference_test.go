// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEnvironmentReference(t *testing.T) {
	reference, err := ParseEnvironmentReference([]byte("environment = \"production\"\nrevision = 42\ndestination = \"private/.env\"\n"))
	require.NoError(t, err)
	assert.Equal(t, &EnvironmentReference{
		Environment: "production",
		Revision:    42,
		Destination: "private/.env",
	}, reference)

	for _, value := range []string{
		"environment = \"production\"\nrevision = 42\n",
		"environment = \"production\"\nrevision = 42\ndestination = \".env\"\nextra = \"value\"\n",
		"environment = \"production\"\nrevision = 42\ndestination = \".env\"\ndestination = \"other.env\"\n",
		"environment = \"production\"\nrevision = 042\ndestination = \".env\"\n",
		"environment = \"Production\"\nrevision = 42\ndestination = \".env\"\n",
		"environment = \"production\"\nrevision = 42\ndestination = \"../.env\"\n",
		"environment = \"production\"\nrevision = 42\ndestination = \".git/config\"\n",
		"# no comments\nenvironment = \"production\"\nrevision = 42\ndestination = \".env\"\n",
		"environment = \"production\\n\"\nrevision = 42\ndestination = \".env\"\n",
	} {
		t.Run(strings.ReplaceAll(value, "\n", "_"), func(t *testing.T) {
			_, err := ParseEnvironmentReference([]byte(value))
			assert.ErrorIs(t, err, ErrInvalidEnvironmentReference)
		})
	}
}

func TestValidateEnvironmentDestination(t *testing.T) {
	for _, destination := range []string{".env", "private/.env", "config/production.toml"} {
		t.Run(destination, func(t *testing.T) {
			actual, err := ValidateEnvironmentDestination(destination)
			require.NoError(t, err)
			assert.Equal(t, destination, actual)
		})
	}
	for _, destination := range []string{"", "../.env", "/tmp/.env", "C:/tmp/.env", `private\\.env`, ".git/config", "private//.env"} {
		t.Run(destination, func(t *testing.T) {
			_, err := ValidateEnvironmentDestination(destination)
			assert.ErrorIs(t, err, ErrInvalidEnvironmentDestination)
		})
	}
}
