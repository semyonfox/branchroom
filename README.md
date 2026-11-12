# Branchroom

Branchroom is an independent, MIT-licensed fork of
[Gitea](https://about.gitea.com/). It explores normal Git collaboration alongside
isolated workspaces for embargoed source changes.

Branchroom is maintained on this fork only, and there is no plan to submit its
changes to Gitea upstream. A read-only Gitea remote may be kept here for maintenance
reference only.

## Status

Branchroom has completed M0a, the M0b.1 storage proof, M0b.2 public-route regression,
the first M1 policy foundation, the M4a–M4h.2a isolated-workspace storage,
source-policy, restricted Git HTTP read/write, authorized clone-handoff, clone-locator
rotation and sealed receive sessions. The M2a–M2c development-only environment
foundations were removed in November 2026 because nothing called them. It adds a disabled-by-default
restricted-store root, a path-safe repository facade, and a public-base import that
forbids local cloning and hard links. The regression checks that ordinary public Git
HTTP and SSH resolution do not select an unregistered restricted store.

When `[branchroom] ENABLED = true`, real repository owners can create policy scopes
and manage direct-user discovery grants at repository settings → Branchroom. Once an
isolated workspace is ready, they can also grant source-read access.
That page stores safe scope, grant, decision, and hash-linked audit metadata; it is
hidden and returns 404 while the feature is disabled. It accepts active individual
accounts and eligible organisation teams. Team grants support discovery and source read,
while source write requires a direct individual grant. Grants can have start and expiry
times. Bots, deploy keys and Actions tasks have no settings controls.
Scope discovery combines ordinary Gitea code-read permission with an explicit
Branchroom grant. Site administrators are trusted operators. Branchroom grants still
apply to their own requests, but upstream administrator tools can impersonate users,
mint PATs or change membership.

A server-internal workspace creator can reserve one opaque UUID mapping per active
scope, copy a pinned public base into a separate bare store, verify the copy, and mark
the mapping ready only after an audit event commits. Its database record contains no
physical path or clone URL. Failed imports stay non-routable. A repository transfer
retires its scopes, grants and workspace mappings. Repository deletion requires every
restricted store to be purged first. Retired mappings retain their stores until the
current owner purges them, including after transfer. Deletion then retires the scopes
and keeps scopes, grants, decisions and the audit chain, so audit history outlives the
repository. Failed imports and provisioning abandoned for over one hour can be retried or
purged.

The owner-only Branchroom settings page can now provision that isolated store from a
repository's current default-branch commit and show its safe state and pinned base.
Provisioning creates and verifies the server-owned static pre-receive hook before the
workspace becomes ready; older ready workspaces were not retrofitted. A hook
installation failure leaves the workspace failed safely. It
does not render a store ID, filesystem path, workspace UUID, or clone URL. A
provisioned store stays non-routable until a user has source-read access.

For a ready workspace, the repository owner can rotate its opaque clone locator from
the same settings page. Rotation is a hard cutover. Once it commits, the old clone URL
has no alias, redirect, or grace period, and a fresh Git request using it fails. A Git
phase that was already authorized when the rotation commits may finish. Rotation does
not bypass ordinary Gitea code-read permission or active grants: Git still requires
`read_source`, and the handoff page still requires both `discover` and `read_source`.
The audit record names the rotation without either route key, a URL, a filesystem path,
or a store ID.

M4h.2a provides a restricted HTTPS receive path for a ready workspace. The owner can
create and revoke audited direct-user `write_source` grants from the settings page. A
push also needs ordinary Gitea code-read and code-write permission plus active
source-read access, which may come from a direct grant or an eligible organization
team. The server issues a one-use, hash-only receive session and passes its bearer
only to the receive-pack child. The server-owned pre-receive hook verifies the trusted
quarantine, refs and ancestry before Git accepts a ref. Receive-pack checks incoming
object integrity and connectivity before running that hook.

The restricted Git HTTP gateway accepts upload-pack and receive-pack advertisement
and RPC at a route-key URL. An authorized direct user can retrieve that URL from the
stable `/{owner}/{repository}/-/branchroom` page. The page requires ordinary Gitea
code-read permission plus `discover` from a direct or eligible team grant and
source-read access, and returns 404 to everyone else. It does not write a policy decision or audit event just because someone refreshes
the page. The Git gateway reauthorizes and audits the actual protocol requests.

Read requests require Basic authentication from an active individual, ordinary Gitea
code-read permission, and active source-read access. A read PAT needs
`read:repository`. Receive requests additionally require ordinary code-write
permission, a direct `write_source` grant, and a `write:repository` scoped PAT when a
PAT is used. Pushes can only create or fast-forward `refs/heads/branchroom/*`; tags,
deletes, non-fast-forward updates, SSH, and LFS fail. The receive request limit is
1 MiB decoded, an authorized request over it gets 413, and the process limit is 45 seconds. Both RPCs accept gzip bodies and
allow 45 seconds to read the request. Upload-pack has a separate 10 minute process
limit. OAuth2 JWTs in either Basic credential position and Actions tokens are refused.
Password Basic and personal access tokens are the only accepted gateway credentials.
Authorisation is repeated for advertisement and RPC. Revocation blocks new receive
sessions, while one already claimed by the server may finish. Disabling Branchroom rejects a claimed session in the hook.
Disabled or denied requests stay non-disclosing.

This is still not a full restricted Git workspace. The experimental HTTPS path has a
tightly gated handoff page and protected push, but no SSH, LFS, public Branchroom API,
environment materialization, Actions integration or pull requests. The
settings page explicitly labels scopes as policy metadata so it cannot be mistaken for
a protected-branch control.

The fork-local source of truth is the
[implementation plan](docs/branchroom/implementation-plan.md). It explains the
scope, threat model, Gitea integration points, milestones, and security gates. The
standalone HTML copy is a separately maintained shareable document that this
repository neither generates nor deploys.

Restricted stores must not overlap public repositories, local storage, or the served
asset, template and option directories. Local imports cannot read from the restricted
root. `gitea dump` excludes restricted stores, which need a separate backup. Branchroom
refuses to start on MSSQL, under FastCGI, or with `[security] DISABLE_GIT_HOOKS = false`.

Runners on the same host can read restricted stores as the service account. Run
`gitea doctor check --run branchroom` to verify every audit chain, find mappings without
a store and stores without a mapping, and check receive hooks. Add `--fix` after moving
the executable, configuration or restricted root to reinstall the hooks.

## Self-hosted demo today

This is a policy demo, not a confidential-source or secret-management demo. Run it
only with synthetic repositories and throwaway accounts.

Enable `[branchroom]` in your local `app.ini` and point `RESTRICTED_ROOT` at an
existing real directory outside `[repository] ROOT`, owned by the Gitea service account.
It must not be a symlink and must deny group and other access, normally with mode `0700`, and must sit below
directories controlled by the Gitea service account or root. Restricted-store
transport requires Unix ownership checks. As a repository
owner, open repository settings → Branchroom, create a scope, grant discovery to a
second test account, then revoke it. That shows the current owner, grant, audit, and
revocation behaviour while ordinary Gitea remains unchanged. After provisioning a
ready synthetic workspace, give the test account ordinary code-write permission,
source-read access, and a direct `write_source` grant. Give it a direct discovery
grant as well so it can retrieve the clone URL from
`/{owner}/{repository}/-/branchroom` while signed in. Push only a dummy file to
`refs/heads/branchroom/<name>`. The settings page does not reveal or distribute the
URL.

Do not put real secrets in Branchroom or create a workspace for real restricted
source. You may provision an isolated store with synthetic source to demonstrate the
storage boundary. The only transport feature is restricted smart HTTP clone and push,
with pushes confined to Branchroom refs. It is not a beta or a full collaboration
workflow. There is no SSH, LFS, API, CLI or Actions endpoint. The
detailed local walkthrough lives in [the self-hosted demo guide](docs/branchroom/self-hosted-demo.md).

## Intended boundary

The plan keeps public and restricted source in separate Git object stores. It does
not try to hide selected files inside an ordinary clone. Environment values are meant
to stay outside Git as encrypted revisions that an authorized person or job can
materialize for a limited time. That needs its own key-management design and is not
implemented.

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

Keep active implementation work in this fork. Branchroom changes are not submitted
to Gitea upstream. The upstream Gitea contributor guide remains useful for code,
test, and review conventions, and a read-only Gitea remote may be used as a
maintenance reference only.

Read the [implementation plan](docs/branchroom/implementation-plan.md) before
starting a Branchroom feature. Start with the isolated-store proof in M0 rather than
trying to add restricted refs to a normal Gitea repository.

## Licence

Branchroom inherits Gitea code under the MIT License. See
[LICENSE](LICENSE).
