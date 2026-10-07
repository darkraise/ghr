# ghr runner update warning, queued update and repository picker

**Date:** 2026-10-05
**Status:** approved in chat section by section (2026-10-05); amended during planning with the owner's rulings (2026-10-05): the free condition counts only jobs ghr can serve, and `setup.sh` stops ghr while it installs a runner
**Register:** docs/superpowers/registers/2026-10-05-ghr-runner-update.md (continues docs/superpowers/registers/2026-10-03-ghr-runners.md row 9, finding C4)
**Code:** github.com/darkraise/ghr (`internal/github`, `internal/runner`, `internal/system`, `internal/daemon`, `internal/api`, `internal/model`, `internal/tui`, `internal/tui/ui`, `cmd/ghr`)
**Builds on:** docs/superpowers/specs/2026-10-05-ghr-repositories-management-design.md

## Goal

Warn when the GitHub Actions runner on the LXC falls behind GitHub's latest release, and let the owner queue an update that ghr performs on its own the next time it is free. Replace the free-text repository name in Add repository with a picker that lists the repositories the token can access.

## Decisions

- **The daemon performs the update itself, in Go.** It follows the steps `setup.sh` uses (`fetch_runner`, `install_runner`, `gc_dist`). Running a bundled shell script was rejected because it duplicates `setup.sh` into ghr and cannot be tested with ghr's fakes. Re-running `setup.sh` was rejected because `setup.sh` stops and restarts the ghr service, which would kill the daemon mid-update.
- **"Free" ignores idle warm runners.** ghr is free when no runner is busy or starting and GitHub reports no queued job that ghr could serve: none matching ghr's labels in any unpaused configured repo. Paused repos get no runners, and a job no ghr runner matches never runs on one, so neither can hold the update back. Idle warm runners are stopped for the update and come back on the new version. Counting them as busy was rejected because in `all` mode with `warm > 0` the update would never run.
- **The update installs the latest release at the moment it runs**, not the version shown when it was queued.
- **A failure clears the queue.** The owner re-queues on purpose; ghr never retries an update on its own.
- **Nothing updates without a queued request.** The 30-day deadline only drives the warning.
- **The picker lists only repositories owned by the configured owner.** `/user/repos` also returns repositories the token's user collaborates on elsewhere (on 2026-10-05 the LXC token listed 71 repositories across two owners, 32 of them public), and ghr can only manage the owner's.

## 1. Runner update

### Version facts

- **Installed version:** the base name of the directory `/opt/ghr/dist/current` resolves to (`Paths.Dist`), for example `2.337.0`. It is read fresh on every check, so a `setup.sh` run that switches `current` is seen on the next check.
- **Releases:** `GET /repos/actions/runner/releases?per_page=30` through the existing GitHub client (one API call). Drafts and prereleases are ignored. Tags look like `v2.338.0`; the leading `v` is dropped. Versions compare numerically by their dot-separated parts.
- **Latest:** the highest version among those releases.
- **Deadline:** 30 days after the `published_at` of the *oldest* release newer than the installed version. GitHub's rule is that a runner must be updated within 30 days of a new version being made available, after which it no longer queues jobs to that runner (docs.github.com, "Autoscaling with self-hosted runners", read 2026-10-05). The deadline is absent when the installed version is current or newer.
- **Checksum:** the release body carries `<!-- BEGIN SHA linux-x64 -->` + 64 hex digits + `<!-- END SHA linux-x64 -->`, the same marker `setup.sh` reads. A release without it cannot be installed.

### Check

The daemon checks at start and then every 24 hours, as part of the run loop and only while the API is allowed (not rate-limited, not degraded). A failed check posts no runner event and is retried on the next tick after 1 hour; a rate limit or rejected token is handled as for any other GitHub call (its own event, API use paused). A check that overlaps an install which has just switched `current` is discarded, so it never overwrites the newer facts. The result is held in memory as the runner-update state below.

### Warning

- **Activity:** when a check first finds a newer latest version, one `warn` event: "runner 2.338.0 is available (installed 2.337.0); update by 2026-11-04". Once per latest version, remembered in the persisted state file so a restart does not repeat it.
- **Settings, Maintenance section:** a Runner line, `runner 2.337.0 → 2.338.0  [UPDATE AVAILABLE]  update by 2026-11-04 (30 days)`. When current: `runner 2.337.0  [UP TO DATE]  checked 2h ago`. Before the first check: `runner 2.337.0  checking…`. A failed check adds a dim "last check failed: <reason>".
- **Top bar:** a `runner ↑` badge after the disk gauge while an update is available: amber, red when the deadline is 7 days away or past. It is absent when current or unknown.
- **`ghr status`:** a `runner` line with the same facts.

### Queue

