# ghr: web logic fixes

**Date:** 2026-10-12
**Status:** design presented in chat and approved by the owner on 2026-10-12, with the rulings on job results (row 78), the unscheduled backend rows (sub-project 7) and the execution strategy made the same day. Amended the same day while the plan was drafted (§2.3, §2.5, §3.1, §3.2, §3.3, §3.4, §3.6, §4.1, §4.2 and §5.1): each amendment records what the code had to decide, and is reported to the owner with the plan.
**Execution strategy:** sequential — the fixes are small, but many share `api/hooks.ts`, `api/client.ts`, `pages/run.tsx`, `pages/repository.tsx` and `lib/settings-form.ts`, and the dev host is the limit: concurrent vitest runs are what makes the web suite time out (register rows 109 and 225) and `/tmp` is a nearly full tmpfs (row 224).
**Register:** docs/superpowers/registers/2026-10-11-ghr-comprehensive-review.md (rows 33, 75 to 109, 225 and 279 to 284). This is sub-project 4 of 7.
**Code:** `web/` (sources, tests, `vite.config.ts`, `tsconfig.json`, `package.json`), `.github/workflows/ci.yml` (one test step), and `internal/model` with the tests of `internal/history` and `internal/activity` (row 78). No other daemon change.

## Goal

The review of build `forge-ui-930381d` found no critical or high defect in the web UI's logic. It found a route guard that asks the daemon twice on every navigation and replaces the whole page when one check fails, number fields that silently change what was typed into a value the daemon reads as unlimited, a log viewer whose work per second and per keystroke grows with the log, results shown as Failed that are not failures, a change stream that can miss a gap or go silent unnoticed, and a list of smaller faults in forms, error texts and polling. This spec fixes them.

All 36 original rows were checked against the code on 2026-10-12. Thirty-five hold as recorded or more widely; row 91's browser failure cannot be told without a browser (§7). The check added six rows, 279 to 284.

## Decisions

