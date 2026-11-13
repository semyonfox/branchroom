# Research, 13 November 2026

Eight findings that Branchroom does not have yet, ranked by what to build first. Each one cites a primary source from 2025 or 2026 where I could reach one, and the code path in this repo where the gap is. The MCP endpoint, the agent identity on tokens and the record of which agent pushed are already planned, so they are left out.

The order favours holes in features this fork already runs. Agents force-push and hold tokens, so those land before the features people name when they explain why they stay on GitHub.

Checked in the repo: `services/automerge/automerge.go`, `services/pull/pull.go`, `services/pull/merge.go`, `services/pull/readiness.go`, `models/auth/access_token.go`, `models/auth/oauth2.go`, `modules/secretscan/secretscan.go`, `routers/private/hook_secret_scan.go` and `models/git/protected_branch.go`.

Not confirmed: the Forgejo issues and Gitea issue 36392 that earlier drafts quoted at length. Search did not return them, so I dropped the quotes and the links to them.

## 1. Scheduled auto-merge survives a force-push

Size S. Evidence: [services/pull/pull.go](../../../../services/pull/pull.go:561) and [services/automerge/automerge.go](../../../../services/automerge/automerge.go:120)

A push to the head branch calls `UpdatePullsRefs`, which only refreshes the base repo's refs. It does not touch the scheduled merge row. The auto-merge check compares the head with the commit the checks ran on, and merges when that commit passes. So a force-pushed head that passes CI can merge without a new review.

The gap is narrower than it looks when "dismiss stale approvals" is on, but that setting is off by default. I did not trace whether the merge path re-checks approvals before it merges. That is the first thing to confirm.

Fix: clear the scheduled row when the head SHA changes, and keep the cancel button and API as they are.

## 2. Access tokens that a secret scanner can tell apart from a commit

Size S. Evidence: [models/auth/access_token.go](../../../../models/auth/access_token.go:54), [modules/secretscan/secretscan.go](../../../../modules/secretscan/secretscan.go:21) and [models/auth/oauth2.go](../../../../models/auth/oauth2.go:218)

Access tokens here are hex-encoded, which is the same alphabet as a commit SHA. The push scanner matches GitHub, GitLab, npm and several other vendor shapes, but nothing for this server's own tokens. OAuth client secrets already use a `gto_` prefix, so the pattern exists in the codebase.

Source for the revocation argument: GitHub's [credential revocation API](https://github.blog/changelog/2025-04-29-credential-revocation-api-to-revoke-exposed-pats-is-now-generally-available/), generally available on 29 April 2025, only works on tokens that can be recognised. That is the reason a prefix matters here.

Fix: give new access tokens a prefix, keep old tokens working, and reject a push that contains a live token this server issued. Runner registration tokens need their own change because the registration flow depends on their format.

## 3. A push-protection bypass needs an audit record

Size S. Evidence: [routers/private/hook_secret_scan.go](../../../../routers/private/hook_secret_scan.go:27)

The hook returns before scanning when the push option `secret_scanning.allow=true` is set. I found no audit write on that path, so any pusher can skip the check, including an agent token.

GitHub made bypass requests manageable through its REST API in [February 2025](https://github.blog/changelog/2025-02-26-manage-push-protection-bypass-requests-for-secret-scanning-with-the-rest-api/). Everyone who is not a bypass approver must ask first, and the request is recorded.

Fix: a setting that turns the option off per repository, and an audit event each time someone uses it. A reviewer queue can come later.

## 4. Block the merge while review conversations are unresolved

Size S. Evidence: [models/git/protected_branch.go](../../../../models/git/protected_branch.go:46)

The protected branch fields cover approvals, rejected reviews, outdated branches and codeowner reviews. None of them looks at whether a review conversation is resolved.

GitHub documents [require conversation resolution before merging](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-protected-branches/about-protected-branches) as a branch protection option. Bots that resolve their own threads after a fix lands need this to be useful.

Fix: one boolean on the protected branch and a check in the readiness computation in `services/pull/readiness.go`.

## 5. Admin-defined secret patterns on the push hook

Size M. Evidence: [modules/secretscan/secretscan.go](../../../../modules/secretscan/secretscan.go:21)

The scanner is a fixed list of eleven vendor patterns. A private forge's own token format is never on that list, and this fork has no way to add one.

GitLab's [custom push protection epic](https://gitlab.com/groups/gitlab-org/-/epics/17997) asks for the same thing. Its follow-up, [epic 20198](https://gitlab.com/groups/gitlab-org/-/epics/20198), reports that 78% of 408 customer configurations customise their patterns. That is the demand signal.

Fix: a short list of admin regexes with a time limit, so a bad pattern cannot stall the hook. Expect false positives, and keep the list small.

## 6. Refuse a merge that carries an open secret finding

Size M. Evidence: [routers/private/hook_secret_scan.go](../../../../routers/private/hook_secret_scan.go:21)

The hook scans added lines at push time and stores nothing. A push over 32 MiB is skipped (`secretScanMaxPatchBytes`), and a secret already in the file never gets scanned. A merge check has nothing to read.

GitHub's [September 2026 changelog](https://github.blog/changelog/2026-09-09-block-pull-requests-with-exposed-secrets-from-merging/) adds a ruleset that blocks a merge until the head commit's scan has finished and the pull request has no open secret alert. Public preview at the time of the post.

Fix: store each finding against the pull request and refuse the merge until it is cleared or dismissed. Start with the push hook output, then cover the head commit.

## 7. Path-scoped approvals by team, with a count

Size M. Evidence: [models/git/protected_branch.go](../../../../models/git/protected_branch.go:317) and [services/pull/readiness.go](../../../../services/pull/readiness.go:111)

This fork already has protected file patterns. `MergeBlockedByProtectedFiles` blocks any merge that touches a matching file, whatever the approvals say. What it cannot do is require a set number of approvals from a named team for those paths.

GitHub made its [required reviewer rule](https://github.blog/changelog/2026-02-17-required-reviewer-rule-is-now-generally-available/) generally available on 17 February 2026. It takes file patterns, team names and a count, and it sits alongside CODEOWNERS.

Fix: extend the existing protected file pattern with team and count fields. Build it on one repository first.

## 8. A real merge queue

Size L. Evidence: [services/automerge/automerge.go](../../../../services/automerge/automerge.go:120)

Auto-merge here runs once per check result. It merges a pull request when that pull request's checks pass. It does not test several queued pull requests combined, so two green pull requests can still break the base branch when they land in sequence.

[gitea-mq](https://github.com/Mic92/gitea-mq/wiki) is an external queue for Gitea that tests the combination before it presses the existing auto-merge button. It shows the design works. I could not confirm the upstream Gitea issue that discusses this, so the case rests on the code and on gitea-mq.

Fix: merge the pull request onto the current queue head in a temporary tree, run CI on that commit, and merge only if it passes. Leave this until items 1 to 7 are done.

## Looked at, and left off the list

- GitHub's [June 2026 change](https://github.blog/changelog/2026-06-18-generated-release-notes-credit-you-for-copilot-pull-requests) credits the person who asked for an agent's pull request. That is attribution, and it overlaps the agent identity work already planned.
- The notification inbox, saved replies and the readiness API are already on this fork. I found nothing from 2025 or 2026 that asked for more.
