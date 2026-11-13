# GitHub vs Branchroom: releases for murmur, seol, oghma and tokentelemetry

Run date: 2026-11-13. Read-only and anonymous. No login, no forms.

## Short answer

Both sites get you to the murmur desktop build in two clicks, and both put the latest release in the repo home sidebar. The differences are in the details:

- **GitHub is better for verifying and linking downloads.** Every asset has a sha256 digest with a copy button, and `https://github.com/<owner>/<repo>/releases/latest/download/<file>` works as a stable link. Branchroom has no digests and no GitHub-style `latest/download` route.
- **Branchroom is better for reading release history.** The home sidebar loads in about 2.2 s against about 11 s on GitHub. Every release card is labelled Stable or Pre-Release, asset lists are in the HTML from the first load, and the tag names are not truncated.
- **The worst Branchroom gaps are small and cheap to fix.** The newest release is never labelled "Latest" in the releases list (only in the sidebar), sizes round to whole MiB above 10 MiB, and the footer shows a dev build string and timings on every page.

Recommendation: add the GitHub-style `latest/download` route first. It is one route on an existing handler, and it is the only change that breaks a link someone already uses (see the URL section).

## Method

- Playwright 1.64.1 headless Chromium. The worktree has no top-level `playwright` package, so the script loads it from `node_modules/.pnpm/playwright@1.64.1`.
- Viewports 1440x900 and 390x844 (touch). Light and dark via `colorScheme`.
- Click counts come from the scripted path. `docY` is the element's distance from the top of the page in CSS px. Screens are `docY / 900` on desktop and `docY / 844` on mobile.
- The think-aloud sections are reconstructed from the scripted steps, the measured offsets and the screenshots. They mark where the page gave a choice or a gap. They are not a timed study with a live person.
- Timing is three cold loads each. GitHub was reached over the internet, Branchroom over the LAN, so the download times are not comparable and I do not claim Branchroom downloads faster.
- Downloads were saved to `scratchpad/haiku-releases/dl/gh` and `dl/br`, then deleted at the end of the run. Sizes were checked against the API.

Screenshots are in `scratchpad/haiku-releases/shots/` as `gh-<viewport>-<theme>-<name>.png` and `br-...`. Measured data is in `scratchpad/haiku-releases/results.json`.

## Task 1: latest release main download (murmur)

I took "main download" to mean `Murmur-linux-x86_64.AppImage`, because the release notes call it the Linux desktop build and the APK is the Android build. If you meant the APK, the click count is the same.

### GitHub, desktop

1. Repo home. The Releases block is still skeleton placeholders for the first few seconds. I waited for it, and it resolved about 11 s after navigation (median of three cold loads). It sits at `docY 592`, so it is visible without scrolling.
2. Click 1: "Murmur 0.1.22 Latest" in the sidebar. This goes straight to the release page, which I expected. I also considered the "Releases 5" heading, which would have added a click.
3. Release page. The AppImage is at `docY 777`, visible without scrolling. Assets are open for the latest release. Each row has a sha256 with a copy button, so I could verify the file in the same place.
4. Click 2: AppImage. The download starts. 216,324,600 bytes in 32.3 s.

Total: **2 clicks, 0 px scrolled**. The hesitation was the sidebar's "Latest" and the "Releases" heading, which both lead to the release.

### Branchroom, desktop

1. Repo home. The Releases block is in the HTML and visible at `docY 484` after about 2.2 s.
2. Click 1: "Murmur 0.1.22" in the sidebar. The sidebar shows a "Latest" pill but the release page shows "Stable" (see Task 5).
3. Release page. The Downloads box is open and the AppImage is first at `docY 535`. I had no digest to check the file against, and the order looked accidental because it puts the uppercase name before the lowercase ones.
4. Click 2: AppImage. 216,324,600 bytes in 4.9 s (LAN).

Total: **2 clicks, 0 px scrolled**.

### Mobile (390 px)

- GitHub: the sidebar Releases block is at `docY 10,920`, about 13 phone screens down, below the README.
- Branchroom: the block is at `docY 11,441`, about 13.6 phone screens down.
- Once there, the click path is the same two clicks on both sites. Neither site overflows horizontally at 390 px (0 px overflow on home, releases and release pages).

This is a shared weakness. A phone user has to scroll past the whole README to find the latest build.

## Task 2: release notes for an older version (murmur 0.1.19)

### GitHub

