// Copyright 2025 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package repo

import (
	"net/http"
	"testing"

	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	git_module "gitea.dev/modules/git"
	"gitea.dev/services/contexttest"

	"github.com/stretchr/testify/assert"
)

func TestViewHomeSubmoduleRedirect(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())

	ctx, _ := contexttest.MockContext(t, "/user2/repo1/src/branch/master/test-submodule")
	submodule := git_module.NewCommitSubmoduleFile("/user2/repo1", "test-submodule", "../repo-other", "any-ref-id")
	handleRepoViewSubmodule(ctx, submodule)
	assert.Equal(t, http.StatusSeeOther, ctx.Resp.WrittenStatus())
	assert.Equal(t, "/user2/repo-other/tree/any-ref-id", ctx.Resp.Header().Get("Location"))

	ctx, _ = contexttest.MockContext(t, "/user2/repo1/src/branch/master/test-submodule")
	submodule = git_module.NewCommitSubmoduleFile("/user2/repo1", "test-submodule", "https://other/user2/repo-other.git", "any-ref-id")
	handleRepoViewSubmodule(ctx, submodule)
	// do not auto-redirect for external URLs, to avoid open redirect or phishing
	assert.Equal(t, http.StatusNotFound, ctx.Resp.WrittenStatus())

	ctx, respWriter := contexttest.MockContext(t, "/user2/repo1/src/branch/master/test-submodule?only_content=true")
	submodule = git_module.NewCommitSubmoduleFile("/user2/repo1", "test-submodule", "../repo-other", "any-ref-id")
	handleRepoViewSubmodule(ctx, submodule)
	assert.Equal(t, http.StatusOK, ctx.Resp.WrittenStatus())
	assert.Equal(t, `<a href="/user2/repo-other/tree/any-ref-id">/user2/repo-other/tree/any-ref-id</a>`, respWriter.Body.String())
}

func TestRepoHomeViewPreference(t *testing.T) {
	assert.NoError(t, unittest.PrepareTestDatabase())
	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	for _, step := range []struct {
		url        string
		signed     bool
		overview   bool
		preference string
	}{
		{"/user2/repo1", true, true, ""},
		{"/user2/repo1/src/branch/master?view=code", true, false, "code"},
		{"/user2/repo1", true, false, "code"},
		{"/user2/repo1?view=overview", true, true, "overview"},
		{"/user2/repo1/src/branch/master", true, false, "overview"},
		{"/user2/repo1?view=code", true, false, "code"},
		{"/user2/repo1?view=invalid", true, false, "code"},
		{"/user2/repo1", false, true, "code"},
		{"/user2/repo1?view=overview", false, true, "code"},
		{"/user2/repo1/src/branch/master?view=overview", true, false, "code"},
	} {
		t.Run(step.url, func(t *testing.T) {
			ctx, _ := contexttest.MockContext(t, step.url)
			ctx.Repo.RepoLink = "/user2/repo1"
			if step.signed {
				ctx.Doer = user
			}
			prepareRepoHomeView(ctx)
			assert.False(t, ctx.Written())
			assert.Equal(t, step.overview, ctx.Data["PageIsRepoOverview"] == true)
			saved, err := user_model.GetUserSetting(ctx, user.ID, repoHomeViewSetting)
			assert.NoError(t, err)
			assert.Equal(t, step.preference, saved)
		})
	}
}
