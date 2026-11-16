# Moving a repository from GitHub

**New migration → GitHub** copies a repository with its history, wiki, LFS files, labels, milestones, issues, pull requests, reviews, comments and releases. Tick **Settings** to copy how the repository is run as well.

Settings need an access token. Use one from an admin of the GitHub repository, because GitHub only shows branch protection, webhooks and teams to admins. A classic token needs the `repo` scope. A fine-grained token needs read access to administration, actions, webhooks, secrets and variables.

## What is copied

| On GitHub | Here |
| --- | --- |
| Branch protection rules | Branch protection rules with the same pattern |
| Direct collaborators | Collaborators, for people whose account here is linked to their GitHub account |
| Teams with access | The team with the same name, if the new owner is an organisation that has one |
| Webhooks | Webhooks, switched off |
| Actions variables | Actions variables |

Access never grows in the move. A person is only added when they have signed in here with GitHub, because the same username can belong to someone else. A team is only added when it gives no more access here than it had on GitHub. GitHub's maintain role becomes write and triage becomes read.

Webhooks arrive switched off for two reasons. GitHub does not give back a webhook's secret, and GitHub may still be sending the same events. Enter the secret again and activate each webhook when you are ready.

Required status checks from GitHub Actions are renamed to match how checks are named here, which is `workflow / job (event)`.

## What is left for you

After the migration, the repository home and its settings link to a **Migration report**. It lists everything that needs a person:

- Secrets. Their values can not be read, so the report lists the names to enter again, including organisation secrets the repository uses.
- Organisation variables the repository uses.
- Environments. There are none here, so their secrets, variables and approval rules are not copied.
- People with no linked account, and teams with no match.
- Branch protection settings with no match, such as linear history.
- Rulesets. Only branch protection rules are copied.
- Anything the token was not allowed to read.

Dismiss the report when you are done.

## From the API

Set `"settings": true` on `POST /api/v1/repos/migrate` with `"service": "github"` and an `auth_token`.
