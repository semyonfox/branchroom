# Think-aloud retest (round two): security lead, embargoed fix on Branchroom staging

Persona: security lead of a small software company. A contractor must fix an embargoed vulnerability without the fix appearing in public history before disclosure. A SOC 2 auditor will read whatever evidence I hand over.

Target: http://10.0.0.5:3402 only. Footer version: 1.28.0+dev-1064-ge9a0b45c18 (round one was dev-1047-g063a5bb6a2). Accounts: ux-lead (repo owner, not a site admin) and ux-contractor. No passwords or tokens were printed or written anywhere. New repository: `ux-lead/embargo-r2-payroll`. The round-one repo `payments-core` was not touched.

Screenshots: a local scratch folder that was not kept. Exports are in the same folder. Helper scripts are in the same folder and read credentials from the credentials file at runtime.

Docs consulted: none. One caveat that matters: I already knew the contractor URL (`/{owner}/{repo}/-/branchroom`) from the round-one report, so I did not have to find it. I tested whether a contractor could find it without that knowledge. They still cannot (see task 3).

Two honest notes about my own run:

1. **I created the repository public by mistake.** The Create Repository form on this instance appears to default to Private, and my click on the checkbox toggled it off, so it was public until I noticed during task 5 (anonymous `curl` returned 200 and anonymous `git ls-remote` listed the branches). I made it private via Settings > Visibility (typed-name confirmation) and re-ran the "collaborator removed" check on the private repo. The result matches round one: both normal and restricted URLs give `Repository not found` for the contractor. The restricted-workspace leak probes do not depend on repo visibility, so I treat them as valid. The public period did sharpen one finding (see N1).
2. A few of my Playwright selectors were wrong on first try (revoke forms moved behind a button, wrong modal). I redid those steps. Nothing below relies on a failed run.

## Summary verdict

Better, and some of it is a real step up: the dialogs work, the wording is human, expiry is shown, the audit evidence now names people, refs, commits and push outcomes, and closing a workspace now warns me. The enforcement on the restricted URL is as good as in round one. I could not read the restricted commit through any normal route.

But I would not run a real embargo on it yet. The two problems that matter most are the same ones as before, only better evidenced:

- The contractor's Write permission on the real repository lets them publish the embargoed commit under any ordinary branch name. Protecting `main` and the new reserved `branchroom/*` prefix do not stop that. I did it three times with a Write collaborator, and the push was accepted every time.
- Neither side has a way in. The contractor is told nothing, and I as the lead cannot see their work without granting myself access.

## Narration per task

### Task 1: private repository, contractor as Write collaborator, protect `main`

- Expectation: stock Gitea.
- Actions: New Repository `embargo-r2-payroll` (see note 1 about visibility), cloned, pushed `src/payslip.js` (SQL built by string concatenation) and `src/tax.js`. Settings > Collaborators: added `ux-contractor`, default permission Write. Settings > Branches: rule `main`, Disable Push.
- Outcome: all worked. Saving the protection rule took a long time to render a screenshot (a 30 second timeout in my script) but the rule was saved.
- Hesitation: the Settings menu entry is still just "Branchroom" with no hint of what it is.
- Screenshots: `04-collab-added.png`, `06-protect-list.png`.

### Task 2: restricted space, minimum access, expiry

