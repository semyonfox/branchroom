# What people dislike about GitHub's web UI (2024 to 2026), and what it means for Branchroom

Research date: 2026-11-13. Read-only desk research, no code touched.

Fixed Branchroom decisions, so this report does not re-argue them: README above the file list, and one-click latest-release download on the repo home.

## How to read the ranking

Ranked by how often the complaint turns up in independent threads, then by how much damage it does. This is not a statistical sample. HN and Reddit skew towards experienced developers who are already annoyed, so complaints from students, designers and PMs are under-represented and I have drawn on guides written for them to fill the gap. Vendor blogs selling an alternative (Revvie, PRFlow) are flagged where used. GitHub shipped a lot of performance and accessibility fixes in late 2025 and early 2026, so some 2025 complaints are partly stale. I note this where it matters.

---

## The 15 complaints, ranked

### 1. The UI got slow and janky after the React rewrite

Hurts: everyone, worst for reviewers and anyone on older hardware.

- https://news.ycombinator.com/item?id=44799861 "Githubs performance has been rapidly degrading ever since they started rewriting everything in React. It's basically impossible to view diffs now because they often fail to load, render correctly, or just are incredibly slow."
- https://news.ycombinator.com/item?id=43654339 "18.09MB and 8.81s to show a 67 file diff."
- https://news.ycombinator.com/item?id=43961329 (Ask HN) "every time I click a button, the page becomes unresponsive for many seconds", including marking a file as viewed.
- https://github.blog/engineering/architecture-optimization/the-uphill-climb-of-making-diff-lines-performant/ GitHub's own admission: "in extreme cases, the JavaScript heap could exceed 1 GB, DOM node counts surpassed 400,000".

Counter-view: https://news.ycombinator.com/item?id=44837652 argues React is not the real culprit. GitHub's changelog (https://github.blog/changelog/2026-02-05-improved-pull-request-files-changed-february-5-updates/) claims up to 67% faster interactions on large PRs and tab switches down from 10+ seconds to a few. I have not verified those numbers independently.

