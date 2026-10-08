# ghr web UI redesign, plan 1: backend

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the redesigned Dashboard its data: three hours of minute metrics and 30 days of hourly rollups that survive restarts, disk bytes, the rate limit and each repo's oldest waiting job in the status, and a new `GET /activity` endpoint.

**Architecture:** `internal/metrics` grows a 180-sample minute ring and a 720-entry hourly rollup ring, persisted atomically to `/var/lib/ghr/metrics.json`. A new pure package `internal/activity` turns history, live instances and metrics into lanes (1h, 3h) or time buckets (24h, 7d, 30d) in a requested time zone. The daemon's `Backend.Activity` caches each window and zone for 5 seconds, and `internal/api` serves it at `GET /activity`.

**Tech Stack:** Go 1.26, standard library only (`time/tzdata` for zone data). Tests use the standard `testing` package.

**Spec:** docs/superpowers/specs/2026-10-08-ghr-web-ui-identity-dashboard-design.md (§4 and the Go parts of §7)

**Execution:** inline — `claude --model sonnet --effort high` — 1 of 8 tasks is heavy (Task 7, total 5) and is delegated; the self-implemented tasks top out at total 4 (Sonnet medium), raised to high because a task is delegated.

**Plan review:** 2026-10-08 — dr-superpowers:judge-opus — executability 17 / coherence 18 / coverage 16 / assumptions 13 (round 1)

## Global Constraints

- Go only in this plan; nothing under `web/src` changes except the generated fixture JSON files under `web/src/api/fixtures/`.
- No new module dependencies. `time/tzdata` is standard library.
- `GET /metrics` keeps its response shape and still returns at most the last 60 minutes of samples.
- Persisted files are written atomically: temp file in the same directory, then rename, mode 0600.
- Every timestamp in an activity response is in the requested zone (`.In(loc)`), so it marshals as RFC 3339 with that zone's offset.
- Comments only where the why is non-obvious; match the surrounding code's style. English only.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no period.
- After any change to a type in `internal/model` that a fixture marshals, regenerate fixtures in the same commit: `GHR_UPDATE_FIXTURES=1 go test ./web/`.
- Run `gofmt -l .` and `go vet ./...` before each commit; both must print nothing.

## Contracts

**`internal/model` (new types and fields)**

```go
// MetricRollup is one UTC hour of metric samples.
type MetricRollup struct {
	At        time.Time `json:"at"`
	Samples   int       `json:"samples"`
	QueuedMax int       `json:"queued_max"`
	CPUAvg    *float64  `json:"cpu_avg,omitempty"`
	MemAvg    *int64    `json:"mem_avg,omitempty"`
}

// Status gains:
	RateLimit      int   `json:"rate_limit,omitempty"`
	DiskUsedBytes  int64 `json:"disk_used_bytes"`
	DiskTotalBytes int64 `json:"disk_total_bytes"`

// RepoStatus gains:
	OldestQueuedAt *time.Time `json:"oldest_queued_at,omitempty"`

type Activity struct {
	Window      string           `json:"window"`
	TZ          string           `json:"tz"`
	From        time.Time        `json:"from"`
	To          time.Time        `json:"to"`
	Capacity    *int             `json:"capacity"`
	HistoryFrom time.Time        `json:"history_from"`
	Lanes       []ActivityLane   `json:"lanes"`
	Buckets     []ActivityBucket `json:"buckets"`
	Waiting     []ActivityPoint  `json:"waiting"`
	CPU         []ActivityCPU    `json:"cpu"`
	Repos       []ActivityRepo   `json:"repos"`
}
type ActivityLane struct {
	Runs []ActivityRun `json:"runs"`
}
type ActivityRun struct {
	InstanceID string            `json:"instance_id"`
	Repo       string            `json:"repo"`
	Workflow   string            `json:"workflow,omitempty"`
	Job        string            `json:"job,omitempty"`
	RunNumber  string            `json:"run_number,omitempty"`
	Segments   []ActivitySegment `json:"segments"`
	HTMLURL    string            `json:"html_url,omitempty"`
}
type ActivitySegment struct {
	State string     `json:"state"` // starting | warm | running | succeeded | failed | cancelled | skipped
	From  time.Time  `json:"from"`
	To    *time.Time `json:"to"`
}
type ActivityBucket struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	BusyMinutes float64   `json:"busy_minutes"`
	BusyPct     *float64  `json:"busy_pct"`
	Succeeded   int       `json:"succeeded"`
	Failed      int       `json:"failed"`
	Cancelled   int       `json:"cancelled"`
	WaitingMax  *int      `json:"waiting_max"`
	CPUAvg      *float64  `json:"cpu_avg"`
}
type ActivityPoint struct {
	At    time.Time `json:"at"`
	Value int       `json:"value"`
}
type ActivityCPU struct {
	At  time.Time `json:"at"`
	CPU *float64  `json:"cpu"`
	Mem *int64    `json:"mem"`
}
type ActivityRepo struct {
	Repo  string         `json:"repo"`
	Hours []ActivityHour `json:"hours"`
}
type ActivityHour struct {
	Start     time.Time `json:"start"`
	Succeeded int       `json:"succeeded"`
	Failed    int       `json:"failed"`
	Cancelled int       `json:"cancelled"`
}
```

`HistoryFrom` is always `Now - Retention`; the config rejects a retention under one day (`internal/config/config.go:289-295`). `TZ` is the zone's name as Go reports it, so a request without `tz` answers `"Local"`; the web UI always sends `tz`. Every slice field marshals as `[]`, never `null`.

**`internal/metrics`**

- `func (s *Sampler) Minutes() []model.MetricSample`: a copy of the minute ring, oldest first, at most 180.
- `func (s *Sampler) Hours() []model.MetricRollup`: a copy of the closed rollups, oldest first, at most 720, followed by the open hour's rollup when one is open.
- `func (s *Sampler) Metrics() model.Metrics`: unchanged shape; `Samples` is the last 60 minutes.
- `Sampler.Path string`: the persistence file; empty disables persistence.
- `func (s *Sampler) Save() error` and `func (s *Sampler) Load() error`.

**`internal/system`**

- `type DiskUsage struct { Pct int; Used, Total int64 }`
- `func (d Docker) DataRootBytes(ctx context.Context) (DiskUsage, error)`

**`internal/activity`**

```go
func Valid(window string) bool // "1h", "3h", "24h", "7d", "30d"

type Input struct {
	Window    string
	Loc       *time.Location // nil means UTC
	Now       time.Time
	Capacity  *int // global_max in queue mode, nil in all mode
	Retention time.Duration
	History   []model.HistoryEntry
	Instances []model.InstanceStatus
	Repos     []string // configured repository names
	Minutes   []model.MetricSample
	Hours     []model.MetricRollup
}

func Build(in Input) model.Activity
```

**`internal/daemon`**

- `Options.MetricsPath string`, default `/var/lib/ghr/metrics.json`.
- `func (b *Backend) Activity(ctx context.Context, window string, loc *time.Location) (model.Activity, error)`

**`internal/api`**

- `api.Backend` gains `Activity(ctx context.Context, window string, loc *time.Location) (model.Activity, error)`.
- Route `GET /activity?window=<1h|3h|24h|7d|30d>&tz=<IANA zone>`. A missing `tz` means `time.Local`. A bad `window` or unknown `tz` answers 400 with the error body.

**Fixtures:** `web/src/api/fixtures/activity-lanes.json` and `web/src/api/fixtures/activity-buckets.json` (Task 8). Plan 2 parses them into its TypeScript types.

## Assumptions (evidence)

- The metrics ring holds 60 samples, sampled once a minute, in memory only: `internal/metrics/metrics.go:18`, `internal/daemon/run.go:206` (read 2026-10-08).
- `history.Store.Query("", "", 0)` returns every entry, newest first, reading the whole file: `internal/history/history.go:86-107`.
- `HistoryEntry.ID` and `InstanceStatus.ID` are both the instance ID: `internal/model/model.go:79,105`, per the Fable review of the spec (2026-10-08).
- Instance states are exactly `starting`, `idle`, `busy`, `cleaning`: `internal/sched/sched.go:14-19`.
- `DataRootUsage` runs `docker info` then `df --output=pcent`: `internal/system/system.go:272-286`; the test fake runner matches commands by prefix: `internal/system/system_test.go:21-35`.
- `cleanup.go` already imports `internal/system`: `internal/runner/cleanup.go:20`.
- The daemon test fake GitHub returns `f.meta` from `TokenMeta()`: `internal/daemon/backend_test.go:84-109`.
- The daemon test store config is queue mode, `global_max: 2`, repos `darkcloud` and `darkmem`, default `history_retention` 30d: `internal/daemon/store_test.go:17-26`, `internal/config/config.go:153`.
- The runner test harness config has repos `darkcloud` and `darkmem`: `internal/runner/fakes_test.go:522-532`.
- `respond` turns an `*api.Error` into its status: `internal/api/server.go:278`; `BadRequest` builds a 400: `internal/api/server.go:51`.
- `df -B1 --output=pcent,used,size <dir>` prints a header line and one line `NN% <used bytes> <size bytes>` (GNU coreutils; the LXC is Debian). Unverified on the LXC; Task 3's manual check confirms it.
- Reaching `/activity` over the web needs no new code: the web listener mounts the whole socket API under `/api/` with `http.StripPrefix`, and `internal/webui/handler_test.go` already covers that a valid `/api/*` request reaches the API handler with the prefix stripped. Task 8 tests the route on the API handler itself.
- Each run carries one segment: `InstanceStatus.Since` (`internal/model/model.go:83`) records only the current state, so the warm-then-running chain in the spec's §4.2 example cannot be built from today's data. The segment list stays a list so a later change can add the chain without a format change; plan 2 renders whatever segments arrive.
- Plan review round 1 scored assumptions 13 (borderline). Decision: proceed. Its Important findings (the `history_from` null case, one-segment runs, the untested periodic save) are fixed or recorded above.
- No external executor lane: `scripts/executors list` printed `codex`, whose gate printed `lane=false reason=plugin-not-enabled` (2026-10-08).
- The register rows for this spec (items 1 and 2) are user-facing and close with plan 2; this plan cites no Items.

## Handoff to plan 2

- The TypeScript mirrors of the new status fields (`rate_limit`, `disk_used_bytes`, `disk_total_bytes`, `oldest_queued_at`) and of `model.Activity` and its parts, with parse tests over the fixtures this plan generates.
- Runs arrive with one segment each (see Assumptions).

## Task index

1. Metrics: minute ring and hourly rollups
2. Metrics: persistence and daemon wiring
3. Status: disk bytes
4. Status: rate limit and oldest waiting job
5. Activity: model types and lanes
6. Activity: buckets and repository hours
7. Daemon: cached activity
8. API route and fixtures

---

### Task 1: Metrics: minute ring and hourly rollups

**Files:**
- Modify: `internal/model/model.go` (after `MetricSample`)
- Modify: `internal/metrics/metrics.go`
- Test: `internal/metrics/metrics_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `model.MetricRollup`, `Sampler.Minutes`, `Sampler.Hours`, and the 60-sample `Sampler.Metrics` (Contracts, `internal/metrics`).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Append to `internal/metrics/metrics_test.go`:

```go
func TestMinuteRingKeepsThreeHoursAndServesOne(t *testing.T) {
	h := newHost(t)
	h.write(t, "cpu.stat", "usage_usec 0\n")
	for i := 0; i < 200; i++ {
		h.s.Sample()
		h.now = h.now.Add(time.Minute)
	}
	mins := h.s.Minutes()
	if len(mins) != 180 {
		t.Fatalf("minutes = %d", len(mins))
	}
	m := h.s.Metrics()
	if len(m.Samples) != 60 || !m.Samples[59].At.Equal(mins[179].At) || !m.Samples[0].At.Equal(mins[120].At) {
		t.Fatalf("metrics serves %d samples ending %v", len(m.Samples), m.Samples[len(m.Samples)-1].At)
	}
}

