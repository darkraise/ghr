# ghr Realtime UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The daemon pushes "these topics changed" over a server-sent event stream, and the web UI refetches those queries at once instead of waiting for its poll timers (register row 3).

**Architecture:** A new `internal/changes` hub fingerprints eight daemon-state topics every 250 ms, woken early by new events and history rows, and sends each changed set to its subscribers. `GET /changes` streams those sets as server-sent events; the web listener re-checks the session every 5 s so a logout ends the stream. The browser reads the stream with `fetch` (every `/api` request needs `X-GHR: 1`), invalidates the matching TanStack queries, and drops daemon-state polling to a 60 s safety net while the stream is live.

**Tech Stack:** Go 1.26 standard library (`net/http`, `sync`, `hash/fnv`, `encoding/json`), React 19 + TypeScript, TanStack Query 5, Vitest + Testing Library under jsdom.

**Spec:** docs/superpowers/specs/2026-10-10-ghr-realtime-ui-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 7 of 11 tasks total 4 or less with risk below 3; Tasks 3, 4, 7 (total 5 or more) and 8 (risk 3) are delegated.

**Plan review:** 2026-10-10 — dr-superpowers:judge-opus — executability 16 / coherence 18 / coverage 17 / assumptions 18 (round 1)

## Global Constraints

- Execution environment: the owner's Linux machine (bash, GNU coreutils `timeout`, Go 1.26 at `/usr/local/go/bin`, Node 24). If `go` is not on `PATH`, run `export PATH=$PATH:/usr/local/go/bin` first.
- Work on the branch `feat/realtime-ui` (Task 1 Step 0 creates it from `master`), never on `master`. Do not push `master`: CI turns every push to `master` into a release. Do not commit `.superpowers/`.
- Go checks, from the repository root: `gofmt -l internal cmd web/*.go` prints nothing; `go vet ./internal/... ./cmd/... ./web/` passes. Never run `go vet ./...` or `go test ./...`: they walk Go code under `web/node_modules`.
- Web checks, from `web/`: `npm test`, `npm run typecheck`, `npm run lint` (eslint with `--max-warnings 0`).
- Run `gofmt -w` on every Go file you change before the checks: a field added to an aligned struct block realigns the whole block.
- Give every test command an explicit timeout (`timeout 300 …`). Run every Go test command in this plan with `-race`.
- Daemon state only: no GitHub-backed data (Actions list, run and job pages) is pushed, and nothing here spends GitHub API budget.
- Topic names, verbatim and lowercase: `status`, `gauges`, `events`, `config`, `token`, `storage`, `history`, `metrics`.
- Timings: watcher tick 250 ms; wake coalescing window 50 ms; at most 32 subscribers; heartbeat 15 s; per-write deadline 30 s; session re-check 5 s; browser reconnect delay 1 s doubling to 30 s; safety poll 60 s while live.
- Wire format, verbatim: headers `Content-Type: text/event-stream`, `Cache-Control: no-store`, `X-Accel-Buffering: no`; first frame `event: hello\ndata: {}\n\n`; each set `event: change\ndata: {"topics":[…]}\n\n`; heartbeat `: ping\n\n`.
- Copy, verbatim: 503 `{"error":"not ready"}`, `{"error":"too many live connections"}`, `{"error":"shutting down"}`.
- English only. Comments only where the why is not obvious.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no period.

## Contracts

**Go — `internal/events` (Task 1)**

- `(r *Ring) SetOnChange(fn func())`: stored under `r.mu`; nil removes it.
- `(r *Ring) Add(...)` reads the hook under `r.mu`, releases the lock, logs, then calls the hook.
- `(r *Ring) Last() int64`: the newest sequence number, 0 before any event.

**Go — `internal/history` (Task 2)**

- `(s *Store) SetOnChange(fn func())`: stored under `s.mu`; nil removes it.
- `(s *Store) Writes() int64`: appends that wrote a line plus prunes that renamed a new file into place. A duplicate append and a prune with no file or an empty file do not count.
- The hook runs after `s.mu` is released, once per counted write.

**Go — `internal/changes` (Task 3, new package)**

- `const MaxSubscribers = 32`.
- `var ErrNotReady, ErrFull, ErrClosed error` (distinct sentinels for `errors.Is`).
- `type Options struct { Tick, WakeWindow time.Duration; Logf func(format string, args ...any) }`: zero values mean 250 ms, 50 ms and `log.Printf`.
- `func New(o Options) *Hub`.
- `func Fingerprint(v any) (uint64, error)`: FNV-64a of `json.Marshal(v)`.
- `(h *Hub) SetSources(sources func() map[string]uint64, before func(topics []string))`: `before` may be nil; it runs with the hub's lock held, before a set holding `status`, `config`, `history` or `metrics` is sent.
- `(h *Hub) Wake()`: never blocks, takes no lock.
- `(h *Hub) Subscribe() (*Sub, error)`: `ErrClosed` after `Close`, `ErrNotReady` before `SetSources`, `ErrFull` at 32 open; the first subscriber's baseline is computed before `Subscribe` returns.
- `type Sub struct { C <-chan []string; … }`: each value is a sorted, de-duplicated topic set shared between subscribers (read-only); `C` is closed when the subscription ends.
- `(s *Sub) Close()`: ends the subscription and closes `C`; safe to call twice and after `Hub.Close`.
- `(h *Hub) Close()`: closes every `C`, stops the watcher and waits for it; safe to call twice and concurrently.

**Go — `internal/daemon`**

- Task 4: `Backend.activityGen atomic.Int64`; `activityEntry.gen int64`; `(b *Backend) staleActivity(topics []string)` (bumps the generation); `func tokenStatus(meta github.TokenMeta, st model.Status) model.TokenStatus`; `(b *Backend) storageFrom(st model.Status) model.Storage`. `Token()` and `Storage()` become one-line wrappers.
- Task 4 test fakes (`backend_test.go`): `fakeManager.now time.Time`, `.diskUsed int64`, `.rate int` (returned in `Status().Now`, `.DiskUsedBytes`, `.RateRemaining`), and `.statusCalls atomic.Int64` (bumped by every `Status` call).
- Task 5: `Backend.Changes *changes.Hub` (nil until the daemon starts); `Backend.flags setupFlagCache`; `(b *Backend) Subscribe() (*changes.Sub, error)` (nil hub → `changes.ErrNotReady`); `(b *Backend) setupFlags() (webRequired, pending bool)`; `(b *Backend) fingerprints() map[string]uint64` — all eight topics, in `internal/daemon/changes.go`.
- Task 7: `var hubOptions changes.Options` in `run.go` (zero value = hub defaults; tests set it). `start(ctx context.Context, o Options, store *Store, ev *events.Ring, hub *changes.Hub, epoch string, webCfg config.Web, webSetupRequired func() bool) (*running, error)`.

**Go — `internal/api` (Task 6)**

- `Backend.Subscribe() (*changes.Sub, error)` added to the interface.
- `GET /changes` per the Global Constraints' wire format and copy.
- `var heartbeat = 15 * time.Second`, `var changesWriteWindow = 30 * time.Second` (tests shorten `heartbeat`).
- Test fake: `fakeBackend.hub *changes.Hub` (nil → `changes.ErrNotReady`), `fakeBackend.subMu sync.Mutex`, `fakeBackend.subs []*changes.Sub` (guarded by `subMu`).

**Go — `internal/webui` (Task 8)**

- `var sessionRecheck = 5 * time.Second`; `var afterFunc = time.AfterFunc`. Both are read once when a request starts.

**Web — `web/src`**

- Task 9, `src/api/live.ts`: `liveConfig: { enabled: boolean }` (true by default); `RETRY_MIN = 1000`, `RETRY_MAX = 30_000`; `interface ChangeHandlers { onHello(): void; onChange(topics: string[]): void; onAlive(): void }`; `class SseParser { constructor(h: ChangeHandlers); push(text: string): void }`; `type StreamEnd = "ended" | "unauthorized"`; `readChanges(h: ChangeHandlers, signal: AbortSignal): Promise<StreamEnd>`; `interface StreamHandlers { onHello(): void; onChange(topics: string[]): void; onEnd(): void }`; `sleep(ms: number, signal: AbortSignal): Promise<void>`; `runChanges(h: StreamHandlers, signal: AbortSignal, wait = sleep): Promise<void>`; `setLive(v: boolean): void`; `useLive(): boolean`.
- Task 9, `src/api/client.ts`: `notifyUnauthorized(): void` calls the handler `setUnauthorizedHandler` installed.
- Task 10, `src/api/use-change-stream.ts`: `TOPIC_QUERIES: Record<string, readonly QueryKey[]>`; `invalidateTopics(qc: QueryClient, topics: readonly string[]): void`; `invalidateAll(qc: QueryClient): void`; `useChangeStream(): void`. `src/test/setup.ts` sets `liveConfig.enabled = false`.
- Task 11, `src/api/hooks.ts`: `POLL_SAFETY = 60_000`; `livePoll(live: boolean, interval: number): number`.

## Assumptions (evidence)

