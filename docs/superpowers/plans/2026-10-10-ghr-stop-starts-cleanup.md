# ghr Stop Starts Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A runner stopped with `ghr kill` or the UI's Stop button moves to cleanup within a second of its unit going down, whether or not a manager tick is in flight (register row 33).

**Architecture:** `Manager.Kill` already runs on the API handler's goroutine. After its unit stop succeeds it asks systemd whether the unit is still active and, when systemd confirms it is not, starts the cleanup itself through the entry point the tick uses, instead of waiting for the woken tick. That entry point learns to refuse after `Close`, with its `WaitGroup` count taken under `m.mu`, and to leave an already cleaning instance alone when asked. The loop wake stays.

**Tech Stack:** Go 1.26 standard library.

**Spec:** docs/superpowers/specs/2026-10-10-ghr-runner-stopping-state-design.md

**Execution:** subagent — `claude --model sonnet --effort high` — both tasks are heavy (risk 3, concurrency).

## Global Constraints

- Execution environment: the owner's Linux machine (bash, GNU coreutils `timeout`, Go 1.26 at `/usr/local/go/bin`). If `go` is not on `PATH`, run `export PATH=$PATH:/usr/local/go/bin` first.
- Work on the branch `fix/stop-starts-cleanup`, which exists and is checked out. Never commit to `master` and never push: CI turns every push to `master` into a release. Do not commit `.superpowers/`.
- Go checks, from the repository root: `gofmt -l internal cmd web/*.go` prints nothing; `go vet ./internal/... ./cmd/... ./web/` passes. Never run `go vet ./...` or `go test ./...`: they walk Go code under `web/node_modules`.
- Give every test command an explicit timeout (`timeout 300 …`). Run every Go test command in this plan with `-race`.
- The change is confined to `internal/runner` and one comment in `internal/daemon/backend.go`. No change to `internal/model`, the change hub, the API handlers, `cmd/ghr` or `web/`.
- `Backend.KillRunner` keeps calling `b.Wake` after a successful stop. GitHub calls stay serial: do not add goroutines to `gatherDemand` or any other tick step.
- Only a confirmed exit starts a cleanup: `SD.Active` answering `err == nil && !active`, the test `refreshUnits` applies. A successful `SD.Stop` alone is not that confirmation.
- Release gate: finishing sets register row 33 to `verify`, never `done`. It becomes `done` only after both manual checks of spec §4 pass on the owner's LXC with a build of this branch, each run several times with the daemon's status sampled alongside the browser so that at least one stop of each kind ends while a tick is in flight, and their timings are written in the row's Note.
- English only. Comments only where the why is not obvious; never mention this plan, a task or a register row in a comment.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no period. No attribution lines.

## Contracts

- Task 1 produces, Task 2 consumes: `func (m *Manager) beginFinish(id string)` in `internal/runner/lifecycle.go` starts nothing once `Manager.Close` has been called, and calls `m.wg.Add(1)` before it releases `m.mu`.
- Task 2 produces: `func (m *Manager) startFinish(id string, skipCleaning bool)` in `internal/runner/lifecycle.go`; `beginFinish(id)` becomes `m.startFinish(id, false)`.
- Task 2 produces, for tests only: `fakeSD.keepActive bool` in `internal/runner/fakes_test.go`; when true, `Stop` succeeds and leaves the unit reading active.

## Assumptions (evidence)