1. Click 1: "Releases" in the sidebar, `docY 556`. This is the only route to the list because GitHub has no top tab for releases.
2. The list is long. "Murmur 0.1.19" is at `docY 2,181`, about 2.4 screens down. Older releases have their Assets collapsed.
3. Click 2: "Murmur 0.1.19". The notes heading shows at `docY 267` on the release page after about 4.8 s. The heading is an h1, and the notes read as a normal article.

Total: **2 clicks, about 2.4 screens of scrolling in the list**.

### Branchroom

1. Click 1: "Releases 5" tab at `docY 98`, which is always visible. This is easier to find than the sidebar heading on GitHub.
2. The list title for 0.1.19 is at `docY 1,635`, about 1.8 screens down. The list is shorter (2,509 px tall against 3,400 px on GitHub).
3. Click 2: "Murmur 0.1.19". The page shows "Stable" and "16 commits to main since this release", which tells me how far the notes are from the current head.

Total: **2 clicks, about 1.8 screens of scrolling in the list**.

## Task 3: platform asset among several (tokentelemetry, linux amd64)

### GitHub

1. Click 1: "TokenTelemetry 0.1.0 Latest" in the sidebar at `docY 773`.
2. The release page has a yellow banner: "This repository was archived by the owner on Nov 9, 2026. It is now read-only." This is a repo state, not a site difference, but it changes what a downloader can expect.
3. The Assets heading is at `docY 2,546`. The asset rows are not in the page until the section is scrolled into view. A scroll to the middle of the page did nothing, and the list appeared about 2.4 s after I scrolled to the heading. For a moment I thought the assets were missing.
4. The seven archives are sorted by name, so linux amd64 is the fourth row (after `checksums.txt`, `darwin_amd64` and `darwin_arm64`). It is at `docY 2,738`, so I scrolled 1,865 px before clicking.
5. Click 2: `tokentelemetry_linux_amd64.tar.gz`. 10,851,542 bytes in 0.6 s.

Total: **2 clicks, 1,865 px of scrolling, and a 2.4 s wait for the asset list**. The platform is only visible in the file name. There is no platform column.

### Branchroom

1. Click 1: "TokenTelemetry 0.1.0" in the sidebar at `docY 538`.
2. The Downloads box is open and in the HTML from the first load. The rows run from `docY 2,226` to `2,454`.
3. I scrolled 1,342 px so that linux amd64 (`docY 2,340`) was in view.
4. Click 2: the same file. 10,851,542 bytes in 0.4 s.

Total: **2 clicks, 1,342 px of scrolling, no wait**. The rows show "10 MiB" for darwin amd64, darwin arm64 and linux amd64, so I had to read the names to tell them apart (see Task 5 on sizes). Like GitHub, the platform is only in the file name.

### seol and oghma (same paths)

- **seol (latest v3.0.0, 10 assets).** Both sites reach the release in one click from the sidebar. The ten assets are `checksums.txt` plus nine binaries across darwin, freebsd, linux and windows. The main download is not obvious on either site, so you have to know the platform.
- **oghma (prerelease only, 7 releases).** There is no stable latest on either site. GitHub's sidebar has only a "Releases 7" count, and the first entry is a "Pre-release" in the list. On GitHub the list collapses every release's Assets, including the newest one (`Assets 3`), so a direct download from the list takes one extra click. Branchroom leaves the newest release open. From the home page both sites take two clicks to the apk (count link, then the release). Stable-latest URLs return 302 to the list on GitHub and 404 on Branchroom (see the URL section).

## Task 4: copy a direct download link and check for a stable latest URL

### Copy the link

- **GitHub:** right-click the asset, then "Copy link address". That is 2 actions. The copied link is relative to github.com and works anywhere: `https://github.com/semyonfox/murmur/releases/download/v0.1.22/Murmur-linux-x86_64.AppImage`. The only copy buttons are next to each digest, not next to the link.
- **Branchroom:** right-click, then "Copy link address". Also 2 actions. The copied link is absolute and uses the LAN address: `http://10.0.0.5:3080/semyon/murmur/releases/download/v0.1.22/Murmur-linux-x86_64.AppImage`. That link only works on your network. If you paste it into a message for someone outside the LAN, it will not load.

### Stable latest URL

Every URL below was tested with curl. For GitHub the first response is a 302 to the tag path, so I followed the redirect to check that the file exists.