- **Spec deviation, §2.1:** `before` (the spec's `BeforePublish`) is the second argument of `SetSources`, not an `Options` field: `Run` builds the hub before `start` builds the `Backend` whose `staleActivity` it calls (`internal/daemon/run.go:289` builds `ev`; `start` builds `b` at `run.go:214`). The behaviour the spec states is unchanged.
- **Wake window:** a wake waits the full 50 ms, drains any wake that arrived meanwhile, then checks once. The plan draft's "wait out the rest of the window since the last check" checks at once on the first wake and again after the window, which is two checks per burst and fails spec §4's "a burst of wakes inside 50 ms causes one check". The cost is at most 50 ms of latency per change.
- `sync.OnceValue` re-panics with the same value on every call after `f` panics (`/usr/local/go/src/sync/oncefunc.go:45`, read 2026-10-10), so `fingerprints` reads the manager status lazily once and a panic in it drops exactly the topics that need it.
- `http.ResponseController.SetWriteDeadline` returns a not-supported error for a writer without `SetWriteDeadline` (`/usr/local/go/src/net/http/responsecontroller.go:105-117`), which Task 6 ignores; on a real connection it overrides the listener's `WriteTimeout` (60 s on the web listener, `run.go:268`).
- `http.Server.Shutdown` starts each `RegisterOnShutdown` hook with `go f()` and does not wait for it (`/usr/local/go/src/net/http/server.go:3155-3156`), so Task 7 also calls `hub.Close()` after `teardown()` before the manager closes.
- A request's context ends only when its client disconnects, even with the web listener's `ReadTimeout`: probed on 2026-10-10 for the run details log plan (docs/superpowers/plans/2026-10-10-ghr-run-details-log-fixes.md, Assumptions). So an open stream is ended by the client, the hub, a write error, or the session re-check.
- Under the web tests' jsdom, a `Response` built from a `ReadableStream` feeds `TextDecoder` with `{ stream: true }` so a character split across chunks decodes, a cancelled reader ends a pending `read()` with `done: true`, and `document.visibilityState` is a string: probed with a throwaway Vitest file on 2026-10-10.
- TanStack Query 5.104.1: `setQueryData` resets `isInvalidated` to false (`successState`, `web/node_modules/@tanstack/query-core/build/modern/query.js:497-504`) and `Query.observers` holds the mounted observers (`query.js:232-233`), whose `options.refetchInterval` the polling tests read.
- The web tests count fetch calls in 103 assertions; `src/test/setup.ts` turns the stream off so those counts hold, and the stream tests turn it back on.
- `history.Store.Prune` rewrites the file even when it drops nothing, so a scheduled prune publishes `history` once; spec §1 counts every replaced file, and `Manager.prune` runs with maintenance, not every tick (`internal/runner/cleanup.go:289-293`).
- `Metrics()` changes once a minute, when the sampler records (`internal/daemon/run.go:211`, `sampler.Run(ctx, time.Minute)`), so `metrics` does not fire `before` on every check.
- `Status.RateLimit` is copied from the token metadata's `RateLimit` (`internal/daemon/backend.go:226-230`), the same value as `TokenStatus.RateLimit`, so the `gauges` projection carries it once.
- Dry run, 2026-10-10: every task's code from this plan, applied to `f1b67dc` in a throwaway worktree, passed `go vet ./internal/... ./cmd/... ./web/`, `go test -race` on `events`, `history`, `changes`, `api`, `webui`, `daemon` and `runner`, `go test -race -count=5 ./internal/changes/`, and `npm run typecheck`, `npm run lint` and `npm test` (86 files, 1032 tests). The only gofmt difference was struct-field realignment. After the round 1 fix, Task 7's test passed three runs under `-race`, and failed when the two `RegisterOnShutdown` lines were deleted ("still open 3 s into shutdown") and when `ev.SetOnChange(hub.Wake)` was deleted (no `events` change).
- **Spec deviation, §3.1:** the spec's `connectChanges({ onHello, onChange, onAlive, signal })` is two functions: `readChanges(handlers, signal)` reads one connection and `runChanges(handlers, signal, wait)` owns the reconnect loop, so the backoff tests inject `wait` instead of faking timers. The behaviour §3.1 states is unchanged.
- **Spec gap, §1:** the hub knows only the topics in its baseline. A topic missing from the first subscriber's baseline (its source failed at that moment) is not logged, and joins the comparison silently when it first appears; it is never published for that first appearance. Plan review round 1 rated this Minor; covering it would need the hub to know the topic list, which the spec leaves to the daemon.
- Task 7's test covers the event ring's wake (an hour-long tick leaves the wake as the only path to a published change) and the shutdown hooks (the stream must end within 3 s, under `Shutdown`'s 5 s limit). `hist.SetOnChange(hub.Wake)` has no test of its own: no `Run` test finishes a job, and the hub's tick would publish the `history` change without it.
- Task 11 checks four of the ten hooks spec §3.3 leaves unchanged (`useActions`, `useSteps`, `useContainers`, `useLogTail`). Task 11 does not edit the other six, so their intervals cannot change.
- Task 8's total is 4, so the assignment table names `impl-sonnet-medium`; it is delegated anyway because its risk is 3, the inline mode's rule for heavy tasks.
- The manual checks of spec §4 run at finishing on the owner's machine; no task performs them.
- `changes.Options` has no clock, although spec §2.1 lists one: the hub tests use short `Tick` and `WakeWindow` values instead, as the plan draft settled (docs/superpowers/notes/2026-10-10-ghr-realtime-ui-plan-draft.md, Contracts settled).
- `web/src/api/fixtures/storage.json` has an install running (`operations.current` set), so `useStorage` polls every second with it; Task 11's idle cases override it.

## Task index

1. Event ring change hook
2. History write counter
3. Change hub
4. Backend helpers for change topics
5. Daemon fingerprints and Subscribe
6. GET /changes route
7. Daemon wiring and shutdown
8. Session re-check on long requests
9. Browser stream client and live store
10. Change stream hook and invalidation
11. Polling gives way to the stream

---

### Task 1: Event ring change hook

**Files:**
- Modify: `internal/events/events.go`
- Test: `internal/events/events_test.go`

**Interfaces:**
- Consumes: none.
- Produces: `Ring.SetOnChange`, `Ring.Last` (Contracts, `internal/events`).

**Items:** 3
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 0: Create the branch**

```bash
git switch -c feat/realtime-ui master
```

- [ ] **Step 1: Write the failing tests**

Append to `internal/events/events_test.go`, and change its import line to the block shown:

```go
import (
	"slices"
	"sync"
	"sync/atomic"
	"testing"
)
```

```go
func TestRingLastAndOnChange(t *testing.T) {
	r := New()
	if r.Last() != 0 {
		t.Fatalf("empty ring: Last = %d", r.Last())
	}
	r.Add("info", "", "before any hook")
	var seen []int64
	// The hook reads the ring, which deadlocks unless Add released its lock first.
	r.SetOnChange(func() { seen = append(seen, r.Last()) })
	r.Add("info", "a", "one")
	r.Add("warn", "a", "two")
	if r.Last() != 3 || !slices.Equal(seen, []int64{2, 3}) {
		t.Fatalf("Last = %d, hook saw %v", r.Last(), seen)
	}
	r.SetOnChange(nil)
	r.Add("info", "a", "three")
	if len(seen) != 2 {
		t.Fatalf("a removed hook still ran: %v", seen)
	}
}

func TestRingHookUnderConcurrency(t *testing.T) {
	r := New()
	var calls atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				r.Add("info", "", "event %d", j)
			}
		}()
		go func() {
			defer wg.Done()
			r.SetOnChange(func() { calls.Add(1) })
			r.Last()
		}()
	}
	wg.Wait()
	if r.Last() != 200 {
		t.Fatalf("Last = %d, want 200", r.Last())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test -race ./internal/events/`
Expected: FAIL to compile with `r.Last undefined` and `r.SetOnChange undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/events/events.go`, replace the `Ring` type and `Add` with the following, and add `SetOnChange` and `Last` after `Add`:

```go
type Ring struct {
	Now      func() time.Time
	mu       sync.Mutex
	seq      int64
	items    []model.Event
	onChange func()
}
```

```go
// Add records an event and mirrors it to the process log (journald under systemd).
func (r *Ring) Add(level, repo, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	r.mu.Lock()
	r.seq++
	r.items = append(r.items, model.Event{Seq: r.seq, Time: r.Now(), Level: level, Repo: repo, Msg: msg})
	if len(r.items) > Capacity {
		r.items = r.items[len(r.items)-Capacity:]
	}
	fn := r.onChange
	r.mu.Unlock()
	log.Printf("%s %s %s", level, repo, msg)
	if fn != nil {
		fn()
	}
}

// SetOnChange installs fn, which Add calls after it releases the ring's lock;
// nil removes it.
func (r *Ring) SetOnChange(fn func()) {
	r.mu.Lock()
	r.onChange = fn
	r.mu.Unlock()
}

// Last returns the newest event's sequence number, 0 before any event.
func (r *Ring) Last() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seq
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test -race ./internal/events/ && gofmt -l internal && go vet ./internal/events/`
Expected: `ok`, no gofmt output, no vet output.

- [ ] **Step 5: Commit**

```bash
git add internal/events/events.go internal/events/events_test.go
git commit -m "feat(events): add a change hook and Last"
```

### Task 2: History write counter

**Files:**
- Modify: `internal/history/history.go`
- Test: `internal/history/history_test.go`

**Interfaces:**
- Consumes: none.
- Produces: `Store.SetOnChange`, `Store.Writes` (Contracts, `internal/history`).

**Items:** 3
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `internal/history/history_test.go`, change the import block to:

```go
import (
	"fmt"
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

Append:

```go
func TestWritesCountsOnlyRealWrites(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	var seen []int64
	// The hook reads the store, which deadlocks unless the lock was released first.
	s.SetOnChange(func() { seen = append(seen, s.Writes()) })
	if err := s.Prune(time.Now()); err != nil || s.Writes() != 0 {
		t.Fatalf("prune with no file: writes %d err %v", s.Writes(), err)
	}
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	e := model.HistoryEntry{ID: "a1", Repo: "a", RunID: 1, FinishedAt: t0}
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(e); err != nil {
		t.Fatal(err)
	}
	if s.Writes() != 1 {
		t.Fatalf("a duplicate append counted: writes %d", s.Writes())
	}
	if err := s.Prune(t0.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if s.Writes() != 2 || !slices.Equal(seen, []int64{1, 2}) {
		t.Fatalf("writes %d, hook saw %v", s.Writes(), seen)
	}
	s.SetOnChange(nil)
	if err := s.Append(model.HistoryEntry{ID: "b1", Repo: "b", RunID: 2, FinishedAt: t0}); err != nil {
		t.Fatal(err)
	}
	if s.Writes() != 3 || len(seen) != 2 {
		t.Fatalf("after removing the hook: writes %d, hook saw %v", s.Writes(), seen)
	}
}

func TestHistoryHookUnderConcurrency(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	var calls atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := s.Append(model.HistoryEntry{ID: fmt.Sprint(i), RunID: int64(i)}); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			s.SetOnChange(func() { calls.Add(1) })
			s.Writes()
		}()
	}
	wg.Wait()
	if s.Writes() != 4 {
		t.Fatalf("writes %d, want 4", s.Writes())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test -race ./internal/history/`
Expected: FAIL to compile with `s.SetOnChange undefined` and `s.Writes undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/history/history.go`, replace the `Store` type and `Append` (from `type Store struct` through the end of `Append`) with:

```go
type Store struct {
	Path     string
	mu       sync.Mutex
	writes   int64
	onChange func()
}

// SetOnChange installs fn, which Append and Prune call after a counted write,
// once the store's lock is released; nil removes it.
func (s *Store) SetOnChange(fn func()) {
	s.mu.Lock()
	s.onChange = fn
	s.mu.Unlock()
}

// Writes counts the appends that wrote a line and the prunes that replaced
// the file.
func (s *Store) Writes() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes
}

// counted bumps the write counter when wrote is set and returns the hook to
// call once s.mu is released. s.mu is held.
func (s *Store) counted(wrote bool) func() {
	if !wrote {
		return nil
	}
	s.writes++
	return s.onChange
}

// Append records e unless an entry with the same instance ID and run ID is
// already stored, so a finalization repeated after a crash adds no duplicate.
func (s *Store) Append(e model.HistoryEntry) error {
	s.mu.Lock()
	wrote, err := s.append(e)
	fn := s.counted(wrote)
	s.mu.Unlock()
	if fn != nil {
		fn()
	}
	return err
}

func (s *Store) append(e model.HistoryEntry) (bool, error) {
	all, err := s.readAll()
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
	f, err := os.OpenFile(s.Path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	// A crash mid-write can leave a fragment with no trailing newline; start a
	// new line so this record is not merged into the fragment and lost.
	st, err := f.Stat()
	if err != nil {
		return false, err
	}
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
	return true, f.Close()
}
```

Replace `Prune` (from its doc comment to the end of the file) with:

```go
// Prune drops entries that finished before cutoff, rewriting the file atomically.
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
	all, err := s.readAll()
	if err != nil || all == nil {
		return false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".history-*.jsonl")
	if err != nil {
		return false, err
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

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test -race ./internal/history/ ./internal/runner/ && gofmt -l internal && go vet ./internal/history/`
Expected: `ok` for both packages, no gofmt output, no vet output.

- [ ] **Step 5: Commit**

```bash
git add internal/history/history.go internal/history/history_test.go
git commit -m "feat(history): count writes and add a change hook"
```

### Task 3: Change hub

**Files:**
- Create: `internal/changes/hub.go`
- Test: `internal/changes/hub_test.go`

**Interfaces:**
- Consumes: none.
- Produces: the whole `internal/changes` package (Contracts, `internal/changes`).

**Items:** 3
**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

The hub's locking rules, which every function below follows: one mutex `mu` guards the subscriber set, the sources, `closed`, `stop`, `prev` and `missing`. Only the hub sends on or closes a subscriber channel, and only while holding `mu`, so a send on a closed channel cannot happen. `check` calls the sources without the lock (they read daemon state and may be slow), then takes the lock and returns at once if its own `stop` channel was closed meanwhile, because that means the last subscriber left or the hub closed and `prev` may belong to a newer watcher.

- [ ] **Step 1: Write the failing tests**

Create `internal/changes/hub_test.go`:

```go
package changes

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"
)

// source is a fake fingerprint function whose values tests change.
type source struct {
	mu    sync.Mutex
	fp    map[string]uint64
	calls int
	gate  chan struct{} // when set, get waits for it to close
}

func newSource(fp map[string]uint64) *source { return &source{fp: fp} }

func (s *source) get() map[string]uint64 {
	s.mu.Lock()
	s.calls++
	gate := s.gate
	out := maps.Clone(s.fp)
	s.mu.Unlock()
	if gate != nil {
		<-gate
	}
	return out
}

// set applies every change in one step, so one check sees them together.
func (s *source) set(kv map[string]uint64) {
	s.mu.Lock()
	maps.Copy(s.fp, kv)
	s.mu.Unlock()
}

func (s *source) drop(topic string) {
	s.mu.Lock()
	delete(s.fp, topic)
	s.mu.Unlock()
}

func (s *source) n() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// block makes later reads wait until the returned channel is closed.
func (s *source) block() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gate = make(chan struct{})
	return s.gate
}

type logs struct {
	mu    sync.Mutex
	lines []string
}

func (l *logs) logf(format string, args ...any) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

func (l *logs) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.lines)
}

const fast = 5 * time.Millisecond

func newHub(t *testing.T, tick, window time.Duration, src *source) (*Hub, *logs) {
	t.Helper()
	l := &logs{}
	h := New(Options{Tick: tick, WakeWindow: window, Logf: l.logf})
	h.SetSources(src.get, nil)
	t.Cleanup(h.Close)
	return h, l
}

func subscribe(t *testing.T, h *Hub) *Sub {
	t.Helper()
	s, err := h.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func recv(t *testing.T, s *Sub) []string {
	t.Helper()
	select {
	case got, ok := <-s.C:
		if !ok {
			t.Fatal("the channel closed")
		}
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("no topic set within 2 s")
	}
	return nil
}

func quiet(t *testing.T, s *Sub, d time.Duration) {
	t.Helper()
	select {
	case got := <-s.C:
		t.Fatalf("unexpected set %v", got)
	case <-time.After(d):
	}
}

// waitChecks waits until src has been read n more times.
func waitChecks(t *testing.T, src *source, n int) {
	t.Helper()
	target := src.n() + n
	deadline := time.Now().Add(2 * time.Second)
	for src.n() < target {
		if time.Now().After(deadline) {
			t.Fatalf("fewer than %d checks within 2 s", n)
		}
		time.Sleep(time.Millisecond)
	}
}

func waitClosed(t *testing.T, s *Sub) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-s.C:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("the channel was not closed")
		}
	}
}

func TestPublishesOnlyChangedTopics(t *testing.T) {
	src := newSource(map[string]uint64{"config": 1, "status": 1})
	h, _ := newHub(t, fast, fast, src)
	s := subscribe(t, h)
	src.set(map[string]uint64{"status": 2})
	if got := recv(t, s); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("got %v", got)
	}
	quiet(t, s, 50*time.Millisecond)
}

func TestTopicsChangedInOneCheckFormOneSet(t *testing.T) {
	src := newSource(map[string]uint64{"config": 1, "status": 1})
	h, _ := newHub(t, fast, fast, src)
	s := subscribe(t, h)
	src.set(map[string]uint64{"status": 2, "config": 2})
	if got := recv(t, s); !slices.Equal(got, []string{"config", "status"}) {
		t.Fatalf("got %v", got)
	}
}

func TestSubscribeTakesTheBaselineBeforeReturning(t *testing.T) {
	src := newSource(map[string]uint64{"status": 1})
	h, _ := newHub(t, fast, fast, src)
	s := subscribe(t, h)
	if src.n() < 1 {
		t.Fatal("Subscribe returned before reading the sources")
	}
	src.set(map[string]uint64{"status": 2})
	if got := recv(t, s); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("got %v", got)
	}
}

func TestFreshBaselineAfterTheLastSubscriberLeaves(t *testing.T) {
	src := newSource(map[string]uint64{"status": 1})
	h, _ := newHub(t, fast, fast, src)
	subscribe(t, h).Close()
	src.set(map[string]uint64{"status": 2})
	s := subscribe(t, h)
	quiet(t, s, 50*time.Millisecond)
	src.set(map[string]uint64{"status": 3})
	if got := recv(t, s); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("got %v", got)
	}
}

func TestWakeChecksBeforeTheTick(t *testing.T) {
	src := newSource(map[string]uint64{"events": 1})
	h, _ := newHub(t, time.Hour, 10*time.Millisecond, src)
	s := subscribe(t, h)
	src.set(map[string]uint64{"events": 2})
	h.Wake()
	if got := recv(t, s); !slices.Equal(got, []string{"events"}) {
		t.Fatalf("got %v", got)
	}
}

