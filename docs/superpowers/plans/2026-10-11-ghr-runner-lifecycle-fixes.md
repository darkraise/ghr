# Runner Lifecycle and Scheduler Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close register rows 1 to 18, 175, 176 and 177: stop ghr from stopping healthy runners, keeping broken ones and letting one repository's failure stop the others, bound its retry loops, and stop root from trusting files a job can write.

**Architecture:** Targeted fixes inside `internal/runner` and `internal/sched`, in the order the tick runs them. `internal/sched` stays pure and learns one flag, `Unusable`; everything that needs the clock or the API stays in `internal/runner`. Records root must trust move to a new root-only directory.

**Tech Stack:** Go 1.26 (standard library only), bash for `deploy/setup.sh` and its test.

**Spec:** docs/superpowers/specs/2026-10-11-ghr-runner-lifecycle-fixes-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 9 of 20 tasks are heavy (3, 4, 5, 7, 8, 11, 15, 16, 17) and are delegated; the other 11 run in the session

**Plan review:** 2026-10-11 — dr-superpowers:judge-opus — executability 14 / coherence 18 / coverage 18 / assumptions 15 (round 2)

**Parallelism:** sequential — nearly every task edits `tick.go`, `lifecycle.go` or `manager.go` in one package, and later tasks use fields earlier ones add

## Global Constraints

- Go is at `/usr/local/go/bin` and not on PATH: prefix every Go command with `PATH=/usr/local/go/bin:$PATH`.
- Before each commit run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/` (must print nothing) and the test command the task names. A task that touches `internal/runner` also runs `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -count=1`.
- Tests use the package's fakes (`fakeGH`, `fakeSD`, `fakeDocker`, `fakeHost`) and the harness clock `h.now`. No test sleeps.
- Event texts are part of the spec. Copy them exactly as the task gives them.
- When an existing test fails only because it put a runner in the idle state by hand while the fake GitHub does not report that runner online, fix the test's setup with `h.gh.setRunner(<runner id>, "online", false)`. Report any other existing-test failure in the task report; never change production code to make an old expectation pass.
- Code comments: only where the reason is not obvious from the code. No comment mentions a task, a register row or a fix.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no trailing period, no attribution or co-author lines.
- Never deploy to the runner LXC (192.168.0.99). No change under `web/`.
- English only.

## Contracts

Names one task adds and a later task uses. All are in package `runner` unless marked.

- `sched.Instance.Unusable bool` (Task 2): the runner can take no job. It does not cover a queued job, is not counted as starting or idle for the warm count, and is always in `IdleToStop` when idle. It still counts toward the repository cap and `global_max`.
- `func (m *Manager) enterLocked(i *instance, s sched.State, now time.Time)` (Task 3): sets `State` and `StateSince`; `m.mu` must be held. Sets `i.cameOnline` when `s` is idle or busy.
- `func (m *Manager) move(id string, to sched.State, from ...sched.State) bool` (Task 3): moves the live instance to `to` only when its live state is one of `from`.
- `instance.cameOnline bool` (Task 3): the instance has been idle or busy at least once.
- `func (m *Manager) beginTick()` (Task 4): called by `Tick` right after `m.ticks++`. Clears every instance's `answered`; from Task 5 it also resets `m.lists`.
- `func (m *Manager) markAnswered(id string)` and `instance.answered bool` (Task 4): the runners API answered for the instance in the current tick.
- `func (m *Manager) registrationGone(ctx context.Context, repo string, id int64, now time.Time) bool` (Task 5).
- `instance.lost bool` (Task 5): the registration was confirmed missing, not deleted by ghr. `instance.deregistered` then also holds.
- Test helper `func idleOnline(t *testing.T, h *harness, repo string)` in `internal/runner/lifecycle_test.go` (Task 5): spawns the harness's first runner for `repo` (instance `aaaaaa`, runner ID 101), reports it online in the fake GitHub, runs `refreshRunners`, and fails the test unless the instance is idle.
- `instance.offlineSince time.Time` (Task 6): when an idle runner was first seen offline; zero while online.
- `func tokenRejected(err error) bool` (Task 7): true only for a `*github.APIError` with `Kind == github.ErrAuth` and `Status == 401`.
- `func permissionText(ae *github.APIError) string` (Task 7): `token lacks permission for this repository (needs <permissions>): <ae.Error()>`, without the parenthesis when `ae.Permissions` is empty.
- `func (m *Manager) spawnFailed(repo, msg string, now time.Time) time.Duration` (Task 8): records a failed start and returns the wait before the next attempt.
- `func (m *Manager) clearSpawnBackoffLocked(repo string)` (Task 8); maps `m.spawnFails`, `m.spawnRetryAt`, `m.spawnErr`, keyed by the lower-cased repository name; `const maxSpawnBackoff = 10 * time.Minute`.
- `instance.adopted bool` (Task 8): rebuilt by `Adopt`, not spawned by this process.
- `gatherDemand` returns `(allAnswered bool, unanswered string)` (Task 9); `unanswered` is the first unpaused repository in config order that did not answer, `""` when all did.
- `Paths.Private string` (Task 14): root-only directory, `/var/lib/ghr/private` on the daemon, mode 0700.
- `func (m *Manager) privateMeta(id string) string` returns `<Private>/<id>.json`; `func (m *Manager) privateProjects(id string) string` returns `<Private>/<id>.projects`; `func (m *Manager) removePrivate(id string)` deletes both (Task 14).

## Assumptions (evidence)

- `systemctl stop` of a unit that is not loaded exits 5 and prints `Failed to stop X.service: Unit X.service not loaded.` Evidence: `systemctl stop ghr-runner-zzzzzz.service; echo $?` on the development host, systemd 257, 2026-10-11. The runner LXC's systemd version is unverified — Task 20 lists it for the owner's live check.
- `system.Exec` wraps the `*exec.ExitError` with `%w` and appends stderr to the error text. Evidence: `internal/system/system.go:52-71`.
- The first 400 entries of the official runner tarball are all owned by 1001:1001; the rest of the archive was not listed. Evidence: `curl -fsSL https://github.com/actions/runner/releases/download/v2.330.0/actions-runner-linux-x64-2.330.0.tar.gz | tar -tvz --numeric-owner | head -400 | awk '{print $2}' | sort | uniq -c` printed `400 1001/1001` on 2026-10-11. Task 18's flag and ownership repair do not depend on the remainder. The runner user's uid on the runner LXC is unverified — Task 20 lists `id -u ghrunner` for the owner.
- `Host.Extract` is called only by `internal/runner/install.go:123`; toolchain installs build their own tar command. Evidence: `rg -n 'Extract' internal --glob '*.go'`, 2026-10-11.
- The UI kit 6.9.6 ships a global reduced-motion rule. Evidence: `web/node_modules/darkraise-ui/dist/styles.css:13453-13461`.
- `os.Root` with `Lstat`, `OpenFile` and `FS` exists in the installed toolchain, and `syscall.O_NONBLOCK` is defined on Linux and Windows. Evidence: `/usr/local/go/src/os/root.go:117,209,357`; `/usr/local/go/src/syscall/types_windows.go:50`; `go version` prints go1.26.1.
- `os.Root` opens every path component with `O_NOFOLLOW` and resolves symlinks itself, refusing any that leave the root; a symlink that stays inside the root is followed. Evidence: `/usr/local/go/src/os/root_unix.go:67-117`. Task 17 therefore roots reads at the log directory itself.
- `(*os.Root).OpenRoot` exists. Evidence: `/usr/local/go/src/os/root.go:132`.
- A command started through `system.Exec` ends when its caller's context ends: `execute` derives its timeout context from the caller's and passes it to `exec.CommandContext`. Evidence: `internal/system/system.go:49-52`. Task 11's 15-second bound on the disk reading relies on it.
- Reading a path below a regular file fails with ENOTDIR, which is not `fs.ErrNotExist`; `os.ReadFile` of a missing file in an existing directory is. Evidence: Go's `syscall.Errno.Is` maps only ENOENT to `ErrNotExist`. Task 16's tests therefore fail writes through the `writeFile` test seam instead of using an impossible directory.
- The fake GitHub in runner tests answers 404 for a runner ID it does not hold, registers a new runner as `offline`, and lists `listed[repo]`. Evidence: `internal/runner/fakes_test.go:88-132`.
- A test harness starts with `ticks == 0`, so its first `Tick` is a disk-check tick (`ticks%10 == 1`). Evidence: `internal/runner/tick.go:48`.
- `deploy/tests/setup_test.sh` runs without root and replaces commands with shell functions. Evidence: its header comment, lines 2-3.
- The code blocks in this plan were written against the tree at `53f76ae` and were not compiled. Each task's own test run is the check; a block that does not compile is corrected to the same behaviour and noted in the task report.

## Task index

1. Systemd stop of a unit that is gone
2. Unusable runners in the scheduler
3. Guarded state changes
4. Start timeout needs an answer
5. A registration that is gone
6. Idle runner offline too long
7. A 403 stops one repository
8. Spawn backoff
9. Why a queued update waits
10. Status during backoff and pause
11. Docker prunes off the tick
12. Cleanup count and give-up event
13. Reconcile and other hosts
14. The root-only directory
15. Compose project list moves
16. Instance metadata moves
17. Runner log reader
18. Archive ownership
19. Docs and housekeeping
20. Full verification and live-check request

---

### Task 1: Systemd stop of a unit that is gone