func TestHourlyRollups(t *testing.T) {
	h := newHost(t) // 12:00 UTC
	queued := 0
	h.s.Counts = func() Snapshot { return Snapshot{Live: 1, Queued: queued} }
	usage := int64(0)
	for i := 0; i < 60; i++ { // 12:00 to 12:59
		queued = i % 7
		usage += 30_000_000 // 30 s of CPU a minute on 2 CPUs is 25%
		h.write(t, "cpu.stat", "usage_usec "+strconv.FormatInt(usage, 10)+"\n")
		h.s.Sample()
		h.now = h.now.Add(time.Minute)
	}
	if hrs := h.s.Hours(); len(hrs) != 1 || hrs[0].Samples != 60 {
		t.Fatalf("open hour %+v", hrs)
	}
	h.s.Sample() // 13:00 closes 12:00
	hrs := h.s.Hours()
	if len(hrs) != 2 {
		t.Fatalf("hours %+v", hrs)
	}
	r := hrs[0]
	if !r.At.Equal(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)) || r.Samples != 60 || r.QueuedMax != 6 {
		t.Fatalf("closed hour %+v", r)
	}
	if r.CPUAvg == nil || *r.CPUAvg != 25 || r.MemAvg == nil || *r.MemAvg != (8388608-8204288)*1024 {
		t.Fatalf("averages %+v", r)
	}
	if hrs[1].Samples != 1 || !hrs[1].At.Equal(time.Date(2026, 10, 5, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("new open hour %+v", hrs[1])
	}
}

func TestClockJumps(t *testing.T) {
	h := newHost(t)
	h.write(t, "cpu.stat", "usage_usec 0\n")
	h.s.Sample()                     // 12:00
	h.now = h.now.Add(3 * time.Hour) // 15:00: 12:00 closes, 13:00 and 14:00 stay absent
	h.s.Sample()
	h.now = h.now.Add(-2 * time.Hour) // 13:00: a backwards clock step
	h.s.Sample()
	hrs := h.s.Hours()
	if len(hrs) != 2 || hrs[0].At.Hour() != 12 || hrs[1].At.Hour() != 15 || hrs[1].Samples != 1 {
		t.Fatalf("hours %+v", hrs)
	}
	if n := len(h.s.Minutes()); n != 3 {
		t.Fatalf("minutes = %d", n)
	}
}

func TestHourRingKeepsThirtyDays(t *testing.T) {
	h := newHost(t)
	h.write(t, "cpu.stat", "usage_usec 0\n")
	for i := 0; i < 30*24+5; i++ {
		h.s.Sample()
		h.now = h.now.Add(time.Hour)
	}
	if n := len(h.s.Hours()); n != 30*24+1 { // 720 closed and the open one
		t.Fatalf("hours = %d", n)
	}
}
```

`strconv` is already imported by this test file.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/metrics/ -run 'TestMinuteRing|TestHourlyRollups|TestClockJumps|TestHourRing'`
Expected: FAIL to compile with `h.s.Minutes undefined` and `h.s.Hours undefined`.

- [ ] **Step 3: Add the model type**

In `internal/model/model.go`, directly after the `MetricSample` struct, add:

```go
// MetricRollup is one UTC hour of metric samples. CPUAvg and MemAvg are
// absent when no sample in the hour carried them.
type MetricRollup struct {
	At        time.Time `json:"at"`
	Samples   int       `json:"samples"`
	QueuedMax int       `json:"queued_max"`
	CPUAvg    *float64  `json:"cpu_avg,omitempty"`
	MemAvg    *int64    `json:"mem_avg,omitempty"`
}
```

- [ ] **Step 4: Implement the rings**

In `internal/metrics/metrics.go`:

Replace the two-line package comment with:

```go
// Package metrics keeps three hours of one-minute samples of the runner
// counts and the container's CPU and memory use, and 30 days of hourly rollups.
```

Replace the `keep` constant and its comment with:

```go
const (
	keepMinutes = 180 // three hours at one a minute
	showMinutes = 60  // what GET /metrics serves
	keepHours   = 30 * 24
)
```

Add three fields to `Sampler`, after `samples []model.MetricSample`:

```go
	hours   []model.MetricRollup
	open    *openHour
	closed  bool // an hour closed since the last save
```

Replace the ring trimming at the end of `Sample()`:

```go
	s.samples = append(s.samples, smp)
	if len(s.samples) > keep {
		s.samples = s.samples[len(s.samples)-keep:]
	}
```

with:

```go
	s.samples = append(s.samples, smp)
	if len(s.samples) > keepMinutes {
		s.samples = s.samples[len(s.samples)-keepMinutes:]
	}
	s.addToHour(smp)
```

Replace `Metrics()` with:

```go
// Metrics returns a copy of the last hour of samples and the current figures.
func (s *Sampler) Metrics() model.Metrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.cur
	start := max(len(s.samples)-showMinutes, 0)
	m.Samples = append([]model.MetricSample{}, s.samples[start:]...)
	return m
}

// Minutes returns a copy of every kept minute sample, oldest first.
func (s *Sampler) Minutes() []model.MetricSample {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.MetricSample{}, s.samples...)
}

// Hours returns a copy of the closed hourly rollups, oldest first, followed
// by the hour in progress.
func (s *Sampler) Hours() []model.MetricRollup {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]model.MetricRollup{}, s.hours...)
	if s.open != nil {
		out = append(out, s.open.rollup())
	}
	return out
}

// openHour accumulates the samples of the UTC hour in progress.
type openHour struct {
	At        time.Time `json:"at"`
	Samples   int       `json:"samples"`
	QueuedMax int       `json:"queued_max"`
	CPUSum    float64   `json:"cpu_sum"`
	CPUN      int       `json:"cpu_n"`
	MemSum    float64   `json:"mem_sum"`
	MemN      int       `json:"mem_n"`
}

func (o *openHour) add(smp model.MetricSample) {
	o.Samples++
	o.QueuedMax = max(o.QueuedMax, smp.Queued)
	if smp.CPU != nil {
		o.CPUSum += *smp.CPU
		o.CPUN++
	}
	if smp.Mem != nil {
		o.MemSum += float64(*smp.Mem)
		o.MemN++
	}
}

func (o *openHour) rollup() model.MetricRollup {
	r := model.MetricRollup{At: o.At, Samples: o.Samples, QueuedMax: o.QueuedMax}
	if o.CPUN > 0 {
		v := o.CPUSum / float64(o.CPUN)
		r.CPUAvg = &v
	}
	if o.MemN > 0 {
		v := int64(o.MemSum / float64(o.MemN))
		r.MemAvg = &v
	}
	return r
}

// addToHour folds smp into the open UTC hour, closing it when smp belongs to
// a later one. Rollups are keyed on UTC so every one spans 60 minutes; a
// sample from before the open hour (a backwards clock step) is left out.
// The caller holds s.mu.
func (s *Sampler) addToHour(smp model.MetricSample) {
	hour := smp.At.UTC().Truncate(time.Hour)
	if s.open != nil && hour.Before(s.open.At) {
		return
	}
	if s.open != nil && hour.After(s.open.At) {
		s.closeHour()
	}
	if s.open == nil {
		s.open = &openHour{At: hour}
	}
	s.open.add(smp)
}

// closeHour moves the open hour into the rollup ring. The caller holds s.mu.
func (s *Sampler) closeHour() {
	s.hours = append(s.hours, s.open.rollup())
	if len(s.hours) > keepHours {
		s.hours = s.hours[len(s.hours)-keepHours:]
	}
	s.open = nil
	s.closed = true
}
```

- [ ] **Step 5: Run the package tests**

Run: `go test ./internal/metrics/`
Expected: PASS, including the existing `TestQuotaAndRing` (it still sees 60 samples from `Metrics()`).

- [ ] **Step 6: Run the fixture and API tests**

Run: `go test ./web/ ./internal/api/ ./internal/daemon/`
Expected: PASS. `GET /metrics` and its fixture are unchanged.

- [ ] **Step 7: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/model/model.go internal/metrics/metrics.go internal/metrics/metrics_test.go
git commit -m "feat(metrics): keep 3h of minutes and hourly rollups"
```

### Task 2: Metrics: persistence and daemon wiring

**Files:**
- Modify: `internal/metrics/metrics.go`
- Modify: `internal/daemon/run.go` (`Options`, `DefaultOptions`, `start`)
- Test: `internal/metrics/metrics_test.go`, `internal/daemon/metrics_load_test.go` (create)

**Interfaces:**
- Consumes: `openHour`, `Sampler.Minutes`, `Sampler.Hours` from Task 1.
- Produces: `Sampler.Path`, `Sampler.Save`, `Sampler.Load`, `Options.MetricsPath` (Contracts).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

- [ ] **Step 1: Write the failing tests**

Append to `internal/metrics/metrics_test.go`, adding `"bytes"`, `"encoding/json"` and `"runtime"` to its imports:

```go
func sameJSON(t *testing.T, a, b any) bool {
	t.Helper()
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	return bytes.Equal(x, y)
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	h := newHost(t)
	h.write(t, "cpu.stat", "usage_usec 0\n")
	h.s.Path = filepath.Join(h.dir, "state", "metrics.json")
	for i := 0; i < 90; i++ { // 12:00 to 13:29: one closed hour and an open one
		h.s.Sample()
		h.now = h.now.Add(time.Minute)
	}
	if err := h.s.Save(); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if fi, err := os.Stat(h.s.Path); err != nil || fi.Mode().Perm() != 0o600 {
			t.Fatalf("file %v %v", fi, err)
		}
	}
	back := NewSampler(h.s.Counts, h.s.Disk)
	back.Now = func() time.Time { return h.now }
	back.Path = h.s.Path
	if err := back.Load(); err != nil {
		t.Fatal(err)
	}
	if !sameJSON(t, back.Minutes(), h.s.Minutes()) || !sameJSON(t, back.Hours(), h.s.Hours()) {
		t.Fatalf("round trip lost data:\n%+v\n%+v", back.Hours(), h.s.Hours())
	}
	back.Sample() // 13:30 keeps adding to the resumed open hour
	if hrs := back.Hours(); len(hrs) != 2 || hrs[1].Samples != 31 {
		t.Fatalf("resumed hour %+v", hrs)
	}
}

func TestLoadDropsStaleAndClosesAnOldOpenHour(t *testing.T) {
	h := newHost(t)
	h.write(t, "cpu.stat", "usage_usec 0\n")
	h.s.Path = filepath.Join(h.dir, "metrics.json")
	for i := 0; i < 30; i++ { // 12:00 to 12:29, the hour still open
		h.s.Sample()
		h.now = h.now.Add(time.Minute)
	}
	if err := h.s.Save(); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(4 * time.Hour) // 16:30: every minute is older than three hours
	back := NewSampler(h.s.Counts, h.s.Disk)
	back.Now = func() time.Time { return h.now }
	back.Path = h.s.Path
	if err := back.Load(); err != nil {
		t.Fatal(err)
	}
	if n := len(back.Minutes()); n != 0 {
		t.Fatalf("minutes = %d", n)
	}
	hrs := back.Hours()
	if len(hrs) != 1 || hrs[0].Samples != 30 || !hrs[0].At.Equal(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("hours %+v", hrs)
	}
	back.Sample()
	if hrs := back.Hours(); len(hrs) != 2 || hrs[0].Samples != 30 || hrs[1].Samples != 1 {
		t.Fatalf("after a new sample %+v", hrs)
	}
}

