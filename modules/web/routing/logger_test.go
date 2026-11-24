// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package routing

import (
	"testing"

	"gitea.dev/modules/setting"
	"gitea.dev/modules/test"

	"github.com/stretchr/testify/assert"
)

func TestRequestURIForLog(t *testing.T) {
	defer test.MockVariableValue(&setting.AppSubURL, "/forge")()
	for _, tc := range []struct {
		path string
		want string
	}{
		{"/_branchroom/acme/app/KEY.git/info/refs", "/_branchroom/acme/app/%5Bredacted%5D.git/info/refs"},
		{"/forge/_branchroom/acme/app/KEY.git", "/forge/_branchroom/acme/app/%5Bredacted%5D.git"},
		{"//_branchroom/acme/app/KEY.git", "/_branchroom/acme/app/%5Bredacted%5D.git"},
		{"/_branchroom//acme/app/KEY.git", "/_branchroom/acme/app/%5Bredacted%5D.git"},
		{"/_branchroom/acme/app/KEY.git?service=git-upload-pack", "/_branchroom/acme/app/%5Bredacted%5D.git?service=git-upload-pack"},
		{"/_branchroom/acme.git/app/KEY.git", "/_branchroom/acme.git/app/%5Bredacted%5D.git"},
		{"/acme/app.git?x=1", "/acme/app.git?x=1"},
	} {
		assert.Equal(t, tc.want, RequestURIForLog(tc.path), tc.path)
	}
}
