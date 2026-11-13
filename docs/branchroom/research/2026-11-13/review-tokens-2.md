# Security re-review: repository-limited tokens, round two

Target: `106068bcff` (fixes in `d30d5f2ddc`, `2e490bd8f6`, `106068bcff`, picker change `b0db031d74`), reviewed on a detached checkout. Product code untouched, temporary test deleted, `git status` clean, no branch created or moved.

## Verdict

**Safe to deploy: yes, on security grounds.** Every bypass from round one is closed or reduced to a low-severity leak. I could not get a limited token to read or write outside its repository list through commit references, notifications, feeds, admin routes, sudo, fork, generate, push-to-create or the container catalog.

Two things to know before you deploy:

1. Auto-merges that a limited token scheduled before this change run with the user's full rights, even after the token expires (N7). Check `pull_auto_merge` once after migrating.
2. Three functional bugs, none a security hole, will bite agents and admin automation: N1 (the guard blocks legitimate writes to paths ending in `/forks` or `/generate`), N2 (a push loses its post-processing if the token is revoked before the queue runs), N3 (sudo with any token breaks hook-backed writes). I would fix N1 before agents rely on the API for file writes.

## Round-one findings

| # | Round-one finding | Status | Evidence at HEAD |
| --- | --- | --- | --- |
| 1 | Commit keywords change issues outside the list | Fixed | `user2/repo2#1` stayed `is_closed=true comments=1` after: API commit `reopen user2/repo2#1` (201), real `git push` over HTTP with the token, the same push with `-o` push options, API merge with that merge message, and an auto-merge with that message. `close #1` in the allowed repo still closed `user2/repo1#1` |
| 1b | Auto-merge variant (not run last time) | Fixed for new schedules | See "Auto-merge" below |
| 2 | Notifications leak | Fixed | `/notifications?all=true`, `/notifications/new`, `/notifications/threads/5`, `PUT /notifications`, `PATCH /notifications/threads/5` all 403. `/repos/user2/repo1/notifications` 200 with repo1 only, `/repos/user2/repo2/notifications` 404 |
| 3 | Own activity feed skips the filter | Fixed | API feed 403. `/user2.rss` and `/user2.atom` return an empty feed (1 title) for the limited token and for a limited admin token, against 2 titles with `user2/repo2` links for the unrestricted token. `/org3.rss` same pattern |
| 4 | Limited admin token keeps admin reads | Partly fixed | `/admin/{users,emails,cron,orgs,hooks,actions/runs,actions/jobs}`, `/orgs/org3/{hooks,actions/variables}`, `/orgs/privated_org/actions/runners` all 403. `?sudo=user2` and the `Sudo:` header 403. Still open: admin view of user profiles, see N4 |
| 5 | Fork and generate create repositories | Fixed | See "Fork and oracle" below |
| 6 | `/v2/_catalog` lists images | Fixed | 404 with the token as Basic, 404 with a package JWT minted from it, 200 for the unrestricted control |
| 7 | Fork exposes private parent | Fixed | `GET /repos/user20/big_test_private_fork_7` and `/repos/search` return no `parent` key for the limited token, `"parent":{"id":28,...org19...}` for the unrestricted control |
| 8 | Token creation existence oracle | Fixed | Identical `422 {"message":"repository not found"}` for hidden `user10/repo6`, missing `user10/no-such-repo`, missing owner `no-such-owner/repo`, `user10/REPO6` and `user2/repo1/extra` |
| 9 | Issue search returns all public issues | Fixed | `/repos/issues/search?state=all&limit=50` returns `user2/repo1` (5) only, `owner=org3` returns nothing, `/repos/search` returns `user2/repo1` only |

The committed tests `TestAPITokenRepositoryBoundaries` and `TestAPITokenRestrictions` pass, as do the existing auto-merge, sudo and admin API tests.

## Fork and oracle (task 3)

Limited token, `repositories: ["user2/repo1"]`:

```
POST /repos/user2/repo1/forks                         -> 403
POST /repos/user2/repo1/forks/                        -> 403
POST /repos/user2/repo1/forks?x=1 {organization:org3} -> 403
POST /repos/user2/repo1/forks {organization:no-such}  -> 403 (same body, so no owner oracle)
POST /repos/user2/repo1//forks                        -> 403
POST /repos/user2/repo1/Forks, /fork%73               -> 404 (no such route)
POST /repos/user2/repo1/generate (owner org3, user2)  -> 403
POST /repos/migrate, /user/repos, /orgs/org3/repos    -> 403
git push to user2/zz-push-create.git                  -> 404
repository rows before=61 after=61
```

