# ghr web UI pages, plan 3: repositories, toolchains, storage and settings

> **For agentic workers:** REQUIRED SUB-SKILL: the skill the **Execution:** line names — dr-superpowers:subagent-driven-development for `subagent`, dr-superpowers:executing-plans for `inline`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild Repositories and the repository page, Toolchains and Storage, and Settings on plan 2's page kit, then finish the page-wide rules: the dialog copy, the Docker root path, the badge removal, and an anti-slop check that walks every source file.

**Architecture:** Each page drops its card stack for plan 2's `Section`, `FieldGroup` and `Field`, with pure helpers in `lib/` (`repos.ts`, `toolchains.ts`, `storage.ts`, `token.ts`) feeding one-sentence page descriptions and table cells. Repositories reuses the Dashboard's `RepoTable` with a `"full"` column set; the repository page adds a one-repository jobs chart (`/activity?repo=`) and a form of field groups with a section index, as Settings does. Old components are replaced file by file, so every task leaves the suite green, and the last task deletes the badge helpers and turns the anti-slop file list into a walk of `web/src`.

**Tech Stack:** React 19, TypeScript, TanStack Router and Query, darkraise-ui, Lucide icons, Vitest with Testing Library and axe-core.

**Spec:** docs/superpowers/specs/2026-10-08-ghr-web-ui-pages-design.md (§5, §6, §7, the dialog rules of §1.2, §2.5's display of `disk_root`, and the anti-slop and deletion parts of §8; delivery step 3 of §9), plus plan 2's "Deferred to plan 3" list (docs/superpowers/plans/2026-10-08-ghr-web-ui-pages-2-kit-runners-history.md)

**Execution:** inline — `claude --model sonnet --effort medium` — no task is heavy (every total is 4 or less, no risk 3), so nothing is delegated; the highest self-implemented total is 4 (Sonnet medium).

**Plan review:** 2026-10-08 — dr-superpowers:judge-opus — executability 16 / coherence 18 / coverage 19 / assumptions 15 (round 1)

## Global Constraints

- Work on the branch `feat/web-ui-pages`, after plan 1 (docs/superpowers/plans/2026-10-08-ghr-web-ui-pages-1-backend.md) and plan 2 (docs/superpowers/plans/2026-10-08-ghr-web-ui-pages-2-kit-runners-history.md) are complete on it. Nothing is pushed or merged without the owner: pushing master cuts a release.
- Only files under `web/src` change. No new npm dependencies.
- Spec 1's rules bind every file touched: one accent colour; borders, never shadows; 6px radius on controls and 10px on surfaces; no pills or kit `Badge` around plain text (`TagField`'s removable chips stay); no "·" separators; no em or en dashes in UI strings; no "✖", "⚠", "●" or "…" glyphs in copy this plan writes; mono only for data (IDs, numbers, times, sizes, versions, paths, labels); sentence case. The setup wizard's own lines ("Starting ghr…") stay: the spec keeps setup out of scope apart from shared components and button labels.
- Copy uses today's formatters from `web/src/lib/format.ts`: `ago()` ("2h ago"), `dur()`, `hhmm()`, `monthDay()` ("Oct 3"), `plural()` ("1 job", "2 jobs"), `humanBytes()` ("190.0 MB"), `dateTime()`.
- Toasts and inline messages that this plan rewrites keep their words and start with a capital letter ("Settings not saved", "Saved, but re-reading the config failed: …"). Error text from the daemon is shown as given.
- Loading states use the kit's `Spinner` with a sentence-case `label` and no ellipsis. Errors use plan 2's `ErrorLine` (role `alert`, a Lucide icon, **Try again** when a retry makes sense).
- Every control that changes the daemon is disabled while `useStatus().isError` is true. Navigation and copy actions stay enabled.
- Icon buttons carry an `aria-label` naming their row ("Remove node 22.11.0"), plus a tooltip.
- Lucide icons are size 15, except 13 beside `text-xs` copy.
- Each `ConfirmDialog` gets a short question naming the thing as its `title`, the consequence as its `body`, and an action label that repeats the verb.
- Until Task 22 replaces it with a directory walk, each new or rebuilt non-test source file is added to the `FILES` list in `web/src/anti-slop.test.ts` in the task that creates it, and a deleted file is removed from it (keep the list's grouping: components, then api and lib, then pages, then styles).
- Before each commit, run in `web/`: `npm run typecheck`, `npm run lint`, and `npx vitest run`. All three must pass. `react-refresh/only-export-components` runs with `--max-warnings 0`, so a component file exports only components and types; exported helper functions and hooks live in `lib/`.
- Comments only where the why is non-obvious. English only.
- Commits: `<type>(<scope>): <subject>`, subject at most 50 characters, imperative, no period.

## Contracts

**From plan 2** (exists before Task 1; restated so each task can use it without reading plan 2)

```tsx
// web/src/components/page/section.tsx: <section aria-labelledby> named by its h2
export function Section(props: { title: string; aside?: ReactNode; children: ReactNode; className?: string }): JSX.Element
// web/src/components/page/state-text.tsx: the state in sentence case, coloured by stateTone
export function StateText(props: { state: string; label?: string }): JSX.Element
// web/src/components/page/error-line.tsx: role="alert", CircleAlert icon, "Try again" button when onRetry is given
export function ErrorLine(props: { children: ReactNode; onRetry?: () => void }): JSX.Element
// web/src/components/page/field.tsx
export function FieldGroup(props: { id: string; title: string; note?: string; children: ReactNode }): JSX.Element
export function Field(props: { label: string; htmlFor?: string; help?: ReactNode; error?: string; changed?: boolean; children: ReactNode }): JSX.Element
// web/src/components/page/section-nav.tsx: renders nothing below 1280px
export interface NavItem { id: string; title: string }
export function SectionNav(props: { items: readonly NavItem[] }): JSX.Element | null
// web/src/lib/status.ts
export type Tone = "accent" | "warn" | "bad" | "ok" | "muted"
export function stateTone(state: string): Tone // case-insensitive; unknown states are "muted"
// web/src/lib/use-media-query.ts
export const WIDE = "(min-width: 1024px)"
export const WIDEST = "(min-width: 1280px)"
export function useMediaQuery(query: string): boolean
// web/src/api/hooks.ts
export function useActivity(window: ActivityWindow, repo?: string) // GET /api/activity?window=&tz=[&repo=]
// web/src/components/buckets-chart.tsx
export function BucketsChart(props: { activity: Activity; now: number; width: number; tracks?: "all" | "jobs"; picked?: string | null; onPick?: (b: ActivityBucket) => void }): JSX.Element
// web/src/test/media.ts: (min-width: Npx) queries match against width; restored after the test
export function setViewport(width: number): { resize(width: number): void }
```

- `FieldGroup` renders `<section id={id} aria-labelledby={`${id}-title`}>`; its `h2` has `id={`${id}-title`}` and `tabIndex={-1}`, then the optional note, then its children inside a `div` with 1px `divide-y` dividers. A child that is not a `Field` adds its own `py-3`.
- `Field` renders a `<label htmlFor>` when `htmlFor` is given and a plain `span` otherwise; the help line is `text-muted-foreground`, the error line `text-destructive`, and `changed` shows the word "Changed" in `text-warning`.
- `StateText` tones: accent (`text-primary`) for busy, running, active, online, matched; warn (`text-warning`) for waiting, starting, queued, expires soon, unverified, refused, interrupted; bad (`text-destructive`) for error, failed, failure, offline, unmatched, rejected; ok (`text-success`) for ok, success, valid, up to date; muted for anything else.
```tsx
// web/src/components/save-bar.tsx: "1 unsaved change" / "N unsaved changes" with Discard and Save changes; renders nothing at count 0
export function SaveBar(props: { count: number; saving: boolean; disabled?: boolean; onSave: () => void; onDiscard: () => void }): JSX.Element | null
// lists up to three "; "-separated reasons as plain text, then "and N more"
export function RejectedAlert(props: { message: string }): JSX.Element
// web/src/components/unsaved-guard.tsx: asks before leaving with changes; lets same-pathname navigations through
export function UnsavedGuard(props: { count: number; page: string; saving: boolean; onSave: () => Promise<boolean>; onDiscard: () => void }): JSX.Element | null
```
- `web/src/a11y.test.tsx` has a helper `violations(): Promise<string[]>` that runs axe on `#main-content` with colour contrast off, and imports `fixtures`, `mockApi`, `authedRoutes`, `renderApp` and `setViewport`.
- `ConfirmDialog` (`components/confirm-dialog.tsx`) takes `Confirm { title: string; body?: string; action: string; destructive?: boolean; run: () => void }`.

**Repositories** (Tasks 2 to 8)

```ts
// web/src/lib/repos.ts
export function weekRate(week: ActivityWeek | undefined): number | undefined // whole percent of succeeded / (succeeded + failed); undefined when both are 0
export function weekText(week: ActivityWeek): string                       // "46 succeeded, 2 failed, 1 cancelled in 7 days"
export function reposSummary(status: Status): string                       // "2 configured, 1 running, 1 paused"; counts of 0 left out; "" when all are 0
export function runnersText(repo: RepoStatus): string                      // "1 of 2", or "1, no limit" when max is 0
export function repoDescription(repo: RepoStatus): string                  // "Running 1 of 2 runners, 3 jobs waiting" | "Idle" | "Paused" | the error
```

```tsx
// web/src/components/runner-meter.tsx: one aria-hidden cell per allowed runner (data-slot="runner", data-filled="true" when busy); nothing when max is 0
export function RunnerMeter(props: { active: number; max: number }): JSX.Element | null
// web/src/components/repo-table.tsx (props added; defaults keep the Dashboard unchanged)
export function RepoTable(props: { repos: RepoStatus[]; activity: ActivityRepo[] | undefined; now: number; offline: boolean; onOpen: (name: string) => void; columns?: "compact" | "full"; config?: Config }): JSX.Element
// web/src/components/label-check-group.tsx: FieldGroup id "workflow-labels", title "Workflow labels"
export function LabelCheckGroup(props: { name: string; effective: string[]; degraded: boolean; degradedReason?: string; disabled: boolean; onAddLabel: (label: string) => void }): JSX.Element
// web/src/components/registrations-group.tsx: FieldGroup id "registrations", title "GitHub registrations"
export function RegistrationsGroup(props: { name: string; degraded: boolean; degradedReason?: string; disabled: boolean }): JSX.Element
// web/src/components/repo-activity.tsx: Section "Last 24 hours" with the jobs chart and the facts list
export function RepoActivity(props: { name: string; repo: RepoStatus | undefined; now: number }): JSX.Element
```

- `useRepoActions(repo, offline).askRemove()` opens `{ title: "Remove {name}?", body: "Its running jobs finish first.", action: "Remove", destructive: true }` (Task 4).

**Toolchains** (Tasks 10 to 13)

```ts
// web/src/lib/toolchains.ts
export function lastDotnetMajor(t: Toolchain, all: Toolchain[]): string // unchanged
export function queueText(ops: Operations): string                      // "Installing node 24, extracting, 1 more queued" | "2 queued" | ""
export interface ToolGroup { tool: string; rows: Toolchain[] }
export function groupByTool(toolchains: Toolchain[]): ToolGroup[]       // tools in first-seen order
export interface CachePart { key: string; label: string; bytes: number; slot: 1 | 2 | 3 | 4 | 5 }
export function cacheParts(storage: Storage): CachePart[]               // the four largest tools get slots 1 to 4 (drawn bg-chart-1..4); the rest plus other_tool_cache share "Other" (key "__other") in slot 5
export function toolchainsSummary(storage: Storage): string             // "1 installed, 270.0 MB" | "Not measured yet"
// web/src/lib/use-popular-set.ts
export function usePopularSet(): { confirm: Confirm | null; close: () => void; ask: () => void }
```

```tsx
// web/src/components/toolchain-actions.tsx: Install (filled) and Install popular set (outline), with their dialogs
export function ToolchainActions(props: { offline: boolean }): JSX.Element
// web/src/components/size-bar.tsx: the size in mono with a relative accent bar on bg-muted
export function SizeBar(props: { bytes: number; longest: number }): JSX.Element
// web/src/components/toolchain-sections.tsx
export function InstalledSection(props: { storage: Storage; status: Status | undefined; offline: boolean }): JSX.Element // Section "Installed"
export function ToolCacheSection(props: { storage: Storage }): JSX.Element | null                                        // Section "Tool cache"; null when the cache is empty
// web/src/components/refused-hint.tsx (props unchanged): "{what}: refused while 1 job runs" with a warn TriangleAlert, muted text
export function RefusedHint(props: { what: string; status: Status | undefined }): JSX.Element | null
```

**Storage** (Tasks 14 to 17)

```ts
// web/src/lib/storage.ts
export function storageSummary(status: Status, highWater: number): string // "Docker uses 61% of /var/lib/docker and prunes above 80%" | "Not measured yet"
export interface PruneAsk { title: string; body: string; action: string; destructive: boolean }
export interface PruneRow extends PruneAsk { scope: Exclude<PruneScope, "standard">; text: string }
export function pruneRows(storage: Storage, config: Config | undefined): PruneRow[]
export function standardPrune(config: Config | undefined): PruneAsk
export function lastPruneText(p: LastPrune): string                      // "Last prune: manual build-cache-all at 13:16, ok"
// web/src/lib/use-prune.ts
export function usePrune(): { confirm: Confirm | null; close: () => void; ask: (scope: PruneScope, c: PruneAsk) => void }
```

```tsx
// web/src/components/storage-disk.tsx: Section "Disk"
export function DiskSection(props: { storage: Storage; status: Status; highWater: number; className?: string }): JSX.Element
// web/src/components/prune-section.tsx: Section "Prune"
export function PruneSection(props: { storage: Storage; status: Status | undefined; config: Config | undefined; disabled: boolean; className?: string }): JSX.Element
// web/src/components/storage-tables.tsx
export function PackageCachesSection(props: { storage: Storage; status: Status | undefined; offline: boolean; className?: string }): JSX.Element
export function OperationsSection(props: { storage: Storage; className?: string }): JSX.Element
```

**Settings** (Tasks 18 to 21)

```ts
// web/src/lib/token.ts
export function tokenWord(t: TokenStatus, now: number): string // "valid" | "expires soon" (ok within 14 days) | t.state
export function expiryText(t: TokenStatus, now: number): string // "Expires Dec 2, in 60 days" | "Expires Oct 4, in less than a day" | "Expired Oct 1" | "Expiry unknown"
export function rateText(t: TokenStatus | undefined): string     // "4,980 of 5,000, resets 14:45" | "" before it is known
```

```tsx
// web/src/components/settings-form.tsx (props unchanged): FieldGroups "general", "timing", "disk", "runner-defaults"
export function SettingsSections(props: { form: SettingsForm }): JSX.Element
// web/src/components/token-section.tsx: FieldGroup id "token", title "GitHub token"
export function TokenSection(): JSX.Element
// web/src/components/runner-section.tsx: FieldGroup id "runner", title "Runner and config"
export function RunnerSection(): JSX.Element
// web/src/components/account-section.tsx: FieldGroup id "account", title "Account"
export function AccountSection(): JSX.Element
```

## Assumptions (evidence)

- Plans 1 and 2 have landed on `feat/web-ui-pages`: `ActivityRepo.week`, `Status.disk_root` (the status fixture sets `/var/lib/docker`), `Step` times, `useActivity(window, repo)`, and every name under Contracts, From plan 2 (docs/superpowers/plans/2026-10-08-ghr-web-ui-pages-1-backend.md Task 8, step "In `web/fixtures_test.go`"; docs/superpowers/plans/2026-10-08-ghr-web-ui-pages-2-kit-runners-history.md, Contracts). Plan 2 Task 13 gives `a11y.test.tsx` its `violations()` helper and the `fixtures` and `setViewport` imports.
- The activity fixtures' one repository entry is darkmem, with `week` `{1, 1, 0}` after plan 1 Task 5, so its 7-day rate is 50% (plan 1, Assumptions).
- Fixture facts the tests rely on, read on 2026-10-08: status `now` 2026-10-03T14:05Z, darkmem active 1 of 2 with 3 queued and last job `build #41` finished 13:40, darkcloud paused, old-repo paused, removing and in error, one busy and one idle instance, `runner_update` queued with a deadline (web/src/api/fixtures/status.json); config mode `queue`, owner `darkraise`, global labels `homelab`, darkmem max 2, warm 1, labels `gpu`, prefixes `darkmem-`, darkcloud with no warm, `build_cache_keep` `20GB` (config.json); storage node 22.11.0 x64 190 MB installed 2026-09-30, PyPy 80 MB, NuGet present 3.6 GB 41000 files last written 13:05, Cargo absent, Images 8.1 GB, Build Cache reclaimable 380 MB, build cache type `regular`, no Local Volumes row, current op install node 24 "extracting" with 1 queued, recent op `clear nuget` at 13:06 ok, last prune manual build-cache-all finished 13:16 ok freeing 6.8 MB, measured 14:03 (storage.json); token ok, rate 4980 of 5000 resetting 14:45 (token.json); label check group `homelab, self-hosted` with 12 jobs last seen 13:05 (label-check.json); registration `ghr-aaaaaa` online and busy (registrations.json); available repos are all private (available-repos.json); version 22.11.0 is lts (toolchain-choices.json).
- Tests run with `TZ=UTC` (web/vite.config.ts:23), and `useNow` follows `status.now` (web/src/lib/use-now.ts), so `hhmm` and `monthDay` in tests read UTC.
- darkraise-ui's `Spinner` renders its `label` as visible text (node_modules/darkraise-ui/dist/components/spinner/Spinner.d.ts:7-8); its `Button` `loading` prop disables the button and sets `aria-busy` (components/button/Button.d.ts:8-15); `PageHeader`'s `description` is a string (layout/types.d.ts:56-61).
- The `bg-chart-1` to `bg-chart-5` utilities exist: darkraise-ui's theme maps `--color-chart-N` to `--chart-N` (grep of node_modules/darkraise-ui/dist on 2026-10-08), and web/src/styles/ghr-theme.css:72-76 and :124-128 set `--chart-1` to `--chart-5` in both modes. Tailwind 4 (`@tailwindcss/vite`, web/vite.config.ts:3) scans the component file that names them.
- `useWidth` never reports less than 720px (web/src/lib/use-width.ts:5), which is why the repository page's facts move beside the chart only at 1280px.
- Testing Library's `getByText` matches an element by its own text nodes (`getNodeText`, https://testing-library.com/docs/dom-testing-library/api-custom-queries/#getnodetext), so a cell holding `<span>50%</span><span class="sr-only">…</span>` is found through the inner span, and `ErrorLine`'s message is found through its inner `span`.
- `DiskBreakdown` repeats sizes in its legend (web/src/components/disk-breakdown.tsx:40-47; "Package caches" sums the caches, web/src/lib/disk.ts:19), so Storage tests scope size assertions to a table row or region (Tasks 16 and 17).
- The runner version line checks `running`, `queued`, `deadline`, then `checked_at`, in the order today's card uses (web/src/components/maintenance-card.tsx:24-68) and spec §7 lists; "Up to date" therefore means "checked, no deadline". The daemon reports a newer runner through `deadline` (spec §7's states); a newer `latest` without a deadline would also read "Up to date", as it does today.
- The shell owns Log out: one "Log out" button that navigates with `ignoreBlocker: true` (web/src/components/shell.tsx:71-78, :102), which `shell.test.tsx` finds as a single button.
- The shell's runner update card repeats **Queue update** and **Cancel update** outside `main` (web/src/components/update-card.tsx:72-86), so the runner section tests query inside the "Runner and config" region.
- `ConfirmDialog` already takes a `body` (web/src/components/confirm-dialog.tsx:13-19, :36); no change to it is needed.
- `react-refresh/only-export-components` warns on a component file that exports a function, and lint runs with `--max-warnings 0` (plan 2, Assumptions), so `usePopularSet` and `usePrune` live in `lib/`.
- Deletion safety (grep on 2026-10-08): `repo-actions.tsx` is used only by `pages/repositories.tsx`; `repo-summary.tsx` only by the Repositories and repository pages; `repoState`, `maxText`, `ACTIVITY_LIMIT` and `useRepoActivity` only by `repo-summary.tsx` and their tests; `lib/activity.ts` only by `repo-summary.tsx`; `toolchains-card.tsx` only by `pages/toolchains.tsx` and `pages/setup.tsx`; `storage-cards.tsx` only by `pages/storage.tsx`; `token-card.tsx`, `maintenance-card.tsx` and `account-card.tsx` only by `pages/settings.tsx`; `label-check-card.tsx` and `registrations-card.tsx` only by `pages/repository.tsx`; `stepper.tsx` by nothing.
- Kit `Badge` importers on 2026-10-08: add-repo-dialog, install-dialog, label-check-card, maintenance-card, registrations-card, repo-summary, runners-table, state-badge, storage-cards, tag-field, token-card, lib/status, history, runner-detail and components.test. Plan 2 removes runners-table, runner-detail and History's; this plan removes the rest except `tag-field.tsx`.
- A scan of every non-test source file on 2026-10-08 with the anti-slop test's own `dashLiterals` found dash literals only in runners-table.tsx, runner-detail.tsx, runners.tsx and query.ts (plan 2 rewrites or deletes each) and in token-card.tsx, lib/activity.ts, lib/toolchains.ts and pages/storage.tsx (Tasks 19, 9, 10 and 16 replace each); no file used a Tailwind gradient utility. Task 22's walk therefore passes once both plans land, and its Step 2 catches anything missed.
- Plan 2 defect, reported to the owner: plan 2 Task 4's `RejectedAlert` drops the "✖" prefix, but `web/src/pages/repository.test.tsx:105` and `web/src/pages/settings.test.tsx:124-125` still assert `"✖ …"` strings that plan 2 Task 4 does not update. Plan 2's executor will meet that failure at its Task 4 and must fix those three assertions there. This plan's Tasks 8 and 18 write the bare strings either way.
- Rulings made while planning (spec silent):
  - Toast and form messages keep their words and gain a capital first letter (Global Constraints); field errors from `lib/duration.ts` ("poll_interval must be at least 5s") start with a key name and stay as they are.
  - The Warm column shows the effective value, the configured `warm` or the default of 1, and is blank for a repository missing from `/config`.
  - `reposSummary` counts only repositories not being removed, for all three numbers.
  - The 7 days column's hidden sentence is rendered only when a rate is shown.
  - `RefusedHint` fixes its grammar: "Remove: refused while 1 job runs".
  - Every prune toasts "Prune started"; queued operations toast "Queued: {what}".
  - Settings' header reads "Serving {owner} in {mode} mode", the one live summary the page has.
  - The repository facts list shows "None yet" for a repository with no finished job.
- No task carries an `**Executor:**` line: the only registered external executor, codex, reported `lane=false reason=plugin-not-enabled` on 2026-10-08.

## Task index

1. Restyle the Add repository and Install dialogs
2. Add the repository summaries
3. Add the full columns to the repository table
4. Rebuild the Repositories page
5. Rebuild the workflow labels group
6. Rebuild the registrations group
7. Show a repository's last 24 hours
8. Rebuild the repository page
9. Delete the old activity summary
10. Rework the toolchain helpers
11. Share the install actions
12. Rebuild the installed toolchains list
13. Draw the tool cache bar
14. Show the Docker root path on the Dashboard
15. Add the Storage helpers
16. Rebuild the disk and prune sections
17. Rebuild the package caches and operations
18. Rebuild the Settings config sections
19. Rebuild the GitHub token section
20. Rebuild the runner and config section
21. Rebuild the account section and the Settings page
22. Retire the badges and walk every source file

---

### Task 1: Restyle the Add repository and Install dialogs

**Files:**
- Modify: `web/src/components/add-repo-dialog.tsx` (replace)
- Modify: `web/src/components/install-dialog.tsx` (replace)
- Modify: `web/src/pages/setup.tsx:142` and `web/src/pages/repositories.tsx:32` (button label), `web/src/components/toolchains-card.tsx:96` (button label)
- Test: `web/src/components/add-repo-dialog.test.tsx`, `web/src/components/install-dialog.test.tsx`, `web/src/pages/setup.test.tsx:133`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `ErrorLine` (Contracts, From plan 2).
- Produces: nothing new; `AddRepoDialog` and `InstallDialog` keep their props.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

In `web/src/components/add-repo-dialog.test.tsx`, make these exact replacements (every occurrence):
- `"+ Add repository"` → `"Add repository"`
- `"pick one below"` → `"Pick one below"`
- `"⚠ self-hosted runners on a public repo can run anyone's code"` → `"Self-hosted runners on a public repo can run anyone's code"`
- `"no match"` → `"No match"`
- `"nothing to pick"` → `"Nothing to pick"`
- `"✖ connection refused"` → `"connection refused"`
- `"✖ repo new-repo is already configured"` → `"repo new-repo is already configured"`
- `"added new-repo"` → `"Added new-repo"`

Then, in the test "moves between the pickable repos with the arrow keys, Home and End", add after `const option = …`:

```tsx
    expect(within(option("gamma")).getByText("public")).toHaveClass("text-muted-foreground")
    expect(within(option("alpha")).queryByText("private")).toBeNull()
```

and append inside `describe("Add repository dialog", …)`:

```tsx
  it("shows a busy Add while the request runs", async () => {
    mockApi(routes({ "POST /api/repos": () => new Promise<Response>(() => {}) }))
    const { dialog, user } = await open()
    await user.click(dialog.getByRole("option", { name: /new-repo/ }))
    await user.click(dialog.getByRole("button", { name: "Add" }))
    // The kit's loading state marks the button aria-busy and keeps its label.
    await waitFor(() => expect(dialog.getAllByRole("button").filter((b) => b.getAttribute("aria-busy") === "true")).toHaveLength(1))
    expect(dialog.getAllByRole("button").find((b) => b.getAttribute("aria-busy") === "true")).toBeDisabled()
    expect(dialog.queryByText(/Adding/)).toBeNull()
  })
```

In `web/src/components/install-dialog.test.tsx`, make these exact replacements (every occurrence):
- `{ name: "Install…" }` → `{ name: "Install" }`
- `"pick one below, or type a version"` → `"Pick one below, or type a version"`
- `"no match"` → `"No match"`
- `"✖ upstream down"` → `"upstream down"`
- `{ name: "Retry" }` → `{ name: "Try again" }`
- `'✖ unknown toolchain "node"'` → `'unknown toolchain "node"'`
- `"✖ connection refused"` → `"connection refused"`
- `"queued: install node 22.11.0"` → `"Queued: install node 22.11.0"`

and replace `expect(await dialog.findByText("lts")).toBeInTheDocument()` with:

```tsx
    expect(await dialog.findByText("lts")).toHaveClass("text-muted-foreground")
```

In `web/src/pages/setup.test.tsx:133`, change `{ name: "+ Add repository" }` to `{ name: "Add repository" }`.

