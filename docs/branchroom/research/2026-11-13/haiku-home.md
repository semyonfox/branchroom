# GitHub vs Branchroom: repo home page think-aloud comparison

Read-only, anonymous, no forms submitted. The only click was opening the Code (clone) dropdown on murmur to see what it holds.

Screenshots: a local scratch folder that was not kept
Full pages are named `gh-<repo>-<desktop|mobile>-<light|dark>.png` and `br-...`. The first screen of each desktop and mobile shot is in `folds/`, named `fold-<site>-<repo>-<viewport>-<scheme>.png`.
Raw boxes: `measurements-gh.json` and `measurements-br.json` in the same folder.

## Bottom line

Branchroom already gets the README closer than GitHub does. On murmur at 1440x900 the README body starts at y=696, inside the first screen. On GitHub it starts at y=911, just below it. On oghma, which has a longer file list, both are below the fold, and Branchroom's is still 363px higher (2065 against 2428).

The real gaps are smaller and fixable: the clone URL is cut off, the mobile commit column is clipped, the contributors block is missing, and the write controls look live to a signed-out viewer.

My recommendation for the README is a side-by-side layout on wide screens, with the file list on the left and the README on the right. Collapsing the file list would help less, because oghma would still need about 1,900px of scroll.

---

## Repo home page

### GitHub (murmur, desktop)

My eye lands on the green Code button first, then the About panel on the right. Above the file list there is a black global bar, a breadcrumb, and an icon tab strip, which together take about 170px before any content. The file list is tidy: folder icons, truncated commit messages, right-aligned dates. The README card shows at the very bottom of the screen with only its header, so I have to scroll to read the project description. The language bar is there at the edge, but the legend is off screen. Contributors with avatars are a useful touch. Dark mode is solid.

### Branchroom (murmur, desktop)

My eye goes to the stats strip, "51 Commits | 6 Branches | 5 Tags", which I like straight away. The blue Code button sits at the top right of the file list. The commit line is one thin line with the full message. The README text is already on screen, which is what I want. The sidebar starts with a "Search code" box above the description, and it sits right next to "Go to file", so it reads as redundant. Releases and the language legend are visible. I notice there is no contributors block. Dark mode works.

### GitHub (murmur, mobile 390px)

About comes first, then the Code button and the file list. Commit messages are gone and only the ages are left, so the list reads like a phone app. The README is about 1,100px down.

### Branchroom (murmur, mobile 390px)

The header is taller, with a title, three icon buttons and the tabs. Description, stats, branch picker, Go to file, Add File and Code stack into three rows. The file list keeps commit messages, but they are clipped to roughly 105px, so each one reads as "chore(androi...". That looks noisy.

## Folder page (`scripts`)

### GitHub

A Files tree sits on the left, with breadcrumb, commit strip and table on the right. Rows are 41px apart. Commit messages are truncated with an ellipsis at about 480px. The file icons are generic. It is clean and easy to scan.

### Branchroom

The breadcrumb includes a copy icon, and there are Go to file, Add File, History and a Files tree on the left. Python files have language icons. The commit messages are full length, with no truncation at 1440px. Rows are 37px apart, so the list is a bit denser. It is more informative. Add File is visible to a signed-out viewer.

## File page (`scripts/benchmark_cleanup.py`)

### GitHub

The code block starts at y=348 with line numbers. The toolbar has Code and Blame tabs, Raw, copy and download, and a summary of "279 lines (254 loc) · 12.4 KB". Nothing is in the way.

### Branchroom

A yellow "This file contains ambiguous Unicode characters" banner sits between the file header (bottom at y=315) and the code (top at y=411). That is about 96px of banner, and it pushes the first code line to y=414, against 348 on GitHub. The banner has a close button, so it can be dismissed, but it shows on every such file. The toolbar is busy: Raw, Permalink, Blame, History, Escape, download, copy, RSS, edit and delete. The "280 lines | 12 KiB | Python" line is nicer than GitHub's. The edit and delete icons look live to a signed-out viewer. Their links exist, and a tooltip says you need a fork or write access. I did not click them.

---

## Measured differences (desktop 1440x900 unless noted, light scheme, scroll 0)

