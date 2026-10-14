# Configuration

ghr reads `/etc/ghr/config.yaml`, written from [`config.example.yaml`](../deploy/config.example.yaml) on a first install, and the token from `/etc/ghr/token`. Most settings can be changed from the web UI's Settings page or the `ghr` command, which rewrite the config file and drop its comments.

After editing the file by hand, apply it with `systemctl reload ghr`. The `web` block is read at start only and needs `systemctl restart ghr`.

## Reference

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
labels: [docker]            # added to every runner, besides self-hosted, linux and x64
runner_limits:              # per runner
  memory_max: 6G            # empty for no limit
  cpu_quota: 200%           # 100% is one CPU core
web:
  listen: 0.0.0.0:8080
  hosts: [ghr.lan]          # names a proxy serves the UI under; IPs and localhost always work
github_storage:             # see artifact-storage.md
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

Durations take `s`, `m`, `h` and `d` suffixes.

When a runner ends, ghr archives its diagnostic logs, the `Runner_*.log` and `Worker_*.log` files that `ghr logs` shows, each cut to its first 16 MiB, and removes them `history_retention` after the job ended. Step output is not kept; GitHub has it.

## Scheduling modes

- `queue`: at most `global_max` jobs at once across repositories, and at most each repository's `max` (default 1). Further jobs wait as "Queued" in GitHub until a slot frees.
- `all`: no `global_max`; ghr never runs more than 99 runners in total, and starts at most 10 per poll. Repositories are unlimited within that unless `max` is set. Each repository keeps `warm` idle runners ready (default 1), so a job starts without waiting for a runner to register.

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
ghr runner-update [--cancel]  # queue a runner update; downloaded in the background, installed when no job is running or queued
ghr web set-password          # prompts, or reads stdin
ghr web reset-password
ghr setup                     # first-run state: configured, wizard, owner
ghr setup github [--owner <owner>]          # set the owner and token (prompts, or reads stdin)
ghr setup finish [--toolchains popular|none]
ghr version
journalctl -u ghr -f          # the daemon's log
```

The toolchain, cache, storage and prune commands are described in [Toolchains and caches](toolchains.md).
