# GitHub vs Gitea Feature Parity Assessment

This assessment reviews the functional differences between GitHub and the current Gitea codebase (`review-branchroom-audit-d78448905844f068`, November 2026) for a solo developer managing approximately 90 repositories with active CI/CD, releases, issues, reviews, and dependency update bots.

## Feature Parity Table

| Feature | GitHub | This Gitea Build | Gap Severity | Cheapest Fix |
| :--- | :--- | :--- | :--- | :--- |
| **PR Reviews: Suggested Changes** | Markdown suggestions via ````suggestion ... ```` with inline diff and one-click commit buttons (single or batch). | Not supported. Code comments are standard markdown without patch application UI. In `templates/repo/diff/comment_form.tmpl:19-27` and `models/issues/comment.go:274-320`, comments only accept text content. `services/feed/notifier_test.go:8` treats ````suggestion` as raw string data. | Annoying | Code change (L): Requires comment AST parsing, diff proposal rendering, and git commit patch applicator service. |
| **PR Reviews: Multi-line Comments** | Drag-to-select range across multiple lines in the diff viewer. | Single-line only. Comment model stores only one line index (`Line int64` in `models/issues/comment.go:318`). Diff click handler in `web_src/js/features/repo-issue.ts:289` passes a single `idx` attribute into `input[name='line']`. | Annoying | Code change (M): Add `line_start` and `line_end` fields to comment model, update diff template range anchors, and add drag selection in `web_src/js/features/repo-diff.ts`. |
| **PR Reviews: Review Requests** | Request review from individual users and teams; re-request reviews on push. | Fully supported for both users and teams via `services/issue/review_request.go:19-48`, `models/issues/review.go:12-45`, and `templates/repo/issue/sidebar/reviewer_list.tmpl`. | None | None (built-in). |
| **PR Reviews: CODEOWNERS** | Evaluates `.github/CODEOWNERS`, `CODEOWNERS`, and `docs/CODEOWNERS`. Blocks PR merge if required in branch protection. | Supported, but candidate paths are hardcoded to `CODEOWNERS`, `docs/CODEOWNERS`, and `.gitea/CODEOWNERS` in `services/issue/pull.go:14`. Path `.github/CODEOWNERS` is ignored. Enforced via branch protection in `routers/web/repo/issue_view.go:102-106`. | Minor | Repo adjustment / Config: Symlink or move `.github/CODEOWNERS` to `.gitea/CODEOWNERS` or root. (Or Code change S: add `".github/CODEOWNERS"` to `codeOwnerFiles` in `services/issue/pull.go:14`). |
| **PR Reviews: Merge Queue** | Native merge queue serialising PRs into speculative test branches before merging to main. | No merge queue. Internal `services/automergequeue/automergequeue.go:19-35` is merely a worker pool for scheduling single PR merges upon CI completion (`services/pull/merge.go:210-235`). No multi-PR batching or speculative builds. | Minor | Code change (L): Complex scheduler with speculative test branches and CI integration. (Low impact for a solo developer). |
| **PR Reviews: Auto-merge** | Built-in auto-merge option ("Enable auto-merge") when branch protection checks pass. | Supported natively ("Schedule auto-merge when checks succeed") in `services/automerge/automerge.go:1-50`, `routers/web/repo/pull_merge_form.go:15-30`, and `templates/repo/issue/view_content/pull_merge_box.tmpl`. | None | None (built-in). |
| **PR Reviews: Draft PRs** | Dedicated `draft: true` state with UI toggle, distinct badge, and suppression of review notifications. | Implemented via title prefixes (`WIP:`, `[WIP]`) configured in `modules/setting/repository.go:130` and evaluated by `models/issues/pull.go:88-115`. Sidebar toggle in `templates/repo/issue/sidebar/wip_switch.tmpl` prepends or strips the title string. The API maps `Draft: pr.IsWorkInProgress(ctx)` in `services/convert/pull.go:48`. | Minor | Config: Customise `WORK_IN_PROGRESS_PREFIXES = WIP:,[WIP]` under `[repository.pull-request]` in `app.ini`. |
| **PR Reviews: Stacked PRs** | Arbitrary branch targeting; tools like Graphite/gh-stack automate reparenting. | Arbitrary branch targeting supported. Gitea automatically retargets child PRs to default branch when parent branch merges if `RetargetChildrenOnMerge` is enabled (`services/pull/pull.go:1020-1035`, defaulting to `true` in `modules/setting/repository.go:135`). Issue dependencies can also block merging (`models/issues/dependency.go:10-40`). | Minor | None (default configuration already handles branch retargeting). |
| **Actions: Workflow Compatibility** | Native GitHub Actions runner engine. | Reads `.github/workflows` and `.gitea/workflows` (`modules/setting/actions.go:48-52`). Supports matrix builds (`services/actions/matrix.go`), reusable workflows (`services/actions/reusable_workflow.go`), concurrency (`services/actions/concurrency.go`), OIDC tokens (`services/actions/oidc.go`), and step summaries (`routers/api/actions/artifacts.go:127`). | Minor | Existing tool: Use `DEFAULT_ACTIONS_URL = github` so third-party marketplace actions resolve from GitHub. |
| **Actions: Runner Infrastructure** | Free hosted runners (Linux, macOS, Windows) with broad runtime tooling pre-installed. | No built-in runner daemon. Requires deploying and maintaining `act_runner` as a separate daemon (Docker or systemd). Standard Docker images are either bare (missing build tools) or heavy (20GB+ for `catthehacker/ubuntu:act-latest`). Windows/macOS runners require dedicated host machines. | Blocker / Annoying | Existing tool: Deploy `act_runner` using Docker socket or binary mode, mapped to appropriate base images. |
| **Actions: Cache (`actions/cache`)** | Native 10GB per repository cache backend. | Gitea server does not host an Actions cache service (`_apis/artifactcache`). The runner fails on `actions/cache` unless `act_runner` internal cache server is explicitly enabled and assigned local storage. | Annoying | Existing tool config: Enable `cache.enabled: true` and configure `cache.dir` inside `act_runner` `config.yaml`. |
| **Dependency Update Bots** | Built-in Dependabot version and security updates via `dependabot.yml` without self-hosted infrastructure. | No native dependency bot. Gitea recognises `renovate.json` and `dependabot.yml` file icons (`options/fileicon/material-icon-rules.json:15`), but runs no updates itself. | Annoying | Existing tool: Run Renovate CLI via scheduled Gitea Actions workflow or a systemd cron container using `platform: 'gitea'` and a personal access token. |
| **Security Alerts & Secret Scanning** | Push protection blocks leaked secrets; automated vulnerability alerts via GitHub Advisory Database. | No secret scanner or push protection in core Gitea (`docs/branchroom/self-hosted-demo.md:15`). Secret tokens only carry prefixes to aid external scanners (`models/deploykey/deploykey_token.go:15`). No vulnerability advisory database. | Annoying | Existing tool: Install `gitleaks` or `trufflehog` in git pre-commit hooks or Actions CI; run `osv-scanner` or `trivy` in CI workflows. |
| **Code Search** | Instant regex, symbol, and scoped repository search across all branches and commits via Blackbird. | Code indexer is disabled by default (`RepoIndexerEnabled = false` in `modules/setting/indexer.go:48`). When enabled, Bleve indexes only default branches (`modules/indexer/code/indexer.go:68`) up to 1MB per file. No multi-branch or rich symbol search. | Annoying | Config: Enable `[indexer] REPO_INDEXER_ENABLED = true`, `REPO_INDEXER_PATH = indexers/repos.bleve`, and raise `MAX_FILE_SIZE = 5242880` in `app.ini`. |
| **Notifications & Email** | Web inbox, fine-grained thread subscriptions, mobile push notifications, and reply-by-email. | Basic web notification list (`templates/user/notification/notification.tmpl`). Reply-by-email is implemented via an IMAP IDLE background poller (`services/mailer/incoming/incoming.go:25-60`), but requires self-hosted IMAP/SMTP credentials. No push notifications. | Minor | Config / Existing tool: Configure `[mailer]` and `[mailer.incoming]` in `app.ini`; hook critical notifications to Telegram/Discord via `services/webhook/`. |
| **Mobile UX** | Official iOS and Android native apps with biometric authentication, triage filters, and push alerts. | Responsive web UI only (`web_src/css/repo.css`). Large diffs, tables, and project boards require horizontal scrolling. No official native mobile application. | Minor | Existing tool: Use mobile browser PWA or third-party open-source clients (e.g. GitJournal). |
| **Projects Boards** | Projects v2 (Memex) with table, board, roadmap views, custom fields, and automated workflows. | Basic Kanban boards only (`models/project/project.go:25`, `templates/repo/projects/view.tmpl`). Columns hold issue/PR cards. No custom attributes, table spreadsheet view, or roadmap Gantt charts. | Minor | Existing tool: Accept basic Kanban or use dedicated self-hosted project tools (Vikunja, Kanboard). |
| **Discussions** | Community forum with categories, answerable Q&A threads, and voting. | No Discussions module exists (`services/actions/permission_parser.go:50` only parses the token scope). Only Issues and Wiki are available. | Minor | Process / Config: Use an Issue label such as `discussion` or `rfc` to track non-actionable topics. |
| **Pages** | Native static site hosting with custom domains, automatic HTTPS, and branch builds. | No built-in Pages hosting service in standard Gitea (`services/wiki/wiki_path.go:10`). | Minor | Existing tool: Build static assets in Gitea Actions and deploy to Cloudflare Pages, Netlify, or a local reverse proxy (Caddy/Nginx). |
| **Packages Registry** | Containers (OCI), npm, maven, nuget, rubygems. | Supports 24+ package ecosystems: Alpine, Arch, Cargo, Composer, Conan, Conda, Container (OCI), CRAN, Debian, Generic, Go, Helm, Maven, npm, NuGet, Pub, PyPI, RPM, RubyGems, Swift, Terraform (`routers/api/packages/api.go`, `modules/setting/packages.go:15-45`). | None (Gitea win) | None (enabled by default under `[packages]`). |
| **GitHub CLI Equiv. (`tea`)** | `gh` CLI with rich interactive terminal UI, PR checks watch, extension marketplace, and browser commands. | `tea` CLI is maintained as a separate project. Handles basic PR/issue creation and listings, but lacks Actions run monitoring, log streaming, and extension support. | Annoying | Existing tool: Install `tea` with custom shell helper functions or wrap raw git commands. |
| **Webhooks & Integrations** | Wide ecosystem of GitHub Apps with fine-grained permissions and turn-key SaaS integrations. | Supports Slack, Discord, Telegram, Matrix, MSTeams, Feishu, Dingtalk, WeChat, and generic JSON payloads (`services/webhook/`). Lacks GitHub Apps architecture; external SaaS tools rarely support self-hosted Gitea directly. | Minor | Config: Use built-in messaging webhooks or generic webhook forwarders. |
| **Mirroring to GitHub** | Target repository for git push. | Push mirroring is built into Gitea (`services/mirror/mirror_push.go:26-55`). Automatically syncs branches, tags, and wiki to GitHub on push. Does not sync PR discussions, issue trackers, releases, or review threads. | None | Config: Add GitHub push mirror remote with Personal Access Token in repository settings. |

---

## Config-Only Wins for `app.ini`

A solo developer migrating to this Gitea build can immediately resolve several key functional gaps using configuration alone:

1. **Activate Code Search across all repositories**
   By default, repository code indexing is turned off. Turning it on enables full-text repository code search across your 90 repos:
   ```ini
   [indexer]
   REPO_INDEXER_ENABLED = true
   REPO_INDEXER_REPO_TYPES = sources,forks
   REPO_TYPE = bleve
   REPO_INDEXER_PATH = data/indexers/repos.bleve
   MAX_FILE_SIZE = 5242880
   REPO_INDEXER_EXCLUDE_VENDORED = true
   ```

2. **Actions GitHub Action resolution & Workflow directories**
   Ensure Gitea Actions recognises `.github/workflows` without modifying repository files, and resolves standard marketplace actions directly from GitHub:
   ```ini
   [actions]
   ENABLED = true
   DEFAULT_ACTIONS_URL = github
   WORKFLOW_DIRS = .gitea/workflows,.github/workflows
   ```

3. **Automatic PR retargeting on merge**
   Ensure child pull requests in stacked branch workflows are automatically retargeted to the default branch when their parent branch is merged and deleted:
   ```ini
   [repository.pull-request]
   RETARGET_CHILDREN_ON_MERGE = true
   DEFAULT_DELETE_BRANCH_AFTER_MERGE = true
   WORK_IN_PROGRESS_PREFIXES = WIP:,[WIP]
   ```

4. **Package registry storage and quotas**
   Ensure package registries are unrestricted so Cargo, PyPI, Container, npm, and Generic packages operate smoothly without artificial file limits:
   ```ini
   [packages]
   ENABLED = true
   LIMIT_TOTAL_OWNER_SIZE = -1
   ```

5. **Reply-by-email for PR and issue triage**
   If an IMAP mailbox is available, incoming mail processing allows replying to review comments and issues directly from your mobile email client without opening the web interface:
   ```ini
   [mailer]
   ENABLED = true
   FROM = gitea@example.com
   SMTP_ADDR = mail.example.com
   SMTP_PORT = 587

   [mailer.incoming]
   ENABLED = true
   REPLY_TO_ADDRESS = incoming+%{token}@mail.example.com
   HOST = imap.example.com
   PORT = 993
   USE_TLS = true
   USERNAME = incoming@mail.example.com
   PASSWORD = secret
   ```

6. **Automated push mirroring to GitHub**
   Configure repository push mirrors under repo settings to mirror all commits and tags to GitHub upon push, keeping your GitHub contribution graph active and providing an off-site git backup automatically.
