# Security policy

## Supported versions

Only the latest release receives fixes. Upgrade by re-running `setup.sh`, as described in [Installation](docs/installation.md#upgrade).

## Reporting a vulnerability

Please do not open a public issue for a security problem. Report it privately through GitHub: on the repository's **Security** tab, choose **Report a vulnerability**.

Include the ghr version, what an attacker can do, and the steps to reproduce it.

## Scope

ghr runs CI jobs natively on its host, so some risks are part of its design rather than vulnerabilities:

- Any job can become root on the host through Docker. Run ghr on a dedicated host.
- A repository that lets fork pull requests run workflows lets anyone run code on the host. ghr refuses public repositories unless `allow_public` is set for them, and refuses jobs whose event comes from a fork whatever the repository's visibility. The job-started hook refuses a job by ending the runner's worker process, because a hook that only exits non-zero fails the job but still lets its `if: always()` and `if: failure()` steps run. This was verified from the actions/runner source, not with a real fork pull request; a check against a test repository is still due before release. A runner that is already idle when a private repository turns public may still take one job before the next poll pauses it, so never enable fork pull request workflows.
- An `issue_comment` workflow on a fork's pull request runs the base branch's code and is not refused; it is safe only while it does not check out the pull request's code.
- A `merge_group` workflow is not refused. A merge queue builds a branch of the base repository that holds the pull request's code, a fork's included, and runs it as base-repository code once a maintainer adds the pull request to the queue.
- The web UI is plain HTTP unless a TLS proxy is put in front of it.

Reports about ways around these protections, such as reaching the web UI without the password, running jobs for a repository or a fork ghr should refuse, or a download ghr installs without checking it, are in scope.
