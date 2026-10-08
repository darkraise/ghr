# ghr web UI redesign, plan 2: identity, shell and Dashboard

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the ghr web UI its own look (palette, IBM Plex type, brand mark), rebuild the app shell around runner capacity, and replace the Dashboard with stat cards, an activity recorder, a repository table, a disk breakdown and an event list.

**Architecture:** ghr keeps darkraise-ui and restyles it from one stylesheet, `web/src/styles/ghr-theme.css`, which overrides the theme engine's inline tokens with `!important` and rebinds Tailwind's font and radius theme. Pure helpers in `web/src/lib/` compute everything the new components draw (capacity, summary sentence, chart geometry text, disk parts); each component takes plain props and is tested alone. The Dashboard page wires the existing hooks plus the new `useActivity` hook for `GET /api/activity` (served since plan 1).

**Tech Stack:** React 19, TypeScript 5.7, TanStack Query and Router, darkraise-ui 6.9.6, Tailwind CSS 4, lucide-react 1.52, Vitest 4 with jsdom and Testing Library, axe-core 4.13.0, `@fontsource/ibm-plex-sans` and `@fontsource/ibm-plex-mono` 5.3.0.

**Spec:** docs/superpowers/specs/2026-10-08-ghr-web-ui-identity-dashboard-design.md (§1, §2, §3, §5, §6 and the frontend parts of §7; §4 shipped in plan 1)

**Execution:** inline — `claude --model sonnet --effort high` — 1 of 25 tasks is heavy (Task 21, total 5) and is delegated; the self-implemented tasks top out at total 4 (Sonnet medium), raised to high because a task is delegated.

**Plan review:** 2026-10-08 — dr-superpowers:judge-opus — executability 15 / coherence 17 / coverage 17 / assumptions 13 (round 1)

## Global Constraints

- Work on the branch `feat/web-ui-overhaul`, created from `master`. Merge only when the owner says so: pushing `master` releases a version (the CI release job).
- Frontend only. No Go file changes. Run `npm` and `npx` commands from `web/`; run `git` commands from the repository root (every path in a commit step starts with `web/`).
- Every colour comes from a token: Tailwind classes such as `text-primary`, `bg-muted`, `fill-success/30`, or `hsl(var(--token))`. No raw hex in `.ts` or `.tsx` files. The only hex values allowed are in `web/src/styles/ghr-theme.css` comments (none needed) and the static `web/public/logo.svg` and `web/public/favicon.svg`.
- Anti-slop rules (spec §1.7), binding on every file this plan creates or touches: one accent colour; no purple; no gradients on buttons, text or surfaces; no nested cards; status dots only in the capacity bar; no pills around plain text; no "·" chains as separators; empty cells stay blank (no dash characters); no em dash (U+2014) or en dash (U+2013) anywhere in UI copy; buttons name their action; errors say what happened and what to do; monospace (`font-mono`) only for data (numbers, times, IDs, versions, code, chart axis labels such as "Lane 1" and tick times per spec §1.2, the `ghr` wordmark); no decorative SVG, background patterns or illustrations; no shadows, glow, glass or `backdrop-filter`.
- Sentence case everywhere. No uppercase letter-spaced labels.
- Icons are Lucide only. Inline icons use `size={15}`; stroke width comes from CSS (Task 5). A decorative icon gets `aria-hidden="true"`; an icon that carries meaning gets `role="img"` and an `aria-label`.
- Motion: only `opacity` or `transform`, 150ms or less, except the `ghr-pulse` animation (Task 5).
- A `.tsx` file exports components only (the lint rule `react-refresh/only-export-components` runs with `--max-warnings 0`). Put exported helper functions in `.ts` files under `src/lib/`.
- Comments only where the why is non-obvious; match the surrounding code. English only.
- Before each commit, from `web/`: `npm run typecheck` and `npm run lint` both pass, and the task's tests pass. Give every test command an explicit timeout (the Bash tool's `timeout` parameter, 600000 ms for the full suite).
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no period. Scope `web` unless a step says otherwise.
- Tests run with `TZ=UTC` (set in `vite.config.ts`), so local-time formatting in tests is UTC. The shared fixtures put "now" at `2026-10-03T14:05:00Z`.

## Contracts

**API types** (`web/src/api/types.ts`, Task 1)

```ts
// Status gains
  rate_limit?: number
  disk_used_bytes: number
  disk_total_bytes: number
// RepoStatus gains
  oldest_queued_at?: string

export type ActivityWindow = "1h" | "3h" | "24h" | "7d" | "30d"
export interface ActivitySegment { state: string; from: string; to: string | null }
export interface ActivityRun { instance_id: string; repo: string; workflow?: string; job?: string; run_number?: string; segments: ActivitySegment[]; html_url?: string }
export interface ActivityLane { runs: ActivityRun[] }
export interface ActivityBucket { start: string; end: string; busy_minutes: number; busy_pct: number | null; succeeded: number; failed: number; cancelled: number; waiting_max: number | null; cpu_avg: number | null }
export interface ActivityPoint { at: string; value: number }
export interface ActivityCPU { at: string; cpu: number | null; mem: number | null }
export interface ActivityHour { start: string; succeeded: number; failed: number; cancelled: number }
export interface ActivityRepo { repo: string; hours: ActivityHour[] }
export interface Activity { window: string; tz: string; from: string; to: string; capacity: number | null; history_from: string; lanes: ActivityLane[]; buckets: ActivityBucket[]; waiting: ActivityPoint[]; cpu: ActivityCPU[]; repos: ActivityRepo[] }
```

Segment states: `starting`, `warm`, `running`, `succeeded`, `failed`, `cancelled`, `skipped`. `busy_pct` is a percentage (0 to 100, one decimal), not a fraction.

**Test fixtures** (`web/src/test/fixtures.ts`): `fixtures.activityLanes: Activity` (window `1h`, capacity 2) and `fixtures.activityBuckets: Activity` (window `24h`, capacity null), Task 1. `authedRoutes()` answers `GET /api/activity` (lanes for `1h`/`3h`, buckets otherwise) and `GET /api/storage`, Task 24.

**API client and hooks** (Task 2)
- `api.activity(window: ActivityWindow, tz: string, signal?: AbortSignal): Promise<Activity>` → `GET /api/activity?window=…&tz=…`.
- `keys.activity(window: string, tz: string)` → `["activity", window, tz]`.
- `browserZone(): string` → the browser's IANA zone.
- `useActivity(window: ActivityWindow)` → TanStack query polling every `POLL_SLOW` (5000 ms).

**Stylesheet** (`web/src/styles/ghr-theme.css`, imported by `web/src/styles/globals.css` after the kit; Tasks 3 to 5)
- `ghr-pulse`: the 1.8s pulse, stopped under `prefers-reduced-motion`.
- `ghr-mark` on a focusable chart element and `ghr-mark-box` on the shape inside it that shows the focus ring.
- `ghr-rail-wide`: hidden while the sidebar rail is collapsed.
- `--ghr-host`: a CSS string (for example `"192.168.0.99"`) that the shell sets on `<html>`; the stylesheet appends it to the brand label.
- Inside `.dr-sidebar-layout-aside` and `.dr-mobile-drawer-content`, light mode rebinds the page tokens to the dark palette, so kit controls in the rail stay readable.

**Formatting helpers** (`web/src/lib/format.ts`, Task 9): `monthDay(d: Date): string` → `"Oct 3"`; `plural(n: number, word: string): string` → `"1 day"`, `"2 days"`.

**Capacity** (Task 8)
- `web/src/lib/capacity.ts`: `type SegmentKind = "busy" | "starting" | "warm" | "free"`; `interface Capacity { segments: SegmentKind[]; over: number; busy: number; text: string; label: string }`; `capacity(status: Status): Capacity`.
- `web/src/components/capacity-bar.tsx`: `CapacitySegments({ status, size?: "sm" | "lg", tone?: "card" | "rail" })`, `CapacityBar({ status })`.

**Shell pieces**: `UpdateCard({ update: RunnerUpdate; offline: boolean })` in `update-card.tsx` (Task 9); `ModeControl()` in `mode-control.tsx` (Task 10); `Brand({ className?: string })` in `brand.tsx` (Task 7).

**Dashboard pieces**
- `ResultIcon({ conclusion: string })` in `result-icon.tsx`; `EventList({ events: GhrEvent[]; limit?: number })` in `event-list.tsx` (Task 12).
- `dashboardSummary(status: Status): string` in `web/src/lib/summary.ts` (Task 13).
- `Sparkline({ values, max?, label, step?: boolean, className?: string })` (Task 14 adds `step` and `className`); `StatCard({ label, value?, unit?, children? })` and `StatCards({ status: Status; metrics: Metrics | undefined; now: number })` in `stat-card.tsx` (Task 14).
- `web/src/lib/activity-view.ts` (Task 15): `ACTIVITY_WINDOWS: readonly ActivityWindow[]`, `isLaneWindow(w: string): boolean`, `windowWords(w: string): string`, `rangeText(window: string, from: string, to: string): string`, `scaleX(t: number, from: number, to: number, width: number): number`, `laneTicks(from: number, to: number, window: string): { at: number; label: string }[]`, `bucketTickLabel(window: string, start: string): string | null`, `runEnd(run: ActivityRun, now: number): number`, `runLabel(run: ActivityRun): string`, `runAria(run: ActivityRun, now: number): string`, `bucketAria(b: ActivityBucket, a: { window: string; capacity: number | null }): string`, `activitySummary(a: Activity): string`, `retentionText(a: Activity): string`, `sinceText(window: string, iso: string): string`, `lineRuns(points: { x: number; v: number | null }[], y: (v: number) => number): { x: number; y: number }[][]`, `GUTTER = 64`, `PAD_RIGHT = 8`.
- `LanesChart({ activity: Activity; now: number; width: number; onOpenRunner: (id: string) => void })` (Task 16); `BucketsChart({ activity: Activity; now: number; width: number })` (Task 17).
- `WindowControl({ value: ActivityWindow; onChange: (w: ActivityWindow) => void })` in `window-control.tsx`; `useActivityWindow(): [ActivityWindow, (w: ActivityWindow) => void]` in `web/src/lib/use-activity-window.ts`, stored under the `localStorage` key `ghr-activity-window` (Task 18).
- `useWidth<T extends HTMLElement>(fallback: number): [RefObject<T | null>, number]` in `web/src/lib/use-width.ts`, minimum 720 (Task 19); `ActivityPanel({ selected: ActivityWindow; onSelect: (w: ActivityWindow) => void; activity: Activity | undefined; error: unknown; onRetry: () => void; now: number; onOpenRunner: (id: string) => void })` (Task 19).
- `RepoActivityStrip({ repo: string; hours: ActivityHour[] })` (Task 20).
- `useRepoActions(repo: RepoStatus, offline: boolean): { locked: boolean; togglePause: () => void; askRemove: () => void; confirm: Confirm | null; closeConfirm: () => void }` in `web/src/lib/use-repo-actions.ts` (Task 21).
- `repoStateWord(r: RepoStatus): "Error" | "Removing" | "Paused" | "Running" | "Idle"` in `web/src/lib/status.ts`; `RepoTable({ repos: RepoStatus[]; activity: ActivityRepo[] | undefined; now: number; offline: boolean; onOpen: (name: string) => void })` (Task 22).
- `diskParts(storage: Storage | undefined, used: number): DiskPart[]` with `interface DiskPart { key: string; label: string; bytes: number }` in `web/src/lib/disk.ts`; `DiskBreakdown({ status: Status; storage: Storage | undefined; highWater: number })` (Task 23).

## Assumptions (evidence)

- darkraise-ui 6.9.6 writes its tokens as inline styles on `<html>` (`applyTokens`, ThemeProvider.tsx:164 in the v6.9.6 tag of github.com/darkraise/darkraise-web-template), and `generateTokens` is exported from `darkraise-ui/theme` (theme/index.ts:8). Read 2026-10-08.
- `--chart-1` to `--chart-5` and `--sf-hue*` are whole colours (`hsl(...)`), every other engine token is an HSL triplet (generateTokens.ts:39-44, 58-85). Read 2026-10-08.
- The kit declares `--font-sans`, `--font-heading`, `--font-mono` and the radius scale in `@theme inline` (node_modules/darkraise-ui/dist/styles.css:11445-11548), so Tailwind folds them into utilities at build time; overriding the CSS variables alone does not reach `font-mono` or `rounded-md`. ghr therefore redeclares them in its own `@theme inline` block. Verified 2026-10-08 by grepping the built `web/dist/assets/*.css` (literal `font-family:JetBrains Mono,...` in utilities). Task 4 verifies the override with a build.
- The kit's component CSS sits in `@layer components`, so ghr's unlayered rules win over it without `!important`; only the inline-styled tokens need `!important`. Unverified for every rule; Tasks 3 to 5 verify the result with a build.
- `SidebarLayout` props `showThemeSwitcher`, `activeBar`, `navHeader`, `navFooter`, `headerSlot`, `notificationSlot`, `user`, `onLogout` exist (layout/sidebar/SidebarLayout.tsx:22-74, v6.9.6). The header bar is `<header class="dr-layout-header">` inside `.dr-sidebar-layout`; the rail is `<aside class="dr-sidebar-layout-aside">` with `data-collapsed="true"` when collapsed; the drawer is `.dr-mobile-drawer-content`. Read 2026-10-08.
- `NavItem.badge` is a string rendered in `.dr-sidebar-nav-badge` (SidebarNav.tsx:140), so a link's accessible name becomes "Runners2" style text; tests that found sidebar links by exact name switch to a `^Runners` pattern (Task 11).
- `BrandLogo` renders `<img src="/logo.svg">` plus `appName` in `.dr-brand-logo-label` (BrandLogo.tsx); the host name is appended by CSS from `--ghr-host` because the kit has no slot for it. Ruling, logged here.
- ToggleGroup `type="single"` renders `role="radio"` items with `aria-checked` (ToggleGroup.tsx:199-201); `DropdownMenuItem` takes `disabled` and `onSelect` (DropdownMenu.tsx:429-438). Read 2026-10-08.
- `useTheme()` returns `mode` and `setMode`; `setMode` writes only the `mode` key (ThemeProvider.tsx:1035-1060). Read 2026-10-08.
- `GET /api/activity` serves the plan 1 response; `web/src/api/fixtures/activity-lanes.json` and `activity-buckets.json` already exist and match it (generated by `web/fixtures_test.go`). Checked 2026-10-08.
- Palette HSL triplets were computed from the spec's hex values on 2026-10-08 with a WCAG 2.x script; the ratios match the spec (light `bad` on ground 4.58, dark `shell-muted` on `shell-hi` 4.68). The dark palette on the light-mode rail measures 5.13 or more for text and 8.41 for accent. Task 3's test recomputes them.
- Package versions: `@fontsource/ibm-plex-sans` and `@fontsource/ibm-plex-mono` 5.3.0 (published 2026-07-19), `axe-core` 4.13.0 (2026-08-05; 4.14.0 is from 2026-10-05, too new). `npm view` on 2026-10-08.
- Rulings on spec gaps, each recorded as a register row: `web/src/lib/activity.ts` stays, because `repo-summary.tsx` (Repositories and Repository pages) still uses it (row 10); the Disk heading omits the Docker data root path, which no API field exposes (row 11); `errorText`'s em dash is outside this plan's files (row 12); the manual pre-release check (spec §7) needs a deployment and is row 13.
- Further rulings: the update card is a `bg-card` card inside the rail; the capacity panel uses `shell-hi` with free segments in the shell colour (tone `rail`); a repository strip cell's intensity counts every run that hour (succeeded, failed, cancelled); a job whose conclusion is not success, failure, cancelled or skipped shows the failed icon, as today (register row 5 stays open); the brand wordmark also loads Plex Mono 600, which §2 uses although §1.2 lists only 400 and 500; "Collecting data since" names the start of the first bucket with data, because ghr does not report when it started; the brand label's "host name" is `window.location.hostname`, which is an IP address when the owner browses by IP; the Repositories heading carries "N configured" as the mockup does.
- The kit computes `--surface-card-fill`, `--surface-popover-fill`, `--surface-raised-fill`, `--surface-overlay-fill` and `--surface-overlay-base` once on `<html>` from the root tokens (node_modules/darkraise-ui/dist/styles.css:11957-11990, its own comment at 11973-11977), and `bg-card` paints `--color-card: var(--surface-card-fill)` (styles.css:11471). The light-mode rail scope therefore redeclares them (Task 5). Found by plan review round 1, 2026-10-08.
- The kit already draws focus as a 2px `--focus-ring` outline with a 2px offset (styles.css:219-221, source theme.css:219), so mapping `--focus-ring` to accent (Task 3) meets spec §1.6 for kit controls; only ghr's SVG chart marks need their own rule (`ghr-mark-box`, Task 5).
- `SidebarLayout` wraps the whole shell in a `TooltipProvider` (SidebarLayout.tsx:170), which the in-app tooltips of `ModeControl` and the repository row menu rely on; isolated component tests wrap their own. `<main id="main-content">` exists (SidebarLayout.tsx:255); Task 25 scopes axe to it. `busy_pct` is a percentage with one decimal (internal/activity/buckets.go:123, buckets_test.go:53). The kit registers Tailwind colours for `primary`, `success`, `warning`, `destructive`, `muted` and `muted-foreground` (styles.css:11445-11487), so `fill-success/30` and the like compile. Read 2026-10-08.
- Plan review round 1 scored assumptions 13 (borderline). Decision: proceed. Its two Important findings (the rail surface fills, and git paths versus the working directory) are fixed above, and every unverified premise now names the task whose test or build checks it.

## Task index

1. Mirror the dashboard API types
2. Fetch activity from the client
3. Override the palette tokens
4. Switch the fonts to IBM Plex
5. Add shape, motion, icon and rail styles
6. Pin the theme and clear old theme settings
7. Draw the brand mark
8. Draw the capacity bar
9. Show the runner update card
10. Add the colour mode control
11. Rewire the app shell
12. Add the result icon and event list
13. Write the Dashboard summary sentence
14. Build the stat cards
15. Add the activity view helpers
16. Draw the activity lanes
17. Draw the activity buckets
18. Add the window control
19. Assemble the activity panel
20. Draw the repository activity strip
21. Share the repository actions
22. Build the repository table
23. Draw the disk breakdown
24. Rebuild the Dashboard page
25. Enforce the anti-slop and accessibility rules

---

### Task 1: Mirror the dashboard API types

**Files:**
- Modify: `web/src/api/types.ts` (`RepoStatus`, `Status`, append the activity types)
- Modify: `web/src/test/fixtures.ts`
- Test: `web/src/api/types.test.ts`

**Interfaces:**
- Consumes: the JSON fixtures `web/src/api/fixtures/status.json`, `status-degraded.json`, `activity-lanes.json`, `activity-buckets.json` (already generated by plan 1).
- Produces: Contracts › API types; `fixtures.activityLanes`, `fixtures.activityBuckets`.

**Items:** 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Create the branch**

```bash
git checkout master
git checkout -b feat/web-ui-overhaul
```

- [ ] **Step 2: Write the failing type assertions**

In `web/src/api/types.test.ts`, add two imports next to the other fixture imports:

```ts
import activityBucketsJson from "./fixtures/activity-buckets.json"
import activityLanesJson from "./fixtures/activity-lanes.json"
```

add `Activity` to the `import type { … } from "./types"` list, add these two lines after `const choices: ToolchainChoice[] = choicesJson`:

```ts
const lanes: Activity = activityLanesJson
const buckets: Activity = activityBucketsJson
```

and add these tests inside `describe("type fixtures", …)`:

```ts
  it("status carries the Dashboard fields", () => {
    expect(status.rate_limit).toBe(5000)
    expect(status.disk_used_bytes).toBe(146_000_000_000)
    expect(status.disk_total_bytes).toBe(240_000_000_000)
    expect(status.repos[0]?.oldest_queued_at).toBe("2026-10-03T14:02:00Z")
    expect(degraded).not.toHaveProperty("rate_limit")
    expect(degraded.disk_used_bytes).toBe(0)
    expect(degraded.repos[0]).not.toHaveProperty("oldest_queued_at")
  })

  it("activity parses as lanes and as buckets", () => {
    expect(lanes.capacity).toBe(2)
    expect(lanes.lanes[0]?.runs[2]?.segments[0]?.to).toBeNull()
    expect(lanes.lanes[0]?.runs[0]).not.toHaveProperty("html_url")
    expect(lanes.cpu[2]?.cpu).toBeNull()
    expect(buckets.capacity).toBeNull()
    expect(buckets.buckets).toHaveLength(25)
    expect(buckets.buckets[0]?.busy_pct).toBeNull()
    expect(buckets.repos[0]?.hours).toHaveLength(24)
  })
```

- [ ] **Step 3: Run the type check to verify it fails**

Run: `npm run typecheck`
Expected: FAIL with errors such as `Property 'rate_limit' does not exist on type 'Status'` and `Module '"./types"' has no exported member 'Activity'`.

- [ ] **Step 4: Add the types**

In `web/src/api/types.ts`, change `RepoStatus` to:

```ts
export interface RepoStatus {
  name: string
  paused: boolean
  removing?: boolean
  max: number
  active: number
  queued: number
  oldest_queued_at?: string
  error?: string
  last_job?: HistoryEntry
}
```

In `Status`, replace the two lines `rate_remaining: number` and `disk_pct: number` with:

```ts
  rate_remaining: number
  rate_limit?: number
  disk_pct: number
  disk_used_bytes: number
  disk_total_bytes: number
```

Append to the end of the file:

```ts
export type ActivityWindow = "1h" | "3h" | "24h" | "7d" | "30d"

export interface ActivitySegment {
  state: string
  from: string
  to: string | null
}

export interface ActivityRun {
  instance_id: string
  repo: string
  workflow?: string
  job?: string
  run_number?: string
  segments: ActivitySegment[]
  html_url?: string
}

export interface ActivityLane {
  runs: ActivityRun[]
}

export interface ActivityBucket {
  start: string
  end: string
  busy_minutes: number
  busy_pct: number | null
  succeeded: number
  failed: number
  cancelled: number
  waiting_max: number | null
  cpu_avg: number | null
}

export interface ActivityPoint {
  at: string
  value: number
}

export interface ActivityCPU {
  at: string
  cpu: number | null
  mem: number | null
}

export interface ActivityHour {
  start: string
  succeeded: number
  failed: number
  cancelled: number
}

export interface ActivityRepo {
  repo: string
  hours: ActivityHour[]
}

export interface Activity {
  window: string
  tz: string
  from: string
  to: string
  capacity: number | null
  history_from: string
  lanes: ActivityLane[]
  buckets: ActivityBucket[]
  waiting: ActivityPoint[]
  cpu: ActivityCPU[]
  repos: ActivityRepo[]
}
```

- [ ] **Step 5: Expose the activity fixtures to tests**

In `web/src/test/fixtures.ts`, add the imports

```ts
import activityBucketsJson from "@/api/fixtures/activity-buckets.json"
import activityLanesJson from "@/api/fixtures/activity-lanes.json"
```

add `Activity` to the `import type { … } from "@/api/types"` list, add

```ts
const activityLanes: Activity = activityLanesJson
const activityBuckets: Activity = activityBucketsJson
```

after `const toolchainChoices: …`, and add `activityLanes,` and `activityBuckets,` to the `fixtures` object.

- [ ] **Step 6: Run the checks to verify they pass**

Run: `npm run typecheck && npx vitest run src/api/types.test.ts`
Expected: no type errors; `types.test.ts` passes, including the two new tests.

- [ ] **Step 7: Commit**

```bash
git add web/src/api/types.ts web/src/api/types.test.ts web/src/test/fixtures.ts
git commit -m "feat(web): mirror the activity and status fields"
```

---

### Task 2: Fetch activity from the client

**Files:**
- Modify: `web/src/api/client.ts` (the `api` object)
- Modify: `web/src/api/hooks.ts` (`keys`, new `browserZone`, new `useActivity`)
- Test: `web/src/api/client-pages.test.ts`, `web/src/api/hooks.test.tsx`

**Interfaces:**
- Consumes: Contracts › API types (`Activity`, `ActivityWindow`).
- Produces: Contracts › API client and hooks.

**Items:** 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

In `web/src/api/client-pages.test.ts`, add inside `describe("api for the later pages", …)`:

```ts
  it("asks for activity by window and zone", async () => {
    const { calls } = mockApi({ "GET /api/activity": { window: "3h" } })
    expect((await api.activity("3h", "Asia/Ho_Chi_Minh")).window).toBe("3h")
    expect(calls[0]?.search).toBe("?window=3h&tz=Asia%2FHo_Chi_Minh")
  })
```

In `web/src/api/hooks.test.tsx`, change the hooks import to

```ts
import { appendEvents, appendLog, browserZone, MAX_EVENTS, MAX_LOG, useActivity, useEvents, useLogTail } from "./hooks"
```

add `import { fixtures } from "@/test/fixtures"` below the `withQuery` import, and append:

```ts
describe("useActivity", () => {
  it("asks for the window in the browser's zone", async () => {
    const { calls } = mockApi({ "GET /api/activity": fixtures.activityLanes })
    const { wrapper } = withQuery()
    const { result } = renderHook(() => useActivity("3h"), { wrapper })
    await waitFor(() => expect(result.current.data?.capacity).toBe(2))
    expect(browserZone()).toBe("UTC")
    expect(calls[0]?.search).toBe("?window=3h&tz=UTC")
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `npx vitest run src/api/client-pages.test.ts src/api/hooks.test.tsx`
Expected: FAIL with `api.activity is not a function` and `useActivity is not a function` (or an import error naming `browserZone`).

- [ ] **Step 3: Add the client call**

In `web/src/api/client.ts`, add `Activity` and `ActivityWindow` to the type import from `./types`, and add this line to the `api` object directly after the `metrics:` line:

```ts
  activity: (window: ActivityWindow, tz: string, signal?: AbortSignal) =>
    request<Activity>("GET", `/api/activity?${query({ window, tz })}`, undefined, signal),
```

- [ ] **Step 4: Add the hook**

In `web/src/api/hooks.ts`, change the type import to

```ts
import type { ActivityWindow, GhrEvent, LogChunk, Status, Storage } from "./types"
```

add to `keys`, after `metrics`:

```ts
  activity: (window: string, tz: string) => ["activity", window, tz] as const,
```

and add after `useMetrics`:

```ts
export function browserZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone
}