- **TUI:** the Runner line has `[ Queue update ]` while an update is available and none is queued, and `( Cancel queued update )` while one is queued. A queued update shows `[QUEUED]` and "runs when no job is running or queued"; a running one shows `[UPDATING]`. Both buttons are disabled while the daemon is unreachable.
- **CLI:** `ghr runner-update` queues; `ghr runner-update --cancel` cancels.
- **`POST /runner-update`** runs a fresh check first, then queues. 202 when queued (or already queued); 409 "runner 2.337.0 is already up to date" when nothing newer exists; 409 while an update is running; 503 while degraded or shutting down; GitHub errors map as elsewhere (429 with `retry_at`).
- **`DELETE /runner-update`** cancels: 204, also when nothing was queued; 409 while the update is running.
- **Persistence:** `/var/lib/ghr/runner-update.json` holds `queued_at` and `warned_version`, written atomically (temp file and rename). A queued update survives a daemon restart.
- **Events:** "runner update queued", "runner update cancelled".

### Run

On each tick, after demand is gathered, a queued update starts when all of these hold:

1. no instance is Starting or Busy;
2. this tick's demand gathering got an answer from every unpaused configured repo, with the config unchanged since, and found no queued job matching ghr's labels;
3. the API is allowed and ghr is not degraded;
4. the maintenance reservation that manual and automatic pruning share is free; the update takes it.

Then the manager sets `updating`, which makes `spawnPlanned` spawn nothing, stops every Idle instance through the existing stop path (an instance that turns out busy, or that cannot be checked or stopped, means ghr is not free: the update releases the reservation and waits for a later tick, still queued), posts "runner update started", and runs the update in the background under the manager-owned context, registered with the wait group as a prune is:

1. Fresh release check. If the installed version is already the latest, post "runner already up to date (2.338.0)", clear the queue and finish.
2. Remove any `dist/*.tmp` left by an interrupted run. Download `https://github.com/actions/runner/releases/download/v<ver>/actions-runner-linux-x64-<ver>.tar.gz` (no `Authorization` header; it is a public asset) into `dist/<ver>.tmp/runner.tar.gz`, and verify its SHA-256 against the release-body checksum.
3. Extract into `dist/<ver>.tmp`, delete the tarball, run `bin/installdependencies.sh` there.
4. Rename `dist/<ver>.tmp` to `dist/<ver>` (when `dist/<ver>/run.sh` already exists and is executable, steps 2 to 4 are skipped), create `current.tmp` pointing at it and rename it over `current`.
5. Delete every other directory in `dist` except `current`. This is safe while runners are alive because each instance copies the runner files at spawn.
6. Clear the queue, post `ok` "runner updated to 2.338.0", refresh the version facts.

The whole run, from stopping idle instances on, is bounded by a 10-minute timeout. Afterwards `updating` and the reservation are released; spawning resumes on the next tick and warm runners return on the new version. A job queued during the update waits for that tick.

**Failure:** any failed step before the switch in step 4 leaves `current` untouched, removes `dist/<ver>.tmp`, clears the queue, and posts `error` "runner update failed: <step>: <reason>". **Shutdown** cancels the context; the update stops, posts "runner update interrupted by shutdown", and keeps the queue so it runs again after the restart. A shutdown or timeout that comes after the dependency script but before the switch also leaves `current` untouched. A `.tmp` left behind is removed by the next run. Once `current` has switched the update has succeeded: a version directory that cannot be removed in step 5 posts a `warn` and is removed by the next update. If the state file cannot be rewritten to clear the queue, it is removed instead, so the update never repeats on its own.

**Host operations.** The downloader (an HTTP client with the 10-minute bound), extraction and the dependency script go behind the manager's existing seams so tests fake them: `Host` gains `Extract(ctx, tarball, dir)` (real: `tar -xzf`) and `RunScript(ctx, dir, path)` (real: executes the script in a process group of its own, which a cancel or timeout kills whole, so apt started by the script does not outlive it; `Host` already has a field named `Run`), and a `Fetch(ctx, url, dst)` dependency does the download.

**`setup.sh` compatibility.** The `dist/<version>`, `dist/<version>.tmp` and `current` layout is unchanged, so `setup.sh` and the daemon keep working on the same tree. `setup.sh` stops ghr before it installs the runner and starts it again at the end, or when a later step fails, so the two never install at the same time; runner units are separate and keep running. A stop that interrupts the daemon's update leaves a `.tmp` that either side removes. While ghr is stopped, `setup.sh` deletes `runner-update.json`, so a queued update does not survive it: once the daemon is back up it could otherwise start installing into `dist` while `setup.sh`'s final cleanup is still removing from it. `setup.sh` installs GitHub's latest runner itself, so the dropped queue is normally moot; if a newer runner appears later, the owner queues it again, and the cost of the deletion is one repeated "runner X is available" warning.