| URL | First response | Final response |
| --- | --- | --- |
| `https://github.com/semyonfox/murmur/releases/latest` | 302 to `https://github.com/semyonfox/murmur/releases/tag/v0.1.22` | n/a |
| `https://github.com/semyonfox/murmur/releases/latest/download/Murmur-linux-x86_64.AppImage` | 302 to `.../releases/download/v0.1.22/Murmur-linux-x86_64.AppImage` | 206, `Content-Range: bytes 0-0/216324600` |
| `https://github.com/semyonfox/murmur/releases/latest/download/does-not-exist.zip` | 302 (GitHub redirects missing names too) | 404 after following |
| `https://github.com/semyonfox/oghma/releases/latest` | 302 to `https://github.com/semyonfox/oghma/releases` (no stable release) | n/a |
| `https://github.com/semyonfox/oghma/releases/latest/download/x.apk` | 404 | n/a |
| `https://github.com/semyonfox/murmur/archive/refs/tags/v0.1.22.zip` | 302 | 200 |
| `http://10.0.0.5:3080/semyon/murmur/releases/latest` | 303 to `http://10.0.0.5:3080/semyon/murmur/releases/tag/v0.1.22` | n/a |
| `http://10.0.0.5:3080/semyon/murmur/releases/latest/download/Murmur-linux-x86_64.AppImage` | **404** (no route) | n/a |
| `http://10.0.0.5:3080/semyon/murmur/releases/download/latest/Murmur-linux-x86_64.AppImage` | 200 (HEAD) | 206, `Content-Range: bytes 0-0/216324600` |
| `http://10.0.0.5:3080/semyon/murmur/releases/download/latest/murmur-0.1.22.apk` | 206 | n/a |
| `http://10.0.0.5:3080/semyon/murmur/releases/download/latest/does-not-exist.zip` | 404 | n/a |
| `http://10.0.0.5:3080/semyon/murmur/releases/download/v0.1.22/Murmur-linux-x86_64.AppImage` | 200 (HEAD) | n/a |
| `http://10.0.0.5:3080/semyon/murmur/releases/tag/latest` | 404 | n/a |
| `http://10.0.0.5:3080/api/v1/repos/semyon/murmur/releases/latest` | 200 (JSON) | n/a |
| `http://10.0.0.5:3080/semyon/murmur/archive/v0.1.22.zip` | 200 | n/a |
| `http://10.0.0.5:3080/semyon/murmur/archive/v0.1.22.tar.gz` | 200 | n/a |
| `http://10.0.0.5:3080/semyon/oghma/releases/latest` | 404 (no stable release) | n/a |
| `http://10.0.0.5:3080/semyon/oghma/releases/download/latest/oghmanotes-alpha.apk` | 404 | n/a |
| `http://10.0.0.5:3080/semyon/tokentelemetry/releases/latest` | 303 to `.../releases/tag/v0.1.0` | n/a |
| `http://10.0.0.5:3080/semyon/seol/releases/latest` | 303 to `.../releases/tag/v3.0.0` | n/a |

What this means:

- Branchroom has the `download/latest/<file>` alias, but not GitHub's `latest/download/<file>` form. So a link copied from GitHub docs, a README badge or an install script returns 404 on Branchroom.
- The alias is implemented in `routers/web/repo/repo.go:294` (`RedirectDownload`). It only applies when no release has the tag `latest`, and it resolves through `GetLatestRelease`, which ignores prereleases (`models/repo/release.go:358`).
- The GitHub-style route is missing from `routers/web/web.go:1516-1519`, which only registers `/releases/latest`, `/releases/download/{vTag}/{fileName}` and the attachment routes.
- I did not search Branchroom's docs for the `download/latest` alias, so I cannot say whether it is documented. It works in this build (`Version: 1.28.0+dev-1074-g7a33e1c1fe`), but a future upgrade could break it.
- The GitHub 302 proves nothing on its own. A made-up file name also gets a 302, so you have to follow the redirect to know whether the asset exists.

## Task 5: comparisons

