# Branchroom vs GitHub: UI comparison

Read-only, anonymous, one capture per page and theme, 13 November 2026.

Screenshots and metrics are in the folder a local scratch folder that was not kept. Filenames below are relative to that folder.

## Bottom line

Branchroom has the same structure as GitHub, so most of the gap is weight and chrome, not layout. The main problems are bold list titles on every row, a repo action bar repeated on every page, tall tabs, default avatars for migrated users, and a font that falls back to the system face. Branchroom does better on density and on provenance: its commit rows are 37 to 43px against GitHub's 41 to 47px on the repo page, and it says where migrated content came from. Fix the top four ranked items below and keep the commit table and diff summary as they are.

## Think-aloud, page by page

### Profile

**GitHub** (`gh-profile-desk-light.png`): Opens on Overview with a README card, a photo and follower counts. I know where I am straight away. It is busy at 1440px because of the three columns, but nothing looks dated.

**Branchroom** (`br-profile-desk-light.png`): Lands on the Repositories tab, so there is no README. The avatar is a generated pattern, not a photo, which makes the page feel anonymous. The repo rows are easy to scan: blue name, grey description, language dot, counts. It looks plain and a little dated, but it reads more easily than GitHub's card layout.

### Global header, repo header and tab bar

**GitHub** (`gh-repo-desk-light.png`): A black 68px header with search, the "/" shortcut, Sign in and Sign up. The repo header has a breadcrumb, then tabs with icons and counts. The black bar is heavy against the light page, but the tabs are easy to read.

**Branchroom** (`br-repo-desk-light.png`): A light 50px header with Explore, Help and Sign In, and no global search. Repo search sits in the sidebar as "Search code...". The title row has three boxed buttons (RSS, Watch 1, Star 0, Fork 0). The tabs have counts but no icons and are 41px tall. The commit summary box ("2,168 Commits, 10 Branches, 7 Tags") is useful. Above the file list there are two search fields, "Go to file", "Add File" and a blue Code button, so there are a lot of controls before the content.

**Mobile, dark** (`gh-repo-mob-dark.png`, `br-repo-mob-dark.png`): GitHub keeps Code and Issues visible with a More menu. Branchroom shows Code and Issues, then "...", so Pull Requests is hidden. The top 250px of the Branchroom page is chrome: header, title, three buttons, tabs and a search field.

### Pull request list

**GitHub** (`gh-pulls-desk-light.png`): A search bar with `is:pr state:open`, Open and Closed counts, filter dropdowns, label pills and avatars on the right. Titles are weight 500 and rows are 67px apart. It is clear and calm.

**Branchroom** (`br-pulls-desk-light.png`): An Open and Closed segmented control with filters. Every title is bold (600), so each row shouts. The base and head branch chips (`dev « dependabot/npm_and_yar...`) are useful. Rows show no reviewer, check or avatar. The footer reads "Powered by Gitea, Version 1.28.0-dev-1074-g7a33e1c1fe, Page 111ms, Template 45ms", which is build noise on every page.

**Mobile, dark** (`gh-pulls-mob-dark.png`, `br-pulls-mob-dark.png`): GitHub rows show check counts such as 4/4 and reviewer avatars. Branchroom rows fit more text but show no status. Its filters take two rows before the list starts.

### Merged PR with reviews (#609): conversation

I used PR #609 on both sites. PR #721, which I tried first, has zero reviews on GitHub, so it does not meet the brief. #609 is merged with 9 reviews on GitHub and shows the same on Branchroom.

**GitHub** (`gh-pr-desk-light.png`): A 32px title, a purple Merged pill, the author and branch on one line, and tabs. The conversation has a comment card with an Owner badge. The right sidebar lists reviewers, with "1 more reviewer" collapsed. The hierarchy is clear.

