# ghr Runner Stopping State Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A runner stopped with `ghr kill` or the UI's Stop button reads "stopping" everywhere in under a second, and is cleaned up as soon as its unit is down (register row 25).

**Architecture:** The runner manager sets an in-memory `stopping` flag on the instance before it asks systemd to stop the unit, and runs that stop on a context its caller cannot cancel. `Manager.Status` reports the flag, the existing `status` change topic publishes it within 250 ms, and `Backend.KillRunner` wakes the manager loop once the unit is down so the exit is handled at once. The activity view, `ghr status` and the web UI read the flag; no state, count or segment changes.

**Tech Stack:** Go 1.26 standard library, React 19 + TypeScript, Vitest + Testing Library under jsdom.

**Spec:** docs/superpowers/specs/2026-10-10-ghr-runner-stopping-state-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 7 of 8 tasks total 4 or less with risk below 3; Task 1 (total 5) is delegated.

**Plan review:** 2026-10-10 — codex gpt-6-astra / xhigh — executability 16 / coherence 19 / coverage 18 / assumptions 17 (round 1)

## Global Constraints

- Execution environment: the owner's Linux machine (bash, GNU coreutils `timeout`, Go 1.26 at `/usr/local/go/bin`, Node 24). If `go` is not on `PATH`, run `export PATH=$PATH:/usr/local/go/bin` first.
- Work on the branch `feat/runner-stopping` (Task 1 Step 0 creates it from `master`), never on `master`. Do not push `master`: CI turns every push to `master` into a release. Do not commit `.superpowers/`.
- Go checks, from the repository root: `gofmt -l internal cmd web/*.go` prints nothing; `go vet ./internal/... ./cmd/... ./web/` passes. Never run `go vet ./...` or `go test ./...`: they walk Go code under `web/node_modules`.
- Web checks, from `web/`: `npm test`, `npm run typecheck`, `npm run lint` (eslint with `--max-warnings 0`).
- Run `gofmt -w` on every Go file you change before the checks: a field added to an aligned struct block realigns the whole block.
- Give every test command an explicit timeout (`timeout 300 …`). Run every Go test command in this plan with `-race`.
- The flag is a boolean beside the state. Do not add a `stopping` state: `sched.State`, the capacity bar, the busy counts, `lib/capacity.ts`, `lib/summary.ts`, `lib/status.ts`, `components/refused-hint.tsx` and `components/capacity-bar.tsx` stay as they are.
- Only `Manager.Kill` sets the flag, and nothing in the manager except `Manager.Status` reads it. The idle timeout, the start timeout, repository removal and runner updates are out of scope.
- JSON name, verbatim: `stopping`, with `omitempty`, on `model.InstanceStatus` and `model.ActivityRun`. The generated fixtures under `web/src/api/fixtures/` must not change; frontend tests build their own stopping instance.
- Copy, verbatim: list and panel text `Stopping`; list row with a job `Stopping, <job name> #<run number>`; run description word `stopping`; dialog title `Runner <id> is already stopping`; dialog description `ghr is stopping it and will clean it up.`; legend entry `Stopping`; `ghr status` STATE `stopping`.
- A stopping run's open lane segment, verbatim classes: `fill-none stroke-muted-foreground`, with `stroke-dasharray="3 2"` and no pulsing end marker.
- Release gate: finishing sets register row 25 to `verify`, never `done`, when the tasks are complete. It becomes `done` only after both manual checks of spec §4 pass on the owner's LXC with a build of this branch, with a busy runner and the Dashboard open, and their timings are written in the row's Note: (1) within a second of `ghr kill <id>` the runner's bar is the dashed grey outline and its tooltip says "stopping", and within a second of the command returning (or of a tick in flight ending) the lane reads "finishing, result pending" or the job's result; (2) the same with Ctrl-C on `ghr kill` two seconds in, timed from the unit's exit. No task performs these checks.
- English only. Comments only where the why is not obvious.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no period.

## Contracts

**Go — `internal/model`**

- Task 1: `InstanceStatus.Stopping bool` with tag `json:"stopping,omitempty"`.
- Task 3: `ActivityRun.Stopping bool` with tag `json:"stopping,omitempty"`.

**Go — `internal/runner` (Task 1)**

