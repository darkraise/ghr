# Daemon Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close register rows 19 to 45 except 33, and row 209: keep the history file readable, file job log output under the right step, stop caches and the token verdict from outliving what they were based on, bound the values and listings that have no limit, and give a public repository no runners unless it is allowed.

**Architecture:** Targeted fixes, package by package, leaf packages first (`internal/lru`, `internal/github`, `internal/joblog`, `internal/wfgraph`, `internal/history`, `internal/metrics`, `internal/config`), then the runner manager's public-repository state, then `internal/daemon` and the few web files that show the new answers. The only structural move is the cache type, which becomes `internal/lru` because the GitHub client now needs it too.

**Tech Stack:** Go 1.26 (standard library plus `gopkg.in/yaml.v3`, already a dependency), TypeScript and React with Vitest under `web/`.

**Spec:** docs/superpowers/specs/2026-10-11-ghr-daemon-fixes-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 10 of 30 tasks are heavy (3, 4, 9, 14, 16, 17, 18, 21, 24, 26) and are delegated; the other 20 run in the session

**Plan review:** 2026-10-11 — dr-superpowers:judge-opus — executability 18 / coherence 18 / coverage 19 / assumptions 18 (round 2)

**Parallelism:** sequential — 15 of the 26 register rows edit `internal/daemon`, most of them `backend.go`, and later tasks use names earlier ones add

## Global Constraints

- Go is at `/usr/local/go/bin` and not on PATH: prefix every Go command with `PATH=/usr/local/go/bin:$PATH`. If the shell refuses that prefix, call `/usr/local/go/bin/go` and `/usr/local/go/bin/gofmt` by their absolute paths instead.
- Before each commit run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (must print nothing) and the test command the task names.
- Go tests use the package's existing fakes and clocks. No new test calls `time.Sleep`. Where a result arrives on another goroutine, a test may use the polling helpers `internal/daemon`'s tests already have (`waitFor`, `scanCounts`, `waitLabelCheck`), and a `time.After` guard that only elapses when the test would otherwise hang is allowed.
- `npx` and `npm` commands run in `web/` (`cd web` first); `git`, `go` and `gofmt` commands run from the repository root, and a task's `git add` paths are relative to the root. One web test file runs with `npx vitest run <path>`; before the commit of any task that touches `web/`, run `npm run lint` and `npm run typecheck` in `web/`.
- Event texts, error messages and UI copy are part of the spec. Copy them exactly as the task gives them.
- A task's "replace these lines" blocks were written against commit `406202f` plus the tasks before it. When a block to replace is not found exactly as quoted, find the same lines by their content, apply the same change, and say so in the task report; never skip the change.
- When an existing test fails because it asserted behaviour this plan changes, the task says so and gives the corrected lines. Report any other existing-test failure in the task report; never change production code to make an old expectation pass.
- Code comments: only where the reason is not obvious from the code. No comment mentions a task, a register row or a fix.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no trailing period, no attribution or co-author lines.
- Never deploy to the runner LXC (192.168.0.99).
- English only.

## Contracts

Names one task adds and a later task uses.

- Package `github.com/darkraise/ghr/internal/lru` (Task 1): `type Cache[K comparable, V any] struct`; `func New[K comparable, V any](maxEntries int, maxSize int64) *Cache[K, V]`; methods `Get(k K) (V, bool)`, `Add(k K, v V, size int64)`, `Remove(k K)`. Bounded by entry count and, when `maxSize` is above 0, by the total of the sizes given to `Add`; the least recently used entry goes first; an entry larger than `maxSize` alone is not kept. Callers hold their own lock. After Task 1 the daemon's caches (`logCache.parsed`, `logCache.notes`, `wfCache.files`) are `*lru.Cache[...]` and call `Get`, `Add` and `Remove`.
- `Client.Sleep func(ctx context.Context, d time.Duration) error` in `internal/github` (Task 4).
- `github.ErrBadContent` (Task 5): sentinel `error` with the text `github: file content cannot be decoded`; `GetContent` wraps it with `%w` for an `encoding` other than `base64` and for undecodable base64.
- `wfFile.bad bool` in `internal/daemon/rungraph.go` (Task 6): the workflow file at that commit cannot be decoded; kept without expiry.
- `Client.OpenJobLog` and `Client.JobLog` in `internal/github` (Task 8) answer a job that has no log with an `*APIError` of kind `ErrNotFound`, which `github.IsKind(err, github.ErrNotFound)` recognises.
- `logNone` (Task 9): a `logOutcome` in `internal/daemon/joblogs.go` for a completed job whose log is not found; stored among the log misses. `model.JobLog.NoLog bool` with the JSON key `no_log` (Task 9); `web/src/api/fixtures/job-log.json` carries `"no_log": false` from Task 9 on. TypeScript `JobLog.no_log: boolean` (Task 10).
- `noteNoJobs = "This run has no jobs yet."` in `internal/daemon/rungraph.go` (Task 11).
- `config.MaxRunners = 99` (Task 14).
- `config.Repo.AllowPublic bool` with the tags `yaml:"allow_public,omitempty" json:"allow_public,omitempty"` (Task 16).
- In `internal/runner` (Task 17): the `runner.GitHub` interface gains `GetRepo(ctx context.Context, repo string) (*github.Repository, error)`; `fakeGH` in `internal/runner/fakes_test.go` implements it and has `setPublic(repo string, public bool)`. In `internal/runner/visibility.go`: the Manager field `vis`, per-repository visibility state keyed by the lower-cased repository name; `func (m *Manager) visibilityLocked(repo string) (public, known bool)` (`m.mu` held); `const visibilityTTL = 6 * time.Hour`; `func (m *Manager) readVisibility(ctx context.Context, repo string, now time.Time) error`; `func (m *Manager) forgetRemoved(cfg *config.Config)`.
- In `internal/runner/visibility.go` (Task 18): `func (m *Manager) blocked(cfg *config.Config, repo config.Repo) bool`; `func (m *Manager) blockedLocked(cfg *config.Config, repo config.Repo) bool`, the same test for callers that hold `m.mu`; `func (m *Manager) unread(repo config.Repo) bool`; `func (m *Manager) planConfig(cfg *config.Config) *config.Config`, which returns a copy of cfg with `Paused` set on every blocked repository.
- Test helpers in `internal/runner/blocked_test.go` (Task 18), used again by Task 19: `func allowPublic(h *harness, repo string)` sets `allow_public` for repo on the harness config and calls `h.m.ClearDegraded()`, as a reload of config.yaml does; `func homelabJob(h *harness, id int64) github.Job` returns a queued job with the label `homelab` created at `h.now`; `func spawnsFor(h *harness, repo string) int` counts the `GenerateJITConfig` calls for repo.
- `model.OptInt` (Task 20): `type OptInt struct { Set bool; Value *int }`. `Set` false means the key was absent; `Set` true with `Value` nil means JSON `null` (clear the value); `Set` true with `Value` non-nil is a number. It has `UnmarshalJSON`, `MarshalJSON` and `IsZero() bool` (true when `Set` is false), and the constructors `model.Int(n int) OptInt` and `model.ClearInt() OptInt`.
- `model.RepoPatch.Max` and `.Warm` are `model.OptInt` with the tags `json:"max,omitzero"` and `json:"warm,omitzero"` (Task 21). On the wire an absent key leaves the value, `null` clears it, a number sets it. TypeScript `RepoPatch.max?: number | null` and `warm?: number | null` (Task 22).
- `Backend.CheckTokenOwner func(ctx context.Context, token string) error` in `internal/daemon` (Task 24).
- `func (s *Store) Reload() (warnings []string, tokenChanged bool, err error)` in `internal/daemon/store.go` (Task 25).
- `func (b *Backend) tokenGen() uint64` in `internal/daemon` (Task 26): `b.GH.TokenMeta().Generation`.
- Test helpers in `internal/daemon/tokengen_test.go` (Task 26), extended by Task 27: `type tokenCache struct { name, fetch string; setup func(m *fakeManager, gh *fakeGH); read func(b *Backend) error; calls func(gh *fakeGH) int; other func(b *Backend) }`, one row per cache that follows the token: the GitHub call that fills it, how a test prepares it, the request that reads through it, how many of those calls were made, and a request for another entry of the same cache (nil when there is none); `func tokenCaches() []tokenCache` returns the rows, and Task 27 appends its rows to that list; `type midFetchGH struct { GitHub; on string; swap func(); swapped atomic.Bool }` wraps the fake GitHub and runs `swap` once, inside the first call named `on` (`ListRecentRuns`, `GetRun`, `ListJobs`, `JobLog` or `GetContent`), before that call is answered.
- `runDetailPoll(data: RunDetail | undefined, attempt: number | undefined): number | false` in `web/src/api/hooks.ts` (Task 28).

## Assumptions (evidence)

- Every task's code was carried out red-then-green in a drafter's worktree on `406202f`, in six chains (Tasks 1-6, 7-11, 12-15, 16-19, 20-25, 26-30). All 30 task texts were then replayed in plan order in one worktree on `406202f`: every quoted block to replace was found where its task runs, every intermediate state passed `gofmt`, `go vet` and `go test ./...`, and the final state passed `go test -race -count=1 ./...` (22 packages), the web unit suite (1170 tests), lint and typecheck. Evidence: the integration report of 2026-10-11. Not rerun there: commit steps, `deploy/tests/setup_test.sh`, the race detector at each intermediate state, and the expected-failure steps outside `internal/daemon`.
- `rg` is a shell function in some agent shells and is then missing inside a script run with `bash file.sh`; a task's `rg` check may be run with `grep -rn` instead.
- A skipped step carries equal start and end times and prints nothing; steps after a cancelled step are reported `skipped`, only the interrupted one `cancelled`. Evidence: `gh api` on job 114375054549 of `darkraise/darkraise-ai-plugins` (step 9) and job 114404096068 of `darkraise/darkrouter` (steps 6 to 10), 2026-10-11.
- ghr sets a job-started and a job-completed hook on every runner, so its jobs carry the steps `Set up runner` and `Complete runner`, each with a `##[group]Run '/opt/ghr/hooks/…'` header. Evidence: `internal/runner/lifecycle.go:132-133`; the logs of jobs 111871024678 and 111870951263 of `darkraise/darkmem`, fetched 2026-10-11.
- For a job that never ran, `api.github.com` answers the log request 302 and the blob store answers `404 The specified blob does not exist`; an expired log is a 410 from `api.github.com` itself. Evidence: `gh api -i` on job 114404210515 of `darkraise/darkrouter` and job 111363230492 of `darkraise/ghr-e2e`, and on job 64934606621 of `darkraise/flashpoint-web`, 2026-10-11. Unverified: whether the store can answer 404 for a moment right after a job completes; register row 213 records it.
- `record` is the one place the client's token verdict is set, called from `do` and from `request`. Evidence: `internal/github/client.go:195-223`, `internal/github/download.go:50`.
- Every GitHub client is built with `New`, so a cache and a slot channel created there are never nil. Evidence: `rg 'Client\{|github\.New\('` finds only `internal/daemon/run.go:166`, `:228`, `internal/daemon/setup.go:158` and `internal/daemon/run_test.go:147`.
- No test in `internal/github` runs in parallel, so tests may shrink the package-level limits. Evidence: `rg 't\.Parallel' internal/github` finds nothing.
- Nothing outside `internal/github/client_test.go` sets `Client.Sleep`. Evidence: `rg Sleep internal cmd --glob '*.go'`.
- `json:",omitzero"` and `IsZero` are honoured by the toolchain in use. Evidence: `go.mod` says `go 1.26.0`; `internal/config/config.go:54` already uses `omitzero`; `TestMutatingCommands` in `cmd/ghr/cli_test.go` pins the CLI's PATCH body.
- `Store.Update` calls its function with a fresh clone under the store's lock, and `Validate` only reads, so the warnings of the config before an update can be computed inside that callback. Evidence: `internal/daemon/store.go:102-118`, `internal/config/config.go:293-403`.
- The token is stored before the client's generation rises, so reading the generation before a fetch is the safe order. Evidence: `internal/github/client.go:269-272`, `internal/daemon/backend.go:654-657`.
- `dropLabelChecks` with an unchanged generation drops only the checks of removed repositories. Evidence: `internal/daemon/labelcheck.go:56-71`, `:84-89`.
- A job is recorded in the next place before it leaves the previous one (instance, pending record, history). Evidence: `internal/runner/lifecycle.go:371-372`, `:590-594`.
- Nothing relies on a history prune always rewriting the file. Evidence: `internal/runner/cleanup.go:304` reads only the error; `internal/daemon/changes.go:91` counts `Writes`; the suites of `internal/runner` and `internal/daemon` pass with the new rule.
- `Parse` refuses unknown keys, so `allow_public` is an error until the field exists. Evidence: `internal/config/config.go:163` (`KnownFields(true)`).
- `GetRepo` follows GitHub's redirect for a renamed repository, because the API client sets no `CheckRedirect`. Evidence: `internal/github/client.go:158`. Not checked against GitHub.
- Spawning and the idle stop each have one call site. Evidence: `rg 'm\.spawn\(' internal/runner` finds only `tick.go`; `rg 'sched\.IdleToStop' internal` finds only `stopIdle`.
- Only `deploy/setup.sh` creates the `setup-pending` and `toolchains-pending` markers today. Evidence: `deploy/setup.sh:174`.
- The web client treats a missing attempt and attempt 0 alike. Evidence: `web/src/api/client.ts:126`.
- `web/fixtures_test.go` fails when `web/src/api/fixtures/job-log.json` is out of date with the Go model, which is why Task 9 regenerates it. Evidence: `go test ./web/` in the drafter's worktree, 2026-10-11.
- The daemon's test fake reads the token generation without a lock. Two table tests in `internal/daemon/tokengen_test.go` change the token while a fetch is in flight, through `midFetchGH`: the change runs on the fetching goroutine itself, inside the GitHub call, so no second goroutine touches the fake's generation at that moment, and every other row waits for its download to end before the change. Evidence: `go test -race -count=1 ./...` on the replayed tree, and four further `-race` runs of `internal/daemon`, 2026-10-11. A later test that changed the token from another goroutine during a download would trip the race detector.
- The spec's "none sleeps" is read as "no new test calls `time.Sleep`": Tasks 24, 25 and 30 use `waitFor`, `scanCounts` and `waitLabelCheck`, polling helpers the daemon's tests already have, for results that arrive on detached goroutines. Decision of the plan review, 2026-10-11.
- Scores count file shapes, not file instances, as `reference/ladder.md` says; a task that edits a source file, its test and a fixture of one kind scores 1 on files.

## Task index

1. The lru package
2. Only a 401 rejects the token
3. Bounds on the GitHub client
4. Cancellable writes
5. An undecodable file is its own error
6. A workflow file that cannot be decoded
7. Splitting a log into steps
8. A job with no log: the client
9. A job with no log: the daemon
10. A job with no log: the job page
11. Graph matching
12. History file: oversized lines, prune, mode, sync
13. Metrics after a clock step
14. Runner cap: config
15. Runner cap: the settings form
16. Adding a repository
17. Reading repository visibility
18. Blocked and unread repositories: planning
19. Blocked and unread repositories: demand, status and events
20. Patching config: the optional number type
21. Patching config: null, sorted names, new warnings
22. Clearing max and warm on the repository page
23. Setup is pending until finished
24. Checking a new token
25. Reload with the same token
26. Caches follow the token generation (Actions list, run details, runner steps)
27. Caches follow the token generation (logs, annotations, workflow marks)
28. A re-run of a finished run
29. Runner steps and activity order
30. Panic guards on detached goroutines

---

### Task 1: The lru package

**Files:**
- Create: `internal/lru/lru.go`, `internal/lru/lru_test.go`
- Modify: `internal/daemon/rungraph.go` (imports, `wfCache`, `workflowFile`, the `lru` type at the end of the file), `internal/daemon/joblogs.go` (imports, `logCache`, `initLocked`, `downloadLog`, `JobLog`), `internal/daemon/annotations.go` (`RunAnnotations`)
- Test: `internal/lru/lru_test.go`, `internal/daemon/rungraph_test.go`, `internal/daemon/joblogs_test.go`, `internal/daemon/annotations_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: package `github.com/darkraise/ghr/internal/lru` with `Cache`, `New`, `Get`, `Add`, `Remove` (Contracts). After this task the daemon's caches are `*lru.Cache[...]`.

**Items:** 45
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

The daemon keeps a small generic cache type, `lru`, at the end of `internal/daemon/rungraph.go`. The GitHub client needs the same type, so it moves to its own package with exported names. Its `len()` method, which only tests call, is not carried over. Nothing about how the cache behaves changes.

- [ ] **Step 1: Write the failing test**

Create `internal/lru/lru_test.go`. The first test is the daemon's `TestLRUBoundsEntriesAndSize`, moved; where it called `len()` it reads the unexported `items` map, the way it already reads `size`.

```go
package lru

import "testing"

func TestCacheBoundsEntriesAndSize(t *testing.T) {
	c := New[string, int](2, 10)
	c.Add("a", 1, 4)
	c.Add("b", 2, 4)
	c.Get("a")
	c.Add("c", 3, 4)
	if _, ok := c.Get("b"); ok || len(c.items) != 2 {
		t.Fatalf("b should be evicted as least recently used, %d entries", len(c.items))
	}
	c.Add("d", 4, 8)
	if _, ok := c.Get("a"); ok || len(c.items) != 1 || c.size != 8 {
		t.Fatalf("size bound: %d entries, size %d", len(c.items), c.size)
	}
	c.Add("e", 5, 11)
	if _, ok := c.Get("e"); ok || len(c.items) != 1 {
		t.Fatalf("an entry above maxSize must not be kept, %d entries", len(c.items))
	}
	if v, ok := c.Get("d"); !ok || v != 4 {
		t.Fatalf("d = %d, %v", v, ok)
	}
	c.Remove("d")
	c.Remove("d")
	if _, ok := c.Get("d"); ok || len(c.items) != 0 || c.order.Len() != 0 || c.size != 0 {
		t.Fatalf("after Remove: %d entries, %d in order, size %d", len(c.items), c.order.Len(), c.size)
	}
}

func TestCacheWithoutSizeBound(t *testing.T) {
	c := New[int, string](2, 0)
	c.Add(1, "one", 1<<40)
	c.Add(2, "two", 1<<40)
	c.Add(1, "uno", 1<<40)
	c.Add(3, "three", 1<<40)
	if _, ok := c.Get(2); ok {
		t.Fatal("2 should be evicted: adding 1 again made it the most recently used")
	}
	if v, ok := c.Get(1); !ok || v != "uno" || len(c.items) != 2 {
		t.Fatalf("1 = %q, %v, %d entries", v, ok, len(c.items))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/lru/ -count=1`
Expected: FAIL, the package does not build: `internal/lru/lru_test.go:6:7: undefined: New`

- [ ] **Step 3: Write the package**

Create `internal/lru/lru.go`:

```go
// Package lru is a map that forgets its least recently used entries.
package lru

import "container/list"

// Cache is a map bounded by entry count and, when maxSize is above 0, by the
// total of the sizes given to Add; the least recently used entries go
// first. Callers hold their own lock.
type Cache[K comparable, V any] struct {
	maxEntries int
	maxSize    int64
	size       int64
	order      *list.List // front is the most recently used
	items      map[K]*list.Element
}

type item[K comparable, V any] struct {
	key  K
	val  V
	size int64
}

func New[K comparable, V any](maxEntries int, maxSize int64) *Cache[K, V] {
	return &Cache[K, V]{maxEntries: maxEntries, maxSize: maxSize, order: list.New(), items: map[K]*list.Element{}}
}

func (c *Cache[K, V]) Get(k K) (V, bool) {
	e, ok := c.items[k]
	if !ok {
		var zero V
		return zero, false
	}
	c.order.MoveToFront(e)
	return e.Value.(*item[K, V]).val, true
}

// Add stores v under k. An entry larger than maxSize on its own is not kept.
func (c *Cache[K, V]) Add(k K, v V, size int64) {
	c.Remove(k)
	if c.maxSize > 0 && size > c.maxSize {
		return
	}
	c.items[k] = c.order.PushFront(&item[K, V]{key: k, val: v, size: size})
	c.size += size
	for c.order.Len() > c.maxEntries || (c.maxSize > 0 && c.size > c.maxSize) {
		c.Remove(c.order.Back().Value.(*item[K, V]).key)
	}
}

func (c *Cache[K, V]) Remove(k K) {
	e, ok := c.items[k]
	if !ok {
		return
	}
	c.order.Remove(e)
	delete(c.items, k)
	c.size -= e.Value.(*item[K, V]).size
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/lru/ -count=1`
Expected: PASS

- [ ] **Step 5: Switch the daemon to the package**

In `internal/daemon/rungraph.go`:

In the import block, delete the line `"container/list"`, and replace

```go
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
```

with

```go
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/lru"
	"github.com/darkraise/ghr/internal/model"
```

In `wfCache`, replace

```go
	files *lru[string, wfFile]
```

with

```go
	files *lru.Cache[string, wfFile]
```

In `workflowFile`, replace

```go
		c.files = newLRU[string, wfFile](wfFileEntries, 0)
	}
	f, ok := c.files.get(key)
```

with

```go
		c.files = lru.New[string, wfFile](wfFileEntries, 0)
	}
	f, ok := c.files.Get(key)
```

and replace

```go
	c.files.add(key, f, 0)
```

with

```go
	c.files.Add(key, f, 0)
```

Delete everything after the closing brace of `runGraph`: the comment that starts `// lru is a map bounded by entry count`, the types `lru` and `lruItem`, the function `newLRU`, and the methods `get`, `add`, `remove` and `len`. `runGraph` is then the last declaration in the file.

In `internal/daemon/joblogs.go`:

In the import block, replace

```go
	"github.com/darkraise/ghr/internal/joblog"
	"github.com/darkraise/ghr/internal/model"
```

with

```go
	"github.com/darkraise/ghr/internal/joblog"
	"github.com/darkraise/ghr/internal/lru"
	"github.com/darkraise/ghr/internal/model"
```

In `logCache`, replace

```go
	parsed *lru[int64, model.JobLog]
	notes  *lru[int64, jobNotes]
```

with

```go
	parsed *lru.Cache[int64, model.JobLog]
	notes  *lru.Cache[int64, jobNotes]
```

In `initLocked`, replace

```go
	c.parsed = newLRU[int64, model.JobLog](1<<30, parsedLogBytes)
	c.notes = newLRU[int64, jobNotes](annotationEntries, 0)
```

with

```go
	c.parsed = lru.New[int64, model.JobLog](1<<30, parsedLogBytes)
	c.notes = lru.New[int64, jobNotes](annotationEntries, 0)
```

In `downloadLog`, replace

```go
		c.parsed.add(j.id, res.log, size)
		c.notes.add(j.id, res.notes, 0)
```

with

```go
		c.parsed.Add(j.id, res.log, size)
		c.notes.Add(j.id, res.notes, 0)
```

In `JobLog`, replace

```go
	if l, ok := c.parsed.get(jobID); ok {
```

with

```go
	if l, ok := c.parsed.Get(jobID); ok {
```

In `internal/daemon/annotations.go`, in `RunAnnotations`, replace

```go
		if n, ok := c.notes.get(j.ID); ok {
```

with

```go
		if n, ok := c.notes.Get(j.ID); ok {
```

replace

```go
		if l, ok := c.parsed.get(j.ID); ok {
```

with

```go
		if l, ok := c.parsed.Get(j.ID); ok {
```

and replace

```go
			c.notes.add(j.ID, n, 0)
```

with

```go
			c.notes.Add(j.ID, n, 0)
```

- [ ] **Step 6: Update the daemon tests that used the old type**

In `internal/daemon/rungraph_test.go`, delete the whole of `TestLRUBoundsEntriesAndSize`. Step 1 moved it into the new package. Every import of the file is still used.

In `internal/daemon/annotations_test.go`, in `TestRunAnnotationsReusesTheParsedLog`, replace

```go
	b.logs.notes.remove(2001)
```

with

```go
	b.logs.notes.Remove(2001)
```

In `internal/daemon/joblogs_test.go`, in `TestJobLogStopsWaitingButKeepsTheDownload`, replace

```go
		_, ok := b.logs.parsed.get(1002)
```

with

```go
		_, ok := b.logs.parsed.Get(1002)
```

`TestJobLogParsesAndKeepsTheLog` in the same file ends by reading the caches' unexported `maxEntries` and `maxSize`, which another package cannot read any more. The package gets no accessor for them: the test checks the same two bounds through what the caches keep. Replace

```go
	if b.logs.notes.maxEntries != annotationEntries || b.logs.parsed.maxSize != parsedLogBytes {
		t.Fatalf("bounds %d entries, %d bytes", b.logs.notes.maxEntries, b.logs.parsed.maxSize)
	}
}
```

with

```go
	// The caches' bounds, read from what they keep: the notes cache holds
	// annotationEntries entries, and the parsed cache refuses one log larger
	// than parsedLogBytes.
	b.logs.mu.Lock()
	defer b.logs.mu.Unlock()
	for i := range annotationEntries {
		b.logs.notes.Add(int64(5000+i), jobNotes{}, 0)
	}
	_, oldest := b.logs.notes.Get(1002)
	_, next := b.logs.notes.Get(5000)
	if oldest || !next {
		t.Fatalf("notes cache after %d more entries: kept the first %v, kept the second %v", annotationEntries, oldest, next)
	}
	b.logs.parsed.Add(7001, model.JobLog{JobID: 7001}, parsedLogBytes)
	b.logs.parsed.Add(7002, model.JobLog{JobID: 7002}, parsedLogBytes+1)
	_, fits := b.logs.parsed.Get(7001)
	_, tooBig := b.logs.parsed.Get(7002)
	if !fits || tooBig {
		t.Fatalf("parsed cache: kept a log of parsedLogBytes %v, kept a larger one %v", fits, tooBig)
	}
}
```

The download earlier in that test left job 1002 in the notes cache, so `annotationEntries` further entries push exactly that one out when the bound is `annotationEntries`. A parsed log of exactly `parsedLogBytes` is kept and one byte more is refused only when the size bound is `parsedLogBytes`.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go build ./... && PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/lru/ ./internal/daemon/ -count=1`
Expected: PASS for both packages

Run: `rg -n 'newLRU|lruItem|\*lru\[' internal/ cmd/`
Expected: no output

- [ ] **Step 8: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (must print nothing), then:

```bash
git add internal/lru/lru.go internal/lru/lru_test.go internal/daemon/rungraph.go internal/daemon/rungraph_test.go internal/daemon/joblogs.go internal/daemon/joblogs_test.go internal/daemon/annotations.go internal/daemon/annotations_test.go
git commit -m "refactor(lru): move the cache type to its own package"
```

### Task 2: Only a 401 rejects the token

**Files:**
- Modify: `internal/github/client.go` (`record`, `contentsPathRe`)
- Test: `internal/github/client_test.go` (`TestTokenMetaOnlyCountsTokenVerdicts`), `internal/github/runs_test.go` (`TestRepositoryNamedContentsStillGivesAVerdict`)

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names

**Items:** 27, 44
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

`record` is the one place the client sets its verdict on the token (`TokenMeta().OK` and `.CheckedAt`), for API calls and for downloads. Today it marks the token rejected on any `ErrAuth`, which includes every 403 that is not a rate limit, unless the path is a Contents path. After this task only a 401 does; a 403 leaves the verdict as it was, like a 404 or a server error. The Contents exemption then has nothing to exempt and goes, with its pattern.

- [ ] **Step 1: Write the failing test**

`TestTokenMetaOnlyCountsTokenVerdicts` in `internal/github/client_test.go` already walks the statuses that are not a verdict. Add 403 to them. Its handler answers a 403 with `X-RateLimit-Remaining: 4403`, no `Retry-After` and the message `x`, which the client classifies as `ErrAuth`, not as a rate limit.

Replace the comment above the test

```go
// Only a success or an authentication or permission failure is a verdict on
// the token; a server error, a 404 or a rate limit leaves the last verdict in
// place.
```

with

```go
// Only a success or a 401 is a verdict on the token; a server error, a 404, a
// rate limit or a 403 leaves the last verdict in place.
```

and in the test replace

```go
	for _, status = range []int{500, 404, 429} {
```

with

```go
	for _, status = range []int{500, 404, 429, 403} {
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/github/ -run TestTokenMetaOnlyCountsTokenVerdicts -count=1`
Expected: FAIL with `after 403: {Generation:0 CheckedAt:2026-10-05 12:04:00 +0000 UTC OK:false ...`

- [ ] **Step 3: Write the implementation**

In `internal/github/client.go` replace the whole `record` method, including its comment, with:

```go
// record stores the response's rate and expiry headers, unless the token
// changed while the request was in flight. Only a success or a 401 is a
// verdict on the token. A 403 refuses one repository or one call, so it
// leaves CheckedAt and OK as they were, like every other failure.
func (c *Client) record(gen uint64, h http.Header, apiErr *APIError) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return
	}
	if n, err := strconv.Atoi(h.Get("X-RateLimit-Remaining")); err == nil {
		c.remaining = n
		c.meta.RateRemaining = &n
	}
	if n, err := strconv.Atoi(h.Get("X-RateLimit-Limit")); err == nil {
		c.meta.RateLimit = &n
	}
	if n, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		t := time.Unix(n, 0)
		c.meta.RateReset = &t
	}
	if t, ok := parseExpiry(h.Get("GitHub-Authentication-Token-Expiration")); ok {
		c.meta.ExpiresAt = &t
	}
	switch {
	case apiErr == nil:
		c.meta.CheckedAt, c.meta.OK = c.Now(), true
	case apiErr.Status == http.StatusUnauthorized:
		c.meta.CheckedAt, c.meta.OK = c.Now(), false
	}
}
```

Directly below it, delete `contentsPathRe` and its comment:

```go
// contentsPathRe matches the contents endpoint of any repository, not a
// repository that happens to be named contents.
var contentsPathRe = regexp.MustCompile(`^/repos/[^/]+/[^/]+/contents/`)
```

Keep the `regexp` import: `nextLinkRe` in the same file still uses it.

- [ ] **Step 4: Remove the test of the deleted exemption**

In `internal/github/runs_test.go`, delete the whole of `TestRepositoryNamedContentsStillGivesAVerdict`. It asserted that a 403 on a repository named `contents` still rejects the token, which pinned the pattern deleted in Step 3; under the new rule no 403 rejects the token, and Step 1's row covers that.

Leave `TestMissingContentsPermissionIsNotATokenVerdict` in the same file as it is. It still passes: it is the new rule seen through `GetContent`, on a client that has made no other call.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/github/ ./internal/daemon/ ./internal/runner/ -count=1`
Expected: PASS for all three packages

- [ ] **Step 6: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (must print nothing), then:

```bash
git add internal/github/client.go internal/github/client_test.go internal/github/runs_test.go
git commit -m "fix(github): let only a 401 reject the token"
```

### Task 3: Bounds on the GitHub client

**Files:**
- Modify: `internal/github/client.go` (imports, the limits, `Client`, `New`, `ForgetCache`, `do`, `getAll`, `ListRuns`, `ListJobs`, `ListRunners`), `internal/github/runs.go` (`ListAttemptJobs`, `ListArtifacts`), `internal/github/releases.go` (`ListUserRepos`)
- Test: `internal/github/client_test.go`, `internal/github/download_test.go`

**Interfaces:**
- Consumes: package `github.com/darkraise/ghr/internal/lru` (Contracts): `lru.Cache`, `lru.New`, `Get`, `Add`
- Produces: nothing a later task names

**Items:** 28
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 2 = 5

Three things in the client have no upper bound today. The ETag cache holds up to 1,000 response bodies of any size and is emptied whole when it reaches 1,000. A response body is read without a limit. A listing follows `next` links without a limit. After this task:

- the ETag cache is an `lru.Cache` bounded at 1,000 entries and 32 MiB of body bytes, so it drops the least recently used entry instead of everything;
- an API response body larger than 8 MiB fails the call with `github: response larger than 8 MiB`;
- a listing stops after 50 pages with `github: listing has more than 50 pages`, and the caller gets the error and no items. The six listing methods return what they collected together with the error today; they return `nil` with it after this task.

Log and artifact downloads (`download.go`) have their own limits and are not touched.

The cache and body limits are package variables so that the tests can reach them with a few bytes. The page limit stays a constant: 50 requests against a local test server are cheap.

- [ ] **Step 1: Write the failing tests**

Add to `internal/github/client_test.go` (the end of the file is fine; every import they need is already there):

```go
// setLimit changes one of the client's bounds for the length of a test.
func setLimit[T any](t *testing.T, p *T, v T) {
	t.Helper()
	old := *p
	*p = v
	t.Cleanup(func() { *p = old })
}

func TestETagCacheEvictsTheLeastRecentlyUsed(t *testing.T) {
	if maxCacheEntries != 1000 || maxCacheBytes != 32<<20 {
		t.Fatalf("cache bounds: %d entries, %d bytes", maxCacheEntries, maxCacheBytes)
	}
	body := func(repo string) string { return `{"full_name":"darkraise/` + repo + `"}` }
	for _, tc := range []struct {
		name    string
		entries int
		bytes   int64
	}{
		{"by count", 2, 1 << 20},
		{"by bytes", 100, int64(2 * len(body("a")))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setLimit(t, &maxCacheEntries, tc.entries)
			setLimit(t, &maxCacheBytes, tc.bytes)
			var mu sync.Mutex
			var seen []string
			c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				repo := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
				mu.Lock()
				defer mu.Unlock()
				if r.Header.Get("If-None-Match") != "" {
					seen = append(seen, repo+" revalidated")
					w.WriteHeader(http.StatusNotModified)
					return
				}
				seen = append(seen, repo)
				w.Header().Set("ETag", `"`+repo+`"`)
				fmt.Fprint(w, body(repo))
			})
			// The second read of a makes b the least recently used, so c
			// evicts b and a is still revalidated afterwards.
			for _, repo := range []string{"a", "b", "a", "c", "a", "b"} {
				if _, err := c.GetRepo(context.Background(), repo); err != nil {
					t.Fatal(err)
				}
			}
			c.ForgetCache()
			if _, err := c.GetRepo(context.Background(), "a"); err != nil {
				t.Fatal(err)
			}
			mu.Lock()
			defer mu.Unlock()
			want := "a, b, a revalidated, c, a revalidated, b, a"
			if got := strings.Join(seen, ", "); got != want {
				t.Fatalf("requests %s\nwant     %s", got, want)
			}
		})
	}
}

func TestResponseBodyIsBounded(t *testing.T) {
	if maxBodyBytes != 8<<20 {
		t.Fatalf("body bound: %d bytes", maxBodyBytes)
	}
	setLimit(t, &maxBodyBytes, 64)
	const frame = len(`{"full_name":""}`)
	for _, tc := range []struct {
		name    string
		size    int
		wantErr string
	}{
		{"at the limit", 64, ""},
		{"over the limit", 65, "github: response larger than 8 MiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"full_name":"`+strings.Repeat("x", tc.size-frame)+`"}`)
			})
			repo, err := c.GetRepo(context.Background(), "darkcloud")
			if tc.wantErr == "" {
				if err != nil || len(repo.FullName) != tc.size-frame {
					t.Fatalf("repo %+v err %v", repo, err)
				}
				return
			}
			if repo != nil || err == nil || err.Error() != tc.wantErr {
				t.Fatalf("repo %+v err %v", repo, err)
			}
		})
	}
}

// A listing longer than 50 pages is refused whole: the caller gets the error
// and no items.
func TestListingStopsAfter50Pages(t *testing.T) {
	ctx := context.Background()
	const tooLong = "github: listing has more than 50 pages"
	for _, tc := range []struct {
		name    string
		pages   int
		list    func(c *Client) (int, error)
		want    int
		wantErr string
	}{
		{"runs, exactly 50 pages", 50, func(c *Client) (int, error) { v, err := c.ListRuns(ctx, "r", "queued"); return len(v), err }, 50, ""},
		{"runs", 60, func(c *Client) (int, error) { v, err := c.ListRuns(ctx, "r", "queued"); return len(v), err }, 0, tooLong},
		{"jobs", 60, func(c *Client) (int, error) { v, err := c.ListJobs(ctx, "r", 5); return len(v), err }, 0, tooLong},
		{"runners", 60, func(c *Client) (int, error) { v, err := c.ListRunners(ctx, "r"); return len(v), err }, 0, tooLong},
		{"attempt jobs", 60, func(c *Client) (int, error) { v, err := c.ListAttemptJobs(ctx, "r", 5, 1); return len(v), err }, 0, tooLong},
		{"artifacts", 60, func(c *Client) (int, error) { v, err := c.ListArtifacts(ctx, "r", 5); return len(v), err }, 0, tooLong},
		{"user repositories", 60, func(c *Client) (int, error) { v, err := c.ListUserRepos(ctx); return len(v), err }, 0, tooLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var base string
			c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				page = max(page, 1)
				if page < tc.pages {
					w.Header().Set("Link", fmt.Sprintf(`<%s%s?page=%d>; rel="next"`, base, r.URL.Path, page+1))
				}
				if r.URL.Path == "/user/repos" {
					fmt.Fprint(w, `[{"name":"r"}]`)
					return
				}
				fmt.Fprint(w, `{"workflow_runs":[{"id":1}],"jobs":[{"id":1}],"runners":[{"id":1}],"artifacts":[{"id":1}]}`)
			})
			base = c.BaseURL
			n, err := tc.list(c)
			if tc.wantErr == "" {
				if err != nil || n != tc.want {
					t.Fatalf("%d items, err %v", n, err)
				}
				return
			}
			if err == nil || err.Error() != tc.wantErr || n != 0 {
				t.Fatalf("%d items, err %v", n, err)
			}
			if len(f.requests) != 50 {
				t.Fatalf("%d requests, want 50", len(f.requests))
			}
		})
	}
}
```

The over-long listings end at page 60 rather than never, so the test also ends when the page limit is missing.

- [ ] **Step 2: Declare the bounds**

The tests name `maxCacheBytes` and `maxBodyBytes`, which do not exist yet, and take the address of `maxCacheEntries`, which is a constant. Declare the bounds first, without using the new ones, so that the tests build and fail for what they test.

In `internal/github/client.go` replace

```go
const maxCacheEntries = 1000
```

with

```go
// Bounds on what the client keeps and reads. They are variables so that
// tests can reach them with small fixtures.
var (
	maxCacheEntries       = 1000
	maxCacheBytes   int64 = 32 << 20
	maxBodyBytes    int64 = 8 << 20
)

// maxPages is how many pages of 100 a listing may have.
const maxPages = 50
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/github/ -run 'TestETagCacheEvictsTheLeastRecentlyUsed|TestResponseBodyIsBounded|TestListingStopsAfter50Pages' -count=1`
Expected: FAIL, with these failures:

- `TestETagCacheEvictsTheLeastRecentlyUsed/by_count`: `requests a, b, a revalidated, c, a, b, a` (the cache was emptied whole at its limit, so `a` was lost too)
- `TestETagCacheEvictsTheLeastRecentlyUsed/by_bytes`: `requests a, b, a revalidated, c, a revalidated, b revalidated, a` (nothing bounds the bytes)
- `TestResponseBodyIsBounded/over_the_limit`: `repo &{FullName:xxx... Private:false} err <nil>`
- `TestListingStopsAfter50Pages`: the six rows other than `runs, exactly 50 pages` each with `60 items, err <nil>`

- [ ] **Step 4: Write the implementation**

All in `internal/github/client.go` unless another file is named.

Add the `lru` import as a second group of the import block. Replace

```go
	"sync"
	"time"
)

type ErrKind int
```

with

```go
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/lru"
)

type ErrKind int
```

Below the `const maxPages = 50` line added in Step 2, add the two errors:

```go
var (
	errBodyTooLarge = errors.New("github: response larger than 8 MiB")
	errTooManyPages = errors.New("github: listing has more than 50 pages")
)
```

In the `Client` struct replace

```go
	mu         sync.Mutex
	cache      map[string]cached
```

with

```go
	mu sync.Mutex
	// cache holds the last body per GET URL for ETag revalidation, sized by
	// its body bytes.
	cache      *lru.Cache[string, cached]
```

In `New` replace

```go
		remaining: -1,
	}
}
```

with

```go
		cache:     lru.New[string, cached](maxCacheEntries, maxCacheBytes),
		remaining: -1,
	}
}
```

In `ForgetCache` replace

```go
	c.cache = nil
```

with

```go
	c.cache = lru.New[string, cached](maxCacheEntries, maxCacheBytes)
```

In `do` replace

```go
	prev, hasPrev := c.cache[u]
```

with

```go
	prev, hasPrev := c.cache.Get(u)
```

replace

```go
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
```

with

```go
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(data)) > maxBodyBytes {
		return nil, "", errBodyTooLarge
	}
```

and replace

```go
			if gen == c.gen {
				// Run and job URLs are unbounded over time; reset rather than track LRU.
				if c.cache == nil || len(c.cache) >= maxCacheEntries {
					c.cache = map[string]cached{}
				}
				c.cache[u] = cached{etag: etag, body: data, next: next}
			}
```

with

```go
			if gen == c.gen {
				c.cache.Add(u, cached{etag: etag, body: data, next: next}, int64(len(data)))
			}
```

Replace the whole `getAll` method with:

```go
// getAll reads a listing page by page. On an error the caller must drop what
// each has collected: a listing cut short would read as a complete one.
func (c *Client) getAll(ctx context.Context, u string, each func([]byte) error) error {
	for page := 0; u != ""; page++ {
		if page == maxPages {
			return errTooManyPages
		}
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
```

Each of the six methods that call `getAll` ends with the same line. In all six, which are `ListRuns`, `ListJobs` and `ListRunners` in `client.go`, `ListAttemptJobs` and `ListArtifacts` in `internal/github/runs.go`, and `ListUserRepos` in `internal/github/releases.go`, replace the last line of the method

```go
	return out, err
```

with

```go
	if err != nil {
		return nil, err
	}
	return out, nil
```

`rg -n 'return out, err' internal/github/` must then print nothing.

- [ ] **Step 5: Update the two tests that read the cache as a map**

`len(c.cache)` no longer compiles. Both tests assert that a redirect answer is not kept in the ETag cache; ask the cache for the request's URL instead.

In `internal/github/download_test.go`, in `TestJobLogFollowsTheRedirectWithoutTheToken`, replace

```go
	if len(c.cache) != 0 {
		t.Fatalf("redirect kept in the ETag cache: %v", c.cache)
	}
```

with

```go
	if _, ok := c.cache.Get(c.repoURL("darkmem", "/actions/jobs/1002/logs")); ok {
		t.Fatal("redirect kept in the ETag cache")
	}
```

and in `TestArtifactURLDoesNotFollow` replace the same three lines with

```go
	if _, ok := c.cache.Get(c.repoURL("darkmem", "/actions/artifacts/11/zip")); ok {
		t.Fatal("redirect kept in the ETag cache")
	}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test -race ./internal/github/ -count=1`
Expected: PASS

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go build ./... && PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ ./internal/runner/ -count=1`
Expected: PASS for both packages

- [ ] **Step 7: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (must print nothing), then:

```bash
git add internal/github/client.go internal/github/runs.go internal/github/releases.go internal/github/client_test.go internal/github/download_test.go
git commit -m "fix(github): bound the cache, bodies and listings"
```

### Task 4: Cancellable writes

**Files:**
- Modify: `internal/github/client.go` (`Client`, `New`, new `sleep`, `mutate`)
- Test: `internal/github/client_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `Client.Sleep func(ctx context.Context, d time.Duration) error` (Contracts)

**Items:** 36
**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

`mutate` runs the client's state-changing calls (`DeleteRunner`, `GenerateJITConfig`) one at a time and at least one second apart. Today it waits for its turn on a mutex and sleeps the spacing without looking at its context, so a call whose request was cancelled, or a daemon that is closing, still waits. After this task:

- the turn is a one-slot channel created in `New` and taken in a `select` with `ctx.Done()`;
- `Client.Sleep` takes the context and returns an error, so the spacing wait ends with the context too;
- in both cases `mutate` returns the context's own error and sends nothing.

Every client in the repository is built with `New`, so the channel is never nil.

- [ ] **Step 1: Write the failing tests**

In `internal/github/client_test.go`, six tests replace `c.Sleep` with a fake clock. Give each the new signature.

In `TestGenerateJITConfigBodyAndSpacing` and again in `TestSuspendedMutationFailsWithoutWaiting`, replace

```go
	c.Sleep = func(d time.Duration) { slept = append(slept, d); now = now.Add(d) }
```

with

```go
	c.Sleep = func(_ context.Context, d time.Duration) error { slept = append(slept, d); now = now.Add(d); return nil }
```

In `TestDeleteRunner404IsSuccess` and again in `TestMutationFailuresClassified` (there one tab deeper), replace

```go
	c.Sleep = func(time.Duration) {}
```

with

```go
	c.Sleep = func(context.Context, time.Duration) error { return nil }
```

In `TestRateLimitSuspendsEveryRequest` replace

```go
	c.Sleep = func(d time.Duration) { now = now.Add(d) }
```

with

```go
	c.Sleep = func(_ context.Context, d time.Duration) error { now = now.Add(d); return nil }
```

In `TestConcurrentMutationsAreSerialAndSpaced` replace

```go
	c.Sleep = func(d time.Duration) { mu.Lock(); now = now.Add(d); mu.Unlock() }
```

with

```go
	c.Sleep = func(_ context.Context, d time.Duration) error { mu.Lock(); now = now.Add(d); mu.Unlock(); return nil }
```

Those are all of them: before this step, `rg -n 'Sleep = ' internal/ cmd/` lists exactly these six lines, and nothing outside this file sets `Sleep`.

Then add the new test below `TestConcurrentMutationsAreSerialAndSpaced`:

```go
// A state-changing call whose context ends gives up its place in the queue
// and its spacing wait, and sends nothing.
func TestMutationStopsWaitingWhenItsContextEnds(t *testing.T) {
	t.Run("while queued", func(t *testing.T) {
		arrived, release := make(chan struct{}), make(chan struct{})
		c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) {
			close(arrived)
			<-release
			w.WriteHeader(204)
		})
		first := make(chan error, 1)
		go func() { first <- c.DeleteRunner(context.Background(), "r", 1) }()
		<-arrived
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		second := make(chan error, 1)
		go func() { second <- c.DeleteRunner(ctx, "r", 2) }()
		select {
		case err := <-second:
			if err != context.Canceled {
				t.Errorf("queued call: %#v", err)
			}
		case <-time.After(5 * time.Second): // elapses only when the call is still queued
			t.Error("the call is still queued after its context ended")
		}
		close(release)
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if len(f.requests) != 1 {
			t.Fatalf("requests %v", f.requests)
		}
	})
	t.Run("while spacing", func(t *testing.T) {
		c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
		now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
		c.Now = func() time.Time { return now }
		ctx, cancel := context.WithCancel(context.Background())
		var waits []time.Duration
		c.Sleep = func(ctx context.Context, d time.Duration) error {
			waits = append(waits, d)
			cancel()
			return ctx.Err()
		}
		if err := c.DeleteRunner(ctx, "r", 1); err != nil {
			t.Fatal(err)
		}
		// The context's own error, not the failure of a request sent under it.
		if err := c.DeleteRunner(ctx, "r", 2); err != context.Canceled {
			t.Fatalf("spacing call: %#v", err)
		}
		if len(waits) != 1 || waits[0] != time.Second || len(f.requests) != 1 {
			t.Fatalf("waits %v, requests %v", waits, f.requests)
		}
		// The cancelled call gave its turn back: the next one is sent.
		if err := c.DeleteRunner(context.Background(), "r", 3); err != nil {
			t.Fatal(err)
		}
		if len(f.requests) != 2 || f.requests[1] != "DELETE /repos/darkraise/r/actions/runners/3" {
			t.Fatalf("requests %v", f.requests)
		}
	})
	t.Run("the default wait", func(t *testing.T) {
		c := New("darkraise", func() string { return "tok" })
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := c.Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
			t.Fatalf("wait under an ended context: %v", err)
		}
		if err := c.Sleep(context.Background(), 0); err != nil {
			t.Fatalf("wait of no time: %v", err)
		}
	})
}
```

How the test stays free of sleeps: in "while queued" the first call holds the turn inside the test server's handler until `release` is closed, and the second call's context has already ended, so the only thing it can do is return. The 5-second `time.After` is a guard that elapses only when the test fails; a passing run never waits for it. Both subtests compare the error with `context.Canceled` itself on purpose: a call that went on and sent its request under the ended context would fail with a `*url.Error` that merely wraps it.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/github/ -run TestMutationStopsWaitingWhenItsContextEnds -count=1`
Expected: FAIL, the package does not build: `cannot use func(_ context.Context, d time.Duration) error {…} (value of type func(_ context.Context, d "time".Duration) error) as func("time".Duration) value in assignment`, and `too many arguments in call to c.Sleep`

- [ ] **Step 3: Write the implementation**

In `internal/github/client.go` replace the whole `Client` struct with:

```go
type Client struct {
	BaseURL string // https://api.github.com
	Owner   string
	Token   func() string
	HTTP    *http.Client
	// Blob fetches the signed download URLs GitHub redirects to. It sends no
	// token, and the caller's context bounds the whole download.
	Blob *http.Client
	Now  func() time.Time
	// Sleep waits d. When ctx ends first it returns at once with ctx's error.
	Sleep func(ctx context.Context, d time.Duration) error

	mu sync.Mutex
	// cache holds the last body per GET URL for ETag revalidation, sized by
	// its body bytes.
	cache     *lru.Cache[string, cached]
	remaining int
	retryAt   time.Time
	gen       uint64 // bumped when the token changes; older responses are not recorded
	meta      TokenMeta
	// mutSlot holds one value while a state-changing call has its turn. It
	// is a channel so that waiting for the turn can end with the context.
	mutSlot    chan struct{}
	lastMutate time.Time // guarded by mutSlot
}
```

Replace the whole `New` function with the following, and add `sleep` directly below it:

```go
func New(owner string, token func() string) *Client {
	return &Client{
		BaseURL: "https://api.github.com", Owner: owner, Token: token,
		HTTP: &http.Client{Timeout: 30 * time.Second}, Blob: newBlobClient(), Now: time.Now, Sleep: sleep,
		cache:     lru.New[string, cached](maxCacheEntries, maxCacheBytes),
		remaining: -1,
		mutSlot:   make(chan struct{}, 1),
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
```

Replace the whole `mutate` method, including its comment, with:

```go
// mutate serialises state-changing calls and spaces them at least 1s apart.
// A call whose context ends while it waits for its turn or for the spacing
// returns the context's error and sends nothing.
func (c *Client) mutate(ctx context.Context, method, u string, body any) ([]byte, error) {
	select {
	case c.mutSlot <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-c.mutSlot }()
	if until := c.SuspendedUntil(); !until.IsZero() {
		return nil, &APIError{Status: http.StatusTooManyRequests, Kind: ErrRateLimit, Message: "rate limited until " + until.Format(time.RFC3339), RetryAt: until}
	}
	if wait := c.lastMutate.Add(time.Second).Sub(c.Now()); wait > 0 {
		if err := c.Sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
	data, _, err := c.do(ctx, method, u, body)
	c.lastMutate = c.Now()
	return data, err
}
```

The `sync` import stays: `mu` is still a `sync.Mutex`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test -race ./internal/github/ -count=1`
Expected: PASS

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go build ./... && PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ ./internal/runner/ -count=1`
Expected: PASS for both packages

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (must print nothing), then:

```bash
git add internal/github/client.go internal/github/client_test.go
git commit -m "fix(github): end a queued write with its context"
```

### Task 5: An undecodable file is its own error

**Files:**
- Modify: `internal/github/runs.go` (imports, new `ErrBadContent`, `GetContent`)
- Test: `internal/github/runs_test.go` (`TestGetContent`)

**Interfaces:**
- Consumes: nothing
- Produces: `github.ErrBadContent` (Contracts)

**Items:** 34
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

`GetContent` reads one file through GitHub's Contents API. It fails in two ways that are a property of the file and not of the call: GitHub answers a file over 1 MB with `encoding: none` and no content, and the base64 it sends may not decode. Today both come back as plain errors that a caller cannot tell from a network failure. This task gives them one sentinel, `ErrBadContent`, wrapped with `%w`. The task after this one makes the run page answer it with a note instead of failing.

- [ ] **Step 1: Write the failing test**

In `internal/github/runs_test.go`, add `"errors"` to the import block, between `"context"` and `"fmt"`, and replace the whole of `TestGetContent` with:

```go
func TestGetContent(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, want, wantURI, wantErr string
	}{
		{name: "base64 with line breaks", path: ".github/workflows/ci.yml",
			body:    `{"content":"am9iczoK\nICBi\nOiB7fQo=\n","encoding":"base64"}`,
			want:    "jobs:\n  b: {}\n",
			wantURI: "GET /repos/darkraise/darkmem/contents/.github/workflows/ci.yml?ref=a1f9c3e"},
		{name: "segments escaped, slashes kept", path: ".github/workflows/a b#1.yml",
			body:    `{"content":"","encoding":"base64"}`,
			wantURI: "GET /repos/darkraise/darkmem/contents/.github/workflows/a%20b%231.yml?ref=a1f9c3e"},
		{name: "too large for the API", path: "big.yml",
			body:    `{"content":"","encoding":"none"}`,
			wantURI: "GET /repos/darkraise/darkmem/contents/big.yml?ref=a1f9c3e",
			wantErr: `github: file content cannot be decoded: big.yml has encoding "none", not base64`},
		{name: "base64 that does not decode", path: "bad.yml",
			body:    `{"content":"am9i!","encoding":"base64"}`,
			wantURI: "GET /repos/darkraise/darkmem/contents/bad.yml?ref=a1f9c3e",
			wantErr: "github: file content cannot be decoded: bad.yml: illegal base64 data at input byte 4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) })
			got, err := c.GetContent(context.Background(), "darkmem", tc.path, "a1f9c3e")
			if tc.wantErr != "" {
				if err == nil || err.Error() != tc.wantErr || !errors.Is(err, ErrBadContent) || got != nil {
					t.Fatalf("got %q err %v", got, err)
				}
			} else if err != nil || string(got) != tc.want {
				t.Fatalf("got %q err %v", got, err)
			}
			if f.requests[0] != tc.wantURI {
				t.Fatalf("request %s", f.requests[0])
			}
		})
	}
}
```

Three things differ from the test as it is. The row `too large for the API` expected the text `github: big.yml has encoding "none", not base64`; the error now starts with the sentinel's text, so its expected text changes. The row `base64 that does not decode` is new. Both error rows must also satisfy `errors.Is(err, ErrBadContent)` and return no bytes.

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/github/ -run TestGetContent -count=1`
Expected: FAIL, the package does not build: `undefined: ErrBadContent`

- [ ] **Step 3: Write the implementation**

In `internal/github/runs.go`, add `"errors"` to the import block, between `"encoding/json"` and `"fmt"`.

Replace the whole `GetContent` method, including its comment, with the following (the sentinel goes directly above it):

```go
// ErrBadContent is a file GitHub answered for but did not deliver in a form
// that can be decoded. Asking again gives the same answer.
var ErrBadContent = errors.New("github: file content cannot be decoded")

// GetContent reads one file at ref. GitHub answers base64 with line breaks;
// a file over 1 MB comes back with encoding "none". That, and base64 that
// does not decode, is an ErrBadContent.
func (c *Client) GetContent(ctx context.Context, repo, path, ref string) ([]byte, error) {
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	u := c.repoURL(repo, "/contents/"+strings.Join(segs, "/")+"?ref="+url.QueryEscape(ref))
	var body struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := c.getJSON(ctx, u, &body); err != nil {
		return nil, err
	}
	if body.Encoding != "base64" {
		return nil, fmt.Errorf("%w: %s has encoding %q, not base64", ErrBadContent, path, body.Encoding)
	}
	data, err := base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(body.Content))
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrBadContent, path, err)
	}
	return data, nil
}
```

An error from `getJSON` (a 404, a 403, a rate limit, a network failure) is returned as it is and is not an `ErrBadContent`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/github/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (must print nothing), then:

```bash
git add internal/github/runs.go internal/github/runs_test.go
git commit -m "fix(github): name undecodable file content"
```

### Task 6: A workflow file that cannot be decoded

**Files:**
- Modify: `internal/daemon/rungraph.go` (`wfFile`, the comment on `wfCache`, `workflowFile`, `runGraph`)
- Test: `internal/daemon/rungraph_test.go` (`TestRunGraphNotes`, `TestWorkflowFileCache`)

**Interfaces:**
- Consumes: `github.ErrBadContent` (Contracts); `lru.Cache` with `Get` and `Add` (Contracts), which `workflowFile` already uses
- Produces: `wfFile.bad bool` (Contracts)

**Items:** 34
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

`runGraph` builds the job graph of a run from its workflow file. When it cannot, it returns a note that says why and the run page is shown without a graph; any error it returns fails the whole run page with a 502. Today a workflow file that GitHub does not deliver in a decodable form (a file over 1 MB, or bad base64) is such an error. `GetContent` reports that case as `github.ErrBadContent`. After this task:

- `runGraph` answers it with the existing note `ghr could not read <path>.` (the constant `noteInvalid`). Network, rate-limit and server errors still fail the call.
- `workflowFile` remembers such a file as bad, without expiry, the way it remembers a file's bytes: a file at a commit never changes, so it is not fetched again on every page load.

- [ ] **Step 1: Write the failing tests**

In `internal/daemon/rungraph_test.go`, in the table of `TestRunGraphNotes`, add a row after the `invalid YAML` row. Replace

```go
		{"invalid YAML", ciPath, "jobs: [", nil, ciJobs(), "ghr could not read .github/workflows/ci.yml."},
	} {
```

with

```go
		{"invalid YAML", ciPath, "jobs: [", nil, ciJobs(), "ghr could not read .github/workflows/ci.yml."},
		{"undecodable file", ciPath, "", fmt.Errorf("%w: %s has encoding %q, not base64", github.ErrBadContent, ciPath, "none"), ciJobs(),
			"ghr could not read .github/workflows/ci.yml."},
	} {
```

In `TestWorkflowFileCache`, add this subtest directly above the `never keeps a 403` subtest:

```go
	t.Run("keeps an undecodable file for good", func(t *testing.T) {
		b, _, gh := newBackend(t)
		clock := frozenClock(b, actionsT0)
		gh.contentErr = fmt.Errorf("%w: %s has encoding %q, not base64", github.ErrBadContent, ciPath, "none")
		b.runGraph(ctx, "darkmem", "abc", ciPath, ciJobs())
		gh.contentErr = nil
		gh.content = map[string][]byte{contentKey("darkmem", ciPath, "abc"): []byte(ciYAML)}
		clock.Store(actionsT0.Add(24 * time.Hour).UnixNano())
		g, note, err := b.runGraph(ctx, "darkmem", "abc", ciPath, ciJobs())
		if err != nil || g != nil || note != fmt.Sprintf(noteInvalid, ciPath) || gh.count("GetContent") != 1 {
			t.Fatalf("graph %+v note %q err %v, %d calls", g, note, err, gh.count("GetContent"))
		}
	})
```

The fake would serve a good file on a second fetch, a day later. The subtest passes only when there is no second fetch.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run 'TestRunGraphNotes|TestWorkflowFileCache' -count=1`
Expected: FAIL, with these two failures:

- `TestRunGraphNotes/undecodable_file`: `graph <nil> note "" err github: file content cannot be decoded: .github/workflows/ci.yml has encoding "none", not base64`
- `TestWorkflowFileCache/keeps_an_undecodable_file_for_good`: a graph with two nodes, `note "" err <nil>, 2 calls`

- [ ] **Step 3: Write the implementation**

In `internal/daemon/rungraph.go` replace

```go
// wfFile is a workflow file's bytes, or the time GitHub answered 404 for it.
type wfFile struct {
	data      []byte
	missingAt time.Time
}
```

with

```go
// wfFile is a workflow file's bytes, the time GitHub answered 404 for it, or
// the mark that GitHub delivered it in a form that cannot be decoded.
type wfFile struct {
	data      []byte
	missingAt time.Time
	bad       bool
}
```

Replace the comment above `wfCache`

```go
// wfCache keeps workflow files by repository, commit and path. A file at a
// commit never changes, so bytes do not expire; only "not found" does, and
// a 403, 429 or 5xx is never kept, so granting the permission shows the
// graph on the next call.
```

with

```go
// wfCache keeps workflow files by repository, commit and path. A file at a
// commit never changes, so neither its bytes nor the mark that it cannot be
// decoded expire; only "not found" does, and a 403, 429 or 5xx is never
// kept, so granting the permission shows the graph on the next call.
```

Replace the whole `workflowFile` method with:

```go
func (b *Backend) workflowFile(ctx context.Context, repo, sha, path string) ([]byte, error) {
	c := &b.wfFiles
	key := strings.ToLower(repo) + "\x00" + sha + "\x00" + path
	c.mu.Lock()
	if c.files == nil {
		c.files = lru.New[string, wfFile](wfFileEntries, 0)
	}
	f, ok := c.files.Get(key)
	c.mu.Unlock()
	switch {
	case ok && f.bad:
		return nil, github.ErrBadContent
	case ok && f.missingAt.IsZero():
		return f.data, nil
	case ok && b.now().Sub(f.missingAt) < wfMissingTTL:
		return nil, &github.APIError{Status: 404, Kind: github.ErrNotFound, Message: "Not Found"}
	}
	data, err := b.GH.GetContent(ctx, repo, path, sha)
	switch {
	case err == nil:
		f = wfFile{data: data}
	case github.IsKind(err, github.ErrNotFound):
		f = wfFile{missingAt: b.now()}
	case errors.Is(err, github.ErrBadContent):
		f = wfFile{bad: true}
	default:
		return nil, err
	}
	c.mu.Lock()
	c.files.Add(key, f, 0)
	c.mu.Unlock()
	return data, err
}
```

The `bad` case is tested before the `missingAt.IsZero()` case on purpose: a bad entry has a zero `missingAt` too and would otherwise be returned as an empty file.

In `runGraph`, replace

```go
		return nil, noteNoPermission, nil
	case err != nil:
		return nil, "", err
	}
```

with

```go
		return nil, noteNoPermission, nil
	case errors.Is(err, github.ErrBadContent):
		return nil, fmt.Sprintf(noteInvalid, path), nil
	case err != nil:
		return nil, "", err
	}
```

The file already imports `errors` and `fmt`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (must print nothing), then:

```bash
git add internal/daemon/rungraph.go internal/daemon/rungraph_test.go
git commit -m "fix(daemon): show the note for an undecodable file"
```

### Task 7: Splitting a log into steps

**Files:**
- Modify: `internal/joblog/joblog.go` (`Parse`, new helpers `runnerPosts` and `regular`)
- Test: `internal/joblog/joblog_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names

**Items:** 20, 31
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

`joblog.Parse` files each log line under a step. It goes by the steps' start and end times, which GitHub rounds to whole seconds, plus one rule for `##[group]Run ` lines. Two faults follow. A skipped step has equal start and end times and prints nothing, yet it is a candidate like any other, so it takes the next step's header and first second of output. Post steps and `Complete job` usually start in the second the last regular step ended, nothing moves the parser on, and they show no lines.

This task keeps the one pointer (the current step) and replaces the rules. Two definitions:

- **The runner's post steps** are found by position: starting from the last step and skipping `Complete job`, `Complete runner` and `Stop containers`, they are the unbroken run of steps whose name starts with `Post ` (the word and a space). A step a person named `Post coverage comment` in the middle of a job is a regular step.
- **A regular step** is any step that is not `Set up job`, `Initialize containers`, `Stop containers`, `Complete job` or one of the runner's post steps. Those print no `##[group]Run ` header of their own.

`Set up runner` and `Complete runner` are the steps of the runner's job-started and job-completed hooks. ghr sets both hooks on every runner it manages, so every job a ghr runner runs has them: `Set up runner` directly after `Set up job`, and `Complete runner` between the post steps (and `Stop containers`, when the job has containers) and `Complete job`. Each prints its own header, `##[group]Run '/opt/ghr/hooks/job-started.sh'` and `##[group]Run '/opt/ghr/hooks/job-completed.sh'`, so both are regular steps and rule 2 opens them. `Complete runner` is skipped only when looking for the post steps; without that, the walk back from the end would stop at it and find none.

The rules, in order:

1. A step whose conclusion is `skipped` takes no lines and is never a candidate.
2. A line that starts with `##[group]Run `, when the current step already has lines, moves to the first later regular step whose window contains the line's time. With no such step it stays with the current one.
3. A line that is exactly `Post job cleanup.` moves to the first later runner's post step whose window contains the line's time, unless the current step is one of the runner's post steps that has no lines yet; then it stays. With no such step it stays with the current one.
4. A line that is exactly `Cleaning up orphan processes`, when the last step with times is named `Complete job`, moves to that step.
5. Any other line stays with the current step, unless its time is more than one second past the current step's end; then it moves to the next step that has not ended.

Rule 1 names `skipped` only, and that is checked: in job 114404096068 of `darkraise/darkrouter` (read on 2026-10-11) the interrupted step 5 is `cancelled` and lasted from 06:09:18Z to 06:09:40Z, and the steps after it, 6 to 10, are `skipped` with start and end both 06:09:40Z. A `cancelled` step ran and printed output, so it stays a candidate.

The hook steps are checked too. Over the real logs of jobs 111871024678 and 111870951263 of `darkraise/darkmem`, which ghr runners ran, the parser of this task files `Set up runner`, each post step, `Complete runner` and `Complete job` under their own first lines. The old parser already opened `Set up runner` correctly; it left the post steps and `Complete job` empty.

- [ ] **Step 1: Write the failing tests**

In `internal/joblog/joblog_test.go`, inside `TestParseEdges`, add three step lists directly after the line `untimed := []model.Step{{Number: 1, Name: "A"}, {Number: 2, Name: "B"}}`:

```go
	skipped := []model.Step{
		{Number: 1, Name: "Build", Conclusion: "success", StartedAt: at("10:00:00"), CompletedAt: at("10:00:05")},
		{Number: 2, Name: "Publish", Conclusion: "skipped", StartedAt: at("10:00:05"), CompletedAt: at("10:00:05")},
		{Number: 3, Name: "Test", Conclusion: "success", StartedAt: at("10:00:05"), CompletedAt: at("10:00:09")},
	}
	namedPost := []model.Step{
		{Number: 1, Name: "Set up job", StartedAt: at("10:00:00"), CompletedAt: at("10:00:01")},
		{Number: 2, Name: "Post coverage comment", StartedAt: at("10:00:01"), CompletedAt: at("10:00:02")},
		{Number: 3, Name: "Build", StartedAt: at("10:00:02"), CompletedAt: at("10:00:03")},
		{Number: 4, Name: "Post Run actions/checkout@v4", StartedAt: at("10:00:03"), CompletedAt: at("10:00:03")},
		{Number: 5, Name: "Complete job", StartedAt: at("10:00:03"), CompletedAt: at("10:00:03")},
	}
	containers := []model.Step{
		{Number: 1, Name: "Set up job", StartedAt: at("10:00:00"), CompletedAt: at("10:00:01")},
		{Number: 2, Name: "Initialize containers", StartedAt: at("10:00:01"), CompletedAt: at("10:00:05")},
		{Number: 3, Name: "Build", StartedAt: at("10:00:05"), CompletedAt: at("10:00:06")},
		{Number: 4, Name: "Stop containers", StartedAt: at("10:00:06"), CompletedAt: at("10:00:06")},
		{Number: 5, Name: "Complete job", StartedAt: at("10:00:06"), CompletedAt: at("10:00:06")},
	}
```

In the same test, add three rows at the end of the table, directly after the `"no step has times"` row and before the `} {` that closes the table:

```go
		{"a skipped step takes no lines",
			"2026-10-03T10:00:00Z a\n2026-10-03T10:00:05.2Z ##[group]Run go test\n2026-10-03T10:00:05.4Z ok\n", skipped,
			[][]model.LogLine{{{N: 1, Text: "a"}}, {}, {{N: 1, Kind: "group", Text: "Run go test"}, {N: 2, Text: "ok"}}}},
		{"a step a person named Post keeps its header and takes no cleanup line",
			"2026-10-03T10:00:00.1Z a\n2026-10-03T10:00:01.2Z Post job cleanup.\n" +
				"2026-10-03T10:00:01.4Z ##[group]Run gh pr comment\n2026-10-03T10:00:02.2Z ##[group]Run make\n" +
				"2026-10-03T10:00:03.1Z Post job cleanup.\n2026-10-03T10:00:03.3Z Cleaning up orphan processes\n", namedPost,
			[][]model.LogLine{
				{{N: 1, Text: "a"}, {N: 2, Text: "Post job cleanup."}},
				{{N: 1, Kind: "group", Text: "Run gh pr comment"}},
				{{N: 1, Kind: "group", Text: "Run make"}},
				{{N: 1, Text: "Post job cleanup."}},
				{{N: 1, Text: "Cleaning up orphan processes"}}}},
		{"a Run header never moves to a container step",
			"2026-10-03T10:00:00.1Z a\n2026-10-03T10:00:01.5Z ##[group]Run early\n" +
				"2026-10-03T10:00:03Z ##[group]Starting postgres service container\n" +
				"2026-10-03T10:00:05.2Z ##[group]Run make\n2026-10-03T10:00:06.5Z ##[group]Run late\n", containers,
			[][]model.LogLine{
				{{N: 1, Text: "a"}, {N: 2, Kind: "group", Text: "Run early"}},
				{{N: 1, Kind: "group", Text: "Starting postgres service container"}},
				{{N: 1, Kind: "group", Text: "Run make"}, {N: 2, Kind: "group", Text: "Run late"}},
				{}, {}}},
```

The third row also holds the case of a `##[group]Run ` line with no fitting later step: `Run late` stays with `Build`.

Append to the end of the file three fixtures and their test. The first two are shaped like the two real logs the faults were found in, both from GitHub's own runners. The third is shaped like a job a ghr runner ran, with the two hook steps:

```go
// Shaped like job 114400898499 of darkraise/ghr: the last regular step, the
// post steps and "Complete job" all start and end in the same second.
var postSteps = []model.Step{
	{Number: 1, Name: "Set up job", Conclusion: "success", StartedAt: at("05:48:50"), CompletedAt: at("05:48:50")},
	{Number: 2, Name: "Run actions/checkout@v4", Conclusion: "success", StartedAt: at("05:48:50"), CompletedAt: at("05:48:51")},
	{Number: 3, Name: "Run actions/setup-go@v5", Conclusion: "success", StartedAt: at("05:48:51"), CompletedAt: at("05:49:01")},
	{Number: 13, Name: "Run npm run build", Conclusion: "success", StartedAt: at("05:51:13"), CompletedAt: at("05:51:25")},
	{Number: 14, Name: "no inlined fonts", Conclusion: "success", StartedAt: at("05:51:25"), CompletedAt: at("05:51:25")},
	{Number: 27, Name: "Post Run actions/setup-go@v5", Conclusion: "success", StartedAt: at("05:51:25"), CompletedAt: at("05:51:25")},
	{Number: 28, Name: "Post Run actions/checkout@v4", Conclusion: "success", StartedAt: at("05:51:25"), CompletedAt: at("05:51:25")},
	{Number: 29, Name: "Complete job", Conclusion: "success", StartedAt: at("05:51:25"), CompletedAt: at("05:51:25")},
}

const postLog = "2026-10-03T05:48:50.0356954Z Current runner version: '2.337.0'\n" +
	"2026-10-03T05:48:50.8596515Z ##[group]Run actions/checkout@v4\n" +
	"2026-10-03T05:48:51.7647897Z ##[group]Run actions/setup-go@v5\n" +
	"2026-10-03T05:51:13.4998330Z ##[group]Run npm run build\n" +
	"2026-10-03T05:51:19.0988182Z built in 5.19s\n" +
	"2026-10-03T05:51:25.1999952Z ##[group]Run ls dist/assets/*.css > /dev/null\n" +
	"2026-10-03T05:51:25.2062670Z ##[endgroup]\n" +
	"2026-10-03T05:51:25.2274449Z Post job cleanup.\n" +
	"2026-10-03T05:51:25.5063663Z Cache hit occurred on the primary key setup-go\n" +
	"2026-10-03T05:51:25.5318701Z Post job cleanup.\n" +
	"2026-10-03T05:51:25.6228285Z git version 2.55.0\n" +
	"2026-10-03T05:51:25.7394992Z Cleaning up orphan processes\n" +
	"2026-10-03T05:51:25.7718673Z ##[warning]Node.js 20 is deprecated.\n"

// Shaped like job 114375054549 of darkraise/darkraise-ai-plugins: a step
// failed, so the first post step is skipped, with equal start and end times.
var skippedPostSteps = []model.Step{
	{Number: 1, Name: "Set up job", Conclusion: "success", StartedAt: at("03:04:02"), CompletedAt: at("03:04:03")},
	{Number: 2, Name: "Run actions/checkout@v5", Conclusion: "success", StartedAt: at("03:04:03"), CompletedAt: at("03:04:08")},
	{Number: 3, Name: "Run actions/setup-node@v5", Conclusion: "success", StartedAt: at("03:04:08"), CompletedAt: at("03:04:13")},
	{Number: 5, Name: "Run Windows regression suites", Conclusion: "failure", StartedAt: at("03:04:13"), CompletedAt: at("03:10:26")},
	{Number: 9, Name: "Post Run actions/setup-node@v5", Conclusion: "skipped", StartedAt: at("03:10:26"), CompletedAt: at("03:10:26")},
	{Number: 10, Name: "Post Run actions/checkout@v5", Conclusion: "success", StartedAt: at("03:10:26"), CompletedAt: at("03:10:28")},
	{Number: 11, Name: "Complete job", Conclusion: "success", StartedAt: at("03:10:28"), CompletedAt: at("03:10:28")},
}

const skippedPostLog = "2026-10-03T03:04:02.5104326Z Current runner version: '2.337.0'\n" +
	"2026-10-03T03:04:03.4199469Z ##[group]Run actions/checkout@v5\n" +
	"2026-10-03T03:04:08.6692765Z ##[group]Run actions/setup-node@v5\n" +
	"2026-10-03T03:04:13.8946258Z ##[group]Run node scripts/test-all.mjs --shard 5/6\n" +
	"2026-10-03T03:10:26.7660359Z ##[error]Process completed with exit code 1.\n" +
	"2026-10-03T03:10:26.7946449Z Post job cleanup.\n" +
	"2026-10-03T03:10:27.1000000Z git version 2.55.0.windows.1\n" +
	"2026-10-03T03:10:28.8334560Z Cleaning up orphan processes\n"

// Shaped like job 111871024678 of darkraise/darkmem, which a ghr runner ran:
// ghr's job hooks add "Set up runner" after "Set up job" and "Complete runner"
// between the post steps and "Complete job". Each prints its own Run header.
var hookSteps = []model.Step{
	{Number: 1, Name: "Set up job", Conclusion: "success", StartedAt: at("16:41:19"), CompletedAt: at("16:41:22")},
	{Number: 2, Name: "Set up runner", Conclusion: "success", StartedAt: at("16:41:22"), CompletedAt: at("16:41:22")},
	{Number: 3, Name: "Run actions/checkout@v4", Conclusion: "success", StartedAt: at("16:41:22"), CompletedAt: at("16:41:26")},
	{Number: 8, Name: "Build", Conclusion: "success", StartedAt: at("16:42:36"), CompletedAt: at("16:42:40")},
	{Number: 14, Name: "Post Run actions/setup-node@v4", Conclusion: "success", StartedAt: at("16:42:40"), CompletedAt: at("16:42:40")},
	{Number: 15, Name: "Post Run actions/checkout@v4", Conclusion: "success", StartedAt: at("16:42:40"), CompletedAt: at("16:42:40")},
	{Number: 16, Name: "Complete runner", Conclusion: "success", StartedAt: at("16:42:40"), CompletedAt: at("16:42:40")},
	{Number: 17, Name: "Complete job", Conclusion: "success", StartedAt: at("16:42:40"), CompletedAt: at("16:42:40")},
}

const hookLog = "2026-10-03T16:41:19.5104326Z Current runner version: '2.337.0'\n" +
	"2026-10-03T16:41:22.3125688Z A job started hook has been configured by the self-hosted runner administrator\n" +
	"2026-10-03T16:41:22.3198136Z ##[group]Run '/opt/ghr/hooks/job-started.sh'\n" +
	"2026-10-03T16:41:22.3209650Z ##[endgroup]\n" +
	"2026-10-03T16:41:22.3488745Z ##[group]Run actions/checkout@v4\n" +
	"2026-10-03T16:42:36.2000000Z ##[group]Run npx vite build\n" +
	"2026-10-03T16:42:39.9000000Z built in 3.4s\n" +
	"2026-10-03T16:42:40.5000000Z Post job cleanup.\n" +
	"2026-10-03T16:42:40.6000000Z Cache hit occurred on the primary key node-cache\n" +
	"2026-10-03T16:42:40.7000000Z Post job cleanup.\n" +
	"2026-10-03T16:42:40.9000000Z git version 2.55.0\n" +
	"2026-10-03T16:42:40.9390787Z A job completed hook has been configured by the self-hosted runner administrator\n" +
	"2026-10-03T16:42:40.9412987Z ##[group]Run '/opt/ghr/hooks/job-completed.sh'\n" +
	"2026-10-03T16:42:40.9422548Z ##[endgroup]\n" +
	"2026-10-03T16:42:40.9543007Z Cleaning up orphan processes\n"

func TestParseGivesPostStepsTheirLines(t *testing.T) {
	for _, tc := range []struct {
		name  string
		raw   string
		steps []model.Step
		want  [][]string
	}{
		{"post steps in one second", postLog, postSteps, [][]string{
			{"Current runner version: '2.337.0'"},
			{"Run actions/checkout@v4"},
			{"Run actions/setup-go@v5"},
			{"Run npm run build", "built in 5.19s"},
			{"Run ls dist/assets/*.css > /dev/null", ""},
			{"Post job cleanup.", "Cache hit occurred on the primary key setup-go"},
			{"Post job cleanup.", "git version 2.55.0"},
			{"Cleaning up orphan processes", "Node.js 20 is deprecated."},
		}},
		{"a skipped post step", skippedPostLog, skippedPostSteps, [][]string{
			{"Current runner version: '2.337.0'"},
			{"Run actions/checkout@v5"},
			{"Run actions/setup-node@v5"},
			{"Run node scripts/test-all.mjs --shard 5/6", "Process completed with exit code 1."},
			{},
			{"Post job cleanup.", "git version 2.55.0.windows.1"},
			{"Cleaning up orphan processes"},
		}},
		// The line the runner prints ahead of each hook's Run header stays
		// with the step before it.
		{"the job hooks of a ghr runner", hookLog, hookSteps, [][]string{
			{"Current runner version: '2.337.0'", "A job started hook has been configured by the self-hosted runner administrator"},
			{"Run '/opt/ghr/hooks/job-started.sh'", ""},
			{"Run actions/checkout@v4"},
			{"Run npx vite build", "built in 3.4s"},
			{"Post job cleanup.", "Cache hit occurred on the primary key node-cache"},
			{"Post job cleanup.", "git version 2.55.0", "A job completed hook has been configured by the self-hosted runner administrator"},
			{"Run '/opt/ghr/hooks/job-completed.sh'", ""},
			{"Cleaning up orphan processes"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := [][]string{}
			for _, s := range Parse([]byte(tc.raw), tc.steps) {
				texts := []string{}
				for _, l := range s.Lines {
					texts = append(texts, l.Text)
				}
				got = append(got, texts)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("lines %q\nwant  %q", got, tc.want)
			}
		})
	}
}
```

Also append this test. A job whose every step is skipped has no step a line may go to, so no step gets a line, whatever the log holds:

```go
func TestParseGivesNoLinesWhenEveryStepIsSkipped(t *testing.T) {
	steps := []model.Step{
		{Number: 1, Name: "Build", Conclusion: "skipped", StartedAt: at("02:52:44"), CompletedAt: at("02:52:44")},
		{Number: 2, Name: "Test", Conclusion: "skipped"},
	}
	for name, raw := range map[string]string{
		"an empty log":     "",
		"a log with lines": "2026-10-03T02:52:44.0000000Z ##[group]Run make\n2026-10-03T02:52:44.1000000Z done\n",
	} {
		for _, s := range Parse([]byte(raw), steps) {
			if len(s.Lines) != 0 {
				t.Errorf("%s: skipped step %d got %d lines", name, s.Number, len(s.Lines))
			}
		}
	}
}
```

Against the old parser it fails with `an empty log: skipped step 1 got 1 lines` and `a log with lines: skipped step 1 got 2 lines`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/joblog/ -run 'TestParseEdges|TestParseGivesPostStepsTheirLines|TestParseGivesNoLinesWhenEveryStepIsSkipped' -count=1`

Expected: FAIL in six subtests, each because the old parser files lines under the wrong step:

- `TestParseEdges/a_skipped_step_takes_no_lines`: `Run go test` and `ok` are under the skipped step `Publish`, and `Test` is empty.
- `TestParseEdges/a_step_a_person_named_Post_keeps_its_header_and_takes_no_cleanup_line`: the second `Post job cleanup.` and `Cleaning up orphan processes` are under `Build`, and the last two steps are empty.
- `TestParseEdges/a_Run_header_never_moves_to_a_container_step`: `Run early` is under `Initialize containers` and `Run late` under `Stop containers`.
- `TestParseGivesPostStepsTheirLines/post_steps_in_one_second`: everything from the first `Post job cleanup.` on is under `no inlined fonts`, and the last three steps are empty.
- `TestParseGivesPostStepsTheirLines/a_skipped_post_step`: `Post job cleanup.` is under `Run Windows regression suites`, and `Complete job` is empty.
- `TestParseGivesPostStepsTheirLines/the_job_hooks_of_a_ghr_runner`: both `Post job cleanup.` blocks are under `Build`; `Run '/opt/ghr/hooks/job-completed.sh'` and `Cleaning up orphan processes` are under `Post Run actions/setup-node@v4`; the last three steps are empty. The first three steps are already right: the old parser opens `Set up runner` correctly.

- [ ] **Step 3: Write the implementation**

In `internal/joblog/joblog.go`, replace the whole `Parse` function together with its comment (from the line `// Parse splits raw into one LogStep per step, in the order given. Steps that` down to the function's closing brace, which is just above `// LevelRank orders annotation levels`) with:

```go
// Steps the runner names itself. They print no "##[group]Run " header.
const (
	setUpJob       = "Set up job"
	initContainers = "Initialize containers"
	stopContainers = "Stop containers"
	completeJob    = "Complete job"
)

// completeRunner is the step a job-completed hook adds between the post steps
// and "Complete job"; ghr sets that hook on every runner it manages. Unlike
// the steps above it prints a "##[group]Run " header, as "Set up runner", the
// job-started hook's step, does.
const completeRunner = "Complete runner"

// The lines the runner prints first for a post step and for "Complete job".
const (
	postHeader     = "Post job cleanup."
	completeHeader = "Cleaning up orphan processes"
)

// runnerPosts marks the runner's post steps. They are found by position:
// walking back from the last step past "Complete job", "Complete runner" and
// "Stop containers", the unbroken run of steps whose name starts with "Post ".
// A step a person named that way earlier in the job is not one.
func runnerPosts(steps []model.Step) []bool {
	post := make([]bool, len(steps))
	i := len(steps) - 1
	for i >= 0 && (steps[i].Name == completeJob || steps[i].Name == completeRunner || steps[i].Name == stopContainers) {
		i--
	}
	for ; i >= 0 && strings.HasPrefix(steps[i].Name, "Post "); i-- {
		post[i] = true
	}
	return post
}

// regular reports whether a step prints a "##[group]Run " header of its own;
// post says whether it is one of the runner's post steps.
func regular(s model.Step, post bool) bool {
	switch s.Name {
	case setUpJob, initContainers, stopContainers, completeJob:
		return false
	}
	return !post
}

// Parse splits raw into one LogStep per step, in the order given. Steps that
// never started and skipped steps get no lines. Lines without a timestamp stay
// with the line before them.
//
// One pointer, the current step, moves forward through the steps. GitHub
// rounds step times to whole seconds, so times alone cannot split steps that
// share a second; the lines the runner prints first for a step do:
//   - a "##[group]Run " line, when the current step already has lines, moves to
//     the first later regular step whose window holds the line's time;
//   - a "Post job cleanup." line moves to the first later runner's post step
//     whose window holds the line's time, unless the current step is such a
//     step with no lines yet;
//   - a "Cleaning up orphan processes" line moves to "Complete job" when that
//     is the last step with times;
//   - any other line more than a second past the current step's end moves to
//     the next step that has not ended.
//
// A line with no step to move to stays with the current step.
func Parse(raw []byte, steps []model.Step) []model.LogStep {
	out := make([]model.LogStep, len(steps))
	for i, s := range steps {
		out[i] = model.LogStep{Number: s.Number, Name: s.Name, Lines: []model.LogLine{}}
	}
	if len(steps) == 0 {
		return out
	}
	post := runnerPosts(steps)
	// timed holds the steps a line can go to. A skipped step has times, equal
	// ones, and prints nothing, so it is left out.
	var timed []int
	// complete is the place in timed of "Complete job" when that is the last
	// step with times, and -1 otherwise.
	complete := -1
	// first takes every line when timed is empty.
	first := -1
	for i, s := range steps {
		skipped := s.Conclusion == "skipped"
		if first < 0 && !skipped {
			first = i
		}
		if s.StartedAt == nil {
			continue
		}
		complete = -1
		if skipped {
			continue
		}
		timed = append(timed, i)
		if s.Name == completeJob {
			complete = len(timed) - 1
		}
	}
	// With every step skipped there is no step a line may go to.
	if first < 0 {
		return out
	}
	// end is a step's completion plus slack, or no bound while it has none.
	end := func(k int) (time.Time, bool) {
		c := steps[timed[k]].CompletedAt
		if c == nil {
			return time.Time{}, false
		}
		return c.Add(slack), true
	}
	contains := func(k int, t time.Time) bool {
		if t.Before(steps[timed[k]].StartedAt.Add(-slack)) {
			return false
		}
		e, ok := end(k)
		return !ok || !t.After(e)
	}

	text := strings.TrimPrefix(string(raw), "\uFEFF")
	text = strings.TrimSuffix(text, "\n")
	p := 0
	// next is the first place after p that fits, or p when none does.
	next := func(fits func(k int) bool) int {
		for k := p + 1; k < len(timed); k++ {
			if fits(k) {
				return k
			}
		}
		return p
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		at, body, ok := split(line)
		if ok && len(timed) > 0 {
			cur := timed[p]
			hasLines := len(out[cur].Lines) > 0
			switch {
			case strings.HasPrefix(body, "##[group]Run ") && hasLines:
				p = next(func(k int) bool { return regular(steps[timed[k]], post[timed[k]]) && contains(k, at) })
			case body == postHeader:
				if !post[cur] || hasLines {
					p = next(func(k int) bool { return post[timed[k]] && contains(k, at) })
				}
			case body == completeHeader && complete >= 0:
				p = complete
			default:
				if e, bounded := end(p); bounded && at.After(e) {
					p = next(func(k int) bool {
						e2, b2 := end(k)
						return !b2 || !e2.Before(at)
					})
				}
			}
		}
		idx := first
		if len(timed) > 0 {
			idx = timed[p]
		}
		k, t := kind(body)
		out[idx].Lines = append(out[idx].Lines, model.LogLine{N: len(out[idx].Lines) + 1, Kind: k, Text: t})
	}
	return out
}
```

How the code maps to the rules: leaving skipped steps out of `timed` is rule 1, and it holds for every rule that picks a step. The `switch` holds rules 2 to 5 in order. A `##[group]Run ` line that arrives while the current step has no lines is not rule 2's case and falls to the last case, as it did before. `first` is where every line goes when no step has usable times; it is the first step that is not skipped, so rule 1 holds there too.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/joblog/ -run 'TestParseEdges|TestParseGivesPostStepsTheirLines|TestParseGivesNoLinesWhenEveryStepIsSkipped' -count=1`

Expected: PASS

- [ ] **Step 5: Correct the existing test the new rule changes**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/joblog/ -count=1`

Expected: FAIL in `TestParseSplitsTheDarkmemLog` with `line counts [1 4 0 3 5 6 0 1], want [1 3 1 3 5 6 0 1]`. Its fixture has a `##[group]Run docker compose up -d` line at 02:52:44.9, which the old parser moved to step 3, `Initialize containers`. That step prints no `##[group]Run ` header, so under rule 2 the line now stays with step 2. The fixture is left as it is and two expectations change.

In `TestParseSplitsTheDarkmemLog` replace

```go
	if want := []int{1, 3, 1, 3, 5, 6, 0, 1}; !reflect.DeepEqual(counts, want) {
```

with

```go
	if want := []int{1, 4, 0, 3, 5, 6, 0, 1}; !reflect.DeepEqual(counts, want) {
```

and replace

```go
		{3, 1, "group", "Run docker compose up -d"},
```

with

```go
		// "Initialize containers" prints no Run header, so this one stays put.
		{2, 4, "group", "Run docker compose up -d"},
```

- [ ] **Step 6: Run the package suites**

`internal/daemon` parses logs through `joblog.Parse`, so its suite runs too.

Run: `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/`
Expected: no output

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/joblog/ ./internal/daemon/ -count=1`
Expected: PASS for both packages

- [ ] **Step 7: Commit**

```bash
git add internal/joblog/joblog.go internal/joblog/joblog_test.go
git commit -m "fix(joblog): split steps by the runner's markers"
```

### Task 8: A job with no log: the client

**Files:**
- Modify: `internal/github/download.go` (`OpenJobLog`)
- Test: `internal/github/download_test.go` (`TestDownloadErrors`)

**Interfaces:**
- Consumes: nothing
- Produces: `Client.OpenJobLog` and `Client.JobLog` in `internal/github` answer a job that has no log with an `*APIError` of kind `ErrNotFound`. The next task turns that answer into `logNone` (Contracts).

**Items:** 26
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

A job cancelled before it started completes without ever running, so it has no log. The daemon is to treat such a job as one that produced no log, not as a failed read, and for that it must be able to tell the case apart. Today it cannot.

What GitHub answers was checked on 2026-10-11 with job 114404210515 of `darkraise/darkrouter` (completed, cancelled, no steps, no runner). `GET /repos/darkraise/darkrouter/actions/jobs/114404210515/logs` answered `302 Found` from api.github.com, as it does for any job, and the address it redirected to answered `404 The specified blob does not exist` (`x-ms-error-code: BlobNotFound`). An expired log is different: api.github.com answers `410 Gone` itself (job 64934606621 of `darkraise/flashpoint-web`), and the client already reports that as kind `ErrGone`.

So the 404 arrives at the download, which `OpenJobLog` reports today as the plain error `github: log download answered 404`. This task makes it an `*APIError` of kind `ErrNotFound`. Any other status from the download stays the plain error it is.

- [ ] **Step 1: Write the failing test**

In `internal/github/download_test.go`, in `TestDownloadErrors`, replace

```go
		call    func(c *Client) error
		kind    ErrKind
		plain   string
	}{
```

with

```go
		call    func(c *Client) error
		kind    ErrKind
		plain   string
		store   int // when set, GitHub redirects to a log store that answers this status
	}{
```

and replace

```go
			plain: "github: /repos/darkraise/darkmem/actions/artifacts/11/zip answered 200 without a download address"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
```

with

```go
			plain: "github: /repos/darkraise/darkmem/actions/artifacts/11/zip answered 200 without a download address"},
		{name: "no log", store: 404, kind: ErrNotFound,
			call: func(c *Client) error { _, err := c.JobLog(ctx, "darkmem", 1, 10); return err }},
		{name: "no log, opened", store: 404, kind: ErrNotFound,
			call: func(c *Client) error { _, _, err := c.OpenJobLog(ctx, "darkmem", 1); return err }},
		{name: "log store error", store: 500,
			call:  func(c *Client) error { _, err := c.JobLog(ctx, "darkmem", 1, 10); return err },
			plain: "github: log download answered 500"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.store)
			}))
			t.Cleanup(store.Close)
			c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.store != 0 {
					http.Redirect(w, r, store.URL+"/log?sig=1", http.StatusFound)
					return
				}
				for k, v := range tc.headers {
```

The rest of the loop body stays as it is. In the three new rows the fake GitHub redirects to a second server, the log store, which answers the row's status. The third row pins that a status other than 404 stays a plain error.

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/github/ -run TestDownloadErrors -count=1`

Expected: FAIL in `TestDownloadErrors/no_log` and `TestDownloadErrors/no_log,_opened`, both with `err &errors.errorString{s:"github: log download answered 404"}, want kind 3`. `TestDownloadErrors/log_store_error` passes.

- [ ] **Step 3: Write the implementation**

In `internal/github/download.go`, in `OpenJobLog`, replace

```go
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("github: log download answered %d", resp.StatusCode)
	}
```

with

```go
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			// GitHub redirects for a job that never ran as for any other; only
			// the store it redirects to knows that the job has no log.
			return nil, 0, &APIError{Status: http.StatusNotFound, Kind: ErrNotFound, Message: "this job has no log"}
		}
		return nil, 0, fmt.Errorf("github: log download answered %d", resp.StatusCode)
	}
```

and in the comment above `OpenJobLog` replace

```go
// Content-Length, -1 when GitHub sends none. The body stays bound to ctx; the
// caller closes it.
```

with

```go
// Content-Length, -1 when GitHub sends none. The body stays bound to ctx; the
// caller closes it. A job that has no log is an *APIError of kind ErrNotFound.
```

`JobLog` calls `OpenJobLog`, so it returns the same error. The error is built here and never passes through `record`, so it does not touch the token verdict.

- [ ] **Step 4: Run the package suite**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/github/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (must print nothing), then:

```bash
git add internal/github/download.go internal/github/download_test.go
git commit -m "fix(github): report a job without a log as 404"
```

### Task 9: A job with no log: the daemon

**Files:**
- Modify: `internal/model/model.go` (`JobLog`), `internal/daemon/joblogs.go` (the `logOutcome` constants, the comment on `logMiss`, `readLog`, `flaggedLog`, the comment on `JobLog`, `openRawLog`), `internal/daemon/annotations.go` (`RunAnnotations`), `web/src/api/fixtures/job-log.json` (regenerated from the Go type)
- Test: `internal/daemon/joblogs_test.go` (`TestJobLogOutcomes`, `TestRawLogErrorsReleaseEverything`), `internal/daemon/annotations_test.go` (`TestRunAnnotationsCountsLogOutcomes`)

**Interfaces:**
- Consumes: `Client.OpenJobLog` and `Client.JobLog` in `internal/github` answer a job that has no log with an `*APIError` of kind `ErrNotFound`, which `github.IsKind(err, github.ErrNotFound)` recognises. The caches in `internal/daemon/joblogs.go` are `*lru.Cache` (Contracts) by now; no line this task changes calls them.
- Produces: `logNone` (Contracts); `model.JobLog.NoLog` with the JSON key `no_log` (Contracts)

**Items:** 26
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

A job cancelled before it started completes without ever running, so it has no log. Today `RunAnnotations` counts every completed job whose log cannot be read as a failed read, and the job page shows an error for such a job. After this task a completed job whose log request ends in a 404 has the outcome `logNone`: `JobLog` returns it flagged `no_log`, `RunAnnotations` counts it nowhere, and the raw log route answers 404 with `this job produced no log`. The outcome is remembered for 10 minutes, like an expired log.

`finishedJob` reads the job from GitHub before any log is requested, so a 404 for the log is about the log, not about the repository or the job.

- [ ] **Step 1: Add the field to the model**

In `internal/model/model.go` replace

```go
// JobLog is GET /actions/{repo}/jobs/{jobId}/log: a finished job's log split
// into its steps.
type JobLog struct {
	JobID    int64     `json:"job_id"`
	TooLarge bool      `json:"too_large"`
	Expired  bool      `json:"expired"`
	Steps    []LogStep `json:"steps"`
}
```

with

```go
// JobLog is GET /actions/{repo}/jobs/{jobId}/log: a finished job's log split
// into its steps. NoLog marks a job that never ran, so GitHub has no log for
// it.
type JobLog struct {
	JobID    int64     `json:"job_id"`
	TooLarge bool      `json:"too_large"`
	Expired  bool      `json:"expired"`
	NoLog    bool      `json:"no_log"`
	Steps    []LogStep `json:"steps"`
}
```

Nothing sets the field yet. It comes first so that the tests of the next step compile and fail on behaviour.

- [ ] **Step 2: Write the failing tests**

The daemon tests' fake GitHub answers a log request with `ghNotFound()`, an `*github.APIError` of kind `ErrNotFound`, which is what the real client returns for a job that has no log.

In `internal/daemon/joblogs_test.go`, in `TestJobLogOutcomes`, replace

```go
		{"too large", github.ErrTooLarge, model.JobLog{JobID: 1002, TooLarge: true}, 0, true},
		{"failed", errors.New("boom"), model.JobLog{}, 502, false},
```

with

```go
		{"too large", github.ErrTooLarge, model.JobLog{JobID: 1002, TooLarge: true}, 0, true},
		{"no log", ghNotFound(), model.JobLog{JobID: 1002, NoLog: true}, 0, true},
		{"failed", errors.New("boom"), model.JobLog{}, 502, false},
```

and, in the same test, replace

```go
				if apiStatus(err) != tc.status || l.JobID != tc.want.JobID || l.Expired != tc.want.Expired || l.TooLarge != tc.want.TooLarge {
```

with

```go
				if apiStatus(err) != tc.status || l.JobID != tc.want.JobID || l.Expired != tc.want.Expired || l.TooLarge != tc.want.TooLarge || l.NoLog != tc.want.NoLog {
```

The row's last field, `true`, is `retained`: the test asks twice and expects one download, which is the 10 minutes of remembering.

In `TestRawLogErrorsReleaseEverything`, replace

```go
			410, "GitHub no longer keeps this job's log", 0, true},
		{"failed", func(b *Backend, gh *fakeGH) { gh.logErr[1002] = errors.New("boom") }, 502, "", 0, true},
```

with

```go
			410, "GitHub no longer keeps this job's log", 0, true},
		{"no log", func(b *Backend, gh *fakeGH) { gh.logErr[1002] = ghNotFound() }, 404, "this job produced no log", 0, true},
		{"failed", func(b *Backend, gh *fakeGH) { gh.logErr[1002] = errors.New("boom") }, 502, "", 0, true},
```

In `internal/daemon/annotations_test.go`, in `TestRunAnnotationsCountsLogOutcomes`, replace

```go
	annotatedRun(gh, logJobOf(gh, 2001, "a", "x"), logJobOf(gh, 2002, "b", "x"), logJobOf(gh, 2003, "c", "x"))
	gh.logErr[2001] = &github.APIError{Status: 410, Kind: github.ErrGone}
	gh.logErr[2002] = github.ErrTooLarge
	gh.logErr[2003] = errors.New("boom")
	ctx := context.Background()
	for i, step := range []struct {
		at        time.Duration
		downloads int
	}{{0, 3}, {logRetryTTL - time.Second, 3}, {logRetryTTL, 6}} {
```

with

```go
	annotatedRun(gh, logJobOf(gh, 2001, "a", "x"), logJobOf(gh, 2002, "b", "x"), logJobOf(gh, 2003, "c", "x"), logJobOf(gh, 2004, "d", "x"))
	gh.logErr[2001] = &github.APIError{Status: 410, Kind: github.ErrGone}
	gh.logErr[2002] = github.ErrTooLarge
	gh.logErr[2003] = errors.New("boom")
	// Job d never ran and has no log: it is counted nowhere.
	gh.logErr[2004] = ghNotFound()
	ctx := context.Background()
	for i, step := range []struct {
		at        time.Duration
		downloads int
	}{{0, 4}, {logRetryTTL - time.Second, 4}, {logRetryTTL, 8}} {
```

The loop below these lines stays as it is: it still expects one expired log, one too large and one failed, so the fourth job must add to none of them. The download counts grow by one per round because the fourth job is asked for again once the 10 minutes are over.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run 'TestJobLogOutcomes|TestRawLogErrorsReleaseEverything|TestRunAnnotationsCountsLogOutcomes' -count=1`

Expected: FAIL, with these three failures:

- `TestJobLogOutcomes/no_log`: `call 0: log {JobID:0 TooLarge:false Expired:false NoLog:false Steps:[]} err not found on GitHub`
- `TestRawLogErrorsReleaseEverything/no_log`: `err not found on GitHub, want 404 "this job produced no log"`
- `TestRunAnnotationsCountsLogOutcomes`: `step 0: {Items:[] Omitted:map[] Unread:0 ExpiredLogs:1 TooLarge:1 Failed:2} err <nil>`

- [ ] **Step 4: Write the implementation**

In `internal/daemon/joblogs.go`, in the `logOutcome` constants, replace

```go
	logTooLarge
	logFailed
```

with

```go
	logTooLarge
	// logNone is a completed job GitHub has no log for: the job never ran.
	logNone
	logFailed
```

Replace the comment above `logMiss`

```go
// logMiss is an expired, too large or failed outcome, kept logRetryTTL.
```

with

```go
// logMiss is an expired, too large, absent or failed outcome, kept
// logRetryTTL.
```

In `readLog`, replace

```go
	case github.IsKind(err, github.ErrGone):
		return logResult{outcome: logExpired}, 0
	}
```

with

```go
	case github.IsKind(err, github.ErrGone):
		return logResult{outcome: logExpired}, 0
	case github.IsKind(err, github.ErrNotFound):
		// finishedJob read this job before the download started, so the 404
		// is about the log, not about the repository or the job.
		return logResult{outcome: logNone}, 0
	}
```

In `flaggedLog`, replace

```go
	return model.JobLog{JobID: id, TooLarge: o == logTooLarge, Expired: o == logExpired, Steps: []model.LogStep{}}
```

with

```go
	return model.JobLog{JobID: id, TooLarge: o == logTooLarge, Expired: o == logExpired, NoLog: o == logNone, Steps: []model.LogStep{}}
```

Replace the comment above `JobLog`

```go
// JobLog returns a finished job's log split into steps. An expired or too
// large log comes back flagged; a failed download is tried again.
```

with

```go
// JobLog returns a finished job's log split into steps. An expired, too large
// or absent log comes back flagged; a failed download is tried again.
```

In `openRawLog`, replace

```go
		return nil, 0, &api.Error{Status: http.StatusGone, Msg: "GitHub no longer keeps this job's log"}
	case err != nil:
```

with

```go
		return nil, 0, &api.Error{Status: http.StatusGone, Msg: "GitHub no longer keeps this job's log"}
	case github.IsKind(err, github.ErrNotFound):
		return nil, 0, api.NotFound("this job produced no log")
	case err != nil:
```

Nothing else in `joblogs.go` changes. `downloadLog` already keeps every outcome that is neither parsed nor busy as a miss, and `JobLog` already serves a miss that is not a failed one as a flagged log for `logRetryTTL`, so `logNone` is remembered for 10 minutes by the existing code.

`RunAnnotations` needs no new branch either: its counting `switch` names the outcomes it counts, and `logNone` is none of them. Say so where a reader would look for it. In `internal/daemon/annotations.go` replace

```go
		if o, ok := outcomes[j.ID]; ok {
			switch o {
```

with

```go
		if o, ok := outcomes[j.ID]; ok {
			// logNone is counted nowhere: a job that never ran has no log to
			// expire, to be too large or to fail on.
			switch o {
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run 'TestJobLogOutcomes|TestRawLogErrorsReleaseEverything|TestRunAnnotationsCountsLogOutcomes' -count=1`

Expected: PASS

- [ ] **Step 6: Regenerate the web fixture**

`web/fixtures_test.go` keeps `web/src/api/fixtures/` equal to the JSON of the Go types, so the new field makes one fixture stale.

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./web/ -count=1`

Expected: FAIL with `web/src/api/fixtures/job-log.json is out of date with the Go types; regenerate with: GHR_UPDATE_FIXTURES=1 go test ./web/`

Run: `GHR_UPDATE_FIXTURES=1 PATH=/usr/local/go/bin:$PATH timeout 300 go test ./web/ -count=1`

Run: `git diff --stat -- web/`

Expected: one file, `web/src/api/fixtures/job-log.json`, with one inserted line. The file now begins:

```json
{
  "job_id": 1002,
  "too_large": false,
  "expired": false,
  "no_log": false,
  "steps": [
```

The TypeScript type `JobLog` does not name `no_log` yet. The web code still compiles, because a JSON file may carry a property its type does not name.

- [ ] **Step 7: Run the suites**

Run: `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/`
Expected: no output

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/model/ ./internal/daemon/ ./internal/api/ ./web/ -count=1`
Expected: PASS for all four packages

In `web/`, because a file under `web/` changed:

Run: `timeout 300 npx vitest run src/api/types.test.ts`
Expected: PASS

Run: `timeout 300 npm run lint` and `timeout 300 npm run typecheck`
Expected: both end without an error

- [ ] **Step 8: Commit**

```bash
git add internal/model/model.go internal/daemon/joblogs.go internal/daemon/joblogs_test.go internal/daemon/annotations.go internal/daemon/annotations_test.go web/src/api/fixtures/job-log.json
git commit -m "fix(daemon): flag a job that produced no log"
```

### Task 10: A job with no log: the job page

**Files:**
- Modify: `web/src/api/types.ts` (`JobLog`), `web/src/pages/run.tsx` (`JobLogs`)
- Test: `web/src/pages/run.test.tsx`, `web/src/components/log-viewer.test.tsx`, `web/src/lib/log-view.test.ts`

**Interfaces:**
- Consumes: the JSON key `no_log` of `model.JobLog` (Contracts). The daemon sends it on every job log, and `web/src/api/fixtures/job-log.json` already carries `"no_log": false`.
- Produces: TypeScript `JobLog.no_log: boolean` (Contracts)

**Items:** 26
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

A job cancelled before it started has no log. The daemon answers the log request for such a job with `no_log: true` and no steps. Today the job page does not know the flag and draws an empty log viewer with a `Raw log` button whose download can only fail. After this task the page shows `This job produced no log.` and offers no raw log download.

The `npx` and `npm` commands of this task run in `web/` (`cd web` first). `git` runs from the repository root, which is what the paths in the commit step are relative to.

- [ ] **Step 1: Write the failing test**

In `web/src/pages/run.test.tsx`, in the `Job page` block, add a test directly after the test `says when GitHub no longer keeps the log`. Replace

```tsx
    expect(await screen.findByText("GitHub no longer keeps this job's log.")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Raw log" })).toBeNull()
  })
```

with

```tsx
    expect(await screen.findByText("GitHub no longer keeps this job's log.")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Raw log" })).toBeNull()
  })

  it("says when the job produced no log and offers no raw log", async () => {
    mockApi(runRoutes({ "GET /api/actions/darkmem/jobs/1002/log": { ...fixtures.jobLog, no_log: true, steps: [] } }))
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    expect(await screen.findByText("This job produced no log.")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Raw log" })).toBeNull()
    expect(screen.queryByLabelText("Search logs")).toBeNull()
  })
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 300 npx vitest run src/pages/run.test.tsx`

Expected: FAIL in `Job page > says when the job produced no log and offers no raw log` with `Unable to find an element with the text: This job produced no log.`; the other tests of the file pass.

- [ ] **Step 3: Write the implementation**

In `web/src/api/types.ts`, in `JobLog`, replace

```ts
export interface JobLog {
  job_id: number
  too_large: boolean
  expired: boolean
  steps: LogStep[]
}
```

with

```ts
export interface JobLog {
  job_id: number
  too_large: boolean
  expired: boolean
  no_log: boolean
  steps: LogStep[]
}
```

In `web/src/pages/run.tsx`, in `JobLogs`, replace

```tsx
  if (log.data.expired) return <p className="text-sm text-muted-foreground">GitHub no longer keeps this job&apos;s log.</p>
```

with

```tsx
  if (log.data.expired) return <p className="text-sm text-muted-foreground">GitHub no longer keeps this job&apos;s log.</p>
  if (log.data.no_log) return <p className="text-sm text-muted-foreground">This job produced no log.</p>
```

The `Raw log` button is drawn only by the branches below this line, so returning here is what leaves it out.

- [ ] **Step 4: Give the test builders the new field**

Run: `timeout 300 npm run typecheck`

Expected: four errors with the code `TS2741`, each saying that property `no_log` is missing in a type where `JobLog` requires it, at `src/components/log-viewer.test.tsx` lines 24, 83 and 174 and `src/lib/log-view.test.ts` line 73. These are the four places that build a `JobLog` by hand.

In `web/src/components/log-viewer.test.tsx`, in `bigLog`, replace

```tsx
  return { job_id: 9, too_large: false, expired: false, steps: [{ number: 1, name: "Build", lines }] }
```

with

```tsx
  return { job_id: 9, too_large: false, expired: false, no_log: false, steps: [{ number: 1, name: "Build", lines }] }
```

In the test `prefixes notices without a tint and warnings with one`, replace

```tsx
      job_id: 9,
      too_large: false,
      expired: false,
      steps: [
```

with

```tsx
      job_id: 9,
      too_large: false,
      expired: false,
      no_log: false,
      steps: [
```

In the test `keeps a group that straddles the cut folded under its header`, replace

```tsx
    const log: JobLog = { job_id: 9, too_large: false, expired: false, steps: [{ number: 1, name: "Build", lines }] }
```

with

```tsx
    const log: JobLog = { job_id: 9, too_large: false, expired: false, no_log: false, steps: [{ number: 1, name: "Build", lines }] }
```

In `web/src/lib/log-view.test.ts`, in the test `counts several occurrences in one line and skips endgroup lines`, replace

```ts
      job_id: 1,
      too_large: false,
      expired: false,
      steps: [{ number: 1, name: "s", lines: [line(1, "", "aa aa"), line(2, "endgroup", "aa")] }],
```

with

```ts
      job_id: 1,
      too_large: false,
      expired: false,
      no_log: false,
      steps: [{ number: 1, name: "s", lines: [line(1, "", "aa aa"), line(2, "endgroup", "aa")] }],
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `timeout 300 npx vitest run src/pages/run.test.tsx src/api/types.test.ts src/lib/log-view.test.ts src/components/log-viewer.test.tsx`

Expected: PASS for all four files

Run: `timeout 300 npm run lint` and `timeout 300 npm run typecheck`

Expected: both end without an error

- [ ] **Step 6: Commit**

```bash
git add web/src/api/types.ts web/src/pages/run.tsx web/src/pages/run.test.tsx web/src/components/log-viewer.test.tsx web/src/lib/log-view.test.ts
git commit -m "fix(web): say when a job produced no log"
```

### Task 11: Graph matching

**Files:**
- Modify: `internal/wfgraph/wfgraph.go` (`rawJob`, new type `strategy`, `Parse`, `pattern`, new method `better`, `newPattern`, `Build`), `internal/daemon/rungraph.go` (the note constants, `runGraph`)
- Test: `internal/wfgraph/wfgraph_test.go` (`ciYAML`, `TestParse`, `TestBuildMatching`), `internal/daemon/rungraph_test.go` (`TestRunGraphNotes`), `internal/daemon/rundetail_test.go` (`TestRunGraphPanicCostsOnlyTheGraph`)

**Interfaces:**
- Consumes: nothing. `runGraph` has a case for `github.ErrBadContent` (Contracts) by now; no line this task changes is near it.
- Produces: `noteNoJobs = "This run has no jobs yet."` in `internal/daemon/rungraph.go` (Contracts)

**Items:** 41
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

The run page draws a graph of a run's jobs from the workflow file. `wfgraph.Parse` reads the file's jobs, `wfgraph.Build` puts each of the run's jobs on the workflow job whose name pattern matches it, and `runGraph` in the daemon calls both. Three small faults are closed here:

- When two patterns match a job's name with equal literal prefixes, the earlier workflow job wins today, even when the later pattern is the more specific one. After this task the pattern with more literal text in total wins, and only a remaining tie goes to the earlier job.
- A `strategy` written as one expression, such as `strategy: ${{ fromJSON(needs.plan.outputs.strategy) }}`, fails the whole file today, so the run page shows no graph. After this task it is accepted and the job counts as a matrix job.
- A run with no jobs yet gets an empty graph and no explanation today. After this task `runGraph` returns no graph and the note `This run has no jobs yet.`, and it does so before the workflow file is read.

- [ ] **Step 1: Write the failing wfgraph tests**

In `internal/wfgraph/wfgraph_test.go`, at the end of the `ciYAML` constant, replace

```go
  release:
    needs: [docs, frontend-tests]
    uses: ./.github/workflows/release.yml
`
```

with

```go
  release:
    needs: [docs, frontend-tests]
    uses: ./.github/workflows/release.yml
  deploy:
    strategy: ${{ fromJSON(needs.pick-runner.outputs.strategy) }}
`
```

In `TestParse`, replace

```go
		{Key: "release", Needs: []string{"docs", "frontend-tests"}, Uses: true},
	}
```

with

```go
		{Key: "release", Needs: []string{"docs", "frontend-tests"}, Uses: true},
		// A strategy written as one expression counts as a matrix.
		{Key: "deploy", Matrix: true},
	}
```

In `TestBuildMatching`, add a row above the `ties go to the earlier job` row. Replace

```go
		{"ties go to the earlier job", []Job{{Key: "a", Name: "Test ${{ matrix.x }}"}, {Key: "b", Name: "Test ${{ matrix.y }}"}}, "Test 1", "a"},
```

with

```go
		{"more literal text wins among equal prefixes", []Job{{Key: "any", Name: "Test ${{ matrix.os }}"}, {Key: "arm", Name: "Test ${{ matrix.os }} on arm"}}, "Test linux on arm", "arm"},
		{"ties go to the earlier job", []Job{{Key: "a", Name: "Test ${{ matrix.x }}"}, {Key: "b", Name: "Test ${{ matrix.y }}"}}, "Test 1", "a"},
```

Both patterns of the new row match `Test linux on arm` and both have the literal prefix `Test `. The second has 12 bytes of literal text against 5, so it must win although it comes later. The row below it stays as the guard for a real tie: equal prefixes and equal totals go to the earlier job.

`TestBuildLaysOutTheRun` parses `ciYAML` too and needs no change: no job of its run matches `deploy`, and a workflow job without a run is left out of the graph.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/wfgraph/ -count=1`

Expected: FAIL, with these three failures:

- `TestParse` and `TestBuildLaysOutTheRun`, both with `wfgraph: invalid workflow: job deploy: yaml: unmarshal errors:` followed by `cannot unmarshal !!str` and `into struct { Matrix interface {} "yaml:\"matrix\"" }`
- `TestBuildMatching/more_literal_text_wins_among_equal_prefixes`: `nodes [{Key:any Name:Test ${{ matrix.os }} Column:0 Needs:[] JobIDs:[1] Group:false}] err <nil>`

- [ ] **Step 3: Write the wfgraph implementation**

In `internal/wfgraph/wfgraph.go`, replace the `rawJob` type

```go
type rawJob struct {
	Name     string    `yaml:"name"`
	Needs    needsList `yaml:"needs"`
	Strategy struct {
		Matrix any `yaml:"matrix"`
	} `yaml:"strategy"`
	Uses string `yaml:"uses"`
}
```

with

```go
// strategy tells whether a job's strategy has a matrix. A strategy written as
// one expression counts as having one: what it expands to is not in the file.
type strategy struct {
	matrix bool
}

func (s *strategy) UnmarshalYAML(v *yaml.Node) error {
	if v.Kind == yaml.ScalarNode {
		s.matrix = true
		return nil
	}
	var m struct {
		Matrix any `yaml:"matrix"`
	}
	if err := v.Decode(&m); err != nil {
		return err
	}
	s.matrix = m.Matrix != nil
	return nil
}

type rawJob struct {
	Name     string    `yaml:"name"`
	Needs    needsList `yaml:"needs"`
	Strategy strategy  `yaml:"strategy"`
	Uses     string    `yaml:"uses"`
}
```

The YAML decoder does not call `UnmarshalYAML` for a missing or null `strategy`, so such a job keeps `matrix` false, and it follows an alias before the call, so the method never sees one.

In `Parse`, replace

```go
			Matrix: raw.Strategy.Matrix != nil, Uses: raw.Uses != ""})
```

with

```go
			Matrix: raw.Strategy.matrix, Uses: raw.Uses != ""})
```

Replace the `pattern` type

```go
type pattern struct {
	label  string
	prefix string
	re     *regexp.Regexp
}
```

with

```go
type pattern struct {
	label  string
	prefix string
	// literal is how many bytes of the label are literal text, not expressions.
	literal int
	re      *regexp.Regexp
}

// better reports whether p is the closer match than q for a name both match:
// the longer literal prefix wins, then the more literal text in total.
func (p pattern) better(q pattern) bool {
	if len(p.prefix) != len(q.prefix) {
		return len(p.prefix) > len(q.prefix)
	}
	return p.literal > q.literal
}
```

Replace the whole `newPattern` function, keeping the comment above it, with:

```go
func newPattern(j Job) pattern {
	label := j.Name
	if label == "" {
		label = j.Key
	}
	lits := exprRe.Split(label, -1)
	quoted := make([]string, len(lits))
	literal := 0
	for i, l := range lits {
		quoted[i] = regexp.QuoteMeta(l)
		literal += len(l)
	}
	expr := strings.Join(quoted, ".*?")
	if j.Matrix && len(lits) == 1 {
		expr += `(?: \(.*\))?`
	}
	if j.Uses {
		expr += `(?: / .*)?`
	}
	return pattern{label: label, prefix: lits[0], literal: literal, re: regexp.MustCompile(`(?s)^` + expr + `$`)}
}
```

Replace the comment above `Build`

```go
// Build lays the run's jobs out per workflow job. A job goes to the workflow
// job whose name is its exact text, otherwise to the matching pattern with
// the longest literal prefix, ties to the earlier job. Workflow jobs with no
// run are dropped and their needs pass through to what they need.
```

with

```go
// Build lays the run's jobs out per workflow job. A job goes to the workflow
// job whose name is its exact text, otherwise to the matching pattern with
// the longest literal prefix; among equal prefixes the pattern with the most
// literal text in total wins, and a remaining tie goes to the earlier job.
// Workflow jobs with no run are dropped and their needs pass through to what
// they need.
```

and in `Build` replace

```go
				if p.re.MatchString(a.Name) && (best < 0 || len(p.prefix) > len(pats[best].prefix)) {
```

with

```go
				if p.re.MatchString(a.Name) && (best < 0 || p.better(pats[best])) {
```

The loop walks the patterns in file order and replaces `best` only for a strictly better one, which is what sends a remaining tie to the earlier job.

- [ ] **Step 4: Run the wfgraph suite**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/wfgraph/ -count=1`

Expected: PASS

- [ ] **Step 5: Commit the wfgraph change**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` (must print nothing), then:

```bash
git add internal/wfgraph/wfgraph.go internal/wfgraph/wfgraph_test.go
git commit -m "fix(wfgraph): refine job matching and strategy"
```

- [ ] **Step 6: Write the failing daemon test**

In `internal/daemon/rungraph_test.go`, in the table of `TestRunGraphNotes`, add a first row. Replace

```go
		{"dynamic workflow", "dynamic/dependabot/dependabot-updates", ciYAML, nil, ciJobs(), "This run has no workflow file to read."},
```

with

```go
		{"no jobs yet", ciPath, ciYAML, nil, nil, "This run has no jobs yet."},
		{"dynamic workflow", "dynamic/dependabot/dependabot-updates", ciYAML, nil, ciJobs(), "This run has no workflow file to read."},
```

and, in the body of the same test's loop, replace

```go
			if err != nil || g != nil || note != tc.note {
				t.Fatalf("graph %+v note %q err %v", g, note, err)
			}
		})
	}
}

func TestRunGraphBuildsFromTheFile(t *testing.T) {
```

with

```go
			if err != nil || g != nil || note != tc.note {
				t.Fatalf("graph %+v note %q err %v", g, note, err)
			}
			if len(tc.jobs) == 0 && gh.count("GetContent") != 0 {
				t.Fatalf("the workflow file was read for a run with no jobs: %v", gh.calls)
			}
		})
	}
}

func TestRunGraphBuildsFromTheFile(t *testing.T) {
```

The new row gives the fake a readable workflow file, so the second check fails if `runGraph` fetches it.

- [ ] **Step 7: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run TestRunGraphNotes -count=1`

Expected: FAIL in `TestRunGraphNotes/no_jobs_yet` with `graph &{Nodes:[]} note "" err <nil>`

- [ ] **Step 8: Write the daemon implementation**

In `internal/daemon/rungraph.go`, in the constants at the top of the file, replace

```go
	noteInvalid      = "ghr could not read %s."
```

with

```go
	noteInvalid      = "ghr could not read %s."
	noteNoJobs       = "This run has no jobs yet."
```

In `runGraph`, directly below the deferred function that recovers from a panic, replace

```go
	}()
	if !strings.HasPrefix(path, ".github/workflows/") {
```

with

```go
	}()
	// Without jobs there is nothing to lay out, so the file is not read.
	if len(jobs) == 0 {
		return nil, noteNoJobs, nil
	}
	if !strings.HasPrefix(path, ".github/workflows/") {
```

The check comes before the path check on purpose: a run with no jobs gets this note whatever its workflow path is. The run page needs no change. It already shows a run's note above the jobs timeline whenever the run has no graph.

- [ ] **Step 9: Run the test to verify it passes**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run TestRunGraphNotes -count=1`

Expected: PASS

- [ ] **Step 10: Correct the existing test the note changes**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -count=1`

Expected: FAIL in `TestRunGraphPanicCostsOnlyTheGraph` with `graph <nil>, note "This run has no jobs yet.", err <nil>`. The test makes the fake panic while the workflow file is read, and it calls `runGraph` with no jobs, so the file is no longer read and the panic never happens. It has to pass jobs to reach the read.

In `internal/daemon/rundetail_test.go`, in `TestRunGraphPanicCostsOnlyTheGraph`, replace

```go
	g, note, err := b.runGraph(context.Background(), "darkmem", "abc", ciPath, nil)
```

with

```go
	g, note, err := b.runGraph(context.Background(), "darkmem", "abc", ciPath, ciJobs())
```

- [ ] **Step 11: Run the package suites**

Run: `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/`
Expected: no output

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/wfgraph/ ./internal/daemon/ -count=1`
Expected: PASS for both packages

- [ ] **Step 12: Commit the daemon change**

```bash
git add internal/daemon/rungraph.go internal/daemon/rungraph_test.go internal/daemon/rundetail_test.go
git commit -m "fix(daemon): note a run that has no jobs yet"
```

### Task 12: History file: oversized lines, prune, mode, sync

**Files:**
- Modify: `internal/history/history.go`
- Test: `internal/history/history_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names. `Store.Append`, `Store.Query`, `Store.Prune`, `Store.Writes` and `Store.SetOnChange` keep their signatures.

**Items:** 19, 35
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 2 = 3

`history.jsonl` holds one JSON object per line. Today `readAll` reads it with a `bufio.Scanner` limited to 1 MiB a line, so one longer line makes every `Append`, `Query` and `Prune` fail with `bufio.Scanner: token too long`, and `Prune` cannot remove the line because it reads first. This task has two parts, each with its own test cycle and commit:

1. Steps 1 to 5: reading skips a line it cannot decode and counts it, and a prune rewrites the file only when it has something to drop.
2. Steps 6 to 10: the file is 0600 from its first append, and both writers sync.

Rules this task implements, so that none is left to judgment:

- A line whose content, without its newline, is longer than 1 MiB (1,048,576 bytes) is skipped. A line of exactly 1 MiB is read.
- `readAll` returns how many lines it skipped. The count covers both kinds of line it skips: too long, and not valid JSON.
- The store logs one line when that count differs from the previous read's count and is not zero: `history: <path>: skipped <n> line(s) longer than 1 MiB or not valid JSON`. A count that returns to zero is remembered silently, so a later bad line is logged again.
- `prune` rewrites the file when at least one entry finished before the cutoff, or when its read skipped at least one line. Otherwise it writes nothing, does not count as a write, and so does not call the change hook. A missing file is still left missing.
- `append` creates the file 0600, and sets 0600 on a file it opened that has any permission bit beyond 0600.
- `append` syncs the file before closing it. `prune` syncs the temporary file before the rename. Neither sync has a test: a test cannot observe an fsync without a seam in the production code, and the spec's test list asks for none.

- [ ] **Step 1: Write the failing tests for reading and pruning**

In `internal/history/history_test.go`, add `"bytes"` and `"log"` to the import block, which becomes:

```go
import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)
```

`TestWritesCountsOnlyRealWrites` asserts that a prune dropping nothing counts as a write, which this task changes. In that test, replace these lines:

```go
	if err := s.Prune(t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if s.Writes() != 2 || !slices.Equal(seen, []int64{1, 2}) {
		t.Fatalf("writes %d, hook saw %v", s.Writes(), seen)
	}
```

with:

```go
	before, err := os.Stat(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Prune(t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	// A rewrite renames a new file into place, so the same file means no rewrite.
	if after, err := os.Stat(s.Path); err != nil || !os.SameFile(before, after) || s.Writes() != 1 {
		t.Fatalf("a prune that drops nothing: writes %d, hook saw %v, err %v", s.Writes(), seen, err)
	}
	if err := s.Prune(t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if s.Writes() != 2 || !slices.Equal(seen, []int64{1, 2}) {
		t.Fatalf("writes %d, hook saw %v", s.Writes(), seen)
	}
```

The rest of that test stays as it is: the append after it still makes the count 3.

Append to the end of the file:

```go
// entryLine is a history line of exactly n bytes, without its newline.
func entryLine(id string, n int) string {
	head := `{"id":"` + id + `","repo":"`
	return head + strings.Repeat("x", n-len(head)-2) + `"}`
}

func TestReadAllSkipsWhatItCannotDecode(t *testing.T) {
	good := `{"id":"good","repo":"a","run_id":1}`
	for name, c := range map[string]struct {
		file    string
		ids     string
		skipped int
	}{
		"empty file":                    {"", "", 0},
		"a line of exactly 1 MiB":       {entryLine("edge", maxLine) + "\n" + good + "\n", "edge,good", 0},
		"a line one byte over":          {good + "\n" + entryLine("big", maxLine+1) + "\n", "good", 1},
		"an oversized line between two": {good + "\n" + entryLine("big", 2*maxLine) + "\n" + good + "\n", "good,good", 1},
		"an oversized last line":        {good + "\n" + entryLine("big", 2*maxLine), "good", 1},
		"a line that is not JSON":       {"garbage\n" + good + "\n", "good", 1},
		"a last line with no newline":   {good + "\n" + good, "good,good", 0},
	} {
		s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
		if err := os.WriteFile(s.Path, []byte(c.file), 0o600); err != nil {
			t.Fatal(err)
		}
		got, skipped, err := s.readAll()
		var ids []string
		for _, e := range got {
			ids = append(ids, e.ID)
		}
		if err != nil || strings.Join(ids, ",") != c.ids || skipped != c.skipped {
			t.Errorf("%s: entries %v, skipped %d, err %v; want %s and %d", name, ids, skipped, err, c.ids, c.skipped)
		}
	}
}

// One line too long to decode must not stop the store: appends and queries
// keep working, the skip is logged once, and the next prune removes the line.
func TestOversizedLineIsSkippedAndPruned(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	if err := s.Append(model.HistoryEntry{ID: "old", Repo: "a", RunID: 1, FinishedAt: t0}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(s.Path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(entryLine("big", 2*maxLine) + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(model.HistoryEntry{ID: "new", Repo: "a", RunID: 2, FinishedAt: t0.Add(time.Hour)}); err != nil {
		t.Fatalf("append beside an oversized line: %v", err)
	}
	got, err := s.Query("", "", time.Time{}, 0)
	if err != nil || len(got) != 2 || got[0].ID != "new" || got[1].ID != "old" {
		t.Fatalf("query beside an oversized line: %+v %v", got, err)
	}
	want := "history: " + s.Path + ": skipped 1 line(s) longer than 1 MiB or not valid JSON\n"
	if n := strings.Count(logs.String(), "skipped"); n != 1 || !strings.HasSuffix(logs.String(), want) {
		t.Fatalf("%d skip lines, want one ending %q:\n%s", n, want, logs.String())
	}
	writes := s.Writes()
	if err := s.Prune(t0.Add(-time.Hour)); err != nil { // a cutoff before every entry
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.Path)
	if err != nil || len(data) > 1024 || s.Writes() != writes+1 {
		t.Fatalf("after the prune: %d bytes, writes %d (was %d), err %v", len(data), s.Writes(), writes, err)
	}
	if got, err := s.Query("", "", time.Time{}, 0); err != nil || len(got) != 2 {
		t.Fatalf("the prune lost entries: %+v %v", got, err)
	}
	if n := strings.Count(logs.String(), "skipped"); n != 1 {
		t.Fatalf("%d skip lines after the prune:\n%s", n, logs.String())
	}

	// The same when the oversized line is all the file holds.
	if err := os.WriteFile(s.Path, []byte(entryLine("big", 2*maxLine)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Prune(t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(s.Path); err != nil || st.Size() != 0 || s.Writes() != writes+2 {
		t.Fatalf("a file of one oversized line: size %d, writes %d (was %d), err %v", st.Size(), s.Writes(), writes, err)
	}
}
```

Also append this test. A missing file is a read that skipped nothing, so a bad line in a file written afterwards is logged again:

```go
func TestSkippedLineIsLoggedAgainAfterTheFileWasMissing(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	for round := 1; round <= 2; round++ {
		if err := os.WriteFile(s.Path, []byte("not json\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Query("", "", time.Time{}, 0); err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(logs.String(), "skipped"); n != round {
			t.Fatalf("round %d: %d skip lines, want %d:\n%s", round, n, round, logs.String())
		}
		if err := os.Remove(s.Path); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Query("", "", time.Time{}, 0); err != nil {
			t.Fatal(err)
		}
	}
}
```

Like this step's other tests it does not run until the implementation step is in place, because the test file does not build before then. With the new `readAll` but without its `s.skipped = 0` line for a missing file, it fails with `round 2: 1 skip lines, want 2`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/history/ -count=1`
Expected: FAIL to compile with `undefined: maxLine` and `assignment mismatch: 3 variables but s.readAll returns 2 values`.

The behaviour these tests catch, once the package compiles against the old reader, is `bufio.Scanner: token too long` from `readAll`, `Append` and `Prune`, and `a prune that drops nothing: writes 2, hook saw [1 2]` from `TestWritesCountsOnlyRealWrites`.

- [ ] **Step 3: Write the implementation**

All edits are in `internal/history/history.go`.

Replace the import block with:

```go
import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/model"
)
```

Replace the `Store` type with:

```go
// maxLine is the longest line readAll decodes. An entry ghr writes is a few
// hundred bytes.
const maxLine = 1 << 20

type Store struct {
	Path     string
	mu       sync.Mutex
	writes   int64
	onChange func()
	skipped  int // lines the last read skipped; readAll logs a change
}
```

Replace the whole `readAll` method with:

```go
// readAll returns the stored entries and how many lines it skipped: a line
// longer than maxLine, or one that is not valid JSON. It logs that number
// when it differs from the previous read's and is not zero. s.mu is held.
func (s *Store) readAll() ([]model.HistoryEntry, int, error) {
	f, err := os.Open(s.Path)
	if os.IsNotExist(err) {
		s.skipped = 0
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	var out []model.HistoryEntry
	skipped := 0
	r := bufio.NewReaderSize(f, 64*1024)
	var line []byte
	for {
		// A line arrives in buffer-sized pieces; once it is too long the rest
		// of it is read and thrown away.
		line = line[:0]
		long := false
		var rerr error
		for {
			var chunk []byte
			chunk, rerr = r.ReadSlice('\n')
			if !long {
				line = append(line, chunk...)
				long = len(bytes.TrimSuffix(line, []byte{'\n'})) > maxLine
			}
			if rerr != bufio.ErrBufferFull {
				break
			}
		}
		if rerr != nil && rerr != io.EOF {
			return nil, 0, rerr
		}
		if rerr == io.EOF && len(line) == 0 {
			break
		}
		var e model.HistoryEntry
		if !long && json.Unmarshal(bytes.TrimSuffix(line, []byte{'\n'}), &e) == nil {
			out = append(out, e)
		} else {
			skipped++
		}
		if rerr == io.EOF {
			break
		}
	}
	if skipped != s.skipped {
		s.skipped = skipped
		if skipped > 0 {
			log.Printf("history: %s: skipped %d line(s) longer than 1 MiB or not valid JSON", s.Path, skipped)
		}
	}
	return out, skipped, nil
}
```

`readAll` has three callers. In `append`, replace its first line

```go
	all, err := s.readAll()
```

with

```go
	all, _, err := s.readAll()
```

In `Query`, replace

```go
	s.mu.Lock()
	all, err := s.readAll()
	s.mu.Unlock()
```

with

```go
	s.mu.Lock()
	all, _, err := s.readAll()
	s.mu.Unlock()
```

The third caller is `prune`. Replace `Prune` and `prune`, from the comment above `Prune` to the end of the file, with:

```go
// Prune drops entries that finished before cutoff and lines the read skipped.
// It replaces the file atomically, and only when there is something to drop.
func (s *Store) Prune(cutoff time.Time) error {
	s.mu.Lock()
	wrote, err := s.prune(cutoff)
	fn := s.counted(wrote)
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
	return err
}

func (s *Store) prune(cutoff time.Time) (bool, error) {
	all, skipped, err := s.readAll()
	if err != nil {
		return false, err
	}
	keep := make([]model.HistoryEntry, 0, len(all))
	for _, e := range all {
		if !e.FinishedAt.Before(cutoff) {
			keep = append(keep, e)
		}
	}
	if len(keep) == len(all) && skipped == 0 {
		return false, nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".history-*.jsonl")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	w := bufio.NewWriter(tmp)
	for _, e := range keep {
		line, _ := json.Marshal(e)
		w.Write(append(line, '\n'))
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Rename(tmp.Name(), s.Path); err != nil {
		return false, err
	}
	return true, nil
}
```

The old `prune` returned early when the read gave no entry (`if err != nil || all == nil`). That return is gone on purpose: a file holding only a line that cannot be read is now rewritten empty. A missing file and an empty file still write nothing, because nothing was skipped and nothing is older than the cutoff.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/history/ -count=1`
Expected: PASS. The output may show `history: ...: skipped 1 line(s) ...` lines from `TestAppendAfterTornTail` and `TestReadAllSkipsWhatItCannotDecode`; they are the new log line, not failures.

- [ ] **Step 5: Commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
git add internal/history/history.go internal/history/history_test.go
git commit -m "fix(history): skip oversized lines, prune on need"
```

`gofmt -l` must print nothing.

- [ ] **Step 6: Write the failing test for the file mode**

Append to `internal/history/history_test.go`:

```go
// A prune leaves the file 0600 because it replaces it, but a prune with
// nothing to drop writes nothing, so an append sets the mode itself.
func TestAppendLeavesTheFile0600(t *testing.T) {
	for name, before := range map[string]func(path string) error{
		"a file it creates": func(string) error { return nil },
		"a file it finds 0644": func(path string) error {
			if err := os.WriteFile(path, nil, 0o644); err != nil {
				return err
			}
			return os.Chmod(path, 0o644)
		},
	} {
		s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
		if err := before(s.Path); err != nil {
			t.Fatal(err)
		}
		if err := s.Append(model.HistoryEntry{ID: "a", Repo: "a", RunID: 1}); err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(s.Path)
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v, err %v; want -rw-------", name, st.Mode().Perm(), err)
		}
	}
}
```

- [ ] **Step 7: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/history/ -run TestAppendLeavesTheFile0600 -count=1`
Expected: FAIL with `a file it creates: mode -rw-r--r--` and `a file it finds 0644: mode -rw-r--r--`

- [ ] **Step 8: Write the implementation**

In `internal/history/history.go`, replace the whole `append` method with:

```go
func (s *Store) append(e model.HistoryEntry) (bool, error) {
	all, _, err := s.readAll()
	if err != nil {
		return false, err
	}
	for _, x := range all {
		if x.ID == e.ID && x.RunID == e.RunID {
			return false, nil
		}
	}
	line, err := json.Marshal(e)
	if err != nil {
		return false, err
	}
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o600)
	if err != nil {
		return false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return false, err
	}
	if st.Mode().Perm()&^0o600 != 0 {
		if err := f.Chmod(0o600); err != nil {
			return false, err
		}
	}
	// A crash mid-write can leave a fragment with no trailing newline; start a
	// new line so this record is not merged into the fragment and lost.
	if st.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, st.Size()-1); err != nil {
			return false, err
		}
		if last[0] != '\n' {
			line = append([]byte{'\n'}, line...)
		}
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return false, err
	}
	if err := f.Sync(); err != nil {
		return true, err
	}
	return true, f.Close()
}
```

A failed sync returns `true` with the error, like the failed close on the last line already does: the line is in the file, so the write is counted and readers are told.

In `prune`, replace

```go
	if err := w.Flush(); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
```

with

```go
	if err := w.Flush(); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
```

- [ ] **Step 9: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test -race ./internal/history/ -count=1`
Expected: PASS

The store's callers are in two other packages: `internal/runner/cleanup.go` calls `History.Prune` and reads only its error, and `internal/daemon/changes.go` counts `Hist.Writes` for the `history` change topic. Neither relied on a prune always rewriting. Confirm it:

Run: `PATH=/usr/local/go/bin:$PATH timeout 590 go test ./internal/runner/ ./internal/daemon/ -count=1`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
git add internal/history/history.go internal/history/history_test.go
git commit -m "fix(history): create 0600 and sync writes"
```

`gofmt -l` must print nothing.

### Task 13: Metrics after a clock step

**Files:**
- Modify: `internal/metrics/metrics.go` (`Load`, `Sample`, `addToHour`)
- Test: `internal/metrics/metrics_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names

**Items:** 42
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The sampler keeps a ring of one-minute samples and a ring of hourly rollups, plus the hour in progress (`open`). Two things go wrong when the system clock is set back:

- `Load` keeps what is dated after now. A minute sample or a rollup from "the future" then sits in the rings until real time passes it.
- `addToHour` leaves out every sample from an hour before the open one. After a step back of more than an hour, nothing is counted until the clock reaches the open hour again.

Rules this task implements:

- `Load` drops a minute sample and a rollup whose time is after now. A saved open hour that is after the current hour is already dropped today, and stays dropped; nothing changes for it.
- `addToHour`, given a sample from an hour before the open one, drops the open hour and every rollup that is not before the sample's hour, then opens the sample's hour and counts the sample. Those hours will be lived again, so what was counted for them goes.
- `Sample` drops every kept minute sample dated after the new one before it appends the new one. This applies to any step back, also one inside the open hour; in that case the open hour keeps counting as it does today.

`TestClockJumps` asserts the old behaviour (the sample after the step is left out) and is rewritten in Step 1.

- [ ] **Step 1: Write the failing tests**

In `internal/metrics/metrics_test.go`, replace the whole `TestClockJumps` function with:

```go
func TestClockJumps(t *testing.T) {
	h := newHost(t)
	h.write(t, "cpu.stat", "usage_usec 0\n")
	h.s.Sample()                     // 12:00
	h.now = h.now.Add(3 * time.Hour) // 15:00: 12:00 closes, 13:00 and 14:00 stay absent
	h.s.Sample()
	h.now = h.now.Add(time.Hour) // 16:00: 15:00 closes
	h.s.Sample()
	if hrs := h.s.Hours(); len(hrs) != 3 || hrs[1].At.Hour() != 15 || hrs[2].At.Hour() != 16 {
		t.Fatalf("hours before the step %+v", hrs)
	}
	h.now = h.now.Add(-3 * time.Hour) // 13:00: a backwards clock step
	h.s.Sample()
	h.now = h.now.Add(time.Minute) // 13:01
	h.s.Sample()
	// 15:00 and 16:00 will be lived again, so what was counted for them goes.
	hrs := h.s.Hours()
	if len(hrs) != 2 || hrs[0].At.Hour() != 12 || hrs[1].At.Hour() != 13 || hrs[1].Samples != 2 {
		t.Fatalf("hours after the step %+v", hrs)
	}
	mins := h.s.Minutes()
	if len(mins) != 3 || mins[0].At.Hour() != 12 || mins[1].At.Hour() != 13 || mins[2].At.Minute() != 1 {
		t.Fatalf("minutes after the step %+v", mins)
	}
	h.now = h.now.Add(-30 * time.Second) // 13:00:30: a step back inside the open hour
	h.s.Sample()
	mins = h.s.Minutes()
	if len(mins) != 3 || mins[2].At.Second() != 30 {
		t.Fatalf("minutes after a step inside the hour %+v", mins)
	}
	if hrs := h.s.Hours(); len(hrs) != 2 || hrs[1].Samples != 3 {
		t.Fatalf("hours after a step inside the hour %+v", hrs)
	}
}
```

Add this test directly above `TestLoadMissingAndMalformed`:

```go
// A clock set back before a restart leaves the file ahead of the clock. What
// is dated after now would otherwise sit in the rings until real time passed it.
func TestLoadDropsWhatIsDatedAfterNow(t *testing.T) {
	h := newHost(t)
	h.write(t, "cpu.stat", "usage_usec 0\n")
	h.s.Path = filepath.Join(h.dir, "metrics.json")
	noon := h.now
	// 12:00, 12:01, 14:00, 14:01 and 15:00
	for _, after := range []time.Duration{0, time.Minute, 2 * time.Hour, 2*time.Hour + time.Minute, 3 * time.Hour} {
		h.now = noon.Add(after)
		h.s.Sample()
	}
	if hrs := h.s.Hours(); len(hrs) != 3 { // 12:00 and 14:00 closed, 15:00 open
		t.Fatalf("saved hours %+v", hrs)
	}
	if err := h.s.Save(); err != nil {
		t.Fatal(err)
	}
	h.now = noon.Add(90 * time.Minute) // 13:30
	back := NewSampler(h.s.Counts, h.s.Disk)
	back.Now = func() time.Time { return h.now }
	back.Path = h.s.Path
	if err := back.Load(); err != nil {
		t.Fatal(err)
	}
	mins := back.Minutes()
	if len(mins) != 2 || mins[1].At.Hour() != 12 || mins[1].At.Minute() != 1 {
		t.Fatalf("minutes %+v", mins)
	}
	hrs := back.Hours()
	if len(hrs) != 1 || hrs[0].At.Hour() != 12 || hrs[0].Samples != 2 {
		t.Fatalf("hours %+v", hrs)
	}
}
```

Both tests use the package's `newHost`, whose clock is the `h.now` field and starts at 2026-10-05 12:00 UTC.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/metrics/ -run 'TestClockJumps|TestLoadDropsWhatIsDatedAfterNow' -count=1`
Expected: FAIL. `TestClockJumps` fails with `hours after the step` listing three hours (12:00, 15:00 and 16:00). `TestLoadDropsWhatIsDatedAfterNow` fails with `minutes` listing five samples.

- [ ] **Step 3: Write the implementation**

All edits are in `internal/metrics/metrics.go`.

Add `"slices"` to the import block, between `"runtime"` and `"strconv"`:

```go
	"runtime"
	"slices"
	"strconv"
```

Replace the comment above `Load`:

```go
// Load restores what Save wrote, dropping minutes older than three hours and
// rollups older than 30 days. A saved open hour resumes when it is the
// current UTC hour and is closed as it stands otherwise. A missing file is an
// empty start; a malformed one returns an error and changes nothing.
```

with:

```go
// Load restores what Save wrote, dropping minutes older than three hours,
// rollups older than 30 days, and minutes and rollups dated after now (the
// clock was set back). A saved open hour resumes when it is the current UTC
// hour, is closed as it stands when it is an earlier one, and is dropped when
// it is a later one. A missing file is an empty start; a malformed one
// returns an error and changes nothing.
```

Inside `Load`, replace:

```go
	for _, m := range st.Minutes {
		if now.Sub(m.At) <= keepMinutes*time.Minute {
			s.samples = append(s.samples, m)
		}
	}
```

with:

```go
	for _, m := range st.Minutes {
		if !m.At.After(now) && now.Sub(m.At) <= keepMinutes*time.Minute {
			s.samples = append(s.samples, m)
		}
	}
```

and replace:

```go
	for _, r := range st.Hours {
		if now.Sub(r.At) <= keepHours*time.Hour {
			s.hours = append(s.hours, r)
		}
	}
```

with:

```go
	for _, r := range st.Hours {
		if !r.At.After(now) && now.Sub(r.At) <= keepHours*time.Hour {
			s.hours = append(s.hours, r)
		}
	}
```

The `switch` on the saved open hour further down in `Load` is not changed.

Inside `Sample`, replace:

```go
	s.samples = append(s.samples, smp)
	if len(s.samples) > keepMinutes {
```

with:

```go
	// A sample dated after this one is from before a backwards clock step.
	s.samples = slices.DeleteFunc(s.samples, func(m model.MetricSample) bool { return m.At.After(now) })
	s.samples = append(s.samples, smp)
	if len(s.samples) > keepMinutes {
```

Replace the whole `addToHour` method, including its comment, with:

```go
// addToHour folds smp into the open UTC hour, closing it when smp belongs to
// a later one. Rollups are keyed on UTC so every one spans 60 minutes. A
// sample from before the open hour means the clock stepped back: the open
// hour and every rollup that is not before the sample's hour are dropped,
// because those hours will be lived again, and the sample's hour is opened.
// The caller holds s.mu.
func (s *Sampler) addToHour(smp model.MetricSample) {
	hour := smp.At.UTC().Truncate(time.Hour)
	switch {
	case s.open != nil && hour.Before(s.open.At):
		s.open = nil
		s.hours = slices.DeleteFunc(s.hours, func(r model.MetricRollup) bool { return !r.At.Before(hour) })
	case s.open != nil && hour.After(s.open.At):
		s.closeHour()
	}
	if s.open == nil {
		s.open = &openHour{At: hour}
	}
	s.open.add(smp)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test -race ./internal/metrics/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
git add internal/metrics/metrics.go internal/metrics/metrics_test.go
git commit -m "fix(metrics): keep counting after a clock step back"
```

`gofmt -l` must print nothing.

### Task 14: Runner cap: config

**Files:**
- Modify: `internal/config/config.go` (new constant, `Parse`, new `clampRunners`, `Validate`)
- Modify: `deploy/config.example.yaml` (line 3, the `global_max` line)
- Test: `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `config.MaxRunners = 99` (Contracts)

**Items:** 21
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

`global_max`, a repository's `max` and a repository's `warm` have no upper bound today. The Activity page pads its lanes up to `global_max` and the web UI draws one element per slot, so one large number in `config.yaml` makes both very large. The cap is 99, the limit the web UI's forms already apply.

Two paths reach the config, and they are treated differently on purpose:

- **Loading `config.yaml`** (`Parse`, at start and on reload): a value above 99 is lowered to 99 and a warning is returned. The file is not rewritten by loading; the next save writes 99. The daemon already writes every warning `Parse` returns as a `config: <warning>` event, so nothing outside this package changes.
- **An API change** (PATCH `/config`, add repository): `Validate` refuses a value above 99. Those paths go through `Store.Update` in `internal/daemon/store.go`, which calls `Validate` on a copy of the already clamped config, and `Backend.update` turns the error into a 400. Nothing in `internal/daemon` or `cmd/ghr` changes: `ghr set global-max`, `ghr set max`, `ghr set warm` and `ghr repo add --max` send the number and print the daemon's refusal.

`Parse` clamps before it validates, so no file can load into a config that cannot be saved.

Exact texts:

| Where | Text |
|---|---|
| `Parse` warning, `global_max` | `global_max 500 is above 99; using 99` |
| `Parse` warning, a repository's `max` | `<repo>: max 120 is above 99; using 99` |
| `Parse` warning, a repository's `warm` | `<repo>: warm 100 is above 99; using 99` |
| `Validate` error, `global_max` | `global_max must be <= 99` |
| `Validate` error, a repository's `max` | `<repo>: max must be <= 99` |
| `Validate` error, a repository's `warm` | `<repo>: warm must be <= 99` |

The existing errors `global_max must be >= 1`, `<repo>: max must be >= 0` and `<repo>: warm must be >= 0` stay as they are.

- [ ] **Step 1: Write the failing tests**

In `internal/config/config_test.go`, in `TestValidateErrors`, replace these lines:

```go
	three := 3
	cases := map[string]func(c *Config){
		"mode must be":                 func(c *Config) { c.Mode = "fast" },
		"global_max must be >= 1":      func(c *Config) { c.GlobalMax = 0 },
		"duplicate repo darkmem":       func(c *Config) { c.Repos = append(c.Repos, Repo{Name: "darkmem"}) },
		"darkmem: max must be >= 0":    func(c *Config) { c.Repos[1].Max = &neg },
```

with:

```go
	three := 3
	over := MaxRunners + 1
	cases := map[string]func(c *Config){
		"mode must be":                 func(c *Config) { c.Mode = "fast" },
		"global_max must be >= 1":      func(c *Config) { c.GlobalMax = 0 },
		"global_max must be <= 99":     func(c *Config) { c.GlobalMax = over },
		"duplicate repo darkmem":       func(c *Config) { c.Repos = append(c.Repos, Repo{Name: "darkmem"}) },
		"darkmem: max must be >= 0":    func(c *Config) { c.Repos[1].Max = &neg },
		"darkmem: max must be <= 99":   func(c *Config) { c.Repos[1].Max = &over },
		"darkmem: warm must be <= 99":  func(c *Config) { c.Repos[1].Warm = &over },
```

Add this test directly above the comment `// The floors themselves are valid; only values below them are rejected.`:

```go
// A config.yaml written by hand above the cap is lowered on load, with one
// warning for each value, and what was loaded saves and loads again.
func TestParseClampsRunnerCounts(t *testing.T) {
	c, warnings, err := Parse([]byte(`
owner: darkraise
mode: all
global_max: 500
labels: [homelab]
repos:
  - name: darkcloud
    max: 120
    warm: 100
  - name: darkmem
    max: 99
    warm: 0
`))
	if err != nil {
		t.Fatal(err)
	}
	want := "global_max 500 is above 99; using 99|darkcloud: max 120 is above 99; using 99|darkcloud: warm 100 is above 99; using 99"
	if got := strings.Join(warnings, "|"); got != want {
		t.Fatalf("warnings\n got %s\nwant %s", got, want)
	}
	cloud, mem := c.Repo("darkcloud"), c.Repo("darkmem")
	if c.GlobalMax != MaxRunners || *cloud.Max != MaxRunners || *cloud.Warm != MaxRunners || *mem.Max != 99 || *mem.Warm != 0 {
		t.Fatalf("global_max %d, darkcloud %d/%d, darkmem %d/%d", c.GlobalMax, *cloud.Max, *cloud.Warm, *mem.Max, *mem.Warm)
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatalf("saving a clamped config: %v", err)
	}
	back, warnings, err := Load(path)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("loading what was saved: warnings %v, err %v", warnings, err)
	}
	if back.GlobalMax != MaxRunners || *back.Repo("darkcloud").Max != MaxRunners || *back.Repo("darkcloud").Warm != MaxRunners {
		t.Fatalf("saved file: %+v", back)
	}
}
```

In that test `darkcloud` has `max: 120` and `warm: 100`. Both become 99, so the existing rule `warm must be <= max` holds only because the clamp runs before the validation.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/config/ -count=1`
Expected: FAIL to compile with `undefined: MaxRunners`. To see the behaviour fail first, add the `MaxRunners` constant from Step 3 alone and run again: `TestValidateErrors` then fails three times with `want error containing "global_max must be <= 99", got <nil>` (and the same for `darkmem: max` and `darkmem: warm`), and `TestParseClampsRunnerCounts` fails with an empty `got` under `warnings`.

- [ ] **Step 3: Write the implementation**

All edits are in `internal/config/config.go`. Each is a short group of lines, because a later task also edits this file.

Insert directly above the comment `// SystemLabels are the read-only labels GitHub gives every Linux x64 runner.`:

```go
// MaxRunners is the most runners global_max, a repository's max or a
// repository's warm may ask for. Parse lowers a larger value in config.yaml;
// Validate refuses one, which is what an API change gets.
const MaxRunners = 99

```

In `Parse`, replace its last lines:

```go
	c.Owner = strings.TrimSpace(c.Owner)
	warnings, err := c.Validate()
	if err != nil {
		return nil, nil, err
	}
	return c, warnings, nil
}
```

with:

```go
	c.Owner = strings.TrimSpace(c.Owner)
	clamped := c.clampRunners()
	warnings, err := c.Validate()
	if err != nil {
		return nil, nil, err
	}
	return c, append(clamped, warnings...), nil
}

// clampRunners lowers global_max and every repository's max and warm to
// MaxRunners, and returns one warning for each value it lowered.
func (c *Config) clampRunners() []string {
	var warnings []string
	clamp := func(what string, v *int) {
		if *v > MaxRunners {
			warnings = append(warnings, fmt.Sprintf("%s %d is above %d; using %d", what, *v, MaxRunners, MaxRunners))
			*v = MaxRunners
		}
	}
	clamp("global_max", &c.GlobalMax)
	for i := range c.Repos {
		r := &c.Repos[i]
		if r.Max != nil {
			clamp(r.Name+": max", r.Max)
		}
		if r.Warm != nil {
			clamp(r.Name+": warm", r.Warm)
		}
	}
	return warnings
}
```

In `Validate`, replace:

```go
	if c.GlobalMax < 1 {
		errs = append(errs, "global_max must be >= 1")
	}
```

with:

```go
	if c.GlobalMax < 1 {
		errs = append(errs, "global_max must be >= 1")
	}
	if c.GlobalMax > MaxRunners {
		errs = append(errs, fmt.Sprintf("global_max must be <= %d", MaxRunners))
	}
```

and, inside the loop over `c.Repos`, replace:

```go
		if r.Warm != nil && *r.Warm < 0 {
			errs = append(errs, r.Name+": warm must be >= 0")
		}
```

with:

```go
		if r.Warm != nil && *r.Warm < 0 {
			errs = append(errs, r.Name+": warm must be >= 0")
		}
		if r.Max != nil && *r.Max > MaxRunners {
			errs = append(errs, fmt.Sprintf("%s: max must be <= %d", r.Name, MaxRunners))
		}
		if r.Warm != nil && *r.Warm > MaxRunners {
			errs = append(errs, fmt.Sprintf("%s: warm must be <= %d", r.Name, MaxRunners))
		}
```

`Save` and `Clone` are not changed. `Save` calls `Validate`, so it refuses a value above 99; `Clone` copies through YAML without `Parse`, so it never clamps.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/config/ -count=1`
Expected: PASS

- [ ] **Step 5: Name the cap in the example config**

In `deploy/config.example.yaml` replace line 3:

```yaml
global_max: 2               # queue mode only
```

with:

```yaml
global_max: 2               # queue mode only; at most 99, like a repo's max and warm
```

Only the comment changes.

- [ ] **Step 6: Run the packages that load and patch the config**

Run: `PATH=/usr/local/go/bin:$PATH timeout 590 go test ./internal/daemon/ ./internal/api/ ./cmd/ghr/ ./internal/sched/ ./internal/activity/ -count=1`
Expected: PASS. No test in them uses a value above 99.

- [ ] **Step 7: Commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
git add internal/config/config.go internal/config/config_test.go deploy/config.example.yaml
git commit -m "fix(config): cap runner counts at 99"
```

`gofmt -l` must print nothing.

### Task 15: Runner cap: the settings form

**Files:**
- Modify: `web/src/lib/settings-form.ts` (`fieldErrors`, one new constant)
- Test: `web/src/pages/settings.test.tsx`

**Interfaces:**
- Consumes: the value of `config.MaxRunners = 99` (Contracts). The daemon refuses a `global_max` above it with a 400; this form says so before sending.
- Produces: nothing a later task names

**Items:** 21
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

The settings form checks `global_max` before it sends a PATCH. Today the check is "a whole number, at least 1". It becomes 1 to 99. The same form is used by the Settings page and by the last step of the setup wizard, so both get the bound.

The form's messages mirror the daemon's. The new one is `global_max must be <= 99`; the existing `global_max must be >= 1` stays for an empty box, a fraction and a number below 1.

The number box already carries `max={99}` in `web/src/components/settings-form.tsx`, which stops the spinner at 99 but not a typed 100. The other places the web UI takes a runner count already hold it at 99 and are not touched: the Add repository dialog (`clampMax` in `web/src/components/add-repo-dialog.tsx`) and the Max and Warm boxes of the repository page (`setNumber` in `web/src/pages/repository.tsx`).

- [ ] **Step 1: Write the failing test**

In `web/src/pages/settings.test.tsx`, in the test `checks the fields before sending`, replace these two lines:

```tsx
    await user.clear(screen.getByLabelText("Global max"))
    expect(screen.getByText("global_max must be >= 1")).toBeInTheDocument()
```

with:

```tsx
    const globalMax = screen.getByLabelText("Global max")
    await user.clear(globalMax)
    await user.type(globalMax, "100")
    expect(screen.getByText("global_max must be <= 99")).toBeInTheDocument()
    await user.clear(globalMax)
    await user.type(globalMax, "99")
    expect(screen.queryByText(/^global_max must be/)).toBeNull()
    await user.clear(globalMax)
    expect(screen.getByText("global_max must be >= 1")).toBeInTheDocument()
```

The rest of that test stays: with the box left empty, Save is still refused and no PATCH is sent.

- [ ] **Step 2: Run the test to verify it fails**

Run, in `web/`: `timeout 300 npx vitest run src/pages/settings.test.tsx`
Expected: FAIL, one test, with `Unable to find an element with the text: global_max must be <= 99`

- [ ] **Step 3: Write the implementation**

In `web/src/lib/settings-form.ts`, insert directly below the `const FLOORS` line:

```ts

// The daemon's cap on global_max (config.MaxRunners); it refuses a larger value.
const MAX_RUNNERS = 99
```

In `fieldErrors`, replace:

```ts
  if (!Number.isInteger(max) || max < 1) errors.global_max = "global_max must be >= 1"
```

with:

```ts
  if (!Number.isInteger(max) || max < 1) errors.global_max = "global_max must be >= 1"
  else if (max > MAX_RUNNERS) errors.global_max = `global_max must be <= ${MAX_RUNNERS}`
```

- [ ] **Step 4: Run the tests to verify they pass**

Run, in `web/`: `timeout 300 npx vitest run src/pages/settings.test.tsx src/pages/setup.test.tsx`
Expected: PASS, both files. `setup.test.tsx` is included because the setup wizard uses the same form.

- [ ] **Step 5: Lint and type-check**

Run, in `web/`: `timeout 300 npm run lint` and then `timeout 300 npm run typecheck`
Expected: both end without an error or a warning

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/settings-form.ts web/src/pages/settings.test.tsx
git commit -m "fix(web): bound global max at 99 in settings"
```

### Task 16: Adding a repository

**Files:**
- Modify: `internal/config/config.go` (the `Repo` struct only)
- Modify: `deploy/config.example.yaml` (its last line, `repos: []`)
- Modify: `internal/daemon/backend.go` (the `AddRepo` function only)
- Test: `internal/config/config_test.go`, `internal/daemon/backend_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `config.Repo.AllowPublic` (Contracts)

**Items:** 37, 32
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

Row 32 is only started here: this task adds the config key and stores it on add. Reading visibility and acting on it are the next two tasks.

What changes for a user:

- `config.yaml` accepts `allow_public: true` on a repository. Today that key is refused as unknown.
- `ghr repo add` and the web's Add dialog store the repository under the name GitHub reports (the part of `full_name` after the slash), not under what was typed. GitHub answers for any spelling of a name, and answers for a renamed repository under its new name.
- Two adds of the same repository at the same moment: one succeeds, the other gets 409. Today the second gets 400.
- A repository GitHub now reports under another owner (it was transferred) is refused with 400.

- [ ] **Step 1: Write the failing config test**

Append to `internal/config/config_test.go`:

```go
// allow_public is the only thing that lets ghr serve a public repository, so
// it must survive a load, a clone and a save, and stay out of the file and the
// API's JSON when it is off.
func TestAllowPublicRoundTrips(t *testing.T) {
	c, _, err := Parse([]byte(sample + "    allow_public: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Repo("darkmem").AllowPublic || c.Repo("darkcloud").AllowPublic {
		t.Fatalf("parsed %+v", c.Repos)
	}
	if !c.Clone().Repo("darkmem").AllowPublic {
		t.Fatal("clone lost allow_public")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	back, _, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Repo("darkmem").AllowPublic || back.Repo("darkcloud").AllowPublic {
		t.Fatalf("saved %+v", back.Repos)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "allow_public: true"); n != 1 || strings.Count(string(raw), "allow_public") != 1 {
		t.Fatalf("allow_public must be written once, for darkmem only:\n%s", raw)
	}
	data, err := json.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), `"allow_public":true`) != 1 || strings.Count(string(data), "allow_public") != 1 {
		t.Fatalf("JSON %s", data)
	}
}
```

The test file's `sample` constant ends with the repository `darkmem`, so the appended line gives that repository the key.

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/config/ -run TestAllowPublicRoundTrips -count=1`
Expected: the build fails with `c.Repo("darkmem").AllowPublic undefined (type *Repo has no field or method AllowPublic)`

- [ ] **Step 3: Add the field and document the key**

In `internal/config/config.go`, in the `Repo` struct, replace this line:

```go
	Paused              bool     `yaml:"paused,omitempty" json:"paused,omitempty"`
```

with:

```go
	Paused              bool     `yaml:"paused,omitempty" json:"paused,omitempty"`
	// AllowPublic lets ghr serve the repository while GitHub reports it
	// public. A job in a public repository can come from anyone's pull
	// request and runs on this host.
	AllowPublic bool `yaml:"allow_public,omitempty" json:"allow_public,omitempty"`
```

`Config.Clone` copies through YAML and `Save` writes YAML, so the tag is all both need; change neither.

In `deploy/config.example.yaml` replace the last line:

```yaml
repos: []
```

with:

```yaml
repos: []
# A repository entry, as `ghr repo add` and the web UI write it:
#   - name: my-repo
#     allow_public: true      # required to serve a public repository; without it ghr starts no runner for one
```

- [ ] **Step 4: Run the config tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/config/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit the config key**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
git add internal/config/config.go internal/config/config_test.go deploy/config.example.yaml
git commit -m "feat(config): add allow_public per repository"
```

- [ ] **Step 6: Write the failing daemon tests**

All four edits are in `internal/daemon/backend_test.go`.

(a) The test fake must be able to hold two `GetRepo` calls at once, and it must answer with a `full_name` as GitHub does. In the `fakeGH` struct replace these eight lines:

```go
	repos    map[string]*github.Repository
	forgot   bool
	meta     github.TokenMeta
	runners  map[int64]github.Runner
	getErr   error
	delErr   error
	deleted  []int64
	getCalls int // GetRepo calls
```

with:

```go
	repos     map[string]*github.Repository
	forgot    bool
	meta      github.TokenMeta
	runners   map[int64]github.Runner
	getErr    error
	delErr    error
	deleted   []int64
	getCalls  int    // GetRepo calls; guarded by lmu
	onGetRepo func() // when set, GetRepo calls it after counting the call
```

(b) Replace the fake's `GetRepo` method with:

```go
func (f *fakeGH) GetRepo(ctx context.Context, repo string) (*github.Repository, error) {
	f.lmu.Lock()
	f.getCalls++
	f.lmu.Unlock()
	if f.onGetRepo != nil {
		f.onGetRepo()
	}
	r, ok := f.repos[repo]
	if !ok {
		return nil, &github.APIError{Status: 404, Kind: github.ErrNotFound}
	}
	return r, nil
}
```

(c) In `newBackend` replace these three lines:

```go
	gh := &fakeGH{repos: map[string]*github.Repository{
		"newrepo": {Private: true}, "public": {Private: false}, "darkcloud": {Private: true},
	}}
```

with:

```go
	gh := &fakeGH{repos: map[string]*github.Repository{
		"newrepo":   {FullName: "darkraise/newrepo", Private: true},
		"public":    {FullName: "darkraise/public"},
		"darkcloud": {FullName: "darkraise/darkcloud", Private: true},
	}}
```

This is a correction of an existing fixture, not of an expectation: the fake answered without a `full_name`, which GitHub never does, and after Step 8 `AddRepo` reads it. `TestAddRepo`, `TestAddRepoTakesOverAWatchedRepo` and the label-check test that re-adds `darkcloud` depend on it and need no other change.

(d) Add these two tests directly after `TestAddRepo`:

```go
// GitHub answers for any spelling of a name, and for a renamed repository
// under its new name. The config must hold GitHub's spelling, or one
// repository could be configured twice under two names.
func TestAddRepoStoresGitHubsName(t *testing.T) {
	b, _, gh := newBackend(t)
	ctx := context.Background()
	gh.repos["NewRepo"] = &github.Repository{FullName: "darkraise/newrepo", Private: true}
	gh.repos["oldcloud"] = &github.Repository{FullName: "darkraise/darkcloud", Private: true}
	gh.repos["fresh"] = &github.Repository{FullName: "darkraise/fresh", Private: true}
	gh.repos["stale"] = &github.Repository{FullName: "darkraise/fresh", Private: true}
	gh.repos["moved"] = &github.Repository{FullName: "someone-else/moved", Private: true}
	gh.repos["Public"] = &github.Repository{FullName: "DarkRaise/public"}

	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "NewRepo"}); err != nil {
		t.Fatal(err)
	}
	if r := b.Store.Config().Repo("newrepo"); r == nil || r.Name != "newrepo" || r.AllowPublic {
		t.Fatalf("stored %+v, want GitHub's spelling and no allow_public", r)
	}
	ev := b.Events.After(0)
	if last := ev[len(ev)-1]; last.Repo != "newrepo" || last.Msg != "repo added" {
		t.Fatalf("event %+v", last)
	}

	// darkcloud is configured; its old name passes the early check, which
	// knows only what was typed.
	err := b.AddRepo(ctx, model.AddRepoRequest{Name: "oldcloud"})
	if apiStatus(err) != 409 || err.Error() != "repo darkcloud is already configured" {
		t.Fatalf("renamed duplicate: %v", err)
	}

	// A watched repository added under its old name leaves watch_repos under
	// the name it is stored with.
	if err := b.WatchRepo(ctx, "fresh"); err != nil {
		t.Fatal(err)
	}
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "stale"}); err != nil {
		t.Fatal(err)
	}
	if c := b.Store.Config(); c.Repo("fresh") == nil || c.Repo("stale") != nil || len(c.WatchRepos) != 0 {
		t.Fatalf("repos %+v watch_repos %v", c.Repos, c.WatchRepos)
	}

	err = b.AddRepo(ctx, model.AddRepoRequest{Name: "moved"})
	if apiStatus(err) != 400 || err.Error() != "moved now lives at someone-else/moved; ghr serves only repositories of darkraise" {
		t.Fatalf("transferred: %v", err)
	}
	if b.Store.Config().Repo("moved") != nil {
		t.Fatal("a transferred repository was stored")
	}

	// The owner is compared ignoring case, and the request's allow_public is kept.
	if err := b.AddRepo(ctx, model.AddRepoRequest{Name: "Public", AllowPublic: true}); err != nil {
		t.Fatal(err)
	}
	saved, _, err := config.Load(b.Store.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if r := saved.Repo("public"); r == nil || r.Name != "public" || !r.AllowPublic {
		t.Fatalf("saved %+v, want public with allow_public", r)
	}
}

// Two adds of one repository can both pass the early check. The check inside
// the update decides, and the loser gets the 409 a late duplicate gets.
func TestConcurrentAddRepoConflicts(t *testing.T) {
	b, _, gh := newBackend(t)
	entered, release := make(chan struct{}), make(chan struct{})
	gh.onGetRepo = func() {
		entered <- struct{}{}
		<-release
	}
	errs := make(chan error, 2)
	for range 2 {
		go func() { errs <- b.AddRepo(context.Background(), model.AddRepoRequest{Name: "newrepo"}) }()
	}
	<-entered
	<-entered // both are past the early check
	close(release)
	first, second := <-errs, <-errs
	if (first == nil) == (second == nil) || (apiStatus(first) != 409 && apiStatus(second) != 409) {
		t.Fatalf("errors %v and %v, want one success and one 409", first, second)
	}
	n := 0
	for _, r := range b.Store.Config().Repos {
		if r.Name == "newrepo" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("newrepo configured %d times", n)
	}
}
```

The test store's owner is `darkraise` (the `cfgYAML` constant in `store_test.go`), and it already holds `darkcloud` and `darkmem`.

- [ ] **Step 7: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run 'TestAddRepoStoresGitHubsName|TestConcurrentAddRepoConflicts' -count=1`
Expected: both FAIL.
- `TestAddRepoStoresGitHubsName`: `stored &{Name:NewRepo ...}, want GitHub's spelling and no allow_public`
- `TestConcurrentAddRepoConflicts`: `errors <nil> and duplicate repo newrepo (names are case-insensitive), want one success and one 409`

- [ ] **Step 8: Write the implementation**

In `internal/daemon/backend.go` replace the whole `AddRepo` function with:

```go
// AddRepo adds a runner repository under the name GitHub reports for it,
// which is its new name when the typed one was redirected after a rename.
func (b *Backend) AddRepo(ctx context.Context, req model.AddRepoRequest) error {
	typed := strings.TrimSpace(req.Name)
	if typed == "" {
		return api.BadRequest("repo name is required")
	}
	cfg := b.Store.Config()
	// A fast path that saves the GitHub call. It knows only the typed name;
	// the check inside the update decides.
	if cfg.Repo(typed) != nil {
		return api.Conflict("repo " + typed + " is already configured")
	}
	repo, err := b.GH.GetRepo(ctx, typed)
	if github.IsKind(err, github.ErrNotFound) {
		return api.BadRequest(fmt.Sprintf("token cannot see %s/%s: add it to the PAT's repository access first", cfg.Owner, typed))
	}
	if err != nil {
		return err
	}
	owner, name, _ := strings.Cut(repo.FullName, "/")
	// GitHub also redirects a repository that was transferred to another
	// owner, and every later call would go to the configured owner.
	if !strings.EqualFold(owner, cfg.Owner) {
		return api.BadRequest(fmt.Sprintf("%s now lives at %s; ghr serves only repositories of %s", typed, repo.FullName, cfg.Owner))
	}
	if !repo.Private && !req.AllowPublic {
		return api.Conflict(name + " is public; self-hosted runners must only serve private repos (pass --allow-public to override)")
	}
	if err := b.update(func(c *config.Config) error {
		if c.Repo(name) != nil {
			return api.Conflict("repo " + name + " is already configured")
		}
		c.WatchRepos = slices.DeleteFunc(c.WatchRepos, func(w string) bool { return strings.EqualFold(w, name) })
		c.Repos = append(c.Repos, config.Repo{Name: name, Max: req.Max, Labels: req.Labels, AllowPublic: req.AllowPublic})
		return nil
	}); err != nil {
		return err
	}
	b.forgetLabelCheck(name)
	b.Events.Add("info", name, "repo added")
	return nil
}
```

Points a reviewer will check:

- After the GitHub call, every use is of `name` (GitHub's spelling): the check inside the update, the `watch_repos` removal, the stored entry, `forgetLabelCheck` and the event. Only the early check and the two messages about what the user typed use `typed`.
- `b.update` passes an `*api.Error` through unchanged, so the conflict raised inside the update reaches the caller as 409.
- `allow_public` is stored as the request gave it, also for a private repository.
- The transfer message is this task's wording; the spec fixes only the status (400) and that it names where the repository now lives.

- [ ] **Step 9: Run the daemon tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 600 go test -race ./internal/daemon/ -count=1`
Expected: PASS. `-race` matters here: the new test runs two adds at once.

- [ ] **Step 10: Commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
git add internal/daemon/backend.go internal/daemon/backend_test.go
git commit -m "fix(daemon): add repos under GitHub's spelling"
```

### Task 17: Reading repository visibility

**Files:**
- Create: `internal/runner/visibility.go`
- Modify: `internal/runner/manager.go` (the `GitHub` interface, the `Manager` struct, `Init`, `ClearDegraded`)
- Modify: `internal/runner/tick.go` (one line in `Tick`, two lines in `gatherDemand`)
- Test: `internal/runner/visibility_test.go` (new), `internal/runner/fakes_test.go` (the fake gains `GetRepo`), `internal/daemon/run_test.go` (the stub GitHub server answers for the repository)

**Interfaces:**
- Consumes: nothing
- Produces: `runner.GitHub.GetRepo` and the fake's implementation, with the fake's `setPublic(repo string, public bool)`; the Manager's per-repository visibility state with `visibilityLocked` and `visibilityTTL` (Contracts). Also in `internal/runner/visibility.go`, and used by the later tasks on blocked repositories: `func (m *Manager) readVisibility(ctx context.Context, repo string, now time.Time) error` and `func (m *Manager) forgetRemoved(cfg *config.Config)`. The state itself is the Manager field `vis`.

**Items:** 32
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

This task only records what GitHub says. It changes no scheduling: a public repository is still served after it. The one behaviour that does change is that a failing `GetRepo` now shows as that repository's error with the usual poll backoff.

How the read fits the tick:

- `gatherDemand` polls each repository in turn. Right after one repository's poll succeeds, and before the next repository is polled, its visibility is read if the manager has no answer for it or the answer is 6 hours old.
- A failed read takes the same path as a failed poll of that repository: the repository counts as not having answered, `apiErr` files the error (a 401 degrades, a rate limit pauses, anything else is the repository's error with backoff), and the wait doubles while it keeps failing. The answer the manager already had, if any, is left as it is.
- A repository that is not polled (paused, or waiting out a backoff) is not read.
- `ClearDegraded` (token replaced, config reloaded) makes every answer due at the next poll, and keeps the answers until then.
- Answers for repositories that left the config are dropped on every tick, also while API calls are paused.

- [ ] **Step 1: Give the test fake a `GetRepo`**

In `internal/runner/fakes_test.go`, in the `fakeGH` struct, replace these two lines:

```go
	releases  []github.Release
	hook      func(method string) // runs during every call, after it is recorded
```

with:

```go
	releases  []github.Release
	public    map[string]bool     // repos GetRepo reports public; any other is private
	hook      func(method string) // runs during every call, after it is recorded
```

Replace the `newFakeGH` function with:

```go
func newFakeGH() *fakeGH {
	return &fakeGH{runs: map[string][]github.Run{}, jobs: map[int64][]github.Job{}, runners: map[int64]*github.Runner{},
		listed: map[string][]github.Runner{}, errs: map[string]error{}, public: map[string]bool{}, nextID: 100, remaining: 4999}
}
```

Directly after the fake's `RateRemaining` method add:

```go
func (f *fakeGH) GetRepo(_ context.Context, repo string) (*github.Repository, error) {
	if err := f.call("GetRepo", repo); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return &github.Repository{FullName: "darkraise/" + repo, Private: !f.public[repo]}, nil
}

func (f *fakeGH) setPublic(repo string, public bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.public[repo] = public
}
```

The fake reports every repository private unless a test says otherwise, so the package's existing tests keep their meaning. Like every other method of the fake, `GetRepo` records its call as `GetRepo <repo>` and returns an error injected with `setErr("GetRepo <repo>", err)`.

- [ ] **Step 2: Write the failing tests**

Create `internal/runner/visibility_test.go`:

```go
package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
)

// visibility is what the manager last read for repo.
func (h *harness) visibility(repo string) (public, known bool) {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	return h.m.visibilityLocked(repo)
}

func (h *harness) repoReads(repo string) int {
	return strings.Count(h.gh.callsText(), "GetRepo "+repo)
}

// A repository's visibility is read right after its poll succeeded, before
// the tick goes on to the next repository, and then not again until the
// answer is six hours old.
func TestVisibilityIsReadAfterThePollAndEverySixHours(t *testing.T) {
	h := newHarness(t)
	h.gh.setPublic("darkmem", true)
	if _, known := h.visibility("darkmem"); known {
		t.Fatal("an answer before any read")
	}
	h.m.Tick(context.Background())
	want := "ListRuns darkcloud,ListRuns darkcloud,ListRuns darkcloud,GetRepo darkcloud," +
		"ListRuns darkmem,ListRuns darkmem,ListRuns darkmem,GetRepo darkmem"
	if got := h.gh.callsText(); !strings.HasPrefix(got, want) {
		t.Fatalf("calls %s\nwant them to start with %s", got, want)
	}
	if public, known := h.visibility("darkcloud"); public || !known {
		t.Fatalf("darkcloud: public %v known %v", public, known)
	}
	// Repository names are case-insensitive.
	if public, known := h.visibility("DarkMem"); !public || !known {
		t.Fatalf("darkmem: public %v known %v", public, known)
	}
	h.gh.setPublic("darkmem", false)
	h.now = h.now.Add(visibilityTTL - time.Second)
	h.m.Tick(context.Background())
	if public, _ := h.visibility("darkmem"); !public || h.repoReads("darkmem") != 1 {
		t.Fatalf("read again inside six hours: public %v, %d reads", public, h.repoReads("darkmem"))
	}
	h.now = h.now.Add(time.Second)
	h.m.Tick(context.Background())
	if public, _ := h.visibility("darkmem"); public || h.repoReads("darkmem") != 2 {
		t.Fatalf("not read again after six hours: public %v, %d reads", public, h.repoReads("darkmem"))
	}
}

// A replaced or reloaded token is the owner saying "look again": every answer
// is read again at the next poll, and stands until that read.
func TestClearDegradedMakesEveryVisibilityDue(t *testing.T) {
	h := newHarness(t)
	h.gh.setPublic("darkmem", true)
	h.m.Tick(context.Background())
	h.gh.setPublic("darkmem", false)
	h.m.ClearDegraded()
	if public, known := h.visibility("darkmem"); !public || !known {
		t.Fatalf("the last answer must stand until the next read: public %v known %v", public, known)
	}
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if public, _ := h.visibility("darkmem"); public || h.repoReads("darkmem") != 2 || h.repoReads("darkcloud") != 2 {
		t.Fatalf("public %v, reads darkmem %d darkcloud %d", public, h.repoReads("darkmem"), h.repoReads("darkcloud"))
	}
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if h.repoReads("darkmem") != 2 {
		t.Fatalf("read on every tick after ClearDegraded: %d reads", h.repoReads("darkmem"))
	}
}

// A read that fails is handled as that repository's failed poll: its error
// shows, its wait doubles while it keeps failing, and what GitHub said before
// still stands.
func TestFailedVisibilityReadKeepsTheLastAnswer(t *testing.T) {
	h := newHarness(t)
	h.m.Tick(context.Background()) // darkmem is read: private
	h.gh.setPublic("darkmem", true)
	h.gh.setErr("GetRepo darkmem", &github.APIError{Status: 502, Kind: github.ErrServer, Message: "Bad Gateway"})
	h.m.ClearDegraded()
	retryIn := func() time.Duration {
		h.m.mu.Lock()
		defer h.m.mu.Unlock()
		return h.m.retryAt["darkmem"].Sub(h.now)
	}
	for _, wait := range []time.Duration{10 * time.Second, 20 * time.Second} {
		h.now = h.now.Add(wait)
		ok, unanswered := h.m.gatherDemand(context.Background(), h.cfg, h.now)
		if ok || unanswered != "darkmem" {
			t.Fatalf("answered %v, unanswered %q", ok, unanswered)
		}
		st := h.m.Status()
		if st.Degraded || st.Repos[0].Error != "" || st.Repos[1].Error != "github: 502 Bad Gateway" {
			t.Fatalf("degraded %v errors %q and %q", st.Degraded, st.Repos[0].Error, st.Repos[1].Error)
		}
		if got := retryIn(); got != wait {
			t.Fatalf("next poll in %s, want %s", got, wait)
		}
		if public, known := h.visibility("darkmem"); public || !known {
			t.Fatalf("the last answer did not stand: public %v known %v", public, known)
		}
	}
	h.gh.setErr("GetRepo darkmem", nil)
	h.now = h.now.Add(40 * time.Second)
	if ok, unanswered := h.m.gatherDemand(context.Background(), h.cfg, h.now); !ok {
		t.Fatalf("%s did not answer after the read works again", unanswered)
	}
	if public, _ := h.visibility("darkmem"); !public {
		t.Fatal("the new answer was not stored")
	}
}

// A first read that fails leaves the repository without an answer. Only a
// rejected token or a rate limit stops the other repositories being read.
func TestFailedFirstVisibilityReadLeavesNoAnswer(t *testing.T) {
	for name, tc := range map[string]struct {
		err                    github.APIError
		degraded, paused, next bool // next: darkmem, the repository after it, is still read
	}{
		"server error":   {github.APIError{Status: 502, Kind: github.ErrServer}, false, false, true},
		"not visible":    {github.APIError{Status: 404, Kind: github.ErrNotFound}, false, false, true},
		"permission":     {github.APIError{Status: 403, Kind: github.ErrAuth}, false, false, true},
		"token rejected": {github.APIError{Status: 401, Kind: github.ErrAuth}, true, false, false},
		"rate limit":     {github.APIError{Status: 403, Kind: github.ErrRateLimit}, false, true, false},
	} {
		h := newHarness(t)
		tc.err.RetryAt = h.now.Add(time.Hour)
		h.gh.setErr("GetRepo darkcloud", &tc.err)
		ok, unanswered := h.m.gatherDemand(context.Background(), h.cfg, h.now)
		if ok || unanswered != "darkcloud" {
			t.Fatalf("%s: answered %v, unanswered %q", name, ok, unanswered)
		}
		if _, known := h.visibility("darkcloud"); known {
			t.Fatalf("%s: an answer from a failed read", name)
		}
		if _, known := h.visibility("darkmem"); known != tc.next {
			t.Fatalf("%s: darkmem read %v, want %v", name, known, tc.next)
		}
		paused := h.m.PausedUntil().After(h.now)
		if st := h.m.Status(); st.Degraded != tc.degraded || paused != tc.paused || (st.Repos[0].Error != "") != tc.next {
			t.Fatalf("%s: degraded %v paused %v error %q", name, st.Degraded, paused, st.Repos[0].Error)
		}
	}
}

// Only a repository whose poll just succeeded is read.
func TestVisibilityIsNotReadWithoutASuccessfulPoll(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 502, Kind: github.ErrServer})
	pause(h, "darkmem")
	h.m.Tick(context.Background())
	if calls := h.gh.callsText(); strings.Contains(calls, "GetRepo") {
		t.Fatalf("read without a successful poll: %s", calls)
	}
}

// What is kept about a repository goes when it leaves the config, on every
// tick and without an API call, so one that is added again starts unread.
func TestRemovedRepositoryIsForgotten(t *testing.T) {
	h := newHarness(t)
	h.m.Tick(context.Background())
	if _, known := h.visibility("darkmem"); !known {
		t.Fatal("setup: darkmem was not read")
	}
	h.m.apiErr("darkcloud", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Hour)}, h.now)
	without := h.cfg.Clone()
	without.Repos = without.Repos[:1]
	h.cfg = without
	calls := h.gh.callCount()
	h.m.Tick(context.Background())
	if _, known := h.visibility("darkmem"); known {
		t.Fatal("a removed repository kept its answer")
	}
	if _, known := h.visibility("darkcloud"); !known || h.gh.callCount() != calls {
		t.Fatalf("darkcloud known %v, API calls during the pause %d", known, h.gh.callCount()-calls)
	}
}
```

The harness these tests use is the package's own (`newHarness` in `fakes_test.go`): two repositories, `darkcloud` then `darkmem`, a manual clock `h.now`, and `pause(h, repo)` from `tick_test.go`. The third test uses a first answer of "private" on purpose, so it stays valid when a later task starts acting on a public answer.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/runner/ -run 'Visibility|TestRemovedRepositoryIsForgotten' -count=1`
Expected: the build fails with `h.m.visibilityLocked undefined (type *Manager has no field or method visibilityLocked)` and `undefined: visibilityTTL`

- [ ] **Step 4: Write the visibility state and its read**

Create `internal/runner/visibility.go`:

```go
package runner

import (
	"context"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/config"
)

// visibilityTTL is how long GitHub's answer on whether a repository is public
// is used before it is read again.
const visibilityTTL = 6 * time.Hour

// visibility is GitHub's last answer on whether a repository is public. A
// zero readAt makes the next poll read it again whatever its age.
type visibility struct {
	public bool
	readAt time.Time
}

// visibilityLocked reports whether GitHub last said repo is public, and
// whether it has answered for repo at all; m.mu must be held.
func (m *Manager) visibilityLocked(repo string) (public, known bool) {
	v, known := m.vis[strings.ToLower(repo)]
	return v.public, known
}

// readVisibility asks GitHub whether repo is public when the last answer is
// missing or visibilityTTL old. A failed read leaves the last answer as it is.
func (m *Manager) readVisibility(ctx context.Context, repo string, now time.Time) error {
	key := strings.ToLower(repo)
	m.mu.Lock()
	v, known := m.vis[key]
	m.mu.Unlock()
	if known && now.Sub(v.readAt) < visibilityTTL {
		return nil
	}
	r, err := m.GH.GetRepo(ctx, repo)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.vis[key] = visibility{public: !r.Private, readAt: now}
	m.mu.Unlock()
	return nil
}

// forgetRemoved drops the visibility kept for repos that are no longer in the
// config, so one that is removed and added again starts unread. It needs no
// API call, so it runs on every tick.
func (m *Manager) forgetRemoved(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.vis {
		if cfg.Repo(key) == nil {
			delete(m.vis, key)
		}
	}
}
```

`readVisibility` does not hold `m.mu` across the GitHub call; nothing in this package does. `cfg.Repo` compares names ignoring case, so a lower-cased key finds its repository.

- [ ] **Step 5: Wire it into the manager and the tick**

In `internal/runner/manager.go`:

(a) In the `GitHub` interface replace this line:

```go
	GenerateJITConfig(ctx context.Context, repo, name string, labels []string) (*github.JITConfig, error)
```

with:

```go
	GenerateJITConfig(ctx context.Context, repo, name string, labels []string) (*github.JITConfig, error)
	GetRepo(ctx context.Context, repo string) (*github.Repository, error)
```

The daemon passes its `*github.Client` as `GH`, and that type already has this method, so nothing outside this package changes.

(b) In the `Manager` struct replace this line:

```go
	spawnErr       map[string]string     // the last failed start's message
```

with:

```go
	spawnErr       map[string]string     // the last failed start's message
	vis            map[string]visibility // by lower-cased repo; a repo without an entry is unread
```

(c) In `Init` replace this line:

```go
	m.spawnErr = map[string]string{}
```

with:

```go
	m.spawnErr = map[string]string{}
	m.vis = map[string]visibility{}
```

(d) Replace the `ClearDegraded` method, with its comment, by:

```go
// ClearDegraded is called after the token is replaced or reloaded. A new
// token may also be what a failing spawn was waiting for. Every repository's
// visibility is read again at its next poll; the last answer stands until
// then.
func (m *Manager) ClearDegraded() {
	m.mu.Lock()
	m.degraded = false
	m.degradedReason = ""
	m.spawnFails, m.spawnRetryAt, m.spawnErr = map[string]int{}, map[string]time.Time{}, map[string]string{}
	for key, v := range m.vis {
		v.readAt = time.Time{}
		m.vis[key] = v
	}
	m.mu.Unlock()
}
```

In `internal/runner/tick.go`:

(e) In `Tick` replace this line:

```go
	m.forgetPaused(cfg)
```

with:

```go
	m.forgetPaused(cfg)
	m.forgetRemoved(cfg)
```

(f) In `gatherDemand` replace these two lines:

```go
		jobs, err := m.repoDemand(ctx, cfg, r, ours, now)
		if err != nil {
```

with:

```go
		jobs, err := m.repoDemand(ctx, cfg, r, ours, now)
		if err == nil {
			// Read here, before the tick plans anything, so nothing is planned
			// on an answer that is due. A failed read is this poll's failure.
			err = m.readVisibility(ctx, r.Name, now)
		}
		if err != nil {
```

Leave the body of that `if err != nil` block as it is. It already does what a failed read needs: the repository is counted as not having answered, a rejected token is noted, `apiErr` files the error, and the loop stops on a rejected token or a rate limit and goes on to the next repository otherwise. Because the block ends in `continue` or `break`, the lines after it (which record a successful poll and clear the repository's failure count) do not run for a failed read, and that is what makes the wait double.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/runner/ -run 'Visibility|TestRemovedRepositoryIsForgotten' -count=1`
Expected: PASS

- [ ] **Step 7: Run the package suite**

Run: `PATH=/usr/local/go/bin:$PATH timeout 600 go test -race ./internal/runner/ -count=1`
Expected: PASS. No existing test of this package changes: each of them polls through `Tick` or `gatherDemand`, and the fake answers the new call with "private".

- [ ] **Step 8: Correct the daemon's stub GitHub server**

One existing test outside this package starts a whole daemon against a stub GitHub server: `TestRunServesTicksReloadsAndKeepsRunners` in `internal/daemon/run_test.go`. The stub answers 404 for the repository itself, which GitHub never does for a repository the token can poll. With this task the read fails, the repository shows "token lacks access to repo", and the runner the test waits for is never spawned.

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run TestRunServesTicksReloadsAndKeepsRunners -count=1`
Expected: FAIL after about 10 seconds with `timed out waiting for a tick to spawn darkmem's runner`

In `internal/daemon/run_test.go`, in the `fakeGitHub` function, replace these four lines:

```go
		case strings.HasSuffix(p, "/actions/runners"):
			fmt.Fprint(w, `{"runners":[]}`)
		default:
			w.WriteHeader(404)
```

with:

```go
		case strings.HasSuffix(p, "/actions/runners"):
			fmt.Fprint(w, `{"runners":[]}`)
		case p == "/repos/darkraise/darkcloud" || p == "/repos/darkraise/darkmem":
			fmt.Fprintf(w, `{"full_name":%q,"private":true}`, strings.TrimPrefix(p, "/repos/"))
		default:
			w.WriteHeader(404)
```

The file has a second stub server further down with the same `/actions/runners` case; it has no `default:` line directly after it, so the four lines above match only in `fakeGitHub`.

Run: `PATH=/usr/local/go/bin:$PATH timeout 600 go test -race ./internal/daemon/ -count=1`
Expected: PASS. This also proves the daemon's GitHub client still satisfies `runner.GitHub`.

- [ ] **Step 9: Commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
git add internal/runner/visibility.go internal/runner/manager.go internal/runner/tick.go internal/runner/fakes_test.go internal/runner/visibility_test.go internal/daemon/run_test.go
git commit -m "feat(runner): read repository visibility"
```

### Task 18: Blocked and unread repositories: planning

**Files:**
- Modify: `internal/runner/visibility.go` (four new methods and one import)
- Modify: `internal/runner/tick.go` (the `spawnPlanned` function only)
- Modify: `internal/runner/lifecycle.go` (the `stopIdle` function only)
- Test: `internal/runner/blocked_test.go` (new)

**Interfaces:**
- Consumes: `config.Repo.AllowPublic` (Contracts); the visibility state with `visibilityLocked` (Contracts); in the tests, the fake's `GetRepo` (Contracts) with its `setPublic(repo string, public bool)` in `internal/runner/fakes_test.go`, and the manager's `readVisibility(ctx, repo, now) error` in `internal/runner/visibility.go`
- Produces: `blocked`, `unread` and `planConfig` (Contracts); and `func (m *Manager) blockedLocked(cfg *config.Config, repo config.Repo) bool`, the same test as `blocked` for callers that hold `m.mu`, which the next task uses; and the test helpers `allowPublic`, `homelabJob` and `spawnsFor` in `internal/runner/blocked_test.go` (Contracts)

**Items:** 32
**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

Two states, both worked out from what the manager already keeps (GitHub's last answer per repository) and the config:

- **Blocked:** GitHub's last answer says the repository is public, and the config does not set `allow_public` for it.
- **Unread:** GitHub has not answered for the repository yet.

What this task makes them mean:

- A blocked repository gets no new runner, and its idle runners are stopped. A runner of it that is running a job is left alone.
- An unread repository gets no new runner. Nothing else changes for it: its runners are not stopped.

It is done with a copy of the config in which every blocked repository has `Paused` set, handed to the scheduler in the two places that plan: spawning and the idle stop. The real config is never changed, so a blocked repository is still polled and status still shows its `Paused` as the config has it.

Not in this task, and wrong until the next one: status does not yet say why a blocked repository gets nothing, no event is written, and its queued jobs still count (in status, and against a queued runner update).

- [ ] **Step 1: Write the failing tests**

Create `internal/runner/blocked_test.go`:

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

// allowPublic sets allow_public for repo and applies it as a reload of
// config.yaml does.
func allowPublic(h *harness, repo string) {
	c := h.cfg.Clone()
	c.Repo(repo).AllowPublic = true
	h.cfg = c
	h.m.ClearDegraded()
}

func homelabJob(h *harness, id int64) github.Job {
	return github.Job{ID: id, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now}
}

func spawnsFor(h *harness, repo string) int {
	return strings.Count(h.gh.callsText(), "GenerateJITConfig "+repo)
}

// Blocked is public on GitHub without allow_public in the config; unread is
// no answer yet. The plan copy pauses the blocked repositories and nothing
// else, and leaves the config it was made from alone.
func TestBlockedUnreadAndThePlanCopy(t *testing.T) {
	h := newHarness(t)
	h.gh.setPublic("darkmem", true)
	cloud, mem := h.cfg.Repos[0], h.cfg.Repos[1]
	if !h.m.unread(cloud) || !h.m.unread(mem) || h.m.blocked(h.cfg, mem) {
		t.Fatal("before any read every repository is unread and none is blocked")
	}
	if plan := h.m.planConfig(h.cfg); plan.Repos[0].Paused || plan.Repos[1].Paused {
		t.Fatalf("an unread repository is paused in the plan copy: %+v", plan.Repos)
	}
	h.m.gatherDemand(context.Background(), h.cfg, h.now)
	if h.m.unread(cloud) || h.m.unread(mem) {
		t.Fatal("unread after a read")
	}
	if h.m.blocked(h.cfg, cloud) || !h.m.blocked(h.cfg, mem) {
		t.Fatalf("blocked: darkcloud %v darkmem %v", h.m.blocked(h.cfg, cloud), h.m.blocked(h.cfg, mem))
	}
	plan := h.m.planConfig(h.cfg)
	if plan == h.cfg || plan.Repos[0].Paused || !plan.Repos[1].Paused {
		t.Fatalf("plan copy %+v", plan.Repos)
	}
	if h.cfg.Repos[1].Paused {
		t.Fatal("the config itself was changed")
	}
	allowPublic(h, "darkmem")
	if h.m.blocked(h.cfg, h.cfg.Repos[1]) || h.m.planConfig(h.cfg).Repos[1].Paused {
		t.Fatal("blocked with allow_public set")
	}
}

// A repository that turns out to be public gets no new runner and loses its
// idle one. Its running job goes on, the other repositories are served, and
// allow_public brings it back.
func TestBlockedRepositoryGetsNoRunnerAndLosesItsIdleOne(t *testing.T) {
	h := newHarness(t)
	h.cfg.GlobalMax = 6
	three := 3
	h.cfg.Repos[1].Max = &three
	for range 2 {
		if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
			t.Fatal(err)
		}
	}
	h.gh.setRunner(101, "online", true) // aaaaaa has a job
	h.gh.setRunner(102, "online", false)
	h.m.Tick(context.Background()) // darkmem is read: private
	if h.state("aaaaaa") != "busy" || h.state("bbbbbb") != "idle" {
		t.Fatalf("setup: aaaaaa %s bbbbbb %s", h.state("aaaaaa"), h.state("bbbbbb"))
	}

	h.gh.setPublic("darkmem", true)
	h.m.ClearDegraded()
	queuedRun(h, "darkmem", 7, homelabJob(h, 71), homelabJob(h, 72))
	queuedRun(h, "darkcloud", 8, homelabJob(h, 81))
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	h.m.Wait()
	if len(h.sd.stopped) != 1 || h.sd.stopped[0] != "ghr-runner-bbbbbb" || h.state("aaaaaa") != "busy" {
		t.Fatalf("stopped %v, aaaaaa %s; want only the idle runner stopped", h.sd.stopped, h.state("aaaaaa"))
	}
	if spawnsFor(h, "darkmem") != 2 || spawnsFor(h, "darkcloud") != 1 {
		t.Fatalf("spawns: darkmem %d (2 before it was public), darkcloud %d", spawnsFor(h, "darkmem"), spawnsFor(h, "darkcloud"))
	}

	allowPublic(h, "darkmem")
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	h.m.Wait()
	// One of the two queued jobs may be the one aaaaaa is running, which
	// GitHub has not confirmed yet, so one runner is enough.
	if spawnsFor(h, "darkmem") != 3 {
		t.Fatalf("not served with allow_public: %d spawns for darkmem\n%s", spawnsFor(h, "darkmem"), h.eventText())
	}
}

// spawnPlanned plans nothing for a repository GitHub has not answered for,
// whatever else is known about it.
func TestSpawnPlannedLeavesOutAnUnreadRepository(t *testing.T) {
	h := newHarness(t)
	h.cfg.Mode = config.ModeAll // each repository wants one warm runner
	if err := h.m.readVisibility(context.Background(), "darkcloud", h.now); err != nil {
		t.Fatal(err)
	}
	h.m.spawnPlanned(context.Background(), h.cfg, h.now)
	if len(h.gh.jitCalls) != 1 || !strings.HasPrefix(h.gh.jitCalls[0], "ghr-darkcloud-") {
		t.Fatalf("jit calls %v, want a runner for darkcloud only", h.gh.jitCalls)
	}
}

// After a restart nothing has been read. A repository whose first read fails
// keeps the runner it had and gets no new one until GitHub answers.
func TestUnreadRepositoryKeepsItsAdoptedRunner(t *testing.T) {
	h := newHarness(t)
	two := 2
	h.cfg.Repos[1].Max = &two
	makeInstance(t, h, "aaaaaa", "darkmem", 1, false)
	h.sd.active["ghr-runner-aaaaaa"] = true
	h.gh.setRunner(1, "online", false)
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	queuedRun(h, "darkmem", 7, homelabJob(h, 71), homelabJob(h, 72)) // one more than the adopted runner covers
	h.gh.setErr("GetRepo darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
	h.m.Tick(context.Background())
	if !h.m.unread(h.cfg.Repos[1]) || h.state("aaaaaa") != "idle" || len(h.sd.stopped) != 0 || len(h.gh.jitCalls) != 0 {
		t.Fatalf("unread %v aaaaaa %s stopped %v jit calls %v", h.m.unread(h.cfg.Repos[1]), h.state("aaaaaa"), h.sd.stopped, h.gh.jitCalls)
	}
	h.gh.setErr("GetRepo darkmem", nil)
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if len(h.sd.stopped) != 0 || len(h.gh.jitCalls) != 1 {
		t.Fatalf("stopped %v jit calls %v; want the second job's runner once GitHub answered", h.sd.stopped, h.gh.jitCalls)
	}
}

// A public repository that was paused when ghr started has never been read.
// Resuming it must not give it a runner: the read comes before the plan.
func TestResumedPublicRepositoryIsBlockedBeforeItIsPlanned(t *testing.T) {
	h := newHarness(t)
	h.gh.setPublic("darkmem", true)
	pause(h, "darkmem")
	queuedRun(h, "darkmem", 7, homelabJob(h, 71))
	h.m.Tick(context.Background())
	if !h.m.unread(h.cfg.Repos[1]) {
		t.Fatal("a paused repository was read")
	}
	resumed := h.cfg.Clone()
	resumed.Repo("darkmem").Paused = false
	h.cfg = resumed
	h.m.Tick(context.Background())
	if !h.m.blocked(h.cfg, h.cfg.Repos[1]) || len(h.gh.jitCalls) != 0 {
		t.Fatalf("blocked %v jit calls %v", h.m.blocked(h.cfg, h.cfg.Repos[1]), h.gh.jitCalls)
	}
}
```

What the tests lean on, all existing in this package's test files: `newHarness` gives two repositories, `darkcloud` (index 0, `max: 1`) then `darkmem` (index 1), in queue mode with `global_max: 2` and a manual clock `h.now`; new instance IDs come out as `aaaaaa`, `bbbbbb`, and so on, and the fake numbers registrations from 101; `queuedRun` and `pause` are in `tick_test.go`; `makeInstance` is in `adopt_test.go`; the fake GitHub reports a repository private unless `setPublic` says otherwise, and `readVisibility` is the manager's own read.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/runner/ -run 'TestBlockedUnreadAndThePlanCopy|TestBlockedRepositoryGetsNoRunnerAndLosesItsIdleOne|TestSpawnPlannedLeavesOutAnUnreadRepository|TestUnreadRepositoryKeepsItsAdoptedRunner|TestResumedPublicRepositoryIsBlockedBeforeItIsPlanned' -count=1`
Expected: the build fails with `h.m.unread undefined`, `h.m.blocked undefined` and `h.m.planConfig undefined`

- [ ] **Step 3: Define blocked, unread and the plan copy**

In `internal/runner/visibility.go` replace the import block with:

```go
import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/config"
)
```

and add these four methods directly above the comment of the `forgetRemoved` function:

```go
// blockedLocked is blocked with m.mu held.
func (m *Manager) blockedLocked(cfg *config.Config, repo config.Repo) bool {
	r := cfg.Repo(repo.Name)
	if r == nil || r.AllowPublic {
		return false
	}
	public, _ := m.visibilityLocked(repo.Name)
	return public
}

// blocked reports whether repo must get no runner: GitHub last reported it
// public and cfg does not set allow_public for it. A job in a public
// repository can come from anyone's pull request.
func (m *Manager) blocked(cfg *config.Config, repo config.Repo) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.blockedLocked(cfg, repo)
}

// unread reports whether GitHub has not yet said whether repo is public.
// Until it has, repo gets no new runner.
func (m *Manager) unread(repo config.Repo) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, known := m.visibilityLocked(repo.Name)
	return !known
}

// planConfig returns a copy of cfg in which every blocked repo is paused, for
// the scheduler to plan from: a blocked repo then gets no new runner and its
// idle ones stop, while its running jobs finish. Unlike a paused repo it is
// still polled and keeps its error, so the copy is never saved or shown.
func (m *Manager) planConfig(cfg *config.Config) *config.Config {
	plan := *cfg
	plan.Repos = slices.Clone(cfg.Repos)
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range plan.Repos {
		if m.blockedLocked(cfg, r) {
			plan.Repos[i].Paused = true
		}
	}
	return &plan
}

```

Points a reviewer will check:

- `blocked` reads `allow_public` from the `cfg` it is given, looked up by name, so the answer follows the config the caller holds. An unread repository is not blocked: `visibilityLocked` reports `public` false for it.
- `planConfig` copies the `Repos` slice before it sets `Paused`, so the caller's config is untouched. The copy shares the label slices of each repository with the original; nothing writes to them.
- `m.mu` is not reentrant. `blocked` and `unread` take it, so they must not be called with it held; `blockedLocked` is for callers that hold it.

- [ ] **Step 4: Run the tests to see which fail now**

Run the command of Step 2 again.
Expected: `TestBlockedUnreadAndThePlanCopy` and `TestUnreadRepositoryKeepsItsAdoptedRunner` PASS, and three FAIL:
- `TestBlockedRepositoryGetsNoRunnerAndLosesItsIdleOne`: `stopped [], aaaaaa busy; want only the idle runner stopped`
- `TestSpawnPlannedLeavesOutAnUnreadRepository`: `jit calls [ghr-darkcloud-aaaaaa self-hosted,linux,x64,homelab ghr-darkmem-bbbbbb self-hosted,linux,x64,homelab], want a runner for darkcloud only`
- `TestResumedPublicRepositoryIsBlockedBeforeItIsPlanned`: `blocked true jit calls [ghr-darkmem-aaaaaa self-hosted,linux,x64,homelab]`

`TestUnreadRepositoryKeepsItsAdoptedRunner` passes before the planning change because a failed read already leaves the repository in an error state, and a repository in an error state is not planned. It stays in the suite as the guard for that start-up case. The test that fails for the unread rule itself is `TestSpawnPlannedLeavesOutAnUnreadRepository`.

- [ ] **Step 5: Plan from the copy**

In `internal/runner/tick.go` replace the whole `spawnPlanned` function, with its comment, by:

```go
// spawnPlanned spawns what sched.Plan decides, nothing while a runner update
// runs. It plans from planConfig, so a blocked repo gets nothing. Repos that
// are unread, in an error state, or waiting after a failed start, are left
// out of planning. Spawning stops as soon as the live config differs from the
// one the plan used, so a pause or cap change made during polling is never
// overridden.
func (m *Manager) spawnPlanned(ctx context.Context, cfg *config.Config, now time.Time) {
	plan := m.planConfig(cfg)
	var read []config.Repo
	for _, r := range plan.Repos {
		if !m.unread(r) {
			read = append(read, r)
		}
	}
	m.mu.Lock()
	if m.upd.running {
		m.mu.Unlock()
		return
	}
	demand := m.demand
	planCfg := *plan
	planCfg.Repos = nil
	for _, r := range read {
		if m.repoErr[r.Name] == "" && !now.Before(m.retryAt[r.Name]) && !now.Before(m.spawnRetryAt[strings.ToLower(r.Name)]) {
			planCfg.Repos = append(planCfg.Repos, r)
		}
	}
	m.mu.Unlock()
	failed := map[string]bool{}
	for _, s := range sched.Plan(&planCfg, m.schedInstances(cfg), demand, now) {
		if m.Config() != cfg {
			return
		}
		if failed[strings.ToLower(s.Repo)] {
			continue
		}
		err := m.spawn(ctx, cfg, s.Repo)
		if err == nil {
			continue
		}
		failed[strings.ToLower(s.Repo)] = true
		if tokenRejected(err) || github.IsKind(err, github.ErrRateLimit) {
			m.Events.Add("error", s.Repo, "spawn failed: %v", err)
			m.apiErr(s.Repo, err, now)
			return
		}
		msg := spawnText(err)
		d := m.spawnFailed(s.Repo, msg, m.Now())
		m.Events.Add("error", s.Repo, "spawn failed: %s; next attempt in %s", msg, d)
	}
}
```

Only the first part changed: the plan copy, the unread filter, and `planCfg` starting from the copy. The error-state and failed-start filters, the comparison of the live config with `cfg` (the real one, not the copy), `m.spawn(ctx, cfg, ...)` and the whole loop are as they were. `m.schedInstances` keeps taking the real config.

In `internal/runner/lifecycle.go` replace the whole `stopIdle` function, with its comment, by:

```go
// stopIdle stops idle runners chosen by sched.IdleToStop. It chooses from
// planConfig, so every idle runner of a blocked repo is stopped. An unread
// repo is not blocked: its runners stay.
func (m *Manager) stopIdle(ctx context.Context, cfg *config.Config, now time.Time) {
	byID := map[string]instance{}
	for _, i := range m.snapshot() {
		byID[i.ID] = i
	}
	for _, id := range sched.IdleToStop(m.planConfig(cfg), m.schedInstances(cfg), now) {
		if m.stopIdleRunner(ctx, byID[id], now) == idleKept && !m.apiAllowed(now) {
			return
		}
	}
}
```

Each idle runner still goes through `stopIdleRunner`, which asks GitHub whether the runner is busy and deletes its registration before the unit is stopped, so a runner that has just taken a job is not killed.

- [ ] **Step 6: Run the tests to verify they pass**

Run the command of Step 2 again.
Expected: PASS

- [ ] **Step 7: Run the package suites**

Run: `PATH=/usr/local/go/bin:$PATH timeout 600 go test -race ./internal/runner/ -count=1`
Expected: PASS. No existing test changes: every existing test that expects a spawn reaches it through a tick whose poll succeeded, and the fake answers the read that follows with "private".

Run: `PATH=/usr/local/go/bin:$PATH timeout 600 go test -race ./internal/daemon/ -count=1`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
git add internal/runner/visibility.go internal/runner/tick.go internal/runner/lifecycle.go internal/runner/blocked_test.go
git commit -m "feat(runner): block public repos without the flag"
```

### Task 19: Blocked and unread repositories: demand, status and events

**Files:**
- Modify: `internal/runner/visibility.go` (two new functions, `forgetRemoved` replaced)
- Modify: `internal/runner/manager.go` (the `Manager` struct, `Init`, three lines in `Status`)
- Modify: `internal/runner/tick.go` (one line in `Tick`, the `gatherDemand` function)
- Modify: `internal/runner/update.go` (the comment of `startUpdateIfFree` only)
- Test: `internal/runner/blocked_test.go`

**Interfaces:**
- Consumes: `blocked` and `unread` (Contracts); `func (m *Manager) blockedLocked(cfg *config.Config, repo config.Repo) bool`, the same test as `blocked` for callers that hold `m.mu`, which sits beside it in `internal/runner/visibility.go`; the Manager field `vis` and `forgetRemoved(cfg)` in the same file; `config.Repo.AllowPublic` (Contracts); in the tests, the fake's `setPublic(repo string, public bool)` in `internal/runner/fakes_test.go`; the test helpers `allowPublic`, `homelabJob` and `spawnsFor` in `internal/runner/blocked_test.go` (Contracts)
- Produces: nothing a later task names

**Items:** 32
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

A blocked repository (public on GitHub, no `allow_public` in the config) already gets no runner. This task makes the rest of the manager agree with that, and tells the owner.

- **Demand.** A blocked repository is still polled, so a job its runner is working on still gets its name and link. Its queued jobs are not kept: status shows `Queued 0` for it, and they cannot hold a queued runner update back. A failed poll of a blocked repository does not count as "a repository did not answer", which also holds an update back. In short, wherever `gatherDemand` asks today whether a repository is paused in order to decide those two things, it now asks whether the repository is paused or blocked. Whether a repository is polled at all still depends on the real `Paused` only.
- **Status.** A blocked repository's error line is exactly `<repo> is public; ghr serves only private repositories. Set allow_public: true for it in config.yaml and reload to serve it`. It comes before the repository's poll error and its spawn error. It is worked out when status is rendered and never stored with those two, so a successful poll does not clear it, and the poll error shows again once the block is lifted. `Paused` in status stays what the config says.
- **Events.** One event when a repository becomes blocked: level `warn`, the repository in the event's repo field, and the status text above as the message. One event when it stops being blocked: level `ok`, the repository in the repo field, and the message `no longer blocked`. The first follows this package's habit of putting the status message into the event (as the token rejection and the poll errors do); the second follows `GitHub token accepted again`. A repository that is removed from the config writes neither, and one that is added again and found public again is reported again.

The runner-update gate itself (`startUpdateIfFree` in `update.go`) needs no code change: it reads only what `gatherDemand` hands it, namely the demand and the name of the first repository that did not answer. Its comment changes.

Known limit, accepted: `gatherDemand` does not run while GitHub API calls are paused. A repository that becomes blocked during such a pause, because `allow_public` was taken out of the config, shows the text and writes the event at the next tick, but keeps its last queued count until API calls resume. No update can start during the pause anyway.

- [ ] **Step 1: Write the failing tests**

Append to `internal/runner/blocked_test.go`:

```go
const darkmemBlocked = "darkmem is public; ghr serves only private repositories. Set allow_public: true for it in config.yaml and reload to serve it"

// blockDarkmem makes darkmem public on GitHub and runs the tick that finds out.
func blockDarkmem(t *testing.T, h *harness) {
	t.Helper()
	h.gh.setPublic("darkmem", true)
	h.m.ClearDegraded()
	h.m.Tick(context.Background())
	if !h.m.blocked(h.cfg, h.cfg.Repos[1]) {
		t.Fatal("setup: darkmem is not blocked")
	}
}

func eventCount(h *harness, line string) int {
	return strings.Count(h.eventText(), line+"\n")
}

// The owner sees why a blocked repository gets nothing, on the tick that
// found it public and after every later poll, whatever its poll or its last
// start did. Its queue is not counted. What the poll said is kept underneath.
func TestBlockedRepositoryShowsWhyAndNoQueue(t *testing.T) {
	h := newHarness(t)
	h.cfg.GlobalMax = 0 // nothing spawns, so the job stays queued
	queuedRun(h, "darkmem", 7, homelabJob(h, 71))
	h.m.Tick(context.Background())
	if r := h.m.Status().Repos[1]; r.Queued != 1 || r.Error != "" {
		t.Fatalf("setup: queued %d error %q", r.Queued, r.Error)
	}
	check := func(when string) {
		t.Helper()
		st := h.m.Status()
		if r := st.Repos[1]; r.Error != darkmemBlocked || r.Queued != 0 || r.OldestQueuedAt != nil || r.Paused {
			t.Fatalf("%s: error %q queued %d oldest %v paused %v", when, r.Error, r.Queued, r.OldestQueuedAt, r.Paused)
		}
		if e := st.Repos[0].Error; e != "" {
			t.Fatalf("%s: darkcloud shows %q", when, e)
		}
	}
	h.now = h.now.Add(10 * time.Second)
	blockDarkmem(t, h)
	check("on the tick that found it public")
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	check("after a later successful poll")
	h.gh.setErr("ListRuns darkmem", &github.APIError{Status: 502, Kind: github.ErrServer, Message: "Bad Gateway"})
	h.m.spawnFailed("darkmem", "boom", h.now)
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	check("with a failed poll and a failed start")
	allowed := h.cfg.Clone()
	allowed.Repo("darkmem").AllowPublic = true
	h.cfg = allowed
	if e := h.m.Status().Repos[1].Error; e != "github: 502 Bad Gateway" {
		t.Fatalf("with allow_public the poll error must show again, got %q", e)
	}
}

// A repository also becomes blocked without a poll, when the owner takes
// allow_public away. Its last known queue must go even if its next poll fails.
func TestBlockedByConfigDropsTheQueueWithoutAPoll(t *testing.T) {
	h := newHarness(t)
	h.cfg.GlobalMax = 0 // nothing spawns, so the job stays queued
	h.gh.setPublic("darkmem", true)
	allowPublic(h, "darkmem")
	queuedRun(h, "darkmem", 7, homelabJob(h, 71))
	h.m.Tick(context.Background())
	if r := h.m.Status().Repos[1]; r.Queued != 1 || r.Error != "" {
		t.Fatalf("setup: queued %d error %q", r.Queued, r.Error)
	}
	denied := h.cfg.Clone()
	denied.Repo("darkmem").AllowPublic = false
	h.cfg = denied
	h.gh.setErr("ListRuns darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
	h.now = h.now.Add(10 * time.Second)
	ok, unanswered := h.m.gatherDemand(context.Background(), h.cfg, h.now)
	if r := h.m.Status().Repos[1]; r.Queued != 0 || r.Error != darkmemBlocked || !ok {
		t.Fatalf("queued %d error %q, %q did not answer", r.Queued, r.Error, unanswered)
	}
}

// One event when a repository becomes blocked and one when it stops being
// blocked, whichever side changed: the flag in the config or GitHub.
func TestBlockedEventsAreWrittenOncePerChange(t *testing.T) {
	h := newHarness(t)
	blocked, lifted := "warn darkmem "+darkmemBlocked, "ok darkmem no longer blocked"
	tick := func() {
		h.now = h.now.Add(10 * time.Second)
		h.m.Tick(context.Background())
	}
	want := func(when string, nBlocked, nLifted int) {
		t.Helper()
		if eventCount(h, blocked) != nBlocked || eventCount(h, lifted) != nLifted {
			t.Fatalf("%s: want %d blocked and %d lifted events:\n%s", when, nBlocked, nLifted, h.eventText())
		}
	}
	h.m.Tick(context.Background())
	want("private", 0, 0)
	blockDarkmem(t, h)
	tick()
	want("found public", 1, 0)
	allowPublic(h, "darkmem")
	tick()
	tick()
	want("allow_public set", 1, 1)
	denied := h.cfg.Clone()
	denied.Repo("darkmem").AllowPublic = false
	h.cfg = denied
	tick()
	want("allow_public removed", 2, 1)
	h.gh.setPublic("darkmem", false)
	h.m.ClearDegraded()
	tick()
	tick()
	want("private again on GitHub", 2, 2)
}

// Removing a repository is not "no longer blocked". Added again it starts
// unread, and is reported again when it is found public again.
func TestRemovedBlockedRepositoryWritesNoEventAndStartsUnread(t *testing.T) {
	h := newHarness(t)
	blockDarkmem(t, h)
	with := h.cfg
	without := h.cfg.Clone()
	without.Repos = without.Repos[:1]
	h.cfg = without
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	h.cfg = with
	if !h.m.unread(with.Repos[1]) || h.m.blocked(with, with.Repos[1]) {
		t.Fatalf("added again: unread %v blocked %v, want unread and not blocked", h.m.unread(with.Repos[1]), h.m.blocked(with, with.Repos[1]))
	}
	if e := h.m.Status().Repos[1].Error; e != "" {
		t.Fatalf("an unread repository shows %q", e)
	}
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if eventCount(h, "warn darkmem "+darkmemBlocked) != 2 || strings.Contains(h.eventText(), "no longer blocked") {
		t.Fatalf("want two blocked events and no lifted one:\n%s", h.eventText())
	}
}

// Pausing a blocked repository changes neither why it is blocked nor what the
// config says about the pause, and a paused repository is still not polled.
func TestBlockedRepositoryThatIsAlsoPaused(t *testing.T) {
	h := newHarness(t)
	blockDarkmem(t, h)
	pause(h, "darkmem")
	polls := strings.Count(h.gh.callsText(), "ListRuns darkmem")
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if r := h.m.Status().Repos[1]; !r.Paused || r.Error != darkmemBlocked {
		t.Fatalf("paused %v error %q", r.Paused, r.Error)
	}
	if n := strings.Count(h.gh.callsText(), "ListRuns darkmem"); n != polls {
		t.Fatalf("a paused repository was polled: %d then %d", polls, n)
	}
	resumed := h.cfg.Clone()
	resumed.Repo("darkmem").Paused = false
	h.cfg = resumed
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if r := h.m.Status().Repos[1]; r.Paused || r.Error != darkmemBlocked {
		t.Fatalf("resumed: paused %v error %q", r.Paused, r.Error)
	}
	if eventCount(h, "warn darkmem "+darkmemBlocked) != 1 || strings.Contains(h.eventText(), "no longer blocked") {
		t.Fatalf("a pause and a resume are not changes of the block:\n%s", h.eventText())
	}
}

// Nothing will serve a blocked repository, so neither its queue nor its
// failing poll may hold a runner update back.
func TestBlockedRepositoryDoesNotHoldAnUpdateBack(t *testing.T) {
	for name, setup := range map[string]func(h *harness){
		"its queued job": func(h *harness) { queuedRun(h, "darkmem", 7, homelabJob(h, 71)) },
		"its failed poll": func(h *harness) {
			h.gh.setErr("ListRuns darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
		},
	} {
		h := newHarness(t)
		blockDarkmem(t, h)
		setup(h)
		queuedUpdate(t, h, tarballSum())
		h.now = h.now.Add(10 * time.Second)
		h.m.Tick(context.Background())
		h.m.Wait()
		if current(t, h) != "2.338.0" || strings.Contains(h.eventText(), "runner update waiting") {
			t.Fatalf("%s: runner %s\n%s", name, current(t, h), h.eventText())
		}
	}
}
```

What the tests lean on, all existing: `allowPublic`, `homelabJob` and the imports at the top of this file; `queuedRun` and `pause` in `tick_test.go`; `queuedUpdate`, `tarballSum` and `current` in `install_test.go` (`queuedUpdate` installs runner 2.337.0 and queues the update to 2.338.0); `h.eventText()` prints one line per event as `<level> <repo> <message>`. `darkmem` is the second repository of the harness, so `Repos[1]`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/runner/ -run 'TestBlockedRepositoryShowsWhyAndNoQueue|TestBlockedByConfigDropsTheQueueWithoutAPoll|TestBlockedEventsAreWrittenOncePerChange|TestRemovedBlockedRepositoryWritesNoEventAndStartsUnread|TestBlockedRepositoryThatIsAlsoPaused|TestBlockedRepositoryDoesNotHoldAnUpdateBack' -count=1`
Expected: all six FAIL.
- `TestBlockedRepositoryShowsWhyAndNoQueue`: `on the tick that found it public: error "" queued 1 ...`
- `TestBlockedByConfigDropsTheQueueWithoutAPoll`: `queued 1 error "github: 502 ", "darkmem" did not answer`
- `TestBlockedEventsAreWrittenOncePerChange`: `found public: want 1 blocked and 0 lifted events:`
- `TestRemovedBlockedRepositoryWritesNoEventAndStartsUnread`: `want two blocked events and no lifted one:`
- `TestBlockedRepositoryThatIsAlsoPaused`: `paused true error ""`
- `TestBlockedRepositoryDoesNotHoldAnUpdateBack`: `its queued job: runner 2.337.0` or `its failed poll: runner 2.337.0`, whichever row the map yields first

- [ ] **Step 3: Write the status text, the events and the cleanup**

In `internal/runner/visibility.go` replace the whole `forgetRemoved` function, with its comment, by these three functions:

```go
// blockedText tells the owner why a blocked repo gets no runner and what
// lifts the block.
func blockedText(repo string) string {
	return repo + " is public; ghr serves only private repositories. Set allow_public: true for it in config.yaml and reload to serve it"
}

// noteBlocked writes one event when a repo becomes blocked and one when it
// stops being blocked, whether GitHub's answer or the config changed. It
// needs no API call, so it runs on every tick.
func (m *Manager) noteBlocked(cfg *config.Config) {
	var became, lifted []string
	m.mu.Lock()
	for _, r := range cfg.Repos {
		key := strings.ToLower(r.Name)
		is := m.blockedLocked(cfg, r)
		switch {
		case is && !m.wasBlocked[key]:
			m.wasBlocked[key] = true
			became = append(became, r.Name)
		case !is && m.wasBlocked[key]:
			delete(m.wasBlocked, key)
			lifted = append(lifted, r.Name)
		}
	}
	m.mu.Unlock()
	for _, name := range became {
		m.Events.Add("warn", name, "%s", blockedText(name))
	}
	for _, name := range lifted {
		m.Events.Add("ok", name, "no longer blocked")
	}
}

// forgetRemoved drops what is kept about visibility for repos that are no
// longer in the config, so one that is removed and added again starts unread
// and its removal is not reported as a block being lifted. It needs no API
// call, so it runs on every tick.
func (m *Manager) forgetRemoved(cfg *config.Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.vis {
		if cfg.Repo(key) == nil {
			delete(m.vis, key)
		}
	}
	for key := range m.wasBlocked {
		if cfg.Repo(key) == nil {
			delete(m.wasBlocked, key)
		}
	}
}
```

The events are written after `m.mu` is released, as everywhere else in this package.

In `internal/runner/manager.go`:

(a) In the `Manager` struct replace this line:

```go
	vis            map[string]visibility // by lower-cased repo; a repo without an entry is unread
```

with:

```go
	vis            map[string]visibility // by lower-cased repo; a repo without an entry is unread
	wasBlocked     map[string]bool       // lower-cased repos whose block has been reported
```

(b) In `Init` replace this line:

```go
	m.vis = map[string]visibility{}
```

with:

```go
	m.vis = map[string]visibility{}
	m.wasBlocked = map[string]bool{}
```

(c) In `Status` replace these three lines:

```go
		if rs.Error == "" && !r.Paused {
			rs.Error = m.spawnErr[strings.ToLower(r.Name)]
		}
```

with:

```go
		if rs.Error == "" && !r.Paused {
			rs.Error = m.spawnErr[strings.ToLower(r.Name)]
		}
		// Why a blocked repo gets nothing comes before what its poll or its
		// last start said. It is worked out here and not stored with them,
		// so a poll that succeeds does not clear it.
		if m.blockedLocked(cfg, r) {
			rs.Error = blockedText(r.Name)
		}
```

`Status` holds `m.mu` here, so it must call `blockedLocked`, not `blocked`. `cfg` in `Status` is the live config, and `Paused` in the line above these is filled from it and is not touched.

- [ ] **Step 4: Drop a blocked repository's demand**

In `internal/runner/tick.go`:

(a) In `Tick` replace these two lines:

```go
		demandOK, unanswered = m.gatherDemand(ctx, cfg, now)
	}
```

with:

```go
		demandOK, unanswered = m.gatherDemand(ctx, cfg, now)
	}
	m.noteBlocked(cfg)
```

It sits outside the `if`, so it also runs on a tick in which API calls are not allowed, and after `gatherDemand`, so a repository found public in this tick is reported in this tick, before its idle runners are stopped.

(b) Replace the whole `gatherDemand` function, with its comment, by:

```go
// gatherDemand lists matching queued jobs per repo and confirms our in-progress
// jobs. Degraded clears only after a tick in which some repo answered and none
// rejected the token. It reports whether every served repo answered, and the
// first one, in config order, that did not. A served repo is one that is
// neither paused nor blocked.
//
// A repo that does not answer keeps its last known queued jobs for status;
// spawnPlanned already leaves such a repo out. A paused repo is polled only
// while one of its busy runners still lacks its job's details, and a blocked
// repo is polled like any other. The queue of either is never kept: nothing
// will serve it, and it must not hold an update back.
func (m *Manager) gatherDemand(ctx context.Context, cfg *config.Config, now time.Time) (bool, string) {
	ours := map[string]string{}
	for _, i := range m.snapshot() {
		ours[i.RunnerName] = i.ID
	}
	m.mu.Lock()
	last := m.demand
	m.mu.Unlock()
	// Asked per use, not once: a repo's own read, below, can change the answer.
	served := func(r config.Repo) bool { return !r.Paused && !m.blocked(cfg, r) }
	demand := sched.Demand{}
	for _, r := range cfg.Repos {
		if jobs, ok := last[r.Name]; ok && served(r) {
			demand[r.Name] = jobs
		}
	}
	anyOK, authFailed, unanswered := false, false, ""
	missed := func(r config.Repo) {
		if served(r) && unanswered == "" {
			unanswered = r.Name
		}
	}
	forget := func(name string) {
		m.mu.Lock()
		delete(m.fails, name)
		delete(m.retryAt, name)
		delete(m.repoErr, name)
		m.mu.Unlock()
	}
	for _, r := range cfg.Repos {
		if r.Paused && !m.unconfirmedBusy(r.Name) {
			continue // forgetPaused has dropped what its last polls left behind
		}
		m.mu.Lock()
		wait := now.Before(m.retryAt[r.Name])
		m.mu.Unlock()
		if wait {
			missed(r)
			continue
		}
		jobs, err := m.repoDemand(ctx, cfg, r, ours, now)
		if err == nil {
			// Read here, before the tick plans anything, so nothing is planned
			// on an answer that is due. A failed read is this poll's failure.
			err = m.readVisibility(ctx, r.Name, now)
		}
		if err != nil {
			missed(r)
			if tokenRejected(err) {
				authFailed = true
			}
			m.apiErr(r.Name, err, now)
			if authFailed || !m.apiAllowed(now) {
				break
			}
			continue
		}
		anyOK = true
		if served(r) {
			demand[r.Name] = jobs
		} else {
			// The read above may have just found the repo public; what an
			// earlier poll left for it goes too.
			delete(demand, r.Name)
		}
		forget(r.Name)
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
	return unanswered == "", unanswered
}
```

Against the function as it stands, four things differ, and only these:

- the comment;
- the new `served` closure;
- `served(r)` where `!r.Paused` stood, in the loop that carries the last known jobs over and in `missed`;
- after a successful poll, `if served(r) { ... } else { delete(demand, r.Name) }` where `if !r.Paused { demand[r.Name] = jobs }` stood.

The line that decides whether a repository is polled, `if r.Paused && !m.unconfirmedBusy(r.Name)`, must keep using the real `Paused`: a blocked repository is polled. `served` calls `m.blocked`, which takes `m.mu`; every place it is called from here runs without the lock.

In `internal/runner/update.go`, in the comment above `startUpdateIfFree`, replace these three lines:

```go
// instance starting or busy, this tick's demand poll (made with cfg, still the
// live config) answered for every unpaused repo with no queued job, the API
// allowed and not degraded, and the maintenance reservation free. Every idle
```

with:

```go
// instance starting or busy, this tick's demand poll (made with cfg, still the
// live config) answered for every repo that is neither paused nor blocked
// with no queued job, the API allowed and not degraded, and the maintenance
// reservation free. gatherDemand decides which repos count. Every idle
```

- [ ] **Step 5: Run the tests to verify they pass**

Run the command of Step 2 again, with `-count=5` in place of `-count=1` so both rows of the update test come first at least once.
Expected: PASS

- [ ] **Step 6: Run the package suites**

Run: `PATH=/usr/local/go/bin:$PATH timeout 600 go test -race ./internal/runner/ -count=1`
Expected: PASS. No existing test changes. In particular the tests of a paused repository (`TestPausedRepositoryStillConfirmsItsBusyRunner`, `TestPausedRepositoryDropsItsError`, `TestPausingDuringAnAPIPauseStillClearsTheRepository`) and of a failed poll keeping its queued count (`TestQueuedCountSurvivesAFailedPoll`) must still pass unchanged; if one fails, `served` has replaced a use of `r.Paused` it should not have.

Run: `PATH=/usr/local/go/bin:$PATH timeout 600 go test -race ./internal/daemon/ -count=1`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
git add internal/runner/visibility.go internal/runner/manager.go internal/runner/tick.go internal/runner/update.go internal/runner/blocked_test.go
git commit -m "feat(runner): show and report blocked repos"
```

### Task 20: Patching config: the optional number type

**Files:**
- Create: `internal/model/optint.go`
- Test: `internal/model/optint_test.go` (new)

**Interfaces:**
- Consumes: nothing
- Produces: `model.OptInt`, `model.Int`, `model.ClearInt` (Contracts). This task only adds the type. `model.RepoPatch` keeps its `*int` fields until the next task.

**Items:** 38
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

A PATCH body has to tell three cases apart for a repository's `max` and `warm`: the key is absent (leave the value), it is `null` (clear the value, so it is inherited again), or it is a number. A `*int` cannot tell the first two apart. This task adds the value type that can.

- [ ] **Step 1: Write the failing test**

Create `internal/model/optint_test.go`:

```go
package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

// An OptInt field tells three cases apart: the key is absent, it is null, or
// it is a number. Each survives a round trip.
func TestOptIntJSON(t *testing.T) {
	type patch struct {
		Max  OptInt `json:"max,omitzero"`
		Warm OptInt `json:"warm,omitzero"`
	}
	for _, tc := range []struct {
		name  string
		patch patch
		body  string
	}{
		{"absent", patch{}, `{}`},
		{"number", patch{Max: Int(3)}, `{"max":3}`},
		{"zero", patch{Warm: Int(0)}, `{"warm":0}`},
		{"null", patch{Max: ClearInt()}, `{"max":null}`},
		{"both", patch{Max: ClearInt(), Warm: Int(2)}, `{"max":null,"warm":2}`},
	} {
		got, err := json.Marshal(tc.patch)
		if err != nil || string(got) != tc.body {
			t.Errorf("%s: marshalled %s err %v, want %s", tc.name, got, err, tc.body)
		}
		var back patch
		if err := json.Unmarshal([]byte(tc.body), &back); err != nil || !reflect.DeepEqual(back, tc.patch) {
			t.Errorf("%s: decoded %+v err %v, want %+v", tc.name, back, err, tc.patch)
		}
	}
	if !(OptInt{}).IsZero() || Int(0).IsZero() || ClearInt().IsZero() {
		t.Error("only an unset value is zero")
	}
	for _, body := range []string{`{"max":"3"}`, `{"max":1.5}`, `{"warm":true}`} {
		var p patch
		if err := json.Unmarshal([]byte(body), &p); err == nil {
			t.Errorf("%s decoded as %+v", body, p)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/model/ -run TestOptIntJSON -count=1`
Expected: FAIL, the package does not build: `undefined: OptInt`, `undefined: Int`, `undefined: ClearInt`

- [ ] **Step 3: Write the implementation**

Create `internal/model/optint.go`:

```go
package model

import (
	"bytes"
	"encoding/json"
)

// OptInt is an optional number in a PATCH body. It tells three cases apart:
// the key was absent (Set is false), it was null (Set with a nil Value: clear
// the value), or it was a number. Fields of this type carry the omitzero tag,
// so an unset one is left out of the body.
type OptInt struct {
	Set   bool
	Value *int
}

// Int is an OptInt holding n.
func Int(n int) OptInt { return OptInt{Set: true, Value: &n} }

// ClearInt is the OptInt that clears a value; it is written as null.
func ClearInt() OptInt { return OptInt{Set: true} }

func (o OptInt) IsZero() bool { return !o.Set }

func (o OptInt) MarshalJSON() ([]byte, error) {
	if o.Value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(*o.Value)
}

// UnmarshalJSON runs only for a key that is present; the decoder calls it for
// null too.
func (o *OptInt) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		*o = ClearInt()
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*o = Int(n)
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/model/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` first; it must print nothing.

```bash
git add internal/model/optint.go internal/model/optint_test.go
git commit -m "feat(model): add an optional number for patches"
```

### Task 21: Patching config: null, sorted names, new warnings

**Files:**
- Modify: `internal/model/model.go` (`RepoPatch`), `internal/daemon/backend.go` (imports, `update`, the repository loop of `PatchConfig`), `cmd/ghr/cli.go` (`set`)
- Test: `internal/daemon/backend_test.go`, `internal/api/api_test.go`

**Interfaces:**
- Consumes: `model.OptInt`, `model.Int`, `model.ClearInt` (Contracts)
- Produces: `model.RepoPatch.Max` and `.Warm` as `model.OptInt` with the tags `json:"max,omitzero"` and `json:"warm,omitzero"` (Contracts). On the wire: an absent key leaves the value, `null` clears it, a number sets it.

**Items:** 38
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 2 = 5

Three defects in `PATCH /config`, all in `internal/daemon/backend.go`:

- A patch cannot reset a repository's `max` or `warm` to "inherited", because `RepoPatch` holds them as `*int` and `null` decodes to the same nil pointer as an absent key.
- `PatchConfig` ranges over the `repos` map, so with several unknown repositories the one named in the 404 varies from call to call.
- `update` writes every config warning as an event after every change, also the warnings the config already had before the change.

`cmd/ghr/cli_test.go` (`TestMutatingCommands`) already pins the body the CLI sends for `ghr set max darkmem 0` as exactly `{"repos":{"darkmem":{"max":0}}}`. It needs no change and must still pass at the end: it is the check that a patch with only `max` set carries no `warm` key.

- [ ] **Step 1: Write the failing tests**

Append to `internal/daemon/backend_test.go`:

```go
// null clears a repository's max or warm, so the value is inherited again; an
// absent key leaves it alone.
func TestPatchConfigClearsMaxAndWarmOnNull(t *testing.T) {
	b, _, _ := newBackend(t)
	patch := func(body string) *config.Repo {
		t.Helper()
		var p model.ConfigPatch
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatal(err)
		}
		if err := b.PatchConfig(p); err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		return b.Store.Config().Repo("darkcloud")
	}
	patch(`{"repos":{"darkcloud":{"max":3,"warm":2}}}`)
	if r := patch(`{"repos":{"darkcloud":{"paused":true}}}`); r.Max == nil || *r.Max != 3 || r.Warm == nil || *r.Warm != 2 {
		t.Fatalf("absent keys changed max or warm: %+v", r)
	}
	if r := patch(`{"repos":{"darkcloud":{"max":null}}}`); r.Max != nil || r.Warm == nil || *r.Warm != 2 {
		t.Fatalf("max null: max %v warm %v", r.Max, r.Warm)
	}
	if r := patch(`{"repos":{"darkcloud":{"warm":null}}}`); r.Max != nil || r.Warm != nil {
		t.Fatalf("warm null: max %v warm %v", r.Max, r.Warm)
	}
	saved, _, err := config.Load(b.Store.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if r := saved.Repo("darkcloud"); r.Max != nil || r.Warm != nil {
		t.Fatalf("not persisted: max %v warm %v", r.Max, r.Warm)
	}
}

// With several unknown repositories in one patch, the one reported is the
// first in sorted order, whatever order the map yields them in.
func TestPatchConfigReportsUnknownReposInSortedOrder(t *testing.T) {
	b, _, _ := newBackend(t)
	paused := true
	repos := map[string]model.RepoPatch{}
	for _, name := range []string{"zeta", "nope", "alpha", "omega", "beta", "gamma", "delta", "kappa"} {
		repos[name] = model.RepoPatch{Paused: &paused}
	}
	for range 20 {
		err := b.PatchConfig(model.ConfigPatch{Repos: repos})
		if apiStatus(err) != 404 || err.Error() != "unknown repo alpha" {
			t.Fatalf("err %v, want 404 unknown repo alpha", err)
		}
	}
}

// A warning the config already carried is not written as an event again by a
// later change; a warning the change introduces is.
func TestUpdateWritesOnlyNewConfigWarnings(t *testing.T) {
	b, _, _ := newBackend(t)
	warnings := func() []string {
		var out []string
		for _, e := range b.Events.After(0) {
			if e.Level == "warn" && strings.HasPrefix(e.Msg, "config: ") {
				out = append(out, e.Msg)
			}
		}
		return out
	}
	all, prefixes := "all", []string{"proj-"}
	// In all mode darkmem, which sets no max, is unlimited.
	if err := b.PatchConfig(model.ConfigPatch{Mode: &all, Repos: map[string]model.RepoPatch{"darkmem": {CleanupNamePrefixes: &prefixes}}}); err != nil {
		t.Fatal(err)
	}
	first := "config: darkmem: cleanup_name_prefixes can remove a concurrent job's containers when max is not 1"
	if got := warnings(); !reflect.DeepEqual(got, []string{first}) {
		t.Fatalf("after the change that introduced it: %q", got)
	}
	three := 3
	if err := b.PatchConfig(model.ConfigPatch{GlobalMax: &three}); err != nil {
		t.Fatal(err)
	}
	if got := warnings(); len(got) != 1 {
		t.Fatalf("an unchanged warning was written again: %q", got)
	}
	var p model.ConfigPatch
	if err := json.Unmarshal([]byte(`{"repos":{"darkcloud":{"max":4,"cleanup_name_prefixes":["proj-"]}}}`), &p); err != nil {
		t.Fatal(err)
	}
	if err := b.PatchConfig(p); err != nil {
		t.Fatal(err)
	}
	second := "config: darkcloud: cleanup_name_prefixes can remove a concurrent job's containers when max is not 1"
	if got := warnings(); !reflect.DeepEqual(got, []string{first, second}) {
		t.Fatalf("after a change that added a second warning: %q", got)
	}
}
```

These three build the patches that carry `max` or `warm` from JSON, so they compile before the type of those fields changes.

Append to `internal/api/api_test.go`:

```go
// The client leaves an unset max or warm out of the body, and the server
// tells null (clear the value) from an absent key (leave it alone).
func TestPatchConfigMaxAndWarmOverTheWire(t *testing.T) {
	c, b := setup(t)
	err := c.PatchConfig(context.Background(), model.ConfigPatch{Repos: map[string]model.RepoPatch{"darkmem": {Max: model.Int(3)}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{`{"repos":{"darkmem":{"max":null}}}`, `{"repos":{"darkmem":{}}}`} {
		req, _ := http.NewRequest(http.MethodPatch, c.Base+"/config", strings.NewReader(body))
		resp, err := c.HTTP.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("%s: status %d", body, resp.StatusCode)
		}
	}
	sent, null, absent := b.patches[0].Repos["darkmem"], b.patches[1].Repos["darkmem"], b.patches[2].Repos["darkmem"]
	if !reflect.DeepEqual(sent, model.RepoPatch{Max: model.Int(3)}) {
		t.Errorf("the client's patch arrived as %+v", sent)
	}
	if !reflect.DeepEqual(null, model.RepoPatch{Max: model.ClearInt()}) {
		t.Errorf("max null arrived as %+v", null)
	}
	if !reflect.DeepEqual(absent, model.RepoPatch{}) {
		t.Errorf("an empty patch arrived as %+v", absent)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run 'TestPatchConfigClearsMaxAndWarmOnNull|TestPatchConfigReportsUnknownReposInSortedOrder|TestUpdateWritesOnlyNewConfigWarnings' -count=1`
Expected: FAIL, all three:
- `TestPatchConfigClearsMaxAndWarmOnNull` with `max null: max 0x… warm 0x…` (the null left max in place)
- `TestPatchConfigReportsUnknownReposInSortedOrder` with `err unknown repo <a name other than alpha>, want 404 unknown repo alpha`
- `TestUpdateWritesOnlyNewConfigWarnings` with `an unchanged warning was written again`

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/api/ -run TestPatchConfigMaxAndWarmOverTheWire -count=1`
Expected: FAIL, the package does not build: `cannot use model.Int(3) (value of struct type model.OptInt) as *int value in struct literal`

- [ ] **Step 3: Change the patch type**

In `internal/model/model.go`, in `type RepoPatch struct`, replace these two lines:

```go
	Max                 *int      `json:"max,omitempty"`
	Warm                *int      `json:"warm,omitempty"`
```

with:

```go
	Max                 OptInt    `json:"max,omitzero"`
	Warm                OptInt    `json:"warm,omitzero"`
```

The tag must be `omitzero`, not `omitempty`: `omitempty` never leaves a struct out, so an unset field would be written as `null` and would clear the value on the server.

- [ ] **Step 4: Apply the patch in the daemon**

In `internal/daemon/backend.go`, add `"maps"` to the import block, between `"io"` and `"net/http"`.

Replace the whole `update` method, including its comment, with:

```go
// update applies fn through the store (validate, save, swap) and wakes the loop.
// fn works on a copy, so a rejected change leaves no partial state anywhere.
// A warning is written as an event only when the change introduced it.
func (b *Backend) update(fn func(c *config.Config) error) error {
	var before []string
	warnings, err := b.Store.Update(func(c *config.Config) error {
		// Until fn runs, c is a copy of the config this update replaces.
		before, _ = c.Validate()
		return fn(c)
	})
	if err != nil {
		var ae *api.Error
		if errors.As(err, &ae) {
			return err
		}
		return api.BadRequest(err.Error())
	}
	for _, w := range warnings {
		if !slices.Contains(before, w) {
			b.Events.Add("warn", "", "config: %s", w)
		}
	}
	if b.Wake != nil {
		b.Wake()
	}
	return nil
}
```

`Store.Update` calls its function with a fresh copy of the live config while it holds the store's lock, so `before` is exactly the warnings of the config this update replaces, with no window for another update in between. `Validate` only reads the config.

In `PatchConfig`, replace these lines:

```go
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
```

with:

```go
		// Sorted, so the unknown repository reported is always the same one.
		for _, name := range slices.Sorted(maps.Keys(p.Repos)) {
			rp := p.Repos[name]
			r := c.Repo(name)
			if r == nil {
				return api.NotFound("unknown repo " + name)
			}
			if rp.Max.Set {
				r.Max = rp.Max.Value
			}
			if rp.Warm.Set {
				r.Warm = rp.Warm.Value
			}
```

The rest of the loop body (`Labels`, `CleanupNamePrefixes`, `Paused`) stays as it is. A `null` arrives as `Set` true with a nil `Value`, so the assignment clears the repository's pointer and `config.EffectiveMax` and `config.EffectiveWarm` fall back to the inherited value.

- [ ] **Step 5: Update the other users of the old fields**

In `cmd/ghr/cli.go`, in `set`, replace:

```go
		rp := model.RepoPatch{Max: &n}
		if args[0] == "warm" {
			rp = model.RepoPatch{Warm: &n}
		}
```

with:

```go
		rp := model.RepoPatch{Max: model.Int(n)}
		if args[0] == "warm" {
			rp = model.RepoPatch{Warm: model.Int(n)}
		}
```

The CLI gets no form for clearing a value; that is out of scope.

In `internal/daemon/backend_test.go`, `TestPatchConfig` builds two patches with `Max: &two`. Replace:

```go
	all, two, idle := "all", 2, "10m"
	paused := true
	if err := b.PatchConfig(model.ConfigPatch{Mode: &all, IdleTimeout: &idle, Repos: map[string]model.RepoPatch{"darkmem": {Max: &two, Paused: &paused}}}); err != nil {
```

with:

```go
	all, idle := "all", "10m"
	paused := true
	if err := b.PatchConfig(model.ConfigPatch{Mode: &all, IdleTimeout: &idle, Repos: map[string]model.RepoPatch{"darkmem": {Max: model.Int(2), Paused: &paused}}}); err != nil {
```

and, in the same test, replace:

```go
	if err := b.PatchConfig(model.ConfigPatch{Repos: map[string]model.RepoPatch{"nope": {Max: &two}}}); apiStatus(err) != 404 {
```

with:

```go
	if err := b.PatchConfig(model.ConfigPatch{Repos: map[string]model.RepoPatch{"nope": {Max: model.Int(2)}}}); apiStatus(err) != 404 {
```

Then run `PATH=/usr/local/go/bin:$PATH go vet ./...`. It must print nothing. If it names another place that still builds a `RepoPatch` with `&n` for `Max` or `Warm`, change it to `model.Int(n)` in the same way and name the file in the task report.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/model/ ./internal/api/ ./cmd/ghr/ ./internal/daemon/ -count=1`
Expected: PASS for all four packages, including `TestMutatingCommands` in `cmd/ghr`, unchanged.

- [ ] **Step 7: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` first; it must print nothing.

```bash
git add internal/model/model.go internal/daemon/backend.go internal/daemon/backend_test.go internal/api/api_test.go cmd/ghr/cli.go
git commit -m "fix(daemon): clear max and warm with a null patch"
```

### Task 22: Clearing max and warm on the repository page

**Files:**
- Modify: `web/src/api/types.ts` (`RepoPatch`), `web/src/pages/repository.tsx` (`RepositoryForm`)
- Test: `web/src/pages/repository.test.tsx`

**Interfaces:**
- Consumes: the wire form of `model.RepoPatch.Max` and `.Warm` (Contracts, `model.OptInt`): the daemon leaves the value alone when the key is absent, clears it when the key is `null`, and sets it when the key is a number. TypeScript `RepoPatch.max?: number | null` and `warm?: number | null` (Contracts).
- Produces: nothing a later task names

**Items:** 38
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

How the page works today, which the change relies on:

- `repoValues` reads the repository's config into `loaded`; a `max` or `warm` the repository does not set is `undefined` there, because the daemon leaves an unset value out of `GET /config`.
- `draft` holds only the edited fields. `setNumber` stores `undefined` for an emptied box, so `values = { ...loaded, ...draft }` shows the box empty.
- `changedKeys(draft, loaded)` lists the draft keys whose value differs from `loaded`. An emptied box over a set value is therefore already a changed key; an emptied box over an unset value is not.
- Today the page turns that changed key into the error `Enter a number from 0 to 99` and refuses to save, because the patch could not carry "unset". The daemon now accepts `null` for that.

The change keeps `undefined` as the draft's "empty" and maps it to `null` only in the request body. Everything after the request keeps comparing against `undefined`: the check that the daemon applied the save (`repoValues` of the fresh config gives `undefined` for a cleared value), `settle`, and the fallback that writes the sent values into the cached config.

The `Warm must be <= max` check needs no new logic: with Max emptied, `values.max` is `undefined`, so the check does not apply, which matches the daemon, where only an explicit max above 0 bounds warm.

The Max field's help text ends with `Once set, it stays explicit.`, which stops being true once an emptied box clears the value. The help text loses that sentence and becomes exactly `Leave empty for the default: 1 in queue mode, no limit in all mode.`

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/repository.test.tsx`, change the type import at the top from:

```ts
import type { Config } from "@/api/types"
```

to:

```ts
import type { Config, RepoConfig } from "@/api/types"
```

Replace the whole `applyingRoutes` function, including its comment, with this one. The mock daemon now does what the real one does with a `null`: it clears the value and then leaves the key out of the config.

```ts
// The daemon applies a PATCH, so the config read after it reflects the save.
// A null clears the value, and the daemon then leaves the key out.
function applyingRoutes() {
  let config: Config = fixtures.config
  return routes({
    "GET /api/config": () => config,
    "PATCH /api/config": ({ body }: { body: unknown }) => {
      const patch = (body as { repos: Record<string, Record<string, unknown>> }).repos.darkmem
      const apply = (r: RepoConfig) => Object.fromEntries(Object.entries({ ...r, ...patch }).filter(([, v]) => v !== null)) as unknown as RepoConfig
      config = { ...config, repos: (config.repos ?? []).map((r) => (r.name === "darkmem" ? apply(r) : r)) }
      return noContent()
    },
  })
}
```

The first test, `shows the repo's state, settings and the labels its runners get`, asserts the Max field's old help text. In that test replace:

```ts
    expect(screen.getByText("Leave empty for the default: 1 in queue mode, no limit in all mode. Once set, it stays explicit.")).toBeInTheDocument()
```

with:

```ts
    expect(screen.getByText("Leave empty for the default: 1 in queue mode, no limit in all mode.")).toBeInTheDocument()
```

The existing test `keeps an emptied box empty and refuses to save it` asserts the behaviour this task removes. Replace that whole test:

```ts
  it("keeps an emptied box empty and refuses to save it", async () => {
    const { calls } = mockApi(routes())
    const { user } = renderApp("/repositories/darkmem/settings")
    const max = await screen.findByLabelText("Max")
    await user.clear(max)
    expect(max).toHaveValue(null)
    expect(screen.getByText("Enter a number from 0 to 99")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Fix the highlighted settings first")).length).toBeGreaterThan(0)
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
  })
```

with these two:

```ts
  it("sends null for an emptied box, so the value is inherited again", async () => {
    const { calls } = mockApi(applyingRoutes())
    const { user } = renderApp("/repositories/darkmem/settings")
    const max = await screen.findByLabelText("Max")
    await user.clear(max)
    expect(max).toHaveValue(null)
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
    expect(screen.queryByText("Enter a number from 0 to 99")).toBeNull()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ repos: { darkmem: { max: null } } }))
    await waitFor(() => expect(screen.queryByText("1 unsaved change")).toBeNull())
    // Toasts of earlier tests stay on screen, so this names the field.
    expect(screen.queryAllByText(/Daemon did not apply darkmem\.max/)).toHaveLength(0)
    expect(screen.getByLabelText("Max")).toHaveValue(null)
  })

  it("stops checking warm against max once max is emptied", async () => {
    const { calls } = mockApi(applyingRoutes())
    const { user } = renderApp("/repositories/darkmem/settings")
    const warm = await screen.findByLabelText("Warm")
    await user.clear(warm)
    await user.type(warm, "3")
    expect(screen.getByText("Warm must be <= max")).toBeInTheDocument()
    await user.clear(screen.getByLabelText("Max"))
    expect(screen.queryByText("Warm must be <= max")).toBeNull()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ repos: { darkmem: { max: null, warm: 3 } } }))
  })
```

The fixture's `darkmem` has `max: 2` and `warm: 1`. Both tests wait for the PATCH call itself rather than for the `Repositories saved` toast: toasts raised by earlier tests in this file stay on screen, so that text can already be there.

- [ ] **Step 2: Run the tests to verify they fail**

Run, in `web/`: `timeout 300 npx vitest run src/pages/repository.test.tsx`
Expected: 3 failed, 29 passed.
- `shows the repo's state, settings and the labels its runners get` fails with `Unable to find an element with the text: Leave empty for the default: 1 in queue mode, no limit in all mode.`, because the page still shows the longer help text.
- `sends null for an emptied box, so the value is inherited again` fails at `expect(screen.queryByText("Enter a number from 0 to 99")).toBeNull()`, because the page still shows the error.
- `stops checking warm against max once max is emptied` fails with `expected undefined to deeply equal { repos: { darkmem: { …(2) } } }`, because the save is refused and no PATCH is sent.

- [ ] **Step 3: Allow null in the patch type**

In `web/src/api/types.ts` replace:

```ts
export interface RepoPatch {
  max?: number
  warm?: number
```

with:

```ts
// A null max or warm clears the value, so the repository inherits it again.
export interface RepoPatch {
  max?: number | null
  warm?: number | null
```

The other fields of `RepoPatch` stay as they are.

- [ ] **Step 4: Send null for an emptied box**

All four edits are in `RepositoryForm` in `web/src/pages/repository.tsx`.

Replace these lines, which hold the comment that explains the old error:

```ts
  // RepoPatch cannot unset a value, so emptying a box that holds one is an
  // error, not a change; the empty edit stays so the box does not refill.
  const blank = (key: "max" | "warm") =>
    key in draft && draft[key] === undefined && loaded[key] !== undefined ? "Enter a number from 0 to 99" : ""
  const maxError = blank("max")
  const warmError = blank("warm") || (maxValue !== undefined && maxValue > 0 && warmValue > maxValue ? "Warm must be <= max" : "")
```

with:

```ts
  // An emptied Max is undefined here, so warm has no explicit bound, as in the daemon.
  const warmError = maxValue !== undefined && maxValue > 0 && warmValue > maxValue ? "Warm must be <= max" : ""
```

In `save`, replace:

```ts
    if (maxError || warmError) {
      toast.error("Fix the highlighted settings first")
      return false
    }
    const sent: Values = Object.fromEntries(changed.map((k) => [k, draft[k]]))
    setSaving(true)
    setRejection("")
    try {
      await api.patchConfig({ repos: { [name]: sent as RepoPatch } })
```

with:

```ts
    if (warmError) {
      toast.error("Fix the highlighted settings first")
      return false
    }
    const sent: Values = Object.fromEntries(changed.map((k) => [k, draft[k]]))
    // An emptied Max or Warm box is undefined in the draft; null tells the
    // daemon to clear the value, where a missing key would leave it.
    const patch = Object.fromEntries(changed.map((k) => [k, draft[k] ?? null])) as RepoPatch
    setSaving(true)
    setRejection("")
    try {
      await api.patchConfig({ repos: { [name]: patch } })
```

`sent` keeps `undefined` for a cleared value on purpose. The lines below the request compare it with `repoValues` of the config read back, which is `undefined` for a value the daemon cleared, so a daemon that ignored the `null` is still reported with `Daemon did not apply <repo>.max; is it older than this ghr?`.

Still in `save`, in the `catch` of the config re-read, `sent` is spread into the cached config. `RepoPatch` now admits `null`, which `RepoConfig` does not, so the cast changes. Replace:

```ts
        c && { ...c, repos: c.repos?.map((r) => (r.name === name ? { ...r, ...(sent as RepoPatch) } : r)) ?? null },
```

with:

```ts
        c && { ...c, repos: c.repos?.map((r) => (r.name === name ? { ...r, ...(sent as Partial<RepoConfig>) } : r)) ?? null },
```

`RepoConfig` and `RepoPatch` are both already imported from `@/api/types` at the top of the file.

In the JSX, the Max field no longer has an error of its own, and its help text loses its last sentence. Replace:

```tsx
                help="Leave empty for the default: 1 in queue mode, no limit in all mode. Once set, it stays explicit."
                error={maxError}
                changed={is("max")}
```

with:

```tsx
                help="Leave empty for the default: 1 in queue mode, no limit in all mode."
                changed={is("max")}
```

The Warm field keeps `error={warmError}`.

- [ ] **Step 5: Run the tests to verify they pass**

Run, in `web/`: `timeout 300 npx vitest run src/pages/repository.test.tsx`
Expected: PASS, 32 tests.

- [ ] **Step 6: Lint and type-check**

Run, in `web/`: `timeout 300 npm run lint` and `timeout 300 npm run typecheck`
Expected: both end without an error or a warning. Without the cast change of Step 4, `typecheck` fails in `repository.tsx` with `Type 'null' is not assignable to type 'number | undefined'`.

- [ ] **Step 7: Commit**

```bash
git add web/src/api/types.ts web/src/pages/repository.tsx web/src/pages/repository.test.tsx
git commit -m "feat(web): clear max or warm by emptying its box"
```

### Task 23: Setup is pending until finished

**Files:**
- Modify: `internal/daemon/setup.go` (`configure` and its comment, `finish`, the `setup` struct)
- Test: `internal/daemon/setup_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names

**Items:** 39, 40, 209
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

Two defects and one wrong comment in `internal/daemon/setup.go`:

- `POST /setup/finish` can be repeated after setup, and each repeat with `{"toolchains":"popular"}` queues the popular toolchain set again.
- Only `deploy/setup.sh` creates the `setup-pending` marker (the file at `setup.setupPending`). On an install made without it, nothing records that setup is unfinished, so a finish has nothing to check.
- The comment on `configure` says a second request "gets the 409". It does, but only after waiting on the mutex through the first request's GitHub calls.

The fix: `configure` creates the marker itself, so every install path has it from the moment ghr is configured, and `finish` acts only while the marker exists.

The order of the checks in `finish` after this task: a bad body is still 400, a daemon that is not serving yet is still 409 `ghr is not configured yet; finish GitHub setup first`, then a missing marker is 204 with nothing done, and only then does it act.

`ghr setup finish` (`cmd/ghr/setup.go`) needs no change: it prints `first-run setup finished` on any 204, which a repeated finish still answers.

- [ ] **Step 1: Write the failing tests**

All edits are in `internal/daemon/setup_test.go`.

`TestSetupGitHubConfigures` asserts that the setup state after a successful configure has no pending setup. That is the behaviour this task changes. In that test replace:

```go
	if st != (model.SetupState{Starting: true, Owner: "DarkRaise", WebListen: "0.0.0.0:8080"}) {
		t.Fatalf("state %+v", st)
	}
```

with:

```go
	if st != (model.SetupState{Starting: true, SetupPending: true, Owner: "DarkRaise", WebListen: "0.0.0.0:8080"}) {
		t.Fatalf("state %+v", st)
	}
```

Add this new test after `TestSetupGitHubConfigures`:

```go
// Without its marker configure saves nothing: the marker is what later tells
// an unfinished setup from a finished one.
func TestSetupGitHubFailsWhenItCannotMarkSetupPending(t *testing.T) {
	s, readies := newTestSetup(t, "")
	s.setupPending = filepath.Join(t.TempDir(), "missing", "setup-pending")
	rec := setupRequest(s.routes(s.unconfigured()), http.MethodPost, "/setup/github", `{"owner":"darkraise","token":"good"}`)
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "setup-pending") {
		t.Fatalf("configure: %d %s", rec.Code, rec.Body.String())
	}
	if readies.Load() != 0 || s.store.Configured() || s.store.Config().Owner != "" {
		t.Fatalf("readies %d configured %v owner %q", readies.Load(), s.store.Configured(), s.store.Config().Owner)
	}
	if _, err := os.Stat(s.store.TokenPath); !os.IsNotExist(err) {
		t.Fatal("the token was written")
	}
}
```

`TestSetupFinish` ends its finish checks with a second request that asks for `none`, which queues nothing today either, so it cannot see the defect. In that test replace:

```go
	os.WriteFile(s.setupPending, nil, 0o600)
	os.WriteFile(s.toolchainsPending, nil, 0o600)
	if rec := setupRequest(h, http.MethodPost, "/setup/finish", `{"toolchains":"popular"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("popular: %d %s", rec.Code, rec.Body.String())
	}
	if len(space.calls) != 1 || space.calls[0] != "preset popular" || exists(s.setupPending) || exists(s.toolchainsPending) {
		t.Fatalf("calls %v", space.calls)
	}
	if rec := setupRequest(h, http.MethodPost, "/setup/finish", `{"toolchains":"none"}`); rec.Code != http.StatusNoContent || len(space.calls) != 1 {
		t.Fatalf("none with the markers gone: %d calls %v", rec.Code, space.calls)
	}
```

with:

```go
	popular := func() int {
		return setupRequest(h, http.MethodPost, "/setup/finish", `{"toolchains":"popular"}`).Code
	}
	// With no marker there is no setup to finish: nothing is queued or written.
	if code := popular(); code != http.StatusNoContent || len(space.calls) != 0 || len(s.events.After(0)) != 0 {
		t.Fatalf("no marker: %d calls %v events %v", code, space.calls, s.events.After(0))
	}
	os.WriteFile(s.setupPending, nil, 0o600)
	os.WriteFile(s.toolchainsPending, nil, 0o600)
	if code := popular(); code != http.StatusNoContent {
		t.Fatalf("popular: %d", code)
	}
	if len(space.calls) != 1 || space.calls[0] != "preset popular" || exists(s.setupPending) || exists(s.toolchainsPending) {
		t.Fatalf("calls %v", space.calls)
	}
	if code := popular(); code != http.StatusNoContent || len(space.calls) != 1 || len(s.events.After(0)) != 1 {
		t.Fatalf("a second finish: %d calls %v events %v", code, space.calls, s.events.After(0))
	}
```

The lines of `TestSetupFinish` before and after this block stay as they are.

Also append this test to `internal/daemon/setup_test.go` (`sync` is already imported there). Eight finishes arriving together must queue the popular set once and write one event:

```go
func TestSetupFinishActsOnceUnderConcurrentRequests(t *testing.T) {
	s, _ := newTestSetup(t, "DarkRaise")
	space := &fakeStorage{}
	s.backend.Store(&Backend{Space: space, Events: s.events})
	h := s.routes(http.NotFoundHandler())
	os.WriteFile(s.setupPending, nil, 0o600)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if rec := setupRequest(h, http.MethodPost, "/setup/finish", `{"toolchains":"popular"}`); rec.Code != http.StatusNoContent {
				t.Errorf("finish: %d %s", rec.Code, rec.Body.String())
			}
		}()
	}
	close(start)
	wg.Wait()
	if len(space.calls) != 1 || len(s.events.After(0)) != 1 {
		t.Fatalf("calls %v events %v, want one of each", space.calls, s.events.After(0))
	}
}
```

Run this one with the race detector: `PATH=/usr/local/go/bin:$PATH timeout 300 go test -race ./internal/daemon/ -run TestSetupFinishActsOnceUnderConcurrentRequests -count=5`. Before the implementation it fails with `WARNING: DATA RACE` or with `calls [preset popular preset popular] ... want one of each`; after it, it passes.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run 'TestSetup' -count=1`
Expected: FAIL, four tests. `TestSetupFinishActsOnceUnderConcurrentRequests` fails with several `preset popular` calls and events where one of each is wanted. The other three:
- `TestSetupGitHubConfigures` with `state {Configured:false Starting:true SetupPending:false ...}`
- `TestSetupGitHubFailsWhenItCannotMarkSetupPending` with `configure: 204`
- `TestSetupFinish` with `no marker: 204 calls [preset popular] events [...first-run setup finished...]`

- [ ] **Step 3: Create the marker in configure**

In `internal/daemon/setup.go`, replace the comment on `configure`:

```go
// configure validates owner and token against GitHub, saves them and starts
// the manager. One request at a time, so a second one gets the 409.
```

with:

```go
// configure validates owner and token against GitHub, marks setup pending,
// saves them and starts the manager. One request at a time: a second one
// waits for the first, GitHub calls included, and then gets the 409.
```

Inside `configure`, replace:

```go
	login, err := s.check(ctx, owner, token)
	if err != nil {
		return err
	}
	var mismatch *OwnerMismatchError
```

with:

```go
	login, err := s.check(ctx, owner, token)
	if err != nil {
		return err
	}
	// Created before anything is saved, so no install is ever configured
	// without it. One left behind by a failed save is harmless: an
	// unconfigured daemon shows the wizard anyway.
	if err := os.WriteFile(s.setupPending, nil, 0o600); err != nil {
		return fmt.Errorf("mark setup pending: %w", err)
	}
	var mismatch *OwnerMismatchError
```

The marker is written after every refusal `configure` can give (owner syntax, empty token, already configured, another owner, the GitHub check), so a refused request leaves no marker, and before `s.store.Configure`, which is what saves the owner and the token. On an install made by `deploy/setup.sh` the file already exists and is empty; writing it again changes nothing. `os` and `fmt` are already imported.

- [ ] **Step 4: Make finish act only while setup is pending**

In `finish`, in the same file, replace:

```go
		writeSocketError(w, http.StatusConflict, "ghr is not configured yet; finish GitHub setup first")
		return
	}
	if req.Toolchains == "popular" {
```

with:

```go
		writeSocketError(w, http.StatusConflict, "ghr is not configured yet; finish GitHub setup first")
		return
	}
	s.finishMu.Lock()
	defer s.finishMu.Unlock()
	// Setup is finished only once: without the marker there is nothing to do.
	if !exists(s.setupPending) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if req.Toolchains == "popular" {
```

The lock makes the marker check and what follows it one step, so two requests arriving together cannot both see the marker. Add its field to the `setup` struct, in the same file. Replace:

```go
	backend atomic.Pointer[Backend]
	mu      sync.Mutex
}
```

with:

```go
	backend atomic.Pointer[Backend]
	mu      sync.Mutex
	// finishMu makes a finish check the marker and act on it as one step.
	finishMu sync.Mutex
}
```

The rest of `finish` stays as it is: it queues the popular set when asked, removes both markers and writes the `first-run setup finished` event.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run 'TestSetup' -count=1`
Expected: PASS

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test -race ./internal/daemon/ -run TestSetupFinishActsOnceUnderConcurrentRequests -count=5`
Expected: PASS, with no `WARNING: DATA RACE`

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ ./cmd/ghr/ -count=1`
Expected: PASS for both packages. `TestSetupPhase` (a finish before ghr is configured is still 409) and `TestRunReportsAPendingSetup` pass unchanged.

- [ ] **Step 6: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` first; it must print nothing.

```bash
git add internal/daemon/setup.go internal/daemon/setup_test.go
git commit -m "fix(daemon): keep setup pending until finished"
```

### Task 24: Checking a new token

**Files:**
- Modify: `internal/daemon/backend.go` (`Backend` struct, `SetToken`, new `checkTokenOn` and `judgeToken`), `internal/daemon/setup.go` (`check`, new `ownerRepos`), `internal/daemon/run.go` (`start`)
- Test: `internal/daemon/backend_test.go`, `internal/daemon/run_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `Backend.CheckTokenOwner func(ctx context.Context, token string) error` (Contracts)

**Items:** 23
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 2 = 5

Today `Backend.SetToken` (behind `PUT /token`, `ghr token set` and the web token dialog) checks nothing when no runner repository is configured, checks only `Repos[0]` otherwise, and does not wake the run loop. If `Repos[0]` no longer exists on GitHub, no token can be accepted.

After this task:

- With at least one runner repository that is not being removed, `SetToken` runs the existing `CheckToken` on each of them, four at a time, within the request's context. Paused repositories are included: a token rotated while everything is paused must still be checked for the permissions the runners need.
- The results are judged in this order, and the first rule that applies decides:
  1. a 401 from any repository refuses the token with status 400 and `new token rejected: <GitHub's message>`, where the message is the `*github.APIError`'s own text, such as `github: 401 Bad credentials`;
  2. a rate limit from any repository refuses with status 429, the `*github.APIError`'s text and its retry time;
  3. every repository failing refuses the token with status 400 and `new token rejected: <repo>: <failure>`, for the first repository in config order;
  4. otherwise the token is saved, and each failing repository gets a warning event `new token: <repo>: <failure>`.
- With no such repository, the check is the one first-run setup makes: the token must list at least one repository of the owner. `Backend` gets an injected `CheckTokenOwner` for it, and its refusal is returned unchanged.
- Nothing is saved when the token is refused. After saving, `SetToken` calls `Wake`.

A 401 is recognised with `errors.As` on `*github.APIError`, because `checkToken` wraps GitHub's error with the permission it names. The test fake in `backend_test.go` returns a plain error today, which `errors.As` cannot match, so it changes too.

The callers of `PUT /token` need no change: the API server passes the error through `respond`, which already writes an `*api.Error`'s status and `retry_at`; `ghr token set` and the web token dialog print whatever message comes back.

- [ ] **Step 1: Write the failing tests**

In `internal/daemon/backend_test.go`, add `"fmt"` to the import block (after `"errors"`) if it is not there yet.

In `newBackend`, in the same file, the `CheckToken` fake returns a plain error. Replace:

```go
			if token == "bad" {
				return errors.New("401 Bad credentials")
			}
```

with:

```go
			if token == "bad" {
				return &github.APIError{Status: 401, Kind: github.ErrAuth, Message: "Bad credentials"}
			}
```

Append to `internal/daemon/backend_test.go`:

```go
// A new token is checked against every runner repository, and the results
// are judged in a fixed order: a 401, then a rate limit, then every
// repository failing; anything else saves the token with a warning per
// failing repository.
func TestSetTokenJudgesEveryRepository(t *testing.T) {
	const hint = "listing runners failed; the token needs Administration: read/write: "
	unauthorized := fmt.Errorf(hint+"%w", &github.APIError{Status: 401, Kind: github.ErrAuth, Message: "Bad credentials"})
	forbidden := fmt.Errorf(hint+"%w", &github.APIError{Status: 403, Kind: github.ErrAuth, Message: "Resource not accessible by personal access token"})
	retry := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	limited := fmt.Errorf(hint+"%w", &github.APIError{Status: 403, Kind: github.ErrRateLimit, Message: "API rate limit exceeded", RetryAt: retry})
	const denied = hint + "github: 403 Resource not accessible by personal access token"
	for _, tc := range []struct {
		name     string
		results  map[string]error // by repository; one not named passes
		status   int              // 0 means the token is saved
		msg      string
		warnings []string // warning events of a saved token
	}{
		{"every repository passes", nil, 0, "", nil},
		{"a 401 from one", map[string]error{"darkmem": unauthorized}, 400, "new token rejected: github: 401 Bad credentials", nil},
		{"a 401 before a rate limit", map[string]error{"darkcloud": limited, "darkmem": unauthorized}, 400, "new token rejected: github: 401 Bad credentials", nil},
		{"a rate limit from one", map[string]error{"darkmem": limited}, 429, "github: 403 API rate limit exceeded", nil},
		{"a rate limit before every repository failing", map[string]error{"darkcloud": forbidden, "darkmem": limited}, 429, "github: 403 API rate limit exceeded", nil},
		{"every repository fails", map[string]error{"darkcloud": forbidden, "darkmem": errors.New("no route to host")}, 400, "new token rejected: darkcloud: " + denied, nil},
		{"one of two fails", map[string]error{"darkmem": forbidden}, 0, "", []string{"new token: darkmem: " + denied}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, m, gh := newBackend(t)
			woke := 0
			b.Wake = func() { woke++ }
			var mu sync.Mutex
			var checked []string
			b.CheckToken = func(ctx context.Context, token, repo string) error {
				mu.Lock()
				checked = append(checked, repo)
				mu.Unlock()
				return tc.results[repo]
			}
			err := b.SetToken(context.Background(), "tok2")
			slices.Sort(checked)
			if !reflect.DeepEqual(checked, []string{"darkcloud", "darkmem"}) {
				t.Fatalf("checked %v", checked)
			}
			var warnings []string
			for _, e := range b.Events.After(0) {
				if e.Level == "warn" {
					warnings = append(warnings, e.Msg)
				}
			}
			if tc.status != 0 {
				var ae *api.Error
				if !errors.As(err, &ae) || ae.Status != tc.status || ae.Msg != tc.msg || (tc.status == 429) != ae.RetryAt.Equal(retry) {
					t.Fatalf("err %#v", err)
				}
				if b.Store.Token() != "tok1" || gh.forgot || m.cleared || woke != 0 || len(b.Events.After(0)) != 0 {
					t.Fatalf("a refused token changed something: token %q forgot %v cleared %v woke %d", b.Store.Token(), gh.forgot, m.cleared, woke)
				}
				return
			}
			if err != nil || b.Store.Token() != "tok2" || !gh.forgot || !m.cleared || woke != 1 {
				t.Fatalf("err %v token %q forgot %v cleared %v woke %d", err, b.Store.Token(), gh.forgot, m.cleared, woke)
			}
			if !reflect.DeepEqual(warnings, tc.warnings) {
				t.Fatalf("warnings %q, want %q", warnings, tc.warnings)
			}
		})
	}
}

// A paused repository is checked like any other: a token rotated while
// everything is paused still needs the runners' permissions. A repository
// being removed is not checked.
func TestSetTokenChecksPausedButNotRemovingRepos(t *testing.T) {
	b, _, _ := newBackend(t)
	paused := true
	if err := b.PatchConfig(model.ConfigPatch{Repos: map[string]model.RepoPatch{"darkmem": {Paused: &paused}}}); err != nil {
		t.Fatal(err)
	}
	if err := b.RemoveRepo("darkcloud"); err != nil {
		t.Fatal(err)
	}
	var checked []string
	b.CheckToken = func(ctx context.Context, token, repo string) error {
		checked = append(checked, repo)
		return nil
	}
	if err := b.SetToken(context.Background(), "tok2"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(checked, []string{"darkmem"}) {
		t.Fatalf("checked %v", checked)
	}
}

// With no runner repository to check against, the token must pass the owner
// check, and its refusal is returned as it is.
func TestSetTokenWithoutRunnerReposUsesTheOwnerCheck(t *testing.T) {
	b, _, _ := newBackend(t)
	for _, name := range []string{"darkcloud", "darkmem"} {
		if err := b.RemoveRepo(name); err != nil {
			t.Fatal(err)
		}
	}
	b.FinalizeRemovals()
	if n := len(b.Store.Config().Repos); n != 0 {
		t.Fatalf("%d repositories left", n)
	}
	woke := 0
	b.Wake = func() { woke++ }
	b.CheckToken = func(ctx context.Context, token, repo string) error {
		t.Errorf("checked against %s", repo)
		return nil
	}
	refusal := api.BadRequest("the token cannot see any repository of darkraise; grant it access to at least one")
	b.CheckTokenOwner = func(ctx context.Context, token string) error {
		if token == "blind" {
			return refusal
		}
		return nil
	}
	if err := b.SetToken(context.Background(), "blind"); err != refusal || b.Store.Token() != "tok1" || woke != 0 {
		t.Fatalf("err %v token %q woke %d", err, b.Store.Token(), woke)
	}
	if err := b.SetToken(context.Background(), "tok2"); err != nil || b.Store.Token() != "tok2" || woke != 1 {
		t.Fatalf("err %v token %q woke %d", err, b.Store.Token(), woke)
	}
}
```

The config these tests start from (`cfgYAML` in `store_test.go`) has the runner repositories `darkcloud` and `darkmem`, in that order, and the token `tok1`.

`CheckTokenOwner` is wired in `start`, which only a running daemon exercises, so one test goes in `internal/daemon/run_test.go`. Append:

```go
// With no runner repository configured, the running daemon checks a
// replacement token by listing the owner's repositories with it.
func TestRunChecksANewTokenWithoutRunnerRepos(t *testing.T) {
	o := testOptions(t, "owner: DarkRaise\nlabels: [homelab]\n")
	o.GitHubURL = fakeSetupGitHub(t).URL
	startRun(t, o)
	waitFor(t, "the full API", func() bool { st, ok := setupStateOver(o.Socket); return ok && st.Configured })
	code, body, err := socketCall(o.Socket, http.MethodPut, "/token", "other")
	if err != nil || code != http.StatusBadRequest ||
		!strings.Contains(body, "the token cannot see any repository of DarkRaise; grant it access to at least one") {
		t.Fatalf("a token that sees no repository of the owner: %d %s %v", code, body, err)
	}
	if data, _ := os.ReadFile(o.TokenPath); string(data) != "tok\n" {
		t.Fatalf("the refused token was saved: %q", data)
	}
	if code, body, err := socketCall(o.Socket, http.MethodPut, "/token", "good"); err != nil || code != http.StatusNoContent {
		t.Fatalf("a token that sees one: %d %s %v", code, body, err)
	}
	if data, _ := os.ReadFile(o.TokenPath); string(data) != "good\n" {
		t.Fatalf("token file %q", data)
	}
}
```

`fakeSetupGitHub` (in `setup_test.go`) answers `/user/repos` by bearer token: `good` sees `DarkRaise/Alpha` and `DarkRaise/darkmem`, `other` sees only `someone-else/x`.

- [ ] **Step 2: Run the tests to verify they do not build**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run 'TestSetToken|TestRunChecksANewToken' -count=1`
Expected: FAIL, the package does not build: `b.CheckTokenOwner undefined (type *Backend has no field or method CheckTokenOwner)`

- [ ] **Step 3: Add the owner check and wire it**

In `internal/daemon/backend.go`, in `type Backend struct`, add the field right after `CheckToken`:

```go
	// CheckTokenOwner validates a candidate token when there is no runner
	// repository to read: the token must list at least one repository of the owner.
	CheckTokenOwner func(ctx context.Context, token string) error
```

In `internal/daemon/setup.go`, replace the whole `check` method, including its comment, with the following method and function. The listing step moves out unchanged, so first-run setup behaves as before:

```go
// check lists the repositories the token can see and runs checkToken on the
// first of owner's, returning owner's login as GitHub spells it.
func (s *setup) check(ctx context.Context, owner, token string) (string, error) {
	c := github.New(owner, func() string { return token })
	if s.githubURL != "" {
		c.BaseURL = s.githubURL
	}
	mine, err := ownerRepos(ctx, c, owner)
	if err != nil {
		return "", err
	}
	first := mine[0]
	if err := checkToken(ctx, c, first.Name); err != nil {
		return "", setupGitHubError(err, owner+"/"+first.Name)
	}
	return first.Owner.Login, nil
}

// ownerRepos lists the repositories of owner that c's token can see, sorted
// by name, and fails when there is none. Its errors are ready to answer a
// request with.
func ownerRepos(ctx context.Context, c *github.Client, owner string) ([]github.UserRepo, error) {
	repos, err := c.ListUserRepos(ctx)
	if err != nil {
		return nil, setupGitHubError(err, "")
	}
	var mine []github.UserRepo
	for _, r := range repos {
		if strings.EqualFold(r.Owner.Login, owner) {
			mine = append(mine, r)
		}
	}
	if len(mine) == 0 {
		return nil, api.BadRequest(fmt.Sprintf("the token cannot see any repository of %s; grant it access to at least one", owner))
	}
	sort.Slice(mine, func(i, j int) bool { return strings.ToLower(mine[i].Name) < strings.ToLower(mine[j].Name) })
	return mine, nil
}
```

In `internal/daemon/run.go`, in `start`, the `Backend` literal sets `CheckToken`. Replace:

```go
			return checkToken(ctx, c, repo)
		},
		Sampler:          sampler,
```

with:

```go
			return checkToken(ctx, c, repo)
		},
		CheckTokenOwner: func(ctx context.Context, token string) error {
			c := github.New(owner, func() string { return token })
			c.BaseURL = gh.BaseURL
			_, err := ownerRepos(ctx, c, owner)
			return err
		},
		Sampler:          sampler,
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run 'TestSetToken|TestRunChecksANewToken|TestSetup' -count=1`
Expected: every `TestSetup…` test passes (the listing step only moved), and these fail:
- `TestSetTokenJudgesEveryRepository`, all seven cases, with `checked [darkcloud]`
- `TestSetTokenChecksPausedButNotRemovingRepos` with `checked [darkcloud]`
- `TestSetTokenWithoutRunnerReposUsesTheOwnerCheck` with `err <nil> token "blind" woke 0`
- `TestRunChecksANewTokenWithoutRunnerRepos` with `a token that sees no repository of the owner: 204`

- [ ] **Step 5: Write the implementation**

In `internal/daemon/backend.go`, replace the whole `SetToken` method with the following constant, method and two helpers:

```go
// tokenCheckWorkers bounds how many repositories a new token is checked
// against at once.
const tokenCheckWorkers = 4

// SetToken replaces the token after checking it against every runner
// repository that is not being removed. Paused repositories are included: a
// token rotated while everything is paused still needs the permissions the
// runners use. The first rule that applies decides: a 401 from any repository
// refuses the token; a rate limit from any refuses with 429 and its retry
// time; every repository failing refuses with the first one's failure;
// otherwise the token is saved and each failing repository gets a warning
// event. With no repository to check against, the token must pass the owner
// check. A refused token changes nothing.
func (b *Backend) SetToken(ctx context.Context, token string) error {
	var repos []string
	for _, r := range b.Store.Config().Repos {
		if !r.Removing {
			repos = append(repos, r.Name)
		}
	}
	var errs []error
	if len(repos) == 0 {
		if err := b.CheckTokenOwner(ctx, token); err != nil {
			return err
		}
	} else {
		errs = b.checkTokenOn(ctx, token, repos)
		if err := judgeToken(repos, errs); err != nil {
			return err
		}
	}
	if err := b.Store.SetToken(token); err != nil {
		return err
	}
	b.GH.ForgetCache()
	b.dropLabelChecks()
	b.M.ClearDegraded()
	b.Events.Add("ok", "", "GitHub token replaced")
	for i, err := range errs {
		if err != nil {
			b.Events.Add("warn", "", "new token: %s: %v", repos[i], err)
		}
	}
	if b.Wake != nil {
		b.Wake()
	}
	return nil
}

// checkTokenOn runs CheckToken for every repository, tokenCheckWorkers at a
// time, and returns the results in the order of repos.
func (b *Backend) checkTokenOn(ctx context.Context, token string, repos []string) []error {
	errs := make([]error, len(repos))
	sem := make(chan struct{}, tokenCheckWorkers)
	var wg sync.WaitGroup
	for i, repo := range repos {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			errs[i] = b.CheckToken(ctx, token, repo)
		}()
	}
	wg.Wait()
	return errs
}

// judgeToken returns the refusal for a new token given each repository's
// check result, or nil when the token is to be saved. errors.As is needed
// because checkToken wraps GitHub's error with the permission it names.
func judgeToken(repos []string, errs []error) error {
	var ae *github.APIError
	for _, err := range errs {
		if errors.As(err, &ae) && ae.Status == http.StatusUnauthorized {
			return api.BadRequest("new token rejected: " + ae.Error())
		}
	}
	failed := 0
	for _, err := range errs {
		if errors.As(err, &ae) && ae.Kind == github.ErrRateLimit {
			return &api.Error{Status: http.StatusTooManyRequests, Msg: ae.Error(), RetryAt: ae.RetryAt}
		}
		if err != nil {
			failed++
		}
	}
	if failed == len(repos) {
		return api.BadRequest("new token rejected: " + repos[0] + ": " + errs[0].Error())
	}
	return nil
}
```

Everything this code uses is already imported in `backend.go` (`errors`, `net/http`, `sync`, `api`, `github`). The warning events carry the repository in their text and leave the event's own repository field empty, so the text reads the same wherever it is shown.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test -race ./internal/daemon/ -run 'TestSetToken|TestRunChecksANewToken|TestSetup' -count=1`
Expected: PASS, with no data race reported.

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ ./internal/api/ ./cmd/ghr/ -count=1`
Expected: PASS for all three packages. `TestPauseAllStepsKillToken` (which sets the token `bad` and expects a 400) passes with the changed fake, through rule 1.

- [ ] **Step 7: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` first; it must print nothing.

```bash
git add internal/daemon/backend.go internal/daemon/backend_test.go internal/daemon/setup.go internal/daemon/run.go internal/daemon/run_test.go
git commit -m "fix(daemon): check a new token on every repository"
```

### Task 25: Reload with the same token

**Files:**
- Modify: `internal/daemon/store.go` (`OpenStore`, `Reload`), `internal/daemon/backend.go` (`Reload`), `internal/daemon/run.go` (the reload case of the unconfigured phase in `Run`)
- Test: `internal/daemon/store_test.go`, `internal/daemon/backend_test.go`, `internal/daemon/labelcheck_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `func (s *Store) Reload() (warnings []string, tokenChanged bool, err error)` (Contracts)

**Items:** 25
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

Today every reload (`POST /reload`, `ghr reload`, SIGHUP) calls the GitHub client's `ForgetCache`. That drops the client's ETag cache, the token's metadata, any rate-limit suspension and, through the token generation it bumps, every label check, also when only `config.yaml` changed and the token is the same.

After this task `Store.Reload` also reports whether the token changed, and `Backend.Reload` calls `ForgetCache` only then. `ClearDegraded` still runs on every reload, so a reload remains the way to say "try the token again now". `dropLabelChecks` also still runs on every reload: with an unchanged token generation it drops only the checks of repositories that left the config.

The other callers of `Store.Reload` take the extra result and ignore it: `OpenStore`, the reload case of the unconfigured phase in `run.go`, and the existing calls in `store_test.go`.

- [ ] **Step 1: Write the failing tests for the backend**

`TestReloadAppliesWakesAndRejects` in `internal/daemon/backend_test.go` asserts that a reload with an unchanged token forgets the client's cache. That is the behaviour this task changes. In that test replace:

```go
	ws, err := b.Reload()
	if err != nil || ws == nil || woke != 1 || !gh.forgot || !m.cleared {
		t.Fatalf("reload: %v %v woke=%d forgot=%v cleared=%v", ws, err, woke, gh.forgot, m.cleared)
	}
	if err := os.WriteFile(b.Store.TokenPath, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Reload(); apiStatus(err) != 400 || woke != 1 {
		t.Fatalf("empty token: %v woke=%d", err, woke)
	}
```

with:

```go
	ws, err := b.Reload()
	if err != nil || ws == nil || woke != 1 || !m.cleared {
		t.Fatalf("reload: %v %v woke=%d cleared=%v", ws, err, woke, m.cleared)
	}
	// The token did not change, so what the client holds for it stays.
	if gh.forgot {
		t.Fatal("a reload with the same token dropped the GitHub client's cache")
	}
	m.cleared = false
	if err := os.WriteFile(b.Store.TokenPath, []byte("tok2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Reload(); err != nil || woke != 2 || !gh.forgot || !m.cleared {
		t.Fatalf("reload with a new token: %v woke=%d forgot=%v cleared=%v", err, woke, gh.forgot, m.cleared)
	}
	if err := os.WriteFile(b.Store.TokenPath, []byte("  \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Reload(); apiStatus(err) != 400 || woke != 2 {
		t.Fatalf("empty token: %v woke=%d", err, woke)
	}
```

The rest of the test stays. The test's fake client sets `gh.forgot` in `ForgetCache`, which is the only call that drops the real client's ETag cache and rate-limit suspension.

`TestLabelCheckCancelledEagerlyOnTokenChange` in `internal/daemon/labelcheck_test.go` has a `reload` case that reloads without changing the token and expects the running scan to be cancelled. A reload cancels a scan only when it brings a new token, so that case must write one first. In `labelcheck_test.go`, add `"os"` to the import block (after `"errors"`), and in that test replace:

```go
			} else {
				_, err = b.Reload()
			}
```

with:

```go
			} else {
				if err := os.WriteFile(b.Store.TokenPath, []byte("tok2\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				_, err = b.Reload()
			}
```

Add this new test right after `TestLabelCheckCancelledEagerlyOnTokenChange`:

```go
// A reload that keeps the token keeps a running scan: what it reads is read
// with the token still in use.
func TestLabelCheckSurvivesAReloadWithTheSameToken(t *testing.T) {
	b, _, gh := newBackend(t)
	labelGH(gh)
	gh.block = true
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatal(err)
	}
	gh.scanCounts(t, func(in, _ int) bool { return in == 1 })
	if _, err := b.Reload(); err != nil {
		t.Fatal(err)
	}
	if lc, _ := b.LabelCheck("darkcloud"); lc.State != "checking" {
		t.Fatalf("the scan was dropped: %+v", lc)
	}
	gh.scanCounts(t, func(in, c int) bool { return in == 1 && c == 0 })
	b.Close()
	gh.scanCounts(t, func(in, c int) bool { return in == 0 && c == 1 })
}
```

`scanCounts` is the package's existing helper that waits for the fake's counters; `b.Close()` ends the blocked scan so the test leaves no goroutine behind.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run 'TestReloadAppliesWakesAndRejects|TestLabelCheckCancelledEagerlyOnTokenChange|TestLabelCheckSurvivesAReloadWithTheSameToken' -count=1`
Expected: FAIL for two of the three:
- `TestReloadAppliesWakesAndRejects` with `a reload with the same token dropped the GitHub client's cache`
- `TestLabelCheckSurvivesAReloadWithTheSameToken` with `the scan was dropped: {State:not_checked ...}`

`TestLabelCheckCancelledEagerlyOnTokenChange` passes before and after this task; its change keeps it meaningful once a same-token reload stops cancelling scans.

- [ ] **Step 3: Write the failing test for the store**

In `internal/daemon/store_test.go`, add this test before `TestStoreConfigure`:

```go
// Reload reports a token change only when the token file holds another token
// than the one in use, and never for a reload it rejects.
func TestStoreReloadReportsATokenChange(t *testing.T) {
	s := newStore(t)
	reload := func(want bool, what string) {
		t.Helper()
		if _, changed, err := s.Reload(); err != nil || changed != want {
			t.Fatalf("%s: changed %v err %v", what, changed, err)
		}
	}
	reload(false, "the same token")
	os.WriteFile(s.TokenPath, []byte("  tok1\n\n"), 0o600)
	reload(false, "the same token with other whitespace")
	os.WriteFile(s.TokenPath, []byte("tok2\n"), 0o600)
	reload(true, "a new token")
	reload(false, "the new token again")
	os.WriteFile(s.TokenPath, []byte("tok3\n"), 0o600)
	os.WriteFile(s.ConfigPath, []byte("mode: fast\n"), 0o600)
	if _, changed, err := s.Reload(); err == nil || changed || s.Token() != "tok2" {
		t.Fatalf("a rejected reload: changed %v err %v token %q", changed, err, s.Token())
	}

	first := unconfiguredStore(t, "darkraise", nil)
	if _, changed, err := first.Reload(); err != nil || changed {
		t.Fatalf("no token yet: changed %v err %v", changed, err)
	}
	os.WriteFile(first.TokenPath, []byte("tok1\n"), 0o600)
	if _, changed, err := first.Reload(); err != nil || !changed {
		t.Fatalf("the first token: changed %v err %v", changed, err)
	}
}
```

`newStore` opens a store whose token file holds `tok1`; `unconfiguredStore(t, "darkraise", nil)` opens one with an owner and no token file.

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run TestStoreReload -count=1`
Expected: FAIL, the package does not build: `assignment mismatch: 3 variables but s.Reload returns 2 values`

- [ ] **Step 4: Report the token change from the store**

In `internal/daemon/store.go`, replace the whole `Reload` method, including its comment, with:

```go
// Reload re-reads config and token; on any error the previous values stay active.
// The owner cannot change while the daemon runs: the GitHub client and every
// live runner belong to the owner it started with. A first owner may be set.
// tokenChanged reports whether the token now in use differs from the one
// before the call, so a caller can keep what it fetched with an unchanged one.
func (s *Store) Reload() (warnings []string, tokenChanged bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg, warnings, err := config.Load(s.ConfigPath)
	if err != nil {
		return nil, false, err
	}
	if cur := s.cfg.Load(); cur != nil && cur.Owner != "" && cur.Owner != cfg.Owner {
		return nil, false, fmt.Errorf("owner changed from %s to %s: restart ghr to switch owners", cur.Owner, cfg.Owner)
	}
	tok, err := s.readToken()
	if err != nil {
		return nil, false, err
	}
	s.cfg.Store(cfg)
	if tok != "" && tok != s.Token() {
		s.token.Store(&tok)
		tokenChanged = true
	}
	return warnings, tokenChanged, nil
}
```

`readToken` already trims the file's content, so a token file that differs only in whitespace is the same token.

In `OpenStore`, in the same file, replace:

```go
	warnings, err := s.Reload()
```

with:

```go
	warnings, _, err := s.Reload()
```

In `internal/daemon/run.go`, in `Run`, in the `case <-reload:` of the loop that waits while ghr is unconfigured, replace:

```go
				ws, err := store.Reload()
```

with:

```go
				ws, _, err := store.Reload()
```

In `internal/daemon/store_test.go`, six existing calls take two results. They are in `TestStoreUpdateReloadToken` (two) and `TestStoreReloadSetsTheFirstOwnerAndToken` (four), and each starts with `if _, err := s.Reload(); `. Change that start to `if _, _, err := s.Reload(); ` on all six lines and leave the rest of each line as it is. One command does it:

```bash
sed -i 's/if _, err := s\.Reload(); /if _, _, err := s.Reload(); /' internal/daemon/store_test.go
```

- [ ] **Step 5: Forget the client's cache only for a new token**

In `internal/daemon/backend.go`, in `Reload`, replace the method's comment and its first line:

```go
// Reload re-reads config.yaml and the token file, as SIGHUP does. On any
// error the previous values stay active. It wakes the run loop, which ticks;
// it never ticks itself.
func (b *Backend) Reload() ([]string, error) {
	warnings, err := b.Store.Reload()
```

with:

```go
// Reload re-reads config.yaml and the token file, as SIGHUP does. On any
// error the previous values stay active. It wakes the run loop, which ticks;
// it never ticks itself. What the GitHub client holds for the token (its
// response cache, the token's metadata, a rate-limit suspension) is dropped
// only when the token changed. The degraded state is cleared every time, so
// a reload remains the way to say "try the token again now".
func (b *Backend) Reload() ([]string, error) {
	warnings, tokenChanged, err := b.Store.Reload()
```

and, further down in the same method, replace:

```go
	b.GH.ForgetCache()
	b.dropLabelChecks()
	b.M.ClearDegraded()
	b.Events.Add("info", "", "config and token reloaded")
```

with:

```go
	if tokenChanged {
		b.GH.ForgetCache()
	}
	b.dropLabelChecks()
	b.M.ClearDegraded()
	b.Events.Add("info", "", "config and token reloaded")
```

`SetToken` has the same three calls followed by another event (`GitHub token replaced`); do not change those.

Then run `PATH=/usr/local/go/bin:$PATH go vet ./...`. It must print nothing. If it names another caller of `Store.Reload` that still takes two results, give it the extra `_` and name the file in the task report.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ -run 'TestStore|TestReload|TestLabelCheck' -count=1`
Expected: PASS

Run: `PATH=/usr/local/go/bin:$PATH timeout 300 go test ./internal/daemon/ ./internal/api/ ./cmd/ghr/ -count=1`
Expected: PASS for all three packages.

- [ ] **Step 7: Commit**

Run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/` first; it must print nothing.

```bash
git add internal/daemon/store.go internal/daemon/store_test.go internal/daemon/backend.go internal/daemon/backend_test.go internal/daemon/run.go internal/daemon/labelcheck_test.go
git commit -m "fix(daemon): keep the client cache on a same-token reload"
```

### Task 26: Caches follow the token generation (Actions list, run details, runner steps)

**Files:**
- Create: `internal/daemon/tokengen_test.go`
- Modify: `internal/daemon/backend.go` (the `Backend` struct, new `tokenGen`, `RunnerSteps`), `internal/daemon/actions.go` (`actionsCache`, `Actions`), `internal/daemon/rundetail.go` (`detailCache`, `RunDetail`)
- Test: `internal/daemon/tokengen_test.go`

**Interfaces:**
- Consumes: nothing from Contracts. `GitHub.TokenMeta().Generation` and the test fake's `ForgetCache` (which adds 1 to the generation) exist already.
- Produces: `Backend.tokenGen` (Contracts); and the test helpers `tokenCache`, `tokenCaches` and `midFetchGH` in `internal/daemon/tokengen_test.go` (Contracts)

**Items:** 24
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

Background. The GitHub client counts token changes in `TokenMeta().Generation`; `ForgetCache` adds 1 to it. The daemon keeps its own copies of what it fetched with the token, and today they survive a token change. This task makes three of them follow the generation, the way `labelChecks.refresh` in `internal/daemon/labelcheck.go` already does: the Actions list with its per-repository errors and kept runs, the run details, and the runner steps. The parsed logs, the annotations, the log misses and the workflow "not found" marks are not part of this task.

The rule for each cache:

- It remembers the generation it was filled under and is emptied when the generation differs.
- A fetch reads the generation before it starts and keeps its result only if the generation is still the same when it ends. The caller of that fetch, and any caller that joined it, still gets the answer. So the maps of fetches in flight (`detailCache.calls`, `Backend.stepsCalls`) are never emptied.
- The generation is read while the cache's own lock is held. The client takes no lock of the daemon's, so the order is always cache lock, then client lock.

The Actions list differs in one point: `Actions` holds its lock for the whole build, so nothing else can touch the cache meanwhile. Its result is therefore stored under the generation the build began under, and a build the token changed during is never served, because the next call sees another generation and starts over.

- [ ] **Step 1: Write the failing tests**

Create `internal/daemon/tokengen_test.go`:

```go
package daemon

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// tokenCache is one cache of data fetched with the token: the GitHub call
// that fills it, how a test prepares it, the request that reads through it,
// how many of those GitHub calls it has made, and a request for another entry
// of the same cache (nil when a new token cannot reach the cache through
// another entry).
type tokenCache struct {
	name  string
	fetch string
	setup func(m *fakeManager, gh *fakeGH)
	read  func(b *Backend) error
	calls func(gh *fakeGH) int
	other func(b *Backend)
}

func tokenCaches() []tokenCache {
	ctx := context.Background()
	return []tokenCache{
		{"actions list", "ListRecentRuns",
			func(_ *fakeManager, gh *fakeGH) { gh.recentBy = map[string][]github.Run{} },
			func(b *Backend) error { _, err := b.Actions(ctx); return err },
			recentCalls,
			nil},
		{"run detail", "GetRun",
			func(_ *fakeManager, gh *fakeGH) { detailRun(gh, "completed") },
			func(b *Backend) error { _, err := b.RunDetail(ctx, "darkmem", 102, 0); return err },
			func(gh *fakeGH) int { return gh.count("GetRun darkmem 102") },
			func(b *Backend) { b.RunDetail(ctx, "darkmem", 103, 0) }},
		{"runner steps", "ListJobs",
			func(m *fakeManager, _ *fakeGH) { m.insts = []model.InstanceStatus{{ID: "aaaaaa"}} },
			func(b *Backend) error { _, err := b.RunnerSteps(ctx, "aaaaaa"); return err },
			jobCallCount,
			func(b *Backend) { b.RunnerSteps(ctx, "zzzzzz") }},
	}
}

// midFetchGH runs swap inside the first GitHub call named on, before that
// call is answered, so a test can replace the token while a fetch is in
// flight.
type midFetchGH struct {
	GitHub
	on      string
	swap    func()
	swapped atomic.Bool
}

func (g *midFetchGH) at(call string) {
	if call == g.on && g.swapped.CompareAndSwap(false, true) {
		g.swap()
	}
}

func (g *midFetchGH) ListRecentRuns(ctx context.Context, repo string, n int) ([]github.Run, error) {
	g.at("ListRecentRuns")
	return g.GitHub.ListRecentRuns(ctx, repo, n)
}

func (g *midFetchGH) GetRun(ctx context.Context, repo string, id int64) (*github.Run, error) {
	g.at("GetRun")
	return g.GitHub.GetRun(ctx, repo, id)
}

func (g *midFetchGH) ListJobs(ctx context.Context, repo string, runID int64) ([]github.Job, error) {
	g.at("ListJobs")
	return g.GitHub.ListJobs(ctx, repo, runID)
}

// What one token fetched is not served to the next.
func TestCachesAreRefilledAfterATokenChange(t *testing.T) {
	for _, c := range tokenCaches() {
		t.Run(c.name, func(t *testing.T) {
			b, m, gh := newBackend(t)
			frozenClock(b, actionsT0)
			c.setup(m, gh)
			if err := c.read(b); err != nil {
				t.Fatal(err)
			}
			one := c.calls(gh)
			if err := c.read(b); err != nil || one == 0 || c.calls(gh) != one {
				t.Fatalf("second read: %d calls after %d, err %v", c.calls(gh), one, err)
			}
			gh.ForgetCache()
			if err := c.read(b); err != nil || c.calls(gh) != 2*one {
				t.Fatalf("after a token change: %d calls, want %d, err %v", c.calls(gh), 2*one, err)
			}
		})
	}
}

// A fetch that was in flight when the token changed answers its caller and
// leaves nothing behind, also when a request for another entry has already
// moved the cache to the new token. The fetch after it is kept as usual.
func TestAFetchAcrossATokenChangeIsNotStored(t *testing.T) {
	for _, c := range tokenCaches() {
		t.Run(c.name, func(t *testing.T) {
			b, m, gh := newBackend(t)
			frozenClock(b, actionsT0)
			c.setup(m, gh)
			b.GH = &midFetchGH{GitHub: gh, on: c.fetch, swap: func() {
				gh.ForgetCache()
				if c.other != nil {
					c.other(b)
				}
			}}
			if err := c.read(b); err != nil {
				t.Fatalf("the caller of the fetch: %v", err)
			}
			one := c.calls(gh)
			if err := c.read(b); err != nil || one == 0 || c.calls(gh) != 2*one {
				t.Fatalf("next read: %d calls, want %d, err %v", c.calls(gh), 2*one, err)
			}
			if err := c.read(b); err != nil || c.calls(gh) != 2*one {
				t.Fatalf("third read: %d calls, want %d, err %v", c.calls(gh), 2*one, err)
			}
		})
	}
}
```

How the second test works: `midFetchGH` calls `gh.ForgetCache()` from inside the GitHub call that fills the cache, so the generation rises while the fetch is in flight, with no second goroutine and no waiting. For the run details and the runner steps it then reads another entry of the same cache (run 103, runner `zzzzzz`), which moves the cache to the new generation before the first fetch ends; that is the case in which a fetch that stored its result unconditionally would leave old data in a cache that already counts as new. The Actions list has one entry and holds its lock for the whole build, so it has no such request. `recentCalls`, `jobCallCount`, `detailRun`, `frozenClock` and `actionsT0` are existing helpers of the package's tests.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/daemon/ -run 'TestCachesAreRefilledAfterATokenChange|TestAFetchAcrossATokenChangeIsNotStored' -count=1`
Expected: FAIL in all six subtests:

- `TestCachesAreRefilledAfterATokenChange/actions_list`: `after a token change: 2 calls, want 4, err <nil>`
- `TestCachesAreRefilledAfterATokenChange/run_detail` and `/runner_steps`: `after a token change: 1 calls, want 2, err <nil>`
- `TestAFetchAcrossATokenChangeIsNotStored/actions_list`: `next read: 2 calls, want 4, err <nil>`
- `TestAFetchAcrossATokenChangeIsNotStored/run_detail` and `/runner_steps`: `next read: 1 calls, want 2, err <nil>`

- [ ] **Step 3: Add `tokenGen` and make the runner steps follow it**

In `internal/daemon/backend.go`, in the `Backend` struct, replace

```go
	stepsMu    sync.Mutex
	steps      map[string]stepsEntry
	stepsCalls map[string]*stepsCall
```

with

```go
	stepsMu    sync.Mutex
	steps      map[string]stepsEntry
	stepsGen   uint64 // the token generation steps was filled under
	stepsCalls map[string]*stepsCall
```

Directly below the `now` method (`func (b *Backend) now() time.Time`), add:

```go
// tokenGen is the GitHub client's token generation, which rises each time
// the token changes. A cache of data fetched with the token remembers the
// generation it was filled under and is emptied once it differs. A fetch
// reads the generation before it starts: the new token is stored before the
// generation rises, so a fetch may use a newer token than its generation
// (its result is then dropped) but never an older one. A cache reads it
// while holding its own lock; the client takes no lock of the daemon's, so
// the cache's lock always comes first.
func (b *Backend) tokenGen() uint64 { return b.GH.TokenMeta().Generation }
```

Replace the whole `RunnerSteps` method (it has no comment above it today) with:

```go
// RunnerSteps returns the steps of the job a runner is running. Callers for
// one runner share a fetch, and its answer is kept stepsTTL unless the token
// changed while it ran.
func (b *Backend) RunnerSteps(ctx context.Context, id string) ([]model.Step, error) {
	live := map[string]bool{}
	for _, in := range b.M.Status().Instances {
		live[in.ID] = true
	}
	now := b.now()
	b.stepsMu.Lock()
	gen := b.tokenGen()
	if gen != b.stepsGen {
		b.steps, b.stepsGen = nil, gen
	}
	for k, e := range b.steps {
		if !live[k] || now.Sub(e.at) >= stepsTTL {
			delete(b.steps, k)
		}
	}
	if e, ok := b.steps[id]; ok {
		b.stepsMu.Unlock()
		return e.steps, nil
	}
	if c, ok := b.stepsCalls[id]; ok {
		b.stepsMu.Unlock()
		select {
		case <-c.done:
			return c.steps, c.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	c := &stepsCall{done: make(chan struct{})}
	if b.stepsCalls == nil {
		b.stepsCalls = map[string]*stepsCall{}
	}
	b.stepsCalls[id] = c
	b.stepsMu.Unlock()

	// Joined callers share this fetch, so one caller closing its tab must
	// not cancel it for the others.
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	c.steps, c.err = b.fetchSteps(fctx, id)
	cancel()

	b.stepsMu.Lock()
	delete(b.stepsCalls, id)
	if c.err == nil && live[id] && b.tokenGen() == gen {
		if b.steps == nil {
			b.steps = map[string]stepsEntry{}
		}
		b.steps[id] = stepsEntry{at: b.now(), steps: c.steps}
	}
	b.stepsMu.Unlock()
	close(c.done)
	return c.steps, c.err
}
```

Compared with today this adds the comment, the four lines after `b.stepsMu.Lock()` that read the generation and empty `b.steps`, and `&& b.tokenGen() == gen` in the condition that stores the result. `fetchSteps` is unchanged.

- [ ] **Step 4: Make the Actions list follow it**

In `internal/daemon/actions.go`, replace the type `actionsCache` and the comment above it with:

```go
// good keeps each repository's runs from its last successful call, keyed by
// lower-case name, so a paused, rate-limited or rejected repository keeps
// showing them; a config change or a new token starts it over.
type actionsCache struct {
	mu     sync.Mutex
	cfg    *config.Config
	gen    uint64 // the token generation val and good were fetched under
	at     time.Time
	ok     bool
	val    model.Actions
	good   map[string][]model.ActionsRun
	goodAt time.Time
	// histFailing holds whether the last build failed to read history, so a
	// read that keeps failing warns once rather than at every build.
	histFailing bool
}
```

In `Actions`, replace

```go
	cfg := b.Store.Config()
	if c.ok && c.cfg == cfg && b.now().Sub(c.at) < actionsTTL {
		return c.val, nil
	}
	if c.cfg != cfg {
		c.good, c.goodAt = map[string][]model.ActionsRun{}, time.Time{}
	}
```

with

```go
	cfg := b.Store.Config()
	gen := b.tokenGen()
	if c.ok && c.cfg == cfg && c.gen == gen && b.now().Sub(c.at) < actionsTTL {
		return c.val, nil
	}
	if c.cfg != cfg || c.gen != gen {
		c.good, c.goodAt = map[string][]model.ActionsRun{}, time.Time{}
	}
	// The build is kept under the generation it began under, so one the
	// token changed during is never served: the next call starts over.
	c.gen = gen
```

Nothing else in `Actions` changes. The line near its end that stores the build (`c.cfg, c.at, c.ok, c.val = cfg, now, true, out`) stays as it is.

- [ ] **Step 5: Make the run details follow it**

In `internal/daemon/rundetail.go`, replace

```go
type detailCache struct {
	mu      sync.Mutex
	entries map[detailKey]detailEntry
```

with

```go
type detailCache struct {
	mu      sync.Mutex
	gen     uint64 // the token generation entries was filled under
	entries map[detailKey]detailEntry
```

In `RunDetail`, replace

```go
	c.mu.Lock()
	if e, ok := c.entries[key]; ok && b.now().Sub(e.at) < detailTTL(e.val) {
```

with

```go
	c.mu.Lock()
	gen := b.tokenGen()
	if gen != c.gen {
		c.entries, c.gen = nil, gen
	}
	if e, ok := c.entries[key]; ok && b.now().Sub(e.at) < detailTTL(e.val) {
```

and, further down in the same method, replace

```go
	delete(c.calls, key)
	if call.err == nil {
```

with

```go
	delete(c.calls, key)
	if call.err == nil && b.tokenGen() == gen {
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/daemon/ -run 'TestCachesAreRefilledAfterATokenChange|TestAFetchAcrossATokenChangeIsNotStored' -count=1`
Expected: PASS

- [ ] **Step 7: Run the package's tests with the race detector**

Run: `PATH=/usr/local/go/bin:$PATH go test -race ./internal/daemon/ -count=1`
Expected: PASS. No existing test changes with this task.

- [ ] **Step 8: Commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
git add internal/daemon/tokengen_test.go internal/daemon/backend.go internal/daemon/actions.go internal/daemon/rundetail.go
git commit -m "fix(daemon): drop Actions, run and step caches on new token"
```

### Task 27: Caches follow the token generation (logs, annotations, workflow marks)

**Files:**
- Modify: `internal/daemon/joblogs.go` (`logCall`, `logCache`, `initLocked`, `startLogLocked`, `downloadLog`, and the `initLocked` calls in `JobLog` and `openRawLog`), `internal/daemon/annotations.go` (the `initLocked` call in `RunAnnotations`), `internal/daemon/rungraph.go` (`wfFile`, `workflowFile`)
- Test: `internal/daemon/tokengen_test.go`

**Interfaces:**
- Consumes: `Backend.tokenGen` (Contracts); package `internal/lru` with `lru.New`, `Get`, `Add` (Contracts), which `logCache.parsed`, `logCache.notes` and `wfCache.files` already are; `wfFile.bad` and `github.ErrBadContent` (Contracts), which `workflowFile` already uses; `logNone` (Contracts), which is stored among the log misses and needs no line of its own here.; the test helpers `tokenCache`, `tokenCaches` and `midFetchGH` in `internal/daemon/tokengen_test.go` (Contracts)
- Produces: nothing a later task names

**Items:** 24
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

Background. `Backend.tokenGen()` returns the GitHub client's token generation, which rises each time the token changes. The Actions list, the run details and the runner steps already follow it. This task does the same for what is left of the data fetched with the token:

- the log cache (`logCache` in `joblogs.go`): parsed logs, annotations (`notes`) and log misses. A miss is an expired, too large, absent or failed read that is remembered for 10 minutes; the annotations route counts a remembered failed read without trying again, so a failure caused by the old token must not outlive it;
- the "workflow file not found" marks in `wfCache` (`rungraph.go`). A 404 for a workflow file can mean the token cannot see it. The file's bytes and the `bad` mark are the same for every token and are kept.

The rule is the one the other caches follow: a cache remembers the generation it was filled under and is emptied when the generation differs; a fetch reads the generation before it starts and keeps its result only if the generation is still the same when it ends, while the callers waiting on it still get the answer. The generation is read while the cache's own lock is held (cache lock first, then the client's).

Two points are particular to these caches:

- A log download runs on its own goroutine. It carries the generation it began under in its `logCall`, and downloads in flight (`logCache.calls`) are never dropped.
- The workflow cache holds bytes and marks in one `lru.Cache`, which cannot be walked. So each "not found" mark carries the generation it was answered under, and a mark from another generation counts as absent.

- [ ] **Step 1: Write the failing tests**

All edits in this step are in `internal/daemon/tokengen_test.go`.

Add `"errors"` to the import block, which becomes:

```go
import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)
```

In `tokenCaches`, add five rows after the `runner steps` row. Replace

```go
			jobCallCount,
			func(b *Backend) { b.RunnerSteps(ctx, "zzzzzz") }},
	}
}
```

with

```go
			jobCallCount,
			func(b *Backend) { b.RunnerSteps(ctx, "zzzzzz") }},
		{"parsed log", "JobLog",
			func(_ *fakeManager, gh *fakeGH) {
				logJobOf(gh, 1002, "Backend tests", "x")
				logJobOf(gh, 1003, "other", "x")
			},
			func(b *Backend) error { _, err := b.JobLog(ctx, "darkmem", 1002); return err },
			func(gh *fakeGH) int { return gh.count("JobLog darkmem 1002") },
			func(b *Backend) { b.JobLog(ctx, "darkmem", 1003) }},
		{"log miss", "JobLog",
			func(_ *fakeManager, gh *fakeGH) {
				logJobOf(gh, 1002, "Backend tests", "x")
				logJobOf(gh, 1003, "other", "x")
				gh.logErr[1002] = &github.APIError{Status: 410, Kind: github.ErrGone}
			},
			func(b *Backend) error { _, err := b.JobLog(ctx, "darkmem", 1002); return err },
			func(gh *fakeGH) int { return gh.count("JobLog darkmem 1002") },
			func(b *Backend) { b.JobLog(ctx, "darkmem", 1003) }},
		{"annotations", "JobLog",
			func(_ *fakeManager, gh *fakeGH) {
				annotatedRun(gh, logJobOf(gh, 2001, "Build", "##[error]build broke"))
				logJobOf(gh, 1003, "other", "x")
			},
			func(b *Backend) error { _, err := b.RunAnnotations(ctx, "darkmem", 102, 0); return err },
			func(gh *fakeGH) int { return gh.count("JobLog darkmem 2001") },
			func(b *Backend) { b.JobLog(ctx, "darkmem", 1003) }},
		{"failed log read", "JobLog",
			func(_ *fakeManager, gh *fakeGH) {
				annotatedRun(gh, logJobOf(gh, 2001, "Build", "x"))
				logJobOf(gh, 1003, "other", "x")
				gh.logErr[2001] = errors.New("boom")
			},
			func(b *Backend) error { _, err := b.RunAnnotations(ctx, "darkmem", 102, 0); return err },
			func(gh *fakeGH) int { return gh.count("JobLog darkmem 2001") },
			func(b *Backend) { b.JobLog(ctx, "darkmem", 1003) }},
		{"workflow file not found", "GetContent",
			func(*fakeManager, *fakeGH) {},
			func(b *Backend) error { _, _, err := b.runGraph(ctx, "darkmem", "abc", ciPath, ciJobs()); return err },
			func(gh *fakeGH) int { return gh.count("GetContent") },
			nil},
	}
}
```

Each log row reads one job, so one download runs at a time; `other` reads the log of job 1003 through the same cache. The `failed log read` row goes through the annotations route because only that route serves a remembered failed read. The workflow row has no `other`: a mark carries its own generation, so no other request can move it.

Below the `ListJobs` method of `midFetchGH`, add the two GitHub calls that fill these caches:

```go
func (g *midFetchGH) JobLog(ctx context.Context, repo string, jobID int64, limit int64) ([]byte, error) {
	g.at("JobLog")
	return g.GitHub.JobLog(ctx, repo, jobID, limit)
}

func (g *midFetchGH) GetContent(ctx context.Context, repo, path, ref string) ([]byte, error) {
	g.at("GetContent")
	return g.GitHub.GetContent(ctx, repo, path, ref)
}

// A workflow file's bytes at a commit are the same for every token, so only
// the "not found" answer follows the token.
func TestWorkflowFileBytesOutliveATokenChange(t *testing.T) {
	b, _, gh := newBackend(t)
	gh.content = map[string][]byte{contentKey("darkmem", ciPath, "abc"): []byte(ciYAML)}
	for range 2 {
		if g, note, err := b.runGraph(context.Background(), "darkmem", "abc", ciPath, ciJobs()); g == nil || note != "" || err != nil {
			t.Fatalf("graph %+v note %q err %v", g, note, err)
		}
		gh.ForgetCache()
	}
	if n := gh.count("GetContent"); n != 1 {
		t.Fatalf("%d GetContent calls, want 1", n)
	}
}
```

`TestWorkflowFileBytesOutliveATokenChange` is a guard: it passes before and after this task and pins that bytes are not dropped with the token.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/daemon/ -run 'TestCachesAreRefilledAfterATokenChange|TestAFetchAcrossATokenChangeIsNotStored|TestWorkflowFileBytesOutliveATokenChange' -count=1`
Expected: FAIL in the five new subtests of each table test, the three older subtests and `TestWorkflowFileBytesOutliveATokenChange` pass:

- `TestCachesAreRefilledAfterATokenChange/parsed_log`, `/log_miss`, `/annotations`, `/failed_log_read`, `/workflow_file_not_found`: `after a token change: 1 calls, want 2, err <nil>`
- `TestAFetchAcrossATokenChangeIsNotStored`, the same five subtests: `next read: 1 calls, want 2, err <nil>`

The output also carries the line `job 2001 log: boom`, which the daemon logs for the failed read.

- [ ] **Step 3: Make the log cache follow the generation**

In `internal/daemon/joblogs.go`, replace

```go
type logCall struct {
	done    chan struct{}
	started chan struct{} // closed when the download gets its slot
	res     logResult
}
```

with

```go
type logCall struct {
	done    chan struct{}
	started chan struct{} // closed when the download gets its slot
	gen     uint64        // the token generation the download began under
	res     logResult
}
```

In the `logCache` struct, replace the line

```go
	misses map[int64]logMiss
```

with

```go
	misses map[int64]logMiss
	gen    uint64 // the token generation parsed, notes and misses were filled under
```

Replace the whole `initLocked` method and its comment with:

```go
// initLocked creates the cache on first use and empties what an older token
// read; the caller holds mu. Downloads in flight are left to finish for the
// callers waiting on them.
func (c *logCache) initLocked(gen uint64) {
	first := c.slots == nil
	if first {
		c.slots = make(chan struct{}, logSlots)
		c.calls = map[int64]*logCall{}
	}
	if first || gen != c.gen {
		c.gen = gen
		c.parsed = lru.New[int64, model.JobLog](1<<30, parsedLogBytes)
		c.notes = lru.New[int64, jobNotes](annotationEntries, 0)
		c.misses = map[int64]logMiss{}
	}
}
```

`initLocked` is called in three places, each as the line `c.initLocked()` right after `c.mu.Lock()`: in `JobLog` and `openRawLog` in `joblogs.go`, and in `RunAnnotations` in `internal/daemon/annotations.go`. Replace each of the three with:

```go
	c.initLocked(b.tokenGen())
```

Replace the whole `startLogLocked` method and its comment with:

```go
// startLogLocked joins the job's download in flight or starts one; the
// caller holds mu and has called initLocked, so c.gen is the generation the
// download begins under.
func (b *Backend) startLogLocked(j logJob) *logCall {
	c := &b.logs
	if call, ok := c.calls[j.id]; ok {
		return call
	}
	call := &logCall{done: make(chan struct{}), started: make(chan struct{}), gen: c.gen}
	c.calls[j.id] = call
	// The caller reads logRequestWindow: a read on the detached goroutine
	// would race with tests that change it while earlier downloads still run.
	go b.downloadLog(j, call, logRequestWindow)
	return call
}
```

Replace the whole `downloadLog` method and its comment with:

```go
// downloadLog runs detached from every request, so a caller that stops
// waiting still leaves the result in the caches. A download the token
// changed during leaves nothing there; the callers waiting on it still get
// its result.
func (b *Backend) downloadLog(j logJob, call *logCall, queueWait time.Duration) {
	c := &b.logs
	res, size := b.readLog(j, call.started, queueWait)

	c.mu.Lock()
	delete(c.calls, j.id)
	if b.tokenGen() == call.gen {
		switch res.outcome {
		case logParsed:
			c.parsed.Add(j.id, res.log, size)
			c.notes.Add(j.id, res.notes, 0)
			delete(c.misses, j.id)
		case logBusy:
		default:
			c.misses[j.id] = logMiss{at: b.now(), res: res}
		}
	}
	c.mu.Unlock()
	call.res = res
	close(call.done)
}
```

Compared with the method as it stands, this adds two sentences to the comment and puts the `switch` inside `if b.tokenGen() == call.gen { ... }`. The `switch` itself is unchanged.

- [ ] **Step 4: Make the workflow "not found" marks follow the generation**

In `internal/daemon/rungraph.go`, in the `wfFile` struct, replace the line

```go
	bad       bool
```

with

```go
	bad       bool
	// missingGen is the token generation the 404 at missingAt was answered
	// under: a file one token cannot see may be visible to the next.
	missingGen uint64
```

In `workflowFile`, make three changes. Replace

```go
	f, ok := c.files.Get(key)
	c.mu.Unlock()
```

with

```go
	f, ok := c.files.Get(key)
	gen := b.tokenGen()
	c.mu.Unlock()
```

Replace the line

```go
	case ok && b.now().Sub(f.missingAt) < wfMissingTTL:
```

with

```go
	case ok && f.missingGen == gen && b.now().Sub(f.missingAt) < wfMissingTTL:
```

Replace the line

```go
		f = wfFile{missingAt: b.now()}
```

with

```go
		f = wfFile{missingAt: b.now(), missingGen: gen}
```

The generation is read before `GetContent` is called, so a 404 that arrives after the token changed is stored as a mark of the old generation and is not honoured. The cases for bytes (`ok && f.missingAt.IsZero()`) and for `bad` are left alone.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/daemon/ -run 'TestCachesAreRefilledAfterATokenChange|TestAFetchAcrossATokenChangeIsNotStored|TestWorkflowFileBytesOutliveATokenChange' -count=1`
Expected: PASS

- [ ] **Step 6: Run the package's tests with the race detector**

Run: `PATH=/usr/local/go/bin:$PATH go test -race ./internal/daemon/ -count=1`
Expected: PASS. No existing test changes with this task.

- [ ] **Step 7: Commit**

`gofmt -l` must print nothing; if it names `rungraph.go` or `joblogs.go`, run `gofmt -w` on that file, because a new struct field can shift the alignment of its neighbours.

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
git add internal/daemon/tokengen_test.go internal/daemon/joblogs.go internal/daemon/annotations.go internal/daemon/rungraph.go
git commit -m "fix(daemon): drop log and workflow marks on new token"
```

### Task 28: A re-run of a finished run

**Files:**
- Modify: `internal/daemon/rundetail.go` (`detailTTL`, its two calls in `RunDetail`, the comment on `RunDetail`), `web/src/api/hooks.ts` (`runDetailPoll`, `useRunDetail`)
- Test: `internal/daemon/rundetail_test.go`, `web/src/api/client-pages.test.ts`

**Interfaces:**
- Consumes: nothing
- Produces: `runDetailPoll(data, attempt)` (Contracts)

**Items:** 22
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

Background. The run page asks the daemon for a run's details either as "the latest attempt" (no attempt number; the daemon's cache key has attempt 0) or as a numbered attempt. Today the daemon keeps a completed attempt for 60 seconds under both keys, and the page stops polling once the shown attempt has completed. So when a finished run is re-run, the page keeps showing the old attempt until it is reloaded after the cache entry has ended.

Two changes close that:

- Daemon: what is stored under the latest-attempt key lives 15 seconds whatever its status, because a re-run changes which attempt that key stands for. Under a numbered attempt an entry still lives 15 seconds while the attempt runs and 60 seconds once it has completed.
- Web: `runDetailPoll` takes the requested attempt. For a completed run requested as the latest attempt it returns `POLL_SAFETY` (60 s) instead of `false`; for a numbered attempt it still returns `false`. The web client treats a missing attempt and attempt 0 alike as "the latest", so `runDetailPoll` does the same.

One fetch of the latest attempt fills two entries: the latest-attempt key and the key of that attempt's number. They now end at different times.

- [ ] **Step 1: Write the failing daemon tests**

In `internal/daemon/rundetail_test.go`, replace the whole test `TestRunDetailCacheLifetime` with:

```go
// The latest attempt (0) is read again after 15 s whatever its status, so a
// re-run of a finished run shows up; a numbered attempt that has completed
// never changes and is kept 60 s.
func TestRunDetailCacheLifetime(t *testing.T) {
	for _, tc := range []struct {
		status  string
		attempt int
		ttl     time.Duration
	}{
		{"in_progress", 0, detailActiveTTL},
		{"in_progress", 2, detailActiveTTL},
		{"completed", 0, detailActiveTTL},
		{"completed", 2, detailDoneTTL},
	} {
		b, _, gh := newBackend(t)
		clock := frozenClock(b, actionsT0)
		detailRun(gh, tc.status)
		for i, step := range []struct {
			at   time.Duration
			want int
		}{{0, 1}, {tc.ttl - time.Millisecond, 1}, {tc.ttl, 2}} {
			clock.Store(actionsT0.Add(step.at).UnixNano())
			if _, err := b.RunDetail(context.Background(), "darkmem", 102, tc.attempt); err != nil || gh.count("GetRun") != step.want {
				t.Fatalf("%s attempt %d step %d: %d calls, want %d, err %v", tc.status, tc.attempt, i, gh.count("GetRun"), step.want, err)
			}
		}
	}
}

// A fetch of the latest attempt also fills that attempt's numbered entry,
// which outlives the latest-attempt entry once the attempt has completed.
func TestRunDetailNumberedEntryOutlivesTheLatest(t *testing.T) {
	b, _, gh := newBackend(t)
	clock := frozenClock(b, actionsT0)
	detailRun(gh, "completed")
	ctx := context.Background()
	if _, err := b.RunDetail(ctx, "darkmem", 102, 0); err != nil {
		t.Fatal(err)
	}
	clock.Store(actionsT0.Add(detailActiveTTL).UnixNano())
	if _, err := b.RunDetail(ctx, "darkmem", 102, 2); err != nil || gh.count("GetRun") != 1 {
		t.Fatalf("attempt 2 after 15 s: %d calls, want 1, err %v", gh.count("GetRun"), err)
	}
	if _, err := b.RunDetail(ctx, "darkmem", 102, 0); err != nil || gh.count("GetRun") != 2 {
		t.Fatalf("the latest attempt after 15 s: %d calls, want 2, err %v", gh.count("GetRun"), err)
	}
}
```

The old `TestRunDetailCacheLifetime` asserted that a completed run read as the latest attempt is kept 60 seconds, which this task changes; its table now names the attempt, and the row `{"completed", 2, detailDoneTTL}` keeps guarding the 60 seconds of a numbered attempt. `detailRun` gives run 102 with two attempts, so attempt 2 is the latest one by number.

- [ ] **Step 2: Run the daemon tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/daemon/ -run 'TestRunDetailCacheLifetime|TestRunDetailNumberedEntryOutlivesTheLatest' -count=1`
Expected: FAIL with

- `TestRunDetailCacheLifetime`: `completed attempt 0 step 2: 1 calls, want 2, err <nil>`
- `TestRunDetailNumberedEntryOutlivesTheLatest`: `the latest attempt after 15 s: 1 calls, want 2, err <nil>`

- [ ] **Step 3: Write the daemon implementation**

In `internal/daemon/rundetail.go`, replace the whole function `detailTTL` (it has no comment today) with:

```go
// detailTTL is how long d is kept under k. The latest-attempt key (attempt
// 0) is always short-lived, because a re-run of a finished run changes which
// attempt it stands for; a numbered attempt that has completed never changes.
func detailTTL(k detailKey, d model.RunDetail) time.Duration {
	if k.attempt != 0 && d.Status == "completed" {
		return detailDoneTTL
	}
	return detailActiveTTL
}
```

`detailTTL` is called twice, both in `RunDetail`. Replace the line

```go
	if e, ok := c.entries[key]; ok && b.now().Sub(e.at) < detailTTL(e.val) {
```

with

```go
	if e, ok := c.entries[key]; ok && b.now().Sub(e.at) < detailTTL(key, e.val) {
```

and, in the loop that drops expired entries before a result is stored, replace the line

```go
			if now.Sub(e.at) >= detailTTL(e.val) {
```

with

```go
			if now.Sub(e.at) >= detailTTL(k, e.val) {
```

The first call passes `key`, the key of the request; the second passes `k`, the loop's own key, so each entry is judged by the key it is stored under.

Replace the comment above `RunDetail`

```go
// RunDetail returns one attempt of a run, 0 meaning the latest. Callers for
// the same key share one fetch, detached from any one request; the result is
// kept 15 s while the attempt runs and 60 s once it has completed.
```

with

```go
// RunDetail returns one attempt of a run, 0 meaning the latest. Callers for
// the same key share one fetch, detached from any one request. The result is
// kept 15 s as the latest attempt; under its attempt number it is kept 15 s
// while the attempt runs and 60 s once it has completed.
```

- [ ] **Step 4: Run the daemon tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/daemon/ -run 'TestRunDetail' -count=1`
Expected: PASS

- [ ] **Step 5: Run the package's tests**

Run: `PATH=/usr/local/go/bin:$PATH go test -race ./internal/daemon/ -count=1`
Expected: PASS. Besides `TestRunDetailCacheLifetime`, replaced in Step 1, no existing test changes.

- [ ] **Step 6: Write the failing web test**

In `web/src/api/client-pages.test.ts`, add `POLL_SAFETY` to the import from `./hooks`, which becomes:

```ts
import { annotationsPoll, keys, POLL_ACTIONS, POLL_SAFETY, POLL_SLOW, runDetailPoll, useJobLog, useRunAnnotations, useRunDetail } from "./hooks"
```

Replace the test

```ts
  it("polls a run only while its attempt is not completed", () => {
    expect(runDetailPoll(undefined)).toBe(false)
    expect(runDetailPoll(fixtures.runDetail)).toBe(false)
    expect(runDetailPoll({ ...fixtures.runDetail, status: "in_progress" })).toBe(POLL_ACTIONS)
  })
```

with

```ts
  it.each([
    ["no data, latest attempt", undefined, undefined, false],
    ["no data, numbered attempt", undefined, 2, false],
    ["an active run, latest attempt", { ...fixtures.runDetail, status: "in_progress" }, undefined, POLL_ACTIONS],
    ["an active run, numbered attempt", { ...fixtures.runDetail, status: "in_progress" }, 2, POLL_ACTIONS],
    ["a completed run, latest attempt", fixtures.runDetail, undefined, POLL_SAFETY],
    ["a completed run, numbered attempt", fixtures.runDetail, 2, false],
  ])("polls a run's details with %s", (_why, data, attempt, want) => {
    expect(runDetailPoll(data, attempt)).toBe(want)
  })
```

The old test asserted that a completed run is never polled; that now holds only for a numbered attempt. `fixtures.runDetail` is a completed run.

- [ ] **Step 7: Run the web test to verify it fails**

Run, in `web/`: `npx vitest run src/api/client-pages.test.ts`
Expected: FAIL in one test, `polls a run's details with a completed run, latest attempt`: `AssertionError: expected false to be 60000`. The other five rows pass, because the function ignores the second argument until Step 8.

- [ ] **Step 8: Write the web implementation**

In `web/src/api/hooks.ts`, replace

```ts
// A run's details change only while the shown attempt runs.
export function runDetailPoll(data: RunDetail | undefined): number | false {
  return data && data.status !== "completed" ? POLL_ACTIONS : false
}
```

with

```ts
// A run's details change while the shown attempt runs. A completed run read
// as its latest attempt (no attempt number) is still read once a minute, so a
// re-run shows up; a numbered attempt that has completed never changes.
export function runDetailPoll(data: RunDetail | undefined, attempt: number | undefined): number | false {
  if (!data) return false
  if (data.status !== "completed") return POLL_ACTIONS
  return attempt ? false : POLL_SAFETY
}
```

In `useRunDetail`, replace the line

```ts
    refetchInterval: (q) => runDetailPoll(q.state.data),
```

with

```ts
    refetchInterval: (q) => runDetailPoll(q.state.data, attempt),
```

`attempt` is the hook's own third parameter. `runDetailPoll` has no other caller.

- [ ] **Step 9: Run the web test, the linter and the type check**

Run, in `web/`:

```bash
npx vitest run src/api/client-pages.test.ts
npm run lint
npm run typecheck
```

Expected: every test in the file passes, and lint and typecheck report nothing.

- [ ] **Step 10: Commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
git add internal/daemon/rundetail.go internal/daemon/rundetail_test.go web/src/api/hooks.ts web/src/api/client-pages.test.ts
git commit -m "fix(run-detail): show a re-run of a finished run"
```

### Task 29: Runner steps and activity order

**Files:**
- Modify: `internal/daemon/backend.go` (imports, `RunnerSteps`, new `fetchStepsSafely`, `fetchSteps`, `Activity`)
- Test: `internal/daemon/backend_test.go` (the `fakeManager` fake and new tests), `internal/daemon/rundetail_test.go` (the `panickyGH` test double)

**Interfaces:**
- Consumes: nothing from Contracts. `RunnerSteps` already reads `Backend.tokenGen` (Contracts); this task keeps those lines as they are.
- Produces: nothing a later task names

**Items:** 29, 30
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

Background. Two defects in `internal/daemon/backend.go`, fixed one after the other with a commit each.

Runner steps (Steps 1 to 6). `RunnerSteps` answers the steps of the job a runner is running. Its fetch, `fetchSteps`, asks the manager for the runner's repository and run, and then calls GitHub's `ListJobs`. Today it calls GitHub also while the token is rejected or a rate limit pauses the API, and a panic inside the fetch would leave the shared call registered in `b.stepsCalls` for good, so every later request for that runner would wait on it. The fix:

- `b.githubErr()` is checked immediately before `ListJobs`, and nowhere earlier. So an unknown runner still answers 404, a runner without a job still answers an empty list, and an answer still in the 5-second cache is still served, all without GitHub and therefore also while the token is rejected (503) or calls are paused (429).
- The fetch runs through a new `fetchStepsSafely`, which turns a panic into the error `could not read the runner's steps`, the way `fetchDetailSafely` in `rundetail.go` does for run details.

Activity (Steps 7 to 11). A job is first a live instance, then a pending record, then a history entry, and it is recorded in the next place before it leaves the one before. `Activity` reads pending records, then history, then instances, so a job that finishes between the first two reads and the last is in none of them and vanishes from the view for one build. The fix reads instances, then pending records, then history, the order `ghrRuns` in `actions.go` already uses.

- [ ] **Step 1: Extend the test doubles for the runner steps tests**

In `internal/daemon/backend_test.go`, replace the whole method `RunnerRepoAndRun` of `fakeManager` with:

```go
func (f *fakeManager) RunnerRepoAndRun(id string) (string, int64, string, error) {
	switch id {
	case "aaaaaa":
		return "darkcloud", 55, "ghr-darkcloud-aaaaaa", nil
	case "bbbbbb": // a runner that has no job
		return "darkcloud", 0, "ghr-darkcloud-bbbbbb", nil
	}
	return "", 0, "", runner.ErrUnknownRunner(id)
}
```

It answers as before for `aaaaaa` and for unknown IDs; `bbbbbb` is new.

In `internal/daemon/rundetail_test.go`, give `panickyGH` a flag that makes `ListJobs` panic. Replace the line

```go
	run, jobLog, content, openLog bool
```

with

```go
	run, jobs, jobLog, content, openLog bool
```

and add this method below the `GetRun` method of `panickyGH`:

```go
func (p *panickyGH) ListJobs(ctx context.Context, repo string, runID int64) ([]github.Job, error) {
	if p.jobs {
		panic("boom")
	}
	return p.GitHub.ListJobs(ctx, repo, runID)
}
```

- [ ] **Step 2: Write the failing runner steps tests**

Append to `internal/daemon/backend_test.go`:

```go
// While the token is rejected or API calls are paused, RunnerSteps still
// answers whatever needs no GitHub call and refuses only the fetch.
func TestRunnerStepsRefuseOnlyTheGitHubCall(t *testing.T) {
	for _, tc := range []struct {
		name   string
		block  func(m *fakeManager, now time.Time)
		status int
	}{
		{"token rejected", func(m *fakeManager, _ time.Time) { m.degraded = "401 Bad credentials" }, 503},
		{"calls paused", func(m *fakeManager, now time.Time) { m.paused = now.Add(10 * time.Minute) }, 429},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, m, gh := newBackend(t)
			m.insts = []model.InstanceStatus{{ID: "aaaaaa"}, {ID: "bbbbbb"}}
			now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
			b.Now = func() time.Time { return now }
			ctx := context.Background()
			if _, err := b.RunnerSteps(ctx, "aaaaaa"); err != nil {
				t.Fatal(err)
			}
			tc.block(m, now)
			if s, err := b.RunnerSteps(ctx, "aaaaaa"); err != nil || len(s) != 1 {
				t.Fatalf("cached answer: steps %+v err %v", s, err)
			}
			if _, err := b.RunnerSteps(ctx, "zzzzzz"); apiStatus(err) != 404 {
				t.Fatalf("unknown runner: %v", err)
			}
			if s, err := b.RunnerSteps(ctx, "bbbbbb"); err != nil || len(s) != 0 {
				t.Fatalf("runner without a job: steps %+v err %v", s, err)
			}
			now = now.Add(stepsTTL)
			if _, err := b.RunnerSteps(ctx, "aaaaaa"); apiStatus(err) != tc.status || jobCallCount(gh) != 1 {
				t.Fatalf("the fetch: err %v, %d GitHub calls, want status %d and 1 call", err, jobCallCount(gh), tc.status)
			}
		})
	}
}

// A panic inside the fetch is an error for its callers, and the shared call
// is cleared so the next request fetches again.
func TestRunnerStepsPanicLeavesNoCallBehind(t *testing.T) {
	b, m, gh := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa"}}
	p := &panickyGH{GitHub: gh, jobs: true}
	b.GH = p
	ctx := context.Background()
	if _, err := b.RunnerSteps(ctx, "aaaaaa"); err == nil || err.Error() != "could not read the runner's steps" {
		t.Fatalf("panic answered %v", err)
	}
	b.stepsMu.Lock()
	left := len(b.stepsCalls)
	b.stepsMu.Unlock()
	if left != 0 {
		t.Fatalf("%d calls left in flight", left)
	}
	p.jobs = false
	if s, err := b.RunnerSteps(ctx, "aaaaaa"); err != nil || len(s) != 1 {
		t.Fatalf("the next request: steps %+v err %v", s, err)
	}
}
```

In the first test the first three assertions after `tc.block` already hold today, because nothing is refused yet; they pin that the check does not move further up than the GitHub call. The last assertion is the one that fails.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/daemon/ -run 'TestRunnerStepsRefuseOnlyTheGitHubCall' -count=1`
Expected: FAIL in both subtests with `the fetch: err <nil>, 2 GitHub calls, want status 503 and 1 call` and `the fetch: err <nil>, 2 GitHub calls, want status 429 and 1 call`.

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/daemon/ -run 'TestRunnerStepsPanicLeavesNoCallBehind' -count=1`
Expected: FAIL, the test stops with `panic: boom [recovered, repanicked]` and a stack trace: nothing recovers the panic yet.

- [ ] **Step 4: Write the runner steps implementation**

In `internal/daemon/backend.go`, add `"log"` to the import block, between `"io"` and `"maps"`, unless it is already there.

In `RunnerSteps`, replace

```go
	// Joined callers share this fetch, so one caller closing its tab must
	// not cancel it for the others.
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	c.steps, c.err = b.fetchSteps(fctx, id)
	cancel()
```

with

```go
	c.steps, c.err = b.fetchStepsSafely(ctx, id)
```

Nothing else in `RunnerSteps` changes: no `githubErr` check goes into it.

Replace the whole `fetchSteps` method (it has no comment today) with these two methods:

```go
// fetchStepsSafely runs the fetch detached from ctx, because joined callers
// share it and one of them closing its tab must not cancel it for the
// others. A panic becomes an error: the call must still leave b.stepsCalls
// and close done, or every later request for the runner would wait on it for
// good.
func (b *Backend) fetchStepsSafely(ctx context.Context, id string) (steps []model.Step, err error) {
	fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("runner %s steps: panic: %v", id, r)
			steps, err = nil, errors.New("could not read the runner's steps")
		}
	}()
	return b.fetchSteps(fctx, id)
}

// fetchSteps answers an unknown runner and a runner without a job before it
// asks whether GitHub may be called, so both still answer while the token is
// rejected or calls are paused.
func (b *Backend) fetchSteps(ctx context.Context, id string) ([]model.Step, error) {
	repo, runID, runnerName, err := b.M.RunnerRepoAndRun(id)
	if err != nil {
		return nil, notFoundIfUnknown(err)
	}
	if runID == 0 {
		return nil, nil
	}
	if err := b.githubErr(); err != nil {
		return nil, err
	}
	jobs, err := b.GH.ListJobs(ctx, repo, runID)
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		if j.RunnerName != runnerName {
			continue
		}
		return modelSteps(j.Steps), nil
	}
	return nil, nil
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/daemon/ -run 'TestRunnerSteps|TestPauseAllStepsKillToken' -count=1`
Expected: PASS. With `-v` the run also shows `runner aaaaaa steps: panic: boom`, the log line of the recovered panic.

- [ ] **Step 6: Run the package's tests and commit**

Run: `PATH=/usr/local/go/bin:$PATH go test -race ./internal/daemon/ -count=1`
Expected: PASS. No existing test changes with this half.

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
git add internal/daemon/backend.go internal/daemon/backend_test.go internal/daemon/rundetail_test.go
git commit -m "fix(daemon): gate and guard the runner steps fetch"
```

- [ ] **Step 7: Write the failing activity test**

In `internal/daemon/backend_test.go`, give `fakeManager` a hook that runs when the instances are read. In the `fakeManager` struct, replace the line

```go
	onPending func() // runs when Pending is read, before it returns
```

with

```go
	onPending func() // runs when Pending is read, before it returns
	onStatus  func() // runs when Status is read, before it returns
```

In the method `Status` of `fakeManager`, replace the line

```go
	f.statusCalls.Add(1)
```

with

```go
	f.statusCalls.Add(1)
	if f.onStatus != nil {
		f.onStatus()
	}
```

Replace the whole test `TestActivityKeepsAJobFinalizedMidBuild` and the comment above it with:

```go
// A job moves from the instances to the pending records to history. One that
// moves on while the view is built is read from where it moved to, because
// the three are read in that order.
func TestActivityKeepsAJobFinalizedMidBuild(t *testing.T) {
	now := time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		arm  func(m *fakeManager, finalize func())
	}{
		{"an instance finishes as the instances are read", func(m *fakeManager, finalize func()) {
			m.insts = []model.InstanceStatus{{ID: "p", Repo: "darkmem", State: "busy", Since: now.Add(-20 * time.Minute)}}
			m.onStatus = func() {
				m.insts = nil
				finalize()
			}
		}},
		{"a pending record is finalized as the pending records are read", func(m *fakeManager, finalize func()) {
			m.onPending = finalize
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, m, _ := newBackend(t)
			b.Now = func() time.Time { return now }
			done := false
			tc.arm(m, func() {
				if done {
					return
				}
				done = true
				if err := b.Hist.Append(model.HistoryEntry{ID: "p", Repo: "darkmem", RunID: 1, Conclusion: "success",
					StartedAt: now.Add(-20 * time.Minute), FinishedAt: now.Add(-10 * time.Minute)}); err != nil {
					t.Fatal(err)
				}
			})
			a, err := b.Activity(context.Background(), "1h", "", time.UTC)
			if err != nil {
				t.Fatal(err)
			}
			if len(a.Lanes) == 0 || len(a.Lanes[0].Runs) != 1 || a.Lanes[0].Runs[0].InstanceID != "p" {
				t.Fatalf("lanes %+v", a.Lanes)
			}
		})
	}
}
```

The second row is the old test unchanged in substance: a pending record that reaches history while the pending records are read. The first row is new: the job is a live instance until the moment the instances are read, and by then it is in history only.

- [ ] **Step 8: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/daemon/ -run 'TestActivityKeepsAJobFinalizedMidBuild' -count=1`
Expected: FAIL in `TestActivityKeepsAJobFinalizedMidBuild/an_instance_finishes_as_the_instances_are_read` with `lanes [{Runs:[]} {Runs:[]}]`; the other subtest passes.

- [ ] **Step 9: Write the activity implementation**

In `internal/daemon/backend.go`, in `Activity`, replace

```go
	// Pending before history: finalize appends to history before it removes
	// the pending record, so a job in between is in at least one of them.
	pend := b.M.Pending()
```

with

```go
	// Instances, then pending records, then history: a job moves through
	// them in that order, and it is recorded in the next place before it
	// leaves the one before, so a job that moves during the build is read in
	// at least one of them.
	insts := b.M.Status().Instances
	pend := b.M.Pending()
```

and, a few lines further down in the same method, replace the line

```go
		History: hist, Pending: pend, Instances: b.M.Status().Instances,
```

with

```go
		History: hist, Pending: pend, Instances: insts,
```

- [ ] **Step 10: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/daemon/ -run 'TestActivity' -count=1`
Expected: PASS

- [ ] **Step 11: Run the package's tests and commit**

Run: `PATH=/usr/local/go/bin:$PATH go test -race ./internal/daemon/ -count=1`
Expected: PASS. Besides `TestActivityKeepsAJobFinalizedMidBuild`, replaced in Step 7, no existing test changes.

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
git add internal/daemon/backend.go internal/daemon/backend_test.go
git commit -m "fix(daemon): read instances first in Activity"
```

### Task 30: Panic guards on detached goroutines

**Files:**
- Modify: `internal/daemon/labelcheck.go` (imports, `scanLabels`, new `gatherLabelsSafely`), `internal/daemon/actions.go` (the fetch goroutine in `Actions`)
- Test: `internal/daemon/labelcheck_test.go`, `internal/daemon/actions_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names

**Items:** 43
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

Background. A panic on a goroutine that no request is waiting on ends the whole daemon. Two such goroutines in `internal/daemon` have no `recover`, unlike `readLog` in `joblogs.go` and `fetchDetailSafely` in `rundetail.go`:

- `scanLabels` in `labelcheck.go`, started by `StartLabelCheck` with `go b.scanLabels(...)`. It gathers a repository's recent jobs from GitHub and publishes the result. After a panic the scan must still publish, as a partial result with the error `could not finish the check`; otherwise the repository would show "checking" for good.
- the goroutine that `Actions` in `actions.go` starts for each repository to read its recent runs. After a panic that repository's row must carry the error `could not read the runs`, and the build must finish with the other repositories.

- [ ] **Step 1: Write the failing tests**

Append to `internal/daemon/labelcheck_test.go`:

```go
// panickyRuns panics when the recent runs of repo are listed.
type panickyRuns struct {
	GitHub
	repo string
}

func (p *panickyRuns) ListRecentRuns(ctx context.Context, repo string, n int) ([]github.Run, error) {
	if repo == p.repo {
		panic("boom")
	}
	return p.GitHub.ListRecentRuns(ctx, repo, n)
}

// A panic inside a scan ends it as a partial result with an error, so the
// daemon keeps running and the repository does not show "checking" for good.
func TestLabelCheckPanicEndsTheScan(t *testing.T) {
	b, _, gh := newBackend(t)
	labelGH(gh)
	b.GH = &panickyRuns{GitHub: gh, repo: "darkcloud"}
	if err := b.StartLabelCheck("darkcloud"); err != nil {
		t.Fatal(err)
	}
	lc := waitLabelCheck(t, b, "darkcloud")
	if lc.State != "done" || !lc.Partial || lc.Error != "could not finish the check" || lc.CheckedAt == nil ||
		lc.Groups == nil || len(lc.Groups) != 0 {
		t.Fatalf("%+v", lc)
	}
}
```

Append to `internal/daemon/actions_test.go`:

```go
// A panic while one repository's runs are read is that repository's error;
// the build still finishes and lists the others.
func TestActionsPanicIsOneRepositorysError(t *testing.T) {
	b, _, gh := newBackend(t)
	gh.recentBy = map[string][]github.Run{"darkmem": {testRun(2, "completed", "success", time.Now().Add(-time.Hour))}}
	b.GH = &panickyRuns{GitHub: gh, repo: "darkcloud"}
	a, err := b.Actions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if runIDs(a) != "[2]" || a.Repos[0].Repo != "darkcloud" || a.Repos[0].Error != "could not read the runs" || a.Repos[1].Error != "" {
		t.Fatalf("runs %s repos %+v", runIDs(a), a.Repos)
	}
}
```

`panickyRuns` wraps the package's fake GitHub client and serves both tests; listing the recent runs is the first GitHub call of a label scan and the only one of an Actions fetch. The test backend is configured with the repositories `darkcloud` and `darkmem`, in that order. `labelGH`, `waitLabelCheck`, `testRun` and `runIDs` are existing helpers.

- [ ] **Step 2: Run the tests to verify they fail**

Each test ends the test binary, so run them one at a time.

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/daemon/ -run 'TestLabelCheckPanicEndsTheScan' -count=1`
Expected: FAIL, the binary stops with `panic: boom` and a stack trace through `gatherLabels`.

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/daemon/ -run 'TestActionsPanicIsOneRepositorysError' -count=1`
Expected: FAIL, the binary stops with `panic: boom` and a stack trace through `(*Backend).Actions.func1`.

- [ ] **Step 3: Guard the label scan**

In `internal/daemon/labelcheck.go`, add `"log"` to the import block, between `"errors"` and `"net/http"`.

In `scanLabels`, replace the line

```go
	groups, err := gatherLabels(ctx, b.GH, name)
```

with

```go
	groups, err := gatherLabelsSafely(ctx, b.GH, name)
```

and add this function directly below `scanLabels`:

```go
// gatherLabelsSafely turns a panic in the scan into its error. The scan runs
// on a detached goroutine, where a panic would end the daemon, and it must
// still publish its result, or the repo would show "checking" for good.
func gatherLabelsSafely(ctx context.Context, gh GitHub, repo string) (groups []model.LabelGroup, err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("label check %s: panic: %v", repo, r)
			groups, err = []model.LabelGroup{}, errors.New("could not finish the check")
		}
	}()
	return gatherLabels(ctx, gh, repo)
}
```

The rest of `scanLabels` already does what the result needs: an error that is not a deadline sets `Partial` and puts the error's text into `Error`.

- [ ] **Step 4: Guard the Actions fetch**

In `internal/daemon/actions.go`, in `Actions`, replace

```go
			go func() {
				defer wg.Done()
				select {
```

with

```go
			go func() {
				defer wg.Done()
				// A panic on this detached goroutine would end the daemon,
				// so it becomes this repository's error.
				defer func() {
					if p := recover(); p != nil {
						log.Printf("actions: %s: panic: %v", r.repo, p)
						runs[i], errs[i] = nil, errors.New("could not read the runs")
					}
				}()
				select {
```

`errors` and `log` are already imported there. Deferred calls run last-registered first, so after a panic the slot taken from `sem` is released, then this function stores the error, then `wg.Done` lets the build go on. The recovered value is named `p` because `r` is the repository of the surrounding loop.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/daemon/ -run 'TestLabelCheckPanicEndsTheScan|TestActionsPanicIsOneRepositorysError' -count=1`
Expected: PASS. With `-v` the run also shows the two log lines `label check darkcloud: panic: boom` and `actions: darkcloud: panic: boom`.

- [ ] **Step 6: Run the package's tests with the race detector**

Run: `PATH=/usr/local/go/bin:$PATH go test -race ./internal/daemon/ -count=1`
Expected: PASS. No existing test changes with this task.

- [ ] **Step 7: Commit**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
git add internal/daemon/labelcheck.go internal/daemon/labelcheck_test.go internal/daemon/actions.go internal/daemon/actions_test.go
git commit -m "fix(daemon): recover panics on detached goroutines"
```

- [ ] **Step 8: Check the whole tree**

This is the plan's last task, so check everything once more, after the commit above. From the repository root:

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/
PATH=/usr/local/go/bin:$PATH go vet ./...
PATH=/usr/local/go/bin:$PATH timeout 900 go test -race -count=1 ./...
```

Expected: `gofmt` prints nothing, `go vet` prints nothing, and every package line of `go test` starts with `ok` (22 packages).

Then in `web/`:

```bash
cd web
timeout 600 npx vitest run
timeout 300 npm run lint
timeout 300 npm run typecheck
```

Expected: `Test Files 90 passed (90)` with about 1170 tests passed and none failed, and `lint` and `typecheck` exit 0 with no finding. A failure here belongs to the task that introduced it: report it with the failing test's name and output, and change nothing to make it pass.