The fork is refused and nothing is created. The token-creation oracle is gone (row 8). One smaller oracle remains on a route the guard lets through, see N6.

## Attacks on the fixes

### Can a client set or strip the restriction?

No path found.

- SSH: `cmd/serv.go:328` always appends `GITEA_PUSHER_EXT_DOER_DATA=<server value>` after `os.Environ()`, empty included, so a value smuggled through `SendEnv` loses. The built-in SSH server forwards only `GIT_PROTOCOL` (`modules/ssh/ssh.go:69`). Code reading, SSH not run.
- HTTP: the hook environment is the server's own plus `DoerPushingEnvironment` (`routers/web/repo/githttp.go:270`). The only client-influenced variable is `GIT_PROTOCOL`, limited by a strict regex (`githttp.go:352`).
- Push options: `cmd/hook.go:488` reads `GIT_PUSH_OPTION_*` into a map. `UserExtDoerData` comes from the environment only (`cmd/hook.go:270,406,596`). A real push with `-o GITEA_PUSHER_EXT_DOER_DATA= -o repo.private=false` changed nothing.
- Internal API: `POST /api/internal/hook/post-receive/user2/repo1` with the token as Bearer and as `X-Gitea-Internal-Auth` returns 403. An empty bearer returns 403. It needs `INTERNAL_TOKEN` (`routers/private/internal.go:25`).

### Password or SSH push mistaken for limited, or the reverse?

No. A password push and an unrestricted-token push with `reopen user2/repo2#1` each added the commit reference comment on `user2/repo2#1` (comments 4 to 5, then 5 to 6). They went to a non-default branch, so the reference was recorded without reopening, which is normal. The limited token's pushes added nothing. Empty ext data takes the old `GetPossibleUserByID` path (`services/repository/push.go:98`), so deploy keys, Actions tasks and sessions behave as before.

### Auto-merge

Branch protection on `master` with a required status check, pull request in `user2/repo1`, scheduled through the API by the limited token with merge message `reopen user2/repo2#1`:

```
schedule                         -> 201, row ext="access-token:4|all|{"RepositoryIDs":[1],...}"
token expired, checks pass       -> merged=false, schedule still present
token valid again, checks pass   -> merged=true, user2/repo2#1 still closed, comments unchanged
token revoked, checks pass       -> merged=false, schedule still present
```

Expiry, revocation and the allowlist are all re-read from the database at merge time (`models/user/user_system.go:141-153`), not trusted from the stored string. Sound.

Legacy rows are the gap, see N7.

### Deny-by-default guard

Blocked (403) with the limited token: `/user/{emails,keys,gpg_keys,teams,orgs,repos,followers,following,starred,subscriptions,times,stopwatches,applications/oauth2,hooks,settings,actions/*,blocks}`, `/users/user2/{tokens,repos,orgs,keys,gpg_keys,followers,heatmap,starred,subscriptions}` (Bearer and Basic), `/users/search`, `/orgs`, `/orgs/org3` and everything under it, `/teams/1`, `/teams/1/{members,repos,activities/feeds}`, `/repositories/{id}`, `/packages/user2`, `/topics/search`, `/gitignore/templates`, `/licenses`, `/label/templates`, `/signing-key.gpg`, `POST /markdown`, `DELETE /token`.

Allowed (200): `/user`, `/version`, `/settings/api`, `/settings/ui`, `/users/{name}`, `/repos/search`, `/repos/issues/search`. A private user still returns 404 to a non-admin token.

Agent workflow on the allowed repo, all fine: read repo, list and create issues, comment, labels, list and create branches, create file, open pull request, read it and its files, post a review, post and read commit statuses, contents, trees, collaborators, assignees, releases, hooks, Actions runs, merge. An issue body that mentions `user2/repo2#1` creates no cross reference.

Gaps and false positives are N1, N4, N5 and N6.

## New findings

### N1. Medium (functional): the guard blocks legitimate writes ending in `/forks` or `/generate`

`routers/api/v1/api.go:870-871` matches on the path suffix of any repository route, not on the two routes it means.

