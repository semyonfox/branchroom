# Security review: token expiry and repository allowlist

Target: `708a428034` "feat(auth): let access tokens expire and be limited to chosen repositories".
Reviewed on a detached checkout of that commit. Product code untouched, temporary tests deleted, `git status` clean.

## Verdict

Expiry is sound. I found no way for an expired token, or a credential derived from one, to keep working on a request path.

The allowlist is not sound yet. The direct repository paths (API repo routes, git HTTP, LFS, packages, Branchroom gateway) hold, but there are three real bypasses and several smaller leaks. Do not ship before fixing 1 to 4.

| # | Severity | What | Proven by |
| --- | --- | --- | --- |
| 1 | High | Commit message keywords change issues in repos outside the list | integration test |
| 2 | High | Notifications API returns threads of repos outside the list | integration test |
| 3 | High | The user's own activity feed (API and RSS) skips the repo filter | integration test |
| 4 | Medium | A site admin's limited token keeps admin reads across the instance | integration test |
| 5 | Medium | Fork and generate create repositories outside the list, including in orgs | integration test |
| 6 | Low | `/v2/_catalog` lists container images outside the list | integration test |
| 7 | Low | A fork's `parent` exposes the private parent outside the list | integration test |
| 8 | Low | Token creation tells a hidden private repo apart from a missing one | integration test |
| 9 | Low | Issue search returns public issues from every repo | integration test |
| 10 | Info | Design gaps and robustness notes | probe and code reading |

All tests used fixture users on SQLite via `make 'test-integration#Name'`. The limited token was `user2`, scope `all`, `repositories: ["user2/repo1"]`. For finding 4 it was `user1` (site admin) with the same list.

## Findings

### 1. High: commit keywords write to issues outside the allowlist

`services/repository/push.go:100` and `:142` reload the pusher with `user_model.GetPossibleUserByID(ctx, opts.PusherID)`. That user has no `ExtDoerData`, so the token restriction is gone. `:185` hands it to `issue_service.UpdateIssuesCommit`, and `services/issue/commit.go:164` then asks `GetDoerRepoPermission(ctx, refRepo, doer)` for the referenced repo with full user rights.

Request, limited token:

```
POST /api/v1/repos/user2/repo1/contents/zz-review.txt
{"message": "reopen user2/repo2#1", "content": "eA=="}
-> 201
```

Evidence after flushing queues:

```
issue user2/repo2#1 is_closed before=true after=false
comments on that issue before=1 after=3
```

`user2/repo2` is private and not in the list. The token reopened its issue and added a commit reference comment carrying the commit's first line. `close`/`fix` keywords work the same way, and so does a plain `git push` or a merge message. An issue-body cross reference (`see user2/repo2#1`) is correctly refused because that path keeps the request doer (comments stayed at 3).

Same root cause, read only, not run: `services/automerge/automerge.go:211` reloads the scheduling doer the same way, then merges and calls `DeleteBranchAfterMerge` (`:243`). A limited token that schedules an auto merge with branch deletion on a pull request whose head is a fork outside the list would delete that branch with full rights, and the merge still runs after the token has expired.

Fix: carry the pusher's ext doer data through the push queue (`PushUpdateOptions` next to `PusherID`, it is already in the hook options as `UserExtDoerData`) and rebuild the doer with `GetDoerPermissionUser`. For auto merge either store the credential with the schedule or refuse scheduling from a repository-limited token.

### 2. High: notifications leak repos outside the allowlist

`services/convert/notification.go:30` computes the permission with `GetIndividualUserRepoPermission(ctx, n.Repository, n.User)`. `n.User` comes from the database, not from the request doer. `routers/api/v1/notify/notifications.go:27,46` filters by `UserID` only.

```
GET /api/v1/notifications?all=true
-> 200, thread id=5 repo=user2/repo2 private=true subject.title="issue4"
   subject.url=.../api/v1/repos/user2/repo2/issues/1

GET /api/v1/notifications/threads/5
-> 200, repo=user2/repo2 subject.title="issue4"
```

The response includes the full repository object, issue title, state and latest comment URLs for every repo the user is notified about.

