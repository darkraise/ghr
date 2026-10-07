# ghr toolchains 2: daemon, API, CLI and setup.sh Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire plan 1's `internal/toolchain` into the ghr daemon: a new `internal/storage` package (operation queue, measurer, package-cache clears), Docker disk reporting, prune scopes with `last_prune`, the `GET /storage` family of API routes, the `ghr storage|toolchain|cache|prune` CLI commands, and the homelab `setup.sh` pre-install of the popular toolchains.

**Architecture:** `internal/system` gains install-length runners and Docker disk methods. `internal/runner` gains a `Disk` seam, `BusyCount`, prune scopes and a `last_prune` record with a `PruneDone` hook. `internal/storage` owns a `Service` holding the snapshot `GET /storage` serves, one measurer goroutine and one queue worker that runs installs, removals and clears one at a time, refusing removals and clears while runners are busy. The daemon Backend maps service errors to HTTP statuses; the API, client and CLI follow the existing patterns.

**Tech Stack:** Go 1.26 (go.mod), standard library only; `internal/toolchain` (plan 1); bash for `setup.sh` and `tests/setup_test.sh`.

**Spec:** docs/superpowers/specs/2026-10-06-ghr-toolchains-caches-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 7 of 16 tasks are heavy (Tasks 1, 5, 6, 7, 9, 10, 13: total 5 or more, or risk 3), not a majority, so the session self-implements the other nine and delegates those seven through delegated-task.md with the full per-task review; the highest self-implemented total is 4 (impl-sonnet-medium), raised to `high` because tasks are delegated.

**Plan review:** 2026-10-06 — dr-superpowers:judge-opus — executability 18 / coherence 19 / coverage 18 / assumptions 17 (round 1)

## Global Constraints

- Code repository: `D:/Repositories/Personal/ghr` (github.com/darkraise/ghr). Work in a worktree at `D:/Repositories/Personal/ghr-toolchains` on a new branch `feat/toolchains-daemon` from master `c6f5f0b`. The plan, spec and register live in `D:/Repositories/Personal/homelab`; Task 15 edits `D:/Repositories/Personal/homelab/github-runner/` on homelab `master`.
- This plan is part 2 of 3 for the spec: 1 installers (done, merged at `c6f5f0b`), 2 daemon, API, CLI and `setup.sh` (this plan), 3 TUI, release and LXC acceptance.
- Never push, never open a pull request, never commit `.superpowers/`. Pushing waits for the owner's yes at finishing.
- Every command runs from the ghr worktree root in Git Bash with an explicit `timeout` (git included), unless a step names the homelab repository. Package tests: `timeout 300 go test ./internal/<pkg>/`. Before each commit also run `timeout 120 gofmt -l internal cmd` (must print nothing) and `timeout 300 go vet ./...` (must print nothing). CI runs `go test -race -count=1 -timeout 180s ./...` on ubuntu only on a push; `-race` cannot run on this Windows box (no cgo).
- Tests that need Unix file modes, symlinks or ownership skip on Windows (`runtime.GOOS == "windows"`) or when `os.Symlink` fails; Task 16 runs them as root in the WSL distro `dev`.
- Edit Go files with the Edit or Write tools. Bash heredocs and inline `python -` lose backslashes.
- Comments: none unless the why is non-obvious; never reference this plan, a task, a review or a finding.
- Commits: `<type>(<scope>): <subject>`, subject ≤ 50 characters, imperative, no period. Scopes: `system`, `toolchain`, `model`, `runner`, `storage`, `daemon`, `api`, `cli`; homelab: `github-runner`. One commit per task.
- Tool names (API and CLI): `node`, `go`, `python`, `java`, `dotnet`. Preset name: `popular`. Paths: tool cache `/var/lib/ghr/toolcache`, runner home `/home/ghrunner`, runner user `ghrunner`.
- Prune scopes, exactly: `standard`, `build-cache-keep`, `build-cache-all`, `dangling-images`, `unused-volumes`. `unused-volumes` runs `docker volume prune -f`, is manual only, and is refused while any runner is busy.
- Refusal message, exactly: `refused: N jobs running` (N the busy count). Queuing CLI commands print exactly `queued — follow with: ghr storage`.
- Operation kinds: `install`, `remove`, `clear`. Outcomes: `ok`, `failed`, `refused`, `interrupted`, `skipped`. Prune outcomes: `ok`, `errors`, `interrupted`; prune triggers `auto`, `manual`; the automatic prune's scope is `auto`.
- The measurer runs every 30 minutes, after every queued operation, after every prune, and on `POST /storage/refresh`; triggers during a measurement coalesce into one more. The last 10 finished operations are kept, newest first.
- Each install command and download is limited to 60 minutes (`daemon.InstallTimeout = time.Hour`); `Exec`, `ExecGroup` and `Download` keep reading `CommandTimeout` and `DownloadTimeout` on every call.
- Sizes from Docker are decimal (base 1000): `B`, `kB`, `MB`, `GB`, `TB`, `PB`, optionally followed by ` (NN%)`.

## Contracts

**C1 `internal/system` timeouts (Task 1):** `func ExecGroupFor(d time.Duration) Runner`; `func DownloadFor(d time.Duration) func(ctx context.Context, url, dst string) error`.

**C2 `internal/toolchain` (Task 2):** `func NewEnv(root, home, user string, run system.Runner, fetch func(ctx context.Context, url, dst string) error) *Env`. Unchanged from plan 1 and consumed here: `toolchain.New(e *Env) *Set`, `(*Set).Get/Available/Installed/Other/Root/CleanTmp`, `toolchain.Popular []Entry{Tool, Spec}`, `toolchain.Installer`, `toolchain.Installed{Tool, Version, Arch, Path, InstalledAt}`, `toolchain.Choice{Spec, Version, LTS}`, `toolchain.Release{Tool, Version, Folder, URL, SHA256}`, `toolchain.ErrUnknownTool`, `toolchain.ErrNotInstalled`, `toolchain.ErrAlreadyInstalled`. Java versions are listed and removed in the `+` form (`21.0.8+9`).

**C3 `internal/system` Docker disk (Task 3):**

```go
type DiskRow struct {
	Type        string // Images, Containers, Local Volumes, Build Cache
	Count       int
	Active      int
	Bytes       int64
	Reclaimable int64
}
type CacheTypeUsage struct {
	Type        string
	Count       int
	Bytes       int64
	Reclaimable int64
}
func ParseSize(s string) (int64, error)
func (d Docker) DiskUsage(ctx context.Context) ([]DiskRow, error)
func (d Docker) BuildCacheUsage(ctx context.Context) ([]CacheTypeUsage, error)
func (d Docker) PruneAllBuildCache(ctx context.Context) (string, error)
func (d Docker) PruneUnusedVolumes(ctx context.Context) (string, error)
```

**C4 `internal/model` (Task 4), file `internal/model/storage.go`:** `Storage`, `Toolchain`, `Folder`, `PackageCache`, `DockerDisk`, `DockerRow`, `BuildCacheType`, `Operations`, `Operation`, `LastPrune`, `PruneStep`, `ToolchainChoice`, `InstallRequest`, `func HumanBytes(n int64) string` — fields and JSON tags exactly as Task 4 Step 3 writes them.

**C5 `internal/runner` (Task 5):**

```go
type Disk interface {
	DiskUsage(ctx context.Context) ([]system.DiskRow, error)
	BuildCacheUsage(ctx context.Context) ([]system.CacheTypeUsage, error)
	PruneAllBuildCache(ctx context.Context) (string, error)
	PruneUnusedVolumes(ctx context.Context) (string, error)
}
// Manager gains the field `Disk Disk`.
const (
	ScopeStandard       = "standard"
	ScopeBuildCacheKeep = "build-cache-keep"
	ScopeBuildCacheAll  = "build-cache-all"
	ScopeDanglingImages = "dangling-images"
	ScopeUnusedVolumes  = "unused-volumes"
)
var Scopes = []string{ScopeStandard, ScopeBuildCacheKeep, ScopeBuildCacheAll, ScopeDanglingImages, ScopeUnusedVolumes}
var ErrUnknownScope = errors.New("unknown prune scope")
type BusyError struct{ N int } // Error(): "refused: N jobs running"
func (m *Manager) BusyCount() int
```

**C6 `internal/runner` (Task 6):** `func (m *Manager) StartPruneScope(scope string) error` (`StartPrune()` stays and runs `standard`); `func (m *Manager) LastPrune() *model.LastPrune`; Manager field `PruneDone func()`.

**C7 `internal/storage` caches (Task 7):** `type Cache struct{ Name, Label string; Paths []string }`; `var Caches []Cache` (the spec §2 table, in its order); unexported `cacheByName(name string) (Cache, bool)`, `(Cache) abs(home string) []string`, `(Cache) present(home string) bool`, `clearPath(path, opID string) error`, `noLinkedParent(home, path string) error`, `clearCache(home string, c Cache, opID string) error`, `sweepClearing(home string) error`, `chownLike(path string, fi os.FileInfo) error`, `allocated(fi os.FileInfo) int64`, const `clearingTag = ".ghr-clearing-"`. Test helpers in `caches_test.go`: `writeFile(t, path, body)`, `entries(t, dir) []string`.

**C8 `internal/storage` measurement (Task 8):**

```go
type Toolchains interface {
	Get(tool string) (toolchain.Installer, error)
	Available(ctx context.Context, tool string) ([]toolchain.Choice, error)
	Installed() ([]toolchain.Installed, error)
	Other() ([]string, error)
	Root() string
	CleanTmp() error
}
type Docker interface {
	DiskUsage(ctx context.Context) ([]system.DiskRow, error)
	BuildCacheUsage(ctx context.Context) ([]system.CacheTypeUsage, error)
}
```

Unexported: `usage{Bytes, Files int64; Last time.Time}`, `walk(path string) (usage, bool, error)`, `measured{at; toolchains; other; caches; docker []model.DockerRow; cacheTypes []model.BuildCacheType; err string}`, `emptyMeasured() measured`, `measure(ctx, tools Toolchains, docker Docker, home string, at time.Time) measured`. Test fakes in `fakes_test.go`: `fakeTools`, `fakeInstaller`, `newInstaller(map[string]string) *fakeInstaller`, `fakeDisk`, `waitFor(t, what, cond)`.

**C9 `internal/storage` service (Task 9):** `type Service struct{ Tools Toolchains; Docker Docker; Home string; Busy func() int; Events *events.Ring; Now func() time.Time; NewID func() string; … }`; `var ErrMeasuring, ErrClosed`; `const MeasureInterval = 30 * time.Minute`; methods `Start()`, `Close()`, `Wait()`, `Snapshot() model.Storage`, `Refresh() error`, `Trigger()`; unexported `type op struct{ model.Operation; run func(ctx context.Context, progress func(step string)) (outcome, message string) }`. Test helpers in `service_test.go`: `newService(t, tools, disk) *Service` (unstarted), `startService(t, s)` (starts it and closes it at cleanup), `recvOrFail(t, ch, what)`.

**C10 `internal/storage` queue (Task 10):** `var ErrUnknownCache, ErrNotPresent, ErrUnknownPreset, ErrMissingVersion`; methods `Install(tool, spec string) error`, `InstallPreset(name string) error`, `Remove(tool, version string) error`, `Clear(name string) error`, `Available(ctx context.Context, tool string) ([]model.ToolchainChoice, error)`.

**C11 `internal/daemon` (Task 11):** `type StorageService interface{ Snapshot() model.Storage; Refresh() error; Available(ctx, tool) ([]model.ToolchainChoice, error); Install(tool, spec string) error; InstallPreset(name string) error; Remove(tool, version string) error; Clear(name string) error }`; Backend field `Space StorageService`; daemon `Manager` interface: `StartPrune() error` replaced by `StartPruneScope(scope string) error`, plus `LastPrune() *model.LastPrune`; Backend methods `Storage() model.Storage`, `RefreshStorage() error`, `AvailableToolchains(ctx context.Context, tool string) ([]model.ToolchainChoice, error)`, `InstallToolchain(req model.InstallRequest) error`, `RemoveToolchain(tool, version string) error`, `ClearCache(name string) error`, `PruneScope(scope string) error`.

**C12 `internal/api` (Task 12):** `api.Backend` gains the seven C11 Backend methods. Routes: `GET /storage`, `POST /storage/refresh`, `GET /toolchains/available?tool=`, `POST /toolchains`, `DELETE /toolchains/{tool}/{version}`, `POST /caches/{name}/clear`, `POST /prune/{scope}`; mutations answer 202. Client: `Storage(ctx) (model.Storage, error)`, `RefreshStorage(ctx) error`, `AvailableToolchains(ctx, tool) ([]model.ToolchainChoice, error)`, `InstallToolchain(ctx, tool, version) error`, `InstallPreset(ctx, preset) error`, `RemoveToolchain(ctx, tool, version) error`, `ClearCache(ctx, name) error`, `PruneScope(ctx, scope) error`.

**C13 `internal/daemon` wiring (Task 13):** `const InstallTimeout = time.Hour`; `Options.Disk runner.Disk` (nil uses `system.Docker{Run: system.Exec}`); test double `nopDisk` in `run_test.go`.

**C14 CLI (Task 14):** commands of spec §8 in `cmd/ghr/storage.go`; const `queuedNote = "queued — follow with: ghr storage"`.

## Assumptions (evidence)

- `execute` wraps every command in `context.WithTimeout(ctx, CommandTimeout)` read on each call, and `Download` uses `DownloadTimeout` likewise, so a wrapper cannot lengthen either limit — `internal/system/system.go:25,40-41,365-371`. The advisor check of the chosen mechanism (judge-opus, 2026-10-06) found it sound, on two conditions: keep `Exec`/`ExecGroup`/`Download` reading the variables per call, because `system_test.go:214-244` overrides `CommandTimeout` at run time; and update the two `NewEnv` calls in `internal/toolchain/env_test.go:57,91`.
- Every long install step already goes through `Env.Run`, `Env.Fetch` or `Env.Extract` (`internal/toolchain/layout.go:119,217,284`, `python.go:46,71`, `dotnet.go:119,129`, `env.go:40-43`) — advisor check and Fable review, 2026-10-06.
- `docker system df --format json` prints one object per line with string fields `Active`, `Reclaimable`, `Size`, `TotalCount`, `Type`; `-v --format json` prints one document with keys `BuildCache`, `Containers`, `Images`, `Volumes` — captured on the runner LXC (Docker 29.8.2) on 2026-10-06 via `ssh -i ~/.ssh/ghr_lxc root@192.168.0.99`; copies in `.superpowers/sdd/2026-10-06-ghr-toolchains-2-daemon/fixtures/`.
- Build-cache record fields (`ID` with `*` when in use, `CacheType`, `Size` humanized to three significant digits, `InUse`/`Shared` as `"true"`/`"false"`, …) come from docker/cli `cli/command/formatter/buildcache.go` at tag v29.8.2 (fetched 2026-10-06); the LXC had no build cache. Plan 3's LXC acceptance (register row 4) confirms them against real records.
- `docker volume prune -f` without `--all` removes only unused anonymous volumes; a volume referenced by any container, running or stopped, is in use — `docker volume prune --help` on the LXC 2026-10-06 and the Fable review of the spec amendment (docs/superpowers/notes/2026-10-06-ghr-toolchains-caches-amendment-fable-review.md).
- `docker builder prune -af` and `docker volume prune -f` print a `Total:` or `Total reclaimed space:` line that the existing `reclaimed` reads — `internal/system/system.go:306-313`; unverified for these two commands on Docker 29.8.2 — plan 3's LXC acceptance (register rows 4 and 5) verifies `last_prune` shows the space freed.
- `readJobFiles` takes `m.mu` itself per instance (`internal/runner/lifecycle.go:155-185`), so `BusyCount` calls it first and then takes the lock to count; spec §2 ("takes the manager's lock, runs readJobFiles()") is met in that order, as holding the lock across the call would deadlock.
- `filepath.WalkDir` does not follow symbolic links and reports entries with `Lstat` (Go `path/filepath` documentation); `syscall.Stat_t.Blocks` counts 512-byte units on Linux (stat(2)).
- The daemon's run tests build `Options` with `Docker: nopDocker{}` on two lines matching `Docker: nopDocker{}, Host: dirHost{}` — `internal/daemon/run_test.go:172,246` (`grep -c` → 2, 2026-10-06).
- The web UI forwards every `/api/` path to the API handler without a route list — `internal/webui/handler.go:41` — so the new routes inherit login, `X-GHR` and the Origin check.
- The Python runtime libraries' Debian 13 names (`libssl3t64 libffi8 libsqlite3-0 liblzma5 libbz2-1.0 libgdbm6t64 libncursesw6 libreadline8t64`) — `apt-cache policy` on the LXC, 2026-10-06; all were already installed there.
- `setup.sh` changes land on homelab `master` before the ghr release (plan 3). Until the LXC runs a ghr with `ghr toolchain`, queuing fails and `setup.sh` only warns, which spec §8 requires.
- No task carries an `**Items:**` line: register rows 1–5 (docs/superpowers/registers/2026-10-06-ghr-toolchains-caches.md) are discharged by plan 3's release and LXC acceptance task, where their acceptance criteria can be observed; this plan builds what they rest on.
- No external executor: `codex-gate usable=false reason=plugin-not-enabled lane=false` (2026-10-06).
- WSL distro `dev` runs Linux test binaries as root and has `tar` — plan 1 handoff (`.superpowers/sdd/2026-10-06-ghr-toolchains-1-installers/handoff.md:26`).

## Task index

1. Install-length command and download limits
2. NewEnv takes the download function
3. Docker disk usage, build cache and new prunes
4. Storage data model
5. Runner disk seam and busy count
6. Prune scopes and last prune
7. Package caches and clears
8. Measurement
9. Storage service and measurer loop
10. Operation queue
11. Backend storage methods
12. API routes and client
13. Daemon wiring
14. CLI commands
15. setup.sh toolchain pre-install
16. Final verification

---

### Task 1: Install-length command and download limits

**Files:**
- Modify: `internal/system/system.go` (`Exec`, `ExecGroup`, `execute`, `Download`)
- Test: `internal/system/system_test.go`, `internal/system/host_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: C1 `ExecGroupFor`, `DownloadFor`.

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 0: Create the worktree and branch**

```bash
cd /d/Repositories/Personal/ghr
timeout 30 git status --short
timeout 30 git worktree add -b feat/toolchains-daemon /d/Repositories/Personal/ghr-toolchains c6f5f0b
cd /d/Repositories/Personal/ghr-toolchains
timeout 30 git log --oneline -1
```

Expected: `git status` prints nothing; the last line is `c6f5f0b fix(toolchain): harden installs after review`. Every later command runs from `D:/Repositories/Personal/ghr-toolchains`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/system/system_test.go`:

```go
func TestExecGroupFollowsCommandTimeout(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not on PATH")
	}
	old := CommandTimeout
	CommandTimeout = 100 * time.Millisecond
	defer func() { CommandTimeout = old }()
	start := time.Now()
	if _, err := ExecGroup(context.Background(), "sleep", "10"); err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("ExecGroup ignored CommandTimeout: %v", time.Since(start))
	}
}

func TestExecGroupForTimesOutAtItsOwnLimit(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not on PATH")
	}
	old := CommandTimeout
	CommandTimeout = time.Hour
	defer func() { CommandTimeout = old }()
	start := time.Now()
	if _, err := ExecGroupFor(100*time.Millisecond)(context.Background(), "sleep", "10"); err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("ExecGroupFor ignored its own timeout: %v", time.Since(start))
	}
}

func TestExecGroupForOutlastsCommandTimeout(t *testing.T) {
	if _, err := exec.LookPath("sleep"); err != nil {
		t.Skip("sleep not on PATH")
	}
	old := CommandTimeout
	CommandTimeout = 100 * time.Millisecond
	defer func() { CommandTimeout = old }()
	if _, err := ExecGroupFor(time.Minute)(context.Background(), "sleep", "1"); err != nil {
		t.Fatalf("ExecGroupFor used CommandTimeout: %v", err)
	}
}
```

In `internal/system/host_test.go`, add `"time"` to the import block (after `"testing"`), then append:

```go
// hangingServer never answers; a request ends only when the client gives up.
func hangingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadFollowsDownloadTimeout(t *testing.T) {
	srv := hangingServer(t)
	old := DownloadTimeout
	DownloadTimeout = 100 * time.Millisecond
	defer func() { DownloadTimeout = old }()
	start := time.Now()
	if err := Download(context.Background(), srv.URL, filepath.Join(t.TempDir(), "f")); err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("Download ignored DownloadTimeout: %v", time.Since(start))
	}
}

func TestDownloadForTimesOutAtItsOwnLimit(t *testing.T) {
	srv := hangingServer(t)
	old := DownloadTimeout
	DownloadTimeout = time.Hour
	defer func() { DownloadTimeout = old }()
	start := time.Now()
	if err := DownloadFor(100*time.Millisecond)(context.Background(), srv.URL, filepath.Join(t.TempDir(), "f")); err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("DownloadFor ignored its own timeout: %v", time.Since(start))
	}
}

func TestDownloadForSavesTheBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("archive"))
	}))
	t.Cleanup(srv.Close)
	dst := filepath.Join(t.TempDir(), "a.tar.gz")
	if err := DownloadFor(time.Minute)(context.Background(), srv.URL, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "archive" {
		t.Fatalf("saved %q", b)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/system/`
Expected: FAIL to compile — `undefined: ExecGroupFor`, `undefined: DownloadFor`.

- [ ] **Step 3: Write the implementation**

In `internal/system/system.go`, replace the block from `// Exec is the real Runner.` through the line `ctx, cancel := context.WithTimeout(ctx, CommandTimeout)` (the first two lines of `execute`) with:

```go
// Exec is the real Runner. Errors name only the command and its first argument,
// because full argument lists can carry secrets (the JIT config).
func Exec(ctx context.Context, name string, args ...string) ([]byte, error) {
	return execute(ctx, CommandTimeout, false, name, args)
}

// ExecGroup is Exec for a command that starts children of its own, such as a
// script running apt: cancelling it kills its whole process group, not only
// the command.
func ExecGroup(ctx context.Context, name string, args ...string) ([]byte, error) {
	return execute(ctx, CommandTimeout, true, name, args)
}

// ExecGroupFor is ExecGroup with its own timeout d in place of
// CommandTimeout, for commands that legitimately run longer, such as a
// toolchain install.
func ExecGroupFor(d time.Duration) Runner {
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return execute(ctx, d, true, name, args)
	}
}

func execute(ctx context.Context, timeout time.Duration, group bool, name string, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
```

The rest of `execute` stays as it is.

Replace the whole `Download` function (from `// Download saves url to dst, following redirects.` to its closing brace; keep `DownloadTimeout` above it) with:

```go
// Download saves url to dst, following redirects. It sends no credentials:
// it fetches public release assets.
func Download(ctx context.Context, url, dst string) error {
	return download(ctx, DownloadTimeout, url, dst)
}

// DownloadFor is Download with its own timeout d in place of DownloadTimeout.
func DownloadFor(d time.Duration) func(ctx context.Context, url, dst string) error {
	return func(ctx context.Context, url, dst string) error {
		return download(ctx, d, url, dst)
	}
}

func download(ctx context.Context, timeout time.Duration, url, dst string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/system/`
Expected: `ok` (the `sleep` tests may report SKIP if `sleep` is not on PATH; they run in CI).

Run: `timeout 600 go test ./...`, `timeout 120 gofmt -l internal cmd`, `timeout 300 go vet ./...`
Expected: every package `ok`; gofmt and vet print nothing.

- [ ] **Step 5: Commit**

```bash
timeout 30 git add internal/system/system.go internal/system/system_test.go internal/system/host_test.go
timeout 30 git commit -m "feat(system): add install-length runner and download"
```

### Task 2: NewEnv takes the download function

**Files:**
- Modify: `internal/toolchain/env.go` (`NewEnv`)
- Test: `internal/toolchain/env_test.go:51-108`