**Branchroom** (`br-pr-desk-light.png`): The same title and pill. The deleted branch name is struck through, which tells you it is gone. The diff stat (+262 -46) sits in the tab row. Migrated comments carry "(Migrated from github.com)", which is honest provenance. Against that, the comment avatar is the Gitea logo, the Watch, Star and Fork bar repeats above the title, and reviewers appear as names with GitHub marks instead of avatars.

**Mobile, dark** (`gh-pr-mob-dark.png`, `br-pr-mob-dark.png`): Both tab strips clip their last tab at 390px (GitHub "Checks 6", Branchroom "Fi"). The 32px title wraps to five lines on Branchroom. The struck-through branch name is hard to read at this width.

### Files changed

**GitHub** (`gh-pr_files-desk-light.png`): A file tree with a filter, per-file diffs with line numbers, and "Changes from all commits". It is crisp and well spaced. It renders 7,350 DOM elements for this PR.

**Branchroom** (`br-pr_files-desk-light.png`): The summary line "18 changed files with 262 additions and 46 deletions" is the best element on either page. Inline review comments sit above the diff with a Review button. The avatars are the same default icons as before. It renders 10,882 DOM elements for the same PR, roughly 48% more than GitHub.

### Issues list

**GitHub** (`gh-issues-desk-light.png`): A left sidebar (Issues, Assigned to me, Created by me, Mentioned, Recent activity, Views, Milestones, Labels) and a search bar with qualifiers. Titles are weight 500 and rows are 65px apart. Dates are absolute ("opened on Aug 27").

**Branchroom** (`br-issues-desk-light.png`): A full-width list with no sidebar. Bold titles make it heavy, and this is just as true on the dark theme (`br-issues-desk-dark.png`). Rows are 66px apart. The meta line uses relative time and has no avatar. The 0/4 task progress bars are a good touch.

### One issue (#478)

**GitHub** (`gh-issue-desk-light.png`): A 32px title with a grey number, a green Open pill, a comment card with an Owner badge, and a sidebar that says "No one assigned". The yellow banner "Sign up for free to join this conversation" appears because I am logged out.

**Branchroom** (`br-issue-desk-light.png`): The title fits on one line. The meta reads "opened 3 months ago by semyonfox, 0 comments". The sidebar has a grey "No Branch/Tag Specified" box that looks like a broken field. The Participants box shows a placeholder icon instead of a photo.

### Commits list

**GitHub** (`gh-commits-desk-light.png`): Grouped by day ("Commits on Nov 12, 2026"), with avatars, "authored 14 hours ago", Verified badges, CI check counts (2/2, 10/15) and copy and browse icons on each SHA. Rows are 65 to 70px apart. It is airy and reads well, but you scroll more.

**Branchroom** (`br-commits-desk-light.png`): A table with Author, SHA1, Message and Date columns and monospace SHA chips. It is much denser. There is some noise: "2,168 Commits" appears in the stat bar and again as "2168 Commits" in the table header. Author names are bold and the avatars are the Gitea default. There is no CI status column.

## Difference table

