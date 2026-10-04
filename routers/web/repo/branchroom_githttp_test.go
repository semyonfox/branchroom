// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

func TestReadBranchroomGitHTTPRPCBodyClosesStalledRequest(t *testing.T) {
	body := &branchroomBlockingRequestBody{closed: make(chan struct{})}
	_, ok := readBranchroomGitHTTPRPCBody(body, time.Now().Add(10*time.Millisecond))
	assert.False(t, ok)
	select {
	case <-body.closed:
	default:
		t.Fatal("stalled request body was not closed")
	}
}

type branchroomBlockingRequestBody struct {
	closed chan struct{}
	once   sync.Once
}

func (body *branchroomBlockingRequestBody) Read([]byte) (int, error) {
	<-body.closed
	return 0, io.ErrClosedPipe
}

func (body *branchroomBlockingRequestBody) Close() error {
	body.once.Do(func() {
		close(body.closed)
	})
	return nil
}
