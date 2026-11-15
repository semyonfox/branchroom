// Copyright 2020 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package integration

import (
	"fmt"
	"net/http"
	"testing"

	activities_model "gitea.dev/models/activities"
	auth_model "gitea.dev/models/auth"
	branchroom_model "gitea.dev/models/branchroom"
	"gitea.dev/models/db"
	issues_model "gitea.dev/models/issues"
	repo_model "gitea.dev/models/repo"
	"gitea.dev/models/unittest"
	user_model "gitea.dev/models/user"
	api "gitea.dev/modules/structs"
	"gitea.dev/tests"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPINotification(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	t.Run("public only", func(t *testing.T) {
		user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
		thread5 := unittest.AssertExistsAndLoadBean(t, &activities_model.Notification{ID: 5})

		token := getUserToken(t, user2.Name, auth_model.AccessTokenScopeReadNotification, auth_model.AccessTokenScopePublicOnly)
		req := NewRequest(t, "GET", "/api/v1/notifications").
			AddTokenAuth(token)
		MakeRequest(t, req, http.StatusForbidden)

		req = NewRequest(t, "GET", "/api/v1/notifications/new").
			AddTokenAuth(token)
		MakeRequest(t, req, http.StatusForbidden)

		req = NewRequest(t, "GET", fmt.Sprintf("/api/v1/notifications/threads/%d", thread5.ID)).
			AddTokenAuth(token)
		MakeRequest(t, req, http.StatusForbidden)
	})

	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	repo1 := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 1})
	thread5 := unittest.AssertExistsAndLoadBean(t, &activities_model.Notification{ID: 5})
	assert.NoError(t, thread5.LoadAttributes(t.Context()))
	session := loginUser(t, user2.Name)
	token := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeWriteNotification, auth_model.AccessTokenScopeWriteRepository)

	MakeRequest(t, NewRequest(t, "GET", "/api/v1/notifications"), http.StatusUnauthorized)

	// -- GET /notifications --
	// test filter
	since := "2000-01-01T00%3A50%3A01%2B00%3A00" // 946687801
	req := NewRequest(t, "GET", "/api/v1/notifications?since="+since).
		AddTokenAuth(token)
	resp := MakeRequest(t, req, http.StatusOK)
	apiNL := DecodeJSON(t, resp, []api.NotificationThread{})

	assert.Len(t, apiNL, 1)
	assert.EqualValues(t, 5, apiNL[0].ID)

	// test filter
	before := "2000-01-01T01%3A06%3A59%2B00%3A00" // 946688819

	req = NewRequest(t, "GET", fmt.Sprintf("/api/v1/notifications?all=%s&before=%s", "true", before)).
		AddTokenAuth(token)
	resp = MakeRequest(t, req, http.StatusOK)
	apiNL = DecodeJSON(t, resp, []api.NotificationThread{})

	assert.Len(t, apiNL, 3)
	assert.EqualValues(t, 4, apiNL[0].ID)
	assert.True(t, apiNL[0].Unread)
	assert.False(t, apiNL[0].Pinned)
	assert.EqualValues(t, 3, apiNL[1].ID)
	assert.False(t, apiNL[1].Unread)
	assert.True(t, apiNL[1].Pinned)
	assert.EqualValues(t, 2, apiNL[2].ID)
	assert.False(t, apiNL[2].Unread)
	assert.False(t, apiNL[2].Pinned)

	// -- GET /repos/{owner}/{repo}/notifications --
	req = NewRequest(t, "GET", fmt.Sprintf("/api/v1/repos/%s/%s/notifications?status-types=unread", user2.Name, repo1.Name)).
		AddTokenAuth(token)
	resp = MakeRequest(t, req, http.StatusOK)
	apiNL = DecodeJSON(t, resp, []api.NotificationThread{})

	assert.Len(t, apiNL, 1)
	assert.EqualValues(t, 4, apiNL[0].ID)

	// -- GET /repos/{owner}/{repo}/notifications -- multiple status-types
	req = NewRequest(t, "GET", fmt.Sprintf("/api/v1/repos/%s/%s/notifications?status-types=unread&status-types=pinned", user2.Name, repo1.Name)).
		AddTokenAuth(token)
	resp = MakeRequest(t, req, http.StatusOK)
	apiNL = DecodeJSON(t, resp, []api.NotificationThread{})

	assert.Len(t, apiNL, 2)
	assert.EqualValues(t, 4, apiNL[0].ID)
	assert.True(t, apiNL[0].Unread)
	assert.False(t, apiNL[0].Pinned)
	assert.EqualValues(t, 3, apiNL[1].ID)
	assert.False(t, apiNL[1].Unread)
	assert.True(t, apiNL[1].Pinned)

	MakeRequest(t, NewRequest(t, "GET", fmt.Sprintf("/api/v1/notifications/threads/%d", 1)), http.StatusUnauthorized)

	// -- GET /notifications/threads/{id} --
	// get forbidden
	req = NewRequest(t, "GET", fmt.Sprintf("/api/v1/notifications/threads/%d", 1)).
		AddTokenAuth(token)
	MakeRequest(t, req, http.StatusForbidden)

	// get own
	req = NewRequest(t, "GET", fmt.Sprintf("/api/v1/notifications/threads/%d", thread5.ID)).
		AddTokenAuth(token)
	resp = MakeRequest(t, req, http.StatusOK)
	apiN := DecodeJSON(t, resp, &api.NotificationThread{})

	assert.EqualValues(t, 5, apiN.ID)
	assert.False(t, apiN.Pinned)
	assert.True(t, apiN.Unread)
	assert.Equal(t, "issue4", apiN.Subject.Title)
	assert.EqualValues(t, "Issue", apiN.Subject.Type)
	assert.Equal(t, thread5.Issue.APIURL(t.Context()), apiN.Subject.URL)
	assert.Equal(t, thread5.Repository.HTMLURL(), apiN.Repository.HTMLURL)

	MakeRequest(t, NewRequest(t, "GET", "/api/v1/notifications/new"), http.StatusUnauthorized)

	// -- check notifications --
	req = NewRequest(t, "GET", "/api/v1/notifications/new").
		AddTokenAuth(token)
	resp = MakeRequest(t, req, http.StatusOK)
	newStruct := DecodeJSON(t, resp, &struct {
		New int64 `json:"new"`
	}{})
	assert.Positive(t, newStruct.New)

	// -- mark notifications as read --
	req = NewRequest(t, "GET", "/api/v1/notifications?status-types=unread").
		AddTokenAuth(token)
	resp = MakeRequest(t, req, http.StatusOK)
	apiNL = DecodeJSON(t, resp, []api.NotificationThread{})
	assert.Len(t, apiNL, 2)

	lastReadAt := "2000-01-01T00%3A50%3A01%2B00%3A00" // 946687801 <- only Notification 4 is in this filter ...
	req = NewRequest(t, "PUT", fmt.Sprintf("/api/v1/repos/%s/%s/notifications?last_read_at=%s", user2.Name, repo1.Name, lastReadAt)).
		AddTokenAuth(token)
	MakeRequest(t, req, http.StatusResetContent)

	req = NewRequest(t, "GET", "/api/v1/notifications?status-types=unread").
		AddTokenAuth(token)
	resp = MakeRequest(t, req, http.StatusOK)
	apiNL = DecodeJSON(t, resp, []api.NotificationThread{})
	assert.Len(t, apiNL, 1)

	// -- PATCH /notifications/threads/{id} --
	req = NewRequest(t, "PATCH", fmt.Sprintf("/api/v1/notifications/threads/%d", thread5.ID)).
		AddTokenAuth(token)
	MakeRequest(t, req, http.StatusResetContent)

	assert.Equal(t, activities_model.NotificationStatusUnread, thread5.Status)
	thread5 = unittest.AssertExistsAndLoadBean(t, &activities_model.Notification{ID: 5})
	assert.Equal(t, activities_model.NotificationStatusRead, thread5.Status)

	// -- check notifications --
	req = NewRequest(t, "GET", "/api/v1/notifications/new").
		AddTokenAuth(token)
	resp = MakeRequest(t, req, http.StatusOK)
	newStruct = DecodeJSON(t, resp, &struct {
		New int64 `json:"new"`
	}{})
	assert.Zero(t, newStruct.New)
}

