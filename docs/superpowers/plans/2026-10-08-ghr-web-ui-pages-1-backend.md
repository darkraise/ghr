# ghr web UI pages, plan 1: backend

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the five optional API additions the redesigned pages need: a finish-time bound on `/history`, a repository filter and 7-day counts on `/activity`, step start and finish times, and the Docker data root path in `/status`.

**Architecture:** `history.Store.Query` gains a `since` bound that `GET /history?since=` and `api.Client.History` pass through. `activity.Build` gains an `Input.Repo` that narrows history and instances to one repository and drops host-wide figures, and every `ActivityRepo` gains a `Week` count; `Backend.Activity` and `GET /activity?repo=` carry the repo and key the cache by it. `github.Step` and `model.Step` gain step times, and `system.DiskUsage` gains the data root path that `/status` reports as `disk_root`. Each Go type change regenerates the frontend fixtures and mirrors the field in `web/src/api/types.ts`.

**Tech Stack:** Go 1.26 standard library; TypeScript only for the type mirror and its fixture test. The TypeScript client parameters (`api.activity`'s repo, `api.history`'s since) and the query keys that include them belong to plan 2, the first frontend plan.

**Spec:** docs/superpowers/specs/2026-10-08-ghr-web-ui-pages-design.md (§2 and the Go parts of §8)

**Execution:** inline — `claude --model sonnet --effort medium` — no task is heavy (every total is 4 or less, no risk 3), so nothing is delegated; the highest self-implemented total is 4 (Sonnet medium).

**Plan review:** 2026-10-08 — dr-superpowers:judge-opus — executability 17 / coherence 18 / coverage 17 / assumptions 15 (round 1)

## Global Constraints

- Work on the branch `feat/web-ui-pages` (create it from master, in a worktree, before Task 1 if it does not exist). Pushing master cuts a release through CI, so nothing is pushed or merged without the owner.
- Go changes plus, under `web/src`, only: the generated fixture JSON in `web/src/api/fixtures/`, `web/src/api/types.ts`, and `web/src/api/types.test.ts`.
- No new module or npm dependencies.
- Every addition is optional: an absent query parameter or field behaves as before, so the CLI and older callers keep working.
- After any change to a type that a fixture marshals, regenerate the fixtures in the same commit: `GHR_UPDATE_FIXTURES=1 go test ./web/`.
- Before each commit run `gofmt -w` on the Go files you changed, then `gofmt -l .` and `go vet ./...`; both must print nothing. Then `go test -race ./...` must pass (CI runs it with `-race`, and Task 6 touches a concurrent cache).
- After a change under `web/src`, run in `web/`: `npm run typecheck` and `npx vitest run src/api/types.test.ts`; both must pass.
- Comments only where the why is non-obvious; match the surrounding code's style. English only.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no period.

## Contracts

**`internal/history`**

```go
// since zero means no bound; otherwise entries finished at or after since.
func (s *Store) Query(repo, conclusion string, since time.Time, limit int) ([]model.HistoryEntry, error)
```

**`internal/api`**

```go
// Backend (interface) changes:
History(repo, conclusion string, since time.Time, limit int) ([]model.HistoryEntry, error)
Activity(ctx context.Context, window, repo string, loc *time.Location) (model.Activity, error)

// Client:
func (c *Client) History(ctx context.Context, repo, conclusion string, since time.Time, limit int) ([]model.HistoryEntry, error)
```

- `GET /history?since=<RFC 3339>`: absent or empty means no bound; any other value that `time.Parse(time.RFC3339, …)` rejects answers 400 `since must be an RFC 3339 time`.
- `GET /activity?repo=<name>`: optional, any name accepted, matched case-insensitively.
- `Client.History` omits `since` for the zero time and sends `since.Format(time.RFC3339Nano)` otherwise.

**`internal/activity`**

```go
type Input struct {
	// ...existing fields...
	Repo string // when set, only this repository's runs, and no host figures
}
```

With `Repo` set: history and instances are kept only when `strings.EqualFold(x.Repo, Repo)`; `Repos` keeps only the configured names that match (configured spelling); `Capacity`, `Minutes` and `Hours` are cleared, so `capacity`, every `busy_pct`, `waiting_max` and `cpu_avg` are `null`, `waiting` and `cpu` are `[]`, and lanes are not padded.

**`internal/model`**

```go
type ActivityRepo struct {
	Repo  string         `json:"repo"`
	Hours []ActivityHour `json:"hours"`
	Week  ActivityWeek   `json:"week"`
}

type ActivityWeek struct {
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
}

// Step gains:
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`

// Status gains:
	DiskRoot string `json:"disk_root,omitempty"`