| Element | GitHub | Branchroom | Better, and why |
| --- | --- | --- | --- |
| Global chrome before content | Black nav 0 to 69, breadcrumb to 84, tabs 120 to 168 | Top bar 0 to 50, repo header 50 to 98, tabs 98 to 139 | Branchroom. 29px more of the first screen goes to content. |
| README body, murmur | y=911 (below fold). Card header visible near 855 | y=696, H1 at 758, text visible from about 835 | Branchroom. The README is on the first screen. |
| README body, oghma | y=2428 | y=2065 | Branchroom, but both are far down on long repos. |
| File table top, murmur | y=363 | y=237 | Branchroom. |
| File row pitch | 41px | 37px | Branchroom. 11 root rows fit in 449px. |
| Branch picker | x=112, y=261, row also holds Branches and Tags links | x=80, y=197, w=99. Counts moved to a strip at y=154 | Branchroom. The strip is clearer. |
| Stats strip | none (counts are links beside the picker) | y=154 to 188, full width | Branchroom. |
| Code (clone) button | Green, x=906, y=261, 110 by 32 | Blue, x=959, y=197, 105 by 30 | Tie. Colour is a matter of taste. |
| Clone field width | 332px, full URL fits | 236px, content is 267px, so the end of the URL is cut off | GitHub. Copying a truncated URL is a real annoyance. |
| Clone options | HTTPS, GitHub CLI, Copilot app, ZIP | HTTP, Gitea CLI, VS Code, VSCodium, IntelliJ, ZIP, TAR.GZ, BUNDLE | Branchroom has more options. |
| Latest commit row | Commit row starts at 309, count sits at the far right | Single line at 246 (h24), count in the strip | Branchroom. The count is easier to find. |
| Full commit messages on folder rows | Truncated with ellipsis | Full, at 1440px | Branchroom. |
| About and description, murmur | "About" heading at 245, description at 297 | "Description" heading at 203, text at 230 | Branchroom is higher. Naming differs only cosmetically. |
| Description, oghma | y=297, h=96 | y=230, h=80 | Branchroom is higher. |
| Website, oghma | y=407 | y=317 | Branchroom. |
| Topics, oghma | y=438 | y=348 to 399 (two rows) | Branchroom is higher. |
| Sidebar search | none | "Search code" box, y=154 to 188 | GitHub. It duplicates Go to file. |
| Releases block, murmur | y=539, h=141, with "+4 releases" | y=451, Latest badge | Tie. |
| Contributors | y=680, h=149, two avatars | none | GitHub. It is missing from the Branchroom home page. |
| Packages block | y=643 block, 87px tall on murmur (present, contents not checked) | none (tab only) | Branchroom is quieter. Debatable. |
| Language bar and legend, murmur | Section at 829. Bar at about 883, legend below the fold | Header at 549, bar at about 580, legend 600 to 680 | Branchroom. The full legend is on the first screen. |
| Language bar, oghma | Section at 981, below the fold | Header at 570 | Branchroom. |
| Visibility badge | "Public" next to the repo name | none | GitHub. |
| Desktop page height, murmur / oghma / seol / dotfiles | 5492 / 6367 / 9937 / 5615 | 5004 / 5923 / 9405 / 5258 | Branchroom is shorter in every case. |
| Mobile About, murmur | About at 252, description at 304, above file list | Description at 261, above file list | Tie. |
| Mobile tool row | Branch picker and Code share a row at 489. File table at 591 | Branch picker at 489, Code on its own row at 564. File table at 604 | GitHub. A small gain. |
| Mobile commit column | Hidden. Only the age is shown | Shown, clipped to about 105px of text | GitHub. |
| Mobile README | 1138 | 1060 | Branchroom, slightly. |
| Mobile page height, murmur | 11529 | 11825 | GitHub. |
| Language icons in file lists | Generic file icon | Language icons (Python, etc.) | Branchroom. |
| Ambiguous Unicode warning | None on the file page | Yellow banner, about 96px, pushes code to y=414 (GitHub: 348) | GitHub for normal files. The banner is a useful signal, but it is too loud. |
| Write controls for signed-out viewers | Not shown | Add File, edit and delete visible. Tooltips explain the requirement | GitHub. |
| File meta line | "279 lines (254 loc) · 12.4 KB" | "280 lines \| 12 KiB \| Python" | Branchroom. Shows the language. |
| Dark mode | Works | Works | Tie. |
| Folder tree sidebar | Present | Present | Tie. |

---

## Ten most valuable Branchroom changes, ranked

Each change is listed with the screenshots that show it and the pixel detail. Positions are page coordinates at scroll 0.

1. **Bring the README alongside the file list on wide screens.** On oghma, the README starts at y=2065 (`br-oghma-desktop-light.png`), which is 1,165px below the fold. On murmur it is at 696 (`br-murmur-desktop-light.png`). Put the README in a right-hand column at 1280px and up, with the file list on the left. The README would then start beside the file table (top at y=237), so its opening lines are on the first screen on any repo. I have not built this, so the 237 is the target position, not a measured one. Collapsing the file list would help less, because oghma's README would still start below the fold.

