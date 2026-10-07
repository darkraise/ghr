# ghr Repositories page, management features and graphics

**Date:** 2026-10-05
**Status:** approved in chat section by section (2026-10-05); revised after the Fable and Codex reviews (2026-10-05, see Appendix)
**Register:** docs/superpowers/registers/2026-10-05-ghr-repositories-page.md
**Code:** github.com/darkraise/ghr (`internal/tui`, `internal/tui/ui`, `internal/api`, `internal/daemon`, `internal/runner`, `internal/github`, `internal/model`, new `internal/metrics`)
**Builds on:** docs/superpowers/specs/2026-10-04-ghr-tui-revamp-design.md

## Goal

Give repositories a page of their own, where every per-repo setting, labels included, can be edited and managed. Make every management action the daemon can take reachable from the TUI, adding the few it lacks. Make the TUI more graphical with status badges, activity charts, host resource gauges and history visuals.

## Decisions

- **Repo editing lives on the new Repositories page.** Settings keeps only global settings. Keeping editors on both pages was rejected because it creates two editors for the same values, and a read-only Repositories page was rejected because it does not fix the Dashboard dead end.
- **Management scope:** GitHub token status and replacement, GitHub runner registrations, a workflow label check, and maintenance actions (prune now, reload config).
- **Label check source:** the jobs of recent workflow runs, read through the Actions API the PAT already allows. Reading workflow files was rejected because it needs Contents: read on the PAT and cannot resolve matrix or expression `runs-on` values statically.
- **Graphics scope:** status badges, Dashboard sparklines, host CPU and memory gauges, per-repo activity, and History duration bars.
- **Badges are plain text blocks with a background colour.** Powerline pill glyphs were rejected because they need a patched Nerd Font and render as boxes otherwise.
- **The daemon keeps the metrics series.** Sampling in the TUI was rejected because the charts would start empty every time `ghr` opens and could not show CPU or memory.
- **Delivery:** one spec, one plan, one release. Splitting into three plans and releases was offered and declined.

## 1. Pages and navigation

The sidebar lists five pages:

| Key | Page | Contents |
|---|---|---|
| 1 | Dashboard | tiles with sparklines and gauges, the Repositories and Runners cards, Activity |
| 2 | Repositories (new) | repo list, the selected repo's editor, management and activity |
| 3 | Runners | badges added |
| 4 | History | duration bars and result badges |
| 5 | Settings | global settings, GitHub token, Maintenance |

Runners, History and Settings each move up one number key: `press` accepts `1`–`5`, Help says "1-5 switch page" and its stepper note says "1-5 type into it". "▌ 2 Repositories" is exactly the sidebar's 16 columns. The tab row's short names become Dash, Repo, Run, Hist, Set.

**Dashboard changes:**
- The selected repo row's buttons are `( Edit )  ( Pause )  [ Remove ]`. Edit, or `e` while the Repositories card has focus, opens the Repositories page with that repo selected.
- The header's `[ + Add ]` button becomes `[ + Add repository ]`. It opens the existing Add repository dialog.

**Settings** loses its repo cards and its `[ + Add repository ]` row. It keeps General (mode, global max, owner), Timing, Disk and retention, and Runner defaults (global labels, memory max, CPU quota), and gains the GitHub token and Maintenance sections (§3).

**Leave guard.** Settings and Repositories each hold their own unsaved edits, Save, Discard and rejected-save alert. Leaving either page with unsaved edits opens the Save / Discard / Stay dialog for that page; its text names the page ("on the Repositories page"). After a Save chosen in that dialog succeeds, navigation proceeds only if the page has no unsaved edits left. If an edit was made while the save was in flight, the page stays and the dialog reopens. This also fixes that path on today's Settings page, which navigates or quits over such edits (`settings.go` `refetched`).

**Help** gains a Repositories group and becomes seven groups. No column may exceed ten rows, so the Help text stays at 14 lines (ten rows, a blank, three notes) and the dialog fits 22 rows:

| Column | Groups (rows including title) |
|---|---|
| 1 | Global (5: `1-5`, `tab`, `↑↓ j k`, `? / q`) over Detail (4: `← / →`, `x / esc`, `pgup/dn`) |
| 2 | Dashboard (6: `h / →`, `p / P`, `+ - [ ]`, `m`, `a / d / e` "add/remove/edit") over History (3) |
| 3 | Repositories (4: `a / d`, `p`, `ctrl+s`) over Settings (5) |
| 4 | Runner rows (4) |

