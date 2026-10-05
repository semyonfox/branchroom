# Branchroom implementation plan

- Status: M0–M1 foundations; M4a–M4h.2a isolated-workspace storage, source policy, restricted Git HTTP read/write, clone handoff/locator rotation, and sealed source-write receive sessions; M2a–M2c development-only encrypted-environment plumbing; v1 restricted workspaces not complete
- Working name: Branchroom
- Fork status: maintained on this fork only; no Branchroom upstream submission or architecture discussion is planned; a read-only Gitea remote is maintenance reference only
- Baseline inspected: `main` at `35786a6ca11c3b9bc5ea852e4aeab42a37fb6d1b` on 19 August 2026
- Last updated: 8 September 2026

This is the versioned implementation plan for this fork. A separately maintained
HTML copy is not built or published by this repository. Until a milestone lands, the
document describes intended behaviour, not a feature that is available.

## This fork today

Branchroom starts as a clean Gitea fork, not as a partially implemented security
product. The inspected baseline has normal Gitea repository hosting, pull requests,
Actions, Git HTTP and SSH, repository-scoped permissions, and a single configured
repository root.

M0a adds a disabled-by-default `[branchroom]` configuration section, an explicitly
provisioned restricted repository root, overlap and symlink-alias validation, and a
low-level facade for an absolute repository location below that root. M0b.1 adds a
test-only public-base import. It uses a bare mirror clone with local optimization and
hard links disabled, removes its public remote, and proves that object directories
and files are not shared. It also proves that a new restricted commit, tree, and blob
are absent from the public store. M0b.2 verifies that ordinary public Git endpoints
do not accidentally select an unregistered restricted store: smart ref advertisement,
known-SHA fetch, dumb object access, remote archive, and ordinary SSH storage-path
resolution all stay in the public repository.

M1 adds ordered migration 349 and persisted scope, grant, decision, and hash-linked
audit-event records. A single server-internal evaluator composes an ordinary Gitea
code-read ceiling with an explicit Branchroom discovery grant. It has no allow cache:
the next evaluation rereads the database, so a committed grant revocation applies to
the next protected request. The first protected operation, `GetScopeMetadata`, returns
only safe metadata and turns a denial or unknown scope into the same not-found result.
The evaluator has focused coverage for direct users, organization teams, and deploy
keys. Actions-task grants now record the real Gitea workflow filename, source
repository, source commit, ref, runner, task, and expiry facts, but no Actions, API,
or Git handler consumes them until it can derive those facts server-side from the
authenticated task.

The first owner-facing page is `/OWNER/REPOSITORY/settings/branchroom`. It is hidden
and returns 404 for every method while the feature is disabled. When enabled, only an
actual repository owner or an organization team with repository-admin access can
create a repository scope; a Gitea site administrator is not a Branchroom bypass.
Creating a scope writes the creator's explicit discovery grant and a safe audit event.
The page can also create and revoke a direct, active individual user's `discover`
grant for an active scope. Once M4d has verified a ready isolated workspace, it can
also grant that user source-read access; this is policy preparation,
not source exposure. It does not create team, bot, deploy-key, Actions-task, write,
or expiring grants. Retired scopes are hidden from a new repository owner after
transfer. The page deliberately says that it creates policy metadata, not a restricted
Git workspace.

M4a adds ordered migration 350 and a persisted `branchroom_workspace` mapping. Its
logical workspace and physical-store identities are separate opaque UUIDs; the record
holds a scope, repository ID, pinned public-base commit, object format, state, and
safe failure code, never a filesystem path, remote URL, or LFS location. An
owner-authorized server-internal creator reserves a `provisioning` row, copies the
public store using the M0 import primitive, verifies the exact commit and object
format in the isolated store, then marks it `ready` and appends an audit event. A
failed import remains non-ready (and is marked `failed` when its safe failure record
commits), so it never becomes routable. M4a itself added no HTTP, SSH, LFS, API, or
clone URL that resolved a mapping to its isolated store.

M4b makes isolated storage manageable without exposing it. The owner-only settings
page can provision one active repository scope from the current default-branch commit
and show the safe workspace state plus its pinned public-base SHA. The handler never
accepts a store identifier or filesystem path, and the rendered page never contains a
store ID, workspace UUID, physical path, or clone URL. It cannot provision an
environment scope, retired scope, or scope in another repository. A `ready` mapping
has only the narrow M4e Git HTTP read route after a direct-user authorization decision;
it still has no SSH, LFS, API, CLI, or Actions route.

M4c adds a package-internal source-read resolver for a ready repository workspace.
It accepts only a repository ID, opaque workspace key, and direct user ID; it reloads
the user and their ordinary code-read permission, serializes with transfer and
deletion, rechecks the repository scope and workspace lifecycle under row locks, and
requires explicit `read_source` access. The accepted policy surfaces are `system` and
the dedicated M4e Git HTTP adapter. The Git HTTP adapter can match an eligible team
grant; deploy-key and Actions-task source grants do not match. It returns a restricted
repository facade only after the allow audit commits, and every denial returns the same
no-such-workspace result. M4d adds the narrowly scoped owner route to manage
source-read access for a ready workspace; it does not render a clone URL. The resolver is a
non-cacheable internal seam, not a general source-access feature.

M2a adds ordered migration 351 and encrypted-environment revision and lease records.
It is a deliberately sealed development-only backend, not an environment product
surface. `[branchroom.environment]` is disabled by default, accepts only a local
development key file, and makes production startup fail when enabled. Each revision
uses a fresh 32-byte data-encryption key, AES-256-GCM payload encryption, and an
AES-256-GCM wrapped data key. The database contains ciphertext, nonces, a safe key
provider/reference, a ciphertext digest, revision metadata, and no plaintext,
filesystem destination, raw key, or raw bearer secret. A direct active user can
obtain a short-lived lease after the evaluator derives their actual ordinary Gitea
code-read permission and matches an explicit `materialize` grant. The raw 32-byte
bearer secret is returned only in memory and persisted as a SHA-256 hash. The lease
is claimed before decryption, so replay, expiry, revocation, or a failed write cannot
make it reusable. Claim is the irrevocable authorization boundary: later revocation
cannot recall a plaintext release already in progress. Materialization accepts only a
unexported `os.Root` materialization primitive, a strict relative destination, and
the internal system surface. It rejects absolute paths,
traversal, volume names, backslashes, `.git`, control characters, and root-escaping
symlinks. There is no web, API, CLI, Git, Actions, runner, direct-storage, or tracked-
reference entry point for this backend.

M2b adds a package-internal identity resolver for an already parsed environment
reference. It revalidates the typed reference, serializes with repository transfer
and deletion through the repository lifecycle lock, and makes exact indexed lookups
by `(repository, lower environment name)` and `(scope, numeric revision)`. It returns
only the scope ID and opaque revision key. The result is a non-cacheable snapshot,
not an authorization or materialization capability: a later lease request repeats
the policy and lifecycle checks. The resolver creates no decision, audit event, or
lease, and no Git hook, route, runner, or CLI invokes it.

M2c adds a pure, disconnected public-content preflight module. It reserves the
case-insensitive `.branchroom` root and permits only
`.branchroom/env/<environment>.toml` files whose parsed environment name matches the
path. It rejects unsafe, control-containing, invalid UTF-8, oversized, and
Windows-aliasing paths; it rejects content larger than 1 MiB rather than silently
skipping it. For ordinary paths it reports at most one line number per rule, in fixed
rule order, without returning any matched bytes. The initial high-confidence rules
are PEM private-key headers, AWS `AKIA`/`ASIA` access-key IDs, GitHub classic,
fine-grained, and stateless token forms, and the default GitLab `glpat-` prefix.
It deliberately does not flag generic `password` configuration or custom GitLab
prefixes. There is no inline source bypass: a future exception must be external and
audited. No push, pull request, import, or publication path calls the preflight yet.

