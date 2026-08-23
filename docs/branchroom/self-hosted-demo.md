# Self-hosted demo

This guide exercises the Branchroom behaviour intended for a local demo today:
repository-owner policy scope, discovery-grant, source-grant management, clone-locator
rotation, and a narrow read-only Git clone handoff. This is not a usable restricted Git
workspace or real secret-management system.

## Before you start

Use a development instance, a throwaway repository, and two throwaway accounts. Do
not use source or credentials that matter. Keep `[branchroom.environment]` disabled.

Create a separate existing directory for restricted stores. It must be a real
directory, not a symlink, and it must not be group or world writable. It must not be
the public repository root, live inside it, or contain it. For example:

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
   account. The second account now needs both `discover` and `read_source` grants,
   and a clone requires both.
8. Refresh the page and confirm that the scope, workspace state, and both grants are
   shown.
9. Sign in as the second account and open
   `/{owner}/{repository}/-/branchroom`. It shows the scope name and a read-only clone
   URL for that account only. Copy it and clone with the account password, or a
   `read:repository` personal access token if the account has 2FA enabled.
10. Return to the owner's Branchroom settings and rotate the clone locator for the
    ready workspace. It is a hard cutover: the copied URL has no redirect, alias, or
    grace period.
11. Sign in again as the second account and refresh the handoff page to get the new
    clone URL. A fresh fetch with the old one fails. The new one appears only while
    ordinary code-read permission and both direct grants remain active.
12. Revoke either grant. Refreshing the handoff page returns 404, and a new fetch using
    the copied URL fails.

Provisioning copies the public base, so a normal local demo clone may contain only
that public-base history. It also creates and verifies the server-owned static
pre-receive hook before the workspace becomes ready; if installation fails, the
workspace stays failed safely, and older ready workspaces were not retrofitted.
Do not inject private commits into the restricted store just
to make the demo more dramatic. The feature still lacks a safe user-facing write path.

The repository owner receives an explicit discovery grant when creating the scope.
A site administrator is not a Branchroom bypass. Teams, bots, deploy keys, and
Actions tasks are not available through this page. The owner can store and revoke a
direct `write_source` grant for a ready workspace, but such grants are stored-only
and inert and do not enable a push. Source-read grants are
limited to active individual users and a ready repository workspace. They are one
required check for the experimental read-only gateway, not a general-purpose access
role.

Rotation does not revoke grants. A Git protocol phase that was already authorized when
the rotation commits may finish, but a fresh request using the old clone URL fails. The
audit record says that the locator changed without storing either route key, a clone
URL, a filesystem path, or a store ID.

## What is deliberately missing

- No push, SSH, LFS, API, or CLI endpoint. The only transport route is an experimental,
  read-only smart HTTP upload-pack path. Its opaque clone URL appears only on the
  signed-in handoff page after both direct grants, and it is not linked from ordinary
  repository pages.
- `write_source` stays deny-only. The owner can store and revoke audited
  direct-active-user `write_source` grants for a ready workspace, but those grants
  are stored-only and inert: evaluation checks ordinary code-read and code-write
  permission and then always returns `write_source_unavailable` before a grant can
  match. The migration-backed receive-session model, bounded internal verifier,
  hidden pre-receive command, and static server-owned `core.hooksPath` hook exist
  but all deny. Migration 355 lets the internal verifier complete only a fully bound,
  correctly bearer-authenticated claimed session as denied with the unchanged static
  policy denial; replay adds no second audit, and nominal expiry after a valid claim
  does not block that completion. There is no production issuer, claimant route,
  user-facing session API, receive-pack route, SSH route, or allow path, so nothing
  here enables a push.
- Provisioned isolated storage has no retry or deletion control. It is not a usable
  collaboration workspace.
- No real secret storage or release. The development-only encryption code is sealed
  internal plumbing and refuses production startup. Environment materialization and
  production KMS stay deferred and disabled, with no selected production provider.
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
advertisement and RPC request. The next work is the remaining transport and lifecycle
boundaries. Production KMS and secret-bearing runner work stay deferred and disabled,
with no production provider selected.