Four columns need an inside width of 98 (4 × 23 + 3 × 2), a screen of 104 columns or more. Narrower, the groups stack in one column and the dialog scrolls with its existing `↑↓ scroll` hint.

## 2. Repositories page

**Wide layout (100 columns or more).** The content area splits into a list card on the left, 30 columns wide, a one-column gap, and the selected repo's panel. At 100 columns the content is 83 wide, so the panel is 52 wide (48 inside). The page header holds the title and `[ + Add repository ]`. The unsaved bar sticks to the bottom of the page, as on Settings.

```
Repositories                                          [ + Add repository ]
╭─ Repos ─────────────────────╮ ╭─ darkcloud ────────────────────────────╮
│ › darkcloud    ACTIVE  ●    │ │  ACTIVE   0/1 running · 0 queued       │
│   darkmem      ACTIVE       │ │ ✔ #411 lint · 2m ago                   │
│   darkagents   PAUSED       │ │                ( Pause )   [ Remove ]  │
│   ghr-e2e      ACTIVE       │ ├─ Capacity ─────────────────────────────┤
│                             │ │ Max   › [ − ] 1 [ + ]                  │
│                             │ │ Warm    [ − ] 1 [ + ] (default)        │
│                             │ ├─ Labels ───────────────────────────────┤
│                             │ │ Repo labels  darkcloud-linux ✕  + add  │
│                             │ │ Runners get  self-hosted linux x64     │
│                             │ │              homelab docker (global)   │
│                             │ │              darkcloud-linux           │
╰─────────────────────────────╯ ╰────────────────────────────────────────╯
● 1 unsaved change (darkcloud)                ( Discard )  [ Save changes ]
```

**The list card** is 26 columns inside: the cursor (2), the name (11, cut with `…`), a space, the badge (up to 10, ` REMOVING `), a space, and `●` when the repo has unsaved edits. It scrolls to keep the selection visible. A click selects a row. While the list has focus, `↑`/`↓` and `j`/`k` move the selection, and `tab` moves focus into the panel. The mouse wheel over the list moves the selection; over the panel it scrolls the panel. Selection is kept by repo name across refreshes; when a refresh removes the selected repo, selection moves to the repo now at its position, or the last one.

**Narrow layout (under 100 columns).** The list card stays, full width and a few rows tall (scrolling to keep the selection visible), at the top of the page, and the panel's cards stack below it full width. It keeps the wide layout's selection keys and clicks. (A `Repo [ darkcloud ▾ ]` dropdown was considered; the owner chose the list card on 2026-10-05.)

**The panel**, top to bottom:
1. **Summary:** status badge, live/max runners, queued jobs, the last job (icon, run number, job name, age), and Pause/Resume and Remove buttons.
2. **Capacity:** Max and Warm steppers on separate rows, with the warm ≤ max check. Max's default text follows the **saved** mode (the Repositories page has no mode field), and refreshes when a new config arrives.
3. **Labels:** the Repo labels tag list, and a **Runners get** block: the label set newly started runners for this repo will carry once the edits are saved. It is computed with `config.CustomLabels` over the saved global labels and the draft repo labels, so it is normalised and de-duplicated exactly as the daemon does it, and it wraps over as many lines as it needs. Global labels are marked `(global)`; a label in both lists appears once, marked global. The block notes "runners already running keep their labels".
4. **Cleanup:** the Cleanup prefixes tag list.
5. **Activity** (§4).
6. **Workflow labels** (§3).
7. **GitHub registrations** (§3).

Rows in the panel use the narrow row form (description under the control) whenever the panel is under 70 columns wide, so controls are never cut. The panel scrolls with the wheel and `pgup`/`pgdn`, and focus moves scroll the focused control into view, using the same line-range method as Settings.

**Editing.** The page has one form holding the editable fields (max, warm, labels, cleanup prefixes) of every repo. Field keys are `repos/<name>/<field>`; the Settings code that parses `settings/repo/` keys (`buildPatch`, `repoAction`, `fieldName`, `settingsSections`) moves here, so there is one parser. Switching repos keeps edits. The unsaved bar counts edits across all repos and names the repos with edits, cutting the list to fit; its text shortens as on Settings so the buttons are never cut. Save sends every edit in one config patch. Rejected saves, the in-app checks and the "daemon did not apply" warning work as on Settings today.

