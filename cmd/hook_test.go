// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package cmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"gitea.dev/modules/branchroom"
	"gitea.dev/modules/private"
	repo_module "gitea.dev/modules/repository"
	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func TestPktLine(t *testing.T) {
	// test read
	ctx := t.Context()
	s := strings.NewReader("0000")
	r := bufio.NewReader(s)
	result, err := readPktLine(ctx, r, pktLineTypeFlush)
	assert.NoError(t, err)
	assert.Equal(t, pktLineTypeFlush, result.Type)

	s = strings.NewReader("0006a\n")
	r = bufio.NewReader(s)
	result, err = readPktLine(ctx, r, pktLineTypeData)
	assert.NoError(t, err)
	assert.Equal(t, pktLineTypeData, result.Type)
	assert.Equal(t, []byte("a\n"), result.Data)

	// test write
	w := bytes.NewBuffer([]byte{})
	err = writeFlushPktLine(ctx, w)
	assert.NoError(t, err)
	assert.Equal(t, []byte("0000"), w.Bytes())

	w.Reset()
	err = writeDataPktLine(ctx, w, []byte("a\nb"))
	assert.NoError(t, err)
	assert.Equal(t, []byte("0007a\nb"), w.Bytes())
}

func TestParseGitHookCommitRefLine(t *testing.T) {
	oldCommitID, newCommitID, refName, ok := parseGitHookCommitRefLine("a b c")
	assert.True(t, ok)
	assert.Equal(t, "a", oldCommitID)
	assert.Equal(t, "b", newCommitID)
	assert.Equal(t, "c", string(refName))

	_, _, _, ok = parseGitHookCommitRefLine("a\tb\tc")
	assert.False(t, ok)

	_, _, _, ok = parseGitHookCommitRefLine("a b")
	assert.False(t, ok)
}

