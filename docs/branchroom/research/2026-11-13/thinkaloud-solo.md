# Usability Test Report: Branchroom Self-Hosted Forge Evaluation

**Tester Persona:** Undergraduate Computer Science Student  
**Experience Level:** Daily GitHub user for 3+ years (coursework, lab group repos, hackathons)  
**Target Platform:** Branchroom staging instance (`http://10.0.0.5:3402`)  
**Evaluation Methodology:** Think-aloud protocol across eight core developer journeys  
**Date of Evaluation:** 13 November 2026  

---

## Executive Summary

As a computer science student accustomed to GitHub as second nature, testing Branchroom (a fork of Gitea) feels immediately familiar yet peppered with subtle stumbling blocks. The core Git operations, pull request diffing, and code search perform with impressive speed on local hardware. The lightweight footprint and lack of sluggish JavaScript bloat are delightful compared to heavier modern web platforms.

However, several user experience friction points hinder a seamless transition from GitHub:
1. Form affordances are frequently hidden behind collapsible toggles or secondary icon buttons (such as the SSH key creation form and the compare-page pull request creator) where GitHub presents open forms by default.
2. SSH host configuration defaults to the operating system daemon user (`semyon`) rather than the universal `git` user, breaking terminal muscle memory.
3. Reviewer assignment enforces strict prior repository collaboration without helpful inline prompts, leaving dropdowns disabled with no explanatory tooltip.
4. Public repository migrations from GitHub disable issues and pull requests unless a personal access token is provided, surprising users who know these assets are public on GitHub.

Overall, Branchroom demonstrates high technical competence, but refining its workflow conventions to match common developer expectations will dramatically lower onboarding barriers.

---

## Task-by-Task Narrated Walkthrough

### Task 1: Repository Creation, SSH Configuration, and Initial Push

* **Goal:** Set up an SSH key, create a new repository with a README, clone via both SSH and HTTP, add Python project source files, and push to main.
* **Success:** Yes.
* **Time Taken:** 3 minutes 45 seconds.
* **Screenshots:** `00_login_page.png`, `01_dashboard.png`, `02_ssh_keys_settings.png`, `02b_ssh_panel_expanded.png`, `02c_ssh_key_filled.png`, `03_ssh_key_added.png`, `04_create_repo_page.png`, `05_create_repo_filled.png`, `06_repo_homepage.png`, `07_clone_ssh_dropdown.png`, `08_repo_after_push.png`.

#### Think-Aloud Narration
"I log in with my `ux-solo` student credentials. The dashboard opens quickly. The layout is clean and responsive. The first thing I need to do before cloning anything is add my SSH key, just like on GitHub. I click my profile avatar in the top right and head into Settings.

In Settings, I look for 'SSH / GPG Keys'. The menu item is right there in the sub-navigation. On GitHub, there is a large green 'New SSH key' button at the top right of the key list. Here, I see the panel header 'Manage SSH Keys' with a small button labelled 'Add Key'. When I click it, a collapsible form slides open. It is clean, but having the form collapsed by default when no keys exist caused a moment of hesitation. I generate an Ed25519 key on my laptop, paste the public key into the Content box, name it 'laptop-key', and click 'Add Key'. The key immediately appears with its SHA256 fingerprint.

Next, I need to create a repository for my university coursework tracker, `study-tracker`. I click the '+' icon in the top header and select 'New Repository'. The form is straightforward: owner is `ux-solo`, repository name is `study-tracker`, and visibility is set to Private. I check 'Initialize Repository' with a Default README. I hit 'Create Repository'. The repository lands on its homepage in under half a second.

Now to clone. On GitHub, the green '<> Code' button opens a popup with tabs for HTTPS, SSH, and GitHub CLI. In Branchroom, there is a similar 'Code' dropdown button. When opened, it provides copyable SSH and HTTPS URLs. 

Here comes my first real stumble: the SSH URL is `ssh://semyon@10.0.0.5:2222/ux-solo/study-tracker.git`. In my head, every Git forge uses `git@<host>`. Because the server runs under the OS account `semyon` without an explicit `SSH_USER = git` mapping in the config, typing `git@10.0.0.5` fails with public key rejection. Once I copy the exact URL provided in the dropdown, `git clone` connects cleanly to port 2222, greets me with 'Hi there, ux-solo!', and clones the repository. I also test cloning over HTTP to verify credentials, which succeeds without issue.

I populate the project with `tracker.py` (which contains `calculate_weighted_score`), `courses.py`, and `config.json`. I commit the files and push over SSH to `origin main`. Refreshing the web UI shows all files, commit messages, and the rendered README instantly."