KMS is not part of the self-hosted Git-only V1. `[branchroom.environment]` remains
disabled; no production provider is selected and there is no supported production
environment-materialization mode. If environment materialization returns to scope, it
requires its own key-management design and security review. The development-only
encryption code is a test seam, not a local secret manager or a beta feature: it
reads a local development key, is refused in production, and stores no key bytes. It
is not a supported environment-materialization deployment mode.

Repository transfer retires every affected scope, revokes its active grants, and
retires its workspace mappings and active environment revisions in the same
transaction; it revokes unclaimed environment leases. Repository deletion still
purges policy rows when no retained data exists. Once a workspace mapping or encrypted
environment revision exists, deletion fails closed until a retention collector can
remove the restricted store or encrypted record outside the database transaction. This
avoids silently orphaning or erasing restricted data.

M4e, M4f, and M4g provide a narrow restricted Git HTTP read boundary: an authorized
direct user can retrieve a clone locator from a stable signed-in page, then Git
independently reauthorizes the actual protocol requests. A repository owner can hard
cut over the locator for a ready workspace without revealing either key in settings.
M4h.2a adds the matching restricted HTTPS source-write boundary. A ready-workspace
owner can create and revoke audited direct-user `write_source` grants. A push also
requires active source-read access and ordinary Gitea code-read and
code-write permission. The server issues a one-use, hash-only receive session and
passes its short-lived bearer only to the receive-pack child. It verifies the
server-owned hook, trusted quarantine objects, and fast-forward updates before Git
accepts a ref. The transport accepts only `refs/heads/branchroom/*`, rejects tags,
deletes, and non-fast-forward updates, limits each request to 1 MiB and 45 seconds,
and has no SSH or LFS equivalent. Revocation blocks a new receive session. A session
already claimed by the server may finish.
There is still no SSH, LFS, public Branchroom API, production KMS,
environment-reference integration, materialization CLI, Actions integration, cache
isolation, or backup enforcement. This is not an end-to-end collaboration boundary.

The codebase is Go with a JavaScript and TypeScript frontend. It uses
`modelmigration/` for persisted-schema changes, `models/` for data and permission
logic, `services/` for server behaviour, `routers/` for HTTP and SSH-adjacent
handlers, and `modules/setting/` for configuration. Build and test guidance lives
in `docs/development.md` and `docs/testing.md`.

This fork is maintained on this fork only. A read-only reference to upstream Gitea
may help with maintenance releases; no Branchroom upstream submission or
architecture discussion is planned, and a read-only Gitea remote is maintenance
reference only.

## The decision

Do not change Git itself in v1.

Branchroom keeps ordinary Git repositories ordinary. It adds real access boundaries around work that must stay private:

- Public code and normal private repositories stay close to upstream Gitea.
- Restricted source work lives in separate bare Git repositories with separate object stores.
- A repository may hold safe references to environment files. The plaintext stays in an encrypted environment store outside Git.
- CI jobs and agents get short-lived access that names exactly what they can read or materialize.
- Making an embargoed patch public is a separate release action, not a normal merge.

The product can look like one project to a team. It must not use one shared Git object store for public and restricted source.

## What we are building

Branchroom is a Git host for teams that need ordinary collaboration most of the time and hard boundaries some of the time.

It should support:

- a public repository with normal Git HTTP, SSH, pull requests, issues, and Actions
- restricted branches and pull requests for security fixes or internal work
- versioned environment files that authorized people and jobs can materialize
- scoped CI logs, artifacts, caches, notifications, search results, API responses, and webhooks
- task-scoped access for agents
- a deliberate release path from restricted work to public code

The first useful story is simple:

1. A maintainer creates a restricted security branch from public main.
2. The security group reviews it and runs private CI.
3. A release manager checks what will become public.
4. Branchroom publishes only the approved patch, notes, and advisory.

### A restricted branch is a workspace, not a ref

"Restricted branch" is user-facing shorthand. In v1 it must be a restricted
workspace backed by a separate bare repository, with its own object store and a
mapping to its public base. It is not a special ref inside the public repository.

That distinction drives the later implementation choices:

- clone, fetch, and push must resolve the workspace before Gitea gives Git a storage path
- a restricted pull request needs a first-class mapping to the workspace and its scope
- public-base import must copy data into the restricted store without alternates, hard
  links, shared packs, shared indexes, or shared LFS storage
- promotion must create or apply a public patch deliberately. It cannot turn a private
  ref into a public ref

### Current mapping lifecycle

M4a makes the physical-store relationship explicit without making it reachable:

    provisioning -> ready -> retired
          |
          +-----> failed

Only the future gateway may resolve a `ready` mapping, and then only after it also
proves the owning scope is active and the authenticated request passes the policy
decision. `provisioning`, `failed`, and `retired` mappings have no transport behavior.
The current deletion path rejects any workspace mapping. A later retention collector
must use explicit `purge_pending` and `purged` states, perform filesystem deletion
after a durable database transition, and retain enough safe metadata for recovery and
audit.

## The Git constraint

Git commits are content-addressed. A commit points to one tree. That tree contains path names and blob IDs.

If two people receive different versions of the same tree, they do not have the same commit. The tree hash changes, then the commit hash changes, then every later commit built on it changes too.

That means a normal clone cannot safely contain a secret file and then hide it in the web UI. The client already has the blob.

Git namespaces and hidden refs do not solve this. They share the same object database, and Git documents that namespaces are not read access control.

### The v1 answer

For restricted source code, use a different physical bare repository and object store. All people allowed into that restricted workspace see the same real Git history and the same SHAs. People outside it cannot discover, advertise, or fetch its refs or objects.

For private environment values, do not store plaintext as Git blobs. Commit a safe reference instead:

    # tracked in the repository
    # .branchroom/env/production.toml
    environment = "production"
    revision = 42
    destination = ".env"

The encrypted environment store holds revision 42. A policy decision controls whether a person or job can write that revision to a temporary file.

### What this does not solve

An authorized person can copy plaintext after receiving it. A malicious workflow can try to send a secret over the network. Branchroom stops new access after a grant is revoked and reduces accidental leaks. It cannot take back copied data.

Arbitrary private directories inside an ordinary public clone also stay out of v1. That needs a different Git view for each reader, which means rewritten history or a hardened custom Git gateway. Start with a restricted branch or an explicit private overlay.

## Threat model and failure behaviour

Branchroom protects restricted work from public users, ordinary repository
collaborators, organization members without a grant, API tokens and deploy keys
without a grant, Actions jobs outside the scope, and every secondary read surface
that Gitea exposes.

It does not claim to protect restricted data from a Gitea process administrator who
can alter the running binary, a host administrator who can read process memory or
storage, a database or backup operator with unrestricted access, a key-provider
administrator, or an approved person or runner after plaintext has been delivered.
Those are deployment and operations trust boundaries. They need separate controls
before an internet-facing deployment.

Protected read, write, materialize, and publish operations should fail closed when
the policy evaluator, scope mapping, audit writer, or required storage authorizer
is unavailable. M1 must define bounded decision caching and revocation invalidation.
Revocation stops new requests. It cannot recall source bytes, logs, or environment
values that an authorized client has already received.

## Scope model

A scope is a named private area with a policy. A grant names a subject, an action, the thing it applies to, any conditions, an expiry, and revocation state.