**Interfaces:**
- Consumes: C1 (the daemon will pass `DownloadFor`; this task's tests pass their own functions).
- Produces: C2 `NewEnv(root, home, user, run, fetch)`.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

In `internal/toolchain/env_test.go`, replace the whole `TestNewEnvWiresTheRunner` function with:

```go
func TestNewEnvWiresTheRunnerAndFetch(t *testing.T) {
	var calls []string
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil, nil
	}
	fetch := func(_ context.Context, url, dst string) error {
		calls = append(calls, "fetch "+url+" "+dst)
		return nil
	}
	e := NewEnv("/var/lib/ghr/toolcache", "/home/ghrunner", "ghrunner", run, fetch)
	if e.Root != "/var/lib/ghr/toolcache" || e.Home != "/home/ghrunner" || e.User != "ghrunner" || e.Sources != DefaultSources() {
		t.Fatalf("env %+v", e)
	}
	if err := e.Extract(context.Background(), "a.tar.gz", "dir"); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Run(context.Background(), "chown", "x"); err != nil {
		t.Fatal(err)
	}
	if err := e.Fetch(context.Background(), "https://example.test/a.tar.gz", "dst"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"tar -xzf a.tar.gz -C dir", "chown x", "fetch https://example.test/a.tar.gz dst"}; !slices.Equal(calls, want) {
		t.Fatalf("calls %q", calls)
	}
	if a, b := e.OpID(), e.OpID(); a == b || a == "" {
		t.Fatalf("op ids %q, %q", a, b)
	}
}
```

In `TestNewEnvExtractsWithRealTar`, change `NewEnv(dir, "", "", system.Exec)` to `NewEnv(dir, "", "", system.Exec, system.Download)`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 300 go test ./internal/toolchain/ -run NewEnv`
Expected: FAIL to compile — `too many arguments in call to NewEnv`.

- [ ] **Step 3: Write the implementation**

In `internal/toolchain/env.go`, replace the `NewEnv` doc comment and function with:

```go
// NewEnv is the daemon's Env for the tool cache at root. run should kill a
// command's whole process group on cancel (system.ExecGroupFor), because the
// install scripts start children of their own; run and fetch carry the
// install-length timeouts.
func NewEnv(root, home, user string, run system.Runner, fetch func(ctx context.Context, url, dst string) error) *Env {
	return &Env{
		Root:    root,
		Home:    home,
		User:    user,
		Sources: DefaultSources(),
		Get:     httpGet,
		Fetch:   fetch,
		Run:     run,
		Extract: func(ctx context.Context, archive, dir string) error {
			_, err := run(ctx, "tar", "-xzf", archive, "-C", dir)
			return err
		},
		OpID: newOpID,
	}
}
```

`env.go` still imports `internal/system` for the `system.Runner` type.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/toolchain/`
Expected: `ok`.

Run: `timeout 600 go test ./...`, `timeout 120 gofmt -l internal cmd`, `timeout 300 go vet ./...`
Expected: every package `ok`; gofmt and vet print nothing.

- [ ] **Step 5: Commit**

```bash
timeout 30 git add internal/toolchain/env.go internal/toolchain/env_test.go
timeout 30 git commit -m "feat(toolchain): take the download function in NewEnv"
```

### Task 3: Docker disk usage, build cache and new prunes

**Files:**
- Create: `internal/system/disk.go`
- Create: `internal/system/testdata/df.jsonl`, `internal/system/testdata/df-buildcache.jsonl`, `internal/system/testdata/df-v.json`
- Test: `internal/system/disk_test.go`

**Interfaces:**
- Consumes: the package's existing `Docker`, `lines`, `reclaimed` and the test helper `fake` (`internal/system/system_test.go:20-36`).
- Produces: C3.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the fixtures**

`internal/system/testdata/df.jsonl` — real output from the runner LXC, Docker 29.8.2, 2026-10-06 (four lines, newline at the end):

```
{"Active":"2","Reclaimable":"5.627GB (85%)","Size":"6.571GB","TotalCount":"9","Type":"Images"}
{"Active":"2","Reclaimable":"0B (0%)","Size":"40.96kB","TotalCount":"2","Type":"Containers"}
{"Active":"1","Reclaimable":"8.65GB (100%)","Size":"8.65GB","TotalCount":"68","Type":"Local Volumes"}
{"Active":"0","Reclaimable":"0B","Size":"0B","TotalCount":"0","Type":"Build Cache"}
```

`internal/system/testdata/df-buildcache.jsonl` — the Build Cache row's `Reclaimable` never carries a percentage (two lines, newline at the end):

```
{"Active":"2","Reclaimable":"5.627GB (85%)","Size":"6.571GB","TotalCount":"9","Type":"Images"}
{"Active":"1","Reclaimable":"1.2GB","Size":"1.5TB","TotalCount":"42","Type":"Build Cache"}
```

`internal/system/testdata/df-v.json` — the real `docker system df -v --format json` document trimmed to one image, one container and two volumes, with four `BuildCache` records written from the docker/cli v29.8.2 formatter (one line, newline at the end):

```
{"BuildCache":[{"CacheType":"regular","CreatedAt":"2026-10-05 09:12:44.123456789 +0000 UTC","CreatedSince":"20 hours ago","Description":"[build 3/5] RUN apt-get update","ID":"k2j3h4g5f6d7*","InUse":"true","LastUsedAt":"2026-10-06 03:00:01.5 +0000 UTC","LastUsedSince":"2 hours ago","Parent":"","Shared":"false","Size":"1.23GB","UsageCount":"4"},{"CacheType":"regular","CreatedAt":"2026-10-04 08:01:02.5 +0000 UTC","CreatedSince":"45 hours ago","Description":"[build 2/5] COPY . .","ID":"m8n7b6v5c4x3","InUse":"false","LastUsedAt":"2026-10-04 08:01:02.5 +0000 UTC","LastUsedSince":"45 hours ago","Parent":"k2j3h4g5f6d7","Shared":"true","Size":"456MB","UsageCount":"1"},{"CacheType":"exec.cachemount","CreatedAt":"2026-10-01 10:00:00 +0000 UTC","CreatedSince":"5 days ago","Description":"cached mount /root/.nuget/packages from exec /bin/sh -c dotnet restore","ID":"p1o2i3u4y5t6","InUse":"false","LastUsedAt":"2026-10-06 01:00:00 +0000 UTC","LastUsedSince":"4 hours ago","Parent":"","Shared":"false","Size":"2.1GB","UsageCount":"9"},{"CacheType":"source.local","CreatedAt":"2026-10-06 04:30:00 +0000 UTC","CreatedSince":"30 minutes ago","Description":"local source for context","ID":"z9x8c7v6b5n4","InUse":"false","LastUsedAt":"","LastUsedSince":"","Parent":"","Shared":"false","Size":"12.5kB","UsageCount":"1"}],"Containers":[{"Command":"\"/bin/alloy run /etc/alloy/config.alloy --storage.path=/var/lib/alloy/data\"","CreatedAt":"2026-06-18 14:28:44 +0700 +07","HealthStatus":"none","ID":"7355c6fa7c55807f2112f5ab80a1a38848879cddba11c189a21e51f1595ff3ee","Image":"grafana/alloy:latest","Labels":"com.docker.compose.project=alloy,com.docker.compose.service=alloy","LocalVolumes":"0","Mounts":"/opt/alloy/config.alloy,/var/run/docker.sock,/opt/alloy/alloy-data","Names":"alloy","Networks":"host","Platform":null,"Ports":"","RunningFor":"3 months ago","Size":"24.6kB","State":"running","Status":"Up 47 hours"}],"Images":[{"Containers":"0","CreatedAt":"2026-10-03 07:24:32 +0700 +07","CreatedSince":"3 days ago","Digest":"","ID":"sha256:4510b5fe6b79cf801a2442429263d1154945210d1282142b375fab31c3e55a8b","Repository":"myoung34/github-runner","SharedSize":"0B","Size":"3.32GB","Tag":"ubuntu-noble","UniqueSize":"3.323GB"}],"Volumes":[{"Availability":"N/A","Driver":"local","Group":"N/A","Labels":"com.docker.volume.anonymous=","Links":"0","Mountpoint":"/var/lib/docker/volumes/7fb3455f94fda981ac0d2067ec9b77c958f23c5421b3fb7eb5bfc20f72dd20dc/_data","Name":"7fb3455f94fda981ac0d2067ec9b77c958f23c5421b3fb7eb5bfc20f72dd20dc","Scope":"local","Size":"53.68MB","Status":"N/A"},{"Availability":"N/A","Driver":"local","Group":"N/A","Labels":"com.docker.volume.anonymous=","Links":"0","Mountpoint":"/var/lib/docker/volumes/11aed66a13edb516ff38a5d86fcadc22a6de258acc6035f4915bf4f7f75d5a78/_data","Name":"11aed66a13edb516ff38a5d86fcadc22a6de258acc6035f4915bf4f7f75d5a78","Scope":"local","Size":"79.74MB","Status":"N/A"}]}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/system/disk_test.go`:

```go
package system

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseSize(t *testing.T) {
	for in, want := range map[string]int64{
		"0B":            0,
		"512B":          512,
		"40.96kB":       40960,
		"6.571GB":       6571000000,
		"5.627GB (85%)": 5627000000,
		"0B (0%)":       0,
		"1.5TB":         1500000000000,
		"2PB":           2000000000000000,
		" 456MB ":       456000000,
	} {
		got, err := ParseSize(in)
		if err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "GB", "12XB", "abcMB", "1.2.3GB"} {
		if _, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) accepted", in)
		}
	}
}

func TestDiskUsageReadsTheLXCOutput(t *testing.T) {
	run, calls := fake(map[string]string{"docker system df --format json": readFixture(t, "df.jsonl")})
	rows, err := Docker{Run: run}.DiskUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []DiskRow{
		{Type: "Images", Count: 9, Active: 2, Bytes: 6571000000, Reclaimable: 5627000000},
		{Type: "Containers", Count: 2, Active: 2, Bytes: 40960, Reclaimable: 0},
		{Type: "Local Volumes", Count: 68, Active: 1, Bytes: 8650000000, Reclaimable: 8650000000},
		{Type: "Build Cache", Count: 0, Active: 0, Bytes: 0, Reclaimable: 0},
	}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows %+v", rows)
	}
	if got := (*calls)[0].String(); got != "docker system df --format json" {
		t.Fatalf("call %s", got)
	}
}

func TestDiskUsageReadsABareBuildCacheReclaimable(t *testing.T) {
	run, _ := fake(map[string]string{"docker system df --format json": readFixture(t, "df-buildcache.jsonl")})
	rows, err := Docker{Run: run}.DiskUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1] != (DiskRow{Type: "Build Cache", Count: 42, Active: 1, Bytes: 1500000000000, Reclaimable: 1200000000}) {
		t.Fatalf("rows %+v", rows)
	}
}

func TestDiskUsageRejectsAnUnreadableRow(t *testing.T) {
	run, _ := fake(map[string]string{"docker system df --format json": `{"Active":"2","Reclaimable":"1GB","Size":"lots","TotalCount":"9","Type":"Images"}`})
	if _, err := (Docker{Run: run}).DiskUsage(context.Background()); err == nil || !strings.Contains(err.Error(), "Images") {
		t.Fatalf("err %v", err)
	}
}

func TestBuildCacheUsageGroupsRecordsByType(t *testing.T) {
	run, calls := fake(map[string]string{"docker system df -v --format json": readFixture(t, "df-v.json")})
	got, err := Docker{Run: run}.BuildCacheUsage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []CacheTypeUsage{
		{Type: "exec.cachemount", Count: 1, Bytes: 2100000000, Reclaimable: 2100000000},
		{Type: "regular", Count: 2, Bytes: 1686000000, Reclaimable: 456000000},
		{Type: "source.local", Count: 1, Bytes: 12500, Reclaimable: 12500},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("usage %+v", got)
	}
	if c := (*calls)[0].String(); c != "docker system df -v --format json" {
		t.Fatalf("call %s", c)
	}
}

func TestBuildCacheUsageWithoutRecords(t *testing.T) {
	run, _ := fake(map[string]string{"docker system df -v --format json": `{"BuildCache":[],"Containers":[],"Images":[],"Volumes":[]}`})
	got, err := Docker{Run: run}.BuildCacheUsage(context.Background())
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestPruneAllBuildCacheAndUnusedVolumes(t *testing.T) {
	run, calls := fake(map[string]string{
		"docker builder prune -af": "ID\tRECLAIMABLE\nabc\t3.1GB\nTotal:\t3.1GB\n",
		"docker volume prune -f":   "Deleted Volumes:\n7fb3455f\n\nTotal reclaimed space: 8.65GB\n",
	})
	d := Docker{Run: run}
	if freed, err := d.PruneAllBuildCache(context.Background()); err != nil || freed != "3.1GB" {
		t.Fatalf("build cache freed %q, %v", freed, err)
	}
	if freed, err := d.PruneUnusedVolumes(context.Background()); err != nil || freed != "8.65GB" {
		t.Fatalf("volumes freed %q, %v", freed, err)
	}
	if a, b := (*calls)[0].String(), (*calls)[1].String(); a != "docker builder prune -af" || b != "docker volume prune -f" {
		t.Fatalf("calls %s; %s", a, b)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/system/ -run 'ParseSize|DiskUsage|BuildCacheUsage|PruneAll'`
Expected: FAIL to compile — `undefined: ParseSize`, `undefined: DiskRow`, `undefined: CacheTypeUsage`, and the four `Docker` methods.

- [ ] **Step 4: Write the implementation**

Create `internal/system/disk.go`:

```go
package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// DiskRow is one row of `docker system df`.
type DiskRow struct {
	Type        string // Images, Containers, Local Volumes, Build Cache
	Count       int
	Active      int
	Bytes       int64
	Reclaimable int64
}

// CacheTypeUsage sums the build cache records of one CacheType; records not
// in use are reclaimable.
type CacheTypeUsage struct {
	Type        string
	Count       int
	Bytes       int64
	Reclaimable int64
}

var sizeUnits = map[string]float64{"B": 1, "kB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12, "PB": 1e15}

// ParseSize reads Docker's humanized sizes, which use decimal units:
// "40.96kB", "0B", or "5.627GB (85%)" with the share Docker appends.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, " ("); i >= 0 {
		s = s[:i]
	}
	i := strings.IndexFunc(s, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if i <= 0 {
		return 0, fmt.Errorf("unreadable size %q", s)
	}
	mult, ok := sizeUnits[s[i:]]
	if !ok {
		return 0, fmt.Errorf("unknown unit in size %q", s)
	}
	n, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0, fmt.Errorf("unreadable size %q", s)
	}
	return int64(math.Round(n * mult)), nil
}

// DiskUsage reads `docker system df`: one JSON object per line, every field a string.
func (d Docker) DiskUsage(ctx context.Context) ([]DiskRow, error) {
	out, err := d.Run(ctx, "docker", "system", "df", "--format", "json")
	if err != nil {
		return nil, err
	}
	var rows []DiskRow
	for _, l := range lines(out) {
		var r struct{ Type, TotalCount, Active, Size, Reclaimable string }
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			return nil, fmt.Errorf("docker system df: %w", err)
		}
		count, err1 := strconv.Atoi(r.TotalCount)
		active, err2 := strconv.Atoi(r.Active)
		size, err3 := ParseSize(r.Size)
		reclaimable, err4 := ParseSize(r.Reclaimable)
		if err := errors.Join(err1, err2, err3, err4); err != nil {
			return nil, fmt.Errorf("docker system df %s: %w", r.Type, err)
		}
		rows = append(rows, DiskRow{Type: r.Type, Count: count, Active: active, Bytes: size, Reclaimable: reclaimable})
	}
	return rows, nil
}

// BuildCacheUsage groups the build cache records of `docker system df -v`
// by CacheType, sorted by type name.
func (d Docker) BuildCacheUsage(ctx context.Context) ([]CacheTypeUsage, error) {
	out, err := d.Run(ctx, "docker", "system", "df", "-v", "--format", "json")
	if err != nil {
		return nil, err
	}
	var doc struct {
		BuildCache []struct{ CacheType, Size, InUse string }
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, fmt.Errorf("docker system df -v: %w", err)
	}
	by := map[string]*CacheTypeUsage{}
	for _, r := range doc.BuildCache {
		n, err := ParseSize(r.Size)
		if err != nil {
			return nil, fmt.Errorf("docker system df -v: %w", err)
		}
		u := by[r.CacheType]
		if u == nil {
			u = &CacheTypeUsage{Type: r.CacheType}
			by[r.CacheType] = u
		}
		u.Count++
		u.Bytes += n
		if r.InUse != "true" {
			u.Reclaimable += n
		}
	}
	res := make([]CacheTypeUsage, 0, len(by))
	for _, u := range by {
		res = append(res, *u)
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Type < res[j].Type })
	return res, nil
}

func (d Docker) PruneAllBuildCache(ctx context.Context) (string, error) {
	out, err := d.Run(ctx, "docker", "builder", "prune", "-af")
	return reclaimed(out), err
}

// PruneUnusedVolumes removes anonymous volumes no container references.
// Without --all, named volumes stay.
func (d Docker) PruneUnusedVolumes(ctx context.Context) (string, error) {
	out, err := d.Run(ctx, "docker", "volume", "prune", "-f")
	return reclaimed(out), err
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/system/`
Expected: `ok`.

Run: `timeout 600 go test ./...`, `timeout 120 gofmt -l internal cmd`, `timeout 300 go vet ./...`
Expected: every package `ok`; gofmt and vet print nothing.

- [ ] **Step 6: Commit**

```bash
timeout 30 git add internal/system/disk.go internal/system/disk_test.go internal/system/testdata
timeout 30 git commit -m "feat(system): read Docker disk usage and build cache"
```

### Task 4: Storage data model

**Files:**
- Create: `internal/model/storage.go`
- Test: `internal/model/storage_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: C4.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `internal/model/storage_test.go`:

```go
package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestStorageJSON(t *testing.T) {
	now := time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)
	st := Storage{
		Toolchains:     []Toolchain{{Tool: "node", Version: "22.11.0", Arch: "x64", Path: "/p", Bytes: 1, InstalledAt: now}},
		OtherToolCache: []Folder{{Name: "PyPy", Bytes: 2}},
		PackageCaches:  []PackageCache{{Name: "npm", Label: "npm", Paths: []string{"/h/.npm"}, Present: true, Bytes: 3, Files: 4, LastWritten: &now}},
		Docker: DockerDisk{
			Rows:            []DockerRow{{Type: "Images", Count: 9, Active: 2, Bytes: 5, Reclaimable: 6}},
			BuildCacheTypes: []BuildCacheType{{Type: "regular", Count: 1, Bytes: 7, Reclaimable: 8}},
			DiskPct:         61,
		},
		MeasuredAt:   &now,
		Measuring:    true,
		MeasureError: "x",
		Operations: Operations{
			Current: &Operation{ID: "o1", Kind: "install", Target: "node 22", StartedAt: now, Progress: "extracting"},
			Queued:  2,
			Recent:  []Operation{{ID: "o0", Kind: "clear", Target: "npm", StartedAt: now, FinishedAt: &now, Outcome: "ok", Message: "cleared npm"}},
		},
		LastPrune: &LastPrune{Trigger: "manual", Scope: "standard", StartedAt: now, FinishedAt: &now, Outcome: "ok",
			Steps: []PruneStep{{Name: "dangling images", Freed: 9}, {Name: "disk usage", Error: "df failed"}}},
	}
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"toolchains":[{"tool":"node","version":"22.11.0","arch":"x64","path":"/p","bytes":1,"installed_at":"2026-10-06T14:00:00Z"}],` +
		`"other_tool_cache":[{"name":"PyPy","bytes":2}],` +
		`"package_caches":[{"name":"npm","label":"npm","paths":["/h/.npm"],"present":true,"bytes":3,"files":4,"last_written":"2026-10-06T14:00:00Z"}],` +
		`"docker":{"rows":[{"type":"Images","count":9,"active":2,"bytes":5,"reclaimable":6}],"build_cache_types":[{"type":"regular","count":1,"bytes":7,"reclaimable":8}],"disk_pct":61},` +
		`"measured_at":"2026-10-06T14:00:00Z","measuring":true,"measure_error":"x",` +
		`"operations":{"current":{"id":"o1","kind":"install","target":"node 22","started_at":"2026-10-06T14:00:00Z","progress":"extracting"},"queued":2,` +
		`"recent":[{"id":"o0","kind":"clear","target":"npm","started_at":"2026-10-06T14:00:00Z","finished_at":"2026-10-06T14:00:00Z","outcome":"ok","message":"cleared npm"}]},` +
		`"last_prune":{"trigger":"manual","scope":"standard","started_at":"2026-10-06T14:00:00Z","finished_at":"2026-10-06T14:00:00Z","outcome":"ok",` +
		`"steps":[{"name":"dangling images","freed":9},{"name":"disk usage","freed":0,"error":"df failed"}]}}`
	if string(b) != want {
		t.Fatalf("json\n got %s\nwant %s", b, want)
	}
}

func TestOperationsWithoutCurrentAndNoPrune(t *testing.T) {
	b, err := json.Marshal(Storage{Operations: Operations{Recent: []Operation{}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"current":null`, `"recent":[]`, `"last_prune":null`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("missing %s in %s", want, b)
		}
	}
}

func TestInstallRequestJSON(t *testing.T) {
	for req, want := range map[InstallRequest]string{
		{Tool: "node", Version: "22"}: `{"tool":"node","version":"22"}`,
		{Preset: "popular"}:           `{"preset":"popular"}`,
	} {
		if b, _ := json.Marshal(req); string(b) != want {
			t.Errorf("%+v → %s", req, b)
		}
	}
	if b, _ := json.Marshal(ToolchainChoice{Spec: "21", Version: "21.0.8+9", LTS: true}); string(b) != `{"spec":"21","version":"21.0.8+9","lts":true}` {
		t.Errorf("choice %s", b)
	}
}