- First impression of the page (`07-branchroom-empty.png`): much better. "Create and manage restricted workspaces for this repository." The yellow box now says "Set up a workspace, then grant access to the people who need it. Restricted source is kept separately from the ordinary repository." That reads as reassurance, not as a warning that I am unprotected. "Scope", "policy metadata" and "provisioning" are gone.
- Created `embargo-fix`. The page told me "Creating a workspace lets you see it exists. Set it up and grant clone or push access below." The "below" now points at real forms once I click **Set up workspace**. (Before set-up, only the "see it exists" form is shown.)
- "Set up workspace" says "Copy the current default branch into a separate repository. Grant clone access to let people read it." I understood that. After setting up, I see "Workspace copy ready" and the 40-character SHA. It does not say the copy will not follow `main` later. I would assume it does not, but I am guessing.
- Granting the contractor access is still three separate forms ("Who can see this workspace exists", "Who can clone and fetch", "Who can push to branchroom/* branches"), each needing the username and expiry typed again. I typed `ux-contractor` and `2026-11-20 18:00` three times.
- The fields now say "Optional. Leave expiry empty to use the instance default grant duration. Times are in UTC." Good, that answers the timezone question.
- The push section now carries: "Pushing also needs Write on the ordinary repository. Protect its default branch to prevent accidental public pushes." I had already done that. I took the sentence as "protecting main is enough". It is not (N1).
- After saving, every grant row shows "Expires next week" with Renew and Revoke buttons. This is a real improvement over round one, where I could not tell that my expiry had been saved. The page shows only the relative phrase though. The exact value (`2026-11-20T18:00:00Z`) exists only in the markup. As an auditor-facing lead I would want the date on screen. My own grant defaulted to 3 months (`2027-02-11`).
- Time: about 5 minutes, mostly reading. Success: yes, and this time I could verify what I had configured.
- Screenshots: `07-branchroom-empty.png`, `08-workspace-created.png`, `10-contractor-granted.png`.

### Task 3: as the contractor, find, clone, commit, push, probe

- Expectation: something tells me I have been given a restricted workspace.
- What I saw as the contractor: dashboard, notifications, repo page, no link, no banner, no header tab, nothing mentioning Branchroom or restricted (`c01-dashboard.png`, `c02-notifications.png`, `c03-repo.png`). A link scan of the repo page found no matches for branchroom, restricted or workspace. **Not fixed.** A contractor who was not emailed `/-/branchroom` is exactly as stuck as in round one.
- The page itself (typed in by hand) is much better (`c04-source-access.png`): "Your restricted repositories", plain instructions, "Push only to branchroom/* branches, using the restricted clone URL", a note about tokens, "If a clone URL stops working, the owner rotated it or your access changed. Reload this page for the current URL." The raw translation keys are gone.
- Clone from the restricted URL worked. I committed the fix and pushed:
  - `HEAD:main` to the restricted URL: rejected, `Branchroom receive denied`.
  - `HEAD:fix-sqli` to the restricted URL: same message. The git message still does not say which names are allowed. The page does.
  - `HEAD:branchroom/fix-sqli`: accepted. A second commit (a test) also went through as a fast-forward.
- Mistakes on the normal URL as a contractor would make them:
  - `HEAD:main`: **rejected**, "Not allowed to push to protected branch main". Good.
  - `HEAD:refs/heads/branchroom/leak`: **rejected** with a clear message: "branchroom/* branches are reserved. Use the restricted clone URL from the Branchroom access page." This is a real fix of round one's concern 2.
  - `HEAD:refs/heads/fix-public`: **accepted**. The embargoed fix commit is now on a branch in the public repo. Later I also pushed `after-revoke` and `after-revoke2` the same way, including after all Branchroom grants were revoked. With the repo public (my mistake), anonymous `git ls-remote` listed them (N1).
- Probes for leaks of the restricted-only commit (I pushed a second commit only to the restricted branch so the earlier public push could not muddy the test):
  - `git ls-remote` on the normal URL: no restricted branch. `git fetch origin <sha>` in a fresh clone: `not our ref`.
  - Web as contractor, all 404: `/src/branch/branchroom/fix-sqli`, `/commit/<sha>`, `.patch`, `/raw/commit/<sha>/...`, `/compare/...`, `/commits/branch/...`, `/archive/<sha>.zip`. `/branches` and `/activity` return 200 with no mention.
  - API (basic auth): `/branches` lists only public branches. `/branches/branchroom%2Ffix-sqli`, `/commits?sha=<sha>`, `/git/commits/<sha>`, `/contents/...?ref=branchroom/fix-sqli` all 404. A scan flagged several 404 bodies as "mentioning" the fix, but the only match is the 404 text echoing the id I asked for. Nothing leaked.