Fix: add `DoerTokenRepoCond(doer)` on `notification.repo_id` in the find options and in `getThread`, and pass the request doer to the converter. `/notifications/new` should count with the same condition.

### 3. High: own activity feed skips the repo filter

`models/activities/action_list.go:209` short-circuits when the actor is the requested user (or a full site admin) and builds the condition from `user_id` alone. `ActivityQueryCondition`, which contains `AccessibleRepoIDsQuery` and so the allowlist, is never reached.

```
GET /api/v1/users/user2/activities/feeds?limit=50
-> 200, activity id=1 op=close_issue repo=user2/repo2 private=true

GET /user2.rss   (Basic user2:<limited token>)
-> 200, body contains user2/repo2
```

Feed entries carry commit messages, issue titles and comment text. The heatmap, org feed and team feed go through `ActivityQueryCondition` and are filtered correctly.

Fix: add `len(GetDoerRepositoryIDs(opts.Actor)) == 0` to the shortcut condition, or AND `builder.In("repo_id", ids)` into the shortcut branch.

### 4. Medium: a site admin's limited token keeps instance-wide admin reads

`DoerIsSiteAdminFullAccess` was taught about the allowlist, but the API gates still use `ctx.IsUserSiteAdmin()` (`services/context/api.go:268`), which is just `Doer.IsAdmin`. `reqSiteAdmin` (`routers/api/v1/api.go:354`), `reqOrgOwnership` (`:430`), `reqOrgMembership` (`:554`) and `routers/api/v1/notify/threads.go:110` all pass. `models/repo/repo_list.go:818` returns no filter when `ownerID <= 0`, which is what the admin Actions listing uses.

With `user1`'s token limited to `user2/repo1`:

```
GET /api/v1/admin/actions/runs   -> 200, 32895 bytes, includes user2/repo2 and org3/repo5 runs
GET /api/v1/admin/actions/jobs   -> 200, includes user5/repo4, user2/repo2, org3/repo5 jobs
GET /api/v1/admin/users          -> 200, every user
GET /api/v1/admin/emails         -> 200, every email
GET /api/v1/admin/cron           -> 200
GET /api/v1/notifications/threads/5        -> 200, user2's thread for private user2/repo2
GET /api/v1/orgs/org3/actions/variables    -> 200
GET /api/v1/orgs/org3/hooks                -> 200
GET /api/v1/orgs/privated_org/actions/runners -> 200 (not a member)
GET /api/v1/repos/user2/repo2    -> 404 (control, the direct path holds)
```

`?sudo=user2` also works with this token. The allowlist is copied to the impersonated user, so repo routes stay limited, but findings 2 and 3 then apply to any user on the instance.

Writes are blocked by the method guard, and repo and package writes by their own checks. This is read exposure only.

Fix: make `IsUserSiteAdmin()` (API and web) return `DoerIsSiteAdminFullAccess(ctx.Doer)`, or reject repository-limited tokens in `reqSiteAdmin` and `sudo()`. Then `/admin/actions/*` needs the token condition when the caller is not a full admin.

### 5. Medium: fork and generate create repositories outside the allowlist

The guard at `routers/api/v1/api.go:878` lets any non-GET through when the route has a `reponame`. Fork (`:1327`) and generate (`:1260`) act on an allowed source but create a new repository elsewhere.

```
POST /api/v1/repos/user2/repo1/forks {"name":"fork-by-limited"}            -> 202
POST /api/v1/repos/user2/repo1/forks {"organization":"org3","name":"fork-into-org"} -> 202
PATCH /api/v1/repos/user2/repo1 {"template":true}                          -> 200
POST /api/v1/repos/user2/repo1/generate {"owner":"org3","name":"generated-by-limited"} -> 201
```

All three repositories exist in the database afterwards, two of them in `org3`. The token cannot read them back (404), so it cannot do more with them, but it did change account and organisation resources, which the commit message says it cannot. Transfer of the allowed repo to another owner (202) is an action on the allowed repo, I am not counting it.

