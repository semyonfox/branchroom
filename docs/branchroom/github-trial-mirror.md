# Trying Branchroom beside GitHub

You can run a repository here for a while without leaving GitHub, then switch, and still go back.

## Trial: GitHub stays primary

Migrate the repository with **This repository will be a mirror** ticked. Branchroom then pulls from GitHub every eight hours, or at once with **Synchronize Now** in the repository settings.

While it is a mirror the repository is read-only here. Pushes are refused, and issues and pull requests are not copied. Everyone keeps working on GitHub.

## Switch: Branchroom becomes primary

Open the repository settings and choose **Make this repository primary** in the danger zone. Branchroom then:

1. pulls from GitHub one last time, and stops if that pull fails
2. turns the mirror into a regular repository you can push to
3. sets up a push mirror back to GitHub, which runs after every push and every eight hours

The push uses the credentials the mirror pulled with, unless you enter others. They need write access to the GitHub repository. A classic token also needs the `workflow` scope before GitHub accepts changes to workflow files.

From then on GitHub is a copy. Every branch and tag is overwritten from here, and a branch deleted here is deleted there. Anything pushed straight to GitHub is lost at the next push, so tell the team to stop pushing there.

## Going back

GitHub already has every branch and tag, so there is nothing to export. Remove the push mirror in the repository settings and carry on working on GitHub.

Issues and pull requests opened here are not sent to GitHub.

## What this is not

It is not two-way sync. At any moment one side is primary and the other is a copy.

Issues and pull requests that were on GitHub are not brought over by the switch. To have them here, migrate the repository without the mirror option.