**Actions.** Pause/Resume act at once. Remove asks for confirmation, as now. A repo being removed shows `REMOVING` and the note "removing… running jobs finish first", and its controls are disabled.

**Keys.** `a` opens Add repository; `p` pauses or resumes the selected repo; `d` asks to remove it; `ctrl+s` saves. They act on the selected repo whatever control in the page has focus, except while a text or tag input is being edited, which takes the keys (the existing precedence rule). The daemon-changing keys show the unreachable toast while the daemon is unreachable.

**Empty state.** With no repos, the list card reads "No repositories yet" and the panel shows a centred `[ + Add repository ]`.

### Two config pages, one config stream

- **One fetch order.** The request sequence moves from `settingsPage` to the Model. Every config request takes the next number; a response older than the newest shown is dropped, as today.
- **Every accepted config merges into both forms** and updates `m.cfg`.
- **Saves are tagged.** `savedMsg` and `refetchedMsg` carry the page that saved (`pageSettings` or `pageRepos`) and the values sent. A refetch resets only that page's submitted keys, and only that page's saving flag, alert and leave state change.
- **The unsaved dialog, its auto-continue on a refresh that empties the dirty set, and `leaving`/`leaveTo` belong to the page being left.**
- A `configPage` type holds what each page owns: its form, alert, saving flag, scroll and leave state. Both pages embed it.

## 3. Management features

### GitHub token (Settings)

**Recording.** The GitHub client records, from every response, `X-RateLimit-Remaining`, `X-RateLimit-Limit`, `X-RateLimit-Reset` and `GitHub-Authentication-Token-Expiration`, plus the time and outcome (authenticated or rejected) of the last call. These live in one snapshot under the client's mutex, tagged with a credential generation. `SetToken` and Reload bump the generation and clear the snapshot; a response from an older generation does not update it. Each value is absent until observed. The expiry header looks like `2026-12-31 23:59:59 UTC`, is absent for tokens without expiry, and is parsed leniently; an unparseable value counts as absent.

**`GET /token`** returns the snapshot and never the token: `state` (`ok`, `rejected` or `unverified`), `checked_at`, `reason` (the degraded reason, when rejected), `rate_remaining`, `rate_limit`, `rate_reset` and `expires_at`, each absent when unknown.
- `ok`: the most recent GitHub call of this generation succeeded.
- `rejected`: ghr is degraded by an authentication or permission error.
- `unverified`: no call has completed since the daemon started or the token changed.

**The section** shows a badge, `OK`, `EXPIRES SOON` (within 14 days), `REJECTED` or `UNVERIFIED`, then "expires 2026-12-31 (87 days)" or "expiry unknown", the rate limit with its reset time, and "checked 12s ago". A note says: "Replacing checks read access to the first repository only; registration permissions are checked when a runner is next started." `[ Replace token ]` opens a dialog with a masked field and Cancel / Replace. Replace sends the existing `PUT /token`. A rejection stays inside the dialog; success closes it with a toast. The dialog uses the `addedMsg` pattern: the reply carries the dialog that sent it, so a late reply never lands in a newer dialog. The field is cleared on Cancel, on sending and on completion; no copy stays in the model (the request itself holds the value until it returns). Replacing is allowed while ghr is degraded, since that is how a rejected token is fixed.

**Masking.** `ui.TextField` gains `Mask bool`. A masked field renders `•` for every character in every state (editing, focused, blurred, disabled); the raw value never reaches `View`. Its check error never includes the value.

### GitHub runner registrations (Repositories panel)

**`GET /repos/{name}/registrations`** lists the runners GitHub has registered for the repo: id, name, `online`/`offline`, busy, labels (the `github.Runner` type gains `Labels`, decoded from GitHub's label objects by name), and `ghr` (the name is in ghr's namespace, below). An empty list is `[]`.

**`DELETE /repos/{name}/registrations/{id}`**:
1. The id must parse as a positive int64 (400 otherwise); the repo must be configured (404 otherwise).
2. A name matching `ghr-<repo>-<6 hex>` is ghr's namespace and is refused (409). ghr creates the registration before it records the instance, so a name check is the only safe test; ghr's own reconciliation already deletes stale registrations in that namespace.
3. The daemon re-reads the runner with `GetRunner` immediately before deleting. A runner that is gone counts as deleted (204). An online or busy runner is refused (409). A failed lookup refuses (502) rather than deleting blind.
4. GitHub's 422 (busy) maps to 409.

