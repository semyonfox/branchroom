# Moving a repository from another forge

**New migration** copies a repository with its history, wiki, LFS files, labels, milestones, issues, pull requests, reviews, comments and releases. Tick **Settings** to copy how the repository is run as well. Settings can be copied from GitHub, Gitea, Forgejo and Codeberg.

Settings need an access token from an admin of the source repository, because forges only show branch protection, webhooks and teams to admins.

## What is copied

| At the source | Here |
| --- | --- |
| Branch protection rules | Branch protection rules with the same pattern |
| Direct collaborators | Collaborators, for people whose account here is linked to their account at the source |
| Teams with access | The team with the same name, if the new owner is an organisation that has one |
| Webhooks | Webhooks, switched off |
| Actions variables | Actions variables |

Access never grows in the move. A person is only added when they have signed in here with their account on that same server, because the same username or id can belong to someone else. A team is only added when it gives no more access here than it had at the source.

Webhooks arrive switched off for two reasons. No forge gives back a webhook's secret, and the source may still be sending the same events. Enter the secret again and activate each webhook when you are ready.

## What is left for you

After the migration, the repository home and its settings link to a **Migration report**. It lists everything that needs a person:

- Secrets. Their values can not be read, so the report lists the names to enter again.
- People with no linked account, and teams with no match.
- Branch protection settings with no match.
- Anything the token was not allowed to read.

Dismiss the report when you are done.

## From GitHub

A classic token needs the `repo` scope. A fine-grained token needs read access to administration, actions, webhooks, secrets and variables.

- GitHub's maintain role becomes write and triage becomes read.
- Required status checks from GitHub Actions are renamed to match how checks are named here, which is `workflow / job (event)`.
- Rulesets are not copied, only branch protection rules. The report lists them.
- There are no environments here, so their secrets, variables and approval rules are not copied.
- The report also lists organisation secrets and variables the repository uses.

## From Gitea, Forgejo and Codeberg

The token needs the `read:repository` scope.

- Branch protection carries over as it is, apart from its allow-lists. Those name people and teams of the source, so the rule is kept at its strictest where it can be and the report says which lists were dropped.
- Only plain webhooks are copied. The report lists Slack, Discord and other kinds for you to add again.
- People are matched through a Gitea sign-in source for that server. Forgejo and Codeberg use the same kind of source.

## From the API

Set `"settings": true` on `POST /api/v1/repos/migrate` with the `service` and an `auth_token`.