**Files:**
- Modify: `internal/system/system.go:108-111`
- Test: `internal/system/system_test.go`, `internal/runner/update_start_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names

**Items:** 6, 18

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

- [ ] **Step 1: Write the failing test**

Append to `internal/system/system_test.go`:

```go
// systemd answers a stop of a unit it no longer has with exit status 5 and
// "not loaded". The unit is stopped, so that is not a failure.
func TestSystemdStopOfAnUnloadedUnitSucceeds(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	script := func(s string) Systemd {
		return Systemd{Run: func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
			return Exec(ctx, "sh", "-c", s)
		}}
	}
	gone := script("echo 'Failed to stop ghr-runner-gone00.service: Unit ghr-runner-gone00.service not loaded.' >&2; exit 5")
	if err := gone.Stop(context.Background(), "ghr-runner-gone00"); err != nil {
		t.Fatalf("a unit that is not loaded: %v", err)
	}
	for name, s := range map[string]string{
		"another exit status": "echo 'Unit x.service not loaded.' >&2; exit 1",
		"another message":     "echo 'Failed to stop x.service: Access denied' >&2; exit 5",
	} {
		if err := script(s).Stop(context.Background(), "ghr-runner-busy00"); err == nil {
			t.Fatalf("%s must stay an error", name)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/system/ -run TestSystemdStopOfAnUnloadedUnitSucceeds -count=1`
Expected: FAIL with `a unit that is not loaded: sh -c: exit status 5: Failed to stop ...`

- [ ] **Step 3: Write the implementation**

In `internal/system/system.go` replace the `Stop` method with:

```go
// Stop stops a unit. A unit systemd no longer has is stopped already;
// systemctl answers that with exit status 5 and "not loaded".
func (s Systemd) Stop(ctx context.Context, unit string) error {
	_, err := s.Run(ctx, "systemctl", "stop", unit)
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 5 && strings.Contains(err.Error(), "not loaded") {
		return nil
	}
	return err
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/system/ -count=1`
Expected: PASS

- [ ] **Step 5: Pin the runner-level behaviour**

The runner tests' fake systemd already succeeds when asked to stop an inactive unit, which is now what the real wrapper does. Append this guard to `internal/runner/update_start_test.go`. It passes before and after this task: no runner test could fail for this defect, because the defect was in the wrapper, which Step 1's test covers. What it pins is that the tick really does stop the unit twice, so the wrapper's answer to the second stop is what decides whether the update starts.

```go
// The tick's idle stop and the update start of the same tick both stop the
// same idle runner. The second stop finds the unit gone and must not postpone
// the update.
func TestUpdateStartsInTheTickThatStoppedItsIdleRunner(t *testing.T) {
	h := newHarness(t)
	queuedUpdate(t, h, tarballSum())
	id := idleRunner(t, h)
	h.now = h.now.Add(6 * time.Minute) // past idle_timeout
	h.m.Tick(context.Background())
	h.m.Wait()
	if len(h.sd.stopped) != 2 || h.sd.stopped[0] != UnitPrefix+id || h.sd.stopped[1] != UnitPrefix+id {
		t.Fatalf("stopped %v, want the idle stop and then the update's own stop", h.sd.stopped)
	}
	if strings.Contains(h.eventText(), "runner update postponed") || current(t, h) != "2.338.0" {
		t.Fatalf("current %s\n%s", current(t, h), h.eventText())
	}
}
```

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -run TestUpdateStartsInTheTickThatStoppedItsIdleRunner -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/system/system.go internal/system/system_test.go internal/runner/update_start_test.go
git commit -m "fix(system): treat a stop of a gone unit as done"
```

### Task 2: Unusable runners in the scheduler

**Files:**
- Modify: `internal/sched/sched.go`
- Test: `internal/sched/sched_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `sched.Instance.Unusable` (Contracts)

**Items:** 7

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

Append to `internal/sched/sched_test.go`:

```go
// A runner that can take no job (its registration is gone, or it has been
// offline too long) must not hold a queued job back or count as warm, and it
// is always stopped.
func TestUnusableInstanceTakesNoJob(t *testing.T) {
	c := cfg("all", 4, config.Repo{Name: "a", Warm: ptr(1)})
	insts := []Instance{{ID: "x", Repo: "a", State: Idle, StateSince: t0, Unusable: true}}
	if n := Covering(insts, "a", t0); n != 0 {
		t.Fatalf("covering %d", n)
	}
	if got := repos(Plan(c, insts, Demand{}, t0)); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("warm replacement not planned: %v", got)
	}
	queue := cfg("queue", 4, config.Repo{Name: "a", Max: ptr(2)}) // it still holds one of the two slots
	if got := repos(Plan(queue, insts, Demand{"a": jobs("a", 1)}, t0)); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("queued job still counted as covered: %v", got)
	}
	if got := IdleToStop(c, insts, t0); !reflect.DeepEqual(got, []string{"x"}) {
		t.Fatalf("idle to stop %v", got)
	}
	// Until its unit is down it still uses the host, so it keeps its slot.
	repoFull := cfg("queue", 4, config.Repo{Name: "a", Max: ptr(1)})
	if got := Plan(repoFull, insts, Demand{"a": jobs("a", 1)}, t0); len(got) != 0 {
		t.Fatalf("spawned past the repository cap: %v", got)
	}
	hostFull := cfg("queue", 1, config.Repo{Name: "a", Max: ptr(2)})
	if got := Plan(hostFull, insts, Demand{"a": jobs("a", 1)}, t0); len(got) != 0 {
		t.Fatalf("spawned past global_max: %v", got)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/sched/ -run TestUnusableInstanceTakesNoJob -count=1`
Expected: FAIL to compile with `unknown field Unusable in struct literal`

- [ ] **Step 3: Write the implementation**

In `internal/sched/sched.go`, add the field to `Instance`, after `Stale`:

```go
	// Unusable marks a runner that can take no job: its registration is gone,
	// or it has been offline too long. It still occupies its slot until its
	// unit is down.
	Unusable bool
```

In `covers`, replace `if i.Stale {` with:

```go
	if i.Stale || i.Unusable {
```

In `countStates`, replace `if !strings.EqualFold(i.Repo, repo) || i.Stale {` with:

```go
		if !strings.EqualFold(i.Repo, repo) || i.Stale || i.Unusable {
```

and change its comment to `// countStates counts the repo's instances in the given states that can take a job.`

In `IdleToStop`, replace `if i.Stale {` with:

```go
		if i.Stale || i.Unusable {
```

and change the function comment's last clause from `and every stale idle instance or idle instance of a paused (or unconfigured) repo.` to `and every stale or unusable idle instance or idle instance of a paused (or unconfigured) repo.`

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/sched/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sched/sched.go internal/sched/sched_test.go
git commit -m "feat(sched): add the unusable instance flag"
```

### Task 3: Guarded state changes

**Files:**
- Modify: `internal/runner/manager.go` (the `instance` struct and `setState`), `internal/runner/lifecycle.go` (`readJobFiles`, `refreshRunners`, `deregisterIdle`), `internal/runner/tick.go` (`confirm`)
- Test: `internal/runner/lifecycle_test.go`, `internal/runner/update_start_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `enterLocked`, `move`, `instance.cameOnline` (Contracts)

**Items:** 8

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing test**

Append to `internal/runner/lifecycle_test.go`:

```go
// The runners API is slow. A runner that took a job while its answer was on
// the way must not be put back to idle by that answer.
func TestRunnerMadeBusyDuringTheCheckStaysBusy(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.gh.setRunner(101, "online", false)
	h.gh.hook = func(method string) {
		if method == "GetRunner" {
			h.m.mu.Lock()
			h.m.insts["aaaaaa"].State = sched.Busy
			h.m.mu.Unlock()
		}
	}
	h.m.refreshRunners(context.Background())
	if h.state("aaaaaa") != "busy" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -run TestRunnerMadeBusyDuringTheCheckStaysBusy -count=1`
Expected: FAIL with `state idle`

- [ ] **Step 3: Write the implementation**

In `internal/runner/manager.go`, add to the `instance` struct after `stopping`:

```go
	cameOnline   bool      // it has been idle or busy at least once
```

Replace the whole `setState` method with:

```go
// enterLocked puts i in state s; m.mu must be held.
func (m *Manager) enterLocked(i *instance, s sched.State, now time.Time) {
	i.State = s
	i.StateSince = now
	if s == sched.Idle || s == sched.Busy {
		i.cameOnline = true
	}
}

// move puts the instance in state to when its live state is one of from, and
// reports whether it did. Callers decide from a snapshot that a network call
// has made old, so only the live state counts.
func (m *Manager) move(id string, to sched.State, from ...sched.State) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.insts[id]
	if !ok || !slices.Contains(from, i.State) {
		return false
	}
	m.enterLocked(i, to, m.Now())
	return true
}
```

In `internal/runner/lifecycle.go`, in `readJobFiles`, replace

```go
			if inst.State == sched.Starting || inst.State == sched.Idle {
				inst.State = sched.Busy
				inst.StateSince = m.Now()
			}
```

with

```go
			if inst.State == sched.Starting || inst.State == sched.Idle {
				m.enterLocked(inst, sched.Busy, m.Now())
			}
```

In `refreshRunners`, replace

```go
		if r.Busy {
			m.setState(i.ID, sched.Busy)
		} else if r.Status == "online" && i.State == sched.Starting {
			m.setState(i.ID, sched.Idle)
		}
```

with

```go
		if r.Busy {
			m.move(i.ID, sched.Busy, sched.Starting, sched.Idle)
		} else if r.Status == "online" {
			m.move(i.ID, sched.Idle, sched.Starting)
		}
```

In `deregisterIdle`, replace both occurrences of `m.setState(i.ID, sched.Busy)` with:

```go
			m.move(i.ID, sched.Busy, sched.Starting, sched.Idle)
```

(keep each line's own indentation).

In `internal/runner/tick.go`, in `confirm`, replace

```go
	if i.State != sched.Busy {
		i.State = sched.Busy
		i.StateSince = now
	}
```

with

```go
	if i.State != sched.Busy {
		m.enterLocked(i, sched.Busy, now)
	}
```

- [ ] **Step 4: Update the tests that called `setState`**

In `internal/runner/update_start_test.go`:
- in `idleRunner`, replace `h.m.setState(id, sched.Idle)` with `h.m.move(id, sched.Idle, sched.Starting)`;
- in `TestQueuedUpdateWaitsUntilFree`, replace `h.m.setState(id, sched.Busy)` with `h.m.move(id, sched.Busy, sched.Idle)`;
- in `TestUpdateBacksOffWhenAnIdleRunnerCannotStop`, replace `h.m.setState("bbbbbb", sched.Idle)` with `h.m.move("bbbbbb", sched.Idle, sched.Starting)`.

Run `rg -n 'setState\(' internal/` and expect no output.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runner/manager.go internal/runner/lifecycle.go internal/runner/tick.go internal/runner/lifecycle_test.go internal/runner/update_start_test.go
git commit -m "fix(runner): never move a busy runner back to idle"
```

### Task 4: Start timeout needs an answer

**Files:**
- Modify: `internal/runner/manager.go` (the `instance` struct, new helpers), `internal/runner/tick.go` (`Tick`), `internal/runner/lifecycle.go` (`refreshRunners`, `stopStartTimedOut`)
- Test: `internal/runner/lifecycle_test.go`, `internal/runner/adopt_test.go`

**Interfaces:**
- Consumes: `move` (Contracts)
- Produces: `beginTick`, `markAnswered`, `instance.answered` (Contracts)

**Items:** 1, 18

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing tests**

Append to `internal/runner/lifecycle_test.go`:

```go
// A runner that could not be checked may be online. Only an answer from the
// runners API in this tick lets the start timeout stop it.
func TestStartTimeoutWaitsForAnAnswerFromTheRunnersAPI(t *testing.T) {
	for name, block := range map[string]func(h *harness) (unblock func()){
		"API pause": func(h *harness) func() {
			h.m.apiErr("darkmem", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(10 * time.Minute)}, h.now)
			return func() { h.now = h.now.Add(10 * time.Minute) }
		},
		"failed call": func(h *harness) func() {
			h.gh.setErr("GetRunner darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
			return func() { h.gh.setErr("GetRunner darkmem", nil) }
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
				t.Fatal(err)
			}
			unblock := block(h)
			h.now = h.now.Add(3 * time.Minute) // past start_timeout
			h.m.Tick(context.Background())
			if len(h.sd.stopped) != 0 {
				t.Fatalf("stopped without an answer: %v\n%s", h.sd.stopped, h.eventText())
			}
			unblock()
			h.m.Tick(context.Background())
			if !reflect.DeepEqual(h.sd.stopped, []string{"ghr-runner-aaaaaa"}) ||
				!strings.Contains(h.eventText(), "did not come online within 2m0s") {
				t.Fatalf("stopped %v\n%s", h.sd.stopped, h.eventText())
			}
		})
	}
}
```

Append to `internal/runner/adopt_test.go`:

```go
// An adopted runner starts as "starting" from its spawn time. When the first
// check after the restart fails, an online runner must survive until a check
// succeeds.
func TestAdoptedOnlineRunnerSurvivesAFailedFirstCheck(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkmem", 1, false)
	h.sd.active["ghr-runner-aaaaaa"] = true
	h.gh.setRunner(1, "online", false)
	h.now = h.now.Add(3 * time.Minute) // spawned 3m ago; start_timeout is 2m
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.gh.setErr("GetRunner darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
	h.m.Tick(context.Background())
	if len(h.sd.stopped) != 0 || h.state("aaaaaa") != "starting" {
		t.Fatalf("stopped %v state %s", h.sd.stopped, h.state("aaaaaa"))
	}
	h.gh.setErr("GetRunner darkmem", nil)
	h.m.Tick(context.Background())
	if len(h.sd.stopped) != 0 || h.state("aaaaaa") != "idle" {
		t.Fatalf("stopped %v state %s", h.sd.stopped, h.state("aaaaaa"))
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -run 'TestStartTimeoutWaitsForAnAnswerFromTheRunnersAPI|TestAdoptedOnlineRunnerSurvivesAFailedFirstCheck' -count=1`
Expected: FAIL, each with `stopped without an answer` or `stopped [ghr-runner-aaaaaa] state starting`

- [ ] **Step 3: Write the implementation**

In `internal/runner/manager.go`, add to the `instance` struct after `cameOnline`:

```go
	answered     bool      // the runners API answered for it in the current tick
```

Add after `move`:

```go
// beginTick forgets what the last tick learned from the runners API.
func (m *Manager) beginTick() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, i := range m.insts {
		i.answered = false
	}
}

func (m *Manager) markAnswered(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i, ok := m.insts[id]; ok {
		i.answered = true
	}
}
```

In `internal/runner/tick.go`, in `Tick`, add the call after `m.ticks++`:

```go
	m.ticks++
	m.beginTick()
```

In `internal/runner/lifecycle.go`, in `refreshRunners`, add `m.markAnswered(i.ID)` right after the error branch, so the tail of the loop reads:

```go
			continue // start_timeout handles runners that never appear
		}
		m.markAnswered(i.ID)
		if r.Busy {
			m.move(i.ID, sched.Busy, sched.Starting, sched.Idle)
		} else if r.Status == "online" {
			m.move(i.ID, sched.Idle, sched.Starting)
		}
```

Replace the body of the loop in `stopStartTimedOut` up to the `SD.Stop` call, so the function reads:

```go
// stopStartTimedOut stops runners that never came online; finish deletes their registration.
func (m *Manager) stopStartTimedOut(ctx context.Context, cfg *config.Config, now time.Time) {
	for _, id := range sched.StartTimedOut(cfg, m.schedInstances(cfg), now) {
		m.mu.Lock()
		i, ok := m.insts[id]
		repo, answered := "", false
		if ok {
			repo, answered = i.Repo, i.answered
		}
		m.mu.Unlock()
		// A stop a person asked for can finish and remove a runner while this
		// loop is stopping another one. A runner the runners API did not answer
		// for in this tick is left alone: an API pause or a failed call says
		// nothing about whether it came online.
		if !ok || !answered {
			continue
		}
		if err := m.SD.Stop(ctx, UnitPrefix+id); err != nil {
			m.Events.Add("warn", repo, "stop %s: %v", id, err)
			continue
		}
		m.Events.Add("warn", repo, "runner %s did not come online within %s; replaced", id, cfg.StartTimeout)
	}
}
```

- [ ] **Step 4: Update the two tests that call `stopStartTimedOut` directly**

In `internal/runner/lifecycle_test.go`, in both `TestStartTimeoutStopsUnit` and `TestStartTimeoutSkipsARunnerThatIsGone`, add this line directly before the line that sets `h.now = h.now.Add(3 * time.Minute)`:

```go
	h.m.refreshRunners(context.Background()) // the runners API answers: still offline
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runner/manager.go internal/runner/tick.go internal/runner/lifecycle.go internal/runner/lifecycle_test.go internal/runner/adopt_test.go
git commit -m "fix(runner): time out a start only on an answer"
```

### Task 5: A registration that is gone

**Files:**
- Modify: `internal/runner/manager.go` (`instance`, `Manager`, `Init`, `beginTick`, `schedInstances`), `internal/runner/lifecycle.go` (`refreshRunners`, `stopIdleRunner`, `deregisterIdle`, new helpers), `internal/runner/tick.go` (`Tick`)
- Test: `internal/runner/lifecycle_test.go`, `internal/runner/update_start_test.go`

**Interfaces:**
- Consumes: `sched.Instance.Unusable`, `markAnswered`, `beginTick` (Contracts)
- Produces: `registrationGone`, `instance.lost`, the test helper `idleOnline` (Contracts)

**Items:** 3, 7, 18

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 1 - coupling 1 - risk 2 = 5

- [ ] **Step 1: Write the failing tests**

Append to `internal/runner/lifecycle_test.go`:

```go
func idleOnline(t *testing.T, h *harness, repo string) {
	t.Helper()
	if err := h.m.spawn(context.Background(), h.cfg, repo); err != nil {
		t.Fatal(err)
	}
	h.gh.setRunner(101, "online", false)
	h.m.refreshRunners(context.Background())
	if h.state("aaaaaa") != "idle" {
		t.Fatalf("state %s", h.state("aaaaaa"))
	}
}

// GitHub answers 404 for the runner and its list of the repository's runners
// does not hold it: the registration is gone, the runner can take no job, and
// its unit is stopped at once.
func TestIdleRunnerWhoseRegistrationIsGoneIsStopped(t *testing.T) {
	h := newHarness(t)
	idleOnline(t, h, "darkmem")
	h.gh.mu.Lock()
	delete(h.gh.runners, 101)
	h.gh.mu.Unlock()
	h.m.Tick(context.Background())
	if !reflect.DeepEqual(h.sd.stopped, []string{"ghr-runner-aaaaaa"}) ||
		!strings.Contains(h.eventText(), "warn darkmem runner aaaaaa registration is gone; stopped") {
		t.Fatalf("stopped %v\n%s", h.sd.stopped, h.eventText())
	}
	if n := sched.Covering(h.m.schedInstances(h.cfg), "darkmem", h.now); n != 0 {
		t.Fatalf("a runner without a registration still covers %d jobs", n)
	}
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	h.m.Wait()
	if h.state("aaaaaa") != "gone" {
		t.Fatalf("its slot is still held: state %s", h.state("aaaaaa"))
	}
}

// A 404 also means "the token cannot see this repository". When the list
// cannot be read either, nothing is concluded and the runner keeps running.
func TestIdleRunnerIsKeptWhenTheRunnerListCannotBeRead(t *testing.T) {
	h := newHarness(t)
	idleOnline(t, h, "darkmem")
	h.gh.mu.Lock()
	delete(h.gh.runners, 101)
	h.gh.mu.Unlock()
	h.gh.setErr("ListRunners darkmem", &github.APIError{Status: 404, Kind: github.ErrNotFound})
	h.now = h.now.Add(6 * time.Minute) // past idle_timeout, so the idle stop asks too
	h.m.Tick(context.Background())
	if len(h.sd.stopped) != 0 || h.state("aaaaaa") != "idle" {
		t.Fatalf("stopped %v state %s", h.sd.stopped, h.state("aaaaaa"))
	}
	if e := h.m.Status().Repos[1].Error; !strings.Contains(e, "token lacks access") {
		t.Fatalf("repository error %q", e)
	}
	if n := strings.Count(h.gh.callsText(), "ListRunners darkmem"); n != 1 {
		t.Fatalf("the runner list was read %d times in one tick", n)
	}
}

// The list answers and still holds the runner: the 404 proved nothing.
func TestIdleRunnerStillListedIsKept(t *testing.T) {
	h := newHarness(t)
	idleOnline(t, h, "darkmem")
	h.gh.mu.Lock()
	delete(h.gh.runners, 101)
	h.gh.mu.Unlock()
	h.gh.listed["darkmem"] = []github.Runner{{ID: 101, Name: "ghr-darkmem-aaaaaa", Status: "online"}}
	h.now = h.now.Add(6 * time.Minute)
	h.m.Tick(context.Background())
	if len(h.sd.stopped) != 0 || h.state("aaaaaa") != "idle" || len(h.gh.deleted) != 0 {
		t.Fatalf("stopped %v deleted %v state %s", h.sd.stopped, h.gh.deleted, h.state("aaaaaa"))
	}
}

// A list refused for the whole token ends the runner checks of this tick.
func TestRunnerChecksStopWhenTheRunnerListRejectsTheToken(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud") // aaaaaa, runner 101
	h.m.spawn(context.Background(), h.cfg, "darkmem")   // bbbbbb, runner 102
	h.gh.mu.Lock()
	delete(h.gh.runners, 101)
	h.gh.mu.Unlock()
	h.gh.setErr("ListRunners darkcloud", &github.APIError{Status: 401, Kind: github.ErrAuth})
	h.m.refreshRunners(context.Background())
	if !h.m.Status().Degraded || strings.Contains(h.gh.callsText(), "GetRunner darkmem") {
		t.Fatalf("degraded %v, calls %s", h.m.Status().Degraded, h.gh.callsText())
	}
}

// A runner whose registration is gone needs no GitHub call to be stopped, so
// an API pause must not keep it running.
func TestGoneRunnerIsStoppedDuringAnAPIPause(t *testing.T) {
	h := newHarness(t)
	idleOnline(t, h, "darkmem")
	h.gh.mu.Lock()
	delete(h.gh.runners, 101)
	h.gh.mu.Unlock()
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Hour)})
	h.sd.stopErr["ghr-runner-aaaaaa"] = errors.New("unit busy")
	h.m.Tick(context.Background()) // confirmed gone, then the poll is rate-limited; the stop fails
	if h.m.apiAllowed(h.now) || len(h.sd.stopped) != 0 {
		t.Fatalf("API allowed %v, stopped %v", h.m.apiAllowed(h.now), h.sd.stopped)
	}
	delete(h.sd.stopErr, "ghr-runner-aaaaaa")
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background()) // still paused: the stop is retried without an API call
	if !reflect.DeepEqual(h.sd.stopped, []string{"ghr-runner-aaaaaa"}) {
		t.Fatalf("stopped %v\n%s", h.sd.stopped, h.eventText())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -run 'TestIdleRunnerWhoseRegistrationIsGoneIsStopped|TestIdleRunnerIsKeptWhenTheRunnerListCannotBeRead|TestIdleRunnerStillListedIsKept|TestRunnerChecksStopWhenTheRunnerListRejectsTheToken|TestGoneRunnerIsStoppedDuringAnAPIPause' -count=1`
Expected: FAIL: `TestIdleRunnerWhoseRegistrationIsGoneIsStopped` with `stopped []`, `TestIdleRunnerIsKeptWhenTheRunnerListCannotBeRead` with `repository error ""`, `TestRunnerChecksStopWhenTheRunnerListRejectsTheToken` with `degraded false`, `TestGoneRunnerIsStoppedDuringAnAPIPause` with `stopped []`. `TestIdleRunnerStillListedIsKept` passes already; it guards against reading every answered list as "gone".

- [ ] **Step 3: Write the implementation**

In `internal/runner/manager.go`:

Change the `deregistered` line of `instance` and add `lost` after it:

```go
	deregistered bool      // its registration was deleted by ghr or confirmed missing
	lost         bool      // the registration was confirmed missing, not deleted by ghr
```

Add above the `Manager` struct:

```go
// runnerList is one repository's registered runner IDs as read in this tick;
// ok is false when the read failed.
type runnerList struct {
	ids map[int64]bool
	ok  bool
}
```

Add to the `Manager` struct after `retryAt`:

```go
	lists          map[string]runnerList // by lower-cased repo; reset every tick
```

In `Init`, after `m.retryAt = map[string]time.Time{}`:

```go
	m.lists = map[string]runnerList{}
```

In `beginTick`, add before the loop:

```go
	m.lists = map[string]runnerList{}
```

In `schedInstances`, add `Unusable: i.deregistered` to the literal:

```go
		out = append(out, sched.Instance{ID: i.ID, Repo: i.Repo, State: i.State, StateSince: i.StateSince,
			JobConfirmed: i.JobConfirmed, Stale: stale(cfg, i), Unusable: i.deregistered})
```

In `internal/runner/lifecycle.go`, add after `refreshRunners`:

```go
// registrationGone reports whether repo's runner list answers and does not
// hold runner id. A 404 for one runner cannot say that by itself: GitHub also
// answers 404 when the token cannot see the repository. A list that cannot be
// read goes through apiErr and concludes nothing. The list is read once per
// repository per tick.
func (m *Manager) registrationGone(ctx context.Context, repo string, id int64, now time.Time) bool {
	key := strings.ToLower(repo)
	m.mu.Lock()
	l, read := m.lists[key]
	m.mu.Unlock()
	if !read {
		runners, err := m.GH.ListRunners(ctx, repo)
		if err != nil {
			m.apiErr(repo, err, now)
		} else {
			l = runnerList{ids: map[int64]bool{}, ok: true}
			for _, r := range runners {
				l.ids[r.ID] = true
			}
		}
		m.mu.Lock()
		m.lists[key] = l
		m.mu.Unlock()
	}
	return l.ok && !l.ids[id]
}

// markLost records that an idle instance's registration is confirmed missing.
// A starting one is left to the start timeout: a registration made seconds
// ago is not declared gone on one reading.
func (m *Manager) markLost(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i, ok := m.insts[id]; ok && i.State == sched.Idle {
		i.deregistered, i.lost = true, true
	}
}
```

In `refreshRunners`, replace the error branch so it reads:

```go
		r, err := m.GH.GetRunner(ctx, i.Repo, i.RunnerID)
		if err != nil {
			if github.IsKind(err, github.ErrAuth) || github.IsKind(err, github.ErrRateLimit) {
				m.apiErr(i.Repo, err, now)
				return
			}
			if github.IsKind(err, github.ErrNotFound) {
				if m.registrationGone(ctx, i.Repo, i.RunnerID, now) {
					m.markAnswered(i.ID)
					m.markLost(i.ID)
				} else if !m.apiAllowed(now) {
					return // the list was refused for the whole token, or rate-limited
				}
			}
			continue // start_timeout handles runners that never appear
		}
```

Add after `stopIdle`:

```go
// stopDeregistered retries the unit stop of idle runners whose registration
// is already gone. It needs no GitHub call, so it runs on the ticks in which
// API calls are not allowed and stopIdle does not run.
func (m *Manager) stopDeregistered(ctx context.Context, now time.Time) {
	for _, i := range m.snapshot() {
		if i.State == sched.Idle && i.deregistered {
			m.stopIdleRunner(ctx, i, now)
		}
	}
}
```

In `internal/runner/tick.go`, in `Tick`, replace

```go
	if m.apiAllowed(now) {
		m.stopIdle(ctx, cfg, now)
	}
```

with

```go
	if m.apiAllowed(now) {
		m.stopIdle(ctx, cfg, now)
	} else {
		m.stopDeregistered(ctx, now)
	}
```

In `deregisterIdle`, replace

```go
	if err != nil {
		if !github.IsKind(err, github.ErrNotFound) {
			m.apiErr(i.Repo, err, now)
		}
		return idleKept, false
	}
```

with

```go
	if err != nil {
		if !github.IsKind(err, github.ErrNotFound) {
			m.apiErr(i.Repo, err, now)
			return idleKept, false
		}
		if !m.registrationGone(ctx, i.Repo, i.RunnerID, now) {
			return idleKept, false
		}
		m.markLost(i.ID)
		return idleStopped, true
	}
```

In `stopIdleRunner`, replace the last two statements

```go
	m.Events.Add("info", i.Repo, "stopped idle runner %s", i.ID)
	return idleStopped
```

with

```go
	m.mu.Lock()
	lost := false
	if p := m.insts[i.ID]; p != nil {
		lost = p.lost
	}
	m.mu.Unlock()
	if lost {
		m.Events.Add("warn", i.Repo, "runner %s registration is gone; stopped", i.ID)
	} else {
		m.Events.Add("info", i.Repo, "stopped idle runner %s", i.ID)
	}
	return idleStopped
```

Change the comment above `stopIdleRunner`'s `if !gone {` from

```go
	// A runner deregistered by an earlier stop can take no job and its check
	// now answers 404, so only the unit stop is repeated.
```

to

```go
	// A runner whose registration is already gone can take no job and its
	// check answers 404, so only the unit stop is repeated.
```

- [ ] **Step 4: Update the existing tests this changes**

In `internal/runner/lifecycle_test.go`, in `TestStopIdleLeavesRunnerOnCheckFailure`, delete this row of the error table (the two new tests cover a 404):

```go
		&github.APIError{Status: 404, Kind: github.ErrNotFound},
```

In `internal/runner/update_start_test.go`, replace the whole of `TestUpdateBacksOffWhenAnIdleRunnerCannotStop`, including its comment, with:

```go
// An idle runner that cannot be checked holds the update off for a while
// instead of every tick stopping the other idle runners again.
func TestUpdateBacksOffWhenAnIdleRunnerCannotStop(t *testing.T) {
	h := newHarness(t)
	queuedUpdate(t, h, tarballSum())
	idleRunner(t, h)
	if err := h.m.spawn(context.Background(), h.cfg, "darkcloud"); err != nil {
		t.Fatal(err)
	}
	h.gh.setRunner(h.m.insts["bbbbbb"].RunnerID, "online", false)
	h.m.move("bbbbbb", sched.Idle, sched.Starting)
	h.gh.setErr("GetRunner darkcloud", &github.APIError{Status: 502, Kind: github.ErrServer})
	postponed := func() int { return strings.Count(h.eventText(), "runner update postponed") }
	h.m.Tick(context.Background())
	h.m.Wait()
	if postponed() != 1 || strings.Contains(h.eventText(), "runner update started") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	h.now = h.now.Add(time.Minute)
	h.m.Tick(context.Background())
	h.m.Wait()
	if postponed() != 1 || strings.Contains(h.eventText(), "runner update started") {
		t.Fatalf("retried within the backoff:\n%s", h.eventText())
	}
	h.gh.setErr("GetRunner darkcloud", nil)
	h.now = h.now.Add(15 * time.Minute)
	h.m.Tick(context.Background())
	h.m.Wait()
	if current(t, h) != "2.338.0" {
		t.Fatalf("no update after the backoff:\n%s", h.eventText())
	}
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ ./internal/daemon/ -count=1`
Expected: PASS. An existing test that now fails because its hand-made idle runner is missing from the fake GitHub is fixed as Global Constraints says.

- [ ] **Step 6: Commit**

```bash
git add internal/runner/manager.go internal/runner/lifecycle.go internal/runner/tick.go internal/runner/lifecycle_test.go internal/runner/update_start_test.go
git commit -m "fix(runner): stop a runner whose registration is gone"
```

### Task 6: Idle runner offline too long

**Files:**
- Modify: `internal/runner/manager.go` (`instance`, `schedInstances`, new helper), `internal/runner/lifecycle.go` (`refreshRunners`, `stopIdleRunner`)
- Test: `internal/runner/lifecycle_test.go`

**Interfaces:**
- Consumes: `sched.Instance.Unusable`, the test helper `idleOnline` (Contracts)
- Produces: `instance.offlineSince` (Contracts)

**Items:** 3

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

- [ ] **Step 1: Write the failing tests**

Append to `internal/runner/lifecycle_test.go` (`idleOnline` is the helper Task 5 added to this file: it spawns runner `aaaaaa` with runner ID 101 and makes it idle and online):

```go
// An idle runner whose process hangs shows as offline. Within start_timeout it
// may be reconnecting; past it, it is replaced.
func TestIdleRunnerOfflinePastStartTimeoutIsReplaced(t *testing.T) {
	h := newHarness(t)
	idleOnline(t, h, "darkmem")
	h.gh.setRunner(101, "offline", false)
	h.m.Tick(context.Background()) // first seen offline
	h.now = h.now.Add(time.Minute)
	h.m.Tick(context.Background())
	if len(h.sd.stopped) != 0 || sched.Covering(h.m.schedInstances(h.cfg), "darkmem", h.now) != 1 {
		t.Fatalf("replaced within the grace: stopped %v", h.sd.stopped)
	}
	h.now = h.now.Add(time.Minute)
	h.m.Tick(context.Background())
	if !reflect.DeepEqual(h.sd.stopped, []string{"ghr-runner-aaaaaa"}) || !reflect.DeepEqual(h.gh.deleted, []int64{101}) ||
		!strings.Contains(h.eventText(), "warn darkmem runner aaaaaa offline for 2m0s; replaced") {
		t.Fatalf("stopped %v deleted %v\n%s", h.sd.stopped, h.gh.deleted, h.eventText())
	}
}

func TestIdleRunnerBackOnlineRestartsTheOfflineClock(t *testing.T) {
	h := newHarness(t)
	idleOnline(t, h, "darkmem")
	h.gh.setRunner(101, "offline", false)
	h.m.Tick(context.Background())
	h.now = h.now.Add(time.Minute)
	h.gh.setRunner(101, "online", false)
	h.m.Tick(context.Background())
	h.now = h.now.Add(time.Minute)
	h.gh.setRunner(101, "offline", false)
	h.m.Tick(context.Background()) // offline again: the clock starts here
	h.now = h.now.Add(time.Minute)
	h.m.Tick(context.Background())
	if len(h.sd.stopped) != 0 {
		t.Fatalf("stopped %v\n%s", h.sd.stopped, h.eventText())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -run 'TestIdleRunnerOfflinePastStartTimeoutIsReplaced|TestIdleRunnerBackOnlineRestartsTheOfflineClock' -count=1`
Expected: the first FAILS with `stopped [] deleted []`; the second passes already (it guards the clock reset).

- [ ] **Step 3: Write the implementation**

In `internal/runner/manager.go`, add to `instance` after `lost`:

```go
	offlineSince time.Time // when an idle runner was first seen offline; zero while online
```

Add after `markAnswered`:

```go
// noteOnline records what the runners API said about a runner's connection:
// the first time an idle runner is seen offline, or that it is connected.
func (m *Manager) noteOnline(id string, online bool, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.insts[id]
	switch {
	case !ok:
	case online:
		i.offlineSince = time.Time{}
	case i.State == sched.Idle && i.offlineSince.IsZero():
		i.offlineSince = now
	}
}
```

Replace `schedInstances` with:

```go
func (m *Manager) schedInstances(cfg *config.Config) []sched.Instance {
	now, grace := m.Now(), cfg.StartTimeout.D()
	var out []sched.Instance
	for _, i := range m.snapshot() {
		// An idle runner may be reconnecting; start_timeout is how long a
		// runner gets to connect in the first place.
		offline := i.State == sched.Idle && !i.offlineSince.IsZero() && now.Sub(i.offlineSince) >= grace
		out = append(out, sched.Instance{ID: i.ID, Repo: i.Repo, State: i.State, StateSince: i.StateSince,
			JobConfirmed: i.JobConfirmed, Stale: stale(cfg, i), Unusable: i.deregistered || offline})
	}
	return out
}
```

In `internal/runner/lifecycle.go`, in `refreshRunners`, replace

```go
		m.markAnswered(i.ID)
		if r.Busy {
			m.move(i.ID, sched.Busy, sched.Starting, sched.Idle)
		} else if r.Status == "online" {
			m.move(i.ID, sched.Idle, sched.Starting)
		}
```

with

```go
		m.markAnswered(i.ID)
		if r.Busy {
			m.move(i.ID, sched.Busy, sched.Starting, sched.Idle)
		} else if r.Status == "online" {
			m.move(i.ID, sched.Idle, sched.Starting)
		}
		m.noteOnline(i.ID, r.Busy || r.Status == "online", now)
```

Change the comment above `refreshRunners` to:

```go
// refreshRunners moves starting and idle instances to idle or busy from the
// runners API, so an idle runner GitHub reports busy is busy even before its
// hook record or the jobs API shows the job, and notes how long an idle
// runner has been offline.
```

In `stopIdleRunner`, replace the block Task 5 added

```go
	m.mu.Lock()
	lost := false
	if p := m.insts[i.ID]; p != nil {
		lost = p.lost
	}
	m.mu.Unlock()
	if lost {
		m.Events.Add("warn", i.Repo, "runner %s registration is gone; stopped", i.ID)
	} else {
		m.Events.Add("info", i.Repo, "stopped idle runner %s", i.ID)
	}
	return idleStopped
```

with

```go
	m.mu.Lock()
	lost, offline := false, time.Time{}
	if p := m.insts[i.ID]; p != nil {
		lost, offline = p.lost, p.offlineSince
	}
	m.mu.Unlock()
	switch {
	case lost:
		m.Events.Add("warn", i.Repo, "runner %s registration is gone; stopped", i.ID)
	case !offline.IsZero() && now.Sub(offline) >= m.Config().StartTimeout.D():
		m.Events.Add("warn", i.Repo, "runner %s offline for %s; replaced", i.ID, now.Sub(offline).Round(time.Second))
	default:
		m.Events.Add("info", i.Repo, "stopped idle runner %s", i.ID)
	}
	return idleStopped
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ ./internal/daemon/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runner/manager.go internal/runner/lifecycle.go internal/runner/lifecycle_test.go
git commit -m "fix(runner): replace an idle runner that stays offline"
```

### Task 7: A 403 stops one repository

**Files:**
- Modify: `internal/runner/manager.go` (`apiErr`, new helpers), `internal/runner/tick.go` (`gatherDemand`, `spawnPlanned`), `internal/runner/lifecycle.go` (`refreshRunners`), `internal/runner/update.go:175`
- Test: `internal/runner/tick_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `tokenRejected`, `permissionText` (Contracts)

**Items:** 2

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing tests**

Append to `internal/runner/tick_test.go`:

```go
// One repository refusing a write must not stop the others: only a 401 says
// the token itself is bad.
func TestForbiddenSpawnStopsOnlyItsRepository(t *testing.T) {
	h := newHarness(t)
	labels := []string{"homelab"}
	queuedRun(h, "darkcloud", 1, github.Job{ID: 1, Status: "queued", Labels: labels, CreatedAt: h.now.Add(-time.Minute)})
	queuedRun(h, "darkmem", 2, github.Job{ID: 2, Status: "queued", Labels: labels, CreatedAt: h.now})
	h.gh.setErr("GenerateJITConfig darkcloud", &github.APIError{Status: 403, Kind: github.ErrAuth,
		Message: "Resource not accessible by personal access token", Permissions: "administration=write"})
	h.m.Tick(context.Background())
	if h.m.Status().Degraded {
		t.Fatalf("degraded: %s", h.m.Status().DegradedReason)
	}
	if len(h.sd.started) != 1 || !strings.Contains(h.gh.jitCalls[0], "darkmem") {
		t.Fatalf("started %d, jit calls %v", len(h.sd.started), h.gh.jitCalls)
	}
}

// A 403 on the per-runner check is filed under its repository once per
// backoff window, and the other repositories' runners are still checked.
func TestForbiddenRunnerCheckIsThatRepositorysError(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud") // aaaaaa, runner 101
	h.m.spawn(context.Background(), h.cfg, "darkmem")   // bbbbbb, runner 102
	h.gh.setRunner(102, "online", false)
	h.gh.setErr("GetRunner darkcloud", &github.APIError{Status: 403, Kind: github.ErrAuth,
		Message: "Resource not accessible by personal access token", Permissions: "administration=read"})
	h.m.refreshRunners(context.Background())
	st := h.m.Status()
	want := "token lacks permission for this repository (needs administration=read): github: 403 Resource not accessible by personal access token"
	if st.Degraded || st.Repos[0].Error != want || h.state("bbbbbb") != "idle" {
		t.Fatalf("degraded %v error %q bbbbbb %s", st.Degraded, st.Repos[0].Error, h.state("bbbbbb"))
	}
	h.m.refreshRunners(context.Background()) // the same instant: inside the backoff
	if n := strings.Count(h.eventText(), "token lacks permission"); n != 1 {
		t.Fatalf("%d events for one refusal:\n%s", n, h.eventText())
	}
}

func TestForbiddenPollIsThatRepositorysError(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 403, Kind: github.ErrAuth,
		Message: "Resource not accessible by personal access token", Permissions: "actions=read"})
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	st := h.m.Status()
	want := "token lacks permission for this repository (needs actions=read): github: 403 Resource not accessible by personal access token"
	if st.Degraded || st.Repos[0].Error != want || len(h.sd.started) != 1 {
		t.Fatalf("degraded %v started %d error %q", st.Degraded, len(h.sd.started), st.Repos[0].Error)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -run 'TestForbidden' -count=1`
Expected: FAIL, all three with `degraded`

- [ ] **Step 3: Write the implementation**

In `internal/runner/manager.go`, add above `apiErr`:

```go
// tokenRejected reports whether err means GitHub rejected the token itself,
// which only a 401 says. A 403 refuses one repository or one call.
func tokenRejected(err error) bool {
	var ae *github.APIError
	return errors.As(err, &ae) && ae.Kind == github.ErrAuth && ae.Status == 401
}

// permissionText words a 403 that is not a rate limit.
func permissionText(ae *github.APIError) string {
	msg := "token lacks permission for this repository"
	if ae.Permissions != "" {
		msg += " (needs " + ae.Permissions + ")"
	}
	return msg + ": " + ae.Error()
}
```

Replace the whole `apiErr` method, including its comment, with:

```go
// apiErr applies the error-handling table: 401 → degraded, rate limit → pause,
// 403 → per-repo permission error, 404 → per-repo access error, anything else
// → per-repo backoff.
func (m *Manager) apiErr(repo string, err error, now time.Time) {
	var ae *github.APIError
	if errors.As(err, &ae) {
		switch {
		case tokenRejected(err):
			reason := "GitHub rejected the token"
			if ae.Path != "" {
				reason += " on " + ae.Method + " " + ae.Path
			}
			if ae.Permissions != "" {
				reason += " (needs " + ae.Permissions + ")"
			}
			reason += ": " + ae.Error()
			m.mu.Lock()
			was := m.degraded
			m.degraded = true
			m.degradedReason = reason
			m.authCheckAt = now.Add(authRecheck)
			m.mu.Unlock()
			if !was {
				m.Events.Add("error", repo, "%s; spawning stopped", reason)
			}
			return
		case ae.Kind == github.ErrRateLimit:
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
	switch {
	case github.IsKind(err, github.ErrNotFound):
		msg = "token lacks access to repo (add it to the PAT's repository access)"
	case github.IsKind(err, github.ErrAuth):
		msg = permissionText(ae)
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

In `internal/runner/tick.go`, in `gatherDemand`, replace

```go
			if github.IsKind(err, github.ErrAuth) {
				authFailed = true
			}
```

with

```go
			if tokenRejected(err) {
				authFailed = true
			}
```

In `spawnPlanned`, replace

```go
			var ae *github.APIError
			if errors.As(err, &ae) && (ae.Kind == github.ErrAuth || ae.Kind == github.ErrRateLimit) {
				m.apiErr(s.Repo, err, now)
				return
			}
```

with

```go
			if tokenRejected(err) || github.IsKind(err, github.ErrRateLimit) {
				m.apiErr(s.Repo, err, now)
				return
			}
```

and remove `"errors"` from `tick.go`'s imports (nothing else in the file uses it).

In `internal/runner/lifecycle.go`, in `refreshRunners` (not in `finalize`, which has a similar line), replace

```go
			if github.IsKind(err, github.ErrAuth) || github.IsKind(err, github.ErrRateLimit) {
				m.apiErr(i.Repo, err, now)
				return
			}
```

with

```go
			if tokenRejected(err) || github.IsKind(err, github.ErrRateLimit) {
				m.apiErr(i.Repo, err, now)
				return
			}
			if github.IsKind(err, github.ErrAuth) {
				m.repoRefused(i.Repo, err, now)
				continue
			}
```

In `internal/runner/manager.go`, add after `apiErr`:

```go
// repoRefused files an error from a call that is made on every tick under
// its repository, once per backoff window: the status shows it, and the
// event list does not get a line per tick.
func (m *Manager) repoRefused(repo string, err error, now time.Time) {
	m.mu.Lock()
	waiting := now.Before(m.retryAt[repo])
	m.mu.Unlock()
	if !waiting {
		m.apiErr(repo, err, now)
	}
}
```

In `internal/runner/update.go`, in `checkRelease` (line 175), replace

```go
		if github.IsKind(err, github.ErrAuth) || github.IsKind(err, github.ErrRateLimit) {
			m.apiErr("", err, now)
		}
```

with

```go
		// A 403 here refuses the release listing only; it is already recorded
		// as the check's error and belongs to no repository.
		if tokenRejected(err) || github.IsKind(err, github.ErrRateLimit) {
			m.apiErr("", err, now)
		}
```

In `registrationGone`, which also runs on every tick while a runner keeps answering 404, replace

```go
		if err != nil {
			m.apiErr(repo, err, now)
		} else {
```

with

```go
		if err != nil {
			if tokenRejected(err) || github.IsKind(err, github.ErrRateLimit) {
				m.apiErr(repo, err, now)
			} else {
				m.repoRefused(repo, err, now)
			}
		} else {
```

Leave `finalize` and `deregisterIdle` as they are: they pass every error to `apiErr`, which now files a 403 under the repository it came from. Neither runs on every tick for the same runner, so they need no `repoRefused`.

- [ ] **Step 4: Update the test that expected a 403 to degrade**

In `internal/runner/tick_test.go`, replace the whole of `TestAuthErrorNamesCallAndPermission` with:

```go
func TestAuthErrorNamesCallAndPermission(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 401, Kind: github.ErrAuth,
		Message: "Bad credentials",
		Method:  "GET", Path: "/repos/darkraise/darkcloud/actions/runs", Permissions: "actions=read"})
	h.m.Tick(context.Background())
	want := "GitHub rejected the token on GET /repos/darkraise/darkcloud/actions/runs (needs actions=read): " +
		"github: 401 Bad credentials"
	if got := h.m.Status().DegradedReason; got != want {
		t.Fatalf("reason %q\nwant   %q", got, want)
	}
	if !strings.Contains(h.eventText(), "error darkcloud "+want+"; spawning stopped") {
		t.Fatalf("events %s", h.eventText())
	}
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ ./internal/daemon/ -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runner/manager.go internal/runner/tick.go internal/runner/lifecycle.go internal/runner/update.go internal/runner/tick_test.go
git commit -m "fix(runner): keep a 403 to its own repository"
```

### Task 8: Spawn backoff

**Files:**
- Modify: `internal/runner/manager.go` (constants, `instance`, `Manager`, `Init`, `enterLocked`, `ClearDegraded`, `Status`, new helpers), `internal/runner/tick.go` (`spawnPlanned`), `internal/runner/lifecycle.go` (`refreshUnits`, new helper), `internal/runner/adopt.go` (`Adopt`)
- Test: `internal/runner/backoff_test.go` (new)

**Interfaces:**
- Consumes: `enterLocked`, `instance.cameOnline`, `tokenRejected`, `permissionText` (Contracts)
- Produces: `spawnFailed`, `clearSpawnBackoffLocked`, `maxSpawnBackoff`, `instance.adopted` (Contracts)

**Items:** 2, 4, 18

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 1 - coupling 1 - risk 2 = 5

- [ ] **Step 1: Write the failing tests**

Create `internal/runner/backoff_test.go`:

```go
package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/github"
)

func jitAttempts(h *harness, repo string) int {
	return strings.Count(h.gh.callsText(), "GenerateJITConfig "+repo)
}

func spawnFailures(h *harness) int {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	return len(h.m.spawnFails)
}

// A failing spawn is retried after 10 s, 20 s, 40 s and so on. The polls
// between the attempts succeed and must not shorten the wait.
func TestSpawnBackoffDoublesAndSurvivesPolls(t *testing.T) {
	h := newHarness(t)
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.gh.setErr("GenerateJITConfig darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
	for tick, want := range []int{1, 2, 2, 3, 3, 3, 3, 4} { // ticks 10 s apart
		h.m.Tick(context.Background())
		if got := jitAttempts(h, "darkmem"); got != want {
			t.Fatalf("tick %d: %d attempts, want %d\n%s", tick, got, want, h.eventText())
		}
		h.now = h.now.Add(10 * time.Second)
	}
	if !strings.Contains(h.eventText(), "error darkmem spawn failed: github: 502 ; next attempt in 10s") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	var d time.Duration
	for range 20 {
		d = h.m.spawnFailed("darkmem", "boom", h.now)
	}
	if d != maxSpawnBackoff {
		t.Fatalf("delay %s, want the cap %s", d, maxSpawnBackoff)
	}
}

// A 403 on a spawn carries the permission wording in its event and in the
// repository's status line.
func TestForbiddenSpawnSaysWhichPermissionIsMissing(t *testing.T) {
	h := newHarness(t)
	queuedRun(h, "darkcloud", 1, github.Job{ID: 1, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.gh.setErr("GenerateJITConfig darkcloud", &github.APIError{Status: 403, Kind: github.ErrAuth,
		Message: "Resource not accessible by personal access token", Permissions: "administration=write"})
	h.m.Tick(context.Background())
	want := "token lacks permission for this repository (needs administration=write): github: 403 Resource not accessible by personal access token"
	if !strings.Contains(h.eventText(), "error darkcloud spawn failed: "+want+"; next attempt in 10s") || h.m.Status().Repos[0].Error != want {
		t.Fatalf("status %q\n%s", h.m.Status().Repos[0].Error, h.eventText())
	}
}

func TestSpawnBackoffClearsWhenARunnerComesOnline(t *testing.T) {
	h := newHarness(t)
	h.m.spawnFailed("darkmem", "boom", h.now)
	if e := h.m.Status().Repos[1].Error; e != "boom" {
		t.Fatalf("status error %q, want the spawn error", e)
	}
	if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
		t.Fatal(err)
	}
	h.gh.setRunner(101, "online", false)
	h.m.refreshRunners(context.Background())
	if spawnFailures(h) != 0 || h.m.Status().Repos[1].Error != "" {
		t.Fatalf("failures %d error %q", spawnFailures(h), h.m.Status().Repos[1].Error)
	}
	h.m.spawnFailed("darkmem", "boom", h.now)
	h.m.ClearDegraded()
	if spawnFailures(h) != 0 {
		t.Fatal("a replaced token must clear the spawn backoff")
	}
}

// A runner that exits before it ever came online is a failed start: the tick
// that sees the exit must not respawn it at once.
func TestRunnerThatNeverCameOnlineDelaysTheNextSpawn(t *testing.T) {
	h := newHarness(t)
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	if jitAttempts(h, "darkmem") != 1 {
		t.Fatalf("attempts %d", jitAttempts(h, "darkmem"))
	}
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.Tick(context.Background())
	h.m.Wait()
	if !strings.Contains(h.eventText(), "warn darkmem runner aaaaaa exited before coming online; next spawn in 10s") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	h.m.Tick(context.Background())
	if n := jitAttempts(h, "darkmem"); n != 1 {
		t.Fatalf("respawned inside the delay: %d attempts", n)
	}
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if n := jitAttempts(h, "darkmem"); n != 2 {
		t.Fatalf("attempts after the delay: %d", n)
	}
}

func TestAStoppedOrAdoptedRunnerIsNotASpawnFailure(t *testing.T) {
	t.Run("stopped by a person", func(t *testing.T) {
		h := newHarness(t)
		if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
			t.Fatal(err)
		}
		h.sd.keepActive = true // the unit is still stopping when Kill returns
		if err := h.m.Kill(context.Background(), "aaaaaa"); err != nil {
			t.Fatal(err)
		}
		h.sd.keepActive = false
		h.sd.active["ghr-runner-aaaaaa"] = false
		h.m.Tick(context.Background())
		h.m.Wait()
		if spawnFailures(h) != 0 {
			t.Fatalf("events:\n%s", h.eventText())
		}
	})
	t.Run("adopted", func(t *testing.T) {
		h := newHarness(t)
		makeInstance(t, h, "bbbbbb", "darkmem", 7, false)
		h.sd.active["ghr-runner-bbbbbb"] = true
		if err := h.m.Adopt(context.Background()); err != nil {
			t.Fatal(err)
		}
		h.sd.active["ghr-runner-bbbbbb"] = false
		h.m.Tick(context.Background())
		h.m.Wait()
		if spawnFailures(h) != 0 {
			t.Fatalf("events:\n%s", h.eventText())
		}
	})
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -run 'TestSpawnBackoff|TestForbiddenSpawnSaysWhichPermissionIsMissing|TestRunnerThatNeverCameOnline|TestAStoppedOrAdopted' -count=1`
Expected: FAIL to compile with `h.m.spawnFails undefined` and `h.m.spawnFailed undefined`

- [ ] **Step 3: Write the implementation**

In `internal/runner/manager.go`:

Add to the constant block that holds `authRecheck` and `maxBackoff`:

```go
	maxSpawnBackoff = 10 * time.Minute
```

Add to `instance` after `offlineSince`:

```go
	adopted      bool      // rebuilt by Adopt, not spawned by this process
```

Add to the `Manager` struct after `lists`:

```go
	spawnFails     map[string]int       // failed starts in a row, by lower-cased repo
	spawnRetryAt   map[string]time.Time // no spawn for the repo before this
	spawnErr       map[string]string    // the last failed start's message
```

In `Init`, after `m.lists = map[string]runnerList{}`:

```go
	m.spawnFails = map[string]int{}
	m.spawnRetryAt = map[string]time.Time{}
	m.spawnErr = map[string]string{}
```

Replace `enterLocked` with:

```go
// enterLocked puts i in state s; m.mu must be held. The first time a runner
// comes online its repository's failed starts are forgotten.
func (m *Manager) enterLocked(i *instance, s sched.State, now time.Time) {
	i.State = s
	i.StateSince = now
	if s == sched.Idle || s == sched.Busy {
		if !i.cameOnline {
			m.clearSpawnBackoffLocked(i.Repo)
		}
		i.cameOnline = true
	}
}
```

Add after `move`:

```go
// spawnFailed records a failed start for repo and returns the wait before the
// next attempt: 10 s, doubling to maxSpawnBackoff. A successful poll does not
// reset it, because a poll that works says nothing about a spawn that does not.
func (m *Manager) spawnFailed(repo, msg string, now time.Time) time.Duration {
	key := strings.ToLower(repo)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.spawnFails[key]++
	d := 10 * time.Second << (m.spawnFails[key] - 1)
	if d > maxSpawnBackoff || d <= 0 {
		d = maxSpawnBackoff
	}
	m.spawnRetryAt[key] = now.Add(d)
	m.spawnErr[key] = msg
	return d
}

// clearSpawnBackoffLocked forgets repo's failed starts; m.mu must be held.
func (m *Manager) clearSpawnBackoffLocked(repo string) {
	key := strings.ToLower(repo)
	delete(m.spawnFails, key)
	delete(m.spawnRetryAt, key)
	delete(m.spawnErr, key)
}

// spawnText words a failed start for the repository's status line.
func spawnText(err error) string {
	var ae *github.APIError
	if errors.As(err, &ae) && ae.Kind == github.ErrAuth {
		return permissionText(ae)
	}
	return err.Error()
}
```

Replace `ClearDegraded` with:

```go
// ClearDegraded is called after the token is replaced or reloaded. A new
// token may also be what a failing spawn was waiting for.
func (m *Manager) ClearDegraded() {
	m.mu.Lock()
	m.degraded = false
	m.degradedReason = ""
	m.spawnFails, m.spawnRetryAt, m.spawnErr = map[string]int{}, map[string]time.Time{}, map[string]string{}
	m.mu.Unlock()
}
```

In `Status`, directly after the statement that builds `rs` (the `rs := model.RepoStatus{...}` literal), add:

```go
		if rs.Error == "" {
			rs.Error = m.spawnErr[strings.ToLower(r.Name)]
		}
```

In `internal/runner/tick.go`, replace the whole `spawnPlanned` method, including its comment, with:

```go
// spawnPlanned spawns what sched.Plan decides, nothing while a runner update
// runs. Repos in an error state, or waiting after a failed start, are left
// out of planning. Spawning stops as soon as the live config differs from the
// one the plan used, so a pause or cap change made during polling is never
// overridden.
func (m *Manager) spawnPlanned(ctx context.Context, cfg *config.Config, now time.Time) {
	m.mu.Lock()
	if m.upd.running {
		m.mu.Unlock()
		return
	}
	demand := m.demand
	planCfg := *cfg
	planCfg.Repos = nil
	for _, r := range cfg.Repos {
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
		d := m.spawnFailed(s.Repo, msg, now)
		m.Events.Add("error", s.Repo, "spawn failed: %s; next attempt in %s", msg, d)
	}
}
```

In `internal/runner/lifecycle.go`, in `refreshUnits`, replace

```go
		active, err := m.SD.Active(ctx, UnitPrefix+i.ID)
		if err == nil && !active {
			// The snapshot can be out of date by now: a stop may have started
			// the cleanup, and one that failed waits for its retry time.
			m.startFinish(i.ID, true)
		}
```

with

```go
		active, err := m.SD.Active(ctx, UnitPrefix+i.ID)
		if err == nil && !active {
			// Recorded here, not in the cleanup that runs in the background:
			// this tick plans its spawns before that cleanup ends.
			if repo, yes := m.neverOnline(i.ID); yes {
				d := m.spawnFailed(repo, "runner exited before coming online", now)
				m.Events.Add("warn", repo, "runner %s exited before coming online; next spawn in %s", i.ID, d)
			}
			// The snapshot can be out of date by now: a stop may have started
			// the cleanup, and one that failed waits for its retry time.
			m.startFinish(i.ID, true)
		}
```

Add after `refreshUnits`:

```go
// neverOnline reports whether a runner this process spawned is gone without
// ever having come online: not one a person stopped, not one already in
// cleanup, and not one that ran a whole job between two ticks.
func (m *Manager) neverOnline(id string) (repo string, yes bool) {
	m.mu.Lock()
	i, ok := m.insts[id]
	if !ok || i.State == sched.Cleaning || i.cameOnline || i.adopted || i.stopping {
		m.mu.Unlock()
		return "", false
	}
	repo = i.Repo
	m.mu.Unlock()
	return repo, !exists(filepath.Join(m.instanceDir(id), JobFile))
}
```

In `internal/runner/adopt.go`, in `Adopt`, replace

```go
		inst := &instance{Meta: meta, State: sched.Starting, StateSince: meta.SpawnedAt}
```

with

```go
		inst := &instance{Meta: meta, State: sched.Starting, StateSince: meta.SpawnedAt, adopted: true}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ ./internal/daemon/ -count=1`
Expected: PASS. If an existing test spawned a runner, let it exit without coming online and expected an immediate respawn, advance its clock by the delay the new event names; report it.

- [ ] **Step 5: Commit**

```bash
git add internal/runner/manager.go internal/runner/tick.go internal/runner/lifecycle.go internal/runner/adopt.go internal/runner/backoff_test.go
git commit -m "fix(runner): back off after a failed runner start"
```

### Task 9: Why a queued update waits

**Files:**
- Modify: `internal/runner/update.go` (`updateState`, `storeUpdateFile`, `startUpdateIfFree`, new helpers), `internal/runner/tick.go` (`Tick`, `gatherDemand`)
- Test: `internal/runner/update_start_test.go`

**Interfaces:**
- Consumes: `tokenRejected` (Contracts)
- Produces: `gatherDemand`'s two results (Contracts)

**Items:** 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing test**

Append to `internal/runner/update_start_test.go`:

```go
// A queued update that cannot start for a reason other than runners working
// says why, once per reason.
func TestQueuedUpdateSaysWhyItWaits(t *testing.T) {
	h := newHarness(t)
	queuedUpdate(t, h, tarballSum())
	waiting := func() []string {
		var out []string
		for _, line := range strings.Split(h.eventText(), "\n") {
			if strings.Contains(line, "runner update waiting") {
				out = append(out, line)
			}
		}
		return out
	}
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 502, Kind: github.ErrServer})
	h.m.Tick(context.Background())
	h.now = h.now.Add(5 * time.Second) // darkcloud is still in its 10 s backoff
	h.m.Tick(context.Background())
	want := []string{"warn  runner update waiting: darkcloud did not answer its poll"}
	if got := waiting(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events %q", got)
	}
	// A queue made anew is a new wait: its reason is said again.
	if err := h.m.CancelUpdate(); err != nil {
		t.Fatal(err)
	}
	if err := h.m.QueueUpdate(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.m.Tick(context.Background())
	want = append(want, want[0])
	if got := waiting(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events after a requeue %q", got)
	}
	h.gh.setErr("ListRuns darkcloud", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Hour)})
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	want = append(want, "warn  runner update waiting: GitHub API calls are paused")
	if got := waiting(); !reflect.DeepEqual(got, want) {
		t.Fatalf("events %q", got)
	}
	h.gh.setErr("ListRuns darkcloud", nil)
	h.now = h.now.Add(time.Hour)
	h.m.Tick(context.Background())
	h.m.Wait()
	if got := waiting(); !reflect.DeepEqual(got, want) || current(t, h) != "2.338.0" {
		t.Fatalf("current %s events %q", current(t, h), got)
	}
}
```

Add `"reflect"` to the file's imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -run TestQueuedUpdateSaysWhyItWaits -count=1`
Expected: FAIL with `events []`

- [ ] **Step 3: Write the implementation**

In `internal/runner/update.go`, add to `updateState` after `stopRetryAt`:

```go
	waitReason   string    // why a queued update last waited; "" when nothing held it
```

Change `startUpdateIfFree`'s signature and its first statement. Replace

```go
func (m *Manager) startUpdateIfFree(tickCtx context.Context, cfg *config.Config, now time.Time, demandOK bool) {
	if !demandOK || m.Config() != cfg || !m.apiAllowed(now) || m.isDegraded() {
		return
	}
```

with

```go
func (m *Manager) startUpdateIfFree(tickCtx context.Context, cfg *config.Config, now time.Time, demandOK bool, unanswered string) {
	reason := ""
	switch {
	case m.isDegraded():
		reason = "GitHub rejected the token"
	case !m.apiAllowed(now):
		reason = "GitHub API calls are paused"
	case !demandOK:
		reason = unanswered + " did not answer its poll"
	}
	m.noteUpdateWait(reason)
	if reason != "" || m.Config() != cfg {
		return
	}
```

Add after `startUpdateIfFree`:

```go
// noteUpdateWait writes one event when the reason a queued update is held
// back changes. Runners working or jobs queued are the ordinary wait and are
// not reasons here.
func (m *Manager) noteUpdateWait(reason string) {
	m.mu.Lock()
	if m.upd.file.QueuedAt == nil || m.upd.running {
		reason = ""
	}
	changed := reason != m.upd.waitReason
	m.upd.waitReason = reason
	m.mu.Unlock()
	if changed && reason != "" {
		m.Events.Add("warn", "", "runner update waiting: %s", reason)
	}
}
```

In `storeUpdateFile`, replace

```go
	m.mu.Lock()
	m.upd.file = f
	m.mu.Unlock()
	return nil
```

with

```go
	m.mu.Lock()
	if !sameTime(f.QueuedAt, m.upd.file.QueuedAt) {
		// A queue that was cancelled or made anew waits for its own reasons.
		m.upd.waitReason = ""
	}
	m.upd.file = f
	m.mu.Unlock()
	return nil
```

and add after `storeUpdateFile`:

```go
func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
```

In `internal/runner/tick.go`, in `Tick`, replace

```go
	demandOK := false
	if m.apiAllowed(now) {
		demandOK = m.gatherDemand(ctx, cfg, now)
	}
```

with

```go
	demandOK, unanswered := false, ""
	if m.apiAllowed(now) {
		demandOK, unanswered = m.gatherDemand(ctx, cfg, now)
	}
```

and `m.startUpdateIfFree(ctx, cfg, now, demandOK)` with `m.startUpdateIfFree(ctx, cfg, now, demandOK, unanswered)`.

Replace the whole `gatherDemand` method, including its comment, with:

```go
// gatherDemand lists matching queued jobs per repo and confirms our in-progress
// jobs. Degraded clears only after a tick in which some repo answered and none
// rejected the token. It reports whether every unpaused repo answered, and
// the first one, in config order, that did not.
func (m *Manager) gatherDemand(ctx context.Context, cfg *config.Config, now time.Time) (bool, string) {
	ours := map[string]string{}
	for _, i := range m.snapshot() {
		ours[i.RunnerName] = i.ID
	}
	demand := sched.Demand{}
	anyOK, authFailed, unanswered := false, false, ""
	missed := func(name string) {
		if unanswered == "" {
			unanswered = name
		}
	}
	for _, r := range cfg.Repos {
		if r.Paused {
			continue
		}
		m.mu.Lock()
		wait := now.Before(m.retryAt[r.Name])
		m.mu.Unlock()
		if wait {
			missed(r.Name)
			continue
		}
		jobs, err := m.repoDemand(ctx, cfg, r, ours, now)
		if err != nil {
			missed(r.Name)
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
	return unanswered == "", unanswered
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runner/update.go internal/runner/tick.go internal/runner/update_start_test.go
git commit -m "feat(runner): say why a queued update waits"
```

### Task 10: Status during backoff and pause

**Files:**
- Modify: `internal/runner/tick.go` (`Tick`, `gatherDemand`, new helper), `internal/runner/manager.go` (`Status`, new helper)
- Test: `internal/runner/tick_test.go`

**Interfaces:**
- Consumes: `gatherDemand`'s two results, `tokenRejected`, `spawnFailed` and the `m.spawnErr` map (Contracts)
- Produces: nothing a later task names

**Items:** 14, 15

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

- [ ] **Step 1: Write the failing tests**

Append to `internal/runner/tick_test.go`:

```go
// A repository whose poll fails keeps its last known queued count.
func TestQueuedCountSurvivesAFailedPoll(t *testing.T) {
	h := newHarness(t)
	h.cfg.GlobalMax = 0 // nothing spawns, so the job stays queued
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	h.gh.setErr("ListRuns darkmem", &github.APIError{Status: 502, Kind: github.ErrServer})
	h.now = h.now.Add(10 * time.Second)
	h.m.Tick(context.Background())
	if r := h.m.Status().Repos[1]; r.Queued != 1 || r.Error == "" {
		t.Fatalf("queued %d error %q", r.Queued, r.Error)
	}
}

func pause(h *harness, repo string) {
	p := h.cfg.Clone()
	p.Repo(repo).Paused = true
	h.cfg = p
}

// A paused repository is not polled, so an error from before the pause would
// otherwise stay on screen for as long as the pause lasts.
func TestPausedRepositoryDropsItsError(t *testing.T) {
	h := newHarness(t)
	h.gh.setErr("ListRuns darkmem", &github.APIError{Status: 404, Kind: github.ErrNotFound})
	h.m.Tick(context.Background())
	if h.m.Status().Repos[1].Error == "" {
		t.Fatal("no error before the pause")
	}
	pause(h, "darkmem")
	h.m.Tick(context.Background())
	if e := h.m.Status().Repos[1].Error; e != "" {
		t.Fatalf("error kept while paused: %q", e)
	}
}

// A paused repository's busy runner still gets its job confirmed, so its job
// name and link show. Its queue is not counted: nothing will serve it, and it
// must not hold a runner update back.
func TestPausedRepositoryStillConfirmsItsBusyRunner(t *testing.T) {
	h := newHarness(t)
	if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
		t.Fatal(err)
	}
	h.writeJob(t, "aaaaaa", 55)
	h.gh.runs["darkmem/in_progress"] = []github.Run{{ID: 55}}
	h.gh.jobs[55] = []github.Job{
		{ID: 13, Status: "in_progress", RunID: 55, Name: "build", WorkflowName: "CI", RunnerName: "ghr-darkmem-aaaaaa", HTMLURL: "https://x/13"},
		{ID: 14, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now},
	}
	pause(h, "darkmem")
	h.m.Tick(context.Background())
	st := h.m.Status()
	if j := st.Instances[0].Job; j == nil || j.Name != "build" || j.HTMLURL != "https://x/13" {
		t.Fatalf("job %+v", st.Instances[0].Job)
	}
	if st.Repos[1].Queued != 0 {
		t.Fatalf("a paused repository's queue is counted: %d", st.Repos[1].Queued)
	}
	polls := strings.Count(h.gh.callsText(), "ListRuns darkmem")
	h.m.Tick(context.Background())
	if n := strings.Count(h.gh.callsText(), "ListRuns darkmem"); n != polls {
		t.Fatalf("polled again after the job was confirmed: %d then %d", polls, n)
	}
}

// Pausing a repository while API calls are paused must still clear what its
// last polls left behind; that needs no API call.
func TestPausingDuringAnAPIPauseStillClearsTheRepository(t *testing.T) {
	h := newHarness(t)
	h.cfg.GlobalMax = 0 // nothing spawns, so the job stays queued
	queuedRun(h, "darkmem", 2, github.Job{ID: 3, Status: "queued", Labels: []string{"homelab"}, CreatedAt: h.now})
	h.m.Tick(context.Background())
	h.m.apiErr("darkmem", &github.APIError{Status: 502, Kind: github.ErrServer}, h.now)
	h.m.apiErr("darkcloud", &github.APIError{Status: 403, Kind: github.ErrRateLimit, RetryAt: h.now.Add(time.Hour)}, h.now)
	if r := h.m.Status().Repos[1]; r.Queued != 1 || r.Error == "" {
		t.Fatalf("setup: queued %d error %q", r.Queued, r.Error)
	}
	pause(h, "darkmem")
	h.m.Tick(context.Background())
	if r := h.m.Status().Repos[1]; r.Queued != 0 || r.Error != "" {
		t.Fatalf("kept while paused: queued %d error %q", r.Queued, r.Error)
	}
}

func TestPausedRepositoryHidesItsSpawnError(t *testing.T) {
	h := newHarness(t)
	h.m.spawnFailed("darkmem", "boom", h.now)
	pause(h, "darkmem")
	if e := h.m.Status().Repos[1].Error; e != "" {
		t.Fatalf("spawn error shown for a paused repository: %q", e)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -run 'TestQueuedCountSurvivesAFailedPoll|TestPausedRepository|TestPausingDuringAnAPIPause' -count=1`
Expected: FAIL: `queued 0`, `error kept while paused`, a job named `e2e` with no URL, `kept while paused: queued 1`, and `spawn error shown for a paused repository`

- [ ] **Step 3: Write the implementation**

In `internal/runner/manager.go`, add after `snapshot`:

```go
// unconfirmedBusy reports whether repo has a busy runner whose job the jobs
// API has not confirmed yet.
func (m *Manager) unconfirmedBusy(repo string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, i := range m.insts {
		if strings.EqualFold(i.Repo, repo) && i.State == sched.Busy && !i.JobConfirmed {
			return true
		}
	}
	return false
}
```

In `Status`, replace the block Task 8 added

```go
		if rs.Error == "" {
			rs.Error = m.spawnErr[strings.ToLower(r.Name)]
		}
```

with

```go
		// A paused repo gets no spawns, so a failed start from before the
		// pause is not its state any more.
		if rs.Error == "" && !r.Paused {
			rs.Error = m.spawnErr[strings.ToLower(r.Name)]
		}
```

In `internal/runner/tick.go`, add after `gatherDemand`:

```go
// forgetPaused drops what the last polls left behind for paused repos that
// are not being polled: their error, their backoff and their queued jobs. It
// needs no API call, so it runs on every tick, API pause or not.
func (m *Manager) forgetPaused(cfg *config.Config) {
	for _, r := range cfg.Repos {
		if !r.Paused || m.unconfirmedBusy(r.Name) {
			continue
		}
		m.mu.Lock()
		delete(m.fails, r.Name)
		delete(m.retryAt, r.Name)
		delete(m.repoErr, r.Name)
		delete(m.demand, r.Name)
		m.mu.Unlock()
	}
}
```

In `Tick`, call it after `m.readJobFiles()`:

```go
	m.readJobFiles()
	m.forgetPaused(cfg)
```

Still in `internal/runner/tick.go`, replace the whole `gatherDemand` method, including its comment, with:

```go
// gatherDemand lists matching queued jobs per repo and confirms our in-progress
// jobs. Degraded clears only after a tick in which some repo answered and none
// rejected the token. It reports whether every unpaused repo answered, and
// the first one, in config order, that did not.
//
// A repo that does not answer keeps its last known queued jobs for status;
// spawnPlanned already leaves such a repo out. A paused repo is polled only
// while one of its busy runners still lacks its job's details, and its queue
// is never kept: nothing will serve it, and it must not hold an update back.
func (m *Manager) gatherDemand(ctx context.Context, cfg *config.Config, now time.Time) (bool, string) {
	ours := map[string]string{}
	for _, i := range m.snapshot() {
		ours[i.RunnerName] = i.ID
	}
	m.mu.Lock()
	last := m.demand
	m.mu.Unlock()
	demand := sched.Demand{}
	for _, r := range cfg.Repos {
		if jobs, ok := last[r.Name]; ok && !r.Paused {
			demand[r.Name] = jobs
		}
	}
	anyOK, authFailed, unanswered := false, false, ""
	missed := func(r config.Repo) {
		if !r.Paused && unanswered == "" {
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
		if !r.Paused {
			demand[r.Name] = jobs
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

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ ./internal/daemon/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runner/tick.go internal/runner/manager.go internal/runner/tick_test.go
git commit -m "fix(runner): keep status honest in backoff and pause"
```

### Task 11: Docker prunes off the tick

**Files:**
- Modify: `internal/runner/cleanup.go` (`checkDisk`), `internal/runner/manager.go` (new helper), `internal/runner/tick.go` (`Tick`)
- Test: `internal/runner/fakes_test.go`, `internal/runner/maintenance_test.go`, `internal/runner/cleanup_test.go`, `internal/runner/prune_scope_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names

**Items:** 5

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 2 - spec 0 - coupling 0 - risk 3 = 5

- [ ] **Step 1: Write the failing tests**

In `internal/runner/maintenance_test.go`, replace the whole of `TestStartPruneRefusedDuringAutomaticPrune`, including its comment, with:

```go
// tickInBackground runs one tick and fails the test if it does not return:
// a tick must never wait for a Docker prune.
func tickInBackground(t *testing.T, h *harness) {
	t.Helper()
	ticked := make(chan struct{})
	go func() {
		h.m.Tick(context.Background())
		close(ticked)
	}()
	waitOrFail(t, ticked, "the tick to return while the prune runs")
}

// An automatic prune runs in the background and holds the reservation: the
// tick returns while it runs, StartPrune is refused until it ends, and a later
// disk check does not start a second one.
func TestAutomaticPruneRunsOffTheTick(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{99, 99, 50}
	entered, release := holdOn(t, h, "PruneBuildCacheOlderThan")
	h.m.ticks = 0 // the next tick is tick 1, a disk check
	tickInBackground(t, h)
	waitOrFail(t, entered, "the automatic prune")
	if err := h.m.StartPrune(); !errors.Is(err, ErrPruneRunning) {
		t.Fatalf("StartPrune during an automatic prune: %v", err)
	}
	if ms := h.m.Maintenance(); ms.Running || ms.LastStarted != nil {
		t.Fatalf("a refused StartPrune changed the state: %+v", ms)
	}
	h.m.ticks = 10 // the next tick is another disk check
	tickInBackground(t, h)
	if got := h.docker.pruneList(); got != "until=72h" {
		t.Fatalf("a second automatic prune started: %s", got)
	}
	release()
	h.m.Wait()
	if err := h.m.StartPrune(); err != nil {
		t.Fatalf("StartPrune after the automatic prune: %v", err)
	}
	h.m.Wait()
}

// A Docker daemon that does not answer must not hold the tick for the command
// timeout: the tick's own disk reading has a short bound. The bound is a real
// timer, shortened here; nothing sleeps.
func TestAHungDiskMeasurementDoesNotHoldTheTick(t *testing.T) {
	h := newHarness(t)
	orig := diskMeasureTimeout
	diskMeasureTimeout = 20 * time.Millisecond
	t.Cleanup(func() { diskMeasureTimeout = orig })
	holdOn(t, h, "DataRootUsage")
	h.m.ticks = 0
	tickInBackground(t, h)
	if !strings.Contains(h.eventText(), "warn  disk usage: context deadline exceeded") {
		t.Fatalf("events:\n%s", h.eventText())
	}
	if !h.m.reservePrune() {
		t.Fatal("the tick kept the prune reservation")
	}
	h.m.releasePrune()
}

func TestCloseInterruptsABackgroundAutomaticPrune(t *testing.T) {
	h := newHarness(t)
	h.docker.usage = []int{99}
	entered, _ := holdOn(t, h, "PruneBuildCacheOlderThan")
	h.m.ticks = 0
	tickInBackground(t, h)
	waitOrFail(t, entered, "the automatic prune")
	h.m.Close()
	h.m.Wait()
	if lp := h.m.LastPrune(); lp == nil || lp.Outcome != "interrupted" {
		t.Fatalf("last prune %+v", lp)
	}
}
```

In `internal/runner/fakes_test.go`, let the fake's disk reading be held like its prunes. Replace

```go
func (f *fakeDocker) DataRootUsage(context.Context) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
```

with

```go
func (f *fakeDocker) DataRootUsage(ctx context.Context) (int, error) {
	if err := f.wait(ctx, "DataRootUsage"); err != nil {
		return 0, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -run 'TestAutomaticPruneRunsOffTheTick|TestCloseInterruptsABackgroundAutomaticPrune|TestAHungDiskMeasurementDoesNotHoldTheTick' -count=1`
Expected: FAIL to compile with `undefined: diskMeasureTimeout`. To see the behaviour fail first, add the `diskMeasureTimeout` variable from Step 3 alone and run again: each test then FAILS after about 5 seconds with `timed out waiting for the tick to return while the prune runs`

- [ ] **Step 3: Write the implementation**

In `internal/runner/cleanup.go`, replace the start of `checkDisk`, from its comment through the closing brace of `if pct <= cfg.DiskHighWater {`:

```go
// checkDisk prunes Docker build cache and dangling images above disk_high_water.
// A failed usage read keeps the last good measurement; every failure is reported.
func (m *Manager) checkDisk(ctx context.Context, cfg *config.Config) {
	u, err := m.dataRoot(ctx)
	if err != nil {
		m.Events.Add("warn", "", "disk usage: %v", err)
		return
	}
	m.setDisk(u)
	pct := u.Pct
	if pct <= cfg.DiskHighWater {
		return
	}
```

with

```go
// diskMeasureTimeout bounds the tick's own disk reading (docker info and df),
// so a Docker daemon that does not answer costs a tick seconds, not the
// command timeout.
var diskMeasureTimeout = 15 * time.Second

// measureDisk reads the Docker data root's usage and reports whether it is
// above disk_high_water. A failed read keeps the last good measurement.
func (m *Manager) measureDisk(ctx context.Context, cfg *config.Config) (pct int, over bool) {
	ctx, cancel := context.WithTimeout(ctx, diskMeasureTimeout)
	defer cancel()
	u, err := m.dataRoot(ctx)
	if err != nil {
		m.Events.Add("warn", "", "disk usage: %v", err)
		return 0, false
	}
	m.setDisk(u)
	return u.Pct, u.Pct > cfg.DiskHighWater
}

// pruneDisk prunes Docker build cache and dangling images after measureDisk
// found the disk at pct, above disk_high_water. Every failure is reported.
func (m *Manager) pruneDisk(ctx context.Context, cfg *config.Config, pct int) {
```

The rest of the old function body, from `started := m.Now()` to its closing brace, stays as the body of `pruneDisk`, unchanged.

In `internal/runner/manager.go`, add after `releasePrune`:

```go
// startDiskPrune runs pruneDisk in the background and releases the prune
// reservation when it ends; the caller must hold the reservation. Docker's
// prune commands can take minutes, and a tick that waited for them would
// spawn and reap nothing meanwhile. It reports false, starting nothing, once
// Close has been called.
func (m *Manager) startDiskPrune(cfg *config.Config, pct int) bool {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return false
	}
	ctx := m.maintCtx
	// Counted under the lock that guards closed, so Wait cannot miss it.
	m.wg.Add(1)
	m.mu.Unlock()
	go func() {
		defer m.wg.Done()
		defer m.releasePrune()
		m.pruneDisk(ctx, cfg, pct)
	}()
	return true
}
```

In `internal/runner/tick.go`, in `Tick`, replace

```go
	// Manual and automatic pruning share one reservation, so neither runs
	// Docker's prune commands or rewrites the history file under the other;
	// a skipped automatic prune runs on a later tick.
	if (diskDue || retentionDue) && m.reservePrune() {
		func() {
			defer m.releasePrune()
			if diskDue {
				m.checkDisk(ctx, cfg)
			}
			if retentionDue {
				m.prune(cfg, now)
			}
		}()
	}
```

with

```go
	// Manual and automatic pruning share one reservation, so neither runs
	// Docker's prune commands or rewrites the history file under the other;
	// a skipped automatic prune runs on a later tick. The tick measures the
	// disk and prunes history itself; Docker prunes run in the background,
	// which then holds the reservation until they end.
	if (diskDue || retentionDue) && m.reservePrune() {
		handedOver := false
		if retentionDue {
			m.prune(cfg, now)
		}
		if diskDue {
			if pct, over := m.measureDisk(ctx, cfg); over {
				handedOver = m.startDiskPrune(cfg, pct)
			}
		}
		if !handedOver {
			m.releasePrune()
		}
	}
```

- [ ] **Step 4: Keep the tests that called `checkDisk` directly**

In `internal/runner/fakes_test.go`, add after `eventText`:

```go
// checkDisk measures the disk and, above the high-water mark, prunes in the
// calling goroutine: what a tick does across its background prune.
func (h *harness) checkDisk(ctx context.Context) {
	if pct, over := h.m.measureDisk(ctx, h.cfg); over {
		h.m.pruneDisk(ctx, h.cfg, pct)
	}
}
```

Then replace the twelve direct calls:

```bash
sed -i -E 's/h\.m\.checkDisk\((.*), h\.cfg\)/h.checkDisk(\1)/' internal/runner/cleanup_test.go internal/runner/prune_scope_test.go
rg -n 'm\.checkDisk\(' internal/
```

Expected: `rg` prints nothing.

- [ ] **Step 5: Run the tests to verify they pass, with the race detector**

Run: `PATH=/usr/local/go/bin:$PATH go test -race ./internal/runner/ -count=1`
Expected: PASS with no `DATA RACE` report

- [ ] **Step 6: Commit**

```bash
git add internal/runner/cleanup.go internal/runner/manager.go internal/runner/tick.go internal/runner/fakes_test.go internal/runner/maintenance_test.go internal/runner/cleanup_test.go internal/runner/prune_scope_test.go
git commit -m "fix(runner): run automatic Docker prunes off the tick"
```

### Task 12: Cleanup count and give-up event

**Files:**
- Modify: `internal/runner/manager.go` (`instance`), `internal/runner/lifecycle.go` (`finish`)
- Test: `internal/runner/lifecycle_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names

**Items:** 12, 13

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

- [ ] **Step 1: Write the failing tests**

Append to `internal/runner/lifecycle_test.go`:

```go
// The history record counts every container the cleanup removed, not only
// what its last attempt removed, even when the daemon restarted in between.
func TestCleanupCountSumsAttemptsAcrossARestart(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	h.gh.setJobs(55, completedJob(h, 55, "ghr-darkcloud-aaaaaa", "success"))
	h.docker.byLabel["com.docker.compose.project=ghr-aaaaaa"] = []string{"c1", "c2"}
	h.docker.setErr("RemoveNetworksByLabel", errors.New("network busy"))
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	h.docker.byLabel = map[string][]string{} // the containers are gone now
	h.docker.setErr("RemoveNetworksByLabel", nil)
	h.restart(t)
	if err := h.m.Adopt(context.Background()); err != nil { // the unit is gone: cleanup resumes
		t.Fatal(err)
	}
	h.m.Wait()
	if h.state("aaaaaa") != "gone" || !strings.Contains(h.eventText(), "cleanup: 2 ctrs") {
		t.Fatalf("state %s\n%s", h.state("aaaaaa"), h.eventText())
	}
}

// Once cleanup has given up, a later step that keeps failing retries every
// 30 s; the give-up itself is said once.
func TestCleanupGiveUpIsReportedOnce(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkcloud")
	h.writeJob(t, "aaaaaa", 55)
	h.docker.setErr("RemoveNetworksByLabel", errors.New("network busy"))
	h.sd.active["ghr-runner-aaaaaa"] = false
	for n := 1; n < maxCleanupFailures; n++ {
		h.m.refreshUnits(context.Background())
		h.m.Wait()
		h.now = h.now.Add(finishRetry)
	}
	orig := writeFile
	t.Cleanup(func() { writeFile = orig })
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasPrefix(name, h.m.Paths.Pending) {
			return &fs.PathError{Op: "write", Path: name, Err: syscall.EIO}
		}
		return orig(name, data, perm)
	}
	for range 3 {
		h.m.refreshUnits(context.Background())
		h.m.Wait()
		h.now = h.now.Add(finishRetry)
	}
	if n := strings.Count(h.eventText(), "releasing its slot"); n != 1 || h.state("aaaaaa") != "cleaning" {
		t.Fatalf("give-up said %d times, state %s\n%s", n, h.state("aaaaaa"), h.eventText())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -run 'TestCleanupCountSumsAttemptsAcrossARestart|TestCleanupGiveUpIsReportedOnce' -count=1`
Expected: FAIL: the first shows `cleanup: 0 ctrs` in its events, the second `give-up said 3 times`

- [ ] **Step 3: Write the implementation**

In `internal/runner/manager.go`, add to `instance` after `cleanupFails`:

```go
	gaveUp       bool      // the cleanup give-up has been reported
```

In `internal/runner/lifecycle.go`, in `finish`, replace

```go
	removed, err := m.cleanupDocker(ctx, cfg, id, i.Repo)
	fails := 0
	m.mu.Lock()
	if ip, ok := m.insts[id]; ok {
		if errors.Is(err, errListProjects) {
			// Docker unreachable: giving up would delete the instance dir and
			// leak the job's resources, so wait it out without counting.
		} else if err != nil {
			ip.cleanupFails++
		} else {
			ip.cleanupFails = 0
		}
		fails = ip.cleanupFails
	}
	m.mu.Unlock()
	if err != nil {
		// A resource held from outside the compose project (a foreign container
		// on its network, say) fails every retry; holding the repo slot forever
		// would stall the repo, so give up and leave the rest to the operator.
		if fails < maxCleanupFailures {
			return fmt.Errorf("docker cleanup: %w", err)
		}
		m.Events.Add("error", i.Repo, "cleanup of %s failed %d times in a row; releasing its slot, remove the leftovers by hand: %v",
			id, fails, err)
	}
	if hadJob {
		p.Cleanup = removed
		if err := writeJSONAtomic(pendingPath, p); err != nil {
			return fmt.Errorf("write pending history: %w", err)
		}
	}
```

with

```go
	removed, err := m.cleanupDocker(ctx, cfg, id, i.Repo)
	if hadJob && removed > 0 {
		// Summed in the pending record, which the start of finish reads back:
		// a retry, even one after a restart, adds to what earlier attempts
		// removed. A failed write here loses this attempt's count only.
		p.Cleanup += removed
		if werr := writeJSONAtomic(pendingPath, p); werr != nil && err == nil {
			return fmt.Errorf("write pending history: %w", werr)
		}
	}
	fails, announce := 0, false
	m.mu.Lock()
	if ip, ok := m.insts[id]; ok {
		if errors.Is(err, errListProjects) {
			// Docker unreachable: giving up would delete the instance dir and
			// leak the job's resources, so wait it out without counting.
		} else if err != nil {
			ip.cleanupFails++
		} else {
			ip.cleanupFails = 0
		}
		fails = ip.cleanupFails
		if err != nil && fails >= maxCleanupFailures && !ip.gaveUp {
			ip.gaveUp, announce = true, true
		}
	}
	m.mu.Unlock()
	if err != nil {
		// A resource held from outside the compose project (a foreign container
		// on its network, say) fails every retry; holding the repo slot forever
		// would stall the repo, so give up and leave the rest to the operator.
		if fails < maxCleanupFailures {
			return fmt.Errorf("docker cleanup: %w", err)
		}
		// A later step that fails brings finish back here on every retry.
		if announce {
			m.Events.Add("error", i.Repo, "cleanup of %s failed %d times in a row; releasing its slot, remove the leftovers by hand: %v",
				id, fails, err)
		}
	}
	if hadJob {
		if err := writeJSONAtomic(pendingPath, p); err != nil {
			return fmt.Errorf("write pending history: %w", err)
		}
	}
```

The count in the `runner %s exited without a job; cleanup: %d ctrs` event stays this attempt's `removed`: an instance without a job has no pending record to carry a sum.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runner/manager.go internal/runner/lifecycle.go internal/runner/lifecycle_test.go
git commit -m "fix(runner): sum cleanup counts, report give-up once"
```

### Task 13: Reconcile and other hosts

**Files:**
- Modify: `internal/runner/manager.go` (`Manager`), `internal/runner/adopt.go` (`Reconcile`)
- Test: `internal/runner/adopt_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names

**Items:** 11

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 2 = 3

- [ ] **Step 1: Write the failing test**

Append to `internal/runner/adopt_test.go`:

```go
// A second ghr host on the same repository names its runners the same way.
// Reconcile deletes only a registration it has seen offline twice in a row.
func TestReconcileLeavesAnotherHostsRunners(t *testing.T) {
	h := newHarness(t)
	h.gh.listed["darkmem"] = []github.Runner{
		{ID: 21, Name: "ghr-darkmem-111111", Status: "online"},  // the other host's idle runner
		{ID: 22, Name: "ghr-darkmem-222222", Status: "offline"}, // the other host's runner, just starting
		{ID: 23, Name: "ghr-darkmem-333333", Status: "offline"}, // a leftover
		{ID: 24, Name: "ghr-darkmem-444444"},                    // no status given: not known to be offline
	}
	h.m.Reconcile(context.Background(), h.cfg)
	if len(h.gh.deleted) != 0 {
		t.Fatalf("deleted on first sight: %v", h.gh.deleted)
	}
	h.gh.listed["darkmem"][1].Status = "online" // it came online meanwhile
	h.m.Reconcile(context.Background(), h.cfg)
	if !reflect.DeepEqual(h.gh.deleted, []int64{23}) {
		t.Fatalf("deleted %v, want only the leftover", h.gh.deleted)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -run TestReconcileLeavesAnotherHostsRunners -count=1`
Expected: FAIL with `deleted on first sight: [21 22 23 24]`

- [ ] **Step 3: Write the implementation**

In `internal/runner/manager.go`, add to the `Manager` struct after `lastJob`:

```go
	orphans        map[int64]bool // untracked registrations seen offline at the last reconcile; guarded by mu
```

In `internal/runner/adopt.go`, in `Reconcile`, replace the whole loop over `cfg.Repos` (from `for _, r := range cfg.Repos {` to its closing brace, the line before `entries, err := os.ReadDir(m.Paths.Instances)`) with:

```go
	m.mu.Lock()
	before := m.orphans
	m.mu.Unlock()
	seen := map[int64]bool{}
	for _, r := range cfg.Repos {
		runners, err := m.GH.ListRunners(ctx, r.Name)
		if err != nil {
			m.Events.Add("warn", r.Name, "reconcile: list runners: %v", err)
			continue
		}
		prefix := "ghr-" + r.Name + "-"
		for _, rn := range runners {
			id := strings.TrimPrefix(rn.Name, prefix)
			if !strings.HasPrefix(rn.Name, prefix) || !idRe.MatchString(id) {
				continue
			}
			i, tracked := known[id]
			if tracked && strings.EqualFold(i.Repo, r.Name) && i.RunnerID == rn.ID {
				continue
			}
			// Another ghr host serving this repository names its runners the
			// same way. Its live runners are online and a starting one is
			// offline only briefly, so a registration is deleted only when
			// GitHub called it offline at two reconciles in a row.
			if rn.Status != "offline" {
				continue
			}
			seen[rn.ID] = true
			if !tracked && running[id] {
				continue // an untracked unit that failed to stop may still use it
			}
			if !before[rn.ID] {
				continue
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
	m.mu.Lock()
	m.orphans = seen
	m.mu.Unlock()
```

Change the comment above `Reconcile` to:

```go
// Reconcile works from the real unit inventory: it stops running units no
// instance tracks, deletes ghr-<repo>-<id> registrations that match neither a
// tracked instance (same repo and runner ID) nor a running unit once they
// have been offline at two reconciles in a row, and removes instance dirs
// whose unit has confirmed exited.
```

- [ ] **Step 4: Update the two tests that expected a deletion at the first reconcile**

In `internal/runner/adopt_test.go`:
- in `TestReconcileDeletesStaleRegistrationsAndDirs`, add `Status: "offline"` to the runners with IDs 5 and 8 (`{ID: 5, Name: "ghr-darkcloud-bbbbbb", Status: "offline"}` and `{ID: 8, Name: "ghr-darkmem-aaaaaa", Status: "offline"}`), and add a second `h.m.Reconcile(context.Background(), h.cfg)` directly after the existing one;
- in `TestReconcileStopsUntrackedUnitBeforeDeletingRegistration`, make the listed runner `{ID: 3, Name: "ghr-darkmem-ffffff", Status: "offline"}` and add a second `h.m.Reconcile(context.Background(), h.cfg)` directly after the existing one;
- in `TestAdoptKeepsDirWhenStopFails`, make the listed runner `{ID: 9, Name: "ghr-darkmem-cccccc", Status: "offline"}`. It already reconciles twice.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ ./internal/daemon/ -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runner/manager.go internal/runner/adopt.go internal/runner/adopt_test.go
git commit -m "fix(runner): spare another host's runners in reconcile"
```

### Task 14: The root-only directory

**Files:**
- Create: `internal/runner/private.go`, `internal/runner/private_test.go`
- Modify: `internal/runner/manager.go` (`Paths`), `internal/daemon/run.go` (`DefaultOptions`)
- Test: `internal/runner/fakes_test.go` (`newHarness`), `internal/daemon/run_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `Paths.Private`, `privateMeta`, `privateProjects`, `removePrivate` (Contracts)

**Items:** 10, 177

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Create `internal/runner/private_test.go`:

```go
package runner

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemovePrivateDeletesBothRecords(t *testing.T) {
	h := newHarness(t)
	if h.m.Paths.Private == "" || filepath.Dir(h.m.Paths.Private) == h.m.Paths.Instances {
		t.Fatalf("private dir %q must be set and outside the instance dirs", h.m.Paths.Private)
	}
	if err := os.MkdirAll(h.m.Paths.Private, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{h.m.privateMeta("aaaaaa"), h.m.privateProjects("aaaaaa")} {
		if filepath.Dir(p) != h.m.Paths.Private {
			t.Fatalf("%s is not in the private dir", p)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h.m.removePrivate("aaaaaa")
	if entries, _ := os.ReadDir(h.m.Paths.Private); len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
}
```

Append to `internal/daemon/run_test.go`:

```go
func TestDefaultOptionsHaveARootOnlyRecordDir(t *testing.T) {
	if got := DefaultOptions().Paths.Private; got != "/var/lib/ghr/private" {
		t.Fatalf("private dir %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ ./internal/daemon/ -run 'TestRemovePrivateDeletesBothRecords|TestDefaultOptionsHaveARootOnlyRecordDir' -count=1`
Expected: FAIL to compile with `h.m.Paths.Private undefined`

- [ ] **Step 3: Write the implementation**

In `internal/runner/manager.go`, add to `Paths` after `Pending`:

```go
	// Private holds what root must trust about an instance, mode 0700. The
	// instance directory itself belongs to the runner user.
	Private string
```

Create `internal/runner/private.go`:

```go
package runner

import (
	"os"
	"path/filepath"
)

// The instance directory belongs to the runner user, so a job can rewrite
// anything in it. What root must trust about an instance is kept in
// Paths.Private instead, which only root can write.

func (m *Manager) privateMeta(id string) string {
	return filepath.Join(m.Paths.Private, id+".json")
}

func (m *Manager) privateProjects(id string) string {
	return filepath.Join(m.Paths.Private, id+".projects")
}

// removePrivate deletes an instance's private records.
func (m *Manager) removePrivate(id string) {
	os.Remove(m.privateMeta(id))
	os.Remove(m.privateProjects(id))
}
```

In `internal/daemon/run.go`, in `DefaultOptions`, add to the `runner.Paths` literal after `Pending`:

```go
			Private:     "/var/lib/ghr/private",
```

In `internal/runner/fakes_test.go`, in `newHarness`, add `Private: filepath.Join(root, "private"),` to the `Paths` literal after the `Pending` entry.

In `internal/daemon/run_test.go`, add `Private: filepath.Join(dir, "private"),` after the `Pending: filepath.Join(dir, "pending"),` entry in both `runner.Paths{...}` literals (near lines 186 and 260).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ ./internal/daemon/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runner/private.go internal/runner/private_test.go internal/runner/manager.go internal/runner/fakes_test.go internal/daemon/run.go internal/daemon/run_test.go
git commit -m "feat(runner): add a root-only instance record dir"
```

### Task 15: Compose project list moves

**Files:**
- Modify: `internal/runner/cleanup.go` (`projectsFile`, `projects`, `cleanupDocker`), `internal/runner/lifecycle.go` (`finish`)
- Test: `internal/runner/cleanup_test.go`, `internal/runner/lifecycle_test.go` (`diskFullUntilCleanup`)

**Interfaces:**
- Consumes: `Paths.Private`, `privateProjects`, `removePrivate` (Contracts)
- Produces: nothing a later task names

**Items:** 10

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 3 = 5

- [ ] **Step 1: Write the failing tests**

Append to `internal/runner/cleanup_test.go`:

```go
// The instance directory is the job's to write. A project list found there
// must not make root remove someone else's compose project.
func TestCleanupIgnoresAProjectListTheJobWrote(t *testing.T) {
	h := newHarness(t)
	dir := h.m.instanceDir("aaaaaa")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ghr-projects"), []byte("immich\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := h.m.cleanupDocker(context.Background(), h.cfg, "aaaaaa", "darkmem"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"com.docker.compose.project=ghr-aaaaaa"}; !reflect.DeepEqual(h.docker.netLabels, want) {
		t.Fatalf("networks removed for %v", h.docker.netLabels)
	}
}

// The remembered list lives in the root-only directory while a cleanup is
// being retried, and goes when the instance does.
func TestRememberedProjectsLiveInThePrivateDir(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	h.docker.setErr("RemoveNetworksByLabel", errors.New("network busy"))
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if _, err := os.Stat(h.m.privateProjects("aaaaaa")); err != nil {
		t.Fatalf("no remembered list while the cleanup is retried: %v", err)
	}
	h.docker.setErr("RemoveNetworksByLabel", nil)
	h.now = h.now.Add(finishRetry)
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	if _, err := os.Stat(h.m.privateProjects("aaaaaa")); !os.IsNotExist(err) || h.state("aaaaaa") != "gone" {
		t.Fatalf("state %s, remembered list: %v", h.state("aaaaaa"), err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -run 'TestCleanupIgnoresAProjectListTheJobWrote|TestRememberedProjectsLiveInThePrivateDir' -count=1`
Expected: FAIL: the first with `networks removed for [... immich]`, the second with `no remembered list while the cleanup is retried`

- [ ] **Step 3: Write the implementation**

In `internal/runner/cleanup.go`, delete the constant and its comment:

```go
// projectsFile remembers the compose projects cleanup has seen: custom projects
// are found only through their containers, so a retry after those are removed
// must still reach the project's networks and volumes.
const projectsFile = "ghr-projects"
```

In `projects`, replace

```go
	if b, err := os.ReadFile(filepath.Join(dir, projectsFile)); err == nil {
```

with

```go
	// Cleanup remembers the projects it has seen: custom projects are found
	// only through their containers, so a retry after those are removed must
	// still reach the project's networks and volumes. The list is kept where
	// the job cannot write, or a job could name any project on the host.
	if b, err := os.ReadFile(m.privateProjects(id)); err == nil {
```

In `cleanupDocker`, replace

```go
	known := filepath.Join(m.instanceDir(id), projectsFile)
	record := func() error {
		if err := writeFile(known, []byte(strings.Join(names, "\n")+"\n"), 0o644); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("record compose projects: %w", err)
		}
		return nil
	}
```

with

```go
	record := func() error {
		err := os.MkdirAll(m.Paths.Private, 0o700)
		if err == nil {
			err = writeFile(m.privateProjects(id), []byte(strings.Join(names, "\n")+"\n"), 0o600)
		}
		if err != nil {
			return fmt.Errorf("record compose projects: %w", err)
		}
		return nil
	}
```

In `internal/runner/lifecycle.go`, in `finish`, replace

```go
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
```

with

```go
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	m.removePrivate(id)
```

- [ ] **Step 4: Update the full-disk test helper**

In `internal/runner/lifecycle_test.go`, in `diskFullUntilCleanup`, replace `filepath.Base(name) == projectsFile` with `strings.HasSuffix(name, ".projects")`.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/runner/cleanup.go internal/runner/lifecycle.go internal/runner/cleanup_test.go internal/runner/lifecycle_test.go
git commit -m "fix(runner): keep the project list out of job reach"
```

### Task 16: Instance metadata moves

**Files:**
- Modify: `internal/runner/private.go`, `internal/runner/lifecycle.go` (`spawn`), `internal/runner/adopt.go` (`Adopt`, `Reconcile`)
- Test: `internal/runner/adopt_test.go`

**Interfaces:**
- Consumes: `Paths.Private`, `privateMeta`, `removePrivate` (Contracts)
- Produces: nothing a later task names

**Items:** 177

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 3 = 5

- [ ] **Step 1: Write the failing tests**

Add `"io/fs"` and `"syscall"` to `internal/runner/adopt_test.go`'s imports, then append:

```go
func adoptedMeta(h *harness, id string) Meta {
	h.m.mu.Lock()
	defer h.m.mu.Unlock()
	return h.m.insts[id].Meta
}

// A job can replace ghr.json in its own directory. After a restart the
// repository and runner ID must still be the ones root recorded at spawn:
// they choose whose containers cleanup removes and which registration is deleted.
func TestAdoptTrustsTheRootOwnedRecordOverTheInstanceCopy(t *testing.T) {
	h := newHarness(t)
	if err := h.m.spawn(context.Background(), h.cfg, "darkmem"); err != nil {
		t.Fatal(err)
	}
	forged := Meta{ID: "aaaaaa", Repo: "darkcloud", RunnerID: 999, RunnerName: "ghr-darkcloud-aaaaaa"}
	if err := writeJSON(filepath.Join(h.m.instanceDir("aaaaaa"), MetaFile), forged); err != nil {
		t.Fatal(err)
	}
	h.restart(t)
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := adoptedMeta(h, "aaaaaa"); got.Repo != "darkmem" || got.RunnerID != 101 {
		t.Fatalf("adopted %+v", got)
	}
}

// An instance an older ghr spawned has only the copy in its own directory.
// It is adopted from that copy once, and recorded.
func TestAdoptRecordsAnOlderInstancesMetadataOnce(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkmem", 1, false)
	h.sd.active["ghr-runner-aaaaaa"] = true
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	var rec Meta
	if err := readJSON(filepath.Join(h.m.Paths.Private, "aaaaaa.json"), &rec); err != nil || rec.Repo != "darkmem" || rec.RunnerID != 1 {
		t.Fatalf("recorded %+v: %v", rec, err)
	}
	forged := Meta{ID: "aaaaaa", Repo: "darkcloud", RunnerID: 999, RunnerName: "ghr-darkcloud-aaaaaa"}
	if err := writeJSON(filepath.Join(h.m.instanceDir("aaaaaa"), MetaFile), forged); err != nil {
		t.Fatal(err)
	}
	h.restart(t)
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := adoptedMeta(h, "aaaaaa"); got.Repo != "darkmem" || got.RunnerID != 1 {
		t.Fatalf("adopted %+v", got)
	}
}

// When the record cannot be written, adopting from the instance's own copy
// would mean trusting that copy again at every later start. Adoption fails
// instead, and nothing is stopped or removed.
func TestAdoptFailsRatherThanTrustAnUnrecordedCopy(t *testing.T) {
	h := newHarness(t)
	makeInstance(t, h, "aaaaaa", "darkmem", 1, false)
	h.sd.active["ghr-runner-aaaaaa"] = true
	orig := writeFile
	t.Cleanup(func() { writeFile = orig })
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		if strings.HasPrefix(name, h.m.Paths.Private) {
			return &fs.PathError{Op: "write", Path: name, Err: syscall.EIO}
		}
		return orig(name, data, perm)
	}
	if err := h.m.Adopt(context.Background()); err == nil {
		t.Fatal("adopted an instance whose metadata could not be recorded")
	}
	if len(h.sd.stopped) != 0 {
		t.Fatalf("stopped %v", h.sd.stopped)
	}
	if _, err := os.Stat(h.m.instanceDir("aaaaaa")); err != nil {
		t.Fatalf("instance dir removed: %v", err)
	}
}

func TestPrivateRecordsGoWithTheirInstance(t *testing.T) {
	h := newHarness(t)
	h.m.spawn(context.Background(), h.cfg, "darkmem")
	if _, err := os.Stat(filepath.Join(h.m.Paths.Private, "aaaaaa.json")); err != nil {
		t.Fatalf("spawn recorded nothing: %v", err)
	}
	h.sd.active["ghr-runner-aaaaaa"] = false
	h.m.refreshUnits(context.Background())
	h.m.Wait()
	// Records of an instance whose directory is gone are swept at the next start.
	os.WriteFile(filepath.Join(h.m.Paths.Private, "eeeeee.json"), []byte("{}"), 0o600)
	os.WriteFile(filepath.Join(h.m.Paths.Private, "eeeeee.projects"), []byte("x\n"), 0o600)
	if err := h.m.Adopt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(h.m.Paths.Private); len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -run 'TestAdoptTrustsTheRootOwnedRecord|TestAdoptRecordsAnOlderInstances|TestAdoptFailsRatherThanTrust|TestPrivateRecordsGoWithTheirInstance' -count=1`
Expected: FAIL: `adopted {... Repo:darkcloud RunnerID:999 ...}`, `recorded ...: open ...aaaaaa.json: no such file or directory`, `adopted an instance whose metadata could not be recorded`, `spawn recorded nothing`

- [ ] **Step 3: Write the implementation**

Replace `internal/runner/private.go`'s import block with

```go
import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)
```

and append to the file:

```go
// writePrivateMeta records an instance's metadata where only root can write.
func (m *Manager) writePrivateMeta(meta Meta) error {
	if err := os.MkdirAll(m.Paths.Private, 0o700); err != nil {
		return err
	}
	return writeJSONAtomic(m.privateMeta(meta.ID), meta)
}

// errRecordMeta marks a failure to write an instance's private record.
var errRecordMeta = errors.New("record instance metadata")

// readMeta returns the metadata root recorded for an instance. An instance an
// older ghr spawned has only the copy in its own directory; that copy is
// trusted this once and recorded, so rewriting it later changes nothing. If
// it cannot be recorded the error wraps errRecordMeta: trusting the copy
// without recording it would mean trusting it again at every later start.
func (m *Manager) readMeta(id string) (Meta, error) {
	var meta Meta
	err := readJSON(m.privateMeta(id), &meta)
	if errors.Is(err, fs.ErrNotExist) {
		err = readJSON(filepath.Join(m.instanceDir(id), MetaFile), &meta)
		if err == nil && meta.ID == id {
			if werr := m.writePrivateMeta(meta); werr != nil {
				return Meta{}, fmt.Errorf("%w for %s: %w", errRecordMeta, id, werr)
			}
		}
	}
	if err == nil && meta.ID != id {
		err = fmt.Errorf("%s names instance %q", MetaFile, meta.ID)
	}
	return meta, err
}

// sweepPrivate removes the private records of instances that are gone: no
// live instance and no instance directory.
func (m *Manager) sweepPrivate() {
	entries, err := os.ReadDir(m.Paths.Private)
	if err != nil {
		return
	}
	for _, e := range entries {
		id, _, _ := strings.Cut(e.Name(), ".")
		if !idRe.MatchString(id) {
			continue
		}
		m.mu.Lock()
		_, live := m.insts[id]
		m.mu.Unlock()
		if !live && !exists(m.instanceDir(id)) {
			os.Remove(filepath.Join(m.Paths.Private, e.Name()))
		}
	}
}
```

In `internal/runner/lifecycle.go`, in `spawn`, replace

```go
	fail := func(err error) error {
		os.RemoveAll(dir)
```

with

```go
	fail := func(err error) error {
		os.RemoveAll(dir)
		m.removePrivate(id)
```

and replace

```go
	if err := writeJSON(filepath.Join(dir, MetaFile), meta); err != nil {
		return fail(err)
	}
```

with

```go
	if err := m.writePrivateMeta(meta); err != nil {
		return fail(err)
	}
	// A copy for a person reading the directory; nothing trusts it while the
	// private record exists.
	if err := writeJSON(filepath.Join(dir, MetaFile), meta); err != nil {
		return fail(err)
	}
```

In `internal/runner/adopt.go`, in `Adopt`, replace

```go
		var meta Meta
		if err := readJSON(filepath.Join(dir, MetaFile), &meta); err != nil || meta.ID != id {
```

with

```go
		meta, err := m.readMeta(id)
		if errors.Is(err, errRecordMeta) {
			return err
		}
		if err != nil {
```

Add `"errors"` to `adopt.go`'s imports. Directly before that loop (the `for _, e := range entries {` that it sits in), add a first pass, so a record that cannot be written stops adoption before anything has been stopped or removed:

```go
	// Recording comes first, for every instance: when it fails, the runners
	// keep running and the daemon does not start until the directory can be
	// written.
	for _, e := range entries {
		if e.IsDir() && idRe.MatchString(e.Name()) {
			if _, err := m.readMeta(e.Name()); errors.Is(err, errRecordMeta) {
				return err
			}
		}
	}
```

A private record that exists but cannot be read or parsed, or that names another instance, is not a reason to fall back to the instance's own copy: `readMeta` returns that error, and the instance takes the existing "no readable `ghr.json`" branch (its unit is stopped and its directory removed), as an instance with unreadable metadata does today.

Inside the `if err != nil {` branch, replace

```go
			if err := os.RemoveAll(dir); err != nil {
				m.Events.Add("warn", "", "instance %s had no readable %s; its dir could not be removed (%v); reconciliation retries", id, MetaFile, err)
			} else {
				m.Events.Add("warn", "", "instance %s had no readable %s; stopped and removed", id, MetaFile)
			}
```

with

```go
			if err := os.RemoveAll(dir); err != nil {
				m.Events.Add("warn", "", "instance %s had no readable %s; its dir could not be removed (%v); reconciliation retries", id, MetaFile, err)
			} else {
				m.removePrivate(id)
				m.Events.Add("warn", "", "instance %s had no readable %s; stopped and removed", id, MetaFile)
			}
```

Still in `Adopt`, add `m.sweepPrivate()` directly before the final `m.Events.Add("info", "", "adopted %d running runners; ...` line.

In `Reconcile`, add `m.sweepPrivate()` as the last statement of the function.

Change the comment above `Meta` in `internal/runner/manager.go` from `// Meta is written to <instance>/ghr.json at spawn and read back on re-adoption.` to:

```go
// Meta is recorded in Paths.Private at spawn and read back on re-adoption;
// <instance>/ghr.json holds a copy.
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ ./internal/daemon/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/runner/private.go internal/runner/lifecycle.go internal/runner/adopt.go internal/runner/manager.go internal/runner/adopt_test.go
git commit -m "fix(runner): trust only root's instance metadata"
```

### Task 17: Runner log reader

**Files:**
- Modify: `internal/runner/cleanup.go` (`RunnerLog`, `readFrom`)
- Test: `internal/runner/runnerlog_unix_test.go` (new)

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names

**Items:** 10

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 3 = 4

- [ ] **Step 1: Write the failing tests**

Create `internal/runner/runnerlog_unix_test.go` (FIFOs and unprivileged symlinks are Unix-only, so the file carries a build tag):

```go
//go:build unix

package runner

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/darkraise/ghr/internal/model"
)

// readLog runs RunnerLog with a deadline: reading a FIFO nobody writes to
// would block for ever.
func readLog(t *testing.T, h *harness, id string) (model.LogChunk, error) {
	t.Helper()
	type result struct {
		c   model.LogChunk
		err error
	}
	done := make(chan result, 1)
	go func() {
		c, err := h.m.RunnerLog(id, "")
		done <- result{c, err}
	}()
	select {
	case r := <-done:
		return r.c, r.err
	case <-time.After(5 * time.Second):
		t.Fatal("RunnerLog blocked")
		return model.LogChunk{}, nil
	}
}

// The job wrote these files and the daemon reads them as root. Only regular
// files inside the log directory are read.
func TestRunnerLogReadsOnlyRealFilesInsideTheLogDirectory(t *testing.T) {
	h := newHarness(t)
	diag := filepath.Join(h.m.instanceDir("aaaaaa"), "_diag")
	if err := os.MkdirAll(diag, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(h.root, "token")
	if err := os.WriteFile(secret, []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(diag, "Runner_1.log"), []byte("real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(diag, "Runner_2.log")); err != nil {
		t.Fatal(err)
	}
	// A link to a file the job owns, but outside the log directory.
	if err := os.WriteFile(filepath.Join(h.m.instanceDir("aaaaaa"), "notes.txt"), []byte("NOTES"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../notes.txt", filepath.Join(diag, "Runner_3.log")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(diag, "Worker_1.log"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := readLog(t, h, "aaaaaa")
	if err != nil || c.Data != "==> Runner_1.log <==\nreal\n" {
		t.Fatalf("data %q err %v", c.Data, err)
	}
}

// The archive directory is root's, but its files came from the job.
func TestRunnerLogSkipsASymlinkInTheArchive(t *testing.T) {
	h := newHarness(t)
	archive := filepath.Join(h.m.Paths.Logs, "aaaaaa")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(h.root, "token")
	if err := os.WriteFile(secret, []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archive, "Runner_1.log"), []byte("real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(archive, "Worker_1.log")); err != nil {
		t.Fatal(err)
	}
	c, err := readLog(t, h, "aaaaaa")
	if err != nil || c.Data != "==> Runner_1.log <==\nreal\n" {
		t.Fatalf("data %q err %v", c.Data, err)
	}
}

func TestRunnerLogRefusesASymlinkedLogDirectory(t *testing.T) {
	h := newHarness(t)
	elsewhere := filepath.Join(h.root, "elsewhere")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(elsewhere, "Runner_1.log"), []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(h.m.instanceDir("aaaaaa"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, filepath.Join(h.m.instanceDir("aaaaaa"), "_diag")); err != nil {
		t.Fatal(err)
	}
	if c, err := readLog(t, h, "aaaaaa"); err == nil {
		t.Fatalf("read through a symlinked log directory: %q", c.Data)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -run 'TestRunnerLogReadsOnlyRealFiles|TestRunnerLogSkipsASymlinkInTheArchive|TestRunnerLogRefusesASymlinkedLogDirectory' -count=1`
Expected: FAIL: the first with `RunnerLog blocked` after 5 seconds, the archive test with data that contains `SECRET`, the last with `read through a symlinked log directory: "==> Runner_1.log <==\nSECRET"`

- [ ] **Step 3: Write the implementation**

In `internal/runner/cleanup.go`, add `"io/fs"` to the imports.

In `RunnerLog`, replace everything from the line `dir := filepath.Join(m.instanceDir(id), "_diag")` through the closing brace of the `for _, pat := range ...` loop:

```go
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
```

with

```go
	root, err := m.openLogs(id)
	if err != nil {
		return model.LogChunk{}, ErrUnknownRunner(id)
	}
	defer root.Close()
	var files []string
	for _, pat := range []string{"Runner_*.log", "Worker_*.log"} {
		matches, _ := fs.Glob(root.FS(), pat)
		sort.Strings(matches)
		files = append(files, matches...)
	}
```

The entries of `files` are now plain file names inside the log directory. Further down in `RunnerLog`, replace `name := filepath.Base(f)` with `name := f`, and `readFrom(f, off, budget)` with `readFrom(root, name, off, budget)`.

Replace the whole `readFrom` function with:

```go
// openLogs opens the directory that holds a runner's diagnostic logs: the
// live instance's _diag, or its archive. The job wrote these files and
// RunnerLog reads them as root, so the log directory itself is opened as an
// os.Root and every read goes through it: the root keeps the directory it
// opened even if its name is swapped afterwards, and refuses any path that
// leaves it through a symlink. A _diag that is a symlink is refused.
func (m *Manager) openLogs(id string) (*os.Root, error) {
	if inst, err := os.OpenRoot(m.instanceDir(id)); err == nil {
		defer inst.Close()
		if fi, err := inst.Lstat("_diag"); err == nil {
			if !fi.IsDir() {
				return nil, fs.ErrInvalid
			}
			return inst.OpenRoot("_diag")
		}
	}
	dir := filepath.Join(m.Paths.Logs, id)
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return nil, fs.ErrNotExist
	}
	return os.OpenRoot(dir)
}

// readFrom reads up to limit bytes of a log from off. Only a regular file is
// read: a symlink is skipped, and a FIFO or a device would block the reader
// or feed it for ever. The check is repeated on the open file, because the
// job can swap the name between the two; whatever it swaps in, the root
// lets the open reach only a file inside the log directory.
func readFrom(root *os.Root, name string, off int64, limit int) ([]byte, error) {
	if fi, err := root.Lstat(name); err != nil || !fi.Mode().IsRegular() {
		return nil, fs.ErrInvalid
	}
	f, err := root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return nil, fs.ErrInvalid
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)))
	if err != nil {
		return nil, err
	}
	return trimPartialRune(b), nil
}
```

`filepath` is still used by `within`, `projects` and `prune`, so its import stays.

What this does and does not guarantee, for the task report: every read is confined to the directory that was opened, whatever the job swaps afterwards; that guarantee comes from `os.Root` and needs no race to be won. A job that swaps `_diag` for a symlink in the instant between the `Lstat` and the `OpenRoot` gets, at most, another directory of its own instance directory opened as the log directory, which holds only files the job itself wrote. Such a swap cannot be staged in a test without a hook in production code, so the tests cover the states a job can leave behind: a symlinked `_diag`, a symlinked log file inside and outside the directory, a FIFO, and the same in the archive.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/runner/ -count=1 && GOOS=windows PATH=/usr/local/go/bin:$PATH go vet ./internal/runner/`
Expected: PASS, and the Windows vet run reports nothing (it proves the package and its tests still compile there)

- [ ] **Step 5: Commit**

```bash
git add internal/runner/cleanup.go internal/runner/runnerlog_unix_test.go
git commit -m "fix(runner): read only real log files as root"
```

### Task 18: Archive ownership

**Files:**
- Modify: `internal/system/system.go` (`Host.Extract`), `deploy/setup.sh` (`install_runner`)
- Test: `internal/system/host_test.go`, `deploy/tests/setup_test.sh`

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names

**Items:** 16

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 0 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

In `internal/system/host_test.go`, in `TestHostExtractAndRunScriptArgs`, replace the expected command

```go
	if len(*calls) != 1 || (*calls)[0].String() != "tar -xzf /d/2.338.0.tmp/runner.tar.gz -C /d/2.338.0.tmp" {
```

with

```go
	if len(*calls) != 1 || (*calls)[0].String() != "tar --no-same-owner -xzf /d/2.338.0.tmp/runner.tar.gz -C /d/2.338.0.tmp" {
```

In `deploy/tests/setup_test.sh`, add directly after the line `sleep() { SECONDS=$((SECONDS + $1)); }`:

```bash
# The tests run without root: record the ownership repair instead of doing it,
# and record how tar is called while still running it.
chown() { echo "$*" >> "$MARK_DIR/chowns"; }
tar() {
  echo "$*" >> "$MARK_DIR/tars"
  command tar "$@"
}
```

Directly after the line `check "a: current points at the new version" [ "$(target_of_current)" = "$FIXTURE_VERSION" ]` (the third `run install_runner` follows it), add:

```bash
check "a: the runner tarball is extracted without its owner ids" contains "$(cat "$MARK_DIR/tars" 2>/dev/null)" "--no-same-owner -xzf"
# The third run finds the version installed: it must not extract again, and it
# must still repair the ownership an older setup.sh left behind.
: > "$MARK_DIR/tars"
: > "$MARK_DIR/chowns"
```

and directly after the line `check "a: third run is a no-op for an installed version" [ "$(count fetches)" -eq 2 ]`:

```bash
check "a: an installed version is not extracted again" lacks "$(cat "$MARK_DIR/tars")" "-xzf"
check "a: an installed version is still handed to root" contains "$(cat "$MARK_DIR/chowns")" "-R root:root $DIST_DIR/$FIXTURE_VERSION"
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/system/ -run TestHostExtractAndRunScriptArgs -count=1`
Expected: FAIL with `Run calls [tar -xzf ...]`

Run: `bash deploy/tests/setup_test.sh; echo "exit=$?"`
Expected: the output has `FAIL a: the runner tarball is extracted without its owner ids` and `FAIL a: an installed version is still handed to root`, ends with `2 assertion(s) failed`, and `exit=1`

- [ ] **Step 3: Write the implementation**

In `internal/system/system.go`, replace `Host.Extract` and its comment with:

```go
// Extract unpacks a .tar.gz into dir. Run by root, tar restores the archive's
// owner ids unless told not to; the runner tarball's are 1001, which may be a
// real user on this host, so the files are made root's.
func (h Host) Extract(ctx context.Context, tarball, dir string) error {
	_, err := h.Run(ctx, "tar", "--no-same-owner", "-xzf", tarball, "-C", dir)
	return err
}
```

In `deploy/setup.sh`, in `install_runner`, replace

```bash
    tar -xzf "$staging/runner.tar.gz" -C "$staging"
```

with

```bash
    tar --no-same-owner -xzf "$staging/runner.tar.gz" -C "$staging"
```

and add, directly after the `fi` that closes `if [ ! -x "$dir/run.sh" ]; then`:

```bash
  # The runner tarball's owner ids (1001) may be a real user here, and a
  # version an older setup.sh extracted kept them.
  chown -R root:root "$dir"
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=/usr/local/go/bin:$PATH go test ./internal/system/ -count=1`
Expected: PASS

Run: `bash deploy/tests/setup_test.sh; echo "exit=$?"`
Expected: the last lines are `all assertions passed` and `exit=0`

- [ ] **Step 5: Commit**

```bash
git add internal/system/system.go internal/system/host_test.go deploy/setup.sh deploy/tests/setup_test.sh
git commit -m "fix(deploy): keep runner files owned by root"
```

### Task 19: Docs and housekeeping

**Files:**
- Modify: `docs/superpowers/specs/2026-10-03-ghr-runner-manager-design.md:250-251,352`, `.gitignore`, `docs/superpowers/registers/2026-10-08-ghr-web-ui-overhaul.md` (row 18)

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 17, 175, 176

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 0 = 1

- [ ] **Step 1: Correct the older spec**

In `docs/superpowers/specs/2026-10-03-ghr-runner-manager-design.md`, replace

```markdown
   name `ghr-<repo>-<id>`, `runner_group_id: 1`, the effective labels minus the
   system ones, `work_folder: _work`. Then `cp -a` dist → instance dir, `chown -R
```

with

```markdown
   name `ghr-<repo>-<id>`, `runner_group_id: 1`, the effective labels (system
   labels included: GitHub gives a just-in-time runner exactly the labels it is
   registered with), `work_folder: _work`. Then `cp -a` dist → instance dir, `chown -R
```

and replace the table row

```markdown
| 401, or other 403 | daemon `degraded`: no spawns, re-check every 60s; TUI red banner |
```

with

```markdown
| 401 | daemon `degraded`: no spawns, re-check every 60s; TUI red banner |
| other 403 | that repo in error ("token lacks permission for this repository"); others continue. Changed 2026-10-11 by docs/superpowers/specs/2026-10-11-ghr-runner-lifecycle-fixes-design.md |
```

- [ ] **Step 2: Ignore the browser tool's output folder**

Append this line to `.gitignore`:

```
.playwright-mcp/
```

Run: `git check-ignore .playwright-mcp/x`
Expected: prints `.playwright-mcp/x`

- [ ] **Step 3: Correct the older register row**

Row 18 of `docs/superpowers/registers/2026-10-08-ghr-web-ui-overhaul.md` says the kit's enter animations ignore reduced motion. Kit 6.9.6 has a global rule (see Assumptions). Keep the row's state and extend its note:

```bash
R=$(ls -d "$HOME"/.claude/plugins/cache/darkraise/dr-superpowers/*/scripts | sort -V | tail -1)
test -f "$R/register" || echo "register script not found under $R"
bash "$R/register" set docs/superpowers/registers/2026-10-08-ghr-web-ui-overhaul.md 18 deferred --note "web UI plan 2 final review minors and parked preflight-13; 2026-10-11: the reduced-motion part no longer applies, kit 6.9.6 dist/styles.css has a global prefers-reduced-motion rule"
bash "$R/register" check docs/superpowers/registers/2026-10-08-ghr-web-ui-overhaul.md
```

The first line finds the newest installed dr-superpowers plugin; if the second line prints its message, stop and report it.

Expected: `register: 0 errors`

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/specs/2026-10-03-ghr-runner-manager-design.md .gitignore docs/superpowers/registers/2026-10-08-ghr-web-ui-overhaul.md
git commit -m "docs(specs): match the runner spec to the code"
```

### Task 20: Full verification and live-check request

**Files:**
- Modify: none

**Interfaces:**
- Consumes: every earlier task
- Produces: nothing

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 0 - spec 1 - coupling 0 - risk 0 = 1

- [ ] **Step 1: Run every suite**

```bash
PATH=/usr/local/go/bin:$PATH gofmt -l cmd internal
PATH=/usr/local/go/bin:$PATH go vet ./...
PATH=/usr/local/go/bin:$PATH go test ./... -count=1
PATH=/usr/local/go/bin:$PATH go test -race ./internal/runner/ ./internal/sched/ -count=1
bash deploy/tests/setup_test.sh; echo "exit=$?"
```

Expected: `gofmt` prints nothing, `go vet` prints nothing, both `go test` runs end in `ok` for every package with no `DATA RACE`, and the shell test ends with `all assertions passed` and `exit=0`.

- [ ] **Step 2: Confirm nothing outside the plan changed**

This plan's first commit has the subject `fix(system): treat a stop of a gone unit as done`. Everything since its parent must lie inside the plan's paths:

```bash
BASE=$(git log --format=%H --grep='^fix(system): treat a stop of a gone unit as done$' -1)
echo "BASE=$BASE"
git diff --name-only "$BASE"^ HEAD | grep -vE '^(internal/(runner|sched|system)/|internal/daemon/run(_test)?\.go$|deploy/setup\.sh$|deploy/tests/setup_test\.sh$|\.gitignore$|docs/superpowers/)'
git status --short --untracked-files=no
```

Expected: the `BASE=` line shows a commit hash, and the `git diff` and `git status` commands print nothing. Any path they print was changed outside this plan: report it.

- [ ] **Step 3: Ask the owner for the live check**

Do not deploy. Report the results of Step 1 and ask the owner whether to deploy this build to the runner LXC for the live check in the spec's §6. With a yes, the check is:

1. `systemctl --version` and `systemctl stop ghr-runner-zzzzzz.service; echo $?` on the LXC: exit status 5 and the words `not loaded`.
2. `id -u ghrunner` on the LXC: whether it is 1001 says if the host was exposed before this change; `stat -c %U /opt/ghr/dist/current/run.sh` after `setup.sh` ran must print `root`.
3. One normal job run to completion, one idle stop, and a queued runner update that starts without a `runner update postponed` event.

