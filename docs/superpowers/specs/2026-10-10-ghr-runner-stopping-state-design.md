# ghr: show a runner as stopping

**Date:** 2026-10-10
**Status:** design approved in chat on 2026-10-10 (approach and full design); revised after the spec review and approved the same day.
**Register:** docs/superpowers/registers/2026-10-10-ghr-ui-polish-realtime.md (item 25)
**Code:** `internal/model`, `internal/runner`, `internal/daemon`, `internal/activity`, `cmd/ghr`, `web/src`

## Goal

Stopping a runner must show in the UI in under a second. Today it does not. On the LXC on 2026-10-10 a busy runner stopped with `ghr kill` kept showing as running for 16.2 s: 8.4 s while systemd stopped the unit, during which the daemon reports nothing, then 7.8 s until the manager's next 10-second tick noticed the exit. The change stream itself is fast; the daemon's state is what lags.

## Decisions

- **A flag beside the state, not a new state.** A runner being stopped keeps its state (`starting`, `idle` or `busy`) and gains `stopping`. It still holds its slot until its unit exits, so the scheduler, capacity counts, busy totals and runner-update gating stay as they are. Chosen over a `stopping` state, which every switch on state would have to learn and which loses whether the runner was busy, and over a browser-only indicator, which shows nothing for `ghr kill` or a stop from another tab.
- **Wake the manager loop after the unit stops**, so the exit is handled at once and not on the next poll.
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
  - on success adds the `runner %s stopped by user` event as today and leaves `stopping` set. The instance is removed by the existing exit path.
- A second `Kill` of a runner that is already stopping behaves like the first: it calls `SD.Stop` again and returns its result, and a second success adds a second event. If that second call fails it clears the flag even while the first is still in progress; the runner then reads as its state again until its unit exits, which is accepted.
- `Manager.Status` copies the flag to `InstanceStatus.Stopping` while the state is `starting`, `idle` or `busy`, and never for a `cleaning` instance.
- Nothing else in the manager reads the flag. `refreshUnits` moves the instance to `cleaning` when its unit has exited, whatever the flag says.
- Accepted, because nothing reads the flag:
  - a tick that runs during the stop may pick the runner for the idle timeout or the start timeout, stop its unit again and add its own event. Both stops end the same unit;
  - the scheduler counts a stopping `starting` or `idle` runner as able to take a job until its unit exits, so it starts no replacement before then;
  - the flag stays set until the instance is `cleaning`. If `SD.Stop` succeeds and `SD.Active` then keeps failing, the runner reads "stopping" until `SD.Active` answers. Without the flag it would read as its state and be no nearer to cleanup.

### 1.2 Backend (`internal/daemon`)

- `Backend.KillRunner` calls `b.Wake` (when set) after `Manager.Kill` returns nil, as `update` does after a config change. The next tick runs at once and `refreshUnits` starts the cleanup. A tick already in flight finishes first, and the woken tick follows it.
- No change to the change hub. The `status` topic's fingerprint covers `Status.Instances`, so the flag publishes `status` at the hub's next check, within 250 ms.

## 2. Wire

- `model.InstanceStatus` gains `Stopping bool` with JSON `stopping,omitempty`. `GET /status` carries it.
- `model.ActivityRun` gains `Stopping bool` with JSON `stopping,omitempty`. `internal/activity` sets it on the run it builds for a live instance whose `Stopping` is true. Runs built from finished jobs never carry it. Segments are unchanged.
- A cleaning instance never reports `stopping` (§1.1).
- `DELETE /runners/{id}` still answers when the unit has stopped. A caller that disconnects first no longer ends the stop (§1.1): the stop runs to its end, the flag stays, and the loop is woken.

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
  - a `Kill` whose context is cancelled while `SD.Stop` is blocked keeps the flag, and returns nil once the stop completes;
  - after a successful `Kill` and a tick that sees the unit inactive, the instance is `cleaning` and reports no `Stopping`;
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

- On a daemon with a busy runner and the Dashboard open: within a second of `ghr kill <id>` the runner's bar turns into the dashed grey outline and its tooltip says "stopping". Within a second of the command returning, or of a tick in flight ending if one is, the lane reads "finishing, result pending" or the job's result.
- The same with Ctrl-C on `ghr kill` two seconds in: the bar stays the dashed grey outline until the unit exits, and the lane reads "finishing, result pending" or the job's result within a second of that, or of a tick in flight ending.

## Out of scope

- Stops the daemon makes itself: idle timeout, start timeout, repository removal and runner updates. They happen inside a tick and show on the next one.
- Making the unit stop faster. A busy runner took 8.4 s to stop on the LXC; it reads "stopping" for that time.
- Cancelling a stop.