- Time: about 6 minutes without the docs hunt. Success: yes, but only because I knew the URL.

### Task 4: as the lead, confirm the push, audit page filtered to the contractor, export

- Confirming the push: **still no way from the normal pages.** My Branchroom settings page shows no branches, no last push. `/-/branchroom` is a 404 for me. To see the work I had to grant myself clone access later (audit seq 56), then clone the restricted URL. **Not fixed.** The audit page is now good enough to confirm it though (below), so this is partly fixed.
- Audit page (`13-audit.png`, `14-audit-filtered.png`, linked as "embargo-fix" under "Audit evidence"): "Audit hash chain intact", filters (Actor, Kind, From, To in UTC), Filter, Export evidence, a note "The export contains the filtered events and their decisions. Chain verification covers the whole workspace. The export itself is recorded after this snapshot."
  - Each push now has an explicit row: `write_source allow receive_accepted` with the ref and old/new commit, or `write_source deny receive_rejected` with the ref, commit and the reason "Branchroom receive policy only allows refs/heads/branchroom/ references". This is the single biggest improvement. My rejected pushes to `main` and `fix-sqli` are now recorded as denials.
  - Actors are usernames. Grant creation rows carry `expires_unix` (1795197600 is 2026-11-20 18:00 UTC, as I typed it), though as raw JSON unix seconds.
  - Each push still produces a plain `write_source allow grant_matched` row first (the permission check) followed by the outcome row. An auditor who counts "allow" rows will overcount writes. I would label the first row differently.
  - Layout: the table is 1657 px wide in a 960 px panel, so the "Target / grant ID" column is off-screen until you scroll sideways. Time cells read "Nov 13, 2026, 08:46:50" with no timezone label, while the filters say UTC.
- Filter to `ux-contractor`: 25 rows. Export: `branchroom-scope-5.jsonl` (51 lines: 25 events, 25 decisions, 1 summary). The filename is still just a scope id.
- Would my auditor accept the export? **Closer, but not yet.**
  - Good: `actor_name` on events, `details` with the ref and both commit ids, `receive_accepted` versus `receive_rejected` with reasons, grant expiry on grant events, a summary with `verified: true`, `head_hash` and the filters used. An unfiltered export's adjacent `previous_hash` values link correctly (I checked linkage across 61 events, I did not recompute any hash).
  - Still missing: `request_id` is empty on every event, there is no client IP, timestamps are bare unix seconds (only `exported_at` is ISO), `expires_unix` on each `decision` line equals the event time, and there is no stated hash algorithm or verify command.
  - New gap I only noticed once the rest was good: grant created, renewed and revoked events carry a `grant_id` but not the **grantee's name**. To learn that grant 23 belonged to the contractor, I had to match it to later read events. An auditor will ask "who was given access, and when" and the file does not answer directly (N3).
  - The `evidence_exported` event has no details, so the filters used in an export are not recorded in the event itself. They are in the file's summary line.
- Anything sensitive in the export? I searched for the clone-URL route key, `_branchroom`, email addresses, IP addresses, "password", "token" and file content from the fix: none present. The export **does** now contain the embargoed branch name (`branchroom/fix-sqli`) and the commit ids. The auditor does need those, but it means the file itself is confidential until disclosure.
- Time: about 6 minutes. Success: yes.

### Task 5: end of engagement: revoke, rotate

- Rotate clone URL: opens a proper modal ("Copied clone URLs stop working once you rotate this URL. People who still need access must get a new URL from Your restricted repositories."). No JavaScript error. Confirmed. The old URL then gave `Repository not found` and the contractor's page showed a new working URL. **Fixed.** I still never see the new URL as the lead, so I cannot hand it over (see N4).
- Revoke: three Revoke buttons (see, clone, push) with no confirmation, one flash message each. Nothing on the page says the contractor is still a Write collaborator. After the third revoke:
  - Restricted URL: `Repository not found`, a fresh clone fails. Good.
  - Normal URL: `git ls-remote` still worked and the contractor pushed another ordinary branch (`after-revoke2`). **Not fixed.** Removing them under Settings > Collaborators (a normal confirmation dialog) was what actually ended their access. I only found that out by testing, because Branchroom is silent about it.
  - I did not click the Revoke next to my own name. It is one click with no confirmation (still not fixed).
