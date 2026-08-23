// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package branchroom

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testSHA1Zero   = strings.Repeat("0", 40)
	testSHA1Old    = strings.Repeat("1", 40)
	testSHA1New    = strings.Repeat("a", 40)
	testSHA256Zero = strings.Repeat("0", 64)
	testSHA256New  = strings.Repeat("b", 64)
)

func receiveLine(oldOID, newOID, ref string) string {
	return fmt.Sprintf("%s %s %s\n", oldOID, newOID, ref)
}

func TestParseReceiveUpdatesAccepts(t *testing.T) {
	tests := []struct {
		name string
		data string
		want []ReceiveRefUpdate
	}{
		{
			name: "sha1 create with final lf",
			data: receiveLine(testSHA1Zero, testSHA1New, "refs/heads/branchroom/main"),
			want: []ReceiveRefUpdate{{testSHA1Zero, testSHA1New, "refs/heads/branchroom/main"}},
		},
		{
			name: "sha1 update without final lf",
			data: fmt.Sprintf("%s %s %s", testSHA1Old, testSHA1New, "refs/heads/branchroom/main"),
			want: []ReceiveRefUpdate{{testSHA1Old, testSHA1New, "refs/heads/branchroom/main"}},
		},
		{
			name: "sha256 create and update",
			data: receiveLine(testSHA256Zero, testSHA256New, "refs/heads/branchroom/main") +
				receiveLine(strings.Repeat("c", 64), testSHA256New, "refs/heads/branchroom/env/prod"),
			want: []ReceiveRefUpdate{
				{testSHA256Zero, testSHA256New, "refs/heads/branchroom/main"},
				{strings.Repeat("c", 64), testSHA256New, "refs/heads/branchroom/env/prod"},
			},
		},
		{
			name: "mixed algorithms across records with deep nesting",
			data: receiveLine(testSHA1Old, testSHA1New, "refs/heads/branchroom/a/b/c/d") +
				receiveLine(testSHA256Zero, testSHA256New, "refs/tags/v1.0"),
			want: []ReceiveRefUpdate{
				{testSHA1Old, testSHA1New, "refs/heads/branchroom/a/b/c/d"},
				{testSHA256Zero, testSHA256New, "refs/tags/v1.0"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseReceiveUpdates([]byte(tt.data))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseReceiveUpdatesRejects(t *testing.T) {
	valid := receiveLine(testSHA1Old, testSHA1New, "refs/heads/branchroom/x")
	tests := []struct {
		name string
		data string
		want error
	}{
		{name: "empty input", data: "", want: ErrInvalidReceiveUpdates},
		{name: "lone newline", data: "\n", want: ErrInvalidReceiveUpdates},
		{name: "blank middle record", data: valid + "\n" + valid, want: ErrInvalidReceiveUpdates},
		{name: "double final newline", data: valid + "\n", want: ErrInvalidReceiveUpdates},
		{name: "leading newline", data: "\n" + valid, want: ErrInvalidReceiveUpdates},
		{name: "crlf line ending", data: fmt.Sprintf("%s %s refs/heads/branchroom/x\r\n", testSHA1Old, testSHA1New), want: ErrInvalidReceiveUpdates},
		{name: "bare carriage return", data: fmt.Sprintf("%s\r %s refs/heads/branchroom/x\n", testSHA1Old, testSHA1New), want: ErrInvalidReceiveUpdates},
		{name: "tab separator", data: fmt.Sprintf("%s\t%s\trefs/heads/branchroom/x\n", testSHA1Old, testSHA1New), want: ErrInvalidReceiveUpdates},
		{name: "trailing space", data: fmt.Sprintf("%s %s refs/heads/branchroom/x \n", testSHA1Old, testSHA1New), want: ErrInvalidReceiveUpdates},
		{name: "leading space", data: fmt.Sprintf(" %s %s refs/heads/branchroom/x\n", testSHA1Old, testSHA1New), want: ErrInvalidReceiveUpdates},
		{name: "double space between fields", data: fmt.Sprintf("%s  %s refs/heads/branchroom/x\n", testSHA1Old, testSHA1New), want: ErrInvalidReceiveUpdates},
		{name: "two fields only", data: fmt.Sprintf("%s %s\n", testSHA1Old, testSHA1New), want: ErrInvalidReceiveUpdates},
		{name: "four fields via space in ref", data: fmt.Sprintf("%s %s refs/heads/branchroom/x y\n", testSHA1Old, testSHA1New), want: ErrInvalidReceiveUpdates},
		{name: "empty ref field", data: fmt.Sprintf("%s %s \n", testSHA1Old, testSHA1New), want: ErrInvalidReceiveUpdates},
		{name: "missing ref field", data: fmt.Sprintf("%s %s\n", testSHA1Old, testSHA1New), want: ErrInvalidReceiveUpdates},
		{name: "uppercase hex oid", data: receiveLine(strings.Repeat("A", 40), testSHA1New, "refs/heads/branchroom/x"), want: ErrInvalidReceiveUpdates},
		{name: "short oid", data: receiveLine(strings.Repeat("a", 39), testSHA1New, "refs/heads/branchroom/x"), want: ErrInvalidReceiveUpdates},
		{name: "long oid", data: receiveLine(strings.Repeat("a", 41), testSHA1New, "refs/heads/branchroom/x"), want: ErrInvalidReceiveUpdates},
		{name: "mixed oid lengths in record", data: receiveLine(testSHA1Old, testSHA256New, "refs/heads/branchroom/x"), want: ErrInvalidReceiveUpdates},
		{name: "non-hex oid", data: receiveLine(strings.Repeat("g", 40), testSHA1New, "refs/heads/branchroom/x"), want: ErrInvalidReceiveUpdates},
		{name: "both oids zero sha1", data: receiveLine(testSHA1Zero, testSHA1Zero, "refs/heads/branchroom/x"), want: ErrInvalidReceiveUpdates},
		{name: "both oids zero sha256", data: receiveLine(testSHA256Zero, testSHA256Zero, "refs/heads/branchroom/x"), want: ErrInvalidReceiveUpdates},
		{name: "invalid utf-8 in ref", data: fmt.Sprintf("%s %s refs/heads/branchroom/\xff\n", testSHA1Old, testSHA1New), want: ErrInvalidReceiveUpdates},
		{name: "control byte in ref", data: fmt.Sprintf("%s %s refs/heads/branchroom/a\x01b\n", testSHA1Old, testSHA1New), want: ErrInvalidReceiveUpdates},
		{name: "del byte in ref", data: fmt.Sprintf("%s %s refs/heads/branchroom/a\x7fb\n", testSHA1Old, testSHA1New), want: ErrInvalidReceiveUpdates},
		{name: "stream over total size limit", data: strings.TrimRight(strings.Repeat(receiveLine(testSHA1Old, testSHA1New, "refs/heads/branchroom/"+strings.Repeat("x", 800)), 300), "\n"), want: ErrReceiveUpdatesTooLarge},
		{name: "record over line size limit", data: receiveLine(testSHA1Old, testSHA1New, "refs/heads/branchroom/"+strings.Repeat("x", 5000)), want: ErrReceiveUpdatesTooLarge},
		{name: "too many records", data: strings.Repeat(receiveLine(testSHA1Old, testSHA1New, "r"), 1001), want: ErrTooManyReceiveUpdates},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseReceiveUpdates([]byte(tt.data))
			require.ErrorIs(t, err, tt.want)
			assert.Nil(t, got)
		})
	}
}

func TestCheckReceiveRefPolicyV0Accepts(t *testing.T) {
	tests := []struct {
		name   string
		update ReceiveRefUpdate
	}{
		{name: "sha1 create", update: ReceiveRefUpdate{testSHA1Zero, testSHA1New, "refs/heads/branchroom/main"}},
		{name: "sha1 update nested", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, "refs/heads/branchroom/team/project/feature"}},
		{name: "sha256 create", update: ReceiveRefUpdate{testSHA256Zero, testSHA256New, "refs/heads/branchroom/release"}},
		{name: "unicode name", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, "refs/heads/branchroom/café"}},
		{name: "dots inside component", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, "refs/heads/branchroom/v1.0.2"}},
		{name: "component ending with dot mid path", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "v1./ok"}},
		{name: "maximum length name", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, "refs/heads/branchroom/" + strings.Repeat("a", 255)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(t, CheckReceiveRefPolicyV0(tt.update))
		})
	}
}

