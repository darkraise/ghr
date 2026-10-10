# ghr: show a runner as stopping

**Date:** 2026-10-10
**Status:** design approved in chat on 2026-10-10 (approach and full design).
**Register:** docs/superpowers/registers/2026-10-10-ghr-ui-polish-realtime.md (item 25)
**Code:** `internal/model`, `internal/runner`, `internal/daemon`, `internal/activity`, `cmd/ghr`, `web/src`

## Goal

Stopping a runner must show in the UI in under a second. Today it does not. On the LXC on 2026-10-10 a busy runner stopped with `ghr kill` kept showing as running for 16.2 s: 8.4 s while systemd stopped the unit, during which the daemon reports nothing, then 7.8 s until the manager's next 10-second tick noticed the exit. The change stream itself is fast; the daemon's state is what lags.

## Decisions

- **A flag beside the state, not a new state.** A runner being stopped keeps its state (`starting`, `idle` or `busy`) and gains `stopping`. It still holds its slot until its unit exits, so the scheduler, capacity counts, busy totals and runner-update gating stay as they are. Chosen over a `stopping` state, which every switch on state would have to learn and which loses whether the runner was busy, and over a browser-only indicator, which shows nothing for `ghr kill` or a stop from another tab.
- **Wake the manager loop after the unit stops**, so the exit is handled at once and not on the next poll.
- **Only stops a person asks for.** The flag covers `Manager.Kill`, which the UI's Stop button and `ghr kill` both reach. Stops the daemon makes itself are out of scope (see below).

## 1. Daemon

### 1.1 Runner manager (`internal/runner`)

- `instance` gains `stopping bool`.
- `Manager.Kill`:
  - looks the instance up as today, and returns `ErrUnknownRunner` for an unknown ID;
  - sets `stopping` under `m.mu` before it calls `SD.Stop`;
  - if `SD.Stop` fails, clears `stopping` under `m.mu` when the instance still exists, and returns the error;
  - on success adds the `runner %s stopped by user` event as today and leaves `stopping` set. The instance is removed by the existing exit path.
- A second `Kill` of a runner that is already stopping behaves like the first: it calls `SD.Stop` again and returns its result. If that second call fails it clears the flag even while the first is still in progress; the runner then reads as its state again until its unit exits, which is accepted.
- `Manager.Status` copies the flag to `InstanceStatus.Stopping` while the state is `starting`, `idle` or `busy`, and never for a `cleaning` instance.
- Nothing else in the manager reads the flag. `refreshUnits` moves the instance to `cleaning` when its unit has exited, whatever the flag says.

### 1.2 Backend (`internal/daemon`)

- `Backend.KillRunner` calls `b.Wake` (when set) after `Manager.Kill` returns nil, as `update` does after a config change. The next tick runs at once and `refreshUnits` starts the cleanup.
- No change to the change hub. The `status` topic's fingerprint covers `Status.Instances`, so the flag publishes `status` at the hub's next check, within 250 ms.

## 2. Wire

- `model.InstanceStatus` gains `Stopping bool` with JSON `stopping,omitempty`. `GET /status` carries it.
- `model.ActivityRun` gains `Stopping bool` with JSON `stopping,omitempty`. `internal/activity` sets it on the run it builds for a live instance whose `Stopping` is true. Runs built from finished jobs never carry it. Segments are unchanged.
- A cleaning instance never reports `stopping` (§1.1).
- `DELETE /runners/{id}` is unchanged: it still answers when the unit has stopped.

## 3. Clients

### 3.1 Web UI (`web/src`)

- `api/types.ts`: `Instance.stopping?: boolean`, `ActivityRun.stopping?: boolean`.
- A stopping runner reads "stopping" wherever its state is shown:
  - `components/runner-list.tsx`: the state sentence and its screen-reader text say "Stopping"; the state light uses the muted colour;
  - `components/runner-panel.tsx`: the state text is `stopping`;
  - `lib/activity-view.ts` `runAria`: the state word is `stopping` in place of `starting`, `warm, waiting for a job` or `running`.
- `components/stop-runner-dialog.tsx`: for a stopping runner the title is `Runner <id> is already stopping`, the description is `ghr is stopping it and will clean it up.`, and the Stop button is disabled.
- Unchanged: `lib/capacity.ts`, `lib/summary.ts`, `lib/status.ts`, `components/refused-hint.tsx`. They count by state, and a stopping runner still holds its slot.

### 3.2 Command line (`cmd/ghr`)

- `ghr status` prints `stopping` in the STATE column for an instance whose `Stopping` is true. The `global` count is unchanged.

## 4. Testing

### Go

- `internal/runner`:
  - while `SD.Stop` is blocked, `Status` reports the instance with `Stopping` true and its state unchanged;
  - a failed `SD.Stop` clears the flag and returns the error;
  - after a successful `Kill` and a tick that sees the unit inactive, the instance is `cleaning` and reports no `Stopping`;
  - `Kill` of an unknown ID still returns `ErrUnknownRunner`.
- `internal/daemon`:
  - `KillRunner` calls `Wake` once after a successful stop and not after a failed one;
  - the `status` fingerprint changes when an instance's `Stopping` flips, and no other topic does.
- `internal/activity`: a stopping instance's run carries `Stopping`; its segments are the ones its state gives today.
- `cmd/ghr`: the status table prints `stopping` for a stopping instance.

### Frontend

- `runAria` says `stopping` for a stopping run in each of the three live states.
- The runner list and the runner panel say "Stopping" for a stopping instance.
- The stop dialog shows the already-stopping title and a disabled Stop button.
- The capacity bar and the busy count are the same with and without the flag.

### Manual, before release

- On a daemon with a busy runner and the Dashboard open: `ghr kill <id>` shows "stopping" in the lane in under a second, and within a second of the command returning the lane reads "finishing, result pending" or the job's result.

## Out of scope

- Stops the daemon makes itself: idle timeout, start timeout, repository removal and runner updates. They happen inside a tick and show on the next one.
- Making the unit stop faster. A busy runner took 8.4 s to stop on the LXC; it reads "stopping" for that time.
- Cancelling a stop.
