# Think-aloud: GitHub vs Branchroom (Fable)

Anonymous, read-only, dark theme, headless Chromium at 1440x900 and 390x844. Screenshots are in `fable-ta/`, named `<gh|br>-<page>-<d|m>.png`. Pages: murmur and oghma home, murmur and seol releases, oghma PR list, oghma PR 722. Not covered: light theme, the diff view, seol on a phone, anything behind login. I captured the seol and PR list pages for metrics but only looked closely at the ones named below.

Numbers measured in the page, not eyeballed:

| | GitHub | Branchroom |
| --- | --- | --- |
| Page load to `load` event | 3.2 to 5.6 s | 1.7 to 2.3 s |
| README top, murmur desktop | 911 px | 696 px |
| README top, oghma desktop | 2428 px | 2065 px |
| README top, oghma phone | 1310 px | 2548 px |
| First download link, murmur releases desktop | 701 px | 535 px |
| First download link, murmur releases phone | 832 px | 739 px |
| Horizontal overflow at 390 px | none | none |

Load times are not a fair race (LAN against the internet), but Branchroom does feel instant.

## Repository home, desktop

**GitHub (murmur).** Eye lands on the repo name with the avatar and the "Public" pill, then the green Code button. The file table is calm: grey icons, one accent colour. Right rail reads top to bottom as About, Releases with "Latest" and "+ 4 releases", Contributors, Languages. I know within two seconds what this is, that it ships builds and who works on it. README starts just below the fold.

**Branchroom (murmur).** Everything sits higher and tighter, and the README heading is already on screen at 900 px, which I like. But my eye has nowhere to rest. The first row under the tabs is a full-width bar of "51 Commits / 6 Branches / 5 Tags" that is given more weight than the branch picker. The header has no owner avatar and no Public or Private badge, so I cannot tell at a glance whether this repo is exposed. The latest commit row shows the Gitea teacup as the author avatar for "Semyon Fox", which reads as a broken image next to my real face on GitHub.

Things I looked for and did not find: a contributors list, a "+ 4 releases" link under the sidebar release, a clone URL without opening the Code menu.

Things that are there and should not be for a signed-out visitor: "Add File" next to the branch picker, a pencil on the README header, and an "Escape" button on the README header that means nothing to me. Watch, Star and Fork are also full-size buttons for someone who cannot use them.

The sidebar heading says "Description" where I expect "About", and the size line is "4.2 MiB" with a gap. Small, but it is the first text I read.

**oghma, desktop.** 30-odd top-level entries push the README to 2065 px on Branchroom and 2428 px on GitHub. Both are bad, GitHub slightly worse. The real difference is the sidebar: the Releases tab says 7, yet the Branchroom sidebar has no Releases block at all on this repo (`br-home-oghma-d.png`), while murmur has one. I did not check why (possibly the newest release is a pre-release). As a visitor I would conclude oghma has no builds. Stars also read 0 where GitHub says 4, so the mirror makes the project look deader than it is.

The file icons are coloured per type (orange git, red robot for AGENTS.md, blue Docker whale). On a long list it looks busy against GitHub's uniform grey. Taste call, I would mute them.

## Repository home, phone

**GitHub (oghma).** Name, About, topics, then a branch picker and a big green Code button, and the file list begins at about 760 px. Commit messages are dropped from the file rows, leaving name and age. Only Code and Issues show in the tab bar, with a labelled "More" menu.

**Branchroom (oghma).** The search box is the first thing under the tabs, before I know what the repo is. Then Description, then the stats block as five separate lines (Readme, 107 MiB, Stars, Watchers, Forks), then the commits bar, then three stacked rows of controls (branch, Go to file plus Add File, Code). The file table keeps three columns, so every row is clipped to "Sem...", "Merge pull requ...", "chore(ops): ...". Unreadable and no use. The README lands at 2548 px, nearly double GitHub's 1310. This is the weakest page in the comparison.

The tab overflow is a bare "..." at the far right with no label. On the releases and PR pages the active tab is hidden inside it, so the visible tabs are Code and Issues, neither highlighted, and only a thin underline under the dots hints where I am (`br-rel-murmur-m.png`).

## Releases

**GitHub (murmur).** Left rail lists every release as a jump link, and there is a "Find a release" box. Title with a green "Latest" outline pill. Assets have a count, sha256 with a copy button, size and date. Assets are alphabetical, so the first row is `android-update.json`, a 251 byte manifest, not something I want. First download link at 701 px.

**Branchroom (murmur).** Faster to the download: first link at 535 px, and the list leads with the AppImage, then the manifest, then the APK. All three are visible without scrolling at 1440x900. This is the one page where Branchroom beats GitHub on the task.

