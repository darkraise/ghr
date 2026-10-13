# GitHub Artifact Storage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Keep GitHub Actions artifacts from using up the account's monthly storage allowance: a quota monitor with warnings, an age-based auto-cleanup, an Artifacts page, and per-repository retention and Actions cache management.

**Architecture:** A new `internal/ghstorage` service polls GitHub (billing hourly, an inventory of every reachable repository's artifacts and caches every 15 minutes), keeps an in-memory snapshot, runs the cleanup at the end of each inventory, and runs manual deletes and retention changes through one write worker with a rolling-hour budget. The daemon serves the snapshot and the operations under `/github-storage`, adds a quota alert to `/status`, a `github_storage` config block and patch, and a `github-storage` change topic. The web app gains an Artifacts tab and a quota banner in the shell.

**Tech Stack:** Go 1.26 (standard library only); TypeScript 5.7, React 19, TanStack Router and Query 5, the darkraise-ui kit, Tailwind 4, Vitest 4 with Testing Library and jsdom, all already dependencies.

**Spec:** docs/superpowers/specs/2026-10-13-ghr-github-storage-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 9 of 26 tasks are heavy (3, 4, 6, 7, 8, 9, 10, 11, 23) and are delegated; the other 17 run in the session
**Plan review:** 2026-10-13 — dr-superpowers:judge-opus — executability 18 / coherence 18 / coverage 17 / assumptions 16 (round 1)

**Parallelism:** sequential — the owner chose sequential (spec): the cleanup deletes data and runs alone, and the web tasks consume the daemon's JSON

## Global Constraints

- Commands run from the repository root. `/tmp` is a small tmpfs that is nearly full: before the first command of a session run `mkdir -p /root/.cache/ghr-plan-tmp`, and put `TMPDIR=/root/.cache/ghr-plan-tmp` in front of every `npm`, `npx` and Go command. Go is at `/usr/local/go/bin` and not on PATH: prefix every Go command with `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH`.
- Give every command a `timeout`: 300 seconds for one Go package or one web test file, 900 for a whole suite.
- A fresh worktree has no `web/node_modules`. Once, before the first web command: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 600 npm ci --prefer-offline --no-audit --no-fund`. `web/package-lock.json` must not change.
- While working on a task run only the tests its commands name; only the last task runs every suite. A web test that only times out is the loaded host: run it once more, alone, before believing it. Never start a dev server, a watcher or any background process.
- Before each Go commit: `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` prints nothing, and `go vet ./internal/... ./cmd/...` passes. Before each web commit: `cd web && npx tsc --noEmit` and `cd web && npx eslint src --max-warnings 0` print nothing.
- `web/fixtures_test.go` (`TestTypeFixtures`) marshals Go values and compares them with `web/src/api/fixtures/*.json`. A task that changes a JSON shape those fixtures hold updates the fixture in the same task and runs `go test ./web/`.
- No new Go module and no new npm dependency.
- User-visible texts (labels, messages, events, errors, log lines) are part of the spec. Copy them exactly as the task gives them. UI strings contain no en dash and no em dash (`web/src/anti-slop.test.ts`).
- Sizes: bytes become GB with 2^30 (`model.GiB`), `included_mb` becomes GB with 1024.
- When an existing test fails because it asserted behaviour this plan changes, the task says so and gives the corrected lines. Report any other existing-test failure in the task report; never change production code to make an old expectation pass.
- A task's "replace these lines" blocks were written against `master` at `a394778` plus the earlier tasks of this plan. When a block is not found exactly as quoted, find the same lines by their content, apply the same change, and say so in the task report; never skip the change.
- Code comments: only where the reason is not obvious from the code. No comment mentions a task, a register row or the spec.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no trailing period, no attribution or co-author lines.
- Never push, never write to GitHub (no delete, no retention change, no `gh api -X` other than GET), and never deploy to or restart anything on the runner LXC (192.168.0.99). The one exception is the last task: it deploys only after the owner's yes in its first step, and its checks there are read-only.
- English only.

## Contracts

Names one task adds and a later task uses. Every JSON tag is snake_case.

### GitHub client (Task 1, package `github`, `internal/github/storage.go` unless named)

- `Artifact` (`runs.go`) gains `CreatedAt time.Time \`json:"created_at"\`` and `WorkflowRun ArtifactRun \`json:"workflow_run"\``; `type ArtifactRun struct { ID int64 \`json:"id"\`; HeadBranch string \`json:"head_branch"\` }`.
- `type Cache struct { ID int64 \`json:"id"\`; Key string \`json:"key"\`; Ref string \`json:"ref"\`; SizeInBytes int64 \`json:"size_in_bytes"\`; LastAccessedAt time.Time \`json:"last_accessed_at"\`; CreatedAt time.Time \`json:"created_at"\` }`.
- `const SKUActionsStorage = "actions_storage"`, `const SKUPackagesStorage = "packages_storage"`; `func NormalizeSKU(s string) string` lower-cases, turns spaces into underscores and drops hyphens (`"Actions storage"` → `actions_storage`, `"GigabyteHours"` and `"gigabyte-hours"` → `gigabytehours`).
- `type UsageItem struct { Date time.Time; SKU string; Repo string; Quantity float64 }`: `SKU` is normalized; `Repo` is GitHub's `repositoryName` (a bare name, empty for Packages).
- `type SummaryItem struct { SKU string; Gross, Discount, Net float64 }` from `grossQuantity`, `discountQuantity`, `netQuantity`.
- `type Retention struct { Days int \`json:"days"\`; MaxDays int \`json:"maximum_allowed_days"\` }`.
- Methods on `*Client`:
  - `OwnerType(ctx context.Context) (isOrg bool, err error)` — `GET /users/{owner}`, `type == "Organization"`.
  - `TokenLogin(ctx context.Context) (string, error)` — `GET /user`.
  - `StorageUsage(ctx context.Context, isOrg bool, year int, month time.Month) ([]UsageItem, error)` — only items with unit `GigabyteHours` and SKU `actions_storage` or `packages_storage`.
  - `StorageSummary(ctx context.Context, isOrg bool, year int, month time.Month) ([]SummaryItem, error)` — only the two storage SKUs.
  - `ListRepoArtifacts(ctx context.Context, repo string) ([]Artifact, error)` and `ListCaches(ctx context.Context, repo string) ([]Cache, error)` — every page, `per_page=100`.
  - `DeleteArtifact(ctx context.Context, repo string, id int64) (deleted bool, err error)` and `DeleteCache(...)` with the same shape — through `mutate`; 204 is `true`, 404 is `false` with no error.
  - `Retention(ctx context.Context, repo string) (Retention, error)` and `SetRetention(ctx context.Context, repo string, days int) error` (through `mutate`).
- `func IsTooManyPages(err error) bool` reports `errors.Is(err, errTooManyPages)`.

### Model (Task 2, package `model`, `internal/model/ghstorage.go`)

The names, types and tags below are exact; Task 2 gives the file with its doc comments.

```go
const GiB = 1 << 30

type GitHubStorage struct {
	CheckedAt  *time.Time         `json:"checked_at,omitempty"`
	Quota      *GitHubQuota       `json:"quota,omitempty"`
	QuotaError string             `json:"quota_error,omitempty"`
	Error      string             `json:"error,omitempty"` // the repository listing failed; Repos keeps the last list
	Repos      []RepoStorage      `json:"repos"`
	Operation  *GitHubOperation   `json:"operation,omitempty"`
	Deletions  []Deletion         `json:"deletions"`
}

type GitHubQuota struct {
	Month                string     `json:"month"` // "2026-10", UTC
	ReadAt               time.Time  `json:"read_at"`
	IncludedGBHours      float64    `json:"included_gb_hours"`
	UsedGBHours          float64    `json:"used_gb_hours"`
	ActionsGBHours       float64    `json:"actions_gb_hours"`
	PackagesGBHours      float64    `json:"packages_gb_hours"`
	RateGBHoursPerHour   float64    `json:"rate_gb_hours_per_hour"`
	EstimatedGBHours     float64    `json:"estimated_gb_hours"`
	OverAllowanceGBHours *float64   `json:"over_allowance_gb_hours,omitempty"`
	ProjectedGBHours     float64    `json:"projected_gb_hours"`
	RunsOutAt            *time.Time `json:"runs_out_at,omitempty"`
	Level                string     `json:"level"` // "", "projected", "warn", "critical", "exhausted"
	ResetsAt             time.Time  `json:"resets_at"`
}

type RepoStorage struct {
	Name             string         `json:"name"`
	Visibility       string         `json:"visibility"` // "public" or "private"
	Covered          bool           `json:"covered"`
	ArtifactCount    int            `json:"artifact_count"`
	ArtifactBytes    int64          `json:"artifact_bytes"`
	CacheCount       int            `json:"cache_count"`
	CacheBytes       int64          `json:"cache_bytes"`
	RetentionDays    int            `json:"retention_days"`
	RetentionMaxDays int            `json:"retention_max_days"`
	Cleanup          string         `json:"cleanup"`        // "off" or a duration such as "2d", "6h"
	CleanupSource    string         `json:"cleanup_source"` // "global", "override", "none"
	Artifacts        []ArtifactItem `json:"artifacts"`
	Caches           []CacheItem    `json:"caches"`
	Error            string         `json:"error,omitempty"`
}

type ArtifactItem struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Bytes     int64      `json:"bytes"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	RunID     int64      `json:"run_id"`
	Branch    string     `json:"branch"`
}

type CacheItem struct {
	ID         int64     `json:"id"`
	Key        string    `json:"key"`
	Ref        string    `json:"ref"`
	Bytes      int64     `json:"bytes"`
	LastUsedAt time.Time `json:"last_used_at"`
}

type GitHubOperation struct {
	ID         string             `json:"id"`
	Kind       string             `json:"kind"` // "delete-artifacts", "delete-caches", "set-retention"
	Repo       string             `json:"repo,omitempty"`
	Total      int                `json:"total"`
	Done       int                `json:"done"`
	Bytes      int64              `json:"bytes"`
	Failed     []OperationFailure `json:"failed"`
	StartedAt  time.Time          `json:"started_at"`
	FinishedAt *time.Time         `json:"finished_at,omitempty"`
}

type OperationFailure struct {
	Repo  string `json:"repo"`
	ID    int64  `json:"id,omitempty"`
	Error string `json:"error"`
}

type Deletion struct {
	At    time.Time `json:"at"`
	Repo  string    `json:"repo"`
	Kind  string    `json:"kind"` // "artifact" or "cache"
	Name  string    `json:"name"`
	Bytes int64     `json:"bytes"`
	Age   string    `json:"age"`
	By    string    `json:"by"` // "cleanup" or "manual"
}

type StorageAlert struct {
	Level           string     `json:"level"`
	Month           string     `json:"month"`
	UsedGBHours     float64    `json:"used_gb_hours"`
	IncludedGBHours float64    `json:"included_gb_hours"`
	UsedPct         int        `json:"used_pct"`
	RunsOutAt       *time.Time `json:"runs_out_at,omitempty"`
	ResetsAt        time.Time  `json:"resets_at"`
}

type GitHubStoragePatch struct {
	IncludedMB    *int               `json:"included_mb,omitempty"`
	WarnPct       *[]int             `json:"warn_pct,omitempty"`
	CleanupMaxAge *string            `json:"cleanup_max_age,omitempty"` // "" turns cleanup off
	Repos         map[string]*string `json:"repos,omitempty"`           // null removes the override
}

type StoragePreview struct {
	Repo  string `json:"repo"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes"`
}

type DeleteArtifactsRequest struct {
	IDs       []int64 `json:"ids,omitempty"`
	OlderThan string  `json:"older_than,omitempty"`
}

type DeleteCachesRequest struct {
	IDs []int64 `json:"ids,omitempty"`
	All bool    `json:"all,omitempty"`
}

type RetentionRequest struct {
	Days int `json:"days"`
}

type OperationStarted struct {
	ID string `json:"id"`
}
```

`model.Status` gains `GitHubStorage *StorageAlert \`json:"github_storage,omitempty"\`` after `WebListenError`; `model.ConfigPatch` gains `GitHubStorage *GitHubStoragePatch \`json:"github_storage,omitempty"\`` after `Web`. `RepoStorage.Artifacts` and `Caches`, `GitHubStorage.Repos` and `Deletions`, and `GitHubOperation.Failed` are never nil in the API output (`[]`).

### Config (Task 3, package `config`, `internal/config/ghstorage.go`)

```go
type GitHubStorage struct {
	IncludedMB    int                        `yaml:"included_mb,omitempty" json:"included_mb,omitempty"`
	WarnPct       []int                      `yaml:"warn_pct,omitempty" json:"warn_pct,omitempty"`
	CleanupMaxAge Duration                   `yaml:"cleanup_max_age,omitempty" json:"cleanup_max_age,omitempty"`
	Repos         map[string]StorageOverride `yaml:"repos,omitempty" json:"repos,omitempty"`
}

type StorageOverride struct {
	Cleanup string `yaml:"cleanup" json:"cleanup"` // "off" or a duration
}
```

- `Config` gains `GitHubStorage GitHubStorage \`yaml:"github_storage,omitempty" json:"github_storage,omitzero"\`` after `Web`; `Validate` appends `c.GitHubStorage.validate()...` to its errors.
- `const DefaultIncludedMB = 500`; `MinCleanupMaxAge = 24 * time.Hour`; `MinCleanupOverride = time.Hour`; `var DefaultWarnPct = [2]int{70, 90}`.
- `func (g GitHubStorage) Included() int` (0 means 500) and `func (g GitHubStorage) Warn() [2]int` (nil means `[70, 90]`).
- `func (g GitHubStorage) Override(repo string) (StorageOverride, bool)` matches keys ignoring case.
- `func (g GitHubStorage) CleanupFor(repo string) (age time.Duration, source string)`: the override when there is one (`"override"`, age 0 for `off`), else the global age (`"global"`), else `0, "none"`.
- `func ParseCleanup(s string) (time.Duration, error)`: `"off"` is 0; anything else is `ParseDuration`.
- `func CleanupText(d time.Duration) string`: `"off"` for 0, else `ShortDuration(d)`.
- `func ShortDuration(d time.Duration) string`: whole days `"2d"`, whole hours `"6h"`, whole minutes `"90m"`, otherwise `d.String()`.
- Validation messages, exactly:
  - `github_storage.included_mb must be at least 1`
  - `github_storage.warn_pct must be two increasing percentages from 1 to 100, such as [70, 90]`
  - `github_storage.cleanup_max_age must be at least 24h`
  - `github_storage.repos: <key>: cleanup must be off or a duration of at least 1h`
  - `github_storage.repos must not contain an empty name`
  - `github_storage.repos: <key> must be a repository name under owner, without a slash`
  - `github_storage.repos: <a> and <b> differ only by case` (keys taken in sorted order; `<a>` sorts first)

### Patch (Task 4, package `daemon`, `internal/daemon/storagepolicy.go`)

- `func applyStoragePatch(c *config.Config, p *model.GitHubStoragePatch) error` — called by `Backend.PatchConfig` inside its update after the `Web` block. A bad duration or an `included_mb` below 1 returns `api.BadRequest` with the matching Config message; a repository value replaces any key that matches ignoring case, `null` deletes it, and an emptied map becomes nil.
- A value equal to its default (`included_mb` 500, `warn_pct` [70, 90]) is stored as the zero value, so saving the defaults writes no block. An unparsable `cleanup_max_age` is refused as `github_storage.cleanup_max_age: invalid duration "<s>"`.
- Test helper (`internal/daemon/storagepolicy_test.go`): `patchJSON(t *testing.T, b *Backend, body string) error` decodes `body` into a `model.ConfigPatch` and calls `b.PatchConfig`.

### Quota math (Task 5, package `ghstorage`, `internal/ghstorage/quota.go`)

- `const LagHours = 12`.
- `type Billing struct { Month string; ReadAt time.Time; Actions, Packages, PackagesDaily float64; Over *float64 }` — `PackagesDaily` is the Packages GB-hours of the last full UTC day before `ReadAt`; `Over` is the summary's net `actions_storage` + `packages_storage`, nil when the summary failed.
- `func MonthBounds(t time.Time) (start, resets time.Time)` — UTC month of `t`.
- `func SumUsage(items []github.UsageItem, readAt time.Time) (actions, packages, packagesDaily float64)`.
- `func ComputeQuota(b Billing, artifactBytes int64, includedMB int, warn [2]int) model.GitHubQuota` — spec §2.2 and §2.4; computes `RunsOutAt` only when the hours to run out are below the hours left (no Duration overflow).
- `func AlertFrom(q *model.GitHubQuota) *model.StorageAlert` — nil for a nil quota; `UsedPct` is `Used / Included × 100` rounded.

### Service (Tasks 6 to 9, package `ghstorage`)

- Task 6, `internal/ghstorage/cleanup.go`, pure selection, no GitHub calls:
  - `type Pick struct { Repo string; Item model.ArtifactItem }`
  - `func Candidates(items []model.ArtifactItem, age time.Duration, now time.Time, active map[int64]bool) []model.ArtifactItem` — older than `age` by `CreatedAt`, run not in `active`.
  - `func Oldest(picks []Pick, n int) []Pick` — oldest `CreatedAt` first, ties by repo then id, at most `n`.
  - `func Preview(repos []model.RepoStorage, cfg config.GitHubStorage, repo string, age time.Duration, now time.Time) []model.StoragePreview` — with `repo`, that repository with `age`; without, every repository that has no override; `age` 0 gives `[]`; only rows with a count above 0, largest bytes first.
  - `var ActiveStatuses = []string{"queued", "in_progress", "waiting", "requested", "pending"}`.
- Task 7, `internal/ghstorage/service.go`:
  - `type GitHub interface` with exactly: `OwnerType`, `TokenLogin`, `StorageUsage`, `StorageSummary`, `ListRepoArtifacts`, `ListCaches`, `DeleteArtifact`, `DeleteCache`, `Retention`, `SetRetention` (Task 1 signatures), `ListUserRepos(ctx context.Context) ([]github.UserRepo, error)`, `ListRuns(ctx context.Context, repo, status string) ([]github.Run, error)`, `TokenMeta() github.TokenMeta`, `SuspendedUntil() time.Time`, `RateRemaining() int`. `*github.Client` satisfies it.
  - `type Service struct { GH GitHub; Config func() *config.Config; Events *events.Ring; Now func() time.Time; NewID func() string; Describe func(err error, repo string) string; Wake func() }` plus unexported state. `Now` nil is `time.Now`, `NewID` nil makes random ids, `Describe` nil is `err.Error()`, `Wake` nil is nothing.
  - Methods: `Start()`, `Close()`, `Wait()`, `Kick()`, `Refresh() error`, `TokenChanged()`, `PolicyChanged()`, `Snapshot() model.GitHubStorage`, `Alert() *model.StorageAlert`, `Version() int64`.
  - `const InventoryInterval = 15 * time.Minute`, `BillingInterval = time.Hour`, `Workers = 4`, `MinRateLeft = 500`, `MaxDeletions = 200`.
  - Errors: `var ErrBusy = errors.New("a GitHub storage check is already running")`, `ErrClosed = errors.New("ghr is shutting down")`.
  - `Snapshot()` fills each row's `Cleanup`, `CleanupSource` and `Covered` from the current config.
  - `Version()` rises on every snapshot change; `Wake` is called after it rises.
- Task 8, `internal/ghstorage/cleanup_run.go`: `const MaxCleanupDeletes = 100`, `WriteBudget = 300`; `type BudgetError struct { RetryAt time.Time }` (`Error()`: `GitHub write budget used up; try again after <RFC 3339 time>`). Log line and event texts: `github storage: deleted artifact <name> (<size>) in <repo>, age <age>`; event `cleanup deleted <n> artifacts (<size>)` with level `info` and the repository set.
- Task 9, `internal/ghstorage/ops.go`:
  - `var ErrUnknownRepo = errors.New("not in the last GitHub storage inventory")`, `ErrOperationRunning = errors.New("another GitHub storage operation is running")`.
  - `type BadInput string` (an error; the daemon answers 400 with its text).
  - `Preview(cleanup, repo string) ([]model.StoragePreview, error)`, `DeleteArtifact(ctx context.Context, repo string, id int64) error`, `DeleteCache(ctx context.Context, repo string, id int64) error`, `DeleteArtifacts(repo string, req model.DeleteArtifactsRequest) (string, error)`, `DeleteCaches(repo string, req model.DeleteCachesRequest) (string, error)`, `SetRetention(ctx context.Context, repo string, days int) error`, `SetRetentionAll(days int) (string, error)`. The string results are operation ids.

### ghstorage internals (Tasks 7 to 9)

- Unexported names in `service.go` (Task 7): `s.init()` sets up the context, the kick channel and the maps without starting the loop (`Start` calls it); `s.pass(inventory, billing bool)` runs one pass; `s.keep(gen uint64) bool` is false once the service has closed or the token generation has changed; `s.describe(err error, repo string) string`; `s.bump()` (may be called under `s.mu`); `s.recompute(cfg *config.Config)` (needs `s.mu`); `s.removeItem(kind, repo string, id int64)` (needs `s.mu`; `kind` is `"artifact"` or `"cache"`); `s.kick` is a `chan struct{}` with a buffer of 1. `billingDelay(now time.Time) time.Duration` is an hour, or one minute after the UTC month changes when that is sooner.
- Unexported names in `cleanup_run.go` (Task 8): `s.write(fn func() error) error`; `s.activeRuns(ctx, repo) (map[int64]bool, error)`; `s.deleteArtifact(ctx, repo, item model.ArtifactItem, by string) (bool, error)`; `s.recordDeletion(d model.Deletion)` (needs `s.mu`); `s.setRowError(repo, msg string, gen uint64)`; `sizeText(int64) string` (`"0 B"`, `"2 KB"`, `"340 MB"`, `"1.5 GB"`); `ageText(time.Duration) string` (`"59m"`, `"30h"`, `"2d"`).
- Test helpers, package-internal (not usable from `internal/daemon`):
  - `fake_test.go` (Task 7): `newFakeGH() *fakeGH`, whose maps are keyed by repository, with `isOrg`, `login`, `usage`/`usageErr` and `summary`/`sumErr`; it records calls as `"Method repo [id]"`, read with `callsLike(prefix) []string`, and has the hooks `onList(ctx, repo) error`, `onDelete(ctx, repo, id) error` and `onUsage(ctx) error` (runs at the start of `StorageUsage`; an error it returns is the call's answer). `(*fakeGH).addRepo(repo string, private bool, arts ...github.Artifact)` sets retention 90 of 400 days. `ghArt(id, run int64, at time.Time, age time.Duration, bytes int64) github.Artifact`. `fakeClock` with `Now()` and `Add(d)`. `newTestService(t, gh, cfg) (*Service, *fakeClock)` makes a service with no loop, a clock at 2026-10-13 12:00 UTC and operation ids `op-1`, `op-2`, ...; `s.Wait()` waits for operations. `testConfig()` (owner `darkraise`), `row(t, st, repo)`, `eventsLike(ev, sub)`.
  - `service_test.go` (Task 7): `forbidden` (a 403 `*github.APIError`), `blockList(gh, repo)`. `cleanup_run_test.go` (Task 8): `cleanupConfig()`, `artifactIDs(row)`. `ops_test.go` (Task 9): `blockDelete(gh)`.
- Texts later tasks show:
  - Level events: `github storage: quota <level>: <used %.1f> of <included %.1f> GB-hours used in <YYYY-MM>` (level `error` for exhausted and critical, `warn` otherwise); back to no level: `github storage: back under the warning levels for <YYYY-MM>` (level `ok`).
  - Row error for an oversized listing: `<repo> has more than 5,000 artifacts or caches, more than ghr lists: lower its retention`.
  - Operation-finished events (repository set where there is one): `deleted <n> artifacts (<size>)`, `deleted <n> caches (<size>)`, `set retention to <d> days on <n> of <total> repositories`; `; <k> failed` is appended at level `warn` when anything failed, otherwise level `info`.
  - Operation failures: `not in the last inventory`, `<repo> allows at most <max> days`.
  - `BadInput` texts: `give either ids or older_than`, `older_than must be a duration of at least 1h, such as 24h`, `give either ids or all`, `days must be from 1 to <max>`, `cleanup must be off or a duration such as 2d`, or GitHub's own 422 message.
  - Log lines: `github storage: deleted cache <key> (<size>) in <repo>, age <age>` (manual deletes log the artifact line too); `github storage: cleanup stopped: <budget error>` (a spent write budget stops the cleanup with this log line only: no event, no row error).

### Daemon and API (Tasks 10 to 13)

- `daemon.GitHubStorageService` (Task 10, `internal/daemon/ghstorage.go`): an interface of every exported `*ghstorage.Service` method above except `Start`, `Close` and `Wait`. `Backend` gains `GHS GitHubStorageService`.
- Backend methods (Task 10), also added to `api.Backend` (Task 12):
  - `GitHubStorage() model.GitHubStorage`
  - `RefreshGitHubStorage() error`
  - `GitHubStoragePreview(cleanup, repo string) ([]model.StoragePreview, error)`
  - `DeleteGitHubArtifact(ctx context.Context, repo string, id int64) error`
  - `DeleteGitHubCache(ctx context.Context, repo string, id int64) error`
  - `DeleteGitHubArtifacts(repo string, req model.DeleteArtifactsRequest) (model.OperationStarted, error)`
  - `DeleteGitHubCaches(repo string, req model.DeleteCachesRequest) (model.OperationStarted, error)`
  - `SetGitHubRetention(ctx context.Context, repo string, days int) error`
  - `SetGitHubRetentionAll(days int) (model.OperationStarted, error)`
- Error mapping (Task 10): `ErrUnknownRepo` → 404; `ErrBusy` and `ErrOperationRunning` → 409; `*BudgetError` → 429 with `RetryAt`; `BadInput` → 400; `ErrClosed` → 503. Every method that calls GitHub runs `b.githubErr()` first.
- Backend hooks (Task 10, `backend.go`): `Status()` sets `GitHubStorage` from `b.GHS.Alert()`; `SetToken` calls `b.GHS.TokenChanged()`; `Reload` calls `b.GHS.TokenChanged()` when the token file changed and `b.GHS.Kick()` otherwise; a `PatchConfig` with `GitHubStorage` set calls `b.GHS.PolicyChanged()`; each only when `b.GHS` is not nil.
- Test helpers (Task 10, `internal/daemon/ghstorage_test.go`): `fakeGHS` records calls as `"<method> <args>"`, read with `recorded()`, and has the fields `snap`, `alert`, `preview`, `err` and `version` (an `atomic.Int64`); `newGHSBackend(t) (*Backend, *fakeManager, *fakeGHS)`.
- Wiring (Task 11): the `status` fingerprint sets the alert; the change topic `github-storage` counts `b.GHS.Version` and is present only when `b.GHS` is set (so `TestFingerprintsCoverEveryTopic` is unchanged). `run.go` builds the service in `start` (`Config: store.Config`, `GH: gh`, `Events: ev`, `Wake: hub.Wake`, `Describe: storageErrText(owner)`), starts it, and closes and waits for it at shutdown. `storageErrText(owner string) func(err error, repo string) string` uses `actionsErr` for a repository and GitHub's own text when `repo` is empty.
- Routes (Task 12), spec §3.2: `GET /github-storage`, `POST /github-storage/refresh`, `GET /github-storage/preview`, `POST /github-storage/retention`, `POST /github-storage/{repo}/artifacts/delete`, `DELETE /github-storage/{repo}/artifacts/{id}`, `POST /github-storage/{repo}/caches/delete`, `DELETE /github-storage/{repo}/caches/{id}`, `PUT /github-storage/{repo}/retention`. Bulk routes answer 202 with `model.OperationStarted`. A new `started(w)` helper in `server.go` writes it. The preview never answers `null`. A bad body answers 400 `invalid JSON: ...`; a bad path id answers 400 `artifact id must be a positive integer` or `cache id must be a positive integer`. Test helpers in `internal/api/ghstorage_test.go`: `ghsFake` (held in `fakeBackend.ghs`) and `ghsCall(t, srv, method, path, body) (int, string)`.
- CLI line (Task 13): `artifacts  <used> / <included> GB-h this month (<pct>%)` in whole GB-hours, then `, exhausted until <Mon D>` when exhausted, otherwise `, critical` or `, warning` for those levels and `, runs out around <Mon D>` when `runs_out_at` is set; dates in UTC.

### Web (Tasks 14 to 24)

- Types (Task 14, `web/src/api/types.ts`), mirroring the Go JSON: `GitHubStorage`, `GitHubQuota`, `QuotaLevel = "" | "projected" | "warn" | "critical" | "exhausted"`, `RepoStorage`, `ArtifactItem`, `CacheItem`, `GitHubOperation`, `OperationFailure`, `Deletion`, `StorageAlert`, `GitHubStoragePatch`, `StoragePreview`, `GitHubStorageConfig` (`{ included_mb?: number; warn_pct?: number[]; cleanup_max_age?: string; repos?: Record<string, { cleanup: string }> }`); `Config.github_storage?: GitHubStorageConfig`, `Status.github_storage?: StorageAlert`, `ConfigPatch.github_storage?: GitHubStoragePatch`. Fixture `web/src/api/fixtures/github-storage.json`, exported from `web/src/test/fixtures.ts` as `fixtures.githubStorage`; `authedRoutes` gains `"GET /api/github-storage"`.
- Client (Task 15, `api` in `web/src/api/client.ts`): `githubStorage(signal?)`, `refreshGitHubStorage()`, `storagePreview(cleanup: string, repo?: string, signal?: AbortSignal)`, `deleteArtifact(repo, id)`, `deleteCache(repo, id)`, `deleteArtifacts(repo, body: { ids: number[] } | { older_than: string })`, `deleteCaches(repo, body: { ids: number[] } | { all: true })`, `setRetention(repo, days)`, `setRetentionAll(days)`. Repository names are path-escaped with `encodeURIComponent`.
- Hook (Task 16, `web/src/api/hooks.ts`): `keys.githubStorage = ["github-storage"] as const`; `useGitHubStorage()` polls with `POLL_FAST` (1000 ms, `web/src/api/hooks.ts:6`) while busy and `POLL_SLOW` (5000 ms, `hooks.ts:7`) otherwise, through `livePoll` while the change stream is live; `githubStorageBusy(storage?: GitHubStorage): boolean` is true while an operation has no `finished_at`. `TOPIC_QUERIES` maps `"github-storage"` to `[keys.githubStorage]` and adds `keys.githubStorage` to `config`.
- Text helpers (Task 17, `web/src/lib/quota.ts`): `gbh(n: number): string`, `utcDay(iso: string): string` (`"Oct 24"`, UTC), `bannerText(alert: StorageAlert): string`, `canDismiss(level: string): boolean`, `isDismissed(alert: StorageAlert): boolean`, `dismiss(alert: StorageAlert): void`, and the texts `CLEANUP_WARNING` and `RETENTION_WARNING`.
- Components: `ArtifactsPage` (Task 18, `web/src/pages/artifacts.tsx`, route `/artifacts`); `QuotaBanner({ alert })` (Task 19, `web/src/components/quota-banner.tsx`, with the Artifacts tab); `QuotaCard` (Task 20, `components/quota-card.tsx`); `StoragePolicyCard` (Task 21, `components/storage-policy-card.tsx`); `ArtifactRepoTable` (Task 22, `components/artifact-repo-table.tsx`); `ArtifactRepoDetail` (Task 23, `components/artifact-repo-detail.tsx`); `OperationProgress` and `RecentDeletions` (Task 24, `components/storage-operations.tsx`).

- Types (Task 14): `level`, `kind`, `cleanup_source` and `by` are typed `string`, not unions: fixtures imported from JSON read them as `string`, as `Operation.kind` already does. `QuotaLevel` is exported to document the values.
- Fixtures (Task 14), which later web tests rely on:
  - `fixtures.githubStorage` is at level `warn`, `checked_at` 2026-10-03T14:00Z, `runs_out_at` Oct 24 and `resets_at` Nov 1. Its repositories, in order:
    - `darkmem`: private, covered, override `6h`, retention 90 of 400. Artifacts 9001 `frontend-dist` (run 102, `master`) and 9002 `coverage` (run 103, `feature/x`); one cache, id 8787967584.
    - `darkcloud`: private, covered, global `2d`, retention 7 of 90. Artifact 9101 `dist` (run 501, `main`); no caches.
    - `sandbox`: public, not covered, global `2d`, retention unknown (0 of 0), no artifacts or caches, error `token lacks actions=read on darkraise/sandbox: grant it in the PAT settings`.
  - Its operation `op-1` (`delete-artifacts`, darkmem, 3 of 3) is finished, with one failure: id 9003, `not in the last inventory`. It has two deletions: an artifact by cleanup and a cache deleted by hand.
  - `fixtures.storageAlert` is level `critical`, `used_pct` 91, `runs_out_at` 2026-10-06T02:05Z. `fixtures.config.github_storage` is `{cleanup_max_age: "2d", repos: {darkmem: {cleanup: "6h"}}}`. `fixtures.status` has no alert.
- Texts (Task 17): banner `GitHub artifact storage for October is used up: uploads fail until Nov 1.` (exhausted); `GitHub artifact storage is at 91% of October's allowance and runs out around Oct 6.` (warn and critical; the "and runs out" part only when `runs_out_at` is set); `GitHub artifact storage runs out around Oct 24, before the month resets on Nov 1.` (projected). The dismissal key is `ghr-quota-dismissed:<month>:<level>`. `RETENTION_WARNING` is `GitHub's retention also limits how long it keeps runs and logs, which ghr's Actions and log pages read.`
- Banner (Task 19): link `Go to Artifacts`, a `Dismiss` button (dismissing also hides it in memory for the visit); the shell renders it after the restart or listen banner. The tab is `Artifacts`, with lucide `Archive`, after History. Banner tests clear `localStorage` in `beforeEach`; the test setup does not.
- Page composition (Task 18): `ArtifactsPage` has a `Check now` button that toasts `Check started`; its description is `Checked <shortWhen>` or `Not checked yet`. The page holds `data` (`storage.data`), `offline` (`status.isError`), the `refresh` mutation, `queryClient` and `now`. Tasks 20 to 24 add any other hook they need (`useConfig()`, `githubStorageBusy(data)`) and one line each after `{!data && !storage.isError && <Spinner label="Loading" />}`, in this order: `{data && <QuotaCard storage={data} />}`, `StoragePolicyCard`, `OperationProgress`, `ArtifactRepoTable`, `RecentDeletions`. `ArtifactRepoDetail` renders inside `ArtifactRepoTable`'s expanded row, so Task 23 modifies `artifact-repo-table.tsx`.

- Cards (Tasks 20 to 24, all in `web/src/components/`): `QuotaCard({ storage })`; `StoragePolicyCard({ storage, config, offline, busy })` and `CleanupPreview({ age, previews })` (Task 21; `CleanupPreview` is the confirmation detail of a cleanup age, reused by Task 22, and is a component because `react-refresh/only-export-components` refuses other exports from a component file); `ArtifactRepoTable({ storage, config, offline })` (Task 22; it works out `busy` itself with `githubStorageBusy(storage)`); `ArtifactRepoDetail({ repo, owner, busy, offline, ask })` (Task 23; `ask: (c: Confirm) => void` is the table's one `ConfirmDialog`; the repository name in each row is the expand button, with `aria-expanded`); `OperationProgress({ operation })` and `RecentDeletions({ deletions })` (Task 24). Region names: `Quota`, `Policy`, `Operation`, `Repositories`, `Recent deletions`, `Artifacts of <repo>`, `Caches of <repo>`. Final page order: QuotaCard, StoragePolicyCard, OperationProgress, ArtifactRepoTable, RecentDeletions.

## Assumptions (evidence)

- GitHub's answers, read with `gh api` on 2026-10-13: `GET /users/darkraise/settings/billing/usage?year=2026&month=10` returns `{"usageItems": [...]}` whose storage items carry `date` (`2026-10-01T00:00:00Z`, one per day), `sku` (`Actions storage`, `Packages storage`, `Git LFS storage`), `unitType` `GigabyteHours`, a fractional `quantity`, and a bare `repositoryName` (empty for Packages). `.../usage/summary?year=2026&month=10` returns `usageItems` with `sku` `actions_storage` / `packages_storage`, `unitType` `gigabyte-hours`, and `grossQuantity`, `discountQuantity`, `netQuantity`. `GET /repos/darkraise/ghr/actions/caches` returns `actions_caches` with `id`, `ref`, `key`, `size_in_bytes`, `last_accessed_at`, `created_at`. `GET /repos/darkraise/ghr/actions/permissions/artifact-and-log-retention` returns `{"days":90,"maximum_allowed_days":90}`. `GET /users/darkraise` has `"type":"User"`.
- `events.Ring.Add` releases its lock before calling its change hook (`internal/events/events.go:26-40`), so the service may add events while holding its own lock.
- `actionsErr(err, owner, repo)` (`internal/daemon/actions.go:66`) gives the repository wording the spec asks for ("token lacks <permission> on <owner>/<repo>: grant it in the PAT settings"); the daemon passes it to the service as `Describe`.
- `Store.Config()` returns `*config.Config` (`internal/daemon/store.go:54`).
- The daemon builds the full backend only once it is configured (`internal/daemon/run.go` `start`), so an unconfigured daemon runs no passes without extra code.
- Beyond the spec: `model.GitHubStorage.Error` holds a failed repository listing, which has no row to show on.
- F18 (no resurrection after a manual delete) is kept as a set of ids deleted in the last hour that every listing merge filters out, instead of a per-repository generation: the same behaviour with less state.
- The spec's per-repository and per-day billing totals (§2.1) have no field in the snapshot and nothing reads them, so the billing read keeps only the sums the quota needs.

- YAML reads an unquoted `cleanup: off`, as in the spec's example, into the string `off`: `TestGitHubStorageRoundTrips` (Task 3) parses that example and passed on 2026-10-13.
- Inference, not observed: on the 1st of a month the last full UTC day belongs to the previous month, which this month's usage read does not hold, so Packages add nothing to the rate on the 1st.

- The whole web suite (96 files, 1,319 tests) passed with the config fixture's new `github_storage` block on 2026-10-13; after Task 19 the accessibility, app, router and anti-slop tests passed with the new tab (191 tests).
- Prettier is not enforced: `client.ts` and `client-pages.test.ts` failed `prettier --check` before any change, so the gates are `tsc` and `eslint`.

- `go test -race` works on the dev host (`CGO_ENABLED=1`, gcc at `/usr/bin/gcc`): the `ghstorage` package passed `-race -count=20` on 2026-10-13.
- A repository whose retention cannot be read (no Administration: read on a repository outside the configured ones) shows that error on its row; its artifacts and caches are still listed, and the read is retried once a day.
- Event texts say `<n> artifacts` even for 1, as the Contracts word them.

- A throwaway end-to-end check on 2026-10-13 (not part of the plan): a real `Run` against the test fake GitHub served `GET /github-storage` over the socket with `repos: []`, answered `GET /github-storage/preview?cleanup=2d` with `200 []`, and `POST /github-storage/nope/artifacts/delete` with `404 not in the last GitHub storage inventory`. It also found that a failed repository listing read `token cannot see darkraise/: add it to the PAT's repository access first`, which `storageErrText` (Task 11) fixes.
- `hub.Wake` never blocks (`internal/changes/hub.go:97-104`), so the service may call `Wake` under its own lock. No test proves the `Wake: hub.Wake` wiring: runner events in the test daemon also wake the hub, and without it the hub's quarter-second check still publishes the topic.
- Accepted: an unknown artifact or cache id in a known repository also answers 404 `not in the last GitHub storage inventory`, which does not say whether the repository or the item is missing.

- Sizes on the page use the existing `humanBytes` (decimal kB, MB, GB, `web/src/lib/format.ts`), like the disk and caches pages; the 2^30 rule in Global Constraints applies to GB-hours only.
- Component tests that show times seed the status query with `fixtures.status`, because `useNow` runs on the daemon's clock (`web/src/lib/use-now.ts`).
- The repository row's Delete older counts come from `GET /github-storage/preview?cleanup=<age>&repo=<repo>`, which uses the same age test as `older_than` (Task 9), so the dialog says "Up to".
- UI texts the spec does not give were chosen by Tasks 17 to 24 and are given verbatim in those tasks; the `ghr status` wording for levels other than exhausted is Task 13's.
- Each task's code was built and tested on 2026-10-13 in throwaway worktrees, in plan order, from `a394778`: every Step 2 failed as written and every Step 4 passed; each chain's replace blocks were replayed onto its parent commit and reproduced the tested files byte for byte. On the combined branch (Tasks 1 to 25) `go test ./...` passed in every package, gofmt and `go vet` were clean, the web suite passed (104 files, 1,413 tests), tsc and eslint printed nothing, and `deploy/tests/setup_test.sh` exited 0.
- Deviation, accepted: a hand-written `included_mb: 0` loads as the default 500, because 0 is how `omitempty` writes "absent"; `Validate` rejects only negative values (Task 3). The patch (Task 4) refuses 0, so the web form cannot send it. The spec's "rejects included_mb below 1" holds for every value except this one.
- Deviation, accepted: the billing read after the month change runs at `ResetsAt` + 1 minute (`billingDelay`, Task 7), and the old quota is cleared when that pass starts. For up to a minute after 00:00 UTC on the 1st, the banner may still show last month's level.
- Task 26's environment: the daemon's socket is `/run/ghr/ghr.sock` (`api.DefaultSocket`, `internal/api/client.go:19`) and its config `/etc/ghr/config.yaml` (`internal/daemon/run.go:91`); `ghr version` exists (`cmd/ghr/main.go:72`); the register script is `scripts/register` in the dr-superpowers plugin directory, used the same way by `docs/superpowers/plans/2026-10-13-ghr-ui-config-items.md` Task 16; the LXC's key-based SSH and `python3` were used there on 2026-10-13.

## Task index

1. Add the GitHub storage calls
2. Add the storage model types
3. Add the github_storage config block
4. Apply the github_storage patch
5. Compute the quota and warning level
6. Select cleanup candidates
7. Run the storage passes
8. Run the cleanup within a write budget
9. Run manual deletes and retention changes
10. Serve the storage service from the backend
11. Wire the service into the daemon
12. Add the github-storage routes
13. Show the quota in ghr status
14. Add the storage types and fixture
15. Add the storage API calls
16. Add the storage query and change topic
17. Add the quota text helpers
18. Add the Artifacts page
19. Show the quota banner and the Artifacts tab
20. Show the quota card
21. Edit the storage policy
22. List the repositories
23. Manage a repository's artifacts and caches
24. Show operation progress and deletions
25. Document the storage settings
26. Run every suite and check on the LXC

---

### Task 1: Add the GitHub storage calls

**Files:**
- Create: `internal/github/storage.go`
- Modify: `internal/github/runs.go` (`Artifact`, new `ArtifactRun`)
- Test: `internal/github/storage_test.go` (new)
- Test: `internal/github/client_test.go` (`TestListingStopsAfter50Pages`: two rows, the fake body, the error check)

**Interfaces:**
- Consumes: nothing
- Produces: everything under Contracts **GitHub client (Task 1)**: `Artifact.CreatedAt`, `Artifact.WorkflowRun`, `ArtifactRun`, `Cache`, `UsageItem`, `SummaryItem`, `Retention`, `SKUActionsStorage`, `SKUPackagesStorage`, `NormalizeSKU`, `IsTooManyPages`, and the `*Client` methods `OwnerType`, `TokenLogin`, `StorageUsage`, `StorageSummary`, `ListRepoArtifacts`, `ListCaches`, `DeleteArtifact`, `DeleteCache`, `Retention`, `SetRetention`.

**Items:** 1, 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

The calls follow the existing client: reads go through `getJSON` or `getAll` (ETag revalidation, the 50-page limit), writes through `mutate` (one at a time, one second apart). The usage endpoint spells a SKU `Actions storage` and its unit `GigabyteHours`; the summary spells them `actions_storage` and `gigabyte-hours`, hence `NormalizeSKU`. Both shapes were read from GitHub on 2026-10-13 (see Assumptions); the test bodies are trimmed copies of those answers.

- [ ] **Step 1: Write the failing tests**

Before the first command of the session run `mkdir -p /root/.cache/ghr-plan-tmp`.

Create `internal/github/storage_test.go`:

```go
package github

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNormalizeSKU(t *testing.T) {
	for in, want := range map[string]string{
		"Actions storage":  SKUActionsStorage,
		"actions_storage":  SKUActionsStorage,
		"Packages storage": SKUPackagesStorage,
		"GigabyteHours":    "gigabytehours",
		"gigabyte-hours":   "gigabytehours",
	} {
		if got := NormalizeSKU(in); got != want {
			t.Errorf("NormalizeSKU(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOwnerTypeAndTokenLogin(t *testing.T) {
	kind := "User"
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/darkraise":
			fmt.Fprintf(w, `{"login":"darkraise","type":%q}`, kind)
		case "/user":
			fmt.Fprint(w, `{"login":"someone"}`)
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	if isOrg, err := c.OwnerType(ctx); err != nil || isOrg {
		t.Fatalf("user owner: isOrg %v err %v", isOrg, err)
	}
	kind = "Organization"
	if isOrg, err := c.OwnerType(ctx); err != nil || !isOrg {
		t.Fatalf("organization owner: isOrg %v err %v", isOrg, err)
	}
	if login, err := c.TokenLogin(ctx); err != nil || login != "someone" {
		t.Fatalf("login %q err %v", login, err)
	}
	if got := strings.Join(f.requests, "|"); got != "GET /users/darkraise|GET /users/darkraise|GET /user" {
		t.Fatalf("requests %q", got)
	}
}

const usageBody = `{"usageItems":[
{"date":"2026-10-01T00:00:00Z","product":"actions","sku":"Actions storage","quantity":5.745991992,"unitType":"GigabyteHours","repositoryName":"chicken-farm"},
{"date":"2026-10-02T00:00:00Z","product":"packages","sku":"Packages storage","quantity":0.185828452,"unitType":"GigabyteHours","repositoryName":""},
{"date":"2026-10-02T00:00:00Z","product":"git_lfs","sku":"Git LFS storage","quantity":1.6,"unitType":"GigabyteHours","repositoryName":"darkfoto"},
{"date":"2026-10-02T00:00:00Z","product":"actions","sku":"Actions Linux","quantity":120,"unitType":"Minutes","repositoryName":"darkmem"}]}`

const summaryBody = `{"timePeriod":{"year":2026,"month":10},"user":"darkraise","usageItems":[
{"product":"Actions","sku":"actions_linux","grossQuantity":1538.0,"discountQuantity":1538.0,"netQuantity":0.0,"unitType":"minutes"},
{"product":"Actions","sku":"actions_storage","grossQuantity":373.317530205,"discountQuantity":373.315932471,"netQuantity":0.001597734,"unitType":"gigabyte-hours"},
{"product":"Git LFS","sku":"git_lfs_storage","grossQuantity":26.5,"discountQuantity":26.5,"netQuantity":0.0,"unitType":"gigabyte-hours"},
{"product":"Packages","sku":"packages_storage","grossQuantity":11.155870203,"discountQuantity":11.155870203,"netQuantity":0.0,"unitType":"gigabyte-hours"}]}`

// A user owner's billing lives under /users, an organization's under
// /organizations; only Actions and Packages storage are kept.
func TestStorageUsageAndSummary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		isOrg  bool
		prefix string
	}{
		{"user", false, "/users/darkraise/settings/billing/usage"},
		{"organization", true, "/organizations/darkraise/settings/billing/usage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case tc.prefix:
					fmt.Fprint(w, usageBody)
				case tc.prefix + "/summary":
					fmt.Fprint(w, summaryBody)
				default:
					http.NotFound(w, r)
				}
			})
			ctx := context.Background()
			items, err := c.StorageUsage(ctx, tc.isOrg, 2026, time.October)
			if err != nil {
				t.Fatal(err)
			}
			want := []UsageItem{
				{Date: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), SKU: SKUActionsStorage, Repo: "chicken-farm", Quantity: 5.745991992},
				{Date: time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC), SKU: SKUPackagesStorage, Repo: "", Quantity: 0.185828452},
			}
			if len(items) != len(want) {
				t.Fatalf("items %+v", items)
			}
			for i := range want {
				if !items[i].Date.Equal(want[i].Date) || items[i].SKU != want[i].SKU || items[i].Repo != want[i].Repo || items[i].Quantity != want[i].Quantity {
					t.Fatalf("item %d = %+v, want %+v", i, items[i], want[i])
				}
			}
			sum, err := c.StorageSummary(ctx, tc.isOrg, 2026, time.October)
			if err != nil {
				t.Fatal(err)
			}
			wantSum := []SummaryItem{
				{SKU: SKUActionsStorage, Gross: 373.317530205, Discount: 373.315932471, Net: 0.001597734},
				{SKU: SKUPackagesStorage, Gross: 11.155870203, Discount: 11.155870203, Net: 0},
			}
			if len(sum) != 2 || sum[0] != wantSum[0] || sum[1] != wantSum[1] {
				t.Fatalf("summary %+v", sum)
			}
			wantReq := "GET " + tc.prefix + "?year=2026&month=10|GET " + tc.prefix + "/summary?year=2026&month=10"
			if got := strings.Join(f.requests, "|"); got != wantReq {
				t.Fatalf("requests %q, want %q", got, wantReq)
			}
		})
	}
}

func TestRepoArtifactsAndCachesPaginate(t *testing.T) {
	var base string
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		page := "1"
		if r.URL.Query().Get("page") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s%s&page=2>; rel="next"`, base, r.URL.RequestURI()))
		} else {
			page = "2"
		}
		if strings.HasSuffix(r.URL.Path, "/caches") {
			fmt.Fprintf(w, `{"total_count":2,"actions_caches":[{"id":%s,"ref":"refs/heads/master","key":"node-cache","size_in_bytes":71431401,"last_accessed_at":"2026-10-11T05:51:49.688101Z","created_at":"2026-10-11T05:33:45.524625Z"}]}`, page)
			return
		}
		fmt.Fprintf(w, `{"total_count":2,"artifacts":[{"id":%s,"name":"dist","size_in_bytes":2400000,"expired":false,"created_at":"2026-10-10T08:00:00Z","expires_at":"2027-01-08T08:00:00Z","workflow_run":{"id":102,"head_branch":"master"}}]}`, page)
	})
	base = c.BaseURL
	ctx := context.Background()
	arts, err := c.ListRepoArtifacts(ctx, "darkmem")
	if err != nil || len(arts) != 2 || arts[1].ID != 2 {
		t.Fatalf("artifacts %+v err %v", arts, err)
	}
	if a := arts[0]; a.Name != "dist" || a.SizeInBytes != 2400000 || !a.CreatedAt.Equal(time.Date(2026, 10, 10, 8, 0, 0, 0, time.UTC)) ||
		a.WorkflowRun != (ArtifactRun{ID: 102, HeadBranch: "master"}) {
		t.Fatalf("artifact %+v", a)
	}
	caches, err := c.ListCaches(ctx, "darkmem")
	if err != nil || len(caches) != 2 || caches[1].ID != 2 {
		t.Fatalf("caches %+v err %v", caches, err)
	}
	if k := caches[0]; k.Key != "node-cache" || k.Ref != "refs/heads/master" || k.SizeInBytes != 71431401 ||
		!k.LastAccessedAt.Equal(time.Date(2026, 10, 11, 5, 51, 49, 688101000, time.UTC)) ||
		!k.CreatedAt.Equal(time.Date(2026, 10, 11, 5, 33, 45, 524625000, time.UTC)) {
		t.Fatalf("cache %+v", k)
	}
	want := "GET /repos/darkraise/darkmem/actions/artifacts?per_page=100|GET /repos/darkraise/darkmem/actions/artifacts?per_page=100&page=2|" +
		"GET /repos/darkraise/darkmem/actions/caches?per_page=100|GET /repos/darkraise/darkmem/actions/caches?per_page=100&page=2"
	if got := strings.Join(f.requests, "|"); got != want {
		t.Fatalf("requests %q", got)
	}
}

// A 404 means someone else deleted the item first: not deleted, no error.
func TestDeleteArtifactAndCache(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		status  int
		del     func(c *Client) (bool, error)
		path    string
		deleted bool
		wantErr bool
	}{
		{"artifact deleted", 204, func(c *Client) (bool, error) { return c.DeleteArtifact(ctx, "darkmem", 7) }, "/repos/darkraise/darkmem/actions/artifacts/7", true, false},
		{"artifact already gone", 404, func(c *Client) (bool, error) { return c.DeleteArtifact(ctx, "darkmem", 7) }, "/repos/darkraise/darkmem/actions/artifacts/7", false, false},
		{"artifact refused", 403, func(c *Client) (bool, error) { return c.DeleteArtifact(ctx, "darkmem", 7) }, "/repos/darkraise/darkmem/actions/artifacts/7", false, true},
		{"cache deleted", 204, func(c *Client) (bool, error) { return c.DeleteCache(ctx, "darkmem", 9) }, "/repos/darkraise/darkmem/actions/caches/9", true, false},
		{"cache already gone", 404, func(c *Client) (bool, error) { return c.DeleteCache(ctx, "darkmem", 9) }, "/repos/darkraise/darkmem/actions/caches/9", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				if tc.status != 204 {
					fmt.Fprint(w, `{"message":"no"}`)
				}
			})
			c.Sleep = func(context.Context, time.Duration) error { return nil }
			deleted, err := tc.del(c)
			if deleted != tc.deleted || (err != nil) != tc.wantErr {
				t.Fatalf("deleted %v err %v", deleted, err)
			}
			if got := strings.Join(f.requests, "|"); got != "DELETE "+tc.path {
				t.Fatalf("requests %q", got)
			}
		})
	}
}

func TestRetention(t *testing.T) {
	var body string
	c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			b, _ := io.ReadAll(r.Body)
			body = string(b)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		fmt.Fprint(w, `{"days":90,"maximum_allowed_days":400}`)
	})
	c.Sleep = func(context.Context, time.Duration) error { return nil }
	ctx := context.Background()
	r, err := c.Retention(ctx, "darkmem")
	if err != nil || r != (Retention{Days: 90, MaxDays: 400}) {
		t.Fatalf("retention %+v err %v", r, err)
	}
	if err := c.SetRetention(ctx, "darkmem", 3); err != nil {
		t.Fatal(err)
	}
	if body != `{"days":3}` {
		t.Fatalf("body %q", body)
	}
	const path = "/repos/darkraise/darkmem/actions/permissions/artifact-and-log-retention"
	if got := strings.Join(f.requests, "|"); got != "GET "+path+"|PUT "+path {
		t.Fatalf("requests %q", got)
	}
}
```
In `internal/github/client_test.go`, in `TestListingStopsAfter50Pages`, replace

```go
		{"user repositories", 60, func(c *Client) (int, error) { v, err := c.ListUserRepos(ctx); return len(v), err }, 0, tooLong},
```

with

```go
		{"user repositories", 60, func(c *Client) (int, error) { v, err := c.ListUserRepos(ctx); return len(v), err }, 0, tooLong},
		{"repository artifacts", 60, func(c *Client) (int, error) { v, err := c.ListRepoArtifacts(ctx, "r"); return len(v), err }, 0, tooLong},
		{"caches", 60, func(c *Client) (int, error) { v, err := c.ListCaches(ctx, "r"); return len(v), err }, 0, tooLong},
```

Replace

```go
				fmt.Fprint(w, `{"workflow_runs":[{"id":1}],"jobs":[{"id":1}],"runners":[{"id":1}],"artifacts":[{"id":1}]}`)
```

with

```go
				fmt.Fprint(w, `{"workflow_runs":[{"id":1}],"jobs":[{"id":1}],"runners":[{"id":1}],"artifacts":[{"id":1}],"actions_caches":[{"id":1}]}`)
```

Replace

```go
			if err == nil || err.Error() != tc.wantErr || n != 0 {
				t.Fatalf("%d items, err %v", n, err)
			}
			if len(f.requests) != 50 {
```

with

```go
			if err == nil || err.Error() != tc.wantErr || !IsTooManyPages(err) || n != 0 {
				t.Fatalf("%d items, err %v", n, err)
			}
			if len(f.requests) != 50 {
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/github/
```
Expected: FAIL, the package does not build: `internal/github/storage_test.go:15:23: undefined: SKUActionsStorage`, followed by `c.OwnerType undefined (type *Client has no field or method OwnerType)` and more undefined names.

- [ ] **Step 3: Write the implementation**

In `internal/github/runs.go` replace

```go
type Artifact struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	SizeInBytes int64      `json:"size_in_bytes"`
	Expired     bool       `json:"expired"`
	ExpiresAt   *time.Time `json:"expires_at"`
}
```

with

```go
type Artifact struct {
	ID          int64       `json:"id"`
	Name        string      `json:"name"`
	SizeInBytes int64       `json:"size_in_bytes"`
	Expired     bool        `json:"expired"`
	ExpiresAt   *time.Time  `json:"expires_at"`
	CreatedAt   time.Time   `json:"created_at"`
	WorkflowRun ArtifactRun `json:"workflow_run"`
}

// ArtifactRun is the run that uploaded an artifact.
type ArtifactRun struct {
	ID         int64  `json:"id"`
	HeadBranch string `json:"head_branch"`
}
```

Create `internal/github/storage.go`:

```go
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The storage SKUs, normalized. The usage endpoint spells them "Actions
// storage", the summary "actions_storage".
const (
	SKUActionsStorage  = "actions_storage"
	SKUPackagesStorage = "packages_storage"
)

// NormalizeSKU lower-cases s, turns spaces into underscores and drops
// hyphens, so both endpoints' spellings of a SKU or a unit compare equal.
func NormalizeSKU(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(s), " ", "_"), "-", "")
}

func storageSKU(sku string) bool { return sku == SKUActionsStorage || sku == SKUPackagesStorage }

// Cache is one Actions cache entry.
type Cache struct {
	ID             int64     `json:"id"`
	Key            string    `json:"key"`
	Ref            string    `json:"ref"`
	SizeInBytes    int64     `json:"size_in_bytes"`
	LastAccessedAt time.Time `json:"last_accessed_at"`
	CreatedAt      time.Time `json:"created_at"`
}

// UsageItem is one day's storage of one SKU in one repository. Repo is
// empty for Packages.
type UsageItem struct {
	Date     time.Time
	SKU      string
	Repo     string
	Quantity float64
}

// SummaryItem is the month's storage of one SKU, split into the part the
// plan's allowance covers (Discount) and the part over it (Net).
type SummaryItem struct {
	SKU                  string
	Gross, Discount, Net float64
}

type Retention struct {
	Days    int `json:"days"`
	MaxDays int `json:"maximum_allowed_days"`
}

// IsTooManyPages reports whether a listing failed for being longer than
// 50 pages.
func IsTooManyPages(err error) bool { return errors.Is(err, errTooManyPages) }

// OwnerType reports whether the owner is an organization.
func (c *Client) OwnerType(ctx context.Context) (isOrg bool, err error) {
	var u struct {
		Type string `json:"type"`
	}
	if err := c.getJSON(ctx, c.BaseURL+"/users/"+url.PathEscape(c.Owner), &u); err != nil {
		return false, err
	}
	return u.Type == "Organization", nil
}

// TokenLogin is the login of the token's user.
func (c *Client) TokenLogin(ctx context.Context) (string, error) {
	var u struct {
		Login string `json:"login"`
	}
	if err := c.getJSON(ctx, c.BaseURL+"/user", &u); err != nil {
		return "", err
	}
	return u.Login, nil
}

func (c *Client) billingURL(isOrg bool, rest string, year int, month time.Month) string {
	kind := "users"
	if isOrg {
		kind = "organizations"
	}
	return fmt.Sprintf("%s/%s/%s/settings/billing/usage%s?year=%d&month=%d", c.BaseURL, kind, url.PathEscape(c.Owner), rest, year, int(month))
}

// StorageUsage lists the month's daily Actions and Packages storage in
// GB-hours. GitHub's schema says quantity is an integer; it is fractional.
func (c *Client) StorageUsage(ctx context.Context, isOrg bool, year int, month time.Month) ([]UsageItem, error) {
	var body struct {
		UsageItems []struct {
			Date           time.Time `json:"date"`
			SKU            string    `json:"sku"`
			Quantity       float64   `json:"quantity"`
			UnitType       string    `json:"unitType"`
			RepositoryName string    `json:"repositoryName"`
		} `json:"usageItems"`
	}
	if err := c.getJSON(ctx, c.billingURL(isOrg, "", year, month), &body); err != nil {
		return nil, err
	}
	var out []UsageItem
	for _, it := range body.UsageItems {
		sku := NormalizeSKU(it.SKU)
		if NormalizeSKU(it.UnitType) != "gigabytehours" || !storageSKU(sku) {
			continue
		}
		out = append(out, UsageItem{Date: it.Date, SKU: sku, Repo: it.RepositoryName, Quantity: it.Quantity})
	}
	return out, nil
}

// StorageSummary reads the month's Actions and Packages storage totals.
// The endpoint is a public preview.
func (c *Client) StorageSummary(ctx context.Context, isOrg bool, year int, month time.Month) ([]SummaryItem, error) {
	var body struct {
		UsageItems []struct {
			SKU      string  `json:"sku"`
			Gross    float64 `json:"grossQuantity"`
			Discount float64 `json:"discountQuantity"`
			Net      float64 `json:"netQuantity"`
		} `json:"usageItems"`
	}
	if err := c.getJSON(ctx, c.billingURL(isOrg, "/summary", year, month), &body); err != nil {
		return nil, err
	}
	var out []SummaryItem
	for _, it := range body.UsageItems {
		if sku := NormalizeSKU(it.SKU); storageSKU(sku) {
			out = append(out, SummaryItem{SKU: sku, Gross: it.Gross, Discount: it.Discount, Net: it.Net})
		}
	}
	return out, nil
}

// ListRepoArtifacts lists every artifact of the repository, expired ones
// included.
func (c *Client) ListRepoArtifacts(ctx context.Context, repo string) ([]Artifact, error) {
	var out []Artifact
	err := c.getAll(ctx, c.repoURL(repo, "/actions/artifacts?per_page=100"), func(b []byte) error {
		var page struct {
			Artifacts []Artifact `json:"artifacts"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		out = append(out, page.Artifacts...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) ListCaches(ctx context.Context, repo string) ([]Cache, error) {
	var out []Cache
	err := c.getAll(ctx, c.repoURL(repo, "/actions/caches?per_page=100"), func(b []byte) error {
		var page struct {
			Caches []Cache `json:"actions_caches"`
		}
		if err := json.Unmarshal(b, &page); err != nil {
			return err
		}
		out = append(out, page.Caches...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteArtifact deletes one artifact. An artifact already gone (404) is
// not deleted and not an error.
func (c *Client) DeleteArtifact(ctx context.Context, repo string, id int64) (deleted bool, err error) {
	return c.deleteItem(ctx, c.repoURL(repo, fmt.Sprintf("/actions/artifacts/%d", id)))
}

// DeleteCache deletes one cache entry, as DeleteArtifact does an artifact.
func (c *Client) DeleteCache(ctx context.Context, repo string, id int64) (deleted bool, err error) {
	return c.deleteItem(ctx, c.repoURL(repo, fmt.Sprintf("/actions/caches/%d", id)))
}

func (c *Client) deleteItem(ctx context.Context, u string) (bool, error) {
	_, err := c.mutate(ctx, http.MethodDelete, u, nil)
	if IsKind(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func (c *Client) Retention(ctx context.Context, repo string) (Retention, error) {
	var r Retention
	err := c.getJSON(ctx, c.repoURL(repo, "/actions/permissions/artifact-and-log-retention"), &r)
	return r, err
}

func (c *Client) SetRetention(ctx context.Context, repo string, days int) error {
	_, err := c.mutate(ctx, http.MethodPut, c.repoURL(repo, "/actions/permissions/artifact-and-log-retention"), map[string]int{"days": days})
	return err
}
```
- [ ] **Step 4: Run the tests to verify they pass**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/github/
```
Expected: `ok  	github.com/darkraise/ghr/internal/github`. The daemon builds `model.RunArtifact` from `github.Artifact` by field name (`internal/daemon/rundetail.go:323`), so the new fields change nothing there.

- [ ] **Step 5: Format, vet and commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./internal/... ./cmd/...
```
Expected: `gofmt` prints nothing and `go vet` passes.

```bash
git add internal/github/storage.go internal/github/storage_test.go internal/github/runs.go internal/github/client_test.go
git commit -m "feat(github): add artifact storage calls"
```

### Task 2: Add the storage model types

**Files:**
- Create: `internal/model/ghstorage.go`
- Modify: `internal/model/model.go` (`Status`, `ConfigPatch`)
- Test: `internal/model/ghstorage_test.go` (new)

**Interfaces:**
- Consumes: nothing
- Produces: everything under Contracts **Model (Task 2)**: `GiB`, `GitHubStorage`, `GitHubQuota`, `RepoStorage`, `ArtifactItem`, `CacheItem`, `GitHubOperation`, `OperationFailure`, `Deletion`, `StorageAlert`, `GitHubStoragePatch`, `StoragePreview`, `DeleteArtifactsRequest`, `DeleteCachesRequest`, `RetentionRequest`, `OperationStarted`; `Status.GitHubStorage`, `ConfigPatch.GitHubStorage`.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

Types only. The test pins every JSON key, because the web types (Task 14) and the fixture mirror them. `Status.GitHubStorage` and `ConfigPatch.GitHubStorage` are `omitempty`, so every existing status and patch keeps its JSON; `go test ./web/` proves the fixtures still match.

- [ ] **Step 1: Write the failing tests**

Create `internal/model/ghstorage_test.go`:

```go
package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGitHubStorageJSON(t *testing.T) {
	now := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	over := 0.0016
	st := GitHubStorage{
		CheckedAt: &now,
		Quota: &GitHubQuota{Month: "2026-10", ReadAt: now, IncludedGBHours: 363.28, UsedGBHours: 384.5, ActionsGBHours: 373.3,
			PackagesGBHours: 11.2, RateGBHoursPerHour: 0.17, EstimatedGBHours: 386.54, OverAllowanceGBHours: &over,
			ProjectedGBHours: 474.5, Level: "exhausted", ResetsAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		QuotaError: "q",
		Error:      "e",
		Repos: []RepoStorage{{Name: "darkmem", Visibility: "private", Covered: true, ArtifactCount: 1, ArtifactBytes: 2, CacheCount: 1, CacheBytes: 3,
			RetentionDays: 90, RetentionMaxDays: 400, Cleanup: "2d", CleanupSource: "global",
			Artifacts: []ArtifactItem{{ID: 7, Name: "dist", Bytes: 2, CreatedAt: now, ExpiresAt: &now, RunID: 102, Branch: "master"}},
			Caches:    []CacheItem{{ID: 9, Key: "node", Ref: "refs/heads/master", Bytes: 3, LastUsedAt: now}},
			Error:     "r"}},
		Operation: &GitHubOperation{ID: "o1", Kind: "delete-artifacts", Repo: "darkmem", Total: 2, Done: 1, Bytes: 2,
			Failed: []OperationFailure{{Repo: "darkmem", ID: 8, Error: "not in the last inventory"}}, StartedAt: now, FinishedAt: &now},
		Deletions: []Deletion{{At: now, Repo: "darkmem", Kind: "artifact", Name: "dist", Bytes: 2, Age: "3d", By: "cleanup"}},
	}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"checked_at":"2026-10-13T09:00:00Z",` +
		`"quota":{"month":"2026-10","read_at":"2026-10-13T09:00:00Z","included_gb_hours":363.28,"used_gb_hours":384.5,"actions_gb_hours":373.3,` +
		`"packages_gb_hours":11.2,"rate_gb_hours_per_hour":0.17,"estimated_gb_hours":386.54,"over_allowance_gb_hours":0.0016,` +
		`"projected_gb_hours":474.5,"level":"exhausted","resets_at":"2026-11-01T00:00:00Z"},` +
		`"quota_error":"q","error":"e",` +
		`"repos":[{"name":"darkmem","visibility":"private","covered":true,"artifact_count":1,"artifact_bytes":2,"cache_count":1,"cache_bytes":3,` +
		`"retention_days":90,"retention_max_days":400,"cleanup":"2d","cleanup_source":"global",` +
		`"artifacts":[{"id":7,"name":"dist","bytes":2,"created_at":"2026-10-13T09:00:00Z","expires_at":"2026-10-13T09:00:00Z","run_id":102,"branch":"master"}],` +
		`"caches":[{"id":9,"key":"node","ref":"refs/heads/master","bytes":3,"last_used_at":"2026-10-13T09:00:00Z"}],"error":"r"}],` +
		`"operation":{"id":"o1","kind":"delete-artifacts","repo":"darkmem","total":2,"done":1,"bytes":2,` +
		`"failed":[{"repo":"darkmem","id":8,"error":"not in the last inventory"}],"started_at":"2026-10-13T09:00:00Z","finished_at":"2026-10-13T09:00:00Z"},` +
		`"deletions":[{"at":"2026-10-13T09:00:00Z","repo":"darkmem","kind":"artifact","name":"dist","bytes":2,"age":"3d","by":"cleanup"}]}`
	if string(b) != want {
		t.Fatalf("json\n got %s\nwant %s", b, want)
	}
	b, _ = json.Marshal(GitHubStorage{Repos: []RepoStorage{}, Deletions: []Deletion{}})
	if string(b) != `{"repos":[],"deletions":[]}` {
		t.Fatalf("empty snapshot %s", b)
	}
	b, _ = json.Marshal(GitHubQuota{})
	if !strings.Contains(string(b), `"level":""`) || strings.Contains(string(b), "runs_out_at") || strings.Contains(string(b), "over_allowance") {
		t.Fatalf("zero quota %s", b)
	}
}

func TestStatusStorageAlert(t *testing.T) {
	b, _ := json.Marshal(Status{})
	if strings.Contains(string(b), "github_storage") {
		t.Fatalf("status without a quota carries the alert: %s", b)
	}
	at := time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC)
	b, _ = json.Marshal(StorageAlert{Level: "warn", Month: "2026-10", UsedGBHours: 260, IncludedGBHours: 363.28, UsedPct: 72,
		RunsOutAt: &at, ResetsAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)})
	const want = `{"level":"warn","month":"2026-10","used_gb_hours":260,"included_gb_hours":363.28,"used_pct":72,` +
		`"runs_out_at":"2026-10-24T06:00:00Z","resets_at":"2026-11-01T00:00:00Z"}`
	if string(b) != want {
		t.Fatalf("alert %s", b)
	}
}

// A null repository value removes that override, so it must reach the
// daemon as a key with a nil value.
func TestGitHubStoragePatchDecodes(t *testing.T) {
	var p ConfigPatch
	err := json.Unmarshal([]byte(`{"github_storage":{"included_mb":2048,"warn_pct":[60,80],"cleanup_max_age":"","repos":{"darkmem":"6h","homelab":null}}}`), &p)
	if err != nil {
		t.Fatal(err)
	}
	g := p.GitHubStorage
	if g == nil || *g.IncludedMB != 2048 || len(*g.WarnPct) != 2 || (*g.WarnPct)[1] != 80 || *g.CleanupMaxAge != "" {
		t.Fatalf("patch %+v", g)
	}
	v, ok := g.Repos["homelab"]
	if *g.Repos["darkmem"] != "6h" || !ok || v != nil {
		t.Fatalf("repos %+v", g.Repos)
	}
	for _, tc := range []struct {
		v    any
		want string
	}{
		{DeleteArtifactsRequest{IDs: []int64{1, 2}}, `{"ids":[1,2]}`},
		{DeleteArtifactsRequest{OlderThan: "24h"}, `{"older_than":"24h"}`},
		{DeleteCachesRequest{All: true}, `{"all":true}`},
		{RetentionRequest{Days: 3}, `{"days":3}`},
		{OperationStarted{ID: "o1"}, `{"id":"o1"}`},
		{StoragePreview{Repo: "darkmem", Count: 2, Bytes: 9}, `{"repo":"darkmem","count":2,"bytes":9}`},
	} {
		if b, _ := json.Marshal(tc.v); string(b) != tc.want {
			t.Errorf("%+v → %s", tc.v, b)
		}
	}
}
```
- [ ] **Step 2: Run the tests to verify they fail**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/model/
```
Expected: FAIL, the package does not build: `internal/model/ghstorage_test.go:13:8: undefined: GitHubStorage`, followed by `undefined: GitHubQuota` and more.

- [ ] **Step 3: Write the implementation**

Create `internal/model/ghstorage.go`:

```go
package model

import "time"

// GiB is the binary gigabyte GitHub bills storage in.
const GiB = 1 << 30

// GitHubStorage is the GitHub artifact storage snapshot, served by
// GET /github-storage.
type GitHubStorage struct {
	CheckedAt  *time.Time   `json:"checked_at,omitempty"`
	Quota      *GitHubQuota `json:"quota,omitempty"`
	QuotaError string       `json:"quota_error,omitempty"`
	// Error is the last failed listing of the token's repositories; Repos
	// keeps the last complete list.
	Error     string           `json:"error,omitempty"`
	Repos     []RepoStorage    `json:"repos"`
	Operation *GitHubOperation `json:"operation,omitempty"`
	Deletions []Deletion       `json:"deletions"`
}

// GitHubQuota is the month's storage against the plan's allowance, in
// GB-hours.
type GitHubQuota struct {
	Month                string     `json:"month"`
	ReadAt               time.Time  `json:"read_at"`
	IncludedGBHours      float64    `json:"included_gb_hours"`
	UsedGBHours          float64    `json:"used_gb_hours"`
	ActionsGBHours       float64    `json:"actions_gb_hours"`
	PackagesGBHours      float64    `json:"packages_gb_hours"`
	RateGBHoursPerHour   float64    `json:"rate_gb_hours_per_hour"`
	EstimatedGBHours     float64    `json:"estimated_gb_hours"`
	OverAllowanceGBHours *float64   `json:"over_allowance_gb_hours,omitempty"`
	ProjectedGBHours     float64    `json:"projected_gb_hours"`
	RunsOutAt            *time.Time `json:"runs_out_at,omitempty"`
	// Level is "", "projected", "warn", "critical" or "exhausted".
	Level    string    `json:"level"`
	ResetsAt time.Time `json:"resets_at"`
}

type RepoStorage struct {
	Name          string `json:"name"`
	Visibility    string `json:"visibility"`
	Covered       bool   `json:"covered"`
	ArtifactCount int    `json:"artifact_count"`
	ArtifactBytes int64  `json:"artifact_bytes"`
	CacheCount    int    `json:"cache_count"`
	CacheBytes    int64  `json:"cache_bytes"`
	// RetentionDays and RetentionMaxDays are 0 while unknown.
	RetentionDays    int `json:"retention_days"`
	RetentionMaxDays int `json:"retention_max_days"`
	// Cleanup is "off" or an age such as "2d"; CleanupSource is "global",
	// "override" or "none".
	Cleanup       string         `json:"cleanup"`
	CleanupSource string         `json:"cleanup_source"`
	Artifacts     []ArtifactItem `json:"artifacts"`
	Caches        []CacheItem    `json:"caches"`
	Error         string         `json:"error,omitempty"`
}

type ArtifactItem struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Bytes     int64      `json:"bytes"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	RunID     int64      `json:"run_id"`
	Branch    string     `json:"branch"`
}

type CacheItem struct {
	ID         int64     `json:"id"`
	Key        string    `json:"key"`
	Ref        string    `json:"ref"`
	Bytes      int64     `json:"bytes"`
	LastUsedAt time.Time `json:"last_used_at"`
}

// GitHubOperation is a bulk delete or retention change running in the
// background. Kind is "delete-artifacts", "delete-caches" or
// "set-retention"; Repo is empty for retention across repositories.
type GitHubOperation struct {
	ID         string             `json:"id"`
	Kind       string             `json:"kind"`
	Repo       string             `json:"repo,omitempty"`
	Total      int                `json:"total"`
	Done       int                `json:"done"`
	Bytes      int64              `json:"bytes"`
	Failed     []OperationFailure `json:"failed"`
	StartedAt  time.Time          `json:"started_at"`
	FinishedAt *time.Time         `json:"finished_at,omitempty"`
}

type OperationFailure struct {
	Repo  string `json:"repo"`
	ID    int64  `json:"id,omitempty"`
	Error string `json:"error"`
}

// Deletion is one artifact or cache ghr deleted. Kind is "artifact" or
// "cache"; By is "cleanup" or "manual".
type Deletion struct {
	At    time.Time `json:"at"`
	Repo  string    `json:"repo"`
	Kind  string    `json:"kind"`
	Name  string    `json:"name"`
	Bytes int64     `json:"bytes"`
	Age   string    `json:"age"`
	By    string    `json:"by"`
}

// StorageAlert is the quota's level in GET /status.
type StorageAlert struct {
	Level           string     `json:"level"`
	Month           string     `json:"month"`
	UsedGBHours     float64    `json:"used_gb_hours"`
	IncludedGBHours float64    `json:"included_gb_hours"`
	UsedPct         int        `json:"used_pct"`
	RunsOutAt       *time.Time `json:"runs_out_at,omitempty"`
	ResetsAt        time.Time  `json:"resets_at"`
}

// GitHubStoragePatch changes the github_storage block. CleanupMaxAge ""
// turns the cleanup off; a nil Repos value removes that override.
type GitHubStoragePatch struct {
	IncludedMB    *int               `json:"included_mb,omitempty"`
	WarnPct       *[]int             `json:"warn_pct,omitempty"`
	CleanupMaxAge *string            `json:"cleanup_max_age,omitempty"`
	Repos         map[string]*string `json:"repos,omitempty"`
}

// StoragePreview is what a cleanup age would delete in one repository.
type StoragePreview struct {
	Repo  string `json:"repo"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes"`
}

type DeleteArtifactsRequest struct {
	IDs       []int64 `json:"ids,omitempty"`
	OlderThan string  `json:"older_than,omitempty"`
}

type DeleteCachesRequest struct {
	IDs []int64 `json:"ids,omitempty"`
	All bool    `json:"all,omitempty"`
}

type RetentionRequest struct {
	Days int `json:"days"`
}

type OperationStarted struct {
	ID string `json:"id"`
}
```
In `internal/model/model.go`, at the end of `type Status struct`, replace

```go
	WebListenError *WebListenError `json:"web_listen_error,omitempty"`
}
```

with

```go
	WebListenError *WebListenError `json:"web_listen_error,omitempty"`
	// GitHubStorage is the artifact storage quota's level, absent until the
	// first billing read.
	GitHubStorage *StorageAlert `json:"github_storage,omitempty"`
}
```

In `type ConfigPatch struct` replace

```go
	Web              *WebPatch            `json:"web,omitempty"`
	Repos            map[string]RepoPatch `json:"repos,omitempty"`
}
```

with

```go
	Web              *WebPatch            `json:"web,omitempty"`
	GitHubStorage    *GitHubStoragePatch  `json:"github_storage,omitempty"`
	Repos            map[string]RepoPatch `json:"repos,omitempty"`
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/model/ ./web/
```
Expected: `ok` for both packages; `./web/` shows the existing fixtures did not change.

- [ ] **Step 5: Format, vet and commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./internal/... ./cmd/...
```
Expected: `gofmt` prints nothing and `go vet` passes.

```bash
git add internal/model/ghstorage.go internal/model/ghstorage_test.go internal/model/model.go
git commit -m "feat(model): add GitHub storage types"
```

### Task 3: Add the github_storage config block

**Files:**
- Create: `internal/config/ghstorage.go`
- Modify: `internal/config/config.go` (`Config`, `Validate`)
- Test: `internal/config/ghstorage_test.go` (new)

**Interfaces:**
- Consumes: nothing
- Produces: everything under Contracts **Config (Task 3)**: `GitHubStorage`, `StorageOverride`, `Config.GitHubStorage`, `DefaultIncludedMB`, `DefaultWarnPct`, `MinCleanupMaxAge`, `MinCleanupOverride`, `Included`, `Warn`, `Override`, `CleanupFor`, `ParseCleanup`, `CleanupText`, `ShortDuration`, and the validation messages.

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

Zero values mean the defaults, so `omitempty` keeps a block at its defaults out of config.yaml and `omitzero` keeps it out of `GET /config`. `Config.Clone` round-trips through YAML, so the block needs no clone code. YAML reads an unquoted `off` as the string `off` here (the test parses the spec's example). Any ghr older than this release refuses a config with the block, because `Parse` rejects unknown keys; Task 25 documents that.

- [ ] **Step 1: Write the failing tests**

Create `internal/config/ghstorage_test.go`:

```go
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const storageBlock = `github_storage:
  included_mb: 2048
  warn_pct: [60, 80]
  cleanup_max_age: 2d
  repos:
    darkmem: { cleanup: 6h }
    homelab: { cleanup: off }
`

func TestGitHubStorageDefaults(t *testing.T) {
	c, _, err := Parse([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	g := c.GitHubStorage
	if g.Included() != 500 || g.Warn() != [2]int{70, 90} {
		t.Fatalf("defaults: included %d warn %v", g.Included(), g.Warn())
	}
	if age, source := g.CleanupFor("darkmem"); age != 0 || source != "none" {
		t.Fatalf("cleanup %v %q", age, source)
	}
	js, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(js), "github_storage") {
		t.Fatalf("default block serialised to JSON: %s", js)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "github_storage") {
		t.Fatalf("default block saved to YAML:\n%s", data)
	}
}

func TestGitHubStorageRoundTrips(t *testing.T) {
	c, _, err := Parse([]byte(sample + storageBlock))
	if err != nil {
		t.Fatal(err)
	}
	check := func(what string, c *Config) {
		t.Helper()
		g := c.GitHubStorage
		if g.Included() != 2048 || g.Warn() != [2]int{60, 80} || g.CleanupMaxAge.D() != 48*time.Hour {
			t.Fatalf("%s: %+v", what, g)
		}
		for _, tc := range []struct {
			repo   string
			age    time.Duration
			source string
		}{
			{"DarkMem", 6 * time.Hour, "override"},
			{"homelab", 0, "override"},
			{"darkcloud", 48 * time.Hour, "global"},
		} {
			if age, source := g.CleanupFor(tc.repo); age != tc.age || source != tc.source {
				t.Fatalf("%s: %s cleanup %v %q, want %v %q", what, tc.repo, age, source, tc.age, tc.source)
			}
		}
		if o, ok := g.Override("HOMELAB"); !ok || o.Cleanup != "off" {
			t.Fatalf("%s: override %+v %v", what, o, ok)
		}
	}
	check("parsed", c)
	check("clone", c.Clone())
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	back, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	check("loaded", back)
	js, _ := json.Marshal(c.GitHubStorage)
	const want = `{"included_mb":2048,"warn_pct":[60,80],"cleanup_max_age":"2d","repos":{"darkmem":{"cleanup":"6h"},"homelab":{"cleanup":"off"}}}`
	if string(js) != want {
		t.Fatalf("json %s", js)
	}
}

func TestGitHubStorageValidation(t *testing.T) {
	for _, tc := range []struct {
		block string
		want  string
	}{
		{"  included_mb: -1\n", "github_storage.included_mb must be at least 1"},
		{"  warn_pct: [90, 70]\n", "github_storage.warn_pct must be two increasing percentages from 1 to 100, such as [70, 90]"},
		{"  warn_pct: [70]\n", "github_storage.warn_pct must be two increasing percentages from 1 to 100, such as [70, 90]"},
		{"  warn_pct: [0, 90]\n", "github_storage.warn_pct must be two increasing percentages from 1 to 100, such as [70, 90]"},
		{"  warn_pct: [70, 101]\n", "github_storage.warn_pct must be two increasing percentages from 1 to 100, such as [70, 90]"},
		{"  cleanup_max_age: 23h\n", "github_storage.cleanup_max_age must be at least 24h"},
		{"  repos:\n    darkmem: { cleanup: 59m }\n", "github_storage.repos: darkmem: cleanup must be off or a duration of at least 1h"},
		{"  repos:\n    darkmem: { cleanup: never }\n", "github_storage.repos: darkmem: cleanup must be off or a duration of at least 1h"},
		{"  repos:\n    \"\": { cleanup: off }\n", "github_storage.repos must not contain an empty name"},
		{"  repos:\n    darkraise/darkmem: { cleanup: off }\n", "github_storage.repos: darkraise/darkmem must be a repository name under owner, without a slash"},
		{"  repos:\n    darkmem: { cleanup: off }\n    DarkMem: { cleanup: 2h }\n", "github_storage.repos: DarkMem and darkmem differ only by case"},
	} {
		_, _, err := Parse([]byte(sample + "github_storage:\n" + tc.block))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: got %v, want %q", tc.block, err, tc.want)
		}
	}
	for _, block := range []string{
		"  included_mb: 1\n  warn_pct: [1, 100]\n  cleanup_max_age: 24h\n",
		"  repos:\n    darkmem: { cleanup: 1h }\n",
	} {
		if _, _, err := Parse([]byte(sample + "github_storage:\n" + block)); err != nil {
			t.Errorf("%q rejected: %v", block, err)
		}
	}
}

func TestCleanupText(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0:                    "off",
		48 * time.Hour:       "2d",
		6 * time.Hour:        "6h",
		90 * time.Minute:     "90m",
		90*time.Minute + 1e9: "1h30m1s",
	} {
		if got := CleanupText(d); got != want {
			t.Errorf("CleanupText(%v) = %q, want %q", d, got, want)
		}
	}
	for s, want := range map[string]time.Duration{"off": 0, "2d": 48 * time.Hour, "6h": 6 * time.Hour} {
		if got, err := ParseCleanup(s); err != nil || got != want {
			t.Errorf("ParseCleanup(%q) = %v, %v", s, got, err)
		}
	}
	if _, err := ParseCleanup("soon"); err == nil {
		t.Error("ParseCleanup accepted soon")
	}
}
```
- [ ] **Step 2: Run the tests to verify they fail**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/config/
```
Expected: FAIL, the package does not build: `internal/config/ghstorage_test.go:26:9: c.GitHubStorage undefined (type *Config has no field or method GitHubStorage)`, followed by `undefined: CleanupText` and `undefined: ParseCleanup`.

- [ ] **Step 3: Write the implementation**

Create `internal/config/ghstorage.go`:

```go
package config

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"
)

const (
	// DefaultIncludedMB is the Free plan's storage shared by Actions
	// artifacts and Packages.
	DefaultIncludedMB = 500
	// MinCleanupMaxAge keeps a host clock running fast, or an artifact a
	// later run reads, from being cleaned within a day.
	MinCleanupMaxAge   = 24 * time.Hour
	MinCleanupOverride = time.Hour
)

var DefaultWarnPct = [2]int{70, 90}

// GitHubStorage is the github_storage block. A zero field takes its default,
// so a block left at its defaults is not written.
type GitHubStorage struct {
	IncludedMB    int                        `yaml:"included_mb,omitempty" json:"included_mb,omitempty"`
	WarnPct       []int                      `yaml:"warn_pct,omitempty" json:"warn_pct,omitempty"`
	CleanupMaxAge Duration                   `yaml:"cleanup_max_age,omitempty" json:"cleanup_max_age,omitempty"`
	Repos         map[string]StorageOverride `yaml:"repos,omitempty" json:"repos,omitempty"`
}

type StorageOverride struct {
	Cleanup string `yaml:"cleanup" json:"cleanup"`
}

func (g GitHubStorage) Included() int {
	if g.IncludedMB == 0 {
		return DefaultIncludedMB
	}
	return g.IncludedMB
}

func (g GitHubStorage) Warn() [2]int {
	if len(g.WarnPct) != 2 {
		return DefaultWarnPct
	}
	return [2]int{g.WarnPct[0], g.WarnPct[1]}
}

// Override finds a repository's override, ignoring case.
func (g GitHubStorage) Override(repo string) (StorageOverride, bool) {
	for k, o := range g.Repos {
		if strings.EqualFold(k, repo) {
			return o, true
		}
	}
	return StorageOverride{}, false
}

// CleanupFor is the repository's effective cleanup age, 0 when off, and
// where it comes from: "override", "global" or "none".
func (g GitHubStorage) CleanupFor(repo string) (age time.Duration, source string) {
	if o, ok := g.Override(repo); ok {
		d, _ := ParseCleanup(o.Cleanup)
		return d, "override"
	}
	if g.CleanupMaxAge > 0 {
		return g.CleanupMaxAge.D(), "global"
	}
	return 0, "none"
}

// ParseCleanup reads "off" as 0 and anything else as a duration.
func ParseCleanup(s string) (time.Duration, error) {
	if s == "off" {
		return 0, nil
	}
	d, err := ParseDuration(s)
	return d.D(), err
}

func CleanupText(d time.Duration) string {
	if d == 0 {
		return "off"
	}
	return ShortDuration(d)
}

// ShortDuration writes whole days, hours or minutes with one unit, where
// time.Duration.String would write "6h0m0s".
func ShortDuration(d time.Duration) string {
	switch {
	case d > 0 && d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	case d > 0 && d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d > 0 && d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
	}
	return d.String()
}

func (g GitHubStorage) validate() []string {
	var errs []string
	if g.IncludedMB < 0 {
		errs = append(errs, "github_storage.included_mb must be at least 1")
	}
	if w := g.WarnPct; w != nil && (len(w) != 2 || w[0] < 1 || w[0] >= w[1] || w[1] > 100) {
		errs = append(errs, "github_storage.warn_pct must be two increasing percentages from 1 to 100, such as [70, 90]")
	}
	if g.CleanupMaxAge != 0 && g.CleanupMaxAge.D() < MinCleanupMaxAge {
		errs = append(errs, "github_storage.cleanup_max_age must be at least 24h")
	}
	// Sorted, so the pair named for a case clash is always the same.
	first := map[string]string{}
	for _, k := range slices.Sorted(maps.Keys(g.Repos)) {
		if strings.TrimSpace(k) == "" {
			errs = append(errs, "github_storage.repos must not contain an empty name")
			continue
		}
		if strings.Contains(k, "/") {
			errs = append(errs, "github_storage.repos: "+k+" must be a repository name under owner, without a slash")
		}
		if a, ok := first[strings.ToLower(k)]; ok {
			errs = append(errs, "github_storage.repos: "+a+" and "+k+" differ only by case")
		} else {
			first[strings.ToLower(k)] = k
		}
		if c := g.Repos[k].Cleanup; c != "off" {
			if d, err := ParseDuration(c); err != nil || d.D() < MinCleanupOverride {
				errs = append(errs, "github_storage.repos: "+k+": cleanup must be off or a duration of at least 1h")
			}
		}
	}
	return errs
}
```
In `internal/config/config.go`, at the end of `type Config struct`, replace

```go
	Web Web `yaml:"web,omitempty" json:"web,omitzero"`
}
```

with

```go
	Web Web `yaml:"web,omitempty" json:"web,omitzero"`
	// GitHubStorage sets the artifact storage allowance, warnings and cleanup.
	GitHubStorage GitHubStorage `yaml:"github_storage,omitempty" json:"github_storage,omitzero"`
}
```

At the end of `Validate`, replace

```go
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(sortedUnique(errs), "; "))
```

with

```go
	errs = append(errs, c.GitHubStorage.validate()...)
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(sortedUnique(errs), "; "))
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/config/ ./internal/daemon/ ./web/
```
Expected: `ok` for all three packages: the daemon's config tests and the `config.json` fixture are unchanged, because a default block is omitted.

- [ ] **Step 5: Format, vet and commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./internal/... ./cmd/...
```
Expected: `gofmt` prints nothing and `go vet` passes.

```bash
git add internal/config/ghstorage.go internal/config/ghstorage_test.go internal/config/config.go
git commit -m "feat(config): add the github_storage block"
```

### Task 4: Apply the github_storage patch

**Files:**
- Create: `internal/daemon/storagepolicy.go`
- Modify: `internal/daemon/backend.go` (`PatchConfig`)
- Test: `internal/daemon/storagepolicy_test.go` (new)

**Interfaces:**
- Consumes: `model.GitHubStoragePatch`, `ConfigPatch.GitHubStorage` (Contracts **Model (Task 2)**); `config.GitHubStorage`, `StorageOverride`, `DefaultIncludedMB`, `DefaultWarnPct`, `ParseDuration` and the validation messages (Contracts **Config (Task 3)**)
- Produces: `applyStoragePatch` (Contracts **Patch (Task 4)**); `PATCH /config` accepts `github_storage`. For later daemon tests: `patchJSON(t *testing.T, b *Backend, body string) error` in `storagepolicy_test.go` decodes `body` into a `model.ConfigPatch` and calls `b.PatchConfig`.

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

`applyStoragePatch` runs inside `PatchConfig`'s update, on the copy the store validates and saves, so a refused patch leaves the config untouched. It refuses only what `Validate` cannot see: an unparsable `cleanup_max_age` and an `included_mb` below 1 (0 would silently mean the default). A value equal to its default is stored as the zero value, so saving the defaults from the web form writes no block. Task 10 adds the `PolicyChanged` call after the update.

- [ ] **Step 1: Write the failing tests**

Create `internal/daemon/storagepolicy_test.go`:

```go
package daemon

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

func patchJSON(t *testing.T, b *Backend, body string) error {
	t.Helper()
	var p model.ConfigPatch
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	return b.PatchConfig(context.Background(), p)
}

func TestPatchGitHubStorage(t *testing.T) {
	b, _, _ := newBackend(t)
	if err := patchJSON(t, b, `{"github_storage":{"included_mb":2048,"warn_pct":[60,80],"cleanup_max_age":"2d","repos":{"darkmem":"6h","homelab":"off"}}}`); err != nil {
		t.Fatal(err)
	}
	want := config.GitHubStorage{IncludedMB: 2048, WarnPct: []int{60, 80}, CleanupMaxAge: config.Duration(48 * time.Hour),
		Repos: map[string]config.StorageOverride{"darkmem": {Cleanup: "6h"}, "homelab": {Cleanup: "off"}}}
	if got := b.Store.Config().GitHubStorage; !reflect.DeepEqual(got, want) {
		t.Fatalf("block %+v", got)
	}
	saved, _, err := config.Load(b.Store.ConfigPath)
	if err != nil || !reflect.DeepEqual(saved.GitHubStorage, want) {
		t.Fatalf("not persisted: %+v %v", saved.GitHubStorage, err)
	}
	// A key that differs only by case replaces the override; null removes one.
	if err := patchJSON(t, b, `{"github_storage":{"cleanup_max_age":"","repos":{"DarkMem":"12h","homelab":null}}}`); err != nil {
		t.Fatal(err)
	}
	want = config.GitHubStorage{IncludedMB: 2048, WarnPct: []int{60, 80}, Repos: map[string]config.StorageOverride{"DarkMem": {Cleanup: "12h"}}}
	if got := b.Store.Config().GitHubStorage; !reflect.DeepEqual(got, want) {
		t.Fatalf("block %+v", got)
	}
	// Back to every default: the block is no longer written.
	if err := patchJSON(t, b, `{"github_storage":{"included_mb":500,"warn_pct":[70,90],"repos":{"darkmem":null}}}`); err != nil {
		t.Fatal(err)
	}
	if got := b.Store.Config().GitHubStorage; !reflect.DeepEqual(got, config.GitHubStorage{}) {
		t.Fatalf("block %+v", got)
	}
	data, err := os.ReadFile(b.Store.ConfigPath)
	if err != nil || strings.Contains(string(data), "github_storage") {
		t.Fatalf("default block saved: %v\n%s", err, data)
	}
}

func TestPatchGitHubStorageRejects(t *testing.T) {
	b, _, _ := newBackend(t)
	if err := patchJSON(t, b, `{"github_storage":{"cleanup_max_age":"2d"}}`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		body, want string
	}{
		{`{"included_mb":0}`, "github_storage.included_mb must be at least 1"},
		{`{"cleanup_max_age":"soon"}`, `github_storage.cleanup_max_age: invalid duration "soon"`},
		{`{"cleanup_max_age":"23h"}`, "github_storage.cleanup_max_age must be at least 24h"},
		{`{"warn_pct":[90,70]}`, "github_storage.warn_pct must be two increasing percentages from 1 to 100, such as [70, 90]"},
		{`{"repos":{"darkmem":"30m"}}`, "github_storage.repos: darkmem: cleanup must be off or a duration of at least 1h"},
		{`{"repos":{"":"off"}}`, "github_storage.repos must not contain an empty name"},
	} {
		err := patchJSON(t, b, `{"github_storage":`+tc.body+`}`)
		if apiStatus(err) != 400 || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err %v, want 400 %q", tc.body, err, tc.want)
		}
	}
	want := config.GitHubStorage{CleanupMaxAge: config.Duration(48 * time.Hour)}
	if got := b.Store.Config().GitHubStorage; !reflect.DeepEqual(got, want) {
		t.Fatalf("a refused patch changed the block: %+v", got)
	}
}
```
- [ ] **Step 2: Run the tests to verify they fail**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 -run 'TestPatchGitHubStorage' ./internal/daemon/
```
Expected: FAIL: `storagepolicy_test.go:33: block {IncludedMB:0 WarnPct:[] CleanupMaxAge:0s Repos:map[]}` (the patch is ignored), and in `TestPatchGitHubStorageRejects` six lines such as `{"included_mb":0}: err <nil>, want 400 "github_storage.included_mb must be at least 1"`.

- [ ] **Step 3: Write the implementation**

Create `internal/daemon/storagepolicy.go`:

```go
package daemon

import (
	"maps"
	"slices"
	"strings"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

// applyStoragePatch writes a github_storage patch into c; Validate judges
// the values it stores. A value equal to its default is stored as the zero
// value, so a block back at its defaults is not written.
func applyStoragePatch(c *config.Config, p *model.GitHubStoragePatch) error {
	g := &c.GitHubStorage
	if p.IncludedMB != nil {
		if *p.IncludedMB < 1 {
			return api.BadRequest("github_storage.included_mb must be at least 1")
		}
		g.IncludedMB = *p.IncludedMB
	}
	if p.WarnPct != nil {
		g.WarnPct = *p.WarnPct
	}
	if p.CleanupMaxAge != nil {
		g.CleanupMaxAge = 0
		if *p.CleanupMaxAge != "" {
			d, err := config.ParseDuration(*p.CleanupMaxAge)
			if err != nil {
				return api.BadRequest("github_storage.cleanup_max_age: " + err.Error())
			}
			g.CleanupMaxAge = d
		}
	}
	for _, name := range slices.Sorted(maps.Keys(p.Repos)) {
		for k := range g.Repos {
			if strings.EqualFold(k, name) {
				delete(g.Repos, k)
			}
		}
		if v := p.Repos[name]; v != nil {
			if g.Repos == nil {
				g.Repos = map[string]config.StorageOverride{}
			}
			g.Repos[name] = config.StorageOverride{Cleanup: *v}
		}
	}
	if g.IncludedMB == config.DefaultIncludedMB {
		g.IncludedMB = 0
	}
	if slices.Equal(g.WarnPct, config.DefaultWarnPct[:]) {
		g.WarnPct = nil
	}
	if len(g.Repos) == 0 {
		g.Repos = nil
	}
	return nil
}
```
In `internal/daemon/backend.go`, in `PatchConfig`, replace

```go
			if w.Hosts != nil {
				c.Web.Hosts = *w.Hosts
			}
		}
```

with

```go
			if w.Hosts != nil {
				c.Web.Hosts = *w.Hosts
			}
		}
		if p.GitHubStorage != nil {
			if err := applyStoragePatch(c, p.GitHubStorage); err != nil {
				return err
			}
		}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 -run 'TestPatch' ./internal/daemon/
```
Expected: `ok  	github.com/darkraise/ghr/internal/daemon`.

- [ ] **Step 5: Format, vet and commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./internal/... ./cmd/...
```
Expected: `gofmt` prints nothing and `go vet` passes.

```bash
git add internal/daemon/storagepolicy.go internal/daemon/storagepolicy_test.go internal/daemon/backend.go
git commit -m "feat(daemon): apply the github_storage patch"
```

### Task 5: Compute the quota and warning level

**Files:**
- Create: `internal/ghstorage/quota.go`
- Test: `internal/ghstorage/quota_test.go` (new)

**Interfaces:**
- Consumes: `github.UsageItem`, `SKUActionsStorage`, `SKUPackagesStorage` (Contracts **GitHub client (Task 1)**); `model.GitHubQuota`, `StorageAlert`, `GiB` (Contracts **Model (Task 2)**)
- Produces: everything under Contracts **Quota math (Task 5)**: `LagHours`, `Billing`, `MonthBounds`, `SumUsage`, `ComputeQuota`, `AlertFrom`; the package `ghstorage` and its doc comment.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

Pure functions over one billing read, spec §2.2 and §2.4. Every figure is relative to `ReadAt`, not the clock, so the snapshot changes only when a pass changes its inputs. The test uses October 2026 (744 hours: 500 MB is 363.28125 GB-hours) and numbers that are exact in binary floating point, so it compares with `==`.

- [ ] **Step 1: Write the failing tests**

Create `internal/ghstorage/quota_test.go`:

```go
package ghstorage

import (
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

func TestMonthBounds(t *testing.T) {
	for _, tc := range []struct {
		at            time.Time
		start, resets time.Time
	}{
		{time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		{time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC), time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)},
		// 03:00 on Nov 1 at UTC+7 is still October in UTC.
		{time.Date(2026, 11, 1, 3, 0, 0, 0, time.FixedZone("ICT", 7*3600)), time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
	} {
		start, resets := MonthBounds(tc.at)
		if !start.Equal(tc.start) || !resets.Equal(tc.resets) || start.Location() != time.UTC {
			t.Errorf("MonthBounds(%v) = %v, %v", tc.at, start, resets)
		}
	}
}

func TestSumUsage(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	items := []github.UsageItem{
		{Date: day(5), SKU: github.SKUActionsStorage, Repo: "darkmem", Quantity: 204.5},
		{Date: day(12), SKU: github.SKUActionsStorage, Repo: "darkcloud", Quantity: 4.25},
		{Date: day(11), SKU: github.SKUPackagesStorage, Quantity: 0.5},
		{Date: day(12), SKU: github.SKUPackagesStorage, Quantity: 0.75},
		{Date: day(13), SKU: github.SKUPackagesStorage, Quantity: 0.125},
	}
	actions, packages, daily := SumUsage(items, time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC))
	if actions != 208.75 || packages != 1.375 || daily != 0.75 {
		t.Fatalf("actions %v packages %v daily %v", actions, packages, daily)
	}
}

// October 2026 has 744 hours; 500 MB is 500/1024 × 744 = 363.28125 GB-h.
// Read at 09:00 on Oct 13, 447 hours are left before Nov 1.
func TestComputeQuota(t *testing.T) {
	readAt := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	q := ComputeQuota(Billing{Month: "2026-10", ReadAt: readAt, Actions: 90, Packages: 10, PackagesDaily: 6}, model.GiB/2, 500, [2]int{70, 90})
	if q.Month != "2026-10" || !q.ReadAt.Equal(readAt) || !q.ResetsAt.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("quota %+v", q)
	}
	// Binary units: half a GiB of artifacts accrues 0.5 GB-h an hour, and
	// Packages at 6 GB-h a day 0.25 an hour.
	if q.IncludedGBHours != 363.28125 || q.UsedGBHours != 100 || q.ActionsGBHours != 90 || q.PackagesGBHours != 10 ||
		q.RateGBHoursPerHour != 0.75 || q.EstimatedGBHours != 109 || q.ProjectedGBHours != 435.25 {
		t.Fatalf("quota %+v", q)
	}
	if q.OverAllowanceGBHours != nil {
		t.Fatalf("over %v without a summary", *q.OverAllowanceGBHours)
	}

	q = ComputeQuota(Billing{Month: "2026-10", ReadAt: readAt, Actions: 100}, model.GiB, 500, [2]int{70, 90})
	// (363.28125 - 112) / 1 = 251.28125 hours after the read.
	want := readAt.Add(251*time.Hour + 16*time.Minute + 52*time.Second + 500*time.Millisecond)
	if q.RunsOutAt == nil || !q.RunsOutAt.Equal(want) {
		t.Fatalf("runs out at %v, want %v", q.RunsOutAt, want)
	}
	for _, tc := range []struct {
		name  string
		b     Billing
		bytes int64
	}{
		{"zero rate", Billing{Actions: 100}, 0},
		{"estimate already over", Billing{Actions: 360}, model.GiB},
		{"after the reset", Billing{Actions: 100}, model.GiB / 1024},
		{"too slow to fit a duration", Billing{Actions: 100}, 1},
	} {
		tc.b.ReadAt = readAt
		if q := ComputeQuota(tc.b, tc.bytes, 500, [2]int{70, 90}); q.RunsOutAt != nil {
			t.Errorf("%s: runs out at %v", tc.name, q.RunsOutAt)
		}
	}
}

func TestQuotaLevel(t *testing.T) {
	readAt := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	over := func(v float64) *float64 { return &v }
	for _, tc := range []struct {
		name  string
		b     Billing
		bytes int64
		warn  [2]int
		want  string
	}{
		{"billed at the allowance", Billing{Actions: 363.28125}, 0, [2]int{70, 90}, "exhausted"},
		{"GitHub bills past its allowance", Billing{Actions: 100, Over: over(0.0016)}, 0, [2]int{70, 90}, "exhausted"},
		{"rounding noise in net", Billing{Actions: 100, Over: over(0.0005)}, 0, [2]int{70, 90}, ""},
		{"estimate past 90%", Billing{Actions: 320}, model.GiB, [2]int{70, 90}, "critical"},
		{"estimate past 70%", Billing{Actions: 250}, model.GiB, [2]int{70, 90}, "warn"},
		{"own thresholds", Billing{Actions: 200}, 0, [2]int{50, 60}, "warn"},
		{"projected past the allowance", Billing{Actions: 100}, model.GiB, [2]int{70, 90}, "projected"},
		{"none", Billing{Actions: 10}, 0, [2]int{70, 90}, ""},
	} {
		tc.b.ReadAt = readAt
		if got := ComputeQuota(tc.b, tc.bytes, 500, tc.warn).Level; got != tc.want {
			t.Errorf("%s: level %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestAlertFrom(t *testing.T) {
	if AlertFrom(nil) != nil {
		t.Fatal("alert without a quota")
	}
	readAt := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	q := ComputeQuota(Billing{Month: "2026-10", ReadAt: readAt, Actions: 373.3}, 0, 500, [2]int{70, 90})
	a := AlertFrom(&q)
	if a.Level != "exhausted" || a.Month != "2026-10" || a.UsedGBHours != 373.3 || a.IncludedGBHours != 363.28125 ||
		a.UsedPct != 103 || a.RunsOutAt != nil || !a.ResetsAt.Equal(q.ResetsAt) {
		t.Fatalf("alert %+v", a)
	}
	q = ComputeQuota(Billing{Month: "2026-10", ReadAt: readAt, Actions: 100}, model.GiB, 500, [2]int{70, 90})
	if a := AlertFrom(&q); a.RunsOutAt == nil || !a.RunsOutAt.Equal(*q.RunsOutAt) || a.RunsOutAt == q.RunsOutAt {
		t.Fatalf("runs out %v", a.RunsOutAt)
	}
}
```
- [ ] **Step 2: Run the tests to verify they fail**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/ghstorage/
```
Expected: FAIL, the package does not build: `internal/ghstorage/quota_test.go:21:20: undefined: MonthBounds`, followed by `undefined: SumUsage`, `undefined: ComputeQuota` and `undefined: Billing`.

- [ ] **Step 3: Write the implementation**

Create `internal/ghstorage/quota.go`:

```go
// Package ghstorage keeps the snapshot of the owner's GitHub artifact
// storage, runs the age-based cleanup and the manual deletes and retention
// changes, and computes the month's quota and warning level.
package ghstorage

import (
	"math"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// LagHours is how far GitHub's usage figure trails: it recalculates every
// 6 to 12 hours.
const LagHours = 12

// Billing is one billing read. PackagesDaily is the Packages GB-hours of the
// last full UTC day before ReadAt; Over is the summary's net Actions and
// Packages storage, nil when the summary could not be read.
type Billing struct {
	Month                            string
	ReadAt                           time.Time
	Actions, Packages, PackagesDaily float64
	Over                             *float64
}

// MonthBounds is the start of t's UTC month and the start of the next.
func MonthBounds(t time.Time) (start, resets time.Time) {
	t = t.UTC()
	start = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(0, 1, 0)
}

func SumUsage(items []github.UsageItem, readAt time.Time) (actions, packages, packagesDaily float64) {
	r := readAt.UTC()
	today := time.Date(r.Year(), r.Month(), r.Day(), 0, 0, 0, 0, time.UTC)
	yesterday := today.AddDate(0, 0, -1)
	for _, it := range items {
		switch it.SKU {
		case github.SKUActionsStorage:
			actions += it.Quantity
		case github.SKUPackagesStorage:
			packages += it.Quantity
			if !it.Date.Before(yesterday) && it.Date.Before(today) {
				packagesDaily += it.Quantity
			}
		}
	}
	return actions, packages, packagesDaily
}

// ComputeQuota measures the month against the allowance from one billing
// read and the artifact bytes stored now. Everything is relative to
// b.ReadAt, so the result changes only when a pass changes its inputs.
func ComputeQuota(b Billing, artifactBytes int64, includedMB int, warn [2]int) model.GitHubQuota {
	start, resets := MonthBounds(b.ReadAt)
	q := model.GitHubQuota{
		Month:              b.Month,
		ReadAt:             b.ReadAt,
		IncludedGBHours:    float64(includedMB) / 1024 * resets.Sub(start).Hours(),
		UsedGBHours:        b.Actions + b.Packages,
		ActionsGBHours:     b.Actions,
		PackagesGBHours:    b.Packages,
		RateGBHoursPerHour: float64(artifactBytes)/model.GiB + b.PackagesDaily/24,
		ResetsAt:           resets,
	}
	if b.Over != nil {
		v := *b.Over
		q.OverAllowanceGBHours = &v
	}
	left := resets.Sub(b.ReadAt).Hours()
	q.EstimatedGBHours = q.UsedGBHours + q.RateGBHoursPerHour*LagHours
	q.ProjectedGBHours = q.UsedGBHours + q.RateGBHoursPerHour*left
	// Comparing the hours with what is left first keeps a tiny rate from
	// overflowing time.Duration.
	if q.RateGBHoursPerHour > 0 && q.EstimatedGBHours < q.IncludedGBHours {
		if h := (q.IncludedGBHours - q.EstimatedGBHours) / q.RateGBHoursPerHour; h < left {
			at := b.ReadAt.Add(time.Duration(h * float64(time.Hour)))
			q.RunsOutAt = &at
		}
	}
	q.Level = level(q, warn)
	return q
}

// level compares the thresholds with the estimate, because the billed figure
// trails; exhausted uses what GitHub billed, because it promises failures.
func level(q model.GitHubQuota, warn [2]int) string {
	switch {
	case q.UsedGBHours >= q.IncludedGBHours, q.OverAllowanceGBHours != nil && *q.OverAllowanceGBHours > 0.001:
		return "exhausted"
	case q.EstimatedGBHours >= float64(warn[1])/100*q.IncludedGBHours:
		return "critical"
	case q.EstimatedGBHours >= float64(warn[0])/100*q.IncludedGBHours:
		return "warn"
	case q.ProjectedGBHours >= q.IncludedGBHours:
		return "projected"
	}
	return ""
}

// AlertFrom is the status alert for a quota, nil without one.
func AlertFrom(q *model.GitHubQuota) *model.StorageAlert {
	if q == nil {
		return nil
	}
	a := &model.StorageAlert{Level: q.Level, Month: q.Month, UsedGBHours: q.UsedGBHours, IncludedGBHours: q.IncludedGBHours, ResetsAt: q.ResetsAt}
	if q.IncludedGBHours > 0 {
		a.UsedPct = int(math.Round(q.UsedGBHours / q.IncludedGBHours * 100))
	}
	if q.RunsOutAt != nil {
		at := *q.RunsOutAt
		a.RunsOutAt = &at
	}
	return a
}
```
- [ ] **Step 4: Run the tests to verify they pass**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/ghstorage/
```
Expected: `ok  	github.com/darkraise/ghr/internal/ghstorage`.

- [ ] **Step 5: Format, vet and commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./internal/... ./cmd/...
```
Expected: `gofmt` prints nothing and `go vet` passes.

```bash
git add internal/ghstorage/quota.go internal/ghstorage/quota_test.go
git commit -m "feat(ghstorage): compute the storage quota"
```

### Task 6: Select cleanup candidates

**Files:**
- Create: `internal/ghstorage/cleanup.go`
- Test: `internal/ghstorage/cleanup_test.go` (new)

**Interfaces:**
- Consumes: `model.ArtifactItem`, `RepoStorage`, `StoragePreview` (Contracts **Model (Task 2)**); `config.GitHubStorage.Override` (Contracts **Config (Task 3)**)
- Produces: Contracts **Service (Tasks 6 to 9)**, Task 6: `ActiveStatuses`, `Pick`, `Candidates`, `Oldest`, `Preview`.

**Items:** 2

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

Pure functions, no GitHub calls: they decide which artifacts the cleanup (Task 8), a "delete older than" (Task 9) and the save-preview dialogs (Task 9's `Preview`) touch. An artifact is a candidate when it is strictly older than the age (`now - CreatedAt > age`) and its run is not in `active`; an age of 0 is off. `Oldest` orders oldest first, ties by repository then id, so a capped pass always takes the same artifacts. `Preview` without a repository leaves out every repository that has an override, matched ignoring case, because the global age does not apply to them.

- [ ] **Step 1: Write the failing tests**

Before the first command of the session run `mkdir -p /root/.cache/ghr-plan-tmp`.

Create `internal/ghstorage/cleanup_test.go`:

```go
package ghstorage

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

var t0 = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

func art(id int64, run int64, age time.Duration, bytes int64) model.ArtifactItem {
	return model.ArtifactItem{ID: id, Name: "a", Bytes: bytes, CreatedAt: t0.Add(-age), RunID: run}
}

func ids(items []model.ArtifactItem) []int64 {
	out := []int64{}
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

func TestCandidates(t *testing.T) {
	items := []model.ArtifactItem{
		art(1, 10, 30*time.Hour, 1),
		art(2, 20, 24*time.Hour, 1), // exactly the age: not older
		art(3, 30, 25*time.Hour, 1),
		art(4, 40, time.Hour, 1),
	}
	for _, tc := range []struct {
		name   string
		age    time.Duration
		active map[int64]bool
		want   []int64
	}{
		{"older than the age", 24 * time.Hour, nil, []int64{1, 3}},
		{"a run not completed is skipped", 24 * time.Hour, map[int64]bool{30: true}, []int64{1}},
		{"off selects nothing", 0, nil, []int64{}},
		{"a short override", 30 * time.Minute, nil, []int64{1, 2, 3, 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ids(Candidates(items, tc.age, t0, tc.active)); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Candidates = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOldestFirstWithCap(t *testing.T) {
	picks := []Pick{
		{Repo: "b", Item: art(5, 1, 2*time.Hour, 1)},
		{Repo: "a", Item: art(7, 1, 9*time.Hour, 1)},
		{Repo: "b", Item: art(6, 1, 9*time.Hour, 1)},
		{Repo: "a", Item: art(3, 1, 9*time.Hour, 1)},
		{Repo: "a", Item: art(9, 1, 5*time.Hour, 1)},
	}
	got := Oldest(picks, 4)
	var order []string
	for _, p := range got {
		order = append(order, fmt.Sprintf("%s:%d", p.Repo, p.Item.ID))
	}
	if want := []string{"a:3", "a:7", "b:6", "a:9"}; !reflect.DeepEqual(order, want) {
		t.Errorf("Oldest = %v, want %v", order, want)
	}
	if picks[0].Item.ID != 5 {
		t.Error("Oldest reordered its argument")
	}
}

func TestPreview(t *testing.T) {
	repos := []model.RepoStorage{
		{Name: "darkmem", Artifacts: []model.ArtifactItem{art(1, 1, 50*time.Hour, 100), art(2, 2, 3*time.Hour, 7)}},
		{Name: "homelab", Artifacts: []model.ArtifactItem{art(3, 3, 72*time.Hour, 500)}},
		{Name: "chicken-farm", Artifacts: []model.ArtifactItem{art(4, 4, 49*time.Hour, 300), art(5, 5, 60*time.Hour, 1)}},
		{Name: "empty"},
	}
	cfg := config.GitHubStorage{Repos: map[string]config.StorageOverride{"HomeLab": {Cleanup: "off"}}}
	for _, tc := range []struct {
		name string
		repo string
		age  time.Duration
		want []model.StoragePreview
	}{
		{"global skips repositories with an override", "", 48 * time.Hour,
			[]model.StoragePreview{{Repo: "chicken-farm", Count: 2, Bytes: 301}, {Repo: "darkmem", Count: 1, Bytes: 100}}},
		{"one repository with its proposed override", "homelab", 6 * time.Hour,
			[]model.StoragePreview{{Repo: "homelab", Count: 1, Bytes: 500}}},
		{"the repository is matched ignoring case", "DARKMEM", time.Hour,
			[]model.StoragePreview{{Repo: "darkmem", Count: 2, Bytes: 107}}},
		{"off previews nothing", "", 0, []model.StoragePreview{}},
		{"nothing old enough", "", 400 * time.Hour, []model.StoragePreview{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Preview(repos, cfg, tc.repo, tc.age, t0); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Preview = %+v, want %+v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/ghstorage/
```
Expected: the build fails:

```
internal/ghstorage/cleanup_test.go:45:18: undefined: Candidates
internal/ghstorage/cleanup_test.go:53:13: undefined: Pick
internal/ghstorage/cleanup_test.go:60:9: undefined: Oldest
internal/ghstorage/cleanup_test.go:97:14: undefined: Preview
FAIL	github.com/darkraise/ghr/internal/ghstorage [build failed]
```

- [ ] **Step 3: Write the implementation**

Create `internal/ghstorage/cleanup.go`:

```go
package ghstorage

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

// ActiveStatuses are the run statuses of a run that has not completed. A
// deploy job waiting for an environment approval is "waiting".
var ActiveStatuses = []string{"queued", "in_progress", "waiting", "requested", "pending"}

// Pick is one artifact chosen for deletion.
type Pick struct {
	Repo string
	Item model.ArtifactItem
}

// Candidates are the items older than age, measured against now, whose run
// is not in active. An age of 0 is off and selects nothing.
func Candidates(items []model.ArtifactItem, age time.Duration, now time.Time, active map[int64]bool) []model.ArtifactItem {
	out := []model.ArtifactItem{}
	if age <= 0 {
		return out
	}
	for _, it := range items {
		if now.Sub(it.CreatedAt) > age && !active[it.RunID] {
			out = append(out, it)
		}
	}
	return out
}

// Oldest returns at most n picks, oldest first; ties go by repository, then
// id, so a capped pass always takes the same ones.
func Oldest(picks []Pick, n int) []Pick {
	out := slices.Clone(picks)
	slices.SortStableFunc(out, func(a, b Pick) int {
		if c := a.Item.CreatedAt.Compare(b.Item.CreatedAt); c != 0 {
			return c
		}
		if c := strings.Compare(a.Repo, b.Repo); c != 0 {
			return c
		}
		return cmp.Compare(a.Item.ID, b.Item.ID)
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Preview counts what a cleanup age would delete now. With repo, it is that
// repository's proposed override; without, the proposed global age for every
// repository that has no override. It counts artifacts of runs still in
// progress too, which the cleanup itself skips.
func Preview(repos []model.RepoStorage, cfg config.GitHubStorage, repo string, age time.Duration, now time.Time) []model.StoragePreview {
	out := []model.StoragePreview{}
	for _, r := range repos {
		if repo != "" {
			if !strings.EqualFold(r.Name, repo) {
				continue
			}
		} else if _, ok := cfg.Override(r.Name); ok {
			continue
		}
		p := model.StoragePreview{Repo: r.Name}
		for _, it := range Candidates(r.Artifacts, age, now, nil) {
			p.Count++
			p.Bytes += it.Bytes
		}
		if p.Count > 0 {
			out = append(out, p)
		}
	}
	slices.SortStableFunc(out, func(a, b model.StoragePreview) int {
		if c := cmp.Compare(b.Bytes, a.Bytes); c != 0 {
			return c
		}
		return strings.Compare(a.Repo, b.Repo)
	})
	return out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/ghstorage/
```
Expected: `ok  	github.com/darkraise/ghr/internal/ghstorage`.

- [ ] **Step 5: Format, vet and commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./internal/... ./cmd/...
```
Expected: `gofmt` prints nothing and `go vet` passes.

```bash
git add internal/ghstorage/cleanup.go internal/ghstorage/cleanup_test.go
git commit -m "feat(ghstorage): select cleanup candidates"
```

### Task 7: Run the storage passes

**Files:**
- Create: `internal/ghstorage/service.go`
- Test: `internal/ghstorage/fake_test.go` (new: the fake GitHub, the fake clock, `newTestService` and small helpers that Tasks 8 and 9 reuse)
- Test: `internal/ghstorage/service_test.go` (new)

**Interfaces:**
- Consumes: Contracts **GitHub client (Task 1)** (`github.UserRepo`, `Artifact`, `Cache`, `Retention`, `UsageItem`, `SummaryItem`, `APIError`, `IsTooManyPages`, `TokenMeta`); Contracts **Model (Task 2)**; `config.GitHubStorage.Included`, `Warn`, `CleanupFor`, `config.CleanupText`, `config.Config.Repo`, `Watched` (Contracts **Config (Task 3)**); `MonthBounds`, `SumUsage`, `Billing`, `ComputeQuota`, `AlertFrom` (Contracts **Quota math (Task 5)**); `events.Ring`.
- Produces: Contracts **Service (Tasks 6 to 9)**, Task 7: `GitHub`, `Service` and its exported fields, `Start`, `Close`, `Wait`, `Kick`, `Refresh`, `TokenChanged`, `PolicyChanged`, `Snapshot`, `Alert`, `Version`, the constants and `ErrBusy`, `ErrClosed`; and the unexported names Tasks 8 and 9 build on (see Contracts **ghstorage internals**).

**Items:** 1

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

The service mirrors `internal/storage/service.go`: `Start` runs one loop with an inventory timer (15 minutes), a billing timer (an hour, or just after the month changes) and a `kick` channel with a buffer of 1, filled once so the first full pass runs at once. `Refresh` refuses with `ErrBusy` while a pass runs; `Kick` never refuses. A pass reads the config once, is skipped while the client is suspended or has fewer than 500 calls left, and drops its result when the token generation changed or the service closed by the time it finishes (`keep`). The inventory lists the owner's repositories (other owners' repositories that the token reaches are left out, as `AvailableRepos` does), then 4 at a time their artifacts (expired ones dropped) and caches, and their retention at most once a day. A repository whose listing fails keeps its last items with the error. `removeItem` takes a deleted item out of the snapshot and remembers its id for an hour, and every merge leaves those ids out, so a listing that started before a delete cannot bring the item back. The billing timer is re-armed with `billingDelay(now)`: an hour, or one minute after the UTC month changes when that is sooner. Billing reads the owner type once, refuses a user owner's quota unless the token is the owner's, and keeps the last quota on a failure; a pass that starts at or after the old quota's `ResetsAt` clears it before reading, so neither the snapshot nor the alert shows last month's numbers while the new month's read runs or fails. `recompute` writes one event per level change. `Snapshot` copies everything and adds each row's cleanup, cleanup source and coverage from the current config. Tests run passes directly with `s.pass` on a fake clock; the concurrency tests block the fake's listing on channels, never on sleeps.

- [ ] **Step 1: Write the failing tests**

Create `internal/ghstorage/fake_test.go`:

```go
package ghstorage

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// fakeGH is a GitHub whose answers the test sets. Calls records every call
// as "Method repo [id]". Hooks run outside the lock, before the answer.
type fakeGH struct {
	mu        sync.Mutex
	isOrg     bool
	login     string
	usage     []github.UsageItem
	usageErr  error
	summary   []github.SummaryItem
	sumErr    error
	repos     []github.UserRepo
	reposErr  error
	artifacts map[string][]github.Artifact
	caches    map[string][]github.Cache
	listErr   map[string]error
	retention map[string]github.Retention
	retErr    map[string]error
	setRetErr map[string]error
	runs      map[string]map[string][]github.Run // repo → status → runs
	runsErr   map[string]error
	gone      map[int64]bool  // a delete of this id answers 404
	delErr    map[int64]error // a delete of this id fails
	gen       uint64
	suspended time.Time
	remaining int
	calls     []string

	// onList runs at the start of ListRepoArtifacts; an error it returns is
	// the call's answer.
	onList func(ctx context.Context, repo string) error
	// onDelete runs at the start of DeleteArtifact and DeleteCache.
	onDelete func(ctx context.Context, repo string, id int64) error
	// onUsage runs at the start of StorageUsage; an error it returns is the
	// call's answer.
	onUsage func(ctx context.Context) error
}

func newFakeGH() *fakeGH {
	return &fakeGH{
		login: "darkraise", remaining: -1,
		artifacts: map[string][]github.Artifact{}, caches: map[string][]github.Cache{},
		listErr: map[string]error{}, retention: map[string]github.Retention{}, retErr: map[string]error{},
		setRetErr: map[string]error{}, runs: map[string]map[string][]github.Run{}, runsErr: map[string]error{},
		gone: map[int64]bool{}, delErr: map[int64]error{},
	}
}

func (f *fakeGH) call(format string, args ...any) {
	f.mu.Lock()
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
	f.mu.Unlock()
}

// callsLike returns the recorded calls that start with prefix.
func (f *fakeGH) callsLike(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeGH) OwnerType(context.Context) (bool, error) {
	f.call("OwnerType")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.isOrg, nil
}

func (f *fakeGH) TokenLogin(context.Context) (string, error) {
	f.call("TokenLogin")
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.login, nil
}

func (f *fakeGH) StorageUsage(ctx context.Context, isOrg bool, year int, month time.Month) ([]github.UsageItem, error) {
	f.call("StorageUsage %v %d-%02d", isOrg, year, int(month))
	if f.onUsage != nil {
		if err := f.onUsage(ctx); err != nil {
			return nil, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.usage), f.usageErr
}

func (f *fakeGH) StorageSummary(context.Context, bool, int, time.Month) ([]github.SummaryItem, error) {
	f.call("StorageSummary")
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.summary), f.sumErr
}

func (f *fakeGH) ListUserRepos(context.Context) ([]github.UserRepo, error) {
	f.call("ListUserRepos")
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.repos), f.reposErr
}

func (f *fakeGH) ListRepoArtifacts(ctx context.Context, repo string) ([]github.Artifact, error) {
	f.call("ListRepoArtifacts %s", repo)
	if f.onList != nil {
		if err := f.onList(ctx, repo); err != nil {
			return nil, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.listErr[repo]; err != nil {
		return nil, err
	}
	return slices.Clone(f.artifacts[repo]), nil
}

func (f *fakeGH) ListCaches(_ context.Context, repo string) ([]github.Cache, error) {
	f.call("ListCaches %s", repo)
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.caches[repo]), nil
}

func (f *fakeGH) Retention(_ context.Context, repo string) (github.Retention, error) {
	f.call("Retention %s", repo)
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.retention[repo], f.retErr[repo]
}

func (f *fakeGH) SetRetention(_ context.Context, repo string, days int) error {
	f.call("SetRetention %s %d", repo, days)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.setRetErr[repo]; err != nil {
		return err
	}
	r := f.retention[repo]
	r.Days = days
	f.retention[repo] = r
	return nil
}

func (f *fakeGH) ListRuns(_ context.Context, repo, status string) ([]github.Run, error) {
	f.call("ListRuns %s %s", repo, status)
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.runsErr[repo]; err != nil {
		return nil, err
	}
	return slices.Clone(f.runs[repo][status]), nil
}

func (f *fakeGH) del(ctx context.Context, kind, repo string, id int64) (bool, error) {
	f.call("Delete%s %s %d", kind, repo, id)
	if f.onDelete != nil {
		if err := f.onDelete(ctx, repo, id); err != nil {
			return false, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.delErr[id]; err != nil {
		return false, err
	}
	if f.gone[id] {
		return false, nil
	}
	f.gone[id] = true
	return true, nil
}

func (f *fakeGH) DeleteArtifact(ctx context.Context, repo string, id int64) (bool, error) {
	return f.del(ctx, "Artifact", repo, id)
}

func (f *fakeGH) DeleteCache(ctx context.Context, repo string, id int64) (bool, error) {
	return f.del(ctx, "Cache", repo, id)
}

func (f *fakeGH) TokenMeta() github.TokenMeta {
	f.mu.Lock()
	defer f.mu.Unlock()
	return github.TokenMeta{Generation: f.gen}
}

func (f *fakeGH) SuspendedUntil() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.suspended
}

func (f *fakeGH) RateRemaining() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.remaining
}

// addRepo makes repo reachable with the given artifacts, all made by
// different runs. Bytes are 1 GiB each unless set.
func (f *fakeGH) addRepo(repo string, private bool, arts ...github.Artifact) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.repos = append(f.repos, github.UserRepo{Name: repo, Private: private, Owner: github.Account{Login: "darkraise"}})
	f.artifacts[repo] = arts
	f.retention[repo] = github.Retention{Days: 90, MaxDays: 400}
}

// ghArt is an artifact of run run, created age before at.
func ghArt(id, run int64, at time.Time, age time.Duration, bytes int64) github.Artifact {
	return github.Artifact{ID: id, Name: fmt.Sprintf("art-%d", id), SizeInBytes: bytes, CreatedAt: at.Add(-age),
		WorkflowRun: github.ArtifactRun{ID: run, HeadBranch: "main"}}
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// newTestService builds a service on gh and cfg that runs no loop: the test
// runs passes with s.pass. Its clock starts at 2026-10-13 12:00 UTC and its
// operation ids are op-1, op-2, and so on.
func newTestService(t *testing.T, gh *fakeGH, cfg *config.Config) (*Service, *fakeClock) {
	t.Helper()
	clock := &fakeClock{t: time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)}
	ev := events.New()
	ev.Now = clock.Now
	n := 0
	s := &Service{GH: gh, Config: func() *config.Config { return cfg }, Events: ev, Now: clock.Now,
		NewID: func() string { n++; return fmt.Sprintf("op-%d", n) }}
	s.init()
	t.Cleanup(func() { s.Close(); s.Wait() })
	return s, clock
}

func testConfig() *config.Config { return &config.Config{Owner: "darkraise"} }

// row returns the snapshot's row for repo, failing the test without one.
func row(t *testing.T, st model.GitHubStorage, repo string) model.RepoStorage {
	t.Helper()
	for _, r := range st.Repos {
		if r.Name == repo {
			return r
		}
	}
	t.Fatalf("no row for %s in %+v", repo, st.Repos)
	return model.RepoStorage{}
}

// eventsLike returns the messages of the events whose text contains sub.
func eventsLike(ev *events.Ring, sub string) []string {
	out := []string{}
	for _, e := range ev.After(0) {
		if strings.Contains(e.Msg, sub) {
			out = append(out, e.Msg)
		}
	}
	return out
}
```

Create `internal/ghstorage/service_test.go`:

```go
package ghstorage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

var forbidden = &github.APIError{Status: 403, Kind: github.ErrAuth, Message: "Resource not accessible by personal access token"}

func TestInventoryBuildsTheRows(t *testing.T) {
	gh := newFakeGH()
	cfg := testConfig()
	cfg.Repos = []config.Repo{{Name: "darkmem"}}
	cfg.WatchRepos = []string{"homelab"}
	cfg.GitHubStorage = config.GitHubStorage{CleanupMaxAge: config.Duration(48 * time.Hour),
		Repos: map[string]config.StorageOverride{"HOMELAB": {Cleanup: "off"}}}
	s, clock := newTestService(t, gh, cfg)
	now := clock.Now()
	expired := ghArt(3, 30, now, time.Hour, model.GiB)
	expired.Expired = true
	gh.addRepo("darkmem", true, ghArt(1, 10, now, 2*time.Hour, model.GiB), ghArt(2, 20, now, time.Hour, model.GiB), expired)
	gh.addRepo("homelab", false, ghArt(4, 40, now, time.Hour, 5))
	gh.addRepo("chicken-farm", true)
	gh.repos = append(gh.repos, github.UserRepo{Name: "elsewhere", Owner: github.Account{Login: "someone"}})
	gh.caches["darkmem"] = []github.Cache{{ID: 7, Key: "go-mod", Ref: "refs/heads/main", SizeInBytes: 100, LastAccessedAt: now}}

	v := s.Version()
	s.pass(true, false)
	st := s.Snapshot()
	if st.CheckedAt == nil || !st.CheckedAt.Equal(now) || s.Version() == v {
		t.Fatalf("CheckedAt %v, version %d → %d", st.CheckedAt, v, s.Version())
	}
	var names []string
	for _, r := range st.Repos {
		names = append(names, r.Name)
	}
	if want := []string{"darkmem", "homelab", "chicken-farm"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("rows %v, want %v (largest first, other owners left out)", names, want)
	}
	dm := row(t, st, "darkmem")
	if dm.ArtifactCount != 2 || dm.ArtifactBytes != 2*model.GiB || dm.Artifacts[0].ID != 2 || dm.Artifacts[1].RunID != 10 || dm.Artifacts[1].Branch != "main" {
		t.Errorf("darkmem artifacts %+v (expired dropped, newest first)", dm)
	}
	if dm.CacheCount != 1 || dm.CacheBytes != 100 || dm.Caches[0].Key != "go-mod" {
		t.Errorf("darkmem caches %+v", dm.Caches)
	}
	if dm.Visibility != "private" || !dm.Covered || dm.Cleanup != "2d" || dm.CleanupSource != "global" || dm.RetentionDays != 90 || dm.RetentionMaxDays != 400 {
		t.Errorf("darkmem row %+v", dm)
	}
	hl := row(t, st, "homelab")
	if hl.Visibility != "public" || !hl.Covered || hl.Cleanup != "off" || hl.CleanupSource != "override" {
		t.Errorf("homelab row %+v", hl)
	}
	cf := row(t, st, "chicken-farm")
	if cf.Covered || cf.Artifacts == nil || cf.Caches == nil {
		t.Errorf("chicken-farm row %+v", cf)
	}
	cfg.GitHubStorage.CleanupMaxAge = 0
	if r := row(t, s.Snapshot(), "darkmem"); r.Cleanup != "off" || r.CleanupSource != "none" {
		t.Errorf("after the policy changed: %s %s, want off none", r.Cleanup, r.CleanupSource)
	}
}

func TestRetentionIsReadOnceADay(t *testing.T) {
	gh := newFakeGH()
	gh.addRepo("darkmem", true)
	s, clock := newTestService(t, gh, testConfig())
	s.pass(true, false)
	clock.Add(23 * time.Hour)
	s.pass(true, false)
	if n := len(gh.callsLike("Retention darkmem")); n != 1 {
		t.Fatalf("%d retention reads in 23 hours, want 1", n)
	}
	clock.Add(time.Hour)
	s.pass(true, false)
	if n := len(gh.callsLike("Retention darkmem")); n != 2 {
		t.Fatalf("%d retention reads after a day, want 2", n)
	}
}

// tooManyPages is the error a real client returns for a listing longer than
// 50 pages.
func tooManyPages(t *testing.T) error {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", fmt.Sprintf(`<%s/next?%s>; rel="next"`, srv.URL, r.URL.RawQuery+"x"))
		fmt.Fprint(w, `{"artifacts":[]}`)
	}))
	defer srv.Close()
	c := github.New("darkraise", func() string { return "t" })
	c.BaseURL = srv.URL
	_, err := c.ListRepoArtifacts(context.Background(), "big")
	if !github.IsTooManyPages(err) {
		t.Fatalf("err = %v, want the 50-page error", err)
	}
	return err
}

func TestARepositoryErrorKeepsItsLastItems(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  func(t *testing.T) error
		want string
	}{
		{"missing permission", func(*testing.T) error { return forbidden }, "described: " + forbidden.Error()},
		{"too many pages", tooManyPages, "darkmem has more than 5,000 artifacts or caches, more than ghr lists: lower its retention"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := newFakeGH()
			s, clock := newTestService(t, gh, testConfig())
			s.Describe = func(err error, repo string) string { return "described: " + err.Error() }
			gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), time.Hour, 5))
			s.pass(true, false)
			gh.listErr["darkmem"] = tc.err(t)
			s.pass(true, false)
			r := row(t, s.Snapshot(), "darkmem")
			if r.Error != tc.want || r.ArtifactCount != 1 || r.Artifacts[0].ID != 1 {
				t.Fatalf("row %+v, want error %q and the last artifact", r, tc.want)
			}
		})
	}
}

func TestAFailedRepositoryListingKeepsTheRows(t *testing.T) {
	gh := newFakeGH()
	gh.addRepo("darkmem", true)
	s, _ := newTestService(t, gh, testConfig())
	s.pass(true, false)
	gh.reposErr = errors.New("boom")
	s.pass(true, false)
	st := s.Snapshot()
	if st.Error != "boom" || len(st.Repos) != 1 {
		t.Fatalf("error %q, %d rows; want boom and the last row", st.Error, len(st.Repos))
	}
}

func TestPassIsSkippedOnALowRateBudget(t *testing.T) {
	future := time.Date(2026, 10, 13, 13, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		remaining int
		suspended time.Time
		skipped   bool
	}{
		{"unknown budget", -1, time.Time{}, false},
		{"enough left", MinRateLeft, time.Time{}, false},
		{"too few left", MinRateLeft - 1, time.Time{}, true},
		{"suspended", 4000, future, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := newFakeGH()
			gh.remaining, gh.suspended = tc.remaining, tc.suspended
			s, _ := newTestService(t, gh, testConfig())
			s.pass(true, true)
			if skipped := len(gh.callsLike("")) == 0; skipped != tc.skipped {
				t.Fatalf("skipped = %v, want %v (calls %v)", skipped, tc.skipped, gh.callsLike(""))
			}
		})
	}
}

func TestATokenChangeDropsThePass(t *testing.T) {
	gh := newFakeGH()
	gh.addRepo("darkmem", true)
	gh.onList = func(context.Context, string) error {
		gh.mu.Lock()
		gh.gen++
		gh.mu.Unlock()
		return nil
	}
	s, _ := newTestService(t, gh, testConfig())
	s.pass(true, false)
	if st := s.Snapshot(); st.CheckedAt != nil || len(st.Repos) != 0 {
		t.Fatalf("snapshot %+v kept a pass made under the old token", st)
	}
}

// blockList makes the listing of repo wait until release is closed, and
// closes entered when it starts waiting.
func blockList(gh *fakeGH, repo string) (entered, release chan struct{}) {
	entered, release = make(chan struct{}), make(chan struct{})
	gh.onList = func(ctx context.Context, r string) error {
		if r == repo {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}
	return entered, release
}

func TestADeleteIsNotUndoneByAListingUnderWay(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), time.Hour, 5), ghArt(2, 20, clock.Now(), time.Hour, 5))
	s.pass(true, false)
	entered, release := blockList(gh, "darkmem")
	done := make(chan struct{})
	go func() { s.pass(true, false); close(done) }()
	<-entered
	s.mu.Lock()
	s.removeItem("artifact", "darkmem", 1)
	s.mu.Unlock()
	close(release)
	<-done
	r := row(t, s.Snapshot(), "darkmem")
	if r.ArtifactCount != 1 || r.Artifacts[0].ID != 2 {
		t.Fatalf("artifacts %+v, want only 2: the listing still held 1", r.Artifacts)
	}
}

func TestRefreshIsRefusedWhileAPassRuns(t *testing.T) {
	gh := newFakeGH()
	gh.addRepo("darkmem", true)
	s, _ := newTestService(t, gh, testConfig())
	entered, release := blockList(gh, "darkmem")
	done := make(chan struct{})
	go func() { s.pass(true, false); close(done) }()
	<-entered
	if err := s.Refresh(); !errors.Is(err, ErrBusy) {
		t.Fatalf("Refresh during a pass = %v, want ErrBusy", err)
	}
	close(release)
	<-done
	if err := s.Refresh(); err != nil || len(s.kick) != 1 {
		t.Fatalf("Refresh after the pass = %v with %d kicks queued, want nil and 1", err, len(s.kick))
	}
}

func TestCloseStopsAPassUnderWay(t *testing.T) {
	gh := newFakeGH()
	gh.addRepo("darkmem", true)
	s, _ := newTestService(t, gh, testConfig())
	entered, _ := blockList(gh, "darkmem")
	s.Start()
	<-entered // Start runs the first pass at once
	s.Close()
	stopped := make(chan struct{})
	go func() { s.Wait(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Wait did not return after Close")
	}
	if st := s.Snapshot(); st.CheckedAt != nil {
		t.Fatalf("a cancelled pass wrote its result: %+v", st)
	}
	if err := s.Refresh(); !errors.Is(err, ErrClosed) {
		t.Fatalf("Refresh after Close = %v, want ErrClosed", err)
	}
}

// october is a usage answer of 300 GB-hours of Actions storage and 10 of
// Packages, 4 of them on the last full day before 2026-10-13 12:00.
func october() []github.UsageItem {
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }
	return []github.UsageItem{
		{Date: day(5), SKU: github.SKUActionsStorage, Repo: "darkmem", Quantity: 300},
		{Date: day(11), SKU: github.SKUPackagesStorage, Quantity: 6},
		{Date: day(12), SKU: github.SKUPackagesStorage, Quantity: 4},
	}
}

func TestBillingReadsTheQuota(t *testing.T) {
	gh := newFakeGH()
	gh.usage = october()
	gh.summary = []github.SummaryItem{{SKU: github.SKUActionsStorage, Net: 0.25}, {SKU: github.SKUPackagesStorage, Net: 0.25}}
	gh.addRepo("darkmem", true, ghArt(1, 10, time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC), time.Hour, model.GiB))
	s, clock := newTestService(t, gh, testConfig())
	s.pass(true, true)
	st := s.Snapshot()
	q := st.Quota
	if q == nil || st.QuotaError != "" {
		t.Fatalf("quota %+v, error %q", q, st.QuotaError)
	}
	if q.Month != "2026-10" || !q.ReadAt.Equal(clock.Now()) || q.UsedGBHours != 310 || q.ActionsGBHours != 300 || q.PackagesGBHours != 10 {
		t.Errorf("quota %+v", q)
	}
	// 1 GiB of artifacts plus 4 GB-hours of Packages a day.
	if q.RateGBHoursPerHour != 1+4.0/24 || q.OverAllowanceGBHours == nil || *q.OverAllowanceGBHours != 0.5 || q.Level != "exhausted" {
		t.Errorf("rate %v, over %v, level %q", q.RateGBHoursPerHour, q.OverAllowanceGBHours, q.Level)
	}
	if a := s.Alert(); a == nil || a.Level != "exhausted" || a.UsedPct != 85 {
		t.Errorf("alert %+v", a)
	}
	s.pass(false, true)
	if got := eventsLike(s.Events, "github storage: quota"); len(got) != 1 || got[0] != "github storage: quota exhausted: 310.0 of 363.3 GB-hours used in 2026-10" {
		t.Errorf("level events %q, want one", got)
	}
	if n := len(gh.callsLike("OwnerType")); n != 1 {
		t.Errorf("owner type read %d times, want once", n)
	}
	if got := gh.callsLike("StorageUsage"); got[0] != "StorageUsage false 2026-10" {
		t.Errorf("usage read as %q", got[0])
	}
}

func TestBillingErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		set   func(gh *fakeGH)
		want  string
		usage bool // whether the usage endpoint is called
	}{
		{"another user's token", func(gh *fakeGH) { gh.login = "someone" },
			"the quota can only be read with a token of darkraise", false},
		{"no Plan permission", func(gh *fakeGH) { gh.usageErr = forbidden },
			"the token cannot read darkraise's billing: grant the account permission Plan: read", true},
		{"an organization without admin", func(gh *fakeGH) { gh.isOrg, gh.login, gh.usageErr = true, "someone", forbidden },
			"an organization admin's token with Administration: read is needed", true},
		{"anything else", func(gh *fakeGH) { gh.usageErr = errors.New("boom") }, "boom", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := newFakeGH()
			tc.set(gh)
			s, _ := newTestService(t, gh, testConfig())
			s.pass(false, true)
			st := s.Snapshot()
			if st.QuotaError != tc.want || st.Quota != nil {
				t.Errorf("quota error %q, quota %+v; want %q", st.QuotaError, st.Quota, tc.want)
			}
			if called := len(gh.callsLike("StorageUsage")) > 0; called != tc.usage {
				t.Errorf("usage called = %v, want %v", called, tc.usage)
			}
		})
	}
}

func TestAFailedBillingReadKeepsTheLastQuota(t *testing.T) {
	gh := newFakeGH()
	gh.usage = october()
	s, clock := newTestService(t, gh, testConfig())
	s.pass(false, true)
	read := clock.Now()
	clock.Add(time.Hour)
	gh.usageErr = forbidden
	s.pass(false, true)
	st := s.Snapshot()
	if st.Quota == nil || !st.Quota.ReadAt.Equal(read) || st.QuotaError == "" {
		t.Fatalf("quota %+v, error %q; want the last read kept with the error", st.Quota, st.QuotaError)
	}
	gh.usageErr = nil
	gh.sumErr = errors.New("preview gone")
	s.pass(false, true)
	if q := s.Snapshot().Quota; q == nil || q.OverAllowanceGBHours != nil || !q.ReadAt.Equal(clock.Now()) {
		t.Fatalf("quota %+v: a failed summary must not fail the read", q)
	}
}

func TestTheMonthChangeClearsTheQuota(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error // November's read
	}{
		{"until November's read returns", nil},
		{"while November's read fails", errors.New("boom")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gh := newFakeGH()
			gh.usage = october()
			s, clock := newTestService(t, gh, testConfig())
			clock.Add(18*24*time.Hour + 11*time.Hour) // 2026-10-31 23:00
			s.pass(false, true)
			if q := s.Snapshot().Quota; q == nil || q.Month != "2026-10" {
				t.Fatalf("quota %+v", q)
			}
			clock.Add(time.Hour + time.Minute)
			entered, release := make(chan struct{}), make(chan struct{})
			gh.onUsage = func(context.Context) error {
				close(entered)
				<-release
				return tc.err
			}
			done := make(chan struct{})
			go func() { s.pass(false, true); close(done) }()
			<-entered
			if q, a := s.Snapshot().Quota, s.Alert(); q != nil || a != nil {
				t.Fatalf("during November's read: quota %+v, alert %+v; want October's gone", q, a)
			}
			close(release)
			<-done
			st := s.Snapshot()
			if tc.err != nil {
				if st.Quota != nil || st.QuotaError != "boom" {
					t.Fatalf("quota %+v, error %q; want none and boom", st.Quota, st.QuotaError)
				}
				return
			}
			if st.Quota == nil || st.Quota.Month != "2026-11" {
				t.Fatalf("quota %+v, want November's", st.Quota)
			}
			if got := gh.callsLike("StorageUsage"); got[len(got)-1] != "StorageUsage false 2026-11" {
				t.Fatalf("last usage read %q", got[len(got)-1])
			}
		})
	}
}

func TestBillingDelay(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, tc := range []struct {
		now  string
		want time.Duration
	}{
		{"2026-10-13T12:00:00Z", BillingInterval},
		{"2026-10-31T23:00:00Z", time.Hour},
		{"2026-10-31T23:30:00Z", 31 * time.Minute},
		{"2026-10-31T23:59:30Z", 90 * time.Second},
		{"2026-11-01T00:01:00Z", BillingInterval},
		{"2026-12-31T23:20:00Z", 41 * time.Minute},
		{"2026-10-31T18:30:00-05:00", 31 * time.Minute}, // 23:30 UTC: the month is UTC's, not the local one
	} {
		if got := billingDelay(at(tc.now)); got != tc.want {
			t.Errorf("billingDelay(%s) = %v, want %v", tc.now, got, tc.want)
		}
	}
}

func TestPolicyChangedRecomputesWithoutGitHub(t *testing.T) {
	gh := newFakeGH()
	gh.usage = october()
	cfg := testConfig()
	s, _ := newTestService(t, gh, cfg)
	s.pass(false, true)
	calls := len(gh.callsLike(""))
	cfg.GitHubStorage.IncludedMB = 1024
	v := s.Version()
	s.PolicyChanged()
	q := s.Snapshot().Quota
	if q.IncludedGBHours != 744 || q.Level != "" || s.Version() == v || len(gh.callsLike("")) != calls {
		t.Fatalf("included %v, level %q, version %d → %d, calls %d → %d", q.IncludedGBHours, q.Level, v, s.Version(), calls, len(gh.callsLike("")))
	}
	if got := eventsLike(s.Events, "github storage: back under the warning levels for 2026-10"); len(got) != 1 {
		t.Errorf("events %q, want the level's return", eventsLike(s.Events, ""))
	}
}

func TestTokenChangedClearsErrorsAndKicks(t *testing.T) {
	gh := newFakeGH()
	gh.addRepo("darkmem", true)
	gh.listErr["darkmem"] = forbidden
	gh.login = "someone"
	s, _ := newTestService(t, gh, testConfig())
	s.pass(true, true)
	if st := s.Snapshot(); st.QuotaError == "" || row(t, st, "darkmem").Error == "" {
		t.Fatalf("snapshot %+v, want both errors", st)
	}
	s.TokenChanged()
	st := s.Snapshot()
	if st.QuotaError != "" || row(t, st, "darkmem").Error != "" || len(s.kick) != 1 {
		t.Fatalf("after TokenChanged: %+v, %d kicks", st, len(s.kick))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/ghstorage/
```
Expected: the build fails, starting with:

```
internal/ghstorage/fake_test.go:257:69: undefined: Service
internal/ghstorage/fake_test.go:263:8: undefined: Service
internal/ghstorage/service_test.go:157:19: undefined: MinRateLeft
internal/ghstorage/service_test.go:158:20: undefined: MinRateLeft
internal/ghstorage/service_test.go:235:41: undefined: ErrBusy
```

- [ ] **Step 3: Write the implementation**

Create `internal/ghstorage/service.go`:

```go
package ghstorage

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// GitHub is the part of the GitHub client the service uses.
type GitHub interface {
	OwnerType(ctx context.Context) (bool, error)
	TokenLogin(ctx context.Context) (string, error)
	StorageUsage(ctx context.Context, isOrg bool, year int, month time.Month) ([]github.UsageItem, error)
	StorageSummary(ctx context.Context, isOrg bool, year int, month time.Month) ([]github.SummaryItem, error)
	ListRepoArtifacts(ctx context.Context, repo string) ([]github.Artifact, error)
	ListCaches(ctx context.Context, repo string) ([]github.Cache, error)
	DeleteArtifact(ctx context.Context, repo string, id int64) (bool, error)
	DeleteCache(ctx context.Context, repo string, id int64) (bool, error)
	Retention(ctx context.Context, repo string) (github.Retention, error)
	SetRetention(ctx context.Context, repo string, days int) error
	ListUserRepos(ctx context.Context) ([]github.UserRepo, error)
	ListRuns(ctx context.Context, repo, status string) ([]github.Run, error)
	TokenMeta() github.TokenMeta
	SuspendedUntil() time.Time
	RateRemaining() int
}

var (
	ErrBusy   = errors.New("a GitHub storage check is already running")
	ErrClosed = errors.New("ghr is shutting down")
)

const (
	InventoryInterval = 15 * time.Minute
	BillingInterval   = time.Hour
	// Workers is how many repositories an inventory lists at once.
	Workers = 4
	// MinRateLeft is the token's hourly calls a pass leaves to the runner
	// manager: below it the pass is skipped.
	MinRateLeft = 500
	// MaxDeletions is how many deletions the snapshot keeps.
	MaxDeletions = 200
	// retentionTTL is how often a repository's retention is read.
	retentionTTL = 24 * time.Hour
	// deletedTTL is how long a deleted item is kept out of listings that
	// started before its delete.
	deletedTTL = time.Hour
)

// Service keeps the GitHub storage snapshot. Start it once; Close stops it.
type Service struct {
	GH       GitHub
	Config   func() *config.Config
	Events   *events.Ring
	Now      func() time.Time                    // nil means time.Now
	NewID    func() string                       // operation ids; nil makes random ones
	Describe func(err error, repo string) string // a repository's error text; nil means err.Error()
	Wake     func()                              // called after every snapshot change; must not block

	version atomic.Int64

	mu        sync.Mutex
	closed    bool
	running   bool // a pass runs
	checkedAt time.Time
	repos     []model.RepoStorage // sorted by artifact bytes, largest first
	listErr   string
	billing   *Billing
	quota     *model.GitHubQuota
	quotaErr  string
	level     string // the level the last event was written for
	isOrg     *bool  // read once; the owner does not change while the daemon runs
	retention map[string]retentionRead
	deleted   map[string]time.Time // "artifact:<id>" or "cache:<id>" → when deleted
	deletions []model.Deletion     // newest first

	ctx    context.Context
	cancel context.CancelFunc
	kick   chan struct{}
	wg     sync.WaitGroup
}

type retentionRead struct {
	at  time.Time
	r   github.Retention
	err string
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) describe(err error, repo string) string {
	if s.Describe != nil {
		return s.Describe(err, repo)
	}
	return err.Error()
}

// bump marks a snapshot change. It may run under s.mu: Wake must not block.
func (s *Service) bump() {
	s.version.Add(1)
	if s.Wake != nil {
		s.Wake()
	}
}

func (s *Service) init() {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.kick = make(chan struct{}, 1)
	s.retention = map[string]retentionRead{}
	s.deleted = map[string]time.Time{}
}

// Start runs a full pass at once, then the inventory every
// InventoryInterval and the billing read every BillingInterval and just
// after the month changes.
func (s *Service) Start() {
	s.init()
	s.kick <- struct{}{}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.loop()
	}()
}

func (s *Service) loop() {
	inv := time.NewTimer(InventoryInterval)
	bill := time.NewTimer(BillingInterval)
	defer inv.Stop()
	defer bill.Stop()
	for {
		inventory, billing := false, false
		select {
		case <-s.ctx.Done():
			return
		case <-s.kick:
			inventory, billing = true, true
		case <-inv.C:
			inventory = true
		case <-bill.C:
			billing = true
		}
		s.pass(inventory, billing)
		if inventory {
			inv.Reset(InventoryInterval)
		}
		if billing {
			bill.Reset(billingDelay(s.now()))
		}
	}
}

// billingDelay is how long after now the next billing read runs:
// BillingInterval, or a minute after the month changes when that is sooner.
func billingDelay(now time.Time) time.Duration {
	_, resets := MonthBounds(now)
	return min(BillingInterval, resets.Add(time.Minute).Sub(now))
}

// Kick asks for a full pass; kicks during a pass coalesce into one after it.
func (s *Service) Kick() {
	if s.kick == nil {
		return
	}
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Refresh asks for a full pass now; ErrBusy while one runs.
func (s *Service) Refresh() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return ErrClosed
	case s.running:
		return ErrBusy
	}
	s.Kick()
	return nil
}

// TokenChanged clears what the old token's answers said, then asks for a
// full pass with the new one.
func (s *Service) TokenChanged() {
	s.mu.Lock()
	for i := range s.repos {
		s.repos[i].Error = ""
	}
	s.listErr, s.quotaErr = "", ""
	s.retention = map[string]retentionRead{}
	s.bump()
	s.mu.Unlock()
	s.Kick()
}

// PolicyChanged recomputes the quota from the last billing read with the
// current config, without calling GitHub.
func (s *Service) PolicyChanged() {
	cfg := s.Config()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recompute(cfg)
	s.bump()
}

// Version rises on every snapshot change.
func (s *Service) Version() int64 { return s.version.Load() }

// Close stops the passes and any operation; Wait returns once they stopped.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Service) Wait() { s.wg.Wait() }

// pass reads the config once and runs an inventory, a billing read, or both.
// It is skipped while GitHub's rate limit is low or suspends calls.
func (s *Service) pass(inventory, billing bool) {
	cfg := s.Config()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.running = true
	// Last month's numbers are never shown as this month's.
	if s.quota != nil && !s.now().Before(s.quota.ResetsAt) {
		s.quota, s.billing = nil, nil
		s.bump()
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()
	if !s.GH.SuspendedUntil().IsZero() {
		return
	}
	if n := s.GH.RateRemaining(); n >= 0 && n < MinRateLeft {
		return
	}
	gen := s.GH.TokenMeta().Generation
	if inventory {
		s.inventory(cfg, gen)
	}
	if billing {
		s.readBilling(cfg, gen)
	}
}

// keep reports whether a pass started under token generation gen may write
// its result: not once the service closed or the token changed.
func (s *Service) keep(gen uint64) bool {
	return s.ctx.Err() == nil && s.GH.TokenMeta().Generation == gen
}

func (s *Service) readBilling(cfg *config.Config, gen uint64) {
	ctx := s.ctx
	fail := func(msg string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.keep(gen) {
			s.quotaErr = msg
			s.bump()
		}
	}
	s.mu.Lock()
	known := s.isOrg
	s.mu.Unlock()
	isOrg := false
	if known != nil {
		isOrg = *known
	} else {
		v, err := s.GH.OwnerType(ctx)
		if err != nil {
			fail(billingErr(err, cfg.Owner, false))
			return
		}
		isOrg = v
		s.mu.Lock()
		s.isOrg = &v
		s.mu.Unlock()
	}
	// A user's billing endpoint reads the token user's own billing.
	if !isOrg {
		login, err := s.GH.TokenLogin(ctx)
		if err != nil {
			fail(billingErr(err, cfg.Owner, false))
			return
		}
		if !strings.EqualFold(login, cfg.Owner) {
			fail("the quota can only be read with a token of " + cfg.Owner)
			return
		}
	}
	now := s.now()
	year, month, _ := now.UTC().Date()
	items, err := s.GH.StorageUsage(ctx, isOrg, year, month)
	if err != nil {
		fail(billingErr(err, cfg.Owner, isOrg))
		return
	}
	b := Billing{Month: now.UTC().Format("2006-01"), ReadAt: now}
	b.Actions, b.Packages, b.PackagesDaily = SumUsage(items, now)
	// The summary is a public preview: its failure never fails the read.
	if sum, err := s.GH.StorageSummary(ctx, isOrg, year, month); err == nil {
		over := 0.0
		for _, it := range sum {
			over += it.Net
		}
		b.Over = &over
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.keep(gen) {
		return
	}
	s.billing, s.quotaErr = &b, ""
	s.recompute(cfg)
	s.bump()
}

func billingErr(err error, owner string, isOrg bool) string {
	var ge *github.APIError
	if errors.As(err, &ge) && ge.Kind == github.ErrAuth && ge.Status == http.StatusForbidden {
		if isOrg {
			return "an organization admin's token with Administration: read is needed"
		}
		return "the token cannot read " + owner + "'s billing: grant the account permission Plan: read"
	}
	return err.Error()
}

// recompute rebuilds the quota from the last billing read and the artifact
// bytes now, and writes an event when the level changes. Call with s.mu held.
func (s *Service) recompute(cfg *config.Config) {
	if s.billing == nil {
		s.quota = nil
		return
	}
	var bytes int64
	for _, r := range s.repos {
		bytes += r.ArtifactBytes
	}
	q := ComputeQuota(*s.billing, bytes, cfg.GitHubStorage.Included(), cfg.GitHubStorage.Warn())
	s.quota = &q
	if q.Level == s.level {
		return
	}
	s.level = q.Level
	switch q.Level {
	case "":
		s.Events.Add("ok", "", "github storage: back under the warning levels for %s", q.Month)
	case "exhausted", "critical":
		s.Events.Add("error", "", "github storage: quota %s: %.1f of %.1f GB-hours used in %s", q.Level, q.UsedGBHours, q.IncludedGBHours, q.Month)
	default:
		s.Events.Add("warn", "", "github storage: quota %s: %.1f of %.1f GB-hours used in %s", q.Level, q.UsedGBHours, q.IncludedGBHours, q.Month)
	}
}

// listing is one repository's inventory result.
type listing struct {
	name      string
	private   bool
	artifacts []model.ArtifactItem
	caches    []model.CacheItem
	err       error
	retention *retentionRead // nil when not read this pass
}

func (s *Service) inventory(cfg *config.Config, gen uint64) {
	ctx := s.ctx
	all, err := s.GH.ListUserRepos(ctx)
	if err != nil {
		s.mu.Lock()
		if s.keep(gen) {
			s.listErr = s.describe(err, "")
			s.bump()
		}
		s.mu.Unlock()
		return
	}
	var ls []*listing
	for _, r := range all {
		if strings.EqualFold(r.Owner.Login, cfg.Owner) {
			ls = append(ls, &listing{name: r.Name, private: r.Private})
		}
	}
	now := s.now()
	s.mu.Lock()
	due := map[string]bool{}
	for _, l := range ls {
		if rr, ok := s.retention[strings.ToLower(l.name)]; !ok || now.Sub(rr.at) >= retentionTTL {
			due[l.name] = true
		}
	}
	s.mu.Unlock()
	sem := make(chan struct{}, Workers)
	var wg sync.WaitGroup
	for _, l := range ls {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			s.list(ctx, l, due[l.name], now)
		}()
	}
	wg.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.keep(gen) {
		return
	}
	s.merge(ls, now)
	s.checkedAt, s.listErr = now, ""
	s.recompute(cfg)
	s.bump()
}

func (s *Service) list(ctx context.Context, l *listing, readRetention bool, now time.Time) {
	arts, err := s.GH.ListRepoArtifacts(ctx, l.name)
	if err != nil {
		l.err = err
		return
	}
	caches, err := s.GH.ListCaches(ctx, l.name)
	if err != nil {
		l.err = err
		return
	}
	l.artifacts = []model.ArtifactItem{}
	for _, a := range arts {
		if !a.Expired {
			l.artifacts = append(l.artifacts, model.ArtifactItem{ID: a.ID, Name: a.Name, Bytes: a.SizeInBytes,
				CreatedAt: a.CreatedAt, ExpiresAt: a.ExpiresAt, RunID: a.WorkflowRun.ID, Branch: a.WorkflowRun.HeadBranch})
		}
	}
	l.caches = []model.CacheItem{}
	for _, c := range caches {
		l.caches = append(l.caches, model.CacheItem{ID: c.ID, Key: c.Key, Ref: c.Ref, Bytes: c.SizeInBytes, LastUsedAt: c.LastAccessedAt})
	}
	if readRetention {
		rr := retentionRead{at: now}
		if r, err := s.GH.Retention(ctx, l.name); err != nil {
			rr.err = s.describe(err, l.name)
		} else {
			rr.r = r
		}
		l.retention = &rr
	}
}

// merge replaces the rows with the listings. A repository whose listing
// failed keeps its last items with the error. Items deleted since the
// listing started are left out, so a delete is never undone by a listing
// that was already under way. Call with s.mu held.
func (s *Service) merge(ls []*listing, now time.Time) {
	for k, at := range s.deleted {
		if now.Sub(at) >= deletedTTL {
			delete(s.deleted, k)
		}
	}
	prev := map[string]model.RepoStorage{}
	for _, r := range s.repos {
		prev[strings.ToLower(r.Name)] = r
	}
	rows := make([]model.RepoStorage, 0, len(ls))
	for _, l := range ls {
		key := strings.ToLower(l.name)
		if l.retention != nil {
			s.retention[key] = *l.retention
		}
		row := model.RepoStorage{Name: l.name, Visibility: "public", Artifacts: []model.ArtifactItem{}, Caches: []model.CacheItem{}}
		if l.private {
			row.Visibility = "private"
		}
		if l.err != nil {
			old := prev[key]
			row.Artifacts, row.Caches = old.Artifacts, old.Caches
			row.Error = s.describe(l.err, l.name)
			if github.IsTooManyPages(l.err) {
				row.Error = l.name + " has more than 5,000 artifacts or caches, more than ghr lists: lower its retention"
			}
		} else {
			for _, a := range l.artifacts {
				if _, gone := s.deleted[itemKey("artifact", a.ID)]; !gone {
					row.Artifacts = append(row.Artifacts, a)
				}
			}
			for _, c := range l.caches {
				if _, gone := s.deleted[itemKey("cache", c.ID)]; !gone {
					row.Caches = append(row.Caches, c)
				}
			}
		}
		if rr, ok := s.retention[key]; ok {
			row.RetentionDays, row.RetentionMaxDays = rr.r.Days, rr.r.MaxDays
			if row.Error == "" {
				row.Error = rr.err
			}
		}
		rows = append(rows, row)
	}
	s.repos = rows
	s.sortRows()
}

// sortRows recounts every row and sorts the rows by artifact bytes, largest
// first, and each row's items newest first. Call with s.mu held.
func (s *Service) sortRows() {
	for i := range s.repos {
		r := &s.repos[i]
		r.ArtifactCount, r.ArtifactBytes, r.CacheCount, r.CacheBytes = len(r.Artifacts), 0, len(r.Caches), 0
		for _, a := range r.Artifacts {
			r.ArtifactBytes += a.Bytes
		}
		for _, c := range r.Caches {
			r.CacheBytes += c.Bytes
		}
		slices.SortStableFunc(r.Artifacts, func(a, b model.ArtifactItem) int { return b.CreatedAt.Compare(a.CreatedAt) })
		slices.SortStableFunc(r.Caches, func(a, b model.CacheItem) int { return b.LastUsedAt.Compare(a.LastUsedAt) })
	}
	slices.SortStableFunc(s.repos, func(a, b model.RepoStorage) int {
		if c := cmp.Compare(b.ArtifactBytes, a.ArtifactBytes); c != 0 {
			return c
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
}

func itemKey(kind string, id int64) string { return fmt.Sprintf("%s:%d", kind, id) }

// removeItem takes a deleted artifact or cache out of its row and keeps
// later listings from bringing it back. Call with s.mu held.
func (s *Service) removeItem(kind, repo string, id int64) {
	s.deleted[itemKey(kind, id)] = s.now()
	for i := range s.repos {
		r := &s.repos[i]
		if !strings.EqualFold(r.Name, repo) {
			continue
		}
		if kind == "artifact" {
			r.Artifacts = slices.DeleteFunc(r.Artifacts, func(a model.ArtifactItem) bool { return a.ID == id })
		} else {
			r.Caches = slices.DeleteFunc(r.Caches, func(c model.CacheItem) bool { return c.ID == id })
		}
	}
	s.sortRows()
}

// Snapshot is the last inventory and billing read with the operation and
// the deletions. Each row's cleanup and coverage come from the current
// config, so a saved policy shows at once.
func (s *Service) Snapshot() model.GitHubStorage {
	cfg := s.Config()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := model.GitHubStorage{
		QuotaError: s.quotaErr,
		Error:      s.listErr,
		Repos:      make([]model.RepoStorage, 0, len(s.repos)),
		Deletions:  append([]model.Deletion{}, s.deletions...),
	}
	if !s.checkedAt.IsZero() {
		at := s.checkedAt
		out.CheckedAt = &at
	}
	if s.quota != nil {
		q := *s.quota
		out.Quota = &q
	}
	for _, r := range s.repos {
		r.Artifacts = append([]model.ArtifactItem{}, r.Artifacts...)
		r.Caches = append([]model.CacheItem{}, r.Caches...)
		age, src := cfg.GitHubStorage.CleanupFor(r.Name)
		r.Cleanup, r.CleanupSource = config.CleanupText(age), src
		r.Covered = cfg.Repo(r.Name) != nil || cfg.Watched(r.Name)
		out.Repos = append(out.Repos, r)
	}
	return out
}

// Alert is the quota's status alert, nil until a billing read succeeds.
func (s *Service) Alert() *model.StorageAlert {
	s.mu.Lock()
	defer s.mu.Unlock()
	return AlertFrom(s.quota)
}
```

- [ ] **Step 4: Run the tests to verify they pass, also under the race detector**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/ghstorage/
```
```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -race -count=5 ./internal/ghstorage/
```
Expected: `ok  	github.com/darkraise/ghr/internal/ghstorage` both times (the race run builds with cgo; `gcc` is on the dev host).

- [ ] **Step 5: Format, vet and commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./internal/... ./cmd/...
```
Expected: `gofmt` prints nothing and `go vet` passes.

```bash
git add internal/ghstorage/service.go internal/ghstorage/service_test.go internal/ghstorage/fake_test.go
git commit -m "feat(ghstorage): run the storage passes"
```

### Task 8: Run the cleanup within a write budget

**Files:**
- Create: `internal/ghstorage/cleanup_run.go`
- Modify: `internal/ghstorage/service.go` (two fields in `Service`; the end of `inventory`)
- Test: `internal/ghstorage/cleanup_run_test.go` (new)

**Interfaces:**
- Consumes: `Candidates`, `Oldest`, `Pick`, `ActiveStatuses` (Task 6); `Service`, the fake GitHub and helpers of `fake_test.go`, and the internals `keep`, `describe`, `bump`, `recompute`, `removeItem` (Task 7, Contracts **ghstorage internals**).
- Produces: Contracts **Service (Tasks 6 to 9)**, Task 8: `MaxCleanupDeletes`, `WriteBudget`, `BudgetError`, the log line and event texts; the internals `write`, `activeRuns`, `deleteArtifact`, `recordDeletion`, `setRowError`, `sizeText`, `ageText` that Task 9 uses.

**Items:** 2

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

Spec §2.1 and §2.3. Every write goes through `write`: one at a time under `writeMu`, and at most 300 in any rolling hour, past which it answers a `*BudgetError` naming when the oldest write leaves the hour. The cleanup runs at the end of each inventory, with the config the pass read. It lists the not-completed runs (five statuses) only for repositories that have candidates, deletes at most 100 artifacts oldest first across repositories, stops a repository at its first error other than a 404 and records that error on its row, and stops the whole pass when the budget is used up. A 404 takes the artifact out of the snapshot but records nothing. Each confirmed delete is recorded in `Deletions` (newest first, at most 200) and logged; each repository gets one event per pass; and the quota is recomputed when anything was deleted, since the rate depends on the bytes stored.

- [ ] **Step 1: Write the failing tests**

Create `internal/ghstorage/cleanup_run_test.go`:

```go
package ghstorage

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

func cleanupConfig() *config.Config {
	cfg := testConfig()
	cfg.GitHubStorage = config.GitHubStorage{CleanupMaxAge: config.Duration(24 * time.Hour), Repos: map[string]config.StorageOverride{
		"homelab": {Cleanup: "off"}, "chicken-farm": {Cleanup: "6h"},
	}}
	return cfg
}

func artifactIDs(r model.RepoStorage) []int64 {
	out := []int64{}
	for _, a := range r.Artifacts {
		out = append(out, a.ID)
	}
	return out
}

func TestCleanupDeletesArtifactsOlderThanTheAge(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, cleanupConfig())
	now := clock.Now()
	gh.addRepo("darkmem", true, ghArt(1, 10, now, 30*time.Hour, model.GiB), ghArt(2, 20, now, time.Hour, 5), ghArt(3, 30, now, 40*time.Hour, 5))
	gh.addRepo("homelab", true, ghArt(4, 40, now, 100*time.Hour, 5))
	gh.addRepo("chicken-farm", true, ghArt(5, 50, now, 7*time.Hour, 3<<20))
	gh.runs["darkmem"] = map[string][]github.Run{"in_progress": {{ID: 30}}}

	s.pass(true, false)
	if got := gh.callsLike("Delete"); !reflect.DeepEqual(got, []string{"DeleteArtifact darkmem 1", "DeleteArtifact chicken-farm 5"}) {
		t.Fatalf("deletes %v, want darkmem 1 then chicken-farm 5, oldest first", got)
	}
	for _, repo := range []string{"darkmem", "chicken-farm"} {
		for _, st := range ActiveStatuses {
			if len(gh.callsLike("ListRuns "+repo+" "+st)) != 1 {
				t.Errorf("runs of %s with status %s not listed", repo, st)
			}
		}
	}
	if got := gh.callsLike("ListRuns homelab"); len(got) != 0 {
		t.Errorf("homelab has no candidates but its runs were listed: %v", got)
	}
	st := s.Snapshot()
	if got := artifactIDs(row(t, st, "darkmem")); !reflect.DeepEqual(got, []int64{2, 3}) {
		t.Errorf("darkmem keeps %v, want 2 and 3 (3's run is in progress)", got)
	}
	if got := artifactIDs(row(t, st, "homelab")); !reflect.DeepEqual(got, []int64{4}) {
		t.Errorf("homelab keeps %v: its cleanup is off", got)
	}
	want := []model.Deletion{
		{At: now, Repo: "chicken-farm", Kind: "artifact", Name: "art-5", Bytes: 3 << 20, Age: "7h", By: "cleanup"},
		{At: now, Repo: "darkmem", Kind: "artifact", Name: "art-1", Bytes: model.GiB, Age: "30h", By: "cleanup"},
	}
	if !reflect.DeepEqual(st.Deletions, want) {
		t.Errorf("deletions %+v, want %+v", st.Deletions, want)
	}
	if got := eventsLike(s.Events, "cleanup deleted"); !reflect.DeepEqual(got, []string{"cleanup deleted 1 artifacts (1.0 GB)", "cleanup deleted 1 artifacts (3 MB)"}) {
		t.Errorf("events %q", got)
	}
}

func TestCleanupSkipsEveryRunNotCompleted(t *testing.T) {
	for _, status := range ActiveStatuses {
		t.Run(status, func(t *testing.T) {
			gh := newFakeGH()
			s, clock := newTestService(t, gh, cleanupConfig())
			gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), 30*time.Hour, 5))
			gh.runs["darkmem"] = map[string][]github.Run{status: {{ID: 10}}}
			s.pass(true, false)
			if got := gh.callsLike("Delete"); len(got) != 0 {
				t.Fatalf("deleted %v of a run that is %s", got, status)
			}
		})
	}
}

func TestCleanupDeletesAtMost100OldestFirst(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, cleanupConfig())
	var a, b []github.Artifact
	for i := range 60 {
		a = append(a, ghArt(int64(1000+i), int64(i), clock.Now(), time.Duration(100+2*i)*time.Hour, 1))
		b = append(b, ghArt(int64(2000+i), int64(i), clock.Now(), time.Duration(101+2*i)*time.Hour, 1))
	}
	gh.addRepo("a", true, a...)
	gh.addRepo("b", true, b...)
	s.pass(true, false)
	got := gh.callsLike("Delete")
	if len(got) != MaxCleanupDeletes || got[0] != "DeleteArtifact b 2059" || got[99] != "DeleteArtifact a 1010" {
		t.Fatalf("%d deletes from %v to %v, want 100 from b 2059 (oldest) to a 1010", len(got), got[0], got[len(got)-1])
	}
	if n := row(t, s.Snapshot(), "a").ArtifactCount + row(t, s.Snapshot(), "b").ArtifactCount; n != 20 {
		t.Fatalf("%d artifacts left, want the 20 youngest", n)
	}
}

func TestCleanupRecordsNothingForAnArtifactAlreadyGone(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, cleanupConfig())
	gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), 30*time.Hour, 5))
	gh.gone[1] = true
	s.pass(true, false)
	st := s.Snapshot()
	if row(t, st, "darkmem").ArtifactCount != 0 || len(st.Deletions) != 0 || len(eventsLike(s.Events, "cleanup")) != 0 {
		t.Fatalf("row %+v, deletions %+v: a 404 leaves the row and records nothing", row(t, st, "darkmem"), st.Deletions)
	}
}

func TestCleanupStopsARepositoryAtItsFirstError(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, cleanupConfig())
	now := clock.Now()
	gh.addRepo("darkmem", true, ghArt(1, 10, now, 50*time.Hour, 5), ghArt(2, 20, now, 40*time.Hour, 5))
	gh.addRepo("other", true, ghArt(3, 30, now, 45*time.Hour, 5))
	gh.delErr[1] = forbidden
	s.pass(true, false)
	if got := gh.callsLike("Delete"); !reflect.DeepEqual(got, []string{"DeleteArtifact darkmem 1", "DeleteArtifact other 3"}) {
		t.Fatalf("deletes %v, want darkmem to stop after its error", got)
	}
	if r := row(t, s.Snapshot(), "darkmem"); r.Error != forbidden.Error() || r.ArtifactCount != 2 {
		t.Fatalf("darkmem row %+v", r)
	}
}

func TestCleanupSkipsARepositoryWhoseRunsCannotBeListed(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, cleanupConfig())
	gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), 50*time.Hour, 5))
	gh.runsErr["darkmem"] = errors.New("boom")
	s.pass(true, false)
	if got := gh.callsLike("Delete"); len(got) != 0 {
		t.Fatalf("deleted %v without knowing which runs are in progress", got)
	}
	if r := row(t, s.Snapshot(), "darkmem"); r.Error != "boom" {
		t.Fatalf("row error %q, want boom", r.Error)
	}
}

func TestWritesStayWithinTheHourlyBudget(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	first := clock.Now()
	for i := range WriteBudget {
		if err := s.write(func() error { return nil }); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
		clock.Add(time.Second)
	}
	err := s.write(func() error { t.Fatal("ran past the budget"); return nil })
	var be *BudgetError
	if !errors.As(err, &be) || !be.RetryAt.Equal(first.Add(time.Hour)) {
		t.Fatalf("err = %v, want a BudgetError until %v", err, first.Add(time.Hour))
	}
	if want := "GitHub write budget used up; try again after 2026-10-13T13:00:00Z"; err.Error() != want {
		t.Fatalf("message %q, want %q", err.Error(), want)
	}
	clock.Add(time.Hour - WriteBudget*time.Second)
	if err := s.write(func() error { return nil }); err != nil {
		t.Fatalf("an hour after the first write: %v", err)
	}
}

func TestCleanupStopsWhenTheBudgetIsUsed(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, cleanupConfig())
	gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), 50*time.Hour, 5))
	for range WriteBudget {
		_ = s.write(func() error { return nil })
	}
	s.pass(true, false)
	if got := gh.callsLike("Delete"); len(got) != 0 {
		t.Fatalf("deleted %v past the budget", got)
	}
	if r := row(t, s.Snapshot(), "darkmem"); r.Error != "" || r.ArtifactCount != 1 {
		t.Fatalf("row %+v: the budget is not the repository's error", r)
	}
}

func TestDeletionsKeepTheNewest(t *testing.T) {
	s, _ := newTestService(t, newFakeGH(), testConfig())
	s.mu.Lock()
	for i := range MaxDeletions + 5 {
		s.recordDeletion(model.Deletion{Name: fmt.Sprint(i)})
	}
	s.mu.Unlock()
	d := s.Snapshot().Deletions
	if len(d) != MaxDeletions || d[0].Name != "204" || d[len(d)-1].Name != "5" {
		t.Fatalf("%d deletions from %s to %s, want 200 from 204 to 5", len(d), d[0].Name, d[len(d)-1].Name)
	}
}

func TestSizeAndAgeTexts(t *testing.T) {
	for in, want := range map[int64]string{0: "0 B", 1023: "1023 B", 2048: "2 KB", 340 << 20: "340 MB", 3 * model.GiB / 2: "1.5 GB"} {
		if got := sizeText(in); got != want {
			t.Errorf("sizeText(%d) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[time.Duration]string{-time.Minute: "0m", 59 * time.Minute: "59m", 30 * time.Hour: "30h", 50 * time.Hour: "2d"} {
		if got := ageText(in); got != want {
			t.Errorf("ageText(%v) = %q, want %q", in, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/ghstorage/
```
Expected: the build fails, starting with:

```
internal/ghstorage/cleanup_run_test.go:100:17: undefined: MaxCleanupDeletes
internal/ghstorage/cleanup_run_test.go:154:17: undefined: WriteBudget
internal/ghstorage/cleanup_run_test.go:155:15: s.write undefined (type *Service has no field or method write)
```

- [ ] **Step 3: Write the implementation**

Create `internal/ghstorage/cleanup_run.go`:

```go
package ghstorage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/model"
)

const (
	// MaxCleanupDeletes is how many artifacts one cleanup pass deletes; the
	// rest wait for the next pass.
	MaxCleanupDeletes = 100
	// WriteBudget is how many writes ghr's storage work makes in any hour,
	// below GitHub's secondary limit of 500, so the runner manager keeps at
	// least 200 for its own.
	WriteBudget = 300
)

// BudgetError refuses a write once WriteBudget writes were made in the last
// hour.
type BudgetError struct {
	RetryAt time.Time
}

func (e *BudgetError) Error() string {
	return "GitHub write budget used up; try again after " + e.RetryAt.UTC().Format(time.RFC3339)
}

// write runs one GitHub write. Writes run one at a time, so a runner start
// waiting for the client's write slot waits behind at most one of them.
func (s *Service) write(fn func() error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	now := s.now()
	s.writes = slices.DeleteFunc(s.writes, func(t time.Time) bool { return now.Sub(t) >= time.Hour })
	if len(s.writes) >= WriteBudget {
		return &BudgetError{RetryAt: s.writes[0].Add(time.Hour)}
	}
	s.writes = append(s.writes, now)
	return fn()
}

// activeRuns lists the ids of repo's runs that have not completed.
func (s *Service) activeRuns(ctx context.Context, repo string) (map[int64]bool, error) {
	active := map[int64]bool{}
	for _, status := range ActiveStatuses {
		runs, err := s.GH.ListRuns(ctx, repo, status)
		if err != nil {
			return nil, err
		}
		for _, r := range runs {
			active[r.ID] = true
		}
	}
	return active, nil
}

// cleanup deletes the artifacts older than each repository's effective
// cleanup age, oldest first across repositories and at most
// MaxCleanupDeletes, skipping those of runs that have not completed.
func (s *Service) cleanup(cfg *config.Config, gen uint64) {
	ctx := s.ctx
	now := s.now()
	type due struct {
		repo  string
		age   time.Duration
		items []model.ArtifactItem
	}
	var todo []due
	s.mu.Lock()
	for _, r := range s.repos {
		age, _ := cfg.GitHubStorage.CleanupFor(r.Name)
		if len(Candidates(r.Artifacts, age, now, nil)) > 0 {
			todo = append(todo, due{r.Name, age, slices.Clone(r.Artifacts)})
		}
	}
	s.mu.Unlock()
	var picks []Pick
	for _, d := range todo {
		active, err := s.activeRuns(ctx, d.repo)
		if err != nil {
			s.setRowError(d.repo, s.describe(err, d.repo), gen)
			continue
		}
		for _, it := range Candidates(d.items, d.age, now, active) {
			picks = append(picks, Pick{Repo: d.repo, Item: it})
		}
	}
	type tally struct {
		n     int
		bytes int64
	}
	tallies := map[string]*tally{}
	var order []string
	stopped := map[string]bool{}
	for _, p := range Oldest(picks, MaxCleanupDeletes) {
		if stopped[p.Repo] {
			continue
		}
		if !s.keep(gen) {
			break
		}
		deleted, err := s.deleteArtifact(ctx, p.Repo, p.Item, "cleanup")
		var be *BudgetError
		if errors.As(err, &be) {
			log.Printf("github storage: cleanup stopped: %v", err)
			break
		}
		if err != nil {
			stopped[p.Repo] = true
			s.setRowError(p.Repo, s.describe(err, p.Repo), gen)
			continue
		}
		if deleted {
			t := tallies[p.Repo]
			if t == nil {
				t = &tally{}
				tallies[p.Repo] = t
				order = append(order, p.Repo)
			}
			t.n++
			t.bytes += p.Item.Bytes
		}
	}
	for _, repo := range order {
		t := tallies[repo]
		s.Events.Add("info", repo, "cleanup deleted %d artifacts (%s)", t.n, sizeText(t.bytes))
	}
	if len(order) > 0 {
		s.mu.Lock()
		s.recompute(cfg)
		s.bump()
		s.mu.Unlock()
	}
}

func (s *Service) setRowError(repo, msg string, gen uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.keep(gen) {
		return
	}
	for i := range s.repos {
		if strings.EqualFold(s.repos[i].Name, repo) {
			s.repos[i].Error = msg
		}
	}
	s.bump()
}

// deleteArtifact deletes one artifact within the write budget. An artifact
// already gone (404) leaves the snapshot too, but only a delete GitHub
// confirmed is recorded and logged.
func (s *Service) deleteArtifact(ctx context.Context, repo string, it model.ArtifactItem, by string) (bool, error) {
	var deleted bool
	err := s.write(func() error {
		var err error
		deleted, err = s.GH.DeleteArtifact(ctx, repo, it.ID)
		return err
	})
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeItem("artifact", repo, it.ID)
	if deleted {
		now := s.now()
		age := ageText(now.Sub(it.CreatedAt))
		s.recordDeletion(model.Deletion{At: now, Repo: repo, Kind: "artifact", Name: it.Name, Bytes: it.Bytes, Age: age, By: by})
		log.Printf("github storage: deleted artifact %s (%s) in %s, age %s", it.Name, sizeText(it.Bytes), repo, age)
	}
	s.bump()
	return deleted, nil
}

// recordDeletion keeps the MaxDeletions newest deletions, newest first. Call
// with s.mu held.
func (s *Service) recordDeletion(d model.Deletion) {
	s.deletions = append([]model.Deletion{d}, s.deletions...)
	if len(s.deletions) > MaxDeletions {
		s.deletions = s.deletions[:MaxDeletions]
	}
}

func sizeText(n int64) string {
	switch {
	case n >= model.GiB:
		return fmt.Sprintf("%.1f GB", float64(n)/model.GiB)
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

func ageText(d time.Duration) string {
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%dd", d/(24*time.Hour))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", d/time.Hour)
	}
	return fmt.Sprintf("%dm", max(d, 0)/time.Minute)
}
```

In `internal/ghstorage/service.go`, replace these lines (the end of the `Service` struct's fields):

```go
	deleted   map[string]time.Time // "artifact:<id>" or "cache:<id>" → when deleted
	deletions []model.Deletion     // newest first

	ctx    context.Context
```

with:

```go
	deleted   map[string]time.Time // "artifact:<id>" or "cache:<id>" → when deleted
	deletions []model.Deletion     // newest first

	writeMu sync.Mutex
	writes  []time.Time // guarded by writeMu: the writes of the last hour

	ctx    context.Context
```

and replace these lines (the end of `inventory`):

```go
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.keep(gen) {
		return
	}
	s.merge(ls, now)
	s.checkedAt, s.listErr = now, ""
	s.recompute(cfg)
	s.bump()
}
```

with:

```go
	s.mu.Lock()
	if !s.keep(gen) {
		s.mu.Unlock()
		return
	}
	s.merge(ls, now)
	s.checkedAt, s.listErr = now, ""
	s.recompute(cfg)
	s.bump()
	s.mu.Unlock()
	s.cleanup(cfg, gen)
}
```

- [ ] **Step 4: Run the tests to verify they pass, also under the race detector**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/ghstorage/
```
```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -race -count=5 ./internal/ghstorage/
```
Expected: `ok  	github.com/darkraise/ghr/internal/ghstorage` both times.

- [ ] **Step 5: Format, vet and commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./internal/... ./cmd/...
```
Expected: `gofmt` prints nothing and `go vet` passes.

```bash
git add internal/ghstorage/cleanup_run.go internal/ghstorage/cleanup_run_test.go internal/ghstorage/service.go
git commit -m "feat(ghstorage): run the cleanup within a budget"
```

### Task 9: Run manual deletes and retention changes

**Files:**
- Create: `internal/ghstorage/ops.go`
- Modify: `internal/ghstorage/service.go` (two fields in `Service`; the operation in `Snapshot`)
- Test: `internal/ghstorage/ops_test.go` (new)

**Interfaces:**
- Consumes: `Candidates`, `Preview` (Task 6); `Service`, the fake GitHub and helpers of `fake_test.go`, `removeItem`, `describe`, `bump` (Task 7); `write`, `activeRuns`, `deleteArtifact`, `recordDeletion`, `BudgetError`, `sizeText`, `ageText` and the test helper `artifactIDs` of `cleanup_run_test.go` (Task 8); `config.ParseCleanup`, `config.ParseDuration` (Task 3); `model.DeleteArtifactsRequest`, `DeleteCachesRequest`, `GitHubOperation`, `OperationFailure` (Task 2); `github.APIError` and `github.ErrUnprocessable` (existing, `internal/github/client.go:29`).
- Produces: Contracts **Service (Tasks 6 to 9)**, Task 9: `ErrUnknownRepo`, `ErrOperationRunning`, `BadInput`, and the methods `Preview`, `DeleteArtifact`, `DeleteCache`, `DeleteArtifacts`, `DeleteCaches`, `SetRetention`, `SetRetentionAll`, which Task 10 calls.

**Items:** 3, 4

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

Spec §3.2. A repository is matched ignoring case against the snapshot; an unknown repository, or an item not in the snapshot for a single delete, is `ErrUnknownRepo`. Single deletes and a single retention change answer when done; bulk deletes and retention on all repositories start one background operation (one at a time, `ErrOperationRunning` otherwise) whose progress is the snapshot's `Operation` and whose end writes one event. A bulk body names exactly one of `ids` or `older_than` (at least 1h; it skips runs that have not completed, as the cleanup does), or for caches `ids` or `all`; an id not in the snapshot fails with "not in the last inventory". A bulk delete stops at its first error other than a 404, because the rest would fail the same way. Retention days run from 1 to the repository's maximum, or 400 while it is unknown; GitHub's 422 becomes a `BadInput` with GitHub's message, and a success is written into the snapshot at once. Retention on all skips a repository whose maximum is below the value and goes on past one that fails, but stops when the write budget is used. `Close` cancels a running operation through the service's context and `Wait` waits for it. All writes go through Task 8's `write`.

- [ ] **Step 1: Write the failing tests**

Create `internal/ghstorage/ops_test.go`:

```go
package ghstorage

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// blockDelete makes every delete wait until release is closed or the
// service closes, and closes entered when the first one starts waiting.
func blockDelete(gh *fakeGH) (entered, release chan struct{}) {
	entered, release = make(chan struct{}), make(chan struct{})
	var once sync.Once
	gh.onDelete = func(ctx context.Context, repo string, id int64) error {
		once.Do(func() { close(entered) })
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return entered, release
}

func TestDeleteArtifactsByID(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	now := clock.Now()
	gh.addRepo("darkmem", true, ghArt(1, 10, now, 2*time.Hour, 5), ghArt(2, 20, now, time.Hour, 7))
	s.pass(true, false)
	id, err := s.DeleteArtifacts("DARKMEM", model.DeleteArtifactsRequest{IDs: []int64{1, 99}})
	if err != nil || id != "op-1" {
		t.Fatalf("DeleteArtifacts = %q, %v", id, err)
	}
	s.Wait()
	st := s.Snapshot()
	want := &model.GitHubOperation{ID: "op-1", Kind: "delete-artifacts", Repo: "darkmem", Total: 2, Done: 1, Bytes: 5,
		Failed: []model.OperationFailure{{Repo: "darkmem", ID: 99, Error: "not in the last inventory"}}, StartedAt: now, FinishedAt: &now}
	if !reflect.DeepEqual(st.Operation, want) {
		t.Errorf("operation %+v, want %+v", st.Operation, want)
	}
	if got := artifactIDs(row(t, st, "darkmem")); !reflect.DeepEqual(got, []int64{2}) {
		t.Errorf("artifacts left %v, want [2]", got)
	}
	if want := []model.Deletion{{At: now, Repo: "darkmem", Kind: "artifact", Name: "art-1", Bytes: 5, Age: "2h", By: "manual"}}; !reflect.DeepEqual(st.Deletions, want) {
		t.Errorf("deletions %+v", st.Deletions)
	}
	if got := eventsLike(s.Events, "deleted"); !reflect.DeepEqual(got, []string{"deleted 1 artifacts (5 B); 1 failed"}) {
		t.Errorf("events %q", got)
	}
}

func TestDeleteArtifactsOlderThanSkipsRunsNotCompleted(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	now := clock.Now()
	gh.addRepo("darkmem", true, ghArt(1, 10, now, 30*time.Hour, 5), ghArt(2, 20, now, 30*time.Hour, 5), ghArt(3, 30, now, time.Hour, 5))
	gh.runs["darkmem"] = map[string][]github.Run{"waiting": {{ID: 20}}}
	s.pass(true, false)
	if _, err := s.DeleteArtifacts("darkmem", model.DeleteArtifactsRequest{OlderThan: "24h"}); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	if got := gh.callsLike("Delete"); !reflect.DeepEqual(got, []string{"DeleteArtifact darkmem 1"}) {
		t.Fatalf("deletes %v, want only 1", got)
	}
	if op := s.Snapshot().Operation; op.Total != 1 || op.Done != 1 || len(op.Failed) != 0 {
		t.Fatalf("operation %+v", op)
	}
}

func TestBulkRequestsAreChecked(t *testing.T) {
	gh := newFakeGH()
	gh.addRepo("darkmem", true)
	s, _ := newTestService(t, gh, testConfig())
	s.pass(true, false)
	arts := func(r model.DeleteArtifactsRequest) func() error {
		return func() error { _, err := s.DeleteArtifacts("darkmem", r); return err }
	}
	caches := func(r model.DeleteCachesRequest) func() error {
		return func() error { _, err := s.DeleteCaches("darkmem", r); return err }
	}
	for _, tc := range []struct {
		name string
		call func() error
		want error
	}{
		{"ids and an age", arts(model.DeleteArtifactsRequest{IDs: []int64{1}, OlderThan: "2d"}), BadInput("give either ids or older_than")},
		{"neither ids nor an age", arts(model.DeleteArtifactsRequest{IDs: []int64{}}), BadInput("give either ids or older_than")},
		{"an age under an hour", arts(model.DeleteArtifactsRequest{OlderThan: "30m"}), BadInput("older_than must be a duration of at least 1h, such as 24h")},
		{"no duration", arts(model.DeleteArtifactsRequest{OlderThan: "soon"}), BadInput("older_than must be a duration of at least 1h, such as 24h")},
		{"cache ids and all", caches(model.DeleteCachesRequest{IDs: []int64{1}, All: true}), BadInput("give either ids or all")},
		{"neither cache ids nor all", caches(model.DeleteCachesRequest{}), BadInput("give either ids or all")},
		{"artifacts of an unknown repository", func() error {
			_, err := s.DeleteArtifacts("nope", model.DeleteArtifactsRequest{IDs: []int64{1}})
			return err
		}, ErrUnknownRepo},
		{"caches of an unknown repository", func() error {
			_, err := s.DeleteCaches("nope", model.DeleteCachesRequest{All: true})
			return err
		}, ErrUnknownRepo},
		{"retention of 0 days", func() error { _, err := s.SetRetentionAll(0); return err }, BadInput("days must be from 1 to 400")},
		{"retention over 400 days", func() error { _, err := s.SetRetentionAll(401); return err }, BadInput("days must be from 1 to 400")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if op := s.Snapshot().Operation; op != nil {
				t.Fatalf("a refused request started %+v", op)
			}
		})
	}
}

func TestOneOperationRunsAtATime(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), time.Hour, 5))
	gh.caches["darkmem"] = []github.Cache{{ID: 7, Key: "k"}}
	s.pass(true, false)
	entered, release := blockDelete(gh)
	if _, err := s.DeleteArtifacts("darkmem", model.DeleteArtifactsRequest{IDs: []int64{1}}); err != nil {
		t.Fatal(err)
	}
	<-entered
	for name, call := range map[string]func() error{
		"artifacts": func() error {
			_, err := s.DeleteArtifacts("darkmem", model.DeleteArtifactsRequest{IDs: []int64{1}})
			return err
		},
		"caches":    func() error { _, err := s.DeleteCaches("darkmem", model.DeleteCachesRequest{All: true}); return err },
		"retention": func() error { _, err := s.SetRetentionAll(30); return err },
	} {
		if err := call(); !errors.Is(err, ErrOperationRunning) {
			t.Errorf("%s during an operation: %v, want ErrOperationRunning", name, err)
		}
	}
	if op := s.Snapshot().Operation; op.ID != "op-1" || op.FinishedAt != nil {
		t.Fatalf("operation %+v, want op-1 running", op)
	}
	close(release)
	s.Wait()
	if id, err := s.DeleteCaches("darkmem", model.DeleteCachesRequest{All: true}); err != nil || id != "op-2" {
		t.Fatalf("after the first finished: %q, %v", id, err)
	}
}

func TestABulkDeleteStopsAtTheFirstError(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), time.Hour, 5), ghArt(2, 20, clock.Now(), time.Hour, 5))
	gh.delErr[1] = forbidden
	s.pass(true, false)
	if _, err := s.DeleteArtifacts("darkmem", model.DeleteArtifactsRequest{IDs: []int64{1, 2}}); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	op := s.Snapshot().Operation
	if op.Done != 0 || !reflect.DeepEqual(op.Failed, []model.OperationFailure{{Repo: "darkmem", ID: 1, Error: forbidden.Error()}}) {
		t.Fatalf("operation %+v", op)
	}
	if got := gh.callsLike("Delete"); len(got) != 1 {
		t.Fatalf("deletes %v, want the first only", got)
	}
	if got := eventsLike(s.Events, "deleted"); !reflect.DeepEqual(got, []string{"deleted 0 artifacts (0 B); 1 failed"}) {
		t.Errorf("events %q", got)
	}
}

func TestDeleteAllCaches(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	now := clock.Now()
	gh.addRepo("darkmem", true)
	gh.caches["darkmem"] = []github.Cache{
		{ID: 7, Key: "go-mod", SizeInBytes: 100, LastAccessedAt: now.Add(-3 * time.Hour)},
		{ID: 8, Key: "npm", SizeInBytes: 50, LastAccessedAt: now.Add(-50 * time.Hour)},
	}
	gh.gone[8] = true
	s.pass(true, false)
	if _, err := s.DeleteCaches("darkmem", model.DeleteCachesRequest{All: true}); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	st := s.Snapshot()
	if op := st.Operation; op.Kind != "delete-caches" || op.Total != 2 || op.Done != 2 || op.Bytes != 100 {
		t.Errorf("operation %+v", op)
	}
	if r := row(t, st, "darkmem"); r.CacheCount != 0 {
		t.Errorf("caches left %+v", r.Caches)
	}
	if want := []model.Deletion{{At: now, Repo: "darkmem", Kind: "cache", Name: "go-mod", Bytes: 100, Age: "3h", By: "manual"}}; !reflect.DeepEqual(st.Deletions, want) {
		t.Errorf("deletions %+v, want only the cache GitHub deleted", st.Deletions)
	}
	if got := eventsLike(s.Events, "deleted"); !reflect.DeepEqual(got, []string{"deleted 1 caches (100 B)"}) {
		t.Errorf("events %q", got)
	}
}

func TestSingleDeletes(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	now := clock.Now()
	gh.addRepo("darkmem", true, ghArt(1, 10, now, time.Hour, 5), ghArt(2, 20, now, time.Hour, 5), ghArt(3, 30, now, time.Hour, 5))
	gh.caches["darkmem"] = []github.Cache{{ID: 7, Key: "go-mod", SizeInBytes: 100}}
	gh.gone[7] = true
	gh.delErr[2] = forbidden
	s.pass(true, false)
	ctx := context.Background()
	if err := s.DeleteArtifact(ctx, "Darkmem", 1); err != nil {
		t.Fatalf("delete 1: %v", err)
	}
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"an artifact deleted before", s.DeleteArtifact(ctx, "darkmem", 1), ErrUnknownRepo},
		{"an unknown repository", s.DeleteArtifact(ctx, "nope", 1), ErrUnknownRepo},
		{"an unknown cache", s.DeleteCache(ctx, "darkmem", 99), ErrUnknownRepo},
		{"a cache already gone", s.DeleteCache(ctx, "darkmem", 7), nil},
		{"GitHub refuses", s.DeleteArtifact(ctx, "darkmem", 2), forbidden},
	} {
		if !errors.Is(tc.err, tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, tc.err, tc.want)
		}
	}
	st := s.Snapshot()
	if r := row(t, st, "darkmem"); !reflect.DeepEqual(artifactIDs(r), []int64{2, 3}) || r.CacheCount != 0 {
		t.Errorf("row %+v", r)
	}
	if len(st.Deletions) != 1 || st.Deletions[0].Name != "art-1" || st.Deletions[0].By != "manual" || st.Operation != nil {
		t.Errorf("deletions %+v, operation %+v", st.Deletions, st.Operation)
	}
	for range WriteBudget {
		_ = s.write(func() error { return nil })
	}
	var be *BudgetError
	if err := s.DeleteArtifact(ctx, "darkmem", 3); !errors.As(err, &be) {
		t.Errorf("past the budget: %v, want a BudgetError", err)
	}
}

func TestSetRetention(t *testing.T) {
	gh := newFakeGH()
	for _, r := range []string{"darkmem", "short", "unknown", "strict"} {
		gh.addRepo(r, true)
	}
	gh.retention["short"] = github.Retention{Days: 30, MaxDays: 90}
	gh.retErr["unknown"] = forbidden
	gh.setRetErr["strict"] = &github.APIError{Status: 422, Kind: github.ErrUnprocessable, Message: "Invalid retention days"}
	s, _ := newTestService(t, gh, testConfig())
	s.pass(true, false)
	ctx := context.Background()
	for _, tc := range []struct {
		repo string
		days int
		want error
	}{
		{"darkmem", 7, nil},
		{"DARKMEM", 8, nil},
		{"darkmem", 0, BadInput("days must be from 1 to 400")},
		{"short", 91, BadInput("days must be from 1 to 90")},
		{"unknown", 400, nil},
		{"unknown", 401, BadInput("days must be from 1 to 400")},
		{"strict", 5, BadInput("Invalid retention days")},
		{"nope", 5, ErrUnknownRepo},
	} {
		if err := s.SetRetention(ctx, tc.repo, tc.days); !errors.Is(err, tc.want) {
			t.Errorf("SetRetention(%s, %d) = %v, want %v", tc.repo, tc.days, err, tc.want)
		}
	}
	st := s.Snapshot()
	if d := row(t, st, "darkmem").RetentionDays; d != 8 {
		t.Errorf("darkmem retention %d, want 8 at once", d)
	}
	if got := gh.callsLike("SetRetention"); !reflect.DeepEqual(got, []string{"SetRetention darkmem 7", "SetRetention darkmem 8", "SetRetention unknown 400", "SetRetention strict 5"}) {
		t.Errorf("calls %v", got)
	}
	if st.Operation != nil {
		t.Errorf("a single change recorded an operation: %+v", st.Operation)
	}
}

func TestSetRetentionAll(t *testing.T) {
	gh := newFakeGH()
	for _, r := range []string{"a", "b", "c", "d"} {
		gh.addRepo(r, true)
	}
	gh.retention["b"] = github.Retention{Days: 30, MaxDays: 30}
	gh.setRetErr["c"] = forbidden
	gh.retErr["d"] = forbidden
	s, _ := newTestService(t, gh, testConfig())
	s.pass(true, false)
	if _, err := s.SetRetentionAll(60); err != nil {
		t.Fatal(err)
	}
	s.Wait()
	st := s.Snapshot()
	want := []model.OperationFailure{{Repo: "b", Error: "b allows at most 30 days"}, {Repo: "c", Error: forbidden.Error()}}
	if op := st.Operation; op.Kind != "set-retention" || op.Repo != "" || op.Total != 4 || op.Done != 2 || !reflect.DeepEqual(op.Failed, want) {
		t.Errorf("operation %+v", op)
	}
	if got := gh.callsLike("SetRetention"); !reflect.DeepEqual(got, []string{"SetRetention a 60", "SetRetention c 60", "SetRetention d 60"}) {
		t.Errorf("calls %v", got)
	}
	if d := row(t, st, "a").RetentionDays; d != 60 {
		t.Errorf("a retention %d, want 60", d)
	}
	if got := eventsLike(s.Events, "set retention"); !reflect.DeepEqual(got, []string{"set retention to 60 days on 2 of 4 repositories; 2 failed"}) {
		t.Errorf("events %q", got)
	}
}

func TestPreviewChecksItsInput(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), 2*time.Hour, 5))
	s.pass(true, false)
	for _, tc := range []struct {
		cleanup, repo string
		want          []model.StoragePreview
		err           error
	}{
		{"", "", nil, BadInput("cleanup must be off or a duration such as 2d")},
		{"soon", "", nil, BadInput("cleanup must be off or a duration such as 2d")},
		{"2d", "nope", nil, ErrUnknownRepo},
		{"off", "", []model.StoragePreview{}, nil},
		{"1h", "DARKMEM", []model.StoragePreview{{Repo: "darkmem", Count: 1, Bytes: 5}}, nil},
	} {
		got, err := s.Preview(tc.cleanup, tc.repo)
		if !errors.Is(err, tc.err) || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Preview(%q, %q) = %+v, %v; want %+v, %v", tc.cleanup, tc.repo, got, err, tc.want, tc.err)
		}
	}
}

func TestCloseCancelsAnOperation(t *testing.T) {
	gh := newFakeGH()
	s, clock := newTestService(t, gh, testConfig())
	gh.addRepo("darkmem", true, ghArt(1, 10, clock.Now(), time.Hour, 5), ghArt(2, 20, clock.Now(), time.Hour, 5))
	s.pass(true, false)
	entered, _ := blockDelete(gh)
	if _, err := s.DeleteArtifacts("darkmem", model.DeleteArtifactsRequest{IDs: []int64{1, 2}}); err != nil {
		t.Fatal(err)
	}
	<-entered
	s.Close()
	stopped := make(chan struct{})
	go func() { s.Wait(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Wait did not return after Close")
	}
	if op := s.Snapshot().Operation; op.FinishedAt == nil || op.Done != 0 || len(gh.callsLike("Delete")) != 1 {
		t.Fatalf("operation %+v after Close", op)
	}
	if _, err := s.DeleteArtifacts("darkmem", model.DeleteArtifactsRequest{IDs: []int64{2}}); !errors.Is(err, ErrClosed) {
		t.Fatalf("after Close: %v, want ErrClosed", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/ghstorage/
```
Expected: the build fails, starting with:

```
internal/ghstorage/ops_test.go:38:15: s.DeleteArtifacts undefined (type *Service has no field or method DeleteArtifacts)
internal/ghstorage/ops_test.go:67:17: s.DeleteArtifacts undefined (type *Service has no field or method DeleteArtifacts)
```

- [ ] **Step 3: Write the implementation**

Create `internal/ghstorage/ops.go`:

```go
package ghstorage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/config"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

var (
	ErrUnknownRepo      = errors.New("not in the last GitHub storage inventory")
	ErrOperationRunning = errors.New("another GitHub storage operation is running")
)

// BadInput is a request the service refuses as given; the daemon answers 400
// with its text.
type BadInput string

func (e BadInput) Error() string { return string(e) }

// maxRetentionDays is GitHub's longest retention, the limit while a
// repository's own is unknown.
const maxRetentionDays = 400

func (s *Service) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// find is the index of repo's row, matched ignoring case. Call with s.mu held.
func (s *Service) find(repo string) (int, bool) {
	for i, r := range s.repos {
		if strings.EqualFold(r.Name, repo) {
			return i, true
		}
	}
	return 0, false
}

// Preview counts what a cleanup age would delete now, from the snapshot.
func (s *Service) Preview(cleanup, repo string) ([]model.StoragePreview, error) {
	age, err := config.ParseCleanup(cleanup)
	if cleanup == "" || err != nil || age < 0 {
		return nil, BadInput("cleanup must be off or a duration such as 2d")
	}
	cfg := s.Config()
	s.mu.Lock()
	defer s.mu.Unlock()
	if repo != "" {
		i, ok := s.find(repo)
		if !ok {
			return nil, ErrUnknownRepo
		}
		repo = s.repos[i].Name
	}
	return Preview(s.repos, cfg.GitHubStorage, repo, age, s.now()), nil
}

// DeleteArtifact deletes one artifact of the snapshot and answers when done.
func (s *Service) DeleteArtifact(ctx context.Context, repo string, id int64) error {
	s.mu.Lock()
	var it model.ArtifactItem
	found := false
	if i, ok := s.find(repo); ok {
		repo = s.repos[i].Name
		it, found = findItem(s.repos[i].Artifacts, func(a model.ArtifactItem) bool { return a.ID == id })
	}
	s.mu.Unlock()
	if !found {
		return ErrUnknownRepo
	}
	_, err := s.deleteArtifact(ctx, repo, it, "manual")
	return err
}

// DeleteCache deletes one cache entry of the snapshot and answers when done.
func (s *Service) DeleteCache(ctx context.Context, repo string, id int64) error {
	s.mu.Lock()
	var c model.CacheItem
	found := false
	if i, ok := s.find(repo); ok {
		repo = s.repos[i].Name
		c, found = findItem(s.repos[i].Caches, func(c model.CacheItem) bool { return c.ID == id })
	}
	s.mu.Unlock()
	if !found {
		return ErrUnknownRepo
	}
	_, err := s.deleteCache(ctx, repo, c, "manual")
	return err
}

func findItem[T any](items []T, match func(T) bool) (T, bool) {
	if i := slices.IndexFunc(items, match); i >= 0 {
		return items[i], true
	}
	var zero T
	return zero, false
}

// deleteCache deletes one cache entry within the write budget, as
// deleteArtifact does an artifact. Its age is the time since its last use.
func (s *Service) deleteCache(ctx context.Context, repo string, c model.CacheItem, by string) (bool, error) {
	var deleted bool
	err := s.write(func() error {
		var err error
		deleted, err = s.GH.DeleteCache(ctx, repo, c.ID)
		return err
	})
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.removeItem("cache", repo, c.ID)
	if deleted {
		now := s.now()
		age := ageText(now.Sub(c.LastUsedAt))
		s.recordDeletion(model.Deletion{At: now, Repo: repo, Kind: "cache", Name: c.Key, Bytes: c.Bytes, Age: age, By: by})
		log.Printf("github storage: deleted cache %s (%s) in %s, age %s", c.Key, sizeText(c.Bytes), repo, age)
	}
	s.bump()
	return deleted, nil
}

// DeleteArtifacts starts deleting, in the background, either the named
// artifacts or those older than an age. The age skips the artifacts of runs
// that have not completed, as the cleanup does; named ones are deleted as
// named. It returns the operation's id.
func (s *Service) DeleteArtifacts(repo string, req model.DeleteArtifactsRequest) (string, error) {
	if (len(req.IDs) > 0) == (req.OlderThan != "") {
		return "", BadInput("give either ids or older_than")
	}
	var age time.Duration
	if req.OlderThan != "" {
		d, err := config.ParseDuration(req.OlderThan)
		if err != nil || d.D() < time.Hour {
			return "", BadInput("older_than must be a duration of at least 1h, such as 24h")
		}
		age = d.D()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.find(repo)
	if !ok {
		return "", ErrUnknownRepo
	}
	name, items := s.repos[i].Name, slices.Clone(s.repos[i].Artifacts)
	op, err := s.startOp("delete-artifacts", name, len(req.IDs))
	if err != nil {
		return "", err
	}
	s.runOp(func(ctx context.Context) {
		var todo []model.ArtifactItem
		if age > 0 {
			active, err := s.activeRuns(ctx, name)
			if err != nil {
				s.fail(model.OperationFailure{Repo: name, Error: s.describe(err, name)})
			} else {
				todo = Candidates(items, age, s.now(), active)
			}
			s.mu.Lock()
			op.Total = len(todo)
			s.mu.Unlock()
		} else {
			todo = pickByID(name, items, req.IDs, func(a model.ArtifactItem) int64 { return a.ID }, s.fail)
		}
		n, bytes := s.deleteEach(ctx, name, len(todo), func(ctx context.Context, i int) (bool, int64, int64, error) {
			deleted, err := s.deleteArtifact(ctx, name, todo[i], "manual")
			return deleted, todo[i].Bytes, todo[i].ID, err
		})
		s.finishOp(name, "deleted %d artifacts (%s)", n, sizeText(bytes))
	})
	return op.ID, nil
}

// DeleteCaches starts deleting, in the background, either the named cache
// entries or all of the repository's. It returns the operation's id.
func (s *Service) DeleteCaches(repo string, req model.DeleteCachesRequest) (string, error) {
	if (len(req.IDs) > 0) == req.All {
		return "", BadInput("give either ids or all")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i, ok := s.find(repo)
	if !ok {
		return "", ErrUnknownRepo
	}
	name, items := s.repos[i].Name, slices.Clone(s.repos[i].Caches)
	total := len(req.IDs)
	if req.All {
		total = len(items)
	}
	op, err := s.startOp("delete-caches", name, total)
	if err != nil {
		return "", err
	}
	s.runOp(func(ctx context.Context) {
		todo := items
		if !req.All {
			todo = pickByID(name, items, req.IDs, func(c model.CacheItem) int64 { return c.ID }, s.fail)
		}
		n, bytes := s.deleteEach(ctx, name, len(todo), func(ctx context.Context, i int) (bool, int64, int64, error) {
			deleted, err := s.deleteCache(ctx, name, todo[i], "manual")
			return deleted, todo[i].Bytes, todo[i].ID, err
		})
		s.finishOp(name, "deleted %d caches (%s)", n, sizeText(bytes))
	})
	return op.ID, nil
}

// pickByID returns the items named by ids, in the order named, and reports
// each id not among them as failed.
func pickByID[T any](repo string, items []T, ids []int64, id func(T) int64, fail func(model.OperationFailure)) []T {
	var out []T
	for _, want := range ids {
		if it, ok := findItem(items, func(x T) bool { return id(x) == want }); ok {
			out = append(out, it)
		} else {
			fail(model.OperationFailure{Repo: repo, ID: want, Error: "not in the last inventory"})
		}
	}
	return out
}

// startOp makes a new operation the running one. Call with s.mu held.
func (s *Service) startOp(kind, repo string, total int) (*model.GitHubOperation, error) {
	switch {
	case s.closed:
		return nil, ErrClosed
	case s.opRunning:
		return nil, ErrOperationRunning
	}
	s.op = &model.GitHubOperation{ID: s.newID(), Kind: kind, Repo: repo, Total: total,
		Failed: []model.OperationFailure{}, StartedAt: s.now()}
	s.opRunning = true
	s.bump()
	return s.op, nil
}

// runOp runs an operation's work in the background; Close cancels its
// context and Wait waits for it.
func (s *Service) runOp(fn func(ctx context.Context)) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		fn(s.ctx)
	}()
}

func (s *Service) fail(f model.OperationFailure) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.op.Failed = append(s.op.Failed, f)
	s.bump()
}

// deleteEach runs del for items 0 to count-1, counting each on the running
// operation, and stops at the first error: the rest would fail the same way.
// It returns how many were deleted and their bytes; a 404 counts as done
// but not deleted.
func (s *Service) deleteEach(ctx context.Context, repo string, count int, del func(ctx context.Context, i int) (deleted bool, bytes, id int64, err error)) (int, int64) {
	n, total := 0, int64(0)
	for i := range count {
		if ctx.Err() != nil {
			break
		}
		deleted, bytes, id, err := del(ctx, i)
		if err != nil {
			s.fail(model.OperationFailure{Repo: repo, ID: id, Error: s.opErr(err, repo)})
			break
		}
		s.mu.Lock()
		s.op.Done++
		if deleted {
			n++
			total += bytes
			s.op.Bytes += bytes
		}
		s.bump()
		s.mu.Unlock()
	}
	return n, total
}

func (s *Service) opErr(err error, repo string) string {
	var be *BudgetError
	var bi BadInput
	if errors.As(err, &be) || errors.As(err, &bi) {
		return err.Error()
	}
	return s.describe(err, repo)
}

// finishOp ends the running operation and writes one event with its outcome.
func (s *Service) finishOp(repo, format string, args ...any) {
	s.mu.Lock()
	now := s.now()
	s.op.FinishedAt = &now
	s.opRunning = false
	failed := len(s.op.Failed)
	s.bump()
	s.mu.Unlock()
	msg := fmt.Sprintf(format, args...)
	if failed > 0 {
		s.Events.Add("warn", repo, "%s; %d failed", msg, failed)
		return
	}
	s.Events.Add("info", repo, "%s", msg)
}

// SetRetention sets one repository's artifact and log retention and answers
// when done. GitHub refusing the value is a BadInput with its message.
func (s *Service) SetRetention(ctx context.Context, repo string, days int) error {
	s.mu.Lock()
	i, ok := s.find(repo)
	var name string
	var most int
	if ok {
		name, most = s.repos[i].Name, s.repos[i].RetentionMaxDays
	}
	s.mu.Unlock()
	if !ok {
		return ErrUnknownRepo
	}
	if most == 0 {
		most = maxRetentionDays
	}
	if days < 1 || days > most {
		return BadInput(fmt.Sprintf("days must be from 1 to %d", most))
	}
	return s.setRetention(ctx, name, days)
}

func (s *Service) setRetention(ctx context.Context, repo string, days int) error {
	err := s.write(func() error { return s.GH.SetRetention(ctx, repo, days) })
	var ge *github.APIError
	if errors.As(err, &ge) && ge.Kind == github.ErrUnprocessable {
		return BadInput(ge.Message)
	}
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if i, ok := s.find(repo); ok {
		s.repos[i].RetentionDays = days
	}
	key := strings.ToLower(repo)
	rr := s.retention[key]
	rr.r.Days = days
	s.retention[key] = rr
	s.bump()
	return nil
}

// SetRetentionAll starts setting one retention on every repository of the
// snapshot in the background. A repository whose most allowed days are
// below the value is skipped and reported as failed. It returns the
// operation's id.
func (s *Service) SetRetentionAll(days int) (string, error) {
	if days < 1 || days > maxRetentionDays {
		return "", BadInput(fmt.Sprintf("days must be from 1 to %d", maxRetentionDays))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	type target struct {
		name string
		most int
	}
	var targets []target
	for _, r := range s.repos {
		targets = append(targets, target{r.Name, r.RetentionMaxDays})
	}
	op, err := s.startOp("set-retention", "", len(targets))
	if err != nil {
		return "", err
	}
	s.runOp(func(ctx context.Context) {
		set := 0
		for _, t := range targets {
			if ctx.Err() != nil {
				break
			}
			if t.most != 0 && t.most < days {
				s.fail(model.OperationFailure{Repo: t.name, Error: fmt.Sprintf("%s allows at most %d days", t.name, t.most)})
				continue
			}
			err := s.setRetention(ctx, t.name, days)
			if err != nil {
				s.fail(model.OperationFailure{Repo: t.name, Error: s.opErr(err, t.name)})
				var be *BudgetError
				if errors.As(err, &be) {
					break
				}
				continue
			}
			set++
			s.mu.Lock()
			s.op.Done++
			s.bump()
			s.mu.Unlock()
		}
		s.finishOp("", "set retention to %d days on %d of %d repositories", days, set, len(targets))
	})
	return op.ID, nil
}
```

In `internal/ghstorage/service.go`, replace these lines (in the `Service` struct; gofmt realigns the comments):

```go
	deleted   map[string]time.Time // "artifact:<id>" or "cache:<id>" → when deleted
	deletions []model.Deletion     // newest first
```

with:

```go
	deleted   map[string]time.Time   // "artifact:<id>" or "cache:<id>" → when deleted
	deletions []model.Deletion       // newest first
	op        *model.GitHubOperation // the running or last finished operation
	opRunning bool
```

and in `Snapshot`, replace these lines:

```go
	if s.quota != nil {
		q := *s.quota
		out.Quota = &q
	}
```

with:

```go
	if s.quota != nil {
		q := *s.quota
		out.Quota = &q
	}
	if s.op != nil {
		op := *s.op
		op.Failed = append([]model.OperationFailure{}, s.op.Failed...)
		out.Operation = &op
	}
```

- [ ] **Step 4: Run the tests to verify they pass, also under the race detector**

```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -count=1 ./internal/ghstorage/
```
```bash
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test -race -count=5 ./internal/ghstorage/
```
Expected: `ok  	github.com/darkraise/ghr/internal/ghstorage` both times.

- [ ] **Step 5: Format, vet and commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./internal/... ./cmd/...
```
Expected: `gofmt` prints nothing and `go vet` passes.

```bash
git add internal/ghstorage/ops.go internal/ghstorage/ops_test.go internal/ghstorage/service.go
git commit -m "feat(ghstorage): run manual deletes and retention"
```

### Task 10: Serve the storage service from the backend

**Files:**
- Create: `internal/daemon/ghstorage.go`
- Modify: `internal/daemon/backend.go` (the `Backend` struct, `Status`, `SetToken`, `Reload`, `PatchConfig`)
- Test: `internal/daemon/ghstorage_test.go` (new)

**Interfaces:**
- Consumes: Contracts → Service (Tasks 7 to 9): `ghstorage.ErrBusy`, `ErrClosed`, `ErrUnknownRepo`, `ErrOperationRunning`, `BadInput`, `*BudgetError` and the service's exported methods; Contracts → Model (Task 2); `patchJSON` (Contracts → Patch, Task 4).
- Produces: Contracts → Daemon and API: `daemon.GitHubStorageService`, `Backend.GHS`, the nine Backend methods and their error mapping; `Status()` carries the alert; `SetToken`, `Reload` and `PatchConfig` reach the service. Test helpers in `internal/daemon/ghstorage_test.go`: `fakeGHS` (records calls as `"<method> <args>"`, read with `recorded()`; fields `snap`, `alert`, `preview`, `err`, `version`) and `newGHSBackend(t) (*Backend, *fakeManager, *fakeGHS)`; Task 11 adds to this file.

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

`Reload` calls `TokenChanged()` when the token file changed and `Kick()` otherwise, so a reload with a new token also clears the old token's row errors. `RefreshGitHubStorage` and every delete and retention call run `b.githubErr()` first; reading the snapshot or a preview does not call GitHub, so neither waits for it.

- [ ] **Step 1: Write the failing test**

Create `internal/daemon/ghstorage_test.go`:

```go
package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/ghstorage"
	"github.com/darkraise/ghr/internal/model"
)

// fakeGHS records every call as "<method> <args>" and answers with err.
type fakeGHS struct {
	mu      sync.Mutex
	calls   []string
	snap    model.GitHubStorage
	alert   *model.StorageAlert
	preview []model.StoragePreview
	err     error
	version atomic.Int64
}

func (f *fakeGHS) record(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fmt.Sprintf(format, args...))
}

func (f *fakeGHS) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func (f *fakeGHS) Snapshot() model.GitHubStorage { return f.snap }
func (f *fakeGHS) Alert() *model.StorageAlert    { return f.alert }
func (f *fakeGHS) Version() int64                { return f.version.Load() }
func (f *fakeGHS) Refresh() error                { f.record("refresh"); return f.err }
func (f *fakeGHS) Kick()                         { f.record("kick") }
func (f *fakeGHS) TokenChanged()                 { f.record("token-changed") }
func (f *fakeGHS) PolicyChanged()                { f.record("policy") }
func (f *fakeGHS) Preview(cleanup, repo string) ([]model.StoragePreview, error) {
	f.record("preview %s %s", cleanup, repo)
	return f.preview, f.err
}
func (f *fakeGHS) DeleteArtifact(_ context.Context, repo string, id int64) error {
	f.record("delete-artifact %s %d", repo, id)
	return f.err
}
func (f *fakeGHS) DeleteCache(_ context.Context, repo string, id int64) error {
	f.record("delete-cache %s %d", repo, id)
	return f.err
}
func (f *fakeGHS) DeleteArtifacts(repo string, req model.DeleteArtifactsRequest) (string, error) {
	f.record("delete-artifacts %s %v %s", repo, req.IDs, req.OlderThan)
	return "op-1", f.err
}
func (f *fakeGHS) DeleteCaches(repo string, req model.DeleteCachesRequest) (string, error) {
	f.record("delete-caches %s %v %t", repo, req.IDs, req.All)
	return "op-2", f.err
}
func (f *fakeGHS) SetRetention(_ context.Context, repo string, days int) error {
	f.record("retention %s %d", repo, days)
	return f.err
}
func (f *fakeGHS) SetRetentionAll(days int) (string, error) {
	f.record("retention-all %d", days)
	return "op-3", f.err
}

func newGHSBackend(t *testing.T) (*Backend, *fakeManager, *fakeGHS) {
	t.Helper()
	b, m, _ := newBackend(t)
	f := &fakeGHS{}
	b.GHS = f
	return b, m, f
}

// ghsCalls are the backend's GitHub storage methods, each named by what the
// fake records for it.
func ghsCalls(b *Backend) map[string]func() error {
	ctx := context.Background()
	return map[string]func() error{
		"refresh":         b.RefreshGitHubStorage,
		"delete-artifact": func() error { return b.DeleteGitHubArtifact(ctx, "darkmem", 7) },
		"delete-cache":    func() error { return b.DeleteGitHubCache(ctx, "darkmem", 8) },
		"delete-artifacts": func() error {
			_, err := b.DeleteGitHubArtifacts("darkmem", model.DeleteArtifactsRequest{IDs: []int64{1}})
			return err
		},
		"delete-caches": func() error {
			_, err := b.DeleteGitHubCaches("darkmem", model.DeleteCachesRequest{All: true})
			return err
		},
		"retention":     func() error { return b.SetGitHubRetention(ctx, "darkmem", 7) },
		"retention-all": func() error { _, err := b.SetGitHubRetentionAll(7); return err },
		"preview":       func() error { _, err := b.GitHubStoragePreview("2d", ""); return err },
	}
}

func TestGitHubStorageCallsReachTheService(t *testing.T) {
	b, _, f := newGHSBackend(t)
	ctx := context.Background()
	f.snap = model.GitHubStorage{QuotaError: "x"}
	f.preview = []model.StoragePreview{{Repo: "darkmem", Count: 2, Bytes: 10}}
	if st := b.GitHubStorage(); st.QuotaError != "x" {
		t.Fatalf("snapshot %+v", st)
	}
	ps, err := b.GitHubStoragePreview("2d", "darkmem")
	if err != nil || len(ps) != 1 || ps[0].Count != 2 {
		t.Fatalf("preview %+v, %v", ps, err)
	}
	if err := b.RefreshGitHubStorage(); err != nil {
		t.Fatal(err)
	}
	if err := b.DeleteGitHubArtifact(ctx, "DarkMem", 7); err != nil {
		t.Fatal(err)
	}
	if err := b.DeleteGitHubCache(ctx, "darkmem", 8); err != nil {
		t.Fatal(err)
	}
	if err := b.SetGitHubRetention(ctx, "darkmem", 5); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		call func() (model.OperationStarted, error)
		id   string
	}{
		{func() (model.OperationStarted, error) {
			return b.DeleteGitHubArtifacts("darkmem", model.DeleteArtifactsRequest{OlderThan: "24h"})
		}, "op-1"},
		{func() (model.OperationStarted, error) {
			return b.DeleteGitHubCaches("darkmem", model.DeleteCachesRequest{IDs: []int64{3, 4}})
		}, "op-2"},
		{func() (model.OperationStarted, error) { return b.SetGitHubRetentionAll(3) }, "op-3"},
	} {
		op, err := tc.call()
		if err != nil || op.ID != tc.id {
			t.Fatalf("operation %+v, %v; want %s", op, err, tc.id)
		}
	}
	want := []string{
		"preview 2d darkmem", "refresh", "delete-artifact DarkMem 7", "delete-cache darkmem 8", "retention darkmem 5",
		"delete-artifacts darkmem [] 24h", "delete-caches darkmem [3 4] false", "retention-all 3",
	}
	if got := f.recorded(); !slices.Equal(got, want) {
		t.Fatalf("calls %q", got)
	}
}

func TestGitHubStorageErrorsMapToStatuses(t *testing.T) {
	b, _, f := newGHSBackend(t)
	retry := time.Date(2026, 10, 13, 13, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		err  error
		want int
	}{
		{ghstorage.BadInput("give either ids or older_than"), 400},
		{ghstorage.ErrUnknownRepo, 404},
		{ghstorage.ErrBusy, 409},
		{ghstorage.ErrOperationRunning, 409},
		{&ghstorage.BudgetError{RetryAt: retry}, 429},
		{ghstorage.ErrClosed, 503},
	} {
		f.err = tc.err
		for name, call := range ghsCalls(b) {
			err := call()
			if apiStatus(err) != tc.want || apiMsg(err) != tc.err.Error() {
				t.Errorf("%s with %v: status %d message %q, want %d", name, tc.err, apiStatus(err), apiMsg(err), tc.want)
			}
		}
	}
	f.err = &ghstorage.BudgetError{RetryAt: retry}
	var ae *api.Error
	if err := b.RefreshGitHubStorage(); !errors.As(err, &ae) || !ae.RetryAt.Equal(retry) {
		t.Fatalf("budget retry time: %v", err)
	}
	f.err = errors.New("github: 403 Resource not accessible")
	if err := b.DeleteGitHubArtifact(context.Background(), "darkmem", 1); err != f.err {
		t.Fatalf("an unexpected error must pass through unchanged: %v", err)
	}
}

// Every call that reaches GitHub waits while the token is rejected or a
// rate limit pauses the API; reading the snapshot or a preview does not.
func TestGitHubStorageWaitsForGitHub(t *testing.T) {
	b, m, f := newGHSBackend(t)
	m.degraded = "Bad credentials"
	for name, call := range ghsCalls(b) {
		want := 503
		if name == "preview" {
			want = 0
		}
		if err := call(); apiStatus(err) != want {
			t.Errorf("%s while degraded: %v", name, err)
		}
	}
	m.degraded = ""
	m.paused = time.Now().Add(time.Hour)
	for name, call := range ghsCalls(b) {
		want := 429
		if name == "preview" {
			want = 0
		}
		if err := call(); apiStatus(err) != want {
			t.Errorf("%s while paused: %v", name, err)
		}
	}
	if got := f.recorded(); !slices.Equal(got, []string{"preview 2d ", "preview 2d "}) {
		t.Fatalf("calls %q", got)
	}
}

func TestStatusCarriesTheStorageAlert(t *testing.T) {
	b, _, _ := newBackend(t)
	if st := b.Status(); st.GitHubStorage != nil {
		t.Fatalf("alert without a service: %+v", st.GitHubStorage)
	}
	f := &fakeGHS{}
	b.GHS = f
	if st := b.Status(); st.GitHubStorage != nil {
		t.Fatalf("alert without a quota: %+v", st.GitHubStorage)
	}
	f.alert = &model.StorageAlert{Level: "exhausted", Month: "2026-10", UsedPct: 103}
	if st := b.Status(); st.GitHubStorage == nil || st.GitHubStorage.Level != "exhausted" || st.GitHubStorage.UsedPct != 103 {
		t.Fatalf("alert %+v", st.GitHubStorage)
	}
}

func TestTokenReloadAndPolicyReachTheStorageService(t *testing.T) {
	b, _, f := newGHSBackend(t)
	ctx := context.Background()
	if err := b.SetToken(ctx, "tok2"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b.Store.TokenPath, []byte("tok3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Reload(); err != nil {
		t.Fatal(err)
	}
	all := "all"
	if err := b.PatchConfig(ctx, model.ConfigPatch{Mode: &all}); err != nil {
		t.Fatal(err)
	}
	if err := patchJSON(t, b, `{"github_storage":{"cleanup_max_age":"2d"}}`); err != nil {
		t.Fatal(err)
	}
	if err := patchJSON(t, b, `{"github_storage":{"cleanup_max_age":"1h"}}`); apiStatus(err) != 400 {
		t.Fatalf("a refused patch: %v", err)
	}
	want := []string{"token-changed", "kick", "token-changed", "policy"}
	if got := f.recorded(); !slices.Equal(got, want) {
		t.Fatalf("calls %q", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run 'GitHubStorage|StorageAlert|StorageService'`
Expected: FAIL, the build stops at `b.GHS undefined (type *Backend has no field or method GHS)`.

- [ ] **Step 3: Write the implementation**

Create `internal/daemon/ghstorage.go`:

```go
package daemon

import (
	"context"
	"errors"
	"net/http"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/ghstorage"
	"github.com/darkraise/ghr/internal/model"
)

// GitHubStorageService is the part of *ghstorage.Service the backend uses.
type GitHubStorageService interface {
	Snapshot() model.GitHubStorage
	Alert() *model.StorageAlert
	Version() int64
	Refresh() error
	Kick()
	TokenChanged()
	PolicyChanged()
	Preview(cleanup, repo string) ([]model.StoragePreview, error)
	DeleteArtifact(ctx context.Context, repo string, id int64) error
	DeleteCache(ctx context.Context, repo string, id int64) error
	DeleteArtifacts(repo string, req model.DeleteArtifactsRequest) (string, error)
	DeleteCaches(repo string, req model.DeleteCachesRequest) (string, error)
	SetRetention(ctx context.Context, repo string, days int) error
	SetRetentionAll(days int) (string, error)
}

func (b *Backend) GitHubStorage() model.GitHubStorage { return b.GHS.Snapshot() }

// GitHubStoragePreview counts what a cleanup age would delete; it reads the
// snapshot only, so it does not wait for GitHub.
func (b *Backend) GitHubStoragePreview(cleanup, repo string) ([]model.StoragePreview, error) {
	ps, err := b.GHS.Preview(cleanup, repo)
	return ps, ghsErr(err)
}

func (b *Backend) RefreshGitHubStorage() error {
	if err := b.githubErr(); err != nil {
		return err
	}
	return ghsErr(b.GHS.Refresh())
}

func (b *Backend) DeleteGitHubArtifact(ctx context.Context, repo string, id int64) error {
	if err := b.githubErr(); err != nil {
		return err
	}
	return ghsErr(b.GHS.DeleteArtifact(ctx, repo, id))
}

func (b *Backend) DeleteGitHubCache(ctx context.Context, repo string, id int64) error {
	if err := b.githubErr(); err != nil {
		return err
	}
	return ghsErr(b.GHS.DeleteCache(ctx, repo, id))
}

func (b *Backend) DeleteGitHubArtifacts(repo string, req model.DeleteArtifactsRequest) (model.OperationStarted, error) {
	if err := b.githubErr(); err != nil {
		return model.OperationStarted{}, err
	}
	id, err := b.GHS.DeleteArtifacts(repo, req)
	return model.OperationStarted{ID: id}, ghsErr(err)
}

func (b *Backend) DeleteGitHubCaches(repo string, req model.DeleteCachesRequest) (model.OperationStarted, error) {
	if err := b.githubErr(); err != nil {
		return model.OperationStarted{}, err
	}
	id, err := b.GHS.DeleteCaches(repo, req)
	return model.OperationStarted{ID: id}, ghsErr(err)
}

func (b *Backend) SetGitHubRetention(ctx context.Context, repo string, days int) error {
	if err := b.githubErr(); err != nil {
		return err
	}
	return ghsErr(b.GHS.SetRetention(ctx, repo, days))
}

func (b *Backend) SetGitHubRetentionAll(days int) (model.OperationStarted, error) {
	if err := b.githubErr(); err != nil {
		return model.OperationStarted{}, err
	}
	id, err := b.GHS.SetRetentionAll(days)
	return model.OperationStarted{ID: id}, ghsErr(err)
}

// ghsErr gives the storage service's refusals their HTTP statuses. Other
// errors, GitHub's among them, are returned unchanged.
func ghsErr(err error) error {
	var bad ghstorage.BadInput
	var budget *ghstorage.BudgetError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &bad):
		return api.BadRequest(err.Error())
	case errors.Is(err, ghstorage.ErrUnknownRepo):
		return api.NotFound(err.Error())
	case errors.Is(err, ghstorage.ErrBusy), errors.Is(err, ghstorage.ErrOperationRunning):
		return api.Conflict(err.Error())
	case errors.As(err, &budget):
		return &api.Error{Status: http.StatusTooManyRequests, Msg: err.Error(), RetryAt: budget.RetryAt}
	case errors.Is(err, ghstorage.ErrClosed):
		return &api.Error{Status: http.StatusServiceUnavailable, Msg: err.Error()}
	}
	return err
}
```

In `internal/daemon/backend.go`, replace these lines:

```go
	// Space is the storage service: the snapshot, toolchains and caches.
	Space StorageService
```

with:

```go
	// Space is the storage service: the snapshot, toolchains and caches.
	Space StorageService
	// GHS is the GitHub artifact storage service; nil in tests that do not
	// use it.
	GHS GitHubStorageService
```

In `internal/daemon/backend.go`, replace these lines:

```go
	st.Restart = b.restartStatus(st)
	st.WebListenError = b.WebListenErr
	return st
}
```

with:

```go
	st.Restart = b.restartStatus(st)
	st.WebListenError = b.WebListenErr
	if b.GHS != nil {
		st.GitHubStorage = b.GHS.Alert()
	}
	return st
}
```

In `internal/daemon/backend.go`, replace these lines:

```go
	b.GH.ForgetCache()
	b.dropLabelChecks()
	b.M.ClearDegraded()
	b.Events.Add("ok", "", "GitHub token replaced")
```

with:

```go
	b.GH.ForgetCache()
	if b.GHS != nil {
		b.GHS.TokenChanged()
	}
	b.dropLabelChecks()
	b.M.ClearDegraded()
	b.Events.Add("ok", "", "GitHub token replaced")
```

In `internal/daemon/backend.go`, replace these lines:

```go
	b.Events.Add("info", "", "config and token reloaded")
	if b.Wake != nil {
```

with:

```go
	b.Events.Add("info", "", "config and token reloaded")
	if b.GHS != nil {
		if tokenChanged {
			b.GHS.TokenChanged()
		} else {
			b.GHS.Kick()
		}
	}
	if b.Wake != nil {
```

In `internal/daemon/backend.go`, replace these lines:

```go
	if p.Web != nil {
		b.warnPending()
	}
	return nil
}
```

with:

```go
	if p.Web != nil {
		b.warnPending()
	}
	if p.GitHubStorage != nil && b.GHS != nil {
		b.GHS.PolicyChanged()
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run 'GitHubStorage|StorageAlert|StorageService' -v`
Expected: PASS for `TestGitHubStorageCallsReachTheService`, `TestGitHubStorageErrorsMapToStatuses`, `TestGitHubStorageWaitsForGitHub`, `TestStatusCarriesTheStorageAlert`, `TestTokenReloadAndPolicyReachTheStorageService`, and Task 4's `TestPatchGitHubStorage` tests.

Then run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/`
Expected: `ok`.

- [ ] **Step 5: Check and commit**

Run: `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (prints nothing) and `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./internal/... ./cmd/...` (passes).

```bash
git add internal/daemon/ghstorage.go internal/daemon/ghstorage_test.go internal/daemon/backend.go
git commit -m "feat(daemon): serve the GitHub storage service"
```

### Task 11: Wire the service into the daemon

**Files:**
- Modify: `internal/daemon/changes.go` (`fingerprints`)
- Modify: `internal/daemon/run.go` (imports, `running`, `start`, a new `storageErrText`, `Run`'s shutdown)
- Test: `internal/daemon/ghstorage_test.go` (imports, three new tests)

**Interfaces:**
- Consumes: Contracts → Service (Task 7): `ghstorage.Service` and its fields, `Start`, `Close`, `Wait`; Contracts → Daemon and API (Task 10): `Backend.GHS`, `fakeGHS`, `newGHSBackend`. Existing helpers: `changed` (`changes_test.go`), `testOptions`, `startRun`, `waitFor`, `readFrames`, `nextFrame` (`run_test.go`), `cfgYAML` (`store_test.go`).
- Produces: the `github-storage` change topic (a counter of `GHS.Version`, present only when `GHS` is set) that Task 16's `TOPIC_QUERIES` names; the alert in the `status` topic's fingerprint; the running daemon's storage service, started in `start` and closed and waited for at shutdown. `storageErrText(owner string) func(err error, repo string) string` is the service's `Describe`.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

A failed listing of the token's repositories has no repository to name, so `storageErrText` keeps GitHub's text for it; `actionsErr` would read "token cannot see darkraise/: ...".

- [ ] **Step 1: Write the failing tests**

In `internal/daemon/ghstorage_test.go`, replace these lines:

```go
	"fmt"
	"os"
	"slices"
	"sync"
```

with:

```go
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
```

In `internal/daemon/ghstorage_test.go`, replace these lines:

```go
	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/ghstorage"
	"github.com/darkraise/ghr/internal/model"
```

with:

```go
	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/changes"
	"github.com/darkraise/ghr/internal/ghstorage"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
```

Then append to the end of `internal/daemon/ghstorage_test.go`:

```go

func TestFingerprintsCountGitHubStorage(t *testing.T) {
	b, _, f := newGHSBackend(t)
	prev := b.fingerprints()
	if _, ok := prev["github-storage"]; !ok {
		t.Fatalf("no github-storage topic in %v", prev)
	}
	f.version.Add(1)
	cur := b.fingerprints()
	if got := changed(prev, cur); !slices.Equal(got, []string{"github-storage"}) {
		t.Fatalf("a snapshot change changed %v", got)
	}
	f.alert = &model.StorageAlert{Level: "warn", Month: "2026-10", UsedPct: 72}
	if got := changed(cur, b.fingerprints()); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("an alert change changed %v", got)
	}
}

func TestStorageErrText(t *testing.T) {
	text := storageErrText("darkraise")
	lacks := &github.APIError{Status: 403, Kind: github.ErrAuth, Message: "Resource not accessible", Permissions: "actions=read"}
	if got := text(lacks, "darkmem"); got != "token lacks actions=read on darkraise/darkmem: grant it in the PAT settings" {
		t.Fatalf("repository error %q", got)
	}
	missing := &github.APIError{Status: 404, Kind: github.ErrNotFound, Message: "Not Found"}
	if got := text(missing, ""); got != "github: 404 Not Found" {
		t.Fatalf("listing error %q", got)
	}
}

// The daemon starts the storage service and counts its topic: with an
// hour-long tick only wakes publish, and the pass a reload asks for changes
// the snapshot after the stream's baseline.
func TestRunStreamsGHStorage(t *testing.T) {
	old := hubOptions
	hubOptions = changes.Options{Tick: time.Hour}
	t.Cleanup(func() { hubOptions = old })
	o := testOptions(t, cfgYAML)
	reload := make(chan os.Signal, 1)
	o.Reload = reload
	startRun(t, o)
	c := api.NewUnixClient(o.Socket)
	waitFor(t, "the socket to serve status", func() bool { _, err := c.Status(context.Background()); return err == nil })
	req, err := http.NewRequest(http.MethodGet, "http://ghr/changes", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := (&http.Client{Transport: c.HTTP.Transport}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	frames := readFrames(resp.Body)
	if f := nextFrame(t, frames, "hello"); f != "event: hello\ndata: {}\n" {
		t.Fatalf("first frame %q", f)
	}
	reload <- os.Interrupt
	for !strings.Contains(nextFrame(t, frames, `a change naming "github-storage"`), `"github-storage"`) {
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run 'TestFingerprintsCountGitHubStorage|TestStorageErrText|TestRunStreamsGHStorage'`
Expected: FAIL, the build stops at `undefined: storageErrText`.

- [ ] **Step 3: Write the implementation**

In `internal/daemon/changes.go`, replace these lines:

```go
	out := make(map[string]uint64, 8)
```

with:

```go
	out := make(map[string]uint64, 9)
```

In `internal/daemon/changes.go`, replace these lines:

```go
		st.Restart, st.WebListenError = b.restartStatus(st), b.WebListenErr
		st.Now,
```

with:

```go
		st.Restart, st.WebListenError = b.restartStatus(st), b.WebListenErr
		if b.GHS != nil {
			st.GitHubStorage = b.GHS.Alert()
		}
		st.Now,
```

In `internal/daemon/changes.go`, replace these lines:

```go
	hash("metrics", func() any { return b.Metrics() })
	return out
```

with:

```go
	hash("metrics", func() any { return b.Metrics() })
	if b.GHS != nil {
		count("github-storage", b.GHS.Version)
	}
	return out
```

In `internal/daemon/run.go`, replace these lines:

```go
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/github"
```

with:

```go
	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/ghstorage"
	"github.com/darkraise/ghr/internal/github"
```

In `internal/daemon/run.go`, replace these lines:

```go
	space   *storage.Service
	b       *Backend
```

with:

```go
	space   *storage.Service
	ghs     *ghstorage.Service
	b       *Backend
```

In `internal/daemon/run.go`, replace these lines:

```go
	wake := make(chan struct{}, 1)
	b := &Backend{
		Store: store, M: m, GH: gh, Events: ev, Hist: hist, Space: space,
```

with:

```go
	ghs := &ghstorage.Service{GH: gh, Config: store.Config, Events: ev, Wake: hub.Wake, Describe: storageErrText(owner)}
	ghs.Start()

	wake := make(chan struct{}, 1)
	b := &Backend{
		Store: store, M: m, GH: gh, Events: ev, Hist: hist, Space: space, GHS: ghs,
```

In `internal/daemon/run.go`, replace these lines:

```go
	return &running{m: m, space: space, b: b, sampled: sampled, wake: wake}, nil
}
```

with:

```go
	return &running{m: m, space: space, ghs: ghs, b: b, sampled: sampled, wake: wake}, nil
}

// storageErrText words a repository's GitHub storage error as the Actions
// page words a repository's; an error about no one repository, such as a
// failed listing of them, keeps GitHub's text.
func storageErrText(owner string) func(err error, repo string) string {
	return func(err error, repo string) string {
		if repo == "" {
			return err.Error()
		}
		msg, _ := actionsErr(err, owner, repo)
		return msg
	}
}
```

In `internal/daemon/run.go`, replace these lines:

```go
	defer rt.space.Close()
```

with:

```go
	defer rt.space.Close()
	defer rt.ghs.Close()
```

In `internal/daemon/run.go`, replace these lines:

```go
			rt.space.Close()
			rt.b.Close()
```

with:

```go
			rt.space.Close()
			rt.ghs.Close()
			rt.b.Close()
```

In `internal/daemon/run.go`, replace these lines:

```go
				rt.space.Wait()
				close(done)
```

with:

```go
				rt.space.Wait()
				rt.ghs.Wait()
				close(done)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run 'TestFingerprintsCountGitHubStorage|TestStorageErrText|TestRunStreamsGHStorage|TestFingerprintsCoverEveryTopic' -v`
Expected: PASS for all four. `TestFingerprintsCoverEveryTopic` is unchanged: `newBackend` sets no `GHS`, so its topic list has no `github-storage`.

Then run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 600 go test -race ./internal/daemon/`
Expected: `ok` (about 12 seconds).

- [ ] **Step 5: Check and commit**

Run: `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (prints nothing) and `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./internal/... ./cmd/...` (passes).

```bash
git add internal/daemon/changes.go internal/daemon/run.go internal/daemon/ghstorage_test.go
git commit -m "feat(daemon): wire the GitHub storage service"
```

### Task 12: Add the github-storage routes

**Files:**
- Modify: `internal/api/server.go` (`Backend` interface, `NewServer`, a new `started` helper)
- Modify: `internal/api/api_test.go` (`fakeBackend` gains one field)
- Test: `internal/api/ghstorage_test.go` (new: the fake's methods and the route tests)

**Interfaces:**
- Consumes: Contracts → Model (Task 2): `model.GitHubStorage`, `StoragePreview`, `DeleteArtifactsRequest`, `DeleteCachesRequest`, `RetentionRequest`, `OperationStarted`; Contracts → Daemon and API (Task 10): the nine Backend methods, which `daemon.Backend` already has, so `var _ api.Backend = (*Backend)(nil)` keeps compiling.
- Produces: the routes in Contracts → Daemon and API (Task 12), which the web client (Task 15) calls. Bulk routes answer 202 with `{"id": "<operation id>"}`; the preview answers `[]`, never `null`. Test helper: `ghsFake` (`fakeBackend.ghs`: `snap`, `preview`, `err`, `calls`) and `ghsCall(t, srv, method, path, body) (int, string)` in `internal/api/ghstorage_test.go`.

**Items:** 3, 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

The routes only decode, check path ids and pass on: the body rules (exactly one of `ids` and `older_than`, the 1h floor, retention days), the 404 for a repository outside the snapshot and the 409s are the service's (Task 9) and the backend's (Task 10), and are tested there. An empty or malformed body is a 400 `invalid JSON: ...`; a path id that is not a positive integer is a 400 `artifact id must be a positive integer` or `cache id must be a positive integer`.

- [ ] **Step 1: Write the failing test**

In `internal/api/api_test.go`, replace these lines:

```go
	restarts      int            // RequestRestart calls
}
```

with:

```go
	restarts      int            // RequestRestart calls
	ghs           ghsFake        // the GitHub storage methods' state
}
```

Create `internal/api/ghstorage_test.go`:

```go
package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

// ghsFake is fakeBackend's GitHub storage state.
type ghsFake struct {
	snap    model.GitHubStorage
	preview []model.StoragePreview
	err     error // returned by every method that can fail
	calls   []string
}

func (f *fakeBackend) GitHubStorage() model.GitHubStorage { return f.ghs.snap }
func (f *fakeBackend) RefreshGitHubStorage() error {
	f.ghs.calls = append(f.ghs.calls, "refresh")
	return f.ghs.err
}
func (f *fakeBackend) GitHubStoragePreview(cleanup, repo string) ([]model.StoragePreview, error) {
	f.ghs.calls = append(f.ghs.calls, "preview "+cleanup+" "+repo)
	return f.ghs.preview, f.ghs.err
}
func (f *fakeBackend) DeleteGitHubArtifact(_ context.Context, repo string, id int64) error {
	f.ghs.calls = append(f.ghs.calls, fmt.Sprintf("delete-artifact %s %d", repo, id))
	return f.ghs.err
}
func (f *fakeBackend) DeleteGitHubCache(_ context.Context, repo string, id int64) error {
	f.ghs.calls = append(f.ghs.calls, fmt.Sprintf("delete-cache %s %d", repo, id))
	return f.ghs.err
}
func (f *fakeBackend) DeleteGitHubArtifacts(repo string, req model.DeleteArtifactsRequest) (model.OperationStarted, error) {
	f.ghs.calls = append(f.ghs.calls, fmt.Sprintf("delete-artifacts %s %v %s", repo, req.IDs, req.OlderThan))
	return model.OperationStarted{ID: "op-1"}, f.ghs.err
}
func (f *fakeBackend) DeleteGitHubCaches(repo string, req model.DeleteCachesRequest) (model.OperationStarted, error) {
	f.ghs.calls = append(f.ghs.calls, fmt.Sprintf("delete-caches %s %v %t", repo, req.IDs, req.All))
	return model.OperationStarted{ID: "op-2"}, f.ghs.err
}
func (f *fakeBackend) SetGitHubRetention(_ context.Context, repo string, days int) error {
	f.ghs.calls = append(f.ghs.calls, fmt.Sprintf("retention %s %d", repo, days))
	return f.ghs.err
}
func (f *fakeBackend) SetGitHubRetentionAll(days int) (model.OperationStarted, error) {
	f.ghs.calls = append(f.ghs.calls, fmt.Sprintf("retention-all %d", days))
	return model.OperationStarted{ID: "op-3"}, f.ghs.err
}

// ghsCall sends one request to srv and returns its status and body.
func ghsCall(t *testing.T, srv *httptest.Server, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSpace(string(b))
}

func TestGitHubStorageRoutes(t *testing.T) {
	b := &fakeBackend{}
	b.ghs.snap = model.GitHubStorage{QuotaError: "x", Repos: []model.RepoStorage{}, Deletions: []model.Deletion{}}
	srv := httptest.NewServer(NewServer(b))
	defer srv.Close()
	for _, tc := range []struct {
		method, path, body string
		status             int
		answer             string
		preview            []model.StoragePreview
	}{
		{"GET", "/github-storage", "", 200, `{"quota_error":"x","repos":[],"deletions":[]}`, nil},
		{"POST", "/github-storage/refresh", "", 202, "", nil},
		{"GET", "/github-storage/preview?cleanup=2d&repo=darkmem", "", 200, `[{"repo":"darkmem","count":2,"bytes":10}]`,
			[]model.StoragePreview{{Repo: "darkmem", Count: 2, Bytes: 10}}},
		{"GET", "/github-storage/preview?cleanup=off", "", 200, `[]`, nil},
		{"POST", "/github-storage/retention", `{"days":7}`, 202, `{"id":"op-3"}`, nil},
		{"POST", "/github-storage/darkmem/artifacts/delete", `{"ids":[1,2]}`, 202, `{"id":"op-1"}`, nil},
		{"POST", "/github-storage/darkmem/artifacts/delete", `{"older_than":"24h"}`, 202, `{"id":"op-1"}`, nil},
		{"DELETE", "/github-storage/darkmem/artifacts/9", "", 204, "", nil},
		{"POST", "/github-storage/darkmem/caches/delete", `{"all":true}`, 202, `{"id":"op-2"}`, nil},
		{"DELETE", "/github-storage/darkmem/caches/8", "", 204, "", nil},
		{"PUT", "/github-storage/darkmem/retention", `{"days":5}`, 204, "", nil},
	} {
		b.ghs.preview = tc.preview
		status, answer := ghsCall(t, srv, tc.method, tc.path, tc.body)
		if status != tc.status || answer != tc.answer {
			t.Errorf("%s %s: %d %s, want %d %s", tc.method, tc.path, status, answer, tc.status, tc.answer)
		}
	}
	want := []string{
		"refresh", "preview 2d darkmem", "preview off ", "retention-all 7",
		"delete-artifacts darkmem [1 2] ", "delete-artifacts darkmem [] 24h", "delete-artifact darkmem 9",
		"delete-caches darkmem [] true", "delete-cache darkmem 8", "retention darkmem 5",
	}
	if !reflect.DeepEqual(b.ghs.calls, want) {
		t.Fatalf("calls %q", b.ghs.calls)
	}
}

func TestGitHubStorageRouteErrors(t *testing.T) {
	b := &fakeBackend{}
	srv := httptest.NewServer(NewServer(b))
	defer srv.Close()
	for _, tc := range []struct{ method, path, body, answer string }{
		{"POST", "/github-storage/retention", `{"days":`, `invalid JSON`},
		{"POST", "/github-storage/darkmem/artifacts/delete", ``, `invalid JSON`},
		{"POST", "/github-storage/darkmem/caches/delete", `[1]`, `invalid JSON`},
		{"PUT", "/github-storage/darkmem/retention", `{"days":"7"}`, `invalid JSON`},
		{"DELETE", "/github-storage/darkmem/artifacts/abc", ``, `artifact id must be a positive integer`},
		{"DELETE", "/github-storage/darkmem/caches/0", ``, `cache id must be a positive integer`},
	} {
		status, answer := ghsCall(t, srv, tc.method, tc.path, tc.body)
		if status != 400 || !strings.Contains(answer, tc.answer) {
			t.Errorf("%s %s %s: %d %s", tc.method, tc.path, tc.body, status, answer)
		}
	}
	if len(b.ghs.calls) != 0 {
		t.Fatalf("a refused request reached the backend: %q", b.ghs.calls)
	}
	retry := time.Date(2026, 10, 13, 13, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		err    error
		status int
		answer string
	}{
		{NotFound("not in the last GitHub storage inventory"), 404, `{"error":"not in the last GitHub storage inventory"}`},
		{Conflict("another GitHub storage operation is running"), 409, `{"error":"another GitHub storage operation is running"}`},
		{BadRequest("give either ids or older_than"), 400, `{"error":"give either ids or older_than"}`},
		{&Error{Status: 429, Msg: "GitHub rate limit; API calls are paused", RetryAt: retry}, 429,
			`{"error":"GitHub rate limit; API calls are paused","retry_at":"2026-10-13T13:00:00Z"}`},
		{&Error{Status: 503, Msg: "GitHub is rejecting the token: Bad credentials"}, 503,
			`{"error":"GitHub is rejecting the token: Bad credentials"}`},
	} {
		b.ghs.err = tc.err
		for _, r := range []struct{ method, path, body string }{
			{"POST", "/github-storage/refresh", ""},
			{"GET", "/github-storage/preview?cleanup=2d", ""},
			{"POST", "/github-storage/retention", `{"days":7}`},
			{"POST", "/github-storage/darkmem/artifacts/delete", `{"ids":[1]}`},
			{"DELETE", "/github-storage/darkmem/artifacts/1", ""},
			{"POST", "/github-storage/darkmem/caches/delete", `{"ids":[1]}`},
			{"DELETE", "/github-storage/darkmem/caches/1", ""},
			{"PUT", "/github-storage/darkmem/retention", `{"days":7}`},
		} {
			status, answer := ghsCall(t, srv, r.method, r.path, r.body)
			if status != tc.status || answer != tc.answer {
				t.Errorf("%s %s with %v: %d %s", r.method, r.path, tc.err, status, answer)
			}
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/api/ -run GitHubStorage`
Expected: FAIL, every route answers `404 404 page not found`, for example `POST /github-storage/darkmem/caches/delete with GitHub is rejecting the token: Bad credentials: 404 404 page not found`.

- [ ] **Step 3: Write the implementation**

In `internal/api/server.go`, replace these lines:

```go
	PruneScope(scope string) error
	Subscribe() (*changes.Sub, error)
}
```

with:

```go
	PruneScope(scope string) error
	Subscribe() (*changes.Sub, error)
	GitHubStorage() model.GitHubStorage
	RefreshGitHubStorage() error
	GitHubStoragePreview(cleanup, repo string) ([]model.StoragePreview, error)
	DeleteGitHubArtifact(ctx context.Context, repo string, id int64) error
	DeleteGitHubCache(ctx context.Context, repo string, id int64) error
	DeleteGitHubArtifacts(repo string, req model.DeleteArtifactsRequest) (model.OperationStarted, error)
	DeleteGitHubCaches(repo string, req model.DeleteCachesRequest) (model.OperationStarted, error)
	SetGitHubRetention(ctx context.Context, repo string, days int) error
	SetGitHubRetentionAll(days int) (model.OperationStarted, error)
}
```

In `internal/api/server.go`, replace these lines:

```go
	mux.HandleFunc("POST /prune/{scope}", accepted(func(r *http.Request) error { return b.PruneScope(r.PathValue("scope")) }))
	return mux
}
```

with:

```go
	mux.HandleFunc("POST /prune/{scope}", accepted(func(r *http.Request) error { return b.PruneScope(r.PathValue("scope")) }))
	mux.HandleFunc("GET /github-storage", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, b.GitHubStorage()) })
	mux.HandleFunc("POST /github-storage/refresh", accepted(func(*http.Request) error { return b.RefreshGitHubStorage() }))
	mux.HandleFunc("GET /github-storage/preview", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		ps, err := b.GitHubStoragePreview(q.Get("cleanup"), q.Get("repo"))
		if ps == nil {
			ps = []model.StoragePreview{}
		}
		respond(w, ps, err)
	})
	mux.HandleFunc("POST /github-storage/retention", func(w http.ResponseWriter, r *http.Request) {
		var req model.RetentionRequest
		if err := decode(r, &req); err != nil {
			respond(w, nil, err)
			return
		}
		started(w)(b.SetGitHubRetentionAll(req.Days))
	})
	mux.HandleFunc("POST /github-storage/{repo}/artifacts/delete", func(w http.ResponseWriter, r *http.Request) {
		var req model.DeleteArtifactsRequest
		if err := decode(r, &req); err != nil {
			respond(w, nil, err)
			return
		}
		started(w)(b.DeleteGitHubArtifacts(r.PathValue("repo"), req))
	})
	mux.HandleFunc("DELETE /github-storage/{repo}/artifacts/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := positive(r.PathValue("id"), "artifact id")
		if err != nil {
			respond(w, nil, err)
			return
		}
		respond(w, nil, b.DeleteGitHubArtifact(r.Context(), r.PathValue("repo"), id))
	})
	mux.HandleFunc("POST /github-storage/{repo}/caches/delete", func(w http.ResponseWriter, r *http.Request) {
		var req model.DeleteCachesRequest
		if err := decode(r, &req); err != nil {
			respond(w, nil, err)
			return
		}
		started(w)(b.DeleteGitHubCaches(r.PathValue("repo"), req))
	})
	mux.HandleFunc("DELETE /github-storage/{repo}/caches/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := positive(r.PathValue("id"), "cache id")
		if err != nil {
			respond(w, nil, err)
			return
		}
		respond(w, nil, b.DeleteGitHubCache(r.Context(), r.PathValue("repo"), id))
	})
	mux.HandleFunc("PUT /github-storage/{repo}/retention", func(w http.ResponseWriter, r *http.Request) {
		var req model.RetentionRequest
		if err := decode(r, &req); err != nil {
			respond(w, nil, err)
			return
		}
		respond(w, nil, b.SetGitHubRetention(r.Context(), r.PathValue("repo"), req.Days))
	})
	return mux
}
```

In `internal/api/server.go`, replace these lines:

```go
func decode(r *http.Request, v any) error {
```

with:

```go
// started answers 202 with the id of the operation a call started.
func started(w http.ResponseWriter) func(op model.OperationStarted, err error) {
	return func(op model.OperationStarted, err error) {
		if err != nil {
			respond(w, nil, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(op)
	}
}

func decode(r *http.Request, v any) error {
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/api/ -run GitHubStorage -v`
Expected: PASS for `TestGitHubStorageRoutes` and `TestGitHubStorageRouteErrors`.

Then run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./...` (passes: `daemon.Backend` still satisfies `api.Backend`) and `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 600 go test ./internal/api/ ./internal/daemon/ ./internal/webui/`
Expected: three `ok` lines.

- [ ] **Step 5: Check and commit**

Run: `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (prints nothing).

```bash
git add internal/api/server.go internal/api/ghstorage_test.go internal/api/api_test.go
git commit -m "feat(api): add the github-storage routes"
```

### Task 13: Show the quota in ghr status

**Files:**
- Modify: `cmd/ghr/cli.go` (`printStatus`, a new `artifactsLine`)
- Test: `cmd/ghr/cli_test.go` (one new test)

**Interfaces:**
- Consumes: Contracts → Model (Task 2): `model.Status.GitHubStorage *model.StorageAlert`, filled by the daemon (Task 10).
- Produces: nothing other tasks read.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

The line follows the runner line, once a quota exists. The spec gives the exhausted form; the others are this plan's: `artifacts  <used> / <included> GB-h this month (<pct>%)`, then `, exhausted until <Mon D>` when exhausted, otherwise `, critical` or `, warning` for those levels and `, runs out around <Mon D>` when `runs_out_at` is set. Dates are UTC, figures whole GB-hours.

- [ ] **Step 1: Write the failing test**

Append to the end of `cmd/ghr/cli_test.go`:

```go

func TestStatusPrintsTheArtifactQuota(t *testing.T) {
	resets := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	runsOut := time.Date(2026, 10, 24, 6, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		alert *model.StorageAlert
		want  string
	}{
		{&model.StorageAlert{Level: "exhausted", UsedGBHours: 373.3, IncludedGBHours: 363.28, UsedPct: 103, ResetsAt: resets},
			"artifacts  373 / 363 GB-h this month (103%), exhausted until Nov 1"},
		{&model.StorageAlert{Level: "critical", UsedGBHours: 330.2, IncludedGBHours: 363.28, UsedPct: 91, RunsOutAt: &runsOut, ResetsAt: resets},
			"artifacts  330 / 363 GB-h this month (91%), critical, runs out around Oct 24"},
		{&model.StorageAlert{Level: "warn", UsedGBHours: 260, IncludedGBHours: 363.28, UsedPct: 72, RunsOutAt: &runsOut, ResetsAt: resets},
			"artifacts  260 / 363 GB-h this month (72%), warning, runs out around Oct 24"},
		{&model.StorageAlert{Level: "projected", UsedGBHours: 120, IncludedGBHours: 363.28, UsedPct: 33, RunsOutAt: &runsOut, ResetsAt: resets},
			"artifacts  120 / 363 GB-h this month (33%), runs out around Oct 24"},
		{&model.StorageAlert{UsedGBHours: 20.4, IncludedGBHours: 363.28, UsedPct: 6, ResetsAt: resets},
			"artifacts  20 / 363 GB-h this month (6%)"},
		{nil, ""},
	} {
		var out bytes.Buffer
		printStatus(&out, model.Status{Mode: "queue", GlobalMax: 1, GitHubStorage: tc.alert})
		got := ""
		for _, line := range strings.Split(out.String(), "\n") {
			if strings.HasPrefix(line, "artifacts") {
				got = line
			}
		}
		if got != tc.want {
			t.Errorf("artifacts line %q, want %q in:\n%s", got, tc.want, out.String())
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test ./cmd/ghr/ -run TestStatusPrintsTheArtifactQuota`
Expected: FAIL with `artifacts line "", want "artifacts  373 / 363 GB-h this month (103%), exhausted until Nov 1"`.

- [ ] **Step 3: Write the implementation**

In `cmd/ghr/cli.go`, replace these lines:

```go
	fmt.Fprintln(out, runnerLine(st))
	if st.Degraded {
```

with:

```go
	fmt.Fprintln(out, runnerLine(st))
	if a := st.GitHubStorage; a != nil {
		fmt.Fprintln(out, artifactsLine(*a))
	}
	if st.Degraded {
```

In `cmd/ghr/cli.go`, replace these lines:

```go
// daysLeft is the time to deadline in whole days, or "overdue".
```

with:

```go
// artifactsLine is the status line about the month's GitHub artifact
// storage. Dates are UTC, as GitHub's month is.
func artifactsLine(a model.StorageAlert) string {
	s := fmt.Sprintf("artifacts  %.0f / %.0f GB-h this month (%d%%)", a.UsedGBHours, a.IncludedGBHours, a.UsedPct)
	switch a.Level {
	case "exhausted":
		return s + ", exhausted until " + a.ResetsAt.UTC().Format("Jan 2")
	case "critical":
		s += ", critical"
	case "warn":
		s += ", warning"
	}
	if a.RunsOutAt != nil {
		s += ", runs out around " + a.RunsOutAt.UTC().Format("Jan 2")
	}
	return s
}

// daysLeft is the time to deadline in whole days, or "overdue".
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test ./cmd/ghr/`
Expected: `ok`.

- [ ] **Step 5: Check and commit**

Run: `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (prints nothing) and `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./internal/... ./cmd/...` (passes).

```bash
git add cmd/ghr/cli.go cmd/ghr/cli_test.go
git commit -m "feat(cli): show the artifact quota in ghr status"
```

### Task 14: Add the storage types and fixture

**Files:**
- Modify: `web/src/api/types.ts` (`Status`, `Config`, `ConfigPatch`, new types at the end)
- Modify: `web/fixtures_test.go` (the `config` fixture, two new fixtures, `githubStorageFixture`)
- Create (generated): `web/src/api/fixtures/github-storage.json`, `web/src/api/fixtures/storage-alert.json`; Modify (generated): `web/src/api/fixtures/config.json`
- Modify: `web/src/test/fixtures.ts` (`fixtures.githubStorage`, `fixtures.storageAlert`, `authedRoutes`)
- Test: `web/src/api/types.test.ts`

**Interfaces:**
- Consumes: Contracts → Model (Task 2) and Config (Task 3): the Go types the fixtures marshal.
- Produces: Contracts → Web → Types; the fixture contents in Contracts → Web → Fixtures.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

In `web/src/api/types.test.ts`, add two imports in sorted place:

```ts
import githubStorageJson from "./fixtures/github-storage.json"
```

after `import eventsJson from "./fixtures/events.json"`, and

```ts
import storageAlertJson from "./fixtures/storage-alert.json"
```

after `import statusJson from "./fixtures/status.json"`. In the `import type { ... } from "./types"` list add `GitHubStorage,` after `GhrEvent,` and `StorageAlert,` after `Storage,`.

Replace this line:

```ts
describe("run detail fixtures", () => {
```

with:

```ts
describe("GitHub storage fixtures", () => {
  const snap: GitHubStorage = githubStorageJson
  const alert: StorageAlert = storageAlertJson

  it("the snapshot carries the quota, the rows, the operation and the deletions", () => {
    expect(snap.quota?.level).toBe("warn")
    expect(snap.quota?.over_allowance_gb_hours).toBe(0)
    expect(snap.quota?.runs_out_at).toBe("2026-10-24T11:45:00Z")
    expect(snap.repos.map((r) => [r.name, r.covered, r.cleanup, r.cleanup_source])).toEqual([
      ["darkmem", true, "6h", "override"],
      ["darkcloud", true, "2d", "global"],
      ["sandbox", false, "2d", "global"],
    ])
    expect(snap.repos[0]?.artifacts[0]).toEqual({
      id: 9001,
      name: "frontend-dist",
      bytes: 157286400,
      created_at: "2026-10-02T14:05:00Z",
      expires_at: "2026-12-31T14:05:00Z",
      run_id: 102,
      branch: "master",
    })
    expect(snap.repos[0]?.caches[0]?.last_used_at).toBe("2026-10-03T13:05:00Z")
    expect(snap.repos[2]?.error).toContain("token lacks actions=read")
    expect(snap.repos[0]).not.toHaveProperty("error")
    expect(snap.operation?.failed[0]?.id).toBe(9003)
    expect(snap.deletions.map((d) => [d.kind, d.by])).toEqual([
      ["artifact", "cleanup"],
      ["cache", "manual"],
    ])
    expect(snap).not.toHaveProperty("quota_error")
  })

  it("the alert and the config block keep their names", () => {
    expect(alert).toEqual({
      level: "critical",
      month: "2026-10",
      used_gb_hours: 330.5,
      included_gb_hours: 363.28125,
      used_pct: 91,
      runs_out_at: "2026-10-06T02:05:00Z",
      resets_at: "2026-11-01T00:00:00Z",
    })
    expect(fixtures.config.github_storage).toEqual({ cleanup_max_age: "2d", repos: { darkmem: { cleanup: "6h" } } })
    expect(fixtures.status).not.toHaveProperty("github_storage")
  })
})

describe("run detail fixtures", () => {
```

- [ ] **Step 2: Run the test to see it fail**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/api/types.test.ts`
Expected: FAIL with `Failed to resolve import "./fixtures/github-storage.json" from "src/api/types.test.ts". Does the file exist?`

- [ ] **Step 3: Add the types**

In `web/src/api/types.ts`, replace these lines (the end of `Status`):

```ts
  restart: RestartStatus
  web_listen_error?: WebListenError
}
```

with:

```ts
  restart: RestartStatus
  web_listen_error?: WebListenError
  github_storage?: StorageAlert
}
```

Replace these lines (the end of `Config`):

```ts
  watch_repos?: string[]
  web?: WebConfig
}
```

with:

```ts
  watch_repos?: string[]
  web?: WebConfig
  github_storage?: GitHubStorageConfig
}
```

Replace these lines (the end of `ConfigPatch`):

```ts
  repos?: Record<string, RepoPatch>
  web?: WebPatch
}
```

with:

```ts
  repos?: Record<string, RepoPatch>
  web?: WebPatch
  github_storage?: GitHubStoragePatch
}
```

Append to the end of the file. The level, kind, source and by fields are `string`, not unions: a fixture imported from JSON reads its strings as `string`, as the existing `Operation.kind` does.

```ts
// The quota levels; the snapshot sends them as plain strings.
export type QuotaLevel = "" | "projected" | "warn" | "critical" | "exhausted"

export interface GitHubQuota {
  month: string
  read_at: string
  included_gb_hours: number
  used_gb_hours: number
  actions_gb_hours: number
  packages_gb_hours: number
  rate_gb_hours_per_hour: number
  estimated_gb_hours: number
  over_allowance_gb_hours?: number
  projected_gb_hours: number
  runs_out_at?: string
  level: string
  resets_at: string
}

export interface ArtifactItem {
  id: number
  name: string
  bytes: number
  created_at: string
  expires_at?: string
  run_id: number
  branch: string
}

export interface CacheItem {
  id: number
  key: string
  ref: string
  bytes: number
  last_used_at: string
}

export interface RepoStorage {
  name: string
  visibility: string
  covered: boolean
  artifact_count: number
  artifact_bytes: number
  cache_count: number
  cache_bytes: number
  retention_days: number
  retention_max_days: number
  cleanup: string
  cleanup_source: string
  artifacts: ArtifactItem[]
  caches: CacheItem[]
  error?: string
}

export interface OperationFailure {
  repo: string
  id?: number
  error: string
}

export interface GitHubOperation {
  id: string
  kind: string
  repo?: string
  total: number
  done: number
  bytes: number
  failed: OperationFailure[]
  started_at: string
  finished_at?: string
}

export interface Deletion {
  at: string
  repo: string
  kind: string
  name: string
  bytes: number
  age: string
  by: string
}

export interface GitHubStorage {
  checked_at?: string
  quota?: GitHubQuota
  quota_error?: string
  error?: string
  repos: RepoStorage[]
  operation?: GitHubOperation
  deletions: Deletion[]
}

export interface StorageAlert {
  level: string
  month: string
  used_gb_hours: number
  included_gb_hours: number
  used_pct: number
  runs_out_at?: string
  resets_at: string
}

export interface GitHubStorageConfig {
  included_mb?: number
  warn_pct?: number[]
  cleanup_max_age?: string
  repos?: Record<string, { cleanup: string }>
}

// An empty cleanup_max_age turns the cleanup off; a null repository value
// removes that repository's override.
export interface GitHubStoragePatch {
  included_mb?: number
  warn_pct?: number[]
  cleanup_max_age?: string
  repos?: Record<string, string | null>
}

export interface StoragePreview {
  repo: string
  count: number
  bytes: number
}
```

- [ ] **Step 4: Add the Go fixture values**

In `web/fixtures_test.go`, replace this line in the `config` fixture:

```go
			Web:        config.Web{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan"}},
```

with:

```go
			Web:        config.Web{Listen: "0.0.0.0:8080", Hosts: []string{"ghr.lan"}},
			GitHubStorage: config.GitHubStorage{CleanupMaxAge: config.Duration(48 * time.Hour),
				Repos: map[string]config.StorageOverride{"darkmem": {Cleanup: "6h"}}},
```

Replace this line:

```go
		"toolchain-choices": []model.ToolchainChoice{{Spec: "22.11.0", Version: "22.11.0", LTS: true}, {Spec: "24.9.0", Version: "24.9.0"}},
```

with:

```go
		"github-storage": githubStorageFixture(),
		"storage-alert": model.StorageAlert{Level: "critical", Month: "2026-10", UsedGBHours: 330.5, IncludedGBHours: 363.28125,
			UsedPct: 91, RunsOutAt: ptr(at(60 * time.Hour)), ResetsAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		"toolchain-choices": []model.ToolchainChoice{{Spec: "22.11.0", Version: "22.11.0", LTS: true}, {Spec: "24.9.0", Version: "24.9.0"}},
```

Append to the end of the file:

```go
// githubStorageFixture is a snapshot at the warn level: one repository with
// an override, one on the global age, and one the token cannot read.
func githubStorageFixture() model.GitHubStorage {
	return model.GitHubStorage{
		CheckedAt: ptr(at(-5 * time.Minute)),
		Quota: &model.GitHubQuota{Month: "2026-10", ReadAt: at(-20 * time.Minute), IncludedGBHours: 363.28125,
			UsedGBHours: 260.5, ActionsGBHours: 250.25, PackagesGBHours: 10.25, RateGBHoursPerHour: 0.2,
			EstimatedGBHours: 262.9, OverAllowanceGBHours: ptr(0.0), ProjectedGBHours: 396.95,
			RunsOutAt: ptr(time.Date(2026, 10, 24, 11, 45, 0, 0, time.UTC)), Level: "warn",
			ResetsAt: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)},
		Repos: []model.RepoStorage{
			{Name: "darkmem", Visibility: "private", Covered: true, ArtifactCount: 2, ArtifactBytes: 209_715_200,
				CacheCount: 1, CacheBytes: 71_431_401, RetentionDays: 90, RetentionMaxDays: 400, Cleanup: "6h", CleanupSource: "override",
				Artifacts: []model.ArtifactItem{
					{ID: 9001, Name: "frontend-dist", Bytes: 157_286_400, CreatedAt: at(-24 * time.Hour),
						ExpiresAt: ptr(at(89 * 24 * time.Hour)), RunID: 102, Branch: "master"},
					{ID: 9002, Name: "coverage", Bytes: 52_428_800, CreatedAt: at(-3 * time.Hour),
						ExpiresAt: ptr(at(90*24*time.Hour - 3*time.Hour)), RunID: 103, Branch: "feature/x"},
				},
				Caches: []model.CacheItem{{ID: 8787967584, Key: "node-cache-Linux-x64-npm-a4cc85d3", Ref: "refs/heads/master",
					Bytes: 71_431_401, LastUsedAt: at(-time.Hour)}}},
			{Name: "darkcloud", Visibility: "private", Covered: true, ArtifactCount: 1, ArtifactBytes: 20_971_520,
				RetentionDays: 7, RetentionMaxDays: 90, Cleanup: "2d", CleanupSource: "global",
				Artifacts: []model.ArtifactItem{{ID: 9101, Name: "dist", Bytes: 20_971_520, CreatedAt: at(-30 * time.Hour),
					ExpiresAt: ptr(at(7*24*time.Hour - 30*time.Hour)), RunID: 501, Branch: "main"}},
				Caches: []model.CacheItem{}},
			{Name: "sandbox", Visibility: "public", Cleanup: "2d", CleanupSource: "global",
				Artifacts: []model.ArtifactItem{}, Caches: []model.CacheItem{},
				Error: "token lacks actions=read on darkraise/sandbox: grant it in the PAT settings"},
		},
		Operation: &model.GitHubOperation{ID: "op-1", Kind: "delete-artifacts", Repo: "darkmem", Total: 3, Done: 3, Bytes: 52_428_800,
			Failed:    []model.OperationFailure{{Repo: "darkmem", ID: 9003, Error: "not in the last inventory"}},
			StartedAt: at(-10 * time.Minute), FinishedAt: ptr(at(-9 * time.Minute))},
		Deletions: []model.Deletion{
			{At: at(-9 * time.Minute), Repo: "darkmem", Kind: "artifact", Name: "coverage-old", Bytes: 52_428_800, Age: "3d", By: "cleanup"},
			{At: at(-2 * time.Hour), Repo: "darkcloud", Kind: "cache", Name: "node-cache-Linux-x64-npm-0b1c", Bytes: 31_457_280, Age: "5d", By: "manual"},
		},
	}
}
```

Then regenerate the JSON fixtures and check gofmt:

Run: `PATH=/usr/local/go/bin:$PATH gofmt -l web/ && TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH GHR_UPDATE_FIXTURES=1 timeout 300 go test ./web/ && git status --short web/src/api/fixtures/`
Expected: gofmt prints nothing, `ok`, and `M web/src/api/fixtures/config.json` plus the two new files `github-storage.json` and `storage-alert.json`. `config.json` gains only the `github_storage` block (`git diff web/src/api/fixtures/config.json`).

- [ ] **Step 5: Export the fixtures to the web tests**

In `web/src/test/fixtures.ts`, add `import githubStorageJson from "@/api/fixtures/github-storage.json"` after the `events.json` import and `import storageAlertJson from "@/api/fixtures/storage-alert.json"` after the `storage.json` import; in the `import type { ... } from "@/api/types"` list add `GitHubStorage,` after `GhrEvent,` and `StorageAlert,` after `Storage,`.

Replace this line:

```ts
const jobLog: JobLog = jobLogJson
```

with:

```ts
const jobLog: JobLog = jobLogJson
const githubStorage: GitHubStorage = githubStorageJson
const storageAlert: StorageAlert = storageAlertJson
```

Replace these lines (the end of `fixtures`):

```ts
  jobLog,
}
```

with:

```ts
  jobLog,
  githubStorage,
  storageAlert,
}
```

Replace these lines in `authedRoutes`:

```ts
    "GET /api/actions": fixtures.actions,
    ...over,
```

with:

```ts
    "GET /api/actions": fixtures.actions,
    "GET /api/github-storage": fixtures.githubStorage,
    ...over,
```

- [ ] **Step 6: Run the tests to see them pass**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/api/types.test.ts && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`, then from the root `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go test ./web/ && TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./web/`
Expected: 16 tests pass; tsc and eslint print nothing; `ok`; vet prints nothing. The config fixture's new block is read by every page test: the whole web suite (96 files, 1,319 tests) passed with it when this task was written.

- [ ] **Step 7: Commit**

```bash
git add web/fixtures_test.go web/src/api/types.ts web/src/api/types.test.ts web/src/test/fixtures.ts web/src/api/fixtures/config.json web/src/api/fixtures/github-storage.json web/src/api/fixtures/storage-alert.json
git commit -m "feat(web): add the GitHub storage types"
```

### Task 15: Add the storage API calls

**Files:**
- Modify: `web/src/api/client.ts` (imports, the end of `api`)
- Test: `web/src/api/client-pages.test.ts`

**Interfaces:**
- Consumes: Contracts → Web → Types (Task 14).
- Produces: Contracts → Web → Client.

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

In `web/src/api/client-pages.test.ts`, replace these lines:

```ts
    ["prune a scope", () => api.prune("unused-volumes"), "POST", "/api/prune/unused-volumes", undefined],
  ])("%s sends its request"
```

with:

```ts
    ["prune a scope", () => api.prune("unused-volumes"), "POST", "/api/prune/unused-volumes", undefined],
    ["refreshGitHubStorage", () => api.refreshGitHubStorage(), "POST", "/api/github-storage/refresh", undefined],
    ["deleteArtifact", () => api.deleteArtifact("dark mem", 9001), "DELETE", "/api/github-storage/dark%20mem/artifacts/9001", undefined],
    ["deleteCache", () => api.deleteCache("darkmem", 77), "DELETE", "/api/github-storage/darkmem/caches/77", undefined],
    ["deleteArtifacts by id", () => api.deleteArtifacts("darkmem", { ids: [9001, 9002] }), "POST", "/api/github-storage/darkmem/artifacts/delete", { ids: [9001, 9002] }],
    ["deleteArtifacts by age", () => api.deleteArtifacts("darkmem", { older_than: "2d" }), "POST", "/api/github-storage/darkmem/artifacts/delete", { older_than: "2d" }],
    ["deleteCaches by id", () => api.deleteCaches("darkmem", { ids: [77] }), "POST", "/api/github-storage/darkmem/caches/delete", { ids: [77] }],
    ["deleteCaches all", () => api.deleteCaches("darkmem", { all: true }), "POST", "/api/github-storage/darkmem/caches/delete", { all: true }],
    ["setRetention", () => api.setRetention("darkmem", 7), "PUT", "/api/github-storage/darkmem/retention", { days: 7 }],
    ["setRetentionAll", () => api.setRetentionAll(5), "POST", "/api/github-storage/retention", { days: 5 }],
  ])("%s sends its request"
```

Replace this line:

```ts
  it("sends a replacement token as plain text, unchanged", async () => {
```

with:

```ts
  it("reads the GitHub storage snapshot and a cleanup preview", async () => {
    const preview = [{ repo: "darkmem", count: 2, bytes: 209715200 }]
    const { calls } = mockApi({ "GET /api/github-storage": fixtures.githubStorage, "GET /api/github-storage/preview": preview })
    expect((await api.githubStorage()).repos).toHaveLength(3)
    expect(await api.storagePreview("2d")).toEqual(preview)
    await api.storagePreview("off", "dark mem")
    expect(calls.map((c) => c.search)).toEqual(["", "?cleanup=2d", "?cleanup=off&repo=dark+mem"])
  })

  it("sends a replacement token as plain text, unchanged", async () => {
```

- [ ] **Step 2: Run the test to see it fail**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/api/client-pages.test.ts`
Expected: FAIL, 10 tests, with `TypeError: api.refreshGitHubStorage is not a function` and the like.

- [ ] **Step 3: Add the calls**

In `web/src/api/client.ts`, in the `import type { ... } from "./types"` list add `GitHubStorage,` after `GhrEvent,` and `StoragePreview,` after `Storage,`. Replace these lines (the end of `api`):

```ts
  prune: (scope: PruneScope) => send("POST", scope === "standard" ? "/api/prune" : `/api/prune/${seg(scope)}`),
}
```

with:

```ts
  prune: (scope: PruneScope) => send("POST", scope === "standard" ? "/api/prune" : `/api/prune/${seg(scope)}`),
  githubStorage: (signal?: AbortSignal) => request<GitHubStorage>("GET", "/api/github-storage", undefined, signal),
  refreshGitHubStorage: () => send("POST", "/api/github-storage/refresh"),
  storagePreview: (cleanup: string, repo?: string, signal?: AbortSignal) =>
    request<StoragePreview[]>("GET", `/api/github-storage/preview?${query({ cleanup, ...(repo ? { repo } : {}) })}`, undefined, signal),
  deleteArtifact: (repo: string, id: number) => send("DELETE", `/api/github-storage/${seg(repo)}/artifacts/${id}`),
  deleteCache: (repo: string, id: number) => send("DELETE", `/api/github-storage/${seg(repo)}/caches/${id}`),
  // The bulk deletes answer 202; their progress shows in the snapshot.
  deleteArtifacts: (repo: string, body: { ids: number[] } | { older_than: string }) =>
    send("POST", `/api/github-storage/${seg(repo)}/artifacts/delete`, body),
  deleteCaches: (repo: string, body: { ids: number[] } | { all: true }) =>
    send("POST", `/api/github-storage/${seg(repo)}/caches/delete`, body),
  setRetention: (repo: string, days: number) => send("PUT", `/api/github-storage/${seg(repo)}/retention`, { days }),
  setRetentionAll: (days: number) => send("POST", "/api/github-storage/retention", { days }),
}
```

- [ ] **Step 4: Run the test to see it pass**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/api/client-pages.test.ts && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`
Expected: 50 tests pass; tsc and eslint print nothing.

- [ ] **Step 5: Commit**

```bash
git add web/src/api/client.ts web/src/api/client-pages.test.ts
git commit -m "feat(web): add the GitHub storage API calls"
```

### Task 16: Add the storage query and change topic

**Files:**
- Modify: `web/src/api/hooks.ts` (import, `keys`, new `githubStorageBusy` and `useGitHubStorage`)
- Modify: `web/src/api/use-change-stream.ts` (`TOPIC_QUERIES`)
- Test: `web/src/api/hooks-live.test.tsx`, `web/src/api/use-change-stream.test.tsx`

**Interfaces:**
- Consumes: Contracts → Web → Client (Task 15); the daemon's `github-storage` topic (Contracts → Daemon and API, Task 11).
- Produces: Contracts → Web → Hook.

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `web/src/api/use-change-stream.test.tsx`, make these replacements.

```ts
  keys.storage,
  keys.activity("1h", "UTC", ""),
```

becomes:

```ts
  keys.storage,
  keys.githubStorage,
  keys.activity("1h", "UTC", ""),
```

```ts
const ALL = ["activity", "config", "events", "history", "metrics", "status", "storage", "token"]
```

becomes:

```ts
const ALL = ["activity", "config", "events", "github-storage", "history", "metrics", "status", "storage", "token"]
```

```ts
    ["config", ["activity", "config"]],
```

becomes:

```ts
    ["config", ["activity", "config", "github-storage"]],
```

```ts
    ["storage", ["storage"]],
    ["history",
```

becomes:

```ts
    ["storage", ["storage"]],
    ["github-storage", ["github-storage"]],
    ["history",
```

```ts
    expect(Object.keys(TOPIC_QUERIES).sort()).toEqual(["config", "events", "gauges", "history", "metrics", "status", "storage", "token"])
```

becomes:

```ts
    expect(Object.keys(TOPIC_QUERIES).sort()).toEqual([
      "config",
      "events",
      "gauges",
      "github-storage",
      "history",
      "metrics",
      "status",
      "storage",
      "token",
    ])
```

In `web/src/api/hooks-live.test.tsx`, add `useGitHubStorage,` to the `./hooks` import after `useEvents,`. Replace these lines:

```ts
    { name: "useStorage", key: keys.storage, want: POLL_SLOW, use: () => useStorage(undefined) },
  ]
```

with:

```ts
    { name: "useStorage", key: keys.storage, want: POLL_SLOW, use: () => useStorage(undefined) },
    { name: "useGitHubStorage", key: keys.githubStorage, want: POLL_SLOW, use: () => useGitHubStorage() },
  ]
```

Replace this line:

```ts
describe("hooks the stream does not cover", () => {
```

with:

```ts
describe("useGitHubStorage", () => {
  it("polls every second while a bulk operation runs, and every 60 s while live", async () => {
    const running = { id: "op-2", kind: "delete-caches", repo: "darkmem", total: 4, done: 1, bytes: 0, failed: [], started_at: "2026-10-03T14:04:00Z" }
    mockApi(authedRoutes({ "GET /api/github-storage": { ...fixtures.githubStorage, operation: running } }))
    const { queryClient, wrapper } = withQuery()
    renderHook(() => useGitHubStorage(), { wrapper })
    await waitFor(() => expect(interval(queryClient, keys.githubStorage)).toBe(POLL_FAST))
    act(() => setLive(true))
    await waitFor(() => expect(interval(queryClient, keys.githubStorage)).toBe(POLL_SAFETY))
  })
})

describe("hooks the stream does not cover", () => {
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/api/use-change-stream.test.tsx src/api/hooks-live.test.tsx`
Expected: FAIL, 6 tests, with `TypeError: useGitHubStorage is not a function` among them.

- [ ] **Step 3: Add the hook and the topic**

In `web/src/api/hooks.ts`, replace this line:

```ts
import type { ActivityWindow, GhrEvent, LogChunk, RunAnnotations, RunDetail, Status, Storage } from "./types"
```

with:

```ts
import type { ActivityWindow, GhrEvent, GitHubStorage, LogChunk, RunAnnotations, RunDetail, Status, Storage } from "./types"
```

Replace these lines in `keys`:

```ts
  storage: ["storage"] as const,
  availableRepos
```

with:

```ts
  storage: ["storage"] as const,
  githubStorage: ["github-storage"] as const,
  availableRepos
```

Replace this line:

```ts
export function useLabelCheck(
```

with:

```ts
// A bulk delete or retention change reports its progress only in the
// snapshot, so the snapshot is read every second while one runs.
export function githubStorageBusy(storage?: GitHubStorage): boolean {
  return storage?.operation !== undefined && storage.operation.finished_at === undefined
}

export function useGitHubStorage() {
  const live = useLive()
  return useQuery({
    queryKey: keys.githubStorage,
    queryFn: ({ signal }) => api.githubStorage(signal),
    refetchInterval: (q) => livePoll(live, githubStorageBusy(q.state.data) ? POLL_FAST : POLL_SLOW),
  })
}

export function useLabelCheck(
```

In `web/src/api/use-change-stream.ts`, replace this line:

```ts
  config: [keys.config, ["activity"]],
```

with:

```ts
  // The snapshot's cleanup and covered columns follow the config.
  config: [keys.config, ["activity"], keys.githubStorage],
```

and this line:

```ts
  storage: [keys.storage],
```

with:

```ts
  storage: [keys.storage],
  "github-storage": [keys.githubStorage],
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/api/use-change-stream.test.tsx src/api/hooks-live.test.tsx && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`
Expected: 29 tests pass; tsc and eslint print nothing.

- [ ] **Step 5: Commit**

```bash
git add web/src/api/hooks.ts web/src/api/use-change-stream.ts web/src/api/hooks-live.test.tsx web/src/api/use-change-stream.test.tsx
git commit -m "feat(web): read the GitHub storage snapshot"
```

### Task 17: Add the quota text helpers

**Files:**
- Create: `web/src/lib/quota.ts`
- Test: `web/src/lib/quota.test.ts`

**Interfaces:**
- Consumes: Contracts → Web → Types and Fixtures (Task 14).
- Produces: Contracts → Web → Text helpers.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/quota.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import type { StorageAlert } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { bannerText, canDismiss, dismiss, gbh, isDismissed, utcDay } from "./quota"

const alert = (over: Partial<StorageAlert>): StorageAlert => ({ ...fixtures.storageAlert, ...over })

describe("quota text", () => {
  it.each([
    [373.31, "373 GB-h"],
    [10, "10 GB-h"],
    [2.46, "2.5 GB-h"],
    [0, "0 GB-h"],
  ])("writes %d GB-hours as %s", (n, want) => {
    expect(gbh(n)).toBe(want)
  })

  it("writes a day in UTC", () => {
    expect(utcDay("2026-10-24T23:30:00Z")).toBe("Oct 24")
    expect(utcDay("2026-11-01T00:00:00Z")).toBe("Nov 1")
  })

  it.each<[Partial<StorageAlert>, string]>([
    [{ level: "exhausted", used_pct: 103 }, "GitHub artifact storage for October is used up: uploads fail until Nov 1."],
    [{ level: "critical" }, "GitHub artifact storage is at 91% of October's allowance and runs out around Oct 6."],
    [{ level: "warn", used_pct: 72, runs_out_at: undefined }, "GitHub artifact storage is at 72% of October's allowance."],
    [{ level: "projected", used_pct: 40, runs_out_at: "2026-10-24T11:45:00Z" }, "GitHub artifact storage runs out around Oct 24, before the month resets on Nov 1."],
    [{ level: "projected", used_pct: 40, runs_out_at: undefined }, "GitHub artifact storage runs out before the month resets on Nov 1."],
  ])("words the %j banner", (over, want) => {
    expect(bannerText(alert(over))).toBe(want)
  })

  it("lets only the early levels be dismissed", () => {
    expect(["", "projected", "warn", "critical", "exhausted"].map(canDismiss)).toEqual([false, true, true, false, false])
  })
})

describe("banner dismissal", () => {
  beforeEach(() => localStorage.clear())
  afterEach(() => vi.restoreAllMocks())

  it("keeps a dismissal per level and month", () => {
    const warn = alert({ level: "warn" })
    expect(isDismissed(warn)).toBe(false)
    dismiss(warn)
    expect(isDismissed(warn)).toBe(true)
    expect(isDismissed(alert({ level: "projected" }))).toBe(false)
    expect(isDismissed(alert({ level: "warn", month: "2026-11" }))).toBe(false)
  })

  it("never hides a level that cannot be dismissed", () => {
    const critical = alert({ level: "critical" })
    dismiss(critical)
    expect(isDismissed(critical)).toBe(false)
  })

  it("shows the banner when local storage cannot be read or written", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied")
    })
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("denied")
    })
    const warn = alert({ level: "warn" })
    expect(() => dismiss(warn)).not.toThrow()
    expect(isDismissed(warn)).toBe(false)
  })
})
```

- [ ] **Step 2: Run the test to see it fail**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/lib/quota.test.ts`
Expected: FAIL with `Failed to resolve import "./quota" from "src/lib/quota.test.ts". Does the file exist?`

- [ ] **Step 3: Write the helpers**

Create `web/src/lib/quota.ts`:

```ts
import type { QuotaLevel, StorageAlert } from "@/api/types"

export const CLEANUP_WARNING =
  "Cleanup does not protect an artifact that a different run downloads later, such as one a workflow_run workflow, a download by run ID or a nightly job reading the last build needs."

export const RETENTION_WARNING = "GitHub's retention also limits how long it keeps runs and logs, which ghr's Actions and log pages read."

export function gbh(n: number): string {
  const v = n >= 10 ? String(Math.round(n)) : n.toFixed(1).replace(/\.0$/, "")
  return `${v} GB-h`
}

// GitHub's month is taken to be the calendar month in UTC, so its dates are
// written in UTC whatever the browser's zone.
export function utcDay(iso: string): string {
  return new Date(iso).toLocaleString("en-US", { month: "short", day: "numeric", timeZone: "UTC" })
}

function monthName(month: string): string {
  return new Date(`${month}-01T00:00:00Z`).toLocaleString("en-US", { month: "long", timeZone: "UTC" })
}

export function bannerText(alert: StorageAlert): string {
  const resets = utcDay(alert.resets_at)
  const runsOut = alert.runs_out_at ? utcDay(alert.runs_out_at) : undefined
  switch (alert.level) {
    case "exhausted":
      return `GitHub artifact storage for ${monthName(alert.month)} is used up: uploads fail until ${resets}.`
    case "critical":
    case "warn":
      return `GitHub artifact storage is at ${alert.used_pct}% of ${monthName(alert.month)}'s allowance${runsOut ? ` and runs out around ${runsOut}` : ""}.`
    case "projected":
      return `GitHub artifact storage runs out ${runsOut ? `around ${runsOut}, ` : ""}before the month resets on ${resets}.`
    default:
      return ""
  }
}

const DISMISSABLE: readonly QuotaLevel[] = ["projected", "warn"]

export function canDismiss(level: string): boolean {
  return (DISMISSABLE as readonly string[]).includes(level)
}

const dismissKey = (alert: StorageAlert) => `ghr-quota-dismissed:${alert.month}:${alert.level}`

// Local storage can be missing or refuse access; the banner then shows.
export function isDismissed(alert: StorageAlert): boolean {
  if (!canDismiss(alert.level)) return false
  try {
    return localStorage.getItem(dismissKey(alert)) === "1"
  } catch {
    return false
  }
}

export function dismiss(alert: StorageAlert): void {
  if (!canDismiss(alert.level)) return
  try {
    localStorage.setItem(dismissKey(alert), "1")
  } catch {
    // the banner comes back on the next render, which is the safe side
  }
}
```

- [ ] **Step 4: Run the test to see it pass**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/lib/quota.test.ts && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`
Expected: 14 tests pass; tsc and eslint print nothing.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/quota.ts web/src/lib/quota.test.ts
git commit -m "feat(web): add the quota text helpers"
```

### Task 18: Add the Artifacts page

**Files:**
- Create: `web/src/pages/artifacts.tsx`
- Modify: `web/src/router.tsx` (import, `artifactsRoute`, the route tree)
- Test: `web/src/pages/artifacts.test.tsx`

**Interfaces:**
- Consumes: Contracts → Web → Client and Hook (Tasks 15, 16).
- Produces: Contracts → Web → Page composition: `ArtifactsPage` at `/artifacts` and the column Tasks 20 to 24 add their cards to.

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/pages/artifacts.test.tsx`:

```tsx
import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

describe("Artifacts page", () => {
  it("is served at /artifacts and says when GitHub was last checked", async () => {
    mockApi(authedRoutes())
    renderApp("/artifacts")
    expect(await screen.findByRole("heading", { level: 1, name: "Artifacts" })).toBeInTheDocument()
    expect(await screen.findByText("Checked 14:00")).toBeInTheDocument()
  })

  it("says when nothing has been checked yet", async () => {
    mockApi(authedRoutes({ "GET /api/github-storage": { repos: [], deletions: [] } }))
    renderApp("/artifacts")
    expect(await screen.findByText("Not checked yet")).toBeInTheDocument()
  })

  it("shows a failed read with a retry", async () => {
    let fail = true
    mockApi(authedRoutes({ "GET /api/github-storage": () => (fail ? json({ error: "daemon busy" }, 500) : fixtures.githubStorage) }))
    const { user } = renderApp("/artifacts")
    const alert = await screen.findByRole("alert")
    expect(alert).toHaveTextContent("daemon busy")
    fail = false
    await user.click(screen.getByRole("button", { name: "Try again" }))
    expect(await screen.findByText("Checked 14:00")).toBeInTheDocument()
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("shows why the repositories could not be listed", async () => {
    mockApi(authedRoutes({ "GET /api/github-storage": { ...fixtures.githubStorage, error: "GitHub did not answer in time" } }))
    renderApp("/artifacts")
    expect(await screen.findByRole("alert")).toHaveTextContent("Could not list the repositories: GitHub did not answer in time")
  })

  it("starts a check, and shows the refusal while one runs", async () => {
    let busy = false
    const { calls } = mockApi(
      authedRoutes({
        "POST /api/github-storage/refresh": () => (busy ? json({ error: "a GitHub storage check is already running" }, 409) : noContent()),
      }),
    )
    const { user } = renderApp("/artifacts")
    await user.click(await screen.findByRole("button", { name: "Check now" }))
    expect((await screen.findAllByText("Check started")).length).toBeGreaterThan(0)
    busy = true
    await waitFor(() => expect(screen.getByRole("button", { name: "Check now" })).toBeEnabled())
    await user.click(screen.getByRole("button", { name: "Check now" }))
    expect((await screen.findAllByText("a GitHub storage check is already running")).length).toBeGreaterThan(0)
    expect(calls.filter((c) => c.method === "POST")).toHaveLength(2)
  })
})
```

- [ ] **Step 2: Run the test to see it fail**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/pages/artifacts.test.tsx`
Expected: FAIL, 5 tests, with `Unable to find role="heading" and name "Artifacts"` (no route serves `/artifacts` yet).

- [ ] **Step 3: Write the page and its route**

Create `web/src/pages/artifacts.tsx`:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { api } from "@/api/client"
import { keys, useGitHubStorage, useStatus } from "@/api/hooks"
import { ErrorLine } from "@/components/page/error-line"
import { shortWhen } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

export function ArtifactsPage() {
  const storage = useGitHubStorage()
  const status = useStatus()
  const queryClient = useQueryClient()
  const now = useNow()
  const refresh = useMutation({
    mutationFn: () => api.refreshGitHubStorage(),
    onSuccess: () => {
      toast.success("Check started")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.githubStorage }),
  })
  const data = storage.data
  const offline = status.isError
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Artifacts"
        description={data ? (data.checked_at ? `Checked ${shortWhen(data.checked_at, now)}` : "Not checked yet") : undefined}
        actions={
          <Button variant="outline" disabled={offline || refresh.isPending} onClick={() => refresh.mutate()}>
            Check now
          </Button>
        }
      />
      {storage.isError && <ErrorLine onRetry={() => void storage.refetch()}>{errorText(storage.error)}</ErrorLine>}
      {data?.error && <ErrorLine>{`Could not list the repositories: ${data.error}`}</ErrorLine>}
      {!data && !storage.isError && <Spinner label="Loading" />}
    </div>
  )
}
```

In `web/src/router.tsx`, replace this line:

```tsx
import { ActionsPage } from "./pages/actions"
```

with:

```tsx
import { ActionsPage } from "./pages/actions"
import { ArtifactsPage } from "./pages/artifacts"
```

Replace this line:

```tsx
const toolchainsRoute = createRoute({
```

with:

```tsx
const artifactsRoute = createRoute({ getParentRoute: () => appRoute, path: "/artifacts", component: ArtifactsPage })
const toolchainsRoute = createRoute({
```

Replace these lines in the route tree:

```tsx
    historyRoute,
    toolchainsRoute,
```

with:

```tsx
    historyRoute,
    artifactsRoute,
    toolchainsRoute,
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/pages/artifacts.test.tsx src/router.test.tsx && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`
Expected: 5 and 27 tests pass; tsc and eslint print nothing.

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/artifacts.tsx web/src/pages/artifacts.test.tsx web/src/router.tsx
git commit -m "feat(web): add the Artifacts page"
```

### Task 19: Show the quota banner and the Artifacts tab

**Files:**
- Create: `web/src/components/quota-banner.tsx`
- Modify: `web/src/components/shell.tsx` (icon import, banner import, the `Tab` union, `tabs`, the banner column)
- Test: `web/src/components/quota-banner.test.tsx`, `web/src/components/shell.test.tsx` (the tab list)

**Interfaces:**
- Consumes: Contracts → Web → Text helpers (Task 17), the `/artifacts` route (Task 18), `Status.github_storage` (Task 14).
- Produces: `QuotaBanner` (Contracts → Web → Components).

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/quota-banner.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { keys } from "@/api/hooks"
import type { StorageAlert } from "@/api/types"
import { mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const WARN = "GitHub artifact storage is at 72% of October's allowance and runs out around Oct 6."
const CRITICAL = "GitHub artifact storage is at 91% of October's allowance and runs out around Oct 6."
const EXHAUSTED = "GitHub artifact storage for October is used up: uploads fail until Nov 1."
const PROJECTED = "GitHub artifact storage runs out around Oct 6, before the month resets on Nov 1."

function routes(over: Partial<StorageAlert> | null) {
  const alert = over && { ...fixtures.storageAlert, ...over }
  return authedRoutes({ "GET /api/status": { ...fixtures.status, ...(alert ? { github_storage: alert } : {}) } })
}

beforeEach(() => localStorage.clear())
afterEach(() => vi.restoreAllMocks())

describe("quota banner", () => {
  it.each<[Partial<StorageAlert>, string, string, boolean]>([
    [{ level: "projected", used_pct: 40 }, PROJECTED, "border-warning", true],
    [{ level: "warn", used_pct: 72 }, WARN, "border-warning", true],
    [{ level: "critical" }, CRITICAL, "border-destructive", false],
    [{ level: "exhausted", used_pct: 103 }, EXHAUSTED, "border-destructive", false],
  ])("shows %j with its style, and a Dismiss button only for the early levels", async (over, title, border, dismissable) => {
    mockApi(routes(over))
    renderApp("/")
    const banner = await screen.findByRole("region", { name: title })
    expect(banner).toHaveClass(border)
    expect(within(banner).getByRole("link", { name: "Go to Artifacts" })).toHaveAttribute("href", "/artifacts")
    expect(within(banner).queryByRole("button", { name: "Dismiss" }) !== null).toBe(dismissable)
  })

  it.each([
    ["no quota", null],
    ["no level", { level: "" }],
  ])("is absent with %s", async (_why, over) => {
    mockApi(routes(over))
    const { queryClient } = renderApp("/")
    await waitFor(() => expect(queryClient.getQueryData(keys.status)).toBeDefined())
    expect(screen.queryByRole("region", { name: /^GitHub artifact storage/ })).toBeNull()
  })

  it("stays dismissed for that level and month, and comes back at the next level", async () => {
    mockApi(routes({ level: "warn", used_pct: 72 }))
    const { user, unmount } = renderApp("/")
    const banner = await screen.findByRole("region", { name: WARN })
    await user.click(within(banner).getByRole("button", { name: "Dismiss" }))
    await waitFor(() => expect(screen.queryByRole("region", { name: WARN })).toBeNull())
    unmount()

    mockApi(routes({ level: "warn", used_pct: 72 }))
    const again = renderApp("/")
    await waitFor(() => expect(again.queryClient.getQueryData(keys.status)).toBeDefined())
    expect(screen.queryByRole("region", { name: WARN })).toBeNull()
    again.unmount()

    mockApi(routes({ level: "critical" }))
    renderApp("/")
    expect(await screen.findByRole("region", { name: CRITICAL })).toBeInTheDocument()
  })

  it("can still be dismissed for this visit without local storage", async () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("denied")
    })
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new Error("denied")
    })
    mockApi(routes({ level: "warn", used_pct: 72 }))
    const { user } = renderApp("/")
    const banner = await screen.findByRole("region", { name: WARN })
    await user.click(within(banner).getByRole("button", { name: "Dismiss" }))
    await waitFor(() => expect(screen.queryByRole("region", { name: WARN })).toBeNull())
  })

  it("opens the Artifacts page", async () => {
    mockApi(routes({ level: "exhausted", used_pct: 103 }))
    const { user } = renderApp("/")
    const banner = await screen.findByRole("region", { name: EXHAUSTED })
    await user.click(within(banner).getByRole("link", { name: "Go to Artifacts" }))
    expect(await screen.findByRole("heading", { level: 1, name: "Artifacts" })).toBeInTheDocument()
  })
})
```

In `web/src/components/shell.test.tsx`, replace these lines in "lists the pages as tabs":

```tsx
      "History",
      "Repositories",
```

with:

```tsx
      "History",
      "Artifacts",
      "Repositories",
```

- [ ] **Step 2: Run the tests to see them fail**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/components/quota-banner.test.tsx src/components/shell.test.tsx`
Expected: FAIL, 8 tests: "lists the pages as tabs" and each banner test, with `Unable to find role="region" and name "GitHub artifact storage ..."`.

- [ ] **Step 3: Write the banner and add it and the tab to the shell**

Create `web/src/components/quota-banner.tsx`:

```tsx
import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { TriangleAlert } from "lucide-react"
import { useId, useState } from "react"
import type { StorageAlert } from "@/api/types"
import { bannerText, canDismiss, dismiss, isDismissed } from "@/lib/quota"

const BANNER = "flex flex-wrap items-center gap-x-3 gap-y-2 rounded-md border bg-card px-4 py-2.5 text-sm text-card-foreground"

export function QuotaBanner({ alert }: { alert: StorageAlert }) {
  const titleId = useId()
  const key = `${alert.month}:${alert.level}`
  // Kept in memory too, so Dismiss works for this visit without local storage.
  const [hidden, setHidden] = useState<string | null>(null)
  if (alert.level === "" || hidden === key || isDismissed(alert)) return null
  const early = canDismiss(alert.level)
  return (
    <section aria-labelledby={titleId} className={`${BANNER} ${early ? "border-warning" : "border-destructive"}`}>
      <h2 id={titleId} className={`flex items-center gap-1.5 font-semibold ${early ? "text-warning" : "text-destructive"}`}>
        <TriangleAlert size={15} aria-hidden="true" />
        {bannerText(alert)}
      </h2>
      <Link to="/artifacts" className="text-primary hover:underline">
        Go to Artifacts
      </Link>
      {early && (
        <Button
          size="sm"
          variant="ghost"
          className="ml-auto"
          onClick={() => {
            dismiss(alert)
            setHidden(key)
          }}
        >
          Dismiss
        </Button>
      )}
    </section>
  )
}
```

In `web/src/components/shell.tsx`, replace this line:

```tsx
import { Boxes, GitBranch, HistoryIcon, LayoutDashboard, LogOut, Settings, Workflow, type LucideIcon } from "lucide-react"
```

with:

```tsx
import { Archive, Boxes, GitBranch, HistoryIcon, LayoutDashboard, LogOut, Settings, Workflow, type LucideIcon } from "lucide-react"
```

Replace this line:

```tsx
import { ModeControl } from "@/components/mode-control"
```

with:

```tsx
import { ModeControl } from "@/components/mode-control"
import { QuotaBanner } from "@/components/quota-banner"
```

Replace this line:

```tsx
  to: "/" | "/runners" | "/actions" | "/history" | "/repositories" | "/settings"
```

with:

```tsx
  to: "/" | "/runners" | "/actions" | "/history" | "/artifacts" | "/repositories" | "/settings"
```

Replace this line:

```tsx
    { label: "History", to: "/history", icon: HistoryIcon },
```

with:

```tsx
    { label: "History", to: "/history", icon: HistoryIcon },
    { label: "Artifacts", to: "/artifacts", icon: Archive },
```

Replace this line:

```tsx
            {st.web_listen_error ? <ListenErrorBanner error={st.web_listen_error} /> : <RestartBanner status={st} offline={offline} />}
```

with:

```tsx
            {st.web_listen_error ? <ListenErrorBanner error={st.web_listen_error} /> : <RestartBanner status={st} offline={offline} />}
            {st.github_storage && <QuotaBanner alert={st.github_storage} />}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/components/quota-banner.test.tsx src/components/shell.test.tsx && TMPDIR=/root/.cache/ghr-plan-tmp timeout 600 npx vitest run src/a11y.test.tsx src/app.test.tsx src/router.test.tsx src/anti-slop.test.ts && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`
Expected: 28 tests pass, then 191 tests in 4 files pass (the tab row is on every page); tsc and eslint print nothing.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/quota-banner.tsx web/src/components/quota-banner.test.tsx web/src/components/shell.tsx web/src/components/shell.test.tsx
git commit -m "feat(web): show the quota banner and Artifacts tab"
```

### Task 20: Show the quota card

**Files:**
- Create: `web/src/components/quota-card.tsx`
- Modify: `web/src/pages/artifacts.tsx` (one import, one line)
- Test: `web/src/components/quota-card.test.tsx`

**Interfaces:**
- Consumes: Contracts → Web → Types and Fixtures (Task 14), Text helpers `gbh` and `utcDay` (Task 17), Page composition (Task 18).
- Produces: `QuotaCard({ storage })`, a `Quota` region.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

The meter is the used share of the allowance with two markers, the estimate and the projection, each capped at the bar's end. The test seeds the status query because `useNow` runs on the daemon's clock, which the fixture status sets to 2026-10-03.

- [ ] **Step 1: Write the failing test**

Create `web/src/components/quota-card.test.tsx`:

```tsx
import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { keys } from "@/api/hooks"
import type { GitHubQuota, GitHubStorage } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { QuotaCard } from "./quota-card"

function draw(quota: Partial<GitHubQuota> | null, over: Partial<GitHubStorage> = {}) {
  const base = fixtures.githubStorage
  const storage: GitHubStorage = { ...base, quota: quota && base.quota ? { ...base.quota, ...quota } : undefined, ...over }
  const { queryClient, wrapper } = withQuery()
  // useNow reads the daemon's clock from the status, here 2026-10-03.
  queryClient.setQueryData(keys.status, fixtures.status)
  render(<QuotaCard storage={storage} />, { wrapper })
  return screen.getByRole("region", { name: "Quota" })
}

describe("QuotaCard", () => {
  it("shows the billed use against the allowance, with the estimate and projection marked", () => {
    const card = draw({})
    expect(within(card).getByText("72%")).toBeInTheDocument()
    expect(within(card).getByText("261 GB-h of 363 GB-h used in October")).toBeInTheDocument()
    const meter = within(card).getByRole("img", { name: "Used 261 GB-h of 363 GB-h, estimated 263 GB-h, projected 397 GB-h by Nov 1" })
    expect(meter.querySelector("[data-part=used]")).toHaveStyle({ width: "71.7%" })
    expect(meter.querySelector("[data-part=used]")).toHaveClass("bg-warning")
    expect(meter.querySelector("[data-marker=estimate]")).toHaveStyle({ left: "72.4%" })
    expect(meter.querySelector("[data-marker=projected]")).toHaveStyle({ left: "100%" })
    expect(within(card).getByText("Actions 250 GB-h, Packages 10 GB-h")).toBeInTheDocument()
    expect(within(card).getByText("Runs out around Oct 24.")).toBeInTheDocument()
    expect(within(card).getByText("Billing read 13:45")).toBeInTheDocument()
    expect(within(card).getByText(/GitHub recalculates storage every 6 to 12 hours/)).toHaveTextContent(
      "GitHub recalculates storage every 6 to 12 hours, so the estimate adds 12 hours at the current rate. The month is taken to be the calendar month in UTC.",
    )
    expect(within(card).queryByRole("alert")).toBeNull()
  })

  it.each<[string, Partial<GitHubQuota>, string, string]>([
    ["exhausted", { level: "exhausted", used_gb_hours: 373.3, over_allowance_gb_hours: 0.0016, runs_out_at: undefined }, "Uploads fail until Nov 1 unless a budget is set.", "bg-destructive"],
    ["critical past the estimate", { level: "critical", estimated_gb_hours: 370, runs_out_at: undefined }, "Uploads may fail before GitHub's next recalculation.", "bg-destructive"],
    ["calm", { level: "", projected_gb_hours: 300, runs_out_at: undefined }, "Lasts until the month resets on Nov 1.", "bg-primary"],
  ])("words the outlook when %s", (_name, quota, outlook, tint) => {
    const card = draw(quota)
    expect(within(card).getByText(outlook)).toBeInTheDocument()
    expect(within(card).getByRole("img").querySelector("[data-part=used]")).toHaveClass(tint)
  })

  it.each<[number, string | null]>([
    [0.0016, "GitHub reports use past its free allowance: under 0.1 GB-h."],
    [9.5, "GitHub reports use past its free allowance: 9.5 GB-h."],
    [0, null],
  ])("shows GitHub's own figure past the free allowance (%s)", (over, text) => {
    const card = draw({ level: "exhausted", over_allowance_gb_hours: over })
    expect(within(card).queryByText(/past its free allowance/)?.textContent ?? null).toBe(text)
  })

  it("shows the billing error and keeps the last quota", () => {
    const error = "the token cannot read darkraise's billing: grant the account permission Plan: read"
    const card = draw({}, { quota_error: error })
    expect(within(card).getByRole("alert")).toHaveTextContent(error)
    expect(within(card).getByText("72%")).toBeInTheDocument()
  })

  it("says when there is no billing read yet", () => {
    const card = draw(null)
    expect(within(card).getByText("No billing read yet.")).toBeInTheDocument()
    expect(within(card).queryByRole("img")).toBeNull()
  })
})
```

- [ ] **Step 2: Run the test to see it fail**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/components/quota-card.test.tsx`
Expected: FAIL with `Failed to resolve import "./quota-card" from "src/components/quota-card.test.tsx"`.

- [ ] **Step 3: Write the card and put it on the page**

Create `web/src/components/quota-card.tsx`:

```tsx
import type { GitHubQuota, GitHubStorage } from "@/api/types"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import { shortWhen } from "@/lib/format"
import { gbh, utcDay } from "@/lib/quota"
import { useNow } from "@/lib/use-now"

const TINT: Record<string, string> = {
  exhausted: "bg-destructive",
  critical: "bg-destructive",
  warn: "bg-warning",
  projected: "bg-warning",
}

// A share of the allowance as a bar position, capped at the bar's end.
const at = (n: number, of: number) => `${Math.min(100, Math.round((n / of) * 1000) / 10)}%`

function outlook(q: GitHubQuota): string {
  if (q.level === "exhausted") return `Uploads fail until ${utcDay(q.resets_at)} unless a budget is set.`
  if (q.runs_out_at) return `Runs out around ${utcDay(q.runs_out_at)}.`
  if (q.estimated_gb_hours >= q.included_gb_hours) return "Uploads may fail before GitHub's next recalculation."
  return `Lasts until the month resets on ${utcDay(q.resets_at)}.`
}

function QuotaMeter({ q }: { q: GitHubQuota }) {
  const label = `Used ${gbh(q.used_gb_hours)} of ${gbh(q.included_gb_hours)}, estimated ${gbh(q.estimated_gb_hours)}, projected ${gbh(q.projected_gb_hours)} by ${utcDay(q.resets_at)}`
  return (
    <div role="img" aria-label={label} className="relative">
      <div className="flex h-3 overflow-hidden rounded-[3px] bg-muted">
        <span data-part="used" className={TINT[q.level] ?? "bg-primary"} style={{ width: at(q.used_gb_hours, q.included_gb_hours) }} />
      </div>
      <span data-marker="estimate" aria-hidden="true" className="absolute -top-1 h-5 w-0.5 bg-foreground" style={{ left: at(q.estimated_gb_hours, q.included_gb_hours) }} />
      <span data-marker="projected" aria-hidden="true" className="absolute -top-1 h-5 w-0.5 bg-warning" style={{ left: at(q.projected_gb_hours, q.included_gb_hours) }} />
    </div>
  )
}

function monthName(month: string): string {
  return new Date(`${month}-01T00:00:00Z`).toLocaleString("en-US", { month: "long", timeZone: "UTC" })
}

export function QuotaCard({ storage }: { storage: GitHubStorage }) {
  const now = useNow()
  const q = storage.quota
  const over = q?.over_allowance_gb_hours
  const aside = q && <span className="ml-auto font-mono text-sm text-muted-foreground">{`Billing read ${shortWhen(q.read_at, now)}`}</span>
  return (
    <Section id="quota" title="Quota" aside={aside}>
      <div className="flex flex-col gap-3">
        {storage.quota_error && <ErrorLine>{storage.quota_error}</ErrorLine>}
        {q ? (
          <>
            <p className="flex flex-wrap items-baseline gap-x-2">
              <span className="font-mono text-2xl font-semibold">{`${Math.round((q.used_gb_hours / q.included_gb_hours) * 100)}%`}</span>
              <span className="text-sm text-muted-foreground">{`${gbh(q.used_gb_hours)} of ${gbh(q.included_gb_hours)} used in ${monthName(q.month)}`}</span>
            </p>
            <QuotaMeter q={q} />
            <ul className="flex flex-col gap-0.5 text-sm">
              <li className="flex items-center gap-2">
                <span aria-hidden="true" className="h-3 w-0.5 bg-foreground" />
                {`Estimated now ${gbh(q.estimated_gb_hours)}`}
              </li>
              <li className="flex items-center gap-2">
                <span aria-hidden="true" className="h-3 w-0.5 bg-warning" />
                {`Projected by ${utcDay(q.resets_at)} ${gbh(q.projected_gb_hours)}`}
              </li>
            </ul>
            <p className="text-sm">{`Actions ${gbh(q.actions_gb_hours)}, Packages ${gbh(q.packages_gb_hours)}`}</p>
            {over !== undefined && over > 0.001 && (
              <p className="text-sm text-destructive">{`GitHub reports use past its free allowance: ${over < 0.1 ? "under 0.1 GB-h" : gbh(over)}.`}</p>
            )}
            <p className="text-sm font-semibold">{outlook(q)}</p>
          </>
        ) : (
          !storage.quota_error && <p className="text-sm text-muted-foreground">No billing read yet.</p>
        )}
        <p className="text-xs text-muted-foreground">
          GitHub recalculates storage every 6 to 12 hours, so the estimate adds 12 hours at the current rate. The month is taken to be the calendar month in UTC.
        </p>
      </div>
    </Section>
  )
}
```

In `web/src/pages/artifacts.tsx`, replace this line:

```tsx
import { ErrorLine } from "@/components/page/error-line"
```

with:

```tsx
import { ErrorLine } from "@/components/page/error-line"
import { QuotaCard } from "@/components/quota-card"
```

Replace this line:

```tsx
      {!data && !storage.isError && <Spinner label="Loading" />}
```

with:

```tsx
      {!data && !storage.isError && <Spinner label="Loading" />}
      {data && <QuotaCard storage={data} />}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/components/quota-card.test.tsx src/pages/artifacts.test.tsx`
Expected: PASS, 14 tests.

- [ ] **Step 5: Check types and lint**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit` and `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`
Expected: both print nothing.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/quota-card.tsx web/src/components/quota-card.test.tsx web/src/pages/artifacts.tsx
git commit -m "feat(web): show the GitHub storage quota card"
```

### Task 21: Edit the storage policy

**Files:**
- Create: `web/src/components/storage-policy-card.tsx`
- Modify: `web/src/pages/artifacts.tsx` (the hooks import, one import, two hook lines, one line)
- Test: `web/src/components/storage-policy-card.test.tsx`

**Interfaces:**
- Consumes: Contracts → Web → Types and Fixtures (Task 14), Client `storagePreview`, `patchConfig` and `setRetentionAll` (Task 15), Hook `useConfig`, `githubStorageBusy` and `keys` (Task 16), Text helpers `CLEANUP_WARNING` and `RETENTION_WARNING` (Task 17), Page composition (Task 18).
- Produces: `StoragePolicyCard({ storage, config, offline, busy })`, a `Policy` region; `CleanupPreview({ age, previews })`, which Task 22 reuses for a repository's cleanup.

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

Saving sends only the changed keys through `PATCH /api/config` as `github_storage`. A new non-empty cleanup age is previewed first (`GET /api/github-storage/preview?cleanup=<age>`), and the confirmation lists what would go, with `CLEANUP_WARNING`; an empty age turns cleanup off with no preview. A 4xx refusal shows under the card and toasts `Storage policy not saved`, as the settings form does. `CleanupPreview` is a component, not a function, because `react-refresh/only-export-components` refuses other exports from a component file.

- [ ] **Step 1: Write the failing test**

Create `web/src/components/storage-policy-card.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Config } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { renderRoutes } from "@/test/routes"
import { StoragePolicyCard } from "./storage-policy-card"

async function draw({ config = fixtures.config, busy = false }: { config?: Config; busy?: boolean } = {}) {
  const view = renderRoutes({ "/": () => <StoragePolicyCard storage={fixtures.githubStorage} config={config} offline={false} busy={busy} /> }, "/")
  await screen.findByRole("region", { name: "Policy" })
  return view
}

const field = (name: string) => screen.getByRole("spinbutton", { name }) as HTMLInputElement

describe("StoragePolicyCard", () => {
  it("shows the saved policy with the defaults filled in", async () => {
    mockApi({})
    await draw()
    expect(field("Included storage").value).toBe("500")
    expect(field("Warning at").value).toBe("70")
    expect(field("Critical at").value).toBe("90")
    expect((screen.getByRole("textbox", { name: "Cleanup age" }) as HTMLInputElement).value).toBe("2d")
    expect(screen.getByRole("button", { name: "Save policy" })).toBeDisabled()
  })

  it("previews a new cleanup age and saves it once confirmed", async () => {
    const { calls } = mockApi({
      "GET /api/github-storage/preview": [
        { repo: "darkcloud", count: 1, bytes: 20971520 },
        { repo: "darkmem", count: 2, bytes: 209715200 },
      ],
      "PATCH /api/config": () => noContent(),
      "GET /api/config": fixtures.config,
    })
    const { user } = await draw()
    const age = screen.getByRole("textbox", { name: "Cleanup age" })
    await user.clear(age)
    await user.type(age, "1d")
    await user.click(screen.getByRole("button", { name: "Save policy" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Clean artifacts after 1d?" })
    expect(dialog).toHaveTextContent("Cleanup does not protect an artifact that a different run downloads later")
    expect(dialog).toHaveTextContent("Up to 3 artifacts (230.7 MB) older than 1d would go at the next check.")
    expect(within(dialog).getByText("darkcloud: up to 1 artifact, 21.0 MB")).toBeInTheDocument()
    expect(within(dialog).getByText("darkmem: up to 2 artifacts, 209.7 MB")).toBeInTheDocument()
    expect(calls.find((c) => c.path === "/api/github-storage/preview")?.search).toBe("?cleanup=1d")
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
    await user.click(within(dialog).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ github_storage: { cleanup_max_age: "1d" } }))
    expect((await screen.findAllByText("Storage policy saved")).length).toBeGreaterThan(0)
  })

  it.each<[string, (u: Awaited<ReturnType<typeof draw>>["user"]) => Promise<void>, unknown]>([
    [
      "turns cleanup off without a preview",
      async (user) => user.clear(screen.getByRole("textbox", { name: "Cleanup age" })),
      { cleanup_max_age: "" },
    ],
    [
      "saves the allowance and warning levels",
      async (user) => {
        await user.clear(field("Included storage"))
        await user.type(field("Included storage"), "1024")
        await user.clear(field("Warning at"))
        await user.type(field("Warning at"), "60")
        await user.clear(field("Critical at"))
        await user.type(field("Critical at"), "85")
      },
      { included_mb: 1024, warn_pct: [60, 85] },
    ],
  ])("%s", async (_name, edit, patch) => {
    const { calls } = mockApi({ "PATCH /api/config": () => noContent(), "GET /api/config": fixtures.config })
    const { user } = await draw()
    await edit(user)
    await user.click(screen.getByRole("button", { name: "Save policy" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ github_storage: patch }))
    expect(calls.some((c) => c.path === "/api/github-storage/preview")).toBe(false)
  })

  it.each<[string, string, string]>([
    ["Included storage", "0", "Enter a whole number of MB, at least 1"],
    ["Warning at", "95", "Enter two whole percentages from 1 to 100, the first lower"],
  ])("refuses %s %s before sending", async (name, value, error) => {
    const { calls } = mockApi({})
    const { user } = await draw()
    await user.clear(field(name))
    await user.type(field(name), value)
    expect(await screen.findByText(error)).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save policy" }))
    expect(calls).toHaveLength(0)
  })

  it("shows the daemon's refusal", async () => {
    const msg = "github_storage.cleanup_max_age must be at least 24h"
    mockApi({ "GET /api/github-storage/preview": [], "PATCH /api/config": json({ error: msg }, 400) })
    const { user } = await draw()
    const age = screen.getByRole("textbox", { name: "Cleanup age" })
    await user.clear(age)
    await user.type(age, "12h")
    await user.click(screen.getByRole("button", { name: "Save policy" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Clean artifacts after 12h?" })
    expect(dialog).toHaveTextContent("No artifact is older than 12h now.")
    await user.click(within(dialog).getByRole("button", { name: "Save" }))
    expect(await screen.findByRole("alert")).toHaveTextContent(msg)
  })

  it("sets retention on every repository after a warning", async () => {
    const { calls } = mockApi({ "POST /api/github-storage/retention": () => json({ id: "op-2" }, 202) })
    const { user } = await draw()
    await user.type(field("Retention for all"), "7")
    await user.click(screen.getByRole("button", { name: "Set retention" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Set retention to 7 days on 3 repositories?" })
    expect(dialog).toHaveTextContent(
      "GitHub's retention also limits how long it keeps runs and logs, which ghr's Actions and log pages read. A repository whose maximum is lower is skipped.",
    )
    await user.click(within(dialog).getByRole("button", { name: "Set retention" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ days: 7 }))
    expect((await screen.findAllByText("Retention change started")).length).toBeGreaterThan(0)
  })

  it("holds Set retention while an operation runs", async () => {
    mockApi({})
    const { user } = await draw({ busy: true })
    await user.type(field("Retention for all"), "7")
    expect(screen.getByRole("button", { name: "Set retention" })).toBeDisabled()
  })
})
```

- [ ] **Step 2: Run the test to see it fail**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/components/storage-policy-card.test.tsx`
Expected: FAIL with `Failed to resolve import "./storage-policy-card" from "src/components/storage-policy-card.test.tsx"`.

- [ ] **Step 3: Write the card and put it on the page**

Create `web/src/components/storage-policy-card.tsx`:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { ApiError, api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { Config, GitHubStorage, GitHubStoragePatch, StoragePreview } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { ErrorLine } from "@/components/page/error-line"
import { Field, FieldGroup } from "@/components/page/field"
import { FIELD_WIDTH } from "@/lib/field-width"
import { humanBytes, plural } from "@/lib/format"
import { CLEANUP_WARNING, RETENTION_WARNING } from "@/lib/quota"
import { errorText } from "@/query"

// What a cleanup age would delete now, for the confirmation it gets before it
// is saved, globally or for one repository.
export function CleanupPreview({ age, previews }: { age: string; previews: StoragePreview[] }) {
  const count = previews.reduce((n, p) => n + p.count, 0)
  const bytes = previews.reduce((n, p) => n + p.bytes, 0)
  if (count === 0) return <p className="text-sm">{`No artifact is older than ${age} now.`}</p>
  return (
    <div className="flex flex-col gap-1 text-sm">
      <p>{`Up to ${plural(count, "artifact")} (${humanBytes(bytes)}) older than ${age} would go at the next check.`}</p>
      <ul className="flex flex-col gap-0.5">
        {previews.map((p) => (
          <li key={p.repo}>{`${p.repo}: up to ${plural(p.count, "artifact")}, ${humanBytes(p.bytes)}`}</li>
        ))}
      </ul>
    </div>
  )
}

const repositories = (n: number) => (n === 1 ? "1 repository" : `${n} repositories`)

interface Draft {
  included: string
  warn: string
  critical: string
  age: string
}

const whole = (s: string) => (/^\d+$/.test(s.trim()) ? Number(s) : Number.NaN)

function saved(config: Config | undefined): Draft {
  const g = config?.github_storage
  const [warn, critical] = g?.warn_pct ?? [70, 90]
  return { included: String(g?.included_mb ?? 500), warn: String(warn), critical: String(critical), age: g?.cleanup_max_age ?? "" }
}

function problems(d: Draft): Partial<Record<keyof Draft, string>> {
  const out: Partial<Record<keyof Draft, string>> = {}
  if (!(whole(d.included) >= 1)) out.included = "Enter a whole number of MB, at least 1"
  const w = whole(d.warn)
  const c = whole(d.critical)
  if (!(w >= 1 && c <= 100 && w < c)) out.critical = "Enter two whole percentages from 1 to 100, the first lower"
  return out
}

export function StoragePolicyCard({
  storage,
  config,
  offline,
  busy,
}: {
  storage: GitHubStorage
  config: Config | undefined
  offline: boolean
  busy: boolean
}) {
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Partial<Draft>>({})
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const [rejection, setRejection] = useState("")
  const [saving, setSaving] = useState(false)
  const [days, setDays] = useState("")
  const loaded = saved(config)
  const values: Draft = { ...loaded, ...draft }
  const changed = (Object.keys(values) as (keyof Draft)[]).filter((k) => values[k].trim() !== loaded[k])
  const errors = problems(values)
  const set = (key: keyof Draft) => (e: { target: { value: string } }) => setDraft((d) => ({ ...d, [key]: e.target.value }))

  const retention = useMutation({
    mutationFn: (n: number) => api.setRetentionAll(n),
    onSuccess: () => {
      toast.success("Retention change started")
      setDays("")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.githubStorage }),
  })

  async function send(patch: GitHubStoragePatch) {
    setSaving(true)
    setRejection("")
    try {
      await api.patchConfig({ github_storage: patch })
    } catch (err) {
      setSaving(false)
      if (err instanceof ApiError && err.status >= 400 && err.status < 500) {
        setRejection(err.message)
        toast.error("Storage policy not saved")
      } else {
        toast.error(`Storage policy not saved: ${errorText(err)}`)
      }
      return
    }
    setSaving(false)
    setDraft({})
    toast.success("Storage policy saved")
    void queryClient.invalidateQueries({ queryKey: keys.config })
    void queryClient.invalidateQueries({ queryKey: keys.githubStorage })
  }

  async function save() {
    if (changed.length === 0 || saving || Object.keys(errors).length > 0) return
    const patch: GitHubStoragePatch = {}
    if (changed.includes("included")) patch.included_mb = whole(values.included)
    if (changed.includes("warn") || changed.includes("critical")) patch.warn_pct = [whole(values.warn), whole(values.critical)]
    const age = values.age.trim()
    if (!changed.includes("age")) return send(patch)
    patch.cleanup_max_age = age
    if (age === "") return send(patch)
    let previews: StoragePreview[]
    try {
      previews = await api.storagePreview(age)
    } catch (err) {
      setRejection(errorText(err))
      toast.error("Storage policy not saved")
      return
    }
    setConfirm({
      title: `Clean artifacts after ${age}?`,
      body: CLEANUP_WARNING,
      details: <CleanupPreview age={age} previews={previews} />,
      action: "Save",
      destructive: previews.some((p) => p.count > 0),
      run: () => void send(patch),
    })
  }

  const n = whole(days)
  const daysOK = n >= 1 && n <= 400
  const count = storage.repos.length

  return (
    <FieldGroup id="storage-policy" title="Policy" note="Saved in config.yaml as github_storage.">
      {rejection && (
        <div className="py-3">
          <ErrorLine>{rejection}</ErrorLine>
        </div>
      )}
      <Field label="Included storage" htmlFor="included_mb" help="The plan's allowance for artifacts and Packages, in MB." error={errors.included} changed={changed.includes("included")}>
        <Input id="included_mb" type="number" min={1} suffix="MB" groupClassName={FIELD_WIDTH} className="min-w-0" value={values.included} disabled={offline} onChange={set("included")} />
      </Field>
      <Field label="Warning levels" help="Warn, then warn louder, when the estimate reaches these shares of the allowance." error={errors.critical} changed={changed.includes("warn") || changed.includes("critical")}>
        <Input aria-label="Warning at" type="number" min={1} max={100} suffix="%" groupClassName="w-28" className="min-w-0" value={values.warn} disabled={offline} onChange={set("warn")} />
        <Input aria-label="Critical at" type="number" min={1} max={100} suffix="%" groupClassName="w-28" className="min-w-0" value={values.critical} disabled={offline} onChange={set("critical")} />
      </Field>
      <Field label="Cleanup age" htmlFor="cleanup_max_age" help={`Delete artifacts older than this, such as 2d; empty is off, and at least 24h. ${CLEANUP_WARNING}`} changed={changed.includes("age")}>
        <Input id="cleanup_max_age" className={`${FIELD_WIDTH} font-mono`} placeholder="off" value={values.age} disabled={offline} onChange={set("age")} />
      </Field>
      <div className="flex justify-end gap-2 py-3">
        {changed.length > 0 && (
          <Button variant="ghost" disabled={saving} onClick={() => setDraft({})}>
            Discard
          </Button>
        )}
        <Button disabled={offline || saving || changed.length === 0} onClick={() => void save()}>
          Save policy
        </Button>
      </div>
      <Field label="Retention for all" htmlFor="retention_all" help={`Sets GitHub's retention in days on every repository listed below. ${RETENTION_WARNING}`} error={days !== "" && !daysOK ? "Enter a whole number of days from 1 to 400" : undefined}>
        <Input id="retention_all" type="number" min={1} max={400} suffix="days" groupClassName="w-36" className="min-w-0" value={days} disabled={offline} onChange={(e) => setDays(e.target.value)} />
        <Button
          variant="outline"
          disabled={offline || busy || !daysOK || count === 0 || retention.isPending}
          onClick={() =>
            setConfirm({
              title: `Set retention to ${n} days on ${repositories(count)}?`,
              body: `${RETENTION_WARNING} A repository whose maximum is lower is skipped.`,
              action: "Set retention",
              run: () => retention.mutate(n),
            })
          }
        >
          Set retention
        </Button>
      </Field>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </FieldGroup>
  )
}
```

In `web/src/pages/artifacts.tsx`, replace this line:

```tsx
import { keys, useGitHubStorage, useStatus } from "@/api/hooks"
```

with:

```tsx
import { githubStorageBusy, keys, useConfig, useGitHubStorage, useStatus } from "@/api/hooks"
```

Replace this line:

```tsx
import { QuotaCard } from "@/components/quota-card"
```

with:

```tsx
import { QuotaCard } from "@/components/quota-card"
import { StoragePolicyCard } from "@/components/storage-policy-card"
```

Replace this line:

```tsx
  const status = useStatus()
```

with:

```tsx
  const status = useStatus()
  const config = useConfig()
```

Replace this line:

```tsx
  const offline = status.isError
```

with:

```tsx
  const offline = status.isError
  const busy = githubStorageBusy(data)
```

Replace this line:

```tsx
      {data && <QuotaCard storage={data} />}
```

with:

```tsx
      {data && <QuotaCard storage={data} />}
      {data && <StoragePolicyCard storage={data} config={config.data} offline={offline} busy={busy} />}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/components/storage-policy-card.test.tsx src/pages/artifacts.test.tsx`
Expected: PASS, 14 tests.

- [ ] **Step 5: Check types and lint**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit` and `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`
Expected: both print nothing.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/storage-policy-card.tsx web/src/components/storage-policy-card.test.tsx web/src/pages/artifacts.tsx
git commit -m "feat(web): edit the GitHub storage policy"
```

### Task 22: List the repositories

**Files:**
- Create: `web/src/components/artifact-repo-table.tsx`
- Modify: `web/src/pages/artifacts.tsx` (one import, one line)
- Test: `web/src/components/artifact-repo-table.test.tsx`

**Interfaces:**
- Consumes: Contracts → Web → Types and Fixtures (Task 14), Client `setRetention`, `storagePreview` and `patchConfig` (Task 15), Hook `keys` (Task 16), Text helpers (Task 17), Page composition (Task 18), `CleanupPreview` (Task 21).
- Produces: `ArtifactRepoTable({ storage, config, offline })`, a `Repositories` region with one row per repository, whose `<th scope="row">` cell Task 23 turns into the row's expand button.

**Items:** 3, 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

Retention is edited in place: 1 to the repository's maximum (400 while unknown), confirmed with `RETENTION_WARNING`, then `PUT /api/github-storage/<repo>/retention`. Cleanup is edited with a native select (global, age, off): an age, or the global age a repository inherits, is previewed with `?repo=` first like the global age; `off`, or inheriting while the global age is off, saves with no preview. The override goes through `PATCH /api/config` as `github_storage.repos`, `null` removing it. A row at 90 days while the quota level is not empty shows the hint to lower it.

- [ ] **Step 1: Write the failing test**

Create `web/src/components/artifact-repo-table.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { GitHubStorage } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { renderRoutes } from "@/test/routes"
import { ArtifactRepoTable } from "./artifact-repo-table"

async function draw(storage: GitHubStorage = fixtures.githubStorage) {
  const view = renderRoutes({ "/": () => <ArtifactRepoTable storage={storage} config={fixtures.config} offline={false} /> }, "/")
  await screen.findByRole("region", { name: "Repositories" })
  return view
}

const row = (name: string) => screen.getByRole("row", { name: new RegExp(`^${name}\\b`) })

describe("ArtifactRepoTable", () => {
  it("lists each repository's artifacts, caches, retention and cleanup", async () => {
    mockApi({})
    await draw()
    const rows = screen.getAllByRole("row").slice(1)
    expect(rows.map((r) => within(r).getByRole("rowheader").textContent)).toEqual([
      expect.stringMatching(/^darkmem/),
      expect.stringMatching(/^darkcloud/),
      expect.stringMatching(/^sandbox/),
    ])
    const darkmem = row("darkmem")
    for (const text of ["209.7 MB", "2 artifacts", "71.4 MB", "1 cache", "90 days", "6h (override)"]) {
      expect(within(darkmem).getByText(text)).toBeInTheDocument()
    }
    expect(within(row("darkcloud")).getByText("2d (global)")).toBeInTheDocument()
    const sandbox = row("sandbox")
    expect(within(sandbox).getByText("public")).toBeInTheDocument()
    expect(within(sandbox).getByText("Unknown")).toBeInTheDocument()
    expect(within(sandbox).getByText("token lacks actions=read on darkraise/sandbox: grant it in the PAT settings")).toBeInTheDocument()
  })

  it.each<[string, string, boolean]>([
    ["warn", "darkmem", true],
    ["warn", "darkcloud", false],
    ["", "darkmem", false],
  ])("at level %j, hints %s to lower its 90-day retention: %s", async (level, name, hinted) => {
    mockApi({})
    const quota = fixtures.githubStorage.quota && { ...fixtures.githubStorage.quota, level }
    await draw({ ...fixtures.githubStorage, quota })
    expect(within(row(name)).queryByText("At GitHub's 90-day default: lowering it saves storage.") !== null).toBe(hinted)
  })

  it("sets one repository's retention after a warning", async () => {
    const { calls } = mockApi({ "PUT /api/github-storage/darkcloud/retention": () => noContent() })
    const { user } = await draw()
    await user.click(screen.getByRole("button", { name: "Edit retention of darkcloud" }))
    const input = screen.getByRole("spinbutton", { name: "Retention of darkcloud" })
    await user.clear(input)
    await user.type(input, "120")
    expect(screen.getByText("Enter a whole number of days from 1 to 90")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Save retention of darkcloud" })).toBeDisabled()
    await user.clear(input)
    await user.type(input, "3")
    await user.click(screen.getByRole("button", { name: "Save retention of darkcloud" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Set retention of darkcloud to 3 days?" })
    expect(dialog).toHaveTextContent("GitHub's retention also limits how long it keeps runs and logs, which ghr's Actions and log pages read.")
    await user.click(within(dialog).getByRole("button", { name: "Set retention" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PUT")?.body).toEqual({ days: 3 }))
    expect((await screen.findAllByText("Retention of darkcloud set to 3 days")).length).toBeGreaterThan(0)
  })

  it.each<[string, string, string, string | null, string | null]>([
    ["darkcloud", "age", "12h", "?cleanup=12h&repo=darkcloud", "12h"],
    ["darkmem", "inherit", "", "?cleanup=2d&repo=darkmem", null],
  ])("previews %s's cleanup as %s before saving it", async (name, choice, age, search, sent) => {
    const { calls } = mockApi({
      "GET /api/github-storage/preview": [{ repo: name, count: 1, bytes: 20971520 }],
      "PATCH /api/config": () => noContent(),
    })
    const { user } = await draw()
    await user.click(screen.getByRole("button", { name: `Edit cleanup of ${name}` }))
    await user.selectOptions(screen.getByRole("combobox", { name: `Cleanup of ${name}` }), choice)
    if (age) await user.type(screen.getByRole("textbox", { name: `Cleanup age of ${name}` }), age)
    await user.click(screen.getByRole("button", { name: `Save cleanup of ${name}` }))
    const effective = age || "2d"
    const dialog = await screen.findByRole("alertdialog", { name: `Clean ${name}'s artifacts after ${effective}?` })
    expect(dialog).toHaveTextContent(`Up to 1 artifact (21.0 MB) older than ${effective} would go at the next check.`)
    expect(calls.find((c) => c.path === "/api/github-storage/preview")?.search).toBe(search)
    await user.click(within(dialog).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ github_storage: { repos: { [name]: sent } } }))
    expect((await screen.findAllByText(`Cleanup of ${name} saved`)).length).toBeGreaterThan(0)
  })

  it("turns one repository's cleanup off without a preview", async () => {
    const { calls } = mockApi({ "PATCH /api/config": () => noContent() })
    const { user } = await draw()
    await user.click(screen.getByRole("button", { name: "Edit cleanup of darkcloud" }))
    await user.selectOptions(screen.getByRole("combobox", { name: "Cleanup of darkcloud" }), "off")
    await user.click(screen.getByRole("button", { name: "Save cleanup of darkcloud" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ github_storage: { repos: { darkcloud: "off" } } }))
    expect(calls.some((c) => c.path === "/api/github-storage/preview")).toBe(false)
  })

  it("shows a refused preview and saves nothing", async () => {
    const { calls } = mockApi({ "GET /api/github-storage/preview": json({ error: "invalid duration \"1x\"" }, 400) })
    const { user } = await draw()
    await user.click(screen.getByRole("button", { name: "Edit cleanup of darkcloud" }))
    await user.selectOptions(screen.getByRole("combobox", { name: "Cleanup of darkcloud" }), "age")
    await user.type(screen.getByRole("textbox", { name: "Cleanup age of darkcloud" }), "1x")
    await user.click(screen.getByRole("button", { name: "Save cleanup of darkcloud" }))
    expect((await screen.findAllByText("invalid duration \"1x\"")).length).toBeGreaterThan(0)
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
  })
})
```

- [ ] **Step 2: Run the test to see it fail**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/components/artifact-repo-table.test.tsx`
Expected: FAIL with `Failed to resolve import "./artifact-repo-table" from "src/components/artifact-repo-table.test.tsx"`.

- [ ] **Step 3: Write the table and put it on the page**

Create `web/src/components/artifact-repo-table.tsx`:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { toast } from "darkraise-ui/components/sonner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Pencil } from "lucide-react"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { Config, GitHubStorage, RepoStorage } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { Section } from "@/components/page/section"
import { CleanupPreview } from "@/components/storage-policy-card"
import { humanBytes, plural } from "@/lib/format"
import { CLEANUP_WARNING, RETENTION_WARNING } from "@/lib/quota"
import { errorText } from "@/query"

const NUM = "text-right font-mono whitespace-nowrap"
const WIDE = "hidden sm:table-cell"

type Ask = (c: Confirm) => void

function Sized({ bytes, count, word }: { bytes: number; count: number; word: string }) {
  return (
    <>
      <div>{humanBytes(bytes)}</div>
      <div className="text-xs text-muted-foreground">{plural(count, word)}</div>
    </>
  )
}

function RetentionCell({ repo, offline, ask }: { repo: RepoStorage; offline: boolean; ask: Ask }) {
  const queryClient = useQueryClient()
  const [days, setDays] = useState<string | null>(null)
  const max = repo.retention_max_days || 400
  const set = useMutation({
    mutationFn: (n: number) => api.setRetention(repo.name, n),
    onSuccess: (_d, n) => {
      toast.success(`Retention of ${repo.name} set to ${n} days`)
      setDays(null)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.githubStorage }),
  })
  if (days === null) {
    return (
      <span className="flex items-center justify-end gap-1">
        {repo.retention_days > 0 ? `${repo.retention_days} days` : "Unknown"}
        <Button size="icon" variant="ghost" aria-label={`Edit retention of ${repo.name}`} disabled={offline} onClick={() => setDays(String(repo.retention_days || ""))}>
          <Pencil size={14} aria-hidden="true" />
        </Button>
      </span>
    )
  }
  const n = /^\d+$/.test(days) ? Number(days) : Number.NaN
  const ok = n >= 1 && n <= max
  return (
    <div className="flex flex-col items-end gap-1">
      <div className="flex items-center gap-1">
        <Input aria-label={`Retention of ${repo.name}`} type="number" min={1} max={max} suffix="days" groupClassName="w-32" className="min-w-0" value={days} onChange={(e) => setDays(e.target.value)} />
        <Button
          size="sm"
          aria-label={`Save retention of ${repo.name}`}
          disabled={offline || !ok || set.isPending}
          onClick={() =>
            ask({
              title: `Set retention of ${repo.name} to ${n} days?`,
              body: RETENTION_WARNING,
              action: "Set retention",
              run: () => set.mutate(n),
            })
          }
        >
          Save
        </Button>
        <Button size="sm" variant="ghost" onClick={() => setDays(null)}>
          Cancel
        </Button>
      </div>
      {days !== "" && !ok && <p className="text-xs text-destructive">{`Enter a whole number of days from 1 to ${max}`}</p>}
    </div>
  )
}

type Choice = "inherit" | "off" | "age"

function cleanupText(repo: RepoStorage): string {
  return repo.cleanup_source === "none" ? "off" : `${repo.cleanup} (${repo.cleanup_source})`
}

function CleanupCell({ repo, globalAge, offline, ask }: { repo: RepoStorage; globalAge: string; offline: boolean; ask: Ask }) {
  const queryClient = useQueryClient()
  const [choice, setChoice] = useState<Choice | null>(null)
  const [age, setAge] = useState("")
  const save = useMutation({
    mutationFn: (value: string | null) => api.patchConfig({ github_storage: { repos: { [repo.name]: value } } }),
    onSuccess: () => {
      toast.success(`Cleanup of ${repo.name} saved`)
      setChoice(null)
    },
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: keys.config })
      void queryClient.invalidateQueries({ queryKey: keys.githubStorage })
    },
  })

  async function submit(c: Choice) {
    const value = c === "inherit" ? null : c === "off" ? "off" : age.trim()
    const effective = c === "inherit" ? globalAge : c === "age" ? age.trim() : ""
    if (effective === "") return save.mutate(value)
    try {
      const previews = await api.storagePreview(effective, repo.name)
      ask({
        title: `Clean ${repo.name}'s artifacts after ${effective}?`,
        body: CLEANUP_WARNING,
        details: <CleanupPreview age={effective} previews={previews} />,
        action: "Save",
        destructive: previews.some((p) => p.count > 0),
        run: () => save.mutate(value),
      })
    } catch (err) {
      toast.error(errorText(err))
    }
  }

  if (choice === null) {
    return (
      <span className="flex items-center gap-1">
        {cleanupText(repo)}
        <Button
          size="icon"
          variant="ghost"
          aria-label={`Edit cleanup of ${repo.name}`}
          disabled={offline}
          onClick={() => {
            setChoice(repo.cleanup_source === "override" ? (repo.cleanup === "off" ? "off" : "age") : "inherit")
            setAge(repo.cleanup_source === "override" && repo.cleanup !== "off" ? repo.cleanup : "")
          }}
        >
          <Pencil size={14} aria-hidden="true" />
        </Button>
      </span>
    )
  }
  return (
    <div className="flex flex-wrap items-center gap-1">
      <select
        aria-label={`Cleanup of ${repo.name}`}
        className="h-8 rounded-md border border-input bg-background px-2 text-sm"
        value={choice}
        onChange={(e) => setChoice(e.target.value as Choice)}
      >
        <option value="inherit">{`Global (${globalAge || "off"})`}</option>
        <option value="age">Age</option>
        <option value="off">Off</option>
      </select>
      {choice === "age" && (
        <Input aria-label={`Cleanup age of ${repo.name}`} className="w-20 font-mono" placeholder="6h" value={age} onChange={(e) => setAge(e.target.value)} />
      )}
      <Button size="sm" aria-label={`Save cleanup of ${repo.name}`} disabled={offline || save.isPending || (choice === "age" && age.trim() === "")} onClick={() => void submit(choice)}>
        Save
      </Button>
      <Button size="sm" variant="ghost" onClick={() => setChoice(null)}>
        Cancel
      </Button>
    </div>
  )
}

export function ArtifactRepoTable({ storage, config, offline }: { storage: GitHubStorage; config: Config | undefined; offline: boolean }) {
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const globalAge = config?.github_storage?.cleanup_max_age ?? ""
  const warned = (storage.quota?.level ?? "") !== ""
  return (
    <Section id="repositories" title="Repositories" flush>
      {storage.repos.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">No repositories listed yet.</p>
      ) : (
        <div className="overflow-x-auto [&_.dr-table-wrapper]:border-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">Repository</TableHead>
                <TableHead className="text-right">Artifacts</TableHead>
                <TableHead className={`text-right ${WIDE}`}>Caches</TableHead>
                <TableHead className="text-right">Retention</TableHead>
                <TableHead className="pr-4">Cleanup</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {storage.repos.map((r) => (
                <TableRow key={r.name}>
                  <th scope="row" className="py-2 pl-4 text-left align-top font-normal">
                    <span className="flex items-center gap-2">
                      {r.name}{" "}
                      {r.visibility === "public" && <span className="text-xs text-muted-foreground">public</span>}
                    </span>
                    {warned && r.retention_days === 90 && (
                      <p className="text-xs text-warning">At GitHub's 90-day default: lowering it saves storage.</p>
                    )}
                    {r.error && <p className="text-xs whitespace-normal text-destructive">{r.error}</p>}
                  </th>
                  <TableCell className={`align-top ${NUM}`}>
                    <Sized bytes={r.artifact_bytes} count={r.artifact_count} word="artifact" />
                  </TableCell>
                  <TableCell className={`align-top ${NUM} ${WIDE}`}>
                    <Sized bytes={r.cache_bytes} count={r.cache_count} word="cache" />
                  </TableCell>
                  <TableCell className="text-right align-top">
                    <RetentionCell repo={r} offline={offline} ask={setConfirm} />
                  </TableCell>
                  <TableCell className="pr-4 align-top">
                    <CleanupCell repo={r} globalAge={globalAge} offline={offline} ask={setConfirm} />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </Section>
  )
}
```

In `web/src/pages/artifacts.tsx`, replace this line:

```tsx
import { ErrorLine } from "@/components/page/error-line"
```

with:

```tsx
import { ArtifactRepoTable } from "@/components/artifact-repo-table"
import { ErrorLine } from "@/components/page/error-line"
```

Replace this line:

```tsx
      {data && <StoragePolicyCard storage={data} config={config.data} offline={offline} busy={busy} />}
```

with:

```tsx
      {data && <StoragePolicyCard storage={data} config={config.data} offline={offline} busy={busy} />}
      {data && <ArtifactRepoTable storage={data} config={config.data} offline={offline} />}
```

- [ ] **Step 4: Run the tests to see them pass**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/components/artifact-repo-table.test.tsx src/pages/artifacts.test.tsx`
Expected: PASS, 14 tests.

- [ ] **Step 5: Check types and lint**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit` and `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`
Expected: both print nothing.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/artifact-repo-table.tsx web/src/components/artifact-repo-table.test.tsx web/src/pages/artifacts.tsx
git commit -m "feat(web): list repositories' GitHub storage"
```

### Task 23: Manage a repository's artifacts and caches

**Files:**
- Create: `web/src/components/artifact-repo-detail.tsx`
- Modify: `web/src/components/artifact-repo-table.tsx` (imports, the expand state, the rows)
- Test: `web/src/components/artifact-repo-detail.test.tsx`

**Interfaces:**
- Consumes: Contracts → Web → Types and Fixtures (Task 14), Client `deleteArtifact`, `deleteArtifacts`, `deleteCache`, `deleteCaches` and `storagePreview` (Task 15), Hook `githubStorageBusy` and `keys` (Task 16), `ArtifactRepoTable` (Task 22), the run route `/actions/$repo/runs/$id` (`web/src/router.tsx`).
- Produces: `ArtifactRepoDetail({ repo, owner, busy, offline, ask })`, rendered in a repository's expanded row; its regions are `Artifacts of <repo>` and `Caches of <repo>`.

**Items:** 4

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

The repository's name in its row becomes the expand button (`aria-expanded`); the expanded row spans the table and holds the detail. Every delete asks first through the table's one `ConfirmDialog`. Delete selected sends the chosen ids; Delete older previews the age for that repository and sends `older_than`, or toasts that nothing is older; Delete all caches sends `all: true`. The three bulk actions are disabled while an operation runs (`githubStorageBusy`); single deletes stay available. An artifact's run links to ghr's run page when the repository is covered, otherwise to `https://github.com/<owner>/<repo>/actions/runs/<id>`, with the owner from the config. Checkboxes are native inputs. Risk 2: the page sends the ids the owner ticked, so a wrong id list deletes the wrong artifacts; the daemon checks each id against its snapshot (Task 9).

- [ ] **Step 1: Write the failing test**

Create `web/src/components/artifact-repo-detail.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { useState } from "react"
import { describe, expect, it } from "vitest"
import type { RepoStorage } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { json, mockApi, noContent } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { renderRoutes } from "@/test/routes"
import { ArtifactRepoDetail } from "./artifact-repo-detail"
import { ArtifactRepoTable } from "./artifact-repo-table"

const darkmem = fixtures.githubStorage.repos[0] as RepoStorage

function Detail({ repo, busy }: { repo: RepoStorage; busy: boolean }) {
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  return (
    <>
      <ArtifactRepoDetail repo={repo} owner="darkraise" busy={busy} offline={false} ask={setConfirm} />
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}

async function draw(repo: RepoStorage = darkmem, busy = false) {
  const view = renderRoutes({ "/": () => <Detail repo={repo} busy={busy} />, "/actions/$repo/runs/$id": () => <p>run page</p> }, "/")
  await screen.findByRole("region", { name: `Artifacts of ${repo.name}` })
  return view
}

const artifacts = () => screen.getByRole("region", { name: "Artifacts of darkmem" })
const caches = () => screen.getByRole("region", { name: "Caches of darkmem" })
const row = (name: RegExp) => within(artifacts()).getByRole("row", { name })

describe("ArtifactRepoDetail", () => {
  it.each<[boolean, string]>([
    [true, "/actions/darkmem/runs/102"],
    [false, "https://github.com/darkraise/darkmem/actions/runs/102"],
  ])("links an artifact's run to ghr when covered is %s", async (covered, href) => {
    mockApi({})
    await draw({ ...darkmem, covered })
    expect(within(row(/frontend-dist/)).getByRole("link", { name: "Run 102" })).toHaveAttribute("href", href)
    expect(row(/frontend-dist/)).toHaveTextContent("157.3 MB")
    expect(row(/frontend-dist/)).toHaveTextContent("master")
  })

  it("deletes the selected artifacts after asking", async () => {
    const { calls } = mockApi({ "POST /api/github-storage/darkmem/artifacts/delete": () => json({ id: "op-2" }, 202) })
    const { user } = await draw()
    const bulk = within(artifacts()).getByRole("button", { name: "Delete selected" })
    expect(bulk).toBeDisabled()
    await user.click(within(artifacts()).getByRole("checkbox", { name: "Select every artifact of darkmem" }))
    expect(within(artifacts()).getByRole("checkbox", { name: "Select coverage" })).toBeChecked()
    await user.click(within(artifacts()).getByRole("checkbox", { name: "Select coverage" }))
    await user.click(bulk)
    const dialog = await screen.findByRole("alertdialog", { name: "Delete 1 artifact from darkmem?" })
    expect(dialog).toHaveTextContent("Frees 157.3 MB. A run that downloads them later fails.")
    await user.click(within(dialog).getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ ids: [9001] }))
    expect((await screen.findAllByText("Delete started")).length).toBeGreaterThan(0)
    expect(within(artifacts()).getByRole("checkbox", { name: "Select frontend-dist" })).not.toBeChecked()
  })

  it("deletes artifacts older than an age, counted from the preview", async () => {
    const { calls } = mockApi({
      "GET /api/github-storage/preview": [{ repo: "darkmem", count: 2, bytes: 209715200 }],
      "POST /api/github-storage/darkmem/artifacts/delete": () => json({ id: "op-2" }, 202),
    })
    const { user } = await draw()
    await user.type(within(artifacts()).getByRole("textbox", { name: "Delete artifacts of darkmem older than" }), "1d")
    await user.click(within(artifacts()).getByRole("button", { name: "Delete older" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Delete darkmem's artifacts older than 1d?" })
    expect(dialog).toHaveTextContent("Up to 2 artifacts (209.7 MB). Skips the artifacts of runs that have not completed. A run that downloads them later fails.")
    expect(calls.find((c) => c.path === "/api/github-storage/preview")?.search).toBe("?cleanup=1d&repo=darkmem")
    await user.click(within(dialog).getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ older_than: "1d" }))
  })

  it("says when nothing is older than the age", async () => {
    const { calls } = mockApi({ "GET /api/github-storage/preview": [] })
    const { user } = await draw()
    await user.type(within(artifacts()).getByRole("textbox", { name: "Delete artifacts of darkmem older than" }), "30d")
    await user.click(within(artifacts()).getByRole("button", { name: "Delete older" }))
    expect((await screen.findAllByText("No artifact of darkmem is older than 30d")).length).toBeGreaterThan(0)
    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(calls.some((c) => c.method === "POST")).toBe(false)
  })

  it.each<[string, () => HTMLElement, string, string, string]>([
    ["artifact", artifacts, "Delete frontend-dist", "Delete frontend-dist?", "DELETE /api/github-storage/darkmem/artifacts/9001"],
    ["cache", caches, "Delete cache node-cache-Linux-x64-npm-a4cc85d3", "Delete this cache?", "DELETE /api/github-storage/darkmem/caches/8787967584"],
  ])("deletes one %s after asking", async (_kind, region, button, title, route) => {
    const { calls } = mockApi({ [route]: () => noContent() })
    const { user } = await draw()
    await user.click(within(region()).getByRole("button", { name: button }))
    await user.click(within(await screen.findByRole("alertdialog", { name: title })).getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(calls.map((c) => `${c.method} ${c.path}`)).toContain(route))
  })

  it("deletes every cache after asking", async () => {
    const { calls } = mockApi({ "POST /api/github-storage/darkmem/caches/delete": () => json({ id: "op-3" }, 202) })
    const { user } = await draw()
    expect(within(caches()).getByRole("row", { name: /^node-cache-Linux-x64-npm-a4cc85d3/ })).toHaveTextContent("refs/heads/master")
    await user.click(within(caches()).getByRole("button", { name: "Delete all caches" }))
    const dialog = await screen.findByRole("alertdialog", { name: "Delete 1 cache of darkmem?" })
    expect(dialog).toHaveTextContent("Frees 71.4 MB. Jobs rebuild what they need.")
    await user.click(within(dialog).getByRole("button", { name: "Delete" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ all: true }))
  })

  it("holds the bulk deletes while an operation runs", async () => {
    mockApi({})
    const { user } = await draw(darkmem, true)
    await user.click(within(artifacts()).getByRole("checkbox", { name: "Select coverage" }))
    await user.type(within(artifacts()).getByRole("textbox", { name: "Delete artifacts of darkmem older than" }), "1d")
    for (const name of ["Delete selected", "Delete older"]) expect(within(artifacts()).getByRole("button", { name })).toBeDisabled()
    expect(within(caches()).getByRole("button", { name: "Delete all caches" })).toBeDisabled()
    expect(within(artifacts()).getByRole("button", { name: "Delete frontend-dist" })).toBeEnabled()
  })

  it("says when a repository has nothing stored", async () => {
    mockApi({})
    await draw({ ...darkmem, name: "empty", artifacts: [], caches: [], artifact_count: 0, cache_count: 0 })
    expect(within(screen.getByRole("region", { name: "Artifacts of empty" })).getByText("No artifacts.")).toBeInTheDocument()
    expect(within(screen.getByRole("region", { name: "Caches of empty" })).getByText("No caches.")).toBeInTheDocument()
  })
})

describe("ArtifactRepoTable rows", () => {
  it("expand to show the repository's artifacts and caches", async () => {
    mockApi({})
    const { user } = renderRoutes(
      { "/": () => <ArtifactRepoTable storage={fixtures.githubStorage} config={fixtures.config} offline={false} /> },
      "/",
    )
    const toggle = await screen.findByRole("button", { name: "darkcloud" })
    expect(toggle).toHaveAttribute("aria-expanded", "false")
    expect(screen.queryByRole("region", { name: "Artifacts of darkcloud" })).toBeNull()
    await user.click(toggle)
    expect(toggle).toHaveAttribute("aria-expanded", "true")
    expect(within(screen.getByRole("region", { name: "Artifacts of darkcloud" })).getByRole("link", { name: "Run 501" })).toHaveAttribute(
      "href",
      "/actions/darkcloud/runs/501",
    )
    await user.click(toggle)
    expect(screen.queryByRole("region", { name: "Artifacts of darkcloud" })).toBeNull()
  })
})
```

- [ ] **Step 2: Run the test to see it fail**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/components/artifact-repo-detail.test.tsx`
Expected: FAIL with `Failed to resolve import "./artifact-repo-detail" from "src/components/artifact-repo-detail.test.tsx"`.

- [ ] **Step 3: Write the detail**

Create `web/src/components/artifact-repo-detail.tsx`:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { toast } from "darkraise-ui/components/sonner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Trash2 } from "lucide-react"
import { useState, type ReactNode } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { ArtifactItem, RepoStorage } from "@/api/types"
import type { Confirm } from "@/components/confirm-dialog"
import { ago, dateTime, humanBytes, plural } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

const NUM = "text-right font-mono whitespace-nowrap"
const WIDE = "hidden sm:table-cell"

function Part({ title, label, actions, children }: { title: string; label: string; actions: ReactNode; children: ReactNode }) {
  return (
    <section aria-label={label} className="flex min-w-0 flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <h3 className="text-sm font-semibold">{title}</h3>
        <div className="ml-auto flex flex-wrap items-center gap-2">{actions}</div>
      </div>
      {children}
    </section>
  )
}

function RunLink({ repo, owner, artifact }: { repo: RepoStorage; owner: string; artifact: ArtifactItem }) {
  if (artifact.run_id === 0) return <>Unknown</>
  const text = `Run ${artifact.run_id}`
  if (repo.covered) {
    return (
      <Link to="/actions/$repo/runs/$id" params={{ repo: repo.name, id: String(artifact.run_id) }} className="hover:text-primary hover:underline">
        {text}
      </Link>
    )
  }
  return (
    <a href={`https://github.com/${owner}/${repo.name}/actions/runs/${artifact.run_id}`} target="_blank" rel="noreferrer" className="hover:text-primary hover:underline">
      {text}
    </a>
  )
}

export function ArtifactRepoDetail({
  repo,
  owner,
  busy,
  offline,
  ask,
}: {
  repo: RepoStorage
  owner: string
  busy: boolean
  offline: boolean
  ask: (c: Confirm) => void
}) {
  const queryClient = useQueryClient()
  const now = useNow()
  const [selected, setSelected] = useState<number[]>([])
  const [older, setOlder] = useState("")
  const settled = () => queryClient.invalidateQueries({ queryKey: keys.githubStorage })
  const bulk = useMutation({
    mutationFn: (body: { ids: number[] } | { older_than: string }) => api.deleteArtifacts(repo.name, body),
    onSuccess: () => {
      toast.success("Delete started")
      setSelected([])
      setOlder("")
    },
    onSettled: settled,
  })
  const one = useMutation({
    mutationFn: (a: ArtifactItem) => api.deleteArtifact(repo.name, a.id),
    onSuccess: (_d, a) => {
      toast.success(`Deleted ${a.name}`)
      setSelected((s) => s.filter((id) => id !== a.id))
    },
    onSettled: settled,
  })
  const cache = useMutation({
    mutationFn: (id: number) => api.deleteCache(repo.name, id),
    onSuccess: () => {
      toast.success("Cache deleted")
    },
    onSettled: settled,
  })
  const allCaches = useMutation({
    mutationFn: () => api.deleteCaches(repo.name, { all: true }),
    onSuccess: () => {
      toast.success("Delete started")
    },
    onSettled: settled,
  })

  const held = offline || busy
  const chosen = repo.artifacts.filter((a) => selected.includes(a.id))
  const all = repo.artifacts.length > 0 && chosen.length === repo.artifacts.length
  const toggle = (id: number) => setSelected((s) => (s.includes(id) ? s.filter((x) => x !== id) : [...s, id]))

  async function askOlder() {
    const age = older.trim()
    try {
      const previews = await api.storagePreview(age, repo.name)
      const count = previews.reduce((n, p) => n + p.count, 0)
      const bytes = previews.reduce((n, p) => n + p.bytes, 0)
      if (count === 0) {
        toast(`No artifact of ${repo.name} is older than ${age}`)
        return
      }
      ask({
        title: `Delete ${repo.name}'s artifacts older than ${age}?`,
        body: `Up to ${plural(count, "artifact")} (${humanBytes(bytes)}). Skips the artifacts of runs that have not completed. A run that downloads them later fails.`,
        action: "Delete",
        destructive: true,
        run: () => bulk.mutate({ older_than: age }),
      })
    } catch (err) {
      toast.error(errorText(err))
    }
  }

  const cacheBytes = repo.caches.reduce((n, c) => n + c.bytes, 0)

  return (
    <div className="flex flex-col gap-4 px-4 py-3">
      <Part
        title="Artifacts"
        label={`Artifacts of ${repo.name}`}
        actions={
          <>
            <Button
              size="sm"
              variant="outline"
              disabled={held || chosen.length === 0 || bulk.isPending}
              onClick={() =>
                ask({
                  title: `Delete ${plural(chosen.length, "artifact")} from ${repo.name}?`,
                  body: `Frees ${humanBytes(chosen.reduce((n, a) => n + a.bytes, 0))}. A run that downloads them later fails.`,
                  action: "Delete",
                  destructive: true,
                  run: () => bulk.mutate({ ids: chosen.map((a) => a.id) }),
                })
              }
            >
              Delete selected
            </Button>
            <Input
              aria-label={`Delete artifacts of ${repo.name} older than`}
              className="w-20 font-mono"
              placeholder="7d"
              value={older}
              disabled={offline}
              onChange={(e) => setOlder(e.target.value)}
            />
            <Button size="sm" variant="outline" disabled={held || older.trim() === "" || bulk.isPending} onClick={() => void askOlder()}>
              Delete older
            </Button>
          </>
        }
      >
        {repo.artifacts.length === 0 ? (
          <p className="text-sm text-muted-foreground">No artifacts.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-8">
                  <input
                    type="checkbox"
                    aria-label={`Select every artifact of ${repo.name}`}
                    checked={all}
                    onChange={() => setSelected(all ? [] : repo.artifacts.map((a) => a.id))}
                  />
                </TableHead>
                <TableHead>Artifact</TableHead>
                <TableHead className="text-right">Size</TableHead>
                <TableHead className={WIDE}>Created</TableHead>
                <TableHead className={WIDE}>Branch</TableHead>
                <TableHead>Run</TableHead>
                <TableHead>
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {repo.artifacts.map((a) => (
                <TableRow key={a.id}>
                  <TableCell>
                    <input type="checkbox" aria-label={`Select ${a.name}`} checked={selected.includes(a.id)} onChange={() => toggle(a.id)} />
                  </TableCell>
                  <th scope="row" className="py-2 text-left font-normal">
                    {a.name}
                  </th>
                  <TableCell className={NUM}>{humanBytes(a.bytes)}</TableCell>
                  <TableCell className={`whitespace-nowrap text-muted-foreground ${WIDE}`}>
                    <time dateTime={a.created_at} title={dateTime(a.created_at)}>
                      {ago(now - Date.parse(a.created_at))}
                    </time>
                  </TableCell>
                  <TableCell className={`font-mono text-xs ${WIDE}`}>{a.branch}</TableCell>
                  <TableCell className="whitespace-nowrap">
                    <RunLink repo={repo} owner={owner} artifact={a} />
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      size="icon"
                      variant="ghost"
                      aria-label={`Delete ${a.name}`}
                      disabled={offline || one.isPending}
                      onClick={() =>
                        ask({
                          title: `Delete ${a.name}?`,
                          body: `Frees ${humanBytes(a.bytes)}. A run that downloads it later fails.`,
                          action: "Delete",
                          destructive: true,
                          run: () => one.mutate(a),
                        })
                      }
                    >
                      <Trash2 size={15} aria-hidden="true" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Part>
      <Part
        title="Caches"
        label={`Caches of ${repo.name}`}
        actions={
          <Button
            size="sm"
            variant="outline"
            disabled={held || repo.caches.length === 0 || allCaches.isPending}
            onClick={() =>
              ask({
                title: `Delete ${plural(repo.caches.length, "cache")} of ${repo.name}?`,
                body: `Frees ${humanBytes(cacheBytes)}. Jobs rebuild what they need.`,
                action: "Delete",
                destructive: true,
                run: () => allCaches.mutate(),
              })
            }
          >
            Delete all caches
          </Button>
        }
      >
        {repo.caches.length === 0 ? (
          <p className="text-sm text-muted-foreground">No caches.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Key</TableHead>
                <TableHead className={WIDE}>Ref</TableHead>
                <TableHead className="text-right">Size</TableHead>
                <TableHead className={WIDE}>Last used</TableHead>
                <TableHead>
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {repo.caches.map((c) => (
                <TableRow key={c.id}>
                  <th scope="row" className="py-2 text-left font-mono text-xs font-normal break-all whitespace-normal">
                    {c.key}
                  </th>
                  <TableCell className={`font-mono text-xs ${WIDE}`}>{c.ref}</TableCell>
                  <TableCell className={NUM}>{humanBytes(c.bytes)}</TableCell>
                  <TableCell className={`whitespace-nowrap text-muted-foreground ${WIDE}`}>
                    <time dateTime={c.last_used_at} title={dateTime(c.last_used_at)}>
                      {ago(now - Date.parse(c.last_used_at))}
                    </time>
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      size="icon"
                      variant="ghost"
                      aria-label={`Delete cache ${c.key}`}
                      disabled={offline || cache.isPending}
                      onClick={() =>
                        ask({
                          title: "Delete this cache?",
                          body: `${c.key} on ${c.ref}, ${humanBytes(c.bytes)}. The next job that needs it rebuilds it.`,
                          action: "Delete",
                          destructive: true,
                          run: () => cache.mutate(c.id),
                        })
                      }
                    >
                      <Trash2 size={15} aria-hidden="true" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Part>
    </div>
  )
}
```

- [ ] **Step 4: Expand the table's rows**

In `web/src/components/artifact-repo-table.tsx`, replace these lines:

```tsx
import { Pencil } from "lucide-react"
import { useState } from "react"
```

with:

```tsx
import { ChevronRight, Pencil } from "lucide-react"
import { Fragment, useState } from "react"
```

Replace this line:

```tsx
import { keys } from "@/api/hooks"
```

with:

```tsx
import { githubStorageBusy, keys } from "@/api/hooks"
```

Replace this line:

```tsx
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
```

with:

```tsx
import { ArtifactRepoDetail } from "@/components/artifact-repo-detail"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
```

Replace these lines:

```tsx
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const globalAge = config?.github_storage?.cleanup_max_age ?? ""
```

with:

```tsx
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const [open, setOpen] = useState<string[]>([])
  const busy = githubStorageBusy(storage)
  const globalAge = config?.github_storage?.cleanup_max_age ?? ""
```

Replace these lines:

```tsx
              {storage.repos.map((r) => (
                <TableRow key={r.name}>
                  <th scope="row" className="py-2 pl-4 text-left align-top font-normal">
                    <span className="flex items-center gap-2">
                      {r.name}{" "}
                      {r.visibility === "public" && <span className="text-xs text-muted-foreground">public</span>}
                    </span>
                    {warned && r.retention_days === 90 && (
                      <p className="text-xs text-warning">At GitHub's 90-day default: lowering it saves storage.</p>
                    )}
                    {r.error && <p className="text-xs whitespace-normal text-destructive">{r.error}</p>}
                  </th>
                  <TableCell className={`align-top ${NUM}`}>
                    <Sized bytes={r.artifact_bytes} count={r.artifact_count} word="artifact" />
                  </TableCell>
                  <TableCell className={`align-top ${NUM} ${WIDE}`}>
                    <Sized bytes={r.cache_bytes} count={r.cache_count} word="cache" />
                  </TableCell>
                  <TableCell className="text-right align-top">
                    <RetentionCell repo={r} offline={offline} ask={setConfirm} />
                  </TableCell>
                  <TableCell className="pr-4 align-top">
                    <CleanupCell repo={r} globalAge={globalAge} offline={offline} ask={setConfirm} />
                  </TableCell>
                </TableRow>
              ))}
```

with:

```tsx
              {storage.repos.map((r) => {
                const expanded = open.includes(r.name)
                return (
                  <Fragment key={r.name}>
                    <TableRow>
                      <th scope="row" className="py-2 pl-4 text-left align-top font-normal">
                        <span className="flex items-center gap-2">
                          <button
                            type="button"
                            aria-expanded={expanded}
                            className="flex items-center gap-1 hover:text-primary"
                            onClick={() => setOpen((o) => (expanded ? o.filter((n) => n !== r.name) : [...o, r.name]))}
                          >
                            <ChevronRight size={14} aria-hidden="true" className={expanded ? "rotate-90" : ""} />
                            {r.name}
                          </button>{" "}
                          {r.visibility === "public" && <span className="text-xs text-muted-foreground">public</span>}
                        </span>
                        {warned && r.retention_days === 90 && (
                          <p className="text-xs text-warning">At GitHub's 90-day default: lowering it saves storage.</p>
                        )}
                        {r.error && <p className="text-xs whitespace-normal text-destructive">{r.error}</p>}
                      </th>
                      <TableCell className={`align-top ${NUM}`}>
                        <Sized bytes={r.artifact_bytes} count={r.artifact_count} word="artifact" />
                      </TableCell>
                      <TableCell className={`align-top ${NUM} ${WIDE}`}>
                        <Sized bytes={r.cache_bytes} count={r.cache_count} word="cache" />
                      </TableCell>
                      <TableCell className="text-right align-top">
                        <RetentionCell repo={r} offline={offline} ask={setConfirm} />
                      </TableCell>
                      <TableCell className="pr-4 align-top">
                        <CleanupCell repo={r} globalAge={globalAge} offline={offline} ask={setConfirm} />
                      </TableCell>
                    </TableRow>
                    {expanded && (
                      <TableRow>
                        <TableCell colSpan={5} className="bg-background p-0 whitespace-normal">
                          <ArtifactRepoDetail repo={r} owner={config?.owner ?? ""} busy={busy} offline={offline} ask={setConfirm} />
                        </TableCell>
                      </TableRow>
                    )}
                  </Fragment>
                )
              })}
```

- [ ] **Step 5: Run the tests to see them pass**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/components/artifact-repo-detail.test.tsx src/components/artifact-repo-table.test.tsx`
Expected: PASS, 20 tests.

- [ ] **Step 6: Check types and lint**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit` and `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`
Expected: both print nothing.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/artifact-repo-detail.tsx web/src/components/artifact-repo-detail.test.tsx web/src/components/artifact-repo-table.tsx
git commit -m "feat(web): manage a repository's artifacts and caches"
```

### Task 24: Show operation progress and deletions

**Files:**
- Create: `web/src/components/storage-operations.tsx`
- Modify: `web/src/pages/artifacts.tsx` (one import, two lines)
- Test: `web/src/components/storage-operations.test.tsx`
- Test: `web/src/a11y.test.tsx` (an Artifacts page block)

**Interfaces:**
- Consumes: Contracts → Web → Types and Fixtures (Task 14), Page composition (Task 18), the cards of Tasks 20 to 23 (the accessibility test renders them all).
- Produces: `OperationProgress({ operation })` (an `Operation` region, nothing before any operation) and `RecentDeletions({ deletions })` (a `Recent deletions` region).

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 0 = 3

The operation shows above the repositories table: a progress bar while it runs, then its counts, bytes freed, failures and finish time. The accessibility test renders the whole page with a row expanded, which also proves every card is on the page.

- [ ] **Step 1: Write the failing test**

Create `web/src/components/storage-operations.test.tsx`:

```tsx
import { render, screen, within } from "@testing-library/react"
import type { ReactNode } from "react"
import { describe, expect, it } from "vitest"
import { keys } from "@/api/hooks"
import type { Deletion, GitHubOperation } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { OperationProgress, RecentDeletions } from "./storage-operations"

const finished = fixtures.githubStorage.operation as GitHubOperation

function draw(ui: ReactNode) {
  const { queryClient, wrapper } = withQuery()
  // useNow reads the daemon's clock from the status, here 2026-10-03.
  queryClient.setQueryData(keys.status, fixtures.status)
  return render(ui, { wrapper })
}

describe("OperationProgress", () => {
  it("shows a running operation's progress", () => {
    const op: GitHubOperation = { ...finished, kind: "delete-caches", done: 1, total: 4, bytes: 71431401, failed: [], finished_at: undefined }
    draw(<OperationProgress operation={op} />)
    const region = screen.getByRole("region", { name: "Operation" })
    expect(within(region).getByText("Deleting caches in darkmem")).toBeInTheDocument()
    expect(within(region).getByText("1 of 4, 71.4 MB freed")).toBeInTheDocument()
    const bar = within(region).getByRole("progressbar", { name: "Deleting caches in darkmem" })
    expect(bar).toHaveAttribute("aria-valuenow", "1")
    expect(bar).toHaveAttribute("aria-valuemax", "4")
  })

  it("shows a finished operation with its failures", () => {
    draw(<OperationProgress operation={finished} />)
    const region = screen.getByRole("region", { name: "Operation" })
    expect(within(region).getByText("Deleted artifacts in darkmem")).toBeInTheDocument()
    expect(within(region).getByText("3 of 3, 52.4 MB freed, 1 failed, finished 13:56")).toBeInTheDocument()
    expect(within(region).getByText("darkmem 9003: not in the last inventory")).toBeInTheDocument()
    expect(within(region).queryByRole("progressbar")).toBeNull()
  })

  it("names a retention change across repositories", () => {
    const op: GitHubOperation = {
      ...finished,
      kind: "set-retention",
      repo: undefined,
      bytes: 0,
      failed: [{ repo: "darkcloud", error: "its maximum is 90 days" }],
    }
    draw(<OperationProgress operation={op} />)
    expect(screen.getByText("Set retention on every repository")).toBeInTheDocument()
    expect(screen.getByText("3 of 3, 1 failed, finished 13:56")).toBeInTheDocument()
    expect(screen.getByText("darkcloud: its maximum is 90 days")).toBeInTheDocument()
  })

  it("shows nothing before any operation", () => {
    const { container } = draw(<OperationProgress operation={undefined} />)
    expect(container).toBeEmptyDOMElement()
  })
})

describe("RecentDeletions", () => {
  it("lists what was deleted, newest first, and by whom", () => {
    draw(<RecentDeletions deletions={fixtures.githubStorage.deletions} />)
    const region = screen.getByRole("region", { name: "Recent deletions" })
    const rows = within(region).getAllByRole("row").slice(1)
    expect(rows.map((r) => [...r.querySelectorAll("td, th")].map((c) => c.textContent))).toEqual([
      ["13:56", "darkmem", "artifact", "coverage-old", "52.4 MB", "3d", "Cleanup"],
      ["12:05", "darkcloud", "cache", "node-cache-Linux-x64-npm-0b1c", "31.5 MB", "5d", "Manual"],
    ])
  })

  it("says when nothing was deleted", () => {
    const none: Deletion[] = []
    draw(<RecentDeletions deletions={none} />)
    expect(within(screen.getByRole("region", { name: "Recent deletions" })).getByText("Nothing deleted since ghr started.")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run the test to see it fail**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/components/storage-operations.test.tsx`
Expected: FAIL with `Failed to resolve import "./storage-operations" from "src/components/storage-operations.test.tsx"`.

- [ ] **Step 3: Write the two components and put them on the page**

Create `web/src/components/storage-operations.tsx`:

```tsx
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import type { Deletion, GitHubOperation } from "@/api/types"
import { Section } from "@/components/page/section"
import { dateTime, humanBytes, shortWhen } from "@/lib/format"
import { useNow } from "@/lib/use-now"

const VERBS: Record<string, [running: string, finished: string]> = {
  "delete-artifacts": ["Deleting artifacts", "Deleted artifacts"],
  "delete-caches": ["Deleting caches", "Deleted caches"],
  "set-retention": ["Setting retention", "Set retention"],
}

export function OperationProgress({ operation }: { operation: GitHubOperation | undefined }) {
  const now = useNow()
  if (!operation) return null
  const running = operation.finished_at === undefined
  const [doing, done] = VERBS[operation.kind] ?? [operation.kind, operation.kind]
  const title = `${running ? doing : done}${operation.repo ? ` in ${operation.repo}` : " on every repository"}`
  const parts = [`${operation.done} of ${operation.total}`]
  if (operation.bytes > 0) parts.push(`${humanBytes(operation.bytes)} freed`)
  if (operation.failed.length > 0) parts.push(`${operation.failed.length} failed`)
  if (operation.finished_at) parts.push(`finished ${shortWhen(operation.finished_at, now)}`)
  return (
    <section aria-label="Operation" className="flex flex-col gap-2 rounded-md border border-border bg-card p-4">
      <p className="text-sm font-semibold">{title}</p>
      {running && (
        <div
          role="progressbar"
          aria-label={title}
          aria-valuemin={0}
          aria-valuemax={operation.total}
          aria-valuenow={operation.done}
          className="h-2 overflow-hidden rounded-[3px] bg-muted"
        >
          <span className="block h-full bg-primary" style={{ width: `${operation.total > 0 ? (operation.done / operation.total) * 100 : 0}%` }} />
        </div>
      )}
      <p className="text-sm text-muted-foreground">{parts.join(", ")}</p>
      {operation.failed.length > 0 && (
        <ul className="flex flex-col gap-0.5 text-sm text-destructive">
          {operation.failed.map((f) => (
            <li key={`${f.repo}-${f.id ?? ""}`}>{`${f.repo}${f.id ? ` ${f.id}` : ""}: ${f.error}`}</li>
          ))}
        </ul>
      )}
    </section>
  )
}

export function RecentDeletions({ deletions }: { deletions: Deletion[] }) {
  const now = useNow()
  return (
    <Section id="deletions" title="Recent deletions" flush>
      {deletions.length === 0 ? (
        <p className="p-4 text-sm text-muted-foreground">Nothing deleted since ghr started.</p>
      ) : (
        <div className="overflow-x-auto [&_.dr-table-wrapper]:border-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">Time</TableHead>
                <TableHead>Repository</TableHead>
                <TableHead>Kind</TableHead>
                <TableHead>Name</TableHead>
                <TableHead className="text-right">Size</TableHead>
                <TableHead className="text-right">Age</TableHead>
                <TableHead className="pr-4">By</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {deletions.map((d, i) => (
                <TableRow key={`${d.at}-${d.repo}-${d.name}-${i}`}>
                  <TableCell className="pl-4 whitespace-nowrap text-muted-foreground">
                    <time dateTime={d.at} title={dateTime(d.at)}>
                      {shortWhen(d.at, now)}
                    </time>
                  </TableCell>
                  <TableCell>{d.repo}</TableCell>
                  <TableCell>{d.kind}</TableCell>
                  <TableCell className="font-mono text-xs break-all whitespace-normal">{d.name}</TableCell>
                  <TableCell className="text-right font-mono whitespace-nowrap">{humanBytes(d.bytes)}</TableCell>
                  <TableCell className="text-right font-mono">{d.age}</TableCell>
                  <TableCell className="pr-4">{d.by === "cleanup" ? "Cleanup" : "Manual"}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </Section>
  )
}
```

In `web/src/pages/artifacts.tsx`, replace this line:

```tsx
import { StoragePolicyCard } from "@/components/storage-policy-card"
```

with:

```tsx
import { OperationProgress, RecentDeletions } from "@/components/storage-operations"
import { StoragePolicyCard } from "@/components/storage-policy-card"
```

Replace this line:

```tsx
      {data && <ArtifactRepoTable storage={data} config={config.data} offline={offline} />}
```

with:

```tsx
      {data && <OperationProgress operation={data.operation} />}
      {data && <ArtifactRepoTable storage={data} config={config.data} offline={offline} />}
      {data && <RecentDeletions deletions={data.deletions} />}
```

- [ ] **Step 4: Check the whole page's accessibility**

In `web/src/a11y.test.tsx`, replace this line:

```tsx
describe("Settings accessibility", () => {
```

with:

```tsx
describe("Artifacts accessibility", () => {
  it(
    "has no axe violations with every card shown",
    async () => {
      mockApi(authedRoutes())
      const { user } = renderApp("/artifacts")
      for (const name of ["Quota", "Policy", "Operation", "Repositories", "Recent deletions"]) {
        await screen.findByRole("region", { name })
      }
      await user.click(screen.getByRole("button", { name: "darkmem" }))
      await screen.findByRole("region", { name: "Artifacts of darkmem" })
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})

describe("Settings accessibility", () => {
```

- [ ] **Step 5: Run the tests to see them pass**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/components/storage-operations.test.tsx src/pages/artifacts.test.tsx`, then `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx vitest run src/a11y.test.tsx -t Artifacts`
Expected: PASS, 11 tests; then 1 passed and the other accessibility tests skipped.

- [ ] **Step 6: Check types and lint**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit` and `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`
Expected: both print nothing.

- [ ] **Step 7: Run the whole web suite once**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 900 npx vitest run`
Expected: every test passes (104 files, 1,413 tests on 2026-10-13).

- [ ] **Step 8: Commit**

```bash
git add web/src/components/storage-operations.tsx web/src/components/storage-operations.test.tsx web/src/pages/artifacts.tsx web/src/a11y.test.tsx
git commit -m "feat(web): show storage operations and deletions"
```

### Task 25: Document the storage settings

**Files:**
- Modify: `README.md` (Requirements' PAT bullet, the Downgrading note, a new section before "Migrating from the compose runners")
- Modify: `deploy/config.example.yaml` (a commented `github_storage` block after `web:`)

**Interfaces:**
- Consumes: the config keys and limits in Contracts → Config (Task 3); the permissions in Contracts → GitHub client (Task 1).
- Produces: nothing other tasks read.

**Items:** 1, 2, 4

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 0 = 1

- [ ] **Step 1: Check the current text is there**

Run: `grep -c "Actions: read\*\*, and an expiry" README.md; grep -c "^\*\*Downgrading\.\*\*" README.md; grep -c "^## Migrating from the compose runners" README.md; grep -c "github_storage" README.md deploy/config.example.yaml`
Expected: `1`, `1`, `1`, then `README.md:0` and `deploy/config.example.yaml:0`.

- [ ] **Step 2: Name the new token permissions**

In `README.md`, replace these lines:

```markdown
- A fine-grained PAT scoped to exactly those repos, with **Administration: read/write** and
  **Actions: read**, and an expiry. **Contents: read** is optional: with it, a run's page in
  the web UI draws the job graph from the workflow file; without it, a jobs timeline.
```

with:

```markdown
- A fine-grained PAT scoped to exactly those repos, with **Administration: read/write** and
  **Actions: read/write**, and an expiry. **Contents: read** is optional: with it, a run's page in
  the web UI draws the job graph from the workflow file; without it, a jobs timeline.
  The Artifacts page also covers every other repository of the owner the PAT can reach: it
  deletes artifacts and caches with **Actions: write** and changes retention with
  **Administration: write**. It reads the month's storage use with the account permission
  **Plan: read**; for an organization owner, the token must be an organization admin's, with
  **Administration: read** on the organization. The quota can only be read when the token
  belongs to the owner. Without these permissions ghr runs jobs as before, and the Artifacts
  page says which permission is missing.
```

- [ ] **Step 3: Extend the downgrade note**

In `README.md`, replace this line:

```markdown
**Downgrading.** A config that lists `watch_repos` (the repositories the web UI's Actions page watches) is refused by any ghr older than v0.1.18, the first release with that page. Remove the `watch_repos` key from the config file before installing an older release.
```

with:

```markdown
**Downgrading.** A config that lists `watch_repos` (the repositories the web UI's Actions page watches) is refused by any ghr older than v0.1.18, the first release with that page. Remove the `watch_repos` key from the config file before installing an older release. Likewise, a config with a `github_storage` block is refused by any ghr older than the first release with the Artifacts page; remove the block first.
```

- [ ] **Step 4: Add the storage section**

In `README.md`, replace this line:

```markdown
## Migrating from the compose runners
```

with:

````markdown
## GitHub artifact storage

A GitHub plan includes a monthly allowance of storage for Actions artifacts and Packages, 500 MB on the Free plan. GitHub counts it in GB-hours over the month, so an artifact kept for days keeps using the allowance long after its upload, and deleting artifacts once the allowance is spent does not let uploads through again before the next month. The **Artifacts** tab shows the month's use against the allowance, each repository's artifacts, caches and retention, and the cleanup policy. A banner on every page warns before the allowance runs out.

1. Lower the retention to a few days on repositories whose artifacts are only used within their own run: a repository's Retention on the Artifacts page, or **Set retention** for all of them. GitHub's retention also limits how long it keeps runs and logs, which the Actions and log pages read.
2. Set a cleanup age for the rest. At the end of each inventory, every 15 minutes, ghr deletes the artifacts older than it, oldest first and at most 100 at a time, skipping the artifacts of runs that have not completed. The global age is at least 24 hours; a repository's override may go down to 1 hour, or turn cleanup off for it. Cleanup does not protect an artifact that a different run downloads later, such as one a `workflow_run` workflow, a download by run ID or a nightly job reading the last build needs.
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

## Migrating from the compose runners
````

- [ ] **Step 5: Add the commented block to the example config**

In `deploy/config.example.yaml`, replace these lines:

```yaml
web:                          # browser UI; the first visitor sets its password (setup.sh can set it first); Settings → Web access edits it
  listen: 0.0.0.0:8080
  # hosts: [ghr.lan]          # names a proxy serves it under; IPs and localhost always work
```

with:

```yaml
web:                          # browser UI; the first visitor sets its password (setup.sh can set it first); Settings → Web access edits it
  listen: 0.0.0.0:8080
  # hosts: [ghr.lan]          # names a proxy serves it under; IPs and localhost always work
# github_storage:             # the Artifacts page's quota and cleanup; its Policy card edits it
#   included_mb: 500          # the plan's artifact + Packages allowance
#   warn_pct: [70, 90]
#   cleanup_max_age: 2d       # absent is off; at least 24h
#   repos:
#     my-repo: { cleanup: 6h }  # at least 1h, or off
```

- [ ] **Step 6: Check the edits**

Run: `grep -c "Plan: read" README.md; grep -c "^## GitHub artifact storage" README.md; grep -c "github_storage" README.md deploy/config.example.yaml; grep -nP "[\x{2013}\x{2014}]" README.md deploy/config.example.yaml | grep -n "artifact storage\|github_storage\|Plan: read" ; bash deploy/tests/setup_test.sh > /root/.cache/ghr-plan-tmp/setup-test.log 2>&1; echo "setup tests exit $?"`
Expected: `1`, `1`, `README.md:2` and `deploy/config.example.yaml:1`, no line from the dash search, and `setup tests exit 0`.

- [ ] **Step 7: Commit**

```bash
git add README.md deploy/config.example.yaml
git commit -m "docs: explain GitHub artifact storage settings"
```

### Task 26: Run every suite and check on the LXC

**Files:**
- Modify: `docs/superpowers/registers/2026-10-13-ghr-github-storage.md` (Step 7)
- No code changes, unless a check below fails: then fix it in the task the failure belongs to and report it.

**Interfaces:**
- Consumes: everything above.
- Produces: the register's acceptance evidence.

**Items:** 1, 2, 3, 4

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 0 - spec 0 - coupling 1 - risk 2 = 3

- [ ] **Step 1: Ask the owner**

Ask: "All tasks are committed. May I build this branch, deploy it to the runner LXC (192.168.0.99) and restart ghr there to run read-only checks of the Artifacts page? Restarting logs every browser out; running jobs keep running. I will not set a cleanup age, change retention or delete anything there." Wait for an explicit yes. Without it, skip Steps 4 to 6, still run Steps 2, 3 and 7, and in Step 7 record rows 1 to 4 as `verify`.

- [ ] **Step 2: Run the Go suite**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 900 go test ./...`
Expected: every package `ok`. Then `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/ web/` prints nothing and `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./...` passes.

- [ ] **Step 3: Run the web suite and the setup tests**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 900 npx vitest run`, then `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx tsc --noEmit`, then `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npx eslint src --max-warnings 0`, then from the repository root `timeout 300 bash deploy/tests/setup_test.sh > /root/.cache/ghr-plan-tmp/setup-test.log 2>&1; echo "exit $?"`.
Expected: every test passes; tsc and eslint print nothing; `exit 0`.

- [ ] **Step 4: Build and deploy (only after the yes)**

```bash
B=/root/.cache/ghr-plan-tmp; H=$(git rev-parse --short HEAD)
rm -rf $B/build && mkdir -p $B/build && git archive HEAD | tar -x -C $B/build
ln -sfn "$(git rev-parse --show-toplevel)/web/node_modules" $B/build/web/node_modules
(cd $B/build/web && TMPDIR=$B timeout 600 npm run -s build)
(cd $B/build && TMPDIR=$B GOTMPDIR=$B PATH=/usr/local/go/bin:$PATH CGO_ENABLED=0 timeout 600 go build -buildvcs=false -trimpath -ldflags "-s -w -X main.version=gh-storage-$H" -o $B/ghr-gh-storage ./cmd/ghr)
timeout 30 ssh -o BatchMode=yes -o ConnectTimeout=10 root@192.168.0.99 'systemctl list-units --no-legend "ghr-runner-*" | grep -c . ; curl -s --max-time 5 --unix-socket /run/ghr/ghr.sock http://ghr/status | python3 -c "import json,sys; d=json.load(sys.stdin); print(\"busy\", sum(1 for i in d[\"instances\"] if i[\"state\"]==\"busy\"))"'
```

A failing SSH command means the LXC is not reachable from this host: stop and report it. Idle warm runners do not block the deploy (a restart adopts them); busy runners do: when `busy` is above 0, check again every two minutes for at most 30 minutes, then stop and ask the owner. When `busy` is 0:

```bash
OLD=$(timeout 30 ssh -o BatchMode=yes -o ConnectTimeout=10 root@192.168.0.99 ghr version)
timeout 120 scp -q -o BatchMode=yes -o ConnectTimeout=10 $B/ghr-gh-storage root@192.168.0.99:/usr/local/bin/ghr.new
timeout 120 ssh -o BatchMode=yes -o ConnectTimeout=10 root@192.168.0.99 "cp -p /usr/local/bin/ghr /usr/local/bin/ghr.$OLD.bak && mv /usr/local/bin/ghr.new /usr/local/bin/ghr && systemctl restart ghr && sleep 3 && systemctl is-active ghr && ghr version && grep -c github_storage /etc/ghr/config.yaml"
```

Expected: `active`, `gh-storage-<sha>`, and `0` (no `github_storage` block, so cleanup is off).

- [ ] **Step 5: Read-only checks against GitHub (spec §5)**

Wait 2 minutes for the first inventory and billing read, then save the snapshot:

```bash
timeout 30 ssh -o BatchMode=yes -o ConnectTimeout=10 root@192.168.0.99 'curl -s --max-time 10 --unix-socket /run/ghr/ghr.sock http://ghr/github-storage' > /root/.cache/ghr-plan-tmp/ghs.json
python3 -c "import json; d=json.load(open('/root/.cache/ghr-plan-tmp/ghs.json')); print('checked', d.get('checked_at')); print('quota_error', d.get('quota_error')); print('error', d.get('error')); q=d.get('quota') or {}; print({k: q.get(k) for k in ('month','used_gb_hours','included_gb_hours','over_allowance_gb_hours','level','resets_at')}); [print(r['name'], r['artifact_count'], r['artifact_bytes'], r['retention_days'], r.get('error','')) for r in d['repos']]"
```

1. **Quota permission.** When `quota_error` names a permission or the token owner, show it to the owner: granting **Plan: read** to the PAT is their action. After they say it is granted, run `timeout 30 ssh -o BatchMode=yes root@192.168.0.99 'curl -s -X POST --unix-socket /run/ghr/ghr.sock -H "X-GHR: 1" http://ghr/github-storage/refresh -o /dev/null -w "%{http_code}\n"'` (expect `202`), wait 2 minutes and save the snapshot again.
2. **Artifact bytes.** For three repositories with the most artifact bytes, compare with GitHub:

   ```bash
   for r in <repo1> <repo2> <repo3>; do printf '%s ' $r; timeout 60 gh api --paginate "repos/darkraise/$r/actions/artifacts?per_page=100" --jq '.artifacts[] | select(.expired == false) | .size_in_bytes' | awk '{s+=$1; n++} END {print n+0, s+0}'; done
   ```

   Expected: each count and byte sum equals the snapshot's row, give or take artifacts uploaded or expired between the two reads.
3. **Used GB-hours.** `timeout 60 gh api "/users/darkraise/settings/billing/usage?year=<YYYY>&month=<M>" --jq '[.usageItems[] | select(.unitType == "GigabyteHours" and (.sku == "Actions storage" or .sku == "Packages storage")) | .quantity] | add'` equals `used_gb_hours` within 0.1 (GitHub may add an hour between the reads).
4. **Allowance.** `included_gb_hours` equals 500 / 1024 × the hours in the month (744 for a 31-day month: 363.28; 720 for 30 days: 351.56).
5. **Discount.** `timeout 60 gh api "/users/darkraise/settings/billing/usage/summary?year=<YYYY>&month=<M>" --jq '.usageItems[] | select(.sku == "actions_storage" or .sku == "packages_storage") | [.sku, .grossQuantity, .discountQuantity, .netQuantity]'`. Record whether the summed `discountQuantity` stopped growing near `included_gb_hours` prorated to the day (included × hours elapsed / hours in the month) or elsewhere. Elsewhere means the allowance formula needs amending: report it to the owner; do not change code here.
6. **Billing cycle.** On or after 2026-11-01, `used_gb_hours` drops to near zero on the 1st. Before that date this check cannot run: say so in Step 7.

- [ ] **Step 6: Look at the Artifacts page (only after the yes)**

Ask the owner for the web UI password for this session (never store it). Use a browser the session can drive (the Playwright MCP browser, or headless Chrome with `--no-sandbox` when running as root); if none is available, ask the owner to look and report. Open `http://192.168.0.99:8080/artifacts` and record: the quota card shows the used and included figures of Step 5 (or the quota error), the repositories table lists the snapshot's rows with their artifact and cache sizes, expanding one row lists its artifacts with run links, Recent deletions is empty, and when `level` is not empty the banner shows on the Dashboard with its link to `/artifacts`. Do not press any delete, Save or Set retention button. The Playwright tool writes `.playwright-mcp/` into the repository root; delete it afterwards.

- [ ] **Step 7: Record the results**

Run, from the repository root:

```bash
R=docs/superpowers/registers/2026-10-13-ghr-github-storage.md
REG="$(ls -d /root/.claude/plugins/cache/darkraise/dr-superpowers/*/ | sort -V | tail -1)scripts/register"
bash "$REG" set $R 1 verify --note "LXC <date>, build gh-storage-<sha>: Step 5 checks 1 to 5 <passed|what failed>; the billing cycle check is due on 2026-11-01"
bash "$REG" set $R 3 done --note "LXC <date>, build gh-storage-<sha>: the Artifacts page showed the snapshot (Step 6)"
bash "$REG" set $R 2 verify --note "cleanup not run live: needs the owner's go-ahead, starting with one repository"
bash "$REG" set $R 4 verify --note "retention and cache deletes not run live: need the owner's go-ahead, starting with one repository"
```

Record row 3 as `verify` instead when Step 6 did not pass or did not run, and row 1 with what failed. Without the owner's yes in Step 1, record every row as `verify` with the note `LXC checks not run: no owner yes on <date>`. Commit the register: `git add docs/superpowers/registers/2026-10-13-ghr-github-storage.md && git commit -m "docs(register): record GitHub storage checks"`.