func TestCheckReceiveRefPolicyV0Denies(t *testing.T) {
	tests := []struct {
		name   string
		update ReceiveRefUpdate
		want   error
	}{
		{name: "delete sha1", update: ReceiveRefUpdate{testSHA1Old, testSHA1Zero, "refs/heads/branchroom/main"}, want: ErrRefDeletionDenied},
		{name: "delete sha256", update: ReceiveRefUpdate{strings.Repeat("c", 64), testSHA256Zero, "refs/heads/branchroom/main"}, want: ErrRefDeletionDenied},
		{name: "delete outside namespace still denied as delete", update: ReceiveRefUpdate{testSHA1Old, testSHA1Zero, "refs/heads/main"}, want: ErrRefDeletionDenied},

		{name: "other branch", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, "refs/heads/main"}, want: ErrRefOutsideNamespace},
		{name: "namespace prefix without trailing slash", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, "refs/heads/branchroom"}, want: ErrRefOutsideNamespace},
		{name: "namespace lookalike", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, "refs/heads/branchroomx/y"}, want: ErrRefOutsideNamespace},
		{name: "tag namespace", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, "refs/tags/v1.0"}, want: ErrRefOutsideNamespace},
		{name: "empty ref", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, ""}, want: ErrRefOutsideNamespace},
		{name: "relative ref", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, "branchroom/x"}, want: ErrRefOutsideNamespace},

		{name: "empty relative name", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix}, want: ErrUnsafeBranchroomRefName},
		{name: "dot segment", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "."}, want: ErrUnsafeBranchroomRefName},
		{name: "dotdot segment", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + ".."}, want: ErrUnsafeBranchroomRefName},
		{name: "dot leading component", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + ".hidden"}, want: ErrUnsafeBranchroomRefName},
		{name: "nested dot leading component", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "ok/.hidden"}, want: ErrUnsafeBranchroomRefName},
		{name: "dotdot inside name", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "a..b"}, want: ErrUnsafeBranchroomRefName},
		{name: "trailing dot", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "main."}, want: ErrUnsafeBranchroomRefName},
		{name: "nested trailing dot", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "a/b."}, want: ErrUnsafeBranchroomRefName},
		{name: "lock suffix", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "main.lock"}, want: ErrUnsafeBranchroomRefName},
		{name: "nested lock suffix", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "a/b.lock"}, want: ErrUnsafeBranchroomRefName},
		{name: "at-brace", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "release@{1}"}, want: ErrUnsafeBranchroomRefName},
		{name: "at-only component", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "@"}, want: ErrUnsafeBranchroomRefName},
		{name: "space", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "a b"}, want: ErrUnsafeBranchroomRefName},
		{name: "tab", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "a\tb"}, want: ErrUnsafeBranchroomRefName},
		{name: "backslash", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + `a\b`}, want: ErrUnsafeBranchroomRefName},
		{name: "tilde", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "a~1"}, want: ErrUnsafeBranchroomRefName},
		{name: "caret", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "a^2"}, want: ErrUnsafeBranchroomRefName},
		{name: "colon", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "a:b"}, want: ErrUnsafeBranchroomRefName},
		{name: "question mark", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "a?b"}, want: ErrUnsafeBranchroomRefName},
		{name: "asterisk", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "a*b"}, want: ErrUnsafeBranchroomRefName},
		{name: "bracket", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "a[1]"}, want: ErrUnsafeBranchroomRefName},
		{name: "double slash", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "//x"}, want: ErrUnsafeBranchroomRefName},
		{name: "trailing slash", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "x/"}, want: ErrUnsafeBranchroomRefName},
		{name: "control byte", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "a\x01b"}, want: ErrUnsafeBranchroomRefName},
		{name: "del byte", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "a\x7fb"}, want: ErrUnsafeBranchroomRefName},
		{name: "invalid utf-8", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + "a\xffb"}, want: ErrUnsafeBranchroomRefName},
		{name: "over-long name", update: ReceiveRefUpdate{testSHA1Old, testSHA1New, branchroomRefPrefix + strings.Repeat("a", 256)}, want: ErrUnsafeBranchroomRefName},

		{name: "malformed new oid denied", update: ReceiveRefUpdate{testSHA1Old, strings.Repeat("G", 40), branchroomRefPrefix + "x"}, want: ErrInvalidReceiveUpdates},
		{name: "malformed old oid denied", update: ReceiveRefUpdate{"short", testSHA1New, branchroomRefPrefix + "x"}, want: ErrInvalidReceiveUpdates},
		{name: "mixed formats denied", update: ReceiveRefUpdate{testSHA1Old, testSHA256New, branchroomRefPrefix + "x"}, want: ErrInvalidReceiveUpdates},
		{name: "both zero denied", update: ReceiveRefUpdate{testSHA1Zero, testSHA1Zero, branchroomRefPrefix + "x"}, want: ErrInvalidReceiveUpdates},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.ErrorIs(t, CheckReceiveRefPolicyV0(tt.update), tt.want)
		})
	}
}