2. **Stop truncating the clone URL.** The field is 236px wide and the URL needs 267px, so the end of `murmur.git` is cut off (`br-clone-open-desktop-light.png`). GitHub's 332px field fits the whole URL (`gh-clone-open-desktop-light.png`). Widen the popover to fit the longest URL, or let the field wrap.

3. **Remove or move the commit message on narrow screens.** At 390px the message column shows about 105px of text, so it reads "chore(androi..." (`br-murmur-mobile-light.png`, row at about y=653). GitHub hides the column and shows only the age (`gh-murmur-mobile-light.png`). Hide the message below about 600px, or put it on a second line under the file name.

4. **Add a contributors block to the home sidebar.** GitHub shows avatars at y=680 to 829 on murmur (`gh-murmur-desktop-light.png`). Branchroom's sidebar goes straight from Releases (y=451) to Languages (y=549) (`br-murmur-desktop-light.png`). Add a block with avatars between them, and link it to the existing Activity page, where Gitea already shows contributors.

5. **Make write controls look unavailable to signed-out viewers.** Add File, the edit pencil and the delete trash icon look live on the file page (`br-scripts-file-fold.png`, icons on the right of the toolbar at about y=290, read from the screenshot). The links only explain themselves in a tooltip. Hide them when the viewer cannot write, or give them the disabled style with the tooltip visible.

6. **Make the ambiguous Unicode notice smaller.** The banner takes about 96px and pushes the first code line from 348 (GitHub) to 414 (`br-scripts-file-fold.png`, `gh-scripts-file-fold.png`). Show a one-line notice in the file header instead, and keep the Escape button there. The warning is still useful, but it should not sit on top of the code.

7. **Remove the duplicate search from the top of the home sidebar.** The "Search code" box sits at y=154 to 188 (`br-murmur-desktop-light.png`), directly above the description, and it duplicates Go to file in the toolbar at y=197. Drop it from the home sidebar, or merge it into Go to file.

8. **Put Code on the same row as the branch picker on mobile.** On a 390px screen, Branchroom puts the Code button on its own row at y=564, and the file table starts at 604. GitHub puts the branch picker and Code on one row at 489, and the file table starts at 591 (`br-murmur-mobile-light.png`, `gh-murmur-mobile-light.png`). This saves about 13px and removes a stacked row.

9. **Show a visibility badge next to the repo name.** GitHub shows "Public" beside the name (`gh-murmur-desktop-light.png`, about x=243 to 290, y=192 to 211). Branchroom shows nothing, so a visitor cannot tell at a glance whether the repo is public. Add the badge next to the title in `repo-header`.

10. **Tidy the footer on public pages.** Every page ends with the Gitea version string, language picker and theme picker in the footer (`br-murmur-desktop-light.png`, bottom at y=882). These are not above the fold, so the value is low. Hide the version string from anonymous viewers if you want the page to look more finished.

Things Branchroom already does better, which should be kept: the stats strip, full commit messages on desktop, the language legend on the first screen, the shorter desktop page height, and the language icons in file lists.

---

## Method and gaps

- **Browser setup.** The worktree has no `node_modules`, so the script used Playwright 1.64.1 from `another local checkout`, with Chromium from `~/.cache/ms-playwright`.
- **Memory pressure.** The host had about 28 of 31 GB in use, with `earlyoom` running, and it killed the browser twice mid-run. I added relaunch and retry logic (`capture2.cjs`). The final GitHub run completed with no retries. I did not touch the other processes.
- **Measurement.** Boxes come from Playwright `boundingBox()` and `getBoundingClientRect()` at scroll 0. Visible-only matching was needed to avoid hidden duplicate sections.
- **Not measured in the main run.** The GitHub Packages and Used by blocks came back null from the main run. Their positions (643 and 730, each 87px tall) come from a separate DOM dump, not from `measurements-gh.json`.
- **Estimated from screenshots.** The position of the Branchroom language bar (about 580) and legend (600 to 680), the banner height, and the cut point on the mobile commit column (about x=255) were read from screenshots, not measured directly.
- **Dark scheme.** Both sites honour `prefers-color-scheme`. The dark screenshots match the light measurements, as expected.
- **Signed-out only.** Nothing was logged in, nothing was submitted, and the edit, delete and Add File links were read from the DOM but not followed.
