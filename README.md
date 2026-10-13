# ghr

ghr runs native GitHub Actions runners for private repositories on a dedicated LXC running Debian 13 or Ubuntu 22.04, 24.04 or 26.04. Runners are not containers: jobs see the real filesystem, network and Docker daemon, so compose and Testcontainers work without workarounds.

ghr polls each repository for queued jobs and starts single-use (JIT) runners within the configured caps:

- `queue` mode: at most `global_max` jobs at once across repositories, plus each repository's `max` (default 1). Extra jobs wait as "Queued" in GitHub.
- `all` mode: no global cap, repositories unlimited by default, one warm idle runner per repository.

It also installs toolchains into the runners' shared tool cache, prunes Docker when the disk fills, keeps a job history, and watches the owner's GitHub artifact storage. Everything is available from a web UI and the `ghr` command.

## Requirements

- A Proxmox LXC running Debian 13 or Ubuntu 22.04, 24.04 or 26.04 on x86-64, with `features: nesting=1,keyctl=1`. It should host nothing else: any job can become root on it through Docker.
- ghr serves private repositories. It serves a public one only when `allow_public` is set for it. Any repository it serves must **not** enable "Run workflows from fork pull requests", because a fork's pull request could then run its code on this host.
- A fine-grained PAT scoped to exactly those repositories, with an expiry and:
  - **Administration: read/write** and **Actions: read/write**, to run jobs;
  - optionally **Contents: read**: with it, a run's page in the web UI draws the job graph from the workflow file; without it, a jobs timeline.

  The Artifacts page also covers every other repository of the owner the PAT can reach, and needs more:
  - **Actions: write** to delete artifacts and caches, and **Administration: write** to change retention;
  - to read the month's storage use, the account permission **Plan: read**; for an organization, the token must be an organization admin's, with **Administration: read** on the organization. The quota can only be read when the token belongs to the owner.

  Without these, ghr runs jobs as before, and the Artifacts page says which permission is missing.

## Install and upgrade

As root on the LXC:

```bash
curl -fsSL https://github.com/darkraise/ghr/releases/latest/download/setup.sh | bash
# or
wget -qO- https://github.com/darkraise/ghr/releases/latest/download/setup.sh | bash
```

setup.sh installs Docker Engine from Docker's apt repository, then the GitHub Actions runner and ghr after checking their checksums, starts the `ghr` service and prints the browser address that finishes setup. Piped like this, it asks nothing. To answer its prompts in the terminal instead, run `bash <(curl -fsSL https://github.com/darkraise/ghr/releases/latest/download/setup.sh)`, or `bash deploy/setup.sh` from a checkout.

On a first install the prompts ask for the GitHub owner (a user or organization), the PAT (without echo) and the web UI password; press Enter at any prompt to skip it. Finish whatever you skipped in the browser at `http://<lxc>:8080/setup`: the first visitor sets the web password, then enters the owner and PAT, adds repositories, adjusts settings and chooses toolchains.

Scripts set these variables instead of answering prompts:

| Variable | Effect |
|---|---|
| `GHR_VERSION` | ghr release to install, such as `v0.1.21`; `latest` by default |
| `RUNNER_VERSION` | actions/runner version, such as `2.337.0`; `latest` by default |
| `GHR_OWNER`, `GHR_TOKEN` | configure GitHub; `GHR_TOKEN` is ignored without an owner |
| `GHR_WEB_PASSWORD` | web UI password, on a first install |
| `GHR_TOOLCHAINS` | `popular` or `none`; by default `popular` on a first install, `none` on an upgrade |

Then run `ghr setup finish` to end first-run setup without the browser. Each release publishes its own `setup.sh`, at `https://github.com/darkraise/ghr/releases/download/<version>/setup.sh`, which installs that release unless `GHR_VERSION` says otherwise.

**Upgrading.** Re-run setup.sh the same way to upgrade Docker, the runner and ghr. It never overwrites `/etc/ghr/config.yaml` or `/etc/ghr/token`, and while ghr is not configured it asks for the owner and PAT again. Prefer upgrading while no jobs run: a Docker upgrade restarts the Docker daemon, which stops the containers of running jobs. setup.sh stops ghr while it installs the runner, so a queued runner update cannot run at the same time; running runners are separate units and keep their jobs. If setup.sh fails after that, it starts ghr again.