func TestLoadMissingAndMalformed(t *testing.T) {
	s := NewSampler(func() Snapshot { return Snapshot{} }, func() int { return 0 })
	s.Path = filepath.Join(t.TempDir(), "metrics.json")
	if err := s.Load(); err != nil {
		t.Fatalf("missing file: %v", err)
	}
	if err := os.WriteFile(s.Path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Load(); err == nil {
		t.Fatal("malformed file loaded")
	}
	if len(s.Minutes()) != 0 || len(s.Hours()) != 0 {
		t.Fatal("malformed file left data behind")
	}
}

func TestSaveDue(t *testing.T) {
	h := newHost(t) // 12:00
	h.write(t, "cpu.stat", "usage_usec 0\n")
	if !h.s.saveDue() {
		t.Fatal("nothing saved yet, but no save due")
	}
	h.s.Sample()
	h.s.save() // no Path: only records the time
	if h.s.saveDue() {
		t.Fatal("due right after a save")
	}
	h.now = h.now.Add(14 * time.Minute)
	if h.s.saveDue() {
		t.Fatal("due after 14 minutes")
	}
	h.now = h.now.Add(time.Minute)
	if !h.s.saveDue() {
		t.Fatal("not due after 15 minutes")
	}
	h.now = h.now.Add(44 * time.Minute) // 12:59
	h.s.save()
	h.now = h.now.Add(2 * time.Minute) // 13:01: two minutes after a save, the next sample closes 12:00
	h.s.Sample()
	if !h.s.saveDue() {
		t.Fatal("not due after an hour closed")
	}
	h.s.save()
	if h.s.saveDue() {
		t.Fatal("the closed flag survived a save")
	}
}

func TestRunSavesOnStop(t *testing.T) {
	h := newHost(t)
	h.write(t, "cpu.stat", "usage_usec 0\n")
	h.s.Path = filepath.Join(h.dir, "metrics.json")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.s.Run(ctx, time.Hour); close(done) }()
	for i := 0; len(h.s.Minutes()) == 0 && i < 400; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if _, err := os.Stat(h.s.Path); err != nil {
		t.Fatalf("not saved on stop: %v", err)
	}
}
```

Create `internal/daemon/metrics_load_test.go`:

```go
package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darkraise/ghr/internal/events"
	"github.com/darkraise/ghr/internal/metrics"
)

func TestLoadMetricsWarnsOnAMalformedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := metrics.NewSampler(func() metrics.Snapshot { return metrics.Snapshot{} }, func() int { return 0 })
	s.Path = path
	ev := events.New()
	loadMetrics(s, ev)
	got := ev.After(0)
	if len(got) != 1 || got[0].Level != "warn" || !strings.Contains(got[0].Msg, "metrics history unreadable") {
		t.Fatalf("events %+v", got)
	}
}

func TestDefaultMetricsPath(t *testing.T) {
	if p := DefaultOptions().MetricsPath; p != "/var/lib/ghr/metrics.json" {
		t.Fatalf("MetricsPath = %q", p)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/metrics/ ./internal/daemon/ -run 'TestSaveAndLoad|TestLoadDrops|TestLoadMissing|TestSaveDue|TestRunSaves|TestLoadMetrics|TestDefaultMetricsPath'`
Expected: FAIL to compile: `h.s.Path undefined`, `h.s.saveDue undefined`, `undefined: loadMetrics`, `unknown field MetricsPath`.

- [ ] **Step 3: Implement persistence in the sampler**

In `internal/metrics/metrics.go`, add `"encoding/json"`, `"fmt"`, `"log"` and `"path/filepath"` to the imports. Add to `Sampler`, after `Disk func() int`:

```go
	// Path is where Save and Load keep the rings; empty keeps them in memory only.
	Path string
```

and after `closed bool`:

```go
	lastSave time.Time
```

Add below the constants:

```go
// saveEvery bounds what a crash loses.
const saveEvery = 15 * time.Minute

// state is the persisted form of the rings.
type state struct {
	Minutes []model.MetricSample `json:"minutes"`
	Hours   []model.MetricRollup `json:"hours"`
	Open    *openHour            `json:"open,omitempty"`
}
```

Replace `Run` with:

```go
// Run samples at once and then every interval until ctx ends. It saves when
// an hour closes, every saveEvery, and on the way out.
func (s *Sampler) Run(ctx context.Context, every time.Duration) {
	s.Sample()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.save()
			return
		case <-t.C:
			s.Sample()
			if s.saveDue() {
				s.save()
			}
		}
	}
}

// saveDue reports whether an hour closed or saveEvery passed since the last save.
func (s *Sampler) saveDue() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed || s.Now().Sub(s.lastSave) >= saveEvery
}

func (s *Sampler) save() {
	if err := s.Save(); err != nil {
		log.Printf("metrics: save %s: %v", s.Path, err)
	}
	s.mu.Lock()
	s.lastSave, s.closed = s.Now(), false
	s.mu.Unlock()
}

// Save writes the rings and the open hour to Path, atomically and readable
// by root only. An empty Path saves nothing.
func (s *Sampler) Save() error {
	if s.Path == "" {
		return nil
	}
	s.mu.Lock()
	data, err := json.Marshal(state{Minutes: s.samples, Hours: s.hours, Open: s.open})
	s.mu.Unlock()
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".metrics-*.json")
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
	return os.Rename(tmp.Name(), s.Path)
}

