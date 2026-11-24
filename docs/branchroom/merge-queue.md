# Merge queue design

This proposal covers merge commits on protected branches.

## What the queue guarantees

A protected branch can opt into a queue. Each concrete target branch has its own
FIFO order, even when several branches match one protection rule. Ordinary merges
must enter that queue. Existing explicit administrator overrides remain available,
but invalidate affected queue tests. A direct push to the target also invalidates
tests that used its previous tip.

Given target commit B and queued heads A, C and D, prepare immutable commits:

```text
M1 = merge(B, A)
M2 = merge(M1, C)
M3 = merge(M2, D)
```

Run the effective required status checks on each prefix commit. A green PR head
alone never satisfies queue checks. Publish M1, then M2, then M3, only after each
commit's own checks pass and all preceding entries have landed. Later tests may
run while earlier entries wait, but cannot change merge order.

If M2 fails after M1 passes, remove C and rebuild D on M1. Its previous checks
no longer count. If an earlier prefix is still pending, wait for its result before
attributing a later failure. An earlier failure invalidates downstream results
without blaming those PRs. Conflicts follow the same ordering. Missing checks
remain pending until the configured timeout, which removes the affected entry.
Infrastructure errors pause and retry without blaming a PR. Users can explicitly
re-enqueue a removed PR at the tail.

Initially support merge commits only. Squash and rebase need their final commits
prepared before testing too. Disabling the queue stops publication, removes its
temporary refs and cancels queued intent. It must not silently fall back to
ordinary auto-merge. Require at least one effective required check when enabling.

## Reuse, and the missing pieces

Reuse scheduling, cancellation, comments and retained actor credentials from
`services/automerge` and `models/pull/automerge.go`. Recheck the actor's current
permission and token restrictions before publishing each entry. Keep ordinary
auto-merge unchanged on branches that have not opted in.

Reuse `git.MergeTree` and commit construction from
`services/pull/merge_ref.go`, with separate queue refs. Existing
`refs/pull/<index>/merge` refs describe one PR and must remain independent.

Reuse `EffectiveRequiredContexts`, latest commit-status lookup and
`MergeRequiredContextsCommitStatus`. The notifier in
`services/automerge/notify.go` currently reacts only to successful statuses and
finds PRs by head SHA. Extend dispatch to queue candidate SHAs and all terminal
states, including failures. Duplicate and delayed callbacks must be harmless.

`pull.Merge` currently builds a new commit and guards the expected PR head. A
queue needs a prepared-commit publication path that also guards the expected
target tip. Checking a synthetic commit, then calling ordinary `Merge`, would
publish a different commit and break the guarantee. Reuse merge authorisation,
receive hooks, merge intent, issue updates, notifications and branch deletion.
Do not replace that flow with a raw ref update that omits those effects.

## Persisted state and recovery

Store opt-in policy, ordered entries and candidate generations in `branchroom_*`
tables through the next available Branchroom migration. Do not reserve a migration
number until rebasing onto current main. Keep actor credentials in the existing
scheduled-merge record, rather than copying token material into queue rows.

Each entry records its PR, expected head, order, state, candidate SHA, expected
parent SHA, generation and last result. A generation also records the effective
check policy. Use database claims with expiring leases to permit only one active
publisher per target branch across processes. The existing unique work queue is
for wake-ups, not the durable order or the publisher lock.

Use fresh candidate SHAs for rebuilt generations, so late statuses cannot approve
a replacement attempt. Publish each candidate under
`refs/merge-queue/<branch-key>/<generation>/<position>`. Reject user pushes to
that namespace. Delete abandoned refs after cancellation and retain a bounded
history of outcomes without retaining credentials.

At publication, recheck reviews, path approvals, dependencies, permissions,
required-check policy and the exact PR head. Compare and swap the target from the
recorded parent to the tested SHA. A target mismatch rebuilds affected candidates.
A changed or force-pushed PR head cancels its scheduled intent, following the
existing force-push protection, and rebuilds later entries.

Git and the database cannot share a transaction. Record publication intent before
moving the ref. After a crash, compare that intent with the target and commit
ancestry, finish missing merge bookkeeping idempotently, or rebuild without
publishing twice. Reconcile every unfinished entry on startup, not just the
existing auto-merge lookback of 24 hours.

## Running CI on the right commit

Creating a ref does not dispatch CI. Add a queue candidate event carrying the
exact SHA, target, generation and ordered PR list. Actions and external webhook
consumers must check out that SHA and report required contexts against it.
Validate CI support before enabling a branch, otherwise entries can wait forever
for checks that never start.

Use workflow definitions from the trusted target branch, with the same restricted
permissions and secret access as PR CI. Publishing a server-owned ref must not
turn an untrusted fork's workflow into a trusted push workflow. Reuse existing
status authentication and context naming, without granting extra status-writing
rights to a queue participant. Cancel superseded runs where supported and ignore
callbacks for inactive generations.

## Delivery and acceptance

Split delivery into three bounded changes, with opt-in unavailable until all land:

1. Durable policy and queue state, scheduling integration, cancellation, leases
   and restart reconciliation. Include migrations and API configuration.
2. Prefix commit construction, reserved refs, CI dispatch and status correlation.
   Include Actions permissions and external-CI guidance.
3. Publication of the tested commit through the merge lifecycle, target-tip
   concurrency guards, failure recovery, readiness blockers and operational docs.

The final integration suite must prove:

- Two individually green heads whose combination fails never both land.
- With A, C and D queued, failure of C removes it and retests D without C.
- A late success for an abandoned generation cannot publish anything.
- Target movement, changed heads, revoked tokens and lost approvals prevent
  publication until current conditions are satisfied again.
- Concurrent workers and a restart after the Git update cannot merge twice or
  leave the issue claiming an unpublished commit merged.

Run formatting, scoped lint, vet, unit tests, main-path and denial integration
tests, and migration tests for each implementation PR. Use real Git repositories
and synthetic commit statuses. No live deployment is needed.

## Prior art

[GitHub merge queues](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/configuring-pull-request-merges/managing-a-merge-queue)
require checks for queued combinations and explicit CI support for merge groups.
[GitLab merge trains](https://docs.gitlab.com/ci/pipelines/merge_trains/)
test each prefix and rebuild affected later entries after removal.
[gitea-mq](https://github.com/Mic92/gitea-mq) is relevant external prior art for
serialising Gitea merges. These sources support the approach, rather than prove
that this repository already has the required publication or CI interfaces.

Conversation-resolution gates, stored secret findings and restricted-workspace
CI remain outside this design.