| Element | GitHub | Branchroom | Better and why |
| --- | --- | --- | --- |
| Newest release in list | "Latest" pill on the newest card (1 of 5) | "Stable" on all 5 cards, no "Latest" | GitHub. The sidebar says "Latest" but the list does not, so the two disagree. `templates/repo/release/list.tmpl:37` never passes `IsLatest`. |
| Older releases in list | No status pill | "Stable" pill on each | Branchroom. Every release shows its status. |
| Prerelease label | "Pre-release" | "Pre-Release" (capital R) | Tie on meaning. Different casing on each site. |
| Releases list height | 3,400 px for 5 releases | 2,509 px | Branchroom. Less scrolling for the same content. |
| Releases list search | "Find a release" box | None | GitHub, but it only matters once a repo has many releases. |
| Releases list left column | "On this page" TOC of every release | Tag name, commit hash and Compare button | GitHub for navigating long lists. Branchroom for the per-release commit data. |
| Tag name display | Truncated in the meta line ("android-alp...") | Full, wraps in the left column | Branchroom. |
| "N commits to main since this release" | Not shown on the list page for murmur | On all five cards | Branchroom. |
| Asset sha256 | Yes, on every asset, with copy button | None | GitHub, for checking downloads. |
| Asset size | Three significant figures, but binary values under "MB". The 23.5 MB APK is 23.5 MiB | Whole MiB above 10 MiB. The 23.5 MiB APK shows "24 MiB", and 10.47, 10.35 and 10.01 MiB all show "10 MiB" | GitHub for precision, Branchroom for the honest unit. Neither is ideal. |
| Asset size for small files | "251 Bytes" | "251 B" | Tie. |
| Asset sort order | Case-insensitive: `android-update.json`, `murmur-0.1.22.apk`, `Murmur-linux-...AppImage` | Case-sensitive: `Murmur-linux-...AppImage` first, then the lowercase names | GitHub, predictable. Branchroom happens to put the desktop build first. |
| Asset list default state | Newest open, older collapsed, all collapsed on an all-prerelease list | Newest open; older collapsed | Branchroom for prerelease-only repos. |
| Asset list render | On the tokentelemetry tag page, rows appeared about 2.4 s after scrolling to the heading. On murmur they were in the page at 1.5 s without scrolling. | In the HTML on load on both repos | Branchroom, where the rows were lazy. |
| Per-asset download count | Not shown | Not shown | Tie. The API has counts, but each site counts separately (see notes). |
| Source archives | zip and tar.gz, 302 then 200 | zip and tar.gz, 200 directly | Branchroom, one redirect fewer. |
| Release page height (v0.1.22) | 1,027 px | 900 px (fits one 1440x900 screen) | Branchroom. |
| Tags page | 5 rows, each with zip, tar.gz, Notes and Downloads links | 5 rows, each with ZIP, TAR.GZ and Release details, plus a "Search tags" box | GitHub for one-click downloads from a tag. Branchroom for search. |
| Sidebar on repo home | Releases block with "Latest" pill and "+ 4 releases" link | Releases block with "Latest" pill, no more-link | GitHub, for the more-link. |
| Sidebar load time (median of 3) | 11.1 s (13.7, 11.0, 11.1) | 2.2 s (2.2, 2.2, 2.1) | Branchroom. Caveat: GitHub over the internet, Branchroom on the LAN. |
| Footer | Standard | "Powered by Gitea, Version 1.28.0+dev-1074-g7a33e1c1fe, Page: 69ms, Template: 5ms" on every page | GitHub. A dev build string and timings are not for public readers. |
| Dark theme | Background `rgb(13, 17, 23)`, legible | Background `rgb(30, 31, 32)`, legible | Tie. |
| Mobile horizontal overflow (390 px) | 0 px | 0 px | Tie. |
| Mobile position of latest release block | `docY 10,920` | `docY 11,441` | GitHub, by about 520 px. Both are too far down. |
| Copy link | Relative, works anywhere | Absolute LAN URL, works only on the LAN | GitHub. |

### Things Branchroom gets right

- The sidebar loads roughly five times faster in this test.
- On the tokentelemetry tag page, GitHub's asset rows arrived only after a scroll. Branchroom's were in the HTML on load.
- Every release shows its own status, and the tag names are readable.
- The tags page has a search box.
- Mobile layouts fit the screen, with no horizontal scrolling.

## Ranked: eight most valuable Branchroom changes

1. **Add GitHub's `/releases/latest/download/<file>` route.** Measured: `http://10.0.0.5:3080/semyon/murmur/releases/latest/download/Murmur-linux-x86_64.AppImage` returns 404. GitHub returns 302 then 206. The handler already exists as the `download/latest` alias (`repo.go:294`), so the change is one route in `web.go:1516-1519`. Screenshot: none (HTTP evidence). The copy-link context is `br-desk-light-03-release-assets.png`.