- Renewal (new feature): I clicked **Renew** on the contractor's clone grant, expecting a date picker or at least "extend to when?". There is none. One click, flash "Grant renewed.", and the grant now expires **2027-02-11** (now plus 90 days), while their see and push grants still say 2026-11-20. A one-click, unconfirmed jump from one week to three months is not something I want a button to do silently, and the grants for one person now expire on different days (N2). The audit records the renewal (`grant_renewed`, new `expires_unix`), but not the old expiry or the grantee.
- Admin-less expectations. I am the repo owner, not a site admin. `/-/admin` and `/-/admin/users` return 403, as they should. The commit log mentions "let admins revoke a user's access", which I cannot reach and so could not test. As an owner I still have no single "revoke this person" control. The instance default grant duration is not shown to me or changeable by me. I can pick any expiry myself: an explicit `2031-01-01` was accepted for a see grant on a second workspace, so "expire by default" only applies when I leave the field empty. An invalid window (not-before after expiry) is rejected with "The grant time window is invalid or already passed." A duplicate grant is rejected with a sensible message.
- The repo's generic **Audit Log** (Settings > Audit Log) is still empty (`16-repo-audit-log.png`), even though I added a collaborator and a branch rule. Two places called audit, one of which shows nothing.
- Stale-URL attempts: I probed the rotated-out URL (and the revoked URL again). Probes with the **old, rotated-out URL** leave no audit event at all. The probes on the current URL after revocation, collaborator removal and closure do appear (`no_active_grant`, `ordinary_permission_denied`, `workspace_unavailable`). Good for the second group, still a hole for the first.
- Time: about 10 minutes including the renewal and expiry tests. Success: yes.

### Task 6: retire, extract the fix, purge

- Extraction: as in round one the Branchroom UI has no "merge to main". I had to grant myself clone access, find my URL on `/-/branchroom`, clone, check out `branchroom/fix-sqli` and push a normal branch (`embargo-fix-merge`). Pushing straight to `main` was rejected for me too, because I protected `main` against all pushes. That is correct behaviour, but it means the extraction is a PR. I stopped before opening the PR.
- **Close workspace** (was "Retire"): a dialog: "Access ends for everyone, including you. Extract any work you need before closing this workspace. The copy is kept until you delete it permanently." Result: "Workspace closed. The copy is kept." The status badge says "retired". The stale paragraph from round one is gone. **Fixed.**
- **Delete workspace permanently** (was "Purge"): a dialog: "This permanently deletes the workspace and its copied source. Existing clone URLs stop working and this cannot be undone." No typed confirmation. Acceptable for staging, I would want the name typed on a real embargo. It works and the page then says "purged".
- After purge: the audit page still loads, "Audit hash chain intact", a `workspace_purged` event (seq 63) is recorded, and a fresh export still contains all 63 events. That is what I want from an evidence trail.
- Wrinkle: the purged workspace still lists my own active grants with Renew and Revoke, and still offers the "Allow seeing this workspace" and "Allow cloning" forms (`28-purged.png`). Those cannot do anything useful.
- Time: about 5 minutes. Success: yes.

## Round one versus round two

Status key: Fixed, Partly fixed, Not fixed, Not retested.

### Ranked friction points (round-one numbering)

