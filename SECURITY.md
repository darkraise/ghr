# Security policy

## Supported versions

Only the latest release receives fixes. Upgrade by re-running `setup.sh`, as described in [Installation](docs/installation.md#upgrade).

## Reporting a vulnerability

Please do not open a public issue for a security problem. Report it privately through GitHub: on the repository's **Security** tab, choose **Report a vulnerability**.

Include the ghr version, what an attacker can do, and the steps to reproduce it.

## Scope

ghr runs CI jobs natively on its host, so some risks are part of its design rather than vulnerabilities:

- Any job can become root on the host through Docker. Run ghr on a dedicated host.
- A repository that lets fork pull requests run workflows lets anyone run code on the host. ghr refuses public repositories unless `allow_public` is set for them.
- The web UI is plain HTTP unless a TLS proxy is put in front of it.

Reports about ways around these protections, such as reaching the web UI without the password, running jobs for a repository ghr should refuse, or a download ghr installs without checking it, are in scope.
