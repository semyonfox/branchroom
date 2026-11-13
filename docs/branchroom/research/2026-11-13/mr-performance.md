# Branchroom merge request performance: research, baseline and plan

Date: 2026-11-13. Code read at `sync/migrations-split` (66fef628c5). Live server `http://10.0.0.5:3080`, version `1.28.0+dev-1087-g717eb4092d`, anonymous GET only. No product code was edited.

## Summary

Branchroom already has the architecture GitLab is moving to: server-rendered HTML that streams, enhanced by a small amount of JavaScript. Script time is 0.2 to 0.5 s on every page measured. The problems are elsewhere:

1. **The first byte waits about 2 s on any non-trivial diff.** `git diff` itself takes 0.02 to 0.65 s. The rest is full-file syntax highlighting of both sides of every file, done on every request, with no cache, before any HTML is sent.
2. **The HTML is very heavy and is sent uncompressed.** A diff row costs a median 629 bytes and about 16 elements. 152 KB of visible code becomes 1.11 MB of markup. `ENABLE_GZIP` is off, so a 100-file page is 5 to 11.6 MB on the wire. gzip would cut that about 20 times.
3. **The browser lays out everything at once.** There is no `content-visibility`, no containment, no virtualisation. 100 files of a large PR is 248,000 elements and 7 to 8 s of style and layout. Injecting one CSS rule in the browser cut that to 0.4 to 0.8 s.
4. **Large PRs need manual paging.** Only the first 100 files render. The rest need a "Show more" click per 100 files, each taking 4 to 9 s. A 336-file PR needs three clicks and ends at 372,000 elements.
5. **The file view has no practical cap.** A 2.2 MB JavaScript file renders as 18.4 MB of HTML and 391,000 elements, takes 21 s to become interactive, and scrolls at a median 50 ms per frame.

The conversation page and the commits lists are fine (under 1.1 s to interactive on a loaded host).

The recommended order is at the end. The first four items are small and should bring a 300-file PR from roughly 6 s to under 2 s to interactive. The next two are needed to reach 1 s.

## Part 1: how the fast review UIs stay fast

### What each product does

**GitHub (React files-changed view, default since January 2026)**

- v1 of the React view rendered 10 to 15 DOM elements, 8 to 13 React components and 20 or more event handlers per diff line. On extreme PRs GitHub saw a JS heap over 1 GB and more than 400,000 DOM nodes. [1]
- v2 cut components per line from 8 to 2, moved comment and menu state out of the line component, replaced per-line handlers with one delegated handler reading `data-` attributes, and used `Map` lookups for comments. On a 10,000-line split diff: components rendered 183,504 to 50,004, memory 150 to 250 MB down to 80 to 120 MB, INP 450 ms to 100 ms (M1 with 4x slowdown). DOM nodes only fell 10% (200,000 to 180,000). [1]
- Removing two `<code>` wrappers from line number cells is called out as worth it: two nodes per line is 20,000 nodes on 10,000 lines. [1]
- Virtualisation (TanStack Virtual) is used only for the largest PRs (over about 10,000 diff lines). It gave a 10x cut in heap and DOM nodes and INP of 40 to 80 ms instead of 275 to 700 ms. [1]
- Cost of virtualisation, stated by GitHub: find-in-page, select-all, print and extensions that read the diff stop working. [2]
- Server side: "hydrate only visible diff lines" and progressive diff loading with background fetches. [1]
- Older but still the base of their loading model (2016): a table of contents from `git diff-tree --raw`, then patches in batches. The first request carries at most 400 lines or 20 KB of patch. The rest loads automatically. Their numbers: 81% of viewed diffs touch fewer than ten files and 90% have under 1,000 patch lines. p99.9 improved by about 3.5 s. [3]
- The Copilot app diff surface (September 2026) opens a 2,200-file, million-line PR. It relies on fixed row heights so all code geometry is known before paint, keeps comment blocks in a separate lazily measured geometry, highlights off the main thread (plain text first, colour later), streams structure before content, and keeps the last few diffs cached. [4]
- Code view (2023): a virtualised highlighted overlay on top of a plain textarea holding the whole file, so native find still works. Before that, an 18,000-line file took React nearly 27 s to build. [5]

Known problems, from users:

- Tab switch from Conversation to Files changed taking over 5 s, slower than a full page load. Thousands of hidden "+" buttons each with an inline SVG. Around 100,000 DOM nodes. [6]
- Reports of 5 to 60 s for common review actions on 70 to 100 file PRs. [7]
- The new view stops at 1,000 files with no way to load more. [8]
- Files collapsed for performance are easy to miss when searching the page. [9]

**GitLab**

- History: full server render (slow first paint), then a Vue app with JSON batches, then virtual scrolling. Batches start small and grow to 30 files per request. Virtual scrolling took local Total Blocking Time from over 20 s to about 4 s. [10] [11]
- GitLab is now removing virtual scrolling. The Rapid Diffs design lists its downsides: no native search, odd scroll-to-element behaviour, high maintenance cost and constant bugs. [12]
- Rapid Diffs: the server always renders the diff HTML. The first batch is inline in the page, the rest arrives as streamed HTML. Each file is a Web Component. `content-visibility: auto` with a server-provided row count reserves space for off-screen files. One delegated click listener and one shared `IntersectionObserver` for the whole page. Components mount on first interaction. The file tree, discussions and menus load after the critical path. [13]
- Their budgets: time to first diff under 1.5 s for 1,000 changed lines or fewer and under 2 s above that, at most 300 KiB of compressed JS and 150 KiB of other assets. [12]
- Highlighting is cached in Redis (`Gitlab::Diff::HighlightCache`, 7-day TTL, refreshed on push). The cache held about 5 GB on GitLab.com, so it has a real cost. [14] [15]
- Limits: files collapse after 100 files, 5,000 lines or 500 KB rendered. Hard stops at 1,000 files, 50,000 lines or 5 MB. Generated files (`gitlab-generated`) are collapsed and kept out of the budget. [14] [16]

**Gerrit** (from the PolyGerrit source, not a write-up)

- Syntax highlighting runs in a Web Worker (`gr-syntax-layer-worker.ts`). [17]
- Diffs default to a few lines of context. A whole-file render above a line threshold is refused with a "Render anyway (may be slow)" choice. [18]
- Token highlighting re-renders only the roughly 100 visible lines. [19]
- From memory, not sourced in this pass: the change screen lists files collapsed and fetches each diff on expand.

**VS Code and Monaco**

- Only the viewport lines exist in the DOM (a few dozen elements for any file size). [20]
- Diff computation runs in a worker with a 5,000 ms timeout and a 50 MB file cap. On timeout the editor shows a coarse result and offers to remove the limit. [21]
- The multi-file diff editor virtualises whole per-file editors and pools at most five warm instances, so a 300-file review never creates 300 editors. [22]

**Pierre Diffs (`CodeView`)**

- Renders plain text first, then highlights with Shiki in a worker pool, with an LRU cache of highlighted output. Their Linux-kernel-sized diff went from 2.4 GB to 1.15 GB of memory after tuning. They note that shipping highlighted output for very large files across the worker boundary can itself dominate the main thread. [23]

**Graphite, Phabricator, Reviewable, Sourcegraph**

- No primary engineering source found in this pass. One secondary claim: Graphite loaded a large PR about 10x faster than GitHub while showing the whole diff. [9] Treat as anecdote.

### Techniques, with evidence and how Branchroom stands