func TestBurstOfWakesCausesOneCheck(t *testing.T) {
	src := newSource(map[string]uint64{"events": 1})
	h, _ := newHub(t, time.Hour, 50*time.Millisecond, src)
	subscribe(t, h)
	before := src.n()
	for range 10 {
		h.Wake()
	}
	time.Sleep(200 * time.Millisecond)
	if got := src.n() - before; got != 1 {
		t.Fatalf("%d checks for one burst, want 1", got)
	}
}

func TestMissingTopicIsSkippedAndLoggedOnce(t *testing.T) {
	src := newSource(map[string]uint64{"config": 1, "status": 1})
	h, l := newHub(t, fast, fast, src)
	s := subscribe(t, h)
	src.drop("config")
	src.set(map[string]uint64{"status": 2})
	if got := recv(t, s); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("got %v", got)
	}
	waitChecks(t, src, 3)
	if l.count() != 1 {
		t.Fatalf("%d log lines for one missing topic, want 1", l.count())
	}
	// The topic's last fingerprint was kept, so its return with a new value publishes.
	src.set(map[string]uint64{"config": 5})
	if got := recv(t, s); !slices.Equal(got, []string{"config"}) {
		t.Fatalf("got %v", got)
	}
	src.drop("config")
	waitChecks(t, src, 3)
	if l.count() != 2 {
		t.Fatalf("%d log lines after the topic went missing again, want 2", l.count())
	}
}

func TestSubscribeRefusals(t *testing.T) {
	h := New(Options{})
	if _, err := h.Subscribe(); !errors.Is(err, ErrNotReady) {
		t.Fatalf("before SetSources: %v", err)
	}
	src := newSource(map[string]uint64{"status": 1})
	h.SetSources(src.get, nil)
	for i := range MaxSubscribers {
		if _, err := h.Subscribe(); err != nil {
			t.Fatalf("subscriber %d: %v", i+1, err)
		}
	}
	if _, err := h.Subscribe(); !errors.Is(err, ErrFull) {
		t.Fatalf("subscriber 33: %v", err)
	}
	h.Close()
	if _, err := h.Subscribe(); !errors.Is(err, ErrClosed) {
		t.Fatalf("after Close: %v", err)
	}
}

func TestFullChannelMergesSets(t *testing.T) {
	src := newSource(map[string]uint64{"config": 1, "status": 1})
	h, _ := newHub(t, fast, fast, src)
	s := subscribe(t, h)
	src.set(map[string]uint64{"status": 2})
	waitChecks(t, src, 3)
	src.set(map[string]uint64{"config": 2})
	waitChecks(t, src, 3)
	if got := recv(t, s); !slices.Equal(got, []string{"config", "status"}) {
		t.Fatalf("got %v", got)
	}
	quiet(t, s, 30*time.Millisecond)
}

func TestCloseDuringPublishAndWakeDuringClose(t *testing.T) {
	var mu sync.Mutex
	var n uint64
	h := New(Options{Tick: time.Millisecond, WakeWindow: time.Millisecond})
	h.SetSources(func() map[string]uint64 {
		mu.Lock()
		defer mu.Unlock()
		n++
		return map[string]uint64{"status": n}
	}, func([]string) {})
	var subs []*Sub
	for range 5 {
		subs = append(subs, subscribe(t, h))
	}
	stopWake := make(chan struct{})
	wakeDone := make(chan struct{})
	go func() {
		defer close(wakeDone)
		for {
			select {
			case <-stopWake:
				return
			default:
				h.Wake()
			}
		}
	}()
	time.Sleep(30 * time.Millisecond)
	closed := make(chan struct{})
	go func() {
		h.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close deadlocked")
	}
	close(stopWake)
	<-wakeDone
	for _, s := range subs {
		waitClosed(t, s)
	}
}

func TestCloseWaitsForTheWatcherAndIsIdempotent(t *testing.T) {
	src := newSource(map[string]uint64{"status": 1})
	h, _ := newHub(t, fast, fast, src)
	s := subscribe(t, h)
	gate := src.block()
	waitChecks(t, src, 1)
	closed := make(chan struct{})
	go func() {
		h.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned while a check was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(gate)
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after the check ended")
	}
	waitClosed(t, s)
	after := src.n()
	time.Sleep(30 * time.Millisecond)
	if src.n() != after {
		t.Fatal("a source ran after Close returned")
	}
	h.Close()
	s.Close()
}

func TestSubCloseTwice(t *testing.T) {
	src := newSource(map[string]uint64{"status": 1})
	h, _ := newHub(t, fast, fast, src)
	s := subscribe(t, h)
	s.Close()
	s.Close()
	waitClosed(t, s)
}

