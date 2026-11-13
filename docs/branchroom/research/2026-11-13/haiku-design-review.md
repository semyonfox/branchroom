# Branchroom style D: design review

Scope: read-only review of the four D boards (D-RepoHomeLight, D-PullLight, D-AuditLight, D-RepoHomeDark), compared with A, B and the rejected C. Renders are in a local scratch folder that was not kept. Token values were read from the sources, and contrast ratios were calculated from the hex pairs (WCAG 2.x).

## Verdict

D is clean and credible, and it reads as a real product rather than a generated mockup, but it has drifted back toward neutral GitHub grey: it has lost A's serif headings and warm paper, and the four boards disagree on header state, type scale and control heights. Fix those before any other polish.

## Does it look official? (AI tells and weak spots)

No em dashes, semicolons or emoji in the visible copy, and no gradients or shadows apart from the ochre hatch. That is good. The tells that remain:

- **Every board, weight:** 600 is used 17 times on D-RepoHomeLight, 14 times on D-PullLight, 16 times on D-AuditLight. Almost everything is semibold, so hierarchy comes from size and colour alone. That flat, template-like weight is a common generated-UI tell.
- **Type scale:** the repo board alone uses 11, 12, 13, 14, 15, 18, 19 and 26px. Across the set the page titles are 26px (README h1), 24px (PR title), 20px (audit banner title) and 18px (repo name).
- **README copy, D-RepoHomeLight:** "It separates request-serving work from long-running ingestion and retrieval workloads so imported content can become searchable without blocking the main application." That sentence has the AI cadence (abstract noun stacks, "so ... without ..."). Plainer: "Canvas imports and file indexing run in background workers, so the web app stays responsive."
- **Header on the same product:** D-RepoHomeLight shows "Sign in" with a search box. D-PullLight and D-AuditLight show "semyon" with no search, no "+", no notifications, no avatar. The chrome changes between pages, which looks assembled.
- **Fifth hue on D-PullLight:** the merged banner adds purple tint (`#f6f2fc` background, `#3f2a6e` text) on top of the purple pill. The pill can stay, since GitHub uses purple for merged. The banner should be neutral.
- **Strikethrough branch name, D-PullLight:** `docs/roadmap-audio-ingestion` is struck through in the PR meta line. It reads as a removed or erroneous item. The merge box already says "The head branch was deleted", so the name should be a plain mono chip.
- **Dark board is a different project:** D-RepoHomeDark shows Murmur as a public repo, so the restricted treatment is never shown in dark mode. The owner needs restricted work to work in both themes.
- **Audit composition:** D-AuditLight has five rows and then about 280px of empty board below the card. A and B both carry the explanatory line about the hash chain and a row count ("Showing 7 of 148 events"). Without that line the audit log reads as a sample, which undercuts the trust the page exists to build.

## Does it keep what the owner liked?

- **A's serif README headings: lost.** Source Serif 4 appears in A's repo home and audit board (4 and 3 references) and in none of D's files. The owner named this specifically.
- **A's warm paper: mostly lost.** A used `#f6f3ee`, `#efeae2` and `#fffdfa` surfaces with slate ink `#1c2230`. D uses `#f7f7f5` for the page, `#ffffff` for cards and `#16181d` for ink. Those are neutral, closer to GitHub than to A. Only the ochre restricted surfaces still feel warm.
- **Slate ink:** D's `#4b5059` secondary text and `#16181d` primary text are a reasonable slate. Keep those.
- **Harbour blue:** `#1f58a8` for buttons and links is kept. Good.
- **Ochre vault:** `#fbf6ea` background, `#6e4a0b` text and the `#c9a35a` stripe carry over from A. This is the strongest thing D inherits from A.
- **B's security-tape stripe:** kept, as a 6px 135° hatch on the restricted card and the audit banner. Clear.
- **Pleasantness:** D is calm and readable. It is less cold than B, but less distinctive than A.

## Consistency across the four boards

- **Neutral borders: five near-identical greys.** `#d6d6d1` (control borders), `#e2e2de` (card borders), `#ecece8` (row dividers), `#ececE8` (h1 underline, which is mixed case and so a sign of hand-typed values), `#e6e7ea` (dark header). Three tokens would do.
- **Dark surfaces: eight near-identical greys.** `#0a0b0e`, `#0f1115`, `#13151a`, `#16191f`, `#1a1d23`, `#22252c`, `#23262d`, `#262a32`. Layers blur together. Four tokens would give clean stacking.
- **Control heights: 28, 30, 32, 34, 38, 44px.** Repo Watch, Star, Fork and Code are 30px. Rotate URL is 28px. Export evidence is 32px. Table rows are 44px. Use 32px for default buttons, 28px for compact and filter controls, and one row height.
- **Radii:** 4, 6 and 8px. Controls are 6px and cards are 8px, which is consistent. The 4px is used for the Public/Private pills, which is fine if it is stated as the badge radius.
- **Tab patterns:** underline tabs on the repo and PR boards, segmented chips on the audit board. Both are defensible, but name them as two patterns.

## Readability and contrast (measured)

All body and UI text passes WCAG AA. The lowest body values are:

- `#626873` meta text on white: 5.60:1 (5.22:1 on `#f7f7f5`).
- `#1a7f37` green on white: 5.08:1.
- `#b42318` red on `#fdecea`: 5.75:1.
- `#6e4a0b` ochre on `#fbf6ea`: 7.36:1.
- Dark muted `#8b919c` on `#22252c`: 4.84:1, which is just a pass.

Failing or weak non-text colours:

- The `/` separator in the repo breadcrumb, `#9aa0a9` on white: **2.63:1**. On `#f7f7f5` it is 2.45:1.
- The `|` separators in "2,168 commits | 10 branches | 7 tags", `#c4c4be` on white: about 1.6:1. These carry meaning in the branch bar, so they are not just decoration.