| # | Round-one problem | Status | Evidence |
| --- | --- | --- | --- |
| 1 | Contractor has no way to find the workspace | **Not fixed** | `c01`, `c02`, `c03` show no link, banner, notification or header entry. The page exists and reads well, but only at a hand-typed URL. The lead also is not shown the URL to pass on. |
| 2 | Lead cannot see or confirm the contractor's work | **Partly fixed** | The audit page now confirms each push with ref and commits (seq 24, 29, 49). But `/-/branchroom` is still 404 for the lead and no branch list exists. I had to self-grant clone (seq 56). |
| 3 | Restricted write needs Write on the real repo | **Partly fixed** | Added: banner under the push section, `branchroom/*` reserved on the normal URL, `main` protected. Still possible: push the commit to `fix-public`, `after-revoke`, `after-revoke2` on the normal URL. See N1. |
| 4 | Audit export would not satisfy an auditor | **Partly fixed** | Fixed: usernames, refs, old/new commit, accepted and rejected outcomes with reasons, grant expiry on grant events. Not fixed: empty `request_id`, no IP, unix-only timestamps, no hash algorithm or verify command, filename, `decision` `expires_unix` oddity. New: grantee not named (N3). |
| 5 | Rotate and Purge dialogs broken | **Fixed** | `17-rotate-dialog.png`, `24-close-dialog.png`, `26-purge-dialog.png`. No JS error. Panels are hidden until clicked. |
| 6 | Three grants per person, expiry typed three times and shown nowhere | **Partly fixed** | Expiry is now shown ("Expires next week") with Renew, and times are labelled UTC. Still three forms, three lists. The exact date is not on screen. |
| 7 | No single "revoke this person" | **Not fixed** | Three Revoke buttons. After all three, the contractor still listed the repo and pushed `after-revoke2` on the normal URL. Branchroom does not mention the collaborator. An admin-only revoke exists per the commit log. I cannot test or reach it as an owner. |
| 8 | Retire gives no explanation or confirmation | **Fixed** | `24-close-dialog.png`: "Access ends for everyone, including you. Extract any work you need..." and "The copy is kept". |
| 9 | Raw translation keys on the contractor page | **Fixed** | `c04-source-access.png` shows translated text and a properly labelled URL field. |
| 10 | Contractor not told which branches are allowed | **Partly fixed** | The contractor page now says "Push only to branchroom/* branches". The git rejection is still the terse `Branchroom receive denied`. (The audit reason is clear, the contractor does not see it.) |
| 11 | Expired grants vanish with no "expired" marker | **Not retested** | I did not wait for an expiry this round. |
| 12 | "Resource / grant" shows bare numbers | **Partly fixed** | Actor is now a name. The target is `workspace/<uuid>` or `scope/5`, grant is a bare id, and the table's last column is off-screen at 1280 px (N5). |
| 13 | Many events per Git operation | **Not fixed** | The unfiltered export has 33 `read_source` events for a handful of clones and ls-remotes. A clone is not distinguishable from an ls-remote. |
| 14 | Export filename has only a scope id | **Not fixed** | `branchroom-scope-5.jsonl`. |
| 15 | Generic Audit Log menu is empty | **Not fixed** | `16-repo-audit-log.png`: "There are no audit events matching the filter." |
| 16 | "Add other access grants below" points at nothing | **Fixed** | After set-up the clone and push forms are below, and the intro says "grant clone or push access below". |
| 17 | Datetime fields do not say which timezone | **Fixed** | "Times are in UTC." under each form. The audit table's own Time column still has no label (N5). |
| 18 | Workspace is a snapshot, not stated | **Partly fixed** | "Copy the current default branch into a separate repository" plus the SHA is shown. It still does not say the copy will not follow `main`. |

### Security concerns (round-one numbering)

| # | Round-one concern | Status | Evidence |
| --- | --- | --- | --- |
| S1 | Contractor can push to `main` and read the real repo while holding a restricted grant | **Partly fixed** | `main` protection works (`Not allowed to push to protected branch main`) and the UI now suggests it. The real repo remains readable and writable for any other branch name (N1). |
| S2 | Contractor can push the embargoed fix to the public repo by mistake | **Partly fixed** | `branchroom/*` is now reserved with a good message. Any other branch name is accepted. |
| S3 | Audit overstates writes | **Fixed** | Seq 14 and 19 are `write_source deny receive_rejected`. Caveat: a plain `grant_matched allow` row still precedes each outcome row (N8). |
| S4 | Audit coverage gaps | **Partly fixed** | Grant expiry is now recorded and actors are named. Still no events for rotated-out URLs, none for the contractor viewing the page that discloses the URL, request id empty, no IP. |
| S5 | Purge always one click from execution | **Fixed** | Needs a dialog now. No typed confirmation. |
| S6 | Clone URL is a capability URL | **Unchanged** | Rotation works (`18-rotated.png`, old URL not found). The key still lands in `.git/config` and shell history. |
| S7 | Lead can revoke their own discovery grant | **Not fixed** | Revoke sits beside `ux-lead`, no confirmation. I did not click it. |
| S8 | Retire kills the lead's access too | **Fixed** | Stated in the close dialog. |

