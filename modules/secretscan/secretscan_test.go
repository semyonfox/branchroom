// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package secretscan

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/encoding/unicode"
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
		"+NEW=" + strings.Repeat(" ", 1024*1024) + key,
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

func TestPatchWithUTF16Blobs(t *testing.T) {
	encoder := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder()
	old, err := encoder.Bytes([]byte("UNCHANGED=AKIA" + "Q3EGRKMT2OQ5VY7Z\n"))
	require.NoError(t, err)
	next, err := encoder.Bytes([]byte("UNCHANGED=AKIA" + "Q3EGRKMT2OQ5VY7Z\nADDED=AKIA" + "P7DRA5GK4GL3TX7D\n"))
	require.NoError(t, err)

	patch := `commit:aaa
diff --git a/before b/after
similarity index 67%
rename from before
rename to "after\tfile"
index 111..222 100644
Binary files a/before and "b/after\tfile" differ
diff --git "a/new\tfile" "b/new\tfile"
new file mode 100644
index 000..222
Binary files /dev/null and "b/new\tfile" differ
diff --git a/text b/text
index 333..444 100644
--- a/text
+++ b/text
+plain content
`
	var reads []string
	findings, err := new(Scanner).PatchWithBlobs(strings.NewReader(patch), 5, func(oid string) ([]byte, error) {
		reads = append(reads, oid)
		return map[string][]byte{"111": old, "222": next}[oid], nil
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"222", "111", "222"}, reads)
	assert.Equal(t, []Finding{
		{Commit: "aaa", Path: "after\tfile", Kind: "AWS access key", Redacted: "AKIAP7DR********"},
		{Commit: "aaa", Path: "new\tfile", Kind: "AWS access key", Redacted: "AKIAQ3EG********"},
		{Commit: "aaa", Path: "new\tfile", Kind: "AWS access key", Redacted: "AKIAP7DR********"},
	}, findings)
}
