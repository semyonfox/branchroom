# Branchroom

Branchroom is an independent, MIT-licensed fork of
[Gitea](https://about.gitea.com/). It explores normal Git collaboration alongside
isolated workspaces for embargoed source changes and controlled environment
materialization.

This fork is maintained separately and developed here first. If the design proves
useful, it may later be discussed with Gitea upstream; there is no upstream proposal
or commitment today.

## Status

Branchroom has completed M0a, the M0b.1 storage proof, M0b.2 public-route regression,
the first M1 policy foundation, the M4a–M4f isolated-workspace storage,
source-policy, read-only Git HTTP, and authorized clone-handoff slices, and
the M2a–M2c development-only environment foundations. It adds a disabled-by-default
restricted-store root, a path-safe repository facade, and a public-base import that
forbids local cloning and hard links. The regression checks that ordinary public Git
HTTP and SSH resolution do not select an unregistered restricted store.

When `[branchroom] ENABLED = true`, real repository owners can create policy scopes
and manage direct-user discovery grants at repository settings → Branchroom. Once an
isolated workspace is ready, they can also prepare direct-user `read_source` grants.
That page stores safe scope, grant, decision, and hash-linked audit metadata; it is
hidden and returns 404 while the feature is disabled. It accepts active individual
accounts only—never a team, bot, deploy key, Actions task, write grant, or caller-
supplied workspace identity.
Scope discovery combines ordinary Gitea code-read permission with an explicit
Branchroom grant. A Gitea site administrator is not a Branchroom policy bypass.

A server-internal workspace creator can reserve one opaque UUID mapping per active
scope, copy a pinned public base into a separate bare store, verify the copy, and mark
the mapping ready only after an audit event commits. Its database record contains no
physical path or clone URL. Failed imports stay non-routable. A repository transfer
retires its scopes, grants, workspace mappings, environment revisions, and unclaimed
leases. Repository deletion is blocked while a restricted-store mapping or encrypted
environment revision exists, pending an explicit retention collector.

The owner-only Branchroom settings page can now provision that isolated store from a
repository's current default-branch commit and show its safe state and pinned base.
It does not render a store ID, filesystem path, workspace UUID, or clone URL. A
provisioned store stays non-routable until a direct user receives a `read_source`
grant.

There is now a deliberately narrow, read-only smart Git HTTP gateway for a ready
workspace. It accepts only upload-pack advertisement and upload-pack RPC at a route
key URL. An authorized direct user can retrieve that URL from the stable
`/{owner}/{repository}/-/branchroom` page. The page requires ordinary Gitea code-read
permission plus direct `discover` and `read_source` grants, and returns 404 to everyone
else. It does not write a policy decision or audit event just because someone refreshes
the page. The Git gateway reauthorizes and audits the actual protocol requests.

Each Git request requires Basic authentication from an active direct user, ordinary
Gitea code-read permission, and an explicit direct-user `read_source` grant. A personal
access token must include `read:repository`; public-only tokens, browser sessions,
bearer/OAuth credentials, Actions, bots, deploy keys, push, archive, dumb HTTP, SSH,
LFS, API, and CLI paths are not accepted. Authorization is repeated for advertisement
and RPC, and disabled or denied requests stay non-disclosing.

The development-only environment foundation is also disabled by default. It uses a
separate `[branchroom.environment]` setting, refuses production startup, reads a
base64 key from a local file, and stores AES-256-GCM ciphertext plus wrapped per-
revision data keys in the database. It has no route, API, CLI, Git, Actions, or direct
storage endpoint. A direct active user can receive a short-lived, one-use lease only
through the internal system surface; normal repository code-read permission and an
explicit materialize grant are checked again immediately before its one-time claim.
That claim is the irreversible authorization boundary: later revocation cannot recall
a plaintext release already in progress. Plaintext, raw lease secrets, filesystem
paths, and key-file locations are not stored in Branchroom records or audit events.

There is also an inert public-content preflight module. It reserves `.branchroom`
for safe environment-reference files and detects a small set of high-confidence
plaintext credential shapes without returning matched bytes. It is not wired into
pushes, pull requests, imports, or publication yet, and it is not a full secret
scanner.

This is still not a full restricted Git workspace. The experimental HTTP read path has
a tightly gated handoff page, but no push, SSH, LFS, public Branchroom API, production
KMS, tracked-reference integration, materialization CLI, Actions integration, pull
requests, or retention collector. The settings page explicitly labels scopes as policy
metadata so it cannot be mistaken for a protected-branch control.

The fork-local source of truth is the
[implementation plan](docs/branchroom/implementation-plan.md). It explains the
scope, threat model, Gitea integration points, milestones, and security gates. The
separately maintained HTML copy is not built or published by this repository.

## Self-hosted demo today

This is a policy demo, not a confidential-source or secret-management demo. Run it
only with synthetic repositories and throwaway accounts.

Enable `[branchroom]` in your local `app.ini` and point `RESTRICTED_ROOT` at an
existing directory outside `[repository] ROOT`. Leave `[branchroom.environment]`
disabled. As a repository owner, open repository settings → Branchroom, create a
scope, grant discovery to a second test account, then revoke it. That shows the
current owner, grant, audit, and revocation behaviour while ordinary Gitea remains
unchanged. After provisioning a ready synthetic workspace, you may also add and
revoke a direct `read_source` grant. Give the same account the direct discovery grant,
then have it open `/{owner}/{repository}/-/branchroom` while signed in. That page shows
only its authorized clone URL. The settings page does not reveal or distribute one.

Do not put real secrets in Branchroom or create a workspace for real restricted
source. You may provision an isolated store with synthetic source to demonstrate the
storage boundary. The only transport feature is a narrow, read-only smart HTTP clone
path disclosed after both direct grants. It is not a beta or a full collaboration
workflow. There is no Branchroom push, SSH, LFS, API, CLI, Actions, or materialization
endpoint. The detailed local walkthrough lives in [the self-hosted demo guide](docs/branchroom/self-hosted-demo.md).

## Intended boundary

The plan keeps public and restricted source in separate Git object stores. It does
not try to hide selected files inside an ordinary clone. Environment values stay
outside Git as encrypted revisions that an authorized person or job can materialize
for a limited time.

The first valuable flow is a private security fix from restricted workspace, through
scoped CI and release approval, to a deliberate public patch. It is a design goal,
not a shipped feature.

## Development

Branchroom keeps Gitea's local development workflow:

- [Setup and requirements](docs/build-setup.md)
- [Development, formatting, and API changes](docs/development.md)
- [Testing](docs/testing.md)

Useful commands:

```bash
make build
make fmt
make lint-md
```

Run `make help` to list all development targets.

## Fork development

Keep active implementation work in this fork. The upstream Gitea contributor guide
is useful for code, test, and review conventions. If this earns outside interest,
start with an upstream design discussion rather than a large implementation proposal.

Read the [implementation plan](docs/branchroom/implementation-plan.md) before
starting a Branchroom feature. Start with the isolated-store proof in M0 rather than
trying to add restricted refs to a normal Gitea repository.

## Licence

Branchroom inherits Gitea code under the MIT License. See
[LICENSE](LICENSE).
