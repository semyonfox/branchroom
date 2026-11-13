# Alternatives research for Branchroom

Research date: 13 November 2026. Read-only market research. No repository changes.

Recommendation: keep Gitea's cheap, quick self-hosting and familiar repository workflow. Put the redesign effort into review continuity, predictable navigation and permissions that an administrator can explain. A calmer colour scheme will not fix lost comments, unclear access or a slow diff.

## Evidence and limits

This is qualitative research, not a representative survey or a performance benchmark. Searches covered Hacker News, Reddit, product trackers, forums, migration accounts, G2 and product documentation. Exa search worked initially, then hit its free MCP rate limit. Web search supplied the remaining discovery, and Exa page fetch continued to work.

First-hand reports identify friction. Documentation establishes supported behaviour. Vendor statements describe intent, not independently proven satisfaction. G2's summaries are labelled machine-generated on the site and are useful as corroboration, not prevalence estimates. Historical bugs are marked as such. GitLab feedback and performance issues were found, but reliable issue-level vote counts were not exposed in the retrieved pages. Do not describe them as verified high-upvote issues. Codeberg tracker pages were blocked by robots.txt, so Forgejo release notes and migration accounts supplement the direct tracker evidence.

The ranking below is a design judgement for Branchroom's restricted-workspace use case. Permission previews and workspace audit views are recommendations, not claims that these products already implement them.

## GitLab

Praise: users value the integrated CI, registries, review gates and self-hosted suite. Enterprise reviewers on G2 praise identity governance, audit trails and visibility across development. This is a real advantage for organisations that would otherwise assemble several systems.

Complaints: a small-team administrator reported activity, commit and job pages slowing to 3 to 5 seconds on GitLab 17.11 despite a 16-core, 16 GB server. This is one deployment, not a minimum hardware requirement. Navigation feedback records difficulty finding administration, too many clicks, awkward context switching, inconsistent pins and contrast concerns. G2 also records a heavy, complex interface and inconsistent UI performance. Merge request friction includes large-diff responsiveness and changing queues, expanded in the lessons below. The lesson is to preserve integrated capabilities without displaying the whole suite at once.

Sources:

- [GitLab 17.11 self-hosted performance report, May 2025](https://www.reddit.com/r/gitlab/comments/1kuxy2s/gitlab_ee_feels_much_slower_than_before_looking/)
- [GitLab navigation feedback and the team's action summary, 2023](https://gitlab.com/gitlab-org/gitlab/-/issues/409005)
- [G2 GitLab reviews, including enterprise governance praise and UI criticism](https://www.g2.com/products/gitlab/reviews?page=4)

## Gitea

Praise: the strongest recurring advantage is practical simplicity. In a February 2025 self-hosting discussion, a highly supported reply favoured its Go binary, lower resource burden and easier updates over GitLab. That discussion also includes users happy with everyday repository hosting. Familiarity reduces the cost of moving from GitHub.

Complaints: the same thread reports Actions compatibility problems with workflow variables. A UI proposal asks for an adjacent file explorer, joined search and filters, a more compact clone action, artifact management and links from Actions to variables and secrets. Its single thumbs-up is weak evidence of demand, but the specific tasks are useful hypotheses. The maintainer reply also says pinning already exists, showing that discoverability can look like missing functionality.

Administration deserves more scrutiny than its lightweight deployment reputation suggests. A maintainer's access-system proposal describes inconsistent checks, forgotten administrator cases and difficulty changing multiple entities together. This is engineering evidence of a permission problem, not a user survey or a reason to adopt the proposed wholesale rewrite.

For inherited review UI, inspect re-review state, outdated comments, line-range commenting and contextual blame. Concrete review reports appear in lessons 2 and 3. Do not assume that every gap in an old Gitea report persists in Branchroom's base version.

Sources:

- [Self-hosters comparing Gitea and GitLab, including Actions friction](https://www.reddit.com/r/selfhosted/comments/1il8yue/github_alternatives_gitea_vs_gitlab/)
- [Gitea UI proposal and maintainer response, 2024](https://github.com/go-gitea/gitea/issues/31125)
- [Maintainer proposal documenting access-system shortcomings, 2023](https://github.com/go-gitea/gitea/issues/28366)

## Forgejo

Praise: the Gitea-like workflow preserves familiarity. A 2025 migration account found the review and issue interface close to GitHub, with imports preserving numbers, labels and authorship. Forgejo's v8 notes explicitly prioritised stability and fewer arbitrary UI changes. This matters to occasional users who rely on remembered locations.

Complaints: Forgejo's own v8 account describes an organically accumulated UX whose small changes were not always intuitive. It started observing real users rather than accepting proposed UI solutions at face value. Review shortcomings were concrete: v16 introduced multiline review comments and fixed comments appearing beside the wrong diff, without a diff, or in the wrong location. Those are historical problems addressed in that release, not current missing features.

Actions gives migrants familiar YAML and an existing action ecosystem, but the migration writer found CI the hardest part and reported outdated hosted-service documentation in September 2025. Separate software compatibility, runner configuration and Codeberg capacity when diagnosing this. The evidence does not support treating Forgejo and Gitea as permanently identical.

Sources:

- [Forgejo v8, including its candid UX assessment and user research, July 2024](https://forgejo.org/2024-07-release-v8-0/)
- [Forgejo v16, including multiline review and comment placement fixes, July 2026](https://forgejo.org/2026-07-release-v16-0/)
- [Migration account comparing familiarity and CI difficulties, September 2025](https://unterwaditzer.net/2025/codeberg.html)

## Bitbucket

Praise: G2 reviewers value Jira integration, straightforward team reviews, Git integration and pipelines. It can be convenient for teams already using Atlassian tools.

Complaints: G2's summary and visible reviews repeatedly mention slow navigation, clutter, unnecessary clicks, weak discoverability and advanced capabilities behind paid tiers. Atlassian acknowledges that large PR diffs can fail to load with a generic error. A community report also describes timeouts on small diffs, so blaming every failure on oversized PRs is inadequate. These sources concern Cloud, not a benchmark of Bitbucket Data Center. The evidence here is stronger for review performance than for enterprise permission usability.

Sources:

- [G2 Bitbucket praise and complaints](https://www.g2.com/products/bitbucket/reviews?page=2&qs=pros-and-cons)
- [Atlassian support on large-diff review failures](https://support.atlassian.com/bitbucket-cloud/kb/percent22something-went-wrongpercent22-error-message-when-attempting-to-review-a-pull-request/)
- [Users reporting diff timeouts even for small changes, February 2022](https://community.atlassian.com/forums/Bitbucket-questions/Pull-request-diff-often-times-out/qaq-p/1941346)

## Azure DevOps Repos

Praise: enterprise users value connected work items, repositories, pipelines and enforced review policies. A G2 Server review reports smooth daily use once the project structure and permissions were established. Keep Server and Services separate when evaluating hosting and maintenance.

Complaints: a 2025 UI discussion describes dated screens, permissions spread across several locations, hidden hover controls, modal navigation and an awkward split between repository selection and PR navigation. A 2022 review complaint says truncated file content was signalled by an easy-to-miss label. That is a serious review-completeness problem, though its current status was not verified. Onboarding has two separate hurdles: understanding project/team/repository structure and gaining the right access.

Sources:

- [Azure DevOps UI discussion, July 2025](https://www.reddit.com/r/azuredevops/comments/1lvo9gs)
- [Reviewers encountering truncated content, April 2022](https://www.reddit.com/r/azuredevops/comments/ud4mct/how_do_you_deal_with_displayed_content_is/)
- [G2 Azure DevOps Server reviews](https://www.g2.com/products/azure-devops-server/reviews)

## Gerrit

Praise: experienced users favour one commit per review, persistent patchset history, comparisons between revisions and natural stacks. A self-hosted user calls the review UI responsive. Another discussion praises its attention set, which identifies whose action is needed next, and independently scored review requirements.

Complaints: users who like the review model still describe an unattractive UI, difficult administration and a substantial learning curve. A Reddit discussion explicitly asks for Gerrit's review power with GitHub-like usability. One HN user finds ordinary single-instance hosting easy while another calls administration painful. Deployment simplicity and policy configuration are different jobs. Gerrit's commit-oriented model also needs somewhere to discuss the intent of an entire stack.

Sources:

- [First-hand Gerrit praise, admin criticism and hosting discussion, March 2023](https://news.ycombinator.com/item?id=35326546)
- [Review workflow discussion covering attention sets and scoring, February 2024](https://news.ycombinator.com/item?id=39478904)
- [Request for Gerrit power with easier UX, December 2024](https://www.reddit.com/r/git/comments/1hi5us1/power_of_gerrit_with_the_ux_of_github/)

## Phabricator and Phorge

Praise: an Eventbrite user describes Differential's evolving patch reviews, Herald's reviewer-routing rules and the CLI's consistent workflow as stronger than GitHub after an adjustment period. HN users also remember moved-code highlighting, coverage in the gutter and stacked diffs as capabilities mainstream forges lacked. These are useful review references even when the surrounding application is not a good design model.

Complaints: the workflow has an adoption cost. Arcanist adds another tool and terminology. A January 2026 FreeBSD discussion criticises Phorge UX and says maintenance effort is being spent on PHP compatibility. Treat that as one participant's assessment, not proof that the project has no feature work. Do not transfer every old Phabricator complaint to current Phorge without checking.

Sources:

- [Eventbrite user's detailed Phabricator experience, June 2018](https://news.ycombinator.com/item?id=17247590)
- [Reviewers discussing missing Phabricator-style capabilities, July 2024](https://news.ycombinator.com/item?id=40949034)
- [FreeBSD discussion criticising Phorge UX and maintenance burden, January 2026](https://lists.freebsd.org/archives/freebsd-hackers/2026-January/005438.html)

## Sourcehut

Praise: it provides a deliberately small web interface, no mandatory JavaScript and participation through email without requiring an account for every interaction. Its founder's 2021 account describes continued usability on unreliable, high-latency connections. That is a vendor account, not a fresh comparative benchmark. Email-native contributors can keep local copies and work asynchronously.

Complaints: HN contributors describe difficulties updating web-submitted patches and joining discussions without an external mail client. Hand-wrapping messages and managing email quotations become onboarding requirements unrelated to the code change. Some contributors reject patch-by-email workflows outright. The 2020 report is historical, but the broader lesson remains relevant: a lightweight page does not guarantee an easy contribution workflow.

Sources:

- [Sourcehut product description](https://sourcehut.org/)
- [Founder on operation over poor connections, May 2021](https://sourcehut.org/blog/2021-05-08-sourcehut-is-the-fastest-who-cares/)
- [Sourcehut users debating onboarding and web/email friction, November 2020](https://news.ycombinator.com/item?id=25101359)

## Codeberg

Praise: a concrete migration account describes reliable import of issues, PRs and release artifacts, retaining issue numbers, labels and authorship. Familiar UI makes migration less intimidating. For these users, community hosting and control of their work are part of the appeal.

Complaints: CI migration, hosted capacity and availability of macOS runners create more friction than moving repository content. Those are service constraints, not proof that self-hosted Forgejo cannot do the job. Codeberg's own documentation describes failed imports leaving an inaccessible repository, with recovery requiring a direct settings URL and a retry. Calm onboarding must include progress, recoverable errors and clear next steps.

Sources:

- [First-hand Codeberg migration, September 2025](https://unterwaditzer.net/2025/codeberg.html)
- [Codeberg's import documentation and failed-migration recovery](https://docs.codeberg.org/advanced/migrating-repos/)
- [Codeberg's account of CI operating costs and runner-image constraints](https://blog.codeberg.org/letter-from-codeberg-looking-into-2025.html)

## OneDev

Praise: its review documentation shows useful integration within a self-hosted forge: symbol navigation, coverage and problem annotations, selection comments, suggestions, file-path filters and changes since a review. Reviews can also start outside a PR. Community responses include praise for an intuitive interface and an integrated workflow. Documentation establishes capability, not measured popularity or speed.

Complaints: the independent evidence is thinner than for GitLab or Bitbucket. A 2025 HN discussion reports inadequate submission feedback and debates the trust implications of Docker-socket access. Older Reddit feedback includes installation and container-permission friction. Do not turn those accounts into a blanket claim that OneDev is insecure or difficult for everyone. Its useful lesson is that integrated review context can coexist with a relatively focused product, but deployment and runner trust still need clear explanations.

Sources:

- [OneDev review workflow documentation](https://docs.onedev.io/tutorials/code/code-review)
- [HN discussion of UI feedback and runner trust, September 2025](https://news.ycombinator.com/item?id=45291590)
- [Self-hosters discussing OneDev deployment, May 2023](https://www.reddit.com/r/selfhosted/comments/13rhg63/what_selfhosted_git_server/)

## Graphite

Praise: users appreciate continuing dependent work while earlier changes are reviewed. Its stack tooling helps turn one large change into smaller review units. Even a critical HN discussion calls it a strong attempt to improve review on GitHub.

Complaints: that discussion reports friction collaborating on stacks, especially when teammates do not all use Graphite, and confusing merges through GitHub. A separate 2025 HN discussion includes reports of slow staged loading from notification links, leading reviewers to return to GitHub. These are dated experiences, not current timing measurements. The wider objection is asking a team to adopt another service, permissions model and review surface to fix a core forge workflow.

Sources:

- [Graphite discussion covering strengths, collaboration and workflow friction, February 2024](https://news.ycombinator.com/item?id=39478904)
- [Review-tool discussion including Graphite loading complaints, August 2025](https://news.ycombinator.com/item?id=44967469)
- [Users debating the practical value of stacked PRs, January 2024](https://www.reddit.com/r/programming/comments/192twep/never_wait_for_code_review_again_how_stacking/)

## Reviewable

Praise: its documentation supports arbitrary revision comparisons, file-level review state and detailed review completion. The file matrix and newer sidebar aim to keep checks and review context available without losing the code position. These are valuable capabilities for multi-round reviews.

Complaints: a 2022 HN discussion includes users describing a steep learning curve and poor performance. A 2023 HN discussion raises GitHub API limits, repository trust and synchronisation when only part of a team uses an overlay. These reports predate the newer sidebar, so they establish design hazards rather than its present performance. Fine-grained state becomes a burden if users cannot understand how a review becomes complete.

Sources:

- [Reviewable file and revision documentation](https://docs.reviewable.io/files)
- [Reviewable sidebar redesign](https://www.reviewable.io/blog/introducing-the-long-awaited-sidebar/)
- [HN discussion with first-hand Reviewable learning and performance complaints, July 2022](https://news.ycombinator.com/item?id=31841215)

## CodeApprove

Praise: its product design directly addresses hidden review work. It combines discussion, diff, history and CI, allows comments beyond changed lines, retains outdated threads and hands attention between author and reviewer. These are vendor claims about intended behaviour. The author also explains in an HN exchange that its GitHub App uses narrower access than full code-write permission.

Complaints: its public tracker contains reports about comments disappearing after force-push, unclear external-contributor handling, unexpected reviewer state and approval appearing before all threads are resolved. Requests for review-round grouping and clearer outdated-comment indication show that a single-page interface still needs legible chronology. Tracker entries may be old or partly addressed. They are reproducible design questions to check, not evidence every current review fails.

Sources:

- [CodeApprove product and review model](https://codeapprove.com/)
- [Public feedback tracker with bugs, confusion and feature requests](https://github.com/codeapprove/feedback/issues)
- [Author and users discussing permissions and partial-team adoption, March 2023](https://news.ycombinator.com/item?id=35326546)

## Critique-style tools, including CodeFlow and Crocodile

Praise: Google's account of Critique prioritises quick loading, keyboard navigation, clear review state and a simple review process. It links to search, editing and test tools rather than turning review into an entire IDE. CodeFlow users in an HN discussion value a comment panel that jumps directly to the relevant code revision. Crocodile's author describes selection comments, persistent discussions across revisions and floating conversations that do not break up the code.

Complaints and limits: Critique's internal Google environment is not evidence that its access assumptions suit a restricted multi-tenant forge. In Crocodile's launch discussion, users struggled to understand the iteration control and wanted demonstrations using realistic review problems. Powerful revision handling must be understandable on a first visit. Independent adoption evidence for Crocodile is limited.

Sources:

- [Google's engineering account of Critique](https://abseil.io/resources/swe-book/html/ch19.html)
- [Crocodile launch discussion, CodeFlow comparisons and demo criticism, June 2022](https://news.ycombinator.com/item?id=31841215)

## Stacked PR tools beyond Graphite

Praise: ghstack and spr map commits into smaller PRs. Git Town and git-machete help manage dependent branches. Users value continuing work, automated restacking and reviewable layers. A visible dependency graph matters as much as local rebase automation.

Complaints: users dislike installing a privileged app before they know whether the workflow helps, enforced one-commit-per-PR conventions, manual branch relationships and unfamiliar rewriting. A comparison maintained by another tool documents fork-workflow limitations in ghstack, but should not be treated as neutral or permanently current. Stacks also need a shared place for the overall design discussion.

Current comparison caution: GitHub now documents native stack interoperability with Jujutsu, Sapling and Git Town. Older claims that GitHub has no stack support are obsolete. Branchroom should compete on comprehensible review rounds and reliable dependencies rather than merely drawing a stack diagram.

Sources:

- [Users comparing stack tools and installation barriers, February 2024](https://www.reddit.com/r/github/comments/1ajq8ag/whats_your_preferred_tool_for_managing_stacked_prs/)
- [git-stack's comparison of alternatives and workflow limitations](https://github.com/gitext-rs/git-stack/blob/main/docs/comparison.md)
- [GitHub documentation for stack interoperability](https://docs.github.com/en/pull-requests/reference/use-other-tools-with-stacked-pull-requests)

## The 12 lessons that matter most for Branchroom

1. **Make review speed a release requirement.** Copy lightweight pages and focused loading. Avoid a full application shell blocking the first useful diff. Benchmark real large PRs, deep links, typing, file switching and long-lived tabs on modest hardware. Show a file list immediately, load diff content progressively and retain search across files. GitLab's [large-change issue](https://gitlab.com/gitlab-org/gitlab-ce/-/issues/3760) and [file-by-file design](https://gitlab.com/groups/gitlab-org/-/work_items/516), plus Bitbucket's [documented failures](https://support.atlassian.com/bitbucket-cloud/kb/percent22something-went-wrongpercent22-error-message-when-attempting-to-review-a-pull-request/), show why performance and review completeness must be designed together. These are proposed checks, not measured Branchroom results.

2. **Preserve review history across pushes and rebases.** Copy patchset comparisons and a clear option to see changes since the last review. Keep unresolved discussions reachable when their original line changes. Avoid equating an outdated code anchor with a resolved concern. [Gerrit users explain the benefit](https://news.ycombinator.com/item?id=35326546), while [Forgejo v16 documents placement fixes](https://forgejo.org/2026-07-release-v16-0/). A [historical Gitea bug](https://github.com/go-gitea/gitea/issues/22691) even made replies look unsuccessful after the line became outdated. It was closed with a fix in 2023. Check the inherited behaviour rather than assuming it remains broken.

3. **Say whose turn it is and what remains.** Copy Gerrit's attention set. Separate the author, requested reviewers, current action owner, viewed files, unresolved threads and merge requirements. Use plain states such as waiting for author and ready for another review. Avoid a generic green approval that conceals open work. [Gitea's closed re-request report](https://github.com/go-gitea/gitea/issues/37409) describes a button that reloaded without updating review state. [GitLab queue feedback](https://www.reddit.com/r/gitlab/comments/1ik9vea/gitlabs_new_merge_request_ui_what_is_the_expected/) shows how redesigns can break teams using assignees to pass responsibility.

4. **Explain effective access in one place.** For Branchroom, show the workspace, repository, role, source of each grant and the reason an action is blocked. Distinguish workspace administration from unrestricted instance access. Make access changes previewable and auditable. Avoid asking administrators to infer rights from several settings screens. This recommendation follows [Gitea's own access-system critique](https://github.com/go-gitea/gitea/issues/28366), [Azure users' scattered-permission complaint](https://www.reddit.com/r/azuredevops/comments/1lvo9gs) and [GitLab's move to explain unavailable visibility options](https://docs.gitlab.com/releases/17/gitlab-17-1-released/).

5. **Keep workspace and repository context visible and navigation stable.** Show a readable path and an explicit workspace switcher. Keep code, reviews, issues and settings in predictable places. Avoid hidden-hover essentials, shifting conceptual buckets and modals that break browser navigation. Preserve deep links and back-button behaviour. [GitLab's feedback summary](https://gitlab.com/gitlab-org/gitlab/-/issues/409005) documents context and administration discoverability problems. [Forgejo's v8 account](https://forgejo.org/2024-07-release-v8-0/) supports observing users before changing familiar controls.

6. **Never quietly omit part of a review.** Show counts for loaded, unloaded, filtered and reviewed files, with an obvious warning when content is truncated. Offer a way to load or download the complete change. Keep generated-file filtering explicit and reversible. Avoid treating a collapsed file as reviewed. The [Azure truncation complaint](https://www.reddit.com/r/azuredevops/comments/ud4mct/how_do_you_deal_with_displayed_content_is/) demonstrates the risk. [CodeApprove's stated design](https://codeapprove.com/) correctly treats hidden files and conversations as a review problem, though loading everything into the DOM is not the only solution.

7. **Keep context near the code without burying it.** Copy a persistent file tree, path filter, optional comment panel, line-range comments and links to definitions, blame and relevant test results. Keep large discussions from repeatedly splitting the diff. Avoid forcing reviewers to abandon their position for basic context. [OneDev's documented review flow](https://docs.onedev.io/tutorials/code/code-review) is a concrete reference. [Gitea's request for blame inside PR diffs](https://github.com/go-gitea/gitea/issues/36881) identifies an inherited context-switching cost.

8. **Make Actions setup and failure diagnosis ordinary UI tasks.** Show runner availability, job state, logs, artifacts and direct links to authorised variable and secret settings. Explain unsupported workflow syntax before a migration appears complete. Avoid promising total GitHub compatibility. [Gitea's comparison](https://docs.gitea.com/next/usage/actions/comparison) lists differences including ignored environment syntax and unavailable capabilities. [A migration account](https://unterwaditzer.net/2025/codeberg.html) shows CI can dominate the move even when issue import is easy. Distinguish a missing runner from a failed job and a policy denial.

9. **Keep expert workflows optional and interoperable.** Copy native dependency information, layer-only diffs, safe restacking support and a place to explain the whole stack. Let ordinary branch PRs remain straightforward. Avoid requiring every collaborator to install a proprietary CLI or use the same overlay. [Graphite users describe partial-adoption friction](https://news.ycombinator.com/item?id=39478904). [Gerrit-oriented reviewers also miss whole-stack discussion](https://news.ycombinator.com/item?id=41506080). A stack graph alone does not solve review continuity.

10. **Design onboarding around a completed task.** A non-expert should be able to accept an invitation, identify the permitted workspace, open a review and leave useful feedback without learning the whole product. Preserve import identifiers and authorship. Show migration progress and recoverable errors. Avoid making another account, SSH setup or an unfamiliar mail workflow prerequisites for every contribution. [Codeberg's migration documentation](https://docs.codeberg.org/advanced/migrating-repos/) supplies a concrete recovery failure. [Sourcehut's user discussion](https://news.ycombinator.com/item?id=25101359) shows the cost of expecting newcomers to learn a contributor culture before participating.

11. **Show consequential action feedback and preserve drafts.** A submitted comment needs an unmistakable saved state. A denied action needs a reason. Prevent duplicate submissions, retain drafts after recoverable failure and warn before losing edits. For Branchroom's access changes, show the affected scope before confirmation. [The historical Gitea duplicate-reply bug](https://github.com/go-gitea/gitea/issues/22691) illustrates why silence causes repeated actions. [CodeApprove's tracker](https://github.com/codeapprove/feedback/issues) includes requests concerning unsaved edits and confusing reviewer state. These are behavioural requirements, not reasons to add more confirmation dialogs everywhere.

12. **Keep enterprise power out of the everyday path, while making audit useful.** Give administrators dedicated views for membership, policies, runner trust and access history. Let ordinary users focus on their repositories and next action. Audit entries should connect an actor, target, action, result and relevant access change, with visibility constrained by workspace policy. Copy integrated governance, avoid suite-wide clutter and third-party access surprises. [Enterprise GitLab reviewers](https://www.g2.com/products/gitlab/reviews?page=4) value governance while criticising complexity. [Critique's design account](https://abseil.io/resources/swe-book/html/ch19.html) supports keeping review focused and linking other tools. Its broad internal visibility is unsuitable as a default for Branchroom's restricted workspaces.
