# Branchroom

Branchroom is an MIT-licensed fork of [Gitea](https://about.gitea.com/). It is the Git host for this work, including the work agents do.

The fork tracks upstream Gitea and keeps its own changes small enough to merge. Those changes stay here. Nothing in this repository is proposed upstream.

## Direction

A normal day should stay on Branchroom. Repositories, issues, pull requests, reviews, releases, notifications, Actions, and Git over HTTP and SSH come from Gitea. Branchroom adds the gaps that still send people back to GitHub, and the API an agent needs so it can work without someone clicking through the site.

Shipped on top of current Gitea:

- Pull request lists filter by check status and review status, on the repository and on the dashboard.
- `GET /repos/{owner}/{repo}/pulls/{index}/readiness` returns the head commit, the review decision, the check state and the blockers in one response. The fingerprint is an ETag, so polling it is cheap when nothing changed.
- The notification inbox can mark an item done, save it, and filter by why it was sent. The API uses GitHub's `reason` field, and `DELETE /notifications/threads/{id}` marks a thread done.
- Saved replies, from a settings page and from the comment editor.
- Push protection rejects a push whose new commits add a recognisable token or a private key. It stays off until `[repository.secret_scanning] PUSH_PROTECTION` is true. Someone who has checked a match and knows it is not a secret can push that once with `git push -o secret_scanning.allow=true`.
- Access tokens can expire and can be limited to selected repositories.
- Review comments can suggest a change, including a change that spans lines.
- Release assets download from the repository home, including `/releases/latest/download/<file>`.
- Agents can use the [native MCP endpoint](docs/branchroom/mcp.md) to work on pull requests. [Agent tokens and push receipts](docs/branchroom/operations.md#agent-tokens-and-push-receipts) identify the agent behind a push.

## Restricted workspaces

Some source has to live outside the ordinary repository. With `[branchroom] ENABLED = true`, Branchroom keeps that source in a separate Git store, with grants, a hash-linked audit trail and a narrow HTTPS clone and push path.

That path is a real boundary, and it is unfinished. There is no SSH, LFS, Actions or restricted pull request for it. Read [the operations runbook](docs/branchroom/operations.md) before pointing it at anything you cannot lose. The threat model and the milestones are in [the implementation plan](docs/branchroom/implementation-plan.md).

## Try Branchroom

1. Build a Linux binary with embedded assets using `TAGS=bindata make build`. See the [build requirements](docs/build-setup.md) and [supported deployment](docs/branchroom/operations.md#supported-deployment).
2. Follow the [installation steps](docs/branchroom/operations.md#install) on a disposable instance. Create a throwaway repository and a second account for your teammate.
3. Open **Repository settings → Branchroom**, create a scope and provision its workspace. Give the teammate repository code-read permission, discovery and source-read access.
4. As the teammate, open `/{owner}/{repository}/-/branchroom`, copy the scoped URL and run `git clone <copied-url>`. The [demo walkthrough](docs/branchroom/self-hosted-demo.md#walkthrough) also covers granting and revoking push access.

Use the [backup and restore instructions](docs/branchroom/operations.md#backup) to include the restricted store in a backup.

## Development

Branchroom uses Gitea's local workflow:

- [Setup and requirements](docs/build-setup.md)
- [Development, formatting, and API changes](docs/development.md)
- [Testing](docs/testing.md)

```bash
make build
make fmt
make lint-md
```

`make help` lists the rest. A self-contained binary, the kind the live host runs, needs `TAGS=bindata make build`.

## Credits

Branchroom is built on the work of [the Gitea authors](https://github.com/go-gitea/gitea/graphs/contributors) and, before them, [the Gogs authors](https://github.com/gogs/gogs/graphs/contributors). Most of the code here is theirs. If Branchroom is useful to you, [support Gitea](https://opencollective.com/gitea).

Branchroom is not part of the Gitea project and Gitea does not endorse it. Report Branchroom bugs in [this repository's issues](https://github.com/semyonfox/branchroom/issues), not to Gitea. `CONTRIBUTING.md` and `MAINTAINERS` stay as upstream wrote them so merges stay clean. The Gitea maintainers they list do not maintain this fork.

Coding agents write much of the Branchroom-specific code. Each of those commits names the agent and model in an `Assisted-by` trailer. Semyon Fox reviews and owns every change.

## Licence

MIT, the same as Gitea. See [LICENSE](LICENSE).