A team name such as "core team" is not a permission by itself. The policy must state what that team can do.

    scope: security-fix-2026-14
    resource: acme/web restricted workspace

    security-team: read_source, push, review, run_restricted_ci
    release-team: approve_publication
    agent: no access by default

    scope: production environment
    owner: edit, read_history, manage_policy
    deploy-production job: materialize revision 42 once, until job ends
    contributor: no access

### Subjects

- People use existing Gitea users and organization teams.
- Workloads use a short-lived identity tied to one repository, workflow, ref, runner, and expiry.
- Agents get a task grant that names exact repositories, scopes, environment names, tools, network rights, and expiry.

### V1 actions

| Action           | Meaning                                                     | Example                                       |
| ---------------- | ----------------------------------------------------------- | --------------------------------------------- |
| Discover         | See a scope in a list, search result, or notification.      | A reviewer can find the restricted PR.        |
| Read source      | Fetch refs and inspect commits, trees, diffs, and files.    | The security team clones its workspace.       |
| Write source     | Push a server-defined private ref in an isolated workspace. | A maintainer updates an embargoed fix.        |
| Write and review | Comment, request changes, approve, or run checks.           | A maintainer reviews an embargoed fix.        |
| Materialize      | Receive one environment revision as a temporary file.       | A deploy job writes revision 42 to .env.      |
| Read history     | Inspect earlier environment revisions and audit entries.    | An environment owner investigates a rollback. |
| Publish          | Move selected restricted work into public history.          | A release manager publishes a security patch. |
| Manage policy    | Create scopes, grants, revocations, and retention rules.    | A project owner adds a reviewer.              |

Branchroom starts with deny by default. A scope grant can only narrow ordinary Gitea repository access. It never gives someone broader repository access than Gitea already allows.

### Policy evaluator contract

Handlers should ask one server-internal evaluator instead of reading grants
themselves. Its input must include the authenticated identity, the ordinary Gitea
repository permission, the requested Branchroom action, resource and scope IDs,
protocol or UI surface, and relevant conditions such as workflow, ref, runner,
environment revision, and expiry.

The evaluator returns an allow or deny decision with a decision ID, the applicable
scope, the reason safe to disclose, and an expiry. A handler may cache a decision
only until that expiry. Grant revocation must invalidate cached allows before the
next protected request. The evaluator must write an audit event without secret
values. If it cannot record a protected decision, it denies it.

## How it works for people and CI

### Embargoed security fix

1. An owner creates a restricted workspace from a chosen public main SHA.
2. Only approved users, teams, and jobs can find it, clone it, read its PRs, or see its checks.
3. The release role freezes the chosen restricted head.
4. Branchroom produces a publication preview.
5. An approved release action applies the chosen patch to public main or a release branch.
6. The product keeps or removes the restricted workspace under its retention policy.

Private PR comments, failed checks, early patches, branch names, and internal discussion stay restricted. Publication does not copy them into public history by default.

### Environment files

The repository records a safe pointer and a desired output path. The environment service holds encrypted revisions.

    $ git clone branchroom:acme/web
    $ cd web
    $ branchroom env materialize staging

The service checks the developer grant before it releases plaintext. A developer may materialize staging while production remains unavailable.

A deployment job gets a one-use lease. It can materialize a named revision only when it is the approved workflow running on the approved ref and runner. The runner deletes the workspace and temporary credentials after the job.

## Architecture

Gitea remains the host for normal repository work. Branchroom adds a policy layer and routes every protected operation through it.

    Gitea web and API                 Branchroom policy service
    Gitea Git HTTP and SSH       <->  scopes, grants, decisions, leases,
    Gitea Actions scheduler            revocations, audit events

                                      Restricted store manager
                                      isolated bare repositories,
                                      base import, promotion, retention

                                      Environment service
                                      encrypted revisions,
                                      materialization, expiry

Both Git smart HTTP and SSH must ask the same policy code before Git advertises a ref or transfers an object. The web UI is not the security boundary.

### Gitea integration map

The plan builds on the existing permission calculation. It does not replace it.
`GetDoerRepoPermission` already handles ordinary users and task-specific Actions
identities. A Branchroom allow must be an additional, narrower decision after the
existing Gitea permission ceiling.

| Concern                | Current seam in this checkout                                                                                             | Required Branchroom work                                                                                                                    |
| ---------------------- | ------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| Repository permissions | `models/perm/access/repo_permission.go`, especially `GetDoerRepoPermission`                                               | Compose a scope decision with existing user, organization-team, deploy-key, Actions-user, repository-unit, and token rules.                 |
| Git over smart HTTP    | `routers/web/repo/githttp.go`, especially `httpBase`                                                                      | Decide and route the workspace before ref advertisement or RPC. Test upload-pack, receive-pack, archive, and anonymous paths.               |
| Git over dumb HTTP     | `routers/web/repo/githttp.go`, `GetLooseObject`, `GetPackFile`, and `GetIdxFile`                                          | Deny object, pack, and index reads outside the scope, including known-SHA attempts.                                                         |
| Git over SSH           | `routers/private/serv.go`, `ServCommand`                                                                                  | Check the scope before returning `RepoStoragePath`. Cover deploy keys and the `refs/for` receive-pack path.                                 |
| Repository location    | `models/repo/repo_location.go`, `modules/setting/repository.go`, and `[repository] ROOT` in `custom/conf/app.example.ini` | Add a separate restricted root and a store mapping. Do not overload the normal repository root. Decide whether v1 permits restricted wikis. |
| LFS                    | `services/lfs/` and `[storage.lfs]`                                                                                       | Give restricted work a separate LFS namespace and verify batch, object, lock, and direct-download paths use the policy decision.            |
| Actions identity       | `models/perm/access/repo_permission.go`, `services/actions/auth.go`, and `services/actions/workflow.go`                   | Bind a lease to one task, repository, workflow, ref, runner, scope, and expiry. Existing task permissions remain the upper bound.           |
| Actions storage        | `modules/setting/actions.go`, `modules/storage/storage.go`, and `models/actions/artifact.go`                              | Route logs, artifacts, and caches to scope-separated storage. Never rely on a public object URL as an authorization check.                  |
| Metadata reads         | `routers/web/`, `routers/api/`, `services/feed/`, `services/webhook/`, search models, and notification models             | Inventory every query and response that can reveal names, counts, IDs, links, payloads, or status. Filter before pagination and indexing.   |
| Schema and API         | `modelmigration/migrations.go`, `docs/development.md`                                                                     | Add migrations in order. New API routes need Swagger comments, generated output, and validation.                                            |

The feature must use a single policy interface in server code. Page handlers,
transport handlers, background jobs, and storage adapters may call it, but none may
reimplement grant matching locally.

### New Branchroom services

- Policy evaluator for scopes, grants, conditions, leases, and revocation.
- Restricted store manager for creation, public-base import, promotion, and retention.
- Encrypted environment revision store and a small command-line client.
- Workload identity and one-use environment lease issuer.
- Append-only scope audit stream.
- Scoped search, webhook, artifact, and cache policy.

## Data and key handling

The exact database schema can follow the first spike. These record boundaries need to exist from the start.

| Record                   | Purpose                                                                             | Must never contain                    |
| ------------------------ | ----------------------------------------------------------------------------------- | ------------------------------------- |
| Scope                    | Names a restricted workspace, environment, or later overlay.                        | Secret content                        |
| Grant                    | Names subject, action, resource, conditions, expiry, and revocation.                | A hidden broad admin bypass           |
| Restricted store mapping | Links a logical branch or PR to its isolated bare repository and public base.       | Public-store alternates               |
| Environment revision     | Holds immutable encrypted payload, key reference, revision number, and safe digest. | Plaintext in ordinary Git             |
| Lease                    | Names one job, one resource set, one expiry, and one request ID.                    | A reusable owner token                |
| Audit event              | Records who asked, what the service allowed or denied, when, and why.               | Secret values or raw environment text |

