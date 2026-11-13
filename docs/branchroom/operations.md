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

`gitea dump` deliberately leaves the restricted root out. Back it up separately and
keep that archive at least as protected as the source it holds.

1. Stop Gitea, or take an atomic filesystem snapshot of the database and the restricted
   root together. A store and its database row must come from the same moment.
2. `gitea dump --database <type> --file gitea-dump.zip`
3. `tar -C <parent of RESTRICTED_ROOT> -cpf branchroom-restricted.tar <root name>`
   keeps ownership and modes. Encrypt it before it leaves the host.
4. Back up `custom/conf/app.ini` separately. It holds secrets.

## Restore

1. Restore the dump as for any Gitea instance, then unpack the restricted archive with
   `tar -xpf` as the service account.
2. Point `RESTRICTED_ROOT` at the unpacked directory and start Gitea.
3. Run `gitea doctor check --run branchroom`. If the executable, config or restricted
   root moved, it reports receive hooks that fail verification. Run it again with
   `--fix`, then once more without, which must be clean.
4. Clone a restricted workspace through its handoff page to confirm.

`contrib/branchroom/restore-drill.sh` rehearses all of this on throwaway instances,
restoring to different paths and pushing again afterwards. Run it after upgrading Gitea
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
generate repositories, use site administrator powers or impersonate another user.
Account and organisation resources, including emails, keys, teams, runners,
variables, secrets and token lists, return `403 Forbidden`.

The API also permits `GET /user` for identity, `GET /version`, `GET /settings/*`,
public user profiles at `GET /users/{username}`, and repository and issue searches.
Search results include only allowed repositories. Account notification and activity
API endpoints are blocked. Repository notification endpoints and RSS/Atom feeds
respect the repository list. Container catalog listing is unavailable.

Commit references cannot change issues outside the repository list. Scheduled
auto-merges retain the scheduling token's restrictions and cannot run after it
expires or is revoked.
