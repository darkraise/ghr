# ghr — native GitHub Actions runner manager

**Date:** 2026-10-03 (revised after Fable review, same day)
**Status:** Draft, awaiting review
**Register:** `docs/superpowers/registers/2026-10-03-ghr-runners.md`
**Review:** `docs/superpowers/notes/2026-10-03-ghr-spec-fable-review.md`

## Problem

`github-runner/compose.yml` runs one `myoung34/github-runner` container per repo
(darkmem, darkcloud, darkagents) with the host Docker socket mounted. This causes
four classes of failure:

1. **Bind-mount path mismatch:** jobs that run docker/compose/Testcontainers pass
   paths that exist inside the runner container but not on the Docker host.
2. **Test networking:** containers started by jobs are not reachable from the job
   the way they would be on a normal host.
3. **Missing toolchains:** the runner image lacks dotnet/node etc.; `setup-*`
   actions re-download every ephemeral run.
4. **Lifecycle/registration:** ephemeral restarts, stale offline runners,
   re-registration failures, orphaned containers.

There is also no way to cap how many jobs run at once across repos.

## Goals

- Runners run natively (no container) on one dedicated Debian 13 LXC, fixing 1–4.
- Any number of repos, each configurable.
- Two modes: `all` (repos run freely, warm runners kept ready) and `queue` (a global
  concurrency cap plus per-repo caps; excess jobs wait in GitHub's queue).
- A rich TUI and a scriptable CLI to observe and manage everything.
- An idempotent setup script that installs and upgrades the whole stack.

## Non-goals (v1)

Webhooks, multiple runner LXCs, organization-level runners, Windows/macOS runners,
metrics export, per-job container resource limits.

**Deferred to v2:** TUI activity sparklines, elapsed bars scaled to historical
durations, streaming (SSE) event/log endpoints, SQLite history.

## Constraints

- `owner` is a personal GitHub account: runners register per repo; there is no
  shared cross-repo runner pool. The cross-repo queue is enforced by `ghr`.
- Token: fine-grained PAT scoped to exactly the configured repos, with
  **Administration: read/write** (mint, read and delete runner registrations) and
  **Actions: read** (list runs/jobs). Adding a repo to `ghr` requires first adding
  it to the PAT's repository access.
- LXC must have `features: nesting=1,keyctl=1` for Docker.
- The `darkraise/ghr` repo is public so `setup.sh` can download releases without a
  token.

## Trust model

All jobs on the LXC are trusted code. `ghrunner` is in the `docker` group, which is
root-equivalent: any job can `docker run -v /:/host` and read or change anything on
the LXC, including the PAT. Therefore:

- Every configured repo must be **private** and must **not** enable "Run workflows
  from fork pull requests". GitHub advises against self-hosted runners for public
  repos because untrusted workflow code can persistently compromise them.
- The LXC is the blast radius. It hosts nothing but `ghr` and its runners.
- The PAT is scoped to the configured repos only, and has an expiry. Rotation:
  `ghr token set` (reads the new token from stdin) or edit `/etc/ghr/token` and
  `systemctl reload ghr`.
- `ghr repo add` refuses a public repo unless `--allow-public` is passed. The
  fork-PR setting is the operator's responsibility; `ghr` does not check it.

File permissions (root-only token, root-only socket) keep honest mistakes out; they
are not a security boundary against a job.

## Architecture

Three pieces, one LXC:

| Piece | Location | Role |
|---|---|---|
| `setup.sh` | homelab `github-runner/` | Install/upgrade Docker, user, runner dist, `ghr`, hooks, config, systemd unit |
| `ghr daemon` | `darkraise/ghr` repo, release binary | Poll → schedule → spawn → reap; control API on a Unix socket |
| `ghr tui`, `ghr <cmd>` | same binary | Clients of the control API only |

The Go source lives in `darkraise/ghr`. GitHub-hosted Actions build `linux/amd64`
release binaries with a checksum file; `setup.sh` downloads the latest (or a
pinned) release and verifies it.

**Privilege model:** `ghr.service` runs the daemon as **root**. It creates instance
directories, writes config, manages transient units and runs Docker cleanup. Each
runner runs as **`ghrunner`** in its own transient systemd unit. The CLI/TUI is run
as root on the LXC.

### Identifiers

Each runner instance has one `<id>`: 6 lowercase hex characters, unique among live
instances. It is used everywhere:

| Thing | Name |
|---|---|
| systemd unit | `ghr-runner-<id>.service` |
| instance dir | `/var/lib/ghr/instances/<id>/` |
| GitHub runner name | `ghr-<repo>-<id>` |
| compose project default | `ghr-<id>` |
| log archive | `/var/lib/ghr/logs/<id>/` |

### On-disk layout (LXC)

| Path | Content | Owner / mode |
|---|---|---|
| `/usr/local/bin/ghr` | binary | root 0755 |
| `/etc/ghr/config.yaml` | config (source of truth) | root 0600 |
| `/etc/ghr/token` | PAT | root 0600 |
| `/opt/ghr/dist/<version>/` | extracted official `actions-runner` release | root, read-only |
| `/opt/ghr/hooks/job-started.sh`, `job-completed.sh` | runner job hooks | root 0755 |
| `/var/lib/ghr/instances/<id>/` | full copy of dist + `_work`, `ghr.json`, `job.json` | ghrunner |
| `/var/lib/ghr/toolcache/` | shared `RUNNER_TOOL_CACHE` and `DOTNET_INSTALL_DIR` | ghrunner |
| `/var/lib/ghr/logs/<id>/` | archived `_diag` logs of finished runners | root |
| `/var/lib/ghr/history.jsonl` | one line per finished job | root |
| `/run/ghr/ghr.sock` | control API socket | root 0600 |

The runner writes `.runner`, `.credentials` and `.credentials_rsaparams` into its
own install dir when started with `--jitconfig`, so instances cannot share one
dist dir. Each instance gets a plain `cp -a` copy (~200 MB, about a second), then
`chown -R ghrunner`. Hard links are not used: chowning a hard link would chown
the dist file too.

`/home/ghrunner` is shared by all instances (`~/.nuget/packages`, `~/.cache`).
Concurrent jobs tolerate this: NuGet and the `setup-*` actions handle concurrent
writers (a losing `setup-*` extraction re-downloads). Workflows must not rely on
`docker login` state in `~/.docker` persisting or being private.

### Config

```yaml
owner: darkraise
mode: queue                 # queue | all
global_max: 2               # queue mode only
poll_interval: 10s
start_timeout: 2m           # starting runner that never comes online is replaced
idle_timeout: 5m            # see "Idle and pause"
disk_high_water: 80         # percent of the Docker data-root filesystem
build_cache_keep: 20GB      # build cache kept when pruning
history_retention: 30d      # history.jsonl lines and archived logs
labels: [homelab, docker]   # added to every runner (keeps old workflows matching)
runner_limits:              # per-runner systemd limits (runner process tree only)
  memory_max: 6G
  cpu_quota: 200%
repos:
  - name: darkcloud
    max: 1
    labels: [darkcloud-linux]
    cleanup_name_prefixes: [dc-e2e-]
  - name: darkmem
    labels: [darkmem-linux]
  - name: darkagents
    labels: [darkagents-linux]
```

Repo fields: `name` (required), `max` (`0` = unlimited; omitted = `1` in `queue`
mode, unlimited in `all` mode), `warm` (`all` mode only; default `1`), `labels`,
`cleanup_name_prefixes`, `paused` (default `false`).

Validation on load and on every write: unique repo names, `max >= 0`,
`global_max >= 1`, `warm >= 0`, `warm <= max` when `max > 0`, durations and sizes
parse. A **warning** (not an error) when `cleanup_name_prefixes` is set on a repo
whose effective `max` is not `1`.

Writes from the CLI/TUI go through the daemon: it validates, writes a temp file in
`/etc/ghr/`, renames it over `config.yaml`, then applies the change live.
`systemctl reload ghr` (SIGHUP) re-reads config and token from disk; an invalid
file is rejected and the previous config stays active.

### Labels

A runner's **effective labels** are `{self-hosted, linux, x64}` (GitHub's system
labels for this platform) ∪ `config.labels` ∪ `repo.labels`. A queued job matches
when every label in its `labels` list is in the effective set. Comparison is
case-insensitive. Jobs targeting GitHub-hosted labels (`ubuntu-latest`) never
match and are ignored.