| Technique | Evidence | Branchroom today |
| --- | --- | --- |
| Compress HTML | Measured here: 11.59 MB to 0.58 MB with gzip level 6 | Off. Static assets are gzipped, HTML is not |
| Server-side diff pagination with a small first batch | GitHub: 400 lines or 20 KB first, p99.9 down 3.5 s [3]. GitLab: batches growing to 30 files [10] | 100 files or 1,000 lines per file, then manual "Show more" |
| Automatic progressive loading | GitHub [1] [3], GitLab [13] | Manual click per 100 files |
| Streaming HTML | GitLab Rapid Diffs [13] | Already streams (first paint lands about 0.2 s after first byte) but the first byte is blocked by highlighting |
| `content-visibility: auto` with `contain-intrinsic-size` | web.dev: 232 ms to 30 ms rendering [24]. Independent tests: 3,000 cards first frame 144 ms to 38 ms, and a wrong or missing size estimate makes things worse [25]. GitLab uses a server-provided row count [13] | Not used anywhere in `web_src/css`. Trial below: 10x less style and layout |
| Collapse generated and lock files, and do not send them | GitHub `linguist-generated`, GitLab `gitlab-generated` [16] | Folded with CSS only. The full body is still highlighted, rendered and sent |
| Cache highlighted output | GitLab Redis cache [14] [15]. Pierre LRU [23]. GitHub Copilot app keeps recent diffs [4] | None. Every request re-highlights |
| Highlight off the critical path | Gerrit worker [17], Pierre worker pool [23], GitHub Copilot app [4] | Server-side, synchronous, before first byte, capped at 2 s |
| Fewer nodes and handlers per line | GitHub v2: 74% fewer components, INP 450 to 100 ms [1] | About 16 elements per row, plus a button with inline SVG per line when signed in |
| Event delegation, lazy init | GitHub single handler [1]. GitLab one listener and one observer [13] | Mostly delegated already. Per-file tippy menus are created eagerly |
| Row virtualisation | GitHub: 10x heap and DOM cut, but breaks find-in-page [1] [2]. GitLab abandoned it [12] | None. Not recommended yet, see plan |
| Avoid forced layout | GitHub replaced `:has()` selectors and layout-driven drag handling [1]. GitHub Copilot app batches reads [4] | Not a measured problem. Layout cost here is from sheer size |

## Part 2: Branchroom baseline

### Method and caveats

- Playwright 1.64.1 from the worktree, headless Chromium, 1440 by 900, cold cache, one browser at a time, two runs per page. Long tasks, paint and navigation timing from `PerformanceObserver`. DOM, layout and style totals from CDP `Performance.getMetrics`.
- "Interactive" is the later of DOMContentLoaded and the end of the last long task. "Relayout" is the median time to force a full-document layout after changing the body width.
- Server times are curl medians of three runs.
- **The host was under heavy load** (load average 10 to 33 during the runs, 12 cores, swap full) and the server runs on the same machine as the browser. Absolute numbers are pessimistic and noisy. Where two runs differed a lot both are shown. Ratios and sizes are reliable.
- All pages were fetched anonymously in unified view. Signed-in pages are heavier: each commentable line gains a button with an inline SVG (`templates/repo/diff/section_unified.tmpl:35-39`), each file gains a viewed checkbox, and viewed files are sent folded.
- Only `semyon/oghma` was used. `semyon/swim` returns 404 to anonymous requests. 548 PRs.
- The API's `changed_files` is not a guide to the web view for merged PRs. PR 187 reports 1,810 files and 436,000 lines through the API but the files page shows 49 files. PR 286 reports 268 and shows 3. The largest diffs in the web view are PR 129 and 132 (375 and 381 files, about 219,000 changed lines) and PR 720 (336 files, 15,152 lines). This mismatch is worth a separate look.
- Script: a local scratch folder that was not kept. Raw results: a local scratch folder that was not kept.

### Baseline table

Times in ms. "Server" is time to first byte, then time to last byte. Bytes are the HTML document, which is the same on the wire because nothing is compressed. Long tasks are count, then total blocking time.