func TestBeforePublishRunsForStateTopics(t *testing.T) {
	src := newSource(map[string]uint64{"gauges": 1, "history": 1, "status": 1})
	var mu sync.Mutex
	var ran [][]string
	h := New(Options{Tick: fast, WakeWindow: fast})
	h.SetSources(src.get, func(topics []string) {
		mu.Lock()
		ran = append(ran, topics)
		mu.Unlock()
	})
	t.Cleanup(h.Close)
	s := subscribe(t, h)
	for _, topic := range []string{"gauges", "status", "history"} {
		src.set(map[string]uint64{topic: 2})
		if got := recv(t, s); !slices.Equal(got, []string{topic}) {
			t.Fatalf("got %v, want [%s]", got, topic)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 2 || !slices.Equal(ran[0], []string{"status"}) || !slices.Equal(ran[1], []string{"history"}) {
		t.Fatalf("before ran for %v", ran)
	}
}

func TestNeedsBefore(t *testing.T) {
	for _, tc := range []struct {
		topics []string
		want   bool
	}{
		{[]string{"status"}, true},
		{[]string{"config"}, true},
		{[]string{"history"}, true},
		{[]string{"metrics"}, true},
		{[]string{"events", "metrics"}, true},
		{[]string{"gauges"}, false},
		{[]string{"events", "gauges", "storage", "token"}, false},
	} {
		if got := needsBefore(tc.topics); got != tc.want {
			t.Errorf("needsBefore(%v) = %v", tc.topics, got)
		}
	}
}

func TestFingerprint(t *testing.T) {
	a, err := Fingerprint(map[string]int{"x": 1})
	b, _ := Fingerprint(map[string]int{"x": 1})
	c, _ := Fingerprint(map[string]int{"x": 2})
	if err != nil || a != b || a == c {
		t.Fatalf("a=%d b=%d c=%d err=%v", a, b, c, err)
	}
	if _, err := Fingerprint(make(chan int)); err == nil {
		t.Fatal("a value JSON cannot encode gave no error")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test -race ./internal/changes/`
Expected: FAIL to compile with `undefined: New`, `undefined: Hub` and the other package names.

- [ ] **Step 3: Write the implementation**

Create `internal/changes/hub.go`:

```go
// Package changes tells subscribers which topics of the daemon's state
// changed. A watcher compares every topic's fingerprint on each tick and soon
// after a wake, and sends the topics that differ as one set.
package changes

import (
	"encoding/json"
	"errors"
	"hash/fnv"
	"log"
	"slices"
	"sync"
	"time"
)

// MaxSubscribers bounds the open subscriptions, one per open browser stream.
const MaxSubscribers = 32

var (
	ErrNotReady = errors.New("changes: no sources yet")
	ErrFull     = errors.New("changes: too many subscribers")
	ErrClosed   = errors.New("changes: hub closed")
)

type Options struct {
	// Tick is how often the watcher compares fingerprints; zero means 250 ms.
	Tick time.Duration
	// WakeWindow is how long a wake waits for further wakes before its one
	// check; zero means 50 ms.
	WakeWindow time.Duration
	// Logf reports a topic whose fingerprint could not be computed; nil means log.Printf.
	Logf func(format string, args ...any)
}

type Hub struct {
	tick   time.Duration
	window time.Duration
	logf   func(format string, args ...any)
	wake   chan struct{}
	wg     sync.WaitGroup

	mu      sync.Mutex
	sources func() map[string]uint64
	before  func(topics []string)
	subs    map[*Sub]struct{}
	closed  bool
	// stop is closed to end the running watcher, nil while none runs; prev
	// and missing belong to that watcher.
	stop    chan struct{}
	prev    map[string]uint64
	missing map[string]bool
}

// Sub is one subscription. C receives sorted, de-duplicated topic sets and is
// closed when the subscription ends. A set is shared between subscribers, so
// a receiver must not modify it.
type Sub struct {
	C  <-chan []string
	ch chan []string
	h  *Hub
}

func New(o Options) *Hub {
	if o.Tick <= 0 {
		o.Tick = 250 * time.Millisecond
	}
	if o.WakeWindow <= 0 {
		o.WakeWindow = 50 * time.Millisecond
	}
	if o.Logf == nil {
		o.Logf = log.Printf
	}
	return &Hub{tick: o.Tick, window: o.WakeWindow, logf: o.Logf, wake: make(chan struct{}, 1), subs: map[*Sub]struct{}{}}
}

// Fingerprint is FNV-64a of v's JSON.
func Fingerprint(v any) (uint64, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}
	h := fnv.New64a()
	h.Write(data)
	return h.Sum64(), nil
}

// SetSources installs the function that computes every topic's fingerprint,
// and before, which runs ahead of sending a set that holds status, config,
// history or metrics. before runs with the hub's lock held, so it must not
// call the hub.
func (h *Hub) SetSources(sources func() map[string]uint64, before func(topics []string)) {
	h.mu.Lock()
	h.sources, h.before = sources, before
	h.mu.Unlock()
}

// Wake asks for a check before the next tick. It never blocks, so the event
// ring and the history store call it on every write.
func (h *Hub) Wake() {
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

// Subscribe opens a subscription. The first one takes the baseline
// fingerprints before it returns, so a change made right after is published.
func (h *Hub) Subscribe() (*Sub, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.closed:
		return nil, ErrClosed
	case h.sources == nil:
		return nil, ErrNotReady
	case len(h.subs) >= MaxSubscribers:
		return nil, ErrFull
	}
	if h.stop == nil {
		h.prev = h.sources()
		if h.prev == nil {
			h.prev = map[string]uint64{}
		}
		h.missing = map[string]bool{}
		h.stop = make(chan struct{})
		h.wg.Add(1)
		go h.watch(h.stop)
	}
	ch := make(chan []string, 1)
	s := &Sub{C: ch, ch: ch, h: h}
	h.subs[s] = struct{}{}
	return s, nil
}

// Close ends the subscription; the last one to leave stops the watcher and
// drops its fingerprints, so the next subscriber takes a fresh baseline.
func (s *Sub) Close() {
	h := s.h
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[s]; !ok {
		return
	}
	delete(h.subs, s)
	close(s.ch)
	if len(h.subs) == 0 && h.stop != nil {
		close(h.stop)
		h.stop, h.prev, h.missing = nil, nil, nil
	}
}

// Close ends every subscription and waits for the watcher, so no source runs
// after it returns.
func (h *Hub) Close() {
	h.mu.Lock()
	if !h.closed {
		h.closed = true
		for s := range h.subs {
			close(s.ch)
			delete(h.subs, s)
		}
		if h.stop != nil {
			close(h.stop)
			h.stop, h.prev, h.missing = nil, nil, nil
		}
	}
	h.mu.Unlock()
	h.wg.Wait()
}

func (h *Hub) watch(stop chan struct{}) {
	defer h.wg.Done()
	t := time.NewTicker(h.tick)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
		case <-h.wake:
			// Let a burst of wakes finish, then answer all of them with one check.
			select {
			case <-stop:
				return
			case <-time.After(h.window):
			}
			select {
			case <-h.wake:
			default:
			}
		}
		h.check(stop)
	}
}

func (h *Hub) check(stop chan struct{}) {
	h.mu.Lock()
	sources := h.sources
	h.mu.Unlock()
	cur := sources()
	h.mu.Lock()
	defer h.mu.Unlock()
	select {
	case <-stop:
		return
	default:
	}
	var topics []string
	for k, old := range h.prev {
		v, ok := cur[k]
		if !ok {
			if !h.missing[k] {
				h.missing[k] = true
				h.logf("changes: cannot fingerprint %s; skipping it until it can", k)
			}
			continue
		}
		delete(h.missing, k)
		if v != old {
			topics = append(topics, k)
		}
	}
	for k, v := range cur {
		h.prev[k] = v
	}
	if len(topics) == 0 {
		return
	}
	slices.Sort(topics)
	if h.before != nil && needsBefore(topics) {
		h.before(topics)
	}
	for s := range h.subs {
		s.send(topics)
	}
}

// send merges topics with a set the subscriber has not read yet, so a slow
// reader loses no topic. h.mu is held; the hub is the only sender, so the
// send cannot block once the buffer is empty.
func (s *Sub) send(topics []string) {
	select {
	case pending := <-s.ch:
		topics = union(pending, topics)
	default:
	}
	s.ch <- topics
}

func union(a, b []string) []string {
	out := slices.Concat(a, b)
	slices.Sort(out)
	return slices.Compact(out)
}

func needsBefore(topics []string) bool {
	for _, t := range topics {
		switch t {
		case "status", "config", "history", "metrics":
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test -race -count=5 ./internal/changes/ && gofmt -l internal && go vet ./internal/changes/`
Expected: `ok` (five runs, no flake), no gofmt output, no vet output.

- [ ] **Step 5: Commit**

```bash
git add internal/changes/hub.go internal/changes/hub_test.go
git commit -m "feat(changes): add the change hub"
```

### Task 4: Backend helpers for change topics

**Files:**
- Modify: `internal/daemon/backend.go` (the `Backend` struct, `activityEntry`, `Activity`, `Storage`, `Token`)
- Test: `internal/daemon/backend_test.go` (the `fakeManager` type and its `Status`, plus two new tests)

**Interfaces:**
- Consumes: none.
- Produces: `activityGen`, `staleActivity`, `tokenStatus`, `storageFrom`, and the `fakeManager` fields (Contracts, `internal/daemon`, Task 4).

**Items:** 3
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing tests**

In `internal/daemon/backend_test.go`, add `"sync/atomic"` to the import block, add these fields at the end of `fakeManager`:

```go
	now         time.Time    // Status().Now
	diskUsed    int64        // Status().DiskUsedBytes
	rate        int          // Status().RateRemaining
	statusCalls atomic.Int64 // Status calls; activity builds read it from several goroutines
```

and replace `fakeManager.Status` with:

```go
func (f *fakeManager) Status() model.Status {
	f.statusCalls.Add(1)
	return model.Status{Now: f.now, Instances: f.insts, Degraded: f.degraded != "", DegradedReason: f.degraded,
		DiskPct: f.diskPct, DiskUsedBytes: f.diskUsed, RateRemaining: f.rate}
}
```

Append these tests to `internal/daemon/backend_test.go`:

```go
// A change while an activity view is being built makes that build stale, so
// the next call rebuilds it instead of reusing it within the 5 s TTL.
func TestActivityBuiltBeforeAChangeIsNotReused(t *testing.T) {
	b, m, _ := newBackend(t)
	now := time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	ctx := context.Background()
	runs := func(a model.Activity) int {
		n := 0
		for _, l := range a.Lanes {
			n += len(l.Runs)
		}
		return n
	}
	first := true
	m.onPending = func() {
		if first {
			first = false
			b.staleActivity([]string{"history"})
		}
	}
	entry := func(id string, run int64) model.HistoryEntry {
		return model.HistoryEntry{ID: id, Repo: "darkmem", RunID: run, Conclusion: "success",
			StartedAt: now.Add(-20 * time.Minute), FinishedAt: now.Add(-10 * time.Minute)}
	}
	if a, _ := b.Activity(ctx, "1h", "", time.UTC); runs(a) != 0 {
		t.Fatalf("first build: %d runs", runs(a))
	}
	if err := b.Hist.Append(entry("a", 1)); err != nil {
		t.Fatal(err)
	}
	if a, _ := b.Activity(ctx, "1h", "", time.UTC); runs(a) != 1 {
		t.Fatal("a build that began before a change was reused after it")
	}
	if err := b.Hist.Append(entry("b", 2)); err != nil {
		t.Fatal(err)
	}
	if a, _ := b.Activity(ctx, "1h", "", time.UTC); runs(a) != 1 {
		t.Fatal("without a change the rebuilt view was not reused within 5 s")
	}
}

func TestTokenAndStorageFromOneStatus(t *testing.T) {
	b, m, _ := newBackend(t)
	st := model.Status{Degraded: true, DegradedReason: "GitHub rejected the token", DiskPct: 41}
	if ts := tokenStatus(github.TokenMeta{}, st); ts.State != "rejected" || ts.Reason != "GitHub rejected the token" {
		t.Fatalf("token %+v", ts)
	}
	if s := b.storageFrom(st); s.Docker.DiskPct != 41 {
		t.Fatalf("storage disk %d", s.Docker.DiskPct)
	}
	if n := m.statusCalls.Load(); n != 0 {
		t.Fatalf("the helpers read the manager's status %d times", n)
	}
	b.Storage()
	b.Token()
	if n := m.statusCalls.Load(); n != 2 {
		t.Fatalf("Storage and Token read the status %d times, want 2", n)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test -race ./internal/daemon/ -run 'TestActivityBuiltBeforeAChangeIsNotReused|TestTokenAndStorageFromOneStatus'`
Expected: FAIL to compile with `b.staleActivity undefined`, `undefined: tokenStatus` and `b.storageFrom undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/daemon/backend.go`, add `"sync/atomic"` to the import block. In the `Backend` struct, replace

```go
	activityMu sync.Mutex
	activity   map[string]*activityEntry
```

with

```go
	activityMu  sync.Mutex
	activity    map[string]*activityEntry
	activityGen atomic.Int64
```

Replace the `activityEntry` type with:

```go
type activityEntry struct {
	mu  sync.Mutex
	at  time.Time
	ok  bool
	gen int64 // activityGen when the build began
	val model.Activity
}
```

In `Activity`, replace

```go
	now := b.now()
	if e.ok && now.Sub(e.at) < activityTTL {
		return e.val, nil
	}
```

with

```go
	gen := b.activityGen.Load()
	now := b.now()
	if e.ok && e.gen == gen && now.Sub(e.at) < activityTTL {
		return e.val, nil
	}
```

and replace `e.val, e.at, e.ok = activity.Build(in), now, true` with `e.val, e.at, e.gen, e.ok = activity.Build(in), now, gen, true`.

Add after `Activity`:

```go
// staleActivity makes every cached activity view stale, one being built now
// included; the change hub calls it before telling browsers to refetch.
func (b *Backend) staleActivity([]string) { b.activityGen.Add(1) }
```

Replace `Storage` with:

```go
// Storage is the storage snapshot with the last prune and the disk reading,
// which the runner manager keeps.
func (b *Backend) Storage() model.Storage { return b.storageFrom(b.M.Status()) }

// storageFrom is Storage with the disk reading taken from st.
func (b *Backend) storageFrom(st model.Status) model.Storage {
	s := b.Space.Snapshot()
	s.LastPrune = b.M.LastPrune()
	s.Docker.DiskPct = st.DiskPct
	return s
}
```

Replace `Token` with:

```go
// Token reports what GitHub's responses said about the token; never the token.
func (b *Backend) Token() model.TokenStatus { return tokenStatus(b.GH.TokenMeta(), b.M.Status()) }

// tokenStatus is the token's state from GitHub's last answers and the
// manager's degraded flag in st.
func tokenStatus(meta github.TokenMeta, st model.Status) model.TokenStatus {
	ts := model.TokenStatus{State: "unverified", RateRemaining: meta.RateRemaining, RateLimit: meta.RateLimit,
		RateReset: meta.RateReset, ExpiresAt: meta.ExpiresAt}
	if !meta.CheckedAt.IsZero() {
		at := meta.CheckedAt
		ts.CheckedAt = &at
		ts.State = "ok"
		if !meta.OK {
			ts.State = "rejected"
		}
	}
	if st.Degraded {
		ts.State, ts.Reason = "rejected", st.DegradedReason
	}
	return ts
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test -race ./internal/daemon/ && gofmt -l internal && go vet ./internal/daemon/`
Expected: `ok` (the whole daemon suite, including `TestTokenStates`, `TestStorageMergesLastPruneAndDisk` and the activity tests), no gofmt output, no vet output.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/backend.go internal/daemon/backend_test.go
git commit -m "feat(daemon): add activity generations and helpers"
```

### Task 5: Daemon fingerprints and Subscribe

**Files:**
- Create: `internal/daemon/changes.go`
- Modify: `internal/daemon/backend.go` (two `Backend` fields)
- Test: `internal/daemon/changes_test.go`

**Interfaces:**
- Consumes: `changes.New`, `changes.Fingerprint`, `changes.Sub`, `changes.ErrNotReady`, `Hub.SetSources`, `Hub.Subscribe`, `Hub.Close` (Task 3); `events.Ring.Last` (Task 1); `history.Store.Writes` (Task 2); `staleActivity`, `tokenStatus`, `storageFrom`, `fakeManager.now/diskUsed/rate/statusCalls` (Task 4).
- Produces: `Backend.Changes`, `Backend.Subscribe`, `Backend.fingerprints`, `Backend.setupFlags` (Contracts, `internal/daemon`, Task 5).

**Items:** 3
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

The topic projections, from spec §1:

| Topic | Value fingerprinted |
|---|---|
| `status` | manager status with `WebSetupRequired`/`SetupPending` from `setupFlags`, and `Now`, `RateRemaining`, `RateLimit`, `DiskUsedBytes`, `DiskTotalBytes` zeroed |
| `gauges` | status `RateRemaining`, `DiskUsedBytes`, `DiskTotalBytes`; token `RateRemaining`, `RateLimit`, `RateReset`, `CheckedAt` |
| `events` | counter `Events.Last()` |
| `config` | `Config()` |
| `token` | `tokenStatus` with `RateRemaining`, `RateLimit`, `RateReset`, `CheckedAt` set to nil |
| `storage` | `storageFrom(status)` |
| `history` | counter `Hist.Writes()` |
| `metrics` | `Metrics()` |

- [ ] **Step 1: Write the failing tests**

Create `internal/daemon/changes_test.go`:

```go
package daemon

import (
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/changes"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// changed lists the topics whose fingerprints differ between two checks.
func changed(a, b map[string]uint64) []string {
	var out []string
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			out = append(out, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

func TestFingerprintsCoverEveryTopic(t *testing.T) {
	b, _, _ := newBackend(t)
	got := slices.Sorted(maps.Keys(b.fingerprints()))
	want := []string{"config", "events", "gauges", "history", "metrics", "status", "storage", "token"}
	if !slices.Equal(got, want) {
		t.Fatalf("topics %v", got)
	}
}

func TestIdleCountersChangeOnlyGauges(t *testing.T) {
	b, m, gh := newBackend(t)
	now := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	limit := 5000
	gh.meta = github.TokenMeta{CheckedAt: now, OK: true, RateLimit: &limit}
	prev := b.fingerprints()
	for i := 1; i <= 3; i++ {
		rem := limit - i
		reset := now.Add(time.Duration(i) * time.Minute)
		m.rate, m.diskUsed, m.now = 4000-i, int64(1<<30+i), now.Add(time.Duration(i)*time.Second)
		gh.meta = github.TokenMeta{CheckedAt: now.Add(time.Duration(i) * time.Second), OK: true,
			RateLimit: &limit, RateRemaining: &rem, RateReset: &reset}
		cur := b.fingerprints()
		if got := changed(prev, cur); !slices.Equal(got, []string{"gauges"}) {
			t.Fatalf("check %d changed %v", i, got)
		}
		prev = cur
	}
}

func TestStatusFingerprintIgnoresNow(t *testing.T) {
	b, m, _ := newBackend(t)
	prev := b.fingerprints()
	m.now = time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	cur := b.fingerprints()
	if got := changed(prev, cur); len(got) != 0 {
		t.Fatalf("a new Now changed %v", got)
	}
	m.insts = []model.InstanceStatus{{ID: "r", Repo: "darkmem", State: "idle"}}
	if got := changed(cur, b.fingerprints()); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("a new instance changed %v", got)
	}
}

func TestFingerprintsReadTheManagerStatusOnce(t *testing.T) {
	b, m, _ := newBackend(t)
	before := m.statusCalls.Load()
	b.fingerprints()
	if n := m.statusCalls.Load() - before; n != 1 {
		t.Fatalf("one check read the manager's status %d times", n)
	}
}

func TestStateChangesReachTheirTopics(t *testing.T) {
	b, _, _ := newBackend(t)
	prev := b.fingerprints()
	b.Events.Add("info", "", "something happened")
	cur := b.fingerprints()
	if got := changed(prev, cur); !slices.Equal(got, []string{"events"}) {
		t.Fatalf("an event changed %v", got)
	}
	if err := b.Hist.Append(model.HistoryEntry{ID: "a", Repo: "darkmem", RunID: 1}); err != nil {
		t.Fatal(err)
	}
	prev, cur = cur, b.fingerprints()
	if got := changed(prev, cur); !slices.Equal(got, []string{"history"}) {
		t.Fatalf("a history row changed %v", got)
	}
	three := 3
	if err := b.PatchConfig(model.ConfigPatch{GlobalMax: &three}); err != nil {
		t.Fatal(err)
	}
	if got := changed(cur, b.fingerprints()); !slices.Contains(got, "config") {
		t.Fatalf("a config change changed %v", got)
	}
}

type panickyStorage struct{ fakeStorage }

func (p *panickyStorage) Snapshot() model.Storage { panic("snapshot failed") }

type panickyManager struct{ *fakeManager }

func (panickyManager) Status() model.Status { panic("status failed") }

func TestAPanickingTopicIsLeftOut(t *testing.T) {
	b, m, _ := newBackend(t)
	b.Space = &panickyStorage{}
	fp := b.fingerprints()
	if _, ok := fp["storage"]; ok {
		t.Fatal("a panicking storage snapshot still has a fingerprint")
	}
	if _, ok := fp["status"]; !ok {
		t.Fatal("one topic's panic dropped another")
	}
	b.M = panickyManager{m}
	got := slices.Sorted(maps.Keys(b.fingerprints()))
	if !slices.Equal(got, []string{"config", "events", "history", "metrics"}) {
		t.Fatalf("with the manager status panicking: %v", got)
	}
}

func TestSetupFlagsAreReadOncePerSecond(t *testing.T) {
	b, _, _ := newBackend(t)
	now := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	reads := 0
	b.WebSetupRequired = func() bool { reads++; return true }
	b.SetupPending = func() bool { return false }
	b.fingerprints()
	b.fingerprints()
	now = now.Add(999 * time.Millisecond)
	b.fingerprints()
	if reads != 1 {
		t.Fatalf("%d reads within a second, want 1", reads)
	}
	now = now.Add(time.Millisecond)
	b.fingerprints()
	if reads != 2 {
		t.Fatalf("%d reads after a second, want 2", reads)
	}
}

func TestSubscribeNeedsAHub(t *testing.T) {
	b, _, _ := newBackend(t)
	if _, err := b.Subscribe(); !errors.Is(err, changes.ErrNotReady) {
		t.Fatalf("without a hub: %v", err)
	}
	hub := changes.New(changes.Options{})
	t.Cleanup(hub.Close)
	hub.SetSources(b.fingerprints, b.staleActivity)
	b.Changes = hub
	s, err := b.Subscribe()
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test -race ./internal/daemon/ -run 'Fingerprint|IdleCounters|StateChanges|PanickingTopic|SetupFlags|SubscribeNeedsAHub'`
Expected: FAIL to compile with `b.fingerprints undefined`, `b.Subscribe undefined` and `b.Changes undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/daemon/backend.go`, add `"github.com/darkraise/ghr/internal/changes"` to the import block, and in the `Backend` struct add, right after the `SetupPending` field:

```go
	// Changes is the hub GET /changes subscribes to; nil until the daemon has started.
	Changes *changes.Hub
	flags   setupFlagCache
```

Create `internal/daemon/changes.go`:

```go
package daemon

import (
	"sync"
	"time"

	"github.com/darkraise/ghr/internal/changes"
	"github.com/darkraise/ghr/internal/github"
	"github.com/darkraise/ghr/internal/model"
)

// setupFlagsTTL bounds how often the fingerprints read the password file and
// stat the setup marker; the watcher checks four times a second.
const setupFlagsTTL = time.Second

type setupFlagCache struct {
	mu          sync.Mutex
	at          time.Time
	webRequired bool
	pending     bool
}

// Subscribe opens a change subscription for GET /changes.
func (b *Backend) Subscribe() (*changes.Sub, error) {
	if b.Changes == nil {
		return nil, changes.ErrNotReady
	}
	return b.Changes.Subscribe()
}

// setupFlags returns WebSetupRequired and SetupPending, read at most once per
// setupFlagsTTL.
func (b *Backend) setupFlags() (webRequired, pending bool) {
	f := &b.flags
	f.mu.Lock()
	defer f.mu.Unlock()
	now := b.now()
	if f.at.IsZero() || now.Sub(f.at) >= setupFlagsTTL {
		f.webRequired = b.WebSetupRequired != nil && b.WebSetupRequired()
		f.pending = b.SetupPending != nil && b.SetupPending()
		f.at = now
	}
	return f.webRequired, f.pending
}

// fingerprints computes every change topic's fingerprint from one read of the
// manager's status and of the token metadata. A topic that fails or panics is
// left out; the hub skips it until it returns. A panic in a shared read drops
// every topic that needs it, because sync.OnceValue repeats the panic.
func (b *Backend) fingerprints() map[string]uint64 {
	status := sync.OnceValue(b.M.Status)
	meta := sync.OnceValue(func() github.TokenMeta {
		if b.GH == nil {
			return github.TokenMeta{}
		}
		return b.GH.TokenMeta()
	})
	token := func() model.TokenStatus { return tokenStatus(meta(), status()) }
	out := make(map[string]uint64, 8)
	hash := func(topic string, v func() any) {
		defer func() { _ = recover() }()
		if fp, err := changes.Fingerprint(v()); err == nil {
			out[topic] = fp
		}
	}
	count := func(topic string, n func() int64) {
		defer func() { _ = recover() }()
		out[topic] = uint64(n())
	}
	hash("status", func() any {
		st := status()
		st.WebSetupRequired, st.SetupPending = b.setupFlags()
		st.Now, st.RateRemaining, st.RateLimit, st.DiskUsedBytes, st.DiskTotalBytes = time.Time{}, 0, 0, 0, 0
		return st
	})
	hash("gauges", func() any {
		st, ts := status(), token()
		return []any{st.RateRemaining, st.DiskUsedBytes, st.DiskTotalBytes, ts.RateRemaining, ts.RateLimit, ts.RateReset, ts.CheckedAt}
	})
	count("events", b.Events.Last)
	hash("config", b.Config)
	hash("token", func() any {
		ts := token()
		ts.RateRemaining, ts.RateLimit, ts.RateReset, ts.CheckedAt = nil, nil, nil, nil
		return ts
	})
	hash("storage", func() any { return b.storageFrom(status()) })
	count("history", b.Hist.Writes)
	hash("metrics", func() any { return b.Metrics() })
	return out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test -race ./internal/daemon/ && gofmt -l internal && go vet ./internal/daemon/`
Expected: `ok`, no gofmt output, no vet output.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/changes.go internal/daemon/changes_test.go internal/daemon/backend.go
git commit -m "feat(daemon): fingerprint change topics"
```

### Task 6: GET /changes route

**Files:**
- Modify: `internal/api/server.go` (the `Backend` interface, `NewServer`, new helpers at the end)
- Modify: `internal/api/api_test.go` (the `fakeBackend` fake)
- Test: `internal/api/changes_test.go`

**Interfaces:**
- Consumes: `changes.Sub`, `changes.ErrNotReady/ErrFull/ErrClosed`, `changes.New`, `Hub.SetSources/Subscribe/Close`, `MaxSubscribers` (Task 3); `(*daemon.Backend).Subscribe` (Task 5) keeps `var _ api.Backend = (*Backend)(nil)` compiling.
- Produces: `api.Backend.Subscribe`, `GET /changes`, `heartbeat`, `changesWriteWindow`, `fakeBackend.hub/subMu/subs` (Contracts, `internal/api`).

**Items:** 3
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `internal/api/api_test.go`, add `"sync"` and `"github.com/darkraise/ghr/internal/changes"` to the import block, add these fields at the end of `fakeBackend`:

```go
	hub   *changes.Hub     // Subscribe's hub; nil answers changes.ErrNotReady
	subMu sync.Mutex       // guards subs
	subs  []*changes.Sub   // subscriptions Subscribe opened
```

and add the method after `AvailableRepos`:

```go
func (f *fakeBackend) Subscribe() (*changes.Sub, error) {
	if f.hub == nil {
		return nil, changes.ErrNotReady
	}
	s, err := f.hub.Subscribe()
	if err == nil {
		f.subMu.Lock()
		f.subs = append(f.subs, s)
		f.subMu.Unlock()
	}
	return s, err
}
```

Create `internal/api/changes_test.go`:

```go
package api

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/changes"
)

type topicSource struct {
	mu sync.Mutex
	fp map[string]uint64
}

func (s *topicSource) get() map[string]uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return maps.Clone(s.fp)
}

func (s *topicSource) set(topic string, v uint64) {
	s.mu.Lock()
	s.fp[topic] = v
	s.mu.Unlock()
}

func liveServer(t *testing.T) (*httptest.Server, *fakeBackend, *topicSource) {
	t.Helper()
	src := &topicSource{fp: map[string]uint64{"config": 1, "status": 1}}
	hub := changes.New(changes.Options{Tick: 10 * time.Millisecond})
	hub.SetSources(src.get, nil)
	t.Cleanup(hub.Close)
	b := &fakeBackend{hub: hub}
	srv := httptest.NewServer(NewServer(b))
	t.Cleanup(srv.Close)
	return srv, b, src
}

func openChanges(t *testing.T, srv *httptest.Server) (*http.Response, *bufio.Reader, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/changes", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		resp.Body.Close()
	})
	return resp, bufio.NewReader(resp.Body), cancel
}

// frame reads the lines up to the next blank line.
func frame(t *testing.T, rd *bufio.Reader) string {
	t.Helper()
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		var sb strings.Builder
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				ch <- result{sb.String(), err}
				return
			}
			if line == "\n" {
				ch <- result{sb.String(), nil}
				return
			}
			sb.WriteString(line)
		}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("reading a frame: %v (read %q)", r.err, r.s)
		}
		return r.s
	case <-time.After(5 * time.Second):
		t.Fatal("no frame within 5 s")
	}
	return ""
}