func TestHumanBytes(t *testing.T) {
	for n, want := range map[int64]string{
		0:             "0 B",
		999:           "999 B",
		1000:          "1.0 kB",
		1234567:       "1.2 MB",
		180000000:     "180.0 MB",
		6571000000:    "6.6 GB",
		1500000000000: "1.5 TB",
	} {
		if got := HumanBytes(n); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/model/`
Expected: FAIL to compile — `undefined: Storage`, `undefined: HumanBytes` and the other types.

- [ ] **Step 3: Write the implementation**

Create `internal/model/storage.go`:

```go
package model

import (
	"fmt"
	"time"
)

// Storage is what the runner LXC keeps on disk between jobs, served by GET /storage.
type Storage struct {
	Toolchains     []Toolchain    `json:"toolchains"`
	OtherToolCache []Folder       `json:"other_tool_cache"`
	PackageCaches  []PackageCache `json:"package_caches"`
	Docker         DockerDisk     `json:"docker"`
	MeasuredAt     *time.Time     `json:"measured_at,omitempty"`
	Measuring      bool           `json:"measuring"`
	MeasureError   string         `json:"measure_error,omitempty"`
	Operations     Operations     `json:"operations"`
	LastPrune      *LastPrune     `json:"last_prune"`
}

// Toolchain is one installed version in the tool cache.
type Toolchain struct {
	Tool        string    `json:"tool"`
	Version     string    `json:"version"`
	Arch        string    `json:"arch"`
	Path        string    `json:"path"`
	Bytes       int64     `json:"bytes"`
	InstalledAt time.Time `json:"installed_at"`
}

// Folder is a tool cache folder no installer owns, such as PyPy.
type Folder struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

type PackageCache struct {
	Name        string     `json:"name"`
	Label       string     `json:"label"`
	Paths       []string   `json:"paths"`
	Present     bool       `json:"present"`
	Bytes       int64      `json:"bytes"`
	Files       int64      `json:"files"`
	LastWritten *time.Time `json:"last_written,omitempty"`
}

type DockerDisk struct {
	Rows            []DockerRow      `json:"rows"`
	BuildCacheTypes []BuildCacheType `json:"build_cache_types"`
	DiskPct         int              `json:"disk_pct"`
}

type DockerRow struct {
	Type        string `json:"type"`
	Count       int    `json:"count"`
	Active      int    `json:"active"`
	Bytes       int64  `json:"bytes"`
	Reclaimable int64  `json:"reclaimable"`
}

type BuildCacheType struct {
	Type        string `json:"type"`
	Count       int    `json:"count"`
	Bytes       int64  `json:"bytes"`
	Reclaimable int64  `json:"reclaimable"`
}

type Operations struct {
	Current *Operation  `json:"current"`
	Queued  int         `json:"queued"`
	Recent  []Operation `json:"recent"` // newest first
}

// Operation is a queued toolchain install or removal, or a cache clear.
type Operation struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"` // install, remove, clear
	Target     string     `json:"target"`
	StartedAt  time.Time  `json:"started_at"`
	Progress   string     `json:"progress,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Outcome    string     `json:"outcome,omitempty"` // ok, failed, refused, interrupted, skipped
	Message    string     `json:"message,omitempty"`
}

// LastPrune is the newest manual or automatic prune; Outcome is empty while it runs.
type LastPrune struct {
	Trigger    string      `json:"trigger"` // auto, manual
	Scope      string      `json:"scope"`   // a prune scope, or auto
	StartedAt  time.Time   `json:"started_at"`
	FinishedAt *time.Time  `json:"finished_at,omitempty"`
	Outcome    string      `json:"outcome,omitempty"` // ok, errors, interrupted
	Steps      []PruneStep `json:"steps"`
}

type PruneStep struct {
	Name  string `json:"name"`
	Freed int64  `json:"freed"`
	Error string `json:"error,omitempty"`
}

// ToolchainChoice is one entry GET /toolchains/available offers: Spec is
// what POST /toolchains takes, Version what a picker shows.
type ToolchainChoice struct {
	Spec    string `json:"spec"`
	Version string `json:"version"`
	LTS     bool   `json:"lts,omitempty"`
}

// InstallRequest is POST /toolchains: a tool and version, or a preset.
type InstallRequest struct {
	Tool    string `json:"tool,omitempty"`
	Version string `json:"version,omitempty"`
	Preset  string `json:"preset,omitempty"`
}

// HumanBytes formats n in decimal units, as Docker does.
func HumanBytes(n int64) string {
	units := []string{"B", "kB", "MB", "GB", "TB", "PB"}
	f, i := float64(n), 0
	for f >= 1000 && i < len(units)-1 {
		f /= 1000
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	return fmt.Sprintf("%.1f %s", f, units[i])
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/model/`
Expected: `ok`.

Run: `timeout 600 go test ./...`, `timeout 120 gofmt -l internal cmd`, `timeout 300 go vet ./...`
Expected: every package `ok`; gofmt and vet print nothing.

- [ ] **Step 5: Commit**

```bash
timeout 30 git add internal/model/storage.go internal/model/storage_test.go
timeout 30 git commit -m "feat(model): add the storage snapshot types"
```

### Task 5: Runner disk seam and busy count

**Files:**
- Modify: `internal/runner/manager.go` (imports, new `Disk` interface, `Manager` field, scope names, `BusyError`, `BusyCount`)
- Modify: `internal/runner/fakes_test.go` (`fakeDocker` methods, `newHarness`, `restart`)
- Test: `internal/runner/busy_test.go`

**Interfaces:**
- Consumes: C3 `system.DiskRow`, `system.CacheTypeUsage`, `system.Docker`'s four new methods.
- Produces: C5.

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing tests**

Create `internal/runner/busy_test.go`:

```go
package runner

import (
	"os"
	"testing"

	"github.com/darkraise/ghr/internal/sched"
)

var _ Disk = (*fakeDocker)(nil)

// addInstance records an instance in state, with its directory, as a spawn would.
func addInstance(t *testing.T, h *harness, id string, state sched.State) {
	t.Helper()
	h.m.mu.Lock()
	h.m.insts[id] = &instance{Meta: Meta{ID: id, Repo: "darkcloud"}, State: state, StateSince: h.now}
	h.m.mu.Unlock()
	if err := os.MkdirAll(h.m.instanceDir(id), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestBusyCountCountsAHookRecordedJob(t *testing.T) {
	h := newHarness(t)
	addInstance(t, h, "aaaaaa", sched.Idle)
	addInstance(t, h, "bbbbbb", sched.Idle)
	addInstance(t, h, "cccccc", sched.Busy)
	if n := h.m.BusyCount(); n != 1 {
		t.Fatalf("before the hook: %d busy", n)
	}
	h.writeJob(t, "aaaaaa", 55)
	if n := h.m.BusyCount(); n != 2 {
		t.Fatalf("after the hook: %d busy", n)
	}
	h.m.mu.Lock()
	confirmed := h.m.insts["aaaaaa"].JobConfirmed
	h.m.mu.Unlock()
	if confirmed {
		t.Fatal("BusyCount must not wait for GitHub to confirm the job")
	}
}

func TestBusyCountIgnoresCleaningInstances(t *testing.T) {
	h := newHarness(t)
	addInstance(t, h, "aaaaaa", sched.Cleaning)
	h.writeJob(t, "aaaaaa", 55)
	if n := h.m.BusyCount(); n != 0 {
		t.Fatalf("%d busy", n)
	}
}

func TestBusyErrorMessage(t *testing.T) {
	if got := (BusyError{N: 2}).Error(); got != "refused: 2 jobs running" {
		t.Fatalf("message %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/runner/ -run 'Busy'`
Expected: FAIL to compile — `undefined: Disk`, `h.m.BusyCount undefined`, `undefined: BusyError`.

- [ ] **Step 3: Write the implementation**

In `internal/runner/manager.go`:

1. Add `"fmt"` to the import block, after `"errors"`.

2. Directly after the closing brace of the `Docker` interface, add:

```go
// Disk is Docker's disk reporting and the prunes only the prune scopes run.
type Disk interface {
	DiskUsage(ctx context.Context) ([]system.DiskRow, error)
	BuildCacheUsage(ctx context.Context) ([]system.CacheTypeUsage, error)
	PruneAllBuildCache(ctx context.Context) (string, error)
	PruneUnusedVolumes(ctx context.Context) (string, error)
}

var _ Disk = system.Docker{}
```

3. In the `Manager` struct, directly after the line `Docker  Docker`, add the line:

```go
	Disk    Disk // the daemon sets system.Docker
```

4. Directly after the `ErrClosed` declaration (`var ErrClosed = errors.New("ghr is shutting down")`), add:

```go
// Prune scopes, as POST /prune/{scope} names them.
const (
	ScopeStandard       = "standard"
	ScopeBuildCacheKeep = "build-cache-keep"
	ScopeBuildCacheAll  = "build-cache-all"
	ScopeDanglingImages = "dangling-images"
	ScopeUnusedVolumes  = "unused-volumes"
)

var Scopes = []string{ScopeStandard, ScopeBuildCacheKeep, ScopeBuildCacheAll, ScopeDanglingImages, ScopeUnusedVolumes}

var ErrUnknownScope = errors.New("unknown prune scope")

// BusyError refuses an action while runners have jobs.
type BusyError struct{ N int }

func (e BusyError) Error() string { return fmt.Sprintf("refused: %d jobs running", e.N) }
```

5. Directly after the `Maintenance` method, add:

```go
// BusyCount is the number of runners with a job. It reads job.json first, so
// a job the hook has just recorded counts before GitHub confirms it.
// readJobFiles takes mu itself, so the count takes it afterwards.
func (m *Manager) BusyCount() int {
	m.readJobFiles()
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, i := range m.insts {
		if i.State == sched.Busy {
			n++
		}
	}
	return n
}
```

Run `timeout 60 gofmt -w internal/runner/manager.go` to realign the struct fields.

In `internal/runner/fakes_test.go`:

1. Directly after the `PruneDanglingImages` method, add:

```go
func (f *fakeDocker) PruneAllBuildCache(ctx context.Context) (string, error) {
	f.mu.Lock()
	f.prunes = append(f.prunes, "all")
	f.mu.Unlock()
	if err := f.wait(ctx, "PruneAllBuildCache"); err != nil {
		return "", err
	}
	if err := f.err("PruneAllBuildCache"); err != nil {
		return "", err
	}
	return "3GB", nil
}
func (f *fakeDocker) PruneUnusedVolumes(ctx context.Context) (string, error) {
	f.mu.Lock()
	f.prunes = append(f.prunes, "volumes")
	f.mu.Unlock()
	if err := f.wait(ctx, "PruneUnusedVolumes"); err != nil {
		return "", err
	}
	if err := f.err("PruneUnusedVolumes"); err != nil {
		return "", err
	}
	return "8.65GB", nil
}
func (f *fakeDocker) DiskUsage(context.Context) ([]system.DiskRow, error) {
	return nil, f.err("DiskUsage")
}
func (f *fakeDocker) BuildCacheUsage(context.Context) ([]system.CacheTypeUsage, error) {
	return nil, f.err("BuildCacheUsage")
}
```

2. In `newHarness`, change the line `GH:     h.gh, SD: h.sd, Docker: h.docker, Host: h.host,` to:

```go
		GH:     h.gh, SD: h.sd, Docker: h.docker, Disk: h.docker, Host: h.host,
```

3. In `restart`, change `Docker: old.Docker, Host: old.Host,` to `Docker: old.Docker, Disk: old.Disk, Host: old.Host,`.

Run `timeout 60 gofmt -w internal/runner/fakes_test.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/runner/`
Expected: `ok`.

Run: `timeout 600 go test ./...`, `timeout 120 gofmt -l internal cmd`, `timeout 300 go vet ./...`
Expected: every package `ok`; gofmt and vet print nothing.

- [ ] **Step 5: Commit**

```bash
timeout 30 git add internal/runner/manager.go internal/runner/fakes_test.go internal/runner/busy_test.go
timeout 30 git commit -m "feat(runner): add the disk seam and busy count"
```

### Task 6: Prune scopes and last prune

**Files:**
- Modify: `internal/runner/manager.go` (`Manager` fields, `StartPrune`, new `StartPruneScope`, `LastPrune`)
- Modify: `internal/runner/cleanup.go` (imports, `checkDisk`, `forcedPrune`, new helpers)
- Test: `internal/runner/prune_scope_test.go`

**Interfaces:**
- Consumes: C3 `system.ParseSize`; C4 `model.LastPrune`, `model.PruneStep`; C5 scopes, `BusyError`, `BusyCount`, `Disk`; the test helpers `addInstance` (Task 5), `holdOn` and `waitOrFail` (`internal/runner/maintenance_test.go:15-43`).
- Produces: C6.

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing tests**

Create `internal/runner/prune_scope_test.go`:

```go
package runner

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/sched"
)

func countPruneDone(h *harness) *atomic.Int32 {
	var n atomic.Int32
	h.m.PruneDone = func() { n.Add(1) }
	return &n
}

func stepNames(lp *model.LastPrune) string {
	var out []string
	for _, s := range lp.Steps {
		out = append(out, s.Name)
	}
	return strings.Join(out, ",")
}

func TestPruneScopesRunOnlyTheirSteps(t *testing.T) {
	for _, tc := range []struct{ scope, prunes, steps string }{
		{ScopeStandard, "keep=K,images", "build cache to K,dangling images,history and logs past retention"},
		{ScopeBuildCacheKeep, "keep=K", "build cache to K"},
		{ScopeBuildCacheAll, "all", "all build cache"},
		{ScopeDanglingImages, "images", "dangling images"},
		{ScopeUnusedVolumes, "volumes", "unused volumes"},
	} {
		h := newHarness(t)
		keep := h.cfg.BuildCacheKeep
		done := countPruneDone(h)
		if err := h.m.StartPruneScope(tc.scope); err != nil {
			t.Fatalf("%s: %v", tc.scope, err)
		}
		h.m.Wait()
		if got, want := h.docker.pruneList(), strings.ReplaceAll(tc.prunes, "K", keep); got != want {
			t.Errorf("%s: prunes %q, want %q", tc.scope, got, want)
		}
		lp := h.m.LastPrune()
		if lp == nil {
			t.Fatalf("%s: no last prune", tc.scope)
		}
		if lp.Trigger != "manual" || lp.Scope != tc.scope || lp.Outcome != "ok" || lp.FinishedAt == nil || !lp.StartedAt.Equal(h.now) {
			t.Errorf("%s: last prune %+v", tc.scope, lp)
		}
		if got, want := stepNames(lp), strings.ReplaceAll(tc.steps, "K", keep); got != want {
			t.Errorf("%s: steps %q, want %q", tc.scope, got, want)
		}
		if n := done.Load(); n != 1 {
			t.Errorf("%s: PruneDone ran %d times", tc.scope, n)
		}
	}
}

func TestPruneStepsRecordBytesFreed(t *testing.T) {
	h := newHarness(t)
	if err := h.m.StartPruneScope(ScopeStandard); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	want := []model.PruneStep{
		{Name: "build cache to " + h.cfg.BuildCacheKeep, Freed: 5000000000},
		{Name: "dangling images", Freed: 200000000},
		{Name: "history and logs past retention"},
	}
	if got := h.m.LastPrune().Steps; !reflect.DeepEqual(got, want) {
		t.Fatalf("steps %+v", got)
	}
}

func TestAFailedPruneStepKeepsItsError(t *testing.T) {
	h := newHarness(t)
	h.docker.setErr("PruneUnusedVolumes", errors.New("boom"))
	if err := h.m.StartPruneScope(ScopeUnusedVolumes); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	lp := h.m.LastPrune()
	if lp.Outcome != "errors" || !reflect.DeepEqual(lp.Steps, []model.PruneStep{{Name: "unused volumes", Error: "boom"}}) {
		t.Fatalf("last prune %+v", lp)
	}
}

func TestUnknownPruneScope(t *testing.T) {
	h := newHarness(t)
	if err := h.m.StartPruneScope("everything"); !errors.Is(err, ErrUnknownScope) {
		t.Fatalf("err %v", err)
	}
	if h.m.LastPrune() != nil || h.docker.pruneList() != "" {
		t.Fatal("an unknown scope ran or recorded a prune")
	}
}

func TestUnusedVolumesRefusedWhileARunnerIsBusy(t *testing.T) {
	h := newHarness(t)
	addInstance(t, h, "aaaaaa", sched.Idle)
	h.writeJob(t, "aaaaaa", 55)
	err := h.m.StartPruneScope(ScopeUnusedVolumes)
	var busy BusyError
	if !errors.As(err, &busy) || busy.N != 1 || err.Error() != "refused: 1 jobs running" {
		t.Fatalf("err %v", err)
	}
	h.m.Wait()
	if h.docker.pruneList() != "" || h.m.LastPrune() != nil {
		t.Fatal("a refused prune ran or recorded")
	}
	if err := h.m.StartPruneScope(ScopeDanglingImages); err != nil {
		t.Fatalf("other scopes run while busy: %v", err)
	}
	h.m.Wait()
	h.m.Close()
	if err := h.m.StartPruneScope(ScopeUnusedVolumes); !errors.Is(err, ErrClosed) {
		t.Fatalf("after Close, with a busy runner: %v", err)
	}
}

func TestCheckDiskCutShortByShutdownIsInterrupted(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{99}
	entered, _ := holdOn(t, h, "PruneBuildCacheOlderThan")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.m.checkDisk(ctx, h.cfg)
		close(done)
	}()
	waitOrFail(t, entered, "the automatic prune")
	cancel()
	waitOrFail(t, done, "checkDisk to return")
	if lp := h.m.LastPrune(); lp == nil || lp.Outcome != "interrupted" {
		t.Fatalf("last prune %+v", lp)
	}
}

func TestPruneDoneFiresWhenAPruneIsInterrupted(t *testing.T) {
	h := newHarness(t)
	done := countPruneDone(h)
	entered, _ := holdOn(t, h, "PruneAllBuildCache")
	if err := h.m.StartPruneScope(ScopeBuildCacheAll); err != nil {
		t.Fatal(err)
	}
	waitOrFail(t, entered, "the build cache prune")
	h.m.Close()
	h.m.Wait()
	if lp := h.m.LastPrune(); lp.Outcome != "interrupted" || done.Load() != 1 {
		t.Fatalf("last prune %+v, PruneDone %d", lp, done.Load())
	}
}

func TestCheckDiskRecordsAnAutomaticPrune(t *testing.T) {
	h := newHarness(t)
	done := countPruneDone(h)
	h.docker.usage = []int{99, 99, 50}
	h.m.checkDisk(context.Background(), h.cfg)
	lp := h.m.LastPrune()
	if lp == nil || lp.Trigger != "auto" || lp.Scope != "auto" || lp.Outcome != "ok" || lp.FinishedAt == nil {
		t.Fatalf("last prune %+v", lp)
	}
	want := []model.PruneStep{
		{Name: "build cache older than 72h", Freed: 1000000000},
		{Name: "build cache to " + h.cfg.BuildCacheKeep, Freed: 5000000000},
		{Name: "dangling images", Freed: 200000000},
	}
	if !reflect.DeepEqual(lp.Steps, want) {
		t.Fatalf("steps %+v", lp.Steps)
	}
	if n := done.Load(); n != 1 {
		t.Fatalf("PruneDone ran %d times", n)
	}
}

func TestCheckDiskUnderTheHighWaterMarkRecordsNothing(t *testing.T) {
	h := newHarness(t)
	done := countPruneDone(h)
	h.docker.usage = []int{40}
	h.m.checkDisk(context.Background(), h.cfg)
	if h.m.LastPrune() != nil || done.Load() != 0 {
		t.Fatalf("last prune %+v, PruneDone %d", h.m.LastPrune(), done.Load())
	}
}

func TestCheckDiskRecordsFailedReadings(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{99, -1}
	h.m.checkDisk(context.Background(), h.cfg)
	lp := h.m.LastPrune()
	if lp.Outcome != "errors" || stepNames(lp) != "build cache older than 72h,disk usage,dangling images,disk usage" {
		t.Fatalf("last prune %+v", lp)
	}
	if lp.Steps[1].Error != "df failed" {
		t.Fatalf("step %+v", lp.Steps[1])
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/runner/ -run 'Prune|CheckDisk'`
Expected: FAIL to compile — `h.m.PruneDone undefined`, `h.m.StartPruneScope undefined`, `h.m.LastPrune undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/runner/manager.go`:

1. In the `Manager` struct, directly after the `Fetch` field and its comment, add:

```go
	// PruneDone, when set, is called after every manual prune and after an
	// automatic prune that pruned.
	PruneDone func()
```

2. In the same struct, directly after the line declaring `maint`, add:

```go
	lastPruneRec   *model.LastPrune        // the newest manual or automatic prune; guarded by mu
```

3. Replace the whole `StartPrune` function and its doc comment with:

```go
// StartPrune starts a standard manual prune in the background.
func (m *Manager) StartPrune() error { return m.StartPruneScope(ScopeStandard) }

// StartPruneScope starts a manual prune of one scope in the background. The
// caller returns before it finishes; Wait waits for it and Close interrupts
// it. unused-volumes is refused while a runner is busy: a job's unnamed
// volume is unreferenced between the steps that mount it.
func (m *Manager) StartPruneScope(scope string) error {
	if !slices.Contains(Scopes, scope) {
		return fmt.Errorf("%w %q", ErrUnknownScope, scope)
	}
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if closed {
		return ErrClosed
	}
	if scope == ScopeUnusedVolumes {
		if n := m.BusyCount(); n > 0 {
			return BusyError{N: n}
		}
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return ErrClosed
	}
	if m.upd.running {
		m.mu.Unlock()
		return ErrUpdateRunning
	}
	if m.pruning {
		m.mu.Unlock()
		return ErrPruneRunning
	}
	now := m.Now()
	m.pruning = true
	m.maint.Running = true
	m.maint.LastStarted = &now
	m.lastPruneRec = &model.LastPrune{Trigger: "manual", Scope: scope, StartedAt: now, Steps: []model.PruneStep{}}
	ctx := m.maintCtx
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		m.forcedPrune(ctx, scope)
	}()
	return nil
}
```

4. Directly after the `Maintenance` method, add:

```go
// LastPrune is the newest manual or automatic prune, nil before the first.
func (m *Manager) LastPrune() *model.LastPrune {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastPruneRec == nil {
		return nil
	}
	lp := *m.lastPruneRec
	lp.Steps = slices.Clone(lp.Steps)
	return &lp
}
```

Run `timeout 60 gofmt -w internal/runner/manager.go`.

In `internal/runner/cleanup.go`:

1. Add `"github.com/darkraise/ghr/internal/system"` to the import block, after `"github.com/darkraise/ghr/internal/model"`.

2. Replace the whole `checkDisk` function (its doc comment stays) with:

```go
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
	started := m.Now()
	m.mu.Lock()
	m.lastPruneRec = &model.LastPrune{Trigger: "auto", Scope: "auto", StartedAt: started, Steps: []model.PruneStep{}}
	m.mu.Unlock()
	defer m.pruneDone()
	var notes []string
	freedOld, err := m.Docker.PruneBuildCacheOlderThan(ctx, 72)
	m.recordStep("build cache older than 72h", freedOld, err)
	if err != nil {
		notes = append(notes, fmt.Sprintf("prune build cache older than 72h failed: %v", err))
		freedOld = "0B"
	}
	freedKeep := "0B"
	if mid, err := m.Docker.DataRootUsage(ctx); err != nil {
		m.recordStep("disk usage", "", err)
		notes = append(notes, fmt.Sprintf("disk usage after the 72h prune failed: %v", err))
	} else if mid > cfg.DiskHighWater {
		freedKeep, err = m.Docker.PruneBuildCacheTo(ctx, cfg.BuildCacheKeep)
		m.recordStep("build cache to "+cfg.BuildCacheKeep, freedKeep, err)
		if err != nil {
			notes = append(notes, fmt.Sprintf("prune build cache to %s failed: %v", cfg.BuildCacheKeep, err))
			freedKeep = "0B"
		}
	}
	freedImages, err := m.Docker.PruneDanglingImages(ctx)
	m.recordStep("dangling images", freedImages, err)
	if err != nil {
		notes = append(notes, fmt.Sprintf("prune dangling images failed: %v", err))
		freedImages = "0B"
	}
	nowPct := "unknown"
	if after, err := m.Docker.DataRootUsage(ctx); err != nil {
		m.recordStep("disk usage", "", err)
		notes = append(notes, fmt.Sprintf("disk usage after pruning failed: %v", err))
	} else {
		m.setDisk(after)
		nowPct = strconv.Itoa(after) + "%"
	}
	outcome := "ok"
	switch {
	case ctx.Err() != nil:
		outcome = "interrupted"
	case len(notes) > 0:
		outcome = "errors"
	}
	m.finishPruneRecord(outcome)
	msg := fmt.Sprintf("disk %d%% > high-water %d%% — pruned build cache (%s older than 72h, %s to %s) and dangling images (%s); now %s",
		pct, cfg.DiskHighWater, freedOld, freedKeep, cfg.BuildCacheKeep, freedImages, nowPct)
	if len(notes) > 0 {
		msg += "; " + strings.Join(notes, "; ")
	}
	m.Events.Add("warn", "", "%s", msg)
}

// recordStep appends one step to the prune record in progress; freed is
// Docker's "Total reclaimed space", 0 when absent or unreadable.
func (m *Manager) recordStep(name, freed string, err error) {
	st := model.PruneStep{Name: name}
	if err != nil {
		st.Error = err.Error()
	} else if n, perr := system.ParseSize(freed); perr == nil {
		st.Freed = n
	}
	m.mu.Lock()
	if m.lastPruneRec != nil {
		m.lastPruneRec.Steps = append(m.lastPruneRec.Steps, st)
	}
	m.mu.Unlock()
}

func (m *Manager) finishPruneRecord(outcome string) {
	now := m.Now()
	m.mu.Lock()
	if m.lastPruneRec != nil {
		m.lastPruneRec.FinishedAt = &now
		m.lastPruneRec.Outcome = outcome
	}
	m.mu.Unlock()
}

func (m *Manager) pruneDone() {
	if m.PruneDone != nil {
		m.PruneDone()
	}
}
```

3. Replace the whole `forcedPrune` function and its doc comment with:

```go
// forcedPrune is a manual prune of one scope. standard prunes build cache
// down to build_cache_keep and dangling images, then history and logs past
// retention; every scope ends with a fresh disk reading. Unlike checkDisk it
// ignores disk_high_water.
func (m *Manager) forcedPrune(ctx context.Context, scope string) {
	cfg := m.Config()
	failed := false
	step := func(name string, fn func() (string, error)) {
		if ctx.Err() != nil {
			return
		}
		freed, err := fn()
		m.recordStep(name, freed, err)
		if err != nil {
			failed = true
			m.Events.Add("warn", "", "prune: %s failed: %v", name, err)
			return
		}
		m.Events.Add("info", "", "prune: %s freed %s", name, freed)
	}
	keep := func() (string, error) { return m.Docker.PruneBuildCacheTo(ctx, cfg.BuildCacheKeep) }
	images := func() (string, error) { return m.Docker.PruneDanglingImages(ctx) }
	switch scope {
	case ScopeStandard:
		step("build cache to "+cfg.BuildCacheKeep, keep)
		step("dangling images", images)
	case ScopeBuildCacheKeep:
		step("build cache to "+cfg.BuildCacheKeep, keep)
	case ScopeBuildCacheAll:
		step("all build cache", func() (string, error) { return m.Disk.PruneAllBuildCache(ctx) })
	case ScopeDanglingImages:
		step("dangling images", images)
	case ScopeUnusedVolumes:
		step("unused volumes", func() (string, error) { return m.Disk.PruneUnusedVolumes(ctx) })
	}
	if scope == ScopeStandard && ctx.Err() == nil {
		if m.prune(cfg, m.Now()) {
			m.recordStep("history and logs past retention", "0B", nil)
			m.Events.Add("info", "", "prune: history and logs past retention removed")
		} else {
			failed = true
			m.recordStep("history and logs past retention", "", errors.New("failed; see the warnings before this event"))
		}
	}
	if ctx.Err() == nil {
		if pct, err := m.Docker.DataRootUsage(ctx); err != nil {
			failed = true
			m.recordStep("disk usage", "", err)
			m.Events.Add("warn", "", "prune: disk usage: %v", err)
		} else {
			m.setDisk(pct)
			m.Events.Add("info", "", "prune: disk %d%% used", pct)
		}
	}
	outcome, level, msg := "ok", "ok", "prune finished"
	switch {
	case ctx.Err() != nil:
		outcome, level, msg = "interrupted", "warn", "prune interrupted by shutdown"
	case failed:
		outcome, level, msg = "errors", "warn", "prune finished with errors"
	}
	now := m.Now()
	m.mu.Lock()
	m.pruning = false
	m.maint.Running = false
	m.maint.LastFinished = &now
	m.maint.LastOutcome = outcome
	// Finished under the same lock that releases the reservation: once it is
	// released, the next prune replaces lastPruneRec.
	if m.lastPruneRec != nil {
		m.lastPruneRec.FinishedAt = &now
		m.lastPruneRec.Outcome = outcome
	}
	m.mu.Unlock()
	m.Events.Add(level, "", "%s", msg)
	m.pruneDone()
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/runner/`
Expected: `ok` — the new tests and the existing `maintenance_test.go`, `cleanup_test.go` and `update_start_test.go` prune tests all pass.

Run: `timeout 600 go test ./...`, `timeout 120 gofmt -l internal cmd`, `timeout 300 go vet ./...`
Expected: every package `ok`; gofmt and vet print nothing.

- [ ] **Step 5: Commit**

```bash
timeout 30 git add internal/runner/manager.go internal/runner/cleanup.go internal/runner/prune_scope_test.go
timeout 30 git commit -m "feat(runner): add prune scopes and the last prune"
```

### Task 7: Package caches and clears

**Files:**
- Create: `internal/storage/caches.go`, `internal/storage/own_unix.go`, `internal/storage/own_other.go`
- Test: `internal/storage/caches_test.go`, `internal/storage/own_unix_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: C7.

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

The risk is data loss: the daemon runs this as root against `/home/ghrunner`, so every path it deletes must be one it renamed aside itself, and a symlink must be removed, never followed.

- [ ] **Step 1: Write the failing tests**

Create `internal/storage/caches_test.go`:

```go
package storage

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestCacheTable(t *testing.T) {
	want := map[string]string{
		"nuget": ".nuget/packages", "npm": ".npm", "pnpm": ".local/share/pnpm/store", "yarn": ".cache/yarn",
		"pip": ".cache/pip", "gomod": "go/pkg/mod", "gobuild": ".cache/go-build", "maven": ".m2/repository",
		"gradle": ".gradle/caches", "cargo": ".cargo/registry",
	}
	if len(Caches) != len(want) {
		t.Fatalf("%d caches", len(Caches))
	}
	for _, c := range Caches {
		if want[c.Name] != c.Paths[0] {
			t.Errorf("%s: first path %q", c.Name, c.Paths[0])
		}
	}
	if c, ok := cacheByName("gradle"); !ok || c.Label != "Gradle" || len(c.Paths) != 2 || c.Paths[1] != ".gradle/wrapper/dists" {
		t.Fatalf("gradle %+v", c)
	}
	if _, ok := cacheByName("bogus"); ok {
		t.Fatal("bogus found")
	}
}

func TestPresent(t *testing.T) {
	home := t.TempDir()
	c, _ := cacheByName("cargo")
	if c.present(home) {
		t.Fatal("present with no paths")
	}
	if err := os.MkdirAll(filepath.Join(home, ".cargo", "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !c.present(home) {
		t.Fatal("not present with its second path")
	}
	if got := c.abs(home); got[0] != filepath.Join(home, ".cargo", "registry") {
		t.Fatalf("abs %v", got)
	}
}

func TestClearPathLeavesAnEmptyDirectoryWithTheOldMode(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".nuget", "packages")
	writeFile(t, filepath.Join(dir, "newtonsoft.json", "13.0.3", "lib.dll"), "x")
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := clearPath(dir, "op1"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		t.Fatalf("not recreated: %v", err)
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Fatalf("not empty: %v", got)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o750 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	if got := entries(t, filepath.Join(home, ".nuget")); len(got) != 1 || got[0] != "packages" {
		t.Fatalf("left behind: %v", got)
	}
}

func TestClearPathDeletesReadOnlyFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only files block deletion on Windows; the daemon runs on Linux")
	}
	home := t.TempDir()
	dir := filepath.Join(home, "go", "pkg", "mod")
	mod := filepath.Join(dir, "golang.org", "x", "text@v0.30.0")
	writeFile(t, filepath.Join(mod, "go.mod"), "module golang.org/x/text")
	if err := os.Chmod(filepath.Join(mod, "go.mod"), 0o444); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		// Go writes module directories read-only too; only root deletes through them.
		if err := os.Chmod(mod, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	if err := clearPath(dir, "op1"); err != nil {
		t.Fatal(err)
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Fatalf("not empty: %v", got)
	}
}

func TestClearPathRemovesASymlinkNotItsTarget(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "keep.txt"), "keep")
	home := t.TempDir()
	dir := filepath.Join(home, ".npm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if err := clearPath(dir, "op1"); err != nil {
		t.Fatal(err)
	}
	if got := entries(t, dir); len(got) != 0 {
		t.Fatalf("not empty: %v", got)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("the link's target was deleted: %v", err)
	}
}

