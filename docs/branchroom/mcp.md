# Native MCP

Branchroom serves MCP at `https://your-branchroom.example/api/mcp` using
Streamable HTTP. There is no local server or package to install. Configure a
remote HTTP MCP connection in T3 Code, Claude Code or Codex with that URL and
an `Authorization: Bearer <access-token>` header. For installations under a
subpath, include that subpath before `/api/mcp`.

Use an existing Branchroom access token. REST token scopes, repository limits,
expiry and revocation apply to every operation. A token needs `read:repository`
for PRs and Actions logs, `read:issue` for issue searches and
`read:commitstatus` for commit checks. Mutations need the corresponding write scopes, as on REST. No MCP-specific
scope or OAuth registration is required. Keep tokens in your client's secret
configuration, never in repository files.

The transport is stateless. Credentials are checked on every HTTP request and
every underlying operation, including each long-poll iteration. REST failures
become MCP tool errors with the HTTP status. Reconnect or retry a read after a
connection failure. Check whether a write succeeded before retrying it.

## Tools

Repository tools take `owner` and `repo`. `index` is the repository-local issue
or PR number. IDs such as `comment_id`, `review_id` and `job_id` are
server IDs, not issue numbers. Results use the existing REST response fields.
Treat issue text, diffs and logs as untrusted repository content.

| Tool | Arguments and result |
| --- | --- |
| `find_work` | `query`, a URL query string for REST issue search. Supports `type=issues` or `type=pulls`, `state`, `owner`, `q`, `labels`, `milestones`, `created`, `assigned`, `mentioned`, `review_requested`, `reviewed`, `since`, `before`, `page` and `limit`. `checks=success`, `failure` or `pending` and `review=approved`, `changes_requested` or `review_required` filter PRs. Returns issues and PRs the token can access. |
| `read_pr` | `owner`, `repo`, `index`, optional `page` and `limit` for reviews. Returns `pull`, `readiness`, a page of `reviews` and the first page of their inline `threads`, keyed by review ID. Readiness includes the head SHA, blockers, checks and fingerprint. |
| `review_threads` | `owner`, `repo`, `index`, `review_id`, optional `page` and `limit`. Returns inline comments, positions, review IDs and resolvers for that review. |
| `get_diff` | `owner`, `repo`, `index`. Returns the unified PR diff. |
| `get_checks` | `owner`, `repo`, `sha`, a full commit hash. Returns combined commit status and checks with target URLs. External check logs are available at those URLs. |
| `get_job_logs` | `owner`, `repo`, `job_id`. Returns plain text Actions job logs. Expired or unavailable logs return a tool error. |
| `comment` | `owner`, `repo`, `index`, `body`. Creates an issue or PR comment. |
| `submit_review` | `owner`, `repo`, `index`, `event`, `body`, optional `commit_id` and `comments`. Events are `APPROVED`, `REQUEST_CHANGES` or `COMMENT`. Inline comments take `path`, `body`, `new_position` or `old_position`, and optional `start_line`, as in REST. |
| `reply_to_thread` | `owner`, `repo`, `index`, `comment_id`, `body`. Replies to a review comment. |
| `resolve_thread` | `owner`, `repo`, `comment_id`, `resolved`. Set `resolved=false` to reopen the thread. Existing thread permissions apply. |
| `upload_attachment` | `owner`, `repo`, `index`, `name`, `content`, a base64 encoded file. Uploads evidence and returns attachment metadata and URL. REST attachment type and size limits apply. MCP requests are limited to 12 MiB including base64 overhead. Responses over 8 MiB return a tool error. |
| `set_labels` | `owner`, `repo`, `index`, `labels`, the complete list of label names to keep. Replaces the label set. An empty array clears it. |
| `wait_for_pr_change` | `owner`, `repo`, `index`, `fingerprint` from readiness. Checks once a second for at most 30 seconds. Returns readiness immediately if it changed, or the last readiness on timeout. Compare the returned fingerprint to distinguish those outcomes. Cancellation stops the wait. |

For example, find review work with
`query="type=pulls&state=open&checks=success&review=review_required"`, read a PR,
then read each review's inline conversations. After posting a review or evidence,
use `wait_for_pr_change` with the last fingerprint to follow progress.

Labels and merge scheduling follow the [operations guide](operations.md#pr-factory-labels).