Use envelope encryption: each environment revision gets its own data key, and a
configured key provider encrypts that data key. That discussion is future
environment work, not current V1 behaviour.

KMS is not part of the self-hosted Git-only V1. `[branchroom.environment]` remains
disabled; no production provider is selected and there is no supported production
environment-materialization mode. If environment materialization returns to scope, it
requires its own key-management design and security review.

### First schema checkpoint

M1 needs a concrete relational design before routes or UI work. The initial records
should have stable IDs, timestamps, actor IDs, state, and explicit foreign keys.
The exact table names can follow local conventions, but the relationships cannot be
implicit:

| Record                   | Minimum relationship and fields to settle in M1                                                                                 |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------- |
| Scope                    | Kind, display name, state, owner, and the logical repository or environment it protects.                                        |
| Grant                    | Scope, subject type and ID, action, conditions, not-before time, expiry, revocation time, and revoker.                          |
| Restricted store mapping | Scope, logical public repository, physical restricted repository identity, public-base SHA, object format, and retention state. |
| Environment revision     | Environment scope, revision, ciphertext reference, wrapped-key reference, safe digest, creator, and lifecycle state.            |
| Lease                    | Task, scope, revision set, workflow, ref, runner, request ID, expiry, and one-use state.                                        |
| Audit event              | Decision ID, actor, action, resource, result, safe reason, timestamp, and a tamper-evidence strategy.                           |

`Repository.IsPrivate` is not the data model for restricted source. It describes
ordinary Gitea privacy and still uses the normal repository store. The first
restricted-workspace migration should link to the logical public repository without
teaching generic repository queries to treat normal private repositories as scoped.

Every persisted model needs an ordered migration in `modelmigration/` and migration
tests. The design also needs a downgrade policy. Once a workspace has restricted
source or encrypted environment revisions, a rollback must not silently move data
into a shared store or discard the ability to read its audit history.

M1's repository-deletion behavior is deliberately narrower: it purges policy rows
with the deleted repository when there is no restricted workspace mapping. M4a blocks
deletion once a mapping exists, because the restricted bare store cannot safely be
removed inside a database transaction or silently left behind. A future archival or
legal-retention mode must add an explicit tombstone, retention schedule, and
post-commit collector before it can preserve or purge a mapping after deletion.

## CI, runners, and agents

A runner that can read production data is part of the production boundary.

### Avoid an existing naming collision

Gitea already has an Actions feature named "scoped workflows." It controls where
workflow definitions come from. It is not a confidentiality boundary. Branchroom
must use a distinct model, route, configuration, and UI name such as "Branchroom
scope" or "restricted workspace" so a reviewer cannot mistake one for the other.

| Job type          | Allowed access                                                                  | Default safety rule                                 |
| ----------------- | ------------------------------------------------------------------------------- | --------------------------------------------------- |
| Normal PR job     | Read-only job token. No restricted refs or protected environments.              | Fork work gets no private data and no shared cache. |
| Restricted job    | Named restricted workspace. Logs, artifacts, and status inherit its scope.      | Use an ephemeral isolated worker.                   |
| Production deploy | One selected environment revision after approval. Optional cloud OIDC exchange. | The lease expires with the job.                     |

Rules that fail closed:

- Secret-bearing work runs only on ephemeral isolated workers.
- Restricted jobs cannot use host mode or a mounted Docker socket.
- Artifact upload defaults to off after a secret is materialized.
- Runner diagnostics and debug logs carry the job scope.
- Forked PRs never inherit production environments.
- A job cannot turn a user token into a broader job token.
- Leases name the job, workflow, ref, runner, revision, expiry, and request ID. They cannot be replayed.

Use OIDC for cloud access when possible. A signed short-lived workload identity is safer than placing a long-lived cloud credential in an environment file.

Agents start as ordinary contributors. Give an agent a restricted test job when it needs one. Do not hand it a long-lived secret. Record each agent lease and tool token in the audit trail.

## Publication flow

A normal merge must never quietly make an embargoed patch public.

The publication job:

1. Freezes the private source SHA and public target SHA.
2. Shows changed files, diff summary, resulting public SHA, release notes, advisory state, and approvals.
3. Applies the approved patch to the public target.
4. Publishes the release and advisory at the chosen time.
5. Applies the restricted workspace retention policy.

Only a release role with the publish action can run this job.

## Roadmap

### M0: fork discipline and threat model

Prove storage isolation before a protected feature is exposed. Keep active work on
this fork. A read-only upstream reference is optional maintenance plumbing. Any
future upstream discussion starts with the design and threat model, not a large patch.

M0a establishes the configuration and repository-location primitive. The setting is
off by default. When an operator enables it, they must supply an existing restricted
root that does not overlap the public repository root, even through a symlink. The
primitive does not create a workspace or route Git requests.

M0b.1 proves a safe public-base import in a unit test. It mirrors one public bare
repository into a configured restricted root with `--no-local` and `--no-hardlinks`,
then removes the clone remote. The test checks bare repositories, matching copied
refs, separate object directories, no alternates, no remote, no shared object files,
and a new restricted commit/tree/blob that the public store cannot read. M0b.2 adds
the meaningful route-level regression tests before a logical workspace exists. It
does not test Branchroom authorization: there is no workspace URL, mapping, or
policy decision yet.

Work:

- Record the fork baseline and the supported Gitea upgrade procedure.
- Add a disabled-by-default feature flag and configuration validation. Do not change
  normal Gitea behaviour while it is off.
- Write the threat model, trust-boundary diagram, and protected-operation inventory.
- Build a test-only isolated-store spike with one public repository and one
  restricted bare repository. It must use a different bare repository and no alternates,
  shared packs, hard links, shared LFS root, or shared backup job. M0b.1 covers the
  bare Git store, alternates, and hard links. LFS and backup isolation remain open.
- Write hostile protocol tests before product UI: ref advertisement, ref fetch,
  known commit/tree/blob SHA fetch, dumb HTTP object access, LFS batch/object access,
  SSH clone, SSH push, archive download, and direct storage URLs. M0b.2 now covers
  the ordinary public HTTP variants plus the ordinary SSH storage-path assertion.
  LFS, direct storage URLs, and scoped SSH clone/push await a logical workspace.

Verification:

- Run the smallest focused Go tests while adding the spike, then the matching
  integration Git HTTP, SSH, and LFS tests.
- Run `make build` and `make checks` before the milestone closes.

Exit gate: normal Gitea clone, push, pull request, Actions, and upgrade tests still
pass. The team agrees which attackers and administrators are trusted. An actor with
ordinary public-repository access cannot find any restricted object through a
test-only store.

### M1: scopes and audit

Add scope records, grants, revocation, policy decisions, and an append-only audit
trail. Start with people, teams, and workflow identities. Build the admin screen and
API after the server policy API exists.

#### Implemented foundation

- Migration 349 creates `branchroom_scope`, `branchroom_grant`,
  `branchroom_decision`, and `branchroom_audit_event`. The audit stream carries a
  per-scope sequence and SHA-256 chain; decisions and events use safe UUID
  correlation keys.
- `services/branchroom.Evaluator` is the only policy entry point in this slice. It
  executes `discover` only for the exact `scope/<id>` metadata resource. M2a also
  recognizes `materialize` for one exact environment-revision resource, but only when
  the development environment feature is enabled, the caller is a direct user, and
  the surface is the internal system surface. Denied decisions do not return scope
  metadata. Every future action remains fail-closed until a handler defines its
  ordinary Gitea permission ceiling.
