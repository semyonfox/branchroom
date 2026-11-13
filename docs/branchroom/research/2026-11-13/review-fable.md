# Review: suggested changes (e84a2a45bc) and Branchroom audit page (063a5bb6a2)

Reviewer: Fable 5.2, read-only. Branch `sync/upstream-2026-11`.

Every finding marked "reproduced" was run in this worktree with a temporary test
(unit test in `modules/markup/markdown`, integration tests in `tests/integration`,
SQLite). The temporary files are deleted and `git status` is clean. Timings were taken
while another test run was using the machine, so read them as relative, not absolute.

Summary: authorisation on both features held up under every case I tried. The suggestion
feature has three correctness bugs that will bite in normal use, one of which lets a
read-only reviewer get different content committed than the page shows. The audit page
is safe on access and on leaks but loads the whole chain on every request.

## Findings

### 1. High: the diff shown is not the content that gets applied

`modules/markup/markdown/suggestion.go:18`, `services/repository/files/suggestion.go:30-33`

Rendering finds suggestion fences in Gitea's own markdown pipeline (front matter
extraction, math blocks, extensions). Applying finds them with
`goldmark.DefaultParser()`, which has none of that. The two can disagree about which
fence is "the" suggestion, so a reviewer with read access can write a comment whose
visible diff is harmless while the Apply button commits something else under the
maintainer's name.

Reproduced. A user with no write access posted this comment on line 1 of `diff.txt`:

```
---
a: |
 ```suggestion
 evil()
 ```
b: |
 <pre>
---

Small nit:

```suggestion
benign
```

</pre>
```

Gitea strips the front matter into a collapsed `<details>` and renders `-original`
`+benign`. The default parser sees the `<pre>` as an HTML block that swallows the
benign fence, leaving `evil()` as the only suggestion.

```
page shows '+benign': true; page shows '+evil': false; apply button for comment: true
diff.txt after apply: "evil()\nuntouched\n"
```

A `$$` math block gives a second mismatch the other way round (default parser sees a
suggestion, Gitea renders math).

Fix: one source of truth. Collect suggestion fences from the same parser the renderer
uses (`SpecializedMarkdown` converter after `NormalizeEOL` and `ExtractMetadataBytes`),
or have the renderer emit the replacement and make apply use exactly that. Refuse to
apply when the rendered count and the applied count differ.

### 2. High: suggestions written in the browser put CRLF into LF files

`services/repository/files/suggestion.go:98-102`, `modules/markup/markdown/suggestion.go:22`

The review form is submitted as `FormData` (`web_src/js/features/repo-diff.ts:45`), and
browsers encode textarea newlines as CRLF in multipart bodies. `CreateCodeComment` stores
the content verbatim, `Suggestions` returns the fence body with its `\r\n`, and
`prepareSuggestion` writes it into the file unchanged. The commit's own integration test
misses this because it creates comments through the service with `\n`.

Reproduced through the web route `POST /pulls/N/files/reviews/comments` with the body a
browser would send:

```
stored content: "```suggestion\r\nuno\r\ndos\r\n```"
lf.txt after apply: "uno\r\ndos\r\ntwo\n"
```

So in practice every suggestion authored in the UI leaves mixed line endings.

Fix: normalise the suggestion to `\n`, then re-apply the target line's own ending
(`\n` or `\r\n`) to each replacement line.

### 3. Medium: a suggestion can be applied to the wrong line after a push

`services/repository/files/suggestion.go:94-102`

Staleness is decided only by comparing the text at `comment.Line` in the current head
with the original line from the saved patch. `comment.Line` is never remapped after
pushes, and the existing invalidation (`services/pull/review.go:52`, blame commit of
line N) does not fire when line N is now a different line from the same commit. If the
shifted line has the same text (closing braces, blank lines, `end`, `return nil`), the
replacement lands on the wrong line with no warning.

Reproduced. `dup.txt` was `func a() {\n\tx()\n}\n}\n`, reviewer suggested
`} // end of a` on line 3, author then pushed a commit removing line 2. I also ran
`InvalidateCodeComments` explicitly so this is not a race with the async task.

```
comment invalidated after push: false (line=3)
dup.txt before apply: "func a() {\n}\n}\n"
dup.txt after apply:  "func a() {\n}\n} // end of a\n"
```

The brace that was reviewed is now line 2. The other one was changed.