---

### Task 2: Feature Branch, Pull Request, and Reviewer Assignment

* **Goal:** Create a feature branch `feature/grades-validation`, commit code improvements, push to origin, open a pull request, and request `ux-contractor` as reviewer.
* **Success:** Yes (with minor friction on reviewer collaboration requirements).
* **Time Taken:** 3 minutes 10 seconds.
* **Screenshots:** `09_repo_banner_check.png`, `10_pulls_list_empty.png`, `11_compare_page.png`, `11b_compare_form_expanded.png`, `12_pr_form_filled.png`, `13_pr_created_page.png`, `14_collaboration_settings.png`, `15_pr_reviewers_enabled.png`, `16_reviewer_dropdown_open.png`, `17_reviewer_requested.png`.

#### Think-Aloud Narration
"In my terminal, I create a feature branch: `git checkout -b feature/grades-validation`. I edit `tracker.py` to add a new `validate_scores(scores)` function, commit it, and push with `git push -u origin feature/grades-validation`. The terminal output helpfully prints a direct link to create a pull request.

I switch back to the browser and refresh the repository homepage. On GitHub, the moment you push a branch, a bright yellow banner appears across the top of the code browser saying 'feature/grades-validation had recent pushes less than a minute ago [Compare & pull request]'. In Branchroom, no such banner appears. I have to click the 'Pull Requests' tab manually.

On the Pull Requests page, the list is empty. I click 'New Pull Request', which takes me to the compare page (`/compare/main...feature/grades-validation`). Here is another surprise: the page shows the commit comparison and diff, but there is no title or description box. On GitHub, comparing branches that differ immediately presents the open pull request composition form. In Branchroom, the form is hidden behind an action button labelled 'New Pull Request'. Clicking that button finally reveals the title input and markdown editor.

I fill in the title 'Add score validation logic' and description, then click 'Create Pull Request'. The PR is created at `/ux-solo/study-tracker/pulls/1`.

Now I need my classmate / contractor friend `ux-contractor` to review my code. I look at the right sidebar. There is a 'Reviewers' section, but the dropdown is greyed out and unclickable. There is no tooltip or helper text explaining why. On GitHub, if you try to add someone to a private repository, it prompts you to invite them as a collaborator. Here, the control simply sits disabled.

I realise that `ux-contractor` is not yet a collaborator on this private repository. I navigate to repository Settings -> Collaborators, search for `ux-contractor`, and click 'Add Collaborator'. Returning to the pull request page, the Reviewers dropdown is now active. I click it, select `ux-contractor`, and their name appears under Reviewers. The interaction works, but the initial silent disablement was confusing."

---

### Task 3: Peer Code Review, Inline Suggestions, and Requesting Changes

* **Goal:** As `ux-contractor`, inspect the pull request, leave an inline comment with a suggested change using the suggestion mechanism, and submit the review with 'Request changes'.
* **Success:** Yes.
* **Time Taken:** 2 minutes 30 seconds.
* **Screenshots:** `18_contractor_dashboard.png`, `19_contractor_pr_view.png`, `20_files_changed_tab.png`, `21_diff_row_hover.png`, `22_inline_comment_box_opened.png`, `23_suggest_change_clicked.png`, `23_suggest_change_filled.png`, `24_review_started_pending.png`, `25_review_modal_opened.png`, `26_request_changes_panel.png`, `27_review_submitted_conversation.png`.

#### Think-Aloud Narration
"I open a clean browser session and log in as `ux-contractor`. On my dashboard, I can see the repository in my shared feeds. I navigate straight to `http://10.0.0.5:3402/ux-solo/study-tracker/pulls/1`. The PR banner notes that my review was requested.

I click on the 'Files Changed' tab (`/pulls/1/files`). The side-by-side / unified diff viewer is crisp and fast. I locate `def validate_scores(scores):`. Hovering over the line displays a blue '+' button on the line number. I click it, and an inline comment box expands.

In the toolbar of the comment box, I spot an icon button titled 'Suggest change' with a Git diff icon. I click it. Delightfully, it automatically inserts a markdown suggestion block:
```suggestion
def validate_scores(scores):
```
This is identical to GitHub's suggestion functionality. I edit the block to suggest adding a configurable parameter:
```suggestion
def validate_scores(scores, max_score=100):
```
Below the suggestion block, I add an explanatory note: 'Please make max_score configurable rather than hardcoding 100, as some assignments are marked out of 50 or 20.'