| Element | GitHub | Branchroom | Better, and why |
| --- | --- | --- | --- |
| Global header | 68px black bar (52px on mobile), search with "/" | 50px light bar, Explore, Help, Sign In, no search | GitHub for search. Branchroom for height and quietness |
| Body font | Mona Sans, 14px, 21px line height (web font) | System font stack (`-apple-system`), 14px, 20px line height. Falls back to a generic sans on this Linux machine | GitHub. It has a deliberate face and rhythm |
| Dark body background | rgb(13, 17, 23) | rgb(30, 31, 32) | Neutral. GitHub's blue-black has more depth |
| Repo tabs | 32px tall, 14px, icons and counts | 41px tall, 14px, counts, no icons | GitHub. Shorter and easier to scan |
| Repo action bar (RSS, Watch, Star, Fork) | Only on the Code tab | Boxed buttons on every page | GitHub. The bar is noise on PR, issue and commit pages |
| Commit summary | Branch dropdown and commit strip | Bordered box: commits, branches, tags | Branchroom. It shows the shape of the repo at a glance |
| Primary button | "New issue": 97x32, 14px, weight 500, radius 6px | "New Issue": 105x35, 13px, weight 400, radius 4px | GitHub. Size and weight read better |
| List title | 16px, weight 500 | 16px, weight 600 | GitHub. Bold on every row flattens the hierarchy |
| PR list row spacing | 67px | 67px | Same density. The weight is the only difference |
| PR list meta | Labels, avatars, check counts on mobile | Base and head branch chips | Branchroom for branch context. GitHub for status |
| Issue sidebar | Yes, about 240px, with personal filters | None | GitHub for filtering |
| Search syntax | Qualifiers such as `is:pr state:open` | Plain text box | GitHub for power users |
| Mobile repo tabs | Code, Issues, More | Code, Issues, "..." (the active tab is hidden) | GitHub. The current section stays visible |
| Avatars | Real photos | Gitea default for migrated accounts | GitHub. Identity matters in threads |
| Migrated comments | No tag | "(Migrated from github.com)" tag | Branchroom. The provenance is honest |
| Deleted branch in PR header | Blue chip | Struck through | Branchroom. It shows the branch is gone |
| Diff totals | "+262 -46" bar | Same bar, plus a sentence with file, addition and deletion counts | Branchroom |
| Files changed DOM | 7,350 elements | 10,882 elements | GitHub. Lighter for a large PR |
| Commit list | Day groups, 65 to 70px rows, Verified badge, CI counts | Table, 37 to 43px rows (repo page), SHA chips, no CI | Branchroom for density. GitHub for context |
| Footer | Not visible on the captured pages | Version, timers, Auto, English, Licenses, API | GitHub. Build data does not belong on every page |
| Profile landing | Overview with README | Repository list | GitHub for a profile. Branchroom for scanning repos |
| Profile avatar | Photo | Generated pattern | GitHub |

## Ten most valuable Branchroom changes, ranked

1. **Un-bold list titles.** Set row titles to weight 500, as GitHub does. Evidence: `br-pulls-desk-light.png`, `br-issues-desk-light.png`, `br-issues-desk-dark.png`. Measured: 16px at weight 600 on Branchroom, 16px at weight 500 on GitHub. Pull request rows are 67px apart on both, so this is a single CSS change with no layout shift.

2. **Give the body a deliberate font stack.** The body currently uses the platform fallback, which looks dated on Linux. Pick a web font, or a stack with explicit faces, and match GitHub's 14px with 21px line height. Evidence: `br-repo-desk-light.png` and `br-pr-desk-light.png` against `gh-pr-desk-light.png`. Measured: computed font family `-apple-system`, line height 20px, against GitHub's Mona Sans with 21px.

3. **Show repo actions on the Code tab only.** Remove RSS, Watch, Star and Fork from the PR, issue, files and commit pages. Evidence: `br-pr-desk-light.png`, `br-issue-desk-light.png`, `br-commits-desk-light.png`, `br-pr_files-desk-light.png`. GitHub's matching pages (`gh-pr-desk-light.png`, `gh-issue-desk-light.png`, `gh-commits-desk-light.png`) have no action row. Measured: three boxed buttons plus RSS at roughly y 64 to 94 on each Branchroom page.

4. **Keep the active repo tab visible on mobile.** At 390px, Branchroom shows Code and Issues and hides Pull Requests behind "...". Evidence: `br-repo-mob-dark.png`, `br-pulls-mob-dark.png`, `br-pr-mob-dark.png`. GitHub keeps a More menu beside Code and Issues (`gh-repo-mob-dark.png`). Both tab strips clip their last tab on the PR page, so the strip needs a visible scroll edge or a wrap.

5. **Shorten the tab bar.** Tabs are 41px tall with 11.5px of vertical padding. GitHub's are 32px. Evidence: `br-repo-desk-light.png`, `br-pulls-desk-light.png`, `gh-repo-desk-light.png`. Measured: the Issues tab is 135x41 on Branchroom and 128x32 on GitHub.