func TestClearPathOnASymlinkedCacheRemovesOnlyTheLink(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "keep.txt"), "keep")
	home := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, ".npm")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	if err := clearPath(filepath.Join(home, ".npm"), "op1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".npm")); !os.IsNotExist(err) {
		t.Fatalf("link still there: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep.txt")); err != nil {
		t.Fatalf("the link's target was deleted: %v", err)
	}
}

func TestClearCacheRefusesASymlinkedParent(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "pip", "wheel"), "keep")
	home := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, ".cache")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	c, _ := cacheByName("pip")
	if err := clearCache(home, c, "op1"); err == nil || !strings.Contains(err.Error(), "is a symlink") {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "pip", "wheel")); err != nil {
		t.Fatalf("cleared through the link: %v", err)
	}
	writeFile(t, filepath.Join(outside, ".pip"+clearingTag+"op9", "x"), "keep")
	if err := sweepClearing(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, ".pip"+clearingTag+"op9", "x")); err != nil {
		t.Fatalf("swept through the link: %v", err)
	}
}

func TestClearPathMissingIsNothing(t *testing.T) {
	if err := clearPath(filepath.Join(t.TempDir(), "absent"), "op1"); err != nil {
		t.Fatal(err)
	}
}

func TestClearCacheClearsEveryPath(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".gradle", "caches", "modules-2", "f"), "x")
	writeFile(t, filepath.Join(home, ".gradle", "wrapper", "dists", "gradle-8.10", "g"), "x")
	c, _ := cacheByName("gradle")
	if err := clearCache(home, c, "op1"); err != nil {
		t.Fatal(err)
	}
	for _, p := range c.abs(home) {
		if got := entries(t, p); len(got) != 0 {
			t.Fatalf("%s not empty: %v", p, got)
		}
	}
}

func TestSweepClearingRemovesInterruptedClears(t *testing.T) {
	home := t.TempDir()
	left1 := filepath.Join(home, ".nuget", ".packages"+clearingTag+"op1")
	left2 := filepath.Join(home, ".cache", ".pip"+clearingTag+"op2")
	writeFile(t, filepath.Join(left1, "a"), "x")
	writeFile(t, filepath.Join(left2, "b"), "x")
	writeFile(t, filepath.Join(home, ".cache", "pip", "c"), "x")
	if err := sweepClearing(home); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{left1, left2} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Fatalf("%s survived: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".cache", "pip", "c")); err != nil {
		t.Fatalf("the live cache was touched: %v", err)
	}
}

func TestSweepClearingWithoutAHome(t *testing.T) {
	if err := sweepClearing(filepath.Join(t.TempDir(), "nobody")); err != nil {
		t.Fatal(err)
	}
}
```

Create `internal/storage/own_unix_test.go` (it runs as root in Task 16's WSL pass and skips elsewhere):

```go
//go:build unix

package storage

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestClearPathKeepsTheOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("only root can give a directory to another user")
	}
	dir := filepath.Join(t.TempDir(), ".npm")
	writeFile(t, filepath.Join(dir, "f"), "x")
	if err := os.Chown(dir, 12345, 12345); err != nil {
		t.Fatal(err)
	}
	if err := clearPath(dir, "op1"); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st := fi.Sys().(*syscall.Stat_t); st.Uid != 12345 || st.Gid != 12345 {
		t.Fatalf("owner %d:%d", st.Uid, st.Gid)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/storage/`
Expected: FAIL to compile — `undefined: Caches`, `undefined: cacheByName`, `undefined: clearPath`, `undefined: clearingTag`, `undefined: sweepClearing`, `undefined: clearCache`.

- [ ] **Step 3: Write the implementation**

Create `internal/storage/caches.go`:

```go
// Package storage measures and manages what the runner LXC keeps on disk
// between jobs: toolchains in the tool cache, package-manager caches in the
// runner home, and Docker's disk.
package storage

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Cache is one package manager's cache at its default paths, relative to the
// runner home. A workflow that relocates a cache is not seen.
type Cache struct {
	Name  string
	Label string
	Paths []string
}

var Caches = []Cache{
	{Name: "nuget", Label: "NuGet", Paths: []string{".nuget/packages"}},
	{Name: "npm", Label: "npm", Paths: []string{".npm"}},
	{Name: "pnpm", Label: "pnpm", Paths: []string{".local/share/pnpm/store", ".cache/pnpm"}},
	{Name: "yarn", Label: "Yarn", Paths: []string{".cache/yarn", ".yarn/berry/cache"}},
	{Name: "pip", Label: "pip", Paths: []string{".cache/pip"}},
	{Name: "gomod", Label: "Go modules", Paths: []string{"go/pkg/mod"}},
	{Name: "gobuild", Label: "Go build", Paths: []string{".cache/go-build"}},
	{Name: "maven", Label: "Maven", Paths: []string{".m2/repository"}},
	{Name: "gradle", Label: "Gradle", Paths: []string{".gradle/caches", ".gradle/wrapper/dists"}},
	{Name: "cargo", Label: "Cargo", Paths: []string{".cargo/registry", ".cargo/git"}},
}

func cacheByName(name string) (Cache, bool) {
	for _, c := range Caches {
		if c.Name == name {
			return c, true
		}
	}
	return Cache{}, false
}

func (c Cache) abs(home string) []string {
	out := make([]string, len(c.Paths))
	for i, p := range c.Paths {
		out[i] = filepath.Join(home, filepath.FromSlash(p))
	}
	return out
}

func (c Cache) present(home string) bool {
	for _, p := range c.abs(home) {
		if _, err := os.Lstat(p); err == nil {
			return true
		}
	}
	return false
}

const clearingTag = ".ghr-clearing-"

// clearPath empties path so a job sees either the old tree or an empty
// directory, never a half-deleted one: rename it aside (atomic), recreate the
// empty directory with the old owner and mode, then delete the renamed tree.
// The daemon runs as root, so read-only files (Go's module cache) need no
// chmod, and os.RemoveAll removes a symlink without following it. A path
// that is itself a symlink is removed and not recreated.
func clearPath(path, opID string) error {
	fi, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	aside := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+clearingTag+opID)
	if err := os.Rename(path, aside); err != nil {
		return err
	}
	if fi.IsDir() {
		if err := os.Mkdir(path, fi.Mode().Perm()); err != nil {
			return err
		}
		// Mkdir applies the umask.
		if err := os.Chmod(path, fi.Mode().Perm()); err != nil {
			return err
		}
		if err := chownLike(path, fi); err != nil {
			return err
		}
	}
	return os.RemoveAll(aside)
}

// noLinkedParent refuses a path below home when a directory between home and
// the path is a symlink: root must not rename or delete through a link a job
// planted (clearPath already removes a link at the path itself unfollowed).
func noLinkedParent(home, path string) error {
	rel, err := filepath.Rel(home, filepath.Dir(path))
	if err != nil || rel == "." {
		return err
	}
	cur := home
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symlink; not clearing through it", cur)
		}
	}
	return nil
}

// clearCache clears every existing path of c.
func clearCache(home string, c Cache, opID string) error {
	var errs []error
	for _, p := range c.abs(home) {
		if err := noLinkedParent(home, p); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := clearPath(p, opID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// sweepClearing deletes trees a clear renamed aside but did not finish
// deleting, as a daemon stop mid-clear leaves them.
func sweepClearing(home string) error {
	seen := map[string]bool{}
	var errs []error
	for _, c := range Caches {
		for _, p := range c.abs(home) {
			dir := filepath.Dir(p)
			if seen[dir] {
				continue
			}
			seen[dir] = true
			if noLinkedParent(home, p) != nil {
				continue
			}
			es, err := os.ReadDir(dir)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				errs = append(errs, err)
				continue
			}
			for _, e := range es {
				if strings.HasPrefix(e.Name(), ".") && strings.Contains(e.Name(), clearingTag) {
					if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
						errs = append(errs, err)
					}
				}
			}
		}
	}
	return errors.Join(errs...)
}
```

Create `internal/storage/own_unix.go`:

```go
//go:build unix

package storage

import (
	"os"
	"syscall"
)

// chownLike gives path the owner and group fi records, without following a symlink.
func chownLike(path string, fi os.FileInfo) error {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	return os.Lchown(path, int(st.Uid), int(st.Gid))
}

// allocated is the space fi takes on disk, as du counts it.
func allocated(fi os.FileInfo) int64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512
	}
	return fi.Size()
}
```

Create `internal/storage/own_other.go`:

```go
//go:build !unix

package storage

import "os"

// chownLike does nothing: ownership is a Linux concern, and ghr runs on Linux.
func chownLike(string, os.FileInfo) error { return nil }

func allocated(fi os.FileInfo) int64 { return fi.Size() }
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/storage/`
Expected: `ok` (on Windows the read-only test skips, and the symlink tests skip when the account cannot create symlinks).

Run: `timeout 600 go test ./...`, `timeout 120 gofmt -l internal cmd`, `timeout 300 go vet ./...`
Expected: every package `ok`; gofmt and vet print nothing.

- [ ] **Step 5: Commit**

```bash
timeout 30 git add internal/storage
timeout 30 git commit -m "feat(storage): add package caches and safe clears"
```

### Task 8: Measurement

**Files:**
- Create: `internal/storage/measure.go`
- Create: `internal/storage/fakes_test.go`
- Test: `internal/storage/measure_test.go`

**Interfaces:**
- Consumes: C2 toolchain types; C3 `system.DiskRow`, `system.CacheTypeUsage`; C4 model types; C7 `Caches`, `(Cache).abs`, `allocated`, test helper `writeFile`.
- Produces: C8.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the test fakes**

Create `internal/storage/fakes_test.go` (Tasks 9 and 10 use every fake here):

```go
package storage

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/system"
	"github.com/darkraise/ghr/internal/toolchain"
)

type fakeTools struct {
	mu        sync.Mutex
	root      string
	installed []toolchain.Installed
	other     []string
	tools     map[string]*fakeInstaller
	cleaned   int
}

func (f *fakeTools) Get(tool string) (toolchain.Installer, error) {
	if i, ok := f.tools[tool]; ok {
		return i, nil
	}
	return nil, fmt.Errorf("%w %q", toolchain.ErrUnknownTool, tool)
}

func (f *fakeTools) Available(ctx context.Context, tool string) ([]toolchain.Choice, error) {
	i, err := f.Get(tool)
	if err != nil {
		return nil, err
	}
	return i.Available(ctx)
}

func (f *fakeTools) Installed() ([]toolchain.Installed, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.installed), nil
}

func (f *fakeTools) Other() ([]string, error) { return f.other, nil }
func (f *fakeTools) Root() string             { return f.root }

func (f *fakeTools) CleanTmp() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleaned++
	return nil
}

func (f *fakeTools) cleanCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cleaned
}

// fakeInstaller resolves specs from a table. With gate set, Install waits for
// a send on gate or the end of its context; with started set, Install sends
// the version it is installing there first.
type fakeInstaller struct {
	mu       sync.Mutex
	resolve  map[string]string
	have     map[string]bool
	installs []string
	removed  []string
	gate     chan struct{}
	started  chan string
}

func newInstaller(resolve map[string]string) *fakeInstaller {
	return &fakeInstaller{resolve: resolve, have: map[string]bool{}}
}

func (f *fakeInstaller) Available(context.Context) ([]toolchain.Choice, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []toolchain.Choice
	for spec, v := range f.resolve {
		out = append(out, toolchain.Choice{Spec: spec, Version: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Spec < out[j].Spec })
	return out, nil
}

func (f *fakeInstaller) Resolve(_ context.Context, spec string) (toolchain.Release, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.resolve[spec]
	if !ok {
		return toolchain.Release{}, fmt.Errorf("no release matches %q", spec)
	}
	return toolchain.Release{Version: v, Folder: v}, nil
}

func (f *fakeInstaller) Install(ctx context.Context, rel toolchain.Release, progress func(string)) error {
	progress("downloading")
	if f.started != nil {
		f.started <- rel.Version
	}
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.have[rel.Version] {
		return toolchain.ErrAlreadyInstalled
	}
	f.have[rel.Version] = true
	f.installs = append(f.installs, rel.Version)
	return nil
}

func (f *fakeInstaller) Installed() ([]toolchain.Installed, error) { return nil, nil }

func (f *fakeInstaller) Remove(version string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, version)
	return nil
}

func (f *fakeInstaller) installList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.installs)
}

func (f *fakeInstaller) removedList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.removed)
}

// fakeDisk serves fixed Docker figures. With entered set, each DiskUsage call
// sends on it first; with gate set, it then waits for a send on gate or the
// end of its context.
type fakeDisk struct {
	mu      sync.Mutex
	rows    []system.DiskRow
	types   []system.CacheTypeUsage
	err     error
	calls   int
	entered chan struct{}
	gate    chan struct{}
}

func (f *fakeDisk) DiskUsage(ctx context.Context) ([]system.DiskRow, error) {
	f.mu.Lock()
	f.calls++
	entered, gate := f.entered, f.gate
	f.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rows, f.err
}

func (f *fakeDisk) BuildCacheUsage(context.Context) ([]system.CacheTypeUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.types, f.err
}

func (f *fakeDisk) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/storage/measure_test.go`:

```go
package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/system"
	"github.com/darkraise/ghr/internal/toolchain"
)

func TestWalkSumsSizeFilesAndNewestTime(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a"), strings.Repeat("x", 5000))
	writeFile(t, filepath.Join(dir, "sub", "b"), "y")
	writeFile(t, filepath.Join(dir, "sub", "c"), "z")
	newest := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	older := newest.Add(-time.Hour)
	for _, p := range []string{"a", filepath.Join("sub", "b")} {
		if err := os.Chtimes(filepath.Join(dir, p), older, older); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(filepath.Join(dir, "sub", "c"), newest, newest); err != nil {
		t.Fatal(err)
	}
	u, ok, err := walk(dir)
	if err != nil || !ok {
		t.Fatalf("ok %v, err %v", ok, err)
	}
	if u.Files != 3 || !u.Last.Equal(newest) {
		t.Fatalf("usage %+v", u)
	}
	if u.Bytes < 5002 {
		t.Fatalf("%d bytes is less than the files hold", u.Bytes)
	}
}

func TestWalkMissingPath(t *testing.T) {
	u, ok, err := walk(filepath.Join(t.TempDir(), "absent"))
	if ok || err != nil || u != (usage{}) {
		t.Fatalf("usage %+v, ok %v, err %v", u, ok, err)
	}
}

func TestWalkDoesNotFollowSymlinks(t *testing.T) {
	target := t.TempDir()
	writeFile(t, filepath.Join(target, "f1"), "x")
	writeFile(t, filepath.Join(target, "f2"), "x")
	dir := t.TempDir()
	if err := os.Symlink(target, filepath.Join(dir, "link")); err != nil {
		t.Skip("no symlinks here:", err)
	}
	u, _, err := walk(dir)
	if err != nil || u.Files != 0 {
		t.Fatalf("usage %+v, err %v", u, err)
	}
}

func TestMeasureFillsEveryPart(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	node := filepath.Join(root, "node", "22.11.0", "x64")
	writeFile(t, filepath.Join(node, "bin", "node"), "n")
	writeFile(t, filepath.Join(root, "PyPy", "3.10.14", "x64", "bin", "pypy"), "p")
	writeFile(t, filepath.Join(home, ".npm", "_cacache", "index"), "i")
	at := time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)
	tools := &fakeTools{root: root, other: []string{"PyPy"},
		installed: []toolchain.Installed{{Tool: "node", Version: "22.11.0", Arch: "x64", Path: node, InstalledAt: at}}}
	disk := &fakeDisk{
		rows:  []system.DiskRow{{Type: "Images", Count: 9, Active: 2, Bytes: 6571000000, Reclaimable: 5627000000}},
		types: []system.CacheTypeUsage{{Type: "regular", Count: 2, Bytes: 1686000000, Reclaimable: 456000000}},
	}
	m := measure(context.Background(), tools, disk, home, at)
	if m.err != "" {
		t.Fatal(m.err)
	}
	if !m.at.Equal(at) {
		t.Fatalf("at %v", m.at)
	}
	if len(m.toolchains) != 1 || m.toolchains[0].Bytes == 0 || m.toolchains[0].Version != "22.11.0" ||
		m.toolchains[0].Path != node || !m.toolchains[0].InstalledAt.Equal(at) {
		t.Fatalf("toolchains %+v", m.toolchains)
	}
	if len(m.other) != 1 || m.other[0].Name != "PyPy" || m.other[0].Bytes == 0 {
		t.Fatalf("other %+v", m.other)
	}
	if len(m.caches) != len(Caches) {
		t.Fatalf("%d caches", len(m.caches))
	}
	by := map[string]model.PackageCache{}
	for _, c := range m.caches {
		by[c.Name] = c
	}
	npm := by["npm"]
	if !npm.Present || npm.Files != 1 || npm.Bytes == 0 || npm.LastWritten == nil || npm.Label != "npm" || npm.Paths[0] != filepath.Join(home, ".npm") {
		t.Fatalf("npm %+v", npm)
	}
	if by["nuget"].Present || by["nuget"].LastWritten != nil {
		t.Fatalf("nuget %+v", by["nuget"])
	}
	if len(m.docker) != 1 || m.docker[0] != (model.DockerRow{Type: "Images", Count: 9, Active: 2, Bytes: 6571000000, Reclaimable: 5627000000}) {
		t.Fatalf("docker %+v", m.docker)
	}
	if len(m.cacheTypes) != 1 || m.cacheTypes[0] != (model.BuildCacheType{Type: "regular", Count: 2, Bytes: 1686000000, Reclaimable: 456000000}) {
		t.Fatalf("cache types %+v", m.cacheTypes)
	}
}

func TestMeasureKeepsFilesystemResultsWhenDockerFails(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".cache", "pip", "wheels", "w"), "w")
	disk := &fakeDisk{err: errors.New("Cannot connect to the Docker daemon")}
	m := measure(context.Background(), &fakeTools{root: t.TempDir()}, disk, home, time.Now())
	if !strings.Contains(m.err, "docker system df: Cannot connect to the Docker daemon") {
		t.Fatalf("err %q", m.err)
	}
	for _, c := range m.caches {
		if c.Name == "pip" && !c.Present {
			t.Fatal("pip lost with the Docker failure")
		}
	}
	if m.docker == nil || len(m.docker) != 0 || m.cacheTypes == nil {
		t.Fatalf("docker %+v, types %+v", m.docker, m.cacheTypes)
	}
}