2. **Label the newest release "Latest" in the list and on the release page.** Measured: the list has 0 "Latest" and 5 "Stable" on Branchroom, against 1 "Latest" on GitHub. The sidebar already has the data (`home_sidebar_bottom.tmpl:15` passes `IsLatest`), so it is a one-line template change in `release/list.tmpl:37`. Screenshots: `gh-desk-light-02-releases-list.png` and `br-desk-light-02-releases-list.png`. The Stable pill on 0.1.22 is in `br-desk-dark-03-release-assets.png`.

3. **Show a sha256 digest and copy button for each asset.** Measured: GitHub shows 3 digests on murmur and 9 on tokentelemetry, including `checksums.txt`. Branchroom shows none. Digests are the easiest way to check a downloaded AppImage or APK. Screenshots: `gh-desk-light-03-release-assets.png` and `br-desk-light-03-release-assets.png`, and for tokentelemetry `gh-desk-light-05-tokentelemetry-assets.png` and `br-desk-light-05-tokentelemetry-assets.png`. This is more work because Gitea does not store a digest per attachment, so you would compute and store it at upload. That needs a migration, so it is a design decision.

4. **Keep three significant figures in asset sizes.** Measured: Branchroom shows `10 MiB` for three tokentelemetry archives that are 10.47, 10.35 and 10.01 MiB, and `24 MiB` for the 23.51 MiB APK. Screenshots: `br-desk-light-05-tokentelemetry-assets.png` and `br-desk-light-03-release-assets.png`. This is a small template change.

5. **Surface the latest release near the top on phones.** Measured: the latest release block is at `docY 11,441` on Branchroom, about 13.6 phone screens down. GitHub is about 12.9. Screenshots: `br-mob-light-01-home-releases.png` and `gh-mob-light-01-home-releases.png`. A "Latest: v0.1.22, Download" line under the repo header would fix this. The placement is a product call, so check with yourself before building it.

6. **Add a "+ N releases" link to the home sidebar.** Measured: GitHub shows "+ 4 releases" under the latest release. Branchroom shows nothing, so the only route to older releases is the Releases tab. Screenshots: `gh-desk-light-01-home-releases.png` and `br-desk-light-01-home-releases.png`. Small change.

7. **Hide the dev version and debug timings from the footer on public repos.** Measured: `Version: 1.28.0+dev-1074-g7a33e1c1fe Page: 69ms Template: 5ms` on every page in both themes and both viewports. Screenshots: `br-desk-light-03-release-assets.png` and `br-mob-light-03-release-assets.png` (footer at the bottom). Keep it for dev builds or signed-in admins.

8. **Link each tag to its downloads on the tags page.** Measured: GitHub has a "Downloads" link on every tag row (5 of 5 on murmur). Branchroom has "Release details" only. Screenshots: `gh-desk-light-06-tags.png` and `br-desk-light-06-tags.png`. Small change, and the search box on Branchroom's tags page should stay.

## Notes on the run

- **Memory.** During the run the host was at 28 GB of 31 GB with swap full, and `earlyoom` killed the Chromium process twice. The big processes (`next-build`, `java`, `postgres`) belong to other work, so I did not touch them. I split the screenshot pass into one process per site and theme, and I used `--js-flags=--max-old-space-size=192`. All 56 screenshots completed.
- **Lazy assets on GitHub.** My first tokentelemetry run failed because I scrolled to the middle of the page, which did not trigger the asset list. The corrected run scrolls to the Assets heading and waits for the rows.
- **Download counts changed.** My runs and range checks hit the asset URLs, so the counters moved. Murmur AppImage: GitHub 2 to 5, Branchroom 4 to 6. Murmur APK on Branchroom 4 to 5. Tokentelemetry linux amd64: GitHub 3 to 4, Branchroom 2 to 3. Each site counts its own downloads, so the numbers are not comparable.
- **Output path.** You gave `.../4396d314-.../scratchpad/haiku-releases.md`, but this session's scratchpad is `.../f204b61e-.../scratchpad`. I wrote to the path you gave.
- **Not tested.** I did not test Branchroom's comment or reaction features, the feed (`releases.rss`), or login-only pages. I did not look at GitHub's Assets collapse state on murmur's tag page beyond the list. The timing numbers are three runs on one machine.