The "busy Add" test relies on darkraise-ui's `Button` `loading` prop, which disables the button and sets `aria-busy` (node_modules/darkraise-ui/dist/components/button/Button.d.ts:8-15).

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/components/add-repo-dialog.test.tsx src/components/install-dialog.test.tsx src/pages/setup.test.tsx`
Expected: FAIL: no button is named "Add repository" or "Install", and the dialogs still show "✖", "⚠", badges and lower-case copy.

- [ ] **Step 3: Write the implementation**

Replace `web/src/components/add-repo-dialog.tsx` with:

```tsx
import { useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "darkraise-ui/components/dialog"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { Switch } from "darkraise-ui/components/switch"
import { TriangleAlert } from "lucide-react"
import { useState, type KeyboardEvent } from "react"
import { api } from "@/api/client"
import { keys, useAvailableRepos, useStatus } from "@/api/hooks"
import type { AddRepoRequest } from "@/api/types"
import { ErrorLine } from "@/components/page/error-line"
import { TagField } from "@/components/tag-field"
import { errorText } from "@/query"

function clampMax(text: string): number {
  return Math.min(99, Math.max(0, Math.trunc(Number(text))))
}

export function AddRepoDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) onClose()
      }}
    >
      {/* Mounted only while open: each opening starts clean, and a request
          that ends after closing has nowhere to leave its error. */}
      <DialogContent>{open && <AddRepoForm onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}

const KEY_STEP: Record<string, (i: number, n: number) => number> = {
  ArrowDown: (i, n) => Math.min(n - 1, i + 1),
  ArrowUp: (i) => Math.max(0, i - 1),
  Home: () => 0,
  End: (_i, n) => n - 1,
}

function moveFocus(e: KeyboardEvent<HTMLDivElement>) {
  const step = KEY_STEP[e.key]
  if (!step) return
  e.preventDefault()
  const options = Array.from(e.currentTarget.querySelectorAll<HTMLButtonElement>('[role="option"]:not(:disabled)'))
  if (options.length === 0) return
  const at = options.findIndex((o) => o === document.activeElement)
  options[step(at, options.length)]?.focus()
}