What lets it down:
- The badge says "Stable" on every release. The repo home says "Latest" for the same release. I want one release marked Latest and the rest left plain, otherwise the badge tells me nothing.
- No release list, no search, so reaching 0.1.18 means scrolling past four full bodies.
- The tag, commit hash and Compare button float in a left gutter outside the card. On desktop it wastes 160 px of width. On a phone it becomes a row above the card with Compare given the biggest button on screen.
- "semyonfox released this 2 days ago | 1 commits to main" has a literal pipe and "1 commits".
- No checksum. GitHub shows sha256 per asset and I do use it for APKs.
- There is still no "download the latest build" action. I pick a file out of a table on both sites. A single primary button for the newest release, or a per-platform pick, would beat GitHub outright.
- Tabs are "5 Releases / 5 Tags" plus a large "RSS Feed" button. The RSS button is the most prominent control on the page and I will never press it.

On the phone the asset rows wrap to two lines each (name, then size and date), so the three real downloads take about 230 px and start at 739 px. Fine, GitHub is at 832.

## Pull request list (oghma)

**Branchroom.** Dense and readable, labels inline, the base and head branches shown in the row, which GitHub does not do. The head branch is truncated to "dependabot/npm_and_yar..." with 600 px of empty space to its right. Titles are bold at full weight for every row, so nothing stands out. "Compare commits" is the primary blue button for an anonymous visitor. "4 Open / 544 Closed" lumps merged in with closed.

## One pull request (oghma 722)

**GitHub.** Purple "Merged", "merged 1 commit into dev from docs/roadmap-audio-ingestion", Commits 1, Checks 3, Files changed 1, a green tick on the commit, "3 checks passed". The whole story in one line.

**Branchroom.** The same PR shows a red "Closed" badge and "semyonfox wants to merge 0 commits", with Commits "-" and Files Changed "-". The timeline then says "Pull request closed. This pull request cannot be reopened because the branch was deleted." Directly above that, the timeline shows "Merge pull request #722 from semyonfox/docs/roadmap-audio-ingestion", so the page contradicts itself. This is the most serious thing I saw. It is probably a sync or migration data problem (merged state not carried over) and not styling, but a developer reading this page would believe finished work was thrown away. Worth checking how many of the 544 "closed" PRs are really merged.

Also on this page:
- The opening comment's avatar is a large Gitea teacup, and the author line carries a small GitHub mark plus "(Migrated from github.com)". Every later event is by "semyon" with a different generated avatar. One person appears as three identities.
- Sync time is presented as event time: "closed this pull request 11 minutes ago" where GitHub says merged 1 hour ago.
- No CI status anywhere. GitHub's Checks tab and tick have no counterpart on the mirror.
- The title is one huge light-weight line with the number wrapping alone on to a second line. On the phone the title takes five lines and 190 px before any content.
- The sidebar has eight empty sections (Reviewers, Labels, Milestone, Projects, Assignees, Due Date and so on), each saying "No ...". For a read-only visitor that is a column of nothing. On a phone the tab row is cut off at "File" with no scroll hint.

## Overall look

Branchroom is quicker, denser and closer to the content. GitHub is calmer and has clearer hierarchy. Branchroom's problems are mostly weight and emphasis: too many bordered boxes of equal strength, controls for signed-out users, secondary things (RSS, Compare, the commits bar) shouted, and primary things (visibility, latest release, merged state) missing or wrong. The footer shows "Powered by Gitea Version: 1.28.0+dev-1074-g7a33e1c1fe Page: 91ms Template: 44ms" to anonymous visitors, which leaks the exact build and says Gitea, not Branchroom. The logo is still the Gitea teacup, and the top bar offers only Explore, Help and Sign In, with no search.

## Top 8 Branchroom UI changes, ranked

1. Show merged pull requests as Merged with their real commit and file counts, not red "Closed, 0 commits, cannot be reopened". `br-pr-oghma-d.png`
2. Rework the phone repo home: file list with name and age only, stats on one line, search moved down, so the README is not 2548 px away. `br-home-oghma-m.png`
3. Give the newest release one primary download action and mark only it "Latest" (drop "Stable" on every release, match the sidebar wording). `br-rel-murmur-d.png`
4. Show the Releases block in the sidebar whenever the repo has releases (oghma has 7 and shows none), with a link to the rest. `br-home-oghma-d.png`
5. Hide write controls from signed-out visitors: Add File, the README pencil and Escape button, Compare commits, and the empty PR sidebar sections. `br-home-murmur-d.png`
6. Label the phone tab overflow and keep the active tab visible, so Releases and Pull Requests do not vanish into an unlabelled "...". `br-rel-murmur-m.png`
7. Fix identity on mirrored content: real avatars for migrated authors and commits, one identity per person, original event times instead of sync times. `br-pr-oghma-d.png`
8. Rebrand and quieten the chrome: Branchroom logo, add an avatar and visibility badge to the repo header, drop the version and timing footer for anonymous users, demote the RSS button and the commits bar. `br-prs-oghma-d.png`
