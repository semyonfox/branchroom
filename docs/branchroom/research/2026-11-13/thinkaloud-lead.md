# Think-aloud test: security lead, embargoed fix on Branchroom staging

Persona: security lead of a small software company. A contractor must fix an embargoed vulnerability without the fix appearing in the public repository history before disclosure. I also need evidence for a SOC 2 auditor.

Target: http://10.0.0.5:3402 only (version string in footer: 1.28.0+dev-1047-g063a5bb6a2). Accounts used: ux-lead and ux-contractor. No passwords were printed or written to this file.

Docs consulted: once. I got stuck in task 3 (see below) and grepped `docs/branchroom/implementation-plan.md` for the contractor URL. Everything else was done from the UI and git alone.

Screenshots are in a local scratch folder that was not kept. Exports and clones are in the same folder, under `export/` and `clone-*`.

A note on times. These are wall-clock times from a scripted browser, so they understate how long a person would take to read and decide. Tasks 2 and 3 would take a real lead noticeably longer.

## Summary verdict

The enforcement is good. Every leak route I tried from the contractor's side was closed: ls-remote, fetch by SHA, the web UI, the API, archives, raw files, compare. Revocation, expiry and URL rotation all took effect within seconds. Nothing secret is in the evidence export.

The product around the enforcement is not ready for a security lead to rely on:

1. The evidence file would not satisfy my auditor as it stands (numeric actor ids, no ref, no commit, rejected pushes recorded as `allow`, no grant expiry recorded).
2. The contractor cannot find the feature without being told a hidden URL, and the lead cannot see the contractor's work at all without self-granting.
3. The restricted push path requires the contractor to hold ordinary Write on the real repository, so "nothing more than needed" is not achievable. I proved the contractor can push to `main` on the normal URL.
4. The Rotate and Purge confirmations are broken (JavaScript error, panels sit inline on the page).

## Narration per task

### Task 1: private repository, add the contractor with write access

- Expectation: ordinary Gitea. Create repo, push code, add collaborator.
- First impression: the dashboard says nothing about Branchroom, restricted workspaces or embargoes. I would not know the feature exists from the front page.
- Actions: New Repository, name `payments-core`, private, initialise README. Cloned over HTTPS, pushed `src/charge.js` (with an obvious SQL injection) and `src/refund.js`. Settings > Collaborators, searched `ux-contractor`, Add, left the default permission.
- Outcome: repo created, code pushed, collaborator added with "Write".
- Hesitation: none. This is stock Gitea.
- Time: about 4 minutes including tooling set-up. Success: yes.
- Screenshots: `03-repo-home.png`, `05-collab-added.png`.

### Task 2: find Branchroom, set up a restricted space, give minimum access, set expiry

- Expectation: a clearly named feature in the repo header, something like "Restricted workspace" with a wizard: name it, pick people, set an end date, done.
- Where I found it: Settings, left menu, between "Branches" and "Tags". There is no tab in the repo header. I found it only because I read down the Settings menu. The word "Branchroom" means nothing on its own.
- What I saw (`06-branchroom-settings.png`): "Create and inspect policy scopes for restricted work", a yellow banner "A scope is policy metadata. Provisioning alone does not expose isolated source", an empty "Discoverable scopes" list, and a "Create scope" form.
- Where I hesitated:
  - "Scope" and "policy metadata" are not words I use. I wanted "restricted workspace" or "private branch space".
  - The yellow banner reads like a warning that nothing is protected yet. I did not know if my code was exposed or safe.
  - "Creating a scope grants you discovery access. Add other access grants below." There was nothing below except the Create button.
- Actions: created scope `embargo-fix`. The page then showed a button "Provision isolated workspace" with the text "Copy the current default-branch commit into an isolated store. Direct source authorization controls restricted Git access."
  - I did not understand "isolated store" or "direct source authorization". I assumed provisioning was required and clicked it.
  - After provisioning: "Isolated workspace: ready" with a raw 40-character SHA, and new forms for source read and source write appeared.
