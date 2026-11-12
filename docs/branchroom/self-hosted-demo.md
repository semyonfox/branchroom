# Self-hosted demo

This guide exercises the Branchroom behaviour intended for a local demo today:
repository-owner policy scope, discovery and source grant management, clone-locator
rotation, and restricted HTTPS clone and push. This is not a full restricted Git
workspace or real secret-management system.

## Before you start

Use a development instance, a throwaway repository, and two throwaway accounts. Do
not use source or credentials that matter. Keep `[branchroom.environment]` disabled.

Create a separate existing directory for restricted stores. It must be a real
directory, not a symlink, with no group or other access, normally mode `0700`. On
Unix, the root must be owned by the Gitea service account. Its ancestors must be owned
by that account or root. Do not place it below another local user's directory. It must
not be the public repository root, live inside it, or contain it. For example:

```sh
install -d -m 0700 -o gitea -g gitea /srv/gitea-branchroom-repositories
```

Restricted-store transport requires these Unix ownership checks and is unavailable on
non-Unix hosts.

```ini
[branchroom]
ENABLED = true
RESTRICTED_ROOT = /srv/gitea-branchroom-repositories

[branchroom.environment]
ENABLED = false
```

The server refuses to start if the configured restricted root overlaps
`[repository] ROOT`, including through a symlink. It also leaves every Branchroom
route hidden when `ENABLED` is false. `[repository] DISABLE_HTTP_GIT = true` also
hides the handoff result and the restricted Git HTTP gateway.

Restricted stores must not overlap public repositories, local storage, or the served
`public`, `templates` and `options` directories under `CustomPath` and
`StaticRootPath`. A root under the work path, such as `data/branchroom`, is fine. Local
imports cannot read from the restricted root. `gitea dump` excludes restricted stores,
including through a symlinked data path, so they need a separate backup. Branchroom
refuses to start on MSSQL and under FastCGI (`PROTOCOL = fcgi` or `fcgi+unix`), because
the gateway needs row locks and connection read deadlines.

Server-side hooks and runners on the same host can read restricted stores as the service
account. The installed receive hook records the executable, configuration and restricted
root paths. Moving any of them stops pushes, and there is no command to reinstall the
hooks yet.

Pushes run with `receive.fsckObjects`, which is stricter than an ordinary Gitea push.
Incoming objects with legacy defects such as zero-padded file modes, a missing email or
a bad timezone are rejected. History already in the store is not rechecked.

Build and start Gitea using the normal local development instructions in
[`docs/development.md`](../development.md). The environment foundation is not part
of this walkthrough. Do not add a development key file or turn it on.

Use HTTPS if this instance is reachable over a network. A reverse proxy must preserve
the `Authorization` header and must never cache `/_branchroom/` or
`/{owner}/{repository}/-/branchroom`. Treat route keys as sensitive operational
metadata and redact them from optional proxy or analytics logs where practical.
Set `[server] ROOT_URL` to the public HTTPS address, or forward the public Host and
`X-Forwarded-Proto` correctly, so the handoff page generates a usable clone URL.

The receive hook calls `[server] LOCAL_ROOT_URL` to check each push. An HTTP callback
must use a Unix socket, loopback, or a literal IP assigned to the same server. A
callback to another host requires HTTPS with a valid certificate. A server bound
to its own LAN IP can use that IP for the internal callback without adding a
loopback listener. Hostnames other than `localhost` require HTTPS.

## Walkthrough

1. Sign in as the actual owner of a throwaway repository.
2. Open **Repository settings → Branchroom**.
3. Create a scope such as `security-fix-demo`.
4. Optionally provision its isolated workspace. Branchroom copies the repository's
   current default-branch commit into a separate bare store and shows `ready` plus
   the pinned public-base SHA.
5. Give the second throwaway account ordinary Gitea code-read and code-write
   permission. For a private repository, add it as a write collaborator before
   creating Branchroom grants.
6. Add a direct discovery grant for the second throwaway account.
7. Once the workspace is `ready`, give that account source-read access. Use an
   individual `read_source` grant or an eligible organization team. The account needs `discover`
   and source-read access to retrieve the clone URL.
8. Add a direct `write_source` grant for that account. A push needs source-read
   access, that direct write grant, and ordinary Gitea code-write permission.
