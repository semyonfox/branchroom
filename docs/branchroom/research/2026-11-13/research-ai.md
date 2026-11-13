# AI agents and version control, 2025 to 2026

Research snapshot: 13 November 2026. Read-only research for Branchroom.

A forge should treat an agent as a delegated identity with a bounded task. Repository membership alone is too broad. The dangerous combination is untrusted project text, access to confidential source, and a tool that can publish it. Faster code generation also leaves maintainers with more changes than they can review. Buying another model does not fix either problem.

## Scope and evidence limits

I read all four `docs/branchroom/*.md` guides. The operations guide and November review describe the latest supported deployment. Older sections of the implementation plan and demo still give the earlier 1 MiB and 45 second limits. Operations now says 32 MiB and two minutes by default, with a separate ten-minute upload-pack limit. This report uses the current operations contract.

Branchroom already separates restricted Git objects physically, reauthorises Git HTTP requests, restricts pushes to fast-forward `refs/heads/branchroom/*`, rotates clone locators, and retains hash-chained audit history. It does not yet provide task credentials, restricted PRs, restricted CI, an audit viewer, MCP access, or publication. Its internal one-use receive session is not a credential that an external agent can request. The external client still uses an individual user's password or PAT. Bots and Actions identities are refused by this transport.

The security examples below are disclosed research demonstrations unless explicitly described otherwise. They establish feasible failure modes, not measured exploitation rates. Frequency means how often an exposed team is likely to encounter the problem or its prerequisites. It is a judgement from the cited evidence, not an industry incident statistic. The ranking prioritises irreversible disclosure and authority misuse, then operational burden. Several problems share a cause but need different controls.

S means a bounded change to an existing policy or UI. M means a new service and several enforcement points. L means a new workflow spanning storage, permissions and execution. These are relative effort estimates, not delivery promises.

Vendor documentation is rolling. Read the comparisons as what the vendors document at research time, not proof that every deployment has it. In particular, Gitea Enterprise's Agents page says v27 and later and requires an extra purchase. It is not evidence that this community fork already includes those controls. I have not independently established that feature's release date. I excluded future GitLab 19.6 sandbox requirements from the recommendations' competitive baseline.

## Shared platform references

These references support the platform comparisons across the ten entries. Each entry also gives one to three direct evidence URLs.