- Giving the contractor access took three separate forms, each needing the same username and expiry typed again:
  1. "Direct discovery grants" (I guessed this means "can see it exists")
  2. "Direct source-read grants" (can clone)
  3. "Direct source-write grants" (can push). The paragraph under this heading is the only place that tells me the real rules: it needs source-read plus ordinary repository code-write permission, only fast-forward pushes to `refs/heads/branchroom/*`, 1 MiB and 45 second caps, no SSH, LFS, tags or deletes.
- Expiry: each form has "Not before" and "Expires" datetime fields with the hint "Leave both fields empty to grant access without a time window". I set 2026-11-20 18:00 on all three.
  - The fields do not say which timezone they use. The audit page says "(UTC)" but these do not.
  - After saving, the grant lists show only "ux-contractor [Revoke]". The expiry is not displayed anywhere. I could not confirm that my expiry had been saved. (It had. I proved enforcement in task 5.)
- Trust: I did not trust the UI here. I had to infer the grant model, I could not see what I had configured, and I had no clone URL to hand over. I also noticed that as the creator I had only discovery access, so I could not see or read the restricted code myself.
- Time: about 6 minutes of exploration for 30 seconds of clicking. Success: partial. Configured, but I could not verify it from the UI and I would not have known what to tell the contractor.
- Screenshots: `07-scope-created.png`, `08-provisioned.png`, `09-grant-2-source-write-grants.png`.

### Task 3: as the contractor, find, clone, commit, push, then probe

