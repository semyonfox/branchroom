# Branchroom operations

This runbook covers running Branchroom with real restricted source. It assumes the
supported shape below. Anything outside it is unsupported, not merely untested.

## Supported deployment

- One self-hosted, single-tenant Gitea server run by a trusted operator.
- SQLite, PostgreSQL or MySQL. Branchroom refuses to start on MSSQL.
- HTTP or HTTPS. Branchroom refuses to start under FastCGI.
- `[security] DISABLE_GIT_HOOKS = true`, the default. Branchroom refuses to start
  otherwise, because editable server-side hooks run as the service account.
- A TLS-terminating reverse proxy when the server is reachable over a network.
- No Gitea Actions runner on the same host or under the same account. A runner there
  can read the restricted root.
- A dedicated service account. Anything else running as that account, such as another
  CI runner or agent, can read the restricted root.
- Git over SSH through Gitea's built-in server (`START_SSH_SERVER = true` on its own
  port) unless the service account is dedicated. The default mode writes users' keys
  into the service account's `~/.ssh/authorized_keys`.

Site administrators, host administrators and database operators are trusted. Branchroom
does not protect restricted source from them. An internet-facing or multi-tenant
deployment needs an independent security review first, see
[production readiness](#production-readiness).

## Install

1. Create the restricted root as the service account, mode `0700`, outside
   `[repository] ROOT` and outside the served `public`, `templates` and `options`
   directories. Its parents must not be writable by group or other unless sticky.

   ```sh
   install -d -m 0700 -o git -g git /var/lib/gitea/branchroom
   ```

2. Configure it.

   ```ini
   [branchroom]
   ENABLED = true
   RESTRICTED_ROOT = /var/lib/gitea/branchroom
   ;MAX_WORKSPACES_PER_OWNER = 10
   ;MAX_GIT_REQUEST_SIZE = 33554432
   ;GIT_REQUEST_TIMEOUT = 2m
   ```

3. Check `[server] LOCAL_ROOT_URL`. The receive hook calls it for every push. It must be
   loopback, a Unix socket, an IP assigned to this host, or HTTPS with a valid
   certificate. The default derived from `HTTP_ADDR = 0.0.0.0` is `localhost` and works.
4. At the reverse proxy, preserve `Authorization`, never cache `/_branchroom/` or
   `/{owner}/{repository}/-/branchroom`, and allow request bodies up to
   `MAX_GIT_REQUEST_SIZE`. Provisioning copies a repository inside the web request, so
   allow a proxy timeout longer than your largest repository takes to copy.
5. Start Gitea and run `gitea doctor check --run branchroom`.

Route keys in clone URLs are sensitive. Redact `/_branchroom/` paths from proxy and
analytics logs where you can.

## Limits

| Setting | Default | Meaning |
| --- | --- | --- |
| `MAX_GIT_REQUEST_SIZE` | 32 MiB | Largest push or fetch negotiation body. Bodies are held in memory, so this bounds memory per concurrent request. Larger requests from authorised users get 413. |
| `GIT_REQUEST_TIMEOUT` | 2 minutes | Time to read a request body and to run receive-pack. |
| Upload-pack | 10 minutes | Fixed process limit for serving a clone or fetch. |
| `MAX_WORKSPACES_PER_OWNER` | 10 | Workspaces in provisioning, ready or failed state per scope owner. |
| Provisioning | 1 hour | A provisioning attempt older than this is abandoned and can be retried or purged. |

Pushes can only create or fast-forward `refs/heads/branchroom/*`. Incoming objects are
checked with `receive.fsckObjects`, which rejects legacy defects such as zero-padded
file modes that an ordinary Gitea push accepts. While Branchroom is enabled, the
ordinary repository refuses updates to `branchroom/*`. Use the restricted clone URL.
Existing branches with this prefix are kept.

Give contractors **Read only** on the ordinary repository, then grant discovery,
source read and source write in Branchroom. Restricted pushes need ordinary code
Read because the store is separate and only accepts `refs/heads/branchroom/*`.
Repository Write would also let them push embargoed commits to public branches.
Team grants still support discovery and source read only.

## Monitoring

- The `branchroom_integrity` cron task runs at start and every six hours. A failure
  becomes a system notice under Site administration. It verifies every audit chain,
  that each workspace has its store and a verified receive hook, and that no store
  lacks a workspace.
- `gitea doctor check --run branchroom` runs the same check on demand. `--fix` only
  reinstalls receive hooks. It never deletes a store or repairs an audit row, since
  either would destroy restricted source or evidence. A broken audit chain means rows
  were changed outside Gitea and should be investigated as an incident.

## Backup

`gitea dump` leaves the restricted root out unless you add `--branchroom-stores`. With
it, the archive holds restricted source, so keep it at least as protected as that source.

1. Stop Gitea, or take an atomic filesystem snapshot of the database and the restricted
   root together. A store and its database row must come from the same moment.
2. `gitea dump --branchroom-stores --database <type> --file gitea-dump.zip` writes the
   stores under `branchroom/` with their modes, and creates the archive as `0600`.
   Encrypt it before it leaves the host.
3. Back up `custom/conf/app.ini` separately. It holds secrets.

## Restore

1. Restore the dump as for any Gitea instance.
2. As the service account, move `branchroom/` from the unpacked dump to the new
   `RESTRICTED_ROOT`. `unzip` and `tar -xpf` keep its `0700` mode. Check it with
   `stat -c %a`, because Gitea will not serve stores from a root that others can read.
3. Point `RESTRICTED_ROOT` at it and start Gitea.
4. Run `gitea doctor check --run branchroom`. If the executable, config or restricted
   root moved, it reports receive hooks that fail verification. Run it again with
   `--fix`, then once more without, which must be clean.
5. Clone a restricted workspace through its handoff page to confirm.

`contrib/branchroom/restore-drill.sh` rehearses all of this on throwaway instances,
deleting the original, restoring to different paths and pushing again afterwards. Run it after upgrading Gitea
or changing the backup procedure.

## Upgrade

1. Back up as above.
2. Replace the binary at the same path and restart. Migrations run on start.
3. Run `gitea doctor check --run branchroom`. If the binary or config path changed, run
   it with `--fix`.

Branchroom tracks its own migrations in the `branchroom_version` table, apart from
upstream's numbering, so upstream releases can add migrations freely. Earlier Branchroom
builds numbered them inside upstream's sequence, first 352 to 358 and then 369 to 375.
On first start such a database is rewound to just before those numbers, upstream's
migrations run, and Branchroom's replay under their own tracking. The replay changes
nothing they already created.

## Incidents

Suspected disclosure of restricted source:

1. Revoke the affected grants on the Branchroom settings page. Revocation applies to
   the next request. One push already claimed may finish.
2. Rotate the clone URL. Old URLs stop working at once.
3. Export the audit evidence first, because the audit page is a Branchroom route too.
   To shut everything at once, set `[branchroom] ENABLED = false` and restart. Every
   Branchroom route returns 404 and claimed pushes are rejected in the hook.
4. Read the audit trail. Open the scope's **Audit evidence** page in the repository's
   **Settings > Branchroom**. Filter by actor, kind or UTC date range, check the chain
   status and select **Export evidence** to download JSON Lines. Exports contain
   the filtered events and decisions followed by a summary of the whole scope's
   chain verification. Each export is recorded in the audit trail. Decisions whose audit event is
   missing have sequence zero and empty hash fields in the export.
   Run the doctor check to confirm the chain is intact.
   Usernames are resolved when exporting. The recorded actor ID remains the identity
   in the hash chain.
   Permission decisions describe access at request time. Their `expires_unix` equals
   `created_unix` because the decision cannot be reused. Grant creation and renewal
   events carry `not_before_unix` and `expires_unix` in `details`. Grant creation,
   renewal and revocation also store the grantee username or team name at the time
   of the change. Later renames do not change this evidence.
   Receive outcome events distinguish `receive_accepted` from `receive_rejected`
   and include ref names and old/new object IDs. `receive_unconfirmed` means Git did not return a complete status report. It
   does not prove acceptance or rejection. Treat exports as restricted information.
5. Retire the workspace if it should no longer be used, then purge it when its source
   is no longer needed. Repository deletion keeps the audit chain.

Revocation cannot recall source that an authorised client already fetched.

## Production readiness

Verified for the supported deployment, with evidence in `review-2026-11.md`:

- Restricted objects are never served by public Git, web, API, dump or local import
  paths, and every gateway cell of the access matrix is asserted.
- Grant, revocation, rotation, retirement, purge and deletion work end to end in a
  browser and with real Git clients.
- Backup, restore to new paths and hook repair work in a drill, and an upgrade
  rehearsal on a copy of the live LAN instance migrated and served its real workspaces.
- The Branchroom suites pass on SQLite, PostgreSQL and MySQL. Both Docker images build
  and start with Branchroom enabled.

Not done, so not supported:

- An independent security review. Required before internet exposure.
- Restricted pull requests, reviews, CI and publication to the public repository.
- SSH and LFS for restricted workspaces.
- An audit viewer, an audit retention period and per-user rate limits.
- Environment materialization and key management.

## Grant lifetimes and offboarding

`[branchroom] DEFAULT_GRANT_DURATION` defaults to `2160h` (90 days). New user
and team grants without an expiry, including the scope creator's discovery grant,
expire after this duration. It must be positive. `MAX_GRANT_DURATION` defaults to
`0`, meaning no cap. Both settings use Go durations, for example `168h` for seven
days. Explicit expiries beyond the maximum, and non-expiring grants when a maximum
is set, are rejected. The default lifetime is capped at the maximum.

Repository owners can renew active grants from Branchroom settings. Choose the new
expiry in the renewal dialog. It starts at the default duration from now, capped at
the maximum. Renewal is audited.
Expiries within seven days are highlighted. Existing grants keep their expiry
until renewed or revoked.

Site administrators can inspect direct and team grants on the user edit page and
revoke all active direct grants. Each revocation is audited. Team access remains
until membership or the team's grant is removed. Prohibiting login blocks access
on the next request. Deleting an account also blocks access, while grant rows
remain as audit history.

Use **Remove person** to revoke all direct grants in this repository at once.
The checked collaborator option also removes their repository collaboration.
Both changes are audited. Team membership must be removed separately.

## Repository-limited tokens

A token limited to selected repositories can only access those repositories,
including their issues, pull requests and Git data. It cannot create, fork or
generate or transfer repositories, accept or reject transfers, use site administrator
powers or impersonate another user.
Account and organisation resources, including emails, keys, teams, runners,
variables, secrets and token lists, return `403 Forbidden`.

The API also permits `GET /user` for identity, `GET /version`, `GET /settings/*`,
the token owner's profile at `GET /users/{username}`, and repository and issue searches.
Other user profiles return `403 Forbidden`. OAuth userinfo omits organisation and
team memberships.
Search results include only allowed repositories. Account notification and activity
API endpoints are blocked. Repository notification endpoints and RSS/Atom feeds
respect the repository list. Container catalog listing is unavailable.

Commit references cannot change issues outside the repository list. Scheduled
auto-merges retain the scheduling token's restrictions and cannot run after it
expires or is revoked.

## Secret push protection

Push protection is off by default. Turn it on with:

```ini
[repository.secret_scanning]
PUSH_PROTECTION = true
```

A push is rejected when its new commits add a credential with a recognisable
format, such as an AWS, GitHub, GitLab, Slack, Stripe, Google, OpenAI, Anthropic
or npm key, or a private key. The rejection names the file, the commit and the
start of each match. A pusher who knows a match is not a secret can push again
with `git push -o secret_scanning.allow=true`. Each push that skips a match this
way leaves a notice under Site Administration, System Notices, with the pusher,
repository, branch, commit and the redacted matches. Set `ALLOW_BYPASS = false`
in the same section to refuse the option.

On the live host this is turned on. git-sync does not pass the override, so a
commit GitHub already accepted and this check rejects stays on GitHub. That
branch stops moving onto Branchroom until someone removes the credential or,
after looking at the commit, pushes it once with the option above. The sync
engine opens an issue in the affected repository when Branchroom refuses the
update. The issue does not include the hook message, which contains the start
of the match.

### Custom secret patterns

Site administrators can add named Go regular expressions in `app.ini`:

```ini
[repository.secret_scanning.patterns]
internal_token = \bcompany_[A-Za-z0-9]{32}\b
```

Restart after editing the configuration. Invalid regexes and patterns that match
empty text prevent startup, even when push protection is off. Go regexes run in
linear time and do not support backreferences or lookaround. Keep the list small
and test patterns against representative credentials before enabling them.

Custom patterns supplement the built-in vendor list. They scan added lines only,
with the same size limits and placeholder exclusions. Matches use the same
redaction and bypass policy as built-in matches, including admin notices.
Matches of eight characters or fewer are fully hidden.

## Agent tokens and push receipts

New personal access tokens start with `brpat_` followed by 40 hexadecimal
characters. Existing hex tokens still authenticate. Runner registration tokens
are unchanged. Secret push protection recognises the new format, including live
tokens issued by this server.

When creating a token in account settings or through the API, optionally supply
an agent name, an HTTP or HTTPS task URL and a sponsor. API clients send
`agent_identity` with `agent`, `task_url` and `sponsor`. These are declarations by
the token owner, not verified model or sponsor identities. Token permissions and
repository restrictions still apply.

The server records that identity when the token creates an issue, pull request,
comment or review. The web page shows a small attribution marker and API responses
include `agent_identity`. Revoking or regenerating the token does not erase earlier
attribution. Actions made with a password, SSH key or ordinary token have no agent
marker. Commit trailers do not supply this evidence.

Accepted Git pushes made with an agent token record the ref, old and new object
IDs, token ID, agent and task. The commit page and PR commits list show receipts
for pushed tips. Ordinary repository receipts are stored separately from the
Branchroom audit chain. Restricted workspace pushes include identity with their
existing receive outcome evidence. A rejected or unconfirmed restricted push is
not evidence that Git accepted the refs.

## PR factory labels

Import the **Branchroom** label template into each repository. The scoped
`pr/in-progress`, `pr/needs-review` and `pr/ready` labels are exclusive. Applying
one replaces the other labels in the `pr/` scope. The `land` label is independent.

An agent marks work `pr/in-progress` while changing code or fixing checks, then
`pr/needs-review` when it needs a person's decision. After review, mark it
`pr/ready`. A maintainer adds `land` when they intend to merge and enables
**merge when checks succeed** on the PR. Labels describe intent. They do not
schedule a merge or bypass checks, reviews, permissions or branch protection.

If new changes need review, move back to `pr/needs-review`, remove `land` and
cancel the scheduled merge. Agents should read readiness again after a new head
commit and before asking to land. When using MCP `set_labels`, preserve unrelated
label names because it replaces the full label set.

## Path reviewer rules

Repository administrators can require approvals for selected paths on a protected
branch. Create the branch protection first, then use
`PUT /api/v1/repos/{owner}/{repo}/branch_protections/path_reviewers/{name}`:

```json
{"rules":[{"pattern":"src/**","team":"security","required_approvals":2}]}
```

Use the protected branch rule's name, including any wildcard. Patterns use the
same path globs as protected files, with `/` separating directories. All matching
rules apply. The named team must belong to the repository's organisation and have
code write access. Membership is checked again when merging.

To require the applicable CODEOWNERS owners instead, replace `team` with
`"code_owners":true`. The target branch's CODEOWNERS file determines eligible
reviewers. Each applicable ownership rule needs the specified number of distinct
approvals. Missing owners, invalid ownership rules or a matching timeout block
merging. The PR author cannot count towards either kind of rule.

Only each reviewer's latest non-dismissed approval counts. Stale approvals follow
the branch's existing stale-review policy. These rules apply to ordinary and
automatic merges and appear as `path_reviews_missing` in readiness. Existing
administrator and bypass permissions still apply.

`GET` on the same endpoint lists the rules. `PUT` replaces the complete list and
`{"rules":[]}` clears it. Invalid replacements leave the existing rules intact.
Rules live in `branchroom_path_reviewer`, created by Branchroom migration 13.