Online/busy protection for registrations outside ghr's namespace is a last-observed check: GitHub's delete endpoint has no "only if offline" condition, so a runner coming online in the moment between the check and the delete can still be removed.

**The card** lists the registrations: a status badge (`ONLINE`, `BUSY`, `OFFLINE`), a grey `GHR` tag for ghr's namespace, the name and labels. `[ Delete ]` appears on offline rows outside ghr's namespace and asks for confirmation naming the runner; the confirmation captures the repo and id when it opens. After a delete, an older listing response is dropped and the list refetches. The card loads when the repo is selected and has a `( Refresh )` button. With none, it reads "No runners registered. ghr starts single-use runners on demand (and keeps warm ones in all mode)."

### Workflow label check (Repositories panel)

**What the daemon gathers.** For one repo: the 20 most recent workflow runs (a new `ListRecentRuns(repo, n)` that requests `per_page=n` with no status filter and does not follow pagination), plus the runs `ListRuns` returns for `queued`, `in_progress` and `waiting`, de-duplicated by run id. For each run, the latest attempt's jobs. Jobs are grouped by requested label set (lower-cased, sorted); each group records the labels, up to three workflow/job names and a count of the rest, the number of jobs, and the last time seen. The daemon stores these **observations only**; it does not classify them.

**Running a check.** The scan runs in the background with its own 60-second deadline, at most one per repo at a time (a second request joins the running one). If the deadline hits, the groups gathered so far are kept and marked partial.
- **`POST /repos/{name}/label-check`** starts a scan and returns 202. A repo checked under a minute ago returns 429 with the time it can run again.
- **`GET /repos/{name}/label-check`** never scans. It returns `{state: "not_checked"}`, or `{state: "checking", ...}`, or the last result with `checked_at`, `partial`, and `groups`.
- The cache is keyed by the lower-cased repo name, dropped when the repo is removed, and cleared when the token generation changes.

**Classification happens in the TUI**, against the effective labels computed from the saved global labels and the **draft** repo labels, using `sched.MatchLabels`, the same predicate the daemon schedules with:
- **MATCHED:** the set is not empty and `MatchLabels` accepts it. ghr starts runners for these jobs.
- **UNMATCHED:** the set includes `self-hosted` and is not matched. The missing labels are listed.
- **OTHER:** anything else, most often GitHub-hosted runners such as `ubuntu-latest`. An empty label set (a job routed by runner group) is OTHER and says so.

**The card** shows "Checked 3m ago" (with "partial" when cut short), `[ Check now ]`, and one row per group: badge, labels, job names, count and age. While a scan runs, it shows "checking…" and polls the GET every tick until the state changes. Without any result it reads "Not checked yet". Because classification uses the draft labels, rows update as labels are edited.

**Quick fix.** An UNMATCHED row offers `( + add <label> )` for each missing label that is a custom label. It is never offered for operating-system or architecture labels (`linux`, `windows`, `macos`, `x64`, `x86`, `arm`, `arm64`); when those are missing, the row says "needs a different OS or architecture". The button adds the label to the repo's Labels as an unsaved edit, under a note: "Adding a label changes which jobs ghr accepts; it does not install anything on the runner."

### Maintenance (Settings)

**Reload.** **`POST /reload`** calls a backend method shared with SIGHUP. It re-reads `config.yaml` **and the token file** (`Store.Reload`), which keeps today's all-or-nothing behaviour: a parse error, validation error, owner change or empty token changes nothing and returns 400 with the error. On success it clears the GitHub cache, bumps the credential generation, clears the degraded state, posts each warning to Activity, returns the warnings (`[]` when none), and wakes the daemon loop. The loop runs the tick; the HTTP handler never calls `Tick`. The SIGHUP case calls the same method. The toast shows "config reloaded", "config reloaded (N warnings, see Activity)" or the error.

**Prune now.** **`POST /prune`** runs a forced maintenance prune in the background:
1. Docker build cache down to `build_cache_keep`;
2. dangling images;
3. history and archived logs past `history_retention`;
4. re-read disk use.

