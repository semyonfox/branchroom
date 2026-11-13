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

Next is an MCP endpoint, an identity on each agent token, and a record of which agent pushed a commit.

## Restricted workspaces

Some source has to live outside the ordinary repository. With `[branchroom] ENABLED = true`, Branchroom keeps that source in a separate Git store, with grants, a hash-linked audit trail and a narrow HTTPS clone and push path.

That path is a real boundary, and it is unfinished. There is no SSH, LFS, Actions or restricted pull request for it. Read [the operations runbook](docs/branchroom/operations.md) before pointing it at anything you cannot lose. The threat model and the milestones are in [the implementation plan](docs/branchroom/implementation-plan.md).

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

## Licence

Branchroom inherits Gitea code under the MIT License. See [LICENSE](LICENSE).