I click 'Start review'. The comment is saved in a pending state with a 'Pending' badge. At the top right of the diff viewer, the 'Review' button now displays a counter indicating 1 pending comment.

I click the 'Review' button. A popup panel opens containing a general review summary textarea and three action buttons: 'Approve', 'Comment', and 'Request changes'. Unlike GitHub, which uses radio buttons and a single 'Submit review' button, Branchroom uses dedicated action buttons. I type 'Please parameterise max_score rather than hardcoding 100 before we merge' into the textarea and click 'Request changes'.

The page reloads to the conversation timeline. The review is recorded with a red cross icon and the status 'ux-contractor requested changes'. The suggestion block renders with red and green diff highlights."

---

### Task 4: Applying Suggestions, Approval, Squash-Merge, and Branch Deletion

* **Goal:** As `ux-solo`, apply the suggested change directly from the web interface, have `ux-contractor` approve the pull request, perform a squash-merge, and delete the feature branch.
* **Success:** Yes.
* **Time Taken:** 2 minutes 15 seconds.
* **Screenshots:** `28_solo_views_suggested_change.png`, `29_suggestion_applied.png`, `30a_approve_panel.png`, `30_review_approved.png`, `31a_merge_box_initial.png`, `31b_merge_dropdown_open.png`, `31c_squash_selected.png`, `31d_squash_form_opened.png`, `31_squash_merged_branch_deleted.png`, `32_branches_list_after_delete.png`.

#### Think-Aloud Narration
"Back in my `ux-solo` session, I view the pull request conversation tab. Under `ux-contractor`'s review, the suggested diff is visible. Directly on the suggestion box is a button labelled 'Apply suggestion'.

I click 'Apply suggestion'. In under half a second, the page updates. In my terminal, running `git fetch origin` reveals a new commit on the remote branch: `Apply suggestion from review`. This is fantastic; on GitHub, students rely heavily on this one-click suggestion application during collaborative course projects.

Now my reviewer needs to sign off. Switching back to `ux-contractor`, I open the Files Changed tab, click the 'Review' button, leave a note saying 'Looks good now. Approved!', and click 'Approve'. The PR timeline updates to show a green checkmark and 'Approved'.

Switching back to `ux-solo`, the merge box at the bottom of the pull request is now green. By default, the button says 'Create merge commit'. Next to it is a dropdown arrow. Clicking the arrow presents merge options:
- Create merge commit
- Rebase, then fast-forward
- Rebase, then create merge commit
- Create squash commit
- Fast-forward only

I select 'Create squash commit' and click the button. A squash details form appears, showing the squash commit title `Add score validation logic (#1)` and the combined commit messages. Below the message box is a checkbox: 'Delete branch after merge'. I check it and click 'Create squash commit'.

The pull request transitions to 'Merged'. A dedicated 'Delete Branch' button also appears in the merge summary block. Clicking it removes `feature/grades-validation` from the server completely, which I confirm via `git ls-remote origin`. The entire merge and cleanup workflow felt smooth and reliable."

---

### Task 5: Repository Code Search

* **Goal:** Locate the Python function `calculate_weighted_score` using repository code search.
* **Success:** Yes.
* **Time Taken:** 20 seconds.
* **Screenshots:** `33a_code_search_input.png`, `33_code_search_results.png`.

#### Think-Aloud Narration
"I want to verify that code search works within the repository. On GitHub, you press '/' or click the top search input and filter by repository. 

In Branchroom, I notice a prominent search input right in the repository sub-header next to the branch selector: 'Search code...' with a keyboard shortcut badge `<kbd>S</kbd>`. 

I type `calculate_weighted_score` and hit Enter. The search results page (`/ux-solo/study-tracker/search?q=calculate_weighted_score`) loads immediately. It displays `tracker.py`, highlighting lines 3 and 4 with the function definition and docstring, along with the file language (Python) and a 'View File' link. 

The indexing on this instance is instantaneous and the search interface is simpler and less cluttered than GitHub's new code search."

---

### Task 6: Tag and Release Creation with File Attachments

* **Goal:** Create tag `v1.0.0`, publish a formal release with markdown release notes, and attach a file asset (`checksums.txt`).
* **Success:** Yes.
* **Time Taken:** 1 minute 15 seconds.
* **Screenshots:** `34a_releases_page_empty.png`, `34b_new_release_form.png`, `34c_release_form_filled.png`, `34_release_published.png`.

#### Think-Aloud Narration
"In coursework repositories, creating tagged releases is essential when submitting milestones. I click the 'Releases' tab in the repository navigation. The list is empty.