- A scope creation transaction checks real repository ownership, persists the scope,
  gives the creator an explicit discovery grant, and appends a `scope_created` audit
  event. Site-administrator status is deliberately ignored by that ownership check.
- The settings page is server rendered. It hides its navigation item and gives a 404
  for direct GET and POST requests when disabled. It does not pretend that a scope
  creates a protected branch or storage location.
- The owner-only settings page can create and revoke direct active-individual
  `discover` grants for active scopes. The service resolves the target only after it
  verifies ownership and scope state; it records only the immutable grant ID in the
  audit event. It exposes no controls for team, bot, deploy-key, Actions-task, source,
  write, or other future grant types. Retired scopes are not listed or mutable.
- Repository transfer retires scopes, revokes active grants, and appends a safe
  lifecycle event. Repository deletion removes scope, grant, decision, and audit
  rows atomically with the normal repository deletion flow.

The M1 exit gate remains open: current policy coverage is limited to discovery
metadata and a deliberately narrow owner grant-management path. There is no public
Branchroom API, and no restricted workspace transport has been wired to the evaluator.
Before any Actions, API, or Git surface uses task grants, it must resolve the actor,
ordinary permission ceiling, and workflow facts from Gitea's authenticated context
rather than accepting caller-supplied values.

Work:

- Add the first models and ordered migrations for scopes, grants, decisions, and
  audit events. Keep encrypted payloads and physical storage paths out of generic
  activity records.
- Define a server-internal evaluator interface and make one low-risk protected
  operation use it end to end.
- Compose decisions with Gitea's existing repository permission calculation instead
  of replacing users, teams, deploy keys, token scopes, or Actions task permissions.
- Define grant conditions, expiry, cache lifetime, revocation invalidation, safe
  denial messages, audit retention, and audit-query permissions.
- Add admin and API design only after the evaluator has integration coverage. API
  work includes Swagger comments, `make generate-swagger`, and
  `make swagger-validate`.

Verification:

- Unit-test the evaluator's allow, deny, expiry, and revocation cases.
- Add migration tests and integration tests for user, team, deploy-key, and Actions
  task composition.

Exit gate: revoking a user blocks newly requested access across every protected entry
point already wired to the evaluator. Every allow and deny can be explained without
showing private content.

### M2: versioned environment files

Build the encrypted environment store, safe Git reference format, explicit
materialize command, protected-path checks, and basic plaintext-secret detection.
Keep Gitea Actions secrets for compatibility, but do not make them the product
model.

#### Implemented M2a development foundation

- Migration 351 creates `branchroom_environment_revision` and
  `branchroom_environment_lease`. Revisions are immutable, scope-bound, and contain
  only encrypted payload data, wrapped data-key material, nonces, digest, length,
  safe key-provider metadata, and retirement state. Leases contain only an opaque
  UUID, SHA-256 bearer hash, actor, grant/decision binding, expiry, and one-use state.
- The only configured provider is `development-file`. It reads a base64 32-byte
  development key from an absolute `file:` URI, is refused in production, and stores
  no key bytes: configuration retains only the local key-file path, and database rows
  never hold key bytes or that path as a key reference. It is not a supported
  environment-materialization deployment mode, and no managed KMS is awaited for this
  V1.
- The server-internal revision service clears its input buffer, makes a fresh data key
  for every revision, uses authenticated encryption with revision metadata as AAD, and
  records only safe audit metadata. An environment scope gives its creator an explicit
  direct-user `materialize` grant; there is no UI or public grant-management control
  for it.
- The server-internal lease service recomputes the direct user's ordinary Gitea
  code-read ceiling, evaluates the exact `scope/<id>/environment-revision/<uuid>`
  resource, persists only a hash of a fresh 32-byte bearer secret, and issues a bounded
  one-use lease. It repeats policy evaluation before claim and consumes the lease on
  success, denial, expiry, or local-write failure. Claim is deliberately the
  irreversible authorization boundary; later revocation cannot recall an in-progress
  plaintext release.
- The root writer uses an `os.Root` supplied by a future isolated runner. It accepts
  only a strict relative destination and writes an fsynced 0600 temporary file before
  renaming it into that root. It is not a generic checkout writer.
- A strict standalone parser accepts only the environment name, immutable revision,
  and relative destination fields of a tracked reference. Its package-internal
  resolver returns only a scope ID and opaque revision key after exact active-state
  checks; no Git hook, route, runner, or CLI invokes it yet.
- A conservative public-content preflight reserves the safe reference directory and
  reports only high-confidence plaintext credential shapes. It is a bounded scanner,
  not a secret-manager replacement or a complete detector, and remains disconnected
  from repository write paths until push, pull-request, import, and publication rules
  can all enforce the same fail-closed result.

M2a–M2b are intentionally not enabled by any UI, API, command, Git route, Actions
task, runner, or storage URL. They are auditable primitives and regression-test seams
only. Do not widen their `SurfaceSystem` or direct-user gates without the M3
resolved-identity design and a new security review.

Work:

- Specify and strictly parse the tracked reference format. Environment name,
  revision, and destination must be validated before a materialization request.
- Only if environment materialization returns to scope, choose a revision-store key
  provider through its own key-management design and security review. Until then no
  provider is selected and none blocks the self-hosted Git-only V1.
- Materialize only through a lease. Reject path traversal, absolute destinations,
  symlink escapes, repeated lease use, expired leases, and unapproved runners.
- Define protected paths, detector patterns, allowed false-positive bypasses, and
  audit rules for push, pull-request creation, import, and publication.
- Keep ciphertext, key references, and safe digests separate from plaintext. Logs,
  errors, artifacts, and audit events must not contain the value.
- If environment materialization returns to scope, its own key-management design and
  security review must cover explicit key rotation/recovery rules, an isolated runner
  root lifecycle, safe completion/reconciliation for failed physical writes, and the
  repository retention collector. That collector must preserve a durable per-scope
  revision high-water mark or tombstone: a numeric tracked revision must never be
  reused for different ciphertext after retention. The development-file seam stays
  refused in production and is not a supported deployment mode.

Verification:

- Test that a public clone contains only the safe reference.
- Test allowed and denied materialization, expiry, revocation, destination handling,
  and redaction.
- Test that reference resolution chooses the requested immutable revision rather than
  the newest one, and fails after scope retirement or transfer without writing an
  audit event, decision, or lease.
- Test protected-path and plaintext-detector rules against CRLF, token-format, path
  alias, oversize, redaction, and false-positive cases before wiring any write path.
- Run focused backend tests and one integration path that creates, materializes, and
  expires a revision.

Exit gate: a public clone contains no plaintext environment content. A core developer
can get only an allowed environment. Audit logs contain no secret values.

### M3: scoped CI and workload identity

Add approval gates, one-job environment leases, ephemeral runners, scope-separated
logs and artifacts, teardown checks, and optional OIDC exchange for deploys.

Work:

- Bind a workload identity to one Actions task, workflow revision, repository, ref,
  runner, scope, and expiry. A workflow cannot widen it.
- Define the runner topology that is supported for secret-bearing jobs. Host-mode
  runners and mounted Docker sockets are out of scope for that topology.
- Route scoped logs, artifacts, caches, coverage, and runner diagnostics through
  the same policy decision and a separate storage namespace.
- Turn artifact upload off after materialization unless an explicit policy permits a
  named safe output.
- Add deterministic teardown checks for workspace files, temporary credentials,
  caches, and runner registration.

Verification:

- Test a trusted deployment job, a forked pull request, a changed workflow, a wrong
  ref, an expired lease, and a stale runner.
- Run focused Actions tests plus an integration path with an ephemeral runner.

Exit gate: a trusted deployment job gets one chosen revision. A fork job, changed
workflow, or wrong ref cannot. Teardown confirms no secret-bearing workspace or
cache remains.

