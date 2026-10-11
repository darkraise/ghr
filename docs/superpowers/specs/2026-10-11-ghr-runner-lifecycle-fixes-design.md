# ghr: runner lifecycle and scheduler fixes

**Date:** 2026-10-11
**Status:** design presented in chat on 2026-10-11; the owner ruled on the 403 scope, the second-host question and the execution strategy the same day. Awaiting the owner's review of this file.
**Execution strategy:** sequential — nearly every change edits `tick.go`, `lifecycle.go` and `manager.go` in one package and several share new instance fields, so no two parts are free of shared files.
**Register:** docs/superpowers/registers/2026-10-11-ghr-comprehensive-review.md (rows 1 to 18, 175, 176 and 177). This is sub-project 1 of 6; the other rows of that register belong to later specs.
**Code:** `internal/runner`, `internal/sched`, `internal/system`, `internal/daemon/run.go`, `deploy/setup.sh`, `.gitignore`, and one older spec and one older register under `docs/superpowers`

## Goal

The review of build `forge-ui-930381d` found no critical or high defect in the runner manager, but it found ways a healthy runner is stopped, a broken one is kept, and one repository's problem stops every other repository. This spec closes those, bounds the retry loops that have no limit today, and stops root from trusting files a job can write.

## Decisions

- **Targeted fixes, not a state-machine rewrite.** An explicit transition table would fix four rows by construction, but it means rewriting `lifecycle.go` and its 1,100 lines of tests for the product's core loop. Only the small part that pays is taken: one guarded transition helper (§1.5).
- **A 403 belongs to its repository** (owner's ruling, 2026-10-11). Only a 401 stops all spawning. This replaces the "401, or other 403" row of the error table in docs/superpowers/specs/2026-10-03-ghr-runner-manager-design.md.
- **A second ghr host on the same repository is tolerated** (owner's ruling, 2026-10-11): reconcile deletes only registrations it has seen offline twice (§3.4).
- **The scheduler stays pure.** `internal/sched` learns one new instance flag, `Unusable`. Everything that needs the clock, the API or the tick number stays in `internal/runner`.
- **Root-owned records live outside the instance directory.** The instance directory belongs to the runner user, so anything root must trust moves to a new root-only directory (§4.1). Checking a file's owner in place was rejected: the job can still delete the file, and tests would need to fake user ids.

## 1. Runner health and timeouts

### 1.1 A start timeout needs an answer (row 1)

Today `stopStartTimedOut` runs on every tick, while `refreshRunners`, the only thing that moves a runner from starting to idle, is skipped during an API pause and gives up on a failed call. A runner that is online but could not be checked is stopped as "did not come online". Adoption makes this certain after a restart during a pause, because every adopted runner without a job starts as `starting` from its spawn time.

- Each instance carries a flag saying the runners API answered for it in the current tick; the tick clears every flag when it starts. An answer is a successful `GetRunner`, or a 404 that §1.2 confirmed.
- `stopStartTimedOut` stops only instances that were answered in the current tick. `sched.StartTimedOut` is unchanged; the runner package filters its result.
- Adoption is unchanged. With the API working, the first tick moves an online runner to idle before the timeout is considered; without it, nothing is answered and nothing is stopped.
- Accepted consequence: a runner that truly never comes online keeps its slot for as long as the API cannot be read, and is stopped on the first tick after.

### 1.2 A registration that is gone (row 3)

Today a 404 from `GetRunner` on the idle-stop path returns "kept" forever: the runner is never stopped, holds a `global_max` slot, and postpones a queued update by 15 minutes on every attempt. A 404 cannot simply be read as "gone", because GitHub also answers 404 when the token cannot see the repository.

- On a 404 from `GetRunner` (in `refreshRunners` and in `deregisterIdle`), ghr lists the repository's runners, at most once per repository per tick.
  - The list answers and does not contain the runner ID: the registration is confirmed gone.
  - The list fails: nothing is concluded. The error goes through `apiErr` as today, and the runner is left alone.
- An idle runner confirmed gone is marked `deregistered` (the existing field, whose meaning widens to "registration deleted or confirmed missing"). It is then unusable (§1.4) and its unit is stopped on the same tick, with the event `runner <id> registration is gone; stopped`.
- A starting runner confirmed gone only counts as answered for §1.1; it is stopped by the start timeout. A registration created seconds ago is not declared gone on one reading.
- Stopping a runner whose registration is already gone needs no GitHub call, so its unit stop is retried on every tick, also while API calls are paused.
- A runner list that is refused for the whole token (401) or rate-limited ends that tick's runner checks.

### 1.3 An idle runner that went offline (row 3)

Today an idle runner is checked only for `busy`. One whose process hangs stays idle, and in `all` mode a warm runner in that condition stays forever.

- `refreshRunners` records when an idle runner was first seen with a status other than `online` and not busy, and clears that when it is online or busy again.
- Once it has been offline for `start_timeout`, it is unusable (§1.4). It is stopped through `stopIdleRunner`, which still re-checks `busy` and deletes the registration first, with the event `runner <id> offline for <duration>; replaced`.

### 1.4 Unusable runners in the scheduler (rows 3 and 7)

- `sched.Instance` gains `Unusable bool`: the runner can take no job. The runner package sets it for a `deregistered` instance and for one offline past the grace in §1.3.
- An unusable instance is treated as a stale one already is: it does not cover a queued job, it is not counted as starting or idle for the warm count, and when idle it is always in `IdleToStop`.
- It still counts toward the repository cap and `global_max` until its unit is down, because it still uses the host. Since it is now stopped on every tick until the stop succeeds, the slot is no longer held indefinitely.

### 1.5 Guarded state changes (row 8)

`setState` protects only `cleaning`, and `refreshRunners` decides from a snapshot taken before a network call, so a runner that became busy meanwhile can be moved back to idle for one tick.

- `setState` is replaced by a helper that names the states a move is allowed from, checked against the live instance under the lock. Idle is reachable only from starting; busy only from starting or idle; nothing leaves cleaning.
- The same helper records the first time an instance comes online (idle or busy), which §2.2 uses.

## 2. Failure isolation

### 2.1 A 403 stops one repository (row 2)

Today any 403 that is not a rate limit sets the daemon degraded. When one repository refuses a write call, polls for the others still succeed, so the daemon recovers 60 seconds later, tries the write again and degrades again, and no repository gets runners in between.

- The GitHub client is unchanged: a 403 stays `ErrAuth` with its status. The manager decides with one predicate, "the token was rejected", true only for a 401.
- `apiErr`: a 401 sets degraded as today. A 403 becomes that repository's error with the usual poll backoff, worded `token lacks permission for this repository (needs <permissions>): <GitHub's message>`; the permissions part is left out when GitHub sends none.
- A 403 from a spawn is the exception: it is recorded only as a spawn failure (§2.2), with the same wording, and does not start a poll backoff, because the repository's polls are working.
- Call sites stop treating a 403 as global: `gatherDemand` goes on to the next repository, `refreshRunners` goes on to the next instance, and `spawnPlanned` goes on to the next repository. `refreshRunners` runs every tick, so it files a 403 under its repository once per backoff window, not once per tick. A 401 and a rate limit stop each of them as today.
- The error table in the 2026-10-03 spec is amended to match.

### 2.2 Spawn backoff (rows 2 and 4)

Two loops have no limit today. A spawn that fails is retried as soon as the next poll succeeds, because a successful poll clears the repository's failure count. A runner that exits right after starting is replaced about every second tick.

- The manager keeps, per repository, a spawn failure count, the time of the next attempt and the last message. The delay is 10 s doubling to a cap of 10 minutes.
- It is fed by:
  - a `spawn` error that is neither a 401 nor a rate limit (those keep their global handling);
  - a runner spawned by this daemon process that never came online: its unit exited on its own, or the start timeout stopped it. A runner a person stopped does not count, and neither does an adopted one or one that left a job record. The failure is recorded when the tick sees the unit gone, before that tick plans its spawns, so the same tick cannot respawn it.
- `spawnPlanned` leaves a repository out of planning until its next-attempt time.
- It is cleared when a runner of that repository comes online, and wherever `ClearDegraded` is called today (the token was replaced or reloaded). A successful poll does not clear it.
- Status shows the repository's poll error when there is one, otherwise its spawn error.
- Events: each failed attempt still writes its event, now ending `; next attempt in <delay>`. An early exit writes `runner <id> exited before coming online; next spawn in <delay>`. The backoff bounds how often these appear.

### 2.3 Why a queued update is waiting (row 9)

A queued runner update waits for a tick in which every unpaused repository answered its poll, and says nothing while it waits.

- When a queued update is held back by something other than runners working or jobs queued, the manager writes one event naming the reason: `runner update waiting: <repo> did not answer its poll` (the first such repository in config order), `runner update waiting: GitHub API calls are paused`, or `runner update waiting: GitHub rejected the token`.
- When several hold, the first in this order is reported: the token was rejected, API calls are paused, a repository did not answer.
- The event is written when the reason changes, not on every tick, and the remembered reason is dropped when the update starts, is cancelled, or is queued anew.

### 2.4 Status during backoff and pause (rows 14 and 15)

- A repository skipped because it is in backoff, or whose poll failed, keeps its last known queued jobs in the manager's demand, so status no longer shows `Queued 0`. This cannot cause a spawn: such a repository is already left out of planning.
- A paused repository that is not polled has its error, failure count, retry time and queued jobs cleared. This needs no API call and happens on every tick, also during an API pause. Status does not show a paused repository's spawn error (§2.2) either.
- A paused repository that has a busy runner whose job is not yet confirmed is polled like any other, so the runner gets its job name and URL. Its queued jobs are discarded, because a paused repository's queue must not block a runner update, and its answer does not count toward "every unpaused repository answered".

## 3. Stop and cleanup

### 3.1 Stopping a unit systemd no longer has (row 6)

When an idle stop and an update start fall in the same tick, the update stops the same runner again, `systemctl stop` fails because the unit is already gone, and the update is postponed 15 minutes. On systemd 257 the answer is exit status 5 with `Unit <name> not loaded.` (checked on the development host on 2026-10-11).

- `system.Systemd.Stop` returns nil for that answer. A unit that does not exist is stopped; this holds for every caller (`Kill`, the idle stop, the start timeout, adoption and reconcile).
- With this, the test fake's `Stop`, which succeeds on a unit that is not active, matches the real wrapper (row 18).

### 3.2 Automatic prunes off the tick (row 5)

`checkDisk` and the history retention prune run inside the tick, each Docker command bounded only by its 10-minute timeout, and no runner is spawned or reaped meanwhile.

- The tick still decides when they are due and takes the prune reservation. It measures the disk itself (one read, bounded to 15 seconds, so status stays current) and runs the retention prune itself (local file work, no Docker command). Only when the disk is above `disk_high_water` does it hand the reservation to a background goroutine that runs the Docker prunes and releases the reservation when it ends. That goroutine follows the manual prune's pattern: nothing starts after `Close`, it is counted in the manager's wait group, and it runs on the maintenance context so `Close` interrupts it. (Revised 2026-10-11 while planning: moving the measurement off the tick would make every later tick race it for the reservation.)
- While it runs, a manual prune and a runner update are refused or deferred exactly as they are during a manual prune today.

### 3.3 Cleanup count and the give-up event (rows 12 and 13)

- The cleanup count in the history record is the sum over every cleanup attempt of that instance, not the last attempt's. The sum is kept in the pending history record, so it survives a daemon restart between attempts.
- After cleanup gives up, the "releasing its slot" error event is written once per instance. Later retries caused by a failing directory removal report only that failure.

### 3.4 Reconcile and other hosts (row 11)

Reconcile deletes every `ghr-<repo>-<6 hex>` registration it does not track, so a second ghr host on the same repository loses its idle runners.

- Reconcile deletes such a registration only when GitHub reports it `offline` now and it was also untracked and `offline` at the previous reconcile. Any other status, including none, never qualifies. The manager remembers the runner IDs it saw that way last time.
- A live runner of another host is online and never qualifies. A starting one is offline for seconds, and reconciles are 30 ticks apart (5 minutes at the default poll interval), so it qualifies only if it stays offline across two of them, by which time its own host has timed it out.
- Cost: a real leftover is removed one reconcile later than today.

## 4. Files root reads on a job's behalf

### 4.1 Root-only instance records (rows 10 and 177)

The instance directory is owned by the runner user. Two files in it are trusted by root:

- `ghr-projects` lists the compose projects cleanup removes, so a job can name any project on the host.
- `ghr.json` supplies the repository and runner ID after a daemon restart. A job can replace it, and the repository chooses whose `cleanup_name_prefixes` containers are removed and which registration is deleted.

Changes:

- `runner.Paths` gains `Private`, `/var/lib/ghr/private` on the daemon, created mode 0700. It holds `<id>.json` (the instance metadata) and `<id>.projects` (the remembered compose projects), written by root only.
- `spawn` writes `<id>.json` there, atomically, before it starts the unit, and fails the spawn if it cannot. It still writes `ghr.json` into the instance directory for a person reading the directory, but nothing trusts that copy when a private record exists.
- Adoption reads the private record. Only when none exists (an instance spawned by an older ghr, still running across the upgrade) does it read `<instance>/ghr.json`, and it then writes the private record from it, so that copy is trusted once. If the private record cannot be written, adoption fails with an error: nothing is stopped or removed, and the daemon does not start until the directory can be written. An instance with neither a private record nor a readable instance copy is stopped and removed, as one without a readable `ghr.json` is today.
- Cleanup reads and writes the project list only in `Private`. `<instance>/ghr-projects` is no longer read. An instance in the middle of a cleanup retry during the upgrade loses its remembered list once; the container label scan still finds every project that has containers.
- `finish` removes the private records after the instance directory. Adoption and reconcile remove private records whose instance directory is gone and that no live instance uses.

### 4.2 Runner log reader (row 10)

`RunnerLog` runs as root and reads `Runner_*.log` and `Worker_*.log` from a directory the job wrote.

- It refuses a `_diag` or archive directory that is itself a symlink.
- It opens the log directory itself as an `os.Root` and reads every file through it, so no read can reach outside that directory, whatever the job swaps afterwards. It reads a file only if it is a regular file, checked before the open and again on the open file, and opens without blocking. A symlink, a FIFO or a device is skipped as if absent.

### 4.3 Archive ownership (row 16)

GNU tar run by root restores the archive's owner ids. If the runner tarball's ids match a real user on the host, files in the shared runner distribution would be writable by that user.

- Checked on 2026-10-11: every entry of `actions-runner-linux-x64-2.330.0.tar.gz` is owned by 1001:1001. `setup.sh` creates the runner user with plain `useradd`, so on a host where that user gets uid 1001 the shared distribution is writable by jobs.
- `Host.Extract` and the runner extraction in `deploy/setup.sh` add `--no-same-owner`, so extracted files belong to root whatever the archive says.
- `deploy/setup.sh` also runs `chown -R root:root` on the installed version's directory on every run, which repairs a version extracted before this change.

## 5. Docs and housekeeping (rows 17, 175 and 176)

- docs/superpowers/specs/2026-10-03-ghr-runner-manager-design.md: the registration step says the effective labels are sent, system labels included, because GitHub gives a just-in-time runner exactly the labels it is registered with; and the error table row changes as in §2.1.
- `.gitignore` gains `.playwright-mcp/`.
- docs/superpowers/registers/2026-10-08-ghr-web-ui-overhaul.md row 18 says the kit's enter animations ignore reduced motion. Checked on 2026-10-11: kit 6.9.6 `dist/styles.css` has a global `prefers-reduced-motion: reduce` rule that sets animation and transition durations to 0.01ms on every element, so correct that row's note. No code changes for this row.

## 6. Testing

Every fix starts from a failing test. Tests use the package's existing fakes and manual clock; none sleeps.

- **Health and timeouts** (`internal/runner`, `internal/sched`):
  - a starting runner past `start_timeout` is not stopped during an API pause, nor when its `GetRunner` fails, and is stopped on the first tick that answers offline;
  - after adoption with a failing first `GetRunner`, an online runner is idle one tick later and was never stopped;
  - an idle runner answering 404, with a list that omits it, is stopped on that tick and frees its slot; with a failing list it is kept and the repository shows the error;
  - an idle runner offline for less than `start_timeout` is kept and still covers; past it, it is deregistered, stopped and no longer covers or counts as warm;
  - a runner made busy between the snapshot and the answer stays busy;
  - `sched`: an unusable instance does not cover, is not warm, and is in `IdleToStop`.
- **Failure isolation**:
  - a 403 on one repository's spawn leaves the daemon not degraded, and a second repository spawns in the same tick; a 401 still degrades;
  - spawn delays double from 10 s to the cap, survive successful polls, and clear when a runner comes online and on `ClearDegraded`;
  - a runner that exits before coming online delays the next spawn; one a person stopped, and an adopted one, do not;
  - the update-waiting event appears once per reason and again after the reason changes;
  - status keeps the queued count during backoff; a paused repository drops its error, and its unconfirmed busy runner gets a job name and URL without its queue blocking an update.
- **Stop and cleanup**:
  - `system`: exit status 5 with "not loaded" is nil; any other failure is still an error;
  - an idle stop followed by an update start in one tick starts the update;
  - a tick returns while an automatic prune is still running, a second tick does not start another, and `Close` interrupts it and `Wait` returns;
  - the cleanup count sums two attempts; the give-up event appears once across repeated directory-removal failures;
  - reconcile leaves an online untracked registration, leaves an offline one on first sight, and deletes it on the second.
- **Root-only records**:
  - a forged `<instance>/ghr-projects` naming another project is ignored;
  - a replaced `<instance>/ghr.json` does not change the adopted repository or runner ID when a private record exists; without one, the instance copy is adopted and a private record is written;
  - private records are removed by `finish`, and orphans by adoption;
  - `RunnerLog` skips a symlinked log file, a FIFO and a symlinked `_diag`.
- **Suites:** `go vet ./...` and `go test ./...` (Go is at `/usr/local/go/bin`), with `-race` on `internal/runner` because §3.2 adds a goroutine; the existing `deploy/setup.sh` tests for the tar flag.
- **Live check:** deploying to the runner LXC needs the owner's yes each time. The plan ends by asking for it; with a yes, the check is one normal job run to completion, one idle stop, and a queued runner update that starts without a postponement event.

## 7. Verify before fixing

- **Row 2:** the mechanism is reproduced by test, but which GitHub answer a repository-scoped refusal produces has not been seen live. The fix does not depend on it: it applies to every 403 that is not a rate limit.
- **Row 16:** verified on 2026-10-11 (see §4.3). Still unknown: the runner user's uid on the runner LXC, which decides whether that host was exposed; `id -u ghrunner` there settles it.
- **Row 6:** confirm the systemd version on the runner LXC gives the same exit status and message as §3.1 before relying on the text match; match on the exit status and the words "not loaded".

## Out of scope

- The GitHub client's own token verdict, which also flips on any 403 (rows 27 and 44): sub-project 2 applies the §2.1 rule there.
- An upper bound on `global_max` and a repository's `max` (row 21): sub-project 2.
- CLI and web timeouts around a slow unit stop (row 48), download integrity for toolchains (row 60) and the installer's other rows: sub-project 3.
- Any web UI change. Repository errors and events already reach the UI through status and the event list.
- The just-in-time credential visible in the process command line, recorded as deferred in docs/superpowers/notes/2026-10-04-ghr-final-review-deferred.md.

## Row map

| Row | Section |
|---|---|
| 1 | §1.1 |
| 2 | §2.1, §2.2 |
| 3 | §1.2, §1.3, §1.4 |
| 4 | §2.2 |
| 5 | §3.2 |
| 6 | §3.1 |
| 7 | §1.4 |
| 8 | §1.5 |
| 9 | §2.3 |
| 10 | §4.1, §4.2 |
| 11 | §3.4 |
| 12, 13 | §3.3 |
| 14, 15 | §2.4 |
| 16 | §4.3 |
| 17 | §5 |
| 18 | §3.1, §6 |
| 175, 176 | §5 |
| 177 | §4.1 |