| Page | PR or file | Files | Changed lines | Server first / last byte | HTML bytes | DOM elements | First paint | Interactive | Long tasks | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Files changed | #616 | 10 | 2,506 | 2,710 / 3,170 | 1.22 MB | 24,028 | 2,270 to 2,420 | 4,160 to 4,210 | 6, 260 to 490 | 1,534 rows. Relayout 70. Style and layout 1.3 s |
| Files changed | #534 | 58 | 7,301 | 2,400 / 4,290 | 6.08 MB | 126,784 | 1,890 to 2,600 | 5,350 to 9,210 | 6 to 9, 550 to 1,110 | 7,998 rows. Relayout 140 to 340. Style and layout 2.5 to 5.0 s |
| Files changed | #720, first 100 | 336 | 15,152 | 2,420 / 4,160 | 4.86 MB | 90,615 | 2,400 to 2,700 | 5,110 to 6,090 | 6 to 9, 520 to 700 | 5,515 rows. Tree has 471 items. Relayout 120 to 190 |
| Files changed | #720, all files | 336 | 15,152 | 3 more requests, 3,700 to 8,900 each | 20.3 MB total | 371,598 | n/a | 15 to 20 s of loading after 3 clicks | 5 more, longest 630 | 23,683 rows. Relayout 672. About 900,000 DOM nodes. 4,003 listeners |
| Files changed | #129, first 100 | 375 | 219,378 | 5,260 / 10,740 | 11.59 MB | 247,986 | 3,040 to 3,590 | 12,380 to 14,040 | 13 to 15, 3,900 to 4,750 | 16,129 rows. Longest task 2.2 to 2.9 s. Relayout 420 to 450. Style and layout 7.0 to 8.2 s |
| Conversation | #439 | 15 | 1,546 | 210 / 290 | 362 KB | 4,102 | 360 to 550 | 800 to 1,040 | 1 to 2, 70 to 120 | 17 comments, 15 review comments |
| Conversation | #129 | 375 | 219,378 | 240 / 280 | 253 KB | 2,979 | 370 to 390 | 760 to 1,020 | 1 to 3, 90 to 270 | Size of the diff does not matter here |
| Commits list | `main` | n/a | n/a | 23 / 49 | 209 KB | 1,573 | 150 to 250 | 440 to 670 | 1 to 2, 40 to 80 | 50 per page |
| Commits list | #129 commits | n/a | n/a | 175 / 180 | 39 KB | 325 | 650 to 680 | 920 to 960 | 2, 30 to 100 | |
| File view | `scripts/marker-bench.py` | 58 KB | 1,278 lines | 180 to 340 / 210 to 370 | 577 KB | 16,418 | 270 to 510 | 860 to 1,220 | 3 to 4, 110 to 120 | |
| File view | `package-lock.json` | 600 KB | 16,447 lines | 480 / 900 | 5.49 MB | 131,667 | 700 to 790 | 6,470 to 7,030 | 7, 770 to 820 | Relayout 310 to 330. Scroll: 26 to 28 of 299 frames over 33 ms |
| File view | `public/pdf.worker.js` | 2.2 MB | 65,084 lines | 45 / 12,640 | 18.43 MB | 391,243 | 240 | 21,080 | 15, 1,450 | Relayout 1,326. Scroll: median frame 50 ms, 130 of 299 frames over 50 ms, 176 long tasks. Too big to highlight, so this is plain text |

Memory: the JS heap stays small everywhere (3 to 15 MB) because almost nothing is held in script. The cost is in the DOM: 59,000 nodes for the 10-file PR, 592,000 for PR 129's first 100 files, about 900,000 for PR 720 fully loaded, 848,000 for the 2.2 MB file.

Scrolling: programmatic scrolling stays at 16.7 ms per frame on every diff page, including the largest. Scroll jank is only a problem on the big file views. On diff pages the pain is load time and anything that triggers layout (resizing, folding a file, filtering, expanding context, opening a comment form).

### Supporting measurements

**Compression** (same HTML, compressed offline):

| Page | Raw | gzip -6 | zstd -3 |
| --- | --- | --- | --- |
| #616 files | 1.22 MB | 77 KB | 70 KB |
| #173 files (100 of 121) | 8.48 MB | 439 KB | 346 KB |
| #129 files (100 of 375) | 11.59 MB | 579 KB | 408 KB |
| `package-lock.json` view | 5.49 MB | 358 KB | 304 KB |

**Where the server time goes** (black box, so the split is inferred from timings and code, not profiled):

| Request | #616 | #720 | #129 |
| --- | --- | --- | --- |
| Raw `.diff` endpoint, whole PR | 0.02 to 0.04 s, 127 KB | 0.30 s, 1.34 MB | 0.65 s, 8.6 MB |
| Files page, first byte | 1.9 to 2.7 s | 2.2 to 2.4 s | 2.9 to 5.3 s |
| Files page, body streaming after first byte | 0.2 to 0.5 s | 1.0 to 1.7 s | 5.2 to 6.7 s |

- First byte sits at 2.2 to 2.5 s for every PR from 58 files up, whatever its size. That matches the 2 s highlight budget in the code.
- Single-file requests (`?file-only=true&files=...`) on PR 616 took 0.06 to 1.09 s each and 3.6 s in total for ten files. A 36 KB Markdown file with 22 diff rows took 1.06 s. Cost follows file size, not diff size, which is what full-file highlighting of both sides predicts.

**Markup weight** (PR 616, 1,534 rows):