Each step's outcome goes to Activity, and a final event says "prune finished" or "prune finished with errors". It differs from the automatic disk check, which acts only above `disk_high_water` and tries a 72-hour cache prune first; that policy is unchanged.

**One prune at a time.** A `maintenance` state under `m.mu` holds `running`, `last_started`, `last_finished` and `last_outcome`; the existing `lastPrune` moves under `m.mu` too. `POST /prune` returns 409 while any prune runs. The tick skips its automatic disk prune and retention prune while one is running, and they run on a later tick.

**Lifecycle.** The handler sets `running` and registers with the manager's wait group before returning 202. The prune runs under a manager-owned context, not the request's. Shutdown cancels that context, so a long Docker command stops instead of outliving `ShutdownWait`; an interrupted prune posts "prune interrupted by shutdown".

**`/status` gains a `maintenance` object** with those four fields; `last_finished` is absent until a prune completes after the daemon starts. The section shows disk use, "last pruned 2h ago (ok)" or "not pruned since the daemon started", "pruning…" while one runs, `[ Reload config.yaml ]` and `[ Prune now ]` (disabled while one runs).

### Errors, availability and path parameters

- **GitHub errors map to daemon statuses:** rate limit to 429 with `retry_at`, authentication or permission to 403, not found to 404, unprocessable to 409, anything else to 502. The `api.Error` type gains an optional `RetryAt`, and the client decodes it.
- **Secondary rate limits.** A 403 whose message mentions "secondary rate limit" is a rate limit, waiting at least a minute, not an authentication failure. Today it would mark ghr degraded.
- **Degraded.** While ghr is degraded, the registrations and label-check endpoints return 503 and the TUI disables their buttons and shows the reason. Token replacement and Reload stay available. All new buttons are disabled while the daemon is unreachable.
- **Repo names** in new paths resolve through `Config.Repo` (case-insensitive) before any GitHub call; GitHub is called with the configured spelling, escaped as `repoURL` does today.
- **Shared rate budget.** A manual check that hits GitHub's rate limit pauses the daemon's own polling until the reset, as any call through the shared client does. The one-check-a-minute limit protects the scheduler as much as GitHub.

## 4. Graphics

### Badges

`ui.Badge(text, kind)` renders ` TEXT ` in bold, with a background and foreground chosen per kind:
- green with black text: active, online, success, matched, OK;
- blue with white text: busy;
- amber with black text: paused, idle, starting, expires soon, unverified;
- red with white text: error, failure, offline, unmatched, rejected;
- grey with white text: removing, cleaning, GHR, other, cancelled, unknown.

The colour profile is read when rendering. Under the ASCII profile (tests and colourless terminals) a badge renders `[TEXT]`, the same width.

**Selected rows keep badge colours.** `row` and `selectedRow` today strip all styling from a selected row; they change to apply the selection style to the text around the badge and leave the badge's own styling. A colour-profile test pins this, since the ASCII goldens cannot.

Badges replace the `● state` text in the Dashboard tables, the Runners table, the Repositories list and panel, the detail page summary, the registrations card and the label check card. A busy runner keeps its spinner, before the badge.

### Metrics

**Sampler.** A new `internal/metrics` package holds a ring of 60 samples behind its own mutex. A sampler goroutine owned by the daemon takes one sample every 60 seconds on its own ticker, independent of the poll loop, so the series spans an hour whatever `poll_interval` is. Each sample carries its timestamp and:
- **live runners:** instances not cleaning, the number the Running tile shows;
- **queued:** the sum of each repo's matching queued jobs from the last poll, the number the Queued tile shows (it lags by up to one `poll_interval`);
- **CPU percent;**
- **memory used.**

The counts come from one `Status()` snapshot. The sampler stops when the daemon's context ends.

**Readers** (on the LXC, verified 2026-10-05):
- **CPU:** `usage_usec` from `/sys/fs/cgroup/cpu.stat`. Percent is the growth since the previous sample divided by elapsed time × CPUs. CPUs come from `cpu.max` when it sets a quota, otherwise `runtime.NumCPU()` (8 on the LXC). The first sample after start, after a failed read, or after the counter goes backwards has no CPU value.
- **Memory:** `MemTotal − MemAvailable` from `/proc/meminfo`, which lxcfs reports for the container (about 182 MB used of 8 GB on 2026-10-05). `memory.current` was rejected because it counts page cache (561 MB at the same moment).
- **Disk:** the existing disk percentage.

