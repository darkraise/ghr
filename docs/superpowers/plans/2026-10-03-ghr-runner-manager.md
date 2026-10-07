# ghr Runner Manager Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the Docker-based GitHub runners with native JIT runners on a dedicated LXC, managed by a Go supervisor (`ghr`) with queue/all modes, a CLI, a mouse-enabled TUI and an idempotent setup script.

**Architecture:** `ghr daemon` (root, systemd) polls each repo for queued jobs, decides spawns with a pure scheduler under global and per-repo caps, and starts each single-use runner as `ghrunner` in a transient systemd unit; resumable cleanup, adoption and reconciliation keep Docker and GitHub tidy. A JSON control API on a Unix socket serves the CLI and the Bubble Tea TUI. `setup.sh` in homelab installs Docker, the official runner, the `ghr` release and the service.

**Tech Stack:** Go 1.26, gopkg.in/yaml.v3, Bubble Tea v1 + Lip Gloss + Bubbles + bubblezone, GitHub REST API, systemd, Docker Engine, bash, jq.

**Spec:** docs/superpowers/specs/2026-10-03-ghr-runner-manager-design.md

**Execution:** subagent — `claude --model sonnet --effort high` — 15 of 24 tasks are heavy (risk 3 or total 5+), the work spans two repos and does not fit one context.

**Plan review:** 2026-10-03 — dr-superpowers:judge-opus — executability 17 / coherence 17 / coverage 18 / assumptions 16 (round 2)

## Global Constraints

- Tasks 1–21 run in `D:/Repositories/Personal/ghr` (git repo, `master`, remote `https://github.com/darkraise/ghr.git`, no commits yet). Tasks 22–23 run in `D:/Repositories/Personal/homelab`; Task 24 uses both plus the LXC.
- Module path `github.com/darkraise/ghr`; `go.mod` declares `go 1.26`; local toolchain is go1.26.1.
- Pinned dependencies: `gopkg.in/yaml.v3@v3.0.1`, `github.com/charmbracelet/bubbletea@v1.3.10`, `github.com/charmbracelet/lipgloss@v1.1.0`, `github.com/charmbracelet/bubbles@v0.21.1`, `github.com/lrstanley/bubblezone@v1.0.0`, `github.com/charmbracelet/x/exp/golden@v0.1.0`. Do not upgrade to Bubble Tea v2 (`charm.land/bubbletea/v2`): bubblezone v1.0.0 targets v1.
- Copy every code block verbatim, including comments. Create files with the Write tool, never shell heredocs: the Bash tool collapses `\\` to `\` inside heredocs.
- Line endings are LF (`.gitattributes`: `* text=auto eol=lf`); shell scripts must stay LF.
- Every `go test` carries `-count=1 -timeout 180s`, in tasks and in workflows. `go test -race` needs cgo, which this Windows machine lacks; CI runs it on Ubuntu.
- `bash` and `jq` must be on PATH for the hook tests (Task 9); check with `jq --version`.
- Commits: `<type>(<scope>): <subject>`, type one of feat|fix|docs|style|refactor|test|chore|perf, the whole first line ≤ 50 characters, imperative, English. Use each task's commit command as written. Do not push, tag or create GitHub repos before Task 24, and only with your human partner's approval there.
- Trust model (spec): configured repos are private with fork-PR workflows off; `ghr repo add` refuses public repos unless `--allow-public`.
- Labels: system labels `self-hosted, linux, x64` plus `config.labels` plus repo labels, compared case-insensitively.
- Defaults (spec): `mode: queue`, `global_max: 2`, `poll_interval: 10s`, `start_timeout: 2m`, `idle_timeout: 5m`, `disk_high_water: 80`, `build_cache_keep: 20GB`, `history_retention: 30d`, `runner_limits: {memory_max: 6G, cpu_quota: 200%}`, repo `max` 1 in queue mode / unlimited (0) in all mode, `warm` 1.

## Contracts

Every name below is final: use it exactly, and cite the entry from your task's Interfaces block.

**Module** — `github.com/darkraise/ghr`; binary `cmd/ghr`; `var version = "dev"` set by `-ldflags "-X main.version=<tag>"`.

**CLI entry** (`cmd/ghr/main.go`, `cmd/ghr/cli.go`) — `func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int`; exit 0 ok, 1 runtime error, 2 usage error (`type usageError string`). `var newClient = func() *api.Client` (tests replace it). Socket from `$GHR_SOCKET`, default `/run/ghr/ghr.sock`. `func cli(ctx, c *api.Client, args, stdin, out) error` handles every subcommand except `daemon`, `tui`, `version`, `help`. Subcommands: `daemon`, `tui`, `status`, `pause <repo>`, `resume <repo>`, `drain`, `resume-all`, `set mode|global-max|max <repo>|warm <repo> <n>`, `repo add <name> [--max n] [--label l]... [--allow-public]` (`--max` sent only when given; negative is a usage error), `repo rm <name>`, `token set` (stdin), `kill <id>`, `logs <id> [-f]` (follows with the log cursor), `history [--repo] [--conclusion] [--limit]`, `version`, `help`. Task 1 writes `main.go` with `run` handling only `version` and `help`; Task 12 replaces it; Tasks 20 and 21 add the `daemon` and `tui` cases.

**model types** (`internal/model`, JSON tags in parentheses) —
- `Status{Now time.Time (now), Epoch string (epoch), Mode string (mode), GlobalMax int (global_max), Degraded bool (degraded), DegradedReason string (degraded_reason,omitempty), RateRemaining int (rate_remaining), DiskPct int (disk_pct), Repos []RepoStatus (repos), Instances []InstanceStatus (instances)}`. `Epoch` changes on every daemon start; event sequence numbers restart with it.
- `RepoStatus{Name (name), Paused bool (paused), Removing bool (removing,omitempty), Max int (max; 0 = unlimited), Active int (active), Queued int (queued), Error string (error,omitempty), LastJob *HistoryEntry (last_job,omitempty)}`.
- `InstanceStatus{ID (id), Repo (repo), RunnerName (runner_name), State string (state), Since time.Time (since), Job *JobInfo (job,omitempty)}`.
- `JobInfo{RunID int64 (run_id), RunNumber string (run_number), Workflow (workflow), Name (name), HTMLURL (html_url,omitempty), StartedAt time.Time (started_at)}`.
- `Event{Seq int64 (seq), Time time.Time (time), Level string (level: info|ok|warn|error), Repo (repo,omitempty), Msg (msg)}`.
- `HistoryEntry{ID (id), Repo (repo), RunID int64 (run_id), RunNumber string (run_number), Workflow (workflow), JobName (job_name), Conclusion (conclusion), StartedAt time.Time (started_at), FinishedAt time.Time (finished_at), HTMLURL (html_url,omitempty)}`.
- `Step{Number int (number), Name (name), Status (status), Conclusion (conclusion)}`.
- `LogChunk{Data string (data), Next string (next)}` — `Next` is an opaque cursor; `""` reads from the start.
- `Container{ID (id), Name (name), Image (image), State (state), Project (project)}`.
- `RepoPatch{Max *int (max,omitempty), Warm *int (warm,omitempty), Labels *[]string (labels,omitempty), CleanupNamePrefixes *[]string (cleanup_name_prefixes,omitempty), Paused *bool (paused,omitempty)}`.
- `ConfigPatch{Mode *string (mode,omitempty), GlobalMax *int (global_max,omitempty), StartTimeout *string (start_timeout,omitempty), IdleTimeout *string (idle_timeout,omitempty), Repos map[string]RepoPatch (repos,omitempty)}`.
- `AddRepoRequest{Name (name), Max *int (max,omitempty), Labels []string (labels,omitempty), AllowPublic bool (allow_public)}`.

**config** (`internal/config`) — `ModeQueue = "queue"`, `ModeAll = "all"`, `SystemLabels = ["self-hosted","linux","x64"]`. `Config{Owner, Mode string, GlobalMax int, PollInterval, StartTimeout, IdleTimeout Duration, DiskHighWater int, BuildCacheKeep string, HistoryRetention Duration, Labels []string, RunnerLimits RunnerLimits{MemoryMax, CPUQuota string}, Repos []Repo}` (YAML/JSON keys as in the spec). `Repo{Name string, Max *int, Warm *int, Labels, CleanupNamePrefixes []string, Paused bool, Removing bool}` — `removing` is daemon-managed: set together with `paused` by a repo removal, persisted so it survives restarts, cleared by a per-repo resume. `Duration` (accepts `Nd`; `.D()`, `.String()` prints whole days as `Nd`; YAML and JSON as strings), `ParseDuration(s) (Duration, error)`. `Parse([]byte) (*Config, []string, error)` (defaults, then validate), `Load(path)`, `Save(path, *Config) error` (validate, temp file in the same dir, chmod 0600, rename). Methods: `Clone()` (deep copy), `Repo(name) *Repo` (case-insensitive), `EffectiveMax(Repo) int` (explicit max, else 1 in queue mode and 0 = unlimited in all mode), `EffectiveWarm(Repo) int` (default 1), `CustomLabels(Repo)` (config ∪ repo labels, lower-cased, deduplicated, in order), `EffectiveLabels(Repo)` (system labels + custom), `Validate() ([]string, error)` (warnings, or one error joining every violation). Validation: owner set; mode queue|all; global_max ≥ 1; the four durations > 0; disk_high_water 1..100; build_cache_keep like `20GB`; `memory_max` matches `^([1-9][0-9]*[KMGT]?|[1-9][0-9]?%|100%|infinity)$`; `cpu_quota` matches `^[1-9][0-9]*%$`; repo names non-empty and unique ignoring case; max ≥ 0; warm ≥ 0; warm ≤ max only when max is set explicitly and > 0 (warm is used only in all mode, where an omitted max is unlimited); every repo has at least one custom label; no blank cleanup prefix; removing implies paused. Warning: cleanup prefixes on a repo whose effective max is not 1.

**sched** (`internal/sched`) — `State` = `Starting "starting" | Idle "idle" | Busy "busy" | Cleaning "cleaning"`; `BusyCoverGrace = 60s`; `Instance{ID, Repo string, State State, StateSince time.Time, JobConfirmed bool, Stale bool}` (`Stale`: registered with labels other than the repo's current custom labels); `QueuedJob{Repo string, ID int64, CreatedAt time.Time}`; `Demand map[string][]QueuedJob`; `Spawn{Repo string}`. `MatchLabels(jobLabels, effective []string) bool` (case-insensitive subset); `Covering(insts, repo, now) int` (non-stale starting/idle, plus non-stale busy not yet confirmed within the grace); `Uncovered(insts, repo, jobs, now) []QueuedJob` (oldest first, after the first `Covering` jobs); `Plan(cfg, insts, demand, now) []Spawn` (spec Decision; non-stale starting+idle count toward warm; all four states count toward the repo cap, all but cleaning toward the global cap); `IdleToStop(cfg, insts, now) []string` (stale idle at once; idle of paused or unconfigured repos at once; queue mode: idle ≥ idle_timeout; all mode: the oldest `warm` idle are kept, the rest past idle_timeout are returned newest first); `StartTimedOut(cfg, insts, now) []string`.

**github** (`internal/github`) — `New(owner string, token func() string) *Client`; exported fields `BaseURL` (default `https://api.github.com`), `Owner`, `Token`, `HTTP *http.Client` (30 s timeout), `Now func() time.Time`, `Sleep func(time.Duration)`. `ErrKind`: `ErrOther, ErrAuth, ErrRateLimit, ErrNotFound, ErrUnprocessable, ErrServer`. `APIError{Status int, Kind ErrKind, Message string, RetryAt time.Time}`; `IsKind(err, kind) bool`. Classification: 401 → auth; 403/429 with `X-RateLimit-Remaining: 0` (RetryAt = `X-RateLimit-Reset`) or `Retry-After` (RetryAt = now + seconds), or bare 429 (now + 1m) → rate limit; other 403 → auth; 404 → not found; 422 → unprocessable; 5xx → server. After a rate-limit response every request fails fast with an `ErrRateLimit` APIError until RetryAt, without contacting GitHub; `SuspendedUntil() time.Time`; `ForgetCache()` drops the ETag cache and the suspension. GETs use ETag revalidation; the cache keeps body and next-page link per URL (≤1000 URLs, reset when full). Types: `Run{ID, Status}`, `Job{ID, RunID int64, Name, WorkflowName, Status, Conclusion string, Labels []string, RunnerName string, CreatedAt time.Time, StartedAt, CompletedAt *time.Time, HTMLURL string, Steps []Step}`, `Step{Number, Name, Status, Conclusion}`, `Runner{ID int64, Name, Status string, Busy bool}`, `Repository{FullName string, Private bool}`, `JITConfig{Runner Runner, EncodedJITConfig string}`. Methods (all `ctx` first): `ListRuns(repo, status) ([]Run, error)`, `ListJobs(repo, runID) ([]Job, error)` (`filter=latest`, `per_page=100`, paginated), `ListRunners(repo)`, `GetRunner(repo, id) (*Runner, error)`, `DeleteRunner(repo, id) error` (404 = nil; 422 returned as `ErrUnprocessable`), `GenerateJITConfig(repo, name, labels) (*JITConfig, error)` (`runner_group_id: 1`, `work_folder: _work`), `GetRepo(repo) (*Repository, error)`, `RateRemaining() int` (−1 before any response). `GenerateJITConfig` and `DeleteRunner` are serialized and start ≥ 1 s apart.

**history** (`internal/history`) — `Store{Path string}`: `Append(model.HistoryEntry) error` (skips an entry whose `ID` and `RunID` are already stored), `Query(repo, conclusion string, limit int) ([]model.HistoryEntry, error)` (newest `FinishedAt` first; empty filters match all; limit ≤ 0 = all), `Prune(cutoff time.Time) error` (drops `FinishedAt` before cutoff; atomic rewrite).

**events** (`internal/events`) — `Capacity = 1000`; `New() *Ring` (field `Now func() time.Time`); `Add(level, repo, format string, args ...any)` (also writes the standard log); `After(seq int64) []model.Event`.

**system** (`internal/system`) — `type Runner func(ctx, name string, args ...string) ([]byte, error)`; `var CommandTimeout = 10 * time.Minute`; `Exec` (the real Runner: bounded by CommandTimeout; errors name only the command and its first argument). `UnitSpec{Unit, User, WorkDir string, Props []string, Env map[string]string, Command []string}`. `Systemd{Run Runner}`: `Start(ctx, UnitSpec) error` (`systemd-run --unit= --uid= --gid= --collect --quiet [--working-directory=] --property=… --setenv=K=V (sorted) -- cmd…`), `Stop(ctx, unit) error`, `Active(ctx, unit) (bool, error)` (false only for a printed `inactive`, `failed` or `unknown`; a cancelled context or any other failure is an error), `List(ctx, prefix) ([]string, error)` (active units, without `.service`). `ComposeContainer{ID, Project, WorkingDir}`, `NamedContainer{ID, Name}`, `ProjectContainer{ID, Name, Image, State}`. `Docker{Run Runner}`: `ComposeContainers`, `Containers`, `ProjectContainers(ctx, project)`, `ContainerIDsByLabel(ctx, label)`, `RemoveContainers(ctx, ids)` (no-op for none), `RemoveNetworksByLabel`, `RemoveVolumesByLabel`, `DataRootUsage(ctx) (int, error)`, `PruneBuildCacheOlderThan(ctx, hours) (string, error)`, `PruneBuildCacheTo(ctx, keep) (string, error)` (flag read from `docker builder prune --help`: `--reserved-space` when listed, else `--keep-storage`), `PruneDanglingImages(ctx) (string, error)`; the string results are the reclaimed size (`0B` when none). `Host{Run Runner}`: `CopyTree(ctx, src, dst)` (`cp -a`), `ChownR(ctx, path, user)` (`chown -R user:user`).

**Instance files** — instance dir `/var/lib/ghr/instances/<id>/`, `<id>` = 6 lowercase hex. `ghr.json` = `runner.Meta` (`id`, `repo`, `runner_id`, `runner_name`, `labels` — the custom labels registered, `dist_version`, `spawned_at`). `job.json` = `runner.JobRecord`: `job-started.sh` writes `{"run_id","run_attempt","run_number","workflow","job","runner_name","started_at"}` (all strings, JSON-encoded by `jq`, `started_at` RFC3339 UTC); `job-completed.sh` adds `"finished_at"` (or writes `{"finished_at"}` alone when `job.json` is missing or unreadable). Both hooks exit 0 always. Names: unit `ghr-runner-<id>`, runner `ghr-<repo>-<id>`, compose project `ghr-<id>`, archived logs `/var/lib/ghr/logs/<id>/` (root-owned), pending history `/var/lib/ghr/pending/<id>.json`.

**runner — exported** (`internal/runner`) — constants `RunnerUser = "ghrunner"`, `UnitPrefix = "ghr-runner-"`, `MetaFile = "ghr.json"`, `JobFile = "job.json"`. Interfaces with exactly these methods (ctx first everywhere): `GitHub{ListRuns, ListJobs, ListRunners, GetRunner, DeleteRunner, GenerateJITConfig, RateRemaining}` (signatures as in **github**); `Systemd{Start, Stop, Active, List}` and `Docker{ComposeContainers, Containers, ProjectContainers, ContainerIDsByLabel, RemoveContainers, RemoveNetworksByLabel, RemoveVolumesByLabel, DataRootUsage, PruneBuildCacheOlderThan, PruneBuildCacheTo, PruneDanglingImages}` and `Host{CopyTree, ChownR}` (signatures as in **system**). `Paths{Dist, Instances, Logs, Pending, ToolCache, Hooks, Home string}`. `Meta{ID, Repo string, RunnerID int64, RunnerName string, Labels []string, DistVersion string, SpawnedAt time.Time}`; `JobRecord{RunID, RunAttempt, RunNumber, Workflow, Job, RunnerName, StartedAt, FinishedAt string}`. `Manager{Config func() *config.Config, GH GitHub, SD Systemd, Docker Docker, Host Host, Paths Paths, Events *events.Ring, History *history.Store, Now func() time.Time, NewID func() string}` with `Init() error`, `Wait()`, `Status() model.Status`, `Kill(ctx, id) error`, `RunnerRepoAndRun(id) (repo string, runID int64, runnerName string, err error)`, `RunnerLog(id, cursor string) (model.LogChunk, error)` (cursor = URL-query-encoded `file=offset` pairs; ≤256 KiB per call; live `_diag` or the archive), `RunnerContainers(ctx, id) ([]model.Container, error)`, `Tick(ctx)`, `Adopt(ctx) error`, `Reconcile(ctx, *config.Config)`, `ClearDegraded()`; `RandomID() string`; `ErrUnknownRunner` (string type; `"unknown runner <id>"`). Unit env: `HOME`, `GHR_INSTANCE_DIR`, `RUNNER_TOOL_CACHE`, `DOTNET_INSTALL_DIR` (`<ToolCache>/dotnet`), `COMPOSE_PROJECT_NAME` (`ghr-<id>`), `ACTIONS_RUNNER_HOOK_JOB_STARTED`, `ACTIONS_RUNNER_HOOK_JOB_COMPLETED`; props `MemoryMax=`, `CPUQuota=`, `KillMode=control-group`.

**runner — internals shared by Tasks 13–17** (same package; each task's code calls these exactly) —
- Task 13 `manager.go`: `idRe` (`^[0-9a-f]{6}$`, a valid instance ID); `authRecheck = 60s`, `maxBackoff = 2m`; `type instance struct{ Meta; State sched.State; StateSince time.Time; JobConfirmed bool; Job *model.JobInfo; finishing bool; retryFinish time.Time }`; Manager fields `mu sync.Mutex`, `insts map[string]*instance`, `demand sched.Demand`, `repoErr map[string]string`, `fails map[string]int`, `retryAt map[string]time.Time`, `degraded bool`, `degradedReason string`, `authCheckAt`, `pauseUntil time.Time`, `diskPct int`, `ticks int`, `lastPrune time.Time`, `lastJob map[string]model.HistoryEntry`, `epoch string`, `wg sync.WaitGroup`; methods `recordLastJob(e)` (keeps the newest FinishedAt per repo; caller holds `mu`), `instanceDir(id) string`, `setState(id, s)` (never leaves cleaning), `snapshot() []instance` (copies, sorted by ID), `schedInstances(cfg) []sched.Instance`, `isDegraded() bool`, `apiAllowed(now) bool` (false before `pauseUntil`, and while degraded before `authCheckAt`), `apiErr(repo, err, now)` (auth → degraded + recheck in 60 s; rate limit → `pauseUntil`; 404 → repo error "token lacks access…"; else per-repo backoff 10 s doubling to 2 m with `repoErr` set); funcs `stale(cfg, instance) bool`, `writeJSON(path, v)`, `writeJSONAtomic(path, v)`, `readJSON(path, v)`.
- Task 14 `cleanup.go`: `maxLogChunk = 256 KiB`; `within(path, dir) bool`; `projects(ctx, id) ([]string, error)`; `cleanupDocker(ctx, cfg, id, repo) (removed int, err error)` (joins every failure); `checkDisk(ctx, cfg)`; `setDisk(pct)`; `prune(cfg, now)`; `parseCursor(string) map[string]int64`; `readFrom(path, off, limit)`.
- Task 15 `lifecycle.go`: `finishTimeout = 15m`, `finishRetry = 30s`, `conclusionWait = 30m`; `exists(path) bool`; `uniqueID() (string, error)` (skips IDs with a live instance, instance dir, archive or pending record; 1000 attempts); `pendingPath(id) string`; `spawn(ctx, cfg, repo) error`; `parseTime(string) (time.Time, bool)`; `readJobFiles()`; `refreshRunners(ctx)` (starting and idle); `refreshUnits(ctx)` (also retries failed finishes after `retryFinish`); `beginFinish(id)`; `type pending struct{ Entry model.HistoryEntry; RunnerName string; Cleanup int; Deadline time.Time }` (JSON `entry`, `runner_name`, `cleanup`, `deadline`); `pendingFor(instance, JobRecord, recOK bool) (pending, bool)`; `finish(ctx, id) error` (pending record → archive `_diag` + chown root → Docker cleanup → remove dir → delete registration → drop instance → finalize); `finalize(ctx, path)`; `finalizePending(ctx)`; `stopIdle(ctx, cfg, now)` (stops only after GetRunner returns busy=false); `stopStartTimedOut(ctx, cfg, now)`.
- Task 16 `adopt.go`: `activeUnits(ctx) (map[string]bool, error)`.
- Task 17 `tick.go`: `gatherDemand(ctx, cfg, now)`, `repoDemand(ctx, cfg, repo, ours map[string]string, now) ([]sched.QueuedJob, error)`, `confirm(id, github.Job, now)`, `spawnPlanned(ctx, cfg, now)` (plans without repos in error or backoff; stops when `Config()` returns a different pointer than the tick's).

**runner — test harness** (`fakes_test.go`, Task 13; later runner tests use it) — `fakeGH{runs map["<repo>/<status>"][]github.Run, jobs map[int64][]github.Job, runners map[int64]*github.Runner, listed map[string][]github.Runner, deleted []int64, jitCalls []string ("<name> <labels joined by ,>"), calls []string ("<Method> <repo>"), nextID int64 (first JIT runner ID is 101), errs map["<Method> <repo>"]error, remaining int (4999)}` with `setErr(key, err)` (nil deletes), `callCount() int`, `setRunner(id, status, busy)`, `setJobs(runID, jobs...)`. `fakeSD{started []system.UnitSpec, stopped []string, active, stopErr, activeErr map[unit]…}`. `fakeDocker{compose, named, byProject map[project][]system.ProjectContainer, byLabel map[label][]string, removed, netLabels, volLabels, prunes []string, usage []int (successive DataRootUsage values, last repeats, negative = error), errs map[method]error}` with `newFakeDocker()`, `setErr(method, err)`. `fakeHost{chowned []string ("<path>:<user>")}` (CopyTree creates `dst/_diag` and `dst/run.sh`). `harness{m *Manager, cfg *config.Config, gh, sd, docker, host, now time.Time (2026-10-03 12:00 UTC), ids []string (aaaaaa…ffffff, consumed by NewID), root string}`; `newHarness(t)` (config: owner darkraise, queue mode, global_max 2, labels [homelab]; repos darkcloud {max 1, cleanup prefix `dc-e2e-`} and darkmem; Paths under `t.TempDir()`: `dist/2.330.0`, `instances`, `logs`, `pending`, `toolcache`); `h.state(id) string` (`"gone"` when absent); `h.writeJob(t, id, runID, extraJSONFields...)` (run_number 412, workflow CI, job e2e, started 2026-10-03T12:00:00Z); `h.eventText() string` (`"<level> <repo> <msg>\n"` lines); `h.history(t) []string` (`"<id> <conclusion>"`, newest first). Later test files add `completedJob(h, runID, runner, conclusion)` (Task 15), `makeInstance(t, h, id, repo, runnerID, withJob)` (Task 16) and `queuedRun(h, repo, runID, jobs...)` (Task 17).

**api** (`internal/api`) — `DefaultSocket = "/run/ghr/ghr.sock"`; `Error{Status int, Msg string}`, `BadRequest(msg)`, `NotFound(msg)`, `Conflict(msg)` (all `error`); `Backend` interface: `Status() model.Status`, `EventsAfter(seq int64) []model.Event`, `History(repo, conclusion string, limit int) ([]model.HistoryEntry, error)`, `RunnerLog(id, cursor string) (model.LogChunk, error)`, `RunnerSteps(ctx, id) ([]model.Step, error)`, `RunnerContainers(ctx, id) ([]model.Container, error)`, `Config() any`, `PatchConfig(model.ConfigPatch) error`, `AddRepo(ctx, model.AddRepoRequest) error`, `RemoveRepo(name) error`, `SetPausedAll(bool) error`, `SetToken(ctx, token) error`, `KillRunner(ctx, id) error`. `NewServer(Backend) http.Handler` with routes `GET /status`, `GET /events?after=`, `GET /history?repo=&conclusion=&limit=`, `GET /runners/{id}/log?cursor=`, `GET /runners/{id}/steps`, `GET /runners/{id}/containers`, `DELETE /runners/{id}`, `GET /config`, `PATCH /config`, `POST /repos`, `DELETE /repos/{name}`, `POST /repos/{name}/pause|resume` (a `RepoPatch` with `paused`), `POST /pause-all|resume-all`, `PUT /token` (plain-text body); empty lists are `[]`; errors are `{"error": "..."}` with the `Error` status (else 500); no body → 204. `Client{Base string, HTTP *http.Client}`, `NewUnixClient(socket) *Client` (base `http://ghr`, 30 s timeout); methods `Status`, `Events(after)`, `History(repo, conclusion, limit)`, `Log(id, cursor)`, `Steps(id)`, `Containers(id)`, `Config(out any)`, `PatchConfig`, `AddRepo`, `RemoveRepo`, `Pause`, `Resume`, `PauseAll`, `ResumeAll`, `SetToken`, `Kill` (ctx first); a failed status returns `*Error`; an unreachable socket returns `ghr daemon unreachable: …`.

**daemon** (`internal/daemon`) — `Store{ConfigPath, TokenPath string}`: `OpenStore(configPath, tokenPath) (*Store, []string, error)`, `Config() *config.Config` (an immutable snapshot; a new pointer after every successful write or reload), `Token() string`, `Reload() ([]string, error)` (keeps the previous config and token on an invalid file, an empty token, or a changed owner), `Update(fn func(*config.Config) error) ([]string, error)` (clone → fn → validate → `config.Save` → swap; nothing changes on any error), `SetToken(tok) error` (0600, atomic). `Manager` interface (backend's view of `*runner.Manager`): `Status`, `RunnerLog(id, cursor)`, `RunnerContainers(ctx, id)`, `RunnerRepoAndRun(id)`, `Kill(ctx, id)`, `ClearDegraded()`. `GitHub` interface: `GetRepo`, `ListJobs`, `ForgetCache`. `Backend{Store *Store, M Manager, GH GitHub, Events *events.Ring, Hist *history.Store, CheckToken func(ctx, token, repo string) error, Wake func()}` implements `api.Backend`; every successful config change calls `Wake` (when set); `FinalizeRemovals()` removes repos marked `removing` with no live instance, re-checking the flag inside the store update; resume-all leaves removing repos paused. `Options{ConfigPath, TokenPath, Socket, HistoryPath string, Paths runner.Paths, ShutdownWait time.Duration, GitHubURL string, Systemd runner.Systemd, Docker runner.Docker, Host runner.Host, Reload <-chan os.Signal}` (zero test seams = real GitHub, systemd, Docker, host and SIGHUP); `DefaultOptions()` (paths below, ShutdownWait 30 s); `Run(ctx, Options) error`: open store → bind the socket (refuses with `another ghr daemon is already serving <socket>` when a daemon answers; replaces a dead socket) → Init → Adopt → Reconcile → serve → loop (tick, `FinalizeRemovals`, next tick after `poll_interval`; a wake or reload ticks at once) → on cancel: stop serving, wait ≤ ShutdownWait for cleanups, leave runner units running.

**tui** (`internal/tui`) — `Run(c Client) error`; `type Client interface` with exactly these `*api.Client` methods (ctx first): `Status`, `Events(after)`, `History(repo, conclusion, limit)`, `Log(id, cursor)`, `Steps(id)`, `Containers(id)`, `Config(out any)`, `PatchConfig`, `AddRepo`, `RemoveRepo`, `Pause`, `Resume`, `PauseAll`, `ResumeAll`, `Kill`. `ghr tui` calls `tui.Run(newClient())`. The TUI polls status and events every second, follows the selected runner's log on the Runners tab (one request in flight, cursor-checked), refreshes an open detail view's steps and containers every 5 ticks, and resets its event cursor when `Status.Epoch` changes.

**On-disk paths** — `/etc/ghr/config.yaml` (0600), `/etc/ghr/token` (0600), `/opt/ghr/dist/<version>/` with symlink `/opt/ghr/dist/current`, `/opt/ghr/hooks/job-started.sh|job-completed.sh` (0755), `/var/lib/ghr/{instances,toolcache}` (ghrunner), `/var/lib/ghr/{logs,pending}` and `/var/lib/ghr/history.jsonl` (root), `/run/ghr/ghr.sock` (0600), `/usr/local/bin/ghr`, `/etc/systemd/system/ghr.service`.

**Release assets** — tag `v*` → `ghr_linux_amd64.tar.gz` containing `ghr/ghr`, `ghr/job-started.sh`, `ghr/job-completed.sh`, plus `checksums.txt` (sha256sum format).

## Assumptions (evidence)

- Every code block in Tasks 1–21 was compiled and its tests run on 2026-10-03 (Windows 11, go1.26.1) by replaying the tasks in order into an empty directory: each task's test failed before its implementation and passed after, and the final tree passed `gofmt -l`, `go vet ./...` and `go test ./...`. Hook tests ran with jq 1.8.1.
- The pinned dependency set builds together (scratch module, 2026-10-03). `go list -m -versions` showed bubbletea v1.3.10 latest v1; bubblezone v1.0.0 is its latest release.
- `darkraise/ghr` exists, is public and empty (`gh repo view darkraise/ghr --json visibility,isEmpty` → PUBLIC, true, 2026-10-03); `D:/Repositories/Personal/ghr` is its clone with no commits.
- darkmem, darkcloud and darkagents are private (`gh api repos/darkraise/<repo> --jq .private` → true, 2026-10-03).
- darkcloud's CI runs on GitHub-hosted runners (`darkcloud/.github/workflows/ci.yml:10` `runs-on: ubuntu-latest`, commit 1717ae1b), so it cannot exercise ghr; Task 24 uses its own Testcontainers fixture, compiled against testcontainers-go v0.44.0 (`go vet`, 2026-10-03).
- On GitHub Free, Pro and Team, environment wait timers and required reviewers exist only in public repos — https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments (read 2026-10-03). Your human partner chose (2026-10-03) to run the waiting-run check in the public `darkraise/ghr` repo: registered with `--allow-public` only for that check, `workflow_dispatch` only, fork-PR approval required for all outside contributors, removed afterwards. A deterministic fake-client test (`TestQueuedJobInWaitingRunIsServed`, Task 17) covers the same logic.
- Docker documents only `--keep-storage` for `docker builder prune` (https://docs.docker.com/reference/cli/docker/builder/prune/, read 2026-10-03), while buildx documents `--reserved-space`; `PruneBuildCacheTo` therefore reads the flag from the installed command's `--help`. Task 24 Step 5 records that help output.
- actions/runner release notes carry `<!-- BEGIN SHA linux-x64 -->…<!-- END SHA linux-x64 -->` (`gh api repos/actions/runner/releases/latest`, v2.337.0, 2026-10-03).
- JIT runners write `.runner`/`.credentials` into their install dir, so each instance needs its own copy — https://raw.githubusercontent.com/actions/runner/main/src/Runner.Listener/Runner.cs (Fable review, 2026-10-03).
- A non-zero pre-job hook fails the job — https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners/running-scripts-before-or-after-a-job.
- Jobs API exposes `runner_name`, `labels`, `steps` — https://docs.github.com/en/rest/actions/workflow-jobs.
- `jq` is on GitHub's ubuntu-latest image: unverified — the hook tests fail instead of skipping when `CI` is set, so Task 24 Step 2's CI run verifies it.
- `systemd-run --uid=ghrunner` from a root daemon inside an unprivileged Debian 13 LXC: unverified — Task 24 Step 7 verifies it on a running job's unit.
- `docker ps --format '{{.Label "com.docker.compose.project"}}'` prints compose labels: unverified — Task 24 Step 8 verifies it during a compose job.
- A queued job in a run whose other job waits on an environment protection rule is listed under `status=waiting` runs: unverified — Task 24 Step 12 records both API responses.
- Plan decisions beyond the spec text: `GET /config` (the Config tab reads it); CLI `resume-all`, `set max`, `set warm`; `GET /runners/{id}/containers` (the detail view's containers); the log endpoint takes an opaque per-file `cursor` instead of one `offset`, because the Runner and Worker logs grow at the same time; `Status.epoch`; the repo field `removing` persists a pending removal; `job-completed.sh` adds `finished_at` to `job.json` as the spec says (no separate file); `setup.sh` waits up to 60 s for `ghr status` after restarting the service (reporting a failed service separately from a slow start) and removes old dist versions only after that; history waits up to 30 minutes for a job's final conclusion, kept in `/var/lib/ghr/pending/`; IDs with retained logs or pending history are never reused; a failed cleanup keeps the instance `cleaning` (holding its repo slot) and retries every 30 s; runners registered with outdated labels are stale (stop covering, idle ones stop at once); changing `owner` needs a restart; host commands time out after 10 minutes; shutdown waits ≤ 30 s for cleanups, which resume at the next start; the bound socket is the single-daemon lock; `h`/`→` switch dashboard focus. TUI goldens use `charmbracelet/x/exp/golden` on `Model.View()` instead of `teatest`, because a View snapshot is deterministic where a running program's frame stream is not.
- External executors: none. Your human partner chose Claude-only implementers on 2026-10-03, so the lane-eligible tasks carry no **Executor:** line (plan-lint warns on each).
- Walking skeleton: Task 1 establishes module, CLI entry and CI; the socket → daemon → GitHub path is first exercised in Task 20's daemon test (fake GitHub, systemd and Docker) and for real in Task 24.

## Task index

1. Repository skeleton and CI
2. Shared model types
3. Config package
4. Scheduler
5. GitHub REST client
6. Job history store
7. Event ring
8. System adapters (systemd, docker, cp/chown, df)
9. Runner job hooks
10. Release workflow
11. Control API server and client
12. CLI subcommands
13. Runner manager core
14. Runner cleanup, disk pruning and logs
15. Runner lifecycle: spawn, state, finish, idle stop
16. Runner adoption and reconciliation
17. Runner tick: demand, errors, spawning
18. Daemon config and token store
19. Daemon API backend
20. Daemon run loop and `ghr daemon`
21. TUI
22. Homelab setup script
23. Homelab runner README
24. Publish v0.1.0 and verify on the LXC

---

### Task 1: Repository skeleton and CI

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `go.mod`
- Create: `.gitattributes`
- Create: `.gitignore`
- Create: `cmd/ghr/main.go`
- Create: `.github/workflows/ci.yml`
- Test: `cmd/ghr/main_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: module `github.com/darkraise/ghr` (Contracts → Module); `run(args, stdin, stdout, stderr) int` in `cmd/ghr/main.go` (Contracts → CLI entry), extended by Tasks 12, 20 and 21.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 0 = 3

- [ ] **Step 1: Check the starting state**

Confirm the working directory is the empty `ghr` clone: `git -C D:/Repositories/Personal/ghr status` reports `No commits yet` on `master`, and `git -C D:/Repositories/Personal/ghr remote -v` shows `https://github.com/darkraise/ghr.git`. If either differs, stop and ask your human partner.

- [ ] **Step 2: Write the failing test `cmd/ghr/main_test.go`**

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionAndUsage(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"version"}, strings.NewReader(""), &out, &errb); code != 0 || strings.TrimSpace(out.String()) != "dev" {
		t.Fatalf("version: exit %d out %q", code, out.String())
	}
	out.Reset()
	if code := run([]string{"help"}, strings.NewReader(""), &out, &errb); code != 0 || !strings.Contains(out.String(), "usage: ghr") {
		t.Fatalf("help: exit %d", code)
	}
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 2 {
		t.Fatalf("no args: exit %d", code)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./cmd/ghr/`
Expected: FAIL — FAIL: `go.mod` not found / `undefined: run`

- [ ] **Step 4: Write `go.mod`**

```text
module github.com/darkraise/ghr

go 1.26
```

- [ ] **Step 5: Write `.gitattributes`**

```text
* text=auto eol=lf
```

- [ ] **Step 6: Write `.gitignore`**

```text
dist/
```

- [ ] **Step 7: Write `cmd/ghr/main.go`**

```go
// Command ghr manages native GitHub Actions runners: `ghr daemon` runs the supervisor,
// every other subcommand talks to it over its Unix socket.
package main

import (
	"fmt"
	"io"
	"os"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	fmt.Fprintln(stderr, "ghr: unknown command "+args[0])
	fmt.Fprint(stderr, usage)
	return 2
}

const usage = `usage: ghr <command>

  daemon                          run the supervisor (systemd runs this)
  tui                             interactive dashboard
  status                          repos, runners and health
  pause <repo> | resume <repo>    stop/start new runners for a repo
  drain | resume-all              pause/resume every repo
  set mode <queue|all>
  set global-max <n>
  set max <repo> <n>              0 = unlimited
  set warm <repo> <n>
  repo add <name> [--max n] [--label l]... [--allow-public]
  repo rm <name>                  removed once its runners finish
  token set                       read a new PAT from stdin
  kill <id>                       stop a runner
  logs <id> [-f]                  runner diagnostic log
  history [--repo r] [--conclusion c] [--limit n]
  version
`
```

- [ ] **Step 8: Write `.github/workflows/ci.yml`**

```yaml
name: ci

on:
  push:
    branches: [master]
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: gofmt
        run: test -z "$(gofmt -l .)" || (gofmt -l . && exit 1)
      - run: go vet ./...
      - run: go test -race -count=1 -timeout 180s ./...
```

- [ ] **Step 9: Run the check**

Run: `go test -count=1 -timeout 180s ./cmd/ghr/`
Expected: PASS (exit 0).

- [ ] **Step 10: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 11: Commit**

```bash
git add cmd/ghr/main_test.go go.mod .gitattributes .gitignore cmd/ghr/main.go .github/workflows/ci.yml
git commit -m "chore(repo): add module skeleton, CLI and CI"
```

---

### Task 2: Shared model types

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `internal/model/model.go`

**Interfaces:**
- Consumes: nothing.
- Produces: package `model` (Contracts → model types).

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 0 - spec 0 - coupling 2 - risk 0 = 2

- [ ] **Step 1: Write `internal/model/model.go`**

```go
// Package model holds the types shared by the daemon, the control API and its clients.
package model

import "time"

type Status struct {
	Now time.Time `json:"now"`
	// Epoch changes on every daemon start; event sequence numbers restart with it.
	Epoch          string           `json:"epoch"`
	Mode           string           `json:"mode"`
	GlobalMax      int              `json:"global_max"`
	Degraded       bool             `json:"degraded"`
	DegradedReason string           `json:"degraded_reason,omitempty"`
	RateRemaining  int              `json:"rate_remaining"`
	DiskPct        int              `json:"disk_pct"`
	Repos          []RepoStatus     `json:"repos"`
	Instances      []InstanceStatus `json:"instances"`
}

type RepoStatus struct {
	Name     string        `json:"name"`
	Paused   bool          `json:"paused"`
	Removing bool          `json:"removing,omitempty"`
	Max      int           `json:"max"` // 0 = unlimited
	Active   int           `json:"active"`
	Queued   int           `json:"queued"`
	Error    string        `json:"error,omitempty"`
	LastJob  *HistoryEntry `json:"last_job,omitempty"`
}

type InstanceStatus struct {
	ID         string    `json:"id"`
	Repo       string    `json:"repo"`
	RunnerName string    `json:"runner_name"`
	State      string    `json:"state"`
	Since      time.Time `json:"since"`
	Job        *JobInfo  `json:"job,omitempty"`
}

type JobInfo struct {
	RunID     int64     `json:"run_id"`
	RunNumber string    `json:"run_number"`
	Workflow  string    `json:"workflow"`
	Name      string    `json:"name"`
	HTMLURL   string    `json:"html_url,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

type Event struct {
	Seq   int64     `json:"seq"`
	Time  time.Time `json:"time"`
	Level string    `json:"level"` // info | ok | warn | error
	Repo  string    `json:"repo,omitempty"`
	Msg   string    `json:"msg"`
}

type HistoryEntry struct {
	ID         string    `json:"id"`
	Repo       string    `json:"repo"`
	RunID      int64     `json:"run_id"`
	RunNumber  string    `json:"run_number"`
	Workflow   string    `json:"workflow"`
	JobName    string    `json:"job_name"`
	Conclusion string    `json:"conclusion"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	HTMLURL    string    `json:"html_url,omitempty"`
}

type Step struct {
	Number     int    `json:"number"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// LogChunk is the next part of a runner's _diag logs. Next is an opaque cursor
// holding one offset per log file; pass it back to continue, "" to start over.
type LogChunk struct {
	Data string `json:"data"`
	Next string `json:"next"`
}

// Container is one container of a runner instance's compose projects.
type Container struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Image   string `json:"image"`
	State   string `json:"state"`
	Project string `json:"project"`
}

type RepoPatch struct {
	Max                 *int      `json:"max,omitempty"`
	Warm                *int      `json:"warm,omitempty"`
	Labels              *[]string `json:"labels,omitempty"`
	CleanupNamePrefixes *[]string `json:"cleanup_name_prefixes,omitempty"`
	Paused              *bool     `json:"paused,omitempty"`
}

type ConfigPatch struct {
	Mode         *string              `json:"mode,omitempty"`
	GlobalMax    *int                 `json:"global_max,omitempty"`
	StartTimeout *string              `json:"start_timeout,omitempty"`
	IdleTimeout  *string              `json:"idle_timeout,omitempty"`
	Repos        map[string]RepoPatch `json:"repos,omitempty"`
}

type AddRepoRequest struct {
	Name        string   `json:"name"`
	Max         *int     `json:"max,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	AllowPublic bool     `json:"allow_public"`
}
```

- [ ] **Step 2: Run the check**

Run: `go vet ./internal/model/ && go build ./...`
Expected: PASS (exit 0).

- [ ] **Step 3: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add internal/model/model.go
git commit -m "feat(model): add shared API types"
```

---

### Task 3: Config package

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: nothing.
- Produces: package `config` (Contracts → config).

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Add dependencies**

Run: `go get gopkg.in/yaml.v3@v3.0.1`
Expected: exit 0; `go.mod` lists the pinned versions. Do not run `go mod tidy` yet: no source imports them, so tidy would drop them.

- [ ] **Step 2: Write the failing test `internal/config/config_test.go`**

```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sample = `
owner: darkraise
mode: queue
global_max: 2
history_retention: 30d
labels: [homelab, Docker]
repos:
  - name: darkcloud
    max: 1
    labels: [darkcloud-linux]
    cleanup_name_prefixes: [dc-e2e-]
  - name: darkmem
`

func TestParseAppliesDefaults(t *testing.T) {
	c, warnings, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings %v", warnings)
	}
	if c.PollInterval.D() != 10*time.Second || c.IdleTimeout.D() != 5*time.Minute {
		t.Fatalf("defaults not applied: %+v", c)
	}
	if c.HistoryRetention.D() != 30*24*time.Hour {
		t.Fatalf("day suffix not parsed: %v", c.HistoryRetention)
	}
	if c.BuildCacheKeep != "20GB" || c.RunnerLimits.MemoryMax != "6G" {
		t.Fatalf("defaults not applied: %+v", c)
	}
}

func TestEffectiveMaxDependsOnMode(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	mem := *c.Repo("darkmem")
	if got := c.EffectiveMax(mem); got != 1 {
		t.Fatalf("queue default max = %d, want 1", got)
	}
	c.Mode = ModeAll
	if got := c.EffectiveMax(mem); got != 0 {
		t.Fatalf("all default max = %d, want 0 (unlimited)", got)
	}
	if got := c.EffectiveMax(*c.Repo("darkcloud")); got != 1 {
		t.Fatalf("explicit max = %d, want 1", got)
	}
	if got := c.EffectiveWarm(mem); got != 1 {
		t.Fatalf("default warm = %d, want 1", got)
	}
}

func TestLabels(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(c.EffectiveLabels(*c.Repo("darkcloud")), ",")
	if got != "self-hosted,linux,x64,homelab,docker,darkcloud-linux" {
		t.Fatalf("effective labels = %s", got)
	}
}

func TestValidateErrors(t *testing.T) {
	neg := -1
	two := 2
	three := 3
	cases := map[string]func(c *Config){
		"owner is required":            func(c *Config) { c.Owner = "" },
		"mode must be":                 func(c *Config) { c.Mode = "fast" },
		"global_max must be >= 1":      func(c *Config) { c.GlobalMax = 0 },
		"duplicate repo darkmem":       func(c *Config) { c.Repos = append(c.Repos, Repo{Name: "darkmem"}) },
		"darkmem: max must be >= 0":    func(c *Config) { c.Repos[1].Max = &neg },
		"darkmem: warm must be <= max": func(c *Config) { c.Repos[1].Max = &two; c.Repos[1].Warm = &three },
		"needs at least one label":     func(c *Config) { c.Labels = nil; c.Repos[1].Labels = nil },
		"build_cache_keep":             func(c *Config) { c.BuildCacheKeep = "lots" },
		"disk_high_water":              func(c *Config) { c.DiskHighWater = 0 },
		"duplicate repo DarkMem":       func(c *Config) { c.Repos = append(c.Repos, Repo{Name: "DarkMem"}) },
		"darkcloud: warm must be <= max": func(c *Config) {
			one := 1
			c.Repos[0].Max = &one
			c.Repos[0].Warm = &two
		},
		"darkcloud: cleanup_name_prefixes must not contain an empty prefix": func(c *Config) {
			c.Repos[0].CleanupNamePrefixes = []string{"dc-e2e-", " "}
		},
		"darkmem: a repo being removed must stay paused": func(c *Config) { c.Repos[1].Removing = true },
		"runner_limits.memory_max":                       func(c *Config) { c.RunnerLimits.MemoryMax = "6 gigs" },
		"runner_limits.cpu_quota":                        func(c *Config) { c.RunnerLimits.CPUQuota = "2" },
	}
	for want, mutate := range cases {
		c, _, err := Parse([]byte(sample))
		if err != nil {
			t.Fatal(err)
		}
		mutate(c)
		if _, err := c.Validate(); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("want error containing %q, got %v", want, err)
		}
	}
}

func TestValidRunnerLimits(t *testing.T) {
	for _, l := range []RunnerLimits{
		{MemoryMax: "6G", CPUQuota: "200%"},
		{MemoryMax: "512M", CPUQuota: "50%"},
		{MemoryMax: "1073741824", CPUQuota: "100%"},
		{MemoryMax: "50%", CPUQuota: "400%"},
		{MemoryMax: "infinity", CPUQuota: "1%"},
	} {
		c, _, _ := Parse([]byte(sample))
		c.RunnerLimits = l
		if _, err := c.Validate(); err != nil {
			t.Errorf("%+v rejected: %v", l, err)
		}
	}
	for _, l := range []RunnerLimits{
		{MemoryMax: "", CPUQuota: "200%"},
		{MemoryMax: "0", CPUQuota: "200%"},
		{MemoryMax: "6GB", CPUQuota: "200%"},
		{MemoryMax: "150%", CPUQuota: "200%"},
		{MemoryMax: "6G", CPUQuota: ""},
		{MemoryMax: "6G", CPUQuota: "0%"},
		{MemoryMax: "6G", CPUQuota: "2.5"},
	} {
		c, _, _ := Parse([]byte(sample))
		c.RunnerLimits = l
		if _, err := c.Validate(); err == nil {
			t.Errorf("%+v accepted", l)
		}
	}
}

// warm is bounded only by an explicit max: an omitted max is unlimited in
// all mode (the only mode that uses warm), whatever the current mode is.
func TestWarmBoundOnlyByExplicitMax(t *testing.T) {
	two := 2
	for _, mode := range []string{ModeQueue, ModeAll} {
		c, _, _ := Parse([]byte(sample))
		c.Mode = mode
		c.Repos[1].Warm = &two
		if _, err := c.Validate(); err != nil {
			t.Errorf("%s mode, omitted max, warm 2: %v", mode, err)
		}
	}
}

func TestRepoLookupIgnoresCase(t *testing.T) {
	c, _, _ := Parse([]byte(sample))
	if r := c.Repo("DarkCloud"); r == nil || r.Name != "darkcloud" {
		t.Fatalf("Repo(DarkCloud) = %+v", r)
	}
}

func TestRemovingRepoRoundTrips(t *testing.T) {
	c, _, _ := Parse([]byte(sample))
	c.Repos[1].Paused = true
	c.Repos[1].Removing = true
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	back, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if r := back.Repo("darkmem"); !r.Paused || !r.Removing {
		t.Fatalf("removal intent lost: %+v", r)
	}
}

func TestPrefixWarningWhenMaxNotOne(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	two := 2
	c.Repos[0].Max = &two
	warnings, err := c.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "darkcloud") {
		t.Fatalf("warnings = %v", warnings)
	}
}

func TestSaveRoundTrip(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	back, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.HistoryRetention.String() != "30d" || *back.Repo("darkcloud").Max != 1 || back.Repo("darkmem").Max != nil {
		t.Fatalf("round trip lost data: %+v", back)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}

func TestSaveRejectsInvalid(t *testing.T) {
	c, _, _ := Parse([]byte(sample))
	c.GlobalMax = 0
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid config was written")
	}
}

func TestCloneIsDeep(t *testing.T) {
	c, _, _ := Parse([]byte(sample))
	d := c.Clone()
	*d.Repos[0].Max = 5
	d.Labels[0] = "x"
	if *c.Repos[0].Max != 1 || c.Labels[0] != "homelab" {
		t.Fatal("clone shares memory")
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./internal/config/`
Expected: FAIL — FAIL: `undefined: Parse` (and other undefined names)

- [ ] **Step 4: Write `internal/config/config.go`**

```go
// Package config loads, validates and atomically saves /etc/ghr/config.yaml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	ModeQueue = "queue"
	ModeAll   = "all"
)

// SystemLabels are the read-only labels GitHub gives every Linux x64 runner.
var SystemLabels = []string{"self-hosted", "linux", "x64"}

type Config struct {
	Owner            string       `yaml:"owner" json:"owner"`
	Mode             string       `yaml:"mode" json:"mode"`
	GlobalMax        int          `yaml:"global_max" json:"global_max"`
	PollInterval     Duration     `yaml:"poll_interval" json:"poll_interval"`
	StartTimeout     Duration     `yaml:"start_timeout" json:"start_timeout"`
	IdleTimeout      Duration     `yaml:"idle_timeout" json:"idle_timeout"`
	DiskHighWater    int          `yaml:"disk_high_water" json:"disk_high_water"`
	BuildCacheKeep   string       `yaml:"build_cache_keep" json:"build_cache_keep"`
	HistoryRetention Duration     `yaml:"history_retention" json:"history_retention"`
	Labels           []string     `yaml:"labels" json:"labels"`
	RunnerLimits     RunnerLimits `yaml:"runner_limits" json:"runner_limits"`
	Repos            []Repo       `yaml:"repos" json:"repos"`
}

type RunnerLimits struct {
	MemoryMax string `yaml:"memory_max" json:"memory_max"`
	CPUQuota  string `yaml:"cpu_quota" json:"cpu_quota"`
}

type Repo struct {
	Name                string   `yaml:"name" json:"name"`
	Max                 *int     `yaml:"max,omitempty" json:"max,omitempty"`
	Warm                *int     `yaml:"warm,omitempty" json:"warm,omitempty"`
	Labels              []string `yaml:"labels,omitempty" json:"labels,omitempty"`
	CleanupNamePrefixes []string `yaml:"cleanup_name_prefixes,omitempty" json:"cleanup_name_prefixes,omitempty"`
	Paused              bool     `yaml:"paused,omitempty" json:"paused,omitempty"`
	// Removing is daemon-managed: set with Paused by DELETE /repos/{name} so a
	// pending removal survives restarts; a per-repo resume cancels it.
	Removing bool `yaml:"removing,omitempty" json:"removing,omitempty"`
}

// Duration is a time.Duration that also accepts a whole-day suffix ("30d").
type Duration time.Duration

func (d Duration) D() time.Duration { return time.Duration(d) }

func ParseDuration(s string) (Duration, error) {
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid duration %q", s)
		}
		return Duration(time.Duration(n) * 24 * time.Hour), nil
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", s)
	}
	return Duration(v), nil
}

func (d Duration) String() string {
	v := time.Duration(d)
	if v > 0 && v%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", v/(24*time.Hour))
	}
	return v.String()
}

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := ParseDuration(n.Value)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

func (d Duration) MarshalYAML() (any, error) { return d.String(), nil }

func (d Duration) MarshalJSON() ([]byte, error) { return []byte(strconv.Quote(d.String())), nil }

func (d *Duration) UnmarshalJSON(b []byte) error {
	s, err := strconv.Unquote(string(b))
	if err != nil {
		return err
	}
	v, err := ParseDuration(s)
	if err != nil {
		return err
	}
	*d = v
	return nil
}

var (
	sizeRe     = regexp.MustCompile(`^[0-9]+(B|KB|MB|GB|TB)$`)
	memoryRe   = regexp.MustCompile(`^([1-9][0-9]*[KMGT]?|[1-9][0-9]?%|100%|infinity)$`)
	cpuQuotaRe = regexp.MustCompile(`^[1-9][0-9]*%$`)
)

func defaults() *Config {
	return &Config{
		Mode:             ModeQueue,
		GlobalMax:        2,
		PollInterval:     Duration(10 * time.Second),
		StartTimeout:     Duration(2 * time.Minute),
		IdleTimeout:      Duration(5 * time.Minute),
		DiskHighWater:    80,
		BuildCacheKeep:   "20GB",
		HistoryRetention: Duration(30 * 24 * time.Hour),
		RunnerLimits:     RunnerLimits{MemoryMax: "6G", CPUQuota: "200%"},
	}
}

// Parse decodes YAML over the defaults and validates the result.
func Parse(data []byte) (*Config, []string, error) {
	c := defaults()
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, nil, fmt.Errorf("parse config: %w", err)
	}
	warnings, err := c.Validate()
	if err != nil {
		return nil, nil, err
	}
	return c, warnings, nil
}

func Load(path string) (*Config, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return Parse(data)
}

// Save validates c, then writes it atomically (temp file in the same dir + rename).
func Save(path string, c *Config) error {
	if _, err := c.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Clone returns a deep copy.
func (c *Config) Clone() *Config {
	data, err := yaml.Marshal(c)
	if err != nil {
		panic(err)
	}
	out := &Config{}
	if err := yaml.Unmarshal(data, out); err != nil {
		panic(err)
	}
	return out
}

// Repo finds a repo by name, ignoring case: GitHub repository names are case-insensitive.
func (c *Config) Repo(name string) *Repo {
	for i := range c.Repos {
		if strings.EqualFold(c.Repos[i].Name, name) {
			return &c.Repos[i]
		}
	}
	return nil
}

// EffectiveMax returns the repo's cap; 0 means unlimited.
// An omitted max is 1 in queue mode and unlimited in all mode.
func (c *Config) EffectiveMax(r Repo) int {
	if r.Max != nil {
		return *r.Max
	}
	if c.Mode == ModeAll {
		return 0
	}
	return 1
}

func (c *Config) EffectiveWarm(r Repo) int {
	if r.Warm != nil {
		return *r.Warm
	}
	return 1
}

// CustomLabels are the labels ghr registers (config.labels ∪ repo.labels, lower-cased, deduplicated).
func (c *Config) CustomLabels(r Repo) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range append(append([]string{}, c.Labels...), r.Labels...) {
		l = strings.ToLower(strings.TrimSpace(l))
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	return out
}

// EffectiveLabels are the system labels plus CustomLabels.
func (c *Config) EffectiveLabels(r Repo) []string {
	return append(append([]string{}, SystemLabels...), c.CustomLabels(r)...)
}

// Validate returns non-fatal warnings, or an error describing every violation.
func (c *Config) Validate() ([]string, error) {
	var errs []string
	var warnings []string
	if strings.TrimSpace(c.Owner) == "" {
		errs = append(errs, "owner is required")
	}
	if c.Mode != ModeQueue && c.Mode != ModeAll {
		errs = append(errs, fmt.Sprintf("mode must be %q or %q", ModeQueue, ModeAll))
	}
	if c.GlobalMax < 1 {
		errs = append(errs, "global_max must be >= 1")
	}
	for name, d := range map[string]Duration{
		"poll_interval": c.PollInterval, "start_timeout": c.StartTimeout,
		"idle_timeout": c.IdleTimeout, "history_retention": c.HistoryRetention,
	} {
		if d <= 0 {
			errs = append(errs, name+" must be > 0")
		}
	}
	if c.DiskHighWater < 1 || c.DiskHighWater > 100 {
		errs = append(errs, "disk_high_water must be 1..100")
	}
	if !sizeRe.MatchString(c.BuildCacheKeep) {
		errs = append(errs, "build_cache_keep must look like 20GB")
	}
	if !memoryRe.MatchString(c.RunnerLimits.MemoryMax) {
		errs = append(errs, "runner_limits.memory_max must be bytes with an optional K/M/G/T suffix (6G), a percentage (50%) or infinity")
	}
	if !cpuQuotaRe.MatchString(c.RunnerLimits.CPUQuota) {
		errs = append(errs, "runner_limits.cpu_quota must be a positive percentage such as 200%")
	}
	seen := map[string]bool{}
	for _, r := range c.Repos {
		if strings.TrimSpace(r.Name) == "" {
			errs = append(errs, "repo name is required")
			continue
		}
		key := strings.ToLower(r.Name)
		if seen[key] {
			errs = append(errs, "duplicate repo "+r.Name+" (names are case-insensitive)")
		}
		seen[key] = true
		if r.Max != nil && *r.Max < 0 {
			errs = append(errs, r.Name+": max must be >= 0")
		}
		if r.Warm != nil && *r.Warm < 0 {
			errs = append(errs, r.Name+": warm must be >= 0")
		}
		// warm only applies in all mode, where an omitted max is unlimited, so
		// only an explicit max > 0 bounds it, whatever the current mode is.
		if r.Max != nil && *r.Max > 0 && c.EffectiveWarm(r) > *r.Max {
			errs = append(errs, r.Name+": warm must be <= max")
		}
		for _, p := range r.CleanupNamePrefixes {
			if strings.TrimSpace(p) == "" {
				errs = append(errs, r.Name+": cleanup_name_prefixes must not contain an empty prefix")
			}
		}
		if r.Removing && !r.Paused {
			errs = append(errs, r.Name+": a repo being removed must stay paused")
		}
		if len(c.CustomLabels(r)) == 0 {
			errs = append(errs, r.Name+": needs at least one label in labels or repo labels")
		}
		if len(r.CleanupNamePrefixes) > 0 && c.EffectiveMax(r) != 1 {
			warnings = append(warnings, r.Name+": cleanup_name_prefixes can remove a concurrent job's containers when max is not 1")
		}
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(sortedUnique(errs), "; "))
	}
	return warnings, nil
}

func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 5: Run the check**

Run: `go test -count=1 -timeout 180s ./internal/config/`
Expected: PASS (exit 0).

- [ ] **Step 6: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add internal/config/config_test.go internal/config/config.go go.mod go.sum
git commit -m "feat(config): load, validate and save config"
```

---

### Task 4: Scheduler

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `internal/sched/sched.go`
- Test: `internal/sched/sched_test.go`

**Interfaces:**
- Consumes: `config.Config`, `EffectiveMax`, `EffectiveWarm`, `Repo` (Contracts → config).
- Produces: package `sched` (Contracts → sched).

**Items:** 2, 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test `internal/sched/sched_test.go`**

```go
package sched

import (
	"reflect"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
)

var t0 = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

func ptr(n int) *int { return &n }

func cfg(mode string, globalMax int, repos ...config.Repo) *config.Config {
	return &config.Config{
		Owner: "o", Mode: mode, GlobalMax: globalMax, Labels: []string{"homelab"},
		IdleTimeout: config.Duration(5 * time.Minute), StartTimeout: config.Duration(2 * time.Minute),
		Repos: repos,
	}
}

func jobs(repo string, minutesAgo ...int) []QueuedJob {
	var out []QueuedJob
	for i, m := range minutesAgo {
		out = append(out, QueuedJob{Repo: repo, ID: int64(i + 1), CreatedAt: t0.Add(-time.Duration(m) * time.Minute)})
	}
	return out
}

func repos(spawns []Spawn) []string {
	out := []string{}
	for _, s := range spawns {
		out = append(out, s.Repo)
	}
	return out
}

func TestPlan(t *testing.T) {
	a := config.Repo{Name: "a"}
	b := config.Repo{Name: "b"}
	tests := []struct {
		name   string
		cfg    *config.Config
		insts  []Instance
		demand Demand
		want   []string
	}{
		{"queue: global cap stops spawning", cfg("queue", 2, config.Repo{Name: "a", Max: ptr(0)}), nil,
			Demand{"a": jobs("a", 3, 2, 1)}, []string{"a", "a"}},
		{"queue: repo default max 1", cfg("queue", 5, a), nil,
			Demand{"a": jobs("a", 3, 2)}, []string{"a"}},
		{"queue: cross-repo FIFO", cfg("queue", 1, a, b), nil,
			Demand{"a": jobs("a", 1), "b": jobs("b", 9)}, []string{"b"}},
		{"queue: capped head is skipped, next repo served", cfg("queue", 3, a, b),
			[]Instance{{ID: "1", Repo: "a", State: Busy, StateSince: t0.Add(-time.Hour), JobConfirmed: true}},
			Demand{"a": jobs("a", 9), "b": jobs("b", 1)}, []string{"b"}},
		{"queue: idle instance covers a job", cfg("queue", 3, config.Repo{Name: "a", Max: ptr(3)}),
			[]Instance{{ID: "1", Repo: "a", State: Idle, StateSince: t0}},
			Demand{"a": jobs("a", 2, 1)}, []string{"a"}},
		{"queue: unconfirmed busy covers its job (no extra spawn)", cfg("queue", 3, config.Repo{Name: "a", Max: ptr(3)}),
			[]Instance{{ID: "1", Repo: "a", State: Busy, StateSince: t0.Add(-10 * time.Second)}},
			Demand{"a": jobs("a", 1)}, []string{}},
		{"queue: unconfirmed busy stops covering after grace", cfg("queue", 3, config.Repo{Name: "a", Max: ptr(3)}),
			[]Instance{{ID: "1", Repo: "a", State: Busy, StateSince: t0.Add(-2 * time.Minute)}},
			Demand{"a": jobs("a", 1)}, []string{"a"}},
		{"queue: cleaning counts toward repo cap", cfg("queue", 3, a),
			[]Instance{{ID: "1", Repo: "a", State: Cleaning, StateSince: t0}},
			Demand{"a": jobs("a", 1)}, []string{}},
		{"queue: cleaning does not count toward global cap", cfg("queue", 1, a, b),
			[]Instance{{ID: "1", Repo: "a", State: Cleaning, StateSince: t0}},
			Demand{"b": jobs("b", 1)}, []string{"b"}},
		{"queue: paused repo ignored", cfg("queue", 3, config.Repo{Name: "a", Paused: true}), nil,
			Demand{"a": jobs("a", 1)}, []string{}},
		{"all: warm runner kept without demand", cfg("all", 1, a, b), nil,
			Demand{}, []string{"a", "b"}},
		{"all: no global cap, unlimited by default", cfg("all", 1, config.Repo{Name: "a", Warm: ptr(0)}), nil,
			Demand{"a": jobs("a", 3, 2, 1)}, []string{"a", "a", "a"}},
		{"all: explicit max limits", cfg("all", 1, config.Repo{Name: "a", Max: ptr(2), Warm: ptr(1)}),
			[]Instance{{ID: "1", Repo: "a", State: Busy, StateSince: t0.Add(-time.Hour), JobConfirmed: true}},
			Demand{"a": jobs("a", 3, 2)}, []string{"a"}},
		{"all: warm satisfied by idle", cfg("all", 1, a),
			[]Instance{{ID: "1", Repo: "a", State: Idle, StateSince: t0}},
			Demand{}, []string{}},
		{"all: stale idle neither covers nor counts as warm", cfg("all", 1, a),
			[]Instance{{ID: "1", Repo: "a", State: Idle, StateSince: t0, Stale: true}},
			Demand{"a": jobs("a", 1)}, []string{"a"}},
		{"queue: stale idle does not cover but holds the repo cap", cfg("queue", 3, config.Repo{Name: "a", Max: ptr(2)}),
			[]Instance{{ID: "1", Repo: "a", State: Idle, StateSince: t0, Stale: true}},
			Demand{"a": jobs("a", 2, 1)}, []string{"a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := repos(Plan(tc.cfg, tc.insts, tc.demand, t0))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMatchLabels(t *testing.T) {
	eff := []string{"self-hosted", "linux", "x64", "homelab"}
	if !MatchLabels([]string{"Self-Hosted", "Linux", "X64"}, eff) {
		t.Fatal("system labels should match case-insensitively")
	}
	if MatchLabels([]string{"ubuntu-latest"}, eff) {
		t.Fatal("hosted label must not match")
	}
}

func TestIdleToStop(t *testing.T) {
	idle := func(id, repo string, ago time.Duration) Instance {
		return Instance{ID: id, Repo: repo, State: Idle, StateSince: t0.Add(-ago)}
	}
	q := cfg("queue", 2, config.Repo{Name: "a"}, config.Repo{Name: "p", Paused: true})
	got := IdleToStop(q, []Instance{idle("old", "a", 6*time.Minute), idle("new", "a", time.Minute), idle("pz", "p", 0)}, t0)
	if !reflect.DeepEqual(got, []string{"old", "pz"}) {
		t.Fatalf("queue: got %v", got)
	}
	al := cfg("all", 2, config.Repo{Name: "a", Warm: ptr(1)})
	got = IdleToStop(al, []Instance{idle("warm", "a", time.Hour), idle("extra", "a", 10*time.Minute), idle("fresh", "a", time.Minute)}, t0)
	if !reflect.DeepEqual(got, []string{"extra"}) {
		t.Fatalf("all: got %v", got)
	}
	got = IdleToStop(al, []Instance{idle("warm", "a", time.Hour), idle("older", "a", 20*time.Minute), idle("newer", "a", 10*time.Minute)}, t0)
	if !reflect.DeepEqual(got, []string{"newer", "older"}) {
		t.Fatalf("all: surplus must stop newest first, got %v", got)
	}
	stale := idle("stale", "a", 0)
	stale.Stale = true
	got = IdleToStop(al, []Instance{idle("warm", "a", time.Hour), stale}, t0)
	if !reflect.DeepEqual(got, []string{"stale"}) {
		t.Fatalf("stale idle must stop at once, got %v", got)
	}
	got = IdleToStop(q, []Instance{idle("gone", "removed-repo", 0)}, t0)
	if !reflect.DeepEqual(got, []string{"gone"}) {
		t.Fatalf("unconfigured repo: got %v", got)
	}
}

func TestStartTimedOut(t *testing.T) {
	c := cfg("queue", 2, config.Repo{Name: "a"})
	got := StartTimedOut(c, []Instance{
		{ID: "late", Repo: "a", State: Starting, StateSince: t0.Add(-3 * time.Minute)},
		{ID: "ok", Repo: "a", State: Starting, StateSince: t0.Add(-time.Minute)},
		{ID: "idle", Repo: "a", State: Idle, StateSince: t0.Add(-time.Hour)},
	}, t0)
	if !reflect.DeepEqual(got, []string{"late"}) {
		t.Fatalf("got %v", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./internal/sched/`
Expected: FAIL — FAIL: `undefined: Plan`

- [ ] **Step 3: Write `internal/sched/sched.go`**

```go
// Package sched holds the pure scheduling decisions: what to spawn and what to stop.
package sched

import (
	"sort"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/config"
)

type State string

const (
	Starting State = "starting"
	Idle     State = "idle"
	Busy     State = "busy"
	Cleaning State = "cleaning"
)

// BusyCoverGrace bounds how long a busy instance whose job the API has not yet
// reported in_progress keeps covering a queued job.
const BusyCoverGrace = 60 * time.Second

type Instance struct {
	ID           string
	Repo         string
	State        State
	StateSince   time.Time
	JobConfirmed bool
	// Stale marks a runner registered with labels that differ from its repo's
	// current labels: it may be unable to take the jobs demand now counts.
	Stale bool
}

type QueuedJob struct {
	Repo      string
	ID        int64
	CreatedAt time.Time
}

// Demand maps repo name to its matching queued jobs.
type Demand map[string][]QueuedJob

type Spawn struct {
	Repo string
}

// MatchLabels reports whether every job label is in the runner's effective set (case-insensitive).
func MatchLabels(jobLabels, effective []string) bool {
	set := map[string]bool{}
	for _, l := range effective {
		set[strings.ToLower(l)] = true
	}
	for _, l := range jobLabels {
		if !set[strings.ToLower(l)] {
			return false
		}
	}
	return true
}

func covers(i Instance, now time.Time) bool {
	if i.Stale {
		return false
	}
	switch i.State {
	case Starting, Idle:
		return true
	case Busy:
		return !i.JobConfirmed && now.Sub(i.StateSince) < BusyCoverGrace
	}
	return false
}

// Covering counts instances of repo expected to take a queued job.
func Covering(insts []Instance, repo string, now time.Time) int {
	n := 0
	for _, i := range insts {
		if i.Repo == repo && covers(i, now) {
			n++
		}
	}
	return n
}

// Uncovered returns the repo's queued jobs (oldest first) not covered by an instance.
func Uncovered(insts []Instance, repo string, jobs []QueuedJob, now time.Time) []QueuedJob {
	sorted := append([]QueuedJob{}, jobs...)
	sort.SliceStable(sorted, func(a, b int) bool { return sorted[a].CreatedAt.Before(sorted[b].CreatedAt) })
	c := Covering(insts, repo, now)
	if c >= len(sorted) {
		return nil
	}
	return sorted[c:]
}

func repoActive(insts []Instance, repo string) int {
	n := 0
	for _, i := range insts {
		if i.Repo == repo {
			n++ // starting, idle, busy and cleaning all count toward the repo cap
		}
	}
	return n
}

func totalActive(insts []Instance) int {
	n := 0
	for _, i := range insts {
		if i.State != Cleaning {
			n++
		}
	}
	return n
}

// countStates counts the repo's non-stale instances in the given states.
func countStates(insts []Instance, repo string, states ...State) int {
	n := 0
	for _, i := range insts {
		if i.Repo != repo || i.Stale {
			continue
		}
		for _, s := range states {
			if i.State == s {
				n++
			}
		}
	}
	return n
}

// Plan decides which runners to spawn this tick.
func Plan(cfg *config.Config, insts []Instance, demand Demand, now time.Time) []Spawn {
	if cfg.Mode == config.ModeAll {
		return planAll(cfg, insts, demand, now)
	}
	return planQueue(cfg, insts, demand, now)
}

func planQueue(cfg *config.Config, insts []Instance, demand Demand, now time.Time) []Spawn {
	var cands []QueuedJob
	for _, r := range cfg.Repos {
		if r.Paused {
			continue
		}
		for _, j := range Uncovered(insts, r.Name, demand[r.Name], now) {
			j.Repo = r.Name
			cands = append(cands, j)
		}
	}
	sort.SliceStable(cands, func(a, b int) bool { return cands[a].CreatedAt.Before(cands[b].CreatedAt) })
	active := map[string]int{}
	for _, r := range cfg.Repos {
		active[r.Name] = repoActive(insts, r.Name)
	}
	total := totalActive(insts)
	var out []Spawn
	for _, j := range cands {
		if total >= cfg.GlobalMax {
			break
		}
		max := cfg.EffectiveMax(*cfg.Repo(j.Repo))
		if max > 0 && active[j.Repo] >= max {
			continue
		}
		out = append(out, Spawn{Repo: j.Repo})
		active[j.Repo]++
		total++
	}
	return out
}

func planAll(cfg *config.Config, insts []Instance, demand Demand, now time.Time) []Spawn {
	var out []Spawn
	for _, r := range cfg.Repos {
		if r.Paused {
			continue
		}
		uncovered := len(Uncovered(insts, r.Name, demand[r.Name], now))
		warmNeed := cfg.EffectiveWarm(r) - countStates(insts, r.Name, Starting, Idle)
		n := uncovered
		if warmNeed > n {
			n = warmNeed
		}
		if max := cfg.EffectiveMax(r); max > 0 && n > max-repoActive(insts, r.Name) {
			n = max - repoActive(insts, r.Name)
		}
		for ; n > 0; n-- {
			out = append(out, Spawn{Repo: r.Name})
		}
	}
	return out
}

// IdleToStop returns the IDs of idle instances to stop: past idle_timeout in queue mode,
// past idle_timeout and beyond the warm count in all mode (newest first), and every
// stale idle instance or idle instance of a paused (or unconfigured) repo.
func IdleToStop(cfg *config.Config, insts []Instance, now time.Time) []string {
	byRepo := map[string][]Instance{}
	var out []string
	for _, i := range insts {
		if i.State != Idle {
			continue
		}
		if i.Stale {
			out = append(out, i.ID)
			continue
		}
		byRepo[i.Repo] = append(byRepo[i.Repo], i)
	}
	timeout := cfg.IdleTimeout.D()
	repos := make([]string, 0, len(byRepo))
	for name := range byRepo {
		repos = append(repos, name)
	}
	sort.Strings(repos)
	for _, name := range repos {
		idle := byRepo[name]
		sort.SliceStable(idle, func(a, b int) bool { return idle[a].StateSince.Before(idle[b].StateSince) })
		r := cfg.Repo(name)
		if r == nil || r.Paused {
			for _, i := range idle {
				out = append(out, i.ID)
			}
			continue
		}
		keep := 0
		if cfg.Mode == config.ModeAll {
			keep = cfg.EffectiveWarm(*r)
		}
		// The oldest idle instances are the warm ones; surplus stops newest first.
		for idx := len(idle) - 1; idx >= keep; idx-- {
			if now.Sub(idle[idx].StateSince) >= timeout {
				out = append(out, idle[idx].ID)
			}
		}
	}
	return out
}

// StartTimedOut returns the IDs of instances stuck in starting past start_timeout.
func StartTimedOut(cfg *config.Config, insts []Instance, now time.Time) []string {
	var out []string
	for _, i := range insts {
		if i.State == Starting && now.Sub(i.StateSince) >= cfg.StartTimeout.D() {
			out = append(out, i.ID)
		}
	}
	return out
}
```

- [ ] **Step 4: Run the check**

Run: `go test -count=1 -timeout 180s ./internal/sched/`
Expected: PASS (exit 0).

- [ ] **Step 5: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/sched/sched_test.go internal/sched/sched.go
git commit -m "feat(sched): add pure spawn and stop decisions"
```

---

### Task 5: GitHub REST client

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `internal/github/client.go`
- Test: `internal/github/client_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: package `github` (Contracts → github).

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing test `internal/github/client_test.go`**

```go
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeGH struct {
	mu       sync.Mutex
	requests []string
	handler  func(w http.ResponseWriter, r *http.Request)
}

func newClient(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) (*Client, *fakeGH) {
	t.Helper()
	f := &fakeGH{handler: h}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
		f.mu.Unlock()
		f.handler(w, r)
	}))
	t.Cleanup(srv.Close)
	c := New("darkraise", func() string { return "tok" })
	c.BaseURL = srv.URL
	c.HTTP = srv.Client()
	return c, f
}

func TestListRunsPaginates(t *testing.T) {
	var base string
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing auth header")
		}
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s%s&page=2>; rel="next"`, base, r.URL.RequestURI()))
			fmt.Fprint(w, `{"workflow_runs":[{"id":1,"status":"queued"}]}`)
			return
		}
		fmt.Fprint(w, `{"workflow_runs":[{"id":2,"status":"queued"}]}`)
	})
	base = c.BaseURL
	runs, err := c.ListRuns(context.Background(), "darkcloud", "queued")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[1].ID != 2 {
		t.Fatalf("runs = %+v", runs)
	}
	if !strings.Contains(f.requests[0], "/repos/darkraise/darkcloud/actions/runs?per_page=100&status=queued") {
		t.Fatalf("request = %s", f.requests[0])
	}
}

func TestETagRevalidation(t *testing.T) {
	calls := 0
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		fmt.Fprint(w, `{"jobs":[{"id":7,"status":"queued","labels":["self-hosted"]}]}`)
	})
	for i := 0; i < 2; i++ {
		jobs, err := c.ListJobs(context.Background(), "r", 5)
		if err != nil {
			t.Fatal(err)
		}
		if len(jobs) != 1 || jobs[0].ID != 7 {
			t.Fatalf("pass %d: jobs = %+v", i, jobs)
		}
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestClassify(t *testing.T) {
	reset := time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		status  int
		headers map[string]string
		kind    ErrKind
		retryAt time.Time
	}{
		{"401", 401, nil, ErrAuth, time.Time{}},
		{"403 no rate headers", 403, map[string]string{"X-RateLimit-Remaining": "4000"}, ErrAuth, time.Time{}},
		{"403 primary limit", 403, map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": fmt.Sprint(reset.Unix())}, ErrRateLimit, reset},
		{"429 retry-after", 429, map[string]string{"Retry-After": "30"}, ErrRateLimit, time.Date(2026, 10, 3, 12, 0, 30, 0, time.UTC)},
		{"404", 404, nil, ErrNotFound, time.Time{}},
		{"422", 422, nil, ErrUnprocessable, time.Time{}},
		{"502", 502, nil, ErrServer, time.Time{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, `{"message":"nope"}`)
			})
			c.Now = func() time.Time { return time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC) }
			_, err := c.GetRepo(context.Background(), "r")
			if !IsKind(err, tc.kind) {
				t.Fatalf("err = %v, want kind %d", err, tc.kind)
			}
			if !tc.retryAt.IsZero() && !err.(*APIError).RetryAt.Equal(tc.retryAt) {
				t.Fatalf("retryAt = %v, want %v", err.(*APIError).RetryAt, tc.retryAt)
			}
		})
	}
}

func TestGenerateJITConfigBodyAndSpacing(t *testing.T) {
	var bodies []map[string]any
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		bodies = append(bodies, m)
		w.WriteHeader(201)
		fmt.Fprint(w, `{"runner":{"id":42,"name":"ghr-r-abc123"},"encoded_jit_config":"ENC"}`)
	})
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	var slept []time.Duration
	c.Now = func() time.Time { return now }
	c.Sleep = func(d time.Duration) { slept = append(slept, d); now = now.Add(d) }
	for i := 0; i < 2; i++ {
		j, err := c.GenerateJITConfig(context.Background(), "r", "ghr-r-abc123", []string{"homelab"})
		if err != nil {
			t.Fatal(err)
		}
		if j.Runner.ID != 42 || j.EncodedJITConfig != "ENC" {
			t.Fatalf("jit = %+v", j)
		}
	}
	if bodies[0]["runner_group_id"] != float64(1) || bodies[0]["work_folder"] != "_work" || bodies[0]["name"] != "ghr-r-abc123" {
		t.Fatalf("body = %v", bodies[0])
	}
	if len(slept) != 1 || slept[0] != time.Second {
		t.Fatalf("second mutating call should wait 1s, slept %v", slept)
	}
}

func TestDeleteRunner404IsSuccess(t *testing.T) {
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	c.Sleep = func(time.Duration) {}
	if err := c.DeleteRunner(context.Background(), "r", 9); err != nil {
		t.Fatal(err)
	}
	if f.requests[0] != "DELETE /repos/darkraise/r/actions/runners/9" {
		t.Fatalf("request = %s", f.requests[0])
	}
}

func TestRateRemainingTracked(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "4812")
		fmt.Fprint(w, `{"full_name":"darkraise/r","private":true}`)
	})
	if c.RateRemaining() != -1 {
		t.Fatal("expected -1 before any request")
	}
	repo, err := c.GetRepo(context.Background(), "r")
	if err != nil || !repo.Private {
		t.Fatalf("repo = %+v err = %v", repo, err)
	}
	if c.RateRemaining() != 4812 {
		t.Fatalf("remaining = %d", c.RateRemaining())
	}
}

func TestETag304KeepsPagination(t *testing.T) {
	var base string
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if r.Header.Get("If-None-Match") == `"p`+page+`"` {
			w.WriteHeader(http.StatusNotModified) // no Link header on revalidation
			return
		}
		w.Header().Set("ETag", `"p`+page+`"`)
		if page == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s%s&page=2>; rel="next"`, base, r.URL.RequestURI()))
			fmt.Fprint(w, `{"workflow_runs":[{"id":1}]}`)
			return
		}
		fmt.Fprint(w, `{"workflow_runs":[{"id":2}]}`)
	})
	base = c.BaseURL
	for pass := 0; pass < 2; pass++ {
		runs, err := c.ListRuns(context.Background(), "r", "queued")
		if err != nil {
			t.Fatal(err)
		}
		if len(runs) != 2 {
			t.Fatalf("pass %d: runs = %+v", pass, runs)
		}
	}
}

func TestRateLimitSuspendsEveryRequest(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	limited := true
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if limited {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprint(now.Add(time.Minute).Unix()))
			w.WriteHeader(403)
			return
		}
		w.WriteHeader(204)
	})
	c.Now = func() time.Time { return now }
	c.Sleep = func(d time.Duration) { now = now.Add(d) }
	if _, err := c.GetRepo(context.Background(), "r"); !IsKind(err, ErrRateLimit) {
		t.Fatalf("first call: %v", err)
	}
	if got := c.SuspendedUntil(); !got.Equal(now.Add(time.Minute)) {
		t.Fatalf("SuspendedUntil = %v", got)
	}
	limited = false
	if _, err := c.ListRunners(context.Background(), "r"); !IsKind(err, ErrRateLimit) {
		t.Fatalf("GET during suspension: %v", err)
	}
	if err := c.DeleteRunner(context.Background(), "r", 1); !IsKind(err, ErrRateLimit) {
		t.Fatalf("DELETE during suspension: %v", err)
	}
	if len(f.requests) != 1 {
		t.Fatalf("requests sent during suspension: %v", f.requests)
	}
	now = now.Add(time.Minute)
	if !c.SuspendedUntil().IsZero() {
		t.Fatal("suspension should end at RetryAt")
	}
	if err := c.DeleteRunner(context.Background(), "r", 1); err != nil {
		t.Fatalf("after RetryAt: %v", err)
	}
	if len(f.requests) != 2 {
		t.Fatalf("requests = %v", f.requests)
	}
}

func TestMutationFailuresClassified(t *testing.T) {
	for _, tc := range []struct {
		status int
		kind   ErrKind
	}{{422, ErrUnprocessable}, {500, ErrServer}, {401, ErrAuth}} {
		c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			fmt.Fprint(w, `{"message":"no"}`)
		})
		c.Sleep = func(time.Duration) {}
		if _, err := c.GenerateJITConfig(context.Background(), "r", "ghr-r-abc123", []string{"homelab"}); !IsKind(err, tc.kind) {
			t.Errorf("JIT %d: err = %v", tc.status, err)
		}
		if err := c.DeleteRunner(context.Background(), "r", 9); !IsKind(err, tc.kind) {
			t.Errorf("DELETE %d: err = %v", tc.status, err)
		}
	}
}

func TestConcurrentMutationsAreSerialAndSpaced(t *testing.T) {
	var mu sync.Mutex
	inFlight, maxInFlight := 0, 0
	var starts []time.Time
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		if inFlight > maxInFlight {
			maxInFlight = inFlight
		}
		starts = append(starts, now)
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		if r.Method == http.MethodPost {
			w.WriteHeader(201)
			fmt.Fprint(w, `{"runner":{"id":1},"encoded_jit_config":"E"}`)
			return
		}
		w.WriteHeader(204)
	})
	c.Now = clock
	c.Sleep = func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 2; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := c.GenerateJITConfig(context.Background(), "r", "ghr-r-abc123", []string{"homelab"})
			errs <- err
		}()
		go func() { defer wg.Done(); errs <- c.DeleteRunner(context.Background(), "r", 1) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if maxInFlight != 1 {
		t.Fatalf("mutating calls overlapped: max in flight %d", maxInFlight)
	}
	for i := 1; i < len(starts); i++ {
		if gap := starts[i].Sub(starts[i-1]); gap < time.Second {
			t.Fatalf("calls %d and %d only %v apart", i-1, i, gap)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./internal/github/`
Expected: FAIL — FAIL: `undefined: New`

- [ ] **Step 3: Write `internal/github/client.go`**

```go
// Package github is a minimal GitHub REST client for runner management.
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"sync"
	"time"
)

type ErrKind int

const (
	ErrOther ErrKind = iota
	ErrAuth
	ErrRateLimit
	ErrNotFound
	ErrUnprocessable
	ErrServer
)

type APIError struct {
	Status  int
	Kind    ErrKind
	Message string
	RetryAt time.Time // set for ErrRateLimit
}

func (e *APIError) Error() string { return fmt.Sprintf("github: %d %s", e.Status, e.Message) }

type Run struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

type Step struct {
	Number     int    `json:"number"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

type Job struct {
	ID           int64      `json:"id"`
	RunID        int64      `json:"run_id"`
	Name         string     `json:"name"`
	WorkflowName string     `json:"workflow_name"`
	Status       string     `json:"status"`
	Conclusion   string     `json:"conclusion"`
	Labels       []string   `json:"labels"`
	RunnerName   string     `json:"runner_name"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at"`
	CompletedAt  *time.Time `json:"completed_at"`
	HTMLURL      string     `json:"html_url"`
	Steps        []Step     `json:"steps"`
}

type Runner struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"` // online | offline
	Busy   bool   `json:"busy"`
}

type Repository struct {
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
}

type JITConfig struct {
	Runner           Runner `json:"runner"`
	EncodedJITConfig string `json:"encoded_jit_config"`
}

const maxCacheEntries = 1000

type cached struct {
	etag string
	body []byte
	next string // a 304 may omit Link, so the page's next link is cached with its body
}

type Client struct {
	BaseURL string // https://api.github.com
	Owner   string
	Token   func() string
	HTTP    *http.Client
	Now     func() time.Time
	Sleep   func(time.Duration)

	mu         sync.Mutex
	cache      map[string]cached
	remaining  int
	retryAt    time.Time
	mutMu      sync.Mutex
	lastMutate time.Time
}

func New(owner string, token func() string) *Client {
	return &Client{
		BaseURL: "https://api.github.com", Owner: owner, Token: token,
		HTTP: &http.Client{Timeout: 30 * time.Second}, Now: time.Now, Sleep: time.Sleep,
		remaining: -1,
	}
}

// RateRemaining is the last seen x-ratelimit-remaining, or -1 before any request.
func (c *Client) RateRemaining() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.remaining
}

// ForgetCache drops ETag state and any rate-limit suspension, used after the token changes.
func (c *Client) ForgetCache() {
	c.mu.Lock()
	c.cache = nil
	c.retryAt = time.Time{}
	c.mu.Unlock()
}

// SuspendedUntil is the time before which every request fails fast with
// ErrRateLimit, or the zero time when requests are allowed.
func (c *Client) SuspendedUntil() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Now().Before(c.retryAt) {
		return c.retryAt
	}
	return time.Time{}
}

func (c *Client) repoURL(repo, rest string) string {
	return fmt.Sprintf("%s/repos/%s/%s%s", c.BaseURL, url.PathEscape(c.Owner), url.PathEscape(repo), rest)
}

var nextLinkRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// do performs one request; GETs use ETag revalidation. It returns body and the next-page URL.
func (c *Client) do(ctx context.Context, method, u string, body any) ([]byte, string, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, "", err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Authorization", "Bearer "+c.Token())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	c.mu.Lock()
	prev, hasPrev := c.cache[u]
	retryAt := c.retryAt
	c.mu.Unlock()
	if c.Now().Before(retryAt) {
		return nil, "", &APIError{Status: http.StatusTooManyRequests, Kind: ErrRateLimit, Message: "rate limited until " + retryAt.Format(time.RFC3339), RetryAt: retryAt}
	}
	if method == http.MethodGet && hasPrev {
		req.Header.Set("If-None-Match", prev.etag)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if rem := resp.Header.Get("X-RateLimit-Remaining"); rem != "" {
		if n, err := strconv.Atoi(rem); err == nil {
			c.mu.Lock()
			c.remaining = n
			c.mu.Unlock()
		}
	}
	next := ""
	if m := nextLinkRe.FindStringSubmatch(resp.Header.Get("Link")); m != nil {
		next = m[1]
	}
	if resp.StatusCode == http.StatusNotModified && hasPrev {
		return prev.body, prev.next, nil
	}
	if resp.StatusCode >= 300 {
		err := c.classify(resp, data)
		if err.Kind == ErrRateLimit {
			c.mu.Lock()
			if err.RetryAt.After(c.retryAt) {
				c.retryAt = err.RetryAt
			}
			c.mu.Unlock()
		}
		return nil, "", err
	}
	if method == http.MethodGet {
		if etag := resp.Header.Get("ETag"); etag != "" {
			c.mu.Lock()
			// Run and job URLs are unbounded over time; reset rather than track LRU.
			if c.cache == nil || len(c.cache) >= maxCacheEntries {
				c.cache = map[string]cached{}
			}
			c.cache[u] = cached{etag: etag, body: data, next: next}
			c.mu.Unlock()
		}
	}
	return data, next, nil
}

func (c *Client) classify(resp *http.Response, data []byte) *APIError {
	var msg struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(data, &msg)
	e := &APIError{Status: resp.StatusCode, Message: msg.Message}
	switch {
	case resp.StatusCode == 401:
		e.Kind = ErrAuth
	case resp.StatusCode == 403 || resp.StatusCode == 429:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			e.Kind = ErrRateLimit
			if reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
				e.RetryAt = time.Unix(reset, 0)
			} else {
				e.RetryAt = c.Now().Add(time.Minute)
			}
		} else if ra := resp.Header.Get("Retry-After"); ra != "" {
			e.Kind = ErrRateLimit
			secs, _ := strconv.Atoi(ra)
			e.RetryAt = c.Now().Add(time.Duration(secs) * time.Second)
		} else if resp.StatusCode == 429 {
			e.Kind = ErrRateLimit
			e.RetryAt = c.Now().Add(time.Minute)
		} else {
			e.Kind = ErrAuth
		}
	case resp.StatusCode == 404:
		e.Kind = ErrNotFound
	case resp.StatusCode == 422:
		e.Kind = ErrUnprocessable
	case resp.StatusCode >= 500:
		e.Kind = ErrServer
	}
	return e
}

// mutate serialises state-changing calls and spaces them at least 1s apart.
func (c *Client) mutate(ctx context.Context, method, u string, body any) ([]byte, error) {
	c.mutMu.Lock()
	defer c.mutMu.Unlock()
	if wait := c.lastMutate.Add(time.Second).Sub(c.Now()); wait > 0 {
		c.Sleep(wait)
	}
	data, _, err := c.do(ctx, method, u, body)
	c.lastMutate = c.Now()
	return data, err
}

func (c *Client) getAll(ctx context.Context, u string, each func([]byte) error) error {
	for u != "" {
		data, next, err := c.do(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		if err := each(data); err != nil {
			return err
		}
		u = next
	}
	return nil
}

// ListRuns lists workflow runs with the given status (queued, in_progress, waiting).
func (c *Client) ListRuns(ctx context.Context, repo, status string) ([]Run, error) {
	var out []Run
	err := c.getAll(ctx, c.repoURL(repo, "/actions/runs?per_page=100&status="+url.QueryEscape(status)), func(b []byte) error {
		var page struct {
			WorkflowRuns []Run `json:"workflow_runs"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		out = append(out, page.WorkflowRuns...)
		return nil
	})
	return out, err
}

// ListJobs lists the latest attempt's jobs of a run.
func (c *Client) ListJobs(ctx context.Context, repo string, runID int64) ([]Job, error) {
	var out []Job
	err := c.getAll(ctx, c.repoURL(repo, fmt.Sprintf("/actions/runs/%d/jobs?filter=latest&per_page=100", runID)), func(b []byte) error {
		var page struct {
			Jobs []Job `json:"jobs"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		out = append(out, page.Jobs...)
		return nil
	})
	return out, err
}

func (c *Client) ListRunners(ctx context.Context, repo string) ([]Runner, error) {
	var out []Runner
	err := c.getAll(ctx, c.repoURL(repo, "/actions/runners?per_page=100"), func(b []byte) error {
		var page struct {
			Runners []Runner `json:"runners"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		out = append(out, page.Runners...)
		return nil
	})
	return out, err
}

func (c *Client) GetRunner(ctx context.Context, repo string, id int64) (*Runner, error) {
	data, _, err := c.do(ctx, http.MethodGet, c.repoURL(repo, fmt.Sprintf("/actions/runners/%d", id)), nil)
	if err != nil {
		return nil, err
	}
	var r Runner
	return &r, json.Unmarshal(data, &r)
}

// DeleteRunner removes a registration; an already-removed runner (404) is success.
func (c *Client) DeleteRunner(ctx context.Context, repo string, id int64) error {
	_, err := c.mutate(ctx, http.MethodDelete, c.repoURL(repo, fmt.Sprintf("/actions/runners/%d", id)), nil)
	if IsKind(err, ErrNotFound) {
		return nil
	}
	return err
}

func (c *Client) GenerateJITConfig(ctx context.Context, repo, name string, labels []string) (*JITConfig, error) {
	body := map[string]any{"name": name, "runner_group_id": 1, "labels": labels, "work_folder": "_work"}
	data, err := c.mutate(ctx, http.MethodPost, c.repoURL(repo, "/actions/runners/generate-jitconfig"), body)
	if err != nil {
		return nil, err
	}
	var j JITConfig
	return &j, json.Unmarshal(data, &j)
}

func (c *Client) GetRepo(ctx context.Context, repo string) (*Repository, error) {
	data, _, err := c.do(ctx, http.MethodGet, c.repoURL(repo, ""), nil)
	if err != nil {
		return nil, err
	}
	var r Repository
	return &r, json.Unmarshal(data, &r)
}

// IsKind reports whether err is an *APIError of the given kind.
func IsKind(err error, k ErrKind) bool {
	var e *APIError
	return errors.As(err, &e) && e.Kind == k
}
```

- [ ] **Step 4: Run the check**

Run: `go test -count=1 -timeout 180s ./internal/github/`
Expected: PASS (exit 0).

- [ ] **Step 5: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/github/client_test.go internal/github/client.go
git commit -m "feat(github): add REST client with ETag cache"
```

---

### Task 6: Job history store

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `internal/history/history.go`
- Test: `internal/history/history_test.go`

**Interfaces:**
- Consumes: `model.HistoryEntry` (Contracts → model types).
- Produces: `history.Store` (Contracts → history).

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing test `internal/history/history_test.go`**

```go
package history

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

func TestAppendQueryPrune(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	if got, err := s.Query("", "", 0); err != nil || len(got) != 0 {
		t.Fatalf("empty store: %v %v", got, err)
	}
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i, e := range []model.HistoryEntry{
		{ID: "a1", Repo: "a", Conclusion: "success", FinishedAt: t0},
		{ID: "b1", Repo: "b", Conclusion: "failure", FinishedAt: t0.Add(time.Hour)},
		{ID: "a2", Repo: "a", Conclusion: "failure", FinishedAt: t0.Add(2 * time.Hour)},
	} {
		if err := s.Append(e); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	got, _ := s.Query("a", "", 0)
	if len(got) != 2 || got[0].ID != "a2" {
		t.Fatalf("repo filter newest first: %+v", got)
	}
	got, _ = s.Query("", "failure", 1)
	if len(got) != 1 || got[0].ID != "a2" {
		t.Fatalf("conclusion+limit: %+v", got)
	}
	if err := s.Prune(t0.Add(30 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Query("", "", 0)
	if len(got) != 2 || got[1].ID != "b1" {
		t.Fatalf("after prune: %+v", got)
	}
}

func TestQueryOrdersByFinishTime(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, e := range []model.HistoryEntry{
		{ID: "late", Repo: "a", RunID: 1, FinishedAt: t0.Add(2 * time.Hour)},
		{ID: "early", Repo: "a", RunID: 2, FinishedAt: t0},
		{ID: "mid", Repo: "a", RunID: 3, FinishedAt: t0.Add(time.Hour)},
	} {
		if err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := s.Query("a", "", 0)
	if len(got) != 3 || got[0].ID != "late" || got[1].ID != "mid" || got[2].ID != "early" {
		t.Fatalf("order = %+v", got)
	}
	got, _ = s.Query("a", "", 1)
	if len(got) != 1 || got[0].ID != "late" {
		t.Fatalf("limit must keep the newest: %+v", got)
	}
}

func TestAppendSkipsDuplicateInstanceRun(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	e := model.HistoryEntry{ID: "abc123", Repo: "a", RunID: 7, Conclusion: "success", FinishedAt: time.Now()}
	for i := 0; i < 2; i++ {
		if err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	e.RunID = 8
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Query("", "", 0)
	if len(got) != 2 {
		t.Fatalf("want 2 entries (duplicate skipped), got %+v", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./internal/history/`
Expected: FAIL — FAIL: `undefined: Store`

- [ ] **Step 3: Write `internal/history/history.go`**

```go
// Package history stores finished jobs as JSON lines.
package history

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

type Store struct {
	Path string
	mu   sync.Mutex
}

// Append records e unless an entry with the same instance ID and run ID is
// already stored, so a finalization repeated after a crash adds no duplicate.
func (s *Store) Append(e model.HistoryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return err
	}
	for _, x := range all {
		if x.ID == e.ID && x.RunID == e.RunID {
			return nil
		}
	}
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = f.Write(append(line, '\n'))
	return err
}

func (s *Store) readAll() ([]model.HistoryEntry, error) {
	f, err := os.Open(s.Path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []model.HistoryEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var e model.HistoryEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

// Query returns entries by FinishedAt, newest first, filtered by repo and
// conclusion when non-empty. limit <= 0 means no limit. Lines are appended in
// cleanup order, which is not finish order.
func (s *Store) Query(repo, conclusion string, limit int) ([]model.HistoryEntry, error) {
	s.mu.Lock()
	all, err := s.readAll()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(all, func(a, b int) bool { return all[a].FinishedAt.After(all[b].FinishedAt) })
	var out []model.HistoryEntry
	for _, e := range all {
		if (repo == "" || e.Repo == repo) && (conclusion == "" || e.Conclusion == conclusion) {
			out = append(out, e)
			if limit > 0 && len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

// Prune drops entries that finished before cutoff, rewriting the file atomically.
func (s *Store) Prune(cutoff time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil || all == nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".history-*.jsonl")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	w := bufio.NewWriter(tmp)
	for _, e := range all {
		if e.FinishedAt.Before(cutoff) {
			continue
		}
		line, _ := json.Marshal(e)
		w.Write(append(line, '\n'))
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.Path)
}
```

- [ ] **Step 4: Run the check**

Run: `go test -count=1 -timeout 180s ./internal/history/`
Expected: PASS (exit 0).

- [ ] **Step 5: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/history/history_test.go internal/history/history.go
git commit -m "feat(history): store finished jobs as JSON lines"
```

---

### Task 7: Event ring

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `internal/events/events.go`
- Test: `internal/events/events_test.go`

**Interfaces:**
- Consumes: `model.Event` (Contracts → model types).
- Produces: `events.Ring` (Contracts → events).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test `internal/events/events_test.go`**

```go
package events

import "testing"

func TestRingAfterAndCapacity(t *testing.T) {
	r := New()
	for i := 0; i < Capacity+5; i++ {
		r.Add("info", "a", "event %d", i)
	}
	all := r.After(0)
	if len(all) != Capacity || all[0].Seq != 6 {
		t.Fatalf("len=%d first=%d", len(all), all[0].Seq)
	}
	tail := r.After(int64(Capacity + 3))
	if len(tail) != 2 || tail[1].Msg != "event 1004" {
		t.Fatalf("tail = %+v", tail)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./internal/events/`
Expected: FAIL — FAIL: `undefined: New`

- [ ] **Step 3: Write `internal/events/events.go`**

```go
// Package events keeps the most recent daemon events in memory.
package events

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

const Capacity = 1000

type Ring struct {
	Now   func() time.Time
	mu    sync.Mutex
	seq   int64
	items []model.Event
}

func New() *Ring { return &Ring{Now: time.Now} }

// Add records an event and mirrors it to the process log (journald under systemd).
func (r *Ring) Add(level, repo, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	r.mu.Lock()
	r.seq++
	r.items = append(r.items, model.Event{Seq: r.seq, Time: r.Now(), Level: level, Repo: repo, Msg: msg})
	if len(r.items) > Capacity {
		r.items = r.items[len(r.items)-Capacity:]
	}
	r.mu.Unlock()
	log.Printf("%s %s %s", level, repo, msg)
}

// After returns events with Seq > seq, oldest first.
func (r *Ring) After(seq int64) []model.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []model.Event
	for _, e := range r.items {
		if e.Seq > seq {
			out = append(out, e)
		}
	}
	return out
}
```

- [ ] **Step 4: Run the check**

Run: `go test -count=1 -timeout 180s ./internal/events/`
Expected: PASS (exit 0).

- [ ] **Step 5: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/events/events_test.go internal/events/events.go
git commit -m "feat(events): keep recent daemon events in memory"
```

---

### Task 8: System adapters (systemd, docker, cp/chown, df)

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `internal/system/system.go`
- Test: `internal/system/system_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: package `system` (Contracts → system).

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing test `internal/system/system_test.go`**

```go
package system

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type call struct {
	name string
	args []string
}

func (c call) String() string { return c.name + " " + strings.Join(c.args, " ") }

// fake returns outputs by command-line prefix and records every call.
func fake(outputs map[string]string) (Runner, *[]call) {
	var calls []call
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		c := call{name, args}
		calls = append(calls, c)
		for prefix, out := range outputs {
			if strings.HasPrefix(c.String(), prefix) {
				if strings.HasPrefix(out, "ERR:") {
					return nil, errors.New(out[4:])
				}
				return []byte(out), nil
			}
		}
		return nil, nil
	}, &calls
}

func TestSystemdStartArgs(t *testing.T) {
	run, calls := fake(nil)
	err := Systemd{Run: run}.Start(context.Background(), UnitSpec{
		Unit: "ghr-runner-abc123", User: "ghrunner", WorkDir: "/var/lib/ghr/instances/abc123",
		Props: []string{"MemoryMax=6G"}, Env: map[string]string{"B": "2", "A": "1"},
		Command: []string{"/x/run.sh", "--jitconfig", "ENC"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "systemd-run --unit=ghr-runner-abc123 --uid=ghrunner --gid=ghrunner --collect --quiet " +
		"--working-directory=/var/lib/ghr/instances/abc123 --property=MemoryMax=6G --setenv=A=1 --setenv=B=2 -- /x/run.sh --jitconfig ENC"
	if got := (*calls)[0].String(); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestSystemdActiveAndList(t *testing.T) {
	run, _ := fake(map[string]string{
		"systemctl is-active ghr-runner-aaaaaa": "active\n",
		"systemctl is-active ghr-runner-bbbbbb": "inactive\n",
		"systemctl list-units":                  "ghr-runner-aaaaaa.service loaded active running x\nghr-runner-cccccc.service loaded failed failed y\n",
	})
	s := Systemd{Run: run}
	if ok, _ := s.Active(context.Background(), "ghr-runner-aaaaaa"); !ok {
		t.Fatal("aaaaaa should be active")
	}
	if ok, err := s.Active(context.Background(), "ghr-runner-bbbbbb"); ok || err != nil {
		t.Fatalf("bbbbbb should be confirmed inactive, got %v %v", ok, err)
	}
	units, err := s.List(context.Background(), "ghr-runner-")
	if err != nil || len(units) != 1 || units[0] != "ghr-runner-aaaaaa" {
		t.Fatalf("units = %v err = %v", units, err)
	}
}

func TestDockerParsing(t *testing.T) {
	run, calls := fake(map[string]string{
		"docker ps -a --no-trunc --filter label=com.docker.compose.project.working_dir": "c1\tghr-abc123\t/var/lib/ghr/instances/abc123/_work/r/r\n",
		"docker ps -a --no-trunc --format":                                              "c2\tdc-e2e-web\nc3\tother\n",
		"docker network ls":                                                             "n1\nn2\n",
		"docker volume ls":                                                              "",
		"docker info":                                                                   "/var/lib/docker\n",
		"df --output=pcent /var/lib/docker":                                             "Use%\n 81%\n",
		"docker builder prune --help":                                                   "Options:\n      --reserved-space bytes   Amount of disk space always allowed to keep for cache\n",
		"docker builder prune -f --reserved-space":                                      "ID\nTotal:\t6.2GB\n",
	})
	d := Docker{Run: run}
	ctx := context.Background()
	cc, _ := d.ComposeContainers(ctx)
	if len(cc) != 1 || cc[0].Project != "ghr-abc123" || !strings.HasSuffix(cc[0].WorkingDir, "/r/r") {
		t.Fatalf("compose = %+v", cc)
	}
	nc, _ := d.Containers(ctx)
	if len(nc) != 2 || nc[0].Name != "dc-e2e-web" {
		t.Fatalf("containers = %+v", nc)
	}
	if err := d.RemoveNetworksByLabel(ctx, "com.docker.compose.project=p"); err != nil {
		t.Fatal(err)
	}
	if err := d.RemoveVolumesByLabel(ctx, "com.docker.compose.project=p"); err != nil {
		t.Fatal(err)
	}
	pct, err := d.DataRootUsage(ctx)
	if err != nil || pct != 81 {
		t.Fatalf("pct = %d err = %v", pct, err)
	}
	freed, err := d.PruneBuildCacheTo(ctx, "20GB")
	if err != nil || freed != "6.2GB" {
		t.Fatalf("freed = %q err = %v", freed, err)
	}
	var cmds []string
	for _, c := range *calls {
		cmds = append(cmds, c.String())
	}
	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "docker network rm n1 n2") {
		t.Fatalf("networks not removed:\n%s", joined)
	}
	if strings.Contains(joined, "docker volume rm") {
		t.Fatalf("volume rm called with nothing to remove:\n%s", joined)
	}
	if !strings.Contains(joined, "docker builder prune -f --reserved-space 20GB") {
		t.Fatalf("prune flag wrong:\n%s", joined)
	}
}

func TestPruneWithoutReservedSpaceUsesKeepStorage(t *testing.T) {
	run, calls := fake(map[string]string{"docker builder prune --help": "Options:\n      --keep-storage bytes   Amount of disk space to keep for cache\n"})
	if _, err := (Docker{Run: run}).PruneBuildCacheTo(context.Background(), "20GB"); err != nil {
		t.Fatal(err)
	}
	if got := (*calls)[1].String(); got != "docker builder prune -f --keep-storage 20GB" {
		t.Fatalf("got %s", got)
	}
}

func TestExecErrorHidesArguments(t *testing.T) {
	_, err := Exec(context.Background(), "definitely-not-a-command-ghr", "first", "SECRET")
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v", err)
	}
}

func TestSystemdActiveErrorsAreNotInactive(t *testing.T) {
	run, _ := fake(map[string]string{
		"systemctl is-active ghr-runner-gone00": "ERR:exit status 3",
		"systemctl is-active ghr-runner-bus000": "ERR:Failed to connect to bus",
	})
	s := Systemd{Run: run}
	// A unit that no longer exists prints "inactive" or "unknown" with exit 3.
	gone, _ := fake(map[string]string{"systemctl is-active": "unknown\n"})
	if ok, err := (Systemd{Run: gone}).Active(context.Background(), "ghr-runner-gone00"); ok || err != nil {
		t.Fatalf("unknown unit: %v %v", ok, err)
	}
	if _, err := s.Active(context.Background(), "ghr-runner-bus000"); err == nil {
		t.Fatal("a bus failure with no state must be an error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Active(ctx, "ghr-runner-gone00"); err == nil {
		t.Fatal("a cancelled context must be an error")
	}
}

func TestProjectContainers(t *testing.T) {
	run, calls := fake(map[string]string{"docker ps": "c1\tghr-abc123-db-1\tpostgres:17\trunning\nbad line\n"})
	got, err := Docker{Run: run}.ProjectContainers(context.Background(), "ghr-abc123")
	if err != nil || len(got) != 1 || got[0] != (ProjectContainer{ID: "c1", Name: "ghr-abc123-db-1", Image: "postgres:17", State: "running"}) {
		t.Fatalf("got %+v err %v", got, err)
	}
	if !strings.Contains((*calls)[0].String(), "--filter label=com.docker.compose.project=ghr-abc123") {
		t.Fatalf("call = %s", (*calls)[0])
	}
}

func TestExecTimesOut(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not on PATH")
	}
	old := CommandTimeout
	CommandTimeout = 100 * time.Millisecond
	defer func() { CommandTimeout = old }()
	start := time.Now()
	if _, err := Exec(context.Background(), "sleep", "10"); err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("Exec ignored CommandTimeout: %v", time.Since(start))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./internal/system/`
Expected: FAIL — FAIL: `undefined: Systemd`

- [ ] **Step 3: Write `internal/system/system.go`**

```go
// Package system wraps the host commands ghr drives: systemd, docker, cp/chown and df.
package system

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Runner executes a command and returns its stdout.
type Runner func(ctx context.Context, name string, args ...string) ([]byte, error)

// CommandTimeout bounds every command Exec runs, so a hung docker or systemctl
// cannot block the daemon loop or shutdown.
var CommandTimeout = 10 * time.Minute

// Exec is the real Runner. Errors name only the command and its first argument,
// because full argument lists can carry secrets (the JIT config).
func Exec(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, CommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		first := ""
		if len(args) > 0 {
			first = " " + args[0]
		}
		return out, fmt.Errorf("%s%s: %w: %s", name, first, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

type UnitSpec struct {
	Unit    string
	User    string
	WorkDir string
	Props   []string          // e.g. "MemoryMax=6G"
	Env     map[string]string // passed with --setenv
	Command []string
}

type Systemd struct{ Run Runner }

func (s Systemd) Start(ctx context.Context, u UnitSpec) error {
	args := []string{"--unit=" + u.Unit, "--uid=" + u.User, "--gid=" + u.User, "--collect", "--quiet"}
	if u.WorkDir != "" {
		args = append(args, "--working-directory="+u.WorkDir)
	}
	for _, p := range u.Props {
		args = append(args, "--property="+p)
	}
	keys := make([]string, 0, len(u.Env))
	for k := range u.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--setenv="+k+"="+u.Env[k])
	}
	args = append(args, "--")
	args = append(args, u.Command...)
	_, err := s.Run(ctx, "systemd-run", args...)
	return err
}

func (s Systemd) Stop(ctx context.Context, unit string) error {
	_, err := s.Run(ctx, "systemctl", "stop", unit)
	return err
}

var (
	activeStates   = map[string]bool{"active": true, "activating": true, "deactivating": true, "reloading": true}
	inactiveStates = map[string]bool{"inactive": true, "failed": true, "unknown": true}
)

// Active reports whether the unit is running. It returns false only when
// systemctl prints a confirmed inactive state ("unknown" is a unit that no
// longer exists); anything else, such as a cancelled context or a bus error,
// is an error so callers never mistake a failed query for an exited runner.
func (s Systemd) Active(ctx context.Context, unit string) (bool, error) {
	out, err := s.Run(ctx, "systemctl", "is-active", unit) // exits 3 when not active
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	state := strings.TrimSpace(string(out))
	switch {
	case activeStates[state]:
		return true, nil
	case inactiveStates[state]:
		return false, nil
	case err != nil:
		return false, err
	}
	return false, fmt.Errorf("systemctl is-active %s: unexpected output %q", unit, state)
}

// List returns the active units whose name starts with prefix, without ".service".
func (s Systemd) List(ctx context.Context, prefix string) ([]string, error) {
	out, err := s.Run(ctx, "systemctl", "list-units", "--all", "--plain", "--no-legend", "--type=service", prefix+"*")
	if err != nil {
		return nil, err
	}
	var units []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || !activeStates[f[2]] {
			continue
		}
		units = append(units, strings.TrimSuffix(f[0], ".service"))
	}
	return units, nil
}

type ComposeContainer struct {
	ID         string
	Project    string
	WorkingDir string
}

type NamedContainer struct {
	ID   string
	Name string
}

type ProjectContainer struct {
	ID    string
	Name  string
	Image string
	State string
}

type Docker struct{ Run Runner }

func lines(out []byte) []string {
	var res []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			res = append(res, l)
		}
	}
	return res
}

func (d Docker) ComposeContainers(ctx context.Context) ([]ComposeContainer, error) {
	out, err := d.Run(ctx, "docker", "ps", "-a", "--no-trunc",
		"--filter", "label=com.docker.compose.project.working_dir",
		"--format", `{{.ID}}	{{.Label "com.docker.compose.project"}}	{{.Label "com.docker.compose.project.working_dir"}}`)
	if err != nil {
		return nil, err
	}
	var res []ComposeContainer
	for _, l := range lines(out) {
		f := strings.SplitN(l, "\t", 3)
		if len(f) == 3 {
			res = append(res, ComposeContainer{ID: f[0], Project: f[1], WorkingDir: f[2]})
		}
	}
	return res, nil
}

func (d Docker) Containers(ctx context.Context) ([]NamedContainer, error) {
	out, err := d.Run(ctx, "docker", "ps", "-a", "--no-trunc", "--format", "{{.ID}}\t{{.Names}}")
	if err != nil {
		return nil, err
	}
	var res []NamedContainer
	for _, l := range lines(out) {
		f := strings.SplitN(l, "\t", 2)
		if len(f) == 2 {
			res = append(res, NamedContainer{ID: f[0], Name: f[1]})
		}
	}
	return res, nil
}

// ProjectContainers lists the containers of one compose project.
func (d Docker) ProjectContainers(ctx context.Context, project string) ([]ProjectContainer, error) {
	out, err := d.Run(ctx, "docker", "ps", "-a", "--no-trunc",
		"--filter", "label=com.docker.compose.project="+project,
		"--format", "{{.ID}}\t{{.Names}}\t{{.Image}}\t{{.State}}")
	if err != nil {
		return nil, err
	}
	var res []ProjectContainer
	for _, l := range lines(out) {
		f := strings.SplitN(l, "\t", 4)
		if len(f) == 4 {
			res = append(res, ProjectContainer{ID: f[0], Name: f[1], Image: f[2], State: f[3]})
		}
	}
	return res, nil
}

func (d Docker) ContainerIDsByLabel(ctx context.Context, label string) ([]string, error) {
	out, err := d.Run(ctx, "docker", "ps", "-aq", "--no-trunc", "--filter", "label="+label)
	return lines(out), err
}

func (d Docker) RemoveContainers(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := d.Run(ctx, "docker", append([]string{"rm", "-f", "-v"}, ids...)...)
	return err
}

func (d Docker) RemoveNetworksByLabel(ctx context.Context, label string) error {
	out, err := d.Run(ctx, "docker", "network", "ls", "-q", "--filter", "label="+label)
	if err != nil || len(lines(out)) == 0 {
		return err
	}
	_, err = d.Run(ctx, "docker", append([]string{"network", "rm"}, lines(out)...)...)
	return err
}

func (d Docker) RemoveVolumesByLabel(ctx context.Context, label string) error {
	out, err := d.Run(ctx, "docker", "volume", "ls", "-q", "--filter", "label="+label)
	if err != nil || len(lines(out)) == 0 {
		return err
	}
	_, err = d.Run(ctx, "docker", append([]string{"volume", "rm", "-f"}, lines(out)...)...)
	return err
}

// DataRootUsage returns the used percentage of the filesystem holding Docker's data root.
func (d Docker) DataRootUsage(ctx context.Context) (int, error) {
	root, err := d.Run(ctx, "docker", "info", "--format", "{{.DockerRootDir}}")
	if err != nil {
		return 0, err
	}
	out, err := d.Run(ctx, "df", "--output=pcent", strings.TrimSpace(string(root)))
	if err != nil {
		return 0, err
	}
	l := lines(out)
	if len(l) < 2 {
		return 0, fmt.Errorf("unexpected df output %q", out)
	}
	return strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(l[len(l)-1]), "%"))
}

func (d Docker) PruneBuildCacheOlderThan(ctx context.Context, hours int) (string, error) {
	out, err := d.Run(ctx, "docker", "builder", "prune", "-f", "--filter", fmt.Sprintf("until=%dh", hours))
	return reclaimed(out), err
}

// PruneBuildCacheTo prunes the build cache down to keep (e.g. "20GB"). The
// flag name differs between CLI versions and between the classic builder and
// buildx (which "docker builder" may alias), so it is read from the installed
// command's help: --reserved-space when offered, else --keep-storage.
func (d Docker) PruneBuildCacheTo(ctx context.Context, keep string) (string, error) {
	help, err := d.Run(ctx, "docker", "builder", "prune", "--help")
	if err != nil {
		return "", err
	}
	flag := "--keep-storage"
	if strings.Contains(string(help), "--reserved-space") {
		flag = "--reserved-space"
	}
	out, err := d.Run(ctx, "docker", "builder", "prune", "-f", flag, keep)
	return reclaimed(out), err
}

func (d Docker) PruneDanglingImages(ctx context.Context) (string, error) {
	out, err := d.Run(ctx, "docker", "image", "prune", "-f")
	return reclaimed(out), err
}

func reclaimed(out []byte) string {
	for _, l := range lines(out) {
		if strings.HasPrefix(l, "Total reclaimed space:") || strings.HasPrefix(l, "Total:") {
			return strings.TrimSpace(l[strings.Index(l, ":")+1:])
		}
	}
	return "0B"
}

type Host struct{ Run Runner }

// CopyTree copies src to dst (which must not exist), preserving modes and symlinks.
func (h Host) CopyTree(ctx context.Context, src, dst string) error {
	_, err := h.Run(ctx, "cp", "-a", src, dst)
	return err
}

func (h Host) ChownR(ctx context.Context, path, user string) error {
	_, err := h.Run(ctx, "chown", "-R", user+":"+user, path)
	return err
}
```

- [ ] **Step 4: Run the check**

Run: `go test -count=1 -timeout 180s ./internal/system/`
Expected: PASS (exit 0).

- [ ] **Step 5: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/system/system_test.go internal/system/system.go
git commit -m "feat(system): add systemd and docker adapters"
```

---

### Task 9: Runner job hooks

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `hooks/job-started.sh`
- Create: `hooks/job-completed.sh`
- Test: `hooks/hooks_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `hooks/job-started.sh` writing `job.json` and `hooks/job-completed.sh` adding `finished_at` to it (Contracts → Instance files).

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

> Write both scripts with the Write tool, never a shell heredoc. The tests need `bash` and `jq` on PATH (both are on GitHub's ubuntu-latest image, and `setup.sh` installs `jq`); without them the tests skip, so check `jq --version` first and install jq if it is missing.

- [ ] **Step 1: Write the failing test `hooks/hooks_test.go`**

```go
package hooks

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func runHook(t *testing.T, script string, env ...string) {
	t.Helper()
	// Skip on a developer machine without the tools; CI (GitHub sets CI=true) must run these.
	skip := t.Skip
	if os.Getenv("CI") != "" {
		skip = t.Fatal
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		skip("bash not available")
	}
	if _, err := exec.LookPath("jq"); err != nil {
		skip("jq not available")
	}
	cmd := exec.Command(bash, script)
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s exited non-zero: %v\n%s", script, err, out)
	}
}

func readRecord(t *testing.T, dir string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "job.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]string
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("invalid JSON %q: %v", data, err)
	}
	return rec
}

// checkTime asserts v is an RFC3339 UTC second-precision time within [from, to].
func checkTime(t *testing.T, name, v string, from, to time.Time) {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, v)
	if err != nil {
		t.Fatalf("%s %q is not RFC3339: %v", name, v, err)
	}
	if ts.Before(from.Truncate(time.Second)) || ts.After(to) {
		t.Fatalf("%s %v outside [%v, %v]", name, ts, from, to)
	}
}

func TestJobStartedWritesJobJSON(t *testing.T) {
	dir := t.TempDir()
	before := time.Now()
	runHook(t, "job-started.sh", "GHR_INSTANCE_DIR="+dir, "GITHUB_RUN_ID=123", "GITHUB_RUN_ATTEMPT=1",
		"GITHUB_RUN_NUMBER=412", "GITHUB_WORKFLOW=CI \"main\"\n\tnightly", `GITHUB_JOB=e2e\win`, "RUNNER_NAME=ghr-darkcloud-abc123")
	rec := readRecord(t, dir)
	if rec["run_id"] != "123" || rec["run_attempt"] != "1" || rec["run_number"] != "412" ||
		rec["workflow"] != "CI \"main\"\n\tnightly" || rec["job"] != `e2e\win` || rec["runner_name"] != "ghr-darkcloud-abc123" {
		t.Fatalf("record %v", rec)
	}
	checkTime(t, "started_at", rec["started_at"], before, time.Now())
	if _, err := os.Stat(filepath.Join(dir, "job.json.tmp")); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}
}

func TestHooksExitZeroWithoutInstanceDir(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	runHook(t, "job-started.sh", "GHR_INSTANCE_DIR=")
	runHook(t, "job-started.sh", "GHR_INSTANCE_DIR="+missing)
	runHook(t, "job-completed.sh", "GHR_INSTANCE_DIR=")
	runHook(t, "job-completed.sh", "GHR_INSTANCE_DIR="+missing)
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("a hook created the missing instance dir")
	}
}

func TestJobCompletedAddsFinishedAt(t *testing.T) {
	dir := t.TempDir()
	runHook(t, "job-started.sh", "GHR_INSTANCE_DIR="+dir, "GITHUB_RUN_ID=123", "RUNNER_NAME=ghr-r-abc123")
	before := time.Now()
	runHook(t, "job-completed.sh", "GHR_INSTANCE_DIR="+dir)
	rec := readRecord(t, dir)
	if rec["run_id"] != "123" || rec["runner_name"] != "ghr-r-abc123" {
		t.Fatalf("start fields lost: %v", rec)
	}
	checkTime(t, "finished_at", rec["finished_at"], before, time.Now())
}

func TestJobCompletedWithoutJobJSON(t *testing.T) {
	dir := t.TempDir()
	before := time.Now()
	runHook(t, "job-completed.sh", "GHR_INSTANCE_DIR="+dir)
	rec := readRecord(t, dir)
	if len(rec) != 1 {
		t.Fatalf("want finished_at only, got %v", rec)
	}
	checkTime(t, "finished_at", rec["finished_at"], before, time.Now())
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./hooks/`
Expected: FAIL — FAIL: `job-started.sh exited non-zero` (bash cannot open the missing script)

- [ ] **Step 3: Write `hooks/job-started.sh`**

```bash
#!/usr/bin/env bash
# Runner pre-job hook. GitHub fails the job if this exits non-zero, so it must
# never talk to the daemon and must always exit 0. It only records the job.
# jq does the JSON encoding: workflow names may hold quotes or control characters.
dir="${GHR_INSTANCE_DIR:-}"
[ -n "$dir" ] && [ -d "$dir" ] || exit 0
jq -nc \
  --arg run_id "${GITHUB_RUN_ID:-}" --arg run_attempt "${GITHUB_RUN_ATTEMPT:-}" \
  --arg run_number "${GITHUB_RUN_NUMBER:-}" --arg workflow "${GITHUB_WORKFLOW:-}" \
  --arg job "${GITHUB_JOB:-}" --arg runner_name "${RUNNER_NAME:-}" \
  --arg started_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  '$ARGS.named' > "$dir/job.json.tmp" 2>/dev/null \
  && mv -f "$dir/job.json.tmp" "$dir/job.json" 2>/dev/null
exit 0
```

- [ ] **Step 4: Write `hooks/job-completed.sh`**

```bash
#!/usr/bin/env bash
# Runner post-job hook. Must always exit 0. Adds finished_at to job.json; when
# job.json is missing or unreadable it records finished_at alone.
dir="${GHR_INSTANCE_DIR:-}"
[ -n "$dir" ] && [ -d "$dir" ] || exit 0
now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
out="$(jq -c --arg f "$now" '. + {finished_at: $f}' "$dir/job.json" 2>/dev/null)" \
  || out="$(jq -nc --arg f "$now" '{finished_at: $f}' 2>/dev/null)" \
  || exit 0
printf '%s\n' "$out" > "$dir/job.json.tmp" 2>/dev/null \
  && mv -f "$dir/job.json.tmp" "$dir/job.json" 2>/dev/null
exit 0
```

- [ ] **Step 5: Run the check**

Run: `go test -count=1 -timeout 180s ./hooks/`
Expected: PASS (exit 0).

- [ ] **Step 6: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add hooks/hooks_test.go hooks/job-started.sh hooks/job-completed.sh
git commit -m "feat(hooks): record job start and finish"
```

---

### Task 10: Release workflow

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `.github/workflows/release.yml`

**Interfaces:**
- Consumes: `cmd/ghr`, `hooks/*.sh` (Contracts → Release assets).
- Produces: release assets `ghr_linux_amd64.tar.gz` and `checksums.txt` (Contracts → Release assets).

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 0 - spec 0 - coupling 1 - risk 1 = 2

- [ ] **Step 1: Write `.github/workflows/release.yml`**

```yaml
name: release

on:
  push:
    tags: ["v*"]

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - run: go test -count=1 -timeout 180s ./...
      - name: build
        env:
          CGO_ENABLED: "0"
          GOOS: linux
          GOARCH: amd64
        run: |
          mkdir -p dist/ghr
          go build -trimpath -ldflags "-s -w -X main.version=${GITHUB_REF_NAME}" -o dist/ghr/ghr ./cmd/ghr
          cp hooks/job-started.sh hooks/job-completed.sh dist/ghr/
          chmod 0755 dist/ghr/*
          tar -C dist -czf dist/ghr_linux_amd64.tar.gz ghr
          (cd dist && sha256sum ghr_linux_amd64.tar.gz > checksums.txt)
      - name: publish
        env:
          GH_TOKEN: ${{ github.token }}
        run: gh release create "$GITHUB_REF_NAME" dist/ghr_linux_amd64.tar.gz dist/checksums.txt --generate-notes
```

- [ ] **Step 2: Run the check**

Run: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X main.version=v0.0.0-test" -o /tmp/ghr-release-check ./cmd/ghr && rm -f /tmp/ghr-release-check`
Expected: PASS (exit 0).

- [ ] **Step 3: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "chore(release): publish linux/amd64 tarball"
```

---

### Task 11: Control API server and client

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `internal/api/server.go`
- Create: `internal/api/client.go`
- Test: `internal/api/api_test.go`

**Interfaces:**
- Consumes: package `model` (Contracts → model types).
- Produces: `api.Backend`, `api.NewServer`, `api.Client`, `api.Error` (Contracts → api).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test `internal/api/api_test.go`**

```go
package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

type fakeBackend struct {
	patches  []model.ConfigPatch
	added    []model.AddRepoRequest
	removed  []string
	pauseAll []bool
	token    string
	killed   []string
}

func (f *fakeBackend) Status() model.Status {
	return model.Status{Mode: "queue", GlobalMax: 2, Repos: []model.RepoStatus{{Name: "darkcloud", Max: 1, Queued: 2}}}
}
func (f *fakeBackend) EventsAfter(seq int64) []model.Event {
	if seq >= 1 {
		return nil
	}
	return []model.Event{{Seq: 1, Level: "info", Msg: "hello"}}
}
func (f *fakeBackend) History(repo, conclusion string, limit int) ([]model.HistoryEntry, error) {
	return []model.HistoryEntry{{ID: "a", Repo: repo, Conclusion: conclusion, RunNumber: "7"}}, nil
}
func (f *fakeBackend) RunnerLog(id, cursor string) (model.LogChunk, error) {
	if id != "aaaaaa" {
		return model.LogChunk{}, NotFound("unknown runner " + id)
	}
	return model.LogChunk{Data: "log after " + cursor, Next: cursor + "+"}, nil
}
func (f *fakeBackend) RunnerContainers(ctx context.Context, id string) ([]model.Container, error) {
	if id != "aaaaaa" {
		return nil, NotFound("unknown runner " + id)
	}
	return []model.Container{{ID: "c1", Name: "ghr-aaaaaa-db-1", Image: "postgres", State: "running", Project: "ghr-aaaaaa"}}, nil
}
func (f *fakeBackend) RunnerSteps(ctx context.Context, id string) ([]model.Step, error) {
	return []model.Step{{Number: 1, Name: "checkout", Status: "completed", Conclusion: "success"}}, nil
}
func (f *fakeBackend) Config() any { return map[string]any{"owner": "darkraise"} }
func (f *fakeBackend) PatchConfig(p model.ConfigPatch) error {
	if p.GlobalMax != nil && *p.GlobalMax < 1 {
		return BadRequest("global_max must be >= 1")
	}
	f.patches = append(f.patches, p)
	return nil
}
func (f *fakeBackend) AddRepo(ctx context.Context, req model.AddRepoRequest) error {
	if !req.AllowPublic && req.Name == "public" {
		return Conflict("public repo")
	}
	f.added = append(f.added, req)
	return nil
}
func (f *fakeBackend) RemoveRepo(name string) error { f.removed = append(f.removed, name); return nil }
func (f *fakeBackend) SetPausedAll(p bool) error    { f.pauseAll = append(f.pauseAll, p); return nil }
func (f *fakeBackend) SetToken(ctx context.Context, t string) error {
	f.token = t
	return nil
}
func (f *fakeBackend) KillRunner(ctx context.Context, id string) error {
	f.killed = append(f.killed, id)
	return nil
}

func setup(t *testing.T) (*Client, *fakeBackend) {
	t.Helper()
	b := &fakeBackend{}
	srv := httptest.NewServer(NewServer(b))
	t.Cleanup(srv.Close)
	return &Client{Base: srv.URL, HTTP: &http.Client{Timeout: 5 * time.Second}}, b
}

func TestStatusEventsHistory(t *testing.T) {
	c, _ := setup(t)
	ctx := context.Background()
	st, err := c.Status(ctx)
	if err != nil || st.Repos[0].Queued != 2 {
		t.Fatalf("status %+v err %v", st, err)
	}
	ev, err := c.Events(ctx, 0)
	if err != nil || len(ev) != 1 {
		t.Fatalf("events %+v err %v", ev, err)
	}
	ev, err = c.Events(ctx, 1)
	if err != nil || ev == nil || len(ev) != 0 {
		t.Fatalf("events after 1: %+v err %v", ev, err)
	}
	h, err := c.History(ctx, "darkmem", "failure", 5)
	if err != nil || h[0].Repo != "darkmem" || h[0].Conclusion != "failure" {
		t.Fatalf("history %+v err %v", h, err)
	}
}

func TestLogStepsConfig(t *testing.T) {
	c, _ := setup(t)
	ctx := context.Background()
	l, err := c.Log(ctx, "aaaaaa", "Runner_1.log=10&Worker_2.log=3")
	if err != nil || l.Data != "log after Runner_1.log=10&Worker_2.log=3" || l.Next != "Runner_1.log=10&Worker_2.log=3+" {
		t.Fatalf("log %+v err %v", l, err)
	}
	_, err = c.Log(ctx, "zzzzzz", "")
	var ae *Error
	if !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("unknown runner err %v", err)
	}
	cs, err := c.Containers(ctx, "aaaaaa")
	if err != nil || len(cs) != 1 || cs[0].Project != "ghr-aaaaaa" {
		t.Fatalf("containers %+v err %v", cs, err)
	}
	if _, err := c.Containers(ctx, "zzzzzz"); !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("unknown runner containers err %v", err)
	}
	s, err := c.Steps(ctx, "aaaaaa")
	if err != nil || s[0].Name != "checkout" {
		t.Fatalf("steps %+v err %v", s, err)
	}
	var cfg map[string]any
	if err := c.Config(ctx, &cfg); err != nil || cfg["owner"] != "darkraise" {
		t.Fatalf("config %v err %v", cfg, err)
	}
}

func TestMutations(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	three := 3
	if err := c.PatchConfig(ctx, model.ConfigPatch{GlobalMax: &three}); err != nil {
		t.Fatal(err)
	}
	zero := 0
	err := c.PatchConfig(ctx, model.ConfigPatch{GlobalMax: &zero})
	var ae *Error
	if !errors.As(err, &ae) || ae.Status != 400 || ae.Msg != "global_max must be >= 1" {
		t.Fatalf("validation err %v", err)
	}
	if err := c.Pause(ctx, "darkcloud"); err != nil {
		t.Fatal(err)
	}
	if err := c.Resume(ctx, "darkcloud"); err != nil {
		t.Fatal(err)
	}
	if !*b.patches[1].Repos["darkcloud"].Paused || *b.patches[2].Repos["darkcloud"].Paused {
		t.Fatalf("pause patches %+v", b.patches)
	}
	if err := c.AddRepo(ctx, model.AddRepoRequest{Name: "public"}); !errors.As(err, &ae) || ae.Status != 409 {
		t.Fatalf("public repo err %v", err)
	}
	if err := c.AddRepo(ctx, model.AddRepoRequest{Name: "newrepo", Labels: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveRepo(ctx, "old"); err != nil {
		t.Fatal(err)
	}
	if err := c.PauseAll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.ResumeAll(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.SetToken(ctx, "  github_pat_x\n"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetToken(ctx, "   "); !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("empty token err %v", err)
	}
	if err := c.Kill(ctx, "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	if b.added[0].Name != "newrepo" || b.removed[0] != "old" || len(b.pauseAll) != 2 || b.token != "github_pat_x" || b.killed[0] != "aaaaaa" {
		t.Fatalf("backend %+v", b)
	}
}

func TestUnreachableDaemon(t *testing.T) {
	c := NewUnixClient("/nonexistent/ghr.sock")
	if _, err := c.Status(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./internal/api/`
Expected: FAIL — FAIL: `undefined: NewServer`

- [ ] **Step 3: Write `internal/api/server.go`**

```go
// Package api is the daemon's HTTP+JSON control API, served on a Unix socket.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/darkraise/ghr/internal/model"
)

// Error carries an HTTP status to the client.
type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

func BadRequest(msg string) error { return &Error{Status: http.StatusBadRequest, Msg: msg} }
func NotFound(msg string) error   { return &Error{Status: http.StatusNotFound, Msg: msg} }
func Conflict(msg string) error   { return &Error{Status: http.StatusConflict, Msg: msg} }

// Backend is implemented by the daemon.
type Backend interface {
	Status() model.Status
	EventsAfter(seq int64) []model.Event
	History(repo, conclusion string, limit int) ([]model.HistoryEntry, error)
	RunnerLog(id, cursor string) (model.LogChunk, error)
	RunnerSteps(ctx context.Context, id string) ([]model.Step, error)
	RunnerContainers(ctx context.Context, id string) ([]model.Container, error)
	Config() any
	PatchConfig(p model.ConfigPatch) error
	AddRepo(ctx context.Context, req model.AddRepoRequest) error
	RemoveRepo(name string) error
	SetPausedAll(paused bool) error
	SetToken(ctx context.Context, token string) error
	KillRunner(ctx context.Context, id string) error
}

func NewServer(b Backend) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, b.Status()) })
	mux.HandleFunc("GET /events", func(w http.ResponseWriter, r *http.Request) {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		ev := b.EventsAfter(after)
		if ev == nil {
			ev = []model.Event{}
		}
		writeJSON(w, ev)
	})
	mux.HandleFunc("GET /history", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		h, err := b.History(q.Get("repo"), q.Get("conclusion"), limit)
		if h == nil {
			h = []model.HistoryEntry{}
		}
		respond(w, h, err)
	})
	mux.HandleFunc("GET /runners/{id}/log", func(w http.ResponseWriter, r *http.Request) {
		c, err := b.RunnerLog(r.PathValue("id"), r.URL.Query().Get("cursor"))
		respond(w, c, err)
	})
	mux.HandleFunc("GET /runners/{id}/containers", func(w http.ResponseWriter, r *http.Request) {
		cs, err := b.RunnerContainers(r.Context(), r.PathValue("id"))
		if cs == nil {
			cs = []model.Container{}
		}
		respond(w, cs, err)
	})
	mux.HandleFunc("GET /runners/{id}/steps", func(w http.ResponseWriter, r *http.Request) {
		s, err := b.RunnerSteps(r.Context(), r.PathValue("id"))
		if s == nil {
			s = []model.Step{}
		}
		respond(w, s, err)
	})
	mux.HandleFunc("DELETE /runners/{id}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.KillRunner(r.Context(), r.PathValue("id")))
	})
	mux.HandleFunc("GET /config", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, b.Config()) })
	mux.HandleFunc("PATCH /config", func(w http.ResponseWriter, r *http.Request) {
		var p model.ConfigPatch
		if err := decode(r, &p); err != nil {
			respond(w, nil, err)
			return
		}
		respond(w, nil, b.PatchConfig(p))
	})
	mux.HandleFunc("POST /repos", func(w http.ResponseWriter, r *http.Request) {
		var req model.AddRepoRequest
		if err := decode(r, &req); err != nil {
			respond(w, nil, err)
			return
		}
		respond(w, nil, b.AddRepo(r.Context(), req))
	})
	mux.HandleFunc("DELETE /repos/{name}", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.RemoveRepo(r.PathValue("name")))
	})
	mux.HandleFunc("POST /repos/{name}/pause", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.PatchConfig(pausePatch(r.PathValue("name"), true)))
	})
	mux.HandleFunc("POST /repos/{name}/resume", func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, b.PatchConfig(pausePatch(r.PathValue("name"), false)))
	})
	mux.HandleFunc("POST /pause-all", func(w http.ResponseWriter, r *http.Request) { respond(w, nil, b.SetPausedAll(true)) })
	mux.HandleFunc("POST /resume-all", func(w http.ResponseWriter, r *http.Request) { respond(w, nil, b.SetPausedAll(false)) })
	mux.HandleFunc("PUT /token", func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
		if err != nil {
			respond(w, nil, err)
			return
		}
		tok := strings.TrimSpace(string(data))
		if tok == "" {
			respond(w, nil, BadRequest("empty token"))
			return
		}
		respond(w, nil, b.SetToken(r.Context(), tok))
	})
	return mux
}

func pausePatch(name string, paused bool) model.ConfigPatch {
	return model.ConfigPatch{Repos: map[string]model.RepoPatch{name: {Paused: &paused}}}
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil {
		return BadRequest("invalid JSON: " + err.Error())
	}
	return nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		var ae *Error
		if errors.As(err, &ae) {
			status = ae.Status
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	if v == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, v)
}
```

- [ ] **Step 4: Write `internal/api/client.go`**

```go
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

const DefaultSocket = "/run/ghr/ghr.sock"

type Client struct {
	Base string // "http://ghr" for the socket
	HTTP *http.Client
}

// NewUnixClient talks to the daemon over its Unix socket.
func NewUnixClient(socket string) *Client {
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}
	return &Client{Base: "http://ghr", HTTP: &http.Client{Transport: tr, Timeout: 30 * time.Second}}
}

func (c *Client) call(ctx context.Context, method, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("ghr daemon unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return &Error{Status: resp.StatusCode, Msg: e.Error}
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func jsonBody(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

func (c *Client) Status(ctx context.Context) (model.Status, error) {
	var s model.Status
	return s, c.call(ctx, http.MethodGet, "/status", nil, &s)
}

func (c *Client) Events(ctx context.Context, after int64) ([]model.Event, error) {
	var e []model.Event
	return e, c.call(ctx, http.MethodGet, fmt.Sprintf("/events?after=%d", after), nil, &e)
}

func (c *Client) History(ctx context.Context, repo, conclusion string, limit int) ([]model.HistoryEntry, error) {
	q := url.Values{}
	q.Set("repo", repo)
	q.Set("conclusion", conclusion)
	q.Set("limit", fmt.Sprint(limit))
	var h []model.HistoryEntry
	return h, c.call(ctx, http.MethodGet, "/history?"+q.Encode(), nil, &h)
}

// Log returns the runner's log after cursor ("" = from the start); pass the
// returned chunk's Next to continue.
func (c *Client) Log(ctx context.Context, id, cursor string) (model.LogChunk, error) {
	var l model.LogChunk
	return l, c.call(ctx, http.MethodGet, "/runners/"+url.PathEscape(id)+"/log?cursor="+url.QueryEscape(cursor), nil, &l)
}

func (c *Client) Containers(ctx context.Context, id string) ([]model.Container, error) {
	var cs []model.Container
	return cs, c.call(ctx, http.MethodGet, "/runners/"+url.PathEscape(id)+"/containers", nil, &cs)
}

func (c *Client) Steps(ctx context.Context, id string) ([]model.Step, error) {
	var s []model.Step
	return s, c.call(ctx, http.MethodGet, "/runners/"+url.PathEscape(id)+"/steps", nil, &s)
}

// Config decodes the daemon's current config into out (normally *config.Config).
func (c *Client) Config(ctx context.Context, out any) error {
	return c.call(ctx, http.MethodGet, "/config", nil, out)
}

func (c *Client) PatchConfig(ctx context.Context, p model.ConfigPatch) error {
	return c.call(ctx, http.MethodPatch, "/config", jsonBody(p), nil)
}

func (c *Client) AddRepo(ctx context.Context, req model.AddRepoRequest) error {
	return c.call(ctx, http.MethodPost, "/repos", jsonBody(req), nil)
}

func (c *Client) RemoveRepo(ctx context.Context, name string) error {
	return c.call(ctx, http.MethodDelete, "/repos/"+url.PathEscape(name), nil, nil)
}

func (c *Client) Pause(ctx context.Context, name string) error {
	return c.call(ctx, http.MethodPost, "/repos/"+url.PathEscape(name)+"/pause", nil, nil)
}

func (c *Client) Resume(ctx context.Context, name string) error {
	return c.call(ctx, http.MethodPost, "/repos/"+url.PathEscape(name)+"/resume", nil, nil)
}

func (c *Client) PauseAll(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/pause-all", nil, nil)
}

func (c *Client) ResumeAll(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/resume-all", nil, nil)
}

func (c *Client) SetToken(ctx context.Context, token string) error {
	return c.call(ctx, http.MethodPut, "/token", strings.NewReader(token), nil)
}

func (c *Client) Kill(ctx context.Context, id string) error {
	return c.call(ctx, http.MethodDelete, "/runners/"+url.PathEscape(id), nil, nil)
}
```

- [ ] **Step 5: Run the check**

Run: `go test -count=1 -timeout 180s ./internal/api/`
Expected: PASS (exit 0).

- [ ] **Step 6: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add internal/api/api_test.go internal/api/server.go internal/api/client.go
git commit -m "feat(api): add control API server and client"
```

---

### Task 12: CLI subcommands

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `cmd/ghr/cli.go`
- Modify: `cmd/ghr/main.go`
- Test: `cmd/ghr/cli_test.go`

**Interfaces:**
- Consumes: `api.Client` and its methods (Contracts → api).
- Produces: CLI subcommands (Contracts → CLI entry).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing test `cmd/ghr/cli_test.go`**

```go
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

type recorded struct {
	method, path, body string
}

func fakeDaemon(t *testing.T) *[]recorded {
	t.Helper()
	var reqs []recorded
	now := time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		reqs = append(reqs, recorded{r.Method, r.URL.RequestURI(), string(b)})
		switch {
		case r.URL.Path == "/status":
			json.NewEncoder(w).Encode(model.Status{
				Now: now, Mode: "queue", GlobalMax: 3, RateRemaining: 4812, DiskPct: 61,
				Repos: []model.RepoStatus{
					{Name: "darkcloud", Max: 1, Active: 1, Queued: 2,
						LastJob: &model.HistoryEntry{Conclusion: "success", RunNumber: "411", JobName: "lint", FinishedAt: now.Add(-2 * time.Minute)}},
					{Name: "darkagents", Paused: true, Max: 0},
				},
				Instances: []model.InstanceStatus{{ID: "a3f9c1", Repo: "darkcloud", State: "busy", Since: now.Add(-12*time.Minute - 4*time.Second),
					Job: &model.JobInfo{Name: "CI / e2e-journeys", RunNumber: "412"}}},
			})
		case r.URL.Path == "/history":
			json.NewEncoder(w).Encode([]model.HistoryEntry{{Repo: "darkmem", RunNumber: "88", JobName: "build", Conclusion: "failure",
				StartedAt: now.Add(-time.Minute), FinishedAt: now}})
		case strings.HasSuffix(r.URL.Path, "/log"):
			json.NewEncoder(w).Encode(model.LogChunk{Data: "hello log\n", Next: "Runner_1.log=10"})
		case r.URL.Path == "/repos" && strings.Contains(string(b), `"public"`) && !strings.Contains(string(b), `"allow_public":true`):
			w.WriteHeader(409)
			json.NewEncoder(w).Encode(map[string]string{"error": "public is public"})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	newClient = func() *api.Client { return &api.Client{Base: srv.URL, HTTP: srv.Client()} }
	return &reqs
}

func runCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func TestStatusOutput(t *testing.T) {
	fakeDaemon(t)
	code, out, _ := runCLI(t, "", "status")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{"mode queue  global 1/3  api 4812  disk 61%", "darkcloud", "1/1", "success #411 lint (2m0s ago)",
		"darkagents", "paused", "0/∞", "a3f9c1", "CI / e2e-journeys #412", "12m4s"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestMutatingCommands(t *testing.T) {
	reqs := fakeDaemon(t)
	cases := [][]string{
		{"pause", "darkcloud"},
		{"resume", "darkcloud"},
		{"drain"},
		{"resume-all"},
		{"set", "mode", "all"},
		{"set", "global-max", "3"},
		{"set", "max", "darkmem", "0"},
		{"set", "warm", "darkmem", "2"},
		{"repo", "add", "newrepo", "--max", "2", "--label", "a", "--label", "b"},
		{"repo", "rm", "old"},
		{"kill", "a3f9c1"},
	}
	for _, args := range cases {
		if code, _, errOut := runCLI(t, "", args...); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, errOut)
		}
	}
	got := []string{}
	for _, r := range *reqs {
		got = append(got, r.method+" "+r.path+" "+r.body)
	}
	want := []string{
		"POST /repos/darkcloud/pause ",
		"POST /repos/darkcloud/resume ",
		"POST /pause-all ",
		"POST /resume-all ",
		`PATCH /config {"mode":"all"}`,
		`PATCH /config {"global_max":3}`,
		`PATCH /config {"repos":{"darkmem":{"max":0}}}`,
		`PATCH /config {"repos":{"darkmem":{"warm":2}}}`,
		`POST /repos {"name":"newrepo","max":2,"labels":["a","b"],"allow_public":false}`,
		"DELETE /repos/old ",
		"DELETE /runners/a3f9c1 ",
	}
	for i := range want {
		if strings.TrimSpace(got[i]) != strings.TrimSpace(want[i]) {
			t.Errorf("request %d:\n got %s\nwant %s", i, got[i], want[i])
		}
	}
}

func TestRepoAddMaxFlag(t *testing.T) {
	reqs := fakeDaemon(t)
	for _, args := range [][]string{
		{"repo", "add", "r1"},
		{"repo", "add", "r2", "--max", "0"},
		{"repo", "add", "r3", "--max", "3"},
	} {
		if code, _, errOut := runCLI(t, "", args...); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, errOut)
		}
	}
	want := []string{
		`{"name":"r1","allow_public":false}`,
		`{"name":"r2","max":0,"allow_public":false}`,
		`{"name":"r3","max":3,"allow_public":false}`,
	}
	for i, w := range want {
		if got := strings.TrimSpace((*reqs)[i].body); got != w {
			t.Errorf("request %d: got %s want %s", i, got, w)
		}
	}
	for _, args := range [][]string{
		{"repo", "add", "r4", "--max", "-2"},
		{"repo", "add", "r5", "--max", "-1"},
		{"repo", "add", "r6", "extra"},
	} {
		if code, _, _ := runCLI(t, "", args...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
	if len(*reqs) != 3 {
		t.Fatalf("rejected adds reached the daemon: %v", *reqs)
	}
}

func TestTokenSetReadsStdin(t *testing.T) {
	reqs := fakeDaemon(t)
	if code, _, _ := runCLI(t, "github_pat_new\n", "token", "set"); code != 0 {
		t.Fatal("token set failed")
	}
	if r := (*reqs)[0]; r.method != "PUT" || r.path != "/token" || r.body != "github_pat_new" {
		t.Fatalf("request %+v", r)
	}
}

func TestErrorsAndUsage(t *testing.T) {
	fakeDaemon(t)
	code, _, errOut := runCLI(t, "", "repo", "add", "public")
	if code != 1 || !strings.Contains(errOut, "public is public") {
		t.Fatalf("exit %d err %s", code, errOut)
	}
	if code, _, _ := runCLI(t, "", "set", "global-max", "lots"); code != 2 {
		t.Fatalf("bad number exit %d", code)
	}
	if code, _, _ := runCLI(t, "", "frobnicate"); code != 2 {
		t.Fatalf("unknown command exit %d", code)
	}
	if code, out, _ := runCLI(t, "", "version"); code != 0 || strings.TrimSpace(out) != "dev" {
		t.Fatalf("version %d %q", code, out)
	}
}

func TestLogsAndHistory(t *testing.T) {
	fakeDaemon(t)
	if code, out, _ := runCLI(t, "", "logs", "a3f9c1"); code != 0 || out != "hello log\n" {
		t.Fatalf("logs %d %q", code, out)
	}
	code, out, _ := runCLI(t, "", "history", "--repo", "darkmem")
	if code != 0 || !strings.Contains(out, "#88") || !strings.Contains(out, "failure") || !strings.Contains(out, "1m0s") {
		t.Fatalf("history %d:\n%s", code, out)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./cmd/ghr/`
Expected: FAIL — FAIL: `undefined: newClient` / `undefined: printStatus`

- [ ] **Step 3: Write `cmd/ghr/cli.go`**

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

type usageError string

func (e usageError) Error() string { return string(e) }

type stringList []string

func (s *stringList) String() string     { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error { *s = append(*s, v); return nil }

func need(args []string, n int, form string) error {
	if len(args) != n {
		return usageError("usage: ghr " + form)
	}
	return nil
}

func atoi(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, usageError("not a number: " + s)
	}
	return n, nil
}

// logPollInterval is how often `logs -f` polls; tests shorten it.
var logPollInterval = time.Second

func cli(ctx context.Context, c *api.Client, args []string, stdin io.Reader, out io.Writer) error {
	switch args[0] {
	case "status":
		st, err := c.Status(ctx)
		if err != nil {
			return err
		}
		printStatus(out, st)
		return nil
	case "pause", "resume":
		if err := need(args, 2, args[0]+" <repo>"); err != nil {
			return err
		}
		if args[0] == "pause" {
			return c.Pause(ctx, args[1])
		}
		return c.Resume(ctx, args[1])
	case "drain":
		return c.PauseAll(ctx)
	case "resume-all":
		return c.ResumeAll(ctx)
	case "set":
		return set(ctx, c, args[1:])
	case "repo":
		return repo(ctx, c, args[1:])
	case "token":
		if err := need(args, 2, "token set"); err != nil || args[1] != "set" {
			return usageError("usage: ghr token set")
		}
		data, err := io.ReadAll(io.LimitReader(stdin, 64*1024))
		if err != nil {
			return err
		}
		return c.SetToken(ctx, strings.TrimSpace(string(data)))
	case "kill":
		if err := need(args, 2, "kill <id>"); err != nil {
			return err
		}
		return c.Kill(ctx, args[1])
	case "logs":
		return logs(ctx, c, args[1:], out)
	case "history":
		return historyCmd(ctx, c, args[1:], out)
	}
	return usageError("unknown command " + args[0])
}

func set(ctx context.Context, c *api.Client, args []string) error {
	if len(args) == 0 {
		return usageError("usage: ghr set <mode|global-max|max|warm> ...")
	}
	switch args[0] {
	case "mode":
		if err := need(args, 2, "set mode <queue|all>"); err != nil {
			return err
		}
		return c.PatchConfig(ctx, model.ConfigPatch{Mode: &args[1]})
	case "global-max":
		if err := need(args, 2, "set global-max <n>"); err != nil {
			return err
		}
		n, err := atoi(args[1])
		if err != nil {
			return err
		}
		return c.PatchConfig(ctx, model.ConfigPatch{GlobalMax: &n})
	case "max", "warm":
		if err := need(args, 3, "set "+args[0]+" <repo> <n>"); err != nil {
			return err
		}
		n, err := atoi(args[2])
		if err != nil {
			return err
		}
		rp := model.RepoPatch{Max: &n}
		if args[0] == "warm" {
			rp = model.RepoPatch{Warm: &n}
		}
		return c.PatchConfig(ctx, model.ConfigPatch{Repos: map[string]model.RepoPatch{args[1]: rp}})
	}
	return usageError("unknown setting " + args[0])
}

func repo(ctx context.Context, c *api.Client, args []string) error {
	if len(args) < 2 {
		return usageError("usage: ghr repo <add|rm> <name>")
	}
	switch args[0] {
	case "rm":
		if err := need(args, 2, "repo rm <name>"); err != nil {
			return err
		}
		return c.RemoveRepo(ctx, args[1])
	case "add":
		fs := flag.NewFlagSet("repo add", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		max := fs.Int("max", 0, "")
		allowPublic := fs.Bool("allow-public", false, "")
		var labels stringList
		fs.Var(&labels, "label", "")
		if err := fs.Parse(args[2:]); err != nil {
			return usageError(err.Error())
		}
		if fs.NArg() > 0 {
			return usageError("unexpected argument " + fs.Arg(0))
		}
		req := model.AddRepoRequest{Name: args[1], Labels: labels, AllowPublic: *allowPublic}
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "max" {
				req.Max = max
			}
		})
		if req.Max != nil && *req.Max < 0 {
			return usageError("--max must be >= 0 (0 = unlimited)")
		}
		return c.AddRepo(ctx, req)
	}
	return usageError("unknown repo command " + args[0])
}

func logs(ctx context.Context, c *api.Client, args []string, out io.Writer) error {
	follow := false
	var id string
	for _, a := range args {
		if a == "-f" {
			follow = true
		} else {
			id = a
		}
	}
	if id == "" {
		return usageError("usage: ghr logs <id> [-f]")
	}
	cursor := ""
	for {
		chunk, err := c.Log(ctx, id, cursor)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		io.WriteString(out, chunk.Data)
		cursor = chunk.Next
		if !follow {
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(logPollInterval):
		}
	}
}

func historyCmd(ctx context.Context, c *api.Client, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("history", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", "", "")
	conclusion := fs.String("conclusion", "", "")
	limit := fs.Int("limit", 20, "")
	if err := fs.Parse(args); err != nil {
		return usageError(err.Error())
	}
	h, err := c.History(ctx, *repo, *conclusion, *limit)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "FINISHED\tREPO\tRUN\tJOB\tRESULT\tDURATION")
	for _, e := range h {
		fmt.Fprintf(w, "%s\t%s\t#%s\t%s\t%s\t%s\n", e.FinishedAt.Local().Format("2006-01-02 15:04"), e.Repo, e.RunNumber, e.JobName,
			e.Conclusion, e.FinishedAt.Sub(e.StartedAt).Round(time.Second))
	}
	return w.Flush()
}

func maxText(n int) string {
	if n == 0 {
		return "∞"
	}
	return strconv.Itoa(n)
}

func printStatus(out io.Writer, st model.Status) {
	running := 0
	for _, i := range st.Instances {
		if i.State != "cleaning" {
			running++
		}
	}
	global := fmt.Sprintf("%d/%d", running, st.GlobalMax)
	if st.Mode == "all" {
		global = fmt.Sprintf("%d/∞", running)
	}
	fmt.Fprintf(out, "mode %s  global %s  api %d  disk %d%%\n", st.Mode, global, st.RateRemaining, st.DiskPct)
	if st.Degraded {
		fmt.Fprintf(out, "DEGRADED: %s\n", st.DegradedReason)
	}
	fmt.Fprintln(out)
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "REPO\tSTATE\tRUN\tQUEUE\tLAST JOB")
	for _, r := range st.Repos {
		state := "active"
		if r.Paused {
			state = "paused"
		}
		if r.Error != "" {
			state = "error: " + r.Error
		}
		last := "-"
		if r.LastJob != nil {
			last = fmt.Sprintf("%s #%s %s (%s ago)", r.LastJob.Conclusion, r.LastJob.RunNumber, r.LastJob.JobName,
				st.Now.Sub(r.LastJob.FinishedAt).Round(time.Minute))
		}
		fmt.Fprintf(w, "%s\t%s\t%d/%s\t%d\t%s\n", r.Name, state, r.Active, maxText(r.Max), r.Queued, last)
	}
	w.Flush()
	if len(st.Instances) == 0 {
		return
	}
	fmt.Fprintln(out)
	w = tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "RUNNER\tREPO\tSTATE\tJOB\tELAPSED")
	for _, i := range st.Instances {
		job := "-"
		if i.Job != nil {
			job = fmt.Sprintf("%s #%s", i.Job.Name, i.Job.RunNumber)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", i.ID, i.Repo, i.State, job, st.Now.Sub(i.Since).Round(time.Second))
	}
	w.Flush()
}
```

- [ ] **Step 4: Write `cmd/ghr/main.go`** `cmd/ghr/main.go` already exists from Task 1; replace its whole content.

```go
// Command ghr manages native GitHub Actions runners: `ghr daemon` runs the supervisor,
// every other subcommand talks to it over its Unix socket.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/darkraise/ghr/internal/api"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func socketPath() string {
	if s := os.Getenv("GHR_SOCKET"); s != "" {
		return s
	}
	return api.DefaultSocket
}

// newClient is replaced in tests.
var newClient = func() *api.Client { return api.NewUnixClient(socketPath()) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := cli(ctx, newClient(), args, stdin, stdout); err != nil {
		fmt.Fprintln(stderr, "ghr:", err)
		if _, ok := err.(usageError); ok {
			fmt.Fprint(stderr, usage)
			return 2
		}
		return 1
	}
	return 0
}

const usage = `usage: ghr <command>

  daemon                          run the supervisor (systemd runs this)
  tui                             interactive dashboard
  status                          repos, runners and health
  pause <repo> | resume <repo>    stop/start new runners for a repo
  drain | resume-all              pause/resume every repo
  set mode <queue|all>
  set global-max <n>
  set max <repo> <n>              0 = unlimited
  set warm <repo> <n>
  repo add <name> [--max n] [--label l]... [--allow-public]
  repo rm <name>                  removed once its runners finish
  token set                       read a new PAT from stdin
  kill <id>                       stop a runner
  logs <id> [-f]                  runner diagnostic log
  history [--repo r] [--conclusion c] [--limit n]
  version
`
```

- [ ] **Step 5: Run the check**

Run: `go test -count=1 -timeout 180s ./cmd/ghr/`
Expected: PASS (exit 0).

- [ ] **Step 6: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add cmd/ghr/cli_test.go cmd/ghr/cli.go cmd/ghr/main.go
git commit -m "feat(cli): add status and management commands"
```

---

### Task 13: Runner manager core

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `internal/runner/manager.go`
- Test: `internal/runner/fakes_test.go`
- Test: `internal/runner/manager_test.go`

**Interfaces:**
- Consumes: `config`, `events.Ring`, `github` types, `history.Store`, `model`, `sched`, `system` types (Contracts).
- Produces: the exported `runner` API, the Task 13 internals and the runner test harness (Contracts → runner — exported, internals, test harness).

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing test `internal/runner/fakes_test.go`**

```go
package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/history"
	"github.com/darkraise/ghr/internal/system"
)

type fakeGH struct {
	mu        sync.Mutex
	runs      map[string][]github.Run // key repo/status
	jobs      map[int64][]github.Job
	runners   map[int64]*github.Runner
	listed    map[string][]github.Runner
	deleted   []int64
	jitCalls  []string
	calls     []string // "<method> <repo>" of every call
	nextID    int64
	errs      map[string]error // key "<method> <repo>"
	remaining int
}

func newFakeGH() *fakeGH {
	return &fakeGH{runs: map[string][]github.Run{}, jobs: map[int64][]github.Job{}, runners: map[int64]*github.Runner{},
		listed: map[string][]github.Runner{}, errs: map[string]error{}, nextID: 100, remaining: 4999}
}

// call records the call and returns its injected error, if any.
func (f *fakeGH) call(method, repo string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method+" "+repo)
	return f.errs[method+" "+repo]
}

func (f *fakeGH) setErr(key string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.errs, key)
		return
	}
	f.errs[key] = err
}

func (f *fakeGH) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeGH) ListRuns(_ context.Context, repo, status string) ([]github.Run, error) {
	if err := f.call("ListRuns", repo); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs[repo+"/"+status], nil
}

func (f *fakeGH) ListJobs(_ context.Context, repo string, runID int64) ([]github.Job, error) {
	if err := f.call("ListJobs", repo); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.jobs[runID], nil
}

func (f *fakeGH) ListRunners(_ context.Context, repo string) ([]github.Runner, error) {
	if err := f.call("ListRunners", repo); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.listed[repo], nil
}

func (f *fakeGH) GetRunner(_ context.Context, repo string, id int64) (*github.Runner, error) {
	if err := f.call("GetRunner", repo); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.runners[id]
	if !ok {
		return nil, &github.APIError{Status: 404, Kind: github.ErrNotFound}
	}
	c := *r
	return &c, nil
}

func (f *fakeGH) DeleteRunner(_ context.Context, repo string, id int64) error {
	if err := f.call("DeleteRunner", repo); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleted = append(f.deleted, id)
	delete(f.runners, id)
	return nil
}

func (f *fakeGH) GenerateJITConfig(_ context.Context, repo, name string, labels []string) (*github.JITConfig, error) {
	if err := f.call("GenerateJITConfig", repo); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.jitCalls = append(f.jitCalls, name+" "+strings.Join(labels, ","))
	f.runners[f.nextID] = &github.Runner{ID: f.nextID, Name: name, Status: "offline"}
	return &github.JITConfig{Runner: github.Runner{ID: f.nextID, Name: name}, EncodedJITConfig: "ENC-" + name}, nil
}

func (f *fakeGH) RateRemaining() int { return f.remaining }

func (f *fakeGH) setRunner(id int64, status string, busy bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runners[id] = &github.Runner{ID: id, Status: status, Busy: busy}
}

func (f *fakeGH) setJobs(runID int64, jobs ...github.Job) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.jobs[runID] = jobs
}

type fakeSD struct {
	mu        sync.Mutex
	started   []system.UnitSpec
	stopped   []string
	active    map[string]bool
	stopErr   map[string]error // by unit
	activeErr map[string]error // by unit
}

func newFakeSD() *fakeSD {
	return &fakeSD{active: map[string]bool{}, stopErr: map[string]error{}, activeErr: map[string]error{}}
}

func (f *fakeSD) Start(_ context.Context, u system.UnitSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, u)
	f.active[u.Unit] = true
	return nil
}

func (f *fakeSD) Stop(_ context.Context, unit string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.stopErr[unit]; err != nil {
		return err
	}
	f.stopped = append(f.stopped, unit)
	f.active[unit] = false
	return nil
}

func (f *fakeSD) Active(_ context.Context, unit string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.activeErr[unit]; err != nil {
		return false, err
	}
	return f.active[unit], nil
}

func (f *fakeSD) List(_ context.Context, prefix string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for u, a := range f.active {
		if a && strings.HasPrefix(u, prefix) {
			out = append(out, u)
		}
	}
	sort.Strings(out)
	return out, nil
}

type fakeDocker struct {
	mu        sync.Mutex
	compose   []system.ComposeContainer
	named     []system.NamedContainer
	byProject map[string][]system.ProjectContainer
	byLabel   map[string][]string
	removed   []string
	netLabels []string
	volLabels []string
	usage     []int // successive DataRootUsage results; last value repeats
	prunes    []string
	errs      map[string]error // by method name
}

func newFakeDocker() *fakeDocker {
	return &fakeDocker{byLabel: map[string][]string{}, byProject: map[string][]system.ProjectContainer{}, errs: map[string]error{}}
}

func (f *fakeDocker) err(method string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.errs[method]
}

func (f *fakeDocker) setErr(method string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err == nil {
		delete(f.errs, method)
		return
	}
	f.errs[method] = err
}

func (f *fakeDocker) ComposeContainers(context.Context) ([]system.ComposeContainer, error) {
	if err := f.err("ComposeContainers"); err != nil {
		return nil, err
	}
	return f.compose, nil
}
func (f *fakeDocker) Containers(context.Context) ([]system.NamedContainer, error) {
	if err := f.err("Containers"); err != nil {
		return nil, err
	}
	return f.named, nil
}
func (f *fakeDocker) ProjectContainers(_ context.Context, project string) ([]system.ProjectContainer, error) {
	if err := f.err("ProjectContainers"); err != nil {
		return nil, err
	}
	return f.byProject[project], nil
}
func (f *fakeDocker) ContainerIDsByLabel(_ context.Context, label string) ([]string, error) {
	if err := f.err("ContainerIDsByLabel"); err != nil {
		return nil, err
	}
	return f.byLabel[label], nil
}
func (f *fakeDocker) RemoveContainers(_ context.Context, ids []string) error {
	if err := f.err("RemoveContainers"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, ids...)
	return nil
}
func (f *fakeDocker) RemoveNetworksByLabel(_ context.Context, label string) error {
	if err := f.err("RemoveNetworksByLabel"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.netLabels = append(f.netLabels, label)
	return nil
}
func (f *fakeDocker) RemoveVolumesByLabel(_ context.Context, label string) error {
	if err := f.err("RemoveVolumesByLabel"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.volLabels = append(f.volLabels, label)
	return nil
}

// DataRootUsage returns successive f.usage values; a negative value is a failed read.
func (f *fakeDocker) DataRootUsage(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.usage) == 0 {
		return 10, nil
	}
	v := f.usage[0]
	if len(f.usage) > 1 {
		f.usage = f.usage[1:]
	}
	if v < 0 {
		return 0, fmt.Errorf("df failed")
	}
	return v, nil
}
func (f *fakeDocker) PruneBuildCacheOlderThan(_ context.Context, h int) (string, error) {
	f.prunes = append(f.prunes, fmt.Sprintf("until=%dh", h))
	if err := f.err("PruneBuildCacheOlderThan"); err != nil {
		return "", err
	}
	return "1GB", nil
}
func (f *fakeDocker) PruneBuildCacheTo(_ context.Context, keep string) (string, error) {
	f.prunes = append(f.prunes, "keep="+keep)
	if err := f.err("PruneBuildCacheTo"); err != nil {
		return "", err
	}
	return "5GB", nil
}
func (f *fakeDocker) PruneDanglingImages(context.Context) (string, error) {
	f.prunes = append(f.prunes, "images")
	if err := f.err("PruneDanglingImages"); err != nil {
		return "", err
	}
	return "200MB", nil
}

// fakeHost copies by creating the destination with a run.sh, like a real dist.
type fakeHost struct {
	mu      sync.Mutex
	chowned []string
}

func (f *fakeHost) CopyTree(_ context.Context, src, dst string) error {
	if err := os.MkdirAll(filepath.Join(dst, "_diag"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dst, "run.sh"), []byte("#!/bin/sh\n"), 0o755)
}
func (f *fakeHost) ChownR(_ context.Context, path, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.chowned = append(f.chowned, path+":"+user)
	return nil
}

type harness struct {
	m      *Manager
	cfg    *config.Config
	gh     *fakeGH
	sd     *fakeSD
	docker *fakeDocker
	host   *fakeHost
	now    time.Time
	ids    []string
	root   string
}

const testConfig = `
owner: darkraise
mode: queue
global_max: 2
labels: [homelab]
repos:
  - name: darkcloud
    max: 1
    cleanup_name_prefixes: [dc-e2e-]
  - name: darkmem
`

func newHarness(t *testing.T) *harness {
	t.Helper()
	cfg, _, err := config.Parse([]byte(testConfig))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dist := filepath.Join(root, "dist", "2.330.0")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatal(err)
	}
	h := &harness{cfg: cfg, gh: newFakeGH(), sd: newFakeSD(), docker: newFakeDocker(), host: &fakeHost{},
		now: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), root: root,
		ids: []string{"aaaaaa", "bbbbbb", "cccccc", "dddddd", "eeeeee", "ffffff"}}
	ev := events.New()
	ev.Now = func() time.Time { return h.now }
	h.m = &Manager{
		Config: func() *config.Config { return h.cfg },
		GH:     h.gh, SD: h.sd, Docker: h.docker, Host: h.host,
		Paths: Paths{Dist: dist, Instances: filepath.Join(root, "instances"), Logs: filepath.Join(root, "logs"),
			Pending: filepath.Join(root, "pending"), ToolCache: filepath.Join(root, "toolcache"),
			Hooks: "/opt/ghr/hooks", Home: "/home/ghrunner"},
		Events:  ev,
		History: &history.Store{Path: filepath.Join(root, "history.jsonl")},
		Now:     func() time.Time { return h.now },
		NewID: func() string {
			id := h.ids[0]
			h.ids = h.ids[1:]
			return id
		},
	}
	if err := h.m.Init(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(h.m.Paths.Instances, 0o755); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) state(id string) string {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	if i, ok := h.m.insts[id]; ok {
		return string(i.State)
	}
	return "gone"
}

// writeJob writes a job-started record; extra fields (such as finished_at) are appended.
func (h *harness) writeJob(t *testing.T, id string, runID int64, extra ...string) {
	t.Helper()
	rec := fmt.Sprintf(`{"run_id":"%d","run_attempt":"1","run_number":"412","workflow":"CI","job":"e2e","runner_name":"x","started_at":"2026-10-03T12:00:00Z"`, runID)
	for _, e := range extra {
		rec += "," + e
	}
	if err := os.WriteFile(filepath.Join(h.m.instanceDir(id), JobFile), []byte(rec+"}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) eventText() string {
	var b strings.Builder
	for _, e := range h.m.Events.After(0) {
		b.WriteString(e.Level + " " + e.Repo + " " + e.Msg + "\n")
	}
	return b.String()
}

func (h *harness) history(t *testing.T) []string {
	t.Helper()
	got, err := h.m.History.Query("", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range got {
		out = append(out, e.ID+" "+e.Conclusion)
	}
	return out
}
```

- [ ] **Step 2: Write the failing test `internal/runner/manager_test.go`**

```go
package runner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
)

func TestInitLoadsLastJobPerRepo(t *testing.T) {
	h := newHarness(t)
	h.m.History.Append(model.HistoryEntry{ID: "1", Repo: "darkmem", RunNumber: "1", FinishedAt: h.now.Add(-time.Hour)})
	h.m.History.Append(model.HistoryEntry{ID: "2", Repo: "darkmem", RunNumber: "2", FinishedAt: h.now})
	if err := h.m.Init(); err != nil {
		t.Fatal(err)
	}
	st := h.m.Status()
	if st.Repos[1].Name != "darkmem" || st.Repos[1].LastJob == nil || st.Repos[1].LastJob.RunNumber != "2" {
		t.Fatalf("repos %+v", st.Repos)
	}
	if st.Repos[0].LastJob != nil {
		t.Fatal("darkcloud has no history")
	}
}

// Cleanup finishes out of order: an older job finalized later must not replace the last job.
func TestLastJobKeepsNewest(t *testing.T) {
	h := newHarness(t)
	h.m.mu.Lock()
	h.m.recordLastJob(model.HistoryEntry{ID: "new", Repo: "darkmem", FinishedAt: h.now})
	h.m.recordLastJob(model.HistoryEntry{ID: "old", Repo: "darkmem", FinishedAt: h.now.Add(-time.Hour)})
	h.m.mu.Unlock()
	if got := h.m.Status().Repos[1].LastJob; got == nil || got.ID != "new" {
		t.Fatalf("last job %+v", got)
	}
}

func TestStatusEpochChangesPerStart(t *testing.T) {
	h := newHarness(t)
	first := h.m.Status().Epoch
	h.now = h.now.Add(time.Second)
	if err := h.m.Init(); err != nil {
		t.Fatal(err)
	}
	if first == "" || h.m.Status().Epoch == first {
		t.Fatalf("epochs %q %q", first, h.m.Status().Epoch)
	}
}

func TestStatusCountsAndSorts(t *testing.T) {
	h := newHarness(t)
	h.m.mu.Lock()
	h.m.insts["bbbbbb"] = &instance{Meta: Meta{ID: "bbbbbb", Repo: "darkmem"}, State: sched.Busy, StateSince: h.now}
	h.m.insts["aaaaaa"] = &instance{Meta: Meta{ID: "aaaaaa", Repo: "darkmem"}, State: sched.Cleaning, StateSince: h.now}
	h.m.insts["cccccc"] = &instance{Meta: Meta{ID: "cccccc", Repo: "darkcloud"}, State: sched.Idle, StateSince: h.now}
	h.m.demand = sched.Demand{"darkcloud": {{Repo: "darkcloud", ID: 1}, {Repo: "darkcloud", ID: 2}}}
	h.m.mu.Unlock()
	st := h.m.Status()
	if st.Mode != "queue" || st.GlobalMax != 2 || st.RateRemaining != 4999 {
		t.Fatalf("header %+v", st)
	}
	if st.Repos[0].Active != 1 || st.Repos[0].Queued != 2 || st.Repos[0].Max != 1 || st.Repos[1].Active != 2 || st.Repos[1].Max != 1 {
		t.Fatalf("repos %+v", st.Repos)
	}
	var order []string
	for _, i := range st.Instances {
		order = append(order, i.ID)
	}
	if len(order) != 3 || order[0] != "cccccc" || order[1] != "aaaaaa" || order[2] != "bbbbbb" {
		t.Fatalf("instances not sorted by repo then id: %v", order)
	}
}

func TestUnknownRunner(t *testing.T) {
	h := newHarness(t)
	var u ErrUnknownRunner
	if err := h.m.Kill(context.Background(), "zzzzzz"); !errors.As(err, &u) {
		t.Fatalf("kill err %v", err)
	}
	if _, _, _, err := h.m.RunnerRepoAndRun("zzzzzz"); !errors.As(err, &u) {
		t.Fatalf("lookup err %v", err)
	}
	h.m.mu.Lock()
	h.m.insts["aaaaaa"] = &instance{Meta: Meta{ID: "aaaaaa", Repo: "darkcloud", RunnerName: "ghr-darkcloud-aaaaaa"},
		Job: &model.JobInfo{RunID: 55}}
	h.m.mu.Unlock()
	repo, runID, name, err := h.m.RunnerRepoAndRun("aaaaaa")
	if err != nil || repo != "darkcloud" || runID != 55 || name != "ghr-darkcloud-aaaaaa" {
		t.Fatalf("lookup %s %d %s %v", repo, runID, name, err)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./internal/runner/`
Expected: FAIL — FAIL: `undefined: Manager`

- [ ] **Step 4: Write `internal/runner/manager.go`**

```go
// Package runner owns the runner instances: spawning, state tracking, cleanup and reconciliation.
package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/history"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
	"github.com/darkraise/ghr/internal/system"
)

const (
	RunnerUser = "ghrunner"
	UnitPrefix = "ghr-runner-"
	MetaFile   = "ghr.json"
	JobFile    = "job.json"
)

const (
	authRecheck = 60 * time.Second
	maxBackoff  = 2 * time.Minute
)

var idRe = regexp.MustCompile(`^[0-9a-f]{6}$`)

type GitHub interface {
	ListRuns(ctx context.Context, repo, status string) ([]github.Run, error)
	ListJobs(ctx context.Context, repo string, runID int64) ([]github.Job, error)
	ListRunners(ctx context.Context, repo string) ([]github.Runner, error)
	GetRunner(ctx context.Context, repo string, id int64) (*github.Runner, error)
	DeleteRunner(ctx context.Context, repo string, id int64) error
	GenerateJITConfig(ctx context.Context, repo, name string, labels []string) (*github.JITConfig, error)
	RateRemaining() int
}

type Systemd interface {
	Start(ctx context.Context, u system.UnitSpec) error
	Stop(ctx context.Context, unit string) error
	Active(ctx context.Context, unit string) (bool, error)
	List(ctx context.Context, prefix string) ([]string, error)
}

type Docker interface {
	ComposeContainers(ctx context.Context) ([]system.ComposeContainer, error)
	Containers(ctx context.Context) ([]system.NamedContainer, error)
	ProjectContainers(ctx context.Context, project string) ([]system.ProjectContainer, error)
	ContainerIDsByLabel(ctx context.Context, label string) ([]string, error)
	RemoveContainers(ctx context.Context, ids []string) error
	RemoveNetworksByLabel(ctx context.Context, label string) error
	RemoveVolumesByLabel(ctx context.Context, label string) error
	DataRootUsage(ctx context.Context) (int, error)
	PruneBuildCacheOlderThan(ctx context.Context, hours int) (string, error)
	PruneBuildCacheTo(ctx context.Context, keep string) (string, error)
	PruneDanglingImages(ctx context.Context) (string, error)
}

type Host interface {
	CopyTree(ctx context.Context, src, dst string) error
	ChownR(ctx context.Context, path, user string) error
}

// Paths are the on-disk locations ghr uses. Dist may be a symlink to the current runner version.
type Paths struct {
	Dist      string
	Instances string
	Logs      string
	Pending   string // history records waiting for the job's final conclusion
	ToolCache string
	Hooks     string
	Home      string
}

// Meta is written to <instance>/ghr.json at spawn and read back on re-adoption.
type Meta struct {
	ID          string    `json:"id"`
	Repo        string    `json:"repo"`
	RunnerID    int64     `json:"runner_id"`
	RunnerName  string    `json:"runner_name"`
	Labels      []string  `json:"labels"` // the custom labels the runner was registered with
	DistVersion string    `json:"dist_version"`
	SpawnedAt   time.Time `json:"spawned_at"`
}

// JobRecord is <instance>/job.json: written by hooks/job-started.sh, and
// job-completed.sh adds FinishedAt. Times are RFC3339 UTC strings.
type JobRecord struct {
	RunID      string `json:"run_id"`
	RunAttempt string `json:"run_attempt"`
	RunNumber  string `json:"run_number"`
	Workflow   string `json:"workflow"`
	Job        string `json:"job"`
	RunnerName string `json:"runner_name"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
}

type instance struct {
	Meta
	State        sched.State
	StateSince   time.Time
	JobConfirmed bool
	Job          *model.JobInfo
	finishing    bool      // a finish goroutine is running
	retryFinish  time.Time // when a failed finish may run again
}

type Manager struct {
	Config  func() *config.Config
	GH      GitHub
	SD      Systemd
	Docker  Docker
	Host    Host
	Paths   Paths
	Events  *events.Ring
	History *history.Store
	Now     func() time.Time
	NewID   func() string

	mu             sync.Mutex
	insts          map[string]*instance
	demand         sched.Demand
	repoErr        map[string]string
	fails          map[string]int
	retryAt        map[string]time.Time
	degraded       bool
	degradedReason string
	authCheckAt    time.Time
	pauseUntil     time.Time
	diskPct        int
	ticks          int
	lastPrune      time.Time
	lastJob        map[string]model.HistoryEntry
	epoch          string
	wg             sync.WaitGroup
}

// Init prepares internal state and loads the last finished job per repo from history.
func (m *Manager) Init() error {
	m.insts = map[string]*instance{}
	m.demand = sched.Demand{}
	m.repoErr = map[string]string{}
	m.fails = map[string]int{}
	m.retryAt = map[string]time.Time{}
	m.lastJob = map[string]model.HistoryEntry{}
	m.lastPrune = m.Now()
	m.epoch = strconv.FormatInt(m.Now().UnixNano(), 36)
	entries, err := m.History.Query("", "", 0)
	if err != nil {
		return err
	}
	for _, e := range entries {
		m.recordLastJob(e)
	}
	return nil
}

// recordLastJob keeps the newest finished job per repo; m.mu must be held or unshared.
func (m *Manager) recordLastJob(e model.HistoryEntry) {
	if cur, ok := m.lastJob[e.Repo]; !ok || e.FinishedAt.After(cur.FinishedAt) {
		m.lastJob[e.Repo] = e
	}
}

// Wait blocks until background finishes return; each is bounded by finishTimeout.
func (m *Manager) Wait() { m.wg.Wait() }

func (m *Manager) instanceDir(id string) string { return filepath.Join(m.Paths.Instances, id) }

func (m *Manager) setState(id string, s sched.State) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i, ok := m.insts[id]; ok && i.State != s && i.State != sched.Cleaning {
		i.State = s
		i.StateSince = m.Now()
	}
}

func (m *Manager) snapshot() []instance {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]instance, 0, len(m.insts))
	for _, i := range m.insts {
		out = append(out, *i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

// stale reports whether an instance was registered with labels other than its
// repo's current custom labels; an unconfigured repo's instance is not stale.
func stale(cfg *config.Config, i instance) bool {
	r := cfg.Repo(i.Repo)
	if r == nil {
		return false
	}
	want := cfg.CustomLabels(*r)
	got := append([]string{}, i.Labels...)
	sort.Strings(want)
	sort.Strings(got)
	return !slices.Equal(want, got)
}

func (m *Manager) schedInstances(cfg *config.Config) []sched.Instance {
	var out []sched.Instance
	for _, i := range m.snapshot() {
		out = append(out, sched.Instance{ID: i.ID, Repo: i.Repo, State: i.State, StateSince: i.StateSince,
			JobConfirmed: i.JobConfirmed, Stale: stale(cfg, i)})
	}
	return out
}

func writeJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// writeJSONAtomic writes via a temp file and rename so a crash never leaves a partial file.
func writeJSONAtomic(path string, v any) error {
	tmp := path + ".tmp"
	if err := writeJSON(tmp, v); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// Status renders the state for the control API.
func (m *Manager) Status() model.Status {
	cfg := m.Config()
	m.mu.Lock()
	defer m.mu.Unlock()
	st := model.Status{
		Now: m.Now(), Epoch: m.epoch, Mode: cfg.Mode, GlobalMax: cfg.GlobalMax,
		Degraded: m.degraded, DegradedReason: m.degradedReason,
		RateRemaining: m.GH.RateRemaining(), DiskPct: m.diskPct,
		Repos: []model.RepoStatus{}, Instances: []model.InstanceStatus{},
	}
	for _, r := range cfg.Repos {
		rs := model.RepoStatus{Name: r.Name, Paused: r.Paused, Removing: r.Removing, Max: cfg.EffectiveMax(r),
			Queued: len(m.demand[r.Name]), Error: m.repoErr[r.Name]}
		for _, i := range m.insts {
			if i.Repo == r.Name {
				rs.Active++
			}
		}
		if e, ok := m.lastJob[r.Name]; ok {
			e := e
			rs.LastJob = &e
		}
		st.Repos = append(st.Repos, rs)
	}
	for _, i := range m.insts {
		st.Instances = append(st.Instances, model.InstanceStatus{
			ID: i.ID, Repo: i.Repo, RunnerName: i.RunnerName, State: string(i.State), Since: i.StateSince, Job: i.Job,
		})
	}
	sort.Slice(st.Instances, func(a, b int) bool {
		if st.Instances[a].Repo != st.Instances[b].Repo {
			return st.Instances[a].Repo < st.Instances[b].Repo
		}
		return st.Instances[a].ID < st.Instances[b].ID
	})
	return st
}

// ErrUnknownRunner is returned for an ID with no live instance.
type ErrUnknownRunner string

func (e ErrUnknownRunner) Error() string { return "unknown runner " + string(e) }

// Kill stops a runner's unit; its exit is handled by the next tick.
func (m *Manager) Kill(ctx context.Context, id string) error {
	m.mu.Lock()
	i, ok := m.insts[id]
	m.mu.Unlock()
	if !ok {
		return ErrUnknownRunner(id)
	}
	if err := m.SD.Stop(ctx, UnitPrefix+id); err != nil {
		return err
	}
	m.Events.Add("warn", i.Repo, "runner %s stopped by user", id)
	return nil
}

// RunnerRepoAndRun returns the repo, run ID and runner name of an instance's current job, for step lookups.
func (m *Manager) RunnerRepoAndRun(id string) (repo string, runID int64, runnerName string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.insts[id]
	if !ok {
		return "", 0, "", ErrUnknownRunner(id)
	}
	if i.Job == nil {
		return i.Repo, 0, i.RunnerName, nil
	}
	return i.Repo, i.Job.RunID, i.RunnerName, nil
}

func (m *Manager) isDegraded() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.degraded
}

func (m *Manager) apiAllowed(now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if now.Before(m.pauseUntil) {
		return false
	}
	return !m.degraded || !now.Before(m.authCheckAt)
}

// ClearDegraded is called after the token is replaced or reloaded.
func (m *Manager) ClearDegraded() {
	m.mu.Lock()
	m.degraded = false
	m.degradedReason = ""
	m.mu.Unlock()
}

// apiErr applies the error-handling table: auth → degraded, rate limit → pause,
// 404 → per-repo access error, anything else → per-repo backoff.
func (m *Manager) apiErr(repo string, err error, now time.Time) {
	var ae *github.APIError
	if errors.As(err, &ae) {
		switch ae.Kind {
		case github.ErrAuth:
			m.mu.Lock()
			was := m.degraded
			m.degraded = true
			m.degradedReason = ae.Error()
			m.authCheckAt = now.Add(authRecheck)
			m.mu.Unlock()
			if !was {
				m.Events.Add("error", repo, "GitHub rejected the token (%v); spawning stopped", ae)
			}
			return
		case github.ErrRateLimit:
			m.mu.Lock()
			was := now.Before(m.pauseUntil)
			if ae.RetryAt.After(m.pauseUntil) {
				m.pauseUntil = ae.RetryAt
			}
			m.mu.Unlock()
			if !was {
				m.Events.Add("warn", repo, "GitHub rate limit; pausing API calls until %s", ae.RetryAt.Format(time.TimeOnly))
			}
			return
		}
	}
	msg := err.Error()
	if github.IsKind(err, github.ErrNotFound) {
		msg = "token lacks access to repo (add it to the PAT's repository access)"
	}
	m.mu.Lock()
	m.fails[repo]++
	d := 10 * time.Second << (m.fails[repo] - 1)
	if d > maxBackoff || d <= 0 {
		d = maxBackoff
	}
	m.retryAt[repo] = now.Add(d)
	m.repoErr[repo] = msg
	m.mu.Unlock()
	m.Events.Add("warn", repo, "%s; retrying in %s", msg, d)
}
```

- [ ] **Step 5: Run the check**

Run: `go test -count=1 -timeout 180s ./internal/runner/`
Expected: PASS (exit 0).

- [ ] **Step 6: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 7: Commit**

```bash
git add internal/runner/fakes_test.go internal/runner/manager_test.go internal/runner/manager.go
git commit -m "feat(runner): add manager state, status and kill"
```

---

### Task 14: Runner cleanup, disk pruning and logs

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `internal/runner/cleanup.go`
- Test: `internal/runner/cleanup_test.go`

**Interfaces:**
- Consumes: the Task 13 internals and test harness (Contracts → runner); `system.ProjectContainer` (Contracts → system).
- Produces: the Task 14 internals, `Manager.RunnerLog`, `Manager.RunnerContainers` (Contracts → runner).

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 3 = 5

- [ ] **Step 1: Write the failing test `internal/runner/cleanup_test.go`**

```go
package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
	"github.com/darkraise/ghr/internal/system"
)

func TestCleanupDockerProjectsAndPrefixes(t *testing.T) {
	h := newHarness(t)
	dir := h.m.instanceDir("aaaaaa")
	h.docker.compose = []system.ComposeContainer{
		{ID: "c1", Project: "custom-p", WorkingDir: filepath.Join(dir, "_work", "darkcloud", "darkcloud")},
		{ID: "c2", Project: "elsewhere", WorkingDir: filepath.Join(h.root, "instances", "bbbbbb", "_work")},
	}
	h.docker.byLabel["com.docker.compose.project=ghr-aaaaaa"] = []string{"c0"}
	h.docker.byLabel["com.docker.compose.project=custom-p"] = []string{"c1"}
	h.docker.named = []system.NamedContainer{{ID: "c3", Name: "dc-e2e-web"}, {ID: "c4", Name: "unrelated"}}

	removed, err := h.m.cleanupDocker(context.Background(), h.cfg, "aaaaaa", "darkcloud")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(h.docker.removed)
	if removed != 3 || !reflect.DeepEqual(h.docker.removed, []string{"c0", "c1", "c3"}) {
		t.Fatalf("removed %d %v", removed, h.docker.removed)
	}
	want := []string{"com.docker.compose.project=custom-p", "com.docker.compose.project=ghr-aaaaaa"}
	if !reflect.DeepEqual(h.docker.netLabels, want) || !reflect.DeepEqual(h.docker.volLabels, want) {
		t.Fatalf("networks %v volumes %v", h.docker.netLabels, h.docker.volLabels)
	}
}

func TestCleanupSkipsPrefixesForOtherRepos(t *testing.T) {
	h := newHarness(t)
	h.docker.named = []system.NamedContainer{{ID: "c3", Name: "dc-e2e-web"}}
	if removed, err := h.m.cleanupDocker(context.Background(), h.cfg, "aaaaaa", "darkmem"); removed != 0 || err != nil {
		t.Fatalf("darkmem has no prefixes, removed %d err %v", removed, err)
	}
}

// Validation rejects a blank prefix; cleanup must still never treat one as "match all".
func TestCleanupIgnoresBlankPrefix(t *testing.T) {
	h := newHarness(t)
	h.cfg.Repos[0].CleanupNamePrefixes = []string{"", "  "}
	h.docker.named = []system.NamedContainer{{ID: "c4", Name: "another-jobs-db"}}
	if _, err := h.m.cleanupDocker(context.Background(), h.cfg, "aaaaaa", "darkcloud"); err != nil {
		t.Fatal(err)
	}
	if len(h.docker.removed) != 0 {
		t.Fatalf("unrelated containers removed: %v", h.docker.removed)
	}
}

func TestCleanupReportsEveryFailure(t *testing.T) {
	for _, method := range []string{"ComposeContainers", "ContainerIDsByLabel", "RemoveContainers", "RemoveNetworksByLabel", "RemoveVolumesByLabel", "Containers"} {
		h := newHarness(t)
		h.docker.named = []system.NamedContainer{{ID: "c3", Name: "dc-e2e-web"}}
		h.docker.setErr(method, errors.New(method+" broke"))
		if _, err := h.m.cleanupDocker(context.Background(), h.cfg, "aaaaaa", "darkcloud"); err == nil || !strings.Contains(err.Error(), method+" broke") {
			t.Errorf("%s: err %v", method, err)
		}
	}
}

func TestRunnerContainersListsOnlyInstanceProjects(t *testing.T) {
	h := newHarness(t)
	h.m.mu.Lock()
	h.m.insts["aaaaaa"] = &instance{Meta: Meta{ID: "aaaaaa", Repo: "darkmem"}, State: sched.Busy}
	h.m.mu.Unlock()
	dir := h.m.instanceDir("aaaaaa")
	h.docker.compose = []system.ComposeContainer{
		{ID: "c1", Project: "custom-p", WorkingDir: filepath.Join(dir, "_work", "r", "r")},
		{ID: "c2", Project: "other-job", WorkingDir: filepath.Join(h.root, "instances", "bbbbbb", "_work")},
	}
	h.docker.byProject["ghr-aaaaaa"] = []system.ProjectContainer{{ID: "c0", Name: "ghr-aaaaaa-web-1", Image: "nginx", State: "running"}}
	h.docker.byProject["custom-p"] = []system.ProjectContainer{{ID: "c1", Name: "custom-p-db-1", Image: "postgres", State: "running"}}
	h.docker.byProject["other-job"] = []system.ProjectContainer{{ID: "c2", Name: "other-job-db-1"}}
	got, err := h.m.RunnerContainers(context.Background(), "aaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Container{
		{ID: "c1", Name: "custom-p-db-1", Image: "postgres", State: "running", Project: "custom-p"},
		{ID: "c0", Name: "ghr-aaaaaa-web-1", Image: "nginx", State: "running", Project: "ghr-aaaaaa"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	var u ErrUnknownRunner
	if _, err := h.m.RunnerContainers(context.Background(), "zzzzzz"); !errors.As(err, &u) {
		t.Fatalf("unknown runner err %v", err)
	}
}

func TestCheckDiskBelowHighWaterDoesNothing(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{61}
	h.m.checkDisk(context.Background(), h.cfg)
	if len(h.docker.prunes) != 0 || h.m.Status().DiskPct != 61 {
		t.Fatalf("prunes %v disk %d", h.docker.prunes, h.m.Status().DiskPct)
	}
}

func TestCheckDiskPrunesInOrder(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{85, 83, 70}
	h.m.checkDisk(context.Background(), h.cfg)
	if !reflect.DeepEqual(h.docker.prunes, []string{"until=72h", "keep=20GB", "images"}) {
		t.Fatalf("prunes %v", h.docker.prunes)
	}
	if h.m.Status().DiskPct != 70 || !strings.Contains(h.eventText(), "disk 85% > high-water 80%") || !strings.Contains(h.eventText(), "now 70%") {
		t.Fatalf("disk %d events %s", h.m.Status().DiskPct, h.eventText())
	}
}

func TestCheckDiskSkipsKeepPruneWhenAgeFilterSuffices(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{85, 75}
	h.m.checkDisk(context.Background(), h.cfg)
	if !reflect.DeepEqual(h.docker.prunes, []string{"until=72h", "images"}) {
		t.Fatalf("prunes %v", h.docker.prunes)
	}
}

func TestCheckDiskReportsFailuresAndKeepsLastMeasurement(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{85, 83, -1}
	h.docker.setErr("PruneBuildCacheTo", errors.New("unknown flag"))
	h.docker.setErr("PruneDanglingImages", errors.New("daemon busy"))
	h.m.checkDisk(context.Background(), h.cfg)
	ev := h.eventText()
	for _, want := range []string{"prune build cache to 20GB failed: unknown flag", "prune dangling images failed: daemon busy",
		"disk usage after pruning failed", "now unknown"} {
		if !strings.Contains(ev, want) {
			t.Errorf("missing %q in %s", want, ev)
		}
	}
	if h.m.Status().DiskPct != 85 {
		t.Fatalf("a failed read must keep the last good value, got %d", h.m.Status().DiskPct)
	}
}

func TestPruneHistoryAndLogs(t *testing.T) {
	h := newHarness(t)
	old := h.now.Add(-40 * 24 * time.Hour)
	h.m.History.Append(model.HistoryEntry{ID: "old", Repo: "darkmem", FinishedAt: old})
	h.m.History.Append(model.HistoryEntry{ID: "new", Repo: "darkmem", FinishedAt: h.now})
	oldLog := filepath.Join(h.m.Paths.Logs, "aaaaaa")
	newLog := filepath.Join(h.m.Paths.Logs, "bbbbbb")
	os.MkdirAll(oldLog, 0o755)
	os.MkdirAll(newLog, 0o755)
	os.Chtimes(oldLog, old, old)
	h.m.prune(h.cfg, h.now)
	got, _ := h.m.History.Query("", "", 0)
	if len(got) != 1 || got[0].ID != "new" {
		t.Fatalf("history %v", got)
	}
	if _, err := os.Stat(oldLog); !os.IsNotExist(err) {
		t.Fatal("old log dir kept")
	}
	if _, err := os.Stat(newLog); err != nil {
		t.Fatal("new log dir removed")
	}
}

func appendFile(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.WriteString(s)
}

// Both logs grow between polls and the logs are archived mid-follow:
// every byte arrives exactly once.
func TestRunnerLogCursorFollowsGrowingFiles(t *testing.T) {
	h := newHarness(t)
	diag := filepath.Join(h.m.instanceDir("aaaaaa"), "_diag")
	os.MkdirAll(diag, 0o755)
	runnerLog := filepath.Join(diag, "Runner_1.log")
	workerLog := filepath.Join(diag, "Worker_2.log")
	appendFile(t, runnerLog, "r1\n")
	appendFile(t, workerLog, "w1\n")
	var got strings.Builder
	c, err := h.m.RunnerLog("aaaaaa", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Data != "r1\nw1\n" {
		t.Fatalf("first chunk %q", c.Data)
	}
	got.WriteString(c.Data)
	appendFile(t, runnerLog, "r2\n")
	appendFile(t, workerLog, "w2\n")
	c, _ = h.m.RunnerLog("aaaaaa", c.Next)
	got.WriteString(c.Data)
	appendFile(t, workerLog, "w3\n")
	os.MkdirAll(h.m.Paths.Logs, 0o755)
	if err := os.Rename(diag, filepath.Join(h.m.Paths.Logs, "aaaaaa")); err != nil {
		t.Fatal(err)
	}
	c, err = h.m.RunnerLog("aaaaaa", c.Next)
	if err != nil {
		t.Fatal(err)
	}
	got.WriteString(c.Data)
	if got.String() != "r1\nw1\nr2\nw2\nw3\n" {
		t.Fatalf("stream %q", got.String())
	}
	if c, _ = h.m.RunnerLog("aaaaaa", c.Next); c.Data != "" {
		t.Fatalf("nothing new, got %q", c.Data)
	}
	if _, err := h.m.RunnerLog("../etc", ""); err == nil {
		t.Fatal("path traversal accepted")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./internal/runner/`
Expected: FAIL — FAIL: `h.m.cleanupDocker undefined`

- [ ] **Step 3: Write `internal/runner/cleanup.go`**

```go
package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

// maxLogChunk bounds one RunnerLog response; the cursor resumes where it stopped.
const maxLogChunk = 256 * 1024

func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// projects returns the compose projects of an instance: ghr-<id> plus every
// project whose working_dir label lies inside the instance dir, sorted.
func (m *Manager) projects(ctx context.Context, id string) ([]string, error) {
	dir := m.instanceDir(id)
	set := map[string]bool{"ghr-" + id: true}
	cc, err := m.Docker.ComposeContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list compose containers: %w", err)
	}
	for _, c := range cc {
		if c.Project != "" && within(filepath.FromSlash(c.WorkingDir), dir) {
			set[c.Project] = true
		}
	}
	names := make([]string, 0, len(set))
	for p := range set {
		names = append(names, p)
	}
	sort.Strings(names)
	return names, nil
}

// cleanupDocker removes the compose projects started from an instance's work dir
// and the repo's prefix-matched containers. It returns how many containers it
// removed and every failure joined; the caller retries the whole cleanup on error.
func (m *Manager) cleanupDocker(ctx context.Context, cfg *config.Config, id, repo string) (int, error) {
	names, err := m.projects(ctx, id)
	if err != nil {
		return 0, err
	}
	var errs []error
	removed := 0
	for _, p := range names {
		label := "com.docker.compose.project=" + p
		ids, err := m.Docker.ContainerIDsByLabel(ctx, label)
		if err != nil {
			errs = append(errs, fmt.Errorf("list containers of %s: %w", p, err))
		} else if err := m.Docker.RemoveContainers(ctx, ids); err != nil {
			errs = append(errs, fmt.Errorf("remove containers of %s: %w", p, err))
		} else {
			removed += len(ids)
		}
		if err := m.Docker.RemoveNetworksByLabel(ctx, label); err != nil {
			errs = append(errs, fmt.Errorf("remove networks of %s: %w", p, err))
		}
		if err := m.Docker.RemoveVolumesByLabel(ctx, label); err != nil {
			errs = append(errs, fmt.Errorf("remove volumes of %s: %w", p, err))
		}
	}
	r := cfg.Repo(repo)
	if r != nil && len(r.CleanupNamePrefixes) > 0 {
		all, err := m.Docker.Containers(ctx)
		if err != nil {
			return removed, errors.Join(append(errs, fmt.Errorf("list containers: %w", err))...)
		}
		var ids []string
		for _, c := range all {
			for _, p := range r.CleanupNamePrefixes {
				// Config validation rejects blank prefixes; an empty one would match every container.
				if strings.TrimSpace(p) != "" && strings.HasPrefix(c.Name, p) {
					ids = append(ids, c.ID)
					break
				}
			}
		}
		if err := m.Docker.RemoveContainers(ctx, ids); err != nil {
			errs = append(errs, fmt.Errorf("remove prefixed containers: %w", err))
		} else {
			removed += len(ids)
		}
	}
	return removed, errors.Join(errs...)
}

// RunnerContainers lists the containers of a live instance's compose projects.
func (m *Manager) RunnerContainers(ctx context.Context, id string) ([]model.Container, error) {
	m.mu.Lock()
	_, ok := m.insts[id]
	m.mu.Unlock()
	if !ok {
		return nil, ErrUnknownRunner(id)
	}
	names, err := m.projects(ctx, id)
	if err != nil {
		return nil, err
	}
	out := []model.Container{}
	for _, p := range names {
		cs, err := m.Docker.ProjectContainers(ctx, p)
		if err != nil {
			return nil, err
		}
		for _, c := range cs {
			out = append(out, model.Container{ID: c.ID, Name: c.Name, Image: c.Image, State: c.State, Project: p})
		}
	}
	return out, nil
}

// checkDisk prunes Docker build cache and dangling images above disk_high_water.
// A failed usage read keeps the last good measurement; every failure is reported.
func (m *Manager) checkDisk(ctx context.Context, cfg *config.Config) {
	pct, err := m.Docker.DataRootUsage(ctx)
	if err != nil {
		m.Events.Add("warn", "", "disk usage: %v", err)
		return
	}
	m.setDisk(pct)
	if pct <= cfg.DiskHighWater {
		return
	}
	var notes []string
	freedOld, err := m.Docker.PruneBuildCacheOlderThan(ctx, 72)
	if err != nil {
		notes = append(notes, fmt.Sprintf("prune build cache older than 72h failed: %v", err))
		freedOld = "0B"
	}
	freedKeep := "0B"
	if mid, err := m.Docker.DataRootUsage(ctx); err != nil {
		notes = append(notes, fmt.Sprintf("disk usage after the 72h prune failed: %v", err))
	} else if mid > cfg.DiskHighWater {
		if freedKeep, err = m.Docker.PruneBuildCacheTo(ctx, cfg.BuildCacheKeep); err != nil {
			notes = append(notes, fmt.Sprintf("prune build cache to %s failed: %v", cfg.BuildCacheKeep, err))
			freedKeep = "0B"
		}
	}
	freedImages, err := m.Docker.PruneDanglingImages(ctx)
	if err != nil {
		notes = append(notes, fmt.Sprintf("prune dangling images failed: %v", err))
		freedImages = "0B"
	}
	now := "unknown"
	if after, err := m.Docker.DataRootUsage(ctx); err != nil {
		notes = append(notes, fmt.Sprintf("disk usage after pruning failed: %v", err))
	} else {
		m.setDisk(after)
		now = strconv.Itoa(after) + "%"
	}
	msg := fmt.Sprintf("disk %d%% > high-water %d%% — pruned build cache (%s older than 72h, %s to %s) and dangling images (%s); now %s",
		pct, cfg.DiskHighWater, freedOld, freedKeep, cfg.BuildCacheKeep, freedImages, now)
	if len(notes) > 0 {
		msg += "; " + strings.Join(notes, "; ")
	}
	m.Events.Add("warn", "", "%s", msg)
}

func (m *Manager) setDisk(pct int) {
	m.mu.Lock()
	m.diskPct = pct
	m.mu.Unlock()
}

// prune drops history lines and archived logs older than history_retention.
func (m *Manager) prune(cfg *config.Config, now time.Time) {
	cutoff := now.Add(-cfg.HistoryRetention.D())
	if err := m.History.Prune(cutoff); err != nil {
		m.Events.Add("warn", "", "history prune: %v", err)
	}
	entries, _ := os.ReadDir(m.Paths.Logs)
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			os.RemoveAll(filepath.Join(m.Paths.Logs, e.Name()))
		}
	}
	m.lastPrune = now
}

// parseCursor reads "file=offset&file=offset" (URL query encoding).
func parseCursor(cursor string) map[string]int64 {
	out := map[string]int64{}
	q, err := url.ParseQuery(cursor)
	if err != nil {
		return out
	}
	for name, vs := range q {
		if n, err := strconv.ParseInt(vs[0], 10, 64); err == nil && n >= 0 {
			out[name] = n
		}
	}
	return out
}

// RunnerLog returns the runner's diagnostic logs (Runner_* then Worker_*) after
// cursor, read from the live instance or its archive. The Runner and Worker logs
// grow concurrently, so the cursor tracks one offset per file rather than one
// offset into their concatenation; archiving keeps file names, so it stays valid.
func (m *Manager) RunnerLog(id, cursor string) (model.LogChunk, error) {
	if !idRe.MatchString(id) {
		return model.LogChunk{}, ErrUnknownRunner(id)
	}
	dir := filepath.Join(m.instanceDir(id), "_diag")
	if _, err := os.Stat(dir); err != nil {
		dir = filepath.Join(m.Paths.Logs, id)
		if _, err := os.Stat(dir); err != nil {
			return model.LogChunk{}, ErrUnknownRunner(id)
		}
	}
	var files []string
	for _, pat := range []string{"Runner_*.log", "Worker_*.log"} {
		matches, _ := filepath.Glob(filepath.Join(dir, pat))
		sort.Strings(matches)
		files = append(files, matches...)
	}
	offsets := parseCursor(cursor)
	next := url.Values{}
	var data []byte
	for _, f := range files {
		name := filepath.Base(f)
		off := offsets[name]
		if budget := maxLogChunk - len(data); budget > 0 {
			if b, err := readFrom(f, off, budget); err == nil {
				data = append(data, b...)
				off += int64(len(b))
			}
		}
		next.Set(name, strconv.FormatInt(off, 10))
	}
	return model.LogChunk{Data: string(data), Next: next.Encode()}, nil
}

func readFrom(path string, off int64, limit int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(f, int64(limit)))
}
```

- [ ] **Step 4: Run the check**

Run: `go test -count=1 -timeout 180s ./internal/runner/`
Expected: PASS (exit 0).

- [ ] **Step 5: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/runner/cleanup_test.go internal/runner/cleanup.go
git commit -m "feat(runner): clean up containers and prune disk"
```

---

### Task 15: Runner lifecycle: spawn, state, finish, idle stop

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `internal/runner/lifecycle.go`
- Test: `internal/runner/lifecycle_test.go`

**Interfaces:**
- Consumes: the Task 13–14 internals (Contracts → runner); `system.UnitSpec`, `github.IsKind`, `sched` (Contracts).
- Produces: `RandomID` and the Task 15 internals (Contracts → runner); pending history records (Contracts → Instance files).

**Items:** 1

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing test `internal/runner/lifecycle_test.go`**

```go
package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
)

func TestSpawnWritesMetaAndStartsUnit(t *testing.T) {
	h := newHarness(t)
	if err := h.m.spawn(context.Background(), h.cfg, "darkcloud"); err != nil {
		t.Fatal(err)
	}
	if h.gh.jitCalls[0] != "ghr-darkcloud-aaaaaa homelab" {
		t.Fatalf("jit call %q", h.gh.jitCalls[0])
	}
	var meta Meta
	if err := readJSON(filepath.Join(h.m.instanceDir("aaaaaa"), MetaFile), &meta); err != nil {
		t.Fatal(err)
	}
	if meta.RunnerID != 101 || meta.DistVersion != "2.330.0" || meta.RunnerName != "ghr-darkcloud-aaaaaa" ||
		!reflect.DeepEqual(meta.Labels, []string{"homelab"}) || !meta.SpawnedAt.Equal(h.now) {
		t.Fatalf("meta %+v", meta)
	}
	u := h.sd.started[0]
	if u.Unit != "ghr-runner-aaaaaa" || u.User != "ghrunner" || u.Command[1] != "--jitconfig" || u.Command[2] != "ENC-ghr-darkcloud-aaaaaa" {
		t.Fatalf("unit %+v", u)
	}
	if u.Env["COMPOSE_PROJECT_NAME"] != "ghr-aaaaaa" || u.Env["GHR_INSTANCE_DIR"] != h.m.instanceDir("aaaaaa") ||
		!strings.HasSuffix(u.Env["ACTIONS_RUNNER_HOOK_JOB_STARTED"], "job-started.sh") || u.Env["DOTNET_INSTALL_DIR"] == "" {
		t.Fatalf("env %+v", u.Env)
	}
	if strings.Join(u.Props, " ") != "MemoryMax=6G CPUQuota=200% KillMode=control-group" {
		t.Fatalf("props %v", u.Props)
	}
	if len(h.host.chowned) != 1 || !strings.HasSuffix(h.host.chowned[0], ":ghrunner") {
		t.Fatalf("chown %v", h.host.chowned)
	}
	if h.state("aaaaaa") != "starting" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
}

func TestSpawnFailureCleansUp(t *testing.T) {
	h := newHarness(t)
	h.m.Paths.Dist = filepath.Join(h.root, "missing")
	if err := h.m.spawn(context.Background(), h.cfg, "darkcloud"); err == nil {
		t.Fatal("expected error")
	}
	if len(h.gh.deleted) != 1 || h.gh.deleted[0] != 101 {
		t.Fatalf("registration not deleted: %v", h.gh.deleted)
	}
	if _, err := os.Stat(h.m.instanceDir("aaaaaa")); !os.IsNotExist(err) {
		t.Fatal("instance dir left behind")
	}
}

func TestUniqueIDSkipsArchivedLogsAndPendingHistory(t *testing.T) {
	h := newHarness(t)
	os.MkdirAll(filepath.Join(h.m.Paths.Logs, "aaaaaa"), 0o755)
	os.MkdirAll(h.m.Paths.Pending, 0o755)
	os.WriteFile(filepath.Join(h.m.Paths.Pending, "bbbbbb.json"), []byte("{}"), 0o644)
	if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
		t.Fatal(err)
	}
	if h.state("cccccc") != "starting" {
		t.Fatalf("expected cccccc, events:\n%s", h.eventText())
	}
}

func TestStartingToIdleToBusy(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.m.refreshRunners(context.Background())
	if h.state("aaaaaa") != "starting" {
		t.Fatal("offline runner should stay starting")
	}
	h.gh.setRunner(101, "online", false)
	h.m.refreshRunners(context.Background())
	if h.state("aaaaaa") != "idle" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
	h.writeJob(t, "aaaaaa", 55)
	h.m.readJobFiles()
	if h.state("aaaaaa") != "busy" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
	st := h.m.Status()
	if st.Instances[0].Job == nil || st.Instances[0].Job.RunID != 55 || st.Instances[0].Job.RunNumber != "412" {
		t.Fatalf("job %+v", st.Instances[0].Job)
	}
}

// No hook record and no jobs API entry yet: the runners API alone moves idle to busy.
func TestIdleRunnerReportedBusyBecomesBusy(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.gh.setRunner(101, "online", false)
	h.m.refreshRunners(context.Background())
	h.gh.setRunner(101, "online", true)
	h.m.refreshRunners(context.Background())
	if h.state("aaaaaa") != "busy" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
}

func completedJob(h *harness, runID int64, runner, conclusion string) github.Job {
	started := h.now.Add(-2 * time.Minute)
	done := h.now
	return github.Job{ID: 9, RunID: runID, Name: "CI / e2e", Status: "completed", Conclusion: conclusion,
		RunnerName: runner, StartedAt: &started, CompletedAt: &done, HTMLURL: "https://x/9"}
}

func TestExitRecordsHistoryArchivesLogsAndCleansUp(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	os.WriteFile(filepath.Join(h.m.instanceDir("aaaaaa"), "_diag", "Worker_1.log"), []byte("log"), 0o644)
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
	h.docker.byLabel["com.docker.compose.project=ghr-aaaaaa"] = []string{"c1"}
	h.sd.active["ghr-runner-aaaaaa"] = false

	h.m.refreshUnits(context.Background())
	h.m.Wait()

	if h.state("aaaaaa") != "gone" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
	got, _ := h.m.History.Query("", "", 0)
	if len(got) != 1 || got[0].Conclusion != "success" || got[0].JobName != "CI / e2e" || got[0].HTMLURL != "https://x/9" {
		t.Fatalf("history %+v", got)
	}
	archive := filepath.Join(h.m.Paths.Logs, "aaaaaa")
	if _, err := os.Stat(filepath.Join(archive, "Worker_1.log")); err != nil {
		t.Fatal("log not archived")
	}
	if h.host.chowned[len(h.host.chowned)-1] != archive+":root" {
		t.Fatalf("archive not chowned to root: %v", h.host.chowned)
	}
	if _, err := os.Stat(h.m.instanceDir("aaaaaa")); !os.IsNotExist(err) {
		t.Fatal("instance dir kept")
	}
	if _, err := os.Stat(h.m.pendingPath("aaaaaa")); !os.IsNotExist(err) {
		t.Fatal("pending record kept")
	}
	if len(h.gh.deleted) != 1 || h.gh.deleted[0] != 101 {
		t.Fatalf("deleted %v", h.gh.deleted)
	}
	if !strings.Contains(h.eventText(), "ok darkcloud #412 CI / e2e success 2m0s  cleanup: 1 ctrs") {
		t.Fatalf("events %s", h.eventText())
	}
	if h.m.Status().Repos[0].LastJob == nil {
		t.Fatal("last job not recorded")
	}
}

func TestExitWithoutJobRecordsNoHistory(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if got := h.history(t); len(got) != 0 || !strings.Contains(h.eventText(), "exited without a job") {
		t.Fatalf("history %v events %s", got, h.eventText())
	}
}

// A killed job's conclusion arrives after the runner exits: history waits for it.
func TestDelayedConclusionIsRetried(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.writeJob(t, "aaaaaa", 55)
	running := completedJob(h, 55, "ghr-darkmem-aaaaaa", "")
	running.Status, running.CompletedAt = "in_progress", nil
	h.gh.setJobs(55, running)
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "gone" || len(h.history(t)) != 0 {
		t.Fatalf("state %s history %v: must wait for the conclusion", h.state("aaaaaa"), h.history(t))
	}
	h.m.finalizePending(context.Background())
	if len(h.history(t)) != 0 {
		t.Fatal("still in progress: no history yet")
	}
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkmem-aaaaaa", "cancelled"))
	h.m.finalizePending(context.Background())
	if got := h.history(t); !reflect.DeepEqual(got, []string{"aaaaaa cancelled"}) {
		t.Fatalf("history %v", got)
	}
}

func TestConclusionFallsBackToUnknownAfterDeadline(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.writeJob(t, "aaaaaa", 55)
	h.gh.setErr("ListJobs darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	h.m.finalizePending(context.Background())
	if len(h.history(t)) != 0 {
		t.Fatal("API failure before the deadline must retry, not record")
	}
	h.now = h.now.Add(conclusionWait)
	h.m.finalizePending(context.Background())
	if got := h.history(t); !reflect.DeepEqual(got, []string{"aaaaaa unknown"}) {
		t.Fatalf("history %v", got)
	}
}

// The hook record is missing, but the jobs API confirmed the job: history still records it.
func TestFinishFallsBackToConfirmedJob(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	job := completedJob(h, 77, "ghr-darkmem-aaaaaa", "")
	job.Status, job.CompletedAt = "in_progress", nil
	h.m.mu.Lock() // what Tick's confirm records from the jobs API
	h.m.insts["aaaaaa"].Job = &model.JobInfo{RunID: job.RunID, Name: job.Name, StartedAt: *job.StartedAt}
	h.m.mu.Unlock()
	os.WriteFile(filepath.Join(h.m.instanceDir("aaaaaa"), JobFile), []byte("{not json"), 0o644)
	h.gh.setJobs(77, completedJob(h, 77, "ghr-darkmem-aaaaaa", "failure"))
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	got, _ := h.m.History.Query("", "", 0)
	if len(got) != 1 || got[0].RunID != 77 || got[0].Conclusion != "failure" {
		t.Fatalf("history %+v", got)
	}
}

// Without a CompletedAt from the API, the hook's finished_at is the finish time.
func TestFinishTimeFromCompletionHook(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.writeJob(t, "aaaaaa", 55, `"finished_at":"2026-10-03T12:05:00Z"`)
	h.gh.setJobs(55, github.Job{RunID: 55, Name: "e2e", Status: "completed", Conclusion: "success", RunnerName: "ghr-darkmem-aaaaaa"})
	h.now = h.now.Add(time.Hour) // cleanup runs much later
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	got, _ := h.m.History.Query("", "", 0)
	if len(got) != 1 || !got[0].FinishedAt.Equal(time.Date(2026, 10, 3, 12, 5, 0, 0, time.UTC)) {
		t.Fatalf("history %+v", got)
	}
}

// A Docker failure keeps the instance cleaning (holding the repo slot) and is retried.
func TestDockerFailureKeepsSlotAndRetries(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
	h.docker.setErr("RemoveVolumesByLabel", errors.New("volume in use"))
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "cleaning" || !strings.Contains(h.eventText(), "volume in use") {
		t.Fatalf("state %s events %s", h.state("aaaaaa"), h.eventText())
	}
	if _, err := os.Stat(h.m.instanceDir("aaaaaa")); err != nil {
		t.Fatal("instance dir removed before cleanup succeeded")
	}
	if spawns := sched.Plan(h.cfg, h.m.schedInstances(h.cfg), sched.Demand{"darkcloud": {{Repo: "darkcloud", ID: 1, CreatedAt: h.now}}}, h.now); len(spawns) != 0 {
		t.Fatalf("repo cap 1 must stay held while cleanup fails, got %v", spawns)
	}
	h.docker.setErr("RemoveVolumesByLabel", nil)
	h.m.refreshUnits(context.Background()) // before the retry delay: nothing
	h.m.Wait()
	if h.state("aaaaaa") != "cleaning" {
		t.Fatal("retried before finishRetry")
	}
	h.now = h.now.Add(finishRetry)
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "gone" || !reflect.DeepEqual(h.history(t), []string{"aaaaaa success"}) {
		t.Fatalf("state %s history %v", h.state("aaaaaa"), h.history(t))
	}
}

func TestArchiveFailureKeepsInstanceDir(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	os.WriteFile(h.m.Paths.Logs, []byte("a file where the log dir belongs"), 0o644)
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "cleaning" || !strings.Contains(h.eventText(), "archive logs") {
		t.Fatalf("state %s events %s", h.state("aaaaaa"), h.eventText())
	}
	if _, err := os.Stat(filepath.Join(h.m.instanceDir("aaaaaa"), "_diag")); err != nil {
		t.Fatal("logs deleted although archiving failed")
	}
}

func TestStopIdleRechecksBusy(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.gh.setRunner(101, "online", false)
	h.gh.setRunner(102, "online", false)
	h.m.refreshRunners(context.Background())
	h.now = h.now.Add(6 * time.Minute)
	h.gh.setRunner(102, "online", true) // darkcloud picked a job at the last moment
	h.m.stopIdle(context.Background(), h.cfg, h.now)
	if len(h.sd.stopped) != 1 || h.sd.stopped[0] != "ghr-runner-aaaaaa" {
		t.Fatalf("stopped %v", h.sd.stopped)
	}
	if h.state("bbbbbb") != "busy" {
		t.Fatalf("bbbbbb state %s", h.state("bbbbbb"))
	}
}

// Only an affirmative busy=false stops a runner; a failed check leaves it running.
func TestStopIdleLeavesRunnerOnCheckFailure(t *testing.T) {
	for _, err := range []error{
		&github.APIError{Status: 502, Kind: github.ErrServer},
		&github.APIError{Status: 404, Kind: github.ErrNotFound},
		errors.New("connection reset"),
	} {
		h := newHarness(t)
		h.m.spawn(context.Background(), h.cfg, "darkmem")
		h.gh.setRunner(101, "online", false)
		h.m.refreshRunners(context.Background())
		h.now = h.now.Add(6 * time.Minute)
		h.gh.setErr("GetRunner darkmem", err)
		h.m.stopIdle(context.Background(), h.cfg, h.now)
		if len(h.sd.stopped) != 0 || h.state("aaaaaa") != "idle" {
			t.Fatalf("%v: stopped %v state %s", err, h.sd.stopped, h.state("aaaaaa"))
		}
	}
}

// A failed or cancelled systemd query is not an exit: the busy runner's dir stays.
func TestUnitQueryErrorIsNotAnExit(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.writeJob(t, "aaaaaa", 55)
	h.m.readJobFiles()
	h.sd.activeErr["ghr-runner-aaaaaa"] = errors.New("Failed to connect to bus")
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "busy" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
	if _, err := os.Stat(h.m.instanceDir("aaaaaa")); err != nil {
		t.Fatal("instance dir removed")
	}
}

func TestStartTimeoutStopsUnit(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.now = h.now.Add(3 * time.Minute)
	h.m.stopStartTimedOut(context.Background(), h.cfg, h.now)
	if len(h.sd.stopped) != 1 || !strings.Contains(h.eventText(), "did not come online within 2m0s") {
		t.Fatalf("stopped %v events %s", h.sd.stopped, h.eventText())
	}
}

func TestKill(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	if err := h.m.Kill(context.Background(), "zzzzzz"); err == nil {
		t.Fatal("unknown runner accepted")
	}
	if h.sd.stopped[0] != "ghr-runner-aaaaaa" {
		t.Fatalf("stopped %v", h.sd.stopped)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./internal/runner/`
Expected: FAIL — FAIL: `h.m.spawn undefined`

- [ ] **Step 3: Write `internal/runner/lifecycle.go`**

```go
package runner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
	"github.com/darkraise/ghr/internal/system"
)

const (
	finishTimeout  = 15 * time.Minute // bounds one finish attempt, so Wait returns on shutdown
	finishRetry    = 30 * time.Second // delay before a failed finish runs again
	conclusionWait = 30 * time.Minute // how long a finished job's conclusion is polled before "unknown"
)

// RandomID returns 6 lowercase hex characters.
func RandomID() string {
	b := make([]byte, 3)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return !os.IsNotExist(err)
}

// uniqueID returns an ID with no live instance, instance dir, log archive or
// pending history record, so retained logs and history never collide. An
// unreadable path counts as taken, so the attempts are bounded.
func (m *Manager) uniqueID() (string, error) {
	for range 1000 {
		id := m.NewID()
		m.mu.Lock()
		_, taken := m.insts[id]
		m.mu.Unlock()
		if !taken && !exists(m.instanceDir(id)) && !exists(filepath.Join(m.Paths.Logs, id)) &&
			!exists(m.pendingPath(id)) {
			return id, nil
		}
	}
	return "", errors.New("no free runner ID: check that the instance, log and pending dirs are readable")
}

func (m *Manager) pendingPath(id string) string { return filepath.Join(m.Paths.Pending, id+".json") }

// spawn registers a JIT runner for repo and starts it in a transient unit.
func (m *Manager) spawn(ctx context.Context, cfg *config.Config, repo string) error {
	r := cfg.Repo(repo)
	if r == nil {
		return fmt.Errorf("repo %s not configured", repo)
	}
	id, err := m.uniqueID()
	if err != nil {
		return err
	}
	name := "ghr-" + r.Name + "-" + id
	labels := cfg.CustomLabels(*r)
	jit, err := m.GH.GenerateJITConfig(ctx, r.Name, name, labels)
	if err != nil {
		return err
	}
	dir := m.instanceDir(id)
	fail := func(err error) error {
		os.RemoveAll(dir)
		if derr := m.GH.DeleteRunner(ctx, r.Name, jit.Runner.ID); derr != nil {
			m.Events.Add("warn", r.Name, "spawn %s: delete registration: %v", id, derr)
		}
		return err
	}
	dist, err := filepath.EvalSymlinks(m.Paths.Dist)
	if err != nil {
		return fail(err)
	}
	if err := m.Host.CopyTree(ctx, dist, dir); err != nil {
		return fail(err)
	}
	meta := Meta{ID: id, Repo: r.Name, RunnerID: jit.Runner.ID, RunnerName: name, Labels: labels,
		DistVersion: filepath.Base(dist), SpawnedAt: m.Now()}
	if err := writeJSON(filepath.Join(dir, MetaFile), meta); err != nil {
		return fail(err)
	}
	if err := m.Host.ChownR(ctx, dir, RunnerUser); err != nil {
		return fail(err)
	}
	spec := system.UnitSpec{
		Unit:    UnitPrefix + id,
		User:    RunnerUser,
		WorkDir: dir,
		Props: []string{
			"MemoryMax=" + cfg.RunnerLimits.MemoryMax,
			"CPUQuota=" + cfg.RunnerLimits.CPUQuota,
			"KillMode=control-group",
		},
		Env: map[string]string{
			"HOME":                              m.Paths.Home,
			"GHR_INSTANCE_DIR":                  dir,
			"RUNNER_TOOL_CACHE":                 m.Paths.ToolCache,
			"DOTNET_INSTALL_DIR":                filepath.Join(m.Paths.ToolCache, "dotnet"),
			"COMPOSE_PROJECT_NAME":              "ghr-" + id,
			"ACTIONS_RUNNER_HOOK_JOB_STARTED":   filepath.Join(m.Paths.Hooks, "job-started.sh"),
			"ACTIONS_RUNNER_HOOK_JOB_COMPLETED": filepath.Join(m.Paths.Hooks, "job-completed.sh"),
		},
		Command: []string{filepath.Join(dir, "run.sh"), "--jitconfig", jit.EncodedJITConfig},
	}
	if err := m.SD.Start(ctx, spec); err != nil {
		return fail(err)
	}
	m.mu.Lock()
	m.insts[id] = &instance{Meta: meta, State: sched.Starting, StateSince: m.Now()}
	m.mu.Unlock()
	m.Events.Add("info", r.Name, "spawned %s (%s)", id, meta.DistVersion)
	return nil
}

func parseTime(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

// readJobFiles marks instances busy once their job-started hook has written job.json.
func (m *Manager) readJobFiles() {
	for _, i := range m.snapshot() {
		if i.State != sched.Starting && i.State != sched.Idle && i.State != sched.Busy {
			continue
		}
		var rec JobRecord
		if err := readJSON(filepath.Join(m.instanceDir(i.ID), JobFile), &rec); err != nil {
			continue
		}
		m.mu.Lock()
		inst, ok := m.insts[i.ID]
		if ok && inst.State != sched.Cleaning {
			if inst.State == sched.Starting || inst.State == sched.Idle {
				inst.State = sched.Busy
				inst.StateSince = m.Now()
			}
			if inst.Job == nil {
				runID, _ := strconv.ParseInt(rec.RunID, 10, 64)
				started, ok := parseTime(rec.StartedAt)
				if !ok {
					started = m.Now()
				}
				inst.Job = &model.JobInfo{RunID: runID, RunNumber: rec.RunNumber, Workflow: rec.Workflow, Name: rec.Job, StartedAt: started}
			}
		}
		m.mu.Unlock()
	}
}

// refreshRunners moves starting and idle instances to idle or busy from the
// runners API, so an idle runner GitHub reports busy is busy even before its
// hook record or the jobs API shows the job.
func (m *Manager) refreshRunners(ctx context.Context) {
	now := m.Now()
	for _, i := range m.snapshot() {
		if i.State != sched.Starting && i.State != sched.Idle {
			continue
		}
		r, err := m.GH.GetRunner(ctx, i.Repo, i.RunnerID)
		if err != nil {
			if github.IsKind(err, github.ErrAuth) || github.IsKind(err, github.ErrRateLimit) {
				m.apiErr(i.Repo, err, now)
				return
			}
			continue // start_timeout handles runners that never appear
		}
		if r.Busy {
			m.setState(i.ID, sched.Busy)
		} else if r.Status == "online" && i.State == sched.Starting {
			m.setState(i.ID, sched.Idle)
		}
	}
}

// refreshUnits starts cleanup for every instance whose unit has confirmed
// exited, and retries failed cleanups. An unknown unit state is never an exit.
func (m *Manager) refreshUnits(ctx context.Context) {
	now := m.Now()
	for _, i := range m.snapshot() {
		if i.State == sched.Cleaning {
			if !i.finishing && !now.Before(i.retryFinish) {
				m.beginFinish(i.ID)
			}
			continue
		}
		active, err := m.SD.Active(ctx, UnitPrefix+i.ID)
		if err == nil && !active {
			m.beginFinish(i.ID)
		}
	}
}

// beginFinish moves an instance to cleaning and runs finish in the background.
// A failed finish leaves it cleaning (holding its repo slot) for refreshUnits to retry.
func (m *Manager) beginFinish(id string) {
	m.mu.Lock()
	i, ok := m.insts[id]
	if !ok || i.finishing {
		m.mu.Unlock()
		return
	}
	if i.State != sched.Cleaning {
		i.State = sched.Cleaning
		i.StateSince = m.Now()
	}
	i.finishing = true
	repo := i.Repo
	m.mu.Unlock()
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), finishTimeout)
		defer cancel()
		err := m.finish(ctx, id)
		m.mu.Lock()
		if i, ok := m.insts[id]; ok {
			i.finishing = false
			if err != nil {
				i.retryFinish = m.Now().Add(finishRetry)
			}
		}
		m.mu.Unlock()
		if err != nil {
			m.Events.Add("warn", repo, "cleanup of %s failed, retrying in %s: %v", id, finishRetry, err)
		}
	}()
}

// pending is a finished job's history record waiting for its final conclusion.
// It is written before any cleanup step, so a crash at any point loses nothing.
type pending struct {
	Entry      model.HistoryEntry `json:"entry"`
	RunnerName string             `json:"runner_name"`
	Cleanup    int                `json:"cleanup"`
	Deadline   time.Time          `json:"deadline"`
}

// pendingFor builds the record from job.json, falling back to the job the
// manager confirmed through the jobs API. ok is false when no job ran.
func (m *Manager) pendingFor(i instance, rec JobRecord, recOK bool) (pending, bool) {
	if !recOK && i.Job == nil {
		return pending{}, false
	}
	now := m.Now()
	e := model.HistoryEntry{ID: i.ID, Repo: i.Repo, Conclusion: "unknown", StartedAt: i.StateSince, FinishedAt: now}
	if i.Job != nil {
		e.RunID, e.RunNumber, e.Workflow, e.JobName, e.HTMLURL = i.Job.RunID, i.Job.RunNumber, i.Job.Workflow, i.Job.Name, i.Job.HTMLURL
		e.StartedAt = i.Job.StartedAt
	}
	if recOK {
		if id, err := strconv.ParseInt(rec.RunID, 10, 64); err == nil && id != 0 {
			e.RunID = id
		}
		if rec.RunNumber != "" {
			e.RunNumber = rec.RunNumber
		}
		if rec.Workflow != "" {
			e.Workflow = rec.Workflow
		}
		if e.JobName == "" {
			e.JobName = rec.Job
		}
		if t, ok := parseTime(rec.StartedAt); ok {
			e.StartedAt = t
		}
		if t, ok := parseTime(rec.FinishedAt); ok {
			e.FinishedAt = t
		}
	}
	return pending{Entry: e, RunnerName: i.RunnerName, Deadline: now.Add(conclusionWait)}, true
}

// finish runs the exit steps in an order that is safe to repeat after a crash
// or failure: pending history record, log archive, Docker cleanup, instance
// dir, registration. History itself is appended by finalize.
func (m *Manager) finish(ctx context.Context, id string) error {
	cfg := m.Config()
	m.mu.Lock()
	ip, ok := m.insts[id]
	if !ok {
		m.mu.Unlock()
		return nil
	}
	i := *ip
	m.mu.Unlock()
	dir := m.instanceDir(id)

	var rec JobRecord
	recOK := readJSON(filepath.Join(dir, JobFile), &rec) == nil
	p, hadJob := m.pendingFor(i, rec, recOK)
	pendingPath := m.pendingPath(id)
	if hadJob {
		if err := readJSON(pendingPath, &p); err != nil {
			if err := os.MkdirAll(m.Paths.Pending, 0o755); err != nil {
				return err
			}
			if err := writeJSONAtomic(pendingPath, p); err != nil {
				return fmt.Errorf("write pending history: %w", err)
			}
		}
	}

	archive := filepath.Join(m.Paths.Logs, id)
	if diag := filepath.Join(dir, "_diag"); exists(diag) {
		if err := os.MkdirAll(m.Paths.Logs, 0o755); err != nil {
			return fmt.Errorf("archive logs: %w", err)
		}
		if err := os.Rename(diag, archive); err != nil {
			return fmt.Errorf("archive logs: %w", err)
		}
	}
	if exists(archive) {
		if err := m.Host.ChownR(ctx, archive, "root"); err != nil {
			return fmt.Errorf("chown archived logs: %w", err)
		}
	}

	removed, err := m.cleanupDocker(ctx, cfg, id, i.Repo)
	if err != nil {
		return fmt.Errorf("docker cleanup: %w", err)
	}
	if hadJob {
		p.Cleanup = removed
		if err := writeJSONAtomic(pendingPath, p); err != nil {
			return fmt.Errorf("write pending history: %w", err)
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	if err := m.GH.DeleteRunner(ctx, i.Repo, i.RunnerID); err != nil && !github.IsKind(err, github.ErrUnprocessable) {
		m.Events.Add("warn", i.Repo, "delete registration %s: %v (reconciliation retries)", i.RunnerName, err)
	}

	m.mu.Lock()
	delete(m.insts, id)
	m.mu.Unlock()
	if !hadJob {
		m.Events.Add("info", i.Repo, "runner %s exited without a job; cleanup: %d ctrs", id, removed)
		return nil
	}
	m.finalize(ctx, pendingPath)
	return nil
}

// finalize appends a pending record to history once the jobs API reports the
// job completed, or with conclusion "unknown" after its deadline, then deletes it.
func (m *Manager) finalize(ctx context.Context, path string) {
	var p pending
	if err := readJSON(path, &p); err != nil {
		m.Events.Add("warn", "", "unreadable pending history %s removed: %v", filepath.Base(path), err)
		os.Remove(path)
		return
	}
	e := p.Entry
	final := false
	if e.RunID != 0 {
		jobs, err := m.GH.ListJobs(ctx, e.Repo, e.RunID)
		if err != nil && !github.IsKind(err, github.ErrNotFound) {
			if github.IsKind(err, github.ErrAuth) || github.IsKind(err, github.ErrRateLimit) {
				m.apiErr(e.Repo, err, m.Now())
			}
			if m.Now().Before(p.Deadline) {
				return
			}
		}
		for _, j := range jobs {
			if j.RunnerName != p.RunnerName {
				continue
			}
			e.JobName, e.HTMLURL = j.Name, j.HTMLURL
			if j.StartedAt != nil {
				e.StartedAt = *j.StartedAt
			}
			if j.CompletedAt != nil {
				e.FinishedAt = *j.CompletedAt
			}
			if j.Status == "completed" && j.Conclusion != "" {
				e.Conclusion = j.Conclusion
				final = true
			}
		}
	}
	if !final && e.RunID != 0 && m.Now().Before(p.Deadline) {
		return
	}
	if err := m.History.Append(e); err != nil {
		m.Events.Add("warn", e.Repo, "history append: %v", err)
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		m.Events.Add("warn", e.Repo, "remove pending history: %v", err)
	}
	m.mu.Lock()
	m.recordLastJob(e)
	m.mu.Unlock()
	level := "warn"
	switch e.Conclusion {
	case "success":
		level = "ok"
	case "failure":
		level = "error"
	}
	m.Events.Add(level, e.Repo, "#%s %s %s %s  cleanup: %d ctrs", e.RunNumber, e.JobName, e.Conclusion,
		e.FinishedAt.Sub(e.StartedAt).Round(time.Second), p.Cleanup)
}

// finalizePending retries every pending history record whose instance is gone.
func (m *Manager) finalizePending(ctx context.Context) {
	entries, err := os.ReadDir(m.Paths.Pending)
	if err != nil {
		return
	}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !idRe.MatchString(id) {
			continue
		}
		m.mu.Lock()
		_, live := m.insts[id]
		m.mu.Unlock()
		if !live {
			m.finalize(ctx, filepath.Join(m.Paths.Pending, e.Name()))
		}
	}
}

// stopIdle stops idle runners chosen by sched.IdleToStop. Each is stopped only
// after the runners API affirms it is not busy; any error leaves it running.
func (m *Manager) stopIdle(ctx context.Context, cfg *config.Config, now time.Time) {
	byID := map[string]instance{}
	for _, i := range m.snapshot() {
		byID[i.ID] = i
	}
	for _, id := range sched.IdleToStop(cfg, m.schedInstances(cfg), now) {
		i := byID[id]
		r, err := m.GH.GetRunner(ctx, i.Repo, i.RunnerID)
		if err != nil {
			if !github.IsKind(err, github.ErrNotFound) {
				m.apiErr(i.Repo, err, now)
			}
			if !m.apiAllowed(now) {
				return
			}
			continue
		}
		if r.Busy {
			m.setState(id, sched.Busy)
			continue
		}
		if err := m.SD.Stop(ctx, UnitPrefix+id); err != nil {
			m.Events.Add("warn", i.Repo, "stop idle %s: %v", id, err)
			continue
		}
		m.Events.Add("info", i.Repo, "stopped idle runner %s", id)
	}
}

// stopStartTimedOut stops runners that never came online; finish deletes their registration.
func (m *Manager) stopStartTimedOut(ctx context.Context, cfg *config.Config, now time.Time) {
	for _, id := range sched.StartTimedOut(cfg, m.schedInstances(cfg), now) {
		m.mu.Lock()
		repo := m.insts[id].Repo
		m.mu.Unlock()
		if err := m.SD.Stop(ctx, UnitPrefix+id); err != nil {
			m.Events.Add("warn", repo, "stop %s: %v", id, err)
			continue
		}
		m.Events.Add("warn", repo, "runner %s did not come online within %s; replaced", id, cfg.StartTimeout)
	}
}
```

- [ ] **Step 4: Run the check**

Run: `go test -count=1 -timeout 180s ./internal/runner/`
Expected: PASS (exit 0).

- [ ] **Step 5: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/runner/lifecycle_test.go internal/runner/lifecycle.go
git commit -m "feat(runner): spawn JIT runners and reap them"
```

---

### Task 16: Runner adoption and reconciliation

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `internal/runner/adopt.go`
- Test: `internal/runner/adopt_test.go`

**Interfaces:**
- Consumes: the Task 13–15 internals (Contracts → runner).
- Produces: `Manager.Adopt`, `Manager.Reconcile`, `activeUnits` (Contracts → runner).

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 3 = 5

- [ ] **Step 1: Write the failing test `internal/runner/adopt_test.go`**

```go
package runner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
)

func makeInstance(t *testing.T, h *harness, id, repo string, runnerID int64, withJob bool) {
	t.Helper()
	dir := h.m.instanceDir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(filepath.Join(dir, MetaFile), Meta{ID: id, Repo: repo, RunnerID: runnerID, RunnerName: "ghr-" + repo + "-" + id,
		Labels: []string{"homelab"}, SpawnedAt: h.now}); err != nil {
		t.Fatal(err)
	}
	if withJob {
		h.writeJob(t, id, 77)
	}
}

func TestAdopt(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkcloud", 1, true) // running, mid-job
	makeInstance(t, h, "bbbbbb", "darkmem", 2, false)  // exited while daemon was down
	os.MkdirAll(h.m.instanceDir("cccccc"), 0o755)      // no ghr.json, unit active
	h.sd.active["ghr-runner-aaaaaa"] = true
	h.sd.active["ghr-runner-cccccc"] = true
	h.sd.active["ghr-runner-dddddd"] = true // unit without a dir

	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()

	if h.state("aaaaaa") != "busy" {
		t.Fatalf("aaaaaa %s", h.state("aaaaaa"))
	}
	if h.state("bbbbbb") != "gone" || !reflect.DeepEqual(h.gh.deleted, []int64{2}) {
		t.Fatalf("bbbbbb %s deleted %v", h.state("bbbbbb"), h.gh.deleted)
	}
	if _, err := os.Stat(h.m.instanceDir("cccccc")); !os.IsNotExist(err) {
		t.Fatal("cccccc dir kept")
	}
	sort.Strings(h.sd.stopped)
	if !reflect.DeepEqual(h.sd.stopped, []string{"ghr-runner-cccccc", "ghr-runner-dddddd"}) {
		t.Fatalf("stopped %v", h.sd.stopped)
	}
}

// A unit with unreadable metadata whose stop fails keeps its dir; reconciliation
// stops it later and only then deletes its registration and dir.
func TestAdoptKeepsDirWhenStopFails(t *testing.T) {
	h := newHarness(t)
	os.MkdirAll(h.m.instanceDir("cccccc"), 0o755)
	h.sd.active["ghr-runner-cccccc"] = true
	h.sd.stopErr["ghr-runner-cccccc"] = errors.New("job still shutting down")
	h.gh.listed["darkmem"] = []github.Runner{{ID: 9, Name: "ghr-darkmem-cccccc"}}
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.m.Reconcile(context.Background(), h.cfg)
	if _, err := os.Stat(h.m.instanceDir("cccccc")); err != nil {
		t.Fatal("dir removed while its unit may still run")
	}
	if len(h.gh.deleted) != 0 {
		t.Fatalf("registration deleted under a running unit: %v", h.gh.deleted)
	}
	delete(h.sd.stopErr, "ghr-runner-cccccc")
	h.m.Reconcile(context.Background(), h.cfg)
	if _, err := os.Stat(h.m.instanceDir("cccccc")); !os.IsNotExist(err) {
		t.Fatal("dir kept after the unit stopped")
	}
	if !reflect.DeepEqual(h.gh.deleted, []int64{9}) {
		t.Fatalf("deleted %v", h.gh.deleted)
	}
}

// start_timeout counts from the spawn: a restart does not give a stuck runner more time.
func TestAdoptedStartingKeepsSpawnDeadline(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkmem", 1, false)
	h.sd.active["ghr-runner-aaaaaa"] = true
	h.now = h.now.Add(3 * time.Minute) // spawned 3m ago; start_timeout is 2m
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := sched.StartTimedOut(h.cfg, h.m.schedInstances(h.cfg), h.now); !reflect.DeepEqual(got, []string{"aaaaaa"}) {
		t.Fatalf("timed out %v", got)
	}
}

func TestReconcileDeletesStaleRegistrationsAndDirs(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkcloud", 1, false)
	h.m.mu.Lock()
	h.m.insts["aaaaaa"] = &instance{Meta: Meta{ID: "aaaaaa", Repo: "darkcloud", RunnerID: 1, RunnerName: "ghr-darkcloud-aaaaaa"}, State: "idle"}
	h.m.mu.Unlock()
	h.sd.active["ghr-runner-aaaaaa"] = true
	os.MkdirAll(h.m.instanceDir("eeeeee"), 0o755) // orphan dir, no unit
	h.gh.listed["darkcloud"] = []github.Runner{
		{ID: 1, Name: "ghr-darkcloud-aaaaaa"},   // ours, live
		{ID: 5, Name: "ghr-darkcloud-bbbbbb"},   // stale
		{ID: 6, Name: "homelab-darkcloud"},      // not ghr's
		{ID: 7, Name: "ghr-darkcloud-x-123456"}, // another repo's naming, not 6 hex
	}
	// Same ID as darkcloud's live instance, but a different repo: stale.
	h.gh.listed["darkmem"] = []github.Runner{{ID: 8, Name: "ghr-darkmem-aaaaaa"}}
	h.m.Reconcile(context.Background(), h.cfg)
	sort.Slice(h.gh.deleted, func(a, b int) bool { return h.gh.deleted[a] < h.gh.deleted[b] })
	if !reflect.DeepEqual(h.gh.deleted, []int64{5, 8}) {
		t.Fatalf("deleted %v", h.gh.deleted)
	}
	if _, err := os.Stat(h.m.instanceDir("eeeeee")); !os.IsNotExist(err) {
		t.Fatal("orphan dir kept")
	}
	if _, err := os.Stat(h.m.instanceDir("aaaaaa")); err != nil {
		t.Fatal("live dir removed")
	}
}

// An active unit no instance tracks is stopped before its registration is deleted.
func TestReconcileStopsUntrackedUnitBeforeDeletingRegistration(t *testing.T) {
	h := newHarness(t)
	h.sd.active["ghr-runner-ffffff"] = true
	h.gh.listed["darkmem"] = []github.Runner{{ID: 3, Name: "ghr-darkmem-ffffff"}}
	h.m.Reconcile(context.Background(), h.cfg)
	if !reflect.DeepEqual(h.sd.stopped, []string{"ghr-runner-ffffff"}) || !reflect.DeepEqual(h.gh.deleted, []int64{3}) {
		t.Fatalf("stopped %v deleted %v", h.sd.stopped, h.gh.deleted)
	}
}

// A daemon that crashed after appending history but before removing the pending
// record and the instance dir finishes the job again without a duplicate line.
func TestFinishAfterCrashAppendsHistoryOnce(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkmem", 1, true)
	entry := model.HistoryEntry{ID: "aaaaaa", Repo: "darkmem", RunID: 77, Conclusion: "success", FinishedAt: h.now}
	os.MkdirAll(h.m.Paths.Pending, 0o755)
	writeJSON(h.m.pendingPath("aaaaaa"), pending{Entry: entry, RunnerName: "ghr-darkmem-aaaaaa", Deadline: h.now.Add(time.Hour)})
	h.m.History.Append(entry)
	h.gh.setJobs(77, completedJob(h, 77, "ghr-darkmem-aaaaaa", "success"))
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	if h.state("aaaaaa") != "gone" || !reflect.DeepEqual(h.history(t), []string{"aaaaaa success"}) {
		t.Fatalf("state %s history %v", h.state("aaaaaa"), h.history(t))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./internal/runner/`
Expected: FAIL — FAIL: `h.m.Adopt undefined`

- [ ] **Step 3: Write `internal/runner/adopt.go`**

```go
package runner

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
)

// activeUnits returns the IDs of running ghr-runner-* units.
func (m *Manager) activeUnits(ctx context.Context) (map[string]bool, error) {
	units, err := m.SD.List(ctx, UnitPrefix)
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, u := range units {
		out[strings.TrimPrefix(u, UnitPrefix)] = true
	}
	return out, nil
}

// Adopt rebuilds state after a daemon start from running units and instance dirs.
// Instances whose unit is gone are cleaned up. A unit without a readable
// ghr.json is stopped, and its dir is removed only once the stop succeeded;
// Reconcile retries a failed stop and deletes the registration.
func (m *Manager) Adopt(ctx context.Context) error {
	active, err := m.activeUnits(ctx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(m.Paths.Instances, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(m.Paths.Instances)
	if err != nil {
		return err
	}
	now := m.Now()
	adopted, exited := 0, 0
	var toFinish []string
	for _, e := range entries {
		id := e.Name()
		if !e.IsDir() || !idRe.MatchString(id) {
			continue
		}
		dir := m.instanceDir(id)
		var meta Meta
		if err := readJSON(filepath.Join(dir, MetaFile), &meta); err != nil || meta.ID != id {
			if active[id] {
				if err := m.SD.Stop(ctx, UnitPrefix+id); err != nil {
					m.Events.Add("warn", "", "instance %s has no readable %s and its unit did not stop (%v); reconciliation retries", id, MetaFile, err)
					delete(active, id)
					continue
				}
			}
			os.RemoveAll(dir)
			m.Events.Add("warn", "", "instance %s had no readable %s; stopped and removed", id, MetaFile)
			delete(active, id)
			continue
		}
		// start_timeout counts from the spawn, so restarts cannot extend it.
		inst := &instance{Meta: meta, State: sched.Starting, StateSince: meta.SpawnedAt}
		if inst.StateSince.IsZero() {
			inst.StateSince = now
		}
		var rec JobRecord
		if readJSON(filepath.Join(dir, JobFile), &rec) == nil {
			inst.State = sched.Busy
			inst.StateSince = now
			runID, _ := strconv.ParseInt(rec.RunID, 10, 64)
			started, ok := parseTime(rec.StartedAt)
			if !ok {
				started = now
			}
			inst.Job = &model.JobInfo{RunID: runID, RunNumber: rec.RunNumber, Workflow: rec.Workflow, Name: rec.Job, StartedAt: started}
		}
		m.mu.Lock()
		m.insts[id] = inst
		m.mu.Unlock()
		if active[id] {
			adopted++
		} else {
			exited++
			toFinish = append(toFinish, id)
		}
		delete(active, id)
	}
	for id := range active {
		if err := m.SD.Stop(ctx, UnitPrefix+id); err != nil {
			m.Events.Add("warn", "", "unit %s%s has no instance dir and did not stop (%v); reconciliation retries", UnitPrefix, id, err)
			continue
		}
		m.Events.Add("warn", "", "unit %s%s had no instance dir; stopped", UnitPrefix, id)
	}
	for _, id := range toFinish {
		m.beginFinish(id)
	}
	m.Events.Add("info", "", "adopted %d running runners; cleaning up %d that exited while ghr was down", adopted, exited)
	return nil
}

// Reconcile works from the real unit inventory: it stops running units no
// instance tracks, deletes ghr-<repo>-<id> registrations that match neither a
// tracked instance (same repo and runner ID) nor a running unit, and removes
// instance dirs whose unit has confirmed exited.
func (m *Manager) Reconcile(ctx context.Context, cfg *config.Config) {
	running, err := m.activeUnits(ctx)
	if err != nil {
		m.Events.Add("warn", "", "reconcile: list units: %v", err)
		return
	}
	known := map[string]instance{}
	for _, i := range m.snapshot() {
		known[i.ID] = i
	}
	for id := range running {
		if _, ok := known[id]; ok {
			continue
		}
		if err := m.SD.Stop(ctx, UnitPrefix+id); err != nil {
			m.Events.Add("warn", "", "reconcile: stop untracked unit %s%s: %v", UnitPrefix, id, err)
			continue
		}
		delete(running, id)
		m.Events.Add("warn", "", "reconcile: stopped untracked unit %s%s", UnitPrefix, id)
	}
	for _, r := range cfg.Repos {
		runners, err := m.GH.ListRunners(ctx, r.Name)
		if err != nil {
			continue
		}
		prefix := "ghr-" + r.Name + "-"
		for _, rn := range runners {
			id := strings.TrimPrefix(rn.Name, prefix)
			if !strings.HasPrefix(rn.Name, prefix) || !idRe.MatchString(id) {
				continue
			}
			if i, ok := known[id]; ok {
				if strings.EqualFold(i.Repo, r.Name) && i.RunnerID == rn.ID {
					continue
				}
			} else if running[id] {
				continue // an untracked unit that failed to stop may still use it
			}
			err := m.GH.DeleteRunner(ctx, r.Name, rn.ID)
			switch {
			case err == nil:
				m.Events.Add("info", r.Name, "removed stale registration %s", rn.Name)
			case github.IsKind(err, github.ErrUnprocessable):
				// busy; retried at the next reconciliation
			default:
				m.Events.Add("warn", r.Name, "remove stale registration %s: %v", rn.Name, err)
			}
		}
	}
	entries, _ := os.ReadDir(m.Paths.Instances)
	for _, e := range entries {
		id := e.Name()
		if _, ok := known[id]; !e.IsDir() || !idRe.MatchString(id) || ok || running[id] {
			continue
		}
		if active, err := m.SD.Active(ctx, UnitPrefix+id); err == nil && !active {
			os.RemoveAll(m.instanceDir(id))
		}
	}
}
```

- [ ] **Step 4: Run the check**

Run: `go test -count=1 -timeout 180s ./internal/runner/`
Expected: PASS (exit 0).

- [ ] **Step 5: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/runner/adopt_test.go internal/runner/adopt.go
git commit -m "feat(runner): adopt units and reconcile runners"
```

---

### Task 17: Runner tick: demand, errors, spawning

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `internal/runner/tick.go`
- Test: `internal/runner/tick_test.go`

**Interfaces:**
- Consumes: the Task 13–16 internals (Contracts → runner); `sched.Plan`, `github.APIError` (Contracts).
- Produces: `Manager.Tick` and the Task 17 internals (Contracts → runner).

**Items:** 2, 3

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing test `internal/runner/tick_test.go`**

```go
package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
)

func queuedRun(h *harness, repo string, runID int64, jobs ...github.Job) {
	h.gh.runs[repo+"/queued"] = append(h.gh.runs[repo+"/queued"], github.Run{ID: runID, Status: "queued"})
	h.gh.jobs[runID] = jobs
}

func TestGatherDemandFiltersLabelsAndConfirms(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	queuedRun(h, "darkmem", 1,
		github.Job{ID: 11, Status: "queued", Labels: []string{"self-hosted", "Linux", "homelab"}, CreatedAt: h.now},
		github.Job{ID: 12, Status: "queued", Labels: []string{"ubuntu-latest"}, CreatedAt: h.now},
		github.Job{ID: 13, Status: "in_progress", RunID: 1, Name: "build", WorkflowName: "CI", RunnerName: "ghr-darkmem-aaaaaa", HTMLURL: "u"},
	)
	h.gh.runs["darkmem/in_progress"] = []github.Run{{ID: 1}} // same run listed twice must be read once
	h.m.gatherDemand(context.Background(), h.cfg, h.now)
	st := h.m.Status()
	var mem int
	for _, r := range st.Repos {
		if r.Name == "darkmem" {
			mem = r.Queued
		}
	}
	if mem != 1 {
		t.Fatalf("darkmem queued = %d", mem)
	}
	if h.state("aaaaaa") != "busy" || st.Instances[0].Job.Name != "build" || st.Instances[0].Job.Workflow != "CI" {
		t.Fatalf("state %s job %+v", h.state("aaaaaa"), st.Instances[0].Job)
	}
}

// A run waiting on an environment protection rule can hold an independent
// queued job; listing status=waiting runs finds it and a runner is spawned.
func TestQueuedJobInWaitingRunIsServed(t *testing.T) {
	h := newHarness(t)
	h.gh.runs["darkmem/waiting"] = []github.Run{{ID: 9, Status: "waiting"}}
	h.gh.jobs[9] = []github.Job{
		{ID: 91, Status: "waiting", Labels: []string{"homelab"}, CreatedAt: h.now},
		{ID: 92, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now},
	}
	h.m.Tick(context.Background())
	if len(h.gh.jitCalls) != 1 || !strings.HasPrefix(h.gh.jitCalls[0], "ghr-darkmem-") {
		t.Fatalf("jit calls %v", h.gh.jitCalls)
	}
}

func TestTickSpawnsWithinCaps(t *testing.T) {
	h := newHarness(t)
	labels := []string{"self-hosted", "homelab"}
	queuedRun(h, "darkcloud", 1, github.Job{ID: 1, Status: "queued", Labels: labels, CreatedAt: h.now.Add(-3 * time.Minute)},
		github.Job{ID: 2, Status: "queued", Labels: labels, CreatedAt: h.now.Add(-2 * time.Minute)})
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: labels, CreatedAt: h.now.Add(-time.Minute)})
	h.m.Tick(context.Background())
	if len(h.sd.started) != 2 {
		t.Fatalf("started %d units", len(h.sd.started))
	}
	if !strings.Contains(h.gh.jitCalls[0], "darkcloud") || !strings.Contains(h.gh.jitCalls[1], "darkmem") {
		t.Fatalf("jit calls %v", h.gh.jitCalls)
	}
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if len(h.sd.started) != 2 {
		t.Fatalf("second tick over-spawned: %d", len(h.sd.started))
	}
}

func TestAuthErrorDegradesAndRecovers(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 401, Kind: github.ErrAuth, Message: "Bad credentials"})
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	st := h.m.Status()
	if !st.Degraded || len(h.sd.started) != 0 {
		t.Fatalf("degraded %v started %d", st.Degraded, len(h.sd.started))
	}
	h.now = h.now.Add(30 * time.Second)
	if h.m.apiAllowed(h.now) {
		t.Fatal("API should wait for the 60s recheck")
	}
	h.gh.setErr("ListRuns darkcloud", nil)
	h.now = h.now.Add(31 * time.Second)
	h.m.Tick(context.Background())
	if h.m.Status().Degraded || len(h.sd.started) != 1 {
		t.Fatalf("not recovered: degraded %v started %d", h.m.Status().Degraded, len(h.sd.started))
	}
}

// The first repo answers, the second fails authentication: still degraded, no spawn.
func TestAuthFailureOnSecondRepoKeepsDegraded(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 401, Kind: github.ErrAuth})
	h.m.Tick(context.Background())
	h.gh.setErr("ListRuns darkcloud", nil)
	h.gh.setErr("ListRuns darkmem", &github.APIError{Status: 401, Kind: github.ErrAuth})
	queuedRun(h, "darkcloud", 1, github.Job{ID: 1, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.now = h.now.Add(authRecheck)
	h.m.Tick(context.Background())
	if !h.m.Status().Degraded || len(h.sd.started) != 0 {
		t.Fatalf("degraded %v started %d", h.m.Status().Degraded, len(h.sd.started))
	}
}

func TestRateLimitPausesWithoutDegrading(t *testing.T) {
	h := newHarness(t)
	retry := h.now.Add(5 * time.Minute)
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: retry})
	h.m.Tick(context.Background())
	if h.m.Status().Degraded || h.m.apiAllowed(h.now.Add(time.Minute)) || !h.m.apiAllowed(retry) {
		t.Fatal("rate limit should pause until reset without degrading")
	}
}

// A rate limit found while polling demand stops every later API phase of the tick.
func TestRateLimitStopsTheRestOfTheTick(t *testing.T) {
	h := newHarness(t)
	h.cfg.Mode = config.ModeAll // warm spawns would otherwise follow
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Minute)})
	h.m.Tick(context.Background())
	if n := h.gh.callCount(); n != 1 {
		t.Fatalf("API calls after the rate limit: %v", h.gh.calls)
	}
	h.now = h.now.Add(30 * time.Second)
	h.m.Tick(context.Background())
	if n := h.gh.callCount(); n != 1 {
		t.Fatalf("API calls before RetryAt: %v", h.gh.calls)
	}
}

func TestNotFoundIsPerRepoWithBackoff(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 404, Kind: github.ErrNotFound})
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	st := h.m.Status()
	if !strings.Contains(st.Repos[0].Error, "token lacks access") || len(h.sd.started) != 1 {
		t.Fatalf("repo error %q started %d", st.Repos[0].Error, len(h.sd.started))
	}
	h.m.mu.Lock()
	retry := h.m.retryAt["darkcloud"]
	h.m.mu.Unlock()
	if retry != h.now.Add(10*time.Second) {
		t.Fatalf("retryAt %v", retry)
	}
	h.m.apiErr("darkcloud", &github.APIError{Status: 502, Kind: github.ErrServer}, h.now)
	h.m.mu.Lock()
	retry = h.m.retryAt["darkcloud"]
	h.m.mu.Unlock()
	if retry != h.now.Add(20*time.Second) {
		t.Fatalf("second backoff %v", retry)
	}
}

// In all mode a repo in an error state gets no warm runner until a poll succeeds.
func TestAllModeSkipsWarmSpawnsForFailingRepo(t *testing.T) {
	for _, err := range []error{
		&github.APIError{Status: 404, Kind: github.ErrNotFound},
		&github.APIError{Status: 502, Kind: github.ErrServer},
		context.DeadlineExceeded,
	} {
		h := newHarness(t)
		h.cfg.Mode = config.ModeAll
		h.gh.setErr("ListRuns darkcloud", err)
		for tick := 0; tick < 3; tick++ {
			h.m.Tick(context.Background())
			h.now = h.now.Add(10 * time.Second)
		}
		for _, c := range h.gh.jitCalls {
			if strings.Contains(c, "darkcloud") {
				t.Fatalf("%v: warm spawn into a failing repo: %v", err, h.gh.jitCalls)
			}
		}
		if len(h.gh.jitCalls) != 1 {
			t.Fatalf("%v: darkmem's warm runner missing: %v", err, h.gh.jitCalls)
		}
		h.gh.setErr("ListRuns darkcloud", nil)
		h.now = h.now.Add(maxBackoff)
		h.m.Tick(context.Background())
		if len(h.gh.jitCalls) != 2 {
			t.Fatalf("%v: no warm runner after recovery: %v", err, h.gh.jitCalls)
		}
	}
}

// A pause applied while the tick was polling must not be overridden by its spawns.
func TestConfigChangeDuringTickStopsSpawning(t *testing.T) {
	h := newHarness(t)
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	paused := h.cfg.Clone()
	paused.Repo("darkmem").Paused = true
	polls := 0
	h.m.Config = func() *config.Config {
		polls++
		if polls == 1 {
			return h.cfg // the snapshot Tick plans with
		}
		return paused // the operator paused darkmem meanwhile
	}
	h.m.Tick(context.Background())
	if len(h.gh.jitCalls) != 0 {
		t.Fatalf("spawned with a stale config: %v", h.gh.jitCalls)
	}
}

// After a repo's labels change, its idle runner (registered with the old labels)
// no longer covers jobs: it is stopped and a runner with the new labels spawns.
func TestLabelChangeReplacesStaleIdleRunner(t *testing.T) {
	h := newHarness(t)
	h.cfg.Repos[1].Max = new(int)
	*h.cfg.Repos[1].Max = 2
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.gh.setRunner(101, "online", false)
	h.m.refreshRunners(context.Background())
	changed := h.cfg.Clone()
	changed.Repo("darkmem").Labels = []string{"gpu"}
	h.cfg = changed
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab", "gpu"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	if len(h.sd.stopped) != 1 || h.sd.stopped[0] != "ghr-runner-aaaaaa" {
		t.Fatalf("stale idle runner not stopped: %v", h.sd.stopped)
	}
	if len(h.gh.jitCalls) != 2 || h.gh.jitCalls[1] != "ghr-darkmem-bbbbbb homelab,gpu" {
		t.Fatalf("jit calls %v", h.gh.jitCalls)
	}
}

func TestPausedRepoGetsNoRunners(t *testing.T) {
	h := newHarness(t)
	h.cfg.Repos[1].Paused = true
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	if len(h.sd.started) != 0 {
		t.Fatalf("started %d", len(h.sd.started))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./internal/runner/`
Expected: FAIL — FAIL: `h.m.gatherDemand undefined`

- [ ] **Step 3: Write `internal/runner/tick.go`**

```go
package runner

import (
	"context"
	"errors"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
)

// Tick runs one poll → schedule → spawn → reap cycle. Every phase that calls
// the GitHub API re-checks apiAllowed, because an earlier phase may have hit
// a rate limit or an auth failure.
func (m *Manager) Tick(ctx context.Context) {
	cfg := m.Config()
	now := m.Now()
	m.ticks++
	m.refreshUnits(ctx)
	m.readJobFiles()
	if m.apiAllowed(now) {
		m.refreshRunners(ctx)
	}
	if m.apiAllowed(now) {
		m.gatherDemand(ctx, cfg, now)
	}
	if m.apiAllowed(now) {
		m.stopIdle(ctx, cfg, now)
	}
	m.stopStartTimedOut(ctx, cfg, now)
	if m.apiAllowed(now) && !m.isDegraded() {
		m.spawnPlanned(ctx, cfg, now)
	}
	if m.apiAllowed(now) && !m.isDegraded() && m.ticks%30 == 0 {
		m.Reconcile(ctx, cfg)
	}
	if m.apiAllowed(now) {
		m.finalizePending(ctx)
	}
	if m.ticks%10 == 1 {
		m.checkDisk(ctx, cfg)
	}
	if now.Sub(m.lastPrune) >= 24*time.Hour {
		m.prune(cfg, now)
	}
}

// gatherDemand lists matching queued jobs per repo and confirms our in-progress
// jobs. Degraded clears only after a tick in which some repo answered and none
// failed authentication.
func (m *Manager) gatherDemand(ctx context.Context, cfg *config.Config, now time.Time) {
	ours := map[string]string{}
	for _, i := range m.snapshot() {
		ours[i.RunnerName] = i.ID
	}
	demand := sched.Demand{}
	anyOK, authFailed := false, false
	for _, r := range cfg.Repos {
		if r.Paused {
			continue
		}
		m.mu.Lock()
		wait := now.Before(m.retryAt[r.Name])
		m.mu.Unlock()
		if wait {
			continue
		}
		jobs, err := m.repoDemand(ctx, cfg, r, ours, now)
		if err != nil {
			if github.IsKind(err, github.ErrAuth) {
				authFailed = true
			}
			m.apiErr(r.Name, err, now)
			if authFailed || !m.apiAllowed(now) {
				break
			}
			continue
		}
		anyOK = true
		demand[r.Name] = jobs
		m.mu.Lock()
		delete(m.fails, r.Name)
		delete(m.retryAt, r.Name)
		delete(m.repoErr, r.Name)
		m.mu.Unlock()
	}
	m.mu.Lock()
	m.demand = demand
	recovered := anyOK && !authFailed && m.degraded
	if recovered {
		m.degraded = false
		m.degradedReason = ""
	}
	m.mu.Unlock()
	if recovered {
		m.Events.Add("ok", "", "GitHub token accepted again")
	}
}

func (m *Manager) repoDemand(ctx context.Context, cfg *config.Config, r config.Repo, ours map[string]string, now time.Time) ([]sched.QueuedJob, error) {
	eff := cfg.EffectiveLabels(r)
	seen := map[int64]bool{}
	var out []sched.QueuedJob
	for _, status := range []string{"queued", "in_progress", "waiting"} {
		runs, err := m.GH.ListRuns(ctx, r.Name, status)
		if err != nil {
			return nil, err
		}
		for _, run := range runs {
			if seen[run.ID] {
				continue
			}
			seen[run.ID] = true
			jobs, err := m.GH.ListJobs(ctx, r.Name, run.ID)
			if err != nil {
				return nil, err
			}
			for _, j := range jobs {
				switch j.Status {
				case "queued":
					if sched.MatchLabels(j.Labels, eff) {
						out = append(out, sched.QueuedJob{Repo: r.Name, ID: j.ID, CreatedAt: j.CreatedAt})
					}
				case "in_progress":
					if id, ok := ours[j.RunnerName]; ok {
						m.confirm(id, j, now)
					}
				}
			}
		}
	}
	return out, nil
}

func (m *Manager) confirm(id string, j github.Job, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.insts[id]
	if !ok || i.State == sched.Cleaning {
		return
	}
	if i.State != sched.Busy {
		i.State = sched.Busy
		i.StateSince = now
	}
	i.JobConfirmed = true
	started := now
	if j.StartedAt != nil {
		started = *j.StartedAt
	}
	runNumber := ""
	if i.Job != nil {
		runNumber = i.Job.RunNumber
	}
	i.Job = &model.JobInfo{RunID: j.RunID, RunNumber: runNumber, Workflow: j.WorkflowName, Name: j.Name, HTMLURL: j.HTMLURL, StartedAt: started}
}

// spawnPlanned spawns what sched.Plan decides. Repos in an error state are left
// out of planning (no warm spawns into a failing repo) until a demand poll
// succeeds. Spawning stops as soon as the live config differs from the one the
// plan used, so a pause or cap change made during polling is never overridden.
func (m *Manager) spawnPlanned(ctx context.Context, cfg *config.Config, now time.Time) {
	m.mu.Lock()
	demand := m.demand
	planCfg := *cfg
	planCfg.Repos = nil
	for _, r := range cfg.Repos {
		if m.repoErr[r.Name] == "" && !now.Before(m.retryAt[r.Name]) {
			planCfg.Repos = append(planCfg.Repos, r)
		}
	}
	m.mu.Unlock()
	for _, s := range sched.Plan(&planCfg, m.schedInstances(cfg), demand, now) {
		if m.Config() != cfg {
			return
		}
		if err := m.spawn(ctx, cfg, s.Repo); err != nil {
			m.Events.Add("error", s.Repo, "spawn failed: %v", err)
			var ae *github.APIError
			if errors.As(err, &ae) && (ae.Kind == github.ErrAuth || ae.Kind == github.ErrRateLimit) {
				m.apiErr(s.Repo, err, now)
				return
			}
		}
	}
}
```

- [ ] **Step 4: Run the check**

Run: `go test -count=1 -timeout 180s ./internal/runner/`
Expected: PASS (exit 0).

- [ ] **Step 5: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/runner/tick_test.go internal/runner/tick.go
git commit -m "feat(runner): poll demand and spawn within caps"
```

---

### Task 18: Daemon config and token store

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `internal/daemon/store.go`
- Test: `internal/daemon/store_test.go`

**Interfaces:**
- Consumes: `config.Load`, `config.Save`, `Config.Clone`, `Config.Validate` (Contracts → config).
- Produces: `daemon.Store` (Contracts → daemon).

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing test `internal/daemon/store_test.go`**

```go
package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/config"
)

const cfgYAML = `
owner: darkraise
mode: queue
global_max: 2
labels: [homelab]
repos:
  - name: darkcloud
    max: 1
  - name: darkmem
`

func newStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(cfgYAML), 0o600)
	os.WriteFile(filepath.Join(dir, "token"), []byte("tok1\n"), 0o600)
	s, _, err := OpenStore(filepath.Join(dir, "config.yaml"), filepath.Join(dir, "token"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStoreUpdateReloadToken(t *testing.T) {
	s := newStore(t)
	if s.Token() != "tok1" || s.Config().GlobalMax != 2 {
		t.Fatalf("open: token %q cfg %+v", s.Token(), s.Config())
	}
	before := s.Config()
	if _, err := s.Update(func(c *config.Config) error { c.GlobalMax = 0; return nil }); err == nil {
		t.Fatal("invalid update accepted")
	}
	if s.Config() != before {
		t.Fatal("invalid update swapped config")
	}
	if _, err := s.Update(func(c *config.Config) error { c.GlobalMax = 3; return nil }); err != nil {
		t.Fatal(err)
	}
	if before.GlobalMax != 2 || s.Config().GlobalMax != 3 {
		t.Fatal("update mutated the old config or did not apply")
	}
	os.WriteFile(s.ConfigPath, []byte("owner: \"\"\n"), 0o600)
	if _, err := s.Reload(); err == nil || s.Config().GlobalMax != 3 {
		t.Fatalf("bad reload: err %v cfg %+v", err, s.Config())
	}
	os.WriteFile(s.ConfigPath, []byte(strings.Replace(cfgYAML, "owner: darkraise", "owner: someone-else", 1)), 0o600)
	if _, err := s.Reload(); err == nil || !strings.Contains(err.Error(), "restart ghr") || s.Config().Owner != "darkraise" {
		t.Fatalf("owner change accepted: err %v owner %s", err, s.Config().Owner)
	}
	if err := s.SetToken("tok2"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(s.TokenPath)
	if s.Token() != "tok2" || strings.TrimSpace(string(data)) != "tok2" {
		t.Fatalf("token %q file %q", s.Token(), data)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./internal/daemon/`
Expected: FAIL — FAIL: `undefined: OpenStore`

- [ ] **Step 3: Write `internal/daemon/store.go`**

```go
// Package daemon wires config, the runner manager and the control API into the ghr service.
package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/darkraise/ghr/internal/config"
)

// Store owns the live config and token. Writes are validated and saved atomically.
type Store struct {
	ConfigPath string
	TokenPath  string

	mu    sync.Mutex
	cfg   atomic.Pointer[config.Config]
	token atomic.Pointer[string]
}

func OpenStore(configPath, tokenPath string) (*Store, []string, error) {
	s := &Store{ConfigPath: configPath, TokenPath: tokenPath}
	warnings, err := s.Reload()
	return s, warnings, err
}

func (s *Store) Config() *config.Config { return s.cfg.Load() }

func (s *Store) Token() string {
	if t := s.token.Load(); t != nil {
		return *t
	}
	return ""
}

// Reload re-reads config and token; on any error the previous values stay active.
// The owner cannot change while the daemon runs: the GitHub client and every
// live runner belong to the owner it started with.
func (s *Store) Reload() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, warnings, err := config.Load(s.ConfigPath)
	if err != nil {
		return nil, err
	}
	if cur := s.cfg.Load(); cur != nil && cur.Owner != cfg.Owner {
		return nil, fmt.Errorf("owner changed from %s to %s: restart ghr to switch owners", cur.Owner, cfg.Owner)
	}
	data, err := os.ReadFile(s.TokenPath)
	if err != nil {
		return nil, err
	}
	tok := strings.TrimSpace(string(data))
	if tok == "" {
		return nil, errors.New("token file is empty")
	}
	s.cfg.Store(cfg)
	s.token.Store(&tok)
	return warnings, nil
}

// Update applies fn to a copy of the config, validates, saves, then swaps it in.
func (s *Store) Update(fn func(c *config.Config) error) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.Config().Clone()
	if err := fn(c); err != nil {
		return nil, err
	}
	warnings, err := c.Validate()
	if err != nil {
		return nil, err
	}
	if err := config.Save(s.ConfigPath, c); err != nil {
		return nil, err
	}
	s.cfg.Store(c)
	return warnings, nil
}

// SetToken writes the token file (0600, atomic) and swaps it in.
func (s *Store) SetToken(tok string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tmp, err := os.CreateTemp(filepath.Dir(s.TokenPath), ".token-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(tok + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), s.TokenPath); err != nil {
		return err
	}
	s.token.Store(&tok)
	return nil
}
```

- [ ] **Step 4: Run the check**

Run: `go test -count=1 -timeout 180s ./internal/daemon/`
Expected: PASS (exit 0).

- [ ] **Step 5: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/store_test.go internal/daemon/store.go
git commit -m "feat(daemon): add atomic config and token store"
```

---

### Task 19: Daemon API backend

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `internal/daemon/backend.go`
- Test: `internal/daemon/backend_test.go`

**Interfaces:**
- Consumes: `api.Backend`, `api.Error` helpers, `daemon.Store`, `runner.ErrUnknownRunner`, `github` (Contracts).
- Produces: `daemon.Backend` (with `Wake` and `FinalizeRemovals`) and the backend's `Manager` and `GitHub` interfaces (Contracts → daemon).

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing test `internal/daemon/backend_test.go`**

```go
package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/history"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/runner"
)

type fakeManager struct {
	insts   []model.InstanceStatus
	cleared bool
	killed  []string
}

func (f *fakeManager) Status() model.Status { return model.Status{Instances: f.insts} }
func (f *fakeManager) RunnerLog(id, cursor string) (model.LogChunk, error) {
	return model.LogChunk{}, runner.ErrUnknownRunner(id)
}
func (f *fakeManager) RunnerContainers(ctx context.Context, id string) ([]model.Container, error) {
	return nil, runner.ErrUnknownRunner(id)
}
func (f *fakeManager) RunnerRepoAndRun(id string) (string, int64, string, error) {
	if id != "aaaaaa" {
		return "", 0, "", runner.ErrUnknownRunner(id)
	}
	return "darkcloud", 55, "ghr-darkcloud-aaaaaa", nil
}
func (f *fakeManager) Kill(ctx context.Context, id string) error {
	f.killed = append(f.killed, id)
	return nil
}
func (f *fakeManager) ClearDegraded() { f.cleared = true }

type fakeGH struct {
	repos  map[string]*github.Repository
	forgot bool
}

func (f *fakeGH) GetRepo(ctx context.Context, repo string) (*github.Repository, error) {
	r, ok := f.repos[repo]
	if !ok {
		return nil, &github.APIError{Status: 404, Kind: github.ErrNotFound}
	}
	return r, nil
}
func (f *fakeGH) ListJobs(ctx context.Context, repo string, runID int64) ([]github.Job, error) {
	return []github.Job{
		{RunnerName: "someone-else", Steps: []github.Step{{Name: "x"}}},
		{RunnerName: "ghr-darkcloud-aaaaaa", Steps: []github.Step{{Number: 1, Name: "checkout", Status: "completed", Conclusion: "success"}}},
	}, nil
}
func (f *fakeGH) ForgetCache() { f.forgot = true }

func newBackend(t *testing.T) (*Backend, *fakeManager, *fakeGH) {
	t.Helper()
	m := &fakeManager{}
	gh := &fakeGH{repos: map[string]*github.Repository{
		"newrepo": {Private: true}, "public": {Private: false}, "darkcloud": {Private: true},
	}}
	b := &Backend{
		Store: newStore(t), M: m, GH: gh, Events: events.New(),
		Hist: &history.Store{Path: filepath.Join(t.TempDir(), "h.jsonl")},
		CheckToken: func(ctx context.Context, token, repo string) error {
			if token == "bad" {
				return errors.New("401 Bad credentials")
			}
			return nil
		},
	}
	return b, m, gh
}

func apiStatus(err error) int {
	var ae *api.Error
	if errors.As(err, &ae) {
		return ae.Status
	}
	return 0
}

func TestPatchConfig(t *testing.T) {
	b, _, _ := newBackend(t)
	all, two, idle := "all", 2, "10m"
	paused := true
	if err := b.PatchConfig(model.ConfigPatch{Mode: &all, IdleTimeout: &idle, Repos: map[string]model.RepoPatch{"darkmem": {Max: &two, Paused: &paused}}}); err != nil {
		t.Fatal(err)
	}
	c := b.Store.Config()
	if c.Mode != "all" || c.IdleTimeout.String() != "10m0s" || *c.Repo("darkmem").Max != 2 || !c.Repo("darkmem").Paused {
		t.Fatalf("cfg %+v", c)
	}
	bad := "fast"
	if err := b.PatchConfig(model.ConfigPatch{Mode: &bad}); apiStatus(err) != 400 {
		t.Fatalf("bad mode err %v", err)
	}
	if err := b.PatchConfig(model.ConfigPatch{Repos: map[string]model.RepoPatch{"nope": {Max: &two}}}); apiStatus(err) != 404 {
		t.Fatalf("unknown repo err %v", err)
	}
}

func TestAddRepo(t *testing.T) {
	b, _, _ := newBackend(t)
	ctx := context.Background()
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "missing"}); apiStatus(err) != 400 || !strings.Contains(err.Error(), "repository access") {
		t.Fatalf("missing err %v", err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "public"}); apiStatus(err) != 409 {
		t.Fatalf("public err %v", err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "darkcloud"}); apiStatus(err) != 409 {
		t.Fatalf("duplicate err %v", err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "public", AllowPublic: true}); err != nil {
		t.Fatal(err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "newrepo", Labels: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	if b.Store.Config().Repo("newrepo") == nil {
		t.Fatal("repo not saved")
	}
}

func TestRemoveRepoWaitsForRunners(t *testing.T) {
	b, m, _ := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa", Repo: "darkmem"}}
	if err := b.RemoveRepo("darkmem"); err != nil {
		t.Fatal(err)
	}
	if !b.Store.Config().Repo("darkmem").Paused {
		t.Fatal("not paused")
	}
	b.FinalizeRemovals()
	if b.Store.Config().Repo("darkmem") == nil {
		t.Fatal("removed while a runner was live")
	}
	m.insts = nil
	b.FinalizeRemovals()
	if b.Store.Config().Repo("darkmem") != nil {
		t.Fatal("not removed after runners finished")
	}
	if err := b.RemoveRepo("nope"); apiStatus(err) != 404 {
		t.Fatalf("unknown repo err %v", err)
	}
}

// The removal intent lives in config.yaml: a restarted daemon (a new Store and
// Backend over the same files) still finishes it.
func TestRemovalSurvivesRestart(t *testing.T) {
	b, m, _ := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa", Repo: "darkmem"}}
	if err := b.RemoveRepo("darkmem"); err != nil {
		t.Fatal(err)
	}
	store, _, err := OpenStore(b.Store.ConfigPath, b.Store.TokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if r := store.Config().Repo("darkmem"); r == nil || !r.Paused || !r.Removing {
		t.Fatalf("removal intent not persisted: %+v", r)
	}
	restarted := &Backend{Store: store, M: &fakeManager{}, GH: b.GH, Events: events.New(), Hist: b.Hist}
	restarted.FinalizeRemovals()
	if store.Config().Repo("darkmem") != nil {
		t.Fatal("restarted daemon did not finish the removal")
	}
}

func TestResumeCancelsRemoval(t *testing.T) {
	b, m, _ := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa", Repo: "darkmem"}}
	b.RemoveRepo("darkmem")
	resume := false
	if err := b.PatchConfig(model.ConfigPatch{Repos: map[string]model.RepoPatch{"darkmem": {Paused: &resume}}}); err != nil {
		t.Fatal(err)
	}
	m.insts = nil
	b.FinalizeRemovals()
	if r := b.Store.Config().Repo("darkmem"); r == nil || r.Paused || r.Removing {
		t.Fatalf("resumed repo removed or still marked: %+v", r)
	}
}

// A rejected patch changes nothing, including a pending removal.
func TestRejectedPatchKeepsRemoval(t *testing.T) {
	b, _, _ := newBackend(t)
	b.RemoveRepo("darkmem")
	resume, zero := false, 0
	err := b.PatchConfig(model.ConfigPatch{GlobalMax: &zero, Repos: map[string]model.RepoPatch{"darkmem": {Paused: &resume}}})
	if apiStatus(err) != 400 {
		t.Fatalf("err %v", err)
	}
	if r := b.Store.Config().Repo("darkmem"); !r.Paused || !r.Removing {
		t.Fatalf("removal lost by a rejected patch: %+v", r)
	}
}

func TestResumeAllKeepsRemovingReposPaused(t *testing.T) {
	b, _, _ := newBackend(t)
	b.SetPausedAll(true)
	b.RemoveRepo("darkmem")
	if err := b.SetPausedAll(false); err != nil {
		t.Fatal(err)
	}
	c := b.Store.Config()
	if c.Repo("darkcloud").Paused || !c.Repo("darkmem").Paused || !c.Repo("darkmem").Removing {
		t.Fatalf("repos %+v", c.Repos)
	}
}

func TestConfigChangesWakeTheLoop(t *testing.T) {
	b, _, _ := newBackend(t)
	wakes := 0
	b.Wake = func() { wakes++ }
	b.RemoveRepo("darkmem")
	b.SetPausedAll(true)
	zero := 0
	b.PatchConfig(model.ConfigPatch{GlobalMax: &zero}) // rejected: no wake
	if wakes != 2 {
		t.Fatalf("wakes = %d", wakes)
	}
}

func TestPauseAllStepsKillToken(t *testing.T) {
	b, m, gh := newBackend(t)
	ctx := context.Background()
	if err := b.SetPausedAll(true); err != nil {
		t.Fatal(err)
	}
	for _, r := range b.Store.Config().Repos {
		if !r.Paused {
			t.Fatalf("%s not paused", r.Name)
		}
	}
	steps, err := b.RunnerSteps(ctx, "aaaaaa")
	if err != nil || len(steps) != 1 || steps[0].Name != "checkout" {
		t.Fatalf("steps %+v err %v", steps, err)
	}
	if _, err := b.RunnerSteps(ctx, "zzzzzz"); apiStatus(err) != 404 {
		t.Fatalf("unknown runner err %v", err)
	}
	if _, err := b.RunnerLog("zzzzzz", ""); apiStatus(err) != 404 {
		t.Fatalf("log err %v", err)
	}
	if _, err := b.RunnerContainers(ctx, "zzzzzz"); apiStatus(err) != 404 {
		t.Fatalf("containers err %v", err)
	}
	if err := b.KillRunner(ctx, "aaaaaa"); err != nil || m.killed[0] != "aaaaaa" {
		t.Fatalf("kill err %v", err)
	}
	if err := b.SetToken(ctx, "bad"); apiStatus(err) != 400 || b.Store.Token() != "tok1" {
		t.Fatalf("bad token err %v token %q", err, b.Store.Token())
	}
	if err := b.SetToken(ctx, "tok2"); err != nil {
		t.Fatal(err)
	}
	if b.Store.Token() != "tok2" || !gh.forgot || !m.cleared {
		t.Fatal("token not applied")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./internal/daemon/`
Expected: FAIL — FAIL: `undefined: Backend`

- [ ] **Step 3: Write `internal/daemon/backend.go`**

```go
package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/history"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/runner"
)

// Manager is the part of *runner.Manager the backend uses.
type Manager interface {
	Status() model.Status
	RunnerLog(id, cursor string) (model.LogChunk, error)
	RunnerContainers(ctx context.Context, id string) ([]model.Container, error)
	RunnerRepoAndRun(id string) (repo string, runID int64, runnerName string, err error)
	Kill(ctx context.Context, id string) error
	ClearDegraded()
}

// GitHub is the part of the GitHub client the backend uses.
type GitHub interface {
	GetRepo(ctx context.Context, repo string) (*github.Repository, error)
	ListJobs(ctx context.Context, repo string, runID int64) ([]github.Job, error)
	ForgetCache()
}

type Backend struct {
	Store  *Store
	M      Manager
	GH     GitHub
	Events *events.Ring
	Hist   *history.Store
	// CheckToken validates a candidate token by reading repo with it.
	CheckToken func(ctx context.Context, token, repo string) error
	// Wake asks the run loop for an immediate tick after a config change, so a
	// pause stops idle runners at once; nil in tests.
	Wake func()
}

var _ api.Backend = (*Backend)(nil)

// errNotRemoving aborts a FinalizeRemovals update for a repo resumed meanwhile.
var errNotRemoving = errors.New("repo is no longer being removed")

func (b *Backend) Status() model.Status                { return b.M.Status() }
func (b *Backend) EventsAfter(seq int64) []model.Event { return b.Events.After(seq) }
func (b *Backend) Config() any                         { return b.Store.Config() }

func (b *Backend) History(repo, conclusion string, limit int) ([]model.HistoryEntry, error) {
	return b.Hist.Query(repo, conclusion, limit)
}

func notFoundIfUnknown(err error) error {
	var u runner.ErrUnknownRunner
	if errors.As(err, &u) {
		return api.NotFound(err.Error())
	}
	return err
}

func (b *Backend) RunnerLog(id, cursor string) (model.LogChunk, error) {
	c, err := b.M.RunnerLog(id, cursor)
	return c, notFoundIfUnknown(err)
}

func (b *Backend) RunnerContainers(ctx context.Context, id string) ([]model.Container, error) {
	cs, err := b.M.RunnerContainers(ctx, id)
	return cs, notFoundIfUnknown(err)
}

func (b *Backend) RunnerSteps(ctx context.Context, id string) ([]model.Step, error) {
	repo, runID, runnerName, err := b.M.RunnerRepoAndRun(id)
	if err != nil {
		return nil, notFoundIfUnknown(err)
	}
	if runID == 0 {
		return nil, nil
	}
	jobs, err := b.GH.ListJobs(ctx, repo, runID)
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		if j.RunnerName != runnerName {
			continue
		}
		steps := make([]model.Step, 0, len(j.Steps))
		for _, s := range j.Steps {
			steps = append(steps, model.Step{Number: s.Number, Name: s.Name, Status: s.Status, Conclusion: s.Conclusion})
		}
		return steps, nil
	}
	return nil, nil
}

func (b *Backend) KillRunner(ctx context.Context, id string) error {
	return notFoundIfUnknown(b.M.Kill(ctx, id))
}

// update applies fn through the store (validate, save, swap) and wakes the loop.
// fn works on a copy, so a rejected change leaves no partial state anywhere.
func (b *Backend) update(fn func(c *config.Config) error) error {
	warnings, err := b.Store.Update(fn)
	if err != nil {
		var ae *api.Error
		if errors.As(err, &ae) {
			return err
		}
		return api.BadRequest(err.Error())
	}
	for _, w := range warnings {
		b.Events.Add("warn", "", "config: %s", w)
	}
	if b.Wake != nil {
		b.Wake()
	}
	return nil
}

// PatchConfig applies a partial update. Resuming a repo that is being removed
// cancels the removal in the same write.
func (b *Backend) PatchConfig(p model.ConfigPatch) error {
	var cancelled []string
	err := b.update(func(c *config.Config) error {
		if p.Mode != nil {
			c.Mode = *p.Mode
		}
		if p.GlobalMax != nil {
			c.GlobalMax = *p.GlobalMax
		}
		for field, v := range map[*config.Duration]*string{&c.StartTimeout: p.StartTimeout, &c.IdleTimeout: p.IdleTimeout} {
			if v == nil {
				continue
			}
			d, err := config.ParseDuration(*v)
			if err != nil {
				return api.BadRequest(err.Error())
			}
			*field = d
		}
		for name, rp := range p.Repos {
			r := c.Repo(name)
			if r == nil {
				return api.NotFound("unknown repo " + name)
			}
			if rp.Max != nil {
				r.Max = rp.Max
			}
			if rp.Warm != nil {
				r.Warm = rp.Warm
			}
			if rp.Labels != nil {
				r.Labels = *rp.Labels
			}
			if rp.CleanupNamePrefixes != nil {
				r.CleanupNamePrefixes = *rp.CleanupNamePrefixes
			}
			if rp.Paused != nil {
				r.Paused = *rp.Paused
				if !r.Paused && r.Removing {
					r.Removing = false
					cancelled = append(cancelled, r.Name)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, name := range cancelled {
		b.Events.Add("info", name, "repo resumed; removal cancelled")
	}
	return nil
}

func (b *Backend) AddRepo(ctx context.Context, req model.AddRepoRequest) error {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return api.BadRequest("repo name is required")
	}
	if b.Store.Config().Repo(name) != nil {
		return api.Conflict("repo " + name + " is already configured")
	}
	repo, err := b.GH.GetRepo(ctx, name)
	if github.IsKind(err, github.ErrNotFound) {
		return api.BadRequest(fmt.Sprintf("token cannot see %s/%s: add it to the PAT's repository access first", b.Store.Config().Owner, name))
	}
	if err != nil {
		return err
	}
	if !repo.Private && !req.AllowPublic {
		return api.Conflict(name + " is public; self-hosted runners must only serve private repos (pass --allow-public to override)")
	}
	if err := b.update(func(c *config.Config) error {
		c.Repos = append(c.Repos, config.Repo{Name: name, Max: req.Max, Labels: req.Labels})
		return nil
	}); err != nil {
		return err
	}
	b.Events.Add("info", name, "repo added")
	return nil
}

// RemoveRepo pauses the repo and records the removal in config.yaml (paused +
// removing in one write), so it survives a restart; FinalizeRemovals deletes the
// repo once its runners are gone. A per-repo resume cancels it.
func (b *Backend) RemoveRepo(name string) error {
	if b.Store.Config().Repo(name) == nil {
		return api.NotFound("unknown repo " + name)
	}
	if err := b.update(func(c *config.Config) error {
		r := c.Repo(name)
		if r == nil {
			return api.NotFound("unknown repo " + name)
		}
		r.Paused = true
		r.Removing = true
		return nil
	}); err != nil {
		return err
	}
	b.Events.Add("info", name, "repo removal requested; waiting for its runners to finish")
	return nil
}

// FinalizeRemovals deletes repos marked removing that have no instances left.
// The removing flag is re-checked inside the store's update, so a repo resumed
// after the instance check is kept.
func (b *Backend) FinalizeRemovals() {
	var pending []string
	for _, r := range b.Store.Config().Repos {
		if r.Removing {
			pending = append(pending, r.Name)
		}
	}
	if len(pending) == 0 {
		return
	}
	live := map[string]bool{}
	for _, i := range b.M.Status().Instances {
		live[strings.ToLower(i.Repo)] = true
	}
	for _, name := range pending {
		if live[strings.ToLower(name)] {
			continue
		}
		_, err := b.Store.Update(func(c *config.Config) error {
			r := c.Repo(name)
			if r == nil || !r.Removing {
				return errNotRemoving
			}
			out := c.Repos[:0]
			for _, x := range c.Repos {
				if !strings.EqualFold(x.Name, name) {
					out = append(out, x)
				}
			}
			c.Repos = out
			return nil
		})
		switch {
		case errors.Is(err, errNotRemoving):
		case err != nil:
			b.Events.Add("warn", name, "remove repo: %v", err)
		default:
			b.Events.Add("info", name, "repo removed")
		}
	}
}

// SetPausedAll pauses or resumes every repo. Resume-all leaves repos that are
// being removed paused; only a per-repo resume cancels a removal.
func (b *Backend) SetPausedAll(paused bool) error {
	if err := b.update(func(c *config.Config) error {
		for i := range c.Repos {
			if !c.Repos[i].Removing {
				c.Repos[i].Paused = paused
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if paused {
		b.Events.Add("warn", "", "all repos paused (drain)")
	} else {
		b.Events.Add("info", "", "all repos resumed")
	}
	return nil
}

func (b *Backend) SetToken(ctx context.Context, token string) error {
	cfg := b.Store.Config()
	if len(cfg.Repos) > 0 {
		if err := b.CheckToken(ctx, token, cfg.Repos[0].Name); err != nil {
			return api.BadRequest("new token rejected: " + err.Error())
		}
	}
	if err := b.Store.SetToken(token); err != nil {
		return err
	}
	b.GH.ForgetCache()
	b.M.ClearDegraded()
	b.Events.Add("ok", "", "GitHub token replaced")
	return nil
}
```

- [ ] **Step 4: Run the check**

Run: `go test -count=1 -timeout 180s ./internal/daemon/`
Expected: PASS (exit 0).

- [ ] **Step 5: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/backend_test.go internal/daemon/backend.go
git commit -m "feat(daemon): implement control API backend"
```

---

### Task 20: Daemon run loop and `ghr daemon`

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `internal/daemon/run.go`
- Modify: `cmd/ghr/main_test.go`
- Test: `internal/daemon/run_test.go`
- Modify: `cmd/ghr/main.go`

**Interfaces:**
- Consumes: everything in Tasks 3–19 (Contracts).
- Produces: `daemon.Options`, `daemon.DefaultOptions`, `daemon.Run`, the `daemon` CLI case (Contracts → daemon, CLI entry, On-disk paths).

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing test `cmd/ghr/main_test.go`** `cmd/ghr/main_test.go` exists from Task 1; replace its whole content (it keeps `TestVersionAndUsage` and adds `TestDaemonFailsWithoutConfig`).

```go
package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestVersionAndUsage(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"version"}, strings.NewReader(""), &out, &errb); code != 0 || strings.TrimSpace(out.String()) != "dev" {
		t.Fatalf("version: exit %d out %q", code, out.String())
	}
	out.Reset()
	if code := run([]string{"help"}, strings.NewReader(""), &out, &errb); code != 0 || !strings.Contains(out.String(), "usage: ghr") {
		t.Fatalf("help: exit %d", code)
	}
	if code := run(nil, strings.NewReader(""), &out, &errb); code != 2 {
		t.Fatalf("no args: exit %d", code)
	}
}

func TestDaemonFailsWithoutConfig(t *testing.T) {
	if _, err := os.Stat("/etc/ghr/config.yaml"); err == nil {
		t.Skip("a real ghr config exists on this machine; not starting a daemon from a test")
	}
	var out, errb bytes.Buffer
	if code := run([]string{"daemon"}, strings.NewReader(""), &out, &errb); code != 1 || !strings.Contains(errb.String(), "ghr daemon:") {
		t.Fatalf("exit %d stderr %q", code, errb.String())
	}
}
```

- [ ] **Step 2: Write the failing test `internal/daemon/run_test.go`**

```go
package daemon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/runner"
	"github.com/darkraise/ghr/internal/system"
)

type recSD struct {
	mu      sync.Mutex
	started []string
	stopped []string
}

func (s *recSD) Start(_ context.Context, u system.UnitSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.started = append(s.started, u.Unit)
	return nil
}
func (s *recSD) Stop(_ context.Context, unit string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = append(s.stopped, unit)
	return nil
}
func (s *recSD) Active(context.Context, string) (bool, error) { return true, nil }
func (s *recSD) List(context.Context, string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.started...), nil
}
func (s *recSD) counts() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.started), len(s.stopped)
}

type nopDocker struct{}

func (nopDocker) ComposeContainers(context.Context) ([]system.ComposeContainer, error) {
	return nil, nil
}
func (nopDocker) Containers(context.Context) ([]system.NamedContainer, error) { return nil, nil }
func (nopDocker) ProjectContainers(context.Context, string) ([]system.ProjectContainer, error) {
	return nil, nil
}
func (nopDocker) ContainerIDsByLabel(context.Context, string) ([]string, error) { return nil, nil }
func (nopDocker) RemoveContainers(context.Context, []string) error              { return nil }
func (nopDocker) RemoveNetworksByLabel(context.Context, string) error           { return nil }
func (nopDocker) RemoveVolumesByLabel(context.Context, string) error            { return nil }
func (nopDocker) DataRootUsage(context.Context) (int, error)                    { return 10, nil }
func (nopDocker) PruneBuildCacheOlderThan(context.Context, int) (string, error) { return "0B", nil }
func (nopDocker) PruneBuildCacheTo(context.Context, string) (string, error)     { return "0B", nil }
func (nopDocker) PruneDanglingImages(context.Context) (string, error)           { return "0B", nil }

type dirHost struct{}

func (dirHost) CopyTree(_ context.Context, _, dst string) error { return os.MkdirAll(dst, 0o755) }
func (dirHost) ChownR(context.Context, string, string) error    { return nil }

// fakeGitHub serves one queued job for darkmem and accepts JIT registrations.
func fakeGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(p, "/generate-jitconfig"):
			w.WriteHeader(201)
			fmt.Fprint(w, `{"runner":{"id":5,"name":"x"},"encoded_jit_config":"E"}`)
		case strings.HasSuffix(p, "/darkmem/actions/runs") && r.URL.Query().Get("status") == "queued":
			fmt.Fprint(w, `{"workflow_runs":[{"id":1,"status":"queued"}]}`)
		case strings.HasSuffix(p, "/actions/runs"):
			fmt.Fprint(w, `{"workflow_runs":[]}`)
		case strings.HasSuffix(p, "/actions/runs/1/jobs"):
			fmt.Fprint(w, `{"jobs":[{"id":11,"status":"queued","labels":["self-hosted","homelab"],"created_at":"2026-10-03T12:00:00Z"}]}`)
		case strings.HasSuffix(p, "/actions/runners/5"):
			fmt.Fprint(w, `{"id":5,"status":"offline","busy":false}`)
		case strings.HasSuffix(p, "/actions/runners"):
			fmt.Fprint(w, `{"runners":[]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestRunServesTicksReloadsAndKeepsRunners(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	cfg := strings.Replace(cfgYAML, "global_max: 2", "global_max: 2\npoll_interval: 100ms", 1)
	os.WriteFile(cfgPath, []byte(cfg), 0o600)
	os.WriteFile(filepath.Join(dir, "token"), []byte("tok\n"), 0o600)
	dist := filepath.Join(dir, "dist", "2.330.0")
	os.MkdirAll(dist, 0o755)
	sd := &recSD{}
	reload := make(chan os.Signal, 1)
	o := Options{
		ConfigPath: cfgPath, TokenPath: filepath.Join(dir, "token"),
		Socket: filepath.Join(dir, "ghr.sock"), HistoryPath: filepath.Join(dir, "history.jsonl"),
		ShutdownWait: 5 * time.Second,
		Paths: runner.Paths{Dist: dist, Instances: filepath.Join(dir, "instances"), Logs: filepath.Join(dir, "logs"),
			Pending: filepath.Join(dir, "pending"), ToolCache: filepath.Join(dir, "toolcache"), Hooks: "/opt/ghr/hooks", Home: "/home/ghrunner"},
		GitHubURL: fakeGitHub(t).URL, Systemd: sd, Docker: nopDocker{}, Host: dirHost{}, Reload: reload,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, o) }()
	defer func() {
		cancel()
		<-done
	}()
	c := api.NewUnixClient(o.Socket)

	waitFor(t, "the socket to serve status", func() bool { _, err := c.Status(context.Background()); return err == nil })
	waitFor(t, "a tick to spawn darkmem's runner", func() bool { n, _ := sd.counts(); return n == 1 })
	st, _ := c.Status(context.Background())
	if len(st.Instances) != 1 || st.Instances[0].Repo != "darkmem" || st.Epoch == "" {
		t.Fatalf("status %+v", st)
	}

	if err := Run(context.Background(), o); err == nil || !strings.Contains(err.Error(), "already serving") {
		t.Fatalf("second daemon: %v", err)
	}
	if _, err := c.Status(context.Background()); err != nil {
		t.Fatalf("second daemon disturbed the first: %v", err)
	}

	os.WriteFile(cfgPath, []byte(strings.Replace(cfg, "global_max: 2", "global_max: 3", 1)), 0o600)
	reload <- os.Interrupt
	waitFor(t, "the reload", func() bool { st, err := c.Status(context.Background()); return err == nil && st.GlobalMax == 3 })

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	done <- nil // for the deferred receive
	if _, stopped := sd.counts(); stopped != 0 {
		t.Fatalf("shutdown stopped %d runner units; they must keep running", stopped)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./cmd/ghr/ ./internal/daemon/`
Expected: FAIL — build failure `undefined: Options` / `undefined: Run` in `internal/daemon`, and `TestDaemonFailsWithoutConfig` exits 2 (`unknown command daemon`)

- [ ] **Step 4: Write `internal/daemon/run.go`**

```go
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/history"
	"github.com/darkraise/ghr/internal/runner"
	"github.com/darkraise/ghr/internal/system"
)

type Options struct {
	ConfigPath  string
	TokenPath   string
	Socket      string
	HistoryPath string
	Paths       runner.Paths
	// ShutdownWait bounds how long shutdown waits for in-flight cleanups; an
	// unfinished cleanup resumes at the next start.
	ShutdownWait time.Duration

	// Test seams: zero values use GitHub, systemd, Docker, the host and SIGHUP.
	GitHubURL string
	Systemd   runner.Systemd
	Docker    runner.Docker
	Host      runner.Host
	Reload    <-chan os.Signal
}

func DefaultOptions() Options {
	return Options{
		ConfigPath:   "/etc/ghr/config.yaml",
		TokenPath:    "/etc/ghr/token",
		Socket:       api.DefaultSocket,
		HistoryPath:  "/var/lib/ghr/history.jsonl",
		ShutdownWait: 30 * time.Second,
		Paths: runner.Paths{
			Dist:      "/opt/ghr/dist/current",
			Instances: "/var/lib/ghr/instances",
			Logs:      "/var/lib/ghr/logs",
			Pending:   "/var/lib/ghr/pending",
			ToolCache: "/var/lib/ghr/toolcache",
			Hooks:     "/opt/ghr/hooks",
			Home:      "/home/" + runner.RunnerUser,
		},
	}
}

// listen binds the control socket before any runner is touched. The bound
// socket is the single-daemon lock: when another daemon answers on it, Run
// refuses to start instead of removing the live socket; a socket nobody
// answers is a crashed daemon's leftover and is replaced.
func listen(socket string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socket), 0o755); err != nil {
		return nil, err
	}
	if conn, err := net.DialTimeout("unix", socket, time.Second); err == nil {
		conn.Close()
		return nil, fmt.Errorf("another ghr daemon is already serving %s", socket)
	}
	os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	return ln, nil
}

// Run starts the daemon and blocks until ctx is cancelled. Runner units keep running after it exits.
func Run(ctx context.Context, o Options) error {
	store, warnings, err := OpenStore(o.ConfigPath, o.TokenPath)
	if err != nil {
		return err
	}
	ln, err := listen(o.Socket)
	if err != nil {
		return err
	}
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second}
	served := false
	defer func() {
		if !served {
			ln.Close()
		}
	}()

	ev := events.New()
	for _, w := range warnings {
		ev.Add("warn", "", "config: %s", w)
	}
	owner := store.Config().Owner
	gh := github.New(owner, store.Token)
	if o.GitHubURL != "" {
		gh.BaseURL = o.GitHubURL
	}
	hist := &history.Store{Path: o.HistoryPath}
	m := &runner.Manager{
		Config: store.Config, GH: gh, SD: o.Systemd, Docker: o.Docker, Host: o.Host,
		Paths: o.Paths, Events: ev, History: hist, Now: time.Now, NewID: runner.RandomID,
	}
	if m.SD == nil {
		m.SD = system.Systemd{Run: system.Exec}
	}
	if m.Docker == nil {
		m.Docker = system.Docker{Run: system.Exec}
	}
	if m.Host == nil {
		m.Host = system.Host{Run: system.Exec}
	}
	if err := m.Init(); err != nil {
		return err
	}
	if err := m.Adopt(ctx); err != nil {
		return err
	}
	m.Reconcile(ctx, store.Config())

	wake := make(chan struct{}, 1)
	b := &Backend{
		Store: store, M: m, GH: gh, Events: ev, Hist: hist,
		CheckToken: func(ctx context.Context, token, repo string) error {
			c := github.New(owner, func() string { return token })
			c.BaseURL = gh.BaseURL
			_, err := c.GetRepo(ctx, repo)
			return err
		},
		Wake: func() {
			select {
			case wake <- struct{}{}:
			default:
			}
		},
	}
	srv.Handler = api.NewServer(b)
	served = true
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("api server: %v", err)
		}
	}()
	ev.Add("info", "", "ghr daemon started (owner %s, mode %s)", owner, store.Config().Mode)

	reload := o.Reload
	if reload == nil {
		hup := make(chan os.Signal, 1)
		signal.Notify(hup, syscall.SIGHUP)
		defer signal.Stop(hup)
		reload = hup
	}
	timer := time.NewTimer(0)
	defer timer.Stop()
	tick := func() {
		m.Tick(ctx)
		b.FinalizeRemovals()
		timer.Reset(store.Config().PollInterval.D())
	}
	for {
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			srv.Shutdown(shutdownCtx)
			cancel()
			done := make(chan struct{})
			go func() { m.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(o.ShutdownWait):
				log.Printf("shutdown: cleanups still running after %s; they resume at the next start", o.ShutdownWait)
			}
			return nil
		case <-reload:
			warnings, err := store.Reload()
			if err != nil {
				ev.Add("error", "", "reload rejected, keeping previous config: %v", err)
				continue
			}
			for _, w := range warnings {
				ev.Add("warn", "", "config: %s", w)
			}
			gh.ForgetCache()
			m.ClearDegraded()
			ev.Add("info", "", "config and token reloaded")
			tick()
		case <-wake:
			tick()
		case <-timer.C:
			tick()
		}
	}
}
```

- [ ] **Step 5: Edit `cmd/ghr/main.go`**

Add the `daemon` case as the first case of the `switch args[0]` in `run`, directly above `case "version":`:

```go
	case "daemon":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := daemon.Run(ctx, daemon.DefaultOptions()); err != nil {
			fmt.Fprintln(stderr, "ghr daemon:", err)
			return 1
		}
		return 0
```

- [ ] **Step 6: Edit `cmd/ghr/main.go`**

Add `"syscall"` to the standard-library imports (after `"os/signal"`) and `"github.com/darkraise/ghr/internal/daemon"` after the `internal/api` import:

```go
	"os/signal"
	"syscall"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/daemon"
```

- [ ] **Step 7: Run the check**

Run: `go test -count=1 -timeout 180s ./cmd/ghr/ ./internal/daemon/ && go vet ./...`
Expected: PASS (exit 0).

- [ ] **Step 8: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 9: Commit**

```bash
git add cmd/ghr/main_test.go internal/daemon/run_test.go internal/daemon/run.go cmd/ghr/main.go
git commit -m "feat(daemon): run supervisor loop as ghr daemon"
```

---

### Task 21: TUI

Working directory: `D:/Repositories/Personal/ghr`

**Files:**
- Create: `internal/tui/styles.go`
- Create: `internal/tui/model.go`
- Create: `internal/tui/input.go`
- Create: `internal/tui/view.go`
- Test: `internal/tui/tui_test.go`
- Modify: `cmd/ghr/main.go`
- Modify: `go.mod`, `go.sum`
- Create (generated): `internal/tui/testdata/TestDashboardGolden/120.golden`, `80.golden`

**Interfaces:**
- Consumes: the `api.Client` methods named in Contracts → tui, `model` types, `config.Config` (Contracts).
- Produces: `tui.Run(Client) error`, `tui.Client`, the `tui` CLI case (Contracts → tui, CLI entry).

**Items:** 4, 5

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 0 = 3

- [ ] **Step 1: Add dependencies**

Run: `go get github.com/charmbracelet/bubbletea@v1.3.10 github.com/charmbracelet/lipgloss@v1.1.0 github.com/charmbracelet/bubbles@v0.21.1 github.com/lrstanley/bubblezone@v1.0.0 github.com/charmbracelet/x/exp/golden@v0.1.0`
Expected: exit 0; `go.mod` lists the pinned versions. Do not run `go mod tidy` yet: no source imports them, so tidy would drop them.

- [ ] **Step 2: Write the failing test `internal/tui/tui_test.go`**

```go
package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/exp/golden"
	zone "github.com/lrstanley/bubblezone"
	"github.com/muesli/termenv"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	zone.NewGlobal()
	os.Exit(m.Run())
}

var now = time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)

// fakeClient records actions; reads return the configured fields (zero values
// mean the sample status and empty results).
type fakeClient struct {
	calls  []string
	st     *model.Status
	events []model.Event
	steps  []model.Step
	ctrs   []model.Container
	cfg    *config.Config
}

func (f *fakeClient) rec(s string, a ...any) error {
	f.calls = append(f.calls, fmt.Sprintf(s, a...))
	return nil
}
func (f *fakeClient) Status(context.Context) (model.Status, error) {
	if f.st != nil {
		return *f.st, nil
	}
	return sampleStatus(), nil
}
func (f *fakeClient) Events(_ context.Context, after int64) ([]model.Event, error) {
	var out []model.Event
	for _, e := range f.events {
		if e.Seq > after {
			out = append(out, e)
		}
	}
	return out, nil
}

// Log returns one line naming the runner and cursor it was asked for.
func (f *fakeClient) Log(_ context.Context, id, cursor string) (model.LogChunk, error) {
	f.rec("log %s %q", id, cursor)
	return model.LogChunk{Data: fmt.Sprintf("%s after %q\n", id, cursor), Next: cursor + "+"}, nil
}
func (f *fakeClient) Steps(context.Context, string) ([]model.Step, error) { return f.steps, nil }
func (f *fakeClient) Containers(_ context.Context, id string) ([]model.Container, error) {
	f.rec("containers %s", id)
	return f.ctrs, nil
}
func (f *fakeClient) Config(_ context.Context, out any) error {
	if f.cfg != nil {
		*out.(*config.Config) = *f.cfg
	}
	return nil
}
func (f *fakeClient) History(context.Context, string, string, int) ([]model.HistoryEntry, error) {
	return nil, nil
}
func (f *fakeClient) PatchConfig(_ context.Context, p model.ConfigPatch) error {
	switch {
	case p.GlobalMax != nil:
		return f.rec("global_max=%d", *p.GlobalMax)
	case p.Mode != nil:
		return f.rec("mode=%s", *p.Mode)
	}
	for name, rp := range p.Repos {
		if rp.Max != nil {
			return f.rec("%s.max=%d", name, *rp.Max)
		}
		if rp.Labels != nil {
			return f.rec("%s.labels=%s", name, strings.Join(*rp.Labels, ","))
		}
	}
	return f.rec("patch")
}
func (f *fakeClient) AddRepo(_ context.Context, r model.AddRepoRequest) error {
	return f.rec("add %s %s", r.Name, strings.Join(r.Labels, ","))
}
func (f *fakeClient) RemoveRepo(_ context.Context, n string) error { return f.rec("rm %s", n) }
func (f *fakeClient) Pause(_ context.Context, n string) error      { return f.rec("pause %s", n) }
func (f *fakeClient) Resume(_ context.Context, n string) error     { return f.rec("resume %s", n) }
func (f *fakeClient) PauseAll(context.Context) error               { return f.rec("pause-all") }
func (f *fakeClient) ResumeAll(context.Context) error              { return f.rec("resume-all") }
func (f *fakeClient) Kill(_ context.Context, id string) error      { return f.rec("kill %s", id) }

// actions drops the read calls the fake records, leaving the daemon actions.
func (f *fakeClient) actions() []string {
	var out []string
	for _, c := range f.calls {
		if !strings.HasPrefix(c, "log ") && !strings.HasPrefix(c, "containers ") {
			out = append(out, c)
		}
	}
	return out
}

func sampleStatus() model.Status {
	return model.Status{
		Now: now, Epoch: "e1", Mode: "queue", GlobalMax: 3, RateRemaining: 4800, DiskPct: 61,
		Repos: []model.RepoStatus{
			{Name: "darkcloud", Max: 1, Active: 1, Queued: 2,
				LastJob: &model.HistoryEntry{RunNumber: "411", JobName: "lint", Conclusion: "success", FinishedAt: now.Add(-2 * time.Minute)}},
			{Name: "darkmem", Max: 1, Active: 1,
				LastJob: &model.HistoryEntry{RunNumber: "87", JobName: "build / test", Conclusion: "failure", FinishedAt: now.Add(-time.Hour)}},
			{Name: "darkagents", Paused: true, Max: 1, Queued: 1},
		},
		Instances: []model.InstanceStatus{
			{ID: "a3f9c1", Repo: "darkcloud", State: "busy", Since: now.Add(-12 * time.Minute),
				Job: &model.JobInfo{Name: "CI / e2e-journeys", RunNumber: "412", StartedAt: now.Add(-12*time.Minute - 4*time.Second)}},
			{ID: "7be210", Repo: "darkmem", State: "idle", Since: now.Add(-90 * time.Second)},
		},
	}
}

func newModel(c Client, w, h int, st model.Status) Model {
	m := New(c)
	m.now = func() time.Time { return now }
	m.copyFn = func(string) {}
	upd, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	upd, _ = upd.Update(statusMsg{st: st})
	return upd.(Model)
}

func sampleModel(c Client, w, h int) Model {
	upd, _ := newModel(c, w, h, sampleStatus()).Update(eventsMsg{"e1", []model.Event{
		{Seq: 1, Time: now.Add(-3 * time.Minute).Local(), Level: "ok", Repo: "darkcloud", Msg: "#411 lint success 2m10s  cleanup: 3 ctrs"},
		{Seq: 2, Time: now.Add(-2 * time.Minute).Local(), Level: "info", Repo: "darkcloud", Msg: "spawned a3f9c1 (2.330.0)"},
		{Seq: 3, Time: now.Add(-time.Minute).Local(), Level: "warn", Msg: "disk 81% > high-water 80% — pruned build cache"},
	}})
	return upd.(Model)
}

// stripTimes removes the local clock column so goldens do not depend on the test machine's zone.
func stripTimes(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if i := strings.Index(l, ":0"); i >= 2 && strings.Count(l, ":") >= 2 && strings.Contains(l, "  ") {
			l = l[:i-2] + "HH:MM:SS" + l[i+6:]
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func TestDashboardGolden(t *testing.T) {
	for _, w := range []int{120, 80} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			v := sampleModel(&fakeClient{}, w, 30).View()
			golden.RequireEqual(t, []byte(stripTimes(v)))
		})
	}
}

func TestDashboardContent(t *testing.T) {
	v := sampleModel(&fakeClient{}, 120, 30).View()
	for _, want := range []string{"mode ● QUEUE", "global", "2/3", "darkcloud", "⧗ 2", "#411 lint", "2m ago",
		"◌ paused", "a3f9c1", "CI / e2e-journeys  #412", "12m04s", "2 jobs queued (repo cap 1)", "disk 81%", "x kill"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q", want)
		}
	}
	narrow := sampleModel(&fakeClient{}, 72, 30).View()
	if strings.Contains(narrow, "LAST JOB") {
		t.Error("LAST JOB column should be hidden below 80 columns")
	}
}

func TestRemovingRepoState(t *testing.T) {
	st := sampleStatus()
	st.Repos[2].Removing = true
	if v := newModel(&fakeClient{}, 120, 30, st).View(); !strings.Contains(v, "◌ removing") {
		t.Fatal("removing state not shown")
	}
}

func key(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

// run feeds keys and executes resulting commands (one level, ignoring follow-up fetches).
func run(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	var cur tea.Model = m
	for _, k := range keys {
		var cmd tea.Cmd
		cur, cmd = cur.Update(key(k))
		if cmd != nil {
			if done, ok := cmd().(doneMsg); ok {
				cur, _ = cur.Update(done)
			}
		}
	}
	return cur.(Model)
}

// collect runs cmd and returns the messages it produces, expanding batches.
// A command still running after a short wait (the 1 s tick) is dropped.
func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		if batch, ok := msg.(tea.BatchMsg); ok {
			var out []tea.Msg
			for _, c := range batch {
				out = append(out, collect(c)...)
			}
			return out
		}
		if msg == nil {
			return nil
		}
		return []tea.Msg{msg}
	case <-time.After(100 * time.Millisecond):
		return nil
	}
}

// feed applies msg and, recursively, every message its commands produce.
func feed(m Model, msgs ...tea.Msg) Model {
	for _, msg := range msgs {
		upd, cmd := m.Update(msg)
		m = feed(upd.(Model), collect(cmd)...)
	}
	return m
}

func keys(ks ...string) []tea.Msg {
	var out []tea.Msg
	for _, k := range ks {
		out = append(out, key(k))
	}
	return out
}

func ticks(m Model, n int) Model {
	for i := 0; i < n; i++ {
		m = feed(m, tickMsg(now))
	}
	return m
}

func waitZone(t *testing.T, id string) *zone.ZoneInfo {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if z := zone.Get(id); !z.IsZero() {
			return z
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("zone %s never registered", id)
	return nil
}

func leftClick(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft}
}

// click renders m, waits for zone id and feeds a left click on it.
func click(t *testing.T, m Model, id string) Model {
	t.Helper()
	zone.Clear(id)
	m.View()
	z := waitZone(t, id)
	return feed(m, leftClick(z.StartX+1, z.StartY))
}

func TestKeyActions(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m = run(t, m, "p", "+", "]", "m")
	m = run(t, m, "down", "down", "p") // darkagents is paused → resume
	want := []string{"pause darkcloud", "darkcloud.max=2", "global_max=4", "mode=all", "resume darkagents"}
	if strings.Join(c.calls, "|") != strings.Join(want, "|") {
		t.Fatalf("calls %v", c.calls)
	}
	if !strings.Contains(m.View(), "resumed darkagents") {
		t.Fatal("flash line missing")
	}
}

func TestKillBusyRunnerNeedsConfirm(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m.focus = paneRunners
	m = run(t, m, "x")
	if m.overlay != ovConfirm || len(c.calls) != 0 {
		t.Fatalf("overlay %v calls %v", m.overlay, c.calls)
	}
	m = run(t, m, "y")
	if len(c.calls) != 1 || c.calls[0] != "kill a3f9c1" {
		t.Fatalf("calls %v", c.calls)
	}
	m = run(t, m, "down", "x") // idle runner: no confirmation
	if c.calls[1] != "kill 7be210" {
		t.Fatalf("calls %v", c.calls)
	}
}

func TestAddRepoPrompt(t *testing.T) {
	c := &fakeClient{}
	m := run(t, sampleModel(c, 120, 30), "a")
	if m.overlay != ovPrompt {
		t.Fatal("prompt not open")
	}
	for _, r := range "newrepo homelab,gpu" {
		m = run(t, m, string(r))
	}
	m = run(t, m, "enter")
	if len(c.calls) != 1 || c.calls[0] != "add newrepo homelab,gpu" {
		t.Fatalf("calls %v", c.calls)
	}
}

func sampleConfig(t *testing.T, repos ...string) *config.Config {
	t.Helper()
	y := "owner: o\nlabels: [homelab]\nrepos:\n"
	for _, r := range repos {
		y += "  - name: " + r + "\n"
	}
	cfg, _, err := config.Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestConfigTabEdit(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	upd, _ := m.Update(configMsg(sampleConfig(t, "darkcloud")))
	m = run(t, upd.(Model), "4")
	if !strings.Contains(m.View(), "darkcloud.cleanup_name_prefixes") {
		t.Fatal("config fields not rendered")
	}
	m = run(t, m, "down", "down", "down", "down", "down", "down", "enter") // darkcloud.labels
	if m.overlay != ovPrompt || !strings.Contains(m.promptLabel, "darkcloud.labels") {
		t.Fatalf("prompt %q", m.promptLabel)
	}
	m.prompt.SetValue("a, b")
	run(t, m, "enter")
	if len(c.calls) != 1 || c.calls[0] != "darkcloud.labels=a,b" {
		t.Fatalf("calls %v", c.calls)
	}
}

func TestMouseClickSelectsRunnerAndDoubleClickOpensDetail(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m.View()
	z := waitZone(t, "runner-1")
	click := leftClick(z.StartX+2, z.StartY)
	upd, _ := m.Update(click)
	m = upd.(Model)
	if m.runnerSel != 1 || m.focus != paneRunners {
		t.Fatalf("sel %d focus %v", m.runnerSel, m.focus)
	}
	upd, _ = m.Update(click)
	if upd.(Model).overlay != ovDetail {
		t.Fatal("double click did not open detail view")
	}
}

func TestDetailShowsContainers(t *testing.T) {
	c := &fakeClient{ctrs: []model.Container{
		{ID: "c1", Name: "darkcloud-db-1", Image: "postgres:17", State: "running", Project: "ghr-a3f9c1"},
		{ID: "c2", Name: "darkcloud-web-1", Image: "nginx:1.27", State: "exited", Project: "ghr-a3f9c1-e2e"},
	}}
	m := sampleModel(c, 120, 30)
	m.focus = paneRunners
	m = feed(m, key("enter"))
	if m.overlay != ovDetail || strings.Join(c.calls, "|") != "containers a3f9c1" {
		t.Fatalf("overlay %v calls %v", m.overlay, c.calls)
	}
	v := m.View()
	for _, want := range []string{"Containers", "running", "darkcloud-db-1", "postgres:17", "ghr-a3f9c1",
		"exited", "darkcloud-web-1", "nginx:1.27", "ghr-a3f9c1-e2e"} {
		if !strings.Contains(v, want) {
			t.Errorf("detail view missing %q", want)
		}
	}
}

func TestDetailStepsRefreshOnTicks(t *testing.T) {
	c := &fakeClient{steps: []model.Step{{Number: 1, Name: "Set up job", Status: "queued"}}}
	m := sampleModel(c, 120, 30)
	m.focus = paneRunners
	m = feed(m, key("enter"))
	if v := m.View(); !strings.Contains(v, "○ Set up job") {
		t.Fatalf("queued step not shown:\n%s", v)
	}
	c.steps = []model.Step{{Number: 1, Name: "Set up job", Status: "in_progress"}}
	c.ctrs = []model.Container{{Name: "darkcloud-db-1", State: "running"}}
	m = ticks(m, slowPoll)
	v := m.View()
	if !strings.Contains(v, spinnerFrames[m.frame%len(spinnerFrames)]+" Set up job") {
		t.Fatalf("running step not refreshed:\n%s", v)
	}
	if !strings.Contains(v, "darkcloud-db-1") {
		t.Fatal("containers not refreshed")
	}
	c.steps = []model.Step{{Number: 1, Name: "Set up job", Status: "completed", Conclusion: "success"}}
	m = ticks(m, slowPoll)
	if v := m.View(); !strings.Contains(v, "✔ Set up job") {
		t.Fatalf("completed step not refreshed:\n%s", v)
	}
}

func TestStaleAsyncResponsesAreDropped(t *testing.T) {
	c := &fakeClient{steps: []model.Step{{Name: "current step", Status: "queued"}}}
	m := sampleModel(c, 120, 30)
	m.focus = paneRunners
	m = feed(m, keys("down", "enter")...) // detail for 7be210
	m = feed(m, stepsMsg{id: "a3f9c1", steps: []model.Step{{Name: "stale step"}}})
	if v := m.View(); strings.Contains(v, "stale step") || !strings.Contains(v, "current step") {
		t.Fatalf("stale steps response applied:\n%s", v)
	}
	m = feed(m, key("esc"))

	m = feed(m, keys("2", "up")...) // follow a3f9c1
	if m.logText != "a3f9c1 after \"\"\n" || m.logCursor != "+" {
		t.Fatalf("log %q cursor %q", m.logText, m.logCursor)
	}
	m = feed(m, logMsg{gen: m.logGen, cursor: "", chunk: model.LogChunk{Data: "duplicate\n", Next: "+"}})
	if strings.Contains(m.logText, "duplicate") || m.logCursor != "+" {
		t.Fatalf("response for an old cursor appended: %q", m.logText)
	}

	oldGen := m.logGen
	upd, _ := m.Update(key("down")) // follow 7be210; its first request is left in flight
	m = upd.(Model)
	m = feed(m, logMsg{gen: oldGen, cursor: "", chunk: model.LogChunk{Data: "late a3f9c1\n", Next: "+"}})
	if m.logText != "" || !m.logBusy {
		t.Fatalf("response for the previous runner applied: %q busy %v", m.logText, m.logBusy)
	}
	before := len(c.calls)
	m = ticks(m, 2)
	if len(c.calls) != before {
		t.Fatalf("log polled while a request was in flight: %v", c.calls[before:])
	}

	m = feed(m, key("3"))
	m = feed(m, key("r")) // filter repo darkcloud
	m = feed(m, historyMsg{hist: []model.HistoryEntry{{Repo: "unfiltered", JobName: "stale"}}})
	if len(m.hist) != 0 {
		t.Fatalf("history response for the old filter applied: %v", m.hist)
	}
}

func TestRunnersTabFollowsSelection(t *testing.T) {
	c := &fakeClient{}
	m := feed(sampleModel(c, 120, 30), key("2"))
	if v := m.View(); m.logID != "a3f9c1" || !strings.Contains(v, `a3f9c1 after ""`) {
		t.Fatalf("entering Runners did not follow the selection: %q\n%s", m.logID, v)
	}
	m = feed(m, key("down"))
	if v := m.View(); m.logID != "7be210" || !strings.Contains(v, `7be210 after ""`) || strings.Contains(v, "a3f9c1 after") {
		t.Fatalf("keyboard selection did not switch the log: %q\n%s", m.logID, v)
	}
	m = click(t, m, "runner-0")
	if v := m.View(); m.logID != "a3f9c1" || !strings.Contains(v, `a3f9c1 after ""`) || strings.Contains(v, "7be210 after") {
		t.Fatalf("mouse selection did not switch the log: %q\n%s", m.logID, v)
	}
	m = ticks(m, 1)
	if !strings.Contains(m.logText, `a3f9c1 after "+"`) {
		t.Fatalf("followed log not polled from its cursor: %q", m.logText)
	}
	m = feed(m, keys("1", "l")...)
	if m.tab != tabRunners || m.logID != "a3f9c1" {
		t.Fatalf("l: tab %v log %q", m.tab, m.logID)
	}
}

func TestEventsResetOnDaemonRestart(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m = feed(m, eventsMsg{"e1", []model.Event{{Seq: 900, Time: now, Level: "info", Msg: "old daemon event"}}})
	m = feed(m, statusMsg{err: errors.New("connection refused")})
	if !strings.Contains(m.View(), "daemon unreachable") {
		t.Fatal("unreachable banner missing")
	}
	st := sampleStatus()
	st.Epoch = "e2"
	c.st = &st
	c.events = []model.Event{
		{Seq: 1, Time: now, Level: "info", Msg: "new daemon started"},
		{Seq: 2, Time: now, Level: "ok", Repo: "darkcloud", Msg: "adopted a3f9c1"},
	}
	m = ticks(m, 1)
	v := m.View()
	for _, want := range []string{"new daemon started", "adopted a3f9c1"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing new-epoch event %q", want)
		}
	}
	if strings.Contains(v, "old daemon event") || m.lastSeq != 2 {
		t.Fatalf("events of the previous daemon kept (lastSeq %d)", m.lastSeq)
	}
}

func TestShortTerminalKeepsSelectionVisible(t *testing.T) {
	const h = 22
	st := sampleStatus()
	st.Repos, st.Instances = nil, nil
	var repos []string
	for i := 0; i < 12; i++ {
		st.Repos = append(st.Repos, model.RepoStatus{Name: fmt.Sprintf("repo%02d", i), Max: 1})
		repos = append(repos, fmt.Sprintf("repo%02d", i))
	}
	for i := 0; i < 10; i++ {
		st.Instances = append(st.Instances, model.InstanceStatus{ID: fmt.Sprintf("run%02d", i), Repo: "repo00", State: "idle", Since: now})
	}
	check := func(t *testing.T, m Model, want string) {
		t.Helper()
		v := m.View()
		if !strings.Contains(v, want) || !strings.Contains(v, "q quit") || lipgloss.Height(v) > h {
			t.Fatalf("want %q and the footer within %d lines, got %d:\n%s", want, h, lipgloss.Height(v), v)
		}
	}
	down := func(first string, n int) []string {
		out := []string{first}
		for i := 0; i < n; i++ {
			out = append(out, "down")
		}
		return out
	}
	t.Run("dashboard", func(t *testing.T) {
		m := run(t, newModel(&fakeClient{}, 120, h, st), down("1", 11)...)
		check(t, m, "▸ repo11")
		check(t, run(t, m, down("right", 9)...), "▸ run09")
	})
	t.Run("runners", func(t *testing.T) {
		check(t, run(t, newModel(&fakeClient{}, 120, h, st), down("2", 9)...), "▸ run09")
	})
	t.Run("config", func(t *testing.T) {
		upd, _ := newModel(&fakeClient{}, 120, h, st).Update(configMsg(sampleConfig(t, repos[:6]...)))
		check(t, run(t, upd.(Model), down("4", 27)...), "repo05.cleanup_name_prefixes")
	})
}

func TestMouseOverlayButtons(t *testing.T) {
	c := &fakeClient{}
	m := sampleModel(c, 120, 30)
	m.focus = paneRunners
	m = run(t, m, "x")
	m.View()
	m = feed(m, leftClick(0, 0))
	if m.overlay != ovConfirm {
		t.Fatal("a click outside the buttons closed the confirmation")
	}
	m = click(t, m, "btn-ok")
	if m.overlay != ovNone || strings.Join(c.actions(), "|") != "kill a3f9c1" {
		t.Fatalf("overlay %v calls %v", m.overlay, c.actions())
	}

	c.cfg = sampleConfig(t, "darkcloud")
	m = feed(m, key("4"))
	m = click(t, m, "cfg-1") // global_max
	if m.overlay != ovPrompt || !strings.Contains(m.promptLabel, "global_max") {
		t.Fatalf("overlay %v prompt %q", m.overlay, m.promptLabel)
	}
	m.prompt.SetValue("5")
	m = click(t, m, "btn-ok")
	if m.overlay != ovNone || strings.Join(c.actions(), "|") != "kill a3f9c1|global_max=5" {
		t.Fatalf("overlay %v calls %v", m.overlay, c.actions())
	}
	m = click(t, m, "cfg-1")
	m = click(t, m, "btn-cancel")
	if m.overlay != ovNone || len(c.actions()) != 2 {
		t.Fatalf("cancel: overlay %v calls %v", m.overlay, c.actions())
	}
}

func TestHelpMentionsMouseNotes(t *testing.T) {
	v := run(t, sampleModel(&fakeClient{}, 120, 30), "?").View()
	for _, want := range []string{"Shift-drag", "Option-drag in iTerm2", "set -g mouse on"} {
		if !strings.Contains(v, want) {
			t.Errorf("help missing %q", want)
		}
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test -count=1 -timeout 180s ./internal/tui/ ./cmd/ghr/`
Expected: FAIL — FAIL: `undefined: New` / `undefined: Model`

- [ ] **Step 4: Write `internal/tui/styles.go`**

```go
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	colGreen  = lipgloss.AdaptiveColor{Light: "#1a7f37", Dark: "#3fb950"}
	colAmber  = lipgloss.AdaptiveColor{Light: "#9a6700", Dark: "#d29922"}
	colRed    = lipgloss.AdaptiveColor{Light: "#cf222e", Dark: "#f85149"}
	colDim    = lipgloss.AdaptiveColor{Light: "#6e7781", Dark: "#7d8590"}
	colAccent = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#58a6ff"}
	colSelBg  = lipgloss.AdaptiveColor{Light: "#ddf4ff", Dark: "#1f2a37"}

	sGreen  = lipgloss.NewStyle().Foreground(colGreen)
	sAmber  = lipgloss.NewStyle().Foreground(colAmber)
	sRed    = lipgloss.NewStyle().Foreground(colRed)
	sDim    = lipgloss.NewStyle().Foreground(colDim)
	sBold   = lipgloss.NewStyle().Bold(true)
	sAccent = lipgloss.NewStyle().Foreground(colAccent).Bold(true)
	sSel    = lipgloss.NewStyle().Background(colSelBg).Bold(true)
	sBanner = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(colRed).Bold(true).Padding(0, 1)
	sDialog = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colAccent).Padding(1, 2)
)

// cell truncates s to w columns and pads it to exactly w.
func cell(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	if pad := w - ansi.StringWidth(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// box draws a rounded box of total width w with title in the top border.
func box(title string, w int, lines []string) string {
	if w < 8 {
		w = 8
	}
	dashes := w - 5 - ansi.StringWidth(title)
	if dashes < 1 {
		title = ansi.Truncate(title, w-6, "…")
		dashes = w - 5 - ansi.StringWidth(title)
	}
	var b strings.Builder
	b.WriteString(sDim.Render("╭─ ") + sBold.Render(title) + sDim.Render(" "+strings.Repeat("─", dashes)+"╮"))
	for _, l := range lines {
		b.WriteString("\n" + sDim.Render("│") + " " + cell(l, w-4) + " " + sDim.Render("│"))
	}
	b.WriteString("\n" + sDim.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return b.String()
}

func stateStyle(state string) lipgloss.Style {
	switch state {
	case "busy", "active", "success", "running":
		return sGreen
	case "starting", "idle", "paused", "removing", "queued", "waiting", "cancelled", "warn":
		return sAmber
	case "failure", "error", "cleaning":
		return sRed
	}
	return sDim
}

func eventStyle(level string) (string, lipgloss.Style) {
	switch level {
	case "ok":
		return "✔", sGreen
	case "warn":
		return "⚠", sAmber
	case "error":
		return "✖", sRed
	}
	return "▶", sAccent
}

var spinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

func gauge(used, total, width int) string {
	if total <= 0 || width <= 0 {
		return ""
	}
	filled := used * width / total
	if filled > width {
		filled = width
	}
	return "▕" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "▏"
}
```

- [ ] **Step 5: Write `internal/tui/model.go`**

```go
// Package tui is the interactive ghr dashboard (Bubble Tea, mouse via bubblezone).
package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

// Client is the subset of *api.Client the TUI uses.
type Client interface {
	Status(ctx context.Context) (model.Status, error)
	Events(ctx context.Context, after int64) ([]model.Event, error)
	History(ctx context.Context, repo, conclusion string, limit int) ([]model.HistoryEntry, error)
	Log(ctx context.Context, id, cursor string) (model.LogChunk, error)
	Steps(ctx context.Context, id string) ([]model.Step, error)
	Containers(ctx context.Context, id string) ([]model.Container, error)
	Config(ctx context.Context, out any) error
	PatchConfig(ctx context.Context, p model.ConfigPatch) error
	AddRepo(ctx context.Context, req model.AddRepoRequest) error
	RemoveRepo(ctx context.Context, name string) error
	Pause(ctx context.Context, name string) error
	Resume(ctx context.Context, name string) error
	PauseAll(ctx context.Context) error
	ResumeAll(ctx context.Context) error
	Kill(ctx context.Context, id string) error
}

type tab int

const (
	tabDashboard tab = iota
	tabRunners
	tabHistory
	tabConfig
)

var tabNames = []string{"Dashboard", "Runners", "History", "Config"}

type pane int

const (
	paneRepos pane = iota
	paneRunners
)

type overlay int

const (
	ovNone overlay = iota
	ovConfirm
	ovPrompt
	ovDetail
	ovHelp
)

const maxEvents = 200

// slowPoll is the tick interval of polls that are costly on the daemon side:
// steps go to the GitHub jobs API on every request.
const slowPoll = 5

// Async results carry what they were requested for, so a late response for an
// earlier selection, filter, cursor or daemon epoch is dropped.
type (
	tickMsg   time.Time
	statusMsg struct {
		st  model.Status
		err error
	}
	eventsMsg struct {
		epoch string
		ev    []model.Event
	}
	historyMsg struct {
		repo, concl string
		hist        []model.HistoryEntry
	}
	logMsg struct {
		gen    int
		cursor string
		chunk  model.LogChunk
		err    error
	}
	stepsMsg struct {
		id    string
		steps []model.Step
		err   error
	}
	containersMsg struct {
		id   string
		ctrs []model.Container
		err  error
	}
	configMsg *config.Config
	doneMsg   struct {
		text string
		err  error
	}
)

type Model struct {
	c      Client
	now    func() time.Time
	copyFn func(string)

	width, height int
	tab           tab
	focus         pane
	connected     bool
	connErr       string

	st      model.Status
	epoch   string
	events  []model.Event
	lastSeq int64
	hist    []model.HistoryEntry
	cfg     *config.Config

	repoSel, runnerSel, histSel, cfgSel int
	eventScroll, logScroll              int
	histRepo, histConcl                 string

	logID     string
	logText   string
	logCursor string
	logGen    int
	logBusy   bool

	steps             []model.Step
	containers        []model.Container
	stepsErr, ctrsErr string
	detailID          string

	overlay       overlay
	confirmText   string
	confirmAction func() tea.Cmd
	prompt        textinput.Model
	promptLabel   string
	promptSubmit  func(string) tea.Cmd

	flash     string
	flashErr  bool
	frame     int
	lastClick string
	lastAt    time.Time
}

func New(c Client) Model {
	ti := textinput.New()
	ti.CharLimit = 200
	ti.Cursor.SetMode(cursor.CursorStatic)
	return Model{
		c: c, now: time.Now, width: 120, height: 40, prompt: ti,
		copyFn: func(s string) {
			fmt.Fprintf(os.Stdout, "\x1b]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(s)))
		},
	}
}

// Run starts the full-screen TUI with mouse support.
func Run(c Client) error {
	zone.NewGlobal()
	_, err := tea.NewProgram(New(c), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetchStatus(), m.fetchEvents(), m.fetchConfig(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

func (m Model) fetchStatus() tea.Cmd {
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		st, err := m.c.Status(c)
		return statusMsg{st, err}
	}
}

func (m Model) fetchEvents() tea.Cmd {
	after, epoch := m.lastSeq, m.epoch
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		ev, err := m.c.Events(c, after)
		if err != nil {
			return nil
		}
		return eventsMsg{epoch, ev}
	}
}

func (m Model) fetchHistory() tea.Cmd {
	repo, concl := m.histRepo, m.histConcl
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		h, err := m.c.History(c, repo, concl, 200)
		if err != nil {
			return doneMsg{err: err}
		}
		return historyMsg{repo, concl, h}
	}
}

// fetchLog requests the followed log after the current cursor. Only one request
// is in flight: overlapping polls would carry the same cursor.
func (m *Model) fetchLog() tea.Cmd {
	if m.logID == "" || m.logBusy {
		return nil
	}
	m.logBusy = true
	cl, id, cur, gen := m.c, m.logID, m.logCursor, m.logGen
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		chunk, err := cl.Log(c, id, cur)
		return logMsg{gen, cur, chunk, err}
	}
}

// follow points the log pane of the Runners tab at the selected runner,
// starting its log over when that is a different runner.
func (m *Model) follow() tea.Cmd {
	r := m.selectedRunner()
	if m.tab != tabRunners || r == nil || r.ID == m.logID {
		return nil
	}
	m.logID, m.logText, m.logCursor, m.logScroll = r.ID, "", "", 0
	m.logGen++
	m.logBusy = false
	return m.fetchLog()
}

func (m Model) fetchSteps() tea.Cmd {
	id := m.detailID
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		s, err := m.c.Steps(c, id)
		return stepsMsg{id, s, err}
	}
}

func (m Model) fetchContainers() tea.Cmd {
	id := m.detailID
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		cs, err := m.c.Containers(c, id)
		return containersMsg{id, cs, err}
	}
}

func (m Model) fetchConfig() tea.Cmd {
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		var cfg config.Config
		if err := m.c.Config(c, &cfg); err != nil {
			return nil
		}
		return configMsg(&cfg)
	}
}

// action runs fn against the daemon and reports text on success.
func (m Model) action(text string, fn func(c context.Context) error) tea.Cmd {
	return func() tea.Msg {
		c, cancel := ctx()
		defer cancel()
		return doneMsg{text: text, err: fn(c)}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tickMsg:
		m.frame++
		cmds := []tea.Cmd{tick(), m.fetchStatus(), m.fetchEvents()}
		if m.tab == tabRunners {
			cmds = append(cmds, m.fetchLog())
		}
		if m.tab == tabHistory && m.frame%slowPoll == 0 {
			cmds = append(cmds, m.fetchHistory())
		}
		if m.overlay == ovDetail && m.frame%slowPoll == 0 && m.instance(m.detailID) != nil {
			cmds = append(cmds, m.fetchSteps(), m.fetchContainers())
		}
		return m, tea.Batch(cmds...)
	case statusMsg:
		if msg.err != nil {
			m.connected = false
			m.connErr = msg.err.Error()
			return m, nil
		}
		m.connected = true
		m.st = msg.st
		m.clampSelections()
		var cmds []tea.Cmd
		if msg.st.Epoch != m.epoch {
			// The daemon restarted and its event sequence numbers started over.
			m.epoch, m.lastSeq, m.events, m.eventScroll = msg.st.Epoch, 0, nil, 0
			cmds = append(cmds, m.fetchEvents())
		}
		cmds = append(cmds, m.follow())
		return m, tea.Batch(cmds...)
	case eventsMsg:
		if msg.epoch != m.epoch {
			return m, nil
		}
		for _, e := range msg.ev {
			if e.Seq > m.lastSeq {
				m.events = append(m.events, e)
				m.lastSeq = e.Seq
			}
		}
		if len(m.events) > maxEvents {
			m.events = m.events[len(m.events)-maxEvents:]
		}
		return m, nil
	case historyMsg:
		if msg.repo == m.histRepo && msg.concl == m.histConcl {
			m.hist = msg.hist
			m.clampSelections()
		}
		return m, nil
	case logMsg:
		if msg.gen != m.logGen {
			return m, nil
		}
		m.logBusy = false
		if msg.err == nil && msg.cursor == m.logCursor {
			m.logText += msg.chunk.Data
			m.logCursor = msg.chunk.Next
			if len(m.logText) > 256*1024 {
				m.logText = m.logText[len(m.logText)-256*1024:]
			}
		}
		return m, nil
	case stepsMsg:
		if m.overlay == ovDetail && msg.id == m.detailID {
			if msg.err != nil {
				m.stepsErr = msg.err.Error()
			} else {
				m.steps, m.stepsErr = msg.steps, ""
			}
		}
		return m, nil
	case containersMsg:
		if m.overlay == ovDetail && msg.id == m.detailID {
			if msg.err != nil {
				m.ctrsErr = msg.err.Error()
			} else {
				m.containers, m.ctrsErr = msg.ctrs, ""
			}
		}
		return m, nil
	case configMsg:
		m.cfg = msg
		m.clampSelections()
		return m, nil
	case doneMsg:
		if msg.err != nil {
			m.flash, m.flashErr = msg.err.Error(), true
		} else if msg.text != "" {
			m.flash, m.flashErr = msg.text, false
		}
		return m, tea.Batch(m.fetchStatus(), m.fetchEvents(), m.fetchConfig())
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	if m.overlay == ovPrompt {
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(msg)
		return m, cmd
	}
	return m, nil
}

func clamp(v, n int) int {
	if v >= n {
		v = n - 1
	}
	if v < 0 {
		v = 0
	}
	return v
}

func (m *Model) clampSelections() {
	m.repoSel = clamp(m.repoSel, len(m.st.Repos))
	m.runnerSel = clamp(m.runnerSel, len(m.st.Instances))
	m.histSel = clamp(m.histSel, len(m.hist))
	m.cfgSel = clamp(m.cfgSel, len(m.configFields()))
}

func (m Model) selectedRepo() *model.RepoStatus {
	if m.repoSel < len(m.st.Repos) {
		return &m.st.Repos[m.repoSel]
	}
	return nil
}

func (m Model) selectedRunner() *model.InstanceStatus {
	if m.runnerSel < len(m.st.Instances) {
		return &m.st.Instances[m.runnerSel]
	}
	return nil
}

func (m Model) instance(id string) *model.InstanceStatus {
	for i := range m.st.Instances {
		if m.st.Instances[i].ID == id {
			return &m.st.Instances[i]
		}
	}
	return nil
}

// configField is one editable row of the Config tab.
type configField struct {
	label string
	value string
	apply func(v string) (model.ConfigPatch, error)
}
```

- [ ] **Step 6: Write `internal/tui/input.go`**

```go
package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

const doubleClick = 400 * time.Millisecond

func (m Model) handleKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := k.String()
	switch m.overlay {
	case ovPrompt:
		switch key {
		case "esc":
			m.overlay = ovNone
			return m, nil
		case "enter":
			m.overlay = ovNone
			return m, m.promptSubmit(strings.TrimSpace(m.prompt.Value()))
		}
		var cmd tea.Cmd
		m.prompt, cmd = m.prompt.Update(k)
		return m, cmd
	case ovConfirm:
		m.overlay = ovNone
		if key == "y" || key == "enter" {
			return m, m.confirmAction()
		}
		return m, nil
	case ovDetail, ovHelp:
		if key == "esc" || key == "q" || key == "enter" || key == "?" {
			m.overlay = ovNone
		}
		return m, nil
	}
	return m.press(key)
}

// press runs the action bound to key; mouse clicks on footer hints call it too.
func (m Model) press(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "?":
		m.overlay = ovHelp
	case "1", "2", "3", "4":
		return m.switchTab(tab(key[0] - '1'))
	case "tab":
		return m.switchTab((m.tab + 1) % 4)
	case "shift+tab":
		return m.switchTab((m.tab + 3) % 4)
	case "left", "h":
		m.focus = paneRepos
	case "right":
		m.focus = paneRunners
	case "up", "k", "down", "j":
		d := 1
		if key == "up" || key == "k" {
			d = -1
		}
		cmd := m.move(d)
		return m, cmd
	case "pgup":
		m.eventScroll += 5
		m.logScroll += 10
	case "pgdown":
		m.eventScroll = max(0, m.eventScroll-5)
		m.logScroll = max(0, m.logScroll-10)
	case "p":
		return m, m.togglePause()
	case "P":
		return m, m.togglePauseAll()
	case "+", "=", "-":
		return m, m.repoCap(key != "-")
	case "[", "]":
		return m, m.globalCap(key == "]")
	case "m":
		mode := config.ModeAll
		if m.st.Mode == config.ModeAll {
			mode = config.ModeQueue
		}
		return m, m.action("mode "+mode, func(c context.Context) error {
			return m.c.PatchConfig(c, model.ConfigPatch{Mode: &mode})
		})
	case "a":
		return m.openPrompt("Add repo — name [label,label]", "", func(v string) tea.Cmd {
			f := strings.Fields(v)
			if len(f) == 0 {
				return nil
			}
			req := model.AddRepoRequest{Name: f[0]}
			if len(f) > 1 {
				req.Labels = strings.Split(f[1], ",")
			}
			return m.action("added "+f[0], func(c context.Context) error { return m.c.AddRepo(c, req) })
		})
	case "d":
		if r := m.selectedRepo(); r != nil {
			name := r.Name
			return m.openConfirm(fmt.Sprintf("Remove repo %s? Its running jobs finish first.", name), func() tea.Cmd {
				return m.action("removing "+name, func(c context.Context) error { return m.c.RemoveRepo(c, name) })
			})
		}
	case "x":
		if r := m.selectedRunner(); r != nil {
			id, busy := r.ID, r.State == "busy"
			kill := func() tea.Cmd {
				return m.action("stopped "+id, func(c context.Context) error { return m.c.Kill(c, id) })
			}
			if busy {
				return m.openConfirm(fmt.Sprintf("Runner %s is running a job. Stop it?", id), kill)
			}
			return m, kill()
		}
	case "l":
		if m.selectedRunner() != nil {
			m.tab = tabRunners
			cmd := m.follow()
			return m, cmd
		}
	case "r":
		if m.tab == tabHistory {
			m.histRepo = m.cycle(m.histRepo, m.repoNames())
			return m, m.fetchHistory()
		}
	case "c":
		if m.tab == tabHistory {
			m.histConcl = m.cycle(m.histConcl, []string{"success", "failure", "cancelled"})
			return m, m.fetchHistory()
		}
	case "enter":
		return m.enter()
	}
	return m, nil
}

func (m Model) switchTab(t tab) (tea.Model, tea.Cmd) {
	m.tab = t
	switch t {
	case tabRunners:
		cmd := m.follow()
		return m, cmd
	case tabHistory:
		return m, m.fetchHistory()
	case tabConfig:
		return m, m.fetchConfig()
	}
	return m, nil
}

func (m *Model) move(d int) tea.Cmd {
	switch {
	case m.tab == tabHistory:
		m.histSel = clamp(m.histSel+d, len(m.hist))
	case m.tab == tabConfig:
		m.cfgSel = clamp(m.cfgSel+d, len(m.configFields()))
	case m.tab == tabRunners || m.focus == paneRunners:
		m.runnerSel = clamp(m.runnerSel+d, len(m.st.Instances))
		return m.follow()
	default:
		m.repoSel = clamp(m.repoSel+d, len(m.st.Repos))
	}
	return nil
}

func (m Model) repoNames() []string {
	var out []string
	for _, r := range m.st.Repos {
		out = append(out, r.Name)
	}
	return out
}

// cycle steps "" → opts[0] → … → "" (the empty value means no filter).
func (m Model) cycle(cur string, opts []string) string {
	for i, o := range opts {
		if o == cur {
			if i+1 < len(opts) {
				return opts[i+1]
			}
			return ""
		}
	}
	if len(opts) == 0 {
		return ""
	}
	return opts[0]
}

func (m Model) togglePause() tea.Cmd {
	r := m.selectedRepo()
	if r == nil {
		return nil
	}
	name, paused := r.Name, r.Paused
	if paused {
		return m.action("resumed "+name, func(c context.Context) error { return m.c.Resume(c, name) })
	}
	return m.action("paused "+name, func(c context.Context) error { return m.c.Pause(c, name) })
}

func (m Model) togglePauseAll() tea.Cmd {
	all := len(m.st.Repos) > 0
	for _, r := range m.st.Repos {
		all = all && r.Paused
	}
	if all {
		return m.action("resumed all repos", m.c.ResumeAll)
	}
	return m.action("paused all repos (drain)", m.c.PauseAll)
}

func (m Model) repoCap(up bool) tea.Cmd {
	r := m.selectedRepo()
	if r == nil || r.Max == 0 {
		return nil // unlimited: change it in the Config tab
	}
	n := r.Max - 1
	if up {
		n = r.Max + 1
	}
	if n < 1 {
		return nil
	}
	name := r.Name
	return m.action(fmt.Sprintf("%s max %d", name, n), func(c context.Context) error {
		return m.c.PatchConfig(c, model.ConfigPatch{Repos: map[string]model.RepoPatch{name: {Max: &n}}})
	})
}

func (m Model) globalCap(up bool) tea.Cmd {
	n := m.st.GlobalMax - 1
	if up {
		n = m.st.GlobalMax + 1
	}
	if n < 1 {
		return nil
	}
	return m.action(fmt.Sprintf("global max %d", n), func(c context.Context) error {
		return m.c.PatchConfig(c, model.ConfigPatch{GlobalMax: &n})
	})
}

func (m Model) openConfirm(text string, action func() tea.Cmd) (tea.Model, tea.Cmd) {
	m.overlay, m.confirmText, m.confirmAction = ovConfirm, text, action
	return m, nil
}

func (m Model) openPrompt(label, value string, submit func(string) tea.Cmd) (tea.Model, tea.Cmd) {
	m.overlay, m.promptLabel, m.promptSubmit = ovPrompt, label, submit
	m.prompt.SetValue(value)
	m.prompt.CursorEnd()
	// Focus mutates m.prompt; Go leaves unspecified whether `return m, m.prompt.Focus()` copies m first.
	cmd := m.prompt.Focus()
	return m, cmd
}

func (m Model) enter() (tea.Model, tea.Cmd) {
	switch m.tab {
	case tabHistory:
		if m.histSel < len(m.hist) {
			url := m.hist[m.histSel].HTMLURL
			if url == "" {
				m.flash, m.flashErr = "no run URL recorded", true
				return m, nil
			}
			m.copyFn(url)
			m.flash, m.flashErr = "copied "+url, false
		}
		return m, nil
	case tabConfig:
		fields := m.configFields()
		if m.cfgSel < len(fields) {
			f := fields[m.cfgSel]
			return m.openPrompt("Set "+f.label, f.value, func(v string) tea.Cmd {
				p, err := f.apply(v)
				if err != nil {
					return func() tea.Msg { return doneMsg{err: err} }
				}
				return m.action(f.label+" = "+v, func(c context.Context) error { return m.c.PatchConfig(c, p) })
			})
		}
		return m, nil
	}
	if r := m.selectedRunner(); r != nil && (m.tab == tabRunners || m.focus == paneRunners) {
		m.overlay, m.detailID = ovDetail, r.ID
		m.steps, m.containers, m.stepsErr, m.ctrsErr = nil, nil, "", ""
		return m, tea.Batch(m.fetchSteps(), m.fetchContainers())
	}
	return m, nil
}

func intPtr(v string) (*int, error) {
	if v == "" || v == "∞" {
		z := 0
		return &z, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil, fmt.Errorf("not a number: %s", v)
	}
	return &n, nil
}

func listPtr(v string) *[]string {
	out := []string{}
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return &out
}

// configFields lists the editable settings of the Config tab.
func (m Model) configFields() []configField {
	c := m.cfg
	if c == nil {
		return nil
	}
	strPatch := func(set func(p *model.ConfigPatch, v *string)) func(string) (model.ConfigPatch, error) {
		return func(v string) (model.ConfigPatch, error) {
			var p model.ConfigPatch
			set(&p, &v)
			return p, nil
		}
	}
	fields := []configField{
		{"mode", c.Mode, strPatch(func(p *model.ConfigPatch, v *string) { p.Mode = v })},
		{"global_max", strconv.Itoa(c.GlobalMax), func(v string) (model.ConfigPatch, error) {
			n, err := intPtr(v)
			return model.ConfigPatch{GlobalMax: n}, err
		}},
		{"start_timeout", c.StartTimeout.String(), strPatch(func(p *model.ConfigPatch, v *string) { p.StartTimeout = v })},
		{"idle_timeout", c.IdleTimeout.String(), strPatch(func(p *model.ConfigPatch, v *string) { p.IdleTimeout = v })},
	}
	for _, r := range c.Repos {
		name := r.Name
		repoPatch := func(rp model.RepoPatch) model.ConfigPatch {
			return model.ConfigPatch{Repos: map[string]model.RepoPatch{name: rp}}
		}
		maxText := "∞"
		if eff := c.EffectiveMax(r); eff > 0 {
			maxText = strconv.Itoa(eff)
		}
		fields = append(fields,
			configField{name + ".max", maxText, func(v string) (model.ConfigPatch, error) {
				n, err := intPtr(v)
				return repoPatch(model.RepoPatch{Max: n}), err
			}},
			configField{name + ".warm", strconv.Itoa(c.EffectiveWarm(r)), func(v string) (model.ConfigPatch, error) {
				n, err := intPtr(v)
				return repoPatch(model.RepoPatch{Warm: n}), err
			}},
			configField{name + ".labels", strings.Join(r.Labels, ","), func(v string) (model.ConfigPatch, error) {
				return repoPatch(model.RepoPatch{Labels: listPtr(v)}), nil
			}},
			configField{name + ".cleanup_name_prefixes", strings.Join(r.CleanupNamePrefixes, ","), func(v string) (model.ConfigPatch, error) {
				return repoPatch(model.RepoPatch{CleanupNamePrefixes: listPtr(v)}), nil
			}},
		)
	}
	return fields
}

// footerKeys are the clickable hints shown in the footer, in order.
var footerKeys = []struct{ key, label string }{
	{"p", "pause"}, {"+", "repo cap"}, {"-", ""}, {"[", "global cap"}, {"]", ""}, {"m", "mode"},
	{"x", "kill"}, {"l", "logs"}, {"enter", "details"}, {"?", "help"}, {"q", "quit"},
}

// dialogButtons map each overlay button zone to the key it stands for.
var dialogButtons = []struct {
	zone string
	key  tea.KeyType
}{{"btn-ok", tea.KeyEnter}, {"btn-cancel", tea.KeyEsc}}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.overlay != ovNone {
		if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft {
			for _, b := range dialogButtons {
				if zone.Get(b.zone).InBounds(msg) {
					return m.handleKey(tea.KeyMsg{Type: b.key})
				}
			}
		}
		return m, nil
	}
	if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
		up := msg.Button == tea.MouseButtonWheelUp
		switch {
		case zone.Get("events").InBounds(msg):
			if up {
				m.eventScroll++
			} else {
				m.eventScroll = max(0, m.eventScroll-1)
			}
		case zone.Get("log").InBounds(msg):
			if up {
				m.logScroll += 3
			} else {
				m.logScroll = max(0, m.logScroll-3)
			}
		default:
			d := 1
			if up {
				d = -1
			}
			cmd := m.move(d)
			return m, cmd
		}
		return m, nil
	}
	if msg.Action != tea.MouseActionRelease || msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	for i := range tabNames {
		if zone.Get(fmt.Sprintf("tab-%d", i)).InBounds(msg) {
			return m.switchTab(tab(i))
		}
	}
	for _, f := range footerKeys {
		if zone.Get("key-" + f.key).InBounds(msg) {
			return m.press(f.key)
		}
	}
	hit := func(prefix string, n int) int {
		for i := 0; i < n; i++ {
			if zone.Get(fmt.Sprintf("%s-%d", prefix, i)).InBounds(msg) {
				return i
			}
		}
		return -1
	}
	if i := hit("repo", len(m.st.Repos)); i >= 0 {
		m.repoSel, m.focus = i, paneRepos
		return m, nil
	}
	if i := hit("runner", len(m.st.Instances)); i >= 0 {
		m.runnerSel, m.focus = i, paneRunners
		id := fmt.Sprintf("runner-%d", i)
		now := m.now()
		if m.lastClick == id && now.Sub(m.lastAt) < doubleClick {
			m.lastClick = ""
			return m.enter()
		}
		m.lastClick, m.lastAt = id, now
		cmd := m.follow()
		return m, cmd
	}
	if i := hit("hist", len(m.hist)); i >= 0 {
		m.histSel = i
		return m, nil
	}
	if i := hit("cfg", len(m.configFields())); i >= 0 {
		m.cfgSel = i
		return m.enter()
	}
	return m, nil
}
```

- [ ] **Step 7: Write `internal/tui/view.go`**

```go
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"github.com/darkraise/ghr/internal/model"
)

func dur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	h, mi, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, mi)
	}
	return fmt.Sprintf("%dm%02ds", mi, s)
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func maxText(n int) string {
	if n == 0 {
		return "∞"
	}
	return fmt.Sprint(n)
}

func (m Model) View() string {
	w := m.width
	if w < 40 {
		w = 40
	}
	var parts []string
	parts = append(parts, m.header(w))
	if !m.connected {
		parts = append(parts, sBanner.Render("daemon unreachable: "+m.connErr+" — retrying"))
	} else if m.st.Degraded {
		parts = append(parts, sBanner.Render("DEGRADED: "+m.st.DegradedReason+" — no new runners"))
	}
	parts = append(parts, m.tabBar(w))
	used := lipgloss.Height(strings.Join(parts, "\n")) + 2 // footer + flash
	bodyH := m.height - used
	if bodyH < 6 {
		bodyH = 6
	}
	switch m.tab {
	case tabDashboard:
		parts = append(parts, m.dashboard(w, bodyH))
	case tabRunners:
		parts = append(parts, m.runnersTab(w, bodyH))
	case tabHistory:
		parts = append(parts, m.historyTab(w, bodyH))
	case tabConfig:
		parts = append(parts, m.configTab(w, bodyH))
	}
	parts = append(parts, m.flashLine(), m.footer(w))
	out := strings.Join(parts, "\n")
	if m.overlay != ovNone {
		out = m.withOverlay(out, w)
	}
	return zone.Scan(out)
}

func (m Model) header(w int) string {
	running := 0
	for _, i := range m.st.Instances {
		if i.State != "cleaning" {
			running++
		}
	}
	mode := sGreen.Render("● " + strings.ToUpper(m.st.Mode))
	global := fmt.Sprintf("%s %d/%d", gauge(running, m.st.GlobalMax, 3), running, m.st.GlobalMax)
	if m.st.Mode == "all" {
		global = fmt.Sprintf("%d/∞", running)
	}
	api := fmt.Sprintf("%s %d", gauge(max(m.st.RateRemaining, 0), 5000, 10), m.st.RateRemaining)
	diskStyle := sDim
	if m.st.DiskPct >= 80 {
		diskStyle = sAmber
	}
	disk := diskStyle.Render(fmt.Sprintf("%s %d%%", gauge(m.st.DiskPct, 100, 10), m.st.DiskPct))
	line := fmt.Sprintf(" mode %s   global %s   api %s   disk %s", mode, global, api, disk)
	return box("ghr", w, []string{line})
}

func (m Model) tabBar(w int) string {
	var tabs []string
	for i, name := range tabNames {
		label := fmt.Sprintf(" %d %s ", i+1, name)
		if tab(i) == m.tab {
			label = sAccent.Render("[" + label + "]")
		} else {
			label = sDim.Render(" " + label + " ")
		}
		tabs = append(tabs, zone.Mark(fmt.Sprintf("tab-%d", i), label))
	}
	bar := strings.Join(tabs, "")
	help := sDim.Render("? help")
	gap := w - ansi.StringWidth(bar) - ansi.StringWidth(help) - 1
	if gap < 1 {
		gap = 1
	}
	return bar + strings.Repeat(" ", gap) + help
}

func (m Model) row(id string, selected bool, s string, w int) string {
	s = cell(s, w)
	if selected {
		s = sSel.Render(ansi.Strip(s))
	}
	return zone.Mark(id, s)
}

func (m Model) reposLines(w int) []string {
	narrow := w < 80
	inner := w - 4
	hdr := fmt.Sprintf("  %-14s %-10s %-5s %-6s", "REPO", "STATE", "RUN", "QUEUE")
	if !narrow {
		hdr += " LAST JOB"
	}
	lines := []string{sDim.Render(hdr)}
	for i, r := range m.st.Repos {
		state, st := "active", "active"
		dot := "●"
		if r.Paused {
			state, st, dot = "paused", "paused", "◌"
		}
		if r.Removing {
			state, st, dot = "removing", "removing", "◌"
		}
		if r.Error != "" {
			state, st, dot = "error", "error", "✖"
		}
		queue := "–"
		if r.Queued > 0 {
			queue = sAmber.Render(fmt.Sprintf("⧗ %d", r.Queued))
		}
		sel := "  "
		if i == m.repoSel && m.focus == paneRepos {
			sel = "▸ "
		}
		line := sel + cell(r.Name, 14) + " " + stateStyle(st).Render(cell(dot+" "+state, 10)) + " " +
			cell(fmt.Sprintf("%d/%s", r.Active, maxText(r.Max)), 5) + " " + cell(queue, 6)
		if !narrow {
			last := sDim.Render("–")
			if j := r.LastJob; j != nil {
				icon, style := "✔", sGreen
				if j.Conclusion != "success" {
					icon, style = "✖", sRed
				}
				last = style.Render(icon) + fmt.Sprintf(" #%s %s  %s", j.RunNumber, j.JobName, sDim.Render(ago(m.now().Sub(j.FinishedAt))))
			}
			line += " " + last
		}
		if r.Error != "" && !narrow {
			line += "  " + sRed.Render(r.Error)
		}
		lines = append(lines, m.row(fmt.Sprintf("repo-%d", i), i == m.repoSel && m.focus == paneRepos, line, inner))
	}
	if len(m.st.Repos) == 0 {
		lines = append(lines, sDim.Render("  no repos configured — press a to add one"))
	}
	return lines
}

func (m Model) runnerLine(i int, r model.InstanceStatus, selected bool, inner int) string {
	state := r.State
	icon := "○"
	switch state {
	case "busy":
		icon = spinnerFrames[m.frame%len(spinnerFrames)]
	case "starting":
		icon = "◔"
	case "cleaning":
		icon = "♻"
	}
	job, elapsed := sDim.Render("–"), dur(m.now().Sub(r.Since))
	if r.Job != nil {
		job = r.Job.Name
		if r.Job.RunNumber != "" {
			job += "  #" + r.Job.RunNumber
		}
		if !r.Job.StartedAt.IsZero() {
			elapsed = dur(m.now().Sub(r.Job.StartedAt))
		}
	}
	sel := "  "
	if selected {
		sel = "▸ "
	}
	line := sel + cell(r.ID, 8) + " " + cell(r.Repo, 12) + " " + stateStyle(state).Render(cell(icon+" "+state, 11)) + " " +
		cell(job, inner-50) + " " + cell(elapsed, 8)
	return m.row(fmt.Sprintf("runner-%d", i), selected, line, inner)
}

func (m Model) runnersLines(w int) []string {
	inner := w - 4
	lines := []string{sDim.Render(fmt.Sprintf("  %-8s %-12s %-11s %-*s %s", "ID", "REPO", "STATE", inner-50, "JOB", "ELAPSED"))}
	for i, r := range m.st.Instances {
		selected := i == m.runnerSel && (m.focus == paneRunners || m.tab == tabRunners)
		lines = append(lines, m.runnerLine(i, r, selected, inner))
	}
	for _, r := range m.st.Repos {
		if r.Paused || r.Queued == 0 || r.Max == 0 || r.Active < r.Max {
			continue
		}
		lines = append(lines, sAmber.Render(fmt.Sprintf("  %-8s %-12s %-11s %d jobs queued (repo cap %d)", "–", r.Name, "⧗ waiting", r.Queued, r.Max)))
	}
	if len(m.st.Instances) == 0 {
		lines = append(lines, sDim.Render("  no runners — they start when jobs are queued"))
	}
	return lines
}

func (m Model) eventLines(n, w int) []string {
	inner := w - 4
	end := len(m.events) - m.eventScroll
	if end < 0 {
		end = 0
	}
	start := end - n
	if start < 0 {
		start = 0
	}
	var lines []string
	for _, e := range m.events[start:end] {
		icon, style := eventStyle(e.Level)
		lines = append(lines, cell(sDim.Render(e.Time.Local().Format("15:04:05"))+"  "+style.Render(icon)+" "+cell(e.Repo, 11)+" "+e.Msg, inner))
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return lines
}

// fit keeps the first hdr lines and scrolls the rest so that body line sel
// stays visible within n lines.
func fit(lines []string, hdr, sel, n int) []string {
	if len(lines) <= n {
		return lines
	}
	body, vis := lines[hdr:], max(n-hdr, 1)
	start := min(max(sel-vis+1, 0), len(body)-vis)
	return append(lines[:hdr:hdr], body[start:start+vis]...)
}

func (m Model) dashboard(w, h int) string {
	repoLines, runnerLines := m.reposLines(w), m.runnersLines(w)
	room := h - 9 // two table borders plus an Events box of at least 3 lines
	nRepo, nRun := len(repoLines), len(runnerLines)
	if nRepo+nRun > room {
		nRepo = min(nRepo, max(room/2, room-nRun))
		nRun = room - nRepo
	}
	repos := box("Repos", w, fit(repoLines, 1, m.repoSel, nRepo))
	runners := box("Runners", w, fit(runnerLines, 1, m.runnerSel, nRun))
	rest := h - lipgloss.Height(repos) - lipgloss.Height(runners) - 2
	if w < 80 || rest < 3 {
		rest = 3
	}
	events := zone.Mark("events", box("Events", w, m.eventLines(rest, w)))
	return strings.Join([]string{repos, runners, events}, "\n")
}

func (m Model) runnersTab(w, h int) string {
	runners := box("Runners", w, fit(m.runnersLines(w), 1, m.runnerSel, max(h/2-2, 2)))
	logH := h - lipgloss.Height(runners) - 2
	if logH < 3 {
		logH = 3
	}
	title := "Log — no runner selected"
	var lines []string
	if m.logID != "" {
		title = "Log " + m.logID + " (following)"
		all := strings.Split(strings.TrimRight(m.logText, "\n"), "\n")
		end := len(all) - m.logScroll
		if end < 0 {
			end = 0
		}
		start := end - logH
		if start < 0 {
			start = 0
		}
		lines = all[start:end]
	}
	for len(lines) < logH {
		lines = append(lines, "")
	}
	return runners + "\n" + zone.Mark("log", box(title, w, lines))
}

func (m Model) historyTab(w, h int) string {
	inner := w - 4
	filter := "repo: all"
	if m.histRepo != "" {
		filter = "repo: " + m.histRepo
	}
	if m.histConcl != "" {
		filter += "  result: " + m.histConcl
	} else {
		filter += "  result: all"
	}
	lines := []string{
		sDim.Render(filter + "   (r repo, c result, enter copies run URL)"),
		sDim.Render(fmt.Sprintf("  %-16s %-12s %-7s %-*s %-10s %s", "FINISHED", "REPO", "RUN", inner-62, "JOB", "RESULT", "DURATION")),
	}
	visible := h - 4
	start := 0
	if m.histSel >= visible {
		start = m.histSel - visible + 1
	}
	for i := start; i < len(m.hist) && i < start+visible; i++ {
		e := m.hist[i]
		line := "  " + cell(e.FinishedAt.Local().Format("2006-01-02 15:04"), 16) + " " + cell(e.Repo, 12) + " " +
			cell("#"+e.RunNumber, 7) + " " + cell(e.JobName, inner-62) + " " + stateStyle(e.Conclusion).Render(cell(e.Conclusion, 10)) + " " +
			dur(e.FinishedAt.Sub(e.StartedAt))
		lines = append(lines, m.row(fmt.Sprintf("hist-%d", i), i == m.histSel, line, inner))
	}
	if len(m.hist) == 0 {
		lines = append(lines, sDim.Render("  no finished jobs yet"))
	}
	return box("History", w, lines)
}

func (m Model) configTab(w, h int) string {
	inner := w - 4
	fields := m.configFields()
	lines := []string{sDim.Render("enter edits the selected setting; the daemon validates before saving")}
	if fields == nil {
		lines = append(lines, sDim.Render("loading…"))
	}
	for i, f := range fields {
		line := "  " + cell(f.label, 36) + " " + f.value
		lines = append(lines, m.row(fmt.Sprintf("cfg-%d", i), i == m.cfgSel, line, inner))
	}
	return box("Config", w, fit(lines, 1, m.cfgSel, h-2))
}

func (m Model) flashLine() string {
	if m.flash == "" {
		return ""
	}
	if m.flashErr {
		return sRed.Render(" ✖ " + m.flash)
	}
	return sGreen.Render(" ✔ " + m.flash)
}

func (m Model) footer(w int) string {
	var parts []string
	for _, f := range footerKeys {
		text := sAccent.Render(f.key)
		if f.label != "" {
			text += " " + sDim.Render(f.label)
		}
		parts = append(parts, zone.Mark("key-"+f.key, text))
	}
	return " " + ansi.Truncate(strings.Join(parts, "  "), w-1, "…")
}

// buttons renders the clickable dialog buttons: ok runs enter, cancel runs esc.
func buttons(ok, cancel string) string {
	b := func(id, label string) string { return zone.Mark(id, sAccent.Render("[ "+label+" ]")) }
	if ok == "" {
		return b("btn-cancel", cancel)
	}
	return b("btn-ok", ok) + "  " + b("btn-cancel", cancel)
}

func (m Model) withOverlay(base string, w int) string {
	var body string
	switch m.overlay {
	case ovConfirm:
		body = m.confirmText + "\n\n" + buttons("Yes", "No") + "\n" + sDim.Render("y confirm · any other key cancels")
	case ovPrompt:
		body = sBold.Render(m.promptLabel) + "\n\n" + m.prompt.View() + "\n\n" + buttons("Save", "Cancel") + "\n" +
			sDim.Render("enter save · esc cancel")
	case ovHelp:
		body = sBold.Render("Keys") + "\n\n" + strings.Join([]string{
			"1-4 / tab   switch tab            ↑↓ / j k   move selection",
			"h / →       focus repos / runners  p          pause/resume repo",
			"+ / -       repo cap               [ / ]      global cap",
			"m           toggle queue/all       P          pause/resume all",
			"a / d       add / remove repo      x          stop runner",
			"l           follow runner log      enter      details / edit / copy URL",
			"pgup/pgdn   scroll events and log  q          quit",
			"",
			"Mouse: click tabs, rows, footer keys and dialog buttons; double-click a runner; wheel scrolls.",
			"Selecting terminal text needs Shift-drag (Option-drag in iTerm2).",
			"Under tmux, mouse input requires `set -g mouse on`.",
		}, "\n") + "\n\n" + buttons("", "Close")
	case ovDetail:
		body = m.detailBody()
	}
	dialog := sDialog.Render(body)
	return lipgloss.Place(w, lipgloss.Height(base), lipgloss.Center, lipgloss.Center, dialog,
		lipgloss.WithWhitespaceChars(" "))
}

func (m Model) detailBody() string {
	r := m.instance(m.detailID)
	if r == nil {
		return "runner " + m.detailID + " has finished\n\n" + buttons("", "Close")
	}
	lines := []string{sBold.Render(fmt.Sprintf("Runner %s · %s · %s", r.ID, r.Repo, r.State))}
	if r.Job != nil {
		lines = append(lines, fmt.Sprintf("%s  #%s  %s", r.Job.Name, r.Job.RunNumber, r.Job.Workflow))
		if r.Job.HTMLURL != "" {
			lines = append(lines, sDim.Render(r.Job.HTMLURL))
		}
	}
	lines = append(lines, "", sBold.Render("Steps"))
	if m.stepsErr != "" {
		lines = append(lines, sRed.Render(m.stepsErr))
	}
	if len(m.steps) == 0 {
		lines = append(lines, sDim.Render("no steps reported yet"))
	}
	for _, s := range m.steps {
		icon, style := "○", sDim
		switch {
		case s.Status == "in_progress":
			icon, style = spinnerFrames[m.frame%len(spinnerFrames)], sAccent
		case s.Conclusion == "success":
			icon, style = "✔", sGreen
		case s.Conclusion == "failure":
			icon, style = "✖", sRed
		case s.Conclusion == "skipped":
			icon = "–"
		}
		lines = append(lines, style.Render(icon)+" "+s.Name)
	}
	lines = append(lines, "", sBold.Render("Containers"))
	if m.ctrsErr != "" {
		lines = append(lines, sRed.Render(m.ctrsErr))
	}
	if len(m.containers) == 0 {
		lines = append(lines, sDim.Render("none in this runner's compose projects"))
	}
	for _, c := range m.containers {
		lines = append(lines, stateStyle(c.State).Render(cell(c.State, 9))+" "+cell(c.Name, 28)+" "+cell(c.Image, 28)+" "+sDim.Render(c.Project))
	}
	lines = append(lines, "", buttons("", "Close")+"  "+sDim.Render("esc close"))
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 8: Edit `cmd/ghr/main.go`**

Add the `tui` case to the `switch args[0]` in `run`, directly below the `help` case:

```go
	case "tui":
		if err := tui.Run(newClient()); err != nil {
			fmt.Fprintln(stderr, "ghr tui:", err)
			return 1
		}
		return 0
```

- [ ] **Step 9: Edit `cmd/ghr/main.go`**

Add the import below `internal/daemon`:

```go
	"github.com/darkraise/ghr/internal/tui"
```

- [ ] **Step 10: Tidy the module**

Run: `go mod tidy`
Expected: exit 0, and `go.mod` still requires every pinned version from Global Constraints (now as direct requirements).

- [ ] **Step 11: Generate and review the goldens**

Run: `go test -count=1 -timeout 180s ./internal/tui/ -run Golden -update`
Then open `internal/tui/testdata/TestDashboardGolden/120.golden`; it must match this (the event clock column is masked as `HH:MM:SS`):

```text
╭─ ghr ────────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│  mode ● QUEUE   global ▕██░▏ 2/3   api ▕█████████░▏ 4800   disk ▕██████░░░░▏ 61%                                     │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
[ 1 Dashboard ]  2 Runners    3 History    4 Config                                                              ? help
╭─ Repos ──────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│   REPO           STATE      RUN   QUEUE  LAST JOB                                                                    │
│ ▸ darkcloud      ● active   1/1   ⧗ 2    ✔ #411 lint  2m ago                                                         │
│   darkmem        ● active   1/1   –      ✖ #87 build / test  1h ago                                                  │
│   darkagents     ◌ paused   0/1   ⧗ 1    –                                                                           │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
╭─ Runners ────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│   ID       REPO         STATE       JOB                                                                ELAPSED       │
│   a3f9c1   darkcloud    ⣾ busy      CI / e2e-journeys  #412                                            12m04s        │
│   7be210   darkmem      ○ idle      –                                                                  1m30s         │
│   –        darkcloud    ⧗ waiting   2 jobs queued (repo cap 1)                                                       │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯
╭─ Events ─────────────────────────────────────────────────────────────────────────────────────────────────────────────╮
│ HH:MM:SS  ✔ darkcloud   #411 lint success 2m10s  cleanup: 3 ctrs                                                     │
│ HH:MM:SS  ▶ darkcloud   spawned a3f9c1 (2.330.0)                                                                     │
│ HH:MM:SS  ⚠             disk 81% > high-water 80% — pruned build cache                                               │
│                                                                                                                      │
│                                                                                                                      │
│                                                                                                                      │
│                                                                                                                      │
│                                                                                                                      │
│                                                                                                                      │
│                                                                                                                      │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────╯

 p pause  + repo cap  -  [ global cap  ]  m mode  x kill  l logs  enter details  ? help  q quit
```

And `80.golden`:

```text
╭─ ghr ────────────────────────────────────────────────────────────────────────╮
│  mode ● QUEUE   global ▕██░▏ 2/3   api ▕█████████░▏ 4800   disk ▕██████░░░░… │
╰──────────────────────────────────────────────────────────────────────────────╯
[ 1 Dashboard ]  2 Runners    3 History    4 Config                      ? help
╭─ Repos ──────────────────────────────────────────────────────────────────────╮
│   REPO           STATE      RUN   QUEUE  LAST JOB                            │
│ ▸ darkcloud      ● active   1/1   ⧗ 2    ✔ #411 lint  2m ago                 │
│   darkmem        ● active   1/1   –      ✖ #87 build / test  1h ago          │
│   darkagents     ◌ paused   0/1   ⧗ 1    –                                   │
╰──────────────────────────────────────────────────────────────────────────────╯
╭─ Runners ────────────────────────────────────────────────────────────────────╮
│   ID       REPO         STATE       JOB                        ELAPSED       │
│   a3f9c1   darkcloud    ⣾ busy      CI / e2e-journeys  #412    12m04s        │
│   7be210   darkmem      ○ idle      –                          1m30s         │
│   –        darkcloud    ⧗ waiting   2 jobs queued (repo cap 1)               │
╰──────────────────────────────────────────────────────────────────────────────╯
╭─ Events ─────────────────────────────────────────────────────────────────────╮
│ HH:MM:SS  ✔ darkcloud   #411 lint success 2m10s  cleanup: 3 ctrs             │
│ HH:MM:SS  ▶ darkcloud   spawned a3f9c1 (2.330.0)                             │
│ HH:MM:SS  ⚠             disk 81% > high-water 80% — pruned build cache       │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
│                                                                              │
╰──────────────────────────────────────────────────────────────────────────────╯

 p pause  + repo cap  -  [ global cap  ]  m mode  x kill  l logs  enter details…
```

- [ ] **Step 12: Run the check**

Run: `go test -count=1 -timeout 180s ./internal/tui/ ./cmd/ghr/ && go vet ./...`
Expected: PASS (exit 0).

- [ ] **Step 13: Format check**

Run: `gofmt -l .`
Expected: no output.

- [ ] **Step 14: Commit**

```bash
git add internal/tui/tui_test.go internal/tui/styles.go internal/tui/model.go internal/tui/input.go internal/tui/view.go go.mod go.sum cmd/ghr/main.go internal/tui/testdata
git commit -m "feat(tui): add dashboard with keyboard and mouse"
```

---

### Task 22: Homelab setup script

Working directory: `D:/Repositories/Personal/homelab`

**Files:**
- Create: `github-runner/setup.sh`
- Create: `github-runner/tests/setup_test.sh`
- Create: `github-runner/config.example.yaml`
- Create: `github-runner/.gitattributes`

**Interfaces:**
- Consumes: release assets, on-disk paths, config format, hook paths (Contracts).
- Produces: `github-runner/setup.sh`, `github-runner/config.example.yaml`.

**Items:** 1

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write `github-runner/setup.sh`**

```bash
#!/usr/bin/env bash
# Installs or upgrades ghr and native GitHub Actions runners on a Debian 13 LXC.
# Idempotent: re-run to upgrade Docker, the runner and ghr. Never overwrites config or token.
#   GHR_TOKEN=github_pat_... bash setup.sh
# Optional: GHR_VERSION=v0.1.0 RUNNER_VERSION=2.337.0
set -euo pipefail

GHR_REPO="darkraise/ghr"
GHR_VERSION="${GHR_VERSION:-latest}"
RUNNER_VERSION="${RUNNER_VERSION:-latest}"
RUNNER_USER=ghrunner
# The daemon hard-codes these paths; they are variables only so tests can redirect them.
OPT_DIR=/opt/ghr
DIST_DIR="$OPT_DIR/dist"
HOOKS_DIR="$OPT_DIR/hooks"
STATE_DIR=/var/lib/ghr
ETC_DIR=/etc/ghr
READY_TIMEOUT=60
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

log() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

check_host() {
  [ "$(id -u)" -eq 0 ] || die "run as root"
  # shellcheck disable=SC1091
  . /etc/os-release
  { [ "${ID:-}" = debian ] && [ "${VERSION_ID:-}" = 13 ]; } || die "Debian 13 required (found ${PRETTY_NAME:-unknown})"
}

install_docker() {
  log "installing or upgrading Docker Engine and base packages"
  apt-get update -qq
  apt-get install -y -qq ca-certificates curl
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://download.docker.com/linux/debian/gpg -o /etc/apt/keyrings/docker.asc
  chmod a+r /etc/apt/keyrings/docker.asc
  echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] https://download.docker.com/linux/debian ${VERSION_CODENAME} stable" \
    > /etc/apt/sources.list.d/docker.list
  apt-get update -qq
  # Re-runs upgrade installed packages; keep locally edited conffiles (e.g.
  # /etc/containerd/config.toml) instead of stopping at a dpkg prompt.
  DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
    -o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold \
    docker-ce docker-ce-cli containerd.io docker-compose-plugin docker-buildx-plugin \
    git curl jq unzip build-essential ca-certificates
  systemctl enable --now docker >/dev/null
  docker info >/dev/null 2>&1 || die "docker is not working; the LXC needs features: nesting=1,keyctl=1"
}

create_user_and_dirs() {
  id "$RUNNER_USER" >/dev/null 2>&1 || useradd --create-home --shell /bin/bash "$RUNNER_USER"
  usermod -aG docker "$RUNNER_USER"
  install -d -m 0755 "$OPT_DIR" "$DIST_DIR" "$HOOKS_DIR" \
    "$STATE_DIR" "$STATE_DIR/instances" "$STATE_DIR/logs" "$STATE_DIR/pending"
  install -d -m 0700 "$ETC_DIR"
  install -d -m 0755 -o "$RUNNER_USER" -g "$RUNNER_USER" "$STATE_DIR/toolcache"
}

# Downloads the actions-runner tarball for $1 to $2 and verifies its SHA-256.
fetch_runner() {
  local version="$1" out="$2" tgz="actions-runner-linux-x64-$1.tar.gz" want
  curl -fsSL -o "$out" "https://github.com/actions/runner/releases/download/v$version/$tgz"
  want=$(curl -fsSL "https://api.github.com/repos/actions/runner/releases/tags/v$version" | jq -r .body |
    grep -oP '(?<=<!-- BEGIN SHA linux-x64 -->)[0-9a-f]{64}(?=<!-- END SHA linux-x64 -->)') || true
  [ -n "$want" ] || die "no linux-x64 checksum in the v$version release notes"
  echo "$want  $out" | sha256sum -c - >/dev/null || die "actions-runner checksum mismatch"
}

install_runner() {
  local version="$RUNNER_VERSION"
  if [ "$version" = latest ]; then
    version=$(curl -fsSL https://api.github.com/repos/actions/runner/releases/latest | jq -r .tag_name)
    version="${version#v}"
  fi
  local dir="$DIST_DIR/$version" staging="$DIST_DIR/$version.tmp"
  # A dist dir is published only after installdependencies.sh succeeded, so an
  # executable run.sh means the version is complete.
  if [ ! -x "$dir/run.sh" ]; then
    log "installing actions-runner $version"
    rm -rf "$staging"
    mkdir -p "$staging"
    fetch_runner "$version" "$staging/runner.tar.gz"
    tar -xzf "$staging/runner.tar.gz" -C "$staging"
    rm -f "$staging/runner.tar.gz"
    "$staging/bin/installdependencies.sh" >/dev/null || die "actions-runner $version: bin/installdependencies.sh failed"
    mv -T "$staging" "$dir"
  fi
  ln -sfn "$dir" "$DIST_DIR/current.tmp"
  mv -Tf "$DIST_DIR/current.tmp" "$DIST_DIR/current"
}

install_ghr() {
  local base tmp
  if [ "$GHR_VERSION" = latest ]; then
    base="https://github.com/$GHR_REPO/releases/latest/download"
  else
    base="https://github.com/$GHR_REPO/releases/download/$GHR_VERSION"
  fi
  log "installing ghr ($GHR_VERSION)"
  tmp=$(mktemp -d)
  curl -fsSL -o "$tmp/ghr_linux_amd64.tar.gz" "$base/ghr_linux_amd64.tar.gz"
  curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"
  (cd "$tmp" && sha256sum -c checksums.txt >/dev/null) || die "ghr checksum mismatch"
  tar -xzf "$tmp/ghr_linux_amd64.tar.gz" -C "$tmp"
  install -m 0755 "$tmp/ghr/ghr" /usr/local/bin/ghr
  install -m 0755 "$tmp/ghr/job-started.sh" "$tmp/ghr/job-completed.sh" "$HOOKS_DIR/"
  rm -rf "$tmp"
}

install_config() {
  if [ ! -f "$ETC_DIR/config.yaml" ]; then
    install -m 0600 "$SCRIPT_DIR/config.example.yaml" "$ETC_DIR/config.yaml"
    log "wrote $ETC_DIR/config.yaml from config.example.yaml — review owner and repos"
  fi
  if [ ! -f "$ETC_DIR/token" ]; then
    local tok="${GHR_TOKEN:-}"
    if [ -z "$tok" ]; then
      read -rsp "GitHub fine-grained PAT (Administration: read/write, Actions: read): " tok
      echo
    fi
    [ -n "$tok" ] || die "a token is required"
    (umask 077 && printf '%s\n' "$tok" > "$ETC_DIR/token")
  fi
}

install_service() {
  cat > /etc/systemd/system/ghr.service <<'EOF'
[Unit]
Description=ghr GitHub Actions runner manager
After=network-online.target docker.service
Wants=network-online.target
Requires=docker.service

[Service]
ExecStart=/usr/local/bin/ghr daemon
ExecReload=/bin/kill -HUP $MAINPID
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  systemctl enable ghr.service >/dev/null
  systemctl restart ghr.service
}

# Polls `ghr status` until the API answers; adoption and GitHub reconciliation
# run before the daemon serves, so this can take several seconds.
wait_ready() {
  local deadline=$((SECONDS + READY_TIMEOUT)) out state
  log "waiting for ghr to become ready (up to ${READY_TIMEOUT}s)"
  while :; do
    if out=$(timeout 5 ghr status 2>&1); then
      printf '%s\n' "$out"
      return 0
    fi
    # Restart=always keeps a crashing daemon in "activating" (auto-restart), never
    # "failed", so with Type=simple anything but "active" means it exited.
    state=$(systemctl is-active ghr.service || true)
    [ "$state" = active ] || die "ghr service failed (systemd state: $state); see: journalctl -u ghr -n 50"
    [ "$SECONDS" -lt "$deadline" ] ||
      die "ghr service still starting after ${READY_TIMEOUT}s; check: systemctl status ghr; journalctl -u ghr -f"
    sleep 1
  done
}

# Must run only after the restarted daemon is ready: the old daemon may have been
# copying a non-current dist dir into a new instance until it stopped, and the new
# one only reads `current`.
gc_dist() {
  local keep old
  keep=$(readlink -e "$DIST_DIR/current") || return 0
  keep=$(basename "$keep")
  for old in "$DIST_DIR"/*/; do
    old=$(basename "$old")
    if [ "$old" != "$keep" ] && [ "$old" != current ]; then
      log "removing old runner $old"
      rm -rf "${DIST_DIR:?}/$old"
    fi
  done
}

main() {
  check_host
  install_docker
  create_user_and_dirs
  install_runner
  install_ghr
  install_config
  install_service
  wait_ready
  gc_dist
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
```

- [ ] **Step 2: Write `github-runner/tests/setup_test.sh`**

```bash
#!/usr/bin/env bash
# Offline tests for setup.sh: no network, root, apt or systemd. External commands are
# replaced by shell functions and every path points into a temporary directory.
#   bash tests/setup_test.sh
# shellcheck disable=SC2016,SC2317,SC2329
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source-path=SCRIPTDIR/.. source=setup.sh
. "$HERE/../setup.sh"
set +e

T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT

FAILS=0
check() {
  local name="$1"
  shift
  if "$@"; then
    printf 'ok   %s\n' "$name"
  else
    printf 'FAIL %s\n' "$name"
    FAILS=$((FAILS + 1))
  fi
}
contains() { [[ "$1" == *"$2"* ]]; }
lacks() { [[ "$1" != *"$2"* ]]; }
between() { [ "$1" -ge "$2" ] && [ "$1" -le "$3" ]; }
absent() { [ ! -e "$1" ] && [ ! -L "$1" ]; }
count() { cat "$MARK_DIR/$1"; }
bump() { echo $(($(count "$1") + 1)) > "$MARK_DIR/$1"; }
# Runs a setup.sh function the way main runs: errexit on, die exits a subshell.
run() { (set -e; "$@"); }

# Git Bash without Windows symlink rights silently copies on `ln -s`; there the
# `current` link is emulated with a pointer file so the same assertions apply.
touch "$T/probe-target"
if ! { ln -s "$T/probe-target" "$T/probe-link" 2>/dev/null && [ -L "$T/probe-link" ]; }; then
  echo "note: no symlink support; emulating ln -sfn and readlink -e with pointer files"
  ln() {
    [ "$1" = -sfn ] || { echo "unexpected: ln $*" >&2; return 1; }
    rm -rf "$3" && printf '%s\n' "$2" > "$3"
  }
  readlink() {
    [ "$1" = -e ] && [ -f "$2" ] || return 1
    local target
    target=$(cat "$2")
    [ -e "$target" ] && printf '%s\n' "$target"
  }
fi
target_of_current() {
  local t
  t=$(readlink -e "$DIST_DIR/current") || return 1
  basename "$t"
}

FIXTURE_VERSION=2.337.0
FIX="$T/fixture"
mkdir -p "$FIX/bin"
printf '%s\n' '#!/usr/bin/env bash' 'echo runner' > "$FIX/run.sh"
printf '%s\n' '#!/usr/bin/env bash' \
  '[ ! -e "$DIST_DIR/$FIXTURE_VERSION" ] || touch "$MARK_DIR/published-before-deps"' \
  '[ ! -f "$MARK_DIR/fail-deps" ] || exit 1' \
  'touch "$(dirname "$0")/../deps-installed"' > "$FIX/bin/installdependencies.sh"
chmod +x "$FIX/run.sh" "$FIX/bin/installdependencies.sh"
tar -czf "$T/runner.tar.gz" -C "$FIX" .

fetch_runner() {
  bump fetches
  cp "$T/runner.tar.gz" "$2"
}
READY_AFTER=1
FAKE_STATE=active
ghr() {
  bump polls
  if [ "$1" = status ] && [ "$(count polls)" -ge "$READY_AFTER" ]; then
    echo "ghr status: ready"
    return 0
  fi
  echo "daemon unreachable" >&2
  return 1
}
systemctl() {
  [ "$1" = is-active ] || return 0
  echo "$FAKE_STATE"
  [ "$FAKE_STATE" = active ]
}
timeout() { shift; "$@"; }
sleep() { SECONDS=$((SECONDS + $1)); }

new_case() {
  DIST_DIR="$T/$1/dist"
  MARK_DIR="$T/$1/mark"
  mkdir -p "$DIST_DIR" "$MARK_DIR"
  echo 0 > "$MARK_DIR/fetches"
  echo 0 > "$MARK_DIR/polls"
  READY_AFTER=1
  FAKE_STATE=active
  READY_TIMEOUT=60
  RUNNER_VERSION=$FIXTURE_VERSION
  export DIST_DIR MARK_DIR FIXTURE_VERSION
  printf '\n# %s\n' "$1"
}
old_version() {
  mkdir -p "$DIST_DIR/$1"
  cp "$FIX/run.sh" "$DIST_DIR/$1/run.sh"
}

# (a) A failed installdependencies.sh publishes nothing; a re-run installs and publishes.
new_case a-install-runner
touch "$MARK_DIR/fail-deps"
out=$(run install_runner 2>&1)
rc=$?
check "a: failing installdependencies makes install_runner fail" [ "$rc" -ne 0 ]
check "a: error names installdependencies" contains "$out" "installdependencies.sh failed"
check "a: no published dist dir after failure" absent "$DIST_DIR/$FIXTURE_VERSION"
check "a: current not created after failure" absent "$DIST_DIR/current"
touch "$DIST_DIR/$FIXTURE_VERSION.tmp/junk-from-failed-run"
rm "$MARK_DIR/fail-deps"
out=$(run install_runner 2>&1)
rc=$?
check "a: re-run succeeds" [ "$rc" -eq 0 ]
check "a: re-run publishes an executable run.sh" [ -x "$DIST_DIR/$FIXTURE_VERSION/run.sh" ]
check "a: published dir has dependencies installed" [ -f "$DIST_DIR/$FIXTURE_VERSION/deps-installed" ]
check "a: dependencies ran before publishing" absent "$MARK_DIR/published-before-deps"
check "a: leftover temp dir from the failed run was discarded" absent "$DIST_DIR/$FIXTURE_VERSION/junk-from-failed-run"
check "a: no temp dir left after success" absent "$DIST_DIR/$FIXTURE_VERSION.tmp"
check "a: tarball not kept in the dist dir" absent "$DIST_DIR/$FIXTURE_VERSION/runner.tar.gz"
check "a: current points at the new version" [ "$(target_of_current)" = "$FIXTURE_VERSION" ]
run install_runner >/dev/null 2>&1
check "a: third run is a no-op for an installed version" [ "$(count fetches)" -eq 2 ]

# (b) Readiness loop.
new_case b-ready-after-polls
READY_AFTER=3
out=$(run wait_ready 2>&1)
rc=$?
check "b: ready after 3 polls succeeds" [ "$rc" -eq 0 ]
check "b: polled exactly 3 times" [ "$(count polls)" -eq 3 ]
check "b: prints ghr status" contains "$out" "ghr status: ready"

new_case b-service-failed
READY_AFTER=1000
FAKE_STATE=activating
out=$(run wait_ready 2>&1)
rc=$?
check "b: crashed service fails readiness" [ "$rc" -ne 0 ]
check "b: reports service failed" contains "$out" "ghr service failed (systemd state: activating)"
check "b: points at journalctl" contains "$out" "journalctl -u ghr"
check "b: does not report still starting" lacks "$out" "still starting"
check "b: fails on the first poll, without waiting" [ "$(count polls)" -eq 1 ]

new_case b-still-starting
READY_AFTER=1000
READY_TIMEOUT=5
out=$(run wait_ready 2>&1)
rc=$?
check "b: never-ready service fails readiness" [ "$rc" -ne 0 ]
check "b: reports still starting after the timeout" contains "$out" "ghr service still starting after 5s"
check "b: does not report service failed" lacks "$out" "service failed"
# 6 polls at simulated t=0..5; one fewer if a real second boundary passes mid-test.
check "b: polling is bounded by the timeout" between "$(count polls)" 5 6

# (c) Dist GC.
check_host() { :; }
install_docker() { :; }
create_user_and_dirs() { :; }
install_ghr() { :; }
install_config() { :; }
install_service() {
  bump restarts
  [ -d "$DIST_DIR/2.300.0" ] && [ -d "$DIST_DIR/2.336.0" ] && touch "$MARK_DIR/old-present-at-restart"
  [ "$(target_of_current)" = "$FIXTURE_VERSION" ] && touch "$MARK_DIR/current-new-at-restart"
}

new_case c-main-upgrade
echo 0 > "$MARK_DIR/restarts"
old_version 2.300.0
old_version 2.336.0
ln -sfn "$DIST_DIR/2.336.0" "$DIST_DIR/current"
out=$(run main 2>&1)
rc=$?
check "c: upgrade via main succeeds" [ "$rc" -eq 0 ]
check "c: service restarted once" [ "$(count restarts)" -eq 1 ]
check "c: old versions still present at restart" [ -f "$MARK_DIR/old-present-at-restart" ]
check "c: current already switched at restart" [ -f "$MARK_DIR/current-new-at-restart" ]
check "c: old version 2.300.0 removed after ready" absent "$DIST_DIR/2.300.0"
check "c: old version 2.336.0 removed after ready" absent "$DIST_DIR/2.336.0"
check "c: new version kept" [ -x "$DIST_DIR/$FIXTURE_VERSION/run.sh" ]
check "c: current kept and points at the new version" [ "$(target_of_current)" = "$FIXTURE_VERSION" ]

new_case c-main-not-ready
echo 0 > "$MARK_DIR/restarts"
old_version 2.336.0
ln -sfn "$DIST_DIR/2.336.0" "$DIST_DIR/current"
READY_AFTER=1000
FAKE_STATE=activating
out=$(run main 2>&1)
rc=$?
check "c: main fails when the service is not ready" [ "$rc" -ne 0 ]
check "c: no GC when the service is not ready" [ -d "$DIST_DIR/2.336.0" ]

new_case c-gc-direct
old_version 2.300.0
old_version 2.336.0
old_version 2.337.0
mkdir -p "$DIST_DIR/2.338.0.tmp"
ln -sfn "$DIST_DIR/2.336.0" "$DIST_DIR/current"
out=$(run gc_dist 2>&1)
rc=$?
check "c: gc_dist succeeds" [ "$rc" -eq 0 ]
check "c: gc keeps current's target" [ -d "$DIST_DIR/2.336.0" ]
check "c: gc keeps current" [ "$(target_of_current)" = 2.336.0 ]
check "c: gc removes an older version" absent "$DIST_DIR/2.300.0"
check "c: gc removes a newer non-current version" absent "$DIST_DIR/2.337.0"
check "c: gc removes a leftover temp dir" absent "$DIST_DIR/2.338.0.tmp"

echo
if [ "$FAILS" -gt 0 ]; then
  echo "$FAILS assertion(s) failed"
  exit 1
fi
echo "all assertions passed"
```

- [ ] **Step 3: Write `github-runner/config.example.yaml`**

```yaml
owner: darkraise
mode: queue                 # queue | all
global_max: 2               # queue mode only
poll_interval: 10s
start_timeout: 2m
idle_timeout: 5m
disk_high_water: 80         # percent of the Docker data-root filesystem
build_cache_keep: 20GB
history_retention: 30d
labels: [homelab, docker]   # added to every runner; keeps existing workflows matching
runner_limits:
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

- [ ] **Step 4: Write `github-runner/.gitattributes`**

```text
# homelab has core.autocrlf=true; setup.sh is copied to Linux and must keep LF.
*.sh text eol=lf
```

- [ ] **Step 5: Run the check**

Run: `bash -n github-runner/setup.sh && bash -n github-runner/tests/setup_test.sh && (cd github-runner && bash tests/setup_test.sh) && git check-attr eol github-runner/setup.sh`
Expected: exit 0, the setup test prints its passing cases, and `github-runner/setup.sh: eol: lf`.

- [ ] **Step 6: Shellcheck**

Run `shellcheck github-runner/setup.sh github-runner/tests/setup_test.sh` if `shellcheck` is installed; otherwise `docker run --rm -v "$PWD/github-runner:/mnt" koalaman/shellcheck:stable /mnt/setup.sh /mnt/tests/setup_test.sh`.
Expected: no output, exit 0. If neither is available, say so to your human partner instead of skipping silently.

- [ ] **Step 7: Commit**

```bash
git add github-runner/setup.sh github-runner/tests/setup_test.sh github-runner/config.example.yaml github-runner/.gitattributes
git update-index --chmod=+x github-runner/setup.sh
git ls-files -s github-runner/setup.sh   # must start with 100755
git commit -m "feat(github-runner): add native runner setup"
```

---

### Task 23: Homelab runner README

Working directory: `D:/Repositories/Personal/homelab`

**Files:**
- Create: `github-runner/README.md`

**Interfaces:**
- Consumes: `setup.sh` usage and CLI subcommands (Contracts → CLI entry).
- Produces: `github-runner/README.md`.

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 0 - spec 0 - coupling 1 - risk 0 = 1

- [ ] **Step 1: Write `github-runner/README.md`**

````markdown
# github-runner

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

Copy this directory to the LXC and run as root:

```bash
GHR_TOKEN=github_pat_... bash setup.sh
```

Re-run `bash setup.sh` to upgrade Docker, the runner and ghr. It never overwrites
`/etc/ghr/config.yaml` or `/etc/ghr/token`. Pin versions with `GHR_VERSION=v0.1.0` or
`RUNNER_VERSION=2.337.0`. Prefer re-running while no jobs are running: a Docker upgrade
restarts the Docker daemon, which stops the containers of running jobs.

## Use

```bash
ghr tui                       # dashboard (keyboard + mouse)
ghr status
ghr pause darkcloud           # no new runners; running jobs finish
ghr set global-max 3
ghr set mode all
ghr repo add newrepo --label newrepo-linux   # add it to the PAT's repo access first
ghr token set < new-token.txt
ghr logs <id> -f
journalctl -u ghr -f          # daemon log
```

While the TUI captures the mouse, select terminal text with Shift-drag (Option-drag in
iTerm2). Under tmux, mouse input requires `set -g mouse on`.

Edits made through the CLI or TUI rewrite `/etc/ghr/config.yaml`, dropping comments.
After editing the file by hand, apply it with `systemctl reload ghr`.

## Migrating from the compose runners

1. On the old host: `docker compose -f github-runner/compose.yml down` (project `gh-runners`).
2. In each repo's Settings → Actions → Runners, delete the `homelab-<repo>` runner.
   ghr only cleans up runners named `ghr-*`.
3. On the old host: `rm -rf /opt/gh-runners`.
4. Workflows need no edits: the default labels keep `homelab` and `docker`, and the example
   config keeps each `<repo>-linux` label.
````

- [ ] **Step 2: Run the check**

Run: `grep -q 'GHR_TOKEN=github_pat_' github-runner/README.md && grep -q 'Migrating from the compose runners' github-runner/README.md`
Expected: PASS (exit 0).

- [ ] **Step 3: Commit**

```bash
git add github-runner/README.md
git commit -m "docs(github-runner): document install, migration"
```

---

### Task 24: Publish v0.1.0 and verify on the LXC

Working directory: `D:/Repositories/Personal/ghr` and `D:/Repositories/Personal/homelab` locally (with `gh` authenticated as darkraise); the LXC as root over SSH.

**Files:**
- Create: `.github/workflows/e2e-waiting.yml` (ghr)
- Create in the scratch repo `darkraise/ghr-e2e`: `.github/workflows/cap.yml`, `compose.yml`, `testcontainers.yml`, `single.yml`, `tc/go.mod`, `tc/tc_test.go`
- Create: `docs/superpowers/notes/2026-10-03-ghr-e2e.md` (homelab)
- Modify: `CLAUDE.md` (homelab)
- Delete (only with approval): `github-runner/compose.yml`, `github-runner/.env.example` (homelab, untracked)

**Interfaces:**
- Consumes: every earlier task; Release assets, On-disk paths and CLI entry (Contracts).
- Produces: release `v0.1.0`; a running ghr LXC; the E2E record.

**Items:** 1, 2, 3, 4, 5

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

> Every step that publishes, touches GitHub settings, deletes data, or needs the LXC is gated on your human partner. Ask, wait for a yes, and record their answer in the E2E note. Load the helpers below into your local shell first; every check records the `jobs` output, which proves which runner (`ghr-<repo>-<id>`) ran each job and when.

```bash
# dispatch <repo> <workflow>: start a run and print its ID
dispatch() {
  local since; since=$(date -u +%FT%TZ)
  gh workflow run "$2" --repo "darkraise/$1" >/dev/null || return 1
  for _ in $(seq 30); do
    id=$(gh run list --repo "darkraise/$1" --workflow "$2" --event workflow_dispatch --created ">=$since" --json databaseId --jq '.[0].databaseId')
    [ -n "$id" ] && { echo "$id"; return 0; }
    sleep 2
  done
  echo "no run of $2 appeared" >&2; return 1
}
# jobs <repo> <run-id>: name, status, runner, start, end, conclusion per job
jobs() { gh api "repos/darkraise/$1/actions/runs/$2/jobs" --jq '.jobs[] | [.name, .status, .runner_name, .started_at, .completed_at, .conclusion] | @tsv'; }
# pushed_run <repo> <workflow> <sha>: wait up to 5 minutes for the push run of <sha>, print its ID
pushed_run() {
  for _ in $(seq 60); do
    id=$(gh run list --repo "darkraise/$1" --workflow "$2" --event push --commit "$3" --json databaseId --jq '.[0].databaseId')
    [ -n "$id" ] && { echo "$id"; return 0; }
    sleep 5
  done
  echo "no $2 run for $3" >&2; return 1
}
```

- [ ] **Step 1: Ask to publish**

Ask your human partner: "Push `ghr` master to darkraise/ghr (including the e2e-waiting workflow) and tag v0.1.0?" Stop until they say yes. Then write `D:/Repositories/Personal/ghr/.github/workflows/e2e-waiting.yml`:

```yaml
name: e2e-waiting
on: workflow_dispatch
jobs:
  gated:
    runs-on: [self-hosted, homelab]
    environment: e2e-wait
    steps:
      - run: echo "released after the wait timer"
  independent:
    runs-on: [self-hosted, homelab]
    steps:
      - run: echo "ran while gated waits, on $RUNNER_NAME"
```

and commit it: `git -C D:/Repositories/Personal/ghr add .github/workflows/e2e-waiting.yml && git -C D:/Repositories/Personal/ghr commit -m "test(e2e): add waiting-run fixture workflow"`.

- [ ] **Step 2: Push and wait for CI on that commit**

```bash
sha=$(git -C D:/Repositories/Personal/ghr rev-parse HEAD)
git -C D:/Repositories/Personal/ghr push -u origin master
id=$(pushed_run ghr ci "$sha") && gh run watch "$id" --repo darkraise/ghr --exit-status
```

Expected: the `ci` run for `$sha` succeeds (gofmt, vet, `go test -race`). Under `CI` the hook tests fail rather than skip without jq, so success also verifies jq on ubuntu-latest.

- [ ] **Step 3: Tag and release that commit**

```bash
git -C D:/Repositories/Personal/ghr tag v0.1.0 "$sha" && git -C D:/Repositories/Personal/ghr push origin v0.1.0
id=$(pushed_run ghr release "$sha") && gh run watch "$id" --repo darkraise/ghr --exit-status
gh release view v0.1.0 --repo darkraise/ghr --json assets --jq '.assets[].name'
```

Expected: the release run for `$sha` succeeds; assets `ghr_linux_amd64.tar.gz` and `checksums.txt`.

- [ ] **Step 4: Prepare the LXC**

Ask your human partner to create a Debian 13 LXC with `features: nesting=1,keyctl=1`, give you root SSH access, create the private scratch repo `darkraise/ghr-e2e`, and create a fine-grained PAT (Administration: read/write, Actions: read) for darkmem, darkcloud, darkagents, ghr-e2e and ghr. Copy `D:/Repositories/Personal/homelab/github-runner/` to the LXC; in the copied `config.example.yaml` add a repo `ghr-e2e` with `max: 3`. On the LXC:

```bash
cd github-runner && GHR_TOKEN=<pat> bash setup.sh && bash setup.sh && ghr status
```

Expected: both runs exit 0 (the second changes nothing), and `ghr status` lists the four repos with no error.

- [ ] **Step 5: Record the static assumptions**

On the LXC: `docker builder prune --help | grep -E 'reserved-space|keep-storage'`. Expected: at least one flag; record which. `PruneBuildCacheTo` uses `--reserved-space` exactly when it is listed.

- [ ] **Step 6: Add the fixtures to ghr-e2e**

Clone `darkraise/ghr-e2e` into a scratch directory and add these files, then commit and push them (`git commit -m "test(e2e): add ghr e2e fixtures"`):

`.github/workflows/cap.yml`

```yaml
name: cap
on: workflow_dispatch
jobs:
  parallel:
    strategy:
      matrix:
        n: [1, 2, 3]
    runs-on: [self-hosted, homelab]
    steps:
      - run: echo "job ${{ matrix.n }} on $RUNNER_NAME at $(date -u +%T)"; sleep 120
```

`.github/workflows/compose.yml`

```yaml
name: compose
on: workflow_dispatch
jobs:
  web:
    strategy:
      matrix:
        n: [1, 2]
    runs-on: [self-hosted, homelab]
    steps:
      - run: |
          mkdir -p e2e && cd e2e
          printf 'services:\n  web:\n    image: nginx:alpine\n    ports: ["127.0.0.1::80"]\n    volumes: ["./index.html:/usr/share/nginx/html/index.html:ro"]\n' > compose.yml
          echo "ghr-ok-${{ matrix.n }}" > index.html
          docker compose up -d --wait
          port=$(docker compose port web 80 | cut -d: -f2)
          echo "project $COMPOSE_PROJECT_NAME port $port"
          curl -fsS "http://127.0.0.1:$port" | grep "ghr-ok-${{ matrix.n }}"
          # job 1 ends first; job 2 checks its own stack survived job 1's cleanup
          sleep $(( ${{ matrix.n }} == 1 ? 30 : 120 ))
          curl -fsS "http://127.0.0.1:$port" | grep "ghr-ok-${{ matrix.n }}"
```

`.github/workflows/testcontainers.yml`

```yaml
name: testcontainers
on: workflow_dispatch
jobs:
  redis:
    runs-on: [self-hosted, homelab]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: tc/go.mod
          cache: false
      - run: cd tc && go mod tidy && go test -count=1 -timeout 180s ./...
```

`.github/workflows/single.yml`

```yaml
name: single
on: workflow_dispatch
jobs:
  one:
    runs-on: [self-hosted, homelab]
    steps:
      - run: echo "started $(date -u +%FT%TZ) on $RUNNER_NAME"; sleep 30
```

`tc/go.mod`

```text
module ghr-e2e/tc

go 1.26

require github.com/testcontainers/testcontainers-go v0.44.0
```

`tc/tc_test.go`

```go
package tc

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestRedisReachable starts a container through the host Docker daemon and
// connects to its mapped port: the path and network checks of the spec's E2E 3.
func TestRedisReachable(t *testing.T) {
	ctx := context.Background()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "redis:7-alpine",
			ExposedPorts: []string{"6379/tcp"},
			WaitingFor:   wait.ForListeningPort("6379/tcp"),
		},
		Started: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Terminate(context.Background()) })
	endpoint, err := c.Endpoint(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", endpoint, 5*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", endpoint, err)
	}
	conn.Close()
}
```

- [ ] **Step 7: Check A — global cap, runner user**

On the LXC: `ghr pause darkmem && ghr pause darkcloud && ghr pause darkagents && ghr set global-max 2` (no other repo can take a slot). Locally: `run=$(dispatch ghr-e2e cap)`. While it runs, every 15 s for 4 minutes record `jobs ghr-e2e $run` and, on the LXC, `ghr status`; once a job is in progress, on the LXC record `systemctl show ghr-runner-<id> -p User -p ActiveState` for one of the IDs `ghr status` lists.
Expected: never more than 2 jobs `in_progress` and never more than 2 ghr instances besides `cleaning`; the third job stays `queued` until one of the first two completes, then starts (its `started_at` ≥ that `completed_at`); the unit shows `User=ghrunner` and `ActiveState=active`; every `runner_name` starts with `ghr-ghr-e2e-`.

- [ ] **Step 8: Check B — concurrent compose jobs, label template**

On the LXC: `ghr set global-max 3`. Locally: `run=$(dispatch ghr-e2e compose)`. While both jobs run, on the LXC record `docker ps --filter label=com.docker.compose.project --format '{{.Names}}\t{{.Label "com.docker.compose.project"}}'`. Afterwards record `jobs ghr-e2e $run` and `docker ps -a --filter label=com.docker.compose.project`.
Expected: the `docker ps` output is non-empty and shows two different projects `ghr-<id>`; both jobs succeed (job 2's final request ran after job 1 finished and was cleaned up, so its stack survived); their `started_at`–`completed_at` intervals overlap; no compose containers remain afterwards.

- [ ] **Step 9: Check C — Testcontainers**

Locally: `run=$(dispatch ghr-e2e testcontainers)`, wait for it, record `jobs ghr-e2e $run`; on the LXC `docker ps -a --filter ancestor=redis:7-alpine`.
Expected: the job succeeds with no path or network workarounds; no redis container remains.

- [ ] **Step 10: Check D — daemon restart and daemon down**

Restart: locally `run=$(dispatch ghr-e2e cap)`; once a job is `in_progress`, on the LXC `systemctl restart ghr`. Expected: all three jobs succeed, and `ghr status` shows the busy runner adopted after the restart.
Daemon down: on the LXC `ghr set mode all`, then wait until `ghr status` shows an `idle` ghr-e2e runner. Note the time, then `systemctl stop ghr`. Locally `run=$(dispatch ghr-e2e single)` and wait for it to finish; then on the LXC `systemctl start ghr`.
Expected: the job's `started_at` is after the stop time (it started while the daemon was down) and it succeeds; after the start, `ghr history --repo ghr-e2e` lists it once.

- [ ] **Step 11: Check E — warm runner in all mode**

Still in `all` mode, with no ghr-e2e jobs running: record `ghr status`, wait 6 minutes (longer than `idle_timeout`), record it again. Expected: the same idle ghr-e2e runner ID in both. Then `ghr set mode queue`.

- [ ] **Step 12: Check F — queued job inside a waiting run (darkraise/ghr)**

Ask your human partner for a yes to: register the public `ghr` repo with ghr for this check only; require approval for all outside contributors' fork-PR workflows; create the environment `e2e-wait`. Then:

```bash
gh api -X PUT repos/darkraise/ghr/actions/permissions/fork-pr-contributor-approval -f approval_policy=all_external_contributors
gh api -X PUT repos/darkraise/ghr/environments/e2e-wait -F wait_timer=10
```

On the LXC: `ghr repo add ghr --allow-public && ghr pause ghr`. Locally: `run=$(dispatch ghr e2e-waiting)`; while ghr keeps `ghr` paused (the 10-minute wait timer leaves ample time), record `gh api "repos/darkraise/ghr/actions/runs?status=waiting" --jq '.workflow_runs[] | [.id, .status] | @tsv'` and `jobs ghr $run`. Then on the LXC `ghr resume ghr`; record `jobs ghr $run` until the run completes. Finally `ghr repo rm ghr` and confirm with `ghr status`.
Expected: the waiting-runs list contains `$run` with status `waiting` while `independent` is `queued` and `gated` is `waiting`; after the resume, the `jobs` output taken when `independent` turns `in_progress` (on a `ghr-ghr-` runner) still shows `gated` `waiting`, and `independent` completes before `gated` starts. If `gated` is no longer `waiting` at that point, the check did not exercise the path: repeat it rather than recording a pass. Both jobs succeed; `ghr` is gone from `ghr status`.

- [ ] **Step 13: Check G — TUI**

On the LXC: `ghr resume darkmem && ghr resume darkcloud && ghr resume darkagents`, then `ghr tui` over SSH: click each tab; click a runner row and double-click it (steps and containers appear); click a footer hint; scroll the events pane with the wheel; open the Config tab and save a field with the mouse. Expected: each action works; Shift-drag selects text.

- [ ] **Step 14: Record results**

Write `D:/Repositories/Personal/homelab/docs/superpowers/notes/2026-10-03-ghr-e2e.md` with each check's result, the recorded outputs (excerpts), and your human partner's approvals.

- [ ] **Step 15: Migrate from the compose runners**

With your human partner's yes for each: on the old host `docker compose -f github-runner/compose.yml down`; delete the `homelab-darkmem`, `homelab-darkcloud`, `homelab-darkagents` runners in each repo's Settings → Actions → Runners; `rm -rf /opt/gh-runners` on the old host. Then ask whether to delete the untracked `D:/Repositories/Personal/homelab/github-runner/compose.yml` and `.env.example`; delete them only on an explicit yes.

- [ ] **Step 16: Document in homelab CLAUDE.md**

Under "Application services" in `D:/Repositories/Personal/homelab/CLAUDE.md`, add after the list:

```markdown
`github-runner/` is the exception to "one compose per LXC": it holds `setup.sh` for native GitHub Actions runners managed by `ghr` (github.com/darkraise/ghr) on a dedicated LXC. See its README.
```

- [ ] **Step 17: Commit homelab**

```bash
git -C D:/Repositories/Personal/homelab add CLAUDE.md docs/superpowers/notes/2026-10-03-ghr-e2e.md
git -C D:/Repositories/Personal/homelab commit -m "docs(github-runner): record ghr e2e results"
```
