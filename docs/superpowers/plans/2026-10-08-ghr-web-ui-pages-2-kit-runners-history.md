# ghr web UI pages, plan 2: page kit, Runners and History

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the shared page kit and the page-wide copy rules, then rebuild the Runners page (with the runner page joined to it as a split view) and the History page (with a bucket chart above its table).

**Architecture:** A small kit under `web/src/components/page/` (`Section`, `StateText`, `ErrorLine`, `FieldGroup`, `Field`, `SplitView`, `SectionNav`) plus `useMediaQuery` gives every redesigned page the same building blocks; plan 3 consumes them for the remaining pages. The Runners page renders at both `/runners` and `/runners/$id`, holding the selection in the URL: a runner list on the left and a runner panel on the right at 1024px and wider, one or the other below. History keeps its filters in the URL, asks `/activity` for the chart and `/history?since=<the chart's from>` for the table, and lets a picked bucket narrow the table in the browser.

**Tech Stack:** React 19, TypeScript, TanStack Router and Query, darkraise-ui, Lucide icons, Vitest with Testing Library and axe-core.

**Spec:** docs/superpowers/specs/2026-10-08-ghr-web-ui-pages-design.md (§1, §3, §4 and their parts of §8; delivery step 2 of §9)

**Execution:** inline — `claude --model sonnet --effort high` — 1 of 15 tasks is heavy (Task 6, total 5) and is delegated; the self-implemented tasks top out at total 4 (Sonnet medium), raised to high because a task is delegated.

**Plan review:** 2026-10-08 — dr-superpowers:judge-opus — executability 18 / coherence 18 / coverage 18 / assumptions 18 (round 2)

## Deferred to plan 3

These parts of the spec are not built here; plan 3 (frontend part 2) owns them, because each lands with the page that renders it:
- §1.1 removing `StateBadge`, `stateVariant` and the `BadgeVariant` import (the pages that still use them are rebuilt in plan 3).
- §1.2 the dialog rules: Add repository's "public" and Install's "lts" badges become muted words, busy buttons use `loading` instead of "Adding…" and "Queuing…", the setup wizard's "+ Add repository" loses its "+", and `ConfirmDialog` titles split into a title and a body.
- §8 the anti-slop test's Tailwind gradient-utility rule and the directory walk (register row 16), the `stepper.tsx` deletion (row 15), and the `lib/activity.ts` deletion (row 10).
- §2.5 showing `disk_root` on the Dashboard and Storage (row 11).

## Global Constraints

- Work on the branch `feat/web-ui-pages`, after plan 1 (docs/superpowers/plans/2026-10-08-ghr-web-ui-pages-1-backend.md) is complete on it. Nothing is pushed or merged without the owner: pushing master cuts a release.
- Only files under `web/src` change. No new npm dependencies.
- Spec 1's rules bind every file touched: one accent colour; borders, never shadows; 6px radius on controls and 10px on surfaces; no pills around plain text; no "·" separators; no em or en dashes in UI strings; mono only for data; sentence case.
- Copy uses today's formatters: `ago()` ("2h ago"), `dur()` ("3m10s"), `hhmm()`, `monthDay()`, `plural()` from `web/src/lib/format.ts`.
- Every control that changes the daemon is disabled while `useStatus().isError` is true.
- Icon buttons carry an `aria-label` naming their row, plus a tooltip.
- Lucide icons are size 15, except 13 beside `text-xs` copy (form-row errors and rejected-save lines).
- Each new non-test source file is added to the `FILES` list in `web/src/anti-slop.test.ts` in the task that creates it (keep the list's existing grouping: components, then api and lib, then pages, then styles).
- Before each commit, run in `web/`: `npm run typecheck`, `npm run lint`, and `npx vitest run`. All three must pass.
- Comments only where the why is non-obvious. English only.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no period.

## Contracts

**API client and hooks** (`web/src/api/client.ts`, `web/src/api/hooks.ts`)

```ts
// client: since and repo are left out of the query string when empty
api.history(repo: string, conclusion: string, since: string, limit: number, signal?: AbortSignal): Promise<HistoryEntry[]>
api.activity(window: ActivityWindow, tz: string, repo: string, signal?: AbortSignal): Promise<Activity>

// hooks
keys.activity(window: string, tz: string, repo: string)      // ["activity", window, tz, repo]
keys.history(repo: string, conclusion: string, since: string) // ["history", repo, conclusion, since]
export const HISTORY_LIMIT = 500
export function useActivity(window: ActivityWindow, repo?: string)                       // repo defaults to ""
export function useHistory(repo: string, conclusion: string, since: string | undefined)  // disabled while since is undefined
```

**Copy** — `errorText(err)` (`web/src/query.ts`) returns `"{message without a trailing period}. Retry after HH:MM"` for an `ApiError` with `retryAt`, else the message. `unsavedText(n)` (`web/src/lib/draft.ts`) returns `"1 unsaved change"` or `"{n} unsaved changes"`.

**Page kit** (`web/src/components/page/`)

```tsx
// section.tsx
export function Section(props: { title: string; aside?: ReactNode; children: ReactNode; className?: string }): JSX.Element
// state-text.tsx
export function StateText(props: { state: string; label?: string }): JSX.Element
// error-line.tsx
export function ErrorLine(props: { children: ReactNode; onRetry?: () => void }): JSX.Element
// field.tsx
export function FieldGroup(props: { id: string; title: string; note?: string; children: ReactNode }): JSX.Element
export function Field(props: { label: string; htmlFor?: string; help?: ReactNode; error?: string; changed?: boolean; children: ReactNode }): JSX.Element
// split-view.tsx
export function SplitView(props: { list: ReactNode; panel: ReactNode | null; narrow: ReactNode }): JSX.Element
// section-nav.tsx
export interface NavItem { id: string; title: string }
export function SectionNav(props: { items: readonly NavItem[] }): JSX.Element | null
```

- `Section` renders `<section aria-labelledby>` named by its `h2`.
- `FieldGroup` renders `<section id={id} aria-labelledby={`${id}-title`}>` whose `h2` has `id={`${id}-title`}` and `tabIndex={-1}`.
- `SectionNav` items must be a stable array (a module constant). Each entry is a `button` that scrolls `#{id}` into view and focuses `#{id}-title`; the URL does not change. The entry for the last section whose top is at or above 96px from the viewport top carries `aria-current="true"`. It renders nothing below 1280px.
- `SplitView` renders `list` and `panel` side by side at 1024px and wider (`list` alone when `panel` is `null`), and `narrow` below.

**Status words** (`web/src/lib/status.ts`)

```ts
export type Tone = "accent" | "warn" | "bad" | "ok" | "muted"
export function stateTone(state: string): Tone // case-insensitive; unknown states are "muted"
```

**Media** — `web/src/lib/use-media-query.ts`: `useMediaQuery(query: string): boolean`, `WIDE = "(min-width: 1024px)"`, `WIDEST = "(min-width: 1280px)"`. Test helper `web/src/test/media.ts`: `setViewport(width: number): { resize(width: number): void }`, restored after each test.

**Test harness** — `web/src/test/routes.tsx`: `renderRoutes(paths: Record<string, () => ReactNode>, initial: string)` returns `{ router, queryClient, user, ...renderResult }`; it wraps a fresh query client, a `TooltipProvider` and a `Toaster`, and every route keeps its search as given.

**Charts** — `BucketsChart` (`web/src/components/buckets-chart.tsx`) gains `tracks?: "all" | "jobs"` (default `"all"`), `picked?: string | null` (a bucket `start`), and `onPick?: (bucket: ActivityBucket) => void`. With `onPick`, each bucket is `role="button"` with `aria-pressed`, picked by click, Enter or Space. `WindowControl` gains `options?: readonly W[]` and is generic over `W extends ActivityWindow`.

**Runners**

```ts
// lib/steps.ts
export type StepPhase = "running" | "completed" | "pending"
export function stepPhase(step: Step): StepPhase
export interface StepTimeline { bars: ({ offset: number; width: number } | null)[]; durations: (number | null)[] }
export function stepTimeline(steps: Step[], now: number, live: boolean): StepTimeline
// lib/summary.ts
export function runnersSummary(status: Status): string
// components/step-list.tsx
export function StepList(props: { steps: Step[]; now: number; live: boolean }): JSX.Element
// lib/status.ts
export function waitingRepos(status: Status): RepoStatus[]
// components/runner-list.tsx
export function RunnerList(props: { status: Status | undefined; selected: string | undefined; tab: DetailTab; now: number }): JSX.Element
// components/stop-runner-dialog.tsx
export function StopRunnerDialog(props: { instance: InstanceStatus | null; onClose: () => void }): JSX.Element
// components/runner-panel.tsx
export type DetailTab = "steps" | "log" | "containers"
export function RunnerPanel(props: { id: string; tab: DetailTab; backLink: boolean }): JSX.Element
// pages/runners.tsx
export function RunnersPage(): JSX.Element // route /runners
export function RunnerPage(): JSX.Element  // route /runners/$id
```

**History** (`web/src/lib/history.ts`)

```ts
export const HISTORY_WINDOWS: readonly ["24h", "7d", "30d"]
export type HistoryWindow = "24h" | "7d" | "30d"
export const RESULTS: readonly [{ value: "success"; label: "Succeeded" }, { value: "failure"; label: "Failed" }, { value: "cancelled"; label: "Cancelled" }]
export type HistoryResult = "success" | "failure" | "cancelled"
export interface HistorySearch { repo?: string; result?: HistoryResult; window?: HistoryWindow }
export function parseHistorySearch(search: Record<string, unknown>): HistorySearch
export function dayLabel(iso: string, now: number): string            // "Today", "Yesterday", "Mon, Sep 28"
export interface DayGroup { key: string; label: string; rows: HistoryEntry[] }
export function groupByDay(rows: HistoryEntry[], now: number): DayGroup[]
export function took(entry: HistoryEntry): number                      // ms
export function median(values: number[]): number | undefined
export function historySummary(rows: HistoryEntry[]): string          // "2 jobs, 1 failed, median 3m15s"
export function inBucket(rows: HistoryEntry[], bucket: { start: string; end: string }): HistoryEntry[]
export function pickText(bucket: { start: string; end: string }): string // "Showing 13:00 to 14:00, Oct 3"
export function resultWord(conclusion: string): string                  // Succeeded | Cancelled | Skipped | Failed
```

## Assumptions (evidence)

- Plan 1 has landed: `Step` has `started_at?`/`completed_at?`, the steps fixture carries times (step 1 14:01:00 to 14:01:06, step 2 running since 14:01:06, step 3 queued), the server accepts `repo` on `/activity` and an empty or RFC 3339 `since` on `/history` (docs/superpowers/plans/2026-10-08-ghr-web-ui-pages-1-backend.md, Contracts).
- Tests run with `TZ=UTC` (web/vite.config.ts:23), and `useNow` follows `status.now`, which the status fixture sets to 2026-10-03T14:05:00Z (web/src/lib/use-now.ts; web/fixtures_test.go:16). 2026-10-03 is a Saturday, so 2026-09-28 is a Monday.
- jsdom has no `matchMedia` (web/src/test/setup.ts:5-22 installs a stub whose `matches` is always false) and no `Element.prototype.scrollIntoView` (jsdom does not implement layout); Task 2 stubs the latter in setup.
- TanStack Router's `Link` sets `aria-current="page"` and `data-status="active"` on an active link and overrides any value passed (node_modules/@tanstack/react-router/dist/esm/link.js:237-240, version 1.170.41).
- `useBlocker`'s `shouldBlockFn` receives `{ current, next, action }`, each location with a `pathname` (node_modules/@tanstack/react-router/dist/esm/useBlocker.js:55-74).
- darkraise-ui's `ToggleGroup type="single"` renders a `radiogroup` of `radio` items (web/src/components/window-control.test.tsx:11-13).
- The kit's `Tooltip` needs a `TooltipProvider` outside the app (web/src/components/repo-table.test.tsx:3,19); inside the app the theme provider supplies it (the Dashboard's row menus render tooltips in page tests).
- `windowWords("7d")` is `"the last 7 days"` (web/src/lib/activity-view.ts:13-19).
- `bucketAria` labels the fixture's 13:00 bucket `13:00 to 14:00, 5 busy runner-minutes, 1 succeeded, 0 failed, 0 cancelled, at most 3 waiting, CPU 23%` (web/src/components/buckets-chart.test.tsx).
- The history fixture holds `build #41` (darkmem, workflow `ci`, success, 13:35 to 13:40, with a URL) and `deploy #7` (darkcloud, failure, 11:05 to 11:06:30, no URL) on 2026-10-03 (web/src/api/fixtures/history.json). The deploy entry's `workflow` is also `deploy`, so the text "deploy" appears twice in its row once the workflow is shown; tests find that row by `#7`.
- TanStack keeps the raw parsed search in `router.state.location.search`; `validateSearch` output reaches the route's `useSearch`, and the location takes the validated form only on a navigation (router-core `parseLocation` against `buildLocation`). A test of bad URL values therefore checks the rendered state and the requests, not `location.search`.
- `react-refresh/only-export-components` warns on a component file that also exports a function (web/eslint.config.js:20), and lint runs with `--max-warnings 0`; type exports are fine. Helper functions therefore live in `lib/`.
- darkraise-ui's `Spinner` renders an `sr-only` "Loading" when it has no `label`, and `label` takes a node (node_modules/darkraise-ui/dist/components/spinner/Spinner.d.ts:7).
- `/status` polls every 1000 ms (`POLL_FAST`, web/src/api/hooks.ts:32). Tests that wait for a poll to flip state (Tasks 11 and 12) rely on Task 2's 3000 ms `asyncUtilTimeout`, so Task 2 runs before them.
- Steps and containers poll every 5 s (`POLL_SLOW`, web/src/api/hooks.ts); a page test cannot tell "stopped polling" from "not due yet" without waiting over 5 s, so that behaviour stays covered by `useSteps(id, live)` and `useContainers(id, live)` passing `enabled: live`, not by a timing test.
- `Glyph` is used only by `pages/runner-detail.tsx`, and `runners-table.tsx` only by `pages/runners.tsx` and `pages/runner-detail.tsx` (grep on 2026-10-08), so Task 13 can delete them.
- Exact-string tests that this plan's copy changes: `● 1 unsaved change` in app.test.tsx:88, label-check-card.test.tsx:47, draft.test.ts:40-41, repository.test.tsx (44, 48, 108, 150) and settings.test.tsx:52; `— retry after` in app.test.tsx:101 (grep on 2026-10-08). The `… and 1 more` assertion in repository.test.tsx:106 changes in Task 4.

## Task index

1. Send repo and since from the web client
2. Apply the page-wide copy rules
3. Add the Section and StateText components
4. Add the form row, error line and save bar
5. Switch layouts by screen width
6. Jump between sections without leaving the page
7. Draw the jobs chart and pick a bucket
8. Draw a runner's steps on a time axis
9. List the runners and the waiting repositories
10. Move the stop dialog and the empty log line
11. Build the runner panel
12. Join the runner page to the Runners page
13. Retire the old runner page
14. Add the History helpers
15. Rebuild the History page

---

### Task 1: Send repo and since from the web client

**Files:**
- Modify: `web/src/api/client.ts` (`api.history`, `api.activity`)
- Modify: `web/src/api/hooks.ts` (`keys`, `useActivity`, `useHistory`, `useRepoActivity`, new `HISTORY_LIMIT`)
- Modify: `web/src/pages/history.tsx` (one call)
- Test: `web/src/api/client.test.ts`, `web/src/api/client-pages.test.ts`, `web/src/api/hooks.test.tsx`, `web/src/pages/history.test.tsx`

**Interfaces:**
- Consumes: plan 1's `GET /activity?repo=` and `GET /history?since=`.
- Produces: `api.history`, `api.activity`, `keys.activity`, `keys.history`, `HISTORY_LIMIT`, `useActivity`, `useHistory` (Contracts, API client and hooks).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `web/src/api/client-pages.test.ts`, replace the test "asks for activity by window and zone":

```ts
  it("asks for activity by window and zone, and by repo when given", async () => {
    const { calls } = mockApi({ "GET /api/activity": { window: "3h" } })
    expect((await api.activity("3h", "Asia/Ho_Chi_Minh", "")).window).toBe("3h")
    await api.activity("24h", "UTC", "darkmem")
    expect(calls[0]?.search).toBe("?window=3h&tz=Asia%2FHo_Chi_Minh")
    expect(calls[1]?.search).toBe("?window=24h&tz=UTC&repo=darkmem")
  })
```

In `web/src/api/client.test.ts`, in "sends JSON bodies and builds query strings", replace `await api.history("darkmem", "", 200)` with `await api.history("darkmem", "", "", 200)`, and append inside the same `describe`:

```ts
  it("sends since only when it is set", async () => {
    const { calls } = mockApi({ "GET /api/history": [] })
    await api.history("", "failure", "2026-10-01T18:00:00+07:00", 500)
    expect(calls[0]?.search).toBe("?repo=&conclusion=failure&since=2026-10-01T18%3A00%3A00%2B07%3A00&limit=500")
  })
```

In `web/src/api/hooks.test.tsx`, add `useHistory` to the import from `./hooks`, add to the `describe("useActivity", …)` block:

```ts
  it("asks for one repository when given", async () => {
    const { calls } = mockApi({ "GET /api/activity": fixtures.activityBuckets })
    const { wrapper } = withQuery()
    const { result } = renderHook(() => useActivity("24h", "darkmem"), { wrapper })
    await waitFor(() => expect(result.current.data?.window).toBe("24h"))
    expect(calls[0]?.search).toBe("?window=24h&tz=UTC&repo=darkmem")
  })
```