- Expectation: an email or notification, or a visible entry in the repo, saying "you have been given a restricted workspace, clone here".
- What I saw: the dashboard lists `ux-lead/payments-core` as an ordinary repo. Notifications are empty. The repo header shows no Branchroom entry. Settings is a 404 (contractor does not have it). No links on the repo page, the branches page, the activity page, the pull requests page or Explore mention Branchroom, restricted or workspace.
- I tried guessing URLs: `/ux-lead/payments-core/branchroom`, `/branchroom`, `/user/settings/branchroom`, `/-/branchroom` (the last one I tried only as a guess, and it 404'd before I knew the real path was `/ux-lead/payments-core/-/branchroom`). All 404.
- **Stuck. Consulted docs.** `grep` of `docs/branchroom/implementation-plan.md` showed the page is `/{owner}/{repository}/-/branchroom`. A real contractor would only know this if I emailed it to them.
- What I saw there (`c04-source-access.png`): title "Branchroom source access", explanatory text, a yellow note about Git prompting for username and password (and to use a personal access token with `read:repository` or `write:repository` if 2FA is on), then the clone URL in a field with "Copy URL". Two lines show raw translation keys (see Bugs).
- Clone: worked first time, about 4 seconds. The clone has branch `main` at the snapshot commit and the full history to that point (2 commits).
- I made the fix and committed. Then I tried pushes as an ordinary developer would:
  - `git push origin HEAD:fix-sqli` (a normal feature branch name): rejected, `remote: Command error: Branchroom receive denied` and `pre-receive hook declined`.
  - `git push origin main`: rejected with the same message.
  - `git push origin HEAD:refs/heads/branchroom/fix-sqli`: accepted.
  - The error does not say which branch names are allowed. The rule is shown only to the lead on the settings page. The contractor page never mentions `branchroom/`. A contractor would have to guess.
- Probing the restricted branch through normal routes, as the contractor (fresh clone of the normal URL, and a logged-in browser session, and basic-auth API calls):
  - `git ls-remote` on the normal URL: only `main` and `HEAD`.
  - `git fetch origin <fix sha>` in a fresh clone: `fatal: remote error: upload-pack: not our ref`. (My first attempt appeared to succeed. That was a false positive because the object was already in my local clone. I redid it in a fresh clone and recorded the real result.)
  - Web UI, all 404: `/src/branch/branchroom/fix-sqli`, `/commit/<sha>`, `/commit/<sha>.patch`, `/raw/commit/<sha>/...`, `/raw/branch/...`, `/compare/main...branchroom/fix-sqli`, `/commits/branch/...`, `/archive/<branch>.zip`, `/archive/<sha>.zip`. `/branches`, `/activity`, `/graph` return 200 and contain no mention of the branch or commit. Code search for text from the fix: the only hit was my query echoed in the search box. Nothing from the fix leaked.
  - API with basic auth: `/repos/.../branches` lists only `main`. `/branches/branchroom%2Ffix-sqli`, `/commits?sha=<sha>`, `/git/commits/<sha>`, `/contents/...?ref=branchroom/fix-sqli`, `/raw/...?ref=<sha>` all 404 with "object does not exist" style messages. (API calls with only a session cookie return 404 for every endpoint including the base repo. I did not investigate that, I treated it as ordinary Gitea behaviour and redid the checks with basic auth.)
- **Push to main on the normal URL: this succeeded.** `git push origin main` against `http://10.0.0.5:3402/ux-lead/payments-core.git` as the contractor went through (3a79a11..5c45905). A push of `HEAD:refs/heads/branchroom/probe` to the same normal URL also succeeded and created a public branch with the restricted prefix. This is expected for a Write collaborator, but see Security concerns.
- Time: about 10 minutes, most of it hunting for the page and the docs lookup. Success: **no without the docs**, yes with them.
- Screenshots: `c02-repo.png`, `c03-settings-branchroom.png`, `c04-source-access.png`.

### Task 4: as the lead, confirm the push, open the evidence page, filter to the contractor, export

- Expectation: a list of restricted branches and commits so I can see the contractor's push, then an audit page.
- Confirming the push: I could not. My repo page shows 3 commits and 2 branches, but that is the contractor's probe push to the normal URL, not the restricted fix. `/-/branchroom` returns 404 for me, because creating the scope gave me discovery only. To see the fix I had to give myself a source-read grant (done later, nothing in the UI suggested it). The audit page was my only proof the push happened, and it is ambiguous (see below).
- Audit page (`/ux-lead/payments-core/settings/branchroom/scopes/4/audit`, linked only as the scope name under "Audit evidence" on the Branchroom page): green banner "Audit chain verified", filters for actor, kind, from and to date (UTC), a Filter button, an Export evidence button, then a table.
  - I liked the banner and the filters. I did not know what "chain verified" proves or how to check it myself.
  - The table has a "Resource / grant" column where grants show as a bare number (16, 17). No label, no username.
  - Reading the table I see three `write_source allow` rows. I made three push attempts, but two of them were rejected by the server. The audit says all three were allowed. The log records the permission decision, not the result of the push. An auditor would read this as three successful writes.
  - The table times have no timezone label. They matched UTC.
- Filter and export: typed `ux-contractor`, Filter (17 rows), Export evidence. The export form carries the filter in hidden fields and the file matched the filter. Download name: `branchroom-scope-4.jsonl`.
- The export file (`export/branchroom-scope-4.jsonl`, 21 KB, 35 lines):
  - 17 `event` lines, 17 `decision` lines (the same facts again, with `decision_key` instead of `event_key`), and 1 `summary` line.
  - Event fields: `sequence`, `previous_hash`, `event_hash`, `scope_id`, `repo_id`, `grant_id`, `actor_type`, `actor_id`, `action`, `resource_type`, `resource_id`, `surface`, `result`, `reason_code`, `ordinary_allowed`, `request_id`, `created_unix`, `expires_unix`.
  - Summary has repository name, filters used, `exported_at` (ISO), first and last event times, `head_hash`, `verified: true`, `broken_sequence: 0`.
- Would my auditor accept it? **Not as it stands.**
  - Actor is `actor_id: 9`. No username anywhere in the event lines. The auditor cannot map 9 to a person without database access. (The username only appears in the summary's filter, and only because I filtered.)
  - No ref name, commit SHA or push outcome on write events. I cannot show what was pushed.
  - Rejected pushes appear as `allow`.
  - `request_id` is empty on all 17 events. No client IP.
  - Timestamps are bare unix seconds (the ISO form is only on `exported_at`).
  - `expires_unix` is 0 on events. On decision lines it equals the event time, so it does not mean what the name suggests.
  - Unfiltered export, grant-created events: `expires_unix: 0`. The expiry I set (2026-11-20 18:00 UTC would be 1795197600) is not in the file at all. I cannot prove from evidence that access was time-limited.
  - The file says `verified: true` but not how the hash is computed. I did not try to recompute it. Without a documented algorithm or a verify command, the auditor has to take the server's word.
  - Filename has a scope id, not the repo, scope name or date.
  - On the positive side, an export is itself recorded (`evidence_exported`), though the filters used are not in that event.
- Anything sensitive in it? I searched for the clone-URL route key, `_branchroom` paths, the branch name `fix-sqli`, the commit SHAs, email addresses, IP addresses and password or token words. **None present.** It does contain the workspace UUID, repo id, numeric user ids and hashes, which I consider low risk.
- Time: about 6 minutes. Success: export obtained, but I could not confirm the push from the lead side and the evidence is not audit-ready.
- Screenshots: `l03-audit.png`, `l04-filtered.png`.

### Task 5: end of engagement, revoke the contractor and rotate the clone URL

- Rotate: Branchroom page, "Rotate clone URL". Clicking it shows a red banner `JavaScript error: e.show is not a function` and nothing opens (`11-rotate-click.png`). The explanation panel and its "Cancel / Rotate clone URL" buttons are already visible on the page, so I clicked the inline confirm button. It worked ("Branchroom clone URL rotated."). The message does not give me a new URL and I cannot see one, so I would have to tell the contractor to look at their page.
  - Contractor result: the old URL gives `Repository not found` (good, it does not confirm the repo exists). Their page showed a new URL (route key changed) and `ls-remote` on it worked, with the fix branch intact.
- Revoke: three separate Revoke buttons (discovery, source-read, source-write), one per grant list, no confirmation, no single "remove this person". I clicked each. Each flashed "revoked".
  - Contractor result: `ls-remote`, fresh clone and push against the rotated URL all fail with `Repository not found`. `/-/branchroom` is a 404.
- **Not finished: the contractor still had full access to the normal repo.** After the Branchroom revokes, `git ls-remote` on the normal URL still worked for them, and they could still push to `main`. I removed them under Settings > Collaborators (a normal, working dialog). After that the repo is a 404 for them on both web and git.
  - Branchroom does not tell me that revoking its grants leaves the collaborator in place.
- Extra test, expiry enforcement: I tried an expiry in the past, which is rejected with "The grant time window is invalid or already passed." Then I granted discovery and read with expiry `08:01` (set at 07:59). The contractor's access worked until 08:01:00 UTC and failed by 08:01:02. Enforcement is good. The expired grants then disappear from the lists with no "expired" row, so there is no trace in the UI that they ever existed.
- Audit after all this:
  - Denied reads are recorded after revocation and after expiry (`read_source deny no_active_grant`).
  - Attempts against the old, rotated-out URL leave no audit events at all. If a leaked old URL were probed, I would see nothing.
  - The contractor viewing the source-access page (where the URL is shown) did not create an event that I could see.
  - The rotation event records who rotated, not which key.
- Time: about 8 minutes including the extra expiry test. Success: yes, but only by working around the dialog and removing the collaborator separately.
- Screenshots: `11-rotate-click.png`, `l06-after-revoke.png`, `c05-after-revoke.png`, `l07-remove-collab-dialog.png`.

### Task 6: retire the workspace

- Before retiring, I worried about losing the fix, since it only exists in the restricted store and I could not see it. I granted myself source-read, found the clone URL on `/-/branchroom`, cloned, and cherry-picked the contractor's commit onto the real `main` (authorship kept) and pushed. Nothing in Branchroom's UI offers this step. There is no "merge to main" or "export to normal repo".
  - The restricted workspace was a snapshot taken at provisioning (SHA shown on the page). By the time I did this, public `main` had moved (the contractor's probe push), so a cherry-pick was needed, not a fast-forward.
- Retire: button "Retire workspace" with no help text and no confirmation. One click: "Branchroom workspace retired." Status changes to "retired", the stale text "A ready workspace remains hidden until..." stays, and "Purge workspace" is still offered.
  - After retire, my own restricted URL gives `Repository not found` and `/-/branchroom` is a 404, so access is gone for everyone. The audit log recorded `workspace_retired` and a later denial `workspace_unavailable`.
  - I could not tell from the UI what the difference between Retire and Purge is. The Purge text says it "permanently deletes the isolated store", which I took to mean Retire keeps the data but hides it. The UI never says that.
- Time: about 3 minutes, plus the hand-extraction. Success: yes.
- Screenshot: `l10-retired.png`.

## Ranked friction points

### Blockers

1. **The contractor has no way to find the restricted workspace.** URL `/ux-lead/payments-core/-/branchroom`. Expected: a notification, an email, a link on the repo page, or the URL shown to me after granting so I can pass it on. Got: nothing. Every route I guessed was a 404. Screenshot: `c02-repo.png`, `c03-settings-branchroom.png`.
2. **The lead cannot see or confirm the contractor's work.** URL `/ux-lead/payments-core/-/branchroom` returns 404 for the lead. Expected: a list of restricted branches and last push for each scope on my settings page. Got: only the audit table, which says `allow` even for rejected pushes. Screenshot: `l02-lead-source-access.png`, `l03-audit.png`.

### Major

3. **Restricted write needs ordinary Write on the real repo.** URL `/ux-lead/payments-core/settings/branchroom` (text under "Direct source-write grants") and `/settings/collaboration`. Expected: the contractor has nothing on the real repo. Got: they must hold Write, and I showed they could push `main` (5c45905) and create `branchroom/probe` on the normal URL. The UI never warns me. Screenshot: `09-grant-2-source-write-grants.png`.
4. **The audit export would not satisfy an auditor.** URL `/settings/branchroom/scopes/4/audit/export`. Expected: usernames, refs, commit SHAs, push outcomes, grant expiry, IP or request id. Got: numeric actor ids, rejected pushes as `allow`, expiry missing, request id empty. See task 4.
5. **Rotate and Purge confirmations are broken.** URL `/settings/branchroom` after provisioning. Expected: a dialog. Got: `JavaScript error: e.show is not a function` and the confirm panels rendered inline, so "Purge workspace" is permanently visible one click from irreversible deletion. Screenshot: `11-rotate-click.png`.
6. **Three grants per person, expiry typed three times and shown nowhere.** URL `/settings/branchroom`. Expected: one form (person, access level, end date) and a visible list with expiry. Got: three forms and lists with no dates. Screenshot: `09-grant-2-source-write-grants.png`.
7. **No single "revoke this person".** Revoking the three grants leaves the contractor with full read and write on the real repo until they are removed under Collaborators. Screenshot: `l06-after-revoke.png`.
8. **Retire gives no explanation and no confirmation.** URL `/settings/branchroom`. It silently kills all access, including mine, with no warning that the fix must be extracted first. Screenshot: `l10-retired.png`.

### Minor

9. Raw translation keys on the contractor page (see Bugs).
10. The contractor is not told which branch names they may push. Only `Branchroom receive denied` comes back. URL `/-/branchroom`.
11. Grant lists after expiry drop the person with no "expired" marker.
12. The audit table's "Resource / grant" column shows bare numbers (16, 17).
13. Many events per Git operation (14 `read_source` for one clone, a few ls-remotes and three pushes). A clone is not distinguishable from an ls-remote.
14. The export filename `branchroom-scope-4.jsonl` has no repo, scope name or date.
15. The generic "Audit Log" menu entry (`/settings/audit_logs`) has an IP column but is empty and shows none of the Branchroom, collaborator or push events. Two places called audit, one of which is empty.
16. "Add other access grants below" on the first Branchroom screen points at nothing.
17. The datetime fields on grants do not say which timezone they use. The test machine is GMT, so I could not tell UTC from browser-local.
18. The provisioned workspace is a snapshot. The page shows its SHA but does not say it will not follow `main` afterwards.

## Confusing terminology and suggested wording

| Current | What I thought | Suggested |
| --- | --- | --- |
| Branchroom | A product name, not a function | Keep as brand but label the menu entry "Branchroom (restricted workspaces)" |
| Scope | Permission scope, like OAuth | "Restricted workspace" |
| Policy metadata / "Provisioning alone does not expose isolated source" | A warning that I am not protected yet | "Nothing is shared until you give someone access." |
| Provision isolated workspace | Unclear what it does to my code | "Create private copy of main (snapshot at <sha>)" |
| Isolated store | Storage backend jargon | "private copy" |
| Direct discovery grant | Unknown | "Can see this workspace exists" |
| Direct source-read grant | Unknown | "Can clone" |
| Direct source-write grant | Unknown | "Can push to `branchroom/*` branches" |
| Retire workspace | Is it deleted? | "Close workspace (hides it, keeps the copy)" |
| Purge workspace | Fine but destructive | "Delete workspace permanently" |
| Rotate clone URL | Fine | "Replace clone URL (old one stops working)" |
| Branchroom source access | Page name for contractors | "Your restricted repositories" |
| Audit chain verified | Verified by whom, against what? | "Hash chain intact (head <short hash>). How to verify" |
| Resource / grant | Column with a number | "Target / grant id" with the grant owner and level named |

## Security concerns

Nothing I tried let the contractor read the restricted branch or commit through the normal clone URL, web UI or API. The concerns are about what is possible by design or by omission:

1. **Contractor can push to `main` and read all branches of the real repo while holding a restricted grant.** Verified. Restricted writes require ordinary Write, so least privilege cannot be reached with Branchroom alone. Branch protection on `main` would be the mitigation, and I did not test it. The UI does not say so.
2. **A contractor can push to the public repo by mistake.** I pushed `HEAD:refs/heads/branchroom/probe` to the normal URL and it was accepted. The restricted prefix is not reserved there. A contractor with two remotes, or one who types `origin` in the wrong clone, could publish the embargoed fix in the public repository. Reserving the prefix on the normal URL would stop this.
3. **Audit overstates writes.** Two rejected pushes were logged as `write_source allow`. A false "allow" in SOC 2 evidence is worse than a missing row.
4. **Gaps in audit coverage.** Requests with a rotated-out URL leave no event. Viewing the page that discloses the clone URL left no event that I could see. Grant expiry is not recorded. Actor ids are not names.
5. **Purge is always one click from execution** because the dialog bug leaves its confirm button visible on the page.
6. **The clone URL is a capability URL.** The route key sits in the path and lands in `.git/config` and shell history. Rotation covers this, and it worked.
7. **Lead can revoke their own discovery grant** (a Revoke button sits next to `ux-lead`). I did not click it, but it looks like an easy way to lock myself out.
8. **Retire kills the lead's access too.** If the fix has not been extracted first it is gone from every UI path.

Nothing sensitive (secrets, route key, branch names, SHAs, emails, IPs) is present in the evidence export.

## Bugs with reproduction

1. **Raw translation keys on the contractor page.**
   Repro: as a user with discovery and source-read on a ready workspace, open `/ux-lead/payments-core/-/branchroom`.
   Actual: the line above the scope name reads `repo.branchroom.recovery` and the line above the URL field reads `repo.branchroom.clone_url%!(EXTRA string=embargo-fix)`.
   Expected: translated text. The second one also shows a Go format error, so the key has no placeholder. Screenshot: `c04-source-access.png`.
2. **Rotate and Purge dialogs do not open.**
   Repro: as repo owner with a ready workspace, open `/ux-lead/payments-core/settings/branchroom`, click "Rotate clone URL".
   Actual: a red banner `JavaScript error: e.show is not a function`. Both confirm panels (`#branchroom-workspace-purge-modal-4`, `#branchroom-route-key-rotate-modal-4`, class `ui g-modal-confirm modal`) are rendered `display:block; position:static` inline in the page. The normal "Remove Collaborator" dialog on `/settings/collaboration` works, so it is specific to these panels. Screenshot: `11-rotate-click.png`, `10-modals-check.png`.
3. **Rejected pushes are logged as allowed.**
   Repro: as a contractor with active read and write grants, `git push <restricted-url> HEAD:refs/heads/fix-sqli` (rejected with `Branchroom receive denied`). Open the scope's audit page.
   Actual: `write_source allow grant_matched` for that attempt. Expected: a denied or rejected outcome with the ref. Screenshot: `l03-audit.png`.
4. **Grant expiry is not recorded or shown.**
   Repro: grant any user source-read with Expires set. Look at the grant lists on `/settings/branchroom`, and export evidence.
   Actual: no expiry in the list, `expires_unix: 0` on the `*_grant_created` events, and the timestamp is absent from the file. Expected: expiry visible in both places. (Enforcement itself works.)
5. **`expires_unix` on decision lines equals the event time.**
   Repro: export evidence and compare `created_unix` and `expires_unix` on a `decision` line (for example sequence 22: both 1794556391). Expected: either the grant's expiry or 0. From one sample line only.
6. **Stale helper text after retirement.**
   Repro: click Retire on a ready workspace. The status flips to "retired" but the paragraph "A ready workspace remains hidden until a direct user passes the restricted Git HTTP policy check." remains. Screenshot: `l10-retired.png`.
7. **Stale-URL attempts are not audited.**
   Repro: rotate the clone URL, then `git ls-remote <old-url>`. No event appears in the audit between the rotation event and the next real event. Expected: a denial event or at least a counter.

## Three changes that would most raise trust

1. **Make the evidence self-contained and truthful.** Put the username (not only the id), the ref, old and new commit SHA, and the push outcome (accepted, rejected and why) in each event. Record the grant's expiry and not-before on grant events. Fill `request_id` and add client IP. Use ISO timestamps. Name the file `<repo>-<scope-name>-<date>.jsonl`. Document how `event_hash` is computed and ship a one-line verify command, so the auditor does not have to take "chain verified" on trust. Log denials for stale and rotated URLs too.
2. **Replace the three grant forms with one "Share with a person" flow and give both sides a way in.** One form (person, can read, can push, end date and timezone), a list that shows who has what and until when, one "Revoke person" that also offers to remove the collaborator, and a banner explaining that pushing needs Write on the real repository (and suggesting branch protection on `main`). Show the clone URL and the exact instructions after granting so the lead can pass them on, add a visible entry on the repo page for people who have access, and show the lead the restricted branches and latest commits. Reserve the `branchroom/*` prefix on the normal URL.
3. **Fix and clarify the destructive controls.** Make the Rotate and Purge dialogs work and hidden. Put a confirmation on Retire that says what it does ("access ends for everyone, copy kept, extract your fix first") and explain Retire versus Purge. Rewrite the jargon (scope, provisioning, discovery, isolated store, policy metadata) in plain words as in the terminology table.

## What I did not test

SSH, LFS, force-push or branch delete on the restricted URL, organisation team grants, multiple scopes in one repo, branch protection on `main`, the not-before field, a second contractor, and any API route on the restricted URL. I did not try to recompute the hash chain.

## State left on staging

`ux-lead/payments-core` still has the contractor's probe commit on `main`, a public `branchroom/probe` branch and the cherry-picked fix on `main` (8724747). The scope `embargo-fix` is retired, not purged. The contractor is no longer a collaborator. Local clones and exports are under the `thinkaloud-lead/` scratchpad folder. Helper scripts in `tools/` read passwords from the credentials file at runtime and do not store them.