- Visible text in rows: 152 KB. Row HTML: 1.11 MB. That is 7.3 times overhead.
- Median row 629 bytes, 90th percentile 925 bytes. 15.7 elements and about 38 DOM nodes per row.
- `class` attributes are 32% of the page, `rel` attributes 9%, template indentation 9%.
- In PR 129, `package-lock.json` is labelled Generated and folded, and still ships 592 KB and 815 rows.

**`content-visibility` trial.** One rule injected in the browser only, nothing changed on the server: `.diff-file-body { content-visibility: auto; contain-intrinsic-size: auto 600px; }`

| Page | Style plus layout | Relayout | Interactive |
| --- | --- | --- | --- |
| #534 baseline | 2.5 to 5.0 s | 143 to 340 ms | 5.35 to 9.21 s |
| #534 with rule | 0.23 to 0.26 s | 4 to 6 ms | 3.84 to 4.72 s |
| #129 baseline | 7.0 to 8.2 s | 417 to 446 ms | 12.4 to 14.0 s |
| #129 with rule | 0.41 to 0.76 s | 7 to 16 ms | 11.3 to 14.0 s |
| #720 all files baseline | 15.5 s cumulative | 672 ms | n/a |
| #720 all files with rule | 1.06 s cumulative | 9 ms | n/a |

- PR 129's time to interactive barely moves because the server takes 8 to 11 s to send the page. The browser is no longer the limit there.
- Costs seen: with a fixed 600 px placeholder the page height is wrong (66,000 px instead of 566,000 px), and a very fast scroll through the whole page hits frames of 180 to 530 ms as files lay out on arrival. A per-file height estimate from the server fixes the first and softens the second.

## Bottlenecks, with code references

Paths are relative to the worktree root.

1. **Full-file highlighting on every request, before the first byte.** `GetDiffForRender` loops over every file (`services/gitdiff/gitdiff.go:1380`), reads up to 256 KiB of both blobs (`gitdiff.go:515-533`, limit at `gitdiff.go:142`) and runs Chroma over the whole of each side (`gitdiff.go:1418-1425`, `gitdiff.go:1440-1452`). The only guard is a 2 s budget for the whole request (`gitdiff.go:143`, checked at `gitdiff.go:1417`). Files after the budget fall back to line-by-line highlighting during template execution (`gitdiff.go:361-363`). Nothing is cached between requests. The only cache in the package is the per-request inline diff (`gitdiff.go:86`, `gitdiff.go:370-409`).
2. **Folded files are still fully processed and sent.** `ShouldBeHidden` is generated or viewed (`gitdiff.go:597-599`). The template only sets `data-folded` (`templates/repo/diff/box.tmpl:94`, `box.tmpl:170`) and still renders the table (`box.tmpl:192-199`). They are highlighted too, since the loop in item 1 does not check.
3. **No HTML compression.** `ENABLE_GZIP` defaults to off (`modules/setting/server.go:277`, applied at `routers/web/web.go:286`). The live server sends no `Content-Encoding` on pages.
4. **Heavy rows.** `templates/repo/diff/section_unified.tmpl:14-43` emits five cells per line, an empty `<span rel>` in each line number cell (lines 23 and 24), an escape cell that is almost always empty (lines 27 to 29), a marker span (line 30), a `<code>` wrapper (`templates/repo/diff/section_code.tmpl:1`) and, when signed in, a button with an inline SVG per line (lines 35 to 39).
5. **No containment or lazy rendering in the browser.** No `content-visibility` or `contain` anywhere in `web_src/css`. Every file body is laid out at load.
6. **Paging is coarse and manual.** Limits are 100 files, 1,000 lines per file, 5,000 characters per line (`modules/setting/git.go:41-43`, applied at `routers/web/repo/pull.go:796-813`, enforced in `gitdiff.go:746-749` and `gitdiff.go:1028`). The next batch needs a click (`box.tmpl:224-233`). The response to `file-only=true` is the whole page template (`pull.go:987`), which the client parses as a full document to pull out the boxes (`web_src/js/features/repo-diff.ts:157-170`). The same is true for loading one suppressed file (`repo-diff.ts:181-193`). Following an anchor to a file that is not loaded yet fetches batches one after another until it appears (`repo-diff.ts:239-247`).
7. **Three git passes per files page.** The patch (`pull.go:816`), `git diff --shortstat` over the whole PR (`pull.go:839`, `gitdiff.go:1476-1488`) and the diff tree (`pull.go:883`). Cheap here (the conversation page for PR 129 runs the shortstat and answers in 0.2 s) but uncached.
8. **File view renders every line with no chunking.** One table row per line (`templates/repo/view_file.tmpl:118-123`) from `highlight.RenderFullFile` (`routers/web/repo/view_file.go:126`). Highlighting stops at 1 MiB (`modules/highlight/highlight.go:26`) but display continues to 8 MiB (`modules/setting/ui.go:98`). An 8 MiB text file would be roughly 65 MB of HTML.
9. **File tree is not virtualised.** Every entry is a Vue component (`web_src/js/components/DiffFileTree.vue:107`, `DiffFileTreeItem.vue:38`) and the whole tree is inlined as page data (`pull.go:894`). At 471 to 484 entries this cost under 0.4 s of script, so it is not a problem yet.
10. **Per-file widgets are created eagerly.** A tippy menu per file header at init (`repo-diff.ts:140-151`). 1,145 to 1,426 listeners at load and 4,003 with all files of PR 720 loaded.