and append:

```ts
describe("useHistory", () => {
  it("waits for since, then asks for up to 500 jobs since it", async () => {
    const { calls } = mockApi({ "GET /api/history": fixtures.history })
    const { wrapper } = withQuery()
    const { result, rerender } = renderHook(({ since }: { since: string | undefined }) => useHistory("darkmem", "", since), {
      wrapper,
      initialProps: { since: undefined },
    })
    await new Promise((resolve) => setTimeout(resolve, 50))
    expect(calls).toHaveLength(0)
    rerender({ since: "2026-10-03T00:00:00Z" })
    await waitFor(() => expect(result.current.data).toHaveLength(2))
    expect(calls[0]?.search).toBe("?repo=darkmem&conclusion=&since=2026-10-03T00%3A00%3A00Z&limit=500")
  })
})
```

In `web/src/pages/history.test.tsx`, change the three expected query strings from `limit=200` to `limit=500` (they stay otherwise the same; the old page passes an empty `since`).

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/api`
Expected: FAIL: the new client test sees no `repo`/`since` in the query, and `useHistory` is not exported with three arguments (typecheck fails too).

- [ ] **Step 3: Write the implementation**

In `web/src/api/client.ts`, replace the two methods:

```ts
  history: (repo: string, conclusion: string, since: string, limit: number, signal?: AbortSignal) =>
    request<HistoryEntry[]>("GET", `/api/history?${query({ repo, conclusion, ...(since ? { since } : {}), limit })}`, undefined, signal),
```

```ts
  activity: (window: ActivityWindow, tz: string, repo: string, signal?: AbortSignal) =>
    request<Activity>("GET", `/api/activity?${query({ window, tz, ...(repo ? { repo } : {}) })}`, undefined, signal),
```

In `web/src/api/hooks.ts`, change the two keys:

```ts
  activity: (window: string, tz: string, repo: string) => ["activity", window, tz, repo] as const,
```

```ts
  history: (repo: string, conclusion: string, since: string) => ["history", repo, conclusion, since] as const,
```

replace `useActivity` and `useHistory`:

```ts
// The daemon aligns buckets to the zone it is given, so the browser sends its
// own and the columns line up with the viewer's clock.
export function useActivity(window: ActivityWindow, repo = "") {
  const tz = browserZone()
  return useQuery({
    queryKey: keys.activity(window, tz, repo),
    queryFn: ({ signal }) => api.activity(window, tz, repo, signal),
    refetchInterval: POLL_SLOW,
    // Keeps the panel and the repository strips drawn while a new window loads;
    // ActivityPanel reads lanes or buckets from the data's own window.
    placeholderData: keepPreviousData,
  })
}

export const HISTORY_LIMIT = 500

// History passes the from of its activity response as since, so the table
// and the chart cover one span; the query waits until it is known.
export function useHistory(repo: string, conclusion: string, since: string | undefined) {
  return useQuery({
    queryKey: keys.history(repo, conclusion, since ?? ""),
    queryFn: ({ signal }) => api.history(repo, conclusion, since ?? "", HISTORY_LIMIT, signal),
    enabled: since !== undefined,
    refetchInterval: POLL_SLOW,
    placeholderData: keepPreviousData,
  })
}
```

and in `useRepoActivity` change the call to `api.history(name, "", "", ACTIVITY_LIMIT, signal)`.

In `web/src/pages/history.tsx`, change `useHistory(repo, conclusion)` to `useHistory(repo, conclusion, "")`.

Run `grep -rn "keys.activity\|keys.history\|api.activity(\|api.history(" web/src --include=*.ts --include=*.tsx`; every hit must use the new arity.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npm run typecheck && npx vitest run src/api src/pages/history.test.tsx src/pages/dashboard.test.tsx`
Expected: PASS. The Dashboard still asks `?window=24h&tz=UTC`.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src/api web/src/pages/history.tsx web/src/pages/history.test.tsx
git commit -m "feat(web): ask for one repo's activity and since"
```

---

### Task 2: Apply the page-wide copy rules

**Files:**
- Modify: `web/src/query.ts` (`errorText`)
- Modify: `web/src/lib/draft.ts` (`unsavedText`)
- Modify: `web/src/test/setup.ts`
- Create: `web/src/test/setup.test.ts`
- Test: `web/src/app.test.tsx`, `web/src/components/shell.test.tsx`, and the `●` assertions listed in Assumptions
- Modify: `web/src/anti-slop.test.ts` (add `src/query.ts`, `src/lib/draft.ts`)

**Interfaces:**
- Consumes: nothing.
- Produces: `errorText` and `unsavedText` copy (Contracts, Copy); `asyncUtilTimeout` 3000 ms and a `scrollIntoView` stub for every test.

**Items:** 12, 14

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `web/src/app.test.tsx`, replace the test "adds the retry time of a rate-limited request":

```ts
  it("adds the retry time of a rate-limited request as its own sentence", () => {
    const err = new ApiError(429, "GitHub rate limit; API calls are paused", new Date("2026-10-06T14:20:00Z"))
    expect(errorText(err)).toBe("GitHub rate limit; API calls are paused. Retry after 14:20")
    expect(errorText(new ApiError(429, "Paused.", new Date("2026-10-06T14:20:00Z")))).toBe("Paused. Retry after 14:20")
  })
```

Append to `web/src/components/shell.test.tsx` (inside its top-level `describe`):

```ts
  it("reads a rate-limited status failure as one sentence in the banner", async () => {
    mockApi(authedRoutes({ "GET /api/status": () => json({ error: "GitHub rate limit", retry_at: "2026-10-06T14:20:00Z" }, 429) }))
    renderApp("/")
    expect(await screen.findByText("Daemon unreachable: GitHub rate limit. Retry after 14:20. Retrying.")).toBeInTheDocument()
  })
```

Create `web/src/test/setup.test.ts`:

```ts
import { getConfig } from "@testing-library/react"
import { describe, expect, it } from "vitest"

describe("test setup", () => {
  it("gives async queries 3 seconds and stubs scrollIntoView", () => {
    expect(getConfig().asyncUtilTimeout).toBe(3000)
    expect(() => document.createElement("div").scrollIntoView()).not.toThrow()
  })
})
```

Drop the `●` from every expected unsaved-change string (Git Bash, repository root):

```bash
sed -i 's/● \([0-9][0-9]* unsaved change\)/\1/g' web/src/app.test.tsx web/src/components/label-check-card.test.tsx \
  web/src/lib/draft.test.ts web/src/pages/repository.test.tsx web/src/pages/settings.test.tsx
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/app.test.tsx src/test/setup.test.ts src/lib/draft.test.ts src/components/shell.test.tsx`
Expected: FAIL: `errorText` still returns `… — retry after 14:20`, `unsavedText` still starts with `●`, `asyncUtilTimeout` is 1000, and `scrollIntoView` is not a function.

- [ ] **Step 3: Write the implementation**

In `web/src/query.ts`, replace `errorText`:

```ts
// The retry time is its own sentence, so callers can end the text with their
// own punctuation ("…. Retrying.") without doubling a period.
export function errorText(err: unknown): string {
  const text = err instanceof Error ? err.message : String(err)
  if (err instanceof ApiError && err.retryAt) return `${text.replace(/\.$/, "")}. Retry after ${hhmm(err.retryAt)}`
  return text
}
```

In `web/src/lib/draft.ts`:

```ts
export function unsavedText(n: number): string {
  return n === 1 ? "1 unsaved change" : `${n} unsaved changes`
}
```

In `web/src/test/setup.ts`, change the first import line to `import { cleanup, configure } from "@testing-library/react"`, replace the comment above the `matchMedia` stub with:

```ts
// darkraise-ui's ThemeProvider calls window.matchMedia without a guard, and
// jsdom implements neither matchMedia, scrollTo nor scrollIntoView.
```

and replace `window.scrollTo = () => {}` with:

```ts
window.scrollTo = () => {}
Element.prototype.scrollIntoView = () => {}

// A loaded machine can take over a second to settle a poll; findBy queries
// get 3 seconds, under Vitest's 5-second test timeout.
configure({ asyncUtilTimeout: 3000 })
```

In `web/src/anti-slop.test.ts`, add `"src/lib/draft.ts"` after `"src/lib/disk.ts"` and `"src/query.ts"` after `"src/pages/setup.tsx"`.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run`
Expected: PASS, including every page test that asserted the unsaved-change text.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "fix(web): drop the dash from retry and unsaved copy"
```

---

### Task 3: Add the Section and StateText components

**Files:**
- Create: `web/src/components/page/section.tsx`, `web/src/components/page/state-text.tsx`
- Modify: `web/src/lib/status.ts` (add `Tone`, `stateTone`)
- Modify: `web/src/pages/dashboard.tsx` (use the shared `Section`)
- Test: `web/src/components/page/section.test.tsx`, `web/src/components/page/state-text.test.tsx`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `Section`, `StateText`, `Tone`, `stateTone` (Contracts, Page kit and Status words).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/page/section.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { Section } from "./section"

describe("Section", () => {
  it("is a region named by its heading, not by aria-label", () => {
    render(
      <Section title="Disk" aside={<span>aside</span>}>
        <p>body</p>
      </Section>,
    )
    const region = screen.getByRole("region", { name: "Disk" })
    const heading = screen.getByRole("heading", { name: "Disk", level: 2 })
    expect(region).toHaveAttribute("aria-labelledby", heading.id)
    expect(region).not.toHaveAttribute("aria-label")
    expect(screen.getByText("aside")).toBeInTheDocument()
    expect(screen.getByText("body")).toBeInTheDocument()
  })
})
```

Create `web/src/components/page/state-text.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { stateTone } from "@/lib/status"
import { StateText } from "./state-text"

describe("stateTone", () => {
  it.each([
    ["busy", "accent"],
    ["Running", "accent"],
    ["online", "accent"],
    ["matched", "accent"],
    ["waiting", "warn"],
    ["starting", "warn"],
    ["expires soon", "warn"],
    ["refused", "warn"],
    ["failure", "bad"],
    ["offline", "bad"],
    ["rejected", "bad"],
    ["ok", "ok"],
    ["up to date", "ok"],
    ["paused", "muted"],
    ["idle", "muted"],
    ["cancelled", "muted"],
    ["finished", "muted"],
  ])("reads %s as %s", (state, tone) => {
    expect(stateTone(state)).toBe(tone)
  })
})

describe("StateText", () => {
  it("shows the state as a sentence-case word in its tone's colour", () => {
    render(
      <>
        <StateText state="busy" />
        <StateText state="offline" label="Offline now" />
        <StateText state="paused" />
      </>,
    )
    expect(screen.getByText("Busy")).toHaveClass("text-primary")
    expect(screen.getByText("Offline now")).toHaveClass("text-destructive")
    expect(screen.getByText("Paused")).toHaveClass("text-muted-foreground")
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/components/page`
Expected: FAIL: `Cannot find module './section'` and `stateTone` is not exported.

- [ ] **Step 3: Write the implementation**

Create `web/src/components/page/section.tsx`:

```tsx
import { useId, type ReactNode } from "react"

export function Section({ title, aside, children, className = "" }: { title: string; aside?: ReactNode; children: ReactNode; className?: string }) {
  const id = useId()
  return (
    <section aria-labelledby={id} className={`min-w-0 rounded-[10px] border border-border bg-card p-4 ${className}`}>
      <div className="mb-3 flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h2 id={id} className="text-base font-semibold">
          {title}
        </h2>
        {aside}
      </div>
      {children}
    </section>
  )
}
```

Append to `web/src/lib/status.ts`:

```ts
export type Tone = "accent" | "warn" | "bad" | "ok" | "muted"

const tones: Record<string, Tone> = {
  busy: "accent",
  running: "accent",
  active: "accent",
  online: "accent",
  matched: "accent",
  waiting: "warn",
  starting: "warn",
  queued: "warn",
  "expires soon": "warn",
  unverified: "warn",
  refused: "warn",
  interrupted: "warn",
  error: "bad",
  failed: "bad",
  failure: "bad",
  offline: "bad",
  unmatched: "bad",
  rejected: "bad",
  ok: "ok",
  success: "ok",
  valid: "ok",
  "up to date": "ok",
}

export function stateTone(state: string): Tone {
  return tones[state.toLowerCase()] ?? "muted"
}
```

Create `web/src/components/page/state-text.tsx`:

```tsx
import { stateTone, type Tone } from "@/lib/status"

const TONE_CLASS: Record<Tone, string> = {
  accent: "text-primary",
  warn: "text-warning",
  bad: "text-destructive",
  ok: "text-success",
  muted: "text-muted-foreground",
}

const sentence = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

export function StateText({ state, label }: { state: string; label?: string }) {
  return <span className={`font-medium ${TONE_CLASS[stateTone(state)]}`}>{label ?? sentence(state)}</span>
}
```

In `web/src/pages/dashboard.tsx`, delete the local `function Section …` (and the `ReactNode` import if nothing else uses it), and add `import { Section } from "@/components/page/section"`.

In `web/src/anti-slop.test.ts`, add `"src/components/page/section.tsx"` and `"src/components/page/state-text.tsx"` to the components group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/components/page src/pages/dashboard.test.tsx src/a11y.test.tsx`
Expected: PASS; the Dashboard's regions are still found by name.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): share Section and add StateText"
```

---

### Task 4: Add the form row, error line and save bar

**Files:**
- Create: `web/src/components/page/field.tsx`, `web/src/components/page/error-line.tsx`
- Modify: `web/src/components/save-bar.tsx` (`SaveBar`, `RejectedAlert`)
- Test: `web/src/components/page/field.test.tsx`, `web/src/components/save-bar.test.tsx` (create), `web/src/pages/repository.test.tsx:106`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `FieldGroup`, `Field`, `ErrorLine` (Contracts, Page kit); `SaveBar` and `RejectedAlert` keep their props.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/page/field.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { Input } from "darkraise-ui/components/input"
import { describe, expect, it, vi } from "vitest"
import { ErrorLine } from "./error-line"
import { Field, FieldGroup } from "./field"

describe("FieldGroup", () => {
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
})

describe("Field", () => {
  it("labels its control and shows help, an error and a changed mark", () => {
    render(
      <Field label="Poll interval" htmlFor="poll" help="How often GitHub is checked" error="Enter a duration" changed>
        <Input id="poll" />
      </Field>,
    )
    expect(screen.getByLabelText("Poll interval")).toHaveAttribute("id", "poll")
    expect(screen.getByText("How often GitHub is checked")).toHaveClass("text-muted-foreground")
    expect(screen.getByText("Enter a duration")).toHaveClass("text-destructive")
    expect(screen.getByText("Changed")).toHaveClass("text-warning")
  })

  it("renders a read-only row's label as text, not a label element", () => {
    const { container } = render(
      <Field label="Owner">
        <span>darkraise</span>
      </Field>,
    )
    expect(container.querySelector("label")).toBeNull()
    expect(screen.getByText("Owner")).toBeInTheDocument()
    expect(screen.queryByText("Changed")).toBeNull()
  })
})

describe("ErrorLine", () => {
  it("announces the error and offers a retry when given one", async () => {
    const onRetry = vi.fn()
    render(<ErrorLine onRetry={onRetry}>history file unreadable</ErrorLine>)
    expect(screen.getByRole("alert")).toHaveTextContent("history file unreadable")
    await userEvent.setup().click(screen.getByRole("button", { name: "Try again" }))
    expect(onRetry).toHaveBeenCalledOnce()
  })

  it("has no retry button without onRetry", () => {
    render(<ErrorLine>boom</ErrorLine>)
    expect(screen.queryByRole("button")).toBeNull()
  })
})
```

Create `web/src/components/save-bar.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { RejectedAlert, SaveBar } from "./save-bar"

describe("SaveBar", () => {
  it("counts the changes, then saves or discards", async () => {
    const onSave = vi.fn()
    const onDiscard = vi.fn()
    render(<SaveBar count={2} saving={false} onSave={onSave} onDiscard={onDiscard} />)
    expect(screen.getByText("2 unsaved changes")).toHaveClass("text-warning")
    const user = userEvent.setup()
    await user.click(screen.getByRole("button", { name: "Save changes" }))
    await user.click(screen.getByRole("button", { name: "Discard" }))
    expect(onSave).toHaveBeenCalledOnce()
    expect(onDiscard).toHaveBeenCalledOnce()
  })

  it("renders nothing without changes", () => {
    const { container } = render(<SaveBar count={0} saving={false} onSave={() => {}} onDiscard={() => {}} />)
    expect(container).toBeEmptyDOMElement()
  })
})

describe("RejectedAlert", () => {
  it("lists three reasons and counts the rest", () => {
    render(<RejectedAlert message="a is bad; b is bad; c is bad; d is bad; e is bad" />)
    expect(screen.getByText("a is bad")).toBeInTheDocument()
    expect(screen.queryByText("d is bad")).toBeNull()
    expect(screen.getByText("and 2 more")).toBeInTheDocument()
    expect(screen.queryByText(/✖|…/)).toBeNull()
  })
})
```

In `web/src/pages/repository.test.tsx:106`, change `"… and 1 more"` to `"and 1 more"`.

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/components/page/field.test.tsx src/components/save-bar.test.tsx`
Expected: FAIL: `Cannot find module './field'`, and `RejectedAlert` still renders `… and 2 more`.

- [ ] **Step 3: Write the implementation**

Create `web/src/components/page/error-line.tsx`:

```tsx
import { Button } from "darkraise-ui/components/button"
import { CircleAlert } from "lucide-react"
import type { ReactNode } from "react"

export function ErrorLine({ children, onRetry }: { children: ReactNode; onRetry?: () => void }) {
  return (
    <div role="alert" className="flex flex-wrap items-center gap-2 text-sm text-destructive">
      <CircleAlert size={15} aria-hidden="true" className="shrink-0" />
      <span>{children}</span>
      {onRetry && (
        <Button size="sm" variant="outline" onClick={onRetry}>
          Try again
        </Button>
      )}
    </div>
  )
}
```

Create `web/src/components/page/field.tsx`:

```tsx
import { Label } from "darkraise-ui/components/label"
import { CircleAlert } from "lucide-react"
import type { ReactNode } from "react"

// The heading takes focus when the section index jumps here, so it carries
// tabIndex -1 and an id the index can find.
export function FieldGroup({ id, title, note, children }: { id: string; title: string; note?: string; children: ReactNode }) {
  return (
    <section id={id} aria-labelledby={`${id}-title`} className="scroll-mt-4">
      <h2 id={`${id}-title`} tabIndex={-1} className="text-base font-semibold">
        {title}
      </h2>
      {note && <p className="mt-1 text-sm text-muted-foreground">{note}</p>}
      <div className="mt-2 divide-y divide-border border-y border-border">{children}</div>
    </section>
  )
}

export function Field({
  label,
  htmlFor,
  help,
  error,
  changed,
  children,
}: {
  label: string
  htmlFor?: string
  help?: ReactNode
  error?: string
  changed?: boolean
  children: ReactNode
}) {
  return (
    <div className="grid gap-1 py-3 sm:grid-cols-[12rem_minmax(0,1fr)] sm:items-start sm:gap-4">
      {htmlFor ? (
        <Label htmlFor={htmlFor} className="sm:pt-2">
          {label}
        </Label>
      ) : (
        <span className="text-sm font-medium sm:pt-2">{label}</span>
      )}
      <div className="flex min-w-0 flex-col gap-1">
        <div className="flex flex-wrap items-center gap-2">
          {children}
          {changed && <span className="text-xs text-warning">Changed</span>}
        </div>
        {help && <p className="text-xs text-muted-foreground">{help}</p>}
        {error && (
          <p className="flex items-center gap-1 text-xs text-destructive">
            <CircleAlert size={13} aria-hidden="true" className="shrink-0" />
            {error}
          </p>
        )}
      </div>
    </div>
  )
}
```

Replace `web/src/components/save-bar.tsx` with:

```tsx
import { Alert, AlertDescription, AlertTitle } from "darkraise-ui/components/alert"
import { Button } from "darkraise-ui/components/button"
import { CircleAlert } from "lucide-react"
import { rejected, unsavedText } from "@/lib/draft"

export function SaveBar({
  count,
  saving,
  disabled,
  onSave,
  onDiscard,
}: {
  count: number
  saving: boolean
  disabled?: boolean
  onSave: () => void
  onDiscard: () => void
}) {
  if (count === 0) return null
  return (
    <div className="sticky bottom-0 z-10 mt-4 flex flex-wrap items-center justify-between gap-3 rounded-[10px] border border-border bg-card p-3">
      <span className="text-sm text-warning">{unsavedText(count)}</span>
      <div className="flex gap-2">
        <Button variant="outline" disabled={saving || disabled} onClick={onDiscard}>
          Discard
        </Button>
        <Button loading={saving} disabled={saving || disabled} onClick={onSave}>
          Save changes
        </Button>
      </div>
    </div>
  )
}

export function RejectedAlert({ message }: { message: string }) {
  const { lines, more } = rejected(message)
  return (
    <Alert variant="destructive" className="mb-4">
      <AlertTitle>Save rejected</AlertTitle>
      <AlertDescription>
        <ul className="flex flex-col gap-1">
          {lines.map((line) => (
            <li key={line} className="flex items-center gap-1.5">
              <CircleAlert size={13} aria-hidden="true" className="shrink-0" />
              <span>{line}</span>
            </li>
          ))}
          {more > 0 && <li>{`and ${more} more`}</li>}
        </ul>
      </AlertDescription>
    </Alert>
  )
}
```

In `web/src/anti-slop.test.ts`, add `"src/components/page/error-line.tsx"`, `"src/components/page/field.tsx"` and `"src/components/save-bar.tsx"` to the components group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/components src/pages/repository.test.tsx src/pages/settings.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): add form rows, error line and save bar"
```

---

### Task 5: Switch layouts by screen width

**Files:**
- Create: `web/src/lib/use-media-query.ts`, `web/src/components/page/split-view.tsx`, `web/src/test/media.ts`
- Test: `web/src/components/page/split-view.test.tsx`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `useMediaQuery`, `WIDE`, `WIDEST`, `SplitView`, `setViewport` (Contracts, Media and Page kit).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/components/page/split-view.test.tsx`:

```tsx
import { act, render, renderHook, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { useMediaQuery, WIDE, WIDEST } from "@/lib/use-media-query"
import { setViewport } from "@/test/media"
import { SplitView } from "./split-view"

const view = (panel: string | null = "panel") => (
  <SplitView list={<p>list</p>} panel={panel === null ? null : <p>{panel}</p>} narrow={<p>narrow</p>} />
)

describe("useMediaQuery", () => {
  it("matches min-width queries against the viewport", () => {
    setViewport(1100)
    expect(renderHook(() => useMediaQuery(WIDE)).result.current).toBe(true)
    expect(renderHook(() => useMediaQuery(WIDEST)).result.current).toBe(false)
  })
})

describe("SplitView", () => {
  it("puts the list beside the panel at 1024px and wider", () => {
    setViewport(1280)
    render(view())
    expect(screen.getByText("list")).toBeInTheDocument()
    expect(screen.getByText("panel")).toBeInTheDocument()
    expect(screen.queryByText("narrow")).toBeNull()
  })

  it("shows only the narrow content below 1024px", () => {
    setViewport(390)
    render(view())
    expect(screen.getByText("narrow")).toBeInTheDocument()
    expect(screen.queryByText("list")).toBeNull()
  })

  it("shows the list alone when there is no panel", () => {
    setViewport(1280)
    render(view(null))
    expect(screen.getByText("list")).toBeInTheDocument()
    expect(screen.queryByText("narrow")).toBeNull()
  })

  it("follows the width when it changes", () => {
    const media = setViewport(390)
    render(view())
    expect(screen.getByText("narrow")).toBeInTheDocument()
    act(() => media.resize(1280))
    expect(screen.getByText("panel")).toBeInTheDocument()
    expect(screen.queryByText("narrow")).toBeNull()
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run (in `web/`): `npx vitest run src/components/page/split-view.test.tsx`
Expected: FAIL: `Cannot find module '@/lib/use-media-query'`.

- [ ] **Step 3: Write the implementation**

Create `web/src/lib/use-media-query.ts`:

```ts
import { useSyncExternalStore } from "react"

export const WIDE = "(min-width: 1024px)"
export const WIDEST = "(min-width: 1280px)"

export function useMediaQuery(query: string): boolean {
  return useSyncExternalStore(
    (onChange) => {
      const media = window.matchMedia(query)
      media.addEventListener("change", onChange)
      return () => media.removeEventListener("change", onChange)
    },
    () => window.matchMedia(query).matches,
    () => false,
  )
}
```

Create `web/src/components/page/split-view.tsx`:

```tsx
import type { ReactNode } from "react"
import { useMediaQuery, WIDE } from "@/lib/use-media-query"

export function SplitView({ list, panel, narrow }: { list: ReactNode; panel: ReactNode | null; narrow: ReactNode }) {
  const wide = useMediaQuery(WIDE)
  if (!wide) return <>{narrow}</>
  if (panel === null) return <div className="min-w-0">{list}</div>
  return (
    <div className="grid grid-cols-[22rem_minmax(0,1fr)] items-start gap-4">
      <div className="min-w-0">{list}</div>
      <div className="min-w-0">{panel}</div>
    </div>
  )
}
```

Create `web/src/test/media.ts`:

```ts
import { onTestFinished } from "vitest"

// jsdom has no layout, so a test picks a width: (min-width: Npx) queries
// match against it, and resize() tells the listeners. The default stub in
// setup.ts comes back after the test.
export function setViewport(width: number): { resize(width: number): void } {
  let current = width
  const listeners = new Set<() => void>()
  const original = window.matchMedia
  Object.defineProperty(window, "matchMedia", {
    writable: true,
    configurable: true,
    value: (query: string) => {
      const min = /\(min-width:\s*(\d+)px\)/.exec(query)
      return {
        get matches() {
          return min ? current >= Number(min[1]) : false
        },
        media: query,
        onchange: null,
        addEventListener: (_type: string, fn: () => void) => listeners.add(fn),
        removeEventListener: (_type: string, fn: () => void) => listeners.delete(fn),
        addListener: () => {},
        removeListener: () => {},
        dispatchEvent: () => false,
      }
    },
  })
  onTestFinished(() => {
    Object.defineProperty(window, "matchMedia", { writable: true, configurable: true, value: original })
  })
  return {
    resize(next: number) {
      current = next
      for (const fn of listeners) fn()
    },
  }
}
```

In `web/src/anti-slop.test.ts`, add `"src/components/page/split-view.tsx"` to the components group and `"src/lib/use-media-query.ts"` to the lib group.

- [ ] **Step 4: Run the test to verify it passes**

Run (in `web/`): `npx vitest run src/components/page/split-view.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): switch layouts by screen width"
```

---

### Task 6: Jump between sections without leaving the page

**Files:**
- Create: `web/src/components/page/section-nav.tsx`
- Modify: `web/src/components/unsaved-guard.tsx` (`shouldBlockFn`)
- Test: `web/src/components/page/section-nav.test.tsx`, `web/src/components/unsaved-guard.test.tsx`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `FieldGroup` (Task 4), `useMediaQuery`/`WIDEST` and `setViewport` (Task 5).
- Produces: `SectionNav`, `NavItem` (Contracts, Page kit); `UnsavedGuard` lets same-pathname navigations through.

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/page/section-nav.test.tsx`:

```tsx
import { fireEvent, render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { setViewport } from "@/test/media"
import { FieldGroup } from "./field"
import { SectionNav } from "./section-nav"

const ITEMS = [
  { id: "general", title: "General" },
  { id: "timing", title: "Timing" },
] as const

function Page() {
  return (
    <>
      <SectionNav items={ITEMS} />
      <FieldGroup id="general" title="General">
        <p>general rows</p>
      </FieldGroup>
      <FieldGroup id="timing" title="Timing">
        <p>timing rows</p>
      </FieldGroup>
    </>
  )
}

function placeAt(id: string, top: number) {
  const el = document.getElementById(id)
  if (!el) throw new Error(`no #${id}`)
  vi.spyOn(el, "getBoundingClientRect").mockReturnValue({ top } as DOMRect)
}