func TestChangesStreamsHelloAndChanges(t *testing.T) {
	srv, _, src := liveServer(t)
	resp, rd, _ := openChanges(t, srv)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for k, want := range map[string]string{"Content-Type": "text/event-stream", "Cache-Control": "no-store", "X-Accel-Buffering": "no"} {
		if got := resp.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if f := frame(t, rd); f != "event: hello\ndata: {}\n" {
		t.Fatalf("first frame %q", f)
	}
	src.set("status", 2)
	if f := frame(t, rd); f != "event: change\ndata: {\"topics\":[\"status\"]}\n" {
		t.Fatalf("change frame %q", f)
	}
}

func TestChangesPingsWhenIdle(t *testing.T) {
	old := heartbeat
	heartbeat = 50 * time.Millisecond
	t.Cleanup(func() { heartbeat = old })
	srv, _, _ := liveServer(t)
	_, rd, _ := openChanges(t, srv)
	frame(t, rd)
	if f := frame(t, rd); f != ": ping\n" {
		t.Fatalf("idle frame %q", f)
	}
}

func TestChangesRefusals(t *testing.T) {
	notReady := changes.New(changes.Options{})
	full := changes.New(changes.Options{})
	full.SetSources(func() map[string]uint64 { return map[string]uint64{} }, nil)
	for range changes.MaxSubscribers {
		if _, err := full.Subscribe(); err != nil {
			t.Fatal(err)
		}
	}
	closed := changes.New(changes.Options{})
	closed.Close()
	t.Cleanup(full.Close)
	for _, tc := range []struct {
		name string
		hub  *changes.Hub
		want string
	}{
		{"no hub", nil, "not ready"},
		{"no sources", notReady, "not ready"},
		{"full", full, "too many live connections"},
		{"closed", closed, "shutting down"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(NewServer(&fakeBackend{hub: tc.hub}))
			defer srv.Close()
			resp, err := http.Get(srv.URL + "/changes")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var body map[string]string
			json.NewDecoder(resp.Body).Decode(&body)
			if resp.StatusCode != http.StatusServiceUnavailable || body["error"] != tc.want {
				t.Fatalf("%d %v", resp.StatusCode, body)
			}
		})
	}
}

func TestChangesEndsWhenTheClientLeaves(t *testing.T) {
	srv, b, _ := liveServer(t)
	_, rd, cancel := openChanges(t, srv)
	frame(t, rd)
	cancel()
	b.subMu.Lock()
	sub := b.subs[0]
	b.subMu.Unlock()
	select {
	case _, ok := <-sub.C:
		if ok {
			t.Fatal("a set arrived instead of the subscription closing")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the subscription outlived its client")
	}
}

func TestChangesEndsWhenTheHubCloses(t *testing.T) {
	srv, b, _ := liveServer(t)
	_, rd, _ := openChanges(t, srv)
	frame(t, rd)
	b.hub.Close()
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(rd)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the stream ended with %v, want a clean end", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stream outlived the hub")
	}
}

// plainWriter has Flush but no write deadline, like a test recorder.
type plainWriter struct {
	mu  sync.Mutex
	h   http.Header
	buf strings.Builder
}

func (p *plainWriter) Header() http.Header { return p.h }
func (p *plainWriter) WriteHeader(int)     {}
func (p *plainWriter) Flush()              {}

func (p *plainWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.Write(b)
}

func (p *plainWriter) body() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.String()
}