- Every writer of an instance's `State` holds `m.mu` and refuses a `cleaning` instance: `internal/runner/manager.go:370` (`setState`), `internal/runner/lifecycle.go:164-166` (`readJobFiles`), `internal/runner/tick.go:159-163` (`confirm`). Confirmed by the Fable review, docs/superpowers/notes/2026-10-10-ghr-stop-starts-cleanup-fable-review.md, 2026-10-10.
- The only instance lookup that assumes the instance still exists is `internal/runner/lifecycle.go:607` (`stopStartTimedOut`): `rg -n "m\.insts\[" internal/runner` on 2026-10-10, each other hit checks `ok` or nil.
- `finalize` keeps the pending entry's `RunNumber` and `Workflow` and overwrites only the job name, URL, times and conclusion: `internal/runner/lifecycle.go:444-458`.
- `sched.StartTimedOut` returns IDs in the order of its input, and `schedInstances` is sorted by ID: `internal/sched/sched.go:247-255`, `internal/runner/manager.go:376-385`. Task 1's test relies on `aaaaaa` being stopped before `bbbbbb` is looked up.
- The fake systemd's `Stop` marks the unit inactive, so in tests a successful `Kill` is followed by `Active` answering false: `internal/runner/fakes_test.go:188-208`.
- The race detector needs cgo; `gcc` is at `/usr/bin/gcc` (`command -v gcc`, 2026-10-10).
- External executor: the Codex gate printed `usable=true … lane=true` on 2026-10-10. The lane was not offered; the plan is Claude-only by the planner's ruling, to save the owner a question on a two-task plan. The owner may add `**Executor:**` lines by hand.
- `plan-lint` may warn that Task 2's test `TestKillRacingShutdownLeavesNoCleanupRunning` cannot fail deterministically before the change. It is the only test that exercises `wg.Add` against `Wait`; `TestKillAfterCloseStartsNoCleanup` is its deterministic companion.

## Task index

1. Refuse cleanup after Close and skip a vanished runner
2. Start the cleanup from Kill

---

### Task 1: Refuse cleanup after Close and skip a vanished runner

**Files:**
- Modify: `internal/runner/lifecycle.go` (`beginFinish`, about lines 230-246; `stopStartTimedOut`, about lines 603-615)
- Test: `internal/runner/lifecycle_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: the `beginFinish` behaviour in Contracts.

**Items:** 33
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 3 = 4

- [ ] **Step 1: Write the failing tests**

Append to `internal/runner/lifecycle_test.go`:

```go
func TestBeginFinishStartsNothingAfterClose(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.Close()
	h.m.beginFinish("aaaaaa")
	h.m.Wait()
	if h.state("aaaaaa") != "starting" {
		t.Fatalf("state %s after Close, want starting", h.state("aaaaaa"))
	}
	if _, err := os.Stat(h.m.instanceDir("aaaaaa")); err != nil {
		t.Fatalf("instance dir after Close: %v", err)
	}
}