describe("SectionNav", () => {
  it("is not shown below 1280px", () => {
    setViewport(1024)
    render(<Page />)
    expect(screen.queryByRole("navigation", { name: "Sections" })).toBeNull()
  })

  it("scrolls to a section and focuses its heading", async () => {
    setViewport(1280)
    const scroll = vi.spyOn(Element.prototype, "scrollIntoView")
    render(<Page />)
    await userEvent.setup().click(screen.getByRole("button", { name: "Timing" }))
    expect(scroll).toHaveBeenCalled()
    expect(scroll.mock.contexts.at(-1)).toBe(document.getElementById("timing"))
    expect(document.activeElement).toBe(screen.getByRole("heading", { name: "Timing" }))
  })

  it("marks the section nearest the top as current while scrolling", () => {
    setViewport(1280)
    render(<Page />)
    expect(screen.getByRole("button", { name: "General" })).toHaveAttribute("aria-current", "true")
    placeAt("general", -400)
    placeAt("timing", 40)
    fireEvent.scroll(document)
    expect(screen.getByRole("button", { name: "Timing" })).toHaveAttribute("aria-current", "true")
    expect(screen.getByRole("button", { name: "General" })).not.toHaveAttribute("aria-current")
  })
})
```

Append to `section-nav.test.tsx` a case that renders the index beside a guarded form, so a jump with unsaved changes is checked end to end (add these two imports at the top of the file, beside the existing ones):

```tsx
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router"
import { UnsavedGuard } from "@/components/unsaved-guard"
```

```tsx
describe("SectionNav beside unsaved changes", () => {
  it("jumps without opening the unsaved-changes dialog or changing the URL", async () => {
    setViewport(1280)
    function Form() {
      return (
        <>
          <Page />
          <UnsavedGuard count={1} page="Settings" saving={false} onSave={async () => true} onDiscard={() => {}} />
        </>
      )
    }
    const root = createRootRoute({ component: () => <Outlet /> })
    const form = createRoute({ getParentRoute: () => root, path: "/settings", component: Form })
    const router = createRouter({ routeTree: root.addChildren([form]), history: createMemoryHistory({ initialEntries: ["/settings"] }) })
    render(<RouterProvider router={router} />)
    await userEvent.setup().click(await screen.findByRole("button", { name: "Timing" }))
    expect(screen.queryByRole("alertdialog")).toBeNull()
    expect(screen.queryByText("You have 1 unsaved change on the Settings page.")).toBeNull()
    expect(router.state.location.href).toBe("/settings")
  })
})
```

Append to `web/src/components/unsaved-guard.test.tsx`, inside `describe("UnsavedGuard", …)`:

```tsx
  it("lets a navigation within the same page through", async () => {
    const { router } = setup()
    await screen.findByText("form page")
    act(() => router.history.push("/#timing"))
    await waitFor(() => expect(router.state.location.hash).toBe("timing"))
    expect(screen.queryByText("You have 1 unsaved change on the Settings page.")).toBeNull()
    expect(router.state.location.pathname).toBe("/")
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/components/page/section-nav.test.tsx src/components/unsaved-guard.test.tsx`
Expected: FAIL: `Cannot find module './section-nav'`, and the guard opens its dialog for `/#timing`.

- [ ] **Step 3: Write the implementation**

Create `web/src/components/page/section-nav.tsx`:

```tsx
import { useEffect, useState } from "react"
import { useMediaQuery, WIDEST } from "@/lib/use-media-query"

export interface NavItem {
  id: string
  title: string
}

// A section sits under the page header once its top passes this line.
const CURRENT_LINE = 96

// Buttons, not hash links: a hash change is a navigation, and UnsavedGuard
// would treat it as leaving the page. items must be a stable array.
export function SectionNav({ items }: { items: readonly NavItem[] }) {
  const wide = useMediaQuery(WIDEST)
  const [current, setCurrent] = useState(items[0]?.id)

  useEffect(() => {
    function onScroll() {
      let found = items[0]?.id
      for (const item of items) {
        const el = document.getElementById(item.id)
        if (el && el.getBoundingClientRect().top <= CURRENT_LINE) found = item.id
      }
      setCurrent(found)
    }
    // Capture catches the scroll of whichever element holds the page.
    document.addEventListener("scroll", onScroll, { capture: true, passive: true })
    return () => document.removeEventListener("scroll", onScroll, { capture: true })
  }, [items])

  if (!wide) return null

  function jump(id: string) {
    document.getElementById(id)?.scrollIntoView({ block: "start" })
    document.getElementById(`${id}-title`)?.focus({ preventScroll: true })
    setCurrent(id)
  }

  return (
    <nav aria-label="Sections" className="sticky top-4 self-start">
      <ul className="flex flex-col gap-0.5 border-l border-border text-sm">
        {items.map((item) => (
          <li key={item.id}>
            <button
              type="button"
              aria-current={item.id === current ? "true" : undefined}
              onClick={() => jump(item.id)}
              className={`-ml-px w-full border-l-2 px-3 py-1 text-left ${
                item.id === current ? "border-primary font-medium text-foreground" : "border-transparent text-muted-foreground hover:text-foreground"
              }`}
            >
              {item.title}
            </button>
          </li>
        ))}
      </ul>
    </nav>
  )
}
```

In `web/src/components/unsaved-guard.tsx`, replace the `useBlocker` line:

```tsx
  // A navigation inside the page (a hash change) never leaves the form, and
  // blocking one makes the router undo it with a reload.
  const blocker = useBlocker({
    shouldBlockFn: ({ current, next }) => (count > 0 || saving) && next.pathname !== current.pathname,
    enableBeforeUnload: count > 0,
    withResolver: true,
  })
```

In `web/src/anti-slop.test.ts`, add `"src/components/page/section-nav.tsx"` and `"src/components/unsaved-guard.tsx"` to the components group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/components src/pages/settings.test.tsx src/pages/repository.test.tsx src/app.test.tsx`
Expected: PASS; every existing guard test (leaving the page, Stay, Discard, Save) still passes.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): jump between sections in place"
```

---

### Task 7: Draw the jobs chart and pick a bucket

**Files:**
- Modify: `web/src/components/buckets-chart.tsx`
- Modify: `web/src/components/window-control.tsx`
- Test: `web/src/components/buckets-chart.test.tsx`, `web/src/components/window-control.test.tsx`
- Modify: `web/src/anti-slop.test.ts` (add `src/components/buckets-chart.tsx` if absent; it is listed today)

**Interfaces:**
- Consumes: nothing.
- Produces: `BucketsChart`'s `tracks`, `picked`, `onPick`; `WindowControl`'s `options` (Contracts, Charts).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `web/src/components/buckets-chart.test.tsx`, add `ActivityBucket` to the type import, add `userEvent` (`import userEvent from "@testing-library/user-event"`) and `vi` to the vitest import, and replace `draw` with:

```tsx
function draw(over: Partial<Activity> = {}, props: Partial<Parameters<typeof BucketsChart>[0]> = {}) {
  return render(<BucketsChart activity={{ ...base, ...over }} now={now} width={960} {...props} />)
}
```

Append inside `describe("BucketsChart", …)`:

```tsx
  it("draws only busy time and finished jobs in jobs mode", () => {
    const { container } = draw({}, { tracks: "jobs" })
    expect(screen.getByRole("group", { name: "Jobs per bucket for the last 24 hours" })).toBeInTheDocument()
    expect(screen.getByText("Busy")).toBeInTheDocument()
    expect(screen.getByText("Runs")).toBeInTheDocument()
    expect(screen.queryByText("Waiting")).toBeNull()
    expect(screen.queryByText("CPU")).toBeNull()
    expect(container.querySelectorAll("[data-track]")).toHaveLength(0)
    expect(screen.queryByText(/^Collecting data since/)).toBeNull()
  })

  it("lets a bucket be picked by click, Enter or Space when asked", async () => {
    const onPick = vi.fn<(b: ActivityBucket) => void>()
    draw({}, { tracks: "jobs", onPick, picked: "2026-10-03T13:00:00Z" })
    const buttons = screen.getAllByRole("button")
    expect(buttons).toHaveLength(25)
    const thirteen = screen.getByRole("button", { name: /^13:00 to 14:00/ })
    expect(thirteen).toHaveAttribute("aria-pressed", "true")
    expect(buttons.filter((b) => b.getAttribute("aria-pressed") === "true")).toHaveLength(1)
    const user = userEvent.setup()
    await user.click(thirteen)
    thirteen.focus()
    await user.keyboard("{Enter}")
    await user.keyboard(" ")
    expect(onPick).toHaveBeenCalledTimes(3)
    expect(onPick.mock.calls[0]?.[0].start).toBe("2026-10-03T13:00:00Z")
  })

  it("keeps image buckets without onPick", () => {
    draw()
    expect(screen.queryAllByRole("button")).toHaveLength(0)
    expect(screen.getAllByRole("img")).toHaveLength(25)
  })
```

In `web/src/components/window-control.test.tsx`, append inside `describe("WindowControl", …)`:

```tsx
  it("offers only the windows it is given", async () => {
    const onChange = vi.fn()
    render(<WindowControl value="7d" onChange={onChange} options={["24h", "7d", "30d"] as const} />)
    expect(screen.getAllByRole("radio").map((r) => r.textContent)).toEqual(["24h", "7d", "30d"])
    await userEvent.setup().click(screen.getByRole("radio", { name: "30d" }))
    expect(onChange).toHaveBeenCalledWith("30d")
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/components/buckets-chart.test.tsx src/components/window-control.test.tsx`
Expected: FAIL: jobs mode still draws four tracks, there are no buttons, and `WindowControl` still offers five windows.

- [ ] **Step 3: Write the implementation**

In `web/src/components/buckets-chart.tsx`:

1. Change the type import to `import type { Activity, ActivityBucket } from "@/api/types"`.
2. Replace the component's signature and the geometry lines down to `const height = axisTop + AXIS_H`:

```tsx
export function BucketsChart({
  activity,
  now,
  width,
  tracks: mode = "all",
  picked = null,
  onPick,
}: {
  activity: Activity
  now: number
  width: number
  tracks?: "all" | "jobs"
  picked?: string | null
  onPick?: (bucket: ActivityBucket) => void
}) {
  const buckets = activity.buckets
  const jobs = mode === "jobs"
  const trackCount = jobs ? 2 : 4
  const plotW = width - GUTTER - PAD_RIGHT
  const colW = plotW / Math.max(1, buckets.length)
  const colX = (i: number) => GUTTER + i * colW
  const center = (i: number) => colX(i) + colW / 2
  const top = (k: number) => k * (TRACK_H + TRACK_GAP)
  const axisTop = top(trackCount - 1) + TRACK_H
  const height = axisTop + AXIS_H
```

3. Replace the `tracks` array with:

```tsx
  const allTracks: [string, string][] = [
    ["Busy", all ? `peak ${Math.round(busyPeak)} min` : `peak ${Math.round(busyPeak)}%`],
    ["Runs", `peak ${runsPeak}`],
    ["Waiting", known(waits).length > 0 ? `peak ${waitPeak}` : ""],
    ["CPU", known(cpus).length > 0 ? `peak ${Math.round(cpuPeak)}%` : ""],
  ]
  const tracks = allTracks.slice(0, trackCount)
```

4. Change the `<svg>`'s `aria-label` to:

```tsx
      aria-label={`${jobs ? "Jobs" : "Activity"} per bucket for ${windowWords(activity.window)}`}
```

5. Replace the bucket `<g>` element (keep its children) with:

```tsx
          <g
            key={b.start}
            tabIndex={0}
            role={onPick ? "button" : "img"}
            aria-label={label}
            aria-pressed={onPick ? picked === b.start : undefined}
            data-open={open ? "true" : undefined}
            className={`ghr-mark ${open ? "opacity-60" : ""} ${onPick ? "cursor-pointer" : ""}`}
            onClick={onPick ? () => onPick(b) : undefined}
            onKeyDown={
              onPick
                ? (e) => {
                    if (e.key === "Enter" || e.key === " ") {
                      e.preventDefault()
                      onPick(b)
                    }
                  }
                : undefined
            }
          >
```

and, as the last child inside that `<g>` (after the failed-runs `rect`), add:

```tsx
            {picked === b.start && (
              <rect data-picked="true" x={colX(i)} y={0} width={colW} height={axisTop} strokeWidth={1.5} className="fill-none stroke-primary" />
            )}
```

6. Wrap the host lines so jobs mode draws none of them:

```tsx
      {!jobs && (
        <>
          <Line runs={waitRuns} track="waiting" stroke="stroke-warning" fill="fill-warning" />
          <Line runs={cpuRuns} track="cpu" stroke="stroke-primary" fill="fill-primary" />
          {collecting(waits, 2)}
          {collecting(cpus, 3)}
        </>
      )}
```

Replace `web/src/components/window-control.tsx` with:

```tsx
import { ToggleGroup, ToggleGroupItem } from "darkraise-ui/components/toggle-group"
import type { ActivityWindow } from "@/api/types"
import { ACTIVITY_WINDOWS } from "@/lib/activity-view"

export function WindowControl<W extends ActivityWindow = ActivityWindow>({
  value,
  onChange,
  options = ACTIVITY_WINDOWS as readonly W[],
}: {
  value: W
  onChange: (w: W) => void
  options?: readonly W[]
}) {
  return (
    <ToggleGroup
      type="single"
      size="sm"
      variant="outline"
      value={value}
      aria-label="Time window"
      onValueChange={(next) => {
        const w = options.find((x) => x === next)
        if (w) onChange(w)
      }}
    >
      {options.map((w) => (
        <ToggleGroupItem key={w} value={w} className="font-mono">
          {w}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/components src/pages/dashboard.test.tsx src/a11y.test.tsx`
Expected: PASS; the Dashboard chart is unchanged.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): draw the jobs chart and pick a bucket"
```

---

### Task 8: Draw a runner's steps on a time axis

**Files:**
- Create: `web/src/lib/steps.ts`, `web/src/components/step-list.tsx`
- Test: `web/src/lib/steps.test.ts`, `web/src/components/step-list.test.tsx`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: plan 1's `Step.started_at` and `Step.completed_at`.
- Produces: `stepPhase`, `StepPhase`, `stepTimeline`, `StepTimeline`, `StepList` (Contracts, Runners).

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing tests**

Create `web/src/lib/steps.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import type { Step } from "@/api/types"
import { stepPhase, stepTimeline } from "./steps"

const t = (hms: string) => `2026-10-03T${hms}Z`
const steps: Step[] = [
  { number: 1, name: "checkout", status: "completed", conclusion: "success", started_at: t("14:00:00"), completed_at: t("14:01:00") },
  { number: 2, name: "test", status: "in_progress", conclusion: "", started_at: t("14:01:00") },
  { number: 3, name: "upload", status: "queued", conclusion: "" },
]

describe("stepPhase", () => {
  it.each([
    ["in_progress", "running"],
    ["completed", "completed"],
    ["queued", "pending"],
    ["waiting", "pending"],
    ["pending", "pending"],
  ])("reads %s as %s", (status, phase) => {
    expect(stepPhase({ number: 1, name: "x", status, conclusion: "" })).toBe(phase)
  })
})

describe("stepTimeline", () => {
  it("runs the axis to now while the job runs", () => {
    const tl = stepTimeline(steps, Date.parse(t("14:03:00")), true)
    expect(tl.bars).toEqual([{ offset: 0, width: 1 / 3 }, { offset: 1 / 3, width: 2 / 3 }, null])
    expect(tl.durations).toEqual([60_000, 120_000, null])
  })

  it("ends the axis at the last step's end once the runner has finished", () => {
    const done: Step[] = [
      steps[0] as Step,
      { ...(steps[1] as Step), status: "completed", conclusion: "failure", completed_at: t("14:02:00") },
      steps[2] as Step,
    ]
    const tl = stepTimeline(done, Date.parse(t("14:30:00")), false)
    expect(tl.bars).toEqual([{ offset: 0, width: 0.5 }, { offset: 0.5, width: 0.5 }, null])
    expect(tl.durations).toEqual([60_000, 60_000, null])
  })

  it("draws nothing when no step reports a time", () => {
    const tl = stepTimeline([{ number: 1, name: "x", status: "queued", conclusion: "" }], Date.parse(t("14:00:00")), true)
    expect(tl.bars).toEqual([null])
    expect(tl.durations).toEqual([null])
  })
})
```

Create `web/src/components/step-list.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Step } from "@/api/types"
import { StepList } from "./step-list"

const t = (hms: string) => `2026-10-03T${hms}Z`

