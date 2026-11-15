// Copyright 2026 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gitea.dev/modules/json"
	api "gitea.dev/modules/structs"

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Handler creates stateless servers bound to the current request's credentials.
func Handler(rest http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
			return newServer(&client{rest: rest, authorization: r.Header.Get("Authorization"), otp: r.Header.Get("X-Gitea-OTP"), remoteAddr: r.RemoteAddr})
		}, &mcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true, MaxRequestBodyBytes: 12 * 1024 * 1024}).ServeHTTP(w, r)
	}
}

type client struct {
	rest                           http.Handler
	authorization, otp, remoteAddr string
}

type response struct {
	header   http.Header
	code     int
	body     bytes.Buffer
	overflow bool
}

func (r *response) Header() http.Header  { return r.header }
func (r *response) WriteHeader(code int) { r.code = code }
func (r *response) Write(p []byte) (int, error) {
	if r.body.Len()+len(p) > 8*1024*1024 {
		r.overflow = true
		return 0, errors.New("MCP response limit exceeded")
	}
	return r.body.Write(p)
}

type isolatedContext struct{ context.Context }

func (isolatedContext) Value(any) any { return nil }

func (c *client) request(ctx context.Context, method, path, contentType string, body io.Reader) (string, error) {
	// each dispatch needs its own request data and cleanup lifecycle
	ctx = context.WithValue(isolatedContext{ctx}, chi.RouteCtxKey, chi.NewRouteContext())
	r, err := http.NewRequestWithContext(ctx, method, path, body)
	if err != nil {
		return "", err
	}
	r.Header.Set("Authorization", c.authorization)
	r.Header.Set("X-Gitea-OTP", c.otp)
	r.Header.Set("Content-Type", contentType)
	r.RemoteAddr = c.remoteAddr
	w := &response{header: make(http.Header), code: http.StatusOK}
	c.rest.ServeHTTP(w, r)
	if w.overflow {
		return "", errors.New("REST response exceeds 8 MiB, narrow the request")
	}
	if w.code < 200 || w.code >= 300 {
		return "", fmt.Errorf("REST returned %d: %s", w.code, w.body.String())
	}
	return w.body.String(), nil
}

func (c *client) json(ctx context.Context, method, path string, body any) (string, error) {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return "", err
		}
	}
	return c.request(ctx, method, path, "application/json", bytes.NewReader(data))
}

type repository struct {
	Owner string `json:"owner" jsonschema:"Repository owner"`
	Repo  string `json:"repo" jsonschema:"Repository name"`
}

func (r repository) path() (string, error) {
	for _, part := range []string{r.Owner, r.Repo} {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "/\\%?#") {
			return "", errors.New("invalid owner or repository")
		}
	}
	return "/api/v1/repos/" + url.PathEscape(r.Owner) + "/" + url.PathEscape(r.Repo), nil
}

type pullInput struct {
	repository
	Index int64 `json:"index" jsonschema:"Issue or pull request number"`
}

func (p pullInput) path(kind string) (string, error) {
	base, err := p.repository.path()
	if err != nil {
		return "", err
	}
	if p.Index <= 0 {
		return "", errors.New("index must be positive")
	}
	return base + "/" + kind + "/" + strconv.FormatInt(p.Index, 10), nil
}

func result(text string, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil, nil
}