```
POST   /repos/user2/repo1/contents/scripts/generate     -> 403 (201 with an unrestricted token)
POST   /repos/user2/repo1/contents/docs/forks           -> 403
DELETE /repos/user2/repo1/branches/feature/generate     -> 403 (creating it worked: 201)
DELETE /repos/user2/repo1/branches/wip/forks            -> 403
PUT    /repos/user2/repo1/topics/forks                  -> 403 (topics/other: 204)
```

An agent cannot write a file called `generate` or `forks` through the API, or delete a branch with that last segment. `git push` is unaffected. Not a security problem, but it is the kind of failure an agent will not understand.

Fix: drop the suffix test and put the refusal on the two routes (`api.go:1278` generate, `api.go:1345` forks POST) as a middleware or the first line of `repo.Generate` and `repo.CreateFork`.

### N2. Medium (robustness): a push loses its post-processing if the token is gone when the queue runs

`services/repository/push.go:98-105` returns the reload error, and `pushQueueHandler` (`:53`) only logs it. The refs are already written by then.

Reproduction: same `PushUpdateOptions` for a new branch in `user2/repo1`, three credentials:

```
valid limited token            action rows 9  -> 17
revoked token, same ext data   action rows 25 -> 25
no credential                  action rows 33 -> 41
```

With a revoked or expired token the whole batch is dropped: no feed entry, no webhook, no Actions trigger, no pull request head update, no tag sync. It hits an orchestrator that revokes an agent's token straight after its last push, or a token that expires in the same second. The window is short in normal operation and wider when the queue is backed up.

Fix: on a not-found or expired token, keep going with a doer that carries the decoded restrictions (or none and skip `UpdateIssuesCommit`), since the push itself was already authorised in pre-receive.

### N3. Medium (functional regression from `708a428034`): sudo with any token breaks hook-backed writes

`routers/api/v1/api.go:133` copies the admin's token credential to the impersonated user. The hook then calls `GetDoerPermissionUser`, which since `708a428034` looks the token up under the impersonated user's id (`models/user/user_system.go:146`) and fails.

```
GET  /api/v1/user?sudo=user2  (unrestricted admin token)                    -> 200
POST /api/v1/repos/user2/repo1/contents/zz-sudo.txt?sudo=user2 (same token) -> 403 "Internal Server Error (no message for end users)"
```

In round one I noted this only for limited tokens and called it fail-closed. It affects every admin token, so sudo can no longer commit files, merge or manage branches. Limited tokens can no longer sudo at all, so there is no security angle.

Fix: in `GetDoerPermissionUser`, load the token by id alone and accept it when it belongs to the doer or to a site admin, or have `sudo()` drop the credential and keep only the scope.

### N4. Low (security): a limited admin token still gets the admin view of user profiles

`/users/{username}` is on the allowlist, and three checks behind it use `Doer.IsAdmin` directly: `models/user/user.go:1497` (`IsUserVisibleToViewer`), `services/convert/user.go:48` and `routers/api/v1/user/user.go:199`.

```
GET /users/user31 (private profile)  limited admin token -> 200, email, login_name, last_login, active, prohibit_login
                                     limited user2 token -> 404
GET /users/user2 (email kept private) limited admin token -> "email":"user2@example.com", "login_name":"user2"
                                      limited user4 token -> "email":"2+user2@noreply.example.org", "login_name":""
```

One user at a time, names must be known, read only. This is the remainder of finding 4.

Fix: use `DoerIsSiteAdminFullAccess(doer)` in those three places.

### N5. Low (security): OIDC userinfo returns org and team membership to a limited token

`/login/oauth/userinfo` accepts a personal access token as Bearer and is outside the API guard (`routers/web/auth/oauth2_provider.go:96-131`).

```
GET /login/oauth/userinfo (limited token) -> 200
{"sub":"2","preferred_username":"user2","email":"user2@example.com",
 "groups":["org17","org17:test_team","org3","org3:owners","org3:team1","org3:teamcreaterepo","private_org35","private_org35:team24"]}
```

That is what `/user/orgs` and `/user/teams` now refuse, including a private organisation. The email is already in `GET /user`.

Fix: omit `groups` (or refuse the request) when `GetDoerRepositoryIDs(ctx.Doer)` is non-empty.

### N6. Low (security): team repository routes pass the guard

`limitedTokenRouteAllowed` treats any route with a `reponame` parameter as a repository route (`api.go:869`). `/teams/{teamid}/repos/{org}/{reponame}` (`api.go:1838`, `:1848`) has one but is an organisation route without `repoAssignment`.

