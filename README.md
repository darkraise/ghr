# ghr

Native GitHub Actions runners for private repos, managed by [ghr](https://github.com/darkraise/ghr)
on a dedicated Debian 13 LXC. Runners are not containers: jobs see the real filesystem,
network and Docker daemon, so compose and Testcontainers work without workarounds.

`ghr` polls each repo for queued jobs and starts single-use (JIT) runners within the
configured caps:

- `queue` mode: at most `global_max` jobs at once across repos, plus each repo's `max`
  (default 1). Extra jobs wait as "Queued" in GitHub.
- `all` mode: no global cap, repos unlimited by default, one warm idle runner per repo.

## Requirements

- Proxmox LXC, Debian 13, `features: nesting=1,keyctl=1`. It should host nothing else:
  any job can become root on it through Docker.
- Every configured repo is **private** and does **not** enable "Run workflows from fork
  pull requests".
- A fine-grained PAT scoped to exactly those repos, with **Administration: read/write** and
  **Actions: read**, and an expiry.

## Install / upgrade

As root on the LXC:

```bash
curl -fsSL https://github.com/darkraise/ghr/releases/latest/download/setup.sh | bash
# or: wget -qO- https://github.com/darkraise/ghr/releases/latest/download/setup.sh | bash
```

Piped like this, setup.sh asks nothing and ends with the browser URL that finishes setup.
To answer the prompts in the terminal instead, run
`bash <(curl -fsSL https://github.com/darkraise/ghr/releases/latest/download/setup.sh)`,
or `bash deploy/setup.sh` from a checkout. Each release publishes its own `setup.sh`,
which installs that release unless `GHR_VERSION` says otherwise:
`https://github.com/darkraise/ghr/releases/download/<version>/setup.sh`.

On a first install it asks for the GitHub owner (a user or org) and the PAT, the PAT
without echo, then for the web UI password; press Enter at any prompt to skip it.
Finish whatever you skipped in the browser at `http://<lxc>:8080/setup`: the first
visitor sets the web password, then enters the owner and PAT, adds repositories,
adjusts settings and chooses toolchains. Scripts can pass `GHR_OWNER`, `GHR_TOKEN` and
`GHR_WEB_PASSWORD` instead, then run `ghr setup finish`; `GHR_TOKEN` alone no longer
configures ghr.

Re-run setup.sh the same way to upgrade Docker, the runner and ghr. It never overwrites
`/etc/ghr/config.yaml` or `/etc/ghr/token`. While ghr is not configured, a re-run asks for the owner and PAT again. Pin versions with `GHR_VERSION=v0.1.17` or
`RUNNER_VERSION=2.337.0`; a first install needs v0.1.17 or later, the first release that ships its example config. Prefer re-running while no jobs are running: a Docker upgrade
restarts the Docker daemon, which stops the containers of running jobs. setup.sh stops ghr
while it installs the runner, so a queued runner update cannot run at the same time;
running runners are separate units and keep their jobs. If setup.sh fails after that, it
starts ghr again.

**Downgrading.** A config that lists `watch_repos` (the repositories the web UI's Actions page watches) is refused by any ghr older than v0.1.18, the first release with that page. Remove the `watch_repos` key from the config file before installing an older release.

## Use

```bash
ghr                           # status, when run in a terminal
ghr status
ghr pause darkcloud           # no new runners; running jobs finish
ghr set global-max 3
ghr set mode all
ghr runner-update             # queue a runner update; it runs when no job is running or queued
ghr runner-update --cancel    # drop a queued runner update
ghr repo add newrepo --label newrepo-linux   # add it to the PAT's repo access first
ghr token set < new-token.txt
ghr web set-password          # set the web UI password (prompts)
ghr setup                     # first-run state; ghr setup github --owner <owner> sets owner and PAT
ghr setup finish              # end first-run setup without the browser
ghr logs <id> -f
journalctl -u ghr -f          # daemon log
```

Edits made through the CLI or the web UI rewrite `/etc/ghr/config.yaml`, dropping comments.
After editing the file by hand, apply it with `systemctl reload ghr`.

## Toolchains and caches

ghr installs toolchains into the runners' shared tool cache (`/var/lib/ghr/toolcache`) in the layout each `setup-*` action reads, so `setup-node`, `setup-python`, `setup-go`, `setup-java` (Temurin) and `setup-dotnet` use them instead of downloading on every job.

- `ghr storage` shows Docker's disk (images, containers, volumes, and the build cache by type), the last prune, the toolchains, other tool-cache folders, the package caches and the running operation; `ghr storage refresh` measures now.
- `ghr toolchain list`; `ghr toolchain available <tool>`, for `node`, `go`, `python`, `java` or `dotnet`.
- `ghr toolchain install <tool> <version>` takes a full version, a partial one (`22`, `3.13`), `latest`, a Java major (`21`) or a .NET channel (`8.0`). `ghr toolchain install --preset popular` queues Node 22 and 24, .NET 8.0 and 10.0, Python 3.13 and 3.14, Go latest, and Java 21 and 25.
- `ghr toolchain rm <tool> <version>`; `ghr cache list`; `ghr cache clear <name>`, for `nuget`, `npm`, `pnpm`, `yarn`, `pip`, `gomod`, `gobuild`, `maven`, `gradle` or `cargo`.
- `ghr prune [--scope <scope>]`: `standard` (the default), `build-cache-keep`, `build-cache-all`, `dangling-images` or `unused-volumes` (anonymous volumes no container uses).

Installs, removals and clears queue and run one at a time; follow them with `ghr storage`. Removing a toolchain, clearing a cache and `--scope unused-volumes` are refused while any runner has a job.

`setup.sh` queues the popular set on a first install once ghr is configured, and returns without waiting; until then (the owner and PAT are skipped) the set stays pending and is offered in the setup wizard, or queue it with `ghr toolchain install --preset popular`. If the first run fails before the set is queued, the next run queues it. `GHR_TOOLCHAINS=popular` or `GHR_TOOLCHAINS=none` overrides that either way. On an LXC installed before this version, run `ghr toolchain install --preset popular` once.

Python must come from the tool cache on Debian: without a cached version, `setup-python` matches its download against `VERSION_ID` in `/etc/os-release`, and every build it offers targets Ubuntu, so on Debian 13 it finds none. The apt step installs the libraries those builds link.

Removing the last .NET SDK of a major version also removes that major's runtimes and packs, so a job that builds with a newer SDK but runs `net8.0` tests loses them.

## Web UI

ghr serves a browser UI with every page and action ghr offers. The example config turns it on at `0.0.0.0:8080`. Until a password is set, the first visitor to the port sets it, so set it at install (`GHR_WEB_PASSWORD` for scripts) or open the UI right after installing; the first-run wizard at `/setup` stays reachable until you finish it.

1. `ghr status` warns while the UI has no password; `ghr web set-password` (12 to 1024 bytes) sets it over SSH. `ghr web reset-password` deletes the password and logs every browser out, after which the next visitor sets a new one, so set one right after running it.
2. A config written before the web UI has no `web:` block: add one to `/etc/ghr/config.yaml` (see below), set `listen`, and run `systemctl restart ghr`. A reload does not start or move the listener; it only warns `web settings changed; restart ghr to apply`.
3. To reach ghr through nginx-proxy-manager under a name, add that name to `web.hosts` and restart. ghr answers only IP addresses, `localhost` and the listed names, which blocks DNS rebinding. The proxy must forward the original `Host` header, as nginx-proxy-manager does by default.
4. On the LAN the UI is plain HTTP: the password and the session cookie cross the network unencrypted. Put nginx-proxy-manager in front for TLS, and bind `listen` to a LAN-only address; change it from `0.0.0.0` if the LXC has more than one network.
5. Behind the proxy every browser shares the proxy's address, so five wrong passwords in 15 minutes lock everyone out for 15 minutes. Put an nginx-proxy-manager access list in front of the host.
6. Sessions live in memory: every restart, including each `setup.sh` upgrade, logs you out.

```yaml
web:
  listen: 0.0.0.0:8080
  hosts: [ghr.lan]
```

## Migrating from the compose runners

1. On the old host: `docker compose -f github-runner/compose.yml down` (project `gh-runners`).
2. In each repo's Settings → Actions → Runners, delete the `homelab-<repo>` runner.
   ghr only cleans up runners named `ghr-*`.
3. On the old host: `rm -rf /opt/gh-runners`.
4. Workflows need no edits: the default labels keep `homelab` and `docker`, and the example
   config keeps each `<repo>-linux` label.