func newServer(c *client) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "Branchroom", Version: "1.0.0"}, nil)
	mcp.AddTool(s, &mcp.Tool{Name: "inbox", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "Get your review decisions, blocked work, agent activity and unread mentions."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		View string `json:"view,omitempty" jsonschema:"Inbox view: needs, triage, yours, agents, mentions, snoozed or done"`
	},
	) (*mcp.CallToolResult, any, error) {
		text, err := c.json(ctx, "GET", "/api/v1/user/inbox?view="+url.QueryEscape(a.View), nil)
		return result(text, err)
	})

	mcp.AddTool(s, &mcp.Tool{Name: "find_work", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "Find issues and PRs using REST search filters, including checks and review. Query is a URL query string. Results are paginated."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		Query string `json:"query"`
	},
	) (*mcp.CallToolResult, any, error) {
		q, err := url.ParseQuery(a.Query)
		if err != nil {
			return result("", err)
		}
		text, err := c.json(ctx, "GET", "/api/v1/repos/issues/search?"+q.Encode(), nil)
		return result(text, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "read_pr", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "Read a PR, readiness, reviews and their first page of inline threads. Use review_threads to page through longer conversations."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		pullInput
		Page  int `json:"page,omitempty"`
		Limit int `json:"limit,omitempty"`
	},
	) (*mcp.CallToolResult, any, error) {
		path, err := a.path("pulls")
		if err != nil {
			return result("", err)
		}
		var bundle struct {
			Pull      api.PullRequest                   `json:"pull"`
			Readiness api.PullRequestReadiness          `json:"readiness"`
			Reviews   []api.PullReview                  `json:"reviews"`
			Threads   map[int64][]api.PullReviewComment `json:"threads"`
		}
		for _, part := range []struct {
			suffix string
			target any
		}{
			{"", &bundle.Pull}, {"/readiness", &bundle.Readiness}, {fmt.Sprintf("/reviews?page=%d&limit=%d", a.Page, a.Limit), &bundle.Reviews},
		} {
			text, err := c.json(ctx, "GET", path+part.suffix, nil)
			if err != nil {
				return result("", err)
			}
			if err := json.Unmarshal([]byte(text), part.target); err != nil {
				return result("", err)
			}
		}
		bundle.Threads = make(map[int64][]api.PullReviewComment)
		for _, review := range bundle.Reviews {
			if review.CodeCommentsCount == 0 {
				continue
			}
			text, err := c.json(ctx, "GET", fmt.Sprintf("%s/reviews/%d/comments", path, review.ID), nil)
			if err != nil {
				return result("", err)
			}
			var comments []api.PullReviewComment
			if err := json.Unmarshal([]byte(text), &comments); err != nil {
				return result("", err)
			}
			bundle.Threads[review.ID] = comments
		}
		text, err := json.Marshal(bundle)
		return result(string(text), err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "review_threads", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "Read inline review comments with resolver and line information. Supply a review ID from read_pr. Paginated."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		pullInput
		ReviewID int64 `json:"review_id"`
		Page     int   `json:"page,omitempty"`
		Limit    int   `json:"limit,omitempty"`
	},
	) (*mcp.CallToolResult, any, error) {
		path, err := a.path("pulls")
		if err != nil {
			return result("", err)
		}
		if a.ReviewID <= 0 {
			return result("", errors.New("review_id must be positive"))
		}
		text, err := c.json(ctx, "GET", fmt.Sprintf("%s/reviews/%d/comments?page=%d&limit=%d", path, a.ReviewID, a.Page, a.Limit), nil)
		return result(text, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "get_diff", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "Get a PR's unified diff."}, func(ctx context.Context, _ *mcp.CallToolRequest, a pullInput) (*mcp.CallToolResult, any, error) {
		path, err := a.path("pulls")
		if err != nil {
			return result("", err)
		}
		text, err := c.json(ctx, "GET", path+".diff", nil)
		return result(text, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "get_checks", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "Get commit checks, including target URLs. Use the head SHA from readiness."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		repository
		SHA string `json:"sha"`
	},
	) (*mcp.CallToolResult, any, error) {
		path, err := a.path()
		if err != nil {
			return result("", err)
		}
		if len(a.SHA) != 40 && len(a.SHA) != 64 {
			return result("", errors.New("sha must be a full commit hash"))
		}
		for _, ch := range a.SHA {
			if !strings.ContainsRune("0123456789abcdefABCDEF", ch) {
				return result("", errors.New("invalid sha"))
			}
		}
		text, err := c.json(ctx, "GET", path+"/commits/"+a.SHA+"/status", nil)
		return result(text, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "get_job_logs", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "Get plain text logs for an Actions job ID. External checks expose their logs through target URLs in get_checks."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		repository
		JobID int64 `json:"job_id"`
	},
	) (*mcp.CallToolResult, any, error) {
		path, err := a.path()
		if err != nil {
			return result("", err)
		}
		if a.JobID <= 0 {
			return result("", errors.New("job_id must be positive"))
		}
		text, err := c.json(ctx, "GET", fmt.Sprintf("%s/actions/jobs/%d/logs", path, a.JobID), nil)
		return result(text, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "comment", Description: "Comment on an issue or PR."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		pullInput
		Body string `json:"body"`
	},
	) (*mcp.CallToolResult, any, error) {
		path, err := a.path("issues")
		if err != nil {
			return result("", err)
		}
		text, err := c.json(ctx, "POST", path+"/comments", api.CreateIssueCommentOption{Body: a.Body})
		return result(text, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "submit_review", Description: "Submit a PR review. Event: APPROVED, REQUEST_CHANGES or COMMENT. Optional inline comments use REST review comment fields."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		pullInput
		Event    api.ReviewStateType           `json:"event"`
		Body     string                        `json:"body"`
		CommitID string                        `json:"commit_id,omitempty"`
		Comments []api.CreatePullReviewComment `json:"comments,omitempty"`
	},
	) (*mcp.CallToolResult, any, error) {
		path, err := a.path("pulls")
		if err != nil {
			return result("", err)
		}
		text, err := c.json(ctx, "POST", path+"/reviews", api.CreatePullReviewOptions{Event: a.Event, Body: a.Body, CommitID: a.CommitID, Comments: a.Comments})
		return result(text, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "reply_to_thread", Description: "Reply to an inline review comment ID on a PR."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		pullInput
		CommentID int64  `json:"comment_id"`
		Body      string `json:"body"`
	},
	) (*mcp.CallToolResult, any, error) {
		path, err := a.path("pulls")
		if err != nil {
			return result("", err)
		}
		if a.CommentID <= 0 {
			return result("", errors.New("comment_id must be positive"))
		}
		text, err := c.json(ctx, "POST", fmt.Sprintf("%s/comments/%d/replies", path, a.CommentID), api.CreatePullReviewCommentReplyOptions{Body: a.Body})
		return result(text, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "resolve_thread", Description: "Resolve or unresolve an inline review thread using its comment ID."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		repository
		CommentID int64 `json:"comment_id"`
		Resolved  bool  `json:"resolved"`
	},
	) (*mcp.CallToolResult, any, error) {
		path, err := a.path()
		if err != nil {
			return result("", err)
		}
		if a.CommentID <= 0 {
			return result("", errors.New("comment_id must be positive"))
		}
		action := "resolve"
		if !a.Resolved {
			action = "unresolve"
		}
		text, err := c.json(ctx, "POST", fmt.Sprintf("%s/pulls/comments/%d/%s", path, a.CommentID, action), nil)
		return result(text, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "upload_attachment", Description: "Upload base64 evidence to an issue or PR. Existing attachment size and type restrictions apply."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		pullInput
		Name    string `json:"name"`
		Content string `json:"content" jsonschema:"Base64 encoded file"`
	},
	) (*mcp.CallToolResult, any, error) {
		path, err := a.path("issues")
		if err != nil {
			return result("", err)
		}
		data, err := base64.StdEncoding.DecodeString(a.Content)
		if err != nil {
			return result("", err)
		}
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		part, err := writer.CreateFormFile("attachment", a.Name)
		if err != nil {
			return result("", err)
		}
		if _, err = part.Write(data); err != nil {
			return result("", err)
		}
		if err = writer.Close(); err != nil {
			return result("", err)
		}
		text, err := c.request(ctx, "POST", path+"/assets", writer.FormDataContentType(), &buf)
		return result(text, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "set_labels", Description: "Replace issue or PR labels by name. An empty list clears labels."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		pullInput
		Labels []string `json:"labels"`
	},
	) (*mcp.CallToolResult, any, error) {
		path, err := a.path("issues")
		if err != nil {
			return result("", err)
		}
		text, err := c.json(ctx, "PUT", path+"/labels", struct {
			Labels []string `json:"labels"`
		}{Labels: a.Labels})
		return result(text, err)
	})
	mcp.AddTool(s, &mcp.Tool{Name: "wait_for_pr_change", Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}, Description: "Wait up to 30 seconds for the readiness fingerprint to change. Returns readiness on change or timeout. Cancel the request to stop waiting."}, func(ctx context.Context, _ *mcp.CallToolRequest, a struct {
		pullInput
		Fingerprint string `json:"fingerprint"`
	},
	) (*mcp.CallToolResult, any, error) {
		path, err := a.path("pulls")
		if err != nil {
			return result("", err)
		}
		text, err := c.wait(ctx, path+"/readiness", a.Fingerprint, 30*time.Second)
		return result(text, err)
	})
	return s
}

func (c *client) wait(ctx context.Context, path, fingerprint string, timeout time.Duration) (string, error) {
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var text string
	for {
		var err error
		text, err = c.json(ctx, "GET", path, nil)
		if err != nil {
			return "", err
		}
		var readiness api.PullRequestReadiness
		if err = json.Unmarshal([]byte(text), &readiness); err != nil {
			return "", err
		}
		if readiness.Fingerprint != fingerprint {
			return text, nil
		}
		select {
		case <-ctx.Done():
			if parent.Err() != nil {
				return "", parent.Err()
			}
			return text, nil
		case <-ticker.C:
		}
	}
}