- H1, [GitHub cloud-agent risks and mitigations](https://docs.github.com/en/copilot/concepts/security-governance-and-network-settings/risks-and-mitigations). Current documentation uses 'cloud agent' for what was called 'coding agent'.
- H2, [GitHub MCP configuration](https://github.com/github/github-mcp-server/blob/main/docs/server-configuration.md).
- L1, [GitLab agent security threats and controls by execution mode](https://docs.gitlab.com/user/duo_agent_platform/security_threats/).
- L2, [GitLab composite identity](https://docs.gitlab.com/user/duo_agent_platform/composite_identity/).
- G1, [Gitea community API authentication and token scopes](https://docs.gitea.com/development/api-usage/).
- G2, [Gitea Enterprise agents](https://docs.gitea.com/enterprise/features/agents/).
- G3, [Gitea Enterprise fine-grained tokens](https://docs.gitea.com/enterprise/features/fine-grained-access-tokens/).

## Ranked problems

| Rank | Problem | Severity | Frequency | Proposed effort | Natural restricted-workspace fit |
| --- | --- | --- | --- | --- | --- |
| 1 | Project text becomes instructions with real authority | Critical | High exposure, unknown exploit rate | M | Yes |
| 2 | Agents inherit reusable, overbroad credentials | Critical | High configuration risk | M | Yes |
| 3 | Human read access silently becomes permission to send code to a model | Critical | High policy exposure | M | Yes |
| 4 | Restricted work escapes through a public PR or release | Critical | Medium, especially embargo work | L | Yes |
| 5 | A branch or worktree is mistaken for an execution sandbox | Critical | Medium, topology dependent | L | Yes |
| 6 | PR and security-report volume exceeds review capacity | High | Very high for popular public projects | M | Partly |
| 7 | Plausible generated patches contain defects or credentials | High | High | M | Partly |
| 8 | MCP exposes a second, inconsistent permission path | High | Medium and growing | M | Yes |
| 9 | Commit metadata cannot establish who authorised an agent | High | High attribution ambiguity | M | Yes |
| 10 | AI review is mistaken for independent, complete approval | High | High where enabled | S initially | Yes |

### 1. Project text becomes instructions with real authority

An issue, PR description, review comment, README, source comment or agent instruction file can influence tool use. The attacker does not need to compromise the MCP server. Invariant's 26 May 2025 demonstration put instructions in a public issue. An agent with access to private repositories then copied private information into a public PR. Legit Security's 20 May 2025 disclosure demonstrated GitLab Duo prompt injection and private-source exfiltration through rendered HTML. Legit reports that GitLab patched the issue. Neither report establishes widespread production compromise.

Current handling: GitHub limits ordinary cloud-agent triggers to writers, withholds non-writer comments and filters hidden HTML comments, per H1. GitLab documents sanitisation, injection detection and tool restrictions, per L1. Gitea Enterprise has trigger access control, per G2. None of these makes repository prose a trustworthy authority source. Community Gitea's normal issue permissions are not an agent execution policy.

Branchroom idea, M: a task-authorisation record fixed before reading untrusted content. It names allowed reads, write targets and tools. Put repository text in clearly marked data fields, load executable agent configuration from an owner-approved revision, and require separate approval to change permissions. Treat detection as additional filtering. A malicious README must never enlarge the task's authority. Natural fit: yes, extend the existing evaluator rather than trusting instructions in the clone.

Sources:
- https://invariantlabs.ai/blog/mcp-github-vulnerability
- https://www.legitsecurity.com/blog/remote-prompt-injection-in-gitlab-duo
- https://docs.github.com/en/copilot/concepts/security-governance-and-network-settings/risks-and-mitigations

### 2. Agents inherit reusable, overbroad credentials

A developer's PAT often outlives the task and reaches unrelated repositories. A stolen token, confused agent or reused configuration can then act beyond the intended fix. Expiry alone is insufficient if a token can still read every private project during its lifetime.

Current handling: GitHub App installation tokens expire after one hour and can narrow repositories and permissions. Built-in agent restrictions in H1 narrow write behaviour further. GitLab uses composite identities for runner flows, with the intersection of human and service-account permissions. Its authentication guide lists a two-hour OAuth token. Gitea community has API category scopes, per G1. Enterprise documents task-lifetime agent tokens bounded by bot, initiator and token scope, per G2. Enterprise fine-grained PATs also select repositories, per G3. A days-long PAT still differs from a single-task lease.

Branchroom idea, M: issue a hash-stored opaque task credential for one workspace, an exact server-assigned ref, allowed operations and an expiry such as 30 minutes. Record both agent and sponsoring user. Recheck live grants on every request, revoke on cancellation, and constrain renewal to the original grant. Keep ordinary user permission as a ceiling. Do not reuse the clone locator as a bearer credential. Natural fit: yes, this is the most useful next identity slice. It needs a new authenticated agent path, not removal of today's bot rejection.

Sources:
- https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app
- https://docs.gitlab.com/user/duo_agent_platform/authentication/
- https://docs.gitea.com/enterprise/features/agents/#credentials

### 3. Human read access silently becomes permission to send code to a model

An owner may permit a contractor to clone confidential code while forbidding a hosted model from receiving it. Those are different decisions. Once plaintext reaches a developer machine, a forge cannot reliably prevent copying it into another client. A repository instruction saying 'do not use AI' is a policy notice, not enforcement.

Current handling: GitHub documents content exclusion but explicitly says it is not a filesystem boundary. Indirect access and third-party MCP clients can bypass its guarantees. GitLab documents project denylists and mode-dependent file restrictions, per L1. Its review documentation explicitly lists MR text, filenames, diffs and file contents as model inputs. Gitea's ordinary access model is about users and repository units. Enterprise agents intersect identities but related-repository access can still exceed one task, per G2.

Branchroom idea, M: require an owner policy for agent reads, with `agent_read=deny` by default for restricted workspaces. An allow names an approved executor and model-service profile, exact repositories and scopes, and expiry. Enforce before clone, search, raw-file, MCP and review-context delivery. Record the approved destination without logging source. A self-declared provider string is not enforcement. Strong guarantees require a controlled executor and outbound gateway. Human clients remain a stated trust limit. Natural fit: yes, add an agent-specific permission separate from `read_source`.

Sources:
- https://docs.github.com/en/copilot/concepts/security-governance-and-network-settings/content-exclusion
- https://docs.gitlab.com/user/gitlab_duo/code_review/
- https://docs.gitlab.com/user/duo_agent_platform/security_threats/

### 4. Restricted work escapes through a public PR or release

The public forge API itself can be an exfiltration channel. An outbound firewall allowing the forge domain will not prevent an agent from putting private code in a public comment or PR. An embargo also covers branch names, earlier failed patches, review discussion, logs and attachments.

Current handling: GitHub has temporary private security-advisory forks. Its documentation says integrations, including CI, cannot access those forks. This is useful precedent and an integration constraint, not evidence that ordinary Copilot tasks handle embargoes. GitLab documents public-output exfiltration risks, per L1. Separate private projects can hold confidential collaboration, but protected branches do not create a separate reader population inside a clone. Community Gitea similarly offers repository privacy. Branchroom already provides a separate store, but has no publication workflow.

Branchroom idea, L: restricted review followed by an explicit publication action. Freeze private source SHA, public target SHA and candidate patch digest. A release approver sees the resulting public diff and metadata. A dedicated publisher applies only that approved patch. Private commits, comments and logs stay behind. A restricted task credential must have no public comment, PR or push capability. With a controlled executor, restrict additional egress too. A copied patch from a human workstation is outside this guarantee. Natural fit: yes, this is the reason to have isolated stores.

Sources:
- https://docs.github.com/en/code-security/tutorials/fix-reported-vulnerabilities/collaborate-in-a-fork
- https://invariantlabs.ai/blog/mcp-github-vulnerability
- https://docs.gitlab.com/user/duo_agent_platform/security_threats/

### 5. A branch or worktree is mistaken for an execution sandbox

A worktree avoids edit conflicts. It does not isolate the filesystem, credential helper, network, Docker socket or sibling checkouts. Repository setup scripts and tests are executable inputs too. Running restricted jobs as the forge service account defeats the separate-store boundary.

Current handling: GitHub documented default internet restrictions for coding agent in July 2025 and now offers configurable firewall settings. GitLab documents remote filesystem and network sandboxing, with applicability depending on execution mode and runner configuration. Gitea Enterprise uses Actions runners, per G2. Its presence does not prove a particular runner topology is safe. Branchroom operations explicitly prohibits same-host or same-account Actions runners for restricted source.

Branchroom idea, L: support one disposable executor profile with one checkout, no host mounts or Docker socket, fresh credentials and caches, and denied outbound traffic except approved model, forge and dependency endpoints. Separate tasks and security scopes at the worker boundary. Teardown revokes the task and destroys the worker. Bind the task to its exact branch as well as the workspace. Do not claim that today's `branchroom/*` wildcard isolates different agents from each other. Natural fit: yes, but this is a runner project, not a small Git hook change.

Sources:
- https://github.blog/changelog/2025-07-16-configure-internet-access-for-copilot-coding-agent/
- https://docs.github.com/en/copilot/how-tos/copilot-on-github/customize-copilot/customize-the-firewall
- https://docs.gitlab.com/user/duo_agent_platform/environment_sandbox/

### 6. PR and security-report volume exceeds review capacity

Cheap submission does not make verification cheap. curl shut its bounty programme on 1 February 2026 after intensive junk reports. Daniel Stenberg's April follow-up says the junk problem had subsided, but higher-quality report volume was then higher than ever. That distinction matters. This is not only a spam problem. Good agent-generated work also consumes maintainer time.

Current handling: GitHub introduced collaborator-only PR creation in February 2026 and concurrent open-PR limits in June. Its October-updated explanation says agent PRs count towards the user's limit, while drafts do not. GitLab offers MR permissions and approval processes. I did not verify a comparable agent-specific intake cap there. Community Gitea can disable the PR unit and control collaborators. Enterprise documents run and conversation-round limits, per G2, which address automation loops rather than external contribution volume.

Branchroom idea, M: an intake queue with one accountable sponsor, configurable outstanding-change limits per sponsor and agent, and a maintainer acceptance step before reviewer notifications or expensive CI. Count drafts towards resource budgets. Ask for reproduction or validation evidence without letting a model grade its own evidence. Bound repeated runs on an unchanged SHA. Natural fit: partly, restricted workspaces can stage proposals quietly, but a general PR-intake control benefits ordinary repositories too.

Sources:
- https://daniel.haxx.se/blog/2026/04/22/high-quality-chaos/
- https://github.blog/changelog/2026-02-13-new-repository-settings-for-configuring-pull-request-access/
- https://github.blog/open-source/maintainers/how-pull-request-limits-are-cutting-down-the-noise/

### 7. Plausible generated patches contain defects or credentials

Fluent explanations are poor evidence of correctness. GitGuardian's 2026 analysis reports secrets in 3.2% of Claude Code-assisted public commits against a 1.5% overall baseline. This is an observed association in their dataset. It does not establish causation, cover all agent tools or measure private repositories.

Current handling: GitHub's cloud agent documents CodeQL, dependency and secret checks, per H1. GitLab has security scanning and review products. Agent-flow variables also exclude ordinary custom CI/CD variables by default, reducing ambient secret exposure. Community Gitea can run external scanners through Actions and enforce required checks. Those integrations need configuration. Do not describe the removed Branchroom preflight scanner as a live feature.

Branchroom idea, M: require a compact validation record attached to an exact candidate SHA. Include the executed command, exit status and trusted runner identity. Add secret scanning at public publication and require selected status checks. Only ingest structured summaries from a restricted runner, with logs retaining their scope. Scanner findings may block release, but passing checks must not assert that code is safe. Natural fit: partly, ordinary repos need this too, and restricted publication gives one valuable final gate.

Sources:
- https://blog.gitguardian.com/agentic-ai-security-blast-radius/
- https://docs.github.com/en/copilot/concepts/security-governance-and-network-settings/risks-and-mitigations
- https://docs.gitlab.com/user/duo_agent_platform/flows/execution_variables/

### 8. MCP exposes a second, inconsistent permission path

A read-only tool can still disclose confidential code to the model. A separate write tool can publish it. Tool filtering does not itself narrow the repositories reachable through the underlying token. MCP also adds dependencies and configuration that need their own review.

Current handling: GitHub's official MCP server supports tool allowlists, read-only mode and lockdown filtering. Its docs explicitly call lockdown best effort, not a security boundary. GitLab supports tool restrictions, per L1. Gitea's upstream issue confirms its official standalone MCP server exists at `gitea.com/gitea/gitea-mcp`. Direct fetching was robots-blocked, so I did not verify its current credential or repository-filter guarantees. Enterprise separately documents session-authorised built-in MCP, per G2. These are distinct products. Do not confuse community wrappers with the official server.

Branchroom idea, M: a narrow native MCP endpoint using the same task credential and evaluator as restricted Git. Start with workspace metadata, bounded tree and file reads, and proposal status. All resources, search results and errors must obey discovery policy. Add writes only as separate capabilities. Never place an owner PAT behind a shared MCP proxy. Make cross-repository reads require explicit task grants. Natural fit: yes, after task identity exists. Reusing the policy layer is more valuable than advertising hundreds of tools.

Sources:
- https://github.com/github/github-mcp-server/blob/main/docs/server-configuration.md
- https://github.com/go-gitea/gitea/issues/35506
- https://docs.gitea.com/enterprise/features/agents/#built-in-gitea-mcp-server

### 9. Commit metadata cannot establish who authorised an agent

Git author strings and voluntary trailers can be forged or removed. A signature proves control of a signing identity, not that a human understood the patch or that a particular model produced it. Separate accountability from public credit and avoid claiming reliable AI detection from commit text.

Current handling: GitHub documents signed Copilot commits, human co-authorship and session links, per H1. GitLab makes the initiating human the MR author for separation of duties, while runner commits carry service-account authorship. Composite identity does not cover every mode. Scheduled flows are a documented exception. Gitea community supports signed commits and ordinary account identity. Enterprise binds agents to bot accounts and sessions, per G2.

Branchroom idea, M: append a server-observed receipt binding task, sponsor, authenticated agent principal, grant, accepted commit/ref transition and decision ID. Model/version is declared metadata unless an executor can attest it. Show the receipt to authorised reviewers and retain it after repository deletion. Keep public attribution configurable and off unless the repository opts in. Do not require trailers as an authorisation mechanism. Natural fit: yes, extends the existing audit chain. The chain detects changes relative to retained evidence, but cannot prove model honesty or resist a trusted database operator rewriting all history without an external anchor.

Sources:
- https://docs.github.com/en/copilot/concepts/security-governance-and-network-settings/risks-and-mitigations
- https://docs.gitlab.com/user/duo_agent_platform/composite_identity/
- https://docs.gitlab.com/user/duo_agent_platform/flows/execution_variables/

### 10. AI review is mistaken for independent, complete approval

An AI reviewer can miss a vulnerability, invent a finding or follow instructions planted in the patch. A second model reading the same manipulated inputs is not automatically independent. 'No issues found' also says little if large files or parts of a diff were skipped.

Current handling: GitHub recommends human review. Its current rolling docs say AI approvals do not count by default, but can satisfy approval rules when explicitly enabled. They also say review instructions and skills are read from the PR head. This differs from older claims that Copilot can never approve. GitLab distinguishes agentic Code Review Flow from non-agentic Duo review. The latter documents context exclusions and large-MR behaviour. Gitea community has ordinary reviews and required approvals. Enterprise includes review agents, per G2. Verify those agents' approval semantics before treating them as a human gate.

Branchroom idea, S initially: classify agent review separately from required human approval, and forbid the task sponsor or task identity from approving its own change. Bind every review to the reviewed SHA and invalidate it on changes. Show unreviewed files and review failures explicitly. Load mandatory review policy from a trusted base revision. Later, M: add a reviewer task with read-only source access and comment-only output in the same scope. Natural fit: yes, especially before publication. Prefer enforcing independent approval over building another reviewer model.

Sources:
- https://docs.github.com/en/copilot/concepts/agent-capabilities/agent-powered-workflows-and-operations/code-review
- https://docs.github.com/en/copilot/tutorials/roll-out-at-scale/drive-downstream-impact/accelerate-pull-requests
- https://docs.gitlab.com/user/gitlab_duo/code_review/

## Top three things a small self-hosted forge should ship

1. **A task pass with a real read boundary, M.** One agent, accountable sponsor, exact workspace and ref, expiry and live revocation. No public write permission and no unrelated repository enumeration. Owner-approved executor/model destinations are part of the task policy. Start with Git HTTP and a small read-only MCP endpoint. Demonstrate that an injected issue cannot cause a cross-repository read. This combines problems 1, 2, 3 and 8. It exceeds a generic PAT workflow without pretending to control arbitrary developer machines.

2. **An audit viewer with per-task receipts and a stop control, M.** Show which authenticated identity read which workspace, which refs changed, the sponsoring user, grant and decision, and whether the audit chain verifies. Stop revokes new requests immediately, with the documented already-claimed receive exception made visible. Never store prompts or source bodies by default. Support private export of the evidence and configurable public attribution. This makes existing Branchroom machinery useful during an incident and supports problem 9 without building a model platform.

3. **A deliberately narrow publication gate, M for owner-driven Git-only publication, L for full restricted collaboration.** Freeze both SHAs, preview an exact patch and public metadata, check it, and require a separate human approver. Publish through a dedicated identity that restricted agents never receive. Keep earlier attempts and private discussion out of public history. Defer restricted PRs and CI until their secondary stores are genuinely scoped. This addresses problem 4 and enforces the useful part of problem 10.

The competitive claim should be the combination of enforced source scopes, accountable short-lived tasks and deliberate disclosure on a small self-hosted instance. GitHub, GitLab and Gitea Enterprise already document many individual agent features. Calling Branchroom the first forge with agent credentials, MCP or AI review would be false. An executable demonstration of all three boundaries together is a more defensible claim.