// A runner can be removed while a tick is stopping another one.
func TestStartTimeoutSkipsARunnerThatIsGone(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.now = h.now.Add(3 * time.Minute)
	h.sd.stopGate, h.sd.stopEntered = make(chan struct{}), make(chan struct{}, 2)
	go func() {
		<-h.sd.stopEntered
		h.m.mu.Lock()
		delete(h.m.insts, "bbbbbb")
		h.m.mu.Unlock()
		close(h.sd.stopGate)
	}()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("stopStartTimedOut panicked on a runner that is gone: %v", r)
		}
	}()
	h.m.stopStartTimedOut(context.Background(), h.cfg, h.now)
	if !reflect.DeepEqual(h.sd.stopped, []string{"ghr-runner-aaaaaa"}) {
		t.Fatalf("stopped %v, want only ghr-runner-aaaaaa", h.sd.stopped)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `timeout 300 go test -race ./internal/runner/ -run 'TestBeginFinishStartsNothingAfterClose|TestStartTimeoutSkipsARunnerThatIsGone' -count=1`

Expected: both FAIL. The first with `state cleaning after Close, want starting` or `state gone after Close, want starting`; the second with `stopStartTimedOut panicked on a runner that is gone: runtime error: invalid memory address or nil pointer dereference`.

- [ ] **Step 3: Write the implementation**

In `internal/runner/lifecycle.go`, replace the comment and the opening of `beginFinish`, down to and including the `m.wg.Add(1)` line. Before:

```go
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
```

After:

```go
// beginFinish moves an instance to cleaning and runs finish in the background.
// A failed finish leaves it cleaning (holding its repo slot) for refreshUnits to retry.
// It starts nothing once Close has been called: the instance dir stays, and
// adoption cleans the runner up at the next start.
func (m *Manager) beginFinish(id string) {
	m.mu.Lock()
	i, ok := m.insts[id]
	if !ok || i.finishing || m.closed {
		m.mu.Unlock()
		return
	}
	if i.State != sched.Cleaning {
		i.State = sched.Cleaning
		i.StateSince = m.Now()
	}
	i.finishing = true
	repo := i.Repo
	// Counted under the lock that guards closed: a caller that outlives the
	// run loop must not add to the group while Wait runs.
	m.wg.Add(1)
	m.mu.Unlock()
```

The `go func() { defer m.wg.Done() …` block that follows is unchanged.

In the same file, in `stopStartTimedOut`, replace:

```go
		m.mu.Lock()
		repo := m.insts[id].Repo
		m.mu.Unlock()
```

with:

```go
		m.mu.Lock()
		i, ok := m.insts[id]
		repo := ""
		if ok {
			repo = i.Repo
		}
		m.mu.Unlock()
		// A stop a person asked for can finish and remove a runner while this
		// loop is stopping another one.
		if !ok {
			continue
		}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `gofmt -l internal cmd web/*.go; go vet ./internal/... ./cmd/... ./web/ && timeout 300 go test -race ./internal/runner/ ./internal/daemon/ -count=1`

Expected: `gofmt` prints nothing, `go vet` prints nothing, and both packages report `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/runner/lifecycle.go internal/runner/lifecycle_test.go
git commit -m "fix(runner): refuse cleanup after close" -m "beginFinish counts its goroutine under the manager lock and starts
nothing once Close has been called, so a caller that outlives the run
loop cannot race Wait. The start timeout skips a runner that was
removed while it was stopping another one."
```

---

### Task 2: Start the cleanup from Kill

**Files:**
- Modify: `internal/runner/lifecycle.go` (`beginFinish`, about lines 230-250)
- Modify: `internal/runner/manager.go` (`Kill` and its comment, about lines 491-515)
- Modify: `internal/daemon/backend.go` (the `Wake` field comment, about lines 91-93)
- Modify: `internal/runner/fakes_test.go` (`fakeSD`, `fakeSD.Stop`, `newHarness`)
- Test: `internal/runner/lifecycle_test.go`

**Interfaces:**
- Consumes: the `beginFinish` behaviour in Contracts (Task 1).
- Produces: `startFinish` and `fakeSD.keepActive` as stated in Contracts.

**Items:** 33
**Implementer:** dr-superpowers:impl-opus-high
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 3 = 6

- [ ] **Step 1: Extend the test fakes**

In `internal/runner/fakes_test.go`, add a field to `fakeSD` below `stopEntered chan struct{}`:

```go
	// keepActive makes Stop succeed while the unit still reads active.
	keepActive bool
```

In `fakeSD.Stop`, replace the line `f.active[unit] = false` with:

```go
	if !f.keepActive {
		f.active[unit] = false
	}
```

In `newHarness`, directly after the block that ends with `t.Fatal(err)` for `h.m.Init()`, add:

```go
	// A cleanup started by a kill would otherwise outlive its test and race
	// the removal of the temporary directory.
	t.Cleanup(func() { h.m.Wait() })
```

- [ ] **Step 2: Write the failing tests**

In `internal/runner/lifecycle_test.go`, add `"sync"` to the import block, between `"strings"` and `"syscall"`.

In `TestKillMarksTheRunnerStoppingWhileItsUnitStops`, replace the last check. Before:

```go
	if !stoppingOf(h, "aaaaaa") || !strings.Contains(h.eventText(), "runner aaaaaa stopped by user") {
		t.Fatalf("after the stop: stopping %v events %s", stoppingOf(h, "aaaaaa"), h.eventText())
	}
```

After:

```go
	h.m.Wait()
	if s := h.state("aaaaaa"); s == "starting" || stoppingOf(h, "aaaaaa") || !strings.Contains(h.eventText(), "runner aaaaaa stopped by user") {
		t.Fatalf("after the stop: state %s stopping %v events %s", s, stoppingOf(h, "aaaaaa"), h.eventText())
	}
```

Replace the whole of `TestACleaningRunnerNeverReportsStopping`, including the two comment lines above it, with:

```go
// A successful stop reaches cleaning without a tick, and Status hides the
// flag from then on.
func TestACleaningRunnerNeverReportsStopping(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
	h.docker.setErr("RemoveVolumesByLabel", errors.New("volume in use"))
	if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	if h.state("aaaaaa") != "cleaning" || stoppingOf(h, "aaaaaa") {
		t.Fatalf("state %s stopping %v", h.state("aaaaaa"), stoppingOf(h, "aaaaaa"))
	}
}

func TestKillLeavesAFailedCleanupToItsRetry(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
	h.docker.setErr("RemoveVolumesByLabel", errors.New("volume in use"))
	if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	h.docker.setErr("RemoveVolumesByLabel", nil)
	if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	if h.state("aaaaaa") != "cleaning" {
		t.Fatalf("state %s: a kill retried a failed cleanup before finishRetry", h.state("aaaaaa"))
	}
	h.now = h.now.Add(finishRetry)
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "gone" || !reflect.DeepEqual(h.history(t), []string{"aaaaaa success"}) {
		t.Fatalf("state %s history %v", h.state("aaaaaa"), h.history(t))
	}
}

func TestKillBeforeAnyTickKeepsTheJobRecord(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
	if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	got, err := h.m.History.Query("", "", time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "aaaaaa" || got[0].RunID != 55 || got[0].RunNumber != "412" || got[0].Workflow != "CI" {
		t.Fatalf("history %+v", got)
	}
}

func TestKillFinishesWhileATickIsBlockedInTheAPI(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
	h.docker.setErr("RemoveVolumesByLabel", errors.New("volume in use"))
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	h.gh.hook = func(method string) {
		if method == "ListRuns" {
			once.Do(func() {
				close(entered)
				<-release
			})
		}
	}
	ticked := make(chan struct{})
	go func() {
		h.m.Tick(context.Background())
		close(ticked)
	}()
	<-entered
	if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	if h.state("aaaaaa") != "cleaning" {
		t.Fatalf("state %s while the tick is still in its first ListRuns", h.state("aaaaaa"))
	}
	close(release)
	<-ticked
}

func TestKillLeavesAnUnconfirmedExitToTheTick(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  func(h *harness)
	}{
		{"the check fails", func(h *harness) { h.sd.activeErr["ghr-runner-aaaaaa"] = errors.New("bus error") }},
		{"the unit still reads active", func(h *harness) { h.sd.keepActive = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.m.spawn(context.Background(), h.cfg, "darkmem")
			tc.set(h)
			if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
				t.Fatal(err)
			}
			h.m.Wait()
			if h.state("aaaaaa") != "starting" || !stoppingOf(h, "aaaaaa") {
				t.Fatalf("state %s stopping %v, want starting and stopping", h.state("aaaaaa"), stoppingOf(h, "aaaaaa"))
			}
			delete(h.sd.activeErr, "ghr-runner-aaaaaa")
			h.sd.active["ghr-runner-aaaaaa"] = false
			h.m.refreshUnits(context.Background())
			h.m.Wait()
			if h.state("aaaaaa") != "gone" {
				t.Fatalf("state %s after the tick saw the exit", h.state("aaaaaa"))
			}
		})
	}
}

func TestTwoKillsStartOneCleanup(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
	h.sd.stopGate, h.sd.stopEntered = make(chan struct{}), make(chan struct{}, 2)
	done := make(chan error, 2)
	for range 2 {
		go func() { done <- h.m.Kill(context.Background(), "aaaaaa") }()
	}
	<-h.sd.stopEntered
	<-h.sd.stopEntered
	close(h.sd.stopGate)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	h.m.Wait()
	if h.state("aaaaaa") != "gone" || !reflect.DeepEqual(h.history(t), []string{"aaaaaa success"}) {
		t.Fatalf("state %s history %v", h.state("aaaaaa"), h.history(t))
	}
}

func TestKillAfterCloseStartsNoCleanup(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.m.Close()
	if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	h.m.Wait()
	if h.state("aaaaaa") != "starting" || !stoppingOf(h, "aaaaaa") {
		t.Fatalf("state %s stopping %v, want starting and stopping", h.state("aaaaaa"), stoppingOf(h, "aaaaaa"))
	}
	if _, err := os.Stat(h.m.instanceDir("aaaaaa")); err != nil {
		t.Fatalf("instance dir after Close: %v", err)
	}
}

// Under the race detector this is what catches a cleanup counted outside the
// manager lock.
func TestKillRacingShutdownLeavesNoCleanupRunning(t *testing.T) {
	for range 50 {
		h := newHarness(t)
		h.m.spawn(context.Background(), h.cfg, "darkmem")
		killed := make(chan error, 1)
		go func() { killed <- h.m.Kill(context.Background(), "aaaaaa") }()
		h.m.Close()
		h.m.Wait()
		if err := <-killed; err != nil {
			t.Fatal(err)
		}
		h.m.mu.Lock()
		i := h.m.insts["aaaaaa"]
		running := i != nil && i.finishing
		h.m.mu.Unlock()
		if running {
			t.Fatal("a cleanup is running after Wait returned")
		}
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 300 go test -race ./internal/runner/ -run 'TestKill|TestACleaningRunnerNeverReportsStopping|TestTwoKillsStartOneCleanup' -count=1`

Expected: FAIL. `TestKillMarksTheRunnerStoppingWhileItsUnitStops` (`after the stop: state starting`), `TestACleaningRunnerNeverReportsStopping` (`state starting stopping true`), `TestKillLeavesAFailedCleanupToItsRetry`, `TestKillBeforeAnyTickKeepsTheJobRecord` (`history []`), `TestKillFinishesWhileATickIsBlockedInTheAPI` and `TestTwoKillsStartOneCleanup` fail, because no cleanup starts without a tick. `TestKillLeavesAnUnconfirmedExitToTheTick`, `TestKillAfterCloseStartsNoCleanup`, `TestKillRacingShutdownLeavesNoCleanupRunning`, `TestKill` and `TestKillClearsStoppingWhenTheStopFails` pass already: they pin behaviour the change must keep.

- [ ] **Step 4: Write the implementation**

In `internal/runner/lifecycle.go`, replace the comment and the opening of `beginFinish`, down to and including its first `m.mu.Unlock()` / `return` / `}` block. Before:

```go
// beginFinish moves an instance to cleaning and runs finish in the background.
// A failed finish leaves it cleaning (holding its repo slot) for refreshUnits to retry.
// It starts nothing once Close has been called: the instance dir stays, and
// adoption cleans the runner up at the next start.
func (m *Manager) beginFinish(id string) {
	m.mu.Lock()
	i, ok := m.insts[id]
	if !ok || i.finishing || m.closed {
		m.mu.Unlock()
		return
	}
```

After:

```go
// beginFinish moves an instance to cleaning and runs finish in the background.
// A failed finish leaves it cleaning (holding its repo slot) for refreshUnits to retry.
// It starts nothing once Close has been called: the instance dir stays, and
// adoption cleans the runner up at the next start.
func (m *Manager) beginFinish(id string) { m.startFinish(id, false) }

// startFinish is beginFinish; with skipCleaning it leaves an instance that is
// already cleaning alone, so that only refreshUnits retries a failed finish,
// when its delay has passed.
func (m *Manager) startFinish(id string, skipCleaning bool) {
	m.mu.Lock()
	i, ok := m.insts[id]
	if !ok || i.finishing || m.closed || (skipCleaning && i.State == sched.Cleaning) {
		m.mu.Unlock()
		return
	}
```

The rest of the function body is unchanged.

In `internal/runner/manager.go`, replace `Kill` and its comment. Before:

```go
// Kill stops a runner's unit and marks the runner stopping until a tick
// handles its exit. The stop does not end with ctx: a caller that gives up
// would kill systemctl while the unit may still be stopping, and the runner
// would read as running again.
func (m *Manager) Kill(ctx context.Context, id string) error {
	m.mu.Lock()
	i, ok := m.insts[id]
	if ok {
		i.stopping = true
	}
	m.mu.Unlock()
	if !ok {
		return ErrUnknownRunner(id)
	}
	if err := m.SD.Stop(context.WithoutCancel(ctx), UnitPrefix+id); err != nil {
		m.mu.Lock()
		if cur, ok := m.insts[id]; ok {
			cur.stopping = false
		}
		m.mu.Unlock()
		return err
	}
	m.Events.Add("warn", i.Repo, "runner %s stopped by user", id)
	return nil
}
```

After:

```go
// Kill stops a runner's unit, marks the runner stopping while the unit stops,
// and starts its cleanup once systemd confirms the unit is down. It does not
// leave the exit to a tick: one in flight can take seconds. Neither the stop
// nor the check ends with ctx: a caller that gives up would kill systemctl
// while the unit may still be stopping, and the runner would read as running
// again.
func (m *Manager) Kill(ctx context.Context, id string) error {
	m.mu.Lock()
	i, ok := m.insts[id]
	if ok {
		i.stopping = true
	}
	m.mu.Unlock()
	if !ok {
		return ErrUnknownRunner(id)
	}
	ctx = context.WithoutCancel(ctx)
	if err := m.SD.Stop(ctx, UnitPrefix+id); err != nil {
		m.mu.Lock()
		if cur, ok := m.insts[id]; ok {
			cur.stopping = false
		}
		m.mu.Unlock()
		return err
	}
	m.Events.Add("warn", i.Repo, "runner %s stopped by user", id)
	if active, err := m.SD.Active(ctx, UnitPrefix+id); err == nil && !active {
		m.startFinish(id, true)
	}
	return nil
}
```

In `internal/daemon/backend.go`, replace the `Wake` field comment. Before:

```go
	// Wake asks the run loop for an immediate tick after a config change or a
	// runner stop, so a pause stops idle runners and a stopped runner is
	// cleaned up at once; nil in tests.
```

After:

```go
	// Wake asks the run loop for an immediate tick after a config change or a
	// runner stop, so a pause stops idle runners at once, and a stopped
	// runner's replacement starts without waiting for the poll interval; nil
	// in tests.
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `gofmt -l internal cmd web/*.go; go vet ./internal/... ./cmd/... ./web/ && timeout 600 go test -race ./internal/... ./cmd/... ./web/ -count=1`

Expected: `gofmt` prints nothing, `go vet` prints nothing, and every package reports `ok` or `no test files`.

Then run the shutdown race test on its own ten times: `timeout 600 go test -race ./internal/runner/ -run TestKillRacingShutdownLeavesNoCleanupRunning -count=10`

Expected: `ok`, with no `WARNING: DATA RACE` and no `WaitGroup` panic.

- [ ] **Step 6: Commit**

```bash
git add internal/runner/lifecycle.go internal/runner/manager.go internal/runner/fakes_test.go internal/runner/lifecycle_test.go internal/daemon/backend.go
git commit -m "fix(runner): start cleanup when a stop completes" -m "Kill asks systemd whether the unit is down once its stop succeeds and
starts the cleanup itself. A woken tick waits for a tick in flight,
which takes about 6.5 s on the LXC, and the runner read as stopping
for that long after its unit was gone. A cleaning instance is left to
refreshUnits, so repeated kills cannot retry a failed cleanup early."
```