## Scheduling

### Instance states

`starting → idle → busy → cleaning → gone`

| State | Entered when |
|---|---|
| `starting` | unit launched |
| `idle` | runners API reports the runner `online` and `busy: false` |
| `busy` | `job.json` shows a started job, or the runners API reports `busy: true` |
| `cleaning` | unit exited; cleanup running |
| `gone` | cleanup done, instance dir removed |

A `starting` instance that is not `online` within `start_timeout` is stopped, its
registration deleted, and an event logged. `Plan` replaces it on a later tick.

### Demand

Each tick, for every non-paused repo:

1. List workflow runs with `status` = `queued`, `in_progress` and `waiting`
   (a run waiting on a protection rule can still hold an independent queued job),
   using ETag conditional requests (304 responses don't count against the limit).
2. For each run, list jobs (`filter=latest`, `per_page=100`, paginated). Keep:
   - **queued jobs**: `status == queued` and labels match (see Labels);
   - **our in-progress jobs**: `status == in_progress` and `runner_name` is one of
     our instances.
3. Sort the repo's queued jobs by `created_at`.

### Coverage

A queued job is **covered** if some instance of that repo is expected to take it.
`covering(repo)` = its `starting` + `idle` instances + its `busy` instances whose
job the API has not yet reported `in_progress` (GitHub lists a job as `queued` for
a few seconds after a runner takes it). A busy instance stops covering once its
`runner_name` appears on an in-progress job, or 60 s after it became busy.

The first `covering(repo)` queued jobs (by `created_at`) are covered. The rest are
**uncovered**.

A JIT runner can take any matching queued job in its repo, not necessarily the
one that triggered it. Demand is recomputed each tick, so this self-corrects.

### Caps

- `repo_active(repo)` = its `starting` + `idle` + `busy` + `cleaning` instances.
  Counting `cleaning` keeps a repo's next job from starting while the previous
  job's prefix cleanup runs.
- `total_active` = all `starting` + `idle` + `busy` instances (not `cleaning`).
- A repo is **under cap** when its effective `max` is `0` or `repo_active < max`.

### Decision

`Plan(config, instances, demand) -> []SpawnRequest` is a pure function.

**`queue` mode:**
1. Build one list of all uncovered jobs across non-paused repos, oldest first.
2. Walk the list. For each job: if `total_active == global_max`, stop. If its repo
   is not under cap, skip it. Otherwise emit a spawn for that repo, and count the
   new instance as `starting` (it now covers that job; `total_active` and
   `repo_active` increase).

**`all` mode:** no global cap. For each non-paused repo, spawn
`max(warm − (starting + idle), uncovered)` instances, limited to
`max − repo_active` when `max > 0`.

### Runner lifecycle

1. **Spawn:** `POST /repos/{owner}/{repo}/actions/runners/generate-jitconfig` with
   name `ghr-<repo>-<id>`, `runner_group_id: 1`, the effective labels minus the
   system ones, `work_folder: _work`. Then `cp -a` dist → instance dir, `chown -R
   ghrunner`, write `ghr.json` (`id`, `repo`, `runner_id`, `runner_name`,
   `dist_version`, `spawned_at`), and launch:

   ```
   systemd-run --unit ghr-runner-<id> --uid ghrunner --gid ghrunner
     --property MemoryMax=<runner_limits.memory_max> --property CPUQuota=<runner_limits.cpu_quota> --property KillMode=control-group
     --setenv HOME=/home/ghrunner
     --setenv GHR_INSTANCE_DIR=<dir>
     --setenv RUNNER_TOOL_CACHE=/var/lib/ghr/toolcache
     --setenv DOTNET_INSTALL_DIR=/var/lib/ghr/toolcache/dotnet
     --setenv COMPOSE_PROJECT_NAME=ghr-<id>
     --setenv ACTIONS_RUNNER_HOOK_JOB_STARTED=/opt/ghr/hooks/job-started.sh
     --setenv ACTIONS_RUNNER_HOOK_JOB_COMPLETED=/opt/ghr/hooks/job-completed.sh
     <dir>/run.sh --jitconfig <encoded_jit_config>
   ```

   Mutating API calls (`generate-jitconfig`, `DELETE` runner) are made serially,
   at least 1 s apart.
2. **Hooks:** GitHub fails a job whose pre-job hook exits non-zero, so the hooks
   never talk to the daemon. `job-started.sh` writes `$GHR_INSTANCE_DIR/job.json`
   (`GITHUB_RUN_ID`, `GITHUB_RUN_ATTEMPT`, `GITHUB_RUN_NUMBER`, `GITHUB_WORKFLOW`,
   `GITHUB_JOB`, `RUNNER_NAME`, start time) and always exits 0;
   `job-completed.sh` adds the finish time and always exits 0. The daemon reads
   `job.json` each tick. Display name, steps and conclusion come from the jobs API,
   matched by `runner_name` (the `GITHUB_JOB` YAML key cannot be matched to a jobs
   API entry).
3. **Exit:** the daemon watches the unit. When it exits: state `cleaning`; fetch the
   job's conclusion from the jobs API (by `runner_name`); append a line to
   `history.jsonl` (`id`, `repo`, `run_id`, `run_number`, `workflow`, `job_name`,
   `conclusion`, `started_at`, `finished_at`, `html_url`); move `_diag/*` to
   `/var/lib/ghr/logs/<id>/`; run cleanup; remove the instance dir; state `gone`.
   A runner that exits without taking a job records no history line.
4. **Deregistration:** GitHub removes a JIT runner after its one job. The daemon
   still issues `DELETE` for every gone instance: `404` = already removed
   (success); `422` = busy (skip, retry at reconciliation).
5. **Kill (user):** `systemctl stop` the unit; GitHub marks the job failed or
   cancelled after it loses contact; cleanup runs as normal.

Transient units survive daemon restarts and upgrades. On startup the daemon lists
`ghr-runner-*` units and re-adopts each from its instance dir's `ghr.json` and
`job.json`. A unit whose `ghr.json` is missing or unparsable is stopped, its dir
removed, and any registration named `ghr-*-<id>` deleted.

### Idle and pause

- **`queue` mode:** an `idle` instance older than `idle_timeout` is stopped.
- **`all` mode:** only `idle` instances beyond the repo's `warm` count are stopped
  after `idle_timeout` (newest first). Warm instances stay indefinitely.
- **Pausing a repo:** no new spawns; its `idle` instances (warm included) are
  stopped at once; `busy` instances finish their job.
- **Pause all (drain):** every repo paused.

Before stopping any idle instance the daemon re-checks `busy: false` via the
runners API, to shrink the window in which GitHub assigns it a job.

### Cleanup

After every job (state `cleaning`):

1. Collect compose project names: `ghr-<id>` plus the `com.docker.compose.project`
   of every container whose `com.docker.compose.project.working_dir` label is
   inside the instance dir (catches workflows that pass `-p`).
2. For each project, remove its containers, networks and volumes (all labelled
   `com.docker.compose.project=<name>`).
3. Remove containers whose name starts with one of the repo's
   `cleanup_name_prefixes`.

Not covered: plain `docker run` containers without a configured prefix, images,
and build cache (see Disk). Testcontainers removes its own resources via Ryuk;
workflows must not disable Ryuk.

### Disk

Every 10 ticks, check usage of the filesystem holding Docker's data root
(`docker info` → `DockerRootDir`). Above `disk_high_water`:

1. `docker builder prune -f --filter until=72h`
2. If still above: prune build cache down to `build_cache_keep` (using the flag the
   installed Docker supports: `--reserved-space` on Docker ≥ 28, else
   `--keep-storage`).
3. `docker image prune -f` (dangling images only).

Log the bytes freed. BuildKit does not prune cache held by a running build, so
this is safe while jobs run.

Archived logs and `history.jsonl` lines older than `history_retention` are removed
daily.

### Reconciliation

At startup and every 30 ticks, per repo: list registered runners; delete any named
`ghr-<repo>-<id>` with no matching local unit (`404`/`422` handled as above); remove
instance dirs with no unit. Runners not named `ghr-*` are never touched.

### Error handling

| Failure | Behavior |
|---|---|
| API 5xx / network | per-repo exponential backoff (cap 2m); running instances untouched |
| 403/429 with `x-ratelimit-remaining: 0` or `retry-after` | sleep until `x-ratelimit-reset` / `retry-after`; not degraded |
| 401, or other 403 | daemon `degraded`: no spawns, re-check every 60s; TUI red banner |
| 404 on a configured repo | that repo in error ("token lacks access to repo"); others continue |
| JIT config failure | event logged; retried next tick |
| Unit fails to start | event logged; dir removed; registration deleted |
| `start_timeout` exceeded | unit stopped; registration deleted; respawned by `Plan` |
| Invalid config on write or reload | rejected; previous config stays active |

## Control API

HTTP+JSON over `/run/ghr/ghr.sock`. Clients poll; the TUI polls `/status` and
`/events` every second.

| Method | Path | Purpose |
|---|---|---|
| GET | `/status` | mode, caps, degraded flag, API quota, disk, repos (queued/running/state), instances |
| GET | `/events?after=<seq>` | events with sequence number > `seq` (in-memory ring, last 1000) |
| GET | `/history?repo=&conclusion=&limit=` | past jobs from `history.jsonl` |
| GET | `/runners/{id}/log?offset=<n>` | bytes from `n` of the runner's `_diag/Runner_*.log` and `Worker_*.log` (live or archived) |
| GET | `/runners/{id}/steps` | the job's steps from the jobs API |
| PATCH | `/config` | partial config update (mode, caps, repo fields) |
| POST | `/repos` | add repo (checks PAT access and visibility) |
| DELETE | `/repos/{name}` | remove repo (pauses it, waits for busy instances, then removes) |
| POST | `/repos/{name}/pause`, `/resume` | pause/resume one repo |
| POST | `/pause-all`, `/resume-all` | drain mode |
| PUT | `/token` | replace PAT (validated against one configured repo first) |
| DELETE | `/runners/{id}` | stop a runner |

CLI subcommands map 1:1: `ghr status`, `ghr pause darkcloud`, `ghr resume
darkcloud`, `ghr drain`, `ghr set global-max 3`, `ghr set mode all`,
`ghr repo add <name> [--max N] [--label L] [--allow-public]`, `ghr repo rm <name>`,
`ghr token set`, `ghr kill <id>`, `ghr logs <id> [-f]`, `ghr history`.

## TUI

Bubble Tea + Lip Gloss + Bubbles, mouse via `bubblezone`. Runs as root on the LXC,
typically over SSH.

```
╭─ ghr ─ runner-lxc ──────────────────────────────────────────────────────────────╮
│  mode ● QUEUE   global ▰▰▱ 2/3   api ▕██████████▏ 4.8k/5k   disk ▕██████░░░░▏ 61%│
╰──────────────────────────────────────────────────────────────────────────────────╯
 [1 Dashboard]  2 Runners  3 History  4 Config                            ? help
╭─ Repos ──────────────────────────────────────────────────────────────────────────╮
│   REPO         STATE      RUN   QUEUE   LAST JOB                                 │
│ ▸ darkcloud    ● active   1/1   ⧗ 2     ✔ #411 lint            2m ago            │
│   darkmem      ● active   1/1   –       ✖ #87 build / test     1h ago            │
│   darkagents   ◌ paused   0/1   ⧗ 1     ✔ #19 ci               3d ago            │
╰──────────────────────────────────────────────────────────────────────────────────╯
╭─ Runners ────────────────────────────────────────────────────────────────────────╮
│   ID        REPO        STATE        JOB                          ELAPSED        │
│ ▸ a3f9c1    darkcloud   ⣾ busy       CI / e2e-journeys  #412      12m04s         │
│   7be210    darkmem     ⣾ busy       build / test       #88        1m31s         │
│   –         darkcloud   ⧗ waiting    2 jobs queued (repo cap 1)                  │
╰──────────────────────────────────────────────────────────────────────────────────╯
╭─ Events ─────────────────────────────────────────────────────────────────────────╮
│ 14:02:11  ✔ darkcloud  #411 lint            success   2m10s   cleanup: 3 ctrs     │
│ 14:02:40  ▶ darkcloud  spawned a3f9c1 → #412 e2e-journeys                          │
│ 14:05:52  ⚠ disk 81% > high-water — pruned build cache, freed 6.2 GB               │
╰──────────────────────────────────────────────────────────────────────────────────╯
 p pause  +/- repo cap  [/] global cap  m mode  x kill  l logs  enter details  q quit
```

- **Color:** green active/success, amber queued/paused/warning, red failure and the
  degraded banner, dim idle rows; adapts to dark/light terminals.
- **Live:** spinners on busy runners; header gauges and tables refresh every second.
- **Tabs:**
  - **Dashboard** (above).
  - **Runners:** full table with a split pane following the selected runner's log.
  - **History:** past jobs, filterable by repo and conclusion. `enter` copies the run
    URL to the local clipboard via OSC 52 and shows it in the status line.
  - **Config:** form for mode, global cap, and per-repo `max`, `warm`, labels,
    cleanup prefixes; timeouts. Validated by the daemon before save.
- **Detail view** (`enter` on a runner): the job's steps with ✔ / ⣾ / ○ / ✖, plus
  containers in that instance's compose projects.
- **Responsive:** below 80 columns the LAST JOB column is hidden and the Events
  pane shrinks to 3 lines.
- **Reconnect:** if the daemon is unreachable the TUI shows "daemon unreachable"
  and keeps retrying.

**Keyboard** (covers everything):

| Key | Action |
|---|---|
| `1`–`4` / `tab` | switch tab |
| `↑` `↓` / `j` `k` | move selection |
| `p` | pause/resume selected repo |
| `+` / `-` | selected repo cap |
| `[` / `]` | global cap |
| `m` | toggle mode |
| `a` / `d` | add / remove repo (prompted, confirmed) |
| `x` | stop selected runner (confirm if busy) |
| `l` | follow selected runner log |
| `P` | pause/resume all |
| `?` | help |
| `q` | quit |

**Mouse:**
- click a tab to switch to it;
- click a repo or runner row to select it; double-click a runner for its detail view;
- wheel-scroll the Events pane, log pane and History table;
- click a footer hint to run that action on the selection (destructive actions
  still confirm);
- click fields and buttons in the Config form.

While the TUI captures the mouse, terminal text selection needs Shift-drag
(Option-drag in iTerm2). Under tmux, mouse input requires `set -g mouse on`.

## setup.sh

Idempotent, run as root inside the LXC. Steps:

1. Check Debian 13 and root.
2. Install Docker Engine from Docker's apt repo (`docker-ce`, `docker-ce-cli`,
   `containerd.io`, `docker-compose-plugin`, `docker-buildx-plugin`) and base deps
   (`git curl jq unzip build-essential ca-certificates`). Run `docker info` to
   confirm Docker works (fails clearly if the LXC lacks nesting/keyctl).