Also in the same threads: broken back button (https://news.ycombinator.com/item?id=44800616, "impossible to use on iOS" for 9 to 12 months), 14 background requests to draw a file overview, and the dashboard needing five GraphQL calls for an issue list.

Principle: server-rendered pages by default, with plain links and a working back button. Treat time to first useful paint as a budget on every page, and keep JavaScript islands small. Gitea's server-rendered core is already the advantage, so do not let a redesign trade it away for client-side routing. People who left say it directly (https://news.ycombinator.com/item?id=46097829): "I think Github has a nice UI.....when the contents finishes loading."

### 2. Large pull requests: slow, capped, and the workarounds break normal browser behaviour

Hurts: reviewers, maintainers of monorepos and generated-code repos.

- https://www.reddit.com/r/github/comments/1nyrip2/only_showing_the_first_1000_files_oh_cool_guess_i/ (1,014 points) "once your PR passes 1,000 files, GitHub's new 'Files Changed' page just... stops. No total file count. No button to load more."
- https://github.blog/changelog/2026-01-22-improved-pull-request-files-changed-page-on-by-default/ Virtualisation mode means browser "Finding diff content", "Selecting all text" and "Printing or exporting the page" do not work as expected.
- https://discussions6631.rssing.com/chan-78212243/article17889.html Current limits: 1,000 files in single-file mode, 3,000 in experimental mode, 40 comments on the page, 20 replies per comment.
- https://news.ycombinator.com/item?id=47051315 "reviewing big pull requests on GitHub became nearly impossible".

Fair counterpoint: the top reply on the Reddit thread (413 points) says a 1,000-file PR is itself the bug. Rebase noise, vendored files, lockfiles and codemods are still real, so the principle is about honesty and handling, not about supporting giant PRs happily.

Principle: never silently truncate. Always show the true total ("1,284 files, showing 1 to 300"), with a visible way to load the rest. Collapse generated, vendored and lockfile diffs by default with a count. Keep native find-in-page working by paginating by file rather than virtualising the DOM. Let people filter by path, status (unviewed, commented) and size.

### 3. Copilot and AI clutter that cannot be fully turned off

Hurts: everyone who does not want it, and new users who cannot tell what it is.

- https://news.ycombinator.com/item?id=45164497 "GitHub Community Discussions: Past year's top 2 requests are to disable Copilot".
- https://github.com/refined-github/refined-github/issues/8004 Request: "Remove all copilot buttons from the UI, including the explain on actions logs." The maintainer replies "we can't just hide a whole feature that presumably is good/useful", and the thread ends up as a pile of user-written CSS and uBlock filters.
- https://news.ycombinator.com/item?id=45148167 "I've had an ongoing support ticket with GH for several months now asking them to actually disable Copilot".
- https://github.com/orgs/community/discussions/191610 "Hide copilot option got removed from the iOS app and instead there is now a dedicated copilot tab".

GitHub did add settings to hide the dashboard entry point and the Agents tab (https://perrotta.dev/2026/01/github-disable-coding-agent/), but they are buried in settings and each new surface arrives on by default.

Principle: Branchroom should have no AI surface by default. If any is ever added, one master switch, off by default, that removes every trace including menu items, tabs and empty states. Never add a promo slot to the dashboard.

### 4. The dashboard and home page keep changing and rarely show "what needs me"

Hurts: daily users, and newcomers who land on a page that does not explain itself.

- https://www.reddit.com/r/github/comments/1oiy2pr/nearly_entire_github_dashboard_is_useless/ (1,374 points) Top comment: "You can't just search and immediately go to the desired repository, because it will search the public repositories instead."
- https://github.blog/changelog/2026-11-10-a-more-informative-github-dashboard/ GitHub's own words: it "used too much space to show too little", with generous padding, wrapped titles and three items by default.
- https://news.ycombinator.com/item?id=49942818 "Nothing on that new page helps me work on the projects I'm working on." Others praise putting your own issues and PRs first.
- https://www.theregister.com/software/2023/09/13/github-alienates-users-with-ai-recommendations/1439189 The 2023 merging of "Following" and "For You" feeds.

Principle: the home page is a personal worklist. Order it as: waiting on you, your open PRs, recently visited repos, then everything else. Dense rows, a quick repo switcher that searches your repos first, and no algorithmic feed. Layout should stay stable so muscle memory survives upgrades.

### 5. Search regressions and hijacked keyboard shortcuts

Hurts: developers, and everyone who relies on Ctrl+F.

- https://www.theregister.com/software/2023/05/13/github-code-search-redesign-cant-find-many-fans/906843 Complaints listed: no time-based sorting, results limited to five pages, search only on the default branch, changed query syntax. "The new viewing experience is slow and miserable."
- https://news.ycombinator.com/item?id=36730023 "overriding Ctrl-F ... is a crime against humanity."
- https://news.ycombinator.com/item?id=43154358 Alt+F, which should open the browser menu, instead "(un-) marks the issue as 'Favorite'". Also: repo search requires being signed in.
- https://news.ycombinator.com/item?id=48704899 A recent search-bar change: "The search bar has become a very frustrating exercise of retyping queries that originally were just a quick edit."

Principle: never override Ctrl+F or browser chrome shortcuts. Search needs one box with scope chips (this repo, my repos, all), editable query text, and results that work signed out on public repos. Keep keyboard shortcuts single-key, listed on `?`, and remappable or switchable off.

### 6. Actions logs are hard to read, search and navigate

Hurts: anyone whose CI fails, which includes occasional contributors who see a red cross and cannot tell why.

- https://news.ycombinator.com/item?id=44802136 "Trying to find a string in github actions logs is just impossible." The raw-log workaround "doesn't work for still-running jobs".
- https://winbuzzer.com/2026/02/06/github-actions-slowly-killing-engineering-teams-circleci-critique-xcxwbn/ The log viewer "crashed his browser 'repeatedly and reliably'".
- https://github.com/actions/runner/issues/3236 With 20,000 lines, "there are no possibility to search for the cause because older logs are placed at the top".
- https://gist.github.com/jenschelkopf/8dbab326522ae01ac5ced596adf88b19 (2020 GitHub survey, old but still echoed) "it's super annoying how many clicks it takes to get to the actual failing part of the build logs". The 2026 community tool https://blog.diatomlabs.com/i-stopped-tab-switching-ongithub-actions-meet-gha-tui-aaf4d8c25abd advertises a "failed-step jump", so the need is still unmet.

Principle: open on the first failed step, expanded, with errors highlighted. Real in-log search, stream running logs, plain-text download one click away, and a one-line failure summary at the top for non-developers.

### 7. Notifications: noisy, default-subscribe, awkward triage

Hurts: maintainers most, but also newcomers who comment once and get flooded.

- https://pradyunsg.me/blog/2024/03/17/github-notifications-userstyles/ "the action buttons are on the completely opposite side from where my attention is."
- https://drewdevault.com/blog/GitHub-notifications/ (2020, widely cited since) Hover-only action buttons, rows vanishing when filtered to unread, losing your place.
- https://docs.github.com/en/subscriptions-and-notifications/reference/inbox-filters Official limits: no full-text search in the inbox, no `NOT` exclusion, maximum 15 custom filters.
- https://www.urhoba.net/en/post/managing-github-notifications-and-watching "When you comment on an issue, you are automatically subscribed to it. This is the most common source of noise."
- https://revvie.dev/blog/github-notifications-broken (vendor, treat with care) "Everything Is Equally Loud".

One secondary source (https://oregand.github.io/blog/github-notifications-cheatsheet/) says GitHub deprecated "automatically watch repositories" in May 2025. I could not confirm that against GitHub's own docs, so verify before relying on it.

Principle: show why you got each notification as a plain sentence. Put row actions on the left next to the title with keyboard equivalents. Group by repo, default to "only when mentioned or assigned", and tell people what they just subscribed to when they comment.

### 8. PR conversation and review feel unreliable (hidden items, suggestions, viewed state)

Hurts: reviewers and authors, and it erodes trust.

- https://github.com/refined-github/refined-github (readme) The extension exists partly to fix "N hidden items. Load more..." by loading "up to 200 comments at once instead of 60", and to batch-mark files as viewed and jump to the first unviewed file.
- https://news.ycombinator.com/item?id=49942818 One week of bugs: "a diff showing '0 files changed' while there are many" and "suggested changes in a PR not rendering the original line".
- https://github.blog/changelog/2026-01-22-improved-pull-request-files-changed-page-on-by-default/ GitHub's fix list admits earlier bugs where suggested changes "were applied incorrectly, partially applied, deleted lines unexpectedly", and where users "could not add, edit, delete, reply to, or jump to comments".
- https://news.ycombinator.com/item?id=47051315 Collapsed sections not shown made two patch fragments "look like continuous code", so a reviewer commented wrongly. "much less trust now."
- https://github.com/refined-github/refined-github/issues/9346 The "Collapse all / Expand all" buttons were removed, leaving only a hidden Alt-click.

Counterpoint worth keeping: GitHub did add locally saved draft comments, a pending-comments panel and review by commit. Those are good and should be matched.

Principle: the timeline never hides anything without a visible count and a one-click expand-all. Unresolved threads are pinned in a side list. Suggestion apply must be tested like payments (atomic, previewed, with a visible diff of the result). Viewed state is per file and per revision and survives reload, with a progress indicator and "next unviewed".

### 9. Unpredictable UI experiments and silent rollouts

Hurts: everyone, and it makes screen-reader users especially wary.

- https://www.hci.today/en/news/4696 Issue links suddenly opened in a popup. GitHub called it "an experiment", then reversed it after feedback. Users said it did not feel like a link and hurt assistive technology.
- https://www.reddit.com/r/github/comments/1l4bkng/github_new_ui_change_roll_back_that_shit_rn/ (1,003 points) A visible change, with a top reply that it "didn't happen for me, maybe some kind of ab testing?"
- https://news.ycombinator.com/item?id=44799861 "New Issue" in a small modal "only if you have templates set for some reason".
- https://news.ycombinator.com/item?id=49942818 "Opting out just makes the Feed appear on / again... the opt-out-until-we-remove-the-opt-out thing".

Principle: a link is a link (real URL, opens in a tab, back works). No A/B surprises on core pages, and big changes ship with an opt-out that stays.

### 10. Releases and downloads: "Source code" zip confuses people who want the app

Hurts: newcomers, end users, and maintainers answering support questions.

- https://bagder.github.io/github-feedback/releases-tarball.html (curl's maintainer) "Users who against better judgement download one of those packages from the release page doesn't get a 'real' release! This continues to cause confusions among users." There is still no way to disable the auto-generated archives.
- https://github.com/github/docs/issues/31147 GitHub's docs team: "I can definitely understand wondering why these files are being automatically included in every release."
- https://www.virtualcuriosities.com/articles/1697/how-to-download-from-github "most releases are just a bunch of files with weird names with no description of which one you want to download".
- https://www.techupsider.com/articles/how-to-download-from-github Code then Download ZIP "gives you the project's source code, the recipe instead of the meal". Also: "GitHub has no download button for a single folder."
- https://www.elektormagazine.com/labs/github-for-dummies "you cannot just download the files you see listed in a repository."

Branchroom already has the one-click latest download on the repo home. The remaining gaps are on the release page itself and for single files and folders.

Principle: on the releases page, lead with a "Download for your system" button chosen from the visitor's user agent, label assets in plain words (Windows installer, Mac, Linux), and tuck source archives and checksums under "For developers". Offer folder download natively.

### 11. Settings sprawl and a fragmented permission model

Hurts: org admins, repo owners, and new collaborators who cannot see why a tab is missing.

- https://omarcrosby.com/posts/github-repository-settings-in-plain-language/ "a wall of sections, each of them meaningful, most of them unexamined". The Settings tab is invisible without admin: "If the tab is missing, that is why." Old branch protection vs rulesets means pre-mid-2023 tutorials "are all describing this older system".
- https://ghtier.com/permission-model "GitHub does not have one permission system": classic scopes, fine-grained resource permissions, and roles all use different vocabularies.
- https://docs.github.com/en/organizations/managing-peoples-access-to-your-organization-with-roles/permissions-of-custom-organization-roles "You can't add repository permissions without a base repository role."
- https://www.devopsschool.com/tutorials/github-repository-settings-complete-reference-guide-tutorial/ "GitHub periodically reorganizes settings navigation."

Principle: one permission vocabulary everywhere (people, tokens, apps), with a "why can't I do this" explanation on any disabled control. Settings grouped by task ("Who can change this", "How merging works", "Publishing") with search, and a summary page of what is currently restricted.

### 12. Accessibility: improving, but keyboard and focus gaps remain

Hurts: keyboard-only and screen-reader users, and anyone on a tiny budget of motor effort.

- https://accessibility.github.com/conformance/github-com/ GitHub's own WCAG 2.2 AA audit lists 14 high-priority keyboard issues (2.1.1), 37 medium and 140 low focus-order issues (2.4.3), and 83 medium and 401 low info-and-relationships issues (1.3.1).
- https://github.blog/engineering/user-experience/accessibility-considerations-behind-code-search-and-code-view/ GitHub admits virtualising the code view "created its own accessibility problems" and that overriding Ctrl+F is "generally bad practice for screen readers".
- https://github.blog/open-source/building-githubs-next-chapter-in-accessibility/ The rebuilt Files changed page added landmarks and "fewer page reloads (critical for screen reader users who lose their place when pages refresh)".
- https://news.ycombinator.com/item?id=43154358 Hotkeys colliding with browser shortcuts.

Principle: build to WCAG 2.2 AA from the start, and keep real semantic HTML (tables for tables, lists for lists). Do not virtualise or hijack find. Test with NVDA and keyboard-only on PR review and settings, because those are the heavy pages. Keep light and dark themes both passing contrast checks.

### 13. Issues and Projects are powerful but confusing, with weak basics

Hurts: PMs, small teams, occasional contributors. Note: the Projects sources are 2021 to 2023, so treat them as a pattern rather than current detail.

- https://johnwargo.com/posts/2023/my-take-on-github-projects/ "Link a project? What does that mean?" and "Draft has no context here." Projects attach to the account, not the repo, and cards do not stay in sync with issues.
- https://news.ycombinator.com/item?id=29834560 Projects "hijack CMD+F" and open a new tab for anything you click.
- https://qil134.medium.com/redesigning-githubs-project-management-experience-c5af48ac4bfe (small student study, 2020) "Users with less experience with Issue and Pull Request seem to struggle more with the task." Two participants had trouble linking the PR to the issue and requesting a review.
- https://news.ycombinator.com/item?id=44799861 The templates modal using under half the screen, which "is only going to reduce the quality of reported issues".

Principle: one board per repo by default, backed directly by issues, with no "draft" concept to learn. Issue creation is a full page with the template visible. Linking a PR to an issue is a labelled field, not a magic keyword buried in the description.

### 14. Mobile web is patchy

Hurts: reviewers on the move, and anyone downloading a release from a phone.

- https://devactivity.com/insights/github-mobile-ui-glitches-a-roadblock-to-software-development-efficiency/ (Feb 2026 community report) Broken layouts, the default diff view switching to split which was "completely unusable on mobile", and a dead organisation button.
- https://discussions6631.rssing.com/chan-78212243/article17889.html The new Files changed page listed "Small screen (e.g. mobile device) responsive layout" as a known limitation.
- https://www.techupsider.com/articles/how-to-download-from-github On a phone "the Releases link can sit much further down the page".
- https://colinkeany.com/github-responsive/ GitHub's own retrospective says a missing responsive site "was becoming a top user complaint" and that nearly 97% of pages are now responsive.

Principle: design phone width first for the repo home, release page, issue view and PR conversation. Default to unified diffs on narrow screens, use big tap targets, and keep the download button in the first screen.

### 15. The repo home is built for owners, not visitors, and the vocabulary is developer-only

Hurts: newcomers, designers, writers and docs readers.

- https://hci-nerdz.github.io/blog/when-the-file-tree-owns-the-first-viewport/ (opinion piece, useful framing) "'No releases published' is not a visitor fact", and "the file browser owns the first viewport".
- https://github.com/refined-github/refined-github (readme) The extension collapses "repository tabs if rarely used (Insights, Security) or when empty (Projects, Wiki, Actions)" and trims "unnecessary or redundant information from the repository sidebar".
- https://ailearninghub.law.stanford.edu/github-without-the-jargon/ The page is "covered in tabs, file names, buttons, and words that look like ordinary English being used in some other way."
- https://arxiv.org/abs/2101.08903 (https://ar5iv.labs.arxiv.org/html/2101.08903) For novices, "finding a way to start" was the top barrier (22 of 27 respondents agreed).

Branchroom's README-first home already answers half of this. Still open: empty owner-only sections for visitors, always-on tabs, and jargon labels.

Principle: show only sections that have content, and reveal owner tools to owners. Pair every technical term with a plain label.

---

## What GitHub gets right that we must not lose

Evidence-backed, so I am not just listing taste.

1. Review ergonomics that arrived recently: locally saved draft comments, a pending-comments panel before submitting, resizable file tree with indicators for comments and errors, review by commit. Source: https://github.blog/changelog/2026-01-22-improved-pull-request-files-changed-page-on-by-default/
2. Suggested changes with one-click commit and batching, with co-author credit for the suggester. Source: https://docs.github.com/en/enterprise-server@2.21/articles/incorporating-feedback-in-your-pull-request
3. The "viewed" checkbox and progress bar. Even the extension community builds on it (jump to first unviewed, shift-click batch mark), which tells you the core idea is liked.
4. Notification reasons and triage verbs: Done, Saved, Unsubscribe, plus `reason:` filters (https://docs.github.com/en/subscriptions-and-notifications/reference/inbox-filters). The model is good, the layout and limits are the problem.
5. Stable, guessable URLs: `/releases/latest`, `/archive/refs/heads/main.zip`, line anchors, and downloads that need no account (https://www.techupsider.com/articles/how-to-download-from-github).
6. Edit in the browser: pencil icon, commit, "propose changes" that creates a branch and PR for you. Designers and docs writers rely on it (https://pdkaizer.github.io/github-for-designers/).
7. Raw view and the `.` web editor, which HN users repeatedly use as escape hatches when the main UI is slow.
8. The symbol panel and fuzzy file finder, praised even by critics (https://news.ycombinator.com/item?id=44799861).
9. A consistent, predictable top-level shape (Code, Issues, Pull requests, then the rest), which let every tutorial and screenshot stay valid for years.
10. Social discovery and network effect. Not a UI feature and not something a self-hosted fork can copy, but HN switchers say it is the main reason they stay (https://news.ycombinator.com/item?id=44876289). Branchroom's answer is to be fast, calm and trustworthy for the people already inside the instance.
11. Accessibility investment as a visible practice (conformance report, engineering blog posts). Publishing yours would be a differentiator.

---

## Intimidation for normal people: the top 8 barriers

Sources for this section: https://pdkaizer.github.io/github-for-designers/, https://ben.balter.com/2023/03/02/github-for-non-technical-roles/, https://ailearninghub.law.stanford.edu/github-without-the-jargon/, https://buildtolaunch.ai/p/github-for-non-programmers, https://www.techupsider.com/articles/how-to-download-from-github, https://arxiv.org/abs/2101.08903.

1. **"Which button gets me the thing?"** Code, Download ZIP, Releases, Assets and Source code all look similar, and a folder cannot be downloaded at all. People end up with source they cannot run. Calmer UI: one "Get the latest version" action that picks the right file for the visitor's system (already fixed on the home), plain asset names, source archives under "For developers", and native "download this folder".

2. **Jargon.** Commit, branch, pull request, fork, merge, clone, push. One source puts it well: "If a writer had named commit, it would be called 'save with a note'". Calmer UI: show the plain label first with the technical term beside it ("Save your edit (commit)", "Propose a change (pull request)"), and keep the term in tooltips and docs so developers do not lose precision.

3. **Fear of breaking something.** Designers are told outright "You can't break the project by looking", which means the page itself does not reassure them. A browser edit takes up to 12 steps in one guide (Balter). Calmer UI: a single "Suggest an edit" flow with a live preview, one sentence saying "this will not change the main project until someone approves it", and an obvious undo or discard.

4. **Everything shown to everyone.** Tabs for Insights, Security, Projects, Wiki, Agents, empty Releases and Packages, and Copilot entry points. Calmer UI: show only sections with content, collapse owner-only tabs for visitors, remember what each person uses, and keep the page the same shape between upgrades.

5. **Reading a pull request when you do not read code.** A PM or designer sees a code diff and a timeline of events. Calmer UI: lead with a status sentence ("Waiting on you", "2 of 3 checks passed, 1 approval needed"), conversation before files, collapsed generated files, rendered previews for images and Markdown, and commenting on a whole file or the description without picking a line.

6. **"Why can't I do this?"** Missing Settings tab, disabled merge button, a fork you did not know you needed, and role names like Triage and Maintain. Calmer UI: any disabled control explains the reason and the next step in one line ("You need Write access. Ask an owner or suggest a change instead"), and roles use plain descriptions of what they allow.

7. **Notification and email flood after one comment.** Auto-subscribing on comment is "the most common source of noise". Calmer UI: say what was just subscribed ("You will get updates when someone replies or mentions you"), default to quiet, and show the reason on every notification.

8. **A red cross with no explanation.** Failing checks lead to a wall of log text a non-developer cannot parse. Calmer UI: a human sentence ("Automated tests failed on step: Build docs. A maintainer will take a look"), a jump to the first failure, and a clear statement of whether it is the contributor's problem.

---

## Gaps and caveats

- No hard usage data. Everything above is qualitative, and the "ranking" reflects how many independent threads I found, not a survey.
- Dates on a few HN items were not returned by the search tool. Where they are undated I cited them without a date.
- The 1000-file and virtualisation limits come from GitHub's own feedback discussion and changelog and may have moved since. Re-check before quoting numbers.
- Non-developer evidence is thin and mostly from guides written for that audience. If Branchroom wants hard numbers on barriers 1 to 8, the next step is five short moderated sessions with a student, a designer and a PM on a real repo.