func TestParseThenPolicyEndToEnd(t *testing.T) {
	stream := receiveLine(testSHA1Old, testSHA1New, "refs/heads/branchroom/main") +
		receiveLine(testSHA1Old, testSHA1Zero, "refs/tags/v9") +
		receiveLine(testSHA256Zero, testSHA256New, "refs/heads/env/prod")

	updates, err := ParseReceiveUpdates([]byte(stream))
	require.NoError(t, err)
	assert.Len(t, updates, 3)

	assert.NoError(t, CheckReceiveRefPolicyV0(updates[0]))
	assert.ErrorIs(t, CheckReceiveRefPolicyV0(updates[1]), ErrRefDeletionDenied)
	assert.ErrorIs(t, CheckReceiveRefPolicyV0(updates[2]), ErrRefOutsideNamespace)
}

func TestErrorsNeverLeakInput(t *testing.T) {
	hostileRefs := []string{
		"refs/heads/secret-token-abc123",
		"refs/heads/" + strings.Repeat("y", 300),
		branchroomRefPrefix + "a b",
		branchroomRefPrefix + "a\x01b",
	}
	hostileOIDs := []string{testSHA1Old, testSHA1Zero, strings.Repeat("deadbeef", 8)}

	for _, ref := range hostileRefs {
		for _, oid := range hostileOIDs {
			_, parseErr := ParseReceiveUpdates(fmt.Appendf(nil, "%s %s %s extra\n", oid, testSHA1New, ref))
			policyErr := CheckReceiveRefPolicyV0(ReceiveRefUpdate{oid, testSHA1New, ref})
			for _, err := range []error{parseErr, policyErr} {
				require.Error(t, err)
				msg := err.Error()
				assert.NotContains(t, msg, ref)
				assert.NotContains(t, msg, oid)
				assert.NotContains(t, msg, "secret")
				assert.NotContains(t, msg, "deadbeef")
			}
		}
	}
}