3. Create the `ghrunner` user and add it to the `docker` group; create the
   directories above with their owners.
4. Download `actions-runner` (latest or `RUNNER_VERSION`), verify SHA-256, extract to
   `/opt/ghr/dist/<version>/`, run its `bin/installdependencies.sh`.
5. Download the `ghr` release (latest or `GHR_VERSION`), verify its checksum, install
   to `/usr/local/bin/ghr`. Install the hook scripts to `/opt/ghr/hooks/`.
6. If absent, write `/etc/ghr/config.yaml` from `config.example.yaml`, and the token
   from `GHR_TOKEN` or an interactive prompt into `/etc/ghr/token`. Never overwrite
   either.
7. Install `ghr.service` (root, `Restart=always`, `ExecReload` = SIGHUP),
   `daemon-reload`, enable, and restart it.
8. Print `ghr status`.

Upgrades: re-run. A new runner version goes to a new dist dir; running instances
keep their own copy; new spawns use the new version. Dist dirs that are not the
current version are removed.

## Migration from the compose setup

Documented in `github-runner/README.md`:

1. `docker compose down` the old `gh-runners` project on its host.
2. Delete the three `homelab-<repo>` runners in each repo's Settings → Actions →
   Runners (reconciliation only touches `ghr-*` names).
3. Remove `/opt/gh-runners/` on the old host.
4. Workflows keep working without edits: the default labels keep `homelab` and
   `docker`, and the example config keeps each `<repo>-linux` label.