Upstream context that affects merge risk:

- `services/gitdiff/gitdiff.go` is actively optimised upstream. PR 38706 (July 2026) added the inline diff cache and reports about 466 ms of CPU per 1,000 modified lines after the change. PR 32393 kills the git process at the file limit and skips stats for `file-only`. [26] [27]
- The oldest upstream issue on this (14734, 2021) already names Chroma and the size of the generated HTML as the causes, and the upstream answer was the settings `DISABLE_DIFF_HIGHLIGHT`, `MAX_GIT_DIFF_FILES` and "Load more". [28]

## Proposed budgets

Measured anonymous, cold browser cache, desktop Chromium, idle host, unified view.

| Page | Budget |
| --- | --- |
| Files changed, 300 files and 15,000 changed lines | First byte under 300 ms. First diff visible under 800 ms. Interactive under 1 s with warm highlight cache, under 2 s cold. Every file reachable with no clicks. |
| Files changed, any size | First response under 300 KB compressed and under 30,000 elements. No long task over 200 ms. Forced relayout under 20 ms. Total blocking time under 200 ms. |
| Files changed, typical PR (10 files) | Interactive under 500 ms. |
| Conversation | Interactive under 800 ms regardless of diff size. |
| File view up to 1 MB | Interactive under 1 s. Scroll frames under 33 ms at the 95th percentile. |
| File view over 1 MB | First screen under 1 s, rest on demand. |
| Commits lists | Interactive under 500 ms (already met on an idle host by the look of it). |

For comparison, GitLab's own target is first diff under 1.5 s up to 1,000 changed lines and under 2 s above that. [12]

## Ranked plan

Effort S is a day or less, M is a few days, L is a week or more. Merge risk is the chance of conflicts on future upstream syncs.

