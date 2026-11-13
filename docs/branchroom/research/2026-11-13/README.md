# Research, 13 November 2026

Everything the review, research and design lanes wrote on 13 November. These reports were only in temporary scratch folders, so they are kept here. Screenshots and raw measurement files were not kept.

## Where to start

- [gaps-ranked.md](gaps-ranked.md): eight agent-native gaps ranked by what to build first. None of them is built yet.
- [mr-performance.md](mr-performance.md): why big diffs are slow, with measurements and a ranked plan. Only lazy loading of large pull diffs has shipped (`3ad24900f2`). HTML compression is still off in live.
- [mr-github.md](mr-github.md) and [mr-alternatives.md](mr-alternatives.md): what people complain about on GitHub, and on GitLab, Gitea and Forgejo.

## Research

| File | What it covers |
| --- | --- |
| [research-ai.md](research-ai.md) | AI agents and version control |
| [research-enterprise.md](research-enterprise.md) | Enterprise version control pain |
| [research-parity.md](research-parity.md) | GitHub parity gaps |
| [mr-github.md](mr-github.md) | Fifteen ranked GitHub interface complaints |
| [mr-alternatives.md](mr-alternatives.md) | Twelve lessons from GitLab, Gitea, Forgejo and others |
| [mr-performance.md](mr-performance.md) | Large diff performance and a speed budget |
| [gaps-ranked.md](gaps-ranked.md) | Agent-native gaps, ranked |

## Think-alouds and reviews

| File | What it covers |
| --- | --- |
| [thinkaloud-agent.md](thinkaloud-agent.md), [thinkaloud2-agent.md](thinkaloud2-agent.md) | An AI agent operator using Branchroom, two rounds |
| [thinkaloud-lead.md](thinkaloud-lead.md), [thinkaloud2-lead.md](thinkaloud2-lead.md) | A security lead, two rounds |
| [thinkaloud-solo.md](thinkaloud-solo.md) | A solo developer leaving GitHub |
| [haiku-nav.md](haiku-nav.md), [haiku-home.md](haiku-home.md), [haiku-releases.md](haiku-releases.md) | Navigation and pull requests, repo home, releases |
| [fable-thinkaloud.md](fable-thinkaloud.md), [sol-thinkaloud.md](sol-thinkaloud.md) | GitHub and Branchroom side by side |
| [haiku-design-review.md](haiku-design-review.md) | Review of design sample D before it became the default theme |
| [review-fable.md](review-fable.md) | Review of suggested changes and the audit viewer |
| [review-tokens.md](review-tokens.md), [review-tokens-2.md](review-tokens-2.md) | Security review and re-review of token restrictions |

The fixes from the think-alouds, the token reviews, the design review and the releases and repo home passes are on `main`.