I click 'New Release'. The form prompts for:
- Tag Name: I enter `v1.0.0`
- Target Branch: `main`
- Release Title: `v1.0.0 - Initial Stable Release`
- Content: I write markdown release notes detailing the modules and validation logic.

At the bottom of the form is a file dropzone labelled 'Drop files here to upload'. I select a local `checksums.txt` file. The upload completes with a green progress bar.

I click 'Publish Release'. The release page renders beautifully, showing the tag, author badge, formatted markdown notes, auto-generated source archives (zip and tar.gz), and my attached `checksums.txt` file under Assets. This matches GitHub Releases feature-for-feature."

---

### Task 7: Notifications and Webhooks Discovery

* **Goal:** Check pending notifications and locate Webhook or Push Mirror settings within two minutes.
* **Success:** Yes.
* **Time Taken:** 30 seconds total (found in under 1 second).
* **Screenshots:** `35_notifications_page.png`, `36a_repo_settings_nav.png`, `36b_add_webhook_dropdown.png`, `36c_new_webhook_form.png`, `36d_webhook_form_filled.png`, `36e_webhook_added.png`, `36_webhooks_page.png`.

#### Think-Aloud Narration
"In the top navigation header, there is a notification bell displaying the number '2'. I click it and arrive at `/notifications`. Two unread notifications are present: one for `ux-contractor` requesting changes on PR #1, and one for the subsequent review approval. Each item has clear links and action buttons to pin or mark as read.

Next, I need to find where webhooks or push mirrors live. I set a timer. I navigate to repository 'Settings'. 

On the left sidebar, there is an item labelled 'Webhooks' (`/settings/hooks`). I found it in literally half a second. I click it, then click 'Add Webhook'. A dropdown offers various webhook formats (Gitea, Discord, Slack, Telegram, Packagist). I choose Gitea, enter `http://10.0.0.5:9999/test-webhook`, select 'All Events', and click 'Add Webhook'. The webhook is active immediately with a test delivery button.

I also check where Push Mirrors reside: right on the main repository Settings page under 'Mirror Settings'. Both configurations were discoverable with zero friction."

---

### Task 8: Migration of a Public GitHub Repository

* **Goal:** Migrate a public GitHub repository (`https://github.com/octocat/Hello-World`) using the New Migration interface, including issues and pull requests.
* **Success:** Partial (Git commits migrated successfully; issues and pull requests were blocked by token requirement).
* **Time Taken:** 2 minutes 10 seconds.
* **Screenshots:** `37_migration_services.png`, `38_github_migration_form.png`, `39_migration_form_filled.png`, `40_migrated_repo_homepage.png`, `41_migrated_issues.png`, `42_migrated_pulls.png`.

#### Think-Aloud Narration
"I want to test migrating an existing public GitHub project to Branchroom. I click the '+' icon in the top header and select 'New Migration'.

The initial page (`/repo/migrate`) is impressive: it displays dedicated service cards for Git, GitHub, GitLab, Gitea, Bitbucket, Forgejo, Gogs, OneDev, GitBucket, and CodeCommit. I click the GitHub card.

The GitHub migration form opens (`/repo/migrate?service_type=2`). I paste `https://github.com/octocat/Hello-World` into the 'Migrate / Clone From URL' field. 

Here is the major point of friction: under 'Migration Items', there are checkboxes for Labels, Issues, Pull Requests, Releases, and Milestones. However, all of these checkboxes are disabled and greyed out. A warning banner states:
`Access Token is required to migrate additional items`

As a student developer migrating an open-source or public school project, I know that GitHub's public REST and GraphQL APIs serve public repository issues and pull requests without requiring authentication (subject to IP rate limits). On Branchroom, the backend refuses to even attempt fetching issues or pull requests unless a GitHub Personal Access Token is supplied.

I proceed with the migration without a token to see what happens. The Git repository itself clones quickly, creating `ux-solo/Hello-World` with all branches and commit history intact. But navigating to `/issues` and `/pulls` displays zero items. For developers expecting a one-click migration of public projects, requiring a personal token is an unexpected hurdle."

---

## Ranked Friction Points