### Bugs (round-one numbering)

| # | Round-one bug | Status | Evidence |
| --- | --- | --- | --- |
| B1 | Raw translation keys | **Fixed** | See 9. |
| B2 | Rotate and Purge dialogs do not open | **Fixed** | See 5. |
| B3 | Rejected pushes logged as allowed | **Fixed** | See S3. |
| B4 | Grant expiry not recorded or shown | **Fixed** | Shown in lists (relative) and recorded as `expires_unix` on grant events. Raw unix seconds only. |
| B5 | `expires_unix` on decision lines equals the event time | **Not fixed** | Seq 31: `created_unix` 1794559610, `expires_unix` 1794559610. |
| B6 | Stale helper text after retirement | **Fixed** | Absent in `25-closed.png`. |
| B7 | Stale-URL attempts not audited | **Not fixed** | No event between the rotation (seq 38) and the next real event for probes on the old URL. |

## New findings, ranked

### Blocker

**N1. A Write collaborator can publish the embargoed commit on the normal repo under any ordinary branch name, and the in-product advice does not stop it.**
Path: `/ux-lead/embargo-r2-payroll/settings/branchroom` (text under "Who can push to branchroom/* branches") and `/settings/branches`.
Expectation: after I followed the advice ("Protect its default branch to prevent accidental public pushes"), a contractor with Write could not put the fix in public history.
Got: `main` and `branchroom/*` are blocked. `git push <normal-url> HEAD:refs/heads/fix-public` succeeded, and so did `after-revoke` and `after-revoke2` (the last two after every Branchroom grant was revoked, and `after-revoke` pointed at the restricted-only commit). With my accidentally public repo, anonymous users could list those branches. Revoking in Branchroom does nothing about this because they hold Write through Collaborators.
Possible mitigation I did not test: a branch protection rule with a catch-all pattern such as `**`. The UI does not suggest it.
Screenshots: `10-contractor-granted.png` (the advice), `06-protect-list.png`.

### Major

**N2. Renew silently resets a grant to now plus 90 days.**
Path: `/ux-lead/embargo-r2-payroll/settings/branchroom/scopes/5/grants/23/renew` (the Renew button beside each grant).
Expectation: choose a new end date, or at least see what the new date will be.
Got: one click, "Grant renewed.", no confirmation, no date choice. The contractor's clone grant moved from 2026-11-20 to 2027-02-11 while their see and push grants stayed at 2026-11-20, so one person's access now ends on different days. For a time-boxed embargo that is the wrong default.
Screenshot: `15-after-renew.png`.

**N3. Grant events do not name the grantee.**
Path: `/ux-lead/embargo-r2-payroll/settings/branchroom/scopes/5/audit` and its export (seq 4 to 6, 33, 50 to 52 of the unfiltered export).
Expectation: "ux-lead granted ux-contractor push access until 2026-11-20 18:00 UTC", and a matching revoke.
Got: `manage_policy ... source_write_grant_created` with only a numeric grant id and the expiry. The auditor must infer the grantee from the later `read_source` events that cite the same grant id. Revoke and renew events have the same gap.
Screenshot: `13-audit.png`.

### Minor

**N4. The Branchroom menu entry and the contractor's entry point are still unlabelled.** The Settings menu says only "Branchroom". The lead is never shown the contractor URL or told what to send. (Carry-over of round-one 1 and 2.) Path: `/ux-lead/embargo-r2-payroll/settings/branchroom`.