func TestEmptyMeasuredHasNoNilSlices(t *testing.T) {
	m := emptyMeasured()
	if m.toolchains == nil || m.other == nil || m.caches == nil || m.docker == nil || m.cacheTypes == nil {
		t.Fatalf("%+v", m)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/storage/`
Expected: FAIL to compile — `undefined: walk`, `undefined: usage`, `undefined: measure`, `undefined: emptyMeasured`.

- [ ] **Step 4: Write the implementation**

Create `internal/storage/measure.go`:

```go
package storage

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/system"
	"github.com/darkraise/ghr/internal/toolchain"
)

// Toolchains is the part of *toolchain.Set the service uses.
type Toolchains interface {
	Get(tool string) (toolchain.Installer, error)
	Available(ctx context.Context, tool string) ([]toolchain.Choice, error)
	Installed() ([]toolchain.Installed, error)
	Other() ([]string, error)
	Root() string
	CleanTmp() error
}

// Docker is the Docker disk reporting the measurer reads.
type Docker interface {
	DiskUsage(ctx context.Context) ([]system.DiskRow, error)
	BuildCacheUsage(ctx context.Context) ([]system.CacheTypeUsage, error)
}

type usage struct {
	Bytes int64
	Files int64
	Last  time.Time
}

// walk sums the space taken under path (st_blocks × 512, as du counts),
// counts regular files and finds the newest file modification time. It
// never follows a symlink: WalkDir reports entries with Lstat. ok is false
// when path does not exist. Entries that vanish mid-walk, as a running job
// deletes them, are skipped; the first other error is returned with the
// partial sums.
func walk(path string) (usage, bool, error) {
	var u usage
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return u, false, nil
	} else if err != nil {
		return u, false, err
	}
	var first error
	walkErr := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err == nil {
			var fi fs.FileInfo
			if fi, err = d.Info(); err == nil {
				u.Bytes += allocated(fi)
				if fi.Mode().IsRegular() {
					u.Files++
					if fi.ModTime().After(u.Last) {
						u.Last = fi.ModTime()
					}
				}
				return nil
			}
		}
		if !os.IsNotExist(err) && first == nil {
			first = err
		}
		return nil
	})
	if first == nil {
		first = walkErr
	}
	return u, true, first
}

// measured is one complete measurement; the service swaps it into its
// snapshot whole, so readers never see half of one.
type measured struct {
	at         time.Time
	toolchains []model.Toolchain
	other      []model.Folder
	caches     []model.PackageCache
	docker     []model.DockerRow
	cacheTypes []model.BuildCacheType
	err        string
}

func emptyMeasured() measured {
	return measured{
		toolchains: []model.Toolchain{},
		other:      []model.Folder{},
		caches:     []model.PackageCache{},
		docker:     []model.DockerRow{},
		cacheTypes: []model.BuildCacheType{},
	}
}

// measure walks every installed toolchain, other tool-cache folder and
// package cache path, then reads Docker's disk usage last. A failed part is
// named in err and the rest is kept.
func measure(ctx context.Context, tools Toolchains, docker Docker, home string, at time.Time) measured {
	m := emptyMeasured()
	m.at = at
	var errs []string
	note := func(what string, err error) {
		if err != nil {
			errs = append(errs, what+": "+err.Error())
		}
	}
	installed, err := tools.Installed()
	note("toolchains", err)
	for _, in := range installed {
		u, _, err := walk(in.Path)
		note(in.Tool+" "+in.Version, err)
		m.toolchains = append(m.toolchains, model.Toolchain{Tool: in.Tool, Version: in.Version, Arch: in.Arch,
			Path: in.Path, Bytes: u.Bytes, InstalledAt: in.InstalledAt})
	}
	other, err := tools.Other()
	note("tool cache", err)
	for _, name := range other {
		u, _, err := walk(filepath.Join(tools.Root(), name))
		note(name, err)
		m.other = append(m.other, model.Folder{Name: name, Bytes: u.Bytes})
	}
	for _, c := range Caches {
		pc := model.PackageCache{Name: c.Name, Label: c.Label, Paths: c.abs(home)}
		for _, p := range pc.Paths {
			u, ok, err := walk(p)
			note(c.Name, err)
			if !ok {
				continue
			}
			pc.Present = true
			pc.Bytes += u.Bytes
			pc.Files += u.Files
			if !u.Last.IsZero() && (pc.LastWritten == nil || u.Last.After(*pc.LastWritten)) {
				last := u.Last
				pc.LastWritten = &last
			}
		}
		m.caches = append(m.caches, pc)
	}
	rows, err := docker.DiskUsage(ctx)
	note("docker system df", err)
	for _, r := range rows {
		m.docker = append(m.docker, model.DockerRow{Type: r.Type, Count: r.Count, Active: r.Active, Bytes: r.Bytes, Reclaimable: r.Reclaimable})
	}
	types, err := docker.BuildCacheUsage(ctx)
	note("docker build cache", err)
	for _, t := range types {
		m.cacheTypes = append(m.cacheTypes, model.BuildCacheType{Type: t.Type, Count: t.Count, Bytes: t.Bytes, Reclaimable: t.Reclaimable})
	}
	m.err = strings.Join(errs, "; ")
	return m
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/storage/`
Expected: `ok`.

Run: `timeout 600 go test ./...`, `timeout 120 gofmt -l internal cmd`, `timeout 300 go vet ./...`
Expected: every package `ok`; gofmt and vet print nothing.

- [ ] **Step 6: Commit**

```bash
timeout 30 git add internal/storage/measure.go internal/storage/measure_test.go internal/storage/fakes_test.go
timeout 30 git commit -m "feat(storage): measure toolchains, caches and Docker"
```

### Task 9: Storage service and measurer loop

**Files:**
- Create: `internal/storage/service.go`
- Test: `internal/storage/service_test.go`

**Interfaces:**
- Consumes: C4 model types; C7 `sweepClearing`, `clearingTag`, `Caches`, test helper `writeFile`; C8 interfaces, `measure`, `emptyMeasured`, fakes and `waitFor`.
- Produces: C9.

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

The risk is concurrency: the measurer goroutine swaps the snapshot while API handlers read it, so every shared field is read and written under `mu`, and the measurement is built outside the lock and swapped in whole.

- [ ] **Step 1: Write the failing tests**

Create `internal/storage/service_test.go`:

```go
package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/system"
)

// newService builds an unstarted service over the fakes, with a fresh home.
func newService(t *testing.T, tools *fakeTools, disk *fakeDisk) *Service {
	t.Helper()
	if tools.root == "" {
		tools.root = t.TempDir()
	}
	return &Service{Tools: tools, Docker: disk, Home: t.TempDir(), Events: events.New()}
}

func startService(t *testing.T, s *Service) {
	t.Helper()
	s.Start()
	t.Cleanup(func() {
		s.Close()
		s.Wait()
	})
}

func recvOrFail(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestStartCleansUpAndMeasuresAtOnce(t *testing.T) {
	tools := &fakeTools{}
	s := newService(t, tools, &fakeDisk{rows: []system.DiskRow{{Type: "Images", Count: 1}}})
	left := filepath.Join(s.Home, ".nuget", ".packages"+clearingTag+"op1")
	writeFile(t, filepath.Join(left, "x"), "x")
	startService(t, s)
	waitFor(t, "the first measurement", func() bool { return s.Snapshot().MeasuredAt != nil })
	if n := tools.cleanCount(); n != 1 {
		t.Fatalf("CleanTmp ran %d times", n)
	}
	if _, err := os.Lstat(left); !os.IsNotExist(err) {
		t.Fatalf("an interrupted clear survived start: %v", err)
	}
	st := s.Snapshot()
	if len(st.PackageCaches) != len(Caches) || len(st.Docker.Rows) != 1 || st.Toolchains == nil || st.OtherToolCache == nil ||
		st.Docker.BuildCacheTypes == nil || st.Operations.Recent == nil || st.Operations.Current != nil || st.Measuring {
		t.Fatalf("snapshot %+v", st)
	}
}

func TestSnapshotBeforeTheFirstMeasurementHasEmptyLists(t *testing.T) {
	disk := &fakeDisk{entered: make(chan struct{}, 10), gate: make(chan struct{})}
	s := newService(t, &fakeTools{}, disk)
	startService(t, s)
	recvOrFail(t, disk.entered, "the first measurement")
	st := s.Snapshot()
	if st.MeasuredAt != nil || !st.Measuring || st.Toolchains == nil || st.PackageCaches == nil || st.Docker.Rows == nil {
		t.Fatalf("snapshot %+v", st)
	}
}

func TestRefreshIsRefusedDuringAMeasurement(t *testing.T) {
	disk := &fakeDisk{entered: make(chan struct{}, 10), gate: make(chan struct{})}
	s := newService(t, &fakeTools{}, disk)
	startService(t, s)
	recvOrFail(t, disk.entered, "the first measurement")
	if err := s.Refresh(); !errors.Is(err, ErrMeasuring) {
		t.Fatalf("refresh during a measurement: %v", err)
	}
	disk.gate <- struct{}{}
	waitFor(t, "the measurement to end", func() bool {
		st := s.Snapshot()
		return !st.Measuring && st.MeasuredAt != nil
	})
	if err := s.Refresh(); err != nil {
		t.Fatalf("refresh after it: %v", err)
	}
	recvOrFail(t, disk.entered, "the refreshed measurement")
}

func TestTriggersDuringAMeasurementCoalesce(t *testing.T) {
	disk := &fakeDisk{entered: make(chan struct{}, 10), gate: make(chan struct{})}
	s := newService(t, &fakeTools{}, disk)
	startService(t, s)
	recvOrFail(t, disk.entered, "the first measurement")
	s.Trigger()
	s.Trigger()
	s.Trigger()
	disk.gate <- struct{}{}
	recvOrFail(t, disk.entered, "the follow-up measurement")
	if n := len(s.trigger); n != 0 {
		t.Fatalf("%d more measurements queued", n)
	}
	if n := disk.callCount(); n != 2 {
		t.Fatalf("%d measurements", n)
	}
}

func TestReadersNeverSeeAPartialMeasurement(t *testing.T) {
	s := newService(t, &fakeTools{}, &fakeDisk{rows: []system.DiskRow{{Type: "Images"}}})
	startService(t, s)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			s.Trigger()
			time.Sleep(time.Millisecond)
		}
	}()
	for {
		select {
		case <-done:
			return
		default:
		}
		st := s.Snapshot()
		if st.MeasuredAt != nil && (len(st.PackageCaches) != len(Caches) || len(st.Docker.Rows) != 1) {
			t.Fatalf("partial snapshot %+v", st)
		}
	}
}

func TestCloseStopsTheService(t *testing.T) {
	s := newService(t, &fakeTools{}, &fakeDisk{})
	s.Start()
	s.Close()
	waited := make(chan struct{})
	go func() {
		s.Wait()
		close(waited)
	}()
	recvOrFail(t, waited, "Wait after Close")
	if err := s.Refresh(); !errors.Is(err, ErrClosed) {
		t.Fatalf("refresh after Close: %v", err)
	}
}

func TestTriggerBeforeStartDoesNothing(t *testing.T) {
	s := newService(t, &fakeTools{}, &fakeDisk{})
	s.Trigger()
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/storage/`
Expected: FAIL to compile — `undefined: Service`, `undefined: ErrMeasuring`, `undefined: ErrClosed`.

- [ ] **Step 3: Write the implementation**

Create `internal/storage/service.go`:

```go
package storage

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/model"
)

var (
	ErrMeasuring = errors.New("a measurement is already running")
	ErrClosed    = errors.New("ghr is shutting down")
)

// MeasureInterval is how often the measurer runs without being asked.
const MeasureInterval = 30 * time.Minute

// Service holds the snapshot GET /storage serves, runs the measurer, and
// runs toolchain installs, removals and cache clears one at a time.
type Service struct {
	Tools  Toolchains
	Docker Docker
	Home   string     // the runner user's home, where the package caches live
	Busy   func() int // runners with a job; nil counts none
	Events *events.Ring
	Now    func() time.Time // nil means time.Now
	NewID  func() string    // operation ids; nil makes random ones

	mu        sync.Mutex
	snap      measured
	measuring bool
	closed    bool
	queue     []*op
	current   *model.Operation
	recent    []model.Operation // newest first

	ctx     context.Context
	cancel  context.CancelFunc
	trigger chan struct{}
	wake    chan struct{}
	wg      sync.WaitGroup
}

// op is one queued operation; run returns its outcome and message.
type op struct {
	model.Operation
	run func(ctx context.Context, progress func(step string)) (outcome, message string)
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Start deletes what an interrupted install or clear left behind, then
// starts the measurer, which measures at once. Call it once, before any
// other method.
func (s *Service) Start() {
	if err := s.Tools.CleanTmp(); err != nil {
		s.Events.Add("warn", "", "storage: clearing the tool cache's .tmp: %v", err)
	}
	if err := sweepClearing(s.Home); err != nil {
		s.Events.Add("warn", "", "storage: removing interrupted cache clears: %v", err)
	}
	s.snap = emptyMeasured()
	s.recent = []model.Operation{}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.trigger = make(chan struct{}, 1)
	s.wake = make(chan struct{}, 1)
	s.trigger <- struct{}{}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.measureLoop()
	}()
}

func (s *Service) measureLoop() {
	tick := time.NewTicker(MeasureInterval)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
		case <-s.trigger:
		}
		s.mu.Lock()
		s.measuring = true
		s.mu.Unlock()
		m := measure(s.ctx, s.Tools, s.Docker, s.Home, s.now())
		s.mu.Lock()
		if s.ctx.Err() == nil {
			s.snap = m
		}
		s.measuring = false
		s.mu.Unlock()
	}
}

// Trigger asks for a measurement. Triggers that arrive during one coalesce
// into a single measurement after it.
func (s *Service) Trigger() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

// Refresh asks for a measurement now; ErrMeasuring while one runs.
func (s *Service) Refresh() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.closed:
		return ErrClosed
	case s.measuring:
		return ErrMeasuring
	}
	s.Trigger()
	return nil
}

// Snapshot is the last complete measurement and the operation queue's
// state; it never walks a directory.
func (s *Service) Snapshot() model.Storage {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := model.Storage{
		Toolchains:     slices.Clone(s.snap.toolchains),
		OtherToolCache: slices.Clone(s.snap.other),
		PackageCaches:  slices.Clone(s.snap.caches),
		Docker: model.DockerDisk{
			Rows:            slices.Clone(s.snap.docker),
			BuildCacheTypes: slices.Clone(s.snap.cacheTypes),
		},
		Measuring:    s.measuring,
		MeasureError: s.snap.err,
		Operations:   model.Operations{Queued: len(s.queue), Recent: slices.Clone(s.recent)},
	}
	if !s.snap.at.IsZero() {
		at := s.snap.at
		st.MeasuredAt = &at
	}
	if s.current != nil {
		cur := *s.current
		st.Operations.Current = &cur
	}
	return st
}

// Close stops the measurer and the queue: the running operation is
// cancelled and queued ones are dropped. Wait returns once both stopped.
func (s *Service) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *Service) Wait() { s.wg.Wait() }
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/storage/`
Expected: `ok`.

Run: `timeout 600 go test ./...`, `timeout 120 gofmt -l internal cmd`, `timeout 300 go vet ./...`
Expected: every package `ok`; gofmt and vet print nothing.

- [ ] **Step 5: Commit**

```bash
timeout 30 git add internal/storage/service.go internal/storage/service_test.go
timeout 30 git commit -m "feat(storage): add the service and measurer loop"
```

### Task 10: Operation queue

**Files:**
- Create: `internal/storage/queue.go`
- Modify: `internal/storage/service.go` (`Start` launches the worker)
- Test: `internal/storage/queue_test.go`

**Interfaces:**
- Consumes: C2 `toolchain.Popular`, `toolchain.ErrUnknownTool`, `toolchain.ErrNotInstalled`, `toolchain.ErrAlreadyInstalled`; C4 `model.Operation`, `model.ToolchainChoice`, `model.HumanBytes`; C7 `cacheByName`, `(Cache).present`, `clearCache`, test helpers `writeFile`, `entries`; C8 fakes; C9 `Service`, `op`, `ErrClosed`, test helpers `newService`, `startService`, `recvOrFail`.
- Produces: C10.

**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

The risk is concurrency and data loss: the worker shares the queue with API handlers, a removal or clear must check the busy count when it runs rather than when it was queued, and shutdown must interrupt the running operation and drop the rest.

- [ ] **Step 1: Write the failing tests**

Create `internal/storage/queue_test.go`:

```go
package storage

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/toolchain"
)

func queueService(t *testing.T, tools *fakeTools) (*Service, *atomic.Int32) {
	t.Helper()
	var busy atomic.Int32
	s := newService(t, tools, &fakeDisk{})
	s.Busy = func() int { return int(busy.Load()) }
	startService(t, s)
	return s, &busy
}

func nodeTools() (*fakeTools, *fakeInstaller) {
	node := newInstaller(map[string]string{"22": "22.11.0", "24": "24.9.0"})
	return &fakeTools{tools: map[string]*fakeInstaller{"node": node}}, node
}

func recvVersion(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for an install to start")
		return ""
	}
}

func recentOutcomes(s *Service) []string {
	var out []string
	for _, o := range s.Snapshot().Operations.Recent {
		out = append(out, o.Kind+" "+o.Target+" "+o.Outcome)
	}
	return out
}

func eventCount(s *Service, sub string) int {
	n := 0
	for _, e := range s.Events.After(0) {
		if strings.Contains(e.Msg, sub) {
			n++
		}
	}
	return n
}

func TestQueueRunsOperationsInOrderOneAtATime(t *testing.T) {
	tools, node := nodeTools()
	node.gate = make(chan struct{})
	node.started = make(chan string, 4)
	s, _ := queueService(t, tools)
	if err := s.Install("node", "22"); err != nil {
		t.Fatal(err)
	}
	if err := s.Install("node", "24"); err != nil {
		t.Fatal(err)
	}
	if v := recvVersion(t, node.started); v != "22.11.0" {
		t.Fatalf("first install %s", v)
	}
	waitFor(t, "the install's progress", func() bool {
		c := s.Snapshot().Operations.Current
		return c != nil && c.Progress == "downloading"
	})
	st := s.Snapshot()
	if st.Operations.Current.Target != "node 22" || st.Operations.Current.Kind != "install" || st.Operations.Queued != 1 {
		t.Fatalf("operations %+v", st.Operations)
	}
	node.gate <- struct{}{}
	if v := recvVersion(t, node.started); v != "24.9.0" {
		t.Fatalf("second install %s", v)
	}
	node.gate <- struct{}{}
	waitFor(t, "both installs", func() bool { return len(s.Snapshot().Operations.Recent) == 2 })
	if got := recentOutcomes(s); !slices.Equal(got, []string{"install node 24 ok", "install node 22 ok"}) {
		t.Fatalf("recent %q", got)
	}
	if got := node.installList(); !slices.Equal(got, []string{"22.11.0", "24.9.0"}) {
		t.Fatalf("installs %q", got)
	}
	r := s.Snapshot().Operations.Recent[0]
	if r.Message != "installed node 24.9.0" || r.FinishedAt == nil || r.Progress != "" || r.ID == "" {
		t.Fatalf("recent[0] %+v", r)
	}
}

func TestInstallOutcomes(t *testing.T) {
	tools, node := nodeTools()
	node.have["22.11.0"] = true
	s, _ := queueService(t, tools)
	if err := s.Install("node", "22"); err != nil {
		t.Fatal(err)
	}
	if err := s.Install("node", "99"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "two operations", func() bool { return len(s.Snapshot().Operations.Recent) == 2 })
	recent := s.Snapshot().Operations.Recent
	if recent[1].Outcome != "skipped" || recent[1].Message != "node 22.11.0 is already installed" {
		t.Fatalf("installed version %+v", recent[1])
	}
	if recent[0].Outcome != "failed" || !strings.Contains(recent[0].Message, `no release matches "99"`) {
		t.Fatalf("unresolvable version %+v", recent[0])
	}
}

func TestRequestsThatAreRefusedAtOnce(t *testing.T) {
	tools, _ := nodeTools()
	s, _ := queueService(t, tools)
	if err := s.Install("ruby", "3.3"); !errors.Is(err, toolchain.ErrUnknownTool) {
		t.Errorf("unknown tool: %v", err)
	}
	if err := s.Install("node", " "); !errors.Is(err, ErrMissingVersion) {
		t.Errorf("missing version: %v", err)
	}
	if err := s.InstallPreset("everything"); !errors.Is(err, ErrUnknownPreset) {
		t.Errorf("unknown preset: %v", err)
	}
	if err := s.Remove("ruby", "3.3.0"); !errors.Is(err, toolchain.ErrUnknownTool) {
		t.Errorf("remove unknown tool: %v", err)
	}
	if err := s.Remove("node", "20.0.0"); !errors.Is(err, toolchain.ErrNotInstalled) {
		t.Errorf("remove a version not installed: %v", err)
	}
	if err := s.Clear("bogus"); !errors.Is(err, ErrUnknownCache) {
		t.Errorf("unknown cache: %v", err)
	}
	if err := s.Clear("cargo"); !errors.Is(err, ErrNotPresent) {
		t.Errorf("absent cache: %v", err)
	}
	if st := s.Snapshot(); st.Operations.Queued != 0 || st.Operations.Current != nil {
		t.Fatalf("a refused request was queued: %+v", st.Operations)
	}
}

func TestPopularPresetQueuesNineInstallsInOrder(t *testing.T) {
	tools := &fakeTools{tools: map[string]*fakeInstaller{}}
	for _, e := range toolchain.Popular {
		i := tools.tools[e.Tool]
		if i == nil {
			i = newInstaller(map[string]string{})
			tools.tools[e.Tool] = i
		}
		i.resolve[e.Spec] = e.Spec + ".0"
	}
	s, _ := queueService(t, tools)
	if err := s.InstallPreset("popular"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "nine installs", func() bool { return len(s.Snapshot().Operations.Recent) == len(toolchain.Popular) })
	recent := s.Snapshot().Operations.Recent
	for i, e := range toolchain.Popular {
		r := recent[len(recent)-1-i]
		if r.Target != e.Tool+" "+e.Spec || r.Outcome != "ok" {
			t.Errorf("install %d: %+v", i, r)
		}
	}
}

func TestRemoveAndClearCheckBusyWhenTheyRun(t *testing.T) {
	tools, node := nodeTools()
	tools.installed = []toolchain.Installed{{Tool: "node", Version: "20.18.0", Arch: "x64"}}
	node.gate = make(chan struct{})
	node.started = make(chan string, 1)
	s, busy := queueService(t, tools)
	pkg := filepath.Join(s.Home, ".nuget", "packages", "pkg", "a.nupkg")
	writeFile(t, pkg, "x")
	if err := s.Install("node", "22"); err != nil {
		t.Fatal(err)
	}
	recvVersion(t, node.started)
	if err := s.Remove("node", "20.18.0"); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear("nuget"); err != nil {
		t.Fatal(err)
	}
	busy.Store(2)
	node.gate <- struct{}{}
	waitFor(t, "three operations", func() bool { return len(s.Snapshot().Operations.Recent) == 3 })
	for _, r := range s.Snapshot().Operations.Recent[:2] {
		if r.Outcome != "refused" || r.Message != "refused: 2 jobs running" {
			t.Fatalf("while busy: %+v", r)
		}
	}
	if got := node.removedList(); len(got) != 0 {
		t.Fatalf("a refused removal ran: %q", got)
	}
	if _, err := os.Stat(pkg); err != nil {
		t.Fatalf("a refused clear deleted files: %v", err)
	}
	busy.Store(0)
	if err := s.Remove("node", "20.18.0"); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear("nuget"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "five operations", func() bool { return len(s.Snapshot().Operations.Recent) == 5 })
	if got := recentOutcomes(s)[:2]; !slices.Equal(got, []string{"clear nuget ok", "remove node 20.18.0 ok"}) {
		t.Fatalf("once idle: %q", got)
	}
	if got := node.removedList(); !slices.Equal(got, []string{"20.18.0"}) {
		t.Fatalf("removed %q", got)
	}
	if got := entries(t, filepath.Join(s.Home, ".nuget", "packages")); len(got) != 0 {
		t.Fatalf("not cleared: %v", got)
	}
}

func TestCloseInterruptsTheRunningOperationAndDropsTheRest(t *testing.T) {
	tools, node := nodeTools()
	node.gate = make(chan struct{})
	node.started = make(chan string, 1)
	s := newService(t, tools, &fakeDisk{})
	s.Start()
	for _, spec := range []string{"22", "24", "24"} {
		if err := s.Install("node", spec); err != nil {
			t.Fatal(err)
		}
	}
	recvVersion(t, node.started)
	s.Close()
	waited := make(chan struct{})
	go func() {
		s.Wait()
		close(waited)
	}()
	recvOrFail(t, waited, "Wait after Close")
	st := s.Snapshot()
	if st.Operations.Queued != 0 || st.Operations.Current != nil || len(st.Operations.Recent) != 1 || st.Operations.Recent[0].Outcome != "interrupted" {
		t.Fatalf("operations %+v", st.Operations)
	}
	if n := eventCount(s, "dropped: ghr is shutting down"); n != 2 {
		t.Fatalf("%d dropped events", n)
	}
	if err := s.Install("node", "22"); !errors.Is(err, ErrClosed) {
		t.Fatalf("install after Close: %v", err)
	}
}

