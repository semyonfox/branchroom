# Self-hosted demo

This guide exercises the Branchroom behaviour intended for a local demo today:
repository-owner policy scope, discovery-grant, source-grant management, and a narrow
read-only Git clone handoff. This is not a usable restricted Git workspace or real
secret-management system.

## Before you start

Use a development instance, a throwaway repository, and two throwaway accounts. Do
not use source or credentials that matter. Keep `[branchroom.environment]` disabled.

Create a separate existing directory for restricted stores. It must not be the public
repository root, live inside it, or contain it. For example:

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

Build and start Gitea using the normal local development instructions in
[`docs/development.md`](../development.md). The environment foundation is not part
of this walkthrough. Do not add a development key file or turn it on.

Use HTTPS if this instance is reachable over a network. A reverse proxy must preserve
the `Authorization` header and must never cache `/_branchroom/` or
`/{owner}/{repository}/-/branchroom`. Treat route keys as sensitive operational
metadata and redact them from optional proxy or analytics logs where practical.
Set `[server] ROOT_URL` to the public HTTPS address, or forward the public Host and
`X-Forwarded-Proto` correctly, so the handoff page generates a usable clone URL.

## Walkthrough

1. Sign in as the actual owner of a throwaway repository.
2. Open **Repository settings → Branchroom**.
3. Create a scope such as `security-fix-demo`.
4. Optionally provision its isolated workspace. Branchroom copies the repository's
   current default-branch commit into a separate bare store and shows `ready` plus
   the pinned public-base SHA.
5. Give the second throwaway account ordinary Gitea code-read permission. For a
   private repository, add it as a collaborator before creating Branchroom grants.
6. Add a direct discovery grant for the second throwaway account.
7. Once the workspace is `ready`, add a direct `read_source` grant for that same
   account. The second account now needs both `discover` and `read_source` grants.
8. Refresh the page and confirm that the scope, workspace state, and both grants are
   shown.
9. Sign in as the second account and open
   `/{owner}/{repository}/-/branchroom`. It shows the scope name and a read-only clone
   URL for that account only. Copy it and clone with the account password, or a
   `read:repository` personal access token if the account has 2FA enabled.
10. Revoke either grant. Refreshing the handoff page returns 404, and a new fetch using
   the copied URL fails.

Provisioning copies the public base, so a normal local demo clone may contain only
that public-base history. Do not inject private commits into the restricted store just
to make the demo more dramatic. The feature still lacks a safe user-facing write path.

The repository owner receives an explicit discovery grant when creating the scope.
A site administrator is not a Branchroom bypass. Teams, bots, deploy keys, Actions
tasks, and write grants are not available through this page. Source-read grants are
limited to active individual users and a ready repository workspace. They are one
required check for the experimental read-only gateway, not a general-purpose access
role.

## What is deliberately missing

- No push, SSH, LFS, API, or CLI endpoint. The only transport route is an experimental,
  read-only smart HTTP upload-pack path. Its opaque clone URL appears only on the
  signed-in handoff page after both direct grants, and it is not linked from ordinary
  repository pages.
- Provisioned isolated storage has no retry or deletion control. It is not a usable
  collaboration workspace.
- No real secret storage or release. The development-only encryption code is sealed
  internal plumbing and refuses production startup.
- No push-time secret scanner. The current preflight module is not wired into pushes,
  pull requests, imports, or publication.

Provisioning copies the repository synchronously and has no quota or per-owner limit.
Treat it as a single-operator, synthetic-data demo. It is not suitable for a shared
instance or a beta with real restricted source.

The internal source-policy resolver now has one narrow Git HTTP caller. It accepts
only GET/HEAD `info/refs?service=git-upload-pack` and POST `git-upload-pack`, requires
Basic authentication, and rechecks authorization before both protocol phases. It has
no receive-pack, archive, dumb-object, SSH, LFS, API, CLI, or runner surface. The
handoff page is not the security boundary. Git checks the current grant again for each
advertisement and RPC request. The next work is locator rotation and the remaining
transport and lifecycle boundaries, before any real KMS or secret-bearing runner flow.
