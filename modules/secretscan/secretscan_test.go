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

func TestCustomPatterns(t *testing.T) {
	scanner, err := NewScanner(map[string]string{"internal": `company_[A-Za-z0-9]{32}`, "short": `tiny_[0-9]{2}`})
	require.NoError(t, err)
	token := "company_" + strings.Repeat("a1B2", 8)
	findings, err := scanner.Patch(strings.NewReader("commit:aaa\n+++ b/.env\n+TOKEN="+token+"\n+tiny_12"), 5)
	require.NoError(t, err)
	assert.Equal(t, []Finding{
		{Commit: "aaa", Path: ".env", Kind: "internal", Redacted: "company_********"},
		{Commit: "aaa", Path: ".env", Kind: "short", Redacted: "*******"},
	}, findings)
	kind, _ := scanner.Line("ghp_" + strings.Repeat("a1B2", 9))
	assert.Equal(t, "GitHub token", kind)
	for _, expr := range []string{"[", "", "a*"} {
		_, err := NewScanner(map[string]string{"invalid": expr})
		require.Error(t, err)
	}
}