### M4: restricted branches and pull requests

Build the isolated repository root, public-base import, Git HTTP and SSH scope
gateway, restricted-workspace and pull-request models, scoped search,
notifications, webhooks, checks, and artifacts. Start with a closed beta for
security fixes.

#### Implemented M4a: non-routable workspace mapping

- Migration 350 adds the one-workspace-per-scope mapping with separate opaque logical
  and store UUIDs, a full public-base commit ID, object format, lifecycle state, and
  no filesystem path.
- `CreateRepositoryWorkspace` is server-internal and owner-authorized. It validates
  the exact public commit, reserves `provisioning`, imports with no local optimization
  or hard links, removes the public remote, re-verifies the isolated copy, and commits
  `ready` with a safe audit event.
- Tests prove the resulting restricted commit is absent from the public store, failed
  imports stay non-routable, a transfer retires the mapping, and repository deletion
  refuses to orphan a mapping.
- This slice intentionally adds no route, clone URL, setting-page control, API,
  permission grant, or LFS namespace. `ready` means storage verified, not access
  authorized.

#### Implemented M4b: owner-only storage provisioning

- The existing owner-only settings page can provision one active repository scope
  from the current default-branch commit. The server resolves and verifies the full
  commit ID itself; the browser supplies only a scope ID.
- The page shows `ready`, `provisioning`, or `failed` state and the public-base SHA,
  never a physical path, store ID, workspace key, or clone URL. Environment, retired,
  disabled, foreign-repository, duplicate, and non-owner requests fail closed.
- Provisioning is synchronous and a mapping still blocks repository deletion until a
  retention collector exists. M4b itself added no retry, delete, source grant, Git
  route, LFS namespace, or API.

#### Implemented M4c: internal direct-user source authorization

- `authorizeWorkspaceRead` is package-internal and accepts only a repository ID,
  opaque workspace UUID, and direct user ID. It never accepts a scope ID, store ID,
  caller-supplied policy actor, permission ceiling, or protocol surface.
- It locks the repository lifecycle, scope, and workspace in that order, reloads the
  active individual user and ordinary Gitea code-read ceiling, then evaluates an
  explicit `read_source` grant (direct-user, or org-team subject whose team belongs
  to the repo-owner organization) for `workspace/<opaque-key>` on the internal
  system surface. Team grants may also carry `discover`; `write_source` stays
  user-subject only and can succeed only through the sealed receive admission. A non-ready workspace produces an audited safe denial;
  unknown, malformed, disabled-feature, and pre-policy mapping denials stay opaque.
- The returned facade is not a cacheable capability. M4e adds one narrowly bounded
  Git HTTP caller; there is still no generic web, API, SSH, LFS, Actions, or CLI
  caller. A high-volume transport gateway will need a less contentious lifecycle
  serialization mechanism before it calls this once per protected Git operation.
- Focused tests cover discovery-only, not-yet-valid, expired, revoked, direct active
  grants; ordinary-permission denial; malformed and foreign keys; workspace retirement;
  transfer; and ambient-transaction rejection. Source-read grants are currently
  structurally limited to direct users.

#### Implemented M4d: controlled source-read grants

- A repository owner can create or revoke a direct active individual's `read_source`
  grant only after the repository scope has a valid `ready` isolated workspace. The
  browser supplies a scope ID and username only; it never supplies a workspace key,
  store ID, physical path, clone URL, or source object.
- Creation locks the repository lifecycle, scope, and workspace in the same order as
  workspace authorization; revocation serializes with the lifecycle and scope. The
  operations reject disabled, retired, foreign, environment, non-owner, duplicate,
  and non-ready creation targets without creating a grant or an audit event; successful
  changes are audited.
- The settings page displays safe direct-user grant state and explicitly says that a
  source-read grant does not itself reveal a clone URL. M4e makes it one required
  check for a narrow Git HTTP read route; SSH, LFS, API, CLI, Actions, and other public
  source routes still do not call the resolver.

#### Implemented M4e: narrow smart Git HTTP read path

- Migration 352 gives every workspace an independent, cryptographically random,
  lowercase base32 route key. It is a non-correlating locator rather than an
  authorization credential; migration backfill generates fresh random values and
  preserves existing values on retry.
- The only registered restricted route is
  `/_branchroom/{owner}/{repository}/{route-key}.git`. It accepts only GET/HEAD
  `info/refs?service=git-upload-pack` or `git-receive-pack`, plus POST `git-upload-pack`
  or `git-receive-pack`; every other method, query shape, service, object, archive,
  LFS, and wildcard path returns the same generic not-found response before policy
  evaluation.
- A valid operation requires Basic authentication from an active individual, ordinary
  Gitea code-read permission, a ready repository workspace bound to that exact
  repository and route key, and active source-read access from an individual or an
  eligible org team. PATs need
  `read:repository`; public-only PATs, browser sessions, bearer/OAuth credentials,
  Actions identities, bots, deploy keys, and unsupported token scopes fail closed.
  Authorization repeats separately for advertisement and RPC.
- The read gateway runs `git upload-pack --stateless-rpc` against the isolated facade.
  M4h.2a adds a separate `git receive-pack --stateless-rpc` path with its own
  authorization and one-use server-issued admission. Neither path calls normal Git
  HTTP repository resolution, and neither exposes a physical path, store ID, workspace
  UUID, ref, or object through denial responses. Responses are `private, no-store`,
  vary by `Authorization`, and skip the normal `go-get` metadata interceptor.
- Integration coverage proves a granted user clones a restricted-only commit from the
  isolated store while normal public Git HTTP cannot advertise it. It also covers
  anonymous/session/bearer indistinguishability, token scope restrictions, feature
  gates, wrong-repository route-key replay, malformed/unsupported request auditing,
  revocation between advertisement and RPC, and all deliberately absent HTTP surfaces.
- This is an experimental transport proof, not a full workspace: the settings page
  reveals no route key or clone URL, and there is no SSH, LFS, API, Actions, PR,
  search, retention, or recovery flow.

#### Implemented M4f: authorized clone handoff

- The stable signed-in page `/{owner}/{repository}/-/branchroom` returns source
  handoffs only for an active direct individual with ordinary Gitea code-read
  permission, a direct `discover` grant, and active source-read access for a ready
  repository workspace. It returns the same not-found response to everyone else,
  including an owner or site administrator without those direct grants.
- The page URL itself has no workspace, scope, route key, store ID, or source object.
  It renders only each authorized scope name and that scope's opaque clone URL.
  It uses `private, no-store`, varies by Cookie and Authorization, disables referrers,
  and opts out of indexing. Ordinary repository pages and settings continue to reveal
  no clone locator.
- Page rendering reads fresh lifecycle, user, permission, scope, grant, and workspace
  state without using the decision evaluator, so refreshes do not append policy
  decisions or audit events. The M4e gateway remains the authority for source access:
  it reevaluates and audits advertisement and RPC separately.
- Integration coverage proves grants are intersected, another user's scope and locator
  do not render, the authorized URL clones the isolated store, and revoking source
  access removes both the handoff and fresh Git access.

#### Implemented M4g: owner-only clone-locator rotation

- The owner-only settings page can rotate the independent opaque route key for an
  active repository scope with a valid `ready` workspace. The request names only the
  scope. It does not accept or display a route key, workspace key, store ID, path, or
  clone URL.
- Rotation is a hard cutover. Once its transaction commits, the old locator has no
  alias, redirect, or grace period, and a fresh request using it returns the same
  non-disclosing result as an unknown locator. A Git protocol phase authorized before
  the commit may finish.