func TestAPINotificationPUT(t *testing.T) {
	defer tests.PrepareTestEnv(t)()

	user2 := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	thread5 := unittest.AssertExistsAndLoadBean(t, &activities_model.Notification{ID: 5})
	assert.NoError(t, thread5.LoadAttributes(t.Context()))
	session := loginUser(t, user2.Name)
	token := getTokenForLoggedInUser(t, session, auth_model.AccessTokenScopeWriteNotification)

	// Check notifications are as expected
	req := NewRequest(t, "GET", "/api/v1/notifications?all=true").
		AddTokenAuth(token)
	resp := MakeRequest(t, req, http.StatusOK)
	apiNL := DecodeJSON(t, resp, []api.NotificationThread{})

	assert.Len(t, apiNL, 4)
	assert.EqualValues(t, 5, apiNL[0].ID)
	assert.True(t, apiNL[0].Unread)
	assert.False(t, apiNL[0].Pinned)
	assert.EqualValues(t, 4, apiNL[1].ID)
	assert.True(t, apiNL[1].Unread)
	assert.False(t, apiNL[1].Pinned)
	assert.EqualValues(t, 3, apiNL[2].ID)
	assert.False(t, apiNL[2].Unread)
	assert.True(t, apiNL[2].Pinned)
	assert.EqualValues(t, 2, apiNL[3].ID)
	assert.False(t, apiNL[3].Unread)
	assert.False(t, apiNL[3].Pinned)

	//
	// Notification ID 2 is the only one with status-type read & pinned
	// change it to unread.
	//
	req = NewRequest(t, "PUT", "/api/v1/notifications?status-types=read&status-type=pinned&to-status=unread").
		AddTokenAuth(token)
	resp = MakeRequest(t, req, http.StatusResetContent)
	apiNL = DecodeJSON(t, resp, []api.NotificationThread{})
	assert.Len(t, apiNL, 1)
	assert.EqualValues(t, 2, apiNL[0].ID)
	assert.True(t, apiNL[0].Unread)
	assert.False(t, apiNL[0].Pinned)

	//
	// Now notification ID 2 is the first in the list and is unread.
	//
	req = NewRequest(t, "GET", "/api/v1/notifications?all=true").
		AddTokenAuth(token)
	resp = MakeRequest(t, req, http.StatusOK)
	apiNL = DecodeJSON(t, resp, []api.NotificationThread{})

	assert.Len(t, apiNL, 4)
	assert.EqualValues(t, 2, apiNL[0].ID)
	assert.True(t, apiNL[0].Unread)
	assert.False(t, apiNL[0].Pinned)
}