6. **Fix avatars for migrated accounts.** Comment authors, reviewers, participants and commit authors all show the Gitea default. Evidence: `br-pr_files-desk-light.png` (default icon beside semyonfox and coderabbitai), `br-issue-desk-light.png` (participant placeholder), `br-commits-desk-light.png`. GitHub shows photos (`gh-pr-desk-light.png`). Fetch the avatars during migration, or at least give each user a stable generated avatar.

7. **Show review and check status in the PR list.** Rows show labels and branches but not whether a PR is approved or failing. Evidence: `br-pulls-desk-light.png` against `gh-pulls-mob-dark.png`, which shows check counts such as 4/4 and reviewer avatars. This depends on review and check data surviving the import. If it did not, skip it.

8. **Add a global search to the header.** GitHub has a search box with a "/" shortcut (`gh-repo-desk-light.png`). Branchroom's header has Explore, Help and Sign In only (`br-repo-desk-light.png`), and search lives inside each repo. With 177 issues and 545 PRs in one repo, quick jumps matter.

9. **Trim the footer.** The status bar shows the Gitea version string, the page and template timers, and the Auto, English, Licenses and API links. Evidence: `br-pulls-desk-light.png`, `br-issues-desk-light.png`. Keep Licenses and API. Move the version and timers to the admin page, since build hashes and timings are noise for readers.

10. **Cut the DOM on large diffs.** The files view for an 18-file PR renders 10,882 elements, against GitHub's 7,350. Collapse large files by default, or render hunks on demand. Evidence: `br-pr_files-desk-light.png` and `gh-pr_files-desk-light.png`. Numbers from `metrics-v2-br-desk-light.json` and `metrics-v2-gh-desk-light.json`. This matters more on bigger PRs than this one.

Smaller items I did not rank:
- Primary button size and weight: 35px, 13px regular, 4px radius, against GitHub's 32px, 14px medium, 6px radius (`br-pulls-desk-light.png`, `gh-pulls-desk-light.png`).
- "2,168 Commits" shown twice on the commits page (`br-commits-desk-light.png`).
- The grey "No Branch/Tag Specified" box in the issue sidebar (`br-issue-desk-light.png`).
- Two search fields on the repo page (sidebar "Search code" and "Go to file").

## Keep these

- Commit table density (`br-commits-desk-light.png`). Its 37 to 43px rows beat GitHub's 65 to 70px for scanning.
- The diff summary sentence (`br-pr_files-desk-light.png`).
- The migrated-from tags and the struck-through deleted branch (`br-pr-desk-light.png`).
- Base and head branch chips in the PR list (`br-pulls-desk-light.png`).
- The dark theme holds together on the pages I checked (`br-issues-desk-dark.png`, `br-pulls-mob-dark.png`).

## Method and caveats

- Chromium headless through Playwright, anonymous, one capture per page and theme. GitHub is logged out, so its sign-in and sign-up banners are part of what I saw.
- Desktop is 1440x900 in light. Mobile is 390x844 in dark, as asked. Desktop dark captures exist for both sites, but I looked at only `br-issues-desk-dark.png` by eye.
- Metrics come from `metrics-v2-*.json`, measured with the corrected `capture-v2.js`. The older `metrics.json` used selectors that returned zero list rows and picked up "857ms" as a Branchroom heading. Ignore it.
- Branchroom commit row spacing on the commits page is unreliable, because each row has two links. I used the repo page values instead.
- The data differs between the sites. Branchroom shows 5 open PRs (including #722), GitHub shows 4, and the profile counts differ. These are mirror differences, not UI, so I left them out of the ranking.
- Load times are not reported, because the captures were not timed under a fixed network.
- Scripts: `capture-v2.js` (run as `node capture-v2.js <gh|br> <desk|mob> <light|dark>`) and `probe-layout.js` (the bounding-box measurements behind the button and tab figures).