**N5. Audit table overflows and its time column has no timezone.** The table is 1657 px wide inside a 960 px panel, so the "Target / grant ID" column is off-screen. Time cells show local-looking text ("Nov 13, 2026, 08:46:50") while the filters say UTC. Path: `/ux-lead/embargo-r2-payroll/settings/branchroom/scopes/5/audit`. Screenshot: `14-audit-filtered.png`.

**N6. A purged workspace keeps its grants and forms.** After "purged", the page still lists my active grants with Renew and Revoke and still offers "Allow seeing this workspace" and "Allow cloning and fetching". Path: `/ux-lead/embargo-r2-payroll/settings/branchroom`. Screenshot: `28-purged.png`.

**N7. Expiry is shown only as a relative phrase.** "Expires next week" and "Expires in 3 months". The exact date is only in the page markup. For a lead who will paste this into a ticket or tell an auditor, I want the date and time on screen. Path: `/settings/branchroom`. Screenshot: `10-contractor-granted.png`.

**N8. Two `write_source` rows per push.** A `grant_matched allow` row (the permission check) comes first and an outcome row (`receive_accepted` or `receive_rejected`) follows. Counting "allow write" rows overstates writes. Path: `/settings/branchroom/scopes/5/audit`. Screenshot: `13-audit.png`.

**N9. Nothing on the Branchroom page tells me whether the repo is public.** I ended up with a public repo and a restricted workspace and the page did not say anything. A one-line "This repository is public" note on the Branchroom page would have caught my mistake. This may be as much my own error as a product issue (see note 1).

## Would I now trust this for a real embargo?

**No, not yet.**

What changed my mind from round one: the restricted side is solid. I found no way to read the restricted commit through ls-remote, fetch by SHA, the web UI, the API, archives or raw URLs. Rotation, revocation, closure and purge all take effect and are audited. The dialogs work. Wording is plain. Expiry is visible. The audit now says who did what to which ref and whether it was accepted, and it survives a purge.

Why I still say no:

1. **N1.** The mistake I am most afraid of, the contractor pushing the fix to the real repo, is still one `git push` away with a harmless-looking branch name. The product's own advice ("protect the default branch") gives false comfort, and Branchroom's revoke does not end their Write access. I could only close that gap by removing the collaborator by hand.
2. **Nobody gets in without being told.** The contractor has no link and I have no view of their work. For a real embargo I would be relaying URLs by email and granting myself clone access just to look.
3. **The auditor still cannot be handed this as it stands.** I like the names, refs, commits and outcomes. I do not like grant events without grantees, empty request ids, no IP, no documented hash, and a filename that says "scope-5". I would be annotating the export by hand.

What would flip this to yes: a clear public-leak guard (or an honest warning and a documented catch-all protection rule), one "share with a person" flow that can also remove their ordinary Write, a visible entry point for the contractor and a branch and last-push view for the lead, grantee names in every grant event, and a fixed-date renew.

## What I did not test

SSH, LFS, force push or branch delete on the restricted URL, organisation team grants, a personal access token with 2FA and the new token expiry or repository limits, two contractors, API routes on the restricted URL, recomputing the hash chain, a grant actually expiring (and what the list shows afterwards), not-before in the future with a live contractor, a `**` protection rule, opening the PR after extraction, and the admin-only revoke (I am not a site admin).

## State left on staging

- `ux-lead/embargo-r2-payroll` is now private. `main` is at the lead's initial push and protected against all pushes. `ux-contractor` is no longer a collaborator.
- Public branches left behind: `fix-public`, `after-revoke`, `after-revoke2` (contractor pushes, one pointing at the restricted-only commit) and `embargo-fix-merge` (the lead's legitimate extraction).
- Workspace `embargo-fix` is purged. Its audit trail (63 events) is intact. A second workspace, `cap-test`, remains with a see-grant for `ux-lead` and one for `ux-contractor` expiring 2031-01-01.
- Round-one repo `payments-core` was not touched.
- Local clones and exports are in the `thinkaloud2-lead/` scratchpad folder.
