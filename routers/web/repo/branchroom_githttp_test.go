// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"bytes"
	"compress/gzip"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gitea.dev/services/context"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadBranchroomGitHTTPBody(t *testing.T) {
	t.Run("accepts a bounded request", func(t *testing.T) {
		body, ok := readBranchroomGitHTTPBody(bytes.NewReader([]byte("packet-line")))
		assert.True(t, ok)
		assert.Equal(t, []byte("packet-line"), body)
	})

	t.Run("rejects an oversized request before receive admission", func(t *testing.T) {
		body, ok := readBranchroomGitHTTPBody(bytes.NewReader(make([]byte, branchroomGitHTTPMaxRequestBody+1)))
		assert.False(t, ok)
		assert.Len(t, body, branchroomGitHTTPMaxRequestBody+1)
	})
}

func TestReadBranchroomGitHTTPRPCBodyStalledTCP(t *testing.T) {
	finished := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok := readBranchroomGitHTTPRPCBody(context.WrapResponseWriter(w), r, time.Now().Add(50*time.Millisecond))
		_ = r.Body.Close()
		finished <- ok
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()
	_, err = io.WriteString(conn, "POST / HTTP/1.1\r\nHost: localhost\r\nContent-Length: 100\r\n\r\nx")
	require.NoError(t, err)
	select {
	case ok := <-finished:
		assert.False(t, ok)
	case <-time.After(2 * time.Second):
		t.Fatal("stalled HTTP/1.1 body exceeded its deadline")
	}
}

func TestReadBranchroomGitHTTPGzip(t *testing.T) {
	for _, size := range []int{1024, branchroomGitHTTPMaxRequestBody, branchroomGitHTTPMaxRequestBody + 1} {
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		_, err := writer.Write(bytes.Repeat([]byte("x"), size))
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, ok := readBranchroomGitHTTPRPCBody(w, r, time.Now().Add(time.Second))
			assert.Equal(t, size <= branchroomGitHTTPMaxRequestBody, ok)
			assert.Len(t, body, size)
		}))
		req, err := http.NewRequest(http.MethodPost, server.URL, &compressed)
		require.NoError(t, err)
		req.Header.Set("Content-Encoding", "gzip")
		response, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		server.Close()
	}
}

func TestReadBranchroomGitHTTPGzipBoundsChunkedInput(t *testing.T) {
	var member bytes.Buffer
	writer := gzip.NewWriter(&member)
	require.NoError(t, writer.Close())
	compressed := bytes.Repeat(member.Bytes(), branchroomGitHTTPMaxRequestBody/member.Len()+2)

	finished := make(chan bool, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, int64(-1), r.ContentLength)
		_, ok := readBranchroomGitHTTPRPCBody(w, r, time.Now().Add(5*time.Second))
		finished <- ok
	}))
	defer server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL, io.MultiReader(bytes.NewReader(compressed)))
	require.NoError(t, err)
	req.Header.Set("Content-Encoding", "gzip")
	response, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	assert.False(t, <-finished)
}
