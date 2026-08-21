// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckPublicContent(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		content  string
		want     []PublicContentFinding
	}{
		{
			name:     "safe environment reference",
			filePath: ".branchroom/env/production.toml",
			content:  "environment = \"production\"\nrevision = 42\ndestination = \"private/.env\"\n",
		},
		{
			name:     "reserved path",
			filePath: ".branchroom/policy.toml",
			content:  "enabled = true\n",
			want:     []PublicContentFinding{{Rule: PublicContentRuleReservedPath}},
		},
		{
			name:     "invalid environment reference",
			filePath: ".branchroom/env/production.toml",
			content:  "environment = \"production\"\nrevision = 42\ndestination = \"../.env\"\n",
			want:     []PublicContentFinding{{Rule: PublicContentRuleInvalidEnvironmentReference}},
		},
		{
			name:     "environment reference path mismatch",
			filePath: ".branchroom/env/production.toml",
			content:  "revision = 42\ndestination = \"private/.env\"\nenvironment = \"staging\"\n",
			want:     []PublicContentFinding{{Rule: PublicContentRuleReferencePathMismatch}},
		},
		{
			name:     "embedded private key with CRLF header",
			filePath: "config/example.txt",
			content:  "before\r\nkey = \"-----BEGIN PRIVATE KEY-----\"\r\n",
			want:     []PublicContentFinding{{Rule: PublicContentRulePrivateKey, Line: 2}},
		},
		{
			name:     "stateless GitHub token ending in dash",
			filePath: "config/example.txt",
			content:  "ghs_abcdefghijklmnopqrstuvwxyz0123456789.-_installation-\n",
			want:     []PublicContentFinding{{Rule: PublicContentRuleGitHubToken, Line: 1}},
		},
		{
			name:     "default GitLab token ending in dash",
			filePath: "config/example.txt",
			content:  "glpat-abcdefghijklmnopqrstuv-\n",
			want:     []PublicContentFinding{{Rule: PublicContentRuleDefaultGitLabToken, Line: 1}},
		},
		{
			name:     "known token shapes",
			filePath: "config/example.txt",
			content:  "AKIA1234567890ABCDEF\nghp_abcdefghijklmnopqrstuvwxyz0123456789\nghs_abcdefghijklmnopqrstuvwxyz0123456789.-_installation\nglpat-abcdefghijklmnopqrstuv\n",
			want: []PublicContentFinding{
				{Rule: PublicContentRuleAWSAccessKey, Line: 1},
				{Rule: PublicContentRuleGitHubToken, Line: 2},
				{Rule: PublicContentRuleDefaultGitLabToken, Line: 4},
			},
		},
		{
			name:     "ordinary configuration is not a secret detector hit",
			filePath: "config/example.toml",
			content:  "password = \"example\"\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			findings, err := CheckPublicContent(test.filePath, []byte(test.content))
			require.NoError(t, err)
			assert.Equal(t, test.want, findings)
		})
	}
}

func TestCheckPublicContentRejectsUnsafeInput(t *testing.T) {
	for _, filePath := range []string{"", "../secret", "/secret", `private\\secret`, ".branchroom/env/../production.toml", "private/\x01secret", string([]byte{'p', 0xff}), "private/alias.", "private/alias "} {
		t.Run(filePath, func(t *testing.T) {
			_, err := CheckPublicContent(filePath, nil)
			assert.ErrorIs(t, err, ErrInvalidPublicContentPath)
		})
	}

	_, err := CheckPublicContent("config/large.txt", bytes.Repeat([]byte{'a'}, maxPublicContentBytes+1))
	assert.ErrorIs(t, err, ErrPublicContentTooLarge)
	_, err = CheckPublicContent(strings.Repeat("a", maxPublicContentPathBytes+1), nil)
	assert.ErrorIs(t, err, ErrInvalidPublicContentPath)
}

func TestCheckPublicContentReservesBranchroomPathCaseVariants(t *testing.T) {
	for _, filePath := range []string{".branchroom", ".Branchroom/env/production.toml", ".branchroom/ENV/production.toml"} {
		t.Run(filePath, func(t *testing.T) {
			findings, err := CheckPublicContent(filePath, nil)
			require.NoError(t, err)
			assert.Equal(t, []PublicContentFinding{{Rule: PublicContentRuleReservedPath}}, findings)
		})
	}
}

func TestCheckPublicContentNeverReturnsMatchedData(t *testing.T) {
	token := "ghp_abcdefghijklmnopqrstuvwxyz0123456789"
	findings, err := CheckPublicContent("config/example.txt", []byte("token = "+token))
	require.NoError(t, err)
	assert.Equal(t, []PublicContentFinding{{Rule: PublicContentRuleGitHubToken, Line: 1}}, findings)
	assert.NotContains(t, fmt.Sprint(findings), token)

	_, err = CheckPublicContent("../"+token, nil)
	require.ErrorIs(t, err, ErrInvalidPublicContentPath)
	assert.NotContains(t, err.Error(), token)
}