Fix: use `#767c87` or darker for the slash and drop the pipes for middots or spacing.

No focus states are shown on any board. Keyboard focus is not visible in these mockups, so it cannot be judged.

## Comparison with GitHub and established systems

These are from memory of the public defaults, not measured live today.

- **GitHub / Primer:** a logged-in header with search, "+", notifications and avatar on every page; 32px default buttons; 600 for headings only; 400 for body; one restrained neutral border. D is weaker on the header, the button scale and the weight discipline.
- **GOV.UK:** plain, almost no radius, very clear focus treatment, and status by text, not colour alone. D is more decorated than GOV.UK, and it has no focus treatment, which GOV.UK would never omit.
- **Atlassian:** 8px spacing grid and tight, consistent control scale. D has a loose spacing scale (gutters of 12, 14, 16, 18, 20, 28, 32px across boards).
- **Stripe dashboard:** very few badges, one accent, and tabular numbers in tables. D is close here. Its audit table is the best part of the set, and it could go further by using the tabular numerals and a quieter outcome column.

Where D is weaker: header completeness, the single type and weight scale, dark mode coverage, focus and state design, and the lost warmth from A.

## Ranked changes (12)

1. **D-RepoHomeLight, README h1 and h2 (and dark board if reused).** Current: Inter 600 at 26px and 19px. Proposed: Source Serif 4 at 26px, 600, for README h1 and h2 only; keep Inter for UI. Why: the owner named serif headings as a keeper, and it is the most distinctive thing A had.
2. **All light boards, page and card surfaces.** Current: page `#f7f7f5`, cards `#ffffff`. Proposed: page `#f6f3ee`, cards `#fffdfa` (both from A). Why: restores the warm paper without touching contrast, since `#16181d` on `#f6f3ee` stays well above 4.5:1.
3. **D-RepoHomeLight, D-PullLight, D-AuditLight, top header.** Current: repo board shows "Sign in" plus search; PR and audit show "semyon" with nothing else. Proposed: one logged-in header on all three, with search, "+", notification bell and avatar, matching Gitea's real header. Why: chrome must not change between pages of one product.
4. **D-PullLight, PR meta line, head branch.** Current: `docs/roadmap-audio-ingestion` in a grey chip with line-through. Proposed: plain mono chip, no strikethrough. The merge box already says the branch was deleted. Why: strikethrough reads as an error.
5. **All boards, type scale.** Current: 11, 12, 13, 14, 15, 18, 19, 20, 24, 26px. Proposed: 12 (meta), 13 (UI), 14 (body), 16 (section), 20 (repo and page title), 24 (PR and README titles). Why: four different page-title sizes across three light boards.
6. **All boards, font weight.** Current: 600 on about 17, 14 and 16 elements per board. Proposed: 600 only for page and section titles and the active nav item; 500 for labels, buttons and table headers; 400 for body. Why: flat semibold is the strongest generated-UI tell in the set.
7. **All boards, control heights.** Current: 28, 30, 32, 34px buttons and 44px table rows. Proposed: 32px default buttons, 28px compact controls, one row height of 40px. Why: GitHub and Atlassian both use a small, fixed control scale.
8. **All boards, neutral borders.** Current: `#d6d6d1`, `#e2e2de`, `#ecece8`, `#ececE8`, `#e6e7ea`. Proposed: three tokens: `#d6d6d1` (control borders), `#e2e2de` (card borders), `#ecece8` (row dividers); fix the mixed-case hex. Why: five near-identical greys are hard to maintain and show as drift.
9. **D-RepoHomeDark, dark surfaces.** Current: eight greys between `#0a0b0e` and `#262a32`. Proposed: four tokens, canvas `#0f1115`, surface `#161920`, raised `#1c1f27`, border `#2a2e37`. Why: the layers currently blur, which makes the dark board look muddy rather than deliberate.
10. **D-RepoHomeDark, restricted state.** Current: the dark board shows only a public repo (Murmur). Proposed: add a dark restricted callout with the ochre stripe, for example background `#2a2312`, text `#e8c77a`, stripe `#c9a35a`, and check its contrast. Why: restricted work must work in both themes, and this is unproven.
11. **D-RepoHomeLight, separators in breadcrumb and branch bar.** Current: `/` at `#9aa0a9` (2.63:1 on white) and `|` at `#c4c4be` (about 1.6:1). Proposed: `#767c87` for the slash, and middots or spacing in place of the pipes. Why: these carry meaning and fail contrast.
12. **D-AuditLight, below the event table.** Current: five rows, no count, no explanation, then about 280px of empty board. Proposed: a row count ("Showing 5 of 214 events, newest first"), one line on the hash chain ("Each event is hash-chained to the one before. The export is JSON Lines with usernames, refs, commits and a verification summary"), taken from A. Why: the audit log is only credible if it explains how it is verified.

## Keep these

- The Gitea teacup logo and green, in the header of every board.
- Ochre restricted vocabulary: `#fbf6ea` background, `#6e4a0b` text (7.36:1), `#c9a35a` stripe, and the striped top edge on the restricted card and audit banner.
- The audit banner: "Restricted workspace", "embargo-fix", the green "Audit chain intact, 214 events" check, and the primary "Export evidence" button.
- The audit table's columns (Time UTC, Person, Event, Outcome) and its outcome words in green and red, which carry meaning in text as well as colour.
- The diff block in D-PullLight: red and green tints with mono text.
- Tabular numerals and JetBrains Mono for SHAs and branch refs.
- Body and UI text contrast, which passes AA throughout.
- Blue `#1f58a8` for primary buttons and links, which is harbour blue and holds at 6.95:1 with white text.