- Rotation changes only the locator. The Git gateway still checks ordinary Gitea
  code-read permission and active source-read access. A refreshed handoff still
  requires a direct `discover` grant and active source-read access as well.
- The safe audit event records that a route key was rotated, but no audit record
  contains the old or new key, a clone URL, a filesystem path, or a store ID.
- Focused model, service, settings, handoff, and Git HTTP checks cover hard cutover,
  stale-key failure, fresh authorized handoff, grant preservation, and rejection of
  retired or non-ready state.

#### Implemented M4h.2a: restricted HTTPS source-write receive path

- Owners of a ready workspace can create and revoke audited direct-user
  `write_source` grants. A push needs ordinary Gitea code-read and code-write
  permission, active source-read access, and that direct `write_source` grant.
  Source-read access may come from a direct grant or an eligible organization team.
- The route independently authorizes receive-pack advertisement and RPC. It accepts
  Basic authentication from an active individual and requires a `write:repository`
  scoped PAT when a PAT is used. Browser sessions, bearer/OAuth credentials, Actions,
  bots, deploy keys, and unsupported token scopes fail closed.
- Migration 354 stores a one-use receive session with only its UUID key, a SHA-256
  bearer hash, scope, workspace, repository, direct-user binding, expiry, state, and
  terminal outcome. The server issues and claims it after authorization, then passes
  the raw bearer only to the receive-pack child. It never appears in a browser,
  database, log, audit event, or error.
- The pre-receive hook verifies the trusted Git quarantine and the server-owned hook
  tree. It accepts only valid, reachable commits in the workspace object format. A
  push can create or fast-forward only `refs/heads/branchroom/*`. Tags, deletes,
  non-fast-forward updates, malformed objects, and mixed object formats fail.
- The receive RPC has a 1 MiB request limit and a 45 second process limit. It uses a
  separate `git receive-pack --stateless-rpc` child, never normal Git HTTP storage
  lookup or ordinary hook dispatch. SSH and LFS have no write route.
- Claim is the receive authorization boundary. Revoking a grant, changing ordinary
  permission, retiring a workspace, or transferring the repository blocks a new
  receive session. One already claimed session may finish, but disabling Branchroom
  rejects it in the hook. Interrupted claimed sessions are reconciled as failed.

#### Remaining V1 receive work

- Keep actual Git-push adversarial integration coverage for revocation between
  advertisement and RPC, replay and concurrency, lifecycle races, hook and
  configuration tampering, limits, and known-SHA public-store isolation.
- Exercise self-hosted recovery, restricted backup/restore, and stale-session
  reconciliation before treating the transport as a broader collaboration workflow.

The narrow Git-only self-hosted V1 is direct-user clone and protected push over the
isolated HTTPS transport only. Out of V1 stay SSH, LFS, normal Git HTTP outside the
restricted route, API and CLI, archives and raw paths, pull requests, reviews, search,
notifications and webhooks, Actions, runners, artifacts and caches, environment
materialization and KMS, promotion and retention, hosted and multitenant operation,
per-role histories, and partial clones.

Work:

- Extend the transport only after separate design and review for SSH and LFS. Do not
  route dumb HTTP, archive, generic repository object lookups, or normal Git HTTP
  receive-pack to a restricted store by accident.
- Decide v1 behaviour for wikis, repository archives, blame, raw-file URLs, badges,
  releases, packages, dependency metadata, imports, exports, mirrors, forks, and
  attachments. Unsupported operations must reject a restricted workspace clearly.
- Add a restricted pull-request mapping and make comments, reviews, commit statuses,
  checks, logs, artifacts, search, feeds, notifications, webhooks, and API
  pagination inherit its scope.
- Test direct storage URLs, cache keys, index queries, and backup jobs as
  authorization boundaries, not only web pages.

Verification:

- Extend Git HTTP, SSH, LFS, API, search, notification, webhook, Actions artifact,
  and web UI integration tests with scoped and unscoped actors.
- Run targeted tests during each slice, then the relevant integration suite and
  `GITEA_TEST_E2E_FLAGS='<filepath>' make test-e2e` for UI flows.

Exit gate: an unauthorized actor cannot discover, advertise, fetch, or SHA-fetch a
restricted ref or object through HTTP, SSH, API, search, or a secondary store.

### M5: publication and security release flow

Add promotion preview, release approver role, public target checks, advisory flow,
retention policy, and disclosure regression tests.

Work:

- Choose and document one deterministic promotion mechanism. The first candidate is
  a server-controlled patch application or cherry-pick onto a frozen public target.
- Freeze the private source SHA, public target SHA, approvals, advisory state, and
  release time before generating the preview.
- Apply and validate the candidate result in a disposable public-store checkout.
  Show the resulting SHA, changed files, conflicts, release notes, and what remains
  restricted.
- Separate approval from execution. A normal merge, push, or branch update cannot
  trigger publication.
- Apply the retention policy after publication without joining the restricted object
  store to the public one.

Verification:

- Test an approved promotion, a rejected promotion, a changed public target, a
  changed private source, an expired approval, and a retry after interruption.
- Re-run all discovery and known-SHA regression tests after publication.

Exit gate: the preview shows exactly what becomes public. Private pull-request
history stays private. Only an approved release role can publish.

### M6: agent integration and operational beta

Add task-scoped agent grants, agent audit views, GitX-aware job preflight, backup
and recovery drills, upgrade runbooks, observability, an external security review,
and a small hosted beta.

Work:

- Add task grants that name exact repository, scope, environment, tools, network
  rights, and expiry. Agents begin with no restricted access.
- Keep GitX preflight separate from authorization. Record its declared tool
  permissions and the Branchroom decision in the audit trail.
- Define scope-separated backup, restore, key recovery, and deletion procedures.
  Restore drills must show that public and restricted stores do not become joined.
- Add redacted metrics, operational alerts, a rebase and upgrade runbook, and an
  incident procedure for suspected disclosure.
- Obtain an independent security review before exposing restricted Git transport to
  untrusted internet users.

Verification:

- Run backup and restore drills for public repositories, restricted repositories,
  environment revisions, policy records, and key material separately.
- Test agent task expiry, tool-scope denial, lease replay, and recovery without a
  wider grant.

Exit gate: restricted agents cannot leave their task scope. Recovery restores each
store without widening access. An independent review closes the major findings.

## Security gates

These are product requirements. If one fails, the related feature remains behind a flag.

- Public users cannot enumerate or fetch a restricted ref, commit, tree, blob, or LFS object by name or known SHA.
- HTTP, SSH, API, web UI, search, feeds, notifications, webhooks, and caching give the same scoped answer.
- A public clone gets neither private environment bytes nor an object ID that commits to their plaintext.
- Restricted pull request logs, coverage, artifacts, and runner diagnostics inherit the pull request scope.
- Secret-bearing teardown removes workspaces, temporary files, credentials, and caches.
- Protected public paths reject plaintext secret content and provide a useful next step.
- Grant revocation blocks new Git access and materialization immediately. Audit history remains intact.
- Publication preview shows source SHA, target SHA, changed files, result SHA, approvers, release time, and disclosure state.
- Backups and restores preserve scope separation. They never join public and restricted object stores.

### Acceptance-test inventory

The security gates need named tests, not only a checklist. Each new protected route
must be added to this inventory before it is enabled.

