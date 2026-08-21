# Self-hosted demo

This guide exercises the only Branchroom behaviour intended for a local demo today:
repository-owner policy scope and discovery-grant management. It does not create a
restricted Git workspace or manage real secrets.

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
route hidden when `ENABLED` is false.

Build and start Gitea using the normal local development instructions in
[`docs/development.md`](../development.md). The environment foundation is not part
of this walkthrough. Do not add a development key file or turn it on.

## Walkthrough

1. Sign in as the actual owner of a throwaway repository.
2. Open **Repository settings → Branchroom**.
3. Create a scope such as `security-fix-demo`.
4. Optionally provision its isolated workspace. Branchroom copies the repository's
   current default-branch commit into a separate bare store and shows `ready` plus
   the pinned public-base SHA.
5. Add a direct discovery grant for the second throwaway account.
6. Refresh the page and confirm that the scope, workspace state, and grant are shown.
7. Revoke the grant and confirm it no longer appears.
8. Sign in as the second account. It has no Branchroom UI or Git access from this
   flow. The current demo proves grant state and revocation, not source access.

The repository owner receives an explicit discovery grant when creating the scope.
A site administrator is not a Branchroom bypass. Teams, bots, deploy keys, Actions
tasks, source grants, write grants, workspaces, and environment materialization are
not available through this page.

## What is deliberately missing

- No restricted Git clone, fetch, push, HTTP, SSH, LFS, API, or CLI endpoint.
- Provisioned isolated storage has no clone URL, Git transport, retry, or deletion
  control. It is a storage proof, not a usable workspace.
- No real secret storage or release. The development-only encryption code is sealed
  internal plumbing and refuses production startup.
- No push-time secret scanner. The current preflight module is not wired into pushes,
  pull requests, imports, or publication.

Provisioning copies the repository synchronously and has no quota or per-owner limit.
Treat it as a single-operator, synthetic-data demo. It is not suitable for a shared
instance or a beta with real restricted source.

The next useful work is a read-only source-policy resolver and then the Git transport
authorization boundary. That comes before a real KMS or secret-bearing runner flow.
