# Anonymous repository comparison

Started Friday 13 November 2026 at 12:12:36 GMT. Nine-minute exploration cutoff was 12:21:36 GMT. Finished exploration before the cutoff. One anonymous browser tab, closed afterwards. No login, form submission or download. I did not read other reports.

The session's browser rules required the shared preview browser instead of the requested standalone Node Playwright runner. CSS viewports were 1440×900 and 390×844. Saved PNGs were normalised to those dimensions because the browser returned scaled desktop captures and double-density mobile captures. Phone observations concern layout at that width, not touch or a real phone. Screenshots are in `sol-ta/` beside this report. This is a visual comparison, not a loading-speed benchmark.

## Murmur home

GitHub, https://github.com/semyonfox/murmur: My eye goes to the familiar green Code button, then the latest commit. I know where I am without thinking. The file list dominates and the README only begins near the bottom of the desktop view. On the phone, About and the social controls consume a lot of room too. GitHub is not a good model for getting straight to project documentation. The mobile file rows sensibly omit commit messages, so filenames remain readable. Screenshots: `github-murmur-home-desktop.png`, `github-murmur-home-mobile.png`.

Branchroom, http://10.0.0.5:3080/semyon/murmur: This feels recognisable immediately, although the green cup and generic Explore/Help header make it look like stock Gitea rather than a distinct Branchroom product. Desktop is usefully compact. I can already see the README heading and the latest release in the right sidebar. The full-width Commits/Branches/Tags strip draws more attention than those numbers deserve. On the phone, Search code, Description, size and three separate social-stat lines come before the files. The README container starts roughly 1,060 CSS pixels down. Keeping commit messages in narrow mobile rows produces fragments such as "initial murm..." without helping me understand the change. Screenshots: `branchroom-murmur-home-desktop.png`, `branchroom-murmur-home-mobile.png`.

## Murmur releases

GitHub, https://github.com/semyonfox/murmur/releases: "Latest" answers my first question. The release index on the left helps me orientate. The newest release's assets are expanded and the APK is visible on desktop. Checksums make the rows busier, but the filenames are still clear. On the phone, the title, Compare control and metadata push the assets towards the bottom of the first screen. I still need to distinguish an updater JSON from an actual installer. Screenshots: `github-murmur-releases-desktop.png`, `github-murmur-releases-mobile.png`.

Branchroom, http://10.0.0.5:3080/semyon/murmur/releases: This is pleasantly direct on desktop. Downloads are already expanded and both AppImage and APK are in view. From the home page, the route is a release link followed by an asset link, without an extra expansion. I stopped before downloading. "Stable" tells me the release channel, but I still have to infer that this first entry is the latest. AppImage, updater JSON and APK get equal visual treatment. On the phone, RSS, tag, commit and Compare take space before the release title. The APK falls below the first viewport. I want a clear Android/Linux choice above the notes. Screenshots: `branchroom-murmur-releases-desktop.png`, `branchroom-murmur-releases-mobile.png`.

## Murmur pull request

GitHub, https://github.com/semyonfox/murmur/pull/21: The purple Merged badge and destination branch answer what happened. Conversation, Commits, Checks and Files changed are where I expect. I see individual commits starting below the description. Mobile has a large title and a horizontally crowded tab strip, so it is not particularly elegant either. Screenshots: `github-murmur-pr21-desktop.png`, `github-murmur-pr21-mobile.png`.

Branchroom, http://10.0.0.5:3080/semyon/murmur/pulls/21: Desktop gets more of the conversation into view, which I like. The repeated "Migrated from github.com" text and generic cup avatar make the timeline look less personal. The empty reviewers, labels, milestone, projects and assignees sections resemble an unconfigured admin screen. I look for the Checks tab I just saw on GitHub and cannot find an equivalent here. That is an observation about this imported PR, not proof that CI failed. Mobile is clumsy: the title occupies five lines, then the merged badge, author and centred merge sentence compete across one row. The deleted branch's long struck-through name adds noise. Files Changed is partly off-screen. Screenshots: `branchroom-murmur-pr21-desktop.png`, `branchroom-murmur-pr21-mobile.png`.

## Oghma home