A failed read leaves that value absent for that sample; the next sample tries again.

**`GET /metrics`** returns the samples (timestamp, live, queued, cpu, mem; absent values omitted), the current CPU percent, memory used and total, and disk percent. The TUI fetches it on start and then every five ticks.

### Dashboard tiles

Five tiles across the content width:
- **Running:** `1 / 2` and a live-runner sparkline;
- **Queued:** the count and a sparkline;
- **CPU:** a gauge and a sparkline;
- **Memory:** `182M / 8.0G` and a gauge;
- **Disk:** `11%` and a gauge, coloured by the high-water mark as today.

Sparklines use `▁▂▃▄▅▆▇█` and show the newest samples that fit the tile's inside width, newest on the right. A gap of more than 90 seconds between samples, or a missing value, draws a space. Running scales to `global_max` in queue mode and to the series maximum in all mode; Queued scales to its series maximum; CPU scales to 100. Values are cut to the tile width. At 100 columns each tile is 15 wide (11 inside). Below 100 columns or on a short screen the tiles collapse to the one-line summary, as today.

### Repositories Activity card

The card is built from `GET /history?repo=<configured name>&limit=500` (history records the configured spelling). Over the entries from the last 7 days it shows:
- jobs;
- success rate, as successes over finished jobs with a known conclusion;
- average duration, or `–` when there are no jobs;
- a strip of the last 20 results, oldest on the left.

Strip symbols differ even without colour: `■` success (green), `✖` failure (red), `○` cancelled (grey), `?` anything else (grey). When `history_retention` is under 7 days, the window is the retention and the label says so ("last 3d"). When 500 entries are returned, the stats say "≥". The entries are held apart from the History page's rows and filters. Each fetch carries the repo and a request number; a response for an older request or another repo is dropped. It is fetched when the repo is selected and every five ticks while the page shows.

### History page

Each row gets a result badge and a duration bar (`█` filled, `░` empty), scaled to the longest duration among the rows shown; a zero longest duration draws empty bars. The bar is 12 cells when the job column keeps at least 14, 6 cells when that is what fits, and dropped below that. The badge is always shown, and the job column gives way last. The 80-column golden and the narrow fit tests pin these steps.

### Stale responses

Every new asynchronous card carries enough to recognise a stale reply, as the log and event fetches already do:
- **registrations and label check:** the repo and a per-card request number;
- **Activity:** the same, as above;
- **metrics and token:** a request number;
- **token dialog:** the dialog pointer.

A reply older than the newest request for that card, or for a repo no longer selected, is dropped.

## 5. Daemon API summary

| Method | Path | Result |
|---|---|---|
| GET | `/token` | token snapshot (§3) |
| GET | `/repos/{name}/registrations` | GitHub registrations, `[]` when none |
| DELETE | `/repos/{name}/registrations/{id}` | 204; 400 bad id; 404 unknown repo; 409 ghr-namespace, online or busy; 502 lookup failed |
| POST | `/repos/{name}/label-check` | 202 scan started; 429 with `retry_at` within a minute of the last |
| GET | `/repos/{name}/label-check` | `not_checked`, `checking`, or the last observations |
| POST | `/reload` | warnings (`[]` when none), or 400 with the error |
| POST | `/prune` | 202, or 409 while a prune runs |
| GET | `/metrics` | samples and current host figures |
| GET | `/status` | gains `maintenance` |

Each has a matching `api.Client` method and is added to the TUI's `Client` interface and its test fake. While degraded, the registrations and label-check endpoints return 503. GitHub errors map as in §3.

## 6. Code structure