func TestBranchroomPreReceive(t *testing.T) {
	const (
		testSession    = "branchroom-test-session-key-marker"
		testBearer     = "branchroom-test-bearer-secret-marker"
		testUpdates    = "branchroom-test-updates-marker"
		testObjectDir  = "/branchroom-test/store.git/objects/incoming-quarantine"
		testAlternates = "/branchroom-test/store.git/objects"
	)

	t.Run("registered and hidden", func(t *testing.T) {
		var found *cli.Command
		for _, sub := range newHookCommand().Commands {
			if sub.Name == "branchroom-pre-receive" {
				found = sub
				break
			}
		}
		require.NotNil(t, found)
		assert.True(t, found.Hidden)
	})

	defer test.MockVariableValue(&setting.InternalToken, "")()

	t.Run("deny paths", func(t *testing.T) {
		reset := test.MockVariableValue(&branchroomPreReceive, func(_ context.Context, _ private.BranchroomPreReceiveRequest) error {
			require.Fail(t, "endpoint must not be reached on deny paths")
			return nil
		})
		defer reset()
		oversizeQuarantine := strings.Repeat("q", branchroom.MaxReceiveQuarantineValueBytes+1)
		cases := []struct {
			name       string
			session    string
			bearer     []byte
			updates    []byte
			quarantine branchroom.ReceiveQuarantineValues
		}{
			{"missing session", "", []byte(testBearer), []byte(testUpdates), branchroom.ReceiveQuarantineValues{}},
			{"session over max", testSession + strings.Repeat("s", maxBranchroomSessionKeyBytes), []byte(testBearer), []byte(testUpdates), branchroom.ReceiveQuarantineValues{}},
			{"missing bearer", testSession, nil, []byte(testUpdates), branchroom.ReceiveQuarantineValues{}},
			{"bearer over max", testSession, bytes.Repeat([]byte(testBearer), branchroom.MaxReceiveBearerSecretBytes/len(testBearer)+1), []byte(testUpdates), branchroom.ReceiveQuarantineValues{}},
			{"empty stdin", testSession, []byte(testBearer), nil, branchroom.ReceiveQuarantineValues{}},
			{"stdin over max", testSession, []byte(testBearer), make([]byte, branchroom.MaxReceiveUpdatesBytes+1), branchroom.ReceiveQuarantineValues{}},
			{"object directory over max", testSession, []byte(testBearer), []byte(testUpdates), branchroom.ReceiveQuarantineValues{ObjectDirectory: oversizeQuarantine}},
			{"alternates over max", testSession, []byte(testBearer), []byte(testUpdates), branchroom.ReceiveQuarantineValues{AlternateObjectDirectories: oversizeQuarantine}},
			{"quarantine path over max", testSession, []byte(testBearer), []byte(testUpdates), branchroom.ReceiveQuarantineValues{QuarantinePath: oversizeQuarantine}},
			{"valid shape but no internal token", testSession, []byte(testBearer), []byte(testUpdates), branchroom.ReceiveQuarantineValues{}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := hookBranchroomPreReceive(t.Context(), bytes.NewReader(tc.updates), tc.session, bytes.Clone(tc.bearer), tc.quarantine)
				require.Equal(t, errBranchroomReceiveDenied, err)
				assert.NotContains(t, err.Error(), testSession)
				assert.NotContains(t, err.Error(), testBearer)
				assert.NotContains(t, err.Error(), testUpdates)
			})
		}
	})

	t.Run("bearer is cleared", func(t *testing.T) {
		reset := test.MockVariableValue(&branchroomPreReceive, func(_ context.Context, _ private.BranchroomPreReceiveRequest) error {
			require.Fail(t, "endpoint must not be reached on deny paths")
			return nil
		})
		defer reset()
		for _, updates := range [][]byte{nil, []byte(testUpdates)} {
			bearer := bytes.Clone([]byte(testBearer))
			err := hookBranchroomPreReceive(t.Context(), bytes.NewReader(updates), testSession, bearer, branchroom.ReceiveQuarantineValues{})
			require.Equal(t, errBranchroomReceiveDenied, err)
			assert.Equal(t, make([]byte, len(bearer)), bearer)
		}
	})

	t.Run("valid forward", func(t *testing.T) {
		type ctxKey struct{}
		const ctxMarker = "branchroom-test-ctx-value-marker"

		for _, tc := range []struct {
			name      string
			clientErr error
		}{
			{"client error", errors.New("branchroom-test-client-error")},
			{"no error", nil},
		} {
			t.Run(tc.name, func(t *testing.T) {
				var (
					gotOK      bool
					gotCtxVal  string
					gotReq     private.BranchroomPreReceiveRequest
					gotSession string
					gotBearer  []byte
					gotUpdates []byte
					gotQuar    branchroom.ReceiveQuarantineValues
				)
				reset := test.MockVariableValue(&branchroomPreReceive, func(ctx context.Context, req private.BranchroomPreReceiveRequest) error {
					gotCtxVal, gotOK = ctx.Value(ctxKey{}).(string)
					gotReq = req
					gotSession = req.SessionKey
					gotBearer = bytes.Clone(req.BearerSecret)
					gotUpdates = bytes.Clone(req.Updates)
					gotQuar = branchroom.ReceiveQuarantineValues{
						ObjectDirectory:            req.ObjectDirectory,
						AlternateObjectDirectories: req.AlternateObjectDirectories,
						QuarantinePath:             req.QuarantinePath,
					}
					return tc.clientErr
				})
				defer reset()
				resetToken := test.MockVariableValue(&setting.InternalToken, "internal-token-marker")
				defer resetToken()
				tokenBefore := setting.InternalToken

				bearer := []byte(testBearer)
				sentQuar := branchroom.ReceiveQuarantineValues{
					ObjectDirectory:            testObjectDir,
					AlternateObjectDirectories: testAlternates,
					QuarantinePath:             testObjectDir,
				}
				err := hookBranchroomPreReceive(context.WithValue(t.Context(), ctxKey{}, ctxMarker),
					bytes.NewReader([]byte(testUpdates)), testSession, bearer, sentQuar)

				if tc.clientErr != nil {
					require.Equal(t, errBranchroomReceiveDenied, err)
				} else {
					require.NoError(t, err)
				}
				assert.True(t, gotOK)
				assert.Equal(t, ctxMarker, gotCtxVal)
				assert.Equal(t, testSession, gotSession)
				assert.Equal(t, []byte(testBearer), gotBearer)
				assert.Equal(t, []byte(testUpdates), gotUpdates)
				assert.Equal(t, sentQuar, gotQuar)
				if err != nil {
					assert.NotContains(t, err.Error(), testSession)
					assert.NotContains(t, err.Error(), testBearer)
					assert.NotContains(t, err.Error(), testUpdates)
				}
				assert.NotEmpty(t, setting.InternalToken)
				assert.Equal(t, tokenBefore, setting.InternalToken)
				assert.Equal(t, make([]byte, len(bearer)), gotReq.BearerSecret)
				assert.Equal(t, make([]byte, len(testUpdates)), gotReq.Updates)
			})
		}
	})

	t.Run("exact env forwarding", func(t *testing.T) {
		var (
			called bool
			gotReq private.BranchroomPreReceiveRequest
		)
		reset := test.MockVariableValue(&branchroomPreReceive, func(_ context.Context, req private.BranchroomPreReceiveRequest) error {
			req.BearerSecret = bytes.Clone(req.BearerSecret)
			req.Updates = bytes.Clone(req.Updates)
			gotReq = req
			called = true
			return nil
		})
		defer reset()
		resetToken := test.MockVariableValue(&setting.InternalToken, "internal-token-marker")
		defer resetToken()

		t.Setenv(repo_module.EnvBranchroomReceiveSessionKey, testSession)
		t.Setenv(repo_module.EnvBranchroomReceiveBearerSecret, testBearer)
		t.Setenv(private.GitObjectDirectory, testObjectDir)
		t.Setenv(private.GitAlternativeObjectDirectories, testAlternates)
		t.Setenv(private.GitQuarantinePath, testObjectDir)

		r, w, err := os.Pipe()
		require.NoError(t, err)
		_, err = w.WriteString(testUpdates)
		require.NoError(t, err)
		require.NoError(t, w.Close())
		resetStdin := test.MockVariableValue(&os.Stdin, r)
		defer resetStdin()

		err = runHookBranchroomPreReceive(t.Context(), nil)
		require.NoError(t, err)

		assert.True(t, called)
		assert.Equal(t, testSession, gotReq.SessionKey)
		assert.Equal(t, []byte(testBearer), gotReq.BearerSecret)
		assert.Equal(t, testUpdates, string(gotReq.Updates))
		assert.Equal(t, testObjectDir, gotReq.ObjectDirectory)
		assert.Equal(t, testAlternates, gotReq.AlternateObjectDirectories)
		assert.Equal(t, testObjectDir, gotReq.QuarantinePath)
	})

	t.Run("missing quarantine env still forwards", func(t *testing.T) {
		var called bool
		reset := test.MockVariableValue(&branchroomPreReceive, func(_ context.Context, req private.BranchroomPreReceiveRequest) error {
			called = true
			assert.Empty(t, req.ObjectDirectory)
			assert.Empty(t, req.AlternateObjectDirectories)
			assert.Empty(t, req.QuarantinePath)
			return nil
		})
		defer reset()
		resetToken := test.MockVariableValue(&setting.InternalToken, "internal-token-marker")
		defer resetToken()

		t.Setenv(repo_module.EnvBranchroomReceiveSessionKey, testSession)
		t.Setenv(repo_module.EnvBranchroomReceiveBearerSecret, testBearer)
		for _, name := range []string{private.GitObjectDirectory, private.GitAlternativeObjectDirectories, private.GitQuarantinePath} {
			require.NoError(t, os.Unsetenv(name))
		}

		r, w, err := os.Pipe()
		require.NoError(t, err)
		_, err = w.WriteString(testUpdates)
		require.NoError(t, err)
		require.NoError(t, w.Close())
		resetStdin := test.MockVariableValue(&os.Stdin, r)
		defer resetStdin()

		err = runHookBranchroomPreReceive(t.Context(), nil)
		require.NoError(t, err)
		assert.True(t, called)
	})
}