GitHub, https://github.com/semyonfox/oghma: The big repository makes the usual file-first layout feel excessive. The README article starts about 2,428 CSS pixels down on desktop. The About link is useful, and blue topic pills are easier to distinguish from surrounding text than Branchroom's grey rectangles. On mobile, About still postpones the files until well down the screen. Some desktop sidebar sections remained skeleton placeholders during my visits, so I cannot judge their eventual contents. Once reached, the README has a comfortable reading width. Screenshots: `github-oghma-home-desktop-settled.png`, `github-oghma-home-mobile.png`, `github-oghma-readme-desktop.png`.

Branchroom, http://10.0.0.5:3080/semyon/oghma: Dense rows help the desktop file list, but the README container still starts about 2,065 CSS pixels down. The sidebar Readme link is the useful escape route. Its document rendering is clean, though the wider text measure makes paragraphs and bullets feel more tiring than GitHub's. I notice seven releases in the tab but no release section in the sidebar. On the phone, the separate size, stars, watchers and forks rows feel especially wasteful. Search code and Go to file also read as competing ways to find something. Screenshots: `branchroom-oghma-home-desktop.png`, `branchroom-oghma-home-mobile.png`, `branchroom-oghma-readme-desktop.png`.

## Oghma releases

GitHub, https://github.com/semyonfox/oghma/releases: The current Android alpha is obvious and correctly marked Pre-release. Its Assets section is collapsed, unlike Murmur's latest stable release, so getting the APK takes another disclosure step. The left release index is useful on desktop. On the phone the release notes occupy the screen before I get near the installer. Screenshots: `github-oghma-releases-desktop.png`, `github-oghma-releases-mobile.png`.

Branchroom, http://10.0.0.5:3080/semyon/oghma/releases: There is the alpha I could not see in the home sidebar. Its APK is exposed without expanding Downloads, which is better for this task than GitHub's collapsed assets. The absence of a sidebar release link on home matters more now. On the phone, the long tag wraps awkwardly beside a commit hash and Compare before I even reach the title. Both sites make me read release notes before offering the thing I came to install. Screenshots: `branchroom-oghma-releases-desktop.png`, `branchroom-oghma-releases-mobile.png`.

## Oghma pull request

GitHub, https://github.com/semyonfox/oghma/pull/720: This large release summary is surprisingly scannable because linked PRs carry descriptive titles. I can pick out the notes fix or mobile change without opening each link. The mobile title and tabs take space, but the merge summary reads naturally below the badge. Screenshots: `github-oghma-pr720-desktop.png`, `github-oghma-pr720-mobile.png`.

Branchroom, http://10.0.0.5:3080/semyon/oghma/pulls/720: My eye goes to +10254/-4898 and 336 changed files, which quickly establish the size of this merge. Then I lose the descriptive linked titles: the same summary starts with bare references such as #719 and #718. Some explanation remains after each number, but the strong scanning cues are gone. This may be import/rendering behaviour rather than authored content loss. Mobile repeats the awkward merge-header layout and hides the actual section name behind an underlined ellipsis. The first comment header spends another line telling me it was migrated. Screenshots: `branchroom-oghma-pr720-desktop.png`, `branchroom-oghma-pr720-mobile.png`.

## Top eight Branchroom UI changes, ranked

1. Compact mobile home metadata into one social-stat row, demote repository size and move search below the project summary so files and README arrive sooner. Screenshot: `branchroom-oghma-home-mobile.png`.
2. Put clearly labelled Android and Linux installer links above release notes, with updater JSON and source archives as secondary downloads. Screenshot: `branchroom-murmur-releases-mobile.png`.
3. Preserve or resolve descriptive PR titles in imported cross-references so release summaries remain scannable without opening each number. Screenshot: `branchroom-oghma-pr720-desktop.png`.
4. Show the newest prerelease in the home sidebar with an explicit prerelease badge when no stable release exists. Screenshot: `branchroom-oghma-home-desktop.png`.
5. Keep the current mobile section name visible and make Pull Requests and Releases discoverable instead of showing an active underline beneath an anonymous ellipsis. Screenshot: `branchroom-oghma-releases-mobile.png`.
6. Hide commit-message columns on narrow file lists and give filenames the width currently wasted on truncated fragments. Screenshot: `branchroom-murmur-home-mobile.png`.
7. Stack the mobile PR badge and merge sentence in reading order, use a smaller title and wrap branch names without the current three-column jumble. Screenshot: `branchroom-murmur-pr21-mobile.png`.
8. Collapse empty PR sidebar fields and reduce repeated migration notices so useful conversation content carries the visual weight. Screenshot: `branchroom-murmur-pr21-desktop.png`.
