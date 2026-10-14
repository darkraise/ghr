<h1 align="center">ghr</h1>

<p align="center">
  <strong>Self-hosted GitHub Actions runners on one Linux host, with a web UI.</strong>
</p>

<p align="center">
  <a href="https://github.com/darkraise/ghr/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/darkraise/ghr/actions/workflows/ci.yml/badge.svg"></a>
  <a href="https://github.com/darkraise/ghr/releases/latest"><img alt="Latest release" src="https://img.shields.io/github/v/release/darkraise/ghr"></a>
  <a href="LICENSE"><img alt="MIT License" src="https://img.shields.io/github/license/darkraise/ghr"></a>
  <a href="go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/darkraise/ghr"></a>
</p>

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/images/dashboard-dark.png">
  <img alt="The ghr dashboard: runner capacity, waiting jobs, API budget, host load, an activity timeline and the configured repositories" src="docs/images/dashboard-light.png">
</picture>

ghr runs single-use GitHub Actions runners directly on a Debian or Ubuntu host. It watches your repositories for queued jobs, starts a fresh runner for each one within the limits you set, and cleans up after it. Jobs run natively, so Docker, Docker Compose and Testcontainers work exactly as they do on a developer machine.

## Why ghr

Running your own runners usually means choosing between two compromises. Runners in containers need Docker-in-Docker or a mounted socket, and compose files or Testcontainers that publish ports or mount paths then break in subtle ways. [Actions Runner Controller](https://github.com/actions/actions-runner-controller) scales well, but needs a Kubernetes cluster.

ghr targets the space in between: one LXC or VM, native runners, nothing else to operate. A single binary schedules the runners, keeps the host tidy and gives you a dashboard to see what is happening.

## Features

- **Single-use runners.** Every job gets a fresh just-in-time runner that GitHub removes after the job.
- **Scheduling you control.** A global cap with per-repository limits (`queue` mode), or warm idle runners per repository for instant starts (`all` mode).
- **Native Docker.** Jobs see the host's Docker daemon and filesystem; compose projects and containers a job leaves behind are removed when its runner ends.
- **Resource limits.** Each runner is a systemd unit with its own memory and CPU limits.
- **Web UI.** A live dashboard, workflow runs with job graphs and searchable logs, job history, and every setting, behind a password.
- **Toolchain cache.** Pre-installs Node, Python, Go, Java and .NET where `setup-*` actions find them, so jobs skip the download.
- **Disk care.** Prunes Docker when the disk fills and manages the package caches jobs share.
- **Artifact quota.** Tracks the month's GitHub artifact storage against your plan's allowance, warns before it runs out, and cleans up old artifacts.
- **Runner updates.** ghr tracks new runner releases and GitHub's deadline for them, and installs a queued update once no job is running.
- **One-line install.** An idempotent installer that also upgrades.

## Quick start

You need a dedicated Debian 13 or Ubuntu 22.04, 24.04 or 26.04 host on x86-64 (on Proxmox, an LXC with `features: nesting=1,keyctl=1`) and a fine-grained GitHub token with **Administration: read/write** and **Actions: read/write** on the repositories to serve.

As root on the host:

```bash
curl -fsSL https://github.com/darkraise/ghr/releases/latest/download/setup.sh | bash
```

Then open `http://<host>:8080/setup`, enter the setup code `setup.sh` printed (or run `ghr code` on the host), set the web password, enter your GitHub owner and token, and pick repositories and toolchains. To answer those questions in the terminal instead, or to install unattended, see [Installation](docs/installation.md).

The web UI is plain HTTP on every interface by default, so the setup code, password and token cross the network in clear: finish setup from a trusted network or through a [TLS proxy](docs/web-ui.md#behind-a-reverse-proxy), and set `GHR_WEB_LISTEN` to bind another address.

Point your workflows at the runners by label. Every runner carries `self-hosted`, `linux` and `x64`, the labels in the config's `labels` list, and its repository's own labels:

```yaml
jobs:
  build:
    runs-on: [self-hosted, linux, x64]
```

## How it works

ghr runs as a systemd service. Every few seconds it asks the GitHub API for queued jobs in the configured repositories. When a job is waiting and the caps allow it, ghr requests a just-in-time runner configuration from GitHub, copies the runner software into a fresh instance directory and starts it with `systemd-run` as the `ghrunner` user, under the configured memory and CPU limits. The runner takes exactly one job; GitHub removes its registration afterwards.

Job hooks record which job each runner ran. When a runner exits, ghr removes the Docker compose projects and containers the job started, then the instance directory. The `ghr` command talks to the daemon over a Unix socket, and the web UI over HTTP.

## Security

ghr executes your CI jobs natively on the host, so treat the host as trusted by everyone who can run a workflow:

- Any job can become root on the host through Docker. Do not run anything else on it.
- ghr serves public repositories only when `allow_public` is set for them. Never enable "Run workflows from fork pull requests" on a repository ghr serves: a fork's pull request could then run its code on your host.
- The web UI is plain HTTP. Put a TLS proxy in front of it and keep it on a trusted network; see [Web UI](docs/web-ui.md#behind-a-reverse-proxy).

To report a vulnerability, see [SECURITY.md](SECURITY.md).

## Documentation

- [Installation](docs/installation.md): requirements, token permissions, unattended installs and upgrades
- [Configuration](docs/configuration.md): every setting and the command line
- [Web UI](docs/web-ui.md): pages, password, listen address and reverse proxies
- [Toolchains and caches](docs/toolchains.md): pre-installed toolchains, package caches and Docker pruning
- [Artifact storage](docs/artifact-storage.md): the GitHub storage quota, retention and cleanup

## Contributing

Bug reports and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md) for the development setup, tests and commit conventions.

## License

ghr is released under the [MIT License](LICENSE).