| Rank | Severity | UI Location / URL | GitHub Expectation | Branchroom Reality | Screenshot Reference |
| :--- | :--- | :--- | :--- | :--- | :--- |
| **1** | **Major** | `/repo/migrate?service_type=2` (New Migration) | Public repositories can be imported with public issues and PRs without personal authentication. | All migration items (Issues, PRs, Labels, Releases) are locked behind a mandatory GitHub Access Token. | `38_github_migration_form.png`, `41_migrated_issues.png` |
| **2** | **Major** | `/ux-solo/study-tracker/compare/main...<branch>` | Comparing differing branches immediately shows open title and comment inputs to create a pull request. | Creation form is hidden; user must click a secondary 'New Pull Request' link to expand the form. | `11_compare_page.png`, `11b_compare_form_expanded.png` |
| **3** | **Major** | `ux-solo/study-tracker/pulls/1` (Sidebar) | Can search and request review from team members, or receive an explanatory prompt to add them as a collaborator. | Reviewers dropdown is silently disabled (`ui dropdown full-width disabled`) with no tooltip or helper message. | `15_pr_reviewers_enabled.png`, `14_collaboration_settings.png` |
| **4** | **Minor** | Repo Homepage / `<> Code` dropdown | Default SSH clone user is universally `git` (`git@<host>:<repo>`). | Server runs under host account `semyon` without `SSH_USER = git`, causing key rejection if typed manually. | `07_clone_ssh_dropdown.png` |
| **5** | **Minor** | Repo Homepage after branch push | Prominent yellow banner appears: '`<branch>` had recent pushes [Compare & pull request]'. | No recent push banner appears on repository homepage; user must navigate to the Pull Requests tab. | `09_repo_banner_check.png`, `10_pulls_list_empty.png` |
| **6** | **Minor** | `/user/settings/keys` | Prominent 'New SSH key' green button linking to an open creation page. | Form is collapsed inside a panel header button ('Add Key'), easy to overlook when list is empty. | `02_ssh_keys_settings.png`, `02b_ssh_panel_expanded.png` |

---

## Anomalies and Bugs Encountered

### 1. Incomplete Migration of Public GitHub Repository Metadata
* **Reproduction Steps:**
  1. Navigate to `/repo/migrate?service_type=2`.
  2. Enter public URL `https://github.com/octocat/Hello-World` without filling in the Access Token field.
  3. Observe that 'Issues' and 'Pull Requests' checkboxes are completely disabled.
  4. Submit migration.
* **Actual Result:** Only Git commits are migrated; repository issues and PRs remain at zero.
* **Expected Result:** For public repositories, allow fetching public issues and pull requests via unauthenticated GitHub API endpoints, with a warning about potential API rate limits if the repository is large.

### 2. Silent Disablement of Reviewer Dropdown on New Pull Requests
* **Reproduction Steps:**
  1. Create a private repository as user A.
  2. Push a branch and open a pull request.
  3. Attempt to assign user B (another registered user on the instance) as a reviewer from the PR sidebar.
* **Actual Result:** The 'Reviewers' dropdown has class `disabled` and does not react to clicks. No tooltip or helper text explains that user B must first be added as a collaborator.
* **Expected Result:** Either allow selecting any registered user (automatically prompting to grant read access), or display an explanatory tooltip: "Only repository collaborators can be requested as reviewers. [Add Collaborator]".

---

## Three Key Recommendations to Improve Experience

### 1. Auto-Expand Pull Request Creation on Branch Compare
On GitHub, navigating to compare two branches that contain differences immediately presents the PR title input and markdown description box pre-filled with commit messages. In Branchroom, hiding this form behind an extra 'New Pull Request' button forces an unnecessary click and causes confusion for users wondering why they cannot submit their PR. 
* **Action:** Automatically display the pull request form in an expanded state whenever comparing branches with mergeable commits.

### 2. Add Recent Push Banners on Repository Homepages
GitHub's 'Compare & pull request' banner when a branch was recently pushed is one of its most celebrated workflow shortcuts. Introducing a floating banner on the repository homepage when the authenticated user pushed a branch within the past 15 minutes will streamline the developer loop significantly.
* **Action:** Render a contextual prompt at the top of the repository file list offering a direct one-click link to create a pull request for recently pushed branches.

### 3. Graceful Reviewer Onboarding with Contextual Actions
Collaborative workflows depend on quick peer reviews. Silently disabling the Reviewers dropdown when no outside collaborators exist leaves developers at a dead end.
* **Action:** Keep the Reviewers dropdown interactive. If no eligible collaborators exist, display a clear action within the dropdown menu: "No collaborators found. Click here to add a collaborator to this repository."

---

## Conclusion
Branchroom provides a fast, lightweight, and robust self-hosted Git platform that handles complex developer tasks (such as suggestion blocks, one-click diff patching, squash merges, release asset bundling, and code indexing) with distinction. Addressing minor UI discoverability gaps and aligning default conventions with modern forge expectations will make it an unbeatable drop-in alternative for GitHub developers.