Fix: reject `CreateFork` and `Generate` when `GetDoerRepositoryIDs(ctx.Doer)` is non-empty. This is also what GitHub does for fine-grained tokens limited to selected repositories.

### 6. Low: container catalog lists images outside the allowlist

`routers/api/packages/api.go:548` serves `/v2/_catalog` without `PackageAssignment`, and `models/packages/container/search.go:252` filters by owner visibility only.

```
GET /v2/_catalog   (Basic user2:<limited token>)
-> 200 {"repositories":["user2/secret-image"]}
```

`user2/secret-image` is not linked to any repo. Names only: tags, manifests and the v1 package API for the same image return 404.

Fix: return an empty catalog, or 404, for repository-limited doers.

### 7. Low: fork exposes its private parent

`services/convert/repository.go:47-57` embeds the base repo using the fork's permission (there is an upstream FIXME on it).

```
GET /api/v1/repos/user2/repo2-fork   (token limited to the fork)
-> 200, parent.full_name=user2/repo2 parent.private=true
   parent.permissions={admin:true push:true pull:true}
GET /api/v1/repos/user2/repo2        -> 404 (control)
```

Metadata only: name, description, counters, default branch. Merge-upstream (403) and compare against the parent (404) are refused.

Fix: in `innerToRepo`, skip the parent when the doer cannot see it. That needs the doer passed in, or a nil parent for limited tokens.

### 8. Low: token creation distinguishes hidden from missing repositories

`services/auth/access_token.go:24-33` returns the raw lookup error for a missing repo and a different message for one the user cannot see. `routers/api/v1/user/app.go:125-129` echoes it.

```
POST /api/v1/users/user2/tokens {"repositories":["user10/repo6"]}
-> 422 "repository not found"                       (exists, private, not visible to user2)
POST /api/v1/users/user2/tokens {"repositories":["user10/no-such-repo"]}
-> 422 "repository does not exist [id: 0, uid: 0, owner_name: user10, name: no-such-repo]"
```

Any user can enumerate private repository names this way. The web form returns one message for both, so only the API is affected.

Fix: return the same error in both branches.

Validation is otherwise correct. Repositories are resolved by name against the token owner's own permission, a repo the owner cannot see is refused, and there is no way to pass an ID. A public repo of another owner is accepted (201), which seems intended.

### 9. Low: issue search ignores the allowlist for public repos

`routers/common/issue_filter.go:95` sets `allPublic` whenever no owner is given, and the indexer then matches every public issue.

```
GET /api/v1/repos/issues/search?state=all
-> repos: user2/repo1 (5) plus org17/big_test_public_4, org3/repo21, user12/repo10,
   user2/glob, user2/commitsonpr, user30/repo50 and others
GET /api/v1/repos/issues/search?state=all&owner=user2
-> repos: user2/repo1 only
```

Public data, so nothing secret, but the same repos return 404 on every direct route with this token, including git clone. Fix: `allPublic = false` when the doer has an allowlist.

### 10. Info: design gaps and robustness

