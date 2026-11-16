# GitHub Actions workflows on Branchroom

Branchroom runs workflows with Gitea Actions and reads `.github/workflows` as it is. Most CI workflows run without edits once a runner and the secrets are in place. This page lists what does not.

It was checked on 16 November 2026 by putting a typical CI workflow through Branchroom's workflow parser and trigger matching. The workflow had push, pull request, schedule, manual and merge queue triggers, a matrix test job with a service container, a deploy job with an environment and a reusable release workflow. No job was run on a runner, so the rows marked "runner" follow from how runners are set up and were not tested.

## What carries over

- The workflow files. If a repository also has `.gitea/workflows`, only that folder is read.
- `push`, `pull_request`, `pull_request_target`, `issues`, `issue_comment`, `release`, `schedule`, `workflow_dispatch` with inputs, `workflow_run`, `create`, `delete`, `fork`, `gollum`, `registry_package` and the pull request review triggers, with their branch, tag and path filters.
- Matrices, `needs`, `if`, `concurrency` and `permissions`.
- Reusable workflows in the same repository, including `secrets: inherit`.
- `secrets.GITHUB_TOKEN`, the `github` context and `vars`.
- OIDC tokens for jobs with `id-token: write`.

## What breaks

| In the workflow | What happens here | What to do |
| --- | --- | --- |
| `runs-on: ubuntu-latest` | There are no hosted runners. The job waits until a runner with that label is online. | Register a runner that offers the labels your workflows use. |
| `secrets.NAME` | Empty until the secret is entered again. Secret values can not be copied from GitHub. | Enter each secret in the repository or organisation settings. |
| `environment: production` | Accepted and ignored. There are no environments, so environment secrets and variables are missing and no reviewer has to approve. A deploy job that waited for approval on GitHub runs straight away. | Move the secrets to the repository. Guard deploys another way, such as a manual `workflow_dispatch` or a protected branch. |
| `on: merge_group` | Never fires. There is no merge queue. | Rely on the `pull_request` run and the "branch must be up to date" rule. |
| `on:` `deployment`, `deployment_status`, `check_run`, `check_suite`, `status`, `repository_dispatch`, `label`, `milestone`, `discussion`, `discussion_comment`, `page_build`, `public`, `watch`, `branch_protection_rule` | Never fire, and nothing warns you. A workflow with only these triggers never runs. | Trigger from `push`, `workflow_dispatch` or `workflow_run`. |
| `pull_request` with `types:` `ready_for_review`, `converted_to_draft`, `locked`, `unlocked`, `auto_merge_enabled`, `auto_merge_disabled`, `enqueued`, `dequeued` | Those types never fire. The other types work. | Drop them, or use `opened`, `synchronize` and `reopened`. |
| `release` with `types:` `created`, `released`, `prereleased`, `unpublished`, `deleted` | Those types never fire. | Use `published` or `edited`. |
| `uses: org/shared/.github/workflows/x.yml@v1` | A reusable workflow in another repository is looked up on this instance, not on GitHub. The job fails if that repository is not here. | Migrate the shared repository under the same owner and name. |
| `uses: actions/checkout@v4` and other actions | Downloaded from github.com by the runner. Private actions on GitHub can not be fetched. | Give runners access to github.com. Migrate private actions and reference them by full URL. |
| Steps that call the GitHub API: `gh`, `actions/github-script`, actions that comment, label, release or upload code scanning results | `GITHUB_TOKEN` is a Branchroom token and `GITHUB_API_URL` points at Branchroom's API, which is modelled on GitHub's but is not the same. There is no GraphQL API. Expect these steps to fail. | Replace them with calls to the Branchroom API. |
| Pushing to `ghcr.io` or GitHub Packages with `GITHUB_TOKEN` | Refused. GitHub does not know the token. | Use a GitHub token stored as a secret, or the package registry here. |
| Cloud logins through OIDC | The token issuer is `https://your-branchroom.example/api/actions/oidc`, not GitHub's. Trust policies written for GitHub reject it. | Add the Branchroom issuer to the trust policy. |
| Required status checks | Checks are named `workflow / job (event)`, such as `CI / test (24) (pull_request)`, not after the job alone. | Pick the checks again in the branch protection rule after the first run. |
| Dependabot, code scanning, Pages | None of these exist here, so their workflows and config files do nothing. | Use Renovate for updates. Host pages and scanning elsewhere. |
| Caches, artifacts and service containers (runner) | They depend on how the runner is set up. A runner needs a container engine for service containers and a cache server for `actions/cache`. | Check each on your runner before relying on it. |

## While GitHub is still primary

A repository that mirrors GitHub is created with Actions switched off, so nothing runs here during a trial. To try your workflows, switch Actions on under Actions in the repository settings. They then run on every sync, and scheduled workflows run on both sides. Without secrets most deploy and publish jobs fail harmlessly, but check for jobs that need none.
