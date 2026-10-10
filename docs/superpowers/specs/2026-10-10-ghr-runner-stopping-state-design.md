# ghr: show a runner as stopping

**Date:** 2026-10-10
**Status:** design approved in chat on 2026-10-10 (approach and full design); revised after the spec review and approved the same day. Amended later that day for register item 33 (the stop starts the cleanup itself: Decisions, §1.1, §1.2, §2 and §4), approved in chat after the Fable review in docs/superpowers/notes/2026-10-10-ghr-stop-starts-cleanup-fable-review.md. Amended once more that day for register item 38 (§1.1 and §4 state two guards the code already had and name `startFinish`), approved in chat; no behaviour changed.
**Register:** docs/superpowers/registers/2026-10-10-ghr-ui-polish-realtime.md (items 25, 33 and 38)
**Code:** `internal/model`, `internal/runner`, `internal/daemon`, `internal/activity`, `cmd/ghr`, `web/src`

## Goal

Stopping a runner must show in the UI in under a second. Today it does not. On the LXC on 2026-10-10 a busy runner stopped with `ghr kill` kept showing as running for 16.2 s: 8.4 s while systemd stopped the unit, during which the daemon reports nothing, then 7.8 s until the manager's next 10-second tick noticed the exit. The change stream itself is fast; the daemon's state is what lags.

## Decisions