| Surface                          | Verified starting point                                                            | Required proof                                                                                                                                                                                     |
| -------------------------------- | ---------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Restricted smart Git HTTP        | `routers/web/repo/branchroom_githttp.go`                                           | Authorized users can upload-pack. Receive-pack needs code-write, source-read, and direct write-source access; only fast-forward `refs/heads/branchroom/*` pass. Other cases are non-disclosing.    |
| Dumb Git HTTP                    | `GetLooseObject`, `GetPackFile`, and `GetIdxFile` in `routers/web/repo/githttp.go` | A known restricted blob, tree, commit, pack, or index cannot be fetched by URL.                                                                                                                    |
| SSH                              | `routers/private/serv.go`                                                          | An unauthorized user or deploy key cannot receive a restricted storage path, clone, fetch by SHA, or push.                                                                                         |
| LFS                              | `services/lfs/`                                                                    | Batch, object, lock, and direct-download paths do not reveal restricted object IDs or bytes.                                                                                                       |
| Web and API                      | `routers/web/` and `routers/api/`                                                  | Lists, counters, pages, raw files, archives, diffs, blame, autocomplete, and pagination hide inaccessible work before response construction.                                                       |
| Search, feeds, and notifications | Search models, `services/feed/`, and notification handlers                         | An inaccessible workspace contributes no name, count, ID, link, payload, or timing clue.                                                                                                           |
| Webhooks                         | `services/webhook/`                                                                | Creation, delivery, replay, payload persistence, retry, and diagnostics inherit the scope.                                                                                                         |
| Actions                          | Actions models, services, and storage                                              | Logs, artifacts, caches, coverage, statuses, runner diagnostics, and job tokens cannot cross the scope.                                                                                            |
| Object storage and backups       | Storage adapters and deployment jobs                                               | A signed or direct object URL, backup, restore, garbage collector, or cache key cannot bypass the policy or combine stores.                                                                        |
| Promotion                        | Publication service                                                                | Only the approved patch reaches the public store. Private comments, rejected attempts, earlier commits, logs, and attachments remain restricted.                                                   |

## What to take from other platforms

| Platform | Bring over                                                                                      | Do not mistake it for                                                                        |
| -------- | ----------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| Gitea    | MIT Go base, teams, repository UX, Actions, ephemeral runners, normal Git compatibility.        | Repository-wide roles and Actions secrets are not scoped confidential work.                  |
| GitHub   | Environment approvals, OIDC workload identity, CODEOWNERS, private-fork security advisory flow. | Protected branches and environments do not hide source directories in a normal clone.        |
| GitLab   | Short-lived job tokens, environment-scoped file variables, project allowlists.                  | A protected branch is not an isolated confidential object store.                             |
| Forgejo  | Runner scoping and the clear warning that host runners are not a security boundary.             | Exact GitHub Actions behavior should not become a product promise.                           |
| Gerrit   | Separate rights for access, review, approval, and submit.                                       | Private changes are unsafe for embargoed fixes when commits share a repository object store. |
| OneDev   | First-class job configuration and private build inputs.                                         | It is not the starting base. Branchroom stays Go and Gitea-based.                            |

## Where GitX fits

GitX stays standalone at first.

GitX helps people inspect and pin developer tools before they run. Branchroom controls source, reviews, environment files, and workload access. They solve different problems.

The first integration can be small:

1. A scoped Branchroom job runs GitX preflight before it runs a repository tool.
2. The job audit records the pinned source, declared tool permissions, policy decision, and task identity.

GitX is not the secret boundary. Its current permission declarations warn people, but they do not sandbox code. Branchroom still needs isolated runners and narrow leases.

## Not in v1

- Per-role rewritten Git history.
- Arbitrary private source folders in an ordinary clone.
- A full secret-manager replacement.
- Custom partial-clone access control.

Research later:

- A private source overlay for code that can stay outside the public tree.
- A hardened partial-clone gateway. Git partial clone was made for performance, and it can still leak metadata.

## Operational work

- Track Gitea releases closely in this fork. Keep the fork delta small, package the
  changes cleanly, and test every rebase before shipping. A read-only upstream remote
  is a maintenance choice only. Branchroom changes stay in this fork.
- Start self-hosted or single-tenant. Do not add hosted multi-tenant work until storage, tenancy, keys, audit retention, and abuse controls each have an owner.
- Test backups and recovery for public repositories, restricted repositories, encrypted environment revisions, policy data, and key recovery separately.
- Threat model every new scoped service. Pay for an external review before an internet-facing beta exposes restricted Git transport.
- Say exactly what a scope protects and what it does not. Do not blur code-owner review rules, branch protection, encryption, and confidentiality.
- Define incident handling before beta: revoke grants and leases, isolate the runner,
  preserve non-secret evidence, assess secondary stores, notify the affected owner,
  and decide whether the restricted workspace needs retirement.

The first demo worth showing is one security fix end to end. Create a restricted branch from a public repository. Review it privately. Run a scoped CI job. Approve a release. Publish the patch and advisory. Prove that a public contributor cannot see the pre-release work.

## Decisions before implementation

These choices do not change the v1 design. They set the first deployment shape.

| Decision                   | Recommended first choice                                                                                                        |
| -------------------------- | ------------------------------------------------------------------------------------------------------------------------------- |
| Deployment                 | Self-hosted or single-tenant.                                                                                                   |
| Identity                   | Native Gitea users and organization teams. Add SSO and SCIM after scope rules settle.                                           |
| Key provider               | Deferred: no provider for self-hosted Git-only V1; decide only if environment materialization returns to scope.                 |
| Naming                     | Keep Branchroom as a working name. Keep code and protocol names easy to rename later.                                           |
| Restricted source identity | A user-facing restricted workspace backed by a new physical bare repository. It is never a hidden ref in the public repository. |
| Public-base import         | Copy into the restricted store without Git alternates, hard links, shared packs, shared indexes, or shared LFS storage.         |
| Promotion                  | Freeze both SHAs and apply one server-controlled patch or cherry-pick to the public target. Do not promote a ref.               |
| Scope cache and revocation | Short-lived allow decisions with explicit invalidation. A revoked grant blocks the next protected request.                      |
| Audit integrity            | Write an append-only event with tamper evidence. Define who can read it and what happens when the writer is unavailable.        |
| Secret-bearing runner      | An ephemeral isolated runtime with no host mode or mounted Docker socket. Define the supported implementation before M3.        |

## Source material

- [Git object model](https://git-scm.com/book/en/v2/Git-Internals-Git-Objects.html)
- [Git namespace security guidance](https://git-scm.com/docs/gitnamespaces)
- [Git content filters](https://git-scm.com/docs/gitattributes)
- [Gitea source and MIT license](https://github.com/go-gitea/gitea)
- [Gitea repository permission code](https://github.com/go-gitea/gitea/blob/main/models/perm/access/repo_permission.go)
- [Gitea Git HTTP handler](https://github.com/go-gitea/gitea/blob/main/routers/web/repo/githttp.go)
- [Gitea SSH serve path](https://github.com/go-gitea/gitea/blob/main/routers/private/serv.go)
- [Gitea Actions job-token permissions](https://docs.gitea.com/usage/actions/token-permissions/)
- [Gitea runner security guidance](https://docs.gitea.com/usage/actions/act-runner/)
- [Gitea Actions secrets](https://docs.gitea.com/usage/actions/secrets/)
- [GitHub temporary private fork flow](https://docs.github.com/en/code-security/tutorials/fix-reported-vulnerabilities/collaborate-in-a-fork)
- [GitHub environments](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)
- [GitHub Actions OIDC](https://docs.github.com/en/actions/how-tos/secure-your-work/security-harden-deployments/oidc-in-aws)
- [GitLab CI job tokens](https://docs.gitlab.com/ci/jobs/ci_job_token/)
- [Forgejo runner security](https://forgejo.org/docs/latest/admin/actions/security/)
- [Gerrit private-change pitfalls](https://gerrit-review.googlesource.com/Documentation/intro-user.html)
- [SOPS key management](https://getsops.io/docs/usage/key-management/)