Fix: anchor to what was reviewed. Cheapest without a schema change is to also require
the new-side context lines stored in `comment.Patch` (up to 4 above the target) to
match the head lines above `comment.Line`. Stronger: record the head commit at comment
time and refuse when the file's blob differs from it.

### 4. Medium: suggestions can never be applied to CRLF files

`modules/markup/markdown/suggestion.go:30-60`, `services/repository/files/suggestion.go:95`

`CutDiffAroundLine` reads the diff with `bufio.Scanner`, which drops the trailing `\r`,
so the saved patch has `one` where the file has `one\r`. `prepareSuggestion` only trims
`\n` before comparing, so the comparison always fails and the button never appears.

Reproduced on a file containing `one\r\ntwo\r\n`:

```
patch: "diff --git a/crlf.txt b/crlf.txt\n--- /dev/null\n+++ b/crlf.txt\n@@ -0,0 +1,2 @@\n+one"
CanApplySuggestion on CRLF file: false
```

A fuzz of `SuggestionOriginal` against real `CutDiffAroundLine` output (150 random
diffs, about 3300 lines per side) found mismatches only on CRLF files. LF files,
missing trailing newlines, multi-hunk diffs and cut hunks all mapped correctly.

Fix: trim `\r` on both sides of the comparison, together with the fix for finding 2.

### 5. Medium: every audit page view and export loads and hashes the whole chain

`routers/web/repo/setting/branchroom_audit.go:116-150`, `:270`, `:303`,
`models/branchroom/audit_view.go:13`

`branchroomAudit` loads every event for the scope, recomputes every SHA-256, resolves
all actors, filters in Go and only then slices out 50 rows. The export additionally
loads every decision and builds the whole response in a `bytes.Buffer` before writing a
byte. Each smart HTTP read request to a workspace appends an event
(`services/branchroom/workspace_access.go:186`), so a scope with regular fetches reaches
six figures within weeks.

Reproduced with a valid chain of 100,004 events (best of 3):

```
audit page 1 (50 rows shown) takes 4.74s
audit page with a filter matching nothing takes 5.03s
export takes 10.75s, 114285356 bytes buffered in memory before the first byte is written
```

Next and Previous each pay the same cost again.

Fix: query the visible page with `WHERE`, `LIMIT` and `OFFSET` in the database, verify
by iterating rows instead of materialising them, cache the verification result keyed by
`(audit_sequence, audit_head_hash)`, and stream the export to the response.

### 6. Low: per-comment git reads on the pull request page

`routers/web/repo/pull_review.go:488-492`, `templates/repo/diff/suggestion.tmpl:5`,
`services/repository/files/suggestion.go:64-90`