```

`Week` counts history entries of that repository (case-insensitive) with `now-7d <= FinishedAt <= now`, through the existing `tally`: success is succeeded; cancelled and skipped are cancelled; anything else is failed.

**`internal/github`**

```go
type Step struct {
	Number      int        `json:"number"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
}
```

**`internal/system`**

```go
type DiskUsage struct {
	Pct   int
	Used  int64
	Total int64
	Root  string // the data root path; empty when only the percentage was measured
}
```

**`web/src/api/types.ts`**

```ts
export interface ActivityWeek {
  succeeded: number
  failed: number
  cancelled: number
}
// ActivityRepo gains:   week: ActivityWeek
// Step gains:           started_at?: string; completed_at?: string
// Status gains:         disk_root?: string
```

## Assumptions (evidence)

- `tally` counts skipped as cancelled: `finishedState("skipped")` is `"skipped"` and `tally`'s default branch increments cancelled (internal/activity/buckets.go `tally`, activity.go `finishedState`), checked 2026-10-08. The spec's §2.2 originally said skipped is counted in none of the three; it was corrected to match the code on 2026-10-08 while writing this plan.
- `Store.Query` is called from internal/runner/manager.go:207, internal/daemon/backend.go:136 and :213, internal/history/history_test.go (8 calls), and internal/runner/{cleanup_test,fakes_test,lifecycle_test}.go (grep on 2026-10-08, 17 calls in all). Every one of those files already imports `time`.
- `newBackend` (internal/daemon/backend_test.go:212-230) runs in queue mode with `global_max` 2 and the repositories `darkcloud` and `darkmem`, as `TestActivityBuildsFromHistoryAndInstances` asserts (capacity 2, repos darkcloud then darkmem); Task 6 relies on this.
- `fixtureTime` in web/fixtures_test.go:16 is 2026-10-03 14:05 UTC, so `at(-4 * time.Minute)` marshals as `2026-10-03T14:01:00Z` (Task 7).
- The `activity-buckets` fixture (web/fixtures_test.go, `"activity-buckets"`) holds one darkmem success (h1) and one darkmem failure (h2), both inside 7 days of `fixtureTime`, so its week is `{1, 1, 0}` (Task 5).
- `TestDockerParsing`'s fake answers `docker info` with `/var/lib/docker
` (internal/system/system_test.go:102) (Task 8).
- The API fake backend records calls in `activityCalls` as `window|loc` (internal/api/api_test.go:69-72), and `TestActivityRoute` asserts that exact list; Task 6 keeps the format for calls without a repo.
- The daemon's existing cache tests use keys `1h|UTC` and `30d|UTC` (internal/daemon/backend_test.go `TestActivitySlowWindowDoesNotBlockAnother`, `TestActivityCacheDropsExpiredEntries`); Task 6 keeps that key for an empty repo and appends `|<lower-cased repo>` otherwise.
- The fake manager's `RunnerRepoAndRun("aaaaaa")` returns run ID 55 and runner name `ghr-darkcloud-aaaaaa` (internal/daemon/backend_test.go:72-77), and `fakeGH.ListJobs` returns `f.jobs[runID]` when `jobs` is set (:145-147).
- `cmd/ghr` tests record each request's `RequestURI` in `fakeDaemon` (cmd/ghr/cli_test.go:23-30).
- No task here carries an **Items:** line: every register row this spec covers is discharged where the UI shows it (plans 2 and 3). Row 11 (the Docker root path) is assigned to plan 3, which displays the `disk_root` field Task 8 adds.
- A JSON fixture imported into a TypeScript type gets no excess-property check (web/src/api/types.test.ts comment), so new Go fields never break `tsc`; each task adds an explicit assertion instead.

## Task index

1. Bound history queries by finish time
2. Serve history since a time
3. Send since from the Go client
4. Narrow activity to one repository
5. Count each repository's last 7 days
6. Ask the daemon for one repository's activity
7. Report step start and finish times
8. Report the Docker data root path

---

### Task 1: Bound history queries by finish time

**Files:**
- Modify: `internal/history/history.go` (`Query`)
- Modify: `internal/history/history_test.go`
- Modify: `internal/runner/manager.go:207`, `internal/daemon/backend.go:136` and `:213`, `internal/runner/cleanup_test.go`, `internal/runner/fakes_test.go`, `internal/runner/lifecycle_test.go` (callers)

**Interfaces:**
- Consumes: nothing.
- Produces: `history.Store.Query(repo, conclusion string, since time.Time, limit int)` (Contracts, `internal/history`).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

Append to `internal/history/history_test.go` (add `"strings"` to its imports):

```go
func TestQuerySince(t *testing.T) {
	s := &Store{Path: filepath.Join(t.TempDir(), "history.jsonl")}
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, e := range []model.HistoryEntry{
		{ID: "before", Repo: "a", RunID: 1, Conclusion: "success", FinishedAt: t0.Add(-time.Second)},
		{ID: "at", Repo: "a", RunID: 2, Conclusion: "failure", FinishedAt: t0},
		{ID: "after", Repo: "b", RunID: 3, Conclusion: "success", FinishedAt: t0.Add(time.Second)},
		{ID: "later", Repo: "a", RunID: 4, Conclusion: "success", FinishedAt: t0.Add(time.Hour)},
	} {
		if err := s.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		repo, conclusion string
		since            time.Time
		limit            int
		want             string
	}{
		{"", "", time.Time{}, 0, "later,after,at,before"},
		{"", "", t0, 0, "later,after,at"},
		{"a", "", t0, 0, "later,at"},
		{"", "success", t0, 0, "later,after"},
		{"", "", t0, 2, "later,after"},
		{"", "", t0.Add(2 * time.Hour), 0, ""},
	} {
		got, err := s.Query(c.repo, c.conclusion, c.since, c.limit)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, e := range got {
			ids = append(ids, e.ID)
		}
		if strings.Join(ids, ",") != c.want {
			t.Errorf("Query(%q, %q, %v, %d) = %v, want %s", c.repo, c.conclusion, c.since, c.limit, ids, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/history/ -run TestQuerySince`
Expected: FAIL to compile: `too many arguments in call to s.Query`.

- [ ] **Step 3: Write the implementation**

In `internal/history/history.go`, replace `Query` and its comment with:

```go
// Query returns entries by FinishedAt, newest first, filtered by repo and
// conclusion when non-empty, and to entries finished at or after since when
// since is not zero. limit <= 0 means no limit. Lines are appended in cleanup
// order, which is not finish order.
func (s *Store) Query(repo, conclusion string, since time.Time, limit int) ([]model.HistoryEntry, error) {
	s.mu.Lock()
	all, err := s.readAll()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(all, func(a, b int) bool { return all[a].FinishedAt.After(all[b].FinishedAt) })
	var out []model.HistoryEntry
	for _, e := range all {
		if (repo == "" || e.Repo == repo) && (conclusion == "" || e.Conclusion == conclusion) &&
			(since.IsZero() || !e.FinishedAt.Before(since)) {
			out = append(out, e)
			if limit > 0 && len(out) == limit {
				break
			}
		}
	}
	return out, nil
}
```

Then pass the zero time at every other caller. Run from the repository root (Git Bash):

```bash
sed -i -E 's/\.Query\(("[^"]*"), ("[^"]*"), /.Query(\1, \2, time.Time{}, /g' \
  internal/history/history_test.go internal/runner/manager.go \
  internal/runner/cleanup_test.go internal/runner/fakes_test.go internal/runner/lifecycle_test.go
```

The pattern matches only calls whose first two arguments are string literals, so it leaves `TestQuerySince`'s `s.Query(c.repo, …)` alone. In `internal/daemon/backend.go`, edit the two calls by hand:

```go
	hist, err := b.Hist.Query("", "", time.Time{}, 0)
```

```go
func (b *Backend) History(repo, conclusion string, limit int) ([]model.HistoryEntry, error) {
	return b.Hist.Query(repo, conclusion, time.Time{}, limit)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./internal/history/ ./internal/runner/ ./internal/daemon/`
Expected: PASS, and `grep -rn '\.Query("' --include=*.go .` shows every call with four arguments.

- [ ] **Step 5: Commit**

```bash
gofmt -l . ; go test ./...
git add internal/history internal/runner internal/daemon/backend.go
git commit -m "feat(history): bound queries by finish time"
```

---

### Task 2: Serve history since a time

**Files:**
- Modify: `internal/api/server.go` (`Backend.History`, `GET /history`)
- Modify: `internal/daemon/backend.go` (`Backend.History`)
- Test: `internal/api/api_test.go`

**Interfaces:**
- Consumes: `history.Store.Query` with `since` (Contracts, Task 1).
- Produces: `api.Backend.History(repo, conclusion string, since time.Time, limit int)` and the `since` parameter of `GET /history` (Contracts, `internal/api`).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

In `internal/api/api_test.go`, add a field to `fakeBackend` (beside `activityCalls`):

```go
	historySince  []time.Time
```

Replace the fake's `History` method:

```go
func (f *fakeBackend) History(repo, conclusion string, since time.Time, limit int) ([]model.HistoryEntry, error) {
	f.historySince = append(f.historySince, since)
	return []model.HistoryEntry{{ID: "a", Repo: repo, Conclusion: conclusion, RunNumber: "7"}}, nil
}
```

Append the test:

```go
func TestHistorySinceParameter(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	var got []model.HistoryEntry
	for _, q := range []string{"since=2026-10-01T18:00:00%2B07:00", "since=", ""} {
		if err := c.call(ctx, http.MethodGet, "/history?"+q, nil, &got); err != nil {
			t.Fatalf("%q: %v", q, err)
		}
	}
	want := time.Date(2026, 10, 1, 11, 0, 0, 0, time.UTC)
	if len(b.historySince) != 3 || !b.historySince[0].Equal(want) || !b.historySince[1].IsZero() || !b.historySince[2].IsZero() {
		t.Fatalf("backend saw %v", b.historySince)
	}
	err := c.call(ctx, http.MethodGet, "/history?since=yesterday", nil, &got)
	var ae *Error
	if !errors.As(err, &ae) || ae.Status != http.StatusBadRequest || len(b.historySince) != 3 {
		t.Fatalf("unparsable since: %v, backend saw %d calls", err, len(b.historySince))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api/ -run TestHistorySinceParameter`
Expected: FAIL to compile: `*fakeBackend does not implement Backend (wrong type for method History)`.

- [ ] **Step 3: Write the implementation**

In `internal/api/server.go`, change the interface line:

```go
	History(repo, conclusion string, since time.Time, limit int) ([]model.HistoryEntry, error)
```

and replace the `GET /history` handler:

```go
	mux.HandleFunc("GET /history", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		var since time.Time
		if s := q.Get("since"); s != "" {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				respond(w, nil, BadRequest("since must be an RFC 3339 time"))
				return
			}
			since = t
		}
		h, err := b.History(q.Get("repo"), q.Get("conclusion"), since, limit)
		if h == nil {
			h = []model.HistoryEntry{}
		}
		respond(w, h, err)
	})
```

In `internal/daemon/backend.go`:

```go
func (b *Backend) History(repo, conclusion string, since time.Time, limit int) ([]model.HistoryEntry, error) {
	return b.Hist.Query(repo, conclusion, since, limit)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/api/ ./internal/daemon/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . ; go vet ./... ; go test ./...
git add internal/api/server.go internal/api/api_test.go internal/daemon/backend.go
git commit -m "feat(api): serve history since a time"
```

---

### Task 3: Send since from the Go client

**Files:**
- Modify: `internal/api/client.go` (`Client.History`)
- Modify: `cmd/ghr/cli.go` (`historyCmd`)
- Test: `internal/api/api_test.go`, `cmd/ghr/cli_test.go`

**Interfaces:**
- Consumes: the `since` parameter of `GET /history` (Contracts, Task 2).
- Produces: `api.Client.History(ctx, repo, conclusion string, since time.Time, limit int)` (Contracts, `internal/api`).

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing tests**

In `internal/api/api_test.go`, in `TestStatusEventsHistory`, replace the history call and its check:

```go
	h, err := c.History(ctx, "darkmem", "failure", time.Time{}, 5)
	if err != nil || h[0].Repo != "darkmem" || h[0].Conclusion != "failure" {
		t.Fatalf("history %+v err %v", h, err)
	}
```

and append a test (add `"net/url"` to the imports if the file lacks it; `fmt`, `net/http/httptest` and `time` are already imported):

```go
func TestClientHistorySendsSince(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	since := time.Date(2026, 10, 1, 18, 0, 0, 500, time.FixedZone("ICT", 7*60*60))
	if _, err := c.History(ctx, "", "", since, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := c.History(ctx, "", "", time.Time{}, 0); err != nil {
		t.Fatal(err)
	}
	if len(b.historySince) != 2 || !b.historySince[0].Equal(since) || !b.historySince[1].IsZero() {
		t.Fatalf("backend saw %v", b.historySince)
	}

	// The zero time round-trips as zero, so only the raw query shows that it
	// was left out rather than sent.
	var raw []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = append(raw, r.URL.Query())
		fmt.Fprint(w, "[]")
	}))
	defer srv.Close()
	rc := &Client{Base: srv.URL, HTTP: srv.Client()}
	if _, err := rc.History(ctx, "", "", time.Time{}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := rc.History(ctx, "", "", since, 0); err != nil {
		t.Fatal(err)
	}
	if len(raw) != 2 || raw[0].Has("since") || raw[1].Get("since") != "2026-10-01T18:00:00.0000005+07:00" {
		t.Fatalf("raw queries %v", raw)
	}
}
```

In `cmd/ghr/cli_test.go`, replace `TestLogsAndHistory` with:

```go
func TestLogsAndHistory(t *testing.T) {
	reqs := fakeDaemon(t)
	if code, out, _ := runCLI(t, "", "logs", "a3f9c1"); code != 0 || out != "hello log\nsecond chunk\n" {
		t.Fatalf("logs %d %q", code, out)
	}
	code, out, _ := runCLI(t, "", "history", "--repo", "darkmem")
	if code != 0 || !strings.Contains(out, "#88") || !strings.Contains(out, "failure") || !strings.Contains(out, "1m0s") {
		t.Fatalf("history %d:\n%s", code, out)
	}
	for _, r := range *reqs {
		if strings.HasPrefix(r.path, "/history") && strings.Contains(r.path, "since=") {
			t.Fatalf("ghr history sent a since bound: %s", r.path)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/api/ -run 'TestClientHistorySendsSince|TestStatusEventsHistory'`
Expected: FAIL to compile: `too many arguments in call to c.History`.

- [ ] **Step 3: Write the implementation**

In `internal/api/client.go`, replace `History`:

```go
func (c *Client) History(ctx context.Context, repo, conclusion string, since time.Time, limit int) ([]model.HistoryEntry, error) {
	q := url.Values{}
	q.Set("repo", repo)
	q.Set("conclusion", conclusion)
	if !since.IsZero() {
		q.Set("since", since.Format(time.RFC3339Nano))
	}
	q.Set("limit", fmt.Sprint(limit))
	var h []model.HistoryEntry
	err := c.call(ctx, http.MethodGet, "/history?"+q.Encode(), nil, &h)
	return h, err
}
```

In `cmd/ghr/cli.go`, in `historyCmd`:

```go
	h, err := c.History(ctx, *repo, *conclusion, time.Time{}, *limit)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/api/ ./cmd/ghr/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . ; go vet ./... ; go test ./...
git add internal/api/client.go internal/api/api_test.go cmd/ghr/cli.go cmd/ghr/cli_test.go
git commit -m "feat(api): send since from the Go client"
```

---

### Task 4: Narrow activity to one repository

**Files:**
- Modify: `internal/activity/activity.go` (`Input`, `Build`, new `forRepo`)
- Test: `internal/activity/activity_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `activity.Input.Repo` and its narrowing rules (Contracts, `internal/activity`).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

Append to `internal/activity/activity_test.go`:

```go
func TestRepoFilter(t *testing.T) {
	hist := []model.HistoryEntry{
		job("mine", at(13, 10), at(13, 20), "success"),
		{ID: "theirs", Repo: "ghr", Conclusion: "success", StartedAt: at(13, 10), FinishedAt: at(13, 20)},
	}
	insts := []model.InstanceStatus{
		{ID: "r1", Repo: "DarkMem", State: "busy", Since: at(14, 0)},
		{ID: "r2", Repo: "ghr", State: "busy", Since: at(14, 0)},
	}
	minutes := []model.MetricSample{{At: at(14, 0), Queued: 2, CPU: ptr(10.0)}}
	hours := []model.MetricRollup{{At: at(13, 0), Samples: 60, QueuedMax: 3, CPUAvg: ptr(20.0)}}

	lanes := Build(Input{Window: "1h", Now: now, Repo: "darkmem", Capacity: ptr(4), Repos: []string{"DarkMem", "ghr"},
		History: hist, Instances: insts, Minutes: minutes, Hours: hours})
	ids := laneIDs(lanes)
	if lanes.Capacity != nil || len(ids) != 1 || strings.Join(ids[0], ",") != "mine,r1" {
		t.Fatalf("lanes %v capacity %v", ids, lanes.Capacity)
	}
	if len(lanes.Waiting) != 0 || len(lanes.CPU) != 0 {
		t.Fatalf("host figures kept: waiting %v cpu %v", lanes.Waiting, lanes.CPU)
	}
	if len(lanes.Repos) != 1 || lanes.Repos[0].Repo != "DarkMem" {
		t.Fatalf("repos %+v", lanes.Repos)
	}

	b := Build(Input{Window: "24h", Now: now, Repo: "darkmem", Capacity: ptr(4), Repos: []string{"DarkMem"},
		History: hist, Hours: hours})
	for _, bk := range b.Buckets {
		if bk.BusyPct != nil || bk.WaitingMax != nil || bk.CPUAvg != nil {
			t.Fatalf("bucket %v kept host figures: %+v", bk.Start, bk)
		}
	}
	if bk := bucketAt(t, b, at(13, 0)); bk.Succeeded != 1 || bk.BusyMinutes != 10 {
		t.Fatalf("13:00 bucket %+v", bk)
	}

	gone := Build(Input{Window: "24h", Now: now, Repo: "removed-repo", Repos: []string{"DarkMem"},
		History: []model.HistoryEntry{{ID: "old", Repo: "removed-repo", Conclusion: "failure", StartedAt: at(13, 0), FinishedAt: at(13, 5)}}})
	if len(gone.Repos) != 0 || bucketAt(t, gone, at(13, 0)).Failed != 1 {
		t.Fatalf("removed repo: repos %+v", gone.Repos)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/activity/ -run TestRepoFilter`
Expected: FAIL to compile: `unknown field Repo in struct literal of type Input`.

- [ ] **Step 3: Write the implementation**

In `internal/activity/activity.go`, add `"strings"` to the imports, add the field at the end of `Input`:

```go
	Hours     []model.MetricRollup
	// Repo, when set, narrows the view to that repository; see forRepo.
	Repo string
}
```

Make `forRepo` the first statement of `Build`:

```go
func Build(in Input) model.Activity {
	if in.Repo != "" {
		in = forRepo(in)
	}
	loc := in.Loc
```

and add below `Build`:

```go
// forRepo keeps one repository's history and live instances, matching names
// as repoHours does, and its configured name if it has one. Capacity and the
// host metrics belong to the whole host, so they are dropped: buckets then
// carry no busy percentage, waiting or CPU, and lanes are not padded.
func forRepo(in Input) Input {
	var hist []model.HistoryEntry
	for _, h := range in.History {
		if strings.EqualFold(h.Repo, in.Repo) {
			hist = append(hist, h)
		}
	}
	var insts []model.InstanceStatus
	for _, i := range in.Instances {
		if strings.EqualFold(i.Repo, in.Repo) {
			insts = append(insts, i)
		}
	}
	var repos []string
	for _, r := range in.Repos {
		if strings.EqualFold(r, in.Repo) {
			repos = append(repos, r)
		}
	}
	in.History, in.Instances, in.Repos = hist, insts, repos
	in.Capacity, in.Minutes, in.Hours = nil, nil, nil
	return in
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/activity/`
Expected: PASS, including `TestEmptySlicesMarshalAsArrays`.

- [ ] **Step 5: Commit**

```bash
gofmt -l . ; go vet ./... ; go test ./...
git add internal/activity/activity.go internal/activity/activity_test.go
git commit -m "feat(activity): narrow the view to one repository"
```

---

### Task 5: Count each repository's last 7 days

**Files:**
- Modify: `internal/model/model.go:328-338` (`ActivityRepo`, new `ActivityWeek`)
- Modify: `internal/activity/buckets.go` (`repoHours`)
- Test: `internal/activity/buckets_test.go`
- Generated: `web/src/api/fixtures/activity-lanes.json`, `web/src/api/fixtures/activity-buckets.json`
- Modify: `web/src/api/types.ts`, `web/src/api/types.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `model.ActivityRepo.Week`, `model.ActivityWeek`, and the TypeScript `ActivityWeek` and `ActivityRepo.week` (Contracts, `internal/model` and `web/src/api/types.ts`).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

Append to `internal/activity/buckets_test.go`:

```go
func TestRepoWeek(t *testing.T) {
	week := 7 * 24 * time.Hour
	entry := func(id, repo string, end time.Time, conclusion string) model.HistoryEntry {
		return model.HistoryEntry{ID: id, Repo: repo, Conclusion: conclusion, StartedAt: end.Add(-time.Minute), FinishedAt: end}
	}
	a := Build(Input{Window: "24h", Now: now, Repos: []string{"darkmem", "darkcloud"}, History: []model.HistoryEntry{
		entry("edge", "darkmem", now.Add(-week), "success"),
		entry("old", "darkmem", now.Add(-week-time.Second), "success"),
		entry("f", "darkmem", now.Add(-time.Hour), "failure"),
		entry("t", "darkmem", now.Add(-time.Hour), "timed_out"),
		entry("c", "darkmem", now.Add(-time.Hour), "cancelled"),
		entry("s", "darkmem", now.Add(-time.Hour), "skipped"),
		entry("cased", "DarkMem", now.Add(-time.Minute), "success"),
		entry("other", "ghr", now.Add(-time.Minute), "success"),
	}})
	if got := a.Repos[0].Week; got != (model.ActivityWeek{Succeeded: 2, Failed: 2, Cancelled: 2}) {
		t.Fatalf("darkmem week %+v", got)
	}
	if got := a.Repos[1].Week; got != (model.ActivityWeek{}) {
		t.Fatalf("darkcloud week %+v", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/activity/ -run TestRepoWeek`
Expected: FAIL to compile: `a.Repos[0].Week undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/model/model.go`, replace `ActivityRepo` and add `ActivityWeek` after it:

```go
type ActivityRepo struct {
	Repo  string         `json:"repo"`
	Hours []ActivityHour `json:"hours"`
	// Week counts the runs that finished in the 7 days ending now, whatever
	// the window.
	Week ActivityWeek `json:"week"`
}

type ActivityWeek struct {
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
}
```

In `internal/activity/buckets.go`, replace `repoHours`:

```go
// repoHours counts each configured repository's runs by finish time over
// the last 24 local hours, oldest first, the last one in progress, and over
// the 7 days ending now.
func repoHours(in Input, loc *time.Location) []model.ActivityRepo {
	ss := starts(in.Now, "hour", loc, 23)
	weekFrom := in.Now.Add(-7 * 24 * time.Hour)
	out := []model.ActivityRepo{}
	for _, name := range in.Repos {
		r := model.ActivityRepo{Repo: name, Hours: make([]model.ActivityHour, len(ss))}
		for i, s := range ss {
			e := next(s, "hour", loc)
			hr := model.ActivityHour{Start: s.In(loc)}
			for _, h := range in.History {
				if strings.EqualFold(h.Repo, name) && !h.FinishedAt.Before(s) && h.FinishedAt.Before(e) {
					tally(h.Conclusion, &hr.Succeeded, &hr.Failed, &hr.Cancelled)
				}
			}
			r.Hours[i] = hr
		}
		for _, h := range in.History {
			if strings.EqualFold(h.Repo, name) && !h.FinishedAt.Before(weekFrom) && !h.FinishedAt.After(in.Now) {
				tally(h.Conclusion, &r.Week.Succeeded, &r.Week.Failed, &r.Week.Cancelled)
			}
		}
		out = append(out, r)
	}
	return out
}
```

Regenerate the fixtures:

```bash
GHR_UPDATE_FIXTURES=1 go test ./web/
```

In `web/src/api/types.ts`, replace `ActivityRepo` and add `ActivityWeek` after it:

```ts
export interface ActivityRepo {
  repo: string
  hours: ActivityHour[]
  week: ActivityWeek
}

export interface ActivityWeek {
  succeeded: number
  failed: number
  cancelled: number
}
```

In `web/src/api/types.test.ts`, in the test "activity parses as lanes and as buckets", add after the `hours` assertion:

```ts
    expect(buckets.repos[0]?.week).toEqual({ succeeded: 1, failed: 1, cancelled: 0 })
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/activity/ ./web/`, then in `web/`: `npm run typecheck && npx vitest run src/api/types.test.ts`
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
gofmt -l . ; go vet ./... ; go test ./...
git add internal/model/model.go internal/activity/buckets.go internal/activity/buckets_test.go web/src/api
git commit -m "feat(activity): count each repo's last 7 days"
```

---

### Task 6: Ask the daemon for one repository's activity

**Files:**
- Modify: `internal/daemon/backend.go` (`Backend.Activity`)
- Modify: `internal/api/server.go` (`Backend.Activity`, `GET /activity`)
- Test: `internal/daemon/backend_test.go`, `internal/api/api_test.go`

**Interfaces:**
- Consumes: `activity.Input.Repo` (Contracts, Task 4).
- Produces: `api.Backend.Activity(ctx, window, repo string, loc)` and the `repo` parameter of `GET /activity` (Contracts, `internal/api`).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `internal/daemon/backend_test.go`, give every existing `b.Activity` call an empty repo (Git Bash, repository root):

```bash
sed -i -E 's/b\.Activity\(([^,]+), ("[^"]*"), /b.Activity(\1, \2, "", /g' internal/daemon/backend_test.go
```

Then append:

```go
func TestActivityForOneRepo(t *testing.T) {
	b, _, _ := newBackend(t)
	now := time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	ctx := context.Background()
	b.Hist.Append(model.HistoryEntry{ID: "a", Repo: "darkmem", RunID: 1, Conclusion: "success",
		StartedAt: now.Add(-20 * time.Minute), FinishedAt: now.Add(-10 * time.Minute)})
	all, _ := b.Activity(ctx, "24h", "", time.UTC)
	one, err := b.Activity(ctx, "24h", "DarkMem", time.UTC)
	if err != nil || all.Capacity == nil || one.Capacity != nil || len(one.Repos) != 1 || one.Repos[0].Repo != "darkmem" {
		t.Fatalf("one repo %+v capacity %v err %v", one.Repos, one.Capacity, err)
	}
	b.Hist.Append(model.HistoryEntry{ID: "b", Repo: "darkmem", RunID: 2, Conclusion: "failure",
		StartedAt: now.Add(-9 * time.Minute), FinishedAt: now.Add(-5 * time.Minute)})
	if again, _ := b.Activity(ctx, "24h", "darkmem", time.UTC); again.Repos[0].Week.Failed != 0 {
		t.Fatal("a differently cased name missed the cache entry")
	}
	if other, _ := b.Activity(ctx, "24h", "darkcloud", time.UTC); len(other.Repos) != 1 || other.Repos[0].Repo != "darkcloud" {
		t.Fatalf("another repo shared the cache entry: %+v", other.Repos)
	}
}
```

In `internal/api/api_test.go`, replace the fake's `Activity`:

```go
func (f *fakeBackend) Activity(_ context.Context, window, repo string, loc *time.Location) (model.Activity, error) {
	call := window + "|" + loc.String()
	if repo != "" {
		call += "|" + repo
	}
	f.activityCalls = append(f.activityCalls, call)
	return f.activity, nil
}
```

In `TestActivityRoute`, after the `/activity?window=24h` call and before the `reflect.DeepEqual` check, add:

```go
	if err := c.call(ctx, http.MethodGet, "/activity?window=24h&repo=ghr", nil, &got); err != nil {
		t.Fatal(err)
	}
```

and change the expected list and the final count:

```go
	if !reflect.DeepEqual(b.activityCalls, []string{"1h|Asia/Ho_Chi_Minh", "24h|Local", "24h|Local|ghr"}) {
		t.Fatalf("backend saw %v", b.activityCalls)
	}
```

```go
	if len(b.activityCalls) != 3 {
		t.Fatalf("a bad request reached the backend: %v", b.activityCalls)
	}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/daemon/ ./internal/api/`
Expected: FAIL to compile: `too many arguments in call to b.Activity` and `*fakeBackend does not implement Backend`.

- [ ] **Step 3: Write the implementation**

In `internal/api/server.go`, change the interface line:

```go
	Activity(ctx context.Context, window, repo string, loc *time.Location) (model.Activity, error)
```

and the call in the `GET /activity` handler:

```go
		a, err := b.Activity(r.Context(), window, q.Get("repo"), loc)
```

In `internal/daemon/backend.go`, replace the comment and the start of `Activity`, and pass the repo into the input:

```go
// Activity builds the activity view for window in loc, narrowed to one
// repository when repo is set, reusing one built less than activityTTL ago.
// Each window, zone and repository has its own lock, so a slow 30d build
// never holds up a 1h poll. Repository names match case-insensitively.
func (b *Backend) Activity(ctx context.Context, window, repo string, loc *time.Location) (model.Activity, error) {
	key := window + "|" + loc.String()
	if repo != "" {
		key += "|" + strings.ToLower(repo)
	}
	e := b.activityEntry(key)
	e.mu.Lock()
	defer e.mu.Unlock()
```

```go
	in := activity.Input{
		Window: window, Loc: loc, Now: now, Retention: cfg.HistoryRetention.D(), Repo: repo,
		History: hist, Instances: b.M.Status().Instances,
	}
```

The rest of `Activity` is unchanged.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -race ./internal/daemon/ ./internal/api/`
Expected: PASS, including the existing cache tests that use the keys `1h|UTC` and `30d|UTC`.

- [ ] **Step 5: Commit**

```bash
gofmt -l . ; go vet ./... ; go test ./...
git add internal/daemon internal/api
git commit -m "feat(api): serve one repository's activity"
```

---

### Task 7: Report step start and finish times

**Files:**
- Modify: `internal/github/client.go:48-53` (`Step`)
- Modify: `internal/model/model.go:125-130` (`Step`)
- Modify: `internal/daemon/backend.go` (`fetchSteps`)
- Test: `internal/github/client_test.go`, `internal/daemon/backend_test.go`
- Modify: `web/fixtures_test.go` (the `steps` fixture), generated `web/src/api/fixtures/steps.json`
- Modify: `web/src/api/types.ts`, `web/src/api/types.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `github.Step` and `model.Step` time fields, TypeScript `Step.started_at` and `Step.completed_at` (Contracts).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Append to `internal/github/client_test.go`:

```go
func TestJobStepTimes(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"jobs":[{"id":7,"steps":[`+
			`{"number":1,"name":"checkout","status":"completed","conclusion":"success","started_at":"2026-10-05T12:00:00Z","completed_at":"2026-10-05T12:00:04Z"},`+
			`{"number":2,"name":"test","status":"queued","conclusion":null,"started_at":null,"completed_at":null}]}]}`)
	})
	jobs, err := c.ListJobs(context.Background(), "r", 5)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs %+v err %v", jobs, err)
	}
	s := jobs[0].Steps
	if len(s) != 2 || s[0].StartedAt == nil || s[0].CompletedAt == nil ||
		!s[0].CompletedAt.Equal(time.Date(2026, 10, 5, 12, 0, 4, 0, time.UTC)) || s[1].StartedAt != nil || s[1].CompletedAt != nil {
		t.Fatalf("steps %+v", s)
	}
}
```

Append to `internal/daemon/backend_test.go`:

```go
func TestRunnerStepsCarryTimes(t *testing.T) {
	b, m, gh := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa"}}
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	end := start.Add(4 * time.Second)
	gh.jobs = map[int64][]github.Job{55: {{RunnerName: "ghr-darkcloud-aaaaaa", Steps: []github.Step{
		{Number: 1, Name: "checkout", Status: "completed", Conclusion: "success", StartedAt: &start, CompletedAt: &end},
		{Number: 2, Name: "test", Status: "queued"},
	}}}}
	s, err := b.RunnerSteps(context.Background(), "aaaaaa")
	if err != nil || len(s) != 2 || s[0].StartedAt == nil || !s[0].StartedAt.Equal(start) || s[0].CompletedAt == nil ||
		!s[0].CompletedAt.Equal(end) || s[1].StartedAt != nil || s[1].CompletedAt != nil {
		t.Fatalf("steps %+v err %v", s, err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/github/ ./internal/daemon/ -run 'TestJobStepTimes|TestRunnerStepsCarryTimes'`
Expected: FAIL to compile: `s[0].StartedAt undefined` and `unknown field StartedAt in struct literal of type github.Step`.

- [ ] **Step 3: Write the implementation**

In `internal/github/client.go`:

```go
type Step struct {
	Number      int        `json:"number"`
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	Conclusion  string     `json:"conclusion"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
}
```

In `internal/model/model.go`:

```go
type Step struct {
	Number     int    `json:"number"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	// A pending step has neither time, and a running step only StartedAt.
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}
```

In `internal/daemon/backend.go`, in `fetchSteps`:

```go
			steps = append(steps, model.Step{Number: s.Number, Name: s.Name, Status: s.Status, Conclusion: s.Conclusion,
				StartedAt: s.StartedAt, CompletedAt: s.CompletedAt})
```

In `web/fixtures_test.go`, replace the `steps` fixture:

```go
		"steps": []model.Step{
			{Number: 1, Name: "Set up job", Status: "completed", Conclusion: "success",
				StartedAt: ptr(at(-4 * time.Minute)), CompletedAt: ptr(at(-4*time.Minute + 6*time.Second))},
			{Number: 2, Name: "Run tests", Status: "in_progress", StartedAt: ptr(at(-4*time.Minute + 6*time.Second))},
			{Number: 3, Name: "Post checkout", Status: "queued"},
		},
```

Regenerate: `GHR_UPDATE_FIXTURES=1 go test ./web/`

In `web/src/api/types.ts`:

```ts
export interface Step {
  number: number
  name: string
  status: string
  conclusion: string
  started_at?: string
  completed_at?: string
}
```

In `web/src/api/types.test.ts`, inside the `describe("type fixtures", …)` block, add:

```ts
  it("steps carry their times when GitHub reports them", () => {
    expect(steps[0]?.started_at).toBe("2026-10-03T14:01:00Z")
    expect(steps[0]?.completed_at).toBe("2026-10-03T14:01:06Z")
    expect(steps[1]).not.toHaveProperty("completed_at")
    expect(steps[2]).not.toHaveProperty("started_at")
  })
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/github/ ./internal/daemon/ ./web/`, then in `web/`: `npm run typecheck && npx vitest run src/api/types.test.ts`
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
gofmt -l . ; go vet ./... ; go test ./...
git add internal/github internal/model/model.go internal/daemon web/fixtures_test.go web/src/api
git commit -m "feat(api): report step start and finish times"
```

---

### Task 8: Report the Docker data root path

**Files:**
- Modify: `internal/system/system.go` (`DiskUsage`, `DataRootBytes`)
- Modify: `internal/model/model.go:17-20` (`Status`)
- Modify: `internal/runner/manager.go:446` (`Status`)
- Test: `internal/system/system_test.go`, `internal/runner/cleanup_test.go`
- Modify: `web/fixtures_test.go` (the `status` fixture), generated `web/src/api/fixtures/status.json`
- Modify: `web/src/api/types.ts`, `web/src/api/types.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `system.DiskUsage.Root`, `model.Status.DiskRoot` (`disk_root`), TypeScript `Status.disk_root` (Contracts).


**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `internal/system/system_test.go`, in `TestDockerParsing`, change the `DataRootBytes` check:

```go
	du, err := d.DataRootBytes(ctx)
	if err != nil || du != (DiskUsage{Pct: 81, Used: 81000000000, Total: 100000000000, Root: "/var/lib/docker"}) {
		t.Fatalf("bytes = %+v err = %v", du, err)
	}
```

In `internal/runner/cleanup_test.go`, make `bytesDocker` report a root:

```go
func (b bytesDocker) DataRootBytes(ctx context.Context) (system.DiskUsage, error) {
	pct, err := b.DataRootUsage(ctx)
	return system.DiskUsage{Pct: pct, Used: int64(pct) * 1_000_000_000, Total: 100_000_000_000, Root: "/var/lib/docker"}, err
}
```

and replace the two checks in `TestStatusCarriesDiskBytes`:

```go
	if st := h.m.Status(); st.DiskPct != 61 || st.DiskUsedBytes != 0 || st.DiskTotalBytes != 0 || st.DiskRoot != "" {
		t.Fatalf("percent-only docker: %d %d %d %q", st.DiskPct, st.DiskUsedBytes, st.DiskTotalBytes, st.DiskRoot)
	}
```

```go
	if st := h.m.Status(); st.DiskPct != 62 || st.DiskUsedBytes != 62_000_000_000 || st.DiskTotalBytes != 100_000_000_000 || st.DiskRoot != "/var/lib/docker" {
		t.Fatalf("bytes docker: %d %d %d %q", st.DiskPct, st.DiskUsedBytes, st.DiskTotalBytes, st.DiskRoot)
	}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/system/ ./internal/runner/ -run 'TestDockerParsing|TestStatusCarriesDiskBytes'`
Expected: FAIL to compile: `unknown field Root in struct literal of type DiskUsage` and `st.DiskRoot undefined`.

- [ ] **Step 3: Write the implementation**

In `internal/system/system.go`, replace `DiskUsage` and `DataRootBytes`:

```go
// DiskUsage is the data-root filesystem's use: the percentage as df rounds
// it, bytes, and the data root path.
type DiskUsage struct {
	Pct   int
	Used  int64
	Total int64
	Root  string
}
```

```go
// DataRootBytes returns the used percentage, used bytes and size of the
// filesystem holding Docker's data root, and the data root path.
func (d Docker) DataRootBytes(ctx context.Context) (DiskUsage, error) {
	out, err := d.Run(ctx, "docker", "info", "--format", "{{.DockerRootDir}}")
	if err != nil {
		return DiskUsage{}, err
	}
	root := strings.TrimSpace(string(out))
	out, err = d.Run(ctx, "df", "-B1", "--output=pcent,used,size", root)
	if err != nil {
		return DiskUsage{}, err
	}
	l := lines(out)
	var f []string
	if len(l) >= 2 {
		f = strings.Fields(l[len(l)-1])
	}
	if len(f) != 3 {
		return DiskUsage{}, fmt.Errorf("unexpected df output %q", out)
	}
	pct, err1 := strconv.Atoi(strings.TrimSuffix(f[0], "%"))
	used, err2 := strconv.ParseInt(f[1], 10, 64)
	total, err3 := strconv.ParseInt(f[2], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return DiskUsage{}, fmt.Errorf("unexpected df output %q", out)
	}
	return DiskUsage{Pct: pct, Used: used, Total: total, Root: root}, nil
}
```

In `internal/model/model.go`, after `DiskTotalBytes` in `Status`:

```go
	// DiskRoot is Docker's data root path, empty until measured in bytes.
	DiskRoot string `json:"disk_root,omitempty"`
```

In `internal/runner/manager.go`, in `Status`:

```go
		DiskPct:       m.disk.Pct, DiskUsedBytes: m.disk.Used, DiskTotalBytes: m.disk.Total, DiskRoot: m.disk.Root,
```

In `web/fixtures_test.go`, in the `status` fixture (not `status-degraded`), add `DiskRoot: "/var/lib/docker"` after `DiskTotalBytes: 240_000_000_000`. Regenerate: `GHR_UPDATE_FIXTURES=1 go test ./web/`

In `web/src/api/types.ts`, in `Status`, after `disk_total_bytes: number`:

```ts
  disk_root?: string
```

In `web/src/api/types.test.ts`, in the test "status carries the Dashboard fields", add:

```ts
    expect(status.disk_root).toBe("/var/lib/docker")
    expect(degraded).not.toHaveProperty("disk_root")
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/system/ ./internal/runner/ ./web/`, then in `web/`: `npm run typecheck && npx vitest run src/api/types.test.ts`
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
gofmt -l . ; go vet ./... ; go test ./...
git add internal/system internal/model/model.go internal/runner web/fixtures_test.go web/src/api
git commit -m "feat(api): report the Docker data root path"
```