- `instance.stopping bool`, guarded by `Manager.mu`.
- `(m *Manager) Kill(ctx context.Context, id string) error`: same signature. It sets `stopping` before `SD.Stop`, calls `SD.Stop(context.WithoutCancel(ctx), UnitPrefix+id)`, clears the flag and returns the error if the stop fails, and leaves the flag set on success. `ErrUnknownRunner` for an unknown ID, as today.
- `(m *Manager) Status()`: `InstanceStatus.Stopping` is `instance.stopping && State != sched.Cleaning`.
- Test fake (`fakes_test.go`): `fakeSD.stopGate chan struct{}` (when set, `Stop` waits until it is closed or the call's context ends: a closed gate lets `Stop` carry on as usual, an ended context makes it return `ctx.Err()` without stopping the unit); `fakeSD.stopEntered chan struct{}` (receives once per held call).

**Go — `internal/daemon` (Task 2)**

- `(b *Backend) KillRunner(ctx, id)` calls `b.Wake()` (when non-nil) once after `b.M.Kill` returns nil, and never after an error.
- Test fake (`backend_test.go`): `fakeManager.killErr error`, returned by `Kill`.

**Web — `web/src` (Task 5)**

- `api/types.ts`: `InstanceStatus.stopping?: boolean`, `ActivityRun.stopping?: boolean`.
- `lib/activity-view.ts` `runAria(run, now)`: the state word is `stopping` when `run.stopping` is true.

## Assumptions (evidence)

- `systemctl stop` blocks until the unit is down, so a nil return from `SD.Stop` means the next tick's `refreshUnits` will see it inactive: `internal/system/system.go:108-111` runs the command to completion, and `internal/system/system.go:114` counts `deactivating` as active, so a tick during the stop does not start cleanup early (read 2026-10-10).
- A cancelled context kills `systemctl`: `execute` builds the command with `exec.CommandContext` (`internal/system/system.go:49-52`), and the control API passes the request's context to `KillRunner` (`internal/api/server.go:240`). `context.WithoutCancel` keeps the context's values and drops its cancellation; `system.CommandTimeout` (10 minutes, `internal/system/system.go:25`) still bounds the stop.
- The `status` topic's fingerprint is the JSON of the whole status with five fields zeroed (`internal/daemon/changes.go:70-75`), so the new field publishes `status` with no change to the hub. `storage` reads only `DiskPct` from the status (`internal/daemon/backend.go:707-712`), so no other topic moves. Task 2's fingerprint test therefore passes as soon as Task 1's field exists; it is a guard, not a red test.
- The browser refetches the activity view on `status` (`web/src/api/use-change-stream.ts:9`), and the daemon drops its cached activity views before it publishes `status` (`internal/daemon/backend.go:190-192`), so the lane shows the flag without a new topic.
- `stateTone` gives an unlisted state the muted tone (`web/src/lib/status.ts:41-43`), so the panel's "Stopping" is muted with no change to `lib/status.ts`.
- `web/fixtures_test.go` regenerates fixtures from Go values that never set the flag, and `omitempty` leaves it out, so `TestTypeFixtures` passes unchanged (dry run below).
- Dry run, 2026-10-10: every task's code from this plan, applied to `3356cea` in a throwaway worktree, passed `gofmt -l`, `go vet ./internal/... ./cmd/... ./web/`, `go test -race` on `runner`, `model`, `daemon`, `activity`, `api`, `cmd/ghr` and `web`, and `npm run typecheck`, `npm run lint` and `npm test` (87 files, 1040 tests). Each new test failed with its implementation removed, with two exceptions stated in the next two bullets.
- Task 2's `TestAStoppingFlagChangesOnlyStatus` passes before Task 2's implementation (see the fingerprint bullet above).
- Task 5's "counts as it did before it was stopped" passes before any implementation: `lib/capacity.ts` and `lib/summary.ts` count by `state`, which the flag leaves alone. It guards spec §4's "the capacity bar and the busy count are the same with and without the flag".
- jsdom runs no animations, so the kit's dialog unmounts its content the moment it closes and a test through the real dialog cannot see the closing state (probed 2026-10-10: such a test passed with the `instance !== null` guard removed). Task 7's `stop-runner-dialog.test.tsx` therefore replaces `darkraise-ui/components/alert-dialog` with a stand-in that always renders its content; with the guard removed that test failed three runs of three, and with it passed three of three. It is the repository's first `vi.mock` of a kit module.
- The manual checks of spec §4 need a deployed build, a live GitHub job and a browser, so they are a release gate (Global Constraints) and not a task. Row 3 of the register stays at `verify` until they pass.
- Task 5's types are optional fields, so Tasks 6 to 8 compile against a daemon that omits them.

## Task index

1. Manager stopping flag
2. Wake the loop after a stop
3. Activity runs carry the flag
4. ghr status prints stopping
5. Web types and run description
6. Runner list shows stopping
7. Runner panel and stop dialog
8. Lanes chart and legend

---

### Task 1: Manager stopping flag

**Files:**
- Modify: `internal/model/model.go:88-95`
- Modify: `internal/runner/manager.go:130-140`, `internal/runner/manager.go:470-474`, `internal/runner/manager.go:489-502`
- Modify: `internal/runner/fakes_test.go:163-193`
- Test: `internal/runner/lifecycle_test.go` (new tests before `TestPendingListsJobsAwaitingTheirConclusion`, line 591)

**Interfaces:**
- Consumes: nothing.
- Produces: Contracts, "Go — `internal/model`" (Task 1 line) and "Go — `internal/runner` (Task 1)".

**Items:** 25
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 0: Create the branch**

```bash
git switch -c feat/runner-stopping master
```

- [ ] **Step 1: Make the fake systemd able to hold a stop**

In `internal/runner/fakes_test.go`, replace

```go
	stopErr   map[string]error // by unit
	activeErr map[string]error // by unit
}
```

with

```go
	stopErr   map[string]error // by unit
	activeErr map[string]error // by unit
	// stopGate, when set, holds every Stop until it is closed or the call's
	// context ends; stopEntered receives once per held call.
	stopGate    chan struct{}
	stopEntered chan struct{}
}
```

and replace

```go
func (f *fakeSD) Stop(_ context.Context, unit string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
```

with

```go
func (f *fakeSD) Stop(ctx context.Context, unit string) error {
	f.mu.Lock()
	gate, entered := f.stopGate, f.stopEntered
	f.mu.Unlock()
	if gate != nil {
		entered <- struct{}{}
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
```

- [ ] **Step 2: Write the failing tests**

In `internal/runner/lifecycle_test.go`, insert directly above `func TestPendingListsJobsAwaitingTheirConclusion(t *testing.T) {`:

```go
// stoppingOf is the Stopping flag Status reports for id.
func stoppingOf(h *harness, id string) bool {
	for _, i := range h.m.Status().Instances {
		if i.ID == id {
			return i.Stopping
		}
	}
	return false
}

func TestKillMarksTheRunnerStoppingWhileItsUnitStops(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.sd.stopGate, h.sd.stopEntered = make(chan struct{}), make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.m.Kill(ctx, "aaaaaa") }()
	<-h.sd.stopEntered
	if !stoppingOf(h, "aaaaaa") || h.state("aaaaaa") != "starting" {
		t.Fatalf("while the unit stops: stopping %v state %s", stoppingOf(h, "aaaaaa"), h.state("aaaaaa"))
	}
	// The caller gives up: the stop goes on and the flag stays.
	cancel()
	select {
	case err := <-done:
		t.Fatalf("Kill returned %v when its caller's context ended", err)
	case <-time.After(50 * time.Millisecond):
	}
	if !stoppingOf(h, "aaaaaa") {
		t.Fatal("flag cleared when the caller's context ended")
	}
	close(h.sd.stopGate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !stoppingOf(h, "aaaaaa") || !strings.Contains(h.eventText(), "runner aaaaaa stopped by user") {
		t.Fatalf("after the stop: stopping %v events %s", stoppingOf(h, "aaaaaa"), h.eventText())
	}
}

func TestKillClearsStoppingWhenTheStopFails(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.sd.stopErr["ghr-runner-aaaaaa"] = errors.New("bus error")
	if err := h.m.Kill(context.Background(), "aaaaaa"); err == nil || err.Error() != "bus error" {
		t.Fatalf("kill err %v", err)
	}
	if stoppingOf(h, "aaaaaa") || strings.Contains(h.eventText(), "stopped by user") {
		t.Fatalf("stopping %v events %s", stoppingOf(h, "aaaaaa"), h.eventText())
	}
}

// The flag stays on the instance after a stop; Status hides it once the tick
// has seen the unit exit.
func TestACleaningRunnerNeverReportsStopping(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
	h.docker.setErr("RemoveVolumesByLabel", errors.New("volume in use"))
	if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
		t.Fatal(err)
	}
	if !stoppingOf(h, "aaaaaa") {
		t.Fatal("flag cleared by a successful stop")
	}
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "cleaning" || stoppingOf(h, "aaaaaa") {
		t.Fatalf("state %s stopping %v", h.state("aaaaaa"), stoppingOf(h, "aaaaaa"))
	}
}

```

The file already imports `context`, `errors`, `strings` and `time`. The existing `TestKill` and `TestUnknownRunner` already cover `ErrUnknownRunner`; leave them as they are.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `timeout 300 go test -race ./internal/runner/ -run 'TestKill|TestACleaningRunner'`
Expected: build failure, `i.Stopping undefined (type model.InstanceStatus has no field or method Stopping)`.

- [ ] **Step 4: Add the wire field**

In `internal/model/model.go`, replace

```go
	Since      time.Time `json:"since"`
	Job        *JobInfo  `json:"job,omitempty"`
}
```

with

```go
	Since      time.Time `json:"since"`
	Job        *JobInfo  `json:"job,omitempty"`
	// Stopping is true from a person's stop request until the unit has exited.
	Stopping bool `json:"stopping,omitempty"`
}
```

- [ ] **Step 5: Set, report and clear the flag in the manager**

In `internal/runner/manager.go`, replace

```go
	deregistered bool      // stopIdleRunner deleted its registration
```

with

```go
	deregistered bool      // stopIdleRunner deleted its registration
	stopping     bool      // Kill asked systemd to stop the unit
```

In `Status`, replace

```go
			ID: i.ID, Repo: i.Repo, RunnerName: i.RunnerName, State: string(i.State), Since: i.StateSince, Job: i.Job,
		})
```

with

```go
			ID: i.ID, Repo: i.Repo, RunnerName: i.RunnerName, State: string(i.State), Since: i.StateSince, Job: i.Job,
			Stopping: i.stopping && i.State != sched.Cleaning,
		})
```

Replace the whole of `Kill` and its comment

```go
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
```

with

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

Do not read `stopping` anywhere else in the package.

- [ ] **Step 6: Format and run the package**

```bash
gofmt -w internal/model/model.go internal/runner/manager.go internal/runner/fakes_test.go internal/runner/lifecycle_test.go
timeout 300 go test -race ./internal/runner/ ./internal/model/ ./web/
```

Expected: three `ok` lines. `./web/` proves the generated fixtures did not change.

- [ ] **Step 7: Run the Go checks**

```bash
gofmt -l internal cmd web/*.go
go vet ./internal/... ./cmd/... ./web/
```

Expected: no output from either.

- [ ] **Step 8: Commit**

```bash
git add internal/model/model.go internal/runner/manager.go internal/runner/fakes_test.go internal/runner/lifecycle_test.go
git commit -m "feat(runner): mark a stopped runner as stopping"
```

### Task 2: Wake the loop after a stop

**Files:**
- Modify: `internal/daemon/backend.go:91-93`, `internal/daemon/backend.go:358-360`
- Modify: `internal/daemon/backend_test.go:29-33`, `internal/daemon/backend_test.go:95-98`
- Test: `internal/daemon/backend_test.go` (new test above `TestPauseAllStepsKillToken`, line 556), `internal/daemon/changes_test.go` (new test above `type panickyStorage`, line 123)

**Interfaces:**
- Consumes: Contracts, "Go — `internal/model`" (Task 1 line): `InstanceStatus.Stopping`.
- Produces: Contracts, "Go — `internal/daemon` (Task 2)".

**Items:** 25
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Let the fake manager fail a stop**

In `internal/daemon/backend_test.go`, replace

```go
	killed    []string
```

with

```go
	killed    []string
	killErr   error // returned by Kill
```

and replace

```go
	f.killed = append(f.killed, id)
	return nil
}
```

with

```go
	f.killed = append(f.killed, id)
	return f.killErr
}
```

- [ ] **Step 2: Write the tests**

In `internal/daemon/backend_test.go`, insert directly above `func TestPauseAllStepsKillToken(t *testing.T) {`:

```go
func TestKillRunnerWakesTheLoopOnlyAfterAStop(t *testing.T) {
	b, m, _ := newBackend(t)
	ctx := context.Background()
	wakes := 0
	b.Wake = func() { wakes++ }
	m.killErr = errors.New("bus error")
	if err := b.KillRunner(ctx, "aaaaaa"); err == nil || err.Error() != "bus error" || wakes != 0 {
		t.Fatalf("failed stop: err %v wakes %d", err, wakes)
	}
	m.killErr = runner.ErrUnknownRunner("zzzzzz")
	if err := b.KillRunner(ctx, "zzzzzz"); apiStatus(err) != 404 || wakes != 0 {
		t.Fatalf("unknown runner: err %v wakes %d", err, wakes)
	}
	m.killErr = nil
	if err := b.KillRunner(ctx, "aaaaaa"); err != nil || wakes != 1 {
		t.Fatalf("stop: err %v wakes %d", err, wakes)
	}
}

```

In `internal/daemon/changes_test.go`, insert directly above `type panickyStorage struct{ fakeStorage }`:

```go
func TestAStoppingFlagChangesOnlyStatus(t *testing.T) {
	b, m, _ := newBackend(t)
	m.insts = []model.InstanceStatus{{ID: "aaaaaa", Repo: "darkmem", State: "busy"}}
	prev := b.fingerprints()
	m.insts = []model.InstanceStatus{{ID: "aaaaaa", Repo: "darkmem", State: "busy", Stopping: true}}
	cur := b.fingerprints()
	if got := changed(prev, cur); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("setting the flag changed %v", got)
	}
	m.insts = []model.InstanceStatus{{ID: "aaaaaa", Repo: "darkmem", State: "busy"}}
	if got := changed(cur, b.fingerprints()); !slices.Equal(got, []string{"status"}) {
		t.Fatalf("clearing the flag changed %v", got)
	}
}

```

Both files already import everything these tests use (`context`, `errors`, `slices`, `model`, `runner`).

- [ ] **Step 3: Run the tests to verify the wake test fails**

Run: `timeout 300 go test -race ./internal/daemon/ -run 'TestKillRunnerWakes|TestAStoppingFlag'`
Expected: `TestKillRunnerWakesTheLoopOnlyAfterAStop` FAILS with `stop: err <nil> wakes 0`. `TestAStoppingFlagChangesOnlyStatus` PASSES already: it guards the existing fingerprint (see Assumptions).

- [ ] **Step 4: Wake the loop from KillRunner**

In `internal/daemon/backend.go`, replace

```go
	// Wake asks the run loop for an immediate tick after a config change, so a
	// pause stops idle runners at once; nil in tests.
```

with

```go
	// Wake asks the run loop for an immediate tick after a config change or a
	// runner stop, so a pause stops idle runners and a stopped runner is
	// cleaned up at once; nil in tests.
```

and replace

```go
func (b *Backend) KillRunner(ctx context.Context, id string) error {
	return notFoundIfUnknown(b.M.Kill(ctx, id))
}
```

with

```go
func (b *Backend) KillRunner(ctx context.Context, id string) error {
	if err := b.M.Kill(ctx, id); err != nil {
		return notFoundIfUnknown(err)
	}
	if b.Wake != nil {
		b.Wake()
	}
	return nil
}
```

- [ ] **Step 5: Format, run the package and the Go checks**

```bash
gofmt -w internal/daemon/backend.go internal/daemon/backend_test.go internal/daemon/changes_test.go
timeout 300 go test -race ./internal/daemon/ ./internal/api/
gofmt -l internal cmd web/*.go
go vet ./internal/... ./cmd/... ./web/
```

Expected: two `ok` lines, then no output.

- [ ] **Step 6: Commit**

```bash
git add internal/daemon/backend.go internal/daemon/backend_test.go internal/daemon/changes_test.go
git commit -m "feat(daemon): wake the loop after a runner stop"
```

### Task 3: Activity runs carry the flag

**Files:**
- Modify: `internal/model/model.go:469-477`
- Modify: `internal/activity/activity.go:166`
- Test: `internal/activity/activity_test.go` (new test above `TestConclusionStatesAndAbsentURL`, line 128)

**Interfaces:**
- Consumes: Contracts, "Go — `internal/model`" (Task 1 line): `InstanceStatus.Stopping`.
- Produces: Contracts, "Go — `internal/model`" (Task 3 line): `ActivityRun.Stopping`.

**Items:** 25
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

In `internal/activity/activity_test.go`, add `"reflect"` to the imports, between `"encoding/json"` and `"strings"`, and insert directly above `func TestConclusionStatesAndAbsentURL(t *testing.T) {`:

```go
func TestStoppingInstancesKeepTheirSegments(t *testing.T) {
	build := func(stopping bool) map[string]model.ActivityRun {
		a := Build(Input{Window: "1h", Now: now,
			History: []model.HistoryEntry{job("done", at(13, 10), at(13, 20), "success")},
			Instances: []model.InstanceStatus{
				{ID: "s", Repo: "ghr", State: "starting", Since: at(14, 4), Stopping: stopping},
				{ID: "w", Repo: "darkmem", State: "idle", Since: at(14, 0), Stopping: stopping},
				{ID: "r", Repo: "darkmem", State: "busy", Since: at(13, 59), Stopping: stopping,
					Job: &model.JobInfo{RunNumber: "42", Workflow: "ci", Name: "test", StartedAt: at(14, 1)}},
				{ID: "done", Repo: "darkmem", State: "busy", Since: at(13, 10), Stopping: stopping},
			}})
		runs := map[string]model.ActivityRun{}
		for _, l := range a.Lanes {
			for _, r := range l.Runs {
				runs[r.InstanceID] = r
			}
		}
		return runs
	}
	plain, stopping := build(false), build(true)
	for _, id := range []string{"s", "w", "r"} {
		if plain[id].Stopping || !stopping[id].Stopping {
			t.Fatalf("%s: stopping %v without the flag, %v with it", id, plain[id].Stopping, stopping[id].Stopping)
		}
		if !reflect.DeepEqual(plain[id].Segments, stopping[id].Segments) {
			t.Fatalf("%s: segments %+v became %+v", id, plain[id].Segments, stopping[id].Segments)
		}
	}
	if stopping["done"].Stopping {
		t.Fatal("a run built from a finished job carries the flag")
	}
}

```

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 300 go test -race ./internal/activity/ -run TestStoppingInstances`
Expected: build failure, `plain[id].Stopping undefined (type model.ActivityRun has no field or method Stopping)`.

- [ ] **Step 3: Add the field and copy it**

In `internal/model/model.go`, replace

```go
	Segments   []ActivitySegment `json:"segments"`
	HTMLURL    string            `json:"html_url,omitempty"`
}
```

with

```go
	Segments   []ActivitySegment `json:"segments"`
	HTMLURL    string            `json:"html_url,omitempty"`
	// Stopping is the live instance's flag; a run built from a finished job
	// never carries it.
	Stopping bool `json:"stopping,omitempty"`
}
```

In `internal/activity/activity.go`, replace

```go
		r := model.ActivityRun{InstanceID: i.ID, Repo: i.Repo}