- `internal/tui/repos.go` (new): the Repositories page: list, panel, form, keys and mouse. It takes over the repo field specs, `repoAction`, the warm check, repo key parsing and per-repo patch building from `settings.go`.
- `internal/tui/configpage.go` (new): the `configPage` type shared by Settings and Repositories: form, alert, saving flag, scroll and leave state. The Model holds the config request sequence and merges each config into both pages.
- `internal/tui/settings.go`: global sections only, plus the token and maintenance sections.
- `internal/tui/manage.go` (new): the token dialog, the registrations and label-check cards, and the maintenance actions.
- `internal/tui/ui`: `badge.go` and `sparkline.go` (new); `gauge` moves here from `tui/styles.go`; `TextField` gains `Mask`; a multi-line text row for the Runners get block.
- `internal/tui/view.go`: `row` and `selectedRow` keep badge styling on selected rows.
- `internal/metrics` (new): ring buffer, sampler goroutine, and the CPU and memory readers, with file paths injectable for tests.
- `internal/github`: the response metadata snapshot with credential generation; `Runner.Labels`; `ListRecentRuns`; secondary rate-limit classification.
- `internal/daemon/backend.go`: the endpoints of §5. Its `GitHub` interface gains `ListRunners`, `GetRunner`, `DeleteRunner`, `ListRuns` and `ListRecentRuns`, and the test fake follows. The reload method is shared with SIGHUP in `daemon/run.go`. The label-check store with its single-flight scans lives here.
- `internal/api`: routes, the error mapping, `api.Error.RetryAt` and client methods.
- `internal/runner`: the forced maintenance prune and the `maintenance` state under `m.mu`; the tick skips automatic pruning while one runs; `lastPrune` under `m.mu`.

**Suggested build order** (all of it one plan): the daemon endpoints and their tests first (each testable alone), then the `ui` primitives, then the `configPage` split and the Repositories page, then the cards and graphics, then Help and the goldens. That way every step leaves the suite green.

## 7. Error handling

- A failed fetch for a card shows the cleaned error inside the card with a retry button; the rest of the page keeps working.
- All daemon and GitHub text shown in the new cards passes through `clean()` in the TUI. The daemon serves raw text.
- Token values never appear in events, logs, toasts, check errors or API responses.
- A rate-limited card shows when it can retry, from `retry_at`.

## 8. Testing

**TUI:**
- Repositories page: selection by name across refresh, edits kept across repo switches, the save patch contents, the leave guard, Dashboard Edit, and the keys.
- Two config pages: one config response merging into both forms; a save on one page resetting only its own keys.
- The leave guard staying when an edit lands during the save.
- The Runners-get block, including de-duplication.
- The token dialog: masking in every focus state, rejection inside the dialog, and a late reply ignored by a new dialog.
- The registrations card: Delete only on eligible rows, and a stale listing dropped after a delete.
- Label-check classification against draft labels, MATCHED/UNMATCHED/OTHER including the empty set, and the quick-add refused for OS and architecture labels.
- The maintenance buttons and states.
- Badges under both the ASCII and a colour profile, including selected rows.
- Sparklines with gaps and absent values.
- The Activity statistics, with the short-retention window and unknown conclusions.
- The History bar steps.
- Stale-reply drops for every new card.

**Goldens and fit tests:**
- Golden snapshots for the Repositories page at 120 and 100 columns, the Dashboard tiles, Settings, Help at 104 × 22, and History at 120 and 80.
- Narrow (40, 56, 72, 99 columns) and short (22 rows) fit tests for every new page and card. They assert that the buttons and controls are present, not only that no line is too wide.
- A Help test that it scrolls at 103 × 22.

**Daemon:**
- The metrics ring and sampler with a fake clock; the readers against fake files (first sample, a counter going backwards, failed reads).
- The token metadata snapshot: expiry parsing, generation discard of an old response, clearing on replacement and reload.
- Secondary rate-limit classification: primary limit, secondary with and without headers, authentication and permission failures.
- Registrations listing; every delete refusal (namespace, online, busy, lookup failure, bad id, unknown repo); and an absent runner counting as deleted.
- The label-check scan: bounded recent runs, de-duplication, the deadline giving partial results, single-flight, the one-a-minute limit, and cache clearing.
- Reload through the endpoint and through SIGHUP: success, every rejection, and no tick run by the handler.
- Prune: start, overlap refusal, automatic pruning skipped while it runs, events, and shutdown during a prune.
- The GitHub-to-daemon error mapping.

**Acceptance on the LXC** (tmux, as before):
1. Back up `/etc/ghr/config.yaml` and its checksum first.
2. Drive in tmux:
   - Dashboard Edit opens the repo;
   - a ghr-e2e label added and saved;
   - the Runners-get block;
   - a label check on ghr-e2e;
   - the token section;
   - Reload;
   - Prune now through to "prune finished" in Activity;
   - the metrics tiles after two minutes.