func TestChangesStreamWithoutWriteDeadlines(t *testing.T) {
	src := &topicSource{fp: map[string]uint64{"status": 1}}
	hub := changes.New(changes.Options{Tick: 10 * time.Millisecond})
	hub.SetSources(src.get, nil)
	t.Cleanup(hub.Close)
	ctx, cancel := context.WithCancel(context.Background())
	w := &plainWriter{h: http.Header{}}
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/changes", nil)
	done := make(chan struct{})
	go func() {
		NewServer(&fakeBackend{hub: hub}).ServeHTTP(w, req)
		close(done)
	}()
	waitBody := func(want string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !strings.Contains(w.body(), want) {
			if time.Now().After(deadline) {
				t.Fatalf("body %q never held %q", w.body(), want)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitBody("event: hello\n")
	src.set("status", 2)
	waitBody("event: change\n")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler did not return after its request ended")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test -race ./internal/api/ -run Changes`
Expected: FAIL — `undefined: heartbeat` at compile time.

- [ ] **Step 3: Write the implementation**

In `internal/api/server.go`, add `"github.com/darkraise/ghr/internal/changes"` to the import block, and add as the last method of the `Backend` interface:

```go
	Subscribe() (*changes.Sub, error)
```

In `NewServer`, after the `GET /events` route, add:

```go
	mux.HandleFunc("GET /changes", func(w http.ResponseWriter, r *http.Request) {
		sub, err := b.Subscribe()
		if err != nil {
			respond(w, nil, changesErr(err))
			return
		}
		defer sub.Close()
		streamChanges(w, r, sub)
	})
```

At the end of the file, add:

```go
// heartbeat is how often an idle change stream sends a comment, so the
// browser and any proxy see the connection alive; tests shorten it.
var heartbeat = 15 * time.Second

// changesWriteWindow bounds each write to a change stream. Every write
// extends it, so the web listener's WriteTimeout never ends a stream.
var changesWriteWindow = 30 * time.Second

func changesErr(err error) error {
	switch {
	case errors.Is(err, changes.ErrNotReady):
		return &Error{Status: http.StatusServiceUnavailable, Msg: "not ready"}
	case errors.Is(err, changes.ErrFull):
		return &Error{Status: http.StatusServiceUnavailable, Msg: "too many live connections"}
	case errors.Is(err, changes.ErrClosed):
		return &Error{Status: http.StatusServiceUnavailable, Msg: "shutting down"}
	}
	return err
}

// streamChanges sends the subscription's topic sets as server-sent events
// until the client leaves, the hub ends the subscription, or a write fails.
func streamChanges(w http.ResponseWriter, r *http.Request, sub *changes.Sub) {
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	send := func(frame string) bool {
		// The Unix socket and test writers have no write deadline.
		_ = rc.SetWriteDeadline(time.Now().Add(changesWriteWindow))
		if _, err := io.WriteString(w, frame); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	if !send("event: hello\ndata: {}\n\n") {
		return
	}
	ping := time.NewTicker(heartbeat)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case topics, ok := <-sub.C:
			if !ok {
				return
			}
			data, _ := json.Marshal(struct {
				Topics []string `json:"topics"`
			}{topics})
			if !send("event: change\ndata: " + string(data) + "\n\n") {
				return
			}
			ping.Reset(heartbeat)
		case <-ping.C:
			if !send(": ping\n\n") {
				return
			}
		}
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test -race ./internal/api/ ./internal/daemon/ && gofmt -l internal && go vet ./internal/api/ ./internal/daemon/`
Expected: `ok` for both packages, no gofmt output, no vet output.

- [ ] **Step 5: Commit**

```bash
git add internal/api/server.go internal/api/api_test.go internal/api/changes_test.go
git commit -m "feat(api): stream changes on GET /changes"
```

### Task 7: Daemon wiring and shutdown

**Files:**
- Modify: `internal/daemon/run.go` (`start`, `Run`)
- Test: `internal/daemon/run_test.go`

**Interfaces:**
- Consumes: `changes.New`, `Hub.Wake`, `Hub.SetSources`, `Hub.Close` (Task 3); `Ring.SetOnChange` (Task 1); `Store.SetOnChange` (Task 2); `staleActivity` (Task 4); `Backend.Changes`, `fingerprints` (Task 5); `GET /changes` (Task 6).
- Produces: the new `start` signature (Contracts, `internal/daemon`, Task 7).

**Items:** 3
**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 3 = 6

- [ ] **Step 1: Write the failing test**

In `internal/daemon/run_test.go`, add `"bufio"` and `"github.com/darkraise/ghr/internal/changes"` to the import block and append:

```go
// A configured daemon streams changes on its socket, and shutting it down
// ends an open stream instead of waiting on it.
func TestRunStreamsChangesAndEndsThemOnShutdown(t *testing.T) {
	// With an hour-long tick, only the event ring's wake can publish the change.
	old := hubOptions
	hubOptions = changes.Options{Tick: time.Hour}
	t.Cleanup(func() { hubOptions = old })
	o := testOptions(t, cfgYAML)
	reload := make(chan os.Signal, 1)
	o.Reload = reload
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, o) }()
	returned := false
	defer func() {
		cancel()
		if !returned {
			select {
			case <-done:
			case <-time.After(10 * time.Second):
			}
		}
	}()
	c := api.NewUnixClient(o.Socket)
	waitFor(t, "the socket to serve status", func() bool { _, err := c.Status(context.Background()); return err == nil })

	stream := &http.Client{Transport: c.HTTP.Transport}
	req, err := http.NewRequest(http.MethodGet, "http://ghr/changes", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := stream.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /changes: %d", resp.StatusCode)
	}
	frames := make(chan string, 16)
	go func() {
		defer close(frames)
		rd := bufio.NewReader(resp.Body)
		var sb strings.Builder
		for {
			line, err := rd.ReadString('\n')
			if err != nil {
				return
			}
			if line == "\n" {
				frames <- sb.String()
				sb.Reset()
				continue
			}
			sb.WriteString(line)
		}
	}()
	next := func(what string) string {
		t.Helper()
		select {
		case f, ok := <-frames:
			if !ok {
				t.Fatalf("the stream ended while waiting for %s", what)
			}
			return f
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out waiting for %s", what)
		}
		return ""
	}
	if f := next("hello"); f != "event: hello\ndata: {}\n" {
		t.Fatalf("first frame %q", f)
	}
	reload <- os.Interrupt
	for !strings.Contains(next(`a change naming "events"`), `"events"`) {
	}

	cancel()
	// Shutdown gives up on open connections after 5 s; the stream must end
	// well before that, through the hub closing it.
	deadline := time.After(3 * time.Second)
	for closed := false; !closed; {
		select {
		case _, ok := <-frames:
			closed = !ok
		case <-deadline:
			t.Fatal("the change stream was still open 3 s into shutdown")
		}
	}
	select {
	case err := <-done:
		returned = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return within 10 s")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 300 go test -race ./internal/daemon/ -run TestRunStreamsChangesAndEndsThemOnShutdown`
Expected: FAIL to compile with `undefined: hubOptions`. (Were it declared, the request would answer `GET /changes: 503`: the daemon has no hub yet, so `Backend.Subscribe` answers `ErrNotReady`.)

- [ ] **Step 3: Write the implementation**

In `internal/daemon/run.go`, add `"github.com/darkraise/ghr/internal/changes"` to the import block.

Change `start`'s signature to:

```go
func start(ctx context.Context, o Options, store *Store, ev *events.Ring, hub *changes.Hub, epoch string, webCfg config.Web, webSetupRequired func() bool) (*running, error) {
```

In `start`, right after `hist := &history.Store{Path: o.HistoryPath}`, add:

```go
	hist.SetOnChange(hub.Wake)
```

In the `b := &Backend{...}` literal, add the field `Changes: hub,` after `SetupPending: …,`, and between the end of that literal and `return &running{...}`, add:

```go
	hub.SetSources(b.fingerprints, b.staleActivity)
```

Above `type Options struct`, add:

```go
// hubOptions configures the daemon's change hub; the zero value takes the
// hub's defaults. Tests lengthen the tick so only wakes publish.
var hubOptions changes.Options
```

In `Run`, replace `ev := events.New()` with:

```go
	ev := events.New()
	// Built before any listener serves, so both servers can end open change
	// streams when they shut down, during first-run setup too.
	hub := changes.New(hubOptions)
	ev.SetOnChange(hub.Wake)
	srv.RegisterOnShutdown(hub.Close)
	webSrv.RegisterOnShutdown(hub.Close)
```

Change the call `rt, err := start(ctx, o, store, ev, epoch, webCfg, webSetupRequired)` to:

```go
	rt, err := start(ctx, o, store, ev, hub, epoch, webCfg, webSetupRequired)
```

In the configured loop's shutdown branch, replace

```go
		case <-ctx.Done():
			teardown()
			m.Close()
```

with

```go
		case <-ctx.Done():
			teardown()
			// Shutdown starts its hooks without waiting for them, and no
			// fingerprint may read the manager once it closes.
			hub.Close()
			m.Close()
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 600 go test -race ./internal/daemon/ ./internal/api/ && gofmt -l internal && go vet ./internal/daemon/`
Expected: `ok` for both packages (the whole daemon suite, including the existing `TestRun…` tests), no gofmt output, no vet output.

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/run.go internal/daemon/run_test.go
git commit -m "feat(daemon): wire the change hub into Run"
```

### Task 8: Session re-check on long requests

**Files:**
- Modify: `internal/webui/handler.go` (`requireSession`, two package variables, imports)
- Test: `internal/webui/handler_test.go`

**Interfaces:**
- Consumes: none.
- Produces: `sessionRecheck`, `afterFunc` (Contracts, `internal/webui`).

**Items:** 3
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 3 = 4

- [ ] **Step 1: Write the failing tests**

Append to `internal/webui/handler_test.go`:

```go
// waitAPI holds every request open until its context ends.
type waitAPI struct {
	started chan struct{}
	ended   chan struct{}
}

func (a *waitAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	close(a.started)
	<-r.Context().Done()
	close(a.ended)
}

func setSessionRecheck(t *testing.T, d time.Duration) {
	t.Helper()
	old := sessionRecheck
	sessionRecheck = d
	t.Cleanup(func() { sessionRecheck = old })
}

func TestLongRequestEndsWithItsSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(a *Auth, c *clock, id string)
	}{
		{"logout", func(a *Auth, _ *clock, id string) { a.Logout(id) }},
		{"expiry", func(_ *Auth, c *clock, _ string) { c.add(sessionTTL) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setSessionRecheck(t, 20*time.Millisecond)
			a, c := newTestAuth(t)
			id, err := a.Setup(pw)
			if err != nil {
				t.Fatal(err)
			}
			api := &waitAPI{started: make(chan struct{}), ended: make(chan struct{})}
			h := Handler(a, api, testFS(), []string{"ghr.lan"})
			returned := make(chan struct{})
			go func() {
				do(h, http.MethodGet, "/api/changes", "", withCookie(id))
				close(returned)
			}()
			select {
			case <-api.started:
			case <-time.After(2 * time.Second):
				t.Fatal("the request never reached the API")
			}
			select {
			case <-api.ended:
				t.Fatal("a live session's request was cancelled")
			case <-time.After(100 * time.Millisecond):
			}
			tc.end(a, c, id)
			select {
			case <-api.ended:
			case <-time.After(2 * time.Second):
				t.Fatal("the request outlived its session")
			}
			<-returned
		})
	}
}

func TestRequestStopsItsSessionTimer(t *testing.T) {
	var timers []*time.Timer
	afterFunc = func(d time.Duration, f func()) *time.Timer {
		tm := time.AfterFunc(d, f)
		timers = append(timers, tm)
		return tm
	}
	t.Cleanup(func() { afterFunc = time.AfterFunc })
	h, a, _ := newTestHandler(t)
	id, err := a.Setup(pw)
	if err != nil {
		t.Fatal(err)
	}
	if rec := do(h, http.MethodGet, "/api/status", "", withCookie(id)); rec.Code != http.StatusOK {
		t.Fatalf("status: %d", rec.Code)
	}
	if len(timers) != 1 {
		t.Fatalf("%d timers for one request, want 1", len(timers))
	}
	if timers[0].Stop() {
		t.Fatal("the request returned with its re-check timer still armed")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test -race ./internal/webui/ -run 'TestLongRequestEndsWithItsSession|TestRequestStopsItsSessionTimer'`
Expected: FAIL to compile with `undefined: sessionRecheck` and `undefined: afterFunc`.

- [ ] **Step 3: Write the implementation**

In `internal/webui/handler.go`, add `"context"` and `"sync"` to the import block. After the `const (...)` block, add:

```go
// sessionRecheck is how often an /api request still running checks that its
// session exists, so a logout ends an open change stream; tests shorten it.
var sessionRecheck = 5 * time.Second

// afterFunc is time.AfterFunc; tests replace it to see the timers.
var afterFunc = time.AfterFunc
```

Replace `requireSession` with:

```go
// requireSession serves next only for a logged-in session, and cancels the
// request's context once that session ends: logout, a password change or
// reset, or expiry.
func (h *handler) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := h.session(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "not logged in")
			return
		}
		every, after := sessionRecheck, afterFunc
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		var mu sync.Mutex
		done := false
		var timer *time.Timer
		var check func()
		check = func() {
			mu.Lock()
			defer mu.Unlock()
			if done {
				return
			}
			if !h.auth.Valid(id) {
				cancel()
				return
			}
			timer = after(every, check)
		}
		mu.Lock()
		timer = after(every, check)
		mu.Unlock()
		defer func() {
			mu.Lock()
			done = true
			timer.Stop()
			mu.Unlock()
		}()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `timeout 300 go test -race ./internal/webui/ && gofmt -l internal && go vet ./internal/webui/`
Expected: `ok` (the whole webui suite), no gofmt output, no vet output.

- [ ] **Step 5: Commit**

```bash
git add internal/webui/handler.go internal/webui/handler_test.go
git commit -m "feat(webui): recheck sessions on long requests"
```

### Task 9: Browser stream client and live store

**Files:**
- Create: `web/src/api/live.ts`
- Modify: `web/src/api/client.ts` (export `notifyUnauthorized`)
- Test: `web/src/api/live.test.ts`

**Interfaces:**
- Consumes: `GET /changes` wire format (Global Constraints); `setUnauthorizedHandler` (existing, `client.ts:44`).
- Produces: everything listed under Contracts, Task 9.

**Items:** 3
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Create `web/src/api/live.test.ts`:

```ts
import { act, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { json, mockApi } from "@/test/api"
import { setUnauthorizedHandler } from "./client"
import { readChanges, RETRY_MAX, RETRY_MIN, runChanges, setLive, SseParser, useLive } from "./live"

const enc = new TextEncoder()

function recorder() {
  const seen: string[] = []
  return {
    seen,
    handlers: {
      onHello: () => {
        seen.push("hello")
      },
      onChange: (topics: string[]) => {
        seen.push(`change ${topics.join(",")}`)
      },
      onAlive: () => {
        seen.push("alive")
      },
    },
  }
}

function stream(chunks: (string | Uint8Array)[]): Response {
  return new Response(
    new ReadableStream<Uint8Array>({
      start(c) {
        for (const ch of chunks) c.enqueue(typeof ch === "string" ? enc.encode(ch) : ch)
        c.close()
      },
    }),
    { status: 200, headers: { "Content-Type": "text/event-stream" } },
  )
}

const noop = { onHello: () => {}, onChange: () => {}, onEnd: () => {} }

afterEach(() => {
  setUnauthorizedHandler(() => {})
  setLive(false)
})

describe("SseParser", () => {
  it("joins an event split across chunks", () => {
    const { seen, handlers } = recorder()
    const p = new SseParser(handlers)
    p.push("event: cha")
    p.push('nge\ndata: {"topics":["sta')
    p.push('tus"]}\n')
    p.push("\n")
    expect(seen).toEqual(["change status"])
  })

  it("reads two events in one chunk with CRLF line endings", () => {
    const { seen, handlers } = recorder()
    new SseParser(handlers).push('event: hello\r\ndata: {}\r\n\r\nevent: change\r\ndata: {"topics":["config","status"]}\r\n\r\n')
    expect(seen).toEqual(["hello", "change config,status"])
  })

  it("calls onAlive for a comment line", () => {
    const { seen, handlers } = recorder()
    new SseParser(handlers).push(": ping\n\n")
    expect(seen).toEqual(["alive"])
  })

  it("strips one leading space and joins data lines with LF", () => {
    const { seen, handlers } = recorder()
    new SseParser(handlers).push('event: change\ndata: {"topics":\ndata:["status"]}\n\n')
    expect(seen).toEqual(["change status"])
  })

  it("ignores unknown events and unparseable change data", () => {
    const { seen, handlers } = recorder()
    new SseParser(handlers).push('event: other\ndata: {}\n\nevent: change\ndata: not json\n\nevent: change\ndata: {"topics":[1]}\n\n')
    expect(seen).toEqual([])
  })
})

describe("readChanges", () => {
  it("decodes a character split across chunks and sends X-GHR", async () => {
    const bytes = enc.encode('event: change\ndata: {"topics":["café"]}\n\n')
    const cut = bytes.indexOf(0xc3) + 1
    const { fetchMock } = mockApi({ "GET /api/changes": () => stream([bytes.slice(0, cut), bytes.slice(cut)]) })
    const { seen, handlers } = recorder()
    expect(await readChanges(handlers, new AbortController().signal)).toBe("ended")
    expect(seen).toEqual(["change café"])
    expect(new Headers(fetchMock.mock.calls[0]?.[1]?.headers).get("X-GHR")).toBe("1")
  })

  it("reports a 401 to the unauthorized handler", async () => {
    const onUnauthorized = vi.fn()
    setUnauthorizedHandler(onUnauthorized)
    mockApi({ "GET /api/changes": () => json({ error: "not logged in" }, 401) })
    expect(await readChanges(recorder().handlers, new AbortController().signal)).toBe("unauthorized")
    expect(onUnauthorized).toHaveBeenCalledOnce()
  })

  it("ends a 503 without reading it as events", async () => {
    mockApi({ "GET /api/changes": () => json({ error: "not ready" }, 503) })
    const { seen, handlers } = recorder()
    expect(await readChanges(handlers, new AbortController().signal)).toBe("ended")
    expect(seen).toEqual([])
  })

  it("cancels the reader on abort", async () => {
    let cancelled = false
    mockApi({
      "GET /api/changes": () =>
        new Response(
          new ReadableStream<Uint8Array>({
            start(c) {
              c.enqueue(enc.encode("event: hello\ndata: {}\n\n"))
            },
            cancel() {
              cancelled = true
            },
          }),
        ),
    })
    const ctrl = new AbortController()
    const { handlers } = recorder()
    const end = await readChanges({ ...handlers, onHello: () => ctrl.abort() }, ctrl.signal)
    expect(end).toBe("ended")
    expect(cancelled).toBe(true)
  })
})

describe("runChanges", () => {
  it("backs off from 1 s, doubling to 30 s, ending each connection", async () => {
    mockApi({ "GET /api/changes": () => json({ error: "not ready" }, 503) })
    const ctrl = new AbortController()
    const waits: number[] = []
    let ends = 0
    const wait = async (ms: number) => {
      waits.push(ms)
      if (waits.length === 7) ctrl.abort()
    }
    await runChanges({ ...noop, onEnd: () => ends++ }, ctrl.signal, wait)
    expect(waits).toEqual([RETRY_MIN, 2000, 4000, 8000, 16000, RETRY_MAX, RETRY_MAX])
    expect(ends).toBe(7)
  })

  it("resets the delay only after a change or a ping", async () => {
    const answers = [
      () => json({ error: "not ready" }, 503),
      () => json({ error: "not ready" }, 503),
      () => stream(["event: hello\ndata: {}\n\n"]),
      () => stream(["event: hello\ndata: {}\n\n: ping\n\n"]),
      () => stream(['event: hello\ndata: {}\n\nevent: change\ndata: {"topics":["status"]}\n\n']),
    ]
    let n = 0
    mockApi({ "GET /api/changes": () => (answers[n++] ?? (() => json({ error: "not ready" }, 503)))() })
    const ctrl = new AbortController()
    const waits: number[] = []
    const wait = async (ms: number) => {
      waits.push(ms)
      if (waits.length === 5) ctrl.abort()
    }
    await runChanges(noop, ctrl.signal, wait)
    expect(waits).toEqual([1000, 2000, 4000, 1000, 1000])
  })

  it("stops after a 401", async () => {
    const { calls } = mockApi({ "GET /api/changes": () => json({ error: "not logged in" }, 401) })
    const waits: number[] = []
    await runChanges(noop, new AbortController().signal, async (ms) => {
      waits.push(ms)
    })
    expect(waits).toEqual([])
    expect(calls).toHaveLength(1)
  })

  it("does not reconnect after an abort", async () => {
    const { calls } = mockApi({
      "GET /api/changes": () =>
        new Response(
          new ReadableStream<Uint8Array>({
            start(c) {
              c.enqueue(enc.encode("event: hello\ndata: {}\n\n"))
            },
          }),
        ),
    })
    const ctrl = new AbortController()
    const waits: number[] = []
    await runChanges({ ...noop, onHello: () => ctrl.abort() }, ctrl.signal, async (ms) => {
      waits.push(ms)
    })
    expect(waits).toEqual([])
    expect(calls).toHaveLength(1)
  })
})

describe("useLive", () => {
  it("follows setLive", () => {
    const { result } = renderHook(() => useLive())
    expect(result.current).toBe(false)
    act(() => setLive(true))
    expect(result.current).toBe(true)
    act(() => setLive(false))
    expect(result.current).toBe(false)
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (from `web/`): `timeout 300 npx vitest run src/api/live.test.ts`
Expected: FAIL with `Failed to resolve import "./live"`.

- [ ] **Step 3: Write the implementation**

In `web/src/api/client.ts`, add after `setUnauthorizedHandler`:

```ts
export function notifyUnauthorized(): void {
  onUnauthorized()
}
```

Create `web/src/api/live.ts`:

```ts
import { useSyncExternalStore } from "react"
import { notifyUnauthorized } from "./client"

// Tests turn the stream off so their fetch mocks see only the queries.
export const liveConfig = { enabled: true }

export const RETRY_MIN = 1000
export const RETRY_MAX = 30_000

export interface ChangeHandlers {
  onHello: () => void
  onChange: (topics: string[]) => void
  onAlive: () => void
}

// SseParser reads the server-sent events of GET /changes from decoded text
// that may split anywhere, a line or a field included.
export class SseParser {
  private readonly h: ChangeHandlers
  private buf = ""
  private event = ""
  private data: string[] = []

  constructor(h: ChangeHandlers) {
    this.h = h
  }

  push(text: string): void {
    this.buf += text
    for (;;) {
      const i = this.buf.indexOf("\n")
      if (i < 0) return
      let line = this.buf.slice(0, i)
      this.buf = this.buf.slice(i + 1)
      if (line.endsWith("\r")) line = line.slice(0, -1)
      this.line(line)
    }
  }

  private line(line: string): void {
    if (line === "") {
      this.dispatch()
      return
    }
    if (line.startsWith(":")) {
      this.h.onAlive()
      return
    }
    const colon = line.indexOf(":")
    const field = colon < 0 ? line : line.slice(0, colon)
    let value = colon < 0 ? "" : line.slice(colon + 1)
    if (value.startsWith(" ")) value = value.slice(1)
    if (field === "event") this.event = value
    else if (field === "data") this.data.push(value)
  }

  private dispatch(): void {
    const event = this.event
    const data = this.data.join("\n")
    this.event = ""
    this.data = []
    if (event === "hello") {
      this.h.onHello()
    } else if (event === "change") {
      let topics: unknown
      try {
        topics = (JSON.parse(data) as { topics?: unknown }).topics
      } catch {
        return
      }
      if (Array.isArray(topics) && topics.every((t) => typeof t === "string")) this.h.onChange(topics)
    }
  }
}

export type StreamEnd = "ended" | "unauthorized"

// readChanges reads one connection to the change stream until it ends. An
// abort through signal cancels the reader and ends it like any other end.
export async function readChanges(h: ChangeHandlers, signal: AbortSignal): Promise<StreamEnd> {
  let res: Response
  try {
    res = await fetch("/api/changes", { headers: { "X-GHR": "1" }, credentials: "same-origin", signal })
  } catch {
    return "ended"
  }
  if (res.status === 401) {
    notifyUnauthorized()
    return "unauthorized"
  }
  if (!res.ok || !res.body) {
    void res.body?.cancel()
    return "ended"
  }
  const reader = res.body.getReader()
  const cancel = () => {
    reader.cancel().catch(() => {})
  }
  signal.addEventListener("abort", cancel)
  if (signal.aborted) cancel()
  const decoder = new TextDecoder()
  const parser = new SseParser(h)
  try {
    for (;;) {
      const { done, value } = await reader.read()
      if (done) break
      parser.push(decoder.decode(value, { stream: true }))
    }
  } catch {
    // a dropped connection ends the stream like a clean end
  } finally {
    signal.removeEventListener("abort", cancel)
  }
  return "ended"
}

export interface StreamHandlers {
  onHello: () => void
  onChange: (topics: string[]) => void
  // onEnd runs after every connection ends, before any wait to reconnect.
  onEnd: () => void
}

export function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const done = () => {
      clearTimeout(timer)
      signal.removeEventListener("abort", done)
      resolve()
    }
    const timer = setTimeout(done, ms)
    signal.addEventListener("abort", done)
  })
}

