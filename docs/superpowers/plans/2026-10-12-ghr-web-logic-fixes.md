# Web Logic Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close register rows 33, 75 to 109 (except 94), 225 and 279 to 284: a route guard that stops asking the daemon on every navigation and keeps the page when one check fails, number fields that show what was typed, a log viewer whose work no longer grows with the log, results that read as what they are, a change stream that notices gaps and silence, and the smaller faults in forms, error texts and polling.

**Architecture:** Targeted fixes, file by file, and nothing is restructured. Six groups that own separate files: the test suite and housekeeping (`web/vite.config.ts`, the time-zone tests, the theme tokens), session and routing (`router.tsx`, `login.tsx`, `shell.tsx`, the unsaved-changes guard), forms and drafts (the Add repository dialog, the repository and settings pages, the setup wizard), the API client and live data (`api/client.ts`, `api/live.ts`, `api/use-change-stream.ts`, `api/hooks.ts`), the run page and job log (`pages/run.tsx`, the log viewer), and results, links, History, the token page and Actions, with one Go function (`model.Failed`). Three files are shared by named function: `api/hooks.ts`, `lib/history.ts` and `components/runner-panel.tsx`.

**Tech Stack:** TypeScript 5.7, React 19, TanStack Router and TanStack Query 5, the darkraise-ui kit, Tailwind 4, Vitest 4 with Testing Library and jsdom, all already dependencies; Go 1.26 for one function and its tests.

**Spec:** docs/superpowers/specs/2026-10-12-ghr-web-logic-fixes-design.md

**Execution:** inline — `claude --model sonnet --effort high` — 5 of 84 tasks are heavy (9, 23, 28, 52, 63) and are delegated; the other 79 run in the session

**Parallelism:** sequential — many tasks share `api/hooks.ts`, `pages/run.tsx`, `pages/repository.tsx` and `lib/settings-form.ts`, and the dev host is the limit: concurrent vitest runs are what makes the web suite time out, and `/tmp` is a nearly full tmpfs

## Global Constraints

- Web commands run from the repository root in the form the task gives (`cd web && npx vitest run src/...`). `/tmp` is a small tmpfs that is nearly full: before the first command of a session run `mkdir -p /root/.cache/ghr-plan-tmp`, and put `TMPDIR=/root/.cache/ghr-plan-tmp` in front of every `npm`, `npx` and Go command. Give every command a `timeout` (300 seconds for one test file, 900 for the whole suite).
- A fresh worktree has no `web/node_modules`. Once, before the first web command: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 600 npm ci --prefer-offline --no-audit --no-fund`. `web/package-lock.json` must not change in any task.
- While working on a task run only the test files its commands name, one `vitest run` at a time; only the last task runs the whole suite. A test that only times out is the loaded host, not a failure: run it once more, alone, before believing it. Never start a dev server, a watcher (`vitest` without `run`, `--watch`) or any background process.
- A time-zone test file (`*.tz.test.ts`) runs only through `cd web && npm run test:tz -- src/path/to/file.tz.test.ts`; a plain `npx vitest run` on such a file finds no tests. This holds from the task that adds the `test:tz` script onward.
- Before each commit run the test command the task names, then `cd web && npx tsc --noEmit` and `cd web && npx eslint src --max-warnings 0`; both must print nothing.
- No new npm dependency and no new Go module dependency.
- No new test waits on a real timer: use `vi.useFakeTimers()` or the helpers under `web/src/test/`. Shared test helpers under `web/src/test/` may gain an export or a field; nothing existing there is changed or removed unless the task says so.
- Go (one task) is at `/usr/local/go/bin` and not on PATH: prefix every Go command with `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH`, and before its commit run `PATH=/usr/local/go/bin:$PATH gofmt -l internal/` (must print nothing).
- Some agent shells refuse a command with a pipe. Where a task gives a plain form beside such a command, use it.
- User-visible texts (labels, messages, toasts) are part of the spec. Copy them exactly as the task gives them.
- A task's "replace these lines" blocks were written against `master` at `d95cb45` plus the tasks before it that touch the same file. When a block to replace is not found exactly as quoted, find the same lines by their content, apply the same change, and say so in the task report; never skip the change. Three files are edited by tasks from different groups, each in its own functions: `web/src/api/hooks.ts`, `web/src/lib/history.ts` and `web/src/components/runner-panel.tsx`. Change only what the task names in them and do not reformat them.
- The test counts a task quotes in its "Expected: PASS" line were taken with only that group's earlier tasks applied. A higher count of passing tests is not a failure; a failing test is.
- When an existing test fails because it asserted behaviour this plan changes, the task says so and gives the corrected lines. Report any other existing-test failure in the task report; never change production code to make an old expectation pass.
- Code comments: only where the reason is not obvious from the code. No comment mentions a task, a register row or a fix.
- Styling goes in the component as Tailwind classes. Only the task that names `web/src/styles/ghr-theme.css` edits it.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no trailing period, no attribution or co-author lines.
- No write to GitHub, no host change, and never deploy to the runner LXC (192.168.0.99).
- English only.

## Contracts

Names one task adds and a later task uses, and the user-visible texts the plan words.

- **`testTimeout: 15_000` and `maxWorkers: "50%"` in `web/vite.config.ts`** (Task 1). Every later `vitest` run gets them unless it passes `--testTimeout` or `--maxWorkers`, or the shell sets `VITEST_MAX_WORKERS`.
- **`web/vite.config.ts` is type-checked** (Task 2): `"include": ["src", "vite.config.ts"]` in `web/tsconfig.json`. Any later edit of the config must pass `npx tsc --noEmit`.
- **npm script `test:tz`** (Task 3): `"test:tz": "GHR_TEST_TZ=Europe/Berlin vitest run"`. Used by Task 4 (CI) and Task 5. One file: `cd web && npm run test:tz -- src/path/to/file.tz.test.ts`.
- **File-name rule `src/**/*.tz.test.{ts,tsx}`** (Task 3). A test that needs local time goes in a file with this ending. `npm test` and a plain `npx vitest run` never run such a file (a plain run of one by path answers `No test files found, exiting with code 1`); `npm run test:tz` runs only such files, under `Europe/Berlin`.
- **Environment variable `GHR_TEST_TZ`** (Task 3), read in `web/vite.config.ts`: when set, it is the zone and only `*.tz.test.*` files run; when unset or empty, the suite is in UTC as before.
- **`web/src/test/zone.tz.test.ts`** (Task 3): asserts the time-zone run really is in `Europe/Berlin`. Added under `web/src/test/`; nothing existing there was changed.
- **`web/src/lib/history.tz.test.ts`** (Task 5): the time-zone tests of `pickText`. A later task that needs a local-time test of `lib/history.ts` adds to this file.
- **`pickText`'s rule** (Task 5): a bucket of 12 hours or more (`>= 12 * 3_600_000` ms) reads `Showing <Mon D>`; a shorter one reads `Showing HH:MM to HH:MM, <Mon D>`. The texts themselves are unchanged.
- **`web/src/test/palette.ts` and `web/src/test/palette.test.ts`** (Task 6): the new paths of `web/src/lib/palette.ts` and `web/src/lib/palette.test.ts`. Contents unchanged. Task 7's test command names the new test path.
- **The 27 `--log-*` tokens are declared once, in the first `:root` block of `web/src/styles/ghr-theme.css`** (Task 7). A later task that adds or changes a log colour edits that block only; the new test `declares each log colour once, in :root` fails on a second declaration anywhere in the file.
- `web/src/api/auth.ts`, new file, exports `authQuery` (Task 9): `queryOptions({ queryKey: keys.auth, queryFn: ({ signal }) => api.authState(signal), staleTime: POLL_SLOW })`. Used by `router.tsx` (Task 9) and `pages/login.tsx` (Tasks 10 and 11). Anything else that wants the session state through the query cache should use it, so there stays one definition. It imports `keys` and `POLL_SLOW` from `api/hooks.ts`: if another task renames or moves either, this file follows.
- The route guards' setup read (`setupState` in `router.tsx`) is fresh for `POLL_SLOW` (Task 9). A task that changes the daemon's setup state and then navigates must invalidate `keys.setup` first, as `finish` in `pages/setup.tsx` already does; otherwise the guard acts on an answer up to 5 seconds old.
- `web/src/lib/unsaved.ts`, new file, exports `setUnsavedCount(n: number): void` and `unsavedCount(): number` (Task 15). Written by `UnsavedGuard` (Task 15), read by the Shell (Task 16). It is a plain module variable with no subscription: readers call `unsavedCount()` at the moment they need it.
- `UnsavedGuard` (`web/src/components/unsaved-guard.tsx`): props unchanged (`count`, `page`, `saving`, `allow?`, `onSave`, `onDiscard`). New side effect (Task 15): while mounted it writes `count` to `lib/unsaved.ts`, and 0 on unmount. It assumes one guard is mounted at a time; a second guard mounted beside the first (not the case anywhere today, and not the case for a guard in the setup wizard, which renders without the Shell) would overwrite the first one's number.
- `safeRedirect(target)` (`web/src/lib/redirect.ts`, Task 13): same signature; answers `/` for any target holding a control character (Unicode `Cc`), a space or a backslash. A caller that builds a `redirect` value must pass an encoded address such as `router.state.location.href`, never a hand-built string with raw spaces.
- User-visible texts worded here, because the spec gives no wording or only part of it:
  - `Log out and discard 1 unsaved change?` (Task 16): the singular of the spec's `Log out and discard N unsaved changes?`, built with `plural(n, "unsaved change")` the way the guard's own dialog counts.
  - The confirm dialog's action button `Log out`, with the dialog's stock `Cancel` (Task 16).
  - The toast `Log out failed: <error>` (Task 17), in the form of the neighbouring `Reload rejected: <error>` and `Settings not saved: <error>`. The spec says only that "a toast says the log out failed".
  - No new text in Task 12: with `/auth/state` unreadable the Account form shows that request's own error text.
Names and signatures a later task uses:

- Task 18: `runnerCountError(name: string, value: unknown, min: number): string`, exported from `web/src/lib/settings-form.ts`. Returns `""` for a whole number from `min` to 99, `` `${name} must be >= ${min}` `` for anything else below or not whole, `` `${name} must be <= 99` `` above 99. `Number("")` is 0, so the caller decides about an empty box. Used by `fieldErrors` in the same file (Task 18) and by `web/src/pages/repository.tsx` (Task 19).
- Task 19: the shape of a draft entry for `max` and `warm` in `RepositoryForm` (`web/src/pages/repository.tsx`): `undefined` for an emptied box, a `number` for a count the daemon accepts, the typed text as a `string` for a value it would refuse. Anything that reads `values.max` or `values.warm` must expect a string.
- Task 20: `prune(draft: Values, loaded: Values): Values`, exported from `web/src/lib/draft.ts`. Drops the entries whose stored value equals the loaded one (`sameValue`); returns the same object when it drops nothing. Used by Tasks 21 and 22.
- Task 21: on the settings form an unsaved change is a draft entry whose stored value differs from the loaded one (`changedKeys(draft, loaded)` with no third argument). A duration typed in another form, or a space typed after a value, counts and is sent by Save; `equal`, the looser comparison, is used only to check that the daemon applied a save.
- Task 22: `withMax(max: number): Config`, a helper at the top of `web/src/pages/repository.test.tsx`: the fixture config with `darkmem`'s `max` set. Used by Task 24's test.
- Task 23: new file `web/src/lib/saved-config.ts` with `readSavedConfig(queryClient: QueryClient): Promise<Config>`: cancels the config query's fetch in flight, then fetches and resolves with the config. Used by `web/src/lib/settings-form.ts` (Task 23) and `web/src/pages/repository.tsx` (Task 24).
- Task 25: `renderAppInBrowser(path: string)`, a new export of the shared test helper `web/src/test/render.tsx`, with the return value of `renderApp`. It renders the app over the router's browser history, the only history that listens for `beforeunload`, and undoes that history when the test ends (`onTestFinished`). `renderApp` is unchanged. Nothing else was added to `web/src/test/`.
- Task 25: `unsavedSettings(over?, open?)`, a helper at the top of `web/src/pages/setup.test.tsx`, used only by Task 25's tests.
- Task 26: `skipBusy?: boolean` on `NavProps` in `web/src/pages/setup.tsx` (local to that file).
- Task 28: `exact?: boolean` on `TagField` (`web/src/components/tag-field.tsx`). Default: a duplicate is found ignoring case. Used by Task 29 for the Prefixes field.
- Task 30: `toggleLocked: boolean` and `toggleLabel: "Pause" | "Resume" | "Cancel removal"` in the object `useRepoActions` returns (`web/src/lib/use-repo-actions.ts`); `locked` is from then on for Remove only. Used by Tasks 31 and 32.

User-visible texts the plan words (the spec gives none for these):

- Tasks 18 and 19: `Max must be >= 0`, `Max must be <= 99`, `Warm must be >= 0`, `Warm must be <= 99`. Built from the settings form's existing rule; they also mirror the daemon's `<repo>: max must be >= 0` and `<repo>: max must be <= 99`. The existing `Warm must be <= max` is unchanged.
- Task 26: dialog title `Skip the rest of setup?`; body `Setup finishes now and queues the popular toolchain set.` or `Setup finishes now. No toolchain set is queued.`; action button `Skip the rest`. Cancel is the confirm dialog's own button.
- Task 28: `Must be at most 100 characters` and `Already listed as <the tag already in the list>`.

Texts taken from the spec or already in the code: `Cancel removal` (spec §2.7, Task 30); `Save your changes?` (existing dialog, Task 25); `Try again` (the existing button of `ErrorLine`, Task 27).
- Task 37 adds `parsedLine(line: { text: string }): ParsedLine` and `export interface ParsedLine { segments: AnsiSegment[]; text: string }` in `web/src/lib/ansi.tsx`. Task 38 uses `parsedLine`.
- Task 37 changes the signature of `renderAnsi` in `web/src/lib/ansi.tsx` from `(raw: string, query = "", current?: number)` to `(line: { text: string }, query = "", current?: number)`. Its only production caller is `web/src/components/log-viewer.tsx`, changed in the same task. `ansiSegments(raw: string)` and `plainText(raw: string)` are unchanged.
- Task 42 changes `useJobLog` in `web/src/api/hooks.ts` to `useJobLog(repo: string, jobId: number, enabled: boolean, settling = false)`. Task 43 passes the fourth argument. Existing three-argument callers keep working.
- Task 41 gives `useJobLog` a `staleTime` (a function: `Infinity` when the answer has steps, else `0`); Task 42 gives it a `refetchInterval` (15 000 ms while `settling` and the answer's `no_log` is true, else `false`). The 15 000 is written inline in the function, because the run page tasks change only that function of `hooks.ts`.
- Task 36 adds to `web/src/pages/run.test.tsx`: a file-level `vi.mock("@/lib/ansi", …)` that wraps `renderAnsi` in a spy calling the real function, a file-level `afterEach(() => { vi.useRealTimers() })`, the helper `const daemonClock = () => ({ ...fixtures.status, now: new Date().toISOString() })`, and the imports `act`, `afterEach` and `renderAnsi`. Tasks 43 and 45 use `daemonClock`, `act` and the `afterEach`.
- Task 35 adds the same `vi.mock("@/lib/ansi", …)` spy and the imports `vi` and `renderAnsi` to `web/src/components/log-viewer.test.tsx`; Task 39 adds `import { Profiler } from "react"` there.
- Task 41 adds `describe("useJobLog", …)` with `const noLog: JobLog` to `web/src/api/hooks-pages.test.tsx`, between `describe("storageBusy", …)` and `describe("useLabelCheck", …)`, and the imports `act`, `JobLog` and `useJobLog`. Task 42 adds to that block and adds the imports `afterEach` and `vi`. A later task, the one that drops the repository list when its dialog closes, adds `useAvailableRepos` to the same import line from `./hooks`.
- Task 36 changes the props of the page-private component `JobLogs` in `web/src/pages/run.tsx`: `search: JobSearch` becomes `step: number | undefined` and `line: number | undefined`. Task 43 adds `settling: boolean`.
- Task 43 adds `const LOG_SETTLE_MS = 2 * 60_000` and Task 45 adds `const RAW_URL_MS = 40_000`, both private to `web/src/pages/run.tsx`.
- Task 40 adds the classes `[contain-intrinsic-size:auto_1.25rem]` and `[content-visibility:auto]` to every log line row (`Line` in `web/src/components/log-viewer.tsx`).
- Task 46 removes the private function `runError` from `web/src/pages/run.tsx`.
- The run page and job log tasks word no user-visible text; Task 46 shows the daemon's texts unchanged.
- **Error text `the daemon did not answer`** (Task 49). User-visible: it reaches toasts and error lines through `errorText`, and the Shell's alert reads `Daemon unreachable: the daemon did not answer. Retrying.` The spec quotes these words; lower case and no full stop match the daemon's own error texts (`not logged in`, `wrong password`), which the same places show.
- **A timed-out request rejects with a plain `Error`, not an `ApiError`** (Task 49). Code that tests `err instanceof ApiError && err.status === …` sees no status for it.
- **`request<T>(method, path, body?, signal?, timeoutMs = 70_000)`** in `web/src/api/client.ts` (Task 49): an optional fifth parameter is added. Every existing call is unchanged. `REQUEST_TIMEOUT` (70 000), `STOP_TIMEOUT` (110 000) and `withTimeout` are not exported.
- **`ApiError` message rule** (Task 48): JSON `error` when present; else the body cut at 200 characters when the response is `text/plain`; else `"<status> <statusText>"`. `ApiError`, `notifyUnauthorized`, `setUnauthorizedHandler` and `errorText` keep their signatures.
- **`request` resolves `undefined` for any successful response with an empty body** (Task 48), not only `202` and `204`.
- **`readChanges` ends an attempt after 45 seconds without a byte** (Task 50). Signature and return values unchanged; `STREAM_SILENCE` is not exported.
- **`createQueryClient()` sets `refetchOnWindowFocus: false` and `refetchOnReconnect: false` as query defaults for every key in `TOPIC_QUERIES`** (Task 52): the prefixes `["status"]`, `["config"]`, `["metrics"]`, `["token"]`, `["storage"]`, `["activity"]`, `["history"]`, `["events"]`. This covers `useHistory` without editing it. An explicit `refetchOnWindowFocus` or `refetchOnReconnect` on a hook would override the default, so a task that rewrites `useHistory` must not add one. A test that needs these defaults must build its client with `createQueryClient()`; `withQuery()` in `web/src/test/query.tsx` makes a bare client without them.
- **`web/src/query.ts` now imports `web/src/api/use-change-stream.ts`** (Task 52). No source file under `web/src/api/` may import `@/query`, or the two files form a cycle; a test file may, and Task 52's test does. No source file does today.
- **`useSteps(id, enabled)` and `useContainers(id, enabled)`** in `web/src/api/hooks.ts` (Task 56): second parameter renamed from `live`, same type. `RunnerPanel` is their only caller.
- **`useAvailableRepos` has `gcTime: 0`** (Task 57).
- **New files:** `web/src/query.test.tsx` (Task 52), `web/src/components/repo-picker.test.tsx` (Task 55), `web/src/lib/use-now.test.tsx` (Task 58).
- **`setVisibility` in `web/src/api/use-change-stream.test.tsx` dispatches a bubbling event** (Task 52).
- **No new user-visible text besides the timeout error.** The kept-data error lines of Tasks 53 to 55 use the existing `ErrorLine` and its "Try again" button.
Names and files a later task uses:

- `resultState(conclusion: string): string`, exported from `web/src/lib/status.ts` (Task 59). Returns the conclusion for `success`, `cancelled`, `skipped`, `unknown`, `neutral`, `stale`, `action_required`, and `"failure"` otherwise. Used by Tasks 60, 61 and 62. Word and tone come from the existing `stateWord` and `stateTone` of the same file.
- `stateTone("action_required")` is `"warn"` (Task 59) and `stateTone("expired")` is `"bad"` (Task 77): two new entries in the `tones` map of `web/src/lib/status.ts`, which every `StatusPill` reads.
- `web/src/lib/status.test.ts` is a new test file (Task 59).
- `model.Failed(conclusion string) bool` in `internal/model/model.go` answers `false` for `neutral`, `stale` and `action_required` (Task 63). Tasks 64 and 65 rely on it. `internal/model/model_test.go` is a new test file (Task 63).
- The activity API sends a finished job with one of those three conclusions as a lanes segment whose `state` is the conclusion's own name, and counts it in a bucket's `cancelled` (pinned in Task 64, drawn in Task 65). No field is added to the API.
- `web/src/lib/links.ts` and `web/src/lib/links.test.ts` are new files (Task 67).
- `httpsUrl(url: string | undefined): string | undefined`, exported from `web/src/lib/links.ts` (Task 67). Used by Tasks 68, 69 and 70.
- `plainClick(e: { button: number; metaKey: boolean; ctrlKey: boolean; shiftKey: boolean; altKey: boolean }): boolean`, exported from `web/src/lib/links.ts` (Task 71). Used by Task 72.
- `took(entry: HistoryEntry): number | undefined` (it returned `number`) and the new `durations(rows: HistoryEntry[]): number[]`, both in `web/src/lib/history.ts` (Task 75). No later task uses them; any code that calls `took` must handle `undefined`.
- `tokenWord(t, now)` in `web/src/lib/token.ts` can answer `"expired"` (Task 77). Task 78's first test relies on it.
- `count(n: number): string`, exported from `web/src/lib/format.ts` (Task 80). Used by Task 81.
- Removed exports: `clock` from `web/src/lib/format.ts` (Task 80) and `statusWord` from `web/src/lib/actions.ts` (Task 82).

User-visible texts of the results, links, History, token and Actions tasks. The first group is given by the spec; the second is worded by the plan:

- From the spec (Task 59, shown from Tasks 60, 61 and 62): `Neutral`, `Stale`, `Action required`; and `Expired` for the token pill (Task 77).
- Worded here, Task 65: the lanes chart's screen-reader words for the three results, `neutral`, `stale` and `action required`, lower case like the words beside them (`cancelled`, `skipped`).
- Worded here, Task 75: the History summary when no listed job has a duration ends after the counts, for example `1 job, 1 failed` or `3 jobs`; the `, median …` part is left out.
- Worded here, Task 78: the token note, `Replacing the token takes effect at once, outside the save bar. It checks read access to every configured repository; registration permissions are checked when a runner next starts.`
- Task 73 adds no new text: it uses the shared error line, whose button reads `Try again`.

## Assumptions (evidence)

- **Base.** The quoted blocks were written against local `master` at `d95cb45`. `master` stands at `197654c` when this plan is saved, and the one commit between them adds a note under `docs/` only: `git diff --stat d95cb45 master` on 2026-10-12. Local `master` is 129 commits ahead of `origin/master` and nothing is pushed: cut the execution worktree from local `master`. A worktree made by the Agent tool's own isolation started at `f5cd90d`, 128 commits behind, on 2026-10-12, and has to be rebased onto `master` before any task.
- **Every task was carried out before its text was written.** Six groups of tasks (Tasks 1 to 8, 9 to 17, 18 to 34, 35 to 47, 48 to 58 and 59 to 82) were each done red-then-green on a branch of their own on top of `d95cb45`, one commit per task, on 2026-10-12. Each group's whole web suite, `tsc --noEmit` and `eslint src --max-warnings 0` were green at its end, with one exception that the plan's order removes: with Tasks 48 to 58 alone, Task 58 (one shared clock) makes `shows the job's heading, runner, labels and steps` in `web/src/pages/run.test.tsx` fail, and Task 36 corrects that test first. That is why Tasks 35 to 47 come before Tasks 48 to 58.
- **Plan order was replayed.** On 2026-10-12 the 82 task commits were cherry-picked onto `master` in plan order (branch `plan/web-logic-order`), and a script checked, for each task, that every quoted "replace" block stands in one of the task's files before its commit and every quoted "with" block after it. One task did not apply as written: Task 57's edit of the import lines of `web/src/api/hooks-pages.test.tsx`, which Tasks 41 and 42 change first. Its text was corrected and its failing and passing runs were repeated at its place in plan order (`Tests  1 failed | 11 skipped (12)`, then `Tests  12 passed (12)`). No other block was missing.
- **The end state was run; the states between tasks were not.** In plan order only Tasks 21, 28, 39, 57, 76 and 83 and the finished tree were run (Task 84's numbers, below). The other tasks' "Expected" lines were taken with only their own group's earlier tasks applied, which is why a count can be higher at execution (Global Constraints). The tree after Task 82 is the tree of the earlier merge check, branch `plan/web-logic-merge` at `81c1da8`: `git diff plan/web-logic-merge plan/web-logic-order~1 -- web internal .github` printed nothing on 2026-10-12.
- **The plan is sequential and Claude-only.** Sequential is the spec's execution strategy. The external executor question was asked on 2026-10-12 and not answered within 15 minutes, so no task carries an Executor line, although the Codex gate printed `usable=true lane=true`.
- **Row 94 is not in this plan.** The spec defers it as a known limit (section 6) and the register holds it as `deferred`. Rows 204 and 224 are the owner's.
- **Assumptions marked `unverified` below that need a browser** (a typed `1.0` staying on screen, the wizard scrolling, `content-visibility` on a long log, the raw-log download in Safari or Firefox, the log colours after the tokens move) are settled by the browser check of the spec's section 8. It needs a deploy to the runner LXC and the owner's yes, so no task makes it; each task they belong to is correct either way, as its bullet says. The CI step of Task 4 is verified by the first push, which this plan does not make.
- **Readings the owner has not ruled on** (register row 308). None blocks a task; each is what the task implements, and each is a small change if the owner prefers the other reading. Row 96 (Task 74): the History rows stay only when the chart's start slides to its next bucket; after a click on 24h, 7d or 30d the old rows are hidden while loading, as today. Row 76 (Task 18): `1.9` in Max reads `Max must be >= 0`, the settings form's shared rule for anything that is not a whole number. Row 82 (Tasks 25 and 27): the wizard's guard also stops the browser's Back button out of the wizard, with a dialog worded "on the Settings page", and the retry button reads "Try again" where the spec says "Retry". Row 107 (Tasks 30 to 32): the toast after Cancel removal still reads "Resumed <name>". Row 89 (Task 12): when `/auth/state` cannot be read after a `401`, the Account form shows that request's own error. Rows 83 and 281 (Tasks 16 and 17): the wording `Log out and discard 1 unsaved change?`, the button `Log out`, the toast `Log out failed: <error>`. Row 78 (Tasks 63 to 65): the three new results fall into the charts' "cancelled" count, as skipped does today, while History's Cancelled filter matches `cancelled` exactly, so chart and filter can disagree.
- **Scores.** Five tasks are heavy, each with a total of 5: Tasks 9, 23, 28, 52 and 63. Task 83 touches eleven files and scores 1 on files: the same one-line edit in ten test files is one shape, the comment a second. Task 13 (redirect targets) scores risk 2, not 3: it only makes an existing check refuse more, and the router refuses the same targets after it.
All commands were run on 2026-10-12. The failing ("red") outputs were captured at the older commit `f5cd90d`; the work was then rebased onto local `master` (`d95cb45`) and every passing check was run again there. The files the red runs depend on (`web/vite.config.ts`, `web/tsconfig.json`, `web/package.json`, `web/src/lib/history.ts`, `web/src/lib/history.test.ts`, `web/src/styles/ghr-theme.css`, `web/src/styles/theme.test.ts`, `web/src/lib/palette*.ts`, `web/src/components/capacity-bar.test.tsx`) are identical in both commits: `git diff --stat f5cd90d master -- <those paths>` lists none of them.

- The installed Vitest is 4.1.11, Vite 6.4.4, TypeScript 5.7.3: `node -e "console.log(require('vitest/package.json').version, ...)"` in `web/`.
- Vitest's default test timeout is 5 seconds and its default worker count outside watch mode is CPUs minus one: `web/node_modules/vitest/dist/chunks/coverage.DM_a_rWm.js:538` (`resolved.testTimeout ??= ... 5e3`) and `web/node_modules/vitest/dist/chunks/cli-api.CnMVyzaz.js:3832-3838` (`Math.max(numCpus - 1, 1)`).
- `maxWorkers: "50%"` resolves to `max(1, min(CPUs, round(CPUs / 2)))` with CPUs from `os.availableParallelism()`: `web/node_modules/vitest/dist/chunks/coverage.DM_a_rWm.js:23-27` and `:222`. On the dev host (`nproc` prints 12) the resolved value is 6: the `resolveConfig` one-liner of Task 1 printed `testTimeout 5000 maxWorkers undefined` before the edit and `testTimeout 15000 maxWorkers 6` after it.
- The 15-second timeout is applied to tests, not only resolved: a scratch test that slept 6 real seconds and asserted `task.timeout === 15_000` passed under the new config (`Duration 6.62s`). The scratch file was deleted and is not part of any task.
- `VITEST_MAX_WORKERS` in the environment overrides the config: `web/node_modules/vitest/dist/chunks/coverage.DM_a_rWm.js:380`. Read only, not run.
- `tsc` did not read `vite.config.ts` before Task 2 and does after: `npx tsc --noEmit --listFilesOnly | grep -c 'vite\.config\.ts$'` matched nothing before and one path after. With the file included, `npx tsc --noEmit` exits 0 with no change to the config.
- The type check of the config catches a misspelt Vitest option: a scratch edit of `maxWorkers` to `maxWorker` gave `vite.config.ts(29,5): error TS2769 ... 'maxWorker' does not exist in type 'InlineConfig'. Did you mean to write 'maxWorkers'?` (reverted).
- Vitest's `test.env.TZ` really sets the zone the tests see, on a host that is not in UTC: the dev host is at `+07` (`date +%Z`, with `TZ` unset in the shell), the red run of `zone.tz.test.ts` received `'UTC'`, and under `GHR_TEST_TZ=Europe/Berlin` the same test passed, including the two `getHours()` checks that differ between winter and summer time. The pool is `forks` (printed by `resolveConfig`).
- The default run excludes the time-zone files and the time-zone run includes only them: `npx vitest list --filesOnly | grep -c '\.tz\.test\.'` printed `0` and the list had 90 files; `GHR_TEST_TZ=Europe/Berlin npx vitest list --filesOnly` printed only `src/lib/history.tz.test.ts` and `src/test/zone.tz.test.ts`; `npx vitest run src/test/zone.tz.test.ts` printed `No test files found, exiting with code 1`.
- `npm run test:tz -- <file>` passes the path through to Vitest: run with `src/lib/history.tz.test.ts`, it ran that one file (`Tests  4 passed (4)`).
- The CI workflow still parses and the new step sits between `npm test` and `npm run build`: PyYAML 6.0.3 loaded `.github/workflows/ci.yml` and listed the `web` steps as `npm ci, npm run lint, npm run typecheck, npm test, npm run test:tz, npm run build, ...`. That the step passes on GitHub's runner is `unverified`: it needs a push, which the brief forbids. `web/.nvmrc` pins Node 24 and the dev host runs v24.18.0, so the runner's Node is the same major version.
- `"GHR_TEST_TZ=Europe/Berlin vitest run"` relies on a POSIX shell for the npm script. The dev host and CI (`runs-on: ubuntu-latest`, `.github/workflows/ci.yml:13`) are Linux. It would not work in Windows `cmd`; no cross-platform helper is used because no dependency may be added.
- The daemon sends three bucket sizes and aligns six-hour and day buckets to local wall time: `internal/activity/activity.go:24-26` (`hour`, `6h`, `day`) and `internal/activity/buckets.go:44-57` (line 45: "Six-hour and day buckets follow local wall time, so DST days run 23 or 25 hours"). So no real bucket is between 7 and 23 hours long.
- The daemon sends bucket times in the zone it was given, with an offset: `internal/activity/buckets.go:109` (`Start: s.In(loc), End: e.In(loc)`), and the browser sends its own zone: `web/src/api/hooks.ts:119` and `:128`.
- `pickText` has one caller, `web/src/pages/history.tsx:256`, and its old threshold is `>= 24 * 3_600_000` at `web/src/lib/history.ts:108` on `master`.
- Europe/Berlin changes clocks on 2026-03-29 and 2026-10-25: the four rows of `history.tz.test.ts` passed after the fix, and before it the 23-hour row received `'Showing 00:00 to 00:00, Mar 29'`, the text register row 99 reports.
- `web/src/lib/palette.ts` is imported only by `web/src/lib/palette.test.ts`: `rg -n "palette" web/src` found no other import, and after the move `npx tsc --noEmit` and `npx eslint src --max-warnings 0` both exit 0. Outside `web/`, only the finished plan `docs/superpowers/plans/2026-10-10-ghr-forge-ui.md` names the old path.
- `web/eslint.config.js:24` already treats `src/test/**` as test code, so the moved files need no lint change.
- The two copies of the log tokens are byte-identical: `diff <(sed -n 77,104p ghr-theme.css) <(sed -n 160,187p ghr-theme.css)` printed nothing.
- No other file declares a `--log-*` token: `rg -l -e '--log-[a-z-]+\s*:' src index.html` in `web/` lists only `src/styles/ghr-theme.css`, and `rg -l -e '--log-(bg|fg|ansi)' node_modules/darkraise-ui` lists nothing. So the kit cannot override the declarations once they are in `:root`.
- The existing log-palette test keeps working after the move because its `tokens(mode)` merges the `:root` block with the mode block: `web/src/styles/theme.test.ts:19-21`.
- The built stylesheet carries each log token once after Task 7: `npx vite build` succeeded and `grep -o -e '--log-bg:' dist/assets/*.css | wc -l` printed `1` (same for `--log-ansi-bright-white:`). Run at the `f5cd90d` base only; the build output was removed again and `web/dist/.gitkeep` restored. That the log viewer looks the same in a browser is `unverified`: it needs a deploy or a dev server, both outside the brief.
- The new capacity tests can fail: with a scratch change in `web/src/lib/capacity.ts` that capped the free segments at 10, both new tests failed and the eight old ones passed (reverted).
- The daemon caps `global_max` at 99: `internal/config/config.go:39` (`const MaxRunners = 99`) on `master`.
Line numbers are from the tree on top of `master` (`d95cb45`), read on 2026-10-12.

- `notifyUnauthorized` is exported and runs the handler `app.tsx` registers: `web/src/api/client.ts:42-50`, `web/src/app.tsx:15-25`.
- The API client runs the unauthorized handler only for a `401` from a path under `/api/`, so a `401` from `/auth/password` or `/auth/logout` redirects nobody by itself: `web/src/api/client.ts:68-70`.
- The daemon answers `401` on the change-password route both for no session (`not logged in`, `internal/webui/handler.go:271-275`) and for a wrong current password (`internal/webui/handler.go:312-313`). It answers `409` for `ErrPasswordSet` and `ErrSetupRequired`: `internal/webui/handler.go:316-317`, with the texts at `internal/webui/auth.go:31-32`.
- `/auth/state` answers `200` with `authenticated: false` for a visitor without a session, never `401`: `internal/webui/handler.go:202-209`.
- The `401` handler clears the query cache before it navigates (`web/src/app.tsx:19`), and the 5-second freshness depends on it. Checked by mutation on 2026-10-12: with that line commented out, `src/router.test.tsx` "shows the login form when a session ends while the auth answer is fresh" (added by Task 9) fails with `Unable to find role="button" and name "Log in"`, and `src/app.test.tsx` "redirects once for a burst of 401s and keeps the page asked for" fails too. The line was restored; `app.tsx` is unchanged. The mutation ran before the rebase; `app.tsx`, `router.tsx` and both test files are identical on `master`.
- Log out clears the cache before it opens `/login` (`web/src/components/shell.tsx:87` after Task 17). Checked by mutation on 2026-10-12 on the final code: with the `queryClient.clear()` of `logout` commented out, three tests of `src/components/shell.test.tsx` fail ("logs out from the top bar" and "lands on /login even when the logout request fails" with `expected '/' to be '/login'`, "asks before logging out past unsaved edits" with `Unable to find role="button" and name "Log in"`). Restored. Run before the rebase; `shell.tsx` and its test are identical on `master`.
- `@tanstack/react-query` is 5.104.1 (`web/node_modules/@tanstack/react-query/package.json`). `fetchQuery` with a `staleTime` answers from the cache while the data is younger, and a failed fetch keeps the earlier data readable through `getQueryData`: both shown by Task 9's tests passing.
- `@tanstack/react-router` is 1.170.41. Its `resolveRedirect` throws `Redirect blocked: unsafe protocol in href` for a target matching `/^[\x00-\x20]*[\\/][\t\n\r]*[\\/]/`: `web/node_modules/@tanstack/router-core/src/router.ts:2585-2596` and `utils.ts:621`. The login form's `navigate({ href })` with `/<tab>/host`, `/ /host` or `/history\x` renders the router's default `Not Found`: observed in the failing run of Task 13 on 2026-10-12.
- The value the guards put into `?redirect=` is always percent-encoded, so Task 13's rule cannot refuse a legitimate way back. Probe run on 2026-10-12 (a throwaway test, not kept): opening `/history?repo=a b\c` without a session gave `redirect: "/history?repo=a+b%5Cc"`, `/repositories/a b` gave `/repositories/a%20b`, and `/history#a b` gave `/history#a%20b`.
- The setup wizard invalidates `keys.setup` after `setupFinish` and before it navigates (`web/src/pages/setup.tsx:264-267`), so the fresher setup answer does not send a finished wizard back to `/setup`. `src/pages/setup.test.tsx` passes with Task 9 in place (run 2026-10-12, before and after the rebase).
- The confirm dialog's API is `ConfirmDialog({ confirm, onClose })` with `Confirm { title, body?, action, destructive?, run }`: `web/src/components/confirm-dialog.tsx:13-21`. `plural(n, word)` is at `web/src/lib/format.ts:94-96`.
- `UnsavedGuard` is mounted by `web/src/pages/settings.tsx:58` and `web/src/pages/repository.tsx:358`, one page at a time, both inside the Shell.
- The alert dialog closes on Escape: shown by the Escape row of Task 14's test.
- ESLint allows `setState` inside an effect in this project (`react-hooks/set-state-in-effect` is off, `web/eslint.config.js:17`), which Task 14's effect relies on, as the code before it did.
- `web/src/pages/settings.test.tsx` was not edited and passes with Task 12 in place (32 tests on top of `master`, run 2026-10-12).
- Unverified: none of the session and routing tasks was run in a browser. The behaviours rest on jsdom tests through the real router and query client.
- The daemon reads a repository Max of 0 as unlimited and refuses Max or Warm below 0 or above 99: `internal/config/config.go:420` (`EffectiveMax returns the repo's cap; 0 means unlimited`), `:513`, `:516`, `:519`, and `MaxRunners = 99` at `:39`.
- The settings form's rule for `global_max` before Task 18: `web/src/lib/settings-form.ts:43-45` at `d95cb45` (`!Number.isInteger(max) || max < 1` gives `global_max must be >= 1`; above 99 gives `global_max must be <= 99`). Task 18 keeps both messages; `web/src/pages/settings.test.tsx` (`checks the fields before sending`) still passes.
- The Add button is the only caller of `add()` and is disabled while nothing is picked, so `Pick a repository first` cannot be reached: `web/src/components/add-repo-dialog.tsx:55` and `:116` at `d95cb45`.
- React leaves a controlled number input alone when its text is loosely equal to a number prop (`element.value != value`): `web/node_modules/react-dom/cjs/react-dom-client.development.js:1852` (react-dom 19.3.0). That a typed `1.0` or `007` stays on screen in a real browser is inferred from this line and `unverified`; a look at the repository page in a browser would settle it. jsdom shows the typed `-1`, `150` and `1.9` (Task 19's test).
- `fetchQuery` hands back the fetch in flight instead of sending a request: `web/node_modules/@tanstack/query-core/build/modern/query.js:301-302` (react-query 5.104.1), and the failing run of Task 23's test on 2026-10-12 (`expected 2 to be 3`). `cancelQueries` reverts a cancelled query to its previous data by default: `web/node_modules/@tanstack/query-core/build/modern/queryClient.js:285`.
- The config query polls every 5 seconds while the change stream is off, 60 while it is on: `web/src/api/hooks.ts:67`. Tests run with the stream off (`web/src/test/setup.ts:9`).
- Only the router's browser history listens for `beforeunload`; the memory history does not: `web/node_modules/@tanstack/history/dist/esm/index.js:331` is inside `createBrowserHistory` (`:174`), and `createMemoryHistory` (`:369`) has no such listener. `UnsavedGuard` asks for the warning with `enableBeforeUnload: count > 0`: `web/src/components/unsaved-guard.tsx:33`.
- `UnsavedGuard`, unchanged, gives the reload warning inside the wizard: Task 25's test `warns before a reload while settings are unsaved` passes over the browser history and fails without the `<UnsavedGuard … />` line (both runs on 2026-10-12).
- `onTestFinished` is exported by vitest 4.1.11: `web/node_modules/vitest/dist/index.d.ts:5`.
- The kit's `Button` with `loading` sets `aria-busy="true"`, disables the button and keeps its label: the existing test `shows a busy Add while the request runs` in `web/src/components/add-repo-dialog.test.tsx`, and Task 26's busy test.
- `ConfirmDialog` calls `run` and closes on its action button, and has a `Cancel` button: `web/src/components/confirm-dialog.tsx:39-42`.
- The daemon counts a label's length in characters and finds duplicate labels ignoring case: `internal/config/config.go:260` (`utf8.RuneCountInString`), `:45` (`MaxLabelLength = 100`), `:281` (`strings.ToLower(strings.TrimSpace(l))`). For cleanup prefixes it only refuses an empty one: `internal/config/config.go:529-531`.
- Resuming a repository clears its removal: `internal/daemon/backend.go:522-526`. A removal also marks the repository paused: `internal/daemon/backend.go:657-658`.
- The status fixture has a repository being removed (`old-repo`): `web/src/api/fixtures/status.json:42-44`. The config fixture has `global_max: 2` and `darkmem` with `max: 2`, `warm: 1`, the label `gpu` and the prefix `darkmem-`: `web/src/api/fixtures/config.json:4` and `:20-28`.
- The document scrolls on every page, so the wizard's own scroll box is dead: `web/src/styles/ghr-theme.css:259-263` (`html, body { height: auto; overflow: visible; }`, unlayered) over the kit's `html, body { @apply h-full overflow-hidden }` at `web/node_modules/darkraise-ui/dist/styles.css:12861-12864`. That the wizard still scrolls in a real browser after Task 34 is inferred from the CSS and `unverified`; opening `/setup` on a short window would settle it.
- Nothing reads the `tabindex` or `scroll-mt-4` of `FieldGroup`: `rg -n "scroll-mt|tabindex|tabIndex" web/src --glob '*.test.*'` on 2026-10-12 finds only `web/src/components/page/field.test.tsx:19` for it, and no source file focuses a group heading.
- Toasts of earlier tests in a file stay on screen: the comment at `web/src/pages/repository.test.tsx` in the test `sends null for an emptied box, so the value is inherited again`. Tasks 23 and 24 place their test above every test that raises a `Daemon did not apply` toast for that reason.
- The query library keeps the identity of the run-detail data (and so of each job object) and of the job-log data across a refetch that brings equal data. Evidence: Task 36's test, run on 2026-10-12; with `structuralSharing: false` added temporarily to `useRunDetail` the test failed with `expected 33 to be 32`, and with it added to `useJobLog` it failed with `expected 37 to be 32`; both edits were reverted.
- `staleTime` may be a function of the query in the installed query library, and `refetchInterval` refetches an answer whatever its staleness. Evidence: `@tanstack/react-query` 5.104.1 in `web/node_modules` (`node -e` on 2026-10-12); Tasks 41 and 42's tests.
- The query library tells its observers about a result on a zero-delay timer, so a test under fake timers must advance the clock after a refetch before it checks the screen. Evidence: on 2026-10-12 Task 36's last assertion did not catch `structuralSharing: false` until the 50 ms advance was added.
- `useNow()` ticks with `setInterval(…, 1000)` and returns `Date.now()` plus the offset to the daemon's clock, taken from `GET /api/status`: `web/src/lib/use-now.ts:24-35`. Task 36's test drives it with vitest fake timers that fake every timer and `Date`, and fails loudly (`expect(runningFor()).not.toBe(before)`) if a rewritten `useNow` does not move within two seconds of fake time.
- A job carries its completion time: `completed_at?: string` in `RunJob`, `web/src/api/types.ts:479`.
- The daemon answers a missing log with `no_log: true` (`web/src/api/types.ts:572`) and keeps that answer 15 seconds for a job that completed under two minutes ago: `internal/daemon/joblogs.go:28-29` and `:175`.
- The daemon's `404` texts are `not found on GitHub` (`internal/daemon/rundetail.go:92`) and `repo <name> is not configured or watched` (`internal/daemon/rundetail.go:79`).
- `web/src/api/client-pages.test.ts:129` calls `useJobLog("darkmem", 1002, false)` with three arguments, which is why `settling` has a default.
- `plainText(raw)` is still called with a plain string by `web/src/components/run-annotations.tsx:98`, which is why Task 37 leaves it alone.
- The breadcrumbs with a query string are built at `web/src/components/run-frame.tsx:60` and `:62` and rendered through `RouterLink` (`web/src/lib/router-adapter.ts:6`); the kit's breadcrumb navigation has the accessible name `breadcrumb`. Evidence for the last: Task 47's test, run on 2026-10-12.
- React's `Profiler` calls `onRender` after the DOM of a commit is updated, in the React 19.3.0 development build the tests run on. Evidence: Task 39's test, which reads the counter inside `onRender` and saw `2 of 4` then `1 of 4`.
- Tailwind 4.3.3 turns the two arbitrary-property classes into `content-visibility:auto` and `contain-intrinsic-size:auto 1.25rem`. Evidence: `npx vite build --outDir /root/.cache/ghr-plan-tmp/dist-chain5` on 2026-10-12 and a search of the built CSS; the build output was deleted afterwards.
- vitest's fake timers work with the suite's fetch mock, with and without `shouldAdvanceTime`. Evidence: Tasks 36, 42, 43 and 45's tests on 2026-10-12.
- `unverified`: that `content-visibility: auto` on the rows speeds up a log of several thousand lines, keeps scrolling, the jump to a line and the browser's find working, and does not clip the current match's outline. A browser check on the runner LXC, which needs the owner's yes, settles it.
- `unverified`: that revoking the object URL on the next tick fails a download in Safari or Firefox (the spec's own open point). A download in those browsers settles it; the 40 seconds are safe either way.
- The web listener cuts a response after 60 seconds: `internal/daemon/run.go:42` (`webWriteTimeout = 60 * time.Second`) and `:287`.
- The stop route lets a stop take 100 seconds: `internal/api/server.go:463` (`killWriteWindow = 100 * time.Second`).
- The change stream pings every 15 seconds when idle: `internal/api/server.go:506` (`heartbeat = 15 * time.Second`) and `:562` (`": ping\n\n"`).
- `AbortSignal.timeout` does not follow vitest's fake timers: a probe test run on 2026-10-12 under vitest 4.1.11 and jsdom 29.1.1 showed a 1-second `AbortSignal.timeout` still not aborted after `vi.advanceTimersByTimeAsync(5000)`. The probe file was deleted.
- `AbortSignal.any` exists in this test environment (same probe: `typeof AbortSignal.any === "function"`; the tests run with Node's own `AbortController`, Node v24.18.0). It is not used: the chained controller needs no assumption about the browser.
- A mocked `Response` body is readable after one `await vi.advanceTimersByTimeAsync(0)`: same probe, and every fake-timer test in Tasks 49 to 52 and 57 relies on it and passes.
- The query library refetches a stale query on a window focus and on a network return, in the installed `@tanstack/query-core` 5.104.1: `node_modules/@tanstack/query-core/src/queryClient.ts:108-119` (subscriptions to `focusManager` and `onlineManager`), `query.ts:491-508` (`onFocus`, `onOnline`), `queryObserver.ts:858-874` (`shouldFetchOn`). Confirmed by running Task 52's test before the change: `expected [ 1, 2, 3 ] to deeply equal [ 1, 1, 1 ]` (2026-10-12). This settles the spec's "inferred" item for row 86.
- The library listens for `visibilitychange` on `window`, not `document`: `node_modules/@tanstack/query-core/src/focusManager.ts:28`.
- An interval poll is skipped while the tab is hidden and resumes on the next tick: `node_modules/@tanstack/query-core/src/queryObserver.ts:529-535`.
- `setQueryDefaults` matches by key prefix: `node_modules/@tanstack/query-core/src/queryClient.ts:901-912` (`partialMatchKey`); confirmed by Task 52's `useActivity`, `useHistory` and `useEvents` rows, whose keys are longer than the prefix.
- `TOPIC_QUERIES` names exactly the eight queries that use `livePoll`: `web/src/api/use-change-stream.ts:8-17` against `web/src/api/hooks.ts` (`useStatus`, `useConfig`, `useMetrics`, `useActivity`, `useHistory`, `useEvents`, `useToken`, `useStorage`).
- Only the Shell opens the change stream, and the setup wizard uses stream-backed hooks without it: `web/src/components/shell.tsx:57`; `web/src/pages/setup.tsx:128` (`useConfig`), `:203-204` (`useStatus`, `useStorage`).
- `request` is called nowhere outside `web/src/api/client.ts`: `rg -n '\brequest[<(]' web/src --glob '!web/src/api/client.ts'` printed nothing (2026-10-12).
- `useSteps` and `useContainers` are called only from `web/src/components/runner-panel.tsx`: `rg` on 2026-10-12.
- `useNow` is called from 13 source files: `rg -ln 'useNow\(' web/src --glob '!*.test.*' | wc -l` on 2026-10-12.
- The retry button of `ErrorLine` reads "Try again": `web/src/components/page/error-line.tsx:12`.
- Fixture values the tests name: a step `Run tests` and a container `ghr-aaaaaa-db-1` (already used by `web/src/components/runner-panel.test.tsx`), and three available repositories, one named `new-repo` (`web/src/api/fixtures/available-repos.json`).
- With `gcTime: 0` the cache entry is removed on a zero-delay timer after its last observer leaves, not synchronously: Task 57's test needs one timer flush between the unmount and the second mount, and passes with it. `unverified` in the browser beyond that: a real reopen is always later than one timer tick.
- That a stream stalls in practice was not observed (the spec says the same); Task 50 costs one timer while the server pings.
- `unverified`: how a real browser's `fetch` rejects when the request's signal aborts with no reason. The tests model it as an `AbortError` rejection (and an errored body stream). `withTimeout` does not depend on the error's shape: it reports a timeout whenever its own timer fired first.
Line numbers are those of local `master` at `d95cb45`, read on 2026-10-12.

- `stateWord` turns a state it does not list into its name with underscores replaced and the first letter capitalised, and `stateTone` answers `muted` for a state it does not list: `web/src/lib/status.ts:45-47` and `:60-65`. So `neutral`, `stale`, `cancelled`, `skipped` and `unknown` need no entry of their own.
- `StatusPill` derives word, tint and icon from the state alone: `web/src/components/page/status-pill.tsx:6-12`. The icon follows the tone except for `cancelled`: `web/src/lib/state-icon.ts:4-5` and `:25-27`. Action required therefore gets the warning tone's clock icon.
- `resultWord` has three callers whose result changes with Task 62: `statusText` in `web/src/lib/run-details.ts:38-41`, `matchesStatus` in `web/src/lib/actions.ts:42-47`, `historySummary` in `web/src/lib/history.ts:89-94`.
- `model.Failed` (`internal/model/model.go:132`) has exactly two callers: `matchConclusion` (`internal/history/history.go:204`) and `finishedState` (`internal/activity/activity.go:92`). Checked with `rg -n 'model\.Failed' --glob '*.go'` on 2026-10-12.
- `finishedState` returns a conclusion that is neither failed nor `success` unchanged, and `tally` counts anything that is not succeeded, failed or unknown under `default` with cancelled: `internal/activity/activity.go:92-100`, `internal/activity/buckets.go:82-93`. Tasks 64 and 65 follow from this; Task 64's tests show it.
- No other Go code or test names `neutral`, `stale` (as a conclusion) or `action_required`: `rg -n 'action_required|"neutral"|"stale"' --glob '*.go'` on 2026-10-12 found only the lines Tasks 63 and 64 add and two unrelated uses of the word stale.
- The lanes chart draws an unlisted state with `fill-muted stroke-muted-foreground` already (`web/src/components/lanes-chart.tsx:69`) and reads out an unlisted state's raw name (`web/src/lib/activity-view.ts:93`).
- Four places render an `html_url` as a link, and only the lanes chart checks the scheme: `rg -n 'html_url' web/src --glob '!*.test.*'` on 2026-10-12 gives `lanes-chart.tsx:102`, `pages/history.tsx:62`, `run-frame.tsx:87`, `runner-panel.tsx:121`.
- The Dashboard row has its own click handler that opens the repository: `web/src/components/repo-table.tsx:58`. That is why Task 71 keeps `stopPropagation`.
- jsdom logs `Not implemented: navigation to another Document` when a click on an HTML link is not prevented, and does not for an SVG link: seen on 2026-10-12 while running `repo-table.test.tsx` (5 lines) and `lanes-chart.test.tsx` (none). Task 71's test helper exists for this.
- A failed refetch leaves the query's `data` in place and sets `isError`: shown by Task 73's test `keeps the rows when a refresh fails and offers a retry above them`, run on 2026-10-12.
- The History table's `since` is the chart's `from` (`web/src/pages/history.tsx:135`) and is part of the query key (`web/src/api/hooks.ts:141-148`), and `useHistory` keeps the previous rows as placeholder data (`hooks.ts:148`). Task 74 relies on all three.
- The query library hands a `placeholderData` function the last query that had data, not the query of the previous key: `web/node_modules/@tanstack/query-core/build/modern/queryObserver.js:319` and `:388`, version 5.104.1. This is why the rule of Task 74 is in the page and not in `useHistory`: after a change of window the key passes through one with an empty `since`, and the hook alone cannot tell that from a slide.
- `isZeroTime` exists and treats `0001-01-01…` and `undefined` as zero: `web/src/lib/format.ts:45-47`.
- unverified: whether the daemon can write a zero `started_at` into the history today. `jobInfo` falls back to now for a start it cannot parse (`internal/runner/lifecycle.go:152-160`) and the entry starts from the spawn time (`:387-390`), so no path was found that does. An entry with `"started_at":"0001-01-01T00:00:00Z"` in a real `history.jsonl` would settle it. Task 75 is correct either way.
- `SetToken` checks the new token against every configured repository that is not being removed: `internal/daemon/backend.go:747-764`; `CheckToken` reads the repository, its runners and its runs (`internal/daemon/backend.go:92-93`). Hence "read access to every configured repository" in Task 78.
- `count` is duplicated in exactly two places and `toLocaleString` is used nowhere else: `web/src/components/stat-card.tsx:30`, `web/src/lib/token.ts:21`. `web/src/components/shell.tsx:19` has a different function of the same name (it returns `undefined` for no number) and is left alone.
- `clock` and `statusWord` have no caller outside their tests: `rg -n 'statusWord|clock\(' web/src` on 2026-10-12; `tsc --noEmit` is clean after both deletions.
- `HISTORY_LIMIT` is 500 and already imported by the History page: `web/src/api/hooks.ts:137`, `web/src/pages/history.tsx:10`.
- The project has no formatter script (`web/package.json` has `typecheck`, `lint` and `test` only), so "do not reformat" in the task texts means hand edits only.
- Nineteen `timeout: 10_000` options stand in eleven test files once every earlier task is applied, eighteen of them test timeouts: `rg -n 'timeout: ?10_000' web/src --glob '*.test.*'` on the branch `plan/web-logic-order` on 2026-10-12. After Task 83 the ten edited test files and `src/test/setup.test.ts` pass (`Tests  182 passed (182)`), and `tsc --noEmit` and `eslint src --max-warnings 0` exit 0.
- `web/src/test/setup.test.ts` asserts only the 3000 of `asyncUtilTimeout`, not the comment: `web/src/test/setup.test.ts:12`.
- The whole-tree numbers of Task 84 were taken on `plan/web-logic-order` on 2026-10-12, with every task of this plan applied in plan order: `npm test` gave `Test Files  97 passed (97)` and `Tests  1384 passed (1384)`; `npm run test:tz` 2 files and 5 tests; `npm run typecheck`, `npm run lint` and `npm run build` exit 0, and `git status --short` was empty after the build; `gofmt -l internal/ cmd/` and `go vet ./...` printed nothing; `go test -race -count=1 -timeout 480s ./...` printed 22 `ok` lines and no `FAIL`.

## Task index

1. Vitest under load
2. Type-check vite.config.ts
3. A Europe/Berlin time-zone suite
4. Run the time-zone tests in CI
5. A 23-hour day reads as a day
6. Move the palette out of lib
7. Declare the log colours once
8. Capacity bar at the largest global max
9. The route guard keeps the auth answer for five seconds
10. The login page shares the guard's auth query
11. A 409 switches the password form's mode
12. An ended session on the Account form goes to /login
13. Redirect targets with a control character, a space or a backslash
14. The unsaved-changes guard forgets a save that was dismissed
15. The guard publishes its count of unsaved changes
16. Log out asks before it discards unsaved changes
17. A failed logout with a live session stays on the page
18. Max in the Add repository dialog
19. Max and Warm on the repository page
20. A helper that prunes a draft
21. The settings form prunes its draft
22. The repository page prunes its draft
23. The read after a save: the helper and the settings form
24. The read after a save: the repository page
25. The setup wizard's Settings step: unsaved settings
26. The setup wizard: Skip the rest
27. The setup wizard: a failed config read on the Settings step
28. The tag field says why it refuses a tag
29. Cleanup prefixes keep the exact comparison
30. The repo actions hook offers to cancel a removal
31. Cancel a removal from the repository row menu
32. Cancel a removal from the repository page
33. Dead code: the section index leftovers in FieldGroup
34. Dead code: the setup page's scroll box
35. Memoise the log viewer's lines
36. Stop the clock tick at the job page
37. Parse a log line's ANSI once
38. Search the log's parsed lines
39. Defer the log search behind typing
40. Let the browser skip off-screen log lines
41. Read a job log that has steps once
42. Ask again for a log that is not there yet (the hook)
43. Ask again for a log that is not there yet (the job page)
44. Strip DEL and the C1 controls from log lines
45. Keep the raw log's object URL for 40 seconds
46. Show the daemon's own text for a run or job-log 404
47. Test a breadcrumb with a query string through the real router
48. Error text by content type, and empty successful bodies
49. Request timeouts
50. A watchdog on the change stream
51. The missed flag of the change stream
52. One refetch when a tab is shown again
53. Runner steps and containers stay when a poll fails
54. Registrations stay when a read fails
55. The repository picker keeps its list on an error
56. The runner panel polls only the open tab
57. No cached repository list between two dialogs
58. One clock for every useNow caller
59. One classifier for job results
60. Result icons use the classifier
61. The run pill uses the classifier
62. Result words use the classifier
63. The daemon stops counting neutral results as failed
64. Pin how the charts count and name the three results
65. The lanes chart knows the three results
66. Encode the segments of GitHub links
67. One https check for run links
68. The run page links to GitHub only over https
69. History links a job to GitHub only over https
70. The runner panel links to a run only over https
71. A modifier click on a Dashboard repository goes to the browser
72. A modifier click on a live run in the lanes chart goes to the browser
73. The History table keeps its rows when a poll fails
74. The History table keeps its rows when its window slides
75. A job with a zero start time has no duration
76. The History chart's total counts unknown results
77. A token past its expiry reads Expired
78. The token page says the check covers every repository
79. Actions filters by the address before the config has loaded
80. `lib/format.ts` gains `count` and loses `clock`
81. The stat cards and the token page share `count`
82. Delete `statusWord` and write the History cap from its constant
83. No test has less time than the suite's default
84. Whole-tree verification

---

### Task 1: Vitest under load

**Files:**
- Modify: `web/vite.config.ts`

**Interfaces:**
- Consumes: nothing
- Produces: a test timeout of 15 seconds and a worker cap of half the CPUs for every later `vitest` run that does not pass its own `--testTimeout` or `--maxWorkers`

**Items:** 109, 225
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 0 - spec 0 - coupling 0 - risk 1 = 1

Vitest gives each test 5 seconds and starts one worker per CPU but one (11 on the 12-CPU dev host). When the host is busy the workers starve each other and 3 to 15 tests of the web suite time out; all of them pass on a rerun. The task sets the timeout to 15 seconds and caps the workers at half the CPUs, in the config, so `npm test` and CI get both without flags.

There is no behaviour to test red-first here. The check is a one-line script that prints what Vitest resolves from the config file, run before and after the edit.

- [ ] **Step 1: Print what the config resolves to today**

Run: `cd web && node --input-type=module -e "import { resolveConfig } from 'vitest/node'; const { vitestConfig: c } = await resolveConfig(); console.log('testTimeout', c.testTimeout, 'maxWorkers', c.maxWorkers)"`
Expected: `testTimeout 5000 maxWorkers undefined`

- [ ] **Step 2: Write the implementation**

In `web/vite.config.ts`, replace:

```ts
    include: ["src/**/*.test.{ts,tsx}"],
```

with:

```ts
    include: ["src/**/*.test.{ts,tsx}"],
    // On a busy host a worker per CPU starves the tests, and they run past
    // Vitest's 5-second default.
    testTimeout: 15_000,
    maxWorkers: "50%",
```

- [ ] **Step 3: Print what the config resolves to now**

Run: `cd web && node --input-type=module -e "import { resolveConfig } from 'vitest/node'; const { vitestConfig: c } = await resolveConfig(); console.log('testTimeout', c.testTimeout, 'maxWorkers', c.maxWorkers)"`
Expected: `testTimeout 15000 maxWorkers 6` on the 12-CPU dev host. The second number is half of what `nproc` prints, rounded, and at least 1. If the shell has `VITEST_MAX_WORKERS` set, Vitest prints that number instead: unset it and run again.

- [ ] **Step 4: Run one small test file under the new config**

Run: `cd web && npx vitest run src/lib/format.test.ts`
Expected: PASS (`Tests  20 passed (20)`)

- [ ] **Step 5: Commit**

```bash
git add web/vite.config.ts
git commit -m "test(web): give tests 15 s and half the CPUs"
```

### Task 2: Type-check vite.config.ts

**Files:**
- Modify: `web/tsconfig.json`

**Interfaces:**
- Consumes: nothing
- Produces: `web/vite.config.ts` is covered by `npm run typecheck` (`tsc --noEmit`), so a later task that edits the config must leave it type-clean

**Items:** 100
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 0 - spec 0 - coupling 0 - risk 0 = 0

`web/tsconfig.json` includes only `src`, so `tsc --noEmit` never reads `web/vite.config.ts`: a misspelt Vitest option there is silently ignored. The task adds the file to `include`. The config as it stands is type-clean, so nothing else needs changing.

There is no unit test for this. The check is the list of files `tsc` reads, before and after.

- [ ] **Step 1: Show that tsc does not read the config today**

Run: `cd web && npx tsc --noEmit --listFilesOnly | grep -c 'vite\.config\.ts$'`
Expected: `0` (and `grep` exits with status 1)

- [ ] **Step 2: Write the implementation**

In `web/tsconfig.json`, replace:

```json
  "include": ["src"]
```

with:

```json
  "include": ["src", "vite.config.ts"]
```

- [ ] **Step 3: Show that tsc reads the config now**

Run: `cd web && npx tsc --noEmit --listFilesOnly | grep -c 'vite\.config\.ts$'`
Expected: `1`

- [ ] **Step 4: Type-check**

Run: `cd web && npx tsc --noEmit`
Expected: no output, exit status 0. If it reports an error in `vite.config.ts`, fix the config, not the `include`.

- [ ] **Step 5: Commit**

```bash
git add web/tsconfig.json
git commit -m "chore(web): type-check vite.config.ts"
```

### Task 3: A Europe/Berlin time-zone suite

**Files:**
- Modify: `web/vite.config.ts`, `web/package.json` (the `scripts` block only)
- Create: `web/src/test/zone.tz.test.ts`

**Interfaces:**
- Consumes: nothing
- Produces: the npm script `test:tz`; the file-name rule `src/**/*.tz.test.{ts,tsx}` for tests that need local time; the environment variable `GHR_TEST_TZ`

**Items:** 100
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

Every web test runs with `TZ=UTC`, so code that reads local time is never tested in a zone with daylight saving. The suite stays in UTC. This task adds a second, separate run: test files named `*.tz.test.ts` (or `.tsx`) run under `Europe/Berlin`, started by `npm run test:tz`.

The switch is one environment variable, `GHR_TEST_TZ`, read in `web/vite.config.ts`. Without it, Vitest runs everything except the `*.tz.test.*` files, in UTC, exactly as today. With it, Vitest runs only the `*.tz.test.*` files, in the zone the variable names. So `npm test` never runs a time-zone test and `npm run test:tz` runs nothing else.

`package-lock.json` must not change: no dependency is added.

- [ ] **Step 1: Write the failing test**

Create `web/src/test/zone.tz.test.ts`:

```ts
import { describe, expect, it } from "vitest"

describe("the time-zone suite", () => {
  it("runs in Europe/Berlin, a zone with daylight saving", () => {
    expect(Intl.DateTimeFormat().resolvedOptions().timeZone).toBe("Europe/Berlin")
    expect(new Date("2026-01-15T12:00:00Z").getHours()).toBe(13)
    expect(new Date("2026-07-15T12:00:00Z").getHours()).toBe(14)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/test/zone.tz.test.ts`
Expected: FAIL with `AssertionError: expected 'UTC' to be 'Europe/Berlin' // Object.is equality`

- [ ] **Step 3: Write the implementation**

In `web/vite.config.ts`, replace:

```ts
import { defineConfig } from "vitest/config"
```

with:

```ts
import { configDefaults, defineConfig } from "vitest/config"
```

Replace:

```ts
const target = process.env.GHR_DEV_URL ?? "http://localhost:8080"
```

with:

```ts
const target = process.env.GHR_DEV_URL ?? "http://localhost:8080"

// Tests that need local time are named *.tz.test.ts. They run only when
// GHR_TEST_TZ names their zone, and then nothing else runs.
const zone = process.env.GHR_TEST_TZ
const zoneTests = "src/**/*.tz.test.{ts,tsx}"
```

Replace:

```ts
    env: { TZ: "UTC" },
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
```

with:

```ts
    env: { TZ: zone ?? "UTC" },
    setupFiles: ["./src/test/setup.ts"],
    include: [zone ? zoneTests : "src/**/*.test.{ts,tsx}"],
    exclude: zone ? configDefaults.exclude : [...configDefaults.exclude, zoneTests],
```

In `web/package.json`, replace:

```json
    "test": "vitest run"
```

with:

```json
    "test": "vitest run",
    "test:tz": "GHR_TEST_TZ=Europe/Berlin vitest run"
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd web && npm run test:tz`
Expected: PASS (`Tests  1 passed (1)`)

- [ ] **Step 5: Verify the two runs do not overlap**

Run: `cd web && npx vitest list --filesOnly | grep -c '\.tz\.test\.'`
Expected: `0` (the default run lists no time-zone file)

Run: `cd web && GHR_TEST_TZ=Europe/Berlin npx vitest list --filesOnly`
Expected: exactly one line, `src/test/zone.tz.test.ts`

Run: `cd web && npx vitest run src/test/zone.tz.test.ts`
Expected: `No test files found, exiting with code 1` (a time-zone file cannot be run in UTC by mistake)

- [ ] **Step 6: Type-check**

Run: `cd web && npx tsc --noEmit`
Expected: no output, exit status 0

- [ ] **Step 7: Commit**

```bash
git add web/vite.config.ts web/package.json web/src/test/zone.tz.test.ts
git commit -m "test(web): add a Europe/Berlin time-zone suite"
```

### Task 4: Run the time-zone tests in CI

**Files:**
- Modify: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: the npm script `test:tz` from the task "A Europe/Berlin time-zone suite"
- Produces: nothing

**Items:** 100
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 0 - spec 0 - coupling 1 - risk 0 = 1

CI runs `npm test`, which leaves the time-zone tests out. The task adds one step that runs them, directly after `npm test`. Nothing else in the workflow changes.

A workflow cannot be run from here, so there is no failing test. The check is that the step is where it should be and that the command it runs passes locally.

- [ ] **Step 1: Write the implementation**

In `.github/workflows/ci.yml`, replace:

```yaml
      - run: npm test
        working-directory: web
```

with:

```yaml
      - run: npm test
        working-directory: web
      - run: npm run test:tz
        working-directory: web
```

- [ ] **Step 2: Verify the step and its command**

Run: `grep -n -B2 -A1 'npm run test:tz' .github/workflows/ci.yml`
Expected: four lines, the new step directly under `npm test`:

```
39-      - run: npm test
40-        working-directory: web
41:      - run: npm run test:tz
42-        working-directory: web
```

Run: `cd web && npm run test:tz`
Expected: PASS (`Test Files  1 passed (1)` when this task directly follows the task that adds the script; more files once later tasks add time-zone tests)

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: run the web time-zone tests"
```

### Task 5: A 23-hour day reads as a day

**Files:**
- Modify: `web/src/lib/history.ts` (`pickText` only)
- Create: `web/src/lib/history.tz.test.ts`
- Test: `web/src/lib/history.test.ts` (the `pickText` tests only)

**Interfaces:**
- Consumes: the npm script `test:tz` and the `*.tz.test.ts` file-name rule from the task "A Europe/Berlin time-zone suite"
- Produces: nothing

**Items:** 99
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

On the History page, clicking a chart bar shows a line such as `Showing Oct 3` for a day bucket or `Showing 13:00 to 14:00, Oct 3` for a shorter one. `pickText` decides which by asking whether the bucket is at least 24 hours long. The daemon aligns day buckets to local midnight, so on the day the clocks go forward a day is 23 hours long and the page reads `Showing 00:00 to 00:00, Mar 29`.

The rule becomes: a bucket of 12 hours or more is a day bucket. The daemon sends three bucket sizes, one hour, six hours and a day; across a daylight-saving change a six-hour bucket runs 5 or 7 hours and a day 23 or 25, so 12 hours separates them.

Change only `pickText` in `web/src/lib/history.ts`, and in `web/src/lib/history.test.ts` only the `pickText` tests.

- [ ] **Step 1: Write the failing tests**

Create `web/src/lib/history.tz.test.ts`. It runs under `Europe/Berlin`; the bucket times are written the way the daemon sends them, with the zone's offset.

```ts
import { describe, expect, it } from "vitest"
import { pickText } from "./history"

// The daemon aligns six-hour and day buckets to local wall time, so in
// Europe/Berlin they change length on 2026-03-29 and 2026-10-25.
describe("pickText across a daylight-saving change", () => {
  it.each([
    ["a 23-hour day", "2026-03-29T00:00:00+01:00", "2026-03-30T00:00:00+02:00", "Showing Mar 29"],
    ["a 25-hour day", "2026-10-25T00:00:00+02:00", "2026-10-26T00:00:00+01:00", "Showing Oct 25"],
    ["a 5-hour six-hour bucket", "2026-03-29T00:00:00+01:00", "2026-03-29T06:00:00+02:00", "Showing 00:00 to 06:00, Mar 29"],
    ["a 7-hour six-hour bucket", "2026-10-25T00:00:00+02:00", "2026-10-25T06:00:00+01:00", "Showing 00:00 to 06:00, Oct 25"],
  ])("reads %s", (_name, start, end, text) => {
    expect(pickText({ start, end })).toBe(text)
  })
})
```

In `web/src/lib/history.test.ts`, in `describe("inBucket and pickText", ...)`, replace:

```ts
  it("describes the picked span", () => {
    expect(pickText({ start: "2026-10-03T13:00:00Z", end: "2026-10-03T14:00:00Z" })).toBe("Showing 13:00 to 14:00, Oct 3")
    expect(pickText({ start: "2026-10-03T00:00:00Z", end: "2026-10-04T00:00:00Z" })).toBe("Showing Oct 3")
  })
```

with:

```ts
  it("describes the picked span", () => {
    expect(pickText({ start: "2026-10-03T13:00:00Z", end: "2026-10-03T14:00:00Z" })).toBe("Showing 13:00 to 14:00, Oct 3")
    expect(pickText({ start: "2026-10-03T00:00:00Z", end: "2026-10-04T00:00:00Z" })).toBe("Showing Oct 3")
  })

  it("reads a bucket of 12 hours or more as a day", () => {
    expect(pickText({ start: "2026-10-03T00:00:00Z", end: "2026-10-03T12:00:00Z" })).toBe("Showing Oct 3")
    expect(pickText({ start: "2026-10-03T00:00:00Z", end: "2026-10-03T11:59:00Z" })).toBe("Showing 00:00 to 11:59, Oct 3")
  })
```

The existing test `describes the picked span` stays as it is: both of its buckets read the same under the new rule.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npm run test:tz -- src/lib/history.tz.test.ts`
Expected: FAIL, one of four tests (`reads a 23-hour day`), with `AssertionError: expected 'Showing 00:00 to 00:00, Mar 29' to be 'Showing Mar 29' // Object.is equality`

Run: `cd web && npx vitest run src/lib/history.test.ts`
Expected: FAIL, one test (`reads a bucket of 12 hours or more as a day`), with `AssertionError: expected 'Showing 00:00 to 12:00, Oct 3' to be 'Showing Oct 3' // Object.is equality`

- [ ] **Step 3: Write the implementation**

In `web/src/lib/history.ts`, in `pickText`, replace:

```ts
  if (end.getTime() - start.getTime() >= 24 * 3_600_000) return `Showing ${monthDay(start)}`
```

with:

```ts
  // A day bucket runs 23 or 25 hours across a daylight-saving change; the
  // next size down, six hours, runs 7 at most.
  if (end.getTime() - start.getTime() >= 12 * 3_600_000) return `Showing ${monthDay(start)}`
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npm run test:tz -- src/lib/history.tz.test.ts`
Expected: PASS (`Tests  4 passed (4)`)

Run: `cd web && npx vitest run src/lib/history.test.ts`
Expected: PASS (`Tests  15 passed (15)`; the count is higher if an earlier task added tests to this file)

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/history.ts web/src/lib/history.test.ts web/src/lib/history.tz.test.ts
git commit -m "fix(web): read a 23-hour history bucket as a day"
```

### Task 6: Move the palette out of lib

**Files:**
- Modify (move): `web/src/lib/palette.ts` to `web/src/test/palette.ts`
- Test (move): `web/src/lib/palette.test.ts` to `web/src/test/palette.test.ts`

**Interfaces:**
- Consumes: nothing
- Produces: the paths `web/src/test/palette.ts` and `web/src/test/palette.test.ts`

**Items:** 101
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 0 = 1

`web/src/lib/palette.ts` holds the Forge palette as hex colours with a few colour helpers. No page or component imports it: its only user is `web/src/lib/palette.test.ts`, which checks `web/src/styles/ghr-theme.css` against it. So it is test support sitting among the application's modules. The task moves both files, unchanged, to `web/src/test/`, the directory for test-only code.

Neither file is edited. The test imports `./palette` and reads `../styles/ghr-theme.css`; both paths stay right because the two files move together and `src/test` is as deep as `src/lib`.

Nothing changes in behaviour, so there is no failing test first.

- [ ] **Step 1: Move the two files**

```bash
git mv web/src/lib/palette.ts web/src/test/palette.ts
git mv web/src/lib/palette.test.ts web/src/test/palette.test.ts
```

- [ ] **Step 2: Run the moved test**

Run: `cd web && npx vitest run src/test/palette.test.ts`
Expected: PASS (`Tests  11 passed (11)`)

- [ ] **Step 3: Verify nothing is left behind and nothing else used it**

Run: `ls web/src/lib | grep -c palette`
Expected: `0`

Run: `cd web && npx tsc --noEmit`
Expected: no output, exit status 0 (an import of the old path anywhere would be an error here)

Run: `cd web && npx eslint src --max-warnings 0`
Expected: no output, exit status 0

- [ ] **Step 4: Commit**

`git mv` has already staged both moves.

```bash
git commit -m "refactor(web): move the palette under src/test"
```

### Task 7: Declare the log colours once

**Files:**
- Modify: `web/src/styles/ghr-theme.css`
- Test: `web/src/styles/theme.test.ts`

**Interfaces:**
- Consumes: nothing
- Produces: the 27 `--log-*` tokens are declared in the first `:root { ... }` block of `web/src/styles/ghr-theme.css` and nowhere else

**Items:** 101
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The log viewer is dark in both colour modes, so its 27 colour tokens (`--log-bg` to `--log-ansi-bright-white`) have the same values in light and dark. `web/src/styles/ghr-theme.css` nevertheless writes the whole list twice, once in `:root[data-mode="dark"]` and once in `:root[data-mode="light"]`, and a change to one copy can miss the other. The task writes the list once, in the plain `:root` block near the top of the file, and removes both copies. No colour changes.

The tokens `--running`, `--top-bar` and `--tab-line` that follow the list in each mode block differ between the modes and stay where they are.

- [ ] **Step 1: Write the failing test**

In `web/src/styles/theme.test.ts`, in `describe("log palette", ...)`, replace:

```ts
    expect(contrast(t["--log-current"], t["--log-bg"])).toBeGreaterThanOrEqual(3)
  })
})
```

with:

```ts
    expect(contrast(t["--log-current"], t["--log-bg"])).toBeGreaterThanOrEqual(3)
  })

  it("declares each log colour once, in :root", () => {
    const declared = [...css.matchAll(/(--log-[\w-]+):/g)].map((m) => m[1])
    expect(declared).toHaveLength(new Set(declared).size)
    expect(Object.keys(block(":root")).filter((k) => k.startsWith("--log-"))).toEqual(declared)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/styles/theme.test.ts`
Expected: FAIL, one test, with `AssertionError: expected [ '--log-bg', '--log-fg', …(52) ] to have a length of 27 but got 54`

- [ ] **Step 3: Write the implementation**

In `web/src/styles/ghr-theme.css`, in the first `:root {` block (the one that starts with `--shadow-card`), replace:

```css
  --sf-hue-3: transparent !important;
}
```

with:

```css
  --sf-hue-3: transparent !important;
  /* The log viewer is dark in both modes, like GitHub's. */
  --log-bg: 213.3 33.3% 5.3%;
  --log-fg: 212.0 24.6% 88.0%;
  --log-muted: 214.3 12.4% 58.0%;
  --log-command: 188.3 62.7% 56.9%;
  --log-error-bg: 5.4 50.0% 14.0%;
  --log-error-fg: 5.4 81.9% 72.0%;
  --log-warning-bg: 37.7 50.0% 12.0%;
  --log-warning-fg: 37.7 71.6% 62.0%;
  --log-match-bg: 45.0 70.0% 24.0%;
  --log-current: 45.0 95.0% 60.0%;
  --log-target-bg: 213.0 28.6% 13.7%;
  --log-ansi-black: 212.0 9.0% 57.0%;
  --log-ansi-red: 3.0 100.0% 72.0%;
  --log-ansi-green: 128.0 49.0% 52.0%;
  --log-ansi-yellow: 40.0 72.0% 52.0%;
  --log-ansi-blue: 212.0 100.0% 67.0%;
  --log-ansi-magenta: 266.0 100.0% 77.0%;
  --log-ansi-cyan: 184.0 60.0% 52.0%;
  --log-ansi-white: 212.0 13.0% 73.0%;
  --log-ansi-bright-black: 212.0 9.0% 62.0%;
  --log-ansi-bright-red: 3.0 100.0% 80.0%;
  --log-ansi-bright-green: 128.0 60.0% 62.0%;
  --log-ansi-bright-yellow: 44.0 87.0% 60.0%;
  --log-ansi-bright-blue: 212.0 100.0% 78.0%;
  --log-ansi-bright-magenta: 270.0 100.0% 85.0%;
  --log-ansi-bright-cyan: 184.0 58.0% 62.0%;
  --log-ansi-bright-white: 210.0 60.0% 98.0%;
}
```

Then delete the same 28 lines (the comment line and the 27 `--log-*` lines, exactly as written above) from each of the two mode blocks:

- in `:root[data-mode="dark"]` they sit between `--chart-5: hsl(213.8 7.8% 60.0%) !important;` and `--running: 40.6 72.1% 47.8%;`, so afterwards the block ends:

```css
  --chart-5: hsl(213.8 7.8% 60.0%) !important;
  --running: 40.6 72.1% 47.8%;
  --top-bar: 217.5 80.0% 2.0%;
  --tab-line: 11.2 90.1% 68.4%;
}
```

- in `:root[data-mode="light"]` they sit between `--chart-5: hsl(211.4 10.6% 39.0%) !important;` and `--running: 40.1 100.0% 30.2%;`, so afterwards the block ends:

```css
  --chart-5: hsl(211.4 10.6% 39.0%) !important;
  --running: 40.1 100.0% 30.2%;
  --top-bar: 0.0 0.0% 100.0%;
  --tab-line: 10.9 97.2% 72.2%;
}
```

Leave every other line of the file alone, among them the `.ghr-log*` rules lower down that read the tokens with `var(--log-...)`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `grep -c -e '^  --log-' web/src/styles/ghr-theme.css`
Expected: `27` (it was 54)

Run: `cd web && npx vitest run src/styles/theme.test.ts src/test/palette.test.ts`
Expected: PASS (`Tests  27 passed (27)`: 16 in the theme test and 11 in the palette test, which reads the same stylesheet). If the palette has not been moved yet, its test is at `src/lib/palette.test.ts`.

- [ ] **Step 5: Commit**

```bash
git add web/src/styles/ghr-theme.css web/src/styles/theme.test.ts
git commit -m "refactor(web): declare the log colours once"
```

### Task 8: Capacity bar at the largest global max

**Files:**
- Test: `web/src/components/capacity-bar.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 108
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 0 - spec 0 - coupling 0 - risk 0 = 0

The capacity bar draws one segment per runner and per free slot, up to `global_max`. The daemon caps `global_max` at 99, and no test covers that largest value. This task adds two: a row in the existing table of `capacity()` cases, and one test of the rendered bar.

The task adds tests only and changes no source file. Both tests pass at once, because the code already handles 99. What they pin: at `global_max` 99 with one busy and one warm runner, `capacity()` gives 99 segments (busy, warm, then 97 free), the text `1 of 99 busy` and the label `1 of 99 busy, 1 warm, 97 free`; the rendered bar has 99 children, 97 of them free, and no segment carries the `ml-[2px]` gap that marks runners above the max.

If either test fails, do not change `web/src/components/capacity-bar.tsx` or `web/src/lib/capacity.ts`: stop and report the failure.

- [ ] **Step 1: Write the tests**

In `web/src/components/capacity-bar.test.tsx`, in the `it.each` table of `describe("capacity", ...)`, replace:

```tsx
    [
      "all mode",
      status({ mode: "all", instances: [inst("busy", "a"), inst("idle", "b")] }),
      ["busy", "warm"],
      "1 busy",
      "1 busy, 1 warm",
    ],
  ])("%s", (_name, st, segments, text, label) => {
```

with:

```tsx
    [
      "all mode",
      status({ mode: "all", instances: [inst("busy", "a"), inst("idle", "b")] }),
      ["busy", "warm"],
      "1 busy",
      "1 busy, 1 warm",
    ],
    [
      "the largest max",
      status({ global_max: 99, instances: [inst("busy", "a"), inst("idle", "b")] }),
      ["busy", "warm", ...Array<string>(97).fill("free")],
      "1 of 99 busy",
      "1 of 99 busy, 1 warm, 97 free",
    ],
  ])("%s", (_name, st, segments, text, label) => {
```

In `describe("CapacitySegments", ...)`, insert this test directly above `it("draws free slots in the sunk colour", ...)`, with one empty line between the two tests:

```tsx
  it("draws all 99 segments at the largest max, none set apart", () => {
    const largest = status({ global_max: 99, instances: [inst("busy", "a"), inst("idle", "b")] })
    render(<CapacitySegments status={largest} />)
    const bar = screen.getByRole("img", { name: "1 of 99 busy, 1 warm, 97 free" })
    expect(bar.children).toHaveLength(99)
    expect(bar.querySelectorAll('[data-kind="free"]')).toHaveLength(97)
    expect([...bar.children].filter((c) => c.classList.contains("ml-[2px]"))).toEqual([])
  })
```

- [ ] **Step 2: Run the tests**

Run: `cd web && npx vitest run src/components/capacity-bar.test.tsx`
Expected: PASS (`Tests  10 passed (10)`), the two new ones among them: `capacity > the largest max` and `CapacitySegments > draws all 99 segments at the largest max, none set apart`

- [ ] **Step 3: Lint and type-check**

Run: `cd web && npx eslint src/components/capacity-bar.test.tsx --max-warnings 0`
Expected: no output, exit status 0

Run: `cd web && npx tsc --noEmit`
Expected: no output, exit status 0

- [ ] **Step 4: Commit**

```bash
git add web/src/components/capacity-bar.test.tsx
git commit -m "test(web): cover the capacity bar at max 99"
```

### Task 9: The route guard keeps the auth answer for five seconds

**Files:**
- Create: `web/src/api/auth.ts`
- Modify: `web/src/router.tsx`
- Test: `web/src/router.test.tsx`

**Interfaces:**
- Consumes: `keys.auth`, `keys.setup` and `POLL_SLOW` from `web/src/api/hooks.ts`, `api.authState` and `api.setupState` from `web/src/api/client.ts` (all exist; none is edited)
- Produces: `authQuery`, exported from `web/src/api/auth.ts`: the one definition of the `/auth/state` query (key `keys.auth`, fresh for `POLL_SLOW`, 5 seconds). The login page uses it in the next two tasks.

**Items:** 75
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

The route guards in `web/src/router.tsx` ask the daemon `/auth/state` and then `/api/setup` with `staleTime: 0`. The router runs them on every history change, also one that changes only the search or the hash, so each click costs two requests. When the auth request fails, for example while the daemon restarts, the guard throws and the route error page replaces the whole app until Retry is pressed.

This task gives both answers a freshness of 5 seconds and lets a navigation through when the auth request fails but the cache still holds an answer that says the session is authenticated; the Shell's status poll and its Reconnecting alert then report the daemon. A failed request with no such answer (a cold load while the daemon is down) still shows the error page with Retry. An ended session does not wait for the 5 seconds: any API call that answers `401` runs the handler in `web/src/app.tsx`, which clears the query cache and opens `/login`, where the guard then has to ask again. The third test below pins exactly that, so a cached "authenticated" can never bounce the visitor between `/login` and the app.

The tests fake only the clock (`vi.useFakeTimers({ toFake: ["Date"] })`): the query library reads freshness from `Date.now()`, and timers stay real so `findBy` queries and clicks keep working. No test waits.

- [ ] **Step 1: Write the failing tests**

In `web/src/router.test.tsx`, replace the first three import lines:

```tsx
import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi } from "@/test/api"
```

with:

```tsx
import { act, screen, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api } from "@/api/client"
import { json, mockApi, type Call } from "@/test/api"
```

Replace:

```tsx
function renderAt(path: string) {
  return renderApp(path).router
}

describe("routes", () => {
```

with:

```tsx
function renderAt(path: string) {
  return renderApp(path).router
}

function asked(calls: Call[], path: string): number {
  return calls.filter((c) => c.path === path).length
}

describe("routes", () => {
  afterEach(() => {
    vi.useRealTimers()
  })

```

The existing test `offers a retry when the auth check fails while moving between pages` asserts the behaviour this task removes. Replace the whole test:

```tsx
  it("offers a retry when the auth check fails while moving between pages", async () => {
    let down = false
    mockApi(
      authedRoutes({
        "GET /auth/state": () => (down ? json({ error: "connection refused" }, 502) : authed["GET /auth/state"]),
        "GET /api/history": [],
      }),
    )
    const { user } = renderApp("/runners")
    expect(await screen.findByRole("heading", { name: "Runners" })).toBeInTheDocument()
    down = true
    await user.click(screen.getByRole("link", { name: "History" }))
    expect(await screen.findByText("cannot load the page: connection refused")).toBeInTheDocument()
    down = false
    await user.click(screen.getByRole("button", { name: "Retry" }))
    expect(await screen.findByRole("heading", { name: "History" })).toBeInTheDocument()
  })
```

with:

```tsx
  it("keeps the page when the auth check fails and the cache holds a session", async () => {
    vi.useFakeTimers({ toFake: ["Date"] })
    let down = false
    const { calls } = mockApi(
      authedRoutes({
        "GET /auth/state": () => (down ? json({ error: "connection refused" }, 502) : authed["GET /auth/state"]),
        "GET /api/history": [],
      }),
    )
    const { user } = renderApp("/runners")
    expect(await screen.findByRole("heading", { name: "Runners" })).toBeInTheDocument()
    down = true
    vi.setSystemTime(Date.now() + 5000)
    await user.click(screen.getByRole("link", { name: "History" }))
    expect(await screen.findByRole("heading", { name: "History" })).toBeInTheDocument()
    expect(screen.queryByText(/cannot load the page/)).toBeNull()
    expect(asked(calls, "/auth/state")).toBe(2)
  })
```

The test above it, `offers a retry when the daemon cannot be asked who is logged in`, stays as it is: it is the cold load, which still shows the error page.

Insert these two tests directly above the line `  it("loads the app when the setup state cannot be read", async () => {`:

```tsx
  it("asks who is logged in and whether setup is done again only after five seconds", async () => {
    vi.useFakeTimers({ toFake: ["Date"] })
    const { calls } = mockApi(authedRoutes({ "GET /api/setup": setupState({}), "GET /api/history": [] }))
    const router = renderAt("/history")
    expect(await screen.findByRole("heading", { name: "History" })).toBeInTheDocument()
    await act(() => router.navigate({ to: "/history", search: { result: "failure" } }))
    expect(router.state.location.search).toEqual({ result: "failure" })
    expect(asked(calls, "/auth/state")).toBe(1)
    expect(asked(calls, "/api/setup")).toBe(1)
    vi.setSystemTime(Date.now() + 5000)
    await act(() => router.navigate({ to: "/history", search: {} }))
    expect(router.state.location.search).toEqual({})
    expect(asked(calls, "/auth/state")).toBe(2)
    expect(asked(calls, "/api/setup")).toBe(2)
  })

  it("shows the login form when a session ends while the auth answer is fresh", async () => {
    let authenticated = true
    mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated }),
        "GET /api/status": () => (authenticated ? fixtures.status : json({ error: "not logged in" }, 401)),
      }),
    )
    const router = renderAt("/")
    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument()
    authenticated = false
    await act(async () => {
      await api.status().catch(() => undefined)
    })
    expect(await screen.findByRole("button", { name: "Log in" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/login")
  })

```

`setupState` is the helper already defined a few lines above that spot, and `fixtures` is already imported.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/router.test.tsx`
Expected: FAIL, `Tests  2 failed | 27 passed (29)`. `keeps the page when the auth check fails and the cache holds a session` fails with `Unable to find role="heading" and name "History"` (the page shows `cannot load the page: connection refused`), and `asks who is logged in and whether setup is done again only after five seconds` fails with `AssertionError: expected 2 to be 1 // Object.is equality`.

`shows the login form when a session ends while the auth answer is fresh` passes already and must keep passing: it guards the cache clear in `web/src/app.tsx`, which this task starts to depend on.

- [ ] **Step 3: Write the implementation**

Create `web/src/api/auth.ts`:

```ts
import { queryOptions } from "@tanstack/react-query"
import { api } from "./client"
import { keys, POLL_SLOW } from "./hooks"

// The route guards run on every history change, so the answer stays fresh for
// one slow poll. An ended session does not wait for it: the 401 of any API
// call clears the cache and opens /login.
export const authQuery = queryOptions({
  queryKey: keys.auth,
  queryFn: ({ signal }) => api.authState(signal),
  staleTime: POLL_SLOW,
})
```

In `web/src/router.tsx`, replace:

```ts
import { api } from "./api/client"
import { keys } from "./api/hooks"
import type { SetupState } from "./api/types"
```

with:

```ts
import { authQuery } from "./api/auth"
import { api } from "./api/client"
import { keys, POLL_SLOW } from "./api/hooks"
import type { AuthState, SetupState } from "./api/types"
```

Replace:

```ts
// staleTime 0: every navigation into or out of the gated pages asks the
// daemon again, so a logout or an expired session is seen at once.
function authState(queryClient: QueryClient) {
  return queryClient.fetchQuery({ queryKey: keys.auth, queryFn: ({ signal }) => api.authState(signal), staleTime: 0 })
}
```

with:

```ts
// A failed read does not undo a session the cache still holds: the navigation
// goes on and the Shell's status poll reports the daemon. With no such answer
// the read's error reaches the route error page.
async function authState(queryClient: QueryClient): Promise<AuthState> {
  try {
    return await queryClient.fetchQuery(authQuery)
  } catch (err) {
    const cached = queryClient.getQueryData(authQuery.queryKey)
    if (cached?.authenticated) return cached
    throw err
  }
}
```

Replace:

```ts
// staleTime 0, like authState. A failed read does not redirect: the page
// loads, and the Shell's status poll or the wizard reports the daemon.
async function setupState(queryClient: QueryClient): Promise<SetupState | undefined> {
  try {
    return await queryClient.fetchQuery({ queryKey: keys.setup, queryFn: ({ signal }) => api.setupState(signal), staleTime: 0 })
```

with:

```ts
// Fresh as long as the auth answer. A failed read does not redirect: the page
// loads, and the Shell's status poll or the wizard reports the daemon.
async function setupState(queryClient: QueryClient): Promise<SetupState | undefined> {
  try {
    return await queryClient.fetchQuery({ queryKey: keys.setup, queryFn: ({ signal }) => api.setupState(signal), staleTime: POLL_SLOW })
```

Nothing else in `router.tsx` changes: `requireSession`, the `/login` guard and the setup guard keep calling `authState` and `setupState`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/router.test.tsx src/app.test.tsx src/pages/setup.test.tsx`
Expected: PASS, all three files; `src/router.test.tsx` reports 29 tests. `app.test.tsx` and `setup.test.tsx` are included because they go through the same guards (the `401` handler and the wizard's way out).

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npm run lint` and then `cd web && npm run typecheck`
Expected: both end without an error or a warning

- [ ] **Step 6: Commit**

```bash
git add web/src/api/auth.ts web/src/router.tsx web/src/router.test.tsx
git commit -m "fix(web): cache the route guard's auth answer"
```

### Task 10: The login page shares the guard's auth query

**Files:**
- Modify: `web/src/pages/login.tsx`
- Test: `web/src/pages/login.test.tsx`

**Interfaces:**
- Consumes: `authQuery` from `web/src/api/auth.ts` (the previous task)
- Produces: nothing

**Items:** 106
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

Opening `/login` and logging in asks `/auth/state` more often than needed: the `/login` guard asks, the login page asks again the moment it mounts because its own copy of the query has no freshness, the page asks a third time after the login succeeds, and before the previous task the guard of the page it then opens asked a fourth time. With the guard and the page on one query definition the page's mount reuses the guard's fresh answer, and a login makes two calls: one when `/login` opens and one after the login succeeds.

The page keeps invalidating the query after a successful login. That refetch is what makes the guard of the next page read "authenticated" and not the cached "not authenticated".

This discharges only the first part of register row 106 (the number of `/auth/state` calls); its other parts belong to other tasks.

- [ ] **Step 1: Write the failing test**

In `web/src/pages/login.test.tsx`, replace the whole test `logs in`:

```tsx
  it("logs in", async () => {
    let done = false
    mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated: done }),
        "POST /auth/login": () => {
          done = true
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/login")
    await user.type(await screen.findByLabelText("Password"), "correct horse battery")
    await user.click(screen.getByRole("button", { name: "Log in" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/"))
  })
```

with:

```tsx
  it("logs in, asking who is logged in twice", async () => {
    let done = false
    const { calls } = mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated: done }),
        "POST /auth/login": () => {
          done = true
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/login")
    await user.type(await screen.findByLabelText("Password"), "correct horse battery")
    await user.click(screen.getByRole("button", { name: "Log in" }))
    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/")
    expect(calls.filter((c) => c.path === "/auth/state")).toHaveLength(2)
  })
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/pages/login.test.tsx`
Expected: FAIL, `Tests  1 failed | 7 passed (8)`, with `AssertionError: expected [ { method: 'GET', …(4) }, …(2) ] to have a length of 2 but got 3`

- [ ] **Step 3: Write the implementation**

In `web/src/pages/login.tsx`, replace:

```ts
import { ApiError, api } from "@/api/client"
import { keys } from "@/api/hooks"
```

with:

```ts
import { authQuery } from "@/api/auth"
import { ApiError, api } from "@/api/client"
```

Replace:

```ts
  const auth = useQuery({ queryKey: keys.auth, queryFn: ({ signal }) => api.authState(signal) })
```

with:

```ts
  const auth = useQuery(authQuery)
```

Replace:

```ts
      await queryClient.invalidateQueries({ queryKey: keys.auth })
```

with:

```ts
      await queryClient.invalidateQueries({ queryKey: authQuery.queryKey })
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/login.test.tsx`
Expected: PASS (`Tests  8 passed (8)`)

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npm run lint` and then `cd web && npm run typecheck`
Expected: both end without an error or a warning

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/login.tsx web/src/pages/login.test.tsx
git commit -m "fix(web): share the auth query with the login page"
```

### Task 11: A 409 switches the password form's mode

**Files:**
- Modify: `web/src/pages/login.tsx`
- Test: `web/src/pages/login.test.tsx`

**Interfaces:**
- Consumes: `authQuery` from `web/src/api/auth.ts`, already imported into `login.tsx` by the previous task
- Produces: nothing

**Items:** 88
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

The login page shows one of two forms, chosen by `setup_required` in the `/auth/state` answer: "Set a password" or "Log in". The daemon answers `409` when the form is in the wrong mode: `a web password is already set` from Set a password when someone set one in the meantime, and `no web password is set yet; set one first` from a login when none exists. Today the page shows that message and stays in the wrong mode, so every further attempt fails the same way.

On a `409` the page now invalidates the auth query. The refetch brings the daemon's real mode and the form changes to it; the daemon's message stays on screen so the visitor knows why.

- [ ] **Step 1: Write the failing test**

In `web/src/pages/login.test.tsx`, insert directly above the line `  it("says when to retry after too many failures", async () => {`:

```tsx
  it.each([
    [true, "Set password", "POST /auth/setup", "a web password is already set", "Log in"],
    [false, "Log in", "POST /auth/login", "no web password is set yet; set one first", "Set password"],
  ])("changes mode after a 409 (setup required: %s)", async (required, submit, route, error, next) => {
    let setup = required
    mockApi({
      "GET /auth/state": () => ({ setup_required: setup, authenticated: false }),
      [route]: () => {
        setup = !required
        return json({ error }, 409)
      },
    })
    const { user } = renderApp("/login")
    await user.type(await screen.findByLabelText("Password"), "correct horse battery")
    if (required) await user.type(screen.getByLabelText("Confirm password"), "correct horse battery")
    await user.click(screen.getByRole("button", { name: submit }))
    expect(await screen.findByRole("button", { name: next })).toBeInTheDocument()
    expect(screen.getByRole("alert")).toHaveTextContent(error)
  })

```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/pages/login.test.tsx`
Expected: FAIL, `Tests  2 failed | 8 passed (10)`, with `Unable to find role="button" and name "Log in"` for the first row and `Unable to find role="button" and name "Set password"` for the second

- [ ] **Step 3: Write the implementation**

In `web/src/pages/login.tsx`, in `submit`, replace:

```ts
    } catch (err) {
      setError(loginError(err))
    } finally {
```

with:

```ts
    } catch (err) {
      // A 409 means the daemon is in the other mode than this form shows.
      if (err instanceof ApiError && err.status === 409) await queryClient.invalidateQueries({ queryKey: authQuery.queryKey })
      setError(loginError(err))
    } finally {
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/login.test.tsx`
Expected: PASS (`Tests  10 passed (10)`)

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npm run lint` and then `cd web && npm run typecheck`
Expected: both end without an error or a warning

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/login.tsx web/src/pages/login.test.tsx
git commit -m "fix(web): switch the password form's mode on a 409"
```

### Task 12: An ended session on the Account form goes to /login

**Files:**
- Modify: `web/src/components/account-section.tsx`
- Create: `web/src/components/account-section.test.tsx`

**Interfaces:**
- Consumes: `api.authState` and `notifyUnauthorized` from `web/src/api/client.ts` (both exist; the file is not edited)
- Produces: nothing

**Items:** 89
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The daemon's change-password route answers `401` in two cases: the current password is wrong, or the session has ended. The Account form shows "The current password is wrong" for both, so a visitor whose session ran out keeps retyping a correct password. The API client does not help here: it sends the visitor to `/login` only for a `401` from a path under `/api/`, and this route is `/auth/password`.

On a `401` the form now reads `/auth/state`. When it says the session is not authenticated the form runs the app's unauthorized handler (`notifyUnauthorized`), which clears the query cache and opens `/login` with the way back to the Account page. Only a live session reads "The current password is wrong". When `/auth/state` itself cannot be read, the form shows that read's error, because it does not know which of the two cases it is.

The existing Account tests live in `web/src/pages/settings.test.tsx` and are not edited; they still pass, since their mock daemon reports a live session. The new cases go into a test file of the component's own.

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/account-section.test.tsx`:

```tsx
import { screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const live = { setup_required: false, authenticated: true }

async function submit(user: ReturnType<typeof renderApp>["user"]) {
  await user.type(await screen.findByLabelText("Current password"), "old password 1")
  await user.type(screen.getByLabelText("New password"), "new password 12")
  await user.type(screen.getByLabelText("Confirm new password"), "new password 12")
  await user.click(screen.getByRole("button", { name: "Change password" }))
}

describe("AccountSection", () => {
  it("opens /login when the 401 is an ended session", async () => {
    let ended = false
    mockApi(
      authedRoutes({
        "GET /api/token": fixtures.token,
        "GET /auth/state": () => ({ ...live, authenticated: !ended }),
        "POST /auth/password": () => {
          ended = true
          return json({ error: "not logged in" }, 401)
        },
      }),
    )
    const { user, router } = renderApp("/settings/account")
    await submit(user)
    expect(await screen.findByRole("button", { name: "Log in" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/login")
    expect(router.state.location.search).toEqual({ redirect: "/settings/account" })
  })

  it("shows the failed session check, not a wrong password, when /auth/state cannot be read", async () => {
    let down = false
    mockApi(
      authedRoutes({
        "GET /api/token": fixtures.token,
        "GET /auth/state": () => (down ? json({ error: "connection refused" }, 502) : live),
        "POST /auth/password": () => {
          down = true
          return json({ error: "not logged in" }, 401)
        },
      }),
    )
    const { user, router } = renderApp("/settings/account")
    await submit(user)
    expect(await screen.findByRole("alert")).toHaveTextContent("connection refused")
    expect(router.state.location.pathname).toBe("/settings/account")
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/components/account-section.test.tsx`
Expected: FAIL, `Tests  2 failed (2)`. The first test fails with `Unable to find role="button" and name "Log in"`; the second with `Expected element to have text content: connection refused` and `Received: The current password is wrong`.

- [ ] **Step 3: Write the implementation**

In `web/src/components/account-section.tsx`, replace:

```ts
import { ApiError, api } from "@/api/client"
```

with:

```ts
import { ApiError, api, notifyUnauthorized } from "@/api/client"
```

In `change`, replace:

```ts
    } catch (err) {
      setError(err instanceof ApiError && err.status === 401 ? "The current password is wrong" : errorText(err))
    } finally {
```

with:

```ts
    } catch (err) {
      if (!(err instanceof ApiError && err.status === 401)) return setError(errorText(err))
      // The daemon answers 401 for a wrong current password and for an ended
      // session alike; /auth/state tells them apart.
      try {
        if ((await api.authState()).authenticated) setError("The current password is wrong")
        else notifyUnauthorized()
      } catch (stateErr) {
        setError(errorText(stateErr))
      }
    } finally {
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/account-section.test.tsx src/pages/settings.test.tsx`
Expected: PASS, both files; `src/components/account-section.test.tsx` reports 2 tests. `settings.test.tsx` is included because it holds the form's other cases, among them `says when the current password is wrong`.

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npm run lint` and then `cd web && npm run typecheck`
Expected: both end without an error or a warning

- [ ] **Step 6: Commit**

```bash
git add web/src/components/account-section.tsx web/src/components/account-section.test.tsx
git commit -m "fix(web): send an ended session to login from Account"
```

### Task 13: Redirect targets with a control character, a space or a backslash

**Files:**
- Modify: `web/src/lib/redirect.ts`
- Create: `web/src/lib/redirect.test.ts`
- Test: `web/src/pages/login.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing. `safeRedirect` keeps its signature; it now answers `/` for more inputs.

**Items:** 98, 108
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 2 = 3

`safeRedirect` decides whether the `redirect` value in the `/login` address is a path on this site. It lets `/<tab>/host` through, which the router then refuses. Two paths use it and both go wrong today:

- A visitor who is already logged in opens `/login?redirect=…`: the `/login` guard redirects to the value, the router throws `Redirect blocked: unsafe protocol in href`, and the route error page shows.
- A visitor logs in through the form with the same address: the form navigates to the value and the router renders its bare `Not Found` page.

A target with a space or a backslash anywhere ends on `Not Found` on both paths as well.

The fix is one rule in `safeRedirect`: a target that holds a control character, a space or a backslash falls back to `/`. This loses nothing: the guards build the value from the router's own address, which is always percent-encoded (`/repositories/a%20b`, `/history?repo=a+b%5Cc`), so a raw one of these characters marks a target the app did not write. The old `startsWith("/\\")` test is covered by the new rule and goes.

`web/src/lib/redirect.ts` has no test file yet; this task creates it. The two integration tables in `login.test.tsx` show the fix on both paths.

- [ ] **Step 1: Write the failing tests**

Create `web/src/lib/redirect.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import { loginSearch, safeRedirect } from "./redirect"

describe("safeRedirect", () => {
  it.each([
    ["/history", "/history"],
    ["/history?result=failure#top", "/history?result=failure#top"],
    ["/repositories/a%20b%5Cc", "/repositories/a%20b%5Cc"],
  ])("keeps the path %j", (target, want) => {
    expect(safeRedirect(target)).toBe(want)
  })

  it.each([
    undefined,
    "",
    "history",
    "https://evil.example/x",
    "//evil.example/x",
    "/\\evil.example/x",
    "/\t/evil.example/x",
    "/\n/evil.example/x",
    "/\r/evil.example/x",
    "/ /evil.example/x",
    "/history x",
    "/history\\x",
    "/history\u0000",
    "/history\u007f",
    "/history\u0085",
  ])("falls back to / for %j", (target) => {
    expect(safeRedirect(target)).toBe("/")
  })
})

describe("loginSearch", () => {
  it("names the page to come back to, except the dashboard", () => {
    expect(loginSearch("/history?result=failure")).toEqual({ redirect: "/history?result=failure" })
    expect(loginSearch("/")).toEqual({})
  })
})
```

In `web/src/pages/login.test.tsx`, the last test of the file is a table of redirects that leave the site. Replace its first line:

```tsx
  it.each(["//evil.example/x", "https://evil.example/x", "/\\evil.example/x"])("ignores the redirect %s that leaves the site", async (target) => {
```

with:

```tsx
  const offSite = ["//evil.example/x", "https://evil.example/x", "/\\evil.example/x", "/\t/evil.example/x", "/ /evil.example/x", "/history\\x"]

  it.each(offSite)("ignores the redirect %j that leaves the site", async (target) => {
```

and replace the end of that test, which is also the end of the file:

```tsx
    await user.click(screen.getByRole("button", { name: "Log in" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/"))
  })
})
```

with:

```tsx
    await user.click(screen.getByRole("button", { name: "Log in" }))
    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/")
  })

  it.each(offSite)("opens the dashboard for a visitor who is already logged in, whatever the redirect %j", async (target) => {
    mockApi(authedRoutes())
    const { router } = renderApp(`/login?redirect=${encodeURIComponent(target)}`)
    expect(await screen.findByRole("heading", { name: "Dashboard" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/")
  })
})
```

The four lines to replace are the last four of the file: match them together with the closing `})` of the `describe`. The title changes from `%s` to `%j` so a tab in a target is readable in the test's name, and the assertion now waits for the Dashboard heading: a path the router cannot match renders `Not Found`, which the heading catches.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/lib/redirect.test.ts src/pages/login.test.tsx`
Expected: FAIL, `Tests  15 failed | 23 passed (38)`. In `redirect.test.ts` nine rows fail in the form `AssertionError: expected '/\t/evil.example/x' to be '/' // Object.is equality`. In `login.test.tsx` the rows `"/\t/evil.example/x"`, `"/ /evil.example/x"` and `"/history\\x"` fail in both tables with `Unable to find role="heading" and name "Dashboard"`; for the tab row of the logged-in table the page shows `cannot load the page: Redirect blocked: unsafe protocol in href`.

- [ ] **Step 3: Write the implementation**

In `web/src/lib/redirect.ts`, replace:

```ts
// Only a path on this site: "//host" and "/\host" are read by browsers as
// another origin.
export function safeRedirect(target: string | undefined): string {
  return target && target.startsWith("/") && !target.startsWith("//") && !target.startsWith("/\\") ? target : "/"
}
```

with:

```ts
// Only a path on this site: "//host" and "/\host" are read by browsers as
// another origin, and so is "/<tab>/host", because a URL parser drops tabs and
// line breaks. The router writes every address percent-encoded, so a control
// character, a space or a backslash anywhere marks a target it did not write.
export function safeRedirect(target: string | undefined): string {
  return target && target.startsWith("/") && !target.startsWith("//") && !/[\p{Cc} \\]/u.test(target) ? target : "/"
}
```

`\p{Cc}` is the Unicode class of control characters (U+0000 to U+001F, U+007F to U+009F); it needs the `u` flag. `loginSearch` below it is not changed.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib/redirect.test.ts src/pages/login.test.tsx src/router.test.tsx`
Expected: PASS, all three files; `redirect.test.ts` and `login.test.tsx` report 19 tests each. `router.test.tsx` is included because its `/login` tests go through `safeRedirect`.

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npm run lint` and then `cd web && npm run typecheck`
Expected: both end without an error or a warning

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/redirect.ts web/src/lib/redirect.test.ts web/src/pages/login.test.tsx
git commit -m "fix(web): send malformed redirect targets to /"
```

### Task 14: The unsaved-changes guard forgets a save that was dismissed

**Files:**
- Modify: `web/src/components/unsaved-guard.tsx`
- Test: `web/src/components/unsaved-guard.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing. `UnsavedGuard` keeps its props.

**Items:** 79, 108
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

`UnsavedGuard` blocks a navigation away from a form with unsaved changes and shows a dialog with Stay, Discard and Save. Save calls the page's `onSave`; when it reports success the guard sets a `saved` flag, and an effect then releases the blocked navigation once the save has settled (it leaves when no change is left and stays otherwise).

The flag is cleared only inside that effect, and the effect runs only while a navigation is blocked. Press Save, then dismiss the dialog with Stay or Escape while the save is still running: the navigation is no longer blocked, the save finishes, the flag is set and nothing clears it. The next navigation away is blocked, the effect finds the stale flag and resolves it on the spot without ever showing the dialog. With a change still unsaved that means the click on a link silently does nothing.

The guard now clears the flag whenever no navigation is blocked.

- [ ] **Step 1: Write the failing test**

In `web/src/components/unsaved-guard.test.tsx`, replace the import:

```tsx
import { useState } from "react"
```

with:

```tsx
import { useState, type ReactNode } from "react"
```

The helper `setup` builds a router around its form. The new test needs the same router around a different form, so the router part moves into a helper of its own. In `setup`, replace everything from the line that begins `  const root = createRootRoute(` to the closing brace of the function:

```tsx
  const root = createRootRoute({ component: () => <Outlet /> })
  const form = createRoute({ getParentRoute: () => root, path: "/", component: Form })
  const other = createRoute({ getParentRoute: () => root, path: "/other", component: () => <p>other page</p> })
  const inside = createRoute({ getParentRoute: () => root, path: "/inside", component: () => <p>inside page</p> })
  const router = createRouter({ routeTree: root.addChildren([form, other, inside]), history: createMemoryHistory({ initialEntries: ["/"] }) })
  render(<RouterProvider router={router} />)
  return { router, onSave, onDiscard, user: userEvent.setup() }
}
```

with:

```tsx
  return { ...mount(Form), onSave, onDiscard }
}

function mount(Form: () => ReactNode) {
  const root = createRootRoute({ component: () => <Outlet /> })
  const form = createRoute({ getParentRoute: () => root, path: "/", component: Form })
  const other = createRoute({ getParentRoute: () => root, path: "/other", component: () => <p>other page</p> })
  const inside = createRoute({ getParentRoute: () => root, path: "/inside", component: () => <p>inside page</p> })
  const router = createRouter({ routeTree: root.addChildren([form, other, inside]), history: createMemoryHistory({ initialEntries: ["/"] }) })
  render(<RouterProvider router={router} />)
  return { router, user: userEvent.setup() }
}

// A save the test settles by hand, which leaves one change unsaved.
function setupSlowSave() {
  const save: { settle: (ok: boolean) => void } = { settle: () => {} }
  function Form() {
    const [saving, setSaving] = useState(false)
    return (
      <>
        <p>form page</p>
        <UnsavedGuard
          count={1}
          page="Settings"
          saving={saving}
          onSave={() => {
            setSaving(true)
            return new Promise<boolean>((resolve) => {
              save.settle = (ok) => {
                setSaving(false)
                resolve(ok)
              }
            })
          }}
          onDiscard={() => {}}
        />
      </>
    )
  }
  return { ...mount(Form), save }
}
```

Insert this test directly above the line `  it("lets a navigation within the same page through", async () => {`:

```tsx
  it.each(["Stay", "Escape"])("asks again after %s dismissed the dialog during a save", async (how) => {
    const { router, user, save } = setupSlowSave()
    await leave(router)
    await user.click(screen.getByRole("button", { name: "Save" }))
    expect(await screen.findByText("wait for the save to finish")).toBeInTheDocument()
    if (how === "Stay") await user.click(screen.getByRole("button", { name: "Stay" }))
    else await user.keyboard("{Escape}")
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    await act(async () => save.settle(true))
    act(() => router.history.push("/other"))
    await screen.findByRole("alertdialog")
    await act(async () => {})
    expect(screen.getByText("You have 1 unsaved change on the Settings page.")).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/")
  })

```

The empty `act` after the dialog appears lets the guard's effects run, so the last two assertions describe the settled page and not one frame of it.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/unsaved-guard.test.tsx`
Expected: FAIL, `Tests  2 failed | 8 passed (10)`, both rows with `Unable to find role="alertdialog"`

- [ ] **Step 3: Write the implementation**

In `web/src/components/unsaved-guard.tsx`, replace:

```ts
  useEffect(() => {
    if (!saved || saving || blocker.status !== "blocked") return
    setSaved(false)
    if (count === 0) blocker.proceed()
    else blocker.reset()
  }, [saved, saving, count, blocker])
```

with:

```ts
  useEffect(() => {
    if (!saved) return
    // Dismissed while its save ran: the flag must not release the next
    // navigation, which was never asked about.
    if (blocker.status !== "blocked") return setSaved(false)
    if (saving) return
    setSaved(false)
    if (count === 0) blocker.proceed()
    else blocker.reset()
  }, [saved, saving, count, blocker])
```

The two comment lines above the effect (`// Leaves only once the save has settled every change: …`) stay.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/unsaved-guard.test.tsx`
Expected: PASS (`Tests  10 passed (10)`)

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npm run lint` and then `cd web && npm run typecheck`
Expected: both end without an error or a warning

- [ ] **Step 6: Commit**

```bash
git add web/src/components/unsaved-guard.tsx web/src/components/unsaved-guard.test.tsx
git commit -m "fix(web): drop the guard's saved flag when unblocked"
```

### Task 15: The guard publishes its count of unsaved changes

**Files:**
- Create: `web/src/lib/unsaved.ts`
- Modify: `web/src/components/unsaved-guard.tsx`
- Test: `web/src/components/unsaved-guard.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: `setUnsavedCount(n: number): void` and `unsavedCount(): number`, exported from `web/src/lib/unsaved.ts`. The next task's Log out reads `unsavedCount()`. `UnsavedGuard` keeps its props and now calls `setUnsavedCount`.

**Items:** 83
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

Log out lives in the Shell, the frame around every page. The count of unsaved changes lives inside the page that owns the form (Settings or a repository page) and reaches only the `UnsavedGuard` that page mounts. So the Shell cannot know that Log out is about to throw edits away.

This task adds a module that holds one number, and makes the guard write its `count` there while it is mounted and write 0 when it unmounts. One form page is on screen at a time, so one number is enough. Nothing reads it yet; the next task does. On its own this task changes nothing a user can see.

- [ ] **Step 1: Write the failing test**

In `web/src/components/unsaved-guard.test.tsx`, replace:

```tsx
import { UnsavedGuard } from "./unsaved-guard"
```

with:

```tsx
import { unsavedCount } from "@/lib/unsaved"
import { UnsavedGuard } from "./unsaved-guard"
```

Insert this test directly above the line `  it("still guards a navigation the page does not allow", async () => {`:

```tsx
  it("publishes its count while mounted, and none once the page is left", async () => {
    const { router } = setup(true, true, (pathname) => pathname === "/inside")
    await screen.findByText("form page")
    expect(unsavedCount()).toBe(1)
    act(() => router.history.push("/inside"))
    expect(await screen.findByText("inside page")).toBeInTheDocument()
    expect(unsavedCount()).toBe(0)
  })

```

`/inside` is a path the test's guard allows, so the form is left with its one change still unsaved: the 0 can only come from the guard unmounting.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/unsaved-guard.test.tsx`
Expected: FAIL with `Error: Failed to resolve import "@/lib/unsaved" from "src/components/unsaved-guard.test.tsx". Does the file exist?`

- [ ] **Step 3: Write the implementation**

Create `web/src/lib/unsaved.ts`:

```ts
// The unsaved changes of the form page on screen, for code outside the page
// that is about to throw them away. One form page is mounted at a time.
let count = 0

export function setUnsavedCount(n: number): void {
  count = n
}

export function unsavedCount(): number {
  return count
}
```

With only this file in place the test fails with `AssertionError: expected +0 to be 1 // Object.is equality`; the guard does not publish yet.

In `web/src/components/unsaved-guard.tsx`, replace:

```ts
import { useEffect, useState } from "react"
```

with:

```ts
import { useEffect, useState } from "react"
import { setUnsavedCount } from "@/lib/unsaved"
```

Replace:

```ts
  const [saved, setSaved] = useState(false)
```

with:

```ts
  useEffect(() => {
    setUnsavedCount(count)
    return () => setUnsavedCount(0)
  }, [count])
  const [saved, setSaved] = useState(false)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/unsaved-guard.test.tsx`
Expected: PASS (`Tests  11 passed (11)`)

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npm run lint` and then `cd web && npm run typecheck`
Expected: both end without an error or a warning

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/unsaved.ts web/src/components/unsaved-guard.tsx web/src/components/unsaved-guard.test.tsx
git commit -m "feat(web): publish the unsaved-changes count"
```

### Task 16: Log out asks before it discards unsaved changes

**Files:**
- Modify: `web/src/components/shell.tsx`
- Test: `web/src/components/shell.test.tsx`

**Interfaces:**
- Consumes: `unsavedCount()` from `web/src/lib/unsaved.ts` (the previous task); `ConfirmDialog` and the type `Confirm` from `web/src/components/confirm-dialog.tsx` and `plural` from `web/src/lib/format.ts` (both exist)
- Produces: nothing

**Items:** 83
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

The Log out button in the top bar logs out at once, passing the unsaved-changes guard on purpose (`ignoreBlocker: true`), so edits on the Settings page or a repository page are lost without a word. A test pins that today.

With unsaved changes, Log out now first asks through the confirm dialog the app already uses for destructive actions. Its title is `Log out and discard 1 unsaved change?` for one change and `Log out and discard N unsaved changes?` for more; its buttons are Cancel and Log out. Cancel leaves the page and its edits as they are. Without unsaved changes Log out works as before, with no dialog.

- [ ] **Step 1: Write the failing test**

In `web/src/components/shell.test.tsx`, the test `logs out past unsaved edits` asserts the behaviour this task removes. Replace the whole test:

```tsx
  it("logs out past unsaved edits", async () => {
    mockApi(logoutRoutes(noContent))
    const { user, router } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    await user.click(screen.getByRole("button", { name: "Log out" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(screen.queryByText("Unsaved changes")).toBeNull()
  })
```

with:

```tsx
  it("asks before logging out past unsaved edits", async () => {
    const { calls } = mockApi(logoutRoutes(noContent))
    const { user, router } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    await user.click(screen.getByRole("button", { name: "Log out" }))
    const first = await screen.findByRole("alertdialog", { name: "Log out and discard 1 unsaved change?" })
    await user.click(within(first).getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.path === "/auth/logout")).toBe(false)
    expect(router.state.location.pathname).toBe("/settings")
    expect(screen.getByLabelText("Poll interval")).toHaveValue("15s")

    await user.clear(screen.getByLabelText("Global max"))
    await user.type(screen.getByLabelText("Global max"), "3")
    await user.click(screen.getByRole("button", { name: "Log out" }))
    const second = await screen.findByRole("alertdialog", { name: "Log out and discard 2 unsaved changes?" })
    await user.click(within(second).getByRole("button", { name: "Log out" }))
    expect(await screen.findByRole("button", { name: "Log in" })).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/login")
    expect(screen.queryByText("Unsaved changes")).toBeNull()
  })
```

`within`, `waitFor` and `noContent` are already imported in this file.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/shell.test.tsx`
Expected: FAIL, `Tests  1 failed | 19 passed (20)`, with `Unable to find role="alertdialog" and name "Log out and discard 1 unsaved change?"`

- [ ] **Step 3: Write the implementation**

In `web/src/components/shell.tsx`, replace:

```ts
import { useEffect, useRef } from "react"
```

with:

```ts
import { useEffect, useRef, useState } from "react"
```

Replace:

```ts
import { CapacitySegments } from "@/components/capacity-bar"
```

with:

```ts
import { CapacitySegments } from "@/components/capacity-bar"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
```

Replace:

```ts
import { capacity } from "@/lib/capacity"
import { running } from "@/lib/status"
```

with:

```ts
import { capacity } from "@/lib/capacity"
import { plural } from "@/lib/format"
import { running } from "@/lib/status"
import { unsavedCount } from "@/lib/unsaved"
```

In `Shell`, replace:

```ts
  const seenEpoch = useRef<string | undefined>(undefined)
```

with:

```ts
  const seenEpoch = useRef<string | undefined>(undefined)
  const [confirm, setConfirm] = useState<Confirm | null>(null)
```

Replace the end of `logout`:

```ts
    queryClient.clear()
    await navigate({ to: "/login", ignoreBlocker: true })
  }
```

with the same lines followed by the new function:

```ts
    queryClient.clear()
    await navigate({ to: "/login", ignoreBlocker: true })
  }

  function askLogout() {
    const unsaved = unsavedCount()
    if (unsaved === 0) return void logout()
    setConfirm({
      title: `Log out and discard ${plural(unsaved, "unsaved change")}?`,
      action: "Log out",
      destructive: true,
      run: () => void logout(),
    })
  }
```

Replace:

```tsx
          <Button size="sm" variant="ghost" aria-label="Log out" onClick={() => void logout()}>
```

with:

```tsx
          <Button size="sm" variant="ghost" aria-label="Log out" onClick={askLogout}>
```

At the end of the component's JSX, replace:

```tsx
        <Outlet />
      </main>
    </div>
```

with:

```tsx
        <Outlet />
      </main>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </div>
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/shell.test.tsx`
Expected: PASS (`Tests  20 passed (20)`)

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npm run lint` and then `cd web && npm run typecheck`
Expected: both end without an error or a warning

- [ ] **Step 6: Commit**

```bash
git add web/src/components/shell.tsx web/src/components/shell.test.tsx
git commit -m "fix(web): ask before Log out discards edits"
```

### Task 17: A failed logout with a live session stays on the page

**Files:**
- Modify: `web/src/components/shell.tsx`
- Test: `web/src/components/shell.test.tsx`

**Interfaces:**
- Consumes: `api.authState` from `web/src/api/client.ts`, `errorText` from `web/src/query.ts`, `toast` from `darkraise-ui/components/sonner` (all exist)
- Produces: nothing

**Items:** 281
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

`logout` in the Shell swallows a failed `POST /auth/logout`, clears the query cache and opens `/login`. When the request failed and the session is still valid, the `/login` guard sees a logged-in visitor and sends them straight back to the Dashboard: the page flashes, every query starts from an empty cache, and nothing says that the log out did not happen.

After a failed request the Shell now reads `/auth/state`. When it says the session has ended, log out goes on as today (this is the case of a session that had already expired, which the existing test `lands on /login even when the logout request fails` covers and which keeps passing). When it says the session is live, or when it cannot be read, the page stays, the cache is kept and a toast reads `Log out failed: <the request's error>`.

- [ ] **Step 1: Write the failing test**

In `web/src/components/shell.test.tsx`, insert directly above the line `  it("asks before logging out past unsaved edits", async () => {`:

```tsx
  it.each([
    ["is still live", () => ({ setup_required: false, authenticated: true })],
    ["cannot be read", () => json({ error: "connection refused" }, 502)],
  ])("stays and keeps the cache when a logout fails and the session %s", async (_, state) => {
    let failed = false
    mockApi(
      authedRoutes({
        "GET /auth/state": () => (failed ? state() : { setup_required: false, authenticated: true }),
        "POST /auth/logout": () => {
          failed = true
          return json({ error: "boom" }, 500)
        },
      }),
    )
    const { user, router, queryClient } = renderApp("/")
    const logout = await screen.findByRole("button", { name: "Log out" })
    const clear = vi.spyOn(queryClient, "clear")
    const navigate = vi.spyOn(router, "navigate")
    await user.click(logout)
    expect((await screen.findAllByText("Log out failed: boom")).length).toBeGreaterThan(0)
    expect(clear).not.toHaveBeenCalled()
    expect(navigate).not.toHaveBeenCalled()
    expect(router.state.location.pathname).toBe("/")
    expect(screen.getByRole("heading", { name: "Dashboard" })).toBeInTheDocument()
  })

```

`json`, `vi` and `authedRoutes` are already imported in this file. The spies are the proof that the cache was kept and nothing navigated: checking the cache's contents afterwards would not show it, because a cleared cache refills as soon as the Dashboard is back.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/shell.test.tsx`
Expected: FAIL, `Tests  2 failed | 20 passed (22)`, both rows with `Unable to find an element with the text: Log out failed: boom`

- [ ] **Step 3: Write the implementation**

In `web/src/components/shell.tsx`, replace:

```ts
import { Button } from "darkraise-ui/components/button"
```

with:

```ts
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
```

In `logout`, replace:

```ts
    try {
      await api.logout()
    } catch {
      // a session that already ended still lands on the login page
    }
    queryClient.clear()
```

with:

```ts
    try {
      await api.logout()
    } catch (err) {
      // A session that already ended still lands on the login page. A live
      // one would be sent straight back from /login, with its cache gone.
      const ended = await api.authState().then(
        (state) => !state.authenticated,
        () => false,
      )
      if (!ended) {
        toast.error(`Log out failed: ${errorText(err)}`)
        return
      }
    }
    queryClient.clear()
```

`errorText` is already imported from `@/query`. The line `await navigate({ to: "/login", ignoreBlocker: true })` after `queryClient.clear()` stays.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/shell.test.tsx`
Expected: PASS (`Tests  22 passed (22)`)

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npm run lint` and then `cd web && npm run typecheck`
Expected: both end without an error or a warning

- [ ] **Step 6: Commit**

```bash
git add web/src/components/shell.tsx web/src/components/shell.test.tsx
git commit -m "fix(web): stay logged in when a logout fails"
```

### Task 18: Max in the Add repository dialog

**Files:**
- Modify: `web/src/components/add-repo-dialog.tsx` (`AddRepoForm`, `clampMax`)
- Modify: `web/src/lib/settings-form.ts` (`fieldErrors`, one new exported function)
- Test: `web/src/components/add-repo-dialog.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: `runnerCountError(name: string, value: unknown, min: number): string`, exported from `web/src/lib/settings-form.ts`. It returns `""` for a whole number from `min` to 99, `` `${name} must be >= ${min}` `` for anything that is not a whole number or is below `min`, and `` `${name} must be <= 99` `` above 99. The repository page uses it in Task 19.

**Items:** 76, 101, 108
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

The Add repository dialog sends Max through `clampMax`, which turns `-1` into `0`, `150` into `99` and `1.9` into `1` without a word. The daemon reads a Max of 0 as "no limit", so a typed `-1` becomes an unlimited repository. The daemon itself refuses a Max below 0 or above 99.

After this task the dialog sends what was typed or nothing: a Max that is not a whole number from 0 to 99 shows an inline error under the box and disables Add. The rule and its wording are the ones the settings form already applies to `global_max` (`global_max must be >= 1`, `global_max must be <= 99`); that rule moves into one exported function so both places use it. An empty Max keeps its meaning: the key is left out of the request and the daemon applies its default.

The same edit removes one dead line: `add()` starts with `if (!picked) return setError("Pick a repository first")`, but `add()` is only called by the Add button, which is disabled while nothing is picked.

- [ ] **Step 1: Write the failing test**

In `web/src/components/add-repo-dialog.test.tsx`, insert this test directly above the test `keeps the dialog open with the daemon's reason on failure`:

```tsx
  it.each([
    ["-1", "Max must be >= 0"],
    ["150", "Max must be <= 99"],
    ["1.9", "Max must be >= 0"],
  ])("refuses %s in Max, keeps what was typed and blocks Add", async (typed, message) => {
    const { calls } = mockApi(routes({ "POST /api/repos": () => noContent() }))
    const { dialog, user } = await open()
    await user.click(dialog.getByRole("option", { name: /new-repo/ }))
    const max = dialog.getByLabelText("Max")
    await user.type(max, typed)
    expect(max).toHaveValue(Number(typed))
    expect(dialog.getByText(message)).toBeInTheDocument()
    expect(dialog.getByRole("button", { name: "Add" })).toBeDisabled()
    await user.clear(max)
    await user.type(max, "99")
    expect(dialog.queryByText(/^Max must be/)).toBeNull()
    await user.click(dialog.getByRole("button", { name: "Add" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ name: "new-repo", max: 99, allow_public: false }))
  })

```

A fraction gets the `>=` message because that is what the shared rule says for anything that is not a whole number; the settings form does the same for a fractional `global_max`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/add-repo-dialog.test.tsx`
Expected: FAIL, `3 failed | 12 passed (15)`. Each new row fails with `Unable to find an element with the text: Max must be >= 0` (or `Max must be <= 99` for the `150` row).

- [ ] **Step 3: Write the implementation**

In `web/src/lib/settings-form.ts`, replace:

```ts
// The daemon's cap on global_max (config.MaxRunners); it refuses a larger value.
const MAX_RUNNERS = 99
```

with:

```ts
// The daemon's cap on global_max and on a repository's max and warm
// (config.MaxRunners); it refuses a larger value.
const MAX_RUNNERS = 99

// The daemon's rule for a runner count, in its wording: a whole number from
// min to MAX_RUNNERS. Number("") is 0, so an empty box is the caller's to judge.
export function runnerCountError(name: string, value: unknown, min: number): string {
  const n = Number(value)
  if (!Number.isInteger(n) || n < min) return `${name} must be >= ${min}`
  if (n > MAX_RUNNERS) return `${name} must be <= ${MAX_RUNNERS}`
  return ""
}
```

In the same file, in `fieldErrors`, replace:

```ts
  const max = Number(v.global_max)
  if (!Number.isInteger(max) || max < 1) errors.global_max = "global_max must be >= 1"
  else if (max > MAX_RUNNERS) errors.global_max = `global_max must be <= ${MAX_RUNNERS}`
```

with:

```ts
  const max = runnerCountError("global_max", v.global_max, 1)
  if (max) errors.global_max = max
```

In `web/src/components/add-repo-dialog.tsx`, replace:

```tsx
import { TriangleAlert } from "lucide-react"
```

with:

```tsx
import { CircleAlert, TriangleAlert } from "lucide-react"
```

Replace:

```tsx
import { TagField } from "@/components/tag-field"
import { errorText } from "@/query"

function clampMax(text: string): number {
  return Math.min(99, Math.max(0, Math.trunc(Number(text))))
}
```

with:

```tsx
import { TagField } from "@/components/tag-field"
import { runnerCountError } from "@/lib/settings-form"
import { errorText } from "@/query"
```

In `AddRepoForm`, replace:

```tsx
  const [busy, setBusy] = useState(false)

  async function add() {
    if (!picked) return setError("Pick a repository first")
    const req: AddRepoRequest = { name: picked, allow_public: allowPublic }
    if (max.trim() !== "" && Number.isFinite(Number(max))) req.max = clampMax(max)
```

with:

```tsx
  const [busy, setBusy] = useState(false)
  const maxError = max.trim() === "" ? "" : runnerCountError("Max", max, 0)

  async function add() {
    const req: AddRepoRequest = { name: picked, allow_public: allowPublic }
    if (max.trim() !== "") req.max = Number(max)
```

Replace:

```tsx
            onChange={(e) => setMax(e.target.value)}
          />
        </div>
```

with (the error line uses the same classes and icon as the error line of `Field` in `web/src/components/page/field.tsx`):

```tsx
            onChange={(e) => setMax(e.target.value)}
          />
          {maxError && (
            <p className="flex items-center gap-1 text-xs text-destructive">
              <CircleAlert size={13} aria-hidden="true" className="shrink-0" />
              {maxError}
            </p>
          )}
        </div>
```

Replace:

```tsx
        <Button loading={busy} disabled={!picked || busy} onClick={() => void add()}>
```

with:

```tsx
        <Button loading={busy} disabled={!picked || busy || maxError !== ""} onClick={() => void add()}>
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/add-repo-dialog.test.tsx src/pages/settings.test.tsx src/pages/setup.test.tsx`
Expected: PASS (`Tests  56 passed (56)`). The settings and setup files are included because both render the settings form, whose `global_max` messages now come from `runnerCountError`.

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npx tsc --noEmit && npx eslint src --max-warnings 0`
Expected: both end without an error or a warning.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/add-repo-dialog.tsx web/src/components/add-repo-dialog.test.tsx web/src/lib/settings-form.ts
git commit -m "fix(web): refuse an out-of-range Max when adding"
```

### Task 19: Max and Warm on the repository page

**Files:**
- Modify: `web/src/pages/repository.tsx` (`RepositoryForm`)
- Test: `web/src/pages/repository.test.tsx`

**Interfaces:**
- Consumes: `runnerCountError(name: string, value: unknown, min: number): string` from `web/src/lib/settings-form.ts` (Task 18). It returns `""` for a whole number from `min` to 99, `` `${name} must be >= ${min}` `` for anything that is not a whole number or is below `min`, and `` `${name} must be <= 99` `` above 99.
- Produces: nothing a later task names

**Items:** 279, 108
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

`setNumber` on the repository page clamps what is typed into Max and Warm: `-1` becomes `0`, `150` becomes `99` and `1.9` becomes `1`, and the box is rewritten to the clamped number. The daemon reads a Max of 0 as "no limit".

How the page works today, which the change relies on:

- `draft` holds only the edited fields; `values = { ...loaded, ...draft }` is what the boxes show; `changedKeys(draft, loaded)` lists the draft keys that differ from the loaded values, compared with `===`.
- `setNumber` stores `undefined` for an emptied box. That stays: an emptied box over a set value is a change and is sent as `null`, which clears the value.

After this task a draft entry for `max` or `warm` is one of three things: `undefined` (the box was emptied), a number (a count the daemon accepts, so typing `2` over a loaded `2` is still no change), or the typed text as a string (a value the daemon would refuse). A string is shown as typed, gets an inline error from `runnerCountError`, and blocks Save through the existing `Fix the highlighted settings first` toast. The `Warm must be <= max` rule stays and is skipped while Max itself is refused.

The boxes now receive the stored value itself, not `String(value)`. React compares a number prop loosely with the box's text, so a typed `1.0` or `007` is left on screen and sent as `1` or `7`.

- [ ] **Step 1: Write the failing test**

In `web/src/pages/repository.test.tsx`, insert this test directly above the test `sends null for an emptied box, so the value is inherited again`:

```tsx
  it.each([
    ["Max", "-1", "Max must be >= 0"],
    ["Max", "150", "Max must be <= 99"],
    ["Max", "1.9", "Max must be >= 0"],
    ["Warm", "-1", "Warm must be >= 0"],
    ["Warm", "150", "Warm must be <= 99"],
  ])("refuses %s %s, keeps what was typed and blocks the save", async (label, typed, message) => {
    const { calls } = mockApi(routes())
    const { user } = renderApp("/repositories/darkmem/settings")
    const box = await screen.findByLabelText(label)
    await user.clear(box)
    await user.type(box, typed)
    expect(box).toHaveValue(Number(typed))
    expect(screen.getByText(message)).toBeInTheDocument()
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    expect((await screen.findAllByText("Fix the highlighted settings first")).length).toBeGreaterThan(0)
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
  })

```

The fixture's `darkmem` has `max: 2` and `warm: 1`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/pages/repository.test.tsx`
Expected: FAIL, `5 failed | 32 passed (37)`. Each new row fails at `expect(box).toHaveValue(Number(typed))`, for example `expect(element).toHaveValue(-1)` with `Received: 0`, because the box was rewritten to the clamped number.

- [ ] **Step 3: Write the implementation**

All edits are in `web/src/pages/repository.tsx`.

Among the imports, replace:

```tsx
import { SIDE_LINK } from "@/lib/side-link"
```

with:

```tsx
import { runnerCountError } from "@/lib/settings-form"
import { SIDE_LINK } from "@/lib/side-link"
```

In `RepositoryForm`, replace:

```tsx
  const maxValue = values.max as number | undefined
  const warmValue = (values.warm as number | undefined) ?? 1
  // An emptied Max is undefined here, so warm has no explicit bound, as in the daemon.
  const warmError = maxValue !== undefined && maxValue > 0 && warmValue > maxValue ? "Warm must be <= max" : ""
  const locked = Boolean(repoStatus?.removing) || status.isError

  function setNumber(key: "max" | "warm", text: string) {
    const n = Number(text)
    if (text.trim() !== "" && !Number.isFinite(n)) return
    setDraft((d) => ({ ...d, [key]: text.trim() === "" ? undefined : Math.min(99, Math.max(0, Math.trunc(n))) }))
  }
```

with:

```tsx
  const maxError = values.max === undefined ? "" : runnerCountError("Max", values.max, 0)
  const warmCountError = values.warm === undefined ? "" : runnerCountError("Warm", values.warm, 0)
  // An emptied or refused Max is undefined here, so warm has no explicit bound, as in the daemon.
  const maxValue = maxError ? undefined : (values.max as number | undefined)
  const warmValue = (values.warm as number | undefined) ?? 1
  const warmError = warmCountError || (maxValue !== undefined && maxValue > 0 && warmValue > maxValue ? "Warm must be <= max" : "")
  const locked = Boolean(repoStatus?.removing) || status.isError

  // A count the daemon accepts is held as a number, so it compares with the
  // loaded one; anything else is held as typed, so the box keeps showing it.
  function setNumber(key: "max" | "warm", text: string) {
    const typed = text.trim()
    const value = typed === "" ? undefined : runnerCountError(key, typed, 0) === "" ? Number(typed) : typed
    setDraft((d) => ({ ...d, [key]: value }))
  }
```

In `save`, replace:

```tsx
    if (warmError) {
      toast.error("Fix the highlighted settings first")
```

with:

```tsx
    if (maxError || warmError) {
      toast.error("Fix the highlighted settings first")
```

In the JSX of the Max field, replace:

```tsx
                help="Leave empty for the default: 1 in queue mode, no limit in all mode."
                changed={is("max")}
```

with:

```tsx
                help="Leave empty for the default: 1 in queue mode, no limit in all mode."
                error={maxError}
                changed={is("max")}
```

Replace:

```tsx
                  value={typeof values.max === "number" ? String(values.max) : ""}
```

with:

```tsx
                  value={(values.max as number | string | undefined) ?? ""}
```

Replace:

```tsx
                  value={typeof values.warm === "number" ? String(values.warm) : ""}
```

with:

```tsx
                  value={(values.warm as number | string | undefined) ?? ""}
```

The Warm field keeps `error={warmError}`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/repository.test.tsx`
Expected: PASS (`Tests  37 passed (37)`)

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npx tsc --noEmit && npx eslint src --max-warnings 0`
Expected: both end without an error or a warning.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/repository.tsx web/src/pages/repository.test.tsx
git commit -m "fix(web): refuse an out-of-range repo Max or Warm"
```

### Task 20: A helper that prunes a draft

**Files:**
- Modify: `web/src/lib/draft.ts`
- Test: `web/src/lib/draft.test.ts`

**Interfaces:**
- Consumes: nothing
- Produces: `prune(draft: Values, loaded: Values): Values`, exported from `web/src/lib/draft.ts`. It returns the draft without the entries whose stored value equals the loaded one (`sameValue`: `===`, and lists element by element), and returns the very same object when nothing is dropped. Tasks 21 and 22 call it.

**Items:** 80
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

The settings form and the repository page each keep a `draft` object with one entry per edited field. An entry is written on every edit and removed only for the keys a save sent. So an edit typed back to the loaded value stays in the draft; it is invisible while the loaded value is the same, and turns into an "unsaved change" as soon as the daemon's value moves, for example after someone edits `config.yaml` or uses the CLI. Save then writes the old value back over that edit.

This task adds the one helper both forms will use to drop such entries. It compares the stored values on purpose: `60s` typed over a loaded `1m` means the same duration but is not the same text, so it stays in the draft and the box keeps showing `60s`. Whether such an entry counts as a change is decided elsewhere (`changedKeys` with the form's own comparison) and does not change.

The helper returns the same object when it drops nothing, because the forms will call it while rendering and must not set new state on every render.

- [ ] **Step 1: Write the failing test**

In `web/src/lib/draft.test.ts`, replace the import line:

```ts
import { changedKeys, rejected, sameValue, settle, unsavedText, type Equal } from "./draft"
```

with:

```ts
import { changedKeys, prune, rejected, sameValue, settle, unsavedText, type Equal } from "./draft"
```

Insert this block directly above `describe("settle", () => {`:

```ts
describe("prune", () => {
  it("drops the entries that hold the loaded value and keeps the rest", () => {
    const loaded = { mode: "queue", global_max: 2, labels: ["homelab"], max: undefined }
    expect(prune({ mode: "queue", global_max: 3, labels: ["homelab"], max: undefined }, loaded)).toEqual({ global_max: 3 })
  })

  it("compares the stored values, not what they mean", () => {
    const draft = { start_timeout: "60s", memory_max: "6G " }
    expect(prune(draft, { start_timeout: "1m", memory_max: "6G" })).toEqual(draft)
  })

  it("returns the draft itself when nothing is dropped", () => {
    const draft = { global_max: 3 }
    expect(prune(draft, { global_max: 2 })).toBe(draft)
  })
})

```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/lib/draft.test.ts`
Expected: FAIL, `3 failed | 6 passed (9)`, each new test with `TypeError: prune is not a function`

- [ ] **Step 3: Write the implementation**

In `web/src/lib/draft.ts`, insert directly above the comment that starts `// After a save, an edit still equal to what was sent is settled`:

```ts
// An entry that holds the loaded value is no edit, and left in the draft it
// would count as one again once the loaded value moved. The stored values are
// compared, so text that only means the same (60s over 1m) stays as typed.
// Returns the draft itself when nothing is dropped.
export function prune(draft: Values, loaded: Values): Values {
  const kept = Object.entries(draft).filter(([k, v]) => !sameValue(v, loaded[k]))
  return kept.length === Object.keys(draft).length ? draft : Object.fromEntries(kept)
}

```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib/draft.test.ts`
Expected: PASS (`Tests  9 passed (9)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/draft.ts web/src/lib/draft.test.ts
git commit -m "feat(web): add a helper that prunes a draft"
```

### Task 21: The settings form prunes its draft

**Files:**
- Modify: `web/src/lib/settings-form.ts` (`useSettingsForm`)
- Test: `web/src/pages/settings.test.tsx`

**Interfaces:**
- Consumes: `prune(draft: Values, loaded: Values): Values` from `web/src/lib/draft.ts` (Task 20). It returns the draft without the entries whose stored value equals the loaded one, and the very same object when nothing is dropped.
- Produces: nothing a later task names

**Items:** 80, 108
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

`useSettingsForm` keeps a `draft` with one entry per edited field and shows `values = { ...loaded, ...draft }`. An entry leaves the draft only when a save sent it. Two things go wrong with that:

- Type `3` into Global max and then `2` again, over a loaded `2`. The entry `global_max: 2` stays. When the daemon's value later becomes `5` (a CLI edit), the box still shows `2`, the save bar says "1 unsaved change", and Save writes `2` back over the CLI edit.
- Type `3`. The daemon's value becomes `3` by other means, then `4`. The entry `global_max: 3` stays through both, so the second change is hidden and Save reverts it.

After this task the hook drops every draft entry that holds the loaded value, each time it renders. That covers an edit typed back and a config read that brings the draft's value. The hook is used by the Settings page and by the setup wizard's Settings step, so both get it.

A third case is left by the first two. The hook counts its unsaved changes with a looser comparison than the one that prunes: `120s` over a loaded `2m0s`, or a space typed after a loaded value, is not counted today. Such an entry would stay in the draft unseen and turn into an unsaved change, one that reverts a CLI edit, as soon as the daemon's value moved. So from this task on the count compares the stored values too: what stays in the draft is an unsaved change. The looser comparison stays where it belongs, in the check that the daemon applied a save, because the daemon answers a duration in its own form.

One case must not be pruned: while a save is in flight, the loaded values are the ones being replaced. An edit back to the old value during that time is a real edit and has to survive the save. So the hook does not prune while `saving` is true; `settle` deals with the draft when the save ends, as it does today.

Calling `setDraft` while rendering is the pattern React documents for adjusting state to a changed input; `prune` returns the same object when it drops nothing, so the call happens only when there is something to drop. `web/src/components/confirm-dialog.tsx` uses the same pattern.

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/settings.test.tsx`, insert these three tests directly above the test `checks the fields before sending`:

```tsx
  it("forgets an edit typed back to the loaded value, so a later change at the daemon shows", async () => {
    let config: Config = fixtures.config
    mockApi(routes({ "GET /api/config": () => config }))
    const { user, queryClient } = renderApp("/settings")
    const max = await screen.findByLabelText("Global max")
    await user.clear(max)
    await user.type(max, "3")
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
    await user.clear(max)
    await user.type(max, "2")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
    config = { ...fixtures.config, global_max: 5 }
    await queryClient.refetchQueries({ queryKey: ["config"] })
    await waitFor(() => expect(screen.getByLabelText("Global max")).toHaveValue(5))
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("forgets an edit once the daemon holds the same value", async () => {
    let config: Config = fixtures.config
    mockApi(routes({ "GET /api/config": () => config }))
    const { user, queryClient } = renderApp("/settings")
    const max = await screen.findByLabelText("Global max")
    await user.clear(max)
    await user.type(max, "3")
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
    config = { ...fixtures.config, global_max: 3 }
    await queryClient.refetchQueries({ queryKey: ["config"] })
    await waitFor(() => expect(screen.queryByText(/unsaved change/)).toBeNull())
    config = { ...fixtures.config, global_max: 4 }
    await queryClient.refetchQueries({ queryKey: ["config"] })
    await waitFor(() => expect(screen.getByLabelText("Global max")).toHaveValue(4))
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("keeps an edit typed back to the old value while its save is in flight", async () => {
    let config: Config = fixtures.config
    let answer: () => void = () => undefined
    const { calls } = mockApi(
      routes({
        "GET /api/config": () => config,
        "PATCH /api/config": ({ body }: { body: unknown }) =>
          new Promise<Response>((resolve) => {
            answer = () => {
              config = { ...config, ...(body as Partial<Config>) }
              resolve(noContent())
            }
          }),
      }),
    )
    const { user } = renderApp("/settings")
    const max = await screen.findByLabelText("Global max")
    await user.clear(max)
    await user.type(max, "3")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(calls.some((c) => c.method === "PATCH")).toBe(true))
    await user.clear(max)
    await user.type(max, "2")
    answer()
    expect(await screen.findByText("1 unsaved change")).toBeInTheDocument()
    expect(screen.getByLabelText("Global max")).toHaveValue(2)
  })

```

The fixture's `global_max` is 2. `Config`, `fixtures`, `routes`, `noContent`, `waitFor` and `renderApp` are already imported or defined at the top of the file.

Two existing tests assert the count this task changes. In the same file, replace the whole test:

```tsx
  it("does not count an equal duration as a change", async () => {
    mockApi(routes())
    const { user } = renderApp("/settings")
    const start = await screen.findByLabelText("Start timeout")
    await user.clear(start)
    await user.type(start, "120s")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })
```

with:

```tsx
  it("counts a duration typed in another form as a change, and settles it when saved", async () => {
    // The daemon stores a duration in its own form, so it still answers 2m0s.
    const { calls } = mockApi(routes({ "PATCH /api/config": () => noContent() }))
    const { user } = renderApp("/settings")
    const start = await screen.findByLabelText("Start timeout")
    await user.clear(start)
    await user.type(start, "120s")
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ start_timeout: "120s" }))
    await waitFor(() => expect(screen.queryByText(/unsaved change/)).toBeNull())
    expect(screen.getByLabelText("Start timeout")).toHaveValue("2m0s")
    expect(screen.queryByText(/Daemon did not apply/)).toBeNull()
  })
```

and replace the whole test:

```tsx
  it("does not count a space around a loaded value as a change", async () => {
    mockApi(routes())
    const { user } = renderApp("/settings")
    const memory = await screen.findByLabelText("Memory max")
    await user.type(memory, " ")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })
```

with:

```tsx
  it("counts a space typed after a loaded value as a change", async () => {
    mockApi(routes())
    const { user } = renderApp("/settings")
    const memory = await screen.findByLabelText("Memory max")
    await user.type(memory, " ")
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
  })
```

The fixture's `start_timeout` is `2m0s`.

The third test passes before the change and must keep passing after it: it pins the edit made during a save, which a prune without the `saving` condition would lose.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/pages/settings.test.tsx`
Expected: FAIL, `4 failed | 31 passed (35)`.
- `forgets an edit typed back to the loaded value, so a later change at the daemon shows` fails at `expect(element).toHaveValue(5)` with `Received: 2`.
- `forgets an edit once the daemon holds the same value` fails at `expect(element).toHaveValue(4)` with `Received: 3`.
- `counts a duration typed in another form as a change, and settles it when saved` and `counts a space typed after a loaded value as a change` fail with `Unable to find an element with the text: 1 unsaved change.`

- [ ] **Step 3: Write the implementation**

In `web/src/lib/settings-form.ts`, replace the import line:

```ts
import { changedKeys, sameValue, settle, type Equal, type Values } from "@/lib/draft"
```

with:

```ts
import { changedKeys, prune, sameValue, settle, type Equal, type Values } from "@/lib/draft"
```

In `useSettingsForm`, replace:

```ts
  const loaded = config.data ? loadedValues(config.data) : {}
  const values: Values = { ...loaded, ...draft }
```

with:

```ts
  const loaded = config.data ? loadedValues(config.data) : {}
  // During a save the loaded values are the ones being replaced, so an edit
  // back to one of them is a real edit; settle decides about it afterwards.
  const pruned = config.data && !saving ? prune(draft, loaded) : draft
  if (pruned !== draft) setDraft(pruned)
  const values: Values = { ...loaded, ...draft }
```

`saving` and `setDraft` are declared a few lines above, in the same function.

Two lines further down, replace:

```ts
  const changed = config.data ? changedKeys(draft, loaded, equal) : []
```

with:

```ts
  const changed = config.data ? changedKeys(draft, loaded) : []
```

and, near the end of `save`, replace:

```ts
    setDraft((d) => settle(d, sent, equal))
```

with:

```ts
    setDraft((d) => settle(d, sent))
```

Without a third argument both helpers compare the stored values. Leave `equal` and its other use alone: the line in `save` that begins `const missed =` still needs it, and so does the `type Equal` import.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/settings.test.tsx src/pages/setup.test.tsx`
Expected: PASS (`Tests  44 passed (44)`). `setup.test.tsx` is included because the setup wizard uses the same hook.

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npx tsc --noEmit && npx eslint src --max-warnings 0`
Expected: both end without an error or a warning.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/settings-form.ts web/src/pages/settings.test.tsx
git commit -m "fix(web): prune settings edits equal to the config"
```

### Task 22: The repository page prunes its draft

**Files:**
- Modify: `web/src/pages/repository.tsx` (`RepositoryForm`)
- Test: `web/src/pages/repository.test.tsx`

**Interfaces:**
- Consumes: `prune(draft: Values, loaded: Values): Values` from `web/src/lib/draft.ts` (Task 20). It returns the draft without the entries whose stored value equals the loaded one, and the very same object when nothing is dropped.
- Produces: nothing a later task names

**Items:** 80, 108
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

`RepositoryForm` keeps a `draft` with one entry per edited field and shows `values = { ...loaded, ...draft }`, where `loaded = repoValues(repoCfg)` is the repository's part of the config. An entry leaves the draft only when a save sent it. So an edit typed back to the loaded value stays in the draft, and so does an edit whose value the daemon comes to hold by other means. When the daemon's value moves again (a CLI edit, a change to `config.yaml`), the stale entry hides it, the save bar says "1 unsaved change", and Save writes the old value back.

After this task the form drops every draft entry that holds the loaded value, each time it renders. It does not do so while a save is in flight: the loaded values are then the ones being replaced, an edit back to the old value is a real edit, and `settle` deals with the draft when the save ends, as it does today.

Calling `setDraft` while rendering is the pattern React documents for adjusting state to a changed input; `prune` returns the same object when it drops nothing, so the call happens only when there is something to drop.

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/repository.test.tsx`, insert this helper directly above `describe("repository page", () => {`:

```tsx
// The config the daemon serves once darkmem's max has become the given number.
function withMax(max: number): Config {
  return { ...fixtures.config, repos: (fixtures.config.repos ?? []).map((r) => (r.name === "darkmem" ? { ...r, max } : r)) }
}

```

Insert these three tests directly above the test `disables the form while the daemon is unreachable`:

```tsx
  it("forgets an edit typed back to the loaded value, so a later change at the daemon shows", async () => {
    let config: Config = fixtures.config
    mockApi(routes({ "GET /api/config": () => config }))
    const { user, queryClient } = renderApp("/repositories/darkmem/settings")
    const max = await screen.findByLabelText("Max")
    await user.clear(max)
    await user.type(max, "3")
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
    await user.clear(max)
    await user.type(max, "2")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
    config = withMax(5)
    await queryClient.refetchQueries({ queryKey: ["config"] })
    await waitFor(() => expect(screen.getByLabelText("Max")).toHaveValue(5))
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("forgets an edit once the daemon holds the same value", async () => {
    let config: Config = fixtures.config
    mockApi(routes({ "GET /api/config": () => config }))
    const { user, queryClient } = renderApp("/repositories/darkmem/settings")
    const max = await screen.findByLabelText("Max")
    await user.clear(max)
    await user.type(max, "3")
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
    config = withMax(3)
    await queryClient.refetchQueries({ queryKey: ["config"] })
    await waitFor(() => expect(screen.queryByText(/unsaved change/)).toBeNull())
    config = withMax(4)
    await queryClient.refetchQueries({ queryKey: ["config"] })
    await waitFor(() => expect(screen.getByLabelText("Max")).toHaveValue(4))
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })

  it("keeps an edit typed back to the old value while its save is in flight", async () => {
    let config: Config = fixtures.config
    let answer: () => void = () => undefined
    const { calls } = mockApi(
      routes({
        "GET /api/config": () => config,
        "PATCH /api/config": () =>
          new Promise<Response>((resolve) => {
            answer = () => {
              config = withMax(3)
              resolve(noContent())
            }
          }),
      }),
    )
    const { user } = renderApp("/repositories/darkmem/settings")
    const max = await screen.findByLabelText("Max")
    await user.clear(max)
    await user.type(max, "3")
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(calls.some((c) => c.method === "PATCH")).toBe(true))
    await user.clear(max)
    await user.type(max, "2")
    answer()
    expect(await screen.findByText("1 unsaved change")).toBeInTheDocument()
    expect(screen.getByLabelText("Max")).toHaveValue(2)
  })

```

The fixture's `darkmem` has `max: 2`. `Config`, `fixtures`, `routes`, `noContent`, `waitFor` and `renderApp` are already imported or defined at the top of the file.

The third test passes before the change and must keep passing after it: it pins the edit made during a save, which a prune without the `saving` condition would lose.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/pages/repository.test.tsx`
Expected: FAIL, `2 failed | 38 passed (40)`.
- `forgets an edit typed back to the loaded value, so a later change at the daemon shows` fails at `expect(element).toHaveValue(5)` with `Received: 2`.
- `forgets an edit once the daemon holds the same value` fails at `expect(element).toHaveValue(4)` with `Received: 3`.

- [ ] **Step 3: Write the implementation**

In `web/src/pages/repository.tsx`, replace the import line:

```tsx
import { changedKeys, sameValue, settle, type Values } from "@/lib/draft"
```

with:

```tsx
import { changedKeys, prune, sameValue, settle, type Values } from "@/lib/draft"
```

In `RepositoryForm`, replace:

```tsx
  const loaded = repoValues(repoCfg)
  const values: Values = { ...loaded, ...draft }
```

with:

```tsx
  const loaded = repoValues(repoCfg)
  // During a save the loaded values are the ones being replaced, so an edit
  // back to one of them is a real edit; settle decides about it afterwards.
  const pruned = repoCfg && !saving ? prune(draft, loaded) : draft
  if (pruned !== draft) setDraft(pruned)
  const values: Values = { ...loaded, ...draft }
```

`repoCfg`, `saving` and `setDraft` are declared a few lines above, in the same function.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/repository.test.tsx`
Expected: PASS (`Tests  40 passed (40)`)

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npx tsc --noEmit && npx eslint src --max-warnings 0`
Expected: both end without an error or a warning.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/repository.tsx web/src/pages/repository.test.tsx
git commit -m "fix(web): prune repo edits equal to the config"
```

### Task 23: The read after a save: the helper and the settings form

**Files:**
- Create: `web/src/lib/saved-config.ts`
- Modify: `web/src/lib/settings-form.ts` (`save` in `useSettingsForm`)
- Test: `web/src/pages/settings.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: `readSavedConfig(queryClient: QueryClient): Promise<Config>`, exported from `web/src/lib/saved-config.ts`. It cancels the config query's fetch in flight, then fetches the config and resolves with it; the result is also written to the query cache under `keys.config`. Task 24 calls it from the repository page.

**Items:** 97
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

After a successful PATCH the settings form reads the config back with `queryClient.fetchQuery` and compares it with what it sent; a difference raises `Daemon did not apply <key>; is it older than this ghr?`. The config is also polled every few seconds. When a poll left before the save and has not answered yet, `fetchQuery` does not send a request: it hands back the promise of the fetch in flight, whose answer is the config from before the save. The form then reports that the daemon did not apply a save it did apply.

The fix is to cancel the config query's fetch in flight before the confirming read, with the query client's `cancelQueries`. A cancelled fetch leaves the cached data as it was and raises no error. The repository page carries a copy of the same `fetchQuery` line, so the two lines move into one helper; this task adds the helper and uses it in the settings form, and Task 24 uses it on the repository page.

- [ ] **Step 1: Write the failing test**

In `web/src/pages/settings.test.tsx`, insert this test directly above the test `disables the fields while the daemon is unreachable`. The position matters: toasts of earlier tests in the file stay on screen, and no test above this point raises a `Daemon did not apply` toast.

```tsx
  it("confirms a save with a read of its own, not one that left before it", async () => {
    let config: Config = fixtures.config
    let hold = false
    let late: () => void = () => undefined
    const { calls } = mockApi(
      routes({
        "GET /api/config": () => {
          if (!hold) return config
          hold = false
          const old = config
          return new Promise<Response>((resolve) => {
            late = () => resolve(json(old))
          })
        },
        "PATCH /api/config": ({ body }: { body: unknown }) => {
          config = { ...config, ...(body as Partial<Config>) }
          return noContent()
        },
      }),
    )
    const reads = () => calls.filter((c) => c.method === "GET" && c.path === "/api/config").length
    const { user, queryClient } = renderApp("/settings")
    const max = await screen.findByLabelText("Global max")
    await user.clear(max)
    await user.type(max, "3")
    const before = reads()
    hold = true
    void queryClient.refetchQueries({ queryKey: ["config"] })
    await waitFor(() => expect(reads()).toBe(before + 1))
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(reads()).toBe(before + 2))
    late()
    await waitFor(() => expect(screen.queryByText(/unsaved change/)).toBeNull())
    expect(screen.getByLabelText("Global max")).toHaveValue(3)
    expect(screen.queryByText(/Daemon did not apply/)).toBeNull()
  })

```

What the test does: `refetchQueries` starts a config read that the mock holds open, as a poll that left before the save. Save is then pressed. The save must send a config read of its own (`before + 2`); only then does the held read answer, with the old config, and nothing on screen may change because of it.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/pages/settings.test.tsx`
Expected: FAIL, `1 failed | 35 passed (36)`, with `AssertionError: expected 2 to be 3 // Object.is equality` at `await waitFor(() => expect(reads()).toBe(before + 2))`: the save joined the held read and sent none of its own.

- [ ] **Step 3: Write the implementation**

Create `web/src/lib/saved-config.ts`:

```ts
import type { QueryClient } from "@tanstack/react-query"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { Config } from "@/api/types"

// The read that confirms a save. A poll that left before the save can still be
// in flight, and fetchQuery would hand back that poll's answer, the config
// from before the save; so the fetch in flight is cancelled first.
export async function readSavedConfig(queryClient: QueryClient): Promise<Config> {
  await queryClient.cancelQueries({ queryKey: keys.config })
  return queryClient.fetchQuery({ queryKey: keys.config, queryFn: ({ signal }) => api.config(signal), staleTime: 0 })
}
```

In `web/src/lib/settings-form.ts`, replace the import line:

```ts
import { DAY_MS, durationError, sameDuration } from "@/lib/duration"
```

with:

```ts
import { DAY_MS, durationError, sameDuration } from "@/lib/duration"
import { readSavedConfig } from "@/lib/saved-config"
```

In `save`, replace:

```ts
      const fresh = loadedValues(
        await queryClient.fetchQuery({ queryKey: keys.config, queryFn: ({ signal }) => api.config(signal), staleTime: 0 }),
      )
```

with:

```ts
      const fresh = loadedValues(await readSavedConfig(queryClient))
```

The imports of `api` and `keys` in `settings-form.ts` stay: `api.patchConfig` and `keys.config` are still used in `save`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/settings.test.tsx src/pages/setup.test.tsx`
Expected: PASS (`Tests  45 passed (45)`). `setup.test.tsx` is included because the setup wizard saves through the same hook.

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npx tsc --noEmit && npx eslint src --max-warnings 0`
Expected: both end without an error or a warning.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/saved-config.ts web/src/lib/settings-form.ts web/src/pages/settings.test.tsx
git commit -m "fix(web): confirm a settings save with a new read"
```

### Task 24: The read after a save: the repository page

**Files:**
- Modify: `web/src/pages/repository.tsx` (`save` in `RepositoryForm`)
- Test: `web/src/pages/repository.test.tsx`

**Interfaces:**
- Consumes: `readSavedConfig(queryClient: QueryClient): Promise<Config>` from `web/src/lib/saved-config.ts` (Task 23). It cancels the config query's fetch in flight, then fetches the config and resolves with it. Also `withMax(max: number): Config`, the test helper Task 22 added at the top of `web/src/pages/repository.test.tsx`: the fixture config with `darkmem`'s `max` set to the given number.
- Produces: nothing a later task names

**Items:** 97
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

After a successful PATCH the repository page reads the config back with `queryClient.fetchQuery` and compares it with what it sent; a difference raises `Daemon did not apply <repo>.<key>; is it older than this ghr?`. The config is also polled every few seconds. When a poll left before the save and has not answered yet, `fetchQuery` does not send a request: it hands back the promise of the fetch in flight, whose answer is the config from before the save. The page then reports that the daemon did not apply a save it did apply.

`readSavedConfig` cancels the fetch in flight before it reads. This task replaces the page's own `fetchQuery` line with it.

- [ ] **Step 1: Write the failing test**

In `web/src/pages/repository.test.tsx`, insert this test directly above the test `starts a fresh draft on another repository`. The position matters: toasts of earlier tests in the file stay on screen, and no test above this point raises a `Daemon did not apply` toast.

```tsx
  it("confirms a save with a read of its own, not one that left before it", async () => {
    let config: Config = fixtures.config
    let hold = false
    let late: () => void = () => undefined
    const { calls } = mockApi(
      routes({
        "GET /api/config": () => {
          if (!hold) return config
          hold = false
          const old = config
          return new Promise<Response>((resolve) => {
            late = () => resolve(json(old))
          })
        },
        "PATCH /api/config": () => {
          config = withMax(3)
          return noContent()
        },
      }),
    )
    const reads = () => calls.filter((c) => c.method === "GET" && c.path === "/api/config").length
    const { user, queryClient } = renderApp("/repositories/darkmem/settings")
    const max = await screen.findByLabelText("Max")
    await user.clear(max)
    await user.type(max, "3")
    const before = reads()
    hold = true
    void queryClient.refetchQueries({ queryKey: ["config"] })
    await waitFor(() => expect(reads()).toBe(before + 1))
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await waitFor(() => expect(reads()).toBe(before + 2))
    late()
    await waitFor(() => expect(screen.queryByText(/unsaved change/)).toBeNull())
    expect(screen.getByLabelText("Max")).toHaveValue(3)
    expect(screen.queryByText(/Daemon did not apply/)).toBeNull()
  })

```

What the test does: `refetchQueries` starts a config read that the mock holds open, as a poll that left before the save. Save is then pressed. The save must send a config read of its own (`before + 2`); only then does the held read answer, with the old config, and nothing on screen may change because of it. `json`, `noContent`, `routes`, `fixtures` and `Config` are already imported or defined at the top of the file.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/pages/repository.test.tsx`
Expected: FAIL, `1 failed | 40 passed (41)`, with `AssertionError: expected 2 to be 3 // Object.is equality` at `await waitFor(() => expect(reads()).toBe(before + 2))`: the save joined the held read and sent none of its own.

- [ ] **Step 3: Write the implementation**

In `web/src/pages/repository.tsx`, replace the import line:

```tsx
import { repoDescription } from "@/lib/repos"
```

with:

```tsx
import { repoDescription } from "@/lib/repos"
import { readSavedConfig } from "@/lib/saved-config"
```

In `save`, replace:

```tsx
      const fresh = await queryClient.fetchQuery({ queryKey: keys.config, queryFn: ({ signal }) => api.config(signal), staleTime: 0 })
```

with:

```tsx
      const fresh = await readSavedConfig(queryClient)
```

The imports of `api` and `keys` in `repository.tsx` stay: `api.patchConfig` and `keys.config` are still used in `save`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/repository.test.tsx`
Expected: PASS (`Tests  41 passed (41)`)

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npx tsc --noEmit && npx eslint src --max-warnings 0`
Expected: both end without an error or a warning.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/repository.tsx web/src/pages/repository.test.tsx
git commit -m "fix(web): confirm a repo save with a new read"
```

### Task 25: The setup wizard's Settings step: unsaved settings

**Files:**
- Modify: `web/src/pages/setup.tsx` (`SettingsStep`)
- Modify: `web/src/test/render.tsx` (one new export)
- Test: `web/src/pages/setup.test.tsx`

**Interfaces:**
- Consumes: `UnsavedGuard` from `web/src/components/unsaved-guard.tsx`, with the props it has today: `count: number`, `page: string`, `saving: boolean`, `onSave: () => Promise<boolean>`, `onDiscard: () => void`. It is used as it is; this task does not edit that file.
- Produces: `renderAppInBrowser(path: string)`, exported from `web/src/test/render.tsx`, with the same return value as `renderApp`. No later task names either; `renderAppInBrowser` is a shared test helper other tests may use.

**Items:** 82
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

The setup wizard is one page (`/setup`) whose steps are React state. Its Settings step mounts the settings form; leaving the step unmounts the form and drops its unsaved edits. Today only Next asks first, through a "Save your changes?" dialog with Stay, Discard and Save. Back and Skip the rest leave at once and lose the edits, and reloading or closing the tab gives no warning, although the Settings page warns in the same situation.

After this task:

- Back and Skip the rest go through the same dialog as Next. The dialog remembers which of the three was pressed and carries it out after Save succeeds or after Discard.
- The step mounts `UnsavedGuard`, the component the Settings page uses. It asks the router to warn before the page unloads while its `count` is above zero. It needs only the router, which the wizard has, so it works outside the Shell. It also guards a route change (the browser's Back button out of the wizard) with its own dialog, as on the Settings page.

The reload warning is installed by the router's browser history; the memory history that `renderApp` uses does not listen for `beforeunload`. So the test for it needs the app rendered over the browser history, and `web/src/test/render.tsx` gets a second render function for that.

- [ ] **Step 1: Add the render helper the reload test needs**

Replace the whole of `web/src/test/render.tsx` with the following. `renderApp` is unchanged; the additions are two imports and `renderAppInBrowser`.

```tsx
import { createBrowserHistory, createMemoryHistory } from "@tanstack/react-router"
import { render } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { onTestFinished } from "vitest"
import { App } from "@/app"
import { createQueryClient } from "@/query"
import { createAppRouter } from "@/router"
import { trackQueryClient } from "./query-clients"

export function renderApp(path: string) {
  const queryClient = trackQueryClient(createQueryClient())
  const router = createAppRouter({ queryClient, history: createMemoryHistory({ initialEntries: [path] }) })
  const user = userEvent.setup()
  const view = render(<App router={router} queryClient={queryClient} />)
  return { ...view, router, queryClient, user }
}

// renderApp over the browser's own history, the only one that listens for
// beforeunload. That history patches window.history, so it is undone when the
// test ends.
export function renderAppInBrowser(path: string) {
  window.history.replaceState(null, "", path)
  const history = createBrowserHistory()
  onTestFinished(() => {
    history.destroy()
    window.history.replaceState(null, "", "/")
  })
  const queryClient = trackQueryClient(createQueryClient())
  const router = createAppRouter({ queryClient, history })
  const user = userEvent.setup()
  const view = render(<App router={router} queryClient={queryClient} />)
  return { ...view, router, queryClient, user }
}
```

If another task has already added to this file, keep its additions and add only the two imports (`createBrowserHistory`, `onTestFinished`) and the `renderAppInBrowser` function.

- [ ] **Step 2: Write the failing tests**

In `web/src/pages/setup.test.tsx`, replace the import line:

```tsx
import { renderApp } from "@/test/render"
```

with:

```tsx
import { renderApp, renderAppInBrowser } from "@/test/render"
```

Insert this helper directly above `describe("setup wizard", () => {`:

```tsx
// Opens the wizard on its Settings step and makes one unsaved edit there.
async function unsavedSettings(over: Record<string, unknown> = {}, open = renderApp) {
  const { calls } = mockApi(
    authedRoutes({ "GET /api/setup": { ...base, configured: true }, "PATCH /api/config": () => noContent(), ...over }),
  )
  const view = open("/setup")
  await view.user.click(await screen.findByRole("button", { name: "Next" }))
  const max = await screen.findByLabelText("Global max")
  await view.user.clear(max)
  await view.user.type(max, "3")
  return { ...view, calls }
}

```

Insert these three tests directly above the test `renders without the Shell and opens the Add repository dialog`:

```tsx
  it("asks about unsaved settings before going back", async () => {
    const { user, calls } = await unsavedSettings()
    await user.click(screen.getByRole("button", { name: "Back" }))
    const dialog = within(await screen.findByRole("alertdialog"))
    expect(dialog.getByText("Save your changes?")).toBeInTheDocument()
    expect(screen.getByLabelText("Global max")).toHaveValue(3)
    await user.click(dialog.getByRole("button", { name: "Discard" }))
    expect(await screen.findByRole("heading", { name: "Repositories", level: 2 })).toBeInTheDocument()
    expect(calls.some((c) => c.method === "PATCH")).toBe(false)
  })

  it("asks about unsaved settings before skipping the rest, and saves them on Save", async () => {
    const { user, calls } = await unsavedSettings({ "POST /api/setup/finish": () => noContent() })
    await user.click(screen.getByRole("button", { name: "Skip the rest" }))
    const dialog = within(await screen.findByRole("alertdialog"))
    expect(dialog.getByText("Save your changes?")).toBeInTheDocument()
    await user.click(dialog.getByRole("button", { name: "Stay" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.path === "/api/setup/finish")).toBe(false)
    await user.click(screen.getByRole("button", { name: "Skip the rest" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Save" }))
    await waitFor(() => expect(calls.find((c) => c.method === "PATCH")?.body).toEqual({ global_max: 3 }))
  })

  it("warns before a reload while settings are unsaved", async () => {
    const unload = () => {
      const event = new Event("beforeunload", { cancelable: true })
      window.dispatchEvent(event)
      return event.defaultPrevented
    }
    const { user } = await unsavedSettings({}, renderAppInBrowser)
    expect(unload()).toBe(true)
    const max = screen.getByLabelText("Global max")
    await user.clear(max)
    await user.type(max, "2")
    expect(unload()).toBe(false)
  })

```

The fixture's `global_max` is 2, so typing `2` again in the last test leaves nothing unsaved.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/pages/setup.test.tsx`
Expected: FAIL, `3 failed | 9 passed (12)`.
- `asks about unsaved settings before going back` and `asks about unsaved settings before skipping the rest, and saves them on Save` fail with `Unable to find role="alertdialog"`.
- `warns before a reload while settings are unsaved` fails with `AssertionError: expected false to be true // Object.is equality`.

- [ ] **Step 4: Write the implementation**

All edits are in `web/src/pages/setup.tsx`.

Among the imports, replace:

```tsx
import { InstalledSection, ToolCacheSection } from "@/components/toolchain-sections"
```

with:

```tsx
import { InstalledSection, ToolCacheSection } from "@/components/toolchain-sections"
import { UnsavedGuard } from "@/components/unsaved-guard"
```

In `SettingsStep`, replace:

```tsx
  const form = useSettingsForm()
  const [asking, setAsking] = useState(false)
  if (!form.config) return <p className="text-sm text-muted-foreground">loading…</p>

  async function saveAndNext() {
    setAsking(false)
    if (await form.save()) nav.onNext()
  }

  function discardAndNext() {
    form.discard()
    setAsking(false)
    nav.onNext()
  }
```

with:

```tsx
  const form = useSettingsForm()
  const [leaving, setLeaving] = useState<(() => void) | null>(null)
  if (!form.config) return <p className="text-sm text-muted-foreground">loading…</p>

  // Leaving the step unmounts the form, and its unsaved edits with it.
  const ask = (go: () => void) => () => (form.changed.length > 0 ? setLeaving(() => go) : go())

  async function saveAndGo() {
    const go = leaving
    setLeaving(null)
    if (await form.save()) go?.()
  }

  function discardAndGo() {
    form.discard()
    setLeaving(null)
    leaving?.()
  }
```

`setLeaving(() => go)` is written with the arrow on purpose: React calls a function passed to a state setter, and the arrow makes it store `go` and not call it.

Replace:

```tsx
      <Nav {...nav} onNext={() => (form.changed.length > 0 ? setAsking(true) : nav.onNext())} />
      <AlertDialog
        open={asking}
        onOpenChange={(open) => {
          if (!open) setAsking(false)
        }}
      >
```

with:

```tsx
      <Nav {...nav} onBack={nav.onBack && ask(nav.onBack)} onNext={ask(nav.onNext)} onSkip={nav.onSkip && ask(nav.onSkip)} />
      <UnsavedGuard count={form.changed.length} page="Settings" saving={form.saving} onSave={form.save} onDiscard={form.discard} />
      <AlertDialog
        open={leaving !== null}
        onOpenChange={(open) => {
          if (!open) setLeaving(null)
        }}
      >
```

Replace:

```tsx
            <Button variant="outline" onClick={discardAndNext}>
              Discard
            </Button>
            <AlertDialogAction onClick={() => void saveAndNext()}>Save</AlertDialogAction>
```

with:

```tsx
            <Button variant="outline" onClick={discardAndGo}>
              Discard
            </Button>
            <AlertDialogAction onClick={() => void saveAndGo()}>Save</AlertDialogAction>
```

The dialog's title `Save your changes?`, its description and its Stay button stay as they are.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/setup.test.tsx`
Expected: PASS (`Tests  12 passed (12)`)

- [ ] **Step 6: Lint and type-check**

Run: `cd web && npx tsc --noEmit && npx eslint src --max-warnings 0`
Expected: both end without an error or a warning.

- [ ] **Step 7: Commit**

```bash
git add web/src/pages/setup.tsx web/src/pages/setup.test.tsx web/src/test/render.tsx
git commit -m "fix(web): guard unsaved settings in the wizard"
```

### Task 26: The setup wizard: Skip the rest

**Files:**
- Modify: `web/src/pages/setup.tsx` (`NavProps`, `Nav`, `SetupPage`)
- Test: `web/src/pages/setup.test.tsx`

**Interfaces:**
- Consumes: `ConfirmDialog` and the `Confirm` type from `web/src/components/confirm-dialog.tsx`, as they are today: `Confirm` is `{ title: string; body?: string; action: string; destructive?: boolean; run: () => void }`, and `<ConfirmDialog confirm={Confirm | null} onClose={() => void} />` shows an alert dialog with a Cancel button and a button labelled `action` that calls `run` and closes. This task does not edit that file.
- Produces: nothing a later task names

**Items:** 82
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

`Skip the rest` is on the Repositories, Settings and Toolchains steps of the setup wizard. One click ends setup: there is no confirmation, the button shows no busy state and can be pressed again while the request runs, and it always sends `toolchains: "none"`. The daemon reads that as "the popular toolchain set was declined", also when the set is pending and the user never saw the Toolchains step. Finish, on the last step, sends `"popular"` when the set is pending or was switched on, and `"none"` otherwise.

After this task Skip the rest opens a confirmation, sends the same choice Finish would send, and the confirmation says which. While the request runs the button is busy and disabled. An existing test pins that Skip sends `"none"` at once; it is replaced.

Texts this task adds: title `Skip the rest of setup?`; body `Setup finishes now and queues the popular toolchain set.` or `Setup finishes now. No toolchain set is queued.`; action button `Skip the rest`. The Cancel button is the dialog's own.

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/setup.test.tsx`, replace the whole test `finishes without toolchains when the rest is skipped`:

```tsx
  it("finishes without toolchains when the rest is skipped", async () => {
    let finished: unknown
    mockApi(
      authedRoutes({
        "GET /api/setup": { ...base, configured: true },
        "POST /api/setup/finish": ({ body }: { body: unknown }) => {
          finished = body
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/setup")
    await user.click(await screen.findByRole("button", { name: "Skip the rest" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/"))
    expect(finished).toEqual({ toolchains: "none" })
  })
```

with these four:

```tsx
  it.each([
    [true, "Setup finishes now and queues the popular toolchain set.", "popular"],
    [false, "Setup finishes now. No toolchain set is queued.", "none"],
  ])("confirms Skip the rest and sends what Finish would send (popular set pending: %s)", async (pending, body, toolchains) => {
    let finished: unknown
    const { calls } = mockApi(
      authedRoutes({
        "GET /api/setup": { ...base, configured: true, toolchains_pending: pending },
        "POST /api/setup/finish": ({ body }: { body: unknown }) => {
          finished = body
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/setup")
    await user.click(await screen.findByRole("button", { name: "Skip the rest" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(ask.getByText("Skip the rest of setup?")).toBeInTheDocument()
    expect(ask.getByText(body)).toBeInTheDocument()
    expect(calls.some((c) => c.path === "/api/setup/finish")).toBe(false)
    await user.click(ask.getByRole("button", { name: "Skip the rest" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/"))
    expect(finished).toEqual({ toolchains })
  })

  it("skips without the popular set once it was switched off", async () => {
    let finished: unknown
    mockApi(
      authedRoutes({
        "GET /api/setup": { ...base, configured: true },
        "POST /api/setup/finish": ({ body }: { body: unknown }) => {
          finished = body
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/setup")
    await user.click(await screen.findByRole("button", { name: "Next" }))
    await screen.findByLabelText("Global max")
    await user.click(screen.getByRole("button", { name: "Next" }))
    await user.click(await screen.findByRole("switch", { name: "Popular set" }))
    await user.click(screen.getByRole("button", { name: "Skip the rest" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(ask.getByText("Setup finishes now. No toolchain set is queued.")).toBeInTheDocument()
    await user.click(ask.getByRole("button", { name: "Skip the rest" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/"))
    expect(finished).toEqual({ toolchains: "none" })
  })

  it("leaves setup open when Skip the rest is cancelled", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/setup": { ...base, configured: true }, "POST /api/setup/finish": () => noContent() }))
    const { user, router } = renderApp("/setup")
    await user.click(await screen.findByRole("button", { name: "Skip the rest" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.path === "/api/setup/finish")).toBe(false)
    expect(router.state.location.pathname).toBe("/setup")
  })

  it("shows a busy Skip the rest that cannot be pressed again", async () => {
    const { calls } = mockApi(
      authedRoutes({ "GET /api/setup": { ...base, configured: true }, "POST /api/setup/finish": () => new Promise<Response>(() => {}) }),
    )
    const { user } = renderApp("/setup")
    await user.click(await screen.findByRole("button", { name: "Skip the rest" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Skip the rest" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    // The kit's loading state marks the button aria-busy and keeps its label.
    const skip = screen.getByRole("button", { name: "Skip the rest" })
    expect(skip).toHaveAttribute("aria-busy", "true")
    expect(skip).toBeDisabled()
    expect(calls.filter((c) => c.path === "/api/setup/finish")).toHaveLength(1)
  })
```

`base` at the top of the file has `toolchains_pending: true`, so the third test starts with the Popular set switch on and turns it off.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/pages/setup.test.tsx`
Expected: FAIL, `5 failed | 11 passed (16)`. All five new tests fail with `Unable to find role="alertdialog"`: Skip the rest still finishes at once.

- [ ] **Step 3: Write the implementation**

All edits are in `web/src/pages/setup.tsx`.

Among the imports, replace:

```tsx
import { Brand } from "@/components/brand"
```

with:

```tsx
import { Brand } from "@/components/brand"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
```

Replace:

```tsx
  nextLabel?: string
  busy?: boolean
}

function Nav({ onBack, onNext, onSkip, nextLabel = "Next", busy }: NavProps) {
```

with:

```tsx
  nextLabel?: string
  busy?: boolean
  skipBusy?: boolean
}

function Nav({ onBack, onNext, onSkip, nextLabel = "Next", busy, skipBusy }: NavProps) {
```

In `Nav`, replace:

```tsx
        <Button variant="ghost" onClick={onSkip}>
          Skip the rest
```

with:

```tsx
        <Button variant="ghost" onClick={onSkip} loading={skipBusy}>
          Skip the rest
```

In `SetupPage`, replace:

```tsx
  const [finishError, setFinishError] = useState("")
  const configured = state.data?.configured ?? false
```

with:

```tsx
  const [finishError, setFinishError] = useState("")
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const configured = state.data?.configured ?? false
```

Replace:

```tsx
  const skip = () => void finish("none")
  const go = (n: number) => () => setStep(n)
```

with:

```tsx
  const toolchains = usePopular ? "popular" : "none"
  const skip = () =>
    setConfirm({
      title: "Skip the rest of setup?",
      body: usePopular ? "Setup finishes now and queues the popular toolchain set." : "Setup finishes now. No toolchain set is queued.",
      action: "Skip the rest",
      run: () => void finish(toolchains),
    })
  const go = (n: number) => () => setStep(n)
```

`usePopular` is declared a few lines above (`const usePopular = popular ?? s.toolchains_pending`).

Replace:

```tsx
          <Nav onNext={go(3)} onSkip={skip} />
```

with:

```tsx
          <Nav onNext={go(3)} onSkip={skip} skipBusy={finishing} />
```

Replace:

```tsx
      body = <SettingsStep nav={{ onBack: go(2), onNext: go(4), onSkip: skip }} />
```

with:

```tsx
      body = <SettingsStep nav={{ onBack: go(2), onNext: go(4), onSkip: skip, skipBusy: finishing }} />
```

`SettingsStep` spreads `nav` into its `Nav`, so `skipBusy` reaches the button there without another edit.

Replace:

```tsx
          <Nav onBack={go(3)} onNext={go(5)} onSkip={skip} />
```

with:

```tsx
          <Nav onBack={go(3)} onNext={go(5)} onSkip={skip} skipBusy={finishing} />
```

Replace:

```tsx
          <Nav onBack={go(4)} onNext={() => void finish(usePopular ? "popular" : "none")} nextLabel="Finish" busy={finishing} />
```

with:

```tsx
          <Nav onBack={go(4)} onNext={() => void finish(toolchains)} nextLabel="Finish" busy={finishing} />
```

In the JSX the page returns, replace:

```tsx
              {finishError}
            </p>
          )}
        </section>
```

with:

```tsx
              {finishError}
            </p>
          )}
        </section>
        <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/setup.test.tsx`
Expected: PASS (`Tests  16 passed (16)`)

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npx tsc --noEmit && npx eslint src --max-warnings 0`
Expected: both end without an error or a warning.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/setup.tsx web/src/pages/setup.test.tsx
git commit -m "fix(web): confirm Skip the rest in the wizard"
```

### Task 27: The setup wizard: a failed config read on the Settings step

**Files:**
- Modify: `web/src/pages/setup.tsx` (`SettingsStep`)
- Test: `web/src/pages/setup.test.tsx`

**Interfaces:**
- Consumes: `ErrorLine` from `web/src/components/page/error-line.tsx`, as it is today: `<ErrorLine onRetry={() => void}>{text}</ErrorLine>` renders the text in a `role="alert"` line with a `Try again` button. And two fields the settings form hook already returns: `form.configError` (the error text of a failed config read, or `""`) and `form.retryConfig` (reads the config again).
- Produces: nothing a later task names

**Items:** 82
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The Settings step of the setup wizard needs the config. Until it has one the step renders only the text `loading…`: no Back, no Next, no Skip the rest. When the config read fails, that text stays, the error is never shown, and the wizard cannot be continued until a later poll happens to succeed.

After this task the step shows the error of a failed read with a `Try again` button, as the Settings page does, and it renders Back, Next and Skip the rest whether the config has loaded or not. Without a config there is no form and nothing unsaved, so the buttons act directly.

- [ ] **Step 1: Write the failing test**

In `web/src/pages/setup.test.tsx`, insert this test directly above the test `renders without the Shell and opens the Add repository dialog`:

```tsx
  it("shows a failed config read on the Settings step with a retry, and keeps the way on", async () => {
    let failing = true
    mockApi(
      authedRoutes({
        "GET /api/setup": { ...base, configured: true },
        "GET /api/config": () => (failing ? json({ error: "config unreadable" }, 500) : fixtures.config),
      }),
    )
    const { user } = renderApp("/setup")
    await user.click(await screen.findByRole("button", { name: "Next" }))
    expect(await screen.findByRole("alert")).toHaveTextContent("config unreadable")
    for (const name of ["Back", "Next", "Skip the rest"]) expect(screen.getByRole("button", { name })).toBeEnabled()
    failing = false
    await user.click(screen.getByRole("button", { name: "Try again" }))
    expect(await screen.findByLabelText("Global max")).toHaveValue(2)
  })

```

`json`, `fixtures`, `authedRoutes` and `base` are already imported or defined at the top of the file.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/pages/setup.test.tsx`
Expected: FAIL, `1 failed | 16 passed (17)`, with `Unable to find role="alert"`

- [ ] **Step 3: Write the implementation**

In `web/src/pages/setup.tsx`, among the imports, replace:

```tsx
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
```

with:

```tsx
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { ErrorLine } from "@/components/page/error-line"
```

In `SettingsStep`, replace:

```tsx
  if (!form.config) return <p className="text-sm text-muted-foreground">loading…</p>
```

with:

```tsx
  if (!form.config) {
    return (
      <>
        {form.configError ? (
          <ErrorLine onRetry={form.retryConfig}>{form.configError}</ErrorLine>
        ) : (
          <p className="text-sm text-muted-foreground">loading…</p>
        )}
        <Nav {...nav} />
      </>
    )
  }
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/setup.test.tsx`
Expected: PASS (`Tests  17 passed (17)`)

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npx tsc --noEmit && npx eslint src --max-warnings 0`
Expected: both end without an error or a warning.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/setup.tsx web/src/pages/setup.test.tsx
git commit -m "fix(web): show a failed config read in the wizard"
```

### Task 28: The tag field says why it refuses a tag

**Files:**
- Modify: `web/src/components/tag-field.tsx`
- Test: `web/src/components/shared.test.tsx` (only the `TagField` tests and their `Tags` helper)

**Interfaces:**
- Consumes: nothing
- Produces: a new optional prop on `TagField`, `exact?: boolean`. Without it a duplicate is found ignoring case; with it only the same text is a duplicate. Task 29 passes it for the cleanup prefixes.

**Items:** 103
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

`TagField` is the list editor behind Global labels (Settings), Repo labels and Prefixes (repository page) and Labels (Add repository dialog). When the typed tag is longer than 100 or already in the list, `add()` drops it and clears the box, with no message. Two of its checks also disagree with the daemon: the duplicate check is exact, while the daemon refuses labels that differ only in case (`gpu` and `GPU`); and the length is `tag.length`, which counts UTF-16 units, while the daemon counts characters (`utf8.RuneCountInString`), so a tag of 100 emoji is refused here and accepted there.

After this task a refused tag stays in the box and a line under it says why: `Must be at most 100 characters` or `Already listed as <the tag in the list>`. The line goes away as soon as the text is edited, or when the tag is added after all (the listed tag was removed in the meantime). A duplicate is found ignoring case by default, which is the rule for labels; the new `exact` prop restores the exact comparison. Length is counted in characters. The daemon's other label rules (spaces, commas, the labels ghr adds itself) stay the daemon's to report.

Until Task 29 passes `exact` for the Prefixes field, that field also ignores case; Task 29 comes directly after this one.

An existing test pins the old behaviour (a duplicate clears the box); it is rewritten below.

- [ ] **Step 1: Write the failing tests**

In `web/src/components/shared.test.tsx`, replace the `Tags` helper and the first `TagField` test, which are these lines:

```tsx
function Tags({ initial = [] as string[] }) {
  const [value, setValue] = useState(initial)
  return <TagField label="Labels" value={value} onChange={setValue} />
}

describe("TagField", () => {
  it("adds trimmed tags by Enter and by the button, skipping duplicates", async () => {
    const user = userEvent.setup()
    render(<Tags initial={["gpu"]} />)
    await user.type(screen.getByRole("textbox", { name: "Labels" }), "  big {Enter}")
    await user.type(screen.getByRole("textbox", { name: "Labels" }), "gpu")
    await user.click(screen.getByRole("button", { name: "Add to Labels" }))
    expect(screen.getAllByRole("button", { name: /^Remove / }).map((b) => b.getAttribute("aria-label"))).toEqual([
      "Remove gpu",
      "Remove big",
    ])
    expect(screen.getByRole("textbox", { name: "Labels" })).toHaveValue("")
  })
```

with:

```tsx
function Tags({ initial = [] as string[], exact = false }) {
  const [value, setValue] = useState(initial)
  return <TagField label="Labels" value={value} onChange={setValue} exact={exact} />
}

const tags = () => screen.getAllByRole("button", { name: /^Remove / }).map((b) => b.getAttribute("aria-label"))

async function enter(user: ReturnType<typeof userEvent.setup>, text: string) {
  await user.click(screen.getByRole("textbox", { name: "Labels" }))
  await user.paste(text)
  await user.keyboard("{Enter}")
}

describe("TagField", () => {
  it("adds trimmed tags by Enter and by the button", async () => {
    const user = userEvent.setup()
    render(<Tags initial={["gpu"]} />)
    await user.type(screen.getByRole("textbox", { name: "Labels" }), "  big {Enter}")
    await user.type(screen.getByRole("textbox", { name: "Labels" }), "fast")
    await user.click(screen.getByRole("button", { name: "Add to Labels" }))
    expect(tags()).toEqual(["Remove gpu", "Remove big", "Remove fast"])
    expect(screen.getByRole("textbox", { name: "Labels" })).toHaveValue("")
  })

  it.each([
    ["a duplicate", "gpu", "Already listed as gpu"],
    ["a duplicate in another case", "GPU", "Already listed as gpu"],
    ["a tag over 100 characters", "a".repeat(101), "Must be at most 100 characters"],
  ])("refuses %s, keeps the text and says why", async (_what, typed, reason) => {
    const user = userEvent.setup()
    render(<Tags initial={["gpu"]} />)
    await enter(user, typed)
    const box = screen.getByRole("textbox", { name: "Labels" })
    expect(box).toHaveValue(typed)
    expect(screen.getByText(reason)).toBeInTheDocument()
    expect(tags()).toEqual(["Remove gpu"])
    await user.type(box, "x")
    expect(screen.queryByText(reason)).toBeNull()
  })

  it("drops the reason once the tag is added", async () => {
    const user = userEvent.setup()
    render(<Tags initial={["gpu"]} />)
    await enter(user, "GPU")
    expect(screen.getByText("Already listed as gpu")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Remove gpu" }))
    await user.click(screen.getByRole("button", { name: "Add to Labels" }))
    expect(tags()).toEqual(["Remove GPU"])
    expect(screen.queryByText("Already listed as gpu")).toBeNull()
  })

  it("compares exactly when asked to", async () => {
    const user = userEvent.setup()
    render(<Tags initial={["test_"]} exact />)
    await enter(user, "Test_")
    expect(tags()).toEqual(["Remove test_", "Remove Test_"])
    await enter(user, "test_")
    expect(screen.getByText("Already listed as test_")).toBeInTheDocument()
    expect(tags()).toEqual(["Remove test_", "Remove Test_"])
  })

  it("counts characters, not UTF-16 units", async () => {
    const user = userEvent.setup()
    render(<Tags />)
    await enter(user, "😀".repeat(100))
    expect(tags()).toHaveLength(1)
    await enter(user, "😀".repeat(101))
    expect(screen.getByText("Must be at most 100 characters")).toBeInTheDocument()
    expect(tags()).toHaveLength(1)
  })
```

The test `removes a tag` below them and the `ConfirmDialog` tests above them stay as they are. `enter` pastes the text: typing 101 characters key by key is slow.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/components/shared.test.tsx`
Expected: FAIL, `6 failed | 4 passed (10)`.
- The three `refuses …` rows fail at `expect(box).toHaveValue(typed)` with an empty value received: the box was cleared.
- `drops the reason once the tag is added` fails with `Unable to find an element with the text: Already listed as gpu`: `GPU` was added beside `gpu`.
- `compares exactly when asked to` fails with `Unable to find an element with the text: Already listed as test_`.
- `counts characters, not UTF-16 units` fails with `Unable to find an accessible element with the role "button" and name` `/^Remove /`: the 100 emoji were refused.

- [ ] **Step 3: Write the implementation**

Replace the whole of `web/src/components/tag-field.tsx` with:

```tsx
import { Badge } from "darkraise-ui/components/badge"
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { CircleAlert } from "lucide-react"
import { useState } from "react"

const MAX_TAG = 100

export function TagField({
  label,
  value,
  onChange,
  disabled,
  exact,
}: {
  label: string
  value: string[]
  onChange: (next: string[]) => void
  disabled?: boolean
  // The daemon refuses labels that differ only in case, so a duplicate is
  // found ignoring case unless the list is one it compares exactly.
  exact?: boolean
}) {
  const [text, setText] = useState("")
  const [refused, setRefused] = useState("")

  function add() {
    const tag = text.trim()
    if (tag === "") return setText("")
    // Characters as the daemon counts them, not UTF-16 units.
    if ([...tag].length > MAX_TAG) return setRefused(`Must be at most ${MAX_TAG} characters`)
    const listed = value.find((t) => (exact ? t === tag : t.toLowerCase() === tag.toLowerCase()))
    if (listed !== undefined) return setRefused(`Already listed as ${listed}`)
    onChange([...value, tag])
    setText("")
    setRefused("")
  }

  return (
    <div className="flex flex-col gap-2">
      {value.length > 0 && (
        <ul className="flex flex-wrap gap-1">
          {value.map((tag) => (
            <li key={tag}>
              <Badge variant="secondary" className="gap-1">
                {tag}
                <button
                  type="button"
                  aria-label={`Remove ${tag}`}
                  disabled={disabled}
                  onClick={() => onChange(value.filter((t) => t !== tag))}
                >
                  ✕
                </button>
              </Badge>
            </li>
          ))}
        </ul>
      )}
      <div className="flex gap-2">
        <Input
          aria-label={label}
          placeholder="+ add"
          value={text}
          disabled={disabled}
          onChange={(e) => {
            setText(e.target.value)
            setRefused("")
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault()
              add()
            }
          }}
        />
        <Button type="button" variant="outline" aria-label={`Add to ${label}`} disabled={disabled || text.trim() === ""} onClick={add}>
          Add
        </Button>
      </div>
      {refused && (
        <p className="flex items-center gap-1 text-xs text-destructive">
          <CircleAlert size={13} aria-hidden="true" className="shrink-0" />
          {refused}
        </p>
      )}
    </div>
  )
}
```

What changed against the file as it was: the `CircleAlert` import, the `exact` prop, the `refused` state, the body of `add` (a tag that is added clears `refused` as well as the text), the `onChange` of the `Input` (it now also clears `refused`), and the `{refused && …}` line at the end, which uses the classes and icon of the error line of `Field` in `web/src/components/page/field.tsx`. The list of tags and the Add button are unchanged.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/shared.test.tsx src/pages/repository.test.tsx src/pages/settings.test.tsx src/components/add-repo-dialog.test.tsx`
Expected: PASS (`Tests  102 passed (102)`). The three page files are included because each renders a `TagField`.

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npx tsc --noEmit && npx eslint src --max-warnings 0`
Expected: both end without an error or a warning.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/tag-field.tsx web/src/components/shared.test.tsx
git commit -m "fix(web): say why the tag field refuses a tag"
```

### Task 29: Cleanup prefixes keep the exact comparison

**Files:**
- Modify: `web/src/pages/repository.tsx` (the Prefixes field)
- Test: `web/src/pages/repository.test.tsx`

**Interfaces:**
- Consumes: the `exact?: boolean` prop of `TagField` in `web/src/components/tag-field.tsx` (Task 28). Without it a duplicate is found ignoring case and refused with the line `Already listed as <the tag in the list>`; with it only the same text is a duplicate.
- Produces: nothing a later task names

**Items:** 103
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

`TagField` now treats two tags that differ only in case as duplicates. That is the daemon's rule for labels. Cleanup prefixes are container name prefixes; the daemon compares them as they are and has no case rule for them, so `DARKMEM-` next to `darkmem-` is a legitimate second prefix. The repository page's Prefixes field therefore asks for the exact comparison. Repo labels, Global labels and the Add repository dialog's Labels keep the default.

- [ ] **Step 1: Write the failing test**

In `web/src/pages/repository.test.tsx`, insert this test directly above the test `disables the form while the daemon is unreachable`:

```tsx
  it("finds a duplicate label ignoring case, and a duplicate prefix only when it is the same text", async () => {
    mockApi(routes())
    const { user } = renderApp("/repositories/darkmem/settings")
    await user.type(await screen.findByRole("textbox", { name: "Repo labels" }), "GPU{Enter}")
    expect(screen.getByText("Already listed as gpu")).toBeInTheDocument()
    await user.type(screen.getByRole("textbox", { name: "Prefixes" }), "DARKMEM-{Enter}")
    expect(screen.getByRole("button", { name: "Remove DARKMEM-" })).toBeInTheDocument()
    await user.type(screen.getByRole("textbox", { name: "Prefixes" }), "darkmem-{Enter}")
    expect(screen.getByText("Already listed as darkmem-")).toBeInTheDocument()
  })

```

The fixture's `darkmem` has the label `gpu` and the cleanup prefix `darkmem-`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/pages/repository.test.tsx`
Expected: FAIL, `1 failed | 41 passed (42)`, with `Unable to find an accessible element with the role "button" and name "Remove DARKMEM-"`: the prefix was refused as a duplicate of `darkmem-`.

- [ ] **Step 3: Write the implementation**

In `web/src/pages/repository.tsx`, replace:

```tsx
                  label="Prefixes"
                  value={values.cleanup_name_prefixes as string[]}
```

with:

```tsx
                  label="Prefixes"
                  exact
                  value={values.cleanup_name_prefixes as string[]}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/repository.test.tsx`
Expected: PASS (`Tests  42 passed (42)`)

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npx tsc --noEmit && npx eslint src --max-warnings 0`
Expected: both end without an error or a warning.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/repository.tsx web/src/pages/repository.test.tsx
git commit -m "fix(web): compare cleanup prefixes exactly"
```

### Task 30: The repo actions hook offers to cancel a removal

**Files:**
- Modify: `web/src/lib/use-repo-actions.ts`
- Test: `web/src/lib/use-repo-actions.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: two new fields in the object `useRepoActions(repo, offline)` returns: `toggleLocked: boolean` (true only while the daemon is unreachable) and `toggleLabel: "Pause" | "Resume" | "Cancel removal"`. `locked` keeps its meaning (unreachable, or being removed) and from now on is for Remove only. `togglePause()` sends the resume when the repository is being removed. Tasks 31 and 32 use the two new fields.

**Items:** 107
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

Removing a repository marks it `removing` at the daemon and waits for its running jobs. Resuming such a repository cancels the removal: the daemon clears the flag when it is told `paused: false`. The web UI cannot do that today, because `useRepoActions` returns one `locked` flag that is true while a repository is being removed, and both the Pause/Resume toggle and Remove are disabled by it.

After this task the hook tells its callers what the toggle should read and whether it is locked. While a repository is being removed the toggle reads `Cancel removal`, stays enabled and sends the resume; Remove stays locked. Known limit, accepted: the resume also resumes a repository that was paused before its removal began.

This task changes only the hook. The two places that render the toggle still read `locked` and their own label until Tasks 31 and 32.

An existing test pins the single lock; it is replaced.

- [ ] **Step 1: Write the failing tests**

In `web/src/lib/use-repo-actions.test.tsx`, replace the whole test:

```tsx
  it("locks a repository being removed, or while the daemon is unreachable", () => {
    expect(setup({ ...darkmem, removing: true }).result.current.locked).toBe(true)
    expect(setup(darkmem, true).result.current.locked).toBe(true)
    expect(setup(darkmem).result.current.locked).toBe(false)
  })
```

with these three:

```tsx
  it.each([
    ["a repository being removed", { ...darkmem, removing: true }, false, true, false],
    ["an unreachable daemon", darkmem, true, true, true],
    ["a live repository", darkmem, false, false, false],
  ])("locks Remove and the toggle as %s needs", (_what, repo, offline, locked, toggleLocked) => {
    const { result } = setup(repo, offline)
    expect(result.current.locked).toBe(locked)
    expect(result.current.toggleLocked).toBe(toggleLocked)
  })

  it.each([
    [darkmem, "Pause"],
    [darkcloud, "Resume"],
    [{ ...darkmem, removing: true }, "Cancel removal"],
    [{ ...darkcloud, removing: true }, "Cancel removal"],
  ])("names the toggle after what it does (%#)", (repo, label) => {
    expect(setup(repo).result.current.toggleLabel).toBe(label)
  })

  it("cancels a removal by resuming the repository", async () => {
    const { calls } = mockApi({ "POST /api/repos/darkmem/resume": () => noContent() })
    const { result } = setup({ ...darkmem, removing: true })
    act(() => result.current.togglePause())
    await waitFor(() => expect(calls.map((c) => c.path)).toEqual(["/api/repos/darkmem/resume"]))
  })
```

`darkmem` (not paused), `darkcloud` (paused) and `setup` are defined at the top of the file. The last test uses a repository that is being removed but not marked paused, so it fails unless the hook sends the resume because of `removing`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/lib/use-repo-actions.test.tsx`
Expected: FAIL, `8 failed | 2 passed (10)`.
- The three `locks Remove and the toggle …` rows fail with `expected undefined to be false` or `expected undefined to be true`.
- The four `names the toggle …` rows fail with `expected undefined to be 'Pause'` (or `'Resume'`, `'Cancel removal'`).
- `cancels a removal by resuming the repository` fails with `expected [ '/api/repos/darkmem/pause' ] to deeply equal [ '/api/repos/darkmem/resume' ]`.

- [ ] **Step 3: Write the implementation**

In `web/src/lib/use-repo-actions.ts`, replace:

```ts
  return {
    locked: offline || Boolean(repo.removing),
    togglePause: () => pause.mutate(repo.paused),
```

with:

```ts
  const removing = Boolean(repo.removing)
  return {
    locked: offline || removing,
    // A resume cancels a removal at the daemon, so the toggle stays live while
    // Remove is locked.
    toggleLocked: offline,
    toggleLabel: removing ? "Cancel removal" : repo.paused ? "Resume" : "Pause",
    togglePause: () => pause.mutate(removing || repo.paused),
```

The rest of the returned object (`askRemove`, `confirm`, `closeConfirm`) stays as it is. `pause.mutate(true)` calls `api.resumeRepo`, as it already does for a paused repository.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib/use-repo-actions.test.tsx`
Expected: PASS (`Tests  10 passed (10)`)

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npx tsc --noEmit && npx eslint src --max-warnings 0`
Expected: both end without an error or a warning.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/use-repo-actions.ts web/src/lib/use-repo-actions.test.tsx
git commit -m "feat(web): let the repo toggle cancel a removal"
```

### Task 31: Cancel a removal from the repository row menu

**Files:**
- Modify: `web/src/components/repo-row-menu.tsx`
- Test: `web/src/pages/repositories.test.tsx`

**Interfaces:**
- Consumes: `toggleLocked: boolean` and `toggleLabel: "Pause" | "Resume" | "Cancel removal"` from the object `useRepoActions(repo, offline)` returns (Task 30). `toggleLocked` is true only while the daemon is unreachable; `toggleLabel` is `Cancel removal` while the repository is being removed; `togglePause()` then sends the resume. `locked` stays true while a repository is being removed and is for Remove.
- Produces: nothing a later task names

**Items:** 107
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

`RepoRowMenu` is the "More actions" menu on each repository row of the Dashboard and of the Repositories page. Its first item reads Pause or Resume and is disabled by `actions.locked`, which is true while the repository is being removed. So a removal cannot be cancelled from the menu, although resuming the repository cancels it at the daemon.

After this task the item takes its label and its lock from the hook: for a repository being removed it reads `Cancel removal` and is enabled. Remove keeps `actions.locked`.

- [ ] **Step 1: Write the failing test**

In `web/src/pages/repositories.test.tsx`, insert this test directly above the test `removes a repository only after confirmation`:

```tsx
  it("offers to cancel a removal from the row menu, and keeps Remove locked", async () => {
    const { calls } = mockApi(authedRoutes({ "POST /api/repos/old-repo/resume": () => noContent() }))
    const { user } = renderApp("/repositories")
    await user.click(await screen.findByRole("button", { name: "More actions for old-repo" }))
    expect(await screen.findByRole("menuitem", { name: "Remove" })).toHaveAttribute("aria-disabled", "true")
    await user.click(screen.getByRole("menuitem", { name: "Cancel removal" }))
    await waitFor(() => expect(calls.filter((c) => c.method === "POST").map((c) => c.path)).toEqual(["/api/repos/old-repo/resume"]))
  })

```

The status fixture's `old-repo` has `removing: true`. `authedRoutes`, `noContent`, `waitFor` and `renderApp` are already imported at the top of the file.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/pages/repositories.test.tsx`
Expected: FAIL, `1 failed | 18 passed (19)`, with `Unable to find an accessible element with the role "menuitem" and name "Cancel removal"`

- [ ] **Step 3: Write the implementation**

In `web/src/components/repo-row-menu.tsx`, replace:

```tsx
          <DropdownMenuItem disabled={actions.locked} onSelect={actions.togglePause}>
            {repo.paused ? "Resume" : "Pause"}
          </DropdownMenuItem>
```

with:

```tsx
          <DropdownMenuItem disabled={actions.toggleLocked} onSelect={actions.togglePause}>
            {actions.toggleLabel}
          </DropdownMenuItem>
```

The Remove item below it keeps `disabled={actions.locked}`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/repositories.test.tsx src/components/repo-table.test.tsx src/components/repo-list.test.tsx src/pages/dashboard.test.tsx`
Expected: PASS (`Tests  56 passed (56)`). The other three files render the same menu.

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npx tsc --noEmit && npx eslint src --max-warnings 0`
Expected: both end without an error or a warning.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/repo-row-menu.tsx web/src/pages/repositories.test.tsx
git commit -m "feat(web): cancel a removal from the row menu"
```

### Task 32: Cancel a removal from the repository page

**Files:**
- Modify: `web/src/pages/repository.tsx` (`LiveActions`)
- Test: `web/src/pages/repository.test.tsx`

**Interfaces:**
- Consumes: `toggleLocked: boolean` and `toggleLabel: "Pause" | "Resume" | "Cancel removal"` from the object `useRepoActions(repo, offline)` returns (Task 30). `toggleLocked` is true only while the daemon is unreachable; `toggleLabel` is `Cancel removal` while the repository is being removed; `togglePause()` then sends the resume. `locked` stays true while a repository is being removed and is for Remove.
- Produces: nothing a later task names

**Items:** 107
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

The header of a repository's page has a Pause/Resume button, rendered by `LiveActions`. It is disabled by `actions.locked`, which is true while the repository is being removed, so a removal cannot be cancelled from the page, although resuming the repository cancels it at the daemon.

After this task the button takes its label and its lock from the hook: for a repository being removed it reads `Cancel removal` and is enabled. Remove in the "More actions" menu keeps `actions.locked`. The settings form on the page stays read-only while the repository is being removed, and the line `Being removed. Running jobs finish first, and settings are read-only.` stays; both come from the form's own `locked`, which this task does not touch.

- [ ] **Step 1: Write the failing test**

In `web/src/pages/repository.test.tsx`, insert this test directly above the test `removes from the More actions menu after confirmation`:

```tsx
  it("offers to cancel a removal from the header, and keeps Remove locked", async () => {
    const repos = fixtures.status.repos.map((r) => (r.name === "darkmem" ? { ...r, removing: true } : r))
    const { calls } = mockApi(
      routes({ "GET /api/status": { ...fixtures.status, repos }, "POST /api/repos/darkmem/resume": () => noContent() }),
    )
    const { user } = renderApp("/repositories/darkmem")
    await user.click(await screen.findByRole("button", { name: "Cancel removal" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/repos/darkmem/resume")).toBe(true))
    await user.click(screen.getByRole("button", { name: "More actions for darkmem" }))
    expect(await screen.findByRole("menuitem", { name: "Remove" })).toHaveAttribute("aria-disabled", "true")
  })

```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/pages/repository.test.tsx`
Expected: FAIL, `1 failed | 42 passed (43)`, with `Unable to find role="button" and name "Cancel removal"`

- [ ] **Step 3: Write the implementation**

In `web/src/pages/repository.tsx`, in `LiveActions`, replace:

```tsx
      <Button variant="outline" disabled={actions.locked} onClick={actions.togglePause}>
        {repo.paused ? "Resume" : "Pause"}
      </Button>
```

with:

```tsx
      <Button variant="outline" disabled={actions.toggleLocked} onClick={actions.togglePause}>
        {actions.toggleLabel}
      </Button>
```

The `MoreMenu` two lines below keeps `disabled={actions.locked}`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/repository.test.tsx`
Expected: PASS (`Tests  43 passed (43)`)

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npx tsc --noEmit && npx eslint src --max-warnings 0`
Expected: both end without an error or a warning.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/repository.tsx web/src/pages/repository.test.tsx
git commit -m "feat(web): cancel a removal from the repo page"
```

### Task 33: Dead code: the section index leftovers in FieldGroup

**Files:**
- Modify: `web/src/components/page/field.tsx` (`FieldGroup`)
- Test: `web/src/components/page/field.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names

**Items:** 101
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

`FieldGroup` is the titled group of rows on the Settings and repository pages. The pages once had a section index that jumped to a group and moved focus to its heading. The index was deleted; three things it needed are still in `FieldGroup`: `tabIndex={-1}` on the heading, the `scroll-mt-4` class on the section, and a comment that explains both by the index. A test still asserts the `tabindex`.

This task removes the three. The section keeps its `id` and its `aria-labelledby`, and the heading keeps its `id`: together they make the group a named region, which pages and tests rely on (`getByRole("region", { name })`).

- [ ] **Step 1: Rewrite the test**

In `web/src/components/page/field.test.tsx`, replace the whole test:

```tsx
  it("is a region named by a heading the section index can focus", () => {
    render(
      <FieldGroup id="timing" title="Timing" note="Applies at once">
        <p>rows</p>
      </FieldGroup>,
    )
    const region = screen.getByRole("region", { name: "Timing" })
    expect(region).toHaveAttribute("id", "timing")
    const heading = screen.getByRole("heading", { name: "Timing", level: 2 })
    expect(heading).toHaveAttribute("id", "timing-title")
    expect(heading).toHaveAttribute("tabindex", "-1")
    expect(screen.getByText("Applies at once")).toBeInTheDocument()
  })
```

with:

```tsx
  it("is a region named by its heading, which takes no focus", () => {
    render(
      <FieldGroup id="timing" title="Timing" note="Applies at once">
        <p>rows</p>
      </FieldGroup>,
    )
    const region = screen.getByRole("region", { name: "Timing" })
    expect(region).toHaveAttribute("id", "timing")
    expect(region).not.toHaveClass("scroll-mt-4")
    const heading = screen.getByRole("heading", { name: "Timing", level: 2 })
    expect(heading).toHaveAttribute("id", "timing-title")
    expect(heading).not.toHaveAttribute("tabindex")
    expect(screen.getByText("Applies at once")).toBeInTheDocument()
  })
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/page/field.test.tsx`
Expected: FAIL, `1 failed | 4 passed (5)`, at `expect(element).not.toHaveClass("scroll-mt-4")`

- [ ] **Step 3: Write the implementation**

In `web/src/components/page/field.tsx`, replace:

```tsx
// The heading takes focus when the section index jumps here, so it carries
// tabIndex -1 and an id the index can find.
export function FieldGroup({ id, title, note, children }: { id: string; title: string; note?: string; children: ReactNode }) {
  return (
    <section id={id} aria-labelledby={`${id}-title`} className="scroll-mt-4">
      <h2 id={`${id}-title`} tabIndex={-1} className="text-base font-semibold">
```

with:

```tsx
export function FieldGroup({ id, title, note, children }: { id: string; title: string; note?: string; children: ReactNode }) {
  return (
    <section id={id} aria-labelledby={`${id}-title`}>
      <h2 id={`${id}-title`} className="text-base font-semibold">
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/page/field.test.tsx src/pages/settings.test.tsx src/pages/repository.test.tsx`
Expected: PASS (`Tests  84 passed (84)`). The two page files are included because both render `FieldGroup`.

- [ ] **Step 5: Lint and type-check**

Run: `cd web && npx tsc --noEmit && npx eslint src --max-warnings 0`
Expected: both end without an error or a warning.

- [ ] **Step 6: Commit**

```bash
git add web/src/components/page/field.tsx web/src/components/page/field.test.tsx
git commit -m "refactor(web): drop the section index leftovers"
```

### Task 34: Dead code: the setup page's scroll box

**Files:**
- Modify: `web/src/pages/setup.tsx` (the JSX `SetupPage` returns)
- Test: `web/src/pages/setup.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names

**Items:** 101
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The setup page wraps its content in `<div className="h-full overflow-y-auto">`, under a comment that says darkraise-ui locks `html` and `body` to the viewport with `overflow: hidden`, so the wizard needs a scroll box of its own. That was true once. `web/src/styles/ghr-theme.css` now sets `html, body { height: auto; overflow: visible; }`, so the document itself scrolls on every page. With `body` at `height: auto`, `h-full` on the wrapper resolves to no height, and `overflow-y-auto` on a box with no fixed height never scrolls: the wrapper does nothing and the comment is wrong. A test asserts the two classes.

This task removes the wrapper, the comment and the test. Nothing replaces the test: there is no behaviour left to pin, so this task has no failing-test step.

- [ ] **Step 1: Delete the test that pins the dead classes**

In `web/src/pages/setup.test.tsx`, delete this test together with its comment and the blank line after it:

```tsx
  // darkraise-ui sets html and body to overflow hidden, so without its own
  // scroll container the wizard's lower steps are cut off.
  it("scrolls inside its own container", async () => {
    mockApi(authedRoutes({ "GET /api/setup": { ...base, configured: true } }))
    renderApp("/setup")
    const title = await screen.findByRole("heading", { name: "Set up ghr" })
    expect(title.closest(".overflow-y-auto")).toHaveClass("h-full")
  })

```

- [ ] **Step 2: Remove the wrapper and its comment**

In `web/src/pages/setup.tsx`, at the end of `SetupPage`, replace:

```tsx
  // darkraise-ui locks html and body to the viewport with overflow hidden and
  // leaves scrolling to its layouts; the wizard renders outside the Shell's.
  return (
    <div className="h-full overflow-y-auto">
      <div className="mx-auto max-w-4xl p-4 sm:p-8">
        <Brand className="mb-6" />
        <h1 className="mb-1 text-2xl font-semibold">Set up ghr</h1>
        <p className="mb-6 text-sm text-muted-foreground">Each step saves as you go; this page stays open to you until you finish.</p>
        <ol className="mb-6 flex flex-wrap gap-x-4 gap-y-1 text-sm" aria-label="Setup steps">
          {STEPS.map((name, i) => {
            const done = i === 0 || (i === 1 && s.configured) || i < current
            return (
              <li
                key={name}
                aria-current={i === current ? "step" : undefined}
                className={i === current ? "font-semibold" : "text-muted-foreground"}
              >
                {done ? <Check size={15} role="img" aria-label="Done" className="inline align-[-2px] text-success" /> : `${i + 1}.`} {name}
              </li>
            )
          })}
        </ol>
        <section aria-labelledby="setup-step">
          <h2 id="setup-step" className="mb-4 text-lg font-semibold">
            {STEPS[current]}
          </h2>
          {body}
          {finishError && (
            <p role="alert" className="mt-3 text-sm text-destructive">
              {finishError}
            </p>
          )}
        </section>
        <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
      </div>
    </div>
  )
}
```

with (the same content one level less indented, without the outer `div` and the comment):

```tsx
  return (
    <div className="mx-auto max-w-4xl p-4 sm:p-8">
      <Brand className="mb-6" />
      <h1 className="mb-1 text-2xl font-semibold">Set up ghr</h1>
      <p className="mb-6 text-sm text-muted-foreground">Each step saves as you go; this page stays open to you until you finish.</p>
      <ol className="mb-6 flex flex-wrap gap-x-4 gap-y-1 text-sm" aria-label="Setup steps">
        {STEPS.map((name, i) => {
          const done = i === 0 || (i === 1 && s.configured) || i < current
          return (
            <li
              key={name}
              aria-current={i === current ? "step" : undefined}
              className={i === current ? "font-semibold" : "text-muted-foreground"}
            >
              {done ? <Check size={15} role="img" aria-label="Done" className="inline align-[-2px] text-success" /> : `${i + 1}.`} {name}
            </li>
          )
        })}
      </ol>
      <section aria-labelledby="setup-step">
        <h2 id="setup-step" className="mb-4 text-lg font-semibold">
          {STEPS[current]}
        </h2>
        {body}
        {finishError && (
          <p role="alert" className="mt-3 text-sm text-destructive">
            {finishError}
          </p>
        )}
      </section>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
}
```

The `<ConfirmDialog …/>` line is there because Task 26 added it; keep it.

- [ ] **Step 3: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/setup.test.tsx`
Expected: PASS (`Tests  16 passed (16)`), one test fewer than before this task.

- [ ] **Step 4: Lint and type-check**

Run: `cd web && npx tsc --noEmit && npx eslint src --max-warnings 0`
Expected: both end without an error or a warning.

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/setup.tsx web/src/pages/setup.test.tsx
git commit -m "refactor(web): drop the wizard's dead scroll box"
```

### Task 35: Memoise the log viewer's lines

**Files:**
- Modify: `web/src/components/log-viewer.tsx`
- Test: `web/src/components/log-viewer.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: the `vi.mock("@/lib/ansi", …)` spy at the top of `web/src/components/log-viewer.test.tsx`, which later tests in that file may count with

**Items:** 77
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The log viewer (`LogViewer`) draws every line of an open step with the component `Line`. `Line` is a plain function, so any render of the viewer (a group opened, a step opened, a search keystroke) renders every visible line again, up to 5000 per step. This task wraps `Line` in `memo`. Its props are already a line object and three plain values, so nothing else changes.

The test counts renders through `renderAnsi`: every line and every group header calls it exactly once per render. The test file replaces `renderAnsi` with a spy that calls the real function.

- [ ] **Step 1: Write the failing test**

In `web/src/components/log-viewer.test.tsx`, replace the import block at the top:

```tsx
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"
import type { JobLog, LogLine, Step } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { LogViewer } from "./log-viewer"
```

with:

```tsx
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import type { JobLog, LogLine, Step } from "@/api/types"
import { renderAnsi } from "@/lib/ansi"
import { fixtures } from "@/test/fixtures"
import { LogViewer } from "./log-viewer"

// Every line and every group header draws its text through renderAnsi, so
// its calls count how many of them rendered.
vi.mock("@/lib/ansi", async (importOriginal) => {
  const mod = await importOriginal<typeof import("@/lib/ansi")>()
  return { ...mod, renderAnsi: vi.fn(mod.renderAnsi) }
})
```

Then add this test directly above the test `prefixes notices without a tint and warnings with one`:

```tsx
  it("renders no other line again when a group opens", async () => {
    const { user } = renderViewer()
    const draws = vi.mocked(renderAnsi)
    const before = draws.mock.calls.length
    await user.click(screen.getByRole("button", { name: "Run ruff check ." }))
    expect(screen.getByText("ruff check .")).toBeInTheDocument()
    // The group's header and its one line; the step's four other lines stay.
    expect(draws.mock.calls.length - before).toBe(2)
  })
```

The fixture's open step, `Lint (ruff)`, shows one group header and four lines. Opening the group adds its one line.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/log-viewer.test.tsx`
Expected: FAIL, 1 failed and 15 passed, with `AssertionError: expected 6 to be 2 // Object.is equality`

- [ ] **Step 3: Write the implementation**

In `web/src/components/log-viewer.tsx`, replace:

```tsx
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react"
```

with:

```tsx
import { memo, useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react"
```

Replace:

```tsx
function Line({ line, query, current, target }: { line: LogLine; query: string; current?: number; target: boolean }) {
  return (
```

with:

```tsx
// Memoised: a step shows thousands of these, and a render of the viewer
// changes the props of few of them.
const Line = memo(function Line({ line, query, current, target }: { line: LogLine; query: string; current?: number; target: boolean }) {
  return (
```

The function's closing brace becomes the end of the `memo(` call. Replace:

```tsx
      </span>
    </div>
  )
}

export function LogViewer({
```

with:

```tsx
      </span>
    </div>
  )
})

export function LogViewer({
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/log-viewer.test.tsx`
Expected: PASS (`Tests  16 passed (16)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/components/log-viewer.tsx web/src/components/log-viewer.test.tsx
git commit -m "perf(web): memoise the log viewer's lines"
```

### Task 36: Stop the clock tick at the job page

**Files:**
- Modify: `web/src/pages/run.tsx`
- Test: `web/src/pages/run.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: in `web/src/pages/run.test.tsx`, the `vi.mock("@/lib/ansi", …)` spy, the file-level `afterEach(() => { vi.useRealTimers() })` and the helper `daemonClock`, which later tasks in that file use

**Items:** 77
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The job page (`JobView` in `web/src/pages/run.tsx`) calls `useNow()`, which changes once a second, so `JobView` renders once a second. It renders `JobLogs`, which renders the log viewer, so the whole log is rendered again every second. `JobLogs` also gets the route's `search` object as a prop.

This task wraps `JobLogs` in `memo` and passes it the step and line as plain numbers instead of the `search` object. Its other props are a string, the job object and a number that is constant for a completed job. The memo relies on the query library keeping the identity of the job object and of the log object when a refetch brings equal data (structural sharing, the library's default). The test shows that holding: it refetches every query and checks that no line rendered.

Two things the test needs that are easy to miss:

- The page's clock follows the daemon's clock, taken from `GET /api/status`. The fixture's status carries a fixed time, so under fake timers each status poll would pull the page's clock back. The test answers status with the fake clock's time (`daemonClock`).
- The query library tells its observers about a result on a zero-delay timer, so after the refetch the test advances the fake clock by 50 ms before it checks.

An existing test changes with this task. `shows the job's heading, runner, labels and steps` reads the step name `Lint (ruff)`, which is only on screen once the log has loaded, but does not wait for the log. It passed because the clock's first tick rendered the log one turn early. With the memo that no longer happens, so the test waits for the text.

- [ ] **Step 1: Write the failing test**

In `web/src/pages/run.test.tsx`, replace the lines at the top:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { json, mockApi, type Call } from "@/test/api"
import { fixtures, runRoutes } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const runCalls = (calls: Call[]) => calls.filter((c) => c.path.startsWith("/api/actions/"))
```

with:

```tsx
import { act, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { renderAnsi } from "@/lib/ansi"
import { json, mockApi, type Call } from "@/test/api"
import { fixtures, runRoutes } from "@/test/fixtures"
import { renderApp } from "@/test/render"

// Every log line and every group header draws its text through renderAnsi,
// so its calls count how many of them rendered.
vi.mock("@/lib/ansi", async (importOriginal) => {
  const mod = await importOriginal<typeof import("@/lib/ansi")>()
  return { ...mod, renderAnsi: vi.fn(mod.renderAnsi) }
})

afterEach(() => {
  vi.useRealTimers()
})

const runCalls = (calls: Call[]) => calls.filter((c) => c.path.startsWith("/api/actions/"))

// The page's clock follows the daemon's, so under fake timers the daemon's
// time has to move with them.
const daemonClock = () => ({ ...fixtures.status, now: new Date().toISOString() })
```

In the test `shows the job's heading, runner, labels and steps`, replace:

```tsx
    expect(job.getAllByText("Lint (ruff)").length).toBeGreaterThan(0)
```

with:

```tsx
    expect((await job.findAllByText("Lint (ruff)")).length).toBeGreaterThan(0)
```

Add this test in `describe("Job page", …)`, directly above the test `shows why the log could not be read and reads it again on Try again`:

```tsx
  it("renders no log line again on a clock tick or on a refetch that brings equal data", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true, now: new Date("2026-10-03T14:05:00Z") })
    const run = {
      ...fixtures.runDetail,
      status: "in_progress",
      conclusion: "",
      jobs: fixtures.runDetail.jobs.map((j) => (j.id === 1005 ? { ...j, status: "in_progress", conclusion: "", completed_at: undefined } : j)),
    }
    const { calls } = mockApi(runRoutes({ "GET /api/status": daemonClock, "GET /api/actions/darkmem/runs/102": run }))
    const start = vi.mocked(renderAnsi).mock.calls.length
    const { queryClient } = renderApp("/actions/darkmem/runs/102/jobs/1002")
    await screen.findByLabelText("Search logs")
    const rail = within(screen.getByRole("navigation", { name: "Run" }))
    const runningFor = () => rail.getByRole("link", { name: /E2E/ }).textContent
    const draws = vi.mocked(renderAnsi)
    const drawn = draws.mock.calls.length
    expect(drawn).toBeGreaterThan(start)

    const before = runningFor()
    await act(() => vi.advanceTimersByTimeAsync(2000))
    expect(runningFor()).not.toBe(before)
    expect(draws.mock.calls.length).toBe(drawn)

    const reads = (path: string) => calls.filter((c) => c.path === path).length
    const runReads = reads("/api/actions/darkmem/runs/102")
    const logReads = reads("/api/actions/darkmem/jobs/1002/log")
    // The query library tells its observers on a zero-delay timer.
    await act(async () => {
      await queryClient.refetchQueries()
      await vi.advanceTimersByTimeAsync(50)
    })
    expect(reads("/api/actions/darkmem/runs/102")).toBe(runReads + 1)
    expect(reads("/api/actions/darkmem/jobs/1002/log")).toBe(logReads + 1)
    expect(draws.mock.calls.length).toBe(drawn)
  })
```

The run in this test has one job still running (`E2E (api)`), so the rail shows a duration that grows with the page's clock. That it changed proves the tick reached the page; the unchanged draw count proves it stopped before the log. The clock is advanced by two seconds, not one, so the rounded duration is certain to differ.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/pages/run.test.tsx`
Expected: FAIL, 1 failed and 31 passed, with `AssertionError: expected 34 to be 32 // Object.is equality` at the first `expect(draws.mock.calls.length).toBe(drawn)`. The received number is the expected one plus 2: the open step's group header is drawn once per tick. Both numbers grow with the tests that ran before this one in the file, so only their difference matters.

- [ ] **Step 3: Write the implementation**

In `web/src/pages/run.tsx`, replace:

```tsx
import { useId, type ReactNode } from "react"
```

with:

```tsx
import { memo, useId, type ReactNode } from "react"
```

Replace:

```tsx
function JobLogs({ repo, job, search, ms }: { repo: string; job: RunJob; search: JobSearch; ms: number | undefined }) {
  const log = useJobLog(repo, job.id, true)
```

with:

```tsx
// Memoised, and given the step and line as plain values, so the page's
// one-second clock does not render the log again.
const JobLogs = memo(function JobLogs({
  repo,
  job,
  step,
  line,
  ms,
}: {
  repo: string
  job: RunJob
  step: number | undefined
  line: number | undefined
  ms: number | undefined
}) {
  const log = useJobLog(repo, job.id, true)
```

Further down in the same function, replace:

```tsx
      key={`${job.id}:${search.step ?? ""}:${search.line ?? ""}`}
      log={log.data}
      steps={job.steps}
      step={search.step}
      line={search.line}
```

with:

```tsx
      key={`${job.id}:${step ?? ""}:${line ?? ""}`}
      log={log.data}
      steps={job.steps}
      step={step}
      line={line}
```

The function's closing brace becomes the end of the `memo(` call. Replace:

```tsx
      }
    />
  )
}

function JobView(
```

with:

```tsx
      }
    />
  )
})

function JobView(
```

In `JobView`, replace:

```tsx
            <JobLogs repo={run.repo} job={job} search={search} ms={ms} />
```

with:

```tsx
            <JobLogs repo={run.repo} job={job} step={search.step} line={search.line} ms={ms} />
```

The import of `JobSearch` stays: `JobView` still uses it.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/run.test.tsx`
Expected: PASS (`Tests  32 passed (32)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/run.tsx web/src/pages/run.test.tsx
git commit -m "perf(web): stop the clock tick at the job page"
```

### Task 37: Parse a log line's ANSI once

**Files:**
- Modify: `web/src/lib/ansi.tsx`
- Modify: `web/src/components/log-viewer.tsx`
- Test: `web/src/lib/ansi.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: `parsedLine(line: { text: string }): ParsedLine` and `interface ParsedLine { segments: AnsiSegment[]; text: string }`, exported from `web/src/lib/ansi.tsx`; `renderAnsi` now takes the line object as its first argument: `renderAnsi(line: { text: string }, query = "", current?: number): ReactNode[]`

**Items:** 77
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

`renderAnsi(raw, query, current)` tokenises the line's raw text on every call, and it is called on every render of a line. This task adds `parsedLine`, which parses a line object once and keeps the result in a `WeakMap` keyed by that object, and makes `renderAnsi` take the line object and read the cached result. The log's line objects live as long as the log is cached, so each line is parsed once.

`ansiSegments(raw)` and `plainText(raw)` stay as they are: `web/src/components/run-annotations.tsx` calls `plainText` with a plain string.

The test counts how often the line's `text` is read, through a getter: parsing is the only thing that reads it.

- [ ] **Step 1: Write the failing test**

In `web/src/lib/ansi.test.tsx`, replace:

```tsx
import { ansiSegments, matchRanges, plainText, renderAnsi } from "./ansi"

function html(raw: string, query?: string, current?: number): HTMLElement {
  return render(<p>{renderAnsi(raw, query, current)}</p>).container.firstElementChild as HTMLElement
}
```

with:

```tsx
import { ansiSegments, matchRanges, parsedLine, plainText, renderAnsi } from "./ansi"

function html(raw: string, query?: string, current?: number): HTMLElement {
  return render(<p>{renderAnsi({ text: raw }, query, current)}</p>).container.firstElementChild as HTMLElement
}
```

Add this block directly above `describe("matchRanges", () => {`:

```tsx
describe("parsedLine", () => {
  it("parses a line object once, however often it is asked for or rendered", () => {
    let reads = 0
    const line = {
      get text() {
        reads++
        return "\u001b[31mFound\u001b[0m 1 error."
      },
    }
    const parsed = parsedLine(line)
    expect(parsed).toEqual({
      segments: [
        { text: "Found", color: "red", bold: false },
        { text: " 1 error.", color: undefined, bold: false },
      ],
      text: "Found 1 error.",
    })
    expect(parsedLine(line)).toBe(parsed)
    render(<p>{renderAnsi(line, "found", 0)}</p>)
    render(<p>{renderAnsi(line, "error")}</p>)
    expect(reads).toBe(1)
  })
})

```

The `\u001b` in the test is typed as those six characters, a JavaScript escape, as in the tests above it.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/lib/ansi.test.tsx`
Expected: FAIL, 5 failed and 12 passed. The new test fails with `TypeError: parsedLine is not a function`. The four `renderAnsi` tests fail with `TypeError: raw.replace is not a function`, because their helper now passes an object.

- [ ] **Step 3: Write the implementation**

In `web/src/lib/ansi.tsx`, insert directly below the `plainText` function (above the comment `// Case-insensitive, non-overlapping occurrences of query in text, as`):

```tsx
export interface ParsedLine {
  segments: AnsiSegment[]
  // The segments' text joined: what the line shows and what a search reads.
  text: string
}

const parsed = new WeakMap<object, ParsedLine>()

// Keyed by the line object, so a line is parsed once however often it is
// rendered or searched, and the entry goes when the log does.
export function parsedLine(line: { text: string }): ParsedLine {
  let out = parsed.get(line)
  if (!out) {
    const segments = ansiSegments(line.text)
    out = { segments, text: segments.map((s) => s.text).join("") }
    parsed.set(line, out)
  }
  return out
}

```

In the same file, replace:

```tsx
export function renderAnsi(raw: string, query = "", current?: number): ReactNode[] {
  const segments = ansiSegments(raw)
  const ranges = matchRanges(segments.map((s) => s.text).join(""), query)
```

with:

```tsx
export function renderAnsi(line: { text: string }, query = "", current?: number): ReactNode[] {
  const { segments, text: plain } = parsedLine(line)
  const ranges = matchRanges(plain, query)
```

The rest of `renderAnsi` stays. The joined text is named `plain` because the loop below declares its own `text`.

In `web/src/components/log-viewer.tsx`, the two callers pass the line object. In `Line`, replace:

```tsx
        {renderAnsi(line.text, query, current)}
```

with:

```tsx
        {renderAnsi(line, query, current)}
```

In `renderLines`, replace:

```tsx
                  {renderAnsi(
                    row.line.text,
                    query,
```

with:

```tsx
                  {renderAnsi(
                    row.line,
                    query,
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib/ansi.test.tsx src/components/log-viewer.test.tsx src/pages/run.test.tsx`
Expected: PASS (`Tests  65 passed (65)`). The two other files are included because they render lines through `renderAnsi`.

- [ ] **Step 5: Type-check**

Run: `cd web && npx tsc --noEmit`
Expected: no output. A caller of `renderAnsi` that still passes a string would be reported here.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/ansi.tsx web/src/lib/ansi.test.tsx web/src/components/log-viewer.tsx
git commit -m "perf(web): parse a log line's ANSI once"
```

### Task 38: Search the log's parsed lines

**Files:**
- Modify: `web/src/lib/log-view.ts`
- Test: `web/src/lib/log-view.test.ts`

**Interfaces:**
- Consumes: `parsedLine(line: { text: string }): ParsedLine` from `web/src/lib/ansi.tsx`, whose `text` field is the line's text without ANSI codes; it parses a line object once and caches the result by that object
- Produces: nothing

**Items:** 77
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

`findMatches(log, query)` runs on every search keystroke and strips the ANSI codes from every line of every step each time, with `plainText(line.text)`. This task makes it read the cached plain text of `parsedLine(line)` instead, the same cache the renderer uses, so a line is tokenised once for both.

- [ ] **Step 1: Write the failing test**

In `web/src/lib/log-view.test.ts`, in `describe("findMatches", …)`, add this test after the test `counts several occurrences in one line and skips endgroup lines` (it becomes the last test of the block):

```ts
  it("reads each line's text once across searches", () => {
    let reads = 0
    const counted: LogLine = {
      n: 1,
      kind: "",
      get text() {
        reads++
        return "\u001b[31mFound\u001b[0m 1 error."
      },
    }
    const log: JobLog = { job_id: 1, too_large: false, expired: false, no_log: false, steps: [{ number: 1, name: "s", lines: [counted] }] }
    expect(findMatches(log, "found")).toEqual([{ step: 1, line: 1, index: 0 }])
    expect(findMatches(log, "error")).toEqual([{ step: 1, line: 1, index: 0 }])
    expect(reads).toBe(1)
  })
```

`JobLog` and `LogLine` are already imported in that file. The `\u001b` is typed as those six characters.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/lib/log-view.test.ts`
Expected: FAIL, 1 failed and 11 passed, with `AssertionError: expected 2 to be 1 // Object.is equality`

- [ ] **Step 3: Write the implementation**

In `web/src/lib/log-view.ts`, replace:

```ts
import { matchRanges, plainText } from "@/lib/ansi"
```

with:

```ts
import { matchRanges, parsedLine } from "@/lib/ansi"
```

In `findMatches`, replace:

```ts
      matchRanges(plainText(line.text), query).forEach((_, index) => out.push({ step: step.number, line: line.n, index }))
```

with:

```ts
      matchRanges(parsedLine(line).text, query).forEach((_, index) => out.push({ step: step.number, line: line.n, index }))
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib/log-view.test.ts src/components/log-viewer.test.tsx`
Expected: PASS (`Tests  28 passed (28)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/log-view.ts web/src/lib/log-view.test.ts
git commit -m "perf(web): search the log's parsed lines"
```

### Task 39: Defer the log search behind typing

**Files:**
- Modify: `web/src/components/log-viewer.tsx`
- Test: `web/src/components/log-viewer.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 77
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

Each keystroke in the log's search box sets the query, and the same render matches the whole log and re-renders every visible line. On a large log the box lags behind the typing. This task keeps the typed text in its own state and derives the query from it with React's `useDeferredValue`: the box renders at once, and the matching and the lines follow in a second, interruptible render.

One more change is needed for that to be clean. Today a keystroke also resets the current match to the first one (`setCur(0)`). With a deferred query that reset would land one render early, against the previous search's matches, and scroll to the wrong match for a moment. So the current match is stored together with the query it was counted in, and counts as 0 for any other query; when the deferred query changes, the stored match is reset to it, so a text typed again later also starts at its first match, as today. `onQuery` goes away.

The test records the match counter at each commit with React's `Profiler`, whose `onRender` runs after the DOM is updated.

- [ ] **Step 1: Write the failing test and a guard**

In `web/src/components/log-viewer.test.tsx`, add this import below the `@testing-library/user-event` import:

```tsx
import { Profiler } from "react"
```

Add these two tests directly above the test `says when nothing matches`:

```tsx
  it("commits a keystroke before it matches the new text", async () => {
    const user = userEvent.setup()
    const commits: (string | undefined)[] = []
    const count = () => screen.queryByText(/^\d+ of \d+$/)?.textContent ?? undefined
    render(
      <Profiler id="log" onRender={() => commits.push(count())}>
        <LogViewer log={fixtures.jobLog} steps={steps} />
      </Profiler>,
    )
    const box = screen.getByRole("searchbox", { name: "Search logs" })
    await user.type(box, "ruf{Enter}")
    expect(count()).toBe("2 of 4")
    commits.length = 0
    await user.type(box, "f")
    // The box takes the keystroke in a commit of its own, the earlier search
    // untouched; the new search starts again at its first match.
    expect(commits).toEqual(["2 of 4", "1 of 4"])
  })

  it("starts an earlier search again at its first match", async () => {
    const { user } = renderViewer()
    const box = screen.getByRole("searchbox", { name: "Search logs" })
    await user.type(box, "ruf{Enter}")
    expect(screen.getByText("2 of 4")).toBeInTheDocument()
    await user.type(box, "f")
    expect(screen.getByText("1 of 4")).toBeInTheDocument()
    await user.type(box, "{Backspace}")
    expect(screen.getByText("1 of 4")).toBeInTheDocument()
  })
```

The second test passes before the change and must keep passing after it: it pins the reset on every change of the search text, which storing the match beside its query would lose for a text typed a second time. `renderViewer` is defined at the top of the file.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/log-viewer.test.tsx`
Expected: FAIL, 1 failed and 17 passed, with `AssertionError: expected [ '1 of 4' ] to deeply equal [ '2 of 4', '1 of 4' ]`

- [ ] **Step 3: Write the implementation**

All edits are in `web/src/components/log-viewer.tsx`.

Replace:

```tsx
import { memo, useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react"
```

with:

```tsx
import { memo, useDeferredValue, useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode } from "react"
```

In `LogViewer`, replace:

```tsx
  const [query, setQuery] = useState("")
  const [cur, setCur] = useState(0)
  const ref = useRef<HTMLDivElement>(null)
```

with:

```tsx
  const [typed, setTyped] = useState("")
  // The box takes each keystroke at once; the matching and the lines follow
  // when the browser has time for them.
  const query = useDeferredValue(typed)
  // The current match belongs to the search it was counted in, so a new
  // search text starts again at its first match, and so does an earlier
  // text typed again.
  const [at, setAt] = useState({ query: "", index: 0 })
  if (at.query !== query) setAt({ query, index: 0 })
  const cur = at.query === query ? at.index : 0
  const ref = useRef<HTMLDivElement>(null)
```

Calling `setAt` while rendering is the pattern React documents for adjusting state to a changed input; it runs only in the render in which the deferred query has just changed.

Everything below that reads `query` or `cur` (the matches, the lines, the counter, the scroll effect) keeps its code and now follows the deferred query.

Replace:

```tsx
  function onQuery(next: string) {
    setQuery(next)
    setCur(0)
  }

  function onKey(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key !== "Enter" || matches.length === 0) return
    e.preventDefault()
    const next = (cur + (e.shiftKey ? matches.length - 1 : 1)) % matches.length
    setCur(next)
  }
```

with:

```tsx
  function onKey(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key !== "Enter" || matches.length === 0) return
    e.preventDefault()
    const next = (cur + (e.shiftKey ? matches.length - 1 : 1)) % matches.length
    setAt({ query, index: next })
  }
```

In the search box, replace:

```tsx
          value={query}
          onChange={(e) => onQuery(e.target.value)}
```

with:

```tsx
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/log-viewer.test.tsx src/pages/run.test.tsx`
Expected: PASS (`Tests  50 passed (50)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/components/log-viewer.tsx web/src/components/log-viewer.test.tsx
git commit -m "perf(web): defer the log search behind typing"
```

### Task 40: Let the browser skip off-screen log lines

**Files:**
- Modify: `web/src/components/log-viewer.tsx`
- Test: `web/src/components/log-viewer.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 77
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

A step shows its last 5000 lines, and the browser lays out and paints all of them even when a few dozen are on screen. This task gives every line row the CSS `content-visibility: auto`, so the browser skips rows that are off screen, and `contain-intrinsic-size: auto 1.25rem`, the height the browser assumes for a skipped row: 1.25rem is one line (the log's `leading-5`), and `auto` makes it keep a wrapped line's real height once it has drawn it, so the scrollbar does not jump. Both are written as Tailwind arbitrary-property classes on the row; no stylesheet changes. The 5000-line window stays.

jsdom does no layout, so the test can only pin the two classes. Whether scrolling, the jump to a line and the browser's own find behave on a log of several thousand lines, and whether the outline of the current search match is still fully drawn (`content-visibility: auto` clips a row's painting to its box), is checked in a browser later, with the owner's approval. It is not part of this task.

- [ ] **Step 1: Write the failing test**

In `web/src/components/log-viewer.test.tsx`, add this test directly above the test `shows commands in the primary colour and opens steps on click`:

```tsx
  it("lets the browser skip a line row that is off screen, at one line's height", () => {
    renderViewer()
    expect(lineRow(5, "Lint")).toHaveClass("[content-visibility:auto]", "[contain-intrinsic-size:auto_1.25rem]")
  })
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/log-viewer.test.tsx`
Expected: FAIL, 1 failed and 17 passed, with `Expected the element to have class:` `[content-visibility:auto] [contain-intrinsic-size:auto_1.25rem]` and `Received:` `flex gap-3 px-2 ghr-log-error`

- [ ] **Step 3: Write the implementation**

In `web/src/components/log-viewer.tsx`, in `Line`, replace:

```tsx
      className={`flex gap-3 px-2 ${ROW_CLASS[line.kind] ?? ""} ${target ? "ghr-log-target" : ""}`}
```

with:

```tsx
      className={`flex gap-3 px-2 [contain-intrinsic-size:auto_1.25rem] [content-visibility:auto] ${ROW_CLASS[line.kind] ?? ""} ${target ? "ghr-log-target" : ""}`}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/log-viewer.test.tsx`
Expected: PASS (`Tests  18 passed (18)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/components/log-viewer.tsx web/src/components/log-viewer.test.tsx
git commit -m "perf(web): let the browser skip off-screen lines"
```

### Task 41: Read a job log that has steps once

**Files:**
- Modify: `web/src/api/hooks.ts` (`useJobLog` only)
- Test: `web/src/api/hooks-pages.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: `describe("useJobLog", …)` with the constant `noLog` in `web/src/api/hooks-pages.test.tsx`, which the next task on this hook adds tests to

**Items:** 77
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

`useJobLog` sets no `staleTime`, so the query library treats a job log as stale at once and downloads it again whenever the window gets focus. A finished job's log never changes, and the daemon serves logs of up to 4 MiB. This task gives the query a `staleTime` that is `Infinity` once the answer has steps. An answer without steps (no log, expired, too large) stays stale, so it can still be read again.

Change only `useJobLog` in `web/src/api/hooks.ts` and do not reformat the file. In the test file, add the new block where the step says, not at the end of the file.

The test triggers the query library's focus handling directly, with `queryClient.getQueryCache().onFocus()`. A refetch it starts is counted as fetching before that call returns, so waiting for "nothing is fetching" and then counting the requests is reliable for both rows.

- [ ] **Step 1: Write the failing test**

In `web/src/api/hooks-pages.test.tsx`, replace the lines at the top:

```tsx
import { renderHook, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { LabelCheck, Status, Storage } from "@/api/types"
import { mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { storageBusy, useLabelCheck } from "./hooks"
```

with:

```tsx
import { act, renderHook, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { JobLog, LabelCheck, Status, Storage } from "@/api/types"
import { mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { storageBusy, useJobLog, useLabelCheck } from "./hooks"
```

Add this block directly above `describe("useLabelCheck", () => {`:

```tsx
describe("useJobLog", () => {
  const noLog: JobLog = { ...fixtures.jobLog, no_log: true, steps: [] }

  it.each([
    ["does not read a log that has steps again", fixtures.jobLog, 1],
    ["reads a 'no log' answer again", noLog, 2],
  ])("%s when the window gets focus", async (_name, answer, reads) => {
    const { calls } = mockApi({ "GET /api/actions/darkmem/jobs/1002/log": answer })
    const { queryClient, wrapper } = withQuery()
    const { result } = renderHook(() => useJobLog("darkmem", 1002, true), { wrapper })
    await waitFor(() => expect(result.current.data).toBeDefined())
    // What the query library runs when the window gets focus; a refetch it
    // starts is counted as fetching before this returns.
    act(() => queryClient.getQueryCache().onFocus())
    await waitFor(() => expect(queryClient.isFetching()).toBe(0))
    expect(calls).toHaveLength(reads)
  })
})

```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/api/hooks-pages.test.tsx`
Expected: FAIL, 1 failed and 8 passed: `does not read a log that has steps again when the window gets focus` fails with `AssertionError: expected [ { method: 'GET', …(4) }, …(1) ] to have a length of 1 but got 2`. The `no log` row already passes; it guards the other half of the rule.

- [ ] **Step 3: Write the implementation**

In `web/src/api/hooks.ts`, replace:

```ts
export function useJobLog(repo: string, jobId: number, enabled: boolean) {
  return useQuery({ queryKey: keys.jobLog(repo, jobId), queryFn: ({ signal }) => api.jobLog(repo, jobId, signal), enabled })
}
```

with:

```ts
// A finished job's log never changes once it has steps, so it is read once.
// Any other answer can still become a log and stays stale.
export function useJobLog(repo: string, jobId: number, enabled: boolean) {
  return useQuery({
    queryKey: keys.jobLog(repo, jobId),
    queryFn: ({ signal }) => api.jobLog(repo, jobId, signal),
    enabled,
    staleTime: (q) => (q.state.data && q.state.data.steps.length > 0 ? Infinity : 0),
  })
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/api/hooks-pages.test.tsx src/api/client-pages.test.ts src/pages/run.test.tsx`
Expected: PASS (`Tests  81 passed (81)`). `client-pages.test.ts` also calls `useJobLog`, and the job page's tests read the log through it.

- [ ] **Step 5: Commit**

```bash
git add web/src/api/hooks.ts web/src/api/hooks-pages.test.tsx
git commit -m "perf(web): read a job log with steps once"
```

### Task 42: Ask again for a log that is not there yet (the hook)

**Files:**
- Modify: `web/src/api/hooks.ts` (`useJobLog` only)
- Test: `web/src/api/hooks-pages.test.tsx`

**Interfaces:**
- Consumes: `describe("useJobLog", …)` and its constant `noLog` in `web/src/api/hooks-pages.test.tsx`; `useJobLog`'s `staleTime` (an answer with steps is never stale)
- Produces: `useJobLog(repo: string, jobId: number, enabled: boolean, settling = false)`: with `settling` true, an answer whose `no_log` is true is read again every 15 seconds

**Items:** 280
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

GitHub may not have stored a job's log yet when the job has only just completed. The daemon then answers `no_log: true`, and for a job that completed under two minutes ago it keeps that answer only 15 seconds before it asks GitHub again. The job page never asks the daemon again, so "This job produced no log." stays until the window is refocused.

This task gives `useJobLog` a fourth parameter, `settling`. While it is true and the answer is "no log", the hook reads the log again every 15 seconds. The caller decides what "settling" means (the next task: the job completed under two minutes ago), because the hook has no clock. The parameter defaults to `false` because `web/src/api/client-pages.test.ts` calls the hook with three arguments.

The polling does not depend on staleness: a "no log" answer is stale anyway, and once a log with steps arrives the interval stops because the answer is no longer "no log". The first test shows both.

Change only `useJobLog` in `web/src/api/hooks.ts` and do not reformat the file.

- [ ] **Step 1: Write the failing tests**

In `web/src/api/hooks-pages.test.tsx`, replace:

```tsx
import { describe, expect, it } from "vitest"
```

with:

```tsx
import { afterEach, describe, expect, it, vi } from "vitest"
```

In `describe("useJobLog", …)`, directly below the line `const noLog: JobLog = { ...fixtures.jobLog, no_log: true, steps: [] }`, add:

```tsx

  afterEach(() => {
    vi.useRealTimers()
  })
```

At the end of the same `describe`, after the `it.each` that ends with `"%s when the window gets focus"`, add:

```tsx

  it("reads a 'no log' answer again after 15 seconds while the job settles, until the log is there", async () => {
    vi.useFakeTimers()
    let answer = noLog
    const { calls } = mockApi({ "GET /api/actions/darkmem/jobs/1002/log": () => answer })
    const { wrapper } = withQuery()
    const { result } = renderHook(() => useJobLog("darkmem", 1002, true, true), { wrapper })
    await act(() => vi.advanceTimersByTimeAsync(14_000))
    expect(result.current.data?.no_log).toBe(true)
    expect(calls).toHaveLength(1)
    answer = fixtures.jobLog
    await act(() => vi.advanceTimersByTimeAsync(2000))
    expect(calls).toHaveLength(2)
    expect(result.current.data?.steps).toHaveLength(fixtures.jobLog.steps.length)
    await act(() => vi.advanceTimersByTimeAsync(60_000))
    expect(calls).toHaveLength(2)
  })

  it("stops reading a 'no log' answer again once the job has settled", async () => {
    vi.useFakeTimers()
    const { calls } = mockApi({ "GET /api/actions/darkmem/jobs/1002/log": noLog })
    const { wrapper } = withQuery()
    const { rerender } = renderHook(({ settling }) => useJobLog("darkmem", 1002, true, settling), { wrapper, initialProps: { settling: true } })
    await act(() => vi.advanceTimersByTimeAsync(16_000))
    expect(calls).toHaveLength(2)
    rerender({ settling: false })
    await act(() => vi.advanceTimersByTimeAsync(60_000))
    expect(calls).toHaveLength(2)
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/api/hooks-pages.test.tsx`
Expected: FAIL, 2 failed and 9 passed, both with `AssertionError: expected [ { method: 'GET', …(4) } ] to have a length of 2 but got 1`

- [ ] **Step 3: Write the implementation**

In `web/src/api/hooks.ts`, replace:

```ts
// A finished job's log never changes once it has steps, so it is read once.
// Any other answer can still become a log and stays stale.
export function useJobLog(repo: string, jobId: number, enabled: boolean) {
  return useQuery({
    queryKey: keys.jobLog(repo, jobId),
    queryFn: ({ signal }) => api.jobLog(repo, jobId, signal),
    enabled,
    staleTime: (q) => (q.state.data && q.state.data.steps.length > 0 ? Infinity : 0),
  })
}
```

with:

```ts
// A finished job's log never changes once it has steps, so it is read once.
// Any other answer can still become a log and stays stale. GitHub may not
// have stored the log of a job that has only just completed, and the daemon
// keeps that "no log" answer 15 seconds: while the caller says the job is
// settling, the answer is asked for again at that pace.
export function useJobLog(repo: string, jobId: number, enabled: boolean, settling = false) {
  return useQuery({
    queryKey: keys.jobLog(repo, jobId),
    queryFn: ({ signal }) => api.jobLog(repo, jobId, signal),
    enabled,
    staleTime: (q) => (q.state.data && q.state.data.steps.length > 0 ? Infinity : 0),
    refetchInterval: (q) => (settling && q.state.data?.no_log ? 15_000 : false),
  })
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/api/hooks-pages.test.tsx src/api/client-pages.test.ts`
Expected: PASS (`Tests  51 passed (51)`)

- [ ] **Step 5: Type-check**

Run: `cd web && npx tsc --noEmit`
Expected: no output

- [ ] **Step 6: Commit**

```bash
git add web/src/api/hooks.ts web/src/api/hooks-pages.test.tsx
git commit -m "fix(web): ask again for a log not there yet"
```

### Task 43: Ask again for a log that is not there yet (the job page)

**Files:**
- Modify: `web/src/pages/run.tsx`
- Test: `web/src/pages/run.test.tsx`

**Interfaces:**
- Consumes: `useJobLog(repo: string, jobId: number, enabled: boolean, settling = false)` from `web/src/api/hooks.ts`: with `settling` true, an answer whose `no_log` is true is read again every 15 seconds. In `web/src/pages/run.test.tsx`: the helper `daemonClock` (answers `GET /api/status` with the fake clock's time), the file-level `afterEach` that restores real timers, and `act` imported from `@testing-library/react`.
- Produces: nothing

**Items:** 280
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

The hook can read a "no log" answer again every 15 seconds while its caller says the job is settling. This task makes the job page say so: the job is settling while it completed under two minutes ago, by the page's own clock (`useNow()`, which follows the daemon's clock). Two minutes is the daemon's window (`logSettle` in `internal/daemon/joblogs.go`).

`JobView` already has the clock and the job, whose `completed_at` is the completion time. It works out one boolean and passes it to `JobLogs`, which passes it to the hook. `JobLogs` is memoised; a boolean that changes once does not defeat that.

- [ ] **Step 1: Write the failing test**

In `web/src/pages/run.test.tsx`, in `describe("Job page", …)`, add this directly above the test that saves the raw log (`it("saves the raw log through a Blob", …`):

```tsx
  it.each([
    ["asks again after 15 seconds for the log of a job that completed a minute ago", "2026-10-03T14:04:00Z", 2],
    ["does not ask again for the log of a job that completed three minutes ago", "2026-10-03T14:02:00Z", 1],
  ])("%s", async (_name, completedAt, reads) => {
    vi.useFakeTimers({ shouldAdvanceTime: true, now: new Date("2026-10-03T14:05:00Z") })
    const run = { ...fixtures.runDetail, jobs: fixtures.runDetail.jobs.map((j) => (j.id === 1002 ? { ...j, completed_at: completedAt } : j)) }
    const { calls } = mockApi(
      runRoutes({
        "GET /api/status": daemonClock,
        "GET /api/actions/darkmem/runs/102": run,
        "GET /api/actions/darkmem/jobs/1002/log": { ...fixtures.jobLog, no_log: true, steps: [] },
      }),
    )
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    expect(await screen.findByText("This job produced no log.")).toBeInTheDocument()
    await act(() => vi.advanceTimersByTimeAsync(16_000))
    expect(calls.filter((c) => c.path === "/api/actions/darkmem/jobs/1002/log")).toHaveLength(reads)
  })

```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/pages/run.test.tsx`
Expected: FAIL, 1 failed and 33 passed: the `completed a minute ago` row fails with `AssertionError: expected [ { method: 'GET', …(4) } ] to have a length of 2 but got 1`. The `three minutes ago` row already passes; it keeps the two-minute limit honest once the code is in.

- [ ] **Step 3: Write the implementation**

In `web/src/pages/run.tsx`, replace:

```tsx
// Memoised, and given the step and line as plain values, so the page's
// one-second clock does not render the log again.
const JobLogs = memo(function JobLogs({
  repo,
  job,
  step,
  line,
  ms,
}: {
  repo: string
  job: RunJob
  step: number | undefined
  line: number | undefined
  ms: number | undefined
}) {
  const log = useJobLog(repo, job.id, true)
```

with:

```tsx
// How long after a job completes GitHub may still be storing its log; the
// daemon's logSettle.
const LOG_SETTLE_MS = 2 * 60_000

// Memoised, and given the step and line as plain values, so the page's
// one-second clock does not render the log again.
const JobLogs = memo(function JobLogs({
  repo,
  job,
  step,
  line,
  ms,
  settling,
}: {
  repo: string
  job: RunJob
  step: number | undefined
  line: number | undefined
  ms: number | undefined
  settling: boolean
}) {
  const log = useJobLog(repo, job.id, true, settling)
```

In `JobView`, replace:

```tsx
  const ms = jobMs(job, now)
  return (
```

with:

```tsx
  const ms = jobMs(job, now)
  const settling = job.completed_at !== undefined && now - Date.parse(job.completed_at) < LOG_SETTLE_MS
  return (
```

and replace:

```tsx
            <JobLogs repo={run.repo} job={job} step={search.step} line={search.line} ms={ms} />
```

with:

```tsx
            <JobLogs repo={run.repo} job={job} step={search.step} line={search.line} ms={ms} settling={settling} />
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/run.test.tsx`
Expected: PASS (`Tests  34 passed (34)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/run.tsx web/src/pages/run.test.tsx
git commit -m "fix(web): refetch a fresh job's missing log"
```

### Task 44: Strip DEL and the C1 controls from log lines

**Files:**
- Modify: `web/src/lib/ansi.tsx`
- Test: `web/src/lib/ansi.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 104
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The log's ANSI parser removes escape sequences and the C0 control characters other than tab. It leaves DEL (U+007F) and the C1 control characters (U+0080 to U+009F) in the text, where they show as boxes or nothing and count in a search. This task adds both to the characters the parser removes.

Two known limits stay as they are and are not part of this task: colour does not carry from one log line to the next, and an escape sequence split by a line break leaves its parameters as text.

- [ ] **Step 1: Write the failing test**

In `web/src/lib/ansi.test.tsx`, in the table of the test `removes %s`, add two rows below the row `["C0 controls other than tab", …]`:

```tsx
    ["DEL", "a\u007fb", "ab"],
    ["C1 controls", "a\u0080b\u0085c\u009bd\u009fe\u00a0f", "abcde\u00a0f"],
```

Type every `\u…` as written, six characters each: they are JavaScript escapes, not the characters themselves. U+00A0, the first character after the C1 range, must stay in the text.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/lib/ansi.test.tsx`
Expected: FAIL, 2 failed and 17 passed; the DEL row with `AssertionError: expected 'a\u007fb' to be 'ab' // Object.is equality`

- [ ] **Step 3: Write the implementation**

In `web/src/lib/ansi.tsx`, replace:

```tsx
// line, any other escape, and the C0 controls other than tab.
// eslint-disable-next-line no-control-regex -- log lines carry raw control bytes
const TOKEN = /\x1b\[([0-?]*)[ -/]*([@-~])|\x1b\][\s\S]*?(?:\x07|\x1b\\|$)|\x1b[ -/]*[0-~]?|[\x00-\x08\x0a-\x1f]/g
```

with:

```tsx
// line, any other escape, DEL, the C1 controls, and the C0 controls other
// than tab.
// eslint-disable-next-line no-control-regex -- log lines carry raw control bytes
const TOKEN = /\x1b\[([0-?]*)[ -/]*([@-~])|\x1b\][\s\S]*?(?:\x07|\x1b\\|$)|\x1b[ -/]*[0-~]?|[\x00-\x08\x0a-\x1f\x7f-\x9f]/g
```

The only change in the expression is the last character class, which gains `\x7f-\x9f`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib/ansi.test.tsx`
Expected: PASS (`Tests  19 passed (19)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/ansi.tsx web/src/lib/ansi.test.tsx
git commit -m "fix(web): strip DEL and C1 controls from logs"
```

### Task 45: Keep the raw log's object URL for 40 seconds

**Files:**
- Modify: `web/src/pages/run.tsx`
- Test: `web/src/pages/run.test.tsx`

**Interfaces:**
- Consumes: in `web/src/pages/run.test.tsx`, the file-level `afterEach` that restores real timers and `act` imported from `@testing-library/react`
- Produces: nothing

**Items:** 91
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The Raw log button downloads the log as a Blob, makes a temporary object URL for it, clicks a hidden link to that URL and revokes the URL on the next tick (`setTimeout(…, 0)`). A browser that reads the URL only after the click has returned would find it gone. Whether Safari or Firefox do that is not verified; a longer life is safe either way. This task revokes the URL after 40 seconds.

The existing test of the download waits for the revoke with a real timer and would now wait 40 seconds. It changes to fake timers and to the new behaviour: not revoked after 30 seconds, revoked after 40.

- [ ] **Step 1: Change the test**

In `web/src/pages/run.test.tsx`, replace:

```tsx
  it("saves the raw log through a Blob", async () => {
    const text = "2026-10-03T13:55:25.0000000Z Set up job\n"
```

with:

```tsx
  it("saves the raw log through a Blob and keeps its URL for 40 seconds", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const text = "2026-10-03T13:55:25.0000000Z Set up job\n"
```

In the same test, replace:

```tsx
    await waitFor(() => expect(revoke).toHaveBeenCalledWith("blob:http://localhost/raw"))
    click.mockRestore()
```

with:

```tsx
    await act(() => vi.advanceTimersByTimeAsync(30_000))
    expect(revoke).not.toHaveBeenCalled()
    await act(() => vi.advanceTimersByTimeAsync(10_000))
    expect(revoke).toHaveBeenCalledWith("blob:http://localhost/raw")
    click.mockRestore()
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/pages/run.test.tsx`
Expected: FAIL, 1 failed and 33 passed, with `AssertionError: expected "vi.fn()" to not be called at all, but actually been called 1 times`

- [ ] **Step 3: Write the implementation**

In `web/src/pages/run.tsx`, replace:

```tsx
// The Blob goes through a temporary object URL because a navigation to the
// API cannot send X-GHR. The URL is revoked after the click has started the
// download, not during it.
function saveBlob(blob: Blob, fileName: string) {
```

with:

```tsx
// The Blob goes through a temporary object URL because a navigation to the
// API cannot send X-GHR. The URL outlives the click by RAW_URL_MS, because a
// browser may read it only after the click has returned.
const RAW_URL_MS = 40_000

function saveBlob(blob: Blob, fileName: string) {
```

In `saveBlob`, replace:

```tsx
  setTimeout(() => URL.revokeObjectURL(url), 0)
```

with:

```tsx
  setTimeout(() => URL.revokeObjectURL(url), RAW_URL_MS)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/run.test.tsx`
Expected: PASS (`Tests  34 passed (34)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/run.tsx web/src/pages/run.test.tsx
git commit -m "fix(web): keep the raw log's URL for 40 seconds"
```

### Task 46: Show the daemon's own text for a run or job-log 404

**Files:**
- Modify: `web/src/pages/run.tsx`
- Test: `web/src/pages/run.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 92
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The run page and the job page turn every `404` from the daemon into "This run no longer exists on GitHub." (`runError` in `web/src/pages/run.tsx`). That is wrong when the job is gone but the run is not, and when the repository is no longer configured or watched: the daemon says which (`not found on GitHub`, `repo <name> is not configured or watched`). This task removes `runError`; the job log's error and the run detail's error show the daemon's text through `errorText`, as every other status already does.

The sentence "This run no longer exists on GitHub." stays for an address the page refuses without asking the daemon (a run ID that is not a number, a bad attempt, step or line). That is the constant `GONE`, which this task does not touch.

Two existing tests assert the old text and change with this task.

- [ ] **Step 1: Change the tests**

In `web/src/pages/run.test.tsx`, in `describe("Run page", …)`, replace:

```tsx
  it("says the run is gone when GitHub no longer has it", async () => {
    mockApi(runRoutes({ "GET /api/actions/darkmem/runs/102": () => json({ error: "not found on GitHub" }, 404) }))
    renderApp("/actions/darkmem/runs/102")
    expect(await screen.findByText("This run no longer exists on GitHub.")).toBeInTheDocument()
  })
```

with:

```tsx
  it("shows the daemon's wording when the run is not found", async () => {
    mockApi(runRoutes({ "GET /api/actions/darkmem/runs/102": () => json({ error: "not found on GitHub" }, 404) }))
    renderApp("/actions/darkmem/runs/102")
    expect(await screen.findByText("not found on GitHub")).toBeInTheDocument()
    expect(screen.queryByText("This run no longer exists on GitHub.")).toBeNull()
  })
```

In `describe("Job page", …)`, replace:

```tsx
  it("says the job is gone when GitHub has no log for it", async () => {
    mockApi(runRoutes({ "GET /api/actions/darkmem/jobs/1002/log": () => json({ error: "not found on GitHub" }, 404) }))
    renderApp("/actions/darkmem/runs/102/jobs/1002")
    expect(await screen.findByText("This run no longer exists on GitHub.")).toBeInTheDocument()
  })
```

with:

```tsx
  it.each([["not found on GitHub"], ["repo darkmem is not configured or watched"]])(
    "shows the daemon's wording when the job log is not found: %s",
    async (message) => {
      mockApi(runRoutes({ "GET /api/actions/darkmem/jobs/1002/log": () => json({ error: message }, 404) }))
      renderApp("/actions/darkmem/runs/102/jobs/1002")
      expect(await screen.findByText(message)).toBeInTheDocument()
      expect(screen.queryByText("This run no longer exists on GitHub.")).toBeNull()
    },
  )
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/pages/run.test.tsx`
Expected: FAIL, 3 failed and 32 passed, with `Unable to find an element with the text: not found on GitHub.` twice and `Unable to find an element with the text: repo darkmem is not configured or watched.` once (the full stop is the test library's, not part of the text)

- [ ] **Step 3: Write the implementation**

In `web/src/pages/run.tsx`, replace:

```tsx
import { ApiError, api } from "@/api/client"
```

with:

```tsx
import { api } from "@/api/client"
```

Replace:

```tsx
const GONE = "This run no longer exists on GitHub."

function runError(err: unknown): string {
  return err instanceof ApiError && err.status === 404 ? GONE : errorText(err)
}
```

with:

```tsx
const GONE = "This run no longer exists on GitHub."
```

In `RunGate`, replace:

```tsx
          <ErrorLine onRetry={() => void run.refetch()}>{runError(run.error)}</ErrorLine>
        ) : (
```

with:

```tsx
          <ErrorLine onRetry={() => void run.refetch()}>{errorText(run.error)}</ErrorLine>
        ) : (
```

and replace:

```tsx
      {run.isError && <ErrorLine onRetry={() => void run.refetch()}>{runError(run.error)}</ErrorLine>}
```

with:

```tsx
      {run.isError && <ErrorLine onRetry={() => void run.refetch()}>{errorText(run.error)}</ErrorLine>}
```

In `JobLogs`, replace:

```tsx
    return log.isError ? <ErrorLine onRetry={() => void log.refetch()}>{runError(log.error)}</ErrorLine> : <Spinner label="Loading" />
```

with:

```tsx
    return log.isError ? <ErrorLine onRetry={() => void log.refetch()}>{errorText(log.error)}</ErrorLine> : <Spinner label="Loading" />
```

`errorText` is already imported from `@/query`. No use of `runError` or `ApiError` is left in the file.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/run.test.tsx`
Expected: PASS (`Tests  35 passed (35)`)

- [ ] **Step 5: Type-check and lint**

Run: `cd web && npx tsc --noEmit` and then `cd web && npx eslint src/pages --max-warnings 0`
Expected: neither prints an error or a warning. A leftover `ApiError` import would be reported as unused.

- [ ] **Step 6: Commit**

```bash
git add web/src/pages/run.tsx web/src/pages/run.test.tsx
git commit -m "fix(web): show the daemon's text for a run 404"
```

### Task 47: Test a breadcrumb with a query string through the real router

**Files:**
- Test: `web/src/pages/run.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 108
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 0 - spec 0 - coupling 0 - risk 0 = 0

The run page's breadcrumbs are built in `web/src/components/run-frame.tsx` as plain addresses, two of them with a query string: the repository (`/actions?repo=<name>`) and, on a job page, the run number (`/actions/<repo>/runs/<id>?attempt=<n>`). They are rendered by the UI kit through the app's router link. The component's own tests replace the router with plain anchors, so nothing tests that the real router splits such an address into a path and a search. This task adds that test. It changes no production code.

The test is expected to pass as written: it was run against the real router on 2026-10-12 and passed. If it fails, do not change `web/src/components/run-frame.tsx` or the router to make it pass: stop and report the failure, because the defect is then in code this task does not own.

- [ ] **Step 1: Write the test**

In `web/src/pages/run.test.tsx`, in `describe("Run page", …)`, add this directly above the `it.each` whose first row is `["a run ID that is not a number", "/actions/darkmem/runs/abc"]`:

```tsx
  it.each([
    ["the repository on a run page", "/actions/darkmem/runs/102", "darkmem", "/actions?repo=darkmem", "/actions", { repo: "darkmem" }],
    [
      "the run number on a job page",
      "/actions/darkmem/runs/102/jobs/1002",
      "#42",
      "/actions/darkmem/runs/102?attempt=2",
      "/actions/darkmem/runs/102",
      { attempt: 2 },
    ],
  ])("follows the breadcrumb of %s to its path and search", async (_name, from, label, href, pathname, search) => {
    mockApi(runRoutes())
    const { user, router } = renderApp(from)
    // The loading page has breadcrumbs too; the run's come with its title.
    await screen.findByRole("heading", { name: "Fix the cache key", level: 1 })
    const crumbs = within(screen.getByRole("navigation", { name: "breadcrumb" }))
    const crumb = crumbs.getByRole("link", { name: label })
    expect(crumb).toHaveAttribute("href", href)
    await user.click(crumb)
    await waitFor(() => expect(router.state.location.pathname).toBe(pathname))
    expect(router.state.location.search).toEqual(search)
  })

```

A router that took the whole address as a path would leave the pathname at `/actions?repo=darkmem` (or an encoded form of it) and the search empty, and both assertions would fail.

- [ ] **Step 2: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/run.test.tsx`
Expected: PASS (`Tests  37 passed (37)`)

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/run.test.tsx
git commit -m "test(web): follow a breadcrumb with a query"
```

### Task 48: Error text by content type, and empty successful bodies

**Files:**
- Modify: `web/src/api/client.ts`
- Test: `web/src/api/client.test.ts`

**Interfaces:**
- Consumes: nothing
- Produces: the message rule of `ApiError` for a failed response; `request` resolving `undefined` for any successful response with an empty body

**Items:** 81, 102, 108
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 2 = 3

A failed response's whole body becomes the error message unless it is JSON with an `error` field. A proxy that answers with an HTML error page therefore fills a toast with markup. After this task the message is: the JSON `error` when the body is JSON and has one (whatever the content type says, as today); otherwise the body when the response is `text/plain`, cut at 200 characters; otherwise the status code and status text, for example `502 Bad Gateway`. An empty `text/plain` body also gives the status line.

Only `202` and `204` count as answers without a body today, so a `200` with an empty body makes a successful action throw a JSON parse error. After this task any successful response with an empty body resolves to `undefined`.

- [ ] **Step 1: Write the failing test**

In `web/src/api/client.test.ts`, inside `describe("api client", ...)`, insert directly below the test `keeps a plain-text error body as the message`:

```ts
  it.each<[string, string, string, string]>([
    ["an HTML body", "<html><body><h1>502 Bad Gateway</h1><p>nginx</p></body></html>", "text/html", "502 Bad Gateway"],
    ["JSON with no error field", '{"detail":"upstream timed out"}', "application/json", "502 Bad Gateway"],
    ["an empty plain-text body", "", "text/plain", "502 Bad Gateway"],
    ["a long plain-text body", "x".repeat(300), "text/plain; charset=utf-8", "x".repeat(200)],
    ["JSON with an error field and the wrong type", '{"error":"upstream is down"}', "text/html", "upstream is down"],
  ])("reads the error message from %s", async (_name, body, contentType, want) => {
    mockApi({
      "GET /api/status": () => new Response(body, { status: 502, statusText: "Bad Gateway", headers: { "Content-Type": contentType } }),
    })
    await expect(api.status()).rejects.toMatchObject({ status: 502, message: want })
  })

  it("accepts an empty body on a 200", async () => {
    mockApi({ "POST /api/pause-all": () => new Response(null, { status: 200 }) })
    await expect(api.pauseAll()).resolves.toBeUndefined()
  })
```

The existing test above it stays as it is: a `Response` built from a string carries `text/plain;charset=UTF-8`, so `404 page not found` is still the message.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/api/client.test.ts`
Expected: FAIL. The rows `an HTML body`, `JSON with no error field` and `a long plain-text body` fail with `AssertionError: expected ApiError: {"detail":"upstream timed out"} { …(2) } to match object { status: 502, …(1) }` (each with its own body in place of the JSON), and `accepts an empty body on a 200` fails with `AssertionError: promise rejected "SyntaxError: Unexpected end of JSON input" instead of resolving`. The rows `an empty plain-text body` and `JSON with an error field and the wrong type` pass already; they pin behaviour that must not change.

- [ ] **Step 3: Write the implementation**

In `web/src/api/client.ts`, in `request`, replace:

```ts
  if (res.ok) {
    if (res.status === 202 || res.status === 204) return undefined as T
    return (await res.json()) as T
  }
```

with:

```ts
  if (res.ok) {
    const text = await res.text()
    return (text === "" ? undefined : JSON.parse(text)) as T
  }
```

In the same file, replace the whole `toError` function:

```ts
async function toError(res: Response): Promise<ApiError> {
  const text = await res.text()
  let message = text.trim() || `${res.status} ${res.statusText}`.trim()
  let retryAt: Date | undefined
  try {
    const parsed = JSON.parse(text) as { error?: unknown; retry_at?: unknown }
    if (typeof parsed.error === "string") message = parsed.error
    if (typeof parsed.retry_at === "string") retryAt = new Date(parsed.retry_at)
  } catch {
    // a plain-text body keeps its text as the message
  }
  return new ApiError(res.status, message, retryAt)
}
```

with:

```ts
const PLAIN_ERROR_MAX = 200

// A body that is neither the daemon's JSON nor plain text is a page from
// something in between, a proxy's HTML for example, and is not shown.
async function toError(res: Response): Promise<ApiError> {
  const text = await res.text()
  const plain = (res.headers.get("Content-Type") ?? "").toLowerCase().startsWith("text/plain")
  let message = (plain && text.trim().slice(0, PLAIN_ERROR_MAX)) || `${res.status} ${res.statusText}`.trim()
  let retryAt: Date | undefined
  try {
    const parsed = JSON.parse(text) as { error?: unknown; retry_at?: unknown }
    if (typeof parsed.error === "string") message = parsed.error
    if (typeof parsed.retry_at === "string") retryAt = new Date(parsed.retry_at)
  } catch {
    // not JSON: the message above stands
  }
  return new ApiError(res.status, message, retryAt)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/api/client.test.ts src/api/client-pages.test.ts`
Expected: PASS (`Tests  54 passed (54)`; `client.test.ts` alone is `Tests  14 passed (14)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/api/client.ts web/src/api/client.test.ts
git commit -m "fix(web): read error text by content type"
```

### Task 49: Request timeouts

**Files:**
- Modify: `web/src/api/client.ts`
- Test: `web/src/api/client.test.ts`

**Interfaces:**
- Consumes: the body of `request` as Task 48 left it
- Produces: an optional fifth parameter `timeoutMs` on `request`; the error text `the daemon did not answer`

**Items:** 85
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

No request the web UI sends has a time limit. A request the daemon never answers stays pending for ever, and because the query library reuses a request in flight, every later poll of that query waits on it too.

After this task a request fails after 70 seconds with a plain `Error` whose message is `the daemon did not answer`. The daemon's web listener cuts a response after 60 seconds, so nothing useful arrives later than that. Stop runner gets 110 seconds, because its route lets a stop take 100. The raw-log download uses the 70 seconds. The limit covers reading the body, not only the headers. An abort through the caller's own signal (the query library cancelling a query) is passed through unchanged.

The timer is a plain `setTimeout` on a controller chained to the caller's signal. `AbortSignal.timeout` is not used: it does not follow vitest's fake timers, so the limit could not be tested without waiting 70 real seconds.

- [ ] **Step 1: Write the failing test**

In `web/src/api/client.test.ts`, replace the first line:

```ts
import { afterEach, describe, expect, it, vi } from "vitest"
```

with:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
```

Then append at the end of the file, after the closing `})` of `describe("api client", ...)`:

```ts

const aborted = () => new DOMException("The operation was aborted.", "AbortError")

// Stands in for a daemon that never answers: like the browser's fetch, the
// promise rejects when the request's signal aborts.
function silentFetch() {
  vi.stubGlobal(
    "fetch",
    vi.fn(
      (_input: RequestInfo | URL, init?: RequestInit) =>
        new Promise<Response>((_resolve, reject) => {
          init?.signal?.addEventListener("abort", () => reject(aborted()))
        }),
    ),
  )
}

describe("request timeouts", () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  const settle = (call: Promise<unknown>) => {
    const out: { error?: unknown } = {}
    call.catch((err: unknown) => {
      out.error = err
    })
    return out
  }

  it.each<[string, number, () => Promise<unknown>]>([
    ["a read", 70_000, () => api.status()],
    ["an action", 70_000, () => api.pauseAll()],
    ["the raw-log download", 70_000, () => api.rawJobLog("darkmem", 2)],
    ["Stop runner", 110_000, () => api.stopRunner("aaaaaa")],
  ])("gives up on %s after %i ms of silence", async (_name, ms, call) => {
    silentFetch()
    const out = settle(call())
    await vi.advanceTimersByTimeAsync(ms - 1)
    expect(out.error).toBeUndefined()
    await vi.advanceTimersByTimeAsync(1)
    expect(out.error).toEqual(new Error("the daemon did not answer"))
    expect(out.error).not.toBeInstanceOf(ApiError)
  })

  it("keeps counting while a body that started does not end", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
        const body = new ReadableStream<Uint8Array>({
          start(c) {
            c.enqueue(new TextEncoder().encode('{"epoch":'))
            init?.signal?.addEventListener("abort", () => c.error(aborted()))
          },
        })
        return new Response(body, { status: 200 })
      }),
    )
    const out = settle(api.status())
    await vi.advanceTimersByTimeAsync(70_000)
    expect(out.error).toEqual(new Error("the daemon did not answer"))
  })

  it("leaves the caller's own abort as it is", async () => {
    silentFetch()
    const ctrl = new AbortController()
    const out = settle(api.status(ctrl.signal))
    ctrl.abort()
    await vi.advanceTimersByTimeAsync(0)
    expect(out.error).toMatchObject({ name: "AbortError" })
    expect(vi.getTimerCount()).toBe(0)
  })

  it("leaves no timer behind an answered request", async () => {
    mockApi({ "GET /api/status": { epoch: "e" } })
    await api.status()
    expect(vi.getTimerCount()).toBe(0)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/api/client.test.ts`
Expected: FAIL with `AssertionError: expected undefined to deeply equal Error: the daemon did not answer` in the four `gives up on …` rows and in `keeps counting while a body that started does not end` (`Tests  5 failed | 16 passed (21)`). The last two tests pass already; they guard the change.

- [ ] **Step 3: Write the implementation**

In `web/src/api/client.ts`, replace the `request` and `requestBlob` functions:

```ts
export async function request<T>(method: string, path: string, body?: unknown, signal?: AbortSignal): Promise<T> {
  const headers: Record<string, string> = { "X-GHR": "1" }
  let payload: string | undefined
  if (typeof body === "string") {
    headers["Content-Type"] = "text/plain"
    payload = body
  } else if (body !== undefined) {
    headers["Content-Type"] = "application/json"
    payload = JSON.stringify(body)
  }
  const res = await fetch(path, { method, headers, credentials: "same-origin", body: payload, signal })
  if (res.ok) {
    const text = await res.text()
    return (text === "" ? undefined : JSON.parse(text)) as T
  }
  const err = await toError(res)
  // Only an expired session on the API means "log in again"; a 401 from
  // /auth/login or /auth/password is a wrong password the form shows.
  if (res.status === 401 && path.startsWith("/api/")) onUnauthorized()
  throw err
}

// A download whose body is the file itself, not JSON.
async function requestBlob(path: string): Promise<Blob> {
  const res = await fetch(path, { method: "GET", headers: { "X-GHR": "1" }, credentials: "same-origin" })
  if (res.ok) return res.blob()
  const err = await toError(res)
  if (res.status === 401) onUnauthorized()
  throw err
}
```

with:

```ts
// The daemon's web listener cuts a response after 60 seconds, so an answer
// that has not come by then never will.
const REQUEST_TIMEOUT = 70_000
// The stop route lets a stop take 100 seconds.
const STOP_TIMEOUT = 110_000

// Runs one request under a time limit that covers the body as well as the
// headers. An abort through the caller's signal passes through as it is.
async function withTimeout<T>(ms: number, signal: AbortSignal | undefined, run: (signal: AbortSignal) => Promise<T>): Promise<T> {
  const ctrl = new AbortController()
  let timedOut = false
  const timer = setTimeout(() => {
    timedOut = true
    ctrl.abort()
  }, ms)
  const cancel = () => ctrl.abort(signal?.reason)
  signal?.addEventListener("abort", cancel)
  if (signal?.aborted) cancel()
  try {
    return await run(ctrl.signal)
  } catch (err) {
    if (timedOut) throw new Error("the daemon did not answer")
    throw err
  } finally {
    clearTimeout(timer)
    signal?.removeEventListener("abort", cancel)
  }
}

export async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  signal?: AbortSignal,
  timeoutMs = REQUEST_TIMEOUT,
): Promise<T> {
  const headers: Record<string, string> = { "X-GHR": "1" }
  let payload: string | undefined
  if (typeof body === "string") {
    headers["Content-Type"] = "text/plain"
    payload = body
  } else if (body !== undefined) {
    headers["Content-Type"] = "application/json"
    payload = JSON.stringify(body)
  }
  return withTimeout(timeoutMs, signal, async (limited) => {
    const res = await fetch(path, { method, headers, credentials: "same-origin", body: payload, signal: limited })
    if (res.ok) {
      const text = await res.text()
      return (text === "" ? undefined : JSON.parse(text)) as T
    }
    const err = await toError(res)
    // Only an expired session on the API means "log in again"; a 401 from
    // /auth/login or /auth/password is a wrong password the form shows.
    if (res.status === 401 && path.startsWith("/api/")) onUnauthorized()
    throw err
  })
}

// A download whose body is the file itself, not JSON.
async function requestBlob(path: string): Promise<Blob> {
  return withTimeout(REQUEST_TIMEOUT, undefined, async (limited) => {
    const res = await fetch(path, { method: "GET", headers: { "X-GHR": "1" }, credentials: "same-origin", signal: limited })
    if (res.ok) return res.blob()
    const err = await toError(res)
    if (res.status === 401) onUnauthorized()
    throw err
  })
}
```

In the same file, replace:

```ts
const send = (method: string, path: string, body?: unknown): Promise<void> => request<undefined>(method, path, body)
```

with:

```ts
const send = (method: string, path: string, body?: unknown, timeoutMs?: number): Promise<void> =>
  request<undefined>(method, path, body, undefined, timeoutMs)
```

and in the `api` object replace:

```ts
  stopRunner: (id: string) => send("DELETE", `/api/runners/${seg(id)}`),
```

with:

```ts
  stopRunner: (id: string) => send("DELETE", `/api/runners/${seg(id)}`, undefined, STOP_TIMEOUT),
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/api/client.test.ts src/api/client-pages.test.ts src/components/stop-runner-dialog.test.tsx`
Expected: PASS (`Tests  62 passed (62)`; `client.test.ts` alone is `Tests  21 passed (21)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/api/client.ts web/src/api/client.test.ts
git commit -m "fix(web): time out a request the daemon ignores"
```

### Task 50: A watchdog on the change stream

**Files:**
- Modify: `web/src/api/live.ts`
- Test: `web/src/api/live.test.ts`

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names (`readChanges` keeps its signature and still returns `"ended"`)

**Items:** 85
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 2 = 3

The browser reads the daemon's change stream (`GET /api/changes`) with `readChanges`. Nothing notices a connection that has gone silent: a dead socket the browser still believes open leaves the page "live", with its polls slowed to one a minute, for as long as the tab stays open.

The server writes a ping every 15 seconds when it has nothing else to send. After this task `readChanges` ends its attempt after 45 seconds without a byte, counted from the start of the request, from the response's headers and from each chunk. The ended attempt returns `"ended"` like any other end, so `runChanges` takes its existing path: it calls `onEnd`, waits and reconnects.

- [ ] **Step 1: Write the failing test**

In `web/src/api/live.test.ts`, replace:

```ts
import { afterEach, describe, expect, it, vi } from "vitest"
```

with:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
```

Then, inside `describe("readChanges", ...)`, insert directly below its last test, `cancels the reader on abort`:

```ts

  describe("when the stream goes silent", () => {
    beforeEach(() => {
      vi.useFakeTimers()
    })

    afterEach(() => {
      vi.useRealTimers()
    })

    const reading = (handlers = recorder().handlers) => {
      const out: { end?: string } = {}
      void readChanges(handlers, new AbortController().signal).then((end) => {
        out.end = end
      })
      return out
    }

    it("ends the attempt 45 seconds after the last byte", async () => {
      let send: (text: string) => void = () => {}
      let cancelled = false
      mockApi({
        "GET /api/changes": () =>
          new Response(
            new ReadableStream<Uint8Array>({
              start(c) {
                send = (text) => c.enqueue(enc.encode(text))
              },
              cancel() {
                cancelled = true
              },
            }),
          ),
      })
      const { seen, handlers } = recorder()
      const out = reading(handlers)
      await vi.advanceTimersByTimeAsync(0)
      send("event: hello\ndata: {}\n\n")
      await vi.advanceTimersByTimeAsync(30_000)
      send(": ping\n\n")
      await vi.advanceTimersByTimeAsync(44_999)
      expect(out.end).toBeUndefined()
      await vi.advanceTimersByTimeAsync(1)
      expect(out.end).toBe("ended")
      expect(cancelled).toBe(true)
      expect(seen).toEqual(["hello", "alive"])
      expect(vi.getTimerCount()).toBe(0)
    })

    it("ends an attempt whose response never starts", async () => {
      vi.stubGlobal(
        "fetch",
        vi.fn(
          (_input: RequestInfo | URL, init?: RequestInit) =>
            new Promise<Response>((_resolve, reject) => {
              init?.signal?.addEventListener("abort", () => reject(new DOMException("The operation was aborted.", "AbortError")))
            }),
        ),
      )
      const out = reading()
      await vi.advanceTimersByTimeAsync(44_999)
      expect(out.end).toBeUndefined()
      await vi.advanceTimersByTimeAsync(1)
      expect(out.end).toBe("ended")
    })
  })
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/api/live.test.ts`
Expected: FAIL in both new tests with `AssertionError: expected undefined to be 'ended' // Object.is equality` (`Tests  2 failed | 15 passed (17)`)

- [ ] **Step 3: Write the implementation**

In `web/src/api/live.ts`, replace:

```ts
// readChanges reads one connection to the change stream until it ends. An
// abort through signal cancels the reader and ends it like any other end.
export async function readChanges(h: ChangeHandlers, signal: AbortSignal): Promise<StreamEnd> {
  let res: Response
  try {
    res = await fetch("/api/changes", { headers: { "X-GHR": "1" }, credentials: "same-origin", signal })
  } catch {
    return "ended"
  }
  if (res.status === 401) {
```

with:

```ts
// The server writes a ping every 15 seconds while it has nothing to say, so
// this much silence means the connection is dead although it looks open.
const STREAM_SILENCE = 45_000

// readChanges reads one connection to the change stream until it ends. An
// abort through signal, or STREAM_SILENCE without a byte, cancels the reader
// and ends it like any other end.
export async function readChanges(h: ChangeHandlers, signal: AbortSignal): Promise<StreamEnd> {
  const attempt = new AbortController()
  const stop = () => attempt.abort()
  signal.addEventListener("abort", stop)
  if (signal.aborted) stop()
  let watchdog = setTimeout(stop, STREAM_SILENCE)
  const heard = () => {
    clearTimeout(watchdog)
    watchdog = setTimeout(stop, STREAM_SILENCE)
  }
  try {
    return await readAttempt(h, attempt.signal, heard)
  } finally {
    clearTimeout(watchdog)
    signal.removeEventListener("abort", stop)
  }
}

async function readAttempt(h: ChangeHandlers, signal: AbortSignal, heard: () => void): Promise<StreamEnd> {
  let res: Response
  try {
    res = await fetch("/api/changes", { headers: { "X-GHR": "1" }, credentials: "same-origin", signal })
  } catch {
    return "ended"
  }
  heard()
  if (res.status === 401) {
```

The rest of the old function body is now the body of `readAttempt` and stays as it is, except for one line. In it, replace:

```ts
      if (chunk.done) break
      try {
        parser.push(decoder.decode(chunk.value, { stream: true }))
```

with:

```ts
      if (chunk.done) break
      heard()
      try {
        parser.push(decoder.decode(chunk.value, { stream: true }))
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/api/live.test.ts src/api/use-change-stream.test.tsx`
Expected: PASS (`Tests  29 passed (29)`; `live.test.ts` alone is `Tests  17 passed (17)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/api/live.ts web/src/api/live.test.ts
git commit -m "fix(web): end a change stream that goes silent"
```

### Task 51: The missed flag of the change stream

**Files:**
- Modify: `web/src/api/use-change-stream.ts`
- Test: `web/src/api/use-change-stream.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: the rule that a hello refetches every stream-backed query after any gap, which Task 52 relies on

**Items:** 33
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The change stream's first message, the hello, carries no snapshot and no sequence number, so the browser cannot tell what it missed while no stream was open. `useChangeStream` refetches everything only on a hello that follows an earlier hello (its `connected` flag). A first attempt that fails, because the daemon is restarting or already serves 32 subscribers, is therefore followed by a hello that refetches nothing, although the data on screen may be a poll interval old.

After this task the hook keeps a `missed` flag instead. It is set when the page loads hidden, when an attempt ends and when the stream is stopped for a hidden tab. A hello refetches everything when the flag is set, then clears it. The first hello of a page that loaded visible still refetches nothing; the existing test `invalidates per topic, is not live while hidden, and refreshes everything after a reconnect` pins that and must keep passing unchanged.

- [ ] **Step 1: Write the failing test**

In `web/src/api/use-change-stream.test.tsx`, replace:

```ts
import { afterEach, beforeEach, describe, expect, it } from "vitest"
```

with:

```ts
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
```

and replace:

```ts
import { liveConfig, setLive, useLive } from "./live"
```

with:

```ts
import { liveConfig, RETRY_MIN, setLive, useLive } from "./live"
```

Then, inside `describe("useChangeStream", ...)`, insert directly above the test `opens nothing while the stream is turned off`:

```tsx
  it("refreshes everything on the hello that follows a failed first attempt", async () => {
    vi.useFakeTimers()
    try {
      const stream = controlled()
      let n = 0
      const { calls } = mockApi({ "GET /api/changes": () => (n++ === 0 ? json({ error: "not ready" }, 503) : stream.response) })
      const { queryClient, wrapper } = withQuery()
      seed(queryClient)
      renderHook(() => useChangeStream(), { wrapper })
      await act(() => vi.advanceTimersByTimeAsync(0))
      expect(calls.filter((c) => c.path === "/api/changes")).toHaveLength(1)
      await act(() => vi.advanceTimersByTimeAsync(RETRY_MIN))
      expect(calls.filter((c) => c.path === "/api/changes")).toHaveLength(2)
      stream.send("event: hello\ndata: {}\n\n")
      await act(() => vi.advanceTimersByTimeAsync(0))
      expect(invalidated(queryClient)).toEqual(ALL)
    } finally {
      vi.useRealTimers()
    }
  })

  it("refreshes everything on the first hello of a page that loaded hidden", async () => {
    visibility = "hidden"
    const stream = controlled()
    const { calls } = mockApi({ "GET /api/changes": () => stream.response })
    const { queryClient, wrapper } = withQuery()
    seed(queryClient)
    const { result } = renderHook(
      () => {
        useChangeStream()
        return useLive()
      },
      { wrapper },
    )
    expect(calls).toHaveLength(0)
    act(() => setVisibility("visible"))
    await waitFor(() => expect(calls.filter((c) => c.path === "/api/changes")).toHaveLength(1))
    stream.send("event: hello\ndata: {}\n\n")
    await waitFor(() => expect(result.current).toBe(true))
    expect(invalidated(queryClient)).toEqual(ALL)
  })

```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/api/use-change-stream.test.tsx`
Expected: FAIL in both new tests with `AssertionError: expected [] to deeply equal [ Array(8) ]` (`Tests  2 failed | 12 passed (14)`)

- [ ] **Step 3: Write the implementation**

In `web/src/api/use-change-stream.ts`, in `useChangeStream`, replace:

```ts
    // Set by the first hello; a later hello follows a gap that may have
    // missed changes.
    let connected = false
```

with:

```ts
    // True while a change may have gone unannounced: the page loaded hidden,
    // an attempt ended, or the stream was stopped for a hidden tab. The hello
    // carries no snapshot, so the next one refetches everything.
    let missed = document.visibilityState === "hidden"
```

Replace:

```ts
            if (connected) invalidateAll(queryClient)
            connected = true
            setLive(true)
          },
          onChange: (topics) => {
            if (mine()) invalidateTopics(queryClient, topics)
          },
          onEnd: () => {
            if (mine()) setLive(false)
          },
```

with:

```ts
            if (missed) invalidateAll(queryClient)
            missed = false
            setLive(true)
          },
          onChange: (topics) => {
            if (mine()) invalidateTopics(queryClient, topics)
          },
          onEnd: () => {
            if (!mine()) return
            missed = true
            setLive(false)
          },
```

Replace:

```ts
    const stop = () => {
      current?.abort()
      current = undefined
      setLive(false)
    }
```

with:

```ts
    const stop = () => {
      current?.abort()
      current = undefined
      missed = true
      setLive(false)
    }
```

`stop` sets the flag itself because it clears `current` first, so the aborted loop's `onEnd` no longer passes `mine()`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/api/use-change-stream.test.tsx`
Expected: PASS (`Tests  14 passed (14)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/api/use-change-stream.ts web/src/api/use-change-stream.test.tsx
git commit -m "fix(web): refetch on a hello after any stream gap"
```

### Task 52: One refetch when a tab is shown again

**Files:**
- Modify: `web/src/query.ts`
- Create: `web/src/query.test.tsx`
- Test: `web/src/api/use-change-stream.test.tsx`

**Interfaces:**
- Consumes: the missed flag of Task 51 (a hello refetches everything after the stream was stopped for a hidden tab, and on the first hello of a page that loaded hidden)
- Produces: `createQueryClient()` gives the eight stream-backed query keys the defaults `refetchOnWindowFocus: false` and `refetchOnReconnect: false`

**Items:** 86
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

Eight queries are kept fresh by the change stream: status, config, metrics, token, storage, activity, history and events. Showing a hidden tab again fetches each of them twice: the query library refetches stale queries when the window regains focus, and the stream's hello then invalidates them all. A network return does the same (checked in the installed `@tanstack/query-core` 5.104.1: the first test of this task prints `[ 1, 2, 3 ]` before the change, one more request after the focus and one more after the reconnect).

After this task the app's query client turns both library refetches off for those eight keys, so the hello is the one trigger. The keys are taken from `TOPIC_QUERIES` in `web/src/api/use-change-stream.ts`, the same table the hello uses to invalidate, so a query stops refetching on focus exactly when the hello refetches it. The hooks are not edited. Where no stream runs (the setup wizard opens none), the queries' interval polls carry the page; the third stream test pins that. Queries the stream does not cover, such as the Actions list, keep both library refetches.

`setVisibility` in the stream's test file dispatches a `visibilitychange` that does not bubble, which reaches the stream hook's listener on `document` but not the query library's listener on `window`. The browser's event bubbles. This task makes the helper's event bubble; the existing tests are otherwise unchanged.

- [ ] **Step 1: Write the failing test for the query client**

Create `web/src/query.test.tsx`:

```tsx
import { focusManager, onlineManager, QueryClientProvider } from "@tanstack/react-query"
import { act, renderHook } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { useActions, useActivity, useConfig, useEvents, useHistory, useMetrics, useStatus, useStorage, useToken } from "@/api/hooks"
import { mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { trackQueryClient } from "@/test/query-clients"
import { createQueryClient } from "./query"

function withAppClient() {
  const queryClient = trackQueryClient(createQueryClient())
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  return { queryClient, wrapper }
}

const routes = () => authedRoutes({ "GET /api/token": fixtures.token, "GET /api/history": [] })
const flush = () => act(() => vi.advanceTimersByTimeAsync(0))

describe("createQueryClient", () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    focusManager.setFocused(undefined)
    onlineManager.setOnline(true)
    vi.useRealTimers()
  })

  // Shows the tab again, then brings the network back, and counts the
  // requests for path after each.
  async function refetches(path: string, use: () => unknown): Promise<number[]> {
    const { calls } = mockApi(routes())
    const { wrapper } = withAppClient()
    renderHook(use, { wrapper })
    await flush()
    const fetched = () => calls.filter((c) => c.path === path).length
    const counts = [fetched()]
    focusManager.setFocused(false)
    focusManager.setFocused(true)
    await flush()
    counts.push(fetched())
    onlineManager.setOnline(false)
    onlineManager.setOnline(true)
    await flush()
    counts.push(fetched())
    return counts
  }

  it.each<[string, string, () => unknown]>([
    ["useStatus", "/api/status", () => useStatus()],
    ["useConfig", "/api/config", () => useConfig()],
    ["useMetrics", "/api/metrics", () => useMetrics()],
    ["useToken", "/api/token", () => useToken()],
    ["useStorage", "/api/storage", () => useStorage(undefined)],
    ["useActivity", "/api/activity", () => useActivity("1h")],
    ["useHistory", "/api/history", () => useHistory("", "", "2026-10-01T00:00:00Z")],
    ["useEvents", "/api/events", () => useEvents("e1")],
  ])("leaves %s to the change stream on a re-shown tab and a network return", async (_name, path, use) => {
    expect(await refetches(path, use)).toEqual([1, 1, 1])
  })

  it("still refetches a query the stream does not cover", async () => {
    expect(await refetches("/api/actions", () => useActions())).toEqual([1, 2, 3])
  })
})
```

- [ ] **Step 2: Write the failing tests for the stream and the client together**

In `web/src/api/use-change-stream.test.tsx`, replace the import block at the top:

```ts
import type { QueryClient } from "@tanstack/react-query"
import { act, renderHook, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { json, mockApi } from "@/test/api"
import { withQuery } from "@/test/query"
import { keys } from "./hooks"
import { liveConfig, RETRY_MIN, setLive, useLive } from "./live"
```

with:

```ts
import { QueryClientProvider, type QueryClient } from "@tanstack/react-query"
import { act, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { createQueryClient } from "@/query"
import { json, mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { trackQueryClient } from "@/test/query-clients"
import { keys, POLL_SLOW, useActivity, useConfig, useEvents, useHistory, useMetrics, useStatus, useStorage, useToken } from "./hooks"
import { liveConfig, RETRY_MIN, setLive, useLive } from "./live"
```

Replace the helper:

```ts
function setVisibility(v: DocumentVisibilityState) {
  visibility = v
  document.dispatchEvent(new Event("visibilitychange"))
}
```

with:

```ts
// The event bubbles to window, as the browser's does: the query library
// listens there for its refetch on focus.
function setVisibility(v: DocumentVisibilityState) {
  visibility = v
  document.dispatchEvent(new Event("visibilitychange", { bubbles: true }))
}
```

Then, inside `describe("useChangeStream", ...)`, insert directly below its last test, `opens nothing while the stream is turned off`:

```tsx

  describe("with the app's query client", () => {
    const PATHS = ["/api/status", "/api/config", "/api/metrics", "/api/token", "/api/storage", "/api/activity", "/api/history", "/api/events"]
    const each = (n: number) => PATHS.map(() => n)
    const HELLO = "event: hello\ndata: {}\n\n"

    function useStreamBacked() {
      useChangeStream()
      useStatus()
      useConfig()
      useMetrics()
      useToken()
      useStorage(undefined)
      useActivity("1h")
      useHistory("", "", "2026-10-01T00:00:00Z")
      useEvents("e1")
    }

    function draw(changes: () => Response) {
      const { calls } = mockApi(authedRoutes({ "GET /api/token": fixtures.token, "GET /api/history": [], "GET /api/changes": changes }))
      const queryClient = trackQueryClient(createQueryClient())
      renderHook(() => useStreamBacked(), {
        wrapper: ({ children }: { children: ReactNode }) => <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>,
      })
      return () => PATHS.map((path) => calls.filter((c) => c.path === path).length)
    }

    const flush = () => act(() => vi.advanceTimersByTimeAsync(0))

    beforeEach(() => {
      vi.useFakeTimers()
    })

    afterEach(() => {
      vi.useRealTimers()
    })

    it("fetches each stream-backed query once when a hidden tab is shown again", async () => {
      const streams = [controlled(), controlled()]
      let n = 0
      const fetched = draw(() => streams[n++]?.response ?? json({ error: "not ready" }, 503))
      await flush()
      streams[0]?.send(HELLO)
      await flush()
      expect(fetched()).toEqual(each(1))
      act(() => setVisibility("hidden"))
      act(() => setVisibility("visible"))
      await flush()
      streams[1]?.send(HELLO)
      await flush()
      expect(fetched()).toEqual(each(2))
    })

    it("fetches each stream-backed query once when a tab that loaded hidden is shown", async () => {
      visibility = "hidden"
      const stream = controlled()
      const fetched = draw(() => stream.response)
      await flush()
      expect(fetched()).toEqual(each(1))
      act(() => setVisibility("visible"))
      await flush()
      stream.send(HELLO)
      await flush()
      expect(fetched()).toEqual(each(2))
    })

    it("lets the polls carry a re-shown tab where no stream runs", async () => {
      liveConfig.enabled = false
      const fetched = draw(() => json({ error: "not ready" }, 503))
      await flush()
      act(() => setVisibility("hidden"))
      act(() => setVisibility("visible"))
      await flush()
      expect(fetched()).toEqual(each(1))
      await act(() => vi.advanceTimersByTimeAsync(POLL_SLOW))
      expect(Math.min(...fetched())).toBeGreaterThanOrEqual(2)
    })
  })
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/query.test.tsx src/api/use-change-stream.test.tsx`
Expected: FAIL (`Tests  11 failed | 15 passed (26)`):
- the eight `leaves … to the change stream …` rows with `AssertionError: expected [ 1, 2, 3 ] to deeply equal [ 1, 1, 1 ]`
- both `fetches each stream-backed query once …` tests with `AssertionError: expected [ 3, 3, 3, 3, 3, 3, 3, 3 ] to deeply equal [ 2, 2, 2, 2, 2, 2, 2, 2 ]`
- `lets the polls carry a re-shown tab where no stream runs` with `AssertionError: expected [ 2, 2, 2, 2, 2, 2, 2, 2 ] to deeply equal [ 1, 1, 1, 1, 1, 1, 1, 1 ]`

`still refetches a query the stream does not cover` passes already and must keep passing.

- [ ] **Step 4: Write the implementation**

In `web/src/query.ts`, replace:

```ts
import { ApiError } from "./api/client"
import { hhmm } from "./lib/format"
```

with:

```ts
import { ApiError } from "./api/client"
import { TOPIC_QUERIES } from "./api/use-change-stream"
import { hhmm } from "./lib/format"
```

and replace:

```ts
export function createQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
    mutationCache: new MutationCache({ onError: (err) => toast.error(errorText(err)) }),
  })
}
```

with:

```ts
export function createQueryClient(): QueryClient {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
    mutationCache: new MutationCache({ onError: (err) => toast.error(errorText(err)) }),
  })
  // The stream's hello refetches these queries when a tab is shown again or
  // the network returns; the library's own refetch for either would fetch
  // each of them a second time. Where no stream runs, their polls carry on.
  for (const key of Object.values(TOPIC_QUERIES).flat()) {
    client.setQueryDefaults(key, { refetchOnWindowFocus: false, refetchOnReconnect: false })
  }
  return client
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/query.test.tsx src/api/use-change-stream.test.tsx`
Expected: PASS (`Tests  26 passed (26)`)

- [ ] **Step 6: Commit**

```bash
git add web/src/query.ts web/src/query.test.tsx web/src/api/use-change-stream.test.tsx
git commit -m "fix(web): fetch once when a tab is shown again"
```

### Task 53: Runner steps and containers stay when a poll fails

**Files:**
- Modify: `web/src/components/runner-panel.tsx`
- Test: `web/src/components/runner-panel.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing a later task names (`PollError` is local to the file)

**Items:** 84
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The runner panel's Steps tab tests for an error before it tests for data, so one failed poll replaces the step list with an error line, and the line offers no way to retry. The Containers tab does the same.

After this task both tabs keep what the last good poll showed and put the error line with its "Try again" button above it, the pattern the run page (`web/src/pages/run.tsx`) uses. With no data yet, the error line stands alone, as today but with the button.

Do not change the expression that renders the "Open run" link in this file; another task owns it.

- [ ] **Step 1: Write the failing test**

In `web/src/components/runner-panel.test.tsx`, replace:

```ts
import { fireEvent, screen, waitFor, within } from "@testing-library/react"
```

with:

```ts
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react"
```

Then insert directly above the test `says when no step is reported yet`:

```tsx
  it.each<[string, string, string, string]>([
    ["steps", "/runners/aaaaaa?tab=steps", "GET /api/runners/aaaaaa/steps", "Run tests"],
    ["containers", "/runners/aaaaaa?tab=containers", "GET /api/runners/aaaaaa/containers", "ghr-aaaaaa-db-1"],
  ])("keeps the %s on screen when a poll fails, with a way to retry", async (tab, path, route, shown) => {
    let down = false
    mockApi(routes({ [route]: () => (down ? json({ error: "GitHub is not answering" }, 502) : fixtures[tab as "steps" | "containers"]) }))
    const { queryClient, user } = draw(path)
    expect(await screen.findByText(shown)).toBeInTheDocument()
    down = true
    await act(() => queryClient.refetchQueries({ queryKey: [tab, "aaaaaa"] }))
    expect(await screen.findByText("GitHub is not answering")).toBeInTheDocument()
    expect(screen.getByText(shown)).toBeInTheDocument()
    down = false
    await user.click(screen.getByRole("button", { name: "Try again" }))
    await waitFor(() => expect(screen.queryByText("GitHub is not answering")).toBeNull())
    expect(screen.getByText(shown)).toBeInTheDocument()
  })

```

The test asks the query client for the refetch instead of waiting five seconds for the next poll.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/runner-panel.test.tsx -t "keeps the"`
Expected: FAIL in both rows, with `TestingLibraryElementError: Unable to find an element with the text: Run tests.` and `TestingLibraryElementError: Unable to find an element with the text: ghr-aaaaaa-db-1.` (`Tests  2 failed | 1 passed | 13 skipped (16)`; the one that passes is the existing `keeps the last snapshot after the runner ends`)

- [ ] **Step 3: Write the implementation**

In `web/src/components/runner-panel.tsx`, replace:

```tsx
function Steps({ steps, live, finished, now }: { steps: UseQueryResult<Step[]>; live: boolean; finished: boolean; now: number }) {
  if (steps.isError) return <ErrorLine>{errorText(steps.error)}</ErrorLine>
  if (!steps.data) {
    // Steps are polled only while the runner is live.
```

with:

```tsx
// A failed poll keeps what the last good one showed; its error goes above.
function PollError({ query }: { query: UseQueryResult<unknown> }) {
  if (!query.isError) return null
  return <ErrorLine onRetry={() => void query.refetch()}>{errorText(query.error)}</ErrorLine>
}

function Steps({ steps, live, finished, now }: { steps: UseQueryResult<Step[]>; live: boolean; finished: boolean; now: number }) {
  if (!steps.data) {
    if (steps.isError) return null
    // Steps are polled only while the runner is live.
```

Replace:

```tsx
function Containers({ containers }: { containers: UseQueryResult<Container[]> }) {
  if (containers.isError) return <ErrorLine>{errorText(containers.error)}</ErrorLine>
```

with:

```tsx
function Containers({ containers }: { containers: UseQueryResult<Container[]> }) {
  if (!containers.data && containers.isError) return null
```

Replace:

```tsx
        <TabsContent value="steps">
          <Steps steps={steps} live={live} finished={finished} now={now} />
        </TabsContent>
```

with:

```tsx
        <TabsContent value="steps">
          <div className="flex flex-col gap-3">
            <PollError query={steps} />
            <Steps steps={steps} live={live} finished={finished} now={now} />
          </div>
        </TabsContent>
```

Replace:

```tsx
        <TabsContent value="containers">
          <Containers containers={containers} />
        </TabsContent>
```

with:

```tsx
        <TabsContent value="containers">
          <div className="flex flex-col gap-3">
            <PollError query={containers} />
            <Containers containers={containers} />
          </div>
        </TabsContent>
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/runner-panel.test.tsx`
Expected: PASS (`Tests  16 passed (16)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/components/runner-panel.tsx web/src/components/runner-panel.test.tsx
git commit -m "fix(web): keep runner steps when a poll fails"
```

### Task 54: Registrations stay when a read fails

**Files:**
- Modify: `web/src/components/registrations-group.tsx`
- Test: `web/src/components/registrations-group.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 84
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The "GitHub registrations" group on a repository's page tests for an error before it tests for data. A failed read, for example after pressing Refresh, replaces the list with an error line that has no retry of its own.

After this task the list stays and the error line with "Try again" is shown above it. With no list yet, the error line stands alone. While GitHub is rejecting the token the group still shows only that message, as today.

- [ ] **Step 1: Write the failing test**

In `web/src/components/registrations-group.test.tsx`, replace:

```ts
import { mockApi, noContent } from "@/test/api"
```

with:

```ts
import { json, mockApi, noContent } from "@/test/api"
```

Then insert directly above the test `does not read while GitHub rejects the token`:

```tsx
  it("keeps the list when a read fails, with a way to retry", async () => {
    let down = false
    mockApi(
      routes({
        "GET /api/repos/darkmem/registrations": () =>
          down ? json({ error: "GitHub is not answering" }, 502) : [...fixtures.registrations, stale],
      }),
    )
    const { user } = renderApp("/repositories/darkmem/registrations")
    expect(await screen.findByText("old-runner")).toBeInTheDocument()
    down = true
    await user.click(screen.getByRole("button", { name: "Refresh" }))
    const region = within(screen.getByRole("region", { name: "GitHub registrations" }))
    expect(await region.findByText("GitHub is not answering")).toBeInTheDocument()
    expect(region.getByText("old-runner")).toBeInTheDocument()
    down = false
    await user.click(region.getByRole("button", { name: "Try again" }))
    await waitFor(() => expect(region.queryByText("GitHub is not answering")).toBeNull())
    expect(region.getByText("old-runner")).toBeInTheDocument()
  })

```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/registrations-group.test.tsx`
Expected: FAIL with `TestingLibraryElementError: Unable to find an element with the text: old-runner.` (`Tests  1 failed | 4 passed (5)`)

- [ ] **Step 3: Write the implementation**

In `web/src/components/registrations-group.tsx`, replace:

```tsx
  let body: ReactNode
  if (degraded) body = <p className="text-sm text-destructive">{`GitHub is rejecting the token: ${degradedReason ?? ""}`}</p>
  else if (regs.isError) body = <ErrorLine>{errorText(regs.error)}</ErrorLine>
  else if (!regs.data) body = <Spinner label="Loading" />
```

with:

```tsx
  const error = !degraded && regs.isError ? <ErrorLine onRetry={() => void regs.refetch()}>{errorText(regs.error)}</ErrorLine> : null
  let body: ReactNode
  if (degraded) body = <p className="text-sm text-destructive">{`GitHub is rejecting the token: ${degradedReason ?? ""}`}</p>
  else if (!regs.data) body = error ?? <Spinner label="Loading" />
```

and replace:

```tsx
    <FieldGroup id="registrations" title="GitHub registrations">
      {body ? (
```

with:

```tsx
    <FieldGroup id="registrations" title="GitHub registrations">
      {error && regs.data && <div className="py-3">{error}</div>}
      {body ? (
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/registrations-group.test.tsx`
Expected: PASS (`Tests  5 passed (5)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/components/registrations-group.tsx web/src/components/registrations-group.test.tsx
git commit -m "fix(web): keep registrations when a read fails"
```

### Task 55: The repository picker keeps its list on an error

**Files:**
- Modify: `web/src/components/repo-picker.tsx`
- Create: `web/src/components/repo-picker.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 84
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The repository picker of the Add repository and Watch a repository dialogs tests for an error before it tests for data. Its list is read again when the window regains focus, and one failed read replaces the list, and the filter the user typed, with an error line.

After this task the list and the filter stay and the error line with "Try again" is shown above them. With no list yet, the error line stands alone, as today. The picker had no test file of its own; this task adds one.

- [ ] **Step 1: Write the failing test**

Create `web/src/components/repo-picker.test.tsx`:

```tsx
import { act, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"
import { keys } from "@/api/hooks"
import { json, mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { RepoPicker } from "./repo-picker"

describe("RepoPicker", () => {
  it("keeps the list when a read fails, with a way to retry", async () => {
    let down = false
    mockApi({
      "GET /api/repos/available": () => (down ? json({ error: "GitHub is not answering" }, 502) : fixtures.availableRepos),
    })
    const { queryClient, wrapper } = withQuery()
    const user = userEvent.setup()
    render(<RepoPicker picked="" onPick={() => {}} />, { wrapper })
    expect(await screen.findByText("new-repo")).toBeInTheDocument()
    down = true
    await act(() => queryClient.refetchQueries({ queryKey: keys.availableRepos }))
    expect(await screen.findByText("GitHub is not answering")).toBeInTheDocument()
    expect(screen.getByText("new-repo")).toBeInTheDocument()
    down = false
    await user.click(screen.getByRole("button", { name: "Try again" }))
    await waitFor(() => expect(screen.queryByText("GitHub is not answering")).toBeNull())
    expect(screen.getByText("new-repo")).toBeInTheDocument()
  })

  it("shows only the error while there is no list yet", async () => {
    mockApi({ "GET /api/repos/available": () => json({ error: "GitHub is not answering" }, 502) })
    const { wrapper } = withQuery()
    render(<RepoPicker picked="" onPick={() => {}} />, { wrapper })
    expect(await screen.findByText("GitHub is not answering")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument()
    expect(screen.queryByLabelText("Filter repositories")).toBeNull()
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/repo-picker.test.tsx`
Expected: FAIL in the first test with `TestingLibraryElementError: Unable to find an element with the text: new-repo.` (`Tests  1 failed | 1 passed (2)`). The second test passes already; it pins the case that must not change.

- [ ] **Step 3: Write the implementation**

In `web/src/components/repo-picker.tsx`, replace:

```tsx
  if (repos.isError) return <ErrorLine onRetry={() => void repos.refetch()}>{errorText(repos.error)}</ErrorLine>
  if (!repos.data) return <Spinner label="Loading repositories" />
  return (
    <div className="flex flex-col gap-2">
      <Input aria-label="Filter repositories" placeholder="Type to filter" value={filter} onChange={(e) => setFilter(e.target.value)} />
```

with:

```tsx
  const error = repos.isError ? <ErrorLine onRetry={() => void repos.refetch()}>{errorText(repos.error)}</ErrorLine> : null
  if (!repos.data) return error ?? <Spinner label="Loading repositories" />
  return (
    <div className="flex flex-col gap-2">
      {error}
      <Input aria-label="Filter repositories" placeholder="Type to filter" value={filter} onChange={(e) => setFilter(e.target.value)} />
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/repo-picker.test.tsx src/components/add-repo-dialog.test.tsx`
Expected: PASS (`Tests  14 passed (14)`; `repo-picker.test.tsx` alone is `Tests  2 passed (2)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/components/repo-picker.tsx web/src/components/repo-picker.test.tsx
git commit -m "fix(web): keep the repository list on an error"
```

### Task 56: The runner panel polls only the open tab

**Files:**
- Modify: `web/src/components/runner-panel.tsx`
- Modify: `web/src/api/hooks.ts` (`useSteps` and `useContainers` only)
- Test: `web/src/components/runner-panel.test.tsx`

**Interfaces:**
- Consumes: `web/src/components/runner-panel.tsx` as Task 53 left it
- Produces: `useSteps(id, enabled)` and `useContainers(id, enabled)`: the second parameter is renamed from `live`, its type is unchanged

**Items:** 87
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

The runner panel polls a live runner's steps and its containers every five seconds whichever of its three tabs is shown. Steps cost a GitHub API call on each poll and containers a Docker call. The log tail already polls only on the Log tab.

After this task steps are read only while the Steps tab is open and containers only while the Containers tab is open. A tab fetches when it is opened, because enabling a query with stale data fetches it. Data already read stays in the cache while its tab is closed.

In `web/src/api/hooks.ts` change only `useSteps` and `useContainers`. Do not touch `useJobLog`, `useHistory` or the `history` and `jobLog` entries of `keys`, and do not reformat the file: other tasks change those.

- [ ] **Step 1: Write the failing test**

In `web/src/components/runner-panel.test.tsx`, insert directly above the test `says when no step is reported yet`:

```tsx
  it("reads steps and containers only while their own tab is open", async () => {
    const { calls } = mockApi(routes())
    const { queryClient, user } = draw("/runners/aaaaaa?tab=steps")
    const reads = (what: string) => calls.filter((c) => c.path === `/api/runners/aaaaaa/${what}`).length
    const polled = (what: string) => queryClient.getQueryCache().find({ queryKey: [what, "aaaaaa"] })?.isActive() ?? false
    expect(await screen.findByText("Run tests")).toBeInTheDocument()
    expect(reads("containers")).toBe(0)
    expect([polled("steps"), polled("containers")]).toEqual([true, false])

    await user.click(screen.getByRole("tab", { name: "Containers" }))
    expect(await screen.findByText("ghr-aaaaaa-db-1")).toBeInTheDocument()
    expect([polled("steps"), polled("containers")]).toEqual([false, true])

    await user.click(screen.getByRole("tab", { name: "Log" }))
    expect(await screen.findByText(/Listening for Jobs/)).toBeInTheDocument()
    expect([polled("steps"), polled("containers")]).toEqual([false, false])

    const before = reads("steps")
    await user.click(screen.getByRole("tab", { name: "Steps" }))
    await waitFor(() => expect(reads("steps")).toBe(before + 1))
  })

```

`isActive()` is true while a query has an enabled observer, which is also the condition for its interval poll, so the test needs no five-second wait.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/runner-panel.test.tsx -t "only while their own tab"`
Expected: FAIL with `AssertionError: expected 1 to be +0 // Object.is equality` (`Tests  1 failed | 16 skipped (17)`): the containers were read on the Steps tab.

- [ ] **Step 3: Write the implementation**

In `web/src/components/runner-panel.tsx`, replace:

```tsx
  const steps = useSteps(id, live)
  const containers = useContainers(id, live)
```

with:

```tsx
  const steps = useSteps(id, live && tab === "steps")
  const containers = useContainers(id, live && tab === "containers")
```

In `web/src/api/hooks.ts`, replace:

```ts
export function useSteps(id: string, live: boolean) {
  return useQuery({
    queryKey: keys.steps(id),
    queryFn: ({ signal }) => api.steps(id, signal),
    enabled: live,
    refetchInterval: live ? POLL_SLOW : false,
  })
}

export function useContainers(id: string, live: boolean) {
  return useQuery({
    queryKey: keys.containers(id),
    queryFn: ({ signal }) => api.containers(id, signal),
    enabled: live,
    refetchInterval: live ? POLL_SLOW : false,
  })
}
```

with:

```ts
// Steps cost a GitHub call and containers a Docker call on every poll, so the
// caller enables each only while it is on screen.
export function useSteps(id: string, enabled: boolean) {
  return useQuery({
    queryKey: keys.steps(id),
    queryFn: ({ signal }) => api.steps(id, signal),
    enabled,
    refetchInterval: enabled ? POLL_SLOW : false,
  })
}

export function useContainers(id: string, enabled: boolean) {
  return useQuery({
    queryKey: keys.containers(id),
    queryFn: ({ signal }) => api.containers(id, signal),
    enabled,
    refetchInterval: enabled ? POLL_SLOW : false,
  })
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/runner-panel.test.tsx src/api/hooks-live.test.tsx`
Expected: PASS (`Tests  31 passed (31)`; `runner-panel.test.tsx` alone is `Tests  17 passed (17)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/components/runner-panel.tsx web/src/components/runner-panel.test.tsx web/src/api/hooks.ts
git commit -m "perf(web): poll only the open runner tab"
```

### Task 57: No cached repository list between two dialogs

**Files:**
- Modify: `web/src/api/hooks.ts` (`useAvailableRepos` only)
- Test: `web/src/api/hooks-pages.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 93
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The Add repository and Watch a repository dialogs read the list of repositories that can be picked through `useAvailableRepos`. The query library keeps the answer for five minutes after the dialog closes, so reopening either dialog shows the previous list while the new one loads: a repository that was just added or watched is offered again for a moment.

After this task the query keeps no cache once nothing shows it (`gcTime: 0`), so a reopened dialog starts with "Loading repositories".

In `web/src/api/hooks.ts` change only `useAvailableRepos`. Do not touch `useJobLog`, `useHistory` or the `history` and `jobLog` entries of `keys`, and do not reformat the file: other tasks change those.

- [ ] **Step 1: Write the failing test**

In `web/src/api/hooks-pages.test.tsx`, replace:

```ts
import { storageBusy, useJobLog, useLabelCheck } from "./hooks"
```

with:

```ts
import { storageBusy, useAvailableRepos, useJobLog, useLabelCheck } from "./hooks"
```

`act` and `vi` are already imported at the top of the file.

Then append at the end of the file:

```tsx

describe("useAvailableRepos", () => {
  it("keeps no list from one opening to the next", async () => {
    vi.useFakeTimers()
    try {
      const { calls } = mockApi({ "GET /api/repos/available": fixtures.availableRepos })
      const { wrapper } = withQuery()
      const first = renderHook(() => useAvailableRepos(true), { wrapper })
      await act(() => vi.advanceTimersByTimeAsync(0))
      expect(first.result.current.data).toHaveLength(3)
      first.unmount()
      await act(() => vi.advanceTimersByTimeAsync(0))
      const second = renderHook(() => useAvailableRepos(true), { wrapper })
      expect(second.result.current.data).toBeUndefined()
      await act(() => vi.advanceTimersByTimeAsync(0))
      expect(second.result.current.data).toHaveLength(3)
      expect(calls).toHaveLength(2)
    } finally {
      vi.useRealTimers()
    }
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/api/hooks-pages.test.tsx -t "useAvailableRepos"`
Expected: FAIL with `AssertionError: expected [ { name: 'darkmem', …(3) }, …(2) ] to be undefined` (`Tests  1 failed | 11 skipped (12)`)

- [ ] **Step 3: Write the implementation**

In `web/src/api/hooks.ts`, replace:

```ts
export function useAvailableRepos(enabled: boolean) {
  return useQuery({ queryKey: keys.availableRepos, queryFn: ({ signal }) => api.availableRepos(signal), enabled, staleTime: 0 })
}
```

with:

```ts
// The list is dropped when its dialog closes: a kept one would show for a
// moment in the next dialog, with repositories added or watched since.
export function useAvailableRepos(enabled: boolean) {
  return useQuery({
    queryKey: keys.availableRepos,
    queryFn: ({ signal }) => api.availableRepos(signal),
    enabled,
    staleTime: 0,
    gcTime: 0,
  })
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/api/hooks-pages.test.tsx src/components/repo-picker.test.tsx src/components/add-repo-dialog.test.tsx`
Expected: PASS (`Tests  29 passed (29)`; `hooks-pages.test.tsx` alone is `Tests  12 passed (12)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/api/hooks.ts web/src/api/hooks-pages.test.tsx
git commit -m "fix(web): drop the repository list on close"
```

### Task 58: One clock for every useNow caller

**Files:**
- Modify: `web/src/lib/use-now.ts`
- Create: `web/src/lib/use-now.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing (`useNow(): number` and `clockOffset` keep their signatures)

**Items:** 106
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 2 = 3

`useNow` gives a component the current time on the daemon's clock and re-renders it once a second. Thirteen source files call it, and each caller owns its own one-second interval, so a page with several of them runs several timers that fire at different moments.

After this task one module-level ticker serves every caller. It starts with the first caller and stops with the last, and callers read it through `useSyncExternalStore`, the way `web/src/lib/use-media-query.ts` reads a media query. Two behaviours of the old hook are kept and pinned: the first render shows the current time, not the time the ticker last ran, and a new status from the daemon makes the clock read the time at once, so the value never runs behind the status that set the offset.

The two existing `useNow` tests in `web/src/lib/format.test.ts` are not changed and must keep passing.

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/use-now.test.tsx`:

```tsx
import { act, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { keys } from "@/api/hooks"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { useNow } from "./use-now"

const at = (time: string) => Date.parse(`2026-10-03T${time}Z`)

describe("useNow", () => {
  beforeEach(() => {
    vi.useFakeTimers({ now: at("14:05:00") })
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it("serves every caller from one interval and stops it with the last", () => {
    const { wrapper } = withQuery()
    const first = renderHook(() => useNow(), { wrapper })
    const second = renderHook(() => useNow(), { wrapper })
    expect(vi.getTimerCount()).toBe(1)
    act(() => {
      vi.advanceTimersByTime(1000)
    })
    expect([first.result.current, second.result.current]).toEqual([at("14:05:01"), at("14:05:01")])
    first.unmount()
    expect(vi.getTimerCount()).toBe(1)
    second.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })

  it("renders the current time first after standing idle", () => {
    const { wrapper } = withQuery()
    renderHook(() => useNow(), { wrapper }).unmount()
    vi.setSystemTime(at("14:06:00"))
    const rendered: number[] = []
    renderHook(() => rendered.push(useNow()), { wrapper })
    expect(rendered[0]).toBe(at("14:06:00"))
  })

  it("does not run behind a status that arrived between two ticks", () => {
    const { queryClient, wrapper } = withQuery()
    const { result } = renderHook(() => useNow(), { wrapper })
    act(() => {
      vi.advanceTimersByTime(400)
      queryClient.setQueryData(keys.status, { ...fixtures.status, now: "2026-10-03T14:07:30.400Z" })
    })
    expect(result.current).toBe(at("14:07:30.400"))
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/lib/use-now.test.tsx`
Expected: FAIL in the first test with `AssertionError: expected 2 to be 1 // Object.is equality` (`Tests  1 failed | 2 passed (3)`). The other two pass against the old hook; they pin what the rewrite must keep.

- [ ] **Step 3: Write the implementation**

In `web/src/lib/use-now.ts`, replace:

```ts
import { useEffect, useState, useSyncExternalStore } from "react"
```

with:

```ts
import { useEffect, useSyncExternalStore } from "react"
```

and replace the whole `useNow` function at the end of the file:

```ts
export function useNow(): number {
  const [now, setNow] = useState(() => Date.now())
  const offset = useClockOffset()
  // Restarting on a new offset keeps the tick from lagging behind the status
  // that set it, which could read a check made a minute ago as "just now".
  useEffect(() => {
    setNow(Date.now())
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [offset])
  return now + offset
}
```

with:

```ts
// One ticker serves every useNow caller, and runs only while one is mounted.
let now = Date.now()
let timer: ReturnType<typeof setInterval> | undefined
const listeners = new Set<() => void>()

function read(): void {
  const at = Date.now()
  if (at === now) return
  now = at
  for (const l of listeners) l()
}

function subscribe(l: () => void): () => void {
  listeners.add(l)
  timer ??= setInterval(read, 1000)
  return () => {
    listeners.delete(l)
    if (listeners.size > 0) return
    clearInterval(timer)
    timer = undefined
  }
}

// Nothing ticks while nobody listens, so the first caller to render would
// find the time the last one left. Within a second of it the value is kept,
// because a snapshot must not change between two reads of one render.
function snapshot(): number {
  if (timer === undefined) {
    const at = Date.now()
    if (Math.abs(at - now) >= 1000) now = at
  }
  return now
}

export function useNow(): number {
  const offset = useClockOffset()
  const at = useSyncExternalStore(subscribe, snapshot)
  // Reading the clock on a new offset keeps the tick from lagging behind the
  // status that set it, which could read a check made a minute ago as "just now".
  useEffect(read, [offset])
  return at + offset
}
```

`clockOffset` and `useClockOffset` above it stay as they are.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib/use-now.test.tsx src/lib/format.test.ts`
Expected: PASS (`Tests  23 passed (23)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/use-now.ts web/src/lib/use-now.test.tsx
git commit -m "perf(web): tick every clock from one interval"
```

### Task 59: One classifier for job results

**Files:**
- Modify: `web/src/lib/status.ts`
- Create: `web/src/lib/status.test.ts`

**Interfaces:**
- Consumes: nothing
- Produces: `resultState(conclusion: string): string` exported from `web/src/lib/status.ts`. It returns the conclusion itself for `success`, `cancelled`, `skipped`, `unknown`, `neutral`, `stale` and `action_required`, and `"failure"` for anything else. The existing `stateWord` and `stateTone` of the same file turn that state into its word and its tone; `stateTone("action_required")` is now `"warn"`.

**Items:** 78
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

Three places in the web UI turn a job's or a run's conclusion into an icon, a pill or a word, and each one has its own list of the conclusions it knows. Everything a list leaves out is shown as Failed, so GitHub's `neutral`, `stale` and `action_required` read as failures although they are not.

This task adds the one rule the three places will share. The later tasks switch each place to it. The rule: the seven conclusions listed above keep their own name; every other value, `timed_out`, `startup_failure` and any value GitHub adds later included, is a failure, so a new failure-like result can never look harmless. The tone names in this code base are `ok` (success), `bad` (failure), `warn` (warning) and `muted`.

- [ ] **Step 1: Write the failing test**

`web/src/lib/status.ts` has no test file yet. Create `web/src/lib/status.test.ts` with exactly this content:

```ts
import { describe, expect, it } from "vitest"
import { resultState, stateTone, stateWord } from "./status"

describe("resultState", () => {
  it.each([
    ["success", "Succeeded", "ok"],
    ["cancelled", "Cancelled", "muted"],
    ["skipped", "Skipped", "muted"],
    ["unknown", "Unknown", "muted"],
    ["neutral", "Neutral", "muted"],
    ["stale", "Stale", "muted"],
    ["action_required", "Action required", "warn"],
    ["failure", "Failed", "bad"],
    ["timed_out", "Failed", "bad"],
    ["startup_failure", "Failed", "bad"],
    ["a_result_added_later", "Failed", "bad"],
    ["", "Failed", "bad"],
  ])("reads %s as %s in the %s tone", (conclusion, word, tone) => {
    const state = resultState(conclusion)
    expect(stateWord(state)).toBe(word)
    expect(stateTone(state)).toBe(tone)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/lib/status.test.ts`
Expected: FAIL, 12 tests, each with `TypeError: resultState is not a function`

- [ ] **Step 3: Write the implementation**

In `web/src/lib/status.ts`, in the `tones` map, replace:

```ts
  interrupted: "warn",
  error: "bad",
```

with:

```ts
  interrupted: "warn",
  action_required: "warn",
  error: "bad",
```

In the same file, insert directly above the comment line `// A repository at its cap with jobs queued: the jobs wait on its own limit.`:

```ts
const RESULTS = new Set(["success", "cancelled", "skipped", "unknown", "neutral", "stale", "action_required"])

// The state a finished job or run is shown as; stateWord and stateTone give
// its word and tone. A conclusion that is not listed reads as a failure, so a
// result GitHub adds later can never look harmless. "unknown" is ghr's own
// conclusion for a job whose result it never learned.
export function resultState(conclusion: string): string {
  return RESULTS.has(conclusion) ? conclusion : "failure"
}

```

Nothing else in the file changes. `stateWord` already turns `action_required` into `Action required` (it replaces underscores and capitalises), and `stateTone` already answers `muted` for a state its map does not hold, which is what `neutral`, `stale`, `cancelled`, `skipped` and `unknown` get.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib/status.test.ts`
Expected: PASS (`Tests  12 passed (12)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/status.ts web/src/lib/status.test.ts
git commit -m "feat(web): add one classifier for job results"
```

### Task 60: Result icons use the classifier

**Files:**
- Modify: `web/src/components/result-icon.tsx`
- Test: `web/src/components/result-icon.test.tsx`, `web/src/components/step-list.test.tsx`

**Interfaces:**
- Consumes: `resultState(conclusion: string): string` from `web/src/lib/status.ts` (Task 59)
- Produces: nothing

**Items:** 78
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

`ResultIcon` draws the small result icon of a finished job or step on the Dashboard, History, Actions, the run page and the runner panel's step list. It keeps its own set of known conclusions and draws everything else as Failed, so a `neutral`, `stale` or `action_required` job shows a red cross. The icon now takes its state from `resultState`: Neutral and Stale in the muted colour, Action required in the warning colour, and every unknown value still Failed.

- [ ] **Step 1: Write the failing test**

In `web/src/components/result-icon.test.tsx`, add four rows to the table of the existing test. Replace:

```tsx
    ["timed_out", "Failed", "text-destructive"],
  ])("draws %s", (conclusion, label, colour) => {
```

with:

```tsx
    ["timed_out", "Failed", "text-destructive"],
    ["startup_failure", "Failed", "text-destructive"],
    ["neutral", "Neutral", "text-muted-foreground"],
    ["stale", "Stale", "text-muted-foreground"],
    ["action_required", "Action required", "text-warning"],
  ])("draws %s", (conclusion, label, colour) => {
```

One existing test asserts the old rule. `web/src/components/step-list.test.tsx` renders a step whose conclusion is `neutral` and expects its icon to be named Failed. In that file replace:

```tsx
    expect(screen.getByRole("img", { name: "Failed" })).toBeInTheDocument()
```

with:

```tsx
    expect(screen.getByRole("img", { name: "Neutral" })).toBeInTheDocument()
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/components/result-icon.test.tsx src/components/step-list.test.tsx`
Expected: FAIL, 4 of 11 tests: `draws neutral`, `draws stale` and `draws action_required` in `result-icon.test.tsx`, the first with `Unable to find an accessible element with the role "img" and name "Neutral"`, and the one test of `step-list.test.tsx` with the same message. The `startup_failure` row already passes: it pins that an unlisted result stays Failed.

- [ ] **Step 3: Write the implementation**

Replace the whole content of `web/src/components/result-icon.tsx`:

```tsx
import { stateIcon, TONE_TEXT } from "@/lib/state-icon"
import { stateTone, stateWord } from "@/lib/status"

const KNOWN = new Set(["success", "failure", "cancelled", "skipped", "unknown"])

// Any other conclusion shows as failed, as the Dashboard has always shown it.
// "unknown" is ghr's own conclusion for a job whose result it never learned.
export function ResultIcon({ conclusion }: { conclusion: string }) {
  const state = KNOWN.has(conclusion) ? conclusion : "failure"
  const Icon = stateIcon(state)
  return <Icon role="img" aria-label={stateWord(state)} size={15} className={`shrink-0 ${TONE_TEXT[stateTone(state)]}`} />
}
```

with:

```tsx
import { stateIcon, TONE_TEXT } from "@/lib/state-icon"
import { resultState, stateTone, stateWord } from "@/lib/status"

export function ResultIcon({ conclusion }: { conclusion: string }) {
  const state = resultState(conclusion)
  const Icon = stateIcon(state)
  return <Icon role="img" aria-label={stateWord(state)} size={15} className={`shrink-0 ${TONE_TEXT[stateTone(state)]}`} />
}
```

The comment goes with the set it described; `resultState` carries the rule and its reason.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/result-icon.test.tsx src/components/step-list.test.tsx`
Expected: PASS (`Tests  11 passed (11)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/components/result-icon.tsx web/src/components/result-icon.test.tsx web/src/components/step-list.test.tsx
git commit -m "fix(web): draw result icons from the classifier"
```

### Task 61: The run pill uses the classifier

**Files:**
- Modify: `web/src/components/run-frame.tsx` (`RunPill`, one import)
- Test: `web/src/components/run-frame.test.tsx`

**Interfaces:**
- Consumes: `resultState(conclusion: string): string` from `web/src/lib/status.ts` (Task 59)
- Produces: nothing

**Items:** 78
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

The pill in the header of a run page shows a finished run as Succeeded, Cancelled or Skipped and everything else as Failed. That includes `unknown`, which the result icon and the History table show as Unknown, and `neutral`, `stale` and `action_required`. The pill now takes its state from `resultState`, so it agrees with the other two places. A run that is still active keeps its Running or Waiting pill.

- [ ] **Step 1: Write the failing test**

In `web/src/components/run-frame.test.tsx`, add five rows to the table of the test `shows the run's state as a pill before the header actions (%o)`. Replace:

```tsx
    [{ status: "completed", conclusion: "success" }, "Succeeded", "bg-success/15", "text-success"],
    [{ status: "in_progress", conclusion: "" }, "Running", "bg-running/15", "text-running"],
```

with:

```tsx
    [{ status: "completed", conclusion: "success" }, "Succeeded", "bg-success/15", "text-success"],
    [{ status: "completed", conclusion: "timed_out" }, "Failed", "bg-destructive/15", "text-destructive"],
    [{ status: "completed", conclusion: "unknown" }, "Unknown", "bg-muted-foreground/15", "text-muted-foreground"],
    [{ status: "completed", conclusion: "neutral" }, "Neutral", "bg-muted-foreground/15", "text-muted-foreground"],
    [{ status: "completed", conclusion: "stale" }, "Stale", "bg-muted-foreground/15", "text-muted-foreground"],
    [{ status: "completed", conclusion: "action_required" }, "Action required", "bg-warning/15", "text-warning"],
    [{ status: "in_progress", conclusion: "" }, "Running", "bg-running/15", "text-running"],
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/run-frame.test.tsx`
Expected: FAIL, 4 of 19 tests (the `unknown`, `neutral`, `stale` and `action_required` rows), the first with `Unable to find an element with the text: Unknown`. The `timed_out` row already passes: it pins that an unlisted result stays Failed.

- [ ] **Step 3: Write the implementation**

In `web/src/components/run-frame.tsx`, replace:

```tsx
import { SIDE_LINK } from "@/lib/side-link"
```

with:

```tsx
import { SIDE_LINK } from "@/lib/side-link"
import { resultState } from "@/lib/status"
```

and replace the whole `RunPill` function:

```tsx
function RunPill({ run }: { run: RunDetail }) {
  if (!isRunActive(run.status)) {
    const done = run.conclusion === "success" || run.conclusion === "cancelled" || run.conclusion === "skipped"
    return <StatusPill state={done ? run.conclusion : "failure"} />
  }
  return <StatusPill state={run.status === "in_progress" ? "running" : "queued"} />
}
```

with:

```tsx
function RunPill({ run }: { run: RunDetail }) {
  if (!isRunActive(run.status)) return <StatusPill state={resultState(run.conclusion)} />
  return <StatusPill state={run.status === "in_progress" ? "running" : "queued"} />
}
```

`StatusPill` derives the word, the tint and the icon from the state it is given, so nothing else changes.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/run-frame.test.tsx`
Expected: PASS (`Tests  19 passed (19)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/components/run-frame.tsx web/src/components/run-frame.test.tsx
git commit -m "fix(web): take the run pill from the classifier"
```

### Task 62: Result words use the classifier

**Files:**
- Modify: `web/src/lib/history.ts` (`resultWord`, one import)
- Test: `web/src/lib/history.test.ts`, `web/src/lib/actions.test.ts`

**Interfaces:**
- Consumes: `resultState(conclusion: string): string` and the existing `stateWord(state: string): string`, both from `web/src/lib/status.ts` (Task 59)
- Produces: nothing new. `resultWord(conclusion)` keeps its name and signature; it now answers `Neutral`, `Stale` and `Action required` for those three conclusions.

**Items:** 78
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

`resultWord` in `web/src/lib/history.ts` is the third place with its own list of conclusions. It words the status line of a job on the run page, and two counts compare its answer with `"Failed"`: the "N failed" of the History summary and the Failed filter of the Actions page. Today it answers `Failed` for `neutral`, `stale` and `action_required`, so both counts include them. It now words the state `resultState` gives.

Do not touch `pickText` or its tests in these two files, and do not reformat either file: another task changes them.

- [ ] **Step 1: Write the failing tests**

In `web/src/lib/history.test.ts`, in `describe("resultWord", …)` at the end of the file, replace:

```ts
    ["timed_out", "Failed"],
  ])("reads %s as %s", (conclusion, word) => {
    expect(resultWord(conclusion)).toBe(word)
  })
})
```

with:

```ts
    ["timed_out", "Failed"],
    ["startup_failure", "Failed"],
    ["neutral", "Neutral"],
    ["stale", "Stale"],
    ["action_required", "Action required"],
  ])("reads %s as %s", (conclusion, word) => {
    expect(resultWord(conclusion)).toBe(word)
  })

  it("keeps a result that is not a failure out of the summary's failed count", () => {
    const rows = ["neutral", "stale", "action_required", "timed_out"].map((c, i) =>
      entry(String(i), "2026-10-03T13:35:00Z", "2026-10-03T13:40:00Z", c),
    )
    expect(historySummary(rows)).toBe("4 jobs, 1 failed, median 5m00s")
  })
})
```

`entry` and `historySummary` are already defined and imported at the top of that file.

In `web/src/lib/actions.test.ts`, in `describe("splitRuns", …)`, replace:

```ts
    expect(splitRuns(many, undefined, "failed").recent).toHaveLength(30)
  })
})
```

with:

```ts
    expect(splitRuns(many, undefined, "failed").recent).toHaveLength(30)
  })

  it("keeps a result that is not a failure out of Failed", () => {
    const mixed = ["neutral", "stale", "action_required", "timed_out"].map((conclusion, id) => run({ id, conclusion }))
    expect(splitRuns(mixed, undefined, "failed").recent.map((r) => r.id)).toEqual([3])
  })
})
```

`run` is the helper defined near the top of that file; it builds a completed run.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/lib/history.test.ts src/lib/actions.test.ts`
Expected: FAIL, 5 of 34 tests:
- `reads neutral as Neutral` with `expected 'Failed' to be 'Neutral'`, and the same for `stale` and `action_required`
- `keeps a result that is not a failure out of the summary's failed count` with `expected '4 jobs, 4 failed, median 5m00s' to be '4 jobs, 1 failed, median 5m00s'`
- `keeps a result that is not a failure out of Failed` with `expected [ +0, 1, 2, 3 ] to deeply equal [ 3 ]`

- [ ] **Step 3: Write the implementation**

In `web/src/lib/history.ts`, replace:

```ts
import { dur, hhmm, monthDay, plural } from "@/lib/format"
```

with:

```ts
import { dur, hhmm, monthDay, plural } from "@/lib/format"
import { resultState, stateWord } from "@/lib/status"
```

and replace the function with the comment above it:

```ts
// As the result icons read them: anything not a success, a cancellation, a
// skip or unknown is a failure.
export function resultWord(conclusion: string): string {
  if (conclusion === "success") return "Succeeded"
  if (conclusion === "cancelled") return "Cancelled"
  if (conclusion === "skipped") return "Skipped"
  if (conclusion === "unknown") return "Unknown"
  return "Failed"
}
```

with:

```ts
export function resultWord(conclusion: string): string {
  return stateWord(resultState(conclusion))
}
```

`historySummary` in the same file and `matchesStatus` in `web/src/lib/actions.ts` keep comparing `resultWord(...)` with `"Failed"`; neither changes.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib/history.test.ts src/lib/actions.test.ts`
Expected: PASS (`Tests  34 passed (34)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/history.ts web/src/lib/history.test.ts web/src/lib/actions.test.ts
git commit -m "fix(web): word results through the classifier"
```

### Task 63: The daemon stops counting neutral results as failed

**Files:**
- Modify: `internal/model/model.go` (`Failed`)
- Create: `internal/model/model_test.go`
- Test: `internal/history/history_test.go`

**Interfaces:**
- Consumes: nothing
- Produces: `model.Failed(conclusion string) bool` answers `false` for `neutral`, `stale` and `action_required` as well as for `success`, `cancelled`, `skipped` and `unknown`; every other value is still `true`. Its two callers follow without a change of their own: `matchConclusion` in `internal/history/history.go` (the History page's "Failed" filter, sent as `conclusion=failure`) and `finishedState` in `internal/activity/activity.go` (the charts).

**Items:** 78
**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

This is the daemon's half of the result rule. `model.Failed` decides which job conclusions the History "Failed" filter returns and which the activity charts count and draw as failed. It treats everything that is not a success, a cancellation, a skip or `unknown` as a failure, so GitHub's `neutral`, `stale` and `action_required` are filtered and charted as failures. The web UI stops showing those three as Failed in the tasks around this one; this task makes the daemon agree. A conclusion that nobody lists stays a failure, so a result GitHub adds later cannot look harmless.

Only `internal/model/model.go` changes. `internal/history/history.go` and `internal/activity/*.go` are not edited.

- [ ] **Step 1: Write the failing tests**

Create `internal/model/model_test.go` with exactly this content:

```go
package model

import "testing"

func TestFailed(t *testing.T) {
	for conclusion, want := range map[string]bool{
		"success":         false,
		"cancelled":       false,
		"skipped":         false,
		"unknown":         false,
		"neutral":         false,
		"stale":           false,
		"action_required": false,
		"failure":         true,
		"timed_out":       true,
		"startup_failure": true,
		"added_later":     true,
		"":                true,
	} {
		if got := Failed(conclusion); got != want {
			t.Errorf("Failed(%q) = %v, want %v", conclusion, got, want)
		}
	}
}
```

The existing test `TestQueryFailureMeansEveryFailedConclusion` in `internal/history/history_test.go` asserts the old rule: it expects `action_required` among the failures. Correct it and add the two other names. In that test, replace:

```go
	for i, c := range []string{"success", "failure", "timed_out", "startup_failure", "action_required", "cancelled", "skipped", "unknown"} {
		if err := s.Append(model.HistoryEntry{ID: c, Repo: "a", RunID: int64(i + 1), Conclusion: c, FinishedAt: t0.Add(time.Duration(-i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	for conclusion, want := range map[string]string{
		"failure":   "failure,timed_out,startup_failure,action_required",
		"timed_out": "timed_out",
		"unknown":   "unknown",
	} {
```

with:

```go
	for i, c := range []string{"success", "failure", "timed_out", "startup_failure", "action_required", "neutral", "stale", "cancelled", "skipped", "unknown"} {
		if err := s.Append(model.HistoryEntry{ID: c, Repo: "a", RunID: int64(i + 1), Conclusion: c, FinishedAt: t0.Add(time.Duration(-i) * time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	for conclusion, want := range map[string]string{
		"failure":         "failure,timed_out,startup_failure",
		"timed_out":       "timed_out",
		"action_required": "action_required",
		"unknown":         "unknown",
	} {
```

The new `action_required` row pins that a query for that exact conclusion still finds the job; only the `failure` filter leaves it out.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -count=1 ./internal/model/ ./internal/history/`
Expected: FAIL in both packages:
- `TestFailed` with `Failed("neutral") = true, want false`, and the same line for `"stale"` and `"action_required"` (in any order)
- `TestQueryFailureMeansEveryFailedConclusion` with `Query("failure") = [failure timed_out startup_failure action_required neutral stale], want failure,timed_out,startup_failure`

- [ ] **Step 3: Write the implementation**

In `internal/model/model.go`, replace:

```go
// Failed reports whether a job conclusion counts as a failure: anything that
// is not a success, a cancellation, a skip or "unknown", ghr's own conclusion
// for a job whose result it never learned.
func Failed(conclusion string) bool {
	switch conclusion {
	case "success", "cancelled", "skipped", "unknown":
		return false
	}
	return true
}
```

with:

```go
// Failed reports whether a job conclusion counts as a failure: anything that
// is not a success, a cancellation, a skip, one of GitHub's "neutral", "stale"
// and "action_required", or "unknown", ghr's own conclusion for a job whose
// result it never learned. A conclusion GitHub adds later counts as a failure
// until it is listed here, so it cannot look harmless. The web UI's
// resultState keeps the same list.
func Failed(conclusion string) bool {
	switch conclusion {
	case "success", "cancelled", "skipped", "unknown", "neutral", "stale", "action_required":
		return false
	}
	return true
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test -count=1 ./internal/model/ ./internal/history/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/model` and `ok  	github.com/darkraise/ghr/internal/history`)

Then run: `gofmt -l internal/model internal/history`
Expected: no output

- [ ] **Step 5: Commit**

```bash
git add internal/model/model.go internal/model/model_test.go internal/history/history_test.go
git commit -m "fix(model): stop counting neutral results as failed"
```

### Task 64: Pin how the charts count and name the three results

**Files:**
- Test: `internal/activity/buckets_test.go`, `internal/activity/activity_test.go`

**Interfaces:**
- Consumes: `model.Failed` answering `false` for `neutral`, `stale` and `action_required` (Task 63)
- Produces: two facts the web UI relies on, now pinned by tests. A finished job with one of the three conclusions is counted in a bucket's `cancelled` number, as a skipped job is; and its segment in the lanes chart carries the conclusion's own name as its `state` (`"neutral"`, `"stale"`, `"action_required"`).

**Items:** 78
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

This task adds tests only. No source file changes, and `internal/activity/buckets.go` and `internal/activity/activity.go` must not be edited.

The activity charts get their numbers from `tally` in `internal/activity/buckets.go` and their segment states from `finishedState` in `internal/activity/activity.go`. Both ask `model.Failed`. `tally` counts a result that is neither succeeded, failed nor unknown with the cancelled ones, through its `default` branch; `finishedState` returns such a conclusion unchanged. Since the previous task, `neutral`, `stale` and `action_required` take those two paths instead of the failed one. No count is added to the API for them. These tests hold that in place, so a later change to `model.Failed` or to `tally` cannot move the three results silently.

Because the behaviour already changed in the previous task, both tests pass as soon as they are written. Before that change they failed with `neutral: state failed, want neutral` and with `counts {… Succeeded:1 Failed:5 Cancelled:2 Unknown:1 …}`.

- [ ] **Step 1: Extend the two tests**

In `internal/activity/buckets_test.go`, in `TestCountsByConclusion`, replace:

```go
		job("e", at(13, 0), at(13, 9), "unknown"),
	}})
	if b := bucketAt(t, a, at(13, 0)); b.Succeeded != 1 || b.Failed != 1 || b.Cancelled != 2 || b.Unknown != 1 {
		t.Fatalf("counts %+v", b)
	}
}
```

with:

```go
		job("e", at(13, 0), at(13, 9), "unknown"),
		job("f", at(13, 0), at(13, 10), "timed_out"),
		job("g", at(13, 0), at(13, 11), "neutral"),
		job("h", at(13, 0), at(13, 12), "stale"),
		job("i", at(13, 0), at(13, 13), "action_required"),
	}})
	if b := bucketAt(t, a, at(13, 0)); b.Succeeded != 1 || b.Failed != 2 || b.Cancelled != 5 || b.Unknown != 1 {
		t.Fatalf("counts %+v", b)
	}
}
```

The five cancelled are `cancelled`, `skipped`, `neutral`, `stale` and `action_required`; the two failed are `failure` and `timed_out`.

In `internal/activity/activity_test.go`, in `TestConclusionStatesAndAbsentURL`, replace:

```go
		"success": "succeeded", "failure": "failed", "timed_out": "failed", "cancelled": "cancelled", "skipped": "skipped", "unknown": "unknown",
	} {
```

with:

```go
		"success": "succeeded", "failure": "failed", "timed_out": "failed", "cancelled": "cancelled", "skipped": "skipped", "unknown": "unknown",
		"neutral": "neutral", "stale": "stale", "action_required": "action_required",
	} {
```

- [ ] **Step 2: Run the tests to verify they pass**

Run: `go test -count=1 ./internal/activity/`
Expected: PASS (`ok  	github.com/darkraise/ghr/internal/activity`)

If `TestCountsByConclusion` fails with `Failed:5 Cancelled:2`, the previous task's change to `model.Failed` is missing: stop and report it; do not edit `internal/activity/buckets.go`.

Then run: `gofmt -l internal/activity`
Expected: no output

- [ ] **Step 3: Commit**

```bash
git add internal/activity/buckets_test.go internal/activity/activity_test.go
git commit -m "test(activity): pin the tally of non-failure results"
```

### Task 65: The lanes chart knows the three results

**Files:**
- Modify: `web/src/lib/activity-view.ts` (`JOB_STATES`, `RESULT`), `web/src/components/lanes-chart.tsx` (`SEGMENT`)
- Test: `web/src/components/lanes-chart.test.tsx`

**Interfaces:**
- Consumes: the segment states `"neutral"`, `"stale"` and `"action_required"`, which the daemon sends for a finished job with that conclusion (Tasks 63 and 64)
- Produces: nothing

**Items:** 78
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

The lanes chart on the Dashboard and the repository page draws one box per segment of a runner's life and names it for screen readers. It has three tables keyed by segment state: the states that mean "a job ran" (`JOB_STATES`), the word for each state (`RESULT`) and the style of each state (`SEGMENT`). Until now the daemon sent a `neutral`, `stale` or `action_required` job as `failed`. It now sends the conclusion's own name, which none of the three tables holds: such a job would not count as a job (a chart holding only that job says "No jobs ran in the last hour."), and `action_required` would be read out with its underscore.

The three tables gain the three names. The style is the muted one `cancelled` and `skipped` use. It equals the fallback the chart applies to a state it does not know, so the drawing itself does not change; the entries make the style a decision instead of an accident.

- [ ] **Step 1: Write the failing test**

In `web/src/components/lanes-chart.test.tsx`, insert this test directly above `it("draws a stopping run as a dashed grey outline with no pulse", …)`:

```tsx
  it.each([
    ["neutral", "neutral"],
    ["stale", "stale"],
    ["action_required", "action required"],
  ])("draws a job that ended %s muted, with its own word", (state, word) => {
    const { container } = draw({
      lanes: [
        { runs: [{ instance_id: "cccccc", repo: "darkmem", job: "docs", segments: [{ state, from: "2026-10-03T13:08:00Z", to: "2026-10-03T13:09:00Z" }] }] },
      ],
    })
    const box = container.querySelector(`rect[data-state="${state}"]`)
    expect(box).toHaveClass("fill-muted", "stroke-muted-foreground")
    expect(box).not.toHaveAttribute("stroke-dasharray")
    expect(screen.getByRole("img", { name: `darkmem, docs, ${word}, 1m00s` })).toBeInTheDocument()
    expect(screen.queryByText("No jobs ran in the last hour.")).toBeNull()
  })

```

`draw` is the helper at the top of that file.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/lanes-chart.test.tsx`
Expected: FAIL, 3 of 17 tests:
- the `neutral` and `stale` rows at the last line, `expected SVGElement{ …(2), …(2) } to be null`: the chart says no jobs ran
- the `action_required` row with `Unable to find an accessible element with the role "img" and name "darkmem, docs, action required, 1m00s"`

- [ ] **Step 3: Write the implementation**

In `web/src/lib/activity-view.ts`, replace:

```ts
const JOB_STATES = new Set(["running", "finishing", "succeeded", "failed", "cancelled", "skipped", "unknown"])
```

with:

```ts
const JOB_STATES = new Set(["running", "finishing", "succeeded", "failed", "cancelled", "skipped", "unknown", "neutral", "stale", "action_required"])
```

and in the `RESULT` map of the same file, replace:

```ts
  unknown: "result unknown",
}
```

with:

```ts
  unknown: "result unknown",
  neutral: "neutral",
  stale: "stale",
  action_required: "action required",
}
```

In `web/src/components/lanes-chart.tsx`, in the `SEGMENT` map, replace:

```ts
  unknown: "fill-muted stroke-muted-foreground",
}
```

with:

```ts
  unknown: "fill-muted stroke-muted-foreground",
  neutral: "fill-muted stroke-muted-foreground",
  stale: "fill-muted stroke-muted-foreground",
  action_required: "fill-muted stroke-muted-foreground",
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/lanes-chart.test.tsx src/lib/activity-view.test.ts`
Expected: PASS (`Tests  36 passed (36)`). `activity-view.test.ts` is included because `hasJob`, `runAria` and `activitySummary` read the two tables.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/activity-view.ts web/src/components/lanes-chart.tsx web/src/components/lanes-chart.test.tsx
git commit -m "fix(web): draw the three new results in the lanes"
```

### Task 66: Encode the segments of GitHub links

**Files:**
- Modify: `web/src/lib/run-details.ts` (`githubLinks`)
- Test: `web/src/lib/run-details.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing new. `githubLinks(owner, run)` keeps its signature and its three results (`commit`, `workflowFile`, `pull`).

**Items:** 105
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

`githubLinks` builds the GitHub addresses of a run's commit, its workflow file and its pull requests by pasting the owner, the repository name, the commit SHA and the workflow path into a string. Nothing is encoded, so a workflow file named `build and test.yml` gives an address with a raw space, and a `#` or `?` in a name cuts the address short. Each path segment is now passed through `encodeURIComponent`. The workflow path is split on `/` first, so its own slashes stay slashes.

- [ ] **Step 1: Write the failing test**

In `web/src/lib/run-details.test.tsx`, insert this test directly below the test `builds GitHub links from the owner` (above `it("labels attempts", …)`):

```tsx
  it("encodes each path segment of a GitHub link", () => {
    const links = githubLinks("dark raise", { ...run, repo: "dark#mem", head_sha: "a?b", path: ".github/workflows/build and test#1.yml" })
    expect(links.commit).toBe("https://github.com/dark%20raise/dark%23mem/commit/a%3Fb")
    expect(links.workflowFile).toBe("https://github.com/dark%20raise/dark%23mem/blob/a%3Fb/.github/workflows/build%20and%20test%231.yml")
    expect(links.pull(87)).toBe("https://github.com/dark%20raise/dark%23mem/pull/87")
  })

```

`run` is `fixtures.runDetail`, defined at the top of that file.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/lib/run-details.test.tsx`
Expected: FAIL, 1 of 30 tests, with `expected 'https://github.com/dark raise/dark#me…' to be 'https://github.com/dark%20raise/dark%…'`

- [ ] **Step 3: Write the implementation**

In `web/src/lib/run-details.ts`, replace the body of `githubLinks`:

```ts
  const base = `https://github.com/${owner}/${run.repo}`
  return {
    commit: `${base}/commit/${run.head_sha}`,
    workflowFile: run.path.startsWith(".github/workflows/") ? `${base}/blob/${run.head_sha}/${run.path}` : undefined,
    pull: (n) => `${base}/pull/${n}`,
  }
}
```

with:

```ts
  const part = encodeURIComponent
  const base = `https://github.com/${part(owner)}/${part(run.repo)}`
  const file = run.path.split("/").map(part).join("/")
  return {
    commit: `${base}/commit/${part(run.head_sha)}`,
    workflowFile: run.path.startsWith(".github/workflows/") ? `${base}/blob/${part(run.head_sha)}/${file}` : undefined,
    pull: (n) => `${base}/pull/${n}`,
  }
}
```

The signature line above it stays as it is.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib/run-details.test.tsx src/components/run-frame.test.tsx`
Expected: PASS (`Tests  49 passed (49)`). `run-frame.test.tsx` is included because the run frame renders the workflow-file link.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/run-details.ts web/src/lib/run-details.test.tsx
git commit -m "fix(web): encode the segments of GitHub links"
```

### Task 67: One https check for run links

**Files:**
- Create: `web/src/lib/links.ts`, `web/src/lib/links.test.ts`
- Modify: `web/src/components/lanes-chart.tsx` (`RunMark`, one import)

**Interfaces:**
- Consumes: nothing
- Produces: `httpsUrl(url: string | undefined): string | undefined` exported from `web/src/lib/links.ts`. It returns the address when it starts with `https://` and `undefined` otherwise. The next three tasks use it, and a later task adds a second helper to the same file.

**Items:** 105
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

Four places render a link to an `html_url` the daemon passed on from GitHub. Only the lanes chart checks that the address is `https` before it makes it a link; the History table, the run page header and the runner panel link whatever they are given. This task moves the lanes chart's check into one helper and makes the lanes chart use it. Its behaviour does not change, and its two existing tests (`links a finished job to GitHub`, `does not link a run whose URL is not https`) keep covering it. The next three tasks bring the helper to the other three places.

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/links.test.ts` with exactly this content:

```ts
import { describe, expect, it } from "vitest"
import { httpsUrl } from "./links"

describe("httpsUrl", () => {
  it("passes an https address through", () => {
    expect(httpsUrl("https://github.com/darkraise/darkmem/actions/runs/101")).toBe("https://github.com/darkraise/darkmem/actions/runs/101")
  })

  it.each([["http://github.com/darkraise/darkmem"], ["javascript:alert(1)"], ["//github.com/darkraise"], ["/actions"], [""], [undefined]])(
    "gives no address for %s",
    (url) => {
      expect(httpsUrl(url)).toBeUndefined()
    },
  )
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/lib/links.test.ts`
Expected: FAIL with `Error: Failed to resolve import "./links" from "src/lib/links.test.ts". Does the file exist?`

- [ ] **Step 3: Write the implementation**

Create `web/src/lib/links.ts` with exactly this content:

```ts
// An address GitHub gave the daemon is only ever linked when it is https, so
// a javascript: or http: address can never become a link.
export function httpsUrl(url: string | undefined): string | undefined {
  return url?.startsWith("https://") ? url : undefined
}
```

In `web/src/components/lanes-chart.tsx`, replace:

```tsx
import { fmtMem } from "@/lib/format"
```

with:

```tsx
import { fmtMem } from "@/lib/format"
import { httpsUrl } from "@/lib/links"
```

and in `RunMark`, replace:

```tsx
  if (run.html_url?.startsWith("https://")) {
    return (
      <a href={run.html_url} target="_blank" rel="noreferrer" aria-label={label} className="ghr-mark">
```

with:

```tsx
  const url = httpsUrl(run.html_url)
  if (url) {
    return (
      <a href={url} target="_blank" rel="noreferrer" aria-label={label} className="ghr-mark">
```

The lines after it (`{body}`, `</a>`, `)`, `}` and the `<g …>` fallback) stay as they are.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib/links.test.ts src/components/lanes-chart.test.tsx`
Expected: PASS (`Tests  24 passed (24)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/links.ts web/src/lib/links.test.ts web/src/components/lanes-chart.tsx
git commit -m "feat(web): add one https check for run links"
```

### Task 68: The run page links to GitHub only over https

**Files:**
- Modify: `web/src/components/run-frame.tsx` (`RunFrame`, one import)
- Test: `web/src/components/run-frame.test.tsx`

**Interfaces:**
- Consumes: `httpsUrl(url: string | undefined): string | undefined` from `web/src/lib/links.ts` (Task 67)
- Produces: nothing

**Items:** 105
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

The header of a run page has an "Open on GitHub" button whose address is the run's `html_url`, used as it arrives. The button is now rendered only when that address is `https`; with any other address there is no button. The pill and the attempt picker beside it are not affected.

- [ ] **Step 1: Write the failing test**

In `web/src/components/run-frame.test.tsx`, insert this test directly below the test `opens the run on GitHub in a new tab`:

```tsx
  it("offers no GitHub link for a run address that is not https", () => {
    renderFrame({}, { ...fixtures.runDetail, html_url: "http://github.com/darkraise/darkmem/actions/runs/102" })
    expect(screen.queryByRole("link", { name: "Open on GitHub" })).toBeNull()
    expect(screen.getByRole("combobox", { name: "Attempt" })).toBeInTheDocument()
  })

```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/run-frame.test.tsx`
Expected: FAIL, 1 of 20 tests, with `expected <a …(6)></a> to be null`

- [ ] **Step 3: Write the implementation**

In `web/src/components/run-frame.tsx`, replace:

```tsx
import { dur } from "@/lib/format"
```

with:

```tsx
import { dur } from "@/lib/format"
import { httpsUrl } from "@/lib/links"
```

In `RunFrame`, replace:

```tsx
  const workflowFile = githubLinks(owner, run).workflowFile
```

with:

```tsx
  const workflowFile = githubLinks(owner, run).workflowFile
  const runUrl = httpsUrl(run.html_url)
```

and replace:

```tsx
      <Button variant="outline" asChild>
        <a href={run.html_url} target="_blank" rel="noreferrer">
          Open on GitHub
        </a>
      </Button>
    </div>
```

with:

```tsx
      {runUrl && (
        <Button variant="outline" asChild>
          <a href={runUrl} target="_blank" rel="noreferrer">
            Open on GitHub
          </a>
        </Button>
      )}
    </div>
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/run-frame.test.tsx`
Expected: PASS (`Tests  20 passed (20)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/components/run-frame.tsx web/src/components/run-frame.test.tsx
git commit -m "fix(web): link a run to GitHub only over https"
```

### Task 69: History links a job to GitHub only over https

**Files:**
- Modify: `web/src/pages/history.tsx` (`HistoryRow`, one import)
- Test: `web/src/pages/history.test.tsx`

**Interfaces:**
- Consumes: `httpsUrl(url: string | undefined): string | undefined` from `web/src/lib/links.ts` (Task 67)
- Produces: nothing

**Items:** 105
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

Each row of the History table ends with two buttons when the job has an `html_url`: "Open run", a link to that address, and "Copy run URL". The address is used as it arrives. Both buttons are now shown only when the address is `https`; a row with any other address shows neither, exactly as a row without an address does today.

- [ ] **Step 1: Write the failing test**

In `web/src/pages/history.test.tsx`, add this test as the last one of `describe("History page", …)`, directly below the test `copies a run's URL`:

```tsx
  it("offers no run link for an address that is not https", async () => {
    const [build] = fixtures.history as [HistoryEntry]
    mockApi(authedRoutes({ "GET /api/history": [{ ...build, html_url: "http://github.com/darkraise/darkmem/actions/runs/101/job/1" }] }))
    renderApp("/history")
    await screen.findByText("#41")
    expect(screen.queryByRole("link", { name: /^Open run/ })).toBeNull()
    expect(screen.queryByRole("button", { name: /^Copy run URL/ })).toBeNull()
  })
```

`HistoryEntry`, `fixtures`, `mockApi`, `authedRoutes` and `renderApp` are already imported at the top of that file.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/pages/history.test.tsx -t "not https"`
Expected: FAIL, 1 test, with `expected <a …(9)><svg …(11)>…(3)</svg></a> to be null`

- [ ] **Step 3: Write the implementation**

In `web/src/pages/history.tsx`, replace:

```tsx
} from "@/lib/history"
```

with:

```tsx
} from "@/lib/history"
import { httpsUrl } from "@/lib/links"
```

and in `HistoryRow`, replace:

```tsx
  const url = entry.html_url
```

with:

```tsx
  const url = httpsUrl(entry.html_url)
```

The rest of `HistoryRow` already renders the two buttons only when `url` is set.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/history.test.tsx`
Expected: PASS (`Tests  21 passed (21)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/history.tsx web/src/pages/history.test.tsx
git commit -m "fix(web): link a History job only over https"
```

### Task 70: The runner panel links to a run only over https

**Files:**
- Modify: `web/src/components/runner-panel.tsx` (the `runUrl` line of `RunnerPanel`, one import)
- Test: `web/src/components/runner-panel.test.tsx`

**Interfaces:**
- Consumes: `httpsUrl(url: string | undefined): string | undefined` from `web/src/lib/links.ts` (Task 67)
- Produces: nothing

**Items:** 105
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

The runner panel shows an "Open run" button, and a "Copy run URL" entry in its menu, when the runner's job has an `html_url`. The address is used as it arrives. It now passes through `httpsUrl`, so an address that is not `https` gives neither the button nor the menu entry; both already disappear when there is no address.

Other tasks change other parts of `web/src/components/runner-panel.tsx` and of its test file. Change only the two lines named below, add only the one test, and do not reformat either file.

- [ ] **Step 1: Write the failing test**

In `web/src/components/runner-panel.test.tsx`, insert this test directly below the first test of `describe("RunnerPanel", …)` (`shows the runner's facts, its steps and a link to its run`):

```tsx
  it("offers no run link for a job address that is not https", async () => {
    mockApi(
      routes({
        "GET /api/status": {
          ...fixtures.status,
          instances: fixtures.status.instances.map((i) =>
            i.id === "aaaaaa" && i.job ? { ...i, job: { ...i.job, html_url: "http://github.com/darkraise/darkmem/actions/runs/102/job/2" } } : i,
          ),
        },
      }),
    )
    draw()
    expect(await screen.findByText("Running for")).toBeInTheDocument()
    expect(screen.queryByRole("link", { name: "Open run" })).toBeNull()
  })

```

`routes` and `draw` are the helpers at the top of that file. The test waits for `Running for`, which the panel shows only once it has the runner, so the link's absence is not just an unloaded panel.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/runner-panel.test.tsx -t "not https"`
Expected: FAIL, 1 test, with `expected <a …(6)><svg …(11)>…(3)</svg></a> to be null`

- [ ] **Step 3: Write the implementation**

In `web/src/components/runner-panel.tsx`, replace:

```tsx
import { dateTimeSec, dur, startedAt } from "@/lib/format"
```

with:

```tsx
import { dateTimeSec, dur, startedAt } from "@/lib/format"
import { httpsUrl } from "@/lib/links"
```

and in `RunnerPanel`, replace:

```tsx
  const runUrl = job?.html_url
```

with:

```tsx
  const runUrl = httpsUrl(job?.html_url)
```

If the import line from `@/lib/format` names other functions by the time this task runs, keep that line as it is and only add the `@/lib/links` line below it.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/runner-panel.test.tsx`
Expected: PASS (`Tests  15 passed (15)`; more if other tasks added tests to this file)

- [ ] **Step 5: Commit**

```bash
git add web/src/components/runner-panel.tsx web/src/components/runner-panel.test.tsx
git commit -m "fix(web): link a runner's run only over https"
```

### Task 71: A modifier click on a Dashboard repository goes to the browser

**Files:**
- Modify: `web/src/lib/links.ts`, `web/src/components/repo-table.tsx`
- Test: `web/src/components/repo-table.test.tsx`

**Interfaces:**
- Consumes: the file `web/src/lib/links.ts` (Task 67)
- Produces: `plainClick(e: { button: number; metaKey: boolean; ctrlKey: boolean; shiftKey: boolean; altKey: boolean }): boolean` exported from `web/src/lib/links.ts`. It is `true` only for a click with the main button and no modifier key. The next task uses it.

**Items:** 90
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

A repository's name in the Dashboard table is a real link (`/repositories/<name>`), but its click handler always calls `preventDefault` and opens the repository in the current tab. So Ctrl click, Cmd click, Shift click and Alt click, which a browser uses to open a link in a new tab or window or to download it, do nothing special.

The handler now steps aside for such a click: it does not call `preventDefault` and does not call `onOpen`, so the browser follows the link's own address its own way. It still stops the click from reaching the table row. The row has its own click handler that opens the repository in the current tab; without the stop, a Ctrl click would open the new tab and also navigate the current one.

- [ ] **Step 1: Write the failing test**

All edits are in `web/src/components/repo-table.test.tsx`.

Change the first import from:

```tsx
import { render, screen, waitFor, within } from "@testing-library/react"
```

to:

```tsx
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react"
```

Replace the end of the `draw` function:

```tsx
  const user = userEvent.setup()
  render(
    <TooltipProvider>
      <RepoTable repos={repos} activity={fixtures.activityLanes.repos} now={now} offline={offline} onOpen={onOpen} onEdit={onEdit} />
    </TooltipProvider>,
    { wrapper },
  )
  return { onOpen, onEdit, user }
}
```

with this, which also adds a helper below it:

```tsx
  const user = userEvent.setup()
  const { container } = render(
    <TooltipProvider>
      <RepoTable repos={repos} activity={fixtures.activityLanes.repos} now={now} offline={offline} onOpen={onOpen} onEdit={onEdit} />
    </TooltipProvider>,
    { wrapper },
  )
  return { onOpen, onEdit, user, container }
}

// Clicks a link and says whether the table left the click to the browser.
// jsdom logs an error when a click navigates, so the click is cancelled at
// React's root, after the table's own handlers have run.
function leftToBrowser(container: HTMLElement, link: HTMLElement, init: MouseEventInit = {}): boolean {
  let left = false
  container.addEventListener("click", (e) => {
    left = !e.defaultPrevented
    e.preventDefault()
  })
  fireEvent.click(link, init)
  return left
}
```

Insert these two tests directly below the test `opens a repository from its name, its row or Edit`:

```tsx
  it("handles a plain click on a repository's name itself", () => {
    const { onOpen, container } = draw()
    expect(leftToBrowser(container, screen.getByRole("link", { name: "darkcloud" }))).toBe(false)
    expect(onOpen).toHaveBeenCalledTimes(1)
  })

  it.each([
    ["Ctrl", { ctrlKey: true }],
    ["Cmd", { metaKey: true }],
    ["Shift", { shiftKey: true }],
    ["Alt", { altKey: true }],
    ["another button", { button: 1 }],
  ])("leaves a click with %s on a repository's name to the browser", (_how, init) => {
    const { onOpen, container } = draw()
    expect(leftToBrowser(container, screen.getByRole("link", { name: "darkcloud" }), init)).toBe(true)
    expect(onOpen).not.toHaveBeenCalled()
  })

```

`onOpen` not being called at all also proves the row did not navigate: the row's handler calls the same `onOpen`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/repo-table.test.tsx`
Expected: FAIL, 5 of 17 tests (the five `leaves a click with …` rows), each with `expected false to be true`. The plain-click test already passes.

- [ ] **Step 3: Write the implementation**

In `web/src/lib/links.ts`, append below `httpsUrl`:

```ts

// False for a click the browser must handle itself: with a modifier key or
// another button it opens the link's own address in a new tab or window.
export function plainClick(e: { button: number; metaKey: boolean; ctrlKey: boolean; shiftKey: boolean; altKey: boolean }): boolean {
  return e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey
}
```

In `web/src/components/repo-table.tsx`, replace:

```tsx
import { ago } from "@/lib/format"
```

with:

```tsx
import { ago } from "@/lib/format"
import { plainClick } from "@/lib/links"
```

and replace the link's handler:

```tsx
                  onClick={(e) => {
                    e.preventDefault()
                    e.stopPropagation()
                    onOpen(r.name)
                  }}
```

with:

```tsx
                  onClick={(e) => {
                    // The row opens the repository in this tab on any click
                    // that reaches it, so the click stops here either way.
                    e.stopPropagation()
                    if (!plainClick(e)) return
                    e.preventDefault()
                    onOpen(r.name)
                  }}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/repo-table.test.tsx src/lib/links.test.ts`
Expected: PASS (`Tests  24 passed (24)`), and the output holds no `Not implemented: navigation` line.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/links.ts web/src/components/repo-table.tsx web/src/components/repo-table.test.tsx
git commit -m "fix(web): leave modifier clicks on a repo to the browser"
```

### Task 72: A modifier click on a live run in the lanes chart goes to the browser

**Files:**
- Modify: `web/src/components/lanes-chart.tsx` (`RunMark`, one import)
- Test: `web/src/components/lanes-chart.test.tsx`

**Interfaces:**
- Consumes: `plainClick(e): boolean` from `web/src/lib/links.ts` (Task 71)
- Produces: nothing

**Items:** 90
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

In the lanes chart, the mark of a run that is still live is a real link to its runner page (`/runners/<id>`), but its click handler always calls `preventDefault` and opens the runner in the current tab. A click with Ctrl, Cmd, Shift or Alt, or with another button, is now left to the browser, which follows the link's own address its own way. A plain click keeps opening the runner in the current tab.

- [ ] **Step 1: Write the failing test**

In `web/src/components/lanes-chart.test.tsx`, change the first import from:

```tsx
import { render, screen } from "@testing-library/react"
```

to:

```tsx
import { fireEvent, render, screen } from "@testing-library/react"
```

Insert these two tests directly below the test `opens the runner page from a live run`:

```tsx
  it.each([
    ["Ctrl", { ctrlKey: true }],
    ["Cmd", { metaKey: true }],
    ["Shift", { shiftKey: true }],
    ["Alt", { altKey: true }],
    ["another button", { button: 1 }],
  ])("leaves a click with %s on a live run to the browser", (_how, init) => {
    const { onOpenRunner } = draw()
    const link = screen.getByRole("link", { name: "darkmem, ci, test, run 42, running, 4m00s" })
    expect(link).toHaveAttribute("href", "/runners/aaaaaa")
    expect(fireEvent.click(link, init)).toBe(true)
    expect(onOpenRunner).not.toHaveBeenCalled()
  })

  it("handles a plain click on a live run itself", () => {
    const { onOpenRunner } = draw()
    expect(fireEvent.click(screen.getByRole("link", { name: "darkmem, ci, test, run 42, running, 4m00s" }))).toBe(false)
    expect(onOpenRunner).toHaveBeenCalledTimes(1)
  })

```

`fireEvent.click` returns `false` when a handler called `preventDefault`, and `true` when the click was left alone.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/lanes-chart.test.tsx`
Expected: FAIL, 5 of 23 tests (the five `leaves a click with …` rows), each with `expected false to be true`. The plain-click test already passes.

- [ ] **Step 3: Write the implementation**

In `web/src/components/lanes-chart.tsx`, replace:

```tsx
import { httpsUrl } from "@/lib/links"
```

with:

```tsx
import { httpsUrl, plainClick } from "@/lib/links"
```

and in `RunMark`, replace:

```tsx
        onClick={(e) => {
          e.preventDefault()
          onOpenRunner(run.instance_id)
        }}
```

with:

```tsx
        onClick={(e) => {
          if (!plainClick(e)) return
          e.preventDefault()
          onOpenRunner(run.instance_id)
        }}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/lanes-chart.test.tsx`
Expected: PASS (`Tests  23 passed (23)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/components/lanes-chart.tsx web/src/components/lanes-chart.test.tsx
git commit -m "fix(web): leave modifier clicks on a lane to the browser"
```

### Task 73: The History table keeps its rows when a poll fails

**Files:**
- Modify: `web/src/pages/history.tsx` (`HistoryPage`)
- Test: `web/src/pages/history.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 84
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The History page polls `/api/history`. When one poll fails, the query library keeps the rows it had and also reports the error. The page tests for the error before it looks at the rows, so a single failed poll replaces the whole table with an error line, and that line has no retry button.

After this task the page keeps what it has. With rows on screen, a failed poll adds an error line with a "Try again" button in a band above the rows, the way the page already shows its "Showing …" notes. With nothing loaded yet, the error line takes the table's place as today, and now also has the button.

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/history.test.tsx`, two existing tests describe the old behaviour: `shows a failed read` has no retry, and `pads a failed refresh that leaves the earlier rows behind` asserts that a failed refresh pads the error line in place of the rows. Replace both tests:

```tsx
  it("shows a failed read", async () => {
    mockApi(authedRoutes({ "GET /api/history": () => json({ error: "history file unreadable" }, 500) }))
    renderApp("/history")
    expect(within(await screen.findByRole("alert")).getByText("history file unreadable")).toBeInTheDocument()
  })

  it("pads a failed refresh that leaves the earlier rows behind", async () => {
    let fail = false
    mockApi(authedRoutes({ "GET /api/history": () => (fail ? json({ error: "history file unreadable" }, 500) : fixtures.history) }))
    const { queryClient } = renderApp("/history")
    await screen.findByRole("table")
    fail = true
    await queryClient.invalidateQueries({ queryKey: ["history"] })
    const alert = await screen.findByRole("alert")
    expect(within(alert).getByText("history file unreadable")).toBeInTheDocument()
    expect(alert.closest(".p-4")).not.toBeNull()
  })
```

with these two:

```tsx
  it("shows a failed first read with a retry", async () => {
    let fail = true
    mockApi(authedRoutes({ "GET /api/history": () => (fail ? json({ error: "history file unreadable" }, 500) : fixtures.history) }))
    const { user } = renderApp("/history")
    const alert = await screen.findByRole("alert")
    expect(within(alert).getByText("history file unreadable")).toBeInTheDocument()
    expect(alert.closest(".p-4")).not.toBeNull()
    fail = false
    await user.click(within(alert).getByRole("button", { name: "Try again" }))
    expect(await screen.findByText("#7")).toBeInTheDocument()
    expect(screen.queryByRole("alert")).toBeNull()
  })

  it("keeps the rows when a refresh fails and offers a retry above them", async () => {
    let fail = false
    mockApi(authedRoutes({ "GET /api/history": () => (fail ? json({ error: "history file unreadable" }, 500) : fixtures.history) }))
    const { queryClient, user } = renderApp("/history")
    const table = await screen.findByRole("table")
    fail = true
    await queryClient.invalidateQueries({ queryKey: ["history"] })
    const alert = await screen.findByRole("alert")
    expect(within(alert).getByText("history file unreadable")).toBeInTheDocument()
    expect(within(table).getByText("#7")).toBeInTheDocument()
    expect(screen.getByText("2 jobs, 1 failed, median 3m15s")).toBeInTheDocument()
    expect(alert.compareDocumentPosition(table) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    fail = false
    await user.click(within(alert).getByRole("button", { name: "Try again" }))
    await waitFor(() => expect(screen.queryByRole("alert")).toBeNull())
    expect(screen.getByText("#7")).toBeInTheDocument()
  })
```

`Try again` is the label the shared `ErrorLine` component gives its retry button.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/pages/history.test.tsx -t "retry"`
Expected: FAIL, 2 tests:
- `shows a failed first read with a retry` with `Unable to find an accessible element with the role "button" and name "Try again"`
- `keeps the rows when a refresh fails and offers a retry above them` at `expect(within(table).getByText("#7")).toBeInTheDocument()`, because the table has been removed from the page

- [ ] **Step 3: Write the implementation**

All three edits are in `HistoryPage` in `web/src/pages/history.tsx`.

Replace:

```tsx
  let body
  if (history.isError) body = <ErrorLine>{errorText(history.error)}</ErrorLine>
  else if (!settled) {
    body = activity.isError ? <p className="text-sm text-muted-foreground">The table loads once the chart does.</p> : <Spinner label="Loading" />
```

with:

```tsx
  const failure = history.isError ? <ErrorLine onRetry={() => void history.refetch()}>{errorText(history.error)}</ErrorLine> : null

  let body
  if (!settled) {
    body =
      failure ??
      (activity.isError ? <p className="text-sm text-muted-foreground">The table loads once the chart does.</p> : <Spinner label="Loading" />)
```

The `} else if (rows.length === 0) {` branch and the table branch below it stay as they are.

Replace:

```tsx
      <Section title="Finished jobs" aside={settled ? <span className="text-sm text-muted-foreground">{historySummary(rows)}</span> : undefined} flush>
        {picked && (
```

with:

```tsx
      <Section title="Finished jobs" aside={settled ? <span className="text-sm text-muted-foreground">{historySummary(rows)}</span> : undefined} flush>
        {failure && settled && <div className="border-b border-border px-4 py-2.5">{failure}</div>}
        {picked && (
```

Replace:

```tsx
        {!history.isError && settled && rows.length > 0 ? body : <div className="p-4">{body}</div>}
```

with:

```tsx
        {settled && rows.length > 0 ? body : <div className="p-4">{body}</div>}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/history.test.tsx`
Expected: PASS (`Tests  21 passed (21)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/history.tsx web/src/pages/history.test.tsx
git commit -m "fix(web): keep History rows when a poll fails"
```

### Task 74: The History table keeps its rows when its window slides

**Files:**
- Modify: `web/src/pages/history.tsx` (`HistoryPage`)
- Test: `web/src/pages/history.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 96
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The History table reads the jobs finished since the start of the chart above it. That start, `since`, is the `from` of the chart's last answer, and it moves forward each time the chart's oldest bucket rolls out: once an hour for the 24-hour window, every six hours for 7 days, once a day for 30 days. `since` is part of the table's query key, so each move starts a new query. `useHistory` keeps the previous rows as placeholder data while the new ones load, but the page throws placeholder rows away, because rows of another repository or result would sit under the wrong controls. The effect: the table blanks to a spinner at every bucket boundary, although nothing the user controls changed.

The page now remembers which controls (repository, result, window) the last rows it really read belong to. Placeholder rows are shown when the controls are still those, which is exactly the case where only `since` moved, and hidden otherwise, as today. `useHistory` and `web/src/api/hooks.ts` do not change.

- [ ] **Step 1: Write the failing test**

In `web/src/pages/history.test.tsx`, insert these two tests directly below the test `hides the previous filter's rows while the next ones load`:

```tsx
  it("keeps the rows while the window moves to its next bucket", async () => {
    const [build] = fixtures.history as [HistoryEntry]
    let from = "2026-10-02T14:00:00Z"
    let release = () => {}
    const gate = new Promise<void>((resolve) => (release = resolve))
    const { calls } = mockApi(
      authedRoutes({
        "GET /api/activity": () => ({ ...fixtures.activityBuckets, from }),
        "GET /api/history": async ({ url }: { url: URL }) => {
          if (url.searchParams.get("since") === "2026-10-02T14:00:00Z") return fixtures.history
          await gate
          return [build]
        },
      }),
    )
    const { queryClient } = renderApp("/history")
    await screen.findByText("#7")
    from = "2026-10-02T15:00:00Z"
    await queryClient.invalidateQueries({ queryKey: ["activity"] })
    await waitFor(() => expect(historyCalls(calls)).toContain("?repo=&conclusion=&since=2026-10-02T15%3A00%3A00Z&limit=500"))
    expect(screen.getByText("#7")).toBeInTheDocument()
    expect(screen.getByText("2 jobs, 1 failed, median 3m15s")).toBeInTheDocument()
    release()
    expect(await screen.findByText("1 job, median 5m00s")).toBeInTheDocument()
    expect(screen.queryByText("#7")).toBeNull()
  })

  it("hides the previous window's rows while the next ones load", { timeout: 10_000 }, async () => {
    let release = () => {}
    const gate = new Promise<void>((resolve) => (release = resolve))
    const { calls } = mockApi(
      authedRoutes({
        "GET /api/activity": ({ url }: { url: URL }) => ({
          ...fixtures.activityBuckets,
          from: url.searchParams.get("window") === "24h" ? "2026-10-02T15:00:00Z" : "2026-10-02T14:00:00Z",
        }),
        "GET /api/history": async ({ url }: { url: URL }) => {
          if (url.searchParams.get("since") !== "2026-10-02T14:00:00Z") await gate
          return fixtures.history
        },
      }),
    )
    const { user } = renderApp("/history")
    await screen.findByText("#7")
    await user.click(screen.getByRole("radio", { name: "24h" }))
    await waitFor(() => expect(historyCalls(calls)).toContain("?repo=&conclusion=&since=2026-10-02T15%3A00%3A00Z&limit=500"))
    expect(screen.queryByText("#7")).toBeNull()
    expect(screen.queryByText("2 jobs, 1 failed, median 3m15s")).toBeNull()
    release()
    expect(await screen.findByText("#7")).toBeInTheDocument()
  })

```

The first test moves only the chart's `from`, holds the table's next answer back, and expects the old rows and their summary to stay until it arrives. The second changes the window with the 24h button and expects the old rows to be hidden while the next ones load; it passes today and guards the other half of the rule.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/pages/history.test.tsx -t "rows while"`
Expected: 1 failed, 2 passed. `keeps the rows while the window moves to its next bucket` fails with `Unable to find an element with the text: #7`. The two `hides the previous …` tests pass.

- [ ] **Step 3: Write the implementation**

In `HistoryPage` in `web/src/pages/history.tsx`, replace:

```tsx
  // Rows kept from the previous filter while the next ones load would sit
  // under the wrong controls.
  const settled = history.isPlaceholderData ? undefined : history.data
```

with:

```tsx
  // Rows kept while the next ones load stay on screen only when the window
  // slid to its next bucket. After a change of repository, result or window
  // they would sit under the wrong controls. shownFor names the controls the
  // last rows read from the daemon belong to.
  const controls = `${repo}\n${result}\n${span}`
  const [shownFor, setShownFor] = useState(controls)
  if (history.data && !history.isPlaceholderData && shownFor !== controls) setShownFor(controls)
  const settled = history.isPlaceholderData && shownFor !== controls ? undefined : history.data
```

`useState` is already imported. Setting state during render under a condition is React's way to adjust state to what a render sees; the condition is false on the render that follows, so it does not loop.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/history.test.tsx`
Expected: PASS (`Tests  23 passed (23)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/history.tsx web/src/pages/history.test.tsx
git commit -m "fix(web): keep History rows when the window slides"
```

### Task 75: A job with a zero start time has no duration

**Files:**
- Modify: `web/src/lib/history.ts` (`took`, `historySummary`, one new function, one import), `web/src/pages/history.tsx` (`HistoryRow`, `HistoryPage`, one import)
- Test: `web/src/pages/history.test.tsx`

**Interfaces:**
- Consumes: the existing `isZeroTime(iso: string | undefined): boolean` from `web/src/lib/format.ts`
- Produces: `took(entry: HistoryEntry): number | undefined` (it returned `number`) and the new `durations(rows: HistoryEntry[]): number[]`, both in `web/src/lib/history.ts`. Nothing later in this plan uses them.

**Items:** 95
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

A history entry's `started_at` can be Go's zero time, `0001-01-01T00:00:00Z`, when the daemon never learned when the job started. `took` subtracts it from the finish time anyway and gets about two thousand years. On the History page that number is shown as the job's duration, it becomes the scale of the duration bars, so every other bar shrinks to nothing, and it enters the median of the summary.

Such a job now has no duration. Its row shows the finish time and no duration or bar, and it is left out of the bar scale and of the median. When no listed job has a duration, the summary ends after the counts, with no median.

Do not touch `pickText` in `web/src/lib/history.ts`, and do not reformat that file: another task changes it.

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/history.test.tsx`, insert these two tests directly above the test `keys the chart's busy time in the series colour`:

```tsx
  it("gives a job with a zero start time no duration and keeps it out of the bars and the median", async () => {
    const [build, deploy] = fixtures.history as [HistoryEntry, HistoryEntry]
    mockApi(authedRoutes({ "GET /api/history": [build, { ...deploy, started_at: "0001-01-01T00:00:00Z" }] }))
    renderApp("/history")
    const unstarted = (await screen.findByText("#7")).closest("tr") as HTMLElement
    expect(within(unstarted).getByText("11:06")).toBeInTheDocument()
    expect(within(unstarted).queryByText(/^\d+[hm]\d+[ms]$/)).toBeNull()
    expect(unstarted.querySelector("[data-duration-bar]")).toBeNull()
    const started = screen.getByText("#41").closest("tr") as HTMLElement
    expect(within(started).getByText("5m00s")).toBeInTheDocument()
    expect(started.querySelector("[data-duration-bar] > span")).toHaveStyle({ width: "100%" })
    expect(screen.getByText("2 jobs, 1 failed, median 5m00s")).toBeInTheDocument()
  })

  it("leaves the median out when no job has a duration", async () => {
    const [, deploy] = fixtures.history as [HistoryEntry, HistoryEntry]
    mockApi(authedRoutes({ "GET /api/history": [{ ...deploy, started_at: "0001-01-01T00:00:00Z" }] }))
    renderApp("/history")
    expect(await screen.findByText("1 job, 1 failed")).toBeInTheDocument()
  })

```

In the fixture, `build` (run #41) ran from 13:35 to 13:40 and `deploy` (run #7) finished at 11:06:30 with the result `failure`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/pages/history.test.tsx -t "duration"`
Expected: 2 failed, 1 passed:
- `gives a job with a zero start time no duration …` with `expected <span …(1)>…(1)</span> to be null`: the row shows a duration
- `leaves the median out when no job has a duration` with `Unable to find an element with the text: 1 job, 1 failed`

- [ ] **Step 3: Give such a job no duration**

In `web/src/lib/history.ts`, replace:

```ts
import { dur, hhmm, monthDay, plural } from "@/lib/format"
```

with:

```ts
import { dur, hhmm, isZeroTime, monthDay, plural } from "@/lib/format"
```

Replace:

```ts
export function took(entry: HistoryEntry): number {
  return Date.parse(entry.finished_at) - Date.parse(entry.started_at)
}
```

with:

```ts
// A job whose start was never recorded carries Go's zero time and has no
// duration: measured from year 1 it would dwarf every real one.
export function took(entry: HistoryEntry): number | undefined {
  if (isZeroTime(entry.started_at)) return undefined
  return Date.parse(entry.finished_at) - Date.parse(entry.started_at)
}

export function durations(rows: HistoryEntry[]): number[] {
  return rows.flatMap((r) => took(r) ?? [])
}
```

In `historySummary` in the same file, replace:

```ts
  const mid = median(rows.map(took)) ?? 0
  return `${plural(rows.length, "job")}${failed > 0 ? `, ${failed} failed` : ""}, median ${dur(mid)}`
```

with:

```ts
  const mid = median(durations(rows))
  return `${plural(rows.length, "job")}${failed > 0 ? `, ${failed} failed` : ""}${mid === undefined ? "" : `, median ${dur(mid)}`}`
```

- [ ] **Step 4: Show it on the page**

In `web/src/pages/history.tsx`, in the import from `@/lib/history`, replace:

```tsx
import {
  groupByDay,
  HISTORY_WINDOWS,
```

with:

```tsx
import {
  durations,
  groupByDay,
  HISTORY_WINDOWS,
```

`took` stays in that import list; `HistoryRow` still uses it.

In `HistoryRow`, replace:

```tsx
          <span className="flex flex-col items-end gap-1">
            <RunValue kind="duration">{dur(ms)}</RunValue>
            <span data-duration-bar aria-hidden="true" className="block h-1.5 w-20 rounded-[2px] bg-muted">
              <span className="block h-1.5 rounded-[2px] bg-foreground/40" style={{ width: `${Math.round((ms / longest) * 100)}%` }} />
            </span>
          </span>
        </>
```

with:

```tsx
          {ms !== undefined && (
            <span className="flex flex-col items-end gap-1">
              <RunValue kind="duration">{dur(ms)}</RunValue>
              <span data-duration-bar aria-hidden="true" className="block h-1.5 w-20 rounded-[2px] bg-muted">
                <span className="block h-1.5 rounded-[2px] bg-foreground/40" style={{ width: `${Math.round((ms / longest) * 100)}%` }} />
              </span>
            </span>
          )}
        </>
```

In `HistoryPage`, replace:

```tsx
  const longest = Math.max(1, ...rows.map(took))
```

with:

```tsx
  const longest = Math.max(1, ...durations(rows))
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/history.test.tsx src/lib/history.test.ts`
Expected: PASS (`Tests  44 passed (44)`)

Then run: `cd web && npx tsc --noEmit`
Expected: no output. Without Step 4, it fails in `history.tsx` because `ms` may be `undefined`.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/history.ts web/src/pages/history.tsx web/src/pages/history.test.tsx
git commit -m "fix(web): give a job with no start no duration"
```

### Task 76: The History chart's total counts unknown results

**Files:**
- Modify: `web/src/components/buckets-chart.tsx` (`totalRuns`)
- Test: `web/src/components/buckets-chart.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 282
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The bucket chart on the History page and the Dashboard writes "No jobs ran in the last …" across itself when its run total is zero. The total adds each bucket's succeeded, failed and cancelled jobs and leaves out `unknown`, the count of jobs whose result ghr never learned. A window that holds only such jobs therefore says no jobs ran, while the summary beside the chart, which does add `unknown`, counts them. The total now adds `unknown` too.

- [ ] **Step 1: Write the failing test**

In `web/src/components/buckets-chart.test.tsx`, insert this test directly below the test `says when no jobs ran`:

```tsx
  it("counts a job whose result is unknown as a job that ran", () => {
    draw({ buckets: base.buckets.map((b, i) => ({ ...b, succeeded: 0, failed: 0, cancelled: 0, unknown: i === 0 ? 1 : 0 })) })
    expect(screen.queryByText("No jobs ran in the last 24 hours.")).toBeNull()
    expect(screen.getAllByRole("img")).toHaveLength(25)
  })

```

`draw` and `base` are defined at the top of that file.

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/components/buckets-chart.test.tsx`
Expected: FAIL, 1 of 17 tests, with `expected SVGElement{ …(2), …(2) } to be null`

- [ ] **Step 3: Write the implementation**

In `web/src/components/buckets-chart.tsx`, replace:

```tsx
  const totalRuns = buckets.reduce((n, b) => n + b.succeeded + b.failed + b.cancelled, 0)
```

with:

```tsx
  const totalRuns = buckets.reduce((n, b) => n + b.succeeded + b.failed + b.cancelled + b.unknown, 0)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/buckets-chart.test.tsx`
Expected: PASS (`Tests  17 passed (17)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/components/buckets-chart.tsx web/src/components/buckets-chart.test.tsx
git commit -m "fix(web): count unknown results in the chart total"
```

### Task 77: A token past its expiry reads Expired

**Files:**
- Modify: `web/src/lib/token.ts` (`tokenWord`), `web/src/lib/status.ts` (`tones`)
- Test: `web/src/lib/token.test.ts`

**Interfaces:**
- Consumes: nothing
- Produces: `tokenWord(t, now)` answers `"expired"` for a token in state `ok` whose `expires_at` is at or before `now`; `stateTone("expired")` is `"bad"`. The next task's test relies on both.

**Items:** 106
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The token page shows the token's state as a pill. For a token the daemon still calls `ok`, the pill reads "Expires soon", in the warning tone, when the expiry is less than 14 days away. A date that has already passed is also "less than 14 days away", so an expired token reads "Expires soon" while the text beside the pill says "Expired Oct 2". The daemon's state can lag: it only changes when a call to GitHub is refused.

`tokenWord` now answers `expired` once the expiry has passed, using the same boundary as `expiryText` in the same file (`left <= 0`), and the state gets the failure tone. A state other than `ok` still wins: a rejected token reads Rejected whatever its date.

- [ ] **Step 1: Write the failing tests**

In `web/src/lib/token.test.ts`, replace:

```ts
import { fixtures } from "@/test/fixtures"
import { expiryText, rateText, tokenWord } from "./token"
```

with:

```ts
import { fixtures } from "@/test/fixtures"
import { stateTone, stateWord } from "./status"
import { expiryText, rateText, tokenWord } from "./token"
```

and in `describe("tokenWord", …)`, replace:

```ts
    [token({ expires_at: at(10 * DAY) }), "expires soon"],
    [token({ state: "unverified" }), "unverified"],
    [token({ state: "rejected", expires_at: at(DAY) }), "rejected"],
  ])("reads %j as %s", (t, word) => {
    expect(tokenWord(t, now)).toBe(word)
  })
})
```

with:

```ts
    [token({ expires_at: at(10 * DAY) }), "expires soon"],
    [token({ expires_at: at(1000) }), "expires soon"],
    [token({ expires_at: at(0) }), "expired"],
    [token({ expires_at: at(-DAY) }), "expired"],
    [token({ state: "unverified" }), "unverified"],
    [token({ state: "rejected", expires_at: at(DAY) }), "rejected"],
    [token({ state: "rejected", expires_at: at(-DAY) }), "rejected"],
  ])("reads %j as %s", (t, word) => {
    expect(tokenWord(t, now)).toBe(word)
  })

  it("puts an expired token in the failure tone and one about to expire in the warning tone", () => {
    expect(stateTone(tokenWord(token({ expires_at: at(-DAY) }), now))).toBe("bad")
    expect(stateWord(tokenWord(token({ expires_at: at(-DAY) }), now))).toBe("Expired")
    expect(stateTone(tokenWord(token({ expires_at: at(10 * DAY) }), now))).toBe("warn")
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/lib/token.test.ts`
Expected: FAIL, 3 of 17 tests:
- the two rows that expect `expired`, with `expected 'expires soon' to be 'expired'`
- `puts an expired token in the failure tone …` with `expected 'warn' to be 'bad'`

- [ ] **Step 3: Write the implementation**

In `web/src/lib/token.ts`, replace:

```ts
  if (t.state !== "ok") return t.state
  return t.expires_at && Date.parse(t.expires_at) - now < SOON ? "expires soon" : "valid"
}
```

with:

```ts
  if (t.state !== "ok") return t.state
  if (!t.expires_at) return "valid"
  // The daemon's last check can still say ok after the expiry has passed.
  const left = Date.parse(t.expires_at) - now
  if (left <= 0) return "expired"
  return left < SOON ? "expires soon" : "valid"
}
```

In `web/src/lib/status.ts`, in the `tones` map, replace:

```ts
  rejected: "bad",
  ok: "ok",
```

with:

```ts
  rejected: "bad",
  expired: "bad",
  ok: "ok",
```

`stateWord` already capitalises `expired` to `Expired`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib/token.test.ts src/lib/status.test.ts src/components/token-section.test.tsx`
Expected: PASS, three files. `token.test.ts` has 17 tests.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/token.ts web/src/lib/token.test.ts web/src/lib/status.ts
git commit -m "fix(web): read a token past its expiry as expired"
```

### Task 78: The token page says the check covers every repository

**Files:**
- Modify: `web/src/components/token-section.tsx` (`NOTE`)
- Test: `web/src/components/token-section.test.tsx`

**Interfaces:**
- Consumes: `tokenWord` answering `"expired"` and `stateTone("expired")` being `"bad"` (Task 77), for the first of the two tests
- Produces: nothing

**Items:** 283, 106
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

The note under "GitHub token" in Settings says that replacing the token "checks read access to the first repository only". That was true once. The daemon's `SetToken` now checks the new token against every configured repository before it accepts it. The note is corrected.

The task also adds a page-level test for the Expired pill of the previous task. That test passes as soon as it is written; it holds the pill's word and tone in place on the page itself.

- [ ] **Step 1: Write the tests**

In `web/src/components/token-section.test.tsx`, insert these two tests directly below the test `warns when the token expires within 14 days`:

```tsx
  it("shows a token past its expiry as expired, in the failure tone", async () => {
    mockApi(authedRoutes({ "GET /api/token": token({ expires_at: new Date(DAEMON_NOW - DAY).toISOString() }) }))
    renderApp("/settings/token")
    const section = within(await screen.findByRole("region", { name: "GitHub token" }))
    expectPill(await section.findByText("Expired"), "bg-destructive/15", "text-destructive")
    expect(section.getByText("Expired Oct 2")).toBeInTheDocument()
    expect(section.queryByText("Expires soon")).toBeNull()
  })

  it("says that replacing the token checks every configured repository", async () => {
    mockApi(authedRoutes({ "GET /api/token": token() }))
    renderApp("/settings/token")
    const section = within(await screen.findByRole("region", { name: "GitHub token" }))
    expect(
      section.getByText(
        "Replacing the token takes effect at once, outside the save bar. It checks read access to every configured repository; registration permissions are checked when a runner next starts.",
      ),
    ).toBeInTheDocument()
  })

```

`token`, `DAY`, `DAEMON_NOW` and `expectPill` are already defined or imported at the top of that file.

- [ ] **Step 2: Run the tests to verify the note test fails**

Run: `cd web && npx vitest run src/components/token-section.test.tsx`
Expected: 1 failed, 9 passed. `says that replacing the token checks every configured repository` fails with `Unable to find an element with the text: Replacing the token takes effect at once, outside the save bar. It checks read access to every configured repository; registration permissions are checked when a runner next starts.` The Expired test passes.

- [ ] **Step 3: Write the implementation**

In `web/src/components/token-section.tsx`, replace:

```tsx
const NOTE =
  "Replacing the token takes effect at once, outside the save bar. It checks read access to the first repository only; registration permissions are checked when a runner next starts."
```

with:

```tsx
const NOTE =
  "Replacing the token takes effect at once, outside the save bar. It checks read access to every configured repository; registration permissions are checked when a runner next starts."
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/token-section.test.tsx`
Expected: PASS (`Tests  10 passed (10)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/components/token-section.tsx web/src/components/token-section.test.tsx
git commit -m "fix(web): say the token check covers every repo"
```

### Task 79: Actions filters by the address before the config has loaded

**Files:**
- Modify: `web/src/pages/actions.tsx` (`ActionsPage`)
- Test: `web/src/pages/actions.test.tsx`

**Interfaces:**
- Consumes: nothing
- Produces: nothing

**Items:** 106, 284
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

The Actions page takes its repository filter from `?repo=` in the address, but only after it has matched that name against the configured and watched repositories in the config. The config is a separate request. Until it answers, the page has no list, treats the name as unknown and ignores it. Two things follow. Opening `/actions?repo=darkcloud` first shows the runs of every repository, then narrows. And clicking a Status filter in that moment builds the new address from the repository the page believes in, which is none, so `?repo=` is dropped.

The page now uses the name from the address as it stands until the config has loaded: it filters by it, the Repository box shows it, and a Status change keeps it. Once the config has loaded nothing changes from today: a known name is matched, ignoring case, and a name that is neither configured nor watched leaves the address.

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/actions.test.tsx`, insert these three tests directly below the test `drops values it does not know, from the page and the URL`:

```tsx
  it("filters by the repository in the address before the config has loaded", async () => {
    mockApi(authedRoutes({ "GET /api/config": () => new Promise(() => {}) }))
    renderApp("/actions?repo=darkcloud")
    expect(await screen.findByText("0 running, 0 queued in darkcloud. Updated 14:05.")).toBeInTheDocument()
    expect(box().getByRole("link", { name: /^deploy/ })).toBeInTheDocument()
    expect(box().queryByRole("link", { name: /^Bump the runner/ })).toBeNull()
    expect(screen.getByRole("combobox", { name: "Repository" })).toHaveTextContent("darkcloud")
  })

  it("keeps the repository when Status changes before the config has loaded", async () => {
    mockApi(authedRoutes({ "GET /api/config": () => new Promise(() => {}) }))
    const { user, router } = renderApp("/actions?repo=darkcloud")
    await screen.findByText("0 running, 0 queued in darkcloud. Updated 14:05.")
    await user.click(screen.getByRole("radio", { name: "Failed" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ repo: "darkcloud", status: "failed" }))
    expect(box().getByRole("link", { name: /^deploy/ })).toBeInTheDocument()
  })

  it("filters by an unknown repository until the config has loaded, then drops it from the URL", async () => {
    let release: (config: unknown) => void = () => {}
    const gate = new Promise((resolve) => (release = resolve))
    mockApi(authedRoutes({ "GET /api/config": () => gate }))
    const { router } = renderApp("/actions?repo=nope")
    expect(await screen.findByText("No runs match these filters.")).toBeInTheDocument()
    expect(router.state.location.search).toEqual({ repo: "nope" })
    release(fixtures.config)
    await waitFor(() => expect(router.state.location.search).toEqual({}))
    expect(await screen.findByText("1 running, 1 queued across 4 repositories. Updated 14:05.")).toBeInTheDocument()
  })

```

The first two tests answer `/api/config` with a promise that never settles, so the config stays unloaded for the whole test. `box` is the helper at the top of that file.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/pages/actions.test.tsx -t "config has loaded"`
Expected: FAIL, 3 tests. The first two with `Unable to find an element with the text: 0 running, 0 queued in darkcloud. Updated 14:05.`, the third with `Unable to find an element with the text: No runs match these filters.`: the page shows the unfiltered list.

- [ ] **Step 3: Write the implementation**

In `ActionsPage` in `web/src/pages/actions.tsx`, replace:

```tsx
  const repo = names.find((n) => n.toLowerCase() === (search.repo ?? "").toLowerCase())
  const status: ActionsStatus | undefined = search.status
```

with:

```tsx
  const known = names.find((n) => n.toLowerCase() === (search.repo ?? "").toLowerCase())
  // Until the config has loaded there is no list to check the address
  // against, so the repository it names filters as it stands.
  const repo = config.data ? known : search.repo
  const status: ActionsStatus | undefined = search.status
```

The effect below it, which removes an unknown repository from the address, already waits for `config.data` and keeps working unchanged, as does `update`, which reads `repo`.

In the Repository select of the same component, replace:

```tsx
              {`${name} (watched)`}
            </SelectItem>
          ))}
        </SelectContent>
```

with:

```tsx
              {`${name} (watched)`}
            </SelectItem>
          ))}
          {repo && !names.includes(repo) && <SelectItem value={repo}>{repo}</SelectItem>}
        </SelectContent>
```

That extra item exists only while the config is unloaded: the select lists its repositories from the config, and without an item for the current value its box would be empty.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/pages/actions.test.tsx`
Expected: PASS (`Tests  28 passed (28)`)

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/actions.tsx web/src/pages/actions.test.tsx
git commit -m "fix(web): filter Actions by repo before config loads"
```

### Task 80: `lib/format.ts` gains `count` and loses `clock`

**Files:**
- Modify: `web/src/lib/format.ts`
- Test: `web/src/lib/format.test.ts`

**Interfaces:**
- Consumes: nothing
- Produces: `count(n: number): string` exported from `web/src/lib/format.ts`; it writes a number with thousands separators (`4980` gives `4,980`). The next task uses it. `clock` is no longer exported.

**Items:** 101
**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

Two pieces of housekeeping in one file. The stat cards and the token page each define the same one-line helper `count`, which writes a number with thousands separators; it gets one home here, and the next task makes both callers use it. And `clock`, which formats a time as `HH:MM:SS`, is used by nothing but its own test line: both go.

Other tasks change other tests in `web/src/lib/format.test.ts`. Change only the import line and the lines named below, and do not reformat the file.

- [ ] **Step 1: Write the failing test and drop the test of `clock`**

In `web/src/lib/format.test.ts`, in the import from `./format`, replace the name `clock` with `count`. The line as it stands:

```ts
import { ago, clock, dateTime, dateTimeSec, dur, elapsed, fmtMem, hhmm, humanBytes, isZeroTime, monthDay, plural, series, startedAt } from "./format"
```

becomes:

```ts
import { ago, count, dateTime, dateTimeSec, dur, elapsed, fmtMem, hhmm, humanBytes, isZeroTime, monthDay, plural, series, startedAt } from "./format"
```

In `describe("small formatters", …)`, replace:

```ts
  it("formats local times (TZ=UTC in tests)", () => {
    expect(clock("2026-10-03T14:05:09Z")).toBe("14:05:09")
    expect(hhmm(new Date("2026-10-06T14:20:00Z"))).toBe("14:20")
```

with:

```ts
  it("count groups the thousands of a number", () => {
    expect(count(4980)).toBe("4,980")
    expect(count(1_250_000)).toBe("1,250,000")
    expect(count(0)).toBe("0")
  })
  it("formats local times (TZ=UTC in tests)", () => {
    expect(hhmm(new Date("2026-10-06T14:20:00Z"))).toBe("14:20")
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/lib/format.test.ts`
Expected: FAIL, 1 of 21 tests, `count groups the thousands of a number`, with `TypeError: count is not a function`

- [ ] **Step 3: Write the implementation**

In `web/src/lib/format.ts`, delete this function and the blank line after it:

```ts
export function clock(iso: string): string {
  const d = new Date(iso)
  return `${hhmm(d)}:${pad(d.getSeconds())}`
}

```

and append at the end of the file, below `plural`:

```ts

export function count(n: number): string {
  return n.toLocaleString("en-US")
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib/format.test.ts`
Expected: PASS (`Tests  21 passed (21)`)

Then run: `cd web && npx tsc --noEmit`
Expected: no output, which also shows nothing else imported `clock`.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/format.ts web/src/lib/format.test.ts
git commit -m "refactor(web): add count to format and drop clock"
```

### Task 81: The stat cards and the token page share `count`

**Files:**
- Modify: `web/src/components/stat-card.tsx`, `web/src/lib/token.ts`

**Interfaces:**
- Consumes: `count(n: number): string` from `web/src/lib/format.ts` (Task 80)
- Produces: nothing

**Items:** 101
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

`web/src/components/stat-card.tsx` and `web/src/lib/token.ts` each define the same private helper, `const count = (n: number) => n.toLocaleString("en-US")`. Both now import the one in `lib/format.ts`. Nothing a user sees changes, so there is no new test: the existing tests of both callers already assert the formatted numbers (`4,980` and `of 5,000` in `stat-card.test.tsx`, `4,980 of 5,000, resets 14:45` in `token.test.ts`) and must stay green.

- [ ] **Step 1: Use the shared helper in the stat cards**

In `web/src/components/stat-card.tsx`, replace:

```tsx
import { fmtMem, series } from "@/lib/format"
```

with:

```tsx
import { count, fmtMem, series } from "@/lib/format"
```

and delete this line and the blank line after it (it stands directly above `export function StatCards(`):

```tsx
const count = (n: number) => n.toLocaleString("en-US")

```

- [ ] **Step 2: Use the shared helper in the token text**

In `web/src/lib/token.ts`, replace:

```ts
import { hhmm, monthDay, plural } from "./format"
```

with:

```ts
import { count, hhmm, monthDay, plural } from "./format"
```

and delete this line and the blank line after it (it stands directly above `export function rateText(`):

```ts
const count = (n: number) => n.toLocaleString("en-US")

```

- [ ] **Step 3: Run the tests to verify they still pass**

Run: `cd web && npx vitest run src/components/stat-card.test.tsx src/lib/token.test.ts src/lib/format.test.ts`
Expected: PASS (`Tests  45 passed (45)`)

Then run: `cd web && npx tsc --noEmit`
Expected: no output

- [ ] **Step 4: Commit**

```bash
git add web/src/components/stat-card.tsx web/src/lib/token.ts
git commit -m "refactor(web): share count between card and token"
```

### Task 82: Delete `statusWord` and write the History cap from its constant

**Files:**
- Modify: `web/src/lib/actions.ts`, `web/src/pages/history.tsx`
- Test: `web/src/lib/actions.test.ts`

**Interfaces:**
- Consumes: nothing
- Produces: nothing. `statusWord` is no longer exported from `web/src/lib/actions.ts`.

**Items:** 101
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 0 = 1

Two more pieces of housekeeping, neither of which changes what a user sees.

`statusWord` in `web/src/lib/actions.ts` is called by nothing but its own tests; the Actions page draws a run's state with `RunStatusIcon`. The function and its tests go. The table test that used it also checks `isActive` and `isQueued`, which stay; it keeps those two columns and gains a row for a completed run.

The History page says "Showing the newest 500 jobs in this window." when the list is cut at the cap. The cap itself is the constant `HISTORY_LIMIT` (500, in `web/src/api/hooks.ts`), which the same line already uses in its condition; the sentence now takes its number from the constant. The existing test `says when the 500-job cap cut the window short` keeps asserting the sentence.

- [ ] **Step 1: Trim the tests of `statusWord`**

In `web/src/lib/actions.test.ts`, in the import from `./actions`, replace:

```ts
  startText,
  statusWord,
} from "./actions"
```

with:

```ts
  startText,
} from "./actions"
```

and replace the end of `describe("run states", …)`:

```ts
    ["queued", true, true, "Queued"],
    ["waiting", true, true, "Queued"],
    ["pending", true, true, "Queued"],
    ["requested", true, true, "Queued"],
    ["action_required", true, true, "Queued"],
    ["in_progress", true, false, "Running"],
  ])("reads %s", (status, active, queued, word) => {
    const r = run({ status, conclusion: "" })
    expect([isActive(r), isQueued(r), statusWord(r)]).toEqual([active, queued, word])
  })

  it("words a completed run by its result", () => {
    expect(statusWord(run({ conclusion: "success" }))).toBe("Succeeded")
    expect(statusWord(run({ conclusion: "timed_out" }))).toBe("Failed")
    expect(statusWord(run({ conclusion: "skipped" }))).toBe("Skipped")
  })
})
```

with:

```ts
    ["queued", true, true],
    ["waiting", true, true],
    ["pending", true, true],
    ["requested", true, true],
    ["action_required", true, true],
    ["in_progress", true, false],
    ["completed", false, false],
  ])("reads %s", (status, active, queued) => {
    const r = run({ status, conclusion: "" })
    expect([isActive(r), isQueued(r)]).toEqual([active, queued])
  })
})
```

- [ ] **Step 2: Delete `statusWord`**

In `web/src/lib/actions.ts`, delete this function and the blank line after it:

```ts
export function statusWord(r: ActionsRun): string {
  if (!isActive(r)) return resultWord(r.conclusion)
  return isQueued(r) ? "Queued" : "Running"
}

```

The import of `resultWord` at the top of the file stays: `matchesStatus` still uses it.

- [ ] **Step 3: Take the number from the constant**

In `web/src/pages/history.tsx`, replace:

```tsx
          <p className="border-b border-border px-4 py-2.5 text-sm text-muted-foreground">Showing the newest 500 jobs in this window.</p>
```

with:

```tsx
          <p className="border-b border-border px-4 py-2.5 text-sm text-muted-foreground">{`Showing the newest ${HISTORY_LIMIT} jobs in this window.`}</p>
```

`HISTORY_LIMIT` is already imported in that file.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib/actions.test.ts src/pages/history.test.tsx`
Expected: PASS, both files (`actions.test.ts` has 15 tests).

Then run: `cd web && npx tsc --noEmit`
Expected: no output, which also shows nothing else imported `statusWord`.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/actions.ts web/src/lib/actions.test.ts web/src/pages/history.tsx
git commit -m "refactor(web): drop statusWord and a literal limit"
```

### Task 83: No test has less time than the suite's default

**Files:**
- Modify: `web/src/test/setup.ts` (one comment)
- Test: `web/src/api/hooks-pages.test.tsx`
- Test: `web/src/api/hooks.test.tsx`
- Test: `web/src/components/add-repo-dialog.test.tsx`
- Test: `web/src/components/install-dialog.test.tsx`
- Test: `web/src/components/runner-panel.test.tsx`
- Test: `web/src/pages/actions.test.tsx`
- Test: `web/src/pages/history.test.tsx`
- Test: `web/src/pages/repositories.test.tsx`
- Test: `web/src/pages/runners.test.tsx`
- Test: `web/src/pages/storage.test.tsx`

**Interfaces:**
- Consumes: the 15-second `testTimeout` in `web/vite.config.ts` (Contracts)
- Produces: nothing

**Items:** 109, 225
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

Eighteen tests in ten files were given `{ timeout: 10_000 }` when the suite's default test timeout was 5 seconds, to give them more time. The default is now 15 seconds, and a per-test timeout wins over the default, so these eighteen tests have less time than every other test: the opposite of what the option was written for. This task removes the option from those tests, the same edit eighteen times, and corrects one comment that still names the 5 seconds.

Per-test timeouts of 15, 20 and 30 seconds stay. So does `{ timeout: 10_000 }` in `web/src/pages/setup.test.tsx`: there it is the third argument of a `findByRole` call, a wait inside a test, not a test's timeout. `web/src/pages/setup.test.tsx` is not edited.

This task has no failing test first: it changes how long tests may run, not what they check. Step 1 shows the options, Step 3 shows they are gone.

- [ ] **Step 1: List the overrides**

Run: `rg -n 'timeout: ?10_000' web/src --glob '*.test.*'`
Expected: 19 lines. Eighteen are `it("…", { timeout: 10_000 }, async () => {` lines in the ten test files listed above; one is the `findByRole` line in `web/src/pages/setup.test.tsx`.

- [ ] **Step 2: Remove the option from the eighteen tests**

Run, from the repository root, as one command:

```bash
sed -i 's/, { timeout: 10_000 }, async () => {/, async () => {/' web/src/api/hooks-pages.test.tsx web/src/api/hooks.test.tsx web/src/components/add-repo-dialog.test.tsx web/src/components/install-dialog.test.tsx web/src/components/runner-panel.test.tsx web/src/pages/actions.test.tsx web/src/pages/history.test.tsx web/src/pages/repositories.test.tsx web/src/pages/runners.test.tsx web/src/pages/storage.test.tsx
```

Each of the eighteen lines loses `{ timeout: 10_000 }, ` and nothing else. For example, in `web/src/api/hooks-pages.test.tsx` the line

```ts
  it("polls while the check runs and stops when it is done", { timeout: 10_000 }, async () => {
```

becomes

```ts
  it("polls while the check runs and stops when it is done", async () => {
```

If `sed` cannot be used, make the same edit by hand on these tests, found by their names:

- `web/src/api/hooks-pages.test.tsx`: `polls while the check runs and stops when it is done`
- `web/src/api/hooks.test.tsx`: `polls by cursor and starts over when the epoch changes`, `waits for an epoch before polling`, `advances the cursor on every poll`, `does not poll while the log is closed`
- `web/src/components/add-repo-dialog.test.tsx`: `sends Add while the daemon is unreachable and shows the request's error`
- `web/src/components/install-dialog.test.tsx`: `sends Install after the daemon went unreachable and shows the request's error`
- `web/src/components/runner-panel.test.tsx`: `disables Stop when the daemon stops answering for a live runner`
- `web/src/pages/actions.test.tsx`: `ticks the elapsed time of a running run with the daemon's clock`
- `web/src/pages/history.test.tsx`: `hides the previous filter's rows while the next ones load`, `hides the previous window's rows while the next ones load`
- `web/src/pages/repositories.test.tsx`: `keeps the table but locks its actions while the daemon is unreachable`
- `web/src/pages/runners.test.tsx`: `keeps the selection when the selected runner finishes`, `follows a runner that picks up a job while the dialog is open`, `says a runner that left while the dialog is open has finished`, `says a runner that starts stopping while the dialog is open is already stopping`
- `web/src/pages/storage.test.tsx`: `disables pruning while the daemon is unreachable`, `toasts operations that finish while the page is open, failures first`

- [ ] **Step 3: Check that only the wait in the setup test is left**

Run: `rg -n 'timeout: ?10_000' web/src --glob '*.test.*'`
Expected: one line, in `web/src/pages/setup.test.tsx`, containing `findByRole("heading", { name: "Repositories" }, { timeout: 10_000 })`.

Run: `git diff --stat`
Expected: the last line is `10 files changed, 18 insertions(+), 18 deletions(-)`.

- [ ] **Step 4: Correct the comment in the test setup**

In `web/src/test/setup.ts`, replace:

```ts
// A loaded machine can take over a second to settle a poll; findBy queries
// get 3 seconds, under Vitest's 5-second test timeout.
```

with:

```ts
// A loaded machine can take over a second to settle a poll; findBy queries
// get 3 seconds, under the suite's 15-second test timeout.
```

Change nothing else in this file.

- [ ] **Step 5: Run the edited test files**

Run: `cd web && npx vitest run src/api/hooks-pages.test.tsx src/api/hooks.test.tsx src/components/add-repo-dialog.test.tsx src/components/install-dialog.test.tsx src/components/runner-panel.test.tsx src/pages/actions.test.tsx src/pages/history.test.tsx src/pages/repositories.test.tsx src/pages/runners.test.tsx src/pages/storage.test.tsx src/test/setup.test.ts`
Expected: PASS (`Test Files  11 passed (11)`, `Tests  182 passed (182)`)

- [ ] **Step 6: Commit**

```bash
git add web/src/api/hooks-pages.test.tsx web/src/api/hooks.test.tsx web/src/components/add-repo-dialog.test.tsx web/src/components/install-dialog.test.tsx web/src/components/runner-panel.test.tsx web/src/pages/actions.test.tsx web/src/pages/history.test.tsx web/src/pages/repositories.test.tsx web/src/pages/runners.test.tsx web/src/pages/storage.test.tsx web/src/test/setup.ts
git commit -m "test(web): drop timeouts under the suite default"
```

### Task 84: Whole-tree verification

**Files:**
- Test: the whole tree; this task changes no file and makes no commit

**Interfaces:**
- Consumes: the finished state of every task before this one, and the npm script `test:tz` (Contracts)
- Produces: nothing
**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 0 - spec 0 - coupling 0 - risk 0 = 0

The tasks before this one each ran the test files they touched. This task runs everything once on the finished branch: the whole web suite, the time-zone suite, the type check, the linter and the build, then the Go checks with the race detector, because one task changed `internal/model`.

This task fixes nothing and commits nothing. When a step fails, report the failing file, test or package with its output and stop.

- [ ] **Step 1: Run the whole web suite**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 900 npm test`
Expected: `Test Files  97 passed (97)` and `Tests  1384 passed (1384)`, no `FAIL` line. A test that only times out is the loaded host: run its file once more, alone, before reporting it.

- [ ] **Step 2: Run the time-zone suite**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npm run test:tz`
Expected: `Test Files  2 passed (2)` and `Tests  5 passed (5)`.

- [ ] **Step 3: Type-check and lint**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npm run typecheck`
Expected: exit status 0; no line after npm's own two lines that name the script.

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 300 npm run lint`
Expected: exit status 0; no line after npm's own two lines that name the script.

- [ ] **Step 4: Build the web UI**

Run: `cd web && TMPDIR=/root/.cache/ghr-plan-tmp timeout 600 npm run build`
Expected: exit status 0 and a line `✓ built in` followed by a time. Vite's warning that some chunks are larger than 500 kB is not a failure.

Run: `git status --short`
Expected: no output. The build writes into `web/dist/`, which is ignored except for `web/dist/.gitkeep`, and the build script writes that file back.

- [ ] **Step 5: Check the Go formatting and vet the tree**

Run: `PATH=/usr/local/go/bin:$PATH gofmt -l internal/ cmd/`
Expected: no output.

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH timeout 300 go vet ./...`
Expected: no output, exit status 0.

- [ ] **Step 6: Run every Go test with the race detector**

Run: `TMPDIR=/root/.cache/ghr-plan-tmp GOTMPDIR=/root/.cache/ghr-plan-tmp PATH=/usr/local/go/bin:$PATH go test -race -count=1 -timeout 480s ./...`
Expected: one `ok` line for each of the 22 packages that have tests (`internal/model`, `internal/history` and `internal/activity` among them), no `FAIL` line and no `DATA RACE` text. Give the command at least nine minutes before treating it as hung.

- [ ] **Step 7: Report**

Report the result of each step. The browser check the spec lists (a job page with a log of several thousand lines, a Ctrl click on a Dashboard repository and on a chart mark, a daemon restart while a page is open, and the raw-log download) is not part of this task: it needs a deploy to the runner LXC, and that needs the owner's yes.