3. Create a throwaway registration with `gh api -X POST repos/darkraise/ghr-e2e/actions/runners/generate-jitconfig` (fields `name=accept-stale-<random>`, `runner_group_id=1`, `labels[]=self-hosted`, `work_folder=_work`), record its id, and never start it. Delete it through the registrations card. If the run fails first, delete it with `gh api -X DELETE`.
4. Dispatch ghr-e2e's `single` workflow once. While its runner is registered, check that the `ghr-…` row has no Delete button.
5. Leave darkcloud's `linux-1` alone.
6. Copy the backed-up `config.yaml` back, `systemctl reload ghr`, and compare checksums.

## 9. Delivery

One implementation plan, merged to master and released by CI as the next patch version, then deployed to the LXC with `GHR_VERSION` pinned and checked as in §8.

## Out of scope

- Editing `owner` (it still needs a daemon restart).
- Reading workflow files or adding Contents permission to the PAT.
- Persisting the metrics series across daemon restarts.
- Organisation-level runners and runner groups.
- The deferred rows of the 2026-10-04 register, which stay deferred.

## Appendix: review dispositions (2026-10-05)

Fable (judge seat, Fable 5.1) and Codex (`gpt-6-astra`, high effort, read-only) reviewed the first draft. Every finding was checked against the code before it was ruled on. All findings are adopted into the text above except as noted:

| Finding | Ruling |
|---|---|
| Codex 1 (blocker), Fable 4.1: deleting a registration in the window between GitHub registration and instance record | Adopted using Fable's remedy: the whole `ghr-<repo>-<6 hex>` namespace is refused. Codex's ownership-reservation mechanism was not adopted: the namespace rule closes the same window without new shared state, and reconciliation already owns that namespace (`adopt.go:136-158`). |
| Codex 2: fresh check and fail-closed delete | Adopted, with the last-observed limitation stated (§3). |
| Codex 3, Fable 3.3: `self-hosted` is not required for a match | Adopted: classification uses `MatchLabels` alone; HOSTED became OTHER. |
| Codex 4: quick-add can advertise an OS the runner lacks | Adopted: never offered for OS or architecture labels, with a note. |
| Codex 5, Fable 3.1 and 3.2: unbounded `ListRuns`, 5-second TUI timeout | Adopted: `ListRecentRuns`, a background scan with a 60 s deadline and partial results, and POST to start / GET to read. |
| Codex 6, Fable 3.4: cached classification goes stale | Adopted: the daemon caches observations; the TUI classifies against draft labels. |
| Codex 7, Fable 2.1: page-local request sequences | Adopted: one Model-level sequence, both forms merge, saves tagged by page. |
| Codex 8: save-and-leave drops edits made during the save | Adopted; also fixes today's Settings page. |
| Codex 9 to 11, Fable 1.1, 5.1 to 5.3: prune and reload lifecycle | Adopted: a forced prune sequence, one maintenance lock across manual and automatic pruning, a manager-owned context cancelled on shutdown, reload of config and token, and only the loop ticks. Codex's "drain instead of cancel" option was rejected: Docker commands can run ten minutes, past `ShutdownWait`. |
| Codex 12: tick-based sampling is uneven | Adopted: an independent 60-second sampler with timestamps. |
| Codex 13, Fable 1.6: chart counts differ from the tiles | Adopted: the series sample exactly the tile quantities; the all-mode scale is defined. |
| Codex 14 and 15, Fable 1.3, 3.5: token state and metadata | Adopted: `ok`/`rejected`/`unverified`, a metadata snapshot per credential generation, and limit and reset recorded. |
| Codex 16, Fable 5.5: stale async replies | Adopted (§4 Stale responses). |
| Codex 17, Fable 2.5: masking only while editing leaks the token | Adopted: masked in every state. |
| Codex 18: secondary rate limits classed as auth failures; `github.APIError` becomes 500 | Adopted (§3 Errors). This is a live bug in v0.1.6. |
| Codex 19 to 29 and Fable 1.2, 1.4, 1.5, 1.7 to 1.9, 2.2 to 2.4, 2.6 to 2.10, 3.6, 3.7, 4.2, 4.3, 5.4, 5.6, 5.7, 6.1 to 6.7 | Adopted. Fable 2.4's list width changed the list from 26 to 30 columns, because the 26-column list left 7 columns for names. Codex 25 asked for an unlimited history query; a 500-entry limit with a "≥" marker was chosen instead, to keep the reply within the TUI's 5-second request limit. |
| Fable 6.8 (scope note) | Adopted as the suggested build order in §6. |
