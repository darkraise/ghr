# Configuration

ghr reads `/etc/ghr/config.yaml`, written from [`config.example.yaml`](../deploy/config.example.yaml) on a first install, and the token from `/etc/ghr/token`. Most settings can be changed from the web UI's Settings page or the `ghr` command, which rewrite the config file and drop its comments.

After editing the file by hand, apply it with `systemctl reload ghr`. The `web` block is read at start only and needs `systemctl restart ghr`.

## Reference

```yaml
owner: my-user              # GitHub user or organization
mode: queue                 # queue | all
global_max: 2               # queue mode only; at most 99, like a repository's max and warm
poll_interval: 10s          # how often GitHub is checked, at least 5s; demand is read every second tick below 1,000 API calls left
start_timeout: 2m           # a runner not online by then is stopped; the next spawn waits, longer after each failure
idle_timeout: 5m            # idle runners beyond warm stop after this
disk_high_water: 80         # percent of Docker's data-root filesystem that triggers pruning
build_cache_keep: 20GB      # Docker build cache kept when pruning
history_retention: 30d      # job history and logs older than this are removed; at least 1d
labels: [docker]            # added to every runner, besides self-hosted, linux and x64; at most 97 custom labels per repository
runner_limits:              # per runner
  memory_max: 6G            # infinity for no limit
  cpu_quota: 200%           # 100% is one CPU core
web:
  listen: 0.0.0.0:8080
  hosts: [ghr.lan]          # names a proxy serves the UI under; IPs and localhost always work
  trusted_proxies: [10.0.0.2]   # reverse proxies, as IPs or CIDR prefixes, whose X-Forwarded-For names the browser; see web-ui.md
github_storage:             # see artifact-storage.md
  included_mb: 500
artifact_store:             # self-hosted artifact storage; see self-hosted-artifacts.md
  endpoint: https://s3.example.com   # or http://192.168.1.20:9000 for a private, loopback or link-local IP; no path
  bucket: gha-artifacts
  cleanup_max_age: 7d       # empty is 7d; off, or at least 24h
jobs:                       # background tasks; see Background tasks below
  ghr_update_check: { enabled: true, every: 24h }
  runner_update_check: { enabled: true, every: 24h }
  prune: { enabled: true, every: 24h }
  storage_measure: { enabled: true, every: 30m }
repos:
  - name: my-repo
    max: 1                  # jobs at once; 0 = unlimited
    warm: 1                 # all mode: idle runners kept ready
    labels: [my-repo-linux]
    cleanup_name_prefixes: [my-repo-]   # when a runner ends, also remove containers whose names start with these; with max above 1 this can hit a concurrent job's containers, including another repository's whose names share the prefix
    allow_public: true      # required to serve a public repository; jobs whose event comes from a fork are refused even then, by ending the runner's worker process (see SECURITY.md)
watch_repos: [other-repo]   # shown on the Actions page; ghr runs no runners for them
```

Durations take `s`, `m`, `h` and `d` suffixes.

`artifact_store.endpoint` takes `https://host[:port]` for any host, or `http://ip[:port]` when the IP address is private, loopback or link-local, such as `http://192.168.1.20:9000`. It refuses a path (a trailing `/` included), `http://` with a host name, `localhost` included, and `http://` with any other address, carrier-grade NAT addresses in `100.64.0.0/10` among them. A refused endpoint fails with `artifact_store.endpoint:` followed by the reason; [Self-hosted artifacts](self-hosted-artifacts.md#the-endpoint) gives the rule in full.

A ghr release from before [self-hosted artifacts](self-hosted-artifacts.md) refuses a config with an `artifact_store` block, because config loading rejects fields it does not know. Remove the block before downgrading.

When a runner ends, ghr archives its diagnostic logs, the `Runner_*.log` and `Worker_*.log` files that `ghr logs` shows, each cut to its first 16 MiB, and removes them `history_retention` after the job ended. Step output is not kept; GitHub has it.

## Scheduling modes

- `queue`: at most `global_max` jobs at once across repositories, and at most each repository's `max` (default 1). Further jobs wait as "Queued" in GitHub until a slot frees.
- `all`: no `global_max`; ghr never runs more than 99 runners in total, and starts at most 10 per poll. Repositories are unlimited within that unless `max` is set. Each repository keeps `warm` idle runners ready (default 1), so a job starts without waiting for a runner to register.

## Background tasks

The `jobs` block schedules the work ghr does on its own. Each task has `enabled` and `every`; a config without the block, or without one of its tasks, keeps the defaults shown in the reference. The web UI's Jobs page shows each task's last and next run, runs one now, and edits the block.

| Task | What it does | Shortest `every` |
|---|---|---|
| `ghr_update_check` | asks GitHub for the newest ghr release; runs at start, then on its interval | 1h |
| `runner_update_check` | asks GitHub for the newest runner release and its forced-update deadline; runs at start, then on its interval | 1h |
| `prune` | removes history and logs older than `history_retention`; while on, a disk above `disk_high_water` also prunes Docker | 1h |
| `storage_measure` | measures the disk use of Docker, toolchains and package caches for the Storage page | 5m |

ghr refuses a shorter interval. Turning two of the tasks off writes a warning to Events:

- `runner_update_check` off: GitHub stops assigning jobs to a runner past its forced-update deadline, and ghr no longer learns of new runner releases or deadlines.
- `prune` off: history and logs older than history_retention are kept, and a full disk is no longer pruned automatically.

With `prune` off ghr still measures the disk for the Dashboard. With `storage_measure` off the Storage page still measures when you refresh it or after a prune.

## Command line

```bash
ghr                           # status, when run in a terminal
ghr status                    # repositories, runners and health
ghr pause <repo>              # no new runners for a repository; running jobs finish
ghr resume <repo>
ghr drain                     # pause every repository
ghr resume-all
ghr set mode queue|all
ghr set global-max <n>
ghr set max <repo> <n>        # 0 = unlimited
ghr set warm <repo> <n>
ghr repo add <name> [--max n] [--label l]... [--allow-public]   # grant the token access to it first
ghr repo rm <name>            # removed once its runners finish
ghr token set < new-token.txt # or prompts
ghr kill <id>                 # stop a runner
ghr logs <id> [-f]            # a runner's diagnostic log
ghr history [--repo r] [--conclusion c] [--limit n]
ghr artifact-store            # the self-hosted artifact store: connection, totals and each repository
ghr artifact-store set-key admin|job   # replace a key: prompts for its ID and secret, or reads them as two lines of stdin
ghr update                    # ghr's and the runner's versions and update state
ghr update --check            # run the update checks that are on, wait up to 60 s, then show
ghr update ghr [--cancel]     # install the newest ghr release, or cancel the install before it replaces the files
ghr update runner [--cancel]  # queue a runner update; downloaded in the background, installed when no job is running or queued
ghr runner-update [--cancel]  # same as ghr update runner
ghr jobs                      # background tasks with their last and next run
ghr jobs run <name>           # run a background task now
ghr jobs set <name> [--every <duration>] [--on | --off]
ghr code                      # the web UI setup code, until a password is set
ghr web set-password          # prompts, or reads stdin
ghr web reset-password        # forget the password; the browser then needs a new code from ghr code
ghr setup                     # first-run state: configured, wizard, owner
ghr setup github [--owner <owner>]          # set the owner and token (prompts, or reads stdin)
ghr setup finish [--toolchains popular|none]
ghr version
journalctl -u ghr -f          # the daemon's log
```

The toolchain, cache, storage and prune commands are described in [Toolchains and caches](toolchains.md).