```

with

```go
		r := model.ActivityRun{InstanceID: i.ID, Repo: i.Repo, Stopping: i.Stopping}
```

Do not touch the segments, the history loop or the pending loop.

- [ ] **Step 4: Format, run the packages and the Go checks**

```bash
gofmt -w internal/model/model.go internal/activity/activity.go internal/activity/activity_test.go
timeout 300 go test -race ./internal/activity/ ./internal/model/ ./internal/daemon/ ./web/
gofmt -l internal cmd web/*.go
go vet ./internal/... ./cmd/... ./web/
```

Expected: four `ok` lines, then no output.

- [ ] **Step 5: Commit**

```bash
git add internal/model/model.go internal/activity/activity.go internal/activity/activity_test.go
git commit -m "feat(activity): carry the stopping flag on runs"
```

### Task 4: ghr status prints stopping

**Files:**
- Modify: `cmd/ghr/cli.go:349`
- Test: `cmd/ghr/cli_test.go` (new test above `TestMutatingCommands`, line 89)

**Interfaces:**
- Consumes: Contracts, "Go — `internal/model`" (Task 1 line): `InstanceStatus.Stopping`.
- Produces: nothing another task uses.

**Items:** 25
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing test**

In `cmd/ghr/cli_test.go`, insert directly above `func TestMutatingCommands(t *testing.T) {`:

```go
func TestStatusPrintsStoppingForAStoppingRunner(t *testing.T) {
	now := time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	printStatus(&out, model.Status{Now: now, Mode: "queue", GlobalMax: 3, Instances: []model.InstanceStatus{
		{ID: "a3f9c1", Repo: "darkcloud", State: "busy", Stopping: true, Since: now.Add(-time.Minute),
			Job: &model.JobInfo{Name: "e2e", RunNumber: "412"}},
		{ID: "b7d2e0", Repo: "darkcloud", State: "idle", Since: now.Add(-time.Minute)},
	}})
	rows := map[string][]string{}
	for _, line := range strings.Split(out.String(), "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			rows[f[0]] = f
		}
	}
	if got := strings.Join(rows["a3f9c1"], " "); got != "a3f9c1 darkcloud stopping e2e #412 1m0s" {
		t.Errorf("stopping row %q in:\n%s", got, out.String())
	}
	if got := strings.Join(rows["b7d2e0"], " "); got != "b7d2e0 darkcloud idle - 1m0s" {
		t.Errorf("idle row %q in:\n%s", got, out.String())
	}
	if !strings.Contains(out.String(), "global 2/3") {
		t.Errorf("a stopping runner must still count:\n%s", out.String())
	}
}

```

The file already imports `bytes`, `strings`, `time` and `model`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `timeout 300 go test -race ./cmd/ghr/ -run TestStatusPrintsStopping`
Expected: FAIL with `stopping row "a3f9c1 darkcloud busy e2e #412 1m0s"`.

- [ ] **Step 3: Print the flag in the STATE column**

In `cmd/ghr/cli.go`, replace

```go
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", i.ID, i.Repo, i.State, job, st.Now.Sub(i.Since).Round(time.Second))
```

with

```go
		state := i.State
		if i.Stopping {
			state = "stopping"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", i.ID, i.Repo, state, job, st.Now.Sub(i.Since).Round(time.Second))
```

Leave the `running` count above it as it is: it counts every instance that is not `cleaning`.

- [ ] **Step 4: Format, run the package and the Go checks**

```bash
gofmt -w cmd/ghr/cli.go cmd/ghr/cli_test.go
timeout 300 go test -race ./cmd/ghr/
gofmt -l internal cmd web/*.go
go vet ./internal/... ./cmd/... ./web/
```

Expected: one `ok` line, then no output.

- [ ] **Step 5: Commit**

```bash
git add cmd/ghr/cli.go cmd/ghr/cli_test.go
git commit -m "feat(cli): print stopping in ghr status"
```

### Task 5: Web types and run description

**Files:**
- Modify: `web/src/api/types.ts:15-22`, `web/src/api/types.ts:366-374`
- Modify: `web/src/lib/activity-view.ts:93`
- Test: `web/src/lib/activity-view.test.ts` (new test above `it("describes each bucket"`, line 77), `web/src/lib/summary.test.ts` (new block at the end of the file)

**Interfaces:**
- Consumes: nothing from an earlier task's code; the field names are the JSON names in Global Constraints.
- Produces: Contracts, "Web — `web/src` (Task 5)".

**Items:** 25
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Add the optional fields**

In `web/src/api/types.ts`, in `export interface InstanceStatus`, replace

```ts
  since: string
  job?: JobInfo
}
```

with

```ts
  since: string
  job?: JobInfo
  stopping?: boolean
}
```

and in `export interface ActivityRun`, replace

```ts
  segments: ActivitySegment[]
  html_url?: string
}
```

with

```ts
  segments: ActivitySegment[]
  html_url?: string
  stopping?: boolean
}
```

- [ ] **Step 2: Write the failing test**

In `web/src/lib/activity-view.test.ts`, replace

```ts
import type { ActivityBucket } from "@/api/types"
```

with

```ts
import type { ActivityBucket, ActivityRun } from "@/api/types"
```

and insert directly above `  it("describes each bucket", () => {`:

```ts
  it("says stopping for a stopping run in each live state", () => {
    const live = (state: string): ActivityRun => ({
      instance_id: "cccccc",
      repo: "darkmem",
      stopping: true,
      segments: [{ state, from: "2026-10-03T14:04:00Z", to: null }],
    })
    expect(["starting", "warm", "running"].map((s) => runAria(live(s), now))).toEqual([
      "darkmem, runner cccccc, stopping, 1m00s",
      "darkmem, runner cccccc, stopping, 1m00s",
      "darkmem, runner cccccc, stopping, 1m00s",
    ])
    const running = lanes.lanes.flatMap((l) => l.runs).find((r) => r.instance_id === "aaaaaa") as ActivityRun
    expect(runAria({ ...running, stopping: true }, now)).toBe("darkmem, ci, test, run 42, stopping, 4m00s")
  })

```

In `web/src/lib/summary.test.ts`, replace

```ts
import { dashboardSummary, runnersSummary } from "./summary"
```

with

```ts
import { capacity } from "./capacity"
import { dashboardSummary, runnersSummary } from "./summary"
```

and append at the end of the file:

```ts

describe("a stopping runner", () => {
  it("counts as it did before it was stopped", () => {
    const instances = [inst("busy", "a"), inst("busy", "b"), inst("starting", "c"), inst("idle", "d")].map((i) => ({ ...i, stopping: true }))
    const stopping = st({ global_max: 5, instances, repos: [repo({})] })
    expect(capacity(stopping)).toEqual({
      segments: ["busy", "busy", "starting", "warm", "free"],
      over: 0,
      busy: 2,
      text: "2 of 5 busy",
      label: "2 of 5 busy, 1 starting, 1 warm, 1 free",
    })
    expect(dashboardSummary(stopping)).toBe("2 of 5 runners busy.")
    expect(runnersSummary(stopping)).toBe("4 live")
  })
})
```

`inst`, `st` and `repo` are the helpers at the top of that file. The expected values are what these functions give the same four runners without the flag.

- [ ] **Step 3: Run the tests to verify the description test fails**

Run, from `web/`: `timeout 300 npx vitest run src/lib/activity-view.test.ts src/lib/summary.test.ts`
Expected: FAIL in "says stopping for a stopping run in each live state": the first value is `darkmem, runner cccccc, starting, 1m00s`. "counts as it did before it was stopped" PASSES already: it guards counts this plan must not change (see Assumptions).

- [ ] **Step 4: Say stopping in the run description**

In `web/src/lib/activity-view.ts`, in `runAria`, replace

```ts
  const state = last ? (RESULT[last.state] ?? last.state) : ""
```

with

```ts
  const state = run.stopping ? "stopping" : last ? (RESULT[last.state] ?? last.state) : ""
```

- [ ] **Step 5: Run the tests and the web checks**

Run, from `web/`:

```bash
timeout 300 npx vitest run src/lib/activity-view.test.ts src/lib/summary.test.ts
timeout 300 npm run typecheck
timeout 300 npm run lint
```

Expected: both test files pass; typecheck and lint print no errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/api/types.ts web/src/lib/activity-view.ts web/src/lib/activity-view.test.ts web/src/lib/summary.test.ts
git commit -m "feat(web): say stopping in run descriptions"
```

### Task 6: Runner list shows stopping

**Files:**
- Modify: `web/src/components/runner-list.tsx:4`, `web/src/components/runner-list.tsx:18`, `web/src/components/runner-list.tsx:89-104`
- Test: `web/src/components/runner-list.test.tsx` (new test above `it("marks the selected runner and makes only it tabbable"`, line 42)

**Interfaces:**
- Consumes: Contracts, "Web — `web/src` (Task 5)": `InstanceStatus.stopping`.
- Produces: nothing another task uses.

**Items:** 25
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing test**

In `web/src/components/runner-list.test.tsx`, insert directly above `  it("marks the selected runner and makes only it tabbable", async () => {`:

```tsx
  it("says a stopping runner is stopping, with a job and without", async () => {
    draw({ ...fixtures.status, instances: fixtures.status.instances.map((i) => ({ ...i, stopping: true })) })
    // The second line is the visible state; the sr-only text is for screen readers.
    const secondLine = (row: HTMLElement) => row.querySelector(".col-span-2")
    const busy = await link("aaaaaa")
    expect(secondLine(busy)).toHaveTextContent(/^Stopping, test #42$/)
    expect(busy.querySelector(".sr-only")).toHaveTextContent(/^Stopping$/)
    expect(busy).not.toHaveTextContent("Busy")
    const light = busy.querySelector("span")
    expect(light).toHaveClass("bg-muted-foreground")
    expect(light).not.toHaveClass("ghr-pulse")
    const idle = await link("bbbbbb")
    expect(secondLine(idle)).toHaveTextContent(/^Stopping$/)
    expect(idle.querySelector(".sr-only")).toHaveTextContent(/^Stopping$/)
    expect(idle).not.toHaveTextContent("Idle")
  })

```

In the fixture, `aaaaaa` is busy with the job `test` #42 and `bbbbbb` is idle with no job. The light is the first `span` inside the row's link, and the second line is the only element with the class `col-span-2`.

- [ ] **Step 2: Run the test to verify it fails**

Run, from `web/`: `timeout 300 npx vitest run src/components/runner-list.test.tsx`
Expected: FAIL in "says a stopping runner is stopping, with a job and without": the second line reads `test #42`.

- [ ] **Step 3: Show the flag in the row**

In `web/src/components/runner-list.tsx`, replace

```tsx
import type { Status } from "@/api/types"
```

with

```tsx
import type { InstanceStatus, Status } from "@/api/types"
```

Replace

```tsx
const sentence = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)
```

with

```tsx
const sentence = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

// A stopping runner has no light of its own: it takes the muted fallback.
const shown = (i: InstanceStatus) => (i.stopping ? "stopping" : i.state)
```

Replace

```tsx
${LIGHT[i.state] ?? "bg-muted-foreground"}
```

with

```tsx
${LIGHT[shown(i)] ?? "bg-muted-foreground"}
```

Replace

```tsx
                  <span className="sr-only">{sentence(i.state)}</span>
```

with

```tsx
                  <span className="sr-only">{sentence(shown(i))}</span>
```

Replace

```tsx
                    <>
                      {i.job.name} <span className="font-mono">#{i.job.run_number}</span>
                    </>
                  ) : (
                    <span aria-hidden="true">{sentence(i.state)}</span>
                  )}
```

with

```tsx
                    <>
                      {i.stopping && <span aria-hidden="true">Stopping, </span>}
                      {i.job.name} <span className="font-mono">#{i.job.run_number}</span>
                    </>
                  ) : (
                    <span aria-hidden="true">{sentence(shown(i))}</span>
                  )}
```

The visible "Stopping, " is hidden from assistive technology because the row's screen-reader text already says "Stopping".

- [ ] **Step 4: Run the test and the web checks**

Run, from `web/`:

```bash
timeout 300 npx vitest run src/components/runner-list.test.tsx
timeout 300 npm run typecheck
timeout 300 npm run lint
```

Expected: the test file passes; typecheck and lint print no errors.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/runner-list.tsx web/src/components/runner-list.test.tsx
git commit -m "feat(web): show stopping in the runner list"
```

### Task 7: Runner panel and stop dialog

**Files:**
- Modify: `web/src/components/runner-panel.tsx:136`, `web/src/components/runner-panel.tsx:146`
- Modify: `web/src/components/stop-runner-dialog.tsx:46-56`, `web/src/components/stop-runner-dialog.tsx:72`
- Test: `web/src/components/runner-panel.test.tsx` (new test above `it("keeps the last snapshot after the runner ends"`, line 124), `web/src/pages/runners.test.tsx` (new test above `it("toasts the runner it stopped"`, line 141)
- Create: `web/src/components/stop-runner-dialog.test.tsx`

**Interfaces:**
- Consumes: Contracts, "Web — `web/src` (Task 5)": `InstanceStatus.stopping`.
- Produces: nothing another task uses.

**Items:** 25
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

In `web/src/components/runner-panel.test.tsx`, insert directly above `  it("keeps the last snapshot after the runner ends", async () => {`:

```tsx
  it("says a stopping runner is stopping and offers no second stop", async () => {
    mockApi(
      routes({
        "GET /api/status": {
          ...fixtures.status,
          instances: fixtures.status.instances.map((i) => (i.id === "aaaaaa" ? { ...i, stopping: true } : i)),
        },
      }),
    )
    draw()
    expect(await screen.findByText("Stopping")).toHaveClass("text-muted-foreground")
    expect(screen.queryByText("Busy")).toBeNull()
    expect(screen.getByRole("button", { name: "Stop runner" })).toBeDisabled()
  })

```

In `web/src/pages/runners.test.tsx`, insert directly above `  it("toasts the runner it stopped", async () => {`:

```tsx
  it("says a runner that starts stopping while the dialog is open is already stopping", { timeout: 10_000 }, async () => {
    let stopping = false
    const { calls } = mockApi(
      routes({
        "GET /api/status": () => ({
          ...fixtures.status,
          instances: fixtures.status.instances.map((i) => (stopping && i.id === "bbbbbb" ? { ...i, stopping: true } : i)),
        }),
      }),
    )
    const { user } = renderApp("/runners/bbbbbb")
    await user.click(await screen.findByRole("button", { name: "Stop runner" }))
    expect(await screen.findByText("Stop runner bbbbbb?")).toBeInTheDocument()
    stopping = true
    const ask = within(screen.getByRole("alertdialog"))
    expect(await ask.findByText("Runner bbbbbb is already stopping")).toBeInTheDocument()
    expect(ask.getByText("ghr is stopping it and will clean it up.")).toBeInTheDocument()
    expect(ask.getByRole("button", { name: "Stop" })).toBeDisabled()
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)
  })

```

Create `web/src/components/stop-runner-dialog.test.tsx` with exactly this content:

```tsx
import { render, screen, waitFor } from "@testing-library/react"
import type { ButtonHTMLAttributes, ReactNode } from "react"
import { describe, expect, it, vi } from "vitest"
import { keys } from "@/api/hooks"
import type { InstanceStatus, Status } from "@/api/types"
import { mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { StopRunnerDialog } from "./stop-runner-dialog"

// The kit keeps a closing dialog's content mounted while its exit animation
// runs. jsdom runs no animation, so this stand-in renders the content whether
// the dialog is open or not.
vi.mock("darkraise-ui/components/alert-dialog", () => {
  const Box = ({ children }: { children?: ReactNode }) => <div>{children}</div>
  const Btn = ({ children, ...rest }: ButtonHTMLAttributes<HTMLButtonElement>) => <button {...rest}>{children}</button>
  return {
    AlertDialog: Box,
    AlertDialogContent: Box,
    AlertDialogHeader: Box,
    AlertDialogFooter: Box,
    AlertDialogTitle: ({ children }: { children?: ReactNode }) => <h2>{children}</h2>,
    AlertDialogDescription: ({ children }: { children?: ReactNode }) => <p>{children}</p>,
    AlertDialogCancel: Btn,
    AlertDialogAction: Btn,
  }
})

const idle = fixtures.status.instances.find((i) => i.id === "bbbbbb") as InstanceStatus
const nowStopping: Status = {
  ...fixtures.status,
  instances: fixtures.status.instances.map((i) => (i.id === "bbbbbb" ? { ...i, stopping: true } : i)),
}

describe("StopRunnerDialog", () => {
  it("keeps its wording while it closes, and says already stopping while open", async () => {
    let stopping = false
    mockApi(authedRoutes({ "GET /api/status": () => (stopping ? nowStopping : fixtures.status) }))
    const { queryClient, wrapper } = withQuery()
    const { rerender } = render(<StopRunnerDialog instance={idle} onClose={() => {}} />, { wrapper })
    expect(screen.getByRole("heading")).toHaveTextContent("Stop runner bbbbbb?")
    await waitFor(() => expect(queryClient.getQueryData(keys.status)).toEqual(fixtures.status))

    // Closing: the runner turns up as stopping, and the wording must not move.
    rerender(<StopRunnerDialog instance={null} onClose={() => {}} />)
    stopping = true
    await queryClient.invalidateQueries({ queryKey: keys.status })
    await waitFor(() => expect(queryClient.getQueryData(keys.status)).toEqual(nowStopping))
    expect(screen.getByRole("heading")).toHaveTextContent("Stop runner bbbbbb?")
    expect(screen.getByText("ghr stops the runner and cleans it up.")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Stop" })).toBeEnabled()

    // Open over the same runner: now it says so and offers no second stop.
    rerender(<StopRunnerDialog instance={idle} onClose={() => {}} />)
    expect(screen.getByRole("heading")).toHaveTextContent("Runner bbbbbb is already stopping")
    expect(screen.getByText("ghr is stopping it and will clean it up.")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Stop" })).toBeDisabled()
  })
})
```

The mock stands in for the kit's dialog so that a closed dialog's content stays in the document, as it does in a browser while the exit animation runs. The two existing test files already import everything their new tests use (`within`, `mockApi` and `fixtures`).

- [ ] **Step 2: Run the tests to verify they fail**

Run, from `web/`: `timeout 300 npx vitest run src/components/runner-panel.test.tsx src/pages/runners.test.tsx src/components/stop-runner-dialog.test.tsx`
Expected: three FAILs: "says a stopping runner is stopping and offers no second stop", "says a runner that starts stopping while the dialog is open is already stopping" and "keeps its wording while it closes, and says already stopping while open" (its last block finds `Stop runner bbbbbb?` where it expects `Runner bbbbbb is already stopping`).

- [ ] **Step 3: Show the flag in the panel**

In `web/src/components/runner-panel.tsx`, replace

```tsx
{finished ? <StateText state="finished" /> : inst && <StateText state={inst.state} />}
```

with

```tsx
{finished ? <StateText state="finished" /> : inst && <StateText state={inst.stopping ? "stopping" : inst.state} />}
```

and replace

```tsx
<Button variant="destructive" disabled={!live || status.isError} onClick={() => setStopping(true)}>
```

with

```tsx
<Button variant="destructive" disabled={!live || status.isError || current.stopping === true} onClick={() => setStopping(true)}>
```

`live` is `current !== undefined`, so TypeScript narrows `current` after `!live ||`. The component's own `stopping` state means "the dialog is open"; do not rename it.

- [ ] **Step 4: Word the dialog for a stopping runner**

In `web/src/components/stop-runner-dialog.tsx`, replace

```tsx
  const busy = live?.state === "busy"
  let title = `Stop runner ${live?.id}?`
  let description = "ghr stops the runner and cleans it up."
  if (gone) {
    title = `Runner ${live?.id} has already finished`
    description = "There is nothing left to stop."
  } else if (busy) {
```

with

```tsx
  const busy = live?.state === "busy"
  // Only while open: a dialog closing after its own Stop keeps its wording
  // when the runner turns up as stopping.
  const stopping = instance !== null && current?.stopping === true
  let title = `Stop runner ${live?.id}?`
  let description = "ghr stops the runner and cleans it up."
  if (gone) {
    title = `Runner ${live?.id} has already finished`
    description = "There is nothing left to stop."
  } else if (stopping) {
    title = `Runner ${live?.id} is already stopping`
    description = "ghr is stopping it and will clean it up."
  } else if (busy) {
```

and replace

```tsx
            disabled={gone}
```

with

```tsx
            disabled={gone || stopping}
```

- [ ] **Step 5: Run the tests and the web checks**

Run, from `web/`:

```bash
timeout 300 npx vitest run src/components/runner-panel.test.tsx src/pages/runners.test.tsx src/components/stop-runner-dialog.test.tsx
timeout 300 npm run typecheck
timeout 300 npm run lint
```

Expected: all three test files pass; typecheck and lint print no errors.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/runner-panel.tsx web/src/components/stop-runner-dialog.tsx web/src/components/runner-panel.test.tsx web/src/pages/runners.test.tsx web/src/components/stop-runner-dialog.test.tsx
git commit -m "feat(web): show stopping in panel and dialog"
```

### Task 8: Lanes chart and legend

**Files:**
- Modify: `web/src/components/lanes-chart.tsx:24`, `web/src/components/lanes-chart.tsx:50-77`
- Modify: `web/src/components/activity-panel.tsx:39`
- Test: `web/src/components/lanes-chart.test.tsx` (new test above `it("links a finished job to GitHub"`, line 49), `web/src/components/activity-panel.test.tsx:32`

**Interfaces:**
- Consumes: Contracts, "Web — `web/src` (Task 5)": `ActivityRun.stopping` and `runAria`'s `stopping` word.
- Produces: nothing another task uses.

**Items:** 25
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

In `web/src/components/lanes-chart.test.tsx`, insert directly above `  it("links a finished job to GitHub", () => {`:

```tsx
  it("draws a stopping run as a dashed grey outline with no pulse", () => {
    const lanes = fixtures.activityLanes.lanes.map((l) => ({
      runs: l.runs.map((r) =>
        r.instance_id === "aaaaaa"
          ? { ...r, stopping: true, segments: [{ state: "running", from: "2026-10-03T13:45:00Z", to: null }] }
          : r.instance_id === "bbbbbb"
            ? { ...r, stopping: true }
            : r,
      ),
    }))
    const { container } = draw({ lanes })
    for (const state of ["running", "warm"]) {
      const box = container.querySelector(`rect[data-state="${state}"]`)
      expect(box).toHaveClass("fill-none", "stroke-muted-foreground")
      expect(box).not.toHaveClass("fill-primary", "stroke-primary")
      expect(box).toHaveAttribute("stroke-dasharray", "3 2")
    }
    expect(container.querySelector(".ghr-pulse")).toBeNull()
    expect(screen.getByText("darkmem test #42")).toHaveClass("fill-foreground")
    expect(screen.getByRole("link", { name: "darkmem, ci, test, run 42, stopping, 20m00s" })).toBeInTheDocument()
    expect(container.querySelector('rect[data-state="succeeded"]')).not.toHaveAttribute("stroke-dasharray")
  })

```

The running run is stretched back to 13:45 so its bar is wide enough to carry its text label.

In `web/src/components/activity-panel.test.tsx`, in the test "shows the range, a lanes legend and the lanes for 1h", replace

```tsx
["Succeeded", "Failed", "Unknown", "Running", "Finishing", "Warm", "Starting", "Jobs waiting", "Memory"]
```

with

```tsx
["Succeeded", "Failed", "Unknown", "Running", "Finishing", "Warm", "Starting", "Stopping", "Jobs waiting", "Memory"]
```

- [ ] **Step 2: Run the tests to verify they fail**

Run, from `web/`: `timeout 300 npx vitest run src/components/lanes-chart.test.tsx src/components/activity-panel.test.tsx`
Expected: two FAILs: "draws a stopping run as a dashed grey outline with no pulse" and "shows the range, a lanes legend and the lanes for 1h".

- [ ] **Step 3: Draw the stopping segment**

In `web/src/components/lanes-chart.tsx`, replace

```tsx
type Point = { x: number; y: number }
```

with

```tsx
// The open segment of a run being stopped, whatever its state: the only
// unfilled dashed grey outline in the chart.
const STOPPING = "fill-none stroke-muted-foreground"

type Point = { x: number; y: number }
```

Replace

```tsx
        const x2 = x(s.to === null ? now : Date.parse(s.to))
        return (
```

with

```tsx
        const x2 = x(s.to === null ? now : Date.parse(s.to))
        const stopping = run.stopping === true && s.to === null
        return (
```

Replace

```tsx
            strokeDasharray={s.state === "warm" || s.state === "unknown" ? "3 2" : undefined}
            className={`ghr-mark-box ${SEGMENT[s.state] ?? "fill-muted stroke-muted-foreground"}`}
```

with

```tsx
            strokeDasharray={stopping || s.state === "warm" || s.state === "unknown" ? "3 2" : undefined}
            className={`ghr-mark-box ${stopping ? STOPPING : (SEGMENT[s.state] ?? "fill-muted stroke-muted-foreground")}`}
```

Replace

```tsx
      {last?.state === "running" && last.to === null && (
```

with

```tsx
      {last?.state === "running" && last.to === null && !run.stopping && (
```

Replace

```tsx
className={`pointer-events-none ${last?.state === "running" ? "fill-primary-foreground" : "fill-foreground"}`}
```

with

```tsx
className={`pointer-events-none ${last?.state === "running" && !run.stopping ? "fill-primary-foreground" : "fill-foreground"}`}
```

Leave `data-state` as the segment's state.

- [ ] **Step 4: Add the legend entry**

In `web/src/components/activity-panel.tsx`, replace

```tsx
        ["Starting", <Swatch className="fill-none stroke-muted-foreground" />],
```

with

```tsx
        ["Starting", <Swatch className="fill-none stroke-muted-foreground" />],
        ["Stopping", <Swatch className="fill-none stroke-muted-foreground" dash />],
```

- [ ] **Step 5: Run the whole web suite and the web checks**

Run, from `web/`:

```bash
timeout 600 npm test
timeout 300 npm run typecheck
timeout 300 npm run lint
```

Expected: every test file passes (87 files); typecheck and lint print no errors.

- [ ] **Step 6: Run the Go suites the feature touches**

Run, from the repository root:

```bash
timeout 600 go test -race ./internal/runner/ ./internal/model/ ./internal/daemon/ ./internal/activity/ ./internal/api/ ./cmd/ghr/ ./web/
```

Expected: seven `ok` lines.

- [ ] **Step 7: Commit**

```bash
git add web/src/components/lanes-chart.tsx web/src/components/activity-panel.tsx web/src/components/lanes-chart.test.tsx web/src/components/activity-panel.test.tsx
git commit -m "feat(web): draw stopping runs in the lanes"
```