// The daemon aligns buckets to the zone it is given, so the browser sends its
// own and the columns line up with the viewer's clock.
export function useActivity(window: ActivityWindow) {
  const tz = browserZone()
  return useQuery({
    queryKey: keys.activity(window, tz),
    queryFn: ({ signal }) => api.activity(window, tz, signal),
    refetchInterval: POLL_SLOW,
  })
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `npx vitest run src/api/client-pages.test.ts src/api/hooks.test.tsx && npm run typecheck && npm run lint`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add web/src/api/client.ts web/src/api/hooks.ts web/src/api/client-pages.test.ts web/src/api/hooks.test.tsx
git commit -m "feat(web): poll GET /activity for a window"
```

---

### Task 3: Override the palette tokens

**Files:**
- Create: `web/src/styles/ghr-theme.css`
- Modify: `web/src/styles/globals.css`
- Test: `web/src/styles/theme.test.ts`

**Interfaces:**
- Consumes: `generateTokens` from `darkraise-ui/theme`; `themeConfig` from `web/src/theme.config.ts`.
- Produces: Contracts › Stylesheet (the file and its palette blocks). Tasks 4 and 5 append to the same file and the same test.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/styles/theme.test.ts`:

```ts
import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { generateTokens } from "darkraise-ui/theme"
import { describe, expect, it } from "vitest"
import { themeConfig } from "@/theme.config"

const css = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "ghr-theme.css"), "utf8")

function block(selector: string): Record<string, string> {
  const at = css.indexOf(`${selector} {`)
  if (at < 0) throw new Error(`ghr-theme.css has no "${selector} {" block`)
  const body = css.slice(css.indexOf("{", at) + 1, css.indexOf("}", at))
  const out: Record<string, string> = {}
  for (const m of body.matchAll(/(--[\w-]+):\s*([^;]+);/g)) out[m[1] ?? ""] = (m[2] ?? "").trim()
  return out
}

function tokens(mode: "dark" | "light"): Record<string, string> {
  return { ...block(":root"), ...block(`:root[data-mode="${mode}"]`) }
}

function rgb(value: string): number[] {
  const [h = 0, s = 0, l = 0] = value.replace("!important", "").trim().split(/\s+/).map((p) => parseFloat(p))
  const a = (s / 100) * Math.min(l / 100, 1 - l / 100)
  const f = (n: number) => {
    const k = (n + h / 30) % 12
    return l / 100 - a * Math.max(-1, Math.min(k - 3, 9 - k, 1))
  }
  return [f(0), f(8), f(4)]
}

function luminance(value: string): number {
  const [r = 0, g = 0, b = 0] = rgb(value).map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4))
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function contrast(a: string | undefined, b: string | undefined): number {
  const x = luminance(a ?? "")
  const y = luminance(b ?? "")
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05)
}

const PAIRS: [string, string, number][] = [
  ["--foreground", "--background", 4.5],
  ["--foreground", "--card", 4.5],
  ["--muted-foreground", "--background", 4.5],
  ["--muted-foreground", "--card", 4.5],
  ["--muted-foreground", "--muted", 4.5],
  ["--primary", "--background", 4.5],
  ["--primary", "--card", 4.5],
  ["--success", "--background", 4.5],
  ["--success", "--card", 4.5],
  ["--destructive", "--background", 4.5],
  ["--destructive", "--card", 4.5],
  ["--warning", "--background", 4.5],
  ["--warning", "--card", 4.5],
  ["--primary-foreground", "--primary", 4.5],
  ["--success-foreground", "--success", 4.5],
  ["--destructive-foreground", "--destructive", 4.5],
  ["--warning-foreground", "--warning", 4.5],
  ["--sidebar-foreground", "--surface-sidebar", 4.5],
  ["--sidebar-foreground", "--sidebar-hover-bg", 4.5],
  ["--sidebar-foreground-muted", "--surface-sidebar", 4.5],
  ["--sidebar-foreground-muted", "--sidebar-hover-bg", 4.5],
  ["--primary", "--muted", 3],
]

describe("ghr palette", () => {
  it.each(["dark", "light"] as const)("overrides every token the engine writes in %s mode", (mode) => {
    const d = themeConfig.defaults
    const engine = generateTokens({
      accentColor: d.accentColor,
      surfaceColor: d.surfaceColor,
      preset: d.preset,
      backgroundStyle: d.backgroundStyle,
      backgroundIntensity: d.backgroundIntensity,
      accentIntensity: d.accentIntensity,
      mode,
    })
    const ours = tokens(mode)
    expect(Object.keys(engine).filter((k) => !(k in ours))).toEqual([])
    expect(Object.keys(engine).filter((k) => !ours[k]?.endsWith("!important"))).toEqual([])
  })

  it.each(["dark", "light"] as const)("keeps text and outlines readable in %s mode", (mode) => {
    const t = tokens(mode)
    for (const [fg, bg, min] of PAIRS) expect(contrast(t[fg], t[bg]), `${fg} on ${bg}`).toBeGreaterThanOrEqual(min)
  })

  it("neutralises shadows, noise, gradients and the kit's overlays", () => {
    const root = block(":root")
    expect(root["--shadow-card"]).toBe("none !important")
    expect(root["--shadow-dropdown"]).toBe("none !important")
    expect(root["--noise-opacity"]).toBe("0 !important")
    expect(root["--content-gradient-overlay"]).toBe("none !important")
    expect(css).toMatch(/\.sidebar-gradient-overlay::before,\s*\.header-gradient-overlay::before,\s*main\[data-content\]::before \{\s*background: none !important;/)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npx vitest run src/styles/theme.test.ts`
Expected: FAIL with `ENOENT` for `ghr-theme.css`.

- [ ] **Step 3: Write the palette**

Create `web/src/styles/ghr-theme.css`:

```css
/* ghr's palette over darkraise-ui. The theme engine writes these tokens as
   inline styles on <html>, so each override needs !important to win. The
   values are HSL triplets because the kit reads them as hsl(var(--token));
   --chart-* and --sf-hue* are whole colours. */

:root {
  --shadow-card: none !important;
  --shadow-dropdown: none !important;
  --noise-opacity: 0 !important;
  --content-gradient-overlay: none !important;
  --surface-tint: none !important;
  --bg-style: solid !important;
  --sf-hue: transparent !important;
  --sf-hue-2: transparent !important;
  --sf-hue-3: transparent !important;
}

:root[data-mode="dark"] {
  --background: 212.7 29.7% 7.3% !important;
  --surface-base: 212.7 29.7% 7.3% !important;
  --card: 211.8 32.1% 10.4% !important;
  --popover: 211.8 32.1% 10.4% !important;
  --surface-raised: 211.8 32.1% 10.4% !important;
  --surface-overlay: 211.8 32.1% 10.4% !important;
  --surface-header: 211.8 32.1% 10.4% !important;
  --muted: 213.0 28.6% 13.7% !important;
  --secondary: 213.0 28.6% 13.7% !important;
  --accent: 213.0 28.6% 13.7% !important;
  --surface-sunken: 213.0 28.6% 13.7% !important;
  --control-well-subtle: 213.0 28.6% 13.7% !important;
  --control-well-deep: 213.0 28.6% 13.7% !important;
  --border: 212.5 26.1% 18.0% !important;
  --input: 212.5 26.1% 18.0% !important;
  --border-default: 212.5 26.1% 18.0% !important;
  --border-subtle: 212.5 26.1% 14.0% !important;
  --border-strong: 212.5 26.1% 22.0% !important;
  --foreground: 212.0 24.6% 88.0% !important;
  --card-foreground: 212.0 24.6% 88.0% !important;
  --popover-foreground: 212.0 24.6% 88.0% !important;
  --secondary-foreground: 212.0 24.6% 88.0% !important;
  --accent-foreground: 212.0 24.6% 88.0% !important;
  --muted-foreground: 214.3 12.4% 55.7% !important;
  --legend: 214.3 12.4% 55.7% !important;
  --primary: 188.3 62.7% 56.9% !important;
  --primary-fill: 188.3 62.7% 56.9% !important;
  --ring: 188.3 62.7% 56.9% !important;
  --focus-ring: 188.3 62.7% 56.9% !important;
  --info: 188.3 62.7% 56.9% !important;
  --primary-foreground: 192.7 73.3% 8.8% !important;
  --info-foreground: 192.7 73.3% 8.8% !important;
  --success: 136.2 39.7% 50.6% !important;
  --success-foreground: 212.7 29.7% 7.3% !important;
  --destructive: 5.4 81.9% 65.3% !important;
  --destructive-foreground: 212.7 29.7% 7.3% !important;
  --warning: 37.7 71.6% 57.3% !important;
  --warning-foreground: 212.7 29.7% 7.3% !important;
  --surface-sidebar: 213.3 33.3% 5.3% !important;
  --sidebar-foreground: 213.0 22.7% 82.7% !important;
  --sidebar-foreground-hover: 213.0 22.7% 82.7% !important;
  --sidebar-foreground-muted: 214.0 13.0% 54.9% !important;
  --sidebar-hover-bg: 213.0 30.3% 12.9% !important;
  --sidebar-border: 213.0 30.3% 12.9% !important;
  --chart-1: hsl(188.3 62.7% 56.9%) !important;
  --chart-2: hsl(190.3 44.9% 40.6%) !important;
  --chart-3: hsl(193.2 43.0% 31.0%) !important;
  --chart-4: hsl(195.9 39.8% 24.1%) !important;
  --chart-5: hsl(214.3 12.4% 55.7%) !important;
}

:root[data-mode="light"] {
  --background: 210.0 21.4% 94.5% !important;
  --surface-base: 210.0 21.4% 94.5% !important;
  --card: 0.0 0.0% 100.0% !important;
  --popover: 0.0 0.0% 100.0% !important;
  --surface-raised: 0.0 0.0% 100.0% !important;
  --surface-overlay: 0.0 0.0% 100.0% !important;
  --surface-header: 0.0 0.0% 100.0% !important;
  --muted: 213.3 20.9% 91.6% !important;
  --secondary: 213.3 20.9% 91.6% !important;
  --accent: 213.3 20.9% 91.6% !important;
  --surface-sunken: 213.3 20.9% 91.6% !important;
  --control-well-subtle: 213.3 20.9% 91.6% !important;
  --control-well-deep: 213.3 20.9% 91.6% !important;
  --border: 214.3 19.4% 85.9% !important;
  --input: 214.3 19.4% 85.9% !important;
  --border-default: 214.3 19.4% 85.9% !important;
  --border-subtle: 214.3 19.4% 89.9% !important;
  --border-strong: 214.3 19.4% 81.9% !important;
  --foreground: 213.3 33.3% 10.6% !important;
  --card-foreground: 213.3 33.3% 10.6% !important;
  --popover-foreground: 213.3 33.3% 10.6% !important;
  --secondary-foreground: 213.3 33.3% 10.6% !important;
  --accent-foreground: 213.3 33.3% 10.6% !important;
  --muted-foreground: 214.8 15.0% 40.6% !important;
  --legend: 214.8 15.0% 40.6% !important;
  --primary: 189.0 88.2% 26.7% !important;
  --primary-fill: 189.0 88.2% 26.7% !important;
  --ring: 189.0 88.2% 26.7% !important;
  --focus-ring: 189.0 88.2% 26.7% !important;
  --info: 189.0 88.2% 26.7% !important;
  --primary-foreground: 0.0 0.0% 100.0% !important;
  --info-foreground: 0.0 0.0% 100.0% !important;
  --success: 135.9 55.4% 30.8% !important;
  --success-foreground: 0.0 0.0% 100.0% !important;
  --destructive: 4.4 61.6% 48.0% !important;
  --destructive-foreground: 0.0 0.0% 100.0% !important;
  --warning: 37.8 100.0% 27.1% !important;
  --warning-foreground: 0.0 0.0% 100.0% !important;
  --surface-sidebar: 212.0 28.3% 10.4% !important;
  --sidebar-foreground: 213.8 23.5% 86.7% !important;
  --sidebar-foreground-hover: 213.8 23.5% 86.7% !important;
  --sidebar-foreground-muted: 214.0 14.3% 58.8% !important;
  --sidebar-hover-bg: 213.9 24.7% 18.2% !important;
  --sidebar-border: 213.9 24.7% 18.2% !important;
  --chart-1: hsl(189.0 88.2% 26.7%) !important;
  --chart-2: hsl(190.0 31.5% 51.4%) !important;
  --chart-3: hsl(191.3 30.3% 65.7%) !important;
  --chart-4: hsl(195.0 29.5% 76.1%) !important;
  --chart-5: hsl(214.8 15.0% 40.6%) !important;
}

.sidebar-gradient-overlay::before,
.header-gradient-overlay::before,
main[data-content]::before {
  background: none !important;
}
```

- [ ] **Step 4: Import it after the kit**

Replace `web/src/styles/globals.css` with:

```css
@import "darkraise-ui/styles.css";
@import "./ghr-theme.css";
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `npx vitest run src/styles/theme.test.ts`
Expected: PASS (5 tests). If the coverage test lists a missing key, the installed kit writes a token this file lacks: add it to both mode blocks (or to `:root` if it is mode-independent) with the role it plays, and rerun.

- [ ] **Step 6: Check the build, then commit**

Run: `npm run build`
Expected: exits 0.

```bash
git add web/src/styles/ghr-theme.css web/src/styles/globals.css web/src/styles/theme.test.ts
git commit -m "feat(web): override the kit palette with ghr's"
```

---

### Task 4: Switch the fonts to IBM Plex

**Files:**
- Modify: `web/package.json`, `web/package-lock.json` (through npm)
- Modify: `web/src/main.tsx:1-6`
- Modify: `web/src/styles/ghr-theme.css` (insert a block)

**Interfaces:**
- Consumes: Contracts › Stylesheet (the file from Task 3).
- Produces: `font-sans` and `font-mono` utilities resolve to IBM Plex Sans and IBM Plex Mono.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Show the old fonts in the build (the failing check)**

Run: `npm run build && grep -l "IBM Plex" dist/assets/*.css`
Expected: `grep` prints nothing and exits 1 (no Plex yet).

- [ ] **Step 2: Swap the font packages**

```bash
npm uninstall @fontsource/inter @fontsource/jetbrains-mono
npm install @fontsource/ibm-plex-sans@5.3.0 @fontsource/ibm-plex-mono@5.3.0
ls node_modules/@fontsource/ibm-plex-sans/latin-600.css node_modules/@fontsource/ibm-plex-mono/latin-600.css
```

Expected: both `ls` paths print.

- [ ] **Step 3: Import the faces**

In `web/src/main.tsx`, replace the six `@fontsource` import lines (lines 1 to 6) with:

```ts
import "@fontsource/ibm-plex-sans/latin-400.css"
import "@fontsource/ibm-plex-sans/latin-500.css"
import "@fontsource/ibm-plex-sans/latin-600.css"
import "@fontsource/ibm-plex-mono/latin-400.css"
import "@fontsource/ibm-plex-mono/latin-500.css"
import "@fontsource/ibm-plex-mono/latin-600.css"
```

- [ ] **Step 4: Rebind the font theme**

In `web/src/styles/ghr-theme.css`, insert directly after the opening comment (before `:root {`):

```css
/* The kit declares its fonts in @theme inline, which Tailwind folds into the
   utilities at build time; redeclaring them here is what reaches font-mono
   and every @apply font-sans in the kit's own CSS. */
@theme inline {
  --font-sans: "IBM Plex Sans", ui-sans-serif, system-ui, sans-serif;
  --font-heading: "IBM Plex Sans", ui-sans-serif, system-ui, sans-serif;
  --font-mono: "IBM Plex Mono", ui-monospace, SFMono-Regular, Menlo, monospace;
}
```

- [ ] **Step 5: Verify the build uses Plex only**

Run: `npm run build && grep -c "IBM Plex Mono" dist/assets/*.css && ! grep -q '"Inter"\|JetBrains Mono' dist/assets/*.css && ls dist/assets | grep -c "ibm-plex"`
Expected: exits 0; the first count is 1 or more, the last count is 10 or more (woff and woff2 files for six faces).

- [ ] **Step 6: Run the suite parts that render text, then commit**

Run: `npx vitest run src/styles src/app.test.tsx && npm run typecheck && npm run lint`
Expected: all pass.

```bash
git add web/package.json web/package-lock.json web/src/main.tsx web/src/styles/ghr-theme.css
git commit -m "feat(web): set the type in IBM Plex"
```

---

### Task 5: Add shape, motion, icon and rail styles

**Files:**
- Modify: `web/src/styles/ghr-theme.css` (append)
- Test: `web/src/styles/theme.test.ts` (append tests)

**Interfaces:**
- Consumes: the file and the `block` and `contrast` helpers from Task 3.
- Produces: Contracts › Stylesheet (`ghr-pulse`, `ghr-mark`, `ghr-mark-box`, `ghr-rail-wide`, `--ghr-host`, the light-mode rail scope).

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Append to `web/src/styles/theme.test.ts`:

```ts
describe("ghr chrome styles", () => {
  it("keeps the light-mode rail readable with the dark palette", () => {
    const light = tokens("light")
    const rail = { ...light, ...block(':root[data-mode="light"] .dr-mobile-drawer-content') }
    const shell = light["--surface-sidebar"]
    const checks: [string, number][] = [
      ["--foreground", 4.5],
      ["--muted-foreground", 4.5],
      ["--warning", 4.5],
      ["--destructive", 4.5],
      ["--success", 4.5],
      ["--primary", 3],
    ]
    for (const [fg, min] of checks) {
      expect(contrast(rail[fg], shell), `${fg} on the rail`).toBeGreaterThanOrEqual(min)
      expect(contrast(rail[fg], rail["--card"]), `${fg} on a rail card`).toBeGreaterThanOrEqual(min)
    }
  })

  it("recomputes the kit's surface fills inside the rail", () => {
    // bg-card paints var(--surface-card-fill), which the kit computes on
    // <html>; without these the rail's cards stay the light-mode white.
    const rail = block(':root[data-mode="light"] .dr-mobile-drawer-content')
    expect(rail["--surface-card-fill"]).toBe("hsl(var(--card))")
    expect(rail["--surface-popover-fill"]).toBe("hsl(var(--popover))")
    expect(rail["--surface-raised-fill"]).toBe("hsl(var(--surface-raised))")
    expect(rail["--surface-overlay-fill"]).toBe("hsl(var(--surface-overlay))")
  })

  it("hides the header bar at 768px and wider", () => {
    expect(css).toMatch(/@media \(min-width: 768px\) \{\s*\.dr-sidebar-layout > \.dr-layout-header \{\s*display: none;/)
  })

  it("pins two radii: 6px on controls and 10px on surfaces", () => {
    expect(block(":root[data-radius]")).toEqual({ "--radius": "10px", "--radius-button": "6px" })
    expect(css).toMatch(/--radius-md: 6px;/)
  })

  it("stops the pulse when motion is reduced", () => {
    expect(css).toMatch(/\.ghr-pulse \{\s*animation: ghr-pulse 1\.8s ease-out infinite;/)
    expect(css).toMatch(/@media \(prefers-reduced-motion: reduce\) \{\s*\.ghr-pulse \{\s*animation: none;/)
  })

  it("draws every Lucide icon at one stroke width", () => {
    expect(css).toMatch(/\.lucide \{\s*stroke-width: 1\.75;/)
  })

  it("appends the host to the brand label", () => {
    expect(css).toMatch(/\.dr-brand-logo-label::after \{\s*content: var\(--ghr-host, ""\);/)
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `npx vitest run src/styles/theme.test.ts`
Expected: the six new tests FAIL (the first with `ghr-theme.css has no ":root[data-mode="light"] .dr-mobile-drawer-content {" block`); the Task 3 tests still pass.

- [ ] **Step 3: Append the styles**

Append to `web/src/styles/ghr-theme.css`:

```css
/* The rail is dark in both modes. Kit controls inside it read the page
   tokens, so in light mode the rail rebinds them to the dark palette over
   the light-mode shell colour. */
:root[data-mode="light"] .dr-sidebar-layout-aside,
:root[data-mode="light"] .dr-mobile-drawer-content {
  --background: 212.0 28.3% 10.4%;
  --card: 211.8 32.1% 10.4%;
  --popover: 211.8 32.1% 10.4%;
  --surface-raised: 211.8 32.1% 10.4%;
  --surface-overlay: 211.8 32.1% 10.4%;
  --muted: 213.0 28.6% 13.7%;
  --secondary: 213.0 28.6% 13.7%;
  --accent: 213.0 28.6% 13.7%;
  --surface-sunken: 213.0 28.6% 13.7%;
  --control-well-subtle: 213.0 28.6% 13.7%;
  --control-well-deep: 213.0 28.6% 13.7%;
  --border: 212.5 26.1% 18.0%;
  --input: 212.5 26.1% 18.0%;
  --border-default: 212.5 26.1% 18.0%;
  --border-subtle: 212.5 26.1% 14.0%;
  --border-strong: 212.5 26.1% 22.0%;
  --foreground: 212.0 24.6% 88.0%;
  --card-foreground: 212.0 24.6% 88.0%;
  --popover-foreground: 212.0 24.6% 88.0%;
  --secondary-foreground: 212.0 24.6% 88.0%;
  --accent-foreground: 212.0 24.6% 88.0%;
  --muted-foreground: 214.3 12.4% 55.7%;
  --legend: 214.3 12.4% 55.7%;
  --primary: 188.3 62.7% 56.9%;
  --primary-fill: 188.3 62.7% 56.9%;
  --ring: 188.3 62.7% 56.9%;
  --focus-ring: 188.3 62.7% 56.9%;
  --info: 188.3 62.7% 56.9%;
  --primary-foreground: 192.7 73.3% 8.8%;
  --info-foreground: 192.7 73.3% 8.8%;
  --success: 136.2 39.7% 50.6%;
  --success-foreground: 212.7 29.7% 7.3%;
  --destructive: 5.4 81.9% 65.3%;
  --destructive-foreground: 212.7 29.7% 7.3%;
  --warning: 37.7 71.6% 57.3%;
  --warning-foreground: 212.7 29.7% 7.3%;
  /* The kit computes these fills once on <html> from the root tokens, so
     they must be redeclared here to pick up the rebound values above. */
  --surface-card-fill: hsl(var(--card));
  --surface-card-fill-opaque: hsl(var(--card));
  --surface-popover-fill: hsl(var(--popover));
  --surface-raised-fill: hsl(var(--surface-raised));
  --surface-overlay-fill: hsl(var(--surface-overlay));
  --surface-overlay-base: hsl(var(--popover));
  --surface-overlay-border: hsl(var(--border));
}

@theme inline {
  --radius-sm: 4px;
  --radius-md: 6px;
  --radius-lg: 10px;
  --radius-xl: 10px;
}

:root[data-radius] {
  --radius: 10px;
  --radius-button: 6px;
}

body {
  font-variant-numeric: tabular-nums;
}

.lucide {
  stroke-width: 1.75;
}

.dr-sidebar-nav-icon,
.dr-sidebar-nav-icon-svg {
  width: 16px;
  height: 16px;
}

.dr-sidebar-nav-item.active,
.dr-sidebar-nav-item[data-status="active"] {
  background: hsl(var(--sidebar-hover-bg));
  box-shadow: none;
  font-weight: 600;
}

.dr-sidebar-nav-badge {
  padding: 0;
  border-radius: 0;
  background: none;
  color: hsl(var(--sidebar-foreground-muted));
  font-family: "IBM Plex Mono", ui-monospace, monospace;
  font-size: var(--text-xs);
}

.dr-brand-logo-label {
  font-family: "IBM Plex Mono", ui-monospace, monospace;
  font-weight: 600;
}

.dr-brand-logo-label::after {
  content: var(--ghr-host, "");
  margin-left: 0.5rem;
  color: hsl(var(--sidebar-foreground-muted));
  font-size: var(--text-xs);
  font-weight: 400;
}

.dr-sidebar-layout-aside[data-collapsed="true"] .ghr-rail-wide {
  display: none;
}

.dr-layout-header {
  border-color: hsl(var(--border));
}

/* A page's summary sentence reads at the base size, not the kit's small. */
.dr-page-header-description {
  font-size: var(--text-base);
}

@media (min-width: 768px) {
  .dr-sidebar-layout > .dr-layout-header {
    display: none;
  }
}

::selection {
  background: hsl(var(--primary) / 0.3);
  color: hsl(var(--foreground));
}

input,
textarea {
  caret-color: hsl(var(--primary));
}

* {
  scrollbar-color: hsl(var(--border-strong)) transparent;
}

*::-webkit-scrollbar-thumb {
  background-color: hsl(var(--border-strong));
}

*::-webkit-scrollbar-thumb:hover {
  background-color: hsl(var(--muted-foreground));
}

.ghr-mark {
  outline: none;
}

.ghr-mark:focus-visible .ghr-mark-box {
  stroke: hsl(var(--focus-ring));
  stroke-width: 2px;
}

@keyframes ghr-pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.25;
  }
}

.ghr-pulse {
  animation: ghr-pulse 1.8s ease-out infinite;
}

