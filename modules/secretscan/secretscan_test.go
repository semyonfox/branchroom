// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package secretscan

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLine(t *testing.T) {
	// assembled at runtime so this file doesn't trip scanners itself
	ghToken := "ghp_" + strings.Repeat("a1B2", 9)
	for _, c := range []struct {
		line, kind string
	}{
		{"token=brpat_" + strings.Repeat("a1b2", 10), "Branchroom access token"},
		{`aws_access_key_id = AKIA` + "Q3EGRKMT2OQ5VY7Z", "AWS access key"},
		{`token: "` + ghToken + `"`, "GitHub token"},
		{"-----BEGIN OPENSSH " + "PRIVATE KEY-----", "private key"},
		{"ANTHROPIC_API_KEY=sk-ant-" + "api03-" + strings.Repeat("Zx9_", 6), "Anthropic API key"},
		{"key: AKIA" + "IOSFODNN7EXAMPLE", ""},
		{"ghp_short", ""},
		{"-----BEGIN PUBLIC KEY-----", ""},
		{"sha: 1a8823cd1a9549fde083f992f6b9b87a7ab74fb3", ""},
	} {
		kind, _ := Line(c.line)
		assert.Equal(t, c.kind, kind, c.line)
	}
}

func TestPatch(t *testing.T) {
	key := "AKIA" + "Q3EGRKMT2OQ5VY7Z"
	patch := strings.Join([]string{
		"commit:aaa",
		"--- a/.env",
		"+++ b/.env",
		"-OLD=" + key, // removing a secret is fine
		"+NEW=" + key,
		"commit:bbb",
		"+++ b/README.md",
		"+nothing here",
	}, "\n")
	findings, err := Patch(strings.NewReader(patch), 5)
	require.NoError(t, err)
	assert.Equal(t, []Finding{{Commit: "aaa", Path: ".env", Kind: "AWS access key", Redacted: "AKIAQ3EG********"}}, findings)
}