- Account and organisation reads are open to a limited token. Confirmed 200 for `/user/emails`, `/user/keys`, `/user/teams`, `/user/orgs`, `/user/applications/oauth2`, `/user/hooks`, `/orgs/{org}/teams`, `/orgs/{org}/members`, `/orgs/{org}/actions/{runners,secrets,variables}`, `/orgs/{org}/hooks`, and `/users/{name}/tokens` (Basic with the token as password lists every token's name, last eight, scopes and repositories). Only writes are blocked. Decide whether a repository-limited token should see any of this. I would deny it by default.
- `DELETE /api/v1/token` returns 403 for a limited token, so it cannot revoke itself.
- No cap on `repositories`. Each name costs two queries at creation, and every listing gets an `IN (...)` with that many parameters. MSSQL allows 2100 parameters, and the MySQL `TEXT` column holds 64 KB. Both fail closed, but a cap of around 100 avoids it.
- `MAX_TOKEN_DURATION` is checked at creation only. Tokens that predate the setting, or have no expiry, keep working.
- `sudo` with a limited token copies the admin's credential to the target user. `GetDoerPermissionUser` then looks the token up under the target's uid (`models/user/user_system.go:146`) and fails, so anything that goes through git hooks (file edits, merges) errors for that combination. Fails closed.
- When every listed repository has been deleted the token stays restricted (correct), but the settings page shows an empty list.

## Checked and found safe

Direct repository access with the limited token:

- API `repoAssignment` (`/repos/{owner}/{repo}/...`, star, subscription, repo notifications): 404 outside the list. `/repositories/{id}`: 404.
- Listings filtered to the list: `/user/repos`, `/users/{u}/repos`, `/orgs/{o}/repos`, `/repos/search`, `/user/starred`, `/user/subscriptions`, `/users/{u}/starred`, `/users/{u}/subscriptions`, `/teams/{id}/repos`, `/user/times`, `/user/stopwatches`, `/user/actions/{runs,jobs}`, `/orgs/{o}/actions/{runs,jobs}`, owner-level project issues, heatmap, org and team feeds.
- Cross-repo operations from an allowed repo: compare with a head outside the list (404), pull request with such a head (404), issue dependencies and blocks (404), merge-upstream (403), issue-body cross references (no comment created), comment IDs of another repo (no data), team repo add and remove (403).
- Method guard: `POST /user/repos`, `POST /users/{u}/tokens` (Bearer and Basic), `POST /admin/users/{u}/repos`, `POST /markdown` all 403. Runner registration tokens for user, org and admin are POST, so blocked.
- Git over HTTP: repo outside the list, its wiki, receive-pack, push-to-create and public repos (own and other owners, so the anonymous shortcut does not apply) all 404. Allowed repo works.
- Web routes that accept Basic or OAuth2: repo home, raw, archive, repo, tag, release and branch feeds, badge: 404 outside the list. `/attachments/{uuid}` goes through `GetDoerRepoPermission`.
- LFS: batch 404, locks list and create 401. Batch never auto-links an object from another repo.
- Packages: generic download, v1 package API, delete, upload of a new package, link and unlink, npm metadata all 404 unless the package version is linked to an allowed repo. Unlinked owner packages are denied. Container tags and manifests 404.
- Branchroom gateway: `DoerTokenAllowsRepo` runs before workspace authorisation (`branchroom_githttp.go:394`). Only passwords and personal access tokens are accepted there.
- Pull update: `services/pull/update.go:148`.

Derived credentials and expiry:

- Package JWT from `/v2/token` carries the token credential, is re-read from the database on every use (`GetDoerPermissionUser`), keeps the allowlist (linked-outside package 404), and is not accepted by `/api/v1` (401).
- After expiry: API, `/token` introspection, git HTTP on a public repo, raw download, RSS, `/v2/token`, package download, and a package JWT minted before expiry all return 401.
- Token cache stores only the ID and hash. The row is re-read on every hit, so expiry and deletion take effect at once (`models/auth/access_token.go:103-114`).
- LFS JWTs are only minted over SSH and reload the doer the same way. Actions task tokens and runner tokens are separate credentials and never derive from a personal access token.
- OAuth2 grants carry no allowlist, which is right since they are approved per application. A personal access token sent as Bearer goes through `NewAccessTokenExtDoerData` (`services/auth/oauth2.go:149`) and is limited.
- Ext doer data encoding: scope and JSON cannot contain `|`, and the allowlist is taken from the database row, not from the encoded string.

Migration and storage:

- `modelmigration/v29/branchroom9.go` only adds two columns. `ExpiresUnix NOT NULL DEFAULT 0` leaves existing tokens non-expiring, a NULL `repository_ids` loads as unrestricted, and the migration is idempotent (its test runs it twice).
- `JSON TEXT` is `TEXT` on MySQL (64 KB) and PostgreSQL (unbounded). `UpdateAccessToken` writes all columns from a fully loaded row, and `RegenerateAccessToken` only touches the hash columns, so neither drops the restriction.
- Expiry validation: past dates and epoch 0 under a maximum are refused in the model, the API and the web form.

Not run: MySQL, PostgreSQL and MSSQL (SQLite only), SSH, a real `git lfs` or `docker` client, non-database issue indexers, and the auto-merge variant of finding 1.