// Load restores what Save wrote, dropping minutes older than three hours and
// rollups older than 30 days. A saved open hour resumes when it is the
// current UTC hour and is closed as it stands otherwise. A missing file is an
// empty start; a malformed one returns an error and changes nothing.
func (s *Sampler) Load() error {
	if s.Path == "" {
		return nil
	}
	data, err := os.ReadFile(s.Path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var st state
	if err := json.Unmarshal(data, &st); err != nil {
		return fmt.Errorf("%s: %w", s.Path, err)
	}
	now := s.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = nil
	for _, m := range st.Minutes {
		if now.Sub(m.At) <= keepMinutes*time.Minute {
			s.samples = append(s.samples, m)
		}
	}
	if len(s.samples) > keepMinutes {
		s.samples = s.samples[len(s.samples)-keepMinutes:]
	}
	s.hours = nil
	for _, r := range st.Hours {
		if now.Sub(r.At) <= keepHours*time.Hour {
			s.hours = append(s.hours, r)
		}
	}
	s.open = nil
	if o := st.Open; o != nil {
		current := now.UTC().Truncate(time.Hour)
		switch {
		case o.At.Equal(current):
			s.open = o
		case o.At.Before(current) && now.Sub(o.At) <= keepHours*time.Hour:
			s.hours = append(s.hours, o.rollup())
		}
	}
	if len(s.hours) > keepHours {
		s.hours = s.hours[len(s.hours)-keepHours:]
	}
	return nil
}
```

- [ ] **Step 4: Wire the daemon**

In `internal/daemon/run.go`:

Add to `Options`, after `HistoryPath string`:

```go
	// MetricsPath keeps the metrics rings across restarts; empty keeps them in memory.
	MetricsPath string
```

Add to `DefaultOptions()`, after the `HistoryPath` line:

```go
		MetricsPath:           "/var/lib/ghr/metrics.json",
```

In `start`, directly after the `sampler := metrics.NewSampler(...)` statement and before `sampled := make(chan struct{})`, add:

```go
	sampler.Path = o.MetricsPath
	loadMetrics(sampler, ev)
```

Add this function below `notConfigured`:

```go
// loadMetrics restores the metrics rings; an unreadable file starts them
// empty with a warning, because losing graphs must not stop the daemon.
func loadMetrics(s *metrics.Sampler, ev *events.Ring) {
	if err := s.Load(); err != nil {
		ev.Add("warn", "", "metrics history unreadable, starting empty: %v", err)
	}
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/metrics/ ./internal/daemon/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/metrics/metrics.go internal/metrics/metrics_test.go internal/daemon/run.go internal/daemon/metrics_load_test.go
git commit -m "feat(metrics): persist the rings across restarts"
```

### Task 3: Status: disk bytes

**Files:**
- Modify: `internal/system/system.go` (`DataRootUsage`)
- Modify: `internal/runner/manager.go` (`diskPct` field, `Status`)
- Modify: `internal/runner/cleanup.go` (`checkDisk`, the post-prune measurement, the maintenance measurement, `setDisk`)
- Modify: `internal/model/model.go` (`Status`)
- Modify: `web/fixtures_test.go` (status values) and regenerate `web/src/api/fixtures/status.json`
- Test: `internal/system/system_test.go`, `internal/runner/cleanup_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `system.DiskUsage`, `Docker.DataRootBytes`, `Status.DiskUsedBytes`, `Status.DiskTotalBytes` (Contracts).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `internal/system/system_test.go`, in `TestDockerParsing`, replace the fake output entry

```go
		"df --output=pcent /var/lib/docker":                                             "Use%\n 81%\n",
```

with

```go
		"df -B1 --output=pcent,used,size /var/lib/docker":                               "Use%         Used    1B-blocks\n 81% 81000000000 100000000000\n",
```

and directly after the existing `pct, err := d.DataRootUsage(ctx)` check, add:

```go
	du, err := d.DataRootBytes(ctx)
	if err != nil || du != (DiskUsage{Pct: 81, Used: 81000000000, Total: 100000000000}) {
		t.Fatalf("bytes = %+v err = %v", du, err)
	}
```

Append to `internal/runner/cleanup_test.go`:

```go
// bytesDocker is a Docker that also reports the data root in bytes.
type bytesDocker struct{ *fakeDocker }

func (b bytesDocker) DataRootBytes(ctx context.Context) (system.DiskUsage, error) {
	pct, err := b.DataRootUsage(ctx)
	return system.DiskUsage{Pct: pct, Used: int64(pct) * 1_000_000_000, Total: 100_000_000_000}, err
}

func TestStatusCarriesDiskBytes(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{61}
	h.m.checkDisk(context.Background(), h.cfg)
	if st := h.m.Status(); st.DiskPct != 61 || st.DiskUsedBytes != 0 || st.DiskTotalBytes != 0 {
		t.Fatalf("percent-only docker: %d %d %d", st.DiskPct, st.DiskUsedBytes, st.DiskTotalBytes)
	}
	h.m.Docker = bytesDocker{h.docker}
	h.docker.usage = []int{62}
	h.m.checkDisk(context.Background(), h.cfg)
	if st := h.m.Status(); st.DiskPct != 62 || st.DiskUsedBytes != 62_000_000_000 || st.DiskTotalBytes != 100_000_000_000 {
		t.Fatalf("bytes docker: %d %d %d", st.DiskPct, st.DiskUsedBytes, st.DiskTotalBytes)
	}
}
```

If `cleanup_test.go` does not import `"github.com/darkraise/ghr/internal/system"`, add it.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/system/ ./internal/runner/ -run 'TestDockerParsing|TestStatusCarriesDiskBytes'`
Expected: FAIL to compile: `d.DataRootBytes undefined`, `undefined: DiskUsage`, `st.DiskUsedBytes undefined`.

- [ ] **Step 3: Implement the system side**

In `internal/system/system.go`, replace `DataRootUsage` and its comment with:

```go
// DiskUsage is the data-root filesystem's use: the percentage as df rounds
// it, and bytes.
type DiskUsage struct {
	Pct   int
	Used  int64
	Total int64
}

// DataRootUsage returns the used percentage of the filesystem holding Docker's data root.
func (d Docker) DataRootUsage(ctx context.Context) (int, error) {
	u, err := d.DataRootBytes(ctx)
	return u.Pct, err
}

// DataRootBytes returns the used percentage, used bytes and size of the
// filesystem holding Docker's data root.
func (d Docker) DataRootBytes(ctx context.Context) (DiskUsage, error) {
	root, err := d.Run(ctx, "docker", "info", "--format", "{{.DockerRootDir}}")
	if err != nil {
		return DiskUsage{}, err
	}
	out, err := d.Run(ctx, "df", "-B1", "--output=pcent,used,size", strings.TrimSpace(string(root)))
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
	return DiskUsage{Pct: pct, Used: used, Total: total}, nil
}
```

- [ ] **Step 4: Implement the runner side**

In `internal/runner/manager.go`, replace the field `diskPct        int` with `disk           system.DiskUsage` (keep the struct's alignment as `gofmt` leaves it). In `Status()`, replace `DiskPct: m.diskPct,` with:

```go
		DiskPct: m.disk.Pct, DiskUsedBytes: m.disk.Used, DiskTotalBytes: m.disk.Total,
```

In `internal/runner/cleanup.go`:

Replace the start of `checkDisk`:

```go
	pct, err := m.Docker.DataRootUsage(ctx)
	if err != nil {
		m.Events.Add("warn", "", "disk usage: %v", err)
		return
	}
	m.setDisk(pct)
```

with:

```go
	u, err := m.dataRoot(ctx)
	if err != nil {
		m.Events.Add("warn", "", "disk usage: %v", err)
		return
	}
	m.setDisk(u)
	pct := u.Pct
```

Replace the post-prune measurement:

```go
	if after, err := m.Docker.DataRootUsage(ctx); err != nil {
		m.recordStep("disk usage", "", err)
		notes = append(notes, fmt.Sprintf("disk usage after pruning failed: %v", err))
	} else {
		m.setDisk(after)
		nowPct = strconv.Itoa(after) + "%"
	}
```

with:

```go
	if after, err := m.dataRoot(ctx); err != nil {
		m.recordStep("disk usage", "", err)
		notes = append(notes, fmt.Sprintf("disk usage after pruning failed: %v", err))
	} else {
		m.setDisk(after)
		nowPct = strconv.Itoa(after.Pct) + "%"
	}
```

Replace the maintenance measurement:

```go
		if pct, err := m.Docker.DataRootUsage(ctx); err != nil {
			failed = true
			m.recordStep("disk usage", "", err)
			m.Events.Add("warn", "", "prune: disk usage: %v", err)
		} else {
			m.setDisk(pct)
			m.Events.Add("info", "", "prune: disk %d%% used", pct)
		}
```

with:

```go
		if u, err := m.dataRoot(ctx); err != nil {
			failed = true
			m.recordStep("disk usage", "", err)
			m.Events.Add("warn", "", "prune: disk usage: %v", err)
		} else {
			m.setDisk(u)
			m.Events.Add("info", "", "prune: disk %d%% used", u.Pct)
		}
```

Replace `setDisk`:

```go
func (m *Manager) setDisk(pct int) {
	m.mu.Lock()
	m.diskPct = pct
	m.mu.Unlock()
}
```

with:

```go
func (m *Manager) setDisk(u system.DiskUsage) {
	m.mu.Lock()
	m.disk = u
	m.mu.Unlock()
}

// diskBytes is a Docker that can also report the data root in bytes. The
// check is optional so the Docker interface and its fakes stay as they are.
type diskBytes interface {
	DataRootBytes(ctx context.Context) (system.DiskUsage, error)
}

// dataRoot measures the data-root filesystem, with bytes when the Docker
// can report them.
func (m *Manager) dataRoot(ctx context.Context) (system.DiskUsage, error) {
	if d, ok := m.Docker.(diskBytes); ok {
		return d.DataRootBytes(ctx)
	}
	pct, err := m.Docker.DataRootUsage(ctx)
	return system.DiskUsage{Pct: pct}, err
}
```

Leave the mid-prune `m.Docker.DataRootUsage(ctx)` check (the one compared with `cfg.DiskHighWater` before `PruneBuildCacheTo`) as it is: it only needs the percentage and stores nothing.

In `internal/model/model.go`, add to `Status` after `DiskPct        int               `json:"disk_pct"``:

```go
	// DiskUsedBytes and DiskTotalBytes are 0 until the first measurement.
	DiskUsedBytes  int64 `json:"disk_used_bytes"`
	DiskTotalBytes int64 `json:"disk_total_bytes"`
```

In `web/fixtures_test.go`, in the `"status"` value, change `RateRemaining: 4980, DiskPct: 61,` to:

```go
			RateRemaining: 4980, DiskPct: 61, DiskUsedBytes: 146_000_000_000, DiskTotalBytes: 240_000_000_000,
```

- [ ] **Step 5: Regenerate fixtures and run the tests**

Run: `GHR_UPDATE_FIXTURES=1 go test ./web/ && go test ./internal/system/ ./internal/runner/ ./internal/daemon/ ./web/`
Expected: PASS. `git diff --stat web/src/api/fixtures` shows `status.json` and `status-degraded.json` changed (the new byte fields have no `omitempty`) and nothing else.

- [ ] **Step 6: Manual check on the runner LXC (record the output in the commit body)**

Run: `ssh -i ~/.ssh/ghr_lxc root@192.168.0.99 'df -B1 --output=pcent,used,size "$(docker info --format "{{.DockerRootDir}}")"'`
Expected: a header line and one line of three fields, for example ` 61% 146000000000 240000000000`. If the format differs, stop and report it.

- [ ] **Step 7: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/system/system.go internal/system/system_test.go internal/runner/manager.go internal/runner/cleanup.go internal/runner/cleanup_test.go internal/model/model.go web/fixtures_test.go web/src/api/fixtures
git commit -m "feat(status): report disk used and total bytes"
```

### Task 4: Status: rate limit and oldest waiting job

**Files:**
- Modify: `internal/model/model.go` (`Status`, `RepoStatus`)
- Modify: `internal/daemon/backend.go` (`Status`)
- Modify: `internal/runner/manager.go` (`Status`)
- Modify: `web/fixtures_test.go` (status values) and regenerate fixtures
- Test: `internal/daemon/backend_test.go`, `internal/runner/manager_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `Status.RateLimit`, `RepoStatus.OldestQueuedAt` (Contracts).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Append to `internal/daemon/backend_test.go`:

```go
func TestStatusCarriesTheRateLimit(t *testing.T) {
	b, _, gh := newBackend(t)
	if st := b.Status(); st.RateLimit != 0 {
		t.Fatalf("before any response: %d", st.RateLimit)
	}
	n := 15000
	gh.meta = github.TokenMeta{RateLimit: &n}
	if st := b.Status(); st.RateLimit != 15000 {
		t.Fatalf("rate limit = %d", st.RateLimit)
	}
}
```

Append to `internal/runner/manager_test.go` (add `"github.com/darkraise/ghr/internal/sched"` to its imports if missing):

```go
func TestStatusCarriesTheOldestQueuedJob(t *testing.T) {
	h := newHarness(t)
	early := h.now.Add(-5 * time.Minute)
	h.m.mu.Lock()
	h.m.demand = map[string][]sched.QueuedJob{"darkmem": {
		{Repo: "darkmem", ID: 1, CreatedAt: h.now.Add(-time.Minute)},
		{Repo: "darkmem", ID: 2, CreatedAt: early},
	}}
	h.m.mu.Unlock()
	for _, r := range h.m.Status().Repos {
		switch r.Name {
		case "darkmem":
			if r.OldestQueuedAt == nil || !r.OldestQueuedAt.Equal(early) {
				t.Fatalf("darkmem oldest = %v", r.OldestQueuedAt)
			}
		case "darkcloud":
			if r.OldestQueuedAt != nil {
				t.Fatalf("darkcloud oldest = %v", r.OldestQueuedAt)
			}
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/daemon/ ./internal/runner/ -run 'TestStatusCarriesTheRateLimit|TestStatusCarriesTheOldestQueuedJob'`
Expected: FAIL to compile: `st.RateLimit undefined`, `r.OldestQueuedAt undefined`.

- [ ] **Step 3: Implement**

In `internal/model/model.go`, add to `Status` after `RateRemaining  int               `json:"rate_remaining"``:

```go
	// RateLimit is the token's hourly request limit, absent until GitHub has answered.
	RateLimit int `json:"rate_limit,omitempty"`
```

and to `RepoStatus` after `Queued   int           `json:"queued"``:

```go
	// OldestQueuedAt is when the repo's longest-waiting matching job was queued,
	// as of the scheduler's last successful poll.
	OldestQueuedAt *time.Time `json:"oldest_queued_at,omitempty"`
```

In `internal/daemon/backend.go`, in `Status()`, before `return st`, add:

```go
	if b.GH != nil {
		if l := b.GH.TokenMeta().RateLimit; l != nil {
			st.RateLimit = *l
		}
	}
```

In `internal/runner/manager.go`, in `Status()`, directly after the `rs := model.RepoStatus{...}` statement, add:

```go
		for _, q := range m.demand[r.Name] {
			if rs.OldestQueuedAt == nil || q.CreatedAt.Before(*rs.OldestQueuedAt) {
				at := q.CreatedAt
				rs.OldestQueuedAt = &at
			}
		}
```

In `web/fixtures_test.go`, in the `"status"` value, add `RateLimit: 5000,` after `RateRemaining: 4980,`, and in the `darkmem` repo entry add `OldestQueuedAt: ptr(at(-3 * time.Minute)),` after `Queued: 3,`.

- [ ] **Step 4: Regenerate fixtures and run the tests**

Run: `GHR_UPDATE_FIXTURES=1 go test ./web/ && go test ./internal/daemon/ ./internal/runner/ ./web/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/model/model.go internal/daemon/backend.go internal/daemon/backend_test.go internal/runner/manager.go internal/runner/manager_test.go web/fixtures_test.go web/src/api/fixtures
git commit -m "feat(status): add rate limit and oldest waiting job"
```

### Task 5: Activity: model types and lanes

**Files:**
- Modify: `internal/model/model.go` (append the activity types)
- Create: `internal/activity/activity.go`
- Test: `internal/activity/activity_test.go`

**Interfaces:**
- Consumes: `model.MetricSample`, `model.MetricRollup` (Task 1).
- Produces: the `model.Activity*` types, `activity.Valid`, `activity.Input`, `activity.Build` for the lanes windows (Contracts).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `internal/activity/activity_test.go`:

```go
package activity

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/darkraise/ghr/internal/model"
)

var now = time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)

func at(h, m int) time.Time { return time.Date(2026, 10, 3, h, m, 0, 0, time.UTC) }

func ptr[T any](v T) *T { return &v }

func job(id string, start, end time.Time, conclusion string) model.HistoryEntry {
	return model.HistoryEntry{ID: id, Repo: "darkmem", RunNumber: "41", Workflow: "ci", JobName: "build",
		Conclusion: conclusion, StartedAt: start, FinishedAt: end}
}

func laneIDs(a model.Activity) [][]string {
	out := [][]string{}
	for _, l := range a.Lanes {
		ids := []string{}
		for _, r := range l.Runs {
			ids = append(ids, r.InstanceID)
		}
		out = append(out, ids)
	}
	return out
}

func TestValid(t *testing.T) {
	for _, w := range []string{"1h", "3h", "24h", "7d", "30d"} {
		if !Valid(w) {
			t.Errorf("%s rejected", w)
		}
	}
	for _, w := range []string{"", "2h", "1d", "1H"} {
		if Valid(w) {
			t.Errorf("%q accepted", w)
		}
	}
}

func TestLanesPackRunsIntoFreeLanes(t *testing.T) {
	a := Build(Input{Window: "1h", Now: now, Capacity: ptr(4), History: []model.HistoryEntry{
		job("a", at(13, 10), at(13, 20), "success"),
		job("b", at(13, 15), at(13, 30), "success"),
		job("c", at(13, 20), at(13, 25), "failure"), // starts as a ends: reuses its lane
	}})
	got := laneIDs(a)
	want := [][]string{{"a", "c"}, {"b"}, {}, {}}
	if len(got) != len(want) || strings.Join(got[0], ",") != "a,c" || strings.Join(got[1], ",") != "b" || len(got[2]) != 0 || len(got[3]) != 0 {
		t.Fatalf("lanes %v, want %v", got, want)
	}
	if !a.From.Equal(at(13, 5)) || !a.To.Equal(now) || len(a.Buckets) != 0 {
		t.Fatalf("range %v to %v, buckets %d", a.From, a.To, len(a.Buckets))
	}
}

func TestLanesGrowPastCapacityAndTieByInstanceID(t *testing.T) {
	a := Build(Input{Window: "1h", Now: now, Capacity: ptr(1), History: []model.HistoryEntry{
		job("b", at(13, 30), at(13, 40), "success"),
		job("a", at(13, 30), at(13, 35), "success"),
	}})
	if got := laneIDs(a); len(got) != 2 || got[0][0] != "a" || got[1][0] != "b" {
		t.Fatalf("lanes %v", got)
	}
	none := Build(Input{Window: "1h", Now: now})
	if len(none.Lanes) != 0 || none.Capacity != nil {
		t.Fatalf("all mode with no runs: %+v", none.Lanes)
	}
}

func TestRunsOutsideTheWindowAreLeftOut(t *testing.T) {
	a := Build(Input{Window: "1h", Now: now, History: []model.HistoryEntry{job("old", at(12, 0), at(12, 30), "success")}})
	if len(a.Lanes) != 0 {
		t.Fatalf("lanes %v", laneIDs(a))
	}
	b := Build(Input{Window: "3h", Now: now, History: []model.HistoryEntry{job("old", at(12, 0), at(12, 30), "success")}})
	if len(b.Lanes) != 1 {
		t.Fatalf("3h lanes %v", laneIDs(b))
	}
}

func TestLiveInstances(t *testing.T) {
	a := Build(Input{Window: "1h", Now: now,
		History: []model.HistoryEntry{job("done", at(13, 10), at(13, 20), "success")},
		Instances: []model.InstanceStatus{
			{ID: "s", Repo: "ghr", State: "starting", Since: at(14, 4)},
			{ID: "w", Repo: "darkmem", State: "idle", Since: at(14, 0)},
			{ID: "r", Repo: "darkmem", State: "busy", Since: at(13, 59), Job: &model.JobInfo{
				RunNumber: "42", Workflow: "ci", Name: "test", HTMLURL: "https://example/42", StartedAt: at(14, 1)}},
			{ID: "x", Repo: "darkmem", State: "cleaning", Since: at(14, 3)},
			{ID: "done", Repo: "darkmem", State: "cleaning", Since: at(13, 20)},
		}})
	runs := map[string]model.ActivityRun{}
	for _, l := range a.Lanes {
		for _, r := range l.Runs {
			runs[r.InstanceID] = r
		}
	}
	if len(runs) != 4 {
		t.Fatalf("runs %v", laneIDs(a))
	}
	check := func(id, state string, from time.Time) {
		t.Helper()
		sg := runs[id].Segments
		if len(sg) != 1 || sg[0].State != state || !sg[0].From.Equal(from) || sg[0].To != nil {
			t.Fatalf("%s segments %+v", id, sg)
		}
	}
	check("s", "starting", at(14, 4))
	check("w", "warm", at(14, 0))
	check("r", "running", at(14, 1))
	if r := runs["r"]; r.Job != "test" || r.RunNumber != "42" || r.Workflow != "ci" || r.HTMLURL != "https://example/42" {
		t.Fatalf("running run %+v", r)
	}
	if sg := runs["done"].Segments; len(sg) != 1 || sg[0].State != "succeeded" || sg[0].To == nil || !sg[0].To.Equal(at(13, 20)) {
		t.Fatalf("finished run %+v", sg)
	}
}

func TestConclusionStatesAndAbsentURL(t *testing.T) {
	for conclusion, state := range map[string]string{
		"success": "succeeded", "failure": "failed", "timed_out": "failed", "cancelled": "cancelled", "skipped": "skipped",
	} {
		a := Build(Input{Window: "1h", Now: now, History: []model.HistoryEntry{job("j", at(13, 30), at(13, 40), conclusion)}})
		if got := a.Lanes[0].Runs[0].Segments[0].State; got != state {
			t.Errorf("%s: state %s, want %s", conclusion, got, state)
		}
		data, _ := json.Marshal(a.Lanes[0].Runs[0])
		if strings.Contains(string(data), "html_url") {
			t.Errorf("absent html_url marshalled: %s", data)
		}
	}
}

func TestMinutesInTheWindow(t *testing.T) {
	a := Build(Input{Window: "1h", Now: now, Minutes: []model.MetricSample{
		{At: at(12, 50), Queued: 9},
		{At: at(13, 10), Queued: 1, CPU: ptr(20.0), Mem: ptr(int64(1 << 30))},
		{At: at(14, 0), Queued: 3},
	}})
	if len(a.Waiting) != 2 || a.Waiting[0].Value != 1 || a.Waiting[1].Value != 3 {
		t.Fatalf("waiting %+v", a.Waiting)
	}
	if len(a.CPU) != 2 || a.CPU[0].CPU == nil || *a.CPU[0].CPU != 20 || a.CPU[1].CPU != nil {
		t.Fatalf("cpu %+v", a.CPU)
	}
}

func TestZoneAndHistoryFrom(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	a := Build(Input{Window: "1h", Now: now, Loc: loc, Retention: 30 * 24 * time.Hour,
		History: []model.HistoryEntry{job("j", at(13, 30), at(13, 40), "success")}})
	if a.TZ != "Asia/Ho_Chi_Minh" || a.To.Location() != loc || a.From.Location() != loc {
		t.Fatalf("zone %s %v %v", a.TZ, a.To.Location(), a.From.Location())
	}
	if !a.HistoryFrom.Equal(now.Add(-30*24*time.Hour)) || a.HistoryFrom.Location() != loc {
		t.Fatalf("history from %v", a.HistoryFrom)
	}
	if a.Lanes[0].Runs[0].Segments[0].From.Location() != loc {
		t.Fatal("segment times are not in the zone")
	}
	data, _ := json.Marshal(a)
	if !strings.Contains(string(data), `"to":"2026-10-03T21:05:00+07:00"`) {
		t.Fatalf("json %s", data)
	}
	if b := Build(Input{Window: "1h", Now: now}); b.TZ != "UTC" {
		t.Fatalf("nil zone: %s", b.TZ)
	}
}

func TestEmptySlicesMarshalAsArrays(t *testing.T) {
	data, _ := json.Marshal(Build(Input{Window: "1h", Now: now}))
	for _, k := range []string{`"lanes":[]`, `"buckets":[]`, `"waiting":[]`, `"cpu":[]`, `"repos":[]`} {
		if !strings.Contains(string(data), k) {
			t.Errorf("missing %s in %s", k, data)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/activity/`
Expected: FAIL to compile: `undefined: Build`, `undefined: Valid`, `undefined: Input`.

- [ ] **Step 3: Add the model types**

Append to `internal/model/model.go`:

```go
// Activity is GET /activity: what the runners did over a window, as lanes of
// runs (1h, 3h) or time buckets (24h, 7d, 30d), with every time in TZ.
type Activity struct {
	Window   string `json:"window"`
	TZ       string `json:"tz"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	Capacity *int      `json:"capacity"` // global_max in queue mode, null in all mode
	// HistoryFrom is the oldest finish time history_retention keeps.
	HistoryFrom time.Time        `json:"history_from"`
	Lanes       []ActivityLane   `json:"lanes"`
	Buckets     []ActivityBucket `json:"buckets"`
	Waiting     []ActivityPoint  `json:"waiting"`
	CPU         []ActivityCPU    `json:"cpu"`
	Repos       []ActivityRepo   `json:"repos"`
}

// ActivityLane holds runs one after another; it is not a fixed runner slot.
type ActivityLane struct {
	Runs []ActivityRun `json:"runs"`
}

// ActivityRun is one runner instance's time in the window.
type ActivityRun struct {
	InstanceID string            `json:"instance_id"`
	Repo       string            `json:"repo"`
	Workflow   string            `json:"workflow,omitempty"`
	Job        string            `json:"job,omitempty"`
	RunNumber  string            `json:"run_number,omitempty"`
	Segments   []ActivitySegment `json:"segments"`
	HTMLURL    string            `json:"html_url,omitempty"`
}

// ActivitySegment is a stretch of one state; To is null while it lasts.
type ActivitySegment struct {
	State string     `json:"state"` // starting | warm | running | succeeded | failed | cancelled | skipped
	From  time.Time  `json:"from"`
	To    *time.Time `json:"to"`
}

type ActivityBucket struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	BusyMinutes float64   `json:"busy_minutes"`
	BusyPct     *float64  `json:"busy_pct"`
	Succeeded   int       `json:"succeeded"`
	Failed      int       `json:"failed"`
	Cancelled   int       `json:"cancelled"`
	WaitingMax  *int      `json:"waiting_max"`
	CPUAvg      *float64  `json:"cpu_avg"`
}

type ActivityPoint struct {
	At    time.Time `json:"at"`
	Value int       `json:"value"`
}

type ActivityCPU struct {
	At  time.Time `json:"at"`
	CPU *float64  `json:"cpu"`
	Mem *int64    `json:"mem"`
}

type ActivityRepo struct {
	Repo  string         `json:"repo"`
	Hours []ActivityHour `json:"hours"`
}

type ActivityHour struct {
	Start     time.Time `json:"start"`
	Succeeded int       `json:"succeeded"`
	Failed    int       `json:"failed"`
	Cancelled int       `json:"cancelled"`
}
```

Run `gofmt -w internal/model/model.go` afterwards; it realigns the struct tags.

- [ ] **Step 4: Implement the package**

Create `internal/activity/activity.go`:

```go
// Package activity turns job history, live runners and metrics into the
// Dashboard's activity view: lanes of runs for short windows and time
// buckets for long ones.
package activity

import (
	"sort"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

type spec struct {
	lanes  bool
	span   time.Duration // lanes windows
	bucket string        // "hour", "6h" or "day"
	closed int           // closed buckets before the open one
}

var windows = map[string]spec{
	"1h":  {lanes: true, span: time.Hour},
	"3h":  {lanes: true, span: 3 * time.Hour},
	"24h": {bucket: "hour", closed: 24},
	"7d":  {bucket: "6h", closed: 28},
	"30d": {bucket: "day", closed: 30},
}

// Valid reports whether window is one Build accepts.
func Valid(window string) bool {
	_, ok := windows[window]
	return ok
}

type Input struct {
	Window    string
	Loc       *time.Location // nil means UTC
	Now       time.Time
	Capacity  *int // global_max in queue mode, nil in all mode
	Retention time.Duration
	History   []model.HistoryEntry
	Instances []model.InstanceStatus
	Repos     []string // configured repository names
	Minutes   []model.MetricSample
	Hours     []model.MetricRollup
}

// Build assembles the activity view for in.Window, which must be Valid.
func Build(in Input) model.Activity {
	loc := in.Loc
	if loc == nil {
		loc = time.UTC
	}
	sp := windows[in.Window]
	now := in.Now
	a := model.Activity{
		Window: in.Window, TZ: loc.String(), To: now.In(loc), Capacity: in.Capacity,
		Lanes: []model.ActivityLane{}, Buckets: []model.ActivityBucket{},
		Waiting: []model.ActivityPoint{}, CPU: []model.ActivityCPU{}, Repos: []model.ActivityRepo{},
	}
	a.HistoryFrom = now.Add(-in.Retention).In(loc)
	rs := runs(in, loc)
	if sp.lanes {
		from := now.Add(-sp.span)
		a.From = from.In(loc)
		a.Lanes = pack(rs, from, now, in.Capacity)
		for _, m := range in.Minutes {
			if m.At.Before(from) || m.At.After(now) {
				continue
			}
			a.Waiting = append(a.Waiting, model.ActivityPoint{At: m.At.In(loc), Value: m.Queued})
			a.CPU = append(a.CPU, model.ActivityCPU{At: m.At.In(loc), CPU: m.CPU, Mem: m.Mem})
		}
	}
	return a
}

// finishedState maps a job conclusion to a segment state; anything that is
// not a success, a cancellation or a skip counts as a failure.
func finishedState(conclusion string) string {
	switch conclusion {
	case "success":
		return "succeeded"
	case "cancelled":
		return "cancelled"
	case "skipped":
		return "skipped"
	default:
		return "failed"
	}
}

// runs builds one run per runner instance: a finished job from history, or
// a live instance in its current state. A cleaning instance is left out
// until its history entry lands; a live instance that already has one is
// shown from history.
func runs(in Input, loc *time.Location) []model.ActivityRun {
	seen := map[string]bool{}
	out := []model.ActivityRun{}
	for _, e := range in.History {
		seen[e.ID] = true
		to := e.FinishedAt.In(loc)
		out = append(out, model.ActivityRun{
			InstanceID: e.ID, Repo: e.Repo, Workflow: e.Workflow, Job: e.JobName, RunNumber: e.RunNumber, HTMLURL: e.HTMLURL,
			Segments: []model.ActivitySegment{{State: finishedState(e.Conclusion), From: e.StartedAt.In(loc), To: &to}},
		})
	}
	for _, i := range in.Instances {
		if seen[i.ID] {
			continue
		}
		r := model.ActivityRun{InstanceID: i.ID, Repo: i.Repo}
		switch i.State {
		case "starting":
			r.Segments = []model.ActivitySegment{{State: "starting", From: i.Since.In(loc)}}
		case "idle":
			r.Segments = []model.ActivitySegment{{State: "warm", From: i.Since.In(loc)}}
		case "busy":
			from := i.Since
			if j := i.Job; j != nil {
				r.Workflow, r.Job, r.RunNumber, r.HTMLURL = j.Workflow, j.Name, j.RunNumber, j.HTMLURL
				if !j.StartedAt.IsZero() {
					from = j.StartedAt
				}
			}
			r.Segments = []model.ActivitySegment{{State: "running", From: from.In(loc)}}
		default:
			continue
		}
		out = append(out, r)
	}
	return out
}

// span is a run's first start and its last end, now while it lasts.
func span(r model.ActivityRun, now time.Time) (time.Time, time.Time) {
	last := r.Segments[len(r.Segments)-1]
	end := now
	if last.To != nil {
		end = *last.To
	}
	return r.Segments[0].From, end
}

// pack places the runs overlapping [from, to) into lanes: sorted by start,
// then instance ID, each into the first lane whose last run ended by its
// start. There are at least capacity lanes when capacity is set.
func pack(rs []model.ActivityRun, from, to time.Time, capacity *int) []model.ActivityLane {
	var in []model.ActivityRun
	for _, r := range rs {
		s, e := span(r, to)
		if e.After(from) && s.Before(to) {
			in = append(in, r)
		}
	}
	sort.SliceStable(in, func(a, b int) bool {
		sa, _ := span(in[a], to)
		sb, _ := span(in[b], to)
		if !sa.Equal(sb) {
			return sa.Before(sb)
		}
		return in[a].InstanceID < in[b].InstanceID
	})
	lanes := []model.ActivityLane{}
	var ends []time.Time
	for _, r := range in {
		s, e := span(r, to)
		placed := false
		for i := range lanes {
			if !ends[i].After(s) {
				lanes[i].Runs = append(lanes[i].Runs, r)
				ends[i] = e
				placed = true
				break
			}
		}
		if !placed {
			lanes = append(lanes, model.ActivityLane{Runs: []model.ActivityRun{r}})
			ends = append(ends, e)
		}
	}
	if capacity != nil {
		for len(lanes) < *capacity {
			lanes = append(lanes, model.ActivityLane{Runs: []model.ActivityRun{}})
		}
	}
	return lanes
}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/activity/ ./internal/model/ ./web/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/model/model.go internal/activity/activity.go internal/activity/activity_test.go
git commit -m "feat(activity): pack runner runs into lanes"
```

### Task 6: Activity: buckets and repository hours

**Files:**
- Create: `internal/activity/buckets.go`
- Modify: `internal/activity/activity.go` (`Build`)
- Test: `internal/activity/buckets_test.go`

**Interfaces:**
- Consumes: `Input`, `runs`, `finishedState`, `spec`, `windows` from Task 5; `model.MetricRollup` from Task 1.
- Produces: bucket and `repos` output of `activity.Build` (Contracts).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

Create `internal/activity/buckets_test.go`:

```go
package activity

import (
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

func bucketAt(t *testing.T, a model.Activity, start time.Time) model.ActivityBucket {
	t.Helper()
	for _, b := range a.Buckets {
		if b.Start.Equal(start) {
			return b
		}
	}
	t.Fatalf("no bucket at %v", start)
	return model.ActivityBucket{}
}

func TestBucketCountsAndAlignment(t *testing.T) {
	for _, c := range []struct {
		window string
		count  int
		first  time.Time
		size   time.Duration
	}{
		{"24h", 25, time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC), time.Hour},
		{"7d", 29, time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC), 6 * time.Hour},
		{"30d", 31, time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC), 24 * time.Hour},
	} {
		a := Build(Input{Window: c.window, Now: now})
		if len(a.Buckets) != c.count || !a.Buckets[0].Start.Equal(c.first) || !a.From.Equal(c.first) {
			t.Fatalf("%s: %d buckets from %v", c.window, len(a.Buckets), a.From)
		}
		last := a.Buckets[len(a.Buckets)-1]
		if !last.Start.Before(now) || !last.End.After(now) || last.End.Sub(last.Start) != c.size || len(a.Lanes) != 0 {
			t.Fatalf("%s: open bucket %v to %v", c.window, last.Start, last.End)
		}
	}
}

func TestBusyMinutesSplitAndOpenBucketDenominator(t *testing.T) {
	a := Build(Input{Window: "24h", Now: now, Capacity: ptr(2),
		History: []model.HistoryEntry{job("j", at(12, 50), at(13, 10), "success")},
		Instances: []model.InstanceStatus{{ID: "r", Repo: "darkmem", State: "busy", Since: at(14, 0),
			Job: &model.JobInfo{StartedAt: at(14, 0)}}},
	})
	b12, b13, b14 := bucketAt(t, a, at(12, 0)), bucketAt(t, a, at(13, 0)), bucketAt(t, a, at(14, 0))
	if b12.BusyMinutes != 10 || b13.BusyMinutes != 10 || b14.BusyMinutes != 5 {
		t.Fatalf("busy %v %v %v", b12.BusyMinutes, b13.BusyMinutes, b14.BusyMinutes)
	}
	if b12.BusyPct == nil || *b12.BusyPct != 8.3 || b14.BusyPct == nil || *b14.BusyPct != 50 {
		t.Fatalf("busy pct %v %v", b12.BusyPct, b14.BusyPct)
	}
	if b12.Succeeded != 0 || b13.Succeeded != 1 {
		t.Fatalf("counted by finish time: %d %d", b12.Succeeded, b13.Succeeded)
	}
	all := Build(Input{Window: "24h", Now: now, History: []model.HistoryEntry{job("j", at(12, 50), at(13, 10), "success")}})
	if bucketAt(t, all, at(13, 0)).BusyPct != nil {
		t.Fatal("busy pct without a capacity")
	}
}

func TestCountsByConclusion(t *testing.T) {
	a := Build(Input{Window: "24h", Now: now, History: []model.HistoryEntry{
		job("a", at(13, 0), at(13, 5), "success"),
		job("b", at(13, 0), at(13, 6), "failure"),
		job("c", at(13, 0), at(13, 7), "cancelled"),
		job("d", at(13, 0), at(13, 8), "skipped"),
	}})
	if b := bucketAt(t, a, at(13, 0)); b.Succeeded != 1 || b.Failed != 1 || b.Cancelled != 2 {
		t.Fatalf("counts %+v", b)
	}
}

func TestRollupsFillWaitingAndCPU(t *testing.T) {
	hours := []model.MetricRollup{
		{At: at(12, 0), Samples: 60, QueuedMax: 1, CPUAvg: ptr(40.0)},
		{At: at(13, 0), Samples: 60, QueuedMax: 3, CPUAvg: ptr(20.0)},
	}
	a := Build(Input{Window: "24h", Now: now, Hours: hours})
	if b := bucketAt(t, a, at(12, 0)); b.WaitingMax == nil || *b.WaitingMax != 1 || b.CPUAvg == nil || *b.CPUAvg != 40 {
		t.Fatalf("12:00 %+v", b)
	}
	if b := bucketAt(t, a, at(11, 0)); b.WaitingMax != nil || b.CPUAvg != nil {
		t.Fatalf("a bucket without rollups %+v", b)
	}
	w := Build(Input{Window: "7d", Now: now, Hours: hours})
	if b := bucketAt(t, w, at(12, 0)); b.WaitingMax == nil || *b.WaitingMax != 3 || b.CPUAvg == nil || *b.CPUAvg != 30 {
		t.Fatalf("6h bucket %+v", b)
	}
}

func TestDaylightSavingDays(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	fall := Build(Input{Window: "30d", Loc: ny, Now: time.Date(2026, 11, 10, 17, 0, 0, 0, time.UTC)})
	if b := bucketAt(t, fall, time.Date(2026, 11, 1, 0, 0, 0, 0, ny)); b.End.Sub(b.Start) != 25*time.Hour {
		t.Fatalf("Nov 1 lasts %v", b.End.Sub(b.Start))
	}
	spring := Build(Input{Window: "30d", Loc: ny, Now: time.Date(2026, 3, 20, 17, 0, 0, 0, time.UTC)})
	if b := bucketAt(t, spring, time.Date(2026, 3, 8, 0, 0, 0, 0, ny)); b.End.Sub(b.Start) != 23*time.Hour {
		t.Fatalf("Mar 8 lasts %v", b.End.Sub(b.Start))
	}
	week := Build(Input{Window: "7d", Loc: ny, Now: time.Date(2026, 11, 3, 17, 0, 0, 0, time.UTC)})
	if b := bucketAt(t, week, time.Date(2026, 11, 1, 0, 0, 0, 0, ny)); b.End.Sub(b.Start) != 7*time.Hour {
		t.Fatalf("Nov 1 00:00 to 06:00 lasts %v", b.End.Sub(b.Start))
	}
}

func TestZoneChangesAlignment(t *testing.T) {
	hcm, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	a := Build(Input{Window: "30d", Loc: hcm, Now: now})
	for _, b := range a.Buckets {
		if b.Start.Hour() != 0 || b.Start.Minute() != 0 || b.Start.Location() != hcm || b.Start.UTC().Hour() != 17 {
			t.Fatalf("bucket starts %v (%v UTC)", b.Start, b.Start.UTC())
		}
	}
}

func TestRepoHours(t *testing.T) {
	a := Build(Input{Window: "1h", Now: now, Repos: []string{"darkmem", "darkcloud"}, History: []model.HistoryEntry{
		job("a", at(13, 0), at(13, 10), "success"),
		job("b", at(13, 20), at(13, 40), "failure"),
		{ID: "c", Repo: "removed-repo", Conclusion: "success", StartedAt: at(13, 0), FinishedAt: at(13, 5)},
	}})
	if len(a.Repos) != 2 || a.Repos[0].Repo != "darkmem" || a.Repos[1].Repo != "darkcloud" {
		t.Fatalf("repos %+v", a.Repos)
	}
	hrs := a.Repos[0].Hours
	if len(hrs) != 24 || !hrs[23].Start.Equal(at(14, 0)) || !hrs[0].Start.Equal(time.Date(2026, 10, 2, 15, 0, 0, 0, time.UTC)) {
		t.Fatalf("hours from %v to %v", hrs[0].Start, hrs[len(hrs)-1].Start)
	}
	if hrs[22].Succeeded != 1 || hrs[22].Failed != 1 || a.Repos[1].Hours[22].Succeeded != 0 {
		t.Fatalf("13:00 %+v", hrs[22])
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/activity/ -run 'TestBucket|TestBusy|TestCounts|TestRollups|TestDaylight|TestZoneChanges|TestRepoHours'`
Expected: FAIL: bucket windows return no buckets (`0 buckets`) and `repos` is empty.

- [ ] **Step 3: Implement**

Create `internal/activity/buckets.go`:

```go
package activity

import (
	"math"
	"strings"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

// floor returns the start of the bucket holding t, in loc.
func floor(t time.Time, kind string, loc *time.Location) time.Time {
	t = t.In(loc)
	y, mo, d := t.Date()
	switch kind {
	case "hour":
		// Subtracting the local minutes keeps half-hour zones and repeated
		// DST hours on their own local hour boundaries.
		return t.Add(-time.Duration(t.Minute())*time.Minute - time.Duration(t.Second())*time.Second - time.Duration(t.Nanosecond()))
	case "6h":
		return time.Date(y, mo, d, t.Hour()/6*6, 0, 0, 0, loc)
	default:
		return time.Date(y, mo, d, 0, 0, 0, 0, loc)
	}
}

// next returns the start of the bucket after the one starting at start.
// Six-hour and day buckets follow local wall time, so DST days run 23 or 25 hours.
func next(start time.Time, kind string, loc *time.Location) time.Time {
	start = start.In(loc)
	y, mo, d := start.Date()
	switch kind {
	case "hour":
		return start.Add(time.Hour)
	case "6h":
		return time.Date(y, mo, d, start.Hour()+6, 0, 0, 0, loc)
	default:
		return time.Date(y, mo, d+1, 0, 0, 0, 0, loc)
	}
}

// starts lists the start of n closed buckets followed by the open one holding now.
func starts(now time.Time, kind string, loc *time.Location, n int) []time.Time {
	out := []time.Time{floor(now, kind, loc)}
	for len(out) <= n {
		out = append([]time.Time{floor(out[0].Add(-time.Nanosecond), kind, loc)}, out...)
	}
	return out
}

func overlap(a0, a1, b0, b1 time.Time) time.Duration {
	s, e := a0, a1
	if b0.After(s) {
		s = b0
	}
	if b1.Before(e) {
		e = b1
	}
	if !e.After(s) {
		return 0
	}
	return e.Sub(s)
}

func tally(conclusion string, succeeded, failed, cancelled *int) {
	switch finishedState(conclusion) {
	case "succeeded":
		*succeeded++
	case "failed":
		*failed++
	default:
		*cancelled++
	}
}

// buckets builds sp.closed closed buckets and the open one. Busy time counts
// running and finished segments; the open bucket's percentage divides by the
// minutes elapsed in it. Runs are counted by finish time, and waiting and CPU
// come from the hourly rollups whose UTC hour starts inside the bucket.
func buckets(in Input, sp spec, rs []model.ActivityRun, loc *time.Location) (time.Time, []model.ActivityBucket) {
	now := in.Now
	ss := starts(now, sp.bucket, loc, sp.closed)
	out := make([]model.ActivityBucket, len(ss))
	for i, s := range ss {
		e := next(s, sp.bucket, loc)
		upto := e
		if now.Before(upto) {
			upto = now
		}
		b := model.ActivityBucket{Start: s.In(loc), End: e.In(loc)}
		busy := 0.0
		for _, r := range rs {
			for _, sg := range r.Segments {
				if sg.State == "starting" || sg.State == "warm" {
					continue
				}
				end := now
				if sg.To != nil {
					end = *sg.To
				}
				busy += overlap(sg.From, end, s, upto).Minutes()
			}
		}
		b.BusyMinutes = math.Round(busy*10) / 10
		if in.Capacity != nil && *in.Capacity > 0 && upto.After(s) {
			pct := math.Round(busy/(float64(*in.Capacity)*upto.Sub(s).Minutes())*1000) / 10
			b.BusyPct = &pct
		}
		for _, h := range in.History {
			if !h.FinishedAt.Before(s) && h.FinishedAt.Before(e) {
				tally(h.Conclusion, &b.Succeeded, &b.Failed, &b.Cancelled)
			}
		}
		cpuSum, cpuN := 0.0, 0
		for _, r := range in.Hours {
			if r.At.Before(s) || !r.At.Before(e) {
				continue
			}
			if b.WaitingMax == nil || r.QueuedMax > *b.WaitingMax {
				w := r.QueuedMax
				b.WaitingMax = &w
			}
			if r.CPUAvg != nil {
				cpuSum += *r.CPUAvg * float64(r.Samples)
				cpuN += r.Samples
			}
		}
		if cpuN > 0 {
			v := math.Round(cpuSum/float64(cpuN)*10) / 10
			b.CPUAvg = &v
		}
		out[i] = b
	}
	return ss[0], out
}

// repoHours counts each configured repository's runs by finish time over
// the last 24 local hours, oldest first, the last one in progress.
func repoHours(in Input, loc *time.Location) []model.ActivityRepo {
	ss := starts(in.Now, "hour", loc, 23)
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
		out = append(out, r)
	}
	return out
}
```

In `internal/activity/activity.go`, in `Build`, replace:

```go
	if sp.lanes {
```

through the closing brace of that `if` block, keeping its body, so that it ends with an `else` branch, and add the repos line after it. The tail of `Build` becomes:

```go
	if sp.lanes {
		from := now.Add(-sp.span)
		a.From = from.In(loc)
		a.Lanes = pack(rs, from, now, in.Capacity)
		for _, m := range in.Minutes {
			if m.At.Before(from) || m.At.After(now) {
				continue
			}
			a.Waiting = append(a.Waiting, model.ActivityPoint{At: m.At.In(loc), Value: m.Queued})
			a.CPU = append(a.CPU, model.ActivityCPU{At: m.At.In(loc), CPU: m.CPU, Mem: m.Mem})
		}
	} else {
		from, bs := buckets(in, sp, rs, loc)
		a.From, a.Buckets = from.In(loc), bs
	}
	a.Repos = repoHours(in, loc)
	return a
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/activity/`
Expected: PASS, including every Task 5 test (`TestEmptySlicesMarshalAsArrays` still finds `"repos":[]` because no repos were given).

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/activity/activity.go internal/activity/buckets.go internal/activity/buckets_test.go
git commit -m "feat(activity): build time buckets and repo hours"
```

### Task 7: Daemon: cached activity

**Files:**
- Modify: `internal/daemon/backend.go`
- Test: `internal/daemon/backend_test.go`

**Interfaces:**
- Consumes: `activity.Build`, `activity.Input` (Tasks 5 and 6); `Sampler.Minutes`, `Sampler.Hours` (Task 1).
- Produces: `Backend.Activity` (Contracts, `internal/daemon`).

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing tests**

Append to `internal/daemon/backend_test.go` (add `"github.com/darkraise/ghr/internal/model"` and `"time"` to its imports if missing):

```go
func TestActivityBuildsFromHistoryAndInstances(t *testing.T) {
	b, m, _ := newBackend(t)
	now := time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	if err := b.Hist.Append(model.HistoryEntry{ID: "a", Repo: "darkmem", RunID: 1, Conclusion: "success",
		StartedAt: now.Add(-50 * time.Minute), FinishedAt: now.Add(-40 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	m.insts = []model.InstanceStatus{{ID: "r", Repo: "darkmem", State: "busy", Since: now.Add(-time.Minute)}}
	a, err := b.Activity(context.Background(), "1h", time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if a.Capacity == nil || *a.Capacity != 2 || len(a.Lanes) != 2 || !a.HistoryFrom.Equal(now.Add(-30*24*time.Hour)) {
		t.Fatalf("activity %+v", a)
	}
	if len(a.Repos) != 2 || a.Repos[0].Repo != "darkcloud" || a.Repos[1].Repo != "darkmem" {
		t.Fatalf("repos %+v", a.Repos)
	}
}

func TestActivityCachesPerWindowAndZone(t *testing.T) {
	b, _, _ := newBackend(t)
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
	first, _ := b.Activity(ctx, "1h", time.UTC)
	b.Hist.Append(model.HistoryEntry{ID: "a", Repo: "darkmem", RunID: 1, Conclusion: "success",
		StartedAt: now.Add(-20 * time.Minute), FinishedAt: now.Add(-10 * time.Minute)})
	if again, _ := b.Activity(ctx, "1h", time.UTC); runs(again) != runs(first) {
		t.Fatal("a second call within 5 s rebuilt the response")
	}
	if other, _ := b.Activity(ctx, "3h", time.UTC); runs(other) != 1 {
		t.Fatal("another window shared the cache entry")
	}
	now = now.Add(6 * time.Second)
	if later, _ := b.Activity(ctx, "1h", time.UTC); runs(later) != 1 {
		t.Fatal("the cache outlived 5 s")
	}
}

func TestActivitySlowWindowDoesNotBlockAnother(t *testing.T) {
	b, _, _ := newBackend(t)
	held := b.activityEntry("30d|UTC")
	held.mu.Lock()
	defer held.mu.Unlock()
	done := make(chan struct{})
	go func() {
		b.Activity(context.Background(), "1h", time.UTC)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("a 1h build waited for a 30d build")
	}
}

func TestActivityCacheDropsExpiredEntries(t *testing.T) {
	b, _, _ := newBackend(t)
	now := time.Date(2026, 10, 3, 14, 5, 0, 0, time.UTC)
	b.Now = func() time.Time { return now }
	ctx := context.Background()
	b.Activity(ctx, "1h", time.UTC)
	now = now.Add(6 * time.Second)
	b.Activity(ctx, "3h", time.UTC)
	b.activityMu.Lock()
	_, kept := b.activity["1h|UTC"]
	n := len(b.activity)
	b.activityMu.Unlock()
	if kept || n != 1 {
		t.Fatalf("expired entry kept: %v, %d entries", kept, n)
	}
}

func TestActivityAllModeHasNoCapacity(t *testing.T) {
	b, _, _ := newBackend(t)
	if _, err := b.Store.Update(func(c *config.Config) error { c.Mode = config.ModeAll; return nil }); err != nil {
		t.Fatal(err)
	}
	a, err := b.Activity(context.Background(), "24h", time.UTC)
	if err != nil || a.Capacity != nil {
		t.Fatalf("capacity %v err %v", a.Capacity, err)
	}
}
```

`Store.Update` (`internal/daemon/store.go:102`) returns `([]string, error)`. Add `"github.com/darkraise/ghr/internal/config"` to the test imports if missing.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/daemon/ -run TestActivity`
Expected: FAIL to compile: `b.Activity undefined`, `b.activityEntry undefined`.

- [ ] **Step 3: Implement**

In `internal/daemon/backend.go`, add `"github.com/darkraise/ghr/internal/activity"` to the imports. Add two fields at the end of the `Backend` struct:

```go
	activityMu sync.Mutex
	activity   map[string]*activityEntry
```

Add below `Metrics()`:

```go
// activityTTL is how long a built activity response is reused, so several
// open Dashboards polling every 5 s read the history file once between them.
const activityTTL = 5 * time.Second

type activityEntry struct {
	mu  sync.Mutex
	at  time.Time
	ok  bool
	val model.Activity
}

// Activity builds the Dashboard's activity view for window in loc, reusing
// one built less than activityTTL ago. Each window and zone has its own
// lock, so a slow 30d build never holds up a 1h poll.
func (b *Backend) Activity(ctx context.Context, window string, loc *time.Location) (model.Activity, error) {
	e := b.activityEntry(window + "|" + loc.String())
	e.mu.Lock()
	defer e.mu.Unlock()
	now := b.now()
	if e.ok && now.Sub(e.at) < activityTTL {
		return e.val, nil
	}
	cfg := b.Store.Config()
	hist, err := b.Hist.Query("", "", 0)
	if err != nil {
		return model.Activity{}, err
	}
	in := activity.Input{
		Window: window, Loc: loc, Now: now, Retention: cfg.HistoryRetention.D(),
		History: hist, Instances: b.M.Status().Instances,
	}
	if cfg.Mode == config.ModeQueue {
		c := cfg.GlobalMax
		in.Capacity = &c
	}
	for _, r := range cfg.Repos {
		in.Repos = append(in.Repos, r.Name)
	}
	if b.Sampler != nil {
		in.Minutes, in.Hours = b.Sampler.Minutes(), b.Sampler.Hours()
	}
	e.val, e.at, e.ok = activity.Build(in), now, true
	return e.val, nil
}

// activityEntry returns key's cache entry, creating it if needed. Creating
// one also drops expired entries nobody holds, so a client cycling through
// zones does not keep 30-day responses alive.
func (b *Backend) activityEntry(key string) *activityEntry {
	b.activityMu.Lock()
	defer b.activityMu.Unlock()
	if b.activity == nil {
		b.activity = map[string]*activityEntry{}
	}
	e, ok := b.activity[key]
	if !ok {
		now := b.now()
		for k, o := range b.activity {
			if o.mu.TryLock() {
				if !o.ok || now.Sub(o.at) >= activityTTL {
					delete(b.activity, k)
				}
				o.mu.Unlock()
			}
		}
		e = &activityEntry{}
		b.activity[key] = e
	}
	return e
}
```

`config`, `sync`, `time` and `context` are already imported by `backend.go`; confirm with `go build ./internal/daemon/`.

- [ ] **Step 4: Run the tests, with the race detector where available**

Run: `go test ./internal/daemon/ -run TestActivity` and, on Linux or in CI, `go test -race ./internal/daemon/ -run TestActivity`
Expected: PASS. (`-race` needs cgo; on this Windows machine it runs in CI only.)

- [ ] **Step 5: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/daemon/backend.go internal/daemon/backend_test.go
git commit -m "feat(daemon): build and cache the activity view"
```

### Task 8: API route and fixtures

**Files:**
- Modify: `internal/api/server.go` (`Backend` interface, `NewServer`, imports)
- Modify: `web/fixtures_test.go` (activity fixtures)
- Create (generated): `web/src/api/fixtures/activity-lanes.json`, `web/src/api/fixtures/activity-buckets.json`
- Test: `internal/api/api_test.go`

**Interfaces:**
- Consumes: `activity.Valid`, `activity.Build`, `activity.Input` (Tasks 5, 6); `Backend.Activity` (Task 7).
- Produces: `GET /activity` and the two activity fixtures (Contracts, `internal/api` and Fixtures).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

In `internal/api/api_test.go`, add two fields to `fakeBackend`:

```go
	activityCalls []string
	activity      model.Activity
```

and the method:

```go
func (f *fakeBackend) Activity(_ context.Context, window string, loc *time.Location) (model.Activity, error) {
	f.activityCalls = append(f.activityCalls, window+"|"+loc.String())
	return f.activity, nil
}
```

Append the test:

```go
func TestActivityRoute(t *testing.T) {
	c, b := setup(t)
	ctx := context.Background()
	b.activity = model.Activity{Window: "1h", TZ: "Asia/Ho_Chi_Minh"}
	var got model.Activity
	if err := c.call(ctx, http.MethodGet, "/activity?window=1h&tz=Asia/Ho_Chi_Minh", nil, &got); err != nil || got.Window != "1h" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := c.call(ctx, http.MethodGet, "/activity?window=24h", nil, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.activityCalls, []string{"1h|Asia/Ho_Chi_Minh", "24h|Local"}) {
		t.Fatalf("backend saw %v", b.activityCalls)
	}
	for _, q := range []string{"window=2h", "window=1h&tz=Mars/Olympus_Mons", ""} {
		err := c.call(ctx, http.MethodGet, "/activity?"+q, nil, &got)
		var ae *Error
		if !errors.As(err, &ae) || ae.Status != http.StatusBadRequest {
			t.Fatalf("%q: %v", q, err)
		}
	}
	if len(b.activityCalls) != 2 {
		t.Fatalf("a bad request reached the backend: %v", b.activityCalls)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/api/ -run TestActivityRoute`
Expected: FAIL: the route answers 404 (`unknown route` / `not found`), so the first `c.call` returns an error.

- [ ] **Step 3: Implement the route**

In `internal/api/server.go`, add `"github.com/darkraise/ghr/internal/activity"` to the imports and, in its own import line with a comment, the zone database:

```go
	// time/tzdata embeds the zone database, so GET /activity can load the
	// browser's zone on a host without /usr/share/zoneinfo.
	_ "time/tzdata"
```

Add to the `Backend` interface, after `Metrics() model.Metrics`:

```go
	Activity(ctx context.Context, window string, loc *time.Location) (model.Activity, error)
```

Add to `NewServer`, after the `GET /metrics` route:

```go
	mux.HandleFunc("GET /activity", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		window := q.Get("window")
		if !activity.Valid(window) {
			respond(w, nil, BadRequest("window must be one of 1h, 3h, 24h, 7d, 30d"))
			return
		}
		loc := time.Local
		if tz := q.Get("tz"); tz != "" {
			l, err := time.LoadLocation(tz)
			if err != nil {
				respond(w, nil, BadRequest("unknown time zone "+tz))
				return
			}
			loc = l
		}
		a, err := b.Activity(r.Context(), window, loc)
		respond(w, a, err)
	})
```

- [ ] **Step 4: Add the fixtures**

In `web/fixtures_test.go`, add `"github.com/darkraise/ghr/internal/activity"` to the imports, and add two entries to the map `fixtureValues` returns:

```go
		"activity-lanes": activity.Build(activity.Input{
			Window: "1h", Now: fixtureTime, Capacity: ptr(2), Retention: 30 * 24 * time.Hour,
			Repos: []string{"darkmem", "darkcloud"},
			History: []model.HistoryEntry{
				{ID: "h1", Repo: "darkmem", RunID: 101, RunNumber: "41", Workflow: "ci", JobName: "build", Conclusion: "success",
					StartedAt: at(-30 * time.Minute), FinishedAt: at(-25 * time.Minute),
					HTMLURL: "https://github.com/darkraise/darkmem/actions/runs/101/job/1"},
				{ID: "h3", Repo: "darkmem", RunID: 99, RunNumber: "40", Workflow: "ci", JobName: "lint", Conclusion: "failure",
					StartedAt: at(-50 * time.Minute), FinishedAt: at(-44 * time.Minute)},
			},
			Instances: []model.InstanceStatus{
				{ID: "aaaaaa", Repo: "darkmem", RunnerName: "ghr-aaaaaa", State: "busy", Since: at(-5 * time.Minute),
					Job: &model.JobInfo{RunID: 102, RunNumber: "42", Workflow: "ci", Name: "test",
						HTMLURL: "https://github.com/darkraise/darkmem/actions/runs/102/job/2", StartedAt: at(-4 * time.Minute)}},
				{ID: "bbbbbb", Repo: "darkmem", RunnerName: "ghr-bbbbbb", State: "idle", Since: at(-2 * time.Minute)},
			},
			Minutes: []model.MetricSample{
				{At: at(-3 * time.Minute), Live: 1, Queued: 2, CPU: ptr(12.5), Mem: ptr(int64(2 << 30))},
				{At: at(-2 * time.Minute), Live: 2, Queued: 1, CPU: ptr(48.0), Mem: ptr(int64(3 << 30))},
				{At: at(-time.Minute), Live: 2, Queued: 3},
			},
		}),
		"activity-buckets": activity.Build(activity.Input{
			Window: "24h", Now: fixtureTime, Retention: 30 * 24 * time.Hour,
			Repos: []string{"darkmem"},
			History: []model.HistoryEntry{
				{ID: "h1", Repo: "darkmem", RunID: 101, RunNumber: "41", Workflow: "ci", JobName: "build", Conclusion: "success",
					StartedAt: at(-30 * time.Minute), FinishedAt: at(-25 * time.Minute)},
				{ID: "h2", Repo: "darkmem", RunID: 90, RunNumber: "39", Workflow: "ci", JobName: "build", Conclusion: "failure",
					StartedAt: at(-3 * time.Hour), FinishedAt: at(-170 * time.Minute)},
			},
			Hours: []model.MetricRollup{
				{At: time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC), Samples: 60, QueuedMax: 3, CPUAvg: ptr(22.5), MemAvg: ptr(int64(3 << 30))},
			},
		}),
```

The buckets fixture omits `Capacity` on purpose, so it shows the all-mode nulls.

- [ ] **Step 5: Generate the fixtures and run every Go test**

Run: `GHR_UPDATE_FIXTURES=1 go test ./web/ && go test ./...`
Expected: PASS. `web/src/api/fixtures/activity-lanes.json` holds two lanes: the first with runs `h3`, `h1` and `aaaaaa` (each starts after the previous ends), the second with `bbbbbb` (warm while `aaaaaa` runs). `activity-buckets.json` holds 25 buckets with `"capacity": null`.

- [ ] **Step 6: Run the web suite, which parses the fixtures it knows**

Run: `cd web && npm test -- --run && cd ..`
Expected: PASS (the new activity fixtures are not read by any web test until plan 2; the changed status fixture still parses).

- [ ] **Step 7: Commit**

```bash
gofmt -l . && go vet ./...
git add internal/api/server.go internal/api/api_test.go web/fixtures_test.go web/src/api/fixtures
git commit -m "feat(api): serve GET /activity"
```