// runChanges keeps the change stream open until signal aborts or the session
// ends. The delay resets only after a connection delivered a change or a
// ping, so a proxy that drops right after hello cannot cause a 1 s loop.
export async function runChanges(h: StreamHandlers, signal: AbortSignal, wait = sleep): Promise<void> {
  let delay = RETRY_MIN
  while (!signal.aborted) {
    const conn = { alive: false }
    const end = await readChanges(
      {
        onHello: h.onHello,
        onChange: (topics) => {
          conn.alive = true
          h.onChange(topics)
        },
        onAlive: () => {
          conn.alive = true
        },
      },
      signal,
    )
    h.onEnd()
    if (end === "unauthorized" || signal.aborted) return
    if (conn.alive) delay = RETRY_MIN
    await wait(delay, signal)
    delay = Math.min(delay * 2, RETRY_MAX)
  }
}

let live = false
const listeners = new Set<() => void>()

export function setLive(v: boolean): void {
  if (live === v) return
  live = v
  for (const l of listeners) l()
}

function subscribe(l: () => void): () => void {
  listeners.add(l)
  return () => {
    listeners.delete(l)
  }
}

// useLive is true from a hello until the stream ends or the tab is hidden.
export function useLive(): boolean {
  return useSyncExternalStore(subscribe, () => live, () => false)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run (from `web/`): `timeout 300 npx vitest run src/api/live.test.ts && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: all `live.test.ts` tests pass; typecheck and lint print no errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/api/live.ts web/src/api/live.test.ts web/src/api/client.ts
git commit -m "feat(web): add the change stream client"
```

### Task 10: Change stream hook and invalidation

**Files:**
- Create: `web/src/api/use-change-stream.ts`
- Modify: `web/src/components/shell.tsx`, `web/src/test/setup.ts`
- Test: `web/src/api/use-change-stream.test.tsx`, `web/src/components/shell.test.tsx`

**Interfaces:**
- Consumes: `liveConfig`, `runChanges`, `setLive`, `useLive` (Task 9); `keys` (existing, `hooks.ts:12`).
- Produces: `TOPIC_QUERIES`, `invalidateTopics`, `invalidateAll`, `useChangeStream`; the test setup's `liveConfig.enabled = false` (Contracts, Task 10).

**Items:** 3
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

Spec §3.2's table, which `TOPIC_QUERIES` encodes:

| Topic | Queries invalidated |
|---|---|
| `status` | `keys.status`, every `["activity"]` query |
| `gauges` | `keys.status`, `keys.token` |
| `events` | every `["events"]` query |
| `config` | `keys.config`, every `["activity"]` query |
| `token` | `keys.token` |
| `storage` | `keys.storage` |
| `history` | every `["history"]` and `["activity"]` query |
| `metrics` | `keys.metrics`, every `["activity"]` query |

- [ ] **Step 1: Write the failing tests**

Create `web/src/api/use-change-stream.test.tsx`:

```tsx
import type { QueryClient } from "@tanstack/react-query"
import { act, renderHook, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { json, mockApi } from "@/test/api"
import { withQuery } from "@/test/query"
import { keys } from "./hooks"
import { liveConfig, setLive, useLive } from "./live"
import { invalidateTopics, TOPIC_QUERIES, useChangeStream } from "./use-change-stream"

// One query per daemon-state view, plus a GitHub-backed one no topic touches.
const seeded = [
  keys.status,
  keys.config,
  keys.metrics,
  keys.token,
  keys.storage,
  keys.activity("1h", "UTC", ""),
  keys.history("", "", ""),
  keys.events("e1"),
  keys.actions,
]

function seed(qc: QueryClient) {
  for (const k of seeded) qc.setQueryData(k, {})
}

function invalidated(qc: QueryClient): string[] {
  return qc
    .getQueryCache()
    .findAll()
    .filter((q) => q.state.isInvalidated)
    .map((q) => String(q.queryKey[0]))
    .sort()
}

const ALL = ["activity", "config", "events", "history", "metrics", "status", "storage", "token"]

describe("invalidateTopics", () => {
  it.each<[string, string[]]>([
    ["status", ["activity", "status"]],
    ["gauges", ["status", "token"]],
    ["events", ["events"]],
    ["config", ["activity", "config"]],
    ["token", ["token"]],
    ["storage", ["storage"]],
    ["history", ["activity", "history"]],
    ["metrics", ["activity", "metrics"]],
  ])("%s invalidates exactly its queries", (topic, want) => {
    const { queryClient } = withQuery()
    seed(queryClient)
    invalidateTopics(queryClient, [topic])
    expect(invalidated(queryClient)).toEqual(want)
  })

  it("ignores an unknown topic", () => {
    const { queryClient } = withQuery()
    seed(queryClient)
    invalidateTopics(queryClient, ["nope"])
    expect(invalidated(queryClient)).toEqual([])
  })

  it("names every topic", () => {
    expect(Object.keys(TOPIC_QUERIES).sort()).toEqual(["config", "events", "gauges", "history", "metrics", "status", "storage", "token"])
  })
})

function controlled() {
  const enc = new TextEncoder()
  let ctrl: ReadableStreamDefaultController<Uint8Array> | undefined
  const response = new Response(
    new ReadableStream<Uint8Array>({
      start(c) {
        ctrl = c
      },
    }),
    { status: 200 },
  )
  return { response, send: (text: string) => ctrl?.enqueue(enc.encode(text)) }
}

let visibility: DocumentVisibilityState = "visible"

function setVisibility(v: DocumentVisibilityState) {
  visibility = v
  document.dispatchEvent(new Event("visibilitychange"))
}

describe("useChangeStream", () => {
  beforeEach(() => {
    liveConfig.enabled = true
    visibility = "visible"
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => visibility })
  })

  afterEach(() => {
    liveConfig.enabled = false
    Reflect.deleteProperty(document, "visibilityState")
    setLive(false)
  })

  it("invalidates per topic, is not live while hidden, and refreshes everything after a reconnect", async () => {
    const streams = [controlled(), controlled()]
    let n = 0
    const { calls } = mockApi({ "GET /api/changes": () => streams[n++]?.response ?? json({ error: "not ready" }, 503) })
    const { queryClient, wrapper } = withQuery()
    seed(queryClient)
    const { result } = renderHook(
      () => {
        useChangeStream()
        return useLive()
      },
      { wrapper },
    )
    await waitFor(() => expect(calls.filter((c) => c.path === "/api/changes")).toHaveLength(1))
    streams[0]?.send("event: hello\ndata: {}\n\n")
    await waitFor(() => expect(result.current).toBe(true))
    expect(invalidated(queryClient)).toEqual([])
    streams[0]?.send('event: change\ndata: {"topics":["token"]}\n\n')
    await waitFor(() => expect(invalidated(queryClient)).toEqual(["token"]))

    act(() => setVisibility("hidden"))
    expect(result.current).toBe(false)
    seed(queryClient)
    act(() => setVisibility("visible"))
    await waitFor(() => expect(calls.filter((c) => c.path === "/api/changes")).toHaveLength(2))
    streams[1]?.send("event: hello\ndata: {}\n\n")
    await waitFor(() => expect(result.current).toBe(true))
    expect(invalidated(queryClient)).toEqual(ALL)
  })

  it("opens nothing while the stream is turned off", () => {
    liveConfig.enabled = false
    const { calls } = mockApi({})
    const { wrapper } = withQuery()
    renderHook(() => useChangeStream(), { wrapper })
    expect(calls).toHaveLength(0)
  })
})
```

In `web/src/components/shell.test.tsx`, add `import { liveConfig } from "@/api/live"` to the imports and append inside `describe("shell", …)`:

```tsx
  it("opens the change stream once the app is mounted", async () => {
    liveConfig.enabled = true
    try {
      const { calls } = mockApi(
        authedRoutes({
          "GET /api/changes": () =>
            new Response(
              new ReadableStream<Uint8Array>({
                start(c) {
                  c.enqueue(new TextEncoder().encode("event: hello\ndata: {}\n\n"))
                },
              }),
            ),
        }),
      )
      renderApp("/")
      await waitFor(() => expect(calls.some((c) => c.path === "/api/changes")).toBe(true))
    } finally {
      liveConfig.enabled = false
    }
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (from `web/`): `timeout 300 npx vitest run src/api/use-change-stream.test.tsx src/components/shell.test.tsx`
Expected: FAIL with `Failed to resolve import "./use-change-stream"`; the new shell test fails waiting for `/api/changes`.

- [ ] **Step 3: Write the implementation**

Create `web/src/api/use-change-stream.ts`:

```ts
import { useQueryClient, type QueryClient, type QueryKey } from "@tanstack/react-query"
import { useEffect } from "react"
import { keys } from "./hooks"
import { liveConfig, runChanges, setLive } from "./live"

// The queries each change topic makes stale. A one-element key is a prefix:
// it matches every window, zone, repository, filter and epoch.
export const TOPIC_QUERIES: Record<string, readonly QueryKey[]> = {
  status: [keys.status, ["activity"]],
  gauges: [keys.status, keys.token],
  events: [["events"]],
  config: [keys.config, ["activity"]],
  token: [keys.token],
  storage: [keys.storage],
  history: [["history"], ["activity"]],
  metrics: [keys.metrics, ["activity"]],
}

export function invalidateTopics(qc: QueryClient, topics: readonly string[]): void {
  const seen = new Set<string>()
  for (const topic of topics) {
    for (const key of TOPIC_QUERIES[topic] ?? []) {
      const id = JSON.stringify(key)
      if (seen.has(id)) continue
      seen.add(id)
      void qc.invalidateQueries({ queryKey: key })
    }
  }
}

export function invalidateAll(qc: QueryClient): void {
  invalidateTopics(qc, Object.keys(TOPIC_QUERIES))
}

// useChangeStream keeps the daemon's change stream open while the tab is
// visible and refetches what each notice names. Hiding the tab closes it, so
// background tabs neither refetch nor hold one of the browser's six
// connections to the host.
export function useChangeStream(): void {
  const queryClient = useQueryClient()
  useEffect(() => {
    if (!liveConfig.enabled) return
    let current: AbortController | undefined
    // Set by the first hello; a later hello follows a gap that may have
    // missed changes.
    let connected = false
    const start = () => {
      const ctrl = new AbortController()
      current = ctrl
      // An older loop's late callbacks must not touch a newer connection's state.
      const mine = () => current === ctrl
      void runChanges(
        {
          onHello: () => {
            if (!mine()) return
            if (connected) invalidateAll(queryClient)
            connected = true
            setLive(true)
          },
          onChange: (topics) => {
            if (mine()) invalidateTopics(queryClient, topics)
          },
          onEnd: () => {
            if (mine()) setLive(false)
          },
        },
        ctrl.signal,
      )
    }
    const stop = () => {
      current?.abort()
      current = undefined
      setLive(false)
    }
    const onVisibility = () => {
      if (document.visibilityState === "hidden") stop()
      else if (!current) start()
    }
    document.addEventListener("visibilitychange", onVisibility)
    if (document.visibilityState !== "hidden") start()
    return () => {
      document.removeEventListener("visibilitychange", onVisibility)
      stop()
    }
  }, [queryClient])
}
```

In `web/src/components/shell.tsx`, add `import { useChangeStream } from "@/api/use-change-stream"` after the `@/api/hooks` import, and add `useChangeStream()` on the line after `useEvents(epoch)`.

In `web/src/test/setup.ts`, add after the existing imports:

```ts
import { liveConfig } from "@/api/live"

// The change stream would add a fetch to every rendered app, and many tests
// count fetch calls; the stream's own tests turn it back on.
liveConfig.enabled = false
```

- [ ] **Step 4: Run the tests to verify they pass**

Run (from `web/`): `timeout 600 npm test && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: the whole web suite passes (the fetch-counting tests included); typecheck and lint print no errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/api/use-change-stream.ts web/src/api/use-change-stream.test.tsx web/src/components/shell.tsx web/src/components/shell.test.tsx web/src/test/setup.ts
git commit -m "feat(web): refetch on daemon change notices"
```

### Task 11: Polling gives way to the stream

**Files:**
- Modify: `web/src/api/hooks.ts` (`useStatus`, `useConfig`, `useMetrics`, `useActivity`, `useHistory`, `useEvents`, `useToken`, `useStorage`; new `POLL_SAFETY`, `livePoll`)
- Test: `web/src/api/hooks-live.test.tsx`

**Interfaces:**
- Consumes: `useLive`, `setLive` (Task 9).
- Produces: `POLL_SAFETY`, `livePoll` (Contracts, Task 11).

**Items:** 3
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

- [ ] **Step 1: Write the failing tests**

Create `web/src/api/hooks-live.test.tsx`:

```tsx
import type { Query, QueryClient, QueryKey } from "@tanstack/react-query"
import { act, renderHook, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import {
  browserZone,
  keys,
  livePoll,
  POLL_ACTIONS,
  POLL_FAST,
  POLL_SAFETY,
  POLL_SLOW,
  useActions,
  useActivity,
  useConfig,
  useContainers,
  useEvents,
  useHistory,
  useLogTail,
  useMetrics,
  useStatus,
  useSteps,
  useStorage,
  useToken,
} from "./hooks"
import { setLive } from "./live"

type Interval = number | false | undefined

// interval resolves the refetch interval the query's mounted observer asks for now.
function interval(qc: QueryClient, key: QueryKey): Interval {
  const q = qc.getQueryCache().find({ queryKey: key, exact: true })
  if (!q) return undefined
  const opt = q.observers[0]?.options.refetchInterval as Interval | ((q: Query) => Interval)
  return typeof opt === "function" ? opt(q) : opt
}

// The storage fixture has an install running; these cases need it idle.
const idleStorage = { ...fixtures.storage, measuring: false, operations: { current: null, queued: 0, recent: [] } }
const routes = () => authedRoutes({ "GET /api/token": fixtures.token, "GET /api/history": [], "GET /api/storage": idleStorage })

interface Case {
  name: string
  key: QueryKey
  want: number
  use: () => unknown
}

afterEach(() => setLive(false))

const SINCE = "2026-10-01T00:00:00Z"

describe("daemon-state hooks", () => {
  // want is the interval while the stream is not live.
  const cases: Case[] = [
    { name: "useStatus", key: keys.status, want: POLL_FAST, use: () => useStatus() },
    { name: "useEvents", key: keys.events("e1"), want: POLL_FAST, use: () => useEvents("e1") },
    { name: "useConfig", key: keys.config, want: POLL_SLOW, use: () => useConfig() },
    { name: "useMetrics", key: keys.metrics, want: POLL_SLOW, use: () => useMetrics() },
    { name: "useToken", key: keys.token, want: POLL_SLOW, use: () => useToken() },
    { name: "useActivity", key: keys.activity("1h", browserZone(), ""), want: POLL_SLOW, use: () => useActivity("1h") },
    { name: "useHistory", key: keys.history("", "", SINCE), want: POLL_SLOW, use: () => useHistory("", "", SINCE) },
    { name: "useStorage", key: keys.storage, want: POLL_SLOW, use: () => useStorage(undefined) },
  ]

  it.each(cases)("$name polls every 60 s while live", async ({ key, want, use }) => {
    mockApi(routes())
    const { queryClient, wrapper } = withQuery()
    renderHook(use, { wrapper })
    await waitFor(() => expect(interval(queryClient, key)).toBe(want))
    act(() => setLive(true))
    await waitFor(() => expect(interval(queryClient, key)).toBe(POLL_SAFETY))
    act(() => setLive(false))
    await waitFor(() => expect(interval(queryClient, key)).toBe(want))
  })

  it("gives up the busy storage poll while live", async () => {
    // The fixture's running install makes storage busy.
    mockApi(authedRoutes())
    const { queryClient, wrapper } = withQuery()
    renderHook(() => useStorage(undefined), { wrapper })
    await waitFor(() => expect(interval(queryClient, keys.storage)).toBe(POLL_FAST))
    act(() => setLive(true))
    await waitFor(() => expect(interval(queryClient, keys.storage)).toBe(POLL_SAFETY))
  })
})

describe("hooks the stream does not cover", () => {
  const cases: Case[] = [
    { name: "useActions", key: keys.actions, want: POLL_ACTIONS, use: () => useActions() },
    { name: "useSteps", key: keys.steps("aaaaaa"), want: POLL_SLOW, use: () => useSteps("aaaaaa", true) },
    { name: "useContainers", key: keys.containers("aaaaaa"), want: POLL_SLOW, use: () => useContainers("aaaaaa", true) },
    { name: "useLogTail", key: keys.log("aaaaaa"), want: POLL_FAST, use: () => useLogTail("aaaaaa", true) },
  ]

  it.each(cases)("$name keeps its interval while live", async ({ key, want, use }) => {
    mockApi(routes())
    const { queryClient, wrapper } = withQuery()
    act(() => setLive(true))
    renderHook(use, { wrapper })
    await waitFor(() => expect(interval(queryClient, key)).toBe(want))
  })
})

describe("livePoll", () => {
  it("swaps the interval for the safety poll while live", () => {
    expect(livePoll(true, POLL_FAST)).toBe(POLL_SAFETY)
    expect(livePoll(false, POLL_FAST)).toBe(POLL_FAST)
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (from `web/`): `timeout 300 npx vitest run src/api/hooks-live.test.tsx`
Expected: FAIL — `livePoll` and `POLL_SAFETY` are not exported (`livePoll is not a function`), and the daemon-state cases time out waiting for `60000`.

- [ ] **Step 3: Write the implementation**

In `web/src/api/hooks.ts`, add `import { useLive } from "./live"` after the `./client` import, and after `export const POLL_ACTIONS = 15_000` add:

```ts
// While the change stream is live it triggers the refetches; this slow poll
// only catches a change the stream missed.
export const POLL_SAFETY = 60_000

export function livePoll(live: boolean, interval: number): number {
  return live ? POLL_SAFETY : interval
}
```

Replace these hooks with the versions below (each keeps its other options unchanged):

```ts
export function useStatus() {
  const live = useLive()
  return useQuery({ queryKey: keys.status, queryFn: ({ signal }) => api.status(signal), refetchInterval: livePoll(live, POLL_FAST) })
}
```

```ts
export function useConfig() {
  const live = useLive()
  return useQuery({ queryKey: keys.config, queryFn: ({ signal }) => api.config(signal), refetchInterval: livePoll(live, POLL_SLOW) })
}

export function useMetrics() {
  const live = useLive()
  return useQuery({ queryKey: keys.metrics, queryFn: ({ signal }) => api.metrics(signal), refetchInterval: livePoll(live, POLL_SLOW) })
}
```

```ts
export function useActivity(window: ActivityWindow, repo = "") {
  const tz = browserZone()
  const live = useLive()
  return useQuery({
    queryKey: keys.activity(window, tz, repo),
    queryFn: ({ signal }) => api.activity(window, tz, repo, signal),
    refetchInterval: livePoll(live, POLL_SLOW),
    // Keeps the panel and the repository strips drawn while a new window loads;
    // ActivityPanel reads lanes or buckets from the data's own window.
    placeholderData: keepPreviousData,
  })
}
```

```ts
export function useHistory(repo: string, conclusion: string, since: string | undefined) {
  const live = useLive()
  return useQuery({
    queryKey: keys.history(repo, conclusion, since ?? ""),
    queryFn: ({ signal }) => api.history(repo, conclusion, since ?? "", HISTORY_LIMIT, signal),
    enabled: since !== undefined,
    refetchInterval: livePoll(live, POLL_SLOW),
    placeholderData: keepPreviousData,
  })
}
```

In `useEvents`, add `const live = useLive()` after `const key = keys.events(epoch ?? "")` and change `refetchInterval: POLL_FAST,` to `refetchInterval: livePoll(live, POLL_FAST),`.

```ts
export function useToken() {
  const live = useLive()
  return useQuery({ queryKey: keys.token, queryFn: ({ signal }) => api.token(signal), refetchInterval: livePoll(live, POLL_SLOW) })
}
```

```ts
export function useStorage(status: Status | undefined) {
  const live = useLive()
  return useQuery({
    queryKey: keys.storage,
    queryFn: ({ signal }) => api.storage(signal),
    refetchInterval: (q) => livePoll(live, storageBusy(q.state.data, status) ? POLL_FAST : POLL_SLOW),
  })
}
```

Leave `useActions`, `useRunDetail`, `useRunAnnotations`, `useJobLog`, `useLogTail`, `useSteps`, `useContainers`, `useLabelCheck`, `useRegistrations` and `useSetupState` unchanged.

- [ ] **Step 4: Run the tests to verify they pass**

Run (from `web/`): `timeout 600 npm test && timeout 300 npm run typecheck && timeout 300 npm run lint`
Expected: the whole web suite passes; typecheck and lint print no errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/api/hooks.ts web/src/api/hooks-live.test.tsx
git commit -m "feat(web): slow polling while the stream is live"
```