describe("StepList", () => {
  it("names each step's state and draws its time on the axis", () => {
    const steps: Step[] = [
      { number: 1, name: "checkout", status: "completed", conclusion: "success", started_at: t("14:00:00"), completed_at: t("14:01:00") },
      { number: 2, name: "lint", status: "completed", conclusion: "neutral", started_at: t("14:01:00"), completed_at: t("14:01:30") },
      { number: 3, name: "test", status: "in_progress", conclusion: "", started_at: t("14:01:30") },
      { number: 4, name: "upload", status: "queued", conclusion: "" },
    ]
    const { container } = render(<StepList steps={steps} now={Date.parse(t("14:03:00"))} live />)
    expect(screen.getByRole("list", { name: "Steps" })).toBeInTheDocument()
    expect(screen.getByRole("img", { name: "Succeeded" })).toBeInTheDocument()
    expect(screen.getByRole("img", { name: "Failed" })).toBeInTheDocument()
    expect(screen.getByText("Running")).toHaveClass("sr-only")
    expect(screen.getByRole("img", { name: "Pending" })).toBeInTheDocument()
    expect(screen.getByText("upload")).toHaveClass("text-muted-foreground")
    expect(screen.getByText("1m00s")).toBeInTheDocument()
    expect(screen.getByText("1m30s")).toBeInTheDocument()
    const bars = container.querySelectorAll<HTMLElement>("[data-bar]")
    expect(bars).toHaveLength(3)
    expect(bars[0]?.style.left).toBe("0%")
    expect(bars[2]?.style.left).toBe("50%")
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/lib/steps.test.ts src/components/step-list.test.tsx`
Expected: FAIL: `Cannot find module './steps'` and `'./step-list'`.

- [ ] **Step 3: Write the implementation**

Create `web/src/lib/steps.ts`:

```ts
import type { Step } from "@/api/types"

export type StepPhase = "running" | "completed" | "pending"

export function stepPhase(step: Step): StepPhase {
  if (step.status === "in_progress") return "running"
  if (step.status === "completed") return "completed"
  return "pending"
}

export interface StepTimeline {
  bars: ({ offset: number; width: number } | null)[]
  durations: (number | null)[]
}

function stepEnd(step: Step, now: number): number | null {
  if (!step.started_at) return null
  if (step.completed_at) return Date.parse(step.completed_at)
  return stepPhase(step) === "running" ? now : Date.parse(step.started_at)
}

// The axis runs from the first start to now while the runner is live, and to
// the last end once it has finished. Offsets and widths are fractions of it.
export function stepTimeline(steps: Step[], now: number, live: boolean): StepTimeline {
  const starts = steps.flatMap((s) => (s.started_at ? [Date.parse(s.started_at)] : []))
  if (starts.length === 0) return { bars: steps.map(() => null), durations: steps.map(() => null) }
  const from = Math.min(...starts)
  const ends = steps.map((s) => stepEnd(s, now))
  const to = live ? now : Math.max(from, ...ends.flatMap((e) => (e === null ? [] : [e])))
  const span = Math.max(1, to - from)
  const bars: StepTimeline["bars"] = []
  const durations: StepTimeline["durations"] = []
  steps.forEach((s, i) => {
    const end = ends[i]
    if (!s.started_at || end === null || end === undefined) {
      bars.push(null)
      durations.push(null)
      return
    }
    const start = Date.parse(s.started_at)
    bars.push({ offset: (start - from) / span, width: Math.max(0, end - start) / span })
    durations.push(Math.max(0, end - start))
  })
  return { bars, durations }
}
```

Create `web/src/components/step-list.tsx`:

```tsx
import { Spinner } from "darkraise-ui/components/spinner"
import { Circle } from "lucide-react"
import type { Step } from "@/api/types"
import { ResultIcon } from "@/components/result-icon"
import { dur } from "@/lib/format"
import { stepPhase, stepTimeline } from "@/lib/steps"

function StepIcon({ step }: { step: Step }) {
  const phase = stepPhase(step)
  if (phase === "running") {
    return (
      <span className="inline-flex">
        <Spinner size="sm" label={<span className="sr-only">Running</span>} />
      </span>
    )
  }
  if (phase === "completed") return <ResultIcon conclusion={step.conclusion} />
  return <Circle role="img" aria-label="Pending" size={15} className="shrink-0 text-muted-foreground" />
}

export function StepList({ steps, now, live }: { steps: Step[]; now: number; live: boolean }) {
  const timeline = stepTimeline(steps, now, live)
  return (
    <ol aria-label="Steps" className="flex flex-col">
      {steps.map((s, i) => {
        const bar = timeline.bars[i]
        const took = timeline.durations[i]
        const pending = stepPhase(s) === "pending"
        return (
          <li
            key={s.number}
            className="grid grid-cols-[1.25rem_minmax(0,1fr)_4.5rem_minmax(6rem,35%)] items-center gap-2 border-b border-border py-1.5 text-sm last:border-b-0"
          >
            <StepIcon step={s} />
            <span className={`break-words ${pending ? "text-muted-foreground" : ""}`}>{s.name}</span>
            <span className="text-right font-mono text-xs text-muted-foreground">{took === null || took === undefined ? "" : dur(took)}</span>
            <span aria-hidden="true" className="relative h-1.5 rounded-[2px] bg-muted">
              {bar && (
                <span
                  data-bar="true"
                  className={`absolute inset-y-0 rounded-[2px] ${stepPhase(s) === "running" ? "bg-primary" : "bg-primary/60"}`}
                  style={{ left: `${bar.offset * 100}%`, width: `max(2px, ${bar.width * 100}%)` }}
                />
              )}
            </span>
          </li>
        )
      })}
    </ol>
  )
}
```

In `web/src/anti-slop.test.ts`, add `"src/components/step-list.tsx"` to the components group and `"src/lib/steps.ts"` to the lib group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/lib/steps.test.ts src/components/step-list.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): draw a runner's steps on a time axis"
```

---

### Task 9: List the runners and the waiting repositories

**Files:**
- Create: `web/src/components/runner-list.tsx`, `web/src/test/routes.tsx`
- Modify: `web/src/lib/summary.ts` (add `runnersSummary`), `web/src/lib/status.ts` (add `waitingRepos`)
- Test: `web/src/components/runner-list.test.tsx`, `web/src/lib/summary.test.ts`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `DetailTab` — declared here as a local type `"steps" | "log" | "containers"` until Task 11 exports it from `runner-panel.tsx`; Task 11 switches this import.
- Produces: `RunnerList`, `waitingRepos`, `runnersSummary`, `renderRoutes` (Contracts, Runners and Test harness).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Append to `web/src/lib/summary.test.ts` (add `runnersSummary` to its import from `./summary`, and import `fixtures` from `@/test/fixtures` if it is not imported):

```ts
describe("runnersSummary", () => {
  it("counts live runners and every waiting job", () => {
    expect(runnersSummary(fixtures.status)).toBe("2 live, 3 jobs waiting")
  })
  it("leaves cleaning runners out and says when none is live", () => {
    const instances = fixtures.status.instances.map((i) => ({ ...i, state: "cleaning" }))
    expect(runnersSummary({ ...fixtures.status, instances, repos: [] })).toBe("No live runners")
  })
  it("names a single waiting job", () => {
    const repos = fixtures.status.repos.map((r) => ({ ...r, queued: r.name === "darkmem" ? 1 : 0 }))
    expect(runnersSummary({ ...fixtures.status, repos })).toBe("2 live, 1 job waiting")
  })
})
```

Create `web/src/test/routes.tsx`:

```tsx
import { QueryClientProvider } from "@tanstack/react-query"
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router"
import { render } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { Toaster } from "darkraise-ui/components/sonner"
import { TooltipProvider } from "darkraise-ui/components/tooltip"
import type { ReactNode } from "react"
import { createQueryClient } from "@/query"

// Renders components that need the router outside the app's route tree. Each
// key is a path pattern; its view may read params with useParams, and every
// route keeps its search as given.
export function renderRoutes(paths: Record<string, () => ReactNode>, initial: string) {
  const queryClient = createQueryClient()
  const root = createRootRoute({ component: () => <Outlet /> })
  const routes = Object.entries(paths).map(([path, view]) =>
    createRoute({
      getParentRoute: () => root,
      path,
      validateSearch: (search: Record<string, unknown>) => search,
      component: () => <>{view()}</>,
    }),
  )
  const router = createRouter({ routeTree: root.addChildren(routes), history: createMemoryHistory({ initialEntries: [initial] }) })
  const user = userEvent.setup()
  const view = render(
    <QueryClientProvider client={queryClient}>
      <TooltipProvider>
        <RouterProvider router={router} />
        <Toaster />
      </TooltipProvider>
    </QueryClientProvider>,
  )
  return { ...view, router, queryClient, user }
}
```

Create `web/src/components/runner-list.test.tsx`:

```tsx
import { useParams } from "@tanstack/react-router"
import { screen, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Status } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { renderRoutes } from "@/test/routes"
import { RunnerList } from "./runner-list"

const now = Date.parse("2026-10-03T14:05:00Z")

function ListAt({ status }: { status: Status | undefined }) {
  const { id } = useParams({ strict: false })
  return <RunnerList status={status} selected={id} tab="steps" now={now} />
}

function draw(status: Status | undefined = fixtures.status, path = "/runners/aaaaaa?tab=steps") {
  return renderRoutes(
    {
      "/runners": () => <ListAt status={status} />,
      "/runners/$id": () => <ListAt status={status} />,
      "/repositories": () => <p>repositories page</p>,
    },
    path,
  )
}

const link = (id: string) => screen.findByRole("link", { name: new RegExp(`^${id}`) })

describe("RunnerList", () => {
  it("shows each runner's state, repository, job and elapsed time", async () => {
    draw()
    const busy = await link("aaaaaa")
    expect(busy).toHaveTextContent("darkmem")
    expect(busy).toHaveTextContent("test #42")
    expect(busy).toHaveTextContent("4m00s")
    expect(busy).toHaveTextContent("Busy")
    expect(await link("bbbbbb")).toHaveTextContent("Idle")
  })

  it("marks the selected runner and makes only it tabbable", async () => {
    draw()
    const selected = await link("aaaaaa")
    expect(selected).toHaveAttribute("aria-current", "page")
    expect(selected).toHaveAttribute("tabindex", "0")
    expect(await link("bbbbbb")).toHaveAttribute("tabindex", "-1")
  })

  it("moves with the arrow, Home and End keys without adding history", async () => {
    const { router, user } = draw()
    const first = await link("aaaaaa")
    first.focus()
    const depth = router.history.length
    await user.keyboard("{ArrowDown}")
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/bbbbbb"))
    expect(document.activeElement).toBe(await link("bbbbbb"))
    expect(router.history.length).toBe(depth)
    await user.keyboard("{Home}")
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/aaaaaa"))
    await user.keyboard("{End}")
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/bbbbbb"))
  })

  it("lists repositories waiting at their cap", async () => {
    const repos = [{ name: "darkrouter", paused: false, max: 2, active: 2, queued: 2 }]
    draw({ ...fixtures.status, repos })
    expect(await screen.findByText("darkrouter, 2 queued, cap 2")).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "Waiting" })).toBeInTheDocument()
  })

  it("says when no runner is up and links to the repositories", async () => {
    draw({ ...fixtures.status, instances: [], repos: [] }, "/runners")
    expect(await screen.findByText("No runners. They start when a job is queued.")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Repositories" })).toHaveAttribute("href", "/repositories")
  })

  it("waits for the first status", async () => {
    draw(undefined, "/runners")
    expect(await screen.findByText("Waiting for the daemon")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/components/runner-list.test.tsx src/lib/summary.test.ts`
Expected: FAIL: `Cannot find module './runner-list'` and `runnersSummary` is not exported.

- [ ] **Step 3: Write the implementation**

Append to `web/src/lib/status.ts` (`RepoStatus` and `Status` are already imported there):

```ts
// A repository at its cap with jobs queued: the jobs wait on its own limit.
export function waitingRepos(status: Status): RepoStatus[] {
  return status.repos.filter((r) => !r.paused && r.queued > 0 && r.max > 0 && r.active >= r.max)
}
```

Append to `web/src/lib/summary.ts` (add `running` to an import from `./status`):

```ts
export function runnersSummary(status: Status): string {
  const live = running(status)
  const queued = status.repos.reduce((n, r) => n + r.queued, 0)
  const head = live === 0 ? "No live runners" : `${live} live`
  return queued > 0 ? `${head}, ${plural(queued, "job")} waiting` : head
}
```

Create `web/src/components/runner-list.tsx`:

```tsx
import { Link, useNavigate } from "@tanstack/react-router"
import { Spinner } from "darkraise-ui/components/spinner"
import { useRef, type KeyboardEvent } from "react"
import type { Status } from "@/api/types"
import { elapsed } from "@/lib/format"
import { waitingRepos } from "@/lib/status"

type DetailTab = "steps" | "log" | "containers"

// The light shows capacity like the shell's bar: busy pulses, warm is an
// outline, starting is faint.
const LIGHT: Record<string, string> = {
  busy: "ghr-pulse bg-primary",
  starting: "bg-primary/45",
  idle: "border border-primary",
}

const sentence = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

export function RunnerList({
  status,
  selected,
  tab,
  now,
}: {
  status: Status | undefined
  selected: string | undefined
  tab: DetailTab
  now: number
}) {
  const navigate = useNavigate()
  const links = useRef(new Map<string, HTMLAnchorElement>())
  if (!status) return <Spinner label="Waiting for the daemon" />

  const ids = status.instances.map((i) => i.id)
  const waiting = waitingRepos(status)
  if (ids.length === 0 && waiting.length === 0) {
    return (
      <div className="flex flex-col items-start gap-2 py-2">
        <p className="text-sm text-muted-foreground">No runners. They start when a job is queued.</p>
        <Link to="/repositories" className="text-sm text-primary hover:underline">
          Repositories
        </Link>
      </div>
    )
  }
  const tabbable = selected !== undefined && ids.includes(selected) ? selected : ids[0]

  // Arrowing replaces the URL, so Back leaves the page instead of walking
  // through every runner passed on the way.
  function onKeyDown(e: KeyboardEvent<HTMLUListElement>) {
    const at = ids.indexOf((document.activeElement as HTMLElement | null)?.dataset.runner ?? "")
    let next: number
    if (e.key === "ArrowDown") next = Math.min(ids.length - 1, at + 1)
    else if (e.key === "ArrowUp") next = Math.max(0, at - 1)
    else if (e.key === "Home") next = 0
    else if (e.key === "End") next = ids.length - 1
    else return
    e.preventDefault()
    const id = ids[next]
    if (id === undefined) return
    links.current.get(id)?.focus()
    void navigate({ to: "/runners/$id", params: { id }, search: { tab }, replace: true })
  }

  return (
    <div className="flex flex-col gap-4">
      {ids.length > 0 && (
        <ul aria-label="Runners" className="flex flex-col rounded-[10px] border border-border bg-card" onKeyDown={onKeyDown}>
          {status.instances.map((i) => (
            <li key={i.id} className="border-b border-border last:border-b-0">
              <Link
                to="/runners/$id"
                params={{ id: i.id }}
                search={{ tab }}
                data-runner={i.id}
                tabIndex={i.id === tabbable ? 0 : -1}
                ref={(el: HTMLAnchorElement | null) => {
                  if (el) links.current.set(i.id, el)
                  else links.current.delete(i.id)
                }}
                className={`grid grid-cols-[0.625rem_minmax(0,1fr)_auto] items-center gap-x-3 gap-y-0.5 px-3 py-2 text-sm hover:bg-muted/50 ${
                  i.id === selected ? "bg-muted" : ""
                }`}
              >
                <span aria-hidden="true" className={`size-2.5 rounded-[2px] ${LIGHT[i.state] ?? "bg-muted-foreground"}`} />
                <span className="flex min-w-0 flex-wrap items-baseline gap-x-2">
                  <span className="font-mono font-medium">{i.id}</span>
                  <span className="sr-only">{sentence(i.state)}</span>
                  <span className="text-muted-foreground">{i.repo}</span>
                </span>
                <span className="font-mono text-xs text-muted-foreground">{elapsed(i, now)}</span>
                <span />
                <span className="col-span-2 text-muted-foreground">
                  {i.job ? (
                    <>
                      {i.job.name} <span className="font-mono">#{i.job.run_number}</span>
                    </>
                  ) : (
                    <span aria-hidden="true">{sentence(i.state)}</span>
                  )}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
      {waiting.length > 0 && (
        <section aria-labelledby="runners-waiting" className="flex flex-col gap-1">
          <h2 id="runners-waiting" className="text-sm font-medium text-warning">
            Waiting
          </h2>
          <ul className="flex flex-col text-sm">
            {waiting.map((r) => (
              <li key={r.name} className="px-3 py-1">{`${r.name}, ${r.queued} queued, cap ${r.max}`}</li>
            ))}
          </ul>
        </section>
      )}
    </div>
  )
}
```

The idle row's visible word is `aria-hidden` because the `sr-only` span already names the state once; the test's `toHaveTextContent("Idle")` reads both.

In `web/src/anti-slop.test.ts`, add `"src/components/runner-list.tsx"` to the components group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/components/runner-list.test.tsx src/lib/summary.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): list runners and waiting repositories"
```

---

### Task 10: Move the stop dialog and the empty log line

**Files:**
- Create: `web/src/components/stop-runner-dialog.tsx`
- Modify: `web/src/components/runners-table.tsx` (remove `StopRunnerDialog`, re-export it)
- Modify: `web/src/components/log-view.tsx`
- Test: `web/src/components/log-view.test.tsx`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `StopRunnerDialog` at `@/components/stop-runner-dialog` (Contracts, Runners); `LogView` shows "No log output yet" outside its `<pre>`.

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing test**

Append inside `describe("LogView", …)` in `web/src/components/log-view.test.tsx` (add `screen` to its `@testing-library/react` import):

```tsx
  it("says when there is no output yet, outside the log itself", () => {
    const { container } = render(<LogView text="" follow />)
    const note = screen.getByText("No log output yet")
    expect(note.tagName).toBe("P")
    expect(container.querySelector("pre")).toBeEmptyDOMElement()
  })
```

- [ ] **Step 2: Run the test to verify it fails**

Run (in `web/`): `npx vitest run src/components/log-view.test.tsx`
Expected: FAIL: the text is `no log output yet`, inside the `<pre>`.

- [ ] **Step 3: Write the implementation**

Create `web/src/components/stop-runner-dialog.tsx` by moving `StopRunnerDialog` out of `runners-table.tsx` unchanged, with its own imports:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "darkraise-ui/components/alert-dialog"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useStatus } from "@/api/hooks"
import type { InstanceStatus } from "@/api/types"

export function StopRunnerDialog({ instance, onClose }: { instance: InstanceStatus | null; onClose: () => void }) {
  const queryClient = useQueryClient()
  const status = useStatus()
  // Holds the runner while the dialog animates closed, and reads its live
  // state so the wording follows a runner that picks up a job meanwhile.
  const [held, setHeld] = useState<InstanceStatus | null>(instance)
  if (instance !== null && instance.id !== held?.id) setHeld(instance)
  const current = held ? status.data?.instances.find((i) => i.id === held.id) : undefined
  const live = current ?? held
  // A runner can end while the dialog is open; there is then nothing to stop.
  // Not while closing: a runner just stopped here leaves /status meanwhile.
  const gone = instance !== null && status.data !== undefined && current === undefined
  const stop = useMutation({
    mutationFn: async (id: string) => {
      try {
        await api.stopRunner(id)
        return true
      } catch (err) {
        if (err instanceof ApiError && err.status === 404) return false
        throw err
      }
    },
    onSuccess: (stopped, id) => {
      if (stopped) toast.success(`stopped ${id}`)
      else toast.info(`${id} had already finished`)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.status }),
  })
  const busy = live?.state === "busy"
  let title = `Stop runner ${live?.id}?`
  let description = "ghr stops the runner and cleans it up."
  if (gone) {
    title = `Runner ${live?.id} has already finished`
    description = "There is nothing left to stop."
  } else if (busy) {
    title = `Runner ${live?.id} is running a job. Stop it?`
    description = "The job it is running fails."
  }
  return (
    <AlertDialog
      open={instance !== null}
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          <AlertDialogDescription>{description}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            data-variant="destructive"
            disabled={gone}
            onClick={() => {
              if (live) stop.mutate(live.id)
            }}
          >
            Stop
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
```

In `web/src/components/runners-table.tsx`, delete the `StopRunnerDialog` function, add `import { StopRunnerDialog } from "@/components/stop-runner-dialog"` and the line `export { StopRunnerDialog }` (so `pages/runner-detail.tsx` keeps importing it until Task 13), and remove the imports that only the dialog used (`ApiError` and `api`, the `AlertDialog…` components, `toast`, `useMutation`, `useQueryClient`, `keys`, `useStatus`) — run `npm run lint` to confirm none is left unused.

Replace the `return` of `LogView` in `web/src/components/log-view.tsx`:

```tsx
  return (
    <>
      {!text && <p className="mb-2 text-sm text-muted-foreground">No log output yet</p>}
      <pre
        ref={ref}
        onScroll={onScroll}
        className={`${className} overflow-auto whitespace-pre-wrap rounded-[6px] bg-muted p-3 font-mono text-xs`}
      >
        {text}
      </pre>
    </>
  )
```

In `web/src/anti-slop.test.ts`, add `"src/components/log-view.tsx"` and `"src/components/stop-runner-dialog.tsx"` to the components group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/components/log-view.test.tsx src/pages/runners.test.tsx src/pages/runner-detail.test.tsx`
Expected: PASS; the Stop flows on both old pages still work.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "refactor(web): move the stop dialog to its own file"
```

---

### Task 11: Build the runner panel

**Files:**
- Create: `web/src/components/runner-panel.tsx`
- Modify: `web/src/components/runner-list.tsx` (import `DetailTab`)
- Test: `web/src/components/runner-panel.test.tsx`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `StateText`, `ErrorLine` (Tasks 3, 4), `StepList` (Task 8), `StopRunnerDialog` and `LogView` (Task 10), `renderRoutes` (Task 9).
- Produces: `RunnerPanel`, `DetailTab` (Contracts, Runners).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

Create `web/src/components/runner-panel.test.tsx`:

```tsx
import { useParams, useSearch } from "@tanstack/react-router"
import { fireEvent, screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderRoutes } from "@/test/routes"
import { RunnerPanel, type DetailTab } from "./runner-panel"

function PanelAt() {
  const { id } = useParams({ strict: false })
  const { tab } = useSearch({ strict: false }) as { tab?: DetailTab }
  return <RunnerPanel key={id} id={id ?? ""} tab={tab ?? "steps"} backLink />
}

const routes = (over: Record<string, unknown> = {}) =>
  authedRoutes({
    "GET /api/runners/aaaaaa/steps": fixtures.steps,
    "GET /api/runners/aaaaaa/containers": fixtures.containers,
    "GET /api/runners/aaaaaa/log": fixtures.log,
    ...over,
  })

const draw = (path = "/runners/aaaaaa?tab=steps") =>
  renderRoutes({ "/runners/$id": () => <PanelAt />, "/runners": () => <p>runner list</p> }, path)

afterEach(() => {
  Reflect.deleteProperty(HTMLElement.prototype, "scrollHeight")
  Reflect.deleteProperty(HTMLElement.prototype, "clientHeight")
  Reflect.deleteProperty(HTMLElement.prototype, "scrollTop")
  Reflect.deleteProperty(navigator, "clipboard")
  Reflect.deleteProperty(window, "isSecureContext")
})

describe("RunnerPanel", () => {
  it("shows the runner's facts, its steps and a link to its run", async () => {
    mockApi(routes())
    draw()
    expect(await screen.findByRole("heading", { name: "aaaaaa", level: 2 })).toBeInTheDocument()
    expect(await screen.findByText("Run tests")).toBeInTheDocument()
    expect(screen.getByText("Busy")).toHaveClass("text-primary")
    expect(screen.getByText("darkmem")).toBeInTheDocument()
    expect(screen.getByText("Running for")).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Open run" })).toHaveAttribute("href", "https://github.com/darkraise/darkmem/actions/runs/102/job/2")
    expect(screen.getByRole("link", { name: "Back to Runners" })).toHaveAttribute("href", "/runners")
    expect(screen.getByRole("img", { name: "Succeeded" })).toBeInTheDocument()
    expect(screen.getByRole("img", { name: "Pending" })).toBeInTheDocument()
  })

  it("follows the log on the Log tab and pauses when scrolled up", async () => {
    let top = 0
    Object.defineProperty(HTMLElement.prototype, "scrollHeight", { configurable: true, get: () => 500 })
    Object.defineProperty(HTMLElement.prototype, "clientHeight", { configurable: true, get: () => 100 })
    Object.defineProperty(HTMLElement.prototype, "scrollTop", {
      configurable: true,
      get: () => top,
      set: (v: number) => {
        top = v
      },
    })
    mockApi(routes())
    draw("/runners/aaaaaa?tab=log")
    const log = await screen.findByText(/Listening for Jobs/)
    expect(screen.getByRole("switch", { name: "Follow" })).toHaveAttribute("aria-checked", "true")
    top = 0
    fireEvent.scroll(log)
    await waitFor(() => expect(screen.getByRole("switch", { name: "Follow" })).toHaveAttribute("aria-checked", "false"))
  })

  it("lists the containers with their project", async () => {
    mockApi(routes())
    draw("/runners/aaaaaa?tab=containers")
    expect(await screen.findByText("ghr-aaaaaa-db-1")).toBeInTheDocument()
    expect(screen.getByText("ghr-aaaaaa")).toHaveClass("text-muted-foreground")
    expect(screen.getByText("Running")).toBeInTheDocument()
  })

  it("puts the chosen tab in the URL", async () => {
    mockApi(routes())
    const { router, user } = draw()
    await user.click(await screen.findByRole("tab", { name: "Log" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: "log" }))
  })

  it("stops the runner only after confirmation", async () => {
    const { calls } = mockApi(routes({ "DELETE /api/runners/aaaaaa": () => noContent() }))
    const { user } = draw()
    await screen.findByText("Busy")
    await user.click(screen.getByRole("button", { name: "Stop runner" }))
    expect(await screen.findByText("Runner aaaaaa is running a job. Stop it?")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)
    await user.click(screen.getByRole("button", { name: "Stop runner" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Stop" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/runners/aaaaaa")).toBe(true))
  })

  it("keeps the last snapshot after the runner ends", async () => {
    let gone = false
    mockApi(
      routes({
        "GET /api/status": () =>
          gone ? { ...fixtures.status, instances: fixtures.status.instances.filter((i) => i.id !== "aaaaaa") } : fixtures.status,
      }),
    )
    draw()
    expect(await screen.findByText("Run tests")).toBeInTheDocument()
    gone = true
    expect(await screen.findByText("This runner has finished")).toBeInTheDocument()
    expect(screen.getByText("Finished")).toBeInTheDocument()
    expect(screen.getByText("Ran for")).toBeInTheDocument()
    expect(screen.getByText("Run tests")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Stop runner" })).toBeDisabled()
  })

  it("says a runner never seen live keeps no steps", async () => {
    mockApi(routes({ "GET /api/runners/zzzzzz/log": fixtures.log }))
    const { user } = draw("/runners/zzzzzz?tab=steps")
    expect(await screen.findByText("Steps are not kept after a runner finishes.")).toBeInTheDocument()
    expect(screen.getByText("Finished")).toBeInTheDocument()
    await user.click(screen.getByRole("tab", { name: "Log" }))
    expect(await screen.findByText(/Listening for Jobs/)).toBeInTheDocument()
  })

  it("stays up while the daemon cannot be reached", async () => {
    mockApi(routes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    draw()
    expect(await screen.findByRole("heading", { name: "aaaaaa" })).toBeInTheDocument()
    expect(screen.queryByText("This runner has finished")).toBeNull()
    expect(screen.queryByText("Finished")).toBeNull()
    expect(screen.queryByText("Repository")).toBeNull()
    expect(screen.getByRole("button", { name: "Stop runner" })).toBeDisabled()
  })

  it("disables Stop when the daemon stops answering for a live runner", { timeout: 10_000 }, async () => {
    let down = false
    mockApi(routes({ "GET /api/status": () => (down ? json({ error: "connection refused" }, 502) : fixtures.status) }))
    draw()
    await screen.findByText("Busy")
    expect(screen.getByRole("button", { name: "Stop runner" })).toBeEnabled()
    down = true
    await waitFor(() => expect(screen.getByRole("button", { name: "Stop runner" })).toBeDisabled())
    expect(screen.getByText("Busy")).toBeInTheDocument()
  })

  it("says when no step is reported yet", async () => {
    mockApi(routes({ "GET /api/runners/aaaaaa/steps": [] }))
    draw()
    expect(await screen.findByText("No steps reported yet.")).toBeInTheDocument()
  })

  it("copies the run URL and the runner ID from its menu", async () => {
    mockApi(routes())
    const { user } = draw()
    Object.defineProperty(window, "isSecureContext", { value: true, configurable: true })
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    await user.click(await screen.findByRole("button", { name: "More actions for runner aaaaaa" }))
    await user.click(await screen.findByRole("menuitem", { name: "Copy run URL" }))
    expect(writeText).toHaveBeenCalledWith("https://github.com/darkraise/darkmem/actions/runs/102/job/2")
    await user.click(screen.getByRole("button", { name: "More actions for runner aaaaaa" }))
    await user.click(await screen.findByRole("menuitem", { name: "Copy runner ID" }))
    expect(writeText).toHaveBeenCalledWith("aaaaaa")
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run (in `web/`): `npx vitest run src/components/runner-panel.test.tsx`
Expected: FAIL: `Cannot find module './runner-panel'`.

- [ ] **Step 3: Write the implementation**

Create `web/src/components/runner-panel.tsx`:

```tsx
import type { UseQueryResult } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "darkraise-ui/components/dropdown-menu"
import { Label } from "darkraise-ui/components/label"
import { Spinner } from "darkraise-ui/components/spinner"
import { Switch } from "darkraise-ui/components/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "darkraise-ui/components/tabs"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { ChevronLeft, Ellipsis, ExternalLink } from "lucide-react"
import { useEffect, useState } from "react"
import { useContainers, useLogTail, useStatus, useSteps } from "@/api/hooks"
import type { Container, InstanceStatus, Step } from "@/api/types"
import { LogView } from "@/components/log-view"
import { ErrorLine } from "@/components/page/error-line"
import { StateText } from "@/components/page/state-text"
import { StepList } from "@/components/step-list"
import { StopRunnerDialog } from "@/components/stop-runner-dialog"
import { copyWithToast } from "@/lib/clipboard"
import { dateTimeSec, dur, startedAt } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

export type DetailTab = "steps" | "log" | "containers"

function Steps({ steps, live, finished, now }: { steps: UseQueryResult<Step[]>; live: boolean; finished: boolean; now: number }) {
  if (steps.isError) return <ErrorLine>{errorText(steps.error)}</ErrorLine>
  if (!steps.data) {
    // Steps are polled only while the runner is live.
    if (finished) return <p className="text-sm text-muted-foreground">Steps are not kept after a runner finishes.</p>
    return <Spinner label="Loading" />
  }
  if (steps.data.length === 0) return <p className="text-sm text-muted-foreground">No steps reported yet.</p>
  return <StepList steps={steps.data} now={now} live={live} />
}

function Containers({ containers }: { containers: UseQueryResult<Container[]> }) {
  if (containers.isError) return <ErrorLine>{errorText(containers.error)}</ErrorLine>
  if (!containers.data || containers.data.length === 0) {
    return <p className="text-sm text-muted-foreground">No containers in this runner's compose projects.</p>
  }
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>State</TableHead>
          <TableHead>Name</TableHead>
          <TableHead>Image</TableHead>
          <TableHead>Project</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {containers.data.map((c) => (
          <TableRow key={c.id}>
            <TableCell>
              <StateText state={c.state} />
            </TableCell>
            <TableCell className="font-mono">{c.name}</TableCell>
            <TableCell className="font-mono">{c.image}</TableCell>
            <TableCell className="text-muted-foreground">{c.project}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function MoreMenu({ id, runUrl }: { id: string; runUrl: string | undefined }) {
  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button size="icon" variant="ghost" aria-label={`More actions for runner ${id}`}>
              <Ellipsis size={15} aria-hidden="true" />
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent>More actions</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end">
        {runUrl && <DropdownMenuItem onSelect={() => void copyWithToast(runUrl, "run URL")}>Copy run URL</DropdownMenuItem>}
        <DropdownMenuItem onSelect={() => void copyWithToast(id, id)}>Copy runner ID</DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

// The panel outlives the runner: it keeps the last instance it saw and stops
// polling steps and containers once the runner leaves /status. Callers key it
// by ID, so another runner starts clean.
export function RunnerPanel({ id, tab, backLink }: { id: string; tab: DetailTab; backLink: boolean }) {
  const navigate = useNavigate()
  const status = useStatus()
  const now = useNow()
  const [kept, setKept] = useState<InstanceStatus | undefined>(undefined)
  const [endedAt, setEndedAt] = useState<number | undefined>(undefined)
  const [follow, setFollow] = useState(true)
  const [stopping, setStopping] = useState(false)

  const current = status.data?.instances.find((i) => i.id === id)
  const live = current !== undefined
  const finished = status.data !== undefined && !live

  useEffect(() => {
    if (current) {
      if (kept !== current) setKept(current)
      setEndedAt(undefined)
    } else if (status.data && endedAt === undefined) {
      setEndedAt(now)
    }
  }, [current, kept, status.data, endedAt, now])

  const steps = useSteps(id, live)
  const containers = useContainers(id, live)
  const log = useLogTail(id, tab === "log")

  const inst = current ?? kept
  const job = inst?.job
  const runUrl = job?.html_url
  const start = inst ? startedAt(inst) : undefined
  const end = finished ? (endedAt ?? now) : now

  return (
    <div className="flex min-w-0 flex-col gap-4">
      <div className="flex flex-col gap-3 rounded-[10px] border border-border bg-card p-4">
        {backLink && (
          <Link to="/runners" aria-label="Back to Runners" className="inline-flex items-center gap-1 self-start text-sm text-primary hover:underline">
            <ChevronLeft size={15} aria-hidden="true" />
            Runners
          </Link>
        )}
        <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
          <h2 className="font-mono text-lg font-semibold">{id}</h2>
          {finished ? <StateText state="finished" /> : inst && <StateText state={inst.state} />}
          <div className="ml-auto flex flex-wrap items-center gap-2">
            {runUrl && (
              <Button variant="outline" asChild>
                <a href={runUrl} target="_blank" rel="noreferrer">
                  <ExternalLink size={15} aria-hidden="true" />
                  Open run
                </a>
              </Button>
            )}
            <Button variant="destructive" disabled={!live || status.isError} onClick={() => setStopping(true)}>
              Stop runner
            </Button>
            <MoreMenu id={id} runUrl={runUrl} />
          </div>
        </div>
        {finished && <p className="text-sm text-muted-foreground">This runner has finished</p>}
        {inst && (
          <dl className="grid grid-cols-[max-content_minmax(0,1fr)] gap-x-4 gap-y-1 text-sm sm:grid-cols-[max-content_minmax(0,1fr)_max-content_minmax(0,1fr)]">
            <dt className="text-muted-foreground">Repository</dt>
            <dd>{inst.repo}</dd>
            {job && (
              <>
                <dt className="text-muted-foreground">Job</dt>
                <dd>
                  {job.name} <span className="font-mono">#{job.run_number}</span>
                </dd>
              </>
            )}
            {start && (
              <>
                <dt className="text-muted-foreground">{finished ? "Ran for" : "Running for"}</dt>
                <dd className="font-mono">{dur(end - Date.parse(start))}</dd>
                <dt className="text-muted-foreground">Started</dt>
                <dd className="font-mono">{dateTimeSec(start)}</dd>
              </>
            )}
          </dl>
        )}
      </div>
      <Tabs value={tab} onValueChange={(t) => void navigate({ to: "/runners/$id", params: { id }, search: { tab: t as DetailTab } })}>
        <TabsList>
          <TabsTrigger value="steps">Steps</TabsTrigger>
          <TabsTrigger value="log">Log</TabsTrigger>
          <TabsTrigger value="containers">Containers</TabsTrigger>
        </TabsList>
        <TabsContent value="steps">
          <Steps steps={steps} live={live} finished={finished} now={now} />
        </TabsContent>
        <TabsContent value="log">
          <div className="mb-2 flex items-center gap-2">
            <Switch id="follow" checked={follow} onCheckedChange={setFollow} />
            <Label htmlFor="follow">Follow</Label>
          </div>
          {log.isError && <ErrorLine>{errorText(log.error)}</ErrorLine>}
          <LogView text={log.data?.text ?? ""} follow={follow} onFollowChange={setFollow} className="h-[calc(100vh-24rem)] min-h-64" />
        </TabsContent>
        <TabsContent value="containers">
          <Containers containers={containers} />
        </TabsContent>
      </Tabs>
      <StopRunnerDialog instance={stopping && inst ? inst : null} onClose={() => setStopping(false)} />
    </div>
  )
}
```

In `web/src/components/runner-list.tsx`, delete the local `type DetailTab = …` line and add `import type { DetailTab } from "@/components/runner-panel"`.

In `web/src/anti-slop.test.ts`, add `"src/components/runner-panel.tsx"` to the components group.

- [ ] **Step 4: Run the test to verify it passes**

Run (in `web/`): `npx vitest run src/components/runner-panel.test.tsx src/components/runner-list.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): build the runner panel"
```

---

### Task 12: Join the runner page to the Runners page

**Files:**
- Modify: `web/src/pages/runners.tsx` (rewrite)
- Modify: `web/src/router.tsx` (both runner routes)
- Test: `web/src/pages/runners.test.tsx` (rewrite)
- Delete: `web/src/pages/runner-detail.test.tsx`

**Interfaces:**
- Consumes: `RunnerList` (Task 9), `RunnerPanel`, `DetailTab` (Task 11), `SplitView`, `useMediaQuery`, `WIDE`, `setViewport` (Task 5), `runnersSummary` (Task 9).
- Produces: `RunnersPage`, `RunnerPage` and the routes `/runners`, `/runners/$id` (Contracts, Runners).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

Replace `web/src/pages/runners.test.tsx` with:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { setViewport } from "@/test/media"
import { renderApp } from "@/test/render"

const routes = (over: Record<string, unknown> = {}) =>
  authedRoutes({
    "GET /api/runners/aaaaaa/steps": fixtures.steps,
    "GET /api/runners/aaaaaa/containers": fixtures.containers,
    "GET /api/runners/aaaaaa/log": fixtures.log,
    "GET /api/runners/bbbbbb/steps": [],
    "GET /api/runners/bbbbbb/containers": [],
    "GET /api/runners/bbbbbb/log": { data: "bbbbbb says hi\n", next: "x" },
    ...over,
  })

const runnerLink = (id: string) => screen.findByRole("link", { name: new RegExp(`^${id}`) })

describe("Runners page", () => {
  it("sums up the runners in its header", async () => {
    mockApi(routes())
    renderApp("/runners")
    expect(await screen.findByText("2 live, 3 jobs waiting")).toBeInTheDocument()
  })

  it("shows only the list below 1024px", async () => {
    mockApi(routes())
    const { router } = renderApp("/runners")
    expect(await runnerLink("aaaaaa")).toBeInTheDocument()
    expect(screen.queryByRole("tab", { name: "Steps" })).toBeNull()
    expect(router.state.location.pathname).toBe("/runners")
  })

  it("shows only the panel with a way back below 1024px", async () => {
    mockApi(routes())
    renderApp("/runners/aaaaaa")
    expect(await screen.findByRole("heading", { name: "aaaaaa", level: 2 })).toBeInTheDocument()
    expect(screen.getByRole("link", { name: "Back to Runners" })).toBeInTheDocument()
    expect(screen.queryByRole("list", { name: "Runners" })).toBeNull()
  })

  it("selects the first runner in the URL and shows both halves at 1024px and wider", async () => {
    setViewport(1280)
    mockApi(routes())
    const { router } = renderApp("/runners")
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/aaaaaa"))
    expect(await screen.findByRole("heading", { name: "aaaaaa", level: 2 })).toBeInTheDocument()
    expect(await runnerLink("aaaaaa")).toHaveAttribute("aria-current", "page")
    expect(screen.queryByRole("link", { name: "Back to Runners" })).toBeNull()
  })

  it("keeps the selection when the selected runner finishes", { timeout: 10_000 }, async () => {
    setViewport(1280)
    let gone = false
    mockApi(
      routes({
        "GET /api/status": () => ({
          ...fixtures.status,
          instances: fixtures.status.instances.filter((i) => !(gone && i.id === "aaaaaa")),
        }),
      }),
    )
    const { router } = renderApp("/runners")
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/aaaaaa"))
    gone = true
    expect(await screen.findByText("This runner has finished")).toBeInTheDocument()
    expect(router.state.location.pathname).toBe("/runners/aaaaaa")
  })

  it("skips a cleaning runner when it picks the first one", async () => {
    setViewport(1280)
    const instances = fixtures.status.instances.map((i) => (i.id === "aaaaaa" ? { ...i, state: "cleaning" } : i))
    mockApi(routes({ "GET /api/status": { ...fixtures.status, instances } }))
    const { router } = renderApp("/runners")
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/bbbbbb"))
  })

  it("moves the panel with the arrow keys", async () => {
    setViewport(1280)
    mockApi(routes())
    const { router, user } = renderApp("/runners/aaaaaa")
    const first = await runnerLink("aaaaaa")
    first.focus()
    await user.keyboard("{ArrowDown}")
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/bbbbbb"))
    expect(await screen.findByRole("heading", { name: "bbbbbb", level: 2 })).toBeInTheDocument()
  })

  it("opens the log tab from a Dashboard link", async () => {
    mockApi(routes())
    renderApp("/runners/aaaaaa?tab=log")
    expect(await screen.findByText(/Listening for Jobs/)).toBeInTheDocument()
  })

  it("asks plainly before stopping an idle runner", async () => {
    mockApi(routes())
    const { user } = renderApp("/runners/bbbbbb")
    await user.click(await screen.findByRole("button", { name: "Stop runner" }))
    expect(await screen.findByText("Stop runner bbbbbb?")).toBeInTheDocument()
  })

  it("follows a runner that picks up a job while the dialog is open", { timeout: 10_000 }, async () => {
    let busy = false
    mockApi(
      routes({
        "GET /api/status": () => ({
          ...fixtures.status,
          instances: fixtures.status.instances.map((i) => (busy && i.id === "bbbbbb" ? { ...i, state: "busy" } : i)),
        }),
      }),
    )
    const { user } = renderApp("/runners/bbbbbb")
    await user.click(await screen.findByRole("button", { name: "Stop runner" }))
    expect(await screen.findByText("Stop runner bbbbbb?")).toBeInTheDocument()
    busy = true
    expect(await screen.findByText("Runner bbbbbb is running a job. Stop it?")).toBeInTheDocument()
  })

  it("says a runner that left while the dialog is open has finished", { timeout: 10_000 }, async () => {
    let gone = false
    const { calls } = mockApi(
      routes({
        "GET /api/status": () => ({
          ...fixtures.status,
          instances: fixtures.status.instances.filter((i) => !(gone && i.id === "bbbbbb")),
        }),
      }),
    )
    const { user } = renderApp("/runners/bbbbbb")
    await user.click(await screen.findByRole("button", { name: "Stop runner" }))
    expect(await screen.findByText("Stop runner bbbbbb?")).toBeInTheDocument()
    gone = true
    const ask = within(screen.getByRole("alertdialog"))
    expect(await ask.findByText("Runner bbbbbb has already finished")).toBeInTheDocument()
    expect(ask.getByRole("button", { name: "Stop" })).toBeDisabled()
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)
  })

  it("treats a runner the daemon no longer knows as finished", async () => {
    mockApi(routes({ "DELETE /api/runners/aaaaaa": () => json({ error: "runner aaaaaa not found" }, 404) }))
    const { user } = renderApp("/runners/aaaaaa")
    await user.click(await screen.findByRole("button", { name: "Stop runner" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Stop" }))
    expect((await screen.findAllByText("aaaaaa had already finished")).length).toBeGreaterThan(0)
    expect(screen.queryByText("runner aaaaaa not found")).toBeNull()
  })

  it("keeps the runner's name in the dialog while it closes", async () => {
    mockApi(routes())
    const { user } = renderApp("/runners/bbbbbb")
    await user.click(await screen.findByRole("button", { name: "Stop runner" }))
    await screen.findByText("Stop runner bbbbbb?")
    await user.click(screen.getByRole("button", { name: "Cancel" }))
    expect(screen.queryByText(/undefined/)).toBeNull()
  })

  it("drops the snapshot when the page moves to another runner", async () => {
    let gone = false
    mockApi(
      routes({
        "GET /api/status": () =>
          gone ? { ...fixtures.status, instances: fixtures.status.instances.filter((i) => i.id !== "aaaaaa") } : fixtures.status,
      }),
    )
    const { router } = renderApp("/runners/aaaaaa")
    expect(await screen.findByText("Running for")).toBeInTheDocument()
    gone = true
    expect(await screen.findByText("This runner has finished")).toBeInTheDocument()
    await router.navigate({ to: "/runners/$id", params: { id: "zzzzzz" }, search: { tab: "steps" } })
    expect(await screen.findByRole("heading", { name: "zzzzzz" })).toBeInTheDocument()
    expect(screen.queryByText("Ran for")).toBeNull()
  })

  it("waits for the daemon while it cannot be reached", async () => {
    mockApi(routes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    renderApp("/runners")
    expect(await screen.findByText("Waiting for the daemon")).toBeInTheDocument()
  })
})
```

Delete `web/src/pages/runner-detail.test.tsx` (its cases now live in `runner-panel.test.tsx` and above).

- [ ] **Step 2: Run the test to verify it fails**

Run (in `web/`): `npx vitest run src/pages/runners.test.tsx`
Expected: FAIL: the old page has no summary, no split view, and no "Stop runner" button for idle runners at `/runners/bbbbbb`.

- [ ] **Step 3: Write the implementation**

Replace `web/src/pages/runners.tsx` with:

```tsx
import { useNavigate, useParams, useSearch } from "@tanstack/react-router"
import { PageHeader } from "darkraise-ui/layout"
import { useEffect } from "react"
import { useStatus } from "@/api/hooks"
import { SplitView } from "@/components/page/split-view"
import { RunnerList } from "@/components/runner-list"
import { RunnerPanel, type DetailTab } from "@/components/runner-panel"
import { runnersSummary } from "@/lib/summary"
import { useMediaQuery, WIDE } from "@/lib/use-media-query"
import { useNow } from "@/lib/use-now"

function RunnersView({ id, tab }: { id: string | undefined; tab: DetailTab }) {
  const status = useStatus()
  const now = useNow()
  const wide = useMediaQuery(WIDE)
  const navigate = useNavigate()
  const first = status.data?.instances.find((i) => i.state !== "cleaning")?.id

  // On wide screens the first live runner goes into the URL, so the selection
  // stays put when that runner finishes. Narrow screens keep the list.
  useEffect(() => {
    if (wide && id === undefined && first !== undefined) {
      void navigate({ to: "/runners/$id", params: { id: first }, search: { tab: "steps" }, replace: true })
    }
  }, [wide, id, first, navigate])

  const list = <RunnerList status={status.data} selected={id} tab={tab} now={now} />
  const panel = id === undefined ? null : <RunnerPanel key={id} id={id} tab={tab} backLink={!wide} />
  return (
    <div className="flex flex-col gap-4">
      <PageHeader title="Runners" description={status.data ? runnersSummary(status.data) : undefined} />
      <SplitView list={list} panel={panel} narrow={panel ?? list} />
    </div>
  )
}

export function RunnersPage() {
  return <RunnersView id={undefined} tab="steps" />
}

export function RunnerPage() {
  const { id } = useParams({ from: "/app/runners/$id" })
  const { tab } = useSearch({ from: "/app/runners/$id" })
  return <RunnersView id={id} tab={tab} />
}
```

In `web/src/router.tsx`, replace the two imports

```tsx
import { RunnerDetailPage, type DetailTab } from "./pages/runner-detail"
import { RunnersPage } from "./pages/runners"
```

with

```tsx
import type { DetailTab } from "./components/runner-panel"
import { RunnerPage, RunnersPage } from "./pages/runners"
```

and change `component: RunnerDetailPage,` in `runnerRoute` to `component: RunnerPage,`.

In `web/src/anti-slop.test.ts`, add `"src/pages/runners.tsx"` to the pages group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/pages/runners.test.tsx src/router.test.tsx src/components/shell.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add -A web/src
git commit -m "feat(web): join the runner page to Runners"
```

---

### Task 13: Retire the old runner page

**Files:**
- Delete: `web/src/pages/runner-detail.tsx`, `web/src/components/runners-table.tsx`, `web/src/components/glyph.tsx`, `web/src/components/glyph.test.tsx`
- Modify: `web/src/a11y.test.tsx`

**Interfaces:**
- Consumes: the Runners page at both routes (Task 12), `setViewport` (Task 5).
- Produces: nothing new.

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing test**

In `web/src/a11y.test.tsx`, add `import { fixtures } from "@/test/fixtures"` (merge with the existing `authedRoutes` import) and `import { setViewport } from "@/test/media"`, move the axe call into a helper above the first `describe`:

```tsx
async function violations(): Promise<string[]> {
  const main = document.getElementById("main-content")
  if (!main) throw new Error("no main content")
  // jsdom computes no layout or colour, so contrast is checked by the
  // palette test in src/styles/theme.test.ts instead.
  const results = await axe.run(main, { rules: { "color-contrast": { enabled: false } } })
  return results.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)
}
```

make the Dashboard test end with `expect(await violations()).toEqual([])`, and append:

```tsx
describe("Runners accessibility", () => {
  it.each([
    [1280, "the split view"],
    [390, "the narrow panel"],
  ])(
    "has no axe violations at %ipx (%s)",
    async (width) => {
      setViewport(width)
      mockApi(
        authedRoutes({
          "GET /api/runners/aaaaaa/steps": fixtures.steps,
          "GET /api/runners/aaaaaa/containers": fixtures.containers,
        }),
      )
      renderApp("/runners/aaaaaa")
      await screen.findByRole("heading", { name: "aaaaaa", level: 2 })
      await screen.findByText("Run tests")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})
```

Then delete the four files listed under Files.

- [ ] **Step 2: Run the test to verify it fails or exposes a gap**

Run (in `web/`): `npx vitest run src/a11y.test.tsx && npm run typecheck`
Expected: the Runners cases PASS if Tasks 9 to 12 are accessible; any axe violation printed here is a defect in those tasks' markup and is fixed in this step before going on (for example a missing name). `typecheck` passes because nothing imports the deleted files: confirm with `grep -rn "runner-detail\|runners-table\|components/glyph" web/src` printing nothing.

- [ ] **Step 3: Write the implementation**

No new code: the deletions are the change. If Step 2 printed an axe violation, fix the named element in its component (Tasks 9 to 12) and keep the fix in this commit.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add -A web/src
git commit -m "refactor(web): retire the old runner page"
```

---

### Task 14: Add the History helpers

**Files:**
- Create: `web/src/lib/history.ts`
- Test: `web/src/lib/history.test.ts`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: everything under Contracts, History.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/history.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import type { HistoryEntry } from "@/api/types"
import { dayLabel, groupByDay, historySummary, inBucket, median, parseHistorySearch, pickText, resultWord } from "./history"

const now = Date.parse("2026-10-03T14:05:00Z")
const entry = (id: string, start: string, end: string, conclusion = "success"): HistoryEntry => ({
  id,
  repo: "darkmem",
  run_id: 1,
  run_number: "1",
  workflow: "ci",
  job_name: "build",
  conclusion,
  started_at: start,
  finished_at: end,
})

describe("parseHistorySearch", () => {
  it("keeps the values it knows and drops the rest", () => {
    expect(parseHistorySearch({ repo: "darkmem", result: "failure", window: "30d" })).toEqual({ repo: "darkmem", result: "failure", window: "30d" })
    expect(parseHistorySearch({ repo: "", result: "bogus", window: "2h" })).toEqual({})
    expect(parseHistorySearch({ repo: 4 })).toEqual({})
  })
})

describe("dayLabel and groupByDay", () => {
  it("names today, yesterday, then the weekday and date", () => {
    expect(dayLabel("2026-10-03T00:10:00Z", now)).toBe("Today")
    expect(dayLabel("2026-10-02T23:50:00Z", now)).toBe("Yesterday")
    expect(dayLabel("2026-09-28T10:00:00Z", now)).toBe("Mon, Sep 28")
  })

  it("groups rows that are already newest first, keeping their order", () => {
    const rows = [
      entry("a", "2026-10-03T13:00:00Z", "2026-10-03T13:40:00Z"),
      entry("b", "2026-10-03T09:00:00Z", "2026-10-03T09:05:00Z"),
      entry("c", "2026-10-02T09:00:00Z", "2026-10-02T09:05:00Z"),
    ]
    const groups = groupByDay(rows, now)
    expect(groups.map((g) => g.label)).toEqual(["Today", "Yesterday"])
    expect(groups[0]?.rows.map((r) => r.id)).toEqual(["a", "b"])
  })
})

describe("median and historySummary", () => {
  it("takes the middle value, or the mean of the two middle ones", () => {
    expect(median([1, 3, 2])).toBe(2)
    expect(median([4, 1, 3, 2])).toBe(2.5)
    expect(median([])).toBeUndefined()
  })

  it("counts jobs and failures and gives the median duration", () => {
    const rows = [
      entry("a", "2026-10-03T13:35:00Z", "2026-10-03T13:40:00Z"),
      entry("b", "2026-10-03T11:05:00Z", "2026-10-03T11:06:30Z", "failure"),
    ]
    expect(historySummary(rows)).toBe("2 jobs, 1 failed, median 3m15s")
    expect(historySummary([rows[0] as HistoryEntry])).toBe("1 job, median 5m00s")
    expect(historySummary([])).toBe("No jobs")
  })
})

describe("inBucket and pickText", () => {
  it("keeps rows that finished inside the bucket", () => {
    const rows = [
      entry("in", "2026-10-03T13:00:00Z", "2026-10-03T13:40:00Z"),
      entry("edge", "2026-10-03T13:50:00Z", "2026-10-03T14:00:00Z"),
      entry("out", "2026-10-03T11:00:00Z", "2026-10-03T11:06:30Z"),
    ]
    expect(inBucket(rows, { start: "2026-10-03T13:00:00Z", end: "2026-10-03T14:00:00Z" }).map((r) => r.id)).toEqual(["in"])
  })

  it("describes the picked span", () => {
    expect(pickText({ start: "2026-10-03T13:00:00Z", end: "2026-10-03T14:00:00Z" })).toBe("Showing 13:00 to 14:00, Oct 3")
    expect(pickText({ start: "2026-10-03T00:00:00Z", end: "2026-10-04T00:00:00Z" })).toBe("Showing Oct 3")
  })
})

describe("resultWord", () => {
  it.each([
    ["success", "Succeeded"],
    ["cancelled", "Cancelled"],
    ["skipped", "Skipped"],
    ["failure", "Failed"],
    ["timed_out", "Failed"],
  ])("reads %s as %s", (conclusion, word) => {
    expect(resultWord(conclusion)).toBe(word)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run (in `web/`): `npx vitest run src/lib/history.test.ts`
Expected: FAIL: `Cannot find module './history'`.

- [ ] **Step 3: Write the implementation**

Create `web/src/lib/history.ts`:

```ts
import type { HistoryEntry } from "@/api/types"
import { dur, hhmm, monthDay, plural } from "@/lib/format"

export const HISTORY_WINDOWS = ["24h", "7d", "30d"] as const
export type HistoryWindow = (typeof HISTORY_WINDOWS)[number]

export const RESULTS = [
  { value: "success", label: "Succeeded" },
  { value: "failure", label: "Failed" },
  { value: "cancelled", label: "Cancelled" },
] as const
export type HistoryResult = (typeof RESULTS)[number]["value"]

export interface HistorySearch {
  repo?: string
  result?: HistoryResult
  window?: HistoryWindow
}

// Unknown values are dropped, so the page falls back to its defaults.
export function parseHistorySearch(search: Record<string, unknown>): HistorySearch {
  const out: HistorySearch = {}
  if (typeof search.repo === "string" && search.repo !== "") out.repo = search.repo
  const result = RESULTS.find((r) => r.value === search.result)
  if (result) out.result = result.value
  const window = HISTORY_WINDOWS.find((w) => w === search.window)
  if (window) out.window = window
  return out
}

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
const dayKey = (d: Date) => `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`

export function dayLabel(iso: string, now: number): string {
  const d = new Date(iso)
  const today = new Date(now)
  const yesterday = new Date(today.getFullYear(), today.getMonth(), today.getDate() - 1)
  if (dayKey(d) === dayKey(today)) return "Today"
  if (dayKey(d) === dayKey(yesterday)) return "Yesterday"
  return `${WEEKDAYS[d.getDay()] ?? ""}, ${monthDay(d)}`
}

export interface DayGroup {
  key: string
  label: string
  rows: HistoryEntry[]
}

export function groupByDay(rows: HistoryEntry[], now: number): DayGroup[] {
  const out: DayGroup[] = []
  for (const r of rows) {
    const key = dayKey(new Date(r.finished_at))
    const last = out.at(-1)
    if (last?.key === key) last.rows.push(r)
    else out.push({ key, label: dayLabel(r.finished_at, now), rows: [r] })
  }
  return out
}

export function took(entry: HistoryEntry): number {
  return Date.parse(entry.finished_at) - Date.parse(entry.started_at)
}

export function median(values: number[]): number | undefined {
  if (values.length === 0) return undefined
  const sorted = [...values].sort((a, b) => a - b)
  const mid = Math.floor(sorted.length / 2)
  if (sorted.length % 2 === 1) return sorted[mid]
  return ((sorted[mid - 1] ?? 0) + (sorted[mid] ?? 0)) / 2
}

// As the result icons read them: anything not a success, a cancellation or a
// skip is a failure.
export function resultWord(conclusion: string): string {
  if (conclusion === "success") return "Succeeded"
  if (conclusion === "cancelled") return "Cancelled"
  if (conclusion === "skipped") return "Skipped"
  return "Failed"
}

export function historySummary(rows: HistoryEntry[]): string {
  if (rows.length === 0) return "No jobs"
  const failed = rows.filter((r) => resultWord(r.conclusion) === "Failed").length
  const mid = median(rows.map(took)) ?? 0
  return `${plural(rows.length, "job")}${failed > 0 ? `, ${failed} failed` : ""}, median ${dur(mid)}`
}

export function inBucket(rows: HistoryEntry[], bucket: { start: string; end: string }): HistoryEntry[] {
  const start = Date.parse(bucket.start)
  const end = Date.parse(bucket.end)
  return rows.filter((r) => {
    const t = Date.parse(r.finished_at)
    return t >= start && t < end
  })
}

export function pickText(bucket: { start: string; end: string }): string {
  const start = new Date(bucket.start)
  const end = new Date(bucket.end)
  if (end.getTime() - start.getTime() >= 24 * 3_600_000) return `Showing ${monthDay(start)}`
  return `Showing ${hhmm(start)} to ${hhmm(end)}, ${monthDay(start)}`
}
```

In `web/src/anti-slop.test.ts`, add `"src/lib/history.ts"` to the lib group.

- [ ] **Step 4: Run the test to verify it passes**

Run (in `web/`): `npx vitest run src/lib/history.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): add the History helpers"
```

---

### Task 15: Rebuild the History page

**Files:**
- Modify: `web/src/pages/history.tsx` (rewrite)
- Modify: `web/src/router.tsx` (`historyRoute.validateSearch`)
- Test: `web/src/pages/history.test.tsx` (rewrite), `web/src/a11y.test.tsx`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `useActivity`, `useHistory`, `HISTORY_LIMIT` (Task 1), `Section`, `ErrorLine` (Tasks 3, 4), `BucketsChart` `tracks`/`picked`/`onPick` and `WindowControl` `options` (Task 7), the History helpers (Task 14).
- Produces: `HistoryPage` with search `HistorySearch` (Contracts, History).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

Replace `web/src/pages/history.test.tsx` with:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { HistoryEntry } from "@/api/types"
import { json, mockApi } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const secureContext = Object.getOwnPropertyDescriptor(window, "isSecureContext")

afterEach(() => {
  Reflect.deleteProperty(navigator, "clipboard")
  if (secureContext) Object.defineProperty(window, "isSecureContext", secureContext)
  else Reflect.deleteProperty(window, "isSecureContext")
})

const SINCE = "?repo=&conclusion=&since=2026-10-02T14%3A00%3A00Z&limit=500"
const historyCalls = (calls: { path: string; search: string }[]) => calls.filter((c) => c.path === "/api/history").map((c) => c.search)
const activityCalls = (calls: { path: string; search: string }[]) => calls.filter((c) => c.path === "/api/activity").map((c) => c.search)

describe("History page", () => {
  it("lists finished jobs under day headings and sums them up", async () => {
    mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    renderApp("/history")
    expect(await screen.findByText("build")).toBeInTheDocument()
    const table = within(screen.getByRole("table"))
    const today = table.getByText("Today")
    expect(today.tagName).toBe("TH")
    expect(today).toHaveAttribute("scope", "rowgroup")
    expect(table.getByText("#7")).toBeInTheDocument()
    expect(table.getAllByText("deploy")).toHaveLength(2)
    expect(table.getByText("#41")).toBeInTheDocument()
    expect(table.getByText("Succeeded")).toBeInTheDocument()
    expect(table.getByText("Failed")).toBeInTheDocument()
    expect(screen.getByText("2 jobs, 1 failed, median 3m15s")).toBeInTheDocument()
    expect(screen.getAllByRole("link", { name: /^Open run/ })).toHaveLength(1)
  })

  it("reads the table from where the chart starts", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    renderApp("/history")
    await screen.findByText("build")
    expect(activityCalls(calls)[0]).toBe("?window=7d&tz=UTC")
    expect(historyCalls(calls)[0]).toBe(SINCE)
    expect(screen.getByRole("group", { name: "Jobs per bucket for the last 24 hours" })).toBeInTheDocument()
  })

  it("filters by repository through the URL", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user, router } = renderApp("/history")
    await screen.findByText("build")
    await user.click(screen.getByRole("combobox", { name: "Repository" }))
    await user.click(await screen.findByRole("option", { name: "darkmem" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ repo: "darkmem" }))
    await waitFor(() => expect(activityCalls(calls)).toContain("?window=7d&tz=UTC&repo=darkmem"))
    await waitFor(() => expect(historyCalls(calls)).toContain("?repo=darkmem&conclusion=&since=2026-10-02T14%3A00%3A00Z&limit=500"))
  })

  it("filters by result, and a second click goes back to all", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user, router } = renderApp("/history")
    await screen.findByText("build")
    await user.click(screen.getByRole("radio", { name: "Failed" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ result: "failure" }))
    await waitFor(() => expect(historyCalls(calls)).toContain("?repo=&conclusion=failure&since=2026-10-02T14%3A00%3A00Z&limit=500"))
    await user.click(screen.getByRole("radio", { name: "Failed" }))
    await waitFor(() => expect(router.state.location.search).toEqual({}))
    expect(screen.getByRole("radio", { name: "All" })).toBeChecked()
  })

  it("changes the window", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user, router } = renderApp("/history")
    await screen.findByText("build")
    await user.click(screen.getByRole("radio", { name: "24h" }))
    await waitFor(() => expect(router.state.location.search).toEqual({ window: "24h" }))
    await waitFor(() => expect(activityCalls(calls)).toContain("?window=24h&tz=UTC"))
  })

  it("restores its controls from the URL", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": [] }))
    renderApp("/history?repo=old-repo&result=failure&window=30d")
    await waitFor(() => expect(activityCalls(calls)).toContain("?window=30d&tz=UTC&repo=old-repo"))
    expect(screen.getByRole("radio", { name: "Failed" })).toBeChecked()
    expect(screen.getByRole("radio", { name: "30d" })).toBeChecked()
    expect(screen.getByRole("combobox", { name: "Repository" })).toHaveTextContent("old-repo")
  })

  it("falls back to its defaults for values it does not know", async () => {
    const { calls } = mockApi(authedRoutes({ "GET /api/history": [] }))
    renderApp("/history?window=2h&result=bogus")
    await waitFor(() => expect(activityCalls(calls)).toContain("?window=7d&tz=UTC"))
    expect(screen.getByRole("radio", { name: "All" })).toBeChecked()
    expect(screen.getByRole("radio", { name: "7d" })).toBeChecked()
  })

  it("narrows the table to a picked bucket, by mouse or keyboard", async () => {
    mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user } = renderApp("/history")
    await screen.findByText("#7")
    const bucket = await screen.findByRole("button", { name: /^13:00 to 14:00/ })
    await user.click(bucket)
    expect(bucket).toHaveAttribute("aria-pressed", "true")
    expect(await screen.findByText("Showing 13:00 to 14:00, Oct 3")).toBeInTheDocument()
    expect(screen.getByText("#41")).toBeInTheDocument()
    expect(screen.queryByText("#7")).toBeNull()
    await user.click(screen.getByRole("button", { name: "Show whole window" }))
    expect(await screen.findByText("#7")).toBeInTheDocument()
    bucket.focus()
    await user.keyboard("{Enter}")
    expect(await screen.findByText("Showing 13:00 to 14:00, Oct 3")).toBeInTheDocument()
    await user.keyboard(" ")
    expect(await screen.findByText("#7")).toBeInTheDocument()
  })

  it("says when the 500-job cap cut the window short", async () => {
    const [build] = fixtures.history as [HistoryEntry]
    const many = Array.from({ length: 500 }, (_, i) => ({ ...build, id: `h${i}` }))
    mockApi(authedRoutes({ "GET /api/history": many }))
    renderApp("/history")
    expect(await screen.findByText("Showing the newest 500 jobs in this window.")).toBeInTheDocument()
  })

  it("says when nothing finished in the window", async () => {
    mockApi(authedRoutes({ "GET /api/history": [] }))
    renderApp("/history")
    expect(await screen.findByText("No finished jobs in the last 7 days")).toBeInTheDocument()
  })

  it("offers to clear filters that match nothing", async () => {
    mockApi(authedRoutes({ "GET /api/history": [] }))
    const { user, router } = renderApp("/history?result=failure")
    expect(await screen.findByText("No jobs match these filters")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Clear filters" }))
    await waitFor(() => expect(router.state.location.search).toEqual({}))
  })

  it("shows a failed read", async () => {
    mockApi(authedRoutes({ "GET /api/history": () => json({ error: "history file unreadable" }, 500) }))
    renderApp("/history")
    expect(within(await screen.findByRole("alert")).getByText("history file unreadable")).toBeInTheDocument()
  })

  it("shows loading until the chart's span is known", async () => {
    mockApi(authedRoutes({ "GET /api/activity": () => new Promise(() => {}) }))
    renderApp("/history")
    expect((await screen.findAllByText("Loading")).length).toBeGreaterThan(0)
    expect(screen.queryByText(/^No finished jobs/)).toBeNull()
  })

  it("copies a run's URL", async () => {
    mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    const { user } = renderApp("/history")
    Object.defineProperty(window, "isSecureContext", { value: true, configurable: true })
    const writeText = vi.fn(async () => {})
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true })
    await user.click(await screen.findByRole("button", { name: "Copy run URL of build #41" }))
    expect(writeText).toHaveBeenCalledWith("https://github.com/darkraise/darkmem/actions/runs/101/job/1")
    expect((await screen.findAllByText("copied run URL")).length).toBeGreaterThan(0)
  })
})
```

Append to `web/src/a11y.test.tsx`:

```tsx
describe("History accessibility", () => {
  it(
    "has no axe violations with a bucket picked",
    async () => {
      mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
      const { user } = renderApp("/history")
      await screen.findByText("#7")
      await user.click(await screen.findByRole("button", { name: /^13:00 to 14:00/ }))
      await screen.findByText("Showing 13:00 to 14:00, Oct 3")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run (in `web/`): `npx vitest run src/pages/history.test.tsx`
Expected: FAIL: no day headings, no chart, no URL search, the old `limit=500` call has no `since`.

- [ ] **Step 3: Write the implementation**

In `web/src/router.tsx`, add `import { parseHistorySearch } from "./lib/history"` and replace `historyRoute`:

```tsx
const historyRoute = createRoute({
  getParentRoute: () => appRoute,
  path: "/history",
  validateSearch: parseHistorySearch,
  component: HistoryPage,
})
```

Replace `web/src/pages/history.tsx` with:

```tsx
import { useNavigate, useSearch } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import { Spinner } from "darkraise-ui/components/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { ToggleGroup, ToggleGroupItem } from "darkraise-ui/components/toggle-group"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { PageHeader } from "darkraise-ui/layout"
import { Copy, ExternalLink } from "lucide-react"
import { useState } from "react"
import { HISTORY_LIMIT, useActivity, useConfig, useHistory } from "@/api/hooks"
import type { ActivityBucket, HistoryEntry } from "@/api/types"
import { BucketsChart } from "@/components/buckets-chart"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import { ResultIcon } from "@/components/result-icon"
import { WindowControl } from "@/components/window-control"
import { windowWords } from "@/lib/activity-view"
import { copyWithToast } from "@/lib/clipboard"
import { dur, hhmm } from "@/lib/format"
import {
  groupByDay,
  HISTORY_WINDOWS,
  historySummary,
  inBucket,
  pickText,
  RESULTS,
  resultWord,
  took,
  type HistoryResult,
  type HistorySearch,
  type HistoryWindow,
} from "@/lib/history"
import { useNow } from "@/lib/use-now"
import { useWidth } from "@/lib/use-width"
import { errorText } from "@/query"

const ALL = "__all__"

function Legend() {
  const items: [string, string][] = [
    ["Busy", "bg-primary"],
    ["Succeeded", "bg-success/60"],
    ["Failed", "bg-destructive/70"],
  ]
  return (
    <ul aria-label="Legend" className="ml-auto flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
      <li>All results</li>
      {items.map(([label, swatch]) => (
        <li key={label} className="flex items-center gap-1.5">
          <span aria-hidden="true" className={`size-2.5 rounded-[2px] ${swatch}`} />
          {label}
        </li>
      ))}
    </ul>
  )
}

function HistoryRow({ entry, longest }: { entry: HistoryEntry; longest: number }) {
  const ms = took(entry)
  const url = entry.html_url
  const name = `${entry.job_name} #${entry.run_number}`
  return (
    <TableRow>
      <TableCell className="font-mono">{hhmm(new Date(entry.finished_at))}</TableCell>
      <TableCell>{entry.repo}</TableCell>
      <TableCell>
        <span className="block">{entry.job_name}</span>
        <span className="block text-xs text-muted-foreground">{entry.workflow}</span>
      </TableCell>
      <TableCell className="font-mono">#{entry.run_number}</TableCell>
      <TableCell>
        <span className="flex items-center gap-1.5">
          <ResultIcon conclusion={entry.conclusion} />
          <span>{resultWord(entry.conclusion)}</span>
        </span>
      </TableCell>
      <TableCell>
        <span className="flex items-center justify-end gap-2">
          <span className="font-mono">{dur(ms)}</span>
          <span aria-hidden="true" className="h-1.5 w-20 rounded-[2px] bg-muted">
            <span className="block h-1.5 rounded-[2px] bg-primary" style={{ width: `${Math.round((ms / longest) * 100)}%` }} />
          </span>
        </span>
      </TableCell>
      <TableCell className="text-right">
        {url && (
          <span className="flex justify-end gap-1">
            <Tooltip>
              <TooltipTrigger asChild>
                <Button size="icon" variant="ghost" asChild>
                  <a href={url} target="_blank" rel="noreferrer" aria-label={`Open run ${name}`}>
                    <ExternalLink size={15} aria-hidden="true" />
                  </a>
                </Button>
              </TooltipTrigger>
              <TooltipContent>Open run</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger asChild>
                <Button size="icon" variant="ghost" aria-label={`Copy run URL of ${name}`} onClick={() => void copyWithToast(url, "run URL")}>
                  <Copy size={15} aria-hidden="true" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Copy run URL</TooltipContent>
            </Tooltip>
          </span>
        )}
      </TableCell>
    </TableRow>
  )
}

export function HistoryPage() {
  const search = useSearch({ from: "/app/history" })
  const navigate = useNavigate()
  const repo = search.repo ?? ""
  const result: HistoryResult | "" = search.result ?? ""
  const window: HistoryWindow = search.window ?? "7d"
  const config = useConfig()
  const activity = useActivity(window, repo)
  // The table covers what the chart covers: it waits for this window's
  // response and starts at its from.
  const since = activity.data && !activity.isPlaceholderData ? activity.data.from : undefined
  const history = useHistory(repo, result, since)
  const now = useNow()
  const [ref, width] = useWidth<HTMLDivElement>(960)
  const [pick, setPick] = useState<ActivityBucket | null>(null)

  function update(next: { repo?: string; result?: HistoryResult | ""; window?: HistoryWindow }) {
    setPick(null)
    const merged = { repo, result, window, ...next }
    const out: HistorySearch = {}
    if (merged.repo) out.repo = merged.repo
    if (merged.result) out.result = merged.result
    if (merged.window !== "7d") out.window = merged.window
    void navigate({ to: "/history", search: out })
  }

  const names = (config.data?.repos ?? []).map((r) => r.name)
  const repos = repo && !names.includes(repo) ? [...names, repo] : names
  const all = history.data ?? []
  const rows = pick ? inBucket(all, pick) : all
  const groups = groupByDay(rows, now)
  const longest = Math.max(1, ...rows.map(took))
  const filtered = repo !== "" || result !== ""

  let body
  if (history.isError) body = <ErrorLine>{errorText(history.error)}</ErrorLine>
  else if (!history.data) body = activity.isError ? null : <Spinner label="Loading" />
  else if (rows.length === 0) {
    body = pick ? (
      <p className="text-sm text-muted-foreground">No jobs finished in this span.</p>
    ) : filtered ? (
      <div className="flex flex-col items-start gap-2">
        <p className="text-sm text-muted-foreground">No jobs match these filters</p>
        <Button size="sm" variant="outline" onClick={() => update({ repo: "", result: "" })}>
          Clear filters
        </Button>
      </div>
    ) : (
      <p className="text-sm text-muted-foreground">{`No finished jobs in ${windowWords(window)}`}</p>
    )
  } else {
    body = (
      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Finished</TableHead>
              <TableHead>Repository</TableHead>
              <TableHead>Job</TableHead>
              <TableHead>Run</TableHead>
              <TableHead>Result</TableHead>
              <TableHead className="text-right">Duration</TableHead>
              <TableHead>
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          {groups.map((g) => (
            <TableBody key={g.key}>
              <TableRow>
                <th scope="rowgroup" colSpan={7} className="pt-4 pb-1 text-left text-sm font-medium text-muted-foreground">
                  {g.label}
                </th>
              </TableRow>
              {g.rows.map((h) => (
                <HistoryRow key={h.id} entry={h} longest={longest} />
              ))}
            </TableBody>
          ))}
        </Table>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader title="History" description={history.data ? historySummary(rows) : undefined} />
      <div className="flex flex-wrap items-center gap-3">
        <Select value={repo || ALL} onValueChange={(v) => update({ repo: v === ALL ? "" : v })}>
          <SelectTrigger aria-label="Repository" className="w-52">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>All repositories</SelectItem>
            {repos.map((name) => (
              <SelectItem key={name} value={name}>
                {name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <ToggleGroup
          type="single"
          size="sm"
          variant="outline"
          value={result || ALL}
          aria-label="Result"
          onValueChange={(v) => update({ result: RESULTS.find((r) => r.value === v)?.value ?? "" })}
        >
          <ToggleGroupItem value={ALL}>All</ToggleGroupItem>
          {RESULTS.map((r) => (
            <ToggleGroupItem key={r.value} value={r.value}>
              {r.label}
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
        <div className="ml-auto">
          <WindowControl value={window} onChange={(w) => update({ window: w })} options={HISTORY_WINDOWS} />
        </div>
      </div>
      <Section title="Jobs" aside={<Legend />}>
        {activity.isError && <ErrorLine onRetry={() => void activity.refetch()}>{errorText(activity.error)}</ErrorLine>}
        <div ref={ref} className="overflow-x-auto">
          {activity.data ? (
            <BucketsChart
              activity={activity.data}
              now={now}
              width={width}
              tracks="jobs"
              picked={pick?.start ?? null}
              onPick={(b) => setPick((p) => (p?.start === b.start ? null : b))}
            />
          ) : (
            !activity.isError && <Spinner label="Loading" />
          )}
        </div>
      </Section>
      <Section title="Finished jobs">
        {pick && (
          <div className="mb-3 flex flex-wrap items-center gap-2 text-sm">
            <span>{pickText(pick)}</span>
            <Button size="sm" variant="outline" onClick={() => setPick(null)}>
              Show whole window
            </Button>
          </div>
        )}
        {all.length >= HISTORY_LIMIT && <p className="mb-3 text-sm text-muted-foreground">Showing the newest 500 jobs in this window.</p>}
        {body}
      </Section>
    </div>
  )
}
```

The `ResultIcon` inside the Result cell and the visible word name the same outcome; the icon keeps its `role="img"` label because `ResultIcon` is shared and other places show it alone.

In `web/src/anti-slop.test.ts`, add `"src/pages/history.tsx"` to the pages group and `"src/router.tsx"` after it.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/pages/history.test.tsx src/a11y.test.tsx src/router.test.tsx src/components/shell.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): rebuild the History page"
```
