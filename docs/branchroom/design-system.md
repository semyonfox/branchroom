# Branchroom design system

How Branchroom should look, read and behave. Read this before changing a template, a stylesheet or any text a person sees.

It also covers upstream Gitea. When a merge from upstream brings in new or changed UI, port it to these rules in the same change.

## What Branchroom is for

Branchroom is a forge for people at every skill level and for the agents that work for them. It should feel like a place you come back to: your work first, then what changed, then things to find.

It is not an AI product. People bring their own agents, and the forge gives those agents good information.

## Product rules

1. **No built-in AI.** Nothing generates, reviews or merges on the forge's behalf. Agents work through the API and MCP.
2. **Every agent has a responsible person.** Agent work always names the person who answers for it.
3. **Credit the model and the harness.** Agent work shows as `name · agent · model in harness · responsible: person`, because models differ in ability and trust.
4. **The forge informs agents. It does not manage them.** No dispatching work, no agent dashboard, no per-model rules. Agents read the API, MCP, the inbox and the repository's `AGENTS.md`.
5. **An agent never stands in for its person.** A review made with a person's token is shown as the agent's. It does not clear that person's review request or replace their decision.
6. **Each feature lives in one place.** If two pages or two controls do the same job, remove one and link to the other.
7. **The page tells the truth.** See [Truth rules](#truth-rules).

## Page map

What leads on each page. If a change moves something out of its lead position, it needs a reason.

| Page | Leads with | Then |
| --- | --- | --- |
| Home, signed in | Your work: each pull request and issue with the state it is in | Updates grouped by kind, your repositories on the left, a small Discover column on the right |
| Inbox | Needs you | Triage, Your work, Agents, Mentions, Snoozed and Done as views. The link to Notifications, with its unread count, lives here |
| Repository home | The README | The file list below it. The sidebar holds About, the latest release with quick downloads, maintainers, contributors and languages |
| Code | The file or folder you opened | One Code tab. There is no separate Overview tab, and files on the repository home open here |
| Issue or pull request | Title, state and the next step | The conversation, with reviewers, labels and the rest in the side lists |
| Pull request list | One row per pull request | A size chip (XS to XL) and the author's trust level on each row |
| Command palette | Whatever you type | Open with Ctrl+K or Cmd+K on every page. It finds repositories, files, issues, pull requests and settings, and offers commands for the current page |

Other fixed points:

- The header badge counts what needs you. It never counts unread notifications.
- One **Code** button holds clone, ZIP and open-in. Do not add a second download or clone control.
- The main button on a pull request is the next step. Merge is the main button only when the pull request can merge now.
- Settings and admin pages keep the upstream layout inside our header and footer.

## Truth rules

These came from logic passes on a seeded forge. Each one was a real bug.

- **A count matches what the click shows.** A badge that says 9 opens a list of 9.
- **A state never contradicts the page.** Don't say "Ready to merge" while changes are requested. Don't say "No reviews yet" above a review.
- **Say "required" only when something is enforced.** "Some required checks failed" is wrong when no checks are required.
- **No control without a target.** Hide bulk actions until something is selected. Hide "Show full README" when the README already fits.
- **No risky default.** A destructive or final action never has a preselected reason or option.
- **Name the subject.** "Last push to `fix/tides`", not "Last push" beside the main branch's status.
- **Credit the author, describe the event.** A row shows who made the item. What an agent did later is an event such as "commented" or "reviewed".
- **An empty state says what is true.** "Nothing needs you" is better than a blank list under a busy toolbar.
- **Two routes to one thing must agree.** If the same fact appears on two pages, both read it from the same place.

## Visual rules

### Colour

- Keep the Branchroom themes as they are. Do not add a palette or new colour tokens for a feature.
- The accent (`--color-primary`) is for things that carry meaning: the active tab, counts, states, focus and the one primary action on a page.
- State colours are the theme's own: green for passing or open, red for failing or changes requested, yellow for waiting or new, purple for merged.
- The dark theme uses neutral near-black surfaces. The light theme uses warm off-white. Both must work for every change.
- Use theme variables. Never write a raw colour in a feature stylesheet.

### Surfaces and shape

- The page is the body colour. Content sits on cards with a one pixel border in `--color-secondary` and the box body colour.
- Card corners are 8px. Buttons, inputs and chips use `--border-radius` (4px).
- No drop shadows on page content. Shadows are for things that float: menus, dialogs and the palette.
- One level of card. Don't put a bordered card inside another bordered card.

### Type

- System fonts only, as set in `--fonts-regular` and `--fonts-monospace`. No web fonts.
- Body text is 14px. Secondary text uses `--color-text-light`, not a smaller size, unless it is a chip or a count.
- Chips are 11px on an 18px line, with no wrapping.
- Sentence case everywhere. No capitals for emphasis.

### Density and layout

- Rows are compact: one line where the content allows, with the title first and facts after it.
- A list row has at most two chips before it becomes noise. Put the rest on the item's own page.
- Sidebars hold facts and links. They never hold the page's main action.
- Phones: every page works at 390px wide with no sideways page scroll. Tab strips scroll inside themselves. Tap targets are at least 44px high.

### Icons

- Octicons through the `svg` helper, at 16px unless the design needs otherwise.
- An icon that carries meaning has a text label or a tooltip. An icon alone is fine only for a familiar action such as copy.

## Words

- Say what the thing is and what happens next: "Waiting on niamh: changes requested".
- Plain words over forge jargon. A new user should understand a button without knowing Git.
- Don't repeat a heading in the text under it.
- Only edit `options/locale/locale_en-US.json`. Remove a string when the last template using it goes.

## How to build it

- Styles for the redesign live in `web_src/css/forge/`, one file per area: `chrome.css`, `home.css`, `repo-home.css`, `issue.css`, `inbox.css` and `palette.css`. Add to the file for the area you are changing.
- Prefer `tw-*` utilities and `flex-*` helpers for one-off spacing. Write a class when the same shape appears twice.
- Don't edit `web_src/css/themes/` as part of a feature. A theme change is its own decision.
- Follow `docs/guidelines-frontend.md` for everything else.

## Before you open a pull request

1. Render the changed pages in the light theme, the dark theme and at 390px wide. Look at them.
2. Check each changed page against the [Truth rules](#truth-rules): counts, states, empty states and controls without a target.
3. Check the page still leads with what the [Page map](#page-map) says.
4. Run `make lint-templates`, `make lint-css` and `make lint-js` for what you touched.
5. Put before and after screenshots in the pull request, as `AGENTS.md` describes.