| # | Change | Effort | Expected gain | Merge risk |
| --- | --- | --- | --- | --- |
| 1 | Turn on HTML compression: `[server] ENABLE_GZIP = true`, or compress at the reverse proxy | S, config only | Page transfer down about 20x (11.59 MB to 0.58 MB). Matters most over the tunnel and on mobile. Little change on the LAN | None |
| 2 | Add a `Server-Timing` header for the diff phases (git diff, blob reads, highlight, shortstat, tree, template) and take one CPU profile of PR 129 | S | No direct gain. Confirms the split inferred above before the larger items are built | Low |
| 3 | `content-visibility: auto` on `.diff-file-body` with a per-file `contain-intrinsic-size` computed by the server from the row count | S | Style and layout down about 10x (7 to 8 s to under 1 s on PR 129). Relayout 420 ms to about 10 ms. Interactive on PR 534 down about 1.5 to 4 s | Low. One CSS rule and one attribute in `box.tmpl` |
| 4 | Do not highlight, render or send bodies of folded files (generated, vendored, viewed). Show the header and reuse the existing "load diff" path | S | Removes lock files and vendored code from every page. 0.6 MB and 815 rows on PR 129 for one lock file, plus its highlight time | Low to medium. Small edits in `box.tmpl` and the loop in `gitdiff.go` |
| 5 | Make highlighting lazy and cached. Highlight a file when the template reaches it rather than in the loop before the first byte, and cache highlighted lines by blob SHA and lexer in the existing cache | M | First byte from 2 to 5 s down to roughly `git diff` time (0.05 to 0.7 s). Repeat views and "load more" stop paying for Chroma | Medium. `gitdiff.go` is busy upstream. Keep it as a small accessor change and consider offering it upstream |
| 6 | Smaller first batch with automatic follow-up. Send about 20 files or 2,000 rows first, then fetch the rest in the background as the reader approaches the end. Return a fragment for `file-only=true` instead of the full page | M | First response for PR 129 from 11.6 MB over 8 to 10 s to about 1 to 2 MB in under 1 s. No clicks. Removes the full-document parse on each batch | Medium. `pull.go`, `box.tmpl`, `repo-diff.ts` |
| 7 | Slim the row markup. Drop the empty `rel` spans, the empty escape cell, the marker span and the template whitespace. Replace the per-line comment button with one shared button placed by a delegated handler | M | About 40 to 50% fewer bytes and elements per row on every diff. Bigger effect for signed-in users | High. `section_unified.tmpl` and `section_split.tmpl` change upstream often and Branchroom's suggested-changes work touches the same rows. JS selectors depend on the structure |
| 8 | File view: render long files in fixed-size chunks (for example 200 lines each) with `content-visibility` and an exact height, and lower `MAX_DISPLAY_FILE_SIZE` from 8 MiB as a stopgap | M (S for the setting) | The 2.2 MB file from 21 s to an estimated 2 s, scroll back to 16 ms frames. Caps the worst case | Medium. `view_file.tmpl` and the line selection script |
| 9 | Create per-file menus on first click. Virtualise the file tree only when a PR passes about 2,000 files | S | Small. A few hundred listeners and some script time | Low |
| 10 | Row virtualisation | L | Would cap the DOM at any size | High, and not recommended now |

Notes on the choices:

- **Why not virtualise.** GitLab built it and is removing it because of bugs and broken native search. GitHub keeps it only for PRs over about 10,000 lines and lists find-in-page, select-all and print as casualties. Items 3 and 6 give most of the benefit while keeping the page a normal document. Revisit only if PRs over about 50,000 rendered rows become common.
- **Item 3 needs the size estimate.** Independent tests show `content-visibility` without a good `contain-intrinsic-size` can render far more than needed and gets the scrollbar wrong. Diff rows have a fixed height, so `rows * line height + header` is close. Files with inline conversations or wrapped lines will be off and correct themselves once seen (`auto` keeps the last real size). Check that anchor jumps to `#diff-...` and `#issuecomment-...` still land correctly, and that the sticky file header behaves.
- **Item 5 before item 6.** Lazy highlighting makes every batch cheap, which is what makes automatic loading safe to turn on.
- **Item 7 last among the M items.** It is the biggest steady-state win per row but the most likely to hurt on every upstream merge. Do the pieces that are pure deletions first (whitespace, empty spans).
- **Signed-in measurement is missing.** Everything here is anonymous. Before starting item 7, repeat the files-changed runs with a logged-in reviewer to size the per-line button cost.

Expected result for a PR like 720 (336 files, 15,000 lines) after items 1 and 3 to 6, as an estimate rather than a measurement: first response of about 20 files at roughly 1 MB raw and 60 KB compressed, first byte around 0.3 s, interactive around 1 s, remaining files arriving in the background with no clicks, and relayout around 10 ms even with everything loaded.

## Sources