function AddRepoForm({ onClose }: { onClose: () => void }) {
  const repos = useAvailableRepos(true)
  const status = useStatus()
  const queryClient = useQueryClient()
  const [filter, setFilter] = useState("")
  const [picked, setPicked] = useState("")
  const [max, setMax] = useState("")
  const [labels, setLabels] = useState<string[]>([])
  const [allowPublic, setAllowPublic] = useState(false)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function add() {
    if (!picked) return setError("Pick a repository first")
    const req: AddRepoRequest = { name: picked, allow_public: allowPublic }
    if (max.trim() !== "" && Number.isFinite(Number(max))) req.max = clampMax(max)
    if (labels.length > 0) req.labels = labels
    setBusy(true)
    setError("")
    try {
      await api.addRepo(req)
    } catch (err) {
      setError(errorText(err))
      return
    } finally {
      setBusy(false)
    }
    toast.success(`Added ${picked}`)
    void queryClient.invalidateQueries({ queryKey: keys.status })
    void queryClient.invalidateQueries({ queryKey: keys.config })
    onClose()
  }

  const needle = filter.trim().toLowerCase()
  const items = (repos.data ?? []).filter((r) => r.name.toLowerCase().includes(needle))
  const tabStop = items.find((r) => r.name === picked && !r.configured)?.name ?? items.find((r) => !r.configured)?.name
  let picker
  if (repos.isError) {
    picker = <ErrorLine onRetry={() => void repos.refetch()}>{errorText(repos.error)}</ErrorLine>
  } else if (!repos.data) {
    picker = <Spinner label="Loading repositories" />
  } else {
    picker = (
      <div className="flex flex-col gap-2">
        <Input aria-label="Filter repositories" placeholder="Type to filter" value={filter} onChange={(e) => setFilter(e.target.value)} />
        {repos.data.length === 0 && <p className="text-sm text-muted-foreground">Nothing to pick</p>}
        {repos.data.length > 0 && items.length === 0 && <p className="text-sm text-muted-foreground">No match</p>}
        {items.length > 0 && (
          <div role="listbox" aria-label="Repositories" className="max-h-64 overflow-auto rounded-md border" onKeyDown={moveFocus}>
            {items.map((r) => (
              <button
                key={r.name}
                type="button"
                role="option"
                aria-selected={picked === r.name}
                disabled={r.configured}
                tabIndex={r.name === tabStop ? 0 : -1}
                onClick={() => setPicked(r.name)}
                className="flex w-full items-center gap-2 px-2 py-1 text-left text-sm hover:bg-muted focus-visible:bg-muted focus-visible:outline-none disabled:opacity-50 aria-selected:bg-muted"
              >
                <span>{r.name}</span>
                {!r.private && <span className="text-muted-foreground">public</span>}
                {r.configured && <span className="text-muted-foreground">added</span>}
              </button>
            ))}
          </div>
        )}
      </div>
    )
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Add repository</DialogTitle>
      </DialogHeader>
      <DialogBody className="flex flex-col gap-4">
        <div className="flex flex-col gap-1">
          <span className="text-sm font-medium">Repository</span>
          {picked ? <strong>{picked}</strong> : <span className="text-sm text-muted-foreground">Pick one below</span>}
        </div>
        {picker}
        <div className="flex flex-col gap-1">
          <Label htmlFor="add-max">Max</Label>
          <Input
            id="add-max"
            type="number"
            min={0}
            max={99}
            placeholder={status.data?.mode === "all" ? "∞ (default)" : "1 (default)"}
            value={max}
            onChange={(e) => setMax(e.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1">
          <span className="text-sm font-medium">Labels</span>
          <TagField label="Labels" value={labels} onChange={setLabels} />
        </div>
        <div className="flex items-center gap-2">
          <Switch id="allow-public" checked={allowPublic} onCheckedChange={setAllowPublic} />
          <Label htmlFor="allow-public">Allow public repo</Label>
        </div>
        <p className="flex items-center gap-1.5 text-sm text-muted-foreground">
          <TriangleAlert size={15} aria-hidden="true" className="shrink-0 text-warning" />
          Self-hosted runners on a public repo can run anyone's code
        </p>
        {error && <ErrorLine>{error}</ErrorLine>}
      </DialogBody>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button loading={busy} disabled={!picked || busy} onClick={() => void add()}>
          Add
        </Button>
      </DialogFooter>
    </>
  )
}
```

Replace `web/src/components/install-dialog.tsx` with:

```tsx
import { useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { Dialog, DialogBody, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "darkraise-ui/components/dialog"
import { Input } from "darkraise-ui/components/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useToolchainChoices } from "@/api/hooks"
import { ErrorLine } from "@/components/page/error-line"
import { errorText } from "@/query"

const TOOLS = [
  ["node", "Node.js"],
  ["python", "Python"],
  ["go", "Go"],
  ["java", "Java (Temurin)"],
  ["dotnet", ".NET SDK"],
] as const

export function InstallDialog({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) onClose()
      }}
    >
      <DialogContent>{open && <InstallForm onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}

function InstallForm({ onClose }: { onClose: () => void }) {
  const queryClient = useQueryClient()
  const [tool, setTool] = useState("node")
  const [filter, setFilter] = useState("")
  const [picked, setPicked] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const choices = useToolchainChoices(tool, true)
  const target = picked ?? filter.trim()
  const all = choices.data ?? []
  const shown = all.filter((c) => c.version.toLowerCase().includes(filter.trim().toLowerCase()))

  async function submit() {
    setError("")
    setBusy(true)
    try {
      await api.installToolchain(tool, target)
      toast.success(`Queued: install ${tool} ${target}`)
      void queryClient.invalidateQueries({ queryKey: keys.storage })
      onClose()
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Install toolchain</DialogTitle>
      </DialogHeader>
      <DialogBody className="flex flex-col gap-3">
        <Select
          value={tool}
          disabled={busy}
          onValueChange={(v) => {
            setTool(v)
            setPicked(null)
            setFilter("")
          }}
        >
          <SelectTrigger aria-label="Tool">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {TOOLS.map(([value, label]) => (
              <SelectItem key={value} value={value}>
                {label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <p className="text-sm">
          {target ? <strong className="font-mono">{target}</strong> : <span className="text-muted-foreground">Pick one below, or type a version</span>}
        </p>
        <Input
          aria-label="Version"
          placeholder="Type to filter"
          value={filter}
          onChange={(e) => {
            setFilter(e.target.value)
            setPicked(null)
          }}
        />
        {choices.isPending ? (
          <Spinner label="Loading versions" />
        ) : choices.isError ? (
          <ErrorLine onRetry={() => void choices.refetch()}>{errorText(choices.error)}</ErrorLine>
        ) : shown.length === 0 ? (
          <p className="text-sm text-muted-foreground">{all.length === 0 ? "Nothing to pick" : "No match"}</p>
        ) : (
          <ul className="flex max-h-48 flex-col overflow-auto">
            {shown.map((c) => (
              <li key={c.spec}>
                <button
                  type="button"
                  aria-pressed={picked === c.spec}
                  className={`flex w-full items-center gap-2 rounded px-2 py-1 text-left text-sm hover:bg-muted ${picked === c.spec ? "bg-muted" : ""}`}
                  onClick={() => setPicked(c.spec)}
                >
                  <span className="font-mono">{c.version}</span>
                  {c.lts && <span className="text-xs text-muted-foreground">lts</span>}
                </button>
              </li>
            ))}
          </ul>
        )}
        {error && <ErrorLine>{error}</ErrorLine>}
      </DialogBody>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button loading={busy} disabled={busy || target === ""} onClick={() => void submit()}>
          Install
        </Button>
      </DialogFooter>
    </>
  )
}
```

Change the three opener labels (only the text):
- `web/src/pages/setup.tsx`, in `RepositoriesStep`: `+ Add repository` → `Add repository`.
- `web/src/pages/repositories.tsx`, in `addButton`: `+ Add repository` → `Add repository`.
- `web/src/components/toolchains-card.tsx`: `Install…` → `Install`.

In `web/src/anti-slop.test.ts`, add `"src/components/add-repo-dialog.tsx"` and `"src/components/install-dialog.tsx"` to the components group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/components/add-repo-dialog.test.tsx src/components/install-dialog.test.tsx src/pages/setup.test.tsx src/pages/repositories.test.tsx src/pages/toolchains.test.tsx src/anti-slop.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "fix(web): restyle the add and install dialogs"
```

---

### Task 2: Add the repository summaries

**Files:**
- Create: `web/src/lib/repos.ts`
- Test: `web/src/lib/repos.test.ts`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: plan 1's `ActivityWeek` type in `web/src/api/types.ts`.
- Produces: `weekRate`, `weekText`, `reposSummary`, `runnersText`, `repoDescription` (Contracts, Repositories).

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 0 = 1

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/repos.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import type { RepoStatus } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { repoDescription, reposSummary, runnersText, weekRate, weekText } from "./repos"

const repo = (over: Partial<RepoStatus>): RepoStatus => ({ name: "darkmem", paused: false, max: 3, active: 0, queued: 0, ...over })

describe("weekRate", () => {
  it.each([
    [{ succeeded: 46, failed: 2, cancelled: 1 }, 96],
    [{ succeeded: 1, failed: 1, cancelled: 0 }, 50],
    [{ succeeded: 0, failed: 0, cancelled: 4 }, undefined],
    [undefined, undefined],
  ])("reads %j as %s", (week, rate) => {
    expect(weekRate(week)).toBe(rate)
  })
})

describe("weekText", () => {
  it("names every count", () => {
    expect(weekText({ succeeded: 46, failed: 2, cancelled: 1 })).toBe("46 succeeded, 2 failed, 1 cancelled in 7 days")
  })
})

describe("reposSummary", () => {
  it("counts configured, running and paused repositories, leaving out those being removed", () => {
    expect(reposSummary(fixtures.status)).toBe("2 configured, 1 running, 1 paused")
  })
  it("leaves out a count of 0", () => {
    expect(reposSummary({ ...fixtures.status, repos: [repo({})] })).toBe("1 configured")
    expect(reposSummary({ ...fixtures.status, repos: [] })).toBe("")
  })
})

describe("runnersText", () => {
  it("shows the cap, or no limit", () => {
    expect(runnersText(repo({ active: 2, max: 3 }))).toBe("2 of 3")
    expect(runnersText(repo({ active: 1, max: 0 }))).toBe("1, no limit")
  })
})

describe("repoDescription", () => {
  it.each([
    [repo({ active: 2, queued: 1 }), "Running 2 of 3 runners, 1 job waiting"],
    [repo({ active: 1, max: 0 }), "Running 1 runner"],
    [repo({}), "Idle"],
    [repo({ queued: 2 }), "Idle, 2 jobs waiting"],
    [repo({ paused: true, queued: 2 }), "Paused"],
    [repo({ paused: true, error: "GitHub: not found" }), "GitHub: not found"],
  ])("describes %j", (r, text) => {
    expect(repoDescription(r)).toBe(text)
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run (in `web/`): `npx vitest run src/lib/repos.test.ts`
Expected: FAIL: `Cannot find module './repos'`.

- [ ] **Step 3: Write the implementation**

Create `web/src/lib/repos.ts`:

```ts
import type { ActivityWeek, RepoStatus, Status } from "@/api/types"
import { plural } from "./format"

// Cancelled and skipped runs say nothing about health, so the rate leaves
// them out.
export function weekRate(week: ActivityWeek | undefined): number | undefined {
  if (!week) return undefined
  const decided = week.succeeded + week.failed
  return decided === 0 ? undefined : Math.round((week.succeeded / decided) * 100)
}

export function weekText(week: ActivityWeek): string {
  return `${week.succeeded} succeeded, ${week.failed} failed, ${week.cancelled} cancelled in 7 days`
}

// A repository being removed is on its way out, as the Dashboard counts it.
export function reposSummary(status: Status): string {
  const live = status.repos.filter((r) => !r.removing)
  const counts: [number, string][] = [
    [live.length, "configured"],
    [live.filter((r) => r.active > 0).length, "running"],
    [live.filter((r) => r.paused).length, "paused"],
  ]
  return counts
    .filter(([n]) => n > 0)
    .map(([n, word]) => `${n} ${word}`)
    .join(", ")
}

export function runnersText(repo: RepoStatus): string {
  return repo.max === 0 ? `${repo.active}, no limit` : `${repo.active} of ${repo.max}`
}

export function repoDescription(repo: RepoStatus): string {
  if (repo.error) return repo.error
  if (repo.paused) return "Paused"
  const waiting = repo.queued > 0 ? `, ${plural(repo.queued, "job")} waiting` : ""
  if (repo.active === 0) return `Idle${waiting}`
  const runners = repo.max === 0 ? plural(repo.active, "runner") : `${repo.active} of ${repo.max} runners`
  return `Running ${runners}${waiting}`
}
```

In `web/src/anti-slop.test.ts`, add `"src/lib/repos.ts"` to the lib group.

- [ ] **Step 4: Run the test to verify it passes**

Run (in `web/`): `npx vitest run src/lib/repos.test.ts src/anti-slop.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): add the repository summaries"
```

---

### Task 3: Add the full columns to the repository table

**Files:**
- Create: `web/src/components/runner-meter.tsx`
- Modify: `web/src/components/repo-table.tsx`
- Test: `web/src/components/repo-table.test.tsx`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `weekRate`, `weekText` (Task 2); plan 1's `ActivityRepo.week`.
- Produces: `RunnerMeter`, `RepoTable`'s `columns` and `config` props (Contracts, Repositories).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `web/src/components/repo-table.test.tsx`, add `ActivityRepo` and `Config` to the type import from `@/api/types`, and append inside `describe("RepoTable", …)`:

```tsx
  function drawFull(config: Config, activity: ActivityRepo[]) {
    const { wrapper } = withQuery()
    render(
      <TooltipProvider>
        <RepoTable repos={fixtures.status.repos} activity={activity} now={now} offline={false} onOpen={vi.fn()} columns="full" config={config} />
      </TooltipProvider>,
      { wrapper },
    )
  }
  const week = [{ repo: "darkmem", hours: [], week: { succeeded: 46, failed: 2, cancelled: 1 } }]

  it("keeps the Dashboard's columns by default", () => {
    draw()
    expect(screen.queryByRole("columnheader", { name: "7 days" })).toBeNull()
    expect(screen.queryByRole("columnheader", { name: "Labels" })).toBeNull()
    expect(screen.queryByText("Removing. Running jobs finish first.")).toBeNull()
  })

  it("adds the week's success rate and the labels in full mode", () => {
    drawFull(fixtures.config, week)
    expect(screen.getByRole("columnheader", { name: "7 days" })).toBeInTheDocument()
    expect(screen.queryByRole("columnheader", { name: "Warm" })).toBeNull()
    expect(within(row("darkmem")).getByText("96%")).toHaveClass("font-mono")
    expect(within(row("darkmem")).getByText("46 succeeded, 2 failed, 1 cancelled in 7 days")).toHaveClass("sr-only")
    expect(within(row("darkcloud")).queryByText(/%$/)).toBeNull()
    expect(within(row("darkmem")).getByText("gpu")).toHaveClass("font-mono")
  })

  it("shows warm in all mode, with the default of 1 when unset", () => {
    drawFull({ ...fixtures.config, mode: "all" }, week)
    expect(screen.getByRole("columnheader", { name: "Warm" })).toBeInTheDocument()
    const warm = (name: string) => within(row(name)).getByTestId("warm")
    expect(warm("darkmem")).toHaveTextContent("1")
    expect(warm("darkcloud")).toHaveTextContent("1")
  })

  it("notes a repository being removed under its name", () => {
    drawFull(fixtures.config, week)
    expect(within(row("old-repo")).getByText("Removing. Running jobs finish first.")).toHaveClass("text-muted-foreground")
  })

  it("hides the extra columns below 768px", () => {
    drawFull({ ...fixtures.config, mode: "all" }, week)
    for (const name of ["Warm", "7 days", "Labels"]) {
      expect(screen.getByRole("columnheader", { name })).toHaveClass("hidden", "md:table-cell")
    }
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/components/repo-table.test.tsx`
Expected: FAIL: `RepoTable` has no `columns` prop (typecheck), and no "7 days" column renders.

- [ ] **Step 3: Write the implementation**

Create `web/src/components/runner-meter.tsx`:

```tsx
// One cell per allowed runner, filled for each busy one. An unlimited
// repository (max 0) has nothing to draw.
export function RunnerMeter({ active, max }: { active: number; max: number }) {
  if (max === 0) return null
  return (
    <span aria-hidden="true" className="flex gap-0.5">
      {Array.from({ length: max }, (_, i) => (
        <span
          key={i}
          data-slot="runner"
          data-filled={i < active ? "true" : undefined}
          className={`h-2.5 w-1.5 rounded-[1px] ${i < active ? "bg-primary" : "bg-muted"}`}
        />
      ))}
    </span>
  )
}
```

In `web/src/components/repo-table.tsx`:

0. Change the comment above `RowMenu` from "the max stepper lives on the repository page" to "the max field lives on the repository page" (the `Stepper` component is deleted in Task 22, and its grep there expects no other hit).
1. Change the imports: `import type { ActivityRepo, Config, RepoStatus } from "@/api/types"`, add `import { RunnerMeter } from "@/components/runner-meter"` and `import { weekRate, weekText } from "@/lib/repos"`.
2. Replace `RunnerCells` with:

```tsx
function RunnerCells({ repo }: { repo: RepoStatus }) {
  if (repo.max === 0) return <span className="font-mono">{repo.active}</span>
  return (
    <span className="flex items-center gap-2">
      <span className="font-mono">
        {repo.active}/{repo.max}
      </span>
      <RunnerMeter active={repo.active} max={repo.max} />
    </span>
  )
}

const EXTRA = "hidden md:table-cell"
```

3. Replace `export function RepoTable …` to the end of the file with:

```tsx
export function RepoTable({
  repos,
  activity,
  now,
  offline,
  onOpen,
  columns = "compact",
  config,
}: {
  repos: RepoStatus[]
  activity: ActivityRepo[] | undefined
  now: number
  offline: boolean
  onOpen: (name: string) => void
  columns?: "compact" | "full"
  config?: Config
}) {
  const full = columns === "full"
  const warmShown = full && config?.mode === "all"
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Repository</TableHead>
          <TableHead>State</TableHead>
          <TableHead>Runners</TableHead>
          <TableHead>Waiting</TableHead>
          {warmShown && <TableHead className={`${EXTRA} text-right`}>Warm</TableHead>}
          <TableHead>Last 24 hours</TableHead>
          {full && <TableHead className={`${EXTRA} text-right`}>7 days</TableHead>}
          <TableHead>Last job</TableHead>
          {full && <TableHead className={EXTRA}>Labels</TableHead>}
          <TableHead>
            <span className="sr-only">Actions</span>
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {repos.map((r) => {
          const word = repoStateWord(r)
          const seen = activity?.find((a) => a.repo === r.name)
          const cfg = config?.repos?.find((c) => c.name === r.name)
          const rate = weekRate(seen?.week)
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
                {full && r.removing && <p className="text-xs text-muted-foreground">Removing. Running jobs finish first.</p>}
              </TableCell>
              <TableCell className={STATE_CLASS[word] ?? ""}>{word}</TableCell>
              <TableCell>
                <RunnerCells repo={r} />
              </TableCell>
              <TableCell>{r.queued > 0 && <span className="font-mono text-warning">{r.queued}</span>}</TableCell>
              {warmShown && (
                <TableCell data-testid="warm" className={`${EXTRA} text-right font-mono`}>
                  {cfg ? (cfg.warm ?? 1) : ""}
                </TableCell>
              )}
              <TableCell>{seen && <RepoActivityStrip repo={r.name} hours={seen.hours} />}</TableCell>
              {full && (
                <TableCell className={`${EXTRA} text-right`}>
                  {rate !== undefined && seen && (
                    <>
                      <span className="font-mono">{`${rate}%`}</span>
                      <span className="sr-only">{` ${weekText(seen.week)}`}</span>
                    </>
                  )}
                </TableCell>
              )}
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
              {full && (
                <TableCell className={`${EXTRA} whitespace-normal`}>
                  <span className="font-mono text-xs break-words text-muted-foreground">{(cfg?.labels ?? []).join(" ")}</span>
                </TableCell>
              )}
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

The existing tests index cells in compact mode (Runners at 2, Waiting at 3, Last job at 5), which is unchanged.

In `web/src/anti-slop.test.ts`, add `"src/components/runner-meter.tsx"` to the components group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/components/repo-table.test.tsx src/pages/dashboard.test.tsx src/anti-slop.test.ts`
Expected: PASS; the Dashboard's table is unchanged.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): add full columns to the repo table"
```

---

### Task 4: Rebuild the Repositories page

**Files:**
- Modify: `web/src/pages/repositories.tsx` (replace)
- Modify: `web/src/lib/use-repo-actions.ts` (`askRemove`)
- Delete: `web/src/components/repo-actions.tsx`
- Test: `web/src/pages/repositories.test.tsx` (replace), `web/src/lib/use-repo-actions.test.tsx:34`, `web/src/pages/repository.test.tsx` (the test "is reached from the Repositories page"), `web/src/a11y.test.tsx`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `RepoTable` with `columns="full"` (Task 3), `reposSummary` (Task 2), `Section`, `useActivity` (Contracts, From plan 2).
- Produces: the remove confirmation copy of `useRepoActions` (Contracts, Repositories).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Replace `web/src/pages/repositories.test.tsx` with:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const row = (name: string) => within(screen.getByRole("link", { name }).closest("tr") as HTMLElement)

describe("Repositories page", () => {
  it("sums up the repositories in the header", async () => {
    mockApi(authedRoutes())
    renderApp("/repositories")
    expect(await screen.findByText("2 configured, 1 running, 1 paused")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Add repository" })).toBeEnabled()
  })

  it("lists every repository with the week's success rate and labels", async () => {
    mockApi(authedRoutes())
    renderApp("/repositories")
    const table = within(await screen.findByRole("region", { name: "Configured repositories" }))
    expect(table.getByRole("columnheader", { name: "7 days" })).toBeInTheDocument()
    await waitFor(() => expect(row("darkmem").getByText("50%")).toBeInTheDocument())
    expect(row("darkmem").getByText("gpu")).toBeInTheDocument()
    expect(row("old-repo").getByText("Removing. Running jobs finish first.")).toBeInTheDocument()
  })

  it("asks for the last 24 hours of activity", async () => {
    const { calls } = mockApi(authedRoutes())
    renderApp("/repositories")
    await screen.findByRole("region", { name: "Configured repositories" })
    await waitFor(() => expect(calls.some((c) => c.path === "/api/activity" && c.search === "?window=24h&tz=UTC")).toBe(true))
  })

  it("waits for the daemon before the first status", async () => {
    mockApi(authedRoutes({ "GET /api/status": () => json({ error: "connection refused" }, 502) }))
    renderApp("/repositories")
    expect(await screen.findByText("Waiting for the daemon")).toBeInTheDocument()
  })

  it("keeps the table but locks its actions while the daemon is unreachable", { timeout: 10_000 }, async () => {
    let reads = 0
    mockApi(authedRoutes({ "GET /api/status": () => (++reads === 1 ? fixtures.status : json({ error: "connection refused" }, 502)) }))
    const { user } = renderApp("/repositories")
    expect(await screen.findByText("Reconnecting", {}, { timeout: 4000 })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Add repository" })).toBeDisabled()
    await user.click(screen.getByRole("button", { name: "More actions for darkmem" }))
    expect(await screen.findByRole("menuitem", { name: "Pause" })).toHaveAttribute("aria-disabled", "true")
  })

  it("says when no repository is configured, with the button that fixes it", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, repos: [] } }))
    renderApp("/repositories")
    expect(await screen.findByText("No repositories yet")).toBeInTheDocument()
    expect(screen.getAllByRole("button", { name: "Add repository" })).toHaveLength(2)
    expect(screen.queryByRole("region", { name: "Configured repositories" })).toBeNull()
  })

  it("pauses a repository from its row menu", async () => {
    const { calls } = mockApi(authedRoutes({ "POST /api/repos/darkmem/pause": () => noContent() }))
    const { user } = renderApp("/repositories")
    await user.click(await screen.findByRole("button", { name: "More actions for darkmem" }))
    await user.click(await screen.findByRole("menuitem", { name: "Pause" }))
    expect((await screen.findAllByText("Paused darkmem")).length).toBeGreaterThan(0)
    expect(calls.filter((c) => c.method === "POST").map((c) => c.path)).toEqual(["/api/repos/darkmem/pause"])
  })

  it("removes a repository only after confirmation", async () => {
    const { calls } = mockApi(authedRoutes({ "DELETE /api/repos/darkmem": () => noContent() }))
    const { user } = renderApp("/repositories")
    await user.click(await screen.findByRole("button", { name: "More actions for darkmem" }))
    await user.click(await screen.findByRole("menuitem", { name: "Remove" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(ask.getByText("Remove darkmem?")).toBeInTheDocument()
    expect(ask.getByText("Its running jobs finish first.")).toBeInTheDocument()
    await user.click(ask.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)

    await user.click(screen.getByRole("button", { name: "More actions for darkmem" }))
    await user.click(await screen.findByRole("menuitem", { name: "Remove" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/repos/darkmem")).toBe(true))
    expect((await screen.findAllByText("Removing darkmem")).length).toBeGreaterThan(0)
  })

  it("opens a repository from its row", async () => {
    mockApi(authedRoutes())
    const { user, router } = renderApp("/repositories")
    await user.click(await screen.findByRole("link", { name: "darkcloud" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/repositories/darkcloud"))
  })

  it("shows warm in all mode", async () => {
    mockApi(authedRoutes({ "GET /api/config": { ...fixtures.config, mode: "all" } }))
    renderApp("/repositories")
    expect(await screen.findByRole("columnheader", { name: "Warm" })).toBeInTheDocument()
  })
})
```

In `web/src/lib/use-repo-actions.test.tsx:34`, replace the title assertion with:

```tsx
    expect(result.current.confirm).toMatchObject({ title: "Remove darkmem?", body: "Its running jobs finish first.", action: "Remove" })
```

In `web/src/pages/repository.test.tsx`, replace the test "is reached from the Repositories page" with:

```tsx
  it("is reached from the Repositories page", async () => {
    mockApi(routes())
    const { user, router } = renderApp("/repositories")
    await user.click(await screen.findByRole("link", { name: "darkmem" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/repositories/darkmem"))
  })
```

Append to `web/src/a11y.test.tsx`:

```tsx
describe("Repositories accessibility", () => {
  it(
    "has no axe violations",
    async () => {
      mockApi(authedRoutes())
      renderApp("/repositories")
      await screen.findByRole("region", { name: "Configured repositories" })
      await screen.findByText("50%")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/pages/repositories.test.tsx src/lib/use-repo-actions.test.tsx src/a11y.test.tsx`
Expected: FAIL: the page still renders cards, there is no "Configured repositories" region or header summary, and the confirmation title is the old sentence.

- [ ] **Step 3: Write the implementation**

Replace `web/src/pages/repositories.tsx` with:

```tsx
import { useNavigate } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useState } from "react"
import { useActivity, useConfig, useStatus } from "@/api/hooks"
import { AddRepoDialog } from "@/components/add-repo-dialog"
import { Section } from "@/components/page/section"
import { RepoTable } from "@/components/repo-table"
import { reposSummary } from "@/lib/repos"
import { useNow } from "@/lib/use-now"

export function RepositoriesPage() {
  const status = useStatus()
  const config = useConfig()
  const activity = useActivity("24h")
  const now = useNow()
  const navigate = useNavigate()
  const [adding, setAdding] = useState(false)

  const st = status.data
  if (!st) {
    return (
      <div className="flex flex-col gap-4">
        <PageHeader title="Repositories" />
        <Spinner label="Waiting for the daemon" />
      </div>
    )
  }
  const offline = status.isError
  const add = (
    <Button disabled={offline} onClick={() => setAdding(true)}>
      Add repository
    </Button>
  )
  return (
    <div className="flex flex-col gap-4">
      <PageHeader title="Repositories" description={reposSummary(st) || undefined} actions={add} />
      {st.repos.length === 0 ? (
        <div className="flex flex-col items-start gap-2">
          <p className="text-sm text-muted-foreground">No repositories yet</p>
          {add}
        </div>
      ) : (
        <Section title="Configured repositories">
          <div className="overflow-x-auto">
            <RepoTable
              repos={st.repos}
              activity={activity.data?.repos}
              now={now}
              offline={offline}
              onOpen={(name) => void navigate({ to: "/repositories/$name", params: { name } })}
              columns="full"
              config={config.data}
            />
          </div>
        </Section>
      )}
      <AddRepoDialog open={adding} onClose={() => setAdding(false)} />
    </div>
  )
}
```

In `web/src/lib/use-repo-actions.ts`, replace the `askRemove` property with:

```ts
    askRemove: () =>
      setConfirm({
        title: `Remove ${repo.name}?`,
        body: "Its running jobs finish first.",
        action: "Remove",
        destructive: true,
        run: () => remove.mutate(),
      }),
```

Delete `web/src/components/repo-actions.tsx` (its only caller was the old page; confirm with `grep -rn "repo-actions\"\|RepoActionButtons" web/src` printing nothing).

In `web/src/anti-slop.test.ts`, remove `"src/components/repo-actions.tsx"` and add `"src/pages/repositories.tsx"` to the pages group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/pages src/lib src/components/repo-table.test.tsx src/components/add-repo-dialog.test.tsx src/a11y.test.tsx src/anti-slop.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add -A web/src
git commit -m "feat(web): rebuild the Repositories page"
```

---
### Task 5: Rebuild the workflow labels group

**Files:**
- Create: `web/src/components/label-check-group.tsx`
- Delete: `web/src/components/label-check-card.tsx`
- Modify: `web/src/pages/repository.tsx` (import and element only)
- Test: rename `web/src/components/label-check-card.test.tsx` to `web/src/components/label-check-group.test.tsx` and update it
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `FieldGroup`, `StateText`, `ErrorLine` (Contracts, From plan 2).
- Produces: `LabelCheckGroup` (Contracts, Repositories).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

Run `git mv web/src/components/label-check-card.test.tsx web/src/components/label-check-group.test.tsx`, then in it:
- rename `describe("Workflow labels card", …)` to `describe("Workflow labels group", …)`;
- in "shows the label groups and whether ghr takes them", replace `expect(screen.getByText("matched")).toBeInTheDocument()` with `expect(screen.getByText("Matched")).toHaveClass("text-primary")`, and append `expect(screen.getByText("12 jobs, last seen 1h ago")).toBeInTheDocument()` and `expect(screen.getByRole("region", { name: "Workflow labels" })).toHaveAttribute("id", "workflow-labels")`;
- replace the body of "offers the missing labels and adds one as an unsaved edit" with:

```tsx
    const { calls } = mockApi(routes({ "GET /api/repos/darkmem/label-check": unmatched }))
    const { user } = renderApp("/repositories/darkmem")
    expect(await screen.findByText("Missing arm64, big")).toBeInTheDocument()
    expect(screen.getByText("Unmatched")).toHaveClass("text-destructive")
    expect(screen.getByText(/, partial$/)).toBeInTheDocument()
    expect(screen.getByText("ci / build +2 more")).toBeInTheDocument()
    expect(screen.getByText("Needs a different OS or architecture")).toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Add label arm64" })).toBeNull()
    expect(
      screen.getByText("Adding a label changes which jobs ghr accepts. It does not install anything on the runner."),
    ).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "Add label big" }))
    expect(screen.getByRole("button", { name: "Remove big" })).toBeInTheDocument()
    expect(screen.getByText("1 unsaved change")).toBeInTheDocument()
    expect(calls.some((c) => c.method !== "GET")).toBe(false)
```

- in "starts a check and polls it", replace `expect(await screen.findByText("Checking…")).toBeInTheDocument()` with `expect(await screen.findByText("Checking")).toBeInTheDocument()`.

Append inside the `describe`:

```tsx
  it("shows a failed read as an error line", async () => {
    mockApi(routes({ "GET /api/repos/darkmem/label-check": () => json({ error: "GitHub: 502" }, 502) }))
    renderApp("/repositories/darkmem")
    const region = within(await screen.findByRole("region", { name: "Workflow labels" }))
    expect(await region.findByRole("alert")).toHaveTextContent("GitHub: 502")
  })
```

and change the imports at the top to `import { screen, waitFor, within } from "@testing-library/react"` and `import { json, mockApi } from "@/test/api"`.

The fixture's group was last seen at 13:05 and the status `now` is 14:05, so it reads "1h ago" (web/src/api/fixtures/label-check.json, status.json).

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/components/label-check-group.test.tsx`
Expected: FAIL: the card still shows badges, "missing", "+ add" buttons and "Checking…".

- [ ] **Step 3: Write the implementation**

Create `web/src/components/label-check-group.tsx`:

```tsx
import { useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { TriangleAlert } from "lucide-react"
import { useState, type ReactNode } from "react"
import { api } from "@/api/client"
import { keys, useLabelCheck, useStatus } from "@/api/hooks"
import { ErrorLine } from "@/components/page/error-line"
import { FieldGroup } from "@/components/page/field"
import { StateText } from "@/components/page/state-text"
import { ago, plural } from "@/lib/format"
import { classify, OS_ARCH } from "@/lib/labels"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

export function LabelCheckGroup({
  name,
  effective,
  degraded,
  degradedReason,
  disabled,
  onAddLabel,
}: {
  name: string
  effective: string[]
  degraded: boolean
  degradedReason?: string
  disabled: boolean
  onAddLabel: (label: string) => void
}) {
  const now = useNow()
  const queryClient = useQueryClient()
  // Wait for the first status: until it arrives, a degraded daemon looks healthy.
  const statusRead = useStatus().data !== undefined
  const check = useLabelCheck(name, statusRead && !degraded)
  const [starting, setStarting] = useState(false)
  const lc = degraded ? undefined : check.data

  async function start() {
    setStarting(true)
    try {
      await api.startLabelCheck(name)
      await queryClient.invalidateQueries({ queryKey: keys.labelCheck(name) })
    } catch (err) {
      toast.error(`Label check: ${errorText(err)}`)
    } finally {
      setStarting(false)
    }
  }

  let state: ReactNode = "Not checked yet"
  if (lc?.state === "checking") state = <Spinner label="Checking" />
  else if (lc?.state === "done" && lc.checked_at) state = `Checked ${ago(now - Date.parse(lc.checked_at))}${lc.partial ? ", partial" : ""}`

  // A label missing from several groups gets one Add button, on its first group.
  const offered = new Set<string>()
  const groups = (lc?.groups ?? []).map((g, i) => {
    const { kind, missing } = classify(g.labels, effective)
    const adds = missing.filter((l) => !OS_ARCH.has(l) && !offered.has(l))
    for (const l of adds) offered.add(l)
    return (
      <div key={i} className="flex flex-col gap-1.5 py-3 text-sm">
        <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
          <StateText state={kind} />
          {g.labels.length > 0 ? <span className="font-mono">{g.labels.join(", ")}</span> : <span>No labels (runner group)</span>}
          <span>{g.more > 0 ? `${g.jobs.join(", ")} +${g.more} more` : g.jobs.join(", ")}</span>
          <span className="text-muted-foreground">{`${plural(g.count, "job")}, last seen ${ago(now - Date.parse(g.last_seen))}`}</span>
        </div>
        {kind === "unmatched" && (
          <>
            <p>{`Missing ${missing.join(", ")}`}</p>
            {adds.length > 0 && (
              <div className="flex flex-wrap gap-2">
                {adds.map((l) => (
                  <Button key={l} size="sm" variant="outline" disabled={disabled} onClick={() => onAddLabel(l)}>
                    {`Add label ${l}`}
                  </Button>
                ))}
              </div>
            )}
            {missing.some((l) => OS_ARCH.has(l)) && (
              <p className="flex items-center gap-1.5 text-muted-foreground">
                <TriangleAlert size={15} aria-hidden="true" className="shrink-0 text-warning" />
                Needs a different OS or architecture
              </p>
            )}
          </>
        )}
      </div>
    )
  })

  return (
    <FieldGroup id="workflow-labels" title="Workflow labels">
      <div className="flex flex-wrap items-center gap-3 py-3 text-sm">
        <span>{state}</span>
        <Button size="sm" variant="outline" disabled={disabled || degraded || starting || lc?.state === "checking"} onClick={() => void start()}>
          Check now
        </Button>
      </div>
      {degraded && (
        <div className="py-3">
          <ErrorLine>{`GitHub is rejecting the token: ${degradedReason ?? ""}`}</ErrorLine>
        </div>
      )}
      {lc?.error && (
        <div className="py-3">
          <ErrorLine>{lc.error}</ErrorLine>
        </div>
      )}
      {check.isError && !degraded && (
        <div className="py-3">
          <ErrorLine>{errorText(check.error)}</ErrorLine>
        </div>
      )}
      {groups}
      {offered.size > 0 && (
        <p className="py-3 text-xs text-muted-foreground">Adding a label changes which jobs ghr accepts. It does not install anything on the runner.</p>
      )}
    </FieldGroup>
  )
}
```

In `web/src/pages/repository.tsx`, change `import { LabelCheckCard } from "@/components/label-check-card"` to `import { LabelCheckGroup } from "@/components/label-check-group"` and the element `<LabelCheckCard …/>` to `<LabelCheckGroup …/>` (same props). Delete `web/src/components/label-check-card.tsx`.

In `web/src/anti-slop.test.ts`, add `"src/components/label-check-group.tsx"` to the components group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/components/label-check-group.test.tsx src/pages/repository.test.tsx src/anti-slop.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add -A web/src
git commit -m "feat(web): rebuild the workflow labels group"
```

---

### Task 6: Rebuild the registrations group

**Files:**
- Create: `web/src/components/registrations-group.tsx`
- Delete: `web/src/components/registrations-card.tsx`
- Modify: `web/src/pages/repository.tsx` (import and element only)
- Test: rename `web/src/components/registrations-card.test.tsx` to `web/src/components/registrations-group.test.tsx` and update it
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `FieldGroup`, `StateText`, `ErrorLine` (Contracts, From plan 2).
- Produces: `RegistrationsGroup` (Contracts, Repositories).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

Run `git mv web/src/components/registrations-card.test.tsx web/src/components/registrations-group.test.tsx`, then in it:
- rename `describe("GitHub registrations card", …)` to `describe("GitHub registrations group", …)`;
- in "lists registrations and offers Delete only for an offline foreign one", replace `expect(screen.getByText("offline")).toBeInTheDocument()` with:

```tsx
    const region = within(screen.getByRole("region", { name: "GitHub registrations" }))
    expect(region.getByText("Offline")).toHaveClass("text-destructive")
    expect(region.getByText("Busy")).toHaveClass("text-primary")
    expect(region.getByText("ghr")).toHaveClass("text-muted-foreground")
    expect(region.getByText("self-hosted linux")).toHaveClass("font-mono")
```

- in "deletes a registration only after confirmation", replace `expect(await screen.findByText("Delete the runner registration old-runner from darkmem?")).toBeInTheDocument()` with:

```tsx
    const ask = within(await screen.findByRole("alertdialog"))
    expect(ask.getByText("Delete runner registration old-runner?")).toBeInTheDocument()
    expect(ask.getByText("It is removed from darkmem on GitHub.")).toBeInTheDocument()
```

  and `"deleted old-runner"` with `"Deleted old-runner"`;
- replace `"No runners registered. ghr starts single-use runners on demand (and keeps warm ones in all mode)."` with `"No runners registered. ghr starts single-use runners on demand, and keeps warm ones in all mode."`.

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/components/registrations-group.test.tsx`
Expected: FAIL: the card still shows badges and the old copy.

- [ ] **Step 3: Write the implementation**

Create `web/src/components/registrations-group.tsx`:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { useState, type ReactNode } from "react"
import { api } from "@/api/client"
import { keys, useRegistrations, useStatus } from "@/api/hooks"
import type { Registration } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { ErrorLine } from "@/components/page/error-line"
import { FieldGroup } from "@/components/page/field"
import { StateText } from "@/components/page/state-text"
import { errorText } from "@/query"

export function RegistrationsGroup({
  name,
  degraded,
  degradedReason,
  disabled,
}: {
  name: string
  degraded: boolean
  degradedReason?: string
  disabled: boolean
}) {
  const statusRead = useStatus().data !== undefined
  const regs = useRegistrations(name, statusRead && !degraded)
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const remove = useMutation({
    mutationFn: (r: Registration) => api.deleteRegistration(name, r.id),
    onSuccess: (_data, r) => {
      toast.success(`Deleted ${r.name}`)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.registrations(name) }),
  })

  let body: ReactNode
  if (degraded) body = <ErrorLine>{`GitHub is rejecting the token: ${degradedReason ?? ""}`}</ErrorLine>
  else if (regs.isError) body = <ErrorLine>{errorText(regs.error)}</ErrorLine>
  else if (!regs.data) body = <Spinner label="Loading" />
  else if (regs.data.length === 0) {
    body = <p className="text-sm text-muted-foreground">No runners registered. ghr starts single-use runners on demand, and keeps warm ones in all mode.</p>
  }

  return (
    <FieldGroup id="registrations" title="GitHub registrations">
      {body ? (
        <div className="py-3">{body}</div>
      ) : (
        (regs.data ?? []).map((r) => (
          <div key={r.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-3 text-sm">
            <StateText state={r.busy ? "busy" : r.status} />
            {r.ghr && <span className="text-muted-foreground">ghr</span>}
            <span>{r.name}</span>
            <span className="font-mono text-xs text-muted-foreground">{r.labels.join(" ")}</span>
            {!r.ghr && !r.busy && r.status === "offline" && (
              <Button
                size="sm"
                variant="outline"
                className="ml-auto"
                aria-label={`Delete ${r.name}`}
                disabled={disabled}
                onClick={() =>
                  setConfirm({
                    title: `Delete runner registration ${r.name}?`,
                    body: `It is removed from ${name} on GitHub.`,
                    action: "Delete",
                    destructive: true,
                    run: () => remove.mutate(r),
                  })
                }
              >
                Delete
              </Button>
            )}
          </div>
        ))
      )}
      <div className="py-3">
        <Button size="sm" variant="outline" disabled={disabled || degraded} onClick={() => void regs.refetch()}>
          Refresh
        </Button>
      </div>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </FieldGroup>
  )
}
```

`ConfirmDialog` renders into a portal, so it adds no row of its own inside the group.

In `web/src/pages/repository.tsx`, change `import { RegistrationsCard } from "@/components/registrations-card"` to `import { RegistrationsGroup } from "@/components/registrations-group"` and the element `<RegistrationsCard …/>` to `<RegistrationsGroup …/>` (same props). Delete `web/src/components/registrations-card.tsx`.

In `web/src/anti-slop.test.ts`, add `"src/components/registrations-group.tsx"` to the components group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/components/registrations-group.test.tsx src/components/label-check-group.test.tsx src/pages/repository.test.tsx src/anti-slop.test.ts`
Expected: PASS; the degraded test still finds the token line twice (labels and registrations).

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add -A web/src
git commit -m "feat(web): rebuild the registrations group"
```

---

### Task 7: Show a repository's last 24 hours

**Files:**
- Create: `web/src/components/repo-activity.tsx`
- Test: `web/src/components/repo-activity.test.tsx`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `useActivity(window, repo)`, `BucketsChart` with `tracks="jobs"`, `Section`, `ErrorLine`, `useMediaQuery`/`WIDEST`, `setViewport` (Contracts, From plan 2); `RunnerMeter` (Task 3); `runnersText`, `weekRate` (Task 2).
- Produces: `RepoActivity` (Contracts, Repositories).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 0 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/components/repo-activity.test.tsx`:

```tsx
import { act, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { RepoStatus } from "@/api/types"
import { json, mockApi } from "@/test/api"
import { fixtures } from "@/test/fixtures"
import { setViewport } from "@/test/media"
import { withQuery } from "@/test/query"
import { RepoActivity } from "./repo-activity"

const now = Date.parse("2026-10-03T14:05:00Z")
const darkmem = fixtures.status.repos[0] as RepoStatus

function draw(repo: RepoStatus | undefined = darkmem) {
  const { wrapper } = withQuery()
  return render(<RepoActivity name="darkmem" repo={repo} now={now} />, { wrapper })
}

describe("RepoActivity", () => {
  it("asks for the repository's last 24 hours and draws only its jobs, without picking", async () => {
    const { calls } = mockApi({ "GET /api/activity": fixtures.activityBuckets })
    draw()
    expect(await screen.findByRole("group", { name: "Jobs per bucket for the last 24 hours" })).toBeInTheDocument()
    expect(screen.getByRole("region", { name: "Last 24 hours" })).toBeInTheDocument()
    expect(calls[0]?.search).toBe("?window=24h&tz=UTC&repo=darkmem")
    expect(screen.queryAllByRole("button")).toHaveLength(0)
  })

  it("lists the runners, waiting jobs, the week and the last job", async () => {
    mockApi({ "GET /api/activity": fixtures.activityBuckets })
    draw()
    expect(await screen.findByText("1 succeeded, 1 failed, 50%")).toBeInTheDocument()
    expect(screen.getByText("1 of 2")).toBeInTheDocument()
    const runners = screen.getByText("Runners").closest("div") as HTMLElement
    expect(runners.querySelectorAll('[data-slot="runner"]')).toHaveLength(2)
    expect((screen.getByText("Waiting").closest("div") as HTMLElement).textContent).toBe("Waiting3")
    expect(screen.getByRole("img", { name: "Succeeded" })).toBeInTheDocument()
    expect(screen.getByText("25m ago")).toBeInTheDocument()
  })

  it("puts the facts beside the chart at 1280px and under it below", async () => {
    mockApi({ "GET /api/activity": fixtures.activityBuckets })
    const media = setViewport(1280)
    draw()
    expect(screen.getByText("Runners").closest("dl")).toHaveClass("w-56")
    act(() => media.resize(1024))
    expect(screen.getByText("Runners").closest("dl")).toHaveClass("grid-cols-2")
  })

  it("leaves the facts blank without a status, and says when no job has finished", () => {
    mockApi({ "GET /api/activity": () => new Promise(() => {}) })
    draw(undefined)
    expect(screen.queryByText("1 of 2")).toBeNull()
    expect(screen.getByText("None yet")).toBeInTheDocument()
    expect(screen.getByText("Loading")).toBeInTheDocument()
  })

  it("offers a retry when the activity cannot be read", async () => {
    mockApi({ "GET /api/activity": () => json({ error: "history file unreadable" }, 500) })
    draw()
    expect(await screen.findByRole("alert")).toHaveTextContent("history file unreadable")
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument()
  })
})
```

The activity fixture's one repository entry is darkmem, whose week is `{1, 1, 0}` after plan 1; darkmem's status is 1 active of 2 with 3 queued, and its last job `build #41` finished at 13:40 (web/src/api/fixtures/status.json).

- [ ] **Step 2: Run the test to verify it fails**

Run (in `web/`): `npx vitest run src/components/repo-activity.test.tsx`
Expected: FAIL: `Cannot find module './repo-activity'`.

- [ ] **Step 3: Write the implementation**

Create `web/src/components/repo-activity.tsx`:

```tsx
import { Spinner } from "darkraise-ui/components/spinner"
import type { ReactNode } from "react"
import { useActivity } from "@/api/hooks"
import type { RepoStatus } from "@/api/types"
import { BucketsChart } from "@/components/buckets-chart"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import { ResultIcon } from "@/components/result-icon"
import { RunnerMeter } from "@/components/runner-meter"
import { ago } from "@/lib/format"
import { runnersText, weekRate } from "@/lib/repos"
import { useMediaQuery, WIDEST } from "@/lib/use-media-query"
import { useWidth } from "@/lib/use-width"
import { errorText } from "@/query"

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="flex flex-wrap items-center gap-2 text-sm">{children}</dd>
    </div>
  )
}

// The chart keeps its 720px minimum, so the facts move beside it only when
// both fit.
export function RepoActivity({ name, repo, now }: { name: string; repo: RepoStatus | undefined; now: number }) {
  const activity = useActivity("24h", name)
  const wide = useMediaQuery(WIDEST)
  const [ref, width] = useWidth<HTMLDivElement>(720)
  const week = activity.data?.repos.find((r) => r.repo.toLowerCase() === name.toLowerCase())?.week
  const rate = weekRate(week)
  const job = repo?.last_job
  return (
    <Section title="Last 24 hours">
      {activity.isError && (
        <div className="mb-3">
          <ErrorLine onRetry={() => void activity.refetch()}>{errorText(activity.error)}</ErrorLine>
        </div>
      )}
      <div className={wide ? "flex items-start gap-6" : "flex flex-col gap-4"}>
        <div ref={ref} className="min-w-0 flex-1 overflow-x-auto">
          {activity.data ? (
            <BucketsChart activity={activity.data} now={now} width={width} tracks="jobs" />
          ) : (
            !activity.isError && <Spinner label="Loading" />
          )}
        </div>
        <dl className={wide ? "grid w-56 shrink-0 content-start gap-3" : "grid grid-cols-2 gap-3 sm:grid-cols-4"}>
          <Fact label="Runners">
            {repo && (
              <>
                <RunnerMeter active={repo.active} max={repo.max} />
                <span className="font-mono">{runnersText(repo)}</span>
              </>
            )}
          </Fact>
          <Fact label="Waiting">{repo && <span className="font-mono">{repo.queued}</span>}</Fact>
          <Fact label="Last 7 days">
            {week && <span>{`${week.succeeded} succeeded, ${week.failed} failed${rate === undefined ? "" : `, ${rate}%`}`}</span>}
          </Fact>
          <Fact label="Last job">
            {job ? (
              <>
                <ResultIcon conclusion={job.conclusion} />
                <span>
                  {job.job_name} <span className="font-mono">#{job.run_number}</span>
                </span>
                <span className="text-muted-foreground">{ago(now - Date.parse(job.finished_at))}</span>
              </>
            ) : (
              <span className="text-muted-foreground">None yet</span>
            )}
          </Fact>
        </dl>
      </div>
    </Section>
  )
}
```

In `web/src/anti-slop.test.ts`, add `"src/components/repo-activity.tsx"` to the components group.

- [ ] **Step 4: Run the test to verify it passes**

Run (in `web/`): `npx vitest run src/components/repo-activity.test.tsx src/anti-slop.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): chart one repository's last 24 hours"
```

---

### Task 8: Rebuild the repository page

**Files:**
- Modify: `web/src/pages/repository.tsx` (replace)
- Test: `web/src/pages/repository.test.tsx`, `web/src/a11y.test.tsx`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `LabelCheckGroup` (Task 5), `RegistrationsGroup` (Task 6), `RepoActivity` (Task 7), `repoDescription` (Task 2), `useRepoActions` remove copy (Task 4); `Field`, `FieldGroup`, `SectionNav`, `SaveBar`, `RejectedAlert`, `UnsavedGuard`, `setViewport`, `violations` (Contracts, From plan 2).
- Produces: nothing new.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/repository.test.tsx`:

1. Change the first import to `import { screen, waitFor, within } from "@testing-library/react"` and add `import { setViewport } from "@/test/media"`.
2. Replace the test "shows the repo's settings and the labels its runners get" with:

```tsx
  it("shows the repo's state, settings and the labels its runners get", async () => {
    mockApi(routes())
    renderApp("/repositories/darkmem")
    expect(await screen.findByLabelText("Max")).toHaveValue(2)
    expect(screen.getByLabelText("Warm")).toHaveValue(1)
    expect(screen.getByText("Running 1 of 2 runners, 3 jobs waiting")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Remove gpu" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Remove darkmem-" })).toBeInTheDocument()
    expect(within(screen.getByRole("list", { name: "Runner labels" })).getAllByRole("listitem").map((li) => li.textContent)).toEqual([
      "self-hosted",
      "linux",
      "x64",
      "homelabglobal",
      "gpu",
    ])
    expect(screen.getByText("Leave empty for the default: 1 in queue mode, no limit in all mode. Once set, it stays explicit.")).toBeInTheDocument()
    expect(screen.getByRole("region", { name: "Last 24 hours" })).toBeInTheDocument()
    expect(screen.getByRole("region", { name: "Capacity" })).toBeInTheDocument()
  })
```

3. Make these exact replacements in the rest of the file:
- `"daemon did not apply darkmem.cleanup_name_prefixes; is it older than this ghr?"` → `"Daemon did not apply darkmem.cleanup_name_prefixes; is it older than this ghr?"`
- `/daemon did not apply/` → `/Daemon did not apply/`
- `"saved, but re-reading the config failed: ghr is restarting"` → `"Saved, but re-reading the config failed: ghr is restarting"`
- `"✖ darkmem: needs at least one label in labels or repo labels"` → `"darkmem: needs at least one label in labels or repo labels"`
- `"repositories not saved"` → `"Repositories not saved"`
- `"✖ warm must be <= max"` → `"Warm must be <= max"`
- `"fix the highlighted settings first"` → `"Fix the highlighted settings first"` (every occurrence)
- `"✖ enter a number from 0 to 99"` → `"Enter a number from 0 to 99"`
- in "says when the repo is not configured": `expect(await screen.findByText("Repository not found")).toBeInTheDocument()` → `expect(await screen.findByText("Repository not found. It may have been removed.")).toBeInTheDocument()` followed by `expect(screen.getByRole("link", { name: "Go to Repositories" })).toHaveAttribute("href", "/repositories")`

4. In "locks the form while the repo is being removed", append:

```tsx
    expect(screen.getByText("Being removed. Running jobs finish first, and settings are read-only.")).toBeInTheDocument()
```

5. Append inside `describe("repository page", …)`:

```tsx
  it("pauses from the header and links to the repository's history", async () => {
    const { calls } = mockApi(routes({ "POST /api/repos/darkmem/pause": () => noContent() }))
    const { user } = renderApp("/repositories/darkmem")
    expect(await screen.findByRole("link", { name: "View history" })).toHaveAttribute("href", "/history?repo=darkmem")
    await user.click(screen.getByRole("button", { name: "Pause" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/repos/darkmem/pause")).toBe(true))
  })

  it("removes from the More actions menu after confirmation", async () => {
    const { calls } = mockApi(routes({ "DELETE /api/repos/darkmem": () => noContent() }))
    const { user } = renderApp("/repositories/darkmem")
    await user.click(await screen.findByRole("button", { name: "More actions for darkmem" }))
    await user.click(await screen.findByRole("menuitem", { name: "Remove" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(ask.getByText("Remove darkmem?")).toBeInTheDocument()
    await user.click(ask.getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/repos/darkmem")).toBe(true))
  })

  it("disables pause and remove until the repository's status exists, but not its history", async () => {
    const repos = fixtures.status.repos.filter((r) => r.name !== "darkmem")
    mockApi(routes({ "GET /api/status": { ...fixtures.status, repos } }))
    const { user } = renderApp("/repositories/darkmem")
    expect(await screen.findByRole("button", { name: "Pause" })).toBeDisabled()
    expect(screen.getByRole("link", { name: "View history" })).toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "More actions for darkmem" }))
    expect(await screen.findByRole("menuitem", { name: "Remove" })).toHaveAttribute("aria-disabled", "true")
  })

  it("marks a changed field and indexes the sections at 1280px", async () => {
    setViewport(1280)
    mockApi(routes())
    const { user } = renderApp("/repositories/darkmem")
    const max = await screen.findByLabelText("Max")
    await user.clear(max)
    await user.type(max, "3")
    expect(screen.getByText("Changed")).toHaveClass("text-warning")
    const nav = within(screen.getByRole("navigation", { name: "Sections" }))
    expect(nav.getAllByRole("button").map((b) => b.textContent)).toEqual(["Capacity", "Labels", "Cleanup", "Workflow labels", "GitHub registrations"])
  })
```

Append to `web/src/a11y.test.tsx`:

```tsx
describe("Repository page accessibility", () => {
  it(
    "has no axe violations at 1280px",
    async () => {
      setViewport(1280)
      mockApi(
        authedRoutes({
          "GET /api/history": [],
          "GET /api/repos/darkmem/label-check": fixtures.labelCheck,
          "GET /api/repos/darkmem/registrations": fixtures.registrations,
        }),
      )
      renderApp("/repositories/darkmem")
      await screen.findByRole("group", { name: "Jobs per bucket for the last 24 hours" })
      await screen.findByText("ghr-aaaaaa")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/pages/repository.test.tsx src/a11y.test.tsx`
Expected: FAIL: no description, no header actions, no "Runner labels" list, no section index, and the old lower-case copy.

- [ ] **Step 3: Write the implementation**

Replace `web/src/pages/repository.tsx` with:

```tsx
import { useQueryClient } from "@tanstack/react-query"
import { Link, useParams } from "@tanstack/react-router"
import { Button } from "darkraise-ui/components/button"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "darkraise-ui/components/dropdown-menu"
import { Input } from "darkraise-ui/components/input"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { PageHeader } from "darkraise-ui/layout"
import { Ellipsis } from "lucide-react"
import { useState } from "react"
import { ApiError, api } from "@/api/client"
import { keys, useConfig, useStatus } from "@/api/hooks"
import type { Config, RepoConfig, RepoPatch, RepoStatus } from "@/api/types"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { LabelCheckGroup } from "@/components/label-check-group"
import { Field, FieldGroup } from "@/components/page/field"
import { SectionNav } from "@/components/page/section-nav"
import { RegistrationsGroup } from "@/components/registrations-group"
import { RepoActivity } from "@/components/repo-activity"
import { RejectedAlert, SaveBar } from "@/components/save-bar"
import { TagField } from "@/components/tag-field"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { changedKeys, sameValue, settle, type Values } from "@/lib/draft"
import { repoDescription } from "@/lib/repos"
import { useNow } from "@/lib/use-now"
import { useRepoActions } from "@/lib/use-repo-actions"
import { errorText } from "@/query"

const SYSTEM_LABELS = ["self-hosted", "linux", "x64"]

const SECTIONS = [
  { id: "capacity", title: "Capacity" },
  { id: "labels", title: "Labels" },
  { id: "cleanup", title: "Cleanup" },
  { id: "workflow-labels", title: "Workflow labels" },
  { id: "registrations", title: "GitHub registrations" },
] as const

function repoValues(r: RepoConfig | undefined): Values {
  return { max: r?.max, warm: r?.warm, labels: r?.labels ?? [], cleanup_name_prefixes: r?.cleanup_name_prefixes ?? [] }
}

// Mirrors config.CustomLabels: global then repo labels, trimmed, lower-cased
// and deduplicated, so the preview shows what the runners register.
function customLabels(global: string[], repo: string[]): { label: string; global: boolean }[] {
  const seen = new Set<string>()
  const out: { label: string; global: boolean }[] = []
  for (const [list, isGlobal] of [
    [global, true],
    [repo, false],
  ] as const) {
    for (const raw of list) {
      const label = raw.trim().toLowerCase()
      if (label === "" || seen.has(label)) continue
      seen.add(label)
      out.push({ label, global: isGlobal })
    }
  }
  return out
}

function MoreMenu({ name, disabled, onRemove }: { name: string; disabled: boolean; onRemove: () => void }) {
  return (
    <DropdownMenu>
      <Tooltip>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button size="icon" variant="outline" aria-label={`More actions for ${name}`}>
              <Ellipsis size={15} aria-hidden="true" />
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent>More actions</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end">
        <DropdownMenuItem disabled={disabled} onSelect={onRemove} className="text-destructive">
          Remove
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function HistoryLink({ name }: { name: string }) {
  return (
    <Button variant="outline" asChild>
      <Link to="/history" search={{ repo: name }}>
        View history
      </Link>
    </Button>
  )
}

// useRepoActions needs the repository's status, so the live actions mount
// only once it exists.
function LiveActions({ repo, offline }: { repo: RepoStatus; offline: boolean }) {
  const actions = useRepoActions(repo, offline)
  return (
    <>
      <Button variant="outline" disabled={actions.locked} onClick={actions.togglePause}>
        {repo.paused ? "Resume" : "Pause"}
      </Button>
      <HistoryLink name={repo.name} />
      <MoreMenu name={repo.name} disabled={actions.locked} onRemove={actions.askRemove} />
      <ConfirmDialog confirm={actions.confirm} onClose={actions.closeConfirm} />
    </>
  )
}

function HeaderActions({ name, repo, offline }: { name: string; repo: RepoStatus | undefined; offline: boolean }) {
  return (
    <div className="flex flex-wrap gap-2">
      {repo ? (
        <LiveActions repo={repo} offline={offline} />
      ) : (
        <>
          <Button variant="outline" disabled>
            Pause
          </Button>
          <HistoryLink name={name} />
          <MoreMenu name={name} disabled onRemove={() => {}} />
        </>
      )}
    </div>
  )
}

export function RepositoryPage() {
  const { name } = useParams({ from: "/app/repositories/$name" })
  // Keyed by name: each repository starts with its own empty draft.
  return <RepositoryForm key={name} name={name} />
}

function RepositoryForm({ name }: { name: string }) {
  const status = useStatus()
  const config = useConfig()
  const now = useNow()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState<Values>({})
  const [saving, setSaving] = useState(false)
  const [rejection, setRejection] = useState("")

  const cfg = config.data
  const repoCfg = cfg?.repos?.find((r) => r.name === name)
  const repoStatus = status.data?.repos.find((r) => r.name === name)
  const loaded = repoValues(repoCfg)
  const values: Values = { ...loaded, ...draft }
  const changed = changedKeys(draft, loaded)
  const maxValue = values.max as number | undefined
  const warmValue = (values.warm as number | undefined) ?? 1
  // RepoPatch cannot unset a value, so emptying a box that holds one is an
  // error, not a change; the empty edit stays so the box does not refill.
  const blank = (key: "max" | "warm") =>
    key in draft && draft[key] === undefined && loaded[key] !== undefined ? "Enter a number from 0 to 99" : ""
  const maxError = blank("max")
  const warmError = blank("warm") || (maxValue !== undefined && maxValue > 0 && warmValue > maxValue ? "Warm must be <= max" : "")
  const locked = Boolean(repoStatus?.removing) || status.isError

  function setNumber(key: "max" | "warm", text: string) {
    const n = Number(text)
    if (text.trim() !== "" && !Number.isFinite(n)) return
    setDraft((d) => ({ ...d, [key]: text.trim() === "" ? undefined : Math.min(99, Math.max(0, Math.trunc(n))) }))
  }

  function discard() {
    setDraft({})
    setRejection("")
  }

  async function save(): Promise<boolean> {
    if (maxError || warmError) {
      toast.error("Fix the highlighted settings first")
      return false
    }
    const sent: Values = Object.fromEntries(changed.map((k) => [k, draft[k]]))
    setSaving(true)
    setRejection("")
    try {
      await api.patchConfig({ repos: { [name]: sent as RepoPatch } })
    } catch (err) {
      if (err instanceof ApiError && err.status >= 400 && err.status < 500) {
        setRejection(err.message)
        toast.error("Repositories not saved")
      } else {
        toast.error(`Repositories not saved: ${errorText(err)}`)
      }
      setSaving(false)
      return false
    }
    toast.success("Repositories saved")
    try {
      const fresh = await queryClient.fetchQuery({ queryKey: keys.config, queryFn: ({ signal }) => api.config(signal), staleTime: 0 })
      const applied = repoValues(fresh.repos?.find((r) => r.name === name))
      const missed = Object.keys(sent).find((key) => !sameValue(applied[key], sent[key]))
      if (missed) toast.error(`Daemon did not apply ${name}.${missed}; is it older than this ghr?`)
    } catch (err) {
      toast.error(`Saved, but re-reading the config failed: ${errorText(err)}`)
      // Until a read succeeds the cached config shows what was sent; the next
      // poll replaces it with what the daemon holds.
      queryClient.setQueryData<Config>(keys.config, (c) =>
        c && { ...c, repos: c.repos?.map((r) => (r.name === name ? { ...r, ...(sent as RepoPatch) } : r)) ?? null },
      )
    }
    setDraft((d) => settle(d, sent))
    setSaving(false)
    return true
  }

  const breadcrumbs = [{ label: "Repositories", href: "/repositories" }, { label: name }]
  if (!cfg) {
    return (
      <div className="flex flex-col gap-4">
        <PageHeader breadcrumbs={breadcrumbs} title={name} />
        <Spinner label="Loading" />
      </div>
    )
  }
  if (!repoCfg) {
    return (
      <div className="flex flex-col gap-4">
        <PageHeader breadcrumbs={breadcrumbs} title={name} />
        <div className="flex flex-col items-start gap-2">
          <p className="text-sm text-muted-foreground">Repository not found. It may have been removed.</p>
          <Link to="/repositories" className="text-sm text-primary hover:underline">
            Go to Repositories
          </Link>
        </div>
      </div>
    )
  }

  const custom = customLabels(cfg.labels ?? [], values.labels as string[])
  const effective = [...SYSTEM_LABELS, ...custom.map((c) => c.label)]
  const degraded = status.data?.degraded ?? false
  const is = (key: string) => changed.includes(key)

  function addLabel(label: string) {
    const current = values.labels as string[]
    if (current.some((l) => l.toLowerCase() === label.toLowerCase())) return
    setDraft((d) => ({ ...d, labels: [...current, label] }))
  }

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        breadcrumbs={breadcrumbs}
        title={name}
        description={repoStatus ? repoDescription(repoStatus) : undefined}
        actions={<HeaderActions name={name} repo={repoStatus} offline={status.isError} />}
      />
      {repoStatus?.removing && <p className="text-sm text-muted-foreground">Being removed. Running jobs finish first, and settings are read-only.</p>}
      {rejection && <RejectedAlert message={rejection} />}
      <RepoActivity name={name} repo={repoStatus} now={now} />
      <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_12rem]">
        <div className="flex min-w-0 flex-col gap-6">
          <FieldGroup id="capacity" title="Capacity">
            <Field
              label="Max"
              htmlFor="repo-max"
              help="Leave empty for the default: 1 in queue mode, no limit in all mode. Once set, it stays explicit."
              error={maxError}
              changed={is("max")}
            >
              <Input
                id="repo-max"
                type="number"
                min={0}
                max={99}
                className="w-24"
                disabled={locked}
                value={typeof values.max === "number" ? String(values.max) : ""}
                onChange={(e) => setNumber("max", e.target.value)}
              />
            </Field>
            <Field label="Warm" htmlFor="repo-warm" help="Runners kept ready in all mode. Leave empty for the default of 1." error={warmError} changed={is("warm")}>
              <Input
                id="repo-warm"
                type="number"
                min={0}
                max={99}
                className="w-24"
                disabled={locked}
                value={typeof values.warm === "number" ? String(values.warm) : ""}
                onChange={(e) => setNumber("warm", e.target.value)}
              />
            </Field>
          </FieldGroup>
          <FieldGroup id="labels" title="Labels">
            <Field label="Repo labels" help="Added to this repository's runners." changed={is("labels")}>
              <TagField
                label="Repo labels"
                value={values.labels as string[]}
                disabled={locked}
                onChange={(next) => setDraft((d) => ({ ...d, labels: next }))}
              />
            </Field>
            <Field label="Runners get" help="Runners already running keep their labels.">
              <ul aria-label="Runner labels" className="flex flex-wrap gap-x-3 gap-y-1 text-sm">
                {SYSTEM_LABELS.map((l) => (
                  <li key={`system-${l}`} className="font-mono">
                    {l}
                  </li>
                ))}
                {custom.map((c) => (
                  <li key={`custom-${c.label}`}>
                    <span className="font-mono">{c.label}</span>
                    {c.global && <span className="ml-1 text-muted-foreground">global</span>}
                  </li>
                ))}
              </ul>
            </Field>
          </FieldGroup>
          <FieldGroup id="cleanup" title="Cleanup">
            <Field label="Prefixes" help="Container name prefixes removed after each job." changed={is("cleanup_name_prefixes")}>
              <TagField
                label="Prefixes"
                value={values.cleanup_name_prefixes as string[]}
                disabled={locked}
                onChange={(next) => setDraft((d) => ({ ...d, cleanup_name_prefixes: next }))}
              />
            </Field>
          </FieldGroup>
          <LabelCheckGroup name={name} effective={effective} degraded={degraded} degradedReason={status.data?.degraded_reason} disabled={locked} onAddLabel={addLabel} />
          <RegistrationsGroup name={name} degraded={degraded} degradedReason={status.data?.degraded_reason} disabled={locked} />
        </div>
        <SectionNav items={SECTIONS} />
      </div>
      <SaveBar count={changed.length} saving={saving} disabled={status.isError} onSave={() => void save()} onDiscard={discard} />
      <UnsavedGuard count={changed.length} page="Repositories" saving={saving} onSave={save} onDiscard={discard} />
    </div>
  )
}
```

`Link to="/history" search={{ repo: name }}` type-checks against plan 2's `validateSearch: parseHistorySearch` on the History route.

In `web/src/anti-slop.test.ts`, add `"src/pages/repository.tsx"` to the pages group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/pages/repository.test.tsx src/components/label-check-group.test.tsx src/components/registrations-group.test.tsx src/a11y.test.tsx src/anti-slop.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): rebuild the repository page"
```

---

### Task 9: Delete the old activity summary

**Files:**
- Delete: `web/src/components/repo-summary.tsx`, `web/src/lib/activity.ts`, `web/src/lib/activity.test.ts`
- Modify: `web/src/api/hooks.ts` (remove `keys.repoActivity`, `ACTIVITY_LIMIT`, `useRepoActivity`)
- Modify: `web/src/lib/status.ts` (remove `repoState`), `web/src/lib/format.ts` (remove `maxText`)
- Test: `web/src/api/hooks-pages.test.tsx`, `web/src/lib/format.test.ts`

**Interfaces:**
- Consumes: the repository page no longer importing `repo-summary` (Task 8).
- Produces: nothing.

**Items:** 10

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing test**

There is no new behaviour to test; the check is that nothing still imports what this task deletes. Run (repository root):

```bash
grep -rn "repo-summary\|lib/activity\"\|from \"./activity\"\|useRepoActivity\|repoActivity\|ACTIVITY_LIMIT\|repoState\b\|maxText" web/src --include=*.ts --include=*.tsx
```

Expected before the change: hits only in the files this task deletes or edits (`components/repo-summary.tsx`, `lib/activity.ts`, `lib/activity.test.ts`, `api/hooks.ts`, `api/hooks-pages.test.tsx`, `lib/status.ts`, `lib/format.ts`, `lib/format.test.ts`). `lib/activity-view.ts` and its imports are a different module and stay. A hit anywhere else means Task 8 left an import behind: fix that first.

- [ ] **Step 2: Remove the code**

- Delete `web/src/components/repo-summary.tsx`, `web/src/lib/activity.ts` and `web/src/lib/activity.test.ts`.
- In `web/src/api/hooks.ts`, delete the `repoActivity: …` line from `keys`, the `export const ACTIVITY_LIMIT = 500` line, and the whole `useRepoActivity` function.
- In `web/src/api/hooks-pages.test.tsx`, delete the `describe("useRepoActivity", …)` block and change the import to `import { storageBusy, useLabelCheck } from "./hooks"`; drop `fixtures` from its import only if nothing else in the file uses it (the `storageBusy` tests use it, so it stays).
- In `web/src/lib/status.ts`, delete `repoState`.
- In `web/src/lib/format.ts`, delete `maxText`. In `web/src/lib/format.test.ts`, delete the test "maxText shows 0 as unlimited" and remove `maxText` from its import.

- [ ] **Step 3: Verify nothing refers to the removed code**

Run the grep from Step 1 again.
Expected: no output.

- [ ] **Step 4: Run the suite**

Run (in `web/`): `npm run typecheck && npx vitest run`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add -A web/src
git commit -m "refactor(web): delete the old activity summary"
```

---
### Task 10: Rework the toolchain helpers

**Files:**
- Modify: `web/src/lib/toolchains.ts` (replace)
- Create: `web/src/lib/toolchains.test.ts`
- Test: `web/src/components/toolchains-card.test.tsx` (move the helper tests out, update the queue line)
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: nothing.
- Produces: `queueText`, `groupByTool`, `ToolGroup`, `cacheParts`, `CachePart`, `toolchainsSummary` (Contracts, Toolchains). `CachePart.slot` is the chart colour number, so the class names stay in the component that draws them.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/toolchains.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import type { Storage, Toolchain } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { cacheParts, groupByTool, lastDotnetMajor, queueText, toolchainsSummary } from "./toolchains"

const tc = (tool: string, version: string, bytes = 1): Toolchain => ({ tool, version, arch: "x64", path: "", bytes, installed_at: "2026-10-01T00:00:00Z" })
const sdk = (version: string) => tc("dotnet", version)

describe("lastDotnetMajor", () => {
  it("names the major only for the last SDK of that major", () => {
    const all = [sdk("8.0.404"), sdk("8.0.100"), sdk("10.0.100")]
    expect(lastDotnetMajor(sdk("10.0.100"), all)).toBe("10")
    expect(lastDotnetMajor(sdk("8.0.404"), all)).toBe("")
    expect(lastDotnetMajor(tc("node", "22.11.0"), all)).toBe("")
  })
})

describe("queueText", () => {
  it("names the running operation, its progress and what waits after it", () => {
    expect(queueText(fixtures.storage.operations)).toBe("Installing node 24, extracting, 1 more queued")
    expect(queueText({ current: { id: "x", kind: "clear", target: "nuget", started_at: "" }, queued: 0, recent: [] })).toBe("Clearing nuget")
    expect(queueText({ current: { id: "x", kind: "prune", target: "all", started_at: "" }, queued: 3, recent: [] })).toBe("Prune all, 3 more queued")
    expect(queueText({ current: null, queued: 2, recent: [] })).toBe("2 queued")
    expect(queueText({ current: null, queued: 0, recent: [] })).toBe("")
  })
})

describe("groupByTool", () => {
  it("keeps each tool's versions together, tools in first-seen order", () => {
    const groups = groupByTool([tc("node", "22"), tc("python", "3.13"), tc("node", "24")])
    expect(groups.map((g) => [g.tool, g.rows.map((r) => r.version)])).toEqual([
      ["node", ["22", "24"]],
      ["python", ["3.13"]],
    ])
  })
})

describe("cacheParts", () => {
  it("gives the four largest tools a slot each and the rest, with the job folders, to Other", () => {
    const storage: Storage = {
      ...fixtures.storage,
      toolchains: [tc("node", "22", 500), tc("node", "24", 400), tc("go", "1.23", 300), tc("java", "21", 200), tc("python", "3.13", 100), tc("dotnet", "8.0", 50)],
      other_tool_cache: [{ name: "PyPy", bytes: 25 }],
    }
    expect(cacheParts(storage)).toEqual([
      { key: "node", label: "node", bytes: 900, slot: 1 },
      { key: "go", label: "go", bytes: 300, slot: 2 },
      { key: "java", label: "java", bytes: 200, slot: 3 },
      { key: "python", label: "python", bytes: 100, slot: 4 },
      { key: "__other", label: "Other", bytes: 75, slot: 5 },
    ])
  })

  it("is empty for an empty cache", () => {
    expect(cacheParts({ ...fixtures.storage, toolchains: null, other_tool_cache: null })).toEqual([])
  })
})

describe("toolchainsSummary", () => {
  it("counts the managed toolchains and sizes the whole cache", () => {
    expect(toolchainsSummary(fixtures.storage)).toBe("1 installed, 270.0 MB")
    expect(toolchainsSummary({ ...fixtures.storage, measured_at: undefined })).toBe("Not measured yet")
  })
})
```

The storage fixture holds node 22.11.0 at 190 MB and the job folder PyPy at 80 MB, measured at 14:03 (web/src/api/fixtures/storage.json).

In `web/src/components/toolchains-card.test.tsx`, delete the `describe("lastDotnetMajor", …)` and `describe("queueText", …)` blocks, the `sdk` helper only if nothing else in the file uses it (the .NET test does, so it stays), and the import of `@/lib/toolchains`; then change `"installing node 24 — extracting (1 queued)"` to `"Installing node 24, extracting, 1 more queued"`.

- [ ] **Step 2: Run the test to verify it fails**

Run (in `web/`): `npx vitest run src/lib/toolchains.test.ts`
Expected: FAIL: `groupByTool`, `cacheParts` and `toolchainsSummary` are not exported, and `queueText` returns the old dash form.

- [ ] **Step 3: Write the implementation**

Replace `web/src/lib/toolchains.ts` with:

```ts
import type { Operations, Storage, Toolchain } from "@/api/types"
import { humanBytes } from "./format"

const VERBS: Record<string, string> = { install: "Installing", remove: "Removing", clear: "Clearing" }

const sentence = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)

// Removing the last SDK of a .NET major also removes that major's runtimes
// and packs, so the confirmation says so.
export function lastDotnetMajor(t: Toolchain, all: Toolchain[]): string {
  if (t.tool !== "dotnet") return ""
  const major = t.version.split(".")[0] ?? ""
  return all.some((o) => o.tool === "dotnet" && o.version !== t.version && o.version.startsWith(`${major}.`)) ? "" : major
}

export function queueText(ops: Operations): string {
  const c = ops.current
  if (!c) return ops.queued > 0 ? `${ops.queued} queued` : ""
  let text = `${VERBS[c.kind] ?? sentence(c.kind)} ${c.target}`
  if (c.progress) text += `, ${c.progress}`
  if (ops.queued > 0) text += `, ${ops.queued} more queued`
  return text
}

export interface ToolGroup {
  tool: string
  rows: Toolchain[]
}

export function groupByTool(toolchains: Toolchain[]): ToolGroup[] {
  const groups: ToolGroup[] = []
  for (const t of toolchains) {
    const group = groups.find((g) => g.tool === t.tool)
    if (group) group.rows.push(t)
    else groups.push({ tool: t.tool, rows: [t] })
  }
  return groups
}

export interface CachePart {
  key: string
  label: string
  bytes: number
  slot: 1 | 2 | 3 | 4 | 5
}

const SLOTS = [1, 2, 3, 4] as const

// The four largest tools get a chart colour each; smaller tools and the
// folders jobs installed share the fifth as "Other". A tool is not a status,
// so the bar never uses a status colour.
export function cacheParts(storage: Storage): CachePart[] {
  const totals = new Map<string, number>()
  for (const t of storage.toolchains ?? []) totals.set(t.tool, (totals.get(t.tool) ?? 0) + t.bytes)
  const tools = [...totals].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
  const parts: CachePart[] = tools.slice(0, 4).map(([tool, bytes], i) => ({ key: tool, label: tool, bytes, slot: SLOTS[i] ?? 4 }))
  const rest = tools.slice(4).reduce((n, [, bytes]) => n + bytes, 0) + (storage.other_tool_cache ?? []).reduce((n, f) => n + f.bytes, 0)
  if (rest > 0) parts.push({ key: "__other", label: "Other", bytes: rest, slot: 5 })
  return parts
}

export function toolchainsSummary(storage: Storage): string {
  if (!storage.measured_at) return "Not measured yet"
  const tools = storage.toolchains ?? []
  const bytes = [...tools, ...(storage.other_tool_cache ?? [])].reduce((n, t) => n + t.bytes, 0)
  return `${tools.length} installed, ${humanBytes(bytes)}`
}
```

In `web/src/anti-slop.test.ts`, add `"src/lib/toolchains.ts"` to the lib group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/lib/toolchains.test.ts src/components/toolchains-card.test.tsx src/anti-slop.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): add the toolchain cache helpers"
```

---

### Task 11: Share the install actions

**Files:**
- Create: `web/src/lib/use-popular-set.ts`, `web/src/components/toolchain-actions.tsx`
- Modify: `web/src/components/toolchains-card.tsx` (use `ToolchainActions`)
- Test: `web/src/components/toolchains-card.test.tsx` (the popular set test)
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `InstallDialog` (Task 1), `ConfirmDialog`.
- Produces: `usePopularSet`, `ToolchainActions` (Contracts, Toolchains).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing test**

In `web/src/components/toolchains-card.test.tsx`, replace the test "installs the popular set only once confirmed" with:

```tsx
  it("installs the popular set only once confirmed", async () => {
    const { calls } = mockApi(routes({ "POST /api/toolchains": () => noContent() }))
    const { user } = renderApp("/toolchains")
    await user.click(await screen.findByRole("button", { name: "Install popular set" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(ask.getByText("Install the popular set?")).toBeInTheDocument()
    expect(
      ask.getByText("node 22, node 24, dotnet 8.0, dotnet 10.0, python 3.13, python 3.14, go latest, java 21, java 25. Versions already installed are skipped."),
    ).toBeInTheDocument()
    await user.click(ask.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "POST")).toBe(false)

    await user.click(screen.getByRole("button", { name: "Install popular set" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Install popular set" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ preset: "popular" }))
    expect((await screen.findAllByText("Queued: popular set")).length).toBeGreaterThan(0)
  })
```

- [ ] **Step 2: Run the test to verify it fails**

Run (in `web/`): `npx vitest run src/components/toolchains-card.test.tsx`
Expected: FAIL: the confirmation is still one long title, and the toast reads "queued: popular set".

- [ ] **Step 3: Write the implementation**

Create `web/src/lib/use-popular-set.ts`:

```ts
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { Confirm } from "@/components/confirm-dialog"

// The daemon's popular preset (internal/toolchain/set.go), listed in the
// same words the confirmation shows before queueing it.
const POPULAR = "node 22, node 24, dotnet 8.0, dotnet 10.0, python 3.13, python 3.14, go latest, java 21, java 25. Versions already installed are skipped."

export function usePopularSet() {
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const popular = useMutation({
    mutationFn: () => api.installPreset("popular"),
    onSuccess: () => {
      toast.success("Queued: popular set")
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.storage }),
  })
  return {
    confirm,
    close: () => setConfirm(null),
    ask: () => setConfirm({ title: "Install the popular set?", body: POPULAR, action: "Install popular set", run: () => popular.mutate() }),
  }
}
```

Create `web/src/components/toolchain-actions.tsx`:

```tsx
import { Button } from "darkraise-ui/components/button"
import { useState } from "react"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { InstallDialog } from "@/components/install-dialog"
import { usePopularSet } from "@/lib/use-popular-set"

export function ToolchainActions({ offline }: { offline: boolean }) {
  const popular = usePopularSet()
  const [installing, setInstalling] = useState(false)
  return (
    <div className="flex flex-wrap gap-2">
      <Button disabled={offline} onClick={() => setInstalling(true)}>
        Install
      </Button>
      <Button variant="outline" disabled={offline} onClick={popular.ask}>
        Install popular set
      </Button>
      <ConfirmDialog confirm={popular.confirm} onClose={popular.close} />
      <InstallDialog open={installing} onClose={() => setInstalling(false)} />
    </div>
  )
}
```

In `web/src/components/toolchains-card.tsx`:
- delete `POPULAR_QUESTION`, the `installing` state, the `popular` mutation, and the `InstallDialog` import and element;
- replace the whole `<div className="flex flex-wrap gap-2">…</div>` that holds the two install buttons with `<ToolchainActions offline={offline} />`, and add `import { ToolchainActions } from "@/components/toolchain-actions"`.

In `web/src/anti-slop.test.ts`, add `"src/components/toolchain-actions.tsx"` to the components group and `"src/lib/use-popular-set.ts"` to the lib group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/components/toolchains-card.test.tsx src/components/install-dialog.test.tsx src/pages/setup.test.tsx src/anti-slop.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "refactor(web): share the toolchain install actions"
```

---

### Task 12: Rebuild the installed toolchains list

**Files:**
- Create: `web/src/components/size-bar.tsx`, `web/src/components/toolchain-sections.tsx`
- Modify: `web/src/components/refused-hint.tsx` (replace), `web/src/pages/toolchains.tsx` (replace), `web/src/pages/setup.tsx` (`ToolchainsStep`)
- Delete: `web/src/components/toolchains-card.tsx`, `web/src/components/toolchains-card.test.tsx`
- Test: `web/src/pages/toolchains.test.tsx` (replace); the refused-hint strings in `web/src/pages/storage.test.tsx:20` and `web/src/components/storage-cards.test.tsx:20`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `ToolchainActions`, `usePopularSet` (Task 11); `queueText`, `groupByTool`, `lastDotnetMajor` (Task 10); `Section`, `ErrorLine` (Contracts, From plan 2).
- Produces: `SizeBar`, `InstalledSection`, `RefusedHint`'s copy (Contracts, Toolchains).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Replace `web/src/pages/toolchains.test.tsx` with:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { Toolchain } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const routes = (over: Record<string, unknown> = {}) => authedRoutes({ "GET /api/storage": fixtures.storage, ...over })

const sdk = (version: string): Toolchain => ({ tool: "dotnet", version, arch: "x64", path: "", bytes: 1, installed_at: "2026-10-01T00:00:00Z" })

const installed = () => within(screen.getByRole("region", { name: "Installed" }))

describe("Toolchains page", () => {
  it("offers Install and the popular set in the header", async () => {
    mockApi(routes())
    renderApp("/toolchains")
    expect(await screen.findByRole("heading", { name: "Toolchains", level: 1 })).toBeInTheDocument()
    expect(await screen.findByRole("button", { name: "Install" })).toBeEnabled()
    expect(screen.getByRole("button", { name: "Install popular set" })).toBeEnabled()
  })

  it("groups the installed toolchains by tool", async () => {
    mockApi(routes())
    renderApp("/toolchains")
    await screen.findByRole("region", { name: "Installed" })
    expect(installed().getByText("node", { selector: "th" })).toHaveAttribute("scope", "rowgroup")
    const row = within(installed().getByText("22.11.0").closest("tr") as HTMLElement)
    expect(row.getByText("22.11.0")).toHaveClass("font-mono")
    expect(row.getByText("x64")).toHaveClass("text-muted-foreground")
    expect(row.getByText("190.0 MB")).toHaveClass("font-mono")
    expect(row.getByText("2026-09-30")).toHaveClass("font-mono")
    expect(row.getByRole("button", { name: "Remove node 22.11.0" })).toBeEnabled()
  })

  it("lists the folders jobs installed, without actions", async () => {
    mockApi(routes())
    renderApp("/toolchains")
    await screen.findByRole("region", { name: "Installed" })
    expect(installed().getByText("Installed by jobs")).toBeInTheDocument()
    expect(installed().getByText("A job's own setup step installed these. ghr does not manage them.")).toBeInTheDocument()
    expect(installed().getByText("PyPy")).toBeInTheDocument()
    expect(installed().getByText("80.0 MB")).toHaveClass("font-mono")
    expect(installed().queryByRole("button", { name: /PyPy/ })).toBeNull()
  })

  it("shows the running operation and the refusal while jobs run", async () => {
    mockApi(routes())
    renderApp("/toolchains")
    expect(await screen.findByText("Installing node 24, extracting, 1 more queued")).toBeInTheDocument()
    expect(screen.getByText("Remove: refused while 1 job runs")).toHaveClass("text-muted-foreground")
  })

  it("says when the tool cache is empty, with the popular set", async () => {
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, toolchains: null, other_tool_cache: [] } }))
    renderApp("/toolchains")
    expect(await screen.findByText("The tool cache is empty")).toBeInTheDocument()
    expect(screen.getAllByRole("button", { name: "Install popular set" })).toHaveLength(2)
  })

  it("shows a failed read with a retry", async () => {
    mockApi(routes({ "GET /api/storage": () => json({ error: "storage unavailable" }, 500) }))
    renderApp("/toolchains")
    expect(await screen.findByText("storage unavailable")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument()
    expect(screen.queryByText("Loading")).toBeNull()
  })

  it("removes a toolchain only once confirmed", async () => {
    const { calls } = mockApi(routes({ "DELETE /api/toolchains/node/22.11.0": () => noContent() }))
    const { user } = renderApp("/toolchains")
    await user.click(await screen.findByRole("button", { name: "Remove node 22.11.0" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(ask.getByText("Remove node 22.11.0?")).toBeInTheDocument()
    expect(ask.getByText("Frees 190.0 MB. Jobs that need it install it again.")).toBeInTheDocument()
    await user.click(ask.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "DELETE")).toBe(false)

    await user.click(screen.getByRole("button", { name: "Remove node 22.11.0" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Remove" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/toolchains/node/22.11.0")).toBe(true))
    expect((await screen.findAllByText("Queued: remove node 22.11.0")).length).toBeGreaterThan(0)
  })

  it("warns in the body when removing the last .NET SDK of a major", async () => {
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, toolchains: [sdk("8.0.404")] } }))
    const { user } = renderApp("/toolchains")
    await user.click(await screen.findByRole("button", { name: "Remove dotnet 8.0.404" }))
    expect(
      await screen.findByText("Frees 1 B. Jobs that need it install it again. It is the last .NET 8 SDK, so this also removes the 8.0 runtimes and packs."),
    ).toBeInTheDocument()
  })

  it("installs the popular set only once confirmed", async () => {
    const { calls } = mockApi(routes({ "POST /api/toolchains": () => noContent() }))
    const { user } = renderApp("/toolchains")
    await user.click(await screen.findByRole("button", { name: "Install popular set" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(ask.getByText("Install the popular set?")).toBeInTheDocument()
    await user.click(ask.getByRole("button", { name: "Install popular set" }))
    await waitFor(() => expect(calls.find((c) => c.method === "POST")?.body).toEqual({ preset: "popular" }))
  })

  it("names the remove button in a tooltip", async () => {
    mockApi(routes())
    const { user } = renderApp("/toolchains")
    await user.hover(await screen.findByRole("button", { name: "Remove node 22.11.0" }))
    expect(await screen.findByRole("tooltip")).toHaveTextContent("Remove")
  })
})
```

Change the refused-hint strings the Storage tests expect:
- `web/src/pages/storage.test.tsx`: `"Unused volumes: refused while 1 jobs run"` → `"Unused volumes: refused while 1 job runs"`
- `web/src/components/storage-cards.test.tsx`: `"Clear: refused while 1 jobs run"` → `"Clear: refused while 1 job runs"`

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/pages/toolchains.test.tsx src/pages/storage.test.tsx src/components/storage-cards.test.tsx`
Expected: FAIL: there is no "Installed" region, the actions are not in the header, and the refused hint still reads "1 jobs run".

- [ ] **Step 3: Write the implementation**

Create `web/src/components/size-bar.tsx`:

```tsx
import { humanBytes } from "@/lib/format"

// The bar compares a row with the largest row in view, not with the disk.
export function SizeBar({ bytes, longest }: { bytes: number; longest: number }) {
  return (
    <span className="flex items-center justify-end gap-2">
      <span className="font-mono">{humanBytes(bytes)}</span>
      <span aria-hidden="true" className="h-1.5 w-20 rounded-[2px] bg-muted">
        <span className="block h-1.5 rounded-[2px] bg-primary" style={{ width: `${Math.round((bytes / Math.max(1, longest)) * 100)}%` }} />
      </span>
    </span>
  )
}
```

Replace `web/src/components/refused-hint.tsx` with:

```tsx
import { TriangleAlert } from "lucide-react"
import type { Status } from "@/api/types"
import { plural } from "@/lib/format"

function busyJobs(status: Status | undefined): number {
  return status?.instances.filter((i) => i.state === "busy").length ?? 0
}

export function RefusedHint({ what, status }: { what: string; status: Status | undefined }) {
  const n = busyJobs(status)
  if (n === 0) return null
  return (
    <p className="flex items-center gap-1.5 text-sm text-muted-foreground">
      <TriangleAlert size={15} aria-hidden="true" className="shrink-0 text-warning" />
      {`${what}: refused while ${plural(n, "job")} ${n === 1 ? "runs" : "run"}`}
    </p>
  )
}
```

Create `web/src/components/toolchain-sections.tsx`:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { Trash2 } from "lucide-react"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { Status, Storage, Toolchain } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { Section } from "@/components/page/section"
import { RefusedHint } from "@/components/refused-hint"
import { SizeBar } from "@/components/size-bar"
import { dateTime, humanBytes } from "@/lib/format"
import { groupByTool, lastDotnetMajor, queueText } from "@/lib/toolchains"
import { usePopularSet } from "@/lib/use-popular-set"

export function InstalledSection({ storage, status, offline }: { storage: Storage; status: Status | undefined; offline: boolean }) {
  const queryClient = useQueryClient()
  const popular = usePopularSet()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const remove = useMutation({
    mutationFn: (t: Toolchain) => api.removeToolchain(t.tool, t.version),
    onSuccess: (_data, t) => {
      toast.success(`Queued: remove ${t.tool} ${t.version}`)
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: keys.storage }),
  })
  const tools = storage.toolchains ?? []
  const others = storage.other_tool_cache ?? []
  const queue = queueText(storage.operations)
  const longest = Math.max(1, ...tools.map((t) => t.bytes))

  function askRemove(t: Toolchain) {
    const major = lastDotnetMajor(t, tools)
    let body = `Frees ${humanBytes(t.bytes)}. Jobs that need it install it again.`
    if (major) body += ` It is the last .NET ${major} SDK, so this also removes the ${major}.0 runtimes and packs.`
    setConfirm({ title: `Remove ${t.tool} ${t.version}?`, body, action: "Remove", destructive: true, run: () => remove.mutate(t) })
  }

  return (
    <Section title="Installed">
      <div className="flex flex-col gap-3">
        {queue && (
          <p className="flex items-center gap-2 text-sm">
            <Spinner aria-hidden="true" />
            <span>{queue}</span>
          </p>
        )}
        <RefusedHint what="Remove" status={status} />
        {tools.length === 0 && others.length === 0 ? (
          <div className="flex flex-col items-start gap-2">
            <p className="text-sm text-muted-foreground">The tool cache is empty</p>
            <Button variant="outline" disabled={offline} onClick={popular.ask}>
              Install popular set
            </Button>
          </div>
        ) : (
          <>
            {tools.length > 0 && (
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Version</TableHead>
                      <TableHead>Arch</TableHead>
                      <TableHead className="text-right">Size</TableHead>
                      <TableHead>Installed</TableHead>
                      <TableHead>
                        <span className="sr-only">Actions</span>
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  {groupByTool(tools).map((g) => (
                    <TableBody key={g.tool}>
                      <TableRow>
                        <th scope="rowgroup" colSpan={5} className="pt-4 pb-1 text-left text-sm font-medium">
                          {g.tool}
                        </th>
                      </TableRow>
                      {g.rows.map((t) => (
                        <TableRow key={`${t.tool}/${t.version}/${t.arch}`}>
                          <TableCell className="font-mono">{t.version}</TableCell>
                          <TableCell className="font-mono text-muted-foreground">{t.arch}</TableCell>
                          <TableCell>
                            <SizeBar bytes={t.bytes} longest={longest} />
                          </TableCell>
                          <TableCell className="font-mono">{dateTime(t.installed_at).slice(0, 10)}</TableCell>
                          <TableCell className="text-right">
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <Button size="icon" variant="ghost" aria-label={`Remove ${t.tool} ${t.version}`} disabled={offline} onClick={() => askRemove(t)}>
                                  <Trash2 size={15} aria-hidden="true" />
                                </Button>
                              </TooltipTrigger>
                              <TooltipContent>Remove</TooltipContent>
                            </Tooltip>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  ))}
                </Table>
              </div>
            )}
            {others.length > 0 && (
              <div className="flex flex-col gap-1">
                <h3 className="text-sm font-medium">Installed by jobs</h3>
                <p className="text-sm text-muted-foreground">A job's own setup step installed these. ghr does not manage them.</p>
                <ul className="flex flex-col gap-1 text-sm">
                  {others.map((f) => (
                    <li key={f.name} className="flex justify-between gap-4">
                      <span>{f.name}</span>
                      <span className="font-mono">{humanBytes(f.bytes)}</span>
                    </li>
                  ))}
                </ul>
              </div>
            )}
          </>
        )}
      </div>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
      <ConfirmDialog confirm={popular.confirm} onClose={popular.close} />
    </Section>
  )
}
```

Replace `web/src/pages/toolchains.tsx` with:

```tsx
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useStatus, useStorage } from "@/api/hooks"
import { ErrorLine } from "@/components/page/error-line"
import { ToolchainActions } from "@/components/toolchain-actions"
import { InstalledSection } from "@/components/toolchain-sections"
import { useOperationToasts } from "@/lib/use-operation-toasts"
import { errorText } from "@/query"

export function ToolchainsPage() {
  const status = useStatus()
  const storage = useStorage(status.data)
  useOperationToasts(storage.data)
  const offline = status.isError
  return (
    <div className="flex flex-col gap-4">
      <PageHeader title="Toolchains" actions={<ToolchainActions offline={offline} />} />
      {storage.isError && <ErrorLine onRetry={() => void storage.refetch()}>{errorText(storage.error)}</ErrorLine>}
      {storage.data ? (
        <InstalledSection storage={storage.data} status={status.data} offline={offline} />
      ) : (
        !storage.isError && <Spinner label="Loading" />
      )}
    </div>
  )
}
```

In `web/src/pages/setup.tsx`, replace `import { ToolchainsCard } from "@/components/toolchains-card"` with `import { ToolchainActions } from "@/components/toolchain-actions"` and `import { InstalledSection } from "@/components/toolchain-sections"`, and in `ToolchainsStep` replace the line `{storage.data && <ToolchainsCard storage={storage.data} status={status.data} offline={status.isError} />}` with:

```tsx
      <ToolchainActions offline={status.isError} />
      {storage.data && <InstalledSection storage={storage.data} status={status.data} offline={status.isError} />}
```

Delete `web/src/components/toolchains-card.tsx` and `web/src/components/toolchains-card.test.tsx` (their behaviour is covered by `pages/toolchains.test.tsx` now; confirm `grep -rn "toolchains-card\|ToolchainsCard" web/src` prints nothing).

In `web/src/anti-slop.test.ts`, add `"src/components/refused-hint.tsx"`, `"src/components/size-bar.tsx"` and `"src/components/toolchain-sections.tsx"` to the components group, and `"src/pages/toolchains.tsx"` to the pages group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/pages src/components src/anti-slop.test.ts`
Expected: PASS, including the setup wizard's toolchain step.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add -A web/src
git commit -m "feat(web): rebuild the installed toolchains list"
```

---

### Task 13: Draw the tool cache bar

**Files:**
- Modify: `web/src/components/toolchain-sections.tsx` (add `ToolCacheSection`)
- Modify: `web/src/pages/toolchains.tsx` (header description, the section), `web/src/pages/setup.tsx` (`ToolchainsStep`)
- Test: `web/src/pages/toolchains.test.tsx`, `web/src/a11y.test.tsx`

**Interfaces:**
- Consumes: `cacheParts`, `CachePart`, `toolchainsSummary` (Task 10); `InstalledSection` (Task 12).
- Produces: `ToolCacheSection` (Contracts, Toolchains).

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing tests**

Append inside `describe("Toolchains page", …)` in `web/src/pages/toolchains.test.tsx`:

```tsx
  it("draws the tool cache by tool, in chart colours", async () => {
    mockApi(routes())
    const { container } = renderApp("/toolchains")
    expect(await screen.findByRole("img", { name: "Tool cache: node 190.0 MB, Other 80.0 MB" })).toBeInTheDocument()
    expect(container.querySelector('[data-part="node"]')).toHaveClass("bg-chart-1")
    expect(container.querySelector('[data-part="__other"]')).toHaveClass("bg-chart-5")
    const legend = within(screen.getByRole("region", { name: "Tool cache" }))
    expect(legend.getByText("Other")).toBeInTheDocument()
    expect(legend.getByText("190.0 MB")).toHaveClass("font-mono")
  })

  it("sums up the cache in the header", async () => {
    mockApi(routes())
    renderApp("/toolchains")
    expect(await screen.findByText("1 installed, 270.0 MB")).toBeInTheDocument()
  })

  it("says the cache is not measured before the first measurement", async () => {
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, measured_at: undefined } }))
    renderApp("/toolchains")
    expect(await screen.findByText("Not measured yet")).toBeInTheDocument()
  })

  it("draws no tool cache bar for an empty cache", async () => {
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, toolchains: null, other_tool_cache: [] } }))
    renderApp("/toolchains")
    await screen.findByText("The tool cache is empty")
    expect(screen.queryByRole("region", { name: "Tool cache" })).toBeNull()
  })
```

Append to `web/src/a11y.test.tsx`:

```tsx
describe("Toolchains accessibility", () => {
  it(
    "has no axe violations",
    async () => {
      mockApi(authedRoutes())
      renderApp("/toolchains")
      await screen.findByRole("img", { name: /^Tool cache: / })
      await screen.findByText("Installing node 24, extracting, 1 more queued")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/pages/toolchains.test.tsx src/a11y.test.tsx`
Expected: FAIL: no "Tool cache" section or header description.

- [ ] **Step 3: Write the implementation**

In `web/src/components/toolchain-sections.tsx`, add `import { cacheParts, groupByTool, lastDotnetMajor, queueText, type CachePart } from "@/lib/toolchains"` in place of the existing `@/lib/toolchains` import, and append:

```tsx
const SLOT_CLASS: Record<CachePart["slot"], string> = {
  1: "bg-chart-1",
  2: "bg-chart-2",
  3: "bg-chart-3",
  4: "bg-chart-4",
  5: "bg-chart-5",
}

export function ToolCacheSection({ storage }: { storage: Storage }) {
  const parts = cacheParts(storage)
  const total = parts.reduce((n, p) => n + p.bytes, 0)
  if (total === 0) return null
  return (
    <Section title="Tool cache">
      <div className="flex flex-col gap-3">
        <div
          role="img"
          aria-label={`Tool cache: ${parts.map((p) => `${p.label} ${humanBytes(p.bytes)}`).join(", ")}`}
          className="flex h-3 overflow-hidden rounded-[3px] bg-muted"
        >
          {parts.map((p) => (
            <span key={p.key} data-part={p.key} className={SLOT_CLASS[p.slot]} style={{ width: `${(p.bytes / total) * 100}%` }} />
          ))}
        </div>
        <ul className="grid grid-cols-2 gap-x-4 gap-y-1 text-sm sm:grid-cols-3">
          {parts.map((p) => (
            <li key={p.key} className="flex items-center gap-2">
              <span aria-hidden="true" className={`size-2.5 shrink-0 rounded-[2px] ${SLOT_CLASS[p.slot]}`} />
              <span className="flex-1">{p.label}</span>
              <span className="font-mono text-muted-foreground">{humanBytes(p.bytes)}</span>
            </li>
          ))}
        </ul>
      </div>
    </Section>
  )
}
```

The `bg-chart-1` to `bg-chart-5` utilities exist because darkraise-ui maps `--color-chart-N` to the theme's `--chart-N` (web/src/styles/ghr-theme.css:72-76 and 124-128 set both modes).

In `web/src/pages/toolchains.tsx`, add `import { toolchainsSummary } from "@/lib/toolchains"`, change the `InstalledSection` import to `import { InstalledSection, ToolCacheSection } from "@/components/toolchain-sections"`, give the header `description={storage.data ? toolchainsSummary(storage.data) : undefined}`, and render the cache above the list:

```tsx
      {storage.data ? (
        <>
          <ToolCacheSection storage={storage.data} />
          <InstalledSection storage={storage.data} status={status.data} offline={offline} />
        </>
      ) : (
        !storage.isError && <Spinner label="Loading" />
      )}
```

In `web/src/pages/setup.tsx`, import `ToolCacheSection` beside `InstalledSection` and render `{storage.data && <ToolCacheSection storage={storage.data} />}` on the line above the `InstalledSection` line in `ToolchainsStep`.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/pages/toolchains.test.tsx src/pages/setup.test.tsx src/a11y.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): draw the tool cache by tool"
```

---
### Task 14: Show the Docker root path on the Dashboard

**Files:**
- Modify: `web/src/pages/dashboard.tsx` (the Disk section's `aside`)
- Test: `web/src/pages/dashboard.test.tsx`

**Interfaces:**
- Consumes: plan 1's `Status.disk_root` (the status fixture sets it to `/var/lib/docker`).
- Produces: nothing new.

**Items:** 11

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 0 = 2

- [ ] **Step 1: Write the failing test**

Append inside the top-level `describe` of `web/src/pages/dashboard.test.tsx` (it already has a `region(name)` helper returning `within(screen.getByRole("region", { name }))`, used by "shows disk use"):

```tsx
  it("shows Docker's data root beside the Disk heading once it is measured", async () => {
    mockApi(authedRoutes())
    renderApp("/")
    await screen.findByRole("region", { name: "Disk" })
    expect(region("Disk").getByText("/var/lib/docker")).toHaveClass("font-mono", "text-muted-foreground")
  })

  it("leaves the data root out until it is measured", async () => {
    mockApi(authedRoutes({ "GET /api/status": { ...fixtures.status, disk_root: undefined } }))
    renderApp("/")
    await screen.findByRole("region", { name: "Disk" })
    expect(region("Disk").queryByText("/var/lib/docker")).toBeNull()
  })
```

If the file does not import `fixtures` yet, add it to the `@/test/fixtures` import.

- [ ] **Step 2: Run the test to verify it fails**

Run (in `web/`): `npx vitest run src/pages/dashboard.test.tsx`
Expected: FAIL: the Disk section shows no path.

- [ ] **Step 3: Write the implementation**

In `web/src/pages/dashboard.tsx`, replace the Disk section's `aside` with:

```tsx
            aside={
              <>
                {st.disk_root && <span className="font-mono text-sm text-muted-foreground">{st.disk_root}</span>}
                <Link to="/storage" className="ml-auto text-sm text-primary hover:underline">
                  Storage
                </Link>
              </>
            }
```

- [ ] **Step 4: Run the test to verify it passes**

Run (in `web/`): `npx vitest run src/pages/dashboard.test.tsx src/a11y.test.tsx`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): show the Docker root on the Dashboard"
```

---

### Task 15: Add the Storage helpers

**Files:**
- Create: `web/src/lib/storage.ts`
- Test: `web/src/lib/storage.test.ts`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: plan 1's `Status.disk_root`.
- Produces: `storageSummary`, `PruneAsk`, `PruneRow`, `pruneRows`, `standardPrune`, `lastPruneText` (Contracts, Storage).

**Items:** 11

**Implementer:** dr-superpowers:impl-sonnet-low
**Evaluation:** files 1 - spec 0 - coupling 0 - risk 0 = 1

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/storage.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import type { Storage } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { lastPruneText, pruneRows, standardPrune, storageSummary } from "./storage"

describe("storageSummary", () => {
  it("names the use, the data root and the high-water mark", () => {
    expect(storageSummary(fixtures.status, 80)).toBe("Docker uses 61% of /var/lib/docker and prunes above 80%")
  })
  it("leaves out the root until it is known", () => {
    expect(storageSummary({ ...fixtures.status, disk_root: undefined }, 85)).toBe("Docker uses 61% and prunes above 85%")
  })
  it("says when nothing is measured yet", () => {
    expect(storageSummary({ ...fixtures.status, disk_total_bytes: 0 }, 80)).toBe("Not measured yet")
  })
})

describe("pruneRows", () => {
  it("says what each targeted prune removes and how much Docker can free", () => {
    expect(pruneRows(fixtures.storage, fixtures.config).map((r) => [r.scope, r.text, r.action, r.destructive])).toEqual([
      ["build-cache-keep", "Build cache beyond 20GB", "Trim build cache", false],
      ["build-cache-all", "All build cache, up to 380.0 MB", "Remove build cache", true],
      ["dangling-images", "Dangling images", "Remove dangling images", false],
      ["unused-volumes", "Unused volumes", "Remove unused volumes", true],
    ])
  })

  it("splits each confirmation into a question and its consequence", () => {
    const withVolumes: Storage = {
      ...fixtures.storage,
      docker: { ...fixtures.storage.docker, rows: [...(fixtures.storage.docker.rows ?? []), { type: "Local Volumes", count: 3, active: 1, bytes: 1_500_000_000, reclaimable: 1_200_000_000 }] },
    }
    expect(pruneRows(withVolumes, fixtures.config).map((r) => [r.text, r.title, r.body])).toEqual([
      ["Build cache beyond 20GB", "Trim the build cache to 20GB?", "Build cache beyond that size is removed."],
      ["All build cache, up to 380.0 MB", "Remove all build cache?", "Frees up to 380.0 MB. The next builds start cold."],
      ["Dangling images", "Remove dangling images?", "Images that are untagged and used by no container are removed."],
      [
        "Unused volumes, up to 1.2 GB",
        "Remove unused volumes?",
        "Frees up to 1.2 GB. Every volume no container uses is removed. It is refused while jobs run.",
      ],
    ])
  })

  it("names the keep size even before the config is read", () => {
    expect(pruneRows(fixtures.storage, undefined)[0]?.text).toBe("Build cache beyond the build cache keep size")
  })
})

describe("standardPrune", () => {
  it("asks before the standard prune", () => {
    expect(standardPrune(fixtures.config)).toEqual({
      title: "Prune now?",
      body: "Removes build cache beyond 20GB, dangling images, and history and logs past retention.",
      action: "Prune",
      destructive: false,
    })
  })
})

describe("lastPruneText", () => {
  it("names the trigger, scope, time and outcome", () => {
    const p = fixtures.storage.last_prune
    if (!p) throw new Error("fixture has a last prune")
    expect(lastPruneText(p)).toBe("Last prune: manual build-cache-all at 13:16, ok")
    expect(lastPruneText({ ...p, trigger: "auto" })).toBe("Last prune: auto at 13:16, ok")
  })
})
```

- [ ] **Step 2: Run the test to verify it fails**

Run (in `web/`): `npx vitest run src/lib/storage.test.ts`
Expected: FAIL: `Cannot find module './storage'`.

- [ ] **Step 3: Write the implementation**

Create `web/src/lib/storage.ts`:

```ts
import type { Config, LastPrune, PruneScope, Status, Storage } from "@/api/types"
import { hhmm, humanBytes } from "./format"

export function storageSummary(status: Status, highWater: number): string {
  if (status.disk_total_bytes === 0) return "Not measured yet"
  const where = status.disk_root ? ` of ${status.disk_root}` : ""
  return `Docker uses ${status.disk_pct}%${where} and prunes above ${highWater}%`
}

export interface PruneAsk {
  title: string
  body: string
  action: string
  destructive: boolean
}

export interface PruneRow extends PruneAsk {
  scope: Exclude<PruneScope, "standard">
  text: string
}

const KEEP = "the build cache keep size"

function reclaimable(storage: Storage, type: string): number {
  return (storage.docker.rows ?? []).find((r) => r.type === type)?.reclaimable ?? 0
}

// Docker reports what a prune can free only for some types; a row without a
// figure says what goes and leaves the size out.
export function pruneRows(storage: Storage, config: Config | undefined): PruneRow[] {
  const keep = config?.build_cache_keep ?? KEEP
  const cache = reclaimable(storage, "Build Cache")
  const volumes = reclaimable(storage, "Local Volumes")
  const upTo = (n: number) => (n > 0 ? `, up to ${humanBytes(n)}` : "")
  const frees = (n: number) => (n > 0 ? `Frees up to ${humanBytes(n)}. ` : "")
  return [
    {
      scope: "build-cache-keep",
      text: `Build cache beyond ${keep}`,
      action: "Trim build cache",
      destructive: false,
      title: `Trim the build cache to ${keep}?`,
      body: "Build cache beyond that size is removed.",
    },
    {
      scope: "build-cache-all",
      text: `All build cache${upTo(cache)}`,
      action: "Remove build cache",
      destructive: true,
      title: "Remove all build cache?",
      body: `${frees(cache)}The next builds start cold.`,
    },
    {
      scope: "dangling-images",
      text: "Dangling images",
      action: "Remove dangling images",
      destructive: false,
      title: "Remove dangling images?",
      body: "Images that are untagged and used by no container are removed.",
    },
    {
      scope: "unused-volumes",
      text: `Unused volumes${upTo(volumes)}`,
      action: "Remove unused volumes",
      destructive: true,
      title: "Remove unused volumes?",
      body: `${frees(volumes)}Every volume no container uses is removed. It is refused while jobs run.`,
    },
  ]
}

export function standardPrune(config: Config | undefined): PruneAsk {
  const keep = config?.build_cache_keep ?? KEEP
  return {
    title: "Prune now?",
    body: `Removes build cache beyond ${keep}, dangling images, and history and logs past retention.`,
    action: "Prune",
    destructive: false,
  }
}

export function lastPruneText(p: LastPrune): string {
  const what = p.trigger === "auto" ? "auto" : `${p.trigger} ${p.scope}`
  return `Last prune: ${what} at ${hhmm(new Date(p.finished_at ?? p.started_at))}, ${p.outcome ?? "unknown"}`
}
```

In `web/src/anti-slop.test.ts`, add `"src/lib/storage.ts"` to the lib group.

- [ ] **Step 4: Run the test to verify it passes**

Run (in `web/`): `npx vitest run src/lib/storage.test.ts src/anti-slop.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): add the storage summaries"
```

---

### Task 16: Rebuild the disk and prune sections

**Files:**
- Create: `web/src/lib/use-prune.ts`, `web/src/components/storage-disk.tsx`, `web/src/components/prune-section.tsx`
- Modify: `web/src/pages/storage.tsx` (replace)
- Test: `web/src/pages/storage.test.tsx` (replace), `web/src/components/storage-cards.test.tsx:15`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `storageSummary`, `pruneRows`, `standardPrune`, `lastPruneText`, `PruneAsk` (Task 15); `RefusedHint` (Task 12); `DiskBreakdown`; `Section`, `ErrorLine` (Contracts, From plan 2).
- Produces: `usePrune`, `DiskSection`, `PruneSection` (Contracts, Storage).

**Items:** 11

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Replace `web/src/pages/storage.test.tsx` with:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const routes = (over: Record<string, unknown> = {}) => authedRoutes({ "GET /api/storage": fixtures.storage, ...over })

const region = (name: string) => within(screen.getByRole("region", { name }))

describe("Storage page", () => {
  it("sums up Docker's disk use in the header", async () => {
    mockApi(routes())
    renderApp("/storage")
    expect(await screen.findByText("Docker uses 61% of /var/lib/docker and prunes above 80%")).toBeInTheDocument()
  })

  it.each([
    [{ disk_root: undefined }, "Docker uses 61% and prunes above 80%"],
    [{ disk_total_bytes: 0 }, "Not measured yet"],
  ])("reads a status with %j as %s", async (over, text) => {
    mockApi(routes({ "GET /api/status": { ...fixtures.status, ...over } }))
    renderApp("/storage")
    expect((await screen.findAllByText(text)).length).toBeGreaterThan(0)
  })

  it("shows Docker's rows, the build cache types, the data root and the last prune", async () => {
    mockApi(routes())
    renderApp("/storage")
    await screen.findByRole("region", { name: "Disk" })
    const table = within(region("Disk").getByRole("table"))
    expect(table.getByRole("columnheader", { name: "Reclaimable" })).toHaveClass("text-right")
    expect(table.getByText("8.1 GB")).toHaveClass("font-mono", "text-right")
    expect(table.getByText("regular")).toBeInTheDocument()
    expect(region("Disk").getByText("/var/lib/docker")).toHaveClass("font-mono")
    expect(region("Disk").getByText("Last prune: manual build-cache-all at 13:16, ok")).toBeInTheDocument()
    expect(region("Disk").getByText("all build cache: 6.8 MB freed")).toBeInTheDocument()
  })

  it("offers each targeted prune with what it can free", async () => {
    mockApi(routes())
    renderApp("/storage")
    const prune = within(await screen.findByRole("region", { name: "Prune" }))
    expect(prune.getByText("Build cache beyond 20GB")).toBeInTheDocument()
    expect(prune.getByText("All build cache, up to 380.0 MB")).toBeInTheDocument()
    expect(prune.getByText("Dangling images")).toBeInTheDocument()
    expect(prune.getByText("Unused volumes")).toBeInTheDocument()
    expect(prune.getByRole("button", { name: "Trim build cache" })).toBeEnabled()
    expect(prune.getByText("Unused volumes: refused while 1 job runs")).toBeInTheDocument()
  })

  it("leaves toolchains to the Toolchains page", async () => {
    mockApi(routes())
    renderApp("/storage")
    await screen.findByRole("region", { name: "Disk" })
    expect(screen.queryByRole("button", { name: "Install popular set" })).toBeNull()
  })

  it("disables pruning while the daemon is unreachable", { timeout: 10_000 }, async () => {
    let reads = 0
    mockApi(routes({ "GET /api/status": () => (++reads === 1 ? fixtures.status : json({ error: "connection refused" }, 502)) }))
    renderApp("/storage")
    expect(await screen.findByText("Reconnecting", {}, { timeout: 4000 })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Prune" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "Trim build cache" })).toBeDisabled()
  })

  it("says when nothing was pruned since the daemon started", async () => {
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, last_prune: null } }))
    renderApp("/storage")
    expect(await screen.findByText("No prune since the daemon started")).toBeInTheDocument()
  })

  it("disables pruning while a prune runs", async () => {
    mockApi(routes({ "GET /api/status": { ...fixtures.status, maintenance: { running: true } } }))
    renderApp("/storage")
    expect(await screen.findByText("Pruning")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Prune" })).toBeDisabled()
  })

  it("shows a failed read with a retry", async () => {
    mockApi(routes({ "GET /api/storage": () => json({ error: "storage unavailable" }, 500) }))
    renderApp("/storage")
    expect(await screen.findByText("storage unavailable")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument()
    expect(screen.queryByText("Loading")).toBeNull()
  })

  it.each([
    ["Prune", "Prune now?", "Removes build cache beyond 20GB, dangling images, and history and logs past retention."],
    ["Trim build cache", "Trim the build cache to 20GB?", "Build cache beyond that size is removed."],
    ["Remove build cache", "Remove all build cache?", "Frees up to 380.0 MB. The next builds start cold."],
    ["Remove dangling images", "Remove dangling images?", "Images that are untagged and used by no container are removed."],
    ["Remove unused volumes", "Remove unused volumes?", "Every volume no container uses is removed. It is refused while jobs run."],
  ])("asks before %s and sends nothing on Cancel", async (label, title, body) => {
    const { calls } = mockApi(routes())
    const { user } = renderApp("/storage")
    await user.click(await screen.findByRole("button", { name: label }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(ask.getByText(title)).toBeInTheDocument()
    expect(ask.getByText(body)).toBeInTheDocument()
    expect(ask.getByRole("button", { name: label })).toBeInTheDocument()
    await user.click(ask.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "POST")).toBe(false)
  })

  it("prunes a scope once confirmed", async () => {
    const { calls } = mockApi(routes({ "POST /api/prune/build-cache-all": () => noContent() }))
    const { user } = renderApp("/storage")
    await user.click(await screen.findByRole("button", { name: "Remove build cache" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Remove build cache" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/prune/build-cache-all")).toBe(true))
    expect((await screen.findAllByText("Prune started")).length).toBeGreaterThan(0)
  })

  it("sends the standard prune to /prune", async () => {
    const { calls } = mockApi(routes({ "POST /api/prune": () => noContent() }))
    const { user } = renderApp("/storage")
    await user.click(await screen.findByRole("button", { name: "Prune" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Prune" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/prune")).toBe(true))
  })
})
```

The page now draws `DiskBreakdown`, whose legend repeats the package caches' total ("Package caches 3.6 GB", web/src/lib/disk.ts:19), so the old card test's bare `getByText("3.6 GB")` would find two elements. In `web/src/components/storage-cards.test.tsx:15`, replace `expect(screen.getByText("3.6 GB")).toBeInTheDocument()` with:

```tsx
    expect(within(screen.getByText("41000 files").closest("tr") as HTMLElement).getByText("3.6 GB")).toBeInTheDocument()
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/pages/storage.test.tsx`
Expected: FAIL: no header description, no "Disk" or "Prune" region, and the old button labels.

- [ ] **Step 3: Write the implementation**

Create `web/src/lib/use-prune.ts`:

```ts
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { PruneScope } from "@/api/types"
import type { Confirm } from "@/components/confirm-dialog"
import type { PruneAsk } from "./storage"

// A prune shows in /status (maintenance) as well as /storage, so both are
// read again once it is sent.
export function usePrune() {
  const queryClient = useQueryClient()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const prune = useMutation({
    mutationFn: (scope: PruneScope) => api.prune(scope),
    onSuccess: () => {
      toast.success("Prune started")
    },
    onSettled: () =>
      Promise.all([queryClient.invalidateQueries({ queryKey: keys.storage }), queryClient.invalidateQueries({ queryKey: keys.status })]),
  })
  return {
    confirm,
    close: () => setConfirm(null),
    ask: (scope: PruneScope, c: PruneAsk) =>
      setConfirm({ title: c.title, body: c.body, action: c.action, destructive: c.destructive, run: () => prune.mutate(scope) }),
  }
}
```

Create `web/src/components/storage-disk.tsx`:

```tsx
import { Spinner } from "darkraise-ui/components/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import type { Status, Storage } from "@/api/types"
import { DiskBreakdown } from "@/components/disk-breakdown"
import { Section } from "@/components/page/section"
import { humanBytes } from "@/lib/format"
import { lastPruneText } from "@/lib/storage"

const NUM = "text-right font-mono"

function LastPrune({ storage, status }: { storage: Storage; status: Status }) {
  const p = storage.last_prune
  if (status.maintenance.running || (p && !p.finished_at)) return <Spinner label="Pruning" />
  if (!p?.finished_at) return <p className="text-sm text-muted-foreground">No prune since the daemon started</p>
  return (
    <div className="flex flex-col gap-1 text-sm">
      <p>{lastPruneText(p)}</p>
      {(p.steps ?? []).length > 0 && (
        <ul className="flex flex-col gap-0.5">
          {(p.steps ?? []).map((st) => (
            <li key={st.name} className={st.error ? "text-destructive" : "text-muted-foreground"}>
              {st.error ? `${st.name}: ${st.error}` : `${st.name}: ${humanBytes(st.freed)} freed`}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}

export function DiskSection({ storage, status, highWater, className = "" }: { storage: Storage; status: Status; highWater: number; className?: string }) {
  const d = storage.docker
  return (
    <Section
      title="Disk"
      className={className}
      aside={status.disk_root ? <span className="font-mono text-sm text-muted-foreground">{status.disk_root}</span> : undefined}
    >
      <div className="flex flex-col gap-4">
        <DiskBreakdown status={status} storage={storage} highWater={highWater} />
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Type</TableHead>
                <TableHead className="text-right">Count</TableHead>
                <TableHead className="text-right">Active</TableHead>
                <TableHead className="text-right">Size</TableHead>
                <TableHead className="text-right">Reclaimable</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(d.rows ?? []).flatMap((r) => [
                <TableRow key={r.type}>
                  <TableCell>{r.type}</TableCell>
                  <TableCell className={NUM}>{r.count}</TableCell>
                  <TableCell className={NUM}>{r.active}</TableCell>
                  <TableCell className={NUM}>{humanBytes(r.bytes)}</TableCell>
                  <TableCell className={NUM}>{humanBytes(r.reclaimable)}</TableCell>
                </TableRow>,
                ...(r.type === "Build Cache" ? (d.build_cache_types ?? []) : []).map((t) => (
                  <TableRow key={`build-cache-${t.type}`} className="text-muted-foreground">
                    <TableCell className="pl-6">{t.type}</TableCell>
                    <TableCell className={NUM}>{t.count}</TableCell>
                    <TableCell />
                    <TableCell className={NUM}>{humanBytes(t.bytes)}</TableCell>
                    <TableCell className={NUM}>{humanBytes(t.reclaimable)}</TableCell>
                  </TableRow>
                )),
              ])}
            </TableBody>
          </Table>
        </div>
        <LastPrune storage={storage} status={status} />
      </div>
    </Section>
  )
}
```

Create `web/src/components/prune-section.tsx`:

```tsx
import { Button } from "darkraise-ui/components/button"
import type { Config, Status, Storage } from "@/api/types"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { Section } from "@/components/page/section"
import { RefusedHint } from "@/components/refused-hint"
import { pruneRows } from "@/lib/storage"
import { usePrune } from "@/lib/use-prune"

export function PruneSection({
  storage,
  status,
  config,
  disabled,
  className = "",
}: {
  storage: Storage
  status: Status | undefined
  config: Config | undefined
  disabled: boolean
  className?: string
}) {
  const prune = usePrune()
  return (
    <Section title="Prune" className={className}>
      <div className="flex flex-col divide-y divide-border">
        {pruneRows(storage, config).map((row) => (
          <div key={row.scope} className="flex flex-col gap-2 py-3 first:pt-0 last:pb-0">
            <div className="flex flex-wrap items-center justify-between gap-3">
              <span className="text-sm">{row.text}</span>
              <Button size="sm" variant={row.destructive ? "destructive" : "outline"} disabled={disabled} onClick={() => prune.ask(row.scope, row)}>
                {row.action}
              </Button>
            </div>
            {row.scope === "unused-volumes" && <RefusedHint what="Unused volumes" status={status} />}
          </div>
        ))}
      </div>
      <ConfirmDialog confirm={prune.confirm} onClose={prune.close} />
    </Section>
  )
}
```

Replace `web/src/pages/storage.tsx` with (the package caches and operations keep their current cards until Task 17):

```tsx
import { Button } from "darkraise-ui/components/button"
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { useConfig, useStatus, useStorage } from "@/api/hooks"
import { ConfirmDialog } from "@/components/confirm-dialog"
import { ErrorLine } from "@/components/page/error-line"
import { PruneSection } from "@/components/prune-section"
import { OperationsCard, PackageCachesCard } from "@/components/storage-cards"
import { DiskSection } from "@/components/storage-disk"
import { standardPrune, storageSummary } from "@/lib/storage"
import { useOperationToasts } from "@/lib/use-operation-toasts"
import { usePrune } from "@/lib/use-prune"
import { errorText } from "@/query"

// Below 1280px the sections stack in reading order. At 1280px and wider
// they form a 3:2 grid: Disk over Package caches, Prune over Recent
// operations.
const PLACE = {
  disk: "xl:col-start-1 xl:row-start-1",
  prune: "xl:col-start-2 xl:row-start-1",
  caches: "xl:col-start-1 xl:row-start-2",
  operations: "xl:col-start-2 xl:row-start-2",
}

export function StoragePage() {
  const status = useStatus()
  const config = useConfig()
  const storage = useStorage(status.data)
  useOperationToasts(storage.data)
  const prune = usePrune()
  const st = status.data
  const data = storage.data
  const offline = status.isError
  const highWater = config.data?.disk_high_water ?? 80
  const disabled = offline || (st?.maintenance.running ?? false)
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Storage"
        description={st ? storageSummary(st, highWater) : undefined}
        actions={
          <Button disabled={disabled} onClick={() => prune.ask("standard", standardPrune(config.data))}>
            Prune
          </Button>
        }
      />
      {storage.isError && <ErrorLine onRetry={() => void storage.refetch()}>{errorText(storage.error)}</ErrorLine>}
      {data && st ? (
        <div className="grid items-start gap-4 xl:grid-cols-[minmax(0,3fr)_minmax(0,2fr)]">
          <DiskSection storage={data} status={st} highWater={highWater} className={PLACE.disk} />
          <PruneSection storage={data} status={st} config={config.data} disabled={disabled} className={PLACE.prune} />
          <div className={PLACE.caches}>
            <PackageCachesCard storage={data} status={st} offline={offline} />
          </div>
          <div className={PLACE.operations}>
            <OperationsCard storage={data} />
          </div>
        </div>
      ) : (
        !storage.isError && <Spinner label="Loading" />
      )}
      <ConfirmDialog confirm={prune.confirm} onClose={prune.close} />
    </div>
  )
}
```

In `web/src/anti-slop.test.ts`, add `"src/components/prune-section.tsx"` and `"src/components/storage-disk.tsx"` to the components group, `"src/lib/use-prune.ts"` to the lib group, and `"src/pages/storage.tsx"` to the pages group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/pages/storage.test.tsx src/components/storage-cards.test.tsx src/anti-slop.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): rebuild the disk and prune sections"
```

---

### Task 17: Rebuild the package caches and operations

**Files:**
- Create: `web/src/components/storage-tables.tsx`
- Delete: `web/src/components/storage-cards.tsx`, `web/src/components/storage-cards.test.tsx`
- Modify: `web/src/pages/storage.tsx` (use the new sections), `web/src/lib/use-operation-toasts.ts` (the separator)
- Test: `web/src/pages/storage.test.tsx`, `web/src/a11y.test.tsx`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `SizeBar`, `RefusedHint` (Task 12); `Section`, `StateText`, `ErrorLine` (Contracts, From plan 2); the page layout (Task 16).
- Produces: `PackageCachesSection`, `OperationsSection` (Contracts, Storage).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

Add `import type { Storage } from "@/api/types"` to `web/src/pages/storage.test.tsx` and append after the existing `describe`:

```tsx
describe("Package caches", () => {
  it("lists the present caches and names the absent ones", async () => {
    mockApi(routes())
    renderApp("/storage")
    const caches = within(await screen.findByRole("region", { name: "Package caches" }))
    const row = within(caches.getByText("NuGet").closest("tr") as HTMLElement)
    expect(row.getByText(".nuget/packages")).toHaveClass("font-mono")
    expect(row.getByText("3.6 GB")).toHaveClass("font-mono")
    expect(row.getByText("41000")).toHaveClass("font-mono")
    expect(row.getByText("1h ago")).toBeInTheDocument()
    expect(caches.getByText("Not present: Cargo")).toBeInTheDocument()
    expect(caches.queryByRole("button", { name: "Clear Cargo" })).toBeNull()
    expect(caches.getByText("Measured 14:03")).toBeInTheDocument()
    expect(caches.getByText("Clear: refused while 1 job runs")).toBeInTheDocument()
  })

  it("says when a cache was never written", async () => {
    const caches = (fixtures.storage.package_caches ?? []).map((c) => ({ ...c, last_written: undefined }))
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, package_caches: caches } }))
    renderApp("/storage")
    expect(await screen.findByText("Never written")).toBeInTheDocument()
  })

  it("clears a cache only once confirmed", async () => {
    const { calls } = mockApi(routes({ "POST /api/caches/nuget/clear": () => noContent() }))
    const { user } = renderApp("/storage")
    await user.click(await screen.findByRole("button", { name: "Clear NuGet" }))
    const ask = within(await screen.findByRole("alertdialog"))
    expect(ask.getByText("Clear NuGet?")).toBeInTheDocument()
    expect(ask.getByText("Frees 3.6 GB. Jobs download what they need again.")).toBeInTheDocument()
    await user.click(ask.getByRole("button", { name: "Cancel" }))
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull())
    expect(calls.some((c) => c.method === "POST")).toBe(false)

    await user.click(screen.getByRole("button", { name: "Clear NuGet" }))
    await user.click(within(await screen.findByRole("alertdialog")).getByRole("button", { name: "Clear" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/caches/nuget/clear")).toBe(true))
    expect((await screen.findAllByText("Queued: clear NuGet")).length).toBeGreaterThan(0)
  })

  it("starts a measurement without asking", async () => {
    const { calls } = mockApi(routes({ "POST /api/storage/refresh": () => noContent() }))
    const { user } = renderApp("/storage")
    await user.click(await screen.findByRole("button", { name: "Refresh" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/storage/refresh")).toBe(true))
    expect((await screen.findAllByText("Measurement started")).length).toBeGreaterThan(0)
  })

  it("shows a running measurement and a failed one", async () => {
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, measuring: true, measure_error: "du failed" } }))
    renderApp("/storage")
    const caches = within(await screen.findByRole("region", { name: "Package caches" }))
    expect(caches.getByText("Measuring")).toBeInTheDocument()
    expect(caches.getByRole("button", { name: "Refresh" })).toBeDisabled()
    expect(caches.getByRole("alert")).toHaveTextContent("du failed")
  })
})

describe("Recent operations", () => {
  it("lists finished operations with their outcome as a word", async () => {
    mockApi(routes())
    renderApp("/storage")
    const row = within((await screen.findByText("cleared NuGet (3.6 GB freed)")).closest("tr") as HTMLElement)
    expect(row.getByText("13:06")).toHaveClass("font-mono")
    expect(row.getByText("clear")).toBeInTheDocument()
    expect(row.getByText("nuget")).toBeInTheDocument()
    expect(row.getByText("OK")).toHaveClass("text-success")
    expect(row.getByText("cleared NuGet (3.6 GB freed)")).toHaveClass("text-muted-foreground")
  })

  it("says when nothing has run", async () => {
    mockApi(routes({ "GET /api/storage": { ...fixtures.storage, operations: { current: null, queued: 0, recent: null } } }))
    renderApp("/storage")
    expect(await screen.findByText("No operations since the daemon started.")).toBeInTheDocument()
  })

  it("toasts operations that finish while the page is open, failures first", { timeout: 10_000 }, async () => {
    const later: Storage = {
      ...fixtures.storage,
      operations: {
        current: null,
        queued: 0,
        recent: [
          {
            id: "op5",
            kind: "remove",
            target: "go 1.23.1",
            started_at: "2026-10-03T14:06:00Z",
            finished_at: "2026-10-03T14:06:05Z",
            outcome: "failed",
            message: "permission denied",
          },
          {
            id: "op4",
            kind: "install",
            target: "node 24",
            started_at: "2026-10-03T14:04:00Z",
            finished_at: "2026-10-03T14:06:00Z",
            outcome: "ok",
            message: "installed node 24.9.0",
          },
          ...(fixtures.storage.operations.recent ?? []),
        ],
      },
    }
    let reads = 0
    mockApi(routes({ "GET /api/storage": () => (++reads === 1 ? fixtures.storage : later) }))
    renderApp("/storage")
    await screen.findByText("cleared NuGet (3.6 GB freed)")
    const toasts = await screen.findAllByText("Remove go 1.23.1: permission denied; install node 24: installed node 24.9.0", undefined, { timeout: 4000 })
    expect(toasts.length).toBeGreaterThan(0)
  })
})
```

Append to `web/src/a11y.test.tsx`:

```tsx
describe("Storage accessibility", () => {
  it(
    "has no axe violations",
    async () => {
      mockApi(authedRoutes())
      renderApp("/storage")
      await screen.findByText("cleared NuGet (3.6 GB freed)")
      await screen.findByText("Last prune: manual build-cache-all at 13:16, ok")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/pages/storage.test.tsx src/a11y.test.tsx`
Expected: FAIL: the package caches and operations are still cards with badges, "not present" rows and the "·" toast.

- [ ] **Step 3: Write the implementation**

Create `web/src/components/storage-tables.tsx`:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "darkraise-ui/components/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "darkraise-ui/components/tooltip"
import { Eraser } from "lucide-react"
import { useState } from "react"
import { api } from "@/api/client"
import { keys } from "@/api/hooks"
import type { PackageCache, Status, Storage } from "@/api/types"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { ErrorLine } from "@/components/page/error-line"
import { Section } from "@/components/page/section"
import { StateText } from "@/components/page/state-text"
import { RefusedHint } from "@/components/refused-hint"
import { SizeBar } from "@/components/size-bar"
import { ago, hhmm, humanBytes } from "@/lib/format"
import { useNow } from "@/lib/use-now"

export function PackageCachesSection({
  storage,
  status,
  offline,
  className = "",
}: {
  storage: Storage
  status: Status | undefined
  offline: boolean
  className?: string
}) {
  const queryClient = useQueryClient()
  const now = useNow()
  const [confirm, setConfirm] = useState<Confirm | null>(null)
  const settled = () => queryClient.invalidateQueries({ queryKey: keys.storage })
  const clear = useMutation({
    mutationFn: (c: PackageCache) => api.clearCache(c.name),
    onSuccess: (_data, c) => {
      toast.success(`Queued: clear ${c.label}`)
    },
    onSettled: settled,
  })
  const refresh = useMutation({
    mutationFn: () => api.refreshStorage(),
    onSuccess: () => {
      toast.success("Measurement started")
    },
    onSettled: settled,
  })
  const all = storage.package_caches ?? []
  const present = all.filter((c) => c.present)
  const absent = all.filter((c) => !c.present)
  const longest = Math.max(1, ...present.map((c) => c.bytes))
  const aside = (
    <span className="ml-auto flex items-center gap-2 text-sm text-muted-foreground">
      {storage.measuring ? (
        <Spinner label="Measuring" />
      ) : (
        <span>{storage.measured_at ? `Measured ${hhmm(new Date(storage.measured_at))}` : "Not measured yet"}</span>
      )}
      <Button size="sm" variant="outline" disabled={offline || storage.measuring} onClick={() => refresh.mutate()}>
        Refresh
      </Button>
    </span>
  )

  return (
    <Section title="Package caches" aside={aside} className={className}>
      <div className="flex flex-col gap-3">
        {present.length > 0 && (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Cache</TableHead>
                  <TableHead>Paths</TableHead>
                  <TableHead className="text-right">Size</TableHead>
                  <TableHead className="text-right">Files</TableHead>
                  <TableHead>Last written</TableHead>
                  <TableHead>
                    <span className="sr-only">Actions</span>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {present.map((c) => (
                  <TableRow key={c.name}>
                    <TableCell>{c.label}</TableCell>
                    <TableCell className="whitespace-normal">
                      <span className="font-mono text-xs break-all text-muted-foreground">{(c.paths ?? []).join(" ")}</span>
                    </TableCell>
                    <TableCell>
                      <SizeBar bytes={c.bytes} longest={longest} />
                    </TableCell>
                    <TableCell className="text-right font-mono">{c.files}</TableCell>
                    <TableCell>{c.last_written ? ago(now - Date.parse(c.last_written)) : "Never written"}</TableCell>
                    <TableCell className="text-right">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <Button
                            size="icon"
                            variant="ghost"
                            aria-label={`Clear ${c.label}`}
                            disabled={offline}
                            onClick={() =>
                              setConfirm({
                                title: `Clear ${c.label}?`,
                                body: `Frees ${humanBytes(c.bytes)}. Jobs download what they need again.`,
                                action: "Clear",
                                destructive: true,
                                run: () => clear.mutate(c),
                              })
                            }
                          >
                            <Eraser size={15} aria-hidden="true" />
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>Clear</TooltipContent>
                      </Tooltip>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
        {absent.length > 0 && <p className="text-sm text-muted-foreground">{`Not present: ${absent.map((c) => c.label).join(", ")}`}</p>}
        <RefusedHint what="Clear" status={status} />
        {storage.measure_error && <ErrorLine>{storage.measure_error}</ErrorLine>}
      </div>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </Section>
  )
}

export function OperationsSection({ storage, className = "" }: { storage: Storage; className?: string }) {
  const recent = storage.operations.recent ?? []
  return (
    <Section title="Recent operations" className={className}>
      {recent.length === 0 ? (
        <p className="text-sm text-muted-foreground">No operations since the daemon started.</p>
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Time</TableHead>
                <TableHead>Kind</TableHead>
                <TableHead>Target</TableHead>
                <TableHead>Outcome</TableHead>
                <TableHead>Message</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {recent.map((o) => (
                <TableRow key={o.id}>
                  <TableCell className="font-mono">{hhmm(new Date(o.finished_at ?? o.started_at))}</TableCell>
                  <TableCell>{o.kind}</TableCell>
                  <TableCell>{o.target}</TableCell>
                  <TableCell>{o.outcome && <StateText state={o.outcome} label={o.outcome === "ok" ? "OK" : undefined} />}</TableCell>
                  <TableCell className="whitespace-normal text-muted-foreground">{o.message}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </Section>
  )
}
```

In `web/src/pages/storage.tsx`, replace the `storage-cards` import with `import { OperationsSection, PackageCachesSection } from "@/components/storage-tables"`, and replace the two wrapper `div`s with:

```tsx
          <PackageCachesSection storage={data} status={st} offline={offline} className={PLACE.caches} />
          <OperationsSection storage={data} className={PLACE.operations} />
```

In `web/src/lib/use-operation-toasts.ts`, replace the line `const text = [...bad, ...good].join(" · ")` with:

```ts
    const joined = [...bad, ...good].join("; ")
    const text = joined.charAt(0).toUpperCase() + joined.slice(1)
```

Delete `web/src/components/storage-cards.tsx` and `web/src/components/storage-cards.test.tsx` (their cases now live in `pages/storage.test.tsx`; confirm `grep -rn "storage-cards" web/src` prints nothing).

In `web/src/anti-slop.test.ts`, add `"src/components/storage-tables.tsx"` to the components group and `"src/lib/use-operation-toasts.ts"` to the lib group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/pages/storage.test.tsx src/pages/toolchains.test.tsx src/a11y.test.tsx src/anti-slop.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add -A web/src
git commit -m "feat(web): rebuild the package caches and operations"
```

---
### Task 18: Rebuild the Settings config sections

**Files:**
- Modify: `web/src/components/settings-form.tsx` (replace)
- Modify: `web/src/lib/settings-form.ts` (five toast strings)
- Test: `web/src/pages/settings.test.tsx`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `Field`, `FieldGroup`, `RejectedAlert` (Contracts, From plan 2).
- Produces: `SettingsSections` built from `FieldGroup`s with ids `general`, `timing`, `disk`, `runner-defaults` (Contracts, Settings). The setup wizard's settings step renders it unchanged.

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/settings.test.tsx`, change the first import to `import { screen, waitFor, within } from "@testing-library/react"` and make these exact replacements (every occurrence):
- `"start runners only for queued jobs, up to the global max"` → `"Start runners only for queued jobs, up to the global max"`
- `"limits apply to newly started runners"` → `"Limits apply to newly started runners."`
- `"keep warm runners per repo, up to each repo's max"` → `"Keep warm runners per repo, up to each repo's max"`
- `"✖ poll_interval must be at least 5s"` → `"poll_interval must be at least 5s"`
- `"✖ global_max must be >= 1"` → `"global_max must be >= 1"`
- `"fix the highlighted settings first"` → `"Fix the highlighted settings first"`
- `"✖ build_cache_keep must look like 20GB"` → `"build_cache_keep must look like 20GB"`
- `"✖ runner_limits.cpu_quota must be a positive percentage such as 200%"` → `"runner_limits.cpu_quota must be a positive percentage such as 200%"`
- `"settings not saved"` → `"Settings not saved"`
- `"settings not saved: ghr is shutting down"` → `"Settings not saved: ghr is shutting down"`
- `"daemon did not apply global_max; is it older than this ghr?"` → `"Daemon did not apply global_max; is it older than this ghr?"`
- `/daemon did not apply/` → `/Daemon did not apply/`
- `/daemon did not apply poll_interval/` → `/Daemon did not apply poll_interval/`
- `"saved, but re-reading the config failed: ghr is restarting"` → `"Saved, but re-reading the config failed: ghr is restarting"`

Append inside `describe("Settings page", …)`:

```tsx
  it("groups the fields into sections and marks a changed one", async () => {
    mockApi(routes())
    const { user } = renderApp("/settings")
    const poll = await screen.findByLabelText("Poll interval")
    for (const name of ["General", "Timing", "Disk and retention", "Runner defaults"]) {
      expect(screen.getByRole("region", { name })).toBeInTheDocument()
    }
    expect(screen.getByText("How often GitHub is checked (at least 5s)")).toHaveClass("text-muted-foreground")
    expect(screen.getByText("Build cache kept when pruning. For example 20GB")).toBeInTheDocument()
    expect(screen.getByText("Change it in config.yaml and restart the daemon")).toBeInTheDocument()
    expect(screen.getByText("darkraise")).toHaveClass("font-mono")
    await user.clear(poll)
    await user.type(poll, "15s")
    expect(within(screen.getByRole("region", { name: "Timing" })).getByText("Changed")).toHaveClass("text-warning")
    expect(screen.queryByText("●")).toBeNull()
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/pages/settings.test.tsx`
Expected: FAIL: the sections are still cards, the help text is lower case, errors carry "✖", and the toasts are lower case.

- [ ] **Step 3: Write the implementation**

Replace `web/src/components/settings-form.tsx` with:

```tsx
import { Input } from "darkraise-ui/components/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "darkraise-ui/components/select"
import { Field, FieldGroup } from "@/components/page/field"
import { RejectedAlert } from "@/components/save-bar"
import { TagField } from "@/components/tag-field"
import type { SettingsForm } from "@/lib/settings-form"

const MODES: Record<string, string> = {
  queue: "Start runners only for queued jobs, up to the global max",
  all: "Keep warm runners per repo, up to each repo's max",
}

export function SettingsSections({ form }: { form: SettingsForm }) {
  const { values, changed, errors, offline, set } = form
  const text = (key: string) => String(values[key] ?? "")
  const num = (key: string) => (Number.isNaN(values[key]) ? "" : String(values[key]))
  const toNum = (s: string) => (s === "" ? Number.NaN : Number(s))
  const is = (key: string) => changed.includes(key)

  function textField(key: string, label: string, help: string) {
    return (
      <Field label={label} htmlFor={key} help={help} changed={is(key)} error={errors[key]}>
        <Input id={key} className="w-40" value={text(key)} disabled={offline} onChange={(e) => set(key)(e.target.value)} />
      </Field>
    )
  }

  return (
    <div className="flex flex-col gap-6">
      {form.rejection && <RejectedAlert message={form.rejection} />}
      <FieldGroup id="general" title="General">
        <Field label="Mode" help={MODES[text("mode")]} changed={is("mode")}>
          <Select value={text("mode")} onValueChange={set("mode")} disabled={offline}>
            <SelectTrigger aria-label="Mode" className="w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {Object.keys(MODES).map((m) => (
                <SelectItem key={m} value={m}>
                  {m}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field label="Global max" htmlFor="global_max" help="Applies in queue mode" changed={is("global_max")} error={errors.global_max}>
          <Input
            id="global_max"
            type="number"
            min={1}
            max={99}
            step={1}
            className="w-24"
            value={num("global_max")}
            disabled={offline}
            onChange={(e) => set("global_max")(toNum(e.target.value))}
          />
        </Field>
        <Field label="Owner" help="Change it in config.yaml and restart the daemon">
          <span className="font-mono text-sm">{form.config?.owner}</span>
        </Field>
      </FieldGroup>
      <FieldGroup id="timing" title="Timing">
        {textField("poll_interval", "Poll interval", "How often GitHub is checked (at least 5s)")}
        {textField("start_timeout", "Start timeout", "A runner not online by then is replaced")}
        {textField("idle_timeout", "Idle timeout", "Idle runners beyond warm stop after this")}
      </FieldGroup>
      <FieldGroup id="disk" title="Disk and retention">
        <Field label="Disk high-water" htmlFor="disk_high_water" help="Disk use that triggers pruning" changed={is("disk_high_water")} error={errors.disk_high_water}>
          <Input
            id="disk_high_water"
            type="number"
            min={1}
            max={100}
            step={5}
            suffix="%"
            className="w-24"
            value={num("disk_high_water")}
            disabled={offline}
            onChange={(e) => set("disk_high_water")(toNum(e.target.value))}
          />
        </Field>
        {textField("build_cache_keep", "Build cache keep", "Build cache kept when pruning. For example 20GB")}
        {textField("history_retention", "History retention", "History and logs older than this are removed (at least 1d)")}
      </FieldGroup>
      <FieldGroup id="runner-defaults" title="Runner defaults" note="Limits apply to newly started runners.">
        <Field label="Global labels" help="Added to every runner" changed={is("labels")}>
          <TagField label="Global labels" value={values.labels as string[]} onChange={set("labels")} disabled={offline} />
        </Field>
        {textField("memory_max", "Memory max", "Per runner. For example 6G, 50% or infinity")}
        {textField("cpu_quota", "CPU quota", "Per runner. For example 200%")}
      </FieldGroup>
    </div>
  )
}
```

In `web/src/lib/settings-form.ts`, change only these strings:
- `"fix the highlighted settings first"` → `"Fix the highlighted settings first"`
- `"settings not saved"` → `"Settings not saved"`
- `` `settings not saved: ${errorText(err)}` `` → `` `Settings not saved: ${errorText(err)}` ``
- `` `daemon did not apply ${patchKey(missed)}; is it older than this ghr?` `` → `` `Daemon did not apply ${patchKey(missed)}; is it older than this ghr?` ``
- `` `saved, but re-reading the config failed: ${errorText(err)}` `` → `` `Saved, but re-reading the config failed: ${errorText(err)}` ``

In `web/src/anti-slop.test.ts`, add `"src/components/settings-form.tsx"` to the components group and `"src/lib/settings-form.ts"` to the lib group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/pages/settings.test.tsx src/pages/setup.test.tsx src/anti-slop.test.ts`
Expected: PASS; the setup wizard's settings step still saves.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add web/src
git commit -m "feat(web): rebuild the settings config sections"
```

---

### Task 19: Rebuild the GitHub token section

**Files:**
- Create: `web/src/lib/token.ts`, `web/src/components/token-section.tsx`
- Delete: `web/src/components/token-card.tsx`
- Modify: `web/src/pages/settings.tsx` (import and element only)
- Test: `web/src/lib/token.test.ts` (create); rename `web/src/components/token-card.test.tsx` to `web/src/components/token-section.test.tsx` and update it
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `FieldGroup`, `Field`, `StateText`, `ErrorLine` (Contracts, From plan 2).
- Produces: `tokenWord`, `expiryText`, `rateText`, `TokenSection` (Contracts, Settings).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

Create `web/src/lib/token.test.ts`:

```ts
import { describe, expect, it } from "vitest"
import type { TokenStatus } from "@/api/types"
import { fixtures } from "@/test/fixtures"
import { expiryText, rateText, tokenWord } from "./token"

const DAY = 86_400_000
const now = Date.parse("2026-10-03T14:05:00Z")
const at = (ms: number) => new Date(now + ms).toISOString()
const token = (over: Partial<TokenStatus>): TokenStatus => ({ state: "ok", ...over })

describe("tokenWord", () => {
  it.each([
    [token({ expires_at: at(60 * DAY) }), "valid"],
    [token({}), "valid"],
    [token({ expires_at: at(10 * DAY) }), "expires soon"],
    [token({ state: "unverified" }), "unverified"],
    [token({ state: "rejected", expires_at: at(DAY) }), "rejected"],
  ])("reads %j as %s", (t, word) => {
    expect(tokenWord(t, now)).toBe(word)
  })
})

describe("expiryText", () => {
  it.each([
    [token({ expires_at: at(60 * DAY + 3_600_000) }), "Expires Dec 2, in 60 days"],
    [token({ expires_at: at(DAY + 3_600_000) }), "Expires Oct 4, in 1 day"],
    [token({ expires_at: at(2 * 3_600_000) }), "Expires Oct 3, in less than a day"],
    [token({ expires_at: at(-DAY) }), "Expired Oct 2"],
    [token({}), "Expiry unknown"],
  ])("reads %j as %s", (t, text) => {
    expect(expiryText(t, now)).toBe(text)
  })
})

describe("rateText", () => {
  it("shows what is left of the limit and when it resets", () => {
    expect(rateText(fixtures.token)).toBe("4,980 of 5,000, resets 14:45")
    expect(rateText({ ...fixtures.token, rate_limit: undefined })).toBe("4,980, resets 14:45")
    expect(rateText({ ...fixtures.token, rate_reset: undefined })).toBe("4,980 of 5,000")
  })
  it("is blank until known", () => {
    expect(rateText(undefined)).toBe("")
    expect(rateText(token({}))).toBe("")
  })
})
```

Run `git mv web/src/components/token-card.test.tsx web/src/components/token-section.test.tsx`, then in it:
- change the import `import { dateTime } from "@/lib/format"` to nothing (delete the line);
- rename `describe("GitHub token card", …)` to `describe("GitHub token section", …)`;
- replace the body of "shows the token's state, expiry and rate limit" with:

```tsx
    mockApi(authedRoutes({ "GET /api/token": token() }))
    renderApp("/settings")
    expect(await screen.findByText("Expires Dec 2, in 60 days")).toBeInTheDocument()
    const section = within(screen.getByRole("region", { name: "GitHub token" }))
    expect(section.getByText("Valid")).toHaveClass("text-success")
    expect(section.getByText("1m ago")).toBeInTheDocument()
    expect(section.getByText("4,980 of 5,000, resets 14:45")).toHaveClass("font-mono")
```

- in "warns when the token expires within 14 days": `"expires soon"` → `"Expires soon"`;
- in "shows a rejected token's reason": `"rejected"` → `"Rejected"` and `"expiry unknown"` → `"Expiry unknown"`;
- in "offers a retry when the token cannot be read": `"✖ daemon busy"` → `"daemon busy"`, `{ name: "Retry" }` → `{ name: "Try again" }`, `"· checked 1m ago"` → `"1m ago"`;
- in "replaces the token only after confirmation, sending it raw": `"paste the new token first"` → `"Paste the new token first"`, and replace `expect(within(ask).getByText("Replace the GitHub token? ghr uses the new one at once.")).toBeInTheDocument()` with `expect(within(ask).getByText("Replace the GitHub token?")).toBeInTheDocument()` followed by `expect(within(ask).getByText("ghr uses the new one at once.")).toBeInTheDocument()`;
- in "shows a rejected token inside the dialog": `"✖ new token rejected: Bad credentials"` → `"new token rejected: Bad credentials"`.

Append inside the `describe`:

```tsx
  it("leaves the rate limit blank until it is known, and says when nothing is read yet", async () => {
    mockApi(authedRoutes({ "GET /api/token": () => new Promise(() => {}) }))
    renderApp("/settings")
    const section = within(await screen.findByRole("region", { name: "GitHub token" }))
    expect(section.getByText("Not read yet")).toBeInTheDocument()
    expect(section.queryByText("–")).toBeNull()
  })
```

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/lib/token.test.ts src/components/token-section.test.tsx`
Expected: FAIL: `Cannot find module './token'`, and the card still shows a badge, "–" and the old copy.

- [ ] **Step 3: Write the implementation**

Create `web/src/lib/token.ts`:

```ts
import type { TokenStatus } from "@/api/types"
import { DAY_MS } from "./duration"
import { hhmm, monthDay, plural } from "./format"

const SOON = 14 * DAY_MS

export function tokenWord(t: TokenStatus, now: number): string {
  if (t.state !== "ok") return t.state
  return t.expires_at && Date.parse(t.expires_at) - now < SOON ? "expires soon" : "valid"
}

export function expiryText(t: TokenStatus, now: number): string {
  if (!t.expires_at) return "Expiry unknown"
  const at = Date.parse(t.expires_at)
  const day = monthDay(new Date(at))
  const left = at - now
  if (left <= 0) return `Expired ${day}`
  return left < DAY_MS ? `Expires ${day}, in less than a day` : `Expires ${day}, in ${plural(Math.floor(left / DAY_MS), "day")}`
}

const count = (n: number) => n.toLocaleString("en-US")

export function rateText(t: TokenStatus | undefined): string {
  if (t?.rate_remaining === undefined) return ""
  const limit = t.rate_limit !== undefined ? ` of ${count(t.rate_limit)}` : ""
  const reset = t.rate_reset ? `, resets ${hhmm(new Date(t.rate_reset))}` : ""
  return `${count(t.rate_remaining)}${limit}${reset}`
}
```

Create `web/src/components/token-section.tsx`:

```tsx
import { useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "darkraise-ui/components/dialog"
import { Input } from "darkraise-ui/components/input"
import { Label } from "darkraise-ui/components/label"
import { toast } from "darkraise-ui/components/sonner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useStatus, useToken } from "@/api/hooks"
import { ConfirmDialog, type Confirm } from "@/components/confirm-dialog"
import { ErrorLine } from "@/components/page/error-line"
import { Field, FieldGroup } from "@/components/page/field"
import { StateText } from "@/components/page/state-text"
import { ago } from "@/lib/format"
import { expiryText, rateText, tokenWord } from "@/lib/token"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

const NOTE =
  "Replacing the token takes effect at once, outside the save bar. It checks read access to the first repository only; registration permissions are checked when a runner next starts."

export function TokenSection() {
  const token = useToken()
  const status = useStatus()
  const now = useNow()
  const [open, setOpen] = useState(false)
  const t = token.data

  return (
    <FieldGroup id="token" title="GitHub token" note={NOTE}>
      {token.isError && (
        <div className="py-3">
          <ErrorLine onRetry={() => void token.refetch()}>{errorText(token.error)}</ErrorLine>
        </div>
      )}
      <Field label="Status">
        {t ? (
          <>
            <StateText state={tokenWord(t, now)} />
            <span className="text-sm">{expiryText(t, now)}</span>
          </>
        ) : (
          <span className="text-sm text-muted-foreground">Not read yet</span>
        )}
      </Field>
      <Field label="Checked">
        <span className="text-sm">{t?.checked_at ? ago(now - Date.parse(t.checked_at)) : ""}</span>
      </Field>
      <Field label="Rate limit">
        <span className="font-mono text-sm">{rateText(t)}</span>
      </Field>
      {t?.reason && t.state !== "ok" && <p className="py-3 text-sm text-destructive">{t.reason}</p>}
      <div className="py-3">
        <Button variant="outline" disabled={status.isError} onClick={() => setOpen(true)}>
          Replace token
        </Button>
      </div>
      <Dialog
        open={open}
        onOpenChange={(o) => {
          if (!o) setOpen(false)
        }}
      >
        {/* Mounted only while open: each opening starts clean, and a replace
            that ends after closing has nowhere to leave its error. */}
        <DialogContent>{open && <ReplaceTokenForm onClose={() => setOpen(false)} />}</DialogContent>
      </Dialog>
    </FieldGroup>
  )
}

function ReplaceTokenForm({ onClose }: { onClose: () => void }) {
  const queryClient = useQueryClient()
  const [text, setText] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const [confirm, setConfirm] = useState<Confirm | null>(null)

  async function send(value: string) {
    setBusy(true)
    try {
      await api.replaceToken(value)
      onClose()
      toast.success("GitHub token replaced")
      await queryClient.invalidateQueries({ queryKey: keys.token })
    } catch (err) {
      setError(errorText(err))
    } finally {
      setBusy(false)
    }
  }

  function ask() {
    if (text.trim() === "") return setError("Paste the new token first")
    setError("")
    const value = text
    setConfirm({ title: "Replace the GitHub token?", body: "ghr uses the new one at once.", action: "Replace", destructive: true, run: () => void send(value) })
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>Replace GitHub token</DialogTitle>
        <DialogDescription>
          Paste a fine-grained token with Administration: read/write and Actions: read on every configured repository.
        </DialogDescription>
      </DialogHeader>
      <DialogBody className="flex flex-col gap-2">
        <Label htmlFor="new-token">Token</Label>
        <Input id="new-token" type="password" autoComplete="off" value={text} onChange={(e) => setText(e.target.value)} />
        {error && <ErrorLine>{error}</ErrorLine>}
      </DialogBody>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          Cancel
        </Button>
        <Button loading={busy} onClick={ask}>
          Replace
        </Button>
      </DialogFooter>
      <ConfirmDialog confirm={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}
```

In `web/src/pages/settings.tsx`, change `import { TokenCard } from "@/components/token-card"` to `import { TokenSection } from "@/components/token-section"` and `<TokenCard />` to `<TokenSection />`. Delete `web/src/components/token-card.tsx`.

In `web/src/anti-slop.test.ts`, add `"src/components/token-section.tsx"` to the components group and `"src/lib/token.ts"` to the lib group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/lib/token.test.ts src/components/token-section.test.tsx src/pages/settings.test.tsx src/anti-slop.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add -A web/src
git commit -m "feat(web): rebuild the GitHub token section"
```

---

### Task 20: Rebuild the runner and config section

**Files:**
- Create: `web/src/components/runner-section.tsx`
- Delete: `web/src/components/maintenance-card.tsx`
- Modify: `web/src/pages/settings.tsx` (import and element only)
- Test: create `web/src/components/runner-section.test.tsx`; delete the `describe("Maintenance card", …)` block (and the imports only it used) from `web/src/components/settings-cards.test.tsx`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `FieldGroup`, `Field`, `StateText` (Contracts, From plan 2).
- Produces: `RunnerSection` (Contracts, Settings).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 1 - risk 1 = 3

- [ ] **Step 1: Write the failing tests**

Create `web/src/components/runner-section.test.tsx`:

```tsx
import { screen, waitFor, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import type { RunnerUpdate } from "@/api/types"
import { json, mockApi, noContent } from "@/test/api"
import { authedRoutes, fixtures } from "@/test/fixtures"
import { renderApp } from "@/test/render"

const DAY = 86_400_000
// The UI runs on the daemon's clock, the status fixture's now.
const DAEMON_NOW = Date.parse(fixtures.status.now)

function withUpdate(runner_update: RunnerUpdate, over: Record<string, unknown> = {}) {
  return authedRoutes({ "GET /api/token": fixtures.token, "GET /api/status": { ...fixtures.status, runner_update }, ...over })
}

// The shell's update card repeats Queue and Cancel update outside the page.
async function section() {
  return within(await screen.findByRole("region", { name: "Runner and config" }))
}

describe("Runner and config section", () => {
  it("cancels a queued runner update", async () => {
    const { calls } = mockApi(withUpdate(fixtures.status.runner_update, { "DELETE /api/runner-update": () => noContent() }))
    const { user } = renderApp("/settings")
    const s = await section()
    expect(await s.findByText("2.337.0 to 2.338.0")).toHaveClass("font-mono")
    expect(s.getByText("Queued")).toHaveClass("text-warning")
    expect(s.getByText("Runs when no job is running or queued")).toBeInTheDocument()
    await user.click(s.getByRole("button", { name: "Cancel update" }))
    await waitFor(() => expect(calls.some((c) => c.method === "DELETE" && c.path === "/api/runner-update")).toBe(true))
    expect((await screen.findAllByText("Runner update cancelled")).length).toBeGreaterThan(0)
  })

  it("queues a required update and shows its deadline", async () => {
    const deadline = new Date(DAEMON_NOW + 20 * DAY + 3_600_000).toISOString()
    const { calls } = mockApi(
      withUpdate({ installed: "2.337.0", latest: "2.338.0", deadline }, { "POST /api/runner-update": () => new Response(null, { status: 202 }) }),
    )
    const { user } = renderApp("/settings")
    const s = await section()
    expect(await s.findByText("Update required")).toHaveClass("text-warning")
    expect(s.getByText("Required by Oct 23, in 20 days")).toBeInTheDocument()
    await user.click(s.getByRole("button", { name: "Queue update" }))
    await waitFor(() => expect(calls.some((c) => c.method === "POST" && c.path === "/api/runner-update")).toBe(true))
    expect((await screen.findAllByText("Runner update queued")).length).toBeGreaterThan(0)
  })

  it("turns the required word bad within 7 days, and says when it is overdue", async () => {
    mockApi(withUpdate({ installed: "2.337.0", latest: "2.338.0", deadline: new Date(DAEMON_NOW - 2 * DAY).toISOString() }))
    renderApp("/settings")
    const s = await section()
    expect(await s.findByText("Update required")).toHaveClass("text-destructive")
    expect(s.getByText("Required by Oct 1, overdue")).toBeInTheDocument()
  })

  it("says when the runner is up to date", async () => {
    mockApi(withUpdate({ installed: "2.338.0", checked_at: new Date(DAEMON_NOW - 2 * 3_600_000).toISOString() }))
    renderApp("/settings")
    const s = await section()
    expect(await s.findByText("Up to date")).toHaveClass("text-success")
    expect(s.getByText("checked 2h ago")).toBeInTheDocument()
    expect(s.queryByRole("button", { name: "Queue update" })).toBeNull()
  })

  it.each([
    [{}, "Version unknown. No dist/current link was found."],
    [{ installed: "2.337.0", latest: "2.338.0", running: true }, "Updating"],
    [{ installed: "2.337.0" }, "Checking"],
  ])("reads %j as %s", async (u, text) => {
    mockApi(withUpdate(u))
    renderApp("/settings")
    expect(await (await section()).findByText(text)).toBeInTheDocument()
  })

  it("shows a failed check and a failed update", async () => {
    mockApi(
      withUpdate({
        installed: "2.337.0",
        latest: "2.338.0",
        deadline: new Date(DAEMON_NOW + 20 * DAY).toISOString(),
        check_error: "GitHub: 502",
        last_outcome: "failed",
        last_error: "disk full",
      }),
    )
    renderApp("/settings")
    const s = await section()
    expect(await s.findByText("Last update failed: disk full")).toHaveClass("text-destructive")
    expect(s.getByText("Last check failed: GitHub: 502")).toHaveClass("text-muted-foreground")
  })

  it.each([
    [[], "Config reloaded"],
    [["web settings changed; restart ghr to apply"], "Config reloaded with 1 warning. See Events on the Dashboard."],
    [["a", "b", "c"], "Config reloaded with 3 warnings. See Events on the Dashboard."],
  ])("reloads the config with %j and says so", async (warnings, text) => {
    mockApi(withUpdate({}, { "POST /api/reload": warnings }))
    const { user } = renderApp("/settings")
    await user.click(await (await section()).findByRole("button", { name: "Reload config.yaml" }))
    expect((await screen.findAllByText(text)).length).toBeGreaterThan(0)
  })

  it("shows a rejected reload", async () => {
    mockApi(withUpdate({}, { "POST /api/reload": () => json({ error: 'mode must be "queue" or "all"' }, 400) }))
    const { user } = renderApp("/settings")
    await user.click(await (await section()).findByRole("button", { name: "Reload config.yaml" }))
    expect((await screen.findAllByText('Reload rejected: mode must be "queue" or "all"')).length).toBeGreaterThan(0)
  })

  it("explains what reloading does", async () => {
    mockApi(withUpdate({}))
    renderApp("/settings")
    expect(await (await section()).findByText("Reads config.yaml again. Warnings appear under Events on the Dashboard.")).toBeInTheDocument()
  })
})
```

The status fixture's `now` is 2026-10-03T14:05Z, so a deadline 20 days and an hour later is Oct 23, and two days earlier is Oct 1 (`TZ=UTC`, web/vite.config.ts:23).

In `web/src/components/settings-cards.test.tsx`, delete the whole `describe("Maintenance card", …)` block, then delete the imports nothing else in the file uses (`RunnerUpdate`, `dateTime`, `DAY`, `DAEMON_NOW`, `withUpdate`; keep what the `Account card` block still uses).

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/components/runner-section.test.tsx`
Expected: FAIL: there is no "Runner and config" region.

- [ ] **Step 3: Write the implementation**

Create `web/src/components/runner-section.tsx`:

```tsx
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Button } from "darkraise-ui/components/button"
import { toast } from "darkraise-ui/components/sonner"
import { Spinner } from "darkraise-ui/components/spinner"
import { useState } from "react"
import { api } from "@/api/client"
import { keys, useStatus } from "@/api/hooks"
import type { RunnerUpdate } from "@/api/types"
import { Field, FieldGroup } from "@/components/page/field"
import { StateText } from "@/components/page/state-text"
import { DAY_MS } from "@/lib/duration"
import { ago, monthDay, plural } from "@/lib/format"
import { useNow } from "@/lib/use-now"
import { errorText } from "@/query"

function reloadText(warnings: number): string {
  if (warnings === 0) return "Config reloaded"
  return `Config reloaded with ${plural(warnings, "warning")}. See Events on the Dashboard.`
}

function deadlineText(deadline: string, now: number): string {
  const left = Date.parse(deadline) - now
  const when = left <= 0 ? "overdue" : left < DAY_MS ? "in less than a day" : `in ${plural(Math.floor(left / DAY_MS), "day")}`
  return `Required by ${monthDay(new Date(deadline))}, ${when}`
}

// The states are checked in the shell update card's order, so the two never
// disagree about what the runner is doing.
function VersionLine({ u, now }: { u: RunnerUpdate; now: number }) {
  if (!u.installed) return <span className="text-sm">Version unknown. No dist/current link was found.</span>
  const versions = <span className="font-mono text-sm">{`${u.installed} to ${u.latest ?? ""}`}</span>
  if (u.running) {
    return (
      <>
        {versions}
        <StateText state="running" label="Updating" />
      </>
    )
  }
  if (u.queued) {
    return (
      <>
        {versions}
        <StateText state="queued" />
      </>
    )
  }
  if (u.deadline) {
    const urgent = Date.parse(u.deadline) - now <= 7 * DAY_MS
    return (
      <>
        {versions}
        <StateText state={urgent ? "failed" : "waiting"} label="Update required" />
        <span className="text-sm text-muted-foreground">{deadlineText(u.deadline, now)}</span>
      </>
    )
  }
  const installed = <span className="font-mono text-sm">{u.installed}</span>
  if (u.checked_at) {
    return (
      <>
        {installed}
        <StateText state="up to date" />
        <span className="text-sm text-muted-foreground">{`checked ${ago(now - Date.parse(u.checked_at))}`}</span>
      </>
    )
  }
  if (!u.check_error) {
    return (
      <>
        {installed}
        <Spinner label="Checking" />
      </>
    )
  }
  return installed
}

export function RunnerSection() {
  const status = useStatus()
  const queryClient = useQueryClient()
  const now = useNow()
  const [reloading, setReloading] = useState(false)
  const offline = status.isError
  const u = status.data?.runner_update

  const refresh = () =>
    Promise.all([keys.status, ["events"], keys.config, keys.token].map((queryKey) => queryClient.invalidateQueries({ queryKey })))

  const update = useMutation({
    mutationFn: (cancel: boolean) => (cancel ? api.cancelRunnerUpdate() : api.queueRunnerUpdate()),
    onSuccess: (_data, cancel) => {
      toast.success(cancel ? "Runner update cancelled" : "Runner update queued")
    },
    onSettled: () => refresh(),
  })

  async function reload() {
    setReloading(true)
    try {
      toast.success(reloadText((await api.reload()).length))
    } catch (err) {
      toast.error(`Reload rejected: ${errorText(err)}`)
    } finally {
      setReloading(false)
      await refresh()
    }
  }

  const canQueue = u?.deadline !== undefined && !u.running && !u.queued
  return (
    <FieldGroup id="runner" title="Runner and config" note="These act at once, outside the save bar.">
      <Field label="Runner version" help={u?.queued ? "Runs when no job is running or queued" : undefined}>
        {u ? <VersionLine u={u} now={now} /> : <Spinner label="Loading" />}
      </Field>
      {u?.last_outcome === "failed" && u.last_error && <p className="py-3 text-sm text-destructive">{`Last update failed: ${u.last_error}`}</p>}
      {u?.check_error && <p className="py-3 text-sm text-muted-foreground">{`Last check failed: ${u.check_error}`}</p>}
      {(u?.queued || canQueue) && (
        <div className="py-3">
          {u?.queued ? (
            <Button variant="outline" disabled={offline || update.isPending} onClick={() => update.mutate(true)}>
              Cancel update
            </Button>
          ) : (
            <Button disabled={offline || update.isPending} onClick={() => update.mutate(false)}>
              Queue update
            </Button>
          )}
        </div>
      )}
      <Field label="Config" help="Reads config.yaml again. Warnings appear under Events on the Dashboard.">
        <Button variant="outline" disabled={offline} loading={reloading} onClick={() => void reload()}>
          Reload config.yaml
        </Button>
      </Field>
    </FieldGroup>
  )
}
```

In `web/src/pages/settings.tsx`, change `import { MaintenanceCard } from "@/components/maintenance-card"` to `import { RunnerSection } from "@/components/runner-section"` and `<MaintenanceCard />` to `<RunnerSection />`. Delete `web/src/components/maintenance-card.tsx`.

In `web/src/anti-slop.test.ts`, add `"src/components/runner-section.tsx"` to the components group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/components/runner-section.test.tsx src/components/settings-cards.test.tsx src/components/update-card.test.tsx src/pages/settings.test.tsx src/anti-slop.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add -A web/src
git commit -m "feat(web): rebuild the runner and config section"
```

---

### Task 21: Rebuild the account section and the Settings page

**Files:**
- Create: `web/src/components/account-section.tsx`
- Delete: `web/src/components/account-card.tsx`, `web/src/components/settings-cards.test.tsx`
- Modify: `web/src/pages/settings.tsx` (replace)
- Test: `web/src/pages/settings.test.tsx`, `web/src/a11y.test.tsx`
- Modify: `web/src/anti-slop.test.ts`

**Interfaces:**
- Consumes: `SettingsSections` (Task 18), `TokenSection` (Task 19), `RunnerSection` (Task 20); `FieldGroup`, `Field`, `ErrorLine`, `SectionNav`, `SaveBar`, `UnsavedGuard`, `setViewport`, `violations` (Contracts, From plan 2).
- Produces: `AccountSection` (Contracts, Settings).

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 1 - spec 0 - coupling 2 - risk 1 = 4

- [ ] **Step 1: Write the failing tests**

In `web/src/pages/settings.test.tsx`, add `import { setViewport } from "@/test/media"` and append:

```tsx
describe("Settings page layout", () => {
  it("sums up the config in the header", async () => {
    mockApi(routes())
    renderApp("/settings")
    expect(await screen.findByText("Serving darkraise in queue mode")).toBeInTheDocument()
  })

  it("indexes every section at 1280px", async () => {
    setViewport(1280)
    mockApi(routes())
    renderApp("/settings")
    const nav = within(await screen.findByRole("navigation", { name: "Sections" }))
    expect(nav.getAllByRole("button").map((b) => b.textContent)).toEqual([
      "General",
      "Timing",
      "Disk and retention",
      "Runner defaults",
      "GitHub token",
      "Runner and config",
      "Account",
    ])
  })

  it("counts only config fields in the save bar", async () => {
    mockApi(routes())
    const { user } = renderApp("/settings")
    await user.type(await screen.findByLabelText("Current password"), "old password 1")
    expect(screen.queryByText(/unsaved change/)).toBeNull()
  })
})

describe("Account section", () => {
  async function fill(user: ReturnType<typeof renderApp>["user"], current: string, next: string, again: string) {
    await user.type(await screen.findByLabelText("Current password"), current)
    await user.type(screen.getByLabelText("New password"), next)
    await user.type(screen.getByLabelText("Confirm new password"), again)
    await user.click(screen.getByRole("button", { name: "Change password" }))
  }

  it("changes the password", async () => {
    const { calls } = mockApi(routes({ "POST /auth/password": () => noContent() }))
    const { user } = renderApp("/settings")
    await fill(user, "old password 1", "new password 12", "new password 12")
    await waitFor(() => expect(calls.some((c) => c.path === "/auth/password")).toBe(true))
    expect(calls.find((c) => c.path === "/auth/password")?.body).toEqual({ current: "old password 1", new: "new password 12" })
    expect((await screen.findAllByText("Password changed")).length).toBeGreaterThan(0)
    expect(screen.getByLabelText("Current password")).toHaveValue("")
  })

  it("checks the new password before sending", async () => {
    const { calls } = mockApi(routes())
    const { user } = renderApp("/settings")
    await fill(user, "old password 1", "short", "short")
    expect(await screen.findByText("Password must be 12 to 1024 bytes")).toBeInTheDocument()
    await user.clear(screen.getByLabelText("New password"))
    await user.type(screen.getByLabelText("New password"), "new password 12")
    await user.click(screen.getByRole("button", { name: "Change password" }))
    expect(await screen.findByText("The passwords do not match")).toBeInTheDocument()
    expect(calls.some((c) => c.path === "/auth/password")).toBe(false)
  })

  it("says when the current password is wrong", async () => {
    mockApi(routes({ "POST /auth/password": () => json({ error: "wrong password" }, 401) }))
    const { user, router } = renderApp("/settings")
    await fill(user, "bad password 1", "new password 12", "new password 12")
    expect(await screen.findByRole("alert")).toHaveTextContent("The current password is wrong")
    expect(router.state.location.pathname).toBe("/settings")
  })

  it("leaves Log out to the shell, which leaves without asking about unsaved edits", async () => {
    let authenticated = true
    mockApi(
      routes({
        "GET /auth/state": () => ({ setup_required: false, authenticated }),
        "POST /auth/logout": () => {
          authenticated = false
          return noContent()
        },
      }),
    )
    const { user, router } = renderApp("/settings")
    const account = within(await screen.findByRole("region", { name: "Account" }))
    expect(account.queryByRole("button", { name: "Log out" })).toBeNull()
    const poll = await screen.findByLabelText("Poll interval")
    await user.clear(poll)
    await user.type(poll, "15s")
    await user.click(screen.getByRole("button", { name: "Log out" }))
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"))
    expect(screen.queryByText("Unsaved changes")).toBeNull()
  })
})
```

Append to `web/src/a11y.test.tsx`:

```tsx
describe("Settings accessibility", () => {
  it(
    "has no axe violations at 1280px",
    async () => {
      setViewport(1280)
      mockApi(authedRoutes({ "GET /api/token": fixtures.token }))
      renderApp("/settings")
      await screen.findByRole("navigation", { name: "Sections" })
      await screen.findByText("4,980 of 5,000, resets 14:45")
      expect(await violations()).toEqual([])
    },
    30_000,
  )
})
```

The shell renders one "Log out" button (web/src/components/shell.tsx:102, navigating with `ignoreBlocker: true` at :78), which `shell.test.tsx` already finds as a single button.

- [ ] **Step 2: Run the tests to verify they fail**

Run (in `web/`): `npx vitest run src/pages/settings.test.tsx src/a11y.test.tsx`
Expected: FAIL: no header description, no section index, the account card still has Log out, and the messages are lower case.

- [ ] **Step 3: Write the implementation**

Create `web/src/components/account-section.tsx`:

```tsx
import { Button } from "darkraise-ui/components/button"
import { Input } from "darkraise-ui/components/input"
import { toast } from "darkraise-ui/components/sonner"
import { useState, type FormEvent } from "react"
import { ApiError, api } from "@/api/client"
import { ErrorLine } from "@/components/page/error-line"
import { Field, FieldGroup } from "@/components/page/field"
import { errorText } from "@/query"

function lengthOk(password: string): boolean {
  const n = new TextEncoder().encode(password).length
  return n >= 12 && n <= 1024
}

export function AccountSection() {
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [again, setAgain] = useState("")
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)

  async function change(e: FormEvent) {
    e.preventDefault()
    setError("")
    if (!lengthOk(next)) return setError("Password must be 12 to 1024 bytes")
    if (next !== again) return setError("The passwords do not match")
    setBusy(true)
    try {
      await api.changePassword(current, next)
      toast.success("Password changed")
      setCurrent("")
      setNext("")
      setAgain("")
    } catch (err) {
      setError(err instanceof ApiError && err.status === 401 ? "The current password is wrong" : errorText(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <FieldGroup id="account" title="Account" note="Changing the password takes effect at once, outside the save bar.">
      <form className="divide-y divide-border" onSubmit={(e) => void change(e)}>
        <Field label="Current password" htmlFor="current-password">
          <Input
            id="current-password"
            type="password"
            autoComplete="current-password"
            className="max-w-xs"
            value={current}
            onChange={(e) => setCurrent(e.target.value)}
          />
        </Field>
        <Field label="New password" htmlFor="new-password" help="12 to 1024 bytes">
          <Input id="new-password" type="password" autoComplete="new-password" className="max-w-xs" value={next} onChange={(e) => setNext(e.target.value)} />
        </Field>
        <Field label="Confirm new password" htmlFor="confirm-password">
          <Input
            id="confirm-password"
            type="password"
            autoComplete="new-password"
            className="max-w-xs"
            value={again}
            onChange={(e) => setAgain(e.target.value)}
          />
        </Field>
        {error && (
          <div className="py-3">
            <ErrorLine>{error}</ErrorLine>
          </div>
        )}
        <div className="py-3">
          <Button type="submit" loading={busy}>
            Change password
          </Button>
        </div>
      </form>
    </FieldGroup>
  )
}
```

Replace `web/src/pages/settings.tsx` with:

```tsx
import { Spinner } from "darkraise-ui/components/spinner"
import { PageHeader } from "darkraise-ui/layout"
import { AccountSection } from "@/components/account-section"
import { SectionNav } from "@/components/page/section-nav"
import { RunnerSection } from "@/components/runner-section"
import { SaveBar } from "@/components/save-bar"
import { SettingsSections } from "@/components/settings-form"
import { TokenSection } from "@/components/token-section"
import { UnsavedGuard } from "@/components/unsaved-guard"
import { useSettingsForm } from "@/lib/settings-form"

const SECTIONS = [
  { id: "general", title: "General" },
  { id: "timing", title: "Timing" },
  { id: "disk", title: "Disk and retention" },
  { id: "runner-defaults", title: "Runner defaults" },
  { id: "token", title: "GitHub token" },
  { id: "runner", title: "Runner and config" },
  { id: "account", title: "Account" },
] as const

// The save bar counts config fields only; the token, runner and account
// sections act at once with their own buttons.
export function SettingsPage() {
  const form = useSettingsForm()
  const c = form.config
  return (
    <div className="flex flex-col gap-4">
      <PageHeader title="Settings" description={c ? `Serving ${c.owner} in ${c.mode} mode` : undefined} />
      {!c ? (
        <Spinner label="Loading" />
      ) : (
        <>
          <div className="grid gap-6 xl:grid-cols-[minmax(0,1fr)_12rem]">
            <div className="flex min-w-0 flex-col gap-6">
              <SettingsSections form={form} />
              <TokenSection />
              <RunnerSection />
              <AccountSection />
            </div>
            <SectionNav items={SECTIONS} />
          </div>
          <SaveBar count={form.changed.length} saving={form.saving} disabled={form.offline} onSave={() => void form.save()} onDiscard={form.discard} />
          <UnsavedGuard count={form.changed.length} page="Settings" saving={form.saving} onSave={form.save} onDiscard={form.discard} />
        </>
      )}
    </div>
  )
}
```

Delete `web/src/components/account-card.tsx` and `web/src/components/settings-cards.test.tsx` (its remaining Account cases now live in `pages/settings.test.tsx`; confirm `grep -rn "account-card\|AccountCard\|settings-cards" web/src` prints nothing).

In `web/src/anti-slop.test.ts`, add `"src/components/account-section.tsx"` to the components group and `"src/pages/settings.tsx"` to the pages group.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npx vitest run src/pages src/components src/a11y.test.tsx src/anti-slop.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add -A web/src
git commit -m "feat(web): rebuild the Settings page"
```

---

### Task 22: Retire the badges and walk every source file

**Files:**
- Delete: `web/src/components/state-badge.tsx`, `web/src/components/stepper.tsx`
- Modify: `web/src/lib/status.ts` (remove `BadgeVariant`, `variants`, `stateVariant`)
- Modify: `web/src/anti-slop.test.ts` (replace)
- Test: `web/src/components/components.test.tsx` (remove the `StateBadge` block)

**Interfaces:**
- Consumes: every page rebuilt (Tasks 1 to 21, and plan 2), so no source file still imports `StateBadge`, `Glyph` or the kit's `Badge` outside `TagField`.
- Produces: the anti-slop check over every source file.

**Items:** 2, 15, 16

**Implementer:** dr-superpowers:impl-sonnet-medium
**Evaluation:** files 2 - spec 0 - coupling 1 - risk 1 = 4

- [ ] **Step 1: Write the failing test**

Replace `web/src/anti-slop.test.ts` with:

```ts
import { readdirSync, readFileSync } from "node:fs"
import { dirname, join, relative, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import ts from "typescript"
import { describe, expect, it } from "vitest"

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..")
const testHelpers = resolve(root, "src", "test")

// Every source file under src, plus the theme script. Tests and the test
// helpers hold fixture copy rather than UI, so they are left out.
function sourceFiles(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const path = join(dir, entry.name)
    if (entry.isDirectory()) return path === testHelpers ? [] : sourceFiles(path)
    return /\.(tsx?|css)$/.test(entry.name) && !/\.test\.tsx?$/.test(entry.name) ? [path] : []
  })
}

const FILES = [...sourceFiles(resolve(root, "src")).map((p) => relative(root, p).split("\\").join("/")), "public/theme-init.js"].sort()

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

// Tailwind 4 names its gradients bg-linear-*, bg-radial and bg-conic (with
// or without a suffix); Tailwind 3 named them bg-gradient-to-*.
const GRADIENT_UTILITY = /\bbg-(?:linear-|gradient-to-|radial(?:-|\b)|conic(?:-|\b))/

function styleViolations(text: string): string[] {
  const found: string[] = []
  if (/backdrop-filter|backdrop-blur-/.test(text)) found.push("backdrop blur")
  if (/background-clip:\s*text|bg-clip-text/.test(text)) found.push("background-clip: text")
  if (GRADIENT_UTILITY.test(text)) found.push("gradient utility")
  for (const m of text.matchAll(/(?:linear|radial)-gradient\(/g)) {
    const args = gradientArgs(text, (m.index ?? 0) + m[0].length)
    const stops = args.filter((a, i) => !(i === 0 && /^(to |at |circle|ellipse|closest|farthest|[-\d.]+(deg|turn|rad|grad))/.test(a)))
    if (stops.length > 1) found.push(`${m[0]}${args.join(", ")})`)
  }
  return found
}

// TagField's removable chips are controls, so they keep the kit's Badge.
const BADGE_ALLOWED = "src/components/tag-field.tsx"
const RETIRED = /darkraise-ui\/components\/badge|@\/components\/(?:glyph|state-badge)["/]/

describe("anti-slop rules", () => {
  it("catch what they are meant to catch", () => {
    expect(dashLiterals("x.tsx", 'const a = "one — two"; const b = <p>three – four</p>; const c = `five ${a} — six`')).toHaveLength(3)
    expect(dashLiterals("x.ts", "// a comment — is fine\nconst a = 1")).toEqual([])
    expect(styleViolations("background: linear-gradient(to right, red, blue)")).toHaveLength(1)
    expect(styleViolations("background: radial-gradient(circle, hsl(0 0% 0%), transparent)")).toHaveLength(1)
    expect(styleViolations("background: linear-gradient(red)")).toEqual([])
    expect(styleViolations('className="backdrop-blur-sm"')).toHaveLength(1)
    expect(styleViolations('className="bg-clip-text"')).toHaveLength(1)
    expect(styleViolations('className="bg-linear-to-r from-primary to-card"')).toHaveLength(1)
    expect(styleViolations('className="bg-gradient-to-b from-card"')).toHaveLength(1)
    expect(styleViolations('className="bg-radial from-primary"')).toHaveLength(1)
    expect(styleViolations('className="bg-radial-[at_25%_25%]"')).toHaveLength(1)
    expect(styleViolations('className="bg-conic-180 from-primary"')).toHaveLength(1)
    expect(styleViolations('className="bg-card bg-muted bg-primary/45"')).toEqual([])
    expect(RETIRED.test('import { Badge } from "darkraise-ui/components/badge"')).toBe(true)
    expect(RETIRED.test('import { StateBadge } from "@/components/state-badge"')).toBe(true)
    expect(RETIRED.test('import { Glyph } from "@/components/glyph"')).toBe(true)
    expect(RETIRED.test('import { ResultIcon } from "@/components/result-icon"')).toBe(false)
  })

  it("walk every source file and leave tests and test helpers out", () => {
    expect(FILES).toContain("public/theme-init.js")
    expect(FILES).toContain("src/pages/settings.tsx")
    expect(FILES).toContain("src/styles/ghr-theme.css")
    expect(FILES.filter((f) => f.includes(".test."))).toEqual([])
    expect(FILES.filter((f) => f.startsWith("src/test/"))).toEqual([])
  })

  it("keep the kit's Badge to TagField and import no Glyph or StateBadge", () => {
    const offenders = FILES.filter((file) => file !== BADGE_ALLOWED && RETIRED.test(readFileSync(resolve(root, file), "utf8")))
    expect(offenders).toEqual([])
  })

  it.each(FILES)("%s follows them", (file) => {
    const text = readFileSync(resolve(root, file), "utf8")
    const dashes = file.endsWith(".css") ? (text.match(/"[^"\n]*[–—][^"\n]*"/g) ?? []) : dashLiterals(file, text)
    expect(dashes).toEqual([])
    expect(styleViolations(text)).toEqual([])
  })
})
```

In `web/src/components/components.test.tsx`, delete the `describe("StateBadge", …)` block and the two imports `import { stateVariant } from "@/lib/status"` and `import { StateBadge } from "./state-badge"`.

- [ ] **Step 2: Run the test to verify it fails**

Run (in `web/`): `npx vitest run src/anti-slop.test.ts`
Expected: FAIL: "keep the kit's Badge to TagField…" lists `src/components/state-badge.tsx` and `src/lib/status.ts`. Any other file it lists, or any dash or gradient finding in a walked file, is a defect an earlier task left: fix it in this task before going on (Task 9's and Task 19's deletions removed the two known dash files, `lib/activity.ts` and `components/token-card.tsx`).

- [ ] **Step 3: Write the implementation**

- Delete `web/src/components/state-badge.tsx` and `web/src/components/stepper.tsx` (neither has a caller: confirm with `grep -rn "state-badge\|StateBadge\|stepper\|Stepper" web/src --include=*.ts --include=*.tsx` printing only lines in `anti-slop.test.ts`, once both files are gone).
- In `web/src/lib/status.ts`, delete the line `import type { BadgeVariant } from "darkraise-ui/components/badge"`, the `variants` record and the `stateVariant` function. Keep every other export.

- [ ] **Step 4: Run the tests to verify they pass**

Run (in `web/`): `npm run typecheck && npx vitest run src/anti-slop.test.ts src/components/components.test.tsx && npx vitest run`
Expected: PASS; the anti-slop run lists every walked file with none failing.

- [ ] **Step 5: Commit**

```bash
cd web && npm run typecheck && npm run lint && npx vitest run && cd ..
git add -A web/src
git commit -m "test(web): walk every source file for anti-slop"
```