func TestAPIInbox(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	// keep readiness assertions on the PR used by this test
	_, err := db.GetEngine(t.Context()).Where("is_pull = ? AND id <> ?", true, 3).Cols("is_closed").Update(&issues_model.Issue{IsClosed: true})
	require.NoError(t, err)
	require.NoError(t, db.Insert(t.Context(), &issues_model.Review{IssueID: 3, ReviewerID: 2, Type: issues_model.ReviewTypeRequest}))
	hidden := &issues_model.Issue{RepoID: 6, Index: 1, PosterID: 10, Title: "private inbox item"}
	require.NoError(t, db.Insert(t.Context(), hidden))
	for _, issueID := range []int64{1, 7, hidden.ID} {
		require.NoError(t, db.Insert(t.Context(), &branchroom_model.AgentAction{Kind: "issue", TargetID: issueID, Agent: "test-agent", TaskURL: "https://example.org/task", Sponsor: "sponsor"}))
		require.NoError(t, issues_model.UpdateIssueUsersByMentions(t.Context(), issueID, []int64{2}))
		require.NoError(t, db.Insert(t.Context(), &activities_model.Notification{UserID: 2, RepoID: map[int64]int64{1: 1, 7: 2, hidden.ID: 6}[issueID], IssueID: issueID, Source: activities_model.NotificationSourceIssue, Status: activities_model.NotificationStatusUnread}))
	}
	comment := &issues_model.Comment{IssueID: 5, PosterID: 2, Type: issues_model.CommentTypeComment, Content: "recent work on a closed issue"}
	require.NoError(t, db.Insert(t.Context(), comment))
	require.NoError(t, db.Insert(t.Context(), &branchroom_model.AgentAction{Kind: "comment", TargetID: comment.ID, Agent: "recent-agent", Sponsor: "sponsor"}))

	hiddenAllowed := false
	read := func(token string) *api.Inbox {
		response := MakeRequest(t, NewRequest(t, "GET", "/api/v1/user/inbox").AddTokenAuth(token), http.StatusOK)
		result := DecodeJSON(t, response, &api.Inbox{})
		require.Len(t, result.Groups, 4)
		for i, kind := range []string{"decision", "blocked", "agents", "mentions"} {
			assert.Equal(t, kind, result.Groups[i].Kind)
			assert.Equal(t, len(result.Groups[i].Items), result.Groups[i].Count)
			for _, item := range result.Groups[i].Items {
				if !hiddenAllowed {
					assert.NotEqual(t, "user10/repo6", item.Repo)
				}
			}
		}
		return result
	}
	result := read(getUserToken(t, "user2", auth_model.AccessTokenScopeReadIssue, auth_model.AccessTokenScopeReadRepository))
	require.Len(t, result.Groups[0].Items, 1)
	assert.Equal(t, "Your review is requested.", result.Groups[0].Items[0].Reason)
	assert.Equal(t, "Review", result.Groups[0].Items[0].Action.Label)
	require.Len(t, result.Groups[2].Items, 3)
	var agentNames []string
	for _, item := range result.Groups[2].Items {
		require.NotNil(t, item.Agent)
		agentNames = append(agentNames, item.Agent.Name)
		assert.Equal(t, "sponsor", item.Agent.Sponsor)
		if item.Agent.Name == "test-agent" {
			assert.Equal(t, "https://example.org/task", item.Agent.TaskURL)
		}
	}
	assert.ElementsMatch(t, []string{"test-agent", "test-agent", "recent-agent"}, agentNames)
	require.Len(t, result.Groups[3].Items, 2)
	assert.Equal(t, "You were mentioned in an unread notification.", result.Groups[3].Items[0].Reason)
	tokenText := getUserToken(t, "user2", auth_model.AccessTokenScopeReadIssue, auth_model.AccessTokenScopeReadRepository)
	readinessResponse := MakeRequest(t, NewRequest(t, "GET", "/api/v1/repos/user2/repo1/pulls/3/readiness").AddTokenAuth(tokenText), http.StatusOK)
	readiness := DecodeJSON(t, readinessResponse, &api.PullRequestReadiness{})
	review := &issues_model.Review{IssueID: 3, ReviewerID: 2, Type: issues_model.ReviewTypeComment, CommitID: readiness.HeadSHA}
	require.NoError(t, db.Insert(t.Context(), review))
	assert.Empty(t, read(tokenText).Groups[0].Items)
	_, err = db.GetEngine(t.Context()).ID(review.ID).Cols("commit_id").Update(&issues_model.Review{CommitID: "previous-head"})
	require.NoError(t, err)
	assert.Len(t, read(tokenText).Groups[0].Items, 1)

	request := NewRequestWithJSON(t, "POST", "/api/v1/users/user2/tokens", api.CreateAccessTokenOption{Name: "inbox-limited", Scopes: []string{"read:issue", "read:repository"}, Repositories: []string{"user2/repo1"}}).AddBasicAuth("user2")
	token := DecodeJSON(t, MakeRequest(t, request, http.StatusCreated), &api.AccessToken{})
	result = read(token.Token)
	require.NotEmpty(t, result.Groups[2].Items)
	require.NotEmpty(t, result.Groups[3].Items)
	for _, group := range result.Groups {
		for _, item := range group.Items {
			assert.Equal(t, "user2/repo1", item.Repo)
		}
	}
	result = read(getUserToken(t, "user2", auth_model.AccessTokenScopePublicOnly, auth_model.AccessTokenScopeReadIssue, auth_model.AccessTokenScopeReadRepository))
	for _, group := range result.Groups {
		for _, item := range group.Items {
			assert.NotEqual(t, "user2/repo2", item.Repo)
		}
	}
	hiddenAllowed = true
	result = read(getUserToken(t, "user1", auth_model.AccessTokenScopeReadIssue, auth_model.AccessTokenScopeReadRepository))
	require.Len(t, result.Groups[1].Items, 1)
	assert.Contains(t, result.Groups[1].Items[0].Reason, "Changes requested")
}