func TestRecentKeepsTheLastTen(t *testing.T) {
	tools, node := nodeTools()
	node.have["22.11.0"] = true
	s, _ := queueService(t, tools)
	for i := 0; i < 12; i++ {
		if err := s.Install("node", "22"); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "twelve operations", func() bool { return eventCount(s, "install node 22 skipped") == 12 })
	if n := len(s.Snapshot().Operations.Recent); n != 10 {
		t.Fatalf("%d recent operations", n)
	}
}

func TestOperationsReportEventsAndTriggerAMeasurement(t *testing.T) {
	tools, _ := nodeTools()
	disk := &fakeDisk{}
	s := newService(t, tools, disk)
	startService(t, s)
	waitFor(t, "the first measurement", func() bool { return s.Snapshot().MeasuredAt != nil && disk.callCount() == 1 })
	if err := s.Install("node", "22"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the measurement after the install", func() bool { return disk.callCount() >= 2 })
	if eventCount(s, "install node 22 started") != 1 || eventCount(s, "install node 22 ok: installed node 22.11.0") != 1 {
		t.Fatalf("events %+v", s.Events.After(0))
	}
}

func TestAvailableConvertsChoices(t *testing.T) {
	tools, _ := nodeTools()
	s, _ := queueService(t, tools)
	cs, err := s.Available(t.Context(), "node")
	if err != nil || len(cs) != 2 || cs[0].Spec != "22" || cs[0].Version != "22.11.0" {
		t.Fatalf("choices %+v, %v", cs, err)
	}
	if _, err := s.Available(t.Context(), "ruby"); !errors.Is(err, toolchain.ErrUnknownTool) {
		t.Fatalf("unknown tool: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/storage/`
Expected: FAIL to compile — `s.Install undefined`, `undefined: ErrMissingVersion`, `undefined: ErrUnknownPreset`, `undefined: ErrUnknownCache`, `undefined: ErrNotPresent`.

- [ ] **Step 3: Write the implementation**

Create `internal/storage/queue.go`:

```go
package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/toolchain"
)

var (
	ErrUnknownCache   = errors.New("unknown package cache")
	ErrNotPresent     = errors.New("package cache not present")
	ErrUnknownPreset  = errors.New("unknown toolchain preset")
	ErrMissingVersion = errors.New("a toolchain version is required")
)

const recentOps = 10

func (s *Service) newID() string {
	if s.NewID != nil {
		return s.NewID()
	}
	var b [6]byte
	rand.Read(b[:])
	return s.now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b[:])
}

func (s *Service) busy() int {
	if s.Busy == nil {
		return 0
	}
	return s.Busy()
}

func (s *Service) enqueue(ops ...*op) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	s.queue = append(s.queue, ops...)
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return nil
}

// work runs queued operations one at a time until Close, then drops the
// rest of the queue with a warning each.
func (s *Service) work() {
	for {
		s.mu.Lock()
		if s.ctx.Err() != nil {
			dropped := s.queue
			s.queue = nil
			s.mu.Unlock()
			for _, o := range dropped {
				s.Events.Add("warn", "", "%s %s dropped: ghr is shutting down", o.Kind, o.Target)
			}
			return
		}
		if len(s.queue) == 0 {
			s.mu.Unlock()
			select {
			case <-s.ctx.Done():
			case <-s.wake:
			}
			continue
		}
		o := s.queue[0]
		s.queue = s.queue[1:]
		o.StartedAt = s.now()
		cur := o.Operation
		s.current = &cur
		s.mu.Unlock()
		s.Events.Add("info", "", "%s %s started", o.Kind, o.Target)
		outcome, msg := o.run(s.ctx, func(step string) {
			s.mu.Lock()
			if s.current != nil {
				s.current.Progress = step
			}
			s.mu.Unlock()
		})
		finished := s.now()
		done := o.Operation
		done.FinishedAt, done.Outcome, done.Message = &finished, outcome, msg
		s.mu.Lock()
		s.current = nil
		s.recent = append([]model.Operation{done}, s.recent...)
		if len(s.recent) > recentOps {
			s.recent = s.recent[:recentOps]
		}
		s.mu.Unlock()
		level := "info"
		if outcome != "ok" && outcome != "skipped" {
			level = "warn"
		}
		s.Events.Add(level, "", "%s %s %s: %s", o.Kind, o.Target, outcome, msg)
		s.Trigger()
	}
}

func interruptedOr(ctx context.Context, outcome string) string {
	if ctx.Err() != nil {
		return "interrupted"
	}
	return outcome
}

// Install queues one install; spec resolves when the install runs, so an
// unresolvable version fails the operation, not the request.
func (s *Service) Install(tool, spec string) error {
	inst, err := s.Tools.Get(tool)
	if err != nil {
		return err
	}
	if strings.TrimSpace(spec) == "" {
		return ErrMissingVersion
	}
	return s.enqueue(s.installOp(tool, inst, spec))
}

// InstallPreset queues each install of the preset, in its order.
func (s *Service) InstallPreset(name string) error {
	if name != "popular" {
		return fmt.Errorf("%w %q", ErrUnknownPreset, name)
	}
	var ops []*op
	for _, e := range toolchain.Popular {
		inst, err := s.Tools.Get(e.Tool)
		if err != nil {
			return err
		}
		ops = append(ops, s.installOp(e.Tool, inst, e.Spec))
	}
	return s.enqueue(ops...)
}

func (s *Service) installOp(tool string, inst toolchain.Installer, spec string) *op {
	return &op{
		Operation: model.Operation{ID: s.newID(), Kind: "install", Target: tool + " " + spec},
		run: func(ctx context.Context, progress func(string)) (string, string) {
			progress("resolving")
			rel, err := inst.Resolve(ctx, spec)
			if err != nil {
				return interruptedOr(ctx, "failed"), err.Error()
			}
			err = inst.Install(ctx, rel, progress)
			switch {
			case errors.Is(err, toolchain.ErrAlreadyInstalled):
				return "skipped", tool + " " + rel.Version + " is already installed"
			case err != nil:
				return interruptedOr(ctx, "failed"), err.Error()
			}
			return "ok", "installed " + tool + " " + rel.Version
		},
	}
}

// Remove queues the removal of an installed version. It is refused when it
// runs if any runner is busy then.
func (s *Service) Remove(tool, version string) error {
	inst, err := s.Tools.Get(tool)
	if err != nil {
		return err
	}
	installed, err := s.Tools.Installed()
	if err != nil {
		return err
	}
	found := false
	for _, in := range installed {
		if in.Tool == tool && in.Version == version {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: %s %s", toolchain.ErrNotInstalled, tool, version)
	}
	target := tool + " " + version
	return s.enqueue(&op{
		Operation: model.Operation{ID: s.newID(), Kind: "remove", Target: target},
		run: func(ctx context.Context, progress func(string)) (string, string) {
			if n := s.busy(); n > 0 {
				return "refused", fmt.Sprintf("refused: %d jobs running", n)
			}
			progress("removing")
			note := freed(s.toolchainBytes(tool, version))
			if err := inst.Remove(version); err != nil {
				return "failed", err.Error()
			}
			return "ok", "removed " + target + note
		},
	})
}

// Clear queues clearing a present package cache. It is refused when it runs
// if any runner is busy then.
func (s *Service) Clear(name string) error {
	c, ok := cacheByName(name)
	if !ok {
		return fmt.Errorf("%w %q", ErrUnknownCache, name)
	}
	if !c.present(s.Home) {
		return fmt.Errorf("%w: %s", ErrNotPresent, name)
	}
	id := s.newID()
	return s.enqueue(&op{
		Operation: model.Operation{ID: id, Kind: "clear", Target: name},
		run: func(ctx context.Context, progress func(string)) (string, string) {
			if n := s.busy(); n > 0 {
				return "refused", fmt.Sprintf("refused: %d jobs running", n)
			}
			progress("clearing")
			note := freed(s.cacheBytes(name))
			if err := clearCache(s.Home, c, id); err != nil {
				return "failed", err.Error()
			}
			return "ok", "cleared " + c.Label + note
		},
	})
}

// Available lists what the Install dialog offers for tool, newest first.
func (s *Service) Available(ctx context.Context, tool string) ([]model.ToolchainChoice, error) {
	cs, err := s.Tools.Available(ctx, tool)
	if err != nil {
		return nil, err
	}
	out := make([]model.ToolchainChoice, len(cs))
	for i, c := range cs {
		out[i] = model.ToolchainChoice{Spec: c.Spec, Version: c.Version, LTS: c.LTS}
	}
	return out, nil
}

// freed names the size the last measurement saw, for an operation's message.
func freed(bytes int64) string {
	if bytes <= 0 {
		return ""
	}
	return " (" + model.HumanBytes(bytes) + " freed)"
}

func (s *Service) toolchainBytes(tool, version string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, tc := range s.snap.toolchains {
		if tc.Tool == tool && tc.Version == version {
			return tc.Bytes
		}
	}
	return 0
}

func (s *Service) cacheBytes(name string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.snap.caches {
		if c.Name == name {
			return c.Bytes
		}
	}
	return 0
}
```

In `internal/storage/service.go`, in `Start`, replace:

```go
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.measureLoop()
	}()
}
```

with:

```go
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		s.measureLoop()
	}()
	go func() {
		defer s.wg.Done()
		s.work()
	}()
}
```

and change the `Start` doc comment's last two sentences to: `then starts the measurer, which measures at once, and the queue worker. Call it once, before any other method.`

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/storage/`
Expected: `ok`.

Run: `timeout 600 go test ./...`, `timeout 120 gofmt -l internal cmd`, `timeout 300 go vet ./...`
Expected: every package `ok`; gofmt and vet print nothing.

- [ ] **Step 5: Commit**

```bash
timeout 30 git add internal/storage/queue.go internal/storage/queue_test.go internal/storage/service.go
timeout 30 git commit -m "feat(storage): run installs, removals and clears"
```

### Task 11: Backend storage methods

**Files:**
- Modify: `internal/daemon/backend.go` (imports, `Manager` interface, `StorageService`, `Backend.Space`, `Prune`, new methods)
- Modify: `internal/daemon/backend_test.go` (`fakeManager`, `newBackend`)
- Test: `internal/daemon/storage_test.go`

**Interfaces:**
- Consumes: C4 `model.Storage`, `model.LastPrune`, `model.ToolchainChoice`, `model.InstallRequest`; C5 `runner.ScopeStandard`, `runner.ErrUnknownScope`, `runner.BusyError`; C6 `StartPruneScope`, `LastPrune`; C9/C10 `storage.ErrMeasuring`, `storage.ErrClosed`, `storage.ErrUnknownCache`, `storage.ErrNotPresent`, `storage.ErrUnknownPreset`, `storage.ErrMissingVersion`; C2 `toolchain.ErrUnknownTool`, `toolchain.ErrNotInstalled`.
- Produces: C11.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `internal/daemon/backend_test.go`:

1. In the `fakeManager` struct, add these fields after `paused    time.Time // returned by PausedUntil`:

```go
	scopes    []string         // scopes StartPruneScope was asked for
	lastPrune *model.LastPrune // returned by LastPrune
	diskPct   int              // Status().DiskPct
```

2. Replace the `StartPrune` method with:

```go
func (f *fakeManager) StartPruneScope(scope string) error {
	f.prunes++
	f.scopes = append(f.scopes, scope)
	return f.pruneErr
}

func (f *fakeManager) LastPrune() *model.LastPrune { return f.lastPrune }
```

3. In the `Status` method, change the returned value to `model.Status{Instances: f.insts, Degraded: f.degraded != "", DegradedReason: f.degraded, DiskPct: f.diskPct}`.

4. In `newBackend`, add `Space: &fakeStorage{},` to the `Backend` literal, after `Hist: ...`.

Run `timeout 60 gofmt -w internal/daemon/backend_test.go`.

Create `internal/daemon/storage_test.go`:

```go
package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
	"github.com/darkraise/ghr/internal/runner"
	"github.com/darkraise/ghr/internal/storage"
	"github.com/darkraise/ghr/internal/toolchain"
)

type fakeStorage struct {
	snap     model.Storage
	err      error // returned by every mutation and by Refresh
	availErr error
	calls    []string
}

func (f *fakeStorage) Snapshot() model.Storage { return f.snap }
func (f *fakeStorage) Refresh() error {
	f.calls = append(f.calls, "refresh")
	return f.err
}
func (f *fakeStorage) Available(_ context.Context, tool string) ([]model.ToolchainChoice, error) {
	f.calls = append(f.calls, "available "+tool)
	if f.availErr != nil {
		return nil, f.availErr
	}
	return []model.ToolchainChoice{{Spec: "22", Version: "22.11.0"}}, nil
}
func (f *fakeStorage) Install(tool, spec string) error {
	f.calls = append(f.calls, "install "+tool+" "+spec)
	return f.err
}
func (f *fakeStorage) InstallPreset(name string) error {
	f.calls = append(f.calls, "preset "+name)
	return f.err
}
func (f *fakeStorage) Remove(tool, version string) error {
	f.calls = append(f.calls, "remove "+tool+" "+version)
	return f.err
}
func (f *fakeStorage) Clear(name string) error {
	f.calls = append(f.calls, "clear "+name)
	return f.err
}

func apiMsg(err error) string {
	var ae *api.Error
	if errors.As(err, &ae) {
		return ae.Msg
	}
	return ""
}

func TestStorageMergesLastPruneAndDisk(t *testing.T) {
	b, m, _ := newBackend(t)
	now := time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)
	m.lastPrune = &model.LastPrune{Trigger: "auto", Scope: "auto", StartedAt: now}
	m.diskPct = 73
	b.Space.(*fakeStorage).snap = model.Storage{MeasureError: "x", Docker: model.DockerDisk{Rows: []model.DockerRow{{Type: "Images"}}}}
	st := b.Storage()
	if st.LastPrune == nil || st.LastPrune.Trigger != "auto" || st.Docker.DiskPct != 73 || st.MeasureError != "x" || len(st.Docker.Rows) != 1 {
		t.Fatalf("storage %+v", st)
	}
}

func TestStorageErrorsMapToStatuses(t *testing.T) {
	b, _, _ := newBackend(t)
	fs := b.Space.(*fakeStorage)
	for _, tc := range []struct {
		err  error
		want int
	}{
		{fmt.Errorf("%w %q", toolchain.ErrUnknownTool, "ruby"), 400},
		{fmt.Errorf("%w %q", storage.ErrUnknownPreset, "all"), 400},
		{storage.ErrMissingVersion, 400},
		{fmt.Errorf("%w: node 20.0.0", toolchain.ErrNotInstalled), 404},
		{fmt.Errorf("%w %q", storage.ErrUnknownCache, "bogus"), 404},
		{fmt.Errorf("%w: cargo", storage.ErrNotPresent), 409},
		{storage.ErrClosed, 503},
	} {
		fs.err = tc.err
		for name, call := range map[string]func() error{
			"install": func() error { return b.InstallToolchain(model.InstallRequest{Tool: "node", Version: "22"}) },
			"remove":  func() error { return b.RemoveToolchain("node", "22.11.0") },
			"clear":   func() error { return b.ClearCache("nuget") },
		} {
			err := call()
			if got := apiStatus(err); got != tc.want || apiMsg(err) != tc.err.Error() {
				t.Errorf("%s with %v: status %d message %q, want %d", name, tc.err, got, apiMsg(err), tc.want)
			}
		}
	}
	fs.err = errors.New("disk on fire")
	if err := b.ClearCache("nuget"); err == nil || apiStatus(err) != 0 {
		t.Fatalf("an unexpected error must pass through unchanged: %v", err)
	}
}

func TestInstallToolchainRequests(t *testing.T) {
	b, _, _ := newBackend(t)
	fs := b.Space.(*fakeStorage)
	if err := b.InstallToolchain(model.InstallRequest{Tool: "node", Version: "22"}); err != nil {
		t.Fatal(err)
	}
	if err := b.InstallToolchain(model.InstallRequest{Preset: "popular"}); err != nil {
		t.Fatal(err)
	}
	if err := b.InstallToolchain(model.InstallRequest{}); apiStatus(err) != 400 {
		t.Fatalf("empty request: %v", err)
	}
	if err := b.InstallToolchain(model.InstallRequest{Tool: "node", Version: "22", Preset: "popular"}); apiStatus(err) != 400 {
		t.Fatalf("tool and preset: %v", err)
	}
	if want := []string{"install node 22", "preset popular"}; !slices.Equal(fs.calls, want) {
		t.Fatalf("calls %q", fs.calls)
	}
}

func TestRefreshStorage(t *testing.T) {
	b, _, _ := newBackend(t)
	fs := b.Space.(*fakeStorage)
	if err := b.RefreshStorage(); err != nil {
		t.Fatal(err)
	}
	fs.err = storage.ErrMeasuring
	if err := b.RefreshStorage(); apiStatus(err) != 409 {
		t.Fatalf("measuring: %v", err)
	}
	fs.err = storage.ErrClosed
	if err := b.RefreshStorage(); apiStatus(err) != 503 {
		t.Fatalf("closed: %v", err)
	}
}

func TestAvailableToolchains(t *testing.T) {
	b, _, _ := newBackend(t)
	fs := b.Space.(*fakeStorage)
	cs, err := b.AvailableToolchains(context.Background(), "node")
	if err != nil || len(cs) != 1 || cs[0].Version != "22.11.0" {
		t.Fatalf("choices %+v, %v", cs, err)
	}
	fs.availErr = fmt.Errorf("%w %q", toolchain.ErrUnknownTool, "ruby")
	if _, err := b.AvailableToolchains(context.Background(), "ruby"); apiStatus(err) != 400 {
		t.Fatalf("unknown tool: %v", err)
	}
	fs.availErr = errors.New("GET https://api.adoptium.net/v3/info/available_releases: 503 Service Unavailable")
	_, err = b.AvailableToolchains(context.Background(), "java")
	if apiStatus(err) != 502 || apiMsg(err) != fs.availErr.Error() {
		t.Fatalf("source failure: %v", err)
	}
}

func TestPruneScopeStatuses(t *testing.T) {
	b, m, _ := newBackend(t)
	if err := b.PruneScope(runner.ScopeBuildCacheAll); err != nil {
		t.Fatal(err)
	}
	if err := b.Prune(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"build-cache-all", "standard"}; !slices.Equal(m.scopes, want) {
		t.Fatalf("scopes %q", m.scopes)
	}
	for _, tc := range []struct {
		err  error
		want int
	}{
		{fmt.Errorf("%w %q", runner.ErrUnknownScope, "everything"), 400},
		{runner.BusyError{N: 2}, 409},
		{runner.ErrPruneRunning, 409},
		{runner.ErrUpdateRunning, 409},
		{runner.ErrClosed, 503},
	} {
		m.pruneErr = tc.err
		err := b.PruneScope(runner.ScopeUnusedVolumes)
		if apiStatus(err) != tc.want || apiMsg(err) != tc.err.Error() {
			t.Errorf("%v: status %d message %q", tc.err, apiStatus(err), apiMsg(err))
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/daemon/`
Expected: FAIL to compile — `unknown field Space in struct literal of type Backend`, `*fakeManager does not implement Manager (missing method StartPrune)`, `b.Storage undefined`, `b.PruneScope undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/daemon/backend.go`:

1. Add to the import block, in order with the other internal imports: `"github.com/darkraise/ghr/internal/storage"` and `"github.com/darkraise/ghr/internal/toolchain"`.

2. In the `Manager` interface, replace the line `StartPrune() error` with:

```go
	StartPruneScope(scope string) error
	LastPrune() *model.LastPrune
```

3. Directly after the `GitHub` interface, add:

```go
// StorageService is the part of *storage.Service the backend uses.
type StorageService interface {
	Snapshot() model.Storage
	Refresh() error
	Available(ctx context.Context, tool string) ([]model.ToolchainChoice, error)
	Install(tool, spec string) error
	InstallPreset(name string) error
	Remove(tool, version string) error
	Clear(name string) error
}
```

4. In the `Backend` struct, directly after the line `Hist   *history.Store`, add:

```go
	// Space is the storage service: the snapshot, toolchains and caches.
	Space StorageService
```

5. Replace the whole `Prune` method and its doc comment with:

```go
// Prune starts a standard manual prune; its progress goes to the events.
func (b *Backend) Prune() error { return b.PruneScope(runner.ScopeStandard) }

// PruneScope starts a manual prune of one scope.
func (b *Backend) PruneScope(scope string) error {
	err := b.M.StartPruneScope(scope)
	var busy runner.BusyError
	switch {
	case errors.Is(err, runner.ErrUnknownScope):
		return api.BadRequest(err.Error())
	case errors.As(err, &busy), errors.Is(err, runner.ErrPruneRunning), errors.Is(err, runner.ErrUpdateRunning):
		return api.Conflict(err.Error())
	case errors.Is(err, runner.ErrClosed):
		return &api.Error{Status: http.StatusServiceUnavailable, Msg: err.Error()}
	}
	return err
}

// Storage is the storage snapshot with the last prune and the disk reading,
// which the runner manager keeps.
func (b *Backend) Storage() model.Storage {
	st := b.Space.Snapshot()
	st.LastPrune = b.M.LastPrune()
	st.Docker.DiskPct = b.M.Status().DiskPct
	return st
}

func (b *Backend) RefreshStorage() error { return storageErr(b.Space.Refresh()) }

// AvailableToolchains lists what an install of tool can ask for; an
// upstream failure is a 502 carrying its message.
func (b *Backend) AvailableToolchains(ctx context.Context, tool string) ([]model.ToolchainChoice, error) {
	cs, err := b.Space.Available(ctx, tool)
	switch {
	case errors.Is(err, toolchain.ErrUnknownTool):
		return nil, api.BadRequest(err.Error())
	case err != nil:
		return nil, &api.Error{Status: http.StatusBadGateway, Msg: err.Error()}
	}
	return cs, nil
}

// InstallToolchain queues one install or a preset's installs.
func (b *Backend) InstallToolchain(req model.InstallRequest) error {
	switch {
	case req.Preset != "" && req.Tool != "":
		return api.BadRequest("give a preset or a tool, not both")
	case req.Preset != "":
		return storageErr(b.Space.InstallPreset(req.Preset))
	case req.Tool == "":
		return api.BadRequest("a tool or a preset is required")
	}
	return storageErr(b.Space.Install(req.Tool, req.Version))
}

func (b *Backend) RemoveToolchain(tool, version string) error {
	return storageErr(b.Space.Remove(tool, version))
}

func (b *Backend) ClearCache(name string) error { return storageErr(b.Space.Clear(name)) }

// storageErr gives a storage or toolchain error the status the API answers with.
func storageErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, toolchain.ErrUnknownTool), errors.Is(err, storage.ErrUnknownPreset), errors.Is(err, storage.ErrMissingVersion):
		return api.BadRequest(err.Error())
	case errors.Is(err, toolchain.ErrNotInstalled), errors.Is(err, storage.ErrUnknownCache):
		return api.NotFound(err.Error())
	case errors.Is(err, storage.ErrNotPresent), errors.Is(err, storage.ErrMeasuring):
		return api.Conflict(err.Error())
	case errors.Is(err, storage.ErrClosed):
		return &api.Error{Status: http.StatusServiceUnavailable, Msg: err.Error()}
	}
	return err
}
```

Run `timeout 60 gofmt -w internal/daemon/backend.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/daemon/`
Expected: `ok` — the new tests and the existing `TestPruneStartsOrConflicts` pass.

Run: `timeout 600 go test ./...`, `timeout 120 gofmt -l internal cmd`, `timeout 300 go vet ./...`
Expected: every package `ok`; gofmt and vet print nothing.

- [ ] **Step 5: Commit**

```bash
timeout 30 git add internal/daemon/backend.go internal/daemon/backend_test.go internal/daemon/storage_test.go
timeout 30 git commit -m "feat(daemon): serve storage, toolchains and scopes"
```

### Task 12: API routes and client

**Files:**
- Modify: `internal/api/server.go` (`Backend` interface, `NewServer` routes, new `accepted` helper)
- Modify: `internal/api/client.go` (eight client methods)
- Modify: `internal/api/api_test.go` (`fakeBackend` fields)
- Test: `internal/api/storage_test.go`

**Interfaces:**
- Consumes: C4 `model.Storage`, `model.ToolchainChoice`, `model.InstallRequest`; C11 Backend method set (the daemon's `*Backend` already implements it, so `var _ api.Backend = (*Backend)(nil)` keeps compiling).
- Produces: C12.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `internal/api/api_test.go`, add these fields to the `fakeBackend` struct, after `availErr     error`:

```go
	storage      model.Storage
	storageErr   error // returned by every storage method
	choices      []model.ToolchainChoice
	storageCalls []string
```

Run `timeout 60 gofmt -w internal/api/api_test.go`.

Create `internal/api/storage_test.go`:

```go
package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

func (f *fakeBackend) Storage() model.Storage {
	f.storageCalls = append(f.storageCalls, "storage")
	return f.storage
}
func (f *fakeBackend) RefreshStorage() error {
	f.storageCalls = append(f.storageCalls, "refresh")
	return f.storageErr
}
func (f *fakeBackend) AvailableToolchains(_ context.Context, tool string) ([]model.ToolchainChoice, error) {
	f.storageCalls = append(f.storageCalls, "available "+tool)
	return f.choices, f.storageErr
}
func (f *fakeBackend) InstallToolchain(req model.InstallRequest) error {
	f.storageCalls = append(f.storageCalls, fmt.Sprintf("install tool=%s version=%s preset=%s", req.Tool, req.Version, req.Preset))
	return f.storageErr
}
func (f *fakeBackend) RemoveToolchain(tool, version string) error {
	f.storageCalls = append(f.storageCalls, "remove "+tool+" "+version)
	return f.storageErr
}
func (f *fakeBackend) ClearCache(name string) error {
	f.storageCalls = append(f.storageCalls, "clear "+name)
	return f.storageErr
}
func (f *fakeBackend) PruneScope(scope string) error {
	f.storageCalls = append(f.storageCalls, "prune "+scope)
	return f.storageErr
}

func TestStorageRoutesThroughTheClient(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	b.storage = model.Storage{MeasureError: "x", PackageCaches: []model.PackageCache{{Name: "npm"}}}
	st, err := c.Storage(ctx)
	if err != nil || st.MeasureError != "x" || len(st.PackageCaches) != 1 {
		t.Fatalf("storage %+v, %v", st, err)
	}
	b.choices = []model.ToolchainChoice{{Spec: "21", Version: "21.0.8+9", LTS: true}}
	cs, err := c.AvailableToolchains(ctx, "java")
	if err != nil || len(cs) != 1 || !cs[0].LTS || cs[0].Version != "21.0.8+9" {
		t.Fatalf("choices %+v, %v", cs, err)
	}
	for _, call := range []func() error{
		func() error { return c.RefreshStorage(ctx) },
		func() error { return c.InstallToolchain(ctx, "node", "22") },
		func() error { return c.InstallPreset(ctx, "popular") },
		func() error { return c.RemoveToolchain(ctx, "java", "21.0.8+9") },
		func() error { return c.ClearCache(ctx, "nuget") },
		func() error { return c.PruneScope(ctx, "unused-volumes") },
	} {
		if err := call(); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		"storage", "available java", "refresh",
		"install tool=node version=22 preset=", "install tool= version= preset=popular",
		"remove java 21.0.8+9", "clear nuget", "prune unused-volumes",
	}
	if !reflect.DeepEqual(b.storageCalls, want) {
		t.Fatalf("calls %q", b.storageCalls)
	}
}

func TestStorageMutationsAnswer202(t *testing.T) {
	srv := httptest.NewServer(NewServer(&fakeBackend{}))
	defer srv.Close()
	for _, r := range []struct{ method, path, body string }{
		{http.MethodPost, "/storage/refresh", ""},
		{http.MethodPost, "/toolchains", `{"tool":"node","version":"22"}`},
		{http.MethodDelete, "/toolchains/node/22.11.0", ""},
		{http.MethodPost, "/caches/nuget/clear", ""},
		{http.MethodPost, "/prune/dangling-images", ""},
	} {
		req, err := http.NewRequest(r.method, srv.URL+r.path, strings.NewReader(r.body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Errorf("%s %s: %d", r.method, r.path, resp.StatusCode)
		}
	}
}

func TestStorageErrorsReachTheClient(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	b.storageErr = Conflict("package cache not present: cargo")
	var ae *Error
	if err := c.ClearCache(ctx, "cargo"); !errors.As(err, &ae) || ae.Status != 409 || ae.Msg != "package cache not present: cargo" {
		t.Fatalf("clear: %v", err)
	}
	b.storageErr = &Error{Status: http.StatusBadGateway, Msg: "GET https://api.adoptium.net: 503"}
	if _, err := c.AvailableToolchains(ctx, "java"); !errors.As(err, &ae) || ae.Status != 502 {
		t.Fatalf("available: %v", err)
	}
	b.storageErr = NotFound("toolchain version not installed: node 20.0.0")
	if err := c.RemoveToolchain(ctx, "node", "20.0.0"); !errors.As(err, &ae) || ae.Status != 404 {
		t.Fatalf("remove: %v", err)
	}
	b.storageErr = &Error{Status: http.StatusServiceUnavailable, Msg: "ghr is shutting down"}
	if err := c.InstallToolchain(ctx, "node", "22"); !errors.As(err, &ae) || ae.Status != 503 || ae.Msg != "ghr is shutting down" {
		t.Fatalf("install: %v", err)
	}
	b.storageErr = BadRequest("unknown prune scope \"everything\"")
	if err := c.PruneScope(ctx, "everything"); !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("prune scope: %v", err)
	}
	srv := httptest.NewServer(NewServer(&fakeBackend{}))
	defer srv.Close()
	resp, err := srv.Client().Post(srv.URL+"/toolchains", "application/json", strings.NewReader("{"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad JSON: %d", resp.StatusCode)
	}
}

func TestOlderDaemonAsksForARestartOnStorageRoutes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /prune", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusAccepted) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := &Client{Base: srv.URL, HTTP: &http.Client{Timeout: 5 * time.Second}}
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"prune scope": func() error { return c.PruneScope(ctx, "build-cache-all") },
		"storage":     func() error { _, err := c.Storage(ctx); return err },
		"install":     func() error { return c.InstallToolchain(ctx, "node", "22") },
		"clear":       func() error { return c.ClearCache(ctx, "nuget") },
	} {
		if err := call(); err == nil || !strings.Contains(err.Error(), "systemctl restart ghr") {
			t.Errorf("%s: %v", name, err)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./internal/api/`
Expected: FAIL to compile — `c.Storage undefined`, `c.AvailableToolchains undefined` and the other client methods.

- [ ] **Step 3: Write the implementation**

In `internal/api/server.go`:

1. In the `Backend` interface, after the line `AvailableRepos(ctx context.Context) ([]model.AvailableRepo, error)`, add:

```go
	Storage() model.Storage
	RefreshStorage() error
	AvailableToolchains(ctx context.Context, tool string) ([]model.ToolchainChoice, error)
	InstallToolchain(req model.InstallRequest) error
	RemoveToolchain(tool, version string) error
	ClearCache(name string) error
	PruneScope(scope string) error
```

2. In `NewServer`, directly before the final `return mux`, add:

```go
	mux.HandleFunc("GET /storage", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, b.Storage()) })
	mux.HandleFunc("POST /storage/refresh", accepted(func(*http.Request) error { return b.RefreshStorage() }))
	mux.HandleFunc("GET /toolchains/available", func(w http.ResponseWriter, r *http.Request) {
		cs, err := b.AvailableToolchains(r.Context(), r.URL.Query().Get("tool"))
		if cs == nil {
			cs = []model.ToolchainChoice{}
		}
		respond(w, cs, err)
	})
	mux.HandleFunc("POST /toolchains", accepted(func(r *http.Request) error {
		var req model.InstallRequest
		if err := decode(r, &req); err != nil {
			return err
		}
		return b.InstallToolchain(req)
	}))
	mux.HandleFunc("DELETE /toolchains/{tool}/{version}", accepted(func(r *http.Request) error {
		return b.RemoveToolchain(r.PathValue("tool"), r.PathValue("version"))
	}))
	mux.HandleFunc("POST /caches/{name}/clear", accepted(func(r *http.Request) error { return b.ClearCache(r.PathValue("name")) }))
	// A route of its own, so a daemon older than the scopes answers a
	// plain-text 404 instead of running a standard prune.
	mux.HandleFunc("POST /prune/{scope}", accepted(func(r *http.Request) error { return b.PruneScope(r.PathValue("scope")) }))
```

3. Directly after the `pausePatch` function, add:

```go
// accepted answers 202 once fn has queued or started its work.
func accepted(fn func(r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(r); err != nil {
			respond(w, nil, err)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}
```

In `internal/api/client.go`, directly before the `ResetWebPassword` method, add:

```go
// Storage returns the daemon's storage snapshot; it never measures.
func (c *Client) Storage(ctx context.Context) (model.Storage, error) {
	var s model.Storage
	err := c.call(ctx, http.MethodGet, "/storage", nil, &s)
	return s, err
}

// RefreshStorage asks the daemon to measure now.
func (c *Client) RefreshStorage(ctx context.Context) error {
	return c.call(ctx, http.MethodPost, "/storage/refresh", nil, nil)
}

// AvailableToolchains lists what an install of tool can ask for.
func (c *Client) AvailableToolchains(ctx context.Context, tool string) ([]model.ToolchainChoice, error) {
	var cs []model.ToolchainChoice
	err := c.call(ctx, http.MethodGet, "/toolchains/available?tool="+url.QueryEscape(tool), nil, &cs)
	return cs, err
}

// InstallToolchain queues an install; version is resolved when it runs.
func (c *Client) InstallToolchain(ctx context.Context, tool, version string) error {
	return c.call(ctx, http.MethodPost, "/toolchains", jsonBody(model.InstallRequest{Tool: tool, Version: version}), nil)
}

// InstallPreset queues every install of a preset.
func (c *Client) InstallPreset(ctx context.Context, preset string) error {
	return c.call(ctx, http.MethodPost, "/toolchains", jsonBody(model.InstallRequest{Preset: preset}), nil)
}

// RemoveToolchain queues the removal of an installed version.
func (c *Client) RemoveToolchain(ctx context.Context, tool, version string) error {
	return c.call(ctx, http.MethodDelete, "/toolchains/"+url.PathEscape(tool)+"/"+url.PathEscape(version), nil, nil)
}

// ClearCache queues clearing a package cache.
func (c *Client) ClearCache(ctx context.Context, name string) error {
	return c.call(ctx, http.MethodPost, "/caches/"+url.PathEscape(name)+"/clear", nil, nil)
}

// PruneScope starts a manual prune of one scope; its outcome arrives as events.
func (c *Client) PruneScope(ctx context.Context, scope string) error {
	return c.call(ctx, http.MethodPost, "/prune/"+url.PathEscape(scope), nil, nil)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/api/ ./internal/daemon/`
Expected: both `ok`.

Run: `timeout 600 go test ./...`, `timeout 120 gofmt -l internal cmd`, `timeout 300 go vet ./...`
Expected: every package `ok`; gofmt and vet print nothing.

- [ ] **Step 5: Commit**

```bash
timeout 30 git add internal/api/server.go internal/api/client.go internal/api/api_test.go internal/api/storage_test.go
timeout 30 git commit -m "feat(api): add storage, toolchain and scope routes"
```

### Task 13: Daemon wiring

**Files:**
- Modify: `internal/daemon/run.go` (imports, `Options.Disk`, `InstallTimeout`, `Run`)
- Test: `internal/daemon/run_test.go`

**Interfaces:**
- Consumes: C1 `ExecGroupFor`, `DownloadFor`; C2 `NewEnv`, `toolchain.New`; C5 `Manager.Disk`, `BusyCount`; C6 `PruneDone`; C9/C10 `storage.Service`; C11 `Backend.Space`; C12 client `Storage`, `PruneScope`.
- Produces: C13.

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing test**

In `internal/daemon/run_test.go`:

1. Directly after the `nopDocker` methods (after `func (nopDocker) PruneDanglingImages`), add:

```go
type nopDisk struct{}

func (nopDisk) DiskUsage(context.Context) ([]system.DiskRow, error) {
	return []system.DiskRow{{Type: "Images", Count: 1}}, nil
}
func (nopDisk) BuildCacheUsage(context.Context) ([]system.CacheTypeUsage, error) { return nil, nil }
func (nopDisk) PruneAllBuildCache(context.Context) (string, error)               { return "0B", nil }
func (nopDisk) PruneUnusedVolumes(context.Context) (string, error)               { return "0B", nil }
```

2. Replace every occurrence (there are two) of `Docker: nopDocker{}, Host: dirHost{}` with `Docker: nopDocker{}, Disk: nopDisk{}, Host: dirHost{}` (Edit with replace_all), then check: `timeout 30 grep -c "Disk: nopDisk{}" internal/daemon/run_test.go` prints `2`.

   Replace every occurrence (there are two) of `Home: "/home/ghrunner"}` with `Home: filepath.Join(dir, "home")}` (Edit with replace_all; both literals have `dir` in scope), so the storage service never sweeps or measures a real home; then `timeout 30 grep -c 'Home: filepath.Join(dir, "home")' internal/daemon/run_test.go` prints `2`.

3. Add `"github.com/darkraise/ghr/internal/model"` to the import block, after the `internal/github` import.

4. Append:

```go
func TestRunServesStorageAndSweepsTheToolCache(t *testing.T) {
	o := testOptions(t, cfgYAML)
	left := filepath.Join(o.Paths.ToolCache, ".tmp", "20261006T140000-abcdef", "partial")
	if err := os.MkdirAll(left, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, o) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("Run did not return after cancel")
		}
	}()
	c := api.NewUnixClient(o.Socket)
	var st model.Storage
	waitFor(t, "the first measurement", func() bool {
		var err error
		st, err = c.Storage(context.Background())
		return err == nil && st.MeasuredAt != nil
	})
	if len(st.PackageCaches) != 10 || len(st.Docker.Rows) != 1 || st.Docker.Rows[0].Type != "Images" || st.Toolchains == nil {
		t.Fatalf("storage %+v", st)
	}
	if _, err := os.Stat(filepath.Join(o.Paths.ToolCache, ".tmp")); !os.IsNotExist(err) {
		t.Fatalf(".tmp survived the start: %v", err)
	}
	var ae *api.Error
	if err := c.PruneScope(context.Background(), "everything"); !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("unknown scope: %v", err)
	}
	if err := c.InstallToolchain(context.Background(), "ruby", "3.3"); !errors.As(err, &ae) || ae.Status != 400 {
		t.Fatalf("unknown tool: %v", err)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 300 go test ./internal/daemon/ -run 'RunServesStorage'`
Expected: FAIL to compile — `unknown field Disk in struct literal of type Options`.

- [ ] **Step 3: Write the implementation**

In `internal/daemon/run.go`:

1. Add to the import block, in order with the other internal imports: `"github.com/darkraise/ghr/internal/storage"` and `"github.com/darkraise/ghr/internal/toolchain"`.

2. In `Options`, directly after the line `Docker    runner.Docker`, add the line `Disk      runner.Disk`, and run `timeout 60 gofmt -w internal/daemon/run.go` after the edits below.

3. Directly after the `Options` struct, add:

```go
// InstallTimeout bounds each command and each download of a toolchain
// install, far above system.CommandTimeout: a .NET SDK, a JDK or Python's
// pip step can take longer than that on a slow link.
const InstallTimeout = time.Hour
```

4. In `Run`, change the `runner.Manager` literal's first line to:

```go
		Config: store.Config, GH: gh, SD: o.Systemd, Docker: o.Docker, Disk: o.Disk, Host: o.Host, Fetch: o.Fetch,
```

and directly after the block

```go
	if m.Docker == nil {
		m.Docker = system.Docker{Run: system.Exec}
	}
```

add:

```go
	if m.Disk == nil {
		m.Disk = system.Docker{Run: system.Exec}
	}
```

5. Directly after the line `m.Reconcile(ctx, store.Config())`, add:

```go
	tools := toolchain.New(toolchain.NewEnv(o.Paths.ToolCache, o.Paths.Home, runner.RunnerUser,
		system.ExecGroupFor(InstallTimeout), system.DownloadFor(InstallTimeout)))
	space := &storage.Service{Tools: tools, Docker: m.Disk, Home: o.Paths.Home, Busy: m.BusyCount, Events: ev}
	space.Start()
	m.PruneDone = space.Trigger
```

6. In the `Backend` literal, change `Store: store, M: m, GH: gh, Events: ev, Hist: hist,` to `Store: store, M: m, GH: gh, Events: ev, Hist: hist, Space: space,`.

   In the web UI block, change

```go
		static, err := fs.Sub(web.Dist, "dist")
		if err != nil {
			return err
		}
```

   to

```go
		static, err := fs.Sub(web.Dist, "dist")
		if err != nil {
			space.Close()
			return err
		}
```

7. In the shutdown branch (`case <-ctx.Done():`), change

```go
			m.Close()
			b.Close()
			<-sampled
			done := make(chan struct{})
			go func() { m.Wait(); close(done) }()
```

to

```go
			m.Close()
			space.Close()
			b.Close()
			<-sampled
			done := make(chan struct{})
			go func() {
				m.Wait()
				space.Wait()
				close(done)
			}()
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./internal/daemon/`
Expected: `ok` — the new test and every existing `Run` test pass, and each run shuts down within its wait.

Run: `timeout 600 go test ./...`, `timeout 120 gofmt -l internal cmd`, `timeout 300 go vet ./...`
Expected: every package `ok`; gofmt and vet print nothing.

- [ ] **Step 5: Commit**

```bash
timeout 30 git add internal/daemon/run.go internal/daemon/run_test.go
timeout 30 git commit -m "feat(daemon): start the storage service"
```

### Task 14: CLI commands

**Files:**
- Create: `cmd/ghr/storage.go`
- Modify: `cmd/ghr/cli.go` (`cli` dispatch)
- Modify: `cmd/ghr/main.go` (`usage`)
- Modify: `cmd/ghr/cli_test.go` (`fakeDaemon` routes)
- Test: `cmd/ghr/storage_test.go`

**Interfaces:**
- Consumes: C4 `model.Storage` and its parts, `model.HumanBytes`; C12 client methods, plus the existing `(*api.Client).Prune`; the existing `need`, `usageError`, `fakeDaemon`, `runCLI` (`cmd/ghr/cli.go:26-31`, `cmd/ghr/cli_test.go:23-70`).
- Produces: C14.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

In `cmd/ghr/cli_test.go`, in `fakeDaemon`'s `switch`, directly before `default:`, add:

```go
		case r.URL.Path == "/storage":
			json.NewEncoder(w).Encode(sampleStorage(now))
		case r.URL.Path == "/toolchains/available":
			json.NewEncoder(w).Encode([]model.ToolchainChoice{{Spec: "21", Version: "21.0.8+9", LTS: true}, {Spec: "24", Version: "24.0.2+12"}})
```

Create `cmd/ghr/storage_test.go`:

```go
package main

import (
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

func sampleStorage(now time.Time) model.Storage {
	fin := now.Add(-time.Minute)
	return model.Storage{
		Toolchains:     []model.Toolchain{{Tool: "node", Version: "22.11.0", Arch: "x64", Bytes: 180_000_000, InstalledAt: now}},
		OtherToolCache: []model.Folder{{Name: "PyPy", Bytes: 300_000_000}},
		PackageCaches: []model.PackageCache{
			{Name: "nuget", Label: "NuGet", Paths: []string{"/home/ghrunner/.nuget/packages"}, Present: true, Bytes: 1_200_000_000, Files: 1234, LastWritten: &now},
			{Name: "cargo", Label: "Cargo", Paths: []string{"/home/ghrunner/.cargo/registry", "/home/ghrunner/.cargo/git"}},
		},
		Docker: model.DockerDisk{
			DiskPct: 61,
			Rows: []model.DockerRow{
				{Type: "Images", Count: 9, Active: 2, Bytes: 6_571_000_000, Reclaimable: 5_627_000_000},
				{Type: "Build Cache", Count: 4, Bytes: 3_798_512_500, Reclaimable: 2_568_512_500},
			},
			BuildCacheTypes: []model.BuildCacheType{{Type: "exec.cachemount", Count: 1, Bytes: 2_100_000_000, Reclaimable: 2_100_000_000}},
		},
		MeasuredAt:   &now,
		MeasureError: "docker build cache: boom",
		Operations: model.Operations{
			Current: &model.Operation{ID: "o2", Kind: "install", Target: "node 24", StartedAt: now, Progress: "extracting"},
			Queued:  2,
			Recent:  []model.Operation{{ID: "o1", Kind: "clear", Target: "npm", StartedAt: fin, FinishedAt: &fin, Outcome: "refused", Message: "refused: 1 jobs running"}},
		},
		LastPrune: &model.LastPrune{Trigger: "manual", Scope: "build-cache-all", StartedAt: fin, FinishedAt: &fin, Outcome: "errors",
			Steps: []model.PruneStep{{Name: "all build cache", Freed: 3_100_000_000}, {Name: "disk usage", Error: "df failed"}}},
	}
}

func TestStorageCommandPrintsEverySection(t *testing.T) {
	reqs := fakeDaemon(t)
	code, out, errb := runCLI(t, "", "storage")
	if code != 0 {
		t.Fatalf("code %d stderr %s", code, errb)
	}
	for _, want := range []string{
		"DOCKER DISK (disk 61%)", "Images", "6.6 GB", "5.6 GB", "  exec.cachemount", "2.1 GB",
		"last prune: manual · build-cache-all", "errors — all build cache 3.1 GB, disk usage failed: df failed",
		"TOOLCHAINS", "22.11.0", "180.0 MB", "OTHER TOOL CACHE", "PyPy", "300.0 MB",
		"PACKAGE CACHES", "nuget", "1.2 GB", "1234", "/home/ghrunner/.nuget/packages", "cargo", "not present",
		"measure error: docker build cache: boom",
		"running: install node 24 — extracting (2 queued)", "refused: 1 jobs running",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if got := (*reqs)[0]; got.method != "GET" || got.path != "/storage" {
		t.Fatalf("request %+v", got)
	}
}

func TestPruneLineWithoutAPrune(t *testing.T) {
	if got := pruneLine(nil); got != "last prune: none since ghr started" {
		t.Fatalf("line %q", got)
	}
	start := time.Date(2026, 10, 6, 14, 0, 0, 0, time.UTC)
	if got := pruneLine(&model.LastPrune{Trigger: "auto", Scope: "auto", StartedAt: start}); !strings.HasSuffix(got, " · pruning…") {
		t.Fatalf("running %q", got)
	}
}

func TestStorageMutationCommands(t *testing.T) {
	for _, tc := range []struct {
		args               []string
		method, path, body string
		out                string
	}{
		{[]string{"storage", "refresh"}, "POST", "/storage/refresh", "", "measuring — follow with: ghr storage"},
		{[]string{"toolchain", "install", "node", "22"}, "POST", "/toolchains", `{"tool":"node","version":"22"}`, queuedNote},
		{[]string{"toolchain", "install", "--preset", "popular"}, "POST", "/toolchains", `{"preset":"popular"}`, queuedNote},
		{[]string{"toolchain", "rm", "java", "21.0.8+9"}, "DELETE", "/toolchains/java/21.0.8+9", "", queuedNote},
		{[]string{"cache", "clear", "nuget"}, "POST", "/caches/nuget/clear", "", queuedNote},
		{[]string{"prune"}, "POST", "/prune", "", "prune started — follow with: ghr storage"},
		{[]string{"prune", "--scope", "unused-volumes"}, "POST", "/prune/unused-volumes", "", "prune started — follow with: ghr storage"},
	} {
		reqs := fakeDaemon(t)
		code, out, errb := runCLI(t, "", tc.args...)
		if code != 0 {
			t.Errorf("%v: code %d stderr %s", tc.args, code, errb)
			continue
		}
		if len(*reqs) != 1 {
			t.Errorf("%v: %d requests", tc.args, len(*reqs))
			continue
		}
		got := (*reqs)[0]
		if got.method != tc.method || got.path != tc.path || strings.TrimSpace(got.body) != tc.body {
			t.Errorf("%v: request %+v", tc.args, got)
		}
		if strings.TrimSpace(out) != tc.out {
			t.Errorf("%v: output %q", tc.args, out)
		}
	}
}

func TestToolchainAndCacheListings(t *testing.T) {
	reqs := fakeDaemon(t)
	code, out, _ := runCLI(t, "", "toolchain", "list")
	if code != 0 || !strings.Contains(out, "22.11.0") || !strings.Contains(out, "PyPy") || strings.Contains(out, "PACKAGE CACHES") {
		t.Fatalf("toolchain list:\n%s", out)
	}
	code, out, _ = runCLI(t, "", "toolchain", "available", "java")
	if code != 0 || !strings.Contains(out, "21.0.8+9") || !strings.Contains(out, "lts") || !strings.Contains(out, "24.0.2+12") {
		t.Fatalf("toolchain available:\n%s", out)
	}
	if got := (*reqs)[1].path; got != "/toolchains/available?tool=java" {
		t.Fatalf("available request %s", got)
	}
	code, out, _ = runCLI(t, "", "cache", "list")
	if code != 0 || !strings.Contains(out, "nuget") || !strings.Contains(out, "not present") || strings.Contains(out, "TOOLCHAINS") {
		t.Fatalf("cache list:\n%s", out)
	}
}

func TestStorageCommandUsageErrors(t *testing.T) {
	fakeDaemon(t)
	for _, args := range [][]string{
		{"storage", "bogus"}, {"toolchain"}, {"toolchain", "install", "node"}, {"toolchain", "install", "--preset"},
		{"toolchain", "rm", "node"}, {"toolchain", "bogus"}, {"cache"}, {"cache", "clear"}, {"prune", "extra"},
	} {
		code, _, errb := runCLI(t, "", args...)
		if code != 2 || !strings.Contains(errb, "usage:") {
			t.Errorf("%v: code %d stderr %q", args, code, errb)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test ./cmd/ghr/`
Expected: FAIL to compile — `undefined: queuedNote`, `undefined: pruneLine`.

- [ ] **Step 3: Write the implementation**

Create `cmd/ghr/storage.go`:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/darkraise/ghr/internal/api"
	"github.com/darkraise/ghr/internal/model"
)

const queuedNote = "queued — follow with: ghr storage"

func storageCmd(ctx context.Context, c *api.Client, args []string, out io.Writer) error {
	switch {
	case len(args) == 0:
		st, err := c.Storage(ctx)
		if err != nil {
			return err
		}
		printStorage(out, st)
		return nil
	case len(args) == 1 && args[0] == "refresh":
		if err := c.RefreshStorage(ctx); err != nil {
			return err
		}
		fmt.Fprintln(out, "measuring — follow with: ghr storage")
		return nil
	}
	return usageError("usage: ghr storage [refresh]")
}

func toolchainCmd(ctx context.Context, c *api.Client, args []string, out io.Writer) error {
	if len(args) == 0 {
		return usageError("usage: ghr toolchain <list|available|install|rm> ...")
	}
	switch args[0] {
	case "list":
		if err := need(args, 1, "toolchain list"); err != nil {
			return err
		}
		st, err := c.Storage(ctx)
		if err != nil {
			return err
		}
		printToolchains(out, st)
		return nil
	case "available":
		if err := need(args, 2, "toolchain available <tool>"); err != nil {
			return err
		}
		cs, err := c.AvailableToolchains(ctx, args[1])
		if err != nil {
			return err
		}
		w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
		fmt.Fprintln(w, "SPEC\tVERSION\tLTS")
		for _, ch := range cs {
			lts := ""
			if ch.LTS {
				lts = "lts"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\n", ch.Spec, ch.Version, lts)
		}
		return w.Flush()
	case "install":
		var err error
		switch {
		case len(args) == 3 && args[1] == "--preset":
			err = c.InstallPreset(ctx, args[2])
		case len(args) == 3 && !strings.HasPrefix(args[1], "-"):
			err = c.InstallToolchain(ctx, args[1], args[2])
		default:
			return usageError("usage: ghr toolchain install <tool> <version> | --preset popular")
		}
		if err != nil {
			return err
		}
		fmt.Fprintln(out, queuedNote)
		return nil
	case "rm":
		if err := need(args, 3, "toolchain rm <tool> <version>"); err != nil {
			return err
		}
		if err := c.RemoveToolchain(ctx, args[1], args[2]); err != nil {
			return err
		}
		fmt.Fprintln(out, queuedNote)
		return nil
	}
	return usageError("unknown toolchain command " + args[0])
}

func cacheCmd(ctx context.Context, c *api.Client, args []string, out io.Writer) error {
	if len(args) == 0 {
		return usageError("usage: ghr cache <list|clear> ...")
	}
	switch args[0] {
	case "list":
		if err := need(args, 1, "cache list"); err != nil {
			return err
		}
		st, err := c.Storage(ctx)
		if err != nil {
			return err
		}
		printCaches(out, st)
		fmt.Fprintln(out, measuredLine(st))
		return nil
	case "clear":
		if err := need(args, 2, "cache clear <name>"); err != nil {
			return err
		}
		if err := c.ClearCache(ctx, args[1]); err != nil {
			return err
		}
		fmt.Fprintln(out, queuedNote)
		return nil
	}
	return usageError("unknown cache command " + args[0])
}

func pruneCmd(ctx context.Context, c *api.Client, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	scope := fs.String("scope", "", "")
	if err := fs.Parse(args); err != nil {
		return usageError(err.Error())
	}
	if fs.NArg() > 0 {
		return usageError("unexpected argument " + fs.Arg(0))
	}
	var err error
	if *scope == "" {
		err = c.Prune(ctx)
	} else {
		err = c.PruneScope(ctx, *scope)
	}
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "prune started — follow with: ghr storage")
	return nil
}

func stamp(t time.Time) string { return t.Local().Format("2006-01-02 15:04") }

func printStorage(out io.Writer, st model.Storage) {
	fmt.Fprintf(out, "DOCKER DISK (disk %d%%)\n", st.Docker.DiskPct)
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "TYPE\tCOUNT\tACTIVE\tSIZE\tRECLAIMABLE")
	for _, r := range st.Docker.Rows {
		fmt.Fprintf(w, "%s\t%d\t%d\t%s\t%s\n", r.Type, r.Count, r.Active, model.HumanBytes(r.Bytes), model.HumanBytes(r.Reclaimable))
		if r.Type == "Build Cache" {
			for _, ct := range st.Docker.BuildCacheTypes {
				fmt.Fprintf(w, "  %s\t%d\t-\t%s\t%s\n", ct.Type, ct.Count, model.HumanBytes(ct.Bytes), model.HumanBytes(ct.Reclaimable))
			}
		}
	}
	w.Flush()
	fmt.Fprintln(out, pruneLine(st.LastPrune))
	fmt.Fprintln(out)
	printToolchains(out, st)
	fmt.Fprintln(out)
	printCaches(out, st)
	fmt.Fprintln(out, measuredLine(st))
	fmt.Fprintln(out)
	printOperations(out, st.Operations)
}

func pruneLine(lp *model.LastPrune) string {
	if lp == nil {
		return "last prune: none since ghr started"
	}
	head := fmt.Sprintf("last prune: %s · %s · %s", lp.Trigger, lp.Scope, stamp(lp.StartedAt))
	if lp.FinishedAt == nil {
		return head + " · pruning…"
	}
	var steps []string
	for _, s := range lp.Steps {
		if s.Error != "" {
			steps = append(steps, s.Name+" failed: "+s.Error)
		} else {
			steps = append(steps, s.Name+" "+model.HumanBytes(s.Freed))
		}
	}
	line := head + " · " + lp.Outcome
	if len(steps) > 0 {
		line += " — " + strings.Join(steps, ", ")
	}
	return line
}

// printToolchains prints the installed toolchains and the tool-cache
// folders no installer owns, which have no Remove.
func printToolchains(out io.Writer, st model.Storage) {
	fmt.Fprintln(out, "TOOLCHAINS")
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "TOOL\tVERSION\tSIZE\tINSTALLED")
	for _, tc := range st.Toolchains {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", tc.Tool, tc.Version, model.HumanBytes(tc.Bytes), stamp(tc.InstalledAt))
	}
	w.Flush()
	if len(st.OtherToolCache) == 0 {
		return
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "OTHER TOOL CACHE")
	w = tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSIZE")
	for _, f := range st.OtherToolCache {
		fmt.Fprintf(w, "%s\t%s\n", f.Name, model.HumanBytes(f.Bytes))
	}
	w.Flush()
}

func printCaches(out io.Writer, st model.Storage) {
	fmt.Fprintln(out, "PACKAGE CACHES")
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSIZE\tFILES\tLAST WRITTEN\tPATH")
	for _, pc := range st.PackageCaches {
		paths := strings.Join(pc.Paths, ", ")
		if !pc.Present {
			fmt.Fprintf(w, "%s\tnot present\t\t\t%s\n", pc.Name, paths)
			continue
		}
		last := "-"
		if pc.LastWritten != nil {
			last = stamp(*pc.LastWritten)
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", pc.Name, model.HumanBytes(pc.Bytes), pc.Files, last, paths)
	}
	w.Flush()
}

func measuredLine(st model.Storage) string {
	var s string
	switch {
	case st.Measuring:
		s = "measuring…"
	case st.MeasuredAt == nil:
		s = "not measured yet"
	default:
		s = "measured " + stamp(*st.MeasuredAt)
	}
	if st.MeasureError != "" {
		s += "  measure error: " + st.MeasureError
	}
	return s
}

func printOperations(out io.Writer, ops model.Operations) {
	if c := ops.Current; c != nil {
		line := "running: " + c.Kind + " " + c.Target
		if c.Progress != "" {
			line += " — " + c.Progress
		}
		fmt.Fprintf(out, "%s (%d queued)\n", line, ops.Queued)
	} else {
		fmt.Fprintf(out, "no operation running (%d queued)\n", ops.Queued)
	}
	if len(ops.Recent) == 0 {
		return
	}
	w := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "FINISHED\tKIND\tTARGET\tOUTCOME\tMESSAGE")
	for _, o := range ops.Recent {
		finished := "-"
		if o.FinishedAt != nil {
			finished = stamp(*o.FinishedAt)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", finished, o.Kind, o.Target, o.Outcome, o.Message)
	}
	w.Flush()
}
```

In `cmd/ghr/cli.go`, in `cli`'s `switch args[0]`, directly before `case "web":`, add:

```go
	case "storage":
		return storageCmd(ctx, c, args[1:], out)
	case "toolchain":
		return toolchainCmd(ctx, c, args[1:], out)
	case "cache":
		return cacheCmd(ctx, c, args[1:], out)
	case "prune":
		return pruneCmd(ctx, c, args[1:], out)
```

In `cmd/ghr/main.go`, in `usage`, directly before the line `  web reset-password              forget the web UI password`, add:

```
  storage [refresh]               disk use: Docker, toolchains, package caches
  toolchain list | available <tool>
  toolchain install <tool> <version> | --preset popular
  toolchain rm <tool> <version>   refused while jobs run
  cache list | clear <name>       clear is refused while jobs run
  prune [--scope <scope>]         standard (default), build-cache-keep,
                                  build-cache-all, dangling-images, unused-volumes
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test ./cmd/ghr/`
Expected: `ok`.

Run: `timeout 600 go test ./...`, `timeout 120 gofmt -l internal cmd`, `timeout 300 go vet ./...`
Expected: every package `ok`; gofmt and vet print nothing.

- [ ] **Step 5: Commit**

```bash
timeout 30 git add cmd/ghr/storage.go cmd/ghr/storage_test.go cmd/ghr/cli.go cmd/ghr/cli_test.go cmd/ghr/main.go
timeout 30 git commit -m "feat(cli): add storage, toolchain, cache and prune"
```

### Task 15: setup.sh toolchain pre-install

**Files:**
- Modify: `D:/Repositories/Personal/homelab/github-runner/setup.sh` (header, globals, `warn`, `install_docker`, `install_config`, new `check_toolchains` and `preinstall_toolchains`, `main`)
- Modify: `D:/Repositories/Personal/homelab/github-runner/tests/setup_test.sh` (`ghr` stub, new sections f and g)
- Modify: `D:/Repositories/Personal/homelab/github-runner/README.md` (new section)

**Interfaces:**
- Consumes: C14 command `ghr toolchain install --preset popular`.
- Produces: the `GHR_TOOLCHAINS` setting (`popular` or `none`).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

Every command in this task runs from `D:/Repositories/Personal/homelab/github-runner`. Edit the files with the Edit tool.

- [ ] **Step 1: Write the failing tests**

In `tests/setup_test.sh`, replace the `ghr()` stub (from `ghr() {` to its closing `}`) with:

```bash
ghr() {
  if [ "$1" = toolchain ]; then
    echo "$*" >> "$MARK_DIR/ghr-calls"
    [ ! -f "$MARK_DIR/fail-queue" ]
    return
  fi
  bump polls
  if [ "$1" = status ] && [ "$(count polls)" -ge "$READY_AFTER" ]; then
    echo "ghr status: ready"
    return 0
  fi
  echo "daemon unreachable" >&2
  return 1
}
```

Directly before the line `# (c) Dist GC.`, add:

```bash
# (f) The apt step installs the libraries the python-versions builds link.
new_case f-apt
out=$( (
  apt-get() { echo "apt-get $*" >> "$MARK_DIR/apt"; }
  curl() { :; }
  install() { :; }
  chmod() { :; }
  write_docker_source() { :; }
  systemctl() { :; }
  docker() { :; }
  run install_docker
) 2>&1)
rc=$?
check "f: install_docker succeeds with stubs" [ "$rc" -eq 0 ]
apt=$(cat "$MARK_DIR/apt" 2>/dev/null)
for lib in libssl3t64 libffi8 libsqlite3-0 liblzma5 libbz2-1.0 libgdbm6t64 libncursesw6 libreadline8t64; do
  check "f: apt installs $lib" contains "$apt" " $lib"
done

# (g) A first install queues the popular toolchains; an upgrade does not.
new_case g-first-install
ETC_DIR="$T/g-first-install/etc"
mkdir -p "$ETC_DIR"
out=$( (
  set -e
  GHR_TOKEN=tok install_config >/dev/null
  echo "first=$FIRST_INSTALL"
  FIRST_INSTALL=0
  GHR_TOKEN=tok install_config >/dev/null
  echo "again=$FIRST_INSTALL"
) 2>&1)
check "g: a missing config.yaml is a first install" contains "$out" "first=1"
check "g: an existing config.yaml is an upgrade" contains "$out" "again=0"

new_case g-preinstall
: > "$MARK_DIR/ghr-calls"
out=$( (FIRST_INSTALL=1; GHR_TOOLCHAINS=; run preinstall_toolchains) 2>&1)
check "g: a first install queues the popular preset" [ "$(cat "$MARK_DIR/ghr-calls")" = "toolchain install --preset popular" ]
check "g: says the installs continue in the background" contains "$out" "follow with: ghr storage"
: > "$MARK_DIR/ghr-calls"
(FIRST_INSTALL=0; GHR_TOOLCHAINS=; run preinstall_toolchains) >/dev/null 2>&1
check "g: an upgrade queues nothing" [ ! -s "$MARK_DIR/ghr-calls" ]
: > "$MARK_DIR/ghr-calls"
(FIRST_INSTALL=0; GHR_TOOLCHAINS=popular; run preinstall_toolchains) >/dev/null 2>&1
check "g: GHR_TOOLCHAINS=popular queues on an upgrade" [ -s "$MARK_DIR/ghr-calls" ]
: > "$MARK_DIR/ghr-calls"
(FIRST_INSTALL=1; GHR_TOOLCHAINS=none; run preinstall_toolchains) >/dev/null 2>&1
check "g: GHR_TOOLCHAINS=none skips a first install" [ ! -s "$MARK_DIR/ghr-calls" ]
touch "$MARK_DIR/fail-queue"
out=$( (FIRST_INSTALL=1; GHR_TOOLCHAINS=; run preinstall_toolchains) 2>&1)
rc=$?
check "g: a failed queue does not fail setup" [ "$rc" -eq 0 ]
check "g: a failed queue warns" contains "$out" "warning:"
rm -f "$MARK_DIR/fail-queue"
out=$( (GHR_TOOLCHAINS=all; run check_toolchains) 2>&1)
rc=$?
check "g: an unknown GHR_TOOLCHAINS is rejected" [ "$rc" -ne 0 ]
check "g: names the allowed values" contains "$out" "popular or none"
out=$( (GHR_TOOLCHAINS=none; run check_toolchains) 2>&1)
rc=$?
check "g: GHR_TOOLCHAINS=none is accepted" [ "$rc" -eq 0 ]
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 120 bash tests/setup_test.sh`
Expected: exit 1; `FAIL` lines for every `f: apt installs …` check and for the `g:` checks that need the new code (`preinstall_toolchains` and `check_toolchains` are not defined, and `FIRST_INSTALL` is unset). Three `g:` checks print `ok` already, because a missing function also queues nothing and exits non-zero: "an upgrade queues nothing", "GHR_TOOLCHAINS=none skips a first install" and "an unknown GHR_TOOLCHAINS is rejected". Every earlier check still prints `ok`.

- [ ] **Step 3: Write the implementation**

In `setup.sh`:

1. Change the header line `# Optional: GHR_VERSION=v0.1.1 RUNNER_VERSION=2.337.0` to the two lines:

```bash
# Optional: GHR_VERSION=v0.1.1 RUNNER_VERSION=2.337.0
#   GHR_TOOLCHAINS=popular|none (default: popular on a first install, none on an upgrade)
```

2. Directly after the line `RUNNER_VERSION="${RUNNER_VERSION:-latest}"`, add:

```bash
GHR_TOOLCHAINS="${GHR_TOOLCHAINS:-}"
# Set by install_config when config.yaml did not exist yet.
FIRST_INSTALL=0
# The python-versions builds setup-python installs link these at run time.
PYTHON_RUNTIME_LIBS=(libssl3t64 libffi8 libsqlite3-0 liblzma5 libbz2-1.0 libgdbm6t64 libncursesw6 libreadline8t64)
```

3. Directly after the `die()` line, add:

```bash
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
```

4. In `install_docker`, change the line `    git curl jq unzip build-essential ca-certificates` to:

```bash
    git curl jq unzip build-essential ca-certificates "${PYTHON_RUNTIME_LIBS[@]}"
```

5. In `install_config`, directly after the line `    log "wrote $ETC_DIR/config.yaml from config.example.yaml — review owner and repos"`, add:

```bash
    FIRST_INSTALL=1
```

6. Directly before `main() {`, add:

```bash
check_toolchains() {
  case "$GHR_TOOLCHAINS" in
    "" | popular | none) ;;
    *) die "GHR_TOOLCHAINS must be popular or none (got: $GHR_TOOLCHAINS)" ;;
  esac
}

# Queues the popular toolchains on a first install, or when GHR_TOOLCHAINS
# asks, and returns at once: the daemon installs them in the background. A
# toolchain the owner removed is not queued again by an upgrade.
preinstall_toolchains() {
  local mode="$GHR_TOOLCHAINS"
  if [ -z "$mode" ]; then
    mode=none
    [ "$FIRST_INSTALL" != 1 ] || mode=popular
  fi
  [ "$mode" = popular ] || return 0
  if timeout 30 ghr toolchain install --preset popular; then
    log "popular toolchains queued; they install in the background — follow with: ghr storage"
  else
    warn "could not queue the popular toolchains; queue them later with: ghr toolchain install --preset popular"
  fi
}

```

7. In `main`, add the line `  check_toolchains` directly after `  check_host`, and the line `  preinstall_toolchains` directly after `  gc_dist`.

In `README.md`, directly before the line `## Migrating from the compose runners`, add:

```markdown
## Toolchains and caches

ghr installs toolchains into the runners' shared tool cache (`/var/lib/ghr/toolcache`) in the layout each `setup-*` action reads, so `setup-node`, `setup-python`, `setup-go`, `setup-java` (Temurin) and `setup-dotnet` use them instead of downloading on every job.

- `ghr storage` shows Docker's disk (images, containers, volumes, and the build cache by type), the last prune, the toolchains, other tool-cache folders, the package caches and the running operation; `ghr storage refresh` measures now.
- `ghr toolchain list`; `ghr toolchain available <tool>`, for `node`, `go`, `python`, `java` or `dotnet`.
- `ghr toolchain install <tool> <version>` takes a full version, a partial one (`22`, `3.13`), `latest`, a Java major (`21`) or a .NET channel (`8.0`). `ghr toolchain install --preset popular` queues Node 22 and 24, .NET 8.0 and 10.0, Python 3.13 and 3.14, Go latest, and Java 21 and 25.
- `ghr toolchain rm <tool> <version>`; `ghr cache list`; `ghr cache clear <name>`, for `nuget`, `npm`, `pnpm`, `yarn`, `pip`, `gomod`, `gobuild`, `maven`, `gradle` or `cargo`.
- `ghr prune [--scope <scope>]`: `standard` (the default), `build-cache-keep`, `build-cache-all`, `dangling-images` or `unused-volumes` (anonymous volumes no container uses).

Installs, removals and clears queue and run one at a time; follow them with `ghr storage`. Removing a toolchain, clearing a cache and `--scope unused-volumes` are refused while any runner has a job.

`setup.sh` queues the popular set on a first install and returns without waiting. `GHR_TOOLCHAINS=popular` or `GHR_TOOLCHAINS=none` overrides that either way. On an LXC installed before this version, run `ghr toolchain install --preset popular` once.

Python must come from the tool cache on Debian: without a cached version, `setup-python` matches its download against `VERSION_ID` in `/etc/os-release`, and every build it offers targets Ubuntu, so on Debian 13 it finds none. The apt step installs the libraries those builds link.

Removing the last .NET SDK of a major version also removes that major's runtimes and packs, so a job that builds with a newer SDK but runs `net8.0` tests loses them.

```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 120 bash tests/setup_test.sh`
Expected: every line `ok`, ending `all assertions passed`, exit 0.

If `shellcheck` is on PATH, also run `timeout 60 shellcheck setup.sh tests/setup_test.sh`; expected: no output.

- [ ] **Step 5: Commit**

```bash
cd /d/Repositories/Personal/homelab
timeout 30 git add github-runner/setup.sh github-runner/tests/setup_test.sh github-runner/README.md
timeout 30 git commit -m "feat(github-runner): pre-install popular toolchains"
```

Expected: `timeout 30 git status --short github-runner` afterwards prints nothing.

### Task 16: Final verification

**Files:**
- None created or modified, unless a check fails; a fix is its own commit, `fix(<scope>): <subject>`, in the repository it belongs to.

**Interfaces:**
- Consumes: every earlier task.
- Produces: the verification evidence the final review reads.

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 0 - spec 0 - coupling 1 - risk 0 = 1

- [ ] **Step 1: Run the whole suite on Windows**

From `D:/Repositories/Personal/ghr-toolchains`:

```bash
timeout 900 go test -count=1 ./...
timeout 120 gofmt -l internal cmd
timeout 300 go vet ./...
```

Expected: every package `ok`; gofmt and vet print nothing.

- [ ] **Step 2: Run the Unix-only tests as root in WSL**

Each package's test binary runs from its own source directory (the `system` tests read `testdata/`). `MSYS_NO_PATHCONV=1` stops Git Bash from rewriting the `/mnt/...` paths it hands to `wsl.exe`.

```bash
out=/d/Repositories/Personal/ghr-wsltest
mkdir -p "$out"
for p in system toolchain storage runner daemon api model ghr; do
  dir=internal/$p
  [ "$p" = ghr ] && dir=cmd/ghr
  GOOS=linux GOARCH=amd64 timeout 300 go test -c -o "$out/$p.test" ./$dir || echo "BUILD FAIL $p"
done
for p in system toolchain storage runner daemon api model ghr; do
  dir=internal/$p
  [ "$p" = ghr ] && dir=cmd/ghr
  echo "== $p"
  MSYS_NO_PATHCONV=1 timeout 600 wsl -d dev -u root -- sh -c "cd /mnt/d/Repositories/Personal/ghr-toolchains/$dir && /mnt/d/Repositories/Personal/ghr-wsltest/$p.test -test.count=1 -test.v > /tmp/ghr-$p.log 2>&1; echo exit=\$?; printf 'pass %s skip %s fail %s\n' \$(grep -c -- '--- PASS' /tmp/ghr-$p.log) \$(grep -c -- '--- SKIP' /tmp/ghr-$p.log) \$(grep -c -- '--- FAIL' /tmp/ghr-$p.log)"
done
timeout 60 wsl --terminate dev
rm -rf "$out"
```

Expected: no `BUILD FAIL`; every package prints `exit=0` and `fail 0`. Record each package's pass/skip/fail line in the ledger. A failure in a test this plan did not add or change is reported to the owner, not fixed here.

- [ ] **Step 3: Run the setup.sh tests**

```bash
cd /d/Repositories/Personal/homelab/github-runner
timeout 120 bash tests/setup_test.sh
```

Expected: `all assertions passed`.

- [ ] **Step 4: Confirm nothing is left running or uncommitted**

```bash
cd /d/Repositories/Personal/ghr-toolchains && timeout 30 git status --short
cd /d/Repositories/Personal/homelab && timeout 30 git status --short github-runner
timeout 30 wsl -l --running
```

Expected: the ghr worktree is clean; homelab's `github-runner/` prints nothing; `dev` is not among the running distributions. Nothing is pushed: `-race` runs in CI only when the owner approves the push at finishing.