9. Refresh the page and confirm that the scope, workspace state, and all grants are
   shown.
10. Sign in as the second account and open
    `/{owner}/{repository}/-/branchroom`. It shows the scope name and clone URL for
    that account only. Copy it and clone with the account password, or a personal
    access token that has `write:repository` if the account has 2FA enabled.
11. In the clone, create a test branch and push a harmless file:

    ```bash
    git switch -c branchroom/demo
    printf 'DEMO_VALUE=not-a-secret\n' > demo.env
    git add demo.env
    git commit -m 'test restricted push'
    git push origin HEAD:refs/heads/branchroom/demo
    ```

    The push may be at most 1 MiB decoded and must finish within 45 seconds. Gzip
    request bodies are supported. Reading an RPC body has a 45 second deadline.
    Upload-pack has a separate 10 minute process limit. It can only
    create or fast-forward `refs/heads/branchroom/*`; tags, deletes,
    non-fast-forward updates, SSH, and LFS are rejected.
12. Return to the owner's Branchroom settings and rotate the clone locator for the
    ready workspace. It is a hard cutover: the copied URL has no redirect, alias, or
    grace period.
13. Sign in again as the second account and refresh the handoff page to get the new
    clone URL. A fresh fetch with the old one fails. The new one appears only while
    ordinary code-read permission and the discovery grant and source-read access
    remain active.
14. Revoke the direct `write_source` grant, make another commit, and retry the push.
    The new push fails. Revoke source-read access to block new fetches and new receive
    sessions too. A receive session already claimed by the server may finish.

Provisioning copies the public base, so a normal local demo clone may contain only
that public-base history. It also creates and verifies the server-owned static
pre-receive hook before the workspace becomes ready. If installation fails, the
workspace stays failed safely. Use only dummy data in this demo, including in
`demo.env`.

The repository owner receives an explicit discovery grant when creating the scope.
Site administrators are trusted operators who can use upstream impersonation, password
reset and membership tools. Direct `write_source` grants apply
only to active individual users and ready workspaces. Organization teams can provide
source-read access, but never source-write access. Bots, deploy keys, and Actions
tasks cannot use this transport. OAuth2 JWTs in either Basic credential position are
refused. Use password Basic or a personal access token. Discovery and source-read grants
may each come from a direct grant or an eligible team.

Rotation does not revoke grants. A Git protocol phase that was already authorized when
the rotation commits may finish, but a fresh request using the old clone URL fails. The
audit record says that the locator changed without storing either route key, a clone
URL, a filesystem path, or a store ID.

## What is deliberately missing

- No SSH, LFS, API, or CLI endpoint. The only transports are the restricted smart
  HTTP upload-pack and receive-pack paths. The clone URL appears only on the signed-in
  handoff page and is not linked from ordinary repository pages.
- No general write access. The receive path is limited to active individual users who
  have ordinary code-write permission, active source-read access, and a direct
  `write_source` grant. It accepts only protected Branchroom refs and cannot create
  tags, delete refs, or force a non-fast-forward update.
- No restricted pull-request or publication workflow. Ordinary pull requests merge
  public repository branches; restricted work is not promoted into them.
- No real secret storage or release. The development-only encryption code is sealed
  internal plumbing and refuses production startup. Environment materialization and
  production KMS stay deferred and disabled, with no selected production provider.
- No push-time secret scanner. The current preflight module is not wired into pushes,
  pull requests, imports, or publication.

Provisioning copies the repository synchronously. `MAX_WORKSPACES_PER_OWNER` limits
active workspace counts, but there is no storage-size quota.
Treat it as a single-operator, synthetic-data demo. It is not suitable for a shared
instance or a beta with real restricted source.

The restricted Git HTTP gateway accepts only GET/HEAD
`info/refs?service=git-upload-pack` or `git-receive-pack`, and POST `git-upload-pack`
or `git-receive-pack`. It requires Basic authentication and rechecks authorization for
each advertisement and RPC request. It has no archive, dumb-object, SSH, LFS, API,
CLI, or runner route. The handoff page is not the security boundary. Production KMS
and secret-bearing runner work stay deferred and disabled, with no production provider
selected.