```
token limited to user2/repo1:
GET /teams/1/repos/org3/repo3 -> 404 {"message":"not found"}
GET /teams/1/repos/org3/nope  -> 404 {"message":"repository does not exist [id: 0, uid: 3, owner_name: , name: nope]"}

token limited to org3/repo3:
PUT    /teams/2/repos/org3/repo3 -> 204   team_repo rows for repo3: 2 -> 3
DELETE /teams/2/repos/org3/repo3 -> 204   3 -> 2
```

The first pair tells an existing repository outside the list from a missing one, for organisations the user has a team in. The second changes an organisation team's repository list. It only ever concerns the allowed repo, and `PUT /repos/{o}/{r}/teams/{team}` does the same by design, so the impact is small. It is a hole in the stated rule that organisation resources are denied.

Fix: require the path to start with `/api/v1/repos/` (plus `/api/v1/user/starred/` and `/api/v1/user/subscriptions/` if wanted) before trusting `reponame`.

### N7. Low (security, upgrade only): auto-merges scheduled before the change run unrestricted

Existing `pull_auto_merge` rows get an empty `user_ext_doer_data`, which is also what a session-scheduled merge has, so `GetDoerPermissionUser` returns the plain user (`models/user/user_system.go:124-126`).

Reproduction: schedule with the limited token, blank the column to imitate a pre-migration row, expire the token, pass the checks:

```
legacy row, token expired -> merged=true
user2/repo2#1             -> is_closed=false, comments 1 -> 3
```

The merge ran after expiry and the commit reference reopened an issue outside the list. Exposure is limited to schedules made by limited tokens between deploying `708a428034` and this fix. The rows cannot be told apart from session ones, so a migration cannot fix it.

Action: after migrating, run `SELECT pull_id, doer_id FROM pull_auto_merge WHERE user_ext_doer_data IS NULL OR user_ext_doer_data = ''` and cancel any that an agent scheduled. If limited tokens were never used to schedule merges on the live instance, there is nothing to do.

Related, by design but worth knowing: when the token expires or is revoked the schedule row stays (`services/automerge/automerge.go:211-214` returns an error, nothing deletes the row). The pull request keeps showing as scheduled and will merge if the same token becomes valid again. Deleting the schedule on a not-found or expired token would be cleaner.

### N8. Info

- `POST /repos/user2/repo1/transfer {"new_owner":"org3"}` still returns 202 and moves the allowed repo into the organisation. It acts on the allowed repo and the allowlist follows it by id, but it does change what an organisation owns. Decide whether transfer belongs with fork and generate.
- `HEAD /api/v1/user` returns 403 because the allowlist checks for GET only. `DELETE /api/v1/token` still returns 403, so a limited token cannot revoke itself.
- `/user/repos` is now 403. Clients that discover repositories that way (tea, some MCP servers) need `/repos/search`. Expected under the product decision, but it will surprise tools.
- Push updates queued before the upgrade carry no credential and are processed unrestricted. Only matters for a persistent queue with a backlog at upgrade time.
- A repo admin token can still leave access behind that outlives it (deploy key, webhook, collaborator, runner registration token). Inherent to admin rights on the repo, same as GitHub.

## Surfaces checked

- Commit references: API file create, real git push over HTTP, push options, API merge, auto-merge, own-repo control, password and unrestricted-token controls.
- Hook transport: SSH and HTTP environment construction, push options, internal API auth (code reading plus two live probes).
- Auto-merge: schedule, expiry, revocation, allowlist at merge time, legacy row.
- API guard: 75 account, organisation, team, admin and misc GET routes, write routes, the allowlisted routes, starred and subscription routes, sudo by parameter and header.
- Agent workflow: 30 calls on the allowed repo covering issues, pull requests, reviews, contents, branches, statuses, topics and merge.
- Token entry points outside `/api/v1`: user and organisation RSS and Atom feeds, `/v2/_catalog` with token and package JWT, `/login/oauth/userinfo`, web repo home with Basic.
- Token creation errors for hidden, missing and malformed repository names.
- Migration `branchroom11`: adds one nullable `TEXT` column, no data change (code reading).

Not run: MySQL, PostgreSQL and MSSQL (SQLite only), a real SSH push, the Branchroom gateway receive path (by code reading it uses its own pre-receive and does not feed the push queue through the pusher environment), the web form for token creation, and a live race for N2 (I drove the queue handler directly with a revoked token's credential).