- **A flag beside the state, not a new state.** A runner being stopped keeps its state (`starting`, `idle` or `busy`) and gains `stopping`. It still holds its slot until its unit exits, so the scheduler, capacity counts, busy totals and runner-update gating stay as they are. Chosen over a `stopping` state, which every switch on state would have to learn and which loses whether the runner was busy, and over a browser-only indicator, which shows nothing for `ghr kill` or a stop from another tab.
- **The stop starts the cleanup itself.** `Manager.Kill` moves the runner to `cleaning` as soon as systemd confirms its unit is down, without waiting for the manager loop. The first version only woke the loop, and a woken tick waits for a tick in flight: on the LXC a tick takes about 6.5 s (15 GitHub list calls in sequence, three for each of five repositories), and on 2026-10-10 a stopped runner kept reading "stopping" 6.1 s and 7.0 s after its unit was down. The loop is still woken, so a replacement runner starts promptly and an exit systemd did not confirm is retried. Chosen over polling the repositories concurrently, which only shortens the wait (1.9 s measured) and goes against GitHub's advice to make requests serially, and over checking for the exit at points inside a tick, which leaves gaps as long as one repository's calls.
- **Only stops a person asks for.** The flag covers `Manager.Kill`, which the UI's Stop button and `ghr kill` both reach. Stops the daemon makes itself are out of scope (see below).
- **The stop outlives its request.** `Manager.Kill` runs the unit stop on a context its caller cannot cancel. A caller that gives up (Ctrl-C on `ghr kill`, a browser reload, the CLI client's 30-second timeout) would otherwise kill `systemctl` and fail the call while the unit may still be stopping; the flag would clear, nothing would wake the loop, and the runner would read as running again.
- **Stopping is visible without hovering.** Every place that shows a runner shows the flag in what a sighted person sees, the Dashboard lane and a busy runner's list row included, not only in labels and screen-reader text.

## 1. Daemon

### 1.1 Runner manager (`internal/runner`)

- `instance` gains `stopping bool`.
- `Manager.Kill`:
  - looks the instance up as today, and returns `ErrUnknownRunner` for an unknown ID;
  - sets `stopping` under `m.mu` before it calls `SD.Stop`;
  - calls `SD.Stop` with `context.WithoutCancel(ctx)`, so the stop does not end when the caller's context does. `system.CommandTimeout` (10 minutes) still bounds the command;
  - if `SD.Stop` fails, clears `stopping` under `m.mu` when the instance still exists, and returns the error;
  - on success adds the `runner %s stopped by user` event as today, then asks `SD.Active` for the unit on the same uncancellable context. When it answers not active with no error, the test `refreshUnits` applies, `Kill` calls `startFinish(id, true)`, which is `beginFinish` except that it leaves an instance that is already `cleaning` alone: the instance moves to `cleaning` and `finish` runs in the background, as it does for an exit a tick finds. Otherwise `Kill` leaves `stopping` set and the next tick's `refreshUnits` handles the exit.
- `Kill` never starts a finish for an instance that is already `cleaning`. A failed cleanup is retried only when `refreshUnits` finds `retryFinish` due, so repeated kills cannot spend the cleanup's failure allowance.
- `beginFinish` starts nothing once `Close` has been called. It reads `closed` and calls `wg.Add` inside its `m.mu` section, as `StartPruneScope` does, because a `Kill` can outlive the HTTP shutdown and would otherwise race `Wait`. The instance directory stays as it is, and adoption cleans the runner up at the next start.
- `stopStartTimedOut` skips an ID whose instance is gone. Until now an instance could leave `starting` only at the start of a tick; a `Kill` can now move it to `cleaning`, and its finish can remove it, while a tick is in one of its steps.
- `stopIdleRunner` returns at once, counting the runner as stopped, for an instance that is gone or `cleaning`. A `Kill` can move it there after `stopIdle`'s snapshot, and that cleanup deletes the registration itself, so the tick makes no GitHub call and no unit stop for it.
- A second `Kill` of a runner that is already stopping behaves like the first: it calls `SD.Stop` again and returns its result, and a second success adds a second event. If that second call fails it clears the flag even while the first is still in progress; the runner then reads as its state again until its unit exits, which is accepted. Two kills that both succeed start one finish: `beginFinish` returns when a finish is already running.
- `Manager.Status` copies the flag to `InstanceStatus.Stopping` while the state is `starting`, `idle` or `busy`, and never for a `cleaning` instance.
- Nothing else in the manager reads the flag. `refreshUnits` moves the instance to `cleaning` when its unit has exited, whatever the flag says. It starts that cleanup with `startFinish(id, true)` as `Kill` does: its snapshot can predate a cleanup a `Kill` started and that has since failed, and that cleanup waits for its retry time.
- Accepted, because nothing reads the flag:
  - a tick that runs during the stop may pick the runner for the idle timeout or the start timeout, stop its unit again and add its own event. Both stops end the same unit;
  - the scheduler counts a stopping `starting` or `idle` runner as able to take a job until its unit exits, so it starts no replacement before then;
  - the flag stays set until the instance is `cleaning`. If `SD.Stop` succeeds and `SD.Active` then keeps failing, the runner reads "stopping" until `SD.Active` answers. Without the flag it would read as its state and be no nearer to cleanup.

### 1.2 Backend (`internal/daemon`)

- `Backend.KillRunner` calls `b.Wake` (when set) after `Manager.Kill` returns nil, as `update` does after a config change. The wake does not start the cleanup, which `Kill` has already done (§1.1). It makes the next tick run at once, so a replacement runner is started without waiting for the poll interval, and `refreshUnits` handles the exit when `SD.Active` did not confirm it. A tick already in flight finishes first, and the woken tick follows it.
- No change to the change hub. The `status` topic's fingerprint covers `Status.Instances`, so the flag publishes `status` at the hub's next check, within 250 ms.

## 2. Wire

- `model.InstanceStatus` gains `Stopping bool` with JSON `stopping,omitempty`. `GET /status` carries it.
- `model.ActivityRun` gains `Stopping bool` with JSON `stopping,omitempty`. `internal/activity` sets it on the run it builds for a live instance whose `Stopping` is true. Runs built from finished jobs never carry it. Segments are unchanged.
- A cleaning instance never reports `stopping` (§1.1).
- `DELETE /runners/{id}` still answers when the unit has stopped, now after the one `SD.Active` call that follows the stop. A caller that disconnects first no longer ends the stop (§1.1): the stop runs to its end, the flag stays, and the loop is woken.

## 3. Clients

### 3.1 Web UI (`web/src`)

- `api/types.ts`: `InstanceStatus.stopping?: boolean`, `ActivityRun.stopping?: boolean`.
- A stopping runner reads "stopping" wherever its state is shown:
  - `components/runner-list.tsx`: the state light uses the muted colour and does not pulse, and the screen-reader text says "Stopping". The row's second line starts with "Stopping": alone for a runner with no job, and as `Stopping, <job name> #<run number>` for a runner with one, because that line shows the job in place of the state today;
  - `components/runner-panel.tsx`: the state text is "Stopping", in the muted tone `stateTone` gives an unlisted state. The panel's "Stop runner" button is disabled;
  - `components/lanes-chart.tsx`: the open segment of a stopping run is a dashed outline in the muted-foreground colour with no fill, whatever its segment state, and the pulsing end marker is not drawn. A label on it uses the foreground colour. No other state in the chart is an unfilled dashed grey outline;
  - `components/activity-panel.tsx`: the lanes legend gains "Stopping" with that swatch;
  - `lib/activity-view.ts` `runAria`: the state word is `stopping` in place of `starting`, `warm, waiting for a job` or `running`. It feeds the lane's tooltip, its accessible name and the screen-reader table.
- `components/stop-runner-dialog.tsx`: for a stopping runner the title is `Runner <id> is already stopping`, the description is `ghr is stopping it and will clean it up.`, and the Stop button is disabled.
  - With the panel's button disabled, this is the case of a runner that starts stopping, from `ghr kill` or another tab, while the dialog is open.
  - The wording applies only while the dialog is open. A dialog closing after its own Stop keeps the wording it had, as it does for a runner that has finished.
- The UI offers no second stop for a stopping runner. `ghr kill` still sends one (§1.1). A second stop does not end the unit sooner, so this is accepted.
- Unchanged: `lib/capacity.ts`, `lib/summary.ts`, `lib/status.ts`, `components/refused-hint.tsx`, `components/capacity-bar.tsx`. They count by state, and a stopping runner still holds its slot.

### 3.2 Command line (`cmd/ghr`)

- `ghr status` prints `stopping` in the STATE column for an instance whose `Stopping` is true. The JOB column still shows a busy runner's job. The `global` count is unchanged.

## 4. Testing

### Go

- `internal/runner`:
  - while `SD.Stop` is blocked, `Status` reports the instance with `Stopping` true and its state unchanged;
  - a failed `SD.Stop` clears the flag and returns the error;
  - a `Kill` whose context is cancelled while `SD.Stop` is blocked keeps the flag while the stop is blocked, and returns nil once the stop completes;
  - after a successful `Kill`, with no tick run, the instance is `cleaning` and reports no `Stopping`;
  - a `Kill` that completes while a tick is blocked in a GitHub call leaves the instance `cleaning` before that tick ends;
  - when `SD.Active` errors, or answers active, `Kill` starts no finish and the flag stays; the next `refreshUnits` that sees the unit inactive starts it;
  - two concurrent `Kill`s of one runner start one finish;
  - a `Kill` of a `cleaning` instance whose `retryFinish` is not due starts no finish;
  - a `refreshUnits` that took its snapshot before a `Kill`, and asks systemd after that `Kill`'s cleanup has failed, starts no second finish;
  - `stopIdleRunner`, given a snapshot from before a `Kill`, makes no GitHub call and no unit stop for an instance that is `cleaning` or gone;
  - a `Kill` after `Close` starts no finish and leaves the instance directory; `Kill` racing `Close` and `Wait`, repeated under the race detector, neither panics nor leaves a finish running after `Wait` returns;
  - a `Kill`, before any tick, of a runner whose `job.json` is written leaves a pending record with the run number and workflow;
  - `stopStartTimedOut` skips an instance that is gone;
  - every test that kills a runner waits for the manager's background work before its temporary directory is removed;
  - `Kill` of an unknown ID still returns `ErrUnknownRunner`.
- `internal/daemon`:
  - `KillRunner` calls `Wake` once after a successful stop and not after a failed one;
  - the `status` fingerprint changes when an instance's `Stopping` flips, and no other topic does.
- `internal/activity`: a stopping instance's run carries `Stopping`; its segments are the ones its state gives today.
- `cmd/ghr`: the status table prints `stopping` for a stopping instance.

### Frontend

- `runAria` says `stopping` for a stopping run in each of the three live states.
- The runner list says "Stopping" on the second line of a stopping runner with a job and of one without, and its light does not pulse.
- The runner panel says "Stopping" and its "Stop runner" button is disabled.
- The lanes chart draws a stopping run's open segment as the dashed muted outline with no pulse marker, and the legend lists "Stopping".
- The stop dialog shows the already-stopping title and a disabled Stop button when its runner starts stopping while it is open, and keeps its wording while it closes after its own Stop.
- The capacity bar and the busy count are the same with and without the flag.
- The generated fixtures (`web/fixtures_test.go`) do not change: no fixture value sets the flag and `omitempty` leaves it out. The frontend tests build their own stopping instance.

### Manual, before release

- On a daemon with a busy runner and the Dashboard open: within a second of `ghr kill <id>` the runner's bar turns into the dashed grey outline and its tooltip says "stopping". Within a second of the command returning, whether or not a tick is in flight, the lane reads "finishing, result pending" or the job's result.
- The same with Ctrl-C on `ghr kill` two seconds in: the bar stays the dashed grey outline until the unit exits, and the lane reads "finishing, result pending" or the job's result within a second of that, whether or not a tick is in flight.
- Both checks are run several times, with the daemon's status sampled alongside the browser, so that at least one stop of each kind ends while a tick is in flight.

## Out of scope

- Stops the daemon makes itself: idle timeout, start timeout, repository removal and runner updates. They happen inside a tick and show on the next one.
- Making the unit stop faster. A busy runner took 8.4 s to stop on the LXC; it reads "stopping" for that time.
- The length of a tick. It takes about 6.5 s on the LXC, so the daemon polls about every 16.5 s with `poll_interval: 10s`, and a job that ends or is queued on its own is noticed that much later (register item 37).
- Cancelling a stop.
