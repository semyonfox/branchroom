# Where companies struggle with source control and code hosting, 2025 to 2026

Research lane for Branchroom, written 13 November 2026. Read-only: no repository file was
changed.

## How to read this

- Ranking is by how many organisations hit the pain and what it costs them when they do. It
  is not ranked by how well Branchroom fits. Fit is covered in each entry and in the picks
  at the end.
- Vote and comment counts were read from the GitHub and GitLab APIs on 13 November 2026.
- "Gitea today" was checked against this worktree (upstream main, November 2026), not
  against memory. Where a claim comes from the tree it says so.
- Effort: S is days, M is a few weeks, L is a milestone of its own.
- Evidence strength varies. Incident write-ups and vendor docs are solid. Vendor surveys
  (Faros) are telemetry from paying customers. Path-level permission requests have low vote
  counts on every tracker, so that demand is real but diffuse. I say so where it matters.

What Branchroom has today, from `docs/branchroom/`: restricted workspaces in a separate
object store, per-user `discover`, `read_source` and `write_source` grants with optional
not-before and expiry times, team read grants, rotatable clone URLs, a hash-chained
decision and audit log verified by a doctor check and a six-hourly cron task, and a backup
and restore drill. Not there yet: an audit viewer, audit retention, restricted pull
requests, CI, publication to the public repository, SSH, LFS, rate limits. Site
administrators are trusted.

## Top 10 pains

### 1. One credential opens everything its owner can reach

The most common and most expensive failure of the last two years. Tokens are scoped to a
person or an organisation, not to a piece of work, so one stolen token is every repository.

- Gheist, October 2026: stolen GitHub tokens used to walk every reachable repository, clone
  private ones and dump secrets through planted workflows. Wiz recovered 13,500+ planted
  branches from 850+ accounts. GitHub's hold on suspicious workflow runs does not cover
  private repositories or GitHub Enterprise Server.
  https://www.helpnetsecurity.com/2026/10/27/gheist-github-token-abuse/
- wfx-loot, November 2026 (still running): 4,837 compromised accounts opened probe pull
  requests in 61,764 repositories. 10,431 of those were repositories the account did not
  own, reached through collaborator or organisation access.
  https://www.helpnetsecurity.com/2026/11/12/wfx-loot-github-actions-secret-stealer/
- GitHub's own fine-grained PAT feedback thread (+147, 227 comments) is still open four
  years on. The top comment (+102) asks for non-expiring CI tokens, and outside
  collaborators still need classic tokens with the broad `repo` scope.
  https://github.com/orgs/community/discussions/36441