### Status

`/status` gains `runner_update`: `installed`, `latest`, `latest_published`, `deadline`, `checked_at`, `check_error`, `queued`, `queued_at`, `running`, `last_outcome` (`ok`, `failed`, `current`), `last_error`, `last_finished`, each absent when unknown.

## 2. Repository picker

### Daemon

**`GET /repos/available`** lists the repositories the token can access that are owned by the configured owner: `name`, `private`, and `configured` (case-insensitive match against config). It reads `GET /user/repos?per_page=100`, following the `Link` next page, keeps those whose `owner.login` equals the configured owner ignoring case, and sorts by name ignoring case. 503 while degraded; GitHub errors map as elsewhere. There is no cache: the dialog asks once each time it opens.

### Widget

A new `ui.Picker`: a filter field and a list window of 8 rows below it.

- Typing edits the filter; the list shows options whose name contains the filter, ignoring case.
- Up and Down move the highlight, scrolling the window; Enter picks the highlighted option. The mouse wheel scrolls; a click picks.
- An option can be disabled; it is shown dim with a note and cannot be picked.
- Rows wider than the dialog are cut with an ellipsis.
- While focused it takes printable keys, Backspace, Up, Down and Enter; Tab and Esc go to the dialog.

### Dialog

The name text field is replaced by the picker. On open the dialog shows "loading repositories…" and requests `GET /repos/available`. Each row reads `darkcloud  [PRIVATE]` or `booklore  [PUBLIC]`; configured repositories are disabled and read "added". The picked repository is shown as `Repository  darkcloud`. Add stays disabled until a repository is picked. A failed list shows the error inside the dialog with `( Retry )`. The reply carries the dialog that asked, as `addedMsg` does, so a late reply never lands in a newer dialog.

Picking a public repository still needs the Allow public toggle; the daemon keeps enforcing it. `ghr repo add <name>` in the CLI keeps free text.

## 3. Daemon API summary

| Endpoint | Purpose |
|---|---|
| `GET /status` | gains `runner_update` |
| `POST /runner-update` | check, then queue an update |
| `DELETE /runner-update` | cancel a queued update |
| `GET /repos/available` | repositories the picker offers |

## 4. Error handling

- A failed release check never warns or clears anything; it shows "last check failed" and retries after an hour.
- When `current` cannot be resolved, the installed version is unknown: the Runner line says "runner version unknown (no dist/current)", no warning is posted, and queueing is refused with 409 "run setup.sh first".
- A release without a linux-x64 checksum fails the update at step 2 with "no linux-x64 checksum in the v<ver> release notes".
- The update never leaves `current` pointing at an incomplete directory: `current` moves only after the rename of a directory whose dependency script succeeded.
- Picker errors stay inside the dialog; Add is never enabled by an error.

## 5. Testing

- `internal/github`: release listing (drafts and prereleases skipped), checksum parsing, version comparison, `/user/repos` pagination.
- Version facts: latest and deadline with zero, one and several newer releases, installed newer than latest.
- Free condition: each of the four conditions blocks on its own; idle warm runners do not block and are stopped; an idle runner that takes a job or cannot be stopped defers the update; a config change during the poll defers it.
- Run: success switches `current` and removes old versions; spawning is paused while updating; checksum mismatch, extraction failure and a failed dependency script each leave `current` untouched, remove the `.tmp` and clear the queue; shutdown keeps the queue; an existing complete version directory skips the download; already-current finishes without downloading.
- Persistence: queued flag and warned version survive a restart; one warning per version.
- API: queue, cancel, already up to date, running, degraded, over HTTP and at the backend.
- TUI: the Runner line in every state, the buttons and their disabled states, the top-bar badge colours, goldens for Settings with an update available.
- Picker widget: filter, scroll window, disabled options, keys, mouse.
- Dialog: loading, error and Retry, Add disabled until a pick, a late reply dropped, the public toggle still required, a golden.
- CLI: `ghr runner-update` and `--cancel`.
- LXC acceptance: the Runner line shows 2.337.0 as current; `ghr runner-update` answers "already up to date"; the Add repository dialog lists the owner's repositories with configured ones marked added.

## 6. Delivery

One ghr branch, merged and released through CI (the next patch version), deployed to the LXC with `GHR_VERSION` pinned, after the owner approves the release. One homelab change ships with it: `setup.sh` stops `ghr.service` before installing the runner (tested by `tests/setup_test.sh`), copied to the LXC's `/root/github-runner` before the deploy.

## Out of scope

- Updating without a queued request, including at the deadline.
- Updating ghr itself or Docker.
- Choosing a runner version, or downgrading.
- Listing organisation repositories the token can reach only through `/orgs/{org}/repos`.