In homelab, `github-runner/compose.yml` and `.env.example` are replaced by
`setup.sh`, `config.example.yaml` and `README.md`.

## Testing

- **Scheduler (`Plan`), table-driven:** global and per-repo caps; `max: 0`
  unlimited; mode-dependent `max` default; cross-repo FIFO; skipping a capped repo
  while others have free slots; `all` mode warm counts; `cleaning` counting toward
  repo cap but not global; paused repos; label matching including system labels and
  case; coverage by a busy instance whose job is still listed `queued` (no extra
  spawn).
- **Lifecycle state machine:** `starting → idle → busy → cleaning → gone`;
  `start_timeout`; idle timeout in both modes (warm kept in `all`); pause stops idle
  only; busy re-check before stop; re-adoption from `ghr.json`/`job.json` after
  daemon restart; unparsable `ghr.json`. Uses fake `GitHub`, `Systemd` and `Docker`
  interfaces.
- **GitHub client:** `httptest` fake — pagination, ETag/304, rate-limit 403/429
  (sleep, not degraded), 401 (degraded), 404 (per-repo error), JIT failure,
  `DELETE` 404/422 handling, serial mutating calls.
- **Cleanup:** project-name collection from labels; containers, networks and
  volumes removed; prefix removal.
- **Config:** validation rules, the prefix warning, atomic write-back round trip,
  reload with invalid file.
- **Hooks:** scripts write `job.json` and exit 0 even when the instance dir is
  missing.
- **TUI:** `teatest` golden snapshots for the dashboard at 120 and 80 columns; mouse
  click on a row selects it.
- **End to end (manual, fresh LXC):**
  1. `setup.sh` on a fresh Debian 13 LXC; re-run it (idempotent).
  2. Scratch private repo; workflow starting 3 jobs at once; `global_max: 2`:
     exactly 2 run, the third shows "Queued" in GitHub until a slot frees.
  3. A compose + Testcontainers job runs with no path or networking workarounds.
  4. Two concurrent compose jobs of the same repo (`max: 2`) don't collide.
  5. `systemctl restart ghr` mid-job: the job finishes. A job that **starts** while
     the daemon is down also succeeds.
  6. `all` mode: the warm runner survives past `idle_timeout`.
  7. A run where one job waits on an environment protection rule and another is
     queued: the queued job is picked up.