Agents make this worse. Invariant Labs showed in May 2025 that an agent holding an
all-repositories token can be steered by a public issue into leaking private repositories
(https://invariantlabs.ai/blog/mcp-github-vulnerability).

- GitHub: fine-grained PATs went GA in March 2025 but still have gaps (outside
  collaborators, multi-organisation use, some APIs). Classic tokens remain common.
- GitLab: fine-grained PATs were still in beta in mid 2026 (epic 18177, +21). Granular
  permissions for other token types were descoped.
- Gitea: tokens carry category scopes such as `read:repository` and cannot be limited to
  chosen repositories. The request is open with +93
  (https://github.com/go-gitea/gitea/issues/25900). Checked in the tree: the token model
  has no repository binding.

**Branchroom idea.** A stolen ordinary token already cannot read a restricted workspace
without a grant, and OAuth and Actions tokens are refused outright. Go one step further
with workspace-bound credentials: a clone credential minted on the handoff page that works
for one workspace only, expires with the grant, and is useless against the rest of the
instance. Then a leaked credential exposes one workspace for a bounded time, and the audit
log names it. **M.** A smaller first step is to refuse PATs that have no expiry on the
restricted gateway. **S.**

### 2. Compliance evidence is collected by hand

Every company with SOC 2, ISO 27001 or SOX obligations has to prove that change and access
controls held for the whole audit period, not that they are switched on today. The forge
holds the facts but does not produce the evidence.

- A pull request "does not, by itself, prove that every production change used the
  workflow". Auditors sample merged changes and need author, last pusher, reviewer,
  reviewer authority at the time, and bypass use, joined up per change.
  https://oneuptime.com/blog/post/2026-08-04-github-pull-requests-change-management-evidence/view
- GitHub keeps audit events for 180 days and Git events for 7 days. Anything longer needs
  streaming to your own store, which is an Enterprise feature.
  https://docs.github.com/en/enterprise-cloud@latest/admin/concepts/security-and-compliance/audit-log-for-an-enterprise
- GitLab: Premium can view and query audit events, only Ultimate can stream them.
  https://docs.gitlab.com/security/security_telemetry/

Compliance vendors (Vanta, Drata and the like) exist largely to scrape this out of forges,
which is itself evidence of the gap.

- GitHub: rulesets, bypass lists, required approval of the last push. Evidence export is
  API scripting or a third-party tool.
- GitLab: compliance frameworks and approval rules, mostly in Ultimate.
- Gitea: upstream now has an audit log (checked in the tree: `models/audit`, 138 event
  types). It is off by default, keeps 30 days by default, has no tamper evidence, and
  exports JSON lines. Branch protection covers required approvals, stale approval
  dismissal, signed commits and `BlockAdminMergeOverride`.

**Branchroom idea.** An evidence pack: one command or button that exports, for a scope and
a date range, every grant, revocation, rotation, policy change and access decision, with
the chain head hash and a verification result, as a single file an auditor can keep. The
data and the verifier already exist. What is missing is the viewer and the export. **S for
the export, M with a viewer.** Pair it with an audit retention setting, since
`operations.md` lists that as not done.

### 3. Code review is the bottleneck, and AI made it worse

Universal and costly, and the one pain here that Branchroom should not chase.

- Faros telemetry from 10,000+ developers on 1,255 teams: teams with high AI adoption merge
  98% more pull requests and review time rises 91%. Vendor data, but the direction matches
  the 2025 DORA report.
  https://www.faros.ai/blog/ai-productivity-paradox-report-2025
- GitHub's thread on low-quality contributions (+231, 132 comments, January 2026).
  https://github.com/orgs/community/discussions/185387
- Code ownership that pages whole teams on every change (+401).
  https://github.com/orgs/community/discussions/35673

- GitHub: CODEOWNERS, merge queue, Copilot review.
- GitLab: approval rules, Duo review.
- Gitea: CODEOWNERS and required code-owner review exist. The complaint that code owners
  were not enforceable was closed in July 2026
  (https://github.com/go-gitea/gitea/issues/32602).

**Branchroom idea.** Do not compete on general review tooling. The only review work worth
doing is the restricted pull request already planned in M4, kept small: a restricted
workspace has a short, named reviewer list by construction, so review routing is trivial.
**L**, and only because restricted pull requests are.

### 4. The repository is the only read boundary

Teams want one project with a part that fewer people can read. Git and every forge say no,
so they split repositories, lose atomic changes, and manage permissions across the pieces.
This is the monorepo versus polyrepo argument seen from the permissions side.

- "Private or restricted-access branches within a public repository", July 2026, +15. The
  author points at GitHub's own temporary private forks as proof the workflow is valid.
  https://github.com/orgs/community/discussions/202434
- Folder-level access in a monorepo, +9. The only answer offered is branch protection,
  which controls writes, not reads.
  https://github.com/orgs/community/discussions/102755
- GitLab "Restrict read access to files by user or group", closed, with submodules given
  as the workaround. https://gitlab.com/gitlab-org/gitlab/-/issues/6051

Vote counts are low everywhere because the request is scattered over many duplicates and
most people already know the answer is no. Gitea's version was closed in 2022
(https://github.com/go-gitea/gitea/issues/4152).

- GitHub: repository-level read only. CODEOWNERS and push rulesets restrict writes.
- GitLab: same, plus protected branches and push rules for writes.
- Gitea: same.
- Perforce and Gerrit do offer path or ref read rules, which is why some regulated shops
  never left them.

**Branchroom idea.** This is the product. The implementation plan is right that per-path
views need rewritten history and should stay out. What is missing for day-to-day use is
keeping the restricted workspace current: a "sync public base" action that fetches the
public repository's new default-branch commits into the workspace so restricted work can be
rebased, recorded in the audit log. Without it a workspace goes stale after the first week.
**M.**

### 5. Access outlives the reason for it

Offboarding and access reviews fail on exceptions, not on policy.

- CrowdSec, disclosed autumn 2026: a departing engineer kept GitHub access "to finish some
  work", his laptop was later compromised, and private repositories were downloaded. The
  company found out nearly four months later when the code appeared on a forum.
  https://www.techtarget.com/cybersecurity/news/366651571/CrowdSec-breach-exposes-security-blind-spots-in-developer-access
- The same article's point: "When temporary exceptions are granted without
  platform-enforced expiry, offboarding ceases to be a security control."
- Shai-Hulud, September 2025: stolen tokens were used to flip victims' private repositories
  public with a `-migration` suffix.
  https://threats.wiz.io/all-incidents/shai-hulud-ongoing-package-supply-chain-compromise-delivering-data-stealing-malware

- GitHub: SCIM and SAML remove membership on Enterprise. Tokens, SSH keys and OAuth grants
  are separate. Repository access has no expiry date.
- GitLab: membership can carry an expiry date, which is the best of the three.
- Gitea: no expiry on collaborators or team membership.

**Branchroom idea.** Grants already support expiry, but it is optional (checked:
`ExpiresUnix` of 0 means never). Make expiry required for individual grants, with an
instance maximum such as 90 days and a one-click renew that is itself audited. Add a
per-user page listing every scope a person can reach, with revoke-all. That turns the
access review an auditor asks for into a list someone can read in a minute. **S.**

### 6. Nobody can say who read the source

After a token leak the first question is "was the code copied?", and most plans cannot
answer it.

- A GitHub Team customer on 11 November 2026, investigating a compromised PAT, asks how to
  confirm whether private source was cloned. The answer: `git.clone` and `git.fetch` events
  exist but are not in the web audit log and are kept for 7 days.
  https://github.com/orgs/community/discussions/212708
- GitLab only streams Git read events, and streaming is Ultimate. Anonymous reads are never
  recorded. https://docs.gitlab.com/user/compliance/audit_event_schema
- CrowdSec (above) learnt of the copy from a public forum, not from a log.

- GitHub: 7 days, API or stream only.
- GitLab: Ultimate streaming only.
- Gitea: the new upstream audit log has no Git read events at all (checked: the action
  list in `models/audit/action.go` has no clone or fetch entries).

**Branchroom idea.** Branchroom already records a decision for every upload-pack and
receive-pack request and chains it. Nothing else in this comparison does that on a free,
self-hosted plan. It only needs to be readable: an audit page per scope answering "who
fetched this workspace, when, with which credential type, and was it allowed". `operations.md`
currently tells operators to query the tables by hand. **S to M.** This is the cheapest
clear lead available.

### 7. Contractor and client code separation

Consultancies and agencies hold many clients' code on one instance. Product companies let
contractors into one corner. Both rely on repository sprawl and discipline.

- Red Hat Consulting's self-managed GitLab, October 2025: attackers claimed about 570 GB
  from 28,000 repositories, including around 800 customer engagement reports. Red Hat
  confirmed the breach but not the totals. One instance held every client's material.
  https://www.bleepingcomputer.com/news/security/red-hat-confirms-security-incident-after-hackers-breach-gitlab-instance/
- GitHub Enterprise Managed Users cannot collaborate outside their enterprise, so
  contractors need a provisioned guest identity or a second account.
  https://github.blog/changelog/2024-05-02-ga-guest-collaborators-for-enterprise-managed-users
- "Can I give read-only access to a private repo?" on a personal account, +299 and 203
  comments since 2017. https://github.com/orgs/community/discussions/23128

- GitHub: outside collaborators per repository, guest collaborators on EMU. Billed per seat.
- GitLab: Guest and external users, minimal access role, mostly paid tiers.
- Gitea: restricted users and per-repository collaborators. No engagement concept, no
  expiry.

**Branchroom idea.** A workspace per engagement already fits. Add the engagement shape on
top: grants that share one end date, a retire-and-purge reminder when the date passes, and
the purge recorded in the chain so the consultancy can show a client that their code is
gone. Purge with evidence is something no mainstream forge offers. **S to M.** Branchroom
would not have saved Red Hat on its own, because site administrators and anyone who takes
the host can read every store. Say that plainly in any pitch.

### 8. Self-hosting is heavy, and the controls are paywalled

Sovereignty and breach worry are pushing teams back to self-hosting at the same time as the
two big self-hosted products get harder to run.

- GitHub Enterprise Server: feature upgrades with data migrations "may cause a few hours
  of downtime" during which nobody can use the instance.
  https://docs.github.com/en/enterprise-server@3.20/admin/upgrading-your-instance/preparing-to-upgrade/overview-of-the-upgrade-process
- CVE-2024-4985, a CVSS 10 SAML bypass on GHES, followed by a regression of the fix
  (CVE-2024-9487). Self-hosted customers carry the patch window themselves.
  https://www.securityweek.com/critical-authentication-bypass-resolved-in-github-enterprise-server/
- GitLab: twice-monthly patch releases plus required upgrade stops every third minor
  version, so a neglected instance cannot jump straight to the fixed release.
  https://docs.gitlab.com/update/versions/gitlab_18_changes/

The Dutch government chose self-hosted Forgejo in 2026, ruling GitHub out as proprietary
and GitLab out for its open-core split
(https://itsfoss.com/news/netherlands-forgejo-migration/). Audit streaming, compliance
frameworks and fine-grained roles sit in GitLab Ultimate and GitHub Enterprise, so small
regulated teams pay enterprise prices for two or three controls.

- Gitea: one binary, small footprint, SQLite to PostgreSQL. This is its real advantage.

**Branchroom idea.** Keep the advantage and do not spend it. The operations runbook,
restore drill and doctor check are already better than most forks manage. The gaps a real
team will hit first are SSH and LFS for restricted workspaces (**M each**) and a backup
command that covers the restricted root so operators do not have to remember a second tar
step (**S**). The biggest risk is on Branchroom's own side: a fork that falls behind
upstream security releases recreates this pain. The November sync shows it can be done, so
keep the cadence written down.

### 9. Embargoed security fixes have no good home

Fewer organisations hit this than the pains above, but those that do have no workable
tool, and it is Branchroom's lead story.

- GitHub temporary private forks: "integrations, including CI, cannot access temporary
  private forks".
  https://docs.github.com/en/code-security/tutorials/fix-reported-vulnerabilities/collaborate-in-a-fork
- GitHub asked maintainers what to fix in advisories in March 2026 (+25, 29 comments). Two
  of the top answers: "CI on private forks is by far the most critical for us" and "I
  currently use separate private repos".
  https://github.com/orgs/community/discussions/189802
- Forks share an object network on GitHub. Truffle Security showed that commits from
  private and deleted forks stay fetchable by hash, and GitHub called it documented
  behaviour.
  https://trufflesecurity.com/blog/anyone-can-access-deleted-and-private-repo-data-github

Large projects work around it with a second private organisation and their own CI. Node.js
keeps `nodejs-private` and syncs branches by hand
(https://github.com/nodejs/node/blob/main/doc/contributing/security-release-process.md).

- GitHub: advisory plus temporary private fork, no CI, shared fork network.
- GitLab: confidential issue plus a private fork. The docs warn that a fork in another
  namespace may expose the work to a different set of users
  (https://docs.gitlab.com/user/project/merge_requests/confidential/).
- Gitea: nothing merged. Confidential issues have been requested since 2017 (+227,
  https://github.com/go-gitea/gitea/issues/3217). A security advisories pull request has
  been open since October 2026 (https://github.com/go-gitea/gitea/pull/39673) and has no
  private workspace for the fix.

**Branchroom idea.** The separate object store already beats GitHub's fork network on the
Truffle finding. Two things make it a finished story. First, publication (planned M5):
freeze both SHAs, show exactly what becomes public, apply it to the public repository, and
record it in the chain. **M.** Second, CI on restricted work, which is what maintainers
actually ask for. It needs an isolated runner and is **L**. Until it exists, be honest that
Branchroom matches GitHub's gap here. A cheap interim that helps: accept a signed "checks
passed" attestation pushed by a maintainer's own machine and show it on the workspace. **S.**

### 10. Inner source: findable without being open

Companies want engineers to discover internal code and ask for access, without making
everything readable. Forges offer all or nothing per repository, and repository sprawl makes
both discovery and permissions worse.

- "Create groups of repositories" on GitHub, +604 and 160 comments, open since 2022.
  https://github.com/orgs/community/discussions/41348
- Gitea subgroups, +381 and 129 comments, open since 2017.
  https://github.com/go-gitea/gitea/issues/1872
- State of InnerSource 2024 (about 120 respondents): only 15% said inner source was the
  default on all projects. The figure is from the report summary, I did not read the PDF.
  https://innersourcecommons.org/learn/research/state-of-innersource-survey-2024/

- GitHub: `internal` visibility exists only on Enterprise.
- GitLab: `internal` visibility on all tiers, plus nested groups.
- Gitea: `limited` visibility for signed-in users on organisations. No request-access flow.

**Branchroom idea.** `discover` without `read_source` is already the inner-source shape:
you can see that a scope exists and who owns it, and nothing else. Add a "request access"
button on the handoff page that files a request to the owner, with approve and deny
recorded in the chain. **S.** It also gives owners a paper trail for why each grant exists,
which feeds pain 2.

## What I would not build

- Per-path or per-branch read rules inside one repository. The plan already rules it out
  and the Git object model has not changed.
- General review tooling, AI review, merge queues. Others are far ahead and it is not why
  anyone would pick Branchroom.
- Anything that implies protection from site administrators or a compromised host.
  Branchroom does not provide it, and the Red Hat case is exactly that threat.

## Top 3 picks

Chosen for: small enough for one maintainer, builds on what is already written, and puts
Branchroom clearly ahead of what GitHub Team, GitLab Premium and stock Gitea offer.

1. **Read evidence you can show someone (pains 6 and 2). S to M.**
   An audit page per scope and an evidence-pack export with chain verification. Branchroom
   already records and chains every fetch and push decision. GitHub keeps Git read events
   for 7 days and hides them from the web log. GitLab streams them only on Ultimate. Stock
   Gitea does not record them. This is mostly a viewer and an export over existing tables,
   and it closes the "no audit UI yet" line in `operations.md`.

2. **Access that ends by itself (pains 5, 7 and 1). S, then M.**
   Required expiry on individual grants with an instance maximum, audited renewal, a
   per-user reach page with revoke-all, and engagement end dates that prompt retire and
   purge. Follow with workspace-bound clone credentials. CrowdSec, wfx-loot and Gheist are
   all the same story: access with no end date and credentials wider than the task. Only
   GitLab has membership expiry today, and nobody ties credential lifetime to the grant.

3. **Finish the embargo story with publication (pain 9). M.**
   Sync the public base into the workspace, then publish a frozen restricted head to the
   public repository with a preview and an audit record. With the separate object store
   this is already safer than GitHub's fork network, and Gitea has no equivalent. It is not
   clearly ahead of GitHub until restricted CI exists, which is L. Ship publication first
   and say the CI gap out loud.

If only one ships, make it the first. It is the smallest, it is the feature a buyer can be
shown in two minutes, and the other two both write into it.

## Gaps in this research

- No first-hand administrator accounts of GitHub Enterprise Server pain were found. That
  entry rests on GitHub's own upgrade docs and CVE history.
- Red Hat figures are the attackers' claims. Red Hat confirmed the breach, not the totals.
- I could not find a survey that measures stale forge access directly. Pain 5 rests on
  incidents.
- SOX-specific sources were thin. Pain 2 is argued from SOC 2 material, and the
  segregation-of-duties requirement is the same in substance.
- Bitbucket Data Center was checked and left out: Atlassian's 2029 Data Center end of life
  exempts Bitbucket.