- **Targeted fixes.** Nothing is restructured and no dependency is added.
- **A result ghr does not know stays Failed** (owner's ruling, 2026-10-12). `neutral`, `stale` and `action_required` get their own word and tone; `timed_out`, `startup_failure` and any value GitHub adds later read as Failed, so a new failure-like result can never look harmless. The daemon's rule changes with the browser's so the History filter and the charts agree with the icon.
- **The log viewer gets no virtual list.** The fix removes the work that scales with the line count and lets the browser skip lines that are off screen. A virtual list fights wrapped lines of unknown height and the jump to a line.
- **Row 33 is fixed in the browser** (owner's ruling, 2026-10-11), and a gap of a few milliseconds stays (§3.4).
- **The 34 backend rows found during sub-projects 1 to 3 are sub-project 7** (owner's ruling, 2026-10-12), designed after sub-project 6.

## 1. Session and routing

### 1.1 The route guard (row 75, and the login calls of row 106)

`appRoute.beforeLoad` in `router.tsx` fetches `/auth/state` and then `/api/setup`, both with `staleTime: 0`. The router runs `beforeLoad` on every history change, including one that changes only the search or the hash. A failed auth check throws, so the route error page replaces the Shell until Retry is pressed. Opening `/login` and logging in makes four `/auth/state` calls.

- The guard and the login page share one auth query: one key, one function, fresh for 5 seconds (`POLL_SLOW`). The setup check gets the same freshness.
- When the auth fetch fails and the cache holds an answer that says the session is authenticated, the guard lets the navigation through. The Shell's status poll and its Reconnecting alert report the daemon, as they do today for a failed setup read.
- When the auth fetch fails and the cache holds no such answer (a cold load while the daemon is down), the error page with Retry is shown as today.
- The login page keeps invalidating the auth query after a successful login, so the guard does not read the cached "not authenticated".
- An ended session is still seen at once: every `401` runs the handler in `app.tsx`, which clears the query cache and goes to `/login`.

A login then makes two `/auth/state` calls.

### 1.2 The password forms (rows 88 and 89)

- **Row 88:** a `409` from Set a password, or from a login when no password exists, invalidates the auth query, so the form changes to the mode the daemon is in.
- **Row 89:** the change-password route answers `401` both for a wrong current password and for an ended session. On a `401` the Account form reads `/auth/state`; an ended session runs the unauthorized handler and lands on `/login`, and only a live session reads "The current password is wrong".

### 1.3 Redirect targets (row 98)

`safeRedirect` passes `/<tab>/host`, which the router refuses with an error page. A target that contains a control character, a space or a backslash falls back to `/`.

### 1.4 Log out (rows 83 and 281)

- **Row 83:** Log out discards unsaved edits without asking; a test pins that today. `UnsavedGuard` publishes its count of unsaved changes to a small module store, and the Shell reads it: with unsaved changes, Log out first asks "Log out and discard N unsaved changes?" through the existing confirm dialog.
- **Row 281:** a failed logout request is swallowed, the query cache is cleared, and with the session still valid the `/login` guard sends the user back to the Dashboard. After a failed request the Shell reads `/auth/state`: when it says the session has ended, log out proceeds as today; when it says the session is live, or cannot be read, the page stays, the cache is kept and a toast says the log out failed.

## 2. Forms and drafts

### 2.1 Max and Warm (rows 76 and 279)

The Add repository dialog clamps Max, and the repository page clamps Max and Warm: a negative number becomes 0, which the daemon reads as unlimited for Max, 150 becomes 99 and 1.9 becomes 1, all without a message. The daemon refuses a value below 0 or above 99 with a message of its own.

- Neither form changes what was typed. The field shows the typed text.
- A value that is not a whole number from 0 to 99 shows an inline error with the rule the settings form already uses, and blocks Add or Save.
- An emptied Max or Warm keeps today's meaning. The "Warm must be <= max" rule stays.
- The unreachable "Pick a repository first" message in the dialog goes with row 101.

### 2.2 The unsaved-changes guard (row 79)

Dismissing the dialog with Stay or Escape while its Save runs leaves the guard's saved flag set, and the next blocked navigation is released without a dialog. The guard clears the flag whenever no navigation is blocked.

### 2.3 Drafts (row 80)

A draft entry is written on every edit and removed only for keys a save sent. An edit typed back to the loaded value stays in the draft, and counts as an unsaved change once the daemon's value moves, for example after a CLI edit; Save then reverts that edit.

- An edit whose value equals the loaded value removes the key from the draft. One helper in `lib/draft.ts` serves the settings form and the repository page.
- When a config read brings a value equal to a draft entry, that entry is removed.
- Equality is of the stored values: `60s` typed over `1m` is a change.
- Pruning pauses while a save is in flight. The loaded value is then the one being replaced, and an edit back to it is a real edit that must survive the save.

### 2.4 The read after a save (row 97)

The read that confirms a save can join a poll that left before the save, and then reports "Daemon did not apply". The save cancels the config query's fetch in flight before it reads. The settings form and the repository page share one helper for this; they duplicate the block today.

### 2.5 The setup wizard's Settings step (row 82)

- Back and Skip the rest go through the same "Save your changes?" dialog as Next when settings are unsaved.
- A reload or a closed tab warns while settings are unsaved, as the Settings page does. The step mounts the existing `UnsavedGuard` unchanged for this, so the guard also asks before the browser's Back button leaves the wizard.
- Skip the rest asks for confirmation, shows a busy state and cannot be pressed twice.
- Skip the rest sends the toolchain choice Finish would send: the popular set when it is pending or was picked, none otherwise. The confirmation says which. Today it always sends none and so declines a pending set.
- A failed config read shows the error with Retry and keeps Back, Next and Skip the rest. Today the step shows "loading…" with no way on until a retry succeeds.

### 2.6 The tag field (row 103)

A tag over 100 characters or a duplicate is dropped and the text cleared, with no message. The duplicate check is exact, while the daemon refuses labels that differ only in case, and the length counts UTF-16 units where the daemon counts characters.

- A refused tag keeps the typed text and shows why inline.
- For labels, a duplicate is found ignoring case. Cleanup prefixes keep the exact comparison; the daemon has no case rule for them.
- Length is counted in characters.

The daemon's other label rules (spaces, commas, the system labels) stay the daemon's to report.

### 2.7 A removal in progress (row 107)

The daemon cancels a removal when the repository is resumed, but the web UI locks Pause and Resume while a repository is being removed. While removing, the toggle reads "Cancel removal" and sends the resume; Remove stays locked. Known limit: the resume also resumes a repository that was paused before the removal began.

## 3. API client and live data

### 3.1 Error bodies (rows 81 and 102)

- **Row 81:** a failed response's whole body becomes the error message unless it is JSON with an `error` field, so a proxy's HTML page fills a toast. The message is the JSON `error` when there is one, the body when the response is `text/plain` (cut at 200 characters, counted in UTF-16 units; the daemon's plain-text errors are ASCII), and otherwise the status code and status text.
- **Row 102:** only `202` and `204` count as bodiless. Any successful response with an empty body is accepted.

### 3.2 Silence and timeouts (row 85)

Nothing in the browser notices a change stream that has gone silent, and no request has a timeout; a hung poll also blocks that query's later polls, which reuse the request in flight.

- The stream reader ends its attempt after 45 seconds without a byte, the wait for the response's headers included: a connection that never answers is the same silence. The server writes a ping every 15 seconds when idle. The ended attempt takes the existing path: polls return to their fast interval, the stream retries, and the next hello refetches (§3.4).
- A request fails after 70 seconds with a plain "the daemon did not answer" error, an `Error` and not an `ApiError`, since no status code fits it; the web listener cuts a response after 60. Stop runner gets 110 seconds, because its route allows a stop 100. The raw-log download uses the default.

### 3.3 Polls that hide data (row 84)

Runner steps, the History table, registrations and the repository picker test for an error before they test for data, so one failed poll replaces what is on screen; the History table offers no Retry. Each keeps its data and shows the error line with Retry above it, as the run page does. The runner panel's containers have the same fault in the same file and get the same fix.

### 3.4 Stream gaps and double fetches (rows 33 and 86)

The stream's hello carries no snapshot and no sequence number. Today only a hello after an earlier hello refetches, so a first attempt that fails (the daemon restarting, 32 subscribers) is followed by a hello that refetches nothing, although the data may be a poll interval old. Re-showing a tab fetches every stream-backed query twice: the query library's focus refetch and the hello's invalidation.

- The stream hook keeps a "missed" flag: set when the page loads hidden, when an attempt ends and when the stream is stopped for a hidden tab. A hello refetches everything when the flag is set, then clears it.
- The eight stream-backed queries stop refetching on window focus and on network return. This is set once, in the query client's defaults for the stream's topics, not on each hook, so it follows the list of topics. The hello is the one trigger. When the stream cannot connect, the fast interval polls carry the page, as today; the setup wizard, which opens no stream, is carried the same way.

Known limit: a change in the milliseconds between a page's first fetch and the hub's subscription is announced by the 60-second safety poll. Closing it means fetching every query twice on each page load or holding the first paint until the stream answers.

### 3.5 Polls nobody is looking at (row 87)

Runner steps and containers poll whichever tab of the runner panel is shown; steps cost a GitHub call every 5 seconds and containers a Docker call. Steps poll only on the Steps tab and containers only on the Containers tab, as the log tail does. A tab fetches when it is opened.

### 3.6 Lists that show the previous answer (rows 93 and 96)

- **Row 93:** reopening Add or Watch shows the previous repository list while the new one loads. The available-repositories query keeps no cache between openings.
- **Row 96:** the History table blanks when its time window moves to the next bucket boundary. The previous rows stay when the repository and result filters are unchanged and only the window moved, that is when the chart's start slid to its next bucket; after a click on 24h, 7d or 30d the old rows are hidden while loading, as today. The rule is in the History page, not in `useHistory`: the query library hands a placeholder function the last query that had data, not the previous key's, so the hook cannot tell a slide from a change of window.

### 3.7 A log that is not there yet (row 280)

The daemon keeps a not-found log answer for a just-completed job only 15 seconds and asks GitHub again (row 213), but the job page never asks the daemon again. While the answer is "no log" and the job completed under two minutes ago, the job log is refetched every 15 seconds.

### 3.8 One clock (row 106)

Each `useNow` caller owns a one-second interval. One module-level ticker serves them all.

## 4. Run page, logs and lists

### 4.1 The log viewer (row 77)

The job page re-renders once a second for its clock, and with it every rendered log line; nothing is memoised. Each render regroups all lines of every open step and tokenises each rendered line's ANSI again. A search keystroke strips ANSI from every line of every step. `useJobLog` has no `staleTime`, so each window focus downloads the whole log again. A step renders its last 5000 lines; search or "Show N earlier lines" can mount a whole step, and the daemon serves logs up to 4 MiB.

- The job page's log section (`JobLogs`) and each line (`Line`) are memoised, and the page passes the open step and line as plain values, so the clock's tick stops at the job page. `LogViewer` itself is not memoised: its heading and toolbar are elements made anew on every render, so a memo around it could never skip one.
- A line's ANSI segments and plain text are computed once and cached by the line object.
- The search input is deferred, so typing stays ahead of the matching.
- A log that has steps is never stale. A "no log" answer stays refetchable (§3.7).
- Line rows use `content-visibility: auto` with the intrinsic size `auto 1.25rem`, one line's height, so the browser skips lines that are off screen; `auto` lets it keep the real height of a wrapped line it has already drawn, so the scrollbar and the jump to a line do not shift.
- The 5000-line window stays.

### 4.2 Results (row 78)

Three places map a conclusion to an icon, a pill or a word, and each sends everything it does not list to Failed: `ResultIcon`, the run pill, and `resultWord`. The run pill also shows `unknown` as Failed where the other two say Unknown. The daemon's `model.Failed` applies the same rule to the History "failed" filter and the activity charts.

- One classifier in `lib/status.ts` serves all three:

| Conclusion | Word | Tone |
|---|---|---|
| `success` | Succeeded | success |
| `cancelled`, `skipped`, `unknown` | as today | as today |
| `neutral` | Neutral | muted |
| `stale` | Stale | muted |
| `action_required` | Action required | warning |
| anything else | Failed | failure |

- The run pill shows `unknown` as Unknown, as the other two places do.
- `model.Failed` answers false for the three new names, so the History filter and the chart tallies stop counting them as failures.
- The chart tally keeps its existing default: a result that is neither succeeded, failed nor unknown is counted with cancelled, as skipped is today. No count is added to the API.
- The lanes chart draws a segment of one of the three results in the muted style with its word; its style and word maps gain the three names, and so does its list of job states, without which a job with one of these results is not counted as a job.

### 4.3 ANSI (row 104)

DEL and the C1 control characters join the characters the parser removes. Known limits, not fixed: colour does not carry from one line to the next, and a sequence split by a line break leaves its parameters as text.

### 4.4 Links (rows 105 and 90)

- **Row 105:** `githubLinks` encodes each path segment it puts into a GitHub address. One helper returns an address only when it is `https`, and the four places that render an `html_url` use it; without one they render no link.
- **Row 90:** the repository link in the Dashboard table and the mark link in the lanes chart always call `preventDefault`. A click with Ctrl, Cmd, Shift or Alt, or with another button, is left to the browser. The Dashboard row still does not navigate on such a click.

### 4.5 Downloads (rows 91 and 94)

- **Row 91:** the raw log's object URL is revoked on the next tick. It is revoked after 40 seconds. The log is still buffered in memory, up to the daemon's 32 MiB cap: the route needs the app's request header, so a plain navigation cannot fetch it.
- **Row 94:** no change (§6).

### 4.6 Job-log errors (row 92)

Any `404` on the job log reads "This run no longer exists on GitHub", also when the job is gone or the repository is no longer covered. The job page shows the daemon's own error text. The run page does the same for the run-detail `404`.

### 4.7 History numbers (rows 95, 99 and 282)

- **Row 95:** a job with a zero start time has no duration: its row shows none, and it stays out of the bar scale and the median.
- **Row 99:** a bucket of 12 hours or more is a day bucket, so a 23-hour day reads as a day.
- **Row 282:** the History chart's run total counts unknown-result jobs.

### 4.8 The token page (rows 106 and 283)

- **Row 106:** a token whose expiry has passed reads Expired in the failure tone, not "Expires soon".
- **Row 283:** the note says replacing the token checks every configured repository.

### 4.9 The Actions filters (rows 106 and 284)

Until the config has loaded, Actions filters by the repository named in the address, and changing Status keeps it. Once the config has loaded, a repository that is neither configured nor watched leaves the address, as today.

## 5. Tests and housekeeping

### 5.1 The web suite under load (rows 109 and 225)

`vite.config.ts` sets a 15-second test timeout and caps workers at half the CPUs. This is the plan's first task. Eighteen tests carry a per-test timeout of 10 seconds from the time the default was 5; a per-test timeout wins over the default, so those overrides are removed and no test has less time than the default.

### 5.2 Time zones and the config file (row 100)

- `vite.config.ts` is type-checked.
- The suite stays in UTC. Tests that need local time, the 23-hour day of §4.7 among them, live in their own files and run under `Europe/Berlin`, a zone with daylight saving, through a second npm script and a second CI step.

### 5.3 Dead code (row 101)

- Deleted with their tests: `clock`, `statusWord`, the leftovers of the section index in `FieldGroup`, the stale comment and classes in the setup page, and the unreachable "Pick a repository first".
- Merged: `count()` moves to `lib/format.ts`; the 500 beside `HISTORY_LIMIT` is interpolated; the log colour tokens are written once in `:root`.
- `lib/palette.ts` moves beside the test that is its only user.
- Kept: the `current` option of the run frame's link renderer, which the component's tests use.
- Not here: the two wordings of the runner-update deadline are row 169, sub-project 6.

### 5.4 Test gaps (row 108)

Each gap is closed by the section that fixes its defect: Stay or Escape during a save (§2.2), a server value changing under a draft (§2.3), an HTML error body (§3.1), redirect targets with control characters (§1.3), and a negative or out-of-range Max (§2.1). Two more tests are added: the capacity bar at the largest `global_max`, 99, and a breadcrumb with a query string through the real router.

## 6. Rows with no behaviour change

- **Row 94:** deferred as a known limit. A failure the daemon reports for an artifact already shows as a toast. Only a failure at GitHub's signed address, issued a moment before, replaces the app with GitHub's error document. The page's content security policy rules out a hidden frame, and a tab opened ahead of the request would flash on every successful download.

## 7. Verify before fixing

- **Row 91:** whether revoking on the next tick fails a download in Safari or Firefox is not verified; the longer delay is safe either way.
- **Row 77:** memoising the log component relies on the query library keeping the job object's identity across refetches that bring equal data. This is its documented default and is inferred, not tested; the plan's test for the tick settles it.
- **Row 86:** that a network return doubles fetches the way a window focus does is inferred from the library's defaults.
- **Row 98:** the already-logged-in path was read to the router's "Redirect blocked" error; the path through the login form's own navigation was not traced. The fix covers both.
- **Row 85:** that a stream stalls in practice is not observed; the watchdog costs nothing while the server pings.
- **Row 82:** the review said the step loads forever; the config poll keeps retrying, so it ends when a read succeeds.

## 8. Testing

Every fix starts from a failing test, in the test file that already covers the code; `lib/redirect.ts` gets its first test file. No new test waits on a real timer.

- **Session and routing:** a search-only navigation inside the fresh window makes no auth call; a failed check with a cached session keeps the Shell; a cold failed check shows the error page; a login makes two auth calls. A `409` switches the password form's mode. A `401` with an ended session leaves the Account form for `/login`. Redirect targets with a tab, a space and a backslash give `/`. Log out with unsaved changes asks first; a failed logout with a live session stays and keeps the cache.
- **Forms and drafts:** `-1`, `150` and `1.9` in Max show an error and block Add and Save, in the dialog and on the repository page. Stay during a save leaves the guard working. A value typed back leaves no draft entry, and a server value equal to a draft entry removes it. A save does not accept a read that left before it. In the wizard, Back and Skip the rest ask about unsaved settings, Skip the rest confirms and sends the pending set, and a failed config read shows Retry. A refused tag keeps its text and says why; `GPU` after `gpu` is a duplicate for labels. Cancel removal sends the resume and Remove stays locked.
- **API client and live data:** an HTML error body gives the status line; an empty `200` succeeds. A silent stream ends its attempt after 45 seconds of fake time; a request fails after 70 and Stop runner after 110. A failed poll keeps the data in the four places. A hello after a failed first attempt refetches; re-showing a tab fetches each stream-backed query once. Containers do not poll on the Steps tab. A reopened dialog shows no old list. The History rows stay when the window moves. A fresh "no log" answer is asked again after 15 seconds. Two `useNow` callers share one interval.
- **Run page, logs and lists:** a clock tick does not re-render a log line; a line is tokenised once; a log with steps is not fetched again on focus. Each conclusion in §4.2's table gives its word and tone at all three places, and `timed_out` stays Failed; `model.Failed` answers false for the three names, the History filter leaves them out of "failed", and the tally counts them with cancelled. DEL and C1 characters are removed. A workflow path with a space is encoded; an `http` address renders no link. A Ctrl click is not prevented. A zero start time gives no duration and does not flatten the bars; a 23-hour bucket is a day. An expired token reads Expired. Actions filters by `?repo=` before the config loads and keeps it on a Status change.
- **Suites:** in `web/`: `npm test`, the time-zone script, `npm run typecheck` and `npm run lint`. For the Go change: `go vet ./...` and `go test -race ./...` with `TMPDIR` and `GOTMPDIR` on the root disk (Go is at `/usr/local/go/bin`).
- **Browser check:** deploying to the runner LXC needs the owner's yes each time. With a yes: a job page with a log of several thousand lines (typing in the search, scrolling, the jump to a line, the browser's own find), a Ctrl click on a Dashboard repository and on a chart mark, a daemon restart while a page is open (the Shell stays and the data refreshes after the reconnect), and the raw-log download.

## Out of scope

- Layout, contrast and overflow (rows 110 to 137): sub-project 5.
- UX and accessibility (rows 138 to 174), among them the Max help text and dialog focus (row 149), field errors tied to their inputs (row 141), the varying words for one state (row 151), paging for History and Actions (row 145) and the wording of the runner-update deadline (row 169): sub-project 6.
- The 32 backend rows of sub-project 7, the owner's live check (row 204) and `/tmp` on the dev host (row 224).
- A snapshot or sequence number in the stream's hello, which would close §3.4's known limit from the server.

## Row map

| Row | Section |
|---|---|
| 33, 86 | §3.4 |
| 75 | §1.1 |
| 76, 279 | §2.1 |
| 77 | §4.1 |
| 78 | §4.2 |
| 79 | §2.2 |
| 80 | §2.3 |
| 81, 102 | §3.1 |
| 82 | §2.5 |
| 83, 281 | §1.4 |
| 84 | §3.3 |
| 85 | §3.2 |
| 87 | §3.5 |
| 88, 89 | §1.2 |
| 90, 105 | §4.4 |
| 91 | §4.5 |
| 92 | §4.6 |
| 93, 96 | §3.6 |
| 94 | §6, deferred |
| 95, 99, 282 | §4.7 |
| 97 | §2.4 |
| 98 | §1.3 |
| 100 | §5.2 |
| 101 | §5.3 |
| 103 | §2.6 |
| 104 | §4.3 |
| 106 | §1.1 (login calls), §3.8 (clock), §4.8 (token), §4.9 (Actions) |
| 107 | §2.7 |
| 108 | §5.4 |
| 109, 225 | §5.1 |
| 280 | §3.7 |
| 283 | §4.8 |
| 284 | §4.9 |