`CanApplySuggestion` runs from the template once per code comment. For each comment
holding one suggestion it repeats the permission lookup, the protected-branch lookup,
resolves the branch head and tree entry, and reads the entire blob into memory
(`GetBlobContent` with the blob's full size as the limit) to look at one line.

Measured on the conversation tab as a user with write access, same comments with only
the fence language changed:

```
30 code comments on a small file: plain fences 475ms, suggestion fences 773ms
plus 10 comments on a 5 MB file:  plain fences 744ms, suggestion fences 1.43s
```

About 10 ms per suggestion on a small file and about 70 ms per suggestion on a 5 MB
file, on every page view, with no size cap.

Fix: resolve permission, protection and head commit once per request, read only up to
the target line with a size cap, or show the button to anyone who can push and do the
staleness check on click.

### 7. Low: "verified" ignores the decision rows the export publishes as evidence

`models/branchroom/audit_verify.go:29-47`, `routers/web/repo/setting/branchroom_audit.go:349`

Verification covers `branchroom_audit_event` and the scope head only. The export pairs
each event with its `branchroom_decision` row and prints orphan decisions, but never
compares them. Since every decision is written in the same transaction as its event, an
orphan or a mismatching decision is direct evidence of tampering that the code already
holds and reports as fine.

Reproduced. I rewrote one decision row (`allow` to `deny`), deleted the newest event and
rewound the scope head to its predecessor:

```
after tamper: "Audit chain verified"
export event:    sequence 3, result "allow", reason_code "second_read"
export decision: sequence 3, result "deny",  reason_code "rewritten"
export decision: sequence 0, reason_code "leak_read"   (event deleted)
export summary:  "events":3,"decisions":4,"verified":true
```

Tail truncation with a head rewind cannot be detected from the chain alone, and the
runbook does treat database operators as trusted. The point is narrower: the summary
says verified while its own rows contradict each other.

Fix: during verification also check that each event has a decision with identical
fields and that no decision lacks an event, and report that as a separate
`decisions_consistent` flag (or fold it into `verified`).

### 8. Low, not reproduced: conflicts surface as errors on a busy scope

`routers/web/repo/setting/branchroom_audit.go:275-283`, `:368-383`,
`models/branchroom/audit_view.go:27-46`, `services/branchroom/integrity.go:45`

By reading only. The export fails with a 500 if any event is appended between its
snapshot and its own `RecordDecisionAndAudit`, which has no retry. With a large chain
that window is seconds (see finding 5). `VerifyScopeAuditChain` now also returns
`ErrAuditConflict` after three changed reads, and `CheckIntegrity` reports any error as
"scope N audit chain: ...", so the cron task can raise a system notice for a healthy
chain. Fix: retry the export record, and have `CheckIntegrity` treat `ErrAuditConflict`
as "retry later", not as a chain problem.

### 9. Low, by reading: the evidence page is unavailable when it is most needed

`routers/web/web.go:1211-1231`, `routers/web/repo/setting/branchroom.go:763`,
`docs/branchroom/operations.md` (Incidents, steps 3 and 4)

The runbook says to set `ENABLED = false` to shut everything down, then to read the
audit trail on the settings page. Both `MustEnableBranchroom` and
`requireBranchroomScopeOwner` return 404 while disabled, so the audit page and export
are gone until Branchroom is switched back on. Fix: allow the two read-only audit routes
while disabled, or reorder the runbook.

## Checked and found fine

Suggested changes

- Who can apply: a signed-in user with read access only gets no button, and a direct
  POST leaves the head unchanged (reproduced). The check is the same
  `CanMaintainerWriteToBranch` plus `VerifyBranchProtection` used for web edits, on the
  head repository, so fork heads follow "allow maintainer edits" and protected branches
  and protected file patterns are honoured. `ChangeRepoFiles` checks protection again.
- IDOR: a `comment_id` from another repository returns 404 through
  `/user2/repo2/issues/apply_suggestion` (reproduced). Non-pull issues, non-code
  comments, left-side comments, invalidated or resolved comments, pending reviews, AGit
  pulls, merged or closed pulls, mirrors and archived head repositories are all refused.
- Route: POST only, behind `reqSignIn`, `RepoAssignment`, `RepoMustNotBeArchived` and
  the pulls unit check, with the normal cross-origin protection.
- XSS: the original line and the suggestion go through `highlight.RenderCodeByLexer`
  and come out escaped (`<img onerror>`, `</code></pre><script>` tested). The node
  attribute carrying the original is never written to HTML. Metas are cloned per render
  context, so the original does not leak between comments.
- Head moved: `LastCommitID` is passed to `ChangeRepoFiles`, which rejects a changed
  file and cannot fast-forward a moved branch. A double click fails the second time on
  the text comparison.
- Line mapping against the saved patch: correct for LF files across multi-hunk diffs,
  cut hunks and files without a trailing newline (fuzzed, see finding 4).
- Templates: `and` short-circuits when `CanApplySuggestion` is absent, so routes that
  render the comment templates without it (blob excerpt) do not error.

Audit page and export

- Access: repository admin collaborator who is not the owner gets 404 on both routes
  while `/settings` returns 200, site admin gets 404, anonymous is redirected to sign in,
  another repository's scope id through this repository gets 404, scope ids `0` and
  `abc` get 400 and an unknown id gets 404 (all reproduced). Impersonated sessions are
  refused by `requireBranchroomScopeOwner`.
- Export is POST only and GET returns 405, `Cache-Control: no-store` is set on both.
- Leaks: the export is built from an explicit struct with no route key, store id,
  receive-session field or source content. `resource_id` is `scope/<id>` or
  `workspace/<workspace key>`, which is the logical id and not the route key or store
  id. `request_id` is never populated by any caller today.
- Chain verification itself: modified event fields, missing events and a head mismatch
  are all detected with a non-zero sequence, and the snapshot read retries when an
  append lands mid-read, so a concurrent append does not show as a broken chain.
- Output escaping in `branchroom_audit.tmpl` and the pagination URLs is correct.