@media (prefers-reduced-motion: reduce) {
  .ghr-pulse {
    animation: none;
  }

  *,
  *::before,
  *::after {
    transition-duration: 0s !important;
  }
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `npx vitest run src/styles/theme.test.ts && npm run build`
Expected: all theme tests pass; the build exits 0.

- [ ] **Step 5: Commit**

```bash
git add web/src/styles/ghr-theme.css web/src/styles/theme.test.ts
git commit -m "feat(web): style shape, motion, icons and the rail"
```

---

### Task 6: Pin the theme and clear old theme settings

**Files:**
- Modify: `web/src/theme.config.ts`
- Modify: `web/public/theme-init.js`
- Test: `web/src/theme-init.test.ts` (create), `web/src/styles/theme.test.ts` (append one test)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: the pinned `themeConfig` (mode defaults to `dark`, switcher off); `theme-init.js` clears `theme-*` keys once behind the `ghr-theme-v2` flag.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 1 = 2

- [ ] **Step 1: Write the failing tests**

Create `web/src/theme-init.test.ts`:

```ts
import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { beforeEach, describe, expect, it } from "vitest"

const script = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "../public/theme-init.js"), "utf8")
const run = () => new Function(script)()
const root = document.documentElement

describe("theme-init.js", () => {
  beforeEach(() => {
    localStorage.clear()
    for (const name of root.getAttributeNames()) if (name.startsWith("data-")) root.removeAttribute(name)
  })

  it("clears the old theme settings once and keeps the mode", () => {
    localStorage.setItem("theme-preset", "glass")
    localStorage.setItem("theme-density", "spacious")
    localStorage.setItem("mode", "light")
    localStorage.setItem("ghr-activity-window", "24h")
    run()
    expect(localStorage.getItem("theme-preset")).toBeNull()
    expect(localStorage.getItem("theme-density")).toBeNull()
    expect(localStorage.getItem("mode")).toBe("light")
    expect(localStorage.getItem("ghr-activity-window")).toBe("24h")
    expect(localStorage.getItem("ghr-theme-v2")).toBe("1")
    expect(root.getAttribute("data-mode")).toBe("light")
  })

  it("clears them only the first time", () => {
    localStorage.setItem("ghr-theme-v2", "1")
    localStorage.setItem("theme-radius", "pill")
    run()
    expect(localStorage.getItem("theme-radius")).toBe("pill")
  })

  it("falls back to dark", () => {
    run()
    expect(root.getAttribute("data-mode")).toBe("dark")
  })

  it("resolves system from the media query", () => {
    localStorage.setItem("mode", "system")
    run()
    expect(root.getAttribute("data-mode")).toBe("light")
  })

  it("pins the theme axes before React mounts", () => {
    run()
    expect(root.getAttribute("data-density")).toBe("compact")
    expect(root.getAttribute("data-radius")).toBe("subtle")
    expect(root.getAttribute("data-font-size")).toBe("medium")
  })
})
```

The `matchMedia` stub in `src/test/setup.ts` answers `matches: false`, so `system` resolves to light.

Append to `web/src/styles/theme.test.ts`:

```ts
describe("theme config", () => {
  it("pins every axis and turns the switcher off", () => {
    expect(themeConfig.switcher.enabled).toBe(false)
    expect(Object.values(themeConfig.switcher.axes).every((on) => !on)).toBe(true)
    expect(themeConfig.defaults).toMatchObject({
      preset: "default",
      density: "compact",
      fontSize: "medium",
      elevation: "low",
      buttonElevation: "flat",
      radius: "subtle",
      controlDepth: "flush",
      shellStyle: "classic",
      backgroundStyle: "solid",
      outerGlow: "none",
      innerGlow: "none",
      mode: "dark",
    })
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `npx vitest run src/theme-init.test.ts src/styles/theme.test.ts`
Expected: FAIL: the old script keeps `theme-preset`, falls back to `system`, sets no `data-density`; the config test sees `enabled: true`.

- [ ] **Step 3: Pin the config**

Replace `web/src/theme.config.ts` with:

```ts
import type { ThemeConfig } from "darkraise-ui/theme"

// ghr has one look: ghr-theme.css overrides the palette the engine derives,
// and the only choice left to the viewer is the colour mode.
export const themeConfig: ThemeConfig = {
  defaults: {
    accentColor: "blue",
    surfaceColor: "slate",
    preset: "default",
    backgroundStyle: "solid",
    backgroundIntensity: "balanced",
    gradientPattern: "blobs",
    mode: "dark",
    density: "compact",
    elevation: "low",
    buttonElevation: "flat",
    surfaceIntensity: "balanced",
    radius: "subtle",
    controlDepth: "flush",
    shellStyle: "classic",
    sidebarActiveBar: "ring",
    fontSize: "medium",
    accentIntensity: "balanced",
    outerGlow: "none",
    innerGlow: "none",
  },
  switcher: {
    enabled: false,
    axes: {
      mode: false,
      accentColor: false,
      surfaceColor: false,
      preset: false,
      backgroundStyle: false,
      backgroundIntensity: false,
      gradientPattern: false,
      density: false,
      elevation: false,
      buttonElevation: false,
      surfaceIntensity: false,
      radius: false,
      controlDepth: false,
      shellStyle: false,
      sidebarActiveBar: false,
      fontSize: false,
      accentIntensity: false,
      outerGlow: false,
      innerGlow: false,
      presetAxes: false,
    },
  },
}
```

- [ ] **Step 4: Rewrite the init script**

Replace `web/public/theme-init.js` with:

```js
;(function () {
  var root = document.documentElement
  var mode = null
  try {
    var store = window.localStorage
    // The old theme switcher saved every axis; a saved axis would override
    // the pins in theme.config.ts, so they are cleared once.
    if (!store.getItem("ghr-theme-v2")) {
      for (var i = store.length - 1; i >= 0; i--) {
        var key = store.key(i)
        if (key && key.indexOf("theme-") === 0) store.removeItem(key)
      }
      store.setItem("ghr-theme-v2", "1")
    }
    mode = store.getItem("mode")
  } catch (e) {
    mode = null
  }
  var dark =
    mode === "system" ? window.matchMedia("(prefers-color-scheme: dark)").matches : mode !== "light"
  root.setAttribute("data-mode", dark ? "dark" : "light")

  // The attribute-driven pins from theme.config.ts that change the first
  // paint; ThemeProvider sets the rest when React mounts.
  var pinned = {
    preset: "default",
    "background-style": "solid",
    density: "compact",
    "font-size": "medium",
    radius: "subtle",
    elevation: "low",
    "button-elevation": "flat",
    "control-depth": "flush",
    "shell-style": "classic",
    "sidebar-active-bar": "ring",
  }
  for (var axis in pinned) root.setAttribute("data-" + axis, pinned[axis])
})()
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `npx vitest run src/theme-init.test.ts src/styles/theme.test.ts src/app.test.tsx && npm run typecheck && npm run lint`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add web/src/theme.config.ts web/public/theme-init.js web/src/theme-init.test.ts web/src/styles/theme.test.ts
git commit -m "feat(web): pin the theme and default to dark"
```

---

### Task 7: Draw the brand mark

**Files:**
- Modify: `web/public/logo.svg` (replace)
- Create: `web/public/favicon.svg`
- Modify: `web/index.html:5`
- Create: `web/src/components/brand.tsx`
- Modify: `web/src/pages/login.tsx:68-72`, `web/src/pages/setup.tsx:320-321`
- Test: `web/src/components/brand.test.tsx`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `Brand({ className?: string })`; the sidebar's `BrandLogo` shows the new `logo.svg`.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 0 - risk 1 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/components/brand.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { mockApi } from "@/test/api"
import { renderApp } from "@/test/render"
import { Brand } from "./brand"

describe("Brand", () => {
  it("shows the mark beside the wordmark", () => {
    const { container } = render(<Brand />)
    expect(screen.getByText("ghr")).toHaveClass("font-mono")
    expect(container.querySelector("svg")).toHaveAttribute("aria-hidden", "true")
  })

  it("heads the login page", async () => {
    mockApi({ "GET /auth/state": { setup_required: false, authenticated: false } })
    renderApp("/login")
    expect(await screen.findByText("ghr")).toHaveClass("font-mono")
    expect(screen.getAllByText("Log in")).toHaveLength(2)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npx vitest run src/components/brand.test.tsx`
Expected: FAIL with `Failed to resolve import "./brand"`.

- [ ] **Step 3: Write the component**

Create `web/src/components/brand.tsx`:

```tsx
export function Brand({ className = "" }: { className?: string }) {
  return (
    <div className={`flex items-center justify-center gap-2 ${className}`}>
      <svg width="28" height="28" viewBox="0 0 22 22" aria-hidden="true" className="text-foreground">
        <rect x="2" y="3.5" width="18" height="3.2" rx="1.6" fill="currentColor" />
        <rect x="2" y="9.4" width="12" height="3.2" rx="1.6" fill="currentColor" opacity="0.62" />
        <rect x="2" y="15.3" width="6.5" height="3.2" rx="1.6" fill="currentColor" opacity="0.38" />
        <circle cx="16.6" cy="16.9" r="2.6" className="fill-primary" />
      </svg>
      <span className="font-mono text-xl font-semibold">ghr</span>
    </div>
  )
}
```

- [ ] **Step 4: Put it on the login and setup pages**

In `web/src/pages/login.tsx`, add `import { Brand } from "@/components/brand"` to the imports, and change the returned wrapper and title (currently lines 69 to 72) from

```tsx
    <div className="flex min-h-screen items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>ghr</CardTitle>
```

to

```tsx
    <div className="flex min-h-screen flex-col items-center justify-center gap-6 p-4">
      <Brand />
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>{setup ? "Set a password" : "Log in"}</CardTitle>
```

In `web/src/pages/setup.tsx`, add `import { Brand } from "@/components/brand"` and `import { Check } from "lucide-react"`, and insert `<Brand className="mb-6" />` as the first child of `<div className="mx-auto max-w-4xl p-4 sm:p-8">`, directly above `<h1 className="mb-1 text-2xl font-semibold">Set up ghr</h1>`. In the same file the step list marks a finished step with a Unicode "✓" (spec §1.5 retires glyphs in files this spec touches); change

```tsx
                {done ? "✓" : `${i + 1}.`} {name}
```

to

```tsx
                {done ? <Check size={15} role="img" aria-label="Done" className="inline align-[-2px] text-success" /> : `${i + 1}.`} {name}
```

- [ ] **Step 5: Replace the logo and add the favicon**

Replace `web/public/logo.svg` with (the rail is dark in both modes, so the bars are light):

```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 22 22"><rect x="2" y="3.5" width="18" height="3.2" rx="1.6" fill="#c9d2dd"/><rect x="2" y="9.4" width="12" height="3.2" rx="1.6" fill="#c9d2dd" opacity=".62"/><rect x="2" y="15.3" width="6.5" height="3.2" rx="1.6" fill="#c9d2dd" opacity=".38"/><circle cx="16.6" cy="16.9" r="2.6" fill="#4cc3d6"/></svg>
```

Create `web/public/favicon.svg` (a dark tile, so the mark reads on light and dark browser tabs):

```svg
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 22 22"><rect width="22" height="22" rx="5" fill="#0d1218"/><rect x="2" y="3.5" width="18" height="3.2" rx="1.6" fill="#c9d2dd"/><rect x="2" y="9.4" width="12" height="3.2" rx="1.6" fill="#c9d2dd" opacity=".62"/><rect x="2" y="15.3" width="6.5" height="3.2" rx="1.6" fill="#c9d2dd" opacity=".38"/><circle cx="16.6" cy="16.9" r="2.6" fill="#4cc3d6"/></svg>
```

In `web/index.html`, change line 5 to:

```html
    <link rel="icon" type="image/svg+xml" href="/favicon.svg" />
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `npx vitest run src/components/brand.test.tsx src/pages/login.test.tsx src/pages/setup.test.tsx src/app.test.tsx && npm run typecheck && npm run lint`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add web/public/logo.svg web/public/favicon.svg web/index.html web/src/components/brand.tsx web/src/components/brand.test.tsx web/src/pages/login.tsx web/src/pages/setup.tsx
git commit -m "feat(web): draw the ghr lanes mark"
```

---

### Task 8: Draw the capacity bar

**Files:**
- Create: `web/src/lib/capacity.ts`
- Create: `web/src/components/capacity-bar.tsx`
- Test: `web/src/components/capacity-bar.test.tsx`

**Interfaces:**
- Consumes: `Status`, `InstanceStatus` (existing types).
- Produces: Contracts › Capacity.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/capacity-bar.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { InstanceStatus, Status } from "@/api/types"
import { capacity } from "@/lib/capacity"
import { fixtures } from "@/test/fixtures"
import { CapacityBar, CapacitySegments } from "./capacity-bar"

const inst = (state: string, id: string): InstanceStatus => ({
  id,
  repo: "darkmem",
  runner_name: `ghr-${id}`,
  state,
  since: "2026-10-03T14:00:00Z",
})
const status = (over: Partial<Status>): Status => ({ ...fixtures.status, ...over })
const threeBusy = status({ global_max: 2, instances: [inst("busy", "a"), inst("busy", "b"), inst("busy", "c")] })

describe("capacity", () => {
  it.each([
    [
      "queue mode",
      status({ global_max: 4, instances: [inst("busy", "a"), inst("busy", "b"), inst("idle", "c")] }),
      ["busy", "busy", "warm", "free"],
      "2 of 4 busy",
      "2 of 4 busy, 1 warm, 1 free",
    ],
    [
      "starting, with cleaning left out",
      status({ global_max: 4, instances: [inst("starting", "a"), inst("cleaning", "b"), inst("busy", "c")] }),
      ["busy", "starting", "free", "free"],
      "1 of 4 busy",
      "1 of 4 busy, 1 starting, 2 free",
    ],
    ["above the max", threeBusy, ["busy", "busy", "busy"], "3 busy, max 2", "3 busy, max 2"],
    [
      "all mode",
      status({ mode: "all", instances: [inst("busy", "a"), inst("idle", "b")] }),
      ["busy", "warm"],
      "1 busy",
      "1 busy, 1 warm",
    ],
  ])("%s", (_name, st, segments, text, label) => {
    const c = capacity(st)
    expect(c.segments).toEqual(segments)
    expect(c.text).toBe(text)
    expect(c.label).toBe(label)
  })

  it("counts the segments above the max", () => {
    expect(capacity(threeBusy).over).toBe(1)
    expect(capacity(fixtures.status).over).toBe(0)
  })
})

describe("CapacityBar", () => {
  it("draws one segment per runner and free slot, with the count", () => {
    render(<CapacityBar status={fixtures.status} />)
    expect(screen.getByText("Runners")).toBeInTheDocument()
    expect(screen.getByText("1 of 2 busy")).toHaveClass("font-mono")
    const bar = screen.getByRole("img", { name: "1 of 2 busy, 1 warm" })
    expect([...bar.children].map((c) => c.getAttribute("data-kind"))).toEqual(["busy", "warm"])
    expect(bar.children[0]).toHaveClass("ghr-pulse")
    expect(bar.children[1]).not.toHaveClass("ghr-pulse")
  })

  it("sets the segments above the max apart", () => {
    const { container } = render(<CapacitySegments status={threeBusy} />)
    const segments = container.querySelectorAll("[data-kind]")
    expect(segments[2]).toHaveClass("ml-[2px]")
    expect(segments[1]).not.toHaveClass("ml-[2px]")
  })

  it("draws free slots in the shell colour on the rail and in sunk on a card", () => {
    const free = status({ global_max: 3, instances: [inst("busy", "a")] })
    const { container, rerender } = render(<CapacitySegments status={free} tone="rail" />)
    expect(container.querySelector('[data-kind="free"]')).toHaveClass("bg-[hsl(var(--surface-sidebar))]")
    rerender(<CapacitySegments status={free} />)
    expect(container.querySelector('[data-kind="free"]')).toHaveClass("bg-muted")
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `npx vitest run src/components/capacity-bar.test.tsx`
Expected: FAIL with `Failed to resolve import "@/lib/capacity"`.

- [ ] **Step 3: Write the capacity model**

Create `web/src/lib/capacity.ts`:

```ts
import type { Status } from "@/api/types"

export type SegmentKind = "busy" | "starting" | "warm" | "free"

export interface Capacity {
  segments: SegmentKind[]
  over: number
  busy: number
  text: string
  label: string
}

// A cleaning instance no longer counts against global_max (sched.go), so it
// has no segment.
const KIND: Record<string, SegmentKind | undefined> = { busy: "busy", starting: "starting", idle: "warm" }
const ORDER: SegmentKind[] = ["busy", "starting", "warm"]

export function capacity(status: Status): Capacity {
  const live = status.instances.flatMap((i) => {
    const kind = KIND[i.state]
    return kind ? [kind] : []
  })
  const count = (kind: SegmentKind) => live.filter((k) => k === kind).length
  const counted = ORDER.flatMap((kind) => Array<SegmentKind>(count(kind)).fill(kind))
  const busy = count("busy")
  const extra = [count("starting") > 0 && `${count("starting")} starting`, count("warm") > 0 && `${count("warm")} warm`]
  if (status.mode === "all") {
    const text = `${busy} busy`
    return { segments: counted, over: 0, busy, text, label: [text, ...extra].filter(Boolean).join(", ") }
  }
  const max = status.global_max
  const free = Math.max(0, max - counted.length)
  const over = Math.max(0, counted.length - max)
  const text = over > 0 ? `${busy} busy, max ${max}` : `${busy} of ${max} busy`
  const label = [text, ...extra, free > 0 && `${free} free`].filter(Boolean).join(", ")
  return { segments: [...counted, ...Array<SegmentKind>(free).fill("free")], over, busy, text, label }
}
```

- [ ] **Step 4: Write the components**

Create `web/src/components/capacity-bar.tsx`:

```tsx
import type { Status } from "@/api/types"
import { capacity, type SegmentKind } from "@/lib/capacity"

// A busy runner's light is the one other authored motion (spec 1.4).
const KIND_CLASS: Record<Exclude<SegmentKind, "free">, string> = {
  busy: "ghr-pulse bg-primary",
  starting: "bg-primary/45",
  warm: "border border-primary",
}

export function CapacitySegments({
  status,
  size = "sm",
  tone = "card",
}: {
  status: Status
  size?: "sm" | "lg"
  tone?: "card" | "rail"
}) {
  const c = capacity(status)
  const free = tone === "rail" ? "bg-[hsl(var(--surface-sidebar))]" : "bg-muted"
  const firstOver = c.segments.length - c.over
  return (
    <div role="img" aria-label={c.label} className={`flex ${size === "lg" ? "h-3 gap-1.5" : "h-2 gap-1"}`}>
      {c.segments.map((kind, i) => (
        <span
          key={i}
          data-kind={kind}
          className={`min-w-1 flex-1 rounded-[2px] ${kind === "free" ? free : KIND_CLASS[kind]} ${c.over > 0 && i === firstOver ? "ml-[2px]" : ""}`}
        />
      ))}
    </div>
  )
}

export function CapacityBar({ status }: { status: Status }) {
  const c = capacity(status)
  return (
    <div className="flex flex-col gap-2 rounded-[6px] bg-[hsl(var(--sidebar-hover-bg))] p-2.5">
      <div className="ghr-rail-wide flex items-baseline justify-between gap-2 text-sm">
        <span className="text-[hsl(var(--sidebar-foreground-muted))]">Runners</span>
        <span className="font-mono text-[hsl(var(--sidebar-foreground))]">{c.text}</span>
      </div>
      <CapacitySegments status={status} tone="rail" />
    </div>
  )
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `npx vitest run src/components/capacity-bar.test.tsx && npm run typecheck && npm run lint`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/capacity.ts web/src/components/capacity-bar.tsx web/src/components/capacity-bar.test.tsx
git commit -m "feat(web): draw runner capacity as segments"
```

---

### Task 9: Show the runner update card

**Files:**
- Modify: `web/src/lib/format.ts` (append `monthDay`, `plural`)
- Create: `web/src/components/update-card.tsx`
- Test: `web/src/lib/format.test.ts` (append), `web/src/components/update-card.test.tsx`

**Interfaces:**
- Consumes: `RunnerUpdate` (existing type), `api.queueRunnerUpdate`, `api.cancelRunnerUpdate`, `useNow`.
- Produces: Contracts › Formatting helpers; `UpdateCard({ update, offline })`.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Append to `web/src/lib/format.test.ts` (add `monthDay` and `plural` to its import from `./format`):

```ts
describe("monthDay", () => {
  it("names the month and day", () => {
    expect(monthDay(new Date("2026-10-03T14:05:00Z"))).toBe("Oct 3")
  })
})

describe("plural", () => {
  it("adds an s except for one", () => {
    expect(plural(1, "day")).toBe("1 day")
    expect(plural(0, "day")).toBe("0 days")
    expect(plural(6, "job")).toBe("6 jobs")
  })
})
```

Create `web/src/components/update-card.test.tsx`:

```tsx
import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it } from "vitest"
import type { RunnerUpdate } from "@/api/types"
import { DAY_MS } from "@/lib/duration"
import { monthDay } from "@/lib/format"
import { mockApi, noContent } from "@/test/api"
import { withQuery } from "@/test/query"
import { UpdateCard } from "./update-card"

function draw(update: RunnerUpdate, offline = false) {
  const { wrapper } = withQuery()
  const user = userEvent.setup()
  const view = render(<UpdateCard update={update} offline={offline} />, { wrapper })
  return { ...view, user }
}

// An hour past whole days, so the day count does not tip while the test runs.
const inDays = (days: number) => new Date(Date.now() + days * DAY_MS + 3_600_000).toISOString()

describe("UpdateCard", () => {
  it("stays hidden when there is nothing to do", () => {
    const { container } = draw({ installed: "2.338.0", latest: "2.338.0" })
    expect(container).toBeEmptyDOMElement()
  })

  it("asks to queue an update before the deadline", async () => {
    const { calls } = mockApi({ "POST /api/runner-update": () => noContent() })
    const deadline = inDays(20)
    const { user } = draw({ latest: "2.338.0", deadline })
    expect(screen.getByText(`2.338.0 required by ${monthDay(new Date(deadline))}, 20 days left`)).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "Runner update" })).toHaveClass("text-warning")
    await user.click(screen.getByRole("button", { name: "Queue update" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/runner-update")).toBe(true))
  })

  it("turns red within 7 days of the deadline", () => {
    draw({ latest: "2.338.0", deadline: inDays(6) })
    expect(screen.getByText(/, 6 days left$/)).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "Runner update" })).toHaveClass("text-destructive")
  })

  it("says when the deadline has passed", () => {
    draw({ latest: "2.338.0", deadline: inDays(-2) })
    expect(screen.getByText(/, overdue$/)).toBeInTheDocument()
  })

  it("offers to cancel a queued update", async () => {
    const { calls } = mockApi({ "DELETE /api/runner-update": () => noContent() })
    const { user } = draw({ latest: "2.338.0", queued: true, queued_at: "2026-10-03T14:02:00Z" })
    expect(screen.getByText("Queued since 14:02. Runners update between jobs.")).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Cancel update" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/runner-update")).toBe(true))
  })

  it("shows a running update with no action", () => {
    draw({ running: true, queued: true })
    expect(screen.getByText("Updating runners")).toBeInTheDocument()
    expect(screen.queryByRole("button")).toBeNull()
  })

  it("offers to try a failed update again", async () => {
    const { calls } = mockApi({ "POST /api/runner-update": () => noContent() })
    const { user } = draw({ latest: "2.338.0", deadline: inDays(20), last_outcome: "failed", last_error: "download failed: 502" })
    expect(screen.getByText("download failed: 502")).toHaveClass("text-destructive")
    await user.click(screen.getByRole("button", { name: "Try again" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST")).toBe(true))
  })

  it("disables its action while the daemon is unreachable", () => {
    draw({ latest: "2.338.0", deadline: inDays(20) }, true)
    expect(screen.getByRole("button", { name: "Queue update" })).toBeDisabled()
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `npx vitest run src/lib/format.test.ts src/components/update-card.test.tsx`
Expected: FAIL (`monthDay` is not exported; `./update-card` does not resolve).

- [ ] **Step 3: Add the formatting helpers**

Append to `web/src/lib/format.ts`:

```ts
const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]

export function monthDay(d: Date): string {
  return `${MONTHS[d.getMonth()] ?? ""} ${d.getDate()}`
}

export function plural(n: number, word: string): string {
  return `${n} ${word}${n === 1 ? "" : "s"}`
}
```

- [ ] **Step 4: Write the card**

Create `web/src/components/update-card.tsx`:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { TriangleAlert } from "lucide-react"
import { useId } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { RunnerUpdate } from "@/api/types"
import { DAY_MS } from "@/lib/duration"
import { hhmm, monthDay, plural } from "@/lib/format"
import { useNow } from "@/lib/use-now"

type UpdateState = "hidden" | "running" | "queued" | "failed" | "due"

function updateState(u: RunnerUpdate): UpdateState {
  if (u.running) return "running"
  if (u.queued) return "queued"
  if (!u.deadline) return "hidden"
  return u.last_outcome === "failed" ? "failed" : "due"
}

function deadlineText(u: RunnerUpdate, now: number): string {
  const deadline = Date.parse(u.deadline ?? "")
  const left = deadline - now
  const when = left <= 0 ? "overdue" : left < DAY_MS ? "less than a day left" : `${plural(Math.floor(left / DAY_MS), "day")} left`
  return `${u.latest ?? "A newer runner"} required by ${monthDay(new Date(deadline))}, ${when}`
}

export function UpdateCard({ update, offline }: { update: RunnerUpdate; offline: boolean }) {
  const queryClient = useQueryClient()
  const now = useNow()
  const titleId = useId()
  const act = useMutation({
    mutationFn: (cancel: boolean) => (cancel ? api.cancelRunnerUpdate() : api.queueRunnerUpdate()),
    onSuccess: (_data, cancel) => {
      toast.success(cancel ? "Runner update cancelled" : "Runner update queued")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.status }),
  })
  const state = updateState(update)
  if (state === "hidden") return null
  const urgent = update.deadline !== undefined && Date.parse(update.deadline) - now <= 7 * DAY_MS
  const locked = offline || act.isPending
  const queuedAt = update.queued_at ? new Date(update.queued_at) : new Date(now)
  return (
    <section
      aria-labelledby={titleId}
      className={`flex flex-col items-start gap-2 rounded-[10px] border bg-card p-3 text-sm text-card-foreground ${urgent ? "border-destructive" : "border-warning"}`}
    >
      <h2 id={titleId} className={`flex items-center gap-1.5 font-semibold ${urgent ? "text-destructive" : "text-warning"}`}>
        <TriangleAlert size={15} aria-hidden="true" />
        Runner update
      </h2>
      {state === "running" && (
        <p className="flex items-center gap-2">
          <Spinner />
          <span>Updating runners</span>
        </p>
      )}
      {state === "queued" && <p>{`Queued since ${hhmm(queuedAt)}. Runners update between jobs.`}</p>}
      {(state === "due" || state === "failed") && <p>{deadlineText(update, now)}</p>}
      {state === "failed" && update.last_error && <p className="text-destructive">{update.last_error}</p>}
      {state === "queued" && (
        <Button size="sm" variant="outline" disabled={locked} onClick={() => act.mutate(true)}>
          Cancel update
        </Button>
      )}
      {state === "due" && (
        <Button size="sm" disabled={locked} onClick={() => act.mutate(false)}>
          Queue update
        </Button>
      )}
      {state === "failed" && (
        <Button size="sm" disabled={locked} onClick={() => act.mutate(false)}>
          Try again
        </Button>
      )}
    </section>
  )
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `npx vitest run src/lib/format.test.ts src/components/update-card.test.tsx && npm run typecheck && npm run lint`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/format.ts web/src/lib/format.test.ts web/src/components/update-card.tsx web/src/components/update-card.test.tsx
git commit -m "feat(web): add the runner update card"
```

---

### Task 10: Add the colour mode control

**Files:**
- Create: `web/src/components/mode-control.tsx`
- Test: `web/src/components/mode-control.test.tsx`

**Interfaces:**
- Consumes: the pinned `themeConfig` (Task 6), `useTheme` from `darkraise-ui/theme`.
- Produces: `ModeControl()`.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/components/mode-control.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { TooltipProvider } from "darkraise-ui/components/tooltip"
import { ThemeProvider } from "darkraise-ui/theme"
import { beforeEach, describe, expect, it } from "vitest"
import { themeConfig } from "@/theme.config"
import { ModeControl } from "./mode-control"

function draw() {
  const user = userEvent.setup()
  render(
    <ThemeProvider config={themeConfig}>
      <TooltipProvider>
        <ModeControl />
      </TooltipProvider>
    </ThemeProvider>,
  )
  return user
}

describe("ModeControl", () => {
  beforeEach(() => localStorage.clear())

  it("starts on dark and saves the chosen mode", async () => {
    const user = draw()
    expect(screen.getByRole("radiogroup", { name: "Colour mode" })).toBeInTheDocument()
    expect(screen.getByRole("radio", { name: "Dark" })).toBeChecked()
    await user.click(screen.getByRole("radio", { name: "Light" }))
    expect(screen.getByRole("radio", { name: "Light" })).toBeChecked()
    expect(localStorage.getItem("mode")).toBe("light")
    expect(document.documentElement.getAttribute("data-mode")).toBe("light")
  })

  it("names each mode in a tooltip", async () => {
    const user = draw()
    await user.hover(screen.getByRole("radio", { name: "System" }))
    expect(await screen.findByRole("tooltip", {}, { timeout: 3000 })).toHaveTextContent("System")
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npx vitest run src/components/mode-control.test.tsx`
Expected: FAIL with `Failed to resolve import "./mode-control"`.

- [ ] **Step 3: Write the control**

Create `web/src/components/mode-control.tsx`:

```tsx
import { ToggleGroup, ToggleGroupItem } from "darkraise-ui/components/toggle-group"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { useTheme } from "darkraise-ui/theme"
import { Monitor, Moon, Sun } from "lucide-react"

const MODES = [
  { value: "dark", label: "Dark", Icon: Moon },
  { value: "light", label: "Light", Icon: Sun },
  { value: "system", label: "System", Icon: Monitor },
] as const

export function ModeControl() {
  const { mode, setMode } = useTheme()
  return (
    <ToggleGroup
      type="single"
      size="sm"
      variant="outline"
      value={mode}
      aria-label="Colour mode"
      onValueChange={(value) => {
        const next = MODES.find((m) => m.value === value)
        if (next) setMode(next.value)
      }}
    >
      {MODES.map(({ value, label, Icon }) => (
        <Tooltip key={value}>
          <TooltipTrigger asChild>
            <ToggleGroupItem value={value} aria-label={label}>
              <Icon size={15} aria-hidden="true" />
            </ToggleGroupItem>
          </TooltipTrigger>
          <TooltipContent>{label}</TooltipContent>
        </Tooltip>
      ))}
    </ToggleGroup>
  )
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `npx vitest run src/components/mode-control.test.tsx && npm run typecheck && npm run lint`
Expected: PASS. If the tooltip test cannot find `role="tooltip"`, read `node_modules/darkraise-ui/dist/components/tooltip/Tooltip.d.ts` and the rendered DOM for the content's role, and assert on what it renders; keep the assertion.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/mode-control.tsx web/src/components/mode-control.test.tsx
git commit -m "feat(web): add the dark, light and system control"
```

---

### Task 11: Rewire the app shell

**Files:**
- Modify: `web/src/components/shell.tsx` (replace)
- Delete: `web/src/components/status-chips.tsx`
- Test: `web/src/components/shell.test.tsx` (replace); copy updates in `web/src/pages/runner-detail.test.tsx:105`, `web/src/components/add-repo-dialog.test.tsx:44`, `web/src/components/install-dialog.test.tsx:77,93`, `web/src/pages/repository.test.tsx:159`, `web/src/pages/dashboard.test.tsx:8-16`

**Interfaces:**
- Consumes: `CapacityBar`, `capacity` (Task 8), `UpdateCard` (Task 9), `ModeControl` (Task 10), the `ghr-rail-wide` class and `--ghr-host` (Task 5).
- Produces: the shell every page renders in.

**Items:** 1

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 2 = 4

- [ ] **Step 1: Write the new shell tests**

Replace `web/src/components/shell.test.tsx` with:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { keys } from "@/api/hooks"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

function logoutRoutes(answer: () => Response) {
  let authenticated = true
  return authedRoutes({
    "GET /auth/state": () => ({ setup_required: false, authenticated }),
    "POST /auth/logout": () => {
      authenticated = false
      return answer()
    },
  })
}

describe("shell", () => {
  it("lists the pages in two groups", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    for (const label of ["Dashboard", "Runners", "History", "Repositories", "Toolchains", "Storage", "Settings"]) {
      expect((await screen.findAllByRole("link", { name: new RegExp(`^${label}`) })).length).toBeGreaterThan(0)
    }
    expect(screen.getByText("Operate")).toBeInTheDocument()
    expect(screen.getByText("Configure")).toBeInTheDocument()
  })

  it("counts live runners and configured repositories in the nav", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    await waitFor(() => expect(within(screen.getByRole("link", { name: /^Runners/ })).getByText("2")).toBeInTheDocument())
    await waitFor(() => expect(within(screen.getByRole("link", { name: /^Repositories/ })).getByText("2")).toBeInTheDocument())
  })

  it("shows reconnecting while status polls fail, until one succeeds", async () => {
    let fail = true
    mockApi(authedRoutes({ "GET /api/status": () => (fail ? json({ error: "connection refused" }, 502) : fixtures.status) }))
    renderApp("/")
    expect(await screen.findByText("Daemon unreachable: connection refused. Retrying.")).toBeInTheDocument()
    fail = false
    await waitFor(() => expect(screen.queryByText(/Daemon unreachable/)).toBeNull(), { timeout: 3000 })
  })

  it("shows why the daemon is degraded", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, degraded: true, degraded_reason: "GitHub rejected the token" } }))
    renderApp("/")
    expect(await screen.findByText("GitHub rejected the token. No new runners start until this clears.")).toBeInTheDocument()
  })

  it("refetches the config when the daemon restarts", async () => {
    let epoch = "e1"
    mockApi(authedRoutes({ "GET /api/status": () => ({ ...fixtures.status, epoch }) }))
    const { queryClient } = renderApp("/")
    const invalidate = vi.spyOn(queryClient, "invalidateQueries")
    await waitFor(() => expect(queryClient.getQueryData(keys.status)).toBeDefined())
    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: keys.config })
    epoch = "e2"
    await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: keys.config }), { timeout: 3000 })
  })

  it("keeps config, metrics and events polling on every page", async () => {
    const { calls } = mockApi(authedRoutes())
    renderApp("/storage")
    await screen.findByRole("heading", { name: "Storage" })
    await waitFor(() => {
      for (const path of ["/api/config", "/api/metrics", "/api/events"]) {
        expect(calls.some((c) => c.path === path)).toBe(true)
      }
    })
  })

  it("logs out from the rail", async () => {
    const { calls } = mockApi(logoutRoutes(noContent))
    const { user, router } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Log out" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(calls.some((c) => c.method === "POST" && c.path === "/auth/logout")).toBe(true)
  })

  it("lands on /login even when the logout request fails", async () => {
    const { calls } = mockApi(logoutRoutes(() => json({ error: "session already ended" }, 500)))
    const { user, router } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Log out" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(calls.some((c) => c.method === "POST" && c.path === "/auth/logout")).toBe(true)
  })

  it("logs out from the header's user menu past unsaved edits", async () => {
    mockApi(logoutRoutes(noContent))
    const { user, router } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    await user.click(screen.getByRole("button", { name: "O" }))
    await user.click(await screen.findByRole("menuitem", { name: "Log out" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(screen.queryByText("Unsaved changes")).toBeNull()
  })

  it("sends an expired session to /login", async () => {
    let authenticated = true
    mockApi(
      authedRoutes({
        "GET /auth/state": () => ({ setup_required: false, authenticated }),
        "GET /api/status": () => {
          authenticated = false
          return json({ error: "not logged in" }, 401)
        },
      }),
    )
    const { router } = renderApp("/")
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"), { timeout: 3000 })
    expect(screen.queryByText(/Daemon unreachable/)).toBeNull()
  })

  it("shows runner capacity and the update card on every page", async () => {
    mockApi(authedRoutes({ "GET /api/history": fixtures.history }))
    renderApp("/history")
    expect((await screen.findAllByText("1 of 2 busy")).length).toBeGreaterThan(0)
    expect(screen.getByRole("img", { name: "1 of 2 busy, 1 warm" })).toBeInTheDocument()
    expect(screen.getByText("Queued since 13:55. Runners update between jobs.")).toBeInTheDocument()
  })

  it("puts the capacity readout and the user menu in the header bar", async () => {
    mockApi(authedRoutes())
    const { container } = renderApp("/")
    await waitFor(() => {
      const header = container.querySelector<HTMLElement>(".dr-layout-header")
      if (!header) throw new Error("no header bar")
      expect(within(header).getByText("1 of 2 busy")).toBeInTheDocument()
      expect(within(header).getByRole("button", { name: "O" })).toBeInTheDocument()
    })
  })

  it("offers dark, light and system in place of the theme switcher", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByRole("radiogroup", { name: "Colour mode" })).toBeInTheDocument()
  })

  it("names the host for the brand label", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    await waitFor(() => expect(document.documentElement.style.getPropertyValue("--ghr-host")).toBe('"localhost"'))
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `npx vitest run src/components/shell.test.tsx`
Expected: FAIL: no "Operate" group, the old banner copy, no "Log out" button in the rail, no capacity bar.

- [ ] **Step 3: Rewrite the shell**

Replace `web/src/components/shell.tsx` with:

```tsx
import { useQueryClient } from "@tanstack/react-query"
import { Link, Outlet, useNavigate } from "@tanstack/react-router"
import { Alert, AlertDescription, AlertTitle } from "darkraise-ui/components/alert"
import { Button } from "darkraise-ui/components/button"
import { SidebarLayout, useBrandStore, type NavGroup } from "darkraise-ui/layout"
import { Boxes, GitBranch, HardDrive, HistoryIcon, LayoutDashboard, LogOut, Settings, Wrench } from "lucide-react"
import { useEffect, useRef } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useConfig, useEvents, useMetrics, useStatus } from "@/api/hooks"
import type { Config, Status } from "@/api/types"
import { CapacityBar } from "@/components/capacity-bar"
import { ModeControl } from "@/components/mode-control"
import { UpdateCard } from "@/components/update-card"
import { capacity } from "@/lib/capacity"
import { running } from "@/lib/status"
import { errorText } from "@/query"

function count(n: number | undefined): string | undefined {
  return n ? String(n) : undefined
}

function navGroups(status: Status | undefined, config: Config | undefined): NavGroup[] {
  return [
    {
      label: "Operate",
      items: [
        { label: "Dashboard", href: "/", icon: LayoutDashboard },
        { label: "Runners", href: "/runners", icon: Boxes, badge: count(status && running(status)) },
        { label: "History", href: "/history", icon: HistoryIcon },
      ],
    },
    {
      label: "Configure",
      items: [
        { label: "Repositories", href: "/repositories", icon: GitBranch, badge: count(config?.repos?.length) },
        { label: "Toolchains", href: "/toolchains", icon: Wrench },
        { label: "Storage", href: "/storage", icon: HardDrive },
        { label: "Settings", href: "/settings", icon: Settings },
      ],
    },
  ]
}

export function Shell() {
  const status = useStatus()
  const config = useConfig()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const epoch = status.data?.epoch
  const seenEpoch = useRef<string | undefined>(undefined)
  // Mounted here, not per page, so config, metrics and the event feed poll
  // on every page, and the Dashboard opens with them warm.
  useMetrics()
  useEvents(epoch)

  useEffect(() => {
    useBrandStore.getState().setAppName("ghr")
    // The kit's brand label holds only the app name; ghr-theme.css appends
    // the host from this property.
    document.documentElement.style.setProperty("--ghr-host", JSON.stringify(window.location.hostname))
  }, [])

  useEffect(() => {
    if (epoch === undefined) return
    if (seenEpoch.current !== undefined && seenEpoch.current !== epoch) {
      void queryClient.invalidateQueries({ queryKey: keys.config })
    }
    seenEpoch.current = epoch
  }, [epoch, queryClient])

  async function logout() {
    try {
      await api.logout()
    } catch {
      // a session that already ended still lands on the login page
    }
    queryClient.clear()
    await navigate({ to: "/login", ignoreBlocker: true })
  }

  const st = status.data
  const offline = status.isError
  const unreachable = status.isError && !(status.error instanceof ApiError && status.error.status === 401)
  return (
    <SidebarLayout
      nav={navGroups(st, config.data)}
      showThemeSwitcher={false}
      activeBar="ring"
      notificationSlot={null}
      headerSlot={st && <span className="font-mono text-sm">{capacity(st).text}</span>}
      navHeader={st && <CapacityBar status={st} />}
      navFooter={
        <div className="flex flex-col gap-3">
          {st && (
            <div className="ghr-rail-wide empty:hidden">
              <UpdateCard update={st.runner_update} offline={offline} />
            </div>
          )}
          <div className="ghr-rail-wide flex items-center gap-2 text-sm text-[hsl(var(--sidebar-foreground))]">
            <span className="flex-1 truncate">Owner</span>
            <ModeControl />
            <Button size="sm" variant="ghost" onClick={() => void logout()}>
              <LogOut size={15} aria-hidden="true" />
              Log out
            </Button>
          </div>
        </div>
      }
      user={{ name: "Owner", email: "ghr" }}
      onLogout={() => void logout()}
    >
      {unreachable && (
        <Alert variant="destructive" className="mb-4">
          <AlertTitle>Reconnecting</AlertTitle>
          <AlertDescription>{`Daemon unreachable: ${errorText(status.error)}. Retrying.`}</AlertDescription>
        </Alert>
      )}
      {st?.degraded && (
        <Alert variant="warning" className="mb-4">
          <AlertTitle>Degraded</AlertTitle>
          <AlertDescription>{`${st.degraded_reason ?? "The daemon is degraded"}. No new runners start until this clears.`}</AlertDescription>
        </Alert>
      )}
      {st?.setup_pending && (
        <Alert className="mb-4">
          <AlertTitle>First-run setup is not finished.</AlertTitle>
          <AlertDescription>
            <Link to="/setup" className="underline">
              Finish setup
            </Link>
          </AlertDescription>
        </Alert>
      )}
      <Outlet />
    </SidebarLayout>
  )
}
```

- [ ] **Step 4: Remove the status chips and update the copy other tests read**

```bash
git rm web/src/components/status-chips.tsx
```

Then make these edits:
- `web/src/pages/runner-detail.test.tsx:105`: `"daemon unreachable: connection refused — retrying"` becomes `"Daemon unreachable: connection refused. Retrying."`.
- `web/src/components/add-repo-dialog.test.tsx:44`, `web/src/components/install-dialog.test.tsx:77` and `:93`: `/daemon unreachable/` becomes `/Daemon unreachable/`.
- `web/src/pages/repository.test.tsx:159`: `{ name: "Runners" }` becomes `{ name: /^Runners/ }` (the nav link now carries the runner count).
- `web/src/pages/dashboard.test.tsx`: delete the whole `it("shows the status chips", …)` test (lines 8 to 16); the chips are gone.

- [ ] **Step 5: Run the suite to verify it passes**

Run: `npm test` (timeout 600000)
Expected: every test passes. A failure that names "Runners" or "Repositories" as a link means another test finds a nav link by exact name: switch it to the `^Label` pattern.

- [ ] **Step 6: Lint, type-check and commit**

Run: `npm run typecheck && npm run lint`
Expected: both pass.

```bash
git add -A web/src
git commit -m "feat(web): rebuild the shell around runner capacity"
```

---

### Task 12: Add the result icon and event list

**Files:**
- Create: `web/src/components/result-icon.tsx`, `web/src/components/event-list.tsx`
- Test: `web/src/components/result-icon.test.tsx`, `web/src/components/event-list.test.tsx`

**Interfaces:**
- Consumes: `GhrEvent` (existing type), `hhmm` (existing).
- Produces: `ResultIcon({ conclusion })`, `EventList({ events, limit? })`.

**Items:** 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/result-icon.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { ResultIcon } from "./result-icon"

describe("ResultIcon", () => {
  it.each([
    ["success", "Succeeded", "text-success"],
    ["failure", "Failed", "text-destructive"],
    ["cancelled", "Cancelled", "text-muted-foreground"],
    ["skipped", "Skipped", "text-muted-foreground"],
    ["unknown", "Failed", "text-destructive"],
  ])("draws %s", (conclusion, label, colour) => {
    render(<ResultIcon conclusion={conclusion} />)
    expect(screen.getByRole("img", { name: label })).toHaveClass(colour)
  })
})
```

Create `web/src/components/event-list.test.tsx`:

```tsx
import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { GhrEvent } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { EventList } from "./event-list"

const event = (seq: number, level = "info", msg = `event ${seq}`): GhrEvent => ({ seq, time: "2026-10-03T14:00:00Z", level, msg })

describe("EventList", () => {
  it("lists the newest first, with an icon for ok, warn and error only", () => {
    render(<EventList events={fixtures.events} />)
    const items = screen.getAllByRole("listitem")
    expect(items.map((li) => li.querySelector("time")?.textContent)).toEqual(["14:04", "14:03", "14:02"])
    expect(within(items[0] as HTMLElement).getByRole("img", { name: "Warning" })).toHaveClass("text-warning")
    expect(within(items[1] as HTMLElement).getByRole("img", { name: "OK" })).toHaveClass("text-success")
    expect(within(items[2] as HTMLElement).queryByRole("img")).toBeNull()
    expect(screen.getByText("runner aaaaaa started")).toBeInTheDocument()
    expect(screen.getByText("darkmem")).toHaveClass("text-muted-foreground")
  })

  it("marks errors", () => {
    render(<EventList events={[event(1, "error", "GitHub refused the token")]} />)
    expect(screen.getByRole("img", { name: "Error" })).toHaveClass("text-destructive")
  })

  it("shows the last 8", () => {
    render(<EventList events={Array.from({ length: 10 }, (_, i) => event(i + 1))} />)
    expect(screen.getAllByRole("listitem")).toHaveLength(8)
    expect(screen.getByText("event 10")).toBeInTheDocument()
    expect(screen.queryByText("event 2")).toBeNull()
  })

  it("says when there are none", () => {
    render(<EventList events={[]} />)
    expect(screen.getByText("No events yet.")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `npx vitest run src/components/result-icon.test.tsx src/components/event-list.test.tsx`
Expected: FAIL with `Failed to resolve import "./result-icon"` and `"./event-list"`.

- [ ] **Step 3: Write the components**

Create `web/src/components/result-icon.tsx`:

```tsx
import { Ban, Check, CircleSlash, X, type LucideIcon } from "lucide-react"

interface Result {
  label: string
  Icon: LucideIcon
  className: string
}

const FAILED: Result = { label: "Failed", Icon: X, className: "text-destructive" }

const RESULTS: Record<string, Result | undefined> = {
  success: { label: "Succeeded", Icon: Check, className: "text-success" },
  failure: FAILED,
  cancelled: { label: "Cancelled", Icon: Ban, className: "text-muted-foreground" },
  skipped: { label: "Skipped", Icon: CircleSlash, className: "text-muted-foreground" },
}

// Any other conclusion shows as failed, as the Dashboard has always shown it.
export function ResultIcon({ conclusion }: { conclusion: string }) {
  const { label, Icon, className } = RESULTS[conclusion] ?? FAILED
  return <Icon role="img" aria-label={label} size={15} className={`shrink-0 ${className}`} />
}
```

Create `web/src/components/event-list.tsx`:

```tsx
import { Check, TriangleAlert, X, type LucideIcon } from "lucide-react"
import type { GhrEvent } from "@/api/types"
import { hhmm } from "@/lib/format"

const LEVELS: Record<string, { label: string; Icon: LucideIcon; className: string } | undefined> = {
  ok: { label: "OK", Icon: Check, className: "text-success" },
  warn: { label: "Warning", Icon: TriangleAlert, className: "text-warning" },
  error: { label: "Error", Icon: X, className: "text-destructive" },
}

export function EventList({ events, limit = 8 }: { events: GhrEvent[]; limit?: number }) {
  const shown = events.slice(-limit).reverse()
  if (shown.length === 0) return <p className="py-2 text-sm text-muted-foreground">No events yet.</p>
  return (
    <ul className="flex flex-col text-sm">
      {shown.map((e) => {
        const level = LEVELS[e.level]
        return (
          <li key={e.seq} className="grid grid-cols-[3rem_1rem_minmax(0,1fr)] items-start gap-2 border-b border-border py-1.5 last:border-b-0">
            <time dateTime={e.time} className="font-mono text-muted-foreground">
              {hhmm(new Date(e.time))}
            </time>
            <span className="pt-0.5">{level && <level.Icon role="img" aria-label={level.label} size={15} className={level.className} />}</span>
            <span className="break-words">
              {e.repo && <span className="text-muted-foreground">{e.repo} </span>}
              <span>{e.msg}</span>
            </span>
          </li>
        )
      })}
    </ul>
  )
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `npx vitest run src/components/result-icon.test.tsx src/components/event-list.test.tsx && npm run typecheck && npm run lint`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/result-icon.tsx web/src/components/result-icon.test.tsx web/src/components/event-list.tsx web/src/components/event-list.test.tsx
git commit -m "feat(web): add result icons and the event list"
```

---

### Task 13: Write the Dashboard summary sentence

**Files:**
- Create: `web/src/lib/summary.ts`
- Test: `web/src/lib/summary.test.ts`

**Interfaces:**
- Consumes: `plural` (Task 9).
- Produces: `dashboardSummary(status: Status): string`.

**Items:** 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/summary.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import type { InstanceStatus, RepoStatus, Status } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { dashboardSummary } from "./summary"

const repo = (over: Partial<RepoStatus>): RepoStatus => ({ name: "darkmem", paused: false, max: 2, active: 0, queued: 0, ...over })
const inst = (state: string, id: string): InstanceStatus => ({ id, repo: "darkmem", runner_name: `ghr-${id}`, state, since: "2026-10-03T14:00:00Z" })
const st = (over: Partial<Status>): Status => ({ ...fixtures.status, instances: [], ...over })

describe("dashboardSummary", () => {
  it.each([
    ["the fixture", fixtures.status, "1 of 2 runners busy. 3 jobs are waiting in darkmem. old-repo: GitHub: not found."],
    ["nothing busy", st({ repos: [repo({})] }), "No runners busy."],
    ["all mode", st({ mode: "all", instances: [inst("busy", "a"), inst("busy", "b")], repos: [repo({})] }), "2 runners busy."],
    ["all paused", st({ repos: [repo({ paused: true }), repo({ name: "gone", removing: true })] }), "All repositories are paused."],
    [
      "paused while runners finish",
      st({ instances: [inst("busy", "a"), inst("busy", "b")], repos: [repo({ paused: true })] }),
      "All repositories are paused, and 2 runners are finishing.",
    ],
    ["one job waiting", st({ repos: [repo({ queued: 1 })] }), "No runners busy. 1 job is waiting in darkmem."],
    [
      "two repositories waiting",
      st({ repos: [repo({ queued: 2 }), repo({ name: "ghr", queued: 1 })] }),
      "No runners busy. 3 jobs are waiting in darkmem and ghr.",
    ],
    [
      "three repositories waiting",
      st({ repos: [repo({ queued: 1 }), repo({ name: "ghr", queued: 1 }), repo({ name: "darkcloud", queued: 1 })] }),
      "No runners busy. 3 jobs are waiting across 3 repositories.",
    ],
    [
      "two errors",
      st({ repos: [repo({ error: "GitHub: not found" }), repo({ name: "ghr", error: "GitHub: forbidden." })] }),
      "No runners busy. 2 repositories have errors.",
    ],
    ["an error ending in a period", st({ repos: [repo({ error: "GitHub: forbidden." })] }), "No runners busy. darkmem: GitHub: forbidden."],
    ["no repositories", st({ repos: [] }), "No runners busy."],
  ])("%s", (_name, status, sentence) => {
    expect(dashboardSummary(status)).toBe(sentence)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npx vitest run src/lib/summary.test.ts`
Expected: FAIL with `Failed to resolve import "./summary"`.

- [ ] **Step 3: Write the function**

Create `web/src/lib/summary.ts`:

```ts
import type { Status } from "@/api/types"
import { plural } from "./format"

function where(names: string[]): string {
  if (names.length === 1) return `in ${names[0] ?? ""}`
  if (names.length === 2) return `in ${names[0] ?? ""} and ${names[1] ?? ""}`
  return `across ${names.length} repositories`
}

// A degraded daemon has its own banner, so the sentence leaves it out.
export function dashboardSummary(status: Status): string {
  const busy = status.instances.filter((i) => i.state === "busy").length
  const live = status.repos.filter((r) => !r.removing)
  const parts: string[] = []
  if (live.length > 0 && live.every((r) => r.paused)) {
    parts.push(
      busy === 0
        ? "All repositories are paused."
        : `All repositories are paused, and ${plural(busy, "runner")} ${busy === 1 ? "is" : "are"} finishing.`,
    )
  } else if (busy === 0) {
    parts.push("No runners busy.")
  } else {
    parts.push(status.mode === "all" ? `${plural(busy, "runner")} busy.` : `${busy} of ${status.global_max} runners busy.`)
  }
  const waiting = status.repos.filter((r) => r.queued > 0)
  const queued = waiting.reduce((n, r) => n + r.queued, 0)
  if (queued > 0) parts.push(`${queued === 1 ? "1 job is" : `${queued} jobs are`} waiting ${where(waiting.map((r) => r.name))}.`)
  const errors = status.repos.filter((r) => r.error)
  const first = errors[0]
  if (errors.length === 1 && first) parts.push(`${first.name}: ${(first.error ?? "").replace(/\.$/, "")}.`)
  else if (errors.length > 1) parts.push(`${errors.length} repositories have errors.`)
  return parts.join(" ")
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `npx vitest run src/lib/summary.test.ts && npm run typecheck && npm run lint`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/summary.ts web/src/lib/summary.test.ts
git commit -m "feat(web): sum up the daemon in one sentence"
```

---

### Task 14: Build the stat cards

**Files:**
- Modify: `web/src/components/sparkline.tsx` (replace)
- Create: `web/src/components/stat-card.tsx`
- Test: `web/src/components/stat-card.test.tsx`, `web/src/components/components.test.tsx` (append to the `Sparkline` describe)

**Interfaces:**
- Consumes: `CapacitySegments`, `capacity` (Task 8); `series`, `fmtMem` (existing).
- Produces: `Sparkline` with `step` and `className`; `StatCard`, `StatCards({ status, metrics, now })`.

**Items:** 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Append inside `describe("Sparkline", …)` in `web/src/components/components.test.tsx`:

```tsx
  it("draws steps when asked, in the given colour", () => {
    const { container } = render(<Sparkline label="queued" values={[1, 3]} step className="text-warning" />)
    expect(container.querySelector("polyline")?.getAttribute("points")?.split(" ")).toHaveLength(3)
    expect(screen.getByRole("img", { name: "queued" })).toHaveClass("text-warning")
  })
```

Create `web/src/components/stat-card.test.tsx`:

```tsx
import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { fixtures } from "@/test/fixtures"
import { StatCards } from "./stat-card"

const now = Date.parse("2026-10-03T14:05:00Z")
const card = (name: string) => within(screen.getByRole("region", { name }))

describe("StatCards", () => {
  it("shows runners, waiting jobs, the API budget and the host", () => {
    render(<StatCards status={fixtures.status} metrics={fixtures.metrics} now={now} />)
    expect(card("Runners").getByText("1")).toHaveClass("font-mono")
    expect(card("Runners").getByText("of 2 busy")).toBeInTheDocument()
    expect(card("Runners").getByRole("img", { name: "1 of 2 busy, 1 warm" })).toBeInTheDocument()
    expect(card("Waiting jobs").getByText("3")).toBeInTheDocument()
    expect(card("Waiting jobs").getByText("oldest 3 min")).toBeInTheDocument()
    expect(card("Waiting jobs").getByRole("img", { name: "Waiting jobs over the last hour" })).toHaveClass("text-warning")
    expect(card("API budget").getByText("4,980")).toBeInTheDocument()
    expect(card("API budget").getByText("of 5,000")).toBeInTheDocument()
    expect(card("API budget").getByLabelText("Remaining API budget")).toHaveAttribute("aria-valuenow", "4980")
    expect(card("Host").getByText("31%")).toBeInTheDocument()
    expect(card("Host").getByText("CPU, 3.0G of 16.0G memory")).toBeInTheDocument()
    expect(card("Host").getByRole("img", { name: "CPU over the last hour" })).toBeInTheDocument()
  })

  it("reads plain busy in all mode", () => {
    render(<StatCards status={{ ...fixtures.status, mode: "all" }} metrics={fixtures.metrics} now={now} />)
    expect(card("Runners").getByText("busy")).toBeInTheDocument()
  })

  it("says the API budget is not measured until GitHub answers", () => {
    render(<StatCards status={{ ...fixtures.status, rate_limit: undefined }} metrics={fixtures.metrics} now={now} />)
    expect(card("API budget").getByText("Not measured yet")).toBeInTheDocument()
    expect(card("API budget").queryByLabelText("Remaining API budget")).toBeNull()
  })

  it("waits for host metrics", () => {
    render(<StatCards status={fixtures.status} metrics={undefined} now={now} />)
    expect(card("Host").getByText("Not measured yet")).toBeInTheDocument()
  })

  it("leaves out the oldest age when nothing waits", () => {
    const repos = fixtures.status.repos.map((r) => ({ ...r, queued: 0, oldest_queued_at: undefined }))
    render(<StatCards status={{ ...fixtures.status, repos }} metrics={fixtures.metrics} now={now} />)
    expect(card("Waiting jobs").queryByText(/oldest/)).toBeNull()
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `npx vitest run src/components/stat-card.test.tsx src/components/components.test.tsx`
Expected: FAIL (`./stat-card` does not resolve; the step test sees 2 points and no `text-warning`).

- [ ] **Step 3: Extend the sparkline**

Replace `web/src/components/sparkline.tsx` with:

```tsx
const W = 120
const H = 32

export function Sparkline({
  values,
  max,
  label,
  step = false,
  className = "text-primary",
}: {
  values: (number | null)[]
  max?: number
  label: string
  step?: boolean
  className?: string
}) {
  const nums = values.filter((v): v is number => v !== null)
  const top = max ?? Math.max(1, ...nums)
  const dx = values.length > 1 ? W / (values.length - 1) : W
  const runs: [number, number][][] = [[]]
  values.forEach((v, i) => {
    const run = runs.at(-1) ?? []
    if (v === null) {
      if (run.length > 0) runs.push([])
      return
    }
    const x = i * dx
    const y = H - 2 - (Math.min(v, top) / top) * (H - 4)
    const last = run.at(-1)
    if (step && last) run.push([x, last[1]])
    run.push([x, y])
  })
  return (
    <svg role="img" aria-label={label} viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" className={`mt-2 h-8 w-full ${className}`}>
      {runs
        .filter((r) => r.length > 0)
        .map((r, i) => (
          <polyline
            key={i}
            fill="none"
            stroke="currentColor"
            strokeWidth="1.5"
            vectorEffect="non-scaling-stroke"
            points={r.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(" ")}
          />
        ))}
    </svg>
  )
}
```

- [ ] **Step 4: Write the cards**

Create `web/src/components/stat-card.tsx`:

```tsx
import type { ReactNode } from "react"
import type { Metrics, Status } from "@/api/types"
import { CapacitySegments } from "@/components/capacity-bar"
import { Sparkline } from "@/components/sparkline"
import { capacity } from "@/lib/capacity"
import { fmtMem, series } from "@/lib/format"

export function StatCard({ label, value, unit, children }: { label: string; value?: ReactNode; unit?: string; children?: ReactNode }) {
  return (
    <section aria-label={label} className="flex min-w-0 flex-col gap-1 rounded-[10px] border border-border bg-card p-3">
      <h2 className="text-sm text-muted-foreground">{label}</h2>
      <p className="flex flex-wrap items-baseline gap-x-1.5">
        {value !== undefined && <span className="font-mono text-2xl font-medium">{value}</span>}
        {unit && <span className="text-sm text-muted-foreground">{unit}</span>}
      </p>
      {children}
    </section>
  )
}

// The age moves in poll_interval steps: oldest_queued_at is only as fresh as
// the scheduler's last successful poll.
function oldestWaiting(status: Status, now: number): string | undefined {
  const times = status.repos.flatMap((r) => (r.oldest_queued_at ? [Date.parse(r.oldest_queued_at)] : []))
  if (times.length === 0) return undefined
  const minutes = Math.floor((now - Math.min(...times)) / 60_000)
  return minutes < 1 ? "oldest under 1 min" : `oldest ${minutes} min`
}

const count = (n: number) => n.toLocaleString("en-US")

export function StatCards({ status, metrics, now }: { status: Status; metrics: Metrics | undefined; now: number }) {
  const c = capacity(status)
  const samples = metrics?.samples ?? []
  const queued = status.repos.reduce((n, r) => n + r.queued, 0)
  const limit = status.rate_limit
  const cpu = metrics?.cpu
  const memory =
    metrics?.mem_used !== undefined && metrics.mem_total !== undefined ? `, ${fmtMem(metrics.mem_used)} of ${fmtMem(metrics.mem_total)} memory` : ""
  return (
    <div className="grid grid-cols-2 gap-3 min-[641px]:grid-cols-4">
      <StatCard label="Runners" value={c.busy} unit={status.mode === "all" ? "busy" : `of ${status.global_max} busy`}>
        <div className="mt-2">
          <CapacitySegments status={status} size="lg" />
        </div>
      </StatCard>
      <StatCard label="Waiting jobs" value={queued} unit={oldestWaiting(status, now)}>
        <Sparkline label="Waiting jobs over the last hour" values={series(samples, (s) => s.queued)} step className="text-warning" />
      </StatCard>
      <StatCard label="API budget" value={limit ? count(status.rate_remaining) : undefined} unit={limit ? `of ${count(limit)}` : "Not measured yet"}>
        {limit ? (
          <div
            role="meter"
            aria-label="Remaining API budget"
            aria-valuemin={0}
            aria-valuemax={limit}
            aria-valuenow={status.rate_remaining}
            className="mt-2 h-2 overflow-hidden rounded-[2px] bg-muted"
          >
            <div className="h-full bg-primary" style={{ width: `${Math.min(100, (status.rate_remaining / limit) * 100)}%` }} />
          </div>
        ) : null}
      </StatCard>
      <StatCard label="Host" value={cpu === undefined ? undefined : `${cpu.toFixed(0)}%`} unit={cpu === undefined ? "Not measured yet" : `CPU${memory}`}>
        <Sparkline label="CPU over the last hour" values={series(samples, (s) => s.cpu)} max={100} />
      </StatCard>
    </div>
  )
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `npx vitest run src/components/stat-card.test.tsx src/components/components.test.tsx src/pages/dashboard.test.tsx && npm run typecheck && npm run lint`
Expected: all pass (the old Dashboard still renders `Sparkline` without the new props).

- [ ] **Step 6: Commit**

```bash
git add web/src/components/sparkline.tsx web/src/components/stat-card.tsx web/src/components/stat-card.test.tsx web/src/components/components.test.tsx
git commit -m "feat(web): add the Dashboard stat cards"
```

---

### Task 15: Add the activity view helpers

**Files:**
- Create: `web/src/lib/activity-view.ts`
- Test: `web/src/lib/activity-view.test.ts`

**Interfaces:**
- Consumes: Contracts › API types; `dur`, `hhmm`, `monthDay`, `plural` from `web/src/lib/format.ts`.
- Produces: Contracts › Dashboard pieces › `web/src/lib/activity-view.ts` (every export listed there).

**Items:** 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/activity-view.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import type { ActivityBucket } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import {
  activitySummary,
  bucketAria,
  bucketTickLabel,
  isLaneWindow,
  laneTicks,
  lineRuns,
  rangeText,
  retentionText,
  runAria,
  runEnd,
  runLabel,
  scaleX,
  sinceText,
  windowWords,
} from "./activity-view"

const lanes = fixtures.activityLanes
const buckets = fixtures.activityBuckets
const now = Date.parse("2026-10-03T14:05:00Z")
const HOUR = 3_600_000

describe("activity view", () => {
  it("tells lane windows from bucket windows and words them", () => {
    expect(["1h", "3h", "24h", "7d", "30d"].map(isLaneWindow)).toEqual([true, true, false, false, false])
    expect(windowWords("1h")).toBe("the last hour")
    expect(windowWords("3h")).toBe("the last 3 hours")
    expect(windowWords("30d")).toBe("the last 30 days")
  })

  it.each([
    ["1h", "2026-10-03T13:05:00Z", "2026-10-03T14:05:00Z", "13:05 to 14:05"],
    ["24h", "2026-10-02T14:00:00Z", "2026-10-03T14:05:00Z", "Oct 2 14:00 to Oct 3 14:05, per hour"],
    ["7d", "2026-09-27T00:00:00Z", "2026-10-03T14:05:00Z", "Sep 27 to Oct 3, per 6 hours"],
    ["30d", "2026-09-04T00:00:00Z", "2026-10-03T14:05:00Z", "Sep 4 to Oct 3, per day"],
  ])("states the %s range", (window, from, to, text) => {
    expect(rangeText(window, from, to)).toBe(text)
  })

  it("scales time to a width and clamps it", () => {
    expect(scaleX(50, 0, 100, 200)).toBe(100)
    expect(scaleX(-5, 0, 100, 200)).toBe(0)
    expect(scaleX(150, 0, 100, 200)).toBe(200)
  })

  it("ticks every 10 minutes in 1h and every 30 in 3h", () => {
    expect(laneTicks(now - HOUR, now, "1h").map((t) => t.label)).toEqual(["13:10", "13:20", "13:30", "13:40", "13:50", "14:00"])
    expect(laneTicks(now - 3 * HOUR, now, "3h").map((t) => t.label)).toEqual(["11:30", "12:00", "12:30", "13:00", "13:30", "14:00"])
  })

  it.each([
    ["24h", "2026-10-02T18:00:00Z", "18:00"],
    ["24h", "2026-10-02T19:00:00Z", null],
    ["7d", "2026-09-28T00:00:00Z", "Sep 28"],
    ["7d", "2026-09-28T06:00:00Z", null],
    ["30d", "2026-09-28T00:00:00Z", "Sep 28"],
    ["30d", "2026-09-29T00:00:00Z", null],
  ])("labels a %s tick at %s", (window, start, label) => {
    expect(bucketTickLabel(window, start)).toBe(label)
  })

  it("describes each run", () => {
    const runs = lanes.lanes.flatMap((l) => l.runs)
    expect(runs.map((r) => runAria(r, now))).toEqual([
      "darkmem, ci, lint, run 40, failed, 6m00s",
      "darkmem, ci, build, run 41, succeeded, 5m00s",
      "darkmem, ci, test, run 42, running, 4m00s",
      "darkmem, runner bbbbbb, warm, waiting for a job, 2m00s",
    ])
    expect(runs.map(runLabel)).toEqual(["darkmem lint #40", "darkmem build #41", "darkmem test #42", ""])
    expect(runs.map((r) => runEnd(r, now))).toEqual([Date.parse("2026-10-03T13:21:00Z"), Date.parse("2026-10-03T13:40:00Z"), now, now])
  })

  it("describes each bucket", () => {
    const lastClosed = buckets.buckets[23] as ActivityBucket
    expect(bucketAria(lastClosed, buckets)).toBe(
      "13:00 to 14:00, 5 busy runner-minutes, 1 succeeded, 0 failed, 0 cancelled, at most 3 waiting, CPU 23%",
    )
    expect(bucketAria({ ...lastClosed, busy_pct: 8.3 }, { window: "24h", capacity: 2 })).toContain("8.3% busy against today's max of 2")
    const empty = bucketAria(buckets.buckets[0] as ActivityBucket, buckets)
    expect(empty).toContain("waiting not measured")
    expect(empty).toContain("CPU not measured")
  })

  it("sums up a window", () => {
    expect(activitySummary(lanes)).toBe("3 jobs in the last hour, 1 failed, 1 running.")
    expect(activitySummary(buckets)).toBe("2 jobs in the last 24 hours, 1 failed.")
    expect(activitySummary({ ...lanes, lanes: [] })).toBe("No jobs ran in the last hour.")
  })

  it("states retention and when metrics start", () => {
    expect(retentionText(lanes)).toBe("History is kept for 30 days")
    expect(retentionText({ ...lanes, history_from: "2026-10-03T00:05:00Z" })).toBe("History is kept for 14 hours")
    expect(sinceText("24h", "2026-10-03T13:00:00Z")).toBe("Collecting data since 13:00")
    expect(sinceText("7d", "2026-10-03T12:00:00Z")).toBe("Collecting data since Oct 3 12:00")
  })

  it("breaks lines at missing values", () => {
    const points = [
      { x: 0, v: 1 },
      { x: 1, v: null },
      { x: 2, v: 2 },
      { x: 3, v: 3 },
    ]
    expect(lineRuns(points, (v) => v * 10)).toEqual([[{ x: 0, y: 10 }], [{ x: 2, y: 20 }, { x: 3, y: 30 }]])
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npx vitest run src/lib/activity-view.test.ts`
Expected: FAIL with `Failed to resolve import "./activity-view"`.

- [ ] **Step 3: Write the helpers**

Create `web/src/lib/activity-view.ts`:

```ts
import type { Activity, ActivityBucket, ActivityRun, ActivityWindow } from "@/api/types"
import { dur, hhmm, monthDay, plural } from "./format"

export const ACTIVITY_WINDOWS: readonly ActivityWindow[] = ["1h", "3h", "24h", "7d", "30d"]

// Room on the left of every chart for lane and track labels.
export const GUTTER = 64
export const PAD_RIGHT = 8

const HOUR = 3_600_000
const JOB_STATES = new Set(["running", "succeeded", "failed", "cancelled", "skipped"])

const WORDS: Record<string, string | undefined> = {
  "1h": "the last hour",
  "3h": "the last 3 hours",
  "24h": "the last 24 hours",
  "7d": "the last 7 days",
  "30d": "the last 30 days",
}

const RESULT: Record<string, string | undefined> = {
  starting: "starting",
  warm: "warm, waiting for a job",
  running: "running",
  succeeded: "succeeded",
  failed: "failed",
  cancelled: "cancelled",
  skipped: "skipped",
}

export function isLaneWindow(w: string): boolean {
  return w === "1h" || w === "3h"
}

export function windowWords(w: string): string {
  return WORDS[w] ?? `the last ${w}`
}

export function rangeText(window: string, from: string, to: string): string {
  const f = new Date(from)
  const t = new Date(to)
  if (isLaneWindow(window)) return `${hhmm(f)} to ${hhmm(t)}`
  if (window === "24h") return `${monthDay(f)} ${hhmm(f)} to ${monthDay(t)} ${hhmm(t)}, per hour`
  return `${monthDay(f)} to ${monthDay(t)}, ${window === "7d" ? "per 6 hours" : "per day"}`
}

export function scaleX(t: number, from: number, to: number, width: number): number {
  if (to <= from) return 0
  return Math.min(width, Math.max(0, ((t - from) / (to - from)) * width))
}

export function laneTicks(from: number, to: number, window: string): { at: number; label: string }[] {
  const step = window === "3h" ? 30 : 10
  const first = new Date(from)
  first.setSeconds(0, 0)
  first.setMinutes(Math.ceil(first.getMinutes() / step) * step)
  const ticks: { at: number; label: string }[] = []
  for (let t = first.getTime(); t <= to; t += step * 60_000) {
    if (t >= from) ticks.push({ at: t, label: hhmm(new Date(t)) })
  }
  return ticks
}

export function bucketTickLabel(window: string, start: string): string | null {
  const d = new Date(start)
  if (window === "24h") return d.getHours() % 6 === 0 ? hhmm(d) : null
  if (window === "7d") return d.getHours() === 0 ? monthDay(d) : null
  if (window === "30d") return d.getDay() === 1 ? monthDay(d) : null
  return null
}

export function runEnd(run: ActivityRun, now: number): number {
  const to = run.segments.at(-1)?.to
  return to === undefined || to === null ? now : Date.parse(to)
}

export function runLabel(run: ActivityRun): string {
  if (!run.job) return ""
  return [run.repo, run.job, run.run_number && `#${run.run_number}`].filter(Boolean).join(" ")
}

export function runAria(run: ActivityRun, now: number): string {
  const last = run.segments.at(-1)
  const job = [...run.segments].reverse().find((s) => JOB_STATES.has(s.state)) ?? last
  const ms = job ? (job.to === null ? now : Date.parse(job.to)) - Date.parse(job.from) : 0
  const who = run.job ? [run.repo, run.workflow, run.job, run.run_number && `run ${run.run_number}`] : [run.repo, `runner ${run.instance_id}`]
  const state = last ? (RESULT[last.state] ?? last.state) : ""
  return [...who, state, dur(ms)].filter(Boolean).join(", ")
}

function bucketRange(window: string, b: ActivityBucket): string {
  const s = new Date(b.start)
  const e = new Date(b.end)
  if (window === "24h") return `${hhmm(s)} to ${hhmm(e)}`
  if (window === "7d") return `${monthDay(s)} ${hhmm(s)} to ${hhmm(e)}`
  return monthDay(s)
}

// busy_pct divides by today's global_max, not the cap in force then.
export function bucketAria(b: ActivityBucket, a: { window: string; capacity: number | null }): string {
  const busy =
    a.capacity === null ? `${Math.round(b.busy_minutes)} busy runner-minutes` : `${b.busy_pct ?? 0}% busy against today's max of ${a.capacity}`
  return [
    bucketRange(a.window, b),
    busy,
    `${b.succeeded} succeeded`,
    `${b.failed} failed`,
    `${b.cancelled} cancelled`,
    b.waiting_max === null ? "waiting not measured" : `at most ${b.waiting_max} waiting`,
    b.cpu_avg === null ? "CPU not measured" : `CPU ${Math.round(b.cpu_avg)}%`,
  ].join(", ")
}

export function activitySummary(a: Activity): string {
  const words = windowWords(a.window)
  if (isLaneWindow(a.window)) {
    const jobs = a.lanes.flatMap((l) => l.runs).filter((r) => r.segments.some((s) => JOB_STATES.has(s.state)))
    if (jobs.length === 0) return `No jobs ran in ${words}.`
    const failed = jobs.filter((r) => r.segments.at(-1)?.state === "failed").length
    const running = jobs.filter((r) => r.segments.some((s) => s.state === "running" && s.to === null)).length
    return `${plural(jobs.length, "job")} in ${words}, ${failed} failed, ${running} running.`
  }
  const total = a.buckets.reduce((n, b) => n + b.succeeded + b.failed + b.cancelled, 0)
  if (total === 0) return `No jobs ran in ${words}.`
  const failed = a.buckets.reduce((n, b) => n + b.failed, 0)
  return `${plural(total, "job")} in ${words}, ${failed} failed.`
}

// Pruning runs once a day, so the edge this describes is a guide.
export function retentionText(a: Activity): string {
  const hours = Math.round((Date.parse(a.to) - Date.parse(a.history_from)) / HOUR)
  return hours % 24 === 0 ? `History is kept for ${plural(hours / 24, "day")}` : `History is kept for ${plural(hours, "hour")}`
}

export function sinceText(window: string, iso: string): string {
  const d = new Date(iso)
  return `Collecting data since ${window === "24h" ? hhmm(d) : `${monthDay(d)} ${hhmm(d)}`}`
}

export function lineRuns(points: { x: number; v: number | null }[], y: (v: number) => number): { x: number; y: number }[][] {
  const runs: { x: number; y: number }[][] = []
  let current: { x: number; y: number }[] = []
  for (const p of points) {
    if (p.v === null) {
      if (current.length > 0) runs.push(current)
      current = []
      continue
    }
    current.push({ x: p.x, y: y(p.v) })
  }
  if (current.length > 0) runs.push(current)
  return runs
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `npx vitest run src/lib/activity-view.test.ts && npm run typecheck && npm run lint`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/activity-view.ts web/src/lib/activity-view.test.ts
git commit -m "feat(web): add the activity chart helpers"
```

---

### Task 16: Draw the activity lanes

**Files:**
- Create: `web/src/components/lanes-chart.tsx`
- Test: `web/src/components/lanes-chart.test.tsx`

**Interfaces:**
- Consumes: Contracts › API types; the `activity-view` helpers (Task 15); `fmtMem`; the `ghr-mark`, `ghr-mark-box` and `ghr-pulse` classes (Task 5).
- Produces: `LanesChart({ activity, now, width, onOpenRunner })`.

**Items:** 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/components/lanes-chart.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import type { Activity } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { LanesChart } from "./lanes-chart"

const now = Date.parse("2026-10-03T14:05:00Z")

function draw(over: Partial<Activity> = {}, at = now) {
  const onOpenRunner = vi.fn()
  const view = render(<LanesChart activity={{ ...fixtures.activityLanes, ...over }} now={at} width={960} onOpenRunner={onOpenRunner} />)
  return { ...view, onOpenRunner }
}

const runningWidth = (container: HTMLElement) => Number(container.querySelector('rect[data-state="running"]')?.getAttribute("width"))

describe("LanesChart", () => {
  it("labels each lane and the time axis", () => {
    draw()
    expect(screen.getByText("Lane 1")).toBeInTheDocument()
    expect(screen.getByText("Lane 2")).toBeInTheDocument()
    expect(screen.getByText("13:10")).toBeInTheDocument()
    expect(screen.getByText("14:00")).toBeInTheDocument()
  })

  it("draws each segment state", () => {
    const { container } = draw()
    for (const state of ["failed", "succeeded", "running", "warm"]) {
      expect(container.querySelector(`rect[data-state="${state}"]`)).not.toBeNull()
    }
    expect(container.querySelector('rect[data-state="warm"]')).toHaveAttribute("stroke-dasharray", "3 2")
    expect(container.querySelector(".ghr-pulse")).not.toBeNull()
  })

  it("links a finished job to GitHub", () => {
    draw()
    const link = screen.getByRole("link", { name: "darkmem, ci, build, run 41, succeeded, 5m00s" })
    expect(link).toHaveAttribute("href", "https://github.com/darkraise/darkmem/actions/runs/101/job/1")
    expect(link).toHaveAttribute("target", "_blank")
  })

  it("opens the runner page from a live run", async () => {
    const { onOpenRunner } = draw()
    await userEvent.setup().click(screen.getByRole("link", { name: "darkmem, ci, test, run 42, running, 4m00s" }))
    expect(onOpenRunner).toHaveBeenCalledWith("aaaaaa")
  })

  it("keeps a finished run without a GitHub link focusable but not a link", () => {
    draw()
    const name = "darkmem, ci, lint, run 40, failed, 6m00s"
    expect(screen.getByRole("img", { name })).toHaveAttribute("tabindex", "0")
    expect(screen.queryByRole("link", { name })).toBeNull()
  })

  it("extends a running bar to now", () => {
    const { container, rerender } = draw()
    const before = runningWidth(container)
    rerender(<LanesChart activity={fixtures.activityLanes} now={now + 60_000} width={960} onOpenRunner={vi.fn()} />)
    expect(runningWidth(container)).toBeGreaterThan(before)
  })

  it("draws waiting jobs, CPU and a dashed memory line, with gaps", () => {
    const { container } = draw()
    expect(container.querySelector('path[data-track="waiting"]')).not.toBeNull()
    expect(container.querySelectorAll('polyline[data-track="cpu"]')).toHaveLength(1)
    expect(container.querySelector('polyline[data-track="mem"]')).toHaveAttribute("stroke-dasharray", "3 2")
    expect(screen.getByText("peak 3")).toBeInTheDocument()
    expect(screen.getByText("peak 48%")).toBeInTheDocument()
  })

  it("says when no jobs ran", () => {
    draw({ lanes: [] })
    expect(screen.getByText("No jobs ran in the last hour.")).toBeInTheDocument()
    expect(screen.getByText("Lane 1")).toBeInTheDocument()
  })

  it("shades the time before history starts", () => {
    const { container } = draw({ history_from: "2026-10-03T13:35:00Z" })
    expect(container.querySelector('[data-retention="true"]')).not.toBeNull()
    expect(screen.getByText(/^History is kept for/)).toBeInTheDocument()
  })

  it("marks now", () => {
    const { container } = draw()
    expect(container.querySelector('line[data-now="true"]')).toHaveAttribute("x1", String(960 - 8))
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npx vitest run src/components/lanes-chart.test.tsx`
Expected: FAIL with `Failed to resolve import "./lanes-chart"`.

- [ ] **Step 3: Write the chart**

Create `web/src/components/lanes-chart.tsx`:

```tsx
import type { Activity, ActivityRun } from "@/api/types"
import { GUTTER, laneTicks, lineRuns, PAD_RIGHT, retentionText, runAria, runEnd, runLabel, scaleX, windowWords } from "@/lib/activity-view"
import { fmtMem } from "@/lib/format"

const LANE_H = 20
const LANE_GAP = 6
const TRACK_H = 36
const TRACK_GAP = 16
const AXIS_H = 18
const MINUTE = 60_000

const SEGMENT: Record<string, string | undefined> = {
  starting: "fill-none stroke-muted-foreground",
  warm: "fill-none stroke-primary",
  running: "fill-primary stroke-primary",
  succeeded: "fill-success/30 stroke-success/60",
  failed: "fill-destructive/30 stroke-destructive/70",
  cancelled: "fill-muted stroke-muted-foreground",
  skipped: "fill-muted stroke-muted-foreground",
}

type Point = { x: number; y: number }
const points = (run: Point[]) => run.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ")

function RunMark({
  run,
  top,
  x,
  now,
  onOpenRunner,
}: {
  run: ActivityRun
  top: number
  x: (t: number) => number
  now: number
  onOpenRunner: (id: string) => void
}) {
  const label = runAria(run, now)
  const last = run.segments.at(-1)
  const first = run.segments[0]
  const startX = first ? x(Date.parse(first.from)) : 0
  const endX = x(runEnd(run, now))
  const text = runLabel(run)
  const fits = text !== "" && endX - startX > text.length * 6.5 + 8
  const body = (
    <>
      <title>{label}</title>
      {run.segments.map((s, i) => {
        const x1 = x(Date.parse(s.from))
        const x2 = x(s.to === null ? now : Date.parse(s.to))
        return (
          <rect
            key={i}
            data-state={s.state}
            x={x1}
            y={top + 0.5}
            width={Math.max(2, x2 - x1)}
            height={LANE_H - 1}
            rx={2}
            strokeWidth={1}
            strokeDasharray={s.state === "warm" ? "3 2" : undefined}
            className={`ghr-mark-box ${SEGMENT[s.state] ?? "fill-muted stroke-muted-foreground"}`}
          />
        )
      })}
      {last?.state === "running" && last.to === null && (
        <rect x={endX - 3} y={top + 0.5} width={3} height={LANE_H - 1} className="ghr-pulse fill-foreground/70" />
      )}
      {fits && (
        <text
          x={startX + 4}
          y={top + LANE_H / 2 + 4}
          className={`pointer-events-none ${last?.state === "running" ? "fill-primary-foreground" : "fill-foreground"}`}
        >
          {text}
        </text>
      )}
    </>
  )
  if (last?.to === null) {
    return (
      <a
        href={`/runners/${encodeURIComponent(run.instance_id)}`}
        aria-label={label}
        className="ghr-mark"
        onClick={(e) => {
          e.preventDefault()
          onOpenRunner(run.instance_id)
        }}
      >
        {body}
      </a>
    )
  }
  if (run.html_url) {
    return (
      <a href={run.html_url} target="_blank" rel="noreferrer" aria-label={label} className="ghr-mark">
        {body}
      </a>
    )
  }
  return (
    <g tabIndex={0} role="img" aria-label={label} className="ghr-mark">
      {body}
    </g>
  )
}

// The time axis ends at now and slides each second, so running bars grow
// between polls.
export function LanesChart({
  activity,
  now,
  width,
  onOpenRunner,
}: {
  activity: Activity
  now: number
  width: number
  onOpenRunner: (id: string) => void
}) {
  const from = now - (Date.parse(activity.to) - Date.parse(activity.from))
  const plotW = width - GUTTER - PAD_RIGHT
  const x = (t: number) => GUTTER + scaleX(t, from, now, plotW)
  const laneCount = Math.max(1, activity.lanes.length)
  const laneTop = (i: number) => i * (LANE_H + LANE_GAP)
  const lanesH = laneTop(laneCount) - LANE_GAP
  const waitTop = lanesH + TRACK_GAP
  const cpuTop = waitTop + TRACK_H + TRACK_GAP
  const axisTop = cpuTop + TRACK_H
  const height = axisTop + AXIS_H
  const historyFrom = Date.parse(activity.history_from)
  const runCount = activity.lanes.reduce((n, l) => n + l.runs.length, 0)

  const waiting = activity.waiting.map((p) => ({ t: Date.parse(p.at), v: p.value }))
  const waitPeak = Math.max(0, ...waiting.map((p) => p.v))
  const waitY = (v: number) => waitTop + TRACK_H - (v / Math.max(1, waitPeak)) * TRACK_H
  const waitFirst = waiting[0]
  let waitPath = ""
  if (waitFirst) {
    const base = waitTop + TRACK_H
    waitPath = `M ${x(waitFirst.t)} ${base}`
    waiting.forEach((p, i) => {
      const next = waiting[i + 1]
      waitPath += ` V ${waitY(p.v)} H ${x(next ? next.t : Math.min(now, p.t + MINUTE))}`
    })
    waitPath += ` V ${base} Z`
  }

  const samples = activity.cpu.map((p) => ({ x: x(Date.parse(p.at)), cpu: p.cpu, mem: p.mem }))
  const cpuPeak = Math.max(0, ...samples.flatMap((p) => (p.cpu === null ? [] : [p.cpu])))
  const memPeak = Math.max(1, ...samples.flatMap((p) => (p.mem === null ? [] : [p.mem])))
  const cpuLines = lineRuns(
    samples.map((p) => ({ x: p.x, v: p.cpu })),
    (v) => cpuTop + TRACK_H - (Math.min(v, 100) / 100) * TRACK_H,
  )
  const memLines = lineRuns(
    samples.map((p) => ({ x: p.x, v: p.mem })),
    (v) => cpuTop + TRACK_H - (v / memPeak) * TRACK_H,
  )

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="group"
      aria-label={`Runner lanes for ${windowWords(activity.window)}`}
      className="block text-xs"
    >
      {Array.from({ length: laneCount }, (_, i) => (
        <g key={i}>
          <text x={0} y={laneTop(i) + LANE_H / 2 + 4} className="fill-muted-foreground font-mono">
            {`Lane ${i + 1}`}
          </text>
          <rect x={GUTTER} y={laneTop(i)} width={plotW} height={LANE_H} rx={3} className="fill-muted/50" />
        </g>
      ))}
      {laneTicks(from, now, activity.window).map((t) => (
        <g key={t.at}>
          <line x1={x(t.at)} x2={x(t.at)} y1={0} y2={axisTop} className="stroke-border" />
          <text x={x(t.at)} y={axisTop + 13} textAnchor="middle" className="fill-muted-foreground font-mono">
            {t.label}
          </text>
        </g>
      ))}
      {historyFrom > from && (
        <g data-retention="true">
          <rect x={GUTTER} y={0} width={x(historyFrom) - GUTTER} height={axisTop} className="fill-muted" />
          <text x={GUTTER + 6} y={12} className="fill-muted-foreground">
            {retentionText(activity)}
          </text>
        </g>
      )}
      {activity.lanes.map((lane, i) =>
        lane.runs.map((run) => <RunMark key={run.instance_id} run={run} top={laneTop(i)} x={x} now={now} onOpenRunner={onOpenRunner} />),
      )}
      {runCount === 0 && (
        <text x={GUTTER + plotW / 2} y={lanesH / 2 + 4} textAnchor="middle" className="fill-muted-foreground">
          {`No jobs ran in ${windowWords(activity.window)}.`}
        </text>
      )}

      <text x={0} y={waitTop + 12} className="fill-muted-foreground">
        Waiting
      </text>
      {waitFirst && (
        <text x={0} y={waitTop + 26} className="fill-muted-foreground font-mono">
          {`peak ${waitPeak}`}
        </text>
      )}
      <rect x={GUTTER} y={waitTop} width={plotW} height={TRACK_H} className="fill-muted/30" />
      {waitPath && <path data-track="waiting" d={waitPath} strokeWidth={1} className="fill-warning/25 stroke-warning" />}

      <text x={0} y={cpuTop + 12} className="fill-muted-foreground">
        CPU
      </text>
      {cpuLines.length > 0 && (
        <text x={0} y={cpuTop + 26} className="fill-muted-foreground font-mono">
          {`peak ${Math.round(cpuPeak)}%`}
        </text>
      )}
      {memLines.length > 0 && (
        <text x={0} y={cpuTop + TRACK_H} className="fill-muted-foreground font-mono">
          {`mem ${fmtMem(memPeak)}`}
        </text>
      )}
      <rect x={GUTTER} y={cpuTop} width={plotW} height={TRACK_H} className="fill-muted/30" />
      {cpuLines.map((run, i) =>
        run.length > 1 ? (
          <polyline key={i} data-track="cpu" points={points(run)} strokeWidth={1.5} className="fill-none stroke-primary" />
        ) : (
          <circle key={i} data-track="cpu" cx={run[0]?.x} cy={run[0]?.y} r={1.5} className="fill-primary" />
        ),
      )}
      {memLines.map((run, i) =>
        run.length > 1 ? (
          <polyline key={i} data-track="mem" points={points(run)} strokeWidth={1} strokeDasharray="3 2" className="fill-none stroke-muted-foreground" />
        ) : (
          <circle key={i} data-track="mem" cx={run[0]?.x} cy={run[0]?.y} r={1.5} className="fill-muted-foreground" />
        ),
      )}

      <line data-now="true" x1={GUTTER + plotW} x2={GUTTER + plotW} y1={0} y2={axisTop} strokeWidth={1.5} className="stroke-primary" />
    </svg>
  )
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `npx vitest run src/components/lanes-chart.test.tsx && npm run typecheck && npm run lint`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/lanes-chart.tsx web/src/components/lanes-chart.test.tsx
git commit -m "feat(web): draw runner runs in lanes"
```

---

### Task 17: Draw the activity buckets

**Files:**
- Create: `web/src/components/buckets-chart.tsx`
- Test: `web/src/components/buckets-chart.test.tsx`

**Interfaces:**
- Consumes: Contracts › API types; the `activity-view` helpers (Task 15); the `ghr-mark` and `ghr-mark-box` classes (Task 5).
- Produces: `BucketsChart({ activity, now, width })`.

**Items:** 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/components/buckets-chart.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Activity } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { BucketsChart } from "./buckets-chart"

const now = Date.parse("2026-10-03T14:05:00Z")
const base = fixtures.activityBuckets

function draw(over: Partial<Activity> = {}) {
  return render(<BucketsChart activity={{ ...base, ...over }} now={now} width={960} />)
}

describe("BucketsChart", () => {
  it("labels each track with the peak it reaches", () => {
    draw()
    for (const track of ["Busy", "Runs", "Waiting", "CPU"]) expect(screen.getByText(track)).toBeInTheDocument()
    expect(screen.getByText("peak 10 min")).toBeInTheDocument()
    expect(screen.getByText("peak 1")).toBeInTheDocument()
    expect(screen.getByText("peak 3")).toBeInTheDocument()
    expect(screen.getByText("peak 23%")).toBeInTheDocument()
  })

  it("measures busy time against capacity when there is one", () => {
    const buckets = base.buckets.map((b) => ({ ...b, busy_pct: (b.busy_minutes / 120) * 100 }))
    draw({ capacity: 2, buckets })
    expect(screen.getByText("peak 8%")).toBeInTheDocument()
  })

  it("makes every column focusable with its numbers", () => {
    draw()
    expect(screen.getAllByRole("img")).toHaveLength(25)
    const column = screen.getByRole("img", {
      name: "13:00 to 14:00, 5 busy runner-minutes, 1 succeeded, 0 failed, 0 cancelled, at most 3 waiting, CPU 23%",
    })
    expect(column).toHaveAttribute("tabindex", "0")
  })

  it("draws the bucket in progress lighter", () => {
    const { container } = draw()
    const open = container.querySelectorAll('[data-open="true"]')
    expect(open).toHaveLength(1)
    expect(open[0]).toHaveClass("opacity-60")
  })

  it("stacks failed runs on succeeded runs", () => {
    const { container } = draw()
    expect(container.querySelectorAll('rect[data-kind="succeeded"]')).toHaveLength(1)
    expect(container.querySelectorAll('rect[data-kind="failed"]')).toHaveLength(1)
  })

  it("leaves gaps for missing metrics and says when they start", () => {
    const { container } = draw()
    expect(screen.getAllByText("Collecting data since 13:00")).toHaveLength(2)
    expect(container.querySelectorAll('circle[data-track="waiting"]')).toHaveLength(1)
    expect(container.querySelectorAll('polyline[data-track="waiting"]')).toHaveLength(0)
  })

  it("shades the time before history starts", () => {
    const { container } = draw({ history_from: "2026-10-03T00:05:00Z" })
    expect(container.querySelector('[data-retention="true"]')).not.toBeNull()
    expect(screen.getByText("History is kept for 14 hours")).toBeInTheDocument()
  })

  it("labels time every 6 hours", () => {
    draw()
    expect(screen.getByText("18:00")).toBeInTheDocument()
    expect(screen.getByText("00:00")).toBeInTheDocument()
    expect(screen.queryByText("19:00")).toBeNull()
  })

  it("marks now inside the bucket in progress", () => {
    const { container } = draw()
    const x = Number(container.querySelector('line[data-now="true"]')?.getAttribute("x1"))
    const colW = (960 - 64 - 8) / 25
    expect(x).toBeGreaterThan(64 + 24 * colW)
    expect(x).toBeLessThan(64 + 25 * colW)
  })

  it("says when no jobs ran", () => {
    draw({ buckets: base.buckets.map((b) => ({ ...b, succeeded: 0, failed: 0, cancelled: 0 })) })
    expect(screen.getByText("No jobs ran in the last 24 hours.")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npx vitest run src/components/buckets-chart.test.tsx`
Expected: FAIL with `Failed to resolve import "./buckets-chart"`.

- [ ] **Step 3: Write the chart**

Create `web/src/components/buckets-chart.tsx`:

```tsx
import type { Activity } from "@/api/types"
import { bucketAria, bucketTickLabel, GUTTER, lineRuns, PAD_RIGHT, retentionText, scaleX, sinceText, windowWords } from "@/lib/activity-view"

const TRACK_H = 44
const TRACK_GAP = 16
const AXIS_H = 18

type Point = { x: number; y: number }
const points = (run: Point[]) => run.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(" ")
const known = (values: (number | null)[]) => values.flatMap((v) => (v === null ? [] : [v]))

function Line({ runs, track, stroke, fill }: { runs: Point[][]; track: string; stroke: string; fill: string }) {
  return (
    <>
      {runs.map((run, i) =>
        run.length > 1 ? (
          <polyline key={i} data-track={track} points={points(run)} strokeWidth={1.5} className={`fill-none ${stroke}`} />
        ) : (
          <circle key={i} data-track={track} cx={run[0]?.x} cy={run[0]?.y} r={2} className={fill} />
        ),
      )}
    </>
  )
}

export function BucketsChart({ activity, now, width }: { activity: Activity; now: number; width: number }) {
  const buckets = activity.buckets
  const plotW = width - GUTTER - PAD_RIGHT
  const colW = plotW / Math.max(1, buckets.length)
  const colX = (i: number) => GUTTER + i * colW
  const center = (i: number) => colX(i) + colW / 2
  const top = (k: number) => k * (TRACK_H + TRACK_GAP)
  const axisTop = top(3) + TRACK_H
  const height = axisTop + AXIS_H

  const all = activity.capacity === null
  const busy = buckets.map((b) => (all ? b.busy_minutes : Math.min(100, b.busy_pct ?? 0)))
  const busyPeak = Math.max(0, ...busy)
  const busyScale = all ? Math.max(1, busyPeak) : 100
  const runsPeak = Math.max(0, ...buckets.map((b) => b.succeeded + b.failed))
  const runsScale = Math.max(1, runsPeak)
  const waits = buckets.map((b) => b.waiting_max)
  const cpus = buckets.map((b) => b.cpu_avg)
  const waitPeak = Math.max(0, ...known(waits))
  const cpuPeak = Math.max(0, ...known(cpus))
  const waitRuns = lineRuns(
    waits.map((v, i) => ({ x: center(i), v })),
    (v) => top(2) + TRACK_H - (v / Math.max(1, waitPeak)) * TRACK_H,
  )
  const cpuRuns = lineRuns(
    cpus.map((v, i) => ({ x: center(i), v })),
    (v) => top(3) + TRACK_H - (Math.min(v, 100) / 100) * TRACK_H,
  )

  const first = buckets[0]
  const last = buckets.at(-1)
  const start = first ? Date.parse(first.start) : now
  const end = last ? Date.parse(last.end) : now
  const historyFrom = Date.parse(activity.history_from)
  const totalRuns = buckets.reduce((n, b) => n + b.succeeded + b.failed + b.cancelled, 0)
  const nowX = last
    ? colX(buckets.length - 1) + colW * Math.min(1, Math.max(0, (now - Date.parse(last.start)) / (Date.parse(last.end) - Date.parse(last.start))))
    : GUTTER + plotW

  const tracks: [string, string][] = [
    ["Busy", all ? `peak ${Math.round(busyPeak)} min` : `peak ${Math.round(busyPeak)}%`],
    ["Runs", `peak ${runsPeak}`],
    ["Waiting", known(waits).length > 0 ? `peak ${waitPeak}` : ""],
    ["CPU", known(cpus).length > 0 ? `peak ${Math.round(cpuPeak)}%` : ""],
  ]

  // A null is a gap, never a zero; nulls from the start of the window mean
  // the metrics began part-way through it.
  function collecting(values: (number | null)[], k: number) {
    const lead = values.findIndex((v) => v !== null)
    if (lead === 0 || values.length === 0) return null
    const since = buckets[lead < 0 ? buckets.length - 1 : lead]
    if (!since) return null
    return (
      <text x={GUTTER + 6} y={top(k) + TRACK_H / 2 + 4} className="fill-muted-foreground">
        {sinceText(activity.window, since.start)}
      </text>
    )
  }

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="group"
      aria-label={`Activity per bucket for ${windowWords(activity.window)}`}
      className="block text-xs"
    >
      {tracks.map(([name, peak], k) => (
        <g key={name}>
          <text x={0} y={top(k) + 12} className="fill-muted-foreground">
            {name}
          </text>
          {peak && (
            <text x={0} y={top(k) + 26} className="fill-muted-foreground font-mono">
              {peak}
            </text>
          )}
          <rect x={GUTTER} y={top(k)} width={plotW} height={TRACK_H} className="fill-muted/30" />
        </g>
      ))}
      {buckets.map((b, i) => {
        const label = bucketTickLabel(activity.window, b.start)
        return label === null ? null : (
          <g key={`tick-${b.start}`}>
            <line x1={colX(i)} x2={colX(i)} y1={0} y2={axisTop} className="stroke-border" />
            <text x={colX(i) + 2} y={axisTop + 13} className="fill-muted-foreground font-mono">
              {label}
            </text>
          </g>
        )
      })}
      {historyFrom > start && (
        <g data-retention="true">
          <rect x={GUTTER} y={0} width={scaleX(historyFrom, start, end, plotW)} height={axisTop} className="fill-muted" />
          <text x={GUTTER + 6} y={12} className="fill-muted-foreground">
            {retentionText(activity)}
          </text>
        </g>
      )}
      {buckets.map((b, i) => {
        const open = i === buckets.length - 1
        const busyH = ((busy[i] ?? 0) / busyScale) * TRACK_H
        const okH = (b.succeeded / runsScale) * TRACK_H
        const failH = (b.failed / runsScale) * TRACK_H
        const x0 = colX(i) + 1
        const w = Math.max(1, colW - 2)
        const label = bucketAria(b, activity)
        return (
          <g
            key={b.start}
            tabIndex={0}
            role="img"
            aria-label={label}
            data-open={open ? "true" : undefined}
            className={`ghr-mark ${open ? "opacity-60" : ""}`}
          >
            <title>{label}</title>
            <rect x={colX(i)} y={0} width={colW} height={axisTop} className="ghr-mark-box fill-transparent" />
            {busyH > 0 && <rect data-kind="busy" x={x0} y={top(0) + TRACK_H - busyH} width={w} height={busyH} className="fill-primary" />}
            {okH > 0 && <rect data-kind="succeeded" x={x0} y={top(1) + TRACK_H - okH} width={w} height={okH} className="fill-success/60" />}
            {failH > 0 && (
              <rect data-kind="failed" x={x0} y={top(1) + TRACK_H - okH - failH} width={w} height={failH} className="fill-destructive/70" />
            )}
          </g>
        )
      })}
      <Line runs={waitRuns} track="waiting" stroke="stroke-warning" fill="fill-warning" />
      <Line runs={cpuRuns} track="cpu" stroke="stroke-primary" fill="fill-primary" />
      {collecting(waits, 2)}
      {collecting(cpus, 3)}
      {totalRuns === 0 && (
        <text x={GUTTER + plotW / 2} y={top(1) + TRACK_H / 2 + 4} textAnchor="middle" className="fill-muted-foreground">
          {`No jobs ran in ${windowWords(activity.window)}.`}
        </text>
      )}
      <line data-now="true" x1={nowX} x2={nowX} y1={0} y2={axisTop} strokeWidth={1.5} className="stroke-primary" />
    </svg>
  )
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `npx vitest run src/components/buckets-chart.test.tsx && npm run typecheck && npm run lint`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/buckets-chart.tsx web/src/components/buckets-chart.test.tsx
git commit -m "feat(web): draw activity in time buckets"
```

---

### Task 18: Add the window control

**Files:**
- Create: `web/src/components/window-control.tsx`
- Create: `web/src/lib/use-activity-window.ts`
- Test: `web/src/components/window-control.test.tsx`

**Interfaces:**
- Consumes: `ACTIVITY_WINDOWS` (Task 15), `ActivityWindow` (Task 1).
- Produces: `WindowControl({ value, onChange })`; `useActivityWindow(): [ActivityWindow, (w: ActivityWindow) => void]`, stored under `ghr-activity-window`.

**Items:** 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/window-control.test.tsx`:

```tsx
import { act, render, renderHook, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { beforeEach, describe, expect, it, vi } from "vitest"
import { useActivityWindow } from "@/lib/use-activity-window"
import { WindowControl } from "./window-control"

describe("WindowControl", () => {
  it("offers every window and reports the choice", async () => {
    const onChange = vi.fn()
    render(<WindowControl value="1h" onChange={onChange} />)
    expect(screen.getByRole("radiogroup", { name: "Time window" })).toBeInTheDocument()
    expect(screen.getAllByRole("radio").map((r) => r.textContent)).toEqual(["1h", "3h", "24h", "7d", "30d"])
    expect(screen.getByRole("radio", { name: "1h" })).toBeChecked()
    await userEvent.setup().click(screen.getByRole("radio", { name: "24h" }))
    expect(onChange).toHaveBeenCalledWith("24h")
  })
})

describe("useActivityWindow", () => {
  beforeEach(() => localStorage.clear())

  it("starts at 1h and remembers the choice", () => {
    const { result } = renderHook(() => useActivityWindow())
    expect(result.current[0]).toBe("1h")
    act(() => result.current[1]("7d"))
    expect(result.current[0]).toBe("7d")
    expect(localStorage.getItem("ghr-activity-window")).toBe("7d")
    expect(renderHook(() => useActivityWindow()).result.current[0]).toBe("7d")
  })

  it("ignores a stored value that is not a window", () => {
    localStorage.setItem("ghr-activity-window", "2w")
    expect(renderHook(() => useActivityWindow()).result.current[0]).toBe("1h")
  })
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `npx vitest run src/components/window-control.test.tsx`
Expected: FAIL with `Failed to resolve import "@/lib/use-activity-window"`.

- [ ] **Step 3: Write the hook and the control**

Create `web/src/lib/use-activity-window.ts`:

```ts
import { useState } from "react"
import type { ActivityWindow } from "@/api/types"
import { ACTIVITY_WINDOWS } from "./activity-view"

const KEY = "ghr-activity-window"

function stored(): ActivityWindow {
  try {
    const saved = localStorage.getItem(KEY)
    return ACTIVITY_WINDOWS.find((w) => w === saved) ?? "1h"
  } catch {
    return "1h"
  }
}

export function useActivityWindow(): [ActivityWindow, (w: ActivityWindow) => void] {
  const [current, setCurrent] = useState<ActivityWindow>(stored)
  function choose(w: ActivityWindow) {
    setCurrent(w)
    try {
      localStorage.setItem(KEY, w)
    } catch {
      // storage refused, as in a private window: the choice lasts this visit
    }
  }
  return [current, choose]
}
```

Create `web/src/components/window-control.tsx`:

```tsx
import { ToggleGroup, ToggleGroupItem } from "darkraise-ui/components/toggle-group"
import type { ActivityWindow } from "@/api/types"
import { ACTIVITY_WINDOWS } from "@/lib/activity-view"

export function WindowControl({ value, onChange }: { value: ActivityWindow; onChange: (w: ActivityWindow) => void }) {
  return (
    <ToggleGroup
      type="single"
      size="sm"
      variant="outline"
      value={value}
      aria-label="Time window"
      onValueChange={(next) => {
        const w = ACTIVITY_WINDOWS.find((x) => x === next)
        if (w) onChange(w)
      }}
    >
      {ACTIVITY_WINDOWS.map((w) => (
        <ToggleGroupItem key={w} value={w} className="font-mono">
          {w}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `npx vitest run src/components/window-control.test.tsx && npm run typecheck && npm run lint`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/window-control.tsx web/src/lib/use-activity-window.ts web/src/components/window-control.test.tsx
git commit -m "feat(web): pick and remember the activity window"
```

---

### Task 19: Assemble the activity panel

**Files:**
- Create: `web/src/lib/use-width.ts`
- Create: `web/src/components/activity-panel.tsx`
- Test: `web/src/components/activity-panel.test.tsx`

**Interfaces:**
- Consumes: `LanesChart` (Task 16), `BucketsChart` (Task 17), `WindowControl` (Task 18), the `activity-view` helpers (Task 15), `errorText` from `web/src/query.ts`.
- Produces: `useWidth`; `ActivityPanel({ selected, onSelect, activity, error, onRetry, now, onOpenRunner })` with `selected: ActivityWindow`, `onSelect: (w: ActivityWindow) => void`, `activity: Activity | undefined`, `error: unknown`, `onRetry: () => void`, `now: number`, `onOpenRunner: (id: string) => void`.

**Items:** 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/components/activity-panel.test.tsx`:

```tsx
import { render, screen, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import type { ComponentProps } from "react"
import { describe, expect, it, vi } from "vitest"
import { fixtures } from "@/test/fixtures"
import { ActivityPanel } from "./activity-panel"

const now = Date.parse("2026-10-03T14:05:00Z")

function draw(over: Partial<ComponentProps<typeof ActivityPanel>> = {}) {
  const props: ComponentProps<typeof ActivityPanel> = {
    selected: "1h",
    onSelect: vi.fn(),
    activity: fixtures.activityLanes,
    error: null,
    onRetry: vi.fn(),
    now,
    onOpenRunner: vi.fn(),
    ...over,
  }
  render(<ActivityPanel {...props} />)
  return props
}

const legend = () => within(screen.getByRole("list", { name: "Legend" }))

describe("ActivityPanel", () => {
  it("shows the range, a lanes legend and the lanes for 1h", () => {
    draw()
    expect(screen.getByRole("heading", { name: "Activity" })).toBeInTheDocument()
    expect(screen.getByText("13:05 to 14:05")).toBeInTheDocument()
    for (const key of ["Succeeded", "Failed", "Running", "Warm", "Starting", "Jobs waiting", "Memory"]) {
      expect(legend().getByText(key)).toBeInTheDocument()
    }
    expect(screen.getByRole("group", { name: "Runner lanes for the last hour" })).toBeInTheDocument()
  })

  it("shows buckets with their own legend", () => {
    draw({ selected: "24h", activity: fixtures.activityBuckets })
    expect(screen.getByText("Oct 2 14:00 to Oct 3 14:05, per hour")).toBeInTheDocument()
    expect(legend().getByText("Busy runner-minutes")).toBeInTheDocument()
    expect(screen.getByRole("group", { name: "Activity per bucket for the last 24 hours" })).toBeInTheDocument()
  })

  it("calls busy time slot time when there is a capacity", () => {
    draw({ selected: "24h", activity: { ...fixtures.activityBuckets, capacity: 2 } })
    expect(legend().getByText("Busy slot time")).toBeInTheDocument()
  })

  it("reports a window choice", async () => {
    const props = draw()
    await userEvent.setup().click(screen.getByRole("radio", { name: "7d" }))
    expect(props.onSelect).toHaveBeenCalledWith("7d")
  })

  it("sums up the window for screen readers, with a table of the same data", () => {
    draw()
    expect(screen.getByText("3 jobs in the last hour, 1 failed, 1 running.")).toHaveClass("sr-only")
    const table = screen.getByRole("table", { name: "Activity data" })
    expect(within(table).getAllByRole("row")).toHaveLength(5)
  })

  it("keeps the last data when a refresh fails, and offers to retry", async () => {
    const props = draw({ error: new Error("boom") })
    expect(screen.getByRole("alert")).toHaveTextContent("Couldn't load activity: boom")
    expect(screen.getByRole("group", { name: "Runner lanes for the last hour" })).toBeInTheDocument()
    await userEvent.setup().click(screen.getByRole("button", { name: "Retry" }))
    expect(props.onRetry).toHaveBeenCalled()
  })

  it("waits for the first answer", () => {
    draw({ activity: undefined })
    expect(screen.getByText("Loading activity")).toBeInTheDocument()
  })

  it("shows only the error when nothing has loaded", () => {
    draw({ activity: undefined, error: new Error("boom") })
    expect(screen.getByRole("alert")).toBeInTheDocument()
    expect(screen.queryByText("Loading activity")).toBeNull()
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npx vitest run src/components/activity-panel.test.tsx`
Expected: FAIL with `Failed to resolve import "./activity-panel"`.

- [ ] **Step 3: Write the width hook**

Create `web/src/lib/use-width.ts`:

```ts
import { useEffect, useRef, useState, type RefObject } from "react"

// Below this the chart scrolls inside its panel instead of squeezing, so the
// page body never scrolls sideways.
const MIN_WIDTH = 720

export function useWidth<T extends HTMLElement>(fallback: number): [RefObject<T | null>, number] {
  const ref = useRef<T>(null)
  const [width, setWidth] = useState(fallback)
  useEffect(() => {
    const el = ref.current
    if (!el || typeof ResizeObserver === "undefined") return
    const observer = new ResizeObserver(([entry]) => {
      if (entry) setWidth(Math.max(MIN_WIDTH, Math.floor(entry.contentRect.width)))
    })
    observer.observe(el)
    return () => observer.disconnect()
  }, [])
  return [ref, width]
}
```

- [ ] **Step 4: Write the panel**

Create `web/src/components/activity-panel.tsx`:

```tsx
import { Button } from "darkraise-ui/components/button"
import type { ReactNode } from "react"
import type { Activity, ActivityWindow } from "@/api/types"
import { BucketsChart } from "@/components/buckets-chart"
import { LanesChart } from "@/components/lanes-chart"
import { WindowControl } from "@/components/window-control"
import { activitySummary, bucketAria, isLaneWindow, rangeText, runAria } from "@/lib/activity-view"
import { useWidth } from "@/lib/use-width"
import { errorText } from "@/query"

function Swatch({ className, dash = false }: { className: string; dash?: boolean }) {
  return (
    <svg width="14" height="10" aria-hidden="true" className="shrink-0">
      <rect x="0.5" y="0.5" width="13" height="9" rx="2" strokeWidth="1" strokeDasharray={dash ? "3 2" : undefined} className={className} />
    </svg>
  )
}

function LineSwatch({ className, dash = false }: { className: string; dash?: boolean }) {
  return (
    <svg width="14" height="10" aria-hidden="true" className="shrink-0">
      <line x1="0" y1="5" x2="14" y2="5" strokeWidth="1.5" strokeDasharray={dash ? "3 2" : undefined} className={className} />
    </svg>
  )
}

// Each state reads by shape as well as colour: solid, dashed outline, plain
// outline, or tint.
function Legend({ lanes, capacity }: { lanes: boolean; capacity: number | null }) {
  const items: [string, ReactNode][] = lanes
    ? [
        ["Succeeded", <Swatch className="fill-success/30 stroke-success/60" />],
        ["Failed", <Swatch className="fill-destructive/30 stroke-destructive/70" />],
        ["Cancelled", <Swatch className="fill-muted stroke-muted-foreground" />],
        ["Running", <Swatch className="fill-primary stroke-primary" />],
        ["Warm", <Swatch className="fill-none stroke-primary" dash />],
        ["Starting", <Swatch className="fill-none stroke-muted-foreground" />],
        ["Jobs waiting", <Swatch className="fill-warning/25 stroke-warning" />],
        ["CPU", <LineSwatch className="stroke-primary" />],
        ["Memory", <LineSwatch className="stroke-muted-foreground" dash />],
      ]
    : [
        [capacity === null ? "Busy runner-minutes" : "Busy slot time", <Swatch className="fill-primary stroke-primary" />],
        ["Succeeded", <Swatch className="fill-success/60 stroke-success/60" />],
        ["Failed", <Swatch className="fill-destructive/70 stroke-destructive/70" />],
        ["Most jobs waiting", <LineSwatch className="stroke-warning" />],
        ["Average CPU", <LineSwatch className="stroke-primary" />],
      ]
  return (
    <ul aria-label="Legend" className="mb-2 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
      {items.map(([label, swatch]) => (
        <li key={label} className="flex items-center gap-1.5">
          {swatch}
          {label}
        </li>
      ))}
    </ul>
  )
}

function ActivityTable({ activity, now }: { activity: Activity; now: number }) {
  const lanes = isLaneWindow(activity.window)
  const rows = lanes
    ? activity.lanes.flatMap((lane, i) => lane.runs.map((run) => ({ key: run.instance_id, cells: [`Lane ${i + 1}`, runAria(run, now)] })))
    : activity.buckets.map((b) => ({ key: b.start, cells: [bucketAria(b, activity)] }))
  const headers = lanes ? ["Lane", "Run"] : ["Bucket"]
  return (
    <table className="sr-only">
      <caption>Activity data</caption>
      <thead>
        <tr>
          {headers.map((h) => (
            <th key={h} scope="col">
              {h}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((r) => (
          <tr key={r.key}>
            {r.cells.map((c, i) => (
              <td key={i}>{c}</td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  )
}

export function ActivityPanel({
  selected,
  onSelect,
  activity,
  error,
  onRetry,
  now,
  onOpenRunner,
}: {
  selected: ActivityWindow
  onSelect: (w: ActivityWindow) => void
  activity: Activity | undefined
  error: unknown
  onRetry: () => void
  now: number
  onOpenRunner: (id: string) => void
}) {
  const [ref, width] = useWidth<HTMLDivElement>(960)
  const failed = error !== null && error !== undefined
  const lanes = activity !== undefined && isLaneWindow(activity.window)
  return (
    <section aria-labelledby="activity-title" className="rounded-[10px] border border-border bg-card p-4">
      <div className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-2">
        <h2 id="activity-title" className="text-base font-semibold">
          Activity
        </h2>
        {activity && <span className="font-mono text-sm text-muted-foreground">{rangeText(activity.window, activity.from, activity.to)}</span>}
        <div className="ml-auto">
          <WindowControl value={selected} onChange={onSelect} />
        </div>
      </div>
      {failed && (
        <div role="alert" className="mb-3 flex flex-wrap items-center gap-2 text-sm text-destructive">
          <span>{`Couldn't load activity: ${errorText(error)}`}</span>
          <Button size="sm" variant="outline" onClick={onRetry}>
            Retry
          </Button>
        </div>
      )}
      {activity && <Legend lanes={lanes} capacity={activity.capacity} />}
      <div ref={ref} className="overflow-x-auto">
        {activity ? (
          lanes ? (
            <LanesChart activity={activity} now={now} width={width} onOpenRunner={onOpenRunner} />
          ) : (
            <BucketsChart activity={activity} now={now} width={width} />
          )
        ) : (
          !failed && <p className="py-8 text-center text-sm text-muted-foreground">Loading activity</p>
        )}
      </div>
      {activity && (
        <>
          <p className="sr-only">{activitySummary(activity)}</p>
          <ActivityTable activity={activity} now={now} />
        </>
      )}
    </section>
  )
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `npx vitest run src/components/activity-panel.test.tsx && npm run typecheck && npm run lint`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/use-width.ts web/src/components/activity-panel.tsx web/src/components/activity-panel.test.tsx
git commit -m "feat(web): assemble the activity panel"
```

---

### Task 20: Draw the repository activity strip

**Files:**
- Create: `web/src/components/repo-activity-strip.tsx`
- Test: `web/src/components/repo-activity-strip.test.tsx`

**Interfaces:**
- Consumes: `ActivityHour` (Task 1).
- Produces: `RepoActivityStrip({ repo, hours })`.

**Items:** 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/components/repo-activity-strip.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { ActivityHour } from "@/api/types"
import { RepoActivityStrip } from "./repo-activity-strip"

const hour = (i: number, over: Partial<ActivityHour> = {}): ActivityHour => ({
  start: new Date(Date.parse("2026-10-02T15:00:00Z") + i * 3_600_000).toISOString(),
  succeeded: 0,
  failed: 0,
  cancelled: 0,
  ...over,
})

describe("RepoActivityStrip", () => {
  it("shades each hour by its runs and outlines the hour in progress", () => {
    const hours = Array.from({ length: 24 }, (_, i) => hour(i))
    hours[5] = hour(5, { succeeded: 1 })
    hours[6] = hour(6, { succeeded: 3 })
    hours[7] = hour(7, { succeeded: 5 })
    hours[8] = hour(8, { succeeded: 2, failed: 1 })
    hours[9] = hour(9, { cancelled: 1 })
    render(<RepoActivityStrip repo="darkmem" hours={hours} />)
    const strip = screen.getByRole("img", { name: "darkmem, last 24 hours: 11 succeeded, 1 failed" })
    const cells = [...strip.children]
    expect(cells.map((c) => c.getAttribute("data-cell")).slice(4, 10)).toEqual(["none", "ok-1", "ok-2", "ok-3", "bad", "ok-1"])
    expect(cells[23]).toHaveAttribute("data-open", "true")
    expect(cells[23]).toHaveClass("outline-primary")
    expect(cells[22]).not.toHaveAttribute("data-open")
  })

  it("says when nothing ran", () => {
    render(<RepoActivityStrip repo="darkmem" hours={Array.from({ length: 24 }, (_, i) => hour(i))} />)
    expect(screen.getByRole("img", { name: "darkmem, last 24 hours: no runs" })).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npx vitest run src/components/repo-activity-strip.test.tsx`
Expected: FAIL with `Failed to resolve import "./repo-activity-strip"`.

- [ ] **Step 3: Write the strip**

Create `web/src/components/repo-activity-strip.tsx`:

```tsx
import type { ActivityHour } from "@/api/types"

type Cell = "none" | "ok-1" | "ok-2" | "ok-3" | "bad"

const CELL_CLASS: Record<Cell, string> = {
  none: "bg-muted",
  "ok-1": "bg-success/35",
  "ok-2": "bg-success/65",
  "ok-3": "bg-success",
  bad: "bg-destructive",
}

function cellOf(h: ActivityHour): Cell {
  if (h.failed > 0) return "bad"
  const runs = h.succeeded + h.cancelled
  if (runs === 0) return "none"
  return runs === 1 ? "ok-1" : runs <= 3 ? "ok-2" : "ok-3"
}

export function RepoActivityStrip({ repo, hours }: { repo: string; hours: ActivityHour[] }) {
  const ok = hours.reduce((n, h) => n + h.succeeded, 0)
  const failed = hours.reduce((n, h) => n + h.failed, 0)
  const label = ok + failed === 0 ? `${repo}, last 24 hours: no runs` : `${repo}, last 24 hours: ${ok} succeeded, ${failed} failed`
  return (
    <div role="img" aria-label={label} className="flex h-4 items-stretch gap-px">
      {hours.map((h, i) => {
        const cell = cellOf(h)
        const open = i === hours.length - 1
        return (
          <span
            key={h.start}
            data-cell={cell}
            data-open={open ? "true" : undefined}
            className={`w-1.5 rounded-[1px] ${CELL_CLASS[cell]} ${open ? "outline outline-1 -outline-offset-1 outline-primary" : ""}`}
          />
        )
      })}
    </div>
  )
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `npx vitest run src/components/repo-activity-strip.test.tsx && npm run typecheck && npm run lint`
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add web/src/components/repo-activity-strip.tsx web/src/components/repo-activity-strip.test.tsx
git commit -m "feat(web): draw each repository's last 24 hours"
```

---

### Task 21: Share the repository actions

**Files:**
- Create: `web/src/lib/use-repo-actions.ts`
- Modify: `web/src/components/repo-actions.tsx` (replace)
- Test: `web/src/lib/use-repo-actions.test.tsx`

**Interfaces:**
- Consumes: `api.pauseRepo`, `api.resumeRepo`, `api.removeRepo`, `Confirm` and `ConfirmDialog` from `web/src/components/confirm-dialog.tsx` (existing).
- Produces: `useRepoActions(repo, offline)` (Contracts › Dashboard pieces); `RepoActionButtons` keeps its props and behaviour for the Repositories page.

**Items:** 9

**Implementer:** dr-superpowers:impl-sonnet-high
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 2 = 5

- [ ] **Step 1: Record the baseline**

Run: `npx vitest run src/pages/repositories.test.tsx src/pages/dashboard.test.tsx src/pages/repository.test.tsx`
Expected: PASS. These tests drive `RepoActionButtons` today and must pass unchanged after the refactor.

- [ ] **Step 2: Write the failing hook test**

Create `web/src/lib/use-repo-actions.test.tsx`:

```tsx
import { act, renderHook, waitFor } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { RepoStatus } from "@/api/types"
import { mockApi, noContent } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { useRepoActions } from "./use-repo-actions"

const darkmem = fixtures.status.repos[0] as RepoStatus
const darkcloud = fixtures.status.repos[1] as RepoStatus

function setup(repo: RepoStatus, offline = false) {
  const { wrapper } = withQuery()
  return renderHook(() => useRepoActions(repo, offline), { wrapper })
}

describe("useRepoActions", () => {
  it("pauses a running repository and resumes a paused one", async () => {
    const { calls } = mockApi({
      "POST /api/repos/darkmem/pause": () => noContent(),
      "POST /api/repos/darkcloud/resume": () => noContent(),
    })
    act(() => setup(darkmem).result.current.togglePause())
    act(() => setup(darkcloud).result.current.togglePause())
    await waitFor(() => expect(calls.map((c) => c.path).sort()).toEqual(["/api/repos/darkcloud/resume", "/api/repos/darkmem/pause"]))
  })

  it("asks before removing, then removes", async () => {
    const { calls } = mockApi({ "DELETE /api/repos/darkmem": () => noContent() })
    const { result } = setup(darkmem)
    act(() => result.current.askRemove())
    expect(result.current.confirm?.title).toBe("Remove repo darkmem? Its running jobs finish first.")
    expect(calls).toHaveLength(0)
    act(() => result.current.confirm?.run())
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/repos/darkmem")).toBe(true))
    act(() => result.current.closeConfirm())
    expect(result.current.confirm).toBeNull()
  })

  it("locks a repository being removed, or while the daemon is unreachable", () => {
    expect(setup({ ...darkmem, removing: true }).result.current.locked).toBe(true)
    expect(setup(darkmem, true).result.current.locked).toBe(true)
    expect(setup(darkmem).result.current.locked).toBe(false)
  })
})
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `npx vitest run src/lib/use-repo-actions.test.tsx`
Expected: FAIL with `Failed to resolve import "./use-repo-actions"`.

- [ ] **Step 4: Extract the hook**

Create `web/src/lib/use-repo-actions.ts`:

```ts
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { RepoStatus } from "@/api/types"
import type { Confirm } from "@/components/confirm-dialog"

// Every control that pauses or removes a repository goes through here, so the
// Dashboard's row menu and the Repositories page cannot drift.
export function useRepoActions(repo: RepoStatus, offline: boolean) {
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const refresh = () =>
    Promise.all([
      queryClient.invalidateQueries({ queryKey: keys.status }),
      queryClient.invalidateQueries({ queryKey: keys.config }),
    ])
  const pause = useMutation({
    mutationFn: (resume: boolean) => (resume ? api.resumeRepo(repo.name) : api.pauseRepo(repo.name)),
    onSuccess: (_data, resume) => {
      toast.success(`${resume ? "resumed" : "paused"} ${repo.name}`)
    },
    onSettled: refresh,
  })
  const remove = useMutation({
    mutationFn: () => api.removeRepo(repo.name),
    onSuccess: () => {
      toast.success(`removing ${repo.name}`)
    },
    onSettled: refresh,
  })
  return {
    locked: offline || Boolean(repo.removing),
    togglePause: () => pause.mutate(repo.paused),
    askRemove: () =>
      setConfirm({
        title: `Remove repo ${repo.name}? Its running jobs finish first.`,
        action: "Remove",
        destructive: true,
        run: () => remove.mutate(),
      }),
    confirm,
    closeConfirm: () => setConfirm(null),
  }
}
```

Replace `web/src/components/repo-actions.tsx` with:

```tsx
import { Button } from "darkraise-ui/components/button"
import type { RepoStatus } from "@/api/types"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { useRepoActions } from "@/lib/use-repo-actions"

export function RepoActionButtons({ repo, offline }: { repo: RepoStatus; offline: boolean }) {
  const actions = useRepoActions(repo, offline)
  return (
    <>
      <Button
        size="sm"
        variant="secondary"
        aria-label={`${repo.paused ? "Resume" : "Pause"} ${repo.name}`}
        disabled={actions.locked}
        onClick={actions.togglePause}
      >
        {repo.paused ? "Resume" : "Pause"}
      </Button>
      <Button size="sm" variant="destructive" aria-label={`Remove ${repo.name}`} disabled={actions.locked} onClick={actions.askRemove}>
        Remove
      </Button>
      <ConfirmDialog confirm={actions.confirm} onClose={actions.closeConfirm} />
    </>
  )
}
```

- [ ] **Step 5: Run the hook test and the baseline again**

Run: `npx vitest run src/lib/use-repo-actions.test.tsx src/pages/repositories.test.tsx src/pages/dashboard.test.tsx src/pages/repository.test.tsx && npm run typecheck && npm run lint`
Expected: all pass, with the Step 1 tests unchanged.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/use-repo-actions.ts web/src/lib/use-repo-actions.test.tsx web/src/components/repo-actions.tsx
git commit -m "refactor(web): share pause and remove for a repo"
```

---

### Task 22: Build the repository table

**Files:**
- Modify: `web/src/lib/status.ts` (append `repoStateWord`)
- Create: `web/src/components/repo-table.tsx`
- Test: `web/src/components/repo-table.test.tsx`

**Interfaces:**
- Consumes: `useRepoActions` (Task 21), `RepoActivityStrip` (Task 20), `ResultIcon` (Task 12), `ConfirmDialog`, `ago`.
- Produces: `repoStateWord(r)`; `RepoTable({ repos, activity, now, offline, onOpen })`.

**Items:** 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

Create `web/src/components/repo-table.test.tsx`:

```tsx
import { render, screen, waitFor, within } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { TooltipProvider } from "darkraise-ui/components/tooltip"
import { describe, expect, it, vi } from "vitest"
import type { RepoStatus } from "@/api/types"
import { mockApi, noContent } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { withQuery } from "@/test/query"
import { RepoTable } from "./repo-table"

const now = Date.parse("2026-10-03T14:05:00Z")
const [darkmem, darkcloud, oldRepo] = fixtures.status.repos as [RepoStatus, RepoStatus, RepoStatus]

function draw(repos: RepoStatus[] = fixtures.status.repos, offline = false) {
  const onOpen = vi.fn()
  const { wrapper } = withQuery()
  const user = userEvent.setup()
  render(
    <TooltipProvider>
      <RepoTable repos={repos} activity={fixtures.activityLanes.repos} now={now} offline={offline} onOpen={onOpen} />
    </TooltipProvider>,
    { wrapper },
  )
  return { onOpen, user }
}

const row = (name: string) => screen.getByRole("link", { name }).closest("tr") as HTMLElement
const cell = (name: string, index: number) => row(name).querySelectorAll("td")[index] as HTMLElement

describe("RepoTable", () => {
  it("names each state in a word", () => {
    draw([darkmem, darkcloud, oldRepo, { ...darkmem, name: "idle-repo", active: 0 }, { ...darkcloud, name: "leaving", paused: false, removing: true }])
    expect(within(row("darkmem")).getByText("Running")).toHaveClass("text-primary")
    expect(within(row("darkcloud")).getByText("Paused")).toHaveClass("text-muted-foreground")
    expect(within(row("old-repo")).getByText("Error")).toHaveClass("text-destructive")
    expect(within(row("idle-repo")).getByText("Idle")).toBeInTheDocument()
    expect(within(row("leaving")).getByText("Removing")).toHaveClass("text-muted-foreground")
  })

  it("shows a repository's raw error under its name", () => {
    draw()
    expect(within(row("old-repo")).getByText("GitHub: not found")).toHaveClass("text-destructive")
  })

  it("shows waiting jobs in warn and leaves zero blank", () => {
    draw()
    expect(within(row("darkmem")).getByText("3")).toHaveClass("text-warning")
    expect(cell("darkcloud", 3).textContent).toBe("")
  })

  it("draws one cell per allowed runner, or only the count when unlimited", () => {
    draw([darkmem, { ...darkcloud, max: 0, active: 1 }])
    expect(cell("darkmem", 2)).toHaveTextContent("1/2")
    expect(cell("darkmem", 2).querySelectorAll('[data-slot="runner"]')).toHaveLength(2)
    expect(cell("darkmem", 2).querySelectorAll('[data-filled="true"]')).toHaveLength(1)
    expect(cell("darkcloud", 2).textContent).toBe("1")
  })

  it("draws the last 24 hours with the hour in progress outlined", () => {
    draw()
    const strip = within(row("darkmem")).getByRole("img", { name: /^darkmem, last 24 hours/ })
    expect(strip.children).toHaveLength(24)
    expect(strip.lastElementChild).toHaveAttribute("data-open", "true")
  })

  it("shows the last job with its result", () => {
    draw()
    const last = cell("darkmem", 5)
    expect(within(last).getByRole("img", { name: "Succeeded" })).toBeInTheDocument()
    expect(last).toHaveTextContent("build #41")
    expect(last).toHaveTextContent("25m ago")
  })

  it("pauses from the row menu", async () => {
    const { calls } = mockApi({ "POST /api/repos/darkmem/pause": () => noContent() })
    const { user } = draw()
    await user.click(screen.getByRole("button", { name: "More actions for darkmem" }))
    await user.click(await screen.findByRole("menuitem", { name: "Pause" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/repos/darkmem/pause")).toBe(true))
  })

  it("asks before removing from the row menu", async () => {
    const { calls } = mockApi({ "DELETE /api/repos/darkmem": () => noContent() })
    const { user } = draw()
    await user.click(screen.getByRole("button", { name: "More actions for darkmem" }))
    await user.click(await screen.findByRole("menuitem", { name: "Remove" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(calls).toHaveLength(0)
    await user.click(ask.getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/repos/darkmem")).toBe(true))
  })

  it("opens a repository from its name, its row or Edit", async () => {
    const { onOpen, user } = draw()
    await user.click(screen.getByRole("link", { name: "darkcloud" }))
    expect(onOpen).toHaveBeenLastCalledWith("darkcloud")
    await user.click(within(row("old-repo")).getByText("Error"))
    expect(onOpen).toHaveBeenLastCalledWith("old-repo")
    await user.click(screen.getByRole("button", { name: "More actions for darkmem" }))
    await user.click(await screen.findByRole("menuitem", { name: "Edit" }))
    expect(onOpen).toHaveBeenLastCalledWith("darkmem")
    expect(onOpen).toHaveBeenCalledTimes(3)
  })

  it("locks pause and remove while the daemon is unreachable", async () => {
    const { calls } = mockApi({})
    const { user } = draw(fixtures.status.repos, true)
    await user.click(screen.getByRole("button", { name: "More actions for darkmem" }))
    await user.click(await screen.findByRole("menuitem", { name: "Pause" }))
    expect(calls).toHaveLength(0)
  })

  it("names the row menu in a tooltip", async () => {
    const { user } = draw()
    await user.hover(screen.getByRole("button", { name: "More actions for darkmem" }))
    expect(await screen.findByRole("tooltip", {}, { timeout: 3000 })).toHaveTextContent("More actions")
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npx vitest run src/components/repo-table.test.tsx`
Expected: FAIL with `Failed to resolve import "./repo-table"`.

- [ ] **Step 3: Add the state word**

Append to `web/src/lib/status.ts`:

```ts
export function repoStateWord(r: RepoStatus): "Error" | "Removing" | "Paused" | "Running" | "Idle" {
  if (r.error) return "Error"
  if (r.removing) return "Removing"
  if (r.paused) return "Paused"
  return r.active > 0 ? "Running" : "Idle"
}
```

- [ ] **Step 4: Write the table**

Create `web/src/components/repo-table.tsx`:

```tsx
import { Button } from "darkraise-ui/components/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "darkraise-ui/components/dropdown-menu"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { Ellipsis } from "lucide-react"
import type { ActivityRepo, RepoStatus } from "@/api/types"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { RepoActivityStrip } from "@/components/repo-activity-strip"
import { ResultIcon } from "@/components/result-icon"
import { ago } from "@/lib/format"
import { repoStateWord } from "@/lib/status"
import { useRepoActions } from "@/lib/use-repo-actions"

const STATE_CLASS: Record<string, string | undefined> = {
  Running: "text-primary",
  Paused: "text-muted-foreground",
  Removing: "text-muted-foreground",
  Error: "text-destructive",
}

function RunnerCells({ repo }: { repo: RepoStatus }) {
  if (repo.max === 0) return <span className="font-mono">{repo.active}</span>
  return (
    <span className="flex items-center gap-2">
      <span className="font-mono">
        {repo.active}/{repo.max}
      </span>
      <span aria-hidden="true" className="flex gap-0.5">
        {Array.from({ length: repo.max }, (_, i) => (
          <span
            key={i}
            data-slot="runner"
            data-filled={i < repo.active ? "true" : undefined}
            className={`h-2.5 w-1.5 rounded-[1px] ${i < repo.active ? "bg-primary" : "bg-muted"}`}
          />
        ))}
      </span>
    </span>
  )
}

// Changing a cap is rare, so the max stepper lives on the repository page and
// the row keeps one menu.
function RowMenu({ repo, offline, onOpen }: { repo: RepoStatus; offline: boolean; onOpen: (name: string) => void }) {
  const actions = useRepoActions(repo, offline)
  return (
    <>
      <DropdownMenu>
        <Tooltip>
          <TooltipTrigger asChild>
            <DropdownMenuTrigger asChild>
              <Button size="icon" variant="ghost" aria-label={`More actions for ${repo.name}`}>
                <Ellipsis size={15} aria-hidden="true" />
              </Button>
            </DropdownMenuTrigger>
          </TooltipTrigger>
          <TooltipContent>More actions</TooltipContent>
        </Tooltip>
        <DropdownMenuContent align="end">
          <DropdownMenuItem disabled={actions.locked} onSelect={actions.togglePause}>
            {repo.paused ? "Resume" : "Pause"}
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={() => onOpen(repo.name)}>Edit</DropdownMenuItem>
          <DropdownMenuItem disabled={actions.locked} onSelect={actions.askRemove} className="text-destructive">
            Remove
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <ConfirmDialog confirm={actions.confirm} onClose={actions.closeConfirm} />
    </>
  )
}

export function RepoTable({
  repos,
  activity,
  now,
  offline,
  onOpen,
}: {
  repos: RepoStatus[]
  activity: ActivityRepo[] | undefined
  now: number
  offline: boolean
  onOpen: (name: string) => void
}) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Repository</TableHead>
          <TableHead>State</TableHead>
          <TableHead>Runners</TableHead>
          <TableHead>Waiting</TableHead>
          <TableHead>Last 24 hours</TableHead>
          <TableHead>Last job</TableHead>
          <TableHead>
            <span className="sr-only">Actions</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {repos.map((r) => {
          const word = repoStateWord(r)
          const hours = activity?.find((a) => a.repo === r.name)?.hours
          return (
            <TableRow key={r.name} className="cursor-pointer" onClick={() => onOpen(r.name)}>
              <TableCell>
                <a
                  href={`/repositories/${encodeURIComponent(r.name)}`}
                  className="font-medium hover:underline"
                  onClick={(e) => {
                    e.preventDefault()
                    e.stopPropagation()
                    onOpen(r.name)
                  }}
                >
                  {r.name}
                </a>
                {r.error && <p className="text-xs text-destructive">{r.error}</p>}
              </TableCell>
              <TableCell className={STATE_CLASS[word] ?? ""}>{word}</TableCell>
              <TableCell>
                <RunnerCells repo={r} />
              </TableCell>
              <TableCell>{r.queued > 0 && <span className="font-mono text-warning">{r.queued}</span>}</TableCell>
              <TableCell>{hours && <RepoActivityStrip repo={r.name} hours={hours} />}</TableCell>
              <TableCell>
                {r.last_job && (
                  <span className="flex items-center gap-1.5 whitespace-nowrap">
                    <ResultIcon conclusion={r.last_job.conclusion} />
                    <span>
                      {r.last_job.job_name} <span className="font-mono">#{r.last_job.run_number}</span>
                    </span>
                    <span className="text-muted-foreground">{ago(now - Date.parse(r.last_job.finished_at))}</span>
                  </span>
                )}
              </TableCell>
              <TableCell className="text-right" onClick={(e) => e.stopPropagation()}>
                <RowMenu repo={r} offline={offline} onOpen={onOpen} />
              </TableCell>
            </TableRow>
          )
        })}
      </TableBody>
    </Table>
  )
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `npx vitest run src/components/repo-table.test.tsx && npm run typecheck && npm run lint`
Expected: all pass. If the tooltip and menu triggers do not compose (the menu does not open), nest them the other way round (`DropdownMenuTrigger asChild` outside `TooltipTrigger asChild`) and rerun; keep both behaviours tested.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/status.ts web/src/components/repo-table.tsx web/src/components/repo-table.test.tsx
git commit -m "feat(web): add the Dashboard repository table"
```

---

### Task 23: Draw the disk breakdown

**Files:**
- Create: `web/src/lib/disk.ts`
- Create: `web/src/components/disk-breakdown.tsx`
- Test: `web/src/components/disk-breakdown.test.tsx`

**Interfaces:**
- Consumes: `Status.disk_used_bytes`, `Status.disk_total_bytes` (Task 1); `Storage` (existing); `humanBytes` (existing).
- Produces: `diskParts(storage, used)`, `DiskPart`; `DiskBreakdown({ status, storage, highWater })`.

**Items:** 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/components/disk-breakdown.test.tsx`:

```tsx
import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { diskParts } from "@/lib/disk"
import { fixtures } from "@/test/fixtures"
import { DiskBreakdown } from "./disk-breakdown"

describe("diskParts", () => {
  it("splits the used bytes into what fills the disk", () => {
    expect(diskParts(fixtures.storage, 146_000_000_000)).toEqual([
      { key: "build-cache", label: "Build cache", bytes: 420_000_000 },
      { key: "images", label: "Images", bytes: 8_100_000_000 },
      { key: "containers", label: "Containers", bytes: 0 },
      { key: "volumes", label: "Local volumes", bytes: 0 },
      { key: "package-caches", label: "Package caches", bytes: 3_600_000_000 },
      { key: "toolchains", label: "Toolchains", bytes: 270_000_000 },
      { key: "other", label: "Other", bytes: 133_610_000_000 },
    ])
  })

  it("clamps other at zero", () => {
    expect(diskParts(fixtures.storage, 1_000).at(-1)?.bytes).toBe(0)
  })

  it("shows one used part until storage is measured", () => {
    expect(diskParts(undefined, 5)).toEqual([{ key: "used", label: "Used", bytes: 5 }])
  })
})

describe("DiskBreakdown", () => {
  it("shows the headline, the stacked bar, the prune marker and the legend", () => {
    const { container } = render(<DiskBreakdown status={fixtures.status} storage={fixtures.storage} highWater={80} />)
    expect(screen.getByText("61%")).toHaveClass("font-mono")
    expect(screen.getByText("146.0 GB of 240.0 GB used, prunes at 80%")).toBeInTheDocument()
    expect(container.querySelector('[data-marker="high-water"]')).toHaveStyle({ left: "80%" })
    expect(container.querySelector('[data-part="images"]')).toHaveStyle({ width: `${(8_100_000_000 / 240_000_000_000) * 100}%` })
    const legend = within(screen.getByRole("list"))
    expect(legend.getByText("Images")).toBeInTheDocument()
    expect(legend.getByText("8.1 GB")).toBeInTheDocument()
    expect(legend.getByText("133.6 GB")).toBeInTheDocument()
  })

  it("never paints a category in a status colour", () => {
    const { container } = render(<DiskBreakdown status={fixtures.status} storage={fixtures.storage} highWater={80} />)
    for (const part of container.querySelectorAll("[data-part]")) expect(part.className).not.toMatch(/success|destructive|warning/)
  })

  it("says when the disk is not measured yet", () => {
    render(<DiskBreakdown status={{ ...fixtures.status, disk_total_bytes: 0 }} storage={undefined} highWater={80} />)
    expect(screen.getByText("Not measured yet")).toBeInTheDocument()
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `npx vitest run src/components/disk-breakdown.test.tsx`
Expected: FAIL with `Failed to resolve import "@/lib/disk"`.

- [ ] **Step 3: Write the split**

Create `web/src/lib/disk.ts`:

```ts
import type { Storage } from "@/api/types"

export interface DiskPart {
  key: string
  label: string
  bytes: number
}

const sum = (items: { bytes: number }[] | null) => (items ?? []).reduce((n, i) => n + i.bytes, 0)

export function diskParts(storage: Storage | undefined, used: number): DiskPart[] {
  if (!storage) return [{ key: "used", label: "Used", bytes: used }]
  const docker = (type: string) => storage.docker.rows?.find((r) => r.type === type)?.bytes ?? 0
  const parts: DiskPart[] = [
    { key: "build-cache", label: "Build cache", bytes: docker("Build Cache") },
    { key: "images", label: "Images", bytes: docker("Images") },
    { key: "containers", label: "Containers", bytes: docker("Containers") },
    { key: "volumes", label: "Local volumes", bytes: docker("Local Volumes") },
    { key: "package-caches", label: "Package caches", bytes: sum(storage.package_caches) },
    { key: "toolchains", label: "Toolchains", bytes: sum(storage.toolchains) + sum(storage.other_tool_cache) },
  ]
  const known = parts.reduce((n, p) => n + p.bytes, 0)
  return [...parts, { key: "other", label: "Other", bytes: Math.max(0, used - known) }]
}
```

- [ ] **Step 4: Write the component**

Create `web/src/components/disk-breakdown.tsx`:

```tsx
import type { Status, Storage } from "@/api/types"
import { diskParts } from "@/lib/disk"
import { humanBytes } from "@/lib/format"

// Accent and grey tints only: a category is not a status.
const TINT: Record<string, string | undefined> = {
  "build-cache": "bg-primary",
  images: "bg-primary/70",
  containers: "bg-primary/45",
  volumes: "bg-primary/25",
  "package-caches": "bg-muted-foreground/70",
  toolchains: "bg-muted-foreground/45",
  other: "bg-muted-foreground/25",
  used: "bg-primary",
}

export function DiskBreakdown({ status, storage, highWater }: { status: Status; storage: Storage | undefined; highWater: number }) {
  const total = status.disk_total_bytes
  if (total === 0) return <p className="text-sm text-muted-foreground">Not measured yet</p>
  const used = status.disk_used_bytes
  const parts = diskParts(storage, used)
  return (
    <div className="flex flex-col gap-3">
      <p className="flex flex-wrap items-baseline gap-x-2">
        <span className="font-mono text-2xl font-medium">{status.disk_pct}%</span>
        <span className="text-sm text-muted-foreground">{`${humanBytes(used)} of ${humanBytes(total)} used, prunes at ${highWater}%`}</span>
      </p>
      <div className="relative">
        <div
          role="img"
          aria-label={`Disk use: ${parts.map((p) => `${p.label} ${humanBytes(p.bytes)}`).join(", ")}`}
          className="flex h-3 overflow-hidden rounded-[3px] bg-muted"
        >
          {parts.map((p) => (
            <span key={p.key} data-part={p.key} className={TINT[p.key] ?? "bg-muted-foreground/25"} style={{ width: `${(p.bytes / total) * 100}%` }} />
          ))}
        </div>
        <span data-marker="high-water" aria-hidden="true" className="absolute -top-1 h-5 w-0.5 bg-warning" style={{ left: `${highWater}%` }} />
      </div>
      <ul className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm">
        {parts.map((p) => (
          <li key={p.key} className="flex items-center gap-2">
            <span aria-hidden="true" className={`size-2.5 shrink-0 rounded-[2px] ${TINT[p.key] ?? "bg-muted-foreground/25"}`} />
            <span className="flex-1">{p.label}</span>
            <span className="font-mono text-muted-foreground">{humanBytes(p.bytes)}</span>
          </li>
        ))}
      </ul>
    </div>
  )
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `npx vitest run src/components/disk-breakdown.test.tsx && npm run typecheck && npm run lint`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add web/src/lib/disk.ts web/src/components/disk-breakdown.tsx web/src/components/disk-breakdown.test.tsx
git commit -m "feat(web): draw what fills the disk"
```

---

### Task 24: Rebuild the Dashboard page

**Files:**
- Modify: `web/src/pages/dashboard.tsx` (replace)
- Modify: `web/src/test/fixtures.ts` (`authedRoutes`)
- Test: `web/src/pages/dashboard.test.tsx` (replace)

**Interfaces:**
- Consumes: `useActivity` (Task 2), `useActivityWindow` (Task 18), `dashboardSummary` (Task 13), `StatCards` (Task 14), `ActivityPanel` (Task 19), `RepoTable` (Task 22), `DiskBreakdown` (Task 23), `EventList` (Task 12); existing `useStatus`, `useMetrics`, `useConfig`, `useEvents`, `useStorage`, `AddRepoDialog`.
- Produces: the Dashboard page; `authedRoutes()` answers `GET /api/activity` and `GET /api/storage` by default.

**Items:** 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Serve activity and storage in the shared test routes**

In `web/src/test/fixtures.ts`, add these two entries to the object `authedRoutes` returns, after `"GET /api/events": fixtures.events,`:

```ts
    "GET /api/activity": ({ url }: { url: URL }) =>
      ["1h", "3h"].includes(url.searchParams.get("window") ?? "") ? fixtures.activityLanes : fixtures.activityBuckets,
    "GET /api/storage": fixtures.storage,
```

- [ ] **Step 2: Write the failing page tests**

Replace `web/src/pages/dashboard.test.tsx` with:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { beforeEach, describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const region = (name: string) => within(screen.getByRole("region", { name }))

describe("Dashboard page", () => {
  beforeEach(() => localStorage.clear())

  it("sums up the daemon in one sentence", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByText("1 of 2 runners busy. 3 jobs are waiting in darkmem. old-repo: GitHub: not found.")).toBeInTheDocument()
  })

  it("shows the stat cards", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    await screen.findByRole("region", { name: "Runners" })
    expect(region("Runners").getByText("of 2 busy")).toBeInTheDocument()
    expect(region("Waiting jobs").getByText("oldest 3 min")).toBeInTheDocument()
    expect(region("API budget").getByText("4,980")).toBeInTheDocument()
    await waitFor(() => expect(region("Host").getByText("31%")).toBeInTheDocument())
  })

  it("asks for the remembered activity window in the browser's zone", async () => {
    localStorage.setItem("ghr-activity-window", "24h")
    const { calls } = mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByRole("group", { name: "Activity per bucket for the last 24 hours" })).toBeInTheDocument()
    expect(calls.find((c) => c.path === "/api/activity")?.search).toBe("?window=24h&tz=UTC")
  })

  it("switches the activity window and remembers it", async () => {
    const { calls } = mockApi(authedRoutes())
    const { user } = renderApp("/")
    await screen.findByRole("group", { name: "Runner lanes for the last hour" })
    await user.click(screen.getByRole("radio", { name: "7d" }))
    await waitFor(() => expect(calls.some((c) => c.search === "?window=7d&tz=UTC")).toBe(true))
    expect(localStorage.getItem("ghr-activity-window")).toBe("7d")
  })

  it("opens a runner from its running bar", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/")
    await user.click(await screen.findByRole("link", { name: /^darkmem, ci, test, run 42, running/ }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/runners/aaaaaa"))
  })

  it("lists repositories and opens one", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/")
    await screen.findByRole("region", { name: "Repositories" })
    expect(region("Repositories").getByText("Running")).toBeInTheDocument()
    expect(region("Repositories").getByText("2 configured")).toBeInTheDocument()
    expect(region("Repositories").getByRole("link", { name: "Manage" })).toHaveAttribute("href", "/repositories")
    await user.click(region("Repositories").getByRole("link", { name: "darkcloud" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/repositories/darkcloud"))
  })

  it("pauses every repository", async () => {
    const { calls } = mockApi(authedRoutes({ "POST /api/pause-all": () => noContent() }))
    const { user } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Pause all" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/pause-all")).toBe(true))
    expect((await screen.findAllByText("Paused all repositories. Running jobs finish first.")).length).toBeGreaterThan(0)
  })

  it("resumes when every live repository is paused", async () => {
    const repos = fixtures.status.repos.map((r) => ({ ...r, paused: true }))
    const { calls } = mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, repos }, "POST /api/resume-all": () => noContent() }))
    const { user } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Resume all" }))
    await waitFor(() => expect(calls.some((c) => c.path === "/api/resume-all")).toBe(true))
  })

  it("shows disk use", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    await screen.findByRole("region", { name: "Disk" })
    await waitFor(() => expect(region("Disk").getByText("146.0 GB of 240.0 GB used, prunes at 80%")).toBeInTheDocument())
    expect(region("Disk").getByRole("link", { name: "Storage" })).toHaveAttribute("href", "/storage")
    await waitFor(() => expect(region("Disk").getByText("Images")).toBeInTheDocument())
  })

  it("shows the latest events", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    expect(await screen.findByText("runner aaaaaa started")).toBeInTheDocument()
  })

  it("shows the empty states", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, repos: [], instances: [] }, "GET /api/events": [] }))
    renderApp("/")
    expect(await screen.findByText("No repositories yet")).toBeInTheDocument()
    expect(screen.getByText("No events yet.")).toBeInTheDocument()
    expect(screen.getByRole("heading", { name: "Activity" })).toBeInTheDocument()
  })

  it("waits for the daemon while it cannot be reached", async () => {
    mockApi(authedRoutes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    renderApp("/")
    expect(await screen.findByText("Waiting for the daemon")).toBeInTheDocument()
  })

  it("disables its actions while the daemon is unreachable", async () => {
    let fail = false
    mockApi(authedRoutes({ "GET /api/status": () => (fail ? json({ error: "connection refused" }, 502) : fixtures.status) }))
    renderApp("/")
    for (const name of ["Pause all", "Add repository"]) expect(await screen.findByRole("button", { name })).toBeEnabled()
    fail = true
    await waitFor(() => expect(screen.getByRole("button", { name: "Pause all" })).toBeDisabled(), { timeout: 3000 })
    expect(screen.getByRole("button", { name: "Add repository" })).toBeDisabled()
  })

  it("opens the add repository dialog", async () => {
    mockApi(authedRoutes({ "GET /api/repos/available": fixtures.availableRepos }))
    const { user } = renderApp("/")
    await user.click(await screen.findByRole("button", { name: "Add repository" }))
    expect(await screen.findByRole("heading", { name: "Add repository" })).toBeInTheDocument()
  })
})
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `npx vitest run src/pages/dashboard.test.tsx`
Expected: FAIL: the old page has no summary sentence, regions or activity panel.

- [ ] **Step 4: Rewrite the page**

Replace `web/src/pages/dashboard.tsx` with:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link, useNavigate } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useState, type ReactNode } from "react"
import { api } from "@/api/client"
import { keys, useActivity, useConfig, useEvents, useMetrics, useStatus, useStorage } from "@/api/hooks"
import type { RepoStatus } from "@/api/types"
import { ActivityPanel } from "@/components/activity-panel"
import { AddRepoDialog } from "@/components/add-repo-dialog"
import { DiskBreakdown } from "@/components/disk-breakdown"
import { EventList } from "@/components/event-list"
import { RepoTable } from "@/components/repo-table"
import { StatCards } from "@/components/stat-card"
import { dashboardSummary } from "@/lib/summary"
import { useActivityWindow } from "@/lib/use-activity-window"
import { useNow } from "@/lib/use-now"

function allPaused(repos: RepoStatus[]): boolean {
  const live = repos.filter((r) => !r.removing)
  return live.length > 0 && live.every((r) => r.paused)
}

function Section({ title, aside, children }: { title: string; aside?: ReactNode; children: ReactNode }) {
  return (
    <section aria-label={title} className="min-w-0 rounded-[10px] border border-border bg-card p-4">
      <div className="mb-3 flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <h2 className="text-base font-semibold">{title}</h2>
        {aside}
      </div>
      {children}
    </section>
  )
}

export function DashboardPage() {
  const status = useStatus()
  const metrics = useMetrics()
  const config = useConfig()
  const events = useEvents(status.data?.epoch)
  const storage = useStorage(status.data)
  const [selected, select] = useActivityWindow()
  const activity = useActivity(selected)
  const now = useNow()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [adding, setAdding] = useState(false)
  const toggle = useMutation({
    mutationFn: (resume: boolean) => (resume ? api.resumeAll() : api.pauseAll()),
    onSuccess: (_data, resume) => {
      toast.success(resume ? "Resumed all repositories" : "Paused all repositories. Running jobs finish first.")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.status }),
  })

  const st = status.data
  if (!st) {
    return (
      <>
        <PageHeader title="Dashboard" />
        <Spinner label="Waiting for the daemon" />
      </>
    )
  }
  const offline = status.isError
  const paused = allPaused(st.repos)
  const configured = st.repos.filter((r) => !r.removing).length
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Dashboard"
        description={dashboardSummary(st)}
        actions={
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" disabled={offline || toggle.isPending} onClick={() => toggle.mutate(paused)}>
              {paused ? "Resume all" : "Pause all"}
            </Button>
            <Button disabled={offline} onClick={() => setAdding(true)}>
              Add repository
            </Button>
          </div>
        }
      />
      <StatCards status={st} metrics={metrics.data} now={now} />
      <ActivityPanel
        selected={selected}
        onSelect={select}
        activity={activity.data}
        error={activity.error}
        onRetry={() => void activity.refetch()}
        now={now}
        onOpenRunner={(id) => void navigate({ to: "/runners/$id", params: { id } })}
      />
      <div className="grid gap-4 xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
        <Section
          title="Repositories"
          aside={
            <>
              <span className="text-sm text-muted-foreground">{configured} configured</span>
              <Link to="/repositories" className="ml-auto text-sm text-primary hover:underline">
                Manage
              </Link>
            </>
          }
        >
          {st.repos.length === 0 ? (
            <div className="flex flex-col items-start gap-2 py-2">
              <p className="text-sm text-muted-foreground">No repositories yet</p>
              <Button size="sm" disabled={offline} onClick={() => setAdding(true)}>
                Add repository
              </Button>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <RepoTable
                repos={st.repos}
                activity={activity.data?.repos}
                now={now}
                offline={offline}
                onOpen={(name) => void navigate({ to: "/repositories/$name", params: { name } })}
              />
            </div>
          )}
        </Section>
        <div className="flex min-w-0 flex-col gap-4">
          <Section
            title="Disk"
            aside={
              <Link to="/storage" className="ml-auto text-sm text-primary hover:underline">
                Storage
              </Link>
            }
          >
            <DiskBreakdown status={st} storage={storage.data} highWater={config.data?.disk_high_water ?? 80} />
          </Section>
          <Section title="Events">
            <EventList events={events} />
          </Section>
        </div>
      </div>
      <AddRepoDialog open={adding} onClose={() => setAdding(false)} />
    </div>
  )
}
```

- [ ] **Step 5: Run the page tests, then the whole suite**

Run: `npx vitest run src/pages/dashboard.test.tsx` then `npm test` (timeout 600000)
Expected: both pass. `"{configured} configured"` renders as two text nodes inside one `<span>`, which `getByText("2 configured")` matches; if another suite now fails because `authedRoutes` answers `/api/storage` by default, that test relied on the 404: give it an explicit route for the answer it expects.

- [ ] **Step 6: Lint, type-check, build and commit**

Run: `npm run typecheck && npm run lint && npm run build`
Expected: all exit 0.

```bash
git add web/src/pages/dashboard.tsx web/src/pages/dashboard.test.tsx web/src/test/fixtures.ts
git commit -m "feat(web): rebuild the Dashboard around activity"
```

---

### Task 25: Enforce the anti-slop and accessibility rules

**Files:**
- Modify: `web/package.json`, `web/package-lock.json` (through npm)
- Test: `web/src/anti-slop.test.ts`, `web/src/a11y.test.tsx`
- Modify, only if a test finds a violation: the component that causes it.

**Interfaces:**
- Consumes: every file the earlier tasks created or touched; `renderApp`, `authedRoutes`.
- Produces: the anti-slop file list that spec 2 extends.

**Items:** 1, 9

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 1 - coupling 1 - risk 0 = 3

- [ ] **Step 1: Add axe-core**

```bash
npm install -D axe-core@4.13.0
```

- [ ] **Step 2: Write the anti-slop test**

Create `web/src/anti-slop.test.ts`:

```ts
import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import ts from "typescript"
import { describe, expect, it } from "vitest"

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..")

// The files the identity and Dashboard spec created or restyled. Spec 2 adds
// each page it redesigns, until this list is all of src.
const FILES = [
  "public/theme-init.js",
  "src/components/activity-panel.tsx",
  "src/components/brand.tsx",
  "src/components/buckets-chart.tsx",
  "src/components/capacity-bar.tsx",
  "src/components/disk-breakdown.tsx",
  "src/components/event-list.tsx",
  "src/components/lanes-chart.tsx",
  "src/components/mode-control.tsx",
  "src/components/repo-activity-strip.tsx",
  "src/components/repo-actions.tsx",
  "src/components/repo-table.tsx",
  "src/components/result-icon.tsx",
  "src/components/shell.tsx",
  "src/components/sparkline.tsx",
  "src/components/stat-card.tsx",
  "src/components/update-card.tsx",
  "src/components/window-control.tsx",
  "src/api/client.ts",
  "src/api/hooks.ts",
  "src/lib/activity-view.ts",
  "src/lib/capacity.ts",
  "src/lib/disk.ts",
  "src/lib/format.ts",
  "src/lib/status.ts",
  "src/lib/summary.ts",
  "src/lib/use-activity-window.ts",
  "src/lib/use-repo-actions.ts",
  "src/lib/use-width.ts",
  "src/pages/dashboard.tsx",
  "src/pages/login.tsx",
  "src/pages/setup.tsx",
  "src/styles/ghr-theme.css",
  "src/theme.config.ts",
]

const DASH = /[–—]/

function dashLiterals(name: string, text: string): string[] {
  const kind = name.endsWith(".tsx") ? ts.ScriptKind.TSX : name.endsWith(".js") ? ts.ScriptKind.JS : ts.ScriptKind.TS
  const source = ts.createSourceFile(name, text, ts.ScriptTarget.Latest, true, kind)
  const found: string[] = []
  const visit = (node: ts.Node) => {
    const literal =
      ts.isStringLiteral(node) ||
      ts.isNoSubstitutionTemplateLiteral(node) ||
      ts.isTemplateHead(node) ||
      ts.isTemplateMiddle(node) ||
      ts.isTemplateTail(node) ||
      ts.isJsxText(node)
    if (literal && DASH.test(node.text)) found.push(node.text.trim())
    ts.forEachChild(node, visit)
  }
  visit(source)
  return found
}

function gradientArgs(text: string, start: number): string[] {
  const args: string[] = []
  let depth = 0
  let current = ""
  for (let i = start; i < text.length; i++) {
    const ch = text.charAt(i)
    if (ch === "(") depth++
    if (ch === ")") {
      if (depth === 0) break
      depth--
    }
    if (ch === "," && depth === 0) {
      args.push(current.trim())
      current = ""
      continue
    }
    current += ch
  }
  args.push(current.trim())
  return args
}

function styleViolations(text: string): string[] {
  const found: string[] = []
  if (/backdrop-filter|backdrop-blur-/.test(text)) found.push("backdrop blur")
  if (/background-clip:\s*text|bg-clip-text/.test(text)) found.push("background-clip: text")
  for (const m of text.matchAll(/(?:linear|radial)-gradient\(/g)) {
    const args = gradientArgs(text, (m.index ?? 0) + m[0].length)
    const stops = args.filter((a, i) => !(i === 0 && /^(to |at |circle|ellipse|closest|farthest|[-\d.]+(deg|turn|rad|grad))/.test(a)))
    if (stops.length > 1) found.push(`${m[0]}${args.join(", ")})`)
  }
  return found
}

describe("anti-slop rules", () => {
  it("catch what they are meant to catch", () => {
    expect(dashLiterals("x.tsx", 'const a = "one — two"; const b = <p>three – four</p>; const c = `five ${a} — six`')).toHaveLength(3)
    expect(dashLiterals("x.ts", "// a comment — is fine\nconst a = 1")).toEqual([])
    expect(styleViolations("background: linear-gradient(to right, red, blue)")).toHaveLength(1)
    expect(styleViolations("background: radial-gradient(circle, hsl(0 0% 0%), transparent)")).toHaveLength(1)
    expect(styleViolations("background: linear-gradient(red)")).toEqual([])
    expect(styleViolations('className="backdrop-blur-sm"')).toHaveLength(1)
    expect(styleViolations('className="bg-clip-text"')).toHaveLength(1)
  })

  it.each(FILES)("%s follows them", (file) => {
    const text = readFileSync(resolve(root, file), "utf8")
    const dashes = file.endsWith(".css") ? (text.match(/"[^"\n]*[–—][^"\n]*"/g) ?? []) : dashLiterals(file, text)
    expect(dashes).toEqual([])
    expect(styleViolations(text)).toEqual([])
  })
})
```

- [ ] **Step 3: Write the accessibility test**

Create `web/src/a11y.test.tsx`:

```tsx
import { screen } from "@testing-library/react"
import axe from "axe-core"
import { beforeEach, describe, expect, it } from "vitest"
import { mockApi } from "@/test/api"
import { authedRoutes } from "@/test/fixtures"
import { renderApp } from "@/test/render"

describe("Dashboard accessibility", () => {
  beforeEach(() => localStorage.clear())

  it.each([
    ["1h", "Runner lanes for the last hour"],
    ["24h", "Activity per bucket for the last 24 hours"],
  ])(
    "has no axe violations with the %s window",
    async (win, chart) => {
      localStorage.setItem("ghr-activity-window", win)
      mockApi(authedRoutes())
      renderApp("/")
      await screen.findByRole("group", { name: chart })
      await screen.findByText("146.0 GB of 240.0 GB used, prunes at 80%")
      const main = document.getElementById("main-content")
      if (!main) throw new Error("no main content")
      // jsdom computes no layout or colour, so contrast is checked by the
      // palette test in src/styles/theme.test.ts instead.
      const results = await axe.run(main, { rules: { "color-contrast": { enabled: false } } })
      expect(results.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)).toEqual([])
    },
    30_000,
  )
})
```

- [ ] **Step 4: Run both tests**

Run: `npx vitest run src/anti-slop.test.ts src/a11y.test.tsx`
Expected: PASS. If a file fails the anti-slop check, fix the copy or class in that file (never shorten the list or loosen a pattern). If axe reports a violation, fix it in the ghr component that renders the reported node (for example a missing name or an invalid ARIA attribute), and rerun; a violation inside a darkraise-ui component that ghr cannot change goes to your human partner with the axe rule id.

- [ ] **Step 5: Run the whole suite and the checks**

Run: `npm test` (timeout 600000), then `npm run typecheck && npm run lint && npm run build`
Expected: all pass.

- [ ] **Step 6: Commit**

```bash
git add web/package.json web/package-lock.json web/src/anti-slop.test.ts web/src/a11y.test.tsx
git commit -m "test(web): check the anti-slop rules and axe"
```

If Step 4 needed fixes, add those files to the same commit only when they are one-line copy or ARIA fixes; otherwise commit them first as `fix(web): <what was wrong>`.
