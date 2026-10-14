# Installation

## Requirements

**Host.** A dedicated LXC or VM running Debian 13 or Ubuntu 22.04, 24.04 or 26.04 on x86-64 (amd64 only), with root access. On Proxmox, the LXC needs `features: nesting=1,keyctl=1` so Docker can run inside it. The host should run nothing else: any job can become root on it through Docker. All repositories ghr serves form one trust domain. A job in any of them can read the GitHub token, take over other repositories' runners and read their job secrets, and leave files in the shared home, package caches and tool cache that later jobs of every repository run. "Single-use" means a fresh runner registration and working directory, not a clean host. Serve only repositories whose workflow authors you would trust with root on the host and with every other served repository.

**Repositories.** ghr serves private repositories. It serves a public one only when `allow_public` is set for it. Any repository it serves must **not** enable "Run workflows from fork pull requests", because a fork's pull request could then run its code on your host.

**Token.** A fine-grained personal access token scoped to exactly the repositories ghr serves, with an expiry:

| Permission | Needed for |
|---|---|
| Administration: read/write | running jobs (required) |
| Actions: read/write | running jobs (required) |
| Contents: read (optional) | drawing a run's job graph from the workflow file; without it the web UI shows a jobs timeline |

The Artifacts page also covers every other repository of the owner the token can reach, and uses more:

| Permission | Needed for |
|---|---|
| Actions: write | deleting artifacts and caches |
| Administration: write | changing artifact retention |
| Plan: read (account permission) | reading the month's storage use |

For an organization, reading storage use needs an organization admin's token with **Administration: read** on the organization. The quota can only be read when the token belongs to the owner. Without these permissions ghr runs jobs as usual, and the Artifacts page says which permission is missing.

## Install

As root on the host:

```bash
curl -fsSL https://github.com/darkraise/ghr/releases/latest/download/setup.sh | bash
# or
wget -qO- https://github.com/darkraise/ghr/releases/latest/download/setup.sh | bash
```

`setup.sh` installs Docker Engine from Docker's apt repository, then the GitHub Actions runner and ghr after checking their checksums, starts the `ghr` service and prints the address that finishes setup in the browser, with the one-time setup code the browser asks for before the web password is set.

To check the script before it runs, download it, verify its build provenance with the [GitHub CLI](https://cli.github.com/) and then run it:

```bash
curl -fsSLO https://github.com/darkraise/ghr/releases/latest/download/setup.sh
gh attestation verify setup.sh -R darkraise/ghr
bash setup.sh
```

Verify the release tarball the same way: download `ghr_linux_amd64.tar.gz` from the release and run `gh attestation verify ghr_linux_amd64.tar.gz -R darkraise/ghr`. Releases before attestations were added have none to verify. `setup.sh` checks the tarball against the release's `checksums.txt`, which comes from the same place as the tarball, so only the attestation proves where a file was built.

Piped from curl, the script asks nothing. To answer its prompts in the terminal instead, run:

```bash
bash <(curl -fsSL https://github.com/darkraise/ghr/releases/latest/download/setup.sh)
```

or `bash deploy/setup.sh` from a checkout. The prompts ask for the web UI password, then the GitHub owner (a user or organization) and the token (the password and the token without echo); press Enter at any prompt to skip it.

Finish whatever you skipped at `http://<host>:8080/setup`: enter the setup code `setup.sh` printed (or run `ghr code` on the host), set the web password, then enter the owner and token, add repositories, adjust settings and choose toolchains.

### Unattended install

Scripts set these variables instead of answering prompts:

| Variable | Effect |
|---|---|
| `GHR_VERSION` | ghr release to install, such as `v0.1.22`; `latest` by default |
| `RUNNER_VERSION` | actions/runner version, such as `2.337.0`; `latest` by default |
| `GHR_OWNER`, `GHR_TOKEN` | configure GitHub; `GHR_TOKEN` is ignored without an owner |
| `GHR_WEB_PASSWORD` | web UI password, on a first install; the browser then needs no setup code |
| `GHR_WEB_LISTEN` | web UI listen address as `host:port`, on a first install; `0.0.0.0:8080` by default, `127.0.0.1:8080` when a TLS proxy runs on the same host. An existing `config.yaml` keeps its `web.listen` |
| `GHR_TOOLCHAINS` | `popular` or `none`; by default `popular` on a first install, `none` on an upgrade |

Then run `ghr setup finish` to end first-run setup without the browser.

```bash
curl -fsSL https://github.com/darkraise/ghr/releases/latest/download/setup.sh |
  GHR_OWNER=my-org GHR_TOKEN="$(cat token.txt)" GHR_WEB_PASSWORD='a long password' bash
ghr setup finish
```

Each release publishes its own `setup.sh`, at `https://github.com/darkraise/ghr/releases/download/<version>/setup.sh`, which installs that release unless `GHR_VERSION` says otherwise.

## Upgrade

Re-run `setup.sh` the same way to upgrade Docker, the runner and ghr. It never overwrites `/etc/ghr/config.yaml` or `/etc/ghr/token`, and while ghr is not configured it asks for the owner and token again.

Prefer upgrading while no jobs run: a Docker upgrade restarts the Docker daemon, which stops the containers of running jobs. `setup.sh` stops ghr while it installs the runner, so a queued runner update cannot run at the same time; running runners are separate systemd units and keep their jobs. If `setup.sh` fails after that, it starts ghr again.

## Older releases

- A first install needs v0.1.17 or later, the first release that ships its example config.
- A config that lists `watch_repos` is refused by any ghr older than v0.1.18, the first release with the Actions page. Remove the `watch_repos` key before installing an older release.
- A config with a `github_storage` block is refused by any ghr older than the first release with the Artifacts page. Remove the block first.