1. GitHub Blog, "The uphill climb of making diff lines performant", 2026-04-03. https://github.blog/engineering/architecture-optimization/the-uphill-climb-of-making-diff-lines-performant/
2. GitHub Changelog, "Improved pull request Files changed page on by default", 2026-01-22. https://github.blog/changelog/2026-01-22-improved-pull-request-files-changed-page-on-by-default/ and the 2026-02-05 update https://github.blog/changelog/2026-02-05-improved-pull-request-files-changed-february-5-updates/
3. GitHub Blog, "How we made diff pages three times faster", 2016-12-06. https://github.blog/engineering/architecture-optimization/how-we-made-diff-pages-3x-faster/
4. GitHub Blog, "Rendering huge pull requests in the GitHub Copilot app", 2026-09-23. https://github.blog/engineering/user-experience/rendering-huge-pull-requests-in-the-github-copilot-app/
5. GitHub Blog, "Crafting a better, faster code view", 2023-06-21. https://github.blog/engineering/architecture-optimization/crafting-a-better-faster-code-view/
6. "Why is GitHub UI getting so much slower?" https://yoyo-code.com/why-is-github-ui-getting-so-much-slower/
7. Hacker News, "Did GitHub UI become unbearably slow?", 2025-05. https://news.ycombinator.com/item?id=43961329
8. Reddit r/github, "Only showing the first 1000 files", 2025-10. https://www.reddit.com/r/github/comments/1nyrip2/only_showing_the_first_1000_files_oh_cool_guess_i/
9. David Gomes, "Looking Back On Phabricator For Code Review", 2026-02. https://davidgomes.com/phabricator-code-review/
10. GitLab Docs, "Merge request diffs frontend overview". https://docs.gitlab.com/development/merge_request_concepts/diffs/frontend/
11. GitLab MR 60312, "Added virtual scrolling to diffs". https://gitlab.com/gitlab-org/gitlab/-/merge_requests/60312
12. GitLab Handbook, "Reusable Rapid Diffs" design document. https://handbook.gitlab.com/handbook/engineering/architecture/design-documents/rapid_diffs/ and epic 18457 https://gitlab.com/groups/gitlab-org/-/work_items/18457
13. GitLab Docs, "Rapid Diffs". https://docs.gitlab.com/development/fe_guide/rapid_diffs/
14. GitLab Docs, "Working with diffs" (limits and caches). https://docs.gitlab.com/development/merge_request_concepts/diffs/ and "Diff limits administration" https://docs.gitlab.com/administration/diff_limits/
15. GitLab issue 367160, highlight cache memory on Redis. https://gitlab.com/gitlab-org/gitlab/-/work_items/367160
16. GitLab MR 267276, generated files and safe limits. https://gitlab.com/gitlab-org/gitlab/-/merge_requests/267276
17. Gerrit source, `gr-syntax-layer-worker.ts`. https://gerrit.googlesource.com/gerrit/+/47185f76b5334ceec87034d9b3df30888598aa92/polygerrit-ui/app/embed/diff/gr-syntax-layer/gr-syntax-layer-worker.ts
18. Gerrit source, `gr-diff-element.ts` (large diff warning). https://gerrit.googlesource.com/gerrit/+/4a953cccb3b6c83690a0608a835345c4b239f950/polygerrit-ui/app/embed/diff/gr-diff/gr-diff-element.ts
19. Gerrit source, `token-highlight-layer.ts`. https://gerrit.googlesource.com/gerrit/+/47185f76b5334ceec87034d9b3df30888598aa92/polygerrit-ui/app/embed/diff/gr-diff-builder/token-highlight-layer.ts
20. "The Monaco Editor: The Engine Behind Every Keystroke in VS Code" (secondary). https://roopik.com/blog/vscode-internals-monaco-editor
21. VS Code source, `editorOptions.ts` (`maxComputationTime`, `maxFileSize`). https://github.com/Microsoft/vscode/blob/main/src/vs/editor/common/config/editorOptions.ts
22. Survey of the VS Code diff editor internals (secondary, cites source paths). https://sparkles-docs.pages.dev/research/diff-review/vscode
23. Pierre Computer Company, "On Rendering Diffs", 2026-05-29. https://pierre.computer/writing/on-rendering-diffs
24. web.dev, "content-visibility: the new CSS property that boosts your rendering performance". https://web.dev/articles/content-visibility
25. "I Measured content-visibility: auto on 3,000 Cards", 2026-10. https://raxxo.shop/blogs/lab/i-measured-content-visibility-auto-on-3-000-cards and Nolan Lawson, 2024-09 https://nolanlawson.com/2024/09/18/improving-rendering-performance-with-css-content-visibility/
26. Gitea PR 38706, "perf(gitdiff): optimize inline diff highlighting using cache". https://github.com/go-gitea/gitea/pull/38706
27. Gitea PR 32393, "improve performance of diffs". https://github.com/go-gitea/gitea/pull/32393
28. Gitea issue 14734, "It takes a long time to open a big pull request page". https://github.com/go-gitea/gitea/issues/14734