**Older releases.** A first install needs v0.1.17 or later, the first release that ships its example config. A config that lists `watch_repos` (the repositories the Actions page watches) is refused by any ghr older than v0.1.18, the first release with that page; remove the `watch_repos` key before installing an older release. Likewise, a config with a `github_storage` block is refused by any ghr older than the first release with the Artifacts page; remove the block first.

## Web UI

ghr serves a browser UI with every page and action ghr offers. The example config turns it on at `0.0.0.0:8080`.

- **Dashboard**: runner capacity, repositories, disk use and recent events.
- **Runners**: every runner with its state and diagnostic log.
- **Actions**: workflow runs of the served and watched repositories, with each run's job graph, annotations and searchable step logs.
- **History**: finished jobs with their durations and conclusions.
- **Artifacts**: the month's GitHub storage quota and each repository's artifacts, caches, retention and cleanup (see [GitHub artifact storage](#github-artifact-storage)).
- **Repositories**: add and remove repositories, and set each one's caps, labels and pause.
- **Settings**: scheduling, runner limits, disk, history and web access; the GitHub token, the GitHub storage policy, the runner software, the account; toolchains, package caches and Docker's disk use.

Until a password is set, the first visitor to the port sets it, so set it at install (`GHR_WEB_PASSWORD` for scripts) or open the UI right after installing. The first-run wizard at `/setup` stays reachable until you finish it.

1. `ghr status` warns while the UI has no password; `ghr web set-password` (12 to 1024 characters) sets it over SSH. `ghr web reset-password` deletes the password and logs every browser out, after which the next visitor sets a new one, so set one right after running it.
2. A config written before the web UI has no `web:` block: add one to `/etc/ghr/config.yaml` (see [Configuration](#configuration)), set `listen`, and run `systemctl restart ghr`. After that, Settings → Web access changes the listen address and hostnames, and the **Restart ghr** banner applies them. A reload does not start or move the listener; it only warns `web settings changed; restart ghr to apply`. The UI refuses an address the browser could not reach it on, and a restart waits while ghr installs, removes or clears toolchains and caches, has such operations queued, prunes, or updates the runner software; running jobs keep running. If the address fails at start, ghr serves the UI on the last address a browser used and shows why.
3. To reach ghr through nginx-proxy-manager under a name, add that name to `web.hosts` (Settings → Web access, or `config.yaml`) and restart. ghr answers only IP addresses, `localhost` and the listed names, which blocks DNS rebinding. The proxy must forward the original `Host` header, as nginx-proxy-manager does by default.
4. On the LAN the UI is plain HTTP: the password and the session cookie cross the network unencrypted. Put nginx-proxy-manager in front for TLS, and bind `listen` to a LAN-only address; change it from `0.0.0.0` if the LXC has more than one network.
5. Behind the proxy every browser shares the proxy's address, so five wrong passwords in 15 minutes lock everyone out for 15 minutes. Put an nginx-proxy-manager access list in front of the host.
6. Sessions live in memory: every restart, including each `setup.sh` upgrade, logs you out.
7. The UI receives updates over one long-lived request, `/api/changes`. ghr marks it `X-Accel-Buffering: no`, which nginx honours, and sends a ping every 15 s, inside nginx's default 60 s read timeout, so nginx-proxy-manager needs no extra settings. Another proxy must pass the response through unbuffered and allow 15 s of silence. If the stream cannot stay open, the UI still works and falls back to polling.

## Command line

```bash
ghr                           # status, when run in a terminal
ghr status                    # repositories, runners and health
ghr pause <repo>              # no new runners for a repository; running jobs finish
ghr resume <repo>
ghr drain                     # pause every repository
ghr resume-all
ghr set mode queue|all
ghr set global-max 3
ghr set max <repo> <n>        # 0 = unlimited
ghr set warm <repo> <n>
ghr repo add <name> [--max n] [--label l]... [--allow-public]   # add it to the PAT's repository access first
ghr repo rm <name>            # removed once its runners finish
ghr token set < new-token.txt # or prompts
ghr kill <id>                 # stop a runner
ghr logs <id> [-f]            # a runner's diagnostic log
ghr history [--repo r] [--conclusion c] [--limit n]
ghr runner-update [--cancel]  # queue a runner update; it runs when no job is running or queued
ghr web set-password          # prompts, or reads stdin
ghr web reset-password
ghr setup                     # first-run state: configured, wizard, owner
ghr setup github [--owner <owner>]          # set the owner and PAT (prompts, or reads stdin)
ghr setup finish [--toolchains popular|none]
ghr version
journalctl -u ghr -f          # the daemon's log
```

The toolchain, cache, storage and prune commands are under [Toolchains and caches](#toolchains-and-caches).

## Configuration

ghr reads `/etc/ghr/config.yaml`, written from `config.example.yaml` on a first install, and the PAT from `/etc/ghr/token`. Edits made through the CLI or the web UI rewrite the config file, dropping comments. After editing the file by hand, apply it with `systemctl reload ghr`; the `web` block needs `systemctl restart ghr`.

```yaml
owner: my-user              # GitHub user or organization
mode: queue                 # queue | all
global_max: 2               # queue mode only; at most 99, like a repository's max and warm
poll_interval: 10s          # how often GitHub is checked; at least 5s
start_timeout: 2m           # a runner not online by then is replaced
idle_timeout: 5m            # idle runners beyond warm stop after this
disk_high_water: 80         # percent of Docker's data-root filesystem that triggers pruning
build_cache_keep: 20GB      # Docker build cache kept when pruning
history_retention: 30d      # job history and logs older than this are removed; at least 1d
labels: [homelab, docker]   # added to every runner, besides self-hosted, linux and x64
runner_limits:              # per runner
  memory_max: 6G            # empty for no limit
  cpu_quota: 200%           # 100% is one CPU core
web:
  listen: 0.0.0.0:8080
  hosts: [ghr.lan]          # names a proxy serves the UI under; IPs and localhost always work
github_storage:             # see GitHub artifact storage
  included_mb: 500
repos:
  - name: my-repo
    max: 1                  # jobs at once; 0 = unlimited
    warm: 1                 # all mode: idle runners kept ready
    labels: [my-repo-linux]
    cleanup_name_prefixes: [my-repo-]   # when a runner ends, also remove containers whose names start with these; with max above 1 this can hit a concurrent job's
    allow_public: true      # required to serve a public repository
watch_repos: [other-repo]   # shown on the Actions page; ghr runs no runners for them
```

## Toolchains and caches

ghr installs toolchains into the runners' shared tool cache (`/var/lib/ghr/toolcache`) in the layout each `setup-*` action reads, so `setup-node`, `setup-python`, `setup-go`, `setup-java` (Temurin) and `setup-dotnet` use them instead of downloading on every job.

- `ghr storage` shows Docker's disk (images, containers, volumes, and the build cache by type), the last prune, the toolchains, other tool-cache folders, the package caches and the running operation; `ghr storage refresh` measures now.
- `ghr toolchain list`; `ghr toolchain available <tool>`, for `node`, `go`, `python`, `java` or `dotnet`.
- `ghr toolchain install <tool> <version>` takes a full version, a partial one (`22`, `3.13`), `latest`, a Java major (`21`) or a .NET channel (`8.0`). `ghr toolchain install --preset popular` queues Node 22 and 24, .NET 8.0 and 10.0, Python 3.13 and 3.14, Go latest, and Java 21 and 25.
- `ghr toolchain rm <tool> <version>`; `ghr cache list`; `ghr cache clear <name>`, for `nuget`, `npm`, `pnpm`, `yarn`, `pip`, `gomod`, `gobuild`, `maven`, `gradle` or `cargo`.
- `ghr prune [--scope <scope>]`: `standard` (the default), `build-cache-keep`, `build-cache-all`, `dangling-images` or `unused-volumes` (anonymous volumes no container uses).

Installs, removals and clears queue and run one at a time; follow them with `ghr storage`. Removing a toolchain, clearing a cache and `--scope unused-volumes` are refused while any runner has a job.

`setup.sh` queues the popular set on a first install once ghr is configured, and returns without waiting; until then (the owner and PAT are skipped) the set stays pending and is offered in the setup wizard, or queue it with `ghr toolchain install --preset popular`. If the first run fails before the set is queued, the next run queues it. `GHR_TOOLCHAINS=popular` or `GHR_TOOLCHAINS=none` overrides that either way.

The Python builds `setup-python` uses are made for Ubuntu, and ghr installs the ones made for the host's release; on Debian it installs Ubuntu 24.04's, and setup.sh installs the libraries they link. On Debian, Python must come from the tool cache: without a cached version, `setup-python` matches its download against `VERSION_ID` in `/etc/os-release` and finds none for Debian 13.

Removing the last .NET SDK of a major version also removes that major's runtimes and packs, so a job that builds with a newer SDK but runs `net8.0` tests loses them.

## GitHub artifact storage

A GitHub plan includes a monthly allowance of storage for Actions artifacts and Packages, 500 MB on the Free plan. GitHub counts it in GB-hours over the month, so an artifact kept for days keeps using the allowance long after its upload, and deleting artifacts once the allowance is spent does not let uploads through again before the next month. The **Artifacts** tab shows the month's use against the allowance and each repository's artifacts, caches, retention and cleanup; the storage policy itself is set under **Settings**, **GitHub storage**. A banner on every page warns before the allowance runs out.

1. Lower the retention to a few days on repositories whose artifacts are only used within their own run: a repository's Retention on the Artifacts page, or **Set retention** for all of them. GitHub's retention also limits how long it keeps runs and logs, which the Actions and log pages read.
2. Set a cleanup age for the rest, under **Settings**, **GitHub storage**; a repository's own age is set in its row on the Artifacts page. At the end of each inventory, every 15 minutes, ghr deletes the artifacts older than it, oldest first and at most 100 at a time, skipping the artifacts of runs that have not completed. The global age is at least 24 hours; a repository's override may go down to 1 hour, or turn cleanup off for it. Cleanup does not protect an artifact that a different run downloads later, such as one a `workflow_run` workflow, a download by run ID or a nightly job reading the last build needs.
3. `included_mb` is the plan's allowance in MB, 500 unless set. The month is taken to be the calendar month in UTC.
4. The Actions cache does not count against the allowance and is never cleaned automatically. A repository's caches can be deleted from its row.

```yaml
github_storage:
  included_mb: 500
  warn_pct: [70, 90]          # the estimate's warning and critical levels
  cleanup_max_age: 2d         # absent is off; at least 24h
  repos:
    my-repo: { cleanup: 6h }  # at least 1h
    other-repo: { cleanup: off }
```

## Development

ghr is a Go daemon and CLI (`cmd/ghr`, `internal/`) with a React web UI (`web/`) embedded into the binary. It needs Go 1.26 and the Node version in `web/.nvmrc`.

```bash
(cd web && npm ci && npm run build)    # writes web/dist, which the binary embeds
go build -ldflags "-X main.version=dev" -o ghr ./cmd/ghr
```

Tests:

```bash
go test -race ./...
(cd web && npm run lint && npm run typecheck && npm test)
bash deploy/tests/setup_test.sh        # setup.sh, offline
```

CI runs these on every pull request and every push to `master`. Each push to `master` that passes them publishes a release: the next patch version, with `ghr_linux_amd64.tar.gz`, `checksums.txt` and a `setup.sh` that installs that release.

## Migrating from the compose runners

1. On the old host: `docker compose -f github-runner/compose.yml down` (project `gh-runners`).
2. In each repository's Settings → Actions → Runners, delete the `homelab-<repo>` runner. ghr only cleans up runners named `ghr-*`.
3. On the old host: `rm -rf /opt/gh-runners`.
4. Workflows need no edits: the default labels keep `homelab` and `docker`, and the example config keeps each `<repo>-linux` label.
